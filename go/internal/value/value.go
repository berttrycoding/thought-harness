// Package value holds the value signal — one scalar V(s) consumed at four sites.
//
//	Branch.value (rerank)        — which branch to expand next
//	Filter.admit (trust)         — trust this raw candidate before voicing?
//	Controller.loop_exhausted    — think more, or pay to ACT?
//	convertibility               — which patterns are worth compiling?
//
// MVP status: bootstrap only (the LLM-propose-and-recommend *shape*, faked deterministically). Not
// yet RL-grounded. The spec's §12 path: bootstrap -> reality-grounded RL -> distil into a cheap
// learned V. reward is wired to come only from grounded OBSERVATIONs, never self-grading.
//
// Ported from the (now-removed) Python thought_harness/value.py. Tier 2 (depends on Tier-0/1 graph + events +
// types). The math is done on UNROUNDED terms; round(x,3) is replicated only at the SAME emit
// sites Python rounds (signals + the per-branch values map) so the wire stream is byte-identical.
package value

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// pendingUserTerm is the standing V(s) contribution of an unanswered USER_INPUT line (§4.12
// interrupt policy, mandate A1). It must exceed the Controller's pursuit threshold (critic config,
// 0.4) so the term ALONE keeps a waiting user's line in the resumable frontier.
const pendingUserTerm = 0.5

// ValueSignal computes V(s) over the thought graph. It holds only the emit closure; all state is
// the graph it is handed each call (mutate-in-place via the graph's pointer maps).
type ValueSignal struct {
	emit      events.Emit
	groundGat *config.Gate // value.grounded_reward; nil ⇒ always-on (the pre-config behaviour)
	engage    engageConfig // AWAKE-DISP rung 1: the awake-user engagement value floor (default OFF ⇒ inert)
}

// engageConfig is the AWAKE-DISP rung-1 engagement floor (conscious.activity.awake_user_engage,
// docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md). When the gate is ON and the loop is awake,
// the FOCUSED (active-branch) unresolved user line's pending-user term carries an ADDITIVE boost (weight)
// on top of the standing pendingUserTerm — so the line reliably out-competes the endogenous wander in the
// frontier rerank. All fields are nil/zero by default (the wire-OFF posture), making the whole floor inert
// ⇒ byte-identical V(s). The getters read the LIVE shared config so a TUI live-flip is honoured with no
// rebuild. awake gates it to the continuous loop (the value signal is shared with reactive, which runs one
// episode per turn and must not see the boost).
type engageConfig struct {
	gate   *config.Gate   // conscious.activity.awake_user_engage; nil ⇒ inert (OFF)
	weight func() float64 // conscious.activity.awake_user_engage_weight; nil ⇒ 0
	awake  func() bool    // is the loop awake/continuous? nil ⇒ false (inert in reactive)
}

// on reports whether the engagement floor is active right now (gate ON AND the loop is awake). nil-safe.
func (e engageConfig) on() bool {
	return e.gate != nil && e.gate.Enabled() && e.awake != nil && e.awake()
}

// boost is the additive engagement weight (0 when inert / nil getter; clamped non-negative).
func (e engageConfig) boost() float64 {
	if e.weight == nil {
		return 0.0
	}
	w := e.weight()
	if w < 0 {
		return 0.0
	}
	return w
}

// New builds a ValueSignal bound to an emit closure (Python ValueSignal.__init__).
func New(emit events.Emit) *ValueSignal { return &ValueSignal{emit: emit} }

// SetAwakeEngage wires the AWAKE-DISP rung-1 engagement floor (conscious.activity.awake_user_engage).
// gate ⇒ the opt-in toggle (nil/OFF ⇒ inert); weight ⇒ the live additive-boost getter; awake ⇒ the
// "is the loop continuous?" predicate (the value signal is shared with reactive, so the boost is gated to
// the awake loop). All-nil leaves the floor inert ⇒ byte-identical V(s). The engine calls this once after
// the gates are built. Pattern-A: a pure deterministic value computation, no model call.
func (v *ValueSignal) SetAwakeEngage(gate *config.Gate, weight func() float64, awake func() bool) {
	v.engage = engageConfig{gate: gate, weight: weight, awake: awake}
}

