package realhard

import (
	"fmt"
	"sort"
	"strings"
)

// subagentguard.go — the SUB-AGENT-BEATS-BEST-MEMBER GUARD (design doc §6 "L4 sub-agent
// GUARD" + §7.6 the first architecture-claim guard; source: "Multi-Agent Teams Hold
// Experts Back", arXiv 2602.01011).
//
// THE FINDING IT GUARDS AGAINST. LLM teams UNDERPERFORM their strongest member by
// 8–38% because they "integrative-compromise" instead of deferring to the expert.
// This is a direct FALSIFICATION RISK for our sub-agent / teaming claims: a harness
// arm that dispatches a TEAM of sub-agents must prove it ADDS over a SINGLE STRONG
// agent — otherwise the sub-agent layer is ANTI-VALUE (matches the memory line that
// an elegant architecture can move no metric).
//
// THE GUARD. When an A/B includes harness SUB-AGENT DISPATCH it MUST also run a
// SINGLE-STRONG-AGENT baseline arm (ArmSingleStrong). The guard compares the harness
// (team-dispatching) arm's per-task solve-rate against the single-strong baseline,
// task-paired, with the Newcombe two-proportion CI (the same boundary-correct
// interval the Bernoulli capability test uses). It is a FAILABLE standing check:
//
//   - PASS  — the harness arm BEATS the single-strong baseline (aggregate diff
//     resolves > 0: the CI lower bound clears 0). The sub-agent layer adds value.
//   - HOLDS-BACK — the harness arm is RESOLVABLY BELOW the single-strong baseline
//     (aggregate diff resolves < 0): the team integrative-compromises away the
//     expert's edge — the sub-agent layer is ANTI-VALUE on this suite. The guard
//     FLAGS it (the failable outcome the doc requires).
//   - INCONCLUSIVE — the diff does not resolve at this K (CI straddles 0): no claim
//     either way; raise K (the Bernoulli adaptive recommender sizes it).
//   - NOT-APPLICABLE — no single-strong baseline arm was supplied: the guard cannot
//     run (the harness arm's sub-agent claim is UNGUARDED — itself a reportable gap).
//
// SCOPE / HONESTY. Pure CONTROL: closed-form proportion arithmetic over the per-task
// (solved, K) counts the suite already collects, NO model, NO RNG, NO clock — same
// determinism as bernoulli.go. The guard does not RUN the arms (the suite does); it
// REDUCES their per-task counts into a verdict. The single-strong arm is the
// strongest single config the harness can field WITHOUT sub-agent fan-out (the doc's
// "single strong agent" baseline) — the caller supplies its per-task counts; this
// file does not prescribe how it is produced (that is an arms.go / cmd concern).

// ArmSingleStrong is the single-strong-agent baseline arm name: the strongest single
// agent the harness can field WITHOUT sub-agent dispatch/fan-out (the doc's "single
// strong agent" the team must beat). It is distinct from ArmBare (raw model, no
// scaffold at all) and ArmHarness (the full scaffold, which MAY dispatch sub-agents).
const ArmSingleStrong = "single-strong"

// SubAgentVerdict is the guard's verdict.
type SubAgentVerdict string

const (
	// SubAgentPass — the team (harness) arm BEATS the single-strong baseline (the
	// paired aggregate diff resolves > 0). The sub-agent layer adds value.
	SubAgentPass SubAgentVerdict = "PASS"
	// SubAgentHoldsBack — the team arm is RESOLVABLY BELOW the single-strong baseline
	// (diff resolves < 0): teams hold the expert back here — the sub-agent layer is
	// ANTI-VALUE. The FAILABLE outcome.
	SubAgentHoldsBack SubAgentVerdict = "HOLDS-BACK"
	// SubAgentInconclusive — the diff does not resolve at this K (CI straddles 0).
	SubAgentInconclusive SubAgentVerdict = "INCONCLUSIVE"
	// SubAgentNotApplicable — no single-strong baseline was supplied; the guard cannot
	// run (the sub-agent claim is UNGUARDED).
	SubAgentNotApplicable SubAgentVerdict = "NOT-APPLICABLE"
)

// SubAgentGuard is the result of the sub-agent-beats-best-member check.
type SubAgentGuard struct {
	Verdict SubAgentVerdict
	// Passed is the boolean gate: true ONLY for SubAgentPass. A HOLDS-BACK or an
	// INCONCLUSIVE both fail the gate (a guard that cannot prove the team beats the
	// expert does not pass), but only HOLDS-BACK is the affirmative anti-value FLAG.
	Passed bool
	// Flagged is true for HOLDS-BACK: the sub-agent layer is resolvably anti-value
	// (the standing alarm the doc requires).
	Flagged bool

	HasBaseline bool // a single-strong arm was supplied
	NTasks      int  // tasks compared (paired by ID)

	// SingleStrongRate / TeamRate are the aggregate (over tasks x replays) solve-rates.
	SingleStrongRate float64
	TeamRate         float64

	// MeanDiff is the paired-by-task aggregate (team - single-strong) with its
	// Newcombe-derived 95% CI. Resolves > 0 ⇒ PASS; < 0 ⇒ HOLDS-BACK.
	MeanDiff     float64
	MeanDiffCILo float64
	MeanDiffCIHi float64

	// PerTask is the per-task (team - single-strong) diff with its CI.
	PerTask []BernPairedTaskDiff
	// HeldBackTasks lists tasks where the team is RESOLVABLY below the single-strong
	// baseline (per-task CI excludes 0 on the negative side) — where the expert was
	// held back.
	HeldBackTasks []string

	Reason string
}

