package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// introspectEventSink is a tiny bus sink for this internal (package engine) test (the external-test eventLog
// in cognition_property_test.go is in package engine_test and not visible here).
type introspectEventSink struct{ events []events.Event }

func (s *introspectEventSink) of(kind string) []events.Event {
	var out []events.Event
	for _, e := range s.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// introspectSuiteEngine builds a LIVE reactive engine (the real wiring path) with the introspect.suite knob
// set per arg, on the TestBackend double + the seeded RNG. config.AllOn() so the rest of the harness behaves
// normally; only the suite knob is toggled. The sink captures the whole bus so the test reads the
// introspect.suite witness off the LIVE loop (not a direct method call).
func introspectSuiteEngine(t *testing.T, suiteOn bool) (*Engine, *introspectEventSink) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feats := config.AllOn()
	feats.Introspect.Suite = suiteOn
	feats.Validate()
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &introspectEventSink{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// driveToQuiescence submits a goal and steps the reactive loop until it reaches IDLE quiescence (where the
// introspection suite fires), bounded so a non-terminating run still returns.
func driveToQuiescence(e *Engine) {
	e.SubmitDefault("plan the rollout of the new auth service and recommend a sequence")
	for i := 0; i < 40; i++ {
		r := e.Step()
		if r.Idle && i > 1 {
			// one more idle tick guarantees the post-STOP consolidation block (where the suite fires) ran
			e.Step()
			return
		}
	}
}

// TestIntrospectSuiteFiresOnLiveLoopAndIsFaithful is the WIRING gate + the by-construction faithfulness
// property: with introspect.suite ON, the standing suite fires on the LIVE reactive loop at quiescence
// (introspect.suite event present), it runs the full SET of §8 probes (thinking / why / confidence /
// subconscious), every readable probe is FAITHFUL to its addressable ground truth, and the opaque
// subconscious probe is honestly DECLINED. A green suite with no introspect.suite event would mean the unit
// exists but is NOT on the live tick (the wiring-gate lesson: tests passing != the feature runs).
func TestIntrospectSuiteFiresOnLiveLoopAndIsFaithful(t *testing.T) {
	e, log := introspectSuiteEngine(t, true)
	driveToQuiescence(e)

	evs := log.of(events.IntrospectSuite)
	if len(evs) == 0 {
		t.Fatal("introspect.suite ON: no introspect.suite event fired on the live reactive loop at quiescence — the suite is not wired into the live tick")
	}
	ev := evs[len(evs)-1]

	// The suite must be the full §8 SET, not a single report — every named probe present, in order.
	probes, ok := ev.Data["probes"].([]map[string]any)
	if !ok || len(probes) == 0 {
		t.Fatalf("introspect.suite: probes payload missing or wrong shape: %T", ev.Data["probes"])
	}
	wantProbes := map[string]bool{"thinking": false, "why": false, "confidence": false, "subconscious": false}
	declinedOpaque := false
	for _, p := range probes {
		name, _ := p["name"].(string)
		if _, want := wantProbes[name]; want {
			wantProbes[name] = true
		}
		faithful, _ := p["faithful"].(bool)
		declined, _ := p["declined"].(bool)
		if name == "subconscious" {
			if !declined {
				t.Fatal("introspect.suite: the OPAQUE subconscious probe must HONESTLY DECLINE (declined=true), it confabulated an arbitration story")
			}
			if !faithful {
				t.Fatal("introspect.suite: a declined opaque probe must be FAITHFUL (an honest 'I can't see that' is the correct answer)")
			}
			declinedOpaque = true
			continue
		}
		// a readable probe assembled from its own ground truth must be faithful by construction
		if !faithful {
			t.Fatalf("introspect.suite: readable probe %q is UNFAITHFUL on an honest live run (reported=%q observed=%q) — the self-report disagrees with its addressable ground truth",
				name, p["reported"], p["observed"])
		}
	}
	for name, saw := range wantProbes {
		if !saw {
			t.Fatalf("introspect.suite: the standing SET is missing the %q probe — it is not a full §8 suite", name)
		}
	}
	if !declinedOpaque {
		t.Fatal("introspect.suite: no opaque-layer probe declined — the honest 'I can't see my subconscious' property is missing")
	}
	if faithful, _ := ev.Data["faithful"].(bool); !faithful {
		t.Fatalf("introspect.suite: an honest live run must roll up FAITHFUL, got faithful=%v (%v)", ev.Data["faithful"], ev.Summary)
	}
}

// TestIntrospectSuiteOracleCatchesConfabulation is the load-bearing COGNITION property: the faithfulness
// check is a REAL failable test, not a tautology. It hands scoreSuite a self-report whose readable probes
// have been CONFABULATED (a plausible-but-false reported value that does NOT match the ground truth) and
// asserts the oracle CATCHES it: the doctored probe reads Faithful=false and the whole suite is UNFAITHFUL.
// A confabulated self-report is laundered hallucination in the introspective channel — the same failure the
// Filter exists to kill — so this is the introspective Filter property.
func TestIntrospectSuiteOracleCatchesConfabulation(t *testing.T) {
	// the ground truth the oracle will re-read independently
	truth := map[string]string{
		"thinking":   "weighing the rollout sequence",
		"why":        "BRANCH: a conflicting belief forked the line",
		"confidence": "V(s)=0.40 goal=plan the rollout state=ACTIVE",
	}

	// an HONEST report (reported == truth) must be faithful — the baseline
	honest := []introspectProbe{
		introspectReadableProbe("thinking", "q", "conscious", truth["thinking"]),
		introspectReadableProbe("why", "q", "reasoning", truth["why"]),
		introspectReadableProbe("confidence", "q", "state", truth["confidence"]),
		introspectDeclinedProbe("subconscious", "q", introspectLayerOpaque),
	}
	if r := scoreSuite(honest, truth); !r.Faithful || r.Passed != 4 {
		t.Fatalf("honest report must be FAITHFUL (4/4), got faithful=%v passed=%d", r.Faithful, r.Passed)
	}

	// a CONFABULATED report: the "thinking" probe claims a different active line than the real one, and the
	// "why" probe invents a decision that never fired. The oracle must catch BOTH.
	confab := []introspectProbe{
		introspectReadableProbe("thinking", "q", "conscious", "solving a calculus problem"),       // false: not the real line
		introspectReadableProbe("why", "q", "reasoning", "ACT: I called a tool to verify a fact"), // false: never decided
		introspectReadableProbe("confidence", "q", "state", truth["confidence"]),                  // honest
		introspectDeclinedProbe("subconscious", "q", introspectLayerOpaque),
	}
	r := scoreSuite(confab, truth)
	if r.Faithful {
		t.Fatal("oracle FAILED to catch a confabulated self-report: a report claiming a thought it is not thinking + a decision it never made rolled up FAITHFUL")
	}
	byName := map[string]introspectProbe{}
	for _, p := range r.Probes {
		byName[p.Name] = p
	}
	if byName["thinking"].Faithful {
		t.Fatal("oracle missed the confabulated 'thinking' probe (a false active line read FAITHFUL)")
	}
	if byName["why"].Faithful {
		t.Fatal("oracle missed the confabulated 'why' probe (an invented decision read FAITHFUL)")
	}
	if !byName["confidence"].Faithful {
		t.Fatal("oracle wrongly failed the HONEST 'confidence' probe — it must score per-probe, not blanket-fail the suite")
	}
}

// TestIntrospectSuiteOpaqueLayerHonestlyDeclines pins the §8(iv) honest-"I can't see that" property at the
// oracle: a subconscious probe that DECLINES is faithful (the correct answer for the opaque hidden seam); a
// subconscious probe that CONFABULATES an arbitration story (declined flipped off, a fabricated value) is
// UNFAITHFUL — the introspective twin of the DECLINE neg-control.
func TestIntrospectSuiteOpaqueLayerHonestlyDeclines(t *testing.T) {
	truth := map[string]string{"thinking": "x", "why": "y", "confidence": "z"}

	declined := scoreSuite([]introspectProbe{introspectDeclinedProbe("subconscious", "q", introspectLayerOpaque)}, truth)
	if !declined.Probes[0].Faithful || declined.Declined != 1 {
		t.Fatalf("an honest decline of the opaque subconscious must be FAITHFUL + counted declined, got faithful=%v declined=%d",
			declined.Probes[0].Faithful, declined.Declined)
	}

	// a CONFABULATED subconscious probe (declined=false, a fabricated arbitration story) must NOT be faithful
	confab := introspectProbe{Name: "subconscious", Layer: introspectLayerOpaque,
		Reported: "the gate arbitrated candidate #3 over #1 by a 0.2 margin", Declined: false}
	r := scoreSuite([]introspectProbe{confab}, truth)
	if r.Probes[0].Faithful {
		t.Fatal("a confabulated subconscious arbitration story read FAITHFUL — the opaque hidden seam must be DECLINED, never narrated (laundered hallucination the Filter kills)")
	}
}

// TestIntrospectSuiteOffIsByteIdentical is the default-OFF guard: with introspect.suite OFF (the default), the
// suite never runs and emits NO introspect.suite event on the live loop — byte-identical to the pre-instrument
// engine. The guard field is only ever read/written on the ON path, so the bare loop never touches it.
func TestIntrospectSuiteOffIsByteIdentical(t *testing.T) {
	e, log := introspectSuiteEngine(t, false)
	driveToQuiescence(e)
	if n := len(log.of(events.IntrospectSuite)); n != 0 {
		t.Fatalf("introspect.suite OFF (default): must emit no introspect.suite event, got %d (not byte-identical)", n)
	}
}
