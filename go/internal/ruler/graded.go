package ruler

// graded.go — GATE-2 instrument: the GRADED faculty-signature characterization.
//
// THE PROBLEM the binary axis cannot solve. The cognition probe's faculty signatures are
// BINARY (fired / not-fired). On the saturated claude substrate a binary signature flips
// run-to-run (the cognition-probe-001 suite: faculty ~82%, soft on/off 80%≈indistinguishable;
// the binary signature is ~saturated AND ~4/5-unstable) — so a binary signature cannot detect a
// SUBTLE faculty-engagement lift: the signal is buried in run-to-run flipping. The graded
// aptness score (campaign.CogStability.Aptness — degree of faculty engagement on [0,1], not a
// 0/1 flip) is the proposed finer instrument. This file characterizes its noise floor: is the
// graded signature actually FINER (smaller within-task σ relative to between-task spread) than
// the binary one, so a faculty-engagement delta would be DETECTABLE?
//
// THE READ. Over per-task per-replay graded vectors it recovers:
//   - SigmaWithin: the pooled within-task SD of the graded score across the K replays (the
//     graded noise floor — the run-to-run wobble of a fixed task's engagement degree).
//   - BetweenSD: the SD across tasks of the per-task mean graded score (the real
//     faculty-difference signal — do harder tasks engage the faculty MORE than easier ones).
//   - ICC: the fraction of variance that is real between-task difference (same one-way
//     random-effects ICC(1) as the binary axis, on the continuous score).
//   - MDE: the smallest graded-score lift the instrument can resolve at the planned N/K.
//   - Verdict: GRADED-CLEARS (the graded signature clears its own noise floor — between-task
//     signal present AND MDE under a claimable graded lift AND ICC over the floor → a faculty
//     delta is detectable → suite (a) worth building) vs GRADED-RESATURATES (even graded +
//     harder, the between-task spread is swamped by within-task noise OR there is no between-task
//     variance at all → the frontier re-saturates the graded signature too → CAPABILITY-IS-W6-ONLY)
//     vs GRADED-DEGENERATE (not enough exercised data — e.g. the offline test double where every
//     replay is identical so there is no noise to characterize).
//
// Determinism: pure arithmetic over the input vectors; no RNG / clock / I/O.

import (
	"math"

	"github.com/berttrycoding/thought-harness/internal/campaign"
)

// DefaultGradedClaimableLift is the graded-score lift the campaign would want to claim — the
// graded MDE must come in UNDER it for the instrument to resolve a faculty-engagement delta. The
// graded score is on [0,1] like the binary rate, so it matches the binary DefaultClaimableLift
// (0.15): an instrument that cannot detect a 0.15 graded-engagement change at the planned N is
// too coarse for the eval's faculty-scaling claims.
const DefaultGradedClaimableLift = 0.15

// GradedTask is ONE task's K-replay GRADED faculty-engagement vector — the gate-2 analogue of
// the binary TaskReplays. Scores is the per-replay aptness on [0,1] (length K once the run
// completes); the ruler reduces it into the within/between/ICC/MDE read.
type GradedTask struct {
	// ID identifies the task (Goal/Signature).
	ID string
	// Scores is the per-replay graded faculty-engagement vector on [0,1].
	Scores []float64
}

// FromCogGraded adapts the cognition replay aggregates' per-replay graded aptness vector into
// the graded ruler's rows. CogStability.Aptness is the per-replay [0,1] engagement vector.
func FromCogGraded(rows []campaign.CogStability) []GradedTask {
	out := make([]GradedTask, len(rows))
	for i, r := range rows {
		out[i] = GradedTask{ID: r.Goal, Scores: append([]float64(nil), r.Aptness...)}
	}
	return out
}

// GradedVerdict is the one-line read of the graded faculty instrument.
type GradedVerdict string

