package value

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// UpdateForest is the forest-aware rerank (slice a.5b wiring): like Update, each branch's Value is
// AppraiseForest against its OWN binding via bind(bid) → (goal, goalless). An empty binding ("", false)
// reproduces the default single-goal value, so a forest run with no bindings == the default. Epistemic
// is preserved from the standard appraisal. Writes Branch.Value/Epistemic in place; returns the values.
func (v *ValueSignal) UpdateForest(g *graph.ThoughtGraph, bind func(bid int) (string, bool)) map[int]float64 {
	values := make(map[int]float64, len(g.Branches))
	bids := make([]int, 0, len(g.Branches))
	for bid := range g.Branches {
		bids = append(bids, bid)
	}
	sort.Ints(bids)
	for _, bid := range bids {
		goal, goalless := bind(bid)
		_, epistemic := v.appraiseFull(g, bid) // preserve the epistemic split (content quality)
		g.Branches[bid].Value = v.AppraiseForest(g, bid, goal, goalless).Value
		g.Branches[bid].Epistemic = epistemic
		values[bid] = g.Branches[bid].Value
	}
	v.emitEngage(g, values[g.ActiveBranch]) // AWAKE-DISP rung 1: witness the engagement floor (forest rerank path)
	return values
}

// AppraiseForest is the forest-aware value selection (slice a.5b, 02-conscious.md §1.8 / G5): a GOALLESS
// (wandering) branch gets the intrinsic value (no goal_sim term); a goal-bound branch gets the
// goal-relative value against its OWN per-branch goal (an empty goal falls back to the graph goal).
// One value function spanning extrinsic (goal_sim) and intrinsic (curiosity/coherence). The engine
// passes the per-branch (goal, goalless) binding; until that binding is threaded through the rerank
// loop this is opt-in (the default Update path still uses the single-goal AppraiseBranch).
func (v *ValueSignal) AppraiseForest(g *graph.ThoughtGraph, bid int, goal string, goalless bool) types.Appraisal {
	if goalless {
		return v.IntrinsicValue(g, bid)
	}
	return v.BranchValueForGoal(g, bid, goal)
}
