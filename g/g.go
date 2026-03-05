package g

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

var (
	goroPfx        = []byte("goroutine ")
	lockedToThread = []byte(", locked to thread")
	minutesSfx     = []byte(" minutes")
	synctestPfx    = []byte(", synctest bubble ")

	createdByPfx = []byte("created by ")
	inGoro       = []byte(" in goroutine ")

	goroUnavailable = []byte("goroutine running on other thread; stack unavailable")
	nonGoFunction   = []byte("non-Go function")

	additionalFramesElided = []byte("...additional frames elided...")
	nFramesElidedPfx       = []byte("...")
	nFramesElidedSfx       = []byte(" frames elided...")

	originatingPfx = []byte("[originating from goroutine ")

	crashSeparator = []byte("-----")

	// the following maps memoize strings
	callNames  = make(map[string]string, 1000)
	fileNames  = make(map[string]string, 100)
	statusStrs = make(map[string]string, 50)
)

func strmap(m map[string]string, in []byte) string {
	s, ok := m[string(in)]
	if !ok {
		s = string(in)
		m[s] = s
	}
	return s
}

func parseInt(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	var i int
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		i = 10*i + int(c-'0')
	}
	return i, true
}

// parseNewG parses a goroutine header line.
//
// Format from runtime/traceback.go:
//
//	goroutine <ID>[ gp=<ptr> m=<id> mp=<ptr>] [<status>[ (leaked)][ (scan)][ (durable)][, <N> minutes][, locked to thread][, synctest bubble <N>][ labels:{...}]]:
func parseNewG(in []byte) (g *goroutine, ok bool) {
	if !bytes.HasPrefix(in, goroPfx) {
		return nil, false
	}
	in = in[len(goroPfx):]

	// Parse ID.
	idEnd := bytes.IndexByte(in, ' ')
	if idEnd < 1 {
		return nil, false
	}
	id, ok := parseInt(in[:idEnd])
	if !ok {
		return nil, false
	}
	in = in[idEnd+1:]

	// Skip optional "gp=0x... m=N mp=0x..." debug info before the '['.
	bracket := bytes.IndexByte(in, '[')
	if bracket < 0 {
		return nil, false
	}
	in = in[bracket:]

	// Must be [...]:
	l := len(in)
	if l < 4 || in[0] != '[' || in[l-1] != ':' || in[l-2] != ']' {
		return nil, false
	}
	inner := in[1 : l-2] // content between [ and ]

	g = &goroutine{
		id:             id,
		parentGoid:     -1,
		synctestBubble: -1,
	}

	// Strip labels:{...} from end if present.
	if idx := bytes.Index(inner, []byte(" labels:{")); idx >= 0 {
		labelsRaw := inner[idx+len(" labels:{"):]
		if len(labelsRaw) > 0 && labelsRaw[len(labelsRaw)-1] == '}' {
			labelsRaw = labelsRaw[:len(labelsRaw)-1]
			g.labels = parseLabels(labelsRaw)
		}
		inner = inner[:idx]
	}

	// Strip comma-separated tail annotations right to left.
	// Order: , synctest bubble N  |  , locked to thread  |  , N minutes
	if idx := bytes.Index(inner, synctestPfx); idx >= 0 {
		numRaw := inner[idx+len(synctestPfx):]
		if n, ok := parseInt(numRaw); ok {
			g.synctestBubble = n
		}
		inner = inner[:idx]
	}

	if bytes.HasSuffix(inner, lockedToThread) {
		inner = inner[:len(inner)-len(lockedToThread)]
		g.locked = true
	}

	// Check for ", N minutes".
	if idx := bytes.LastIndex(inner, []byte(", ")); idx >= 0 {
		tail := inner[idx+2:]
		if bytes.HasSuffix(tail, minutesSfx) {
			numRaw := tail[:len(tail)-len(minutesSfx)]
			if n, ok := parseInt(numRaw); ok {
				g.minutes = n
				inner = inner[:idx]
			}
		}
	}

	// What remains is status possibly with (leaked), (scan), (durable).
	status := inner
	if bytes.HasSuffix(status, []byte(" (durable)")) {
		status = status[:len(status)-len(" (durable)")]
		g.durable = true
	}
	if bytes.HasSuffix(status, []byte(" (scan)")) {
		status = status[:len(status)-len(" (scan)")]
		g.scan = true
	}
	if bytes.HasSuffix(status, []byte(" (leaked)")) {
		status = status[:len(status)-len(" (leaked)")]
		g.leaked = true
	}

	g.status = strmap(statusStrs, status)
	return g, true
}

