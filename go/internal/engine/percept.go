// percept.go is the deterministic PERCEPT-LOG seam + the read_clock sensor (cognitive power-cycle,
// Track 1.5; proposal docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md
// §3.2 + §11 Track 1.5; red-team amendments 3 & 4).
//
// THE PROBLEM IT SOLVES. Real-world sensing (the clock now; news/host later) is NON-DETERMINISTIC and
// would break the seeded-RNG / golden determinism contract — UNLESS each sensed value is recorded once
// at the seam and replayed thereafter. read_clock is the FIRST such sensor; it ships WITH the percept-
// log so it never violates the engine's time-blind determinism (the same discipline clock.go states:
// a nil clock is time-blind; a wired clock enters time deterministically via clock.Fake in tests).
//
// RECORD vs REPLAY (one record drives both). A LIVE run RECORDS fresh reads (e.clk.Now()) into the
// percept-log, persisted for later. A GOLDEN/resumed run REPLAYS the logged value for a (tick, kind) so
// it is deterministic even though the live clock differs.
//
// THE DIVERGENCE CONTRACT (red-team amendment 3). A loaded log carries a Version + a Substrate stamp;
// on boot the engine REFUSES to replay a log whose Version or Substrate does not match the running
// engine's (it falls back to cold-sense / time-blind) — NEVER a best-effort partial replay. A version
// bump or a substrate switch must not silently mis-replay an old log.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. A sense fires only when the opt-in sense.clock knob is ON AND a Clock
// is wired AND a non-nil Store backs the log. Default OFF / nil clock ⇒ no read, no log entry, no
// event — the tick-only, time-blind engine, unchanged.
//
// HEADLESS-PURE. No time.Now() (only the injected e.clk); no I/O (only the injected Store). The engine
// owns the divergence CHECK; the store is I/O-only (it loads the raw record), mirroring how the engine
// — not the store — owns the resume RNG-width check.
package engine

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// perceptClockKind names the clock sensor in the percept-log (the entry's Kind).
const perceptClockKind = "clock"

// perceptClockFormat is the deterministic, stable string encoding for a recorded clock instant — a
// fixed reference layout so a recorded value round-trips byte-identically through the log and a replay
// reproduces it exactly. RFC3339Nano is sortable, lossless, and free of locale/wall-clock surprises.
const perceptClockFormat = "2006-01-02T15:04:05.999999999Z07:00"

// perceptKey is the by-(tick, kind) lookup key for the replay map. Stable + deterministic.
func perceptKey(tick int, kind string) string { return kind + "@" + itoa(tick) }

// senseEnabled reports whether grounded clock-sensing may fire: the opt-in sense.clock knob is ON AND a
// Clock is wired. nil features / nil clock ⇒ false (the time-blind default), so the bare path never
// reaches the sensor and stays byte-identical.
func (e *Engine) senseEnabled() bool {
	return e.features != nil && e.features.Sense.Clock && e.clk != nil
}

// perceptLogActive reports whether ANY percept-RECORDING sensor is live — the clock (senseEnabled) OR
// the outward fetch_web (senseWebEnabled). The percept-log is the shared record/replay store for every
// boundary sense, so its LOAD (replay) + SAVE gates must open when EITHER sensor is enabled, not just
// the clock. With both senses off / no seam wired ⇒ false ⇒ no load, no save ⇒ byte-identical to the
// pre-sensing engine (and savePerceptLog additionally requires a non-empty log, so an enabled-but-never-
// fired sensor still writes nothing).
func (e *Engine) perceptLogActive() bool {
	return e.senseEnabled() || e.senseWebEnabled()
}

