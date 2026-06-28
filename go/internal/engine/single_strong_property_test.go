package engine_test

import (
	"os"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// newSingleStrongEngine builds a reactive engine with subconscious.single_strong_agent flipped to the
// requested state (everything else all-on) on the test double, with the event log subscribed. The flag is
// opt-in ⇒ default OFF even on the all-on baseline, so `single=false` is the full-fan-out (harness) path.
func newSingleStrongEngine(t *testing.T, single bool) (*engine.Engine, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.New() // AllOn (single_strong_agent is opt-in ⇒ default OFF even here)
	feat.Subconscious.SingleStrongAgent = single
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// firedThisEpisode returns the max count of specialists that fired on any single subconscious.dispatch tick
// across the episode (the widest fan-out the engine produced this run) — the size of the multi-agent "team"
// the single-strong guard collapses.
func maxDispatchFanout(log *eventLog) int {
	max := 0
	for _, ev := range log.of(string(events.SubDispatch)) {
		if n, ok := intData(ev, "count"); ok && n > max {
			max = n
		}
	}
	return max
}

// TestSingleStrongFiresLive is the WIRING-GATE proof (the saved lesson: a flag SET but never consulted on
// the live tick passes the unit tests yet is dead on the loop — and the BENCH-SUITE-A2 residue exists
// precisely because the SubAgents/MaxParWidth config fields were NOT consumed, so the guard's two arms were
// IDENTICAL engines). With subconscious.single_strong_agent ON, the subconscious.single_strong event MUST
// fire on the engine's ACTUAL tick across a real reactive episode whenever a multi-member team is collapsed
// — proving the collapse is the LIVE dispatch path, not a dead field. With it OFF (the default), the event
// must NEVER appear (the full fan-out is byte-identical).
func TestSingleStrongFiresLive(t *testing.T) {
	// a goal that lights several faculties at once (a safety/change shape AND an arithmetic shape AND a
	// social opener) so a tick presents a MULTI-specialist field to collapse.
	const goal = "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through."

	on, onLog := newSingleStrongEngine(t, true)
	on.SubmitDefault(goal)
	on.Run(30)

	// the accessor confirms the engine wired the flag onto the subconscious engine (read side of the wire).
	if !on.Subconscious().SingleStrong() {
		t.Fatal("the engine did not wire single_strong_agent onto the subconscious engine (SingleStrong()==false with the flag ON)")
	}
	collapses := onLog.of(string(events.SubSingleStrong))
	if len(collapses) == 0 {
		t.Fatal("single_strong_agent ON: subconscious.single_strong must fire on a multi-member tick — " +
			"the collapse is NOT on the live tick (dead wire), or no tick ever fired >1 specialist (re-pick the goal)")
	}
	// The event must carry the guard payload the design promises: how many fired, which one was kept, how
	// many teammates were dropped. Observability IS the contract.
	first := collapses[0]
	for _, key := range []string{"fired", "kept", "dropped"} {
		if _, ok := first.Data[key]; !ok {
			t.Fatalf("subconscious.single_strong missing required key %q (payload: %v)", key, first.Data)
		}
	}
	fired, _ := intData(first, "fired")
	dropped, _ := intData(first, "dropped")
	if fired < 2 {
		t.Fatalf("single_strong fired=%d: the collapse must only fire when >1 specialist was admitted (a real team)", fired)
	}
	if dropped != fired-1 {
		t.Fatalf("single_strong dropped=%d but fired=%d (dropped must equal fired-1 — exactly one survivor)", dropped, fired)
	}

	off, offLog := newSingleStrongEngine(t, false)
	off.SubmitDefault(goal)
	off.Run(30)
	if len(offLog.of(string(events.SubSingleStrong))) != 0 {
		t.Fatal("single_strong_agent OFF: subconscious.single_strong must NEVER fire (the full fan-out must be byte-identical)")
	}
	if off.Subconscious().SingleStrong() {
		t.Fatal("single_strong_agent OFF: SingleStrong() must be false (the wire must be inert)")
	}
}

// TestSingleStrongCollapsesTheTeamToItsBestMember is the COGNITION property (not plumbing), and the LOAD-
// BEARING claim of the BENCH-SUITE-A2 residue: the single-strong arm and the full-harness arm must be
// genuinely NON-IDENTICAL engines, so the teams-vs-best-member guard's A/B is two different plants. On the
// live loop the full harness fans a multi-specialist team out to the gate; the single-strong arm collapses
// that team to its single best member — STRICTLY FEWER candidates reach the gate on at least one tick, and
// the survivor is the strongest fired specialist (the "best member"). A mechanical "does the loop run" test
// passes straight through this; what we assert is that the collapse actually CHANGED what the engine fed
// downstream (the failure mode this build closes: a flag that changes nothing, so the guard is vacuous).
func TestSingleStrongCollapsesTheTeamToItsBestMember(t *testing.T) {
	const goal = "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through."

	// FULL harness: confirm a real multi-member team actually fanned out (else the guard has nothing to
	// collapse and the comparison is vacuous — surface that as a fail, not a silent skip).
	full, fullLog := newSingleStrongEngine(t, false)
	full.SubmitDefault(goal)
	full.Run(30)
	teamWidth := maxDispatchFanout(fullLog)
	if teamWidth < 2 {
		t.Fatalf("the full harness never fanned out a multi-member team (max dispatch fan-out = %d) — "+
			"the single-strong guard has nothing to collapse; the goal must light >1 faculty on a tick", teamWidth)
	}

	// SINGLE-STRONG: the collapse must drive the dispatched fan-out down — at least one tick that fired a
	// team in the full run must now feed exactly ONE candidate to the gate (a strictly different engine).
	single, singleLog := newSingleStrongEngine(t, true)
	single.SubmitDefault(goal)
	single.Run(30)

	collapses := singleLog.of(string(events.SubSingleStrong))
	if len(collapses) == 0 {
		t.Fatal("single-strong arm produced no collapse on the same multi-team goal — the arms are IDENTICAL " +
			"engines (the BENCH-SUITE-A2 bug); the guard would compare a plant against itself")
	}

	// the collapse is real: every collapse this episode kept exactly ONE member and dropped the rest, and the
	// kept member carries a domain (it is a genuine fired specialist, the best of the team — not an empty
	// stand-in).
	for _, ev := range collapses {
		fired, _ := intData(ev, "fired")
		dropped, _ := intData(ev, "dropped")
		kept, _ := ev.Data["kept"].(string)
		if fired < 2 || dropped != fired-1 {
			t.Fatalf("collapse payload malformed: fired=%d dropped=%d (want fired>=2, dropped=fired-1)", fired, dropped)
		}
		if kept == "" {
			t.Fatal("collapse kept an empty domain — the surviving candidate is not a real fired specialist (best member)")
		}
	}

	// the LOAD-BEARING non-identity: the single-strong engine's dispatched fan-out is strictly narrower than
	// the full harness's on its widest tick (the team was reduced to its best member). After a collapse a
	// single_strong tick feeds exactly one candidate to the gate, so the subconscious.dispatch count on a
	// collapsed tick must be 1 — strictly fewer than the full harness's team width.
	collapsedTickHadOne := false
	for _, ev := range singleLog.of(string(events.SubDispatch)) {
		if n, ok := intData(ev, "count"); ok && n == 1 {
			collapsedTickHadOne = true
			break
		}
	}
	if !collapsedTickHadOne {
		t.Fatal("after the single-strong collapse, no dispatch tick fed exactly ONE candidate to the gate — " +
			"the collapse did not actually narrow the fired set the engine feeds downstream (the guard's arms " +
			"would still be effectively identical)")
	}
	singleWidth := maxDispatchFanout(singleLog)
	if singleWidth >= teamWidth {
		t.Fatalf("single-strong max fan-out %d is not strictly narrower than the full harness's %d — the "+
			"collapse did not reduce the team (the two guard arms are NOT distinct engines)", singleWidth, teamWidth)
	}
}

// TestLiveClaudeSingleStrongStillAnswers is the gated live-substrate proof that collapsing the sub-agent
// fan-out to its single best member does NOT break the harness's ability to produce a grounded answer on the
// real model — the single-strong arm is a degraded-but-functional engine, not a broken one (a CONTENT path:
// the surviving best member still drives a real Respond, so the test double — whose canned Respond would
// answer regardless — is necessary-but-not-sufficient). It runs a real reactive episode on live claude with
// single_strong ON and asserts (a) the collapse actually fired on the live loop and (b) a non-empty answer
// still came out. Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes); it COMPILES + SKIPS offline so
// the normal suite stays deterministic.
func TestLiveClaudeSingleStrongStillAnswers(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate validation (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.New()
	feat.Subconscious.SingleStrongAgent = true // the path under test
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	goal := "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through."
	e.SubmitDefault(goal)
	for i := 0; i < 40 && e.LastResponse() == ""; i++ {
		e.Step()
	}

	if len(log.of(string(events.SubSingleStrong))) == 0 {
		t.Fatal("single-strong collapse never fired on the live claude loop — the path under test was not exercised " +
			"(re-pick a goal that lights a multi-member team)")
	}
	resp := e.LastResponse()
	t.Logf("LIVE single-strong response: %s", resp)
	if resp == "" {
		t.Fatal("no response produced on the live substrate with single_strong ON — collapsing the team to its " +
			"best member broke the harness's ability to answer (a real delivery bug the test double would mask)")
	}
}
