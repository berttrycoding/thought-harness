package stability

// consistency.go is SLAM M5 — the CONSISTENCY / OBSERVABILITY INVARIANT as a STANDING durability check,
// distinct from the five control-theoretic conditions (n<1, U<=1, 0<K*g<2, mu>0, bounded fan-out).
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §5 #7 ("A consistency invariant in the
// durability/stability gate — a new automated check alongside the existing five conditions: the estimator
// never gains information in unobservable directions") + §5b ("M5 is not optional for awake") + §6 (M5);
// docs/internal/notes/2026-06-20-slam-M1-build-spec.md §7/§9 ("M5 must be in for awake go-live").
//
// WHY IT IS A DURABILITY OBLIGATION, NOT A SIGNAL ITEM: a continuously-running self-estimator that gains
// SPURIOUS information (the Huang-2010 EKF-inconsistency: rank-1 of fabricated certainty in the
// unobservable global-frame direction per update) compounds over a long AWAKE run into catastrophic
// overconfidence — confidently-wrong, the exact "feels dumb" failure mode. Reactive episodes are too
// short to see it; awake mode is exactly where it bites. So the invariant — "every bit of certainty the
// estimator gains is justified by a grounded observation; it gains ZERO information in unobservable
// directions" — is a genuine awake-durability requirement, re-MEASURED on every change rather than
// hand-derived, and REQUIRED-WITH-M1 before any awake go-live.

import (
	"fmt"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/estimate"
	"github.com/berttrycoding/thought-harness/internal/grounding"
)

// slamConsistencyFeatures is the awake flag-ON stack with the SLAM M1 estimator AND its M5 consistency
// monitor turned ON. The estimator + monitor are PURE CONTROL (control.Innovate + closed-form information
// accounting, no model call, no fan-out, no actuation of theta) and the monitor is a pure WITNESS that
// never alters the estimate — so this plant's control dimensions (n fork-depth, U schedulability, fan-out
// width, mu baseline, K*g loop gain) are IDENTICAL to the flag-OFF awake plant. The point of the cell is
// the SIXTH obligation (the consistency invariant), measured on the same awake stack the gate validates.
func slamConsistencyFeatures() *engine.Engine {
	feat := awakeFlagOnFeatures() // the same flag-ON awake stack the gate already validates
	feat.Slam.Innovation = true   // M1: the explicit innovation/residual on the action->reality path
	feat.Slam.Consistency = true  // M5: the consistency/observability monitor (the witness under test)
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7 // the deterministic awake seed the other awake cells pin
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.slamConsistencyFeatures: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(24)
	return e
}

