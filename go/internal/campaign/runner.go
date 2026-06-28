package campaign

// runner.go — the W5 campaign ORCHESTRATOR: one batch end-to-end, snapshot → generate → funnel →
// bench A/B → keep-rule → keep/revert via the ledger. The token-spending and IO-bound steps
// (generate on Claude, bench on the substrate, the persist ledger) are INJECTED as interfaces, so
// the orchestration logic is provable on fixtures with zero tokens; the real run wires the real
// implementations. This is the loop the supervision plan describes; Evaluate (keeprule.go) is its
// decision core.

import (
	"fmt"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/funnel"
)

// Generator produces a batch of candidate registry entries. The real impl is Claude-backed
// (cost-guarded); a dry-run uses a file source or a deterministic stub.
type Generator interface {
	Generate(batchSize int) ([]funnel.Candidate, error)
}

// Funnel screens a candidate batch: Tier-0 (anti-filler + dedup) then Tier-1 (retrieval integrity),
// returning the admitted survivors and whether Tier-1 held (no rank-1 regression). Wraps
// internal/funnel for the real run.
type Funnel interface {
	Screen(cands []funnel.Candidate) (admitted []funnel.Candidate, tier1Pass bool, err error)
}

// Bencher runs the held-out capability suite with a given registry state dir ("" = the baseline arm)
// and returns the arm's per-item pass/fail + token spend. The real impl runs the bench on the chosen
// substrate behind the W4 cost guard.
type Bencher interface {
	Bench(stateDir string) (ArmResult, error)
}

// BatchStore stages an admitted batch as a registry state dir the Bencher can seed, then commits the
// kept batch into the live registry or discards a reverted one. The real impl writes persist records.
type BatchStore interface {
	WriteBatch(batchID string, admitted []funnel.Candidate) (stateDir string, err error)
	Commit(stateDir string) error  // fold a KEPT batch into the live registry state
	Discard(stateDir string) error // drop a REVERTED batch
}

// Ledger snapshots the pre-batch baseline (the revert point) and records the keep/revert decision
// with its evidence. The real impl wraps the W1 persist ledger.
type Ledger interface {
	Snapshot(name string) error
	Record(decision, reason string) error
}

// Runner wires the injected steps into the batch loop.
type Runner struct {
	Gen    Generator
	Funnel Funnel
	Bench  Bencher
	Store  BatchStore
	Ledger Ledger
	Rule   KeepRule
}

// BaselineSnapshot is the name of the pre-batch revert point the runner takes before screening.
const BaselineSnapshot = "auto:campaign-baseline"

// BatchOutcome is the result of one batch: the verdict + what the funnel admitted + what the runner
// did about it (the committed/reverted action), so the campaign loop and the audit read one record.
type BatchOutcome struct {
	BatchID   string
	Generated int
	Admitted  int
	Tier1Pass bool
	Verdict   Verdict
	Action    string // "committed" | "reverted" | "staged (margin — human decides)"
	StateDir  string
}

// RunBatch runs ONE campaign batch end-to-end and returns its outcome. The sequence is the gate
// ladder of the supervision plan; a step error aborts the batch (the baseline is already snapshotted,
// so the live state is safe). MARGIN leaves the batch STAGED (not committed, not discarded) for the
// human's keep/revert call — never auto-applied.
func (r *Runner) RunBatch(batchID string, batchSize int) (BatchOutcome, error) {
	out := BatchOutcome{BatchID: batchID}
	if r.Rule.Alpha == 0 {
		r.Rule = DefaultKeepRule()
	}

	// 1. snapshot the baseline — the revert point for the whole batch.
	if err := r.Ledger.Snapshot(BaselineSnapshot); err != nil {
		return out, fmt.Errorf("snapshot baseline: %w", err)
	}

	// 2. generate candidates.
	cands, err := r.Gen.Generate(batchSize)
	if err != nil {
		return out, fmt.Errorf("generate: %w", err)
	}
	out.Generated = len(cands)

	// 3. funnel: Tier-0 admit + Tier-1 retrieval integrity.
	admitted, tier1Pass, err := r.Funnel.Screen(cands)
	if err != nil {
		return out, fmt.Errorf("funnel: %w", err)
	}
	out.Admitted = len(admitted)
	out.Tier1Pass = tier1Pass

	// An empty admitted set is an immediate revert (nothing survived the funnel — no batch to test).
	if len(admitted) == 0 {
		out.Verdict = Verdict{Decision: Revert, Tier1Regressed: !tier1Pass, Reason: "no candidate survived the funnel"}
		out.Action = "reverted"
		_ = r.Ledger.Record(out.Verdict.Decision.String(), out.Verdict.Reason)
		return out, nil
	}

	// 4. stage the admitted batch as a seedable registry state.
	stateDir, err := r.Store.WriteBatch(batchID, admitted)
	if err != nil {
		return out, fmt.Errorf("write batch: %w", err)
	}
	out.StateDir = stateDir

	// 5. bench A/B CONCURRENTLY: the baseline arm ("") and the with-batch arm (stateDir) are independent
	// — different state dirs, fresh engines/backends, separate per-arm budgets — so they run in parallel,
	// doubling inflight throughput on a slow metered substrate. Effective inflight = 2 × --concurrency.
	// (The arms are read-only over their own state; the result is identical to running them serially.)
	var (
		baseArm, batchArm ArmResult
		baseErr, batchErr error
		wg                sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); baseArm, baseErr = r.Bench.Bench("") }()
	go func() { defer wg.Done(); batchArm, batchErr = r.Bench.Bench(stateDir) }()
	wg.Wait()
	if baseErr != nil {
		return out, fmt.Errorf("bench baseline: %w", baseErr)
	}
	if batchErr != nil {
		return out, fmt.Errorf("bench with-batch: %w", batchErr)
	}

	// 6. evaluate the keep-rule.
	out.Verdict = Evaluate(baseArm, batchArm, tier1Pass, r.Rule)

	// 7. act on the verdict (and record it on the ledger as evidence).
	switch out.Verdict.Decision {
	case Keep:
		if err := r.Store.Commit(stateDir); err != nil {
			return out, fmt.Errorf("commit kept batch: %w", err)
		}
		out.Action = "committed"
	case Revert:
		_ = r.Store.Discard(stateDir)
		out.Action = "reverted"
	case Margin:
		out.Action = "staged (margin — human decides)"
	}
	if err := r.Ledger.Record(out.Verdict.Decision.String(), out.Verdict.Reason); err != nil {
		return out, fmt.Errorf("ledger record: %w", err)
	}
	return out, nil
}
