package campaign

// decode_aggregator_test.go — GATE-1 instrument unit tests. The aggregator is the GROUP-BY-role
// analogue of addLLMCost, so these tests INJECT synthetic llm.call events carrying a role +
// completion_tokens (the offline test double emits no real usage, so a constant-0 fold would pass
// a live test). They assert the per-role fold sums completion ONLY, partitions by the verbatim
// role label, reconciles its total with addLLMCost over the SAME stream, and computes shares
// exactly. Mutation-sensitive: a hardcoded-0 / prompt-inclusive / un-grouped fold fails here.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// roleEv builds a synthetic llm.call event carrying a role label + prompt/completion usage.
func roleEv(role string, prompt, completion int) events.Event {
	return events.Event{Kind: events.LLM, Data: events.D{
		"role":              role,
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
	}}
}

// TestDecodeAggregatorGroupsByRole asserts the fold partitions completion tokens by the verbatim
// role label, sorts heaviest-first, and computes shares/absolutes exactly.
func TestDecodeAggregatorGroupsByRole(t *testing.T) {
	agg := NewDecodeAggregator()
	evs := []events.Event{
		roleEv("synthesize_program", 1000, 600), // synthesis: 600 + 200 = 800
		roleEv("synthesize_program", 1000, 200),
		roleEv("action.respond", 500, 150), // respond: 150
		roleEv("conscious.generate", 800, 50),
		{Kind: "seam.filter", Data: events.D{"role": "x", "completion_tokens": 999}}, // NOT llm.call → ignored
	}
	for _, ev := range evs {
		agg.Fold(ev)
	}
	bd := agg.Breakdown()

	// total decode = 800 + 150 + 50 = 1000 (the 999 from the non-llm.call event excluded; prompt excluded).
	if bd.TotalCompletion != 1000 {
		t.Fatalf("TotalCompletion = %d, want 1000 (completion ONLY; non-llm.call event excluded)", bd.TotalCompletion)
	}
	if bd.TotalCalls != 4 {
		t.Fatalf("TotalCalls = %d, want 4 (the seam.filter event is not an llm.call)", bd.TotalCalls)
	}
	// heaviest-first ordering: synthesize_program (800) before action.respond (150) before conscious.generate (50).
	if len(bd.Roles) != 3 {
		t.Fatalf("roles = %d, want 3 distinct roles", len(bd.Roles))
	}
	if bd.Roles[0].Role != "synthesize_program" || bd.Roles[0].Completion != 800 || bd.Roles[0].Calls != 2 {
		t.Errorf("top role = %+v, want synthesize_program completion=800 calls=2", bd.Roles[0])
	}
	if bd.Roles[1].Role != "action.respond" || bd.Roles[1].Completion != 150 {
		t.Errorf("second role = %+v, want action.respond completion=150", bd.Roles[1])
	}
	// share of synthesis = 800/1000 = 0.8 — the gate-1 read.
	if got := bd.ShareOf("synthesize_program"); got != 0.8 {
		t.Errorf("ShareOf(synthesize_program) = %v, want 0.8", got)
	}
	if got := bd.CompletionOf("synthesize_program"); got != 800 {
		t.Errorf("CompletionOf(synthesize_program) = %d, want 800", got)
	}
	// absent role → 0 share, 0 completion (not a panic).
	if got := bd.ShareOf("nonexistent"); got != 0 {
		t.Errorf("ShareOf(absent) = %v, want 0", got)
	}
}

// TestDecodeAggregatorReconcilesWithAddLLMCost is the cross-check: over the SAME event stream the
// per-role aggregator's total MUST equal what the flat addLLMCost fold sums — the per-role split is
// a partition of the existing total, never a different number. A divergence means the gate-1
// instrument is not measuring the same cost the campaign already gates on.
func TestDecodeAggregatorReconcilesWithAddLLMCost(t *testing.T) {
	evs := []events.Event{
		roleEv("synthesize_program", 1000, 600),
		roleEv("conscious.generate", 800, 120),
		roleEv("action.respond", 500, 40),
		roleEv("", 100, 7), // missing role label → buckets under "" but still counts toward the total
		llmEv(200, 33),     // also missing role → "" bucket
	}
	agg := NewDecodeAggregator()
	var calls, tokens, completion int
	for _, ev := range evs {
		agg.Fold(ev)
		addLLMCost(ev, &calls, &tokens, &completion)
	}
	bd := agg.Breakdown()
	if bd.TotalCompletion != completion {
		t.Errorf("aggregator TotalCompletion=%d must equal addLLMCost completion=%d (the per-role split is a partition)", bd.TotalCompletion, completion)
	}
	if bd.TotalCalls != calls {
		t.Errorf("aggregator TotalCalls=%d must equal addLLMCost calls=%d", bd.TotalCalls, calls)
	}
	// completion = 600+120+40+7+33 = 800; the two role-less events both land in the "" bucket.
	if bd.TotalCompletion != 800 {
		t.Errorf("TotalCompletion = %d, want 800", bd.TotalCompletion)
	}
}

