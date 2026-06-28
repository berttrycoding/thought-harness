package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// longThought is a realistically verbose ~24-word thought (the kind a model produces), so the level-2
// headline gist is a genuine cut — the regime where the working window earns its bound.
func longThought(i int) string {
	return fmt.Sprintf("thought number %d works through a distinct sub-topic of the overall problem at some length, weighing the evidence and the open questions before committing to a tentative next step here", i)
}

func longerContext(n int) []types.Thought {
	var ctx []types.Thought
	for i := 1; i <= n; i++ {
		ctx = append(ctx, types.Thought{
			ID: i, Tick: i, Source: types.GENERATED, Confidence: 0.5, Text: longThought(i),
		})
	}
	return ctx
}

func longContext(n int) []types.Thought {
	var ctx []types.Thought
	for i := 1; i <= n; i++ {
		ctx = append(ctx, types.Thought{
			ID: i, Tick: i, Source: types.GENERATED, Confidence: 0.5,
			Text: fmt.Sprintf("thought number %d exploring a distinct sub-topic of the problem", i),
		})
	}
	return ctx
}

// TestWorkingContextPassthroughDefault: ContextBudget==0 is a strict passthrough — the engine sees the
// raw context unchanged (this is why the scenario goldens hold).
func TestWorkingContextPassthroughDefault(t *testing.T) {
	ctx := longContext(8)
	got := compressContext(ctx, 0)
	if len(got) != len(ctx) {
		t.Fatalf("budget 0 must passthrough unchanged; got %d want %d", len(got), len(ctx))
	}
	for i := range ctx {
		if got[i].ID != ctx[i].ID || got[i].Text != ctx[i].Text {
			t.Fatalf("passthrough altered item %d", i)
		}
	}
}

// TestWorkingContextCompressesUnderBudget is the P4.1 gate: with a budget set, the working context is
// BOUNDED (within the word budget), keeps the FOCUS sharp (recall), and preserves each thought's Source
// (it's the same thoughts, just zoomed) — budget + recall + durability hold through the wiring.
func TestWorkingContextCompressesUnderBudget(t *testing.T) {
	ctx := longContext(12) // ~9 words each => ~108 words of raw context
	const budget = 20      // words

	got := compressContext(ctx, budget)

	// budget: the working set fits the word budget (zoommem guarantees Total <= Budget).
	words := 0
	for _, t := range got {
		words += len(strings.Fields(t.Text))
	}
	if words > budget {
		t.Fatalf("working context is over budget: %d words > %d", words, budget)
	}
	// compression engaged (fewer/shorter than the raw 12).
	if len(got) >= len(ctx) && words >= 100 {
		t.Fatalf("compression did not engage under a tight budget; %d items / %d words", len(got), words)
	}
	// recall: the FOCUS (the most recent thought, id 12) is pinned and present.
	focusPresent := false
	for _, th := range got {
		if th.ID == 12 {
			focusPresent = true
		}
		if th.Source != types.GENERATED {
			t.Fatalf("the working context must preserve each thought's Source; item %d had %v", th.ID, th.Source)
		}
	}
	if !focusPresent {
		t.Fatal("recall FAIL: the focus thought was dropped from the working context")
	}
}

// ----------------------------------------------------------------------------
// D5 — sliding-window compaction over the active line (THOUGHT_WORKING_WINDOW)
// ----------------------------------------------------------------------------

// TestSlidingWindowPassthroughOff: window<=0 is a strict passthrough — the engine sees the raw context
// unchanged (the default-OFF byte-identity that keeps the goldens safe). Also covers the within-window
// case (a context no larger than the window+goal is never gisted).
func TestSlidingWindowPassthroughOff(t *testing.T) {
	ctx := longContext(20)
	// OFF (window 0): identical slice.
	got := slidingWindow(ctx, 0)
	if len(got) != len(ctx) {
		t.Fatalf("window 0 must passthrough; got %d want %d", len(got), len(ctx))
	}
	for i := range ctx {
		if got[i].ID != ctx[i].ID || got[i].Text != ctx[i].Text {
			t.Fatalf("window 0 altered item %d", i)
		}
	}
	// within-window (window >= len-1): nothing to gist, passthrough.
	small := longContext(4)
	for _, w := range []int{4, 5, 10} {
		out := slidingWindow(small, w)
		for i := range small {
			if out[i].Text != small[i].Text {
				t.Fatalf("window %d on a %d-thought ctx must passthrough; item %d gisted", w, len(small), i)
			}
		}
	}
}

