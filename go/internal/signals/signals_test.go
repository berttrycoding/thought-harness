package signals

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// feed drives a recorder over a scripted event stream (tick boundaries are explicit Tick events) and
// returns the closed frames. The events carry the SAME data shape the live components emit.
func feed(t *testing.T, w *bytes.Buffer, script []events.Event) []SignalFrame {
	t.Helper()
	rec := NewRecorder(w)
	for _, ev := range script {
		rec.On(ev)
	}
	rec.Close()
	return rec.Frames()
}

func tick(n int) events.Event { return events.Event{Tick: n, Kind: events.Tick, Layer: "tick"} }

func ev(tk int, kind string, data events.D) events.Event {
	return events.Event{Tick: tk, Kind: kind, Layer: "", Data: data}
}

// TestOneFramePerTick — the bedrock contract: the recorder emits EXACTLY one frame per tick boundary,
// in tick order, and the in-progress final tick is flushed only on Close.
func TestOneFramePerTick(t *testing.T) {
	var buf bytes.Buffer
	frames := feed(t, &buf, []events.Event{
		tick(1),
		ev(1, events.Generate, events.D{}),
		tick(2),
		tick(3),
		ev(3, events.Decision, events.D{"decision": "THINK"}),
	})
	if len(frames) != 3 {
		t.Fatalf("expected exactly 3 frames (one per tick), got %d", len(frames))
	}
	for i, f := range frames {
		if f.Tick != i+1 {
			t.Errorf("frame %d: tick=%d, want %d", i, f.Tick, i+1)
		}
		if f.Schema != SchemaVersion {
			t.Errorf("frame %d: schema=%d, want %d", i, f.Schema, SchemaVersion)
		}
	}
	// the sidecar holds exactly one JSON line per frame.
	if got := bytes.Count(buf.Bytes(), []byte("\n")); got != 3 {
		t.Errorf("sidecar lines=%d, want 3", got)
	}
}

// TestDurabilityVectorTracksRegulator — the SignalFrame must carry the regulator's OWN durability
// terms (n/U/μ/θ/λ̄) verbatim, and derive reserve as the budget headroom (1−U as 0–100). This is the
// substrate the on/off durability comparison reads; if it doesn't track the plant, the chart lies.
func TestDurabilityVectorTracksRegulator(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1),
		ev(1, events.Regulator, events.D{"n": 0.25, "U": 0.40, "mu": 0.5, "theta": 0.55, "lam_bar": 0.667}),
		tick(2),
		ev(2, events.Regulator, events.D{"n": 0.30, "U": 0.90, "mu": 0.5, "theta": 0.60, "lam_bar": 0.714}),
	})
	f1, f2 := frames[0], frames[1]
	if f1.N != 0.25 || f1.U != 0.40 || f1.Mu != 0.5 || f1.Theta != 0.55 {
		t.Errorf("frame1 durability vector wrong: %+v", f1)
	}
	if f1.LambdaBar != 0.667 {
		t.Errorf("frame1 lambda_bar=%v, want 0.667", f1.LambdaBar)
	}
	// reserve = (1−U)*100 → 60 at U=0.40, 10 at U=0.90.
	if f1.Reserve != 60 {
		t.Errorf("frame1 reserve=%d, want 60 (1-U*100 at U=0.4)", f1.Reserve)
	}
	if f2.Reserve != 10 {
		t.Errorf("frame2 reserve=%d, want 10 (drains under load)", f2.Reserve)
	}
	// the level signals LATCH across ticks: frame2 carries the latest regulator read.
	if f2.U != 0.90 {
		t.Errorf("frame2 U=%v, want 0.90 (latest read latches)", f2.U)
	}
}

// TestConditionStory — the one-word condition is the Pattern-A composition the vitals mock locks:
// runaway excitation ⇒ DEGRADED, saturated load ⇒ LOADED, awake/user-waiting ⇒ ENGAGED, else NOMINAL.
func TestConditionStory(t *testing.T) {
	cases := []struct {
		name   string
		script []events.Event
		want   string
	}{
		{"nominal", []events.Event{tick(1), ev(1, events.Regulator, events.D{"n": 0.1, "U": 0.3})}, "NOMINAL"},
		{"loaded", []events.Event{tick(1), ev(1, events.Regulator, events.D{"n": 0.2, "U": 0.95})}, "LOADED"},
		{"degraded", []events.Event{tick(1), ev(1, events.Regulator, events.D{"n": 1.0, "U": 0.5})}, "DEGRADED"},
		{"engaged-awake", []events.Event{tick(1), ev(1, events.Arousal, events.D{"to": "AWAKE"})}, "ENGAGED"},
		{"engaged-waiting", []events.Event{tick(1), ev(1, events.Port, events.D{"source": "USER_INPUT", "text": "hi"})}, "ENGAGED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frames := feed(t, &bytes.Buffer{}, c.script)
			if frames[0].Condition != c.want {
				t.Errorf("condition=%q, want %q (frame %+v)", frames[0].Condition, c.want, frames[0])
			}
		})
	}
}

