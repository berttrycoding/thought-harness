// introspect.go is the reach=self INTROSPECTION SENSORS for the cognitive power-cycle (Track 3; proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §5 + §11 Track 3). It
// adds the TWO missing reach=self reads the orientation pass folds in — beyond senseSelf's in-memory
// engine fields (goal/open-lines/tick) and senseClock's "what time is it now":
//
//   - read_host (host.go seam) — "the machine I live on / my footprint": the harness's OWN process
//     footprint (AllocMB / SysMB / Goroutines), read across the INJECTED host.Host seam EXACTLY like the
//     clock — host.Wall at the edge, host.Fake in tests; a nil host is HOST-BLIND (no runtime read). The
//     engine never calls runtime.* directly (headless-pure); the only runtime read lives in host.Wall,
//     constructed at the edge.
//   - read_event_log (the bounded tap below) — "my own logs/traces": the engine's OWN emitted events,
//     made READABLE BY ITSELF. Events are outbound-only otherwise; this is the missing INBOUND
//     introspection path — a passive, fixed-cap in-memory ring that subscribes to the engine's bus.
//
// DETERMINISM. The Fake host returns FIXED values (offline, byte-stable); the event ring is in-memory,
// bounded, and a PASSIVE tap (it only reads/records what the bus already fans out — it never re-orders,
// duplicates, or drops an emit a real subscriber sees). The seeded RNG is untouched and no wall clock is
// read here.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. senseHost reads only when sense.host is ON AND a Host is wired (else
// (zero,false)). The event tap is wired ONLY when sense.event_log is ON — so the default engine adds NO
// bus subscriber, NO ring, and NO host read. With every knob off the live loop is byte-identical to the
// pre-introspection engine. Mirrors the senseClock / orientOnce gating shape.
//
// HEADLESS-PURE. No I/O, no wall clock, no unseeded randomness; runtime.* only behind host.Wall (the
// injected seam, constructed at the edge).
package engine

import (
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
	hostpkg "github.com/berttrycoding/thought-harness/internal/host"
)

// introspectRingCap is the fixed window of the engine's own-event tap — the last N event summaries the
// reach=self read_event_log sensor can see (a small introspection window, NOT the bus's full 4000-deep
// replay log). 64 is enough for "what have I been doing lately" without retaining the whole run.
const introspectRingCap = 64

// SetHost wires the host/runtime seam: h is the host (hostpkg.Wall at the edge, hostpkg.Fake in tests;
// nil reverts to host-blind). Call before Process/Run; the footprint is sampled on the orientation pass
// (a boot-time read), never on a hot tick. Mirrors SetClock.
func (e *Engine) SetHost(h hostpkg.Host) { e.hst = h }

// senseHostEnabled reports whether the read_host sensor may fire: the opt-in sense.host knob is ON AND a
// Host is wired. nil features / nil host ⇒ false (the footprint-blind default), so the bare path never
// reaches the sensor and stays byte-identical. Mirrors senseEnabled (the clock gate).
func (e *Engine) senseHostEnabled() bool {
	return e.features != nil && e.features.Sense.Host && e.hst != nil
}

// senseHost is the read_host reach=self sensor: a read of the harness's OWN process footprint across the
// injected Host seam. When disabled (knob off / nil host) it is a NO-OP returning (zero,false) — no read,
// byte-identical. When enabled it returns the seam's Sample (the Fake's fixed values offline; the live
// runtime stats at the edge). The only host read in the sensor path. Mirrors senseClock's shape.
func (e *Engine) senseHost() (hostpkg.Sample, bool) {
	if !e.senseHostEnabled() {
		return hostpkg.Sample{}, false
	}
	return e.hst.Sample(), true
}

// senseEventLogEnabled reports whether the read_event_log sensor may fire: the opt-in sense.event_log
// knob is ON AND the tap ring is wired. nil features / nil ring ⇒ false, so the default path never reads
// the ring and stays byte-identical.
func (e *Engine) senseEventLogEnabled() bool {
	return e.features != nil && e.features.Sense.EventLog && e.eventRing != nil
}

