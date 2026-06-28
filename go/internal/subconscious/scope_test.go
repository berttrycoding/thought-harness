package subconscious

import "testing"

// TestScopeCeilingAndPick pins slice §3.3a: the ceiling (eager) caps each registry; a pick (lazy) is
// admitted only within the ceiling — a worker can never widen it. Re-picking a facet is stable.
func TestScopeCeilingAndPick(t *testing.T) {
	s := NewScope("refactor", []string{"inspect", "transform"}, 2)

	// category ceiling.
	if !s.AllowsCategory("inspect") || !s.AllowsCategory("TRANSFORM") {
		t.Error("declared categories must be allowed (case-insensitive)")
	}
	if s.AllowsCategory("mutate") {
		t.Error("an undeclared category must be outside the ceiling")
	}

	// tier ceiling.
	if !s.AllowsTier(2) || s.AllowsTier(3) {
		t.Error("tier ceiling: <= cap allowed, above refused")
	}

	// a pick WITHIN the ceiling is admitted + recorded.
	if got, ok := s.Pick("operator", "ground_op", "inspect"); !ok || got != "ground_op" {
		t.Errorf("in-ceiling pick refused: got=%q ok=%v", got, ok)
	}
	// re-pick is stable (same facet returns the recorded pick).
	if got, ok := s.Pick("operator", "other_op", "inspect"); !ok || got != "ground_op" {
		t.Errorf("re-pick should be stable: got=%q", got)
	}
	// a pick OUTSIDE the ceiling is REFUSED (a worker can't widen it).
	if _, ok := s.Pick("tool", "rm_rf", "mutate"); ok {
		t.Error("out-of-ceiling pick must be refused")
	}

	// an empty ceiling is unconstrained.
	open := NewScope("", nil, 0)
	if !open.AllowsCategory("anything") || !open.AllowsTier(99) {
		t.Error("an empty ceiling must be unconstrained")
	}
}
