package action

import (
	"testing"
	"time"
)

// TestToolCategoryMatchesNameClassifier is the gap-6 wiring gate: every builtin's OWN Category() tag must
// agree with the name-only classifier (classifyCall) — so replacing the two name->category GUESS switches
// (subagent.go toolCategory + gateroute.go classifyCall) with the tool-owned tag never changes a routing
// decision. A drift here would mean a tool routes differently depending on which code path classified it.
func TestToolCategoryMatchesNameClassifier(t *testing.T) {
	for _, tool := range DefaultTools(t.TempDir(), time.Second) {
		owned := tool.Category()            // the tool's own tag (gap 6)
		byName := classifyCall(tool.Name()) // the name-only fallback
		if owned != byName {
			t.Errorf("%s: tool-owned Category()=%s disagrees with classifyCall=%s (the two taxonomies drifted)",
				tool.Name(), owned, byName)
		}
	}
}

// TestBuiltinCategoriesAreCorrect pins the actual taxonomy of each builtin (the values the gate routes on),
// so an accidental edit to a Category() method is caught.
func TestBuiltinCategoriesAreCorrect(t *testing.T) {
	want := map[string]TaxClass{
		"read_file":  {Op: OpInspect, Reach: ReachLocalWorld},
		"write_file": {Op: OpMutate, Reach: ReachLocalWorld},
		"run_shell":  {Op: OpExecute, Reach: ReachLocalWorld},
		"run_tests":  {Op: OpExecute, Reach: ReachLocalWorld}, // inherits RunShell.Category via embedding
		"search":     {Op: OpInspect, Reach: ReachLocalWorld},
	}
	seen := map[string]bool{}
	for _, tool := range DefaultTools(t.TempDir(), time.Second) {
		seen[tool.Name()] = true
		if got := tool.Category(); got != want[tool.Name()] {
			t.Errorf("%s.Category() = %s, want %s", tool.Name(), got, want[tool.Name()])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("builtin %s not present in DefaultTools (the taxonomy table is stale)", name)
		}
	}
}
