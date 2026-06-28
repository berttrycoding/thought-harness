package synthfidelity

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// TestBankIntegrity proves the gold bank is well-formed: every fixture has an ID, a
// goal, a non-empty Expect, a GoodProgram that parses+verifies, a BadProgram that
// parses+verifies (a bad program must be PLAUSIBLE — a valid program with the wrong
// structure, not a malformed one), and a sane threshold. IDs are unique.
func TestBankIntegrity(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	fixtures := loadBank(t)

	seen := map[string]bool{}
	for _, fx := range fixtures {
		if fx.ID == "" {
			t.Errorf("fixture with empty ID")
			continue
		}
		if seen[fx.ID] {
			t.Errorf("duplicate fixture ID %q", fx.ID)
		}
		seen[fx.ID] = true
		if fx.Goal == "" {
			t.Errorf("[%s] empty goal", fx.ID)
		}
		if isEmptyExpect(fx.Expect) {
			t.Errorf("[%s] empty Expect (a fixture must constrain SOMETHING)", fx.ID)
		}
		if len(fx.GoodProgram) == 0 {
			t.Errorf("[%s] missing good_program", fx.ID)
		}
		if len(fx.BadProgram) == 0 {
			t.Errorf("[%s] missing bad_program", fx.ID)
		}
		// Both programs must be valid, parseable trees (the bad one is plausible-but-
		// wrong, NOT malformed): featuresOf parses+verifies.
		if _, ok, reason := featuresOf(fx.GoodProgram, cat); !ok {
			t.Errorf("[%s] good_program does not parse/verify: %s", fx.ID, reason)
		}
		if _, ok, reason := featuresOf(fx.BadProgram, cat); !ok {
			t.Errorf("[%s] bad_program must be a PLAUSIBLE (valid) program, not malformed: %s", fx.ID, reason)
		}
	}
	if len(fixtures) < 6 {
		t.Errorf("bank should have >= 6 fixtures (target-spec: 6-10), got %d", len(fixtures))
	}
}

// isEmptyExpect reports whether an Expect constrains nothing at all.
func isEmptyExpect(e Expect) bool {
	return len(e.MustOperators) == 0 && len(e.ForbidOperators) == 0 && len(e.MustFamilies) == 0 &&
		len(e.MustMoves) == 0 && len(e.RequireShapes) == 0 && len(e.ForbidShapes) == 0 &&
		e.MinSteps == 0 && e.MaxSteps == 0 && !e.ActOnReality && len(e.MustToolScope) == 0
}

// TestDriveRealSynthesiserOffline drives the REAL synthesiser (cognition.Synthesize
// via the offline TestBackend) over every fixture and asserts (a) it is fully
// deterministic — two runs produce identical results — and (b) the measured outcome
// matches the bank's authored SynthesiserCovers expectation (NO drift). Drift here
// would mean the synthesiser's capability moved and the bank must be revisited; it
// is reported, not silently tolerated. This is the offline-vettable A5 measurement.
func TestDriveRealSynthesiserOffline(t *testing.T) {
	w := DefaultWeights()
	fixtures := loadBank(t)

	r1 := DriveAll(fixtures, w)
	r2 := DriveAll(fixtures, w)

	if len(r1) != len(fixtures) || len(r2) != len(fixtures) {
		t.Fatalf("DriveAll returned %d/%d results for %d fixtures", len(r1), len(r2), len(fixtures))
	}

	covered, gaps := 0, 0
	for i, res := range r1 {
		fx := fixtures[i]
		// Determinism: byte-stable score + pass + shape across two offline runs.
		if res.Verdict.Score != r2[i].Verdict.Score || res.Verdict.Pass != r2[i].Verdict.Pass || res.Shape != r2[i].Shape {
			t.Errorf("[%s] non-deterministic Drive: run1(score=%.4f pass=%v shape=%q) != run2(score=%.4f pass=%v shape=%q)",
				fx.ID, res.Verdict.Score, res.Verdict.Pass, res.Shape, r2[i].Verdict.Score, r2[i].Verdict.Pass, r2[i].Shape)
		}
		// No drift: the measured fidelity pass must match the authored covers flag.
		if res.Drift {
			t.Errorf("[%s] DRIFT: authored synthesiser_covers=%v but real synthesis pass=%v (score=%.3f, source=%s, shape=%q)\n  %s",
				fx.ID, fx.SynthesiserCovers, res.Verdict.Pass, res.Verdict.Score, res.Source, res.Shape, res.Verdict.Reason)
		}
		if fx.SynthesiserCovers {
			covered++
		} else {
			gaps++
		}
		t.Logf("[%s] worker=%s covers=%v synth=%v source=%s shape=%q score=%.3f pass=%v",
			fx.Worker, fx.ID, fx.SynthesiserCovers, res.Synthesised, res.Source, res.Shape, res.Verdict.Score, res.Verdict.Pass)
	}
	t.Logf("A5 synthesiser fidelity: %d covered (real synthesis is faithful) + %d KNOWN GAPS (rankable capability gaps) over %d fixtures",
		covered, gaps, len(fixtures))

	// The bank must contain BOTH covered cases and gaps — a bank of all-covered would
	// not probe the synthesiser's frontier; all-gaps would not prove the oracle
	// credits a faithful synthesis. (The target-spec §3 intent: a miss is a precise
	// gap, so gaps must be present and labeled.)
	if covered == 0 {
		t.Errorf("bank has NO covered fixtures — it cannot show the synthesiser producing a faithful program")
	}
	if gaps == 0 {
		t.Errorf("bank has NO gap fixtures — A5's sharpest signal (a rankable capability gap) is unprobed")
	}
}
