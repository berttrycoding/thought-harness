package engine

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/seams"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// retracement.go wires the late-injection re-entry (slice c, 02-conscious.md §2b / 04-seams.md §3.3): the
// hidden seam PROPOSES (buffers an injection anchored to a decision node + tick), and the Controller FIRES
// the move when the buffer is drained. The one-way mirror holds — the conscious experiences "a new thought
// about an earlier decision", not "the seam moved me".

// BufferLateInjection buffers a late subconscious injection (the "light-bulb after the calculation came
// back") anchored to the decision-node branch it pertains to + the tick it arrived. It is routed on the
// next drain (drainRetracements): still on the anchor → inject at head; the anchor is a PASSED line →
// the Controller fires mcp.Reenter; too old → drop as stale. The band-pass (#19) is the live producer;
// this entry point is also how a caller/test injects one.
func (e *Engine) BufferLateInjection(text string, anchorBranch, tick int) {
	if e.pendingInj == nil {
		e.pendingInj = seams.NewPendingInjectionBuffer(8)
	}
	e.pendingInj.Add(text, anchorBranch, tick)
}

// retracementEnabled reports whether the late-injection buffer should be drained this tick.
func (e *Engine) retracementEnabled() bool {
	return e.features != nil && e.features.Conscious.Activity.Retracement && e.pendingInj != nil && e.pendingInj.Len() > 0
}

// drainRetracements drains the pending-injection buffer against the current tick + active branch and acts
// on each routing (04 §3.2): InjectAtHead appends the injection to the active line; ProposeRetracement has
// the Controller fire mcp.Reenter on the PASSED decision node (fork + focus + retracement event, nothing
// overwritten); DropStale records the relevance-decay drop. A no-op when retracement is off / the buffer is
// empty. Called at the TOP of a tick so a re-entry repositions the active branch before this tick thinks.
func (e *Engine) drainRetracements(tick int) {
	for _, r := range e.pendingInj.Drain(tick, e.graph.ActiveBranch) {
		switch r.Route {
		case seams.ProposeRetracement:
			// the seam proposed; the Controller FIRES Reenter — re-open the passed node, fork a new line
			// seeded with the late injection, focus to it. Reenter itself emits the conscious.mcp
			// retracement event (graph/mcp.go). A stale/unknown anchor makes Reenter a -1 no-op.
			seed := &types.Thought{ID: -1, Text: r.Text, Source: types.INJECTED, Confidence: 0.5}
			e.mcp.Reenter(r.AnchorBranch, "late injection", seed)
		case seams.InjectAtHead:
			// still on the anchor line — just voice the late injection on the active head.
			e.appendThought(&types.Thought{ID: -1, Text: r.Text, Source: types.INJECTED, Confidence: 0.5}, tick)
		case seams.DropStale:
			e.bus.Emit(events.MCP, "stale late injection dropped (relevance decay)",
				events.D{"op": "drop_stale", "anchor": r.AnchorBranch, "text": r.Text})
		}
	}
}
