package scaling

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
)

// TestSkillJudgeSetVerifiedAndUseful: the seed skill registry yields a non-trivial judge-set, every
// quiz is rank-1-correct at baseline BY CONSTRUCTION, and the set is deterministic across builds.
func TestSkillJudgeSetVerifiedAndUseful(t *testing.T) {
	reg := cognition.NewSkillRegistry(true)
	set := BuildSkillJudgeSet(reg)
	if len(set) < 4 {
		t.Fatalf("the seed registry should yield a useful judge-set, got %d quizzes", len(set))
	}
	base := SkillRanker(reg)
	for _, q := range set {
		if got := base(q.Text); len(got) == 0 || got[0] != q.ExpectedID {
			t.Fatalf("judge-set quiz not rank-1 at baseline: %q -> want %s got %v", q.Text, q.ExpectedID, got[:min(3, len(got))])
		}
	}
	again := BuildSkillJudgeSet(reg)
	if len(again) != len(set) {
		t.Fatalf("judge-set not deterministic: %d vs %d", len(set), len(again))
	}
	for i := range set {
		if set[i] != again[i] {
			t.Fatalf("judge-set order/content differs at %d", i)
		}
	}
}

// TestSkillJudgeSetCatchesDilution: the generated judge-set actually does Tier-1's job — a
// near-duplicate CLONE of an existing skill (identical triggers, earlier-sorting name) ties its
// MatchScore and steals rank-1 on the tiebreak, failing RetrievalIntegrity. (A global trigger-hog
// does NOT dilute: MatchScore normalizes by the skill's own trigger count, so hoarding triggers
// scores LOW — a designed-in defense the first version of this test usefully demonstrated.)
func TestSkillJudgeSetCatchesDilution(t *testing.T) {
	reg := cognition.NewSkillRegistry(true)
	set := BuildSkillJudgeSet(reg)
	target, ok := reg.Get("decompose-task")
	if !ok {
		t.Fatal("seed registry should have decompose-task")
	}
	clone := []cognition.Skill{{Name: "aaa-decompose-clone", Tier: target.Tier, Triggers: target.Triggers}}
	res := funnel.RetrievalIntegrity(set, SkillRanker(reg), ShadowSkillRanker(reg, clone))
	if res.Pass {
		t.Fatal("a trigger-identical clone must fail Tier-1 against the generated judge-set")
	}
	// and a clean batch passes.
	clean := []cognition.Skill{{Name: "deploy-service", Tier: "unit", Triggers: []string{"deploy", "rollout"}}}
	if res := funnel.RetrievalIntegrity(set, SkillRanker(reg), ShadowSkillRanker(reg, clean)); !res.Pass {
		t.Fatalf("a clean batch must pass Tier-1, regressions=%v", res.Regressions)
	}
}

// TestOperatorJudgeSetVerified: the seed operator catalog yields a verified, deterministic judge-set
// covering most of the catalog.
func TestOperatorJudgeSetVerified(t *testing.T) {
	reg := cognition.NewOperatorRegistry()
	set := BuildOperatorJudgeSet(reg)
	if len(set) < 10 {
		t.Fatalf("the seed catalog (34 ops) should yield a broad judge-set, got %d", len(set))
	}
	base := OperatorRanker(reg)
	for _, q := range set {
		if got := base(q.Text); len(got) == 0 || got[0] != q.ExpectedID {
			t.Fatalf("operator quiz not rank-1: %q -> %s", q.Text, q.ExpectedID)
		}
	}
}

// TestKnowledgeJudgeSetFromRecorded: knowledge quizzes are built from whatever the live registry
// holds (discovered, not pre-stocked): empty registry -> empty set; recorded entries -> verified quizzes.
func TestKnowledgeJudgeSetFromRecorded(t *testing.T) {
	reg := knowledge.NewKnowledgeRegistry(nil, nil)
	if set := BuildKnowledgeJudgeSet(reg); len(set) != 0 {
		t.Fatalf("an empty registry must yield an empty judge-set, got %d", len(set))
	}
	reg.Record(knowledge.Knowledge{Statement: "MaxParWidth bounds the parallel fan-out of a synthesised program",
		Kind: "fact", Entities: []string{"MaxParWidth"}, Source: "reality:read_file", Grounded: true, Trust: 0.9, ValidFrom: 1})
	reg.Record(knowledge.Knowledge{Statement: "the regulator holds theta between its configured bounds",
		Kind: "fact", Entities: []string{"regulator"}, Source: "reality:read_file", Grounded: true, Trust: 0.9, ValidFrom: 1})
	set := BuildKnowledgeJudgeSet(reg)
	if len(set) != 2 {
		t.Fatalf("two recorded statements should yield two verified quizzes, got %d", len(set))
	}
	base := KnowledgeRanker(reg)
	for _, q := range set {
		if got := base(q.Text); len(got) == 0 || got[0] != q.ExpectedID {
			t.Fatalf("knowledge quiz not rank-1: %q", q.Text)
		}
	}
}
