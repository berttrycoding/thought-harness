package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/campaign"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/ruler"
)

// cmdProbe is the Phase-1 GAP-FINDING pass: run the BASELINE harness over a diverse/hard suite and report
// per-task pass/fail + what it produced. The FAILURES are the ranked target list for registry scaling —
// where adding skills/operators/knowledge would LIFT a campaign A/B vs sit idle (a candidate only lifts on
// a task the baseline FAILS). Parallel over --concurrency (the #34 throughput lever); cost-tagged.
func cmdProbe(argv []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	suitePath := fs.String("suite", "", "JSON file of probe tasks ([]campaign.HeldOutTask) (required)")
	backend := fs.String("backend", "test", "substrate: test | claude (real, metered) | llm")
	llmModel := fs.String("llm-model", "auto", "model id for --backend claude/llm")
	state := fs.String("state", "", "baseline registry state dir ('' = empty/default — the gap is vs the bare harness)")
	maxTicks := fs.Int("max-ticks", 8, "engine ticks per task")
	concurrency := fs.Int("concurrency", 1, "tasks run through N worker goroutines (max throughput; tune to the substrate rate limit)")
	cognition := fs.Bool("cognition", false, "COGNITION mode: score whether each task's intended cognitive FACULTY fired (branch/act/honest/conflict/decompose/deliberate), not an answer oracle")
	enable := fs.String("enable", "", "comma list of dotted toggle paths to force ON (e.g. --enable conscious.activity.soft to test the branching faculty)")
	replays := fs.Int("replays", 1, "COGNITION noise-floor: run each task K times and report the per-faculty fire RATE + the run-to-run variance (a faculty that flips is instrument noise, not signal)")
	workspace := fs.String("workspace", "", "enable the REAL action layer (read_file/search/run tools) sandboxed to DIR — the AGENTIC axis (where the harness scaffold lifts a bare model)")
	maxItems := fs.Int("max-items", 0, "cap the suite to the first N tasks (0 = all) — bound the metered-substrate cost on a large suite")
	decode := fs.Bool("decode", false, "GATE-1: report the PER-ROLE decode (completion-token) breakdown per task + pooled (where do the tokens go; how big is synthesize_program's share) — run on the HEAVIEST synthesis/planning tasks")
	dumpFaculty := fs.String("dump-faculty-suite", "", "write the built-in v2 outcome-tied faculty suite (campaign.FacultySuite) to this JSON path and exit — the canonical generator for data/campaign/cognition-probe-faculty-v2.json (no run)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	// --dump-faculty-suite: emit the built-in v2 faculty suite (the JSON the probe --suite path loads)
	// and exit. The Go campaign.FacultySuite() is the truth; this regenerates the on-disk artifact.
	if *dumpFaculty != "" {
		data, err := campaign.FacultySuiteJSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "probe: dump faculty suite:", err)
			return 1
		}
		if err := os.WriteFile(*dumpFaculty, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "probe: dump faculty suite:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "wrote %d tasks (%d bytes) to %s\n", len(campaign.FacultySuite()), len(data), *dumpFaculty)
		return 0
	}
	feat, err := resolveFeatures(&featureFlags{enable: *enable})
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		return 1
	}
	if *suitePath == "" {
		fmt.Fprintln(os.Stderr, "probe: --suite is required")
		return 2
	}

	substrate, makeBackend := campaignBackend(*backend, *llmModel)
	newEngine := func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Features = feat        // honour --enable (e.g. the branching/soft-policy knobs)
		cfg.Workspace = *workspace // enable the real tools (the agentic/reality-access axis) when set
		if stateDir != "" {
			st, e := persist.NewJSONLStore(stateDir)
			if e != nil {
				return nil, e
			}
			cfg.Store = st
		}
		be, e := makeBackend()
		if e != nil {
			return nil, e
		}
		return engine.NewEngine(&cfg, be)
	}
	bencher := campaign.EngineBencher{MaxTicks: *maxTicks, NewEngine: newEngine, Concurrency: *concurrency}

	// COGNITION mode: score the cognitive FACULTY (the process), not an answer oracle.
	if *cognition {
		cogTasks, err := loadCognitionSuite(*suitePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "probe:", err)
			return 1
		}
		if *backend != "test" {
			fmt.Fprintf(os.Stderr, "probe (cognition): %d tasks x %d replays on %q (metered), concurrency %d\n", len(cogTasks), *replays, substrate, *concurrency)
		}
		if *replays > 1 {
			return reportCognitionStability(bencher.CognitionProbeReplays(cogTasks, *state, *replays), substrate)
		}
		return reportCognition(bencher.CognitionProbe(cogTasks, *state), substrate)
	}

	tasks, err := loadSuite(*suitePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		return 1
	}
	if *maxItems > 0 && *maxItems < len(tasks) {
		tasks = tasks[:*maxItems]
	}
	bencher.Tasks = tasks

	// GATE-1 per-role decode breakdown: where do the completion tokens go per task, and how big is
	// synthesize_program's share (the efficiency-measurability read). Runs over the SAME suite shape.
	if *decode {
		if *backend != "test" {
			fmt.Fprintf(os.Stderr, "probe (decode): %d tasks on substrate %q (metered), concurrency %d\n", len(tasks), substrate, *concurrency)
		}
		return reportDecode(bencher.DecodeProbe(*state), substrate)
	}

	// ANSWER-ORACLE noise floor (A1 instrument gap): K>1 runs each task K times and reports the per-task
	// solved-rate + grounded-rate + cache-immune replay cost (the mirror of the --cognition --replays path).
	if *replays > 1 {
		if *backend != "test" {
			fmt.Fprintf(os.Stderr, "probe: %d tasks x %d replays on substrate %q (metered), concurrency %d\n", len(tasks), *replays, substrate, *concurrency)
		}
		return reportProbeStability(bencher.ProbeReplays(*state, *replays), substrate)
	}

	if *backend != "test" {
		fmt.Fprintf(os.Stderr, "probe: %d tasks on substrate %q (metered), concurrency %d\n", len(tasks), substrate, *concurrency)
	}
	results := bencher.Probe(*state)

	// Report: the GAP MAP — failures first (the target list), then the pass rate.
	passed := 0
	var fails []campaign.ProbeResult
	var totComp int // completion (output) tokens across the suite — the cache-immune replay-cost total (0 offline)
	for _, r := range results {
		totComp += r.Completion
		if r.Pass {
			passed++
		} else {
			fails = append(fails, r)
		}
	}
	sort.SliceStable(fails, func(i, j int) bool { return fails[i].Calls > fails[j].Calls }) // costliest gaps first

	meanComp := 0.0
	if len(results) > 0 {
		meanComp = float64(totComp) / float64(len(results))
	}
	fmt.Printf("=== GAP MAP: %d/%d solved by the baseline harness (%d GAPS) — substrate %s ===\n",
		passed, len(results), len(fails), substrate)
	// completion-ONLY tokens = the cache-immune Skill-Miner-curve cost (W5 def-of-done: gate on this, not answers)
	fmt.Printf("    replay cost: %d completion-tokens total, %.1f mean/task (cache-immune; 0 on the test double)\n\n", totComp, meanComp)
	if len(fails) > 0 {
		fmt.Println("GAPS (where the baseline FAILS — the registry-scaling target list):")
		for _, r := range fails {
			fmt.Printf("  [FAIL] (%d calls, comp=%d) %s\n", r.Calls, r.Completion, clip(r.Goal, 70))
			fmt.Printf("         want=%q grounded=%v got=%q\n", r.Expect, r.Grounded, clip(r.Answer, 80))
		}
		fmt.Println()
	}
	fmt.Println("SOLVED:")
	for _, r := range results {
		if r.Pass {
			fmt.Printf("  [ ok ] (%d calls, comp=%d) %s\n", r.Calls, r.Completion, clip(r.Goal, 70))
		}
	}
	return 0
}

