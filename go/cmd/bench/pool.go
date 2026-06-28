package main

import (
	"sync"

	"github.com/berttrycoding/thought-harness/internal/bench/eval"
	"github.com/berttrycoding/thought-harness/internal/bench/ledger"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cost"
)

// ---------------------------------------------------------------------------
// Concurrency: a bounded worker pool over INDEPENDENT (mechanism, item, arm,
// replay) cells.
//
// Each cell builds its OWN fresh engine + backend + sandbox via the
// BackendFactory (see tiera.RunItem / tierb.RunScenario and
// runner.Runner.runEngine), so concurrent cells share NO mutable engine state.
// The only shared sinks are the append-only ledger (guarded by a mutex) and the
// per-(item,arm) cellStore / call tally (guarded by a mutex). The deterministic
// per-cell seed (seed-base + r) is computed when the job is built, identically to
// the serial path — so results are byte-identical regardless of --concurrency.
//
// The Phase-0 + lift reduction (eval.Summarize) runs AFTER every job completes,
// reading the fully-populated cellStore, so it is unaffected by execution order.
// ---------------------------------------------------------------------------

// job is one INDEPENDENT unit of work: run one (mechanism, item/scenario, arm,
// replay) cell, append its row to the ledger, and record its indicators into the
// owning mechanism×tier cellStore. run is a closure that performs the cell with
// the already-bound factory; collect is invoked under the shared collectMu so the
// ledger append, the cell add, and the call tally are serialized while the model
// work itself runs in parallel.
type job struct {
	// run executes the cell (the slow part: a fresh engine + backend) WITHOUT
	// touching any shared state, returning the ledger row + the cell indicators.
	run func() jobResult
	// collect deposits run's output into the shared sinks. It is invoked while the
	// pool holds collectMu, so it may freely touch the ledger, the cellStore, and
	// the running call tally.
	collect func(res jobResult)
}

// jobResult is what one cell produces: the ledger row to append, the (item, arm)
// pass/isolation indicators to record, the model-call count to tally, and the
// cell's per-call token usage (one per llm.call) the campaign-wide cost report
// aggregates per ROLE / per MODEL.
type jobResult struct {
	rec       ledger.Record
	itemID    string
	arm       benchtypes.Arm
	pass      bool
	isolation bool
	calls     int
	// llmCalls is the cell's per-call token usage off the trace (nil on the offline
	// double). Accumulated campaign-wide under collectMu and fed to the cost report.
	llmCalls []cost.LLMCall
}

// runPool runs every job through n worker goroutines (n>=1), with the slow
// run() executing in parallel and the collect() side-effects serialized under ONE
// mutex (so ledger.Append, cellStore.add, and the call tally are race-free —
// collect() is the single place shared state is touched). It returns the total
// model-call count across all jobs.
//
// n==1 degenerates to exactly the serial behaviour: one in-flight cell, appended
// and recorded in submission order. n>1 keeps the LM Studio continuous-batching
// slots full while preserving identical per-cell seeds and identical reductions
// (the reducer is order-independent, so a different completion order is fine).
//
// guard is the GPU/model GUARD (nil / disabled on the test double or --no-guard):
// every guard.everyCells COLLECTED cells, AND once before the first cell, it
// re-checks the loaded model id against the pinned expected id (a cheap GET
// /v1/models). On a CONFIRMED swap it sets aborted, the FEEDER stops dispatching
// further cells, the workers finish what is already in flight, and the pool
// returns early — so the partial (uncontaminated-prefix + the few in-flight
// cells) results are flushed by the caller, which then exits NON-ZERO. The guard
// check runs under collectMu (serialized with the appends) so the cell counter is
// consistent; the GET itself is tiny.
//
// It returns the total model-call count, the campaign-wide per-call token usage
// (every cell's llm.call usage records, concatenated under the collect lock — the
// substrate the cost report aggregates per ROLE / per MODEL and prices), AND the
// per-RUN call lists (one entry per cell, kept un-flattened) — the substrate
// cost.PerTickSpend buckets by tick PER RUN for the per-tick spend headline. A cell
// with no llm.* events (the offline double) contributes no run entry.
func runPool(jobs []job, n int, guard *modelGuard, budget *costGuard) (totalCalls int, llmCalls []cost.LLMCall, runCalls [][]cost.LLMCall) {
	if n < 1 {
		n = 1
	}
	total := len(jobs)

	var collectMu sync.Mutex // the ONLY shared-state lock: ledger + cells + tally + cost + guard.

	// PRE-RUN GUARD: verify the loaded model BEFORE the first cell runs (a swap that
	// happened before the campaign started is caught here, not after wasted work).
	// A confirmed swap dispatches no jobs and returns an empty (flushable) result.
	guard.Verify(0, total)
	if guard.Aborted() {
		return 0, nil, nil
	}

	in := make(chan job)
	var wg sync.WaitGroup
	var collected int
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for j := range in {
				res := j.run() // parallel: fresh engine/backend/sandbox, no shared state.
				collectMu.Lock()
				j.collect(res) // serialized: appendRow + cells.add.
				totalCalls += res.calls
				llmCalls = append(llmCalls, res.llmCalls...)
				// Keep this cell's calls as ONE run (un-flattened) so PerTickSpend buckets
				// by tick within the run — each cell is a fresh engine whose ticks restart.
				if len(res.llmCalls) > 0 {
					runCalls = append(runCalls, res.llmCalls)
				}
				collected++
				// GUARD CADENCE: re-check the loaded model every everyCells collected cells.
				// On a confirmed swap, Verify sets aborted (the feeder below stops on it).
				if guard.shouldCheck(collected) {
					guard.Verify(collected, total)
				}
				// BUDGET GUARD: fold this cell's token/call usage into the running total; once a
				// cap is crossed the feeder stops dispatching (W4 — metered-substrate ceiling).
				budget.add(res.llmCalls)
				collectMu.Unlock()
			}
		}()
	}
	// FEEDER: dispatch cells until the work is done OR the guard aborts. On abort we
	// stop sending NEW cells; the workers drain whatever is already in flight, then
	// the channel closes and Wait returns — flushing the uncontaminated prefix.
	for _, j := range jobs {
		if guard.Aborted() || budget.Aborted() {
			break
		}
		in <- j
	}
	close(in)
	wg.Wait()
	return totalCalls, llmCalls, runCalls
}

