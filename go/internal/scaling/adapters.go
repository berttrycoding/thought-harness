// Package scaling is the WF-C registry-scaling campaign layer (registry-scaling-strategy.md §5/§8):
// the bridge between the LIVE registries (operators / skills / knowledge) and the registry-agnostic
// validation funnel (internal/funnel). This file is the ADAPTERS — per-registry candidate conversion
// (entry → funnel.Candidate with the right ClusterKey) and the Tier-1 retrieval rankers (baseline =
// the live entries; shadow = live + batch MERGED, no registry mutation) — so funnel.RetrievalIntegrity
// can assert a batch causes no rank-1 regression before any model-in-the-loop lift run.
//
// The funnel stays a pure leaf; THIS package imports the registries. Everything here is deterministic
// and offline (the campaign's Tier-2 lift reuses internal/bench separately).
package scaling

import (
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
)

// ---------------------------------------------------------------------------
// candidate converters (entry → funnel.Candidate, per-registry ClusterKey)
// ---------------------------------------------------------------------------

// OperatorCandidate maps a proposed operator spec into the funnel. ClusterKey = Move/Family (the §4
// Stage-C bucketing for operators, via the canonical funnel.OperatorClusterKey rule); Text = name +
// intent (the dedup/near-dup surface). Provenance, links, and exercised are the FEEDER's responsibility
// — the anti-filler 3-test judges what the feeder claims, so a feeder must only set exercised=true after
// genuinely applying the operator once.
func OperatorCandidate(spec cognition.OperatorSpec, provenance string, links []string, exercised bool) funnel.Candidate {
	return funnel.Candidate{
		ID:         spec.Name,
		Kind:       string(funnel.KindOperator),
		ClusterKey: funnel.OperatorClusterKey(string(spec.Move), spec.Family),
		Text:       spec.Name + ": " + spec.Intent,
		Provenance: provenance,
		Links:      links,
		Exercised:  exercised,
	}
}

// SkillCandidate maps a proposed skill into the funnel. ClusterKey = tier + body TOPOLOGY (Shape(), via
// the canonical funnel.SkillClusterKey rule) — two skills with the same control-flow shape are near-dup
// suspects; Text = name + triggers + shape. The skill's converged Program is carried on the candidate
// (Program + Triggers) so a kept skill stages with its recallable structure (the efficiency lever).
func SkillCandidate(s cognition.Skill, provenance string, links []string, exercised bool) funnel.Candidate {
	return funnel.Candidate{
		ID:         s.Name,
		Kind:       string(funnel.KindSkill),
		ClusterKey: funnel.SkillClusterKey(s.Tier, s.Body.Shape()),
		Text:       s.Name + " triggers: " + strings.Join(s.Triggers, ", ") + " body: " + s.Body.Shape(),
		Provenance: provenance,
		Links:      links,
		Exercised:  exercised,
		Program:    s.Body.ToDict(),
		Triggers:   s.Triggers,
	}
}

// WorkflowCandidate maps a recurring synthesised Program (a candidate NAMED workflow) into the funnel.
// ClusterKey = the program TOPOLOGY (Shape(), via the canonical funnel.WorkflowClusterKey rule) — two
// programs with the same control-flow shape are near-dup suspects (the same chain, renamed); Text =
// name + goal + shape (the dedup/near-dup surface). The serialized program rides on the candidate so a
// kept workflow stages with its runnable structure. name is the proposed workflow's stable id (e.g. the
// minted Shape template name). Provenance/links/exercised are the feeder's responsibility (the anti-
// filler 3-test judges them) — a feeder sets exercised=true only after genuinely running the program once.
func WorkflowCandidate(name string, prog cognition.Program, provenance string, links []string, exercised bool) funnel.Candidate {
	return funnel.Candidate{
		ID:         name,
		Kind:       string(funnel.KindWorkflow),
		ClusterKey: funnel.WorkflowClusterKey(prog.Shape()),
		Text:       name + " goal: " + prog.Goal + " shape: " + prog.Shape(),
		Provenance: provenance,
		Links:      links,
		Exercised:  exercised,
		Program:    prog.ToDict(),
	}
}

