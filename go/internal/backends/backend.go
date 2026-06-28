// Package backends defines the swappable "language faculty" (CONTENT roles) the architecture's
// CONSCIOUS layer, the hidden seam and the Critic use — generate an effortful thought, re-voice an
// injection, summarise to gist, write the user-facing answer, apply one operator, and (toolmaker
// path) synthesise a workflow program. It also carries the Filter's Pattern-C ESCALATION judgment
// (the optional model ceiling above the deterministic admission floor).
//
// The CONTROL roles (the admission FLOOR + candidate ranking) are NOT here: they are pure
// Pattern-A math and live in internal/control (the Tier-1 leaf the production path calls directly).
// A backend never owns control; it owns CONTENT plus the one escalation judgment.
//
// Domain competence (arithmetic, simulation, memory) lives in the SPECIALISTS, not here.
//
// TestBackend (test.go) is deterministic and offline. It is NOT the product default — the harness
// thinks with a real model (see internal/llm ResolveSubstrate); the test double survives for the
// CONTENT roles (pinned by the tests and the golden scenarios), so the cognitive-property tests stay
// reproducible without a live model. It is CONTENT-only: its admission/ranking behaviour is
// delegated to internal/control, not owned here.
// internal/llm's OpenAICompatBackend is the real Stage-1 substrate; it slots in behind this
// exact interface.
//
// Tier-1 leaf discipline: this package imports only types, events and scheduler. It does NOT
// import cognition/synth — SynthesizeProgram returns the RAW map[string]any program dict, and
// TestBackend defers shape recognition to an injected ShapeRecognizer closure wired at
// engine construction (the Go break for Python's lazy `from .synth import recognize_shape`,
// the one hidden Tier1->Tier4 inversion; see DESIGN §2.3).
package backends

import (
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Backend is the core CONTENT interface every backend (test double + LLM) implements: the
// language-faculty roles plus the toolmaker SynthesizeProgram and the AppraiserName tag. The
// CONTROL roles (ScoreAdmit/Rank) are NOT here — they are Pattern-A math in internal/control,
// which the production path calls directly. The Filter's model-backed admission contribution is
// the optional FilterEscalator capability (below), never a core method. Pinned from the CONTENT
// intersection of TestBackend + OpenAICompatBackend (DESIGN §2.3).
type Backend interface {
	// Generate is the CONSCIOUS serial effortful loop — the next GENERATED thought. The
	// seeded *cpyrand.Random is threaded in (never a package-global) so the stream is
	// reproducible AND byte-identical to CPython's random.Random draw sequence.
	Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string

	// Wander is the AWAKE-mode idle content role: it authors ONE short first-person idle thought
	// (the endogenous baseline μ — the curiosity musings, default-mode associations, and drive-goal
	// seed text the awake stream lives on). It REPLACES the old hardcoded string pools (those were
	// manufactured intelligence; content roles = the model — feedback-heuristic-control-only):
	//   - kind=="curiosity"   — a reflective, NOT action-inviting idle musing (a curiosity goal seed);
	//   - kind=="association" — a spontaneous default-mode wander (a loose connection that surfaced);
	//   - kind=="develop"     — a development-agenda DRIVE goal aimed at the domain carried in hint.
	// hint carries the domain for "develop" (e.g. "science, maths and engineering"); "" otherwise.
	// The seeded *cpyrand.Random is threaded so the test double can rotate its offline pool
	// deterministically (golden stability) while the real backend ignores it. On model decline it
	// returns "" (DARK) — the SAME contract as Generate; the caller goes dark (returns nil / empty),
	// never a canned substitute. The test double (the legitimate offline-content home) returns
	// deterministic VARIED content rotated by rng (never a constant — the diversity property holds).
	Wander(kind, hint string, ctx []types.Thought, rng *cpyrand.Random) string

	// Transform re-voices a raw return into the self-narrative's first-person format
	// (the hidden seam's TRANSFORM step).
	Transform(c types.Candidate, hist []types.Thought) string

	// Summarize compresses a branch to gist (lossy by design).
	Summarize(ts []types.Thought) string

	// Respond synthesises the user-facing answer from the resolved thought graph (an ACTION).
	Respond(goal string, ctx []types.Thought) string

	// OperatorApply lets a runtime sub-agent apply one operator to the context (its scoped
	// job). Returns the synthesised content of that single cognitive move.
	OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string

	// EmitVerdict is the decision-CONCLUSION role (A2): after a Deliberator/Verifier worker has
	// reasoned (the priorReasoning carries its accumulated decompose/compare/contrast/rank
	// output), ask it to STATE its final verdict in a fixed, machine-readable shape — a single
	// last line `VERDICT: <label>`. This is DISTINCT from OperatorApply, whose prompt forbids
	// concluding the task ("do not solve the whole task"); a deliberation that ends in `rank`
	// produces a ranking, not a stated pick, so a verdict needs its own contract.
	//
	// worker is the worker kind ("deliberator"|"verifier"); optionLabels are the choosable labels
	// ONLY (the option IDs/names a deliberator may pick — NEVER the criteria weights or the
	// ground-truth winner, so the call cannot leak the answer; for a verifier the labels are the
	// fixed accept|refuse|cannot-verify set the caller supplies). The returned string is the
	// model's raw response; the caller parses the last `VERDICT:` line with parseVerdictLine and
	// falls back to the prose parser only when no well-formed line is present. A CONTENT role: on
	// model decline it surfaces the gap (returns "") — never a substituted verdict.
	EmitVerdict(worker, goal string, optionLabels []string, priorReasoning string) string

	// SynthesizeProgram is the toolmaker path: WRITE a workflow program (control-flow tree of
	// operators) for this goal, optionally minting new operators. Returns the RAW
	// {operators?, program, rationale, source} dict and ok=true, or (nil, false) to defer to
	// the deterministic shape recogniser. The raw-dict return keeps backends a Tier-1 leaf
	// (no cognition import); synth parses/verifies it. Python returned `dict | None`; the
	// trailing bool carries the None.
	SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool)

	// AppraiserName reports who appraised — tagged onto captured Appraisals (P6). Python read
	// this off the backend as `getattr(backend, 'appraiser_name', 'heuristic')`; promoted to a
	// method so every backend declares it.
	AppraiserName() string
}

