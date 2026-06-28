package runner

import (
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// EventCollector is an in-memory bus sink: it appends every Event the engine emits, in
// emission order, so the full per-arm trace is available to the isolation predicates after
// the run (spec §5.1: emit the full event trace per arm so genuine mechanism use is
// checkable from the trace, not inferred).
//
// The engine bus fans out synchronously from a single emitter, so order is deterministic;
// the mutex guards only against a defensive concurrent read (none on the hot path) and
// makes the snapshot copy race-free.
type EventCollector struct {
	mu     sync.Mutex
	buffer []events.Event
}

// newCollector builds an empty EventCollector.
func newCollector() *EventCollector { return &EventCollector{} }

// add is the bus subscriber callback — it appends one event. Wired via
// eng.Bus().Subscribe(coll.add) BEFORE the run so the opening trace is captured.
func (c *EventCollector) add(ev events.Event) {
	c.mu.Lock()
	c.buffer = append(c.buffer, ev)
	c.mu.Unlock()
}

// events returns a copy of the captured trace, oldest-to-newest. A copy so a caller cannot
// mutate the collector's buffer.
func (c *EventCollector) events() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.buffer))
	copy(out, c.buffer)
	return out
}

// Len reports how many events have been captured (the smoke test asserts > 0).
func (c *EventCollector) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buffer)
}
