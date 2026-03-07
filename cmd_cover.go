package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/twmb/gr/cover"
)

func coverCmd(args []string) {
	flags := flag.NewFlagSet("cover", flag.ExitOnError)

	var (
		dir       = flags.String("dir", "", "project directory for resolving source files")
		uncovered = flags.Bool("uncovered", false, "show uncovered line ranges instead of function summary")
		pkg       = flags.Bool("pkg", false, "aggregate coverage by package instead of per-function")
		top       = flags.Int("top", 0, "show only top N results")
		funcPat   = flags.String("func", "", "filter to functions/files matching pattern")
		minStmts  = flags.Int("min-statements", 0, "hide functions/blocks with fewer than N statements")
		sortBy    = flags.String("sort", "", `sort order: "stmts" sorts by uncovered statement count descending`)
		no100     = flags.Bool("no-100", false, "hide functions with 100% coverage")
		diff      = flags.Bool("diff", false, "compare two coverage profiles: gr c -diff old.out new.out")
	)

	flags.Usage = func() {
		fmt.Fprint(os.Stderr, `gr cover - coverage profile analyzer

Usage: gr cover [flags] [coverprofile]
       gr cover -diff [flags] old.out new.out

Analyzes Go coverage profiles produced by "go test -coverprofile=FILE".

Default output shows per-function coverage sorted by percentage (ascending).

Flags:
  -dir <path>            project directory for resolving source files
  -func <pattern>        filter to functions/files matching pattern
  -top <N>               show only top N results
  -uncovered             show uncovered line ranges instead of function summary
  -pkg                   aggregate coverage by package
  -min-statements <N>    hide functions/blocks with fewer than N statements
  -sort stmts            sort by uncovered statement count (descending)
  -no-100                hide fully covered functions/packages
  -diff                  compare two profiles (old.out new.out)

Examples:
  gr c coverage.out                       functions sorted by coverage (ascending)
  gr c -pkg coverage.out                  package-level summary
  gr c -uncovered coverage.out            show uncovered line ranges
  gr c -top 10 coverage.out               10 least covered functions
  gr c -func mypackage coverage.out       filter to matching functions/files
  gr c -sort stmts -min-statements 5      biggest coverage gaps first
  gr c -diff old.out new.out              compare coverage before/after
  gr c -diff -func pkg old.out new.out    compare, filtered to pkg
`)
	}
	flags.Parse(args)

	fo := filterOpts{
		pattern:     *funcPat,
		topN:        *top,
		minStmts:    *minStmts,
		sortByStmts: *sortBy == "stmts",
		no100:       *no100,
	}

	if *diff {
		if flags.NArg() != 2 {
			die("-diff requires exactly two positional args: old.out new.out")
		}
		oldResult := loadProfile(flags.Arg(0), *dir)
		newResult := loadProfile(flags.Arg(1), *dir)
		writeDiff(oldResult, newResult, fo)
		return
	}

	var input *os.File
	switch {
	case flags.NArg() == 1:
		f, err := os.Open(flags.Arg(0))
		if err != nil {
			die("unable to open %s: %v", flags.Arg(0), err)
		}
		defer f.Close()
		input = f
	case flags.NArg() == 0:
		input = os.Stdin
	default:
		flags.Usage()
		os.Exit(1)
	}

	profile, err := cover.ParseProfile(input)
	if err != nil {
		die("parse error: %v", err)
	}

	result, err := cover.Analyze(profile, *dir)
	if err != nil {
		die("analysis error: %v", err)
	}

	if *uncovered {
		writeUncovered(result, fo)
	} else if *pkg {
		writePkgCoverage(result, fo)
	} else {
		writeFuncCoverage(result, fo)
	}
}

func loadProfile(path, dir string) *cover.Result {
	f, err := os.Open(path)
	if err != nil {
		die("unable to open %s: %v", path, err)
	}
	defer f.Close()
	profile, err := cover.ParseProfile(f)
	if err != nil {
		die("parse error in %s: %v", path, err)
	}
	result, err := cover.Analyze(profile, dir)
	if err != nil {
		die("analysis error in %s: %v", path, err)
	}
	return result
}

