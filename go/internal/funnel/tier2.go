package funnel

// tier2.go — the Tier-2 LIFT RUNNER (registry-scaling-strategy.md §4 Tier 2, §5 PHASE 6 LIFT-SEARCH):
// the funnel stage that runs a BEFORE/AFTER lift over an admitted batch and applies the
// keep-or-revert-the-batch decision. It is the expensive, model-in-the-loop stage that ONLY ever sees
// candidates that already cleared Stage-C + Tier-0 + Tier-1 (the cheap deterministic gates above).
//
// THE LEAF PROPERTY IS PRESERVED. The funnel stays a pure stdlib leaf: the actual benchmark (which
// imports internal/bench + the engine) is INJECTED as a LiftBench, exactly like Similarity is injected
// for the near-dup cut. The caller (internal/scaling, the campaign) wires the real two-arm
// internal/bench probe; a test injects a deterministic FAKE bench. So the keep-or-revert LOGIC — the
// part the spec calls the testable core — is provable offline with zero tokens.
//
// THE METRIC IS COMPLETION-TOKENS-PER-TASK AT HELD UTILITY (the saturated-frontier lesson, the
// 2026-06-15 registry-scaling research §"Code gaps" #3): on a high-cache substrate cached INPUT
// dominates total cost, so a real registry win shows in COMPLETION/synthesise tokens, not total. We
// gate on completion-tokens-per-solved (cache-immune), never answer-rate alone — when both arms already
// solve a saturated grounded task the only open axis is efficiency (a minted skill recalls a converged
// Program instead of re-synthesising it). Capability must NOT regress (the hard floor); given flat
// capability, a measured completion-token reduction beyond the noise floor is the KEEP.
//
// OPT-IN / ADDITIVE. Nothing above calls Tier-2 — Admit / RetrievalIntegrity are untouched. The default
// funnel behaviour is byte-identical; a caller opts in by constructing a Tier2Runner and calling RunTier2.
// So this file adds a stage, it does not change one.

import (
	"fmt"
	"math"
)

// ---------------------------------------------------------------------------
// The injected bench (keeps funnel a leaf)
// ---------------------------------------------------------------------------

// ArmStats is one bench arm's outcome on the held-out lift suite: the paired per-task pass/fail and the
// per-task COMPLETION-token spend, in the SAME task order across arms (the pairing the keep decision
// needs). CompletionTokens is the cache-immune efficiency signal — total tokens are dominated by cached
// input on a high-cache substrate and so are NOT the metric (research §"Code gaps" #3). It mirrors
// campaign.ArmResult but lives in the leaf so the funnel's Tier-2 contract carries no internal import.
type ArmStats struct {
	// PerItem[i] is whether task i was SOLVED by this arm; paired index-for-index with the other arm.
	PerItem []bool
	// CompletionPerItem[i] is the COMPLETION tokens task i cost this arm (cache-immune). Same order as
	// PerItem. Empty/zero on the offline double (no llm.* events) — the runner then reads no efficiency
	// signal, which is the honest answer offline.
	CompletionPerItem []int
}

// Solved counts the tasks this arm passed.
func (a ArmStats) Solved() int {
	n := 0
	for _, ok := range a.PerItem {
		if ok {
			n++
		}
	}
	return n
}

// Total is the number of tasks attempted.
func (a ArmStats) Total() int { return len(a.PerItem) }

// PassRate is the capability score (fraction solved); 0 for an empty suite.
func (a ArmStats) PassRate() float64 {
	if a.Total() == 0 {
		return 0
	}
	return float64(a.Solved()) / float64(a.Total())
}

// CompletionTokens sums the per-task completion spend.
func (a ArmStats) CompletionTokens() int {
	t := 0
	for _, c := range a.CompletionPerItem {
		t += c
	}
	return t
}

// CompletionPerSolved is the headline efficiency metric: completion tokens per task SOLVED. +Inf when
// nothing solved — you cannot be "efficient" at zero capability, so a 0-solved arm never reads cheaper.
func (a ArmStats) CompletionPerSolved() float64 {
	s := a.Solved()
	if s == 0 {
		return math.Inf(1)
	}
	return float64(a.CompletionTokens()) / float64(s)
}

// LiftBench runs the held-out lift suite for one arm. stateDir == "" is the BASELINE arm (seed
// registries only); a non-empty stateDir is the WITH-BATCH arm (seed + the staged candidate batch). The
// real implementation runs the two-arm internal/bench probe on the chosen substrate behind the cost
// guard; a test injects a deterministic fake. It returns the arm's paired per-task stats or an error
// (a budget overrun / engine failure — the batch then aborts, never a silent keep).
type LiftBench interface {
	BenchArm(stateDir string) (ArmStats, error)
}

