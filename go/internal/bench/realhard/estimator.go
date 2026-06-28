package realhard

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/ruler"
)

// estimator.go — the NOISE-AWARE MEASUREMENT ESTIMATOR (spec:
// docs/internal/notes/2026-06-19-noise-aware-estimator-spec.md; theory:
// docs/internal/notes/2026-06-19-noise-modeled-measurement-theory.md).
//
// WHAT IT IS. A pure, deterministic, OFFLINE reducer that sits where ComputeSigmaR
// (sigmar.go) does and consumes the per-(task, launch, config) solve matrix WITH
// the per-run covariates the suite already collects. It models the ±56pp claude
// run-to-run swing as a LAUNCH random effect (u_r ~ N(0, σ²_run)) rather than
// pretending it away, so it can:
//
//   - report the CAPABILITY effect β (a config's effect on solve-rate) with a
//     VALID confidence interval from a variance-component decomposition (task +
//     launch random effects) — the naive pooled SD gives INVALID too-narrow CIs
//     that fire false gates; the valid CI is the CORRECTNESS fix, not just a cost
//     cut. Emits UNDER-IDENTIFIED (à la regulator/gain.go) when the components are
//     not identifiable (R or T_eff too small);
//   - exploit WITHIN-LAUNCH PAIRING (Miller 2024): when A and B ran in the SAME
//     launch, the per-task within-launch paired difference cancels the
//     task-difficulty common-mode (the cheap ~2-3× lever) and is summarized with a
//     clustered-by-launch SE;
//   - report the REQUIRED-R to gate an effect δ from the estimated variance
//     components (+ the covariate ρ² when CUPED is used);
//   - gate ROBUSTNESS (the THOUGHT_DELIBERATIVE_K lever's target) by a bootstrap CI
//     on the variance ratio σ²_run(ON)/σ²_run(OFF) with the strict mean-guard;
//   - apply CUPED variance reduction (1−ρ²) using ONLY an UPSTREAM task-difficulty
//     proxy (the launch-temperature proxy = mean ModelCalls over SATURATED tasks),
//     with a LEAKAGE GUARD: report β raw vs adjusted and DROP the covariate if it
//     swings β beyond its own SE (mirrors gain.go's honest-prior fallback).
//
// HONEST CEILING (spec §4.3/§8). When the non-saturated tasks are coins re-flipping
// per launch, σ²_run ≈ p(1−p) is maximal and a 15pp effect needs ~100+ launches
// with no covariate help. This estimator is the SQUEEZE (a ~2× run saving on top of
// the real levers: deliberative K to shrink σ²_run at the source; more hard
// non-saturated tasks to raise T_eff; higher K) — NOT a substitute for shrinking
// σ²_run or for a temperature-controllable substrate. It does not manufacture
// signal the substrate's coin-flip variance destroys.
//
// DETERMINISM. Pure arithmetic over collected results; the ONLY randomness is a
// SEEDED bootstrap (cpyrand) for the variance-ratio CI — same input + same seed ⇒
// same verdict, bit-for-bit (CLAUDE.md headless-pure + determinism rules). No model,
// no wall clock, no I/O.

// EstMode selects the analysis pass (the --estimator knob; all default to OFF →
// byte-identical to today's ComputeSigmaR-only behaviour).
type EstMode string

const (
	// EstOff — no estimator: the report carries only the ComputeSigmaR-equivalent
	// headline (mean per-task σ_R + mean solve-rate). Byte-identical to today.
	EstOff EstMode = "off"
	// EstPaired — the §3 pure-arithmetic covariate-adjustable estimators (paired
	// within-launch differencing when A/B share launches; the cluster-robust SE;
	// the variance-component β CI; CUPED on the clean launch-temp covariate). No
	// optimizer; the default upgrade.
	EstPaired EstMode = "paired"
	// EstGLMM — reserved for the full logistic mixed-model fit (optional,
	// optimizer-backed). NOT in the minimal build; routed to EstPaired with a note.
	EstGLMM EstMode = "glmm"
)

// EstVerdict extends ruler's vocabulary (the spec §6.2 verdict set). FEASIBLE /
// NOISY-RULER / LOW-RELIABILITY / DEGENERATE are inherited in spirit; the two new
// states are the observer "not yet identified" and the leakage-guard trip.
type EstVerdict string

const (
	// VerdictFeasible — the variance components are identified AND the effect's CI
	// lower bound clears 0 AND the effect resolves the locked MDE/δ at this R.
	EstFeasible EstVerdict = "FEASIBLE"
	// VerdictNoisy — identified but the effect does NOT resolve δ at this R (the
	// required-R exceeds the observed R): collect more launches (or shrink σ²_run).
	EstNoisy EstVerdict = "NOISY-RULER"
	// VerdictDegenerate — not enough exercised data (R<2, T_eff<1, all-saturated,
	// or no config contrast). NOT a pass; the instrument has not been exercised.
	EstDegenerate EstVerdict = "DEGENERATE"
	// VerdictUnderIdentified — there IS data but the variance components are not
	// identifiable at this R/T_eff (the observer "not yet identified" state — the
	// gain.go prior-fallback analogue): a CI is reported but flagged as not
	// trustworthy; raise R / T_eff before gating.
	EstUnderIdentified EstVerdict = "UNDER-IDENTIFIED"
	// VerdictLeakageSuspected — the CUPED covariate swung β beyond its own SE → it
	// is correlated with the TREATMENT, not just the noise → it is leaking. The raw
	// (un-adjusted) β is reported; the covariate is DROPPED.
	EstLeakageSuspected EstVerdict = "LEAKAGE-SUSPECTED"
)

// --- identifiability constants (mirror regulator/gain.go's confidence gate) ---
const (
	// estMinLaunches is the minimum R to attempt variance-component identification
	// of the run effect (a sample SD needs ≥2; the run-effect CI needs more). Below
	// this the verdict is DEGENERATE. Mirrors gain.go's gainMinPairs discipline:
	// enough data to even ATTEMPT before any verdict is trusted.
	estMinLaunches = 2
	// estMinTaskEff is the minimum number of NON-SATURATED (informative) tasks the
	// run-effect can be estimated from. Saturated tasks (rate ≡ 1 or ≡ 0 every
	// launch) carry σ²_run = 0 and contribute nothing; below this the components are
	// UNDER-IDENTIFIED (spec §1.1 honesty flag — the dominant real-world limit).
	estMinTaskEff = 2
	// estBootstrapN is the number of resamples for the variance-ratio bootstrap CI
	// (the robustness gate). Fixed (deterministic with the seed); large enough for a
	// stable 90% interval on the small launch matrices realhard produces.
	estBootstrapN = 2000
	// estBootstrapSeed is the FIXED seed for the robustness bootstrap so the gate is
	// reproducible bit-for-bit (CLAUDE.md determinism). Overridable via the config.
	estBootstrapSeed = 0xE57104A7
)

