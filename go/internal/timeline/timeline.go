// Package timeline holds the Conscious layer's episodic time-event log — the ordered trajectory of
// attention, time alongside the structural thought graph (design: docs/cognition/02-conscious.md
// §2a, §2b).
//
// Two correlated representations carry the Conscious substrate. The thought graph (internal/graph) is
// STRUCTURAL — nodes, branches, edges, one EXPANDED — the "map". This timeline is TEMPORAL — the
// ordered itinerary of attention: thought-created, focus-shifted / re-entered, branched, acted. The
// graph cannot express "attention was at node 7, then traced back to node 3"; that retracement is a
// temporal fact the topology can't hold (02 §2a). So the conscious carries both, joined by the tick.
//
// Three load-bearing properties, each enforced and tested:
//
//   - APPEND-ONLY. Thoughts are time-dependent and never overwritten — *graph forks, timeline appends.*
//     Append never mutates an existing record; re-thinking the same node adds a second event (a
//     ReEntered) after the first. The log is the honest record of the order things were thought and
//     re-thought.
//   - TICK-CORRELATED. Every event carries a TICK so the log aligns with external action-event ticks
//     (the watched seam stamps readyTick — internal/seams/watched.go; 04 §4). Ticks are the join between
//     subjective thought-time and objective action-time, letting the conscious ask "did reality confirm
//     the belief I held at the moment I decided?". Ticks are passed in, never read from a wall clock
//     (determinism, per the repo's seeded-RNG discipline).
//   - DETERMINISTIC + ORDER-PRESERVING. All queries return events in insertion order; the tick is the
//     caller's deterministic clock, not the package's.
//
// This is NOT the subconscious Episodic *memory* registry (01): that is retained past-instance history;
// this is attention's live trajectory, a substrate the Controller reasons over now. Same word, different
// objects (02 §2a). It is also distinct from the observability event bus (internal/events): the bus is
// the 74-kind TUI/tracer firehose; this promotes a small cognitive subset to a first-class substrate the
// conscious reasons over — the watched seam's BranchID+Claim anchor (04 §3.1) is the primitive it
// generalizes.
//
// The package is a Tier-1 leaf: it imports nothing from the rest of the tree (anchors are plain ints,
// not graph/types references), so the engine and seams can feed it without an import cycle.
package timeline

// Kind enumerates the cognitive subset of attention events the timeline records. It is the temporal
// vocabulary of 02 §2a's "ordered trajectory of attention" — deliberately small (a promoted subset of
// the 74-kind observability bus), not the full event vocabulary.
type Kind string

const (
	// ThoughtCreated — a new thought was voiced onto a branch (a node appeared in the graph).
	ThoughtCreated Kind = "thought-created"
	// FocusShifted — the single EXPANDED "CPU" moved to another branch (graph FOCUS; 02 §2).
	FocusShifted Kind = "focus-shifted"
	// ReEntered — a past decision node was re-opened with late evidence (the retracement / Reenter move,
	// 02 §2b). Distinct from FocusShifted so the timeline records retracement as its own event.
	ReEntered Kind = "re-entered"
	// Branched — the active line forked a new sibling (graph BRANCH; 02 §2).
	Branched Kind = "branched"
	// Acted — an intention crossed the watched seam (a decision node whose belief reality will confirm
	// or refute; the tick is the join key to the returned observation's readyTick).
	Acted Kind = "acted"
)

// Event is one entry in the episodic trajectory: WHAT happened (Kind), WHEN (Tick, the deterministic
// clock the caller passes in), and WHERE it was anchored (BranchID always; ThoughtID when the event
// pins to a specific node). The anchor generalizes the watched seam's BranchID+Claim primitive (04
// §3.1) so a returned observation can be joined back to the belief held when the decision was made.
type Event struct {
	// Kind is the temporal event class (thought-created / focus-shifted / re-entered / branched / acted).
	Kind Kind
	// Tick is the deterministic clock stamp at which the event happened — the join key to action-event
	// ticks (the watched seam's readyTick). Passed in by the engine; never read from a wall clock.
	Tick int
	// BranchID anchors the event to a line of the forest (the always-present anchor — the retracement
	// query ByAnchor keys off it). Branches are addressable ints (graph.ThoughtGraph.Branches), so a
	// plain int keeps this package a dependency-free leaf.
	BranchID int
	// ThoughtID optionally pins the event to a specific addressable node (nil == "no node", e.g. a
	// focus shift that names a branch but not a single thought). *int mirrors the codebase convention
	// for an optional id (types.Thought.BranchID, seams.PolledObservation.BranchID — nil == None).
	ThoughtID *int
}

// Timeline is the append-only episodic log: the ordered trajectory of attention. Append never
// overwrites; the queries read back in insertion order.
type Timeline struct {
	events []Event
}

// New constructs an empty Timeline ready to Append.
func New() *Timeline {
	return &Timeline{events: []Event{}}
}

// Append records one event at the END of the trajectory. APPEND-ONLY: it never mutates or replaces an
// existing record — re-thinking a node (a ReEntered at the same anchor) adds a second event after the
// first, preserving the honest order in which things were thought and re-thought (02 §2a).
func (t *Timeline) Append(e Event) {
	t.events = append(t.events, e)
}

// Len reports how many events have been recorded.
func (t *Timeline) Len() int { return len(t.events) }

// All returns every event, in insertion order, as a COPY — a caller mutating the result cannot corrupt
// the timeline's append-only record. (A leaked backing array would silently break the never-overwrite
// invariant.)
func (t *Timeline) All() []Event {
	out := make([]Event, len(t.events))
	copy(out, t.events)
	return out
}

// Since returns every event with Tick >= tick, in insertion order — the temporal-window query the
// Controller uses to look at recent attention. The result is a fresh slice (never nil).
func (t *Timeline) Since(tick int) []Event {
	out := make([]Event, 0, len(t.events))
	for _, e := range t.events {
		if e.Tick >= tick {
			out = append(out, e)
		}
	}
	return out
}

// ByAnchor returns every event anchored to branchID, in insertion order — "what happened on this line,
// when?", the retracement query (02 §2b). Combined with the Tick stamp it expresses the action-time
// join (the belief held on a branch at-or-before an observation's readyTick). The result is a fresh
// slice (never nil).
func (t *Timeline) ByAnchor(branchID int) []Event {
	out := make([]Event, 0, len(t.events))
	for _, e := range t.events {
		if e.BranchID == branchID {
			out = append(out, e)
		}
	}
	return out
}
