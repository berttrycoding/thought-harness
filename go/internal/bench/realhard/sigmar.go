package realhard

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// sigmar.go — the σ_R INSTRUMENT: the run-level ROBUSTNESS measurement.
//
// THE QUANTITY. The realhard eval swings ±56pp run-to-run on claude (same flags-off code). Reframed
// as a robustness gap = σ_R, the cross-run (cross-LAUNCH) standard deviation of the PER-TASK
// solve-rate. The harness has no cross-sample outcome redundancy (one trajectory / one episode / one
// verdict), so this run-level variance is the thing the deliberative lever (THOUGHT_DELIBERATIVE_K)
// attacks. To gate that lever we must first MEASURE σ_R reproducibly.
//
// THE METHOD (genuine independence, NOT K-replay within one launch).
//   - run the WHOLE realhard suite R times, each launch independent: a DISTINCT seed OFFSET per launch
//     so launch-to-launch is genuine cross-run variation, not a re-replay of one seeded launch;
//   - per task, per launch: the launch's solve-rate for that task (0/1 at one replay, a fraction at
//     more) — collected into an R×T matrix;
//   - per task: the SAMPLE standard deviation of its R per-launch solve-rates = that task's σ_R;
//   - aggregate: the MEAN per-task σ_R (the headline robustness number) AND the overall mean
//     solve-rate (the mean-guard companion).
//
// KEY: σ_R is computed PER TASK and then averaged — NEVER pooled. A pooled SD over a flattened
// solve vector cannot distinguish a real per-task variance collapse from a between-task win-reshuffle
// (two tasks swapping which one solved leaves the pool SD unchanged while every per-task σ falls).
// ComputeSigmaR enforces the per-task isolation; SigmaRReport carries it through to the wire.
//
// Determinism: the math (ComputeSigmaR) is a pure reduction over the matrix — no RNG, no I/O. The
// driver (RunSigmaR) threads the seeded per-launch offset so two RunSigmaR calls on the same backend
// + base produce the same matrix (headless-pure; CLAUDE.md determinism rule).

// SigmaRReport is the run-level robustness reduction over R independent launches.
type SigmaRReport struct {
	Launches  int   // R — the number of independent suite launches
	Replays   int   // K replays per task WITHIN each launch (>=1; the per-launch solve-rate denominator)
	SeedBase  int64 // the base seed; launch l used SeedBase + l*launchStride
	Substrate string

	// PerTask is one row per task, in stable task order. Each carries the task's R per-launch
	// solve-rates, its σ_R (sample SD across those R), and its mean solve-rate.
	PerTask []SigmaRTask

	// MeanSigmaR is the headline: the mean of the per-task σ_R values (NOT a pooled SD).
	MeanSigmaR float64
	// MeanSolveRate is the overall mean solve-rate across every task×launch (the mean-guard).
	MeanSolveRate float64
}

// SigmaRTask is one task's σ_R row.
type SigmaRTask struct {
	TaskID     string
	Capability Capability
	// LaunchRates is the task's solve-rate per launch, in launch order (length == Launches).
	LaunchRates []float64
	// SigmaR is the SAMPLE standard deviation of LaunchRates (this task's run-level robustness).
	SigmaR float64
	// MeanRate is the mean of LaunchRates (this task's mean solve-rate).
	MeanRate float64
}

// launchStride keeps successive LAUNCH base seeds far apart in the seed space so two launches do not
// share a near-identical draw prefix. It is a large fixed stride (a pure function of the launch
// index), so the whole σ_R run is reproducible while the launches are genuinely independent — NOT a
// re-replay of one launch. (The per-replay seed WITHIN a launch is still SeedBase' + r, exactly as
// RunSuite does, so an in-launch K-replay stays in-launch.)
const launchStride int64 = 100_003

