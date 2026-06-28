package llm

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/route"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// recordingTier builds an OpenAICompatBackend whose transport records every call's model id and
// returns a canned non-empty completion — so a test can SEE which tier a routed call actually reached
// (the proof the dispatch RUNS, not just that the route logic is correct). The returned slice pointer
// accumulates the model ids the transport saw.
func recordingTier(model string, seen *[]string) *OpenAICompatBackend {
	b := NewOpenAICompat(Options{BaseURL: "test://" + model, Model: model})
	b.transport = func(reqBody map[string]any, _ bool) (postResult, error) {
		m, _ := reqBody["model"].(string)
		*seen = append(*seen, m)
		return postResult{content: "answer-from-" + m, finish: "stop",
			reasoningTokens: -1, promptTokens: -1, completionTokens: -1,
			totalTokens: -1, cachedInputTokens: -1, cacheMissTokens: -1}, nil
	}
	return b
}

// newRoutedTiered builds a TieredBackend with two recording tiers + an installed router, and returns
// it alongside the per-tier call logs and the emitted routing.tier events.
func newRoutedTiered(t *testing.T, enabled bool, policy route.RoutePolicy) (*TieredBackend, *[]string, *[]string, *[]events.Event) {
	t.Helper()
	primarySeen, utilitySeen := &[]string{}, &[]string{}
	tb := NewTiered(recordingTier("primary-model", primarySeen), recordingTier("utility-model", utilitySeen))
	var evs []events.Event
	tb.BindEmit(func(kind, summary string, data map[string]any) events.Event {
		e := events.Event{Kind: kind, Summary: summary, Data: data}
		evs = append(evs, e)
		return e
	})
	tb.SetTierRouter(route.NewRouter(enabled, policy, true))
	return tb, primarySeen, utilitySeen, &evs
}

func routingEvents(evs []events.Event) []events.Event {
	var out []events.Event
	for _, e := range evs {
		if e.Kind == events.RoutingTier {
			out = append(out, e)
		}
	}
	return out
}

// TestTieredFloorDispatchByteIdentical: with the router OFF, the floor decides — every reasoning role
// reaches the PRIMARY tier, summarize reaches the UTILITY tier (exactly the pre-router dispatch), and
// NO routing.tier event fires. This is the byte-identical-flag-OFF wiring proof at the dispatch layer.
func TestTieredFloorDispatchByteIdentical(t *testing.T) {
	tb, primarySeen, utilitySeen, evs := newRoutedTiered(t, false, route.NewThompsonPolicy(cpyrand.New(1)))
	rng := cpyrand.New(1)

	tb.Generate("g", nil, rng)
	tb.Transform(types.Candidate{Text: "x", Source: types.GENERATED}, nil)
	tb.Respond("g", nil)
	tb.OperatorApply("deliberator", "do x", "intent", "logic", "g", nil)
	tb.Summarize([]types.Thought{{Text: "a"}})

	// 4 reasoning roles -> primary; 1 summarize -> utility (the historical split, unchanged).
	if len(*primarySeen) != 4 {
		t.Fatalf("router OFF: primary tier should serve the 4 reasoning roles, got %d: %v", len(*primarySeen), *primarySeen)
	}
	if len(*utilitySeen) != 1 {
		t.Fatalf("router OFF: utility tier should serve the 1 summarize, got %d: %v", len(*utilitySeen), *utilitySeen)
	}
	if re := routingEvents(*evs); len(re) != 0 {
		t.Fatalf("router OFF must emit NO routing.tier event (goldens hold), got %d", len(re))
	}
}