const (
	// GradedClears — the graded signature clears its own noise floor: there IS between-task
	// faculty-difference signal, the ICC is over the floor, AND the graded MDE comes in under the
	// claimable graded lift. A faculty-engagement delta is detectable at this N/K → the harder
	// suite resolves faculty differentiation the binary/easy probe cannot → suite (a) worth building.
	GradedClears GradedVerdict = "GRADED-CLEARS"
	// GradedResaturates — the graded signature does NOT clear: either there is no between-task
	// variance (every task engages the faculty to the same degree — the frontier re-saturates the
	// graded score too), or the within-task graded noise swamps the between-task spread (ICC under
	// floor / MDE over the claimable lift). CAPABILITY-IS-W6-ONLY on this axis.
	GradedResaturates GradedVerdict = "GRADED-RESATURATES"
	// GradedDegenerate — not enough exercised data to characterize (N<2 tasks, K<2 replays, or zero
	// variance everywhere — e.g. the deterministic test double). The graded instrument has not been
	// exercised on a non-deterministic substrate yet.
	GradedDegenerate GradedVerdict = "GRADED-DEGENERATE"
)

// GradedCharacterization is the full graded-instrument read over a set of per-task per-replay
// graded vectors. Everything is deterministic in the input.
type GradedCharacterization struct {
	// Tasks is the number of distinct tasks (N); K is the replay count.
	Tasks int
	K     int

	// Mean is the grand mean graded score (across all tasks' means) — the saturation level (near
	// 1.0 = the faculty engages fully everywhere, a saturation warning even if variance exists).
	Mean float64
	// SigmaWithin is the pooled within-task SD of the graded score across the K replays (the graded
	// noise floor: a fixed task's run-to-run engagement wobble).
	SigmaWithin float64
	// SigmaWithinAveraged is SigmaWithin/√K — the noise on the per-task MEAN graded estimate after
	// K-averaging.
	SigmaWithinAveraged float64
	// BandHalfWidth is ±2·SigmaWithinAveraged — a per-task graded delta inside this is
	// indistinguishable from replay noise.
	BandHalfWidth float64
	// BetweenSD is the SD across tasks of the per-task mean graded score (the real faculty-difference
	// signal). NonZero between-SD with small within-SD is the detectability condition.
	BetweenSD float64
	// BetweenVar is BetweenSD² (the ICC between-task variance input).
	BetweenVar float64
	// WithinVar is SigmaWithin² (the ICC within-task variance input).
	WithinVar float64
	// ICC is the one-way random-effects ICC(1) on the continuous graded score — the fraction of
	// variance that is real between-task difference.
	ICC float64
	// MDE is the smallest graded-score lift resolvable at this N/K (α=0.05, power=0.80, two-arm
	// paired-by-task), in graded-score units.
	MDE float64

	// ICCFloor / ClaimableLift are the thresholds the gate ran against (echoed for the report).
	ICCFloor      float64
	ClaimableLift float64
	// Feasible is the gate: between-task signal present AND ICC ≥ floor AND MDE ≤ claimable lift.
	Feasible bool
	// Verdict is the one-line read (GRADED-CLEARS / GRADED-RESATURATES / GRADED-DEGENERATE).
	Verdict GradedVerdict
}

