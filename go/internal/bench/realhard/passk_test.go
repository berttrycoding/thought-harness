package realhard

import (
	"math"
	"strings"
	"testing"
)

// passk_test.go — the pass^k RELIABILITY metric (tau2-bench brittleness axis, design
// doc §7.3). Plants KNOWN (p, k) pairs and asserts the closed form p^k, the boundary
// behaviour, the Wilson-CI propagation, the iid-trust gate (overdispersion revokes
// trust), and the off-by-default (PassK=0 ⇒ byte-identical) anchor.

func approxPK(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// TestPassKClosedForm: pass^k = p^k against hand-computed values.
func TestPassKClosedForm(t *testing.T) {
	cases := []struct {
		p    float64
		k    int
		want float64
	}{
		{0.9, 1, 0.9},     // pass^1 == pass@1 == p
		{0.9, 2, 0.81},    // 0.9^2
		{0.9, 5, 0.59049}, // 0.9^5
		{0.5, 3, 0.125},   // 0.5^3
		{0.99, 10, 0.99 * 0.99 * 0.99 * 0.99 * 0.99 * 0.99 * 0.99 * 0.99 * 0.99 * 0.99},
		{0.7, 4, 0.7 * 0.7 * 0.7 * 0.7},
	}
	for _, c := range cases {
		got := PassK(c.p, c.k)
		if !approxPK(got, c.want, 1e-9) {
			t.Errorf("PassK(%g,%d) = %g, want %g", c.p, c.k, got, c.want)
		}
	}
}

// TestPassKBoundaries: the degenerate inputs (k<=0, p<=0, p>=1).
func TestPassKBoundaries(t *testing.T) {
	if PassK(0.5, 0) != 1 {
		t.Error("PassK(_,0) must be 1 (the empty conjunction is vacuously true)")
	}
	if PassK(0.5, -3) != 1 {
		t.Error("PassK(_,negative) must be 1")
	}
	if PassK(0, 5) != 0 {
		t.Error("PassK(0,k>0) must be 0 (an always-failing task is never reliable)")
	}
	if PassK(1, 5) != 1 {
		t.Error("PassK(1,k) must be 1 (a perfectly reliable task stays reliable under any k)")
	}
}

// TestPassKBrittleness is the WHOLE POINT (the tau2-bench finding): a HIGH pass@1
// (capability) can collapse under pass^k (reliability). A task that solves 90% of the
// time passes@1 strongly but is brittle: across 10 independent tries the chance ALL
// succeed is barely a third. pass^k must DECAY monotonically in k for 0<p<1.
func TestPassKBrittleness(t *testing.T) {
	p := 0.9
	prev := 1.0
	for k := 1; k <= 10; k++ {
		got := PassK(p, k)
		if got >= prev {
			t.Errorf("pass^%d=%g must be < pass^%d=%g (brittleness: reliability decays in k)", k, got, k-1, prev)
		}
		prev = got
	}
	// the collapse is real: pass@1=0.9 but pass^10 < 0.5 (worse than a coin flip that
	// the agent is reliable across 10 tries).
	if PassK(0.9, 10) >= 0.5 {
		t.Errorf("pass^10 of a 0.9-capable task must collapse below 0.5 (the brittleness the metric surfaces); got %g", PassK(0.9, 10))
	}
}

// TestPassKWiredIntoEstimate: the per-task estimate carries pass^k derived from p̂,
// with the Wilson CI propagated through the same monotone p^k map.
func TestPassKWiredIntoEstimate(t *testing.T) {
	// 9/10 solved ⇒ p̂=0.9.
	e := estimateBernTask(BernTaskInput{TaskID: "t", Solved: 9, K: 10}, 3)
	if e.PassKAt != 3 {
		t.Fatalf("PassKAt = %d, want 3", e.PassKAt)
	}
	if !approxPK(e.PassK, 0.9*0.9*0.9, 1e-9) {
		t.Errorf("per-task PassK = %g, want 0.729", e.PassK)
	}
	// the CI bounds map through p^k and stay ordered (lo <= point <= hi) because p^k is
	// monotone increasing in p.
	if !(e.PassKLo <= e.PassK && e.PassK <= e.PassKHi) {
		t.Errorf("PassK CI not ordered: lo=%g point=%g hi=%g", e.PassKLo, e.PassK, e.PassKHi)
	}
	if !approxPK(e.PassKLo, PassK(e.WilsonLo, 3), 1e-12) {
		t.Errorf("PassKLo = %g, want PassK(WilsonLo)=%g", e.PassKLo, PassK(e.WilsonLo, 3))
	}
	// PassK=0 ⇒ no pass^k read (byte-identical anchor).
	off := estimateBernTask(BernTaskInput{TaskID: "t", Solved: 9, K: 10}, 0)
	if off.PassKAt != 0 || off.PassK != 0 {
		t.Errorf("PassK=0 must leave the pass^k fields zero, got PassKAt=%d PassK=%g", off.PassKAt, off.PassK)
	}
}

// TestPassKReportAggregateAndTrust: the report's mean pass@1 vs mean pass^k contrast,
// and the iid-trust gate — a clean iid suite trusts pass^k; the off mode leaves it
// untouched.
func TestPassKReportAggregateAndTrust(t *testing.T) {
	// two clean, non-saturated tasks (informative ⇒ no overdispersion trip on a clean
	// single launch with matched difficulty).
	offs := []BernTaskInput{
		{TaskID: "a", Solved: 8, K: 10}, // p=0.8
		{TaskID: "b", Solved: 6, K: 10}, // p=0.6
	}
	rep := EstimateBernoulli(offs, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, PassK: 3})
	if rep.PassKAt != 3 {
		t.Fatalf("report PassKAt = %d, want 3", rep.PassKAt)
	}
	wantMeanP1 := (0.8 + 0.6) / 2
	if !approxPK(rep.MeanPass1, wantMeanP1, 1e-9) {
		t.Errorf("MeanPass1 = %g, want %g", rep.MeanPass1, wantMeanP1)
	}
	wantMeanPK := (PassK(0.8, 3) + PassK(0.6, 3)) / 2
	if !approxPK(rep.MeanPassK, wantMeanPK, 1e-9) {
		t.Errorf("MeanPassK = %g, want %g", rep.MeanPassK, wantMeanPK)
	}
	// reliability gap: mean pass^k is materially BELOW mean pass@1 (the brittleness).
	if rep.MeanPassK >= rep.MeanPass1 {
		t.Errorf("MeanPassK (%g) must be below MeanPass1 (%g) — the reliability gap", rep.MeanPassK, rep.MeanPass1)
	}
	// on a clean iid suite the pass^k read is TRUSTED.
	if !rep.PassKTrusted {
		t.Errorf("clean iid suite should TRUST pass^k; PassKTrusted=false (overdispersed=%v)", rep.Overdispersion.Overdispersed)
	}
	// off mode (PassK unset) ⇒ no pass^k pass; the report fields stay zero.
	repOff := EstimateBernoulli(offs, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn})
	if repOff.PassKAt != 0 || repOff.MeanPassK != 0 || repOff.PassKTrusted {
		t.Errorf("PassK unset must leave the pass^k report fields zero/untrusted, got PassKAt=%d MeanPassK=%g trusted=%v",
			repOff.PassKAt, repOff.MeanPassK, repOff.PassKTrusted)
	}
}