// wireEventTap installs the passive event-log tap — a bounded in-memory ring subscribed to the engine's
// OWN bus — IFF the opt-in sense.event_log knob is on. Called once from NewEngine (after the bus + features
// resolve). The default path (knob off) wires NOTHING: no ring, no subscriber, no behavior change — the
// live loop stays byte-identical. The tap is provably side-effect-free: its subscriber only APPENDS a
// summary to its own ring (it never emits, mutates engine state, or touches the dispatched event), so it
// cannot re-order/duplicate/drop an emit any other subscriber sees (the bus snapshots its subscriber list
// per Emit and fans out independently).
func (e *Engine) wireEventTap() {
	if e.features == nil || !e.features.Sense.EventLog || e.bus == nil {
		return
	}
	e.eventRing = newIntrospectRing(introspectRingCap)
	ring := e.eventRing
	e.eventTapUnsub = e.bus.Subscribe(func(ev events.Event) {
		ring.push(introspectSummary(ev))
	})
}

// senseEventLog is the read_event_log reach=self sensor: it returns the last n event summaries from the
// engine's own-event ring, oldest-to-newest. When disabled (knob off / nil ring) it returns nil — no
// read, byte-identical. Deterministic in a seeded run (the events are deterministic, so the ring's
// contents are). n<=0 ⇒ nil; n larger than the live count returns all live summaries.
func (e *Engine) senseEventLog(n int) []string {
	if !e.senseEventLogEnabled() || n <= 0 {
		return nil
	}
	return e.eventRing.last(n)
}

// introspectSummary renders one event into the compact "kind: summary" string the reach=self read keeps
// (the full event still lives on the bus replay log; the ring is the engine's own short introspection
// view). Stable + deterministic — no clock, no RNG.
func introspectSummary(ev events.Event) string {
	if ev.Summary == "" {
		return ev.Kind
	}
	return ev.Kind + ": " + ev.Summary
}

// recentEventCount reports how many events the own-event ring currently holds (the "recent: <n> events"
// marker the orientation thought folds in). 0 when sensing is off / the ring is empty. Bounded by the
// ring cap, so it never exceeds introspectRingCap.
func (e *Engine) recentEventCount() int {
	if !e.senseEventLogEnabled() {
		return 0
	}
	return e.eventRing.len()
}

// introspectRing is the bounded, in-memory own-event tap: a fixed-cap ring of event summaries the engine
// taps off its OWN bus (the missing inbound introspection path). It mirrors the events package's private
// ring (deque(maxlen=N)) but holds the compact summary strings the reach=self read returns, and it
// carries its own tiny mutex so the bus's mid-dispatch fan-out (which runs subscribers OUTSIDE the bus
// lock) appends race-free. Not exported — it is an engine-internal introspection buffer.
type introspectRing struct {
	mu    sync.Mutex
	buf   []string
	start int
	size  int
}

// newIntrospectRing builds a ring of capacity cap. A non-positive cap yields an always-empty ring (push
// is a no-op), matching deque(maxlen=0).
func newIntrospectRing(capacity int) *introspectRing {
	if capacity < 0 {
		capacity = 0
	}
	return &introspectRing{buf: make([]string, capacity)}
}

// push appends s, evicting the oldest summary if the buffer is full. O(1), allocation-free per push (the
// backing slice is fixed at construction) — the per-emit cost is one slice write, so the tap does not
// allocate per-emit in a way a test could detect.
func (r *introspectRing) push(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return
	}
	if r.size < len(r.buf) {
		r.buf[(r.start+r.size)%len(r.buf)] = s
		r.size++
		return
	}
	r.buf[r.start] = s
	r.start = (r.start + 1) % len(r.buf)
}

// last returns up to the last n summaries in oldest-to-newest order (a fresh slice — the caller may keep
// it). n<=0 ⇒ nil; n larger than the live count returns all live summaries.
func (r *introspectRing) last(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || r.size == 0 {
		return nil
	}
	if n > r.size {
		n = r.size
	}
	out := make([]string, n)
	// the live elements are [start, start+size); take the last n of them.
	first := r.size - n
	for i := 0; i < n; i++ {
		out[i] = r.buf[(r.start+first+i)%len(r.buf)]
	}
	return out
}

// len reports the number of live summaries (<= cap).
func (r *introspectRing) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}
