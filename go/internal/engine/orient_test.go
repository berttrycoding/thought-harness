package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// orientEngine builds a full engine (the LIVE wiring path, like the percept tests) with sense.orient and
// sense.clock set per arg, a Fake clock at a KNOWN instant, and (optionally) a rehydrated priorContext
// carrying a KNOWN gist — the smallest harness exercising the orientation pass through startEpisode. The
// priorContext is set directly (Track 2 normally rehydrates it on boot; the test pins it so the gist is
// asserted exactly against a known value, mirroring graph_spine_test's hand-set episodeContext).
func orientEngine(t *testing.T, orientOn, clockOn bool, gist string, fakeAt clockpkg.Clock) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feats := config.AllOn()
	feats.Sense.Orient = orientOn
	feats.Sense.Clock = clockOn
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SetClock(fakeAt, 0) // a Clock is WIRED; only the knobs gate the senses
	if gist != "" {
		e.priorContext = &subconscious.Context{
			Goal: "the prior episode goal",
			L1: subconscious.L1Snapshot{
				BranchID:   3,
				Gist:       gist,
				ThoughtIDs: []int{1, 2, 3},
				Resolution: "EXPANDED",
			},
			L2: map[string]any{},
		}
	}
	return e
}

// orientationThought returns the first GENERATED thought whose text begins with the orientation template
// prefix ("Resuming. ..."), or ("", false) if none — the way the tests locate the injected re-grounding
// thought in the conscious stream without coupling to its graph id.
func orientationThought(e *Engine) (types.Thought, bool) {
	for _, th := range e.graph.History() {
		if th.Source == types.GENERATED && strings.HasPrefix(th.Text, "Resuming. Prior focus:") {
			return th, true
		}
	}
	return types.Thought{}, false
}

// TestOrientOffByteIdentical is the FLAG-OFF byte-identical guard (the §11 Track-3 default): with
// sense.orient OFF, startEpisode runs the orientation pass as a pure no-op — NO re-grounding thought is
// injected, NO belief is written, and NO perception.orient event fires — even with a prior spine + a
// wired clock present. (Only the KNOBS, not the inputs, gate the pass.)
func TestOrientOffByteIdentical(t *testing.T) {
	// orient OFF, clock OFF, but a prior spine IS present and a clock IS wired.
	e := orientEngine(t, false, false, "the prior focus gist", clockpkg.NewFake())
	beliefsBefore := e.semantic.Len()
	e.startEpisode("answer the user", true)

	if _, ok := orientationThought(e); ok {
		t.Fatal("orient OFF: a re-grounding thought was injected, want none (byte-identical)")
	}
	if got := e.semantic.Len() - beliefsBefore; got != 0 {
		t.Fatalf("orient OFF: %d belief(s) written, want 0 (no handshake)", got)
	}
	if n := countKind(e.bus, events.PerceptionOrient); n != 0 {
		t.Fatalf("orient OFF: %d perception.orient events, want 0 (byte-identical)", n)
	}
	if e.oriented {
		t.Fatal("orient OFF: once-flag burned, want untouched (the pass never engaged)")
	}
}

// TestOrientOnInjectsCorrectThought is the CORRECTNESS ORACLE for the conscious-layer re-grounding (not a
// firing count): with sense.orient + sense.clock ON, a Fake clock at a KNOWN instant, and a rehydrated
// prior spine with a KNOWN gist, the injected thought's text must CONTAIN the EXACT clock value (== the
// Fake instant) AND the EXACT prior gist (== what Track 2 persisted). A paraphrase that loses either value
// fails — the orientation must re-ground on the real sensed reality, verbatim.
func TestOrientOnInjectsCorrectThought(t *testing.T) {
	const gist = "tracing the regulator gain estimate via lag-1 regression"
	fake := clockpkg.NewFake() // fixed instant 2026-01-01T00:00:00Z (clock.NewFake's epoch)
	e := orientEngine(t, true, true, gist, fake)

	e.startEpisode("continue the analysis", true)

	th, ok := orientationThought(e)
	if !ok {
		t.Fatal("orient ON resume-boot: no re-grounding GENERATED thought was injected")
	}
	// The EXACT clock value the Fake produced — the same encoding senseClock records.
	wantClock := fake.Now().UTC().Format(perceptClockFormat)
	if !strings.Contains(th.Text, wantClock) {
		t.Fatalf("orientation thought missing the EXACT sensed clock value.\n thought: %q\n want substring: %q", th.Text, wantClock)
	}
	// The EXACT prior gist Track 2 persisted — the orientation re-grounded on "where I was", verbatim.
	if !strings.Contains(th.Text, gist) {
		t.Fatalf("orientation thought missing the EXACT prior gist.\n thought: %q\n want substring: %q", th.Text, gist)
	}
	// It is the mind's own next thought (GENERATED), not an external dump.
	if th.Source != types.GENERATED {
		t.Fatalf("orientation thought source = %v, want GENERATED (re-voiced as the stream's own thought)", th.Source)
	}
}