// ComputeSigmaR is the PURE σ_R math: given an R×T matrix of per-launch per-task solve-rates
// (rates[launch][task]), it returns the per-task σ_R rows, the mean per-task σ_R, and the overall
// mean solve-rate. It is the gate prerequisite — unit-tested offline on synthetic matrices (no model,
// no engine) before any spend.
//
// rates must be a non-empty rectangular R×T matrix (R launches, T tasks); taskIDs/caps name the T
// columns (parallel slices, may be nil — then columns are named c0..c{T-1} with empty caps). The
// per-task σ is the SAMPLE standard deviation (denominator R-1) — the unbiased estimator for the
// cross-launch spread. With R==1 the sample SD is undefined; it is reported as 0 (a single launch
// has no measurable run-level variance) and that degeneracy is the caller's signal to raise R.
//
// PER-TASK ISOLATION is structural: each column's σ is computed from ONLY that column's R values, so
// one task's variance can never leak into another's σ. The mean σ_R is the simple mean of the column
// σ values (never a pooled SD over the flattened matrix).
func ComputeSigmaR(rates [][]float64, taskIDs []string, caps []Capability) (rows []SigmaRTask, meanSigmaR, meanSolveRate float64) {
	r := len(rates)
	if r == 0 {
		return nil, 0, 0
	}
	t := len(rates[0])
	rows = make([]SigmaRTask, t)

	var sigmaSum, rateSum float64
	var rateCount int
	for col := 0; col < t; col++ {
		// gather ONLY this task's column across the R launches (per-task isolation).
		colRates := make([]float64, r)
		for l := 0; l < r; l++ {
			colRates[l] = rates[l][col]
			rateSum += rates[l][col]
			rateCount++
		}
		sig := sampleSD(colRates)
		mean := meanOf(colRates)
		id := ""
		if col < len(taskIDs) {
			id = taskIDs[col]
		} else {
			id = fmt.Sprintf("c%d", col)
		}
		var cp Capability
		if col < len(caps) {
			cp = caps[col]
		}
		rows[col] = SigmaRTask{
			TaskID:      id,
			Capability:  cp,
			LaunchRates: colRates,
			SigmaR:      sig,
			MeanRate:    mean,
		}
		sigmaSum += sig
	}
	if t > 0 {
		meanSigmaR = sigmaSum / float64(t)
	}
	if rateCount > 0 {
		meanSolveRate = rateSum / float64(rateCount)
	}
	return rows, meanSigmaR, meanSolveRate
}

// sampleSD is the sample standard deviation (denominator n-1) of xs. n<2 → 0 (a single observation
// has no measurable spread; the unbiased estimator is undefined and reported as 0). Uses the
// two-pass (mean-then-deviation) form for numerical stability.
func sampleSD(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	mean := meanOf(xs)
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1))
}

// meanOf is the arithmetic mean of xs (0 for an empty slice).
func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// RunSigmaR drives the σ_R measurement: it runs the whole realhard suite R times (each launch on a
// distinct seed offset), reduces each launch into its per-task solve-rates, and computes the σ_R
// report. Only the HARNESS arm is run (ARM B is the config under test — the σ_R question is the
// harness's run-level variance, the thing the deliberative lever moves; the bare arm has no engine and
// no per-launch trajectory to vary).
//
// launches<1 → 1; replays<1 → 1 (the per-launch per-task solve-rate denominator). substrate is the
// substrate label for the report tag (caller-supplied, since the factory is opaque here). Returns the
// report; an error from any launch aborts.
func RunSigmaR(launches int, replays int, seedBase int64, maxTicks int, concurrency int, workspaceRoot string, factory BackendFactory, substrate string, taskFilter string) (SigmaRReport, error) {
	if launches < 1 {
		launches = 1
	}
	if replays < 1 {
		replays = 1
	}
	tasks := FilterTasks(Tasks(), taskFilter)
	taskIDs := make([]string, len(tasks))
	caps := make([]Capability, len(tasks))
	for i, tk := range tasks {
		taskIDs[i] = tk.ID
		caps[i] = tk.Capability
	}

	// rates[launch][taskIndex] = the launch's harness solve-rate for that task.
	rates := make([][]float64, launches)
	for l := 0; l < launches; l++ {
		launchSeed := seedBase + int64(l)*launchStride
		cfg := SuiteConfig{
			Factory:       factory,
			Replays:       replays,
			SeedBase:      launchSeed,
			MaxTicks:      maxTicks,
			Concurrency:   concurrency,
			WorkspaceRoot: workspaceRoot,
			OnlyArm:       ArmHarness, // σ_R is the harness arm's run-level variance (ARM B is under test)
			TaskFilter:    taskFilter,
		}
		results, _, err := RunSuite(cfg)
		if err != nil {
			return SigmaRReport{}, fmt.Errorf("sigma-r launch %d: %w", l, err)
		}
		rates[l] = launchTaskRates(results, taskIDs)
	}

	rows, meanSigmaR, meanSolveRate := ComputeSigmaR(rates, taskIDs, caps)
	return SigmaRReport{
		Launches:      launches,
		Replays:       replays,
		SeedBase:      seedBase,
		Substrate:     substrate,
		PerTask:       rows,
		MeanSigmaR:    meanSigmaR,
		MeanSolveRate: meanSolveRate,
	}, nil
}

// launchTaskRates reduces one launch's flat harness results into a per-task solve-rate, in the order
// of taskIDs (so the matrix columns are aligned across launches). A task with no harness result in
// this launch contributes 0 (it should never happen — RunSuite runs every task — but the zero keeps
// the matrix rectangular).
func launchTaskRates(results []RunResult, taskIDs []string) []float64 {
	solved := map[string]int{}
	total := map[string]int{}
	for _, r := range results {
		if r.Arm != ArmHarness {
			continue
		}
		total[r.TaskID]++
		if r.Verdict.Solved {
			solved[r.TaskID]++
		}
	}
	out := make([]float64, len(taskIDs))
	for i, id := range taskIDs {
		if total[id] > 0 {
			out[i] = float64(solved[id]) / float64(total[id])
		}
	}
	return out
}

