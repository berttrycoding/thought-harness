package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// awake_dispatch_test.go — the AWAKE-DISP rung-0 regression gate
// (docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md).
//
// THE MEASURED BUG: the awake (continuous) loop's subconscious NEVER fires on a user input, while the
// reactive loop fires it 3x on the IDENTICAL input. Pinned root cause (NOT lag, NOT roster coverage, NOT
// the dispatch config gate): the awake interrupt path (continuous.go) forks/focuses a branch onto the
// ongoing graph (OnInterrupt) but NEVER synthesises a workflow for the user's goal — so the subconscious
// dispatch (which runs every tick) has NO relevance entry (e.workflow) to recognise and stays QUIET. The
// reactive loop, by contrast, runs startEpisode per user turn, which synthesises + SetWorkflow's the
// workflow that fires. The pin: on the awake user line the focus IS correct (the user branch is active) and
// the RAW user text IS in ActiveContext — only the synthesised workflow is missing.
//
// THE FIX (conscious.activity.awake_user_dispatch, default OFF): on a focused, unresolved awake user line,
// synthesise a workflow for the goal and wire it onto the subconscious (the same SetWorkflow reactive runs),
// once per branch. The Controller's existing DELIVER closes the line.
//
// These are the GATE: with the flag ON the awake subconscious fires on the rate-limiter input (approaching
// reactive's count); with it OFF the awake stream is byte-identical (fire=0, the current behaviour). A
// reactive control guards against a roster regression.

const rateLimiterInput = "design a rate limiter that supports BOTH per-tenant and a global cap, and explain how the two interact when a tenant's burst would push the system past the global cap"

