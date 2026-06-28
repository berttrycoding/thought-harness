package grounding

import (
	"math"
	"testing"
)

// TestEvaluateCompute is the N.1b gate: a computable claim is resolved DETERMINISTICALLY — a correct
// arithmetic equality grounds, a wrong one is refuted, and a non-arithmetic claim defers (NotComputable
// — never guessed). The whole table is fixed input -> fixed verdict, no model, fully reproducible.
func TestEvaluateCompute(t *testing.T) {
	cases := []struct {
		claim string
		want  Verdict
	}{
		// grounded equalities
		{"12 * 31 = 372", Grounded},
		{"2 + 2 = 4", Grounded},
		{"the answer is 50 / 2 = 25", Grounded}, // extracted from text
		{"(3 + 4) * 2 = 14", Grounded},          // precedence + parens
		{"2 ^ 10 = 1024", Grounded},             // exponent
		{"0.1 + 0.2 = 0.3", Grounded},           // float tolerance
		{"-5 + 3 = -2", Grounded},               // unary minus
		{"100 - 1 != 98", Grounded},             // a true inequality
		{"999999 + 1 = 1000000", Grounded},      // large EXACT arithmetic must still ground
		// refuted equalities
		{"2 + 2 = 5", Refuted},
		{"12 * 31 = 370", Refuted},
		{"8472 / 31 = 99", Refuted},
		{"3 != 3", Refuted},            // a false inequality
		{"1000000 = 1000001", Refuted}, // off-by-one at magnitude 1e6 must NOT ground (the *1e3 rel-tol bug)
		{"5000000 = 5000003", Refuted}, // off-by-three at magnitude 5e6 must NOT ground
		// not computable
		{"the sky is blue", NotComputable},
		{"the refactor is safe to ship", NotComputable},
		{"x + 1 = 5", NotComputable}, // symbolic, not arithmetic (no value for x)
		{"", NotComputable},
	}
	for _, c := range cases {
		got := EvaluateCompute(c.claim)
		if got.Verdict != c.want {
			t.Errorf("EvaluateCompute(%q) = %v (%s), want %v", c.claim, got.Verdict, got.Detail, c.want)
		}
	}
}

// TestEvaluateComputeIsDeterministic runs the same claim many times and asserts an identical verdict +
// computed value every time (no wall clock, no randomness) — the property that makes it trustworthy
// ground truth.
func TestEvaluateComputeIsDeterministic(t *testing.T) {
	first := EvaluateCompute("8472 / 31 = 273.29")
	for i := 0; i < 100; i++ {
		got := EvaluateCompute("8472 / 31 = 273.29")
		if got.Verdict != first.Verdict || math.Abs(got.Computed-first.Computed) > 1e-12 {
			t.Fatalf("non-deterministic: run %d = %v/%v, first = %v/%v", i, got.Verdict, got.Computed, first.Verdict, first.Computed)
		}
	}
	// 8472/31 = 273.290..., so the claim "= 273.29" is close but NOT within tolerance -> refuted (the
	// evaluator does not round a wrong-but-close claim into truth).
	if first.Verdict != Refuted {
		t.Fatalf("8472/31=273.29 should be refuted (real value ~273.29032); got %v", first.Verdict)
	}
}

// TestComputeRefutesAGuess is the grounding scenario in miniature: an internal guess that is arithmetically
// wrong is REFUTED by deterministic computation — exactly what the grounding loop needs from this layer.
func TestComputeRefutesAGuess(t *testing.T) {
	guess := "I worked it out: 137 * 9 = 1233" // real value 1233 -> actually grounded
	if EvaluateCompute(guess).Verdict != Grounded {
		t.Fatalf("137*9=1233 is correct and should ground")
	}
	wrong := "I worked it out: 137 * 9 = 1333"
	if EvaluateCompute(wrong).Verdict != Refuted {
		t.Fatalf("137*9=1333 is wrong and must be refuted, not accepted")
	}
}

// TestEvaluateComputeNormalizesGlyphs covers the typographic math glyphs (× ÷ −) a model or the UI
// voices: they normalize to ASCII so the claim still grounds/refutes, and Result.Claim carries the
// clean ASCII span the engine keys the ledger on.
func TestEvaluateComputeNormalizesGlyphs(t *testing.T) {
	cases := []struct {
		in      string
		verdict Verdict
		claim   string
	}{
		{"It comes to me: 12 × 31 = 372.", Grounded, "12 * 31 = 372"},
		{"7 × 8 = 54", Refuted, "7 * 8 = 54"},
		{"100 ÷ 4 = 25", Grounded, "100 / 4 = 25"},
		{"10 − 3 = 7", Grounded, "10 - 3 = 7"},
		{"the cat sat on the mat", NotComputable, ""},
	}
	for _, c := range cases {
		r := EvaluateCompute(c.in)
		if r.Verdict != c.verdict {
			t.Errorf("%q: verdict = %v, want %v", c.in, r.Verdict, c.verdict)
		}
		if r.Claim != c.claim {
			t.Errorf("%q: claim = %q, want %q", c.in, r.Claim, c.claim)
		}
	}
}
