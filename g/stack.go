package g

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type call struct {
	name string
	file string
	line int
}

type frame struct {
	call        call
	args        []int
	inlFunc     bool
	argsElided  bool
	unavailable bool
}

type goroutine struct {
	id      int
	status  string
	minutes int
	locked  bool

	framesElided bool

	createdBy call

	// all goroutines must have at least one frame
	stack []frame
}

func (g *goroutine) base() *frame {
	return &g.stack[0]
}

func (g *goroutine) roughEq(o *goroutine) bool {
	return g.ends() == o.ends()
}

func (g *goroutine) ends() ends {
	return ends{g.base().call, g.createdBy}
}

const unavail = "\t<unavailable"

func (g *goroutine) full() string {
	var b strings.Builder
	var grow int

	grow += len(g.status) + 1
	for _, frame := range g.stack {
		grow += len(frame.call.name) + len(frame.call.file) + 19 + 3 // line, \n\t:\n
		if frame.unavailable {
			grow += len(unavail)
		}
	}

	var numbuf [19]byte

	b.Grow(grow)
	b.WriteString(g.status)
	b.WriteByte('\n')
	for _, frame := range g.stack {
		b.WriteString(frame.call.name)
		b.WriteString("\n\t")
		if frame.unavailable {
			b.WriteString(unavail)
		} else {
			b.WriteString(frame.call.file)
			line := strconv.AppendInt(numbuf[:0], int64(frame.call.line), 10)
			b.WriteByte(':')
			b.Write(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

type ends struct {
	top, bottom call
}

type Grouped struct {
	groups [][]*goroutine
	exact  bool
}

type Dump struct {
	gs []*goroutine
}

func (d *Dump) Coalesce(exact bool) *Grouped {
	dups := make(map[interface{}][]*goroutine, 100)

	for _, g := range d.gs {
		if exact {
			full := g.full()
			dups[full] = append(dups[full], g)
		} else {
			dups[g.ends()] = append(dups[g.ends()], g)
		}
	}

	var lengths [][]*goroutine

	for _, gs := range dups {
		lengths = append(lengths, gs)
	}

	goroGroups(lengths).sort()

	return &Grouped{
		groups: lengths,
		exact:  exact,
	}
}

type goroGroups [][]*goroutine

func (g goroGroups) sort() {
	sort.Slice(g, func(i, j int) bool { return len(g[i]) > len(g[j]) })
}

func (g *Grouped) Top(n int) *Grouped {
	if n > len(g.groups) {
		return g
	}
	return &Grouped{
		groups: g.groups[:n],
		exact:  g.exact,
	}
}

func (g *Grouped) FilterMinutesUnder(lowerLim int) *Grouped {
	orig := *g
	for i, group := range g.groups {
		for j := 0; j < len(group); j++ {
			g := group[j]
			if g.minutes < lowerLim {
				group[j] = group[len(group)-1]
				group = group[:len(group)-1]
				j--
			}
		}
		g.groups[i] = group
	}
	for i := 0; i < len(g.groups); i++ {
		group := g.groups[i]
		if len(group) == 0 {
			g.groups[i] = g.groups[len(g.groups)-1]
			g.groups = g.groups[:len(g.groups)-1]
			i--
		}
	}
	goroGroups(g.groups).sort()

	r := *g
	*g = orig
	return &r
}

func (f *frame) writeTo(w io.Writer) {
	if f.unavailable {
		fmt.Fprintf(w, "%s\n\t%s\n", f.call.name, unavail)
	} else {
		fmt.Fprintf(w, "%s\n\t%s:%d\n", f.call.name, f.call.file, f.call.line)
	}
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

func (g *Grouped) DropGroup(n int) *Grouped {
	if n > len(g.groups) {
		return g
	}

	r := *g
	if n < len(g.groups)-1 {
		copy(r.groups[n:], r.groups[n+1:])
	}
	r.groups = r.groups[:len(r.groups)-1]
	goroGroups(r.groups).sort()
	return &r
}

func (g *Grouped) DropGroupsBySize(n int) *Grouped {
	r := *g
	for i := 0; i < len(r.groups); i++ {
		group := r.groups[i]
		if len(group) < n {
			r.groups[i] = r.groups[len(r.groups)-1]
			r.groups = r.groups[:len(r.groups)-1]
			i--
		}
	}
	goroGroups(r.groups).sort()
	return &r
}

func (g *Grouped) GetGroup(n int) *Grouped {
	if n > len(g.groups) {
		return &Grouped{}
	}

	return &Grouped{
		groups: [][]*goroutine{g.groups[n]},
		exact:  g.exact,
	}
}

func (g *Grouped) Write(w io.Writer, short bool) {
	if !g.ok() {
		fmt.Fprintf(w, "no groups remaining\n")
		return
	}

	for _, group := range g.groups {
		minmin := 1 << 62
		minmax := 0

		for _, g := range group {
			if g.minutes > minmax {
				minmax = g.minutes
			}
			if g.minutes < minmin {
				minmin = g.minutes
			}
		}

		g := group[0]

		fmt.Fprintf(w, "» %d [minutes min %d max %d]\n",
			len(group), minmin, minmax)
		if short {
			g.base().writeTo(w)
			if len(g.stack) == 2 {
				g.stack[1].writeTo(w)
			} else if len(g.stack) > 2 {
				fmt.Printf("› %d frames skipped\n", len(g.stack)-1)
			}
		} else {
			for _, frame := range g.stack {
				frame.writeTo(w)
			}

		}
		if g.createdBy.name != "" {
			fmt.Printf("%s\n\t%s:%d\n\n",
				g.createdBy.name, g.createdBy.file, g.createdBy.line,
			)
		} else {
			fmt.Println()
		}
		fmt.Println()
	}
}