// CharacterizeGraded reduces the per-task per-replay graded vectors into the full graded-
// instrument read and applies the gate-2 verdict. Pure and deterministic.
//
// opts reuse the binary Options shape (ICCFloor); the graded claimable-lift uses
// DefaultGradedClaimableLift unless ClaimableLift is overridden (>0).
func CharacterizeGraded(rows []GradedTask, opts Options) GradedCharacterization {
	claimable := DefaultGradedClaimableLift
	if opts.ClaimableLift > 0 {
		claimable = opts.ClaimableLift
	}
	c := GradedCharacterization{
		Tasks:         len(rows),
		ICCFloor:      opts.iccFloor(),
		ClaimableLift: claimable,
	}
	// Common K (taken from the first non-empty vector).
	for _, r := range rows {
		if len(r.Scores) > 0 {
			c.K = len(r.Scores)
			break
		}
	}

	var taskMeans []float64
	var withinVarSum float64
	var withinTaskN int
	for _, r := range rows {
		if len(r.Scores) == 0 {
			continue
		}
		taskMeans = append(taskMeans, mean(r.Scores))
		if len(r.Scores) >= 2 {
			withinVarSum += sampleVar(r.Scores)
			withinTaskN++
		}
	}

	c.Mean = mean(taskMeans)
	if withinTaskN > 0 {
		c.WithinVar = withinVarSum / float64(withinTaskN)
		c.SigmaWithin = math.Sqrt(c.WithinVar)
	}
	if c.K > 0 {
		c.SigmaWithinAveraged = c.SigmaWithin / math.Sqrt(float64(c.K))
	} else {
		c.SigmaWithinAveraged = c.SigmaWithin
	}
	c.BandHalfWidth = 2 * c.SigmaWithinAveraged

	c.BetweenVar = sampleVar(taskMeans)
	c.BetweenSD = math.Sqrt(c.BetweenVar)

	// ICC(1) on the continuous score. The within-task estimator is already the unbiased sample
	// variance (sampleVar uses n−1), so — unlike the binary p(1-p) path — no k/(k−1) correction is
	// applied: MSW = mean within-task sample variance directly; MSB = k·between-task variance.
	c.ICC = computeICC1Continuous(c.BetweenVar, c.WithinVar, c.K)

	// MDE on the graded score axis: two-arm, paired-by-task, K-averaged per-arm.
	if c.Tasks > 0 && c.SigmaWithinAveraged > 0 {
		c.MDE = (zAlpha + zBeta) * c.SigmaWithinAveraged * math.Sqrt(2/float64(c.Tasks))
	}

	c.Verdict, c.Feasible = deriveGradedVerdict(c)
	return c
}

// CharacterizeCogGraded is the convenience entrypoint straight off the cognition replay
// aggregates (no manual FromCogGraded needed).
func CharacterizeCogGraded(rows []campaign.CogStability, opts Options) GradedCharacterization {
	return CharacterizeGraded(FromCogGraded(rows), opts)
}

// computeICC1Continuous is ICC(1) on a CONTINUOUS per-replay score: the within-task MSW is the
// mean of the (already unbiased, n−1) within-task sample variances, so no p(1-p) correction is
// applied. MSB = k·Var(per-task means). Clamped to [0,1]; degenerate (k<2 or denom≤0) → 0.
func computeICC1Continuous(betweenVar, withinVar float64, k int) float64 {
	if k < 2 {
		return 0
	}
	kf := float64(k)
	msb := kf * betweenVar
	msw := withinVar // already the unbiased within-task sample variance
	denom := msb + (kf-1)*msw
	if denom <= 0 {
		return 0
	}
	icc := (msb - msw) / denom
	if icc < 0 {
		return 0
	}
	if icc > 1 {
		return 1
	}
	return icc
}

// deriveGradedVerdict applies the gate-2 verdict. DEGENERATE first (under-2 tasks/replays, or no
// variance ANYWHERE — every replay of every task identical, the test-double / fully-saturated
// state). Otherwise the graded instrument CLEARS only when ALL THREE hold: there is real
// between-task faculty-difference signal (BetweenSD > 0), the ICC clears the reliability floor,
// AND the graded MDE comes in under the claimable graded lift. Any failure → RESATURATES (the
// frontier re-saturates the graded signature too, on this axis).
func deriveGradedVerdict(c GradedCharacterization) (GradedVerdict, bool) {
	if c.Tasks < 2 || c.K < 2 || (c.WithinVar == 0 && c.BetweenVar == 0) {
		return GradedDegenerate, false
	}
	if c.BetweenSD == 0 || c.ICC < c.ICCFloor || c.MDE > c.ClaimableLift {
		return GradedResaturates, false
	}
	return GradedClears, true
}