// LiftBenchFunc adapts a plain function into a LiftBench (the common injection shape).
type LiftBenchFunc func(stateDir string) (ArmStats, error)

// BenchArm implements LiftBench.
func (f LiftBenchFunc) BenchArm(stateDir string) (ArmStats, error) { return f(stateDir) }

// ---------------------------------------------------------------------------
// The keep-or-revert-the-batch decision (the testable core)
// ---------------------------------------------------------------------------

// LiftDecision is the verdict a batch faces after the Tier-2 A/B.
type LiftDecision int

const (
	// LiftRevert — the batch is reverted (a capability regression, no measurable gain, or a Tier-1
	// regression handed in by the cheaper upstream gate).
	LiftRevert LiftDecision = iota
	// LiftKeep — the batch is kept (a significant capability lift, OR cheaper at flat capability).
	LiftKeep
	// LiftMargin — ambiguous (a promising-but-unproven positive lean at flat cost): the human decides.
	LiftMargin
)

// String renders the decision for the ledger/report.
func (d LiftDecision) String() string {
	switch d {
	case LiftKeep:
		return "KEEP"
	case LiftMargin:
		return "MARGIN"
	default:
		return "REVERT"
	}
}

// Tier2Config configures the keep-or-revert-the-batch rule. The zero value is filled with DefaultTier2
// by RunTier2, so a caller may pass an empty config.
type Tier2Config struct {
	// Alpha is the significance level for the paired-capability McNemar test (default 0.05). A
	// capability change is only acted on as significant below this p.
	Alpha float64
	// MinEfficiency is the minimum completion-tokens-per-solved REDUCTION that counts as "cheaper"
	// (default DefaultMinEfficiency). It MUST be derived from a measured noise floor (research §STEP 1:
	// MinEfficiency = 2σ of completion-tokens-per-solved across replays) — the default is a placeholder
	// floor so a single-token float wobble never reads as a win, NOT a calibrated value.
	MinEfficiency float64
}

// DefaultMinEfficiency is the placeholder completion-tokens-per-solved floor: a batch must save at least
// this many completion tokens per solved task to be auto-kept on the efficiency axis. It is deliberately
// a coarse, conservative non-zero floor (so float noise never wins) — the CALIBRATED floor is 2σ of the
// measured per-replay noise (research §STEP 1) and must override this before the gate is trusted on a
// real substrate (that calibration is the bench-oracle-doctor follow-up, NOT done here).
const DefaultMinEfficiency = 1.0

// DefaultTier2 is the standard keep-rule configuration.
func DefaultTier2() Tier2Config { return Tier2Config{Alpha: 0.05, MinEfficiency: DefaultMinEfficiency} }

// LiftVerdict is the full Tier-2 decision with the numbers behind it (the ledger evidence + the
// human-readable reason).
type LiftVerdict struct {
	// Decision is KEEP / REVERT / MARGIN.
	Decision LiftDecision
	// CapabilityLift is the with-batch pass-rate minus the baseline pass-rate (the answer-rate axis).
	CapabilityLift float64
	// CompletionDelta is the completion-tokens-per-solved REDUCTION (positive = cheaper). The headline
	// efficiency axis at held utility (cache-immune).
	CompletionDelta float64
	// McNemarP is the two-sided p of the paired capability change (1.0 when no discordant pairs).
	McNemarP float64
	// Fixed / Broke are the McNemar discordant pairs: tasks the batch FIXED (baseline-fail → batch-pass)
	// and BROKE (baseline-pass → batch-fail).
	Fixed, Broke int
	// Tier1Regressed echoes the upstream Tier-1 gate (a Tier-1 regression is a hard REVERT here too).
	Tier1Regressed bool
	// Reason is the human-readable WHY (the ledger evidence string).
	Reason string
}

