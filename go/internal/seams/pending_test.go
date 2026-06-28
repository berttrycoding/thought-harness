package seams

import "testing"

// TestPendingInjectionBuffer pins the routing (deferred #27, 04 §3.2): an injection anchored to the
// active line injects-at-head; one anchored to a passed line proposes a retracement; an aged one drops
// stale (stale-first); Drain empties the buffer.
func TestPendingInjectionBuffer(t *testing.T) {
	b := NewPendingInjectionBuffer(5)
	b.Add("insight on the active line", 0, 10) // anchor 0 (will be active)
	b.Add("insight on a passed line", 2, 10)   // anchor 2 (passed)
	b.Add("an old idea", 3, 1)                 // created tick 1 -> age 11 > 5 at tick 12

	routed := b.Drain(12, 0) // current tick 12, active branch 0
	if len(routed) != 3 {
		t.Fatalf("drained %d, want 3", len(routed))
	}
	got := map[string]Routing{}
	for _, r := range routed {
		got[r.Text] = r.Route
	}
	if got["insight on the active line"] != InjectAtHead {
		t.Errorf("active-anchor: %v, want inject-at-head", got["insight on the active line"])
	}
	if got["insight on a passed line"] != ProposeRetracement {
		t.Errorf("passed-anchor: %v, want propose-retracement", got["insight on a passed line"])
	}
	if got["an old idea"] != DropStale {
		t.Errorf("aged: %v, want drop-stale", got["an old idea"])
	}
	if b.Len() != 0 {
		t.Errorf("buffer not emptied after Drain: Len=%d", b.Len())
	}
}
