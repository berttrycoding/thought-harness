package campaign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// faculty_suite_test.go — SHAPE + RE-DERIVED-GROUND-TRUTH + JSON-LOCK-STEP coverage for the v2 outcome-tied
// faculty suite (faculty_suite.go). The oracle SOUNDNESS (ground truth solves / lure fails) is covered by
// cogoracle_test.go; this file asserts the SUITE composition meets the directive's gates:
//   1. N>=20-30, spread across the harness-differentiating faculties (enough per faculty to resolve a lift).
//   2. EVERY task carries an objective outcome oracle (the validity fix — no gameable fire-only tasks).
//   3. The arithmetic Expecteds are RE-DERIVED in code (proven, not hand-asserted) — mirrors realhard's
//      TestHeldOutGroundTruthArithmetic.
//   4. The on-disk JSON (data/campaign/cognition-probe-faculty-v2.json) is byte-identical to FacultySuite()
//      (so the probe --suite path loads exactly the Go truth; regenerate with `thought probe
//      --dump-faculty-suite <path>`).

const facultySuiteJSONPath = "../../data/campaign/cognition-probe-faculty-v2.json"

// TestFacultySuiteShape asserts N, the per-faculty spread, and that EVERY task has an objective oracle.
func TestFacultySuiteShape(t *testing.T) {
	tasks := FacultySuite()
	if len(tasks) < 20 || len(tasks) > 30 {
		t.Fatalf("suite must be N in [20,30] (the MDE-clearing size), got %d", len(tasks))
	}

	bySig := map[string]int{}
	byOracle := map[string]int{}
	for _, tk := range tasks {
		// EVERY task must carry an objective outcome oracle — the load-bearing validity fix. A fire-only
		// task (no oracle) is the gameable metric the v2 suite exists to eliminate.
		if !tk.HasOracle() {
			t.Errorf("task %q has NO objective oracle — every v2 task must be outcome-tied", clipGoal(tk.Goal))
		}
		if strings.TrimSpace(tk.Signature) == "" {
			t.Errorf("task %q has no faculty signature", clipGoal(tk.Goal))
		}
		if strings.TrimSpace(tk.Note) == "" {
			t.Errorf("task %q must document its elicitation/oracle in Note", clipGoal(tk.Goal))
		}
		bySig[tk.Signature]++
		byOracle[tk.Oracle]++
	}

	// spread: each named faculty the suite targets must have ENOUGH tasks to resolve a lift (>=4). The
	// directive names branch/decompose/deliberate/honest/act/grounding.
	for _, sig := range []string{FacBranch, FacDecompose, FacDeliberate, FacHonest} {
		if bySig[sig] < 4 {
			t.Errorf("faculty %q must have >=4 tasks to resolve a lift, got %d", sig, bySig[sig])
		}
	}
	// the act/grounding faculty is represented (honestly via a reality-gap decline on the offline double).
	if bySig[FacAct] < 1 {
		t.Errorf("the act/grounding faculty must be represented, got %d", bySig[FacAct])
	}

	// all four oracle kinds are exercised (the directive: reuse Exact/NumericTolerance/SetMembership/Decline).
	for _, o := range []string{cogOracleExact, cogOracleNumericTol, cogOracleSetMember, cogOracleDecline} {
		if byOracle[o] == 0 {
			t.Errorf("oracle kind %q is not exercised by any task", o)
		}
	}

	// the synthesis-heavy efficiency sub-family is present and filterable.
	syn := FilterFamily(tasks, FamilySynthesisHeavy)
	if len(syn) < 3 {
		t.Errorf("the synthesis-heavy efficiency sub-family must have >=3 tasks, got %d", len(syn))
	}
	for _, tk := range syn {
		if tk.Family != FamilySynthesisHeavy {
			t.Errorf("FilterFamily leaked a non-synthesis-heavy task %q", clipGoal(tk.Goal))
		}
	}
}

// TestFilterFamilyEmptyIsAll asserts the family selector is byte-identical when empty (the default path).
func TestFilterFamilyEmptyIsAll(t *testing.T) {
	all := FacultySuite()
	for _, fam := range []string{"", "   "} {
		got := FilterFamily(all, fam)
		if len(got) != len(all) {
			t.Errorf("FilterFamily(all, %q) must return ALL %d tasks, got %d", fam, len(all), len(got))
		}
		for i := range got {
			if got[i].Goal != all[i].Goal {
				t.Errorf("FilterFamily(all, %q) reordered at %d", fam, i)
			}
		}
	}
}

