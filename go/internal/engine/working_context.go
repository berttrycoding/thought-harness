// working_context.go wires the zoomable working set (internal/zoommem) into the engine's context path
// (P4.1) and the D5 sliding-window compaction over the active line. The synthesiser + responder consume a
// snapshot of the active branch; two OPT-IN, default-OFF transforms can bound that snapshot as the thought
// graph grows:
//
//   - SLIDING WINDOW (D5, THOUGHT_WORKING_WINDOW): keep the root goal (the setpoint) + the most-recent-N
//     thoughts of the active line at FULL detail, and LOSSY-GIST the older same-line thoughts to a one-line
//     headline (multicompress level 2). The graph is never mutated — full text stays in graph.Nodes — so it
//     is a read-time VIEW: on a refocus (mcp.Focus) ActiveContext() returns the new active line at full
//     detail again, restoring the gisted detail (expand-on-focus). Nothing is dropped (a gisted thought
//     keeps its ID/Source/Confidence, only its Text is shortened), so the active context stays bounded as
//     the graph grows instead of growing linearly with the tick count.
//   - CONTEXT BUDGET (P4.1, ContextBudget): the zoommem zoomable working set, a word-size budget over the
//     (already-windowed) context — relevant-old kept over irrelevant-recent; an out-of-budget unit folds to
//     a pointer, never vanishes.
//
// Both are OFF by default (window N==0, budget==0) ⇒ a strict PASSTHROUGH of the raw active-branch context,
// byte-identical to before — the wiring is opt-in and the scenario goldens are unchanged.
package engine

import (
	"os"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/zoommem"
)

// workingWindow is the D5 sliding-window size, resolved ONCE at package init from THOUGHT_WORKING_WINDOW,
// defaulting to 0 (OFF) — mirroring resolveParallelPhases / THOUGHT_PARALLEL_PHASES. N>0 keeps the goal +
// the most-recent-N active-line thoughts full and gists the rest; an unset / non-positive / unparsable
// value keeps the safe default-off (raw passthrough).
var workingWindow = resolveWorkingWindow()

// resolveWorkingWindow reads THOUGHT_WORKING_WINDOW once. It is a count (>=1 enables the window); 0 / unset /
// negative / unparsable ⇒ OFF.
func resolveWorkingWindow() int {
	raw := os.Getenv("THOUGHT_WORKING_WINDOW")
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// slidingWindow bounds ctx (an ordered active-branch context, oldest..newest) to a sliding window: the root
// goal (ctx[0], the setpoint) and the most-recent `window` thoughts stay at FULL detail; the older middle
// thoughts are LOSSY-gisted to a one-line headline (multicompress level 2). window<=0, or a context already
// within the window, is a strict passthrough (byte-identical). Pure + deterministic (no clock, no RNG, no
// backend): the gist is graph.LevelGist over each older thought's own text, so the cut is reproducible.
//
// COGNITION-PRESERVING: it never DROPS a thought (each kept unit retains its ID/Source/Confidence/Branch —
// only the gisted ones' Text is shortened) and it gists ONLY older thoughts BEHIND the live window — the
// recent active line (the focus) is always full. Because the graph keeps full text in Nodes, the gist is a
// read-time view: a refocus that makes another branch active returns ITS full thoughts here, so detail is
// restored on expand-on-focus.
func slidingWindow(ctx []types.Thought, window int) []types.Thought {
	if window <= 0 || len(ctx) <= window+1 {
		// +1: the goal at ctx[0] is always kept full, so a window of N admits N recent + the goal
		// before any gisting is needed.
		return ctx
	}
	out := make([]types.Thought, len(ctx))
	// the cut index: everything at or after `from` is recent (kept full); ctx[0] (goal) is kept full;
	// the middle (1..from-1) is gisted.
	from := len(ctx) - window
	for i, t := range ctx {
		if i == 0 || i >= from {
			out[i] = t // goal + the recent window: full detail
			continue
		}
		g := t // copy: never mutate the source node (the graph holds the only mutable copy)
		g.Text = graph.LevelGist([]string{t.Text}, 2)
		out[i] = g
	}
	return out
}

// compressContext applies the zoomable working set to a context within a word-size budget. budget<=0 (or
// an empty context) is a strict passthrough. Otherwise it assembles the zoommem working set focused on
// the latest thought and returns it as Thoughts whose TEXT is the chosen zoom level (Source/Confidence
// preserved). Bounded by construction (zoommem guarantees Total<=Budget).
func compressContext(ctx []types.Thought, budget int) []types.Thought {
	if budget <= 0 || len(ctx) == 0 {
		return ctx
	}
	byID := make(map[int]types.Thought, len(ctx))
	units := make([]zoommem.Unit, len(ctx))
	for i, t := range ctx {
		byID[t.ID] = t
		units[i] = zoommem.Unit{ID: t.ID, Tick: t.Tick, Thought: t.Text, Full: t.Text}
	}
	focusID := ctx[len(ctx)-1].ID // focus = the most recent thought (the current line)
	zc := zoommem.Assemble(units, focusID, budget)

	out := make([]types.Thought, 0, len(zc.Shown))
	for _, sh := range zc.Shown {
		t := byID[sh.Unit.ID] // keep Source / Confidence / branch
		t.Text = sh.Text()    // compress the text to the chosen zoom level
		out = append(out, t)
	}
	return out
}

// workingContext is the active-branch context the engine's consumers should see: the raw active context by
// default, or — when opted in — the D5 sliding-window over the active line (THOUGHT_WORKING_WINDOW) and/or
// the zoomable working set (ContextBudget). The window runs first (bounds the COUNT as the graph grows),
// then the budget (bounds the word-size). Both OFF ⇒ raw passthrough.
func (e *Engine) workingContext() []types.Thought {
	raw := e.graph.ActiveContext()
	windowed := slidingWindow(raw, workingWindow)
	if workingWindow > 0 && e.bus != nil {
		// emit only when the window actually fired (some thoughts were gisted) — the bound is observable
		// (the observability contract). Only fires on the ON path, so default-OFF goldens are unaffected.
		if gisted := countGisted(raw, windowed); gisted > 0 {
			e.bus.Emit(events.MemoryCompact,
				"working-window kept goal+"+itoa(workingWindow)+" recent, gisted "+itoa(gisted),
				events.D{"window": workingWindow, "total": len(raw), "gisted": gisted})
		}
	}
	return compressContext(windowed, e.cfg.ContextBudget)
}

// countGisted counts how many thoughts the sliding window lossy-compressed (text changed). raw and
// windowed are the same length (slidingWindow never drops a unit).
func countGisted(raw, windowed []types.Thought) int {
	n := 0
	for i := range raw {
		if raw[i].Text != windowed[i].Text {
			n++
		}
	}
	return n
}
