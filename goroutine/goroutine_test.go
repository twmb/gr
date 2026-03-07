package goroutine

import (
	"strings"
	"testing"
)

func TestBasicGoroutine(t *testing.T) {
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 1 {
		t.Fatalf("got %d goroutines, want 1", len(gs))
	}
	g := gs[0]
	if g.id != 1 {
		t.Errorf("id = %d, want 1", g.id)
	}
	if g.status != "running" {
		t.Errorf("status = %q, want %q", g.status, "running")
	}
	if len(g.stack) != 1 {
		t.Fatalf("stack len = %d, want 1", len(g.stack))
	}
	if g.stack[0].call.name != "main.main" {
		t.Errorf("call name = %q, want %q", g.stack[0].call.name, "main.main")
	}
	if g.stack[0].call.file != "/home/user/main.go" {
		t.Errorf("file = %q, want %q", g.stack[0].call.file, "/home/user/main.go")
	}
	if g.stack[0].call.line != 10 {
		t.Errorf("line = %d, want 10", g.stack[0].call.line)
	}
}

func TestMinutesAndLocked(t *testing.T) {
	input := `goroutine 42 [select, 5 minutes, locked to thread]:
runtime.gopark(0xc0001, 0x0, 0x0)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.id != 42 {
		t.Errorf("id = %d, want 42", g.id)
	}
	if g.status != "select" {
		t.Errorf("status = %q, want %q", g.status, "select")
	}
	if g.minutes != 5 {
		t.Errorf("minutes = %d, want 5", g.minutes)
	}
	if !g.locked {
		t.Error("locked = false, want true")
	}
	if g.stack[0].args != "0xc0001, 0x0, 0x0" {
		t.Errorf("args = %q, want %q", g.stack[0].args, "0xc0001, 0x0, 0x0")
	}
}

func TestScanAnnotation(t *testing.T) {
	input := `goroutine 7 [running (scan)]:
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.status != "running" {
		t.Errorf("status = %q, want %q", g.status, "running")
	}
	if !g.scan {
		t.Error("scan = false, want true")
	}
}

func TestLeakedAnnotation(t *testing.T) {
	input := `goroutine 99 [chan receive (leaked)]:
main.worker()
	/home/user/main.go:20 +0x30

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.status != "chan receive" {
		t.Errorf("status = %q, want %q", g.status, "chan receive")
	}
	if !g.leaked {
		t.Error("leaked = false, want true")
	}
}

func TestDurableAnnotation(t *testing.T) {
	input := `goroutine 10 [select (durable)]:
main.loop()
	/home/user/main.go:30 +0x10

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.status != "select" {
		t.Errorf("status = %q, want %q", g.status, "select")
	}
	if !g.durable {
		t.Error("durable = false, want true")
	}
}

func TestDebugHeader(t *testing.T) {
	input := `goroutine 1 gp=0xc000006000 m=0 mp=0xb08f00 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.id != 1 {
		t.Errorf("id = %d, want 1", g.id)
	}
	if g.status != "running" {
		t.Errorf("status = %q, want %q", g.status, "running")
	}
}

func TestFrameWithOffset(t *testing.T) {
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.stack[0].call.line != 10 {
		t.Errorf("line = %d, want 10", g.stack[0].call.line)
	}
}

func TestFrameWithDebugPointers(t *testing.T) {
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3 fp=0xc0000 sp=0xc0000 pc=0x4a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.stack[0].call.line != 10 {
		t.Errorf("line = %d, want 10", g.stack[0].call.line)
	}
}

func TestInlinedFrame(t *testing.T) {
	input := `goroutine 1 [running]:
main.inlined(...)
	/home/user/main.go:5
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 2 {
		t.Fatalf("stack len = %d, want 2", len(g.stack))
	}
	if !g.stack[0].inline {
		t.Error("first frame inline = false, want true")
	}
	if g.stack[1].inline {
		t.Error("second frame inline = true, want false")
	}
}

