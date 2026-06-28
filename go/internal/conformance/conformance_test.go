package conformance

// Tests for the L0 conformance ROLLUP (Track H, benchmark-taxonomy §1 L0 + §5 build-order #1). The rollup
// is the "does it even run as a harness?" gate; these tests pin that it (a) runs every S1..S16 scenario on
// the live loop, (b) applies the requirement checklist + wiring scan per run, (c) requires the named
// subsystems to fire across the suite, and (d) emits ONE conformance.rollup verdict on the bus. Deterministic
// + offline (the test double + a fixed seed) — a CONTROL instrument, no model.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
)

// TestRollupPassesOnTheLiveHarness is the headline L0 gate: the real S1..S16 harness, run on the live loop,
// must PASS conformance (it does run as a harness). Every requirement check + every per-run wiring scan
// passes, every suite-wide subsystem layer fires.
func TestRollupPassesOnTheLiveHarness(t *testing.T) {
	res := Run(nil)

	if want := len(scenarios.All()); res.Scenarios != want {
		t.Fatalf("ran %d scenarios, want %d (the whole S1..S16 suite)", res.Scenarios, want)
	}
	if !res.Pass {
		t.Errorf("L0 conformance FAILED on the live harness (it must run as a harness). Failures:")
		for _, f := range res.Failures {
			t.Errorf("  - %s", f)
		}
	}
	if !res.WiringOK {
		t.Errorf("wiring scan incomplete: suite-missing layers=%v", res.SuiteMissing)
	}
	if res.ChecksTotal == 0 || res.ChecksPassed != res.ChecksTotal {
		t.Errorf("checks %d/%d passed, want all", res.ChecksPassed, res.ChecksTotal)
	}
	if len(res.SuiteMissing) != 0 {
		t.Errorf("suite-wide wiring missing layers %v — a subsystem is dead-wired suite-wide", res.SuiteMissing)
	}
	// every suiteLayer (incl. subconscious + action) must appear across the suite — the proof the named
	// subsystems are not just compiled but actually fire on the live loop somewhere.
	seen := make(map[string]bool, len(res.SuiteCovered))
	for _, l := range res.SuiteCovered {
		seen[l] = true
	}
	for _, l := range suiteLayers {
		if !seen[l] {
			t.Errorf("suite layer %q never fired across S1..S16 (covered=%v)", l, res.SuiteCovered)
		}
	}
}

// TestRollupEmitsVerdict pins the observability contract: the rollup emits exactly one conformance.rollup
// verdict on the supplied bus, carrying the PASS/FAIL + the check tallies.
func TestRollupEmitsVerdict(t *testing.T) {
	bus := events.NewDefault()
	var verdicts []events.Event
	bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.ConformanceRollup {
			verdicts = append(verdicts, ev)
		}
	})

	res := Run(bus)

	if len(verdicts) != 1 {
		t.Fatalf("conformance.rollup emitted %d times, want exactly 1", len(verdicts))
	}
	v := verdicts[0]
	if pass, _ := v.Data["pass"].(bool); pass != res.Pass {
		t.Errorf("conformance.rollup pass=%v, want %v (must match the returned verdict)", pass, res.Pass)
	}
	if ct, _ := v.Data["checks_total"].(int); ct != res.ChecksTotal {
		t.Errorf("conformance.rollup checks_total=%v, want %d", v.Data["checks_total"], res.ChecksTotal)
	}
}

// TestEveryScenarioRunsTheFullChecklist guards that each scenario gets the complete requirement checklist +
// wiring scan (6 checks), so a future scenario added without the checklist coverage is caught.
func TestEveryScenarioRunsTheFullChecklist(t *testing.T) {
	res := Run(nil)
	for _, rr := range res.Runs {
		// the requirement checklist + the wiring scan = 6 named checks per run.
		const wantChecks = 6
		if len(rr.Checks) != wantChecks {
			t.Errorf("%s ran %d checks, want %d (the full requirement checklist + wiring scan)", rr.ID, len(rr.Checks), wantChecks)
		}
		// every run must have actually emitted events (the loop ran) and covered the per-run core layers.
		if rr.Events == 0 {
			t.Errorf("%s emitted 0 events — the live loop never ran", rr.ID)
		}
	}
}

// TestWiringScanDetectsMissingLayer is the negative unit test for the rollup's set math — the same
// dead-wire property the engine test pins, at the rollup helper level: a required layer absent from coverage
// is reported missing.
func TestWiringScanDetectsMissingLayer(t *testing.T) {
	covered := []string{"conscious", "seam", "critic"}
	if miss := missingLayers(coreLayers, covered); len(miss) == 0 {
		t.Fatalf("missingLayers found nothing missing, but %v lacks most of %v", covered, coreLayers)
	}
	// a fully-covered set ⇒ nothing missing.
	if miss := missingLayers([]string{"conscious"}, covered); len(miss) != 0 {
		t.Errorf("missingLayers reported %v missing from a covered superset, want none", miss)
	}
}

// TestOrphanObservationDetected pins the LATHE SanitizeMessages invariant the checklist enforces: an
// action.observation with no preceding action.intention/act is an orphan tool-result and must be flagged.
func TestOrphanObservationDetected(t *testing.T) {
	orphan, _ := hasOrphanObservation([]string{events.Tick, events.Observation})
	if !orphan {
		t.Errorf("an observation with no preceding intention was NOT flagged as orphan")
	}
	// a well-formed intention->observation pair is NOT an orphan.
	ok, _ := hasOrphanObservation([]string{events.Intention, events.Observation})
	if ok {
		t.Errorf("a well-formed intention->observation pair was flagged as orphan")
	}
}
