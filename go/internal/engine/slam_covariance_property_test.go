package engine

// slam_covariance_property_test.go — the COGNITION property for SLAM M2 (Track F): the SPARSE belief
// COVARIANCE / Information layer. It asserts the THINKING the spec intends — when two beliefs SHARE a
// grounding upstream, a reality-REFUTATION of one makes the harness LOSE certainty in the other (catch
// CORRELATED self-deception), which no per-belief scalar (M1) can see — not merely that the loop runs.
// The flag-OFF half pins byte-identical wire behaviour. Mirrors slam_estimation_property_test.go (M1).
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #2 (Information / correlations) +
// §1 (non-factorization: "all map errors share a common source") + §6 M2.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// covPropEngine builds a reactive engine on the test double with slam.innovation ON (M2 requires M1) and
// slam.covariance set as asked. Returns the engine + a captured event log.
func covPropEngine(t *testing.T, covOn bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn; SLAM defaults OFF
	feat.Slam.Innovation = true
	feat.Slam.Covariance = covOn
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

// appendBelief opens an episode (if needed) and appends one belief thought to the active line, so it
// becomes a tip on the SAME line as the earlier ones (sharing the goal root + early lineage as upstream).
func appendBelief(t *testing.T, e *Engine, text string, confidence float64) *types.Thought {
	t.Helper()
	if e.Graph() == nil {
		e.SubmitDefault("are these two sibling claims, sharing the same premise, both correct?")
		e.Step()
		if e.Graph() == nil {
			t.Fatal("episode did not build a graph")
		}
	}
	b := &types.Thought{ID: -1, Source: types.GENERATED, Text: text, Confidence: confidence}
	return e.Graph().Append(b, e.Bus().Tick)
}

// TestSLAM_M2_RefuteInflatesCorrelatedSibling is the CORE M2 thinking on the LIVE loop: belief A and
// belief B are appended to the SAME active line (so they share the goal-root lineage as a grounding
// upstream). A is grounded confidently, then reality REFUTES B — and because A co-varies with B through
// the shared upstream, A LOSES certainty (an estimate.correlate fires inflating A's variance). The
// harness detected that the premise both share is now suspect — correlated self-deception caught.
func TestSLAM_M2_RefuteInflatesCorrelatedSibling(t *testing.T) {
	e, log := covPropEngine(t, true)

	// Belief A: grounded by reality (confirmed) — it gets a low variance from the grounding.
	appendBelief(t, e, "claim A follows from the shared premise", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))

	// Belief B: a LATER tip on the SAME line, confidently asserted. It shares A's upstream lineage.
	appendBelief(t, e, "claim B also follows from the shared premise", 0.9)

	corrBefore := len(log.of(events.EstimateCorrelate))
	// reality REFUTES B (the shared premise was wrong).
	e.groundObservation(types.Intention{Claim: "claim B holds"}, realObs(false, "structured"))
	corr := log.of(events.EstimateCorrelate)

	if len(corr) <= corrBefore {
		t.Fatal("WIRING/COGNITION GAP: refuting a belief must inflate its co-varying sibling (estimate.correlate) — correlated self-deception not detected")
	}
	last := corr[len(corr)-1]
	// the inflated sibling's variance GREW (it lost certainty because the shared upstream proved unreliable).
	priorVar := floatData(t, last, "priorVar")
	postVar := floatData(t, last, "postVar")
	if postVar <= priorVar {
		t.Fatalf("a correlated refutation must RAISE the sibling's variance (lose certainty); prior=%v post=%v", priorVar, postVar)
	}
	// the correlation was through a genuinely SHARED upstream (>=1), with a positive coefficient.
	if shared := intData(t, last, "shared"); shared < 1 {
		t.Fatalf("the inflated sibling must share at least one grounding upstream; shared=%v", shared)
	}
	if rho := floatData(t, last, "rho"); rho <= 0 {
		t.Fatalf("a co-varying sibling must have a positive correlation coefficient; rho=%v", rho)
	}
}