type filterOpts struct {
	pattern     string
	topN        int
	minStmts    int
	sortByStmts bool
	no100       bool
}

func writeFuncCoverage(r *cover.Result, o filterOpts) {
	funcs := make([]cover.FuncCoverage, 0, len(r.Funcs))
	for _, f := range r.Funcs {
		if o.pattern != "" && !strings.Contains(f.Func, o.pattern) && !strings.Contains(f.File, o.pattern) {
			continue
		}
		if o.minStmts > 0 && f.Statements < o.minStmts {
			continue
		}
		if o.no100 && f.Percent() >= 100 {
			continue
		}
		funcs = append(funcs, f)
	}

	if o.sortByStmts {
		sort.Slice(funcs, func(i, j int) bool {
			ui := funcs[i].Statements - funcs[i].Covered
			uj := funcs[j].Statements - funcs[j].Covered
			if ui != uj {
				return ui > uj
			}
			return funcs[i].File < funcs[j].File
		})
	} else {
		sort.Slice(funcs, func(i, j int) bool {
			pi, pj := funcs[i].Percent(), funcs[j].Percent()
			if pi != pj {
				return pi < pj
			}
			if funcs[i].File != funcs[j].File {
				return funcs[i].File < funcs[j].File
			}
			return funcs[i].StartLine < funcs[j].StartLine
		})
	}

	if o.topN > 0 && o.topN < len(funcs) {
		funcs = funcs[:o.topN]
	}

	if len(funcs) == 0 {
		fmt.Println("no functions match filters")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, f := range funcs {
		fmt.Fprintf(w, "%s\t%s\t%.1f%%\t(%d/%d)\n",
			cover.ShortFile(f.File, r.ModPath), f.Func, f.Percent(), f.Covered, f.Statements)
	}
	w.Flush()

	if r.TotalStmt > 0 {
		pct := float64(r.CoveredStmt) / float64(r.TotalStmt) * 100
		fmt.Printf("\ntotal: %.1f%% (%d/%d statements)\n", pct, r.CoveredStmt, r.TotalStmt)
	}
}

type pkgCoverage struct {
	pkg        string
	statements int
	covered    int
}

func (p *pkgCoverage) percent() float64 {
	if p.statements == 0 {
		return 100.0
	}
	return float64(p.covered) / float64(p.statements) * 100.0
}

func writePkgCoverage(r *cover.Result, o filterOpts) {
	pkgMap := make(map[string]*pkgCoverage)
	for _, f := range r.Funcs {
		if o.pattern != "" && !strings.Contains(f.Func, o.pattern) && !strings.Contains(f.File, o.pattern) {
			continue
		}
		pkg := pkgOf(f.File, r.ModPath)
		pc := pkgMap[pkg]
		if pc == nil {
			pc = &pkgCoverage{pkg: pkg}
			pkgMap[pkg] = pc
		}
		pc.statements += f.Statements
		pc.covered += f.Covered
	}

	pkgs := make([]*pkgCoverage, 0, len(pkgMap))
	for _, pc := range pkgMap {
		if o.minStmts > 0 && pc.statements < o.minStmts {
			continue
		}
		if o.no100 && pc.percent() >= 100 {
			continue
		}
		pkgs = append(pkgs, pc)
	}

	if o.sortByStmts {
		sort.Slice(pkgs, func(i, j int) bool {
			ui := pkgs[i].statements - pkgs[i].covered
			uj := pkgs[j].statements - pkgs[j].covered
			if ui != uj {
				return ui > uj
			}
			return pkgs[i].pkg < pkgs[j].pkg
		})
	} else {
		sort.Slice(pkgs, func(i, j int) bool {
			pi, pj := pkgs[i].percent(), pkgs[j].percent()
			if pi != pj {
				return pi < pj
			}
			return pkgs[i].pkg < pkgs[j].pkg
		})
	}

	if o.topN > 0 && o.topN < len(pkgs) {
		pkgs = pkgs[:o.topN]
	}

	if len(pkgs) == 0 {
		fmt.Println("no packages match filters")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, pc := range pkgs {
		fmt.Fprintf(w, "%s\t%.1f%%\t(%d/%d)\n", pc.pkg, pc.percent(), pc.covered, pc.statements)
	}
	w.Flush()

	if r.TotalStmt > 0 {
		pct := float64(r.CoveredStmt) / float64(r.TotalStmt) * 100
		fmt.Printf("\ntotal: %.1f%% (%d/%d statements)\n", pct, r.CoveredStmt, r.TotalStmt)
	}
}

func pkgOf(file, modPath string) string {
	short := cover.ShortFile(file, modPath)
	if i := strings.LastIndexByte(short, '/'); i >= 0 {
		return short[:i]
	}
	return "."
}

type funcDiff struct {
	file                         string
	funcName                     string
	oldPct, newPct               float64
	oldCovered, oldStmts         int
	newCovered, newStmts         int
	oldUncov, newUncov           int
	deltaPct                     float64
}

func writeDiff(oldR, newR *cover.Result, o filterOpts) {
	type funcKey struct{ file, fn string }

	oldMap := make(map[funcKey]cover.FuncCoverage)
	for _, f := range oldR.Funcs {
		oldMap[funcKey{f.File, f.Func}] = f
	}

	seen := make(map[funcKey]bool)
	var diffs []funcDiff
	for _, nf := range newR.Funcs {
		k := funcKey{nf.File, nf.Func}
		seen[k] = true
		of, existed := oldMap[k]
		oldPct := float64(0)
		if existed {
			oldPct = of.Percent()
		}
		d := funcDiff{
			file:       nf.File,
			funcName:   nf.Func,
			oldPct:     oldPct,
			newPct:     nf.Percent(),
			oldCovered: of.Covered, oldStmts: of.Statements,
			newCovered: nf.Covered, newStmts: nf.Statements,
		}
		d.oldUncov = d.oldStmts - d.oldCovered
		d.newUncov = d.newStmts - d.newCovered
		d.deltaPct = d.newPct - d.oldPct
		diffs = append(diffs, d)
	}
	// Functions removed in new profile.
	for _, of := range oldR.Funcs {
		k := funcKey{of.File, of.Func}
		if seen[k] {
			continue
		}
		d := funcDiff{
			file:       of.File,
			funcName:   of.Func,
			oldPct:     of.Percent(),
			newPct:     -1, // sentinel for "removed"
			oldCovered: of.Covered, oldStmts: of.Statements,
		}
		d.oldUncov = d.oldStmts - d.oldCovered
		d.deltaPct = -d.oldPct
		diffs = append(diffs, d)
	}

	// Filter.
	modPath := newR.ModPath
	if modPath == "" {
		modPath = oldR.ModPath
	}
	filtered := diffs[:0]
	for _, d := range diffs {
		if o.pattern != "" && !strings.Contains(d.funcName, o.pattern) && !strings.Contains(d.file, o.pattern) {
			continue
		}
		stmts := d.newStmts
		if stmts == 0 {
			stmts = d.oldStmts
		}
		if o.minStmts > 0 && stmts < o.minStmts {
			continue
		}
		if o.no100 && d.newPct >= 100 {
			continue
		}
		if d.oldStmts == 0 && d.newStmts == 0 {
			continue
		}
		// Skip unchanged.
		if d.oldCovered == d.newCovered && d.oldStmts == d.newStmts {
			continue
		}
		filtered = append(filtered, d)
	}
	diffs = filtered

	if o.sortByStmts {
		sort.Slice(diffs, func(i, j int) bool {
			di := diffs[i].oldUncov - diffs[i].newUncov
			dj := diffs[j].oldUncov - diffs[j].newUncov
			if di != dj {
				return di > dj // biggest uncov reduction first
			}
			return diffs[i].file < diffs[j].file
		})
	} else {
		sort.Slice(diffs, func(i, j int) bool {
			if diffs[i].deltaPct != diffs[j].deltaPct {
				return diffs[i].deltaPct > diffs[j].deltaPct
			}
			if diffs[i].file != diffs[j].file {
				return diffs[i].file < diffs[j].file
			}
			return diffs[i].funcName < diffs[j].funcName
		})
	}

	if o.topN > 0 && o.topN < len(diffs) {
		diffs = diffs[:o.topN]
	}

	if len(diffs) == 0 {
		fmt.Println("no coverage changes match filters")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, d := range diffs {
		file := cover.ShortFile(d.file, modPath)
		sign := "+"
		if d.deltaPct < 0 {
			sign = ""
		}
		if d.newPct < 0 {
			// removed function
			fmt.Fprintf(w, "%s\t%s\t%.1f%% → removed\t(%d/%d → 0/0)\n",
				file, d.funcName, d.oldPct, d.oldCovered, d.oldStmts)
		} else if d.oldStmts == 0 {
			// new function
			fmt.Fprintf(w, "%s\t%s\tnew → %.1f%%\t(0/0 → %d/%d)\n",
				file, d.funcName, d.newPct, d.newCovered, d.newStmts)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%.1f%% → %.1f%%\t%s%.1f%%\t(%d/%d → %d/%d)\n",
				file, d.funcName, d.oldPct, d.newPct, sign, d.deltaPct,
				d.oldCovered, d.oldStmts, d.newCovered, d.newStmts)
		}
	}
	w.Flush()

	// Total summary.
	if newR.TotalStmt > 0 || oldR.TotalStmt > 0 {
		oldPct := float64(0)
		if oldR.TotalStmt > 0 {
			oldPct = float64(oldR.CoveredStmt) / float64(oldR.TotalStmt) * 100
		}
		newPct := float64(0)
		if newR.TotalStmt > 0 {
			newPct = float64(newR.CoveredStmt) / float64(newR.TotalStmt) * 100
		}
		delta := newPct - oldPct
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		fmt.Printf("\ntotal: %.1f%% → %.1f%% (%s%.1f%%, %d/%d → %d/%d statements)\n",
			oldPct, newPct, sign, delta, oldR.CoveredStmt, oldR.TotalStmt, newR.CoveredStmt, newR.TotalStmt)
	}
}

func writeUncovered(r *cover.Result, o filterOpts) {
	blocks := make([]cover.UncoveredBlock, 0, len(r.Uncovered))
	for _, b := range r.Uncovered {
		if o.pattern != "" && !strings.Contains(b.File, o.pattern) {
			continue
		}
		if o.minStmts > 0 && b.NumStmt < o.minStmts {
			continue
		}
		blocks = append(blocks, b)
	}

	if o.sortByStmts {
		sort.Slice(blocks, func(i, j int) bool {
			if blocks[i].NumStmt != blocks[j].NumStmt {
				return blocks[i].NumStmt > blocks[j].NumStmt
			}
			if blocks[i].File != blocks[j].File {
				return blocks[i].File < blocks[j].File
			}
			return blocks[i].StartLine < blocks[j].StartLine
		})
	} else {
		sort.Slice(blocks, func(i, j int) bool {
			if blocks[i].File != blocks[j].File {
				return blocks[i].File < blocks[j].File
			}
			return blocks[i].StartLine < blocks[j].StartLine
		})
	}

	if o.topN > 0 && o.topN < len(blocks) {
		blocks = blocks[:o.topN]
	}

	if len(blocks) == 0 {
		fmt.Println("no uncovered blocks match filters")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, b := range blocks {
		file := cover.ShortFile(b.File, r.ModPath)
		if b.StartLine == b.EndLine {
			fmt.Fprintf(w, "%s:%d\t%d statements\n", file, b.StartLine, b.NumStmt)
		} else {
			fmt.Fprintf(w, "%s:%d-%d\t%d statements\n", file, b.StartLine, b.EndLine, b.NumStmt)
		}
	}
	w.Flush()
}
