package cognition

import (
	"strings"
	"testing"
)

// A graph with hundreds of branches must render a BOUNDED VALUE panel — not one row per branch (the
// L2 overflow that inflated the panel to 100+ rows and shoved its siblings off the scroll).
func TestRenderValueCapsBranches(t *testing.T) {
	var branches []BranchVM
	for i := 0; i < 200; i++ {
		branches = append(branches, BranchVM{ID: i, Value: float64(i%7) / 10, Status: "STASHED"})
	}
	active := 50
	branches[active].Status = "ACTIVE"
	vm := ViewModel{Width: 72, Snap: SnapshotData{Branches: branches, ActiveBranch: &active}}

	body := renderValue(vm).Body
	lines := strings.Split(body, "\n")
	if len(lines) > maxBranchRows+8 { // header + cap rows + the "+N more" note + a little slack
		t.Fatalf("VALUE panel not capped: %d lines for 200 branches", len(lines))
	}
	if !strings.Contains(body, "more") {
		t.Fatalf("expected a '+N more' overflow note, got:\n%s", body)
	}
	if !strings.Contains(body, "b50") { // the active branch is always shown even though it isn't top-V
		t.Fatalf("active branch b50 was dropped:\n%s", body)
	}
}