// EvaluateLift applies the Tier-2 keep-or-revert-the-batch rule. It is deterministic and model-free —
// the bench produces the numbers, this decides on them. The gate ladder (registry-scaling-strategy §4
// "the decision rule, one line"; the saturated-frontier efficiency lever, research §"surviving thesis"):
//
//  0. Tier-1 retrieval already regressed     → REVERT (the cheap upstream gate caught confusion)
//  1. a SIGNIFICANT capability regression     → REVERT (it breaks more than it fixes, for real)
//  2. a SIGNIFICANT capability lift           → KEEP   (smarter)
//  3. flat capability, CHEAPER (≥ MinEff)     → KEEP   (cheaper at held utility — the saturated lever)
//  4. flat capability, COSTLIER (≤ -MinEff)   → REVERT (no gain, costs more completion)
//  5. a non-significant positive lean, flat $ → MARGIN (promising but unproven — the human decides)
//  6. otherwise (no smarter, no cheaper)      → REVERT (filler)
//
// The arms must be paired (same length, same task order). A length mismatch / empty suite is a
// programming error and yields REVERT with a clear reason — never a silent keep.
func EvaluateLift(baseline, withBatch ArmStats, tier1Pass bool, cfg Tier2Config) LiftVerdict {
	if cfg.Alpha <= 0 {
		cfg.Alpha = 0.05
	}
	if cfg.MinEfficiency <= 0 {
		cfg.MinEfficiency = DefaultMinEfficiency
	}
	v := LiftVerdict{Tier1Regressed: !tier1Pass}

	if baseline.Total() != withBatch.Total() || baseline.Total() == 0 {
		v.Decision = LiftRevert
		v.Reason = "unpaired or empty A/B arms — cannot evaluate Tier-2 lift"
		return v
	}

	v.CapabilityLift = withBatch.PassRate() - baseline.PassRate()
	// completion-tokens-per-solved REDUCTION (positive = the batch is cheaper at held utility).
	v.CompletionDelta = baseline.CompletionPerSolved() - withBatch.CompletionPerSolved()
	if math.IsInf(v.CompletionDelta, 0) || math.IsNaN(v.CompletionDelta) {
		// One arm solved nothing (per-solved is +Inf) — not a MEASURABLE efficiency change.
		v.CompletionDelta = 0
	}
	v.Fixed, v.Broke = discordantBool(baseline.PerItem, withBatch.PerItem)
	v.McNemarP = mcNemarP(v.Fixed, v.Broke)
	significant := v.McNemarP < cfg.Alpha

	// 0. Tier-1 retrieval integrity is the hard gate (caught cheaply upstream).
	if !tier1Pass {
		v.Decision = LiftRevert
		v.Reason = "Tier-1 retrieval regressed (the batch displaced a correct rank-1)"
		return v
	}
	// 1/2. a significant capability change decides on its direction.
	if significant {
		if v.Broke > v.Fixed {
			v.Decision = LiftRevert
			v.Reason = fmt.Sprintf("significant capability regression (broke %d, fixed %d, p=%.3f)", v.Broke, v.Fixed, v.McNemarP)
		} else {
			v.Decision = LiftKeep
			v.Reason = fmt.Sprintf("SMARTER: significant lift (fixed %d, broke %d, p=%.3f, +%.1f%% pass)", v.Fixed, v.Broke, v.McNemarP, v.CapabilityLift*100)
		}
		return v
	}
	// 3/4. capability is flat — decide on the cache-immune completion-token efficiency. The metric is
	// "completion-tokens-per-solved AT HELD UTILITY": utility is held ONLY when the SOLVED COUNT is equal
	// across arms. If the solved count differs (a non-significant capability lean), per-solved efficiency
	// shifts purely from the changing denominator and is NOT a real efficiency signal — so the efficiency
	// branch fires only at genuinely flat utility; an unequal-utility non-significant lean falls through
	// to MARGIN (5). (This is the difference from a per-solved comparison that confounds a denominator
	// change with a cost change.)
	heldUtility := baseline.Solved() == withBatch.Solved()
	if heldUtility {
		if v.CompletionDelta >= cfg.MinEfficiency {
			v.Decision = LiftKeep
			v.Reason = fmt.Sprintf("CHEAPER at held utility (−%.0f completion-tokens/solved)", v.CompletionDelta)
			return v
		}
		if v.CompletionDelta <= -cfg.MinEfficiency {
			v.Decision = LiftRevert
			v.Reason = fmt.Sprintf("no capability gain and COSTLIER (+%.0f completion-tokens/solved)", -v.CompletionDelta)
			return v
		}
	}
	// 5. a non-significant positive capability lean (at flat or unequal cost) — promising but unproven.
	if v.Fixed > v.Broke {
		v.Decision = LiftMargin
		v.Reason = fmt.Sprintf("promising but unproven (fixed %d, broke %d, p=%.3f) — needs the human or more data", v.Fixed, v.Broke, v.McNemarP)
		return v
	}
	// 6. neither smarter nor cheaper.
	v.Decision = LiftRevert
	v.Reason = "no measurable gain (capability flat, completion-cost flat) — filler"
	return v
}

