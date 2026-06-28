// Command bench is the measuring-stick evaluation driver — the registry-scaling
// benchmark's pilot runner (docs/internal/notes/measuring-stick-spec.md §4, §5).
//
// It WIRES the measuring-stick component packages together (it implements no
// scoring math of its own): for each mechanism it loads the Tier-A and Tier-B
// pilot banks, runs each item under the [bare, harness, gate-off] arms over K
// replays, appends every per-replay result to the append-only keep/revert
// ledger, then reduces the replays into the per-mechanism PHASE-0 σ_noise, the
// LIFT contrasts, and a FEASIBILITY verdict, rendered to a plain-text report.
//
// Each (mechanism, item, arm, replay) cell is INDEPENDENT — it builds its own
// fresh engine + backend + sandbox via the BackendFactory — so the cells run
// through a bounded worker pool of --concurrency N (default 1 = serial, exactly
// the historical behaviour). Set --concurrency to the LLM server's
// continuous-batching slot count (e.g. PARALLEL=4 on LM Studio) to keep every
// slot busy. The per-cell seed (seed-base + r) and the post-run Phase-0 + lift
// reduction are unaffected by N, so the ledger rows and the report are IDENTICAL
// regardless of --concurrency — only the order cells execute in changes.
//
//	bench --bank internal/bench/banks/pilot --backend test|llm \
//	      --llm-url http://localhost:1234/v1 --llm-model auto \
//	      --mechanisms grounding,safety,... --tier A|B|both \
//	      --replays K --replays-b K --seed-base 1729 --temp 0.2 \
//	      --concurrency 4 \
//	      --out runs/pilot-ledger.jsonl --report runs/pilot-report.txt
//
// The pilot N is tiny (6 Tier-A items / 2 Tier-B scenarios per mechanism) so the
// CIs are WIDE and usually not significant — that is EXPECTED. The pilot
// establishes σ_noise + direction + plumbing, not power (spec §3, §4.7).
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/bench/eval"
	"github.com/berttrycoding/thought-harness/internal/bench/gen"
	"github.com/berttrycoding/thought-harness/internal/bench/ledger"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	"github.com/berttrycoding/thought-harness/internal/bench/tiera"
	"github.com/berttrycoding/thought-harness/internal/bench/tierb"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cost"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// allMechanisms is the default --mechanisms set: the six load-bearing mechanisms,
// in spec §1.1 order.
var allMechanisms = []benchtypes.Mechanism{
	benchtypes.MechGrounding,
	benchtypes.MechMultiStepRetrace,
	benchtypes.MechSelfImprovement,
	benchtypes.MechContinuousAutonomy,
	benchtypes.MechStability,
	benchtypes.MechSafety,
}

// tierAArms is the per-item arm set: bare (the reference), harness (= gate-on,
// full discipline), and gate-off (the single-toggle ablation). harness and
// gate-on are the same config (AllOn); we run harness as both the total-lift
// arm-1 AND the gate-on arm to avoid a redundant third engine run.
var tierAArms = []benchtypes.Arm{
	benchtypes.ArmBare,
	benchtypes.ArmHarness,
	benchtypes.ArmGateOff,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bench: "+err.Error())
		os.Exit(1)
	}
}

// config is the parsed CLI configuration.
type config struct {
	bank        string
	backend     string
	llmURL      string
	llmModel    string
	mechanisms  []benchtypes.Mechanism
	tier        string // "A" | "B" | "both"
	replaysA    int
	replaysB    int
	maxItems    int // cap Tier-A items to the first N (0 = all) — a fast smoke subset for inner-loop dev
	skipItems   int // skip the first M Tier-A items (0 = none) — chunked campaigns; applied before maxItems
	seedBase    int64
	temp        float64
	out         string
	report      string
	concurrency int    // number of cells in flight at once (1 = serial; default 1)
	fixedSeed   bool   // all replays share seed-base (true test-retest sigma_noise)
	rates       string // rate-card JSON path ("" = embedded config/rates.json seed)
	rateModel   string // --rate-model: project a LOCAL run's $ under this API model id
	noGuard     bool   // --no-guard: disable the mid-run model-swap GUARD (offline/test)
	maxCalls    int    // --max-calls: per-run model-CALL budget ceiling (0 = unlimited) — the metered-substrate guard (W4)
	maxTokens   int    // --max-tokens: per-run TOKEN budget ceiling (0 = unlimited) — the metered-substrate guard (W4)

	// expectedModel is the model id the GUARD pins the run to (resolved once at
	// start: explicit --llm-model, else the first /v1/models id for "auto"). Empty
	// on the test double (no loaded-model concept). Used for the lock file, the
	// guard comparison, and the report/experiments model id.
	expectedModel string
}

