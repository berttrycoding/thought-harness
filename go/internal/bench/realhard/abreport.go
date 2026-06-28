package realhard

import (
	"fmt"
	"sort"
	"strings"
)

// abreport.go — the runnable bare-vs-harness A/B MEASUREMENT (design doc §7.2 the A/B
// arm + §7.3 pass^k + §7.4 METR time-horizon + §6 the sub-agent guard). This is the
// ASSEMBLY layer that wires the already-built instruments into ONE report the
// cmd/realhard --ab mode emits:
//
//   - the bare -> harness LIFT (the headroom proof: does the scaffold ADD over the raw
//     model?), per-arm + per-capability — from report.go's Reduce/Report.
//   - the pass@1 vs pass^k RELIABILITY contrast (tau2-bench brittleness) over the
//     harness arm — from bernoulli.go's EstimateBernoulli (MeanPass1/MeanPassK).
//   - the SUB-AGENT GUARD verdict (the team-harness arm vs the single-strong baseline:
//     does the sub-agent layer add value or hold the expert back?) — from
//     subagentguard.go's CheckSubAgentGuard, fed the single-strong + harness counts.
//   - the METR-style TIME-HORIZON (50%-reliable task length, the autonomy axis) over
//     the harness arm's per-task solve-rate — from timehorizon.go's TimeHorizon, placed
//     on the per-task human-minutes estimate (humanMinutesFor).
//
// PURE CONTROL: the reducers are closed-form arithmetic over the per-run counts the
// suite already collects — no model, no RNG, no clock. The only nondeterminism is the
// upstream episodes (the model). RunAB drives the suite (which DOES call the backend),
// then reduces; reduceAB is the pure reduction the mechanism test exercises offline.

// ABConfig parameterizes a bare-vs-harness A/B run.
type ABConfig struct {
	Factory     BackendFactory
	Replays     int             // K replays per task per arm
	SeedBase    int64           // per-replay seed = SeedBase + replay
	MaxTicks    int             // harness episode cap (0 -> DefaultMaxTicks)
	Concurrency int             // task-level parallelism (1 = serial)
	TaskFilter  string          // FilterTasks substrings ("" = all)
	OnResult    func(RunResult) // optional progress callback (nil ok)
	// PassK is the pass^k reliability k (the tau2-bench brittleness read). 0 ⇒ the
	// pass^k section reports pass@1 only (no decay read).
	PassK int
	// Substrate is the substrate label for the report header (e.g. "claude:sonnet+haiku").
	Substrate string
	// Tasks OVERRIDES the bank (SuiteConfig.Tasks): non-nil runs these tasks instead of
	// the built-in Tasks() — the hook the offline instrument-validation A/B
	// (InstrumentValidationTasks) and a converted EXTERNAL bank run through. Nil => the
	// built-in realhard suite (byte-identical to the pre-flag A/B).
	Tasks []Task
	// SingleStrong toggles the SINGLE-STRONG baseline arm (RunSingleStrong: the full engine
	// with sub-agent fan-out disabled), the third arm that makes the sub-agent-beats-best-member
	// guard return a REAL verdict. Default true (the full three-arm A/B). Set false (cmd/realhard
	// --no-single-strong) to run ONLY the bare + harness arms — the report then renders the
	// HEADLINE lift / pass^k / METR honestly with the guard reported NOT-APPLICABLE, so the
	// two-arm measurement completes + writes its report regardless of the single-strong arm
	// (e.g. when it crashes on the live substrate, or to spend tokens on the headline only).
	SingleStrong bool
}

