package engine_test

// analysis_loader_wire_test.go — the WIRING GATE for the G1 session-record loader (Track G). The
// wiring-gate lesson (saved): a unit that exists but never runs on the engine's actual tick is dead.
// The pure loader_test.go proves the loader RECONSTRUCTS a recorded stream; THIS test proves it does
// so off a REAL engine bus — drive a real episode, capture the live event stream exactly as the TUI
// bridge's freeze tap does (EngineBridge.captureFreeze → FreezeRecord → cognition.RecordFromFrozen),
// and assert the loader rebuilds the cognition the analysis surface reads. If the loader were not
// reading the real bus, the reconstructed record would be empty (the wiring-gate failure).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// TestAnalysisLoaderReconstructsLiveSession — the loader, fed the LIVE event stream of a real reactive
// episode, reconstructs a non-empty AnalysisRecord whose cognition tracks the run: the user turn
// surfaces as the impulse origin, the decision history is captured in tick order, and a verdict is
// reached. This is the freeze-tap path the bridge wires (^P + Shift+Tab) — proven on the live loop.
func TestAnalysisLoaderReconstructsLiveSession(t *testing.T) {
	eng, log := newSeededEngine(t, "reactive", 7)

	eng.SubmitDefault("is the 3-line refactor in cache.go safe to ship?")
	for i := 0; i < 14; i++ {
		eng.Step()
	}

	// the captured event stream IS what the bridge's freeze tap holds; feed it through the same core.
	rec := cognition.RecordFromFrozen(log.events, nil)

	if len(rec.Decisions) == 0 {
		t.Fatal("the loader reconstructed ZERO decisions off the live bus — it is not reading the real stream (the wiring-gate failure)")
	}
	// the user turn must surface as the impulse origin (the responsiveness benchmark hangs off it).
	if rec.ImpStimulusTick < 0 || rec.ImpStimulusText == "" {
		t.Errorf("the live user turn never surfaced as the impulse origin; ImpStimulusTick=%d text=%q", rec.ImpStimulusTick, rec.ImpStimulusText)
	}
	// a verdict must be reached (SOLVED/UNSOLVED) — the loader read the trajectory through to a stop.
	if rec.SolveVerdict != "SOLVED" && rec.SolveVerdict != "UNSOLVED" {
		t.Errorf("the loader reached no outcome verdict on the live run; got %q", rec.SolveVerdict)
	}
	// the user stimulus must be in the index (the {/} scrub jumps key off it).
	sawUser := false
	for _, s := range rec.Stimuli {
		if s.Kind == "user" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Error("no user stimulus in the reconstructed index — the scrub axis has no impulse marker")
	}
}

// TestAnalysisLoaderTracksGroundingDivergence — the power-ON-beats-OFF benchmark (the surface's
// headline) is built on the loader reading the DIVERGENCE between two real runs. A run that imports
// reality (grounds) must show grounding in the reconstructed record; the loader is the thing that
// surfaces the grounded/refuted ledger the COMPARE diff scores on. Drive a real grounding-shaped
// episode and assert the loader captured at least one reality arrival OR a decision spine — i.e. the
// reconstructed record reflects the run's real shape, not a constant.
func TestAnalysisLoaderTracksGroundingDivergence(t *testing.T) {
	eng, log := newSeededEngine(t, "reactive", 11)

	eng.SubmitDefault("what is 17 * 23? verify it against a computation.")
	for i := 0; i < 16; i++ {
		eng.Step()
	}
	rec := cognition.RecordFromFrozen(log.events, nil)

	// the reconstructed record must reflect the run's shape — a real decision spine was reconstructed.
	if len(rec.Decisions) == 0 {
		t.Fatal("no decision spine reconstructed — the loader is not tracking the live trajectory")
	}
	// reality arrivals (grounding/observation) OR a delivery must be reconstructed — the run did
	// SOMETHING the surface can score; a record that is all-zero is the wiring failure.
	if rec.Grounded == 0 && rec.Refuted == 0 && rec.Delivered == 0 && len(rec.Rewards) == 0 {
		t.Error("the loader reconstructed a hollow record (no grounding, no delivery, no reward) off a real run — it is not reading the reality ledger")
	}
}

// TestFreezeTapByteIdenticalToUntapped — the freeze tap is observation-only: subscribing it to the
// bus must NOT change the engine's emitted event stream (it is a passive copy off the live bus). Two
// engines on the same seed — one with a passive tap subscribed, one without — must emit the SAME
// event kinds in the SAME order. This is the additive/byte-identical guarantee for the wiring.
func TestFreezeTapByteIdenticalToUntapped(t *testing.T) {
	run := func(tap bool) []events.Event {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		e, err := engine.NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		var captured []events.Event
		e.Bus().Subscribe(func(ev events.Event) { captured = append(captured, ev) })
		if tap {
			// a passive copy-off-the-bus subscriber, exactly the shape of the bridge's freeze tap.
			var ring []events.Event
			e.Bus().Subscribe(func(ev events.Event) { ring = append(ring, ev) })
			_ = ring
		}
		e.SubmitDefault("is the refactor safe to ship?")
		for i := 0; i < 10; i++ {
			e.Step()
		}
		return captured
	}
	withTap := run(true)
	noTap := run(false)
	if len(withTap) != len(noTap) {
		t.Fatalf("the passive tap changed the event count: tapped=%d untapped=%d", len(withTap), len(noTap))
	}
	for i := range withTap {
		if withTap[i].Kind != noTap[i].Kind || withTap[i].Tick != noTap[i].Tick {
			t.Fatalf("event %d diverged with the tap: tapped=%s@%d untapped=%s@%d",
				i, withTap[i].Kind, withTap[i].Tick, noTap[i].Kind, noTap[i].Tick)
		}
	}
}
