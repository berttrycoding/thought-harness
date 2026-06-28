package realhard

import (
	"fmt"
	"sort"
	"strings"
)

// report.go — reduces the flat per-run results into the headroom proof: per-arm
// solve-rate (ARM A bare = the headroom; ARM B harness = the recovery), the
// pass@k reliability framing (a task counts pass@k-solved if ANY replay solved,
// and all-k-solved if EVERY replay solved), and the per-capability breakdown of
// the bare-vs-harness lift.

// armStats is one arm's aggregate.
type armStats struct {
	Runs        int
	Solved      int // per-run solves (the K-replay solve-rate numerator)
	Confab      int // runs that asserted the lure (confabulation count)
	GroundedRun int // harness runs that imported a reality observation
	ModelCalls  int
	// ToolSelectEsc / ForceGroundEsc total the grounding-fix escalation events over
	// the arm's runs (harness only; 0 for bare). EngagedRuns counts runs in which
	// EITHER escalation fired at least once — the "did the grounding fix engage on
	// the live tick?" tally that a FUTURE A/B reads to confirm engagement.
	ToolSelectEsc  int
	ForceGroundEsc int
	EngagedRuns    int
}

func (a armStats) solveRate() float64 {
	if a.Runs == 0 {
		return 0
	}
	return float64(a.Solved) / float64(a.Runs)
}

// taskRollup is the per-task, per-arm reliability rollup over K replays.
type taskRollup struct {
	TaskID     string
	Capability Capability
	K          int
	BareSolved int // # replays solved by bare
	HarnSolved int // # replays solved by harness
}

// Report is the full reduction.
type Report struct {
	Replays int
	// per-arm overall
	Bare    armStats
	Harness armStats
	// per-capability per-arm
	ByCapBare    map[Capability]armStats
	ByCapHarness map[Capability]armStats
	// per-task reliability
	Tasks []taskRollup
}

