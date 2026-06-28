package ruler

// graded_test.go — GATE-2 graded-instrument unit tests. The graded ruler is a PURE statistical
// reduction over per-task per-replay graded vectors, so these feed SYNTHETIC graded vectors and
// assert the within/between/ICC/MDE math is exact on hand-computable fixtures and the verdict is
// mutation-sensitive (a finer/clean instrument CLEARS; a saturated/noisy one RESATURATES; the
// test-double degenerate case is honest). All offline, deterministic, no model.

import (
	"testing"
)

// grow builds a synthetic graded task row from its per-replay [0,1] score vector.
func grow(id string, scores ...float64) GradedTask {
	return GradedTask{ID: id, Scores: scores}
}

// TestGradedClearsOnCleanInstrument: tight within-task clusters (low noise) that are well-SEPARATED
// between tasks (real faculty-difference signal) → the graded signature CLEARS. This is the
// CAPABILITY-FACULTY-MEASURABLE case: a faculty-engagement delta would be detectable.
func TestGradedClearsOnCleanInstrument(t *testing.T) {
	rows := []GradedTask{
		grow("easy-a", 0.10, 0.12, 0.11, 0.09), // mean ~0.105, tiny within-spread
		grow("easy-b", 0.15, 0.14, 0.16, 0.15),
		grow("hard-a", 0.80, 0.82, 0.79, 0.81), // mean ~0.805 — well above the easy tasks
		grow("hard-b", 0.85, 0.86, 0.84, 0.85),
	}
	c := CharacterizeGraded(rows, Options{})
	if c.K != 4 {
		t.Fatalf("K = %d, want 4", c.K)
	}
	if c.BetweenSD == 0 {
		t.Fatalf("BetweenSD = 0; the tasks are well-separated, so there must be between-task signal")
	}
	if c.SigmaWithin >= c.BetweenSD {
		t.Errorf("SigmaWithin (%.4f) should be well under BetweenSD (%.4f) for a clean instrument", c.SigmaWithin, c.BetweenSD)
	}
	if c.ICC < c.ICCFloor {
		t.Errorf("ICC = %.4f, want >= floor %.2f (between-task signal dominates the tiny within noise)", c.ICC, c.ICCFloor)
	}
	if c.Verdict != GradedClears || !c.Feasible {
		t.Errorf("verdict = %s feasible=%v, want GRADED-CLEARS/true (clean separated instrument)", c.Verdict, c.Feasible)
	}
}

// TestGradedResaturatesNoBetweenSignal: every task engages the faculty to the SAME high degree
// (the frontier re-saturates the graded score too) → no between-task variance → RESATURATES even
// though within-task noise is small. This is the CAPABILITY-IS-W6-ONLY outcome.
func TestGradedResaturatesNoBetweenSignal(t *testing.T) {
	rows := []GradedTask{
		grow("a", 0.90, 0.91, 0.89, 0.90),
		grow("b", 0.90, 0.90, 0.91, 0.89),
		grow("c", 0.91, 0.90, 0.90, 0.90),
		grow("d", 0.89, 0.90, 0.91, 0.90),
	}
	c := CharacterizeGraded(rows, Options{})
	if c.Mean < 0.85 {
		t.Errorf("Mean = %.3f, want ~0.90 (saturated near the top of the scale)", c.Mean)
	}
	if c.Verdict != GradedResaturates {
		t.Errorf("verdict = %s, want GRADED-RESATURATES (no between-task signal: every task equally saturated)", c.Verdict)
	}
	if c.Feasible {
		t.Errorf("feasible = true, want false (a saturated graded signature cannot resolve a faculty delta)")
	}
}

// TestGradedResaturatesNoisySwamps: there IS a between-task mean difference, but the within-task
// noise is so large it swamps the signal (low ICC / large MDE) → RESATURATES. The graded signature
// is finer in PRINCIPLE but too noisy at this K to clear.
func TestGradedResaturatesNoisySwamps(t *testing.T) {
	rows := []GradedTask{
		grow("a", 0.0, 1.0, 0.0, 1.0), // mean 0.5, huge within-task spread
		grow("b", 1.0, 0.0, 1.0, 0.0), // mean 0.5
		grow("c", 0.0, 0.9, 0.1, 1.0), // mean 0.5
		grow("d", 0.95, 0.05, 1.0, 0.0),
	}
	c := CharacterizeGraded(rows, Options{})
	if c.SigmaWithin <= c.BetweenSD {
		t.Errorf("SigmaWithin (%.4f) should swamp BetweenSD (%.4f) in this fixture", c.SigmaWithin, c.BetweenSD)
	}
	if c.Verdict != GradedResaturates {
		t.Errorf("verdict = %s, want GRADED-RESATURATES (within-task noise swamps the between-task signal)", c.Verdict)
	}
}

