package main

import "testing"

// TestRunOneControlMakesExpectedDecision drives the canonical decision-rich scenarios on the pure
// control-floor path (factory=nil, so even the llm/hybrid modes stay on the control floor) and asserts
// each makes its known-correct key decision (spec §8) — the same property the Python compare proved
// (4/4 control). It also pins the controlled-experiment shape: the control decision organ makes ZERO
// model calls.
func TestRunOneControlMakesExpectedDecision(t *testing.T) {
	for id := range expected {
		r := RunOne(id, "control", nil, 0)
		if !r.MadeExpected() {
			got := make([]string, 0, len(r.Decisions))
			for _, d := range r.Decisions {
				got = append(got, d.Decision)
			}
			t.Errorf("%s control: expected key decision %q not made; decisions=%v final=%s",
				id, expected[id], got, r.FinalState)
		}
		if r.LLMCalls != 0 {
			t.Errorf("%s control: llm_calls=%d, want 0 (the control organ never calls a model)", id, r.LLMCalls)
		}
		if r.Escalations != 0 {
			t.Errorf("%s control: escalations=%d, want 0", id, r.Escalations)
		}
		if r.NCompared() != 0 {
			t.Errorf("%s control: n_compared=%d, want 0 (no model consulted)", id, r.NCompared())
		}
	}
}

// TestRunOneNilFactoryDegradesToControl confirms the Python `if mode in (...) and backend_factory`
// guard: with a nil factory the llm/hybrid modes do NOT consult a model — no escalations, no model
// calls — so the comparison harness never requires a live model.
func TestRunOneNilFactoryDegradesToControl(t *testing.T) {
	for _, mode := range []string{"llm", "hybrid"} {
		r := RunOne("S3", mode, nil, 0)
		if r.LLMCalls != 0 || r.Escalations != 0 || r.NCompared() != 0 {
			t.Errorf("%s with nil factory should stay on the control floor: calls=%d escalations=%d compared=%d",
				mode, r.LLMCalls, r.Escalations, r.NCompared())
		}
		if r.FinalState == "" {
			t.Errorf("%s with nil factory should still run to a terminal lifecycle state", mode)
		}
	}
}

// TestRunComparisonShapeAndSummary runs the full grid on the control-floor path and checks the
// Summarize aggregation matches Python: one ModeSummary per mode, Total==len(scenarios), Agreement nil
// when no decision was compared (compared==0 -> Python None), and no disagreements without a model.
func TestRunComparisonShapeAndSummary(t *testing.T) {
	scs := []string{"S1", "S3", "S5", "S8"}
	res := RunComparison(scs, nil, nil, 0)

	for _, mode := range []string{"control", "llm", "hybrid"} {
		ms, ok := res.Summary[mode]
		if !ok {
			t.Fatalf("summary missing mode %q", mode)
		}
		if ms.Total != len(scs) {
			t.Errorf("%s: total=%d, want %d", mode, ms.Total, len(scs))
		}
		if ms.Compared != 0 {
			t.Errorf("%s: compared=%d, want 0 on the control-floor path", mode, ms.Compared)
		}
		if ms.Agreement != nil { // Python: agreement is None when compared==0
			t.Errorf("%s: agreement=%v, want nil (None) when nothing was compared", mode, *ms.Agreement)
		}
	}
	if len(res.Disagreements) != 0 {
		t.Errorf("disagreements=%d, want 0 without a model", len(res.Disagreements))
	}
	if len(res.Runs) != len(scs)*3 {
		t.Errorf("runs=%d, want %d (scenarios × 3 modes)", len(res.Runs), len(scs)*3)
	}
}

// TestSummarizeAgreementAndDisagreements pins the agreement/disagreement aggregation directly on a
// hand-built run set (no engine), so the *float64 None semantics and the escalated/agree filtering are
// covered independently of the heuristic stream. Mirrors the Python summarize() truthiness exactly.
func TestSummarizeAgreementAndDisagreements(t *testing.T) {
	// One run with three consulted decisions: two agree, one disagrees (the disagreement is escalated).
	r := Run{
		Scenario: "S3", Mode: "hybrid", FinalState: "DONE", NDecisions: 3, Escalations: 3,
		Decisions: []decisionRecord{
			{Decision: "THINK", HeuristicDecision: "THINK", LLMDecision: "THINK", LLMSet: true, Escalated: true, Agree: true},
			{Decision: "STOP", HeuristicDecision: "THINK", LLMDecision: "STOP", LLMSet: true, Escalated: true, Agree: false, Ambiguity: 0.8, Reason: "continue on current branch"},
			{Decision: "STOP", HeuristicDecision: "STOP", LLMDecision: "STOP", LLMSet: true, Escalated: true, Agree: true},
		},
	}
	if r.NCompared() != 3 {
		t.Fatalf("n_compared=%d, want 3", r.NCompared())
	}
	if r.NAgree() != 2 {
		t.Fatalf("n_agree=%d, want 2", r.NAgree())
	}
	if len(r.Disagreements()) != 1 {
		t.Fatalf("disagreements=%d, want 1", len(r.Disagreements()))
	}

	res := Summarize([]Run{r})
	ms := res.Summary["hybrid"]
	if ms.Agreement == nil {
		t.Fatal("agreement should be non-nil (3 compared)")
	}
	if want := 2.0 / 3.0; *ms.Agreement != want {
		t.Errorf("agreement=%v, want %v", *ms.Agreement, want)
	}
	if len(res.Disagreements) != 1 {
		t.Fatalf("flattened disagreements=%d, want 1", len(res.Disagreements))
	}
	d := res.Disagreements[0]
	if d.Scenario != "S3" || d.Mode != "hybrid" || d.HeuristicDecision != "THINK" || d.LLMDecision != "STOP" {
		t.Errorf("disagreement row = %+v, want S3/hybrid THINK->STOP", d)
	}
}
