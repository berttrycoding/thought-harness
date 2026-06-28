package control

import (
	"math"
	"testing"
)

// TestExpectedInfoGain_ZeroWhenNothingToLearn pins the boundary cases: an already-certain belief
// (priorVar 0) or an observation that carries no information (obsPrec 0) yields ZERO expected info gain
// — the next-best-observation never picks a belief reality cannot teach it anything new about.
func TestExpectedInfoGain_ZeroWhenNothingToLearn(t *testing.T) {
	if g := ExpectedInfoGain(0, 8, 3); g != 0 {
		t.Fatalf("certain belief (priorVar=0): want 0 gain, got %v", g)
	}
	if g := ExpectedInfoGain(-1, 8, 3); g != 0 {
		t.Fatalf("negative priorVar: want 0 gain, got %v", g)
	}
	if g := ExpectedInfoGain(1.0, 0, 3); g != 0 {
		t.Fatalf("zero-precision obs: want 0 gain, got %v", g)
	}
}

// TestExpectedInfoGain_MonotoneInUncertainty is the next-best-VIEW criterion: at equal precision and
// reach, a MORE-uncertain belief (higher priorVar) yields a strictly larger expected info gain — so the
// ranking prefers grounding the belief the harness is least sure about (directed grounding).
func TestExpectedInfoGain_MonotoneInUncertainty(t *testing.T) {
	low := ExpectedInfoGain(0.2, 8, 0)
	mid := ExpectedInfoGain(1.0, 8, 0)
	high := ExpectedInfoGain(5.0, 8, 0)
	if !(low < mid && mid < high) {
		t.Fatalf("info gain must rise with uncertainty: low=%v mid=%v high=%v", low, mid, high)
	}
	// And it saturates BELOW obsPrec (the most information a single scalar observation can add).
	if high >= 8 {
		t.Fatalf("self-gain must stay below obsPrec=8, got %v", high)
	}
}

// TestExpectedInfoGain_CorrelationReachLeveragesJointGain is the active-SLAM payoff and the load-bearing
// cognition: grounding a belief that MANY siblings co-vary with (high corrReach) is worth MORE than
// grounding an isolated belief of the SAME variance — because the observation's information propagates
// across everything the shared root backs. A high-fan-out root beats an equal-variance leaf.
func TestExpectedInfoGain_CorrelationReachLeveragesJointGain(t *testing.T) {
	isolated := ExpectedInfoGain(1.0, 8, 0) // a leaf: no co-varying siblings
	root := ExpectedInfoGain(1.0, 8, 2.5)   // a shared root: siblings sum rho=2.5
	if root <= isolated {
		t.Fatalf("a high-fan-out root must beat an equal-variance leaf: root=%v isolated=%v", root, isolated)
	}
	// Specifically the joint gain scales by exactly (1 + corrReach).
	if want := isolated * (1 + 2.5); math.Abs(root-want) > 1e-9 {
		t.Fatalf("joint gain = selfGain*(1+reach): want %v got %v", want, root)
	}
	// A negative reach clamps to 0 (defensive) — same as isolated.
	if g := ExpectedInfoGain(1.0, 8, -3); math.Abs(g-isolated) > 1e-9 {
		t.Fatalf("negative reach must clamp to isolated: want %v got %v", isolated, g)
	}
}

// TestExpectedInfoGain_Deterministic locks that the math is a pure function — identical inputs always
// yield bit-identical outputs (the seeded engine loop depends on this for reproducible goldens).
func TestExpectedInfoGain_Deterministic(t *testing.T) {
	for i := 0; i < 1000; i++ {
		a := ExpectedInfoGain(1.3, 4, 1.5)
		b := ExpectedInfoGain(1.3, 4, 1.5)
		if a != b {
			t.Fatalf("non-deterministic: %v != %v", a, b)
		}
	}
}