// newAwakeEngineWithDispatch builds an awake engine on the validated awake defaults (ApplyAwakeDefaults),
// with the AWAKE-DISP rung-0 flag set explicitly. The test double fires specialists FOR REAL (only their
// content is canned), so the wiring question — does an awake user input engage the subconscious? — is
// answered deterministically offline.
func newAwakeEngineWithDispatch(t *testing.T, awakeUserDispatch bool) (*engine.Engine, *eventLog) {
	t.Helper()
	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Conscious.Activity.AwakeUserDispatch = awakeUserDispatch
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// driveAwakeRateLimiter wakes the awake stream, submits the rate-limiter input mid-stream, lets it run, and
// returns the subconscious fire/quiet tallies + the response count over the whole stream.
func driveAwakeRateLimiter(t *testing.T, awakeUserDispatch bool) (fire, quiet, respond int) {
	t.Helper()
	eng, log := newAwakeEngineWithDispatch(t, awakeUserDispatch)
	for i := 0; i < 3; i++ {
		eng.Step() // already awake + wandering
	}
	eng.SubmitDefault(rateLimiterInput)
	for i := 0; i < 8; i++ {
		eng.Step()
	}
	c := map[string]int{}
	for _, e := range log.events {
		c[string(e.Kind)]++
	}
	return c[string(events.SubFire)], c[string(events.SubQuiet)], c["action.respond"]
}

// TestAwakeUserDispatchEngagesSubconscious is the rung-0 gate: with the flag ON, the awake subconscious
// FIRES on the user input the way reactive does (and a deliver follows); with the flag OFF, it stays QUIET
// (fire=0) — the byte-identical current behaviour.
func TestAwakeUserDispatchEngagesSubconscious(t *testing.T) {
	offFire, offQuiet, offResp := driveAwakeRateLimiter(t, false)
	onFire, onQuiet, onResp := driveAwakeRateLimiter(t, true)

	t.Logf("AWAKE flag OFF: subconscious.fire=%d quiet=%d action.respond=%d", offFire, offQuiet, offResp)
	t.Logf("AWAKE flag ON : subconscious.fire=%d quiet=%d action.respond=%d", onFire, onQuiet, onResp)

	// FLAG OFF: the measured bug — the awake subconscious never fires on the user line.
	if offFire != 0 {
		t.Fatalf("flag OFF: expected subconscious.fire=0 (the current awake behaviour), got %d — the flag-OFF path is no longer byte-identical", offFire)
	}
	// FLAG ON: the subconscious now fires on the awake user line (the fix). Approaching reactive's count.
	if onFire == 0 {
		t.Fatalf("flag ON: subconscious never fired on the awake user line (fire=0) — the rung-0 fix did not engage the subconscious")
	}
	if onFire < offFire+1 {
		t.Fatalf("flag ON (fire=%d) did not increase firing over OFF (fire=%d)", onFire, offFire)
	}
	// The fix should approach reactive's count (reactive fires the synthesised `build` workflow 3x). Assert
	// the awake firing reaches at least the reactive count — the engagement is the same workflow, not a
	// weaker partial.
	reactiveFire := reactiveRateLimiterFire(t)
	if onFire < reactiveFire {
		t.Fatalf("flag ON awake firing (fire=%d) fell short of reactive (fire=%d) — engagement is weaker than reactive's", onFire, reactiveFire)
	}
}

// reactiveRateLimiterFire is the control: the SAME input in REACTIVE mode fires the subconscious. This
// guards against a roster regression (if reactive stops firing, the whole comparison is meaningless).
func reactiveRateLimiterFire(t *testing.T) int {
	t.Helper()
	eng, log := newSeededEngine(t, "reactive", 7)
	eng.SubmitDefault(rateLimiterInput)
	for i := 0; i < 25; i++ {
		eng.Step()
	}
	return len(log.of(string(events.SubFire)))
}

// TestReactiveStillFiresControl is the standing roster-coverage control: the rate-limiter input fires the
// reactive subconscious at least once. If this fails the roster regressed and the awake A/B is moot.
func TestReactiveStillFiresControl(t *testing.T) {
	if got := reactiveRateLimiterFire(t); got < 1 {
		t.Fatalf("reactive subconscious did not fire on the engineering input (fire=%d) — roster regression", got)
	}
}

// conversationalInput is a SIMPLE conversational/social turn — no task shape (RecognizeShape returns false).
// It is the input class the awake-bundle GO-LIVE regression broke: awake_user_dispatch fired a wasted
// SynthesizeProgram call on it, perturbing the live stream off the fast tick-0 social DELIVER. The fix gates
// the faculty on a task shape, so a conversational turn is a complete no-op on this path.
const conversationalInput = "hi i am here to ask you some questions"

// driveAwakeConversational wakes the awake stream, submits a CONVERSATIONAL (non-task-shaped) input, lets it
// run, and returns the subconscious fire/synth tallies + the response count over the whole stream.
func driveAwakeConversational(t *testing.T, awakeUserDispatch bool) (fire, synth, respond int) {
	t.Helper()
	eng, log := newAwakeEngineWithDispatch(t, awakeUserDispatch)
	for i := 0; i < 3; i++ {
		eng.Step() // already awake + wandering
	}
	eng.SubmitDefault(conversationalInput)
	for i := 0; i < 8; i++ {
		eng.Step()
	}
	c := map[string]int{}
	for _, e := range log.events {
		c[string(e.Kind)]++
	}
	return c[string(events.SubFire)], c[string(events.SubSynthesize)], c["action.respond"]
}

// TestAwakeUserDispatchSkipsConversational is the CONVERSATIONAL-REGRESSION GATE (the offline guard that
// pins the awake-bundle GO-LIVE fix). The faculty is FOR task-shaped goals; on a SIMPLE conversational turn
// (no RecognizeShape match) the awake_user_dispatch path must be a COMPLETE NO-OP — flipping the flag must
// not synthesise a workflow on it (no SubSynthesize delta from the awake-user-dispatch faculty), so the
// conversational turn answers the normal awake way instead of being diverted. This is the missing coverage:
// the prior gate only exercised a TASK-shaped input, so the conversational regression slipped through to a
// live run. (subFire is NOT asserted equal — the social/respond specialist may fire on the user line in
// BOTH cases; the load-bearing invariant is that the awake-user-dispatch faculty does NOT synthesise a
// workflow on a conversational turn, which is exactly what the perturbation came from.)
func TestAwakeUserDispatchSkipsConversational(t *testing.T) {
	offFire, offSynth, offResp := driveAwakeConversational(t, false)
	onFire, onSynth, onResp := driveAwakeConversational(t, true)

	t.Logf("CONVERSATIONAL flag OFF: subFire=%d synth=%d action.respond=%d", offFire, offSynth, offResp)
	t.Logf("CONVERSATIONAL flag ON : subFire=%d synth=%d action.respond=%d", onFire, onSynth, onResp)

	// The faculty must NOT synthesise a workflow on a conversational (non-task) turn — that wasted synth is
	// the regression's root. With RecognizeShape gating it, ON must not add any synth over OFF.
	if onSynth != offSynth {
		t.Fatalf("flag ON synthesised a workflow on a CONVERSATIONAL turn (synth ON=%d OFF=%d) — the faculty must "+
			"be a no-op on a non-task-shaped goal (the regression's root: a wasted SynthesizeProgram call)", onSynth, offSynth)
	}
	// And the conversational turn must STILL be answered (the normal awake social-deliver path is intact).
	if onResp == 0 {
		t.Fatalf("flag ON: a conversational turn was never answered offline (action.respond=0) — the normal awake deliver path regressed")
	}
}
