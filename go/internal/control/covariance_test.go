package control

import (
	"math"
	"testing"
)

// TestCorrelatedInflation_RaisesSiblingVariance is the M2 thinking: when reality REFUTES a belief, a
// belief that CO-VARIES with it (shared upstream) becomes LESS certain — its variance must GROW, because
// the shared grounding that backed it just proved unreliable. This is "detect correlated self-deception".
func TestCorrelatedInflation_RaisesSiblingVariance(t *testing.T) {
	prior := 0.5
	// fully correlated (rho=1), a hard contradiction (|nu|=1.9).
	post := CorrelatedInflation(prior, 1.0, 1.9)
	if post <= prior {
		t.Fatalf("a correlated refutation must RAISE the sibling variance; prior=%v post=%v", prior, post)
	}
	// a weakly-correlated sibling loses LESS certainty than a strongly-correlated one.
	weak := CorrelatedInflation(prior, 0.3, 1.9)
	if !(weak > prior && weak < post) {
		t.Fatalf("weaker correlation must inflate less: prior=%v weak=%v strong=%v", prior, weak, post)
	}
	// a harder contradiction loses MORE certainty.
	soft := CorrelatedInflation(prior, 1.0, 0.4)
	if !(soft > prior && soft < post) {
		t.Fatalf("a softer contradiction must inflate less: soft=%v hard=%v", soft, post)
	}
}

// TestCorrelatedInflation_IndependentBeliefUntouched is the consistency floor: a belief that shares NO
// upstream (rho=0) is INDEPENDENT, so a refutation elsewhere must leave it EXACTLY unchanged — the sparse
// graph stores no edge, the math is a no-op (byte-identical when there are no correlations).
func TestCorrelatedInflation_IndependentBeliefUntouched(t *testing.T) {
	for _, prior := range []float64{0.1, 0.5, 1.0} {
		if got := CorrelatedInflation(prior, 0, 1.9); got != prior {
			t.Fatalf("rho=0 must be a no-op; prior=%v got=%v", prior, got)
		}
	}
	// no contradiction (innovMag=0) is also a no-op even with full correlation.
	if got := CorrelatedInflation(0.5, 1.0, 0); got != 0.5 {
		t.Fatalf("innovMag=0 must be a no-op; got=%v", got)
	}
}

// TestCorrelatedInflation_NeverShrinks is the load-bearing M5/§0 guarantee: a correlated propagation may
// ONLY raise variance (lose certainty), NEVER lower it — becoming less certain can never be spurious
// information. This is what keeps M2 inside the consistency invariant the M5 witness guards: only a
// direct grounded observation (Innovate) may shrink a variance.
func TestCorrelatedInflation_NeverShrinks(t *testing.T) {
	for _, prior := range []float64{0.05, 0.3, 0.9, 1.0, 2.0} {
		for _, rho := range []float64{0, 0.25, 0.5, 0.9, 1.0, 1.5 /* clamped */} {
			for _, mag := range []float64{0, 0.5, 1.9, 5.0, 50.0 /* saturates */} {
				if got := CorrelatedInflation(prior, rho, mag); got < prior {
					t.Fatalf("inflation must never shrink variance: prior=%v rho=%v mag=%v got=%v", prior, rho, mag, got)
				}
			}
		}
	}
}

// TestCorrelationCoefficient_SaturatesInShared pins the shared-upstream -> rho mapping: 0 shared ->
// independent (rho 0), the FIRST shared upstream already establishes strong correlation (rho 0.5), and
// additional shared ancestors push toward 1 with diminishing returns and never exceed 1.
func TestCorrelationCoefficient_SaturatesInShared(t *testing.T) {
	if got := CorrelationCoefficient(0); got != 0 {
		t.Fatalf("0 shared upstreams must be independent (rho 0); got %v", got)
	}
	if got := CorrelationCoefficient(1); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("the first shared upstream must give rho 0.5; got %v", got)
	}
	prev := 0.0
	for n := 1; n <= 10; n++ {
		got := CorrelationCoefficient(n)
		if got <= prev {
			t.Fatalf("rho must be monotone increasing in shared count; n=%d got=%v prev=%v", n, got, prev)
		}
		if got >= 1.0 {
			t.Fatalf("rho must stay strictly below 1; n=%d got=%v", n, got)
		}
		prev = got
	}
}

// TestTanhPos_MatchesStdlib guards the local tanh/exp helpers (kept local for leaf-purity) against the
// stdlib so a refactor can't silently break the saturation curve the inflation relies on.
func TestTanhPos_MatchesStdlib(t *testing.T) {
	for _, x := range []float64{0, 0.1, 0.5, 1.0, 1.9, 3.0, 10.0} {
		want := math.Tanh(x)
		got := tanhPos(x)
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("tanhPos(%v)=%v, stdlib tanh=%v", x, got, want)
		}
	}
}