// parseLabels parses the content inside labels:{...}.
// Format: "key":"value", "key2":"value2"
func parseLabels(in []byte) []label {
	var labels []label
	for len(in) > 0 {
		in = bytes.TrimLeft(in, " ,")
		if len(in) == 0 {
			break
		}
		// Expect "key"
		key, rest, ok := parseQuoted(in)
		if !ok {
			break
		}
		in = rest
		// Expect :
		if len(in) == 0 || in[0] != ':' {
			break
		}
		in = in[1:]
		// Expect "value"
		value, rest, ok := parseQuoted(in)
		if !ok {
			break
		}
		in = rest
		labels = append(labels, label{key: key, value: value})
	}
	return labels
}

// parseQuoted parses a Go-style quoted string at the start of in.
func parseQuoted(in []byte) (string, []byte, bool) {
	if len(in) == 0 || in[0] != '"' {
		return "", in, false
	}
	s, err := strconv.Unquote(string(grabQuoted(in)))
	if err != nil {
		return "", in, false
	}
	// Advance past the quoted string.
	n := len(strconv.Quote(s))
	return s, in[n:], true
}

// grabQuoted returns the quoted portion including surrounding quotes.
func grabQuoted(in []byte) string {
	if len(in) < 2 || in[0] != '"' {
		return ""
	}
	for i := 1; i < len(in); i++ {
		if in[i] == '\\' {
			i++ // skip escaped char
			continue
		}
		if in[i] == '"' {
			return string(in[:i+1])
		}
	}
	return ""
}

func parseCall(in []byte) (name, args []byte, inline, ok bool) {
	lParen := bytes.LastIndexByte(in, '(')
	if lParen == -1 {
		return nil, nil, false, false
	}
	name = in[:lParen]
	in = in[lParen+1:]
	rParen := bytes.IndexByte(in, ')')
	if rParen == -1 || rParen != len(in)-1 {
		return nil, nil, false, false
	}
	in = in[:rParen]
	if len(in) == 3 && in[0] == '.' && in[1] == '.' && in[2] == '.' {
		return name, nil, true, true
	}
	return name, in, false, true
}

func parseFile(in []byte) (file []byte, line int, ok bool) {
	// Strip leading whitespace (tab or spaces).
	if len(in) > 0 && in[0] == '\t' {
		in = in[1:]
	} else {
		i := 0
		for ; i < len(in) && in[i] == ' '; i++ {
		}
		in = in[i:]
	}

	colon := bytes.IndexByte(in, ':')
	if colon < 1 {
		return nil, 0, false
	}
	file = in[:colon]
	in = in[colon+1:]

	// Parse line number, stop at first non-digit.
	endLineNum := 0
	for endLineNum < len(in) && in[endLineNum] >= '0' && in[endLineNum] <= '9' {
		endLineNum++
	}
	if endLineNum == 0 {
		return nil, 0, false
	}
	lineNum, _ := parseInt(in[:endLineNum])
	// Ignore trailing " +0x...", " fp=...", etc.
	return file, lineNum, true
}

type parser struct {
	g    *goroutine
	next func(*parser, []byte)

	seenGoroutine    bool // true once we've parsed at least one goroutine
	corrupt          bool
	corruptFatal     bool
	expectingPostEnd bool // true when we might see ancestors after a blank line
	mayHaveAncestor  bool // true after created-by file parsed (before blank line)

	// For ancestor parsing.
	ancestor *ancestor
}

type ParseOpt func(*parser) error

func ParseCorruptFatally(p *parser) error {
	p.corruptFatal = true
	return nil
}

