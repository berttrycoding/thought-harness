package campaign

// bencher_test.go — the capability probe on the deterministic test double (no tokens): it runs the
// suite, returns a paired per-item result, and reads zero tokens offline.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// testEngine builds a fresh test-double engine (optionally seeded from a state dir).
func testEngineFactory(stateDir string) (*engine.Engine, error) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	if stateDir != "" {
		st, err := persist.NewJSONLStore(stateDir)
		if err != nil {
			return nil, err
		}
		cfg.Store = st
	}
	return engine.NewEngine(&cfg, backends.NewTest())
}

func TestEngineBencherRunsAndPairs(t *testing.T) {
	b := EngineBencher{
		MaxTicks:  20,
		NewEngine: testEngineFactory,
		Tasks: []HeldOutTask{
			{Goal: "what is 12 times 7?", Expect: "84"},
			{Goal: "is this refactor safe to ship?"}, // grounded-success oracle
			{Goal: "what is 9 times 9?", Expect: "81"},
		},
	}
	arm, err := b.Bench("") // baseline arm
	if err != nil {
		t.Fatalf("Bench: %v", err)
	}
	if arm.Total() != 3 {
		t.Fatalf("per-item count = %d, want 3 (paired with the suite)", arm.Total())
	}
	if arm.Tokens != 0 {
		t.Errorf("the offline double must report 0 tokens, got %d", arm.Tokens)
	}
}

// TestProbeReplaysAggregation drives the ANSWER-ORACLE replays path (A1 instrument gap) on the
// deterministic test double: each task runs K times and the aggregate carries per-task solved-rate,
// grounded-rate, mean completion-tokens, and the replay band. On the test double replays are deterministic
// (zero noise), so a known-passing task must be solved K/K (rate 1.0, stable) and a known-failing task
// 0/K (rate 0.0, stable) — the structure A1 reads. Mutation-sensitive: SolvedRate = Solved/Replays.
func TestProbeReplaysAggregation(t *testing.T) {
	const k = 3
	b := EngineBencher{
		MaxTicks:  20,
		NewEngine: testEngineFactory,
		Tasks: []HeldOutTask{
			{Goal: "what is 12 times 7?", Expect: "84"},        // a solvable oracle task
			{Goal: "xyzzy nonsense", Expect: "IMPOSSIBLE-789"}, // an oracle no run can produce → 0/K
		},
	}
	rows := b.ProbeReplays("", k)
	if len(rows) != len(b.Tasks) {
		t.Fatalf("rows = %d, want %d (one per task, order preserved)", len(rows), len(b.Tasks))
	}
	// every row records K replays.
	for i, r := range rows {
		if r.Replays != k {
			t.Errorf("row %d Replays = %d, want %d", i, r.Replays, k)
		}
		if r.Goal != b.Tasks[i].Goal || r.Expect != b.Tasks[i].Expect {
			t.Errorf("row %d task mismatch: got goal=%q expect=%q", i, r.Goal, r.Expect)
		}
	}
	// task 0 is solvable → solved every replay (deterministic double): K/K, rate 1.0, stable.
	if rows[0].Solved != k {
		t.Fatalf("solvable task Solved = %d, want %d (deterministic test double passes every replay)", rows[0].Solved, k)
	}
	if got := rows[0].SolvedRate(); got != 1.0 {
		t.Errorf("solvable task SolvedRate = %v, want 1.0 (mutation guard: SolvedRate = Solved/Replays)", got)
	}
	if rows[0].Unstable() {
		t.Errorf("a deterministic K/K task must be stable, not flagged unstable")
	}
	// task 1's oracle is impossible → never solved: 0/K, rate 0.0, stable (0 is not a flip).
	if rows[1].Solved != 0 {
		t.Errorf("impossible-oracle task Solved = %d, want 0", rows[1].Solved)
	}
	if got := rows[1].SolvedRate(); got != 0.0 {
		t.Errorf("impossible-oracle task SolvedRate = %v, want 0.0", got)
	}
	if rows[1].Unstable() {
		t.Errorf("a 0/K task must be stable (never passed → no flip)")
	}
	// the offline double emits no real usage → completion cost is 0 (a constant offline; real variance on claude).
	for i, r := range rows {
		if r.Completion != 0 {
			t.Errorf("row %d Completion = %d, want 0 on the offline test double", i, r.Completion)
		}
		if got := r.MeanCompletion(); got != 0 {
			t.Errorf("row %d MeanCompletion = %v, want 0 offline", i, got)
		}
		// W5-1 cost axis: the per-replay completion VECTOR must be retained, one
		// sample per replay (length K), and its sum must equal the Completion sum
		// (the vector is additive over the existing sum, not a replacement). All 0
		// offline → cost-σ = 0 → honest DEGENERATE on the cost axis.
		if len(r.Completions) != k {
			t.Errorf("row %d Completions length = %d, want %d (one sample per replay)", i, len(r.Completions), k)
		}
		var sum int
		for _, c := range r.Completions {
			sum += c
		}
		if sum != r.Completion {
			t.Errorf("row %d sum(Completions)=%d must equal Completion=%d (additive over the sum)", i, sum, r.Completion)
		}
	}
}

// TestProbeStabilityMath guards the aggregation summary math directly (no engine), the answer-path mirror
// of TestCogStabilityMeanCompletion — mutation-sensitive on each rate and the unstable band.
func TestProbeStabilityMath(t *testing.T) {
	// 5 replays: solved 3, grounded 2, 200 completion tokens summed.
	s := ProbeStability{Replays: 5, Solved: 3, Grounded: 2, Completion: 200}
	if got := s.SolvedRate(); got != 0.6 {
		t.Errorf("SolvedRate = %v, want 0.6 (3/5)", got)
	}
	if got := s.GroundedRate(); got != 0.4 {
		t.Errorf("GroundedRate = %v, want 0.4 (2/5)", got)
	}
	if got := s.MeanCompletion(); got != 40 {
		t.Errorf("MeanCompletion = %v, want 40 (200/5)", got)
	}
	if !s.Unstable() {
		t.Errorf("solved 3/5 must be UNSTABLE (flipped: passed some, not all)")
	}
	// all-pass and all-fail are stable; zero-replay rows must not divide by zero.
	if (ProbeStability{Replays: 4, Solved: 4}).Unstable() {
		t.Errorf("4/4 must be stable")
	}
	if (ProbeStability{Replays: 4, Solved: 0}).Unstable() {
		t.Errorf("0/4 must be stable")
	}
	for _, z := range []float64{(ProbeStability{}).SolvedRate(), (ProbeStability{}).GroundedRate(), (ProbeStability{}).MeanCompletion()} {
		if z != 0 {
			t.Errorf("zero-replay row must yield 0, got %v (no divide-by-zero)", z)
		}
	}
}

func TestScoreSolved(t *testing.T) {
	if !scoreSolved(HeldOutTask{Expect: "84"}, "the answer is 84.", false) {
		t.Error("oracle substring should pass")
	}
	if scoreSolved(HeldOutTask{Expect: "84"}, "the answer is 91.", true) {
		t.Error("wrong answer must fail even if grounded")
	}
	if !scoreSolved(HeldOutTask{}, "anything", true) {
		t.Error("no oracle + grounded should pass")
	}
	if scoreSolved(HeldOutTask{}, "anything", false) {
		t.Error("no oracle + not grounded should fail")
	}
}
