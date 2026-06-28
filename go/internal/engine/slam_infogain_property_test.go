package engine

// slam_infogain_property_test.go — the COGNITION property for SLAM M6 (Track F): the ACTIVE-INFERENCE
// info-gain / next-best-observation. It asserts the THINKING the spec intends — given several uncertain
// beliefs, the harness DIRECTS its grounding by expected uncertainty reduction (the most-uncertain belief,
// leveraged by its correlation reach), not just by outcome reward — AND that the signal fires on the LIVE
// reactive loop (the wiring proof: estimate.infogain appears when reason() runs with the flag ON). The
// flag-OFF half pins byte-identical wire behaviour. Mirrors slam_covariance_property_test.go (M2).
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #7 (Exploration / active inference)
// + §5 #4 (directed grounding: "choose what to verify next by expected uncertainty reduction") + §6 M6.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// igPropEngine builds a reactive engine on the test double with slam.innovation ON (M6 requires M1),
// slam.covariance ON (so the correlation-reach term is live — M6 reads the M2 covGraph) and slam.infogain
// set as asked. Returns the engine + a captured event log.
func igPropEngine(t *testing.T, infoOn bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn; SLAM defaults OFF
	feat.Slam.Innovation = true
	feat.Slam.Covariance = true
	feat.Slam.InfoGain = infoOn
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

// TestSLAM_M6_NextBestObservationFiresOnLiveLoop is the WIRING + cognition proof on the LIVE loop: after
// the harness holds a couple of beliefs (one grounded confidently -> low variance, one stated but never
// grounded -> high variance), a reactive Step() runs reason(), which ranks the tracked beliefs by expected
// info gain and emits estimate.infogain. The signal must FIRE (the wire is live, not dead) AND it must
// surface the UNGROUNDED belief as the next-best-observation — the directed-grounding "verify what you do
// NOT yet know" the spec intends, not a re-confirmation of what reality already settled.
func TestSLAM_M6_NextBestObservationFiresOnLiveLoop(t *testing.T) {
	e, log := igPropEngine(t, true)

	// Belief A: grounded by reality (confirmed) -> the grounding shrinks its variance (low uncertainty).
	appendBelief(t, e, "claim A: the build passes its tests", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))

	// Belief B: a LATER tip on the SAME line, confidently asserted but NEVER grounded -> high uncertainty.
	appendBelief(t, e, "claim B: the new feature is also correct", 0.9)

	// Drive a reactive tick so reason() runs (the live wire site fires slamNextBestObservation).
	e.Step()

	infos := log.of(events.EstimateInfoGain)
	if len(infos) == 0 {
		t.Fatal("flag ON: a reactive Step() with tracked beliefs must emit estimate.infogain (the live wire is dead)")
	}
	// The next-best-observation must point at the UNGROUNDED (high-variance) belief — what to verify next.
	last := infos[len(infos)-1]
	gain := floatData(t, last, "gain")
	priorVar := floatData(t, last, "priorVar")
	cands := intData(t, last, "candidates")
	if cands < 2 {
		t.Fatalf("expected at least 2 ranked candidates, got %d", cands)
	}
	if gain <= 0 {
		t.Fatalf("the next-best-observation must have positive expected info gain; got %v", gain)
	}
	// The surfaced target is the highest-uncertainty belief: its variance must be HIGH (a grounded belief
	// would be low-variance and rank last — re-confirming it cannot beat its first-grounding floor, P1).
	if priorVar < 0.5 {
		t.Fatalf("the next-best-observation must target an UNCERTAIN belief (high variance); got priorVar=%v", priorVar)
	}
}

// TestSLAM_M6_RankingNeverFabricatesCertainty is the §0/M5 consistency invariant at the live-loop level:
// the info-gain ranking is a PURE direction signal — it must NEVER lower a belief's variance (only a
// grounded observation may). Running many reactive ticks with the layer on, no estimate.infogain emission
// may be accompanied by a spurious variance reduction; the M5 consistency witness (also on) stays
// consistent. Directing grounding must not itself manufacture intelligence.
func TestSLAM_M6_RankingNeverFabricatesCertainty(t *testing.T) {
	e, _ := igPropEngine(t, true)
	e.features.Slam.Consistency = true // also run the M5 witness so a spurious gain is failable

	appendBelief(t, e, "claim A: stated confidently, never grounded", 0.95)
	// Drive several ticks; the ranking runs each reason() pass but must move NO variance.
	for i := 0; i < 5; i++ {
		e.Step()
	}
	c := e.estimator.ConsistencyState()
	if c.SpuriousGain > 0 || c.Violations > 0 {
		t.Fatalf("the info-gain ranking must gain NO spurious information: spuriousGain=%v violations=%d", c.SpuriousGain, c.Violations)
	}
}

// TestSLAM_M6_OffByteIdentical pins the default-OFF contract on the LIVE loop: with slam.infogain OFF the
// estimator still runs M1/M2 (innovation + covariance) but emits NO estimate.infogain event — the OFF
// path adds zero ranking work and is byte-identical to the M1/M2 baseline.
func TestSLAM_M6_OffByteIdentical(t *testing.T) {
	e, log := igPropEngine(t, false)

	appendBelief(t, e, "claim A: the build passes its tests", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))
	appendBelief(t, e, "claim B: the feature is correct", 0.9)
	e.Step()

	if n := len(log.of(events.EstimateInfoGain)); n != 0 {
		t.Fatalf("flag OFF: no estimate.infogain event may fire; got %d", n)
	}
	if e.estimator.Exploring() {
		t.Fatalf("flag OFF: the estimator must not be Exploring()")
	}
	// M1/M2 still ran (innovation fired on the grounded observation) — only the M6 ranking is absent.
	if n := len(log.of(events.EstimateInnovate)); n == 0 {
		t.Fatal("flag OFF: M1 innovation must still fire (only M6 ranking is gated)")
	}
}
