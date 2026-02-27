package g

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type call struct {
	name string
	file string
	line int
}

type frame struct {
	call        call
	args        string // raw argument text (everything between parens)
	inline      bool   // true if args was "..."
	unavailable bool
	cgo         bool // true for "non-Go function" frames
}

type goroutine struct {
	id      int
	status  string
	minutes int
	locked  bool
	leaked  bool
	scan    bool
	durable bool

	framesElided bool
	elidedCount  int // N from "...N frames elided..."

	createdBy  call
	parentGoid int // from "created by ... in goroutine N"; -1 if not present

	synctestBubble int // -1 if not present
	labels         []label

	ancestors []ancestor

	stack []frame
}

type ancestor struct {
	goid      int
	frames    []frame
	createdBy call
}

type label struct {
	key, value string
}

// Goroutine is the exported handle to a parsed goroutine.
type Goroutine = goroutine

func (g *goroutine) ID() int              { return g.id }
func (g *goroutine) Status() string        { return g.status }
func (g *goroutine) Minutes() int          { return g.minutes }
func (g *goroutine) Locked() bool          { return g.locked }
func (g *goroutine) CreatedByName() string { return g.createdBy.name }

func (g *goroutine) HasFunc(pattern string) bool {
	for _, f := range g.stack {
		if strings.Contains(f.call.name, pattern) {
			return true
		}
	}
	return false
}

// IsRuntime reports whether the goroutine's stack is entirely
// runtime/stdlib/testing infrastructure. A goroutine is runtime if every
// frame is an infrastructure frame (by function prefix or _test.go file)
// and the created-by (if any) also matches.
func (g *goroutine) IsRuntime() bool {
	for _, f := range g.stack {
		if f.cgo || f.unavailable {
			continue
		}
		if !isInfraCall(f.call) {
			return false
		}
	}
	if g.createdBy.name != "" && !isInfraCall(g.createdBy) {
		return false
	}
	return true
}

var runtimePrefixes = []string{
	"runtime.",
	"runtime/",
	"internal/",
	"net.",
	"os/signal.",
	"syscall.",
	"os.",
	"testing.",
	"testing/",
	"sync.",
	"time.",
	"io.",
}

func isRuntimeFunc(name string) bool {
	for _, pfx := range runtimePrefixes {
		if strings.HasPrefix(name, pfx) {
			return true
		}
	}
	return false
}

func isInfraCall(c call) bool {
	if isRuntimeFunc(c.name) {
		return true
	}
	return strings.HasSuffix(c.file, "_test.go") || strings.HasSuffix(c.file, "_testmain.go")
}

// firstAppFrame returns the first non-runtime frame in a goroutine's stack,
// or the top frame if all frames are runtime.
func firstAppFrame(g *goroutine) *frame {
	for i := range g.stack {
		f := &g.stack[i]
		if f.cgo || f.unavailable {
			continue
		}
		if !isRuntimeFunc(f.call.name) {
			return f
		}
	}
	return &g.stack[0]
}

func frameDisplayName(f *frame) string {
	if f.cgo {
		return "non-Go function"
	}
	if f.unavailable {
		return "<unavailable>"
	}
	return f.call.name
}

func (g *goroutine) base() *frame {
	return &g.stack[0]
}

func (g *goroutine) ends() ends {
	return ends{g.base().call, g.createdBy}
}

type ends struct {
	top, bottom call
}

type Grouped struct {
	groups [][]*goroutine
}

type Dump struct {
	gs []*goroutine
}

func NewDump(gs []*goroutine) *Dump {
	return &Dump{gs: gs}
}

func (d *Dump) Goroutines() []*goroutine {
	return d.gs
}

func (d *Dump) Coalesce() *Grouped {
	dups := make(map[string][]*goroutine, 100)

	for _, g := range d.gs {
		key := fmt.Sprintf("%v", g.ends())
		dups[key] = append(dups[key], g)
	}

	groups := make([][]*goroutine, 0, len(dups))
	for _, gs := range dups {
		groups = append(groups, gs)
	}

	sortGroups(groups)
	return &Grouped{groups: groups}
}