// tierAJobs builds one job per (item, arm, replay) cell for a Tier-A mechanism,
// each closing over the captured item/arm/seed and depositing into the shared
// store + cells. The seed is seed-base + r — IDENTICAL to the serial path.
func tierAJobs(
	cfg config, store *ledger.Store,
	factory runner.BackendFactory, mech benchtypes.Mechanism,
	items []benchtypes.TierAItem, arms []benchtypes.Arm, cells *cellStore, batchID string,
) []job {
	substrate := substrateLabel(cfg)
	var jobs []job
	for _, item := range items {
		for _, arm := range arms {
			for r := 0; r < cfg.replaysA; r++ {
				item, arm, r := item, arm, r // capture per-cell.
				seed := cfg.seedBase
				if !cfg.fixedSeed {
					seed += int64(r)
				}
				jobs = append(jobs, job{
					run: func() jobResult {
						progressf("bench: [%s tier-A] item=%s arm=%-9s replay=%d/%d\n",
							mech, item.ID, arm, r+1, cfg.replaysA)
						res := safeRunItem(item, arm, seed, factory)
						return jobResult{
							rec: ledger.Record{
								Tick:            r,
								BatchID:         batchID,
								Mechanism:       mech,
								Tier:            benchtypes.TierAtomic,
								Arm:             arm,
								ItemID:          item.ID,
								Seed:            seed,
								Substrate:       substrate,
								RawOutput:       res.RawOutput,
								OracleVerdict:   res.OracleVerdict,
								IsolationResult: res.IsolationResult,
								EventsPointer:   res.EventsPointer,
								CheckerVersion:  checkerVersion,
							},
							itemID:    item.ID,
							arm:       arm,
							pass:      res.Pass,
							isolation: res.IsolationResult,
							calls:     res.Cost.ModelCalls,
							llmCalls:  res.Calls,
						}
					},
					collect: func(res jobResult) {
						appendRow(store, res.rec)
						cells.add(res.itemID, res.arm, res.pass, res.isolation)
					},
				})
			}
		}
	}
	return jobs
}

// tierBJobs is the Tier-B analogue of tierAJobs: one job per (scenario, arm,
// replay) cell.
func tierBJobs(
	cfg config, store *ledger.Store,
	factory runner.BackendFactory, mech benchtypes.Mechanism,
	scns []benchtypes.TierBScenario, arms []benchtypes.Arm, cells *cellStore, batchID string,
) []job {
	substrate := substrateLabel(cfg)
	var jobs []job
	for _, scn := range scns {
		for _, arm := range arms {
			for r := 0; r < cfg.replaysB; r++ {
				scn, arm, r := scn, arm, r // capture per-cell.
				seed := cfg.seedBase
				if !cfg.fixedSeed {
					seed += int64(r)
				}
				jobs = append(jobs, job{
					run: func() jobResult {
						progressf("bench: [%s tier-B] scenario=%s arm=%-9s replay=%d/%d\n",
							mech, scn.ID, arm, r+1, cfg.replaysB)
						res := safeRunScenario(scn, arm, seed, factory)
						return jobResult{
							rec: ledger.Record{
								Tick:            r,
								BatchID:         batchID,
								Mechanism:       mech,
								Tier:            benchtypes.TierScenario,
								Arm:             arm,
								ItemID:          scn.ID,
								Seed:            seed,
								Substrate:       substrate,
								RawOutput:       res.RawOutput,
								OracleVerdict:   res.OracleVerdict,
								IsolationResult: res.IsolationResult,
								EventsPointer:   res.EventsPointer,
								CheckerVersion:  checkerVersion,
							},
							itemID:    scn.ID,
							arm:       arm,
							pass:      res.Pass,
							isolation: res.IsolationResult,
							calls:     res.Cost.ModelCalls,
							llmCalls:  res.Calls,
						}
					},
					collect: func(res jobResult) {
						appendRow(store, res.rec)
						cells.add(res.itemID, res.arm, res.pass, res.isolation)
					},
				})
			}
		}
	}
	return jobs
}

// mechWork bundles a mechanism×tier's collected cells with the closure that
// reduces them to a MechResult once every job has completed. The reduction is
// deferred until after the pool drains so it reads a fully-populated cellStore
// (order-independent: cellStore is keyed by item/arm), making it invariant to
// --concurrency.
type mechWork struct {
	cells  *cellStore
	reduce func(cells *cellStore) eval.MechResult
}
