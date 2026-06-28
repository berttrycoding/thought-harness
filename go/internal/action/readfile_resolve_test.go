package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The path-miss recovery: a bare or wrong-dir name that has a UNIQUE basename in the workspace
// resolves to it (disclosed), an AMBIGUOUS one falls through to the honest listing, and a direct hit
// is byte-identical to the pre-fix behaviour.
func TestReadFileBasenameResolve(t *testing.T) {
	root := t.TempDir()
	must := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("config/risk.yaml", "max_position_usd: 25000\n")
	must("data/sets/q3.txt", "revenue_usd: 184320\n")
	must("a/dup.txt", "one\n")
	must("b/dup.txt", "two\n")

	rf := NewReadFile(root)

	// 1. Direct hit — unchanged, no resolution note.
	r := rf.Execute(map[string]any{"path": "config/risk.yaml"})
	if r.IsError || !strings.Contains(r.Content, "25000") || strings.Contains(r.Content, "resolved") {
		t.Fatalf("direct hit: %+v", r)
	}
	// 2. Bare name with a unique basename -> resolved + disclosed.
	r = rf.Execute(map[string]any{"path": "risk.yaml"})
	if r.IsError || !strings.Contains(r.Content, "25000") || !strings.Contains(r.Content, "resolved \"risk.yaml\" -> config/risk.yaml") {
		t.Fatalf("bare unique: %+v", r)
	}
	// 3. Wrong-dir name with a unique basename -> resolved.
	r = rf.Execute(map[string]any{"path": "whatever/q3.txt"})
	if r.IsError || !strings.Contains(r.Content, "184320") {
		t.Fatalf("wrong-dir unique: %+v", r)
	}
	// 4. Ambiguous basename -> honest listing, NOT a guess.
	r = rf.Execute(map[string]any{"path": "dup.txt"})
	if !r.IsError || r.ErrorCode != ErrUnavailable || strings.Contains(r.Content, "resolved") {
		t.Fatalf("ambiguous must not resolve: %+v", r)
	}
	// 5. Genuinely absent -> honest listing.
	r = rf.Execute(map[string]any{"path": "nope.go"})
	if !r.IsError || !strings.Contains(r.Content, "could not find") {
		t.Fatalf("absent: %+v", r)
	}
}
