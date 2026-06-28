package regulator

import (
	"math"
	"testing"
)

// TestFrontierBarrierNearZeroAtSmallN: deep in the safe region the barrier is ~0 — richness is
// essentially free until excitation climbs. -ln(1-n) at small n ≈ n, so the penalty is tiny.
func TestFrontierBarrierNearZeroAtSmallN(t *testing.T) {
	if b := FrontierBarrier(0); b != 0 {
		t.Fatalf("FrontierBarrier(0) = %v, want exactly 0", b)
	}
	for _, n := range []float64{0.01, 0.05, 0.1} {
		b := FrontierBarrier(n)
		if b <= 0 {
			t.Fatalf("FrontierBarrier(%v) = %v, want a small positive penalty", n, b)
		}
		// -ln(1-n) ≈ n for small n; a generous bound (< 0.2 at n=0.1, true value ≈ 0.105) proves "~0".
		if b > 0.2 {
			t.Fatalf("FrontierBarrier(%v) = %v, want ~0 (<0.2) deep in the safe region", n, b)
		}
	}
}

// TestFrontierBarrierMonotoneIncreasing: the penalty strictly rises with n across the whole [0,1)
// range — more excitation always costs more, so the optimizer always feels a stronger pull back the
// closer it gets to the cliff.
func TestFrontierBarrierMonotoneIncreasing(t *testing.T) {
	prev := math.Inf(-1)
	for i := 0; i <= 99; i++ {
		n := float64(i) / 100.0 // 0.00 .. 0.99
		b := FrontierBarrier(n)
		if b < prev {
			t.Fatalf("barrier not monotone: FrontierBarrier(%v)=%v < previous %v", n, b, prev)
		}
		if b <= prev && n > 0 {
			t.Fatalf("barrier not STRICTLY increasing at n=%v: %v <= previous %v", n, b, prev)
		}
		prev = b
	}
}

// TestFrontierBarrierBlowsUpToFrontier: as n → 1 the penalty grows without bound (it must dominate any
// bounded richness reward), yet stays FINITE — never +Inf/NaN — so the §5 objective stays a real number
// even when the measured n has crossed the cliff.
func TestFrontierBarrierBlowsUpToFrontier(t *testing.T) {
	b90 := FrontierBarrier(0.9)
	b99 := FrontierBarrier(0.99)
	b999 := FrontierBarrier(0.999)
	if !(b90 < b99 && b99 < b999) {
		t.Fatalf("barrier must keep rising toward the cliff: b(.9)=%v b(.99)=%v b(.999)=%v", b90, b99, b999)
	}
	// "blows up": well past any plausible richness reward by n=0.999 (-ln(0.001) ≈ 6.9).
	if b999 < 5.0 {
		t.Fatalf("FrontierBarrier(0.999) = %v, want a large penalty (>5) near the cliff", b999)
	}
	// finite at and beyond the cliff — clamped, never +Inf or NaN.
	for _, n := range []float64{0.9999999, 1.0, 1.5, 2.0} {
		b := FrontierBarrier(n)
		if math.IsInf(b, 0) || math.IsNaN(b) {
			t.Fatalf("FrontierBarrier(%v) = %v, want a large FINITE penalty (no Inf/NaN)", n, b)
		}
		if b <= b999 {
			t.Fatalf("FrontierBarrier(%v) = %v, want >= the .999 penalty %v (clamped at the cliff)", n, b, b999)
		}
	}
}

// TestFrontierBarrierNegativeNClamped: a negative n (shouldn't happen, but the EMA could undershoot) is
// clamped to 0 — no negative penalty, i.e. the optimizer gets no reward for an impossibly-calm plant.
func TestFrontierBarrierNegativeNClamped(t *testing.T) {
	if b := FrontierBarrier(-0.5); b != 0 {
		t.Fatalf("FrontierBarrier(-0.5) = %v, want 0 (negative n clamped)", b)
	}
}