func sortGroups(groups [][]*goroutine) {
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i]) != len(groups[j]) {
			return len(groups[i]) > len(groups[j])
		}
		return groups[i][0].id < groups[j][0].id
	})
}

func (g *Grouped) Top(n int) *Grouped {
	if n >= len(g.groups) {
		return g
	}
	return &Grouped{groups: g.groups[:n]}
}

func (g *Grouped) FilterMinGroupSize(n int) *Grouped {
	var filtered [][]*goroutine
	for _, group := range g.groups {
		if len(group) >= n {
			filtered = append(filtered, group)
		}
	}
	return &Grouped{groups: filtered}
}

func (g *Grouped) ok() bool {
	return len(g.groups) > 0
}

func (g *Grouped) WriteSummary(w io.Writer) {
	if !g.ok() {
		fmt.Fprintf(w, "no groups remaining\n")
		return
	}
	ngs := 0
	for _, group := range g.groups {
		ngs += len(group)
	}
	fmt.Fprintf(w, "[%d groups, %d goroutines]\n", len(g.groups), ngs)
}

// WriteOpt controls output options for Write and WriteShort.
type WriteOpt struct {
	ShowIDs  bool // show goroutine IDs in group headers
	PerGroup int  // show N example goroutines per group (0 = default single representative)
}

func (f *frame) writeTo(w io.Writer) {
	if f.cgo {
		fmt.Fprintln(w, "non-Go function")
		if f.call.file != "" {
			fmt.Fprintf(w, "\t%s:%d\n", f.call.file, f.call.line)
		}
		return
	}
	if f.unavailable {
		fmt.Fprintf(w, "%s\n\t<unavailable>\n", f.call.name)
	} else {
		fmt.Fprintf(w, "%s\n\t%s:%d\n", f.call.name, f.call.file, f.call.line)
	}
}

func writeGroupHeader(w io.Writer, group []*goroutine, showIDs bool) {
	minmin := 1 << 62
	maxmin := 0
	for _, g := range group {
		if g.minutes > maxmin {
			maxmin = g.minutes
		}
		if g.minutes < minmin {
			minmin = g.minutes
		}
	}

	repr := group[0]
	fmt.Fprintf(w, "--- %d goroutine(s) [%s]", len(group), repr.status)
	if minmin == maxmin {
		if minmin > 0 {
			fmt.Fprintf(w, " [%d min]", minmin)
		}
	} else {
		fmt.Fprintf(w, " [%d-%d min]", minmin, maxmin)
	}
	if showIDs {
		ids := make([]int, len(group))
		for i, g := range group {
			ids[i] = g.id
		}
		sort.Ints(ids)
		fmt.Fprint(w, " [goroutines")
		max := 10
		if len(ids) < max {
			max = len(ids)
		}
		for i := 0; i < max; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, " %d", ids[i])
		}
		if len(ids) > max {
			fmt.Fprintf(w, ", ... +%d more", len(ids)-max)
		}
		fmt.Fprint(w, "]")
	}
	fmt.Fprintln(w)
}

func writeGoroutineStack(w io.Writer, g *goroutine) {
	for _, f := range g.stack {
		f.writeTo(w)
	}
	if g.framesElided {
		if g.elidedCount > 0 {
			fmt.Fprintf(w, "...%d frames elided...\n", g.elidedCount)
		} else {
			fmt.Fprintln(w, "...additional frames elided...")
		}
	}
	if g.createdBy.name != "" {
		fmt.Fprintf(w, "created by %s\n\t%s:%d\n", g.createdBy.name, g.createdBy.file, g.createdBy.line)
	}
}

