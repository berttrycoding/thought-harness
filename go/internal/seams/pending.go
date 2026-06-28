package seams

// pending.go is the hidden-seam pending-injection buffer (deferred task #27 / 04-seams.md §3.4): the
// stateful half the synchronous Relay lacks. A late subconscious injection (the "light-bulb after the
// calculation came back") is buffered anchored to the decision-node branch it pertains to + the tick it
// arrived; on Drain it is routed by where the conscious now is (04 §3.2): still on the anchor → inject;
// the anchor is a passed line → propose a retracement (the Controller fires mcp.Reenter); too old → drop
// as stale (the HPF/relevance-decay cutoff of §2.1). Deterministic over ticks — no wall clock, no RNG.

// Routing is how a drained pending injection should be handled (04 §3.2).
type Routing int

const (
	InjectAtHead       Routing = iota // still relevant to the active line — inject now
	ProposeRetracement                // relevant to a PASSED decision node — propose re-entry there
	DropStale                         // decayed past relevance — drop
)

// String renders the routing for traces.
func (r Routing) String() string {
	switch r {
	case InjectAtHead:
		return "inject-at-head"
	case ProposeRetracement:
		return "propose-retracement"
	default:
		return "drop-stale"
	}
}

type pendingInjection struct {
	text         string
	anchorBranch int
	createdTick  int
}

// PendingInjectionBuffer holds late injections, each anchored to a branch + creation tick, with
// tick-decay. NOT goroutine-safe (the engine is serial).
type PendingInjectionBuffer struct {
	items  []pendingInjection
	maxAge int // ticks an un-drained injection stays relevant (the relevance-decay cutoff)
}

// NewPendingInjectionBuffer builds a buffer whose injections go stale after maxAge ticks (default 8 —
// the W_max-scale window; <1 clamps to 8).
func NewPendingInjectionBuffer(maxAge int) *PendingInjectionBuffer {
	if maxAge < 1 {
		maxAge = 8
	}
	return &PendingInjectionBuffer{maxAge: maxAge}
}

// Add buffers a late injection anchored to anchorBranch, arriving at tick.
func (b *PendingInjectionBuffer) Add(text string, anchorBranch, tick int) {
	b.items = append(b.items, pendingInjection{text: text, anchorBranch: anchorBranch, createdTick: tick})
}

// Len reports how many injections are buffered.
func (b *PendingInjectionBuffer) Len() int { return len(b.items) }

// Routed is one drained injection paired with its routing decision.
type Routed struct {
	Text         string
	AnchorBranch int
	Route        Routing
}

// Drain classifies every buffered injection against the current tick + active branch and EMPTIES the
// buffer (each is handled exactly once). Stale-first: an aged injection drops even if its anchor is
// active. Deterministic, insertion-order preserved.
func (b *PendingInjectionBuffer) Drain(currentTick, activeBranch int) []Routed {
	out := make([]Routed, 0, len(b.items))
	for _, it := range b.items {
		r := Routed{Text: it.text, AnchorBranch: it.anchorBranch}
		switch {
		case currentTick-it.createdTick > b.maxAge:
			r.Route = DropStale
		case it.anchorBranch == activeBranch:
			r.Route = InjectAtHead
		default:
			r.Route = ProposeRetracement
		}
		out = append(out, r)
	}
	b.items = b.items[:0]
	return out
}