// ABReport is the assembled bare-vs-harness measurement.
type ABReport struct {
	Substrate string
	Replays   int

	// Lift is the headroom proof (per-arm + per-capability solve-rate + bare->harness lift).
	Lift Report
	// Reliability carries the pass@1 vs pass^k contrast over the HARNESS arm (the
	// BernoulliReport's MeanPass1/MeanPassK/PassKTrusted + per-task pass^k). PassKAt=0
	// when PassK was not requested.
	Reliability BernoulliReport
	// Guard is the sub-agent-beats-best-member verdict (harness team vs single-strong
	// baseline). Verdict is NOT-APPLICABLE when the single-strong arm was absent.
	Guard SubAgentGuard
	// Horizon is the METR-style 50%-reliable time-horizon over the harness arm.
	Horizon THResult
	// Failures lists every arm-run that errored/panicked on this run (nil on a clean run).
	// A PARTIAL failure (some cells failed, others completed) still produces a report — the
	// failures are recorded here and rendered as an honest banner so the lift/guard numbers
	// are read with the right caveat (the failed cells scored FAIL, shrinking the affected
	// arm's solve-rate; never silently dropped).
	Failures []ArmError
}

// RunAB runs the full bare-vs-harness A/B: bare + harness + single-strong arms over K
// replays, then reduces to the assembled ABReport. The single-strong arm is included so
// the sub-agent guard returns a REAL verdict (not NOT-APPLICABLE). It drives the suite
// (which calls the backend) and is the only non-pure entry — reduceAB does the closed-
// form reduction the mechanism test pins.
//
// ROBUSTNESS: a per-arm error/panic on the live substrate does NOT abort the run — the
// suite records each failure (returned as []ArmError) and keeps going, so the report is
// ALWAYS reduced from whatever completed (a single bad cell never throws away the run).
// The error return is non-nil ONLY on a total wipe-out (every arm failed → no signal).
// The caller logs the armErrs and annotates the report with the partial-failure note.
func RunAB(cfg ABConfig) (ABReport, []ArmError, error) {
	results, armErrs, err := RunSuite(SuiteConfig{
		Factory:             cfg.Factory,
		Replays:             cfg.Replays,
		SeedBase:            cfg.SeedBase,
		MaxTicks:            cfg.MaxTicks,
		Concurrency:         cfg.Concurrency,
		TaskFilter:          cfg.TaskFilter,
		Tasks:               cfg.Tasks, // nil => the built-in bank; non-nil => an override bank (instrument-validation / external)
		OnResult:            cfg.OnResult,
		IncludeSingleStrong: cfg.SingleStrong, // the guard needs the baseline arm; --no-single-strong runs the 2-arm headline only
	})
	if err != nil {
		// total wipe-out: every arm failed — surface the errors so the caller can print
		// the reasons (the report would be empty/meaningless).
		return ABReport{}, armErrs, err
	}
	rep := reduceAB(results, cfg.Tasks, cfg.TaskFilter, cfg.PassK, cfg.Substrate)
	rep.Failures = armErrs // partial-completion failures (nil on a clean run)
	return rep, armErrs, nil
}

