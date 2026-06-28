// feeder.go is the campaign's GENERATOR-FEEDER segment (registry-scaling-strategy §3 feeder 1): a
// batch of PROPOSED entries flows from a JSONL file through STRUCTURAL VERIFY → EXERCISE → Tier-0
// (anti-filler + dedup) → Tier-1 (judge-set retrieval-integrity), producing an auditable report of
// survivors and per-candidate rejection reasons. Tier-2 (the lift run) consumes the survivors — that
// half is GPU-gated and lives with the campaign orchestration.
//
// Provenance is MANDATORY on every candidate (memory: project-claude-substrate-bootstrap-then-
// relocalize — a bootstrap-substrate entry must be tagged so it can be re-validated/relocalized
// later); the anti-filler gate enforces non-empty provenance structurally.
//
// EXERCISE is honest, not a checkbox: a proposed operator is genuinely RUN once — minted into a
// throwaway registry, wrapped in a one-step program, its phase instantiated and its sub-agent FIRED
// on the deterministic test backend. That proves the entry is a runnable cognitive move (the
// anti-filler "applied at least once", at plumbing truth); whether it improves capability is exactly
// what Tier-2's lift gate judges later on a real model.
package scaling

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ProposedOperator is one generator-feeder operator candidate (the JSONL line shape).
type ProposedOperator struct {
	Kind        string   `json:"kind"` // "operator"
	Name        string   `json:"name"`
	Family      string   `json:"family"`
	Intent      string   `json:"intent"`
	Move        string   `json:"move"`         // ground | lift | reframe | transcode | "" (assess/untagged)
	FuelNeeding bool     `json:"fuel_needing"` // a GROUND/REFRAME shape missing concrete content
	Links       []string `json:"links"`        // cross-links (anti-filler: not an island)
	Provenance  string   `json:"provenance"`   // MANDATORY substrate provenance (e.g. claude-code:bootstrap:<cell>)
}

// Batch is a loaded feeder batch (operator candidates now; other kinds parse forward-compatibly and
// are reported, not silently dropped).
type Batch struct {
	Operators []ProposedOperator
	Skipped   int // lines of a kind this loader does not yet admit (forward-compat, surfaced not silent)
}

// LoadBatch reads a feeder batch from JSONL. Unknown kinds are counted in Skipped (no silent drops).
func LoadBatch(r io.Reader) (Batch, error) {
	var b Batch
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return b, err
		}
		switch probe.Kind {
		case "operator":
			var p ProposedOperator
			if err := json.Unmarshal(line, &p); err != nil {
				return b, err
			}
			b.Operators = append(b.Operators, p)
		default:
			b.Skipped++
		}
	}
	return b, sc.Err()
}

// spec converts the proposal to the registry's spec shape.
func (p ProposedOperator) spec() cognition.OperatorSpec {
	return cognition.OperatorSpec{
		Name: p.Name, Family: p.Family, Intent: p.Intent,
		Synthesized: true, Move: cognition.Move(p.Move), FuelNeeding: p.FuelNeeding,
	}
}

// ExerciseOperator RUNS the proposed operator once on the deterministic test backend: mint into a
// THROWAWAY registry (never the live one), wrap in a one-step program, verify + schedule it, then
// instantiate the phase's sub-agent and FIRE it. True iff every stage genuinely ran and the fire
// produced a candidate. This is the anti-filler "exercised" bit earned, not declared.
func ExerciseOperator(p ProposedOperator) bool {
	reg := cognition.NewOperatorRegistry()
	if _, ok := reg.MintWithMove(p.Name, p.Family, p.Intent, cognition.Move(p.Move)); !ok {
		return false
	}
	prog := cognition.Program{Root: cognition.NewStep(p.Name, "general", ""), Goal: "exercise " + p.Name, Synthesized: true}
	if ok, _ := cognition.VerifyProgram(prog, reg); !ok {
		return false
	}
	wf := subconscious.FromProgram(&prog, reg, backends.NewTest(), nil, prog.Goal)
	phase := wf.Current()
	subAgents := wf.Instantiate(phase, nil, nil)
	if len(subAgents) == 0 {
		return false
	}
	rng := cpyrand.New(7)
	ctx := []types.Thought{{ID: 1, Text: prog.Goal, Source: types.GENERATED}}
	for _, sa := range subAgents {
		if c := sa.Fire(ctx, rng); c == nil {
			return false
		}
	}
	return true
}

// liveIntentDup reports the live operator whose INTENT the proposal near-duplicates (sim >= 0.8 on
// intent text alone — names and Move tags deliberately excluded so neither can mask the comparison),
// or "" when the proposal is genuinely novel.
func liveIntentDup(p ProposedOperator, live *cognition.OperatorRegistry) string {
	cand := funnel.Candidate{Text: p.Intent}
	for _, name := range live.Names() {
		spec, ok := live.Get(name)
		if !ok {
			continue
		}
		if funnel.LexicalSimilarity(cand, funnel.Candidate{Text: spec.Intent}) >= 0.8 {
			return name
		}
	}
	return ""
}

