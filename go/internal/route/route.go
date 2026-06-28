// Package route is the cost-aware substrate TIER router — the one learned scheduler component the
// RL/ML scaling research found is justified TODAY (docs/internal/2026-06-20-rl-ml-scheduler-scaling-
// research.md §4/§5 Scenario C: RouteLLM-style, ~2x cost at equal quality; all four crossover
// conditions hold — large effective state, query-dependent, measurable reward, high decision
// frequency). It picks the substrate TIER (utility/haiku vs primary/sonnet — with a seam for a
// future local tier) per CONTENT call.
//
// THE PATTERN IS C (docs/internal/notes/heuristic-llm-pattern-refactor.md §1 Pattern C — control FLOOR +
// optional learned CEILING), structurally the ghOSt "policy advises, kernel keeps invariants"
// split (research §6):
//
//   - FLOOR (Pattern-A, ALWAYS decides, NO model call to decide routing): the EXISTING hardcoded
//     per-role tier split (which roles go to primary vs utility). This is the safe fallback and the
//     instant path — the routing decision is a closed-form map lookup, never an inference call (the
//     ghOSt latency lesson, research §6/§8: the routing decision must be cheap). With the router
//     OFF the floor IS the decision and behaviour is byte-identical to the pre-router TieredBackend.
//   - CEILING (the learned/policy part, flag-gated): on a deterministically-FLAGGED call, the policy
//     may ESCALATE a flagged-HARD call to primary or DOWNGRADE a flagged-EASY call to utility, using
//     a cheap context Signal (role class + a difficulty estimate from V(s) and/or input features). It
//     NEVER overrides a STRUCTURAL requirement (a role the floor PINS to a tier — e.g. respond stays
//     primary), and a non-route (the floor stands) is SURFACED, never silent (Rule 4).
//
// This package is a Tier-1 LEAF: it imports only stdlib + internal/cpyrand (the seeded RNG the
// Thompson policy samples with — never the wall clock, never unseeded randomness, so a route
// decision is reproducible). It does NOT import backends/llm/events — the llm.TieredBackend owns
// the wiring (it emits the routing event and dispatches to the chosen *OpenAICompatBackend), so this
// stays a pure, testable decision component (interfaces over concretes — the policy is swappable).
package route