// ----------------------------------------------------------------------------
// Optional capabilities — Python `hasattr(backend, …)` probes become small SEPARATE
// interfaces, asserted at runtime with `if x, ok := b.(Decider); ok` (mirroring hasattr
// exactly and avoiding a fat core interface). Only the LLM backend implements these; the
// call sites branch on presence.
//
// Signatures are re-derived from the REAL llm.py. The trailing bool / "" is Python's
// `| None` "model declined → fall back" signal the call sites branch on; dropping it loses
// behaviour.
// ----------------------------------------------------------------------------

// Decider lets the Controller ask the model to choose the next move. choice=="" is Python's
// None (model declined / produced an off-list choice); the controller maps a non-empty choice
// via Decision[choice]. why is the one-line rationale captured alongside.
type Decider interface {
	Decide(goal string, ctx []types.Thought, options []string) (choice, why string)
}

// Intender lets the model distil the active branch into an intention. It returns the intention
// text AND its kind (run/send/measure/edit/…); ok=false is Python's None (fall back to the
// regex router).
type Intender interface {
	Intention(goal string, ctx []types.Thought) (text, kind string, ok bool)
}

// SpecialistCaller lets a sub-agent delegate a domain move to the model. It takes the
// sub-agent's description (its scoped job); ok=false is Python's None (fall back to the
// deterministic operator application).
type SpecialistCaller interface {
	Specialist(domain, description string, ctx []types.Thought) (string, bool)
}