// loadCognitionSuite reads a JSON []campaign.CognitionTask (goal + expected cognitive signature).
func loadCognitionSuite(path string) ([]campaign.CognitionTask, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tasks []campaign.CognitionTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse cognition suite %s: %w", path, err)
	}
	return tasks, nil
}

// reportCognition prints the cognitive FACULTY map: for each task, did the intended faculty fire, and what
// DID fire (so a miss is diagnosable). The misses are the COGNITION gaps — where a faculty the architecture
// is designed for did not engage when the task called for it.
func reportCognition(results []campaign.CogResult, substrate string) int {
	fired := 0
	totComp := 0 // completion (output) tokens across the suite — the cache-immune replay cost (0 offline)
	for _, r := range results {
		totComp += r.Completion
		if r.Fired {
			fired++
		}
	}
	meanComp := 0.0
	if len(results) > 0 {
		meanComp = float64(totComp) / float64(len(results))
	}
	fmt.Printf("=== COGNITION MAP: %d/%d faculties engaged — substrate %s ===\n", fired, len(results), substrate)
	fmt.Printf("    replay cost: %d completion-tokens total, %.1f mean/task (cache-immune; 0 on the test double)\n\n", totComp, meanComp)
	for _, r := range results {
		mark := "MISS"
		if r.Fired {
			mark = " ok "
		}
		fmt.Printf("  [%s] want=%-10s observed=%v  (%d calls, comp=%d)\n", mark, r.Signature, r.Observed, r.Calls, r.Completion)
		fmt.Printf("         %s\n", clip(r.Goal, 80))
		if !r.Fired {
			fmt.Printf("         got=%q\n", clip(r.Answer, 80))
		}
	}
	return 0
}

