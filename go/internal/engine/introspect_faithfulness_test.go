package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// faithfulnessEngine builds a reactive engine on the test double with the introspective-faithfulness
// instrument knob set as given, capturing the full event stream. It is the harness for the §8 property
// tests (Track H, benchmark-taxonomy §8).
func faithfulnessEngine(t *testing.T, selfReport bool) (*Engine, *[]events.Event) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.New() // AllOn baseline
	feat.Introspect.SelfReport = selfReport
	feat.Validate()
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var log []events.Event
	e.Bus().Subscribe(func(ev events.Event) { log = append(log, ev) })
	return e, &log
}

// faithfulnessEvents returns every introspect.faithfulness event captured.
func faithfulnessEvents(log []events.Event) []events.Event {
	var out []events.Event
	for _, ev := range log {
		if ev.Kind == events.IntrospectFaithfulness {
			out = append(out, ev)
		}
	}
	return out
}

// TestIntrospectSelfReportIsFaithfulToReadableGroundTruth is the §8 introspective-faithfulness property
// over the READABLE layers: with introspect.self_report ON, a reactive episode emits one
// introspect.faithfulness witness at quiescence whose self-report NAMES THE ACTUAL state — the real goal,
// the real active-line tip, the real lifecycle state, the real V(s), the real recent-event count — not a
// plausible substitute. Every readable field AGREES with the engine's own exported ground truth (faithful),
// and the report is the conjunction over those fields. This is the introspective channel's Filter property:
// the self-report is the state that actually IS, not a confabulation.
func TestIntrospectSelfReportIsFaithfulToReadableGroundTruth(t *testing.T) {
	e, log := faithfulnessEngine(t, true)
	e.SubmitDefault("What is 7 times 8?")
	e.Run(20) // think the episode to quiescence so the readable layers are meaningful

	evs := faithfulnessEvents(*log)
	if len(evs) == 0 {
		t.Fatal("introspect.self_report ON: no introspect.faithfulness event — the self-report never fired at quiescence (instrument not wired into the live loop)")
	}
	ev := evs[len(evs)-1]

	// The reported state must MATCH the engine's own readable ground truth (its exported accessors) — the
	// self-report is faithful to what the harness actually is, not a plausible-sounding answer.
	if got, want := ev.Data["state"], e.LifecycleState(); got != want {
		t.Fatalf("self-report lifecycle state %q != actual %q (confabulated state)", got, want)
	}
	if got, want := ev.Data["goal"], e.Graph().Goal; got != want {
		t.Fatalf("self-report goal %q != actual goal %q (confabulated goal)", got, want)
	}
	if got, want := ev.Data["value"].(float64), e.ActiveValue(); got != want {
		t.Fatalf("self-report V(s) %v != actual %v (confabulated confidence)", got, want)
	}

	// Every readable field must agree with its independently-checked ground truth, and the top-level verdict
	// is the conjunction — a faithful report over the readable layers.
	faithful, _ := ev.Data["faithful"].(bool)
	if !faithful {
		t.Fatalf("self-report over the readable layers is not faithful: %v", ev.Data["fields"])
	}
	fields, _ := ev.Data["fields"].([]map[string]any)
	if len(fields) == 0 {
		t.Fatal("self-report carried no per-field agreement — the faithfulness check is not observable")
	}
	sawConscious, sawGoal := false, false
	for _, f := range fields {
		if f["layer"] == "conscious" {
			sawConscious = true
		}
		if f["layer"] == "goal" {
			sawGoal = true
		}
		if faith, _ := f["faithful"].(bool); !faith {
			t.Fatalf("readable field %v reported %q != observed %q (unfaithful field on a faithful run)",
				f["layer"], f["reported"], f["observed"])
		}
	}
	if !sawConscious || !sawGoal {
		t.Fatalf("self-report missing a readable layer (conscious=%v goal=%v) — it does not cover what the mind is thinking / its goal", sawConscious, sawGoal)
	}
}

