package realhard

import (
	"strings"
	"testing"
)

// subagentguard_test.go — the sub-agent-beats-best-member GUARD (design doc §6 / §7.6;
// "Multi-Agent Teams Hold Experts Back"). Plants three regimes — team beats expert,
// team holds the expert back, and inconclusive — and asserts the FAILABLE verdict,
// plus the NOT-APPLICABLE path when no baseline is supplied.

// strongInputs builds aligned single-strong + team per-task counts from per-task
// (singleSolved, teamSolved) at a fixed K, for a terse guard test.
func guardInputs(k int, rows []struct {
	id           string
	single, team int
}) (single, team []BernTaskInput) {
	for _, r := range rows {
		single = append(single, BernTaskInput{TaskID: r.id, Solved: r.single, K: k})
		team = append(team, BernTaskInput{TaskID: r.id, Solved: r.team, K: k})
	}
	return single, team
}

// TestSubAgentGuardPass: the team (harness) arm RESOLVABLY beats the single-strong
// baseline across tasks ⇒ PASS, passed=true, not flagged.
func TestSubAgentGuardPass(t *testing.T) {
	// high K so the diff resolves; team clearly above single-strong on every task.
	single, team := guardInputs(50, []struct {
		id           string
		single, team int
	}{
		{"t1", 20, 40},
		{"t2", 25, 45},
		{"t3", 22, 42},
	})
	g := CheckSubAgentGuard(single, team)
	if g.Verdict != SubAgentPass {
		t.Fatalf("verdict = %s, want PASS (reason: %s)", g.Verdict, g.Reason)
	}
	if !g.Passed || g.Flagged {
		t.Errorf("PASS must set Passed=true Flagged=false; got Passed=%v Flagged=%v", g.Passed, g.Flagged)
	}
	if g.MeanDiff <= 0 || g.MeanDiffCILo <= 0 {
		t.Errorf("PASS requires a resolved positive diff (CI lo > 0); got diff=%g CIlo=%g", g.MeanDiff, g.MeanDiffCILo)
	}
	if g.TeamRate <= g.SingleStrongRate {
		t.Errorf("team rate (%g) should exceed single-strong (%g)", g.TeamRate, g.SingleStrongRate)
	}
}

// TestSubAgentGuardHoldsBack is the WHOLE POINT (the falsification guard): the team
// arm is RESOLVABLY BELOW the single-strong baseline (integrative-compromise) ⇒
// HOLDS-BACK, flagged anti-value, NOT passed.
func TestSubAgentGuardHoldsBack(t *testing.T) {
	// the single-strong EXPERT solves more than the team on every task (teams hold the
	// expert back 8-38%, the planted finding).
	single, team := guardInputs(50, []struct {
		id           string
		single, team int
	}{
		{"t1", 45, 30},
		{"t2", 42, 28},
		{"t3", 40, 25},
	})
	g := CheckSubAgentGuard(single, team)
	if g.Verdict != SubAgentHoldsBack {
		t.Fatalf("verdict = %s, want HOLDS-BACK (reason: %s)", g.Verdict, g.Reason)
	}
	if g.Passed {
		t.Error("HOLDS-BACK must NOT pass the gate")
	}
	if !g.Flagged {
		t.Error("HOLDS-BACK must FLAG the sub-agent layer as anti-value")
	}
	if g.MeanDiff >= 0 || g.MeanDiffCIHi >= 0 {
		t.Errorf("HOLDS-BACK requires a resolved negative diff (CI hi < 0); got diff=%g CIhi=%g", g.MeanDiff, g.MeanDiffCIHi)
	}
	if len(g.HeldBackTasks) == 0 {
		t.Error("expected at least one per-task held-back flag")
	}
	if !strings.Contains(g.Reason, "ANTI-VALUE") {
		t.Errorf("HOLDS-BACK reason should name the anti-value flag; got %q", g.Reason)
	}
}

// TestSubAgentGuardInconclusive: a small, near-tied difference at low K does not
// resolve ⇒ INCONCLUSIVE (no claim either way; the gate does not pass).
func TestSubAgentGuardInconclusive(t *testing.T) {
	// low K, tiny difference ⇒ the CI straddles 0.
	single, team := guardInputs(5, []struct {
		id           string
		single, team int
	}{
		{"t1", 3, 3},
		{"t2", 2, 3},
	})
	g := CheckSubAgentGuard(single, team)
	if g.Verdict != SubAgentInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE (diff=%g CI[%g,%g])", g.Verdict, g.MeanDiff, g.MeanDiffCILo, g.MeanDiffCIHi)
	}
	if g.Passed || g.Flagged {
		t.Error("INCONCLUSIVE must neither pass nor flag")
	}
	if !(g.MeanDiffCILo <= 0 && g.MeanDiffCIHi >= 0) {
		t.Errorf("INCONCLUSIVE requires the CI to straddle 0; got [%g,%g]", g.MeanDiffCILo, g.MeanDiffCIHi)
	}
}

// TestSubAgentGuardNotApplicable: no single-strong baseline ⇒ NOT-APPLICABLE (the
// sub-agent claim is UNGUARDED — itself a reportable gap), not a silent pass.
func TestSubAgentGuardNotApplicable(t *testing.T) {
	team := []BernTaskInput{{TaskID: "t1", Solved: 30, K: 50}}
	g := CheckSubAgentGuard(nil, team)
	if g.Verdict != SubAgentNotApplicable {
		t.Fatalf("no baseline must be NOT-APPLICABLE, got %s", g.Verdict)
	}
	if g.Passed || g.Flagged {
		t.Error("NOT-APPLICABLE must neither pass nor flag (the claim is simply unguarded)")
	}
	if !strings.Contains(g.Reason, "UNGUARDED") {
		t.Errorf("NOT-APPLICABLE reason should say the claim is unguarded; got %q", g.Reason)
	}
	// a length mismatch is also NOT-APPLICABLE.
	mism := CheckSubAgentGuard(
		[]BernTaskInput{{TaskID: "a", Solved: 1, K: 2}, {TaskID: "b", Solved: 1, K: 2}},
		[]BernTaskInput{{TaskID: "a", Solved: 1, K: 2}},
	)
	if mism.Verdict != SubAgentNotApplicable {
		t.Errorf("length mismatch must be NOT-APPLICABLE, got %s", mism.Verdict)
	}
}

// TestSubAgentGuardRenderNonEmpty: the report renders for an applicable verdict.
func TestSubAgentGuardRenderNonEmpty(t *testing.T) {
	single, team := guardInputs(50, []struct {
		id           string
		single, team int
	}{
		{"t1", 45, 30},
	})
	g := CheckSubAgentGuard(single, team)
	if s := g.Render(); len(s) == 0 || !strings.Contains(s, "SUB-AGENT GUARD") {
		t.Error("guard Render should be non-empty and titled")
	}
}
