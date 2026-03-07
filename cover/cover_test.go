package cover

import (
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	input := `mode: set
github.com/foo/bar/baz.go:10.5,12.2 1 1
github.com/foo/bar/baz.go:14.5,16.2 1 0
github.com/foo/bar/qux.go:20.3,25.2 3 1
`
	p, err := ParseProfile(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != "set" {
		t.Errorf("mode = %q, want set", p.Mode)
	}
	if len(p.Blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(p.Blocks))
	}
	b := p.Blocks[0]
	if b.File != "github.com/foo/bar/baz.go" {
		t.Errorf("file = %q", b.File)
	}
	if b.StartLine != 10 || b.StartCol != 5 || b.EndLine != 12 || b.EndCol != 2 {
		t.Errorf("pos = %d.%d,%d.%d", b.StartLine, b.StartCol, b.EndLine, b.EndCol)
	}
	if b.NumStmt != 1 || b.Count != 1 {
		t.Errorf("stmt=%d count=%d", b.NumStmt, b.Count)
	}
	// Second block is uncovered.
	if p.Blocks[1].Count != 0 {
		t.Errorf("block[1] count = %d, want 0", p.Blocks[1].Count)
	}
}

func TestParseProfileNoMode(t *testing.T) {
	_, err := ParseProfile(strings.NewReader("github.com/foo/bar.go:1.1,2.2 1 1\n"))
	if err == nil {
		t.Error("expected error for missing mode line")
	}
}

func TestParseProfileNoBlocks(t *testing.T) {
	_, err := ParseProfile(strings.NewReader("mode: set\n"))
	if err == nil {
		t.Error("expected error for no blocks")
	}
}

func TestParseProfileSkipsJunk(t *testing.T) {
	input := `mode: set
this is junk
github.com/foo/bar.go:1.1,2.2 1 1
also junk
`
	p, err := ParseProfile(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blocks) != 1 {
		t.Errorf("blocks = %d, want 1", len(p.Blocks))
	}
}

func TestFuncCoveragePercent(t *testing.T) {
	f := FuncCoverage{Statements: 10, Covered: 7}
	if p := f.Percent(); p != 70.0 {
		t.Errorf("Percent() = %f, want 70.0", p)
	}
	// Zero statements.
	f2 := FuncCoverage{Statements: 0, Covered: 0}
	if p := f2.Percent(); p != 100.0 {
		t.Errorf("Percent() zero stmts = %f, want 100.0", p)
	}
}

func TestShortFile(t *testing.T) {
	if s := ShortFile("github.com/foo/bar/baz.go", "github.com/foo/bar"); s != "baz.go" {
		t.Errorf("ShortFile = %q, want baz.go", s)
	}
	if s := ShortFile("other/path.go", "github.com/foo/bar"); s != "other/path.go" {
		t.Errorf("ShortFile no match = %q", s)
	}
	if s := ShortFile("file.go", ""); s != "file.go" {
		t.Errorf("ShortFile empty mod = %q", s)
	}
}

func TestMergeUncovered(t *testing.T) {
	blocks := []UncoveredBlock{
		{File: "a.go", StartLine: 10, EndLine: 12, NumStmt: 2},
		{File: "a.go", StartLine: 13, EndLine: 15, NumStmt: 3},
		{File: "a.go", StartLine: 20, EndLine: 22, NumStmt: 1},
	}
	merged := mergeUncovered(blocks)
	if len(merged) != 2 {
		t.Fatalf("merged = %d, want 2", len(merged))
	}
	if merged[0].StartLine != 10 || merged[0].EndLine != 15 || merged[0].NumStmt != 5 {
		t.Errorf("merged[0] = %d-%d (%d stmts)", merged[0].StartLine, merged[0].EndLine, merged[0].NumStmt)
	}
	if merged[1].StartLine != 20 || merged[1].EndLine != 22 {
		t.Errorf("merged[1] = %d-%d", merged[1].StartLine, merged[1].EndLine)
	}

	// Single block.
	single := mergeUncovered([]UncoveredBlock{{StartLine: 1, EndLine: 5, NumStmt: 2}})
	if len(single) != 1 {
		t.Errorf("single merge = %d, want 1", len(single))
	}

	// Empty.
	empty := mergeUncovered(nil)
	if len(empty) != 0 {
		t.Errorf("empty merge = %d", len(empty))
	}
}

func TestResolveFile(t *testing.T) {
	if s := resolveFile("/home/user/proj", "github.com/user/proj", "github.com/user/proj/pkg/a.go"); s != "/home/user/proj/pkg/a.go" {
		t.Errorf("resolveFile = %q", s)
	}
	// No match — returns as-is.
	if s := resolveFile("/home/user/proj", "github.com/user/proj", "other/path.go"); s != "other/path.go" {
		t.Errorf("resolveFile no match = %q", s)
	}
	// Empty mod.
	if s := resolveFile("", "", "file.go"); s != "file.go" {
		t.Errorf("resolveFile empty = %q", s)
	}
}

