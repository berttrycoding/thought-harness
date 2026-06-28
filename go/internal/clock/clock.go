// Package clock is the INJECTED time seam for the WF-G time-awareness extension (09 §4): the engine
// is deliberately wall-clock-free (CLAUDE.md: ticks are logical time; determinism + the durability
// math forbid time.Now() in engine logic), so wall-clock awareness enters the same way randomness
// does — through an injected interface with a deterministic test double, exactly like the seeded
// cpyrand RNG. A nil Clock anywhere means TIME-BLIND: no time is ever read and behavior is
// byte-identical to the tick-only engine (the default).
package clock

import "time"

// Clock is the one time port. Production wires Wall; tests wire a Fake advanced deterministically.
type Clock interface {
	Now() time.Time
}

// Wall reads the real wall clock. Construct it ONLY at the edge (CLI/config wiring) — never inside
// engine logic — so the engine's time-blindness stays the default and greppable.
type Wall struct{}

// Now returns time.Now().
func (Wall) Now() time.Time { return time.Now() }

// Fake is the deterministic test double: time advances only when the test says so, so a deadline
// test is exactly reproducible (the FakeClock analogue of the seeded RNG).
type Fake struct {
	T time.Time
}

// NewFake starts a fake clock at a fixed, arbitrary epoch (determinism needs stability, not realism).
func NewFake() *Fake {
	return &Fake{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Now returns the fake's current instant.
func (f *Fake) Now() time.Time { return f.T }

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.T = f.T.Add(d) }
