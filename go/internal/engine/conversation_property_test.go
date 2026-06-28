package engine_test

// conversation_property_test.go — the SOCIAL CONTRACT of the awake mind (the simplest user-visible
// properties, untested until 2026-06-12 when a live session found the awake mind permanently mute).
//
// Canon: spec §4.12 "Hearing is input; speaking is effectful" (responding = a watched-seam action),
// §4.12 interrupt policy (being addressed re-seeds VALUE toward the user's goal), §5.6 (the
// maintenance drive resumes unfinished high-value lines). The reply must EMERGE from those mechanisms
// — value pressure + the Controller's decision spine — never from a hardcoded reply path.
//
// Product decision (2026-06-12): silence must be legible (heard / thinking / set aside), and a direct
// user question in awake mode ends with the answer actually crossing the watched seam.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// respondsOf returns the direct conversational replies (action.respond, kind=respond) — outreach
// (kind=outreach) is the mind speaking unprompted and does NOT count as answering the user.
func respondsOf(log *eventLog) []events.Event {
	var out []events.Event
	for _, e := range log.of(events.Respond) {
		if k, _ := e.Data["kind"].(string); k == "respond" {
			out = append(out, e)
		}
	}
	return out
}

// The awake mind ANSWERS when spoken to: a direct user question lands mid-wander, and within a
// generous tick budget the answer crosses the watched seam as a real conversational respond. The
// stream must also SURVIVE answering (the mind keeps living afterwards — answering is not stopping).
func TestAwakeMindAnswersWhenSpokenTo(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 8; i++ { // let it wander first — the turn must interrupt real ongoing thought
		eng.Step()
	}

	eng.SubmitDefault("what is 12 * 7?")
	answered := -1
	for i := 0; i < 40; i++ {
		eng.Step()
		if len(respondsOf(log)) > 0 {
			answered = i
			break
		}
	}
	if answered < 0 {
		t.Fatalf("the awake mind never answered a direct question (40 ticks): %d outreach, %d responds — the mute gap",
			len(log.of(events.Respond)), len(respondsOf(log)))
	}

	// answering must not kill the awake stream: it keeps thinking afterwards.
	before := len(eng.Graph().History())
	for i := 0; i < 12; i++ {
		eng.Step()
	}
	if after := len(eng.Graph().History()); after <= before {
		t.Fatalf("the stream died after answering (history %d -> %d)", before, after)
	}
	if eng.Regulator().N() >= 1.0 {
		t.Fatalf("answering destabilized the stream: n=%.3f", eng.Regulator().N())
	}
}

// A SECOND turn mid-wander is also answered: after replying once the mind wanders again, and a new
// user turn recovers focus and gets its own reply (no one-shot wedge, no sticky pending state).
func TestAwakeMindAnswersSecondTurnAfterWandering(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 6; i++ {
		eng.Step()
	}

	eng.SubmitDefault("what is 3 + 4?")
	for i := 0; i < 40 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
	}
	if len(respondsOf(log)) == 0 {
		t.Fatal("first turn never answered — see TestAwakeMindAnswersWhenSpokenTo")
	}

	for i := 0; i < 10; i++ { // wander between turns
		eng.Step()
	}

	eng.SubmitDefault("what is 9 * 9?")
	for i := 0; i < 40 && len(respondsOf(log)) < 2; i++ {
		eng.Step()
	}
	if got := len(respondsOf(log)); got < 2 {
		t.Fatalf("second turn never answered (%d replies total) — the mind wedged after one exchange", got)
	}
}

// Being addressed is ACKNOWLEDGED structurally (the eye-contact analogue): the salient user turn
// raises the interrupt that suspends-and-focuses a line for it. This is the signal the surface
// decodes into "heard — thinking on it", so silence is legible. (Expected to PASS today — pins the
// mechanism the presence layer builds on.)
func TestAwakeUserTurnIsAcknowledgedByInterrupt(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 6; i++ {
		eng.Step()
	}
	before := len(log.of(events.Interrupt))
	eng.SubmitDefault("are you there?")
	for i := 0; i < 4 && len(log.of(events.Interrupt)) == before; i++ {
		eng.Step()
	}
	if len(log.of(events.Interrupt)) == before {
		t.Fatal("a salient user turn raised no interrupt — the mind never even 'looked up'")
	}
	// the turn entered the mind: the stream holds the USER_INPUT thought. (The focused line moves ON
	// quickly now — the social faculty answers in ~1 tick and the mind resumes wandering — so asserting
	// the ACTIVE context still holds the turn would race the very speed the fix delivers.)
	found := false
	for _, th := range eng.Graph().History() {
		if th.Source == types.USER_INPUT && th.Text == "are you there?" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the user's turn never entered the thought stream")
	}
}

