package decisionoracle

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// TestDriveWiringOffline drives the REAL Deliberator/Verifier sub-agents over every
// fixture on the OFFLINE test double and asserts the Drive PIPELINE works end-to-end:
// each fixture staffs a worker skill, expands+verifies its program, runs its sub-agents,
// extracts a verdict, and scores it. It asserts the WIRING + SCORING, not a specific
// pass-rate: on the deterministic double OperatorApply returns closed-form fragments that
// do not name a confident pick / accept-refuse, so a worker may trivially fail to extract
// a verdict (a hard fail). The real signal — do the workers pass their oracle? — is the
// claude baseline (a follow-up live run); this test proves the measuring apparatus runs.
func TestDriveWiringOffline(t *testing.T) {
	fixtures := loadBank(t)
	be := backends.NewTest()

	results := DriveAll(fixtures, be, 1729)
	if len(results) != len(fixtures) {
		t.Fatalf("DriveAll returned %d results for %d fixtures", len(results), len(fixtures))
	}

	for i, r := range results {
		fx := fixtures[i]
		if r.ID != fx.ID {
			t.Errorf("[%d] result ID %q != fixture ID %q (order not preserved)", i, r.ID, fx.ID)
		}
		// Every fixture must STAFF a worker: a known worker kind expands+verifies its
		// skill and produces output. A non-staffed fixture is a wiring/bank error, not a
		// trivial double-fail.
		if !r.Staffed {
			t.Errorf("[%s] worker %s did not staff (skill=%q shape=%q): %s",
				fx.ID, fx.Worker, r.Skill, r.Shape, r.Score.Reason)
		}
		if r.Skill == "" {
			t.Errorf("[%s] no worker skill resolved for worker %q", fx.ID, fx.Worker)
		}
		if r.Shape == "" {
			t.Errorf("[%s] empty program shape (skill %q did not expand)", fx.ID, r.Skill)
		}
		// The score must be a real number in [0,1] (the oracle ran).
		if r.Score.Score < 0 || r.Score.Score > 1 {
			t.Errorf("[%s] score %.3f out of [0,1]", fx.ID, r.Score.Score)
		}
		t.Logf("[%s] worker=%s skill=%s shape=%s decided=%v score=%.3f pass=%v | %s",
			fx.ID, fx.Worker, r.Skill, r.Shape, r.Score.Decided, r.Score.Score, r.Score.Pass, r.Score.Reason)
	}

	// The rollup produces a per-worker pass-rate (the A2 baseline shape). Assert it sums
	// correctly and is deterministic, not a specific rate.
	stats := Rollup(results)
	if len(stats) == 0 {
		t.Fatalf("Rollup produced no worker stats over %d results", len(results))
	}
	totalFixtures := 0
	for _, s := range stats {
		totalFixtures += s.Total
		if s.Passed > s.Total {
			t.Errorf("worker %s: passed %d > total %d", s.Worker, s.Passed, s.Total)
		}
		if pr := s.PassRate(); pr < 0 || pr > 1 {
			t.Errorf("worker %s: pass-rate %.3f out of [0,1]", s.Worker, pr)
		}
		t.Logf("WORKER %-12s %d/%d pass (rate=%.2f, mean-score=%.3f, decided=%d)",
			s.Worker, s.Passed, s.Total, s.PassRate(), s.MeanScore(), s.Decided)
	}
	if totalFixtures != len(fixtures) {
		t.Errorf("rollup totals %d fixtures != %d loaded", totalFixtures, len(fixtures))
	}

	// Both worker kinds must be exercised — a baseline that only ran one is not an A2
	// baseline (the bank carries both deliberator and verifier fixtures).
	saw := map[Worker]bool{}
	for _, s := range stats {
		saw[s.Worker] = true
	}
	if !saw[WorkerDeliberator] {
		t.Errorf("no Deliberator fixtures driven — A2 baseline must exercise the trade-off worker")
	}
	if !saw[WorkerVerifier] {
		t.Errorf("no Verifier fixtures driven — A2 baseline must exercise the ship worker")
	}
}

// TestDriveDeterministicOffline proves the offline Drive is byte-stable: two runs on the
// test double with the same seed base produce identical verdicts + scores per fixture, so
// the test-double baseline is reproducible (the determinism rule — seeded RNG, no clock).
func TestDriveDeterministicOffline(t *testing.T) {
	fixtures := loadBank(t)
	be := backends.NewTest()

	r1 := DriveAll(fixtures, be, 1729)
	r2 := DriveAll(fixtures, be, 1729)

	for i := range r1 {
		if r1[i].Output != r2[i].Output {
			t.Errorf("[%s] non-deterministic worker output:\n  run1=%q\n  run2=%q", r1[i].ID, r1[i].Output, r2[i].Output)
		}
		if r1[i].Score.Score != r2[i].Score.Score || r1[i].Score.Pass != r2[i].Score.Pass || r1[i].Score.Decided != r2[i].Score.Decided {
			t.Errorf("[%s] non-deterministic score: run1(score=%.4f pass=%v decided=%v) != run2(score=%.4f pass=%v decided=%v)",
				r1[i].ID, r1[i].Score.Score, r1[i].Score.Pass, r1[i].Score.Decided,
				r2[i].Score.Score, r2[i].Score.Pass, r2[i].Score.Decided)
		}
	}
}

