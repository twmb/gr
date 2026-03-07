package cover

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Block struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	NumStmt   int
	Count     int
}

type Profile struct {
	Mode   string
	Blocks []Block
}

type FuncCoverage struct {
	File       string
	Func       string
	StartLine  int
	Statements int
	Covered    int
}

func (f *FuncCoverage) Percent() float64 {
	if f.Statements == 0 {
		return 100.0
	}
	return float64(f.Covered) / float64(f.Statements) * 100.0
}

type UncoveredBlock struct {
	File      string
	StartLine int
	EndLine   int
	NumStmt   int
}

type Result struct {
	Funcs       []FuncCoverage
	Uncovered   []UncoveredBlock
	TotalStmt   int
	CoveredStmt int
	ModPath     string
}

func ParseProfile(r io.Reader) (*Profile, error) {
	scanner := bufio.NewScanner(r)
	p := &Profile{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode: ") {
			p.Mode = line[len("mode: "):]
			continue
		}
		if line == "" {
			continue
		}
		b, ok := parseBlock(line)
		if !ok {
			continue
		}
		p.Blocks = append(p.Blocks, b)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if p.Mode == "" {
		return nil, fmt.Errorf("no mode line found in coverage profile")
	}
	if len(p.Blocks) == 0 {
		return nil, fmt.Errorf("no coverage blocks found")
	}
	return p, nil
}

func parseBlock(line string) (Block, bool) {
	// Format: file:startLine.startCol,endLine.endCol numStmt count
	lastSpace := strings.LastIndexByte(line, ' ')
	if lastSpace < 0 {
		return Block{}, false
	}
	count, err := strconv.Atoi(line[lastSpace+1:])
	if err != nil {
		return Block{}, false
	}
	rest := line[:lastSpace]

	lastSpace = strings.LastIndexByte(rest, ' ')
	if lastSpace < 0 {
		return Block{}, false
	}
	numStmt, err := strconv.Atoi(rest[lastSpace+1:])
	if err != nil {
		return Block{}, false
	}
	rest = rest[:lastSpace]

	// rest = file:startLine.startCol,endLine.endCol
	// Find the colon before the comma (handles Windows drive letters).
	commaIdx := strings.IndexByte(rest, ',')
	if commaIdx < 0 {
		return Block{}, false
	}
	colonIdx := strings.LastIndexByte(rest[:commaIdx], ':')
	if colonIdx < 0 {
		return Block{}, false
	}

	file := rest[:colonIdx]
	positions := rest[colonIdx+1:]

	parts := strings.SplitN(positions, ",", 2)
	if len(parts) != 2 {
		return Block{}, false
	}
	startParts := strings.SplitN(parts[0], ".", 2)
	endParts := strings.SplitN(parts[1], ".", 2)
	if len(startParts) != 2 || len(endParts) != 2 {
		return Block{}, false
	}

	startLine, _ := strconv.Atoi(startParts[0])
	startCol, _ := strconv.Atoi(startParts[1])
	endLine, _ := strconv.Atoi(endParts[0])
	endCol, _ := strconv.Atoi(endParts[1])

	return Block{
		File:      file,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
		NumStmt:   numStmt,
		Count:     count,
	}, true
}

func Analyze(p *Profile, dir string) (*Result, error) {
	var modRoot, modPath string
	if dir != "" {
		modRoot, modPath = findModuleInfoFrom(dir)
	} else {
		modRoot, modPath = findModuleInfo()
	}

	// Group blocks by file.
	fileBlocks := make(map[string][]Block)
	for _, b := range p.Blocks {
		fileBlocks[b.File] = append(fileBlocks[b.File], b)
	}

	r := &Result{ModPath: modPath}

	for profileFile, blocks := range fileBlocks {
		// Accumulate totals.
		for _, b := range blocks {
			r.TotalStmt += b.NumStmt
			if b.Count > 0 {
				r.CoveredStmt += b.NumStmt
			}
		}

		// Collect uncovered blocks.
		var uncov []UncoveredBlock
		for _, b := range blocks {
			if b.Count == 0 && b.NumStmt > 0 {
				uncov = append(uncov, UncoveredBlock{
					File:      profileFile,
					StartLine: b.StartLine,
					EndLine:   b.EndLine,
					NumStmt:   b.NumStmt,
				})
			}
		}
		r.Uncovered = append(r.Uncovered, mergeUncovered(uncov)...)

		// Try to resolve file and parse functions.
		resolved := resolveFile(modRoot, modPath, profileFile)
		extents, err := findFuncs(resolved)
		if err != nil {
			// Can't parse source; create file-level entry.
			stmts, covered := 0, 0
			for _, b := range blocks {
				stmts += b.NumStmt
				if b.Count > 0 {
					covered += b.NumStmt
				}
			}
			if stmts > 0 {
				r.Funcs = append(r.Funcs, FuncCoverage{
					File:       profileFile,
					Func:       "(file)",
					Statements: stmts,
					Covered:    covered,
				})
			}
			continue
		}

		// Map blocks to functions.
		for _, fn := range extents {
			fc := FuncCoverage{
				File:      profileFile,
				Func:      fn.name,
				StartLine: fn.startLine,
			}
			for _, b := range blocks {
				if b.StartLine >= fn.startLine && b.EndLine <= fn.endLine {
					fc.Statements += b.NumStmt
					if b.Count > 0 {
						fc.Covered += b.NumStmt
					}
				}
			}
			if fc.Statements > 0 {
				r.Funcs = append(r.Funcs, fc)
			}
		}
	}

	return r, nil
}

func mergeUncovered(blocks []UncoveredBlock) []UncoveredBlock {
	if len(blocks) <= 1 {
		return blocks
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].StartLine < blocks[j].StartLine
	})
	merged := []UncoveredBlock{blocks[0]}
	for _, b := range blocks[1:] {
		last := &merged[len(merged)-1]
		if b.StartLine <= last.EndLine+1 {
			if b.EndLine > last.EndLine {
				last.EndLine = b.EndLine
			}
			last.NumStmt += b.NumStmt
		} else {
			merged = append(merged, b)
		}
	}
	return merged
}

type funcExtent struct {
	name      string
	startLine int
	endLine   int
}

func findFuncs(filename string) ([]funcExtent, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, err
	}
	var funcs []funcExtent
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			name = recvName(fn.Recv.List[0].Type) + "." + name
		}
		start := fset.Position(fn.Pos())
		end := fset.Position(fn.End())
		funcs = append(funcs, funcExtent{
			name:      name,
			startLine: start.Line,
			endLine:   end.Line,
		})
	}
	return funcs, nil
}

func recvName(typ ast.Expr) string {
	switch t := typ.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	default:
		return "?"
	}
}

func findModuleInfo() (root, modPath string) {
	dir, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	return findModuleInfoFrom(dir)
}

func findModuleInfoFrom(dir string) (root, modPath string) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "module ") {
					return dir, strings.TrimSpace(line[len("module "):])
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func resolveFile(modRoot, modPath, profilePath string) string {
	if modPath != "" && strings.HasPrefix(profilePath, modPath+"/") {
		rel := profilePath[len(modPath)+1:]
		return filepath.Join(modRoot, rel)
	}
	return profilePath
}

// ShortFile strips the module path prefix for display.
func ShortFile(file, modPath string) string {
	if modPath != "" && strings.HasPrefix(file, modPath+"/") {
		return file[len(modPath)+1:]
	}
	return file
}