func TestAdditionalFramesElided(t *testing.T) {
	input := `goroutine 1 [running]:
main.a()
	/home/user/main.go:1 +0x10
...additional frames elided...
created by main.main
	/home/user/main.go:20 +0x30

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if !g.framesElided {
		t.Error("framesElided = false, want true")
	}
	if g.elidedCount != 0 {
		t.Errorf("elidedCount = %d, want 0", g.elidedCount)
	}
	if g.createdBy.name != "main.main" {
		t.Errorf("createdBy = %q, want %q", g.createdBy.name, "main.main")
	}
}

func TestNFramesElided(t *testing.T) {
	input := `goroutine 1 [running]:
main.a()
	/home/user/main.go:1 +0x10
...5 frames elided...
created by main.main
	/home/user/main.go:20 +0x30

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if !g.framesElided {
		t.Error("framesElided = false, want true")
	}
	if g.elidedCount != 5 {
		t.Errorf("elidedCount = %d, want 5", g.elidedCount)
	}
}

func TestCreatedByWithParentGoid(t *testing.T) {
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.main in goroutine 1
	/home/user/main.go:15 +0x20

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.createdBy.name != "main.main" {
		t.Errorf("createdBy = %q, want %q", g.createdBy.name, "main.main")
	}
	if g.parentGoid != 1 {
		t.Errorf("parentGoid = %d, want 1", g.parentGoid)
	}
}

func TestAncestorTracebacks(t *testing.T) {
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 1 {
		t.Fatalf("ancestors len = %d, want 1", len(g.ancestors))
	}
	a := g.ancestors[0]
	if a.goid != 5 {
		t.Errorf("ancestor goid = %d, want 5", a.goid)
	}
	if len(a.frames) != 1 {
		t.Fatalf("ancestor frames len = %d, want 1", len(a.frames))
	}
	if a.frames[0].call.name != "main.spawn" {
		t.Errorf("ancestor frame = %q, want %q", a.frames[0].call.name, "main.spawn")
	}
	if a.createdBy.name != "main.main" {
		t.Errorf("ancestor createdBy = %q, want %q", a.createdBy.name, "main.main")
	}
}

func TestCgoFrame(t *testing.T) {
	input := `goroutine 1 [syscall]:
non-Go function
	/usr/lib/libc.so:0
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 2 {
		t.Fatalf("stack len = %d, want 2", len(g.stack))
	}
	if !g.stack[0].cgo {
		t.Error("first frame cgo = false, want true")
	}
	if g.stack[1].cgo {
		t.Error("second frame cgo = true, want false")
	}
}

func TestUnavailableStack(t *testing.T) {
	input := `goroutine 1 [running]:
	goroutine running on other thread; stack unavailable

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 1 {
		t.Fatalf("stack len = %d, want 1", len(g.stack))
	}
	if !g.stack[0].unavailable {
		t.Error("unavailable = false, want true")
	}
}

func TestJunkBeforeFirstGoroutine(t *testing.T) {
	input := `some junk line
another junk line
SIGQUIT: quit
PC=0x4a3 m=0 sigcode=0

goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 1 {
		t.Fatalf("got %d goroutines, want 1", len(gs))
	}
}

func TestCrashSeparator(t *testing.T) {
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

-----

goroutine 2 [runnable]:
main.other()
	/home/user/main.go:20 +0x50

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 2 {
		t.Fatalf("got %d goroutines, want 2", len(gs))
	}
}

func TestMultipleGoroutines(t *testing.T) {
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

goroutine 2 [chan receive, 3 minutes]:
main.worker(0x1)
	/home/user/main.go:20 +0x30
created by main.main in goroutine 1
	/home/user/main.go:15 +0x20

goroutine 3 [select]:
main.worker(0x2)
	/home/user/main.go:20 +0x30
created by main.main in goroutine 1
	/home/user/main.go:15 +0x20

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 3 {
		t.Fatalf("got %d goroutines, want 3", len(gs))
	}
	if gs[1].minutes != 3 {
		t.Errorf("goroutine 2 minutes = %d, want 3", gs[1].minutes)
	}
	if gs[1].parentGoid != 1 {
		t.Errorf("goroutine 2 parentGoid = %d, want 1", gs[1].parentGoid)
	}
}

func TestSynctestBubble(t *testing.T) {
	input := `goroutine 1 [select, synctest bubble 3]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.synctestBubble != 3 {
		t.Errorf("synctestBubble = %d, want 3", g.synctestBubble)
	}
}

