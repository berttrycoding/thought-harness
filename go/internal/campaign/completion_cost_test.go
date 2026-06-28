package campaign

// completion_cost_test.go — W5-0b: the probe REPLAY-COST instrument. The Skill-Miner curve (the W5
// definition-of-done) is gated on COMPLETION-only tokens (the cache-immune decode cost), so the probe
// must accumulate `completion_tokens` from each llm.call event into ProbeResult/CogResult/CogStability —
// the prompt half is prompt-cache-masked on the frontier substrate and must NOT count toward the curve.
//
// HONEST NOTE: on the offline `test` double an episode emits NO real usage (completion_tokens is absent),
// so a live probe reports Completion=0 — a constant. A test that ran a real episode would pass even if the
// wiring were hardcoded to 0. To test the WIRING (not a constant) this file INJECTS synthetic llm.call
// events carrying completion_tokens and asserts the accumulation sums ONLY the completion half. It is
// mutation-sensitive: hardcode addLLMCost's completion fold to 0 (or drop it) and these tests FAIL.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestCognitionProbeReplaysVectorWiring drives the COGNITION replays path on the test double and asserts
// the per-replay completion VECTOR is retained (W5-1 cost axis): one sample per replay (length K) and the
// vector sums to the existing Completion total (additive, not a replacement). All zeros offline (no real
// usage) → cost-σ=0 → honest DEGENERATE on the cost axis downstream.
func TestCognitionProbeReplaysVectorWiring(t *testing.T) {
	const k = 3
	b := EngineBencher{MaxTicks: 20, NewEngine: testEngineFactory}
	tasks := []CognitionTask{
		{Goal: "what is 12 times 7?", Signature: "deliberate"},
		{Goal: "should we ship this risky change?", Signature: "act"},
	}
	rows := b.CognitionProbeReplays(tasks, "", k)
	if len(rows) != len(tasks) {
		t.Fatalf("rows = %d, want %d", len(rows), len(tasks))
	}
	for i, r := range rows {
		if r.Replays != k {
			t.Errorf("row %d Replays = %d, want %d", i, r.Replays, k)
		}
		if len(r.Completions) != k {
			t.Errorf("row %d Completions length = %d, want %d (one sample per replay)", i, len(r.Completions), k)
		}
		var sum int
		for _, c := range r.Completions {
			sum += c
		}
		if sum != r.Completion {
			t.Errorf("row %d sum(Completions)=%d must equal Completion=%d (additive over the sum)", i, sum, r.Completion)
		}
	}
}

// llmEv builds a synthetic llm.call event carrying the given prompt/completion usage (in-process emit
// stores plain ints; intData also tolerates the float64 a JSON round-trip would produce — covered below).
func llmEv(prompt, completion int) events.Event {
	return events.Event{Kind: events.LLM, Data: events.D{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
	}}
}

// TestAddLLMCost is the unit guard on the SINGLE source of the probe cost wiring: completion must sum the
// completion half ONLY, the total must sum prompt+completion, calls must count llm.call events, and a
// non-llm.call event must be ignored entirely. nil pointers (the cognition probe passes nil tokens) must
// be skipped without counting.
func TestAddLLMCost(t *testing.T) {
	var calls, tokens, completion int
	evs := []events.Event{
		llmEv(100, 10),
		llmEv(200, 30),
		{Kind: "seam.filter", Data: events.D{"completion_tokens": 999}}, // NOT llm.call — must be ignored
		llmEv(0, 5),
	}
	for _, ev := range evs {
		addLLMCost(ev, &calls, &tokens, &completion)
	}
	// 3 llm.call events (the filter event ignored).
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (only llm.call events count; the seam.filter event must be ignored)", calls)
	}
	// completion = 10+30+5 = 45 — the COMPLETION half only (NOT the 100+200 prompt, NOT the 999 from the
	// non-llm.call event). This is the cache-immune Skill-Miner-curve signal.
	if completion != 45 {
		t.Errorf("completion = %d, want 45 (10+30+5 completion ONLY; mutation guard: a hardcoded-0 or prompt-inclusive fold fails here)", completion)
	}
	// total = (100+10)+(200+30)+(0+5) = 345 — prompt+completion summed.
	if tokens != 345 {
		t.Errorf("tokens = %d, want 345 (prompt+completion total)", tokens)
	}
}