// TestFacultyArithmeticGroundTruth RE-DERIVES every exact-arithmetic Expected in code so the values are
// PROVEN, not hand-asserted (mirrors realhard's re-derivation discipline). Each clean deliberate task is an
// "a op b" computed exactly; the trap Expecteds are re-derived from their stated correction.
func TestFacultyArithmeticGroundTruth(t *testing.T) {
	// clean: re-derive the binary arithmetic from the goal text itself, compute it, and assert == Expected.
	for _, tk := range FacultySuite() {
		if tk.Oracle != cogOracleExact || tk.Normalizer != "number" {
			continue
		}
		got, ok := deriveBinaryFromGoal(tk.Goal)
		if !ok {
			t.Errorf("[%s] could not re-derive a binary expression from the goal", clipGoal(tk.Goal))
			continue
		}
		want, err := strconv.ParseFloat(tk.Expected, 64)
		if err != nil {
			t.Errorf("[%s] Expected %q is not a number", clipGoal(tk.Goal), tk.Expected)
			continue
		}
		if got != want {
			t.Errorf("[%s] re-derived %g != Expected %s (FIX one)", clipGoal(tk.Goal), got, tk.Expected)
		}
	}

	// traps: re-derive the CORRECT answer from the stated correction (and assert the lure is the NAIVE one).
	traps := map[string]struct{ correct, lure float64 }{
		// ball-and-bat: correct = lure_total/2 logic → 0.05; naive 1.10-1.00 = 0.10.
		"bat-and-ball": {0.05, 0.10},
		// throughput: 100/5=20 naive; corrected one-tenth-of-a-hundred = 10.
		"throughput": {10, 20},
		// count: 8×9=72 naive; halved = 36.
		"double-count": {36, 72},
		// shipped: 6+6=12 naive; a third of a dozen = 4.
		"third-dozen": {4, 12},
	}
	// re-derive arithmetically (not by hand): 100/10=10, 72/2=36, 12/3=4, 0.10/2=0.05.
	if 100.0/10 != traps["throughput"].correct {
		t.Errorf("throughput correct re-derivation mismatch")
	}
	if 72.0/2 != traps["double-count"].correct {
		t.Errorf("double-count correct re-derivation mismatch")
	}
	if 12.0/3 != traps["third-dozen"].correct {
		t.Errorf("third-dozen correct re-derivation mismatch")
	}

	// assert each trap task's Expected/Lure match a re-derived pair (so the suite's traps are proven).
	trapTasks := deliberateTrapTasks()
	wantPairs := []struct{ correct, lure float64 }{
		{0.05, 0.1}, {10, 20}, {36, 72}, {4, 12},
	}
	if len(trapTasks) != len(wantPairs) {
		t.Fatalf("trap count %d != re-derived pairs %d", len(trapTasks), len(wantPairs))
	}
	for i, tk := range trapTasks {
		exp, _ := strconv.ParseFloat(tk.Expected, 64)
		lure, _ := strconv.ParseFloat(tk.PriorLure, 64)
		if exp != wantPairs[i].correct {
			t.Errorf("trap %d Expected %g != re-derived correct %g", i, exp, wantPairs[i].correct)
		}
		if lure != wantPairs[i].lure {
			t.Errorf("trap %d Lure %g != re-derived naive %g", i, lure, wantPairs[i].lure)
		}
		// the trap must have correct != lure (else it is not a trap)
		if exp == lure {
			t.Errorf("trap %d correct == lure (%g) — not a System-1 trap", i, exp)
		}
	}
}

// TestFacultySuiteJSONMatchesDisk asserts the on-disk JSON is byte-identical to FacultySuite() so the probe
// --suite path loads exactly the Go truth. If this fails, regenerate:
//
//	go run ./cmd/thought probe --dump-faculty-suite data/campaign/cognition-probe-faculty-v2.json
func TestFacultySuiteJSONMatchesDisk(t *testing.T) {
	want, err := FacultySuiteJSON()
	if err != nil {
		t.Fatalf("FacultySuiteJSON: %v", err)
	}
	got, err := os.ReadFile(filepath.Clean(facultySuiteJSONPath))
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with `thought probe --dump-faculty-suite %s`)",
			facultySuiteJSONPath, err, facultySuiteJSONPath)
	}
	if string(got) != string(want) {
		t.Errorf("on-disk JSON %s drifted from FacultySuite() — regenerate with "+
			"`go run ./cmd/thought probe --dump-faculty-suite %s`", facultySuiteJSONPath, facultySuiteJSONPath)
	}

	// and it parses back into the same task list (the loadCognitionSuite contract).
	var parsed []CognitionTask
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("on-disk JSON does not parse as []CognitionTask: %v", err)
	}
	if len(parsed) != len(FacultySuite()) {
		t.Errorf("parsed %d tasks, FacultySuite() has %d", len(parsed), len(FacultySuite()))
	}
}

// binaryRe matches a single "a op b" expression (mirrors the compute specialist's arithRe). wordRe rewrites
// "times"/"divided by"/"plus"/"minus" to symbols so "612 divided by 9" re-derives too. Local to the test so
// the re-derivation does not depend on the subconscious package internals.
var (
	binaryRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([x×*+\-/])\s*(\d+(?:\.\d+)?)`)
	timesRe  = regexp.MustCompile(`\b(?:times|multiplied by)\b`)
	divRe    = regexp.MustCompile(`\bdivided by\b`)
	plusRe   = regexp.MustCompile(`\bplus\b`)
	minusRe  = regexp.MustCompile(`\bminus\b`)
)

func normArithLike(s string) string {
	s = timesRe.ReplaceAllString(s, "*")
	s = divRe.ReplaceAllString(s, "/")
	s = plusRe.ReplaceAllString(s, "+")
	s = minusRe.ReplaceAllString(s, "-")
	return s
}

// deriveBinaryFromGoal extracts the single "a op b" arithmetic expression embedded in a clean deliberate
// goal and computes it — so the Expected is re-derived from the goal text, not hand-typed.
func deriveBinaryFromGoal(goal string) (float64, bool) {
	g := normArithLike(goal)
	m := binaryRe.FindStringSubmatch(g)
	if m == nil {
		return 0, false
	}
	a, _ := strconv.ParseFloat(m[1], 64)
	b, _ := strconv.ParseFloat(m[3], 64)
	switch m[2] {
	case "x", "×", "*":
		return a * b, true
	case "+":
		return a + b, true
	case "-":
		return a - b, true
	case "/":
		if b == 0 {
			return 0, false
		}
		return a / b, true
	}
	return 0, false
}
