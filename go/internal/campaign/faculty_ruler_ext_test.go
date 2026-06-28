package campaign_test

// faculty_ruler_ext_test.go — the FREE RULER FEASIBILITY GATE over the v2 faculty suite (an EXTERNAL test
// package so it can import BOTH campaign and ruler without the ruler→campaign import cycle that blocks an
// internal test). This is the gate the directive asks for: ICC>=0.5 AND MDE<=0.15 on the test double,
// BEFORE any claude run. On the deterministic test double the within-task replay variance is 0, so the
// instrument's resolution is bounded ONLY by whether the suite produces a real BETWEEN-task spread — which
// is exactly what the v2 suite's outcome-tied faculties give. A degenerate suite (all faculties saturate
// identically — the legacy defect) would return DEGENERATE/NOISY here.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/campaign"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/ruler"
)

func facultyRulerEngine(stateDir string) (*engine.Engine, error) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	return engine.NewEngine(&cfg, backends.NewTest())
}

// TestFacultySuiteRulerFeasible asserts the v2 suite passes the free feasibility gate on BOTH the fire-only
// binary axis AND the OUTCOME-TIED (fired-and-correct) axis — the validity-fixed metric a faculty-lever A/B
// gates on. On the test double both clear ICC>=0.5 AND MDE<=0.15 (the gate constants).
func TestFacultySuiteRulerFeasible(t *testing.T) {
	const k = 3
	b := campaign.EngineBencher{MaxTicks: 40, NewEngine: facultyRulerEngine}
	rows := b.CognitionProbeReplays(campaign.FacultySuite(), "", k)

	// fire-only binary axis
	bin := ruler.CharacterizeCog(rows, ruler.Options{})
	if bin.Verdict != ruler.VerdictFeasible {
		t.Errorf("fire-only feasibility: verdict=%s (want FEASIBLE) ICC=%.3f MDE=%.3f between=%.4f",
			bin.Verdict, bin.ICC, bin.MDE, bin.BetweenVar)
	}
	if bin.ICC < ruler.DefaultICCFloor {
		t.Errorf("fire-only ICC=%.3f below floor %.2f", bin.ICC, ruler.DefaultICCFloor)
	}
	if bin.MDE > ruler.DefaultClaimableLift {
		t.Errorf("fire-only MDE=%.3f exceeds claimable lift %.2f", bin.MDE, ruler.DefaultClaimableLift)
	}

	// OUTCOME-TIED (fired-and-correct) axis — the validity-fixed keep-metric.
	fc := make([]campaign.CogStability, len(rows))
	for i, r := range rows {
		r.Fired = r.FiredAndCorrect // gate the binary feasibility on the GATED metric
		fc[i] = r
	}
	oc := ruler.CharacterizeCog(fc, ruler.Options{})
	if oc.Verdict != ruler.VerdictFeasible {
		t.Errorf("OUTCOME-TIED feasibility: verdict=%s (want FEASIBLE) ICC=%.3f MDE=%.3f between=%.4f",
			oc.Verdict, oc.ICC, oc.MDE, oc.BetweenVar)
	}
	if oc.ICC < ruler.DefaultICCFloor || oc.MDE > ruler.DefaultClaimableLift {
		t.Errorf("OUTCOME-TIED gate not cleared: ICC=%.3f (floor %.2f) MDE=%.3f (claim %.2f)",
			oc.ICC, ruler.DefaultICCFloor, oc.MDE, ruler.DefaultClaimableLift)
	}

	t.Logf("v2 faculty suite (N=%d, K=%d): fire-only %s ICC=%.3f MDE=%.3f | outcome-tied %s ICC=%.3f MDE=%.3f",
		len(rows), k, bin.Verdict, bin.ICC, bin.MDE, oc.Verdict, oc.ICC, oc.MDE)
}

// TestFacultySuiteHasOutcomeSpread asserts the OUTCOME axis is genuinely SPREAD (not saturated): some tasks
// solve, some don't — the headroom a lift can move. A suite where every task solves (or none does) has no
// outcome signal regardless of how many faculties fire (the legacy fire-rate-saturation defect, applied to
// the outcome axis).
func TestFacultySuiteHasOutcomeSpread(t *testing.T) {
	b := campaign.EngineBencher{MaxTicks: 40, NewEngine: facultyRulerEngine}
	rows := b.CognitionProbeReplays(campaign.FacultySuite(), "", 3)
	solved, total := 0, 0
	for _, r := range rows {
		if r.OutcomeTied {
			total++
			if r.Correct == r.Replays {
				solved++
			}
		}
	}
	if total == 0 {
		t.Fatal("no outcome-tied tasks")
	}
	if solved == 0 || solved == total {
		t.Errorf("outcome axis is saturated (%d/%d solved) — no headroom for a lift; the suite must spread",
			solved, total)
	}
	t.Logf("outcome spread: %d/%d tasks fully solved on the test double (the rest = real headroom)", solved, total)
}
