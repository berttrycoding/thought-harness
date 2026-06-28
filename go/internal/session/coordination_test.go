package session

import (
	"strings"
	"testing"
)

// TestCoordinationOffIsTodaysBehaviour is the P3.10 gate (the load-bearing half): with every pattern OFF
// (the shipped default / zero value), Coordinate returns the single winning result UNCHANGED — exactly
// today's behaviour.
func TestCoordinationOffIsTodaysBehaviour(t *testing.T) {
	results := []string{"the primary answer", "an alternative", "another"}
	var off CoordinationConfig // zero value = all OFF
	if off.Any() {
		t.Fatal("the zero-value config must be all-OFF")
	}
	if got := Coordinate(off, results, nil, 3); got != "the primary answer" {
		t.Fatalf("OFF must pass through the first result unchanged; got %q", got)
	}
}

// TestMergeSynthesiseCombines: with merge-synthesise ON, the results collapse into one synthesised
// answer (deduped).
func TestMergeSynthesiseCombines(t *testing.T) {
	cfg := CoordinationConfig{MergeSynthesise: true}
	got := Coordinate(cfg, []string{"use an index", "use an index", "add a cache"}, nil, 3)
	if !strings.Contains(got, "index") || !strings.Contains(got, "cache") {
		t.Fatalf("merge-synthesise should combine both unique results; got %q", got)
	}
	if strings.Count(got, "index") != 1 {
		t.Fatalf("merge-synthesise should dedup; got %q", got)
	}
}

// TestProducerCriticReverts: with producer-critic ON, a critic that REJECTS the coordinated result
// reverts to the raw first result (keep-or-revert — coordination never makes it worse).
func TestProducerCriticReverts(t *testing.T) {
	cfg := CoordinationConfig{MergeSynthesise: true, ProducerCritic: true}
	results := []string{"safe answer", "risky idea"}
	// the critic rejects anything containing "risky".
	reject := func(s string) bool { return !strings.Contains(s, "risky") }
	got := Coordinate(cfg, results, reject, 3)
	if strings.Contains(got, "risky") {
		t.Fatalf("a rejected coordinated result must revert to the raw first result; got %q", got)
	}
	if got != "safe answer" {
		t.Fatalf("revert should fall back to results[0]; got %q", got)
	}
}

// TestCoordinationAlwaysCollapsesToOne: every enabled combination still yields a SINGLE result (bounded,
// no new fan-out).
func TestCoordinationAlwaysCollapsesToOne(t *testing.T) {
	all := CoordinationConfig{MergeSynthesise: true, ProducerCritic: true, RefineLoop: true, InterdepDecompose: true}
	got := Coordinate(all, []string{"a", "b", "c"}, func(string) bool { return true }, 2)
	if got == "" {
		t.Fatal("coordination must produce a single non-empty result")
	}
}