// RealityComprehender is the LLM "to_operator" step — ONE structured call that translates the live
// thinking into the reality OBSERVATION it calls for: which capability (read|search|run|none) AND the
// concrete TARGET (the file path to read, the pattern to search, the command to run). This is the
// unification that replaces the read/search/run primitives' KEYWORD triggers AND the regex target
// extraction (feedback-heuristic-control-only: "reality via real tools-as-agent" — the AGENT decides
// what to observe AND on what, not a regex). It is the fix for two layers of the grounding bug:
//
//   - a thought "I need to read risk.yaml" fires the read even without the literal trigger "read the file";
//   - and after the conscious self-corrects ("...read config/risk.yaml"), the TARGET is the corrected
//     path the model intends — NOT whatever the context's first path-like token happens to be.
//
// need=="none" (or target=="" for a read/search) ⇒ no observation this tick. ok=false ⇒ the model declined
// / no model ⇒ the caller falls back to the keyword-trigger + regex FLOOR (so the deterministic test
// double keeps goldens byte-identical — a TestBackend simply does not implement this interface).
type RealityComprehender interface {
	Comprehend(ctx []types.Thought) (need, target string, ok bool)
}

// FilterEscalator is the Filter's Pattern-C ESCALATION judgment (the optional model CEILING above
// the deterministic admission floor). It lets the model judge a flagged-fuzzy admission the
// floor's lexical signals were unsure about: "is this candidate a laundered hallucination the
// floor missed?". It is given the FLOOR's own verdict so the model REFINES rather than re-derives.
// ok=false is "model declined / off-shape" -> the floor stands (Rule 4: surfaced, never silent).
// Only the LLM backend implements it; the test double does NOT (so a flagged-fuzzy case in
// control mode simply lets the floor stand). The escalation NEVER substitutes a deterministic
// stand-in — on decline it returns ok=false and the caller keeps the floor verdict.
type FilterEscalator interface {
	JudgeAdmission(c types.Candidate, hist []types.Thought, floor types.FilterVerdict) (types.FilterVerdict, bool)
}

// SufficiencyJudge is the CRAG-style sufficiency gate's Pattern-C model CEILING above the deterministic
// coverage FLOOR (A-RAG1). It lets the model judge a flagged-fuzzy retrieval the floor's lexical
// coverage could not decide: "given this NEED, does this RECALLED FUEL actually cover it well enough to
// commit on, or should the harness ABSTAIN?". It is told the floor's own verdict ("sufficient" /
// "ambiguous" / "insufficient") so the model REFINES rather than re-derives. It returns one of those
// three verdict strings + ok. The model may MOVE the verdict in either direction (lift an ambiguous to
// sufficient when the fuel clearly answers the need, OR lower it to insufficient when the recall is
// off-topic despite lexical overlap) — but the caller NEVER escalates a structural floor verdict (a
// grounded clear-sufficient or a clear-insufficient), so the model is consulted only on genuinely fuzzy
// retrievals. ok=false is "model declined / off-shape / no model" -> the floor stands (Rule 4: surfaced
// via escalation.floor_stands, never silent). Only the LLM backend implements it; the test double does
// NOT (so a flagged-fuzzy case in control mode simply lets the floor stand, byte-identical). It NEVER
// substitutes a deterministic stand-in. Verdict strings are primitives so this interface stays free of
// any control import (the same Tier-1 discipline FilterEscalator keeps).
type SufficiencyJudge interface {
	JudgeSufficiency(query, fuelText, floorVerdict string) (verdict string, ok bool)
}

// ConscienceJudge is the conscience gate's Pattern-C model CEILING above the deterministic VetAction
// floor (02 §7.2, slice k ceiling). The floor already REFUSED the hard prohibitions; this judges a
// flagged-fuzzy action the floor ALLOWED but that warrants a nuanced good/bad look (a soft cue —
// delete/overwrite/publish/send/external). The model may only TIGHTEN (refuse) — it can never loosen a
// floor refusal (that already happened). allow=true keeps the act; allow=false refuses it with reason.
// ok=false is "model declined / no model" -> the floor's allow stands (Rule 4: surfaced, never silent).
// Only the LLM backend implements it; the test double does NOT (so a fuzzy case lets the floor stand,
// byte-identical). It NEVER substitutes a deterministic stand-in.
type ConscienceJudge interface {
	JudgeConscience(actionText string) (allow bool, reason string, ok bool)
}