// reduceAB is the PURE reduction over the flat per-run results into the assembled
// ABReport: the lift (Reduce), the harness arm's pass^k reliability (EstimateBernoulli),
// the sub-agent guard (single-strong vs harness), and the METR time-horizon (harness
// per-task solve-rate placed on the human-minutes estimate). Deterministic — no model,
// no RNG, no clock. The mechanism test drives this directly on planted/test-double
// results so the wiring is provable offline.
func reduceAB(results []RunResult, bank []Task, taskFilter string, passK int, substrate string) ABReport {
	rep := ABReport{Substrate: substrate}

	// the lift (per-arm + per-capability) — report.go.
	rep.Lift = Reduce(results)
	rep.Replays = rep.Lift.Replays

	// per-task (solved,K) counts per arm — aligned to the SELECTED task order (the same
	// bank + filter the suite ran), so the guard pairs single-strong vs harness by index
	// and the time-horizon places them on the right human-minutes. A nil bank uses the
	// built-in Tasks() (byte-identical to the pre-flag A/B).
	src := bank
	if src == nil {
		src = Tasks()
	}
	tasks := FilterTasks(src, taskFilter)
	taskIDs := make([]string, len(tasks))
	caps := make([]Capability, len(tasks))
	minutes := make([]float64, len(tasks))
	for i, tk := range tasks {
		taskIDs[i] = tk.ID
		caps[i] = tk.Capability
		minutes[i] = humanMinutesFor(tk)
	}
	harnCounts := launchTaskCountsArm(results, taskIDs, caps, ArmHarness)
	singleCounts := launchTaskCountsArm(results, taskIDs, caps, ArmSingleStrong)

	// pass^k reliability over the HARNESS arm — bernoulli.go. The Bernoulli read gives
	// MeanPass1 / MeanPassK and the per-task pass^k (the brittleness axis).
	rep.Reliability = EstimateBernoulli(harnCounts, nil, nil, nil, nil, BernoulliConfig{
		Mode:  EstBernOn,
		PassK: passK,
	})

	// sub-agent guard: the harness (team) arm must BEAT the single-strong baseline.
	// CheckSubAgentGuard returns NOT-APPLICABLE if the single-strong arm produced no
	// counts (K==0 on every task) — so a run without --ab still surfaces the unguarded
	// gap honestly.
	rep.Guard = CheckSubAgentGuard(nonEmptyCounts(singleCounts), harnCounts)

	// METR time-horizon over the harness arm's per-task solve-rate, placed on the
	// human-minutes estimate — timehorizon.go.
	thTasks := make([]THTask, 0, len(harnCounts))
	for i, c := range harnCounts {
		if c.K <= 0 {
			continue
		}
		thTasks = append(thTasks, THTask{
			TaskID:     c.TaskID,
			Capability: c.Capability,
			HumanMin:   minutes[i],
			PHat:       float64(c.Solved) / float64(c.K),
			K:          c.K,
		})
	}
	rep.Horizon = TimeHorizon(thTasks)

	return rep
}

// nonEmptyCounts returns the single-strong counts only if the arm actually ran (some
// task has K>0); otherwise nil, so CheckSubAgentGuard reports NOT-APPLICABLE rather than
// pairing against an all-zero baseline (which would read as a spurious team-beats win).
func nonEmptyCounts(counts []BernTaskInput) []BernTaskInput {
	for _, c := range counts {
		if c.K > 0 {
			return counts
		}
	}
	return nil
}

// humanMinutesFor returns the estimated skilled-human task length (minutes) for the METR
// time-horizon x-axis. The realhard Task has no per-task minutes field, so this is a
// DOCUMENTED per-capability difficulty heuristic (the four families are pitched at
// distinct human-effort tiers): a multi-hop grounding chain or a long-horizon-
// consistency task takes a skilled human materially longer than an anti-confabulation
// decline. It is deterministic (a pure switch on the capability + a stable per-ID nudge
// so same-capability tasks are not all pinned to one x — the WLS fit needs spread). It
// is a HEURISTIC placement, not a measured human-time study — reported as such; the
// horizon NUMBER inherits that caveat (the timehorizon.go Render already flags WLS-on-
// logit honesty). The ranking across families is the load-bearing signal.
func humanMinutesFor(t Task) float64 {
	// an explicit per-task length (a converted external bank's human_min, or the offline
	// instrument-validation set) wins — it is the precise METR x-position the bank author
	// measured/estimated. 0 (every built-in suite task) falls through to the per-capability
	// heuristic below, so the built-in suite is byte-identical.
	if t.HumanMin > 0 {
		return t.HumanMin
	}
	var base float64
	switch t.Capability {
	case CapAntiConfabulation:
		base = 3 // recognize under-specification + decline: quick for a careful human
	case CapAdaptiveBacktracking:
		base = 10 // spot the dead-end + replan
	case CapMultiHopGrounding:
		base = 20 // chain 3+ grounded reads
	case CapLongHorizonConsistency:
		base = 40 // hold a long consistent chain without drift
	default:
		base = 10
	}
	// a small, deterministic per-ID spread (±~20% of base) so same-family tasks land on
	// distinct x-positions (the logistic fit needs >=2 distinct minutes to identify a
	// slope). Derived from a stable hash of the ID — no RNG, no clock.
	nudge := 1.0 + 0.2*(float64(idHashTH(t.ID)%11)-5)/5.0 // in [0.8, 1.2]
	return base * nudge
}

