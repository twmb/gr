package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	prompt "github.com/c-bata/go-prompt"
	"github.com/twmb/gr/g"
)

// TODO: read from pipe

var strict = flag.Bool("s", false, "coalesce goroutines strictly")

func die(why string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, why, args...)
	os.Exit(1)
}

func usage() {
	fmt.Printf(`usage: gr (-s) [stack dump]

`)
	os.Exit(0)
}

func main() {
	flag.Parse()

	if len(flag.Args()) != 1 {
		usage()
	}

	fname := flag.Args()[0]
	dump, err := os.Open(fname)
	if err != nil {
		die("unable to open %s: %v", fname, err)
	}
	defer dump.Close()

	buf := bufio.NewReaderSize(dump, 64<<10)

	stack, err := g.Parse(buf, g.ParseCorruptFatally)
	_ = stack
	if err != nil {
		die("parse stack err: %v", err)
	}

	r := &repl{
		state: stateGrouped,
		stack: stack,
	}

	if *strict {
		r.base = r.stack.Coalesce(true)
	} else {
		r.base = r.stack.Coalesce(false)
	}
	r.on = r.base
	r.on.WriteSummary(os.Stdout)

	p := prompt.New(
		r.executor,
		r.completer,
		prompt.OptionPrefix("> "),
	)
	p.Run()
}

type repl struct {
	state replState
	stack *g.Dump
	base  *g.Grouped

	on *g.Grouped
}

type replState int

const (
	stateGrouped replState = iota
)

func (r *repl) executor(in string) {
	in = strings.TrimSpace(in)

	if len(in) == 0 {
		return
	}

	p := func(full, pfx string) bool { return strings.HasPrefix(full, pfx) }
	num := func(in string) (int, error) {
		in = strings.TrimSpace(in)
		return strconv.Atoi(in)
	}

	switch r.state {
	case stateGrouped:
		switch {
		case in == "r":
			r.on = r.base
			fmt.Println("grouping reset")

		case in == "p":
			r.on.Write(os.Stdout, true)

		case in == "pf":
			r.on.Write(os.Stdout, false)

		case p(in, "fmu"): // must be before f
			n, err := num(in[3:])
			if err != nil {
				fmt.Println("unable to parse filter number")
				return
			}
			r.on = r.on.FilterMinutesUnder(n)
			r.on.WriteSummary(os.Stdout)

		case p(in, "t"):
			n, err := num(in[1:])
			if err != nil {
				fmt.Println("unable to parse top number")
				return
			}
			r.on = r.on.Top(n)
			r.on.WriteSummary(os.Stdout)

		case p(in, "dgs"): // must be before dg
			n, err := num(in[3:])
			if err != nil {
				fmt.Println("unable to parse drop number")
				return
			}
			r.on = r.on.DropGroupsBySize(n)
			r.on.WriteSummary(os.Stdout)

		case p(in, "dg"):
			n, err := num(in[2:])
			if err != nil {
				fmt.Println("unable to parse drop number")
				return
			}
			r.on = r.on.DropGroup(n - 1)
			r.on.WriteSummary(os.Stdout)

		case p(in, "g"):
			n, err := num(in[1:])
			if err != nil {
				fmt.Println("unable to parse group number")
				return
			}
			r.on = r.on.GetGroup(n - 1)
			r.on.WriteSummary(os.Stdout)

		default:
			fmt.Printf("unrecognized command %q\n", in)
		}
	}
}

func (r *repl) completer(in prompt.Document) []prompt.Suggest {
	var s []prompt.Suggest
	switch r.state {
	case stateGrouped:
		s = []prompt.Suggest{
			{Text: "r", Description: "clear filters if present, or clear coalescing"},
			{Text: "p", Description: "print groups"},
			{Text: "pf", Description: "print groups with all frames"},
			{Text: "t", Description: "top <#> groups"},
			{Text: "fmu", Description: "filter goroutines hung for less than <#> minutes"},
			{Text: "dgs", Description: "drop groups smaller than <#>"},
			{Text: "dg", Description: "drop group <#>; 1-indexed"},
			{Text: "g", Description: "keep only group <#>; 1-indexed"},
			// fg: focus group
		}
	}

	return prompt.FilterHasPrefix(s, in.GetWordBeforeCursor(), true)
}