func run() error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	bank := fs.String("bank", gen.PilotBanksRoot, "banks root directory (one <mechanism>-tier{a,b}.jsonl per mechanism)")
	backend := fs.String("backend", "test", "backend: test (offline deterministic double) | llm (OpenAI-compatible local) | session (cc spool worker) | claude (Claude Code CLI bridge, subscription)")
	maxCalls := fs.Int("max-calls", 0, "per-run model-CALL budget ceiling (0 = unlimited). On a metered substrate (claude/session/api) the run ABORTS loudly + exits non-zero once crossed — the frontier analogue of the GPU model-swap guard.")
	maxTokens := fs.Int("max-tokens", 0, "per-run TOKEN budget ceiling (prompt+completion, 0 = unlimited). The run ABORTS loudly + exits non-zero once crossed.")
	llmURL := fs.String("llm-url", "http://localhost:1234/v1", "LLM base URL (--backend llm)")
	llmModel := fs.String("llm-model", "auto", "LLM model id, or 'auto' to detect the loaded model (--backend llm)")
	mechs := fs.String("mechanisms", "", "comma-separated mechanisms (default: all six)")
	tier := fs.String("tier", "both", "tier: A | B | both")
	replays := fs.Int("replays", 3, "Tier-A replays per (item, arm)")
	replaysB := fs.Int("replays-b", 2, "Tier-B replays per (scenario, arm)")
	maxItems := fs.Int("max-items", 0, "cap Tier-A items per mechanism to the FIRST N (0 = all) — a fast SMOKE subset for the inner dev loop, e.g. --max-items 3 --replays 1 --tier A")
	skipItems := fs.Int("skip-items", 0, "skip the FIRST M Tier-A items per mechanism (0 = none) — run a long campaign as validated chunks: --skip-items 4 --max-items 4 = items 5-8")
	seedBase := fs.Int64("seed-base", 1729, "base RNG seed (replay r uses seed-base + r)")
	temp := fs.Float64("temp", 0.2, "fixed sampling temperature across all arms")
	out := fs.String("out", "runs/pilot-ledger.jsonl", "append-only ledger output path")
	report := fs.String("report", "runs/pilot-report.txt", "plain-text feasibility report output path")
	concurrency := fs.Int("concurrency", 1, "cells (item×arm×replay) to run in flight at once; 1 = serial (default). "+
		"Set to the LLM server's continuous-batching slot count (e.g. 4) to keep all slots busy. "+
		"Results are IDENTICAL regardless of this value — only execution order changes.")
	fixedSeed := fs.Bool("fixed-seed", false, "all replays share seed-base (true test-retest: at --temp 0 the harness is deterministic, so sigma_noise measures verifier noise, not harness seed-variance)")
	rates := fs.String("rates", "", "rate-card JSON path (model-id -> {in_uncached,in_cached,out} USD/Mtok); empty = the embedded config/rates.json seed")
	rateModel := fs.String("rate-model", "", "model id to PROJECT a local (unpriced) run's $ against (e.g. deepseek-reasoner) — shows the local-vs-API delta")
	noGuard := fs.Bool("no-guard", false, "disable the mid-run model-swap GUARD (--backend llm only). The guard pins the loaded model at start and ABORTS if it changes mid-run (a swap silently contaminates the benchmark). Use only offline/test.")
	legibleReportPath := fs.String("legible-report", "", "READ-ONLY telemetry rollup mode: read a JSONL event log (the `thought ... --log FILE.jsonl` shape) and print the legible-generation rollup (fast-path hit rate, novel-tag histogram, per-seam parity). Runs NO campaign.")
	legibleTopN := fs.Int("legible-top", 0, "with --legible-report: cap the novel-tag histogram at the top N rows (0 = all)")
	synthFidelityBank := fs.String("synth-fidelity", "", "A5 agent-synthesis-fidelity mode (Track A, registry-target-spec §1/§3): drive the REAL synthesiser OFFLINE+DETERMINISTICALLY over the goal->expected-synthesis fixtures at this JSONL bank and print the per-worker structural-fidelity report. No model, no arms, no campaign — scores the synthesiser's CONSTRUCTION directly.")
	decisionQualityBank := fs.String("decision-quality", "", "A2 decision-quality mode (Track A, registry-target-spec §1/§3): drive the REAL Deliberator + Verifier sub-agents over the decision/ship fixtures at this JSONL bank (the engine's skill->expand->workflow->fire machinery), score each worker's actual verdict against the vetted-sound oracle, write a substrate-tagged ledger, and print the per-worker pass-rate baseline. Honours --backend (test|claude|...). No arms, no isolation predicate — scores OUTPUT correctness directly.")
	bandPassMode := fs.Bool("band-pass", false, "B1 seam.band_pass mechanism mode (#11, metric=grounding precision): drive the REAL engine intake band-pass (engine.bandPassIntake + seams.BandPass) OFFLINE+DETERMINISTICALLY over the standing intake-stream suite OFF then ON, and print the suppress-noise / preserve-signal OFF/ON delta + an honest SIGNAL/NO-SIGNAL verdict. No model, no arms, no campaign — scores the mechanism's intended behaviour directly.")
	gateRouterMode := fs.Bool("gate-router", false, "B1 action.gate_router mechanism mode (#13, metric=safety): drive the REAL action.ToolExecutor.Execute gate pipeline OFFLINE+DETERMINISTICALLY over the standing safety suite OFF then ON, and print the gate-correctness / unsafe-op false-allow OFF/ON delta + an honest SIGNAL/NO-SIGNAL verdict. No model, no arms, no campaign — scores the mechanism's intended gate decisions directly.")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	// --band-pass / --gate-router are standalone OFFLINE mechanism modes (no campaign, no arms, no model):
	// each drives its REAL mechanism over a deterministic suite OFF then ON and prints the OFF/ON delta +
	// an honest SIGNAL/NO-SIGNAL verdict (B1 config-search Phase-1 — the two knobs that need their OWN
	// bench because they are not single-shot probe knobs).
	if *bandPassMode {
		return bandPassModeReport()
	}
	if *gateRouterMode {
		return gateRouterModeReport()
	}

	// --synth-fidelity is a standalone OFFLINE mode (no campaign, no arms, no model): drive the real
	// synthesiser over the A5 fixtures and print the structural-fidelity report. It scores the
	// synthesiser's OWN construction (the Program tree it builds for each goal) against an expected
	// structural spec, so a miss is a precise, rankable capability gap (registry-target-spec §3 stretch).
	if *synthFidelityBank != "" {
		return synthFidelityReport(*synthFidelityBank)
	}

	// --legible-report is a standalone READ-ONLY mode (no campaign): fold a JSONL event log's legible.*
	// events into the WF-E CC-1 part-3 rollup and print it. This is how the three registry-scaling numbers
	// are read AFTER a real-model run with seam.legible_generation ON.
	if *legibleReportPath != "" {
		return legibleReport(*legibleReportPath, *legibleTopN)
	}

	mechList, err := parseMechanisms(*mechs)
	if err != nil {
		return err
	}
	t := strings.ToLower(strings.TrimSpace(*tier))
	switch t {
	case "a", "b", "both":
	default:
		return fmt.Errorf("--tier must be A, B, or both (got %q)", *tier)
	}
	if *backend == "cc" {
		*backend = "session" // alias, same as the thought CLI
	}
	if *backend != "test" && *backend != "llm" && *backend != "session" && *backend != "claude" {
		return fmt.Errorf("--backend must be test, llm, session or claude (got %q)", *backend)
	}

	cfg := config{
		bank:        *bank,
		backend:     *backend,
		llmURL:      *llmURL,
		llmModel:    *llmModel,
		mechanisms:  mechList,
		tier:        t,
		replaysA:    max1(*replays),
		replaysB:    max1(*replaysB),
		maxItems:    *maxItems,
		skipItems:   *skipItems,
		seedBase:    *seedBase,
		temp:        *temp,
		out:         *out,
		report:      *report,
		concurrency: max1(*concurrency),
		fixedSeed:   *fixedSeed,
		rates:       *rates,
		rateModel:   *rateModel,
		noGuard:     *noGuard,
		maxCalls:    *maxCalls,
		maxTokens:   *maxTokens,
	}

	// --decision-quality is a standalone A2 mode (no arm campaign, no isolation predicate): drive the REAL
	// Deliberator/Verifier sub-agents over the decision/ship fixtures and score their actual verdicts. It
	// honours --backend (test|claude|...) so the offline test-double smoke and the live claude baseline run
	// through the same wiring; it writes a substrate-tagged per-substrate ledger and a per-worker pass-rate
	// report (the A2 baseline number). Placed AFTER cfg is built so the backend/out/report flags apply.
	if *decisionQualityBank != "" {
		return decisionQualityReport(cfg, *decisionQualityBank)
	}

	return execute(cfg)
}

