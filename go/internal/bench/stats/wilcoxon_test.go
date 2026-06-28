package stats

import "testing"

func TestWilcoxonSignedRank(t *testing.T) {
	tests := []struct {
		name   string
		diffs  []float64
		wantW  float64 // W+ (sum of positive ranks)
		wantWm float64 // W-
		wantN  int
		wantRB float64 // rank-biserial
		wantZ  float64
		zTol   float64
	}{
		{
			// Published no-tie example: |diffs| = 1..10, the values at |2| and |5|
			// are negative. Ranks of negatives = 2 + 5 = 7 = W-; W+ = 55-7 = 48.
			// rank-biserial = (48-7)/55 = 0.7454..., z (cc) = 2.03859.
			name:  "published-no-tie",
			diffs: []float64{1, -2, 3, 4, -5, 6, 7, 8, 9, 10},
			wantW: 48, wantWm: 7, wantN: 10,
			wantRB: 41.0 / 55.0, wantZ: 2.0385887657505024, zTol: 1e-9,
		},
		{
			// Tie example (exercises the average-rank + tie-variance correction):
			// |diffs| {0.5,0.5,0.5,1,1,2,3,3}, signs as below. Hand-computed
			// W+ = 28, W- = 8 over n=8, tie correction sum(t^3-t)=36 -> var=50.25.
			name:  "with-ties",
			diffs: []float64{0.5, 0.5, -0.5, 1.0, 1.0, -2.0, 3.0, 3.0},
			wantW: 28, wantWm: 8, wantN: 8,
			wantRB: 20.0 / 36.0, wantZ: 1.3401566701313368, zTol: 1e-9,
		},
		{
			// Zeros are dropped before ranking; only the 3 non-zero diffs count.
			name:  "drops-zeros",
			diffs: []float64{0, 0, 1, -2, 3, 0},
			wantW: 1 + 3, wantWm: 2, wantN: 3,
			wantRB: (4.0 - 2.0) / 6.0, wantZ: 0, zTol: 1, // z not asserted tightly (small n)
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := WilcoxonSignedRank(tc.diffs)
			if r.W != tc.wantW {
				t.Errorf("W+ = %v, want %v", r.W, tc.wantW)
			}
			if r.WMinus != tc.wantWm {
				t.Errorf("W- = %v, want %v", r.WMinus, tc.wantWm)
			}
			if r.N != tc.wantN {
				t.Errorf("N = %d, want %d", r.N, tc.wantN)
			}
			if !approx(r.RankBiserial, tc.wantRB, 1e-9) {
				t.Errorf("RankBiserial = %v, want %v", r.RankBiserial, tc.wantRB)
			}
			if tc.zTol < 1 && !approx(r.Z, tc.wantZ, tc.zTol) {
				t.Errorf("Z = %v, want %v", r.Z, tc.wantZ)
			}
			if r.P < 0 || r.P > 1 {
				t.Errorf("P out of [0,1]: %v", r.P)
			}
		})
	}
}

func TestWilcoxonEmpty(t *testing.T) {
	r := WilcoxonSignedRank([]float64{0, 0, 0})
	if r.N != 0 || r.P != 1 {
		t.Errorf("all-zero diffs should give N=0,P=1; got N=%d P=%v", r.N, r.P)
	}
}
