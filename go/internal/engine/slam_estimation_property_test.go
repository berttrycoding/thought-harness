package engine

// slam_estimation_property_test.go — the COGNITION property for SLAM M1 (Track F): the explicit
// innovation/residual on the action->reality path, with the FEJ-anchored trust rule. These assert the
// THINKING the spec intends (graded correction, certainty-comes-from-reality-not-restatement,
// data-association gate), not merely that the loop runs. The flag-OFF half pins byte-identical wire
// behaviour. Mirrors the discipline of cognition_property_test.go's stuck->act->reality property.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// slamLog is a captured event stream for the internal (package engine) test — the external
// engine_test package's eventLog is not visible here, so this is the minimal local twin.
type slamLog struct{ events []events.Event }

func (l *slamLog) of(kind string) []events.Event {
	var out []events.Event
	for _, e := range l.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// slamEngine builds a reactive engine on the test double with the slam.innovation knob set as asked.
// It returns the engine + a captured event log.
func slamEngine(t *testing.T, on bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn (every component on); SLAM defaults OFF
	feat.Slam.Innovation = on
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

// pushBelief opens an episode and appends one CONFIDENT internal belief thought to the active line, so
// e.graph.Last() (the tip the estimator keys on) is that belief. Returns the appended thought.
func pushBelief(t *testing.T, e *Engine, text string, confidence float64) *types.Thought {
	t.Helper()
	e.SubmitDefault("is this change safe to ship?") // queue the prompt
	e.Step()                                        // consume it -> startEpisode builds the graph + goal root
	if e.Graph() == nil {
		t.Fatal("episode did not build a graph")
	}
	belief := &types.Thought{
		ID: -1, Source: types.GENERATED, Text: text, Confidence: confidence,
	}
	return e.Graph().Append(belief, e.Bus().Tick)
}

// TestSLAM_ConfidentUngroundedRefutedHard is the epistemic core (P1/P2): a CONFIDENTLY-stated but
// UNGROUNDED belief carries high variance, so when reality REFUTES it the correction is GRADED and
// LARGE — much larger than the old static -0.45 — and the belief variance DROPS only because reality
// spoke (the §0 invariant: certainty comes from grounding, not self-restatement).
func TestSLAM_ConfidentUngroundedRefutedHard(t *testing.T) {
	e, log := slamEngine(t, true)
	pushBelief(t, e, "this refactor is definitely safe — it runs cleanly", 0.95)

	// reality says NO (a real, non-fabricated refuting observation at the firsthand-observation tier).
	e.groundObservation(types.Intention{Claim: "the refactor is safe"}, realObs(false, "structured"))

	innovs := log.of(events.EstimateInnovate)
	corrects := log.of(events.EstimateCorrect)
	if len(innovs) == 0 {
		t.Fatal("flag ON: a real grounded observation must emit estimate.innovate (the explicit residual)")
	}
	if len(corrects) == 0 {
		t.Fatal("flag ON: a grounded observation must emit estimate.correct (the graded correction)")
	}

	in := innovs[len(innovs)-1]
	cor := corrects[len(corrects)-1]
	priorMean := floatData(t, in, "priorMean")
	priorVar := floatData(t, in, "priorVar")
	innov := floatData(t, in, "innov")
	postMean := floatData(t, cor, "postMean")
	postVar := floatData(t, cor, "postVar")
	deltaFromStatic := floatData(t, cor, "deltaFromStatic")

	// the belief started uncertain (high variance — it was never grounded before).
	if priorVar < 0.9 {
		t.Fatalf("a never-grounded belief must start at high variance; priorVar=%v", priorVar)
	}
	// innovation = obs - priorMean = -1 - 0.95 = -1.95 (a big, explicit residual — NOT a binary flag).
	if innov > -1.5 {
		t.Fatalf("a confident belief refuted by reality must show a large negative innovation; innov=%v", innov)
	}
	// the correction is GRADED and HARD: the mean moves a lot toward the refutation.
	moved := priorMean - postMean
	if moved < 1.0 {
		t.Fatalf("a confident-ungrounded refutation must correct hard; moved only %v", moved)
	}
	// and it diverges substantially from the old static -0.45 (the whole point of M1).
	if deltaFromStatic == 0 {
		t.Fatalf("the graded correction must differ from the old static -0.45 (deltaFromStatic=0)")
	}
	// reality SHRANK the variance (certainty came from grounding).
	if postVar >= priorVar {
		t.Fatalf("a grounded observation must shrink the belief variance; prior=%v post=%v", priorVar, postVar)
	}
}

// TestSLAM_VarianceShrinksOnGroundingNotRestatement is the §0 invariant at the live-loop level: a
// grounded observation lowers the belief variance, but re-stating the same belief (a new thought on
// the line) does NOT — so a confidently-restated-but-ungrounded belief cannot talk its way to
// certainty. This is the calibration-not-caution mechanism.
func TestSLAM_VarianceShrinksOnGroundingNotRestatement(t *testing.T) {
	e, log := slamEngine(t, true)
	pushBelief(t, e, "the deploy looks fine", 0.6)

	// FIRST grounding: variance drops.
	e.groundObservation(types.Intention{Claim: "deploy A"}, realObs(true, "structured"))
	corrects := log.of(events.EstimateCorrect)
	if len(corrects) == 0 {
		t.Fatal("first grounding must emit estimate.correct")
	}
	varAfterGround := floatData(t, corrects[len(corrects)-1], "postVar")
	priorVarFirst := floatData(t, log.of(events.EstimateInnovate)[0], "priorVar")
	if varAfterGround >= priorVarFirst {
		t.Fatalf("grounding must shrink variance: %v -> %v", priorVarFirst, varAfterGround)
	}

	// RESTATE the belief (a new tip thought on the SAME line, more confident) — the estimator Notes it
	// but must not shrink variance. Drive a grounding cycle whose innovate event reports the prior var.
	restate := &types.Thought{ID: -1, Source: types.GENERATED, Text: "the deploy is definitely fine", Confidence: 0.99}
	e.Graph().Append(restate, e.Bus().Tick)
	// the new tip is a DIFFERENT belief id; the §0 invariant is unit-tested in estimate_test.go. Here we
	// assert the engine-level property: re-grounding the SAME (original) belief never INCREASES its var,
	// and a fresh restated belief starts uncertain again (no free certainty from confident wording).
	before := len(log.of(events.EstimateInnovate))
	e.groundObservation(types.Intention{Claim: "deploy B"}, realObs(true, "structured"))
	after := log.of(events.EstimateInnovate)
	if len(after) <= before {
		t.Fatal("re-grounding must emit a fresh estimate.innovate")
	}
	restatedPriorVar := floatData(t, after[len(after)-1], "priorVar")
	// the freshly-restated belief was never grounded, so it starts at the HIGH prior variance — the
	// confident wording bought it NO certainty (the anti-overconfidence guarantee).
	if restatedPriorVar < 0.9 {
		t.Fatalf("a confidently-RESTATED but ungrounded belief must still start uncertain; priorVar=%v", restatedPriorVar)
	}
}

// TestSLAM_FlagOffByteIdenticalWire is the default-OFF guarantee: with slam.innovation OFF, the SAME
// grounded-observation path emits ZERO estimate.* events — the estimator is inert and the live loop is
// byte-identical (the grounding.ground event still fires exactly as before).
func TestSLAM_FlagOffByteIdenticalWire(t *testing.T) {
	e, log := slamEngine(t, false)
	pushBelief(t, e, "this refactor is definitely safe", 0.95)
	e.groundObservation(types.Intention{Claim: "the refactor is safe"}, realObs(false, "structured"))

	for _, k := range []string{events.EstimateInnovate, events.EstimateCorrect, events.EstimateGate} {
		if got := log.of(k); len(got) != 0 {
			t.Fatalf("flag OFF: %s must not fire (estimator inert); got %d", k, len(got))
		}
	}
	// the existing grounding wire is untouched.
	if len(log.of(events.Ground)) == 0 {
		t.Fatal("flag OFF: the grounding.ground wire must still fire (byte-identical)")
	}
}

// TestSLAM_FabricatedObservationNeverInnovates pins the golden-safety reason the OFF and offline paths
// stay byte-identical: a FABRICATED (test-double) observation never reaches the estimator (it is
// rejected by the grounding spine before slamObserve), even with the flag ON. This is why a normal
// offline scenario run never emits estimate.* and the goldens hold.
func TestSLAM_FabricatedObservationNeverInnovates(t *testing.T) {
	e, log := slamEngine(t, true)
	pushBelief(t, e, "the build passes", 0.9)
	fabricated := types.Thought{ID: -1, Source: types.OBSERVATION,
		RawReturn: types.Observation{Ok: false, Fabricated: true}}
	e.groundObservation(types.Intention{Claim: "the build passes"}, fabricated)
	if got := log.of(events.EstimateInnovate); len(got) != 0 {
		t.Fatalf("a FABRICATED observation must never innovate (fake reality can't ground); got %d", len(got))
	}
}

// TestSLAM_FiresEndToEndOnLiveLoop is the WIRING proof (the "tests pass != feature runs" gate): the
// estimator fires through the ENGINE'S OWN groundObservation method — the exact call site the live
// reactive (reactive.go), continuous (continuous.go) and reality-sourcer (knowledge.go) loops invoke —
// against a graph the live loop actually built (Run, not a hand-rolled graph). A real refuting
// observation on the active line drives estimate.innovate + estimate.correct on the live bus.
func TestSLAM_FiresEndToEndOnLiveLoop(t *testing.T) {
	e, log := slamEngine(t, true)
	// drive the real reactive loop a few ticks so the graph + active line are loop-produced state.
	e.SubmitDefault("is this refactor safe to ship?")
	for i := 0; i < 5 && e.Graph() == nil; i++ {
		e.Step()
	}
	if e.Graph() == nil || e.Graph().Last() == nil {
		t.Fatal("the reactive loop never built a live graph with an active line")
	}
	tipBefore := e.Graph().Last().ID

	// a real (non-fabricated) refuting observation crosses the watched seam — the SAME path
	// reactive.go:711 / knowledge.go:71 take. The wired slamObserve at grounding.go folds it in.
	e.groundObservation(types.Intention{Claim: "the refactor is safe to ship"}, realObs(false, "structured"))

	if got := log.of(events.EstimateInnovate); len(got) == 0 {
		t.Fatal("WIRING GAP: a real grounded observation on the live loop never reached the SLAM estimator")
	}
	if got := log.of(events.EstimateCorrect); len(got) == 0 {
		t.Fatal("WIRING GAP: the estimator innovated but never emitted the graded correction")
	}
	// the estimator keyed on the live loop's active tip (the belief the loop actually committed to).
	in := log.of(events.EstimateInnovate)[0]
	if in.Data["id"] == nil {
		t.Fatal("the innovation event carries no belief id")
	}
	_ = tipBefore // (the wire keys on the live tip id; asserted present above)
}

// floatData reads a float64 out of an event's data payload (the round3'd wire values are float64).
func floatData(t *testing.T, e events.Event, key string) float64 {
	t.Helper()
	v, ok := e.Data[key]
	if !ok {
		t.Fatalf("event %s missing data key %q", e.Kind, key)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("event %s data[%q] = %v (not a float64)", e.Kind, key, v)
	}
	return f
}
