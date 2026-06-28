// Package eval wires the measuring-stick component packages
// (internal/bench/{runner,tiera,tierb,stats,ledger,gen}) into the per-mechanism
// PILOT computation the cmd/bench driver renders: Phase-0 noise
// characterization, the LIFT contrasts, and the feasibility verdict
// (docs/internal/notes/measuring-stick-spec.md §4, §5).
//
// This package owns NO scoring math of its own beyond aggregation — it
// reduces the per-replay per-arm types.ItemResult / types.ScenarioResult rows
// (produced by tiera.RunItem / tierb.RunScenario) into:
//
//   - σ_noise (Phase-0, §4.1): the pooled within-item standard deviation of the
//     per-replay pass indicator, per arm, plus the σ of the paired
//     (harness−bare) per-item difference, plus the BARE-arm lure-calibration
//     failure rate per item with the 0.5 admit floor.
//   - LIFT (§1.4): majority-vote pass per arm per item, paired by item, fed to
//     stats.McNemar + stats.BootstrapBCa for both the harness−bare and the
//     gate-on−gate-off (mechanism-specific) contrasts, plus the isolation rate.
//   - The FEASIBILITY verdict (§4.1 gate): does the K-averaged σ_noise clear
//     MDE/2 = 0.075, and what is the one-line read (FEASIBLE / NOISY-RULER /
//     NO-SIGNAL / NEEDS-MORE-N).
//
// The pilot N is tiny (6 Tier-A items, 2 Tier-B scenarios) so the CIs are WIDE
// and usually not significant — that is EXPECTED. The pilot establishes
// σ_noise + direction + plumbing, not statistical power (spec §3, §4.7).
//
// Determinism: the bootstrap is seeded off the replay seed-base so a re-run at
// the same seed-base returns identical CIs (CLAUDE.md "Determinism by default").
// This package reads no wall clock.
package eval