// TestSlidingWindowBoundsAndKeepsGoalAndRecent is the D5 bound + cognition gate: on a GROWN active line,
// the goal (ctx[0], the setpoint) and the most-recent-N thoughts stay FULL, the older middle is lossy-
// gisted (shorter, never dropped), every thought's ID/Source/Confidence survive, and the assembled
// working context is BOUNDED in word-size as the graph grows (it does not grow linearly with the tick
// count once past the window).
func TestSlidingWindowBoundsAndKeepsGoalAndRecent(t *testing.T) {
	const window = 4

	wordsOf := func(ts []types.Thought) int {
		n := 0
		for _, th := range ts {
			n += len(strings.Fields(th.Text))
		}
		return n
	}

	grown := slidingWindow(longerContext(40), window) // goal + 4 recent full, 35 older gisted

	// nothing dropped: same count as the raw context.
	if len(grown) != 40 {
		t.Fatalf("window must never DROP a thought; got %d want 40", len(grown))
	}
	// goal (index 0) stays full.
	if grown[0].Text != longThought(1) {
		t.Fatalf("the goal (ctx[0]) must stay FULL; got %q", grown[0].Text)
	}
	// the most-recent `window` thoughts stay full; the middle is gisted (shorter) and metadata survives.
	if grown[len(grown)-1].Text != longThought(40) {
		t.Fatalf("the most-recent thought (the focus) must stay FULL; got %q", grown[len(grown)-1].Text)
	}
	gisted := 0
	for i, th := range grown {
		full := longThought(i + 1)
		recent := i >= len(grown)-window
		if i == 0 || recent {
			if th.Text != full {
				t.Fatalf("goal/recent item %d must be full; got %q", i, th.Text)
			}
		} else {
			if th.Text == full {
				t.Fatalf("older item %d must be gisted (shorter), but is full: %q", i, th.Text)
			}
			if len(th.Text) >= len(full) {
				t.Fatalf("older item %d gist (%q) is not shorter than full (%q)", i, th.Text, full)
			}
			gisted++
		}
		// metadata is preserved on every unit (gisted or not): nothing silently lost.
		if th.ID != i+1 || th.Source != types.GENERATED || th.Confidence != 0.5 {
			t.Fatalf("item %d lost metadata: id=%d src=%v conf=%v", i, th.ID, th.Source, th.Confidence)
		}
	}
	if gisted != 40-window-1 {
		t.Fatalf("expected %d gisted older thoughts, got %d", 40-window-1, gisted)
	}

	// BOUND: as the graph GROWS, the windowed context grows far slower than the raw context — the
	// per-thought cost of an OLDER thought is capped at the level-2 headline. Measure the slope.
	raw20, raw80 := wordsOf(longerContext(20)), wordsOf(longerContext(80))
	win20 := wordsOf(slidingWindow(longerContext(20), window))
	win80 := wordsOf(slidingWindow(longerContext(80), window))
	rawSlope := float64(raw80-raw20) / 60.0 // words added per extra thought, raw
	winSlope := float64(win80-win20) / 60.0 // words added per extra thought, windowed
	if win80 >= raw80 {
		t.Fatalf("window must SHRINK the grown context: windowed %d >= raw %d", win80, raw80)
	}
	if winSlope >= rawSlope {
		t.Fatalf("window must lower the growth SLOPE: windowed %.2f >= raw %.2f words/thought", winSlope, rawSlope)
	}
	// the full part (goal + window) is a constant; the windowed slope is just the headline cap, so the
	// windowed context grows at well under HALF the raw rate — the bound is real, not marginal.
	if winSlope > rawSlope*0.6 {
		t.Fatalf("window bound too weak: windowed slope %.2f is > 60%% of raw %.2f", winSlope, rawSlope)
	}
}