// the same one-sided α=0.05 / power=0.80 multipliers ruler uses (re-declared so the
// reducer is self-contained; identical constants).
const (
	estZAlpha = 1.6448536269514722 // Φ⁻¹(0.95)
	estZBeta  = 0.8416212335729143 // Φ⁻¹(0.80)
	// estZHalf is the two-sided z for a 90% CI half-width (Φ⁻¹(0.95)); the β CI is
	// reported as a 90% interval so its one-sided lower bound is the α=0.05 keep-gate.
	estZHalf = 1.6448536269514722
)

// EstimatorConfig parameterizes the reducer. The zero value (Mode "" → off) is the
// byte-identical default. Mirrors resolveDeliberativeK's read-once discipline:
// resolved by the caller (cmd/realhard) and passed in.
type EstimatorConfig struct {
	// Mode selects the analysis pass (off | paired | glmm). "" == off.
	Mode EstMode
	// Covariates is the CUPED covariate set. The clean default is {"launch_temp"}
	// (the §2.3 upstream task-difficulty proxy). Leaky covariates (grounded,
	// tool_select, force_ground, value, model_calls) are opt-in and trigger the
	// leakage guard. Empty → no covariate (raw β).
	Covariates []string
	// CUPED enables the variance-reduction adjustment. When false, β is the raw
	// (unadjusted) estimate even if Covariates is non-empty (the leakage diagnostic
	// reports both). Defaults true when Mode != off (set by the caller).
	CUPED bool
	// Delta is the effect the gate must resolve (the required-CI half-width target).
	// 0 → ruler.DefaultClaimableLift (0.15).
	Delta float64
	// ICCFloor is the reliability floor (reused from ruler.DefaultICCFloor). 0 →
	// the default.
	ICCFloor float64
	// Robustness runs the §3b σ²_run variance-ratio bootstrap gate (the
	// deliberative-lever gate) alongside the capability β gate.
	Robustness bool
	// BootstrapSeed seeds the robustness bootstrap (0 → estBootstrapSeed). Surfaced
	// so a test can pin a specific stream; the default is fixed for reproducibility.
	BootstrapSeed uint64
}

func (c EstimatorConfig) delta() float64 {
	if c.Delta > 0 {
		return c.Delta
	}
	return ruler.DefaultClaimableLift
}

func (c EstimatorConfig) iccFloor() float64 {
	if c.ICCFloor > 0 {
		return c.ICCFloor
	}
	return ruler.DefaultICCFloor
}

func (c EstimatorConfig) bootstrapSeed() uint64 {
	if c.BootstrapSeed != 0 {
		return c.BootstrapSeed
	}
	return estBootstrapSeed
}

// EstimatorReport is the full noise-aware read. Everything is deterministic in the
// input results + config (the bootstrap is seeded).
type EstimatorReport struct {
	Mode EstMode

	// --- shape ---
	Tasks         int  // distinct task count
	TaskEff       int  // NON-SATURATED (informative) task count — the pairing benefit cap
	Launches      int  // R per arm (the smaller of the two arms when unpaired; the shared count when paired)
	Paired        bool // true when A and B share launches (within-launch differencing was used)
	HasABContrast bool // true when both an OFF and an ON config are present

	// --- capability / mean effect (§3a) ---
	// Beta is the config effect on the per-task solve RATE (ON minus OFF), on the
	// rate scale (the suite is mostly at the rate boundary so the rate scale is the
	// reported one; a log-odds β is available via the GLMM upgrade). When no AB
	// contrast is present this is 0.
	Beta float64
	// BetaCILo/BetaCIHi is the 90% CI on Beta (one-sided lower bound is the α=0.05
	// keep-gate). VALID: the SE folds in the launch random-effect variance (the
	// correctness fix vs the naive pooled SD).
	BetaCILo float64
	BetaCIHi float64
	// BetaSE is the standard error of Beta (the valid, variance-component / clustered
	// SE).
	BetaSE float64
	// BetaNaiveSE is the INVALID naive pooled-SD standard error (ignores the launch
	// random effect) — reported ONLY to demonstrate how much too-narrow it is vs
	// BetaSE. Never used for the gate.
	BetaNaiveSE float64

	// --- CUPED (§2) ---
	// BetaRaw is β before any covariate adjustment (the leakage-guard reference).
	BetaRaw float64
	// BetaAdjusted is β after CUPED (== BetaRaw when no covariate / CUPED off / the
	// guard dropped the covariate).
	BetaAdjusted float64
	// Rho2 is the fraction of run-level variance the covariate explained (ρ²); the
	// CUPED variance multiplier is (1−Rho2). 0 when no covariate.
	Rho2 float64
	// CovariateUsed lists the covariates that survived the leakage guard (the ones
	// actually applied). Empty when CUPED off or all dropped.
	CovariateUsed []string
	// CovariateDropped lists covariates the leakage guard removed (swung β beyond
	// its SE). Non-empty ⇒ Verdict may be LEAKAGE-SUSPECTED.
	CovariateDropped []string

	// --- run-effect variance (§1.1) ---
	// SigmaRunOff/SigmaRunOn is the run (launch) random-effect SD (sqrt σ²_run) per
	// config — the ±56pp modeled. SigmaRunOn is 0 when there is no ON config.
	SigmaRunOff float64
	SigmaRunOn  float64
	// MeanSigmaR is the ComputeSigmaR-equivalent headline (mean per-task σ_R) for the
	// OFF config — the regression anchor: Mode=off reproduces this exactly.
	MeanSigmaR    float64
	MeanSolveRate float64

	// --- robustness gate (§3b) ---
	// VarianceRatio is σ²_run(ON)/σ²_run(OFF) (the deliberative-lever target;
	// <1 ⇒ the lever shrank the run variance). 0 when no AB contrast / robustness off.
	VarianceRatio float64
	// VarRatioCILo/VarRatioCIHi is the bootstrap 90% CI on VarianceRatio. The
	// robustness keep-signal: CIHi < 1 (the run variance is significantly LOWER on)
	// AND the mean-guard (Beta not significantly negative).
	VarRatioCILo float64
	VarRatioCIHi float64
	// RobustnessRan is true when the §3b gate was computed.
	RobustnessRan bool

	// --- required-N (§4) ---
	// RequiredR is the launch count needed to gate the configured δ from the
	// estimated σ²_run (and ρ² when CUPED). The honest run-dominated formula.
	RequiredR float64
	// Delta echoes the gated effect (for the self-describing report).
	Delta float64

	// --- verdict ---
	Verdict EstVerdict
	// Notes carries honest caveats (under-identification reason, leakage detail).
	Notes []string
}

