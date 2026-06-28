package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// TestSubAgentCategoryScope pins the #31 SubAgent category-scope: a sub-agent staffed under a category
// ceiling has its toolScope FILTERED by category — a tool outside the band is dropped, so a read-only
// sub-agent cannot reach a mutate tool even if it was listed. No scope => the flat toolScope (unchanged).
func TestSubAgentCategoryScope(t *testing.T) {
	tools := []string{"read_file", "search", "write_file", "run_shell"}
	mk := func() *SubAgent {
		return NewSubAgent(cognition.OperatorSpec{Name: "probe"}, "code", "goal", nil, nil, "sa:probe",
			tools, nil, nil)
	}

	// no scope -> the flat toolScope, byte-identical.
	if got := mk().ScopedToolScope(); len(got) != len(tools) {
		t.Fatalf("no scope: expected the flat toolScope (%d), got %d", len(tools), len(got))
	}

	// a read-only (inspect) ceiling -> only inspect-category tools survive.
	ro := mk().WithScope(NewScope("code", []string{"inspect"}, 0))
	got := ro.ScopedToolScope()
	want := map[string]bool{"read_file": true, "search": true}
	if len(got) != len(want) {
		t.Fatalf("inspect ceiling: expected %d tools, got %v", len(want), got)
	}
	for _, tname := range got {
		if !want[tname] {
			t.Errorf("inspect ceiling leaked a non-inspect tool: %q", tname)
		}
	}

	// an inspect+execute ceiling -> read/search/run survive, write dropped.
	ie := mk().WithScope(NewScope("code", []string{"inspect", "execute"}, 0))
	for _, tname := range ie.ScopedToolScope() {
		if tname == "write_file" {
			t.Error("inspect+execute ceiling must drop the mutate tool write_file")
		}
	}
}