func TestLabels(t *testing.T) {
	input := `goroutine 1 [running labels:{"key1":"val1", "key2":"val2"}]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.labels) != 2 {
		t.Fatalf("labels len = %d, want 2", len(g.labels))
	}
	if g.labels[0].key != "key1" || g.labels[0].value != "val1" {
		t.Errorf("label[0] = %q:%q, want key1:val1", g.labels[0].key, g.labels[0].value)
	}
	if g.labels[1].key != "key2" || g.labels[1].value != "val2" {
		t.Errorf("label[1] = %q:%q, want key2:val2", g.labels[1].key, g.labels[1].value)
	}
}

func TestAllAnnotations(t *testing.T) {
	input := `goroutine 1 [IO wait (leaked) (scan) (durable), 10 minutes, locked to thread, synctest bubble 2]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.status != "IO wait" {
		t.Errorf("status = %q, want %q", g.status, "IO wait")
	}
	if !g.leaked {
		t.Error("leaked = false, want true")
	}
	if !g.scan {
		t.Error("scan = false, want true")
	}
	if !g.durable {
		t.Error("durable = false, want true")
	}
	if g.minutes != 10 {
		t.Errorf("minutes = %d, want 10", g.minutes)
	}
	if !g.locked {
		t.Error("locked = false, want true")
	}
	if g.synctestBubble != 2 {
		t.Errorf("synctestBubble = %d, want 2", g.synctestBubble)
	}
}

func TestNoTrailingNewline(t *testing.T) {
	// Test that parsing works when input doesn't end with a trailing blank line.
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 1 {
		t.Fatalf("got %d goroutines, want 1", len(gs))
	}
}

func TestMultipleFrames(t *testing.T) {
	input := `goroutine 1 [running]:
runtime.systemstack_switch()
	/usr/local/go/src/runtime/asm_amd64.s:350 fp=0xc0 sp=0xc0 pc=0x4a
runtime.mallocgc(0x30, 0x0?, 0x1)
	/usr/local/go/src/runtime/malloc.go:1029 +0x5e0 fp=0xc0 sp=0xc0 pc=0x4a
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 3 {
		t.Fatalf("stack len = %d, want 3", len(g.stack))
	}
	if g.stack[1].args != "0x30, 0x0?, 0x1" {
		t.Errorf("args = %q, want %q", g.stack[1].args, "0x30, 0x0?, 0x1")
	}
}

func TestCgoFrameNoFile(t *testing.T) {
	// "non-Go function" with no file/line following it (just another call).
	input := `goroutine 1 [syscall]:
non-Go function
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 2 {
		t.Fatalf("stack len = %d, want 2", len(g.stack))
	}
	if !g.stack[0].cgo {
		t.Error("first frame cgo = false, want true")
	}
	if g.stack[0].call.file != "" {
		t.Errorf("cgo frame file = %q, want empty", g.stack[0].call.file)
	}
}