// §4.12 sustained value re-seed (mandate 2026-06-12 A1): from the moment a user turn lands until
// the answer crosses the watched seam, every branch holding the unanswered USER_INPUT stays
// resume-worthy through every V(s) recompute — and DELIVERY is what resolves it, graph-visibly.
// Before A1 the re-seed was a one-tick blip: OnInterrupt wrote Value=1.0, the next value.Update
// clobbered it back to the bootstrap priors.
func TestUserValuePressureHoldsUntilDelivered(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 6; i++ {
		eng.Step()
	}

	eng.SubmitDefault("what is 12 * 7?")
	sawPending := false
	for i := 0; i < 40 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
		if len(respondsOf(log)) > 0 {
			break // answered on this tick — post-delivery state is asserted below
		}
		g := eng.Graph()
		for bid, b := range g.Branches {
			if !g.UnresolvedUserInput(bid) {
				continue
			}
			sawPending = true
			if b.Value < 0.5 {
				t.Fatalf("tick %d: unanswered user line b%d sank to %.3f — the value pressure did not hold",
					i, bid, b.Value)
			}
		}
	}
	if len(respondsOf(log)) == 0 {
		t.Fatal("the question was never answered — cannot pin the resolution half")
	}

	// delivery resolved the line IN THE GRAPH: no branch still reports an unanswered user turn.
	g := eng.Graph()
	for bid := range g.Branches {
		if g.UnresolvedUserInput(bid) {
			t.Fatalf("after the reply, b%d still reports an unresolved user line — delivery never marked the graph", bid)
		}
	}
	_ = sawPending // zero pending ticks just means the answer landed same-tick; the value-pkg test pins survival
}

// A3 + §5.6 (mandate 2026-06-12): a user question that DEMANDS ground truth is not abandoned by
// the wandering mind. The line may be set aside mid-thought (branch/verify/backtrack are legal),
// but its standing value (the A1 pending term) must bring the mind BACK — resume, ACT for ground
// truth, and answer. Before A3 the top-of-tick 'satisfied -> mind moves on' preemption opened
// fresh wander lines forever and the parked user line rotted at the top of the frontier (S16).
func TestAwakeGroundTruthQuestionIsEventuallyAnswered(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 8; i++ { // wandering first — the question lands mid-thought
		eng.Step()
	}
	eng.SubmitDefault("is this refactor safe to ship?")
	answered := -1
	for i := 0; i < 60 && answered < 0; i++ {
		eng.Step()
		if len(respondsOf(log)) > 0 {
			answered = i
		}
	}
	if answered < 0 {
		t.Fatalf("a ground-truth user question was never answered in 60 awake ticks — the mind "+
			"abandoned the user (%d outreach events, %d responds)",
			len(log.of(events.Respond))-len(respondsOf(log)), len(respondsOf(log)))
	}
	// and the answer resolved the line in the graph (no zombie pending state).
	g := eng.Graph()
	for bid := range g.Branches {
		if g.UnresolvedUserInput(bid) {
			t.Fatalf("answered, but b%d still derives an unresolved user line", bid)
		}
	}
}

// Reactive parity: the same question in reactive mode answers (the episodic special case must keep
// working while the awake general case is fixed). Expected to PASS today — a regression pin.
func TestReactiveModeStillAnswers(t *testing.T) {
	eng, log := newSeededEngine(t, "reactive", 7)
	eng.SubmitDefault("what is 12 * 7?")
	for i := 0; i < 40 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
	}
	if len(respondsOf(log)) == 0 {
		t.Fatal("reactive mode no longer answers a direct question — parity regression")
	}
}