// reportCognitionStability prints the NOISE-FLOOR map: per-task fire-rate over K replays + the aggregate
// engaged-rate. A task whose faculty fires e.g. 3/5 is UNSTABLE (the instrument's own noise); only a config
// delta bigger than this noise band is trustworthy.
func reportCognitionStability(rows []campaign.CogStability, substrate string) int {
	totalFires, totalReplays, unstable, totComp := 0, 0, 0, 0
	totalCorrect, totalFAndC, tiedTasks := 0, 0, 0 // OUTCOME-tie (v2): correct + fired-AND-correct counts
	for _, r := range rows {
		totalFires += r.Fired
		totalReplays += r.Replays
		totComp += r.Completion // completion (output) tokens across all tasks×replays — the cache-immune cost
		totalCorrect += r.Correct
		totalFAndC += r.FiredAndCorrect
		if r.OutcomeTied {
			tiedTasks++
		}
		if r.Fired != 0 && r.Fired != r.Replays {
			unstable++ // flipped at least once across replays
		}
	}
	k := 0
	if len(rows) > 0 {
		k = rows[0].Replays
	}
	meanComp := 0.0
	if totalReplays > 0 {
		meanComp = float64(totComp) / float64(totalReplays) // per-replay mean = the Skill-Miner-curve y-value
	}
	fmt.Printf("=== COGNITION NOISE FLOOR: K=%d replays — substrate %s ===\n", k, substrate)
	fmt.Printf("    aggregate faculty engagement: %d/%d fired (%.0f%%); %d/%d tasks UNSTABLE (flipped across replays)\n",
		totalFires, totalReplays, 100*float64(totalFires)/float64(max1(totalReplays)), unstable, len(rows))
	// OUTCOME-TIE (v2): the gated signal — faculty fired AND the answer was objectively correct (the
	// validity fix; a bare fire-rate is gameable). tiedTasks is how many tasks carry an objective oracle.
	if tiedTasks > 0 {
		fmt.Printf("    OUTCOME-TIED (%d/%d tasks have an objective oracle): %d/%d objectively correct (%.0f%%); %d/%d FIRED-AND-CORRECT (%.0f%%) — the gated metric a faculty-lever A/B moves\n",
			tiedTasks, len(rows), totalCorrect, totalReplays, 100*float64(totalCorrect)/float64(max1(totalReplays)),
			totalFAndC, totalReplays, 100*float64(totalFAndC)/float64(max1(totalReplays)))
	}
	// replay cost = the cache-immune completion-token signal (W5 def-of-done); per-task mean below is the curve's y-axis
	fmt.Printf("    replay cost: %d completion-tokens total, %.1f mean/replay (cache-immune; 0 on the test double)\n\n", totComp, meanComp)
	for _, r := range rows {
		flag := "stable"
		if r.Fired != 0 && r.Fired != r.Replays {
			flag = "UNSTABLE"
		}
		fmt.Printf("  [%-8s] %-10s fired %d/%d (%.0f%%)  correct %d/%d  F&C %d/%d  apt~%.2f  comp~%.1f/replay  %s\n",
			flag, r.Signature, r.Fired, r.Replays, 100*r.Rate(), r.Correct, r.Replays, r.FiredAndCorrect, r.Replays,
			r.MeanAptness(), r.MeanCompletion(), clip(r.Goal, 40))
	}

	// GATE-2: the GRADED faculty-signature noise-floor characterization (does the graded aptness
	// signature clear its own σ on this suite — finer than the binary fired/not the rows above show).
	bin := ruler.CharacterizeCog(rows, ruler.Options{})
	gr := ruler.CharacterizeCogGraded(rows, ruler.Options{})
	fmt.Printf("\n=== GATE-2 GRADED-FACULTY CHARACTERIZATION (ruler) ===\n")
	fmt.Printf("  BINARY axis (fire-only):  verdict=%s  ICC=%.3f  σ_noise=%.3f (avg %.3f)  MDE=%.3f\n", bin.Verdict, bin.ICC, bin.SigmaNoise, bin.SigmaNoiseAveraged, bin.MDE)
	fmt.Printf("  GRADED axis:              verdict=%s  ICC=%.3f  σ_within=%.3f (avg %.3f)  betweenSD=%.3f  MDE=%.3f  grandMean=%.3f\n",
		gr.Verdict, gr.ICC, gr.SigmaWithin, gr.SigmaWithinAveraged, gr.BetweenSD, gr.MDE, gr.Mean)
	// OUTCOME-TIED feasibility: re-run the BINARY feasibility gate on the FIRED-AND-CORRECT success count
	// (the validity-fixed metric) — the free pre-claude gate the v2 suite exists to pass (ICC>=0.5 AND
	// MDE<=0.15 on the gated signal, not the gameable fire-only one).
	if tiedTasks > 0 {
		oc := ruler.CharacterizeCog(outcomeTiedRows(rows), ruler.Options{})
		fmt.Printf("  OUTCOME-TIED (fired&correct): verdict=%s  ICC=%.3f  σ_noise=%.3f (avg %.3f)  MDE=%.3f  <- the GATED keep-metric\n",
			oc.Verdict, oc.ICC, oc.SigmaNoise, oc.SigmaNoiseAveraged, oc.MDE)
	}
	return 0
}

