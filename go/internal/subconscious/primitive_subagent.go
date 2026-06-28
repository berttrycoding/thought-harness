// Package subconscious holds the silent engine — the primitive subagents (the worker faculties) that
// fire on relevance (pull, not push), dispatch, and the generative control (operators -> synthesised
// programs -> sub-agents). This file is the primitive subagents (Tier 2): the REAL primitive set of the
// representation-space rebuild (M2, docs/internal/archive/representation-space-rebuild.md §2). (The type was
// renamed Specialist -> PrimitiveSubAgent; see the iface doc for the stable-contract words it keeps.)
//
// A subconscious primitive is EXACTLY one of two things — never a canned domain opinion
// (feedback-heuristic-control-only applied to the specialist layer, §2.2):
//
//	(T) tool-backed   — carries real ground truth: it DOES (computes, recalls a real store, reads /
//	                    searches / runs reality), it does not opine. These are the system's senses
//	                    and hands — what makes "the one opening to reality" real, not imagined.
//	(M) model-driven  — content comes from the LLM; the engine only does control/gate/math around it.
//	    role          The two stance roles (skeptic/advocate) that preserve fork-on-conflict when
//	                    nothing is runnable — with a REASON, never a fixed string.
//
// The three FAKE specialists (simulation/safety/refactor) and the toy 5-fact MemoryKB are DELETED:
// they manufactured hallucination-with-a-confidence-number inside the engine, exactly what the Filter
// exists to kill. Their capability is recovered evidence-first (run/measure on real branches) with the
// model skeptic/advocate roles as the fallback when nothing is runnable.
//
// On firing a specialist returns a raw *types.Candidate (nil == Python None); the hidden seam
// validates and re-voices it. The seeded *cpyrand.Random is threaded through Fire (never a
// package-global) so the stream is reproducible.
package subconscious

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ctxText joins the last n real thoughts' text, lower-cased, for relevance matching.
//
// The conversation-memory preamble is for reference resolution (respond/generate), NOT for
// specialist relevance — otherwise a prior turn's keywords ('refactor') fire specialists on
// an unrelated new turn ('meaning of life'). Exclude it here. Mirrors Python ctx_text.
func ctxText(ctx []types.Thought, n int) string {
	real := make([]string, 0, len(ctx))
	for _, t := range ctx {
		if !strings.HasPrefix(t.Text, types.RecapPrefix) {
			real = append(real, t.Text)
		}
	}
	if len(real) > n {
		real = real[len(real)-n:]
	}
	return strings.ToLower(strings.Join(real, " \n"))
}

// ctxTextDefault uses the Python default window of n=6.
func ctxTextDefault(ctx []types.Thought) string { return ctxText(ctx, 6) }

// hasAny matches phrases (those containing a space) by substring, single words by word
// boundary (so 'clean' != 'cleanly'). Mirrors Python has_any. The boundary regex is compiled
// per call to match Python's per-call re.search; the trigger lists are tiny so this is cheap.
func hasAny(text string, triggers []string) bool {
	for _, t := range triggers {
		if strings.Contains(t, " ") {
			if strings.Contains(text, t) {
				return true
			}
		} else if regexp.MustCompile(`\b` + regexp.QuoteMeta(t) + `\b`).MatchString(text) {
			return true
		}
	}
	return false
}

// ============================================================================
// PrimitiveSubAgent interface + collaborator ports
// ============================================================================

// PrimitiveSubAgent is a silent sub-agent bound to one primitive — a worker faculty (compute/recall/
// read/search/run/skeptic/advocate/social). It is the reference behind ephemeral SubAgent instances.
// (RENAMED from "Specialist", the retired prior name — one set of truth across doc/code/test per the
// cognition redesign, docs/cognition/01-subconscious.md §3.7. The "specialist" CONTENT role on
// backends.SpecialistCaller, the "specialist" event-kind/wire values, and the persist SpecialistRecord
// are STABLE CONTRACTS and deliberately keep the old word.) Python's ABC with a `domain` class
// attribute becomes a Domain() accessor so concrete structs carry their own domain string.
type PrimitiveSubAgent interface {
	// Domain is the specialist's domain tag (Python's `domain` class/instance attribute),
	// stamped onto every Candidate it produces.
	Domain() string

	// Relevance reports how strongly this primitive lights up for the current context (0..1).
	Relevance(ctx []types.Thought) float64

	// Fire runs silently and returns a raw *Candidate payload, or nil if nothing to say
	// (Python's `Candidate | None`). The seeded rng is threaded in.
	Fire(ctx []types.Thought, rng *cpyrand.Random) *types.Candidate
}