// Estimate is the pure reduction over a flat slice of harness RunResults — ONE
// launch's worth (a single RunSuite call, the shape sigmar.go's launchTaskRates
// consumes). A flat RunResult slice carries no launch tag, so it is exactly ONE
// launch: it groups by TaskID, reduces each task to its per-replay solve-rate +
// covariate row, and routes to EstimateMatrix as a 1-launch OFF-only matrix. The
// run-effect variance is therefore UNDER-IDENTIFIED by construction (it needs R≥2
// launches) — Estimate reports that honestly rather than a fake number; the headline
// σ_R / mean solve-rate ARE computed. For a real run-effect / β gate, collect R
// launches via RunSigmaREstimator and use EstimateData / EstimateMatrix.
func Estimate(results []RunResult, cfg EstimatorConfig) EstimatorReport {
	// stable task order (the matrix column order).
	var taskIDs []string
	caps := map[string]Capability{}
	seen := map[string]bool{}
	for _, r := range results {
		if r.Arm != ArmHarness {
			continue
		}
		if !seen[r.TaskID] {
			seen[r.TaskID] = true
			taskIDs = append(taskIDs, r.TaskID)
			caps[r.TaskID] = r.Capability
		}
	}
	sort.Strings(taskIDs)
	capSlice := make([]Capability, len(taskIDs))
	for i, id := range taskIDs {
		capSlice[i] = caps[id]
	}
	// one launch's per-task rate + covariate row.
	rates := [][]float64{launchTaskRates(results, taskIDs)}
	cov := [][]estCovariates{launchTaskCovariates(results, taskIDs)}
	return EstimateMatrix(rates, nil, cov, nil, taskIDs, capSlice, cfg)
}

// EstimateData is the convenience over the RETAINED SigmaRData (sigmar.go's
// RunSigmaREstimator output): it unpacks the OFF/ON rate + covariate matrices and
// routes to EstimateMatrix. The single-arm σ_R run (no ON arm) still produces the
// off-mode anchor + the run-effect variance; a paired run adds the β / robustness
// gates.
func EstimateData(data SigmaRData, cfg EstimatorConfig) EstimatorReport {
	return EstimateMatrix(data.RatesOff, data.RatesOn, data.CovOff, data.CovOn, data.TaskIDs, data.Caps, cfg)
}

// EstimateMatrix is the structured entry point: ratesOff[launch][task] (required)
// and ratesOn[launch][task] (optional — nil when there is no ON config). When
// paired is true the two matrices' launches are matched (A and B ran in the same
// launch on the same seed offset), enabling within-launch differencing. covOff /
// covOn carry the per-(launch,task) covariate rows aligned to the rate matrices
// (nil → no covariates available; CUPED is then skipped honestly).
//
// taskIDs/caps name the T columns (parallel to the matrices). This is the pure
// reducer the spec's diagram routes to.
func EstimateMatrix(
	ratesOff, ratesOn [][]float64,
	covOff, covOn [][]estCovariates,
	taskIDs []string, caps []Capability,
	cfg EstimatorConfig,
) EstimatorReport {
	rep := EstimatorReport{
		Mode:  normMode(cfg.Mode),
		Delta: cfg.delta(),
	}

	// --- off-mode anchor: exactly ComputeSigmaR's headline, nothing more ---
	rowsOff, meanSigmaR, meanSolveRate := ComputeSigmaR(ratesOff, taskIDs, caps)
	rep.MeanSigmaR = meanSigmaR
	rep.MeanSolveRate = meanSolveRate
	rep.Tasks = len(rowsOff)
	if len(ratesOff) > 0 {
		rep.Launches = len(ratesOff)
	}
	if rep.Mode == EstOff {
		rep.Verdict = EstDegenerate // off-mode produces no gate verdict (anchor only)
		return rep
	}

	rep.HasABContrast = len(ratesOn) > 0

	// --- shape / identifiability (mirror gain.go's confidence gate) ---
	rep.TaskEff = countInformativeTasks(ratesOff, ratesOn)
	identifiable := true
	if rep.Launches < estMinLaunches {
		identifiable = false
		rep.Notes = append(rep.Notes, fmt.Sprintf("R=%d < %d launches: run-effect not identifiable", rep.Launches, estMinLaunches))
	}
	if rep.TaskEff < estMinTaskEff {
		identifiable = false
		rep.Notes = append(rep.Notes, fmt.Sprintf("T_eff=%d < %d informative (non-saturated) tasks: variance components UNDER-IDENTIFIED (spec §1.1)", rep.TaskEff, estMinTaskEff))
	}

	// --- run-effect variance per config (§1.1; ANOVA between-launch component) ---
	rep.SigmaRunOff = math.Sqrt(runEffectVar(ratesOff))
	if rep.HasABContrast {
		rep.SigmaRunOn = math.Sqrt(runEffectVar(ratesOn))
	}

	// --- capability β (§3a) ---
	if rep.HasABContrast {
		paired := isPaired(ratesOff, ratesOn)
		rep.Paired = paired
		beta, se, naiveSE := capabilityBeta(ratesOff, ratesOn, paired)
		rep.BetaRaw = beta
		rep.Beta = beta
		rep.BetaSE = se
		rep.BetaNaiveSE = naiveSE
		rep.BetaAdjusted = beta

		// --- CUPED (§2) with the leakage guard (§2.4) ---
		if cfg.CUPED && len(cfg.Covariates) > 0 && covOff != nil {
			adj := applyCUPED(ratesOff, ratesOn, covOff, covOn, cfg.Covariates, paired, beta, se)
			rep.Rho2 = adj.rho2
			rep.CovariateUsed = adj.used
			rep.CovariateDropped = adj.dropped
			if len(adj.used) > 0 && !adj.leaked {
				rep.BetaAdjusted = adj.beta
				rep.Beta = adj.beta
				rep.BetaSE = adj.se // CUPED shrinks the SE by ~√(1−ρ²)
				// recompute the CI from the adjusted SE below.
			}
			if adj.leaked {
				rep.Notes = append(rep.Notes, fmt.Sprintf("leakage guard: covariate(s) %v swung β by more than its SE (Δβ %.4f > SE %.4f) → DROPPED; raw β reported",
					adj.dropped, adj.swing, se))
			}
		}

		// the valid 90% CI on the (possibly adjusted) β.
		rep.BetaCILo = rep.Beta - estZHalf*rep.BetaSE
		rep.BetaCIHi = rep.Beta + estZHalf*rep.BetaSE
	}

	// --- required-R (§4) ---
	rep.RequiredR = requiredR(rep.runVarForRequiredR(), rep.Rho2, cfg.delta())

	// --- robustness gate (§3b) ---
	if cfg.Robustness && rep.HasABContrast {
		rep.RobustnessRan = true
		ratio, lo, hi := varianceRatioBootstrap(ratesOff, ratesOn, estBootstrapN, cfg.bootstrapSeed())
		rep.VarianceRatio = ratio
		rep.VarRatioCILo = lo
		rep.VarRatioCIHi = hi
	}

	// --- verdict ---
	rep.Verdict = deriveEstVerdict(rep, identifiable, cfg)
	return rep
}