// SetGroundedRewardGate wires the value.grounded_reward config gate (M1). nil ⇒ always-on. When the
// toggle is OFF the grounded-reward term is dropped from V(s) and the reward sum — the value signal
// falls back to its bootstrap priors only (recent-conf + goal-fit). Bypass, not delete: the wire
// stays, the signal still computes, only the grounded contribution is omitted (config.skip records it).
func (v *ValueSignal) SetGroundedRewardGate(g *config.Gate) { v.groundGat = g }

// groundedRewardOn reports whether the grounded-reward term is enabled (nil-safe ⇒ on). On a bypass it
// emits config.skip once.
func (v *ValueSignal) groundedRewardOn() bool {
	if v.groundGat.Disabled() {
		v.groundGat.Skip("grounded reward dropped")
		return false
	}
	return true
}

// Reward is the grounded reward: ONLY from reality-confirmed OBSERVATIONs (never self-graded
// traces). Mirrors Python ValueSignal.reward — the Python `isinstance(raw_return, dict) and
// raw_return.get("ok")` becomes a type-switch on the closed RawReturn union for types.Observation.
func (v *ValueSignal) Reward(g *graph.ThoughtGraph) float64 {
	// CONFIG (M1): value.grounded_reward OFF ⇒ no grounded reward accrues (the bootstrap-only posture).
	if !v.groundedRewardOn() {
		return 0.0
	}
	r := 0.0
	for _, t := range g.ActiveContext() {
		if t.Source != types.OBSERVATION {
			continue
		}
		if o, ok := t.RawReturn.(types.Observation); ok {
			if o.Ok {
				r += 1.0
			} else {
				r += -0.5
			}
		}
	}
	return r
}

// AppraiseBranch returns V(s) WITH its structured why (P6): the breakdown that produced the value,
// not just the scalar. recent_conf and goal_sim are bootstrap priors; grounded_reality is the real
// signal — it moves V up on a confirmed OBSERVATION (ok=True) and down on a refuted one. Mirrors
// Python ValueSignal.appraise_branch.
func (v *ValueSignal) AppraiseBranch(g *graph.ThoughtGraph, bid int) types.Appraisal {
	ap, _ := v.appraiseFull(g, bid)
	return ap
}

