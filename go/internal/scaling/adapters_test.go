package scaling

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
)

// TestOperatorCandidatesThroughTier0: real seed operator specs flow through the funnel's Tier-0 —
// a feeder-complete candidate admits; one missing provenance/links/exercised is rejected as filler;
// an exact near-dup of an admitted candidate merges. End-to-end adapter -> funnel.
func TestOperatorCandidatesThroughTier0(t *testing.T) {
	reg := cognition.NewOperatorRegistry()
	spec, ok := reg.Get("decompose")
	if !ok {
		t.Fatal("seed catalog should have 'decompose'")
	}
	good := OperatorCandidate(spec, "generator:cell-ground/transformative", []string{"skill:decompose-task"}, true)
	if good.ClusterKey == "/" || good.ClusterKey == "" {
		t.Fatalf("ClusterKey should carry Move/Family, got %q", good.ClusterKey)
	}
	filler := OperatorCandidate(cognition.OperatorSpec{Name: "noop-op", Family: "transformative", Intent: "do a thing"},
		"", nil, false) // no provenance, no links, never exercised
	dup := good
	dup.ID = "decompose-copy" // same text -> exact dup of good

	res := funnel.Admit([]funnel.Candidate{good, filler, dup}, funnel.LexicalSimilarity, 0.9)
	admitted := map[string]bool{}
	for _, c := range res.Admitted {
		admitted[c.ID] = true
	}
	if !admitted["decompose"] {
		t.Fatalf("the complete candidate should admit; rejected=%v", res.Rejected)
	}
	if admitted["noop-op"] {
		t.Fatal("the filler candidate (no provenance/links/exercised) must be rejected")
	}
	if admitted["decompose-copy"] {
		t.Fatal("the exact dup must merge into the representative")
	}
}

// TestSkillTier1CleanBatchPasses: canonical queries the live seed registry answers at rank-1 are NOT
// displaced by an unrelated batch — funnel.RetrievalIntegrity passes.
func TestSkillTier1CleanBatchPasses(t *testing.T) {
	reg := cognition.NewSkillRegistry(true)
	canonical := []funnel.Query{
		{Text: "break down this task into steps", ExpectedID: "decompose-task"},
		{Text: "diagnose why is the service failing", ExpectedID: "diagnose"},
	}
	base := SkillRanker(reg)
	for _, q := range canonical { // sanity: the baseline really answers these at rank-1
		if got := base(q.Text); len(got) == 0 || got[0] != q.ExpectedID {
			t.Fatalf("baseline sanity: %q rank-1 = %v, want %s", q.Text, got[:min(3, len(got))], q.ExpectedID)
		}
	}
	clean := []cognition.Skill{{Name: "deploy-service", Tier: "composite", Triggers: []string{"deploy", "rollout"}}}
	res := funnel.RetrievalIntegrity(canonical, base, ShadowSkillRanker(reg, clean))
	if !res.Pass || res.Checked != 2 {
		t.Fatalf("clean batch must pass Tier-1 (checked=%d, regressions=%v)", res.Checked, res.Regressions)
	}
}

// TestSkillTier1ConfusingBatchCaught: a batch skill whose triggers HIJACK a canonical query (higher
// MatchScore than the rightful skill) displaces rank-1 — the exact retrieval-dilution failure Tier-1
// exists to catch — and RetrievalIntegrity fails with the regression named.
func TestSkillTier1ConfusingBatchCaught(t *testing.T) {
	reg := cognition.NewSkillRegistry(true)
	canonical := []funnel.Query{{Text: "break down this task into steps", ExpectedID: "decompose-task"}}
	confusing := []cognition.Skill{{
		Name: "keyword-hog", Tier: "unit",
		// every trigger appears in the canonical query -> MatchScore 1.0 > decompose-task's 1/3.
		Triggers: []string{"break", "down", "this", "task"},
	}}
	res := funnel.RetrievalIntegrity(canonical, SkillRanker(reg), ShadowSkillRanker(reg, confusing))
	if res.Pass {
		t.Fatal("a trigger-hijacking batch must FAIL Tier-1 retrieval-integrity")
	}
	if len(res.Regressions) != 1 || res.Regressions[0].ExpectedID != "decompose-task" {
		t.Fatalf("expected the decompose-task regression named, got %v", res.Regressions)
	}
}