// TestStimulusMarksImpulseOrigin — a salient arrival (user input dominates a reality observation) is
// captured as the per-tick stimulus, with its kind. THIS is the impulse-response origin every
// responsiveness benchmark aligns on; the input strip is the spike train.
func TestStimulusMarksImpulseOrigin(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1), // quiet tick — no arrival
		tick(2),
		ev(2, events.Observation, events.D{"ok": true, "watched": true}), // reality lands
		tick(3),
		ev(3, events.Port, events.D{"source": "USER_INPUT", "text": "is this safe?"}), // user turn
		ev(3, events.Observation, events.D{"ok": true}),                               // + reality same tick
	})
	if frames[0].SalientInput || frames[0].InputKind != "none" {
		t.Errorf("quiet tick should have no stimulus: %+v", frames[0])
	}
	if !frames[1].SalientInput || frames[1].InputKind != "reality" {
		t.Errorf("tick2 should mark a reality stimulus: %+v", frames[1])
	}
	// user input DOMINATES a same-tick reality arrival (the impulse origin a benchmark hangs off).
	if !frames[2].SalientInput || frames[2].InputKind != "user" {
		t.Errorf("tick3 should mark a USER stimulus (dominates reality): %+v", frames[2])
	}
}

// TestPressureRisesWithWaitAge — pressure (stress) is demand on the system: once a user turn lands,
// user_waiting holds and its AGE in ticks climbs until the system responds, then drops. This is the
// "user waiting 6t" pressure read; a benchmark scores responsiveness on it.
func TestPressureRisesWithWaitAge(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(10),
		ev(10, events.Port, events.D{"source": "USER_INPUT", "text": "q"}), // user starts waiting at t=10
		tick(11),
		tick(13),
		ev(13, events.Respond, events.D{"kind": "respond"}), // answered at t=13
		tick(14),
	})
	// frame@10: waiting just started → age 0.
	if !frames[0].UserWaiting || frames[0].WaitingAgeTicks != 0 {
		t.Errorf("t=10 should be waiting age 0: %+v", frames[0])
	}
	// frame@13: answered DURING the tick → no longer waiting.
	if frames[2].UserWaiting {
		t.Errorf("t=13 answered → should not be waiting: %+v", frames[2])
	}
	// frame@11 (between arrival and answer) → still waiting, age 1.
	if !frames[1].UserWaiting || frames[1].WaitingAgeTicks != 1 {
		t.Errorf("t=11 should be waiting age 1: %+v", frames[1])
	}
}

// TestGroundingRespiration — observations imported in the trailing window + the grounded ratio. A
// closed loop running on priors (no grounding) reads low; a loop importing reality reads high. The
// "respiration" vital — a hallucination-risk read.
func TestGroundingRespiration(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1),
		ev(1, events.Ground, events.D{"verdict": "grounded"}),
		tick(2),
		ev(2, events.Ground, events.D{"verdict": "refuted"}),
		tick(3),
	})
	// by tick 3 the window has seen 2 observations (1 grounded, 1 refuted) → ratio 0.5.
	last := frames[2]
	if last.ObservationsInWindow != 2 {
		t.Errorf("observations_in_window=%d, want 2", last.ObservationsInWindow)
	}
	if last.GroundedRatio != 0.5 {
		t.Errorf("grounded_ratio=%v, want 0.5 (1 of 2)", last.GroundedRatio)
	}
}

// TestFaultsFromFallbacks — the "fever" vital: substrate fallbacks + parse failures in the window. A
// fallback whose reason mentions a parse/length cause is also a parse failure.
func TestFaultsFromFallbacks(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1),
		ev(1, events.LLMFallback, events.D{"reason": "unparseable model output"}),
		ev(1, events.LLMFallback, events.D{"finish_reason": "stop"}), // a non-parse fallback
		tick(2),
	})
	last := frames[1]
	if last.FallbacksInWindow != 2 {
		t.Errorf("fallbacks_in_window=%d, want 2", last.FallbacksInWindow)
	}
	if last.ParseFailuresInWindow != 1 {
		t.Errorf("parse_failures_in_window=%d, want 1 (only the unparseable one)", last.ParseFailuresInWindow)
	}
}