// parallelSafePrimitiveSubAgent is the OPT-IN marker for a base specialist whose Fire is safe to run
// CONCURRENTLY in the per-tick fan-out (07-OPTIMISATION-SURVEY.md §A.1 seam #2). A type implements it
// (the no-op ParallelReasonOnly method) ONLY when its Fire is:
//
//	(a) RNG-free      — it ignores the rng arg (so completion order cannot reorder the seeded stream);
//	(b) effect-free   — no executor/sandbox dispatch, no shared-mutable-state write mid-fire (it reads a
//	                    context slice and returns a *Candidate; the graph/value/regulator are touched only
//	                    AFTER Dispatch, on the single tick thread);
//	(c) bus-silent    — it makes NO direct e.emit/bus call inside Fire (the dispatch-level emitFire runs in
//	                    the SERIAL roster loop in index order, so nothing needs buffering — unlike a SubAgent
//	                    whose fireReason emits subconscious.subagent and must be fireBuffered);
//	(d) model-bound   — it makes ONE background-budget model call (backend.Specialist), the work worth
//	                    overlapping; a pure specialist (compute/recall/minted) makes no model call, so there
//	                    is nothing to overlap and it is left to the serial loop (firing it concurrently would
//	                    add scheduling cost for no decode saving).
//
// This is the EXACT base-specialist analogue of SubAgent.reasonOnly() (the seam #1 safety gate). Marking
// is OPT-IN (a type must declare the method) so a NEW specialist is serial-by-default and can only join the
// concurrent set by an author asserting these four properties — the conservative, always-correct posture
// (an unmarked specialist is byte-identical to today). The effectful read/search/run primitives, the solver
// (it emits subconscious.solver_formalize inside Fire AND is opt-in/dark), and the pure compute/recall/minted
// specialists deliberately do NOT implement it.
type parallelSafePrimitiveSubAgent interface {
	// ParallelReasonOnly is a no-op marker: its PRESENCE asserts the four properties above. The method
	// body does nothing — it exists so the dispatcher can detect the marker by type assertion.
	ParallelReasonOnly()
}

// MemoryRecaller is the real-store recall port the `recall` primitive consults (M2 §2.4 — the worst
// synergy-gap fix). The engine satisfies it by wiring the live memory.SemanticRegistry +
// EpisodicRegistry through their never-fabricate, relevance-gated Recall (the toy MemoryKB is gone).
// An interface (not the concrete memory types) keeps the subconscious package decoupled + swappable.
type MemoryRecaller interface {
	// RecallFact returns the single best relevant grounded statement for query and ok=true, or
	// ("", false) on a miss (the precision floor / never-fabricate gate — never a fabricated answer).
	RecallFact(query string) (statement string, ok bool)
}

// ExecutorProvider hands a tool-backed primitive the Action-layer executor so it can dispatch a
// least-privilege, sandbox-gated scoped tool at Fire time. The SubconsciousEngine satisfies it (its
// executor is wired post-construction via SetExecutor), so primitives are constructible in
// DefaultPrimitiveSubAgents before the workspace executor exists, then resolve it lazily when they fire.
type ExecutorProvider interface {
	Executor() *action.ToolExecutor
}

// emitter is the optional bus hook a primitive uses to log a tool-backed fire (recall hit, run
// observation). nil ⇒ silent. The dispatch loop also emits subconscious.fire; this is the
// primitive-internal detail (e.g. which store recall drew from) the dispatch event cannot carry.
type emitter interface{ Emit() events.Emit }