// TestIntrospectHonestlyDeclinesTheOpaqueSubconsciousLayer is the honest-"I can't see that" property (§8 —
// the introspective twin of the DECLINE neg-control): the subconscious hidden seam (FILTER->GATE->TRANSFORM)
// is OPAQUE by design, so the self-report reports it as UNOBSERVABLE rather than confabulating an
// arbitration story. The opaque layer must appear in the declined set AND must NOT be smuggled into a
// readable field as if it were observable.
func TestIntrospectHonestlyDeclinesTheOpaqueSubconsciousLayer(t *testing.T) {
	e, log := faithfulnessEngine(t, true)
	e.SubmitDefault("is this refactor safe to ship?")
	e.Run(20)

	evs := faithfulnessEvents(*log)
	if len(evs) == 0 {
		t.Fatal("no introspect.faithfulness event to check the honest-decline property")
	}
	ev := evs[len(evs)-1]

	opaque, _ := ev.Data["opaque"].([]string)
	declinedSubconscious := false
	for _, l := range opaque {
		if l == introspectLayerSubconscious {
			declinedSubconscious = true
		}
	}
	if !declinedSubconscious {
		t.Fatalf("the opaque subconscious layer is not declined (opaque=%v) — the report claims to see the hidden seam (no honest 'I can't see that')", opaque)
	}
	// The decline must be HONEST, not a confabulation laundered into a readable field: no readable field may
	// claim to observe the subconscious hidden-seam layer.
	fields, _ := ev.Data["fields"].([]map[string]any)
	for _, f := range fields {
		if f["layer"] == introspectLayerSubconscious {
			t.Fatalf("the opaque subconscious layer leaked into a readable field %v — confabulated, not declined", f)
		}
	}
}

// TestIntrospectFaithfulnessOracleCatchesConfabulation is the DISCRIMINATING proof that the faithfulness
// check is a REAL oracle, not a tautology: when a field of the self-report is CONFABULATED (its reported
// value tampered to a plausible-but-false value), checkSelfReport re-derives the ground truth INDEPENDENTLY
// and flags the tampered field faithful=false — and the run unfaithful. This is the introspective Filter:
// laundered hallucination in the self-report channel is caught. A check that passed a confabulation would
// be a fake instrument; this asserts it is not.
func TestIntrospectFaithfulnessOracleCatchesConfabulation(t *testing.T) {
	e, _ := faithfulnessEngine(t, true)
	e.startEpisode("compute the area of a circle", true)
	// Let the episode think a couple of ticks so the active line / V(s) are non-trivial.
	e.Run(5)

	honest := e.buildSelfReport()
	// Sanity: an honest report over the readable layers is faithful (built FROM the ground truth).
	if checked := e.checkSelfReport(honest); !checked.Faithful {
		t.Fatalf("the honest self-report failed the oracle (faithful should hold by construction): %+v", checked.Fields)
	}

	// CONFABULATE the goal field: report a plausible-but-FALSE goal. The honest report's other fields are
	// untouched, so only this field should flip.
	confab := honest
	confab.Fields = append([]reportField(nil), honest.Fields...) // copy so we do not mutate the honest report
	tampered := false
	for i, f := range confab.Fields {
		if f.Layer == "goal" {
			confab.Fields[i].Reported = "win a chess tournament" // a plausible substitute that is NOT the real goal
			tampered = true
		}
	}
	if !tampered {
		t.Fatal("test setup: no goal field to confabulate — the report shape changed")
	}

	checked := e.checkSelfReport(confab)
	if checked.Faithful {
		t.Fatal("the oracle PASSED a confabulated self-report — the faithfulness check is a tautology, not a real instrument (laundered introspective hallucination not caught)")
	}
	// The specific tampered field must be the one flagged unfaithful, against the REAL goal (re-read
	// independently of the report's lie).
	flaggedGoal := false
	for _, f := range checked.Fields {
		if f.Layer == "goal" {
			if f.Faithful {
				t.Fatal("the confabulated goal field passed — the oracle trusted the report instead of the ground truth")
			}
			if f.Observed != e.Graph().Goal {
				t.Fatalf("the oracle did not re-read the real goal: observed %q, actual %q", f.Observed, e.Graph().Goal)
			}
			flaggedGoal = true
		}
		// Every OTHER field stays faithful — the oracle isolated the confabulation, it did not blanket-fail.
		if f.Layer != "goal" && !f.Faithful {
			t.Fatalf("a non-tampered field %v was spuriously flagged unfaithful — the oracle is over-broad", f.Layer)
		}
	}
	if !flaggedGoal {
		t.Fatal("the goal field was not re-checked by the oracle")
	}
}

