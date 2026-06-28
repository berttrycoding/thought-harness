// Package ruler is the W5-1 MEASUREMENT-SOUNDNESS gate: a pure statistical
// function that characterizes the campaign's cost/solve INSTRUMENT before any
// lift number is trusted (docs/internal/notes/2026-06-18-w5-scaling-plan.md §W5-1;
// docs/internal/notes/2026-06-15-registry-scaling-research.md §7.1 the feasibility-gate
// discipline; the Phase-0 verifier-characterization discipline).
//
// THE CONTRACT. On a non-deterministic substrate (claude) a task that solves
// run-to-run-flips is instrument NOISE, not signal. Before a campaign claims a
// strict-`>` lift, the ruler must answer three questions over already-collected
// K-replay data (it RE-RUNS NOTHING — it is a deterministic reduction over the
// existing campaign aggregates ProbeStability / CogStability):
//
//   - sigma_noise: how much does a per-task outcome wobble across replays? (the
//     pooled within-task SD of the 0/1 outcome indicator, matching
//     internal/bench/eval's Phase-0 σ_noise convention).
//   - ICC (reliability): how much of the variance is REAL task-difference vs
//     replay noise? An instrument whose between-task variance is swamped by
//     within-task replay variance cannot rank one config above another no matter
//     how big N is — the signal is noise. ICC(1) (one-way random-effects
//     intraclass correlation) is the standard answer.
//   - the FEASIBILITY GATE: is the instrument discriminating enough to trust a
//     strict-`>` lift at this K? Two clauses, BOTH must hold: ICC ≥ a reliability
//     floor AND the minimum detectable effect (MDE) at the planned N is smaller
//     than the lift you would want to claim. If the gate FAILS the ruler is
//     REJECTED, not patched (research §7.1) — the campaign must raise K / N or
//     lower temperature before any number off this instrument counts.
//
// THE TWO COST REGIMES (W5-1 cost-axis gap closed). The binary axis
// (solved-rate / fire-rate) is characterized EXACTLY from the aggregates: for a
// 0/1 indicator the within-task variance is p(1-p), recoverable from the
// solved-count alone — no per-replay vector is needed. The COMPLETION-TOKEN axis
// depends on what the instrument retained:
//
//   - PER-REPLAY VECTOR PRESENT (TaskReplays.Completions non-empty — the W5-1
//     instrument change ProbeReplays/CognitionProbeReplays now store the
//     per-replay completion vector): the ruler recovers the TRUE within-task
//     cost-σ (the SD of completion tokens across the K replays, averaged over
//     tasks), the K-averaged cost noise band, and a cost MDE — a real, failable
//     cost noise floor. CostWithinFloorAvailable is set true and a cost-axis
//     verdict (DEGENERATE / NOISY / RELIABLE) is produced alongside the binary
//     verdict. On the offline test double every replay has completion=0 → cost-σ=0
//     → the cost verdict is DEGENERATE (honest: no real usage to characterize).
//   - SUM-ONLY (legacy aggregates / Completions empty): the ruler computes only
//     the BETWEEN-task dispersion of the cost means (a coarse band) and CANNOT
//     recover the within-task token noise floor. CostWithinFloorAvailable stays
//     false; the cost axis is DESCRIPTIVE, never treated as a characterized floor.
//
// The PRIMARY feasibility gate runs on the BINARY axis (exact, unchanged) in
// EITHER regime — the cost-axis verdict is reported alongside it, never replacing
// it as the keep-gate.
//
// Determinism: pure arithmetic over the input slice; no RNG, no wall clock, no
// I/O (CLAUDE.md headless-pure + determinism rules). Same input ⇒ same verdict.
package ruler

import (
	"math"

	"github.com/berttrycoding/thought-harness/internal/campaign"
)

// ---------------------------------------------------------------------------
// Thresholds (the pre-registered feasibility constants — research §7.2 demands
// these are FIXED before any campaign run, never tuned to make a run pass).
// ---------------------------------------------------------------------------

// DefaultICCFloor is the reliability floor of the feasibility gate. ICC < 0.5
// is the conventional "poor reliability" band (Koo & Li 2016): below it the
// majority of the instrument's variance is within-task replay noise rather than
// real between-task difference, so the instrument cannot rank one config above
// another regardless of N. 0.5 = "the real task-difference signal is at least as
// large as the replay noise" — the minimum to trust a ranking.
const DefaultICCFloor = 0.5

