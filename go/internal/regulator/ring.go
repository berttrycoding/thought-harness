package regulator

import "github.com/berttrycoding/thought-harness/internal/events"

// ring is a fixed-size ring buffer mirroring Python's collections.deque(maxlen=N): once full,
// every push drops the oldest snapshot. Iteration order is oldest-to-newest. It holds the
// regulator's EMA snapshots (maxlen 240) — the durability sparkline source. Single-threaded;
// the engine drives Update serially.
type ring struct {
	buf   []events.D
	start int // index of the oldest element
	size  int // number of live elements (<= cap)
}

// newRing builds a ring with capacity maxlen. A non-positive maxlen yields an always-empty
// ring (push is a no-op), matching deque(maxlen=0).
func newRing(maxlen int) *ring {
	if maxlen < 0 {
		maxlen = 0
	}
	return &ring{buf: make([]events.D, maxlen)}
}

// push appends snap, evicting the oldest snapshot if the buffer is full.
func (r *ring) push(snap events.D) {
	if len(r.buf) == 0 {
		return
	}
	if r.size < len(r.buf) {
		r.buf[(r.start+r.size)%len(r.buf)] = snap
		r.size++
		return
	}
	r.buf[r.start] = snap
	r.start = (r.start + 1) % len(r.buf)
}

// items returns a fresh slice of the live snapshots in oldest-to-newest order.
func (r *ring) items() []events.D {
	out := make([]events.D, r.size)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}

// len reports the number of live snapshots.
func (r *ring) len() int { return r.size }
