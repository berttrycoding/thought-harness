package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// runtests_relevance_test.go — FIX #36: tool-targeting noise.
//
// The bug: a measure/validate sub-agent carries run_tests in scope, and floorToolCall's bare fallback
// fired `python -m pytest` for ANY such sub-agent — including a QA/lookup goal with an empty workspace,
// where pytest exits 5 (no tests collected), wasting a step and folding an error context that pollutes the
// answer. The fix gates the bare run_tests fallback on code/test relevance. These tests pin BOTH halves: a
// QA/lookup goal does NOT dispatch run_tests; a code goal still does.

// measureSpec returns the seed "measure" operator (ToolScope {run_tests}) — the effectful op whose bare
// fallback was the noise source.
func measureSpec(t *testing.T) cognition.OperatorSpec {
	t.Helper()
	cat := cognition.NewOperatorRegistry()
	spec, ok := cat.Get("measure")
	if !ok {
		t.Fatal("precondition: the seed catalog must carry the measure operator (run_tests scope)")
	}
	return spec
}

// TestRunTestsNotDispatchedOnQAGoal is the bug fix: a QA/lookup goal that staffs a measure sub-agent
// (run_tests scope) must NOT distil a run_tests call — the goal is not code/test relevant, so the bare
// pytest fallback is gated off (no wasted exit-5 step).
func TestRunTestsNotDispatchedOnQAGoal(t *testing.T) {
	spec := measureSpec(t)
	be := backends.NewTest()
	qaGoals := []string{
		"Were Scott Derrickson and Ed Wood the same nationality?",
		"What year did Marie Curie win her first Nobel Prize?",
		"Which river is longer, the Nile or the Amazon?",
	}
	for _, goal := range qaGoals {
		// domain "general" — exactly what the synthesiser resolves for a HotpotQA-style question.
		sa := NewSubAgent(spec, "general", goal, be, nil, "sa:test", spec.ToolScope, nil, nil)
		if call, ok := sa.floorToolCall(); ok && call.Name == "run_tests" {
			t.Fatalf("QA goal %q dispatched run_tests (pytest on an empty workspace) — the relevance gate must block it", goal)
		}
	}
}

// TestRunTestsStillDispatchedOnCodeGoal is the no-regression arm: a CODE goal that staffs a measure
// sub-agent STILL distils run_tests — the legitimate "did my code pass" path is unaffected.
func TestRunTestsStillDispatchedOnCodeGoal(t *testing.T) {
	spec := measureSpec(t)
	be := backends.NewTest()

	// (a) by DOMAIN: a code-domain sub-agent runs the suite.
	saDomain := NewSubAgent(spec, "code", "make the parser work", be, nil, "sa:test", spec.ToolScope, nil, nil)
	if call, ok := saDomain.floorToolCall(); !ok || call.Name != "run_tests" {
		t.Fatalf("code-domain measure sub-agent: floorToolCall = (%v, ok=%v), want run_tests", call, ok)
	}

	// (b) by GOAL TEXT signal: even a "general"-tagged sub-agent runs the suite when the goal text carries
	// a code/test signal (so a code goal that resolved to a broad domain is not starved).
	saSignal := NewSubAgent(spec, "general", "implement the function and run the tests", be, nil, "sa:test", spec.ToolScope, nil, nil)
	if call, ok := saSignal.floorToolCall(); !ok || call.Name != "run_tests" {
		t.Fatalf("code-signal goal: floorToolCall = (%v, ok=%v), want run_tests", call, ok)
	}
}

// TestCodeTestRelevant pins the predicate directly (the relevance pre-gate's contract).
func TestCodeTestRelevant(t *testing.T) {
	cases := []struct {
		goal, domain string
		want         bool
	}{
		{"Were they the same nationality?", "general", false},
		{"What year was the bridge built?", "general", false}, // "built" is not a code signal; "build the" is
		{"refactor the parser", "general", true},
		{"run the tests in the suite", "general", true},
		{"anything", "code", true}, // domain code is always relevant
		{"debug the endpoint", "general", true},
	}
	for _, c := range cases {
		if got := codeTestRelevant(c.goal, c.domain); got != c.want {
			t.Fatalf("codeTestRelevant(%q, %q) = %v, want %v", c.goal, c.domain, got, c.want)
		}
	}
}