// A4 (mandate 2026-06-12): "a user is waiting" is DERIVED from graph state — an unresolved
// USER_INPUT line exists — never a sticky bool that control flow forks on. The derivation must
// track the conversation in both regimes: true from the turn landing until delivery, false while
// the mind wanders or after answering.
func TestUserWaitingDerivedFromGraph(t *testing.T) {
	// reactive: the episode goal IS a user turn — waiting from episode start until the answer.
	eng, log := newSeededEngine(t, "reactive", 7)
	if eng.UserWaiting() {
		t.Fatal("reactive: no episode yet, but the engine says a user is waiting")
	}
	eng.SubmitDefault("what is 12 * 7?")
	eng.Step() // the episode starts: the goal is the user's words
	if len(respondsOf(log)) == 0 && !eng.UserWaiting() {
		t.Fatal("reactive: episode running on the user's question, but the graph derives no waiting user")
	}
	for i := 0; i < 40 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
	}
	if len(respondsOf(log)) == 0 {
		t.Fatal("reactive: never answered")
	}
	if eng.UserWaiting() {
		t.Fatal("reactive: answered, but the graph still derives a waiting user")
	}

	// continuous: wandering derives NO waiting user (the wander seed is the mind's own, not a turn).
	awake, alog := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 6; i++ {
		awake.Step()
		if awake.UserWaiting() {
			t.Fatalf("continuous: wandering (tick %d) but the graph derives a waiting user", i)
		}
	}
	awake.SubmitDefault("what is 3 + 4?")
	awake.Step()
	if len(respondsOf(alog)) == 0 && !awake.UserWaiting() {
		t.Fatal("continuous: a turn landed unanswered, but the graph derives no waiting user")
	}
	for i := 0; i < 40 && len(respondsOf(alog)) == 0; i++ {
		awake.Step()
	}
	if len(respondsOf(alog)) == 0 {
		t.Fatal("continuous: never answered")
	}
	if awake.UserWaiting() {
		t.Fatal("continuous: answered, but the graph still derives a waiting user")
	}
}

// A4 provenance: the graph records WHO set the episode goal. A user-seeded episode's root thought
// is USER_INPUT (their words, not the mind's own generation); the awake self-seeded wander stays
// GENERATED. Provenance is structural (which path seeded the episode), never a text-prefix
// heuristic — and the awake engine needs NO synthetic "(awake)" kickoff to start wandering.
func TestEpisodeRootCarriesUserProvenance(t *testing.T) {
	eng, _ := newSeededEngine(t, "reactive", 7)
	eng.SubmitDefault("what is 12 * 7?")
	eng.Step()
	root := eng.Graph().History()[0]
	if root.Source != types.USER_INPUT {
		t.Fatalf("reactive episode root is %v, want USER_INPUT — the user's words recorded as the mind's own", root.Source)
	}

	// the awake mind wakes itself: no submission at all, the engine self-seeds the wander.
	awake, _ := newSeededEngine(t, "continuous", 7)
	awake.Step()
	aroot := awake.Graph().History()[0]
	if aroot.Source == types.USER_INPUT {
		t.Fatalf("self-seeded wander root is %v — the mind's own seed recorded as a user turn", aroot.Source)
	}
	if awake.UserWaiting() {
		t.Fatal("self-seeded wander derives a waiting user — the kickoff lie is back")
	}
}

// A2: speaking to the user is the EXECUTIVE'S decision, visible on the wire — every conversational
// reply is preceded by a critic.decision DELIVER (never a respond manufactured by engine plumbing),
// and one turn gets exactly one reply.
func TestDeliverIsAnExecutiveDecision(t *testing.T) {
	for _, mode := range []string{"reactive", "continuous"} {
		eng, log := newSeededEngine(t, mode, 7)
		if mode == "continuous" {
			for i := 0; i < 6; i++ {
				eng.Step()
			}
		}
		eng.SubmitDefault("what is 12 * 7?")
		for i := 0; i < 40 && len(respondsOf(log)) == 0; i++ {
			eng.Step()
		}
		if len(respondsOf(log)) != 1 {
			t.Fatalf("%s: want exactly one reply, got %d", mode, len(respondsOf(log)))
		}
		delivers := 0
		for _, ev := range log.of(events.Decision) {
			if d, _ := ev.Data["decision"].(string); d == "DELIVER" {
				delivers++
			}
		}
		if delivers == 0 {
			t.Fatalf("%s: the reply crossed the seam with no DELIVER decision — speech is still plumbing, not a decision", mode)
		}
	}
}