// TestRegulatorFrontierBarrierReadsLiveN: the method form reads the regulator's live measured n and
// returns the same penalty as the pure function on that n — the coupling the experiment objective uses.
func TestRegulatorFrontierBarrierReadsLiveN(t *testing.T) {
	r := New(nil, nil)
	// drive n up with a fork burst so it is non-trivial.
	for i := 0; i < 6; i++ {
		o := DefaultUpdateOpts()
		o.Fired = 10
		o.Forked = 6
		o.BranchesLive = 6
		r.Update(o)
	}
	if r.N() <= 0 {
		t.Fatalf("expected a positive measured n after the burst, got %v", r.N())
	}
	got := r.FrontierBarrier()
	want := FrontierBarrier(r.N())
	if got != want {
		t.Fatalf("method FrontierBarrier()=%v != FrontierBarrier(N()=%v)=%v", got, r.N(), want)
	}
	if got <= 0 {
		t.Fatalf("a positive n must yield a positive barrier, got %v at n=%v", got, r.N())
	}
}

// TestMaxOutstandingDefaultIsWMax: the default MAX_OUTSTANDING cap is 8, mirroring W_max / the focus
// budget — the same "8 in flight" durability ceiling, applied to outstanding async actions.
func TestMaxOutstandingDefaultIsWMax(t *testing.T) {
	if MaxOutstandingDefault != 8 {
		t.Fatalf("MaxOutstandingDefault = %d, want 8 (mirrors W_max)", MaxOutstandingDefault)
	}
	r := New(nil, nil)
	if got := r.MaxOutstanding(); got != 8 {
		t.Fatalf("default Regulator.MaxOutstanding() = %d, want 8 (FocusCapacity)", got)
	}
	// it tracks the configured focus budget, not a hard-coded 8.
	cfg := DefaultConfig()
	cfg.FocusCapacity = 4
	r2 := New(nil, &cfg)
	if got := r2.MaxOutstanding(); got != 4 {
		t.Fatalf("Regulator.MaxOutstanding() = %d, want 4 (tracks FocusCapacity)", got)
	}
}

// TestOutstandingAllowedGate: the predicate admits one more fire iff current < cap (so firing keeps the
// count at or below cap), and back-pressures at the cap.
func TestOutstandingAllowedGate(t *testing.T) {
	cases := []struct {
		current, cap int
		want         bool
	}{
		{0, 8, true},  // empty — fire freely
		{7, 8, true},  // one slot left — the 8th fire is allowed (7 -> 8)
		{8, 8, false}, // at the cap — back-pressure
		{9, 8, false}, // over the cap (shouldn't happen) — still blocked
		{0, 1, true},  // a cap of 1 allows exactly one in flight
		{1, 1, false}, // ...and blocks the second
	}
	for _, c := range cases {
		if got := OutstandingAllowed(c.current, c.cap); got != c.want {
			t.Errorf("OutstandingAllowed(current=%d, cap=%d) = %v, want %v", c.current, c.cap, got, c.want)
		}
	}
}

// TestOutstandingAllowedZeroCapFallsBackToDefault: a missing / non-positive cap must NOT silently
// disable back-pressure — it falls back to MaxOutstandingDefault (8).
func TestOutstandingAllowedZeroCapFallsBackToDefault(t *testing.T) {
	if !OutstandingAllowed(7, 0) {
		t.Fatalf("OutstandingAllowed(7, cap=0) = false, want true (fall back to default 8, 7<8)")
	}
	if OutstandingAllowed(8, 0) {
		t.Fatalf("OutstandingAllowed(8, cap=0) = true, want false (fall back to default 8, 8 not < 8)")
	}
	if OutstandingAllowed(8, -3) {
		t.Fatalf("OutstandingAllowed(8, cap=-3) = true, want false (negative cap falls back to 8)")
	}
}

// TestRegulatorOutstandingAllowed: the method form gates against this regulator's MaxOutstanding() cap —
// the form the watched seam calls before Fire.
func TestRegulatorOutstandingAllowed(t *testing.T) {
	r := New(nil, nil) // cap = 8
	if !r.OutstandingAllowed(7) {
		t.Fatalf("Regulator.OutstandingAllowed(7) = false, want true (7 < 8)")
	}
	if r.OutstandingAllowed(8) {
		t.Fatalf("Regulator.OutstandingAllowed(8) = true, want false (8 not < 8)")
	}
}
