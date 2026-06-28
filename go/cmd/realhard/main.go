// Command realhard drives the REAL-WORLD-HARD cognition eval — the bare-vs-harness
// headroom proof (internal/bench/realhard). For each hard task it runs ARM A
// (bare model, one Generate call, no tools) and ARM B (full harness engine over
// a materialized workspace) over K replays, scores both with the SAME
// deterministic oracle, and prints the per-arm solve-rate + per-capability lift.
//
// This settles the saturation question on REAL difficulty (the toy 10-task probe
// was an artifact): does bare claude sonnet GENUINELY FAIL a meaningful fraction
// (the headroom), and does the harness RECOVER what bare misses?
//
// SUBSTRATE: --backend claude is the live dev/validation default (claude:sonnet
// +haiku). --backend test is the offline double (the suite/UAT path — no model).
// Run FOREGROUND-DURABLE: `go run ./cmd/realhard ... > runs/<name>.log 2>&1`.
//
//	go run ./cmd/realhard --backend claude --replays 3 \
//	    --out runs/realhard.log --report runs/realhard-report.txt
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/bench/banks/external"
	"github.com/berttrycoding/thought-harness/internal/bench/realhard"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "realhard: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("realhard", flag.ContinueOnError)
	backend := fs.String("backend", "test", "backend: test (offline double) | claude (Claude Code bridge) | llm (local OpenAI-compatible)")
	llmURL := fs.String("llm-url", "http://localhost:1234/v1", "LLM base URL (--backend llm)")
	llmModel := fs.String("llm-model", "auto", "model id, or 'auto' (--backend llm/claude)")
	replays := fs.Int("replays", 3, "K replays per task per arm (pass@k reliability)")
	seedBase := fs.Int64("seed-base", 1729, "base RNG seed (per-replay = seed-base + r)")
	maxTicks := fs.Int("max-ticks", 60, "harness episode tick cap")
	concurrency := fs.Int("concurrency", 1, "task-level parallelism (arms within a task run serially)")
	maxCalls := fs.Int("max-calls", 0, "per-suite model-CALL budget ceiling (0 = unlimited); the metered-substrate guard")
	report := fs.String("report", "", "write the plain-text report to this path (also printed)")
	onlyArm := fs.String("only-arm", "", "restrict to one arm for calibration: bare | harness (default both)")
	onlyTask := fs.String("only-task", "", "comma-separated task-ID substrings; run only tasks whose ID contains any of them (e.g. 'held' for the held-out set, 'back,mhop' for the in-suite collateral). Empty = ALL tasks (byte-identical).")
	launches := fs.Int("launches", 0, "σ_R mode: run the whole suite R INDEPENDENT launches and report run-level robustness (per-task σ_R + mean-guard); 0 = OFF (the normal headroom run)")
	sigmaReport := fs.String("sigma-report", "", "write the σ_R report to this path (--launches mode; also printed)")
	estimator := fs.String("estimator", "off", "noise-aware estimator over the σ_R launches: off (today, byte-identical) | paired | glmm (routed to paired)")
	covariates := fs.String("covariates", "launch_temp", "CUPED covariate csv (estimator!=off): launch_temp (clean default) | model_calls,grounded,value,tool_select,force_ground (leaky, leakage-guarded)")
	cuped := fs.Bool("cuped", true, "enable CUPED variance reduction (estimator!=off); false reports raw β only")
	robustnessGate := fs.Bool("robustness-gate", false, "run the σ²_run variance-ratio bootstrap gate (needs a paired ON arm)")
	requiredCI := fs.Float64("required-ci", 0, "the effect δ the gate must resolve (0 -> ruler default 0.15)")
	estReport := fs.String("est-report", "", "write the estimator report to this path (--launches + --estimator!=off; also printed)")
	bernoulli := fs.Bool("bernoulli", false, "BERNOULLI HIGH-K SINGLE-LAUNCH mode: run the suite ONCE at K=--replays and emit the Bernoulli read (per-task p-hat + Wilson CI + variance-by-formula + adaptive-K + overdispersion self-check). Default OFF -> byte-identical")
	bernReport := fs.String("bern-report", "", "write the Bernoulli report to this path (--bernoulli; also printed)")
	bernK := fs.Int("bern-k", 1, "the deliberative sub-episode count k the analytic binomial-majority q uses (THOUGHT_DELIBERATIVE_K of the ON arm being analyzed); >1 enables the analytic-vs-empirical q cross-check when a deliberative arm is present in-process")
	passK := fs.Int("pass-k", 0, "pass^k RELIABILITY read (--bernoulli / --ab): report per-task pass^k=p^k + mean pass@1 vs pass^k (the tau2-bench brittleness axis). 0 = OFF (byte-identical); >=2 for a meaningful reliability read")
	ab := fs.String("ab", "", "A/B MODE: 'bare-vs-harness' runs the full bare + harness + single-strong A/B over the realhard bank (K=--replays) and emits ONE report: per-task + aggregate harness-vs-bare LIFT, pass@1 vs pass^k, the sub-agent-beats-best-member guard verdict, and the METR time-horizon. Default '' = the legacy headroom run. Honours --report/--pass-k/--max-items/--replays/--only-task.")
	bank := fs.String("bank", "realhard", "(--ab) the task bank: 'realhard' (the built-in frontier suite, default) | 'instrument' (the OFFLINE INSTRUMENT-VALIDATION set — double-solvable, length-diverse, so the METR time-horizon + sub-agent guard return REAL non-vacuous verdicts on --backend test) | a PATH to an external bank JSON file (banks/external schema; ARC-AGI-2 / GAIA shape, converted to the realhard task+oracle shape).")
	maxItems := fs.Int("max-items", 0, "cost cap (--ab): run at most N tasks from the (filtered) bank (0 = all). The cheap-pilot selector for the A/B.")
	singleStrong := fs.Bool("single-strong", true, "(--ab) run the SINGLE-STRONG baseline arm (the sub-agent-beats-best-member guard's baseline). Default true (the full 3-arm A/B). Set --single-strong=false (or --no-single-strong) to run ONLY the bare + harness arms — the HEADLINE lift / pass^k / METR still render, with the sub-agent guard reported NOT-APPLICABLE. Use it to land the 2-arm measurement when the single-strong arm is unwanted or fails on the live substrate.")
	noSingleStrong := fs.Bool("no-single-strong", false, "(--ab) convenience alias for --single-strong=false: run the 2-arm (bare + harness) headline only, guard NOT-APPLICABLE.")
	webSearch := fs.Bool("web", false, "WEB-SEARCH edge: enable the subconscious.web_search flag on the HARNESS arm + wire a LIVE DuckDuckGo seam, so a web-lookup goal (GAIA) dispatches a real web search whose result folds into grounding. Default OFF -> the harness arm is web-blind, byte-identical. Opt-in (real network reads + cost); also via env THOUGHT_WEB_SEARCH=1.")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	// WEB-SEARCH edge (--web or THOUGHT_WEB_SEARCH=1): flip the process-wide web-search edge ON so the
	// harness arm enables subconscious.web_search + wires a live DuckDuckGo seam. Set ONCE here, before
	// the suite runs (read-only during the concurrent run). Default OFF -> web-blind harness arm.
	if *webSearch || envTrue("THOUGHT_WEB_SEARCH") {
		realhard.EnableWebSearch(true)
		fmt.Println("    --web: web_search ENABLED on the harness arm (live DuckDuckGo seam; real network reads)")
	}
	// --no-single-strong is the convenience opt-out alias: it forces the single-strong arm OFF
	// (it can only ever turn it off — it never re-enables a --single-strong=false).
	if *noSingleStrong {
		*singleStrong = false
	}
	if *ab != "" && *ab != "bare-vs-harness" {
		return fmt.Errorf("--ab must be empty or bare-vs-harness (got %q)", *ab)
	}
	if *backend != "test" && *backend != "claude" && *backend != "llm" {
		return fmt.Errorf("--backend must be test, claude, or llm (got %q)", *backend)
	}
	if *onlyArm != "" && *onlyArm != realhard.ArmBare && *onlyArm != realhard.ArmHarness {
		return fmt.Errorf("--only-arm must be empty, bare, or harness (got %q)", *onlyArm)
	}
	estMode := realhard.EstMode(*estimator)
	if estMode != realhard.EstOff && estMode != realhard.EstPaired && estMode != realhard.EstGLMM {
		return fmt.Errorf("--estimator must be off, paired, or glmm (got %q)", *estimator)
	}

	factory, substrate := buildFactory(*backend, *llmURL, *llmModel)

	// ANNOUNCE the substrate loudly (standing requirement).
	fmt.Printf("=== REALHARD eval — SUBSTRATE: %s — replays K=%d — concurrency=%d ===\n",
		substrate, *replays, *concurrency)
	if *backend == "claude" {
		fmt.Println("    (live frontier substrate: claude:sonnet+haiku via the per-call CLI bridge; foreground-durable)")
	}
	// resolve the bank: the built-in realhard suite (default), the offline instrument-
	// validation set, or a converted EXTERNAL bank file. Only --ab honours --bank; the
	// other modes always run the built-in suite (bankTasks==nil => Tasks()).
	var bankTasks []realhard.Task
	if *ab != "" {
		bt, label, err := resolveBank(*bank)
		if err != nil {
			return err
		}
		bankTasks = bt
		if *bank != "realhard" {
			fmt.Printf("    --bank=%q -> %s (%d tasks)\n", *bank, label, len(bankTasks))
		}
	}
	bankSrc := realhard.Tasks()
	if bankTasks != nil {
		bankSrc = bankTasks
	}
	tasks := realhard.FilterTasks(bankSrc, *onlyTask)
	if *onlyTask != "" {
		fmt.Printf("    --only-task=%q -> %d of %d tasks selected\n", *onlyTask, len(tasks), len(bankSrc))
	}
	if len(tasks) == 0 {
		return fmt.Errorf("--only-task %q matched no tasks", *onlyTask)
	}
	fmt.Printf("    %d hard tasks across 4 capabilities; %d arm-runs total\n",
		len(tasks), len(tasks)*(*replays)*armCount(*onlyArm))

	// A/B MODE (--ab bare-vs-harness): the full bare + harness + single-strong A/B over the (filtered,
	// capped) bank, reduced to ONE assembled report — the harness-vs-bare LIFT (per-task + aggregate +
	// per-capability), the pass@1 vs pass^k reliability, the sub-agent-beats-best-member guard verdict
	// (the single-strong arm is what makes the guard return a REAL verdict, not NOT-APPLICABLE), and the
	// METR time-horizon. This is the runnable WIRING the live claude measurement drives. Cost-capped via
	// --max-items (caps the task count) + --replays + --max-calls (the in-flight budget banner).
	if *ab != "" {
		// bind --max-items by trimming the SELECTED tasks to the first N and running them as an explicit
		// ID filter (this is what makes the cap actually bind — a substring filter alone would not).
		filter := *onlyTask
		if *maxItems > 0 && len(tasks) > *maxItems {
			ids := make([]string, *maxItems)
			for i := 0; i < *maxItems; i++ {
				ids[i] = tasks[i].ID
			}
			filter = strings.Join(ids, ",")
			fmt.Printf("    --max-items=%d -> capped to %d tasks\n", *maxItems, *maxItems)
		}
		if *singleStrong {
			fmt.Println("    A/B arms: bare + harness + single-strong (3-arm; sub-agent guard ACTIVE)")
		} else {
			fmt.Println("    A/B arms: bare + harness ONLY (--no-single-strong; HEADLINE lift/pass^k/METR; sub-agent guard NOT-APPLICABLE)")
		}
		var callTotal int64
		start := time.Now()
		abCfg := realhard.ABConfig{
			Factory:      factory,
			Replays:      *replays,
			SeedBase:     *seedBase,
			MaxTicks:     *maxTicks,
			Concurrency:  *concurrency,
			TaskFilter:   filter,
			Tasks:        bankTasks, // nil => the built-in realhard suite; non-nil => instrument-validation / external bank
			PassK:        *passK,
			Substrate:    substrate,
			SingleStrong: *singleStrong,
			OnResult: func(r realhard.RunResult) {
				n := atomic.AddInt64(&callTotal, int64(r.ModelCalls))
				solved := "FAIL"
				if r.Verdict.Solved {
					solved = "SOLVE"
				}
				fmt.Printf("  [%s] %-20s %-13s r%d -> %-5s calls=%d grounded=%v  | %s\n",
					abbrevCap(r.Capability), r.TaskID, r.Arm, r.Replay, solved, r.ModelCalls, r.Grounded, r.Verdict.Reason)
				if *maxCalls > 0 && n >= int64(*maxCalls) {
					fmt.Printf("  *** BUDGET: %d model-calls reached the cap %d — finishing in-flight runs ***\n", n, *maxCalls)
				}
			},
		}
		abRep, armErrs, err := realhard.RunAB(abCfg)
		// PARTIAL failures (some arms errored/panicked, others completed): print every one
		// to STDERR at the end of the run (each already printed its FAIL line via OnResult),
		// then STILL render + write the report. A non-nil err is the TOTAL wipe-out case
		// (every arm failed) — print the failures, then fail loud.
		if len(armErrs) > 0 {
			fmt.Fprintf(os.Stderr, "\n*** %d arm-run(s) FAILED during the A/B (recorded as FAIL, not dropped) ***\n", len(armErrs))
			for _, ae := range armErrs {
				fmt.Fprintln(os.Stderr, "  FAIL: "+ae.Error())
			}
		}
		if err != nil {
			return fmt.Errorf("A/B run: %w", err)
		}
		out := abRep.Render()
		fmt.Printf("\n%s\n", out)
		fmt.Printf("elapsed: %s   total model-calls: %d\n", time.Since(start).Round(time.Second), callTotal)
		if *report != "" {
			if err := os.WriteFile(*report, []byte(out), 0o644); err != nil {
				return fmt.Errorf("write report %s: %w", *report, err)
			}
			fmt.Printf("report written: %s\n", *report)
		}
		return nil
	}

	// BERNOULLI HIGH-K SINGLE-LAUNCH mode (--bernoulli): the CHEAP capability+robustness read on the
	// uncontrollable-temperature substrate. Runs the whole suite ONCE at K=--replays and estimates each
	// task's true p (Wilson CI), the outcome variance p(1-p) BY FORMULA (no repeated launches), the
	// adaptive replay allocation, and the iid-Bernoulli overdispersion self-check. Default OFF.
	//
	// HONEST CONSTRAINT (wiring). THOUGHT_DELIBERATIVE_K is read ONCE at engine init (cannot be flipped
	// in-process), so a single invocation produces ONE arm's counts. The two-proportion capability test
	// and the analytic-vs-empirical q cross-check need a SECOND invocation (the ON / deliberative arm,
	// THOUGHT_DELIBERATIVE_K=k set in the environment) — run both and combine offline via
	// realhard.EstimateBernoulli. This invocation reports the OFF (ambient-config) arm; --bern-k tags the
	// k the analytic q would use so the report names the protocol.
	if *bernoulli {
		delibEnv := os.Getenv("THOUGHT_DELIBERATIVE_K") // the engine's K (read once at engine init)
		if delibEnv == "" {
			delibEnv = "unset(1)"
		}
		fmt.Printf("=== BERNOULLI HIGH-K SINGLE-LAUNCH: 1 launch x %d hard tasks x K=%d replays — engine THOUGHT_DELIBERATIVE_K=%s ===\n",
			len(tasks), *replays, delibEnv)
		off, _, _, err := realhard.RunBernoulli(*replays, *seedBase, *maxTicks, *concurrency, "", factory, substrate, nil, nil, *onlyTask)
		if err != nil {
			return fmt.Errorf("bernoulli run: %w", err)
		}
		bcfg := realhard.BernoulliConfig{
			Mode:          realhard.EstBernOn,
			Delta:         *requiredCI,
			DeliberativeK: *bernK,
			PassK:         *passK,
		}
		brep := realhard.EstimateBernoulli(off, nil, nil, nil, nil, bcfg)
		out := brep.Render()
		fmt.Printf("\n%s\n", out)
		fmt.Printf("\nTWO-INVOCATION PROTOCOL (for the capability + deliberative gates):\n")
		fmt.Printf("  OFF arm : THOUGHT_DELIBERATIVE_K unset  go run ./cmd/realhard --backend claude --bernoulli --replays %d --bern-report runs/bern-off.txt\n", *replays)
		fmt.Printf("  ON  arm : THOUGHT_DELIBERATIVE_K=%d      go run ./cmd/realhard --backend claude --bernoulli --replays %d --bern-k %d --bern-report runs/bern-on.txt\n",
			maxK(*bernK, 3), *replays, maxK(*bernK, 3))
		fmt.Printf("  then combine the two arms' per-task counts via realhard.EstimateBernoulli(off, on, delib, ...).\n")
		if *bernReport != "" {
			if err := os.WriteFile(*bernReport, []byte(out), 0o644); err != nil {
				return fmt.Errorf("write bern-report %s: %w", *bernReport, err)
			}
			fmt.Printf("Bernoulli report written: %s\n", *bernReport)
		}
		return nil
	}

	// σ_R mode (--launches R > 0): measure run-level robustness instead of the headroom proof. Runs the
	// whole suite R INDEPENDENT launches (distinct per-launch seed offset), computes the per-task σ_R
	// (never pooled) + the mean-guard solve-rate, and short-circuits the normal run.
	if *launches > 0 {
		fmt.Printf("=== σ_R MODE: %d INDEPENDENT launches x %d hard tasks (harness arm) — measuring run-level robustness ===\n",
			*launches, len(tasks))
		// estimator OFF (default): exactly the existing path (byte-identical).
		if estMode == realhard.EstOff {
			rep, err := realhard.RunSigmaR(*launches, *replays, *seedBase, *maxTicks, *concurrency, "", factory, substrate, *onlyTask)
			if err != nil {
				return fmt.Errorf("sigma-r run: %w", err)
			}
			out := rep.Render()
			fmt.Printf("\n%s\n", out)
			if *sigmaReport != "" {
				if err := os.WriteFile(*sigmaReport, []byte(out), 0o644); err != nil {
					return fmt.Errorf("write sigma-report %s: %w", *sigmaReport, err)
				}
				fmt.Printf("σ_R report written: %s\n", *sigmaReport)
			}
			return nil
		}

		// estimator ON: drive the RETAINING σ_R run (keeps the per-run covariates) and
		// produce the noise-aware EstimatorReport alongside the σ_R report. The single-
		// arm (OFF-only) run delivers the valid-CI run-effect variance + required-R; a
		// paired ON arm (the deliberative-K lever) is run as a SEPARATE campaign and
		// the two SigmaRData sets combined via realhard.EstimateMatrix (the lever is read
		// once at engine init, so it cannot be flipped in-process — honest constraint).
		fmt.Printf("=== ESTIMATOR: %s — covariates=%s cuped=%v robustness-gate=%v δ=%.3f ===\n",
			estMode, *covariates, *cuped, *robustnessGate, requiredCIOr(*requiredCI))
		rep, data, err := realhard.RunSigmaREstimator(*launches, *replays, *seedBase, *maxTicks, *concurrency, "", factory, substrate, nil, *onlyTask)
		if err != nil {
			return fmt.Errorf("sigma-r(est) run: %w", err)
		}
		sigOut := rep.Render()
		fmt.Printf("\n%s\n", sigOut)
		if *sigmaReport != "" {
			if err := os.WriteFile(*sigmaReport, []byte(sigOut), 0o644); err != nil {
				return fmt.Errorf("write sigma-report %s: %w", *sigmaReport, err)
			}
			fmt.Printf("σ_R report written: %s\n", *sigmaReport)
		}
		estCfg := realhard.EstimatorConfig{
			Mode:       estMode,
			Covariates: splitCSV(*covariates),
			CUPED:      *cuped,
			Delta:      *requiredCI,
			Robustness: *robustnessGate,
		}
		estRep := realhard.EstimateData(data, estCfg)
		estOut := estRep.Render()
		fmt.Printf("\n%s\n", estOut)
		if *estReport != "" {
			if err := os.WriteFile(*estReport, []byte(estOut), 0o644); err != nil {
				return fmt.Errorf("write est-report %s: %w", *estReport, err)
			}
			fmt.Printf("estimator report written: %s\n", *estReport)
		}
		return nil
	}

	var callTotal int64
	start := time.Now()
	cfg := realhard.SuiteConfig{
		Factory:     factory,
		Replays:     *replays,
		SeedBase:    *seedBase,
		MaxTicks:    *maxTicks,
		Concurrency: *concurrency,
		OnlyArm:     *onlyArm,
		TaskFilter:  *onlyTask,
		OnResult: func(r realhard.RunResult) {
			n := atomic.AddInt64(&callTotal, int64(r.ModelCalls))
			solved := "FAIL"
			if r.Verdict.Solved {
				solved = "SOLVE"
			}
			esc := ""
			if r.ToolSelectEscalations > 0 || r.ForceGroundEscalations > 0 {
				esc = fmt.Sprintf(" esc[ts=%d fg=%d]", r.ToolSelectEscalations, r.ForceGroundEscalations)
			}
			fmt.Printf("  [%s] %-20s %-8s r%d -> %-5s calls=%d grounded=%v%s  | %s\n",
				abbrevCap(r.Capability), r.TaskID, r.Arm, r.Replay, solved, r.ModelCalls, r.Grounded, esc, r.Verdict.Reason)
			if *maxCalls > 0 && n >= int64(*maxCalls) {
				fmt.Printf("  *** BUDGET: %d model-calls reached the cap %d — finishing in-flight runs ***\n", n, *maxCalls)
			}
		},
	}

	results, armErrs, err := realhard.RunSuite(cfg)
	// PARTIAL failures: print each (already printed its FAIL line via OnResult), then STILL
	// reduce + write the report. A non-nil err is the TOTAL wipe-out (every arm failed).
	if len(armErrs) > 0 {
		fmt.Fprintf(os.Stderr, "\n*** %d arm-run(s) FAILED during the suite (recorded as FAIL, not dropped) ***\n", len(armErrs))
		for _, ae := range armErrs {
			fmt.Fprintln(os.Stderr, "  FAIL: "+ae.Error())
		}
	}
	if err != nil {
		return fmt.Errorf("suite run: %w", err)
	}

	rep := realhard.Reduce(results)
	out := rep.Render(substrate)
	fmt.Printf("\n%s\n", out)
	fmt.Printf("elapsed: %s   total model-calls: %d\n", time.Since(start).Round(time.Second), callTotal)

	if *report != "" {
		if err := os.WriteFile(*report, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write report %s: %w", *report, err)
		}
		fmt.Printf("report written: %s\n", *report)
	}
	return nil
}

