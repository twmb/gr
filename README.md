# gr

`gr` parses Go goroutine stack dumps and groups goroutines by their top stack
frame and created-by function. It's designed for fast triage of large dumps,
especially from test timeouts and production panics. This is also meant to
help LLMs parse goroutine dumps for debugging analysis.

## Install

```
go install github.com/twmb/gr@latest
```

## Usage

```
gr [flags] [file]
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
their stack - whether in a function name like `mypackage.Run`, a file path
like `/pkg/mypackage/server.go`, or a created-by like `mypackage.NewServer`.

### Output modes

| Flag | Description |
|------|-------------|
| *(default)* | Full grouped output: group header + representative stack per group |
| `-short` | Compact: first application frame + created-by per group |
| `-list` | One line per group: count + first application frame + created-by |
| `-summary` | Stats only: status counts, top 5 groups, longest waiters |
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

## Examples

```bash
# Grouped overview
gr dump.txt

# Skip runtime/testing noise
gr -no-runtime dump.txt

# Compact app-only view
gr -no-runtime -short dump.txt

# Quick triage stats
gr -summary dump.txt

# One-line-per-group overview of all groups
gr -list dump.txt

# Find goroutines in a package (matches function names, file paths, created-by)
gr -func mypackage dump.txt

# Find related goroutine pairs
gr -func "setupAssigned|heartbeat" dump.txt

# Find blocked channel receivers
gr -status "chan receive" dump.txt

# Find long-blocked goroutines
gr -min-minutes 5 dump.txt

# Hide known-idle goroutines
gr -v -func updateMetadataLoop dump.txt

# Group by application code instead of blocking syscall
gr -group-by app dump.txt

# Show goroutine IDs for log correlation
gr -show-ids -func mypackage dump.txt

# Compare first and last goroutine in each group
gr -examples 2 dump.txt

# Pipe from a running process
curl http://localhost:6060/debug/pprof/goroutine?debug=2 | gr -no-runtime -short
```

## What `-no-runtime` filters

A goroutine is considered infrastructure (and hidden by `-no-runtime`) if
**every** frame in its stack is one of:

- A function with a known stdlib prefix: `runtime.`, `runtime/`, `internal/`,
  `net.` (bare, not `net/http`), `os/signal.`, `syscall.`, `os.`, `testing.`,
  `testing/`, `sync.`, `time.`, `io.`
- A frame from a `_test.go` or `_testmain.go` file

AND the created-by function (if present) also matches.

This drops GC workers, scavengers, finalizers, signal handlers, the test
binary's main goroutine, test alarms, and test runner scaffolding -- while
keeping any goroutine that has even one application frame.

## How grouping works

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

## CI log prefix auto-detection

`gr` automatically detects and strips CI log prefixes. If the first `goroutine`
header in the input appears at a non-zero byte offset, that offset is used as a
prefix length to strip from all subsequent lines. This handles CI output like:

```
job name	STEP	2026-03-05T06:07:24Z goroutine 1 [running]:
job name	STEP	2026-03-05T06:07:24Z runtime.gopark(...)
```

No flags needed - just pipe the raw CI log directly.
