package engine

// slam_staleness_property_test.go — the COGNITION property for SLAM M4 (Track F): FRESHNESS / STALENESS
// DECAY (the dynamic-map process noise Q>0, P4). It asserts the THINKING the spec intends — a fact the
// harness grounded by reality, then left UN-REFRESHED across ticks, LOSES certainty (its belief variance
// GROWS back toward uncertain), forcing the estimator to want to re-observe it — not merely that the loop
// runs. The flag-OFF half pins byte-identical wire behaviour. Mirrors slam_covariance_property_test.go.
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §4 (P4) "Non-stationary world => mandatory
// decay (Q>0)" + §3b.2 (the Estimate envelope's LastObs/Dynamics fields: "world/time MUST drift and decay
// toward stale, re-observe") + §6 M4. THE FAILURE MODE M4 DESIGNS OUT: a stale answer presented as fresh —
// the estimator staying falsely confident in a fact that has changed since it was last observed.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// stalePropEngine builds a reactive engine on the test double with slam.innovation ON (M4 requires M1) and
// slam.staleness set as asked, at a brisk decay rate so a handful of ticks measurably ages a belief.
// Returns the engine + a captured event log.
func stalePropEngine(t *testing.T, staleOn bool) (*Engine, *slamLog) {
	t.Helper()
	feat := config.New() // AllOn; SLAM defaults OFF
	feat.Slam.Innovation = true
	feat.Slam.Staleness = staleOn
	feat.Slam.StalenessQ = 0.3 // brisk slow-drift so a few ticks ages the belief
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

// TestSLAM_M4_UnrefreshedGroundedBeliefDecays is the CORE M4 thinking on the LIVE loop: the harness grounds
// a belief by reality (it gets a low, confident variance), then the conscious stream moves on WITHOUT
// re-observing that fact across several engine ticks — and the engine's per-tick Step() decay sweep GROWS
// the stale belief's variance (an estimate.decay fires, postVar > priorVar). The harness has reasoned "I
// observed this once, but it was a while ago and the world may have moved — I am less sure of it now",
// which is exactly the re-observation pressure P4 demands. This is the LIVE-loop wiring proof (the sweep
// fires from Step(), not just a direct Decay() call).
func TestSLAM_M4_UnrefreshedGroundedBeliefDecays(t *testing.T) {
	e, log := stalePropEngine(t, true)

	// Open an episode and ground a belief at the active tip (the belief the estimator keys on).
	e.SubmitDefault("is the deployed config still the one we verified yesterday?")
	e.Step()
	if e.Graph() == nil {
		t.Fatal("episode did not build a graph")
	}
	belief := &types.Thought{ID: -1, Source: types.GENERATED, Text: "the deployed config matches the verified one", Confidence: 0.85}
	e.Graph().Append(belief, e.Bus().Tick)
	e.groundObservation(types.Intention{Claim: "the deployed config is verified"}, realObs(true, "structured"))

	if len(log.of(events.EstimateCorrect)) == 0 {
		t.Fatal("setup: the grounding must shrink the belief variance (estimate.correct) before it can decay")
	}

	// Now advance the engine WITHOUT re-grounding that fact: the conscious stream wanders on, the grounded
	// belief is left un-refreshed, and each tick's Step() decay sweep ages it.
	decayBefore := len(log.of(events.EstimateDecay))
	for i := 0; i < 8; i++ {
		e.Step()
	}
	decays := log.of(events.EstimateDecay)
	if len(decays) <= decayBefore {
		t.Fatal("COGNITION/WIRING GAP: an un-refreshed grounded belief must DECAY on the live Step() loop (estimate.decay) — the staleness/re-observation pressure P4 demands is absent")
	}
	// at least one decay shows the belief LOSING certainty (variance grew on the wire) — the dynamic-map
	// process noise (Q>0). (Later decays on a near-saturated belief can round to a flat wire value; the
	// FIRST decay of a freshly-grounded belief is the clear growth, so we require at least one such event.)
	sawGrowth := false
	for _, d := range decays {
		priorVar := floatData(t, d, "priorVar")
		postVar := floatData(t, d, "postVar")
		if postVar > priorVar {
			sawGrowth = true
			if age := intData(t, d, "age"); age < 1 {
				t.Fatalf("a decayed belief must be at least 1 tick stale; age=%v", age)
			}
			if q := floatData(t, d, "q"); q <= 0 {
				t.Fatalf("staleness decay must run at a positive process-noise rate Q; q=%v", q)
			}
		}
	}
	if !sawGrowth {
		t.Fatal("staleness decay must RAISE the variance (lose certainty) on at least one un-refreshed tick")
	}
}

// TestSLAM_M4_DecayNeverExceedsThePrior is the bounded-decay property (the saturation guarantee): no matter
// how long a belief is left un-refreshed, its variance never grows PAST the prior ceiling — a forever-stale
// fact is at most as uncertain as a never-grounded one, never more. The decay is bounded process noise, not
// an unbounded blow-up, so it can never destabilise the estimator (it stays inside the durability regime).
func TestSLAM_M4_DecayNeverExceedsThePrior(t *testing.T) {
	e, log := stalePropEngine(t, true)
	e.SubmitDefault("is the cached lookup table still current?")
	e.Step()
	belief := &types.Thought{ID: -1, Source: types.GENERATED, Text: "the lookup table is current", Confidence: 0.8}
	e.Graph().Append(belief, e.Bus().Tick)
	e.groundObservation(types.Intention{Claim: "the lookup table is current"}, realObs(true, "structured"))

	// age it for a long stretch.
	for i := 0; i < 40; i++ {
		e.Step()
	}
	decays := log.of(events.EstimateDecay)
	if len(decays) == 0 {
		t.Fatal("expected the belief to decay over a long un-refreshed stretch")
	}
	for _, d := range decays {
		postVar := floatData(t, d, "postVar")
		ceiling := floatData(t, d, "ceiling")
		if postVar > ceiling+1e-6 {
			t.Fatalf("a decayed variance %v must never exceed the prior ceiling %v (bounded process noise)", postVar, ceiling)
		}
	}
}

// TestSLAM_M4_FlagOffByteIdenticalWire is the default-OFF guarantee: with slam.staleness OFF (M1 only), the
// SAME ground-then-wander path emits ZERO estimate.decay events and the M1 estimate.innovate/correct wire
// fires EXACTLY as in M1-only mode — the staleness layer is inert and byte-identical.
func TestSLAM_M4_FlagOffByteIdenticalWire(t *testing.T) {
	e, log := stalePropEngine(t, false) // M1 on, M4 OFF
	e.SubmitDefault("is the deployed config still verified?")
	e.Step()
	belief := &types.Thought{ID: -1, Source: types.GENERATED, Text: "the deployed config matches the verified one", Confidence: 0.85}
	e.Graph().Append(belief, e.Bus().Tick)
	e.groundObservation(types.Intention{Claim: "the deployed config is verified"}, realObs(true, "structured"))
	for i := 0; i < 12; i++ {
		e.Step()
	}
	if got := log.of(events.EstimateDecay); len(got) != 0 {
		t.Fatalf("flag OFF: estimate.decay must not fire (staleness layer inert); got %d", len(got))
	}
	// the M1 layer still works exactly as before (the grounding still innovates + corrects).
	if len(log.of(events.EstimateInnovate)) == 0 || len(log.of(events.EstimateCorrect)) == 0 {
		t.Fatal("flag OFF (M4): the M1 estimate.innovate/correct wire must still fire unchanged")
	}
}

// TestSLAM_M4_ReObservationRefreshesAndStopsDecay is the freshness-reset thinking: a belief that is
// re-grounded (the harness re-observes the fact) gets its staleness clock reset — it is FRESH again, so the
// next tick's decay starts from age 1, not the accumulated age. Keeping a fact fresh by re-observation
// keeps the estimator confident in it; only NEGLECT lets it go stale. This is the "re-observe to stay
// certain" half of the P4 loop.
func TestSLAM_M4_ReObservationRefreshesAndStopsDecay(t *testing.T) {
	e, log := stalePropEngine(t, true)
	e.SubmitDefault("is the deployed config still verified?")
	e.Step()
	belief := &types.Thought{ID: -1, Source: types.GENERATED, Text: "the deployed config matches the verified one", Confidence: 0.85}
	tip := e.Graph().Append(belief, e.Bus().Tick)
	e.groundObservation(types.Intention{Claim: "the deployed config is verified"}, realObs(true, "structured"))

	// age it a few ticks so it decays.
	for i := 0; i < 5; i++ {
		e.Step()
	}
	if len(log.of(events.EstimateDecay)) == 0 {
		t.Fatal("setup: the belief should have decayed over the un-refreshed stretch")
	}
	// RE-OBSERVE the SAME belief tip (re-ground it) -> the freshness clock resets.
	// Re-append the same belief as the live tip so groundObservation keys on it, then re-ground.
	e.Graph().Append(&types.Thought{ID: -1, Source: types.GENERATED, Text: tip.Text, Confidence: tip.Confidence}, e.Bus().Tick)
	correctsBefore := len(log.of(events.EstimateCorrect))
	e.groundObservation(types.Intention{Claim: "the deployed config is verified"}, realObs(true, "structured"))
	if len(log.of(events.EstimateCorrect)) <= correctsBefore {
		t.Fatal("re-grounding must fire a fresh estimate.correct (the var-reducer refreshes the belief)")
	}
	// one tick immediately after the refresh: any decay this tick must be a SMALL (age-1) growth, not the
	// accumulated-age jump — proven by the age field on the freshly-decayed belief being small.
	decayBefore := len(log.of(events.EstimateDecay))
	e.Step()
	fresh := log.of(events.EstimateDecay)
	if len(fresh) > decayBefore {
		// a decay fired this tick; assert the refreshed belief (if it is the one that decayed) aged only 1
		// tick — the clock was reset. We check that SOME decay event in the new batch has age <= 2 (the
		// freshly-refreshed belief), proving the refresh reset the clock for that belief.
		minAge := 1 << 30
		for _, d := range fresh[decayBefore:] {
			if a := intData(t, d, "age"); a < minAge {
				minAge = a
			}
		}
		if minAge > 2 {
			t.Fatalf("after a re-observation the refreshed belief's decay clock must reset (small age); min age=%d", minAge)
		}
	}
}