// idHashTH is a tiny stable FNV-1a-style hash of the task ID (deterministic; used only
// to spread same-capability tasks across distinct time-horizon x-positions).
func idHashTH(id string) int {
	const prime = 16777619
	h := 2166136261
	for i := 0; i < len(id); i++ {
		h ^= int(id[i])
		h *= prime
	}
	if h < 0 {
		h = -h
	}
	return h
}

// Render produces the assembled plain-text bare-vs-harness A/B report (no emoji,
// box-drawing only). It stacks the four instrument reports under one header so a single
// invocation SHOWS the harness-vs-bare delta + reliability + guard + autonomy axis.
func (r ABReport) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("%s\n", strings.Repeat("#", 72))
	w("# BARE-vs-HARNESS A/B MEASUREMENT — realhard suite\n")
	w("# substrate: %s   replays(K): %d\n", r.Substrate, r.Replays)
	w("%s\n\n", strings.Repeat("#", 72))

	// 0) PARTIAL-FAILURE banner (honest): if any arm-run errored/panicked, name them up
	// front so the lift/guard numbers below are read with the caveat that the failed cells
	// scored FAIL (they were NOT silently dropped — that would inflate the surviving arm).
	if len(r.Failures) > 0 {
		w("%s\n", strings.Repeat("!", 72))
		w("PARTIAL COMPLETION — %d arm-run(s) FAILED (recorded as FAIL, not dropped):\n", len(r.Failures))
		for _, f := range r.Failures {
			w("  - %s\n", f.Error())
		}
		w("%s\n\n", strings.Repeat("!", 72))
	}

	// 1) the headroom proof (lift, per-arm + per-capability).
	w("%s\n\n", r.Lift.Render(r.Substrate))

	// 2) pass@1 vs pass^k reliability (the brittleness axis).
	w("%s\n", strings.Repeat("=", 72))
	w("RELIABILITY — pass@1 vs pass^k (tau2-bench brittleness; HARNESS arm)\n")
	w("%s\n", strings.Repeat("=", 72))
	if r.Reliability.PassKAt > 0 {
		w("  mean pass@1     : %5.1f%%\n", 100*r.Reliability.MeanPass1)
		w("  mean pass^%-2d    : %5.1f%%   (reliability across %d independent tries)\n",
			r.Reliability.PassKAt, 100*r.Reliability.MeanPassK, r.Reliability.PassKAt)
		w("  reliability gap : %+5.1f pp   (pass@1 minus pass^k — the brittleness)\n",
			100*(r.Reliability.MeanPass1-r.Reliability.MeanPassK))
		if !r.Reliability.PassKTrusted {
			w("  NOTE: pass^k UNTRUSTWORTHY (overdispersion fired — the iid p^k closed form is invalid)\n")
		}
		// the per-task pass^k brittleness (sorted by ID for stability).
		sorted := append([]BernTaskEstimate(nil), r.Reliability.PerTask...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TaskID < sorted[j].TaskID })
		w("%s\n", strings.Repeat("-", 72))
		w("  PER-TASK (pass@1 = p̂ -> pass^%d)\n", r.Reliability.PassKAt)
		for _, e := range sorted {
			if e.K <= 0 {
				continue
			}
			w("    %-22s  pass@1=%.3f -> pass^%d=%.3f  (K=%d)\n",
				e.TaskID, e.PHat, e.PassKAt, e.PassK, e.K)
		}
	} else {
		w("  pass^k read OFF (pass-k=0); pass@1 only.\n")
		w("  mean pass@1 (harness): %5.1f%%\n", 100*r.Lift.Harness.solveRate())
	}
	w("\n")

	// 3) the sub-agent-beats-best-member guard.
	w("%s\n", r.Guard.Render())
	w("\n")

	// 4) the METR-style time-horizon (autonomy axis, harness arm).
	w("%s\n", r.Horizon.Render())

	return b.String()
}
