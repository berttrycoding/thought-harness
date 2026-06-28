package ruler

// skillcurve_test.go — A3 STEP 3 (offline): the curve→ruler bridge. Prove CharacterizeCurve splits the curve
// at the first recall, computes the cohort cost bend, and gates it on the SAME within-cohort cost-σ /
// feasibility machinery the campaign already trusts — DEGENERATE offline (no usage), failable on claude.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/campaign"
)

// curve builds a synthetic curve: `pre` synthesis exposures then `post` recall exposures, each carrying the
// given per-exposure completion cost. mintAt/recallAt mark the inflection. Used to drive CharacterizeCurve
// with KNOWN cohorts so the reduction is checked against hand-computed means.
func curve(preCosts, postCosts []int, mintAt, recallAt int) []campaign.CurvePoint {
	pts := make([]campaign.CurvePoint, 0, len(preCosts)+len(postCosts))
	i := 0
	for _, c := range preCosts {
		pts = append(pts, campaign.CurvePoint{Exposure: i, Completion: c, Minted: i == mintAt, Solved: true, Grounded: true})
		i++
	}
	for _, c := range postCosts {
		pts = append(pts, campaign.CurvePoint{Exposure: i, Completion: c, Recalled: true, Solved: true, Grounded: true})
		_ = recallAt
		i++
	}
	return pts
}

// TestCharacterizeCurveBendsDown: a curve whose post-recall cohort costs LESS bends down (CostBend > 0), and
// the split lands at the first recall exposure. Magnitudes large + within-cohort variance small → the bend
// clears the floor (COST-RELIABLE).
func TestCharacterizeCurveBendsDown(t *testing.T) {
	// pre cohort costs ~1000 (synthesis), post cohort ~400 (recall) — a clear, low-variance bend.
	pre := []int{1010, 990, 1000}     // first recall is at exposure 3
	post := []int{405, 395, 400, 402} // recall cohort, much cheaper
	pts := curve(pre, post, 2, 3)

	cc := CharacterizeCurve(pts, Options{})
	t.Logf("exposures=%d firstMint=%d firstRecall=%d preMean=%.1f postMean=%.1f bend=%.1f costVerdict=%s floorCleared=%v band=%.1f",
		cc.Exposures, cc.FirstMintExposure, cc.FirstRecallExposure, cc.PreMeanCompletion, cc.PostMeanCompletion,
		cc.CostBend, cc.Cost.CostVerdict, cc.CostFloorCleared, cc.Cost.CostBandHalfWidth)

	if cc.FirstRecallExposure != 3 {
		t.Errorf("FirstRecallExposure = %d, want 3", cc.FirstRecallExposure)
	}
	if cc.FirstMintExposure != 2 {
		t.Errorf("FirstMintExposure = %d, want 2", cc.FirstMintExposure)
	}
	if cc.CostBend <= 0 {
		t.Errorf("CostBend = %.1f, want > 0 (post cheaper than pre)", cc.CostBend)
	}
	// pre mean = 1000, post mean = 400.5 → bend ≈ 599.5
	if cc.PreMeanCompletion < 990 || cc.PostMeanCompletion > 410 {
		t.Errorf("cohort means off: pre=%.1f post=%.1f", cc.PreMeanCompletion, cc.PostMeanCompletion)
	}
	if !cc.CostFloorCleared {
		t.Errorf("a 600-token bend with ~10-token within-cohort σ should CLEAR the floor; verdict=%s band=%.1f",
			cc.Cost.CostVerdict, cc.Cost.CostBandHalfWidth)
	}
}

// TestCharacterizeCurveNoRecallIsFlat: a curve with NO recall (the mint never fired) has FirstRecall=-1, the
// whole stream as the PRE cohort, an empty POST cohort, and a zero bend — the honest flat verdict.
func TestCharacterizeCurveNoRecallIsFlat(t *testing.T) {
	pts := []campaign.CurvePoint{
		{Exposure: 0, Completion: 1000},
		{Exposure: 1, Completion: 1000},
		{Exposure: 2, Completion: 1000},
	}
	cc := CharacterizeCurve(pts, Options{})
	if cc.FirstRecallExposure != -1 {
		t.Errorf("FirstRecallExposure = %d, want -1 (no recall)", cc.FirstRecallExposure)
	}
	if cc.CostBend != 0 {
		t.Errorf("CostBend = %.1f, want 0 (flat — no recall cohort)", cc.CostBend)
	}
	if cc.CostFloorCleared {
		t.Error("a flat curve must NOT clear the cost floor")
	}
}

