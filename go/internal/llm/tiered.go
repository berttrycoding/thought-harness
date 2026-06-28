package llm

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/route"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TieredBackend runs two model tiers behind one Backend: a PRIMARY (reasoning) model does the
// real thinking, and a small UTILITY model handles the trivial roles (summaries / progress text)
// to spare the big model's budget. This is hybrid cognition ("not every call deserves the big
// model", HEURISTICS.md) applied at the model layer.
//
// Python forwarded every non-overridden role to the primary via __getattr__; the Go equivalent is
// interface EMBEDDING — TieredBackend embeds a backends.Backend (the primary), so every core
// CONTENT method promotes to the primary automatically, and ONLY Summarize is overridden to route
// to the utility tier. The promoted Primary also satisfies the core interface, so the embedding is
// faithful to Python's "any role not in _UTILITY_ROLES delegates to the primary".
//
// The Python _UTILITY_ROLES = ("summarize",) — only the summarize/compress role is utility; every
// reasoning role (generate/transform/respond/decide/judge_admission/operator/specialist/synth)
// stays on the primary. (The old control roles score_admit/rank are no longer model roles — they
// are Pattern-A math in internal/control.) That maps exactly to overriding only Summarize here.
type TieredBackend struct {
	backends.Backend // the PRIMARY — promotes the 8 core methods + AppraiserName
	Primary          *OpenAICompatBackend
	Utility          *OpenAICompatBackend

	// router is the cost-aware substrate TIER router (internal/route, subconscious.tier_router) — the
	// Pattern-C per-call tier selection (floor + optional learned ceiling). nil ⇒ the FLOOR decides
	// every call (byte-identical to the pre-router dispatch). Installed by SetTierRouter (tier_router.go).
	router *route.Router
	// emit is the TieredBackend-level event bus closure — used ONLY to emit the routing.tier event (the
	// per-tier dispatch decision; the tiers themselves emit their own llm.call/llm.fallback). BindEmit
	// captures it AND forwards to both tiers. nil ⇒ no routing event (the router still routes).
	emit events.Emit
}

// NewTiered wraps a primary reasoning backend with a small utility backend for trivial roles. It also
// wires the MODEL-ESCALATION TIER (Item 3): the primary's structuredEscalate hook is pointed at the
// UTILITY backend's escalateStructured, so a STRUCTURED role (synthesize_program / form_intention)
// that STILL truncates-invalid after the primary's bounded retry exhausts at the max budget gets ONE
// final attempt against the (smaller, often differently-behaved) utility model before the caller falls
// to the control floor. A bare OpenAICompatBackend (no Tiered wrapper) leaves structuredEscalate nil ⇒
// no escalation tier ⇒ zero behaviour change — exactly the common, single-model case. The escalation is
// BOUNDED to a single call (escalateStructured carries no escalator of its own).
func NewTiered(primary, utility *OpenAICompatBackend) *TieredBackend {
	if primary != nil && utility != nil {
		primary.structuredEscalate = utility.escalateStructured
	}
	return &TieredBackend{Backend: primary, Primary: primary, Utility: utility}
}

// ---------------------------------------------------------------------------
// Routable CONTENT roles — each consults the tier router (tier_router.go) to pick Primary vs Utility,
// then dispatches to the chosen tier. With the router OFF the FLOOR decides (route.FloorTier): every
// reasoning role -> Primary, summarize/compress -> Utility — exactly the pre-router dispatch (Generate/
// Transform/Respond/OperatorApply were promoted from the embedded Primary; only Summarize overrode to
// Utility). So the flag-OFF dispatch is byte-identical and no routing.tier event fires.
//
// The Signal's Value is 0 (V(s) not threaded to the per-call backend seam in v1 — the policy leans on
// role + prompt length, the RouteLLM input features; feeding the engine's live V(s) here is the
// documented richer-policy next step). The chat role tags match the per-method role strings used in
// openai.go so route.FloorTier / the scheduler agree on what a role IS.
// ---------------------------------------------------------------------------

