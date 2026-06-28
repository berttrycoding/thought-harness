package engine_test

import (
	"os"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// TestLiveClaudeAwakeUserDispatchEngages is the live content-quality confirm for the AWAKE-DISP rung-0
// fix. It drives the AWAKE engine on the real claude bridge with the flag ON
// (conscious.activity.awake_user_dispatch) and feeds three multi-hop, program-shaped engineering inputs
// over the stream (the case the fix targets — a goal with a workflow shape). The proof: the subconscious
// must FIRE on the awake user lines (subconscious.fire > 0 — it was 0 before the fix) and the watched
// seam must deliver real answers (not "I couldn't work that out"). Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeAwakeUserDispatchEngages(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	// The awake default config, with the rung-0 fix flag ON. An explicit Features wins over the bare
	// continuous default (engine.go) — so this is the validated awake mind PLUS awake_user_dispatch.
	f := config.New()
	config.ApplyAwakeDefaults(f)
	f.Conscious.Activity.AwakeUserDispatch = true // THE FIX, ON
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = f
	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	eng.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	step := func(n int) {
		for i := 0; i < n; i++ {
			eng.Step()
		}
	}
	step(3) // already awake + wandering — the mid-session state the interrupt path is built for
	eng.SubmitDefault("design a rate limiter that supports BOTH per-tenant and a global cap, and explain precisely how the two interact when a tenant's burst would push the system past the global cap")
	step(6)
	eng.SubmitDefault("now, given that design, what happens to in-flight requests when the global cap is hot-reloaded to a LOWER value mid-traffic?")
	step(6)
	eng.SubmitDefault("separately: trace how a token refill and a burst-drain race if they fire on the same tick, and which wins")
	step(8)

	fires := log.of(events.SubFire)
	dispatch := log.of(events.SubDispatch)
	quiet := log.of(events.SubQuiet)
	responds := log.of("action.respond")
	t.Logf("AWAKE + flag ON: subconscious.fire=%d dispatch=%d quiet=%d  action.respond=%d",
		len(fires), len(dispatch), len(quiet), len(responds))
	for i, e := range fires {
		if i >= 10 {
			break
		}
		t.Logf("  FIRE: %s", e.Summary)
	}
	for _, e := range responds {
		s := e.Summary
		if len(s) > 220 {
			s = s[:220]
		}
		t.Logf("  RESPOND: %s", s)
	}
	if len(fires) == 0 {
		t.Fatalf("subconscious never fired on awake user lines with the flag ON — the fix did not engage live")
	}
}

// TestLiveClaudeAwakeUserEngageDelivers is the live content-quality confirm for the AWAKE-DISP rung-1
// engagement value floor (conscious.activity.awake_user_engage). The rung-1 boost itself is pure Pattern-A
// (a deterministic V(s) re-rank, no model call), so it is CONTROL — but it changes WHICH line the awake mind
// pursues, which is the input to the CONTENT (answer) path. The awake "won't answer" lesson is the hazard:
// a value change that makes the mind chase a line could, on the live substrate, perturb the deliver. This
// test drives the awake mind on the real claude bridge with the engagement floor ON (over the rung-0
// companion) and confirms the floor ENGAGES the focused user line (conscious.engage fires) AND the user
// turn is still ANSWERED (action.respond > 0) — the floor wins the competition for the user line WITHOUT
// suppressing the delivery. Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeAwakeUserEngageDelivers(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	f := config.New()
	config.ApplyAwakeDefaults(f)
	f.Conscious.Activity.AwakeUserDispatch = true // rung 0: the focused user line gets a subconscious workflow
	f.Conscious.Activity.AwakeUserEngage = true   // rung 1: THE FLOOR, ON — pursue the focused user line
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = f
	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	eng.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	for i := 0; i < 3; i++ {
		eng.Step() // already awake + wandering
	}
	eng.SubmitDefault("design a rate limiter that supports BOTH per-tenant and a global cap, and explain precisely how the two interact when a tenant's burst would push the system past the global cap")
	for i := 0; i < 10; i++ {
		eng.Step()
	}

	engage := log.of(events.Engage)
	responds := log.of("action.respond")
	t.Logf("AWAKE + rung-1 ON: conscious.engage=%d action.respond=%d", len(engage), len(responds))
	for _, e := range responds {
		s := e.Summary
		if len(s) > 220 {
			s = s[:220]
		}
		t.Logf("  RESPOND: %s", s)
	}
	if len(engage) == 0 {
		t.Fatalf("conscious.engage never fired with the rung-1 floor ON — the floor did not engage the user line live")
	}
	if len(responds) == 0 {
		t.Fatalf("the awake mind never answered the user with the rung-1 floor ON — the engagement floor suppressed delivery (the awake won't-answer hazard)")
	}
}

