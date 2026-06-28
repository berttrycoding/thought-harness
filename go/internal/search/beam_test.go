package search

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// stash mints n sibling branches with the given values, all STASHED (so they form the frontier), and
// returns the graph + the branch ids in mint order.
func stashFrontier(values ...float64) (*graph.ThoughtGraph, []int) {
	g := graph.New("goal")
	ids := make([]int, len(values))
	for i, val := range values {
		id := g.NewBranch(nil, nil)
		g.Branches[id].Value = val
		g.Branches[id].Status = types.STASHED
		ids[i] = id
	}
	return g, ids
}

// TestBeamLambdaZeroIsValueOnly: with λ=0 the beam ordering is pure V(s) — identical to the value-only
// frontier, and BeamBest == View.Best. The verifier-guided path must be a strict superset that reduces
// to today's A* at λ=0 (so the flag is safe).
func TestBeamLambdaZeroIsValueOnly(t *testing.T) {
	g, ids := stashFrontier(0.5, 0.9, 0.5)
	v := New(g)
	// a verifier that would REORDER if it were consulted — but at λ=0 it must be ignored.
	verify := func(b *types.Branch) float64 {
		if b.ID == ids[0] {
			return 1.0
		}
		return 0.0
	}
	beam := v.Beam(verify, 0.0, 0)
	if len(beam) != 3 {
		t.Fatalf("beam len = %d, want 3", len(beam))
	}
	if beam[0].Branch.ID != ids[1] { // ids[1] has the top value 0.9
		t.Fatalf("λ=0 head = %d, want top-value %d", beam[0].Branch.ID, ids[1])
	}
	// tie between ids[0] and ids[2] (both 0.5) → id-ascending.
	if beam[1].Branch.ID != ids[0] || beam[2].Branch.ID != ids[2] {
		t.Fatalf("λ=0 tie order = [%d,%d], want id-ascending [%d,%d]", beam[1].Branch.ID, beam[2].Branch.ID, ids[0], ids[2])
	}
	if bb := v.BeamBest(verify, 0.0); bb == nil || bb.ID != v.Best().ID {
		t.Fatalf("BeamBest(λ=0) must equal View.Best")
	}
}

// TestBeamVerifierFlipsOrder: a lower-VALUE but fully-GROUNDED branch overtakes a higher-value
// ungrounded one once λ is high enough — the core verifier-guided behavior (don't abandon the
// better-grounded line just because its raw value is a touch lower).
func TestBeamVerifierFlipsOrder(t *testing.T) {
	// b0: value 0.60, grounding 1.0 ; b1: value 0.90, grounding 0.0
	g, ids := stashFrontier(0.60, 0.90)
	v := New(g)
	verify := func(b *types.Branch) float64 {
		if b.ID == ids[0] {
			return 1.0
		}
		return 0.0
	}
	// λ=0: value wins → b1 (0.90) is the head.
	if head := v.BeamBest(verify, 0.0); head.ID != ids[1] {
		t.Fatalf("λ=0 head = %d, want high-value %d", head.ID, ids[1])
	}
	// λ=0.5: b0 = .5*.6+.5*1 = .80 ; b1 = .5*.9+.5*0 = .45 → b0 (better grounded) wins.
	if head := v.BeamBest(verify, 0.5); head.ID != ids[0] {
		t.Fatalf("λ=0.5 head = %d, want better-grounded %d", head.ID, ids[0])
	}
	// λ=1: pure grounding → b0 wins decisively.
	if head := v.BeamBest(verify, 1.0); head.ID != ids[0] {
		t.Fatalf("λ=1 head = %d, want fully-grounded %d", head.ID, ids[0])
	}
}

// TestBeamWidthTruncates: a width-K beam returns exactly the top K.
func TestBeamWidthTruncates(t *testing.T) {
	g, _ := stashFrontier(0.1, 0.9, 0.5, 0.7)
	v := New(g)
	beam := v.Beam(nil, 0.0, 2)
	if len(beam) != 2 {
		t.Fatalf("width-2 beam len = %d, want 2", len(beam))
	}
	// top two values are 0.9 then 0.7.
	if beam[0].Value != 0.9 || beam[1].Value != 0.7 {
		t.Fatalf("width-2 beam values = [%v,%v], want [0.9,0.7]", beam[0].Value, beam[1].Value)
	}
}

// TestBeamScoreClampsAndNilVerifier: λ is clamped to [0,1] and a nil verifier contributes no grounding.
func TestBeamScoreClampsAndNilVerifier(t *testing.T) {
	b := &types.Branch{ID: 1, Value: 0.4}
	if s := BeamScore(b, nil, 0.9); s != 0.4 {
		t.Fatalf("nil verifier score = %v, want pure value 0.4", s)
	}
	if s := BeamScore(b, func(*types.Branch) float64 { return 1.0 }, 2.0); s != 1.0 {
		t.Fatalf("λ clamped-to-1 score = %v, want 1.0 (pure grounding)", s)
	}
	if s := BeamScore(b, func(*types.Branch) float64 { return 1.0 }, -1.0); s != 0.4 {
		t.Fatalf("λ clamped-to-0 score = %v, want 0.4 (pure value)", s)
	}
}

// TestBeamBestEmptyFrontier: an empty frontier yields nil (Python None), matching View.Best.
func TestBeamBestEmptyFrontier(t *testing.T) {
	g := graph.New("goal") // no stashed siblings
	v := New(g)
	if bb := v.BeamBest(nil, 0.5); bb != nil {
		t.Fatalf("BeamBest on empty frontier = %v, want nil", bb)
	}
}