// TestCadenceFromCalls — per-tick model-call count + summed latency (the cadence/substrate-latency
// story). The frame attributes calls to the tick they fired in, not across ticks.
func TestCadenceFromCalls(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1),
		ev(1, events.LLM, events.D{"ms": 1200}),
		ev(1, events.LLM, events.D{"ms": 800}),
		tick(2),
		ev(2, events.LLM, events.D{"ms": 300}),
		tick(3), // no calls
	})
	if frames[0].CallsInTick != 2 || frames[0].TickLatencyMs != 2000 {
		t.Errorf("tick1 cadence wrong: calls=%d ms=%d (want 2/2000)", frames[0].CallsInTick, frames[0].TickLatencyMs)
	}
	if frames[1].CallsInTick != 1 || frames[1].TickLatencyMs != 300 {
		t.Errorf("tick2 cadence wrong: calls=%d ms=%d (want 1/300)", frames[1].CallsInTick, frames[1].TickLatencyMs)
	}
	if frames[2].CallsInTick != 0 || frames[2].TickLatencyMs != 0 {
		t.Errorf("tick3 (idle) should have 0 calls/0ms: %+v", frames[2])
	}
}

// TestActiveValueFromValueMap — V(active) is read from the value.update payload (the active branch id
// keys into the per-branch value map), tolerating the float forms a JSON replay yields.
func TestActiveValueFromValueMap(t *testing.T) {
	frames := feed(t, &bytes.Buffer{}, []events.Event{
		tick(1),
		ev(1, events.Value, events.D{"active": 2, "values": map[string]any{"b1": 0.3, "b2": 0.71}, "reward": 0.0}),
	})
	if frames[0].VActive != 0.71 {
		t.Errorf("v_active=%v, want 0.71 (V of the active branch b2)", frames[0].VActive)
	}
}

// TestLambdaBarCliffIsFinite — at the n≥1 cliff λ̄ is +∞; the frame must NOT carry a non-finite value
// (json.Marshal would error). It maps to 0 (off-the-chart) so the sidecar JSON stays valid.
func TestLambdaBarCliffIsFinite(t *testing.T) {
	var buf bytes.Buffer
	frames := feed(t, &buf, []events.Event{
		tick(1),
		ev(1, events.Regulator, events.D{"n": 1.0, "lam_bar": json.Number("1e999")}),
	})
	if frames[0].LambdaBar != 0 {
		t.Errorf("lambda_bar at the cliff should be 0 (finite-guard), got %v", frames[0].LambdaBar)
	}
	// the sidecar line must be valid JSON (no Inf/NaN).
	var rt SignalFrame
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rt); err != nil {
		t.Fatalf("sidecar line is not valid JSON: %v", err)
	}
}

// TestSidecarRoundTrips — the sidecar JSONL round-trips through SignalFrame (the G1 loader's contract:
// read the sidecar back into the same struct). Field tags + schema version must survive a marshal.
func TestSidecarRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	feed(t, &buf, []events.Event{
		tick(7),
		ev(7, events.Regulator, events.D{"n": 0.42, "U": 0.6, "theta": 0.5}),
		ev(7, events.Decision, events.D{"decision": "BRANCH"}),
	})
	var f SignalFrame
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &f); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if f.Tick != 7 || f.N != 0.42 || f.Decision != "BRANCH" || f.Schema != SchemaVersion {
		t.Errorf("round-trip lost fields: %+v", f)
	}
}

// TestNilWriterAccumulatesOnly — a nil writer is the in-memory mode (tests/UAT): frames accumulate,
// nothing is written. Never panics.
func TestNilWriterAccumulatesOnly(t *testing.T) {
	rec := NewRecorder(nil)
	rec.On(tick(1))
	rec.On(ev(1, events.Regulator, events.D{"n": 0.1}))
	rec.Close()
	if len(rec.Frames()) != 1 {
		t.Fatalf("nil-writer recorder should still accumulate frames, got %d", len(rec.Frames()))
	}
}

// TestCloseIsIdempotent — Close flushes the final in-progress tick exactly once; a second Close is a
// no-op (no duplicate final frame).
func TestCloseIsIdempotent(t *testing.T) {
	rec := NewRecorder(nil)
	rec.On(tick(1))
	rec.Close()
	rec.Close()
	if len(rec.Frames()) != 1 {
		t.Fatalf("double Close should not duplicate the final frame, got %d", len(rec.Frames()))
	}
}
