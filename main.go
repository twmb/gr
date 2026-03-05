package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/twmb/gr/g"
)

func die(why string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, why+"\n", args...)
	os.Exit(1)
}

func main() {
	var (
		file       = flag.String("f", "", "input file (alternative to positional arg)")
		noRuntime  = flag.Bool("no-runtime", false, "hide runtime/stdlib goroutines (GC, scavenger, finalizer, signal)")
		status     = flag.String("status", "", "keep goroutines whose status contains pattern")
		funcPat    = flag.String("func", "", "keep goroutines with a matching function in their stack")
		createdBy  = flag.String("created-by", "", "keep goroutines whose created-by matches pattern")
		minMinutes = flag.Int("min-minutes", 0, "keep goroutines blocked >= N minutes")
		maxMinutes = flag.Int("max-minutes", -1, "keep goroutines blocked <= N minutes")
		invert     = flag.Bool("v", false, "invert pattern filters: exclude matching goroutines")

		short    = flag.Bool("short", false, "compact: show only first app frame + created-by per group")
		list     = flag.Bool("list", false, "one line per group: count + first app frame + created-by")
		summary  = flag.Bool("summary", false, "stats only: status counts, top groups, longest waiters")
		showIDs  = flag.Bool("show-ids", false, "show goroutine IDs in group headers")
		perGroup = flag.Int("examples", 0, "show N example goroutines per group with full stacks")

		groupBy      = flag.String("group-by", "", "grouping strategy: \"app\" groups by first app frame (default: top frame)")
		top          = flag.Int("top", 0, "show only top N groups")
		minGroupSize = flag.Int("min-group-size", 0, "hide groups with fewer than N goroutines")
	)

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `gr - goroutine stack dump analyzer

Usage: gr [flags] [file]

Parses Go goroutine stack dumps and groups goroutines by top frame + created-by.
Input from file (positional or -f) or stdin.

Filtering:
  -no-runtime          hide runtime/stdlib goroutines (GC, scavenger, finalizer, signal)
  -status <pattern>    keep goroutines whose status contains pattern
  -func <pattern>      keep goroutines with a matching function in their stack
  -created-by <pat>    keep goroutines whose created-by matches pattern
                       all three support | for OR: -func "foo|bar"
  -min-minutes <N>     keep goroutines blocked >= N minutes
  -max-minutes <N>     keep goroutines blocked <= N minutes
  -v                   invert pattern filters: exclude matching goroutines

Output:
  -short               compact: show only first app frame + created-by per group
  -list                one line per group: count + first app frame + created-by
  -summary             stats only: status counts, top groups, longest waiters
  -show-ids            show goroutine IDs in group headers
  -examples <N>        show N example goroutines per group with full stacks

Grouping:
  -group-by <strategy>  "app" groups by first app frame (default: top frame)

Limiting:
  -top <N>             show only top N groups
  -min-group-size <N>  hide groups with fewer than N goroutines

Examples:
  gr dump.txt                              grouped overview
  gr -no-runtime dump.txt                  skip runtime noise
  gr -no-runtime -short dump.txt           compact app-only view
  gr -summary dump.txt                     quick triage stats
  gr -list dump.txt                         one-line-per-group overview
  gr -func mypackage dump.txt              find goroutines in mypackage
  gr -func "setupAssigned|heartbeat"       find related goroutine pairs
  gr -status "chan receive" dump.txt        find blocked channel receivers
  gr -min-minutes 5 dump.txt               find long-blocked goroutines
  gr -v -func updateMetadataLoop dump.txt  hide known-idle goroutines
  gr -group-by app dump.txt                group by app code, not syscall
  gr -show-ids -func mypackage dump.txt    show IDs for log correlation
  gr -examples 2 dump.txt                  compare goroutines within groups
`)
	}
	flag.Parse()

	// Determine input source.
	var input *os.File
	switch {
	case *file != "":
		f, err := os.Open(*file)
		if err != nil {
			die("unable to open %s: %v", *file, err)
		}
		defer f.Close()
		input = f
	case flag.NArg() == 1:
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			die("unable to open %s: %v", flag.Arg(0), err)
		}
		defer f.Close()
		input = f
	case flag.NArg() == 0:
		input = os.Stdin
	default:
		flag.Usage()
		os.Exit(1)
	}

	dump, err := g.Parse(input)
	if err != nil {
		die("parse error: %v", err)
	}

	// Split pattern flags on | for OR matching.
	var funcPatterns, statusPatterns, createdByPatterns []string
	if *funcPat != "" {
		funcPatterns = strings.Split(*funcPat, "|")
	}
	if *status != "" {
		statusPatterns = strings.Split(*status, "|")
	}
	if *createdBy != "" {
		createdByPatterns = strings.Split(*createdBy, "|")
	}

	// Apply filters.
	all := dump.Goroutines()
	filtered := make([]*g.Goroutine, 0, len(all))
	for _, gr := range all {
		// Pre-filters: -no-runtime and minutes are not affected by -v.
		if *noRuntime && gr.IsRuntime() {
			continue
		}
		if *minMinutes > 0 && gr.Minutes() < *minMinutes {
			continue
		}
		if *maxMinutes >= 0 && gr.Minutes() > *maxMinutes {
			continue
		}

		// Pattern filters: -status, -func, -created-by are inverted by -v.
		match := true
		if match && len(statusPatterns) > 0 {
			found := false
			for _, p := range statusPatterns {
				if strings.Contains(gr.Status(), p) {
					found = true
					break
				}
			}
			if !found {
				match = false
			}
		}
		if match && len(funcPatterns) > 0 {
			found := false
			for _, p := range funcPatterns {
				if gr.HasFunc(p) {
					found = true
					break
				}
			}
			if !found {
				match = false
			}
		}
		if match && len(createdByPatterns) > 0 {
			found := false
			for _, p := range createdByPatterns {
				if strings.Contains(gr.CreatedByName(), p) {
					found = true
					break
				}
			}
			if !found {
				match = false
			}
		}
		if *invert {
			match = !match
		}
		if !match {
			continue
		}
		filtered = append(filtered, gr)
	}

	if len(filtered) == 0 {
		die("no goroutines match filters")
	}

	// Group goroutines.
	d := g.NewDump(filtered)
	var grouped *g.Grouped
	switch *groupBy {
	case "", "top":
		grouped = d.Coalesce()
	case "app":
		grouped = d.CoalesceByApp()
	default:
		die("unknown -group-by value: %s (use \"app\" or \"top\")", *groupBy)
	}

	// Filter by group size.
	if *minGroupSize > 0 {
		grouped = grouped.FilterMinGroupSize(*minGroupSize)
	}

	// Top N.
	if *top > 0 {
		grouped = grouped.Top(*top)
	}

	opt := g.WriteOpt{
		ShowIDs:  *showIDs,
		PerGroup: *perGroup,
	}

	// Output.
	switch {
	case *summary:
		g.WriteTriage(os.Stdout, grouped)
	case *list:
		grouped.WriteSummary(os.Stdout)
		fmt.Println()
		grouped.WriteList(os.Stdout)
	case *short:
		grouped.WriteSummary(os.Stdout)
		fmt.Println()
		grouped.WriteShort(os.Stdout, opt)
	default:
		grouped.WriteSummary(os.Stdout)
		fmt.Println()
		grouped.Write(os.Stdout, opt)
	}
}