// Generate routes the conscious-generate call. Floor: Primary (a reasoning role).
func (t *TieredBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	system, user := PromptGenerate(goal, ctx)
	return t.route("conscious.generate", 0, system, user).Generate(goal, ctx, rng)
}

// Wander routes the AWAKE-mode idle content call to the PRIMARY (reasoning) backend — it is a
// CREATIVE role, not a trivial utility one, so it never downgrades to the utility tier. Explicit
// (rather than relying on the embedded promotion) so the dispatch is deliberate and the chat role
// tag stays "conscious.wander".
func (t *TieredBackend) Wander(kind, hint string, ctx []types.Thought, rng *cpyrand.Random) string {
	system, user := PromptWander(kind, hint, ctx)
	return t.route("conscious.wander", 0, system, user).Wander(kind, hint, ctx, rng)
}

// Transform routes the seam-transform call. Floor: Primary (a reasoning role).
func (t *TieredBackend) Transform(c types.Candidate, hist []types.Thought) string {
	system, user := PromptTransform(c, hist)
	return t.route("seam.transform", 0, system, user).Transform(c, hist)
}

// Summarize routes the conscious-compress call. Floor: Utility (the ONE trivial role — the historical
// override, now expressed as the router floor; the router may ESCALATE a flagged-hard summarize up).
func (t *TieredBackend) Summarize(ts []types.Thought) string {
	system, user := PromptSummarize(ts)
	return t.route("conscious.compress", 0, system, user).Summarize(ts)
}

// Respond routes the user-facing answer. Floor: Primary; the router may NOT downgrade it (respond is
// structurally pinned to primary — a cheap user-facing answer is a visible quality miss).
func (t *TieredBackend) Respond(goal string, ctx []types.Thought) string {
	system, user := PromptRespond(goal, ctx)
	return t.route("action.respond", 0, system, user).Respond(goal, ctx)
}

// OperatorApply routes a sub-agent's scoped operator move. Floor: Primary (a reasoning role); it is a
// BACKGROUND role, so it is a prime downgrade candidate on an easy call.
func (t *TieredBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	system, user := PromptOperatorApply(role, responsibility, domain, goal, ctx)
	return t.route("operator."+role, 0, system, user).OperatorApply(role, responsibility, intent, domain, goal, ctx)
}

// DisplayName reports both tiers (Python TieredBackend.display_name), EXCEPT when the utility tier is a
// session-bridge alias ("session-utility") — there the suffix carries no model identity, just noise, so
// the clean documented label (e.g. "cc:session") is shown instead (S6).
func (t *TieredBackend) DisplayName() string {
	if strings.HasPrefix(t.Utility.Model, "session") {
		return t.Primary.DisplayName()
	}
	return t.Primary.DisplayName() + " (+util:" + t.Utility.Model + ")"
}

// BindEmit wires the bus into BOTH tiers (Python loops over (primary, utility)) AND captures the
// closure at the TieredBackend level so the tier router can emit its routing.tier event (the per-call
// dispatch decision, distinct from each tier's own llm.call/llm.fallback).
func (t *TieredBackend) BindEmit(emit events.Emit) {
	t.emit = emit
	t.Primary.BindEmit(emit)
	t.Utility.BindEmit(emit)
}

// BindScheduler wires the scheduler into BOTH tiers (Python loops over (primary, utility)).
func (t *TieredBackend) BindScheduler(s *scheduler.LLMScheduler) {
	t.Primary.BindScheduler(s)
	t.Utility.BindScheduler(s)
}

// Health reports the primary's health (Python TieredBackend.health -> self.primary.health()).
func (t *TieredBackend) Health() HealthReport { return t.Primary.Health() }

// The optional capability methods (Decide/Intention/PrimitiveSubAgent) are NOT promoted by embedding
// because the embedded field is the interface type backends.Backend, which does not declare them.
// Forward them explicitly to the primary so a TieredBackend satisfies the same optional interfaces
// the bare OpenAICompatBackend does (Python __getattr__ forwarded these to the primary).

