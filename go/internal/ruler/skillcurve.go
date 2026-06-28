package ruler

// skillcurve.go — A3: characterize the Skill-Miner SELF-IMPROVEMENT CURVE on the COST + CAPABILITY axes,
// reusing the W5-1 ruler machinery (no new statistics — the curve is reduced to the SAME within-cohort
// cost-σ / feasibility gate the campaign already trusts).
//
// WHAT THE CURVE IS (vs the campaign K-replay aggregates the ruler usually eats). The campaign ruler
// characterizes per-TASK K-replay rows (Characterize over []TaskReplays). The A3 curve is a different shape:
// one cost sample per EXPOSURE of a recurring goal family, in stream order, where a minted skill begins to
// RECALL (short-circuit synthesis) partway through. The "does the curve bend DOWN?" question is a two-cohort
// comparison: the PRE-RECALL exposures (full synthesis) vs the POST-RECALL exposures (recall short-circuits).
//
// THE REDUCTION (faithful to the cost-σ contract). Split the curve at the first RECALLED exposure into:
//   - PRE cohort  = exposures [0 .. firstRecall)   — synthesis still runs (the expensive arm).
//   - POST cohort = exposures [firstRecall .. N)    — recall short-circuits synthesis (the cheap arm).
// Each cohort's per-exposure completion vector becomes a ruler TaskReplays.Completions (the cohort is one
// "task" measured over its exposures = "replays"), so the ruler recovers the WITHIN-cohort cost-σ noise
// floor EXACTLY as it does for a K-replay task. The cohort MEAN delta (pre − post) is the curve's BEND; it
// is "floor-clearing" iff it exceeds the cost MDE / the within-cohort 2σ band. This re-uses Characterize —
// no bespoke statistics, so the curve claim is gated by the same pre-registered feasibility constants.
//
// HONEST SCOPE. Offline (test double) every completion is 0 → CostVerdict DEGENERATE (no usage to
// characterize) — the offline test proves the SHAPE/wiring (a recall cohort exists, the split is correct),
// not the magnitude. On claude the cohort completions are real and the cost verdict is failable. At small N
// (few exposures per cohort) the within-cohort cost-σ is large and the delta sits BELOW the floor (the
// W5-2c n=5 finding) — Characterize reports COST-NOISY honestly; a floor-clearing claim needs more exposures.

import (
	"github.com/berttrycoding/thought-harness/internal/campaign"
)

// CurveCharacterization is the A3 curve read: the cohort split, the per-cohort cost means + faculty rates,
// the COST bend (pre − post mean completion) with its ruler verdict, and the capability (faculty) direction.
// It is deterministic in the input curve points.
type CurveCharacterization struct {
	// Exposures is N — the number of points in the stream.
	Exposures int
	// FirstRecallExposure is the 0-based exposure at which recall first fired (-1 if recall NEVER fired —
	// the flat-curve failure mode the mutation test catches: no mint/no recall ⇒ no bend).
	FirstRecallExposure int
	// FirstMintExposure is the 0-based exposure at which the idle Consolidate first MINTED a skill (-1 if
	// it never minted). Recall fires on exposures AFTER this; the inflection point.
	FirstMintExposure int

	// --- COST axis (PRIMARY, cost-reliable) ---

	// PreMeanCompletion / PostMeanCompletion are the mean completion tokens per exposure in the PRE-recall
	// and POST-recall cohorts. 0 offline (no usage).
	PreMeanCompletion  float64
	PostMeanCompletion float64
	// CostBend is PreMean − PostMean: POSITIVE means the curve BENT DOWN (post cheaper — the W5 win).
	// 0 offline; the real magnitude is the claude curve.
	CostBend float64
	// CostFloorCleared is true when the bend exceeds the ruler's cost noise floor (the within-cohort 2σ
	// band AND a non-DEGENERATE/non-NOISY cost verdict) — the floor-clearing test the W5 DoD demands.
	CostFloorCleared bool
	// Cost is the full ruler Characterization of the two cohorts on the COST axis (the within-cohort cost-σ,
	// the cost MDE, the COST-RELIABLE/NOISY/DEGENERATE verdict). The keep-gate reads Cost.CostVerdict.
	Cost Characterization

	// --- CAPABILITY axis (caveated, saturated) ---

	// PreFireRate / PostFireRate are the faculty fire fractions in each cohort (only meaningful when the
	// curve's points carry a faculty verdict; 0 otherwise).
	PreFireRate  float64
	PostFireRate float64
	// FacultyDirection is "+" (post fires more — directional capability lift), "-" (post fires less),
	// or "flat". CAVEATED: the binary axis is knife-edge on the saturated suite — never a clean strict-`>`.
	FacultyDirection string

	// --- utility (held-positive check) ---

	// PreSolvedRate / PostSolvedRate are the solved (oracle/grounded) fractions per cohort — the held-utility
	// check (a cost win is only meaningful if utility did not collapse on the cheap arm).
	PreSolvedRate  float64
	PostSolvedRate float64
	// GroundedAny is true if ANY exposure grounded (the cost number is at held-positive utility, not the
	// W5-2b zero-grounding caveat).
	GroundedAny bool
}

