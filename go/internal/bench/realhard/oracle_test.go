package realhard

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// oracle_test.go — MUTATION-TESTS the oracle (the Phase-0 / oracle-doctor
// requirement: a sound oracle credits the ground truth and rejects wrong answers
// — especially the lure). For every task we assert:
//   - the ground-truth answer SOLVES,
//   - the prior lure FAILS,
//   - a battery of generic wrong answers FAIL,
//   - (decline tasks) an honest decline SOLVES and a confident number FAILS.

// groundTruthAnswer renders the canonical correct answer string for a task — the
// thing a perfect arm would emit. For exact/numeric it is the Expected number;
// for decline it is an explicit honest decline.
func groundTruthAnswer(t Task) string {
	switch t.Oracle {
	case OracleExact, OracleNumericTolerance:
		return "The answer is " + t.Expected + "."
	case OracleSetMembership:
		return t.Expected
	case OracleDecline:
		return "This is not determinable from the material; no value is defined here."
	default:
		return ""
	}
}

func TestGroundTruthSolves(t *testing.T) {
	for _, task := range Tasks() {
		v := Score(task, groundTruthAnswer(task))
		if !v.Solved {
			t.Errorf("[%s] ground truth must SOLVE, got fail: %s (answer=%q)",
				task.ID, v.Reason, groundTruthAnswer(task))
		}
	}
}

func TestPriorLureFails(t *testing.T) {
	for _, task := range Tasks() {
		if strings.TrimSpace(task.PriorLure) == "" {
			continue
		}
		// Emit the lure the way a confident bare model would: as the conclusion.
		var lureAns string
		switch task.Oracle {
		case OracleDecline:
			lureAns = "The value is " + task.PriorLure + "."
		default:
			lureAns = "After reasoning, the answer is " + task.PriorLure + "."
		}
		v := Score(task, lureAns)
		if v.Solved {
			t.Errorf("[%s] the PRIOR LURE %q must FAIL the oracle, but it SOLVED: %s",
				task.ID, task.PriorLure, v.Reason)
		}
	}
}

func TestWrongAnswersFail(t *testing.T) {
	// A battery of generic mutations that must never pass an exact/numeric oracle.
	for _, task := range Tasks() {
		if task.Oracle != OracleExact && task.Oracle != OracleNumericTolerance {
			continue
		}
		want, _ := strconv.ParseFloat(strings.TrimSpace(task.Expected), 64)
		mutants := []string{
			fmt.Sprintf("The answer is %g.", want+1),
			fmt.Sprintf("The answer is %g.", want-1),
			fmt.Sprintf("The answer is %g.", want*10),
			fmt.Sprintf("The answer is %g.", want/2),
			"I think it's probably around the documented default.",
			"",
		}
		// for numeric-tolerance, a mutation just inside tolerance SHOULD pass, so
		// keep mutants comfortably outside (>= 1 away dominates the small tols here).
		for _, m := range mutants {
			v := Score(task, m)
			if v.Solved {
				t.Errorf("[%s] wrong answer %q must FAIL, but SOLVED: %s", task.ID, m, v.Reason)
			}
		}
	}
}

func TestDeclineOracleSemantics(t *testing.T) {
	for _, task := range Tasks() {
		if task.Oracle != OracleDecline {
			continue
		}
		// honest declines SOLVE
		for _, ok := range []string{
			"This is not determinable from the provided material.",
			"The value is not defined in this file; it comes from an external package.",
			"I cannot determine the current value from the material.",
		} {
			if v := Score(task, ok); !v.Solved {
				t.Errorf("[%s] honest decline %q must SOLVE, got: %s", task.ID, ok, v.Reason)
			}
		}
		// confident numbers FAIL (the confabulation the task probes)
		for _, bad := range []string{
			"The value is 3.",
			"It is approximately 0.5%.",
			"The retry budget is 7.",
		} {
			if v := Score(task, bad); v.Solved {
				t.Errorf("[%s] confident number %q must FAIL (confabulation), got SOLVE: %s",
					task.ID, bad, v.Reason)
			}
		}
		// EMPTY / give-up FAILS — an empty surface (early-quiesce / truncation / error)
		// is a no-answer, NOT an explicit honest decline (bench-oracle-doctor 2026-06-18:
		// crediting "" inflated the harness by scoring the give-up failure as cognition).
		for _, gaveUp := range []string{"", "   ", "\n", "hmm"} {
			if v := Score(task, gaveUp); v.Solved {
				t.Errorf("[%s] empty/give-up %q must FAIL (not an explicit decline), got SOLVE: %s",
					task.ID, gaveUp, v.Reason)
			}
		}
	}
}

// TestNumberExtractionRobustness guards the number parser against the formats a
// real model emits (commas, sentences, multiple numbers where the LAST is the
// conclusion).
func TestNumberExtractionRobustness(t *testing.T) {
	cases := []struct {
		s    string
		want []float64
	}{
		{"21,600,000 KB = 21,600 MB = 21.6 GB -> 21", []float64{21600000, 21600, 21.6, 21}},
		{"The pool is 12.", []float64{12}},
		{"build 3, QA 8, soak 11, release 12, slip to 14", []float64{3, 8, 11, 12, 14}},
		{"no numbers here", nil},
	}
	for _, c := range cases {
		got := extractNumbers(c.s)
		if len(got) != len(c.want) {
			t.Errorf("extractNumbers(%q) = %v, want %v", c.s, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("extractNumbers(%q)[%d] = %g, want %g", c.s, i, got[i], c.want[i])
			}
		}
	}
}