// runVarForRequiredR picks the run-effect variance the required-R formula uses: the
// OFF config's σ²_run (the baseline noise the gate must overcome). When an ON
// config is present and lower, the OFF variance is the conservative (larger)
// requirement — the gate sizes against the noisier arm.
func (rep EstimatorReport) runVarForRequiredR() float64 {
	v := rep.SigmaRunOff * rep.SigmaRunOff
	if v <= 0 {
		// no identifiable run effect off the OFF arm — fall back to the headline
		// mean per-task σ_R squared (a coarse but honest variance proxy) so the
		// required-R is still a number, not zero.
		v = rep.MeanSigmaR * rep.MeanSigmaR
	}
	return v
}

// --- variance-component primitives -----------------------------------------

// runEffectVar estimates the run (launch) random-effect variance σ²_run from an
// R×T rate matrix via a two-way ANOVA decomposition restricted to the INFORMATIVE
// (non-saturated) tasks. Mirrors ruler.computeICC1's MSB/MSW shape: the run effect
// is the between-LAUNCH mean square minus the residual, divided by the design
// constant (the informative task count).
//
//	σ²_run ≈ max(0, (MSB_launch − MSW) / T_eff)
//
// where MSB_launch = T_eff · Var(per-launch means over informative tasks) and MSW
// is the residual (within-cell) variance after removing the task and launch means.
// Saturated columns are dropped first (they carry σ²_run = 0 and a degenerate α_i,
// spec §1.1), so the estimate is identified only from the tasks that actually move.
// Returns 0 when fewer than 2 launches or fewer than 1 informative task.
func runEffectVar(rates [][]float64) float64 {
	r := len(rates)
	if r < 2 {
		return 0
	}
	t := len(rates[0])
	// select informative columns (not constant down the column).
	var cols []int
	for c := 0; c < t; c++ {
		if !columnConstant(rates, c) {
			cols = append(cols, c)
		}
	}
	teff := len(cols)
	if teff < 1 {
		return 0
	}

	// grand mean over the informative cells.
	var grand float64
	n := 0
	for l := 0; l < r; l++ {
		for _, c := range cols {
			grand += rates[l][c]
			n++
		}
	}
	if n == 0 {
		return 0
	}
	grand /= float64(n)

	// per-launch means (over informative tasks) and per-task means (over launches).
	launchMean := make([]float64, r)
	for l := 0; l < r; l++ {
		var s float64
		for _, c := range cols {
			s += rates[l][c]
		}
		launchMean[l] = s / float64(teff)
	}
	taskMean := make([]float64, teff)
	for ci, c := range cols {
		var s float64
		for l := 0; l < r; l++ {
			s += rates[l][c]
		}
		taskMean[ci] = s / float64(r)
	}

	// SS_launch (between-launch), SS_resid (interaction/residual after task+launch).
	var ssLaunch, ssResid float64
	for l := 0; l < r; l++ {
		d := launchMean[l] - grand
		ssLaunch += float64(teff) * d * d
	}
	for l := 0; l < r; l++ {
		for ci, c := range cols {
			resid := rates[l][c] - launchMean[l] - taskMean[ci] + grand
			ssResid += resid * resid
		}
	}
	dfLaunch := float64(r - 1)
	dfResid := float64((r - 1) * (teff - 1))
	msbLaunch := 0.0
	if dfLaunch > 0 {
		msbLaunch = ssLaunch / dfLaunch
	}
	msw := 0.0
	if dfResid > 0 {
		msw = ssResid / dfResid
	}
	// EMS for a two-way mixed model: E[MSB_launch] = σ²_resid + T_eff·σ²_run, so
	// σ²_run = (MSB_launch − MSW) / T_eff. Clamp at 0 (the ANOVA estimator goes
	// negative when the launch signal is below the residual — interpreted as no run
	// effect, exactly ruler.computeICC1's clamp discipline). The clamp uses a small
	// EPSILON floor (not exact 0) so the MSB−MSW floating-point residue of a genuinely
	// absent run effect (~1e-17 dust) reads as 0, not a spurious tiny variance.
	v := (msbLaunch - msw) / float64(teff)
	if v < runVarEps {
		v = 0
	}
	return v
}

// runVarEps is the floating-point floor below which an estimated σ²_run is treated as
// exactly 0 (numerical dust from the MSB−MSW subtraction when the run effect is
// genuinely absent — e.g. per-task-independent coin flips with no SHARED launch
// shock, the on-disk K1/K3 data). Well below any real run-effect variance (a 1pp
// shared shock is σ²≈1e-4) yet far above double-precision residue.
const runVarEps = 1e-12

// columnConstant reports whether column c is constant down all launches (a
// saturated task: rate ≡ 1 or ≡ 0 — or any single repeated value). Such a task
// carries σ²_run = 0 and is dropped from the run-effect estimate (spec §1.1).
func columnConstant(rates [][]float64, c int) bool {
	if len(rates) == 0 {
		return true
	}
	first := rates[0][c]
	for l := 1; l < len(rates); l++ {
		if math.Abs(rates[l][c]-first) > 1e-12 {
			return false
		}
	}
	return true
}

// countInformativeTasks counts columns that are non-constant in AT LEAST ONE config
// (a task that moves on either arm carries run-effect information). This is T_eff,
// the pairing-benefit cap (spec §4.4).
func countInformativeTasks(ratesOff, ratesOn [][]float64) int {
	t := 0
	if len(ratesOff) > 0 {
		t = len(ratesOff[0])
	} else if len(ratesOn) > 0 {
		t = len(ratesOn[0])
	}
	count := 0
	for c := 0; c < t; c++ {
		informative := false
		if len(ratesOff) >= 2 && !columnConstant(ratesOff, c) {
			informative = true
		}
		if !informative && len(ratesOn) >= 2 && c < len(ratesOn[0]) && !columnConstant(ratesOn, c) {
			informative = true
		}
		if informative {
			count++
		}
	}
	return count
}

// isPaired reports whether the two arms share a launch structure (same launch count
// AND same task count): the precondition for within-launch differencing.
func isPaired(ratesOff, ratesOn [][]float64) bool {
	if len(ratesOff) == 0 || len(ratesOn) == 0 {
		return false
	}
	if len(ratesOff) != len(ratesOn) {
		return false
	}
	return len(ratesOff[0]) == len(ratesOn[0])
}

