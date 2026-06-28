package cpyrand

// state.go adds the get/set-state surface that makes a Random RESUMABLE — the
// foundation under deterministic power-cycle/resume (redesign proposal
// 2026-06-20-cognitive-power-cycle-and-grounded-sensing §3, red-team amendment 2).
//
// Re-seeding via New/Seed restarts the draw stream from position 0 — that is a cold
// restart, NOT a resume. To CONTINUE the identical stream across a power-down, the
// full MT19937 state (the 624-word vector + the read index) must be snapshotted and
// restored. This is the Go analogue of CPython's random.getstate()/setstate().

// State is a serializable snapshot of a Random's full MT19937 state: the 624-word
// state vector (Words) plus the read position (Index). Restoring it into any Random
// makes that generator continue the SAME draw stream from exactly where the snapshot
// was taken — byte-identical to a generator that never stopped.
//
// Index == 624 (== n) means "block exhausted, regenerate on the next draw" — the
// position immediately after seeding. Both Words and Index are value types, so a
// State shares no storage with the generator it came from and is safe to persist
// (JSON/gob) and replay later.
type State struct {
	Words [n]uint32 `json:"words"`
	Index int       `json:"index"`
}

// GetState returns an independent copy of the generator's current state — safe to
// persist and later hand to SetState to resume the identical stream. The state
// vector is copied by value (it is a fixed array, not a slice), so subsequent draws
// on the source generator never mutate a previously-returned State.
func (r *Random) GetState() State {
	return State{Words: r.mt.state, Index: r.mt.index}
}

// SetState restores a generator from a previously captured State, so its next draws
// continue the snapshotted stream exactly. A malformed Index (outside [0, n]) is
// clamped to n (force a block regeneration on the next draw) rather than read out of
// bounds — a corrupt snapshot degrades to a safe fresh block, never a panic. Callers
// that need to reject (not tolerate) a corrupt snapshot should validate Index in
// [0, n] before calling.
func (r *Random) SetState(s State) {
	r.mt.state = s.Words
	if s.Index < 0 || s.Index > n {
		r.mt.index = n
	} else {
		r.mt.index = s.Index
	}
}