func Parse(r io.Reader, opts ...ParseOpt) (*Dump, error) {
	p := new(parser)
	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	// Auto-detect a common prefix on lines (e.g. CI log prefixes
	// like "job name\tSTEP\t2006-01-02T15:04:05.999Z content").
	// We scan for the first "goroutine " header and use the byte
	// offset as the prefix length to strip from all lines. The
	// prefix content may vary per line (timestamps), so we strip
	// by length, not by exact match.
	prefixLen := 0
	prefixDetected := false

	stripPrefix := func(line []byte) []byte {
		if prefixLen > 0 && len(line) >= prefixLen {
			line = line[prefixLen:]
		}
		// CI blank lines are prefix + whitespace; trim to empty.
		if len(line) > 0 && len(bytes.TrimSpace(line)) == 0 {
			return line[:0]
		}
		return line
	}

	gs := make([]*goroutine, 0, 10000)
	for scanner.Scan() {
		line := scanner.Bytes()

		if !prefixDetected {
			if idx := bytes.Index(line, goroPfx); idx > 0 {
				prefixLen = idx
				prefixDetected = true
			} else if idx == 0 {
				prefixDetected = true
			} else {
				// Skip lines before the first goroutine header.
				continue
			}
		}

		if g := p.parse(stripPrefix(line)); g != nil {
			gs = append(gs, g)
		}
	}
	// Flush the last goroutine if pending.
	if g := p.flush(); g != nil {
		gs = append(gs, g)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(gs) == 0 {
		return nil, errors.New("no goroutines found")
	}
	return &Dump{gs: gs}, nil
}

func (p *parser) flush() *goroutine {
	if p.g == nil {
		return nil
	}
	if p.ancestor != nil {
		p.g.ancestors = append(p.g.ancestors, *p.ancestor)
		p.ancestor = nil
	}
	g := p.g
	p.g = nil
	p.next = nil
	p.corrupt = false
	p.expectingPostEnd = false
	p.mayHaveAncestor = false
	return g
}

func (p *parser) parse(line []byte) *goroutine {
	// Handle crash separator.
	if bytes.Equal(line, crashSeparator) {
		return p.flush()
	}

	switch {
	case len(line) == 0:
		if p.g == nil {
			return nil
		}
		if p.ancestor != nil {
			// End of an ancestor section. The goroutine may have
			// more ancestor sections following.
			p.g.ancestors = append(p.g.ancestors, *p.ancestor)
			p.ancestor = nil
			p.expectingPostEnd = true
			p.next = nil
			return nil
		}
		if p.mayHaveAncestor || p.expectingPostEnd {
			// We just finished the created-by (or a previous
			// ancestor). There might be [originating from ...] next.
			p.expectingPostEnd = true
			p.mayHaveAncestor = false
			p.next = nil
			return nil
		}
		return p.flush()

	case p.corrupt:
		return nil

	case p.g == nil && p.expectingPostEnd:
		// We're between the end of a goroutine's main stack (after
		// blank line) and a possible ancestor section.
		// This is really p.g != nil conceptually but we haven't set it;
		// actually p.g should still be set. Let's handle this via
		// expectingPostEnd with g still set.
		// This shouldn't happen because we don't nil p.g when
		// expectingPostEnd is set. Fall through.
		p.expectingPostEnd = false
		p.setCorrupt("unexpected state", line)
		return nil

	case p.expectingPostEnd:
		// After a blank line post created-by or ancestor, check for
		// originating line. If not, this line starts a new goroutine
		// (or junk), so flush and re-parse.
		p.expectingPostEnd = false
		if bytes.HasPrefix(line, originatingPfx) {
			parseOriginatingLine(p, line)
			if p.corrupt {
				return nil
			}
			return nil
		}
		// Not an ancestor — flush current goroutine and re-parse this
		// line as a new goroutine header.
		g := p.flush()
		if newG := p.parse(line); newG != nil {
			// This shouldn't normally happen (new goroutine parsed
			// in one line), but handle it.
			_ = newG
		}
		return g

	case p.g == nil:
		// Looking for a new goroutine header.
		g, ok := parseNewG(line)
		if !ok {
			if !p.seenGoroutine {
				// Skip junk before first goroutine.
				return nil
			}
			p.setCorrupt("failed looking for new goroutine", line)
			return nil
		}
		p.seenGoroutine = true
		p.g = g
		p.next = stateFirstCall
		return nil

	default:
		p.next(p, line)
		if p.corrupt || p.next != nil {
			return nil
		}
		// next == nil means we're done with this goroutine.
		g := p.g
		p.g = nil
		p.corrupt = false
		return g
	}
}

func (p *parser) lastFrame() *frame {
	if p.ancestor != nil {
		return &p.ancestor.frames[len(p.ancestor.frames)-1]
	}
	return &p.g.stack[len(p.g.stack)-1]
}

func stateFirstCall(p *parser, line []byte) {
	// Check for unavailable stack (the "goroutine running on other thread" message).
	trimmed := line
	if len(trimmed) > 0 && trimmed[0] == '\t' {
		trimmed = trimmed[1:]
	}
	if bytes.Equal(trimmed, goroUnavailable) {
		// Create a synthetic frame to hold the unavailable marker.
		p.g.stack = append(p.g.stack, frame{unavailable: true})
		p.next = stateCallOrEnd
		return
	}

	// Check for "non-Go function" (CGO).
	if bytes.HasPrefix(line, nonGoFunction) {
		f := frame{cgo: true}
		p.g.stack = append(p.g.stack, f)
		p.next = stateCgoFileOrNext
		return
	}

	name, args, inline, ok := parseCall(line)
	if !ok {
		p.setCorrupt("failed looking for first function call", line)
		return
	}
	stateParseCall(p, name, args, inline)
	p.next = stateUnavailOrFile
}

func stateParseCall(p *parser, rawName, args []byte, inline bool) {
	f := frame{
		call: call{
			name: strmap(callNames, rawName),
		},
		inline: inline,
	}
	if !inline {
		f.args = string(args)
	}

	if p.ancestor != nil {
		p.ancestor.frames = append(p.ancestor.frames, f)
	} else {
		if len(p.g.stack) == 0 {
			p.g.stack = make([]frame, 0, 6)
		}
		p.g.stack = append(p.g.stack, f)
	}
}

func stateUnavailOrFile(p *parser, line []byte) {
	trimmed := line
	if len(trimmed) > 0 && trimmed[0] == '\t' {
		trimmed = trimmed[1:]
	}
	if bytes.Equal(trimmed, goroUnavailable) {
		p.lastFrame().unavailable = true
		p.next = stateCallOrEnd
		return
	}
	stateFile(p, line)
}

func stateFile(p *parser, line []byte) {
	file, lineNum, ok := parseFile(line)
	if !ok {
		p.setCorrupt("failed looking for file", line)
		return
	}
	f := p.lastFrame()
	f.call.file = strmap(fileNames, file)
	f.call.line = lineNum
	p.next = stateCallOrEnd
}

func stateCgoFileOrNext(p *parser, line []byte) {
	// After "non-Go function", there may be a file:line or the next
	// call/created-by/elision/etc.
	if len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
		file, lineNum, ok := parseFile(line)
		if ok {
			f := p.lastFrame()
			f.call.file = strmap(fileNames, file)
			f.call.line = lineNum
			p.next = stateCallOrEnd
			return
		}
	}
	// Not a file line, treat as next call/end.
	stateCallOrEnd(p, line)
}

