package realhard

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// abrun_test.go — the bare-vs-harness A/B RUNNER mechanism test (the wiring this slice
// builds). It proves the PLUMBING + the assembled report render end-to-end, in two
// layers:
//
//	(a) reduceAB (PURE) over PLANTED per-arm results: every assembled field populates
//	    and the sub-agent guard returns a REAL verdict (PASS / HOLDS-BACK), NOT
//	    NOT-APPLICABLE — the load-bearing claim of this build (the single-strong arm now
//	    feeds the guard). Deterministic, no model.
//	(b) RunAB end-to-end on --backend test (the offline double, canned content): all
//	    THREE arms (bare + harness + single-strong) actually run, the report renders, and
//	    the guard is applicable (the single-strong arm produced counts) — the
//	    mechanism-correctness signal, not a capability lift (the double gives canned
//	    content, so the LIFT number is not meaningful — the LIVE claude run is what the
//	    caller drives for the real delta).

// abPlantArm builds per-task RunResults for one arm from per-task solved-counts at a
// fixed K (replays). cap is constant for the planted set (the report still renders).
func abPlantArm(arm string, k int, capb Capability, solvedByTask map[string]int) []RunResult {
	var out []RunResult
	for id, solved := range solvedByTask {
		for r := 0; r < k; r++ {
			out = append(out, mkResult(id, capb, arm, r, r < solved))
		}
	}
	return out
}

// TestReduceABPopulatesAllSections: PLANTED bare/harness/single-strong arms where the
// harness team RESOLVABLY beats the single-strong baseline. reduceAB must populate the
// lift, the pass^k reliability, the guard (PASS — a REAL verdict, not NOT-APPLICABLE),
// and render the assembled report end-to-end. This is the wiring proof.
func TestReduceABPopulatesAllSections(t *testing.T) {
	const k = 20
	// pick three REAL task IDs so the per-task-ID alignment + human-minutes lookup
	// exercise the live bank (the report places them on the time-horizon).
	ids := realTaskIDs(t, 3)

	// bare: low (the headroom). harness (team): high. single-strong: between — so the
	// team RESOLVABLY beats the single-strong baseline (guard PASS) AND beats bare (lift).
	bareSolved := map[string]int{ids[0]: 2, ids[1]: 3, ids[2]: 2}
	singleSolved := map[string]int{ids[0]: 8, ids[1]: 9, ids[2]: 7}
	teamSolved := map[string]int{ids[0]: 18, ids[1]: 19, ids[2]: 17}

	var results []RunResult
	results = append(results, abPlantArm(ArmBare, k, CapMultiHopGrounding, bareSolved)...)
	results = append(results, abPlantArm(ArmSingleStrong, k, CapMultiHopGrounding, singleSolved)...)
	results = append(results, abPlantArm(ArmHarness, k, CapMultiHopGrounding, teamSolved)...)

	// filter to exactly the planted IDs so reduceAB's task alignment matches.
	filter := strings.Join(ids, ",")
	rep := reduceAB(results, nil, filter, 3, "test")

	// --- lift section ---
	if rep.Lift.Bare.Runs == 0 || rep.Lift.Harness.Runs == 0 {
		t.Fatalf("lift section empty: bare.Runs=%d harness.Runs=%d", rep.Lift.Bare.Runs, rep.Lift.Harness.Runs)
	}
	lift := rep.Lift.Harness.solveRate() - rep.Lift.Bare.solveRate()
	if lift <= 0 {
		t.Errorf("planted harness>bare must show a positive lift; got %g", lift)
	}

	// --- reliability section (pass@1 vs pass^k) ---
	if rep.Reliability.PassKAt != 3 {
		t.Fatalf("reliability PassKAt = %d, want 3", rep.Reliability.PassKAt)
	}
	if rep.Reliability.MeanPass1 <= 0 {
		t.Errorf("MeanPass1 should be populated (>0); got %g", rep.Reliability.MeanPass1)
	}
	// pass^k must be BELOW pass@1 for non-saturated tasks (the brittleness axis).
	if rep.Reliability.MeanPassK >= rep.Reliability.MeanPass1 {
		t.Errorf("pass^k (%g) must be below pass@1 (%g) — the reliability gap",
			rep.Reliability.MeanPassK, rep.Reliability.MeanPass1)
	}

	// --- guard section: a REAL verdict, not NOT-APPLICABLE (the load-bearing claim) ---
	if rep.Guard.Verdict == SubAgentNotApplicable {
		t.Fatalf("the single-strong arm must make the guard APPLICABLE; got NOT-APPLICABLE: %s", rep.Guard.Reason)
	}
	if !rep.Guard.HasBaseline {
		t.Error("guard must report HasBaseline=true (the single-strong arm was supplied)")
	}
	if rep.Guard.Verdict != SubAgentPass {
		t.Errorf("planted team>>single-strong must be PASS; got %s (diff=%g CI[%g,%g])",
			rep.Guard.Verdict, rep.Guard.MeanDiff, rep.Guard.MeanDiffCILo, rep.Guard.MeanDiffCIHi)
	}

	// --- time-horizon section ---
	if rep.Horizon.NEff != len(ids) {
		t.Errorf("time-horizon should see %d informative tasks; got NEff=%d", len(ids), rep.Horizon.NEff)
	}

	// --- the assembled report renders all four sections end-to-end ---
	out := rep.Render()
	for _, want := range []string{
		"BARE-vs-HARNESS A/B MEASUREMENT",
		"LIFT",
		"RELIABILITY",
		"pass^k",
		"SUB-AGENT GUARD",
		"TIME-HORIZON",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("assembled report missing section %q\n---\n%s", want, out)
		}
	}
}