// TestTieredFlaggedHardSummarizeEscalatesToPrimary: with the router ON and a value policy, a
// flagged-HARD summarize (a long prompt trips both the flag and the policy's long-prompt rule) is
// dispatched to the PRIMARY tier — and the routing.tier event records the escalation. This is the
// real CONTENT-method path (Summarize), so it proves the WIRING, not just the route logic.
func TestTieredFlaggedHardSummarizeEscalatesToPrimary(t *testing.T) {
	tb, primarySeen, utilitySeen, evs := newRoutedTiered(t, true, route.NewValuePolicy())
	// build a LONG summarize prompt so route.Flag(Utility) trips on PromptLen (>=2000) and the value
	// policy escalates (PromptLen >= LongPrompt). PromptSummarize joins the last ~12 thoughts, so each
	// must be long enough that 12 of them clear the threshold.
	long := make([]types.Thought, 0, 12)
	for i := 0; i < 12; i++ {
		long = append(long, types.Thought{Text: "a deliberately long thought sentence padded out with many words so that the combined " +
			"summarize prompt comfortably exceeds the two-thousand-rune long-prompt routing threshold once a dozen of them are joined together end to end"})
	}
	tb.Summarize(long)

	if len(*primarySeen) != 1 || len(*utilitySeen) != 0 {
		t.Fatalf("hard summarize should ESCALATE to primary; primary=%v utility=%v", *primarySeen, *utilitySeen)
	}
	re := routingEvents(*evs)
	if len(re) != 1 {
		t.Fatalf("expected exactly 1 routing.tier event, got %d", len(re))
	}
	d := re[0].Data
	if d["tier"] != "primary" || d["floor_tier"] != "utility" || d["reason"] != "escalated" || d["flagged"] != true {
		t.Fatalf("routing event = %v, want tier=primary floor=utility reason=escalated flagged=true", d)
	}
}

// TestTieredFlaggedEasyDowngradesToUtility: a flagged-EASY primary-floored call (low V(s)) under a
// value policy is dispatched to the UTILITY tier — the cost-saving downgrade. Driven through the
// internal route+dispatch seam (routeDispatchForTest) with a low value, since the public CONTENT
// methods pass value=0 in v1 (threading live V(s) is the documented next step); this asserts the
// dispatch HONOURS a downgrade decision.
func TestTieredFlaggedEasyDowngradesToUtility(t *testing.T) {
	tb, primarySeen, utilitySeen, evs := newRoutedTiered(t, true, route.NewValuePolicy())
	// an easy operator call (low value) — floor=primary, flagged-easy, value policy downgrades.
	be := tb.routeDispatchForTest("operator.deliberator", 0.1, "short system", "short user")
	if be != tb.Utility {
		t.Fatalf("easy operator should DOWNGRADE to the utility tier")
	}
	// dispatch a real call through the chosen tier so the wiring (chosen-tier model call) is exercised.
	be.OperatorApply("deliberator", "do x", "i", "logic", "g", nil)
	if len(*utilitySeen) != 1 || len(*primarySeen) != 0 {
		t.Fatalf("the downgraded call must reach the utility tier; primary=%v utility=%v", *primarySeen, *utilitySeen)
	}
	re := routingEvents(*evs)
	if len(re) != 1 || re[0].Data["reason"] != "downgraded" || re[0].Data["tier"] != "utility" {
		t.Fatalf("expected a downgraded routing.tier event to utility, got %v", re)
	}
}

// TestTieredRespondNeverDowngraded: respond is structurally pinned to primary even under a policy that
// always wants utility — the wiring honours the structural-protection rule (a cheap user-facing answer
// is a visible quality miss). The floor stands and the event surfaces the structural reason (Rule 4).
func TestTieredRespondNeverDowngraded(t *testing.T) {
	tb, _, _, evs := newRoutedTiered(t, true, &alwaysUtilityPolicy{})
	be := tb.routeDispatchForTest("action.respond", 0.05, "s", "u") // flagged-easy respond
	if be != tb.Primary {
		t.Fatalf("respond MUST stay on the primary tier (structural pin)")
	}
	re := routingEvents(*evs)
	if len(re) != 1 || re[0].Data["reason"] != "structural" || re[0].Data["tier"] != "primary" {
		t.Fatalf("respond route should be structural/primary, got %v", re)
	}
}

// alwaysUtilityPolicy is a test policy that always proposes the utility tier (to exercise the
// downgrade + structural-pin paths deterministically without depending on the bandit's sampling).
type alwaysUtilityPolicy struct{}

func (alwaysUtilityPolicy) Route(route.Tier, route.Signal) (route.Tier, float64) {
	return route.Utility, 0.9
}
func (alwaysUtilityPolicy) Update(route.Tier, route.Signal, float64) {}
func (alwaysUtilityPolicy) Name() string                             { return "always-utility" }