// cand builds an INJECTED candidate stamped with the given domain (Python PrimitiveSubAgent._cand).
// The optional fields (stance/operator/payload) are passed via the cand options applied here.
func cand(domain, text string, relevance float64, opts ...candOpt) *types.Candidate {
	dom := domain
	c := &types.Candidate{
		Text:      text,
		Source:    types.INJECTED,
		Domain:    &dom,
		Relevance: relevance,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// candOpt is a functional option mirroring Python's **kw on _cand (stance/operator/payload).
type candOpt func(*types.Candidate)

func withStance(stance string) candOpt {
	return func(c *types.Candidate) { s := stance; c.Stance = &s }
}
func withOperator(op types.Operator) candOpt {
	return func(c *types.Candidate) { o := op; c.Operator = &o }
}
func withPayload(p any) candOpt { return func(c *types.Candidate) { c.Payload = p } }

// ============================================================================
// (T) tool-backed primitive: compute  — the de-domained exact evaluator
// ============================================================================

// arithRe matches a binary arithmetic expression (Python _ARITH).
var arithRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([x×*+\-/])\s*(\d+(?:\.\d+)?)`)

// wordOp pairs a natural-language operator regex with its symbol, so "7 times 8" /
// "10 divided by 2" compute too. Order preserved from Python _WORD_OPS.
type wordOp struct {
	re  *regexp.Regexp
	sym string
}

var wordOps = []wordOp{
	{regexp.MustCompile(`\b(?:times|multiplied by)\b`), "*"},
	{regexp.MustCompile(`\bplus\b`), "+"},
	{regexp.MustCompile(`\bminus\b`), "-"},
	{regexp.MustCompile(`\bdivided by\b`), "/"},
}

// normArith rewrites natural-language operators to symbols (Python _norm_arith).
func normArith(text string) string {
	for _, w := range wordOps {
		text = w.re.ReplaceAllString(text, w.sym)
	}
	return text
}

// ComputePrimitiveSubAgent is the `compute` primitive (M2 §2.2): a TOOL-BACKED de-domained exact evaluator.
// It carries real ground truth (an exact arithmetic answer, computed deterministically) — it does
// not opine. This is the old ArithmeticSpecialist recast from a domain into a capability: the move is
// the same (compute the binary expression in context), the domain tag is now the capability `compute`.
type ComputePrimitiveSubAgent struct{}

func (ComputePrimitiveSubAgent) Domain() string { return "compute" }

// match returns the regex submatches for an arithmetic expression, or nil. Mirrors Python
// _ARITH.search(_norm_arith(ctx_text(ctx))): index 0 is the whole match, 1/2/3 the groups.
func (ComputePrimitiveSubAgent) match(ctx []types.Thought) []string {
	return arithRe.FindStringSubmatch(normArith(ctxTextDefault(ctx)))
}

func (a ComputePrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if a.match(ctx) != nil {
		return 0.95
	}
	return 0.0
}

func (a ComputePrimitiveSubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	m := a.match(ctx)
	if m == nil {
		return nil
	}
	av, _ := strconv.ParseFloat(m[1], 64)
	op := m[2]
	bv, _ := strconv.ParseFloat(m[3], 64)
	if op == "/" && bv == 0 {
		return nil // division by zero is undefined — don't manufacture an answer
	}
	var val float64
	switch op {
	case "x", "×", "*":
		val = av * bv
	case "+":
		val = av + bv
	case "-":
		val = av - bv
	case "/":
		val = av / bv
	}
	valS := formatArith(val)
	sym := op
	if op == "x" || op == "×" || op == "*" {
		sym = "×"
	}
	return cand("compute", m[1]+" "+sym+" "+m[3]+" = "+valS, 0.95, withPayload(val))
}

// formatArith reproduces Python `str(int(val)) if val == int(val) else f"{val:.4g}"`.
func formatArith(val float64) string {
	if val == math.Trunc(val) && !math.IsInf(val, 0) {
		return strconv.FormatInt(int64(val), 10)
	}
	return format4g(val)
}

// ============================================================================
// (T) tool-backed primitive: recall  — real retrieval over the REAL memory store
// ============================================================================

// recallTriggers light the recall primitive up: an explicit recall request, OR a question shape that
// wants a stored fact. Kept deliberately small — recall is gated by the store actually HAVING a
// relevant grounded record (the never-fabricate precision floor), not by the keyword alone.
var recallTriggers = []string{
	"recall", "remember", "fact", "do we know", "have we", "what do i know", "what do we know",
	"prior", "before", "last time", "earlier",
}

// RecallPrimitiveSubAgent is the `recall` primitive (M2 §2.4): TOOL-BACKED retrieval over the REAL memory
// registries (memory.Semantic/EpisodicRegistry) through the injected MemoryRecaller — the worst-gap
// fix. The old MemorySpecialist read a hardcoded 5-fact dict completely decoupled from everything the
// engine grounds; this one reaches the live store, so accumulated grounded knowledge is finally
// REACHABLE by the conscious stream it pulls from. It NEVER fabricates a miss: an empty store, or a
// query the store has nothing relevant for, fires nothing (Relevance/Fire return 0/nil).
type RecallPrimitiveSubAgent struct {
	recaller MemoryRecaller // the real-store port (nil ⇒ never fires — no toy fallback)
}

// NewRecallPrimitiveSubAgent builds the recall primitive over the real memory store. A nil recaller is a
// dark primitive (it never fires) — there is deliberately NO hardcoded-fact fallback (the toy KB is
// gone): with no store wired, recall has nothing grounded to surface, so it stays silent.
func NewRecallPrimitiveSubAgent(recaller MemoryRecaller) *RecallPrimitiveSubAgent {
	return &RecallPrimitiveSubAgent{recaller: recaller}
}

func (RecallPrimitiveSubAgent) Domain() string { return "recall" }

// query derives the recall query from the recent real context (the same window other specialists use).
func (RecallPrimitiveSubAgent) query(ctx []types.Thought) string { return ctxTextDefault(ctx) }

func (r *RecallPrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if r.recaller == nil {
		return 0.0
	}
	// Light up only when the context asks for recall AND the real store actually has a relevant
	// grounded record — the never-fabricate precision floor decides, not the keyword alone.
	if !hasAny(ctxTextDefault(ctx), recallTriggers) {
		return 0.0
	}
	if _, ok := r.recaller.RecallFact(r.query(ctx)); ok {
		return 0.85
	}
	return 0.0
}

func (r *RecallPrimitiveSubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	if r.recaller == nil {
		return nil
	}
	fact, ok := r.recaller.RecallFact(r.query(ctx))
	if !ok {
		return nil // never-fabricate: a miss recalls nothing, it does not invent
	}
	// A recalled fact is grounded knowledge from the real store — stamp it INJECTED with the recall
	// domain. The Filter still validates it (it could be stale), but it is sourced, not fabricated.
	return cand("recall", fact, 0.85, withPayload("recall:store"))
}

// ============================================================================
// (T) tool-backed primitives: read / search / run  — the senses + hands
// ============================================================================

// readTriggers / searchTriggers / runTriggers are the genuine "go look at reality" requests. They are
// deliberately NOT bare "runtime/output/execute", which match the words inside a FAILURE observation
// ("NameError at runtime") and would create a feedback loop. A real tool only fires with a bound
// executor (a configured workspace) — offline these primitives stay dark.
var (
	readTriggers   = []string{"read the file", "open the file", "look at the file", "what is in the file", "contents of"}
	searchTriggers = []string{"find where", "search for", "where is", "grep for", "locate the", "where in the code"}
	runTriggers    = []string{"will this run", "will it run", "does this run", "will this code run",
		"does the code run", "run correctly", "run the tests", "run the test suite", "does it pass"}
)

// toolBackedPrimitive is read / search / run (M2 §2.2): TOOL-BACKED senses+hands. Each carries real
// ground truth — it dispatches a least-privilege, sandbox-gated scoped tool through the Action-layer
// executor and folds the GENUINE ToolResult into its candidate. NONE of them writes the world: a
// subconscious primitive may READ reality silently, never CHANGE it (write/edit is an Action-layer action at
// the Controller's ACT decision — the trichotomy's hard line, §2.2). With no executor (offline) they
// stay dark — there is no manufactured "it runs" stand-in (that fake is exactly what M2 deletes).
type toolBackedPrimitive struct {
	domain    string           // the primitive name = its capability ("read" | "search" | "run")
	triggers  []string         // the "go look at reality" requests that light it up
	relevance float64          // its firing relevance when lit + an executor is bound
	provider  ExecutorProvider // resolves the Action-layer executor at Fire time (nil ⇒ never fires)
	emit      events.Emit      // optional bus hook for the observation (nil ⇒ silent)
	// scope picks the least-privilege tool list for this primitive; build distils the concrete
	// ToolCall from the context (returns ok=false ⇒ nothing concrete to run ⇒ the primitive is dark).
	scope []string
	build func(ctx []types.Thought) (action.ToolCall, bool)
	op    types.Operator // the Operator enum stamped on the candidate (SIMULATE for run, VALIDATE else)
	// comp is the SHARED LLM "to_operator" comprehension (Pattern-C CEILING): given the context, ONE call
	// yields the needed capability (read|search|run|none) AND its target (the path/pattern the agent
	// INTENDS). When wired (a real model) it REPLACES both the keyword triggers (the firing decision) and
	// the regex target extraction — so "I need to read config/risk.yaml" fires read AND reads config/
	// risk.yaml. nil ⇒ the keyword-trigger + regex FLOOR stands (the test double keeps goldens
	// byte-identical). On model decline it also falls back to the floor. The cache makes the read/search/
	// run primitives share ONE Comprehend call per dispatch instead of one each.
	comp *comprehendCache
}

// comprehendCache memoizes the ONE Comprehend call per dispatch so the read/search/run primitives SHARE a
// single LLM "to_operator" call (not one each). Keyed by the context text: within a dispatch the three
// primitives see the same ctx and hit the cache; a new tick's ctx recomputes. nil rec / decline ⇒ ok=false
// ⇒ the primitives fall back to the keyword-trigger + regex floor.
type comprehendCache struct {
	rec       backends.RealityComprehender
	key       string
	need, tgt string
	ok, set   bool
}

func (c *comprehendCache) get(ctx []types.Thought) (need, target string, ok bool) {
	if c == nil || c.rec == nil {
		return "", "", false
	}
	if k := ctxTextDefault(ctx); !c.set || k != c.key {
		c.key, c.set = k, true
		c.need, c.tgt, c.ok = c.rec.Comprehend(ctx)
	}
	return c.need, c.tgt, c.ok
}

func (p *toolBackedPrimitive) Domain() string { return p.domain }

// ToolFootprint returns the worker faculty's least-privilege tool-name capability footprint (§3.7: a
// worker faculty = competence + persona + tool-categories) — the concrete tool NAMES THIS tool-backed
// primitive declares it may dispatch (read ⇒ {read_file}, search ⇒ {search}, run ⇒ {run_tests}). It is the
// SAME least-privilege `scope` the primitive dispatches through (one source of truth), exposed so the
// category-sourced staffing resolver (toolpick.go) can build the worker-faculty footprint from the LIVE
// roster rather than a hardcoded duplicate. A copy is returned so a caller cannot mutate the primitive's
// scope. This satisfies the toolFootprinter interface (toolpick.go).
func (p *toolBackedPrimitive) ToolFootprint() []string { return append([]string(nil), p.scope...) }

// exec resolves the bound executor (nil ⇒ offline / no workspace).
func (p *toolBackedPrimitive) exec() *action.ToolExecutor {
	if p.provider == nil {
		return nil
	}
	return p.provider.Executor()
}

func (p *toolBackedPrimitive) Relevance(ctx []types.Thought) float64 {
	if p.exec() == nil { // no real tools available ⇒ dark (no manufactured stand-in)
		return 0.0
	}
	if !p.needed(ctx) {
		return 0.0
	}
	if _, ok := p.buildCall(ctx); !ok { // nothing concrete to run for this context
		return 0.0
	}
	return p.relevance
}

// needed is the firing decision: does this context call for THIS primitive's reality observation?
// Pattern-C — the LLM comprehension is the CEILING (the agent decides what to observe, reading the model's
// own expressed need), the keyword triggers are the FLOOR. With a model wired we use its verdict; on
// decline / no model we fall back to the literal triggers (so the deterministic test double — which is NOT
// a RealityComprehender — keeps every golden byte-identical).
func (p *toolBackedPrimitive) needed(ctx []types.Thought) bool {
	if need, _, ok := p.comp.get(ctx); ok {
		return need == p.domain
	}
	return hasAny(ctxTextDefault(ctx), p.triggers)
}

// buildCall distils the concrete ToolCall: the LLM-comprehended TARGET (the path/pattern the agent
// INTENDS — including a self-corrected path) is the CEILING; the regex extractor (p.build) is the FLOOR.
// read/search bind the target directly; run keeps the floor (its target is a command and its scope is
// run_tests-only). On no model / decline / empty target it falls through to p.build (the test double's
// deterministic path — goldens unchanged).
func (p *toolBackedPrimitive) buildCall(ctx []types.Thought) (action.ToolCall, bool) {
	if need, target, ok := p.comp.get(ctx); ok && need == p.domain && target != "" {
		switch p.domain {
		case "read":
			return action.ToolCall{Name: "read_file", Args: map[string]any{"path": target}}, true
		case "search":
			return action.ToolCall{Name: "search", Args: map[string]any{"pattern": target}}, true
		}
	}
	return p.build(ctx) // regex floor (also the whole run path)
}

func (p *toolBackedPrimitive) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	exe := p.exec()
	if exe == nil {
		return nil
	}
	call, ok := p.buildCall(ctx)
	if !ok {
		return nil
	}
	result := exe.Scoped(p.scope).Execute(call) // action.* events emitted inside Execute
	text := action.SummarizeToolResult(result)
	if p.emit != nil {
		p.emit(events.SubFire, p.domain+" ▸ "+call.Name+": "+clipRunes(text, 36), events.D{
			"domain": p.domain, "tool": call.Name, "ok": !result.IsError, "exit_code": result.ExitCode,
		})
	}
	// A real refutation reads with the "fail" stance so the seam / Critic treat a failure as signal —
	// this is the evidence-first half of fork-on-conflict: run on two branches genuinely differ.
	opts := []candOpt{withOperator(p.op), withPayload(result)}
	if result.IsError {
		opts = append(opts, withStance("fail"))
	}
	return cand(p.domain, text, p.relevance, opts...)
}

// newReadPrimitive builds the `read` primitive (read_file): ground a claim against the real artifact.
// It distils its ToolCall through the ONE shared selector (action.SelectTool) — the SAME selector the
// watched seam and sub-agents use — falling back to the local filePath extractor so no path the old
// build could catch is lost (the selector is a superset, never a regression).
func newReadPrimitive(provider ExecutorProvider, emit events.Emit, comp *comprehendCache) *toolBackedPrimitive {
	return &toolBackedPrimitive{
		domain: "read", triggers: readTriggers, relevance: 0.8, provider: provider, emit: emit,
		scope: []string{"read_file"}, op: types.VALIDATE, comp: comp,
		build: func(ctx []types.Thought) (action.ToolCall, bool) {
			text := ctxTextDefault(ctx)
			if call, ok := action.SelectTool(text, ""); ok && call.Name == "read_file" {
				return call, true
			}
			// superset guard: keep the local "/"-or-any-short-extension path extractor so a path the
			// curated selector doesn't recognise (no listed extension) still reads.
			if path := filePath(text); path != "" {
				return action.ToolCall{Name: "read_file", Args: map[string]any{"path": path}}, true
			}
			return action.ToolCall{}, false
		},
	}
}

// newSearchPrimitive builds the `search` primitive (search): find where something is in the real tree.
// Like read, it routes through action.SelectTool first, falling back to the local keyword extractor so
// no pattern the old build could catch is lost.
func newSearchPrimitive(provider ExecutorProvider, emit events.Emit, comp *comprehendCache) *toolBackedPrimitive {
	return &toolBackedPrimitive{
		domain: "search", triggers: searchTriggers, relevance: 0.8, provider: provider, emit: emit,
		scope: []string{"search"}, op: types.VALIDATE, comp: comp,
		build: func(ctx []types.Thought) (action.ToolCall, bool) {
			text := ctxTextDefault(ctx)
			if call, ok := action.SelectTool(text, ""); ok && call.Name == "search" {
				return call, true
			}
			// superset guard: the local keyword extractor still picks the first >3-char token.
			if kw := keyword(text); kw != "" {
				return action.ToolCall{Name: "search", Args: map[string]any{"pattern": kw}}, true
			}
			return action.ToolCall{}, false
		},
	}
}

// newRunPrimitive builds the `run` primitive (run_tests): don't GUESS "it runs" — run it, read reality.
// This is what recovers the deleted `simulation` fake: a real execution instead of a confident string.
// The candidate carries the SIMULATE operator so the watched-seam / refutation lineage reads the same.
// Its run-the-suite intent goes through action.SelectTool with Kind="run" (the shared selector): for
// these triggers (no explicit command) that yields run_tests{} — identical to the old hardcoded body.
// The primitive's least-privilege scope is run_tests ONLY, so the result is constrained to run_tests
// (a selector-returned run_shell is out of scope here — that path belongs to a command-scoped agent);
// anything other than run_tests falls back to run_tests{}.
func newRunPrimitive(provider ExecutorProvider, emit events.Emit, comp *comprehendCache) *toolBackedPrimitive {
	return &toolBackedPrimitive{
		domain: "run", triggers: runTriggers, relevance: 0.8, provider: provider, emit: emit,
		scope: []string{"run_tests"}, op: types.SIMULATE, comp: comp,
		build: func(ctx []types.Thought) (action.ToolCall, bool) {
			if call, ok := action.SelectTool(ctxTextDefault(ctx), "run"); ok && call.Name == "run_tests" {
				return call, true
			}
			return action.ToolCall{Name: "run_tests", Args: map[string]any{}}, true
		},
	}
}

// filePath finds the first whitespace token in text whose SHAPE is a real local-file path, or "" — the
// recall/read primitive's cheap path extractor (the superset guard behind action.SelectTool).
//
// FIX (#35, the slash false-positive — the subconscious twin of cognition.synthFilePath). The old
// extractor returned ANY token containing "/", so a bare "word/word" ("yes/no", "and/or"), a date
// "12/25", a ratio "3/4", or a URL read as a local file and mis-routed the read precedence. A path now
// REQUIRES a recognized file extension on the leaf (looksLikePath); the rule is the SAME as cognition's so
// the two tiers agree on "names a local file".
func filePath(text string) string {
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, "\"'`.,;:()[]{}")
		if looksLikePath(w) {
			return w
		}
	}
	return ""
}

