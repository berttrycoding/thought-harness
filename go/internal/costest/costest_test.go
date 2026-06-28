package costest

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cost"
)

// --- token estimator ---------------------------------------------------------

func TestEstimateHeuristic(t *testing.T) {
	m := HeuristicTokenModel()
	if m.Calibrated {
		t.Fatal("heuristic model must report uncalibrated")
	}
	// 40 chars / 4 = 10 tokens.
	if got := m.Estimate(strings.Repeat("x", 40)); got != 10 {
		t.Fatalf("Estimate(40 chars) = %d, want 10", got)
	}
	if got := m.Estimate(""); got != 0 {
		t.Fatalf("Estimate(empty) = %d, want 0", got)
	}
}

func TestCalibrateFromUsage(t *testing.T) {
	// Two calls with REAL usage: 100 prompt chars -> 50 tokens (2.0 chars/tok),
	// and a response 60 chars -> 30 tokens (2.0). The whole-log ratio is 2.0.
	calls := []Call{
		{System: strings.Repeat("a", 100), PromptTokens: 50, Response: strings.Repeat("b", 60), CompletionTokens: 30},
	}
	m := CalibrateCPT(calls)
	if !m.Calibrated {
		t.Fatal("model must report calibrated when a usage-bearing call exists")
	}
	if m.CharsPerToken < 1.99 || m.CharsPerToken > 2.01 {
		t.Fatalf("calibrated chars/token = %.3f, want ~2.0", m.CharsPerToken)
	}
	// A usage-LESS call's prompt (200 chars) should now estimate ~100 tokens (200/2.0).
	te := EstimateTokens(append(calls, Call{System: strings.Repeat("c", 200), PromptTokens: -1, CompletionTokens: -1}))
	// in = 50 (real) + 100 (estimated) = 150; the estimated call is counted once.
	if te.InTokens != 150 {
		t.Fatalf("InTokens = %d, want 150 (50 real + 100 calibrated)", te.InTokens)
	}
	if te.EstimatedCalls != 1 {
		t.Fatalf("EstimatedCalls = %d, want 1", te.EstimatedCalls)
	}
}

// --- prefix-reuse cache estimator -------------------------------------------

// Two identical prompts: the first is all MISS (nothing seen yet), the second is a near-total
// HIT (its whole prefix matches), so the workload cache-hit fraction is ~50%.
func TestPrefixIdenticalPrompts(t *testing.T) {
	// A prompt of 128 words so the matched prefix spans two whole 64-token blocks.
	prompt := strings.TrimSpace(strings.Repeat("word ", 128))
	calls := []Call{
		{System: prompt, PromptTokens: 128, CachedInputTokens: -1},
		{System: prompt, PromptTokens: 128, CachedInputTokens: -1},
	}
	// 1 word ~ 1 token here (real PromptTokens=128, words=128 → 1 tok/word). CachedInputTokens=-1
	// means the server reported no cache split → the prefix estimator fills it in.
	est := EstimateCacheHits(calls, HeuristicTokenModel(), 64)
	// Call 1: 0 hit. Call 2: matches all 128 words → 128 tokens, floored to blocks → 128.
	// total in = 256, hit = 128 → 50%.
	if est.HitTokens != 128 {
		t.Fatalf("HitTokens = %d, want 128 (second identical prompt fully cached)", est.HitTokens)
	}
	if frac := est.HitFraction(); frac < 0.49 || frac > 0.51 {
		t.Fatalf("HitFraction = %.3f, want ~0.50", frac)
	}
}

// A shared SYSTEM prefix with diverging USER tails: the shared system block is a hit on the
// second call, the divergent tail is a miss — a partial hit.
func TestPrefixSharedSystemDivergentUser(t *testing.T) {
	sys := strings.TrimSpace(strings.Repeat("sys ", 64)) // 64 shared leading words
	calls := []Call{
		{System: sys, User: "alpha beta gamma", PromptTokens: 67, CachedInputTokens: -1},
		{System: sys, User: "delta epsilon zeta theta", PromptTokens: 68, CachedInputTokens: -1},
	}
	est := EstimateCacheHits(calls, HeuristicTokenModel(), 64)
	// Call 2 shares the 64-word system prefix (1 full block) but diverges at the user tail.
	// Shared tokens ≈ 64 → one full 64-block hit; the rest miss.
	if est.HitTokens != 64 {
		t.Fatalf("HitTokens = %d, want 64 (one shared 64-token system block)", est.HitTokens)
	}
}

