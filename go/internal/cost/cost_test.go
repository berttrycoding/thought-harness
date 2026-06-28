package cost

import (
	"bytes"
	"math"
	"os"
	"testing"
)

// TestEmbedMatchesConfigSeed guards against the embedded default_rates.json
// drifting from the canonical on-disk config/rates.json (the file users edit). The
// two MUST stay byte-identical — default_rates.json is a build-time copy of the
// repo-root config/rates.json (../../config/rates.json relative to this package).
func TestEmbedMatchesConfigSeed(t *testing.T) {
	onDisk, err := os.ReadFile("../../config/rates.json")
	if err != nil {
		t.Skipf("config/rates.json not found from package dir (run from module root): %v", err)
	}
	if !bytes.Equal(onDisk, embeddedRates) {
		t.Fatal("internal/cost/default_rates.json has drifted from config/rates.json — " +
			"re-copy config/rates.json over internal/cost/default_rates.json")
	}
}

// approx asserts two floats are within a small epsilon.
func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f, want %.12f", got, want)
	}
}

// TestEmbeddedCardParses validates the embedded seed rate card decodes and carries
// the seeded DeepSeek rows at the published per-Mtok numbers (the build-integrity
// check Default() relies on).
func TestEmbeddedCardParses(t *testing.T) {
	c := Default()
	for _, id := range []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-flash"} {
		r, ok := c.Lookup(id)
		if !ok {
			t.Fatalf("seed card missing %q", id)
		}
		approx(t, r.InUncached, 0.14)
		approx(t, r.InCached, 0.0028)
		approx(t, r.Out, 0.28)
	}
	pro, ok := c.Lookup("deepseek-v4-pro")
	if !ok {
		t.Fatal("seed card missing deepseek-v4-pro")
	}
	approx(t, pro.InUncached, 1.74)
	approx(t, pro.InCached, 0.0145)
	approx(t, pro.Out, 3.48)
}

// TestUnknownModelIsExplicit is the central contract: an unknown model is NOT
// silently $0 — Lookup says ok=false and a Compute over it leaves HasUSD=false.
func TestUnknownModelIsExplicit(t *testing.T) {
	c := Default()
	if _, ok := c.Lookup("gemma-2-9b-local"); ok {
		t.Fatal("local model should be unknown to the seed card")
	}
	bd := Compute([]LLMCall{
		{Role: "conscious.generate", Model: "gemma-2-9b-local",
			PromptTokens: 1000, CompletionTokens: 500, CachedInputTokens: -1, CacheMissTokens: -1, ReasoningTokens: -1},
	}, c)
	if bd.Total.HasUSD {
		t.Fatal("unknown model must leave HasUSD=false (rates unknown), never a silent $0")
	}
	if len(bd.Total.UnknownModels) != 1 || bd.Total.UnknownModels[0] != "gemma-2-9b-local" {
		t.Fatalf("unknown model not surfaced: %v", bd.Total.UnknownModels)
	}
	// Tokens are still tallied even when unpriced.
	if bd.Total.Tokens.TotalIn() != 1000 || bd.Total.Tokens.Out != 500 {
		t.Fatalf("tokens mis-tallied: %+v", bd.Total.Tokens)
	}
}

// TestDeepSeekCacheSplitPricing prices a DeepSeek call with an explicit hit/miss
// split and checks each token class is billed at its own rate.
func TestDeepSeekCacheSplitPricing(t *testing.T) {
	c := Default()
	// 1000 prompt = 200 cache-hit + 800 cache-miss; 400 completion (50 reasoning).
	bd := Compute([]LLMCall{
		{Role: "Controller.decide", Model: "deepseek-reasoner",
			PromptTokens: 1000, CompletionTokens: 400,
			CachedInputTokens: 200, CacheMissTokens: 800, ReasoningTokens: 50},
	}, c)
	tk := bd.Total.Tokens
	if tk.UncachedIn != 800 || tk.CachedIn != 200 || tk.Out != 400 || tk.Reasoning != 50 {
		t.Fatalf("split wrong: %+v", tk)
	}
	// 800*0.14/1e6 + 200*0.0028/1e6 + 400*0.28/1e6
	want := 800*0.14/1e6 + 200*0.0028/1e6 + 400*0.28/1e6
	if !bd.Total.HasUSD {
		t.Fatal("known model must be priced")
	}
	approx(t, bd.Total.USD, want)
	// cache-hit fraction = 200/1000.
	approx(t, tk.CacheHitFraction(), 0.2)
	// reasoning share = 50/400.
	approx(t, tk.ReasoningShare(), 0.125)
}