// pathExts is the recognized local-file extension set a path-shaped leaf must carry — the subconscious
// copy of cognition.pathExts / action.selector's filePathRe set (cognition is a lower tier and cannot be
// imported here, so the rule is duplicated, kept in sync by intent). A bare "word/word" slash with no
// recognized extension is NOT a path.
var pathExts = map[string]bool{
	"go": true, "py": true, "yaml": true, "yml": true, "md": true, "txt": true,
	"json": true, "toml": true, "sh": true, "js": true, "ts": true, "rs": true,
	"c": true, "h": true, "cpp": true, "java": true, "rb": true, "cfg": true,
	"ini": true, "conf": true, "env": true, "mod": true, "sum": true, "lock": true,
}

// looksLikePath reports whether a (punctuation-trimmed) token has the SHAPE of a real local-file path: a
// leaf with a recognized file extension, OR a path with a separator whose final segment is such a leaf. A
// bare "word/word" slash, a URL, a date "12/25", or a ratio "3/4" are NOT paths. Twin of
// cognition.looksLikePath (#35).
func looksLikePath(w string) bool {
	if w == "" {
		return false
	}
	if strings.Contains(w, "://") { // a URL scheme is never a local file
		return false
	}
	leaf := w
	if i := strings.LastIndexByte(w, '/'); i >= 0 {
		leaf = w[i+1:]
	}
	i := strings.LastIndexByte(leaf, '.')
	if i < 0 || i == len(leaf)-1 {
		return false // no dot, or trailing dot (no extension)
	}
	return pathExts[strings.ToLower(leaf[i+1:])]
}

