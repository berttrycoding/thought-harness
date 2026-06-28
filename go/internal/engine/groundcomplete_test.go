package engine

import (
	"regexp"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestResolveGroundCompleteParsing pins the env-knob parser (default OFF keeps the byte-identical
// RESPOND prompt). Mirrors TestResolveForceGroundParsing: ON only for explicit affirmatives; unset /
// "0" / "false" / garbage / whitespace are OFF — robustness of the resolver.
func TestResolveGroundCompleteParsing(t *testing.T) {
	cases := map[string]bool{
		"": false, "0": false, "false": false, "garbage": false, "  ": false, "off": false, "no": false,
		"1": true, "true": true, "On": true, "yes": true, " on ": true, "TRUE": true, " 1 ": true,
	}
	for val, want := range cases {
		t.Setenv("THOUGHT_GROUND_COMPLETE", val)
		if got := resolveGroundComplete(); got != want {
			t.Errorf("resolveGroundComplete(%q) = %v, want %v", val, got, want)
		}
	}
}

// TestGroundCompleteDirectiveProperties asserts the ENGINE's REAL directive (the one Respond receives)
// is GENERAL + DECLINE-SAFE + carries no hardcoded answer — the red-team's load-bearing conditions, on
// the actual production string (not a test copy).
func TestGroundCompleteDirectiveProperties(t *testing.T) {
	d := groundCompleteDirective
	lower := strings.ToLower(d)

	// NO hardcoded answer/number/enumerated index: the directive contains NO digit at all.
	if regexp.MustCompile(`[0-9]`).MatchString(d) {
		t.Fatalf("directive must carry NO digit (no hardcoded answer/number); got %q", d)
	}

	// NO enumerated trigger keywords (the overfitting trap). The directive must describe a GENERAL
	// reading behaviour, never a keyword lookup table.
	for _, kw := range []string{
		"deprecated", "erratum", "errata", "flag", "multiplier", "multiply by", "superseded",
		"overridden", "patch", "hotfix", "bugfix", "changelog", "footnote", "addendum", "revision",
		"version 2", "tax", "discount",
	} {
		if strings.Contains(lower, kw) {
			t.Fatalf("directive must NOT enumerate the specific trigger keyword %q (overfits); got %q", kw, d)
		}
	}

	// The GENERAL language is present (in-force / correction / adjustment-or-conversion).
	for _, must := range []string{"corrects, replaces, or overrides", "value actually in force",
		"adjusted or converted", "apply that stated adjustment or conversion"} {
		if !strings.Contains(lower, strings.ToLower(must)) {
			t.Fatalf("directive must contain the general phrase %q; got %q", must, d)
		}
	}

	// DECLINE-SAFETY clause is present (the anti-confabulation protection).
	for _, must := range []string{"never invent a value", "decline",
		"external file, package, or dashboard you cannot read", "not determinable from the material"} {
		if !strings.Contains(lower, strings.ToLower(must)) {
			t.Fatalf("directive must contain the decline-safety phrase %q; got %q", must, d)
		}
	}
}

// groundCompleteRecorder wraps the test double with a recording GroundCompletePrompter, proving the
// engine pushes the fragment into a capable backend right before respond.
type groundCompleteRecorder struct {
	*backends.TestBackend
	fragments []string
}

func (g *groundCompleteRecorder) SetGroundCompleteFragment(f string) {
	g.fragments = append(g.fragments, f)
}

var _ backends.GroundCompletePrompter = (*groundCompleteRecorder)(nil)

// runRespondOnce builds a reactive engine on the recorder backend, runs a grounding-shaped episode to a
// respond, and returns the recorder + the captured conscious.ground_complete events.
func runRespondOnce(t *testing.T) (*groundCompleteRecorder, int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	rec := &groundCompleteRecorder{TestBackend: backends.NewTest()}
	e, err := NewEngine(&cfg, rec)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var gcEvents int
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.GroundComplete {
			gcEvents++
		}
	})
	e.SubmitDefault("investigate the config and report the value of MaxRetries")
	e.Run(20)
	return rec, gcEvents
}