func TestCoalesceByEnds(t *testing.T) {
	input := `goroutine 1 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.main
	/home/user/main.go:15 +0x20

goroutine 2 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.main
	/home/user/main.go:15 +0x20

goroutine 3 [select]:
main.other()
	/home/user/main.go:40 +0x50

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	grouped := d.Coalesce()
	groups := grouped.groups
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// Sorted by count descending, the worker group should come first.
	if len(groups[0]) != 2 {
		t.Errorf("first group size = %d, want 2", len(groups[0]))
	}
	if len(groups[1]) != 1 {
		t.Errorf("second group size = %d, want 1", len(groups[1]))
	}
}

func TestIsRuntime(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "pure runtime",
			input: `goroutine 1 [running]:
runtime.gopark(0xc0001)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0

`,
			want: true,
		},
		{
			name: "runtime with runtime created-by",
			input: `goroutine 2 [IO wait]:
internal/poll.runtime_pollWait(0xc0001, 0x72)
	/usr/local/go/src/runtime/netpoll.go:343 +0x85
created by runtime.goexit
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1

`,
			want: true,
		},
		{
			name: "app frame in stack",
			input: `goroutine 3 [chan receive]:
runtime.gopark(0xc0001)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0
myapp.Worker()
	/home/user/myapp/worker.go:10 +0x30

`,
			want: false,
		},
		{
			name: "runtime stack but app created-by",
			input: `goroutine 4 [chan receive]:
runtime.gopark(0xc0001)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0
created by myapp.Start
	/home/user/myapp/start.go:20 +0x30

`,
			want: false,
		},
		{
			name: "net prefix is runtime",
			input: `goroutine 5 [IO wait]:
net.(*netFD).Read(0xc0001)
	/usr/local/go/src/net/fd_posix.go:55 +0x28

`,
			want: true,
		},
		{
			name: "net/http is not runtime",
			input: `goroutine 6 [IO wait]:
net/http.(*Server).Serve(0xc0001)
	/usr/local/go/src/net/http/server.go:3056 +0x30

`,
			want: false,
		},
		{
			name: "syscall is runtime",
			input: `goroutine 7 [syscall]:
syscall.Syscall(0x1, 0x2, 0x3)
	/usr/local/go/src/syscall/syscall_linux.go:50 +0x30

`,
			want: true,
		},
		{
			name: "os.signal is runtime",
			input: `goroutine 8 [chan receive]:
os/signal.loop()
	/usr/local/go/src/os/signal/signal_unix.go:23 +0x30

`,
			want: true,
		},
		{
			name: "cgo frame is runtime",
			input: `goroutine 9 [syscall]:
non-Go function
	/usr/lib/libc.so:0
runtime.goexit()
	/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1

`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			g := d.Goroutines()[0]
			if got := g.IsRuntime(); got != tt.want {
				t.Errorf("IsRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAncestorWithCgoAndElision(t *testing.T) {
	// Ancestor with CGO first frame.
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
non-Go function
	/usr/lib/libc.so:0
main.spawn()
	/home/user/main.go:10 +0x10
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 1 {
		t.Fatalf("ancestors = %d, want 1", len(g.ancestors))
	}
	a := g.ancestors[0]
	if len(a.frames) != 2 {
		t.Fatalf("ancestor frames = %d, want 2", len(a.frames))
	}
	if !a.frames[0].cgo {
		t.Error("ancestor frame[0] should be cgo")
	}
}

func TestAncestorWithElidedFrames(t *testing.T) {
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
...additional frames elided...
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 1 {
		t.Fatalf("ancestors = %d, want 1", len(g.ancestors))
	}
	if g.ancestors[0].createdBy.name != "main.main" {
		t.Errorf("ancestor createdBy = %q, want main.main", g.ancestors[0].createdBy.name)
	}
}

func TestAncestorWithMultipleFrames(t *testing.T) {
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
main.helper()
	/home/user/main.go:8 +0x05
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 1 {
		t.Fatalf("ancestors = %d, want 1", len(g.ancestors))
	}
	if len(g.ancestors[0].frames) != 2 {
		t.Errorf("ancestor frames = %d, want 2", len(g.ancestors[0].frames))
	}
}

func TestAncestorWithCgoInMiddle(t *testing.T) {
	// CGO frame mid-stack in ancestor.
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
non-Go function
main.helper()
	/home/user/main.go:8 +0x05
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors[0].frames) != 3 {
		t.Errorf("ancestor frames = %d, want 3", len(g.ancestors[0].frames))
	}
	if !g.ancestors[0].frames[1].cgo {
		t.Error("ancestor frame[1] should be cgo")
	}
}

func TestMultipleAncestors(t *testing.T) {
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20

[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
created by main.init in goroutine 3
	/home/user/main.go:5 +0x08

[originating from goroutine 3]:
main.init()
	/home/user/main.go:1 +0x01
created by main.main
	/home/user/main.go:0 +0x00

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 2 {
		t.Fatalf("ancestors = %d, want 2", len(g.ancestors))
	}
	if g.ancestors[0].goid != 5 {
		t.Errorf("ancestor[0] goid = %d, want 5", g.ancestors[0].goid)
	}
	if g.ancestors[1].goid != 3 {
		t.Errorf("ancestor[1] goid = %d, want 3", g.ancestors[1].goid)
	}
}

func TestOriginatingWithoutBlankLine(t *testing.T) {
	// [originating from ...] directly after created-by file (no blank line).
	input := `goroutine 10 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.spawn in goroutine 5
	/home/user/main.go:15 +0x20
[originating from goroutine 5]:
main.spawn()
	/home/user/main.go:10 +0x10
created by main.main
	/home/user/main.go:5 +0x08

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.ancestors) != 1 {
		t.Fatalf("ancestors = %d, want 1", len(g.ancestors))
	}
}