func stateCallOrEnd(p *parser, line []byte) {
	// Check for frame elision.
	if bytes.HasPrefix(line, nFramesElidedPfx) {
		if bytes.Equal(line, additionalFramesElided) {
			p.g.framesElided = true
			p.next = stateCreatedByOrEnd
			return
		}
		if bytes.HasSuffix(line, nFramesElidedSfx) {
			inner := line[len(nFramesElidedPfx) : len(line)-len(nFramesElidedSfx)]
			if n, ok := parseInt(inner); ok {
				p.g.framesElided = true
				p.g.elidedCount = n
				p.next = stateCreatedByOrEnd
				return
			}
		}
	}

	// Check for "created by ..."
	if bytes.HasPrefix(line, createdByPfx) {
		parseCreatedBy(p, line)
		return
	}

	// Check for CGO frames.
	if bytes.HasPrefix(line, nonGoFunction) {
		f := frame{cgo: true}
		if p.ancestor != nil {
			p.ancestor.frames = append(p.ancestor.frames, f)
		} else {
			p.g.stack = append(p.g.stack, f)
		}
		p.next = stateCgoFileOrNext
		return
	}

	// Otherwise, expect a function call.
	name, args, inline, ok := parseCall(line)
	if !ok {
		p.setCorrupt("failed looking for function call", line)
		return
	}
	stateParseCall(p, name, args, inline)
	p.next = stateFile
}

func parseCreatedBy(p *parser, line []byte) {
	rest := line[len(createdByPfx):]
	parentGoid := -1
	if idx := bytes.Index(rest, inGoro); idx >= 0 {
		goidRaw := rest[idx+len(inGoro):]
		if n, ok := parseInt(goidRaw); ok {
			parentGoid = n
		}
		rest = rest[:idx]
	}
	if p.ancestor != nil {
		p.ancestor.createdBy.name = strmap(callNames, rest)
		p.next = stateAncestorCreatedByFile
	} else {
		p.g.createdBy.name = strmap(callNames, rest)
		p.g.parentGoid = parentGoid
		p.next = stateCreatedByFile
	}
}

