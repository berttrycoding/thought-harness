// workflow.go ports subconscious/workflow.py — a recognised, on-the-fly PROGRAM of operators, run
// phase by phase.
//
// A workflow is no longer a hand-written linear template: it wraps a Program (program.go) that the
// synthesiser (synth.go) constructs from context — operators composed in series / parallel / loop.
// This file runs that program one phase-group at a time in the tick loop: a sequential step is one
// phase; a parallel group is one phase that instantiates *several* sub-agents at once; a loop is
// unrolled to its (bounded) iteration count. Each phase fills with SubAgents — proper sub-agentic
// objects (role/persona/responsibility/tool-scope) defined at runtime — and biases the Gate per the
// operator's family.
//
// PORT NOTE (Tier 5). A stateful phase-cursor over a SCHEDULED Program. FromProgram maps each
// PhasePlan -> Phase (cognition.ToEnum). Instantiate builds per-step SubAgents wiring executor +
// cognition. Recognize MUTATES the cached `recognized` flag (read by GateBias) — the call ordering
// (Recognize before GateBias each tick) is load-bearing and preserved. The Gate-bias maps mirror
// Python _OP_BIAS exactly.
package subconscious

import (
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// opBias is the per-operator Gate bias: which primitive DOMAINS are privileged while this operator's
// phase is current. After the representation-space rebuild (M2) the privileged domains are the REAL
// primitives, not the deleted fakes: the risk-flagging stance is `skeptic` (was the canned `safety`),
// the change-is-sound stance is `advocate` (was the canned `refactor`). A generative phase nudges the
// advocate (it is proposing a candidate to defend); a validate/measure phase nudges the skeptic (the
// Critic's checking stance), so the fork-on-conflict still gets a directional gate nudge. A missing
// operator ⇒ fall back to the operator's FAMILY prior (familyBias) iff the operator was MINTED.
var opBias = map[string]map[string]float64{
	"decompose": {"decompose": 0.3},
	"generate":  {"advocate": 0.2},
	"validate":  {"skeptic": 0.4}, // Critic / validate privileged this phase
	"measure":   {"skeptic": 0.3},
}

// familyBias is the per-FAMILY Gate prior a runtime-MINTED operator inherits when it has no specific
// opBias entry (sweep #4): a synthesised operator is otherwise born with zero bias. The priors track
// the seeded per-op biases under the real-primitive roster — transformative/generative work privileges
// the `advocate` stance (proposing/defending a candidate), relational (compare/measure/validate/rank)
// privileges the `skeptic` stance (checking), primitives nudge `decompose`, and a bare 'synthesized'
// op gets a modest `advocate` default. Applied ONLY to minted operators (OperatorSpec.Synthesized).
var familyBias = map[string]map[string]float64{
	"transformative": {"advocate": 0.2},
	"relational":     {"skeptic": 0.3},
	"generative":     {"advocate": 0.2},
	"primitive":      {"decompose": 0.2},
	"synthesized":    {"advocate": 0.2},
}

// sourceBias is the SOURCE-AWARE Gate nudge (M5, representation-space-rebuild.md §1.4): a step that
// declares which sourcing-ladder rung it privileges (Step.Source — the path skills' annotation) gets an
// ADDITIVE bias toward the primitive whose lane that source belongs to. It is a privilege, not a route:
// the sourcing ladder still walks its strict order; this only tilts the Gate so the privileged source's
// stance is more likely to win admission while that phase is current. The mapping:
//
//	reality / compute  -> the `skeptic` stance + the `validate` checking lane (EVIDENCE privileges the
//	                      checking/grounding stance — a deduction/analogy validate step wants ground truth).
//	memory             -> the `recall` primitive (a recall/analogize step privileges first-person memory).
//	knowledge / present -> the `recall` lane too (the cheap grounded stores share the recall stance).
//	store              -> `curate` (the induction terminus privileges the store-curation verb).
//	model ("")         -> NO nudge (the bare recombination rung carries no privilege).
//
// It is summed ON TOP of the operator's opBias/familyBias (a source nudge does not replace the move's own
// bias), so a `validate`@reality step is biased toward BOTH the skeptic checking lane (opBias) and reality
// (sourceBias) — the path's grounded close gets a doubled evidence nudge.
var sourceBias = map[string]map[string]float64{
	"reality":   {"skeptic": 0.2, "validate": 0.2},
	"compute":   {"skeptic": 0.2, "validate": 0.2},
	"memory":    {"recall": 0.3},
	"knowledge": {"recall": 0.2},
	"present":   {"recall": 0.1},
	"store":     {"curate": 0.2},
}

// biasForStep resolves the full Gate bias for one step: the operator's move bias (biasFor) PLUS the
// step's source-aware nudge (sourceBias for its Step.Source). The source nudge is additive — it sums into
// the same map so a step privileging reality is tilted toward both the checking lane and reality. An
// un-annotated step (Source==model/"") gets only the operator bias, so seeded/golden programs that carry
// no Source are byte-identical to before this change.
func biasForStep(st cognition.Step, catalog *cognition.OperatorRegistry) map[string]float64 {
	bias := biasFor(st.Operator, catalog)
	if st.Source != cognition.SourceModel {
		for dom, v := range sourceBias[st.Source] {
			bias[dom] += v
		}
	}
	return bias
}

// biasFor resolves the Gate bias for one operator phase: a specific opBias entry wins; otherwise a
// MINTED operator inherits its family prior (familyBias); a seed operator with no named bias gets none.
// catalog may be nil (a bare/template workflow with no catalog) ⇒ only opBias applies.
func biasFor(opName string, catalog *cognition.OperatorRegistry) map[string]float64 {
	bias := map[string]float64{}
	if specific, ok := opBias[opName]; ok {
		for k, v := range specific {
			bias[k] = v
		}
		return bias
	}
	if catalog != nil {
		if spec, found := catalog.Get(opName); found && spec.Synthesized {
			for k, v := range familyBias[spec.Family] {
				bias[k] = v
			}
		}
	}
	return bias
}

// Phase is one scheduled phase-group of the program (>1 step ⇒ parallel fan-out). Mirrors the Python
// @dataclass Phase; Bias defaults to an empty map (Python field(default_factory=dict)).
type Phase struct {
	Operator types.Operator      // enum (back-compat; stamped on the resulting Thought)
	OpName   string              // the operator name driving this phase
	Plan     cognition.PhasePlan // the scheduled phase-group from program.Schedule()
	Bias     map[string]float64  // Gate bias while this phase is current
}

// Workflow is a recognised program, run phase by phase; convertible to an auto-firing macro. Mirrors
// the Python Workflow class — a stateful cursor (i) plus the cached recognized flag that Recognize
// mutates and GateBias reads.
type Workflow struct {
	Name       string
	Phases     []Phase
	Program    *cognition.Program          // the wrapped program (nil ⇒ a bare canonical/template workflow)
	Bespoke    bool                        // synthesised for THIS goal ⇒ always recognised
	Triggers   []string                    // lower-cased keyword triggers (canonical workflows gate on these)
	Catalog    *cognition.OperatorRegistry // the operator catalog instantiate consults (nil ⇒ no sub-agents)
	Backend    backends.Backend            // language faculty handed to each instantiated sub-agent
	Goal       string                      // the episode goal stamped on every sub-agent
	Emit       events.Emit                 // bus closure handed to each sub-agent (nil ⇒ no events)
	i          int                         // the phase cursor (may go one past the end ⇒ exhausted)
	recognized bool                        // cached flag set by Recognize, read by GateBias

	// scope is the §3.3a authority CEILING the producing Capability sourced (gap 4): when set, every
	// SubAgent Instantiate staffs is given this ceiling (.WithScope), so its tool picks resolve within the
	// run's least-privilege band (ScopedToolScope). nil ⇒ no ceiling (the flat toolScope — byte-identical).
	scope *Scope
	// context is the §3.11 rich Context the producing Capability captured (gap 2/3): when set, every
	// staffed SubAgent reads the WHOLE frozen branch snapshot instead of the ≤5 slice. nil ⇒ the ≤5 slice.
	//
	// It is the FALLBACK capture (episode-open) — the recapture closure below supersedes it at staffing
	// time when wired, so a mid-episode worker sees the GROWN branch, not the goal root the episode-open
	// capture froze (the gap-2 live-claude fix: an episode-open snapshot starves a mid-episode worker).
	context *Context
	// recapture RE-CAPTURES the §3.11 Context against the LIVE graph at STAFFING time (gap-2 fix part 1):
	// Instantiate calls it so each staffed worker sees the active branch AS IT IS WHEN STAFFED — which has
	// grown past the goal root — instead of the frozen episode-open snapshot. nil ⇒ use the static context
	// above (byte-identical for any caller that does not wire a recapture; the engine wires it only on the
	// capability-ON path). The closure is supplied by the engine (it owns the live graph + the Capability's
	// capture), and is DETERMINISTIC (graph-derived, no clock/RNG). It returns nil to mean "no live capture
	// available this staffing" (e.g. a graph-less call), in which case the static context is used.
	recapture func() *Context
	// picker is the §3.6/§3.7 CATEGORY-SOURCED tool-pick resolver (gap-5 load-bearing half): when set, each
	// staffed SubAgent's concrete tool set comes from picker.Resolve(spec, scope) — the operator's coarse
	// category footprint resolved against the live registry + the worker-faculty footprint — INSTEAD of the
	// operator's flat OperatorSpec.ToolScope. So the operator's tool name-list is no longer the SOURCE (the
	// gap-9 prerequisite). nil ⇒ the flat ToolScope is used (byte-identical for any caller that does not wire
	// a picker; the engine wires it only on the capability-ON path). The resolved set is byte-identical to the
	// flat ToolScope for the seed ops (toolpick.go parity), so the wire preserves behaviour when flipped on.
	picker *ToolPicker
	// queryFormulation is the subconscious.query_formulation gate (T1.1) stamped onto every staffed SubAgent:
	// when true, a worker's web_search query is formulated from the actual question (the instruction wrapper
	// stripped) instead of the whole goal verbatim. false (the default) ⇒ the raw trimmed goal, byte-identical.
	queryFormulation bool
}

// WithQueryFormulation sets the subconscious.query_formulation gate (T1.1) stamped onto every SubAgent this
// workflow staffs, and returns the workflow for chaining. Default false ⇒ byte-identical (workers search the
// raw trimmed goal). The engine threads the config flag in at every SetWorkflow site.
func (w *Workflow) WithQueryFormulation(on bool) *Workflow { w.queryFormulation = on; return w }

// WithStaffing attaches the Capability-sourced authority CEILING (§3.3a Scope, gap 4), the captured
// §3.11 Context (gap 2/3), and the §3.6/§3.7 category-sourced tool-pick resolver (gap 5) the workflow
// staffs every SubAgent with. All are optional (nil ⇒ today's behaviour for that arm), so a workflow with
// no staffing set is byte-identical to before. Returns the workflow for chaining. This is the seam the
// Capability/engine threads the eager ceiling + rich context + the tool source through to the workers —
// replacing the operator's flat ToolScope inheritance as the tool SOURCE (gap 5) and the ≤5 slice.
//
// context is the episode-OPEN capture (the FALLBACK); recapture (optional) re-captures the Context against
// the LIVE graph at STAFFING time (gap-2 fix), so a mid-episode worker sees the GROWN branch rather than
// the goal root the episode-open snapshot froze. A nil recapture ⇒ the static context is used at every
// staffing (the prior behaviour). recapture is supplied by the engine (it owns the live graph) and must be
// deterministic.
//
// picker is the gap-5 tool SOURCE: when set, each worker's concrete tool set is resolved by category from
// the live registry + the worker-faculty footprint (picker.Resolve), NOT inherited from the operator's flat
// ToolScope. A nil picker ⇒ the flat ToolScope is used (byte-identical). The resolved set is unchanged for
// the seed ops (toolpick.go parity), so flipping the wire on preserves behaviour.
func (w *Workflow) WithStaffing(scope *Scope, context *Context, recapture func() *Context, picker *ToolPicker) *Workflow {
	w.scope = scope
	w.context = context
	w.recapture = recapture
	w.picker = picker
	return w
}

// staffingContext resolves the §3.11 Context a worker is staffed with THIS instantiation: the live
// staffing-time recapture when wired (gap-2 fix — the GROWN branch, not the episode-open goal root), else
// the static episode-open context (the fallback). A recapture that returns nil (no live capture available)
// also falls back to the static context, so a worker always gets the best Context available. nil ⇒ no
// Context (the ≤5 slice path — byte-identical for a workflow with no staffing).
func (w *Workflow) staffingContext() *Context {
	if w.recapture != nil {
		if live := w.recapture(); live != nil {
			return live
		}
	}
	return w.context
}

// Scope returns the workflow's attached §3.3a ceiling (nil ⇒ none) — the read accessor a wiring-gate test
// reads to confirm the Capability sourced and threaded a Scope into staffing.
func (w *Workflow) Scope() *Scope { return w.scope }

// ToolPicker returns the workflow's attached §3.6 category-sourced tool resolver (nil ⇒ none) — the read
// accessor a wiring-gate test reads to confirm the engine threaded the category source into staffing (the
// gap-5 wire). With it set, Instantiate sources each worker's tools from the category footprint, not the
// operator's flat ToolScope.
func (w *Workflow) ToolPicker() *ToolPicker { return w.picker }

// NewWorkflow builds a Workflow with the Python keyword-construction shape:
// Workflow(name, phases, *, bespoke, program=…, triggers=…, catalog=…, backend=…, goal=…, emit=…).
// triggers are lower-cased (Python tuple(t.lower() for t in triggers)). i starts at 0 and recognized
// at false. Most callers use FromProgram; this mirrors the full Python __init__ for a canonical
// (template) workflow.
func NewWorkflow(name string, phases []Phase, bespoke bool, program *cognition.Program,
	triggers []string, catalog *cognition.OperatorRegistry, backend backends.Backend,
	goal string, emit events.Emit) *Workflow {
	return &Workflow{
		Name:     name,
		Phases:   phases,
		Program:  program,
		Bespoke:  bespoke,
		Triggers: lowerAll(triggers),
		Catalog:  catalog,
		Backend:  backend,
		Goal:     goal,
		Emit:     emit,
	}
}

// FromProgram builds a Workflow from a synthesised program (Python Workflow.from_program). It
// schedules the program into PhasePlans, maps each to a Phase (operator name -> the typed enum via
// cognition.ToEnum, defaulting to "generate" for an empty step list), names the workflow by the
// program's control-flow shape (synthesised) or the canonical "design-build-validate" label, and
// marks it bespoke iff the program was synthesised. Goal falls back to the program's own goal.
func FromProgram(program *cognition.Program, catalog *cognition.OperatorRegistry,
	backend backends.Backend, emit events.Emit, goal string) *Workflow {
	var phases []Phase
	for _, plan := range program.Schedule() {
		opName := "generate"
		var bias map[string]float64
		if len(plan.Steps) > 0 {
			opName = plan.Steps[0].Operator
			// source-aware bias (M5): the representative step's Step.Source adds a nudge toward the
			// privileged source's lane on top of the operator's move bias. An un-annotated step (the
			// common case, and every golden program) gets only biasFor — byte-identical to before.
			bias = biasForStep(plan.Steps[0], catalog)
		} else {
			bias = biasFor(opName, catalog)
		}
		phases = append(phases, Phase{
			Operator: cognition.ToEnum(opName),
			OpName:   opName,
			Plan:     plan,
			Bias:     bias,
		})
	}
	name := "design-build-validate"
	if program.Synthesized {
		name = program.Shape()
	}
	g := goal
	if g == "" {
		g = program.Goal
	}
	return &Workflow{
		Name:    name,
		Phases:  phases,
		Program: program,
		Bespoke: program.Synthesized,
		Catalog: catalog,
		Backend: backend,
		Goal:    g,
		Emit:    emit,
	}
}

// -- the engine interface (unchanged shape) -----------------------------

// Recognize decides whether this workflow applies to the current context AND MUTATES the cached
// recognized flag (read by GateBias). A bespoke program was synthesised for this very goal ⇒ it
// applies until its phases run out; a canonical/template workflow still gates on its keyword
// triggers. Mirrors Python recognize — the assignment to self.recognized then return is preserved
// so the ordering (Recognize before GateBias) stays load-bearing.
//
// LEGACY(redesign): the binary keyword self-trigger — the recognition path used when no Capability
// recognizer is wired (subconscious.capability_dispatch OFF). The redesign routes recognition through the
// producing Capability's recognizeViaGraded (the permissive has-any predicate, NOT θ-gated — see below) —
// removable when the 4 redesign flags are retired (recognizeViaGraded then recognises non-bespoke
// workflows, with the SAME has-any criterion this binary path uses, so the verdict is unchanged).
func (w *Workflow) Recognize(ctx []types.Thought) bool {
	w.recognized = !w.Exhausted() && (w.Bespoke || hasAny(ctxTextDefault(ctx), w.Triggers))
	return w.recognized
}

// recognizeViaGraded is the GAP 5-DEEPER recognition predicate routed through the producing Capability: it
// answers "does this workflow APPLY" with the PERMISSIVE has-any criterion `gradedRelevance(stream,
// Triggers) > 0` — the SAME relevance criterion as the binary `has-any` of Recognize (01-subconscious §3.3
// / §2.1: the relevance PULL). theta is carried on the signature but NOT consulted (it is the downstream
// value/admission bar — θ-gating recognition is the refuted double-gate, see the WHY NOT block below). On
// the capability_dispatch-ON path the dispatch loop routes recognition through the producing Capability,
// which calls THIS (carrying the live θ Dispatch already holds, for it to thread onward if it must). The
// decision:
//
//	recognized = !Exhausted() && (Bespoke || gradedRelevance(stream, Triggers) > 0)   // permissive has-any
//
// The BESPOKE short-circuit is PRESERVED unchanged (a program synthesised for this very goal still applies
// until its phases run out, §2.5). For a NON-bespoke (canonical / template) workflow, recognition is
// PERMISSIVE: it fires when the stream matches at least one trigger (gradedRelevance > 0, i.e. has-any) —
// the SAME criterion as the legacy binary Recognize. The Capability OWNS recognition (the unification), but
// it does NOT impose a stricter bar here. Deterministic (no RNG, no clock); MUTATES the cached recognized
// flag (the load-bearing Recognize-before-GateBias ordering) and returns it, exactly like Recognize.
//
// WHY NOT θ-gate recognition (FIX, E5-deeper live A/B, 2026-06-21): an earlier version gated this on
// `gradedRelevance >= θ` to "suppress weak relevance at recognition". The paired live A/B REGRESSED
// multi-hop grounding (capability-entry ON 0.71 vs legacy OFF 0.89) precisely because that double-gate
// dropped weakly-but-genuinely-relevant non-bespoke workflows that the legacy has-any fires and that help
// the grounding chain. Recognition answers "does this workflow APPLY", NOT "is it worth firing" — the
// θ/value admission is a DOWNSTREAM gate (GateBias / the value filter), not a recognition gate. Conflating
// the two was the design gap; θ is intentionally not consulted here.
func (w *Workflow) recognizeViaGraded(ctx []types.Thought, theta float64) bool {
	_ = theta // recognition is permissive (has-any); the θ/value bar is a downstream admission gate, not here
	if w.Exhausted() {
		w.recognized = false
		return false
	}
	if w.Bespoke {
		w.recognized = true // synthesised for this goal ⇒ applies until exhausted
		return true
	}
	// recognised iff at least one trigger matched (gradedRelevance > 0 == has-any). A zero-overlap workflow
	// stays dark (never fires on everything). This is the legacy permissive criterion; the value/θ admission
	// happens downstream, not at recognition (see the A/B note above).
	rel := gradedRelevance(ctxTextDefault(ctx), w.Triggers)
	w.recognized = rel > 0
	return w.recognized
}

// gradedRelevance is the GRADED keyword-overlap relevance (0..1) that supersedes the binary has-any match
// (SUB-SLICE-2): the FRACTION of the trigger set the stream text matches, a principled bounded score. An
// empty trigger set scores 0.0 (a mis-constructed workflow stays silent — never fires on everything, the
// same posture Capability.Relevance takes). Each trigger is matched with the SAME phrase/word-boundary
// matcher the binary path uses (hasAny on the single trigger), so a single trigger's match semantics are
// identical to before — only the aggregation changed from any-of (binary OR) to fraction-of (graded mean).
// Deterministic: text + triggers in, a float out, no RNG/clock. text is already lower-cased by the caller
// (ctxTextDefault); triggers are lower-cased at construction (lowerAll).
func gradedRelevance(text string, triggers []string) float64 {
	if len(triggers) == 0 {
		return 0.0
	}
	matched := 0
	for _, t := range triggers {
		if hasAny(text, []string{t}) {
			matched++
		}
	}
	return float64(matched) / float64(len(triggers))
}

// Recognized reports the cached recognition flag (read accessor for the engine; the value is set by
// the last Recognize call).
func (w *Workflow) Recognized() bool { return w.recognized }

// I reports the current phase cursor — the read side of the Python `self.workflow.i` the TUI's
// render_subconscious shows ("phase {i}"). Read-only; the field is unexported.
func (w *Workflow) I() int { return w.i }

// Current returns the current phase, clamped to the last phase (Python current: phases[min(i,
// len-1)]). Panics on an empty program reaching the runner — the Python RuntimeError.
func (w *Workflow) Current() Phase {
	if len(w.Phases) == 0 {
		panic("workflow has no phases (an empty program reached the runner)")
	}
	idx := w.i
	if idx > len(w.Phases)-1 {
		idx = len(w.Phases) - 1
	}
	return w.Phases[idx]
}

// Complete reports whether the cursor is at (or past) the last phase (Python complete).
func (w *Workflow) Complete() bool {
	return len(w.Phases) > 0 && w.i >= len(w.Phases)-1
}

// Exhausted reports whether all phases have run — the program is finished and should stop
// contributing (Python exhausted: no phases, or i past the end).
func (w *Workflow) Exhausted() bool {
	return len(w.Phases) == 0 || w.i >= len(w.Phases)
}

// Advance steps the cursor one phase forward; it may go one past the end ⇒ exhausted (Recognize then
// returns false). Mirrors Python advance.
func (w *Workflow) Advance() { w.i++ }

// SkipLoopIfSatisfied turns a Loop into a real FEEDBACK operator (P3.2): measure → decide → repeat.
// The scheduler unrolls a loop to its MaxIter bound, but the loop's stopping condition (Until) was dead
// metadata — every iteration ran regardless. This evaluates Until against the current state via the
// supplied predicate after a loop phase completes, and if it holds, COLLAPSES the loop's remaining
// iterations (positions the cursor at the loop's last phase so the next Advance steps past it). It
// early-exits "on success" — exactly the loop's purpose — while the MaxIter unroll remains the hard
// upper bound that guarantees termination (durability).
//
// satisfied may be nil (no feedback wired ⇒ the loop runs to its bound, the prior behaviour, so the
// scenario goldens are unchanged). Returns true iff it skipped remaining iterations.
func (w *Workflow) SkipLoopIfSatisfied(satisfied func(until string) bool) bool {
	if satisfied == nil || w.i < 0 || w.i >= len(w.Phases) {
		return false
	}
	cur := w.Phases[w.i].Plan
	if cur.Loop == nil {
		return false // not inside a loop
	}
	label := *cur.Loop
	// the loop early-exits only when there ARE remaining iterations to skip — i.e. a later phase still
	// carries this loop label.
	j := w.i + 1
	for j < len(w.Phases) {
		p := w.Phases[j].Plan
		if p.Loop != nil && *p.Loop == label {
			j++
		} else {
			break
		}
	}
	if j == w.i+1 {
		return false // already the loop's last phase — nothing to early-exit
	}
	if !satisfied(cur.Until) {
		return false // the stopping condition is not met — keep iterating
	}
	w.i = j - 1 // park on the loop's last phase; the next Advance() steps past the whole loop
	return true
}

// Reset rewinds the phase cursor to 0 and clears the cached recognition flag — what the engine's
// set_mode does on a reactive<->continuous switch so a recognised workflow is not carried across the
// regime change. Mirrors Python's `workflow.i = 0; workflow.recognized = False`.
func (w *Workflow) Reset() {
	w.i = 0
	w.recognized = false
}

// GateBias returns the current phase's Gate bias while recognised, else an empty map (Python
// gate_bias: dict(self.current().bias) if self.recognized else {}). The returned map is a copy so a
// caller can't mutate the phase's bias.
func (w *Workflow) GateBias() map[string]float64 {
	if !w.recognized {
		return map[string]float64{}
	}
	src := w.Current().Bias
	out := make(map[string]float64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// Instantiate fills the phase with runtime sub-agents — one per step (a parallel group ⇒ several).
// Mirrors Python instantiate.
//
// An effectful operator carries a tool_scope; passing the Action layer's executor makes that scope
// load-bearing (the sub-agent dispatches its scoped tool for real). The cognition view lets the
// cognition operators (rank/eliminate/decompose) compute against the live graph. A nil catalog ⇒ no
// sub-agents (Python `if self.catalog is None: return []`); a step whose operator is not in the
// catalog is skipped (Python `if spec is None: continue`).
//
// executor / cognitionView may be nil (Python's executor=None / cognition=None defaults) — they
// disable the effectful / cognition-exec paths in the resulting SubAgent.
func (w *Workflow) Instantiate(phase Phase, executor *action.ToolExecutor,
	cognitionView *CognitiveView) []*SubAgent {
	if w.Catalog == nil {
		return nil
	}
	// gap-2 fix: resolve the Context ONCE per Instantiate against the LIVE graph (staffing time) — the
	// GROWN branch, not the episode-open goal root — and hand the SAME instance to every worker of this
	// phase (so they share one staffing snapshot, deterministic). nil ⇒ no Context (the ≤5 slice path).
	staffCtx := w.staffingContext()
	var out []*SubAgent
	for k, st := range phase.Plan.Steps {
		spec, ok := w.Catalog.Get(st.Operator)
		if !ok {
			continue
		}
		sid := "sa:" + st.Operator + ":" + st.Domain + "@" + strconv.Itoa(w.i) + "." + strconv.Itoa(k)
		// gap 5 (the load-bearing tool SOURCE): when a category resolver is wired, the worker's concrete tool
		// set comes from picker.Resolve(spec, scope) — the operator's coarse CATEGORY FOOTPRINT resolved
		// against the live registry + worker-faculty footprint — NOT the operator's flat spec.ToolScope. So
		// the operator's name-list is no longer the SOURCE (the gap-9 prerequisite). nil picker ⇒ the flat
		// ToolScope (byte-identical). The resolved set is unchanged for the seed ops (toolpick.go parity).
		// LEGACY(redesign): the `spec.ToolScope` default (the flat name-list as the tool SOURCE) is the
		// pre-picker path — removable when gap-9 (OperatorSpec.ToolScope deletion) lands (the picker is then
		// the only source). NOTE: with a picker wired (capability-ON) this default is overwritten just below.
		toolScope := spec.ToolScope
		if w.picker != nil {
			toolScope = w.picker.Resolve(spec, w.scope)
		}
		sa := NewSubAgent(spec, st.Domain, w.Goal, w.Backend, w.Emit, sid,
			toolScope, executor, cognitionView)
		// gap 3: staff the worker with the run's §3.3a authority CEILING (so its tool picks resolve via
		// ScopedToolScope — category-scoped, not the operator's flat list). gap 2: hand it the rich §3.11
		// Context CAPTURED AT STAFFING TIME (so Fire reads the grown branch, not the ≤5 slice / a stale
		// episode-open snapshot). Both nil ⇒ today's behaviour.
		if w.scope != nil {
			sa.WithScope(w.scope)
		}
		if staffCtx != nil {
			sa.WithContext(staffCtx)
		}
		// T1.1: stamp the query-formulation gate so a web_search worker formulates the query from the actual
		// question (the instruction wrapper stripped) instead of the whole goal. false (the default) ⇒ no-op.
		if w.queryFormulation {
			sa.WithQueryFormulation(true)
		}
		out = append(out, sa)
	}
	return out
}