// CheckSubAgentGuard runs the guard. single and team are per-task (solved, K) counts
// aligned by TaskID (same order) — single = the single-strong baseline arm, team = the
// harness arm that dispatches sub-agents. A nil/empty single ⇒ NOT-APPLICABLE (the
// claim is unguarded). The comparison is paired-by-task with the Newcombe two-
// proportion CI (the boundary-correct interval, shared with the Bernoulli capability
// test). Pure, deterministic.
func CheckSubAgentGuard(single, team []BernTaskInput) SubAgentGuard {
	g := SubAgentGuard{}
	if len(single) == 0 {
		g.Verdict = SubAgentNotApplicable
		g.Reason = "no single-strong baseline arm supplied: the harness sub-agent claim is UNGUARDED (run an ArmSingleStrong arm to gate it)"
		return g
	}
	if len(team) != len(single) {
		g.Verdict = SubAgentNotApplicable
		g.Reason = fmt.Sprintf("arm length mismatch (single=%d, team=%d): cannot pair tasks", len(single), len(team))
		return g
	}
	g.HasBaseline = true
	g.NTasks = len(single)

	z := bernZ95
	g.PerTask = make([]BernPairedTaskDiff, len(single))
	var ssSolved, ssK, teamSolved, teamK int
	for i := range single {
		// twoProportionDiffTask computes (on - off); here off=single-strong, on=team, so
		// the diff is (team - single-strong): positive ⇒ the team beat the expert.
		g.PerTask[i] = twoProportionDiffTask(single[i], team[i], z)
		ssSolved += single[i].Solved
		ssK += single[i].K
		teamSolved += team[i].Solved
		teamK += team[i].K
		// a per-task HOLD-BACK: the team is resolvably BELOW the single-strong baseline
		// (the per-task CI excludes 0 and the point diff is negative).
		if g.PerTask[i].Moved && g.PerTask[i].Diff < 0 {
			g.HeldBackTasks = append(g.HeldBackTasks, single[i].TaskID)
		}
	}
	sort.Strings(g.HeldBackTasks)
	g.SingleStrongRate = safeRate(ssSolved, ssK)
	g.TeamRate = safeRate(teamSolved, teamK)

	g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi, _ = aggregateProportionDiff(g.PerTask, single, team, z)

	switch {
	case g.MeanDiffCILo > 0:
		g.Verdict = SubAgentPass
		g.Passed = true
		g.Reason = fmt.Sprintf("the team (harness) arm BEATS the single-strong baseline by %+.3f (95%% CI [%+.3f,%+.3f] clears 0): the sub-agent layer adds value",
			g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi)
	case g.MeanDiffCIHi < 0:
		g.Verdict = SubAgentHoldsBack
		g.Flagged = true
		g.Reason = fmt.Sprintf("ANTI-VALUE FLAG: the team (harness) arm is RESOLVABLY BELOW the single-strong baseline by %+.3f (95%% CI [%+.3f,%+.3f] below 0): teams hold the expert back here — the sub-agent layer is anti-value on this suite ('Multi-Agent Teams Hold Experts Back')",
			g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi)
	default:
		g.Verdict = SubAgentInconclusive
		g.Reason = fmt.Sprintf("INCONCLUSIVE: team - single-strong = %+.3f but the 95%% CI [%+.3f,%+.3f] straddles 0 — cannot prove the team beats (or is beaten by) the expert at this K; raise K",
			g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi)
	}
	return g
}

// Render produces the plain-text guard report (no emoji, box-drawing only).
func (g SubAgentGuard) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("SUB-AGENT GUARD — team (harness) must BEAT a single-strong agent\n")
	w("(falsification guard vs 'Multi-Agent Teams Hold Experts Back', arXiv 2602.01011)\n")
	w("%s\n", strings.Repeat("=", 72))
	w("VERDICT: %s\n", g.Verdict)
	w("  %s\n", g.Reason)
	if !g.HasBaseline {
		return b.String()
	}
	w("%s\n", strings.Repeat("-", 72))
	w("  single-strong rate : %5.1f%%\n", 100*g.SingleStrongRate)
	w("  team (harness) rate: %5.1f%%\n", 100*g.TeamRate)
	w("  aggregate diff (team - single-strong): %+.4f  95%% CI[%+.4f,%+.4f]\n",
		g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi)
	if len(g.HeldBackTasks) > 0 {
		w("  tasks where the team is RESOLVABLY held back: %s\n", strings.Join(g.HeldBackTasks, ", "))
	}
	w("%s\n", strings.Repeat("-", 72))
	w("PER-TASK (team - single-strong, Newcombe CI)\n")
	sorted := append([]BernPairedTaskDiff(nil), g.PerTask...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TaskID < sorted[j].TaskID })
	for _, d := range sorted {
		mark := ""
		if d.Moved {
			if d.Diff < 0 {
				mark = "  HELD-BACK"
			} else {
				mark = "  team-beats"
			}
		}
		w("  %-22s  single=%.3f team=%.3f  diff=%+.3f  CI[%+.3f,%+.3f]%s\n",
			d.TaskID, d.POff, d.POn, d.Diff, d.CILo, d.CIHi, mark)
	}
	return b.String()
}
