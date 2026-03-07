package goroutine

import (
	"bytes"
	"strings"
	"testing"
)

// helpers to build goroutines for testing without parsing.
func mkGoroutine(id int, status string, minutes int, stackCalls ...string) *Goroutine {
	g := &Goroutine{
		id:             id,
		status:         status,
		minutes:        minutes,
		parentGoid:     -1,
		synctestBubble: -1,
	}
	for _, name := range stackCalls {
		g.stack = append(g.stack, frame{
			call: call{name: name, file: "/src/" + name + ".go", line: 10},
		})
	}
	return g
}

func mkGoroutineWithCreatedBy(id int, status string, minutes int, createdByName string, stackCalls ...string) *Goroutine {
	g := mkGoroutine(id, status, minutes, stackCalls...)
	g.createdBy = call{name: createdByName, file: "/src/main.go", line: 5}
	return g
}

func TestAccessors(t *testing.T) {
	g := mkGoroutineWithCreatedBy(42, "chan receive", 5, "main.start", "myapp.Worker")
	g.locked = true
	if g.ID() != 42 {
		t.Errorf("ID() = %d, want 42", g.ID())
	}
	if g.Status() != "chan receive" {
		t.Errorf("Status() = %q, want %q", g.Status(), "chan receive")
	}
	if g.Minutes() != 5 {
		t.Errorf("Minutes() = %d, want 5", g.Minutes())
	}
	if !g.Locked() {
		t.Error("Locked() = false, want true")
	}
	if g.CreatedByName() != "main.start" {
		t.Errorf("CreatedByName() = %q, want %q", g.CreatedByName(), "main.start")
	}
}

func TestHasFunc(t *testing.T) {
	g := mkGoroutineWithCreatedBy(1, "running", 0, "main.main", "runtime.gopark", "myapp.Worker")
	// Match by stack function name.
	if !g.HasFunc("myapp") {
		t.Error("HasFunc(myapp) = false, want true")
	}
	// Match by stack file path.
	if !g.HasFunc("runtime.gopark.go") {
		t.Error("HasFunc(runtime.gopark.go) = false, want true")
	}
	// Match by created-by name.
	if !g.HasFunc("main.main") {
		t.Error("HasFunc(main.main) = false, want true")
	}
	// Match by created-by file.
	if !g.HasFunc("/src/main.go") {
		t.Error("HasFunc(/src/main.go) = false, want true")
	}
	// No match.
	if g.HasFunc("nonexistent") {
		t.Error("HasFunc(nonexistent) = true, want false")
	}
}

func TestFirstAppFrame(t *testing.T) {
	// All runtime frames: returns first frame.
	g1 := mkGoroutine(1, "running", 0, "runtime.gopark", "runtime.selectgo")
	f1 := firstAppFrame(g1)
	if f1.call.name != "runtime.gopark" {
		t.Errorf("all-runtime firstAppFrame = %q, want %q", f1.call.name, "runtime.gopark")
	}

	// Mixed: returns first non-runtime frame.
	g2 := mkGoroutine(2, "running", 0, "runtime.gopark", "myapp.Worker", "myapp.main")
	f2 := firstAppFrame(g2)
	if f2.call.name != "myapp.Worker" {
		t.Errorf("mixed firstAppFrame = %q, want %q", f2.call.name, "myapp.Worker")
	}

	// cgo/unavailable frames are skipped.
	g3 := &Goroutine{
		id:             3,
		status:         "running",
		parentGoid:     -1,
		synctestBubble: -1,
		stack: []frame{
			{cgo: true},
			{unavailable: true},
			{call: call{name: "myapp.Handler", file: "/src/handler.go", line: 1}},
		},
	}
	f3 := firstAppFrame(g3)
	if f3.call.name != "myapp.Handler" {
		t.Errorf("cgo firstAppFrame = %q, want %q", f3.call.name, "myapp.Handler")
	}
}