// outcomeTiedRows projects each CogStability onto the FIRED-AND-CORRECT success count so the ruler's
// binary feasibility gate runs on the GATED (outcome-tied) metric instead of the gameable fire-only one.
// Pure: copies the row and swaps Fired := FiredAndCorrect (and the per-replay vector accordingly).
func outcomeTiedRows(rows []campaign.CogStability) []campaign.CogStability {
	out := make([]campaign.CogStability, len(rows))
	for i, r := range rows {
		r.Fired = r.FiredAndCorrect
		out[i] = r
	}
	return out
}

// reportProbeStability prints the ANSWER-ORACLE NOISE-FLOOR map (A1 instrument gap) — the answer-path
// mirror of reportCognitionStability. Per task: solved-rate + grounded-rate over K replays + the
// cache-immune mean completion cost, flagged UNSTABLE when the solved verdict flipped across replays
// (the instrument's own noise band). The aggregate header is the noise-floored before/after LIFT signal:
// mean solved-rate over the suite + the mean completion-token replay cost. Only a config delta bigger
// than this noise band is trustworthy.
func reportProbeStability(rows []campaign.ProbeStability, substrate string) int {
	totalSolved, totalReplays, unstable, totComp := 0, 0, 0, 0
	for _, r := range rows {
		totalSolved += r.Solved
		totalReplays += r.Replays
		totComp += r.Completion // completion (output) tokens across all tasks×replays — the cache-immune cost
		if r.Unstable() {
			unstable++ // flipped at least once across replays
		}
	}
	k := 0
	if len(rows) > 0 {
		k = rows[0].Replays
	}
	meanComp := 0.0
	if totalReplays > 0 {
		meanComp = float64(totComp) / float64(totalReplays) // per-replay mean = the Skill-Miner-curve y-value
	}
	fmt.Printf("=== GAP MAP NOISE FLOOR: K=%d replays — substrate %s ===\n", k, substrate)
	fmt.Printf("    aggregate solved: %d/%d replays passed (%.0f%%); %d/%d tasks UNSTABLE (flipped across replays)\n",
		totalSolved, totalReplays, 100*float64(totalSolved)/float64(max1(totalReplays)), unstable, len(rows))
	// replay cost = the cache-immune completion-token signal (W5 def-of-done); per-replay mean below is the curve's y-axis
	fmt.Printf("    replay cost: %d completion-tokens total, %.1f mean/replay (cache-immune; 0 on the test double)\n\n", totComp, meanComp)
	for _, r := range rows {
		flag := "stable"
		if r.Unstable() {
			flag = "UNSTABLE"
		}
		fmt.Printf("  [%-8s] solved %d/%d (%.0f%%) grounded %d/%d (%.0f%%)  comp~%.1f/replay  %s\n",
			flag, r.Solved, r.Replays, 100*r.SolvedRate(), r.Grounded, r.Replays, 100*r.GroundedRate(), r.MeanCompletion(), clip(r.Goal, 50))
	}
	return 0
}

