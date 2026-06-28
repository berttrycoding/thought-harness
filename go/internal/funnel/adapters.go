package funnel

// adapters.go — the PER-REGISTRY adapters that let the ONE funnel sieve every scalable registry type
// (skills / operators / workflows / knowledge) through the SAME Stage-C → Tier-0 → Tier-1 → Tier-2
// pipeline (registry-scaling-strategy.md §4 "Stage C — CONSOLIDATE" + the §4 tier table). The funnel is
// registry-AGNOSTIC by construction — a Candidate is the same Candidate for every registry — so the
// only thing that differs per registry is the Stage-C BUCKETING KEY (§4: "by Move+Family for operators,
// by goal+body-shape for skills, by topology for workflows, by statement for knowledge"). These adapters
// own that one per-Kind rule so the bucketing is consistent wherever a candidate is built.
//
// THE LEAF PROPERTY IS PRESERVED. These adapters operate on PLAIN DATA (the per-Kind field values the
// caller already holds) — they do NOT import the live registries. The live-registry-bound converters
// (a cognition.Skill / cognition.OperatorSpec / knowledge.Knowledge → Candidate, which DO import the
// registries) stay in internal/scaling; THESE are the canonical bucketing rules those converters and any
// other feeder follow, so the funnel's own contract is self-describing and testable in the leaf.
//
// ADDITIVE. This file adds the per-registry sieve entry points; it changes nothing in the existing
// Stage-C / Tier-0 / Tier-1 code, and the Tier-2 stage stays opt-in (RegistrySieve.Tier2 is nil unless
// a caller wires a LiftBench).

import (
	"sort"
	"strings"
)

// RegistryKind is the registry a candidate batch targets. It selects the Stage-C bucketing rule and is
// stamped onto every Candidate the adapter builds (the funnel's Kind field). These are the five SCALABLE
// registry types of the scaling matrix (registry-scaling-strategy.md §1); the quality-locked registries
// (tools / paths) are not sieved by quantity and so have no adapter here.
type RegistryKind string

const (
	// KindOperator — the move vocabulary. Stage-C bucket = Move+Family (§4).
	KindOperator RegistryKind = "operator"
	// KindSkill — the goal-matched composites (the main 3-digit registry). Bucket = tier+body-topology (§4).
	KindSkill RegistryKind = "skill"
	// KindWorkflow — the named program shapes. Bucket = topology (§4).
	KindWorkflow RegistryKind = "workflow"
	// KindKnowledge — the discovered, gap-filling facts. Bucket = statement-kind (§4).
	KindKnowledge RegistryKind = "knowledge"
)

// String renders the kind.
func (k RegistryKind) String() string { return string(k) }

// clusterKeyFor is the per-registry Stage-C bucketing rule (registry-scaling-strategy.md §4). Each
// registry buckets on the field set whose collision means "near-duplicate suspect", so near-dup
// comparison stays cheap + local within a bucket. The parts are joined with a separator that cannot
// appear inside a normalized identifier, so distinct part tuples never collide.
func clusterKeyFor(kind RegistryKind, parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		cleaned = append(cleaned, strings.TrimSpace(p))
	}
	return string(kind) + ":" + strings.Join(cleaned, "/")
}

// OperatorClusterKey is the Stage-C bucket for an operator candidate: Move + Family (§4). Two operators
// with the same move and family are near-dup suspects (likely synonyms of one cognitive move).
func OperatorClusterKey(move, family string) string {
	return clusterKeyFor(KindOperator, move, family)
}

// SkillClusterKey is the Stage-C bucket for a skill candidate: tier + body TOPOLOGY (§4). Two skills with
// the same control-flow shape at the same tier are near-dup suspects (the same composite, re-voiced).
func SkillClusterKey(tier, bodyShape string) string {
	return clusterKeyFor(KindSkill, tier, bodyShape)
}

// WorkflowClusterKey is the Stage-C bucket for a workflow candidate: its TOPOLOGY (§4). Two named
// workflows with the same program topology are near-dup suspects (the same chain, renamed).
func WorkflowClusterKey(topology string) string {
	return clusterKeyFor(KindWorkflow, topology)
}

// KnowledgeClusterKey is the Stage-C bucket for a knowledge candidate: the statement KIND (fact /
// pattern / snippet, §4). The near-dup signal within a kind is then the statement-text similarity.
func KnowledgeClusterKey(statementKind string) string {
	return clusterKeyFor(KindKnowledge, statementKind)
}

// ClusterKeyFor dispatches to the per-registry bucketing rule by Kind, given the kind-appropriate parts
// (operator: move, family; skill: tier, body-shape; workflow: topology; knowledge: statement-kind). An
// unknown kind buckets on the joined parts under its own namespace (still deterministic, never a panic).
// This is the one entry point a feeder calls so it never has to know the per-registry rule by hand.
func ClusterKeyFor(kind RegistryKind, parts ...string) string {
	return clusterKeyFor(kind, parts...)
}

// ---------------------------------------------------------------------------
// The registry-agnostic batch sieve — Stage-C → Tier-0 → Tier-1 → (opt-in) Tier-2
// ---------------------------------------------------------------------------