import (
	"math"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// Tier names the substrate tier a CONTENT call is dispatched to. Primary is the big reasoning model
// (sonnet); Utility is the small/cheap model (haiku). Local is the SEAM for a future local tier (the
// re-localization phase, W6) — the router can name it but the TieredBackend has no local tier wired
// yet, so a policy that returns Local is clamped back to the floor by the Router until it exists.
type Tier int

const (
	// Primary is the big reasoning tier (sonnet) — the default for reasoning roles.
	Primary Tier = iota
	// Utility is the small/cheap tier (haiku) — the default for trivial roles (summarize).
	Utility
	// Local is the future on-device tier (W6 re-localization). NOT yet wired in the TieredBackend;
	// the Router clamps a Local pick back to the floor so the seam compiles without a live tier.
	Local
)

// String renders a Tier as its canonical lower-case name (the wire value carried on the event).
func (t Tier) String() string {
	switch t {
	case Primary:
		return "primary"
	case Utility:
		return "utility"
	case Local:
		return "local"
	default:
		return "primary"
	}
}

// Signal is the cheap context the CEILING policy reads to estimate difficulty/value. It is a small
// fixed feature vector (RouteLLM-style: a few cheap features, not a deep encoder) so the routing
// decision stays a closed-form lookup + a tiny linear/posterior read — never a model call.
//
// All fields are derived deterministically by the caller (the TieredBackend) from the call it is
// about to make, so a route decision is reproducible. The richer-bandit next step is to feed the
// engine's live V(s) into Value (today the caller passes 0 when V(s) is not threaded to the call
// site — the policy then leans on Role + PromptLen, the input features RouteLLM itself uses).
type Signal struct {
	// Role is the backend chat role tag (the OPERATION tail, e.g. "summarize", "generate",
	// "respond", "judge_admission") — the single strongest router feature (RouteLLM routes on the
	// query class).
	Role string
	// Value is the difficulty/value estimate in [0,1] from V(s) when the caller has it (0 = not
	// supplied). HIGH value/difficulty argues for primary; LOW argues for utility. This is the
	// natural difficulty signal (internal/value) the research §4 names.
	Value float64
	// PromptLen is the combined system+user prompt rune length — a cheap proxy for query complexity
	// (RouteLLM uses input features). Longer ⇒ likelier-hard ⇒ argues for primary.
	PromptLen int
}

// FloorTier is the cheap, deterministic FLOOR: the EXISTING hardcoded per-role tier split (Pattern-A,
// NO model call). It reproduces the pre-router TieredBackend behaviour exactly — only the trivial
// utility roles route to Utility; every reasoning role stays Primary. This is the safe fallback and
// the instant path; the Router calls it first and the policy only ever ADJUSTS its output.
//
// utilityRoles is the closed set of operations the floor pins to the utility tier — today exactly
// {summarize, compress} (the TieredBackend.Summarize override was the only utility route). Matched on
// the OPERATION tail (the role tag may be "conscious.compress"); a bare "summarize" works too.
func FloorTier(role string) Tier {
	if _, ok := utilityRoles[op(role)]; ok {
		return Utility
	}
	return Primary
}

// utilityRoles is the floor's utility-tier set (the trivial roles). Mirrors the historical
// TieredBackend override (only Summarize routed to the small model). "compress" is the scheduler's
// tail for the summarize/compress role tag ("conscious.compress").
var utilityRoles = map[string]struct{}{
	"summarize": {}, "compress": {},
}

// pinnedToPrimary is the set of roles the floor STRUCTURALLY pins to the primary tier — the router
// may never DOWNGRADE these to a cheaper model however "easy" the call looks, because answer quality
// on these roles is load-bearing and a cheap miss is user-visible (respond = the user-facing answer)
// or a structural fact (decide = the executive move). The CEILING may still ESCALATE within the set
// (it is already primary), but a downgrade is refused and the floor stands (Rule 4, structural). This
// is the route analogue of the Controller/Filter "the model may not override a structural move".
var pinnedToPrimary = map[string]struct{}{
	"respond": {}, "decide": {},
}

// op returns the OPERATION tail of a role tag ("conscious.compress" -> "compress"; a bare "respond"
// is returned unchanged) — the same tail-match the scheduler's IsForeground uses, so the floor and
// the scheduler agree on what a role IS.
func op(role string) string {
	for i := len(role) - 1; i >= 0; i-- {
		if role[i] == '.' {
			return role[i+1:]
		}
	}
	return role
}

// Reason names WHY a route decision landed where it did — carried on the routing event so the
// Pattern-C health surface is legible (floor vs ceiling, and why the ceiling did or did not move it).
type Reason string

const (
	// ReasonFloor — the floor stood: the call was not flagged (not escalation-eligible), or the
	// router is OFF. The policy was not consulted.
	ReasonFloor Reason = "floor"
	// ReasonEscalated — the ceiling escalated a flagged-HARD call up to primary.
	ReasonEscalated Reason = "escalated"
	// ReasonDowngraded — the ceiling downgraded a flagged-EASY call to utility.
	ReasonDowngraded Reason = "downgraded"
	// ReasonStructural — the ceiling WOULD have downgraded, but the role is structurally pinned to
	// primary (respond/decide) — the floor stands (Rule 4, structural-protection).
	ReasonStructural Reason = "structural"
	// ReasonNoUtility — the ceiling chose utility but no utility tier is wired (single-model config)
	// — the floor (primary) stands.
	ReasonNoUtility Reason = "no-utility"
	// ReasonPolicyAgrees — the policy was consulted on a flagged call and AGREED with the floor (no
	// change). Distinct from ReasonFloor (where the policy was never asked) so the health surface can
	// show the policy is active but concurring.
	ReasonPolicyAgrees Reason = "policy-agrees"
)

// Decision is the router's output: the chosen tier, whether the ceiling was consulted, and why.
type Decision struct {
	Tier       Tier    // the tier the call is dispatched to
	FloorTier  Tier    // what the floor alone would have picked (for the health surface)
	Reason     Reason  // why (floor / escalated / downgraded / structural / no-utility / policy-agrees)
	Flagged    bool    // was the call escalation-ELIGIBLE (deterministically flagged fuzzy)?
	Confidence float64 // the policy's confidence in its pick (0 when the floor stood unconsulted)
}

// RoutePolicy is the swappable CEILING — the learned/principled part. Given the floor's pick and the
// cheap Signal, it returns the tier it would route to AND a confidence. It is consulted by the Router
// ONLY on a flagged call (Pattern-C: the policy is the optional ceiling, never the floor). A policy
// keeps its parameters internal; Update feeds back a measured reward so it can be trained via the
// EXISTING keep-or-revert (the caller wraps Update in critic.ExperimentWindow), so the policy is
// keep-or-revert-gated, not a black box.
//
// The interface (not a concrete) is the swap seam: v1 ships ValuePolicy (a principled, transparent
// value-keyed threshold) AND ThompsonPolicy (a contextual bandit over {utility, primary}); the
// richer LinUCB / RouteLLM-classifier is a drop-in next step behind this exact interface.
type RoutePolicy interface {
	// Route returns the tier the policy would pick for this signal + the floor's pick, with a
	// confidence in [0,1]. It NEVER returns Local (no live tier) — the Router enforces the seam.
	Route(floor Tier, s Signal) (Tier, float64)
	// Update feeds back the measured reward for a (tier, signal) the policy chose — quality-at-cost
	// (higher = better). A no-op for a stateless policy. The caller gates the accept/reject of any
	// drift via critic.ExperimentWindow (keep-or-revert), so this is the proposer, never the gate.
	Update(tier Tier, s Signal, reward float64)
	// Name reports the policy kind for the health surface / event payload.
	Name() string
}

// Flag is the deterministic "is this call escalation-eligible?" gate (Pattern-C: escalate ONLY on a
// flagged-fuzzy case). A call is flagged when its difficulty signal is genuinely UNCERTAIN — near the
// boundary where the floor's static role pick might be wrong — so the cheap context can add value:
//
//   - a HIGH value/difficulty on a role the floor routes to UTILITY (a hard summarize is worth the
//     big model) — escalation-eligible;
//   - a LOW value/difficulty on a role the floor routes to PRIMARY (a trivial generate could ride the
//     cheap model) — downgrade-eligible;
//   - a very long prompt on a utility-floored role (complex input ⇒ likelier hard) — eligible.
//
// A clearly-typical call (a normal-length reasoning role at mid value) is NOT flagged — the floor
// stands silently, so the common path pays nothing and the policy is consulted only where it matters.
// fuzzyLo/fuzzyHi bound the "uncertain" value band; longPrompt is the input-complexity trip.
func Flag(floor Tier, s Signal) bool {
	const fuzzyLo, fuzzyHi = 0.30, 0.70
	const longPrompt = 2000 // runes — a long prompt argues the utility floor may be wrong
	switch floor {
	case Utility:
		// the floor wants cheap; flag if it looks HARD (high value OR a long/complex prompt).
		return s.Value >= fuzzyHi || s.PromptLen >= longPrompt
	default: // Primary
		// the floor wants the big model; flag if it looks EASY (clearly low value) so a downgrade
		// can be considered. A zero Value (not supplied) is NOT "easy" — it is "unknown", so an
		// unsupplied-value reasoning call is never spuriously downgraded.
		return s.Value > 0 && s.Value <= fuzzyLo
	}
}

// Router composes the FLOOR with an optional CEILING policy. It is the Pattern-C decision unit: the
// floor always decides; the policy is consulted only on a flagged call; the floor is authoritative on
// a structural pin; a Local pick (no live tier) and a Utility pick with no utility tier both clamp
// back to the floor. enabled is the flag gate — OFF ⇒ the floor is the decision, byte-identical to
// the pre-router behaviour, and the policy is never consulted.
type Router struct {
	enabled      bool
	policy       RoutePolicy
	utilityWired bool // is a utility tier actually wired? (false ⇒ a utility pick clamps to the floor)
}

// NewRouter builds a Router. enabled is the flag gate (default-OFF caller passes false). policy may
// be nil (no ceiling — the floor always stands even when enabled). utilityWired reports whether the
// TieredBackend actually has a utility tier (a single-model config has none, so a utility pick must
// clamp to the floor / primary).
func NewRouter(enabled bool, policy RoutePolicy, utilityWired bool) *Router {
	return &Router{enabled: enabled, policy: policy, utilityWired: utilityWired}
}

// Decide returns the routing Decision for one CONTENT call. It is pure + deterministic over (role,
// signal, policy state): no clock, no unseeded randomness (the Thompson policy draws from a seeded
// RNG threaded into it at construction).
func (r *Router) Decide(s Signal) Decision {
	floor := FloorTier(s.Role)
	d := Decision{Tier: floor, FloorTier: floor, Reason: ReasonFloor}

	// Router OFF, or no policy ⇒ the floor is the decision (Pattern-A only). Byte-identical fallback.
	if r == nil || !r.enabled || r.policy == nil {
		return d
	}
	// Not flagged-fuzzy ⇒ not escalation-eligible ⇒ the floor stands SILENTLY (the common, cheap
	// path — the policy is never consulted, so it pays nothing on a typical call).
	if !Flag(floor, s) {
		return d
	}
	d.Flagged = true

	// Consult the CEILING. The policy proposes a tier + confidence; the Router VALIDATES it against
	// the structural invariants (the ghOSt "kernel re-validates the advisor's move").
	pick, conf := r.policy.Route(floor, s)
	d.Confidence = conf

	// Seam: a Local pick has no live tier — clamp to the floor (the W6 re-localization seam).
	if pick == Local {
		d.Reason = ReasonFloor
		return d
	}
	// The policy agrees with the floor — active but concurring (distinct from the unconsulted floor).
	if pick == floor {
		d.Reason = ReasonPolicyAgrees
		return d
	}
	// A DOWNGRADE to utility.
	if pick == Utility {
		// Structural pin: respond/decide may never be downgraded — the floor stands (Rule 4).
		if _, pinned := pinnedToPrimary[op(s.Role)]; pinned {
			d.Reason = ReasonStructural
			return d
		}
		// No utility tier wired (single-model config) — the floor (primary) stands.
		if !r.utilityWired {
			d.Reason = ReasonNoUtility
			return d
		}
		d.Tier, d.Reason = Utility, ReasonDowngraded
		return d
	}
	// An ESCALATION to primary (from a utility-floored call that looked hard).
	if pick == Primary {
		d.Tier, d.Reason = Primary, ReasonEscalated
		return d
	}
	// Defensive: any other pick keeps the floor.
	return d
}

// Update feeds a measured reward back to the policy for a chosen (tier, signal) — the training
// signal. A no-op when the router is off / has no policy. The caller gates acceptance via
// keep-or-revert (critic.ExperimentWindow), so this only PROPOSES a drift.
func (r *Router) Update(d Decision, s Signal, reward float64) {
	if r == nil || !r.enabled || r.policy == nil {
		return
	}
	r.policy.Update(d.Tier, s, reward)
}

// Enabled reports whether the router's CEILING is active (the flag gate). When false the floor is the
// decision (byte-identical to the pre-router behaviour).
func (r *Router) Enabled() bool { return r != nil && r.enabled }

// PolicyName reports the active policy kind (or "none") for the health surface.
func (r *Router) PolicyName() string {
	if r == nil || r.policy == nil {
		return "none"
	}
	return r.policy.Name()
}

// ============================================================================
// Policies — the swappable CEILING. v1 ships two principled options behind RoutePolicy.
// ============================================================================

// ValuePolicy is the simplest principled ceiling (the "flat first" discipline, research §0/§3a — a
// transparent threshold before reaching for a bandit). It routes purely on the difficulty Signal:
// route HARD (high value / long prompt) to primary, EASY (low value, short prompt) to utility. It is
// stateless (Update is a no-op) and fully deterministic — the right v1 default because it is legible
// and cannot reward-hack (the research's strongest argument against a black box). The learnable seam
// is the threshold; the next step is ThompsonPolicy (below), which LEARNS the boundary online.
type ValuePolicy struct {
	// HardValue is the value at/above which a call routes to primary (default 0.5).
	HardValue float64
	// EasyValue is the value at/below which a call routes to utility (default 0.5).
	EasyValue float64
	// LongPrompt is the prompt rune length at/above which a call is treated as hard (default 1500).
	LongPrompt int
}

// NewValuePolicy builds a ValuePolicy with the principled defaults.
func NewValuePolicy() *ValuePolicy {
	return &ValuePolicy{HardValue: 0.5, EasyValue: 0.5, LongPrompt: 1500}
}

// Route picks a tier from the difficulty signal alone (transparent, stateless).
func (p *ValuePolicy) Route(floor Tier, s Signal) (Tier, float64) {
	// Input complexity dominates: a long/complex prompt is hard regardless of role.
	if s.PromptLen >= p.LongPrompt {
		return Primary, 0.8
	}
	if s.Value >= p.HardValue {
		return Primary, clamp01(s.Value)
	}
	if s.Value > 0 && s.Value <= p.EasyValue {
		return Utility, clamp01(1.0 - s.Value)
	}
	// Unknown value, short prompt ⇒ defer to the floor (no opinion).
	return floor, 0.0
}

// Update is a no-op (ValuePolicy is stateless/transparent).
func (p *ValuePolicy) Update(Tier, Signal, float64) {}

// Name reports the policy kind.
func (p *ValuePolicy) Name() string { return "value-threshold" }

var _ RoutePolicy = (*ValuePolicy)(nil)

// ============================================================================

// ThompsonPolicy is the contextual-bandit CEILING (research §3a — the cheapest/safest learned rung,
// the LeCaR-class "tiny online learner over a handful of experts" the OS reference class actually
// SHIPS, not deep online RL). It is a per-ARM Beta-Bernoulli Thompson sampler over {Utility, Primary}
// keyed by a coarse CONTEXT bucket (role-class x difficulty-band), so it learns a SEPARATE
// explore/exploit posterior for each kind of call — the contextual part. The reward is a Bernoulli
// quality-at-cost success in [0,1]; a win nudges the chosen arm's α up, a loss nudges β up. Sampling
// is from a seeded RNG (reproducible) — never the wall clock.
//
// Why Thompson over LinUCB for v1: Beta-Bernoulli Thompson is the smallest principled contextual
// bandit (two counters per arm per bucket), needs no matrix inversion, and balances explore/exploit
// automatically with no tuning — exactly the LeCaR profile the research recommends as the FIRST
// learned rung. LinUCB (a linear UCB over the full Signal vector) is the documented richer next step
// behind this same RoutePolicy interface.
type ThompsonPolicy struct {
	rng  *cpyrand.Random
	arms map[string]*betaArm // context-bucket key -> the two-arm posterior
}

// betaArm holds the Beta(α,β) success/failure counts for {Utility, Primary} in one context bucket.
type betaArm struct {
	// [Utility, Primary] α (successes+1) and β (failures+1) — Local is not an arm (no live tier).
	alpha [2]float64
	beta  [2]float64
}

// NewThompsonPolicy builds a contextual Thompson sampler over a seeded RNG (required — the policy
// must be reproducible; a nil RNG makes the policy abstain to the floor so it never reaches for
// unseeded randomness).
func NewThompsonPolicy(rng *cpyrand.Random) *ThompsonPolicy {
	return &ThompsonPolicy{rng: rng, arms: map[string]*betaArm{}}
}

// bucket maps a Signal to a coarse, deterministic context key (role-class x difficulty-band) so the
// posterior is learned per kind-of-call (the contextual part) while keeping the arm count tiny.
func bucket(s Signal) string {
	band := "mid"
	switch {
	case s.Value <= 0:
		band = "unk"
	case s.Value < 0.34:
		band = "lo"
	case s.Value < 0.67:
		band = "mid"
	default:
		band = "hi"
	}
	return op(s.Role) + "|" + band
}

// armOf returns (creating if needed) the two-arm posterior for a signal's bucket. The α/β are
// initialised to 1 (a uniform Beta(1,1) prior — no initial bias toward either tier).
func (p *ThompsonPolicy) armOf(s Signal) *betaArm {
	k := bucket(s)
	a, ok := p.arms[k]
	if !ok {
		a = &betaArm{alpha: [2]float64{1, 1}, beta: [2]float64{1, 1}}
		p.arms[k] = a
	}
	return a
}

// Route samples a tier from the bucket's posterior (Thompson sampling): draw a Beta sample for each
// arm, pick the higher. With no RNG the policy abstains to the floor (it must not invent randomness).
// The confidence is the absolute gap between the two posterior means — high when the policy is sure.
func (p *ThompsonPolicy) Route(floor Tier, s Signal) (Tier, float64) {
	if p.rng == nil {
		return floor, 0.0
	}
	a := p.armOf(s)
	uSample := betaSample(p.rng, a.alpha[0], a.beta[0])
	pSample := betaSample(p.rng, a.alpha[1], a.beta[1])
	uMean := a.alpha[0] / (a.alpha[0] + a.beta[0])
	pMean := a.alpha[1] / (a.alpha[1] + a.beta[1])
	conf := math.Abs(uMean - pMean)
	if uSample > pSample {
		return Utility, conf
	}
	return Primary, conf
}

// Update nudges the chosen arm's posterior by a reward in [0,1] (quality-at-cost): the reward mass
// goes to α (success), the complement to β (failure) — the standard Bernoulli-reward Beta update.
// Local is not an arm, so a Local tier is ignored.
func (p *ThompsonPolicy) Update(tier Tier, s Signal, reward float64) {
	if tier == Local {
		return
	}
	idx := 1 // Primary
	if tier == Utility {
		idx = 0
	}
	r := clamp01(reward)
	a := p.armOf(s)
	a.alpha[idx] += r
	a.beta[idx] += 1.0 - r
}

// Name reports the policy kind.
func (p *ThompsonPolicy) Name() string { return "thompson-bandit" }

var _ RoutePolicy = (*ThompsonPolicy)(nil)

// betaSample draws one sample from Beta(α,β) via the gamma ratio X/(X+Y), X~Gamma(α,1), Y~Gamma(β,1),
// using ONLY the seeded RNG (reproducible). Marsaglia-Tsang for the gamma draws (the standard
// rejection method); falls back to the mean for degenerate shapes so it never loops unbounded.
func betaSample(rng *cpyrand.Random, alpha, beta float64) float64 {
	x := gammaSample(rng, alpha)
	y := gammaSample(rng, beta)
	if x+y <= 0 {
		return alpha / (alpha + beta)
	}
	return x / (x + y)
}

// gammaSample draws Gamma(shape, 1) from a seeded RNG (Marsaglia-Tsang). For shape < 1 it boosts to
// shape+1 and scales by U^(1/shape). Bounded: the rejection loop is capped so it never spins on a
// pathological draw (it returns the shape as the mean fallback).
func gammaSample(rng *cpyrand.Random, shape float64) float64 {
	if shape <= 0 {
		return 0
	}
	if shape < 1 {
		u := rng.Float64()
		if u <= 0 {
			u = 1e-12
		}
		return gammaSample(rng, shape+1) * math.Pow(u, 1.0/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for i := 0; i < 256; i++ { // bounded — never an unbounded loop
		x := normalSample(rng)
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1.0-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v
		}
	}
	return d // mean fallback (bounded exit)
}

// normalSample draws a standard normal via Box-Muller from the seeded RNG (two uniforms -> one
// normal; the second deviate is discarded for simplicity — determinism over efficiency).
func normalSample(rng *cpyrand.Random) float64 {
	u1 := rng.Float64()
	if u1 <= 0 {
		u1 = 1e-12
	}
	u2 := rng.Float64()
	return math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)
}

// clamp01 clamps x to [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// SortedBuckets returns the policy's learned context buckets in deterministic order — a read for the
// health surface / a test (the posterior means per bucket). Only the ThompsonPolicy has buckets; the
// generic read keeps the package self-describing.
func (p *ThompsonPolicy) SortedBuckets() []string {
	keys := make([]string, 0, len(p.arms))
	for k := range p.arms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// Cost measurement hook — the deferred live-claude A/B accumulator (NOT run here).
// ============================================================================

// CostAccount is the per-tier cost accumulator for the deferred cost-win measurement. The router only
// proves the MECHANISM (it routes correctly) offline; the actual ~2x cost win (research §4/§5 Scenario
// C) requires a live-claude A/B (router OFF vs ON over the same task suite, summing COMPLETION tokens
// per tier — input-token wins are masked by the bridge's prompt cache, so route on completion cost).
// That A/B is a deferred, user-authorized live-claude run; this is the ready hook for it.
//
// Wiring it for the A/B: a subscriber sums Add(tier, completion_tokens) off the routing.tier event +
// the matching llm.call event's "completion_tokens"; the OFF-vs-ON delta in total weighted cost (with
// the utility tier priced cheaper than primary) is the measured cost win. Mechanism-correct (the route
// dispatches right, proven offline) != cost-win-proven (the magnitude, which only the live A/B shows).
type CostAccount struct {
	// PrimaryCalls / UtilityCalls count the CONTENT calls dispatched to each tier.
	PrimaryCalls, UtilityCalls int
	// PrimaryTokens / UtilityTokens sum the completion tokens spent on each tier (the cost basis).
	PrimaryTokens, UtilityTokens int
	// Escalations / Downgrades / FloorStands count the route decisions by kind (the health surface).
	Escalations, Downgrades, FloorStands int
}

// Add records one routed CONTENT call's tier + completion-token spend + the decision reason.
func (a *CostAccount) Add(tier Tier, completionTokens int, reason Reason) {
	switch tier {
	case Utility:
		a.UtilityCalls++
		a.UtilityTokens += completionTokens
	default:
		a.PrimaryCalls++
		a.PrimaryTokens += completionTokens
	}
	switch reason {
	case ReasonEscalated:
		a.Escalations++
	case ReasonDowngraded:
		a.Downgrades++
	default:
		a.FloorStands++
	}
}

// WeightedCost is the cost basis for the A/B: primary tokens weighted at utilityPrice<1 for utility
// tokens (the cheaper model). The OFF-vs-ON delta in this number over the same suite is the measured
// cost win. utilityPrice is the utility/primary per-token price ratio (e.g. haiku/sonnet ~0.08).
func (a *CostAccount) WeightedCost(utilityPrice float64) float64 {
	return float64(a.PrimaryTokens) + utilityPrice*float64(a.UtilityTokens)
}