// KnowledgeCandidate maps a knowledge statement into the funnel. ClusterKey = kind (fact/pattern/
// snippet); Text = the statement; provenance = the entry's own Source; links = its entities;
// exercised = Grounded (a grounded statement HAS been exercised against a real source — the
// never-fabricate stamp doubles as the anti-filler exercised bit).
func KnowledgeCandidate(k knowledge.Knowledge) funnel.Candidate {
	return funnel.Candidate{
		ID:         k.Statement,
		Kind:       string(funnel.KindKnowledge),
		ClusterKey: funnel.KnowledgeClusterKey(k.Kind),
		Text:       k.Statement,
		Provenance: k.Source,
		Links:      k.Entities,
		Exercised:  k.Grounded,
	}
}

// ---------------------------------------------------------------------------
// Tier-1 rankers (baseline + shadow-merged, no registry mutation)
// ---------------------------------------------------------------------------

// scored pairs an entry ID with its query score for deterministic best-first ranking.
type scored struct {
	id    string
	score float64
}

// rank orders best-first with an ID-ascending tiebreak (deterministic regardless of input order) and
// returns the IDs.
func rank(entries []scored) []string {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		return entries[i].id < entries[j].id
	})
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.id
	}
	return out
}

// lexScore is the shared deterministic query-relevance signal for ranking: content-word Jaccard
// between the query and the entry text (the same offline signal family the funnel's near-dup uses).
func lexScore(query, text string) float64 {
	return funnel.LexicalSimilarity(funnel.Candidate{Text: query}, funnel.Candidate{Text: text})
}

// SkillRanker ranks the LIVE skill registry for a goal query by the registry's OWN match signal
// (Skill.MatchScore — trigger overlap), so Tier-1 measures the real recall path, with a lexical
// fallback for skills whose triggers don't fire (they rank behind genuine matches). This is the
// BASELINE ranker for funnel.RetrievalIntegrity.
func SkillRanker(reg *cognition.SkillRegistry) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, name := range reg.Names() {
			s, ok := reg.Get(name)
			if !ok {
				continue
			}
			entries = append(entries, scored{id: name, score: skillScore(s, query)})
		}
		return rank(entries)
	}
}

// skillScore is the one scoring rule both the baseline and shadow skill rankers use: the registry's
// MatchScore when any trigger fires, else a small lexical-fallback (scaled below any genuine trigger
// match so a triggers-hit always outranks a text-overlap).
func skillScore(s cognition.Skill, query string) float64 {
	if m := s.MatchScore(query); m > 0 {
		return m
	}
	return 0.099 * lexScore(query, s.Name+" "+strings.Join(s.Triggers, " "))
}

// ShadowSkillRanker ranks the live registry PLUS a candidate batch (proposed skills) with the same
// scoring rule — the funnel's "shadow-add" without mutating the registry. Tier-1 then asserts no
// canonical query's rank-1 was displaced by the batch.
func ShadowSkillRanker(reg *cognition.SkillRegistry, batch []cognition.Skill) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, name := range reg.Names() {
			s, ok := reg.Get(name)
			if !ok {
				continue
			}
			entries = append(entries, scored{id: name, score: skillScore(s, query)})
		}
		for _, s := range batch {
			entries = append(entries, scored{id: s.Name, score: skillScore(s, query)})
		}
		return rank(entries)
	}
}

// KnowledgeRanker ranks the LIVE knowledge registry for a query via its real Recall path (relevance-
// gated, currently-valid entries), identified by statement. Entries Recall declines to surface are
// simply absent — exactly what the runtime would (not) retrieve.
func KnowledgeRanker(reg *knowledge.KnowledgeRegistry) funnel.Ranker {
	return func(query string) []string {
		items := reg.Recall(query, "", 16)
		out := make([]string, len(items))
		for i, k := range items {
			out[i] = k.Statement
		}
		return out
	}
}