// TestLongHorizonGroundTruthArithmetic re-derives the long-horizon answers in
// code so the Expected values are PROVEN, not hand-asserted (the oracle is only
// as sound as its ground truth). Each derivation mirrors the prompt's rules
// exactly; a mismatch fails the test (fix the task OR the Expected, never ship a
// wrong ground truth).
func TestLongHorizonGroundTruthArithmetic(t *testing.T) {
	// 0001: 12-event ledger with a mid-chain fee rule change + two reversals.
	a := 0
	a += 200 // E1 deposit
	a -= 40  // E2 transfer A->B
	a -= 10  // E3 fee (v1: flat 10)
	a += 25  // E4 transfer C->A
	a += 60  // E5 deposit
	a += 40  // E6 reverse E2 (undo -40)
	a -= 10  // E7 fee (v1: flat 10, before the E8 rule change)
	// E8 rule change: fee now 5
	a -= 70 // E9 transfer A->B
	a -= 5  // E10 fee (v2: 5)
	a += 70 // E11 reverse E9 (undo -70)
	a += 15 // E12 deposit
	assertExpected(t, "realhard-long-0001", a)

	// 0002: 3-stage pipeline, decimal units, floor.
	stage1KB := 4 * 200 * (8 * 3600) // files/s * KB/file * s/day
	stage2KB := stage1KB / 2         // 50% compress
	stage3KB := stage2KB * 3         // 3x replicate
	gb := stage3KB / (1000 * 1000)   // KB -> GB decimal, integer floor
	assertExpected(t, "realhard-long-0002", gb)

	// 0003: 6-service ordering CSP. Brute-force every permutation; assert the
	// solution is UNIQUE and report S4's position (1-indexed). Services indexed
	// 0..5 for S1..S6.
	pos := []int{0, 1, 2, 3, 4, 5}
	var solutions [][6]int
	permute(pos, 0, func(p []int) {
		// p[i] = position (0-indexed) of service S(i+1).
		posS := func(s int) int { return p[s-1] } // 1-based service id
		ok := true
		ok = ok && posS(6) == 0         // R1: S6 first (0-indexed)
		ok = ok && posS(5) == 5         // R2: S5 last (0-indexed)
		ok = ok && posS(1) < posS(2)    // R3
		ok = ok && posS(3) == posS(1)+1 // R4: S3 immediately after S1
		ok = ok && posS(2) < posS(4)    // R5
		ok = ok && posS(4) == posS(5)-1 // R6: S4 immediately before S5
		if ok {
			var snap [6]int
			copy(snap[:], p)
			solutions = append(solutions, snap)
		}
	})
	if len(solutions) != 1 {
		t.Fatalf("realhard-long-0003: CSP must have EXACTLY one solution, got %d: %v",
			len(solutions), solutions)
	}
	s4pos1indexed := solutions[0][3] + 1 // S4 is service index 3, +1 for 1-based position
	assertExpected(t, "realhard-long-0003", s4pos1indexed)

	// backtrack-0002: pricing erratum (10% off 10*20).
	total := 10.0 * 20.0 * 0.90
	assertExpectedF(t, "realhard-back-0002", total)

	// multihop-0003: gold base 600 * burst_v2 multiplier 2.
	assertExpected(t, "realhard-mhop-0003", 600*2)
}

// permute generates every permutation of a, calling fn on each (fn must not
// retain the slice — it is reused).
func permute(a []int, k int, fn func([]int)) {
	if k == len(a) {
		fn(a)
		return
	}
	for i := k; i < len(a); i++ {
		a[k], a[i] = a[i], a[k]
		permute(a, k+1, fn)
		a[k], a[i] = a[i], a[k]
	}
}

func taskByID(t *testing.T, id string) Task {
	for _, task := range Tasks() {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %s not found", id)
	return Task{}
}

func assertExpected(t *testing.T, id string, got int) {
	t.Helper()
	task := taskByID(t, id)
	if task.Expected != strconv.Itoa(got) {
		t.Errorf("[%s] re-derived ground truth = %d, but task.Expected = %q (FIX one of them)",
			id, got, task.Expected)
	}
}

func assertExpectedF(t *testing.T, id string, got float64) {
	t.Helper()
	task := taskByID(t, id)
	want, _ := strconv.ParseFloat(task.Expected, 64)
	if want != got {
		t.Errorf("[%s] re-derived ground truth = %g, but task.Expected = %q (FIX one of them)",
			id, got, task.Expected)
	}
}

// TestSuiteShape sanity-checks the suite composition (8 core + 6 held-out +
// 12 hard-calibration = 26 tasks, all four caps present). The upper bound is a
// loose sanity ceiling, widened when the hard-calibration batch (tasks_hard.go)
// was added to raise T_eff on the saturated suite.
func TestSuiteShape(t *testing.T) {
	tasks := Tasks()
	if len(tasks) < 6 || len(tasks) > 40 {
		t.Errorf("expected 6-40 hard tasks, got %d", len(tasks))
	}
	caps := map[Capability]int{}
	ids := map[string]bool{}
	for _, task := range tasks {
		caps[task.Capability]++
		if ids[task.ID] {
			t.Errorf("duplicate task ID %s", task.ID)
		}
		ids[task.ID] = true
		if task.Oracle == OracleExact && task.Normalizer == "" {
			// allow empty normalizer only for token tasks; flag bare exact-number with no normalizer
		}
	}
	for _, c := range []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapAntiConfabulation, CapLongHorizonConsistency} {
		if caps[c] == 0 {
			t.Errorf("no task for capability %s", c)
		}
	}
}