// ---------------------------------------------------------------------------
// The runner (drives the injected bench, then evaluates)
// ---------------------------------------------------------------------------

// Tier2Runner runs the BEFORE/AFTER lift for an admitted batch through an injected LiftBench and applies
// EvaluateLift. It holds no per-run state, so one runner is reusable across batches.
type Tier2Runner struct {
	// Bench runs one arm given a registry state dir ("" = baseline, a dir = with-batch). REQUIRED.
	Bench LiftBench
	// Config is the keep-rule configuration (zero value ⇒ DefaultTier2).
	Config Tier2Config
}

// NewTier2Runner builds a runner over the injected bench with the default keep-rule config.
func NewTier2Runner(bench LiftBench) *Tier2Runner {
	return &Tier2Runner{Bench: bench, Config: DefaultTier2()}
}

// LiftResult is the full Tier-2 outcome: the verdict plus the two paired arm stats it was computed over
// (so the ledger records the raw evidence, not just the decision).
type LiftResult struct {
	// Verdict is the keep-or-revert-the-batch decision with its numbers.
	Verdict LiftVerdict
	// Baseline / WithBatch are the two paired arm stats (the raw A/B evidence).
	Baseline  ArmStats
	WithBatch ArmStats
}

// RunTier2 runs the two-arm lift for a staged batch (the with-batch arm reads batchStateDir; the
// baseline arm reads ""), then applies the keep-or-revert decision. tier1Pass is the upstream Tier-1
// verdict (a Tier-1 regression is a hard REVERT — passed through so the Tier-2 decision is self-
// contained for the ledger). A bench error on either arm aborts the batch (returned, never a silent
// keep) — the baseline must already be snapshotted by the caller so the live state is safe.
func (r *Tier2Runner) RunTier2(batchStateDir string, tier1Pass bool) (LiftResult, error) {
	if r.Bench == nil {
		return LiftResult{}, fmt.Errorf("funnel: Tier2Runner has no LiftBench injected")
	}
	cfg := r.Config
	if cfg.Alpha == 0 && cfg.MinEfficiency == 0 {
		cfg = DefaultTier2()
	}
	base, err := r.Bench.BenchArm("")
	if err != nil {
		return LiftResult{}, fmt.Errorf("Tier-2 baseline arm: %w", err)
	}
	withBatch, err := r.Bench.BenchArm(batchStateDir)
	if err != nil {
		return LiftResult{}, fmt.Errorf("Tier-2 with-batch arm: %w", err)
	}
	return LiftResult{
		Verdict:   EvaluateLift(base, withBatch, tier1Pass, cfg),
		Baseline:  base,
		WithBatch: withBatch,
	}, nil
}

// ---------------------------------------------------------------------------
// deterministic stats helpers (kept local — funnel is a pure stdlib leaf)
// ---------------------------------------------------------------------------

// discordantBool counts the McNemar discordant pairs over paired pass/fail: fixed = baseline-fail →
// batch-pass (the batch helped), broke = baseline-pass → batch-fail (the batch hurt).
func discordantBool(baseline, batch []bool) (fixed, broke int) {
	n := len(baseline)
	if len(batch) < n {
		n = len(batch)
	}
	for i := 0; i < n; i++ {
		switch {
		case !baseline[i] && batch[i]:
			fixed++
		case baseline[i] && !batch[i]:
			broke++
		}
	}
	return fixed, broke
}

// mcNemarP is the two-sided p-value of the paired change via the continuity-corrected normal
// approximation of McNemar's test over the discordant pairs (fixed=c, broke=b). With no discordant
// pairs the change is undefined-significant → p=1 (not significant). For small discordant counts the
// normal approximation is conservative (it under-claims significance), which is the safe direction — a
// batch must clear a real bar to be auto-kept. (Mirrors campaign/keeprule.go's mcNemarP; kept local so
// the funnel leaf does not import internal/bench/stats.)
func mcNemarP(fixed, broke int) float64 {
	n := fixed + broke
	if n == 0 {
		return 1.0
	}
	z := (math.Abs(float64(fixed-broke)) - 1.0) / math.Sqrt(float64(n))
	if z < 0 {
		z = 0 // the continuity correction can push a 1-pair difference negative; clamp (p→1)
	}
	return math.Erfc(z / math.Sqrt2) // two-sided
}