// ============================================================================
// (M) model-driven roles: skeptic / advocate  — fork-on-conflict, with a REASON
// ============================================================================

// skepticTriggers / advocateTriggers light the two stance roles. They preserve the fork-on-conflict
// the deleted safety/refactor pair faked — but as a MODEL role (content from the LLM, with a reason),
// never a fixed string. Both light on the same safety/change-review shape so the Gate sees the conflict.
var (
	skepticTriggers  = []string{"safe", "risk", "danger", "break", "regress", "refactor", "ship", "secure"}
	advocateTriggers = []string{"safe", "refactor", "clean", "improve", "tidy", "simplify", "ship", "preserve"}
)

// RolePrimitiveSubAgent is a MODEL-DRIVEN stance role (M2 §2.2): skeptic (argues unsafe/refutes) or advocate
// (argues safe/supports). The content is the model's — when a SpecialistCaller backend is wired, Fire
// makes a domain-scoped model call so the stance comes with a REASON read from the actual context; the
// engine only stamps the stance + relevance (control/gate, not content). With no model wired the role
// stays dark (no canned opinion) — its capability returns only when a model can supply the reasoning,
// faithful to feedback-heuristic-control-only (output CONTENT must be the model, never a heuristic).
type RolePrimitiveSubAgent struct {
	domain      string                    // "skeptic" | "advocate"
	stance      string                    // the stance stamped on the candidate ("unsafe" | "safe")
	triggers    []string                  // the review-shape keywords that light it up
	description string                    // the model-call persona description
	relevance   float64                   // firing relevance
	backend     backends.SpecialistCaller // the model port (nil ⇒ dark — no canned fallback)
}

