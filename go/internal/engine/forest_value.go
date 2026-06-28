package engine

import (
	"math"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/graph"
)

// forest_value.go wires the forest-aware rerank (slice a.5b / 02-conscious.md §1.8 G5): when
// conscious.activity.forest is on, V(s) is recomputed per branch against that branch's OWN goal binding
// — a goalless branch gets intrinsic/wandering value (no goal_sim). The binding is populated by the awake
// forest / subgoal decomposition (BindBranchGoal); with no bindings the forest rerank reproduces the
// default single-goal value, so flipping the flag on is safe.

type branchGoal struct {
	goal     string
	goalless bool
	userLine bool // a user-goal line (vs a drive / wandering self-development line) — drives the μ floor
}

// BindBranchGoal binds a per-branch setpoint (G5, §1.8): a non-empty goal overrides the graph goal for
// that branch; goalless=true makes it a wandering line valued intrinsically. The binding is a USER line
// (the μ floor reserves attention for the non-user lines this does NOT set).
func (e *Engine) BindBranchGoal(bid int, goal string, goalless bool) {
	e.bindBranch(bid, branchGoal{goal: goal, goalless: goalless, userLine: !goalless})
}

// BindDriveBranch binds a non-user (drive / self-development) line — counted toward the μ_min
// self-development floor in cross-goal focus (§1.8). A goalless drive line still wanders intrinsically.
func (e *Engine) BindDriveBranch(bid int, goal string, goalless bool) {
	e.bindBranch(bid, branchGoal{goal: goal, goalless: goalless, userLine: false})
}

func (e *Engine) bindBranch(bid int, b branchGoal) {
	if e.branchGoals == nil {
		e.branchGoals = map[int]branchGoal{}
	}
	e.branchGoals[bid] = b
}

// branchBind reports a branch's goal binding for the forest rerank (empty when unbound).
func (e *Engine) branchBind(bid int) (string, bool) {
	b := e.branchGoals[bid]
	return b.goal, b.goalless
}

// rerank recomputes V over the branches — forest-aware (per-branch binding) when conscious.activity.forest
// is on, else the default single-goal rerank. The rerank sites call this instead of value.Update directly.
func (e *Engine) rerank(g *graph.ThoughtGraph) map[int]float64 {
	if e.features != nil && e.features.Conscious.Activity.Forest {
		return e.value.UpdateForest(g, e.branchBind)
	}
	return e.value.Update(g)
}

// forestFocus picks which branch (root line) the single EXPANDED "CPU" focuses on next, across the forest
// (§1.8 cross-goal focus). User lines win on value (argmax of `values`), BUT a μ_min share of focus is
// reserved for non-user lines (drives + wandering) — the self-development floor that IS the awake regime's
// μ>0 positive-baseline. Deterministic under the seeded RNG: with probability μ_min, focus the best
// NON-USER line (if any exists); otherwise focus the global argmax. This guarantees
// E[attention to non-user] ≥ μ_min. Returns the chosen branch id, or -1 if there are no branches.
//
// Only meaningful when conscious.activity.forest is on AND drive/wandering lines are bound (BindDriveBranch);
// with only user lines it reduces to the plain argmax. The caller passes the freshly-reranked `values`.
func (e *Engine) forestFocus(values map[int]float64) int {
	if len(values) == 0 {
		return -1
	}
	bids := make([]int, 0, len(values))
	for bid := range values {
		bids = append(bids, bid)
	}
	sort.Ints(bids) // stable order before any argmax / RNG draw

	best, bestV := -1, math.Inf(-1)
	bestNonUser, bestNonUserV := -1, math.Inf(-1)
	for _, bid := range bids {
		v := values[bid]
		if v > bestV {
			best, bestV = bid, v
		}
		if !e.branchGoals[bid].userLine && v > bestNonUserV {
			bestNonUser, bestNonUserV = bid, v
		}
	}

	mu := 0.0
	if e.features != nil {
		mu = e.features.Conscious.Activity.SelfDevFloor
	}
	// Reserve a μ_min share of focus for the self-development line, when one exists and the argmax is a
	// user line (a non-user argmax already satisfies the floor). The RNG is the engine's seeded source.
	if mu > 0 && bestNonUser >= 0 && e.branchGoals[best].userLine && e.rng != nil && e.rng.Float64() < mu {
		return bestNonUser
	}
	return best
}
