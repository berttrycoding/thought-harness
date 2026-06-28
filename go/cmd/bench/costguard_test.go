package main

// costguard_test.go — W4 metered-substrate budget guard: trips on a call cap or a token cap, is a
// no-op when uncapped.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cost"
)

func calls(n, promptEach, complEach int) []cost.LLMCall {
	out := make([]cost.LLMCall, n)
	for i := range out {
		out[i] = cost.LLMCall{PromptTokens: promptEach, CompletionTokens: complEach}
	}
	return out
}

func TestCostGuardCallCap(t *testing.T) {
	g := newCostGuard(3, 0) // cap at 3 calls
	g.add(calls(2, 10, 5))  // 2 calls
	if g.Aborted() {
		t.Fatal("2 calls under a cap of 3 must not abort")
	}
	g.add(calls(1, 10, 5)) // 3rd call reaches the cap
	if !g.Aborted() {
		t.Fatal("3 calls must trip the cap of 3")
	}
	if c, _ := g.spent(); c != 3 {
		t.Errorf("spent calls = %d, want 3", c)
	}
}

func TestCostGuardTokenCap(t *testing.T) {
	g := newCostGuard(0, 100) // cap at 100 tokens
	g.add(calls(2, 30, 10))   // 2*(30+10) = 80 tokens
	if g.Aborted() {
		t.Fatal("80 tokens under a cap of 100 must not abort")
	}
	g.add(calls(1, 30, 10)) // +40 -> 120 tokens, over the cap
	if !g.Aborted() {
		t.Fatal("120 tokens must trip the cap of 100")
	}
	if _, tok := g.spent(); tok != 120 {
		t.Errorf("spent tokens = %d, want 120", tok)
	}
	if g.Reason() == "" {
		t.Error("an aborted guard must carry a breach reason")
	}
}

func TestCostGuardUnlimitedIsNoOp(t *testing.T) {
	if g := newCostGuard(0, 0); g != nil {
		t.Fatal("both caps off must yield a nil guard (cheap no-op in the pool)")
	}
	// nil guard is safe to call.
	var g *costGuard
	g.add(calls(100, 999, 999))
	if g.Aborted() {
		t.Fatal("a nil guard never aborts")
	}
}