// launchTaskCovariates reduces one launch's flat harness results into the per-task
// covariate row the noise-aware estimator (estimator.go) consumes. It is the
// RETENTION the spec §6.3 requires: RunSigmaR's existing reducer (launchTaskRates)
// collapses to solved-only and DISCARDS the per-run covariates the suite already
// collects (Value/V(s), ModelCalls, Grounded, the two escalation counts); this
// keeps them, AVERAGED over the in-launch replays per task (the same denominator
// launchTaskRates uses), aligned to taskIDs. No new event emission — only that we
// stop discarding what RunSuite already returns. Grounded becomes a 0/1 fraction
// (the share of in-launch replays that grounded).
func launchTaskCovariates(results []RunResult, taskIDs []string) []estCovariates {
	type acc struct {
		modelCalls  float64
		grounded    float64
		value       float64
		toolSelect  float64
		forceGround float64
		n           int
	}
	by := map[string]*acc{}
	for _, r := range results {
		if r.Arm != ArmHarness {
			continue
		}
		a := by[r.TaskID]
		if a == nil {
			a = &acc{}
			by[r.TaskID] = a
		}
		a.modelCalls += float64(r.ModelCalls)
		if r.Grounded {
			a.grounded += 1
		}
		a.value += r.Value
		a.toolSelect += float64(r.ToolSelectEscalations)
		a.forceGround += float64(r.ForceGroundEscalations)
		a.n++
	}
	out := make([]estCovariates, len(taskIDs))
	for i, id := range taskIDs {
		a := by[id]
		if a == nil || a.n == 0 {
			continue
		}
		inv := 1 / float64(a.n)
		out[i] = estCovariates{
			modelCalls:  a.modelCalls * inv,
			grounded:    a.grounded * inv,
			value:       a.value * inv,
			toolSelect:  a.toolSelect * inv,
			forceGround: a.forceGround * inv,
		}
	}
	return out
}

// SigmaRData is the RETAINED per-launch data the noise-aware estimator consumes: the
// rate matrix (as ComputeSigmaR), the aligned per-(launch,task) covariate matrix, and
// the column names — for the OFF arm and, when a paired ON arm was run, the ON arm
// too. It is the bridge from RunSigmaREstimator's collection to EstimateMatrix; the
// existing SigmaRReport is unchanged (the off-mode path is byte-identical).
type SigmaRData struct {
	TaskIDs []string
	Caps    []Capability
	// RatesOff[launch][task] — the OFF (baseline) arm's per-task launch solve-rates.
	RatesOff [][]float64
	// CovOff[launch][task] — the OFF arm's retained per-(launch,task) covariates.
	CovOff [][]estCovariates
	// RatesOn / CovOn — the ON (config-under-test) arm, populated ONLY when a paired
	// run was requested (RunSigmaREstimator with onConfig != nil). nil otherwise.
	RatesOn [][]float64
	CovOn   [][]estCovariates
	// Paired is true when ON launches share the OFF launches' seed offsets (the
	// within-launch differencing precondition — both arms ran in the SAME launch).
	Paired bool
}

