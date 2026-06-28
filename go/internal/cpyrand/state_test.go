package cpyrand

import "testing"

// TestStateRoundTripResumesIdenticalStream is the determinism foundation gate for
// the cognitive power-cycle/resume work: snapshotting a generator's state and
// restoring it into a DIFFERENT generator must reproduce the exact subsequent draw
// stream. If this fails, "deterministic resume" is a lie — fail before anything
// builds on it. The snapshot is taken mid-block (not on the post-seed boundary),
// the case a naive seed-only "resume" silently gets wrong.
func TestStateRoundTripResumesIdenticalStream(t *testing.T) {
	r := New(12345)
	for i := 0; i < 1000; i++ { // advance off the index==n boundary
		r.Float64()
	}
	snap := r.GetState()

	want := make([]float64, 500) // the continuation the resume must reproduce
	for i := range want {
		want[i] = r.Float64()
	}

	// Restore into a generator seeded differently, so ONLY SetState can make it match.
	other := New(999)
	other.SetState(snap)
	for i := range want {
		if got := other.Float64(); got != want[i] {
			t.Fatalf("draw %d after SetState diverged: got %v, want %v", i, got, want[i])
		}
	}
}

// TestStateRoundTripAtSeedBoundary covers the index==n (just-seeded) snapshot — the
// boundary value SetState must also restore exactly.
func TestStateRoundTripAtSeedBoundary(t *testing.T) {
	r := New(42)
	snap := r.GetState() // index == n here
	if snap.Index != n {
		t.Fatalf("post-seed Index = %d, want %d", snap.Index, n)
	}
	want := make([]uint64, 64)
	for i := range want {
		want[i] = r.GetRandBitsUint64(32)
	}
	other := New(1)
	other.SetState(snap)
	for i := range want {
		if got := other.GetRandBitsUint64(32); got != want[i] {
			t.Fatalf("draw %d after boundary SetState diverged: got %d, want %d", i, got, want[i])
		}
	}
}

// TestGetStateIsACopy confirms the snapshot does not alias the live generator: draws
// after GetState must not mutate a previously-taken State (it would, if Words were a
// slice into internal storage instead of a value array).
func TestGetStateIsACopy(t *testing.T) {
	r := New(7)
	r.Float64()
	snap := r.GetState()
	before := snap
	for i := 0; i < 100; i++ {
		r.Float64()
	}
	if snap != before {
		t.Fatalf("State mutated by later draws on the source generator")
	}
}

// TestSetStateClampsCorruptIndex confirms an out-of-range Index degrades to a safe
// regenerate rather than panicking on an out-of-bounds read.
func TestSetStateClampsCorruptIndex(t *testing.T) {
	r := New(3)
	bad := r.GetState()
	bad.Index = 99999
	r.SetState(bad)
	_ = r.Float64() // must not panic
	neg := r.GetState()
	neg.Index = -5
	r.SetState(neg)
	_ = r.Float64() // must not panic
}