// ConsistencyInvariantHolds is the SLAM M5 standing durability cell: the failable, re-measured check that
// the self-state estimator gains NO spurious information in unobservable directions — the awake-durability
// obligation the design (§5 #7, §5b) names as a sixth condition alongside the five control-theoretic ones.
//
// It establishes the invariant in two complementary ways:
//
//  1. THE STRESS PROBE (deterministic, the load-bearing part). It drives a bare estimator with the worst
//     case for the inconsistency — a CONFIDENT belief restated MANY times (the self-reinforcement that an
//     EKF re-linearizing at its current estimate would convert into fabricated certainty) before any
//     reality grounds it. A consistent estimator gains ZERO information from the restatements (variance
//     stays at the high prior); only a real grounded observation then shrinks variance, and that gain is
//     attributed as GROUNDED, never spurious. This is the precise Huang-2010 failure mode, encoded so a
//     regression that let a self-restatement (or a gated/unassociated observation) shrink variance fails
//     here LOUDLY — the check could not pass vacuously.
//
//  2. THE LIVE-PLANT WITNESS. It runs the awake flag-ON plant with M1+M5 ON and asserts the estimator's
//     run-long consistency witness reports consistent (spuriousGain == 0) — the invariant held over the
//     actual continuous loop, not just the probe — and that the five control-theoretic conditions STILL
//     pass on this plant (the estimator + monitor are pure CONTROL, so they must not move the plant).
//
// The plant is the offline TestBackend double + the seeded RNG, so the cell is reproducible and needs no
// model (the M5 invariant is pure CONTROL — closed-form information accounting — so the canned-content
// double is sufficient here; a CONTENT/cognition slice would additionally need live claude).
func ConsistencyInvariantHolds() Report {
	rep := Report{Workload: "SLAM M5 consistency/observability invariant (no spurious info gain — awake gate)"}

	// (1) THE STRESS PROBE — the Huang-2010 worst case, deterministic.
	cons := stressEstimatorConsistency()
	rep.Checks = append(rep.Checks, Check{
		Name:   "no spurious information gain under confident self-restatement (stress probe)",
		OK:     cons.Consistent() && cons.Violations == 0,
		Detail: detailSpurious(cons),
	})
	// The probe must be NON-VACUOUS: it must actually exercise BOTH the self-restatement path (Notes>0)
	// and a grounded observation that DID legitimately gain information (GroundedGain>0). Else a do-nothing
	// estimator would trivially have zero spurious gain and the cell would gate nothing.
	rep.Checks = append(rep.Checks, Check{
		Name:   "stress probe exercised the estimator (self-restatement + a real grounded gain)",
		OK:     cons.Notes > 0 && cons.GroundedGain > 0,
		Detail: "the probe did not exercise both paths — the invariant would be vacuously satisfied (notes / grounded-gain absent)",
	})

	// (2) THE LIVE-PLANT WITNESS — the awake flag-ON plant with M1+M5 ON.
	//
	// The estimator's consistency monitor must be ACTIVE on the live plant (both knobs synced ON), and the
	// invariant must hold over the run (no spurious gain accrued). On the offline test-double awake plant the
	// grounding path is rarely exercised (the canned-content double does not open to reality), so the witness
	// state is typically vacuously consistent here — that is correct: this cell's job is the DURABILITY
	// obligation (the invariant never breaks + the plant does not move). The proof that the runtime witness
	// EMITS on a live grounding cycle is the engine cognition-property test
	// TestSLAM_M5_NoFabricatedCertaintyUnderRestatement (which drives a real groundObservation + Step), and the
	// proof on a CONTENT-grounding run is the live-claude path (see notes).
	e := slamConsistencyFeatures()
	live, ok := e.EstimatorConsistency()
	rep.Checks = append(rep.Checks, Check{
		Name:   "consistency monitor active on the awake plant (M1+M5 ON)",
		OK:     ok,
		Detail: "the estimator's consistency monitor was not active with both knobs ON — the live witness was not measured",
	})
	rep.Checks = append(rep.Checks, Check{
		Name:   "no spurious information gain over the awake run (live plant)",
		OK:     live.Consistent() && live.Violations == 0,
		Detail: detailSpurious(live),
	})

	// The five control-theoretic conditions must STILL hold on the M1+M5 plant — the estimator + monitor
	// are pure CONTROL and must not move the plant. Fold them in so this cell re-passes the durability gate
	// for the M5 plant rather than the gate being hand-derived (continuous-mode-operator obligation).
	ctrl := CheckEngine(e, "control conditions on the M1+M5 awake plant", "continuous")
	rep.Checks = append(rep.Checks, ctrl.Checks...)
	rep.Regime = ctrl.Regime
	rep.GainMeasured = ctrl.GainMeasured
	rep.Metrics = ctrl.Metrics
	rep.Warnings = append(rep.Warnings, ctrl.Warnings...)
	// Carry the loop-gain telemetry-only verdict from the folded-in control checks: on this saturated/open
	// awake plant g is not identified, so the K·g entry is UNVALIDATED telemetry — it must be excluded from
	// this cell's held-conditions count and surfaced as the explicit telemetry-only line, exactly as on the
	// other awake rows (never banked as a held control-theoretic check).
	rep.KgTelemetryOnly = ctrl.KgTelemetryOnly
	return rep
}

// stressEstimatorConsistency drives a bare M1 estimator (M5 monitor ON) through the Huang-2010 worst case
// and returns the consistency witness. The sequence: declare a CONFIDENT belief, restate it MANY times
// (the self-reinforcement that fabricates certainty in an inconsistent estimator), then ground it with a
// real observation. A consistent estimator: variance holds at the prior through every restatement (zero
// info gain), then shrinks ONCE on the grounded observation (a GROUNDED gain). Deterministic — no RNG, no
// clock, no model.
func stressEstimatorConsistency() estimate.Consistency {
	cfg := estimate.DefaultConfig()
	cfg.Enabled = true
	cfg.Monitor = true
	est := estimate.New(cfg, nil) // nil bus: the probe reads the witness directly, no event needed

	const id = estimate.BeliefID("stress-belief")
	// Restate the SAME confident belief many times — each restatement carries no new information about the
	// world, so a consistent estimator gains nothing. An inconsistent one (a regression that let Note shrink
	// variance) would fabricate certainty here and the cell would catch it.
	for i := 0; i < 20; i++ {
		est.Note(id, 0.95) // a high-confidence self-assertion
	}
	// Now reality grounds the belief at the gold trust tier — the ONE legitimate (observable) info gain.
	est.Observe(id, +1.0, control.TierPrecision(int(grounding.TierFirsthandValidated)))
	// A second belief restated then REFUTED hard at the lowest tier exercises the data-association gate (a
	// gated obs must not shrink variance) — the other spurious-gain tripwire.
	const id2 = estimate.BeliefID("stress-belief-2")
	est.Note(id2, 0.99)
	est.Observe(id2, -1.0, control.TierPrecision(int(grounding.TierTestimony)))
	return est.ConsistencyState()
}

// detailSpurious renders the failure detail for a spurious-gain check (only shown when the check FAILS).
func detailSpurious(c estimate.Consistency) string {
	return fmt.Sprintf("estimator gained spurious information in an unobservable direction (Huang-2010 inconsistency): "+
		"spuriousGain=%.4f violations=%d (groundedGain=%.4f notes=%d obs=%d)",
		c.SpuriousGain, c.Violations, c.GroundedGain, c.Notes, c.Observations)
}
