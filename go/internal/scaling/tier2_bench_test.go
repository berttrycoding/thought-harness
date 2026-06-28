package scaling

// tier2_bench_test.go — proves the funnel's Tier-2 lift runner RUNS on a REAL engine (the test double,
// offline, zero tokens): the EngineLiftBench drives the held-out suite per task through a fresh engine,
// the funnel.Tier2Runner pairs the two arms and applies the keep-or-revert decision. This is the wiring
// gate — "the feature exists" is proven by it actually firing on a live engine, not just by unit tests
// of the decision math.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/campaign"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// liftEngineFactory builds a fresh test-double engine, optionally seeded from a state dir (the with-batch
// arm). Deterministic seed → reproducible across the paired arms.
func liftEngineFactory(stateDir string) (*engine.Engine, error) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 11
	if stateDir != "" {
		st, err := persist.NewJSONLStore(stateDir)
		if err != nil {
			return nil, err
		}
		cfg.Store = st
	}
	return engine.NewEngine(&cfg, backends.NewTest())
}

// the EngineLiftBench runs the suite on a live engine and returns a paired, completion-token-bearing
// ArmStats — offline, deterministic, zero tokens (the double emits no llm.* events).
func TestEngineLiftBenchRunsOnRealEngine(t *testing.T) {
	b := EngineLiftBench{
		MaxTicks:  20,
		NewEngine: liftEngineFactory,
		Tasks: []campaign.HeldOutTask{
			{Goal: "what is 12 times 7?", Expect: "84"},
			{Goal: "is this refactor safe to ship?"}, // grounded-success oracle
			{Goal: "what is 9 times 9?", Expect: "81"},
		},
	}
	arm, err := b.BenchArm("") // baseline arm — runs on a real engine
	if err != nil {
		t.Fatalf("BenchArm: %v", err)
	}
	if arm.Total() != 3 {
		t.Fatalf("per-item count = %d, want 3 (paired with the suite)", arm.Total())
	}
	if len(arm.CompletionPerItem) != 3 {
		t.Fatalf("CompletionPerItem must be paired per task, got %d", len(arm.CompletionPerItem))
	}
	// the offline double emits no llm.* events, so completion tokens are 0 — the honest offline answer.
	if arm.CompletionTokens() != 0 {
		t.Errorf("the offline double must report 0 completion tokens, got %d", arm.CompletionTokens())
	}
	// determinism: a second run of the SAME arm is byte-identical (seeded engine).
	arm2, err := b.BenchArm("")
	if err != nil {
		t.Fatalf("BenchArm (replay): %v", err)
	}
	for i := range arm.PerItem {
		if arm.PerItem[i] != arm2.PerItem[i] {
			t.Fatalf("non-deterministic per-item result at %d: %v vs %v", i, arm.PerItem[i], arm2.PerItem[i])
		}
	}
}

// the FULL Tier-2 path end-to-end on a real engine: EngineLiftBench → funnel.Tier2Runner → a verdict.
// With the test double both arms are byte-identical (no batch effect can show offline), so the verdict is
// the honest "no measurable gain → REVERT" — exactly right: a Tier-2 lift on the OFFLINE double cannot
// keep a batch (the real lift needs a real substrate, the W5-4 follow-up). This proves the path RUNS and
// is wired through the real decision core, not that it fabricates a keep.
func TestTier2RunnerEndToEndOnRealEngine(t *testing.T) {
	bench := EngineLiftBench{
		MaxTicks:  20,
		NewEngine: liftEngineFactory,
		Tasks: []campaign.HeldOutTask{
			{Goal: "what is 12 times 7?", Expect: "84"},
			{Goal: "what is 9 times 9?", Expect: "81"},
		},
	}
	runner := funnel.NewTier2Runner(bench)
	res, err := runner.RunTier2("", true) // "" with-batch dir == baseline → identical arms offline
	if err != nil {
		t.Fatalf("RunTier2: %v", err)
	}
	// identical arms offline → flat capability, flat (zero) completion cost → REVERT (no measurable gain).
	if res.Verdict.Decision != funnel.LiftRevert {
		t.Fatalf("identical offline arms must REVERT (no measurable gain), got %s (%s)", res.Verdict.Decision, res.Verdict.Reason)
	}
	if res.Baseline.Total() != 2 || res.WithBatch.Total() != 2 {
		t.Fatalf("both arms must run the full suite, got base=%d batch=%d", res.Baseline.Total(), res.WithBatch.Total())
	}
}

// the budget cap aborts the lift loudly (returns an error → the batch aborts), never a silent keep. A
// MaxCalls of 0 on the offline double would never trip (no calls), so we assert the no-factory error path
// here (the most deterministic abort) and rely on the funnel unit tests for the decision-side abort.
func TestEngineLiftBenchNoFactoryErrors(t *testing.T) {
	var b EngineLiftBench // no NewEngine
	if _, err := b.BenchArm(""); err == nil {
		t.Fatalf("a bench with no NewEngine factory must error, not nil-panic")
	}
}
