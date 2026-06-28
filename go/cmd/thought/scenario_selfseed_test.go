package main

import "testing"

// TestScenarioSelfSeededDoesNotPanic is the regression guard for the S16 CLI crash: a self-seeded
// scenario (empty Prompts — awake/arousal) must run through `thought scenario` without indexing an empty
// slice in the banner. Before the fix, cmdScenario printed sc.Prompts[0] unconditionally and panicked.
// Runs offline on the test backend (factory nil ⇒ control floor).
func TestScenarioSelfSeededDoesNotPanic(t *testing.T) {
	if code := cmdScenario([]string{"S16", "--backend", "test"}); code != 0 {
		t.Fatalf("cmdScenario S16 returned %d, want 0 (self-seeded scenario must run cleanly)", code)
	}
}