// appraiseFull computes the appraisal AND the epistemic scalar in ONE pass. The two are the same
// signal decomposed: Appraisal.Value includes the conversational-priority (pending-user) term and
// drives Branch.Value — WHICH line to pursue (rerank/frontier/prune/resume). The epistemic scalar
// excludes it — content quality alone, what filter-trust, the scheduler, and the drives consume
// (a waiting user makes a line urgent, not its thoughts more credible).
func (v *ValueSignal) appraiseFull(g *graph.ThoughtGraph, bid int) (types.Appraisal, float64) {
	var thoughts []types.Thought
	for _, t := range g.BranchThoughts(bid) {
		if t.Source != types.METACOG {
			thoughts = append(thoughts, t)
		}
	}
	if len(thoughts) == 0 {
		// Python Appraisal(...) defaults signals to a fresh {} (empty dict, non-nil); the
		// non-nil empty map is load-bearing so the emitted `signals` marshals as {} not null.
		return types.Appraisal{
			Site:          "value.branch",
			Value:         0.0,
			Reason:        "empty branch",
			Signals:       map[string]any{},
			Source:        control.Appraiser, // V(s) is the deterministic CONTROL floor (Pattern A)
			AppraiserConf: 1.0,
		}, 0.0
	}
	// recent = thoughts[-3:]; recent_conf = mean of their confidences. CPython's `sum()` is a
	// compensated (Neumaier) sum, NOT a naive left-fold, so neumaierSum reproduces it. This is NOT
	// removable parity scaffolding: the unrounded recentConf flows into value_prior and out onto the
	// wire as the seam.filter event's FULL-PRECISION confidence (round3 never absorbs it there), so a
	// 1-ULP fold divergence is observable and would change the committed goldens (verified: a naive
	// fold rewrites S2/S5/S15 by one ULP). Load-bearing for byte-identical output — keep.
	recent := thoughts
	if len(recent) > 3 {
		recent = recent[len(recent)-3:]
	}
	confs := make([]float64, len(recent))
	for i, t := range recent {
		confs[i] = t.Confidence
	}
	recentConf := neumaierSum(confs) / float64(len(recent))
	// goal_sim = _sim(thoughts[-1].text, graph.goal); the shared Jaccard consolidates _sim.
	goalSim := types.Jaccard(thoughts[len(thoughts)-1].Text, g.Goal)
	// CONFIG (M1): value.grounded_reward OFF ⇒ the grounded-reality term is dropped (bootstrap priors
	// only). On a bypass groundedRewardOn() emits config.skip once.
	grounded := 0.0
	if v.groundedRewardOn() {
		for _, t := range thoughts {
			if t.Source != types.OBSERVATION {
				continue
			}
			if o, ok := t.RawReturn.(types.Observation); ok {
				if o.Ok {
					grounded += 0.3
				} else {
					grounded += -0.2
				}
			}
		}
	}
	// A1 (§4.12, mandate 2026-06-12): an unanswered user line exerts STANDING value pressure — a
	// term inside V(s), so it survives every recompute. (Before: OnInterrupt wrote Value=1.0 and
	// this very function clobbered it back to the priors on the next Update — a one-tick blip.)
	// 0.5 clears the Controller's pursuit threshold (0.4) on its own, so a set-aside user line
	// stays resume-worthy however the bootstrap priors decay. Releases at graph.MarkDelivered.
	// AWAKE-DISP rung 1 (pendingTerm): when the engagement floor is on (awake + flag), the FOCUSED
	// user line carries an additive boost on top — byte-identical (pendingUserTerm / 0.0) when inert.
	pending := v.pendingTerm(g, bid)
	// value computed from UNROUNDED terms (math unchanged); signals rounded for display only.
	epistemic := math.Max(0.0, math.Min(1.0, 0.55*recentConf+0.35*goalSim+grounded))
	val := math.Max(0.0, math.Min(1.0, 0.55*recentConf+0.35*goalSim+grounded+pending))
	// signals keys mirror Python exactly: the recent_conf/goal_sim entries store the 0.55*/0.35*
	// SCALED terms (the contribution, not the raw prior), rounded to 3 at the emit site.
	signals := map[string]any{
		"recent_conf": round3(0.55 * recentConf),
		"goal_sim":    round3(0.35 * goalSim),
	}
	if grounded != 0 {
		signals["grounded_reality"] = round3(grounded)
	}
	if pending != 0 {
		signals["user_pending"] = round3(pending)
	}
	var reason string
	switch {
	case grounded > 0:
		reason = "reality confirmed this line"
	case grounded < 0:
		reason = "reality refuted this line"
	case pending != 0:
		reason = "user is waiting on this line"
	default:
		reason = fmt.Sprintf("bootstrap prior (conf %.2f, goal-fit %.2f)", recentConf, goalSim)
	}
	return types.Appraisal{
		Site:          "value.branch",
		Value:         val,
		Reason:        reason,
		Signals:       signals,
		Source:        control.Appraiser, // V(s) is the deterministic CONTROL floor (Pattern A)
		AppraiserConf: 1.0,
	}, epistemic
}

// BranchValue is the scalar V(s) of a branch (Python ValueSignal.branch_value).
func (v *ValueSignal) BranchValue(g *graph.ThoughtGraph, bid int) float64 {
	return v.AppraiseBranch(g, bid).Value
}

