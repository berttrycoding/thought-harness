package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/critic"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// verifyanswer_test.go is the COGNITION + WIRE test for T2.1, the INDEPENDENT answer-verifier
// (critic.answer_verify). It asserts the THINKING the gate intends — a committed answer the world does NOT
// corroborate is REFUSED (driven to continue), a corroborated answer COMMITS, and the unverifiable / web-
// blind / flag-OFF paths fall through byte-identically — not merely that the loop runs. It drives the real
// engine override (verifyAnswerDecision) on a controlled graph state, against an injected web seam (a
// query-conditioned fake — NEVER the live network), so it is offline + deterministic.

// answerVerifyEngine builds a heuristic engine (test double) for the answer-verify tests, with the
// critic.answer_verify knob set as requested. With web!=nil it wires the conditioned seam.
func answerVerifyEngine(t *testing.T, on bool, seam web.Web) *Engine {
	t.Helper()
	feat := config.New() // reactive default == AllOn
	feat.Controller.AnswerVerify = on
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if seam != nil {
		e.SetWeb(seam)
	}
	return e
}

// vCondWeb is a query-conditioned web seam: it returns evidence chosen by whether a substring appears in
// the query. The production web.Fake returns a fixed snippet regardless of query, which cannot model a
// supported-vs-unsupported re-retrieval, so the tests use this.
type vCondWeb struct {
	contains string     // substring that selects the hit result
	hit      web.Result // returned when the query contains `contains`
	miss     web.Result // returned otherwise
}

func (w vCondWeb) Fetch(query string) web.Result {
	if w.contains != "" && strings.Contains(strings.ToLower(query), strings.ToLower(w.contains)) {
		return w.hit
	}
	return w.miss
}

// commitState arranges the engine's graph + Controller so that DecideNext produces a GOAL_MET commit on a
// confident INJECTED answer tip (the precondition verifyAnswerDecision keys on). It returns the floor
// decision the Controller decided (STOP / DELIVER). This mirrors TestControllerDecisionSpine's goal-met arm.
func commitState(t *testing.T, e *Engine, goal, answer string) types.Decision {
	t.Helper()
	e.graph = graph.New(goal)
	e.graph.Append(&types.Thought{Text: answer, Source: types.INJECTED, Confidence: 0.85}, 0)
	if !e.controller.GoalSatisfied(e.graph) {
		t.Fatalf("precondition: a confident INJECTED answer %q should satisfy goal %q", answer, goal)
	}
	d := e.controller.DecideNext(e.graph, critic.DefaultDecideOptions())
	if d != types.STOP && d != types.DELIVER {
		t.Fatalf("precondition: a goal-met commit should be STOP/DELIVER, got %v", d)
	}
	return d
}

// TestAnswerVerifyUnsupportedRefusesCommit is the canonical COGNITION property: an answer the INDEPENDENT
// re-retrieved evidence does NOT corroborate (a wrong claim) must NOT commit — the override downgrades the
// GOAL_MET commit to THINK so the line keeps working. This is the same-model-ceiling break: an independent
// signal refutes an answer the model's own re-read would have committed.
func TestAnswerVerifyUnsupportedRefusesCommit(t *testing.T) {
	// The world says the founder is "Grace Hopper"; the harness is about to commit "Zorblatt Penguintron".
	seam := vCondWeb{miss: web.Result{Text: "Acme Corp was founded by Grace Hopper in 1842.", OK: true, Source: "fake"}}
	e := answerVerifyEngine(t, true, seam)
	var verdict, overridden string
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			verdict, _ = ev.Data["verdict"].(string)
			if o, _ := ev.Data["overridden"].(bool); o {
				overridden = "yes"
			}
		}
	})
	floor := commitState(t, e, "Who founded Acme Corp?", "Zorblatt Penguintron")

	got := e.verifyAnswerDecision(floor)
	if got != types.THINK {
		t.Fatalf("an UNSUPPORTED committed answer must be refused (downgraded to THINK), got %v", got)
	}
	if verdict != "unsupported" {
		t.Fatalf("the gate must judge the answer unsupported, got verdict=%q", verdict)
	}
	if overridden != "yes" {
		t.Fatal("the critic.answer_verify event must mark the commit overridden")
	}
}