// TestReduceABGuardHoldsBack: PLANTED single-strong arm that BEATS the team (harness)
// arm ⇒ the guard must FLAG HOLDS-BACK (the falsification axis — the sub-agent layer is
// anti-value). Proves the guard is genuinely failable through the A/B wiring, not pinned
// to PASS.
func TestReduceABGuardHoldsBack(t *testing.T) {
	const k = 30
	ids := realTaskIDs(t, 3)
	bareSolved := map[string]int{ids[0]: 2, ids[1]: 2, ids[2]: 2}
	singleSolved := map[string]int{ids[0]: 27, ids[1]: 26, ids[2]: 28} // the expert
	teamSolved := map[string]int{ids[0]: 15, ids[1]: 14, ids[2]: 16}   // team holds it back

	var results []RunResult
	results = append(results, abPlantArm(ArmBare, k, CapAdaptiveBacktracking, bareSolved)...)
	results = append(results, abPlantArm(ArmSingleStrong, k, CapAdaptiveBacktracking, singleSolved)...)
	results = append(results, abPlantArm(ArmHarness, k, CapAdaptiveBacktracking, teamSolved)...)

	rep := reduceAB(results, nil, strings.Join(ids, ","), 0, "test")
	if rep.Guard.Verdict != SubAgentHoldsBack {
		t.Fatalf("planted single-strong >> team must be HOLDS-BACK; got %s (diff=%g)", rep.Guard.Verdict, rep.Guard.MeanDiff)
	}
	if !rep.Guard.Flagged {
		t.Error("HOLDS-BACK must flag the sub-agent layer anti-value")
	}
	// pass-k=0 ⇒ the reliability section reports pass@1 only (no decay read).
	if rep.Reliability.PassKAt != 0 {
		t.Errorf("pass-k=0 must leave the pass^k read off; got PassKAt=%d", rep.Reliability.PassKAt)
	}
}

