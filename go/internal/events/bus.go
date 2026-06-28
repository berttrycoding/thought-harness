package events

import (
	"strings"
	"sync"
)

// Bus is a synchronous pub/sub bus with a bounded replay ring buffer (a faithful port of
// the Python events.Bus).
//
// Synchronous (not channel-based) fan-out is a HARD requirement: the Python stream order
// is deterministic (seeded Random, single-threaded), and channel scheduling would make
// event order nondeterministic and break golden comparison. The mutex guards the
// subscriber list and the ring only; subscriber callbacks run while the lock is held in
// the Python single-thread sense — here we snapshot the subscribers under the lock and
// dispatch outside it so a subscriber may Subscribe/unsubscribe mid-dispatch without
// deadlock, while preserving deterministic single-emitter ordering.
type Bus struct {
	mu     sync.Mutex
	subs   []subscriber
	nextID uint64
	log    *ring

	// Tick is the loop counter the engine sets once per iteration. Emit reads it but
	// never bumps it (matching Python: the engine owns the tick clock, not the bus).
	Tick int
}

// subscriber pairs a callback with a stable identity so unsubscribe removes the exact
// registration (Go forbids == on funcs, so we cannot identify a callback by value the way
// Python identifies it by object).
type subscriber struct {
	id uint64
	fn func(Event)
}

// New builds a Bus whose replay ring holds the most recent history events.
func New(history int) *Bus {
	return &Bus{log: newRing(history)}
}

// NewDefault builds a Bus with the default replay depth (4000), matching Python's
// Bus(history=4000) default.
func NewDefault() *Bus { return New(4000) }

// Subscribe registers fn and returns an idempotent unsubscribe. The unsubscribe removes
// exactly this registration; calling it more than once is a no-op (matching Python's
// `if fn in self._subs: self._subs.remove(fn)`).
func (b *Bus) Subscribe(fn func(Event)) (unsubscribe func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs = append(b.subs, subscriber{id: id, fn: fn})
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			for i := range b.subs {
				if b.subs[i].id == id {
					b.subs = append(b.subs[:i], b.subs[i+1:]...)
					return
				}
			}
		})
	}
}

// Emit records an event and fans it out synchronously to every current subscriber. The
// layer is DERIVED here from the kind (the substring before the first "."), the ring
// captures it for replay, and the event is returned for the (rare) caller that needs it.
// Emit does NOT advance Tick.
func (b *Bus) Emit(kind, summary string, data map[string]any) Event {
	layer := kind
	if i := strings.IndexByte(kind, '.'); i >= 0 {
		layer = kind[:i]
	}
	if data == nil {
		// Python's dict(data) always yields a (possibly empty) dict, never None; keep the
		// wire shape `"data": {}` rather than `"data": null`.
		data = map[string]any{}
	}
	ev := Event{Tick: b.Tick, Kind: kind, Layer: layer, Summary: summary, Data: data}

	b.mu.Lock()
	b.log.push(ev)
	subs := make([]subscriber, len(b.subs)) // snapshot so a sub may (un)subscribe mid-dispatch
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, s := range subs {
		s.fn(ev)
	}
	return ev
}

// Recent returns up to the last n events from the replay ring, optionally filtered to a
// single layer. A nil layer means "all layers". Order is oldest-to-newest, matching
// Python's list slice items[-n:].
func (b *Bus) Recent(n int, layer *string) []Event {
	b.mu.Lock()
	all := b.log.items()
	b.mu.Unlock()

	var items []Event
	if layer == nil {
		items = all
	} else {
		items = make([]Event, 0, len(all))
		for _, e := range all {
			if e.Layer == *layer {
				items = append(items, e)
			}
		}
	}
	if n < 0 {
		n = 0
	}
	if len(items) > n {
		items = items[len(items)-n:]
	}
	return items
}