// TestAnswerVerifySupportedCommits: an answer the INDEPENDENT evidence corroborates COMMITS — the floor
// decision (STOP/DELIVER) stands. The verifier admits a world-backed answer (it never blocks a supported
// commit).
func TestAnswerVerifySupportedCommits(t *testing.T) {
	seam := vCondWeb{miss: web.Result{Text: "Acme Corp was founded by Ada Lovelace in 1842.", OK: true, Source: "fake"}}
	e := answerVerifyEngine(t, true, seam)
	var verdict string
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			verdict, _ = ev.Data["verdict"].(string)
		}
	})
	floor := commitState(t, e, "Who founded Acme Corp?", "Ada Lovelace")

	got := e.verifyAnswerDecision(floor)
	if got != floor {
		t.Fatalf("a SUPPORTED committed answer must commit (floor %v stands), got %v", floor, got)
	}
	if verdict != "supported" {
		t.Fatalf("the gate must judge the answer supported, got verdict=%q", verdict)
	}
}

// TestAnswerVerifyOffByteIdentical: with critic.answer_verify OFF (the default), the override is a NO-OP —
// the floor decision stands verbatim, no fetch, no critic.answer_verify event — even on an answer the world
// would refute. This is the flag-OFF byte-identity guarantee.
func TestAnswerVerifyOffByteIdentical(t *testing.T) {
	seam := vCondWeb{miss: web.Result{Text: "Acme Corp was founded by Grace Hopper.", OK: true, Source: "fake"}}
	e := answerVerifyEngine(t, false, seam) // flag OFF
	fired := false
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			fired = true
		}
	})
	floor := commitState(t, e, "Who founded Acme Corp?", "Zorblatt Penguintron") // a refutable answer

	got := e.verifyAnswerDecision(floor)
	if got != floor {
		t.Fatalf("flag OFF must be a no-op: floor %v must stand, got %v", floor, got)
	}
	if fired {
		t.Fatal("flag OFF must emit NO critic.answer_verify event (byte-identical)")
	}
}

// TestAnswerVerifyWebBlindNoOp: with the flag ON but NO web seam wired (web-blind), every answer is
// Unverifiable ⇒ the commit always stands ⇒ byte-identical. The flag alone never moves a web-blind run.
func TestAnswerVerifyWebBlindNoOp(t *testing.T) {
	e := answerVerifyEngine(t, true, nil) // flag ON, web-blind
	var verdict string
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			verdict, _ = ev.Data["verdict"].(string)
		}
	})
	floor := commitState(t, e, "Who founded Acme Corp?", "Zorblatt Penguintron")

	got := e.verifyAnswerDecision(floor)
	if got != floor {
		t.Fatalf("web-blind must be a no-op: floor %v must stand, got %v", floor, got)
	}
	if verdict != "unverifiable" {
		t.Fatalf("web-blind ⇒ Unverifiable, got verdict=%q", verdict)
	}
}

// TestAnswerVerifyTestBackendFloorStands: on the offline test double (--backend test), the backend declines
// the AnswerSupportJudge ceiling — so the DETERMINISTIC FLOOR alone decides. A fuzzy-band answer (the floor
// is Unverifiable, the model would be consulted) commits because the test double has no ceiling: the floor
// stands, offline + deterministic. This is the Pattern-C floor-stands guarantee at the engine level.
func TestAnswerVerifyTestBackendFloorStands(t *testing.T) {
	// Evidence shares ONE of the two answer content tokens ⇒ floor Unverifiable (the fuzzy band). With no
	// ceiling (test double), Unverifiable ⇒ the commit stands.
	seam := vCondWeb{miss: web.Result{Text: "The article mentions Lovelace.", OK: true, Source: "fake"}}
	e := answerVerifyEngine(t, true, seam)
	// Sanity: the test double must NOT implement the ceiling (else this asserts the wrong thing).
	if _, ok := e.backend.(backends.AnswerSupportJudge); ok {
		t.Fatal("the test double must NOT implement AnswerSupportJudge (the floor must stand offline)")
	}
	var verdict, floorVerdict string
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			verdict, _ = ev.Data["verdict"].(string)
			floorVerdict, _ = ev.Data["floor_verdict"].(string)
		}
	})
	floor := commitState(t, e, "Who founded Acme?", "Ada Lovelace")

	got := e.verifyAnswerDecision(floor)
	if got != floor {
		t.Fatalf("the fuzzy-band floor must STAND offline (no ceiling): floor %v must commit, got %v", floor, got)
	}
	if floorVerdict != "unverifiable" || verdict != "unverifiable" {
		t.Fatalf("the deterministic floor must decide (no escalation): floor=%q verdict=%q", floorVerdict, verdict)
	}
}