// RegistrySieve runs the full funnel pipeline for one registry's candidate batch in one call, wiring the
// existing stages together so a per-registry caller does not re-sequence Admit → RetrievalIntegrity →
// RunTier2 by hand. Each field is optional EXCEPT the batch passed to Sieve:
//
//   - Theta is the near-dup cutoff for Stage-C/Tier-0 (0 ⇒ DefaultTheta).
//   - Similarity is the injected near-dup signal (nil ⇒ LexicalSimilarity, the offline default).
//   - Canonical/Baseline/Shadow drive Tier-1 retrieval integrity; without all three Tier-1 is SKIPPED
//     (reported as passed-with-note — the same convention as the campaign RealFunnel).
//   - Tier2 + BatchStateDir drive the opt-in Tier-2 lift; with Tier2 nil the lift stage does NOT run
//     (the default — Tier-2 is the expensive opt-in stage), and SieveResult.LiftRun is false.
//
// This is the funnel's per-registry front door: one Kind's batch in, the admitted survivors + the Tier-1
// verdict + (when wired) the Tier-2 lift decision out.
type RegistrySieve struct {
	// Kind is the registry the batch targets (stamped onto candidates that omit their own Kind, and
	// reported in the result). Optional.
	Kind RegistryKind
	// Theta is the Stage-C/Tier-0 near-dup cutoff. 0 ⇒ DefaultTheta.
	Theta float64
	// Similarity is the injected near-dup signal. nil ⇒ LexicalSimilarity (offline default).
	Similarity Similarity

	// Canonical / Baseline / Shadow drive Tier-1; all three required to run Tier-1, else it is skipped.
	Canonical []Query
	Baseline  Ranker
	Shadow    Ranker

	// Tier2 is the opt-in Tier-2 lift runner. nil ⇒ Tier-2 does not run (default). When set, Sieve runs
	// the BEFORE/AFTER lift over the staged batch and fills SieveResult.Lift.
	Tier2 *Tier2Runner
	// BatchStateDir is the staged with-batch registry dir the Tier-2 with-batch arm reads. Required only
	// when Tier2 is set.
	BatchStateDir string
}

// DefaultTheta is the near-dup cutoff used when a sieve does not set one (matches the campaign default).
const DefaultTheta = 0.9

// SieveResult is the per-registry sieve outcome across all wired stages.
type SieveResult struct {
	// Kind echoes the registry kind sieved.
	Kind RegistryKind
	// Admitted are the Tier-0 survivors (anti-filler passed + de-duplicated representatives).
	Admitted []Candidate
	// Rejected maps a rejected candidate ID → its reason (anti-filler fails / near-dup-of:<rep>).
	Rejected map[string]string
	// Tier1Ran reports whether Tier-1 retrieval integrity actually ran (all three inputs supplied).
	Tier1Ran bool
	// Tier1Pass is the Tier-1 verdict: true when no canonical rank-1 regressed (or Tier-1 was skipped).
	Tier1Pass bool
	// Tier1Regressions lists the canonical queries whose correct rank-1 the batch displaced (when ran).
	Tier1Regressions []Query
	// LiftRun reports whether the opt-in Tier-2 lift ran (Tier2 wired).
	LiftRun bool
	// Lift is the Tier-2 keep-or-revert-the-batch result (zero value when LiftRun is false).
	Lift LiftResult
}

// Sieve runs the registry's candidate batch through the full pipeline: Stage-C consolidation + Tier-0
// anti-filler (Admit), then Tier-1 retrieval integrity (when wired), then the opt-in Tier-2 lift (when
// wired). The stages short-circuit cheaply: an empty admitted set or a Tier-1 regression means the
// expensive Tier-2 lift is NOT run (the whole point of the funnel order). It returns the combined
// SieveResult, or an error only from the injected Tier-2 bench (the deterministic stages never error).
func (s RegistrySieve) Sieve(batch []Candidate) (SieveResult, error) {
	theta := s.Theta
	if theta <= 0 {
		theta = DefaultTheta
	}
	sim := s.Similarity
	if sim == nil {
		sim = LexicalSimilarity
	}

	admit := Admit(batch, sim, theta)
	res := SieveResult{
		Kind:      s.Kind,
		Admitted:  admit.Admitted,
		Rejected:  admit.Rejected,
		Tier1Pass: true, // default-pass (skipped Tier-1 is not a regression)
	}

	// Tier-1: only when all three inputs are present (the campaign RealFunnel convention).
	if len(s.Canonical) > 0 && s.Baseline != nil && s.Shadow != nil {
		ir := RetrievalIntegrity(s.Canonical, s.Baseline, s.Shadow)
		res.Tier1Ran = true
		res.Tier1Pass = ir.Pass
		res.Tier1Regressions = ir.Regressions
	}

	// Short-circuit before the expensive lift: nothing survived, or Tier-1 already regressed.
	if len(res.Admitted) == 0 || !res.Tier1Pass || s.Tier2 == nil {
		return res, nil
	}

	lift, err := s.Tier2.RunTier2(s.BatchStateDir, res.Tier1Pass)
	if err != nil {
		return res, err
	}
	res.LiftRun = true
	res.Lift = lift
	return res, nil
}

// AdmittedIDs returns the admitted candidate IDs in stable order (a small convenience for callers that
// stage by ID; the funnel itself orders Admitted by (ClusterKey, ID) already).
func (r SieveResult) AdmittedIDs() []string {
	ids := make([]string, 0, len(r.Admitted))
	for _, c := range r.Admitted {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	return ids
}
