package external

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/bench/realhard"
)

// loader_test.go — the external-bank ADAPTER test (A1). It proves the format adapter
// (1) decodes the checked-in ARC-AGI-2 / GAIA fixtures and maps every field onto the
// internal realhard.Task + OracleKind shape, (2) round-trips a converted task through
// the EXISTING realhard A/B runner on the offline `test` backend (no model), and
// (3) rejects malformed banks loudly (the strict-validation contract).

// TestLoadFixturesConvertCorrectly: the two checked-in fixtures decode and every field
// maps to the internal shape — capability, oracle kind, expected, normalizer, tolerance,
// lure, materials, and the id_prefix namespacing.
func TestLoadFixturesConvertCorrectly(t *testing.T) {
	arc, err := LoadFile(filepath.Join("testdata", "arc-agi-2.sample.json"))
	if err != nil {
		t.Fatalf("load arc fixture: %v", err)
	}
	if len(arc) != 2 {
		t.Fatalf("arc fixture: want 2 tasks, got %d", len(arc))
	}
	// id_prefix "arc" namespaces every id; sorted by ID.
	byID := map[string]realhard.Task{}
	for _, tk := range arc {
		if !strings.HasPrefix(tk.ID, "arc-") {
			t.Errorf("arc task id %q should carry the id_prefix 'arc-'", tk.ID)
		}
		byID[tk.ID] = tk
	}
	g1, ok := byID["arc-agi2-eval-demo-01"]
	if !ok {
		t.Fatalf("missing converted task arc-agi2-eval-demo-01; got %v", keys(byID))
	}
	if g1.Oracle != realhard.OracleExact {
		t.Errorf("grader exact -> OracleExact; got %q", g1.Oracle)
	}
	if g1.Expected != "1,0;0,1" || g1.Normalizer != "token" {
		t.Errorf("expected/normalizer mismatch: %q / %q", g1.Expected, g1.Normalizer)
	}
	if g1.PriorLure != "0,1;1,0" {
		t.Errorf("lure -> PriorLure mismatch: %q", g1.PriorLure)
	}
	if g1.Capability != realhard.CapMultiHopGrounding {
		t.Errorf("capability mismatch: %q", g1.Capability)
	}
	// the grid-bearing item carries its material file.
	g2 := byID["arc-agi2-eval-demo-02"]
	if got := g2.Materials["grid.txt"]; got != "0,1;2,3" {
		t.Errorf("material grid.txt not carried through: %q", got)
	}

	gaia, err := LoadFile(filepath.Join("testdata", "gaia.sample.json"))
	if err != nil {
		t.Fatalf("load gaia fixture: %v", err)
	}
	if len(gaia) != 2 {
		t.Fatalf("gaia fixture: want 2 tasks, got %d", len(gaia))
	}
	// no id_prefix on the gaia bank -> ids unchanged.
	gByID := map[string]realhard.Task{}
	for _, tk := range gaia {
		gByID[tk.ID] = tk
	}
	num := gByID["gaia-l1-demo-01"]
	if num.Oracle != realhard.OracleNumericTolerance || num.Expected != "37" || num.Tolerance != 0.5 {
		t.Errorf("numeric-tolerance mapping mismatch: oracle=%q expected=%q tol=%g", num.Oracle, num.Expected, num.Tolerance)
	}
	if len(num.Materials) != 2 {
		t.Errorf("gaia multi-file item should carry 2 materials, got %d", len(num.Materials))
	}
	dec := gByID["gaia-l2-demo-02"]
	if dec.Oracle != realhard.OracleDecline {
		t.Errorf("decline grader -> OracleDecline; got %q", dec.Oracle)
	}
	if dec.Expected != "" {
		t.Errorf("decline task must clear Expected (got %q)", dec.Expected)
	}
	if dec.Capability != realhard.CapAntiConfabulation {
		t.Errorf("decline item capability mismatch: %q", dec.Capability)
	}
}

