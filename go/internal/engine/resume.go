package engine

import (
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// resume.go is the deterministic power-cycle CURSOR (proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §3,
// red-team amendment 2): an ENUMERABLE registry of every advancing engine RNG stream,
// plus the snapshot/restore of those streams and the logical tick.
//
// Why a registry and not "persist e.rng": the engine advances ≥3 independent seeded
// streams (the main e.rng, the test-double wanderRNG, the tier-router routeRNG). A
// single-stream snapshot would silently drop the others and break a deterministic
// resume. Registering every stream in ONE place makes the set enumerable, so a newly
// added stream is captured the moment it registers instead of being forgotten.
//
// SCOPE: this restores the RNG cursor + tick — necessary for deterministic resume but
// NOT sufficient for byte-identical ENGINE continuation, which also needs the
// graph/forest + lifecycle state (Track 2). Track 1's guarantee is the cursor only.

// rngStream is one named, snapshottable engine RNG stream in the resume registry.
type rngStream struct {
	name string
	get  func() cpyrand.State
	set  func(cpyrand.State)
}

// registerRNG adds a generator to the resume registry under name. nil is ignored (an
// absent optional stream, e.g. the route RNG when the tier router is off). Streams that
// SHARE a generator (the soft policy + subconscious both hold e.rng) register once —
// the pointer is shared, so one snapshot/restore covers every consumer.
func (e *Engine) registerRNG(name string, r *cpyrand.Random) {
	if r == nil {
		return
	}
	e.rngStreams = append(e.rngStreams, rngStream{name: name, get: r.GetState, set: r.SetState})
}

// snapshotRNG captures every registered stream's state, keyed by name.
func (e *Engine) snapshotRNG() map[string]cpyrand.State {
	out := make(map[string]cpyrand.State, len(e.rngStreams))
	for _, s := range e.rngStreams {
		out[s.name] = s.get()
	}
	return out
}

// restoreRNG restores registered streams from a by-name snapshot. Tolerant by design: a
// stream missing from the snapshot is left untouched (it did not exist when the snapshot
// was taken), and an unknown name in the snapshot is ignored.
func (e *Engine) restoreRNG(snap map[string]cpyrand.State) {
	for _, s := range e.rngStreams {
		if st, ok := snap[s.name]; ok {
			s.set(st)
		}
	}
}

// snapshotResumeRecord builds the on-disk resume cursor from the live RNG registry plus
// the current tick. Each fixed [624]uint32 state vector is copied into a fresh slice so
// the record shares no storage with the live generators.
func (e *Engine) snapshotResumeRecord() persist.ResumeRecord {
	streams := make(map[string]persist.RNGStreamState, len(e.rngStreams))
	for name, st := range e.snapshotRNG() {
		words := make([]uint32, len(st.Words))
		copy(words, st.Words[:])
		streams[name] = persist.RNGStreamState{Words: words, Index: st.Index}
	}
	return persist.ResumeRecord{Streams: streams, Tick: e.bus.Tick, Substrate: e.backendLabel}
}

// applyResumeRecord restores the RNG streams + tick from a loaded cursor. A stream whose
// word-vector is not exactly the MT19937 width is skipped (corrupt ⇒ that stream
// cold-starts, never an out-of-bounds copy).
func (e *Engine) applyResumeRecord(r *persist.ResumeRecord) {
	if r == nil {
		return
	}
	snap := make(map[string]cpyrand.State, len(r.Streams))
	for name, ss := range r.Streams {
		var st cpyrand.State
		if len(ss.Words) != len(st.Words) {
			continue
		}
		copy(st.Words[:], ss.Words)
		st.Index = ss.Index
		snap[name] = st
	}
	e.restoreRNG(snap)
	if r.Tick > 0 {
		e.bus.Tick = r.Tick
	}
}

// saveResumeCursor persists the resume cursor alongside the learned-state flush. nil
// store / persistence-off ⇒ no-op (the cursor is meaningless offline). Saving is always
// safe — it writes a file, never mutates engine state — so it is NOT gated by the Resume
// knob; only RESTORING is (loadResume).
func (e *Engine) saveResumeCursor() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled {
		return
	}
	_ = e.cfg.Store.SaveResume(e.snapshotResumeRecord())
}

// loadResume restores the RNG cursor + tick on boot when the Resume knob is ON, so the
// seeded stream CONTINUES from a power-down instead of restarting at position 0. Default
// OFF ⇒ cold-boot, byte-identical to a fresh engine. Gated additionally by a non-nil
// store + persistence-on. Runs AFTER loadState (learned state first, then the cursor).
// The restored tick surfaces in the first tick event, so the resume is observable.
func (e *Engine) loadResume() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled || !e.features.Persist.Resume {
		return
	}
	r, err := e.cfg.Store.LoadResume()
	if err != nil || r == nil {
		return
	}
	e.applyResumeRecord(r)
}