// AcceptanceJudge is the goal-Acceptance Pattern-C model CEILING above the deterministic acceptance
// markers (02 §1.6). When the markers are ambiguous (neither clearly met nor clearly unmeetable), the
// model judges the outcome: outcome is one of "met" / "unmeetable" / "continue". ok=false is "model
// declined / no model" -> the deterministic marker verdict stands. Only the LLM backend implements it.
type AcceptanceJudge interface {
	JudgeAcceptance(goal string, ctx []types.Thought) (outcome string, ok bool)
}

// EngagementJudge is the awake-engagement Pattern-C model CEILING above the deterministic engagement
// FLOOR (AWAKE-DISP rung 2, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md §rung-2). The floor
// (rung 0's cognition.RecognizeShape) already decided the OBVIOUS cases: a clearly task-shaped awake user
// line engages the subconscious (synthesise a workflow), a clearly trivial line (a short greeting /
// chitchat) does not (the floor stands ⇒ the normal social/respond path). This judges only the FUZZY
// MIDDLE: a focused, unresolved awake user line that is NOT lexically task-shaped yet is substantive
// enough that it MIGHT be worth a full subconscious round-trip — "is this worth engaging the subconscious
// on, or is it ambient and best answered the light way?". It is told the floor's own verdict ("quiet" —
// the floor never reached "engage" in this band, by construction) so the model REFINES rather than
// re-derives, and it may only LIFT the decision to "engage" (it never quiets a task-shaped line — that
// never reaches here). It returns "engage" or "quiet" + ok. ok=false is "model declined / off-shape / no
// model" -> the floor stands (Rule 4: surfaced via escalation.floor_stands, never silent). Only the LLM
// backend implements it; the test double does NOT (so a fuzzy case in control mode simply lets the floor
// stand, byte-identical). It NEVER substitutes a deterministic stand-in. Verdict strings are primitives so
// this interface stays free of any control import (the same Tier-1 discipline the other escalators keep).
// COST GUARD: the engine consults this ONLY on the deterministically-flagged fuzzy band (the floor handles
// the obvious cases) and only on a TASK-shape FALSE for a still-unresolved user line — never every tick.
type EngagementJudge interface {
	JudgeEngagement(goal, recentContext, floorVerdict string) (verdict string, ok bool)
}

// AnswerSupportJudge is the answer-verifier's Pattern-C model CEILING above the deterministic support
// FLOOR (T2.1, the INDEPENDENT verifier — docs/internal/notes/2026-06-23-cognitive-engine-capability-audit.md
// P1; Huang 2024 arXiv:2310.01798 "LLMs Cannot Self-Correct Reasoning Yet"). Before the harness COMMITS
// a final factual answer, the verifier re-retrieves web evidence (an INDEPENDENT signal — the world, not
// a model re-read of its own chain) and the floor checks whether that evidence literally contains/aligns
// with the answer. On a flagged-fuzzy case (the evidence is present and topical but the lexical overlap is
// inconclusive) this lets the model judge "does THIS independently-retrieved evidence SUPPORT this
// answer?" — given the floor's own verdict ("supported" / "unverifiable" / "unsupported") so the model
// REFINES rather than re-derives. The INDEPENDENCE GUARANTEE holds because the model judges the answer
// against the RE-RETRIEVED EVIDENCE, never against the original reasoning chain — it never re-reads its own
// work, so it is not the same-model self-correction the literature shows cannot fix a systematic bias.
// It returns one of the three verdict strings + ok. ok=false is "model declined / off-shape / no model" ->
// the floor stands (Rule 4: surfaced via escalation.floor_stands, never silent). Only the LLM backend
// implements it; the test double does NOT (so a fuzzy case in control mode simply lets the deterministic
// floor stand, byte-identical + offline-deterministic). It NEVER substitutes a deterministic stand-in.
// Verdict strings are primitives so this interface stays free of any control import (the same Tier-1
// discipline the other escalators keep).
type AnswerSupportJudge interface {
	JudgeAnswerSupport(question, answer, evidence, floorVerdict string) (verdict string, ok bool)
}

