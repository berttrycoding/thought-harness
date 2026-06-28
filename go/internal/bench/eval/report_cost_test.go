package eval

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cost"
)

// baseHeader is a minimal report header for the cost-render tests (no mechanisms,
// no results — we only exercise the COST block).
func baseHeader(bd *cost.Breakdown, rateModel string) ReportHeader {
	return ReportHeader{
		Backend: "llm", Model: "test", Tiers: "A",
		Cost: bd, RateModel: rateModel,
	}
}

// TestReportPrintsDollarsIOSplitCacheHit is the DoD check: a priced run prints the
// total $, the I/O split, and the cache-hit % (plus the reasoning breakout).
func TestReportPrintsDollarsIOSplitCacheHit(t *testing.T) {
	card := cost.Default()
	bd := cost.Compute([]cost.LLMCall{
		{Role: "conscious.generate", Model: "deepseek-reasoner",
			PromptTokens: 1000, CompletionTokens: 400, CachedInputTokens: 250, CacheMissTokens: 750, ReasoningTokens: 120},
		{Role: "Controller.decide", Model: "deepseek-reasoner",
			PromptTokens: 500, CompletionTokens: 100, CachedInputTokens: 0, CacheMissTokens: 500, ReasoningTokens: 40},
	}, card)

	out := Render(baseHeader(&bd, ""), nil)

	for _, want := range []string{
		"COST",
		"total $          : $",     // a real dollar figure
		"I/O split        : input", // the input vs output split
		"cache-hit %",              // the cache-hit percentage line
		"reasoning tokens :",       // the reasoning breakout
		"per MODEL:",
		"per ROLE:",
		"deepseek-reasoner",  // the rate-card row that priced it
		"conscious.generate", // a per-role row
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cost report missing %q\n---\n%s", want, out)
		}
	}
	// input = 1500 tokens (250+750+0+500), output = 500 → I/O split 75%/25%.
	if !strings.Contains(out, "input 1500 (75.0%)") {
		t.Fatalf("I/O split line wrong:\n%s", out)
	}
	// cache-hit = 250/1500 = 16.7%.
	if !strings.Contains(out, "16.7%") {
		t.Fatalf("cache-hit %% wrong:\n%s", out)
	}
}

// TestReportPrintsPerTickSpend checks the per-tick spend headline line renders when
// the rollup is populated (the WF-E baseline-spend figure).
func TestReportPrintsPerTickSpend(t *testing.T) {
	card := cost.Default()
	bd := cost.Compute([]cost.LLMCall{
		{Role: "conscious.generate", Model: "deepseek-reasoner",
			PromptTokens: 1000, CompletionTokens: 400, CachedInputTokens: 0, CacheMissTokens: 1000, ReasoningTokens: 0},
	}, card)
	h := baseHeader(&bd, "")
	h.PerTick = cost.PerTickSpend([][]cost.LLMCall{{
		{Tick: 0, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
		{Tick: 0, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
		{Tick: 1, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
	}})

	out := Render(h, nil)
	for _, want := range []string{
		"per-tick spend   : calls/tick mean",
		"tokens/tick mean",
		"over 2 populated ticks",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing per-tick line fragment %q\n---\n%s", want, out)
		}
	}
}

// TestReportOmitsPerTickWhenNoTicks checks the per-tick line is omitted when no model
// call fired on any tick (the offline double).
func TestReportOmitsPerTickWhenNoTicks(t *testing.T) {
	card := cost.Default()
	bd := cost.Compute([]cost.LLMCall{
		{Role: "conscious.generate", Model: "deepseek-reasoner",
			PromptTokens: 100, CompletionTokens: 50, CachedInputTokens: 0, CacheMissTokens: 100, ReasoningTokens: 0},
	}, card)
	// Cost block renders (there is a call), but PerTick is left zero.
	out := Render(baseHeader(&bd, ""), nil)
	if strings.Contains(out, "per-tick spend") {
		t.Fatalf("per-tick line must be omitted when Ticks==0:\n%s", out)
	}
}

// TestReportLocalModelShowsTokensAndProjection is the local-vs-API delta: an
// unpriced (local) model shows "rates unknown" + the projected $ under --rate-model.
func TestReportLocalModelShowsTokensAndProjection(t *testing.T) {
	card := cost.Default()
	bd := cost.Compute([]cost.LLMCall{
		{Role: "conscious.generate", Model: "gemma-3-12b",
			PromptTokens: 2000, CompletionTokens: 1000, CachedInputTokens: -1, CacheMissTokens: -1, ReasoningTokens: -1},
	}, card)

	out := Render(baseHeader(&bd, "deepseek-reasoner"), nil)

	for _, want := range []string{
		"rates unknown for gemma-3-12b",
		"projected $      : $", // the local-vs-API projection
		"deepseek-reasoner",
		"rates-unknown", // the per-model row marks the unpriced model explicitly
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("local cost report missing %q\n---\n%s", want, out)
		}
	}
	// It must NOT print a silent $0.00 total for the unpriced run.
	if strings.Contains(out, "total $          : $0.00") {
		t.Fatalf("unpriced run printed a silent $0 total:\n%s", out)
	}
}

// TestReportOmitsCostWhenNoCalls checks the offline-double path: no llm.call events
// ⇒ no COST block (nothing to price).
func TestReportOmitsCostWhenNoCalls(t *testing.T) {
	bd := cost.Compute(nil, cost.Default())
	out := Render(baseHeader(&bd, ""), nil)
	if strings.Contains(out, "\nCOST\n") {
		t.Fatalf("COST block must be omitted on a zero-call run:\n%s", out)
	}
}
