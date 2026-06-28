package action

import (
	"strings"
	"testing"
)

// TestWriteThenReadRoundTrip is the Tier-0/1 real-effect gate: a WriteFile followed by a
// ReadFile in a t.TempDir() must touch real bytes on disk and return the SAME content back,
// with the SAME ToolResult shape (a successful, non-error result naming the right tool). No
// fabrication — the file is genuinely written and genuinely read back.
func TestWriteThenReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	wf := NewWriteFile(dir)
	rf := NewReadFile(dir)

	const content = "the quick brown fox\njumps over the lazy dog\n"

	// --- WRITE: real effect, success ToolResult shape ---
	wres := wf.Execute(map[string]any{"path": "note.txt", "content": content})
	if wres.Name != "write_file" {
		t.Errorf("write ToolResult.Name = %q, want %q", wres.Name, "write_file")
	}
	if wres.IsError {
		t.Errorf("write should succeed, got IsError=true (content=%q)", wres.Content)
	}
	if wres.ErrorCode != "" {
		t.Errorf("write success must carry no ErrorCode, got %q", wres.ErrorCode)
	}
	// Python reports rune count, not byte count; this content is ASCII so they coincide.
	if want := "wrote 44 bytes to note.txt"; wres.Content != want {
		t.Errorf("write Content = %q, want %q", wres.Content, want)
	}

	// --- READ: same content back, same success ToolResult shape ---
	rres := rf.Execute(map[string]any{"path": "note.txt"})
	if rres.Name != "read_file" {
		t.Errorf("read ToolResult.Name = %q, want %q", rres.Name, "read_file")
	}
	if rres.IsError {
		t.Errorf("read should succeed, got IsError=true (content=%q)", rres.Content)
	}
	if rres.ErrorCode != "" {
		t.Errorf("read success must carry no ErrorCode, got %q", rres.ErrorCode)
	}
	// The CENTRAL assertion: the bytes written are the bytes read back.
	if rres.Content != content {
		t.Errorf("round-trip content mismatch:\n wrote %q\n read  %q", content, rres.Content)
	}

	// ToolResult shape parity between the two results: both name their tool, neither errors,
	// neither sets an exit code (a file effect is not a subprocess, so ExitCode stays nil).
	if wres.ExitCode != nil {
		t.Errorf("write ExitCode must be nil (no subprocess), got %v", *wres.ExitCode)
	}
	if rres.ExitCode != nil {
		t.Errorf("read ExitCode must be nil (no subprocess), got %v", *rres.ExitCode)
	}
}

// TestSearchCanonicalFirst is the #43 test-vs-production read-disambiguation gap at the SEARCH source: a
// symbol declared canonically in config and OVERRIDDEN in a test must surface the CANONICAL declaration as
// the FIRST hit, so the conscious (which reads the leading hit, format.go keeps the top 6) extracts the
// production value. The fixture deliberately makes the test file sort BEFORE the canonical one
// alphabetically (a_regulator_test.go < config/limits.go), the exact ordering that buried the real value
// pre-fix; canonicalFirst must reorder it so production leads.
func TestSearchCanonicalFirst(t *testing.T) {
	dir := t.TempDir()
	wf := NewWriteFile(dir)
	// the TEST override (sorts first alphabetically: "a_..." and "config/" both come before, but the test
	// path name is chosen so a naive walk would emit it first).
	wf.Execute(map[string]any{"path": "a_regulator_test.go", "content": "func TestX(){ LearnRate := 0.5 }\n"})
	// the CANONICAL declaration (the production value).
	wf.Execute(map[string]any{"path": "config/limits.go", "content": "const LearnRate = 0.05\n"})

	res := NewSearch(dir).Execute(map[string]any{"pattern": "LearnRate"})
	if res.IsError {
		t.Fatalf("search errored: %q", res.Content)
	}
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >=2 hits, got %q", res.Content)
	}
	// THE assertion: the first hit is the CANONICAL (non-test) declaration, not the test override.
	if !strings.Contains(lines[0], "config/limits.go") || !strings.Contains(lines[0], "0.05") {
		t.Fatalf("first hit must be the canonical declaration (config/limits.go = 0.05), got %q\nall: %q", lines[0], res.Content)
	}
	if !strings.Contains(lines[0], "config/limits.go") {
		t.Fatalf("test override sorted ahead of the canonical declaration (the pre-fix bug): %q", res.Content)
	}
}

// TestIsTestHit pins the classifier the canonical-first ordering depends on: test/fixture paths are
// recognised, production paths are not (so a normal source file is never mis-sorted to the back).
func TestIsTestHit(t *testing.T) {
	test := []string{
		"internal/regulator/gain_test.go:12:LearnRate = 0.5",
		"tests/test_engine.py:3:x",
		"src/foo.test.ts:9:y",
		"pkg/foo_spec.rb:4:z",
		"internal/seams/testdata/py_s6.json:1:{}",
		"fixtures/sample.go:2:a",
		"golddata/x.go:1:b",
	}
	prod := []string{
		"config/limits.go:1:const LearnRate = 0.05",
		"internal/regulator/gain.go:7:LamStar = 1.0",
		"cmd/thought/main.go:1:package main",
		"docs/contest.md:1:not a test", // "contest" must NOT match "test" as a word
	}
	for _, h := range test {
		if !isTestHit(h) {
			t.Errorf("isTestHit(%q) = false, want true", h)
		}
	}
	for _, h := range prod {
		if isTestHit(h) {
			t.Errorf("isTestHit(%q) = true, want false", h)
		}
	}
}
