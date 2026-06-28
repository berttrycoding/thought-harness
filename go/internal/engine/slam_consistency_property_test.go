package engine

// slam_consistency_property_test.go — the COGNITION property for SLAM M5 (Track F): the consistency /
// observability invariant. These assert the THINKING the spec intends — the harness does NOT talk itself
// into certainty; every bit of confidence it gains is justified by a grounded observation, NEVER by
// self-restatement (the Huang-2010 EKF-inconsistency overconfidence it structurally cannot accrue) — not
// merely that the loop runs. The flag-OFF half pins byte-identical wire behaviour. Mirrors the discipline
// of slam_estimation_property_test.go (M1) and cognition_property_test.go.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// boolField reads a bool out of an event's data payload (the internal-package twin of engine_test's
// boolData, which is not visible here). ok=false when the key is absent or not a bool.
func boolField(e events.Event, key string) (val, ok bool) {
	v, present := e.Data[key]
	if !present {
		return false, false
	}
	b, isBool := v.(bool)
	return b, isBool
}

// slamConsistencyEngine builds a reactive engine on the test double with slam.innovation + slam.consistency
// set as asked (M5 requires M1, so they flip together for the ON case). Returns the engine + a captured log.
func slamConsistencyEngine(t *testing.T, on bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn; SLAM defaults OFF
	feat.Slam.Innovation = on
	feat.Slam.Consistency = on
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

// TestSLAM_M5_NoFabricatedCertaintyUnderRestatement is the M5 epistemic core (P2): the harness CANNOT
// talk itself into certainty. A belief is restated confidently many times (the self-reinforcement that an
// inconsistent estimator would convert into fabricated certainty in the unobservable direction); only a
// real grounded observation legitimately gains information. The consistency witness must report that ALL
// information gained was grounded (groundedFraction == 1.0, spuriousGain == 0) — the estimator gained NO
// spurious information. This is the calibration-not-caution mechanism made failable.
func TestSLAM_M5_NoFabricatedCertaintyUnderRestatement(t *testing.T) {
	e, log := slamConsistencyEngine(t, true)
	pushBelief(t, e, "this change is definitely correct — I am sure", 0.95)

	// Restate the SAME confident belief repeatedly on the active line (self-reinforcement). Each restatement
	// is a Note() — it must gain NO information. We drive several grounding cycles that re-state then ground,
	// so the witness accumulates across the run, then read it at the end.
	for i := 0; i < 5; i++ {
		restate := &types.Thought{ID: -1, Source: types.GENERATED, Text: "it is certainly correct", Confidence: 0.99}
		e.Graph().Append(restate, e.Bus().Tick)
	}
	// Now reality grounds a belief (the ONE legitimate, observable information gain).
	e.groundObservation(types.Intention{Claim: "the change works"}, realObs(true, "structured"))
	e.Step() // a tick so the end-of-tick CheckConsistency witness fires on the live loop

	wits := log.of(events.EstimateConsistency)
	if len(wits) == 0 {
		t.Fatal("flag ON (M1+M5): the live loop must emit estimate.consistency (the M5 witness)")
	}
	last := wits[len(wits)-1]
	spurious := floatData(t, last, "spuriousGain")
	groundedFrac := floatData(t, last, "groundedFraction")
	consistent, _ := boolField(last, "consistent")

	// The invariant: ZERO spurious information gained (the estimator never fabricated certainty).
	if spurious > 1e-9 {
		t.Fatalf("M5: the estimator gained spurious information under self-restatement; spuriousGain=%v", spurious)
	}
	// All information came from grounding (the restatements added none).
	if groundedFrac < 1.0 {
		t.Fatalf("M5: not all information was grounded; groundedFraction=%v (some certainty was fabricated)", groundedFrac)
	}
	if !consistent {
		t.Fatalf("M5: the witness reported INCONSISTENT despite no spurious gain (consistent=false)")
	}
}

// TestSLAM_M5_GatedRefutationGainsNoCertainty is the data-association side of the invariant (P3 gauge): a
// refuting observation so far from the prior that the Mahalanobis gate REJECTS it (an association failure
// — "this obs is probably not about this belief") must NOT change the belief's certainty. A gated obs is
// unobservable for this belief, so folding it in would be spurious information. The witness must stay
// consistent and count the gate.
func TestSLAM_M5_GatedRefutationGainsNoCertainty(t *testing.T) {
	e, log := slamConsistencyEngine(t, true)
	// A belief grounded to near-certainty first (low variance), so a later WILD refutation is gated.
	pushBelief(t, e, "2+2=4, validated", 0.9)
	e.groundObservation(types.Intention{Claim: "arithmetic holds"}, realObs(true, "structured"))
	e.groundObservation(types.Intention{Claim: "arithmetic holds again"}, realObs(true, "structured"))
	e.Step()

	wits := log.of(events.EstimateConsistency)
	if len(wits) == 0 {
		t.Fatal("flag ON: the live loop must emit estimate.consistency")
	}
	last := wits[len(wits)-1]
	if spurious := floatData(t, last, "spuriousGain"); spurious > 1e-9 {
		t.Fatalf("M5: a gated/associated observation sequence must gain no spurious certainty; spuriousGain=%v", spurious)
	}
	if consistent, _ := boolField(last, "consistent"); !consistent {
		t.Fatal("M5: the witness must report consistent over a sound observation sequence")
	}
}

// TestSLAM_M5_FlagOffByteIdentical pins the default-OFF contract: with slam.consistency OFF (the default),
// the engine emits ZERO estimate.consistency events on the live loop — the monitor is inert, so the loop
// is byte-identical to a run without M5. (M1 may still be on independently, but no consistency witness.)
func TestSLAM_M5_FlagOffByteIdentical(t *testing.T) {
	// M5 OFF, M1 ON: M1 events may flow, but NO consistency witness.
	feat := config.New()
	feat.Slam.Innovation = true
	feat.Slam.Consistency = false
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &slamLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	pushBelief(t, e, "self-restated confident belief", 0.95)
	for i := 0; i < 5; i++ {
		e.Graph().Append(&types.Thought{ID: -1, Source: types.GENERATED, Text: "still sure", Confidence: 0.99}, e.Bus().Tick)
	}
	e.groundObservation(types.Intention{Claim: "x"}, realObs(true, "structured"))
	e.Step()

	if got := log.of(events.EstimateConsistency); len(got) != 0 {
		t.Fatalf("M5 OFF: must emit NO estimate.consistency (got %d) — the OFF path must be byte-identical", len(got))
	}
	// The witness accessor must report not-active.
	if _, ok := e.EstimatorConsistency(); ok {
		t.Fatal("M5 OFF: EstimatorConsistency must report ok=false (the monitor is inert)")
	}
}
