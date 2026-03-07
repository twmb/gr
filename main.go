package main

import (
	"fmt"
	"os"
)

func die(why string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, why+"\n", args...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "goroutines", "g":
		goroutinesCmd(args)
	case "cover", "c":
		coverCmd(args)
	case "-h", "-help", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gr - Go diagnostic tool

Usage: gr <command> [flags] [args]

Commands:
  goroutines, g    analyze goroutine stack dumps
  cover, c         analyze code coverage profiles

Run "gr <command> -help" for command-specific help.
`)
}