func stateCreatedByFile(p *parser, line []byte) {
	file, lineNum, ok := parseFile(line)
	if !ok {
		p.setCorrupt("failed looking for created by file", line)
		return
	}
	p.g.createdBy.file = strmap(fileNames, file)
	p.g.createdBy.line = lineNum
	p.mayHaveAncestor = true
	p.next = stateAncestorOrEnd
}

func stateAncestorCreatedByFile(p *parser, line []byte) {
	file, lineNum, ok := parseFile(line)
	if !ok {
		p.setCorrupt("failed looking for ancestor created-by file", line)
		return
	}
	p.ancestor.createdBy.file = strmap(fileNames, file)
	p.ancestor.createdBy.line = lineNum
	p.next = stateAncestorCallOrEnd
}

func stateCreatedByOrEnd(p *parser, line []byte) {
	if bytes.HasPrefix(line, createdByPfx) {
		parseCreatedBy(p, line)
		return
	}
	p.setCorrupt("expected created by", line)
}

// stateAncestorOrEnd is set after created-by file is parsed. The blank line
// handler in parse() uses this to know it should look for ancestors.
func stateAncestorOrEnd(p *parser, line []byte) {
	// This state is only reached for non-blank lines. If we get here,
	// it means something unexpected after created-by file.
	if bytes.HasPrefix(line, originatingPfx) {
		parseOriginatingLine(p, line)
		return
	}
	p.next = nil // signal done
}

func parseOriginatingLine(p *parser, line []byte) {
	// [originating from goroutine N]:
	rest := line[len(originatingPfx):]
	if len(rest) < 3 || rest[len(rest)-1] != ':' || rest[len(rest)-2] != ']' {
		p.setCorrupt("bad originating line", line)
		return
	}
	goidRaw := rest[:len(rest)-2]
	goid, ok := parseInt(goidRaw)
	if !ok {
		p.setCorrupt("bad originating goid", line)
		return
	}
	p.ancestor = &ancestor{goid: goid}
	p.next = stateAncestorFirstCall
}

func stateAncestorFirstCall(p *parser, line []byte) {
	if bytes.HasPrefix(line, nonGoFunction) {
		f := frame{cgo: true}
		p.ancestor.frames = append(p.ancestor.frames, f)
		p.next = stateCgoFileOrNext
		return
	}
	name, args, inline, ok := parseCall(line)
	if !ok {
		p.setCorrupt("failed looking for ancestor first call", line)
		return
	}
	stateParseCall(p, name, args, inline)
	p.next = stateAncestorFile
}

func stateAncestorFile(p *parser, line []byte) {
	file, lineNum, ok := parseFile(line)
	if !ok {
		p.setCorrupt("failed looking for ancestor file", line)
		return
	}
	f := p.lastFrame()
	f.call.file = strmap(fileNames, file)
	f.call.line = lineNum
	p.next = stateAncestorCallOrEnd
}

func stateAncestorCallOrEnd(p *parser, line []byte) {
	// Check for elision.
	if bytes.Equal(line, additionalFramesElided) {
		p.next = stateAncestorCreatedByOrEnd
		return
	}
	// Check for created by.
	if bytes.HasPrefix(line, createdByPfx) {
		parseCreatedBy(p, line)
		return
	}
	// Another call.
	if bytes.HasPrefix(line, nonGoFunction) {
		f := frame{cgo: true}
		p.ancestor.frames = append(p.ancestor.frames, f)
		p.next = stateCgoFileOrNext
		return
	}
	name, args, inline, ok := parseCall(line)
	if !ok {
		p.setCorrupt("failed looking for ancestor call", line)
		return
	}
	stateParseCall(p, name, args, inline)
	p.next = stateAncestorFile
}

func stateAncestorCreatedByOrEnd(p *parser, line []byte) {
	if bytes.HasPrefix(line, createdByPfx) {
		parseCreatedBy(p, line)
		return
	}
	p.setCorrupt("expected ancestor created by", line)
}

func (p *parser) setCorrupt(why string, line []byte) {
	p.corrupt = true
	if p.g != nil {
		p.g = nil
		p.ancestor = nil
	}
	p.expectingPostEnd = false
	p.mayHaveAncestor = false
	if p.corruptFatal {
		fmt.Printf("%s on line %q\n", why, line)
		os.Exit(1)
	}
}
