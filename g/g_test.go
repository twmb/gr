package g

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
