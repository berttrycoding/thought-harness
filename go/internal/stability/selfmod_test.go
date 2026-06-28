package stability

// selfmod_test.go is the STABILITY-AXIS test suite: durability as a function of self-modification level.
// It is the standing guard over the third eval axis (after capability + efficiency). Everything here runs
// offline + deterministic on the TestBackend double + seed=7 (the seeded cpyrand RNG) — the durability
// math needs no model.
//
// The three obligations the task names:
//   1. the durability-vs-level CURVE holds across ALL levels (the regulated-regime claim) — the standing
//      guard TestSelfModCurveHoldsAcrossAllLevels;
//   2. each level genuinely EXERCISED its self-modification mechanism (the curve is non-vacuous) — asserted
//      per-row in the same guard via the self-mod evidence;
//   3. the curve is FAILABLE, not a vacuous all-pass (the C0b lesson) — TestSelfModAxisIsFailable.
//
// A verbose characterization (TestCharacterizeSelfModCurve, -v only) prints the per-level condition table.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// TestCharacterizeSelfModCurve prints the durability-vs-self-mod-level table (run with -v). Deterministic
// (TestBackend double, seed=7); verbose only — it asserts nothing (the standing guard is
// TestSelfModCurveHoldsAcrossAllLevels). This is the curve a reader/red-team inspects.
func TestCharacterizeSelfModCurve(t *testing.T) {
	curve := MeasureSelfModCurve()
	t.Log("\n" + FormatSelfModCurve(curve))
}

// TestSelfModCurveHoldsAcrossAllLevels is the load-bearing stability-axis guard: the durable regime holds
// at EVERY self-modification level, AND each level genuinely exercised its mechanism (so the curve is not
// a vacuous all-pass measuring nothing). It is the regulated-regime claim, MEASURED: deeper
// self-modification (mint → synthesise → grow the catalog → awake self-sustain) keeps the five conditions
// inside their bounds. Offline TestBackend double, seed=7 (deterministic).
//
// The per-level EVIDENCE assertions are the anti-vacuity half: a level whose mechanism never fired would
// report DURABLE while measuring nothing. Each level must show the self-modification it claims:
//   - L1 mints at least one reflex (specialist or skill);
//   - L2 synthesises programs AND fans out sub-agents;
//   - L3 grows the operator catalog (the dimension self-modifies);
//   - L4 plants the full seed-intent forest AND keeps a positive awake baseline μ.
func TestSelfModCurveHoldsAcrossAllLevels(t *testing.T) {
	curve := MeasureSelfModCurve()
	if len(curve) != len(SelfModLevels()) {
		t.Fatalf("curve has %d levels, want %d", len(curve), len(SelfModLevels()))
	}
	for _, r := range curve {
		r := r
		t.Run(r.Level.String(), func(t *testing.T) {
			// (1) the durable regime holds at this level (the five conditions, the standing CheckEngine path).
			if !r.Durable() {
				t.Fatalf("self-mod level %s broke the durable regime:\n%s", r.Level, r.Report.Format())
			}
			// the regime is benign at every level: actively-controlled (a real K·g bool), saturated-bounded
			// (θ pinned, open loop, K·g vacuous), or insufficient-loop (a short transient). NEVER a control-
			// loss rail (saturated-runaway / unidentified-active) — those are the honest fails.
			switch r.Regime {
			case regulator.RegimeSaturatedRunaway, regulator.RegimeUnidentifiedActive:
				t.Fatalf("self-mod level %s landed in a control-loss regime %s (not benign)", r.Level, r.Regime)
			}
			// the five conditions hold at their worst-case (every-tick) peaks, encoded as bounds.
			if !(r.PeakN < 1.0 && r.PeakU <= 1.0 && r.MaxFanout <= cognition.MaxParWidth && r.MaxOutstand <= cognition.MaxParWidth) {
				t.Fatalf("self-mod level %s violated a peak bound: peak_n=%.2f peak_U=%.2f fanout=%d outstanding=%d",
					r.Level, r.PeakN, r.PeakU, r.MaxFanout, r.MaxOutstand)
			}

			// (2) the level genuinely exercised its self-modification mechanism (the curve is non-vacuous).
			switch r.Level {
			case L0Fixed:
				// the baseline is the STATIC plant: nothing minted, nothing synthesised, no fan-out.
				if r.MintedOps != 0 || r.MintedSpec != 0 || r.MintedSkill != 0 || r.SynthEvents != 0 || r.SubagentFire != 0 {
					t.Fatalf("L0 must be the static-plant baseline (no self-modification), got %+v", r)
				}
			case L1Convert:
				if r.MintedSpec+r.MintedSkill < 1 {
					t.Fatalf("L1 must mint at least one reflex (specialist/skill), got spec=%d skill=%d",
						r.MintedSpec, r.MintedSkill)
				}
			case L2Synthesize:
				if r.SynthEvents < 1 || r.SubagentFire < 1 {
					t.Fatalf("L2 must synthesise programs AND fan out sub-agents, got synth=%d subagents=%d",
						r.SynthEvents, r.SubagentFire)
				}
			case L3OperatorMint:
				if r.MintedOps < 1 {
					t.Fatalf("L3 must grow the operator catalog (the dimension self-modifies), got ops=%d", r.MintedOps)
				}
			case L4Awake:
				if r.SeedRoots < 1 {
					t.Fatalf("L4 must plant the seed-intent forest, got seeds=%d", r.SeedRoots)
				}
				if r.MinMu <= 0.0 {
					t.Fatalf("L4 must hold a positive awake baseline μ>0, got min_mu=%.2f", r.MinMu)
				}
			}
		})
	}
}

