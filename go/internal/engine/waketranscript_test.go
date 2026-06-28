package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// mkWakeEngine builds a continuous-mode engine on the test backend double (deterministic, offline) with
// the default (all-on) feature config. The graph is nil at construction, so the FIRST continuous tick
// takes the graph==nil WAKE branch — the exact path the B3-outreach bug lives on.
func mkWakeEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = config.New()
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// userTurns counts the role=="user" turns in the transcript and returns the first one's text. This is
// exactly what maybeReachOut's userTopics is built from (continuous.go): zero user turns => empty
// userTopics => outreach can never fire.
func userTurns(e *Engine) (count int, firstText string) {
	for _, turn := range e.transcript {
		if turn.Role == "user" {
			count++
			if firstText == "" {
				firstText = turn.Text
			}
		}
	}
	return count, firstText
}

// TestWakeTranscriptPersistsSeedUserTurn is the B3-OUTREACH WIRING test (THOUGHT_WAKE_TRANSCRIPT). The
// continuous loop's first awake tick takes the graph==nil branch: it Pop()s the queued user percept and
// seeds the episode via startEpisode(fromUser=true). The bug: that path NEVER appends the user turn to
// e.transcript, so maybeReachOut's userTopics is always empty and proactive outreach can never fire
// live. The fix is flag-gated and additive:
//
//   - FLAG OFF (default): byte-identical — the wake path persists NO user turn (the bug remains the
//     default until the claude characterization validates flipping it on).
//   - FLAG ON: the wake path appends the seed user turn, so the transcript holds it (userTopics can now
//     populate).
//
// Mutation-sensitive: if the gated append is removed, the ON assertion fails; if the append is made
// unconditional (no flag), the OFF byte-identical assertion fails.
func TestWakeTranscriptPersistsSeedUserTurn(t *testing.T) {
	prev := wakeTranscriptEnabled
	defer func() { wakeTranscriptEnabled = prev }()

	const seed = "tell me about the weather in Paris"

	// FLAG OFF (default): the wake path must persist NO user turn — byte-identical to today's bug.
	wakeTranscriptEnabled = false
	eOff := mkWakeEngine(t)
	eOff.Submit(seed, true) // queue the user percept BEFORE the first tick (graph==nil => WAKE path)
	if eOff.Graph() != nil {
		t.Fatal("precondition: graph must be nil before the first continuous tick (the WAKE path)")
	}
	eOff.Step() // tick 1: graph==nil WAKE branch — Pop()s the seed, seeds the episode
	if eOff.Graph() == nil {
		t.Fatal("flag OFF: the WAKE path did not seed an episode (graph still nil) — wrong path exercised")
	}
	if c, _ := userTurns(eOff); c != 0 {
		t.Fatalf("flag OFF (default): the WAKE path persisted %d user turn(s) — NOT byte-identical (the bug must remain the default)", c)
	}

	// FLAG ON: the wake path appends the seed user turn, matching the reactive + percept-stream paths.
	wakeTranscriptEnabled = true
	eOn := mkWakeEngine(t)
	eOn.Submit(seed, true)
	eOn.Step() // tick 1: graph==nil WAKE branch — now appends the user turn
	c, first := userTurns(eOn)
	if c != 1 {
		t.Fatalf("flag ON: the WAKE path persisted %d user turn(s), want exactly 1 (no double-append, no drop)", c)
	}
	if first != seed {
		t.Fatalf("flag ON: persisted user turn text = %q, want the seed %q", first, seed)
	}
}

// TestWakeTranscriptNoDoubleAppendOnPerceptStream guards the no-double-append requirement on the OTHER
// continuous path. The percept-stream path (continuous.go) already appends a salient USER_INPUT percept
// to the transcript on its own; the wake-path fix must NOT add a second append there. After the WAKE
// tick consumes the seed via Pop(), a SUBSEQUENT salient user percept flows through the percept-stream
// path on a later tick — and that path's single append must remain single regardless of the flag.
//
// Because Pop() FIFO-dequeues the wake seed, the percept-stream Stream() can never re-deliver it, so the
// wake seed and a later percept are distinct turns; this asserts each is counted exactly once.
func TestWakeTranscriptNoDoubleAppendOnPerceptStream(t *testing.T) {
	prev := wakeTranscriptEnabled
	defer func() { wakeTranscriptEnabled = prev }()
	wakeTranscriptEnabled = true // the fix is ON: the risk is a SECOND append somewhere

	const seed = "investigate the auth module please"
	const later = "actually focus on the database layer instead"

	e := mkWakeEngine(t)
	e.Submit(seed, true)
	e.Step() // WAKE tick: seeds the episode, appends the seed user turn once (flag ON)

	cAfterWake, _ := userTurns(e)
	if cAfterWake != 1 {
		t.Fatalf("after WAKE tick: %d user turns, want exactly 1 (the seed, appended once)", cAfterWake)
	}

	// A later salient user percept now flows through the percept-stream path (graph != nil), which has
	// its OWN append. The flag must not cause a second append for THIS turn.
	e.Submit(later, true)
	e.Step()

	c, _ := userTurns(e)
	// Each distinct user turn is counted at most once. The seed is counted once; the later percept is
	// counted at most once (the percept-stream path may not admit it on this exact tick, but it must
	// never be counted twice). The load-bearing guard: NEVER more than one append per distinct turn.
	if c > 2 {
		t.Fatalf("double-append detected: %d user turns for at most 2 distinct user turns (seed + later)", c)
	}
	// And the seed in particular must still be present exactly once (not duplicated by the percept path).
	seedCount := 0
	for _, turn := range e.transcript {
		if turn.Role == "user" && turn.Text == seed {
			seedCount++
		}
	}
	if seedCount != 1 {
		t.Fatalf("the WAKE seed user turn appears %d times, want exactly 1 (no double-append)", seedCount)
	}
}