// TestOrientHandshakeWritesCorrectBelief is the CORRECTNESS ORACLE for the perception->memory handshake:
// on a resume-wake with the knobs ON, EXACTLY ONE belief is written, it carries the CORRECT date (the Fake
// instant, verbatim), and it is marked grounded. A bus count of "belief written" is the project's
// lying-green trap — this asserts the PAYLOAD, the actual content reality wrote.
func TestOrientHandshakeWritesCorrectBelief(t *testing.T) {
	fake := clockpkg.NewFake()
	e := orientEngine(t, true, true, "the prior focus gist", fake)
	beliefsBefore := e.semantic.Len()

	e.startEpisode("resume work", true)

	// EXACTLY one belief written by the handshake (the structural-handshake count, with the right payload).
	if got := e.semantic.Len() - beliefsBefore; got != 1 {
		t.Fatalf("handshake wrote %d belief(s), want exactly 1", got)
	}
	// The belief carries the CORRECT date verbatim (the Fake instant), and is GROUNDED (Record rejects an
	// ungrounded belief, so a stored belief is grounded by construction — assert the statement payload too).
	wantClock := fake.Now().UTC().Format(perceptClockFormat)
	beliefs := e.semantic.Current()
	var found bool
	for _, b := range beliefs {
		if strings.Contains(b.Statement, wantClock) {
			found = true
			if !b.Grounded {
				t.Fatalf("the date belief is not marked grounded: %+v", b)
			}
			if b.Source != "orientation:read_clock" {
				t.Fatalf("date belief source = %q, want %q", b.Source, "orientation:read_clock")
			}
		}
	}
	if !found {
		t.Fatalf("no grounded belief carries the EXACT sensed date %q.\n current beliefs: %+v", wantClock, beliefs)
	}
	// The belief is RECALLABLE by a date query (it actually entered semantic memory, not a dangling write).
	if rs := e.semantic.Recall("current date time now", 1); len(rs) == 0 {
		t.Fatal("the orientation date belief is not recallable from semantic memory")
	}
}

// TestOrientEmitsWitnessWithCorrectValues asserts the perception.orient event carries the EXACT sensed
// values (gist + clock) in its data — the witness on the bus must report the same reality the thought +
// belief carry (so a TUI / trace reads the true orientation, not a summary that drifts from the payload).
func TestOrientEmitsWitnessWithCorrectValues(t *testing.T) {
	const gist = "the verbatim prior gist to witness"
	fake := clockpkg.NewFake()
	e := orientEngine(t, true, true, gist, fake)

	e.startEpisode("resume", true)

	wantClock := fake.Now().UTC().Format(perceptClockFormat)
	var saw bool
	for _, ev := range e.bus.Recent(10000, nil) {
		if ev.Kind != events.PerceptionOrient {
			continue
		}
		saw = true
		if g, _ := ev.Data["gist"].(string); g != gist {
			t.Fatalf("perception.orient gist = %q, want the EXACT %q", g, gist)
		}
		if c, _ := ev.Data["clock"].(string); c != wantClock {
			t.Fatalf("perception.orient clock = %q, want the EXACT %q", c, wantClock)
		}
		if b, _ := ev.Data["belief"].(bool); !b {
			t.Fatal("perception.orient belief=false, want true (the handshake fired with a clock value)")
		}
	}
	if !saw {
		t.Fatal("no perception.orient event emitted on a resume-wake with the knobs ON")
	}
}