// RunSigmaREstimator drives the σ_R measurement AND retains the per-run covariates
// the noise-aware estimator (estimator.go) consumes — the spec §3 retention change.
// It is a SUPERSET of RunSigmaR: it produces the same SigmaRReport (off-mode
// byte-identical) PLUS the SigmaRData (rate + covariate matrices).
//
// PAIRED A/B (spec §3 + §4 Miller): when onConfig is non-nil, each launch runs BOTH
// the baseline (OFF) suite AND a second suite under the supplied config closure (ON)
// on the SAME launch seed offset — so A and B share the launch's shock and the
// within-launch paired difference cancels the task-difficulty common-mode. onConfig
// is invoked AROUND the ON suite run (e.g. to set/restore the config it toggles); a
// nil onConfig runs the OFF arm only (RatesOn/CovOn stay nil). The OFF arm is always
// run with the ambient config.
//
// Determinism: same per-launch seed offset as RunSigmaR (launchStride), so the OFF
// matrix is identical to RunSigmaR's; the ON matrix uses the SAME launch seeds
// (genuine pairing). Headless-pure: no model in the reducer; the only nondeterminism
// is the upstream episodes (the noise being modeled).
func RunSigmaREstimator(
	launches, replays int, seedBase int64, maxTicks, concurrency int,
	workspaceRoot string, factory BackendFactory, substrate string,
	onConfig func(run func() error) error,
	taskFilter string,
) (SigmaRReport, SigmaRData, error) {
	if launches < 1 {
		launches = 1
	}
	if replays < 1 {
		replays = 1
	}
	tasks := FilterTasks(Tasks(), taskFilter)
	taskIDs := make([]string, len(tasks))
	caps := make([]Capability, len(tasks))
	for i, tk := range tasks {
		taskIDs[i] = tk.ID
		caps[i] = tk.Capability
	}

	data := SigmaRData{TaskIDs: taskIDs, Caps: caps}
	ratesOff := make([][]float64, launches)
	covOff := make([][]estCovariates, launches)
	if onConfig != nil {
		data.RatesOn = make([][]float64, launches)
		data.CovOn = make([][]estCovariates, launches)
		data.Paired = true
	}

	runSuiteAt := func(launchSeed int64) ([]RunResult, error) {
		cfg := SuiteConfig{
			Factory:       factory,
			Replays:       replays,
			SeedBase:      launchSeed,
			MaxTicks:      maxTicks,
			Concurrency:   concurrency,
			WorkspaceRoot: workspaceRoot,
			OnlyArm:       ArmHarness,
			TaskFilter:    taskFilter,
		}
		res, _, err := RunSuite(cfg)
		return res, err
	}

	for l := 0; l < launches; l++ {
		launchSeed := seedBase + int64(l)*launchStride
		// OFF arm (ambient config) — identical to RunSigmaR.
		offRes, err := runSuiteAt(launchSeed)
		if err != nil {
			return SigmaRReport{}, SigmaRData{}, fmt.Errorf("sigma-r(est) OFF launch %d: %w", l, err)
		}
		ratesOff[l] = launchTaskRates(offRes, taskIDs)
		covOff[l] = launchTaskCovariates(offRes, taskIDs)

		// ON arm (config under test) — SAME launch seed (paired).
		if onConfig != nil {
			var onRes []RunResult
			runErr := onConfig(func() error {
				r, e := runSuiteAt(launchSeed)
				onRes = r
				return e
			})
			if runErr != nil {
				return SigmaRReport{}, SigmaRData{}, fmt.Errorf("sigma-r(est) ON launch %d: %w", l, runErr)
			}
			data.RatesOn[l] = launchTaskRates(onRes, taskIDs)
			data.CovOn[l] = launchTaskCovariates(onRes, taskIDs)
		}
	}

	data.RatesOff = ratesOff
	data.CovOff = covOff

	rows, meanSigmaR, meanSolveRate := ComputeSigmaR(ratesOff, taskIDs, caps)
	rep := SigmaRReport{
		Launches:      launches,
		Replays:       replays,
		SeedBase:      seedBase,
		Substrate:     substrate,
		PerTask:       rows,
		MeanSigmaR:    meanSigmaR,
		MeanSolveRate: meanSolveRate,
	}
	return rep, data, nil
}

// Render produces the plain-text σ_R report (no emoji, box-drawing only) — substrate-tagged, the
// per-task σ in stable task-id order, then the headline mean σ_R + the mean-guard solve-rate.
func (rep SigmaRReport) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("REAL-WORLD-HARD COGNITION EVAL — run-level ROBUSTNESS (σ_R)\n")
	w("substrate: %s   launches(R): %d   replays/launch(K): %d   seed-base: %d\n",
		rep.Substrate, rep.Launches, rep.Replays, rep.SeedBase)
	w("%s\n\n", strings.Repeat("=", 72))

	if rep.Launches < 2 {
		w("WARNING: R=%d launch(es) — σ_R is undefined below 2 launches (reported as 0).\n", rep.Launches)
		w("  -> raise --launches to >=2 to measure run-level variance.\n\n")
	}

	w("PER-TASK σ_R (sample SD of the per-launch solve-rate, across R launches)\n")
	w("  (PER-TASK, never pooled: each σ is from ONLY that task's R launch-rates)\n")
	sorted := append([]SigmaRTask(nil), rep.PerTask...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TaskID < sorted[j].TaskID })
	for _, t := range sorted {
		w("  %-20s [%-24s]  σ_R %5.3f   mean-rate %5.3f   rates %s\n",
			t.TaskID, string(t.Capability), t.SigmaR, t.MeanRate, fmtRates(t.LaunchRates))
	}
	w("\n")

	w("HEADLINE\n")
	w("  mean per-task σ_R : %6.4f   (the robustness number — LOWER is more robust)\n", rep.MeanSigmaR)
	w("  overall mean solve-rate : %6.4f   (the mean-guard: a robustness gain must not cost solve-rate)\n",
		rep.MeanSolveRate)
	return b.String()
}

// fmtRates renders a per-launch solve-rate vector compactly, e.g. "[1.00 0.00 1.00]".
func fmtRates(rs []float64) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = fmt.Sprintf("%.2f", r)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