// TestOpenAICachedDerivesUncached checks the OpenAI shape (only a cached portion is
// reported, no explicit miss): uncached = prompt - cached.
func TestOpenAICachedDerivesUncached(t *testing.T) {
	c := Default()
	bd := Compute([]LLMCall{
		{Role: "conscious.generate", Model: "gpt-4o-mini",
			PromptTokens: 1000, CompletionTokens: 300,
			CachedInputTokens: 256, CacheMissTokens: -1, ReasoningTokens: -1},
	}, c)
	tk := bd.Total.Tokens
	if tk.CachedIn != 256 || tk.UncachedIn != 744 {
		t.Fatalf("OpenAI split wrong: cached=%d uncached=%d", tk.CachedIn, tk.UncachedIn)
	}
}

// TestIOSplit checks the input/output share computation over a multi-call run.
func TestIOSplit(t *testing.T) {
	tk := Tokens{UncachedIn: 600, CachedIn: 400, Out: 1000}
	// total = 2000, in = 1000.
	approx(t, tk.InputShare(), 0.5)
	approx(t, tk.OutputShare(), 0.5)
	approx(t, tk.CacheHitFraction(), 0.4)
}

// TestPerRoleAndPerModelAggregation checks a run with multiple roles + models rolls
// up correctly per role, per model, and into the run total.
func TestPerRoleAndPerModelAggregation(t *testing.T) {
	c := Default()
	calls := []LLMCall{
		{Role: "conscious.generate", Model: "deepseek-chat", PromptTokens: 100, CompletionTokens: 50, CachedInputTokens: 0, CacheMissTokens: 100, ReasoningTokens: -1},
		{Role: "conscious.generate", Model: "deepseek-chat", PromptTokens: 200, CompletionTokens: 80, CachedInputTokens: 0, CacheMissTokens: 200, ReasoningTokens: -1},
		{Role: "Controller.decide", Model: "deepseek-reasoner", PromptTokens: 300, CompletionTokens: 120, CachedInputTokens: 100, CacheMissTokens: 200, ReasoningTokens: 60},
	}
	bd := Compute(calls, c)

	// Per-model: deepseek-chat folds the two generate calls.
	chat := bd.ByModel["deepseek-chat"]
	if chat.Tokens.Calls != 2 || chat.Tokens.UncachedIn != 300 || chat.Tokens.Out != 130 {
		t.Fatalf("per-model deepseek-chat wrong: %+v", chat.Tokens)
	}
	// Per-role: conscious.generate spans only deepseek-chat here.
	gen := bd.ByRole["conscious.generate"]
	if gen.Tokens.Calls != 2 || gen.Tokens.Out != 130 {
		t.Fatalf("per-role generate wrong: %+v", gen.Tokens)
	}
	dec := bd.ByRole["Controller.decide"]
	if dec.Tokens.Reasoning != 60 {
		t.Fatalf("per-role decide reasoning wrong: %+v", dec.Tokens)
	}
	// Run total = all three calls.
	if bd.Total.Tokens.Calls != 3 {
		t.Fatalf("total calls wrong: %d", bd.Total.Tokens.Calls)
	}
	if !bd.Total.HasUSD {
		t.Fatal("all-known run must be fully priced")
	}
	// Total USD = per-model sum.
	want := bd.ByModel["deepseek-chat"].USD + bd.ByModel["deepseek-reasoner"].USD
	approx(t, bd.Total.USD, want)
}

// TestProjectUSD prices a LOCAL (unpriced) run's tokens under a chosen API model —
// the local-vs-API delta.
func TestProjectUSD(t *testing.T) {
	c := Default()
	bd := Compute([]LLMCall{
		{Role: "conscious.generate", Model: "gemma-2-9b-local",
			PromptTokens: 1000, CompletionTokens: 500, CachedInputTokens: -1, CacheMissTokens: -1, ReasoningTokens: -1},
	}, c)
	// Local: no real USD.
	if bd.Total.HasUSD {
		t.Fatal("local model should not be priced")
	}
	// Projected under deepseek-reasoner: all 1000 input is uncached (no cache info).
	usd, ok := bd.ProjectUSD("deepseek-reasoner")
	if !ok {
		t.Fatal("deepseek-reasoner must be projectable")
	}
	want := 1000*0.14/1e6 + 500*0.28/1e6
	approx(t, usd, want)
	if _, ok := bd.ProjectUSD("no-such-model"); ok {
		t.Fatal("unknown project model must return ok=false")
	}
}

// TestPartiallyUnpricedTotal checks a run mixing a known and an unknown model: the
// total is NOT fully priced (HasUSD=false) and the unknown model is surfaced.
func TestPartiallyUnpricedTotal(t *testing.T) {
	c := Default()
	bd := Compute([]LLMCall{
		{Role: "a", Model: "deepseek-chat", PromptTokens: 100, CompletionTokens: 50, CachedInputTokens: 0, CacheMissTokens: 100, ReasoningTokens: -1},
		{Role: "b", Model: "local-thing", PromptTokens: 100, CompletionTokens: 50, CachedInputTokens: -1, CacheMissTokens: -1, ReasoningTokens: -1},
	}, c)
	if bd.Total.HasUSD {
		t.Fatal("a run with an unpriced model must not be fully priced")
	}
	if len(bd.Total.UnknownModels) != 1 || bd.Total.UnknownModels[0] != "local-thing" {
		t.Fatalf("unknown model not surfaced in total: %v", bd.Total.UnknownModels)
	}
}