// CharacterizeCurve reduces an A3 skill curve into the two-cohort cost + capability read, gating the cost
// bend on the W5-1 ruler. Pure/deterministic in the points + opts. A curve with NO recall (the mint never
// fired or never recalled) returns FirstRecallExposure=-1, both cohorts = the whole stream, CostBend≈0 —
// the honest flat-curve verdict (the mutation-test failure mode).
func CharacterizeCurve(points []campaign.CurvePoint, opts Options) CurveCharacterization {
	cc := CurveCharacterization{
		Exposures:           len(points),
		FirstRecallExposure: -1,
		FirstMintExposure:   -1,
	}
	if len(points) == 0 {
		return cc
	}
	// locate the first mint + the first recall (the cohort split is at the first RECALL — that is when the
	// cost actually drops; the mint is the cause, the recall is the observable cost effect).
	for _, p := range points {
		if p.Minted && cc.FirstMintExposure < 0 {
			cc.FirstMintExposure = p.Exposure
		}
		if p.Recalled && cc.FirstRecallExposure < 0 {
			cc.FirstRecallExposure = p.Exposure
		}
		if p.Grounded {
			cc.GroundedAny = true
		}
	}

	split := cc.FirstRecallExposure
	if split < 0 {
		// no recall ever — the whole stream is the PRE cohort; POST is empty; bend is 0 (flat).
		split = len(points)
	}
	pre := points[:split]
	post := points[split:]

	preComp, preFire, preSolved, preFireScored := cohortStats(pre)
	postComp, postFire, postSolved, postFireScored := cohortStats(post)

	cc.PreMeanCompletion = meanInt(preComp)
	cc.PostMeanCompletion = meanInt(postComp)
	// CostBend is the down-bend (pre − post) ONLY when there IS a recall cohort to bend INTO. With no recall
	// (post cohort empty) the curve is FLAT by definition — bend 0 — not "pre minus an empty mean of 0"
	// (which would spuriously read the whole pre-cohort cost as a bend). The honest flat verdict.
	if len(post) == 0 {
		cc.CostBend = 0
	} else {
		cc.CostBend = cc.PreMeanCompletion - cc.PostMeanCompletion
	}
	cc.PreFireRate = preFire
	cc.PostFireRate = postFire
	cc.PreSolvedRate = preSolved
	cc.PostSolvedRate = postSolved

	// faculty direction (caveated — saturated suite, knife-edge). Scored iff BOTH cohorts carry a faculty
	// signature AND have exposures (a cohort that named a Signature was scored even if it fired 0 times).
	switch {
	case len(post) == 0 || !preFireScored || !postFireScored:
		cc.FacultyDirection = "flat" // faculty not scored, or no recall cohort to compare
	case postFire > preFire:
		cc.FacultyDirection = "+"
	case postFire < preFire:
		cc.FacultyDirection = "-"
	default:
		cc.FacultyDirection = "flat"
	}

	// COST gate: characterize the two cohorts as two ruler "tasks" whose Completions are the per-exposure
	// cost samples. Reuses Characterize (the SAME within-cohort cost-σ + cost MDE + COST-RELIABLE verdict).
	rows := []TaskReplays{
		cohortRow("pre-recall", preComp),
		cohortRow("post-recall", postComp),
	}
	cc.Cost = Characterize(rows, opts)
	// floor-cleared: the bend is DOWN (post cheaper) AND the cost instrument is exercised+reliable AND the
	// bend exceeds the within-cohort 2σ band (the cost noise floor). DEGENERATE/NOISY ⇒ not cleared (honest
	// at small N / offline).
	cc.CostFloorCleared = cc.CostBend > 0 &&
		cc.Cost.CostVerdict == CostReliable &&
		cc.CostBend > cc.Cost.CostBandHalfWidth

	return cc
}

// cohortStats reduces a cohort of curve points to its completion vector + faculty/solved rates. fireScored
// is false when NO point in the cohort carries a faculty signal (so the rate is meaningless, not 0).
func cohortStats(points []campaign.CurvePoint) (comp []int, fireRate, solvedRate float64, fireScored bool) {
	if len(points) == 0 {
		return nil, 0, 0, false
	}
	comp = make([]int, 0, len(points))
	var fires, solves, scored int
	for _, p := range points {
		comp = append(comp, p.Completion)
		if p.Solved {
			solves++
		}
		if p.Signature != "" { // the faculty axis WAS scored on this exposure (named a signature)
			scored++
			if p.Fired {
				fires++
			}
		}
	}
	// fireScored: at least one exposure named a faculty Signature (the cognition axis was scored). The
	// fire RATE is over the scored exposures only — distinguishing "scored, fired 0 times" (rate 0, scored)
	// from "not scored" (rate meaningless). A cohort that named a Signature but fired 0 is still SCORED.
	fireScored = scored > 0
	rate := 0.0
	if scored > 0 {
		rate = float64(fires) / float64(scored)
	}
	return comp, rate, float64(solves) / float64(len(points)), fireScored
}

// cohortRow builds a ruler TaskReplays for one cohort: the cohort is one "task" whose per-exposure
// completions are the K "replays", so Characterize recovers the within-cohort cost-σ. Success/Replays are
// the binary axis (unused for the cost gate but kept honest — Success counts the non-zero-cost exposures,
// a degenerate placeholder offline where all are 0).
func cohortRow(id string, comp []int) TaskReplays {
	r := TaskReplays{ID: id, Replays: len(comp), Completions: comp}
	var sum int
	for _, c := range comp {
		sum += c
	}
	if len(comp) > 0 {
		r.MeanCompletion = float64(sum) / float64(len(comp))
	}
	return r
}

func meanInt(xs []int) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum int
	for _, x := range xs {
		sum += x
	}
	return float64(sum) / float64(len(xs))
}