// TestSlidingWindowDeterministic: the window is a pure function of (ctx, window) — same input, same
// output, every time (no clock, no RNG, no backend).
func TestSlidingWindowDeterministic(t *testing.T) {
	ctx := longContext(30)
	first := slidingWindow(ctx, 5)
	for r := 0; r < 16; r++ {
		again := slidingWindow(ctx, 5)
		if len(again) != len(first) {
			t.Fatalf("replay %d: length differs", r)
		}
		for i := range first {
			if again[i].Text != first[i].Text || again[i].ID != first[i].ID {
				t.Fatalf("replay %d: item %d differs (nondeterministic)", r, i)
			}
		}
	}
}

// TestSlidingWindowCompressRefocusExpandRestores is the CORE cognition-preservation test: a compressed
// (older, gisted-by-the-window) thought, when the engine REFOCUSES back to its branch, EXPANDS back to
// full detail. The window is a read-time VIEW — the graph keeps full text in Nodes forever — so detail is
// never destroyed: after focusing away to a sibling and back, the active line's recent thoughts are full
// again, and the full text of every windowed-out thought is recoverable from the graph. This is the
// EXPANDED-branch / expand-on-focus property, not a byte count.
func TestSlidingWindowCompressRefocusExpandRestores(t *testing.T) {
	const window = 3
	g := graph.New("solve the riddle")
	be := backends.NewTest()
	mcp := graph.NewThoughtMCP(g, be, events.NewDefault().Emit)

	// grow the active line (branch 0) well past the window.
	deep := []string{
		"the riddle mentions a key that opens no door which is a metaphor worth unpacking carefully",
		"a clue about silence suggests the answer relates to something heard but not spoken aloud",
		"considering the second line the word echo fits the silence-and-sound pattern strongly",
		"the third line about mountains implies an echo that returns from a distant rocky valley",
		"i am now fairly confident the answer to the whole riddle is an echo in a canyon",
	}
	for i, txt := range deep {
		mcp.Tick = i + 1
		g.Append(&types.Thought{ID: -1, Text: txt, Source: types.GENERATED, Confidence: 0.6}, i+1)
	}

	full := g.ActiveContext()
	// the gisted-out thought we will check: an OLDER thought (id 2, the "key" clue) is behind the window.
	const targetID = 2
	var fullTarget string
	for _, th := range full {
		if th.ID == targetID {
			fullTarget = th.Text
		}
	}
	if fullTarget == "" {
		t.Fatal("setup: target thought not found in the active context")
	}

	// 1) the window GISTS the older target (it is behind the recent window) — it is compressed in the VIEW.
	view := slidingWindow(full, window)
	var viewTarget string
	for _, th := range view {
		if th.ID == targetID {
			viewTarget = th.Text
		}
	}
	if viewTarget == fullTarget {
		t.Fatal("setup: target should be GISTED in the windowed view (it is behind the window)")
	}
	if len(viewTarget) >= len(fullTarget) {
		t.Fatalf("gisted target (%q) is not shorter than full (%q)", viewTarget, fullTarget)
	}

	// 2) fork a sibling and FOCUS to it (compress the line we leave), then FOCUS BACK.
	sib := mcp.Branch("explore an alternate reading", &types.Thought{Text: "maybe the answer is a shadow", Source: types.GENERATED})
	mcp.Focus(sib)
	if g.ActiveBranch == 0 {
		t.Fatal("focus did not switch the active branch")
	}
	mcp.Focus(0) // refocus back to the original line — expand-on-focus.
	if g.ActiveBranch != 0 {
		t.Fatal("refocus did not return to the original branch")
	}

	// 3) EXPAND-ON-FOCUS: the active line is back at FULL detail — the windowed-out target's full text is
	// fully recoverable from the graph (the view never destroyed it). The window is lossy in the VIEW only.
	restored := g.ActiveContext()
	var restoredTarget string
	for _, th := range restored {
		if th.ID == targetID {
			restoredTarget = th.Text
		}
	}
	if restoredTarget != fullTarget {
		t.Fatalf("expand-on-focus FAIL: target not restored to full detail; got %q want %q", restoredTarget, fullTarget)
	}
	// and after the refocus, the NEW window keeps the most-recent active-line thoughts full again.
	review := slidingWindow(restored, window)
	if review[len(review)-1].Text == "" {
		t.Fatal("the refocused window dropped the focus thought")
	}
}

