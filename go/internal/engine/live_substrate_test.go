package engine_test

// live_substrate_test.go — the STANDARD way to validate a CONTENT / cognition change on the REAL
// model substrate (the claude bridge), not just the offline test double.
//
// WHY THIS EXISTS. The test double (`backends.NewTest()`, used by newSeededEngine) returns canned,
// deterministic strings for the CONTENT roles — e.g. TestBackend.Respond answers a greeting from a
// HARDCODED line. That is correct for fast, offline, deterministic iteration, but it MASKS whether the
// live model path actually delivers: a change can look "answered" against the double while the real
// substrate path — the thing under test — is broken. That is exactly how the awake "won't answer" bug
// slipped through (the double answered a greeting at tick 1 from its canned line, hiding that the live
// continuous loop is the path that had no deliver guarantee). So: the double is necessary but NOT
// sufficient. A behavior/cognition claim is only confirmed once it runs on the live substrate here.
//
// These tests are GATED behind THOUGHT_LIVE_CLAUDE=1 (each Step spawns `claude -p` per CONTENT call —
// real tokens + minutes), so the normal `go test ./...` suite stays offline + deterministic. Run them
// as the definition-of-done check for any CONTENT-path change:
//
//	THOUGHT_LIVE_CLAUDE=1 go test ./internal/engine -run TestLiveClaude -v -timeout 900s

