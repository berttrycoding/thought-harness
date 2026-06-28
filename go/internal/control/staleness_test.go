package control

import (
	"math"
	"testing"
)

// TestStalenessInflation_NoDecayCases pins the byte-identical-OFF + fresh + saturated cases: a q<=0
// (stationary), age<=0 (fresh), or already-at-ceiling belief is returned UNCHANGED.
func TestStalenessInflation_NoDecayCases(t *testing.T) {
	cases := []struct {
		name                      string
		priorVar, age, q, ceiling float64
	}{
		{"stationary q=0", 0.2, 100, 0, 1.0},
		{"negative q", 0.2, 100, -0.5, 1.0},
		{"fresh age=0", 0.2, 0, 0.1, 1.0},
		{"fresh age<0", 0.2, -3, 0.1, 1.0},
		{"already at ceiling", 1.0, 50, 0.1, 1.0},
		{"already above ceiling", 1.2, 50, 0.1, 1.0},
	}
	for _, c := range cases {
		got := StalenessInflation(c.priorVar, c.age, c.q, c.ceiling)
		if got != c.priorVar {
			t.Errorf("%s: expected unchanged %v, got %v", c.name, c.priorVar, got)
		}
	}
}

// TestStalenessInflation_GrowsTowardCeiling is the core P4 property: a grounded (low-variance) belief left
// un-refreshed GROWS its variance, monotone in age, saturating toward but never exceeding the ceiling.
func TestStalenessInflation_GrowsTowardCeiling(t *testing.T) {
	const priorVar, q, ceiling = 0.1, 0.2, 1.0
	prev := priorVar
	for age := 1; age <= 50; age++ {
		got := StalenessInflation(priorVar, float64(age), q, ceiling)
		if got <= prev {
			t.Fatalf("age=%d: variance must grow monotonically with un-refreshed age; prev=%v got=%v", age, prev, got)
		}
		if got > ceiling {
			t.Fatalf("age=%d: decayed variance %v exceeded the ceiling %v", age, got, ceiling)
		}
		prev = got
	}
	// far in the future it must be (near-)saturated at the ceiling — a forever-un-refreshed belief is as
	// uncertain as a never-grounded one, never more.
	far := StalenessInflation(priorVar, 1000, q, ceiling)
	if math.Abs(far-ceiling) > 1e-6 {
		t.Fatalf("a forever-stale belief must saturate AT the ceiling; got %v (ceiling %v)", far, ceiling)
	}
}

// TestStalenessInflation_GeometricForm checks the closed form against the per-tick recurrence: applying
// the one-tick decay `age` times in sequence must equal the single closed-form call for that age (the
// "one call covers an arbitrary gap" guarantee — semigroup property of the geometric approach).
func TestStalenessInflation_GeometricForm(t *testing.T) {
	const q, ceiling = 0.15, 1.0
	v := 0.05
	stepwise := v
	for i := 0; i < 12; i++ {
		stepwise = StalenessInflation(stepwise, 1, q, ceiling) // advance one tick at a time
	}
	oneShot := StalenessInflation(v, 12, q, ceiling) // the same 12-tick gap in one call
	if math.Abs(stepwise-oneShot) > 1e-9 {
		t.Fatalf("closed-form decay must equal the per-tick recurrence: stepwise=%v oneShot=%v", stepwise, oneShot)
	}
}

// TestStalenessInflation_RateMonotone: a higher process-noise rate q decays a belief FASTER (more
// uncertainty for the same age) — the dial the Dynamics field sets (slow-drift identity vs fast world).
func TestStalenessInflation_RateMonotone(t *testing.T) {
	const priorVar, age, ceiling = 0.1, 5.0, 1.0
	slow := StalenessInflation(priorVar, age, 0.05, ceiling)
	fast := StalenessInflation(priorVar, age, 0.4, ceiling)
	if fast <= slow {
		t.Fatalf("a higher q must decay faster: q=0.05 -> %v, q=0.4 -> %v", slow, fast)
	}
	if slow <= priorVar || fast <= priorVar {
		t.Fatalf("any positive q+age must grow the variance; slow=%v fast=%v prior=%v", slow, fast, priorVar)
	}
}

// TestStalenessInflation_FullDecayInOneTick: q=1 means a belief is fully stale after a single un-refreshed
// tick (the (1-q)^age = 0^age = 0 edge — the most aggressive, "trust only what I just observed" regime).
func TestStalenessInflation_FullDecayInOneTick(t *testing.T) {
	got := StalenessInflation(0.1, 1, 1.0, 1.0)
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("q=1 must fully decay to the ceiling in one tick; got %v", got)
	}
}