// TestSelfModCurveRegressionBounds pins the WORST-CASE condition values per level so a future change that
// tightens a condition toward its cliff cannot slip in silently (the C1 lesson: a silent climb is the
// regression this guard exists to catch). MEASURED 2026-06-18 (every-tick peak, seed=7, TestBackend):
//   - L0–L3 (reactive): peak_n=0.00, peak_U=0.12. HONEST READING (red-team-corrected): n=0 here is NOT
//     evidence that self-modification depth is excitation-neutral — it is that these workloads did not
//     EXERCISE the branching channel (the only thing that moves n): their gate conflicts fire while a
//     synthesized workflow is pending and are swallowed by `WorkflowPending → THINK` before BRANCH
//     (controller.go:788, above the conflict check). A non-workflow reactive goal that DOES branch hits
//     peak_n≈0.38 — i.e. the regulator BOUNDS excitation (n stays ~0.38 even when the channel is hammered
//     40x), it is not that the channel is silent at depth. The durability conclusion holds (and is stronger:
//     the regulator caps n well below the cliff even when exercised); the "excitation-free" causal gloss is
//     RETIRED.
//   - L4 (awake): peak_n=0.39, peak_U=1.00 — the awake forest is the ONLY level that excites the fork ratio
//     and SATURATES the schedule (peak_U sits at the U==1 boundary, the zero-margin saturated-bounded
//     schedule). This is the single place the curve TIGHTENS as self-modification deepens.
//
// Bounds are set with headroom (peak_n<0.50 = ~2.5x to the n=1 cliff); raise only with a documented
// re-measure.
func TestSelfModCurveRegressionBounds(t *testing.T) {
	curve := MeasureSelfModCurve()
	byLevel := map[SelfModLevel]SelfModResult{}
	for _, r := range curve {
		byLevel[r.Level] = r
	}
	// the reactive levels (L0-L3) stay deep in the safe region — n flat, U flat.
	for _, l := range []SelfModLevel{L0Fixed, L1Convert, L2Synthesize, L3OperatorMint} {
		r := byLevel[l]
		if r.PeakN > 0.10 {
			t.Fatalf("%s: reactive peak_n climbed to %.2f (measured 0.00; bound 0.10) — self-mod began exciting n", l, r.PeakN)
		}
		if r.PeakU > 0.30 {
			t.Fatalf("%s: reactive peak_U climbed to %.2f (measured 0.12; bound 0.30)", l, r.PeakU)
		}
	}
	// the awake level (L4) is the one that tightens — n excited, U at the saturation boundary.
	awake := byLevel[L4Awake]
	if awake.PeakN > 0.50 {
		t.Fatalf("L4 awake excited branching past the recorded bound: peak_n=%.2f > 0.50 (measured 0.39)", awake.PeakN)
	}
	if awake.PeakU > 1.0 {
		t.Fatalf("L4 awake over-subscribed the schedule: peak_U=%.2f > 1.0", awake.PeakU)
	}
}

// TestSelfModAxisIsFailable is the C0b anti-vacuity proof: the curve's verdict machinery is FAILABLE, not
// a tautological all-pass. A Report built (through the EXACT regulator→Report path the curve uses) over a
// plant deliberately driven SUPERCRITICAL (n past the n=1 cliff) MUST read NOT-durable. If this passed, a
// real condition violation could never be detected and the whole axis would be meaningless. Offline +
// deterministic (a fixed xorshift drive — no RNG, no wall clock).
func TestSelfModAxisIsFailable(t *testing.T) {
	rep := MutatedSupercriticalReport()
	if rep.OK() {
		t.Fatalf("the supercritical mutation must read NOT-durable — the stability axis is a vacuous all-pass:\n%s",
			rep.Format())
	}
	// the n<1 condition specifically must be the (or a) violated one — the mutation drove n past the cliff.
	var nCheck *Check
	for i := range rep.Checks {
		if strings.HasPrefix(rep.Checks[i].Name, "n<1") {
			nCheck = &rep.Checks[i]
		}
	}
	if nCheck == nil || nCheck.OK {
		t.Fatalf("the supercritical mutation must violate the n<1 condition, got %+v", nCheck)
	}
	// and the regime must be the control-loss FAIL (saturated-runaway), not a benign open loop.
	if rep.Regime != regulator.RegimeSaturatedRunaway.String() {
		t.Fatalf("the supercritical mutation regime = %q, want saturated-runaway-FAIL", rep.Regime)
	}
}

// TestSelfModCurveIsDeterministic re-runs the curve and confirms the verdict + worst-case values are
// byte-stable across runs (the seeded RNG must make the durability ticks reproducible — the hygiene rule).
func TestSelfModCurveIsDeterministic(t *testing.T) {
	a := MeasureSelfModCurve()
	b := MeasureSelfModCurve()
	if len(a) != len(b) {
		t.Fatalf("curve length changed across runs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Level != b[i].Level || a[i].Durable() != b[i].Durable() ||
			a[i].PeakN != b[i].PeakN || a[i].PeakU != b[i].PeakU ||
			a[i].MaxFanout != b[i].MaxFanout || a[i].MaxOutstand != b[i].MaxOutstand ||
			a[i].MintedOps != b[i].MintedOps || a[i].SeedRoots != b[i].SeedRoots {
			t.Fatalf("self-mod curve non-deterministic at level %s:\n A=%+v\n B=%+v", a[i].Level, a[i], b[i])
		}
	}
}