// NewRolePrimitiveSubAgent builds a model-driven stance role. A nil backend is a dark role (it never fires):
// there is NO canned-string fallback — a stance with no reason is exactly the manufactured opinion M2
// deletes. The role's capability returns only when a model can supply the reasoning.
func NewRolePrimitiveSubAgent(domain, stance string, triggers []string, description string,
	backend backends.SpecialistCaller) *RolePrimitiveSubAgent {
	return &RolePrimitiveSubAgent{
		domain: domain, stance: stance, triggers: lowerAll(triggers),
		description: description, relevance: 0.75, backend: backend,
	}
}

func (r *RolePrimitiveSubAgent) Domain() string { return r.domain }

func (r *RolePrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if r.backend == nil { // no model ⇒ no reasoned stance ⇒ dark (never a canned opinion)
		return 0.0
	}
	if hasAny(ctxTextDefault(ctx), r.triggers) {
		return r.relevance
	}
	return 0.0
}

func (r *RolePrimitiveSubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	if r.backend == nil {
		return nil
	}
	text, ok := r.backend.Specialist(r.domain, r.description, ctx)
	if !ok || strings.TrimSpace(text) == "" {
		return nil // the model declined / produced nothing — fire nothing, never a stand-in string
	}
	// VALIDATE is the assess-lane operator both stances carry; the stance is what the Gate forks on.
	return cand(r.domain, text, r.relevance, withStance(r.stance), withOperator(types.VALIDATE))
}

// ParallelReasonOnly marks the stance role as safe for the per-tick concurrent fan-out (seam #2): its
// Fire ignores the rng (RNG-free), takes no external action (a pure backend.Specialist model call),
// writes no shared state mid-fire, and makes NO direct bus emit inside Fire (the dispatch loop emits
// subconscious.fire for it in index order). So overlapping the skeptic/advocate model calls is
// byte-identical to serial — only the wall-clock changes. See parallelSafePrimitiveSubAgent.
func (r *RolePrimitiveSubAgent) ParallelReasonOnly() {}

// ============================================================================
// (M) model-driven role: social — the conversational faculty
// ============================================================================

