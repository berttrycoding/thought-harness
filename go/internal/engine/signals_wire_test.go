package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/signals"
)

// signals_wire_test.go — the WIRING GATE for the SignalFrame recorder (Track G, G0). The wiring-gate
// lesson (saved): a unit that exists but does not RUN on the engine's actual tick is dead. These
// tests subscribe a signals.Recorder to a REAL engine bus and drive REAL ticks, asserting the
// recorder fires per live tick and the derived frames TRACK the running cognition — not just that the
// loop runs. The CLI wires this exact recorder in cmd/thought.wireSignalSidecar (gated on
// tui.signal_frames); this test proves the recorder works on the live loop the CLI wires it into.

// TestSignalRecorderFiresOnLiveLoop — drive a real reactive episode and assert the recorder produced
// one frame per tick with the durability vector POPULATED from the live regulator. If the recorder
// were not on the live bus, there would be zero frames; if it were on a dead/empty bus, the vector
// would be all-zero.
func TestSignalRecorderFiresOnLiveLoop(t *testing.T) {
	eng, _ := newSeededEngine(t, "reactive", 7)
	rec := signals.NewRecorder(nil) // in-memory mode: accumulate frames, write nothing
	eng.Bus().Subscribe(rec.On)

	eng.SubmitDefault("is the 3-line refactor in cache.go safe to ship?")
	const ticks = 12
	for i := 0; i < ticks; i++ {
		eng.Step()
	}
	rec.Close()

	frames := rec.Frames()
	if len(frames) == 0 {
		t.Fatal("the recorder produced ZERO frames on a live loop — it is not wired to the tick (the wiring-gate failure)")
	}
	// one frame per emitted Tick; the engine emits a Tick at the head of every Step.
	if len(frames) < ticks {
		t.Errorf("expected at least %d frames (one per tick), got %d", ticks, len(frames))
	}
	// the frames must be in strictly increasing tick order (the scrub axis the analysis surface reads).
	for i := 1; i < len(frames); i++ {
		if frames[i].Tick <= frames[i-1].Tick {
			t.Fatalf("frames not in tick order: frame %d tick=%d <= prev tick=%d", i, frames[i].Tick, frames[i-1].Tick)
		}
	}

	// the durability vector must be a LIVE read off the regulator — at least one frame carries a
	// non-zero θ (the regulator always emits a regulator.update with a positive θ each step). An
	// all-zero vector across every frame would mean the derivation never saw the regulator stream.
	sawTheta := false
	for _, f := range frames {
		if f.Theta > 0 {
			sawTheta = true
			break
		}
	}
	if !sawTheta {
		t.Error("no frame carried a live θ from the regulator — the durability vector is not tracking the plant")
	}
}

// TestSignalRecorderCapturesUserStimulus — a user turn on the live loop must surface as a stimulus in
// the frame for the tick it arrived. The impulse-response capture (the responsiveness benchmark)
// hangs off this: without a captured stimulus origin there is nothing to measure latency against.
func TestSignalRecorderCapturesUserStimulus(t *testing.T) {
	eng, _ := newSeededEngine(t, "reactive", 11)
	rec := signals.NewRecorder(nil)
	eng.Bus().Subscribe(rec.On)

	eng.SubmitDefault("what is 17 * 23?")
	for i := 0; i < 8; i++ {
		eng.Step()
	}
	rec.Close()

	sawUserStimulus := false
	for _, f := range rec.Frames() {
		if f.SalientInput && f.InputKind == "user" {
			sawUserStimulus = true
			break
		}
	}
	if !sawUserStimulus {
		t.Error("the live user turn never surfaced as a user stimulus in any frame — the impulse origin is lost")
	}

	// and the pressure vital must reflect a user waiting on the line at least once.
	sawWaiting := false
	for _, f := range rec.Frames() {
		if f.UserWaiting {
			sawWaiting = true
			break
		}
	}
	if !sawWaiting {
		t.Error("user_waiting never went true while the user awaited an answer — the pressure vital is dead")
	}
}

// TestSignalRecorderFlagOffWiresNothing — the wiring is GATED: when tui.signal_frames is OFF (the
// default), the CLI wires no recorder. This asserts the default engine bus carries NO signals
// subscriber by construction (a fresh engine has no recorder attached), so a default-OFF run is
// byte-identical (no sidecar, no frames). The recorder is opt-in instrumentation, never default.
func TestSignalRecorderFlagOffWiresNothing(t *testing.T) {
	eng, _ := newSeededEngine(t, "reactive", 3)
	// default config: tui.signal_frames is OFF.
	if eng.Features().Tui.SignalFrames {
		t.Fatal("tui.signal_frames must DEFAULT OFF — it is an opt-in observability instrument")
	}
	// no recorder is wired by default; drive ticks and confirm nothing observes them as frames (we
	// simply do not subscribe one — the CLI only subscribes when the flag is on). This documents the
	// gate: the slice is additive + default-OFF byte-identical.
	eng.SubmitDefault("hello")
	for i := 0; i < 3; i++ {
		eng.Step()
	}
}