// Decide forwards to the primary (Controller.decide).
func (t *TieredBackend) Decide(goal string, ctx []types.Thought, options []string) (choice, why string) {
	return t.Primary.Decide(goal, ctx, options)
}

// Intention forwards to the primary (form_intention).
func (t *TieredBackend) Intention(goal string, ctx []types.Thought) (text, kind string, ok bool) {
	return t.Primary.Intention(goal, ctx)
}

// PrimitiveSubAgent forwards to the primary (a domain-scoped sub-agent call).
func (t *TieredBackend) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	return t.Primary.Specialist(domain, description, ctx)
}

// JudgeAdmission forwards to the primary (the Filter's Pattern-C escalation is a reasoning role,
// not a utility one).
func (t *TieredBackend) JudgeAdmission(c types.Candidate, hist []types.Thought, floor types.FilterVerdict) (types.FilterVerdict, bool) {
	return t.Primary.JudgeAdmission(c, hist, floor)
}

// JudgeSufficiency forwards to the primary (the A-RAG1 CRAG sufficiency ceiling is a coverage-judgment
// reasoning role, not a utility one). MUST be explicit — the optional SufficiencyJudge interface is not on
// the embedded core Backend, so without this the engine's `backend.(backends.SufficiencyJudge)` assertion
// on a tiered backend would fail and the ceiling would silently never fire (the gate would resolve to the
// deterministic floor only, even in hybrid/llm mode).
func (t *TieredBackend) JudgeSufficiency(query, fuelText, floorVerdict string) (string, bool) {
	return t.Primary.JudgeSufficiency(query, fuelText, floorVerdict)
}

// JudgeConscience / JudgeAcceptance forward to the primary (the §7.2 / §1.6 model ceilings are reasoning
// roles). MUST be explicit — the optional ConscienceJudge / AcceptanceJudge interfaces are not on the
// embedded core Backend, so without these the engine's `backend.(backends.ConscienceJudge)` assertion on a
// tiered backend would fail and the ceiling would silently never fire.
func (t *TieredBackend) JudgeConscience(actionText string) (bool, string, bool) {
	return t.Primary.JudgeConscience(actionText)
}

func (t *TieredBackend) JudgeAcceptance(goal string, ctx []types.Thought) (string, bool) {
	return t.Primary.JudgeAcceptance(goal, ctx)
}

// JudgeEngagement forwards to the primary (the AWAKE-DISP rung-2 engagement ceiling is a worth-it
// judgment reasoning role, not a utility one). MUST be explicit — the optional EngagementJudge interface
// is not on the embedded core Backend, so without this the engine's backend.(backends.EngagementJudge)
// assertion on a tiered backend would fail and the ceiling would silently never fire.
func (t *TieredBackend) JudgeEngagement(goal, recentContext, floorVerdict string) (string, bool) {
	return t.Primary.JudgeEngagement(goal, recentContext, floorVerdict)
}

// JudgeAnswerSupport forwards to the primary (the T2.1 independent-verifier Pattern-C ceiling — a
// reasoning role). MUST be forwarded explicitly for the same interface-embedding reason as
// JudgeEngagement: AnswerSupportJudge is not on the embedded core Backend, so without this a tiered
// (sonnet+haiku) config would fail the backend.(backends.AnswerSupportJudge) assertion and the verifier's
// ceiling would silently never fire — the dead-ceiling wiring gap the compile-time guards now prevent.
func (t *TieredBackend) JudgeAnswerSupport(question, answer, evidence, floorVerdict string) (string, bool) {
	return t.Primary.JudgeAnswerSupport(question, answer, evidence, floorVerdict)
}

// Compile-time guards: both real substrates MUST satisfy the T2.1 verifier ceiling, so a future drop of
// either JudgeAnswerSupport method fails the BUILD rather than silently disabling the ceiling (the
// dead-wiring gap red-team caught). The claude bridge IS an OpenAICompatBackend, so this covers it too.
var (
	_ backends.AnswerSupportJudge = (*OpenAICompatBackend)(nil)
	_ backends.AnswerSupportJudge = (*TieredBackend)(nil)
)