// reportDecode prints the GATE-1 per-role decode breakdown: per task the top decoding roles +
// synthesize_program's share, then the POOLED suite-level per-role aggregate (the verdict reads
// the pooled synthesis share). The gate-1 question: is synthesize_program a LARGE share of decode
// (efficiency-measurable on claude) or a minority even on the heaviest planning task (W6-only)?
func reportDecode(rows []campaign.DecodeProbeRow, substrate string) int {
	bds := make([]campaign.DecodeBreakdown, len(rows))
	for i, r := range rows {
		bds[i] = r.Breakdown
	}
	pooled := campaign.MergeBreakdowns(bds)

	fmt.Printf("=== GATE-1 PER-ROLE DECODE BREAKDOWN — substrate %s ===\n", substrate)
	fmt.Printf("    %d tasks; pooled total decode = %d completion-tokens over %d calls\n\n", len(rows), pooled.TotalCompletion, pooled.TotalCalls)
	for _, r := range rows {
		fmt.Printf("  TASK: %s\n", clip(r.Goal, 72))
		fmt.Printf("        total decode = %d tok; synthesize_program = %d tok (%.0f%% share); grounded=%v solved=%v\n",
			r.Breakdown.TotalCompletion, r.SynthCompletion, 100*r.SynthShare, r.Grounded, r.Solved)
		for _, rd := range r.Breakdown.Roles {
			share := 0.0
			if r.Breakdown.TotalCompletion > 0 {
				share = 100 * float64(rd.Completion) / float64(r.Breakdown.TotalCompletion)
			}
			fmt.Printf("          %-26s %6d tok  (%5.1f%%)  %d calls\n", rd.Role, rd.Completion, share, rd.Calls)
		}
		fmt.Println()
	}
	fmt.Println("POOLED (suite-level per-role decode share — the gate-1 verdict reads this):")
	for _, rd := range pooled.Roles {
		share := 0.0
		if pooled.TotalCompletion > 0 {
			share = 100 * float64(rd.Completion) / float64(pooled.TotalCompletion)
		}
		fmt.Printf("  %-26s %7d tok  (%5.1f%%)  %d calls\n", rd.Role, rd.Completion, share, rd.Calls)
	}
	fmt.Printf("\n  synthesize_program POOLED share = %.1f%% (%d tok)\n", 100*pooled.ShareOf("synthesize_program"), pooled.CompletionOf("synthesize_program"))
	return 0
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// clip truncates s to n runes with an ellipsis, collapsing newlines for one-line reporting.
func clip(s string, n int) string {
	r := []rune("")
	for _, c := range s {
		if c == '\n' || c == '\r' {
			c = ' '
		}
		r = append(r, c)
	}
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
