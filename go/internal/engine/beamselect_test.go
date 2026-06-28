package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// beamGraph builds a graph with two stashed branches: A (higher value, pure GENERATED chatter) and
// B (slightly lower value, carries a real OBSERVATION) — the canonical case the verifier-guided beam
// exists to decide differently from value-greedy.
func beamGraph(t *testing.T) (g *graph.ThoughtGraph, aID, bID int) {
	t.Helper()
	g = graph.New("goal")
	aID = g.NewBranch(nil, nil)
	bID = g.NewBranch(nil, nil)
	// branch A: value 0.9, one GENERATED thought.
	ta := &types.Thought{ID: -1, Text: "a plausible guess", Source: types.GENERATED, BranchID: &aID}
	g.Nodes[100] = ta
	g.Branches[aID].ThoughtIDs = []int{100}
	g.Branches[aID].Value = 0.9
	g.Branches[aID].Status = types.STASHED
	// branch B: value 0.8, one real OBSERVATION thought.
	tb := &types.Thought{ID: -1, Text: "reality: the file says 6", Source: types.OBSERVATION, BranchID: &bID}
	g.Nodes[200] = tb
	g.Branches[bID].ThoughtIDs = []int{200}
	g.Branches[bID].Value = 0.8
	g.Branches[bID].Status = types.STASHED
	return g, aID, bID
}

// TestNextFocusBranchLambdaZeroIsValueGreedy: with the flag off (lam=0) the pick is EXACTLY the old
// Frontier()[0] — the higher-value branch, verifier never consulted. The default path is unchanged.
func TestNextFocusBranchLambdaZeroIsValueGreedy(t *testing.T) {
	g, aID, _ := beamGraph(t)
	best := nextFocusBranch(g, 0)
	if best == nil || best.ID != aID {
		t.Fatalf("lam=0 must pick the value-greedy frontier head %d, got %+v", aID, best)
	}
	if fr := g.Frontier(); fr[0].ID != best.ID {
		t.Fatalf("lam=0 pick must equal Frontier()[0]")
	}
}

// TestNextFocusBranchBeamPrefersGrounded: with lam=0.5 the better-GROUNDED branch B overtakes the
// higher-value A — A blends 0.5*0.9+0.5*0 = 0.45, B blends 0.5*0.8+0.5*1.0 = 0.90.
func TestNextFocusBranchBeamPrefersGrounded(t *testing.T) {
	g, _, bID := beamGraph(t)
	best := nextFocusBranch(g, 0.5)
	if best == nil || best.ID != bID {
		t.Fatalf("lam=0.5 must pick the grounded branch %d, got %+v", bID, best)
	}
}

// TestNextFocusBranchEmptyFrontier: no open branches -> nil under both policies (the BACKTRACK case
// then does nothing, exactly as before).
func TestNextFocusBranchEmptyFrontier(t *testing.T) {
	g := graph.New("goal")
	if b := nextFocusBranch(g, 0); b != nil {
		t.Fatalf("empty frontier lam=0 must be nil, got %+v", b)
	}
	if b := nextFocusBranch(g, 0.5); b != nil {
		t.Fatalf("empty frontier lam=0.5 must be nil, got %+v", b)
	}
}

// TestBranchGroundingVerifier: the v1 verifier is the reality-import fraction — 0 for pure GENERATED,
// 1 for all-OBSERVATION, the fraction in between; 0 for an empty/unknown branch.
func TestBranchGroundingVerifier(t *testing.T) {
	g, aID, bID := beamGraph(t)
	v := branchGroundingVerifier(g)
	if got := v(g.Branches[aID]); got != 0 {
		t.Fatalf("pure-GENERATED branch grounding = %v, want 0", got)
	}
	if got := v(g.Branches[bID]); got != 1 {
		t.Fatalf("all-OBSERVATION branch grounding = %v, want 1", got)
	}
	// mixed: add a GENERATED thought to B -> 1/2.
	g.Nodes[201] = &types.Thought{ID: -1, Text: "a follow-on guess", Source: types.GENERATED}
	g.Branches[bID].ThoughtIDs = append(g.Branches[bID].ThoughtIDs, 201)
	if got := v(g.Branches[bID]); got != 0.5 {
		t.Fatalf("mixed branch grounding = %v, want 0.5", got)
	}
	if got := v(nil); got != 0 {
		t.Fatalf("nil branch grounding = %v, want 0", got)
	}
}

// TestResolveBeamLambdaParsing: unset/garbage/negative -> 0 (off); >1 clamps to 1. (The package-level
// beamLambda is resolved from the test process env — unset here — so the default path is exercised by
// every other engine test in this package.)
func TestResolveBeamLambdaParsing(t *testing.T) {
	t.Setenv("THOUGHT_BEAM_LAMBDA", "")
	if v := resolveBeamLambda(); v != 0 {
		t.Fatalf("unset -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_BEAM_LAMBDA", "garbage")
	if v := resolveBeamLambda(); v != 0 {
		t.Fatalf("garbage -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_BEAM_LAMBDA", "-0.5")
	if v := resolveBeamLambda(); v != 0 {
		t.Fatalf("negative -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_BEAM_LAMBDA", "0.35")
	if v := resolveBeamLambda(); v != 0.35 {
		t.Fatalf("0.35 -> %v", v)
	}
	t.Setenv("THOUGHT_BEAM_LAMBDA", "7")
	if v := resolveBeamLambda(); v != 1 {
		t.Fatalf("7 clamps to 1, got %v", v)
	}
}