// Comprehend forwards to the primary (the LLM "to_operator" / unified tool-selection step — a
// reasoning role). This MUST be forwarded explicitly: the embedded field is the interface type
// backends.Backend, which does not declare RealityComprehender, so without this method a
// TieredBackend would NOT satisfy backends.RealityComprehender and every `e.backend.(RealityComprehender)`
// assertion would silently get nil and fall through to the regex/keyword floor — disabling the
// grounding tool-selection fix the moment a utility model is configured.
func (t *TieredBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	return t.Primary.Comprehend(ctx)
}

// SetLegibleFragment forwards to the primary (the legible-generation instrument appends its control
// tag to the conscious Generate prompt, which runs on the primary). Forwarded for the same
// interface-embedding reason as Comprehend: without it, LegiblePrompter is silently lost under a
// tiered config and the instrument never engages.
func (t *TieredBackend) SetLegibleFragment(fragment string) { t.Primary.SetLegibleFragment(fragment) }

// SetPersonaFragment forwards to the primary (the RESPOND role runs on the primary) — explicit for
// the same interface-embedding reason as Comprehend/SetLegibleFragment: without it a tiered config
// would silently drop the learned person adaptation.
func (t *TieredBackend) SetPersonaFragment(fragment string) { t.Primary.SetPersonaFragment(fragment) }

// SetGroundCompleteFragment forwards to the primary (the RESPOND role runs on the primary) — explicit
// for the same interface-embedding reason as SetPersonaFragment: WITHOUT it a tiered config (the
// --backend claude bridge returns a TieredBackend) would silently drop the grounding-completeness
// directive and THOUGHT_GROUND_COMPLETE would never engage on the bench substrate.
func (t *TieredBackend) SetGroundCompleteFragment(fragment string) {
	t.Primary.SetGroundCompleteFragment(fragment)
}

// FormalizeExpression forwards to the primary (the 5th-axis solver formalization is a reasoning role,
// not a utility one — backends.StructureFormalizer). This MUST be explicit for the same
// interface-embedding reason as Comprehend / SetGroundCompleteFragment: the embedded field is the
// interface type backends.Backend, which does NOT declare StructureFormalizer, so WITHOUT this forwarder
// the engine's `e.backend.(backends.StructureFormalizer)` assertion would fail on the --backend claude
// bridge (which returns a TieredBackend), the formalizer would be nil, and the SolverPrimitiveSubAgent would
// stay DARK on the claude substrate even with subconscious.solver_specialist ON — the exact silent-drop
// the SetGroundCompleteFragment forwarder was added to prevent.
func (t *TieredBackend) FormalizeExpression(ctx []types.Thought) (expr string, operands []string, ok bool) {
	return t.Primary.FormalizeExpression(ctx)
}

// compile-time interface checks — a TieredBackend is a drop-in for a bare OpenAICompatBackend.
var (
	_ backends.Backend                = (*TieredBackend)(nil)
	_ backends.StructureFormalizer    = (*TieredBackend)(nil)
	_ backends.Decider                = (*TieredBackend)(nil)
	_ backends.Intender               = (*TieredBackend)(nil)
	_ backends.SpecialistCaller       = (*TieredBackend)(nil)
	_ backends.FilterEscalator        = (*TieredBackend)(nil)
	_ backends.SufficiencyJudge       = (*TieredBackend)(nil)
	_ backends.RealityComprehender    = (*TieredBackend)(nil)
	_ backends.LegiblePrompter        = (*TieredBackend)(nil)
	_ backends.PersonaPrompter        = (*TieredBackend)(nil)
	_ backends.GroundCompletePrompter = (*TieredBackend)(nil)
	_ backends.EmitBinder             = (*TieredBackend)(nil)
	_ backends.SchedulerBinder        = (*TieredBackend)(nil)
	_ backends.DisplayNamer           = (*TieredBackend)(nil)
)
