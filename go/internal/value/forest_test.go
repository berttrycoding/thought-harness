package value

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestAppraiseForest pins the forest value selection (slice a.5b): goalless → IntrinsicValue;
// goal-bound → BranchValueForGoal against the per-branch goal.
func TestAppraiseForest(t *testing.T) {
	g := graph.New("solve x")
	for _, txt := range []string{"alpha", "beta", "gamma"} {
		g.Append(&types.Thought{ID: -1, Text: txt, Source: types.GENERATED, Confidence: 0.6}, 0)
	}
	v := NewSig()
	bid := g.ActiveBranch

	if got, want := v.AppraiseForest(g, bid, "", true).Value, v.IntrinsicValue(g, bid).Value; got != want {
		t.Errorf("goalless: AppraiseForest=%v, want IntrinsicValue=%v", got, want)
	}
	if got, want := v.AppraiseForest(g, bid, "solve x", false).Value, v.BranchValueForGoal(g, bid, "solve x").Value; got != want {
		t.Errorf("goal-bound: AppraiseForest=%v, want BranchValueForGoal=%v", got, want)
	}
}

// TestUpdateForestUnboundMatchesDefault is the safety property for the #18 wiring: the forest rerank with
// NO per-branch bindings reproduces the default single-goal Update exactly (so flipping the flag on is safe).
func TestUpdateForestUnboundMatchesDefault(t *testing.T) {
	g := graph.New("solve x")
	for _, txt := range []string{"a", "b", "c"} {
		g.Append(&types.Thought{ID: -1, Text: txt, Source: types.GENERATED, Confidence: 0.6}, 0)
	}
	v := NewSig()
	def := v.Update(g)
	forest := v.UpdateForest(g, func(int) (string, bool) { return "", false })
	if len(forest) != len(def) {
		t.Fatalf("len: forest %d, default %d", len(forest), len(def))
	}
	for bid := range def {
		if forest[bid] != def[bid] {
			t.Errorf("b%d: forest %v != default %v (empty binding must reproduce the default)", bid, forest[bid], def[bid])
		}
	}
}