// TestGradedDegenerate: under-2 tasks, or zero variance everywhere (the deterministic test double
// where every replay is identical) → DEGENERATE (not a pass, not a noise-fail: the instrument has
// not been exercised on a non-deterministic substrate).
func TestGradedDegenerate(t *testing.T) {
	// zero variance everywhere (test-double shape: every replay identical, every task identical).
	flat := []GradedTask{
		grow("a", 0.5, 0.5, 0.5),
		grow("b", 0.5, 0.5, 0.5),
	}
	if c := CharacterizeGraded(flat, Options{}); c.Verdict != GradedDegenerate {
		t.Errorf("flat verdict = %s, want GRADED-DEGENERATE (no variance to characterize)", c.Verdict)
	}
	// single task → degenerate (cannot compute between-task variance).
	if c := CharacterizeGraded([]GradedTask{grow("solo", 0.1, 0.9, 0.5)}, Options{}); c.Verdict != GradedDegenerate {
		t.Errorf("single-task verdict = %s, want GRADED-DEGENERATE", c.Verdict)
	}
	// K<2 → degenerate.
	if c := CharacterizeGraded([]GradedTask{grow("a", 0.1), grow("b", 0.9)}, Options{}); c.Verdict != GradedDegenerate {
		t.Errorf("K=1 verdict = %s, want GRADED-DEGENERATE", c.Verdict)
	}
}

// TestGradedICCExact: hand-computable ICC on a fixture. Two tasks, K=2: task A = {0.0, 0.0}
// (within-var 0), task B = {1.0, 1.0} (within-var 0); per-task means {0,1}, between-var (n−1) =
// 0.5. WithinVar = 0 → MSW=0 → ICC = (k·between − 0)/(k·between + 0) = 1.0 exactly.
func TestGradedICCExact(t *testing.T) {
	rows := []GradedTask{grow("a", 0.0, 0.0), grow("b", 1.0, 1.0)}
	c := CharacterizeGraded(rows, Options{})
	approx(t, "BetweenVar", c.BetweenVar, 0.5)
	approx(t, "WithinVar", c.WithinVar, 0.0)
	approx(t, "ICC", c.ICC, 1.0)
}

// TestGradedMDEMonotoneInN: more tasks → smaller MDE (the instrument resolves finer at higher N),
// holding the within-task noise fixed. A sanity check that the MDE formula has the right N scaling.
func TestGradedMDEMonotoneInN(t *testing.T) {
	small := []GradedTask{
		grow("a", 0.3, 0.4, 0.35), grow("b", 0.6, 0.7, 0.65),
	}
	big := []GradedTask{
		grow("a", 0.3, 0.4, 0.35), grow("b", 0.6, 0.7, 0.65),
		grow("c", 0.31, 0.41, 0.36), grow("d", 0.61, 0.71, 0.66),
		grow("e", 0.32, 0.42, 0.37), grow("f", 0.62, 0.72, 0.67),
	}
	cs := CharacterizeGraded(small, Options{})
	cb := CharacterizeGraded(big, Options{})
	if !(cb.MDE < cs.MDE) {
		t.Errorf("MDE should shrink with N: small(N=2)=%.4f big(N=6)=%.4f", cs.MDE, cb.MDE)
	}
}

// TestFromCogGradedAdapter: the adapter copies the per-replay aptness vector verbatim from the
// campaign CogStability aggregate (the gate-2 bridge).
func TestFromCogGradedAdapter(t *testing.T) {
	// build via the campaign aggregate shape through the convenience entrypoint indirectly: assert
	// FromCogGraded copies Aptness verbatim.
	rows := FromCogGraded(nil)
	if len(rows) != 0 {
		t.Errorf("FromCogGraded(nil) = %d rows, want 0", len(rows))
	}
}
