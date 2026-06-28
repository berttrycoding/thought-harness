// subagent.go ports subconscious/subagent.py — the sub-agentic object, what a cognitive
// operator instantiates to do its one job.
//
// The spec says each workflow phase "spins up an ephemeral worker defined at runtime." This is
// that worker as a proper sub-agent OBJECT, not a canned string: it carries the attributes a real
// sub-agent needs (the lathe / agent-skill pattern, least-privilege tool scoping):
//
//	role            the operator it embodies — its job (decompose, validate, …)
//	persona         its voice / stance (drawn from the operator's family)
//	responsibility  a single, scoped remit — and an explicit guard against over-reach
//	domain          the rough domain it is scoped to (code / math / safety / planning / general)
//	intent          the operator's domain-general definition (the seed instruction)
//	tools           least-privilege capabilities it may use (default: reason-only, no side effects)
//	context_slice   the bounded slice of the stream it sees (the active line + goal, not the graph)
//
// HARD PORT (dynamic dispatch). SubAgent IMPLEMENTS the PrimitiveSubAgent interface by COMPOSITION (it
// satisfies Domain/Relevance/Fire) — not inheritance. Fire is a nil-check if-ladder over three
// collaborators, dispatching to one of three paths:
//
//	EFFECTUAL      a non-empty toolScope + a bound executor ⇒ dispatch a REAL scoped ToolCall
//	               through the least-privilege executor and fold the genuine ToolResult.
//	COGNITION-EXEC a bound CognitiveView ⇒ rank/eliminate/decompose compute against the live graph.
//	REASON-ONLY    the default ⇒ ask the backend to synthesise the one cognitive move from context.
//
// Either way it returns a raw *Candidate for the hidden seam to judge and re-voice, and emits the
// full subconscious.subagent definition so every runtime instantiation is logged as data to
// standardise and train on. Ported from the (now-removed) Python thought_harness/subconscious/subagent.py.
package subconscious