// TestDriveOfflineExercisesContractPath proves the OFFLINE Drive exercises the verdict-CONTRACT
// path, not merely the prose fallback (A2 fix #5): on the test double, EVERY fixture's worker
// states a well-formed `VERDICT:` line (VerdictLinePresent), the verdict is sourced from the
// contract line (VerdictSource=="line"), and the present-rate is 1.0 — so the offline suite tests
// the new fragile surface (parseVerdictLine), not the legacy parser. This is the regression that
// catches a future change accidentally disabling the contract path offline (which would let the
// suite silently re-validate only the fallback).
func TestDriveOfflineExercisesContractPath(t *testing.T) {
	fixtures := loadBank(t)
	be := backends.NewTest()
	results := DriveAll(fixtures, be, 1729)

	for _, r := range results {
		if !r.VerdictLinePresent {
			t.Errorf("[%s] the offline double must state a VERDICT line (the contract path), got none\n  output=%q", r.ID, r.Output)
		}
		if r.VerdictSource != "line" {
			t.Errorf("[%s] verdict must be sourced from the contract LINE offline, got source=%q (the fallback was used)", r.ID, r.VerdictSource)
		}
	}
	for _, s := range Rollup(results) {
		if s.VerdictLineRate() != 1.0 {
			t.Errorf("worker %s: offline present-rate %.3f != 1.0 — the double must obey the contract on every fixture", s.Worker, s.VerdictLineRate())
		}
	}
}

// TestDriveEmitVerdictFedPriorReasoning proves the verdict call receives the worker's accumulated
// reasoning (A2 fix #2): the combined Output surface contains BOTH the per-step reasoning fragments
// (the program's OperatorApply outputs, recognisable by the "[role]" tag the double emits) AND the
// stated VERDICT line. If the verdict step re-decided from the bare goal (ignoring priorReasoning),
// the reasoning fragments would be absent from the surface the oracle scores.
func TestDriveEmitVerdictFedPriorReasoning(t *testing.T) {
	be := backends.NewTest()
	fx := Fixture{ID: "probe", Worker: WorkerDeliberator, Goal: "mutex or channel? pick one",
		CriteriaWeights: map[string]float64{"simplicity": 1},
		Options: []Option{
			{ID: "mutex", Scores: map[string]float64{"simplicity": 0.8}},
			{ID: "channel", Scores: map[string]float64{"simplicity": 0.4}},
		}}
	r := Drive(fx, be, cpyrand.New(7))
	// The program's per-step outputs (the double tags each with "[role]") must be in the surface
	// alongside the verdict line — proving the verdict was appended to the reasoning, not a
	// stand-alone re-decision from the goal.
	if !strings.Contains(r.Output, "[") {
		t.Errorf("the combined surface must carry the worker's per-step reasoning fragments, got: %q", r.Output)
	}
	if !strings.Contains(r.Output, "VERDICT:") {
		t.Errorf("the combined surface must carry the stated VERDICT line, got: %q", r.Output)
	}
}

// TestDriveStaffsBothWorkerSkills is a focused wiring assertion: the worker-skill map
// resolves to a real, expandable seed skill for BOTH worker kinds, so neither worker is a
// dead stub. (If a seed skill were renamed/removed this fails loud here rather than reading
// as a silent double-fail in the rollup.)
func TestDriveStaffsBothWorkerSkills(t *testing.T) {
	be := backends.NewTest()
	rng := cpyrand.New(7)
	for _, fx := range []Fixture{
		{ID: "probe-delib", Worker: WorkerDeliberator, Goal: "mutex or channel? pick one",
			CriteriaWeights: map[string]float64{"simplicity": 1},
			Options:         []Option{{ID: "mutex", Scores: map[string]float64{"simplicity": 0.8}}, {ID: "channel", Scores: map[string]float64{"simplicity": 0.4}}}},
		{ID: "probe-verif", Worker: WorkerVerifier, Goal: "is it safe to ship? verify", Truth: ClaimTrue},
	} {
		r := Drive(fx, be, rng)
		if r.Skill == "" {
			t.Errorf("[%s] worker %s staffed no skill", fx.ID, fx.Worker)
		}
		if !r.Staffed {
			t.Errorf("[%s] worker %s did not run any sub-agent (shape=%q): %s", fx.ID, fx.Worker, r.Shape, r.Score.Reason)
		}
		if r.Output == "" {
			t.Errorf("[%s] worker %s produced no output surface", fx.ID, fx.Worker)
		}
	}
}