// TestReduceABNoSingleStrongIsNotApplicable: when the single-strong arm is ABSENT (only
// bare + harness ran, the legacy two-arm shape), the guard must honestly report
// NOT-APPLICABLE (the sub-agent claim is unguarded) — never a spurious PASS off an
// all-zero baseline.
func TestReduceABNoSingleStrongIsNotApplicable(t *testing.T) {
	const k = 10
	ids := realTaskIDs(t, 2)
	var results []RunResult
	results = append(results, abPlantArm(ArmBare, k, CapMultiHopGrounding, map[string]int{ids[0]: 2, ids[1]: 3})...)
	results = append(results, abPlantArm(ArmHarness, k, CapMultiHopGrounding, map[string]int{ids[0]: 8, ids[1]: 7})...)

	rep := reduceAB(results, nil, strings.Join(ids, ","), 0, "test")
	if rep.Guard.Verdict != SubAgentNotApplicable {
		t.Fatalf("absent single-strong arm must be NOT-APPLICABLE; got %s", rep.Guard.Verdict)
	}
	// the lift is still computed (the headroom proof does not need the baseline).
	if rep.Lift.Harness.solveRate() <= rep.Lift.Bare.solveRate() {
		t.Error("the lift should still compute without the single-strong arm")
	}
}

// TestRunABEndToEndOnTestDouble: drive the FULL RunAB on the offline test double — all
// THREE arms must run (bare + harness + single-strong), the guard must be APPLICABLE
// (the single-strong arm produced counts), and the report renders end-to-end. This is
// the mechanism-correctness signal the spec's step-3 offline validation requires (NOT a
// capability lift — the double's canned content makes the lift number meaningless; the
// LIVE claude run is what measures the real delta).
func TestRunABEndToEndOnTestDouble(t *testing.T) {
	factory := func(_ int64, _ float64) backends.Backend { return backends.NewTest() }
	// cheap: 2 tasks (the first held-out pair via the substring filter), K=2.
	cfg := ABConfig{
		Factory:      factory,
		Replays:      2,
		SeedBase:     1729,
		MaxTicks:     40,
		Concurrency:  2,
		TaskFilter:   "held-0001,held-0002",
		PassK:        2,
		Substrate:    "test",
		SingleStrong: true, // the full 3-arm A/B: bare + harness + single-strong (the guard's baseline)
	}
	rep, armErrs, err := RunAB(cfg)
	if err != nil {
		t.Fatalf("RunAB on the test double: %v", err)
	}
	if len(armErrs) != 0 {
		t.Fatalf("the offline test double must not error any arm; got %d failures: %v", len(armErrs), armErrs)
	}

	// all three arms ran: bare + harness over both tasks x K=2 ⇒ 4 runs each; single-strong too.
	if rep.Lift.Bare.Runs != 4 {
		t.Errorf("bare arm should have 4 runs (2 tasks x 2 replays); got %d", rep.Lift.Bare.Runs)
	}
	if rep.Lift.Harness.Runs != 4 {
		t.Errorf("harness arm should have 4 runs; got %d", rep.Lift.Harness.Runs)
	}
	// the single-strong arm ran ⇒ the guard is APPLICABLE (HasBaseline) over 2 paired tasks.
	if !rep.Guard.HasBaseline {
		t.Fatalf("the single-strong arm must have run end-to-end (guard HasBaseline); verdict=%s reason=%s",
			rep.Guard.Verdict, rep.Guard.Reason)
	}
	if rep.Guard.NTasks != 2 {
		t.Errorf("guard should pair 2 tasks; got NTasks=%d", rep.Guard.NTasks)
	}
	// the report renders end-to-end with every section present.
	out := rep.Render()
	if !strings.Contains(out, "SUB-AGENT GUARD") || !strings.Contains(out, "TIME-HORIZON") || !strings.Contains(out, "RELIABILITY") {
		t.Errorf("end-to-end report missing a section:\n%s", out)
	}
}

// panicBackend embeds the offline test double but PANICS on Generate — the test stand-in
// for a live-substrate arm-run that blows up (a nil-deref / panic in the engine on claude
// is exactly the "exit 1, no error, no report" failure mode the robustness fix targets,
// since an unrecovered panic in a worker goroutine crashes the whole process). The bare
// arm calls Generate first, so the panic fires there; everything else is the canned double.
type panicBackend struct{ *backends.TestBackend }