// Reduce folds the flat results into a Report.
func Reduce(results []RunResult) Report {
	rep := Report{
		ByCapBare:    map[Capability]armStats{},
		ByCapHarness: map[Capability]armStats{},
	}
	// per (task,arm) replay-solve tallies for the reliability rollup
	type key struct {
		task string
		arm  string
	}
	solvedByKey := map[key]int{}
	kByKey := map[key]int{}
	capByTask := map[string]Capability{}
	taskOrder := []string{}
	seenTask := map[string]bool{}

	for _, r := range results {
		if r.Replay+1 > rep.Replays {
			rep.Replays = r.Replay + 1
		}
		capByTask[r.TaskID] = r.Capability
		if !seenTask[r.TaskID] {
			seenTask[r.TaskID] = true
			taskOrder = append(taskOrder, r.TaskID)
		}
		k := key{r.TaskID, r.Arm}
		kByKey[k]++
		if r.Verdict.Solved {
			solvedByKey[k]++
		}

		acc := func(s *armStats) {
			s.Runs++
			if r.Verdict.Solved {
				s.Solved++
			}
			if r.Verdict.AssertedLure {
				s.Confab++
			}
			if r.Grounded {
				s.GroundedRun++
			}
			s.ModelCalls += r.ModelCalls
			s.ToolSelectEsc += r.ToolSelectEscalations
			s.ForceGroundEsc += r.ForceGroundEscalations
			if r.ToolSelectEscalations > 0 || r.ForceGroundEscalations > 0 {
				s.EngagedRuns++
			}
		}
		switch r.Arm {
		case ArmBare:
			b := rep.Bare
			acc(&b)
			rep.Bare = b
			c := rep.ByCapBare[r.Capability]
			acc(&c)
			rep.ByCapBare[r.Capability] = c
		case ArmHarness:
			h := rep.Harness
			acc(&h)
			rep.Harness = h
			c := rep.ByCapHarness[r.Capability]
			acc(&c)
			rep.ByCapHarness[r.Capability] = c
		}
	}

	for _, tid := range taskOrder {
		bk := key{tid, ArmBare}
		hk := key{tid, ArmHarness}
		rep.Tasks = append(rep.Tasks, taskRollup{
			TaskID:     tid,
			Capability: capByTask[tid],
			K:          maxInt(kByKey[bk], kByKey[hk]),
			BareSolved: solvedByKey[bk],
			HarnSolved: solvedByKey[hk],
		})
	}
	return rep
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Render produces the plain-text headroom report (no emoji, box-drawing only).
func (rep Report) Render(substrate string) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("REAL-WORLD-HARD COGNITION EVAL — bare-vs-harness headroom proof\n")
	w("substrate: %s   replays(K): %d\n", substrate, rep.Replays)
	w("%s\n\n", strings.Repeat("=", 72))

	w("OVERALL SOLVE-RATE (per-run, over all tasks x K replays)\n")
	w("  ARM A bare    : %2d/%2d = %5.1f%%   (the HEADROOM — bare should FAIL a meaningful fraction)\n",
		rep.Bare.Solved, rep.Bare.Runs, 100*rep.Bare.solveRate())
	w("  ARM B harness : %2d/%2d = %5.1f%%   (the RECOVERY — does the harness solve what bare misses?)\n",
		rep.Harness.Solved, rep.Harness.Runs, 100*rep.Harness.solveRate())
	lift := 100 * (rep.Harness.solveRate() - rep.Bare.solveRate())
	w("  LIFT (harness - bare): %+5.1f pp\n", lift)
	w("  bare confabulations (asserted the lure): %d   harness confabulations: %d\n",
		rep.Bare.Confab, rep.Harness.Confab)
	w("  harness grounded-runs (imported reality): %d/%d\n", rep.Harness.GroundedRun, rep.Harness.Runs)
	w("  harness grounding-fix ENGAGEMENT (did the escalation fire on the live tick?):\n")
	w("    tool_select escalations: %d   force_ground escalations: %d   runs-engaged: %d/%d\n\n",
		rep.Harness.ToolSelectEsc, rep.Harness.ForceGroundEsc, rep.Harness.EngagedRuns, rep.Harness.Runs)

	w("PER-CAPABILITY BREAKDOWN (solve-rate bare -> harness)\n")
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapAntiConfabulation, CapLongHorizonConsistency}
	for _, c := range caps {
		ba := rep.ByCapBare[c]
		ha := rep.ByCapHarness[c]
		if ba.Runs == 0 && ha.Runs == 0 {
			continue
		}
		w("  %-26s bare %5.1f%% (%d/%d) -> harness %5.1f%% (%d/%d)  lift %+5.1f pp\n",
			string(c), 100*ba.solveRate(), ba.Solved, ba.Runs,
			100*ha.solveRate(), ha.Solved, ha.Runs,
			100*(ha.solveRate()-ba.solveRate()))
	}
	w("\n")

	w("PER-TASK RELIABILITY (replays solved out of K)\n")
	sorted := append([]taskRollup(nil), rep.Tasks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TaskID < sorted[j].TaskID })
	for _, t := range sorted {
		w("  %-20s [%-24s]  bare %d/%d   harness %d/%d\n",
			t.TaskID, string(t.Capability), t.BareSolved, t.K, t.HarnSolved, t.K)
	}
	w("\n")

	// calibration verdict
	w("CALIBRATION VERDICT\n")
	switch {
	case rep.Bare.Runs == 0:
		w("  no bare runs — cannot assess headroom.\n")
	case rep.Bare.solveRate() >= 0.85:
		w("  WARNING: bare solved %.0f%% — the tasks may be TOO EASY (headroom weak).\n", 100*rep.Bare.solveRate())
		w("  -> recalibrate HARDER before trusting any lift number.\n")
	case rep.Bare.solveRate() <= 0.5:
		w("  HEADROOM CONFIRMED: bare failed a meaningful fraction (solve-rate %.0f%%).\n", 100*rep.Bare.solveRate())
	default:
		w("  PARTIAL HEADROOM: bare solve-rate %.0f%% (some real failure, not saturated).\n", 100*rep.Bare.solveRate())
	}
	if rep.Harness.Runs > 0 {
		switch {
		case lift > 5:
			w("  HARNESS LIFTS: +%.1f pp over bare on these hard tasks.\n", lift)
		case lift < -5:
			w("  HARNESS REGRESSES: %.1f pp BELOW bare — a real negative finding, report it.\n", lift)
		default:
			w("  HARNESS FLAT: %+.1f pp (no clear lift on these tasks — honest null).\n", lift)
		}
	}
	return b.String()
}