// No shared prefix at all → 0% cache hit (every call is cold).
func TestPrefixNoReuse(t *testing.T) {
	calls := []Call{
		{System: "aaa bbb ccc", PromptTokens: 30, CachedInputTokens: -1},
		{System: "xxx yyy zzz", PromptTokens: 30, CachedInputTokens: -1},
	}
	est := EstimateCacheHits(calls, HeuristicTokenModel(), 64)
	if est.HitTokens != 0 {
		t.Fatalf("HitTokens = %d, want 0 (no shared prefix)", est.HitTokens)
	}
	if est.HitFraction() != 0 {
		t.Fatalf("HitFraction = %.3f, want 0", est.HitFraction())
	}
}

// A server-reported cache hit is used DIRECTLY (not re-estimated).
func TestPrefixServerHitWins(t *testing.T) {
	calls := []Call{
		{System: "p q r", PromptTokens: 100, CachedInputTokens: 40},
	}
	est := EstimateCacheHits(calls, HeuristicTokenModel(), 64)
	if est.HitTokens != 40 {
		t.Fatalf("HitTokens = %d, want 40 (server-reported hit used directly)", est.HitTokens)
	}
	if est.ServerHitCalls != 1 {
		t.Fatalf("ServerHitCalls = %d, want 1", est.ServerHitCalls)
	}
}

// --- projection / pricing ----------------------------------------------------

func TestProjectPricesDeepSeek(t *testing.T) {
	prompt := strings.TrimSpace(strings.Repeat("word ", 128))
	calls := []Call{
		{System: prompt, PromptTokens: 128, CompletionTokens: 10, Response: "ten tokens out here please", CachedInputTokens: -1},
		{System: prompt, PromptTokens: 128, CompletionTokens: 10, Response: "ten tokens out here please", CachedInputTokens: -1},
	}
	p := Project(calls, cost.Default(), "deepseek-reasoner", 64)
	if !p.HasUSD {
		t.Fatal("deepseek-reasoner must be priced (in the default card)")
	}
	// in = 256, out = 20; hit = 128 (the 2nd prompt). With deepseek-reasoner rates
	// (miss 0.14, hit 0.0028, out 0.28 per Mtok):
	//   miss = 128 * 0.14/1e6 ; hit = 128 * 0.0028/1e6 ; out = 20 * 0.28/1e6.
	wantMiss := 128 * 0.14 / 1e6
	wantHit := 128 * 0.0028 / 1e6
	wantOut := 20 * 0.28 / 1e6
	want := wantMiss + wantHit + wantOut
	if diff := p.USD - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("USD = %.12f, want %.12f", p.USD, want)
	}
	// The cache must SAVE money vs the cold (no-cache) floor.
	if p.USDNoCacheFloor <= p.USD {
		t.Fatalf("no-cache floor %.12f must exceed cached %.12f", p.USDNoCacheFloor, p.USD)
	}
	if p.Savings() <= 0 {
		t.Fatalf("Savings() = %.4f, want > 0", p.Savings())
	}
}

// An unknown rate model is "rates unknown" (never a silent $0).
func TestProjectUnknownModel(t *testing.T) {
	calls := []Call{{System: "x y z", PromptTokens: 30}}
	p := Project(calls, cost.Default(), "no-such-model", 64)
	if p.HasUSD {
		t.Fatal("unknown model must NOT be priced (HasUSD=false)")
	}
	if p.USD != 0 {
		t.Fatalf("unknown model USD must be 0 (the sentinel), got %.6f", p.USD)
	}
}

// --- log reader --------------------------------------------------------------

func TestReadLogProjectsLLMCalls(t *testing.T) {
	jsonl := `{"tick":1,"kind":"tick","layer":"tick","summary":"","data":{}}
{"tick":2,"kind":"llm.call","layer":"llm","summary":"x","data":{"role":"conscious.generate","model":"m","system":"sys","user":"usr","raw":"out","prompt_tokens":12,"completion_tokens":3,"cached_input_tokens":-1}}
{"tick":3,"kind":"seam.filter","layer":"seam","summary":"","data":{}}
not-json-truncated-line
{"tick":4,"kind":"llm.call","layer":"llm","summary":"y","data":{"role":"action.respond","model":"m","system":"s2","user":"u2","raw":"r2"}}`
	calls, err := readLog(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("readLog: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (only llm.call lines, skipping the truncated line)", len(calls))
	}
	if calls[0].Role != "conscious.generate" || calls[0].PromptTokens != 12 || calls[0].CompletionTokens != 3 {
		t.Fatalf("call[0] mis-projected: %+v", calls[0])
	}
	// The second call reported no usage → -1 sentinels.
	if calls[1].PromptTokens != -1 || calls[1].CompletionTokens != -1 {
		t.Fatalf("call[1] should have absent usage (-1), got %+v", calls[1])
	}
	if calls[0].Concat() != "sys\nusr" {
		t.Fatalf("Concat = %q, want %q", calls[0].Concat(), "sys\nusr")
	}
}