import (
	"math"

	"github.com/berttrycoding/thought-harness/internal/bench/stats"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// MDE is the locked binary minimum-detectable-effect for the pilot (spec §6.0:
// MDE = 0.15 binary). The feasibility gate is MDE/2.
const MDE = 0.15

// FeasibilityThreshold is the σ_noise gate of spec §4.1: a checker whose
// K-averaged σ_noise is below MDE/2 = 0.075 is feasible at this K/temp; above it
// the ruler is too noisy for the effect it must measure.
const FeasibilityThreshold = MDE / 2

// LureAdmitFloor is the bare-model lure/failure-rate admit floor (spec §3.1): an
// item whose bare arm fails below this fraction of replays does not discriminate
// and is flagged.
const LureAdmitFloor = 0.5

// BootstrapReplicates is the BCa resample count for the pilot. The spec default
// is 10k (stats.DefaultBootstrapB); the pilot uses fewer because the paired N is
// tiny and 2000 is already smooth at this N — keeps a long --backend llm run from
// spending seconds per mechanism on bootstrap it does not need.
const BootstrapReplicates = 2000

// ---------------------------------------------------------------------------
// Per-item replay aggregation.
// ---------------------------------------------------------------------------

// ItemArmReplays holds every replay's pass indicator (and isolation indicator)
// for ONE (item, arm) cell. The Phase-0 σ_noise reads the within-cell spread of
// Passes; the lift reads the majority vote.
type ItemArmReplays struct {
	// ItemID is the Tier-A item / Tier-B scenario id this cell scores.
	ItemID string
	// Arm is the arm the replays were run under.
	Arm benchtypes.Arm
	// Passes is one 0/1 pass indicator per replay (len == K).
	Passes []float64
	// Isolations is one 0/1 isolation indicator per replay (len == K) — used to
	// compute the isolation rate on the harness arm's passing replays.
	Isolations []float64
}

// MajorityPass reduces the K replays to a single 0/1 majority-vote pass (spec
// §1.4 lift: "reduce each item to a majority-vote pass per arm over K replays").
// A tie (K even, K/2 passes) is rounded DOWN to a fail — the conservative call.
func (c ItemArmReplays) MajorityPass() float64 {
	if len(c.Passes) == 0 {
		return 0
	}
	var s float64
	for _, p := range c.Passes {
		s += p
	}
	if s > float64(len(c.Passes))/2 {
		return 1
	}
	return 0
}

// FailRate is the fraction of replays the arm got WRONG — the bare-arm value is
// the lure-calibration failure rate (spec §3.1). 0 replays ⇒ 0 (no evidence).
func (c ItemArmReplays) FailRate() float64 {
	if len(c.Passes) == 0 {
		return 0
	}
	var s float64
	for _, p := range c.Passes {
		s += p
	}
	return 1 - s/float64(len(c.Passes))
}

// withinItemVariance is the (population) variance of a 0/1 indicator within one
// item's replays: p(1-p). It is the per-item noise the pooled σ averages.
func withinItemVariance(passes []float64) float64 {
	n := len(passes)
	if n == 0 {
		return 0
	}
	var s float64
	for _, p := range passes {
		s += p
	}
	mean := s / float64(n)
	return mean * (1 - mean)
}

// ---------------------------------------------------------------------------
// Phase-0 σ_noise (spec §4.1).
// ---------------------------------------------------------------------------

// Phase0 is the per-mechanism Phase-0 noise characterization (spec §4.1): the
// pooled within-item σ of the per-replay pass indicator for the harness and bare
// arms, the σ of the paired (harness−bare) per-item difference, and the
// lure-calibration per-item bare failure rates with their admit-floor flags.
type Phase0 struct {
	// K is the replay count the σ's were pooled over.
	K int
	// SigmaHarness is the pooled within-item SD of the per-replay pass indicator
	// for the HARNESS arm (the σ_noise the lift run must beat).
	SigmaHarness float64
	// SigmaBare is the same for the BARE arm (reported for context).
	SigmaBare float64
	// SigmaPairedDiff is the SD of the paired (harness−bare) per-item difference
	// of per-replay means — the noise on the actual contrast.
	SigmaPairedDiff float64
	// LureRates maps item id → bare-arm failure rate (the calibrated lure-emission
	// proxy). An item below LureAdmitFloor is flagged in LureBelowFloor.
	LureRates map[string]float64
	// LureBelowFloor lists the item ids whose bare failure rate is below the 0.5
	// admit floor (they do not discriminate; flag, don't silently keep).
	LureBelowFloor []string
}

// SigmaHarnessAveraged is σ_noise after K-averaging: σ/√K (spec §4.1 "averaging
// cuts σ by √k"). This is the value the feasibility gate compares to MDE/2.
func (p Phase0) SigmaHarnessAveraged() float64 {
	if p.K <= 0 {
		return p.SigmaHarness
	}
	return p.SigmaHarness / math.Sqrt(float64(p.K))
}

// computePhase0 pools the per-item within-replay variance across items for the
// harness and bare arms, and the paired-difference σ across items. cells is the
// per-(item,arm) replay aggregation; K is the replay count.
func computePhase0(cells map[string]map[benchtypes.Arm]*ItemArmReplays, k int) Phase0 {
	p := Phase0{K: k, LureRates: map[string]float64{}}

	var harnVarSum, bareVarSum float64
	var harnN, bareN int
	var diffs []float64

	for itemID, byArm := range cells {
		harn := byArm[benchtypes.ArmHarness]
		bare := byArm[benchtypes.ArmBare]
		if harn != nil {
			harnVarSum += withinItemVariance(harn.Passes)
			harnN++
		}
		if bare != nil {
			bareVarSum += withinItemVariance(bare.Passes)
			bareN++
			fr := bare.FailRate()
			p.LureRates[itemID] = fr
			if fr < LureAdmitFloor {
				p.LureBelowFloor = append(p.LureBelowFloor, itemID)
			}
		}
		// Paired (harness−bare) per-item difference of per-replay means.
		if harn != nil && bare != nil {
			diffs = append(diffs, mean(harn.Passes)-mean(bare.Passes))
		}
	}

	if harnN > 0 {
		p.SigmaHarness = math.Sqrt(harnVarSum / float64(harnN))
	}
	if bareN > 0 {
		p.SigmaBare = math.Sqrt(bareVarSum / float64(bareN))
	}
	p.SigmaPairedDiff = sampleSD(diffs)
	return p
}

// ---------------------------------------------------------------------------
// LIFT (spec §1.4, §4.2, §4.3).
// ---------------------------------------------------------------------------

// Lift is one paired binary contrast reduced to majority-vote passes: the
// McNemar test on the discordant split + the pass-rate difference with a BCa
// 95% CI. Used for both harness−bare and gate-on−gate-off.
type Lift struct {
	// NPairs is the number of items paired across the two arms.
	NPairs int
	// PassA, PassB are the majority-vote pass rates of arm-1 (harness/gate-on) and
	// arm-2 (bare/gate-off).
	PassA, PassB float64
	// McNemar is the exact McNemar verdict on the discordant (b, c) split.
	McNemar stats.McNemarResult
	// Diff is the pass-rate difference (PassA − PassB) with its BCa 95% CI.
	Diff benchtypes.Estimate
}

// computeLift pairs arm-1 vs arm-2 by item (majority-vote pass), runs McNemar on
// the discordant split, and bootstraps a BCa CI on the per-item pass-rate
// difference. seed makes the bootstrap reproducible. Items present in only one
// arm are dropped (an unpaired item cannot enter a paired contrast).
func computeLift(cells map[string]map[benchtypes.Arm]*ItemArmReplays, arm1, arm2 benchtypes.Arm, seed int64) Lift {
	var diffs []float64
	var b, c int // McNemar discordant cells (arm1 pass & arm2 fail; arm1 fail & arm2 pass)
	var sumA, sumB float64
	n := 0
	for _, byArm := range cells {
		a1 := byArm[arm1]
		a2 := byArm[arm2]
		if a1 == nil || a2 == nil {
			continue
		}
		pa := a1.MajorityPass()
		pb := a2.MajorityPass()
		sumA += pa
		sumB += pb
		diffs = append(diffs, pa-pb)
		switch {
		case pa == 1 && pb == 0:
			b++
		case pa == 0 && pb == 1:
			c++
		}
		n++
	}
	lift := Lift{NPairs: n, McNemar: stats.McNemar(b, c)}
	if n > 0 {
		lift.PassA = sumA / float64(n)
		lift.PassB = sumB / float64(n)
	}
	bca := stats.BootstrapBCa(diffs, stats.MeanStat, BootstrapReplicates, seed, 0.05)
	lift.Diff = benchtypes.Estimate{Point: bca.Theta, CILow: bca.Lower, CIHigh: bca.Upper, N: n}
	return lift
}

// IsolationRate is the fraction of HARNESS-arm majority-vote PASSES on which the
// mechanism was genuinely used (isolation witnessed on a majority of replays).
// Spec §1.4: passes that bypass the mechanism are excluded from the numerator.
type IsolationRate struct {
	// Passes is the number of harness items that passed (majority vote).
	Passes int
	// Isolated is the number of those whose isolation also held (majority vote).
	Isolated int
	// Rate is Isolated/Passes (NaN when no harness passes — nothing to isolate).
	Rate float64
}

// computeIsolationRate reads the harness arm's per-item majority pass and
// majority isolation and reports the isolation rate over passing items.
func computeIsolationRate(cells map[string]map[benchtypes.Arm]*ItemArmReplays) IsolationRate {
	ir := IsolationRate{Rate: math.NaN()}
	for _, byArm := range cells {
		harn := byArm[benchtypes.ArmHarness]
		if harn == nil || harn.MajorityPass() != 1 {
			continue
		}
		ir.Passes++
		if majority(harn.Isolations) {
			ir.Isolated++
		}
	}
	if ir.Passes > 0 {
		ir.Rate = float64(ir.Isolated) / float64(ir.Passes)
	}
	return ir
}

// ---------------------------------------------------------------------------
// The per-mechanism × tier result the report renders.
// ---------------------------------------------------------------------------

// Verdict is the one-line feasibility read of a mechanism × tier (spec §4.1).
type Verdict string

const (
	// VerdictFeasible — the K-averaged σ_noise clears MDE/2 AND the harness−bare
	// lift points in the helping direction: the ruler can measure this mechanism
	// at this K/temp (subject to power, which the tiny pilot N does not provide).
	VerdictFeasible Verdict = "FEASIBLE"
	// VerdictNoisyRuler — the K-averaged σ_noise is ABOVE MDE/2: the verifier is
	// noisier than half the effect it must measure. Increase K or lower temp.
	VerdictNoisyRuler Verdict = "NOISY-RULER"
	// VerdictNoSignal — σ is fine but the harness−bare lift is ≤ 0 (the harness
	// does not help on this pilot): no signal to measure.
	VerdictNoSignal Verdict = "NO-SIGNAL"
	// VerdictNeedsMoreN — σ is fine and the direction is positive but the CI is
	// wide / not significant at the pilot N (the expected pilot outcome): the
	// ruler is feasible but under-powered, needs the full N.
	VerdictNeedsMoreN Verdict = "NEEDS-MORE-N"
)

// MechResult is the full per-mechanism × tier pilot computation the report
// renders and the ledger records: the Phase-0 noise, both lift contrasts, the
// isolation rate, and the feasibility verdict.
type MechResult struct {
	// Mechanism + Tier identify the cell.
	Mechanism benchtypes.Mechanism
	// Tier is A or B.
	Tier benchtypes.Tier
	// K is the replay count.
	K int
	// Items is the number of distinct items/scenarios scored.
	Items int
	// GateOffSupported is false when the mechanism's ablation toggle does not yet
	// exist (the gate-on−gate-off contrast is then UNSUPPORTED, not a failure).
	GateOffSupported bool
	// Phase0 is the σ_noise characterization.
	Phase0 Phase0
	// HarnessMinusBare is the total-lift contrast.
	HarnessMinusBare Lift
	// GateOnMinusGateOff is the mechanism-specific (load-bearing) contrast. Zeroed
	// NPairs when GateOffSupported is false.
	GateOnMinusGateOff Lift
	// Isolation is the harness-arm isolation rate over passing items.
	Isolation IsolationRate
	// Verdict is the one-line feasibility read.
	Verdict Verdict
}

// Feasible reports whether the K-averaged harness σ_noise clears the MDE/2 gate.
func (r MechResult) Feasible() bool {
	return r.Phase0.SigmaHarnessAveraged() < FeasibilityThreshold
}

// Contrast projects the two lifts + isolation rate into the shared
// types.Contrast the ledger verdict row records (spec §1.4, §4.6).
func (r MechResult) Contrast() *benchtypes.Contrast {
	iso := benchtypes.Estimate{Point: r.Isolation.Rate, N: r.Isolation.Passes}
	return &benchtypes.Contrast{
		HarnessMinusBare:   r.HarnessMinusBare.Diff,
		GateOnMinusGateOff: r.GateOnMinusGateOff.Diff,
		IsolationRate:      iso,
	}
}

// Summarize runs the full per-mechanism × tier pilot reduction over the collected
// per-(item,arm) replay cells: Phase-0, both lift contrasts, the isolation rate,
// and the feasibility verdict. seed seeds the bootstrap (reproducible CIs).
// gateOffSupported flags whether the ablation arm ran (a missing toggle leaves
// the mechanism-specific contrast UNSUPPORTED rather than fabricating it).
func Summarize(
	mech benchtypes.Mechanism, tier benchtypes.Tier, k int,
	cells map[string]map[benchtypes.Arm]*ItemArmReplays,
	gateOffSupported bool, seed int64,
) MechResult {
	r := MechResult{
		Mechanism:        mech,
		Tier:             tier,
		K:                k,
		Items:            len(cells),
		GateOffSupported: gateOffSupported,
		Phase0:           computePhase0(cells, k),
		HarnessMinusBare: computeLift(cells, benchtypes.ArmHarness, benchtypes.ArmBare, seed),
		Isolation:        computeIsolationRate(cells),
	}
	if gateOffSupported {
		// gate-on == harness by design (the driver runs harness as both the
		// total-lift arm-1 and the gate-on arm; it never emits a separate
		// ArmGateOn cell). Pair the harness cells against gate-off.
		r.GateOnMinusGateOff = computeLift(cells, benchtypes.ArmHarness, benchtypes.ArmGateOff, seed+1)
	}
	r.Verdict = deriveVerdict(r)
	return r
}

// deriveVerdict applies the §4.1 feasibility logic to a computed MechResult. The
// order is: noisy ruler first (the gate the spec calls non-negotiable), then
// no-signal (direction wrong), then needs-more-N (positive direction but the CI
// does not exclude 0 at the pilot N — the expected pilot outcome), else feasible.
func deriveVerdict(r MechResult) Verdict {
	if !r.Feasible() {
		return VerdictNoisyRuler
	}
	point := r.HarnessMinusBare.Diff.Point
	if math.IsNaN(point) || point <= 0 {
		return VerdictNoSignal
	}
	// Positive direction. If the BCa lower bound clears 0 the ruler shows signal at
	// the pilot N; otherwise it is feasible-but-underpowered (the pilot's job is to
	// say so, not to claim significance — spec §3, §4.7).
	if r.HarnessMinusBare.Diff.CILow > 0 {
		return VerdictFeasible
	}
	return VerdictNeedsMoreN
}

// ---------------------------------------------------------------------------
// small numeric helpers (kept local so eval has no cross-package math dep).
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

// sampleSD is the sample (n−1) standard deviation; <2 values ⇒ 0 (no spread).
func sampleSD(xs []float64) float64 {
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
	return math.Sqrt(ss / float64(n-1))
}

// majority reports whether the 0/1 indicators are mostly 1 (strict majority).
func majority(xs []float64) bool {
	if len(xs) == 0 {
		return false
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s > float64(len(xs))/2
}
