package engine_test

// Cognition-property tests for A-RAG4 — V(s)-triggered active re-sourcing on the LIVE engine loop.
//
// These pin the THINKING the spec intends, on the real tick (not just the Controller's decision in
// isolation, which the critic-package unit tests cover): with the flag ON, a LOW-V(s), goal-relevant
// active line makes the engine RE-INVOKE the sourcing ladder (FLARE/active-inference) and fold the
// re-sourced fact back into the conscious stream — and it does so AT MOST ONCE per branch (the bound that
// keeps the plant subcritical, so the awake durability conditions hold). With the flag OFF the loop is
// byte-identical (no trigger, no event). The wiring-gate lesson: a unit test on the decision is necessary
// but NOT sufficient — these prove the wire FIRES on the engine's actual step.
//
// Deterministic on the TestBackend test double + seed 7. Because the double's canned answers keep the
// active branch V(s) high (the CONTENT path the project warns the double MASKS), the low-V(s) precondition
// is established by disabling the rerank (value.signal) and pinning the active-branch V low + seeding a
// goal-relevant low-confidence line — a controlled state that exercises the SAME live hook the real
// substrate reaches when a line genuinely stalls. The end-to-end CONTENT proof is the gated live-claude
// test TestLiveClaudeActiveResource.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// newActiveResourceEngine builds a reactive engine on the test double with controller.active_resource ON
// and value.signal OFF (so a pinned active-branch V(s) is not overwritten by the rerank — the controlled
// low-V state). It subscribes an eventLog sink and returns both.
func newActiveResourceEngine(t *testing.T, on bool) (*engine.Engine, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.AllOn()
	feat.Controller.ActiveResource = on
	feat.Value.Signal = false // pin V(s): the rerank must not overwrite the controlled low-V active branch
	cfg.Features = &feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// stallGoalRelevantLine opens an episode, seeds the durable knowledge registry with a goal-relevant fact
// (so the re-sourcing ladder has GROUNDED fuel to return), and pins the active branch into a LOW-V(s),
// goal-relevant line (a stalled, uncertain line working on the goal). It returns the engine.
func stallGoalRelevantLine(t *testing.T, e *engine.Engine) {
	t.Helper()
	e.SubmitDefault("token bucket rate limiter refill behavior")
	e.Step() // open the episode
	// a grounded fact the ladder can return (rung 2 / knowledge) when re-sourcing the line
	e.Knowledge().Record(knowledge.Knowledge{
		Statement: "the token bucket refills at rate r tokens per second up to capacity b",
		Kind:      "fact", Entities: []string{"token", "bucket", "refill"},
		Source: "ingest:test", Grounded: true, Trust: 0.85,
	})
	g := e.Graph()
	g.Append(&types.Thought{
		ID: -1, Text: "token bucket rate limiter refill remains unclear",
		Source: types.GENERATED, Confidence: 0.05,
	}, e.Bus().Tick)
	g.Active().Value = 0.2 // low V(s): the line is not earning its keep
}

// TestActiveResourceFiresOnLiveLoop — the WIRING + THINKING proof: with the flag ON, a low-V(s),
// goal-relevant line on the live loop triggers a re-source (critic.resource_trigger fires), the sourcing
// ladder is re-invoked (subconscious.source fires), and the re-sourced grounded fact is folded back into
// the conscious stream as a "Re-sourced (...)" OBSERVATION at the source's trust.
func TestActiveResourceFiresOnLiveLoop(t *testing.T) {
	e, log := newActiveResourceEngine(t, true)
	stallGoalRelevantLine(t, e)
	g := e.Graph()
	for s := 0; s < 5; s++ {
		e.Step()
		g.Active().Value = 0.2 // hold the line low-V (rerank is off, but execute may write Value)
	}

	if len(log.of(events.ResourceTrigger)) == 0 {
		t.Fatal("a low-V(s) goal-relevant line did not trigger active re-sourcing on the live loop")
	}
	if len(log.of(events.SubSource)) == 0 {
		t.Fatal("the re-source trigger did not re-invoke the sourcing ladder (no subconscious.source)")
	}
	// the re-sourced grounded fact is voiced back into the stream as an OBSERVATION (so DecideNext +
	// the next rerank see the freshly-imported material).
	resourced := 0
	for _, tt := range g.History() {
		if strings.HasPrefix(tt.Text, "Re-sourced (") {
			resourced++
			if tt.Source != types.OBSERVATION {
				t.Fatalf("a re-sourced fact must be an OBSERVATION, got %s", tt.Source)
			}
		}
	}
	if resourced == 0 {
		t.Fatal("the re-source fired but no grounded fact was folded back into the conscious stream")
	}
}

// TestActiveResourceBoundedOnLiveLoop — the BOUND (durability): across a multi-step stalled line, the
// active re-source fires AT MOST ONCE per branch. This is the no-unbounded-loop guarantee: a low-V line
// does not re-source every tick (which would drive the plant), it re-sources once and then the Controller's
// normal decision spine takes over. A regression that dropped the per-branch marker re-fires every step.
func TestActiveResourceBoundedOnLiveLoop(t *testing.T) {
	e, log := newActiveResourceEngine(t, true)
	stallGoalRelevantLine(t, e)
	g := e.Graph()
	for s := 0; s < 10; s++ {
		e.Step()
		g.Active().Value = 0.2
	}

	// count triggers per branch — none may exceed one.
	perBranch := map[int]int{}
	for _, ev := range log.of(events.ResourceTrigger) {
		if b, ok := ev.Data["branch"].(int); ok {
			perBranch[b]++
		}
	}
	if len(perBranch) == 0 {
		t.Fatal("expected at least one re-source trigger over the stalled run")
	}
	for b, n := range perBranch {
		if n > 1 {
			t.Fatalf("branch %d re-sourced %d times — the once-per-branch bound was violated (unbounded loop)", b, n)
		}
	}
}

// TestActiveResourceOffIsByteIdentical — the OPT-IN guard on the live loop: with the flag OFF, the exact
// same stalled, low-V(s), goal-relevant line produces ZERO critic.resource_trigger events (a silent no-op),
// so the default path is byte-identical. (The scenario goldens pin the full-trace byte-identity; this pins
// the specific precondition that DOES fire when ON stays silent when OFF.)
func TestActiveResourceOffIsByteIdentical(t *testing.T) {
	e, log := newActiveResourceEngine(t, false)
	stallGoalRelevantLine(t, e)
	g := e.Graph()
	for s := 0; s < 10; s++ {
		e.Step()
		g.Active().Value = 0.2
	}

	if got := len(log.of(events.ResourceTrigger)); got != 0 {
		t.Fatalf("flag OFF must not fire active re-sourcing, got %d critic.resource_trigger events", got)
	}
}