// ShadowKnowledgeRanker builds a REAL throwaway registry — the live entries copied in plus the batch
// Recorded on top — and ranks via its genuine Recall path. This is true shadow-add fidelity: the batch
// competes inside the same relevance gate the runtime uses (merged ad-hoc scores would not be
// commensurable with Recall's ordering). The live registry is never mutated. Offline-deterministic
// (nil embedder ⇒ the lexical floor); Record's never-fabricate gate still applies to the batch, exactly
// as it would on a real admit.
func ShadowKnowledgeRanker(reg *knowledge.KnowledgeRegistry, batch []knowledge.Knowledge) funnel.Ranker {
	shadow := knowledge.NewKnowledgeRegistry(nil, nil)
	for _, k := range reg.AllForPersist() {
		shadow.Record(k)
	}
	for _, k := range batch {
		shadow.Record(k)
	}
	return KnowledgeRanker(shadow)
}

// OperatorRanker ranks the LIVE operator catalog for a query lexically over name+intent (operators
// have no runtime query-recall path — the synthesiser picks them by name — so Tier-1's confusion
// signal for operators is "does a canonical intent-query still surface the right operator first").
func OperatorRanker(reg *cognition.OperatorRegistry) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, name := range reg.Names() {
			spec, ok := reg.Get(name)
			if !ok {
				continue
			}
			entries = append(entries, scored{id: name, score: lexScore(query, name+" "+spec.Intent)})
		}
		return rank(entries)
	}
}

// ShadowOperatorRanker ranks the live catalog PLUS a proposed operator batch with the same lexical
// rule — the shadow-add for operator batches.
func ShadowOperatorRanker(reg *cognition.OperatorRegistry, batch []cognition.OperatorSpec) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, name := range reg.Names() {
			spec, ok := reg.Get(name)
			if !ok {
				continue
			}
			entries = append(entries, scored{id: name, score: lexScore(query, name+" "+spec.Intent)})
		}
		for _, spec := range batch {
			entries = append(entries, scored{id: spec.Name, score: lexScore(query, spec.Name+" "+spec.Intent)})
		}
		return rank(entries)
	}
}

// NamedProgram pairs a workflow's stable id with its converged Program — the workflow registry is a set
// of NAMED minted Shape templates (workflows are not yet a first-class live registry, so the existing
// set is passed in explicitly rather than read from a registry object). The lexical relevance surface is
// name + goal + shape, mirroring the operator path (workflows, like operators, are picked by the
// synthesiser by name, not via a runtime query-recall path, so Tier-1's confusion signal is "does a
// canonical workflow-intent query still surface the right workflow first").
type NamedProgram struct {
	Name    string
	Program cognition.Program
}

// workflowText is the shared lexical surface for ranking a named workflow.
func workflowText(np NamedProgram) string {
	return np.Name + " " + np.Program.Goal + " " + np.Program.Shape()
}

// WorkflowRanker ranks the existing named workflows for a query lexically over name+goal+shape — the
// BASELINE ranker for funnel.RetrievalIntegrity on a workflow batch.
func WorkflowRanker(existing []NamedProgram) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, np := range existing {
			entries = append(entries, scored{id: np.Name, score: lexScore(query, workflowText(np))})
		}
		return rank(entries)
	}
}

// ShadowWorkflowRanker ranks the existing named workflows PLUS a proposed workflow batch with the same
// lexical rule — the shadow-add for workflow batches (no registry mutation; the existing set is copied
// into the ranking, the batch appended).
func ShadowWorkflowRanker(existing, batch []NamedProgram) funnel.Ranker {
	return func(query string) []string {
		var entries []scored
		for _, np := range existing {
			entries = append(entries, scored{id: np.Name, score: lexScore(query, workflowText(np))})
		}
		for _, np := range batch {
			entries = append(entries, scored{id: np.Name, score: lexScore(query, workflowText(np))})
		}
		return rank(entries)
	}
}
