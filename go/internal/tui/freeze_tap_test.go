package tui

// freeze_tap_test.go — the G1 freeze-tap GATE + WIRING tests. The freeze tap is the bridge-side path
// that lets ^P + the ANALYSIS surface reconstruct a real session record of the RUNNING mind (vs the
// synthetic SampleAnalysisRecord). It is opt-in (tui.session_record, default OFF) and observation-only.
// These prove the gate (default OFF ⇒ no capture, byte-identical) and the wiring (knob ON ⇒ the tap
// captures the live bus and FreezeRecord reconstructs the cognition the surface reads).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// newTapBridge builds a bridge around a real reactive engine with tui.session_record set to `on`,
// drives a real episode, and returns the bridge so the test can read the freeze tap.
func newTapBridge(t *testing.T, on bool) *EngineBridge {
	t.Helper()
	feat := config.AllOn()
	feat.Tui.SessionRecord = on
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = &feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	b := NewBridge(e)
	e.SubmitDefault("is the 3-line refactor in cache.go safe to ship?")
	for i := 0; i < 14; i++ {
		e.Step()
	}
	return b
}

// TestFreezeTapDefaultOffCapturesNothing — with tui.session_record OFF (the default) the tap captures
// no events and FreezeRecord yields an empty record (the app then falls back to the sample). This is
// the gate: default-OFF allocates no ring and is byte-identical to no tap at all.
func TestFreezeTapDefaultOffCapturesNothing(t *testing.T) {
	b := newTapBridge(t, false)
	if got := len(b.FreezeEvents()); got != 0 {
		t.Errorf("tui.session_record OFF must capture nothing; got %d frozen events", got)
	}
	rec := b.FreezeRecord("x")
	if rec.Ticks != 0 || len(rec.Stimuli) != 0 || len(rec.Decisions) != 0 {
		t.Errorf("OFF must yield an empty record; got ticks=%d stimuli=%d decisions=%d",
			rec.Ticks, len(rec.Stimuli), len(rec.Decisions))
	}
}

// TestFreezeTapOnReconstructsRunningSession — with the knob ON the tap captures the live bus and
// FreezeRecord reconstructs the running mind: the user turn surfaces as a stimulus + impulse origin
// and the decision spine is captured. This is the ^P-freeze data path the analysis surface reads.
func TestFreezeTapOnReconstructsRunningSession(t *testing.T) {
	b := newTapBridge(t, true)
	if len(b.FreezeEvents()) == 0 {
		t.Fatal("tui.session_record ON must capture the live event stream; got 0 frozen events (the tap is not wired)")
	}
	rec := b.FreezeRecord("live-frozen")
	if rec.Name != "live-frozen" {
		t.Errorf("FreezeRecord must label the record; got name %q", rec.Name)
	}
	if len(rec.Decisions) == 0 {
		t.Error("the frozen record reconstructed no decision spine off the live bus")
	}
	sawUser := false
	for _, s := range rec.Stimuli {
		if s.Kind == "user" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Error("the live user turn never surfaced as a stimulus in the frozen record (the impulse origin is lost)")
	}
	if rec.SolveVerdict != "SOLVED" && rec.SolveVerdict != "UNSOLVED" {
		t.Errorf("the frozen record reached no outcome verdict; got %q", rec.SolveVerdict)
	}
}
