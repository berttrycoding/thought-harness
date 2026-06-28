package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/bench/eval"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cost"
)

// TestBuildExperimentRow_Priced verifies a fully-priced (DeepSeek) run produces a
// row with the billed $, the cache-hit %, the I/O split, and the per-mechanism key
// metrics — and that it round-trips through JSON (the ledger is JSONL).
func TestBuildExperimentRow_Priced(t *testing.T) {
	calls := []cost.LLMCall{
		// 1000 prompt tokens, 200 of them a cache HIT; 300 output, 100 reasoning.
		{Role: "conscious.generate", Model: "deepseek-reasoner",
			PromptTokens: 1000, CompletionTokens: 300, CachedInputTokens: 200, CacheMissTokens: 800, ReasoningTokens: 100},
	}
	bd := cost.Compute(calls, cost.Default())

	results := []eval.MechResult{{
		Mechanism: benchtypes.MechGrounding, Tier: benchtypes.TierAtomic, K: 3, Items: 6,
		GateOffSupported:   true,
		Verdict:            eval.VerdictNeedsMoreN,
		Phase0:             eval.Phase0{K: 3, SigmaHarness: 0.27},
		HarnessMinusBare:   eval.Lift{Diff: benchtypes.Estimate{Point: 0.667, CILow: 0.0, CIHigh: 0.833, N: 6}},
		GateOnMinusGateOff: eval.Lift{Diff: benchtypes.Estimate{Point: 0.167, CILow: 0.0, CIHigh: 0.333, N: 6}},
		Isolation:          eval.IsolationRate{Passes: 4, Isolated: 4, Rate: 1.0},
	}}

	cfg := config{
		backend: "llm", llmModel: "deepseek-reasoner", llmURL: "https://api.deepseek.com/v1",
		temp: 0.2, concurrency: 4, tier: "a", replaysA: 3, replaysB: 2, seedBase: 1729,
		mechanisms: []benchtypes.Mechanism{benchtypes.MechGrounding}, rateModel: "deepseek-reasoner",
	}

	// One run with two calls on tick 1 and one on tick 2 → per-tick: tick1=2 calls,
	// tick2=1 call (mean 1.5, median 1.5 over the two populated ticks).
	perTick := cost.PerTickSpend([][]cost.LLMCall{{
		{Tick: 1, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
		{Tick: 1, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
		{Tick: 2, Model: "deepseek-reasoner", PromptTokens: 100, CompletionTokens: 50},
	}})

	ts := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	row := buildExperimentRow(cfg, bd, perTick, results, 468, 73*time.Minute, ts)

	if row.SchemaVersion != experimentSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", row.SchemaVersion, experimentSchemaVersion)
	}
	// per-tick rollup: 2 populated ticks, calls/tick mean 1.5, median 1.5.
	if row.PerTick.Ticks != 2 {
		t.Fatalf("per_tick.ticks = %d, want 2", row.PerTick.Ticks)
	}
	if math.Abs(row.PerTick.CallsPerTickMean-1.5) > 1e-9 || math.Abs(row.PerTick.CallsPerTickMedian-1.5) > 1e-9 {
		t.Fatalf("per_tick calls = mean %v median %v, want 1.5/1.5", row.PerTick.CallsPerTickMean, row.PerTick.CallsPerTickMedian)
	}
	if row.Timestamp != "2026-06-09T12:00:00Z" {
		t.Fatalf("timestamp = %q", row.Timestamp)
	}
	if row.RunID != "bench-20260609T120000Z" {
		t.Fatalf("run_id = %q", row.RunID)
	}
	if !row.Cost.Priced {
		t.Fatalf("expected priced run (deepseek-reasoner is in the card)")
	}
	if row.Cost.USD <= 0 {
		t.Fatalf("expected positive billed USD, got %v", row.Cost.USD)
	}
	// cache-hit % = 200/1000 = 20%.
	if math.Abs(row.Cost.CacheHitPct-20.0) > 1e-6 {
		t.Fatalf("cache_hit_pct = %v, want 20", row.Cost.CacheHitPct)
	}
	// token totals: in 1000, out 300, reasoning 100.
	if row.Tokens.TotalIn != 1000 || row.Tokens.Out != 300 || row.Tokens.Reasoning != 100 {
		t.Fatalf("tokens = %+v", row.Tokens)
	}
	if row.Tokens.CachedIn != 200 || row.Tokens.UncachedIn != 800 {
		t.Fatalf("token split = %+v", row.Tokens)
	}
	if row.Config.Concurrency != 4 || row.Config.Temp != 0.2 || row.Config.KTierA != 3 {
		t.Fatalf("config = %+v", row.Config)
	}
	if len(row.Mechanisms) != 1 {
		t.Fatalf("mechanisms len = %d", len(row.Mechanisms))
	}
	m := row.Mechanisms[0]
	if m.Mechanism != "grounding" || m.Verdict != "NEEDS-MORE-N" {
		t.Fatalf("mech = %+v", m)
	}
	if math.Abs(m.HarnessMinusBare.Point-0.667) > 1e-6 {
		t.Fatalf("harness_minus_bare.point = %v", m.HarnessMinusBare.Point)
	}
	if math.Abs(m.GateOnMinusGateOff.Point-0.167) > 1e-6 {
		t.Fatalf("gate_on_minus_gate_off.point = %v", m.GateOnMinusGateOff.Point)
	}
	if m.IsolationRate != 1.0 {
		t.Fatalf("isolation_rate = %v", m.IsolationRate)
	}

	// Must round-trip through JSON (the ledger is JSONL).
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back experimentRow
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.RunID != row.RunID || back.Cost.USD != row.Cost.USD {
		t.Fatalf("round-trip mismatch")
	}
}

// TestBuildExperimentRow_LocalUnpriced verifies a LOCAL (unpriced) run reports
// Priced=false and a non-nil ProjectedUSD under --rate-model — never a silent $0.
func TestBuildExperimentRow_LocalUnpriced(t *testing.T) {
	calls := []cost.LLMCall{
		{Role: "conscious.generate", Model: "local-gemma",
			PromptTokens: 500, CompletionTokens: 150, CachedInputTokens: 0, CacheMissTokens: 500, ReasoningTokens: 0},
	}
	bd := cost.Compute(calls, cost.Default())
	results := []eval.MechResult{}
	cfg := config{backend: "llm", llmModel: "local-gemma", temp: 0, concurrency: 1, tier: "a",
		replaysA: 2, replaysB: 2, seedBase: 1, rateModel: "deepseek-reasoner"}

	row := buildExperimentRow(cfg, bd, cost.PerTick{}, results, 1, time.Second, time.Now())
	if row.Cost.Priced {
		t.Fatalf("local model should be unpriced")
	}
	if row.Cost.ProjectedUSD == nil {
		t.Fatalf("expected a projected $ under --rate-model deepseek-reasoner")
	}
	if *row.Cost.ProjectedUSD <= 0 {
		t.Fatalf("projected $ = %v, want > 0", *row.Cost.ProjectedUSD)
	}
	if len(row.Cost.UnknownModels) != 1 || row.Cost.UnknownModels[0] != "local-gemma" {
		t.Fatalf("unknown_models = %v", row.Cost.UnknownModels)
	}
}

// TestBuildExperimentRow_NonFiniteScrubbed verifies NaN/Inf (an empty-arm isolation
// rate, a degenerate CI) is scrubbed to 0 so the row stays valid JSON.
func TestBuildExperimentRow_NonFiniteScrubbed(t *testing.T) {
	bd := cost.Compute(nil, cost.Default())
	results := []eval.MechResult{{
		Mechanism: benchtypes.MechSafety, Tier: benchtypes.TierAtomic, K: 2,
		Isolation:        eval.IsolationRate{Rate: math.NaN()},
		HarnessMinusBare: eval.Lift{Diff: benchtypes.Estimate{Point: math.Inf(1)}},
	}}
	cfg := config{backend: "test", tier: "a", replaysA: 2, replaysB: 2}
	row := buildExperimentRow(cfg, bd, cost.PerTick{}, results, 0, time.Second, time.Now())

	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal (non-finite must be scrubbed): %v", err)
	}
	if row.Mechanisms[0].IsolationRate != 0 || row.Mechanisms[0].HarnessMinusBare.Point != 0 {
		t.Fatalf("non-finite not scrubbed: %+v", row.Mechanisms[0])
	}
	// Round-trips cleanly.
	var back experimentRow
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
