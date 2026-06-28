package campaign

// adapters.go — the REAL implementations of the campaign Runner's injected interfaces (B1): the
// ledger over the W1 persist store, the batch store that stages candidates as a seedable registry
// state, a file-backed candidate generator, and the funnel over internal/funnel. The Bencher (the
// token-spending capability probe) lives in bencher.go. These are the last-mile wiring that turns
// the fixture-proven loop into a runnable campaign.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// --- Ledger over the W1 persist store -------------------------------------

// StoreLedger implements Ledger on a persist.Store: Snapshot captures the current live state as a
// named revert point; Record appends a self-change ledger entry. Substrate-tagged for hygiene.
type StoreLedger struct {
	Store     persist.Store
	Substrate string
	Tick      int // the seeded tick to stamp (campaign batches are wall-clock; 0 is fine)
}

func (l StoreLedger) Snapshot(name string) error {
	return l.Store.SaveSnapshot(persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{Name: name, Substrate: l.Substrate, CreatedTick: l.Tick},
		Data: *l.Store.Snapshot(),
	})
}

func (l StoreLedger) Record(decision, reason string) error {
	return l.Store.SaveLedgerEntry(persist.LedgerEntry{
		Tick: l.Tick, Scope: persist.LedgerScopeS1, SafetyMode: persist.SafetyModeSafe,
		Description: "campaign batch " + decision, Evidence: reason, GatePassed: "keep-rule",
		RevertHandle: BaselineSnapshot, Substrate: l.Substrate, SubmittedBy: "campaign",
	})
}

// --- BatchStore: stage candidates as a seedable registry state ------------

// JSONLBatchStore stages an admitted batch as its own JSONL state dir (the Bencher seeds it via
// THOUGHT_BENCH_REGISTRY_STATE), and on KEEP folds the batch's records into the LIVE store. Maps a
// funnel.Candidate to a persist record by its Kind (operator | knowledge | specialist).
type JSONLBatchStore struct {
	Live      persist.Store
	BaseDir   string // batch dirs are created under here
	Substrate string
}

func (s *JSONLBatchStore) meta() persist.Meta {
	return persist.Meta{Grounded: true, Status: persist.StatusActive, UseCount: 1, Substrate: s.Substrate}
}

// writeTo saves one candidate to a store as the record its Kind implies. Unsupported kinds error
// (never a silent drop). operator/knowledge/specialist are the supported registry targets.
func (s *JSONLBatchStore) writeTo(st persist.Store, c funnel.Candidate) error {
	switch strings.ToLower(c.Kind) {
	case "operator":
		return st.SaveOperator(persist.OpRecord{Meta: s.meta(), Name: c.ID, Family: "synthesized", Intent: c.Text, Move: "ground"})
	case "knowledge":
		return st.SaveKnowledge(persist.KnowledgeRecord{Meta: s.meta(), Statement: c.Text, Kind: "fact", Source: c.Provenance, Trust: 0.8})
	case "specialist":
		return st.SaveSpecialist(persist.SpecialistRecord{Meta: s.meta(), Domain: c.ID, GoalKey: c.ClusterKey, Answer: c.Text, Relevance: 0.9, Value: 0.7})
	case "skill":
		// A SKILL candidate (the research's efficiency/cognition lever): a converged reasoning Program
		// recalled instead of re-synthesised. Body is the serialized Program node dict; triggers default
		// to the cluster key when none are given. Previously writeTo ERRORED on "skill" though the funnel
		// lists it as a valid Kind — this closes that gap.
		triggers := c.Triggers
		if len(triggers) == 0 && c.ClusterKey != "" {
			triggers = []string{c.ClusterKey}
		}
		return st.SaveSkill(persist.SkillRecord{Meta: s.meta(), Name: c.ID, Tier: "composite", Triggers: triggers, Body: c.Program, Description: c.Provenance})
	default:
		return fmt.Errorf("campaign: unsupported candidate kind %q (id=%s)", c.Kind, c.ID)
	}
}

