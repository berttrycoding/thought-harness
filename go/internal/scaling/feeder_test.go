package scaling

import (
	"os"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// loadBootstrap loads the real bootstrap-001 batch file.
func loadBootstrap(t *testing.T) Batch {
	t.Helper()
	f, err := os.Open("batches/bootstrap-001.jsonl")
	if err != nil {
		t.Fatalf("open bootstrap batch: %v", err)
	}
	defer f.Close()
	b, err := LoadBatch(f)
	if err != nil {
		t.Fatalf("LoadBatch: %v", err)
	}
	return b
}

// TestBootstrapBatchLoads: the real batch parses, every candidate is an operator with provenance and
// links (the bootstrap-substrate discipline), and no kinds were silently dropped.
func TestBootstrapBatchLoads(t *testing.T) {
	b := loadBootstrap(t)
	if len(b.Operators) < 12 {
		t.Fatalf("bootstrap-001 should carry the full cell-filling batch, got %d", len(b.Operators))
	}
	if b.Skipped != 0 {
		t.Fatalf("no lines should be skipped, got %d", b.Skipped)
	}
	for _, p := range b.Operators {
		if !strings.HasPrefix(p.Provenance, "claude-code:bootstrap:") {
			t.Fatalf("%s: bootstrap candidates must carry substrate provenance, got %q", p.Name, p.Provenance)
		}
		if len(p.Links) == 0 {
			t.Fatalf("%s: candidates must be cross-linked", p.Name)
		}
	}
}

// TestExerciseOperatorRuns: a real batch candidate genuinely RUNS (mint -> program -> verify ->
// instantiate -> fire on the test backend), and a malformed candidate fails the exercise.
func TestExerciseOperatorRuns(t *testing.T) {
	b := loadBootstrap(t)
	if !ExerciseOperator(b.Operators[0]) {
		t.Fatalf("%s should exercise cleanly", b.Operators[0].Name)
	}
	bad := ProposedOperator{Name: "x", Family: "no-such-family", Intent: "too short"}
	if ExerciseOperator(bad) {
		t.Fatal("a malformed candidate must fail the exercise")
	}
}

// TestBootstrapBatchThroughThePipeline is the campaign's first REAL feeder run: the bootstrap batch
// flows verify -> exercise -> Tier-0 -> Tier-1 against the LIVE seed catalog + its generated
// judge-set, and the cell-filling candidates SURVIVE (they are genuinely novel moves, so the funnel
// must not falsely reject them) while the live registry is untouched.
func TestBootstrapBatchThroughThePipeline(t *testing.T) {
	live := cognition.NewOperatorRegistry()
	before := len(live.Names())
	judge := BuildOperatorJudgeSet(live)
	rep := RunOperatorFeeder(loadBootstrap(t), live, judge)

	if !rep.Tier1.Pass {
		t.Fatalf("the bootstrap batch should not dilute the judge-set; regressions=%v", rep.Tier1.Regressions)
	}
	if len(rep.Admitted) < 10 {
		t.Fatalf("most cell-filling candidates should survive the offline gates, admitted=%d rejected=%v",
			len(rep.Admitted), rep.Rejected)
	}
	if len(live.Names()) != before {
		t.Fatal("the feeder must NEVER mutate the live registry (commit happens after Tier-2)")
	}
}

// TestFeederRejectsDishonestCandidates: the gates actually gate — no provenance, a seed-name
// collision, a too-thin intent, and a verbatim duplicate of a LIVE entry are each rejected with a
// named reason.
func TestFeederRejectsDishonestCandidates(t *testing.T) {
	live := cognition.NewOperatorRegistry()
	judge := BuildOperatorJudgeSet(live)
	decompose, _ := live.Get("decompose")
	batch := Batch{Operators: []ProposedOperator{
		{Kind: "operator", Name: "orphan", Family: "primitive", Intent: "a real definition of a move", Links: []string{"x"}, Provenance: ""},
		{Kind: "operator", Name: "decompose", Family: "transformative", Intent: "redefine a frozen seed operator", Links: []string{"x"}, Provenance: "claude-code:test"},
		{Kind: "operator", Name: "thin", Family: "primitive", Intent: "too short", Links: []string{"x"}, Provenance: "claude-code:test"},
		{Kind: "operator", Name: "decompose-clone", Family: decompose.Family, Intent: decompose.Intent, Links: []string{"x"}, Provenance: "claude-code:test"},
	}}
	rep := RunOperatorFeeder(batch, live, judge)
	if len(rep.Admitted) != 0 {
		t.Fatalf("all four dishonest candidates must be rejected, admitted=%v", rep.Admitted)
	}
	for name, wantSub := range map[string]string{
		"orphan":          "not-traceable",
		"decompose":       "verify:",
		"thin":            "verify:",
		"decompose-clone": "near-dup-of:decompose",
	} {
		if r, ok := rep.Rejected[name]; !ok || !strings.Contains(r, wantSub) {
			t.Errorf("%s: rejection should name %q, got %q", name, wantSub, r)
		}
	}
}
