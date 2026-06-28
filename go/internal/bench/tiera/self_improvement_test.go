package tiera

// self_improvement_test.go — regression coverage for the self-improvement (convertibility) cell.
//
// The bug this guards against: self-improvement is a SECOND-occurrence property — a specialist/
// operator is minted from a repeated effortful path and the LATER occurrence resolves via the
// minted artifact. A single Tier-A episode mints at the trailing IDLE consolidate but can never
// REUSE the mint in its own trace, so a one-shot item can never witness mint→reuse (the §1.4
// isolation gate). Every harness pass was discarded as a mechanism-bypass and the cell read
// NO-SIGNAL. RunItem now drives a self-improvement item's goal through ONE engine
// SelfImprovementExposures times (the convertibility couplet), so the early exposures mint and the
// later ones reuse — the learning curve the isolation predicate reads.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/gen"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// TestSelfImprovementIsolationFires asserts every authored self-improvement Tier-A item, run
// through RunItem under the harness arm, witnesses MintThenReused (the mechanism is GENUINELY
// exercised — a mint followed by a reuse of that minted path in the same trace). On a one-shot
// runner this could never hold; it holds because RunItem recurs the goal SelfImprovementExposures
// times. Deterministic on the offline test double across a spread of seeds.
func TestSelfImprovementIsolationFires(t *testing.T) {
	items, err := gen.LoadBankA("../banks/pilot/self-improvement-tiera.jsonl")
	if err != nil {
		t.Fatalf("load self-improvement Tier-A bank: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("self-improvement Tier-A bank is empty")
	}
	seeds := []int64{1729, 1730, 1731, 7, 13}
	for _, it := range items {
		if it.Mechanism != benchtypes.MechSelfImprovement {
			t.Fatalf("item %q is not a self-improvement item (mechanism %q)", it.ID, it.Mechanism)
		}
		if it.TraceOracle == nil {
			t.Errorf("item %q has no trace_oracle — the isolation guard would never apply", it.ID)
		}
		for _, seed := range seeds {
			res := RunItem(it, benchtypes.ArmHarness, seed, runner.TestFactory)
			if !res.IsolationResult {
				t.Errorf("item %q seed %d: isolation did NOT fire (mint→reuse not witnessed): %s",
					it.ID, seed, res.EventsPointer)
			}
		}
	}
}

// TestSelfImprovementRecurrenceOnlyOnHarness pins that the 5x recurrence is harness-only: the
// bare arm answers ONCE (the honest base-model baseline — it has no cross-episode state to learn
// from), so its cost is a single Generate call's worth, while a harness arm takes many more.
func TestSelfImprovementRecurrenceOnlyOnHarness(t *testing.T) {
	items, err := gen.LoadBankA("../banks/pilot/self-improvement-tiera.jsonl")
	if err != nil {
		t.Fatalf("load self-improvement Tier-A bank: %v", err)
	}
	it := items[0]
	bare := RunItem(it, benchtypes.ArmBare, 1729, runner.TestFactory)
	harness := RunItem(it, benchtypes.ArmHarness, 1729, runner.TestFactory)
	if bare.Cost.ModelCalls != 1 {
		t.Errorf("bare arm should be a single Generate call, got ModelCalls=%d", bare.Cost.ModelCalls)
	}
	if harness.Cost.ModelCalls <= bare.Cost.ModelCalls {
		t.Errorf("harness 5x-recurrence should cost more model-calls than the single-shot bare arm (harness=%d bare=%d)",
			harness.Cost.ModelCalls, bare.Cost.ModelCalls)
	}
	// The bare arm must NOT be isolation-gated (it has no trace); its IsolationResult is vacuously true.
	if !bare.IsolationResult {
		t.Errorf("bare arm IsolationResult should be vacuously true (no trace to gate), got false: %s", bare.EventsPointer)
	}
}