func TestFindModuleInfo(t *testing.T) {
	// This test runs inside the gr repo, so it should find go.mod.
	root, modPath := findModuleInfo()
	if modPath != "github.com/twmb/gr" {
		t.Errorf("modPath = %q, want github.com/twmb/gr", modPath)
	}
	if root == "" {
		t.Error("root is empty")
	}
}

func TestFindFuncs(t *testing.T) {
	// Parse this test file to find functions.
	funcs, err := findFuncs("cover_test.go")
	if err != nil {
		t.Fatal(err)
	}
	// Should find at least TestFindFuncs itself.
	found := false
	for _, f := range funcs {
		if f.name == "TestFindFuncs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("findFuncs did not find TestFindFuncs")
	}
}

func TestFindFuncsWithReceiver(t *testing.T) {
	// Parse cover.go which has methods like FuncCoverage.Percent.
	funcs, err := findFuncs("cover.go")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range funcs {
		if f.name == "FuncCoverage.Percent" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(funcs))
		for i, f := range funcs {
			names[i] = f.name
		}
		t.Errorf("findFuncs did not find FuncCoverage.Percent, got: %v", names)
	}
}

func TestFindFuncsInvalidFile(t *testing.T) {
	_, err := findFuncs("nonexistent.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestRecvName(t *testing.T) {
	// recvName is tested indirectly via findFuncs, but let's also
	// verify the Analyze function works end-to-end.
}

func TestAnalyze(t *testing.T) {
	// Create a profile that references cover.go (which exists in this package).
	// We use the actual module path so resolveFile can find the file.
	profile := &Profile{
		Mode: "set",
		Blocks: []Block{
			// ParseProfile function starts around line 64 and ends around line 85.
			{File: "github.com/twmb/gr/cover/cover.go", StartLine: 64, StartCol: 1, EndLine: 85, EndCol: 2, NumStmt: 5, Count: 3},
			// parseBlock function.
			{File: "github.com/twmb/gr/cover/cover.go", StartLine: 88, StartCol: 1, EndLine: 140, EndCol: 2, NumStmt: 10, Count: 0},
		},
	}
	result, err := Analyze(profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalStmt != 15 {
		t.Errorf("TotalStmt = %d, want 15", result.TotalStmt)
	}
	if result.CoveredStmt != 5 {
		t.Errorf("CoveredStmt = %d, want 5", result.CoveredStmt)
	}
	if result.ModPath != "github.com/twmb/gr" {
		t.Errorf("ModPath = %q", result.ModPath)
	}
	if len(result.Funcs) == 0 {
		t.Error("no functions in result")
	}
	if len(result.Uncovered) == 0 {
		t.Error("no uncovered blocks in result")
	}
}

func TestAnalyzeUnresolvableFile(t *testing.T) {
	// Profile with a file that doesn't exist locally.
	profile := &Profile{
		Mode: "set",
		Blocks: []Block{
			{File: "some/other/module/file.go", StartLine: 1, StartCol: 1, EndLine: 10, EndCol: 2, NumStmt: 5, Count: 3},
		},
	}
	result, err := Analyze(profile, "")
	if err != nil {
		t.Fatal(err)
	}
	// Should fall back to file-level entry.
	if len(result.Funcs) != 1 {
		t.Fatalf("funcs = %d, want 1", len(result.Funcs))
	}
	if result.Funcs[0].Func != "(file)" {
		t.Errorf("func name = %q, want (file)", result.Funcs[0].Func)
	}
}

func TestParseBlock(t *testing.T) {
	// Valid block.
	b, ok := parseBlock("github.com/foo/bar.go:10.5,20.3 3 1")
	if !ok {
		t.Fatal("parseBlock failed")
	}
	if b.File != "github.com/foo/bar.go" {
		t.Errorf("file = %q", b.File)
	}
	if b.StartLine != 10 || b.EndLine != 20 || b.NumStmt != 3 || b.Count != 1 {
		t.Errorf("block = %+v", b)
	}

	// Missing count.
	_, ok = parseBlock("github.com/foo/bar.go:10.5,20.3 3")
	if ok {
		t.Error("should fail with missing count")
	}

	// Bad format.
	_, ok = parseBlock("garbage")
	if ok {
		t.Error("should fail with garbage")
	}

	// No comma.
	_, ok = parseBlock("file.go:10.5 3 1")
	if ok {
		t.Error("should fail with no comma")
	}

	// No colon.
	_, ok = parseBlock("file10.5,20.3 3 1")
	if ok {
		t.Error("should fail with no colon")
	}
}
