package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestDetectOverride: the deterministic floor classifies STYLE feedback (and only style feedback).
func TestDetectOverride(t *testing.T) {
	cases := []struct {
		text         string
		trait, value string
		ok           bool
	}{
		{"that was too long, make it shorter", "verbosity", "terse", true},
		{"please be brief next time", "verbosity", "terse", true},
		{"can you give more detail and elaborate", "verbosity", "detailed", true},
		{"use bullet points please", "format", "list", true},
		{"answer in prose, no bullets", "format", "prose", true},
		{"plain language please, less jargon", "style", "plain", true},
		{"what is the capital of France?", "", "", false}, // substantive turn — never an override
		{"why is the build failing?", "", "", false},
	}
	for _, c := range cases {
		trait, value, ok := detectOverride(c.text)
		if ok != c.ok || trait != c.trait || value != c.value {
			t.Errorf("detectOverride(%q) = (%q,%q,%v), want (%q,%q,%v)", c.text, trait, value, ok, c.trait, c.value, c.ok)
		}
	}
}

// personEngine builds a reactive test-backend engine with an event log.
func personEngine(t *testing.T) (*Engine, *[]events.Event) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var log []events.Event
	e.Bus().Subscribe(func(ev events.Event) { log = append(log, ev) })
	return e, &log
}

// TestPersonLoopLearnsFromConsistentFeedback drives the FULL P7.3 loop on the test backend: three
// consecutive style-feedback turns (each arriving after an answer) cross the threshold, the
// preference is LEARNED, surfaced on the bus, and the persona fragment renders it. The audit's
// "looks built, doesn't run" gap is closed only if this passes end-to-end through the ENGINE (not
// the registry alone).
func TestPersonLoopLearnsFromConsistentFeedback(t *testing.T) {
	e, log := personEngine(t)

	turns := []string{
		"weigh the options for the rollout and recommend one",
		"too long — shorter please",
		"again too long, be brief",
		"still too verbose, be terse",
	}
	for _, turn := range turns {
		e.SubmitDefault(turn)
		e.Run(20) // run the episode to its answer so the NEXT turn is feedback on it
	}

	// the preference must be LEARNED in the registry...
	p, ok := e.Person().Preference("verbosity")
	if !ok || p.Value != "terse" || p.Evidence < 3 {
		t.Fatalf("verbosity=terse should be LEARNED after 3 consistent overrides, got %+v ok=%v", p, ok)
	}
	// ...surfaced on the bus the moment it crossed the threshold...
	found := false
	for _, ev := range *log {
		if ev.Kind == events.MemoryRecord {
			if k, _ := ev.Data["kind"].(string); k == "preference" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("learning a preference must emit memory.record kind=preference")
	}
	// ...and rendered into the persona fragment the respond prompt would carry.
	frag := e.personaFragment()
	if !strings.Contains(frag, "verbosity=terse") {
		t.Fatalf("personaFragment should carry the learned preference, got %q", frag)
	}
}

// TestPersonNoFeedbackNoPreference: substantive turns never accumulate preferences, and with nothing
// learned the persona fragment is EMPTY — the respond prompt stays byte-identical (the honesty/
// default-path contract).
func TestPersonNoFeedbackNoPreference(t *testing.T) {
	e, _ := personEngine(t)
	for _, turn := range []string{
		"weigh the options for the rollout",
		"what about the second option?",
		"and the risks?",
	} {
		e.SubmitDefault(turn)
		e.Run(20)
	}
	if len(e.Person().Applied()) != 0 {
		t.Fatalf("substantive turns must not learn preferences: %+v", e.Person().Applied())
	}
	if frag := e.personaFragment(); frag != "" {
		t.Fatalf("no learned preferences -> empty fragment, got %q", frag)
	}
}

// TestFirstTurnIsNeverAnOverride: style wording in the FIRST turn (no prior answer) is a request,
// not feedback — it must not count as an override observation.
func TestFirstTurnIsNeverAnOverride(t *testing.T) {
	e, _ := personEngine(t)
	e.SubmitDefault("be brief: what are the rollout options?")
	e.Run(20)
	if len(e.Person().All()) != 0 {
		t.Fatalf("a first-turn style request must not be an override observation: %+v", e.Person().All())
	}
}

// personaRecorder wraps the test double with a recording PersonaPrompter, proving the engine pushes
// the fragment into a capable backend right before respond.
type personaRecorder struct {
	*backends.TestBackend
	fragments []string
}

func (p *personaRecorder) SetPersonaFragment(f string) { p.fragments = append(p.fragments, f) }

// TestApplyPersonaFragmentReachesBackend: with a PersonaPrompter-capable backend, the respond path
// receives the fragment ("" while nothing is learned — the explicit clear).
func TestApplyPersonaFragmentReachesBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	rec := &personaRecorder{TestBackend: backends.NewTest()}
	e, err := NewEngine(&cfg, rec)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SubmitDefault("weigh the options for the rollout and recommend one")
	e.Run(20)
	if len(rec.fragments) == 0 {
		t.Fatalf("the respond path must push the persona fragment into a capable backend")
	}
	for _, f := range rec.fragments {
		if f != "" {
			t.Fatalf("nothing learned -> every pushed fragment must be empty, got %q", f)
		}
	}
}
