package judge

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// TestRunNilBackendAbstains: no backend → not backed (the caller falls back to the floor).
func TestRunNilBackendAbstains(t *testing.T) {
	if v := Run("the answer is 6", "states 6", nil); v.Backed {
		t.Fatalf("nil backend must not be backed, got %+v", v)
	}
}

// TestRunDeterministicUnderTestDouble: the offline double yields a stable verdict per (output,
// rubric) pair (the Phase-0 test-retest property).
func TestRunDeterministicUnderTestDouble(t *testing.T) {
	a := Run("the answer is 45s, read from config/net.go", "states the grounded value 45s", backends.NewTest())
	b := Run("the answer is 45s, read from config/net.go", "states the grounded value 45s", backends.NewTest())
	if a.Pass != b.Pass || a.Backed != b.Backed || a.Confidence != b.Confidence {
		t.Fatalf("judge not deterministic under the test double: %+v vs %+v", a, b)
	}
}

// TestAffirmsPass: the conservative pass-detection — an explicit affirmative passes, a "fail"/"no"
// veto wins even alongside "pass", and ambiguous text abstains to FAIL.
func TestAffirmsPass(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"PASS — the value matches the source", true},
		{"yes, correct", true},
		{"FAIL: it cited the documented default", false},
		{"No, it cites the documented prior rather than the source", false}, // "no" prefix vetoes
		{"This passes but actually fails the source check", false},          // "fail" anywhere vetoes
		{"the answer is unclear", false},
		{"", false},
	}
	for _, c := range cases {
		if got := affirmsPass(c.raw); got != c.want {
			t.Errorf("affirmsPass(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