// TestMergeBreakdowns pools per-task breakdowns into the suite-level aggregate (the gate-1 verdict
// reads the POOLED synthesis share across the heavy suite, not one task). Order-independent.
func TestMergeBreakdowns(t *testing.T) {
	a := NewDecodeAggregator()
	a.Fold(roleEv("synthesize_program", 0, 300))
	a.Fold(roleEv("action.respond", 0, 100))
	b := NewDecodeAggregator()
	b.Fold(roleEv("synthesize_program", 0, 500))
	b.Fold(roleEv("conscious.generate", 0, 200))

	merged := MergeBreakdowns([]DecodeBreakdown{a.Breakdown(), b.Breakdown()})
	// pooled synthesis = 300+500 = 800; pooled total = 800+100+200 = 1100; share = 800/1100.
	if merged.CompletionOf("synthesize_program") != 800 {
		t.Errorf("pooled synthesis completion = %d, want 800", merged.CompletionOf("synthesize_program"))
	}
	if merged.TotalCompletion != 1100 {
		t.Errorf("pooled total = %d, want 1100", merged.TotalCompletion)
	}
	wantShare := 800.0 / 1100.0
	if got := merged.ShareOf("synthesize_program"); got != wantShare {
		t.Errorf("pooled synthesis share = %v, want %v", got, wantShare)
	}
	// order-independent: merging the other way round is identical.
	merged2 := MergeBreakdowns([]DecodeBreakdown{b.Breakdown(), a.Breakdown()})
	if merged2.TotalCompletion != merged.TotalCompletion || merged2.CompletionOf("synthesize_program") != merged.CompletionOf("synthesize_program") {
		t.Errorf("MergeBreakdowns must be order-independent")
	}
}

// TestDecodeProbeOfflineWiring drives the EngineBencher.DecodeProbe path on the test double and
// asserts the per-row breakdown is wired (one row per task, the synth fields are populated from the
// breakdown). On the offline double real usage is 0 → an honest empty/zero breakdown (no fabricated
// cost), but the WIRING (per-row, reconciling fields) is exercised.
func TestDecodeProbeOfflineWiring(t *testing.T) {
	b := EngineBencher{MaxTicks: 20, NewEngine: testEngineFactory, Tasks: []HeldOutTask{
		{Goal: "design a rate limiter for an API gateway"},
		{Goal: "what is 12 times 7?"},
	}}
	rows := b.DecodeProbe("")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for i, r := range rows {
		// the synth fields must reconcile with the breakdown (the report reads these directly).
		if r.SynthShare != r.Breakdown.ShareOf("synthesize_program") {
			t.Errorf("row %d SynthShare=%v must equal Breakdown.ShareOf(synthesize_program)=%v", i, r.SynthShare, r.Breakdown.ShareOf("synthesize_program"))
		}
		if r.SynthCompletion != r.Breakdown.CompletionOf("synthesize_program") {
			t.Errorf("row %d SynthCompletion=%d must equal Breakdown.CompletionOf(synthesize_program)=%d", i, r.SynthCompletion, r.Breakdown.CompletionOf("synthesize_program"))
		}
		// the breakdown total must equal the sum of its rows (internal consistency).
		var sum int
		for _, rd := range r.Breakdown.Roles {
			sum += rd.Completion
		}
		if sum != r.Breakdown.TotalCompletion {
			t.Errorf("row %d breakdown total=%d must equal sum of role rows=%d", i, r.Breakdown.TotalCompletion, sum)
		}
	}
}