// TestIntrospectSelfReportOffIsByteIdentical pins the DEFAULT-OFF byte-identical contract: with the same
// reactive episode but introspect.self_report OFF (the default), the live loop emits NO
// introspect.faithfulness event and the instrument touches nothing — the flag-OFF half of the additive,
// default-OFF wiring contract.
func TestIntrospectSelfReportOffIsByteIdentical(t *testing.T) {
	e, log := faithfulnessEngine(t, false)
	e.SubmitDefault("What is 7 times 8?")
	e.Run(20)

	if n := len(faithfulnessEvents(*log)); n != 0 {
		t.Fatalf("introspect.self_report OFF: must emit no introspect.faithfulness events, got %d (not byte-identical)", n)
	}
	// emitSelfReport is a no-op when disabled (returns ({}, false)).
	if _, ok := e.emitSelfReport(0); ok {
		t.Fatal("introspect.self_report OFF: emitSelfReport reported it fired (must be a no-op when disabled)")
	}
}

// TestIntrospectSelfReportBoundedOncePerQuiescence pins the bound: even across many idle ticks, the
// instrument emits AT MOST ONE self-report per episode-end (it is a passive read; re-emitting it every idle
// tick would spam the bus and is not a per-tick faculty). A second user turn opens a fresh episode and earns
// one fresh self-report at its own quiescence.
func TestIntrospectSelfReportBoundedOncePerQuiescence(t *testing.T) {
	e, log := faithfulnessEngine(t, true)
	e.SubmitDefault("What is 7 times 8?")
	e.Run(30) // many ticks past quiescence — the engine sits idle most of them

	if n := len(faithfulnessEvents(*log)); n != 1 {
		t.Fatalf("want exactly 1 self-report for one episode's quiescence, got %d (unbounded — re-emits while idle)", n)
	}

	// A second turn is a fresh episode -> exactly one more self-report at its quiescence.
	e.SubmitDefault("Is that even?")
	e.Run(30)
	if n := len(faithfulnessEvents(*log)); n != 2 {
		t.Fatalf("want 2 self-reports for two episodes, got %d (the per-episode guard did not reset)", n)
	}
}

// TestIntrospectFmtValueIsStableAndComparable guards the confidence-field comparison: V(s) must render the
// SAME way for the reported value and the re-read ground truth so a faithful confidence claim compares
// byte-for-byte (a float printed two different ways would spuriously read as unfaithful), and a negative
// epistemic value (a refuted line) renders honestly (sign-correct), not as a misleading positive.
func TestIntrospectFmtValueIsStableAndComparable(t *testing.T) {
	cases := map[float64]string{
		0.0:   "0.00",
		0.5:   "0.50",
		0.456: "0.46",
		0.454: "0.45",
		1.0:   "1.00",
		-0.25: "-0.25", // negative epistemic value renders sign-correct (honest), not as a positive
		-1.5:  "-1.50",
	}
	for v, want := range cases {
		if got := fmtValue(v); got != want {
			t.Errorf("fmtValue(%v) = %q, want %q", v, got, want)
		}
		// The load-bearing property: rendering the same value twice is identical (comparable).
		if fmtValue(v) != fmtValue(v) {
			t.Errorf("fmtValue(%v) is not stable across calls", v)
		}
	}
}