// loadPerceptLog restores the replayable percept-log on boot, enforcing the DIVERGENCE CONTRACT. It
// runs only when sensing is enabled AND a Store + persistence are present; a log whose Version or
// Substrate does not match the running engine's is REFUSED (replay stays off ⇒ the engine cold-senses).
// A missing/corrupt log ⇒ no replay (a fresh record). Mirrors loadResume's gating + AFTER-loadState
// ordering; saving is unconditional (savePerceptLog), only RESTORING is gated.
func (e *Engine) loadPerceptLog() {
	if !e.perceptLogActive() || e.cfg.Store == nil || !e.features.Persist.Enabled {
		return
	}
	r, err := e.cfg.Store.LoadPerceptLog()
	if err != nil || r == nil {
		return
	}
	// The divergence contract (red-team amendment 3): a version OR substrate mismatch REFUSES replay —
	// never a best-effort partial apply. The refused log is left untouched on disk; this run cold-senses
	// and OVERWRITES it with a fresh, matching record on the next flush.
	if r.Version != persist.PerceptLogVersion || r.Substrate != e.backendLabel {
		e.bus.Emit(events.PerceptionClock,
			"percept-log REFUSED (divergence): version/substrate mismatch -> cold-sense",
			events.D{
				"reason":         "divergence",
				"log_version":    r.Version,
				"want_version":   persist.PerceptLogVersion,
				"log_substrate":  r.Substrate,
				"want_substrate": e.backendLabel,
			})
		return
	}
	e.perceptReplay = make(map[string]string, len(r.Entries))
	for _, ent := range r.Entries {
		e.perceptReplay[perceptKey(ent.Tick, ent.Kind)] = ent.Value
	}
	e.perceptReplayOK = true
}

// savePerceptLog persists this run's recorded percepts alongside the learned-state flush (called from
// curateState beside saveResumeCursor). nil store / persistence-off / sensing-off / nothing recorded ⇒
// no-op (the log is meaningless then). Saving is always safe — it writes a file, never mutates engine
// state — so it is NOT gated by replay; only RESTORING is (loadPerceptLog). The record is Version +
// Substrate stamped so the next boot's divergence check has its two keys.
func (e *Engine) savePerceptLog() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled || !e.perceptLogActive() || len(e.perceptLog) == 0 {
		return
	}
	entries := make([]persist.PerceptEntry, len(e.perceptLog))
	copy(entries, e.perceptLog)
	_ = e.cfg.Store.SavePerceptLog(persist.PerceptLogRecord{
		Version:   persist.PerceptLogVersion,
		Substrate: e.backendLabel,
		Entries:   entries,
	})
}

// senseClock is the read_clock sensor. When sensing is disabled (knob off / nil clock) it is a NO-OP
// (returns "", false) — no read, no log entry, no event, byte-identical. When enabled:
//   - REPLAY mode (a version/substrate-matching log holds this (tick, "clock")): return the LOGGED value
//     (deterministic even if the live clock differs) — the golden/resume path.
//   - RECORD mode: read the live e.clk.Now(), encode it, APPEND it to the percept-log, and return it —
//     the live path that captures reality once for later replay.
//
// Either way one perception.clock event is emitted carrying {value, mode, tick}, so the sense is visible
// on the bus. Deterministic offline because e.clk is clock.Fake (a fixed instant), exactly as the
// seeded RNG gives deterministic randomness.
func (e *Engine) senseClock(tick int) (string, bool) {
	if !e.senseEnabled() {
		return "", false
	}
	key := perceptKey(tick, perceptClockKind)
	if e.perceptReplayOK {
		if v, ok := e.perceptReplay[key]; ok {
			e.emitClockSense(v, "replay", tick)
			return v, true
		}
	}
	// RECORD: read the live clock once and log it (the only e.clk read in the sensor path).
	v := e.clk.Now().UTC().Format(perceptClockFormat)
	e.perceptLog = append(e.perceptLog, persist.PerceptEntry{Tick: tick, Kind: perceptClockKind, Value: v})
	e.emitClockSense(v, "record", tick)
	return v, true
}

// emitClockSense emits the perception.clock event (the sense's witness on the bus).
func (e *Engine) emitClockSense(value, mode string, tick int) {
	e.bus.Emit(events.PerceptionClock, "read_clock ["+mode+"]: "+value,
		events.D{"value": value, "mode": mode, "tick": tick})
}