import (
	"regexp"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// personaArchetypes is the persona per operator family — the sub-agent's epistemic stance.
// Mirrors Python _PERSONA; the lookup default ("a focused specialist") matches _PERSONA.get's.
var personaArchetypes = map[string]string{
	"transformative": "a methodical engineer who reshapes the problem",
	"relational":     "a careful analyst who relates and weighs",
	"generative":     "an inventive explorer who proposes candidates",
	"primitive":      "a precise logician",
	"synthesized":    "a purpose-built specialist",
}

// stanceByRole maps an operator role to the stance carried on its Candidate (for conflict
// detection at the Gate). Mirrors Python _STANCE; a role absent here ⇒ no stance (Python None).
var stanceByRole = map[string]string{
	"validate": "checks", "measure": "checks", "contrast": "differs",
	"compare": "shares", "hypothesize": "proposes", "eliminate": "prunes",
}

// stanceFor returns (stance, ok) for a role — the Go form of Python _STANCE.get(role) where a miss
// is None. ok=false ⇒ the Candidate carries no stance.
func stanceFor(role string) (string, bool) { s, ok := stanceByRole[role]; return s, ok }

// SubAgent is a runtime sub-agent that applies ONE operator to the active line, then is discarded.
// It satisfies the PrimitiveSubAgent interface (Domain/Relevance/Fire) by composition — the Go form of
// Python's `class SubAgent(PrimitiveSubAgent)` where the only overridden behaviour is relevance + fire.
//
// Fields are unexported and set through NewSubAgent (so the PrimitiveSubAgent's Domain() method and the
// `domain` field do not collide — Go forbids a same-named field+method): they mirror the validated
// SubAgentSpec schema — spec/domain/goal + the collaborators (backend/executor/cognition) + a
// least-privilege toolScope (empty ⇒ synthesise-from-context only), a verifier tier (default "soft"
// — synthesised content is fallible), and singleShot (default true — one move, then discarded; the
// step-cap anchor = 1). The workflow runner (Tier 5) instantiates one per step.
type SubAgent struct {
	spec      cognition.OperatorSpec // the operator this sub-agent embodies
	domain    string                 // the domain it is scoped to (Python default "general")
	goal      string                 // the episode goal (Python default "")
	backend   backends.Backend       // language faculty for the reason path (nil ⇒ fallback text)
	emit      events.Emit            // bus closure (nil ⇒ no event, Python self.emit is None)
	id        string                 // the sub-agent id, e.g. "sa:decompose:code@1.0"
	toolScope []string               // least-privilege tools (empty ⇒ no effectful tools)
	executor  *action.ToolExecutor   // the parent ToolExecutor (nil ⇒ reason-only, no effects)
	cognition *CognitiveView         // a CognitiveView (nil ⇒ no cognition-exec path)
	verifier  string                 // verifier tier (Python verifier_type; default "soft")
	single    bool                   // one move, then discarded (Python single_shot; default true)
	// scope is the §3.3a CATEGORY ceiling sourced from the Capability (the #31 SubAgent category-scope
	// delta): when set, the toolScope pick is filtered by tool category so a sub-agent can only reach tools
	// within its authority band. nil ⇒ the flat toolScope (today's behaviour, byte-identical).
	scope *Scope
	// context is the §3.11 rich 3-layer Context captured by the producing Capability (the gap-2 delta):
	// when set, Fire concretizes against the WHOLE frozen branch snapshot (Context.WorkerContext) instead
	// of the ≤5 ContextSliceDefault window — the flaky-grounding root-cause fix (a worker no longer reads a
	// starved 5-thought tail). nil ⇒ the ≤5 slice (today's behaviour, byte-identical).
	context *Context
	// queryFormulation is the subconscious.query_formulation gate (T1.1): when true, the web_search branch of
	// floorToolCall FORMULATES the query from the actual question (strips a leading instruction/wrapper
	// clause) instead of searching the whole goal verbatim. false (the default) ⇒ the query is
	// strings.TrimSpace(s.goal) exactly as before ⇒ byte-identical. Pure deterministic string transform.
	queryFormulation bool
}

// WithQueryFormulation sets the subconscious.query_formulation gate (T1.1) and returns the sub-agent for
// chaining. When true, the web_search query is formulated from the actual question (the instruction
// wrapper stripped); false (the default) ⇒ the raw trimmed goal, byte-identical.
func (s *SubAgent) WithQueryFormulation(on bool) *SubAgent { s.queryFormulation = on; return s }

// FormulateQuery is the EXPORTED entry to the T1.1 FLARE wrapper-strip query formulation (the same pure,
// deterministic transform the web_search floor uses — "search the question, not the whole goal", strip a
// leading instruction wrapper). It is exported so other independent-retrieval call sites (the T2.1
// answer-verifier's re-retrieval query) REUSE this one formulation rather than duplicate the wrapper-strip
// heuristic. It delegates to the unexported formulateQuery so there is ONE implementation. Pure Pattern-A
// string op (no model / clock / RNG): same input ⇒ same output.
func FormulateQuery(goal string, ctx ...string) string { return formulateQuery(goal, ctx...) }

// WithScope attaches the Capability-sourced category ceiling (§3.3a) and returns the sub-agent for
// chaining. The sub-agent's effective tool scope is then the toolScope filtered by this ceiling.
func (s *SubAgent) WithScope(sc *Scope) *SubAgent { s.scope = sc; return s }

// WithContext attaches the §3.11 Capability-captured Context (gap 2) and returns the sub-agent for
// chaining. When set, Fire reads the whole frozen branch snapshot (Context.WorkerContext) rather than the
// ≤5 ContextSliceDefault window — the rich-context staffing the redesign specifies. nil ⇒ the ≤5 slice.
func (s *SubAgent) WithContext(c *Context) *SubAgent { s.context = c; return s }

// Context returns the attached §3.11 Context (nil ⇒ none) — the read accessor a wiring-gate test reads to
// prove the worker actually RECEIVED the rich Context when the capability flag is on (built ≠ wired).
func (s *SubAgent) Context() *Context { return s.context }

// ID returns the sub-agent's id (e.g. "sa:validate:code@1.0") — the read accessor a wiring-gate test / the
// trace reads to name the worker. Read-only.
func (s *SubAgent) ID() string { return s.id }

// ToolScope returns a COPY of the worker's RAW staffed tool name-list — the SOURCED set (the gap-5
// category-resolved set when a ToolPicker was wired, else the operator's flat ToolScope). It is the read
// accessor a wiring-gate test reads to prove the worker's tools came from the category source (built ≠
// wired), distinct from ScopedToolScope (which additionally applies the §3.3a ceiling filter). A copy is
// returned so a caller cannot mutate the worker's scope.
func (s *SubAgent) ToolScope() []string { return append([]string(nil), s.toolScope...) }

// toolCategory classifies a builtin tool name into its coarse OPERATION category for the Scope check (gap
// 6 reconciliation): it delegates to the ONE shared action taxonomy (action.ClassifyToolName) rather than
// keeping a duplicate name-switch, so the subagent category-scope and the action gate-router route on the
// SAME operation tag (the audit's "two name-switches -> one source"). The SubAgent holds tool NAMES (its
// toolScope is []string), not Tool objects, so this is the name-keyed arm of the tool-owned Category().
func toolCategory(name string) string {
	return action.ClassifyToolName(name).Op.String()
}

// ScopedToolScope returns the least-privilege toolScope filtered by the category ceiling (§3.3a / #31): a
// tool whose category is outside the scope is dropped. With no scope attached it returns the toolScope
// unchanged (byte-identical). This is the category-scope bite — a sub-agent staffed under a "read-only"
// ceiling cannot reach a mutate tool even if it was listed.
func (s *SubAgent) ScopedToolScope() []string {
	if s.scope == nil {
		return s.toolScope
	}
	out := make([]string, 0, len(s.toolScope))
	for _, t := range s.toolScope {
		if s.scope.AllowsCategory(toolCategory(t)) {
			out = append(out, t)
		}
	}
	return out
}

// NewSubAgent builds a SubAgent with Python's dataclass defaults (verifier_type="soft",
// single_shot=True). It mirrors the keyword construction the workflow runner uses:
// SubAgent(spec=…, domain=…, goal=…, backend=…, emit=…, id=…, tool_scope=…, executor=…, cognition=…).
// A nil backend ⇒ the reason path uses fallback text; a nil executor / cognition disables that
// path; an empty toolScope ⇒ reason-only.
func NewSubAgent(spec cognition.OperatorSpec, domain, goal string, backend backends.Backend,
	emit events.Emit, id string, toolScope []string, executor *action.ToolExecutor,
	cognitionView *CognitiveView) *SubAgent {
	return &SubAgent{
		spec:      spec,
		domain:    domain,
		goal:      goal,
		backend:   backend,
		emit:      emit,
		id:        id,
		toolScope: toolScope,
		executor:  executor,
		cognition: cognitionView,
		verifier:  "soft", // synthesised content is fallible ⇒ soft admission
		single:    true,   // one move, then discarded (step-cap anchor = 1)
	}
}

// --- derived sub-agent attributes -----------------------------------------------------------

// Role is the operator name this sub-agent embodies (Python role property).
func (s *SubAgent) Role() string { return s.spec.Name }

// Persona is the family's epistemic stance (Python persona property). The default mirrors
// _PERSONA.get(family, "a focused specialist").
func (s *SubAgent) Persona() string {
	if p, ok := personaArchetypes[s.spec.Family]; ok {
		return p
	}
	return "a focused specialist"
}

// Responsibility is the sub-agent's single, scoped remit (Python responsibility property).
func (s *SubAgent) Responsibility() string {
	return "Apply the '" + s.Role() + "' operator to the current line: " + s.spec.Intent + "."
}

// OutOfScope is the explicit over-reach guard (Python out_of_scope property).
func (s *SubAgent) OutOfScope() string {
	return "Produce ONLY the result of this one '" + s.Role() + "' move, scoped to " + s.domain + ". " +
		"Do not solve the whole task, do not take any external action, and stay silent if " +
		"the current line is not about " + s.domain + "."
}

// SystemPrompt assembles the deterministic ordered prompt: identity → remit → out-of-scope →
// tools → stopping (Python system_prompt). The tools line lists the scope, or the reason-only
// default phrasing when empty.
func (s *SubAgent) SystemPrompt() string {
	tools := "none (reason from context only)"
	if len(s.toolScope) > 0 {
		tools = strings.Join(s.toolScope, ", ")
	}
	return strings.Join([]string{
		"You are " + s.Persona() + ", acting as the '" + s.Role() + "' operator for a " + s.domain + " task.",
		"Responsibility: " + s.Responsibility(),
		"Out of scope: " + s.OutOfScope(),
		"Tools: " + tools + ".",
		"Return one concise result; you run once.",
	}, "\n")
}

// ContextSlice returns the recent REAL thoughts of the active line (not the whole graph): it drops
// METACOG thoughts and recap-preamble thoughts, then keeps the last n. Mirrors Python context_slice.
func (s *SubAgent) ContextSlice(ctx []types.Thought, n int) []types.Thought {
	real := make([]types.Thought, 0, len(ctx))
	for _, t := range ctx {
		if t.Source != types.METACOG && !strings.HasPrefix(t.Text, types.RecapPrefix) {
			real = append(real, t)
		}
	}
	if len(real) > n {
		real = real[len(real)-n:]
	}
	return real
}

// ContextSliceDefault uses the Python default window n=5.
//
// LEGACY(redesign): the ≤5-thought window — the pre-redesign worker context, the FALLBACK workerSlice uses
// when no rich §3.11 Context is attached (the subconscious.capability OFF-path) — removable when the 4
// redesign flags are retired (a worker then always reads the rich captured branch). NOTE: this is a fallback
// COMPONENT, not a flag OFF-branch — it can only be deleted once the rich Context is unconditionally staffed.
func (s *SubAgent) ContextSliceDefault(ctx []types.Thought) []types.Thought {
	return s.ContextSlice(ctx, 5)
}

// workerSlice is the context a worker actually concretizes against (gap 2). When a §3.11 Context is
// attached (the capability-on path) it returns the RICHER of {the captured branch snapshot, the live
// ≤5 slice} — the rich, scaling context that replaces the ≤5 window WITHOUT ever starving the worker.
// With NO Context attached (the default), or a Context whose snapshot is empty, it falls back to
// ContextSliceDefault(ctx) — byte-identical to before.
//
// THE SAFETY BELT (gap-2 fix part 2). The original wire blindly preferred ANY non-empty captured
// snapshot over the live tail (len(rich)>0). That STARVED a worker whenever the snapshot was thinner
// than the live tail — the live-claude finding: an episode-OPEN capture freezes the goal root (1
// thought), so a mid-episode worker was pinned to that 1-thought snapshot instead of its grown ≤5 tail
// (grounding COLLAPSED, OFF 2/3 → ON 0/3). Picking by thought-count means the captured Context can only
// ever ENRICH (it wins when it carries MORE of the branch than the tail), never STARVE (if it is somehow
// thinner — e.g. a stale/early capture — the live ≤5 tail wins). This is the single seam where the rich
// Context becomes load-bearing: a worker READS it, but only when it is genuinely richer than the tail.
func (s *SubAgent) workerSlice(ctx []types.Thought) []types.Thought {
	live := s.ContextSliceDefault(ctx)
	rich := s.context.WorkerContext()
	// Prefer the RICHER of the two by thought-count — never blindly the non-empty snapshot. A captured
	// Context wins only when it carries MORE of the branch than the live tail (the intended enrichment);
	// when it is thinner (an early/stale capture), the live tail wins (it can never starve the worker).
	if len(rich) > len(live) {
		return rich
	}
	// LEGACY(redesign): the ≤5-slice fallback (no rich §3.11 Context attached, or a thinner snapshot — the
	// subconscious.capability OFF-path) — removable when the 4 redesign flags are retired (the rich captured
	// branch is then always attached, so workerSlice returns it unconditionally). It stays a SAFETY BELT
	// while the flag exists: it can never starve a worker (the live tail always wins over a thinner capture).
	return live
}

// --- PrimitiveSubAgent interface --------------------------------------------------------------------

// Domain reports the sub-agent's domain tag, satisfying PrimitiveSubAgent.Domain. A SubAgent is a
// PrimitiveSubAgent by composition: this accessor + Relevance + Fire are the whole interface.
func (s *SubAgent) Domain() string { return s.domain }

// Relevance reports the sub-agent's relevance: a fixed 0.9 while its phase is current (dispatch
// gates that). Mirrors Python relevance (returns 0.9 unconditionally).
func (s *SubAgent) Relevance(_ []types.Thought) float64 { return 0.9 }

// Fire runs the sub-agent's one move through the nil-check if-ladder over its three collaborators,
// returning a raw *Candidate (nil == Python None) for the hidden seam. Mirrors Python fire.
func (s *SubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	sliced := s.workerSlice(ctx) // gap 2: the rich §3.11 Context (whole branch) when attached, else the ≤5 slice
	// Effectful path: a scoped operator with a bound executor dispatches a REAL tool and folds the
	// genuine result into the Candidate — least-privilege (only its toolScope), sandbox-gated.
	if len(s.toolScope) > 0 && s.executor != nil {
		if c := s.fireTool(sliced); c != nil {
			return c
		}
	}
	// Cognition-execution path: rank/eliminate/decompose compute against the REAL graph/value —
	// not a template.
	if s.cognition != nil {
		if c := s.fireCognition(); c != nil {
			return c
		}
	}
	// Reason-only (default): synthesise the one cognitive move from context (no real effect).
	return s.fireReason(sliced)
}

// reasonOnly reports whether this sub-agent's Fire CANNOT take the effectful (executor) or
// cognition-exec paths — i.e. it will only call fireReason. This is the determinism-safety gate for
// per-phase concurrency: the reason path is pure (a backend OperatorApply call + an s.emit, no shared
// mutable state and no direct bus emit), so it is the ONLY path safe to run concurrently and replay
// byte-identically. The effectful path mutates a shared executor/sandbox AND emits action.* events
// straight to the bus inside Execute (un-buffered); the cognition path reads the live graph; neither
// is buffered here, so a group containing one of them stays serial.
func (s *SubAgent) reasonOnly() bool {
	if len(s.toolScope) > 0 && s.executor != nil {
		return false
	}
	if s.cognition != nil {
		return false
	}
	return true
}

// fireBuffered runs Fire while CAPTURING every s.emit event into an ordered buffer instead of pushing
// it to the bus, returning the candidate plus the buffered events. The concurrent per-phase path
// (dispatch.go) uses this so each branch fires in its own goroutine without touching the shared bus;
// the buffers are then flushed by the caller in deterministic step-index order, keeping the trace
// byte-identical to the serial path. Only valid for a reasonOnly sub-agent (the caller gates on that).
//
// It swaps s.emit for a closure that appends to a local slice for the duration of this call, then
// restores it. This sub-agent is single-shot and instantiated per-phase (never shared across fires),
// so the temporary swap is local to this goroutine's exclusive instance — no cross-goroutine aliasing.
func (s *SubAgent) fireBuffered(ctx []types.Thought, rng *cpyrand.Random) (*types.Candidate, []bufferedEvent) {
	var buf []bufferedEvent
	orig := s.emit
	if orig != nil {
		s.emit = func(kind, summary string, data events.D) events.Event {
			buf = append(buf, bufferedEvent{kind: kind, summary: summary, data: data})
			return events.Event{}
		}
	}
	c := s.Fire(ctx, rng)
	s.emit = orig
	return c, buf
}

// bufferedEvent is one captured s.emit call, held until the caller flushes it in index order onto the
// real bus (preserving the serial-path trace ordering under per-phase concurrency).
type bufferedEvent struct {
	kind    string
	summary string
	data    events.D
}

// fireCognition executes a cognition operator (rank/eliminate/decompose) against the live graph.
// Returns nil (Python None) when this operator has no cognition semantics ⇒ the caller reasons.
// Mirrors Python _fire_cognition.
func (s *SubAgent) fireCognition() *types.Candidate {
	text, payload, ok := ExecuteCognitionOp(s.Role(), s.goal, s.cognition)
	if !ok {
		return nil
	}
	op, _ := payload["op"].(string)
	s.emitDef("sub-agent "+s.Role()+"/"+s.domain+" ▸ "+op+": "+clipRunes(text, 34),
		events.D{"executed": payload["op"]})
	c := cand(s.domain, text, 0.9, withOperator(cognition.ToEnum(s.Role())),
		withPayload(map[string]any(payload)))
	if st, has := stanceFor(s.Role()); has {
		withStance(st)(c)
	}
	return c
}

// fireReason asks the backend to synthesise the one cognitive move from context (the default,
// no-effect path). Falls back to "[role] intent" if the backend errors / is absent. Mirrors
// Python _fire_reason.
func (s *SubAgent) fireReason(sliced []types.Thought) *types.Candidate {
	text := ""
	if s.backend != nil {
		text = s.backend.OperatorApply(s.Role(), s.Responsibility(), s.spec.Intent, s.domain, s.goal, sliced)
	}
	if text == "" {
		text = "[" + s.Role() + "] " + s.spec.Intent
	}
	s.emitDef("sub-agent "+s.Role()+"/"+s.domain+": "+clipRunes(text, 44), nil)
	c := cand(s.domain, text, 0.9, withOperator(cognition.ToEnum(s.Role())))
	if st, has := stanceFor(s.Role()); has {
		withStance(st)(c)
	}
	return c
}

// fireTool builds a least-privilege ToolCall from the operator's scope, dispatches it through the
// scoped executor, and carries the REAL ToolResult (summary as text, full result as payload).
// Returns nil (Python None) when no concrete call exists for this scope ⇒ reason instead. Mirrors
// Python _fire_tool.
func (s *SubAgent) fireTool(ctx []types.Thought) *types.Candidate {
	call, ok := s.toolCall(ctx)
	if !ok {
		return nil
	}
	result := s.executor.Scoped(s.ScopedToolScope()).Execute(call) // category-ceiling filtered (§3.3a/#31); action.* events in Execute
	text := action.SummarizeToolResult(result)
	s.emitDef("sub-agent "+s.Role()+"/"+s.domain+" ▸ "+call.Name+": "+clipRunes(text, 36),
		events.D{"tool": call.Name, "ok": !result.IsError, "exit_code": result.ExitCode})
	// A real refutation reads with conflict stance so the seam/Critic treat a failure as signal:
	// stance = "fail" if error else _STANCE.get(role).
	opts := []candOpt{withOperator(cognition.ToEnum(s.Role())), withPayload(result)}
	if result.IsError {
		opts = append(opts, withStance("fail"))
	} else if st, has := stanceFor(s.Role()); has {
		opts = append(opts, withStance(st))
	}
	return cand(s.domain, text, 0.9, opts...)
}

// toolCall distils this sub-agent's one move into a concrete ToolCall (the safe, low-arg effects —
// read a file, search the tree, run the suite; writes/edits with content stay reason-only until P3).
// ok=false (Python None) ⇒ no concrete call for this scope.
//
// PATTERN C (FIX 1, grounding-chain). The deterministic FLOOR (floorToolCall — the regex over the
// STATIC goal) is the default pick AND the offline/test path. When THOUGHT_MODEL_SELECT is ON and the
// floor is fuzzy/empty for this grounding-shaped step (the next-hop target lives in the prior
// observations, not the static goal), the MODEL CEILING (modelSelectCall over the live ctx) picks the
// call instead — esp. read_file on the path it reasoned to. The pick still runs through
// executor.Execute (all gates fire). With the flag OFF (or no comprehender model / a decline) the
// floor stands => byte-identical.
func (s *SubAgent) toolCall(ctx []types.Thought) (action.ToolCall, bool) {
	floor, floorOK := s.floorToolCall()
	// Model ceiling on a flagged-fuzzy step: the floor could not resolve a usable grounding call from
	// the static goal -> ask the model what to read/search from the live context. nil (flag OFF / no
	// comprehender / decline / out-of-scope) -> keep the floor below.
	if call, ok := s.modelSelectCall(ctx, floor, floorOK); ok {
		return call, true
	}
	return floor, floorOK
}

// floorToolCall is the DETERMINISTIC tool-pick floor (Pattern A — no model): it consults the ONE
// shared selector (action.SelectTool) on the static goal, gated by this sub-agent's least-privilege
// scope, then the scope-keyed fallbacks. This is the sub-agent half of the original grounding fix: an
// `expose-affordances` sub-agent scoped {search, read_file} can READ a named file. It is the offline/
// test path and what the model ceiling (FIX 1) refines only when it is fuzzy/empty.
func (s *SubAgent) floorToolCall() (action.ToolCall, bool) {
	scope := s.toolScope
	// FETCH-URL (subconscious.fetch_url, T1.4): a sub-agent scoped to fetch_url whose GOAL CARRIES AN
	// EXPLICIT http(s) URL fetches that specific page — the emergent browse step (web_search surfaces a
	// result URL in an observation, which becomes the goal/context the next worker fetches). It is evaluated
	// FIRST, above web_search + the shared selector, because a goal that already names a concrete URL wants
	// that page READ, not a fresh search. There is NO hardcoded multi-step loop: each fetch is one
	// independent dispatch gated on a URL being present + fetch_url in scope. Gated on the fetch_url scope
	// grant: with the flag OFF (the tool was never granted) this branch is skipped entirely and the flow
	// below is byte-identical. The fold-into-grounding happens through the same fireTool path.
	if containsScope(scope, "fetch_url") {
		if u := firstURL(s.goal); u != "" {
			return action.ToolCall{Name: "fetch_url", Args: map[string]any{"url": u}}, true
		}
	}
	// WEB-SEARCH (subconscious.web_search): a sub-agent scoped to web_search (expose-affordances when the
	// flag granted it the tool) dispatches a real web search for the WHOLE goal — a web-lookup question
	// wants the full question as the query, not a single keyword. It is evaluated FIRST, above the shared
	// selector + the local-`search` branch, so a lookup-shaped goal reaches the OPEN WEB rather than
	// grepping the (empty) workspace tree — UNLESS the goal names a concrete LOCAL file, in which case the
	// genuinely-local read below still wins (filePath!=""). Gated on web_search being in scope: with the
	// flag OFF (the tool was never granted to the operator) this branch is skipped entirely and the flow
	// below is byte-identical. The fold-into-grounding happens through the same fireTool path (the
	// ToolResult becomes the Candidate text the hidden seam admits).
	if containsScope(scope, "web_search") && filePath(s.goal) == "" {
		raw := strings.TrimSpace(s.goal)
		if raw != "" {
			q := raw
			// QUERY-FORMULATION (subconscious.query_formulation, T1.1; FLARE arXiv:2305.06983): formulate the
			// query from the ACTUAL question — strip a leading instruction/wrapper clause ("Answer this multi-hop
			// question: <Q>" -> "<Q>") — instead of searching the wrapper prose too (the MEASURED bench fix where
			// a wrapped goal made DuckDuckGo return a benchmark meta-page). Pure deterministic string transform
			// (Pattern A — no model/clock/RNG). OFF (the default) ⇒ q stays the raw trimmed goal ⇒ byte-identical.
			if s.queryFormulation {
				q = formulateQuery(raw)
				if q != raw && s.emit != nil {
					// Visible ONLY when the reformulation actually changed the query (a no-op strip is silent ⇒
					// no event on the default/uneffective path).
					s.emit(events.SubQueryFormulate, "web_search query formulated from the question (instruction wrapper stripped)",
						events.D{"id": s.id, "goal": raw, "query": q})
				}
			}
			return action.ToolCall{Name: "web_search", Args: map[string]any{"query": q}}, true
		}
	}
	// Shared selector first, gated by scope — a read/search intent that names a real target wins.
	if call, ok := action.SelectTool(s.goal, ""); ok && containsScope(scope, call.Name) {
		return call, true
	}
	// RUN-TESTS RELEVANCE PRE-GATE (#36, the tool-targeting-noise fix). A measure/validate sub-agent
	// carries run_tests in scope, and the old bare fallback fired `python -m pytest` for ANY such
	// sub-agent — including a QA/lookup goal with an empty workspace, where pytest exits 5 (no tests
	// collected), wasting a step and folding an error context that pollutes the answer. The bare-run
	// fallback now requires the goal/domain to be CODE/TEST relevant (codeTestRelevant): a code task with
	// a test target still runs the suite; a QA/lookup goal does not dispatch run_tests. The shared selector
	// above still fires run_tests when the goal text NAMES a .py target / test wording (that is an explicit
	// run intent regardless of domain), so a genuine "run the tests in foo.py" is unaffected.
	if containsScope(scope, "run_tests") && codeTestRelevant(s.goal, s.domain) {
		return action.ToolCall{Name: "run_tests", Args: map[string]any{}}, true
	}
	if containsScope(scope, "read_file") {
		// A file path the selector's curated extensions missed — local extractor as the superset guard.
		if path := filePath(s.goal); path != "" {
			return action.ToolCall{Name: "read_file", Args: map[string]any{"path": path}}, true
		}
	}
	if containsScope(scope, "search") {
		kw := keyword(s.goal)
		if kw == "" {
			kw = keyword(s.domain)
		}
		if kw != "" {
			return action.ToolCall{Name: "search", Args: map[string]any{"pattern": kw}}, true
		}
		return action.ToolCall{}, false
	}
	return action.ToolCall{}, false // write_file (needs content) ⇒ P3
}

// codeTestSignals are the code/test domain markers that make running the suite relevant. They mirror the
// synthesiser's "code" domain keywords (cognition.domainTable) plus the explicit run/test wording, so a
// code-or-test goal still runs the suite while a QA/lookup goal does not. Deliberately NOT bare words that
// appear in a failure observation (the runTriggers comment's feedback-loop caution) — these are intent
// markers a code goal carries.
var codeTestSignals = []string{
	"code", "function", "api", "endpoint", "bug", "python", "runtime", "compile",
	"test", "tests", "suite", "pytest", "pass the", "passes", "regression",
	"refactor", "implement", "build the", "fix the", "debug",
}

// codeTestRelevant reports whether running the test suite is RELEVANT to this sub-agent's work (#36): the
// sub-agent's resolved domain is "code", OR the goal text carries a code/test signal. It is the relevance
// pre-gate on the bare run_tests fallback — without it a measure/validate sub-agent staffed on a QA/lookup
// goal fired pytest on an empty workspace (exit 5, wasted step, error context). Pure string ops over the
// static goal + domain (Pattern A, deterministic — goldens reproduce).
func codeTestRelevant(goal, domain string) bool {
	if domain == "code" {
		return true
	}
	return hasAny(strings.ToLower(goal), codeTestSignals)
}

// instructionWrapperPrefixes are the case-insensitive leading clauses that mark an INSTRUCTION WRAPPER
// around the actual question — the bench harness's framing prose ("Answer this multi-hop question:",
// "Question:", …). Each is matched at the START of the goal and terminated by a colon; everything up to
// and including that first colon is the wrapper to strip. Deliberately NARROW: ONLY framings whose final
// word is a META-reference to the act of questioning ("question"/"answer …"), never a bare imperative verb
// — because a bare verb is indistinguishable from a SUBJECT ("Find Waldo: where is he hidden" would amputate
// "Waldo", "Search the news at https://…" would cut inside the URL). The earlier wider set (find/search/look
// up/task/please/q) was REMOVED for exactly that subject-amputation hazard (red-team T1.1): it bought no
// measured coverage (the gap is the answer/question framings) and actively degraded plausible goals. A bare
// imperative goal now simply falls through to the verbatim query (today's behaviour), never a wrong strip.
// Lower-cased; matched against the lower-cased goal.
var instructionWrapperPrefixes = []string{
	"answer this", "answer the following", "answer the",
	"question",
	"please answer",
}

// formulateQuery FORMULATES the web_search query from the ACTUAL question (T1.1; FLARE arXiv:2305.06983
// "search the sub-goal, not the whole goal"). It strips a leading INSTRUCTION/WRAPPER clause that is
// terminated by the FIRST colon — "Answer this multi-hop question: Who founded X?" -> "Who founded X?" —
// while keeping the FULL remaining question (it removes wrapper prose, it does NOT reduce to a keyword: a
// web lookup wants the whole question). It is CONSERVATIVE by design:
//
//   - It strips ONLY when the FIRST colon is a WRAPPER BOUNDARY — i.e. that colon is followed by whitespace
//     or ends the string ("Question: …", "Answer this: …"). A content colon inside a URL/scheme
//     ("https://…"), a ratio ("3:4") or a time ("3:00") is followed by a NON-space char, so it is never a
//     boundary and the goal is left intact (this is what kills the "https://news…" false-strip; the guard
//     is independent of the prefix list).
//   - AND only when the text BEFORE that colon, taken whole, BEGINS WITH a known instruction framing on a
//     word boundary (instructionWrapperPrefixes — the narrow answer/question set, never a bare imperative
//     verb), so a real subject ("Find Waldo: …") is never amputated.
//   - It NEVER returns empty: if stripping would leave nothing (the question WAS the wrapper), the raw
//     trimmed goal is returned unchanged.
//
// Pure deterministic string transform (Pattern A — no model, no clock, no RNG): same input ⇒ same output,
// so the seeded-RNG determinism contract and the goldens hold (and it only runs at all behind the
// default-OFF subconscious.query_formulation flag). The variadic ctx is reserved for a future sub-question
// preference (FLARE's active sub-goal) — INTENTIONALLY UNUSED here: the wrapper-strip alone fixes the
// measured gap, and extracting a "more specific active sub-question" from the bounded context slice
// reliably + deterministically adds real parsing risk, so it is SKIPPED for this slice (see the report).
func formulateQuery(goal string, ctx ...string) string {
	_ = ctx // reserved for a future sub-question preference (FLARE); unused this slice (see doc comment)
	raw := strings.TrimSpace(goal)
	idx := strings.Index(raw, ":")
	if idx <= 0 {
		return raw // no colon (or a leading colon) ⇒ nothing to strip
	}
	// Wrapper-boundary guard: only a colon FOLLOWED BY whitespace (or at end-of-string) can be an
	// instruction-wrapper boundary. A content colon — a URL scheme "https:" (followed by '/'), a ratio
	// "3:4", a time "3:00" — is followed by a non-space char and is NOT a boundary, so the goal is left
	// verbatim. (Bench wrappers always read "Question: " / "Answer this: " with the space.)
	if idx+1 < len(raw) && raw[idx+1] != ' ' && raw[idx+1] != '\t' {
		return raw
	}
	prefix := strings.TrimSpace(raw[:idx])
	lower := strings.ToLower(prefix)
	if !hasInstructionPrefix(lower) {
		return raw // the pre-colon clause is not an instruction wrapper (content colon) ⇒ leave intact
	}
	stripped := strings.TrimSpace(raw[idx+1:])
	if stripped == "" {
		return raw // stripping would leave nothing (the question WAS the wrapper) ⇒ keep the raw goal
	}
	return stripped
}

// urlRe matches the FIRST http(s) URL in free text — the emergent browse-step target the fetch_url floor
// branch reads (a URL surfaced in a prior web_search observation that became this worker's goal/context).
// It is deliberately conservative: an http:// or https:// scheme followed by the run of non-whitespace,
// non-trailing-punctuation URL characters, so a URL embedded in prose ("see https://example.com/page.")
// is captured WITHOUT the trailing period. Parens ARE allowed in the interior (Wikipedia-style
// "…/Foo_(bar)") and an unbalanced trailing ')' is shed afterwards. Pure deterministic regex (no clock/RNG).
var urlRe = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'\]]+`)

// firstURL returns the first http(s) URL in text, with trailing prose punctuation trimmed, or "" when the
// text names no URL. It is what makes the fetch_url floor branch fire only when a concrete page target is
// actually present (the emergent browse step), so a goal with no URL falls through to web_search / the
// local tree byte-identically. Deterministic string op.
func firstURL(text string) string {
	m := urlRe.FindString(text)
	if m == "" {
		return ""
	}
	// Trim trailing prose punctuation the char class above already excludes from the interior but that can
	// still cling when a URL ends a sentence ("…/page." -> "…/page").
	m = strings.TrimRight(m, ".,;:!?")
	// Balanced-paren trim: a Wikipedia-style URL ("…/Foo_(bar)") KEEPS its parens (counts balanced), but a
	// URL that merely closes a parenthetical in prose ("(see https://x.com)") sheds the unbalanced ')'.
	for strings.HasSuffix(m, ")") && strings.Count(m, ")") > strings.Count(m, "(") {
		m = m[:len(m)-1]
	}
	return m
}

// hasInstructionPrefix reports whether the lower-cased pre-colon clause BEGINS WITH a known instruction
// wrapper prefix on a WORD BOUNDARY (the whole clause, or the prefix followed by a space). The word-boundary
// guard keeps "questionnaire about X:" (not an instruction) from matching the "question" prefix, while
// "Question:" and "Answer this multi-hop question" both match. Deterministic; no allocation beyond the loop.
func hasInstructionPrefix(clause string) bool {
	for _, p := range instructionWrapperPrefixes {
		if clause == p {
			return true
		}
		if strings.HasPrefix(clause, p) && len(clause) > len(p) && clause[len(p)] == ' ' {
			return true
		}
	}
	return false
}

// emitDef emits the full subconscious.subagent definition plus any per-path extras. Mirrors Python
// _emit: the base map carries the sub-agent's complete definition (so every instantiation is
// logged as data), and **extra overlays the path-specific keys (executed / tool+ok+exit_code).
// A nil emit is the Python `if self.emit is None: return`.
func (s *SubAgent) emitDef(summary string, extra events.D) {
	if s.emit == nil {
		return
	}
	// list(self.tool_scope): Python's list() of an empty tuple is [], which JSON-encodes as []
	// (not null). A nil Go slice encodes as null, so allocate a non-nil slice to match the wire.
	scope := make([]string, len(s.toolScope))
	copy(scope, s.toolScope)
	data := events.D{
		"id":             s.id,
		"role":           s.Role(),
		"persona":        s.Persona(),
		"domain":         s.domain,
		"responsibility": s.Responsibility(),
		"family":         s.spec.Family,
		"tool_scope":     scope, // list(self.tool_scope) — non-nil so empty encodes as []
		"verifier_type":  s.verifier,
		"intent":         s.spec.Intent,
		"system_prompt":  s.SystemPrompt(),
	}
	for k, v := range extra {
		data[k] = v
	}
	s.emit(events.SubSubagent, summary, data)
}

// --- small helpers ---------------------------------------------------------------------------

// keyword returns the first whitespace-token of text whose alphanumeric-only form is longer than 3
// chars, or "" (Python None). Mirrors Python _keyword: for each word, keep only alphanumerics, and
// return the first whose length > 3.
func keyword(text string) string {
	for _, w := range strings.Fields(text) {
		var b strings.Builder
		for _, r := range w {
			if isAlnum(r) {
				b.WriteRune(r)
			}
		}
		if len([]rune(b.String())) > 3 {
			return b.String()
		}
	}
	return ""
}

// isAlnum reports whether r is ASCII alphanumeric — the characters Python str.isalnum keeps for the
// arithmetic/identifier tokens this distillation ever sees (the goals/domains are ASCII).
func isAlnum(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	default:
		return false
	}
}

// containsScope reports whether name is in the tool scope (`"x" in scope`).
func containsScope(scope []string, name string) bool {
	for _, n := range scope {
		if n == name {
			return true
		}
	}
	return false
}

// clipRunes returns the first n runes of s (Python's text[:n] slice — codepoint-based, so a
// multibyte rune is never split). Used only for the human-readable event summary truncation.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
