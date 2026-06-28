// judgeset.go is the campaign's Phase-0a deliverable (registry-scaling-strategy §5 PHASE 0a): the
// CANONICAL JUDGE-SETS — per-registry (query → expected-entry) quizzes that Tier-1 re-runs after
// shadow-adding every candidate batch to catch retrieval dilution. A quiz is only useful if the
// BASELINE already answers it at rank-1, so every generated query is VERIFIED against the live
// ranker at build time and dropped if it misses — the set is correct by construction, deterministic,
// and offline (entry-derived; a model-paraphrase ENRICHMENT pass is a later, GPU-gated extension
// that must pass the same verification).
package scaling

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
)

// BuildSkillJudgeSet derives one canonical query per skill from its OWN trigger vocabulary (the
// registry's real match signal), keeping only queries the baseline ranker answers at rank-1. Skills
// whose triggers collide so heavily that no derived query ranks them first simply contribute no quiz
// (better no quiz than a flaky one).
func BuildSkillJudgeSet(reg *cognition.SkillRegistry) []funnel.Query {
	base := SkillRanker(reg)
	var out []funnel.Query
	for _, name := range reg.Names() {
		s, ok := reg.Get(name)
		if !ok || len(s.Triggers) == 0 {
			continue
		}
		// the query is the skill's triggers composed into a goal-shaped line: hitting MORE of the
		// skill's own triggers maximises ITS MatchScore relative to sibling skills (their triggers
		// appear at most incidentally), which is exactly what makes the quiz answerable at baseline.
		q := "please " + strings.Join(s.Triggers, " ") + " for this goal"
		if got := base(q); len(got) > 0 && got[0] == name {
			out = append(out, funnel.Query{Text: q, ExpectedID: name})
		}
	}
	return out
}

// BuildOperatorJudgeSet derives one canonical query per operator from its intent text (the lexical
// surface the operator ranker scores), keeping only rank-1-verified quizzes.
func BuildOperatorJudgeSet(reg *cognition.OperatorRegistry) []funnel.Query {
	base := OperatorRanker(reg)
	var out []funnel.Query
	for _, name := range reg.Names() {
		spec, ok := reg.Get(name)
		if !ok || strings.TrimSpace(spec.Intent) == "" {
			continue
		}
		q := name + " " + spec.Intent
		if got := base(q); len(got) > 0 && got[0] == name {
			out = append(out, funnel.Query{Text: q, ExpectedID: name})
		}
	}
	return out
}

// BuildKnowledgeJudgeSet derives one canonical query per RECORDED knowledge statement (knowledge is
// discovered at runtime, never pre-stocked — so this set is built against whatever the live registry
// holds when the campaign runs). The query is the statement's own content words; only rank-1-verified
// quizzes are kept.
func BuildKnowledgeJudgeSet(reg *knowledge.KnowledgeRegistry) []funnel.Query {
	base := KnowledgeRanker(reg)
	var out []funnel.Query
	for _, k := range reg.AllForPersist() {
		st := strings.TrimSpace(k.Statement)
		if st == "" {
			continue
		}
		if got := base(st); len(got) > 0 && got[0] == st {
			out = append(out, funnel.Query{Text: st, ExpectedID: st})
		}
	}
	return out
}