func (p *panicBackend) Generate(string, []types.Thought, *cpyrand.Random) string {
	panic("simulated live-substrate blow-up in Generate")
}

// TestRunSuiteRecoversArmPanicAndContinues: a backend that PANICS on Generate must NOT
// crash the process or abort the run. The suite must (a) recover the panic, (b) record it
// as an ArmError, (c) emit a synthetic FAIL RunResult for the failed cell (so the report
// accounts for it), and (d) still return its results. With a single task whose ONLY
// attempted arm (bare) panics on every replay, that is a TOTAL wipe-out ⇒ a non-nil error
// (fail-loud) — but the armErrs + the partial results are still returned for diagnosis.
func TestRunSuiteRecoversArmPanicAndContinues(t *testing.T) {
	factory := func(_ int64, _ float64) backends.Backend {
		return &panicBackend{backends.NewTest()}
	}
	cfg := SuiteConfig{
		Factory:    factory,
		Replays:    2,
		SeedBase:   1729,
		MaxTicks:   40,
		OnlyArm:    ArmBare, // isolate the panic to ONE arm so this is a clean total-wipe assertion
		TaskFilter: "held-0001",
	}
	results, armErrs, err := RunSuite(cfg)
	// every bare run panicked ⇒ a TOTAL wipe-out ⇒ fail-loud (non-nil err), but NOT a crash.
	if err == nil {
		t.Fatal("a backend that panics on every arm must fail loud (non-nil error), not silently")
	}
	if len(armErrs) == 0 {
		t.Fatal("the recovered panic must be recorded as an ArmError (it was swallowed)")
	}
	if !strings.Contains(armErrs[0].Err, "PANIC") || !strings.Contains(armErrs[0].Err, "blow-up") {
		t.Errorf("the ArmError must carry the recovered panic message; got %q", armErrs[0].Err)
	}
	// the failed cells are recorded as synthetic FAIL results (so a partial run still reports).
	if len(results) != len(armErrs) {
		t.Errorf("each failed arm-run must emit one synthetic FAIL result; results=%d armErrs=%d", len(results), len(armErrs))
	}
	for _, r := range results {
		if r.Verdict.Solved {
			t.Errorf("a failed arm-run must score FAIL, never a fabricated SOLVE: %+v", r.Verdict)
		}
		if !strings.HasPrefix(r.Verdict.Reason, "RUN-ERROR:") {
			t.Errorf("a failed arm-run's reason must be tagged RUN-ERROR; got %q", r.Verdict.Reason)
		}
	}
}

// TestRunSuitePartialFailureStillReports: when SOME arm-runs fail but others complete (the
// realistic live case — one task panics, another succeeds), the run must NOT abort: err is
// nil (partial, not total), the failures are returned in armErrs, AND the surviving
// results are present. The report is then buildable from what completed (the AB caller
// renders it with the partial-failure banner). This is the core robustness contract: a
// single bad cell never throws away the whole run.
func TestRunSuitePartialFailureStillReports(t *testing.T) {
	// task held-0001 panics on Generate; held-0002 runs clean on the canned double.
	factory := func(_ int64, _ float64) backends.Backend {
		return &selectivePanicBackend{TestBackend: backends.NewTest(), panicGoalSub: held1Prompt(t)}
	}
	cfg := SuiteConfig{
		Factory:    factory,
		Replays:    1,
		SeedBase:   1729,
		OnlyArm:    ArmBare, // bare-only keeps the assertion deterministic + cheap
		TaskFilter: "held-0001,held-0002",
	}
	results, armErrs, err := RunSuite(cfg)
	if err != nil {
		t.Fatalf("a PARTIAL failure must not fail loud (only a total wipe-out does); got err=%v", err)
	}
	if len(armErrs) != 1 {
		t.Fatalf("exactly one task (held-0001) must fail; got %d failures: %v", len(armErrs), armErrs)
	}
	if !strings.Contains(armErrs[0].TaskID, "held-0001") {
		t.Errorf("the failure must be the panicking task (held-0001); got %s", armErrs[0].TaskID)
	}
	// both tasks contribute a result (one real, one synthetic FAIL) — nothing dropped.
	if len(results) != 2 {
		t.Fatalf("both tasks must yield a result (1 real + 1 synthetic FAIL); got %d", len(results))
	}
}

