package host

import "testing"

// TestFakeReturnsFixedValues is the determinism oracle for the host seam's test double: a Fake returns
// its EXACT fixed footprint on every read, so a host-sensing test is byte-stable (the host analogue of
// clock.Fake). Two reads of the same Fake must be identical.
func TestFakeReturnsFixedValues(t *testing.T) {
	f := NewFake()
	want := Sample{AllocMB: 7, SysMB: 21, Goroutines: 3}
	if got := f.Sample(); got != want {
		t.Fatalf("NewFake().Sample() = %+v, want %+v", got, want)
	}
	// A second read is identical (no drift) — the determinism the engine relies on.
	if a, b := f.Sample(), f.Sample(); a != b {
		t.Fatalf("Fake.Sample() drifted across reads: %+v vs %+v", a, b)
	}
}

// TestFakeCustomValues: a Fake constructed with explicit values returns exactly those (the test wires a
// known footprint and asserts the read carries it verbatim — the correctness oracle the engine tests use).
func TestFakeCustomValues(t *testing.T) {
	want := Sample{AllocMB: 99, SysMB: 256, Goroutines: 42}
	f := &Fake{S: want}
	if got := f.Sample(); got != want {
		t.Fatalf("Fake{S:%+v}.Sample() = %+v, want %+v", want, got, want)
	}
}

// TestWallReadsRealRuntime: the Wall impl reads real runtime stats — it must return a PLAUSIBLE process
// footprint (a live process always has >=1 goroutine and obtained some memory from the OS). This is a
// smoke test that the seam's production path actually reads runtime.* (not that the exact value matches —
// that is non-deterministic by design, which is precisely why the engine wires the Fake offline).
func TestWallReadsRealRuntime(t *testing.T) {
	s := Wall{}.Sample()
	if s.Goroutines < 1 {
		t.Fatalf("Wall.Sample().Goroutines = %d, want >= 1 (the test process is live)", s.Goroutines)
	}
	// SysMB is memory obtained from the OS; a running Go process has obtained at least some.
	if s.SysMB == 0 {
		t.Fatalf("Wall.Sample().SysMB = 0, want > 0 (a live process holds OS memory)")
	}
}

// TestHostInterface: both Wall and Fake satisfy the Host interface (the seam contract — the engine holds a
// Host, never a concrete). A compile-time + runtime check that the swappable port is honoured.
func TestHostInterface(t *testing.T) {
	var _ Host = Wall{}
	var _ Host = NewFake()
	hosts := []Host{Wall{}, NewFake()}
	for _, h := range hosts {
		_ = h.Sample() // each is callable through the interface
	}
}
