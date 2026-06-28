package engine

import (
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/route"
)

// tier_router.go wires the cost-aware substrate TIER router (internal/route, RouteLLM-class) into the
// live loop — the engine half of the feature (the route DECISION lives in internal/route, the per-call
// DISPATCH lives in llm.TieredBackend; this is the construction that connects them when the knob is on).
//
// docs/internal/notes/2026-06-20-rl-ml-scheduler-scaling-research.md §4/§5 Scenario C (the highest-confidence
// learned component) + docs/internal/notes/heuristic-llm-pattern-refactor.md §1 (Pattern C: floor + ceiling).
//
// ADDITIVE + FLAG-GATED + DEFAULT-OFF: with subconscious.tier_router OFF this installs nothing, the
// TieredBackend's per-role FLOOR is the silent decision (byte-identical to the pre-router dispatch),
// and no routing.tier event fires. The router only ENGAGES a TIERED LLM backend (the claude bridge / a
// local primary+utility) — a single-model or test backend has no second tier, so wireTierRouter is a
// no-op there by construction. The route changes WHICH model answers a CONTENT call, NOT the branching
// plant, so it does NOT touch the durability conditions (the regulator measures excitation + fan-out;
// the router adds neither).

// tierRouterOn reports whether the cost-aware tier router knob is enabled (the opt-in flag, default OFF).
func (e *Engine) tierRouterOn() bool {
	return e.features != nil && e.features.Subconscious.TierRouter
}

// wireTierRouter installs the tier router on the backend when the knob is ON and the backend is a
// TIERED LLM backend. It is called once at construction, after the scheduler is bound. A no-op when
// the knob is off or the backend has no utility tier.
//
// Policy choice (v1, the "flat first" discipline, research §0/§3a): a ThompsonPolicy — the cheapest
// principled CONTEXTUAL BANDIT (Beta-Bernoulli Thompson over {utility, primary} per role-class x
// difficulty-band, the LeCaR-class learner the OS reference class actually ships, NOT deep online RL).
// It samples from the engine's SEEDED RNG (a fresh stream seeded off the engine seed, so a route
// decision is reproducible and never touches the soft-policy's RNG state — two independent learners
// must not share a draw sequence). The keep-or-revert OUTER loop (critic.ExperimentWindow) is the
// documented accept/reject gate on any drift; the NEXT learnable step is to thread the engine's live
// V(s) into the route Signal (today 0 ⇒ the policy leans on role + prompt length) and to wrap Update
// in the experiment window. The ValuePolicy (a transparent stateless threshold) is the even-simpler
// alternative behind the same route.RoutePolicy interface.
func (e *Engine) wireTierRouter() {
	if !e.tierRouterOn() {
		return
	}
	tb, ok := e.backend.(*llm.TieredBackend)
	if !ok || tb.Utility == nil {
		return // not a tiered backend (single-model / test) ⇒ no second tier ⇒ no-op
	}
	// A dedicated, seeded RNG stream for the route policy — derived from the engine seed so the route
	// decisions are reproducible AND independent of the conscious soft-policy's RNG draw sequence.
	routeRNG := cpyrand.New(uint64(e.cfg.Seed) ^ 0x52_4f_55_54 /* "ROUT" */)
	e.registerRNG("route", routeRNG) // resume registry (resume.go): the route policy's stream must be snapshotted too
	policy := route.NewThompsonPolicy(routeRNG)
	tb.SetTierRouter(route.NewRouter(true, policy, true))
}