func TestFrameDisplayName(t *testing.T) {
	tests := []struct {
		name string
		f    frame
		want string
	}{
		{"cgo", frame{cgo: true}, "non-Go function"},
		{"unavailable", frame{unavailable: true}, "<unavailable>"},
		{"normal", frame{call: call{name: "myapp.Worker"}}, "myapp.Worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := frameDisplayName(&tt.f)
			if got != tt.want {
				t.Errorf("frameDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewDump(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutine(1, "running", 0, "main.main"),
		mkGoroutine(2, "chan receive", 0, "main.worker"),
	}
	d := NewDump(gs)
	if len(d.Goroutines()) != 2 {
		t.Errorf("Goroutines() len = %d, want 2", len(d.Goroutines()))
	}
}

func TestCoalesceByApp(t *testing.T) {
	// Two goroutines with same app frame but different runtime top frames.
	gs := []*Goroutine{
		mkGoroutineWithCreatedBy(1, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutineWithCreatedBy(2, "select", 0, "main.main", "runtime.selectgo", "myapp.Worker"),
		mkGoroutine(3, "running", 0, "other.Handler"),
	}
	d := NewDump(gs)
	grouped := d.CoalesceByApp()
	if len(grouped.groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(grouped.groups))
	}
	// Largest group first.
	if len(grouped.groups[0]) != 2 {
		t.Errorf("first group size = %d, want 2", len(grouped.groups[0]))
	}
}

func TestGroupedTop(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutine(1, "running", 0, "a.A"),
		mkGoroutine(2, "running", 0, "b.B"),
		mkGoroutine(3, "running", 0, "c.C"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()
	top2 := grouped.Top(2)
	if len(top2.groups) != 2 {
		t.Errorf("Top(2) groups = %d, want 2", len(top2.groups))
	}
	// Top(N) where N >= groups returns same.
	all := grouped.Top(100)
	if len(all.groups) != 3 {
		t.Errorf("Top(100) groups = %d, want 3", len(all.groups))
	}
}

func TestFilterMinGroupSize(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutine(1, "chan receive", 0, "myapp.Worker"),
		mkGoroutine(2, "chan receive", 0, "myapp.Worker"),
		mkGoroutine(3, "running", 0, "myapp.Other"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()
	filtered := grouped.FilterMinGroupSize(2)
	if len(filtered.groups) != 1 {
		t.Fatalf("FilterMinGroupSize(2) groups = %d, want 1", len(filtered.groups))
	}
	if len(filtered.groups[0]) != 2 {
		t.Errorf("group size = %d, want 2", len(filtered.groups[0]))
	}
}

func TestWriteSummary(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutine(1, "running", 0, "a.A"),
		mkGoroutine(2, "running", 0, "a.A"),
		mkGoroutine(3, "running", 0, "b.B"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()
	var buf bytes.Buffer
	grouped.WriteSummary(&buf)
	out := buf.String()
	if !strings.Contains(out, "2 groups") {
		t.Errorf("WriteSummary missing group count: %q", out)
	}
	if !strings.Contains(out, "3 goroutines") {
		t.Errorf("WriteSummary missing goroutine count: %q", out)
	}

	// Empty grouped.
	buf.Reset()
	empty := &Grouped{}
	empty.WriteSummary(&buf)
	if !strings.Contains(buf.String(), "no groups remaining") {
		t.Errorf("empty WriteSummary = %q, want 'no groups remaining'", buf.String())
	}
}

func TestFrameWriteTo(t *testing.T) {
	// Normal frame.
	var buf bytes.Buffer
	f := frame{call: call{name: "myapp.Handler", file: "/src/handler.go", line: 42}}
	f.writeTo(&buf)
	if !strings.Contains(buf.String(), "myapp.Handler") || !strings.Contains(buf.String(), "/src/handler.go:42") {
		t.Errorf("writeTo normal = %q", buf.String())
	}

	// CGO frame with file.
	buf.Reset()
	f2 := frame{cgo: true, call: call{file: "/usr/lib/libc.so", line: 0}}
	f2.writeTo(&buf)
	if !strings.Contains(buf.String(), "non-Go function") || !strings.Contains(buf.String(), "/usr/lib/libc.so") {
		t.Errorf("writeTo cgo = %q", buf.String())
	}

	// CGO frame without file.
	buf.Reset()
	f3 := frame{cgo: true}
	f3.writeTo(&buf)
	out := buf.String()
	if !strings.Contains(out, "non-Go function") {
		t.Errorf("writeTo cgo no file = %q", out)
	}
	if strings.Contains(out, "\t") {
		t.Errorf("writeTo cgo no file should not have file line: %q", out)
	}

	// Unavailable frame.
	buf.Reset()
	f4 := frame{unavailable: true, call: call{name: "runtime.goexit"}}
	f4.writeTo(&buf)
	if !strings.Contains(buf.String(), "<unavailable>") {
		t.Errorf("writeTo unavailable = %q", buf.String())
	}
}

func TestWriteGroupHeader(t *testing.T) {
	// Single goroutine, no minutes.
	group := []*Goroutine{mkGoroutine(1, "running", 0, "a.A")}
	var buf bytes.Buffer
	writeGroupHeader(&buf, group, false)
	out := buf.String()
	if !strings.Contains(out, "1 goroutine(s)") || !strings.Contains(out, "[running]") {
		t.Errorf("header = %q", out)
	}
	if strings.Contains(out, "min") {
		t.Errorf("no-minutes header should not have min: %q", out)
	}

	// Same minutes across group.
	buf.Reset()
	group2 := []*Goroutine{
		mkGoroutine(1, "chan receive", 5, "a.A"),
		mkGoroutine(2, "chan receive", 5, "a.A"),
	}
	writeGroupHeader(&buf, group2, false)
	if !strings.Contains(buf.String(), "[5 min]") {
		t.Errorf("same-minutes header = %q", buf.String())
	}

	// Different minutes.
	buf.Reset()
	group3 := []*Goroutine{
		mkGoroutine(1, "chan receive", 3, "a.A"),
		mkGoroutine(2, "chan receive", 10, "a.A"),
	}
	writeGroupHeader(&buf, group3, false)
	if !strings.Contains(buf.String(), "[3-10 min]") {
		t.Errorf("range-minutes header = %q", buf.String())
	}

	// Show IDs.
	buf.Reset()
	writeGroupHeader(&buf, group3, true)
	out = buf.String()
	if !strings.Contains(out, "[goroutines") || !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Errorf("show-ids header = %q", out)
	}

	// Show IDs with >10 goroutines.
	buf.Reset()
	bigGroup := make([]*Goroutine, 15)
	for i := range bigGroup {
		bigGroup[i] = mkGoroutine(i+1, "select", 0, "a.A")
	}
	writeGroupHeader(&buf, bigGroup, true)
	out = buf.String()
	if !strings.Contains(out, "+5 more") {
		t.Errorf("big group header should have +N more: %q", out)
	}
}

func TestWriteGoroutineStack(t *testing.T) {
	// Basic stack.
	g := mkGoroutineWithCreatedBy(1, "running", 0, "main.main", "myapp.Worker")
	var buf bytes.Buffer
	writeGoroutineStack(&buf, g)
	out := buf.String()
	if !strings.Contains(out, "myapp.Worker") || !strings.Contains(out, "created by main.main") {
		t.Errorf("writeGoroutineStack = %q", out)
	}

	// With frames elided (additional).
	buf.Reset()
	g2 := mkGoroutineWithCreatedBy(2, "running", 0, "main.main", "a.A")
	g2.framesElided = true
	writeGoroutineStack(&buf, g2)
	if !strings.Contains(buf.String(), "...additional frames elided...") {
		t.Errorf("additional elided = %q", buf.String())
	}

	// With frames elided (N count).
	buf.Reset()
	g3 := mkGoroutineWithCreatedBy(3, "running", 0, "main.main", "a.A")
	g3.framesElided = true
	g3.elidedCount = 7
	writeGoroutineStack(&buf, g3)
	if !strings.Contains(buf.String(), "...7 frames elided...") {
		t.Errorf("N elided = %q", buf.String())
	}

	// No created-by.
	buf.Reset()
	g4 := mkGoroutine(4, "running", 0, "main.main")
	writeGoroutineStack(&buf, g4)
	if strings.Contains(buf.String(), "created by") {
		t.Errorf("no created-by should not have 'created by': %q", buf.String())
	}
}

func TestPickExamples(t *testing.T) {
	gs := make([]*Goroutine, 10)
	for i := range gs {
		gs[i] = mkGoroutine(i+1, "running", 0, "a.A")
	}

	// N >= len returns all.
	all := pickExamples(gs, 20)
	if len(all) != 10 {
		t.Errorf("pickExamples(20) = %d, want 10", len(all))
	}

	// N == 1 returns first by ID.
	one := pickExamples(gs, 1)
	if len(one) != 1 || one[0].id != 1 {
		t.Errorf("pickExamples(1) id = %d, want 1", one[0].id)
	}

	// N == 2 returns first and last.
	two := pickExamples(gs, 2)
	if len(two) != 2 {
		t.Fatalf("pickExamples(2) = %d, want 2", len(two))
	}
	if two[0].id != 1 || two[1].id != 10 {
		t.Errorf("pickExamples(2) ids = [%d, %d], want [1, 10]", two[0].id, two[1].id)
	}

	// N == 4 returns first, last, plus middle samples.
	four := pickExamples(gs, 4)
	if len(four) != 4 {
		t.Fatalf("pickExamples(4) = %d, want 4", len(four))
	}
}

func TestGroupedWrite(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutineWithCreatedBy(1, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutineWithCreatedBy(2, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutine(3, "running", 0, "myapp.Other"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()

	// Default write (one representative per group).
	var buf bytes.Buffer
	grouped.Write(&buf, WriteOpt{})
	out := buf.String()
	if !strings.Contains(out, "2 goroutine(s)") || !strings.Contains(out, "1 goroutine(s)") {
		t.Errorf("Write missing group headers: %q", out)
	}
	if !strings.Contains(out, "myapp.Worker") {
		t.Errorf("Write missing stack: %q", out)
	}

	// Write with ShowIDs.
	buf.Reset()
	grouped.Write(&buf, WriteOpt{ShowIDs: true})
	if !strings.Contains(buf.String(), "[goroutines") {
		t.Errorf("Write ShowIDs missing IDs: %q", buf.String())
	}

	// Write with PerGroup examples.
	buf.Reset()
	grouped.Write(&buf, WriteOpt{PerGroup: 2})
	out = buf.String()
	if !strings.Contains(out, "goroutine 1:") || !strings.Contains(out, "goroutine 2:") {
		t.Errorf("Write PerGroup missing examples: %q", out)
	}

	// Empty grouped.
	buf.Reset()
	empty := &Grouped{}
	empty.Write(&buf, WriteOpt{})
	if !strings.Contains(buf.String(), "no groups remaining") {
		t.Errorf("empty Write = %q", buf.String())
	}
}

func TestGroupedWriteShort(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutineWithCreatedBy(1, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutineWithCreatedBy(2, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()

	var buf bytes.Buffer
	grouped.WriteShort(&buf, WriteOpt{})
	out := buf.String()
	if !strings.Contains(out, "2 goroutine(s)") {
		t.Errorf("WriteShort missing header: %q", out)
	}
	// Should show first app frame, not runtime.
	if !strings.Contains(out, "myapp.Worker") {
		t.Errorf("WriteShort missing app frame: %q", out)
	}
	if !strings.Contains(out, "created by main.main") {
		t.Errorf("WriteShort missing created-by: %q", out)
	}

	// Empty grouped.
	buf.Reset()
	empty := &Grouped{}
	empty.WriteShort(&buf, WriteOpt{})
	if !strings.Contains(buf.String(), "no groups remaining") {
		t.Errorf("empty WriteShort = %q", buf.String())
	}
}

func TestGroupedWriteList(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutineWithCreatedBy(1, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutineWithCreatedBy(2, "chan receive", 0, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutine(3, "running", 0, "myapp.Other"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()

	var buf bytes.Buffer
	grouped.WriteList(&buf)
	out := buf.String()
	// Should have two lines, one per group.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("WriteList lines = %d, want 2: %q", len(lines), out)
	}
	// First line should show count 2 with created-by.
	if !strings.Contains(lines[0], "2\t") || !strings.Contains(lines[0], "<- main.main") {
		t.Errorf("WriteList first line = %q", lines[0])
	}
	// Second line has no created-by.
	if !strings.Contains(lines[1], "1\t") {
		t.Errorf("WriteList second line = %q", lines[1])
	}

	// Empty grouped.
	buf.Reset()
	empty := &Grouped{}
	empty.WriteList(&buf)
	if !strings.Contains(buf.String(), "no groups remaining") {
		t.Errorf("empty WriteList = %q", buf.String())
	}
}

func TestWriteTriage(t *testing.T) {
	gs := []*Goroutine{
		mkGoroutineWithCreatedBy(1, "chan receive", 3, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutineWithCreatedBy(2, "chan receive", 10, "main.main", "runtime.gopark", "myapp.Worker"),
		mkGoroutine(3, "select", 0, "myapp.Other"),
	}
	d := NewDump(gs)
	grouped := d.Coalesce()

	var buf bytes.Buffer
	WriteTriage(&buf, grouped)
	out := buf.String()

	if !strings.Contains(out, "Total: 3 goroutines") {
		t.Errorf("WriteTriage missing total: %q", out)
	}
	if !strings.Contains(out, "Status:") {
		t.Errorf("WriteTriage missing status: %q", out)
	}
	if !strings.Contains(out, "chan receive") || !strings.Contains(out, "select") {
		t.Errorf("WriteTriage missing statuses: %q", out)
	}
	if !strings.Contains(out, "Top") {
		t.Errorf("WriteTriage missing top groups: %q", out)
	}
	if !strings.Contains(out, "Longest waiter: 10 min") {
		t.Errorf("WriteTriage missing longest waiter: %q", out)
	}

	// No longest waiter when no minutes.
	buf.Reset()
	gs2 := []*Goroutine{mkGoroutine(1, "running", 0, "a.A")}
	d2 := NewDump(gs2)
	grouped2 := d2.Coalesce()
	WriteTriage(&buf, grouped2)
	if strings.Contains(buf.String(), "Longest waiter") {
		t.Errorf("WriteTriage should not have longest waiter: %q", buf.String())
	}

	// Empty grouped.
	buf.Reset()
	empty := &Grouped{}
	WriteTriage(&buf, empty)
	if !strings.Contains(buf.String(), "no groups remaining") {
		t.Errorf("empty WriteTriage = %q", buf.String())
	}
}

func TestWriteShortNoCreatedBy(t *testing.T) {
	gs := []*Goroutine{mkGoroutine(1, "running", 0, "myapp.Main")}
	d := NewDump(gs)
	grouped := d.Coalesce()
	var buf bytes.Buffer
	grouped.WriteShort(&buf, WriteOpt{})
	if strings.Contains(buf.String(), "created by") {
		t.Errorf("WriteShort should not have created-by: %q", buf.String())
	}
}

// Integration test: parse a dump and exercise all output modes.
func TestFullPipelineOutput(t *testing.T) {
	input := `goroutine 1 [chan receive, 5 minutes]:
runtime.gopark(0xc0001)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0
myapp.Worker(0x1)
	/home/user/myapp/worker.go:20 +0x30
created by myapp.Start in goroutine 10
	/home/user/myapp/start.go:15 +0x20

goroutine 2 [chan receive, 3 minutes]:
runtime.gopark(0xc0001)
	/usr/local/go/src/runtime/proc.go:381 +0x1a0
myapp.Worker(0x2)
	/home/user/myapp/worker.go:20 +0x30
created by myapp.Start in goroutine 10
	/home/user/myapp/start.go:15 +0x20

goroutine 3 [select]:
runtime.selectgo(0xc0001)
	/usr/local/go/src/runtime/select.go:328 +0x7ae
myapp.Manager()
	/home/user/myapp/manager.go:50 +0x100
created by myapp.Start in goroutine 10
	/home/user/myapp/start.go:20 +0x40

goroutine 4 [running]:
myapp.Handler()
	/home/user/myapp/handler.go:10 +0x50

`
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 4 {
		t.Fatalf("got %d goroutines, want 4", len(gs))
	}

	dump := NewDump(gs)
	grouped := dump.Coalesce()
	var buf bytes.Buffer

	// Write.
	grouped.Write(&buf, WriteOpt{})
	out := buf.String()
	if !strings.Contains(out, "2 goroutine(s)") {
		t.Errorf("Write missing group of 2: %q", out)
	}

	// WriteShort.
	buf.Reset()
	grouped.WriteShort(&buf, WriteOpt{})
	out = buf.String()
	if !strings.Contains(out, "myapp.Worker") {
		t.Errorf("WriteShort missing app frame: %q", out)
	}

	// WriteList.
	buf.Reset()
	grouped.WriteList(&buf)

	// WriteTriage.
	buf.Reset()
	WriteTriage(&buf, grouped)
	out = buf.String()
	if !strings.Contains(out, "Total: 4 goroutines") {
		t.Errorf("WriteTriage total: %q", out)
	}
	if !strings.Contains(out, "Longest waiter: 5 min") {
		t.Errorf("WriteTriage longest: %q", out)
	}

	// WriteSummary.
	buf.Reset()
	grouped.WriteSummary(&buf)
	out = buf.String()
	if !strings.Contains(out, "3 groups") || !strings.Contains(out, "4 goroutines") {
		t.Errorf("WriteSummary: %q", out)
	}

	// CoalesceByApp: workers should group together even with different runtime tops.
	grouped2 := dump.CoalesceByApp()
	buf.Reset()
	grouped2.WriteSummary(&buf)

	// Top + FilterMinGroupSize.
	top1 := grouped.Top(1)
	buf.Reset()
	top1.WriteSummary(&buf)
	if !strings.Contains(buf.String(), "1 groups") {
		t.Errorf("Top(1) summary: %q", buf.String())
	}

	min2 := grouped.FilterMinGroupSize(2)
	if len(min2.groups) != 1 {
		t.Errorf("FilterMinGroupSize(2) = %d groups, want 1", len(min2.groups))
	}

	// PerGroup examples.
	buf.Reset()
	grouped.Write(&buf, WriteOpt{PerGroup: 1})
	out = buf.String()
	if !strings.Contains(out, "goroutine 1:") {
		t.Errorf("PerGroup(1) missing example: %q", out)
	}

	// ShowIDs.
	buf.Reset()
	grouped.Write(&buf, WriteOpt{ShowIDs: true})
	if !strings.Contains(buf.String(), "[goroutines") {
		t.Errorf("ShowIDs missing: %q", buf.String())
	}
}

func TestCILogPrefixParsing(t *testing.T) {
	// Simulate CI logs with a fixed-width prefix on every line.
	// The prefix length is auto-detected from the first "goroutine " occurrence.
	pfx := "2024-01-15T10:30:00Z "
	input := pfx + "goroutine 1 [running]:\n" +
		pfx + "main.main()\n" +
		pfx + "\t/home/user/main.go:10 +0x1a3\n" +
		pfx + "\n" +
		pfx + "goroutine 2 [chan receive]:\n" +
		pfx + "runtime.gopark(0xc0001)\n" +
		pfx + "\t/usr/local/go/src/runtime/proc.go:381 +0x1a0\n" +
		pfx + "\n"
	d, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gs := d.Goroutines()
	if len(gs) != 2 {
		t.Fatalf("got %d goroutines, want 2", len(gs))
	}
}