// TestExternalBankRoundTripsThroughRealhardRunner is the load-bearing A1 DoD: a CONVERTED
// external task runs through the EXISTING realhard bare-vs-harness runner (RunBare +
// RunHarness) on the offline `test` backend and is scored by the SAME deterministic
// realhard oracle — proving the adapter produces tasks the runner consumes with no new
// runner code. (The `test` double is canned content, so the SOLVE outcome is not a
// capability signal — the WIRING is: bare + harness both run, produce a verdict, and the
// oracle scores the converted task.)
func TestExternalBankRoundTripsThroughRealhardRunner(t *testing.T) {
	tasks, err := LoadFile(filepath.Join("testdata", "gaia.sample.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := func(seed int64, temp float64) backends.Backend { return backends.NewTest() }
	for _, tk := range tasks {
		// ARM A — bare (one Generate call, no tools): always produces a scored verdict.
		bare := realhard.RunBare(tk, factory, 1729)
		if bare.TaskID != tk.ID || bare.Arm != realhard.ArmBare {
			t.Errorf("%s: bare result mislabeled (id=%q arm=%q)", tk.ID, bare.TaskID, bare.Arm)
		}
		// ARM B — harness (full engine over the materialized workspace): grounds the
		// converted task's Materials and is scored by the converted task's oracle.
		harn, err := realhard.RunHarness(tk, factory, 1729, 40, t.TempDir())
		if err != nil {
			t.Fatalf("%s: harness run errored: %v", tk.ID, err)
		}
		if harn.TaskID != tk.ID || harn.Arm != realhard.ArmHarness {
			t.Errorf("%s: harness result mislabeled (id=%q arm=%q)", tk.ID, harn.TaskID, harn.Arm)
		}
		// the verdict reason is always populated (the oracle ran on the converted task).
		if strings.TrimSpace(harn.Verdict.Reason) == "" {
			t.Errorf("%s: harness verdict has no reason — the oracle did not score the converted task", tk.ID)
		}
	}
}

// TestExternalBankRejectsMalformed: the strict-validation contract — a bad schema,
// an empty bank, an unknown grader, a duplicate id, and a missing required field each
// fail LOUDLY (never a silently-dropped or mis-mapped task).
func TestExternalBankRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"bad schema", `{"schema":"nope","items":[{"id":"a","prompt":"p","grader":"exact","expected":"x"}]}`, "schema"},
		{"empty items", `{"schema":"thought-external-bank/v1","items":[]}`, "no items"},
		{"unknown grader", `{"schema":"thought-external-bank/v1","items":[{"id":"a","prompt":"p","grader":"fuzzy","expected":"x"}]}`, "unknown grader"},
		{"missing expected", `{"schema":"thought-external-bank/v1","items":[{"id":"a","prompt":"p","grader":"exact"}]}`, "requires a non-empty expected"},
		{"missing prompt", `{"schema":"thought-external-bank/v1","items":[{"id":"a","grader":"decline"}]}`, "missing prompt"},
		{"unknown capability", `{"schema":"thought-external-bank/v1","items":[{"id":"a","prompt":"p","capability":"telepathy","grader":"decline"}]}`, "unknown capability"},
		{"unknown field", `{"schema":"thought-external-bank/v1","items":[{"id":"a","prompt":"p","grader":"decline","mystery":1}]}`, "unknown field"},
		{"duplicate id", `{"schema":"thought-external-bank/v1","items":[{"id":"a","prompt":"p","grader":"decline"},{"id":"a","prompt":"q","grader":"decline"}]}`, "duplicate id"},
	}
	for _, c := range cases {
		_, err := LoadReader(strings.NewReader(c.json))
		if err == nil {
			t.Errorf("%s: expected an error, got nil", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q should mention %q", c.name, err.Error(), c.want)
		}
	}
}

// TestExternalBankDeclineRoundTrip: a decline task converts and scores SOLVE when the
// answer is an honest decline (the realhard decline oracle), proving the decline grader
// maps end-to-end through the runner's scorer.
func TestExternalBankDeclineRoundTrip(t *testing.T) {
	tasks, err := LoadReader(strings.NewReader(`{
	  "schema":"thought-external-bank/v1","source":"gaia",
	  "items":[{"id":"d1","prompt":"What is the missing value?","grader":"decline","lure":"42"}]
	}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Oracle != realhard.OracleDecline {
		t.Fatalf("decline task mis-converted: %+v", tasks)
	}
	// the realhard decline oracle credits an explicit honest decline.
	if v := realhard.Score(tasks[0], "This value is not specified in the material."); !v.Solved {
		t.Errorf("an honest decline should score SOLVE on a decline task; got %q", v.Reason)
	}
	// and FAILS a confabulated lure.
	if v := realhard.Score(tasks[0], "The answer is 42."); v.Solved {
		t.Errorf("asserting the lure must FAIL a decline task")
	}
}

func keys(m map[string]realhard.Task) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