// TestKnowledgeTier1ShadowAdd: the knowledge shadow ranker is a REAL throwaway registry (live + batch
// through the genuine Recall path). A clean batch leaves canonical recall intact; a near-duplicate
// statement engineered to outrank the canonical one displaces rank-1 and is caught.
func TestKnowledgeTier1ShadowAdd(t *testing.T) {
	reg := knowledge.NewKnowledgeRegistry(nil, nil)
	live := knowledge.Knowledge{
		Statement: "MaxParWidth bounds the parallel fan-out of a synthesised program",
		Kind:      "fact", Entities: []string{"MaxParWidth"}, Source: "reality:read_file",
		Grounded: true, Trust: 0.9, ValidFrom: 1,
	}
	if !reg.Record(live) {
		t.Fatal("seeding the live registry failed")
	}
	canonical := []funnel.Query{{Text: "MaxParWidth parallel fan-out bound", ExpectedID: live.Statement}}
	base := KnowledgeRanker(reg)
	if got := base(canonical[0].Text); len(got) == 0 || got[0] != live.Statement {
		t.Fatalf("baseline sanity failed: %v", got)
	}

	clean := []knowledge.Knowledge{{
		Statement: "the regulator holds theta between its configured bounds",
		Kind:      "fact", Entities: []string{"regulator"}, Source: "reality:read_file",
		Grounded: true, Trust: 0.9, ValidFrom: 1,
	}}
	if res := funnel.RetrievalIntegrity(canonical, base, ShadowKnowledgeRanker(reg, clean)); !res.Pass {
		t.Fatalf("clean knowledge batch must pass, regressions=%v", res.Regressions)
	}

	confusing := []knowledge.Knowledge{{
		// near-verbatim echo of the QUERY: maximal lexical overlap -> outranks the live statement.
		Statement: "MaxParWidth parallel fan-out bound",
		Kind:      "fact", Entities: []string{"MaxParWidth"}, Source: "ingest:web",
		Grounded: true, Trust: 0.9, ValidFrom: 1,
	}}
	res := funnel.RetrievalIntegrity(canonical, base, ShadowKnowledgeRanker(reg, confusing))
	if res.Pass {
		t.Fatal("a query-echo near-dup must displace rank-1 and FAIL Tier-1")
	}
}

// TestOperatorTier1: the lexical operator ranker surfaces the right seed operator for an intent query
// at baseline, and a name/intent-squatting batch operator displaces it — caught by Tier-1.
func TestOperatorTier1(t *testing.T) {
	reg := cognition.NewOperatorRegistry()
	spec, _ := reg.Get("decompose")
	canonical := []funnel.Query{{Text: spec.Intent, ExpectedID: "decompose"}}
	base := OperatorRanker(reg)
	if got := base(canonical[0].Text); len(got) == 0 || got[0] != "decompose" {
		t.Fatalf("baseline sanity: %v", got[:min(3, len(got))])
	}
	// the squat: a verbatim intent copy under a name that sorts ahead — identical lexical score, so
	// the deterministic ID tiebreak hands it rank-1. Exactly the near-duplicate dilution Tier-1 (and
	// Tier-0 dedup before it) exists to stop.
	squatter := []cognition.OperatorSpec{{
		Name: "aaa-decompose-clone", Family: "transformative",
		Intent: spec.Intent,
	}}
	res := funnel.RetrievalIntegrity(canonical, base, ShadowOperatorRanker(reg, squatter))
	if res.Pass {
		t.Fatal("an intent-squatting operator must displace rank-1 and FAIL Tier-1")
	}
}