// OperatorReport is the feeder pipeline's auditable outcome for one batch.
type OperatorReport struct {
	Admitted []ProposedOperator // survivors of verify + exercise + Tier-0 + Tier-1
	Rejected map[string]string  // candidate name -> reason ("verify:...", "exercise:failed", "anti-filler:...", "near-dup-of:...", "tier1:dilutes")
	Tier1    funnel.IntegrityResult
}

// RunOperatorFeeder pushes a batch through the OFFLINE pipeline segment against the LIVE operator
// registry and its judge-set: STRUCTURAL VERIFY (the registry's own Verify rules) → EXERCISE (each
// candidate genuinely runs once) → Tier-0 funnel.Admit (anti-filler + exact/near dedup, which also
// rejects a candidate duplicating a LIVE entry's text) → Tier-1 RetrievalIntegrity (the admitted
// set must not displace any judge-set rank-1). On a Tier-1 failure the WHOLE batch is rejected
// (the funnel's batch-revert rule) with the diluting queries named. The live registry is never
// mutated — admission to the live registry is the campaign's COMMIT phase, after Tier-2.
func RunOperatorFeeder(batch Batch, live *cognition.OperatorRegistry, judge []funnel.Query) OperatorReport {
	rep := OperatorReport{Rejected: map[string]string{}}

	// 1. structural verify + exercise — per candidate, cheapest first.
	var staged []ProposedOperator
	var cands []funnel.Candidate
	for _, p := range batch.Operators {
		if strings.TrimSpace(p.Provenance) == "" {
			rep.Rejected[p.Name] = "anti-filler:not-traceable (empty provenance)"
			continue
		}
		if ok, why := live.Verify(p.Name, p.Family, p.Intent); !ok {
			rep.Rejected[p.Name] = "verify:" + why
			continue
		}
		if !ExerciseOperator(p) {
			rep.Rejected[p.Name] = "exercise:failed (the candidate could not actually run)"
			continue
		}
		// INTENT-level live-dup guard (bucket-INDEPENDENT): a candidate whose definition is a near-
		// verbatim copy of a live entry's intent is a duplicate move regardless of its name or its
		// (possibly omitted/mislabeled) Move tag. The Tier-0 funnel's near-dup check is bucket-local
		// by design (ClusterKey is a cheap pre-filter), which an adversarial/sloppy generator evades
		// by dropping the move tag — this guard closes that hole using intent text alone.
		if dup := liveIntentDup(p, live); dup != "" {
			rep.Rejected[p.Name] = "near-dup-of:" + dup + " (intent duplicates a live entry)"
			continue
		}
		staged = append(staged, p)
		cands = append(cands, OperatorCandidate(p.spec(), p.Provenance, p.Links, true))
	}

	// 2. Tier-0: anti-filler + dedup over the staged set PLUS the live catalog as context (a candidate
	// that duplicates a live entry's text merges into it and is rejected as a near-dup).
	liveCands := make([]funnel.Candidate, 0)
	for _, name := range live.Names() {
		if spec, ok := live.Get(name); ok {
			liveCands = append(liveCands, OperatorCandidate(spec, "live:catalog", []string{"catalog"}, true))
		}
	}
	admit := funnel.Admit(append(liveCands, cands...), funnel.LexicalSimilarity, 0.8)
	liveNames := map[string]bool{}
	for _, name := range live.Names() {
		liveNames[name] = true
	}
	admittedSet := map[string]bool{}
	for _, c := range admit.Admitted {
		if !liveNames[c.ID] {
			admittedSet[c.ID] = true
		}
	}
	var surviving []ProposedOperator
	var survivingSpecs []cognition.OperatorSpec
	for _, p := range staged {
		if admittedSet[p.Name] {
			surviving = append(surviving, p)
			survivingSpecs = append(survivingSpecs, p.spec())
		} else if r, ok := admit.Rejected[p.Name]; ok {
			rep.Rejected[p.Name] = r
		} else {
			rep.Rejected[p.Name] = "tier0:not-admitted"
		}
	}

	// 3. Tier-1: the surviving set must not displace any judge-set rank-1. Batch-level keep-or-revert.
	rep.Tier1 = funnel.RetrievalIntegrity(judge, OperatorRanker(live), ShadowOperatorRanker(live, survivingSpecs))
	if !rep.Tier1.Pass {
		for _, p := range surviving {
			rep.Rejected[p.Name] = "tier1:batch-reverted (judge-set rank-1 displaced)"
		}
		return rep
	}
	rep.Admitted = surviving
	return rep
}
