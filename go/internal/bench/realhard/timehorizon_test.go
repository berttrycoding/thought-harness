package realhard

import (
	"math"
	"testing"
)

// timehorizon_test.go — the METR-style time-horizon calculator (design doc §7.4).
// Plants a KNOWN logistic (a chosen b0,b1) and asserts the WLS fit recovers the
// 50%-reliable horizon, the monotone reliability band, the degenerate guards, and the
// unidentified (non-negative slope) honest NA.

func approxTH(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// TestTimeHorizonRecoversPlantedH50: build per-task solve-rates EXACTLY on a known
// logistic logit(p) = b0 + b1*log2(min). With high K (so p̂≈p) the WLS fit must
// recover b0,b1 and thus H50 = 2^(-b0/b1) to good precision.
func TestTimeHorizonRecoversPlantedH50(t *testing.T) {
	// planted: H50 = 30 minutes, slope -1 logit per doubling.
	// logit(p) = b1*(log2(min) - log2(H50)), b1=-1, so b0 = -b1*log2(30) = log2(30).
	b1 := -1.0
	h50 := 30.0
	b0 := -b1 * math.Log2(h50)
	mins := []float64{1, 4, 15, 30, 60, 240, 960}
	var tasks []THTask
	for i, m := range mins {
		p := invLogit(b0 + b1*math.Log2(m))
		tasks = append(tasks, THTask{
			TaskID:   string(rune('a'+i)) + "-task",
			HumanMin: m,
			PHat:     p,
			K:        1000, // high K ⇒ p̂≈p, the planted curve
		})
	}
	r := TimeHorizon(tasks)
	if !r.Fitted {
		t.Fatalf("fit should succeed on a planted clean logistic; got NA: %s", r.Reason)
	}
	if !approxTH(r.Beta1, b1, 0.05) {
		t.Errorf("recovered b1=%.4f, want %.4f", r.Beta1, b1)
	}
	if !approxTH(r.Horizon50, h50, 1.0) {
		t.Errorf("recovered H50=%.2fmin, want %.2f", r.Horizon50, h50)
	}
	// the reliability band is ordered: a HIGHER reliability demands a SHORTER task
	// (with a negative slope), so H80 < H50 < H20.
	if !(r.Horizon80 < r.Horizon50 && r.Horizon50 < r.Horizon20) {
		t.Errorf("reliability band must be ordered H80<H50<H20; got %.2f, %.2f, %.2f", r.Horizon80, r.Horizon50, r.Horizon20)
	}
	// at exactly H50 the model predicts ~0.5.
	if pred, ok := r.FittedProbabilityAt(r.Horizon50); !ok || !approxTH(pred, 0.5, 1e-6) {
		t.Errorf("model at H50 should predict ~0.5, got %g (ok=%v)", pred, ok)
	}
}

// TestTimeHorizonDegenerate: <2 distinct-length tasks ⇒ honest NA, no fabricated horizon.
func TestTimeHorizonDegenerate(t *testing.T) {
	// one task only.
	one := TimeHorizon([]THTask{{TaskID: "a", HumanMin: 10, PHat: 0.5, K: 10}})
	if one.Fitted || one.Horizon50 != 0 {
		t.Errorf("single task must be DEGENERATE NA, got fitted=%v H50=%g", one.Fitted, one.Horizon50)
	}
	// two tasks at the SAME length ⇒ no slope identifiable.
	same := TimeHorizon([]THTask{
		{TaskID: "a", HumanMin: 10, PHat: 0.8, K: 10},
		{TaskID: "b", HumanMin: 10, PHat: 0.3, K: 10},
	})
	if same.Fitted {
		t.Errorf("two tasks at the SAME length must be DEGENERATE (zero length-variance); got fitted with H50=%g", same.Horizon50)
	}
	// tasks with no length / no measurement are dropped.
	dropped := TimeHorizon([]THTask{
		{TaskID: "a", HumanMin: 0, PHat: 0.5, K: 10},  // no length
		{TaskID: "b", HumanMin: 10, PHat: 0.5, K: 0},  // no measurement
		{TaskID: "c", HumanMin: 20, PHat: 0.5, K: 10}, // the only placeable, measured one
	})
	if dropped.Fitted {
		t.Errorf("only one placeable+measured task survives ⇒ DEGENERATE; got fitted=%v NEff=%d", dropped.Fitted, dropped.NEff)
	}
}

// TestTimeHorizonUnidentifiedOnPositiveSlope: when LONGER tasks are EASIER (positive
// slope), the horizon is not identifiable — the calc reports an honest UNIDENTIFIED
// NA rather than a meaningless number.
func TestTimeHorizonUnidentifiedOnPositiveSlope(t *testing.T) {
	// solve-rate RISES with length (the opposite of the autonomy assumption).
	tasks := []THTask{
		{TaskID: "a", HumanMin: 1, PHat: 0.2, K: 1000},
		{TaskID: "b", HumanMin: 10, PHat: 0.5, K: 1000},
		{TaskID: "c", HumanMin: 100, PHat: 0.8, K: 1000},
	}
	r := TimeHorizon(tasks)
	if r.Fitted {
		t.Errorf("a positive slope (longer=easier) must be UNIDENTIFIED, not a horizon; got H50=%g", r.Horizon50)
	}
	if r.Beta1 < 0 {
		t.Errorf("planted positive-slope data should recover b1>=0, got %g", r.Beta1)
	}
}

// TestTimeHorizonRenderNonEmpty: the report renders for both fitted and NA.
func TestTimeHorizonRenderNonEmpty(t *testing.T) {
	fitted := TimeHorizon([]THTask{
		{TaskID: "short", HumanMin: 1, PHat: 0.95, K: 100},
		{TaskID: "mid", HumanMin: 30, PHat: 0.5, K: 100},
		{TaskID: "long", HumanMin: 600, PHat: 0.1, K: 100},
	})
	if !fitted.Fitted {
		t.Fatalf("expected a fitted horizon: %s", fitted.Reason)
	}
	if s := fitted.Render(); len(s) == 0 {
		t.Error("fitted Render is empty")
	}
	na := TimeHorizon(nil)
	if s := na.Render(); len(s) == 0 {
		t.Error("NA Render is empty")
	}
}

// TestFmtMinutes: the legible-unit formatter (pure, no clock).
func TestFmtMinutes(t *testing.T) {
	cases := []struct {
		m    float64
		want string
	}{
		{0.5, "30s"},
		{5, "5.0min"},
		{90, "1.5h"},
		{2880, "2.0d"},
	}
	for _, c := range cases {
		if got := fmtMinutes(c.m); got != c.want {
			t.Errorf("fmtMinutes(%g) = %q, want %q", c.m, got, c.want)
		}
	}
}