// TestWorkflowCandidateThroughTier0: a named workflow (a converged Program) flows through Tier-0 — a
// feeder-complete candidate admits with the topology as its bucket + the serialized program carried; a
// feeder-incomplete one is rejected as filler; a same-topology near-dup folds in.
func TestWorkflowCandidateThroughTier0(t *testing.T) {
	prog := cognition.Program{
		Goal: "diagnose then fix a failing service",
		Root: cognition.NewSeq(cognition.StepOp("decompose"), cognition.StepOp("diagnose"), cognition.StepOp("rank")),
	}
	good := WorkflowCandidate("diagnose-fix-flow", prog, "trace-mine:recurring-shape", []string{"skill:diagnose"}, true)
	if good.Kind != string(funnel.KindWorkflow) {
		t.Fatalf("workflow candidate Kind = %q, want workflow", good.Kind)
	}
	if good.ClusterKey != funnel.WorkflowClusterKey(prog.Shape()) {
		t.Fatalf("workflow ClusterKey must be the topology bucket, got %q", good.ClusterKey)
	}
	if good.Program == nil {
		t.Fatalf("workflow candidate must carry its serialized Program")
	}
	filler := WorkflowCandidate("noop-flow", prog, "", nil, false) // no provenance/links/exercised

	res := funnel.Admit([]funnel.Candidate{good, filler}, funnel.LexicalSimilarity, 0.9)
	admitted := map[string]bool{}
	for _, c := range res.Admitted {
		admitted[c.ID] = true
	}
	if !admitted["diagnose-fix-flow"] {
		t.Fatalf("the complete workflow candidate should admit; rejected=%v", res.Rejected)
	}
	if admitted["noop-flow"] {
		t.Fatalf("the feeder-incomplete workflow must be rejected as filler")
	}
}

// TestWorkflowTier1: the lexical workflow ranker surfaces the right named workflow for an intent query
// at baseline, and a name/goal-squatting batch workflow displaces it — caught by Tier-1.
func TestWorkflowTier1(t *testing.T) {
	flowProg := cognition.Program{Goal: "diagnose then fix a failing service", Root: cognition.NewSeq(cognition.StepOp("diagnose"), cognition.StepOp("rank"))}
	other := cognition.Program{Goal: "summarize a long document", Root: cognition.NewSeq(cognition.StepOp("compress"))}
	existing := []NamedProgram{{Name: "diagnose-fix-flow", Program: flowProg}, {Name: "summarize-flow", Program: other}}

	canonical := []funnel.Query{{Text: "diagnose then fix a failing service", ExpectedID: "diagnose-fix-flow"}}
	base := WorkflowRanker(existing)
	if got := base(canonical[0].Text); len(got) == 0 || got[0] != "diagnose-fix-flow" {
		t.Fatalf("baseline sanity: %v", got[:min(2, len(got))])
	}
	clean := []NamedProgram{{Name: "deploy-flow", Program: cognition.Program{Goal: "roll out a release to production", Root: cognition.NewSeq(cognition.StepOp("ground"))}}}
	if res := funnel.RetrievalIntegrity(canonical, base, ShadowWorkflowRanker(existing, clean)); !res.Pass {
		t.Fatalf("clean workflow batch must pass Tier-1, regressions=%v", res.Regressions)
	}
	// the squat: a verbatim goal copy under a name that sorts ahead -> identical lexical score, ID
	// tiebreak hands it rank-1, displacing the rightful workflow.
	squatter := []NamedProgram{{Name: "aaa-flow-clone", Program: cognition.Program{Goal: flowProg.Goal, Root: cognition.NewSeq(cognition.StepOp("diagnose"), cognition.StepOp("rank"))}}}
	res := funnel.RetrievalIntegrity(canonical, base, ShadowWorkflowRanker(existing, squatter))
	if res.Pass {
		t.Fatalf("a goal-squatting workflow must displace rank-1 and FAIL Tier-1")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
