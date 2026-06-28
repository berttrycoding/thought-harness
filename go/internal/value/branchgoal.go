// Per-branch goal-relative + goalless (intrinsic) value — slice (e) of the conscious redesign
// (docs/cognition/02-conscious.md §1.8 "the forest" + §3 "per-branch + goalless"). PROPOSED,
// opt-in: these are NEW exported entry points the engine can select per branch (G5 per-line goal
// binding). They are ADDITIVE — the default AppraiseBranch / appraiseFull path in value.go is
// untouched and byte-identical. Nothing here emits an event (no new event kinds); they are pure,
// read-only appraisals the caller decides what to do with.
//
// Two paths, one shape (§3): when a branch carries a setpoint, value is extrinsic — goal_sim against
// THAT branch's goal (BranchValueForGoal). When a branch is goalless (wandering, default-mode), the
// goal_sim slot is replaced by INTRINSIC drivers — curiosity/novelty + coherence (IntrinsicValue).
// recent_conf, grounded-reality and the pending-user term are shared by both (only the middle
// 0.35-weighted slot differs), so a wandering line is still a usable best-first search heuristic.
package value

import (
	"fmt"
	"math"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// NewSig builds a ValueSignal with no emit closure — for the opt-in per-branch/intrinsic appraisals,
// which are read-only and emit nothing (New(nil) equivalently). Convenience for callers that only
// want the new goalless/goal-relative computations without wiring a bus.
func NewSig() *ValueSignal { return New(nil) }

// nonMetacog materialises a branch's non-METACOG thoughts (the same filter AppraiseBranch applies):
// METACOG thoughts are graph bookkeeping, not content, so they never enter the value math.
func nonMetacog(g *graph.ThoughtGraph, bid int) []types.Thought {
	var thoughts []types.Thought
	for _, t := range g.BranchThoughts(bid) {
		if t.Source != types.METACOG {
			thoughts = append(thoughts, t)
		}
	}
	return thoughts
}

// emptyAppraisal is the shared zero-value appraisal for a branch with no content thoughts — byte-
// identical in shape to appraiseFull's empty-branch return (Site/Source/non-nil empty Signals).
func emptyAppraisal() types.Appraisal {
	return types.Appraisal{
		Site:          "value.branch",
		Value:         0.0,
		Reason:        "empty branch",
		Signals:       map[string]any{},
		Source:        control.Appraiser,
		AppraiserConf: 1.0,
	}
}

// recentConf reproduces appraiseFull's recent-confidence term: the Neumaier-compensated mean of the
// last <=3 thoughts' confidences. Shared so both new paths use the identical bootstrap prior (the
// neumaierSum is load-bearing for byte-identical output — see value.go).
func recentConf(thoughts []types.Thought) float64 {
	recent := thoughts
	if len(recent) > 3 {
		recent = recent[len(recent)-3:]
	}
	confs := make([]float64, len(recent))
	for i, t := range recent {
		confs[i] = t.Confidence
	}
	return neumaierSum(confs) / float64(len(recent))
}

// groundedTerm reproduces appraiseFull's reality term: +0.3 per confirmed OBSERVATION, -0.2 per
// refuted one — gated by the same value.grounded_reward toggle. The ONLY reality-grounded term.
func (v *ValueSignal) groundedTerm(thoughts []types.Thought) float64 {
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
	return grounded
}

// pendingTerm is the standing pending-user pressure (A1): +pendingUserTerm when the branch holds an
// unanswered USER_INPUT line — PLUS the AWAKE-DISP rung-1 engagement boost on the FOCUSED (active) user
// line. The boost (engage.boost()) is applied ONLY when the engagement floor is on (gate ON + awake) AND
// this is the active branch's unresolved user line, so a focused awake user input reliably out-competes
// the endogenous wander in the frontier rerank. When the floor is inert (default OFF / reactive) the boost
// is 0, so this returns exactly pendingUserTerm / 0.0 — byte-identical to the standing A1 behaviour. The
// caller (appraiseFull / BranchValueForGoal / IntrinsicValue) clamps the summed value into [0,1].
func (v *ValueSignal) pendingTerm(g *graph.ThoughtGraph, bid int) float64 {
	if !g.UnresolvedUserInput(bid) {
		return 0.0
	}
	pending := pendingUserTerm
	// AWAKE-DISP rung 1: a FOCUSED unresolved user line gets the additive engagement boost so the mind
	// pursues it over self-directed wander. Active-branch-only — a SET-ASIDE user line keeps the standing
	// pendingUserTerm (so it stays resumable) but does not get the extra pursuit weight; the floor lifts
	// the line the user is waiting on RIGHT NOW.
	if v.engage.on() && bid == g.ActiveBranch {
		pending += v.engage.boost()
	}
	return pending
}

// BranchValueForGoal computes V(s) with goal_sim measured against a PER-BRANCH goal string instead
// of the single global graph.Goal (§1.8 G5 "per-branch goal binding"). It is byte-identical to the
// default appraiseFull path when goal == g.Goal — including the empty-string fallback: an empty goal
// means "this branch has no per-branch override," so it falls back to the graph goal.
//
// This lets the forest evaluate each line against its OWN setpoint: the same thought is close to one
// branch's goal and far from another's. The math, weights, signals keys, reasons, Site and Source all
// match the default V(s) exactly — only the goal_sim reference changes.
func (v *ValueSignal) BranchValueForGoal(g *graph.ThoughtGraph, bid int, goal string) types.Appraisal {
	if goal == "" {
		goal = g.Goal // fall back to the graph goal — equivalent to the default path
	}
	thoughts := nonMetacog(g, bid)
	if len(thoughts) == 0 {
		return emptyAppraisal()
	}
	rc := recentConf(thoughts)
	goalSim := types.Jaccard(thoughts[len(thoughts)-1].Text, goal)
	grounded := v.groundedTerm(thoughts)
	pending := v.pendingTerm(g, bid)

	val := clamp01(0.55*rc + 0.35*goalSim + grounded + pending)
	signals := map[string]any{
		"recent_conf": round3(0.55 * rc),
		"goal_sim":    round3(0.35 * goalSim),
	}
	if grounded != 0 {
		signals["grounded_reality"] = round3(grounded)
	}
	if pending != 0 {
		signals["user_pending"] = round3(pending)
	}
	return types.Appraisal{
		Site:          "value.branch",
		Value:         val,
		Reason:        goalRelativeReason(grounded, pending, rc, goalSim),
		Signals:       signals,
		Source:        control.Appraiser,
		AppraiserConf: 1.0,
	}
}

// goalRelativeReason mirrors appraiseFull's reason ladder for the goal-relative path (the
// bootstrap-prior wording reports the per-branch goal-fit).
func goalRelativeReason(grounded, pending, rc, goalSim float64) string {
	switch {
	case grounded > 0:
		return "reality confirmed this line"
	case grounded < 0:
		return "reality refuted this line"
	case pending != 0:
		return "user is waiting on this line"
	default:
		return fmt.Sprintf("bootstrap prior (conf %.2f, goal-fit %.2f)", rc, goalSim)
	}
}

// IntrinsicValue computes the GOALLESS / wandering value for a branch with no setpoint (§1.8
// "goalless lines = wandering"): the 0.35-weighted goal_sim slot is replaced by INTRINSIC drivers,
// so the value has no goal_sim term at all. recent_conf, grounded-reality and pending-user are kept
// (a wandering line that touches reality or holds a waiting user still earns those), so the value
// stays a usable best-first heuristic — only the middle slot is intrinsic, not extrinsic.
//
// Intrinsic = mean(novelty, coherence), deterministic proxies over the branch's own thoughts:
//   - novelty  (curiosity): how much fresh vocabulary the line keeps introducing vs what it has
//     already explored — high when each thought brings new words, low when it repeats itself.
//   - coherence: how well the line knits its pieces together — high when each thought shares
//     vocabulary with the line so far, low when fragments are disjoint.
//
// Both are an open set (§1.8 "extensible"); conflict-resolution is a future driver, not added here.
func (v *ValueSignal) IntrinsicValue(g *graph.ThoughtGraph, bid int) types.Appraisal {
	thoughts := nonMetacog(g, bid)
	if len(thoughts) == 0 {
		return emptyAppraisal()
	}
	rc := recentConf(thoughts)
	novelty := noveltyTerm(thoughts)
	coherence := coherenceTerm(thoughts)
	intrinsic := 0.5 * (novelty + coherence)
	grounded := v.groundedTerm(thoughts)
	pending := v.pendingTerm(g, bid)

	val := clamp01(0.55*rc + 0.35*intrinsic + grounded + pending)
	signals := map[string]any{
		"recent_conf": round3(0.55 * rc),
		"novelty":     round3(novelty),
		"coherence":   round3(coherence),
		"intrinsic":   round3(0.35 * intrinsic),
	}
	if grounded != 0 {
		signals["grounded_reality"] = round3(grounded)
	}
	if pending != 0 {
		signals["user_pending"] = round3(pending)
	}
	return types.Appraisal{
		Site:          "value.branch",
		Value:         val,
		Reason:        intrinsicReason(grounded, pending, novelty, coherence),
		Signals:       signals,
		Source:        control.Appraiser, // still the deterministic CONTROL floor (Pattern A)
		AppraiserConf: 1.0,
	}
}

// intrinsicReason reports the dominant wandering driver (or the shared reality/pending wording).
func intrinsicReason(grounded, pending, novelty, coherence float64) string {
	switch {
	case grounded > 0:
		return "reality confirmed this line"
	case grounded < 0:
		return "reality refuted this line"
	case pending != 0:
		return "user is waiting on this line"
	case novelty >= coherence:
		return fmt.Sprintf("wandering: novelty-led (novelty %.2f, coherence %.2f)", novelty, coherence)
	default:
		return fmt.Sprintf("wandering: coherence-led (novelty %.2f, coherence %.2f)", novelty, coherence)
	}
}

// noveltyTerm rewards a line that keeps introducing NEW vocabulary (curiosity, §1.8). For each
// thought after the first, the fraction of its words NOT seen in any prior thought of the line; the
// mean over those steps. A single thought has no prior to be novel against -> 1.0 (fully fresh by
// definition). Deterministic, seeded-RNG-free (pure set arithmetic).
func noveltyTerm(thoughts []types.Thought) float64 {
	if len(thoughts) <= 1 {
		return 1.0
	}
	seen := wordSetOf(thoughts[0].Text)
	sum := 0.0
	steps := 0
	for i := 1; i < len(thoughts); i++ {
		words := wordSetOf(thoughts[i].Text)
		if len(words) == 0 {
			// an empty thought introduces nothing; treat as fully un-novel so padding a line with
			// blanks does not inflate curiosity.
			steps++
			continue
		}
		fresh := 0
		for w := range words {
			if _, ok := seen[w]; !ok {
				fresh++
			}
		}
		sum += float64(fresh) / float64(len(words))
		steps++
		for w := range words {
			seen[w] = struct{}{}
		}
	}
	if steps == 0 {
		return 0.0
	}
	return sum / float64(steps)
}

// coherenceTerm rewards a line that knits its pieces together (§1.8): for each thought after the
// first, its word-overlap (Jaccard) with the accumulated vocabulary of the line so far; the mean
// over those steps. A single thought is trivially coherent with itself -> 1.0. Deterministic.
func coherenceTerm(thoughts []types.Thought) float64 {
	if len(thoughts) <= 1 {
		return 1.0
	}
	accum := thoughts[0].Text
	sum := 0.0
	steps := 0
	for i := 1; i < len(thoughts); i++ {
		sum += types.Jaccard(thoughts[i].Text, accum)
		accum += " " + thoughts[i].Text
		steps++
	}
	if steps == 0 {
		return 0.0
	}
	return sum / float64(steps)
}

// wordSetOf lower-cases and splits text into a word set — the same tokenisation types.Jaccard uses
// (strings.Fields over the lower-cased text), kept here so novelty's set arithmetic matches it.
func wordSetOf(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, f := range strings.Fields(strings.ToLower(s)) {
		set[f] = struct{}{}
	}
	return set
}

// clamp01 clamps to [0,1] (the same bound appraiseFull applies to V(s)).
func clamp01(x float64) float64 { return math.Max(0.0, math.Min(1.0, x)) }