// execute is the whole pilot: build the factory + ledger, run every mechanism's
// banks serially, reduce to MechResults, render + persist the report.
func execute(cfg config) error {
	start := time.Now()
	runID := "bench-" + start.UTC().Format("20060102T150405Z")

	// Load the rate card up front so a typo'd --rates path fails BEFORE a long run
	// (the rate card is load-bearing; an empty path uses the embedded seed).
	card, err := cost.LoadFile(cfg.rates)
	if err != nil {
		return err
	}

	// GPU/MODEL LOCK + GUARD (--backend llm only; the test double is exempt). Pin the
	// EXPECTED model id ONCE here — refusing to start with no model loaded — write the
	// advisory runs/gpu.lock (removed on clean exit), and build the guard that aborts
	// the run if the loaded model changes mid-campaign (a silent swap turned two prior
	// campaigns into false NO-SIGNAL). --no-guard disables the periodic check.
	guard, releaseLock, err := setupGuard(&cfg, runID)
	if err != nil {
		return err
	}
	defer releaseLock()

	// ONE backend factory for the chosen backend, with --temp baked in so the
	// runner's internal DefaultTemperature does not override it. The test double
	// ignores temp; the LLM factory bakes temp into every per-arm backend.
	factory := buildFactory(cfg)

	store, finalizeLedger, err := openLedger(cfg.out)
	if err != nil {
		return err
	}

	progressf("bench: backend=%s model=%s tiers=%s K_A=%d K_B=%d temp=%.3g seed-base=%d concurrency=%d\n",
		cfg.backend, displayModel(cfg), tierLabel(cfg.tier), cfg.replaysA, cfg.replaysB, cfg.temp, cfg.seedBase, cfg.concurrency)

	// PLAN PHASE: build the full work list — one job per (mechanism, item, arm,
	// replay) cell — across every mechanism × tier, plus the deferred reductions.
	// Collecting all cells into ONE list lets the pool stay saturated across
	// mechanism boundaries (every continuous-batching slot stays busy). The
	// per-cell seed (seed-base + r) is baked in here, identically to the serial
	// path, so results do not depend on execution order.
	var jobs []job
	var mechReducers []mechWork

	for _, mech := range cfg.mechanisms {
		if cfg.tier == "a" || cfg.tier == "both" {
			js, mw, rerr := planTierA(cfg, store, factory, mech)
			if rerr != nil {
				return rerr
			}
			jobs = append(jobs, js...)
			if mw != nil {
				mechReducers = append(mechReducers, *mw)
			}
		}
		if cfg.tier == "b" || cfg.tier == "both" {
			js, mw, rerr := planTierB(cfg, store, factory, mech)
			if rerr != nil {
				return rerr
			}
			jobs = append(jobs, js...)
			if mw != nil {
				mechReducers = append(mechReducers, *mw)
			}
		}
	}

	// RUN PHASE: drain the work list through the worker pool (concurrency==1 is
	// exactly the serial path). All ledger appends + cell records are serialized
	// inside the pool; only the slow per-cell engine/backend work runs in parallel.
	// llmCalls is every cell's per-call token usage (the cost report's substrate);
	// runCalls keeps those calls grouped per RUN (one entry per cell) so the per-tick
	// rollup can bucket by tick within a run (ticks restart each engine run).
	budget := newCostGuard(cfg.maxCalls, cfg.maxTokens) // W4: metered-substrate budget ceiling (nil = unlimited)
	totalCalls, llmCalls, runCalls := runPool(jobs, cfg.concurrency, guard, budget)

	// COST: aggregate the campaign's per-call token usage per RUN / ROLE / MODEL and
	// price it against the rate card (an unpriced model is reported "rates unknown",
	// never a silent $0; --rate-model projects a local run's $).
	costBreakdown := cost.Compute(llmCalls, card)

	// PER-TICK SPEND (the WF-E baseline headline): mean/median LLM calls + tokens per
	// engine tick, bucketed by tick WITHIN each run (run boundaries respected). Read-
	// only over the same usage records; it prices nothing.
	perTick := cost.PerTickSpend(runCalls)

	// REDUCE PHASE: every cell is now recorded; reduce each mechanism × tier to a
	// MechResult. This reads the fully-populated cellStore (keyed by item/arm), so
	// it is invariant to the order the pool happened to complete cells in.
	var results []eval.MechResult
	for _, mw := range mechReducers {
		results = append(results, mw.reduce(mw.cells))
	}

	// CONTAMINATION: the GUARD caught a mid-run model swap. The cells that ran AFTER
	// the swap were scored against the WRONG model — the whole contrast is invalid.
	// We still FLUSH what completed (the report + ledgers below) but flag every
	// artifact CONTAMINATED and return a non-nil error so main exits NON-ZERO.
	contaminated := guard.Aborted()
	var swap swapDetail
	if contaminated {
		swap = guard.Detail()
	}

	header := eval.ReportHeader{
		Backend:    cfg.backend,
		Model:      resolvedModel(cfg),
		LLMURL:     cfg.llmURL,
		KTierA:     cfg.replaysA,
		KTierB:     cfg.replaysB,
		Temp:       cfg.temp,
		Tiers:      tierLabel(cfg.tier),
		Mechanisms: cfg.mechanisms,
		ModelCalls: totalCalls,
		Wall:       time.Since(start),
		SeedBase:   cfg.seedBase,
		Cost:       &costBreakdown,
		PerTick:    perTick,
		RateModel:  cfg.rateModel,
	}
	text := eval.Render(header, results)
	if contaminated {
		// Prepend the boxed CONTAMINATED notice so the report file cannot be read
		// without seeing it (the on-disk mirror of the stderr banner).
		text = contaminationBanner(swap) + text
	}
	// BUDGET ABORT (W4): the cost guard stopped the run mid-campaign. Prepend the boxed notice so
	// the partial report can never be mistaken for a complete one (mirrors the contamination path).
	if budget.Aborted() {
		text = budgetBanner(budget) + text
	}

	if err := writeReport(cfg.report, text); err != nil {
		return err
	}

	// Record a per-mechanism verdict row on the ledger so the campaign's audit and
	// the keep-rule reads from one source of truth (spec §4.6, §5.7).
	if err := recordVerdicts(store, results, cfg.seedBase); err != nil {
		return err
	}

	// Move the produced ledger onto the exact --out path the user named (the ledger
	// package writes a fixed ledger.jsonl inside a directory; we honor --out FILE).
	ledgerPath, err := finalizeLedger()
	if err != nil {
		return err
	}

	// EXPERIMENT LEDGER: append ONE index row for this whole campaign to the single
	// append-only runs/experiments.jsonl — config + aggregate tokens + $ + cache-hit
	// % + per-mechanism key metrics — so every experiment is revisitable in one file.
	// The wall-clock timestamp is driver-side (time.Now() here is fine — this is a
	// normal binary, NOT engine code). A failed append is logged, not fatal: the run
	// already produced its report + measurement ledger; losing the index row must not
	// abort the run. On contamination the row carries Contaminated=true + the swap
	// detail so the experiments index never reads a swapped run as a clean result.
	wall := time.Since(start)
	row := buildExperimentRow(cfg, costBreakdown, perTick, results, totalCalls, wall, time.Now())
	row.RunID = runID
	if contaminated {
		row.Contaminated = true
		row.Swap = &swap
	}
	if expPath, eerr := appendExperimentRow(row); eerr != nil {
		progressf("bench: WARN experiments ledger append failed: %v\n", eerr)
	} else {
		progressf("bench: experiments -> %s (run_id=%s)\n", expPath, row.RunID)
	}

	progressf("bench: DONE — %d model-calls, %s wall-clock\n", totalCalls, wall.Round(time.Millisecond))
	progressf("bench: ledger -> %s\n", ledgerPath)
	progressf("bench: report -> %s\n", cfg.report)
	// The report itself goes to stdout (the driver's primary artifact).
	fmt.Print(text)

	if contaminated {
		// Flushed what completed; now FAIL the process so no automation reads a
		// swapped campaign as a passing run.
		return fmt.Errorf("MODEL SWAPPED MID-RUN: expected %q got %q at cell %d/%d — "+
			"benchmark CONTAMINATED (report + experiments row flagged); re-load the expected "+
			"model and re-run", swap.Expected, swap.Got, swap.CellIndex, swap.CellTotal)
	}
	// BUDGET ABORT: flushed the prefix; FAIL the process so no automation reads a truncated
	// campaign as a complete run (W4 — the metered-substrate ceiling).
	if budget.Aborted() {
		calls, tokens := budget.spent()
		return fmt.Errorf("BUDGET EXCEEDED: %s (spent %d calls, %d tokens) — run aborted, report "+
			"covers the prefix only; raise --max-calls/--max-tokens or narrow the campaign",
			budget.Reason(), calls, tokens)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tier-A: per-item, per-arm, per-replay.
// ---------------------------------------------------------------------------

// planTierA loads a mechanism's Tier-A bank and builds one job per (item, arm,
// replay) cell, plus the deferred reduction that turns the collected per-(item,
// arm) replay cells into the MechResult once the pool has drained. It runs NO
// cells itself — the pool does (concurrency-controlled). A missing/empty bank is
// NOT fatal: it is logged and yields no jobs and no reducer.
func planTierA(cfg config, store *ledger.Store, factory runner.BackendFactory, mech benchtypes.Mechanism) ([]job, *mechWork, error) {
	path := gen.BankFileA(cfg.bank, mech)
	items, err := gen.LoadBankA(path)
	if err != nil {
		if os.IsNotExist(err) {
			progressf("bench: [%s tier-A] no bank at %s — skip\n", mech, path)
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load Tier-A bank %q: %w", path, err)
	}
	if len(items) == 0 {
		progressf("bench: [%s tier-A] empty bank — skip\n", mech)
		return nil, nil, nil
	}
	// Chunk window (--skip-items M, --max-items N): items[M : M+N]. Skip first so a long
	// campaign can run as validated chunks (items 1-4, 5-8, ...) whose ledgers concatenate;
	// 0/0 = all (the default → byte-identical to before). The full bank is still on disk.
	if cfg.skipItems > 0 {
		if cfg.skipItems >= len(items) {
			progressf("bench: [%s tier-A] --skip-items %d skips the whole bank (%d items) — skip\n", mech, cfg.skipItems, len(items))
			return nil, nil, nil
		}
		progressf("bench: [%s tier-A] CHUNK — skipping first %d of %d items\n", mech, cfg.skipItems, len(items))
		items = items[cfg.skipItems:]
	}
	if cfg.maxItems > 0 && len(items) > cfg.maxItems {
		progressf("bench: [%s tier-A] SMOKE subset — first %d of %d items\n", mech, cfg.maxItems, len(items))
		items = items[:cfg.maxItems]
	}

	gateOffSupported := runner.SupportedGateOff(mech)
	arms := armsFor(gateOffSupported)
	cells := newCells()
	batchID := string(mech) + "-pilot"

	jobs := tierAJobs(cfg, store, factory, mech, items, arms, cells, batchID)
	mw := &mechWork{
		cells: cells,
		reduce: func(c *cellStore) eval.MechResult {
			return eval.Summarize(mech, benchtypes.TierAtomic, cfg.replaysA, c.m, gateOffSupported, cfg.seedBase)
		},
	}
	return jobs, mw, nil
}

// ---------------------------------------------------------------------------
// Tier-B: per-scenario, per-arm, per-replay.
// ---------------------------------------------------------------------------

// planTierB is the Tier-B analogue of planTierA: it loads the mechanism's Tier-B
// bank and builds one job per (scenario, arm, replay) cell plus the deferred
// reduction. It runs no cells itself.
func planTierB(cfg config, store *ledger.Store, factory runner.BackendFactory, mech benchtypes.Mechanism) ([]job, *mechWork, error) {
	path := gen.BankFileB(cfg.bank, mech)
	scns, err := gen.LoadBankB(path)
	if err != nil {
		if os.IsNotExist(err) {
			progressf("bench: [%s tier-B] no bank at %s — skip\n", mech, path)
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load Tier-B bank %q: %w", path, err)
	}
	if len(scns) == 0 {
		progressf("bench: [%s tier-B] empty bank — skip\n", mech)
		return nil, nil, nil
	}

	gateOffSupported := runner.SupportedGateOff(mech)
	arms := armsFor(gateOffSupported)
	cells := newCells()
	batchID := string(mech) + "-pilot"

	jobs := tierBJobs(cfg, store, factory, mech, scns, arms, cells, batchID)
	mw := &mechWork{
		cells: cells,
		reduce: func(c *cellStore) eval.MechResult {
			return eval.Summarize(mech, benchtypes.TierScenario, cfg.replaysB, c.m, gateOffSupported, cfg.seedBase)
		},
	}
	return jobs, mw, nil
}

// ---------------------------------------------------------------------------
// Robust single-run wrappers (a failing model call must NOT crash the campaign).
// ---------------------------------------------------------------------------

// safeRunItem runs one Tier-A item under one arm, recovering from a panic and
// treating an empty / error answer as a fail-with-note rather than a crash (spec
// deliverable: ROBUST to a model call failing). The returned ItemResult always
// has a usable EventsPointer note.
func safeRunItem(item benchtypes.TierAItem, arm benchtypes.Arm, seed int64, factory runner.BackendFactory) (res benchtypes.ItemResult) {
	defer func() {
		if rec := recover(); rec != nil {
			res = benchtypes.ItemResult{
				ID: item.ID, Seed: seed, Arm: arm,
				Pass: false, OracleVerdict: false, IsolationResult: false,
				EventsPointer: fmt.Sprintf("PANIC recovered: %v", rec),
			}
		}
	}()
	res = tiera.RunItem(item, arm, seed, factory)
	// An empty answer from a harness/bare arm is a fail-with-note, not a crash:
	// RunItem already scored it (a blank answer fails the oracle); annotate so the
	// audit shows it was an empty/failed model call rather than a wrong answer.
	if strings.TrimSpace(res.RawOutput) == "" && !strings.HasPrefix(res.EventsPointer, "UNSUPPORTED") {
		res.EventsPointer = "empty-answer (model returned nothing) | " + res.EventsPointer
	}
	return res
}

// safeRunScenario is the Tier-B analogue of safeRunItem.
func safeRunScenario(scn benchtypes.TierBScenario, arm benchtypes.Arm, seed int64, factory runner.BackendFactory) (res benchtypes.ScenarioResult) {
	defer func() {
		if rec := recover(); rec != nil {
			res = benchtypes.ScenarioResult{
				ID: scn.ID, Seed: seed, Arm: arm,
				Pass: false, OracleVerdict: false, IsolationResult: false,
				RawOutput: fmt.Sprintf("PANIC recovered: %v", rec),
			}
		}
	}()
	// Tier-B grounding/retrace scenarios need a REAL Action sandbox: the engine's "run the test
	// suite" intention must dispatch a genuine failing test so Observation.Ok=false can refute the
	// committed line (the offline heuristic-act path canned a "tests pass" outcome — Ok=true — which
	// the retrace isolation predicate can never witness). For the multi-step-retrace mechanism we
	// materialize the scenario's failing-test fixture into a per-scenario temp workspace; every other
	// mechanism keeps the offline path ("") unchanged.
	workspace := ""
	if cleanup, ws, ok := tierBWorkspace(scn); ok {
		workspace = ws
		defer cleanup()
	}
	res = tierb.RunScenario(scn, arm, seed, factory, workspace)
	if strings.TrimSpace(res.RawOutput) == "" {
		res.RawOutput = "empty-answer (model returned nothing)"
	}
	return res
}

// tierBWorkspace materializes a per-scenario Action sandbox for the mechanisms whose Tier-B
// grounding step must hit REAL reality (multi-step-retrace: a failing test that refutes the
// committed line with Observation.Ok=false). It writes the scenario's RetraceFixture (a base64
// failing `test_*.py`, decoded) into a fresh temp dir and returns (cleanup, root, true). For any
// other mechanism — or a retrace scenario with no fixture declared — it returns ok=false and the
// caller runs on the offline path ("") exactly as before. Best-effort: a write failure falls back
// to the offline path rather than failing the cell.
func tierBWorkspace(scn benchtypes.TierBScenario) (func(), string, bool) {
	if scn.Mechanism != benchtypes.MechMultiStepRetrace {
		return nil, "", false
	}
	name, content := tierb.RetraceFixture(scn)
	if name == "" || len(content) == 0 {
		return nil, "", false
	}
	root, err := os.MkdirTemp("", "tierb-retrace-")
	if err != nil {
		return nil, "", false
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
		cleanup()
		return nil, "", false
	}
	return cleanup, root, true
}

// ---------------------------------------------------------------------------
// Wiring helpers.
// ---------------------------------------------------------------------------

// checkerVersion is the pilot checker-version tag recorded on every ledger row
// (spec §5.7 CheckerVersion). Bumped when an oracle/predicate is re-characterized
// (Invalidate then keys off it).
const checkerVersion = "pilot-v1"

// buildFactory builds the single backend factory for the chosen backend, baking
// --temp in so it is honored regardless of the runner's internal default temp.
// The test double is deterministic and ignores temp. The LLM factory is wrapped
// so the campaign's --temp wins over whatever temp the runner threads in
// (RunItem/RunScenario call the factory at the runner's DefaultTemperature; we
// pin the campaign's temp here so every arm samples at the locked temperature).
func buildFactory(cfg config) runner.BackendFactory {
	if cfg.backend == "test" {
		return runner.TestFactory
	}
	if cfg.backend == "session" || cfg.backend == "cc" {
		// The SESSION bridge: every call spools to a worker cc session
		// (THOUGHT_SESSION_SPOOL; tools/cc-lane.sh worker). Temperature and seed
		// are NOT controllable on this substrate — rows carry backend="session"
		// and must never be compared against temperature-controlled local rows
		// as if temp were held. No GPU/lms involvement, so the model-swap guard
		// does not apply (it is llm-only by construction).
		return func(_ int64, _ float64) backends.Backend {
			return llm.NewSessionBridge("", 0, 0)
		}
	}
	if cfg.backend == "claude" {
		// The Claude Code CLI bridge (frontier substrate, subscription, no GPU). Built via the
		// consolidated resolver so --llm-model maps to the primary alias. Temperature is NOT
		// controllable on this substrate — rows carry backend="claude:<model>"; the COST GUARD
		// (--max-calls/--max-tokens) is the metered-substrate ceiling, not the GPU swap guard.
		model := cfg.llmModel
		return func(_ int64, _ float64) backends.Backend {
			be, err := llm.MakeBackend("claude", "", model, 0)
			if err != nil { // unreachable (claude builds offline); fall back to the honest test double
				return backends.NewTest()
			}
			return be
		}
	}
	base := runner.LLMFactory(cfg.llmURL, cfg.llmModel)
	temp := cfg.temp
	return func(seed int64, _ float64) backends.Backend {
		return base(seed, temp)
	}
}

// substrateLabel renders the substrate-provenance tag stamped on every ledger
// row ("test", "llm:<model>", "claude:<model>", "cc:session") — rows from
// different substrates must never be compared as one dataset (CLAUDE.md
// substrate hygiene). The claude case is REQUIRED: a Claude-Code-bridge run is
// a distinct substrate (subscription, temperature-uncontrolled) and must NOT be
// tagged llm: — that would let frontier rows mix into the local-llm dataset and
// break re-localization (see internal/llm.ClassOf, the construction-time truth).
func substrateLabel(cfg config) string {
	switch cfg.backend {
	case "test":
		return "test"
	case "session":
		return "cc:session"
	case "claude":
		return "claude:" + resolvedModel(cfg)
	default:
		return "llm:" + resolvedModel(cfg)
	}
}

// armsFor returns the arm set for a mechanism: the full [bare, harness, gate-off]
// when the ablation toggle exists, else [bare, harness] (the gate-off arm is
// UNSUPPORTED and is not run — the report marks the contrast UNSUPPORTED).
func armsFor(gateOffSupported bool) []benchtypes.Arm {
	if gateOffSupported {
		return tierAArms
	}
	return []benchtypes.Arm{benchtypes.ArmBare, benchtypes.ArmHarness}
}

// cellStore accumulates per-(item, arm) replay indicators for the eval reducer.
type cellStore struct {
	m map[string]map[benchtypes.Arm]*eval.ItemArmReplays
}

func newCells() *cellStore {
	return &cellStore{m: map[string]map[benchtypes.Arm]*eval.ItemArmReplays{}}
}

// add records one replay's pass + isolation indicator into the (item, arm) cell.
func (c *cellStore) add(itemID string, arm benchtypes.Arm, pass, isolation bool) {
	byArm := c.m[itemID]
	if byArm == nil {
		byArm = map[benchtypes.Arm]*eval.ItemArmReplays{}
		c.m[itemID] = byArm
	}
	cell := byArm[arm]
	if cell == nil {
		cell = &eval.ItemArmReplays{ItemID: itemID, Arm: arm}
		byArm[arm] = cell
	}
	cell.Passes = append(cell.Passes, b2f(pass))
	cell.Isolations = append(cell.Isolations, b2f(isolation))
}

// openLedger opens the append-only ledger so its rows ultimately land on the
// EXACT --out FILE path the user named. The ledger package works on a directory
// and writes a fixed ledger.jsonl inside it; to honor an arbitrary --out
// basename (e.g. smoke-ledger.jsonl) WITHOUT clobbering any pre-existing
// ledger.jsonl in the target dir, we stage the run in a unique sibling
// directory, then the returned finalize closure renames the staged ledger.jsonl
// onto outPath. finalize returns the final ledger path (== outPath on success).
//
// Staging in a sibling of outPath (same filesystem) keeps the rename atomic and
// leaves the user's chosen directory untouched until the run completes.
func openLedger(outPath string) (store *ledger.Store, finalize func() (string, error), err error) {
	dir := filepath.Dir(outPath)
	if dir == "" || dir == "." {
		dir = "runs"
		outPath = filepath.Join(dir, filepath.Base(outPath))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("mkdir ledger dir %q: %w", dir, err)
	}
	stageDir, err := os.MkdirTemp(dir, ".bench-ledger-")
	if err != nil {
		return nil, nil, fmt.Errorf("stage ledger dir under %q: %w", dir, err)
	}
	store, err = ledger.Open(stageDir)
	if err != nil {
		os.RemoveAll(stageDir)
		return nil, nil, fmt.Errorf("open ledger dir %q: %w", stageDir, err)
	}
	finalize = func() (string, error) {
		defer os.RemoveAll(stageDir)
		staged := store.Path()
		if err := os.Rename(staged, outPath); err != nil {
			return "", fmt.Errorf("finalize ledger %q -> %q: %w", staged, outPath, err)
		}
		return outPath, nil
	}
	return store, finalize, nil
}

// appendRow appends one measurement row, logging (not crashing) on a write error
// — a single failed append must not abort the whole campaign. The caller
// serializes appends (the pool invokes every collect() under one mutex), so this
// never races the ledger; the underlying file is O_APPEND so the row order is
// whatever order the pool completed cells in (order is irrelevant — the reducer
// keys cells by item/arm, and verdict reads sort).
func appendRow(store *ledger.Store, rec ledger.Record) {
	if err := store.Append(rec); err != nil {
		progressf("bench: WARN ledger append failed (%s/%s/%s): %v\n", rec.Mechanism, rec.Tier, rec.Arm, err)
	}
}

// recordVerdicts appends one keep-rule verdict row per MechResult (spec §4.6). At
// the pilot N every verdict is FLAG (not-yet-validated) — the pilot establishes
// feasibility, never a keep. The Contrast + the feasibility verdict are recorded
// so the campaign audit reads from the ledger alone.
func recordVerdicts(store *ledger.Store, results []eval.MechResult, seedBase int64) error {
	for _, r := range results {
		v := ledger.VerdictInput{
			Tick:        int(seedBase),
			Mechanism:   r.Mechanism,
			Tier:        r.Tier,
			IterK:       0, // pilot = iter-0 baseline (never auto-kept; spec §4.6)
			KeepVerdict: ledger.VerdictFlag,
			// Sanitize NaN/Inf (an empty arm or 0-pass isolation leaves them) — JSON
			// cannot encode them, and a NaN on the ledger is meaningless anyway.
			Contrast: sanitizeContrast(r.Contrast()),
			MDE:      eval.MDE,
		}
		if err := store.RecordVerdict(v); err != nil {
			progressf("bench: WARN verdict append failed (%s/%s): %v\n", r.Mechanism, r.Tier, err)
		}
	}
	return nil
}

// sanitizeContrast replaces any NaN/Inf estimate field with 0 so the contrast can
// be JSON-encoded onto the ledger (an empty arm or a 0-pass isolation rate leaves
// NaN; the report renders the true NaN, but the persisted ledger row must be
// valid JSON). Returns nil unchanged.
func sanitizeContrast(c *benchtypes.Contrast) *benchtypes.Contrast {
	if c == nil {
		return nil
	}
	clean := *c
	clean.HarnessMinusBare = sanitizeEstimate(clean.HarnessMinusBare)
	clean.GateOnMinusGateOff = sanitizeEstimate(clean.GateOnMinusGateOff)
	clean.IsolationRate = sanitizeEstimate(clean.IsolationRate)
	return &clean
}

// sanitizeEstimate zeroes any non-finite field of an Estimate.
func sanitizeEstimate(e benchtypes.Estimate) benchtypes.Estimate {
	e.Point = finite(e.Point)
	e.CILow = finite(e.CILow)
	e.CIHigh = finite(e.CIHigh)
	return e
}

// finite maps NaN/±Inf to 0, leaving every real value unchanged.
func finite(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return x
}

// writeReport writes the rendered report, creating the parent dir.
func writeReport(path, text string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir report dir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("write report %q: %w", path, err)
	}
	return nil
}

// parseMechanisms parses a comma-separated mechanism list (empty ⇒ all six),
// validating each against the known set.
func parseMechanisms(s string) ([]benchtypes.Mechanism, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return allMechanisms, nil
	}
	known := map[benchtypes.Mechanism]bool{}
	for _, m := range allMechanisms {
		known[m] = true
	}
	var out []benchtypes.Mechanism
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		m := benchtypes.Mechanism(part)
		if !known[m] {
			return nil, fmt.Errorf("unknown mechanism %q (known: %s)", part, joinMechs(allMechanisms))
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return allMechanisms, nil
	}
	return out, nil
}

func joinMechs(ms []benchtypes.Mechanism) string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = m.String()
	}
	return strings.Join(parts, ", ")
}

func tierLabel(t string) string {
	switch t {
	case "a":
		return "A"
	case "b":
		return "B"
	default:
		return "A+B"
	}
}

// displayModel is the model label for the opening progress line.
func displayModel(cfg config) string {
	if cfg.backend == "test" {
		return "heuristic-double"
	}
	return resolvedModel(cfg)
}

// resolvedModel is the model id for the report header + the experiments row. For an
// llm run it is the GUARD-pinned expected id (the concrete model the run actually
// targeted, even when --llm-model was "auto"); falls back to the raw flag if the
// guard did not resolve one (e.g. --no-guard never resolved "auto").
func resolvedModel(cfg config) string {
	if cfg.backend == "test" {
		return "heuristic-double"
	}
	if cfg.expectedModel != "" {
		return cfg.expectedModel
	}
	return cfg.llmModel
}

// progressf writes a progress line to STDERR so a long --backend llm run is
// observable while stdout carries the final report (spec deliverable: "print
// progress to stderr ... so a long run is observable").
func progressf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