// TestPassKTrustRevokedOnOverdispersion: a cross-launch overdispersion trip (shared
// shock) revokes pass^k trust — the iid precondition the p^k closed form rests on is
// violated, so the report must flag pass^k untrustworthy (and emit the note).
func TestPassKTrustRevokedOnOverdispersion(t *testing.T) {
	offs := []BernTaskInput{
		{TaskID: "a", Solved: 5, K: 10},
		{TaskID: "b", Solved: 5, K: 10},
	}
	// a multi-launch rate matrix where each task's rate swings WIDELY launch-to-launch
	// (a shared shock) — cross-launch variance >> within-launch Bernoulli variance ⇒
	// OVERDISPERSED.
	crossRates := [][]float64{
		{0.1, 0.1},
		{0.9, 0.9},
		{0.1, 0.1},
		{0.9, 0.9},
	}
	crossKs := []int{10, 10}
	rep := EstimateBernoulli(offs, nil, nil, crossRates, crossKs, BernoulliConfig{Mode: EstBernOn, PassK: 3})
	if !rep.Overdispersion.Overdispersed {
		t.Fatalf("the planted shared shock should trip OVERDISPERSED (stat=%g)", rep.Overdispersion.Statistic)
	}
	if rep.PassKTrusted {
		t.Error("overdispersion must REVOKE pass^k trust (the iid p^k closed form is invalid)")
	}
	// the report must carry an honest untrustworthy note.
	joined := strings.Join(rep.Notes, " ")
	if !strings.Contains(joined, "pass^k UNTRUSTWORTHY") {
		t.Errorf("expected a pass^k untrustworthy note when overdispersed; notes=%v", rep.Notes)
	}
}