// TestGroundCompleteOffClearsAndNoEvent: with the flag OFF (the default), the respond path pushes the
// EMPTY fragment (explicit clear ⇒ byte-identical prompt) and emits NO conscious.ground_complete event.
func TestGroundCompleteOffClearsAndNoEvent(t *testing.T) {
	prev := groundCompleteEnabled
	defer func() { groundCompleteEnabled = prev }()
	groundCompleteEnabled = false

	rec, gcEvents := runRespondOnce(t)
	if len(rec.fragments) == 0 {
		t.Fatalf("the respond path must push the ground-complete fragment into a capable backend (even to clear it)")
	}
	for i, f := range rec.fragments {
		if f != "" {
			t.Fatalf("flag OFF -> every pushed fragment must be empty (byte-identical prompt); fragments[%d] = %q", i, f)
		}
	}
	if gcEvents != 0 {
		t.Fatalf("flag OFF must emit NO conscious.ground_complete event; got %d", gcEvents)
	}
}

// TestGroundCompleteOnPushesDirectiveAndEmits: with the flag ON, the respond path pushes the REAL
// directive into the backend and emits conscious.ground_complete (observability — never silent).
func TestGroundCompleteOnPushesDirectiveAndEmits(t *testing.T) {
	prev := groundCompleteEnabled
	defer func() { groundCompleteEnabled = prev }()
	groundCompleteEnabled = true

	rec, gcEvents := runRespondOnce(t)
	if len(rec.fragments) == 0 {
		t.Fatalf("the respond path must push the ground-complete fragment into a capable backend")
	}
	sawDirective := false
	for _, f := range rec.fragments {
		if f == groundCompleteDirective {
			sawDirective = true
		} else if f != "" {
			t.Fatalf("flag ON must push EITHER the exact directive OR \"\"; got an unexpected fragment %q", f)
		}
	}
	if !sawDirective {
		t.Fatalf("flag ON must push the exact directive at least once; fragments = %v", rec.fragments)
	}
	if gcEvents == 0 {
		t.Fatalf("flag ON must emit conscious.ground_complete when the directive engages (never silent)")
	}
}

// TestGroundCompleteFragmentMethod is the unit on the fragment renderer: OFF -> "" (byte-identical),
// ON -> the exact directive. No engine run needed.
func TestGroundCompleteFragmentMethod(t *testing.T) {
	prev := groundCompleteEnabled
	defer func() { groundCompleteEnabled = prev }()

	e := &Engine{}
	groundCompleteEnabled = false
	if got := e.groundCompleteFragment(); got != "" {
		t.Fatalf("OFF -> fragment must be \"\" (byte-identical); got %q", got)
	}
	groundCompleteEnabled = true
	if got := e.groundCompleteFragment(); got != groundCompleteDirective {
		t.Fatalf("ON -> fragment must be the exact directive; got %q", got)
	}
}

// TestGroundCompleteTestDoubleByteIdentical: the test double does NOT implement GroundCompletePrompter,
// so applyGroundCompleteFragment is a no-op on it — proving the offline/golden path is untouched by
// construction even with the flag ON. (The plain test double, not the recorder.)
func TestGroundCompleteTestDoubleByteIdentical(t *testing.T) {
	prev := groundCompleteEnabled
	defer func() { groundCompleteEnabled = prev }()
	groundCompleteEnabled = true

	if _, ok := backends.Backend(backends.NewTest()).(backends.GroundCompletePrompter); ok {
		t.Fatalf("the test double must NOT implement GroundCompletePrompter (the golden path must be untouched by construction)")
	}
	// applyGroundCompleteFragment on a non-prompter backend must not panic and must emit nothing.
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 1
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var gcEvents int
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.GroundComplete {
			gcEvents++
		}
	})
	e.applyGroundCompleteFragment() // must be a silent no-op on a non-prompter backend
	if gcEvents != 0 {
		t.Fatalf("applyGroundCompleteFragment on a non-prompter backend must emit nothing; got %d", gcEvents)
	}
}