// DefaultClaimableLift is the lift the campaign would want to claim on the binary
// axis — the MDE must come in UNDER this for the instrument to resolve it. It
// matches the locked binary MDE convention of internal/bench/eval (MDE = 0.15):
// an instrument that cannot detect a 0.15 absolute rate change at the planned N
// is too coarse for the campaign's claims.
const DefaultClaimableLift = 0.15

// zAlpha is the one-sided z for α = 0.05 (the strict-`>` keep-gate is one-sided:
// the campaign only ever claims a positive lift). zBeta is the z for power
// 1−β = 0.80. (zAlpha+zBeta) is the standard sample-size effect multiplier.
const (
	zAlpha = 1.6448536269514722 // Φ⁻¹(0.95)
	zBeta  = 0.8416212335729143 // Φ⁻¹(0.80)
)

// ---------------------------------------------------------------------------
// Input: the substrate-independent per-task replay row the ruler reduces.
// ---------------------------------------------------------------------------

// TaskReplays is ONE task's K-replay outcome in the substrate-independent shape
// the ruler consumes: a binary success-count over K replays (solved for the
// answer-oracle ProbeStability, fired for the cognition CogStability) plus the
// per-task MEAN completion-token cost (the only cost statistic the aggregates
// retain — see the package doc's HONEST SCOPE note). It is built from the
// existing campaign aggregates via FromProbe / FromCog; the ruler never re-runs.
type TaskReplays struct {
	// ID identifies the task (Goal/Signature) — for surfacing per-task rows.
	ID string
	// Success is the count of the K replays whose binary outcome was a 1
	// (Solved for the answer oracle, Fired for the cognition faculty).
	Success int
	// Replays is K — the replay count this row was aggregated over.
	Replays int
	// MeanCompletion is the per-replay mean completion-token cost (the cache-
	// immune decode cost). 0 on the offline test double (no real usage).
	MeanCompletion float64
	// Completions is the PER-REPLAY completion-token vector (length K when the
	// instrument retains it; nil/empty for the legacy sum-only aggregates). When
	// present it lets the ruler recover the WITHIN-task cost-σ noise floor (the
	// W5 gate metric is completion-tokens-per-task) — not just the between-task
	// dispersion of the means. All zeros on the offline test double.
	Completions []int
}

// rate is the per-task success fraction (the noise-floored binary rate).
func (t TaskReplays) rate() float64 {
	if t.Replays == 0 {
		return 0
	}
	return float64(t.Success) / float64(t.Replays)
}

// FromProbe adapts the answer-oracle replay aggregates (ProbeReplays output)
// into the ruler's substrate-independent rows. SolvedRate is the binary axis.
func FromProbe(rows []campaign.ProbeStability) []TaskReplays {
	out := make([]TaskReplays, len(rows))
	for i, r := range rows {
		out[i] = TaskReplays{
			ID:             r.Goal,
			Success:        r.Solved,
			Replays:        r.Replays,
			MeanCompletion: r.MeanCompletion(),
			Completions:    r.Completions,
		}
	}
	return out
}