// Update recomputes V over every live branch (the rerank heuristic), writing Branch.value in place
// through the graph's pointer maps, and emits one value.update event carrying the active branch's
// V with its why (P6). Returns the bid->value map. Mirrors Python ValueSignal.update.
func (v *ValueSignal) Update(g *graph.ThoughtGraph) map[int]float64 {
	values := make(map[int]float64, len(g.Branches))
	// Iterate branches in id-ascending order (Python dict insertion order = id-ascending, bids
	// monotonic) so the in-place writes + the emitted summary's frontier_best are deterministic.
	bids := make([]int, 0, len(g.Branches))
	for bid := range g.Branches {
		bids = append(bids, bid)
	}
	sort.Ints(bids)
	for _, bid := range bids {
		ap, epistemic := v.appraiseFull(g, bid)
		g.Branches[bid].Value = ap.Value      // priority: rerank/frontier/prune/resume (incl. pending-user)
		g.Branches[bid].Epistemic = epistemic // quality: trust/scheduler/drives (urgency is not evidence)
		values[bid] = ap.Value
	}
	active := g.ActiveBranch
	ap := v.AppraiseBranch(g, active) // the active branch's V with its why (P6)

	// frontier_best = max over the non-active branches' values (Python default=0 when none).
	frontierBest := 0.0
	for _, bid := range bids {
		if bid == active {
			continue
		}
		if values[bid] > frontierBest {
			frontierBest = values[bid]
		}
	}
	summary := fmt.Sprintf("V(active b%d)=%.2f; frontier_best=%.2f", active, values[active], frontierBest)

	// values map for the wire: {"b{id}": round(v,3)} in id-ascending order. Go map iteration is
	// unordered, but JSON object key order is normalised at compare time; build deterministically.
	wireValues := make(map[string]any, len(values))
	for _, bid := range bids {
		wireValues["b"+strconv.Itoa(bid)] = round3(values[bid])
	}

	if v.emit != nil {
		v.emit(events.Value, summary, events.D{
			"active":  active,
			"values":  wireValues,
			"reward":  v.Reward(g),
			"signals": ap.Signals,
			"reason":  ap.Reason,
			// V(s) is the deterministic CONTROL floor (Pattern A): the appraiser is the control
			// floor's name (control.Appraiser — "control" since M6; was "heuristic" through M1–M5).
			"appraiser": control.Appraiser,
		})
	}
	v.emitEngage(g, values[active]) // AWAKE-DISP rung 1: witness the engagement floor when it boosted the active line
	return values
}

// emitEngage emits one conscious.engage event per rerank WHEN the AWAKE-DISP rung-1 engagement floor
// actually boosted the active line — i.e. the floor is on (gate ON + awake) AND the active branch is a
// FOCUSED unresolved user line. It is the observability contract for the value floor: the trace + TUI see
// that a user line was lifted to win the produce-competition, with the base term, the boost, and the
// resulting V(s). When the floor is inert (default OFF / reactive / no pending user) this is a complete
// no-op (no event) ⇒ byte-identical. Pattern-A: no model call. activeValue is the already-reranked V(s)
// of the active branch (the clamped, boosted value the frontier sees).
func (v *ValueSignal) emitEngage(g *graph.ThoughtGraph, activeValue float64) {
	if v.emit == nil || !v.engage.on() {
		return
	}
	ab := g.ActiveBranch
	if !g.UnresolvedUserInput(ab) {
		return // only the focused unresolved user line is boosted
	}
	boost := v.engage.boost()
	if boost == 0 {
		return // nothing to witness
	}
	v.emit(events.Engage, fmt.Sprintf("engage b%d: V(s)=%.2f (pending %.2f + boost %.2f)", ab, activeValue, pendingUserTerm, boost),
		events.D{
			"branch": ab,
			"base":   round3(pendingUserTerm),
			"boost":  round3(boost),
			"value":  round3(activeValue),
			"weight": round3(boost),
		})
}

// The fourth value site — ranking candidates (§12.2 bootstrap, propose-and-recommend) — is
// performed by the Gate directly via backend.rank in the hidden seam, so it is not re-exposed here
// to avoid implying a second, unconnected ranking path.

// neumaierSum reproduces CPython's built-in sum() over floats — Neumaier compensated summation
// (the algorithm CPython uses, NOT a naive left-fold). The compensation c captures the low-order
// bits lost at each add. This stays load-bearing after the Python ref's removal: the unrounded
// sum-of-confidences flows into value_prior and onto the wire as the seam.filter event's
// full-precision confidence (NOT round3'd there), so swapping it for a naive fold shifts that field
// by 1 ULP and rewrites the committed goldens (verified on S2/S5/S15). Keep for byte-identical output.
func neumaierSum(vals []float64) float64 {
	s := 0.0
	c := 0.0
	for _, x := range vals {
		t := s + x
		if math.Abs(s) >= math.Abs(x) {
			c += (s - t) + x
		} else {
			c += (x - t) + s
		}
		s = t
	}
	return s + c
}

// round3 quantises a float to 3 fixed decimals (round-half-to-even, as both strconv.FormatFloat
// and CPython's float __round__ use) and parses it back — the per-emit-site wire-rounding contract
// every value signal is pinned to (the sink does NOT round). +∞/NaN pass through unchanged. This is
// the live wire quantum, not parity scaffolding — the goldens are minted from these rounded values.
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return r
}