// selectivePanicBackend panics on Generate ONLY when the goal contains panicGoalSub — so
// one task in a multi-task run blows up while the others complete on the canned double.
type selectivePanicBackend struct {
	*backends.TestBackend
	panicGoalSub string
}

func (p *selectivePanicBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	if p.panicGoalSub != "" && strings.Contains(goal, p.panicGoalSub) {
		panic("simulated live-substrate blow-up on held-0001")
	}
	return p.TestBackend.Generate(goal, ctx, rng)
}

// TestSingleStrongArmCompletesOnDouble is the headline-path regression: the SINGLE-STRONG arm
// (RunSingleStrong: the full engine with the sub-agent fan-out config flipped off) must construct AND
// run a clean episode on the offline double WITHOUT panicking — the offline floor under the live
// single-strong crash. (The crash itself is a fan-out worker-GOROUTINE panic reachable only on the live
// substrate, where the stance/Par specialists fire; the double never fans out on a realhard task, so
// this asserts the construct+run path is sound and the goroutine-escape FIX is exercised by the precise
// subconscious unit tests — TestPrimitiveSubAgentFanoutWorkerPanicIsRecoverable / ...ParPhase...). It is
// the offline gate the task names: "the single-strong engine constructs + runs an episode without panic
// on the double".)
func TestSingleStrongArmCompletesOnDouble(t *testing.T) {
	factory := func(_ int64, _ float64) backends.Backend { return backends.NewTest() }
	cfg := SuiteConfig{
		Factory:             factory,
		Replays:             1,
		SeedBase:            1729,
		MaxTicks:            40,
		TaskFilter:          "mhop-0001",
		IncludeSingleStrong: true, // exercise all three arms incl. the single-strong baseline
	}
	results, armErrs, err := RunSuite(cfg)
	if err != nil {
		t.Fatalf("the three-arm run (incl. single-strong) must complete cleanly on the double; got err=%v", err)
	}
	if len(armErrs) != 0 {
		t.Fatalf("no arm should fail on the offline double; got %d failures: %v", len(armErrs), armErrs)
	}
	var sawSingle bool
	for _, r := range results {
		if r.Arm == ArmSingleStrong {
			sawSingle = true
			if strings.HasPrefix(r.Verdict.Reason, "RUN-ERROR:") {
				t.Errorf("the single-strong arm must run clean (no RUN-ERROR) on the double; got %q", r.Verdict.Reason)
			}
		}
	}
	if !sawSingle {
		t.Fatal("the single-strong arm produced no result (it must run when IncludeSingleStrong is set)")
	}
}

// held1Prompt returns a stable substring of the held-0001 task's prompt so the selective
// panic targets exactly that task (deterministic, no hardcoded literal that could drift).
func held1Prompt(t *testing.T) string {
	t.Helper()
	for _, tk := range Tasks() {
		if strings.Contains(tk.ID, "held-0001") {
			// the first ~24 prompt chars are a stable, task-unique marker.
			p := strings.TrimSpace(tk.Prompt)
			if len(p) > 24 {
				return p[:24]
			}
			return p
		}
	}
	t.Fatal("held-0001 not in the bank")
	return ""
}

// realTaskIDs returns the first n stable task IDs from the live bank (so the per-task
// human-minutes lookup + alignment exercise the real Task entries).
func realTaskIDs(t *testing.T, n int) []string {
	t.Helper()
	tasks := Tasks()
	if len(tasks) < n {
		t.Fatalf("bank has %d tasks, need %d", len(tasks), n)
	}
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = tasks[i].ID
	}
	return ids
}
