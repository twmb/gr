package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/twmb/gr/goroutine"
)

func goroutinesCmd(args []string) {
	flags := flag.NewFlagSet("goroutines", flag.ExitOnError)

	var (
		file       = flags.String("f", "", "input file (alternative to positional arg)")
		noRuntime  = flags.Bool("no-runtime", false, "hide runtime/stdlib goroutines (GC, scavenger, finalizer, signal)")
		status     = flags.String("status", "", "keep goroutines whose status contains pattern")
		funcPat    = flags.String("func", "", "keep goroutines with a matching function in their stack")
		createdBy  = flags.String("created-by", "", "keep goroutines whose created-by matches pattern")
		minMinutes = flags.Int("min-minutes", 0, "keep goroutines blocked >= N minutes")
		maxMinutes = flags.Int("max-minutes", -1, "keep goroutines blocked <= N minutes")
		invert     = flags.Bool("v", false, "invert pattern filters: exclude matching goroutines")

		short    = flags.Bool("short", false, "compact: show only first app frame + created-by per group")
		list     = flags.Bool("list", false, "one line per group: count + first app frame + created-by")
		summary  = flags.Bool("summary", false, "stats only: status counts, top groups, longest waiters")
		terse    = flags.Bool("terse", false, "single-line-per-frame output with short paths (for LLM/piping)")
		showIDs  = flags.Bool("show-ids", false, "show goroutine IDs in group headers")
		perGroup = flags.Int("examples", 0, "show N example goroutines per group with full stacks")

		groupBy      = flags.String("group-by", "", `grouping strategy: "app" groups by first app frame (default: top frame)`)
		top          = flags.Int("top", 0, "show only top N groups")
		minGroupSize = flags.Int("min-group-size", 0, "hide groups with fewer than N goroutines")
	)

	flags.Usage = func() {
		fmt.Fprint(os.Stderr, `gr goroutines - goroutine stack dump analyzer

Usage: gr goroutines [flags] [file]

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
  -terse               single-line-per-frame, short paths (for LLM/piping)
  -show-ids            show goroutine IDs in group headers
  -examples <N>        show N example goroutines per group with full stacks

Grouping:
  -group-by <strategy>  "app" groups by first app frame (default: top frame)

Limiting:
  -top <N>             show only top N groups
  -min-group-size <N>  hide groups with fewer than N goroutines

Examples:
  gr g dump.txt                              grouped overview
  gr g -no-runtime dump.txt                  skip runtime noise
  gr g -no-runtime -short dump.txt           compact app-only view
  gr g -summary dump.txt                     quick triage stats
  gr g -list dump.txt                        one-line-per-group overview
  gr g -func mypackage dump.txt              find goroutines in mypackage
  gr g -func "setupAssigned|heartbeat"       find related goroutine pairs
  gr g -status "chan receive" dump.txt        find blocked channel receivers
  gr g -min-minutes 5 dump.txt               find long-blocked goroutines
  gr g -v -func updateMetadataLoop dump.txt  hide known-idle goroutines
  gr g -group-by app dump.txt                group by app code, not syscall
  gr g -show-ids -func mypackage dump.txt    show IDs for log correlation
  gr g -examples 2 dump.txt                  compare goroutines within groups
`)
	}
	flags.Parse(args)

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

	dump, err := goroutine.Parse(input)
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
	filtered := make([]*goroutine.Goroutine, 0, len(all))
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
	d := goroutine.NewDump(filtered)
	var grouped *goroutine.Grouped
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

	opt := goroutine.WriteOpt{
		ShowIDs:  *showIDs,
		PerGroup: *perGroup,
		Terse:    *terse,
	}

	// Output.
	switch {
	case *summary:
		goroutine.WriteTriage(os.Stdout, grouped)
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
