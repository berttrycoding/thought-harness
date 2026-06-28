// Package host is the INJECTED host/runtime seam for the reach=self INTROSPECTION sensors (cognitive
// power-cycle, Track 3 — read_host = "the machine I live on / my footprint"): the engine is deliberately
// headless-pure and time-blind (CLAUDE.md: no I/O, no wall clock, no unseeded reads in engine logic;
// determinism + the durability math forbid runtime.* in engine logic), so the harness's OWN process
// footprint enters the same way time does — through an injected interface with a deterministic test
// double, exactly like internal/clock.Clock and the seeded cpyrand RNG. A nil Host anywhere means
// HOST-BLIND: no runtime stat is ever read and behavior is byte-identical to the footprint-blind engine
// (the default).
//
// PROCESS FOOTPRINT, NOT SYSTEM RAM. Sample reports the harness's OWN process footprint
// (runtime.MemStats Alloc/Sys + runtime.NumGoroutine), NOT system-wide memory. System RAM needs
// platform syscalls (cgo / sysctl / /proc) — non-portable and outside stdlib; the process footprint is
// the honest, portable, stdlib-only "my footprint on the machine I live on". It reads only runtime.*,
// which is cross-platform and dependency-free.
package host

import "runtime"

// Sample is a small, frozen snapshot of the harness's PROCESS footprint — the values the reach=self
// host sensor reads. It carries no pointers into live runtime structures (the values are copied at the
// read), so it is a frozen snapshot the orientation template can fold in safely.
type Sample struct {
	AllocMB    uint64 // currently-allocated heap, MiB (runtime.MemStats.Alloc rounded to MiB)
	SysMB      uint64 // total memory obtained from the OS, MiB (runtime.MemStats.Sys rounded to MiB)
	Goroutines int    // live goroutine count (runtime.NumGoroutine)
}

// Host is the one host/runtime port. Production wires Wall (real runtime stats); tests wire a Fake with
// fixed values. The interface is the injected seam — the engine never calls runtime.* directly.
type Host interface {
	Sample() Sample
}

// Wall reads the real process footprint from the Go runtime. Construct it ONLY at the edge (CLI/config
// wiring) — never inside engine logic — so the engine's footprint-blindness stays the default and
// greppable. runtime.ReadMemStats is a stop-the-world read; it is taken once per orientation pass (a
// boot-time read), never on a hot tick.
type Wall struct{}

// Sample reads runtime.MemStats + runtime.NumGoroutine and returns the process footprint. Alloc/Sys are
// reported in MiB (the raw byte counts are noisy + huge); NumGoroutine is exact.
func (Wall) Sample() Sample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Sample{
		AllocMB:    m.Alloc / (1024 * 1024),
		SysMB:      m.Sys / (1024 * 1024),
		Goroutines: runtime.NumGoroutine(),
	}
}

// Fake is the deterministic test double: it returns FIXED values, so a host-sensing test is exactly
// reproducible (the host analogue of clock.Fake and the seeded RNG). A real runtime read would vary
// run-to-run and break golden determinism — the Fake is the offline, byte-stable stand-in.
type Fake struct {
	S Sample
}

// NewFake builds a Fake at fixed, arbitrary footprint values (determinism needs stability, not realism).
func NewFake() *Fake {
	return &Fake{S: Sample{AllocMB: 7, SysMB: 21, Goroutines: 3}}
}

// Sample returns the fake's fixed footprint.
func (f *Fake) Sample() Sample { return f.S }