import (
	"os"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// newLiveEngine builds an engine on the LIVE claude bridge at the given mode (the real-substrate twin
// of newSeededEngine, which uses the test double). It SKIPS the test unless THOUGHT_LIVE_CLAUDE=1, so
// the live path is opt-in. Use it to write a live variant of any behavior test: same shape as
// newSeededEngine, but the CONTENT roles are real model calls instead of canned strings.
func newLiveEngine(t *testing.T, mode string, seed int) (*engine.Engine, *eventLog) {
	t.Helper()
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = mode
	cfg.Seed = seed
	e, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// TestLiveClaudeAwakeConversation is the canonical real-substrate check for the awake conversation
// contract: the continuous engine is ALREADY running (mid-wander, graph != nil), THEN a user turn
// arrives — so it enters through the PERCEPTION-PORT interrupt path (OnInterrupt), the field scenario
// the test double cannot exercise. The turn MUST get a real reply across the watched seam within the
// awake user-deliver deadline, with no false "set aside — unanswered" stamp, and the graph must no
// longer derive a waiting user. This is the test that would have CAUGHT the awake "won't answer" bug.
func TestLiveClaudeAwakeConversation(t *testing.T) {
	eng, log := newLiveEngine(t, "continuous", 7)

	for i := 0; i < 2; i++ { // already awake and wandering (the user's mid-session state)
		eng.Step()
	}
	eng.SubmitDefault("hi i am here to ask you some questions") // speaks mid-wander -> percept interrupt

	before := len(respondsOf(log))
	answered := -1
	for i := 0; i < 9 && answered < 0; i++ {
		eng.Step()
		if len(respondsOf(log)) > before {
			answered = i
		}
	}

	for _, r := range respondsOf(log) {
		t.Logf("RESPOND: %s", r.Summary)
	}
	asides := 0
	for _, d := range log.of(events.Decision) {
		if a, _ := d.Data["set_aside"].(bool); a {
			asides++
		}
	}
	t.Logf("ticks-after-submit-to-answer=%d  responds=%d  true-set-asides=%d  userWaiting=%v",
		answered, len(respondsOf(log)), asides, eng.UserWaiting())

	if answered < 0 {
		t.Fatalf("a mid-wander user turn was NEVER answered on the live substrate in 9 ticks "+
			"(%d set-aside stamps, %d responds) — the awake 'won't answer' bug", asides, len(respondsOf(log)))
	}
	if eng.UserWaiting() {
		t.Fatalf("answered (tick %d) but the graph still derives a waiting user — zombie pending state", answered)
	}
}

// TestLiveClaudeSufficiencyJudge is the A-RAG1 CONTENT-path live proof (the test double cannot exercise
// the SufficiencyJudge ceiling — it does not implement it, so the gate resolves to the floor offline).
// It calls the REAL claude SufficiencyJudge ceiling directly and asserts it DISCRIMINATES coverage: a
// recall that genuinely covers the need reads "sufficient"; an off-topic recall reads "insufficient" (the
// model is willing to ABSTAIN). This is the structural abstention the THOUGHT_GROUND_COMPLETE prompt-fix
// could not deliver — proven on the live substrate, not the canned double.
func TestLiveClaudeSufficiencyJudge(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	judge, ok := be.(backends.SufficiencyJudge)
	if !ok {
		t.Fatalf("the claude backend must satisfy backends.SufficiencyJudge (the A-RAG1 ceiling); it does not")
	}

	const need = "What is the capital of France and where is it located?"

	// a covering recall -> the model should judge it sufficient.
	covV, covOK := judge.JudgeSufficiency(need,
		"Paris is the capital of France; it sits on the river Seine in north-central France.", "ambiguous")
	t.Logf("covering recall -> %q (ok=%v)", covV, covOK)
	if !covOK {
		t.Fatal("the live ceiling declined on a clean covering case — the CONTENT path did not deliver")
	}
	if covV == "insufficient" {
		t.Fatalf("the live model judged a clearly-covering recall %q — false abstention (over-cautious)", covV)
	}

	// an off-topic recall -> the model should judge it insufficient (willing to ABSTAIN).
	offV, offOK := judge.JudgeSufficiency(need,
		"The recipe needs flour, butter, sugar, and three large eggs baked at 180 degrees.", "ambiguous")
	t.Logf("off-topic recall -> %q (ok=%v)", offV, offOK)
	if !offOK {
		t.Fatal("the live ceiling declined on the off-topic case — the CONTENT path did not deliver")
	}
	if offV == "sufficient" {
		t.Fatalf("the live model judged an OFF-TOPIC recall sufficient — it would over-commit a hollow recall "+
			"(the abstention paradox the gate exists to break), got %q", offV)
	}
}

// TestLiveClaudeSelfBenchShadowDelivers is the live-substrate definition-of-done for the SB0 self-bench
// primitive (Track H): the SHADOW engine runs REAL claude episodes per probe, so the suite scores the
// checkpoint's ACTUAL model-driven behaviour — not the test double's canned answers. The double is
// necessary but NOT sufficient: its hardcoded arithmetic/greeting answers can MASK a delivery bug in the
// shadow content path (the same failure mode as the awake "won't answer" bug). This proves the shadow
// engine genuinely THINKS on the live substrate: every passing cell carries a real, non-canned answer,
// and the loop is propose-and-gate (it measures, never self-commits). Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeSelfBenchShadowDelivers(t *testing.T) {
	eng, _ := newLiveEngine(t, "reactive", 7)

	rep := eng.SelfBench(persist.Snapshot{}, "ck-live", engine.SeedSelfBenchSuite())

	for _, c := range rep.Cells {
		t.Logf("CELL %s pass=%v val=%.2f answer=%q reason=%q", c.Probe, c.Pass, c.Value, c.Answer, c.Reason)
	}
	t.Logf("self-bench(live): passed=%d/%d score=%.2f disposition=%q committed=%v",
		rep.Passed, rep.Total, rep.Score, rep.Disposition, rep.Committed)

	if rep.Total != 3 {
		t.Fatalf("live self-bench did not run the whole suite: total=%d (want 3)", rep.Total)
	}
	// The shadow engine must have DELIVERED on the live substrate — at least one probe scored, and every
	// passing cell carries a real (non-empty) answer. A live shadow path that never delivered (the bug
	// the double would mask) scores 0 / carries empty answers.
	if rep.Passed == 0 {
		t.Fatalf("live self-bench scored 0 — the SHADOW engine did not deliver on claude (the masked-delivery bug)")
	}
	for _, c := range rep.Cells {
		if c.Pass && strings.TrimSpace(c.Answer) == "" {
			t.Fatalf("live self-bench cell %q passed with an EMPTY answer — the shadow content path did not deliver", c.Probe)
		}
	}
	if rep.Disposition != "propose" || rep.Committed {
		t.Fatalf("live self-bench must be propose-and-gate: disposition=%q committed=%v", rep.Disposition, rep.Committed)
	}
}

// TestLiveClaudeActiveResource is the A-RAG4 CONTENT definition-of-done on the REAL substrate: with
// controller.active_resource ON, a goal the harness has GROUNDED FUEL for (a knowledge fact) but whose
// line goes uncertain (low V(s)) must trigger an ACTIVE RE-SOURCE — the Controller re-invokes the
// sourcing ladder (critic.resource_trigger + subconscious.source), and the grounded fact is folded back
// into the stream. Crucially this is the CONTENT path the test double MASKS: the double's canned answers
// keep V high so the precondition rarely arises offline, whereas a real model on a genuinely hard goal
// produces the low-V(s) stalls the trigger is FOR. The bound (at most one re-source per branch) must
// hold on the real loop too. Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeActiveResource(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.AllOn()
	feat.Controller.ActiveResource = true // A-RAG4 ON
	cfg.Features = &feat
	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	eng.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	// a goal-relevant grounded fact in the registry — the ladder has something real to re-source.
	eng.Knowledge().Record(knowledge.Knowledge{
		Statement: "the token bucket refills at a constant rate r tokens/sec up to capacity b; a burst can drain b tokens",
		Kind:      "fact", Entities: []string{"token", "bucket", "refill", "rate", "limiter"},
		Source: "ingest:test", Grounded: true, Trust: 0.85,
	})

	// a genuinely under-specified goal — the model is likely to stall (low V(s)) on a goal-relevant line.
	eng.SubmitDefault("explain precisely how a token-bucket rate limiter refills and what happens during a sustained burst over capacity")
	for i := 0; i < 16; i++ {
		eng.Step()
	}

	triggers := log.of(events.ResourceTrigger)
	for _, ev := range triggers {
		t.Logf("RESOURCE_TRIGGER: %v", ev.Data)
	}
	// the bound MUST hold on the real loop: at most one re-source per branch.
	perBranch := map[int]int{}
	for _, ev := range triggers {
		if b, ok := ev.Data["branch"].(int); ok {
			perBranch[b]++
		}
	}
	for b, n := range perBranch {
		if n > 1 {
			t.Fatalf("branch %d re-sourced %d times on the live loop — the once-per-branch bound was violated", b, n)
		}
	}
	// NOTE: we do not HARD-assert a trigger fires (a frontier model may answer the goal confidently before
	// any line stalls — a legitimate no-trigger run). The hard contract is the BOUND + no crash on the real
	// substrate; the log lines above are the manual-inspection evidence that the re-source fired + the
	// ladder ran when the model did stall. If zero triggers across runs, harden the goal.
	t.Logf("live A-RAG4: %d resource triggers, %d subconscious.source events", len(triggers), len(log.of(events.SubSource)))
}