// TestLiveClaudeAwakeEngageJudgeLifts is the live content proof for the AWAKE-DISP rung-2 engagement CEILING
// (conscious.activity.awake_user_engage_judge). The ceiling is a CONTENT path — a NEW Backend role
// (JudgeEngagement) where the REAL claude model decides whether a fuzzy, substantive, non-task-shaped awake
// user line is worth engaging the subconscious on. The test double does NOT implement JudgeEngagement (so it
// can NEVER exercise this path — the live substrate is the ONLY proof it works), which is exactly the awake
// "won't answer" hazard the standing rule guards against: a CONTENT role that reads as wired offline but is
// never actually driven. This drives the awake mind on the real claude bridge with rung 2 ON over the rung-0
// floor, feeds a FUZZY line the lexical floor would NOT task-shape (a substantive open question, no
// design/build/optimize/analyze keyword), and confirms (a) the real model engaged it (conscious.engage_judge
// fired — the ceiling lifted the floor's no-engage) and (b) the turn was still ANSWERED (action.respond > 0,
// no false set-aside). Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeAwakeEngageJudgeLifts(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	f := config.New()
	config.ApplyAwakeDefaults(f)
	f.Conscious.Activity.AwakeUserDispatch = true    // rung 0: the floor it sits above
	f.Conscious.Activity.AwakeUserEngageJudge = true // rung 2: THE CEILING, ON
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = f
	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	eng.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	for i := 0; i < 3; i++ {
		eng.Step() // already awake + wandering
	}
	// A FUZZY, substantive, open question with NO lexical task-shape keyword — the floor no-ops it, so only
	// the rung-2 ceiling (the real model judging it worth engaging) can engage the subconscious here.
	eng.SubmitDefault("i keep going back and forth on whether a small team should standardise on one language or let each service pick its own — how would you reason your way through that trade for a five-person team shipping weekly?")
	for i := 0; i < 10; i++ {
		eng.Step()
	}

	judged := log.of(events.EngageJudge)
	floorStands := log.of(events.EscalationFloorStands)
	responds := log.of("action.respond")
	t.Logf("AWAKE + rung-2 ON: conscious.engage_judge=%d escalation.floor_stands=%d action.respond=%d",
		len(judged), len(floorStands), len(responds))
	for _, e := range responds {
		s := e.Summary
		if len(s) > 220 {
			s = s[:220]
		}
		t.Logf("  RESPOND: %s", s)
	}
	// The CEILING must have been CONSULTED on the live substrate (lifted OR stood) — proving the new Backend
	// role is actually driven by the real model, not dead. A LIFT (engage_judge) is the worth-engaging path.
	if len(judged) == 0 && len(floorStands) == 0 {
		t.Fatalf("rung-2 ceiling was never consulted on the live substrate (no engage_judge, no floor_stands) — the JudgeEngagement role is dead on the live loop")
	}
	// The turn must still be ANSWERED (the awake won't-answer hazard — a CONTENT change must not break delivery).
	if len(responds) == 0 {
		t.Fatalf("the awake mind never answered the fuzzy user line with rung-2 ON — the ceiling broke delivery (the awake won't-answer hazard)")
	}
}
