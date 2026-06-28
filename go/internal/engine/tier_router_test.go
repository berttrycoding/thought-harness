package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// newTieredEngine constructs a real Engine over a TIERED backend (a claude-shaped primary+utility,
// built offline so it never dials out) with the tier_router knob set, returning the engine and an
// event capture. This is the engine-level WIRING fixture: NewEngine installs (or does not install) the
// router via wireTierRouter, exactly as the live loop does. The routing.tier event fires at the route
// DECISION (before the downstream model call), so the wiring is observable without a reachable model.
func newTieredEngine(t *testing.T, routerOn bool) (*Engine, *[]events.Event) {
	t.Helper()
	// NewClaudeCode returns a *llm.TieredBackend (primary sonnet + utility haiku) — a real tiered
	// backend with the bridge transport; we never dial it in this wiring test (we assert the route
	// DECISION/event, which precedes the model call).
	tb := llm.NewClaudeCode(llm.ClaudeCodeOptions{Model: "sonnet", UtilityModel: "haiku"})
	if _, ok := tb.(*llm.TieredBackend); !ok {
		t.Fatalf("expected NewClaudeCode to return a *TieredBackend (primary+utility), got %T", tb)
	}

	feat := config.AllOn()
	feat.Subconscious.TierRouter = routerOn

	cfg := EngineConfig{Mode: "reactive", Seed: 7, MaxTicks: 4, Cognition: "control", Features: &feat}
	e, err := NewEngine(&cfg, tb)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var evs []events.Event
	e.Bus().Subscribe(func(ev events.Event) { evs = append(evs, ev) })
	return e, &evs
}

// longSummarizeThoughts builds a thought slice whose joined summarize prompt exceeds the 2000-rune
// long-prompt routing threshold, so route.Flag(Utility) trips and the value policy escalates.
func longSummarizeThoughts() []types.Thought {
	long := make([]types.Thought, 0, 12)
	for i := 0; i < 12; i++ {
		long = append(long, types.Thought{Text: "a deliberately long thought sentence padded out with many words so that the combined " +
			"summarize prompt comfortably exceeds the two-thousand-rune long-prompt routing threshold once a dozen of them are joined together end to end"})
	}
	return long
}

// TestEngineWiresTierRouterWhenOn: the WIRING GATE. With subconscious.tier_router ON and a TIERED
// backend, NewEngine installs the router (wireTierRouter), so a flagged-HARD CONTENT call on the
// engine-constructed backend emits a routing.tier event with an escalation. This proves the feature is
// on the LIVE loop's backend, not just unit-testable in isolation.
func TestEngineWiresTierRouterWhenOn(t *testing.T) {
	e, evs := newTieredEngine(t, true)
	tb := e.Backend().(*llm.TieredBackend)

	// a flagged-HARD summarize (a long prompt) — the engine-installed router escalates it to primary
	// and emits routing.tier BEFORE the (un-dialled) model call. We do not assert the model answer;
	// the route DECISION + event is the wiring proof.
	tb.Summarize(longSummarizeThoughts())

	var route *events.Event
	for i := range *evs {
		if (*evs)[i].Kind == events.RoutingTier {
			route = &(*evs)[i]
			break
		}
	}
	if route == nil {
		t.Fatal("router ON: a routing.tier event must fire from the engine-wired backend (the wiring-gate proof)")
	}
	// The engine installs the THOMPSON bandit policy (not the deterministic ValuePolicy the unit tests
	// use), so the chosen tier depends on the (seeded, cold-start) posterior — what the WIRING test
	// proves is that the engine-installed router ENGAGED on a flagged call: the event fired, the floor
	// was the per-role utility floor, the policy is the bandit, and the call was flagged-eligible.
	// (The escalate/downgrade/structural OUTCOMES are asserted deterministically in the llm + route unit
	// tests.) The chosen tier is one of the two real tiers.
	if route.Data["floor_tier"] != "utility" {
		t.Fatalf("hard summarize floor should be utility, got %v", route.Data["floor_tier"])
	}
	if route.Data["policy"] != "thompson-bandit" {
		t.Fatalf("the engine must install the thompson-bandit policy, got %v", route.Data["policy"])
	}
	if route.Data["flagged"] != true {
		t.Fatalf("a hard summarize must be flagged escalation-eligible, got flagged=%v", route.Data["flagged"])
	}
	if tier := route.Data["tier"]; tier != "primary" && tier != "utility" {
		t.Fatalf("routed tier must be one of the real tiers, got %v", tier)
	}
}

// TestEngineTierRouterOffByteIdentical: with the knob OFF (the default), NewEngine installs NO router,
// so the backend's per-role FLOOR decides and NO routing.tier event fires from any CONTENT call. The
// byte-identical-flag-OFF wiring proof at the engine layer.
func TestEngineTierRouterOffByteIdentical(t *testing.T) {
	e, evs := newTieredEngine(t, false)
	tb := e.Backend().(*llm.TieredBackend)

	tb.Generate("g", nil, nil)
	tb.Summarize(longSummarizeThoughts()) // even a "hard" summarize emits nothing when the router is off

	for _, ev := range *evs {
		if ev.Kind == events.RoutingTier {
			t.Fatal("router OFF must emit NO routing.tier event (byte-identical goldens)")
		}
	}
}

// TestEngineTierRouterNoOpOnTestBackend: the knob ON but the backend is the TEST DOUBLE (no tiers) —
// wireTierRouter is a no-op (the test double has no Utility tier), so nothing is installed and the
// offline/golden path is untouched by construction. Proven by the absence of any routing.tier event
// across a full reactive episode.
func TestEngineTierRouterNoOpOnTestBackend(t *testing.T) {
	feat := config.AllOn()
	feat.Subconscious.TierRouter = true // ON, but the test double below has no second tier
	cfg := EngineConfig{Mode: "reactive", Seed: 7, MaxTicks: 8, Cognition: "control", Features: &feat}
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var routed int
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.RoutingTier {
			routed++
		}
	})
	e.Submit("will this code run correctly?", true) // a goal so the episode actually thinks
	e.Run(8)                                        // a full reactive episode — the test double drives every CONTENT role
	if routed != 0 {
		t.Fatalf("tier_router ON with the test double (no tiers) must be a no-op: got %d routing.tier events", routed)
	}
}
