package events

// ring is a fixed-size ring buffer mirroring Python's collections.deque(maxlen=N):
// once full, every Push drops the oldest element. Iteration order is oldest-to-newest.
//
// It is the Bus replay log (maxlen 4000). Not safe for concurrent use on its own; the
// Bus serialises access under its mutex.
type ring struct {
	buf   []Event
	start int // index of the oldest element
	size  int // number of live elements (<= cap)
}

// newRing builds a ring with capacity maxlen. A non-positive maxlen yields an
// always-empty ring (Push is a no-op), matching deque(maxlen=0).
func newRing(maxlen int) *ring {
	if maxlen < 0 {
		maxlen = 0
	}
	return &ring{buf: make([]Event, maxlen)}
}

// push appends ev, evicting the oldest element if the buffer is full.
func (r *ring) push(ev Event) {
	if len(r.buf) == 0 {
		return
	}
	if r.size < len(r.buf) {
		r.buf[(r.start+r.size)%len(r.buf)] = ev
		r.size++
		return
	}
	// full: overwrite the oldest and advance start
	r.buf[r.start] = ev
	r.start = (r.start + 1) % len(r.buf)
}

// items returns a fresh slice of the live elements in oldest-to-newest order.
func (r *ring) items() []Event {
	out := make([]Event, r.size)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}

// len reports the number of live elements.
func (r *ring) len() int { return r.size }