// TestOrientFiresExactlyOncePerEngine guards the once-flag: across several episode-opens only the FIRST
// wake orients — no second re-grounding thought, no second belief, no second perception.orient event —
// even though startEpisode is called repeatedly. The orientation is a boot-time pass, not a per-episode one.
func TestOrientFiresExactlyOncePerEngine(t *testing.T) {
	e := orientEngine(t, true, true, "once-only gist", clockpkg.NewFake())

	e.startEpisode("first wake", true)
	firstBeliefs := e.semantic.Len()
	firstOrients := countKind(e.bus, events.PerceptionOrient)
	if firstOrients != 1 {
		t.Fatalf("first wake: %d perception.orient events, want exactly 1", firstOrients)
	}

	// Two more episode-opens: the orientation must NOT fire again.
	e.startEpisode("second wake", true)
	e.startEpisode("third wake", true)

	if n := countKind(e.bus, events.PerceptionOrient); n != 1 {
		t.Fatalf("after 3 episode-opens: %d perception.orient events, want exactly 1 (once per engine)", n)
	}
	if got := e.semantic.Len() - firstBeliefs; got != 0 {
		t.Fatalf("later wakes wrote %d more belief(s), want 0 (orientation is once-only)", got)
	}
}

// TestOrientNonResumeBootSilent: with sense.orient ON but NEITHER a prior spine NOR clock-sensing (the
// resume-boot precondition unmet), the pass engages, burns the once-flag, and stays SILENT — no thought,
// no belief, no event. There is nothing new to orient on, so light re-orientation correctly does nothing.
func TestOrientNonResumeBootSilent(t *testing.T) {
	// orient ON, clock OFF, no prior spine (gist "" ⇒ orientEngine leaves priorContext nil).
	e := orientEngine(t, true, false, "", clockpkg.NewFake())
	beliefsBefore := e.semantic.Len()

	e.startEpisode("cold-ish boot", true)

	if _, ok := orientationThought(e); ok {
		t.Fatal("non-resume boot: a re-grounding thought was injected, want none (nothing to orient on)")
	}
	if got := e.semantic.Len() - beliefsBefore; got != 0 {
		t.Fatalf("non-resume boot: %d belief(s) written, want 0", got)
	}
	if n := countKind(e.bus, events.PerceptionOrient); n != 0 {
		t.Fatalf("non-resume boot: %d perception.orient events, want 0 (silent)", n)
	}
	// The once-flag IS burned (the pass is attempted at most once per engine, even when the precondition fails).
	if !e.oriented {
		t.Fatal("non-resume boot: once-flag not burned, want burned (the pass engaged but found nothing)")
	}
}

// TestOrientPriorSpineOnlyNoClock: a resume boot with a prior spine present but sense.clock OFF still
// re-grounds the conscious layer on the prior gist (the resume precondition is met by priorContext), but
// writes NO date belief (no clock value to ground) — the conscious re-grounding and the memory handshake
// are independently gated, exactly as the two senses are.
func TestOrientPriorSpineOnlyNoClock(t *testing.T) {
	const gist = "the prior line, no clock this boot"
	e := orientEngine(t, true, false, gist, clockpkg.NewFake()) // orient ON, clock OFF, prior spine present
	beliefsBefore := e.semantic.Len()

	e.startEpisode("resume on spine only", true)

	th, ok := orientationThought(e)
	if !ok {
		t.Fatal("prior-spine resume: no re-grounding thought injected, want one (the spine is the resume trigger)")
	}
	if !strings.Contains(th.Text, gist) {
		t.Fatalf("prior-spine resume: thought missing the EXACT gist %q: %q", gist, th.Text)
	}
	// No clock sense ⇒ no date belief written (the handshake needs a grounded clock value).
	if got := e.semantic.Len() - beliefsBefore; got != 0 {
		t.Fatalf("prior-spine resume with clock OFF: %d belief(s) written, want 0 (no clock to ground)", got)
	}
}