// StructureFormalizer is the 5th-axis classical solver's Pattern-B formalization step (the orchestrate-
// vs-compute split, PAL/Logic-LM; docs/internal/notes/2026-06-19-specialized-component-registry-axis.md §5). It
// is the model's ONE job for a structured arithmetic sub-problem: write the EXPRESSION STRUCTURE — the
// operators/shape with NAMED operand placeholders (a, b, c, ...), e.g. "min(a*b, c)" or "a+b+c" — and
// NEVER the literal numbers. The SolverPrimitiveSubAgent then binds each named operand to a GROUNDED READ (an
// OBSERVATION-sourced thought in the context); a deterministic math/big evaluator computes. This is the
// load-bearing safety boundary: the model is allowed to be wrong about the SHAPE (cheap-checkable via the
// AST parse-validate + the grounded-operand bind), but it can never inject a number — so a mis-formalize
// degrades to "fires nothing" or a flagged mis-bind, never a confident-wrong computed answer.
//
// operands is the ordered set of named placeholders the expr references (so the specialist knows what to
// bind). ok=false is "model declined / no model / off-shape" -> the specialist fires NOTHING (no
// deterministic stand-in number — that would be manufactured intelligence). Only a real model implements
// it; the test double does NOT (so the specialist stays dark on the test backend even with the opt-in knob
// ON, keeping goldens byte-identical). The offline mechanism tests use a small test-local formalizer.
type StructureFormalizer interface {
	FormalizeExpression(ctx []types.Thought) (expr string, operands []string, ok bool)
}

// EmitBinder lets the engine wire the event bus into the backend (llm.bind_emit) so model
// calls emit llm.call / llm.fallback events.
type EmitBinder interface {
	BindEmit(emit events.Emit)
}

// SchedulerBinder lets the engine wire the LLM-call scheduler into the backend
// (llm.bind_scheduler) so background calls obey the per-tick budget.
type SchedulerBinder interface {
	BindScheduler(s *scheduler.LLMScheduler)
}

// DisplayNamer reports a human-readable backend name for the UI (llm.display_name).
type DisplayNamer interface {
	DisplayName() string
}

// LegiblePrompter lets the engine ask the language faculty to APPEND an in-band control-tag
// instruction to the conscious Generate prompt (the WF-E CC-1 legible-generation SHADOW instrument,
// 05-LEGIBLE-GENERATION §5b/§5c). The fragment is registry-derived (the one-source-of-truth contract)
// and set per-tick from the LIVE seam.legible_generation toggle: "" clears it, so when the toggle is
// OFF the Generate prompt is byte-identical to before the instrument existed. Only the LLM backend
// implements it (the test double does not — its Generate ignores the prompt), so the heuristic/test
// path is unaffected by construction.
type LegiblePrompter interface {
	SetLegibleFragment(fragment string)
}

// PersonaPrompter lets the engine ask the language faculty to APPEND the learned person-adaptation
// instruction to the outward-facing RESPOND prompt (P7.3 user adaptation: consistent style feedback
// becomes a learned preference that changes future answers). The fragment is set right before a
// respond and "" clears it — with no learned preferences the prompt is byte-identical to before the
// seam existed. Only the LLM backend implements it (the test double does not), so the offline/golden
// path is unaffected by construction. Same discipline as LegiblePrompter above.
type PersonaPrompter interface {
	SetPersonaFragment(fragment string)
}

// GroundCompletePrompter lets the engine ask the language faculty to APPEND a grounding-completeness
// reading directive to the outward-facing RESPOND prompt when THOUGHT_GROUND_COMPLETE is ON. The
// directive is GENERAL (no enumerated trigger keywords): before answering, use the value actually IN
// FORCE — when a later statement in the material corrects/replaces/overrides an earlier one, the later
// value wins over the first name-match — and apply an in-material adjustment/conversion to the base
// value; AND it PRESERVES the never-fabricate discipline (a value only referenced via an unreadable
// external pointer ⇒ DECLINE, never invent — protecting the anti-confabulation decline). The fragment
// is set right before a respond and "" clears it — with the flag OFF (the default) the RESPOND prompt
// is byte-identical to before this seam existed. Only the LLM backend implements it (the test double
// does not), so the offline/golden path is unaffected by construction. Same discipline as
// PersonaPrompter above. Pattern B: this shapes how the MODEL reads — it never hardcodes an answer.
type GroundCompletePrompter interface {
	SetGroundCompleteFragment(fragment string)
}