func TestCorruptAfterGoroutine(t *testing.T) {
	// Junk line after first goroutine — the corrupt goroutine and
	// everything after it is skipped. Only goroutine 1 survives.
	input := `goroutine 1 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

goroutine 2 [chan receive]:
INVALID LINE HERE
	/home/user/main.go:20 +0x30

goroutine 3 [running]:
main.other()
	/home/user/main.go:30 +0x50

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 1 {
		t.Fatalf("got %d goroutines, want 1", len(gs))
	}
	if gs[0].id != 1 {
		t.Errorf("gs[0].id = %d, want 1", gs[0].id)
	}
}

func TestCgoMiddleOfStack(t *testing.T) {
	// CGO frame in the middle of a regular stack.
	input := `goroutine 1 [syscall]:
runtime.cgocall(0xc0001)
	/usr/local/go/src/runtime/cgocall.go:157 +0x5c
non-Go function
	/usr/lib/libc.so:0
myapp.cgoWrapper()
	/home/user/myapp/cgo.go:10 +0x30
created by myapp.Start
	/home/user/myapp/start.go:5 +0x10

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 3 {
		t.Fatalf("stack len = %d, want 3", len(g.stack))
	}
	if !g.stack[1].cgo {
		t.Error("stack[1] should be cgo")
	}
	if g.stack[1].call.file != "/usr/lib/libc.so" {
		t.Errorf("cgo file = %q, want /usr/lib/libc.so", g.stack[1].call.file)
	}
}

