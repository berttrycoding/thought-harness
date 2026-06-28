package llm

// tier_router.go is the WIRING of the cost-aware substrate TIER router (internal/route) into the
// TieredBackend — the one learned scheduler component the RL/ML scaling research found is justified
// today (docs/internal/notes/2026-06-20-rl-ml-scheduler-scaling-research.md §4/§5 Scenario C, RouteLLM-class).
//
// The TieredBackend is the ONLY place that owns BOTH tiers (Primary + Utility) and sees every CONTENT
// call, so the router lives here. Per routable CONTENT call the backend: builds a cheap difficulty
// Signal (role + prompt length + an optional V(s) value), asks the route.Router for a Decision (the
// deterministic per-role FLOOR + the optional learned CEILING), emits a routing.tier event (Pattern-C
// Rule 4 — the decision is never silent when the router is engaged), and dispatches the call to the
// chosen tier's *OpenAICompatBackend.
//
// FLAG-GATED + DEFAULT-OFF + DETERMINISM-PRESERVING. With subconscious.tier_router OFF the router is
// nil-or-disabled, so route.Router.Decide returns the FLOOR for every call: every reasoning role ->
// Primary, summarize/compress -> Utility — exactly the pre-router TieredBackend behaviour (Generate/
// Transform/Respond/OperatorApply were promoted from the embedded Primary; only Summarize overrode to
// Utility). So with the flag OFF the dispatch is byte-identical, no routing.tier event fires, and the
// goldens are untouched. The router only ENGAGES on a TieredBackend (the claude bridge / a local
// primary+utility); a bare OpenAICompatBackend has no second tier and never reaches this file.
//
// Determinism: the only non-determinism source is the Thompson policy's Beta sampling, which draws
// from a seeded *cpyrand.Random threaded in at construction (SetTierRouter) — never the wall clock,
// never unseeded randomness — so a route decision is reproducible.
//
// Durability note (research §6 / the task constraint): the route changes WHICH model answers a CONTENT
// call, NOT the branching plant — it adds no excitation source, no fan-out, no new tick work; the
// regulator measures excitation (Fired+Baseline) and fan-out, neither of which the router touches. So
// the tier-router does NOT affect the durability conditions (n<1, U<=1, 0<K*g<2, mu>0, bounded
// fan-out). The continuous-mode-operator re-pass confirms this.

import (
	"unicode/utf8"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/route"
)

// SetTierRouter installs the cost-aware tier router on the TieredBackend (the wiring the engine calls
// when subconscious.tier_router is ON). enabled is the flag gate; policy is the swappable CEILING
// (route.NewValuePolicy / route.NewThompsonPolicy, or nil for floor-only). A nil/disabled router is
// the default — the floor decides, byte-identical. utilityWired is always true on a TieredBackend with
// a real utility tier (which is the only case this is called for). Idempotent.
func (t *TieredBackend) SetTierRouter(r *route.Router) { t.router = r }

// route picks the tier for a CONTENT call and emits the routing.tier event when the router is engaged.
// It returns the *OpenAICompatBackend to dispatch to. With the router OFF/nil it returns the FLOOR's
// backend silently (Primary for reasoning roles, Utility for summarize) — byte-identical to the
// pre-router dispatch, no event.
//
// role is the chat role tag (op-tail matched); value is the optional V(s) difficulty estimate (0 when
// the caller does not have it threaded — the policy then leans on role + prompt length, the RouteLLM
// input features); system/user are the prompts (their combined rune length is the cheap complexity
// proxy).
func (t *TieredBackend) route(role string, value float64, system, user string) *OpenAICompatBackend {
	sig := route.Signal{
		Role:      role,
		Value:     value,
		PromptLen: utf8.RuneCountInString(system) + utf8.RuneCountInString(user),
	}
	d := t.router.Decide(sig) // nil-safe: a nil *route.Router returns the floor decision

	// Emit ONLY when the router is actually engaged (the flag is ON). With the router OFF the floor is
	// the silent decision and NO routing.tier event fires (goldens hold). Rule 4: when engaged, EVERY
	// decision is surfaced — floor-stands, escalated, downgraded, structural — never silent.
	if t.router.Enabled() && t.emit != nil {
		t.emit(events.RoutingTier,
			"route ["+role+"] -> "+d.Tier.String()+" ("+string(d.Reason)+
				", floor="+d.FloorTier.String()+")",
			events.D{
				"role":       op(role),
				"tier":       d.Tier.String(),
				"floor_tier": d.FloorTier.String(),
				"reason":     string(d.Reason),
				"flagged":    d.Flagged,
				"value":      value,
				"prompt_len": sig.PromptLen,
				"policy":     t.router.PolicyName(),
				"confidence": d.Confidence,
			})
	}
	if d.Tier == route.Utility {
		return t.Utility
	}
	return t.Primary
}

// routeDispatchForTest exposes the route+emit decision for a chosen role/value/prompt to the package
// tests (the same seam a future V(s)-threading caller will use): it returns the *OpenAICompatBackend
// the call would dispatch to AND fires the routing.tier event. It is the value-aware variant of the
// per-method route() calls (which pass value=0 in v1), so a test can exercise the downgrade /
// structural-pin paths that need a non-zero difficulty value.
func (t *TieredBackend) routeDispatchForTest(role string, value float64, system, user string) *OpenAICompatBackend {
	return t.route(role, value, system, user)
}

// op is the operation tail of a role tag (the same tail-match route.FloorTier / the scheduler use),
// kept local so the event payload reports the bare operation.
func op(role string) string {
	for i := len(role) - 1; i >= 0; i-- {
		if role[i] == '.' {
			return role[i+1:]
		}
	}
	return role
}