// --- capability β (§3a) ----------------------------------------------------

// capabilityBeta estimates the config effect on the per-task solve rate (ON − OFF)
// and BOTH a VALID SE (folding in the launch random effect) and the INVALID naive
// pooled-SD SE (for the demonstration). Two paths:
//
//   - PAIRED (within-launch differencing, Miller 2024): per launch, the mean over
//     tasks of (ON − OFF), then β = mean of those per-launch diffs and the SE is the
//     CLUSTERED-by-launch SE (SD of the per-launch diffs / √R). This cancels the
//     task-difficulty common-mode and the per-launch shared shock cancels too (it is
//     in BOTH arms of the same launch) — the cheap ~2-3× lever.
//   - UNPAIRED: β = mean_task(rate_ON) − mean_task(rate_OFF); the VALID SE folds in
//     each arm's launch random-effect variance (σ²_run/R) PLUS the between-task
//     residual, whereas the naive SE uses only the pooled per-cell SD (ignoring that
//     a whole launch moves together → too narrow → false gates).
func capabilityBeta(ratesOff, ratesOn [][]float64, paired bool) (beta, se, naiveSE float64) {
	if paired {
		r := len(ratesOff)
		t := len(ratesOff[0])
		perLaunchDiff := make([]float64, r)
		for l := 0; l < r; l++ {
			var s float64
			for c := 0; c < t; c++ {
				s += ratesOn[l][c] - ratesOff[l][c]
			}
			perLaunchDiff[l] = s / float64(t)
		}
		beta = meanOf(perLaunchDiff)
		// clustered-by-launch SE: the per-launch diff is the cluster unit (the shared
		// launch shock is already cancelled within the diff), so SE = SD(diffs)/√R.
		se = sampleSD(perLaunchDiff) / math.Sqrt(float64(r))
		// the naive (invalid) SE: pool ALL T×R per-cell diffs as if independent — this
		// ignores the within-launch correlation and is too narrow.
		var cells []float64
		for l := 0; l < r; l++ {
			for c := 0; c < t; c++ {
				cells = append(cells, ratesOn[l][c]-ratesOff[l][c])
			}
		}
		naiveSE = sampleSD(cells) / math.Sqrt(float64(len(cells)))
		return beta, se, naiveSE
	}

	// unpaired: arm means + a variance-component SE.
	muOff, varRunOff, nOff := armMeanAndRunVar(ratesOff)
	muOn, varRunOn, nOn := armMeanAndRunVar(ratesOn)
	beta = muOn - muOff
	// VALID SE: the mean of an arm has variance σ²_run/R (the launch shock averages
	// over R launches) PLUS the residual/√(R·T). The dominant term is the run effect,
	// which the naive pooled SD omits entirely.
	seOff := math.Sqrt(varRunOff/float64(nOff) + residTermVar(ratesOff))
	seOn := math.Sqrt(varRunOn/float64(nOn) + residTermVar(ratesOn))
	se = math.Sqrt(seOff*seOff + seOn*seOn)
	// naive (invalid): pool every cell as independent.
	naiveSE = math.Sqrt(naiveCellSEsq(ratesOff) + naiveCellSEsq(ratesOn))
	return beta, se, naiveSE
}

// armMeanAndRunVar returns the arm's grand mean rate, its run-effect variance, and R.
func armMeanAndRunVar(rates [][]float64) (mu, runVar float64, r int) {
	r = len(rates)
	if r == 0 {
		return 0, 0, 0
	}
	t := len(rates[0])
	var s float64
	n := 0
	for l := 0; l < r; l++ {
		for c := 0; c < t; c++ {
			s += rates[l][c]
			n++
		}
	}
	if n > 0 {
		mu = s / float64(n)
	}
	runVar = runEffectVar(rates)
	return mu, runVar, r
}

// residTermVar is the residual (between-task + within) contribution to an arm-mean
// SE, scaled by 1/(R·T) — the part that DOES average down within a launch. Small
// relative to the run term but kept for a faithful valid SE.
func residTermVar(rates [][]float64) float64 {
	r := len(rates)
	if r == 0 {
		return 0
	}
	t := len(rates[0])
	if r*t == 0 {
		return 0
	}
	// pooled per-cell variance around the per-task means (the within-task spread).
	var ss float64
	n := 0
	for c := 0; c < t; c++ {
		col := make([]float64, r)
		for l := 0; l < r; l++ {
			col[l] = rates[l][c]
		}
		m := meanOf(col)
		for l := 0; l < r; l++ {
			d := rates[l][c] - m
			ss += d * d
			n++
		}
	}
	if n == 0 {
		return 0
	}
	pooled := ss / float64(n)
	return pooled / float64(r*t)
}

// naiveCellSEsq is the INVALID pooled-SD arm-mean variance: treat every one of the
// R×T cells as an independent draw, SE² = Var(cells)/(R·T). This is the too-narrow
// estimator the valid SE corrects (it omits the launch random effect entirely).
func naiveCellSEsq(rates [][]float64) float64 {
	var cells []float64
	for l := range rates {
		cells = append(cells, rates[l]...)
	}
	n := len(cells)
	if n < 2 {
		return 0
	}
	return sampleVar(cells) / float64(n)
}

// sampleVar (n-1) — local mirror of ruler.sampleVar (unexported there).
func sampleVar(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	m := meanOf(xs)
	var ss float64
	for _, x := range xs {
		d := x - m
		ss += d * d
	}
	return ss / float64(n-1)
}

// --- CUPED (§2) ------------------------------------------------------------

// estCovariates is the per-(launch,task) covariate row (the values the suite already
// collects). Aligned to the rate matrices.
type estCovariates struct {
	modelCalls  float64
	grounded    float64
	value       float64
	toolSelect  float64
	forceGround float64
}

// cupedResult is the outcome of the covariate adjustment + leakage guard.
type cupedResult struct {
	beta    float64  // the adjusted β (== raw when leaked/empty)
	se      float64  // the adjusted SE (raw·√(1−ρ²) when applied)
	rho2    float64  // the variance fraction the covariate explained
	used    []string // covariates applied
	dropped []string // covariates the guard removed
	leaked  bool     // the guard tripped
	swing   float64  // |β_adj − β_raw| (the leakage magnitude)
}

