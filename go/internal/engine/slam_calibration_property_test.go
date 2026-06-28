package engine

// slam_calibration_property_test.go — the COGNITION property for SLAM M9 (Track F, calibration
// meta-estimation): the harness LEARNS each source's reliability from the M1 predicted-vs-actual
// residual stream and RE-ESTIMATES the measurement precision R it trusts — the lever on the measured
// same-model self-judging ceiling. These assert the THINKING the spec intends (the system DISCOVERS its
// confident self-predictions are systematically refuted and DOWN-WEIGHTS that source, instead of
// trusting a fixed prior forever), not merely that the loop runs. The flag-OFF half pins byte-identical
// wire behaviour. Mirrors slam_estimation_property_test.go's discipline.

import (
	"strconv"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/grounding"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// calEngine builds a reactive engine on the test double with slam.innovation ON (M9 needs the M1
// residual stream) and slam.calibration set as asked. Returns the engine + a captured event log.
func calEngine(t *testing.T, calOn bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn; SLAM defaults OFF
	feat.Slam.Innovation = true
	feat.Slam.Calibration = calOn
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &slamLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// intentN builds an Intention with a distinct claim per outcome, so the grounding ledger keys each
// distinctly (one refuting observation per belief). realObs grounds these at the firsthand-observation
// tier (ordinal 3 — the precision tierPrec3 below).
func intentN(i int) types.Intention {
	return types.Intention{Claim: "claim number " + strconv.Itoa(i) + " holds"}
}

// tierPrec3 is the FIXED prior precision of the firsthand-observation tier that realObs grounds at —
// the value the estimator uses with calibration OFF, and the baseline M9 must beat when it down-weights.
func tierPrec3() float64 { return control.TierPrecision(int(grounding.TierFirsthandObservation)) }

// TestSLAM_M9_LearnsLowReliabilityFromRefutedSelfPredictions is the headline same-model-ceiling
// cognition (G9): when the model CONFIDENTLY asserts beliefs that reality SYSTEMATICALLY refutes, the
// calibration meta-estimator LEARNS that the source's reliability is low and RE-ESTIMATES its precision
// DOWNWARD (learnedPrec < priorPrec) — the system DISCOVERS it is overconfident against that source
// instead of trusting the fixed prior. This is the thinking a fixed-prior estimator structurally cannot
// do.
func TestSLAM_M9_LearnsLowReliabilityFromRefutedSelfPredictions(t *testing.T) {
	e, log := calEngine(t, true)
	// drive >= the identification gate (8) of confident-but-refuted outcomes at the same tier.
	for i := 0; i < 10; i++ {
		pushBelief(t, e, "this is definitely correct", 0.95)
		e.groundObservation(intentN(i), realObs(false, "structured"))
	}

	cals := log.of(events.EstimateCalibrate)
	if len(cals) == 0 {
		t.Fatal("M9 ON: a grounded residual stream must emit estimate.calibrate (the learned R)")
	}
	last := cals[len(cals)-1]
	samples, _ := last.Data["samples"].(int)
	hitRate := floatData(t, last, "hitRate")
	priorPrec := floatData(t, last, "priorPrec")
	learnedPrec := floatData(t, last, "learnedPrec")
	overconf := floatData(t, last, "overconfidence")
	measured, _ := last.Data["measured"].(bool)

	if samples < 8 {
		t.Fatalf("the tier must accumulate past the identification gate; samples=%v", samples)
	}
	if !measured {
		t.Fatalf("an identified tier must report measured=true; got false (samples=%v)", samples)
	}
	// the model was confidently WRONG every time -> hit-rate ~0 -> the learned precision is DOWN-weighted.
	if hitRate > 0.1 {
		t.Fatalf("systematically-refuted confident predictions must show a low hit-rate; hitRate=%v", hitRate)
	}
	if learnedPrec >= priorPrec {
		t.Fatalf("M9 must DOWN-weight a systematically-refuted source below the fixed prior; prior=%v learned=%v", priorPrec, learnedPrec)
	}
	// and it surfaces the overconfidence (confident assertions reality refuted) — the ceiling signal.
	if overconf < 0.9 {
		t.Fatalf("10/10 confident refutes must read as overconfidence ~1.0; overconfidence=%v", overconf)
	}

	// the engine-level vitals expose the same alarm.
	idTiers, worstTier, worstOverconf, ok := e.CalibrationVitals()
	if !ok {
		t.Fatal("CalibrationVitals must report ok when slam.calibration is on")
	}
	if idTiers == 0 || worstTier < 0 || worstOverconf < 0.9 {
		t.Fatalf("vitals must surface the overconfident tier; identified=%d worstTier=%d worstOverconf=%v",
			idTiers, worstTier, worstOverconf)
	}
}

// TestSLAM_M9_ReWeightsEstimatorPrecision is the WIRING-into-the-estimator proof: the learned R is not
// a sidecar number — it changes the precision the M1 innovation update actually uses. After the
// calibrator has learned a low reliability for the tier, the NEXT grounded observation's estimate.
// innovate event reports an obsPrec BELOW the fixed prior (the down-weighting reached the live Kalman
// update). With calibration OFF, the same sequence reports obsPrec == the fixed prior.
func TestSLAM_M9_ReWeightsEstimatorPrecision(t *testing.T) {
	priorPrec := tierPrec3() // FirsthandObservation precision (the fixed prior)

	// ON: learn a low reliability, then read the next innovation's obsPrec.
	eOn, logOn := calEngine(t, true)
	for i := 0; i < 10; i++ {
		pushBelief(t, eOn, "this is definitely correct", 0.95)
		eOn.groundObservation(intentN(i), realObs(false, "structured"))
	}
	innovsOn := logOn.of(events.EstimateInnovate)
	if len(innovsOn) == 0 {
		t.Fatal("M9 ON: expected estimate.innovate events")
	}
	obsPrecOn := floatData(t, innovsOn[len(innovsOn)-1], "obsPrec")
	if obsPrecOn >= priorPrec {
		t.Fatalf("M9 ON: after learning low reliability the estimator's obsPrec must drop below the prior; prior=%v got=%v", priorPrec, obsPrecOn)
	}

	// OFF (M1 only): the same sequence keeps the FIXED prior precision (byte-identical M1 behaviour).
	eOff, logOff := calEngine(t, false)
	for i := 0; i < 10; i++ {
		pushBelief(t, eOff, "this is definitely correct", 0.95)
		eOff.groundObservation(intentN(i), realObs(false, "structured"))
	}
	innovsOff := logOff.of(events.EstimateInnovate)
	if len(innovsOff) == 0 {
		t.Fatal("M1: expected estimate.innovate events")
	}
	obsPrecOff := floatData(t, innovsOff[len(innovsOff)-1], "obsPrec")
	if obsPrecOff != priorPrec {
		t.Fatalf("M9 OFF: the estimator must use the FIXED prior precision; prior=%v got=%v", priorPrec, obsPrecOff)
	}
	if len(logOff.of(events.EstimateCalibrate)) != 0 {
		t.Fatal("M9 OFF: no estimate.calibrate event may fire (the calibrator is inert)")
	}
}

// TestSLAM_M9_FlagOffByteIdenticalWire is the default-OFF guarantee at the engine level: with
// slam.calibration OFF (even with slam.innovation ON), the SAME grounded sequence emits ZERO
// estimate.calibrate events and CalibrationVitals reports not-ok — the calibrator is inert and the M1
// behaviour is byte-identical.
func TestSLAM_M9_FlagOffByteIdenticalWire(t *testing.T) {
	e, log := calEngine(t, false)
	for i := 0; i < 10; i++ {
		pushBelief(t, e, "this is definitely correct", 0.95)
		e.groundObservation(intentN(i), realObs(false, "structured"))
	}
	if got := log.of(events.EstimateCalibrate); len(got) != 0 {
		t.Fatalf("M9 OFF: estimate.calibrate must not fire; got %d", len(got))
	}
	if _, _, _, ok := e.CalibrationVitals(); ok {
		t.Fatal("M9 OFF: CalibrationVitals must report not-ok (calibrator inert)")
	}
	// the M1 wire is untouched (the residual stream still flows).
	if len(log.of(events.EstimateInnovate)) == 0 {
		t.Fatal("M9 OFF: the M1 estimate.innovate wire must still fire (byte-identical)")
	}
}
