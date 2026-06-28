package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/trace"
)

// TestLiveClaudeAwakeMultiHopTrace drives the AWAKE (continuous) engine on the live claude bridge with
// MULTIPLE multi-hop inputs arriving OVER the stream — the second/third arrive mid-flight, before the
// prior input's subconscious round-trip resolves. It captures the full event stream to
// /tmp/awake_trace.jsonl for PHASE/DESYNC analysis: does the conscious stay in sync with the subconscious
// round-trip when a new input lands while one is in flight, or does it free-run / answer-before-subconscious
// / retracement-patch? Gated behind THOUGHT_LIVE_CLAUDE (newLiveEngine skips otherwise). This is a TRACE
// CAPTURE instrument, not a pass/fail regression gate — it drives + records; analysis is post-hoc.
func TestLiveClaudeAwakeMultiHopTrace(t *testing.T) {
	eng, log := newLiveEngine(t, "continuous", 7) // SKIPS unless THOUGHT_LIVE_CLAUDE=1

	sink, err := trace.NewJsonlSink("/tmp/awake_trace.jsonl")
	if err != nil {
		t.Fatalf("open trace sink: %v", err)
	}
	defer sink.Close()
	eng.Bus().Subscribe(sink.On)

	step := func(n int) {
		for i := 0; i < n; i++ {
			eng.Step()
		}
	}

	step(3) // already awake + wandering — the mid-session state OnInterrupt is built for

	// INPUT 1 — a multi-hop reasoning problem (forces a multi-phase subconscious round-trip).
	eng.SubmitDefault("design a rate limiter that supports BOTH per-tenant and a global cap, and explain precisely how the two interact when a tenant's burst would push the system past the global cap")
	step(5) // round-trip on input 1 starts (a multi-phase workflow typically spans several ticks)

	// INPUT 2 — arrives MID-FLIGHT (input 1 likely not yet resolved): a cross-hop follow-up that DEPENDS on input 1.
	eng.SubmitDefault("now, given that design, what happens to in-flight requests when the global cap is hot-reloaded to a LOWER value mid-traffic?")
	step(5) // input 1 and input 2 round-trips now overlap — the desync stressor

	// INPUT 3 — a third interrupt: a separate multi-hop problem (concurrent cognitive load).
	eng.SubmitDefault("separately: trace how a token refill and a burst-drain race if they fire on the same tick, and which wins")
	step(8) // drain

	if len(log.events) == 0 {
		t.Fatal("no events captured — the awake stream produced nothing")
	}
	t.Logf("awake multi-hop multi-input trace: %d events captured to /tmp/awake_trace.jsonl", len(log.events))
}