// applyCUPED computes the §2.3 launch-temperature proxy (the only CLEAN covariate)
// or the requested leaky ones, adjusts β, and applies the §2.4 leakage guard.
//
// THE FROZEN-θ FORM (the standard CUPED guard, spec §2.4). θ is estimated on the
// OFF arm ONLY — Cov(y_OFF, x_OFF)/Var(x_OFF) — and FROZEN before it is applied to
// BOTH arms' launch outcomes: ỹ_l = y_l − θ·(x_l − x̄). This is load-bearing:
//   - with a frozen θ the adjusted launch-mean GENUINELY differs from the raw mean
//     (an IN-SAMPLE re-fit θ makes the residual mean identically the raw mean — no
//     reduction, no detectable leak — the trap a naive CUPED falls into);
//   - E[ỹ] = E[y] (θ is treatment-independent by construction since it is fit on the
//     OFF arm's NOISE relation), so β stays unbiased;
//   - Var(β̃) = Var(β̂)·(1−ρ²) where ρ² is the variance the FROZEN covariate explains.
//
// The LEAKAGE GUARD (§2.4): if the frozen-θ-adjusted β swings beyond the RAW β's SE,
// the covariate is correlated with the TREATMENT (it differs systematically between
// arms beyond the noise it strips) → it is leaking → DROP it, report raw β. This
// mirrors gain.go's honest-prior fallback.
func applyCUPED(
	ratesOff, ratesOn [][]float64,
	covOff, covOn [][]estCovariates,
	names []string,
	paired bool,
	betaRaw, seRaw float64,
) cupedResult {
	res := cupedResult{beta: betaRaw, se: seRaw}

	// θ is FROZEN on the OFF arm (the treatment-independent fit): the per-launch OFF
	// outcome regressed on the per-launch OFF covariate.
	yOffLaunch := launchOutcomeVector(ratesOff, nil, false) // OFF per-launch mean
	if len(yOffLaunch) < estMinLaunches {
		return res // not enough launches to fit θ
	}

	// The A/B CUPED adjustment to β (the standard form):
	//   β̃ = (ȳ_ON − θ·(x̄_ON − x̄)) − (ȳ_OFF − θ·(x̄_OFF − x̄))
	//     = (ȳ_ON − ȳ_OFF) − θ·(x̄_ON − x̄_OFF)
	// so the SWING off the raw β is exactly θ·(x̄_ON − x̄_OFF). That swing is non-zero
	// EXACTLY WHEN the treatment moved the covariate's mean (x̄_ON ≠ x̄_OFF) — i.e. the
	// covariate is treatment-contaminated = LEAKING. A clean (treatment-orthogonal)
	// covariate has x̄_ON ≈ x̄_OFF, so no swing and only the variance reduction. This
	// is the mechanism the §2.4 guard keys on.
	adjBeta := betaRaw
	totalRho2 := 0.0
	residOff := append([]float64(nil), yOffLaunch...)
	for _, name := range names {
		xOff := covariateLaunchVector(name, ratesOff, covOff, covOn, paired)
		if xOff == nil || len(xOff) != len(residOff) {
			continue // covariate unavailable / mis-shaped → skip honestly
		}
		// FREEZE θ on the OFF-arm noise relation.
		theta, _ := cupedTheta(residOff, xOff)
		if math.IsNaN(theta) || math.IsInf(theta, 0) {
			continue
		}
		// the ON-arm covariate mean (the leak probe): when an ON covariate matrix is
		// present, compute its per-launch covariate the SAME way; else it equals the
		// OFF mean (no detectable leak, the conservative read).
		xbarOff := meanOf(xOff)
		xbarOn := xbarOff
		if covOn != nil {
			xOn := covariateLaunchVector(name, ratesOn, covOn, covOff, paired)
			if xOn != nil && len(xOn) == len(xOff) {
				xbarOn = meanOf(xOn)
			}
		}
		// apply the A/B adjustment to β.
		adjBeta -= theta * (xbarOn - xbarOff)
		// strip the explained variance from the OFF residual (for ρ²).
		newOff := make([]float64, len(residOff))
		for i := range residOff {
			newOff[i] = residOff[i] - theta*(xOff[i]-xbarOff)
		}
		residOff = newOff
		res.used = append(res.used, name)
	}

	if len(res.used) == 0 {
		return res // nothing applied
	}

	// ρ² = the fraction of the OFF-arm outcome variance the frozen covariate stripped
	// (the legitimate, treatment-independent variance reduction).
	totalRho2 = 1 - sampleVar(residOff)/maxEps(sampleVar(yOffLaunch))
	if totalRho2 < 0 {
		totalRho2 = 0
	}
	if totalRho2 > 1 {
		totalRho2 = 1
	}
	res.rho2 = totalRho2
	res.swing = math.Abs(adjBeta - betaRaw)

	// --- the leakage guard (§2.4) ---
	// if the covariate swung β beyond the RAW β's SE, the treatment moved the
	// covariate's mean (x̄_ON ≠ x̄_OFF) → it is leaking → DROP, report raw.
	if seRaw > 0 && res.swing > seRaw {
		res.leaked = true
		res.dropped = res.used
		res.used = nil
		res.beta = betaRaw
		res.se = seRaw
		res.rho2 = 0
		return res
	}

	res.beta = adjBeta
	// CUPED shrinks the SE by √(1−ρ²).
	res.se = seRaw * math.Sqrt(1-totalRho2)
	return res
}

// launchOutcomeVector builds the per-launch capability outcome used by CUPED: when
// paired, the within-launch mean diff (ON − OFF) per launch; else the OFF arm's
// per-launch mean rate (the single-arm signal whose run-to-run swing CUPED strips).
func launchOutcomeVector(ratesOff, ratesOn [][]float64, paired bool) []float64 {
	r := len(ratesOff)
	if r == 0 {
		return nil
	}
	t := len(ratesOff[0])
	out := make([]float64, r)
	for l := 0; l < r; l++ {
		var s float64
		for c := 0; c < t; c++ {
			if paired && l < len(ratesOn) {
				s += ratesOn[l][c] - ratesOff[l][c]
			} else {
				s += ratesOff[l][c]
			}
		}
		out[l] = s / float64(t)
	}
	return out
}

