package campaign

import (
	"strconv"
	"strings"
	"testing"
)

// cogoracle_test.go — MUTATION-TESTS the v2 faculty-suite objective oracle (cogoracle.go), the Phase-0 /
// oracle-doctor requirement mirrored from internal/bench/realhard/oracle_test.go: a SOUND oracle credits
// the ground truth and rejects wrong answers — especially the System-1 lure. For every oracle-bearing task
// it asserts: the ground truth SOLVES, the prior lure FAILS, a battery of generic wrong answers FAIL, and
// (decline tasks) an honest decline SOLVES while a confident number / empty give-up FAIL.

// cogGroundTruth renders the canonical correct answer string for a task — what a perfect arm would emit.
func cogGroundTruth(t CognitionTask) string {
	switch t.Oracle {
	case cogOracleExact, cogOracleNumericTol:
		return "After working it through, the answer is " + t.Expected + "."
	case cogOracleSetMember:
		return t.Expected
	case cogOracleDecline:
		return "This is not determinable from the material; no value is defined here."
	default:
		return ""
	}
}

func TestCogGroundTruthSolves(t *testing.T) {
	for _, task := range FacultySuite() {
		if !task.HasOracle() {
			continue
		}
		v := scoreOutcome(task, cogGroundTruth(task))
		if !v.Solved {
			t.Errorf("[%s] ground truth must SOLVE, got fail: %s (answer=%q)",
				clipGoal(task.Goal), v.Reason, cogGroundTruth(task))
		}
	}
}

func TestCogPriorLureFails(t *testing.T) {
	for _, task := range FacultySuite() {
		if strings.TrimSpace(task.PriorLure) == "" {
			continue
		}
		// Emit the lure the way a confident System-1 / confabulating arm would: as the conclusion.
		var lureAns string
		switch task.Oracle {
		case cogOracleDecline:
			lureAns = "The value is " + task.PriorLure + "."
		default:
			lureAns = "After the naive read, the answer is " + task.PriorLure + "."
		}
		v := scoreOutcome(task, lureAns)
		if v.Solved {
			t.Errorf("[%s] the PRIOR LURE %q must FAIL the oracle, but it SOLVED: %s",
				clipGoal(task.Goal), task.PriorLure, v.Reason)
		}
		if !v.AssertedLure {
			t.Errorf("[%s] the lure answer %q must flag AssertedLure", clipGoal(task.Goal), lureAns)
		}
	}
}

func TestCogWrongAnswersFail(t *testing.T) {
	// generic mutations that must never pass an exact/numeric oracle.
	for _, task := range FacultySuite() {
		if task.Oracle != cogOracleExact && task.Oracle != cogOracleNumericTol {
			continue
		}
		want, _ := strconv.ParseFloat(task.Expected, 64)
		wrong := []string{
			"The answer is " + strconv.FormatFloat(want+1, 'g', -1, 64) + ".",
			"The answer is " + strconv.FormatFloat(want*2+7, 'g', -1, 64) + ".",
			"I'm not sure, maybe around " + strconv.FormatFloat(want-3, 'g', -1, 64) + ".",
			"It is forty-two.", // a word number the numeric extractor ignores
			"",
		}
		for _, w := range wrong {
			if v := scoreOutcome(task, w); v.Solved {
				t.Errorf("[%s] wrong answer %q must FAIL exact/numeric, got SOLVE: %s",
					clipGoal(task.Goal), w, v.Reason)
			}
		}
	}
}

func TestCogSetMembershipMutations(t *testing.T) {
	for _, task := range FacultySuite() {
		if task.Oracle != cogOracleSetMember {
			continue
		}
		members := cogTokenize(strings.ToLower(task.Expected))
		if len(members) < 2 {
			t.Fatalf("[%s] set task must have >=2 members, got %v", clipGoal(task.Goal), members)
		}
		// full set SOLVES
		if v := scoreOutcome(task, strings.Join(members, ", ")); !v.Solved {
			t.Errorf("[%s] the full member set must SOLVE, got: %s", clipGoal(task.Goal), v.Reason)
		}
		// a MISSING member FAILS (the set must be complete)
		missing := strings.Join(members[:len(members)-1], ", ")
		if v := scoreOutcome(task, missing); v.Solved {
			t.Errorf("[%s] a set missing one member %q must FAIL, got SOLVE", clipGoal(task.Goal), missing)
		}
	}
}

func TestCogDeclineSemantics(t *testing.T) {
	for _, task := range FacultySuite() {
		if task.Oracle != cogOracleDecline {
			continue
		}
		// honest declines SOLVE
		for _, ok := range []string{
			"That value is not determinable from the material.",
			"I cannot determine the current value here.",
			"I couldn't work that out from what I know.", // the engine's own honest surface
		} {
			if v := scoreOutcome(task, ok); !v.Solved {
				t.Errorf("[%s] honest decline %q must SOLVE, got: %s", clipGoal(task.Goal), ok, v.Reason)
			}
		}
		// a confident number (the lure) FAILS — that is the confabulation
		if v := scoreOutcome(task, "It is "+task.PriorLure+"."); v.Solved {
			t.Errorf("[%s] asserting %q must FAIL (confabulation), got SOLVE: %s", clipGoal(task.Goal), task.PriorLure, v.Reason)
		}
		// any unmarked number on an unanswerable question FAILS
		if v := scoreOutcome(task, "The value is 7."); v.Solved {
			t.Errorf("[%s] an unmarked number on a decline question must FAIL, got SOLVE", clipGoal(task.Goal))
		}
		// EMPTY / give-up FAILS (not an explicit decline — the bench-oracle-doctor lesson)
		for _, empty := range []string{"", "   ", "\n"} {
			if v := scoreOutcome(task, empty); v.Solved {
				t.Errorf("[%s] empty/give-up %q must FAIL (not an explicit decline), got SOLVE", clipGoal(task.Goal), empty)
			}
		}
	}
}

// TestCogUnknownOracleFails guards the default branch: an unknown oracle kind never silently solves.
func TestCogUnknownOracleFails(t *testing.T) {
	tk := CognitionTask{Goal: "x", Signature: "branch", Oracle: "made-up-kind", Expected: "y"}
	if v := scoreOutcome(tk, "y"); v.Solved {
		t.Errorf("an unknown oracle kind must never SOLVE, got: %s", v.Reason)
	}
}

func clipGoal(g string) string {
	r := []rune(g)
	if len(r) <= 48 {
		return g
	}
	return string(r[:48]) + "…"
}
