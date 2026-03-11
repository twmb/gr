# gr

`gr` is a Go diagnostic tool with two subcommands:

- **`goroutines`** (`g`) — parse and analyze goroutine stack dumps
- **`cover`** (`c`) — analyze code coverage profiles

## Index

- [Install](#install)
- [gr goroutines](#gr-goroutines)
  - [Filtering](#filtering)
  - [Output modes](#output-modes)
  - [Grouping](#grouping)
  - [Limiting](#limiting)
  - [Examples](#examples)
  - [What `-no-runtime` filters](#what--no-runtime-filters)
  - [How grouping works](#how-grouping-works)
  - [CI log prefix auto-detection](#ci-log-prefix-auto-detection)
- [gr cover](#gr-cover)
  - [Flags](#flags)
  - [Examples](#examples-1)
  - [Diff mode](#diff-mode)
  - [Output format](#output-format)

## Install

```
go install github.com/twmb/gr@latest
```

## gr goroutines

Parses Go goroutine stack dumps and groups goroutines by their top stack frame
and created-by function. Designed for fast triage of large dumps, especially
from test timeouts and production panics. Also useful for helping LLMs parse
goroutine dumps for debugging analysis.

```
gr goroutines [flags] [file]
gr g [flags] [file]
```

Input is read from a file (positional arg or `-f`) or stdin.

### Filtering

| Flag | Description |
|------|-------------|
| `-no-runtime` | Hide runtime, stdlib, and testing infrastructure goroutines |
| `-status <pat>` | Keep goroutines whose status contains pattern |
| `-func <pat>` | Keep goroutines with a matching function or file path in their stack (also matches created-by) |
| `-created-by <pat>` | Keep goroutines whose created-by matches pattern |
| `-min-minutes <N>` | Keep goroutines blocked >= N minutes |
| `-max-minutes <N>` | Keep goroutines blocked <= N minutes |
| `-v` | Invert pattern filters (like `grep -v`) |

`-status`, `-func`, and `-created-by` support `|` for OR matching:
`-func "setupAssigned|heartbeat"`.

`-v` inverts only the pattern filters (`-status`, `-func`, `-created-by`),
not `-no-runtime` or the minutes filters.

`-func` searches function names, file paths, and created-by fields. This
means `-func mypackage` matches goroutines that have `mypackage` anywhere in
their stack — whether in a function name like `mypackage.Run`, a file path
like `/pkg/mypackage/server.go`, or a created-by like `mypackage.NewServer`.

### Output modes

| Flag | Description |
|------|-------------|
| *(default)* | Full grouped output: group header + representative stack per group |
| `-short` | Compact: first application frame + created-by per group |
| `-list` | One line per group: count + first application frame + created-by |
| `-summary` | Stats only: status counts, top 5 groups, longest waiters |
| `-terse` | Single-line-per-frame output with short paths (for LLM/piping) |
| `-show-ids` | Add goroutine IDs to group headers (for log correlation) |
| `-examples <N>` | Show N goroutines per group (picks first and last by ID) |

### Grouping

| Flag | Description |
|------|-------------|
| `-group-by app` | Group by first application frame instead of top (blocking) frame |

### Limiting

| Flag | Description |
|------|-------------|
| `-top <N>` | Show only top N groups (by count) |
| `-min-group-size <N>` | Hide groups with fewer than N goroutines |

### Examples

```bash
# Grouped overview
gr g dump.txt

# Skip runtime/testing noise
gr g -no-runtime dump.txt

# Compact app-only view
gr g -no-runtime -short dump.txt

# Quick triage stats
gr g -summary dump.txt

# One-line-per-group overview of all groups
gr g -list dump.txt

# Find goroutines in a package (matches function names, file paths, created-by)
gr g -func mypackage dump.txt

# Find related goroutine pairs
gr g -func "setupAssigned|heartbeat" dump.txt

# Find blocked channel receivers
gr g -status "chan receive" dump.txt

# Find long-blocked goroutines
gr g -min-minutes 5 dump.txt

# Hide known-idle goroutines
gr g -v -func updateMetadataLoop dump.txt

# Group by application code instead of blocking syscall
gr g -group-by app dump.txt

# Show goroutine IDs for log correlation
gr g -show-ids -func mypackage dump.txt

# Compare first and last goroutine in each group
gr g -examples 2 dump.txt

# Terse output for LLM consumption (~60% fewer tokens)
gr g -terse dump.txt

# Pipe from a running process
curl http://localhost:6060/debug/pprof/goroutine?debug=2 | gr g -no-runtime -short
```

`-terse` combines each frame onto a single line with basename-only file paths,
uses `<-` instead of `created by`, and removes blank lines between groups. This
reduces token count by ~60% compared to default output while preserving all
diagnostic information (function names, file names, line numbers). Combines with
`-short`, `-no-runtime`, and other flags.

### What `-no-runtime` filters

A goroutine is considered infrastructure (and hidden by `-no-runtime`) if
**every** frame in its stack is one of:

- A function with a known stdlib prefix: `runtime.`, `runtime/`, `internal/`,
  `net.` (bare, not `net/http`), `os/signal.`, `syscall.`, `os.`, `testing.`,
  `testing/`, `sync.`, `time.`, `io.`
- A frame from a `_test.go` or `_testmain.go` file

AND the created-by function (if present) also matches.

This drops GC workers, scavengers, finalizers, signal handlers, the test
binary's main goroutine, test alarms, and test runner scaffolding — while
keeping any goroutine that has even one application frame.

### How grouping works

By default, goroutines are grouped by their **top stack frame** (the function
at the top of the stack, where the goroutine is currently blocked/running) plus
their **created-by** function. Groups are sorted by count descending.

This means goroutines doing the same thing (blocked at the same call site,
spawned by the same function) collapse into a single group regardless of
differences in their intermediate frames.

With `-group-by app`, goroutines are instead grouped by their **first
non-runtime frame** (the highest application-level function in the stack) plus
their **created-by** function name. This collapses goroutines that are in the
same application code but happen to be blocked at different runtime call sites
(e.g., different `runtime.gopark` locations for channel receives vs selects).

### CI log prefix auto-detection

`gr` automatically detects and strips CI log prefixes. If the first `goroutine`
header in the input appears at a non-zero byte offset, that offset is used as a
prefix length to strip from all subsequent lines. This handles CI output like:

```
job name	STEP	2026-03-05T06:07:24Z goroutine 1 [running]:
job name	STEP	2026-03-05T06:07:24Z runtime.gopark(...)
```

No flags needed — just pipe the raw CI log directly.

## gr cover

Analyzes Go coverage profiles produced by `go test -coverprofile=FILE`. Shows
per-function coverage sorted by percentage, highlights uncovered code, and
helps identify the biggest gaps in test coverage.

```
gr cover [flags] [coverprofile]
gr c [flags] [coverprofile]
gr c -diff [flags] old.out new.out
```

Input is read from a file (positional arg) or stdin.

The tool resolves source files via `go.mod` to map coverage blocks to function
names using `go/ast`. If source files can't be found (e.g., running outside
the project), it falls back to file-level coverage stats.

### Flags

| Flag | Description |
|------|-------------|
| `-dir <path>` | Project directory for resolving source files |
| `-func <pat>` | Filter to functions/files matching pattern |
| `-top <N>` | Show only top N results |
| `-uncovered` | Show uncovered line ranges instead of function summary |
| `-pkg` | Aggregate coverage by package instead of per-function |
| `-min-statements <N>` | Hide functions/blocks with fewer than N statements |
| `-sort stmts` | Sort by uncovered statement count descending (biggest gaps first) |
| `-no-100` | Hide fully covered functions/packages |
| `-diff` | Compare two coverage profiles (see [diff mode](#diff-mode)) |

### Examples

```bash
# Per-function coverage sorted ascending (least covered first)
go test -coverprofile=cover.out ./...
gr c cover.out

# 10 least covered functions
gr c -top 10 cover.out

# Package-level summary
gr c -pkg cover.out

# Biggest coverage gaps by uncovered statement count
gr c -sort stmts cover.out

# Focus on a specific package
gr c -func mypackage cover.out

# Show only functions that need work
gr c -no-100 cover.out

# Large untested functions
gr c -sort stmts -min-statements 5 cover.out

# Show uncovered line ranges
gr c -uncovered cover.out

# Uncovered lines in a specific package
gr c -uncovered -func mypackage cover.out

# Analyze a profile from another project
gr c -dir /path/to/project cover.out
```

### Diff mode

Compare two coverage profiles to see what changed:

```bash
# Before/after comparison
gr c -diff old.out new.out

# Filter diff to a specific package
gr c -diff -func mypackage old.out new.out

# Biggest improvements by statement count
gr c -diff -sort stmts old.out new.out
```

Output shows per-function changes with delta:

```
pkg/foo.go  HandleBar  50.0% → 85.0%  +35.0%  (5/10 → 17/20)
pkg/foo.go  NewBaz     new → 100.0%           (0/0 → 3/3)
pkg/foo.go  oldHelper  75.0% → removed        (3/4 → 0/0)

total: 67.4% → 74.3% (+6.9%, 4616/6849 → 5089/6849 statements)
```

All filter flags (`-func`, `-top`, `-sort stmts`, `-no-100`, `-min-statements`)
work with `-diff`.

### Output format

Default output shows one line per function, sorted by coverage percentage ascending:

```
goroutine/goroutine.go  ParseCorruptFatally  0.0%   (0/2)
goroutine/goroutine.go  parseNewG            90.4%  (47/52)
goroutine/stack.go      WriteTriage          100.0% (39/39)

total: 91.0% (569/625 statements)
```

With `-pkg`, shows one line per package:

```
pkg/kfake   74.3%  (5089/6849)
goroutine   91.0%  (569/625)

total: 78.2% (5658/7474 statements)
```

With `-uncovered`, shows line ranges with statement counts:

```
goroutine/goroutine.go:68-70    1 statements
goroutine/goroutine.go:407-417  3 statements
```