// TestLiveClaudeInboxEscalationBoundedOnRealStream is the live-substrate proof for the O-5 async inbox
// push channel (conscious.activity.inbox_escalation). The escalation LOGIC is pure CONTROL (no model
// call — cooldown / cap / acknowledgement), but it rides the awake outreach stream whose content IS
// model-authored, so this runs the WHOLE channel on the real substrate: proactive outreach (the base
// content push) + inbox escalation (the re-surface) ON, a user that seeds a topic then goes silent, the
// awake loop driven on claude. The load-bearing assertions are the DURABILITY properties (the part that
// must hold on a real, non-canned stream): a re-surface NEVER exceeds the cap (it does not spam), and the
// awake stream stays stable (n<1) while the channel is active. If a base outreach does fire, the channel
// re-surfaces it within the bound; if none fires in the budget, the run still proves the channel is wired
// and does not destabilize the live stream (it does not require an outreach to fire to pass — that is
// non-deterministic on a real model — only that the bound + stability hold).
func TestLiveClaudeInboxEscalationBoundedOnRealStream(t *testing.T) {
	eng, log := newLiveEngine(t, "continuous", 7)
	f := eng.Features()
	f.Conscious.Activity.ProactiveOutreach = true // wake-path transcript + base outreach (the content push)
	f.Conscious.Activity.InboxEscalation = true   // the O-5 re-surface layer under test

	eng.SubmitDefault("i'm thinking about how to make our auth refactor safer") // seed a topic, then go silent
	for i := 0; i < 40; i++ {                                                   // long enough to clear 2+ escalation cooldowns
		eng.Step()
	}

	escalations := 0
	maxSeen := 0
	for _, ev := range log.of(events.InboxEscalate) {
		escalations++
		if esc, ok := ev.Data["escalation"].(int); ok && esc > maxSeen {
			maxSeen = esc
		}
		t.Logf("INBOX-ESCALATE: %s (escalation=%v)", ev.Summary, ev.Data["escalation"])
	}
	t.Logf("live: %d outreach(es), %d escalation(s), max-escalation=%d, n=%.3f",
		len(log.of(events.Respond)), escalations, maxSeen, eng.Regulator().N())

	// DURABILITY BOUND (must hold on the real stream): never re-surface beyond the cap.
	if maxSeen > 2 {
		t.Fatalf("live: an escalation reached count %d > cap 2 — the durability bound failed on the real substrate (the channel spams)", maxSeen)
	}
	// STABILITY (must hold while the channel is active): the awake stream stays subcritical.
	if eng.Regulator().N() >= 1.0 {
		t.Fatalf("live: the async push channel destabilized the awake stream (n=%.3f >= 1)", eng.Regulator().N())
	}
}