// TestAddLLMCostNilTokens covers the cognition-probe call shape: nil tokens pointer (it tracks calls +
// completion, not the prompt-inflated total) must accumulate completion without panicking.
func TestAddLLMCostNilTokens(t *testing.T) {
	var calls, completion int
	addLLMCost(llmEv(500, 12), &calls, nil, &completion)
	addLLMCost(llmEv(500, 8), &calls, nil, &completion)
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if completion != 20 {
		t.Errorf("completion = %d, want 20 (12+8); nil tokens must not drop completion accounting", completion)
	}
}

// TestAddLLMCostJSONRoundTrip asserts the float64 path (a JSON round-trip stores numbers as float64) is
// folded identically — the persisted-event path must not silently zero completion.
func TestAddLLMCostJSONRoundTrip(t *testing.T) {
	var calls, tokens, completion int
	ev := events.Event{Kind: events.LLM, Data: events.D{
		"prompt_tokens":     float64(100),
		"completion_tokens": float64(40),
	}}
	addLLMCost(ev, &calls, &tokens, &completion)
	if completion != 40 || tokens != 140 || calls != 1 {
		t.Errorf("float64 path: calls=%d tokens=%d completion=%d, want 1/140/40", calls, tokens, completion)
	}
}

// TestProbeResultCompletionWiring drives the EXACT production closure shape (addLLMCost into a real
// ProbeResult's fields) and asserts the Completion field carries completion-only — proving the wiring
// reaches the report struct, not just the helper's locals.
func TestProbeResultCompletionWiring(t *testing.T) {
	var r ProbeResult
	addLLMCost(llmEv(300, 25), &r.Calls, &r.Tokens, &r.Completion)
	addLLMCost(llmEv(150, 15), &r.Calls, &r.Tokens, &r.Completion)
	if r.Completion != 40 {
		t.Errorf("ProbeResult.Completion = %d, want 40 (25+15 completion ONLY)", r.Completion)
	}
	if r.Tokens != 490 {
		t.Errorf("ProbeResult.Tokens = %d, want 490 (prompt+completion total)", r.Tokens)
	}
	if r.Calls != 2 {
		t.Errorf("ProbeResult.Calls = %d, want 2", r.Calls)
	}
}

// TestCogResultCompletionWiring mirrors the cognition-probe closure (nil tokens) into a real CogResult.
func TestCogResultCompletionWiring(t *testing.T) {
	var r CogResult
	addLLMCost(llmEv(300, 7), &r.Calls, nil, &r.Completion)
	addLLMCost(llmEv(150, 3), &r.Calls, nil, &r.Completion)
	if r.Completion != 10 {
		t.Errorf("CogResult.Completion = %d, want 10 (7+3 completion ONLY)", r.Completion)
	}
	if r.Calls != 2 {
		t.Errorf("CogResult.Calls = %d, want 2", r.Calls)
	}
}

// TestCogStabilityMeanCompletion guards the replays-path summary math: MeanCompletion is the per-replay
// average (the Skill-Miner-curve y-value), and a zero-replay row must not divide by zero.
func TestCogStabilityMeanCompletion(t *testing.T) {
	s := CogStability{Replays: 4, Completion: 100} // 100 completion tokens summed over 4 replays
	if got := s.MeanCompletion(); got != 25 {
		t.Errorf("MeanCompletion = %v, want 25 (100/4)", got)
	}
	if got := (CogStability{}).MeanCompletion(); got != 0 {
		t.Errorf("zero-replay MeanCompletion = %v, want 0 (no divide-by-zero)", got)
	}
}
