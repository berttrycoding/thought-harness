package tierb

import (
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// eventSink is the Tier-B trace collector: it appends every Event the engine emits across the
// whole multi-turn scenario, in emission order, and lets the driver snapshot the buffer length
// at each turn boundary so the trace can be split per turn (since(before)) without losing the
// flattened union (all()). It mirrors runner.EventCollector but adds the boundary-snapshot
// primitives the multi-turn split needs (the runner's collector is single-shot and unexported).
type eventSink struct {
	mu     sync.Mutex
	buffer []events.Event
}

// newSink builds an empty eventSink.
func newSink() *eventSink { return &eventSink{} }

// add is the bus subscriber callback — appends one event. Wired via eng.Bus().Subscribe(sink.add)
// BEFORE the first turn so the opening trace is captured.
func (s *eventSink) add(ev events.Event) {
	s.mu.Lock()
	s.buffer = append(s.buffer, ev)
	s.mu.Unlock()
}

// len reports how many events have been captured so far (the per-turn boundary marker).
func (s *eventSink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buffer)
}

// since returns a copy of the events captured AFTER index `from` (the events of one turn, given
// the buffer length snapshotted before that turn started). A copy so the caller cannot mutate
// the sink's buffer.
func (s *eventSink) since(from int) []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if from < 0 {
		from = 0
	}
	if from > len(s.buffer) {
		from = len(s.buffer)
	}
	out := make([]events.Event, len(s.buffer)-from)
	copy(out, s.buffer[from:])
	return out
}

// all returns a copy of the full captured trace, oldest-to-newest (the end-state oracle substrate).
func (s *eventSink) all() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]events.Event, len(s.buffer))
	copy(out, s.buffer)
	return out
}
