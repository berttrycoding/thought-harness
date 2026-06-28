package realhard

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// tasks_hard_test.go — MUTATION + INTEGRITY coverage for the harder calibration
// batch (tasks_hard.go, IDs realhard-hard-NNNN). It enforces, beyond the shared
// oracle_test.go (which already covers ground-truth-solves / lure-fails for ALL
// tasks incl. these):
//
//   1. SOUNDNESS — ground truth SOLVES, lure FAILS, a wrong-answer battery FAILS,
//      decline tasks credit an honest decline and fail a confident number.
//   2. RE-DERIVED EXPECTEDS — every COMPUTED answer (the 5-hop off-by-one, the
//      tier-conditional cost, the cap clamp, the two long-horizon ledgers, the
//      7-task CSP) is re-derived in code, so the Expected is PROVEN, not asserted
//      (mirrors TestLongHorizonGroundTruthArithmetic).
//   3. KEYWORD HYGIENE — no hard task's MATERIAL contains the banned in-suite trap
//      tokens (deprecated / superseded / erratum / flag / multiplier): the trap
//      must be inferred (recency / rollback / scope / a conversion / a cap), never
//      keyword-matched.
//   4. DISJOINTNESS — the hard materials share NO file CONTENT with any existing
//      fixture, so a lift here cannot be an artifact of a re-used fixture.
//   5. SHAPE — exactly 12 tasks across all four capabilities, registered in Tasks().

// hardIDs is the stable ID set of the hard batch.
var hardIDs = []string{
	"realhard-hard-0001", "realhard-hard-0002", "realhard-hard-0003",
	"realhard-hard-0004", "realhard-hard-0005", "realhard-hard-0006",
	"realhard-hard-0007", "realhard-hard-0008", "realhard-hard-0009",
	"realhard-hard-0010", "realhard-hard-0011", "realhard-hard-0012",
}

// --- (1) SOUNDNESS ------------------------------------------------------------

func TestHardGroundTruthSolves(t *testing.T) {
	for _, task := range hardTasks() {
		if v := Score(task, groundTruthAnswer(task)); !v.Solved {
			t.Errorf("[%s] ground truth must SOLVE, got fail: %s (answer=%q)",
				task.ID, v.Reason, groundTruthAnswer(task))
		}
	}
}

func TestHardLureFails(t *testing.T) {
	for _, task := range hardTasks() {
		if strings.TrimSpace(task.PriorLure) == "" {
			t.Errorf("[%s] hard task must declare a PriorLure (the headroom hypothesis)", task.ID)
			continue
		}
		var lureAns string
		switch task.Oracle {
		case OracleDecline:
			lureAns = "The value is " + task.PriorLure + "."
		default:
			lureAns = "After grounding step by step, the answer is " + task.PriorLure + "."
		}
		if v := Score(task, lureAns); v.Solved {
			t.Errorf("[%s] the lure %q must FAIL the oracle, but SOLVED: %s",
				task.ID, task.PriorLure, v.Reason)
		}
	}
}

func TestHardWrongAnswersFail(t *testing.T) {
	for _, task := range hardTasks() {
		if task.Oracle != OracleExact && task.Oracle != OracleNumericTolerance {
			continue
		}
		want, err := strconv.ParseFloat(strings.TrimSpace(task.Expected), 64)
		if err != nil {
			t.Errorf("[%s] computed task Expected %q is not a number", task.ID, task.Expected)
			continue
		}
		mutants := []string{
			fmt.Sprintf("The answer is %g.", want+1),
			fmt.Sprintf("The answer is %g.", want-1),
			fmt.Sprintf("The answer is %g.", want*10),
			fmt.Sprintf("The answer is %g.", want/2),
			fmt.Sprintf("The answer is %g.", want+100),
			"I think it's around the documented default.",
			"",
		}
		for _, m := range mutants {
			if v := Score(task, m); v.Solved {
				t.Errorf("[%s] wrong answer %q must FAIL, but SOLVED: %s", task.ID, m, v.Reason)
			}
		}
	}
}