func (s *JSONLBatchStore) WriteBatch(batchID string, admitted []funnel.Candidate) (string, error) {
	dir := filepath.Join(s.BaseDir, "batch-"+batchID)
	st, err := persist.NewJSONLStore(dir)
	if err != nil {
		return "", err
	}
	// Seed the batch dir from the LIVE state first, so the staged dir is the FULL with-batch state
	// (live + batch) — the honest A/B arm (baseline = live, with-batch = live + the new candidates),
	// not batch-in-isolation. nil Live ⇒ batch-only (the offline dry-run case).
	if s.Live != nil {
		if base := s.Live.Snapshot(); base != nil {
			seedStoreFromSnapshot(st, base)
		}
	}
	for _, c := range admitted {
		if err := s.writeTo(st, c); err != nil {
			return "", err
		}
	}
	if err := st.Flush(); err != nil {
		return "", err
	}
	return dir, nil
}

// seedStoreFromSnapshot copies a snapshot's learned records into a store (the registries the bench
// seeds from). Idempotent (the store dedups by hash), so a later Commit re-adding them is harmless.
func seedStoreFromSnapshot(st persist.Store, snap *persist.Snapshot) {
	for _, r := range snap.Operators {
		_ = st.SaveOperator(r)
	}
	for _, r := range snap.Knowledge {
		_ = st.SaveKnowledge(r)
	}
	for _, r := range snap.Specialists {
		_ = st.SaveSpecialist(r)
	}
	for _, r := range snap.Skills {
		_ = st.SaveSkill(r)
	}
}

// Commit folds a kept batch's records into the LIVE store (re-reading the staged dir) and flushes.
func (s *JSONLBatchStore) Commit(stateDir string) error {
	st, err := persist.NewJSONLStore(stateDir)
	if err != nil {
		return err
	}
	snap, err := st.Load()
	if err != nil {
		return err
	}
	for _, r := range snap.Operators {
		_ = s.Live.SaveOperator(r)
	}
	for _, r := range snap.Knowledge {
		_ = s.Live.SaveKnowledge(r)
	}
	for _, r := range snap.Specialists {
		_ = s.Live.SaveSpecialist(r)
	}
	return s.Live.Flush()
}

// Discard drops a reverted batch's staged dir.
func (s *JSONLBatchStore) Discard(stateDir string) error { return os.RemoveAll(stateDir) }

// --- file Generator -------------------------------------------------------

// FileGenerator reads candidate entries from a JSON file (an array of funnel.Candidate) — the
// simplest real batch source (hand-written or a prior model dump), deterministic, no tokens. The
// Claude-backed generator is a later swap behind the same interface.
type FileGenerator struct{ Path string }

func (g FileGenerator) Generate(batchSize int) ([]funnel.Candidate, error) {
	data, err := os.ReadFile(g.Path)
	if err != nil {
		return nil, err
	}
	var cands []funnel.Candidate
	if err := json.Unmarshal(data, &cands); err != nil {
		return nil, fmt.Errorf("campaign: parse candidate file %s: %w", g.Path, err)
	}
	if batchSize > 0 && len(cands) > batchSize {
		cands = cands[:batchSize]
	}
	return cands, nil
}

// --- Funnel over internal/funnel ------------------------------------------

// RealFunnel screens candidates through Tier-0 (anti-filler + dedup, fully real) and Tier-1
// (retrieval integrity) when canonical quizzes + rankers are supplied. Without them Tier-1 passes
// with a note (the P0 measuring stick supplies them); Tier-0 alone is already the main quality gate.
type RealFunnel struct {
	DedupTheta float64        // near-dup cutoff (e.g. 0.9)
	Canonical  []funnel.Query // Tier-1 quizzes (optional)
	Baseline   funnel.Ranker  // baseline ranker (optional, needed for Tier-1)
	Shadow     funnel.Ranker  // shadow ranker over registry+batch (optional)
}

func (f RealFunnel) Screen(cands []funnel.Candidate) ([]funnel.Candidate, bool, error) {
	theta := f.DedupTheta
	if theta <= 0 {
		theta = 0.9
	}
	admit := funnel.Admit(cands, funnel.LexicalSimilarity, theta)
	tier1Pass := true
	if len(f.Canonical) > 0 && f.Baseline != nil && f.Shadow != nil {
		tier1Pass = funnel.RetrievalIntegrity(f.Canonical, f.Baseline, f.Shadow).Pass
	}
	return admit.Admitted, tier1Pass, nil
}