// TestWorkingContextWiresWindow is the WIRED proof: the engine's per-tick context-assembly path
// (workingContext) ACTUALLY uses the sliding window when THOUGHT_WORKING_WINDOW is ON — it bounds the
// context AND emits the observable memory.compact event — and is a byte-identical PASSTHROUGH when OFF
// (the default). It drives the real engine.workingContext() (the thing the synthesiser/responder/intender
// consume), not just the helper. workingWindow is a package var resolved from env at init; the test
// overrides it directly + restores it, so it does not depend on process env order.
func TestWorkingContextWiresWindow(t *testing.T) {
	const window = 4

	// build a real engine on the test double and grow its active branch past the window.
	e := newHeuristicEngine(t, "reactive")
	e.SubmitDefault("work through a long multi-step problem")
	e.Step() // first reason tick: e.graph = graph.New(goal) + the root thought.
	for i := 0; i < 40; i++ {
		e.graph.Append(&types.Thought{ID: -1, Text: longThought(i + 2), Source: types.GENERATED, Confidence: 0.6}, i+2)
	}
	rawLen := len(e.graph.ActiveContext())
	if rawLen < window+2 {
		t.Fatalf("setup: the active branch did not grow past the window (len %d)", rawLen)
	}
	wordsOf := func(ts []types.Thought) int {
		n := 0
		for _, th := range ts {
			n += len(strings.Fields(th.Text))
		}
		return n
	}
	rawWords := wordsOf(e.graph.ActiveContext())

	// OFF (the default 0): workingContext is a byte-identical passthrough of the raw active context,
	// and NO memory.compact event fires.
	saved := workingWindow
	defer func() { workingWindow = saved }()

	workingWindow = 0
	var compactOff int
	unsubOff := e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.MemoryCompact {
			compactOff++
		}
	})
	off := e.workingContext()
	unsubOff()
	raw := e.graph.ActiveContext()
	if len(off) != len(raw) {
		t.Fatalf("OFF must passthrough: got %d want %d", len(off), len(raw))
	}
	for i := range raw {
		if off[i].ID != raw[i].ID || off[i].Text != raw[i].Text {
			t.Fatalf("OFF altered item %d (not byte-identical)", i)
		}
	}
	if compactOff != 0 {
		t.Fatalf("OFF must be silent; got %d memory.compact events", compactOff)
	}

	// ON: workingContext WIRES the window — it bounds the context (gists the older thoughts) AND emits
	// memory.compact. The focus (most-recent) thought and the goal stay full.
	workingWindow = window
	var compactOn int
	var lastData events.D
	unsubOn := e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.MemoryCompact {
			compactOn++
			lastData = ev.Data
		}
	})
	on := e.workingContext()
	unsubOn()

	if compactOn == 0 {
		t.Fatal("ON must EMIT memory.compact — the bound is not observable / not wired")
	}
	if g, ok := lastData["gisted"].(int); !ok || g <= 0 {
		t.Fatalf("memory.compact must report a positive gisted count; got %v", lastData["gisted"])
	}
	onWords := wordsOf(on)
	if onWords >= rawWords {
		t.Fatalf("ON must BOUND the context: windowed %d words >= raw %d", onWords, rawWords)
	}
	// the focus (the active line's most-recent thought) is preserved full in the windowed context.
	rawTail := raw[len(raw)-1].Text
	onTail := ""
	for _, th := range on {
		if th.ID == raw[len(raw)-1].ID {
			onTail = th.Text
		}
	}
	if onTail != rawTail {
		t.Fatalf("ON must keep the focus thought FULL; got %q want %q", onTail, rawTail)
	}
	t.Logf("WIRED: raw=%d words -> windowed=%d words (window=%d, gisted=%v)", rawWords, onWords, window, lastData["gisted"])
}