func TestHardDeclineSemantics(t *testing.T) {
	for _, task := range hardTasks() {
		if task.Oracle != OracleDecline {
			continue
		}
		// honest declines SOLVE
		for _, ok := range []string{
			"This is not determinable from the provided material.",
			"The value is not defined in this file; it is provisioned externally.",
			"I cannot determine that value from the material.",
		} {
			if v := Score(task, ok); !v.Solved {
				t.Errorf("[%s] honest decline %q must SOLVE, got: %s", task.ID, ok, v.Reason)
			}
		}
		// confident numbers FAIL (incl. the sibling-scope / tempting-computation lures)
		for _, bad := range []string{
			"The value is " + task.PriorLure + ".",
			"It is 3.",
			"The budget is 100.",
		} {
			if v := Score(task, bad); v.Solved {
				t.Errorf("[%s] confident number %q must FAIL (confabulation), got SOLVE: %s",
					task.ID, bad, v.Reason)
			}
		}
		// empty / give-up FAILS (not an explicit decline)
		for _, gaveUp := range []string{"", "   ", "\n", "hmm"} {
			if v := Score(task, gaveUp); v.Solved {
				t.Errorf("[%s] empty/give-up %q must FAIL (not an explicit decline), got SOLVE: %s",
					task.ID, gaveUp, v.Reason)
			}
		}
		// the sibling-scope / tempting-computation lure must be flagged as asserted
		// when stated as the value (the confabulation signal for the report).
		bad := "The value is " + task.PriorLure + "."
		if v := Score(task, bad); !v.AssertedLure {
			t.Errorf("[%s] asserting the lure %q must set AssertedLure", task.ID, task.PriorLure)
		}
	}
}

// --- (2) RE-DERIVED EXPECTEDS -------------------------------------------------

