package engine

import "github.com/berttrycoding/thought-harness/internal/timeline"

// timeline_feed.go wires the episodic timeline (slice i, 02-conscious.md §2a) into the engine: the engine
// holds one *timeline.Timeline and appends an attention-move Event at each move site, stamped with the
// current tick. It is OBSERVABILITY-ONLY today (nothing reads it yet), so the feed changes no behaviour;
// the Controller/retracement consume it once those slices are wired. ThoughtCreated + Acted are fed here
// (the core trajectory + the action-time anchor); focus/branch/re-enter events follow with their wirings.

// Timeline returns the episodic attention trajectory — read to reason over recent attention and to run
// the action-time correlation ("did reality confirm the belief I held when I decided?"). Never nil after
// NewEngine.
func (e *Engine) Timeline() *timeline.Timeline { return e.timeline }

// tlThought records a thought-created attention move (slice i). nil timeline ⇒ no-op.
func (e *Engine) tlThought(tick, branch, thoughtID int) {
	if e.timeline == nil {
		return
	}
	id := thoughtID
	e.timeline.Append(timeline.Event{Kind: timeline.ThoughtCreated, Tick: tick, BranchID: branch, ThoughtID: &id})
}

// tlActed records an act crossing the watched seam — the action-time anchor a returned observation joins
// back to by tick (slice i). nil timeline ⇒ no-op.
func (e *Engine) tlActed(tick, branch int) {
	if e.timeline == nil {
		return
	}
	e.timeline.Append(timeline.Event{Kind: timeline.Acted, Tick: tick, BranchID: branch})
}
