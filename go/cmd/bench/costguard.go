package main

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/berttrycoding/thought-harness/internal/cost"
)

// costGuard is the frontier analogue of the model-swap GUARD (guard.go): a per-run BUDGET ceiling
// on model CALLS and TOKENS. A campaign on a metered substrate (claude / session / a remote API)
// must not silently overrun the budget the way a local GPU run can't swap models silently. Once the
// running total crosses a cap the guard ABORTS the run — the pool feeder stops dispatching, the
// in-flight cells drain, and execute() exits NON-ZERO with a loud banner (W4). A zero cap is
// unlimited; a test/offline run leaves both zero, so the guard is a complete no-op there.
type costGuard struct {
	maxCalls  int
	maxTokens int

	mu      sync.Mutex
	calls   int
	tokens  int
	aborted atomic.Bool
	reason  string
}

// newCostGuard builds a guard with the given caps (0 = unlimited). Returns nil when BOTH caps are
// off, so the pool can cheaply skip it.
func newCostGuard(maxCalls, maxTokens int) *costGuard {
	if maxCalls <= 0 && maxTokens <= 0 {
		return nil
	}
	return &costGuard{maxCalls: maxCalls, maxTokens: maxTokens}
}

// add folds a completed cell's per-call usage into the running total and trips the abort the moment
// a cap is crossed. Called under the pool's collect lock (serialized with the other tallies).
func (g *costGuard) add(calls []cost.LLMCall) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.aborted.Load() {
		return
	}
	for _, c := range calls {
		g.calls++
		if c.PromptTokens > 0 {
			g.tokens += c.PromptTokens
		}
		if c.CompletionTokens > 0 {
			g.tokens += c.CompletionTokens
		}
	}
	switch {
	case g.maxCalls > 0 && g.calls >= g.maxCalls:
		g.aborted.Store(true)
		g.reason = fmt.Sprintf("model calls %d reached the budget cap %d", g.calls, g.maxCalls)
	case g.maxTokens > 0 && g.tokens >= g.maxTokens:
		g.aborted.Store(true)
		g.reason = fmt.Sprintf("tokens %d reached the budget cap %d", g.tokens, g.maxTokens)
	}
}

// Aborted reports whether a budget cap has been crossed (nil-safe).
func (g *costGuard) Aborted() bool { return g != nil && g.aborted.Load() }

// Reason is the human-readable breach message (nil-safe; "" when not aborted).
func (g *costGuard) Reason() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reason
}

// spent returns the running (calls, tokens) total (nil-safe).
func (g *costGuard) spent() (calls, tokens int) {
	if g == nil {
		return 0, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls, g.tokens
}

// budgetBanner is the boxed notice prepended to the report (and printed to stderr) when the cost
// guard aborts a run — the on-disk mirror of the abort, so a truncated campaign is never mistaken
// for a complete one.
func budgetBanner(g *costGuard) string {
	calls, tokens := g.spent()
	return "" +
		"┌─────────────────────────────────────────────────────────────────────────┐\n" +
		"│ BUDGET EXCEEDED — run aborted before completion                            │\n" +
		"│ " + fmt.Sprintf("%-73s", g.Reason()) + " │\n" +
		"│ " + fmt.Sprintf("%-73s", fmt.Sprintf("spent: %d calls, %d tokens (this report covers the prefix only)", calls, tokens)) + " │\n" +
		"└─────────────────────────────────────────────────────────────────────────┘\n"
}