// resolveBank maps the --bank value to the task slice the A/B runs over (nil => the
// built-in realhard suite), plus a human label for the banner. "realhard" => nil (the
// default built-in suite); "instrument" => the offline instrument-validation set; any
// other value is treated as a PATH to an external bank JSON file (banks/external schema)
// and converted to the realhard task shape.
func resolveBank(bank string) ([]realhard.Task, string, error) {
	switch bank {
	case "realhard", "":
		return nil, "built-in realhard suite", nil
	case "instrument":
		return realhard.InstrumentValidationTasks(), "offline instrument-validation set", nil
	default:
		tasks, err := external.LoadFile(bank)
		if err != nil {
			return nil, "", fmt.Errorf("--bank %q: %w", bank, err)
		}
		return tasks, "external bank " + bank, nil
	}
}

// splitCSV splits a comma-separated covariate list, trimming spaces and dropping
// empties. "" -> nil (no covariate).
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// requiredCIOr echoes the configured δ or the ruler default (for the banner only).
func requiredCIOr(d float64) float64 {
	if d > 0 {
		return d
	}
	return 0.15
}

// maxK echoes k when >= floor, else the floor (so the printed two-invocation example
// always suggests a sensible deliberative K even when --bern-k defaulted to 1).
func maxK(k, floor int) int {
	if k > floor {
		return k
	}
	return floor
}