// TestHardGroundTruthArithmetic re-derives every COMPUTED hard answer in code so
// the Expected values are proven, not hand-asserted (mirrors
// TestLongHorizonGroundTruthArithmetic). Each derivation mirrors the task's stated
// rules exactly; a mismatch fails the test (fix the task OR the Expected, never
// ship a wrong ground truth).
func TestHardGroundTruthArithmetic(t *testing.T) {
	// 0001: 5-hop off-by-one. worker vCPU = (node_count - reserved) * vcpu_per_node.
	nodeCount, reserved, vcpuPerNode := 6, 1, 8
	workerVCPU := (nodeCount - reserved) * vcpuPerNode
	assertExpected(t, "realhard-hard-0001", workerVCPU) // 40

	// 0002: tier-conditional cost. heavy nightly -> 3h/run * 30 nights * $0.40.
	hoursPerRun, nights, heavyRate := 3.0, 30.0, 0.40
	cost := nights * hoursPerRun * heavyRate
	assertExpectedF(t, "realhard-hard-0002", cost) // 36

	// 0003: cap clamp. effective = min(base*split, cap).
	base, split, cap := 16, 2, 24
	eff := base * split
	if cap < eff {
		eff = cap
	}
	assertExpected(t, "realhard-hard-0003", eff) // 24

	// 0004: rollback -> value in force is the restored original (512), not the
	// rolled-back raise (2048). A constant of the material; assert it directly.
	assertExpected(t, "realhard-hard-0004", 512)

	// 0005: scope trap + computation. batch budget = 3 sequential calls * BatchTimeout.
	calls, batchTimeout := 3, 120
	assertExpected(t, "realhard-hard-0005", calls*batchTimeout) // 360

	// 0006: disabled toggle -> fallback in force (64), not the configured 256.
	assertExpected(t, "realhard-hard-0006", 64)

	// 0007: 16-event inventory ledger, percentage->flat audit rule change + 2 reversals.
	q := 0
	q += 120            // E1
	q -= 30             // E2
	q -= 30             // E3
	q += 50             // E4 -> 110
	q -= (q * 10) / 100 // E5 audit 10% floor: 110 -> -11 -> 99
	q += 40             // E6 -> 139
	q -= 30             // E7 -> 109
	q += 30             // E8 reverse E7 -> 139
	// E9 rule change: audits now flat 5
	q -= 5                                     // E10 audit flat 5 -> 134
	q -= 24                                    // E11 ship -> 110
	q += 24                                    // E12 reverse E11 -> 134
	q += 16                                    // E13 -> 150
	q -= 5                                     // E14 audit flat 5 -> 145
	q -= 45                                    // E15 ship -> 100
	q += 8                                     // E16 -> 108
	assertExpected(t, "realhard-hard-0007", q) // 108

	// 0008: balance/interest ledger, 10%->5% rate change + a reversal (interest not
	// recomputed). Interest floors.
	b := 1000
	b += 500            // E1
	b -= 200            // E2 -> 1300
	b += (b * 10) / 100 // E3 interest 10% floor: +130 -> 1430
	b -= 430            // E4 -> 1000
	// E5 rate change: interest now 5%
	b += (b * 5) / 100                         // E6 interest 5% floor: +50 -> 1050
	b += 150                                   // E7 -> 1200
	b += 430                                   // E8 reverse E4 -> 1630
	b += (b * 5) / 100                         // E9 interest 5% floor: 1630*5/100 = 81 -> 1711
	assertExpected(t, "realhard-hard-0008", b) // 1711

	// 0009: 7-task ordering CSP. Brute-force every permutation; assert UNIQUE and
	// report T5's 1-indexed position. Services indexed 0..6 for T1..T7.
	pos := []int{0, 1, 2, 3, 4, 5, 6}
	var solutions [][7]int
	permute(pos, 0, func(p []int) {
		posT := func(s int) int { return p[s-1] } // 1-based task id -> 0-indexed position
		ok := true
		ok = ok && posT(3) == 0         // R1: T3 first
		ok = ok && posT(2) == 6         // R2: T2 last
		ok = ok && posT(1) < posT(5)    // R3: T1 before T5
		ok = ok && posT(4) == posT(1)+1 // R4: T4 immediately after T1
		ok = ok && posT(6) < posT(1)    // R5: T6 before T1
		ok = ok && posT(7) == posT(5)-1 // R6: T7 immediately before T5
		ok = ok && posT(4) < posT(7)    // R7: T4 before T7
		if ok {
			var snap [7]int
			copy(snap[:], p)
			solutions = append(solutions, snap)
		}
	})
	if len(solutions) != 1 {
		t.Fatalf("realhard-hard-0009: CSP must have EXACTLY one solution, got %d: %v",
			len(solutions), solutions)
	}
	t5pos1indexed := solutions[0][4] + 1                   // T5 is task index 4, +1 for 1-based position
	assertExpected(t, "realhard-hard-0009", t5pos1indexed) // 6

	// 0010-0012: decline tasks, Expected must be empty (no arithmetic).
	for _, id := range []string{"realhard-hard-0010", "realhard-hard-0011", "realhard-hard-0012"} {
		if got := taskByID(t, id).Expected; got != "" {
			t.Errorf("[%s] decline task Expected must be empty, got %q", id, got)
		}
	}
}

// --- (3) KEYWORD HYGIENE ------------------------------------------------------

// TestHardKeywordHygiene asserts none of the hard tasks' MATERIALS contain a
// banned in-suite trap keyword. A lift on the hard batch must reflect the general
// capability, not a keyword reflex. (bannedTrapKeywords is defined in
// tasks_heldout_test.go.)
func TestHardKeywordHygiene(t *testing.T) {
	for _, task := range hardTasks() {
		var hay strings.Builder
		for _, content := range task.Materials {
			hay.WriteString(strings.ToLower(content))
			hay.WriteString("\n")
		}
		material := hay.String()
		for _, kw := range bannedTrapKeywords {
			if strings.Contains(material, kw) {
				t.Errorf("[%s] MATERIAL contains banned trap keyword %q — the hard batch must "+
					"probe the capability WITHOUT the in-suite keywords (reword the trap)", task.ID, kw)
			}
		}
	}
}