// socialGreetRe anchors a greeting at the START of the turn ("hi", "hello there", "good morning…");
// a "hi" buried mid-sentence in a task description is not a greeting turn.
var socialGreetRe = regexp.MustCompile(`^\s*(hi+|hello+|hey+|yo|howdy|greetings|good\s+(morning|afternoon|evening))\b`)

// socialPhrases are whole-utterance social shapes: courtesies, phatic check-ins, identity/wellbeing
// probes. Deliberately small and unambiguous — the deterministic floor must never hijack a real task.
var socialPhrases = []string{
	"are you there", "you there", "can you hear me", "anyone there",
	"how are you", "how's it going", "who are you", "what are you",
	"thank you", "thanks", "goodbye", "good night", "bye", "see you",
}

// socialShaped reports whether a user turn is SOCIAL (greeting / courtesy / check-in) rather than a
// task. Pattern-A floor: anchored greeting or a known social phrase, and SHORT — long messages carry
// task content even when they open with "hi".
func socialShaped(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || len(strings.Fields(t)) > 12 {
		return false
	}
	if socialGreetRe.MatchString(t) {
		return true
	}
	for _, p := range socialPhrases {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

// latestRealThought returns the most recent non-METACOG thought in ctx (the live head of the line),
// or nil. METACOG markers (focus/branch notes) ride between real thoughts and are skipped.
func latestRealThought(ctx []types.Thought) *types.Thought {
	for i := len(ctx) - 1; i >= 0; i-- {
		if ctx[i].Source != types.METACOG {
			return &ctx[i]
		}
	}
	return nil
}

// SocialPrimitiveSubAgent is the conversational faculty — the social analogue of ComputePrimitiveSubAgent. Where
// math has an exact evaluator that fires on an expression, conversation has THIS: it fires when the
// live head of the line is a SOCIAL user turn (greeting / courtesy / check-in) and injects the
// immediate response candidate, model-voiced (Pattern B — never a canned line; dark with no model).
// The Controller's existing GOAL_MET machinery then closes the line in a tick or two, exactly as it
// does for arithmetic — so the awake mind answers "hi" in seconds, not at the tick-47 give-up horizon
// (the measured 2026-06-12 gap: docs/internal/archive/reports/2026-06-12-awake-interaction-reassessment.md §8). The
// same faculty IS the early-ack behaviour the product decisions ask for: a well-socialised mind's
// fast first response to being addressed.
type SocialPrimitiveSubAgent struct {
	backend backends.SpecialistCaller // the model port (nil ⇒ dark — no canned fallback)
}

// NewSocialPrimitiveSubAgent builds the conversational faculty. A nil backend is dark (it never fires):
// a social reply with no model behind it would be a manufactured voice — exactly what Pattern B forbids.
func NewSocialPrimitiveSubAgent(backend backends.SpecialistCaller) *SocialPrimitiveSubAgent {
	return &SocialPrimitiveSubAgent{backend: backend}
}

func (SocialPrimitiveSubAgent) Domain() string { return "social" }

// Relevance fires HIGH (0.95, the compute tier) only while the user's social turn is the live head of
// the line — once any response/thought lands after it, relevance drops to 0 (no re-greeting loops).
func (s *SocialPrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if s.backend == nil {
		return 0.0
	}
	if t := latestRealThought(ctx); t != nil && t.Source == types.USER_INPUT && socialShaped(t.Text) {
		return 0.95
	}
	return 0.0
}

func (s *SocialPrimitiveSubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	if s.Relevance(ctx) == 0.0 {
		return nil
	}
	text, ok := s.backend.Specialist("social",
		"You are the conversational faculty. The user just addressed you socially (a greeting, "+
			"courtesy, or check-in). Reply to them naturally in first person — warm, direct, one or "+
			"two short sentences, no preamble.", ctx)
	if !ok || strings.TrimSpace(text) == "" {
		return nil // the model declined — fire nothing, never a stand-in string
	}
	return cand("social", text, 0.95)
}

// ParallelReasonOnly marks the conversational faculty as safe for the per-tick concurrent fan-out
// (seam #2): its Fire ignores the rng (RNG-free), takes no external action (a pure backend.Specialist
// model call), writes no shared state mid-fire, and makes NO direct bus emit inside Fire (the dispatch
// loop emits subconscious.fire for it in index order). So overlapping the social model call with the
// other reason-only specialists is byte-identical to serial. See parallelSafePrimitiveSubAgent.
func (s *SocialPrimitiveSubAgent) ParallelReasonOnly() {}

// ============================================================================
// MintedPrimitiveSubAgent — convertibility's payoff
// ============================================================================

// MintedPrimitiveSubAgent is a primitive subagent compiled by the convertibility subsystem from a
// repeated GENERATED pattern. Its existence is the observable payoff of practice: work that used to be
// effortful now arrives injected (spec S7).
type MintedPrimitiveSubAgent struct {
	domain    string
	triggers  []string
	answer    string
	relevance float64
	demoted   bool // reverted by convertibility's keep-or-revert when reality refuted its pattern
}

// NewMintedPrimitiveSubAgent builds a minted specialist. Triggers are lower-cased (Python __init__
// `tuple(t.lower() for t in triggers)`); relevance defaults to 0.9 in Python — pass it.
func NewMintedPrimitiveSubAgent(domain string, triggers []string, answer string, relevance float64) *MintedPrimitiveSubAgent {
	return &MintedPrimitiveSubAgent{
		domain:    domain,
		triggers:  lowerAll(triggers),
		answer:    answer,
		relevance: relevance,
	}
}

func (m *MintedPrimitiveSubAgent) Domain() string { return m.domain }

// Triggers / Answer / RelevanceValue expose the minted specialist's compiled state so cross-session
// persistence (M4) can round-trip it: a minted specialist saved on one run is re-registered on the next
// with the same triggers/answer/relevance (the worked answer is REAL — captured when the pattern
// converged, never a fabricated template). A copy of the trigger slice is returned (the caller cannot
// mutate the specialist's internals). RelevanceValue is the standing relevance the specialist fires at.
func (m *MintedPrimitiveSubAgent) Triggers() []string      { return append([]string(nil), m.triggers...) }
func (m *MintedPrimitiveSubAgent) Answer() string          { return m.answer }
func (m *MintedPrimitiveSubAgent) RelevanceValue() float64 { return m.relevance }

// Demote reverts a minted specialist: convertibility calls it when reality REFUTES the pattern the
// specialist was compiled from (keep-or-revert, P0.5). A demoted specialist stays in the roster for
// the trace but never fires again (Relevance==0), so a mint that practice produced but reality
// disproved stops injecting its now-discredited answer.
func (m *MintedPrimitiveSubAgent) Demote() { m.demoted = true }

// Demoted reports whether this minted specialist has been reverted (read by the registry browser and
// the convertibility keep-or-revert tests).
func (m *MintedPrimitiveSubAgent) Demoted() bool { return m.demoted }

func (m *MintedPrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if m.demoted {
		return 0.0 // reverted by keep-or-revert — no longer fires
	}
	if hasAny(ctxTextDefault(ctx), m.triggers) {
		return m.relevance
	}
	return 0.0
}

func (m *MintedPrimitiveSubAgent) Fire(_ []types.Thought, _ *cpyrand.Random) *types.Candidate {
	return cand(m.domain, m.answer, m.relevance, withPayload("minted"))
}

// ============================================================================
// Roster builder — the 7-primitive real set
// ============================================================================

// DefaultPrimitiveSubAgents builds the REAL primitive roster (M2 §2.2): the 7-primitive set —
//
//	tool-backed:  compute · recall · read · search · run   (the senses + hands; carry ground truth)
//	model-driven: skeptic · advocate                       (the two stance roles; content = the LLM)
//
// compute is always live (exact deterministic math). recall is wired to the real memory store through
// the MemoryRecaller (nil ⇒ dark, no toy fallback). read/search/run resolve the Action-layer executor
// lazily via the ExecutorProvider (nil/offline ⇒ dark — no manufactured "it runs"). skeptic/advocate
// fire only when a SpecialistCaller model backend is wired (nil ⇒ dark — no canned opinion). The fake
// simulation/safety/refactor specialists and the toy MemoryKB are DELETED.
//
// recaller / provider / emit may be nil for the bare offline path (only compute is live then). The
// engine passes the live ports. caller is the model port (the backend asserted to SpecialistCaller).
//
// solverFormalizer + solverEnabled gate the OPT-IN 5th-axis classical solver specialist (domain
// "solver", default OFF): it is appended to the roster ONLY when solverEnabled is true (the
// subconscious.solver_specialist knob is on). When solverEnabled is false the roster is byte-identical
// to before (the specialist is simply absent) — the default-OFF/byte-identical guarantee. Even with
// solverEnabled on, a nil solverFormalizer leaves the specialist DARK (it never fires) — so on the test
// double (which is NOT a StructureFormalizer) the specialist is present-but-silent, goldens unchanged.
func DefaultPrimitiveSubAgents(recaller MemoryRecaller, provider ExecutorProvider,
	caller backends.SpecialistCaller, emit events.Emit, comprehender backends.RealityComprehender,
	solverFormalizer backends.StructureFormalizer, solverEnabled bool) []PrimitiveSubAgent {
	// ONE shared comprehension cache so read/search/run make a single Comprehend call per dispatch (not
	// one each). nil comprehender (the test double) ⇒ the cache returns ok=false ⇒ the keyword+regex floor.
	comp := &comprehendCache{rec: comprehender}
	out := []PrimitiveSubAgent{
		ComputePrimitiveSubAgent{},
		NewRecallPrimitiveSubAgent(recaller),
		NewSocialPrimitiveSubAgent(caller),
		newReadPrimitive(provider, emit, comp),
		newSearchPrimitive(provider, emit, comp),
		newRunPrimitive(provider, emit, comp),
		NewRolePrimitiveSubAgent("skeptic", "unsafe", skepticTriggers,
			"You flag risks, edge cases, and possible regressions — with a concrete reason.", caller),
		NewRolePrimitiveSubAgent("advocate", "safe", advocateTriggers,
			"You argue a change preserves behaviour and is sound — with a concrete reason.", caller),
	}
	if solverEnabled {
		out = append(out, NewSolverPrimitiveSubAgent(solverFormalizer, emit))
	}
	return out
}

// ============================================================================
// small helpers
// ============================================================================

// lowerAll lower-cases every element (Python `tuple(t.lower() for t in triggers)`).
func lowerAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}

// format4g reproduces Python's f"{val:.4g}" — 4 significant figures, trailing zeros stripped,
// exponent form when needed. strconv 'g' with precision 4 matches Python's general format for
// the small arithmetic results this is used on.
func format4g(val float64) string {
	return strconv.FormatFloat(val, 'g', 4, 64)
}
