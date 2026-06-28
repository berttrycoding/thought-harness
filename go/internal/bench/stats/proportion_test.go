package stats

import (
	"math"
	"testing"
)

func TestWilsonInterval(t *testing.T) {
	tests := []struct {
		name           string
		s, n           int
		wantLo, wantHi float64
		tol            float64
	}{
		{
			// 0/20 at 95%: known Wilson interval [0, 0.16113]. The lower bound is
			// exactly 0 (the Wilson interval stays inside [0,1] at the boundary,
			// unlike Wald which would go negative).
			name: "0-of-20", s: 0, n: 20,
			wantLo: 0.0, wantHi: 0.16112515805281938, tol: 1e-9,
		},
		{
			// 10/20 at 95%: symmetric known interval [0.29930, 0.70070].
			name: "10-of-20", s: 10, n: 20,
			wantLo: 0.2992980081982123, wantHi: 0.7007019918017877, tol: 1e-9,
		},
		{
			// 20/20: upper bound 1, lower bound < 1 (exact Wilson lower 0.838875).
			name: "20-of-20", s: 20, n: 20,
			wantLo: 0.8388748419471806, wantHi: 1.0, tol: 1e-9,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ci := WilsonInterval(tc.s, tc.n, 0.95)
			if !approx(ci.Lower, tc.wantLo, tc.tol) {
				t.Errorf("Lower = %.10f, want %.10f", ci.Lower, tc.wantLo)
			}
			if !approx(ci.Upper, tc.wantHi, tc.tol) {
				t.Errorf("Upper = %.10f, want %.10f", ci.Upper, tc.wantHi)
			}
		})
	}
}

func TestTwoAFC(t *testing.T) {
	// 18/20 correct: accuracy 0.9, Wilson lower well above 0.5 -> AboveChance.
	r := TwoAFC(18, 20, 0.95)
	if !approx(r.Accuracy, 0.9, 1e-12) {
		t.Errorf("accuracy = %v, want 0.9", r.Accuracy)
	}
	if !r.AboveChance {
		t.Errorf("18/20 should clear chance (Wilson lower %v > 0.5)", r.CI.Lower)
	}
	// 11/20: accuracy 0.55 but the Wilson lower bound dips below 0.5 -> not a
	// certified above-chance discriminator.
	r2 := TwoAFC(11, 20, 0.95)
	if r2.AboveChance {
		t.Errorf("11/20 Wilson lower %v should NOT clear 0.5", r2.CI.Lower)
	}
}

func TestRuleOfThree(t *testing.T) {
	// 0/120 -> 3/120 = 0.025 (the safety claim's quoted ~2.5% ceiling).
	if got := RuleOfThree(120); !approx(got, 0.025, 1e-12) {
		t.Errorf("RuleOfThree(120) = %v, want 0.025", got)
	}
	if got := RuleOfThree(100); !approx(got, 0.03, 1e-12) {
		t.Errorf("RuleOfThree(100) = %v, want 0.03", got)
	}
}

func TestClopperPearsonUpper(t *testing.T) {
	// Exact zero-event upper bound for 0/120 at 95%: 1-(0.025)^(1/120) ≈ 0.030273.
	// (The rule-of-three 0.025 is the approximation; the exact bound is slightly
	// higher, as the spec notes.)
	got := ClopperPearsonUpper(0, 120, 0.95)
	if !approx(got, 0.03027297257742012, 1e-6) {
		t.Errorf("ClopperPearsonUpper(0,120) = %.8f, want 0.0302730", got)
	}
	// The exact bound exceeds the rule-of-three approximation.
	if got <= RuleOfThree(120) {
		t.Errorf("exact CP upper %.5f should exceed rule-of-three %.5f", got, RuleOfThree(120))
	}
	// successes == n -> upper bound is 1.
	if u := ClopperPearsonUpper(20, 20, 0.95); u != 1 {
		t.Errorf("ClopperPearsonUpper(20,20) = %v, want 1", u)
	}
	// A non-zero-event case: 2/100 at 95%. Exact upper ≈ 0.070237 (R binom.test).
	got2 := ClopperPearsonUpper(2, 100, 0.95)
	if !approx(got2, 0.07037011, 1e-4) {
		t.Errorf("ClopperPearsonUpper(2,100) = %.6f, want ~0.07037", got2)
	}
}

func TestNormalRoundTrip(t *testing.T) {
	// Sanity on the shared normal helpers the rest of the package leans on.
	for _, p := range []float64{0.01, 0.1, 0.25, 0.5, 0.75, 0.9, 0.975, 0.99} {
		z := normalQuantile(p)
		back := normalCDF(z)
		if math.Abs(back-p) > 1e-9 {
			t.Errorf("normalQuantile/CDF round-trip failed at p=%v: back=%v", p, back)
		}
	}
	if !approx(normalQuantile(0.975), 1.959963984540054, 1e-9) {
		t.Errorf("z_0.975 wrong: %v", normalQuantile(0.975))
	}
}
