package stats

import (
	"math"
	"testing"
)

// approx reports whether got is within tol of want.
func approx(got, want, tol float64) bool {
	return math.Abs(got-want) <= tol
}

func TestMcNemar(t *testing.T) {
	tests := []struct {
		name      string
		b, c      int
		wantExact float64 // known two-sided exact p (hand-computed)
		wantOR    float64
		tol       float64
	}{
		{
			// Textbook 2x2 (classic McNemar worked example): 12 discordant one
			// way, 5 the other. Exact two-sided p = 0.143463..., OR = 12/5 = 2.4.
			name: "textbook 12-vs-5", b: 12, c: 5,
			wantExact: 0.143463134765625, wantOR: 2.4, tol: 1e-9,
		},
		{
			// Just-significant case: b=21, c=9. Exact two-sided p = 0.042774,
			// OR = 21/9 = 2.3333.
			name: "significant 21-vs-9", b: 21, c: 9,
			wantExact: 0.04277394525706768, wantOR: 21.0 / 9.0, tol: 1e-9,
		},
		{
			// Symmetric discordant split -> no evidence -> p = 1.
			name: "symmetric 8-vs-8", b: 8, c: 8,
			wantExact: 1.0, wantOR: 1.0, tol: 1e-12,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := McNemar(tc.b, tc.c)
			if !approx(r.PExact, tc.wantExact, tc.tol) {
				t.Errorf("PExact = %.12f, want %.12f", r.PExact, tc.wantExact)
			}
			if !approx(r.OddsRatio, tc.wantOR, 1e-12) {
				t.Errorf("OddsRatio = %v, want %v", r.OddsRatio, tc.wantOR)
			}
			// mid-p must be <= exact (less conservative), both in [0,1].
			if r.PMid > r.PExact+1e-12 {
				t.Errorf("PMid %.6f should be <= PExact %.6f", r.PMid, r.PExact)
			}
			if r.PExact < 0 || r.PExact > 1 || r.PMid < 0 || r.PMid > 1 {
				t.Errorf("p out of [0,1]: exact=%v mid=%v", r.PExact, r.PMid)
			}
		})
	}
}

func TestMcNemarEdgeCases(t *testing.T) {
	// No discordant pairs: p=1, OR undefined (NaN).
	r := McNemar(0, 0)
	if r.PExact != 1 || r.PMid != 1 {
		t.Errorf("empty discordant: p should be 1, got exact=%v mid=%v", r.PExact, r.PMid)
	}
	if !math.IsNaN(r.OddsRatio) {
		t.Errorf("empty discordant: OR should be NaN, got %v", r.OddsRatio)
	}
	// c==0, b>0: OR = +Inf.
	r = McNemar(7, 0)
	if !math.IsInf(r.OddsRatio, 1) {
		t.Errorf("c==0: OR should be +Inf, got %v", r.OddsRatio)
	}
	// b==0, c>0: OR = 0.
	r = McNemar(0, 7)
	if r.OddsRatio != 0 {
		t.Errorf("b==0: OR should be 0, got %v", r.OddsRatio)
	}
}