// covariateLaunchVector builds the per-launch covariate value for the named field.
// The CLEAN default "launch_temp" (spec §2.3) = the mean ModelCalls over SATURATED
// tasks (tasks that solve every launch → their telemetry reflects the launch's
// shock, not a task's success → no outcome leak). The leaky names use the per-launch
// mean of that covariate over ALL tasks (treatment-contaminated → the guard exists
// to catch them). Returns nil when the covariates are unavailable.
func covariateLaunchVector(name string, ratesOff [][]float64, covOff, covOn [][]estCovariates, paired bool) []float64 {
	if covOff == nil {
		return nil
	}
	r := len(covOff)
	if r == 0 {
		return nil
	}
	out := make([]float64, r)
	switch name {
	case "launch_temp", "":
		// mean ModelCalls over the SATURATED tasks (columns constant down the OFF
		// matrix at rate 1.0 OR 0.0 — always-solve/always-fail tasks). These are the
		// outcome-independent launch-hotness probes.
		satCols := saturatedColumns(ratesOff)
		if len(satCols) == 0 {
			// no saturated task to probe → fall back to the per-launch mean ModelCalls
			// over all tasks (weaker, but still upstream-ish effort proxy).
			satCols = allColumns(covOff)
		}
		for l := 0; l < r; l++ {
			var s float64
			n := 0
			for _, c := range satCols {
				if c < len(covOff[l]) {
					s += covOff[l][c].modelCalls
					n++
				}
			}
			if n > 0 {
				out[l] = s / float64(n)
			}
		}
		return out
	case "model_calls":
		return perLaunchMean(covOff, func(c estCovariates) float64 { return c.modelCalls })
	case "grounded":
		return perLaunchMean(covOff, func(c estCovariates) float64 { return c.grounded })
	case "value":
		return perLaunchMean(covOff, func(c estCovariates) float64 { return c.value })
	case "tool_select":
		return perLaunchMean(covOff, func(c estCovariates) float64 { return c.toolSelect })
	case "force_ground":
		return perLaunchMean(covOff, func(c estCovariates) float64 { return c.forceGround })
	default:
		return nil
	}
}

// saturatedColumns returns the indices of columns that are constant down the matrix
// (the saturated tasks — the clean launch-temp probes).
func saturatedColumns(rates [][]float64) []int {
	if len(rates) < 2 {
		return nil
	}
	t := len(rates[0])
	var out []int
	for c := 0; c < t; c++ {
		if columnConstant(rates, c) {
			out = append(out, c)
		}
	}
	return out
}

func allColumns(cov [][]estCovariates) []int {
	if len(cov) == 0 {
		return nil
	}
	t := len(cov[0])
	out := make([]int, t)
	for i := range out {
		out[i] = i
	}
	return out
}

func perLaunchMean(cov [][]estCovariates, get func(estCovariates) float64) []float64 {
	r := len(cov)
	out := make([]float64, r)
	for l := 0; l < r; l++ {
		var s float64
		n := 0
		for c := range cov[l] {
			s += get(cov[l][c])
			n++
		}
		if n > 0 {
			out[l] = s / float64(n)
		}
	}
	return out
}

// cupedTheta returns θ = Cov(x,y)/Var(x) and ρ² = Corr(x,y)² (the CUPED variance
// fraction). Degenerate (Var(x)==0) → θ=0, ρ²=0 (a useless covariate).
func cupedTheta(y, x []float64) (theta, rho2 float64) {
	n := len(x)
	if n < 2 || len(y) != n {
		return 0, 0
	}
	mx, my := meanOf(x), meanOf(y)
	var cov, vx, vy float64
	for i := 0; i < n; i++ {
		dx := x[i] - mx
		dy := y[i] - my
		cov += dx * dy
		vx += dx * dx
		vy += dy * dy
	}
	if vx <= 0 {
		return 0, 0
	}
	theta = cov / vx
	if vy <= 0 {
		return theta, 0
	}
	rho2 = (cov * cov) / (vx * vy)
	if rho2 > 1 {
		rho2 = 1
	}
	if rho2 < 0 {
		rho2 = 0
	}
	return theta, rho2
}

func maxEps(v float64) float64 {
	if v < 1e-12 {
		return 1e-12
	}
	return v
}

// --- required-R (§4) -------------------------------------------------------

// requiredR is the launch count to gate an effect δ from the run-effect variance,
// reduced by the covariate (1−ρ²). The honest run-dominated formula (spec §4.1):
//
//	R ≈ 2·(z_α+z_β)²·σ²_run·(1−ρ²)/δ²
//
// Monotone: rises with σ²_run, falls with ρ², falls with δ. Returns 0 when σ²_run
// is 0 (a robust arm needs no launches to gate any δ) or δ ≤ 0.
func requiredR(runVar, rho2, delta float64) float64 {
	if delta <= 0 || runVar <= 0 {
		return 0
	}
	if rho2 < 0 {
		rho2 = 0
	}
	if rho2 > 1 {
		rho2 = 1
	}
	z := estZAlpha + estZBeta
	return 2 * z * z * runVar * (1 - rho2) / (delta * delta)
}

// --- robustness gate (§3b) -------------------------------------------------

// varianceRatioBootstrap computes the variance ratio σ²_run(ON)/σ²_run(OFF) and a
// SEEDED bootstrap 90% CI on it. The bootstrap resamples WHOLE LAUNCHES (preserving
// the within-launch task correlation — the run effect is a launch-level shock, so a
// per-cell resample would destroy it), recomputing runEffectVar on each resample.
// The CI is the 5th/95th percentile of the resampled ratios. Deterministic: the
// cpyrand stream is seeded. A ratio CI with CIHi < 1 is the robustness keep-signal
// (ON's run variance is significantly lower); CILo > 1 is the opposite (ON higher).
func varianceRatioBootstrap(ratesOff, ratesOn [][]float64, nBoot int, seed uint64) (ratio, ciLo, ciHi float64) {
	vOff := runEffectVar(ratesOff)
	vOn := runEffectVar(ratesOn)
	if vOff <= 0 {
		// degenerate denominator: report a ratio of 1 (no resolvable change) with a
		// wide CI so the gate cannot fire (honest: an all-robust OFF arm has no run
		// variance to reduce).
		return 1, 0, math.Inf(1)
	}
	ratio = vOn / vOff

	rng := cpyrand.New(seed)
	rOff := len(ratesOff)
	rOn := len(ratesOn)
	if rOff < 2 || rOn < 2 {
		return ratio, 0, math.Inf(1)
	}
	ratios := make([]float64, 0, nBoot)
	for b := 0; b < nBoot; b++ {
		resOff := resampleLaunches(ratesOff, rng)
		resOn := resampleLaunches(ratesOn, rng)
		bOff := runEffectVar(resOff)
		bOn := runEffectVar(resOn)
		if bOff <= 0 {
			continue // skip a degenerate resample (no denominator)
		}
		ratios = append(ratios, bOn/bOff)
	}
	if len(ratios) == 0 {
		return ratio, 0, math.Inf(1)
	}
	sort.Float64s(ratios)
	ciLo = percentile(ratios, 0.05)
	ciHi = percentile(ratios, 0.95)
	return ratio, ciLo, ciHi
}

// resampleLaunches draws len(rates) launches WITH REPLACEMENT (the cluster
// bootstrap unit is the whole launch row, preserving within-launch correlation).
func resampleLaunches(rates [][]float64, rng *cpyrand.Random) [][]float64 {
	r := len(rates)
	out := make([][]float64, r)
	for i := 0; i < r; i++ {
		idx := rng.Intn(r)
		out[i] = rates[idx]
	}
	return out
}