// TestLiveClaudeSelfModelGroundsWhatItIs is the SELF-MODEL definition-of-done on the REAL substrate (board
// SELF-MODEL; preagi-levels-roadmap §1.5). It proves the fix for the live-observed gap: in awake CHAT the
// harness fell back to the bare model's "I'm an LLM, I can't see my cwd" prior because the conscious stream
// was never TOLD what it is. With sense.self_model ON, the awake mind grounds a STANDING CORE self-model
// (read from the real registries + a real workspace), so when the user asks "what are you / what can you do
// / where are you running" it answers with a GROUNDED self-description — its harness identity + architecture
// + real tools + the actual cwd — NOT "I'm an LLM". This is the CONTENT path the test double MASKS (its
// canned Respond would answer any greeting from a hardcoded line, hiding whether the grounded self-model
// actually reaches the reply). Gated behind THOUGHT_LIVE_CLAUDE=1.
func TestLiveClaudeSelfModelGroundsWhatItIs(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate tests (claude bridge — costs tokens + time)")
	}
	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	// Explicit awake features: the validated awake stack (seed-intent portfolio plants the standing
	// INTROSPECTIVE root + the faculty scheduler gives it focus turns) PLUS sense.self_model ON. A real
	// WORKSPACE so the self-model reads a genuine cwd + real tools (the "where am I running" ground truth).
	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Sense.SelfModel = true
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Workspace = "." // the real reality-facing dir — the self-model grounds the actual cwd + real tools
	cfg.Features = feat
	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	eng.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	// Run the awake loop until the standing introspective root grounds the self-model (a few ticks for the
	// faculty scheduler to resume it).
	grounded := false
	for i := 0; i < 25 && !grounded; i++ {
		eng.Step()
		if len(log.of(events.PerceptionSelfModel)) > 0 {
			grounded = true
		}
	}
	if !grounded {
		t.Fatal("the awake mind never grounded a perception.self_model in 25 ticks — the standing core did not enter the stream on the live substrate")
	}
	sm := log.of(events.PerceptionSelfModel)[0]
	t.Logf("SELF-MODEL grounded: specialists=%v operators=%v tools=%v mode=%v substrate=%v cwd=%v",
		sm.Data["specialists"], sm.Data["operators"], sm.Data["tools"], sm.Data["mode"], sm.Data["substrate"], sm.Data["cwd"])

	// Now ASK the mind what it is — mid-wander, so the turn enters through the perception-port interrupt
	// path (the field scenario) with the grounded self-model already in the stream's context.
	eng.SubmitDefault("what are you, what can you do, and where are you running?")
	before := len(respondsOf(log))
	answered := -1
	for i := 0; i < 9 && answered < 0; i++ {
		eng.Step()
		if len(respondsOf(log)) > before {
			answered = i
		}
	}
	if answered < 0 {
		t.Fatalf("the mind never answered 'what are you' on the live substrate in 9 ticks (the awake won't-answer path)")
	}
	reply := strings.ToLower(eng.LastResponse())
	t.Logf("REPLY: %s", eng.LastResponse())

	// GROUNDED self-description: the reply must reflect the harness self-model — identity/architecture +
	// real capabilities — NOT the bare-model "I'm an LLM" fallback. We require evidence of the GROUNDED
	// self-knowledge the self-model injected (the harness identity OR its layered architecture OR its real
	// tools/registries OR the actual cwd), and we FAIL the bare-model fallback.
	identity := strings.Contains(reply, "harness") || strings.Contains(reply, "silent-injection") ||
		strings.Contains(reply, "three layer") || strings.Contains(reply, "subconscious") ||
		strings.Contains(reply, "thought graph") || strings.Contains(reply, "thought-harness")
	capability := strings.Contains(reply, "specialist") || strings.Contains(reply, "operator") ||
		strings.Contains(reply, "tool") || strings.Contains(reply, "thought graph")
	grounding := strings.Contains(reply, "cwd") || strings.Contains(reply, "directory") ||
		strings.Contains(reply, "workspace") || strings.Contains(reply, "/")

	if !identity {
		t.Fatalf("the mind did NOT name its harness IDENTITY/architecture — it fell back to the bare-model self-image (the gap the self-model fixes). reply=%q", eng.LastResponse())
	}
	if !capability && !grounding {
		t.Fatalf("the mind named its identity but described NO real capability or runtime grounding (tools/specialists/operators/cwd) — the grounded self-model did not reach the reply. reply=%q", eng.LastResponse())
	}
	// The bare-model anti-pattern: a reply that ONLY says "I'm an AI/LLM" and names no harness self-knowledge
	// is exactly the fallback the self-model exists to replace.
	if (strings.Contains(reply, "i am an ai") || strings.Contains(reply, "i'm an ai") ||
		strings.Contains(reply, "i am an llm") || strings.Contains(reply, "language model")) && !identity {
		t.Fatalf("the mind answered with the bare-model 'I'm an LLM' self-image and no harness self-knowledge: %q", eng.LastResponse())
	}
}