// TestSLAM_M2_ConfirmDoesNotInflate is the dual: when reality CONFIRMS a belief, no sibling loses
// certainty (a correlated propagation fires ONLY on a refutation — good news about a shared premise is
// not a reason to distrust its siblings). No estimate.correlate on a confirm.
func TestSLAM_M2_ConfirmDoesNotInflate(t *testing.T) {
	e, log := covPropEngine(t, true)
	appendBelief(t, e, "claim A follows from the shared premise", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))
	appendBelief(t, e, "claim B also follows from the shared premise", 0.9)

	before := len(log.of(events.EstimateCorrelate))
	// reality CONFIRMS B.
	e.groundObservation(types.Intention{Claim: "claim B holds"}, realObs(true, "structured"))
	if got := len(log.of(events.EstimateCorrelate)); got != before {
		t.Fatalf("a CONFIRMED observation must not inflate any sibling; correlate events grew by %d", got-before)
	}
}

// TestSLAM_M2_FlagOffByteIdenticalWire is the default-OFF guarantee: with slam.covariance OFF (M1 only),
// the SAME refute-a-sibling path emits ZERO estimate.correlate events and the M1 estimate.innovate/
// correct wire fires EXACTLY as in M1-only mode — the covariance layer is inert and byte-identical.
func TestSLAM_M2_FlagOffByteIdenticalWire(t *testing.T) {
	e, log := covPropEngine(t, false) // M1 on, M2 OFF
	appendBelief(t, e, "claim A follows from the shared premise", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))
	appendBelief(t, e, "claim B also follows from the shared premise", 0.9)
	e.groundObservation(types.Intention{Claim: "claim B holds"}, realObs(false, "structured"))

	if got := log.of(events.EstimateCorrelate); len(got) != 0 {
		t.Fatalf("flag OFF: estimate.correlate must not fire (covariance layer inert); got %d", len(got))
	}
	// the M1 layer still works exactly as before (the refute still innovates + corrects).
	if len(log.of(events.EstimateInnovate)) == 0 || len(log.of(events.EstimateCorrect)) == 0 {
		t.Fatal("flag OFF (M2): the M1 estimate.innovate/correct wire must still fire unchanged")
	}
}

// TestSLAM_M2_FabricatedObservationNeverCorrelates pins the golden-safety reason an offline scenario run
// stays byte-identical: a FABRICATED (test-double) observation never reaches the estimator, so it can
// never trigger a correlated propagation even with both knobs ON.
func TestSLAM_M2_FabricatedObservationNeverCorrelates(t *testing.T) {
	e, log := covPropEngine(t, true)
	appendBelief(t, e, "claim A follows from the shared premise", 0.8)
	e.groundObservation(types.Intention{Claim: "claim A holds"}, realObs(true, "structured"))
	appendBelief(t, e, "claim B also follows from the shared premise", 0.9)
	fabricated := types.Thought{ID: -1, Source: types.OBSERVATION,
		RawReturn: types.Observation{Ok: false, Fabricated: true}}
	e.groundObservation(types.Intention{Claim: "claim B holds"}, fabricated)
	if got := log.of(events.EstimateCorrelate); len(got) != 0 {
		t.Fatalf("a FABRICATED observation must never correlate (fake reality can't ground); got %d", len(got))
	}
}

// intData reads an int out of an event's data payload (the `shared` count is an int on the wire, not a
// round3'd float). Accepts the int forms a JSON-roundtripped or live event may carry.
func intData(t *testing.T, e events.Event, key string) int {
	t.Helper()
	v, ok := e.Data[key]
	if !ok {
		t.Fatalf("event %s missing data key %q", e.Kind, key)
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		t.Fatalf("event %s data[%q] = %v (not an int)", e.Kind, key, v)
		return 0
	}
}