// percentile returns the p-quantile (0..1) of a SORTED slice (nearest-rank,
// clamped). xs must be sorted ascending.
func percentile(xs []float64, p float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return xs[0]
	}
	if p >= 1 {
		return xs[n-1]
	}
	idx := int(p * float64(n))
	if idx >= n {
		idx = n - 1
	}
	return xs[idx]
}

// --- verdict ---------------------------------------------------------------

// deriveEstVerdict applies the gate, mirroring ruler.deriveVerdict's ordering
// (DEGENERATE first, then the identifiability / leakage / resolution clauses).
func deriveEstVerdict(rep EstimatorReport, identifiable bool, cfg EstimatorConfig) EstVerdict {
	// DEGENERATE: no exercised data / no contrast to gate.
	if rep.Launches < estMinLaunches || rep.TaskEff < 1 {
		return EstDegenerate
	}
	if !rep.HasABContrast && !cfg.Robustness {
		// no AB contrast and no robustness gate requested: there is no effect to gate
		// (single-arm characterization only) — honest DEGENERATE.
		return EstDegenerate
	}
	// LEAKAGE-SUSPECTED: the guard tripped on a covariate.
	if len(rep.CovariateDropped) > 0 {
		return EstLeakageSuspected
	}
	// UNDER-IDENTIFIED: there is data but the variance components are not identifiable
	// (the observer "not yet identified" — gain.go prior-fallback analogue).
	if !identifiable {
		return EstUnderIdentified
	}
	// FEASIBLE vs NOISY: the capability gate resolves δ when the required-R is within
	// the observed R AND the CI lower bound clears 0. Otherwise NOISY (collect more).
	if rep.HasABContrast {
		if rep.RequiredR <= float64(rep.Launches) && rep.BetaCILo > 0 {
			return EstFeasible
		}
		return EstNoisy
	}
	// robustness-only path: FEASIBLE iff the ratio CI excludes 1 in the lower
	// direction (ON significantly lower); else NOISY.
	if rep.RobustnessRan {
		if rep.VarRatioCIHi < 1 {
			return EstFeasible
		}
		return EstNoisy
	}
	return EstNoisy
}

func normMode(m EstMode) EstMode {
	switch m {
	case EstPaired:
		return EstPaired
	case EstGLMM:
		// the GLMM upgrade is not in the minimal build; route to the paired
		// pure-arithmetic estimators (a strict subset of the GLMM's targets).
		return EstPaired
	default:
		return EstOff
	}
}

// --- render ----------------------------------------------------------------

// Render produces the plain-text estimator report (no emoji, box-drawing only).
func (rep EstimatorReport) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("NOISE-AWARE MEASUREMENT ESTIMATOR (realhard)\n")
	w("mode: %-6s   tasks: %d   T_eff(informative): %d   launches(R): %d   paired: %v\n",
		rep.Mode, rep.Tasks, rep.TaskEff, rep.Launches, rep.Paired)
	w("%s\n", strings.Repeat("=", 72))

	if rep.Mode == EstOff {
		w("ESTIMATOR OFF — ComputeSigmaR headline only (byte-identical anchor):\n")
		w("  mean per-task σ_R : %.4f\n", rep.MeanSigmaR)
		w("  mean solve-rate   : %.4f\n", rep.MeanSolveRate)
		return b.String()
	}

	w("VERDICT: %s\n\n", rep.Verdict)

	if rep.HasABContrast {
		w("CAPABILITY EFFECT (β = config effect on solve-rate, ON − OFF)\n")
		w("  β              : %+.4f\n", rep.Beta)
		w("  90%% CI         : [%+.4f, %+.4f]   (one-sided lower bound is the α=0.05 keep-gate)\n", rep.BetaCILo, rep.BetaCIHi)
		w("  SE (VALID)     : %.4f   (folds in the launch random effect — the correctness fix)\n", rep.BetaSE)
		w("  SE (naive pool): %.4f   (INVALID: ignores σ²_run → too narrow → false gates)\n", rep.BetaNaiveSE)
		if rep.BetaNaiveSE > 0 {
			w("  -> the naive SE is %.2fx too narrow; its CI would falsely fire.\n", rep.BetaSE/rep.BetaNaiveSE)
		}
		w("\n")
	}

	if len(rep.CovariateUsed) > 0 || len(rep.CovariateDropped) > 0 {
		w("CUPED COVARIATE ADJUSTMENT\n")
		w("  β raw          : %+.4f\n", rep.BetaRaw)
		w("  β adjusted     : %+.4f\n", rep.BetaAdjusted)
		if len(rep.CovariateUsed) > 0 {
			w("  covariates used: %s   (ρ² = %.3f → variance ×(1−ρ²) = %.3f)\n",
				strings.Join(rep.CovariateUsed, ","), rep.Rho2, 1-rep.Rho2)
		}
		if len(rep.CovariateDropped) > 0 {
			w("  DROPPED (leak) : %s   (swung β beyond its SE — treatment-contaminated)\n",
				strings.Join(rep.CovariateDropped, ","))
		}
		w("\n")
	}

	w("RUN-EFFECT VARIANCE (σ_run = sqrt σ²_run — the modeled ±56pp launch shock)\n")
	w("  σ_run (OFF)    : %.4f\n", rep.SigmaRunOff)
	if rep.HasABContrast {
		w("  σ_run (ON)     : %.4f\n", rep.SigmaRunOn)
	}
	w("\n")

	if rep.RobustnessRan {
		w("ROBUSTNESS GATE (σ²_run ratio ON/OFF — the deliberative-K lever's target)\n")
		w("  variance ratio : %.4f   (<1 ⇒ ON shrank the run-to-run variance)\n", rep.VarianceRatio)
		w("  bootstrap 90%% CI: [%.4f, %s]   (CIHi<1 ⇒ significant robustness gain)\n",
			rep.VarRatioCILo, fmtMaybeInf(rep.VarRatioCIHi))
		w("\n")
	}

	w("REQUIRED-N (launches to gate δ=%.3f from the estimated σ²_run)\n", rep.Delta)
	w("  required R     : %.1f   (observed R = %d)\n", rep.RequiredR, rep.Launches)
	if rep.RequiredR > float64(rep.Launches) {
		w("  -> UNDER-POWERED at this R: collect more launches OR shrink σ²_run at the source (deliberative K).\n")
	}
	w("\n")

	if len(rep.Notes) > 0 {
		w("NOTES (honest caveats)\n")
		for _, n := range rep.Notes {
			w("  - %s\n", n)
		}
	}
	return b.String()
}

func fmtMaybeInf(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return fmt.Sprintf("%.4f", v)
}
