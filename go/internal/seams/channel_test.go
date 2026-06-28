package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestSeamConflictIsSurvivorCountNotStance is the P0.3 (SR-1, seam-as-channel) gate: the seam reports
// "conflict" purely from whether MORE THAN ONE candidate survived admission — competing alternatives
// exist — and NEVER from Stance. Two substantive candidates that both ADMIT but carry NO stance (so the
// old stance detector would have said no-conflict) must still surface as a competition: Conflict=true
// with the non-primary survivor exposed as a loser the Controller can fork to a branch. This test would
// FAIL under the removed Gate.Conflicting (zero distinct stances -> false).
func TestSeamConflictIsSurvivorCountNotStance(t *testing.T) {
	bus, _ := captureBus()
	backend := backends.NewTest()
	filt := NewFilter("control", backend, bus.Emit)
	gate := NewGate(bus.Emit)
	seam := NewHiddenSeam(gate, filt, backend, bus.Emit)

	hist := []types.Thought{
		{ID: 1, Text: "Considering how to lay out the cache", Source: types.GENERATED, Confidence: 0.7},
	}
	// Two clearly-admittable injected candidates with NO stance at all (Stance nil). Under the old
	// stance-conflict rule these would NOT conflict (no distinct stances); under seam-as-channel they
	// compete because both survive.
	cands := []types.Candidate{
		{Text: "Use a write-through cache so reads stay consistent", Source: types.INJECTED,
			Domain: sp("design"), Relevance: 0.9},
		{Text: "Use a write-back cache so writes stay fast", Source: types.INJECTED,
			Domain: sp("design"), Relevance: 0.85},
	}

	res := seam.Relay(cands, hist, map[string]float64{}, 0.5)

	if res.Thought == nil {
		t.Fatal("expected a voiced primary thought (one survivor re-voiced)")
	}
	if !res.Conflict {
		t.Fatal("two surviving candidates must register as competition (conflict=true) even with no stance")
	}
	if len(res.Losers) != 1 {
		t.Fatalf("the non-primary survivor must be surfaced as a competing alternative; got %d losers",
			len(res.Losers))
	}
	// Both candidates were admitted (the competition is real, not an artefact of one rejection).
	admitted := 0
	for _, vp := range res.Verdicts {
		if vp.Verdict.Admit() {
			admitted++
		}
	}
	if admitted != 2 {
		t.Fatalf("both candidates should ADMIT (so they genuinely compete); admitted=%d", admitted)
	}
}

// TestSeamSingleSurvivorNoConflict pins the other edge: one survivor is not a competition. A lone
// admitted candidate is voiced as the continuation with Conflict=false and no losers — nothing for the
// Controller to branch.
func TestSeamSingleSurvivorNoConflict(t *testing.T) {
	bus, _ := captureBus()
	backend := backends.NewTest()
	filt := NewFilter("control", backend, bus.Emit)
	gate := NewGate(bus.Emit)
	seam := NewHiddenSeam(gate, filt, backend, bus.Emit)

	res := seam.Relay(
		[]types.Candidate{
			{Text: "Index the table on the user id to speed the join", Source: types.INJECTED,
				Domain: sp("design"), Relevance: 0.9},
		},
		[]types.Thought{{ID: 1, Text: "thinking about the slow query", Source: types.GENERATED}},
		map[string]float64{}, 0.5,
	)
	if res.Thought == nil {
		t.Fatal("a single admitted candidate must still be voiced")
	}
	if res.Conflict {
		t.Fatal("a single survivor is not a competition (conflict must be false)")
	}
	if len(res.Losers) != 0 {
		t.Fatalf("a single survivor has no competing alternatives; got %d losers", len(res.Losers))
	}
}