// FromCog adapts the cognition faculty-fire replay aggregates
// (CognitionProbeReplays output) into the ruler's rows. Fired is the binary axis.
func FromCog(rows []campaign.CogStability) []TaskReplays {
	out := make([]TaskReplays, len(rows))
	for i, r := range rows {
		out[i] = TaskReplays{
			ID:             r.Goal,
			Success:        r.Fired,
			Replays:        r.Replays,
			MeanCompletion: r.MeanCompletion(),
			Completions:    r.Completions,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// The characterization result.
// ---------------------------------------------------------------------------

// Characterization is the full measurement-soundness read over a set of
// K-replay task rows: the binary-axis noise floor + reliability + the cost-axis
// descriptive band + the feasibility verdict. Everything is deterministic in the
// input rows.
type Characterization struct {
	// Tasks is the number of distinct tasks (N) the rows characterize.
	Tasks int
	// K is the replay count (taken from the rows; the gate assumes a common K).
	K int

	// --- binary axis (EXACT from aggregates) ---

	// SigmaNoise is the pooled within-task SD of the 0/1 outcome indicator:
	// sqrt(mean_i[ p_i(1-p_i) ]). This is the raw per-replay noise (matches
	// internal/bench/eval Phase-0 σ_noise).
	SigmaNoise float64
	// SigmaNoiseAveraged is SigmaNoise/√K — the noise on the per-task RATE
	// estimate after K-averaging (averaging cuts σ by √K).
	SigmaNoiseAveraged float64
	// BandLow/BandHigh is the ±2·SigmaNoiseAveraged noise band (the 2σ band the
	// strategy uses for MinEfficiency): a per-task rate delta inside ±this is
	// indistinguishable from replay noise.
	BandHalfWidth float64

	// BetweenVar is the variance of the per-task success RATES across tasks
	// (sample, n−1) — the real task-difference signal.
	BetweenVar float64
	// WithinVar is the pooled within-task variance of the 0/1 indicator (the
	// MSW input) — the replay noise.
	WithinVar float64
	// ICC is the one-way random-effects intraclass correlation (ICC(1)): the
	// fraction of the total variance that is real between-task difference. ∈
	// [0,1] (clamped; the ANOVA estimator can go slightly negative when the
	// between signal is below noise — clamped to 0).
	ICC float64

	// MDE is the minimum detectable effect on the binary rate axis at this N/K:
	// the smallest absolute rate lift the instrument can resolve at α=0.05,
	// power=0.80, two-arm paired-by-task. Computed from SigmaNoiseAveraged and N.
	MDE float64

	// --- cost axis (the W5 gate metric: completion-tokens-per-task) ---

	// CostMean is the mean across tasks of the per-task mean completion cost.
	CostMean float64
	// CostBetweenSD is the SD across tasks of the per-task mean completion cost
	// (the between-task cost dispersion). Always computed (from the per-task means)
	// — it is a between-task band, NOT the within-task noise floor.
	CostBetweenSD float64

	// CostWithinFloorAvailable is true when the per-replay completion vector was
	// retained (TaskReplays.Completions non-empty), so the WITHIN-task cost-σ
	// noise floor below is a real recovered statistic. False for the legacy
	// sum-only aggregates, in which case the cost axis is descriptive (between-task
	// band only) and the within-task fields are zero. Surfaced so a caller never
	// mistakes a between-task band for a characterized within-task floor.
	CostWithinFloorAvailable bool
	// CostSigmaWithin is the WITHIN-task SD of completion tokens across the K
	// replays, averaged over tasks (pooled within-task SD): the per-replay cost
	// noise. Only meaningful when CostWithinFloorAvailable; 0 otherwise (and 0 on
	// the offline test double where every replay's completion is 0).
	CostSigmaWithin float64
	// CostSigmaWithinAveraged is CostSigmaWithin/√K — the cost noise on the
	// per-task MEAN completion estimate after K-averaging (averaging cuts σ by √K),
	// the cost-axis analogue of SigmaNoiseAveraged.
	CostSigmaWithinAveraged float64
	// CostBandHalfWidth is the ±2·CostSigmaWithinAveraged noise band on the per-task
	// mean completion: a per-task cost delta inside ±this is indistinguishable from
	// replay noise (the cost-axis analogue of BandHalfWidth).
	CostBandHalfWidth float64
	// CostMDE is the minimum detectable effect on the per-task mean completion-token
	// cost at this N/K (α=0.05, power=0.80, two-arm paired-by-task) — the smallest
	// absolute token delta the cost instrument can resolve. Only meaningful when
	// CostWithinFloorAvailable.
	CostMDE float64
	// CostVerdict is the cost-axis read: DEGENERATE (no per-replay vector, or zero
	// within-task cost variance — e.g. the test double), NOISY (within-task cost-σ
	// large relative to the between-task cost spread → the cost instrument can't
	// rank configs by cost), or RELIABLE (between-task cost signal clears the
	// within-task noise). Reported ALONGSIDE the binary Verdict; never the gate.
	CostVerdict CostVerdict

	// --- the gate ---

	// ICCFloor / ClaimableLift are the thresholds the gate ran against (echoed
	// for the report so the verdict is self-describing).
	ICCFloor      float64
	ClaimableLift float64
	// Feasible is the boolean gate: ICC ≥ ICCFloor AND MDE ≤ ClaimableLift.
	Feasible bool
	// Verdict is the one-line read (FEASIBLE / NOISY-RULER / LOW-RELIABILITY /
	// DEGENERATE).
	Verdict Verdict
}

// Verdict is the one-line feasibility read of the instrument.
type Verdict string

const (
	// VerdictFeasible — both gate clauses hold: the instrument is reliable
	// (ICC ≥ floor) AND fine enough (MDE ≤ claimable lift) to trust a strict-`>`
	// lift at this N/K.
	VerdictFeasible Verdict = "FEASIBLE"
	// VerdictNoisyRuler — ICC is fine but the MDE exceeds the claimable lift: the
	// instrument cannot RESOLVE the effect the campaign wants to claim at this
	// N/K. Raise K (cuts σ by √K) or N (cuts MDE) — do not patch, REJECT.
	VerdictNoisyRuler Verdict = "NOISY-RULER"
	// VerdictLowReliability — ICC is below the floor: most of the variance is
	// within-task replay noise, not real task-difference. The instrument cannot
	// rank one config above another regardless of N. Reduce per-replay noise
	// (lower temp / more K) before trusting any ranking.
	VerdictLowReliability Verdict = "LOW-RELIABILITY"
	// VerdictDegenerate — not enough data to characterize (N < 2 tasks, or K < 2
	// replays, or zero variance everywhere — e.g. the deterministic test double
	// where every replay matches, so there is no noise to characterize and ICC
	// is undefined). NOT a pass and NOT a noise failure: the instrument has not
	// been exercised on a non-deterministic substrate yet.
	VerdictDegenerate Verdict = "DEGENERATE"
)

// CostVerdict is the one-line read of the COST instrument (completion-tokens),
// reported alongside the binary Verdict — never the keep-gate itself.
type CostVerdict string

const (
	// CostReliable — the per-replay cost vector is present AND the between-task
	// cost spread clears the within-task cost noise: the cost instrument can rank
	// one config's per-task token cost above another's at this N/K.
	CostReliable CostVerdict = "COST-RELIABLE"
	// CostNoisy — the cost vector is present but the within-task cost-σ swamps the
	// between-task cost spread (the cost MDE exceeds the between-task signal): the
	// cost instrument cannot resolve a per-task cost difference at this N/K. Raise
	// K (cuts cost-σ by √K) or N before trusting a cost ranking.
	CostNoisy CostVerdict = "COST-NOISY"
	// CostDegenerate — no per-replay vector retained (sum-only aggregates), OR the
	// within-task cost variance is zero everywhere (e.g. the offline test double
	// where every replay's completion is 0). The cost instrument has not been
	// exercised; the cost axis is descriptive only.
	CostDegenerate CostVerdict = "COST-DEGENERATE"
)

// ---------------------------------------------------------------------------
// The reduction.
// ---------------------------------------------------------------------------

// Options carries the pre-registered feasibility thresholds. The zero value uses
// the documented defaults (DefaultICCFloor, DefaultClaimableLift).
type Options struct {
	// ICCFloor overrides DefaultICCFloor when > 0.
	ICCFloor float64
	// ClaimableLift overrides DefaultClaimableLift when > 0.
	ClaimableLift float64
}

func (o Options) iccFloor() float64 {
	if o.ICCFloor > 0 {
		return o.ICCFloor
	}
	return DefaultICCFloor
}

func (o Options) claimableLift() float64 {
	if o.ClaimableLift > 0 {
		return o.ClaimableLift
	}
	return DefaultClaimableLift
}

// Characterize reduces the K-replay task rows into the full measurement-soundness
// read and applies the feasibility gate. Pure and deterministic in rows + opts.
//
// The binary axis is characterized EXACTLY (the within-task variance of a 0/1
// indicator is p(1-p), recovered from the success count — no per-replay vector
// needed). The cost axis recovers the TRUE within-task cost-σ noise floor when the
// per-replay completion vector is present (TaskReplays.Completions), else it is
// descriptive between-task only. See the package doc's THE TWO COST REGIMES.
func Characterize(rows []TaskReplays, opts Options) Characterization {
	c := Characterization{
		Tasks:         len(rows),
		ICCFloor:      opts.iccFloor(),
		ClaimableLift: opts.claimableLift(),
	}

	// Common K (the rows are produced with one K; take the modal/first non-zero).
	for _, r := range rows {
		if r.Replays > 0 {
			c.K = r.Replays
			break
		}
	}

	// --- binary axis: per-task rate, within-task variance, between-task variance ---
	var rates []float64
	var withinVarSum float64 // pooled p(1-p)
	var withinTaskN int
	var costs []float64
	for _, r := range rows {
		if r.Replays > 0 {
			p := r.rate()
			rates = append(rates, p)
			withinVarSum += p * (1 - p)
			withinTaskN++
		}
		costs = append(costs, r.MeanCompletion)
	}

	if withinTaskN > 0 {
		c.WithinVar = withinVarSum / float64(withinTaskN)
		c.SigmaNoise = math.Sqrt(c.WithinVar)
	}
	if c.K > 0 {
		c.SigmaNoiseAveraged = c.SigmaNoise / math.Sqrt(float64(c.K))
	} else {
		c.SigmaNoiseAveraged = c.SigmaNoise
	}
	c.BandHalfWidth = 2 * c.SigmaNoiseAveraged

	c.BetweenVar = sampleVar(rates)

	// --- ICC(1), one-way random-effects intraclass correlation ---
	// ICC(1) = (MSB − MSW) / (MSB + (k−1)·MSW)
	//   MSB = k · Var(per-task means)   (between mean square; the per-task mean
	//         is the rate, and k replays each contribute → k× the rate variance)
	//   MSW = mean within-task variance, with the UNBIASED within-task estimator
	//         k/(k−1)·p(1-p) (the n−1 correction inside each task).
	// Clamped to [0,1]: the ANOVA estimator goes slightly negative when the
	// between signal sits below the noise (interpreted as 0 reliability).
	c.ICC = computeICC1(c.BetweenVar, c.WithinVar, c.K)

	// --- MDE on the binary rate axis ---
	// Two-arm, paired-by-task, K-averaged per-arm. The per-arm per-task rate has
	// SE = SigmaNoiseAveraged; a paired difference of N tasks has SE_diff =
	// SigmaNoiseAveraged·√(2/N). The detectable effect at α (one-sided) + power:
	//   MDE = (zAlpha + zBeta) · SigmaNoiseAveraged · √(2/N).
	if c.Tasks > 0 && c.SigmaNoiseAveraged > 0 {
		c.MDE = (zAlpha + zBeta) * c.SigmaNoiseAveraged * math.Sqrt(2/float64(c.Tasks))
	}

	// --- cost axis ---
	// Between-task band (always available, from the per-task means).
	c.CostMean = mean(costs)
	c.CostBetweenSD = sampleSD(costs)

	// Within-task cost noise floor — recoverable ONLY when the per-replay
	// completion vector is present. A row is usable if its vector length == K
	// (every replay sampled). The pooled within-task SD is √(mean over tasks of the
	// sample within-task variance) — the cost-axis analogue of the binary p(1-p).
	var withinCostVarSum float64
	var costVecTaskN int
	costVecPresent := false
	for _, r := range rows {
		if len(r.Completions) == 0 {
			continue
		}
		costVecPresent = true
		// Use the row's own K (== len(Completions) once a run completes); guard a
		// short vector by using its actual length so a partial row is still honest.
		v := r.Completions
		fs := make([]float64, len(v))
		for i, x := range v {
			fs[i] = float64(x)
		}
		if len(fs) >= 2 {
			withinCostVarSum += sampleVar(fs)
			costVecTaskN++
		}
	}
	if costVecPresent {
		c.CostWithinFloorAvailable = true
		if costVecTaskN > 0 {
			c.CostSigmaWithin = math.Sqrt(withinCostVarSum / float64(costVecTaskN))
		}
		if c.K > 0 {
			c.CostSigmaWithinAveraged = c.CostSigmaWithin / math.Sqrt(float64(c.K))
		} else {
			c.CostSigmaWithinAveraged = c.CostSigmaWithin
		}
		c.CostBandHalfWidth = 2 * c.CostSigmaWithinAveraged
		// Cost MDE: same two-arm paired-by-task form as the binary MDE, on the cost
		// scale (token units): (zAlpha+zBeta)·CostSigmaWithinAveraged·√(2/N).
		if c.Tasks > 0 && c.CostSigmaWithinAveraged > 0 {
			c.CostMDE = (zAlpha + zBeta) * c.CostSigmaWithinAveraged * math.Sqrt(2/float64(c.Tasks))
		}
	}
	c.CostVerdict = deriveCostVerdict(c)

	// --- the feasibility gate (BINARY axis — the primary keep-gate, unchanged) ---
	c.Verdict, c.Feasible = deriveVerdict(c)
	return c
}

// CharacterizeProbe / CharacterizeCog are the convenience entrypoints straight
// off the campaign replay aggregates (no manual FromProbe/FromCog needed).
func CharacterizeProbe(rows []campaign.ProbeStability, opts Options) Characterization {
	return Characterize(FromProbe(rows), opts)
}

func CharacterizeCog(rows []campaign.CogStability, opts Options) Characterization {
	return Characterize(FromCog(rows), opts)
}

// computeICC1 is the one-way random-effects ICC(1) from the between-task rate
// variance, the pooled within-task p(1-p) variance, and K. Returns a value in
// [0,1] (clamped). Degenerate inputs (K<2 or non-positive denominator) return 0
// — the verdict layer routes those to DEGENERATE.
func computeICC1(betweenVar, withinVar float64, k int) float64 {
	if k < 2 {
		return 0
	}
	kf := float64(k)
	msb := kf * betweenVar
	// Unbiased within-task variance: k/(k−1)·mean[p(1-p)] corrects the population
	// p(1-p) to the n−1 within-task estimator MSW uses.
	msw := withinVar * kf / (kf - 1)
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

// deriveVerdict applies the gate. DEGENERATE first (not enough exercised data or
// no noise to characterize — e.g. the deterministic test double), then the two
// gate clauses (LOW-RELIABILITY before NOISY-RULER: reliability is the
// prerequisite — a more-precise but unreliable instrument is still useless),
// else FEASIBLE.
func deriveVerdict(c Characterization) (Verdict, bool) {
	// Degenerate: under-2 tasks, under-2 replays, or no variance ANYWHERE on the
	// binary axis (within == 0 AND between == 0 — every task solved identically
	// every replay, so there is no noise floor to characterize and ICC is
	// undefined). This is the offline-test-double / not-yet-on-claude state.
	if c.Tasks < 2 || c.K < 2 || (c.WithinVar == 0 && c.BetweenVar == 0) {
		return VerdictDegenerate, false
	}
	if c.ICC < c.ICCFloor {
		return VerdictLowReliability, false
	}
	if c.MDE > c.ClaimableLift {
		return VerdictNoisyRuler, false
	}
	return VerdictFeasible, true
}

// deriveCostVerdict reads the COST instrument (reported alongside the binary
// verdict, never the keep-gate). DEGENERATE first: no per-replay vector retained,
// under-2 tasks/replays, or zero within-task cost variance (the offline test
// double, where every replay's completion is 0). Otherwise compare the cost MDE
// (the within-task cost noise the instrument can resolve at this N/K) against the
// between-task cost spread (the real cost signal): if the noise floor swamps the
// signal (MDE ≥ between-task SD) the cost instrument cannot rank configs by cost
// → NOISY; else RELIABLE.
func deriveCostVerdict(c Characterization) CostVerdict {
	if !c.CostWithinFloorAvailable || c.Tasks < 2 || c.K < 2 || c.CostSigmaWithin == 0 {
		return CostDegenerate
	}
	// The cost instrument resolves a per-task cost difference only if the smallest
	// detectable cost delta (CostMDE) comes in UNDER the real between-task cost
	// spread it would need to rank. A within-cost noise that exceeds the between-
	// task cost dispersion means the cost ranking is noise.
	if c.CostMDE >= c.CostBetweenSD {
		return CostNoisy
	}
	return CostReliable
}

// ---------------------------------------------------------------------------
// small numeric helpers (local — the package has no cross-package math dep).
// ---------------------------------------------------------------------------

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// sampleVar is the sample (n−1) variance; <2 values ⇒ 0 (no spread).
func sampleVar(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	m := mean(xs)
	var ss float64
	for _, x := range xs {
		d := x - m
		ss += d * d
	}
	return ss / float64(n-1)
}

// sampleSD is the sample (n−1) standard deviation; <2 values ⇒ 0.
func sampleSD(xs []float64) float64 {
	return math.Sqrt(sampleVar(xs))
}
