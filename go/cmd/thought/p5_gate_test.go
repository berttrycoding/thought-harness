package main

import "testing"

// TestP5_1ControllerKeepOrRevertGate is the explicit P5.1 gate: the deterministic-agentic Controller
// must make the correct STRUCTURAL move on each of the decision-rich compare scenarios — the
// keep-or-revert bar of >=4/4. A blind LLM caps at 2/4 (it STOPs where it must BRANCH), which is why the
// redesign is a deterministic spine with agentic muscle, not full-LLM. The agentic muscle never
// overrides a structural move (TestHybridProtectsStructuralDecisions, internal/critic), and stability is
// held by the standing suite — together the redesign's gate.
func TestP5_1ControllerKeepOrRevertGate(t *testing.T) {
	core := []string{"S1", "S3", "S5", "S8"} // STOP / BRANCH / ACT / ACT (S8 acts after M2 deleted the fake hunch)
	correct := 0
	for _, id := range core {
		r := RunOne(id, "control", nil, 0)
		if r.MadeExpected() {
			correct++
		} else {
			t.Logf("%s: expected %q not made (final=%s)", id, expected[id], r.FinalState)
		}
	}
	if correct < 4 {
		t.Fatalf("P5.1 keep-or-revert gate FAILED: the deterministic controller got %d/4, need >=4", correct)
	}
}