func armCount(only string) int {
	if only == "bare" || only == "harness" {
		return 1
	}
	return 2
}

func abbrevCap(c realhard.Capability) string {
	switch c {
	case realhard.CapMultiHopGrounding:
		return "MHOP"
	case realhard.CapAdaptiveBacktracking:
		return "BACK"
	case realhard.CapAntiConfabulation:
		return "CONF"
	case realhard.CapLongHorizonConsistency:
		return "LONG"
	}
	return "????"
}

// buildFactory returns the backend factory + the substrate label.
func buildFactory(backend, llmURL, llmModel string) (realhard.BackendFactory, string) {
	switch backend {
	case "test":
		return func(seed int64, temp float64) backends.Backend { return backends.NewTest() }, "test"
	case "claude":
		model := llmModel
		return func(_ int64, _ float64) backends.Backend {
			be, err := llm.MakeBackend("claude", "", model, 0)
			if err != nil {
				return backends.NewTest()
			}
			return be
		}, "claude:" + claudeModelLabel(model)
	default: // llm
		base := runner.LLMFactory(llmURL, llmModel)
		return realhard.BackendFactory(base), "llm:" + llmModel
	}
}

func claudeModelLabel(model string) string {
	if model == "" || model == "auto" {
		return "sonnet+haiku"
	}
	return model
}

// envTrue reports whether the named env var is set to a truthy value (1/true/yes/on, case-insensitive)
// — the env half of the --web opt-in (THOUGHT_WEB_SEARCH=1).
func envTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