func TestExpectingPostEndTransition(t *testing.T) {
	// After a goroutine with created-by, the next goroutine starts normally.
	// This exercises the expectingPostEnd -> new goroutine path.
	input := `goroutine 1 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.main
	/home/user/main.go:15 +0x20

goroutine 2 [running]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 2 {
		t.Fatalf("got %d goroutines, want 2", len(gs))
	}
	if gs[0].createdBy.name != "main.main" {
		t.Errorf("gs[0] createdBy = %q", gs[0].createdBy.name)
	}
}

func TestSpaceIndentedFile(t *testing.T) {
	// File line with spaces instead of tab.
	input := `goroutine 1 [running]:
main.main()
    /home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if g.stack[0].call.file != "/home/user/main.go" {
		t.Errorf("file = %q, want /home/user/main.go", g.stack[0].call.file)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseNoGoroutines(t *testing.T) {
	_, err := Parse(strings.NewReader("just some random text\n"))
	if err == nil {
		t.Error("expected error for input with no goroutines")
	}
}

func TestFlushAtEOF(t *testing.T) {
	// Goroutine with created-by and no trailing blank line — flush at EOF.
	input := `goroutine 1 [chan receive]:
main.worker()
	/home/user/main.go:20 +0x30
created by main.main
	/home/user/main.go:15 +0x20`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 1 {
		t.Fatalf("got %d goroutines, want 1", len(gs))
	}
	if gs[0].createdBy.name != "main.main" {
		t.Errorf("createdBy = %q, want main.main", gs[0].createdBy.name)
	}
}

func TestUnavailableAfterCall(t *testing.T) {
	// "goroutine running on other thread; stack unavailable" after a function call name.
	input := `goroutine 1 [running]:
runtime.goexit()
	goroutine running on other thread; stack unavailable

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.stack) != 1 {
		t.Fatalf("stack len = %d, want 1", len(g.stack))
	}
	if !g.stack[0].unavailable {
		t.Error("should be unavailable")
	}
}

func TestParseCallNoRParen(t *testing.T) {
	// parseCall with no closing paren.
	_, _, _, ok := parseCall([]byte("main.main("))
	if ok {
		t.Error("expected parseCall to fail with no closing paren")
	}
}

func TestParseCallNoParen(t *testing.T) {
	_, _, _, ok := parseCall([]byte("noparen"))
	if ok {
		t.Error("expected parseCall to fail with no paren")
	}
}

func TestParseIntEmpty(t *testing.T) {
	_, ok := parseInt(nil)
	if ok {
		t.Error("parseInt(nil) should fail")
	}
}

func TestParseIntNonDigit(t *testing.T) {
	_, ok := parseInt([]byte("12x3"))
	if ok {
		t.Error("parseInt with non-digit should fail")
	}
}

func TestGrabQuotedEdgeCases(t *testing.T) {
	// Too short.
	if s := grabQuoted([]byte("\"")); s != "" {
		t.Errorf("grabQuoted single quote = %q, want empty", s)
	}
	// Not starting with quote.
	if s := grabQuoted([]byte("abc")); s != "" {
		t.Errorf("grabQuoted no quote = %q, want empty", s)
	}
	// Unclosed quote.
	if s := grabQuoted([]byte(`"abc`)); s != "" {
		t.Errorf("grabQuoted unclosed = %q, want empty", s)
	}
	// Escaped chars.
	s := grabQuoted([]byte(`"ab\"cd"`))
	if s != `"ab\"cd"` {
		t.Errorf("grabQuoted escaped = %q, want %q", s, `"ab\"cd"`)
	}
}

func TestParseQuotedInvalid(t *testing.T) {
	// Not a quote.
	_, _, ok := parseQuoted([]byte("abc"))
	if ok {
		t.Error("parseQuoted non-quote should fail")
	}
	// Empty.
	_, _, ok = parseQuoted(nil)
	if ok {
		t.Error("parseQuoted nil should fail")
	}
}

func TestLabelsEdgeCases(t *testing.T) {
	// Label with missing colon.
	input := `goroutine 1 [running labels:{"key" "val"}]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.labels) != 0 {
		t.Errorf("labels len = %d, want 0 (malformed)", len(g.labels))
	}
}

func TestLabelsEmptyBraces(t *testing.T) {
	input := `goroutine 1 [running labels:{}]:
main.main()
	/home/user/main.go:10 +0x1a3

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	g := d.Goroutines()[0]
	if len(g.labels) != 0 {
		t.Errorf("labels len = %d, want 0", len(g.labels))
	}
}