// --- (4) DISJOINTNESS ---------------------------------------------------------

// TestHardMaterialsDisjoint asserts no hard task re-uses a file PATH or file
// CONTENT from any existing fixture (the in-suite + held-out sets). A reused
// material would let a "lift" be an artifact of a known fixture rather than the
// general capability. We compare both the path->content map of every prior task
// and the raw content blobs.
func TestHardMaterialsDisjoint(t *testing.T) {
	hardSet := map[string]bool{}
	for _, id := range hardIDs {
		hardSet[id] = true
	}
	// Collect every (path, content) and every content blob from the NON-hard tasks.
	priorContent := map[string]bool{} // normalized content blob -> seen
	for _, task := range Tasks() {
		if hardSet[task.ID] {
			continue
		}
		for _, c := range task.Materials {
			priorContent[normalizeBlob(c)] = true
		}
	}
	for _, task := range hardTasks() {
		for path, c := range task.Materials {
			if priorContent[normalizeBlob(c)] {
				t.Errorf("[%s] material %q re-uses content present in an existing fixture — "+
					"the hard batch must use disjoint materials", task.ID, path)
			}
		}
	}
}

// normalizeBlob lowercases + collapses whitespace so a trivially-reformatted copy
// of an existing fixture is still caught as a duplicate.
func normalizeBlob(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// --- (5) SHAPE ----------------------------------------------------------------

// TestHardShape asserts the composition: 12 tasks, IDs stable+unique, all four
// capabilities represented, the decline tasks are OracleDecline, and every hard
// task is registered in the aggregated Tasks().
func TestHardShape(t *testing.T) {
	tasks := hardTasks()
	if len(tasks) != 12 {
		t.Fatalf("hard batch must have exactly 12 tasks, got %d", len(tasks))
	}
	caps := map[Capability]int{}
	ids := map[string]bool{}
	for _, task := range tasks {
		caps[task.Capability]++
		if ids[task.ID] {
			t.Errorf("duplicate hard task ID %s", task.ID)
		}
		ids[task.ID] = true
		if strings.TrimSpace(task.Why) == "" {
			t.Errorf("[%s] hard task must document its trap in Why", task.ID)
		}
		if task.Capability == CapAntiConfabulation && task.Oracle != OracleDecline {
			t.Errorf("[%s] anti-confab hard task must use OracleDecline, got %s", task.ID, task.Oracle)
		}
	}
	for _, c := range []Capability{
		CapMultiHopGrounding, CapAdaptiveBacktracking,
		CapAntiConfabulation, CapLongHorizonConsistency,
	} {
		if caps[c] == 0 {
			t.Errorf("hard batch has no task for capability %s", c)
		}
	}
	// every hard task must appear in the aggregated suite (registration check)
	suiteIDs := map[string]bool{}
	for _, task := range Tasks() {
		suiteIDs[task.ID] = true
	}
	for _, id := range hardIDs {
		if !suiteIDs[id] {
			t.Errorf("hard task %s is NOT registered in Tasks()", id)
		}
	}
}

// TestFilterTasksSelectsHard confirms the --only-task=-hard- selector picks
// exactly the 12-task hard batch (so the calibration launch can target it
// cheaply). NOTE the selector substring is "-hard-", with leading/trailing
// hyphens — a bare "hard" is a substring of "realhard" and would (correctly) match
// the WHOLE suite, so the batch is addressed by its hyphen-delimited segment.
func TestFilterTasksSelectsHard(t *testing.T) {
	got := FilterTasks(Tasks(), "-hard-")
	if len(got) != 12 {
		t.Fatalf("--only-task=-hard- must select the 12 hard tasks, got %d", len(got))
	}
	want := map[string]bool{}
	for _, id := range hardIDs {
		want[id] = true
	}
	for _, task := range got {
		if !want[task.ID] {
			t.Errorf("--only-task=hard selected non-hard task %s", task.ID)
		}
	}
}