// TestPerTickSpendBucketsByTickWithinRun is the per-tick headline: calls/tokens are
// bucketed by tick WITHIN each run (tick numbers never collide across runs), and the
// mean/median are taken over every populated (run, tick) cell.
func TestPerTickSpendBucketsByTickWithinRun(t *testing.T) {
	// Run A: tick 0 has 2 calls (100 + 200 tokens = 300), tick 1 has 1 call (50).
	// Run B: tick 0 has 1 call (90) — same tick NUMBER as run A's tick 0, but a
	// DISTINCT cell (run boundary respected). Three populated cells: 300, 50, 90 tok
	// and 2, 1, 1 calls.
	// CacheMissTokens carries the uncached input explicitly (the realistic llm.call
	// shape: -1=absent, a number = the reported miss count). Total = miss + out.
	runA := []LLMCall{
		{Tick: 0, Model: "m", PromptTokens: 60, CompletionTokens: 40, CachedInputTokens: -1, CacheMissTokens: 60},   // 100 tok
		{Tick: 0, Model: "m", PromptTokens: 150, CompletionTokens: 50, CachedInputTokens: -1, CacheMissTokens: 150}, // 200 tok
		{Tick: 1, Model: "m", PromptTokens: 30, CompletionTokens: 20, CachedInputTokens: -1, CacheMissTokens: 30},   // 50 tok
	}
	runB := []LLMCall{
		{Tick: 0, Model: "m", PromptTokens: 50, CompletionTokens: 40, CachedInputTokens: -1, CacheMissTokens: 50}, // 90 tok
	}
	pt := PerTickSpend([][]LLMCall{runA, runB})

	if pt.Ticks != 3 {
		t.Fatalf("populated ticks = %d, want 3 (run boundaries must not collapse tick 0)", pt.Ticks)
	}
	// calls per cell = {2, 1, 1} → mean 4/3, median 1.
	approx(t, pt.MeanCalls, 4.0/3.0)
	approx(t, pt.MedianCalls, 1)
	// tokens per cell = {300, 50, 90} → mean 440/3, median 90.
	approx(t, pt.MeanTokens, 440.0/3.0)
	approx(t, pt.MedianTokens, 90)
}

// TestPerTickSpendEmptyIsZero checks no runs / no calls yields an all-zero rollup
// (the offline-double path) — never a divide-by-zero or a fabricated figure.
func TestPerTickSpendEmptyIsZero(t *testing.T) {
	if pt := PerTickSpend(nil); pt != (PerTick{}) {
		t.Fatalf("empty input must be the zero PerTick, got %+v", pt)
	}
	// A run with an empty call-list contributes no populated cell.
	if pt := PerTickSpend([][]LLMCall{{}, {}}); pt.Ticks != 0 {
		t.Fatalf("empty runs must yield 0 populated ticks, got %d", pt.Ticks)
	}
}

// TestPerTickSpendEvenMedian checks the even-count median is the mean of the two
// middle values (4 cells, one call each, token totals 10/20/30/40 → median 25).
func TestPerTickSpendEvenMedian(t *testing.T) {
	pt := PerTickSpend([][]LLMCall{
		{{Tick: 0, Model: "m", PromptTokens: 10, CachedInputTokens: -1, CacheMissTokens: 10}},
		{{Tick: 0, Model: "m", PromptTokens: 20, CachedInputTokens: -1, CacheMissTokens: 20}},
		{{Tick: 0, Model: "m", PromptTokens: 30, CachedInputTokens: -1, CacheMissTokens: 30}},
		{{Tick: 0, Model: "m", PromptTokens: 40, CachedInputTokens: -1, CacheMissTokens: 40}},
	})
	if pt.Ticks != 4 {
		t.Fatalf("ticks = %d, want 4", pt.Ticks)
	}
	approx(t, pt.MedianTokens, 25) // (20+30)/2
	approx(t, pt.MedianCalls, 1)
}

// TestLoadFileFallsBackToDefault checks an empty path returns the embedded card.
func TestLoadFileFallsBackToDefault(t *testing.T) {
	c, err := LoadFile("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup("deepseek-chat"); !ok {
		t.Fatal("empty path should yield the embedded seed card")
	}
}

// TestLoadFileMissingErrors checks a typo'd path fails loudly (not a silent
// fallback) — the rate card is load-bearing.
func TestLoadFileMissingErrors(t *testing.T) {
	if _, err := LoadFile("/nonexistent/rates.json"); err == nil {
		t.Fatal("missing rate-card path must error, not silently fall back")
	}
}