// TestCharacterizeCurveOfflineDegenerate: a curve whose completions are all 0 (the offline test double) yields
// CostVerdict DEGENERATE and does NOT clear the floor — the apparatus reports the offline cost as
// uncharacterizable, never a fabricated win.
func TestCharacterizeCurveOfflineDegenerate(t *testing.T) {
	pts := []campaign.CurvePoint{
		{Exposure: 0, Completion: 0, Solved: true},
		{Exposure: 1, Completion: 0, Solved: true},
		{Exposure: 2, Completion: 0, Minted: true, Solved: true},
		{Exposure: 3, Completion: 0, Recalled: true, Solved: true},
		{Exposure: 4, Completion: 0, Recalled: true, Solved: true},
	}
	cc := CharacterizeCurve(pts, Options{})
	t.Logf("offline curve: firstRecall=%d bend=%.1f costVerdict=%s floorCleared=%v",
		cc.FirstRecallExposure, cc.CostBend, cc.Cost.CostVerdict, cc.CostFloorCleared)
	if cc.FirstRecallExposure != 3 {
		t.Errorf("FirstRecallExposure = %d, want 3 (recall fired even offline)", cc.FirstRecallExposure)
	}
	if cc.Cost.CostVerdict != CostDegenerate {
		t.Errorf("offline cost verdict = %s, want %s (no usage to characterize)", cc.Cost.CostVerdict, CostDegenerate)
	}
	if cc.CostFloorCleared {
		t.Error("the offline (zero-cost) curve must NOT clear the floor — no cost was characterized")
	}
}

// TestCharacterizeCurveNoisyAtSmallN: a real bend but with LARGE within-cohort variance and few exposures
// (the W5-2c n=5-below-floor finding) is reported COST-NOISY and does NOT clear the floor — the apparatus is
// honest that small N cannot clear the floor even when the sign is right.
func TestCharacterizeCurveNoisyAtSmallN(t *testing.T) {
	// pre ~ {200, 1800} (mean 1000, huge σ), post ~ {100, 900} (mean 500, huge σ): bend 500 but the within-
	// cohort σ (~800) swamps it → COST-NOISY.
	pre := []int{200, 1800}
	post := []int{100, 900}
	pts := curve(pre, post, 0, 2)
	cc := CharacterizeCurve(pts, Options{})
	t.Logf("noisy curve: bend=%.1f within-σ=%.1f costMDE=%.1f verdict=%s floorCleared=%v",
		cc.CostBend, cc.Cost.CostSigmaWithin, cc.Cost.CostMDE, cc.Cost.CostVerdict, cc.CostFloorCleared)
	if cc.CostBend <= 0 {
		t.Errorf("CostBend = %.1f, want > 0 (sign is a real down-bend)", cc.CostBend)
	}
	if cc.CostFloorCleared {
		t.Error("a high-variance small-N bend must NOT clear the floor (the W5-2c finding); the apparatus must be honest")
	}
}

// TestCharacterizeCurveFacultyDirection: a curve whose post cohort fires the faculty MORE reports "+"
// (directional capability lift, caveated), and a curve with no faculty scoring reports "flat".
func TestCharacterizeCurveFacultyDirection(t *testing.T) {
	// pre fires 0/2, post fires 2/2 → "+".
	pts := []campaign.CurvePoint{
		{Exposure: 0, Completion: 1000, Signature: "act", Fired: false},
		{Exposure: 1, Completion: 1000, Signature: "act", Fired: false, Minted: true},
		{Exposure: 2, Completion: 400, Signature: "act", Fired: true, Recalled: true},
		{Exposure: 3, Completion: 400, Signature: "act", Fired: true, Recalled: true},
	}
	cc := CharacterizeCurve(pts, Options{})
	if cc.FacultyDirection != "+" {
		t.Errorf("FacultyDirection = %q, want \"+\" (post fires more)", cc.FacultyDirection)
	}

	// no faculty scored anywhere → "flat".
	noFac := []campaign.CurvePoint{
		{Exposure: 0, Completion: 1000},
		{Exposure: 1, Completion: 400, Recalled: true},
	}
	cc2 := CharacterizeCurve(noFac, Options{})
	if cc2.FacultyDirection != "flat" {
		t.Errorf("FacultyDirection = %q, want \"flat\" (no faculty scored)", cc2.FacultyDirection)
	}
}