// pickExamples selects N representative goroutines from a group: first by ID,
// last by ID, then evenly spaced from the middle. The group is sorted by ID
// before selection so "first" and "last" correspond to oldest and newest.
func pickExamples(group []*goroutine, n int) []*goroutine {
	if n >= len(group) {
		return group
	}
	sorted := make([]*goroutine, len(group))
	copy(sorted, group)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].id < sorted[j].id
	})
	if n == 1 {
		return sorted[:1]
	}
	// First and last, then fill from middle.
	picked := make([]*goroutine, 0, n)
	picked = append(picked, sorted[0])
	picked = append(picked, sorted[len(sorted)-1])
	for len(picked) < n {
		// Evenly space from the remaining interior.
		step := float64(len(sorted)-1) / float64(n-1)
		for i := 1; i < n-1; i++ {
			idx := int(float64(i)*step + 0.5)
			picked = append(picked, sorted[idx])
			if len(picked) == n {
				break
			}
		}
		break
	}
	return picked
}

func (g *Grouped) Write(w io.Writer, opt WriteOpt) {
	if !g.ok() {
		fmt.Fprintf(w, "no groups remaining\n")
		return
	}

	for i, group := range g.groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeGroupHeader(w, group, opt.ShowIDs)

		if opt.PerGroup <= 0 {
			writeGoroutineStack(w, group[0])
			continue
		}

		examples := pickExamples(group, opt.PerGroup)
		for j, gr := range examples {
			if j > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "  goroutine %d:\n", gr.id)
			writeGoroutineStack(w, gr)
		}
	}
}

// WriteShort writes compact output: for each group, only the first non-runtime
// frame and the created-by line.
func (g *Grouped) WriteShort(w io.Writer, opt WriteOpt) {
	if !g.ok() {
		fmt.Fprintf(w, "no groups remaining\n")
		return
	}

	for i, group := range g.groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeGroupHeader(w, group, opt.ShowIDs)

		repr := group[0]
		f := firstAppFrame(repr)
		f.writeTo(w)
		if repr.createdBy.name != "" {
			fmt.Fprintf(w, "created by %s\n\t%s:%d\n", repr.createdBy.name, repr.createdBy.file, repr.createdBy.line)
		}
	}
}

// WriteTriage writes stats-only output for initial triage.
func WriteTriage(w io.Writer, grouped *Grouped) {
	if !grouped.ok() {
		fmt.Fprintf(w, "no groups remaining\n")
		return
	}

	total := 0
	statusCounts := make(map[string]int)
	var longestWaiter *goroutine
	for _, group := range grouped.groups {
		for _, g := range group {
			total++
			statusCounts[g.status]++
			if g.minutes > 0 && (longestWaiter == nil || g.minutes > longestWaiter.minutes) {
				longestWaiter = g
			}
		}
	}

	fmt.Fprintf(w, "Total: %d goroutines\n\n", total)

	// Status counts, sorted by count descending.
	type sc struct {
		status string
		count  int
	}
	scs := make([]sc, 0, len(statusCounts))
	for s, c := range statusCounts {
		scs = append(scs, sc{s, c})
	}
	sort.Slice(scs, func(i, j int) bool {
		if scs[i].count != scs[j].count {
			return scs[i].count > scs[j].count
		}
		return scs[i].status < scs[j].status
	})
	fmt.Fprintln(w, "Status:")
	for _, s := range scs {
		fmt.Fprintf(w, "  %d  %s\n", s.count, s.status)
	}

	// Top 5 groups by size.
	n := 5
	if len(grouped.groups) < n {
		n = len(grouped.groups)
	}
	fmt.Fprintf(w, "\nTop %d groups:\n", n)
	for _, group := range grouped.groups[:n] {
		repr := group[0]
		f := firstAppFrame(repr)
		name := frameDisplayName(f)
		if repr.createdBy.name != "" {
			fmt.Fprintf(w, "  %d  %s <- %s\n", len(group), name, repr.createdBy.name)
		} else {
			fmt.Fprintf(w, "  %d  %s\n", len(group), name)
		}
	}

	if longestWaiter != nil {
		f := firstAppFrame(longestWaiter)
		name := frameDisplayName(f)
		fmt.Fprintf(w, "\nLongest waiter: %d min, goroutine %d at %s\n", longestWaiter.minutes, longestWaiter.id, name)
	}
}