// TestAnswerVerifyGiveUpNotVerified: a GIVE_UP close (not a confident answer commit) is NOT verified — the
// verifier targets only GOAL_MET answer commits. A give-up / over-budget / wander close stands untouched.
func TestAnswerVerifyGiveUpNotVerified(t *testing.T) {
	seam := vCondWeb{miss: web.Result{Text: "nothing relevant", OK: true}}
	e := answerVerifyEngine(t, true, seam)
	fired := false
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			fired = true
		}
	})
	// Force a GIVE_UP terminal: a long, low-confidence line over budget (not a confident answer).
	e.graph = graph.New("a hard question")
	for i := 0; i < 18; i++ {
		e.graph.Append(&types.Thought{Text: "grinding " + itoa(i), Source: types.GENERATED, Confidence: 0.3}, i)
	}
	d := e.controller.DecideNext(e.graph, critic.DefaultDecideOptions())
	got := e.verifyAnswerDecision(d)
	if got != d {
		t.Fatalf("a non-GOAL_MET close must not be verified: %v must stand, got %v", d, got)
	}
	if fired {
		t.Fatal("a give-up / non-answer-commit must emit NO critic.answer_verify event")
	}
}

// TestAnswerVerifyBoundedOncePerBranch: the verification fires AT MOST ONCE per branch (the bound that keeps
// it from looping). A second call on the same branch is a no-op (the floor decision stands) and emits no
// second event — the durability bound (no new fan-out, no loop).
func TestAnswerVerifyBoundedOncePerBranch(t *testing.T) {
	seam := vCondWeb{miss: web.Result{Text: "Acme was founded by Ada Lovelace.", OK: true, Source: "fake"}}
	e := answerVerifyEngine(t, true, seam)
	fires := 0
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.AnswerVerify {
			fires++
		}
	})
	floor := commitState(t, e, "Who founded Acme?", "Ada Lovelace")

	e.verifyAnswerDecision(floor)        // fires once
	got := e.verifyAnswerDecision(floor) // bounded: second call on the same branch is a no-op
	if got != floor {
		t.Fatalf("a re-verify on the same branch must be a no-op (floor %v stands), got %v", floor, got)
	}
	if fires != 1 {
		t.Fatalf("the verification must fire at most ONCE per branch (bounded), fired %d times", fires)
	}
}

// TestAnswerVerifyWiredOnLiveLoop is the WIRE test (tests-pass != feature-runs): it drives a REAL reactive
// episode end-to-end via Run — NOT a direct call to the override — and asserts the override is actually
// CONSULTED on the live tick (a critic.answer_verify event fires) when the episode reaches a GOAL_MET answer
// commit. It uses an arithmetic goal because the test double naturally answers it confidently to GOAL_MET;
// the answer is computational, so the verifier correctly NO-OPs (Unverifiable — compute grounding owns it),
// which proves BOTH that the wire fires AND that the computational guard holds on the live path. With the
// flag OFF the SAME episode emits NO event (the live byte-identity arm).
func TestAnswerVerifyWiredOnLiveLoop(t *testing.T) {
	run := func(on bool) (fired int, sawGoalMet bool) {
		e := answerVerifyEngine(t, on, web.NewFake())
		e.Bus().Subscribe(func(ev events.Event) {
			switch ev.Kind {
			case events.AnswerVerify:
				fired++
			case events.Decision:
				if d, _ := ev.Data["decision"].(string); d == "DELIVER" || d == "STOP" {
					if sk, _ := ev.Data["stop_kind"].(string); sk == "GOAL_MET" {
						sawGoalMet = true
					}
				}
			}
		})
		e.Submit("What is 7 times 8?", true)
		e.Run(30)
		return fired, sawGoalMet
	}

	firedOn, goalMet := run(true)
	if !goalMet {
		t.Fatal("precondition: the arithmetic episode should reach a GOAL_MET commit (the verifier's trigger)")
	}
	if firedOn != 1 {
		t.Fatalf("flag ON: the override must be CONSULTED once on the live answer-commit tick, fired %d", firedOn)
	}
	firedOff, _ := run(false)
	if firedOff != 0 {
		t.Fatalf("flag OFF: the SAME live episode must emit NO critic.answer_verify event (byte-identical), fired %d", firedOff)
	}
}

// TestAnswerVerifyDeterministic: the same commit state + same injected seam ⇒ the same override decision
// every call (the determinism contract the goldens depend on). Fresh engine per call to clear the bound.
func TestAnswerVerifyDeterministic(t *testing.T) {
	seam := vCondWeb{miss: web.Result{Text: "Acme Corp was founded by Grace Hopper.", OK: true, Source: "fake"}}
	var first types.Decision
	for i := 0; i < 5; i++ {
		e := answerVerifyEngine(t, true, seam)
		floor := commitState(t, e, "Who founded Acme Corp?", "Zorblatt Penguintron")
		got := e.verifyAnswerDecision(floor)
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("verifyAnswerDecision not deterministic: call %d = %v, first = %v", i, got, first)
		}
	}
	if first != types.THINK {
		t.Fatalf("the refutable answer should deterministically refuse (THINK), got %v", first)
	}
}
