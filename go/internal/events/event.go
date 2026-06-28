// Package events is the trace/event bus — the spine of all observability.
//
// Components never print. They Emit structured Events; observers (the headless tracer,
// each TUI panel) subscribe. To showcase a component, render its events. Event kinds are
// namespaced by layer — the three layers and their cross-cutting organs: subconscious.*
// (the silent engine), conscious.* (the thinking session), action.* (the reality-facing
// layer), plus seam.*, critic.*, value.*, regulator.*, lifecycle.*, convert.*, port.*.
//
// This package imports nothing from the rest of the tree (it is the leaf both the engine
// and the action layer import), resolving the Python local-import cycle by construction.
package events

// Event is the wire record. Field order matches Python's insertion order
// (tick, kind, layer, summary, data) so encoding/json (struct fields in declaration
// order) reproduces the top-level shape byte-for-byte against the Python JSONL.
type Event struct {
	Tick    int            `json:"tick"`    // bus.tick at emission; set by the engine, NOT by Emit
	Kind    string         `json:"kind"`    // full namespaced kind, e.g. "seam.filter"
	Layer   string         `json:"layer"`   // DERIVED in the bus: split(kind, ".", 1)[0]
	Summary string         `json:"summary"` // one-line console string (pre-truncated at source)
	Data    map[string]any `json:"data"`    // arbitrary per-kind payload
}

// Kind is an alias so the typed constants interoperate freely with the string-typed
// Kind field on Event and every emit call site.
type Kind = string

// D is a terse alias for a data-map literal, keeping emit call sites short:
//
//	bus.Emit(events.Filter, "admit", events.D{"confidence": 0.9})
type D = map[string]any

// Emit is the single settled call shape (Python's emit(kind, summary, **data)). It is
// injected into every component as a closure at construction; the component never holds
// the Bus directly. A plain map[string]any is the straightest reproduction of **data and
// keeps the wire contract obvious at every call site.
type Emit func(kind, summary string, data map[string]any) Event

// Canonical event kinds. The vocabulary began as the Python events.K class (now removed); it is
// grown deliberately as new subsystems are made observable. SOURCE OF TRUTH = this const block.
// To add a kind: declare it here (in its namespace group) AND append it to allKinds below -- two
// append-friendly edits in ONE file, with NO count to bump. TestAllKindsMatchConstBlock derives the
// gate by AST-comparing this block against allKinds, so a drop / duplicate / forgot-to-register
// fails loudly -- and there is no hand-maintained number for two parallel edits to collide on (the
// old EXACT-COUNT gate was a merge hotspot; git history holds the vocabulary growth narrative).
const (
	// SUBCONSCIOUS (wire code: subconscious.*)
	SubDispatch   Kind = "subconscious.dispatch"
	SubFire       Kind = "subconscious.fire"
	SubWorkflow   Kind = "subconscious.workflow"
	SubQuiet      Kind = "subconscious.quiet"
	SubSynthesize Kind = "subconscious.synthesize"  // a workflow PROGRAM constructed on the fly (logged as data)
	SubOperator   Kind = "subconscious.operator"    // a NEW operator synthesised + verified at runtime
	SubSubagent   Kind = "subconscious.subagent"    // a runtime sub-agent instantiated for one operator step
	SkillMatch    Kind = "subconscious.skill_match" // a goal matched to a library skill (goal/action system)
	SkillMint     Kind = "subconscious.skill_mint"  // a Program promoted into a named skill (trace->skill)
	// SubCatalogOffer (W3 synthesiser catalog curation) — the goal-scored, bounded SUBSET of operators
	// offered to the synthesis prompt instead of the whole catalog. The Pattern-A retrieval floor made
	// observable: which operators were put in front of the synthesiser, the cap (top-k), and the full
	// catalog size, so the W2 dynamic-generation panel can show "offered N of M". Emitted only when
	// curation is ON (THOUGHT_SYNTH_CATALOG_TOPK > 0 and the catalog exceeds the cap); the default
	// whole-catalog path is silent (byte-identical). Carries {offered, count, catalog_total, top_k, goal}.
	SubCatalogOffer Kind = "subconscious.catalog_offer"
	// SubSolverFormalize (5th-axis classical solver, docs/internal/2026-06-19-specialized-component-registry-
	// axis.md §5) — the SolverPrimitiveSubAgent formalized a structured sub-problem: the LLM (Pattern-B) wrote the
	// EXPRESSION STRUCTURE (operators/shape, named operands a/b/c — never literals) and a deterministic
	// math/big evaluator computed it after every operand bound to a GROUNDED READ. Carries {expr (the shape),
	// bound (operand->value), sources (operand->the grounded thought it traced to), value (the computed
	// result)}. Emitted ONLY when the opt-in subconscious.solver_specialist is registered AND the specialist
	// actually fires (grounded operands + well-formed AST); default OFF ⇒ the specialist is absent ⇒ no
	// event ⇒ byte-identical goldens.
	SubSolverFormalize Kind = "subconscious.solver_formalize"
	// SubScope (01-subconscious §3.3a, capability-rewire gap 4) — the producing Capability SOURCED this
	// run's authority CEILING (the §3.3a eager Scope): the least-privilege band (domain + allowed tool
	// categories + skill-tier) every staffed SubAgent's tool picks must resolve within. It is "this run's
	// authority" made auditable (the Scope is invisible without it). Emitted ONLY when the opt-in
	// subconscious.capability is on (default OFF ⇒ no Capability sources a Scope ⇒ no event ⇒
	// byte-identical goldens). Carries {domain, categories, skill_tier}.
	SubScope Kind = "subconscious.scope"
	// SubEntry (01-subconscious §3.3, cognition-redesign GAP 5-DEEPER) — the producing Capability is the
	// LIVE relevance/dispatch ENTRY this episode: the subconscious dispatch loop routes its per-tick
	// workflow-recognition THROUGH the Capability (Capability.RecognizeWorkflow) instead of the Workflow
	// self-triggering (Workflow.Recognize). The architectural subsumption made observable — without it the
	// entry is invisible (the recognition verdict is byte-identical, so only this event distinguishes the
	// Capability-entry path from the self-trigger path). Emitted ONLY when the opt-in subconscious.
	// capability_dispatch is on AND a producing Capability exists (default OFF ⇒ the Workflow self-triggers
	// ⇒ no event ⇒ byte-identical goldens). Carries {capability, workflow, triggers}.
	SubEntry Kind = "subconscious.entry"
	// SubSpecGate (01-subconscious §3.3, cognition-redesign GAP 5-DEEPER PART 2) — the producing Capability
	// is the LIVE SPECIALIST-firing ENTRY this episode: the subconscious dispatch loop routes each base
	// specialist's admission THROUGH the Capability (Capability.AdmitSpecialist — the §3.3a Scope domain
	// band) instead of the bare relevance gate firing every over-θ specialist. The OTHER half of the §3.3
	// subsumption made observable — on the general-Scope episode path the admission set is byte-identical, so
	// only this event distinguishes the Capability-gated path from the bare relevance firing. Emitted ONLY
	// when the opt-in subconscious.capability_specialists is on AND a producing Capability exists (default
	// OFF ⇒ bare eff>theta admission ⇒ no event ⇒ byte-identical goldens). Carries {capability, domain}.
	SubSpecGate Kind = "subconscious.spec_gate"
	// SubSparse (SPARSE-DISPATCH, docs/internal/notes/2026-06-21-attention-mechanisms-litreview.md §4) — the
	// dispatch loop admitted specialists by SPARSEMAX over the relevance field (Martins & Astudillo 2016)
	// instead of the per-key absolute eff>theta gate: the competitive, self-normalizing relative admission
	// made observable. Carries {tau (the induced sparsemax threshold), floor (theta, the surviving floor),
	// support (|admitted by sparsemax|), admitted (|fired after the θ floor|), candidates (|scored|),
	// weights ([{domain, p, eff}] over the scored field — p is the stamped dispatch confidence)}. Emitted
	// ONLY when the opt-in subconscious.dispatch.sparse is on (default OFF ⇒ the bare eff>theta absolute gate
	// ⇒ no event ⇒ byte-identical goldens). Pattern-A pure CONTROL (closed-form simplex projection, NO model).
	SubSparse Kind = "subconscious.sparse"
	// SubSingleStrong (SUB-AGENT GUARD, docs/internal/notes/2026-06-21-sota-benchmark-suite.md §7.6) — the per-tick
	// sub-agent fan-out was COLLAPSED to its single best member (the highest-effective-relevance fired
	// candidate this tick), the "single strong agent" reference arm for the teams-vs-best-member guard
	// ("Multi-Agent Teams Hold Experts Back", arXiv 2602.01011: teams underperform their best member 8-38%
	// via integrative compromise). The harness's full sub-agent dispatch must measurably BEAT this arm or
	// the sub-agent layer is anti-value. Carries {fired (|admitted before the collapse|), kept (the single
	// surviving domain), dropped (|fired - 1|, the team members discarded)}. Emitted ONLY when the opt-in
	// subconscious.single_strong_agent is on AND the collapse actually dropped a teammate (fired>1) — so on
	// the default path (flag OFF) there is no event and the full fan-out is byte-identical. Pattern-A pure
	// CONTROL (closed-form argmax over the fired field, NO model).
	SubSingleStrong Kind = "subconscious.single_strong"
	// SubQueryFormulate (QUERY-FORMULATION, T1.1; FLARE arXiv:2305.06983) — a sub-agent's web_search query
	// was FORMULATED from the actual question (a leading instruction/wrapper clause stripped) rather than
	// searched as the whole goal verbatim — the MEASURED bench fix (a wrapped goal made DuckDuckGo return a
	// benchmark meta-page). Carries {goal (the raw static goal), query (the formulated query that was sent)}.
	// Emitted ONLY when the opt-in subconscious.query_formulation is on AND the formulated query DIFFERS from
	// the trimmed goal (a no-op reformulation is silent) — so on the default path (flag OFF) there is no event
	// and the query is the trimmed goal, byte-identical. Pattern-A pure CONTROL (deterministic string
	// transform, NO model).
	SubQueryFormulate Kind = "subconscious.query_formulate"
	Goal              Kind = "lifecycle.goal" // a first-class goal enters the system (user / drive)
	// hidden seam
	Filter    Kind = "seam.filter"
	Gate      Kind = "seam.gate"
	Transform Kind = "seam.transform"
	Inject    Kind = "seam.inject"
	// Sufficiency (A-RAG1 — the CRAG-style sufficiency gate, behind seam.sufficiency_gate, default OFF ⇒
	// no event ⇒ byte-identical goldens). Emitted at the concretize stage when the gate grades a fuel-
	// needing candidate's sourced fuel sufficient / ambiguous / insufficient: a deterministic coverage*trust
	// FLOOR (control.ScoreSufficiency) decides, the model CEILING (backends.SufficiencyJudge) refines only
	// a flagged-fuzzy case, and an INSUFFICIENT verdict drives the harness to ABSTAIN (drop the candidate
	// rather than over-commit a hollow recall — the structural fix for the abstention paradox). Carries
	// {verdict, coverage, trust, grounded, rung, appraiser, abstained, operator}.
	Sufficiency Kind = "seam.sufficiency"
	// BandColdStart (B1f — the intake band-pass COLD-START fix, behind seam.band_pass_coldstart, default
	// OFF ⇒ no event ⇒ byte-identical goldens). Emitted when the cold-start fix RESCUES a first-appearance
	// step-edge: a signal that appears HIGH and SUSTAINS high — a novel grounded fact the conscious has
	// never seen — injects at the step the legacy seed-to-x[0] cold-start would have suppressed forever
	// (HPF = x − x = 0). Carries {stream, passed, low_pass, highpass, floor, tick}. The observable proof
	// the band-pass HPF now passes a novel step (04-seams §2.1) instead of killing it on appearance.
	BandColdStart Kind = "seam.band_coldstart"
	// CONSCIOUS (wire code: conscious.*)
	Generate Kind = "conscious.generate"
	Append   Kind = "conscious.append"
	MCP      Kind = "conscious.mcp"
	XRef     Kind = "conscious.xref" // a typed cross-reference between branches (CONTRADICTS/SUPERSEDES/SUPPORTS)
	// SeedIntent (C1, 02-conscious.md §1.8) — a standing endogenous DRIVE root was seeded into the forest at
	// awake boot (the self-sustaining frontier: the loop has something to think about before any user input).
	// Emitted ONLY in the awake/continuous loop when conscious.activity.seed_intents is ON (default OFF ⇒ no
	// seed roots ⇒ no event ⇒ byte-identical goldens). Carries {name, faculty, backed_by, kernel, count,
	// branch}. The reactive/episodic loop never emits it.
	SeedIntent Kind = "conscious.seed_intent"
	// Attention (faculty attention scheduler — the fair-share faculty arbiter, THOUGHT/ knob
	// conscious.activity.faculty_scheduler) — the awake loop's "what to think about" selection chose a
	// standing faculty/drive line by LEAST-RECENTLY-FOCUSED fair-share (round-robin is the W=1 degenerate
	// case) instead of pure frontier argmax, so every faculty gets a turn and the perceptual/mnemonic
	// starvation is broken. Emitted ONLY in the awake/continuous loop when conscious.activity
	// .faculty_scheduler is ON (default OFF ⇒ no scheduler ⇒ no event ⇒ byte-identical goldens). Carries
	// {faculty, branch, name, width, last_focus_tick, candidates}. The reactive loop never emits it.
	Attention Kind = "conscious.attention"
	// RPIV (the Validative faculty's standing capability, conscious.activity.rpiv) — the awake loop ran
	// the RPIV (Research -> Plan -> Implement -> Validate) program template when the faculty scheduler
	// focused a VALIDATIVE seed root. One event per phase carries {phase, operator, source, goal, branch}
	// and the VALIDATE event additionally carries the GROUNDED-check verdict {grounded, decision, score,
	// best} (the keep-or-revert outcome — the loop's independent reward signal). Emitted ONLY in the awake
	// loop when conscious.activity.rpiv (+ faculty_scheduler) is ON (default OFF ⇒ no event ⇒ byte-identical).
	RPIV Kind = "conscious.rpiv"
	// Route (the read-only LANE ROUTER, conscious.activity.route_advisor — O-3, the auto-dev read-only
	// router brought inward; docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md §6/§7 P2). The awake loop ran
	// the value-routed ranking over the live standing faculty/drive lanes — per-lane thresholds + cooldowns —
	// and this carries the verdict: {now, runnable, total, next (best runnable lane label or "none"),
	// next_id, top (the would-be pick, ADVISORY only), audit (the deterministic one-line breakdown), lanes
	// (per-lane label/value/runnable/reason)}. It DECIDES but NEVER DISPATCHES — the existing scheduler/argmax
	// still owns focus, so the plant is unchanged. Emitted ONLY in the awake loop when route_advisor is ON
	// (default OFF ⇒ no scan ⇒ no event ⇒ byte-identical). The reactive loop never emits it.
	Route Kind = "conscious.route"
	// InboxEscalate (O-5 — the async inbox push channel + repetition-escalation, dogfooded inward over
	// proactive outreach, 2026-06-20-auto-dev-lathe-vs-fleet.md §4#6/§6) — an UNACKNOWLEDGED proactive
	// outreach was RE-SURFACED with escalating urgency. The base outreach channel (action.respond with
	// kind:outreach) is fire-once-then-dedup; this is the LATHE inbox.jsonl repetition-escalation brought
	// inward: a developed line the user ignored is re-pushed with a louder marker rather than dropped
	// silently. DURABILITY-BOUNDED: at most InboxMaxEscalations re-pushes (default 2), each gated by a
	// strictly-longer cooldown, and cleared on any user response (acknowledgement). Emitted ONLY in the
	// awake loop when conscious.activity.inbox_escalation is ON (which requires ProactiveOutreach) AND a
	// pending unacknowledged outreach exists past the escalation cooldown (default OFF ⇒ no pending tracking
	// ⇒ no event ⇒ byte-identical). Carries {text, escalation, max, value, first_tick, since}.
	InboxEscalate Kind = "conscious.inbox_escalate"
	// Engage (AWAKE-DISP rung 1 — the deterministic engagement VALUE FLOOR, conscious.activity.
	// awake_user_engage, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md). In the AWAKE regime a
	// FOCUSED, UNRESOLVED user line's V(s) carries an additive engagement boost (the tunable weight
	// conscious.activity.awake_user_engage_weight on TOP of the standing pendingUserTerm) so the line
	// RELIABLY OUT-COMPETES the endogenous wander / default-mode lines and WINS the produce-competition
	// (the frontier rerank + pursuit-threshold resume). Pattern-A — a pure deterministic value computation,
	// NO model call. Emitted ONLY in the awake loop when the flag is ON AND the active branch is a focused
	// unresolved user line (default OFF ⇒ no boost, no event, byte-identical). Carries {branch, base, boost,
	// value, weight}.
	Engage Kind = "conscious.engage"
	// EngageJudge (AWAKE-DISP rung 2 — the Pattern-C engagement model CEILING, conscious.activity.
	// awake_user_engage_judge, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md §rung-2). In the AWAKE
	// regime, when the deterministic engagement floor (rung 0's RecognizeShape) cannot decide a FUZZY user line
	// (substantive but not lexically task-shaped), the model judges whether it is worth engaging the subconscious
	// on a full round-trip. Emitted ONLY when the ceiling actually MOVES the floor's no-engage to "engage"
	// (the model lifted the line into a full subconscious engagement); a "quiet" / declined / no-model
	// escalation surfaces escalation.floor_stands instead (the floor stood, never silent). Pattern-C: the
	// deterministic floor always ran first; the model is the optional ceiling consulted only on the flagged-fuzzy
	// band. Default OFF ⇒ no escalation, no event, byte-identical. Carries {branch, goal, floor, verdict}.
	EngageJudge Kind = "conscious.engage_judge"
	// UCBSelect (T1.3 — the UCB exploration branch-selection policy, env THOUGHT_UCB_C). At the BACKTRACK
	// resume site the engine normally focuses the value-greedy Frontier()[0]; when c>0 the UCB policy
	// (value + c*sqrt(ln N / (1+visits)), internal/search/ucb.go) can instead resume a LESS-VISITED line
	// whose exploration bonus lifts it above the higher-value head — drawing the search toward
	// under-explored branches. Pattern-A: a pure deterministic value+visits computation, NO model call.
	// Emitted ONLY when c>0 AND the UCB pick DIFFERS from the value-greedy head (a non-greedy resume);
	// when c=0 the policy is off, no event, byte-identical. Carries {branch, greedy, value, greedy_value,
	// visits, c}.
	UCBSelect Kind = "conscious.ucb_select"
	// watched seam / ACTION (wire code: action.*)
	Intention   Kind = "action.intention"
	Act         Kind = "action.act"
	Observation Kind = "action.observation"
	Respond     Kind = "action.respond" // the outward-facing answer to the user (an Action-layer action)
	Ask         Kind = "action.ask"     // request information from the user -> AWAITING_USER
	// real tool execution (the Action layer's effectors + safety gates)
	ActionTool        Kind = "action.tool"         // a real tool was dispatched (carries ok/exit_code)
	ActionSandboxDeny Kind = "action.sandbox_deny" // a file-modifying call denied by the sandbox
	ActionSafetyBlock Kind = "action.safety_block" // a command blocked by the safety evaluator
	ActionBlocked     Kind = "action.blocked"      // unknown tool / blocked at execution level / approval denied
	// tiered AUTO-PERMISSION (action.auto_permission) — the per-call SAFE/DANGEROUS classification that
	// removes the human from the per-call approval loop while the sandbox + gates confine it.
	ActionAutoApprove Kind = "action.auto_approve" // a SAFE-tier call (read-only / in-jail / allowlisted) self-authorized, no human prompt
	ActionEscalate    Kind = "action.escalate"     // a DANGEROUS-tier call (irreversible / out-of-jail / non-allowlisted) denied + escalated for review
	// critic
	Decision        Kind = "critic.decision"
	Exhaustion      Kind = "critic.exhaustion"
	Interrupt       Kind = "critic.interrupt"
	ResourceTrigger Kind = "critic.resource_trigger" // V(s)-triggered active re-sourcing (A-RAG4): low-V goal-relevant node -> re-invoke the sourcing ladder
	AnswerVerify    Kind = "critic.answer_verify"    // T2.1 INDEPENDENT answer-verifier: re-retrieved web evidence supports / refutes a committed answer (the same-model-ceiling break)
	// backend (LLM)
	LLM         Kind = "llm.call"
	LLMFallback Kind = "llm.fallback"
	// value / regulator / convert
	Value     Kind = "value.update"
	Regulator Kind = "regulator.update"
	Stability Kind = "regulator.stability"
	Schedule  Kind = "regulator.schedule" // the LLM-call scheduler deferred a background call (budget spent)
	Convert   Kind = "convert.mint"
	// PathMint (the representation-space rebuild, M5) — convertibility recognised a hot, GROUNDED directed
	// traversal (analogy/induction/deduction): a named path that keeps closing on a grounded definition-of-
	// done is PAVED so the directed walk that keeps getting taken becomes automatic. A model-only-DoD path
	// is recorded but never paved (recombination can be confidently wrong, spec §12). Carries {kind, path,
	// count, grounded, value} on a pave / {kind:demote, path, value, floor} on a keep-or-revert reversion.
	PathMint Kind = "convert.path_mint"
	// RegistryRefine (the uniform per-registry self-improvement loop, 01-subconscious.md §3.17 + §3.20)
	// — the generalisation of the eval mint gate into a standing per-registry REFINE pass. At idle
	// consolidation the eval.RefineLoop measures every entry of a registry against its measuring-stick
	// reference (absolute "does it still belong?") AND comparatively vs the entry's own past measurements
	// (instance-eval), and surfaces the per-entry improve / keep / prune SIGNAL. It is SIGNAL-ONLY (it
	// never mutates a registry on the default path), behind convert.refine_loop (opt-in, default OFF).
	// Carries {kind:"registry_refine", registry, stick, improve, keep, prune, prunable[]} on the summary
	// event and {kind:"refine_entry", id, pass, verdict, refine, delta} per flagged entry.
	RegistryRefine Kind = "convert.refine"
	// CostGate (W5 — gate registry growth on the COST/efficiency ruler, at the RUNTIME trace->skill mint).
	// Convertibility accumulates the COMPLETION tokens spent re-synthesising a recurring program shape
	// (NoteSynthesisCost, summed from the synthesize_program llm.call stream). When the cost gate is on
	// (convert.cost_gate, opt-in default OFF) the trace->skill mint additionally requires that accumulated
	// cost to clear a floor: the harness only AUTOMATES a shape worth automating (one that has demonstrably
	// cost real decode to re-derive), declining to mint a cheap shape even when it recurs. SIGNAL on the bus,
	// not a silent gate: {kind:"admit", goal, cost, floor, count} when the cost cleared and the mint proceeds;
	// {kind:"hold", goal, cost, floor, count} when the recurring shape is too cheap to be worth a skill and
	// the mint is deferred. Default OFF ⇒ no cost consultation, no event ⇒ byte-identical (the count×value
	// heuristic alone decides, today's behaviour).
	CostGate Kind = "convert.cost_gate"
	// grounding (SR-4 anti-hallucination spine) — the experiment/validation loop made observable.
	// A real (non-fabricated) observation or a deterministic computation grounds/refutes a claim; a
	// fabricated tier-0 "observation" is rejected upstream and never reaches here.
	Ground  Kind = "grounding.ground"  // a claim was grounded/refuted (carries verdict/tier/status/reused)
	Percept Kind = "grounding.percept" // a sensor percept re-grounded a standing claim (no ACT, continuous)
	// session (the runtime, P3.3+) — the bounded worker spawn tree made observable. A synthesised
	// workflow opens a root Session; each phase dispatches a child (bounded depth + budget); a parallel
	// phase's results merge (reduce/vote). Fires only when a multi-phase program is synthesised.
	SessionSpawn     Kind = "session.spawn"     // a root session opened for a synthesised workflow
	SessionDispatch  Kind = "session.dispatch"  // a child session dispatched for a phase/operator (budgeted)
	SessionMerge     Kind = "session.merge"     // a parallel phase's results merged (reduce/vote)
	SessionTerminate Kind = "session.terminate" // the session tree terminated (carries the spend + reason)
	// memory (the declarative stack, P2.3/P6.x) — episodic + semantic (bi-temporal) memory made
	// observable. Never-fabricate: only a GROUNDED episode/belief is recorded; recall is relevance-gated.
	MemoryRecord  Kind = "memory.record"  // a grounded episode was recorded at episode-end
	MemoryRecall  Kind = "memory.recall"  // related past episode(s)/belief(s) recalled for a fresh goal
	MemoryReflect Kind = "memory.reflect" // an idle-tick reflection distilled an episode into a belief
	// MemoryCompact (D5, THOUGHT_WORKING_WINDOW) — the working-context sliding window fired: the goal + the
	// most-recent-N active-line thoughts stay full, older same-line thoughts are lossy-gisted (read-time
	// view, never a graph mutation — detail is restored on refocus). Emitted ONLY when the window is ON and
	// it actually gisted at least one thought, so the default-OFF run is silent and goldens hold.
	MemoryCompact Kind = "memory.compact" // the working-context window gisted older thoughts -> {window, total, gisted}
	// retrieval (the shared hybrid primitive, P1.x) — lexical (Jaccard) + semantic (cosine) fused by RRF.
	// Emitted when a recall runs, carrying the fused breakdown + whether the semantic side was reachable.
	Retrieval Kind = "retrieval.fused" // a hybrid retrieval ran (lexical/semantic/fused scores + mode)
	// RetrievalSemantic (A-RAG2, subconscious.semantic_recall) — the embeddings SIDECAR was probed at
	// construction and the DENSE half of the shared hybrid retriever either lit up (mode=hybrid + dims +
	// model) or fell back (mode=lexical + the probe reason). Emitted ONCE at engine build, and ONLY when
	// the semantic_recall knob is ON, so the default-OFF run is silent and the goldens (test double, no
	// probe) hold. This makes the otherwise-invisible "is the dense channel live?" decision observable.
	RetrievalSemantic Kind = "retrieval.semantic" // the embeddings sidecar probe lit up / fell back the dense channel
	// Context Assembly (SR-2, P4.2/P4.3) — the seam as a view-producer: a consumer's context is selected
	// + ordered + budget-truncated through one of 5 templates. Emitted when a consumer's view is assembled.
	Assemble Kind = "seam.assemble" // a context view was assembled for a consumer (template + sizes + budget)
	// system-wide config (the representation-space rebuild, M1) — the unified HarnessConfig made
	// observable. A non-default config is never silent: Load reports which toggles are OFF, a live flip
	// announces itself, and a disabled component short-circuits to pass-through carrying its reason.
	ConfigLoad   Kind = "config.load"   // a config was loaded/merged (carries the count + paths of OFF toggles)
	ConfigToggle Kind = "config.toggle" // a toggle was flipped (CLI/env/TUI live) -> {path, on}
	ConfigSkip   Kind = "config.skip"   // a disabled component bypassed its decision -> {component, reason}
	// knowledge registry + sourcing/concretization (the representation-space rebuild, M3) — the durable
	// domain-knowledge layer + the ordered fuel ladder + the concretization step made observable. The
	// sourcing ladder (present→knowledge→memory→reality→generated) routes provenance into the EXISTING
	// Filter trust; a `generated`-rung fuel is the LOW-trust floor (0.42) the membrane already distrusts.
	KnowledgeRecord     Kind = "knowledge.record"     // a grounded item entered the registry -> {kind, source, entities, grounded, trust}
	KnowledgeRecall     Kind = "knowledge.recall"     // the registry surfaced items -> {query, kind, hits, top}
	KnowledgeInvalidate Kind = "knowledge.invalidate" // a refuted item was invalidated -> {statement, count, now_tick}
	// KnowledgePromote (A-RAG5, convertibility-on-facts) — a repeatedly-recalled, high-value knowledge
	// fact was CONSOLIDATED into a durable PRIOR: its trust is migrated up toward the neocortical-prior
	// tier (CLS hippocampus→neocortex). The HOT end of the HOT/WARM/COLD tiering, justified by recall ×
	// value, not age. Emitted by convertibility's fact-consolidation pass at idle (behind convert.facts).
	KnowledgePromote Kind = "knowledge.promote"   // a fact consolidated into a prior -> {statement, recalls, value, from_trust, to_trust}
	SubSource        Kind = "subconscious.source" // the sourcing ladder resolved a need -> {rung, provider, trust, grounded, query}
	SubConcretize    Kind = "subconscious.concretize"
	// GraphWriteBack (A-RAG3, subconscious.graph_recall) — a rung-4 reality fact written BACK into the
	// unified cognition graph as a `fact` node + a `grounds` edge from the line that imported it (the
	// Zep/Graphiti pattern on the existing event-sourced substrate; NO separate vector store). The
	// CognitionGraph folds this event into the node/edge so the fact is later reachable by graph-native
	// multi-hop recall (GraphRAG Local search) when a need's lexical stores miss. Default OFF
	// (subconscious.graph_recall) ⇒ never emitted ⇒ no fact node ⇒ byte-identical.
	GraphWriteBack Kind = "subconscious.graph_writeback" // {statement, node, line, tool, trust, entities}
	// cross-session persistence + the lifecycle/cleanup curator (the representation-space rebuild, M4) —
	// learned artifacts (skills/operators/specialists, gate priors, episodes/beliefs, knowledge) survive
	// a restart, and the durable stores are CURATED (versioned/deduped/decayed/demoted/GC'd/capped) at
	// IDLE consolidation rather than growing unbounded. Persistence is INJECTED (the Store does I/O in
	// its own package, nil-safe — tests/heuristic never touch disk) and the Curator is PURE over records
	// + the seeded tick; never-fabricate holds on every Save (an ungrounded record is rejected).
	PersistLoad   Kind = "persist.load"   // a snapshot was loaded at start -> {skills, operators, specialists, priors, episodes, beliefs, knowledge, dir}
	PersistSave   Kind = "persist.save"   // a learned artifact was persisted -> {artifact, id, version}
	PersistCurate Kind = "persist.curate" // the curator acted on a record -> {action, artifact, id, reason}

	// KeyframeClose (Track F, F-M7 — loop-closure / recurrence keyframe DB, "the HINGE") — the engine
	// re-entered a thought-line it has explored before: a LOOP CLOSURE / anti-rumination signal. The
	// recurrence index is persistent + bi-temporal + substrate-tagged, so the loop-back point can lie in
	// a PRIOR run (cross_run=true) — the cross-session recognition the un-persisted DB blocked (gap G3).
	// Pure CONTROL (a deterministic content fingerprint; no model call). Only fires when persistence.
	// keyframe_db is ON, so the default path is silent + byte-identical.
	KeyframeClose Kind = "keyframe.close" // a known thought-line was re-entered -> {descriptor, gist, count, closures, first_seen, gap, cross_run}

	// Registry Ledger (W1) — named, substrate-tagged snapshots of the entire learned state.
	// Every snapshot/reset/diff is observable; the SELF·EVOLUTION panel renders the ledger.
	RegistrySnapshot Kind = "registry.snapshot" // a named snapshot was saved -> {action, name, substrate}
	RegistryReset    Kind = "registry.reset"    // live state was reverted to a snapshot -> {action, name, substrate}
	RegistryDiff     Kind = "registry.diff"     // two named snapshots were diffed -> {from, to, added, removed, changed}
	RegistryBatch    Kind = "registry.batch"    // a batch delta / ledger entry was applied -> {action, scope, safety_mode}

	// SelfBench loop-close (H-SB2, 2026-06-20-benchmark-taxonomy.md §7.2/§7.6 #4) — the harness OWNS its own
	// fitness function: when a consolidation records a batch of self-changes (mints), the engine measures a
	// SelfBench fitness DELTA of the just-minted batch versus the pre-mint baseline floor, RE-PASSES the
	// durability gate (regulator.StabilityRegime — the mod changed the plant), and issues a keep-or-revert
	// VERDICT against the auto:baseline revert point. SelfBenchVerdict carries the full measurement; Promote /
	// Revert are the action half. DEFAULT = propose-and-gate (measure + PROPOSE, never self-commit a revert);
	// CLOSED-LOOP (the interlock flag) self-reverts via ResetToSnapshot on a net-negative or durability fail.
	SelfBenchVerdict Kind = "selfbench.verdict" // a benched batch was judged -> {minted, delta, fitness, floor, durable, regime, verdict, mode, revert}
	SelfBenchPromote Kind = "selfbench.promote" // the batch cleared the floor AND held durability -> {minted, delta, fitness, regime, mode}
	SelfBenchRevert  Kind = "selfbench.revert"  // the batch FAILED (net-negative or durability fail) -> {reason, delta, regime, revert, committed}
	// hybrid-cognition escalation (the three-pattern refactor, M2) — a Pattern-C escalator's
	// deterministic FLOOR stood as the decision because the model was not consulted or declined:
	// the case was not flagged-fuzzy, no model is wired, the model declined/parse-failed, or it was
	// a STRUCTURAL fact the model may not override. A non-escalation is never silent (Rule 4). Only
	// emitted on escalation-ELIGIBLE cases (mode != control AND flagged-fuzzy, or the structural-skip
	// in those modes), so the default control mode stays silent and goldens hold. Carries
	// {site, decision, floor_decision, ambiguity, reason, model_consulted}.
	EscalationFloorStands Kind = "escalation.floor_stands"
	// model-driven tool-call selection (the grounding-chain fix, THOUGHT_MODEL_SELECT) — a Pattern-C
	// escalation at the SUB-AGENT's effectful tool pick: the deterministic regex FLOOR (action.SelectTool
	// over the STATIC episode goal) yields nothing usable for a grounding-shaped step (the next-hop target
	// lives in the PRIOR observations / current thought, not the goal), so the model CEILING
	// (RealityComprehender.Comprehend over the live context) picks the call — esp. read_file{path} on the
	// path it reasoned to. The model NEVER overrides a structural move; it only PICKS the call when the
	// floor is empty/fuzzy, and the pick still runs through executor.Execute (all gates fire). Emitted ONLY
	// when THOUGHT_MODEL_SELECT is ON AND the escalation actually fires (a real model is a
	// RealityComprehender; the test double is NOT, so the default offline path never reaches here). Carries
	// {floor, floor_tool, model_tool, model_target, escalated}.
	EscalationToolSelect Kind = "escalation.tool_select"
	// force-a-grounding-read-before-give-up (the early-give-up fix, THOUGHT_FORCE_GROUND) — a Pattern-C
	// override at the engine give-up site: a grounding-shaped goal (one that needs reality import) may NOT
	// quiesce/STOP on the PRE-grounding give-up paths until at least one grounding read has been ISSUED.
	// When the Controller's floor decides STOP/GIVE_UP but no read has crossed the seam yet, the engine
	// downgrades the decision to ACT (import reality) instead of giving up from priors. It NEVER blocks a
	// legitimate fact-based STOP (a goal already grounded/answered, or a non-grounding goal). Emitted ONLY
	// when THOUGHT_FORCE_GROUND is ON AND the override actually fires. Carries
	// {floor_decision, forced, reads_issued, grounding_shaped}.
	EscalationForceGround Kind = "escalation.force_ground"
	// legible generation (the SHADOW instrument, WF-E CC-1) — the conscious's in-band control tag
	// (legible.PromptFragment) read by the seam as a SHADOW prediction and compared against the REAL
	// control-floor decision, WITHOUT changing routing (open/advisory, NO cost claim — 05 §4). Emitted
	// ONLY when seam.legible_generation is ON (it DEFAULTS OFF even in AllOn(), an explicit opt-in
	// exception), so the default run is silent and every golden stays byte-identical.
	LegibleTag    Kind = "legible.tag"    // a tag was parsed at a seam point -> {site, op, domain, conf, act, known, novel}
	LegibleParity Kind = "legible.parity" // shadow-route vs actual decision -> {site, shadow, actual, agree}
	LegibleNovel  Kind = "legible.novel"  // a novel:<desc> sighting (the ranked scaling gap signal) -> {desc, op}
	// ports & lifecycle
	Port      Kind = "port"
	Lifecycle Kind = "lifecycle.transition"
	Episode   Kind = "lifecycle.episode" // a new cognitive process begins (carries the process id)
	// Deadline (WF-G time-awareness, 09 §4): the episode's wall-clock deadline expired -> the engine
	// forces STOP (answer best-so-far). Emitted ONLY when a Clock + per-episode deadline are wired
	// (the default engine is time-blind: nil clock, no time reads, byte-identical behavior). Carries
	// {deadline_ms, elapsed_ms}.
	Deadline Kind = "lifecycle.deadline"
	Arousal  Kind = "arousal.transition"
	Tick     Kind = "tick"
	// ContinuousDecision (the continuous-autonomy frozen-snapshot probe, measuring-stick-spec §3.4) —
	// the awake regime's single next-decision class, classified deterministically from a frozen engine
	// state (frontier/arousal/lull/cooldown/floor) by the continuous-mode policy. It is the bench
	// Tier-A forced-choice answer AND the isolation witness (a harness pass that never emitted this
	// never ran the awake decision spine). Carries {decision, endogenous}. The episodic loop never
	// emits it; only the frozen-probe path (and a future live awake tick) does.
	ContinuousDecision Kind = "continuous.decision"
	// Deliberation (the ROBUSTNESS lever — cross-sample outcome redundancy, THOUGHT_DELIBERATIVE_K) —
	// the self-consistency reconciliation over K INDEPENDENT trajectories. The harness has no other
	// cross-sample outcome redundancy (one trajectory / one episode / one verdict), so run-level
	// variance (σ_R) is unattacked; running K independent deterministically-seeded episodes and
	// reconciling their final answers by majority vote (V(s) tie-break) concentrates the outcome. The
	// decision is made over the K candidate answers + their V(s) appraisals — the SAME value.Rank /
	// GATE-Select machinery, never a new scorer. Emitted ONLY when THOUGHT_DELIBERATIVE_K > 1 (K==1 is
	// byte-identical: no extra episode, no event). Carries {k, candidates, tally, winner, why, tie}.
	Deliberation Kind = "conscious.deliberation"
	// GroundComplete (the grounding-completeness reading directive, THOUGHT_GROUND_COMPLETE) — the RESPOND
	// prompt carried a GENERAL directive asking the model, before answering, to use the value actually IN
	// FORCE (a later in-material correction/replacement/override beats the first name-match) and to apply
	// an in-material adjustment/conversion to the base value, while preserving the never-fabricate decline.
	// Pattern B: the directive shapes how the model READS — it never hardcodes an answer. Emitted ONLY when
	// the flag is ON AND a respond is about to fire with the directive engaged (flag OFF ⇒ no fragment ⇒
	// byte-identical RESPOND prompt ⇒ no event). Carries {site, flag}.
	GroundComplete Kind = "conscious.ground_complete"
	// RoutingTier (the cost-aware substrate TIER router, subconscious.tier_router — RouteLLM-class,
	// docs/internal/notes/2026-06-20-rl-ml-scheduler-scaling-research.md §4/§5 Scenario C) — a Pattern-C tier
	// route: a CONTENT call's substrate tier (utility/haiku vs primary/sonnet) was chosen by the
	// deterministic per-role FLOOR + an optional learned CEILING. The floor (the existing hardcoded
	// role->tier split) always decides; on a flagged-fuzzy call the policy may ESCALATE a hard call up
	// or DOWNGRADE an easy call to the cheaper tier — never overriding a structurally-pinned role
	// (respond/decide stay primary), and a non-route (the floor stood) is surfaced, never silent
	// (Rule 4). The route changes WHICH model answers, NOT the branching plant, so it does not touch the
	// durability conditions. Emitted ONLY when subconscious.tier_router is ON (default OFF ⇒ the floor is
	// the silent decision, byte-identical to the pre-router tiered backend ⇒ no event ⇒ goldens hold).
	// Carries {role, tier, floor_tier, reason, flagged, value, prompt_len, policy, confidence}.
	RoutingTier Kind = "routing.tier"
	// PerceptionClock (the read_clock sensor — cognitive power-cycle, Track 1.5, proposal
	// 2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §3.2 + §11 Track 1.5) — the engine SENSED
	// the wall clock at episode-open across the injected Clock seam. The sensed value rides the replayable
	// PERCEPT-LOG: in RECORD mode the live e.clk.Now() is read + appended; in REPLAY mode (a loaded,
	// version/substrate-matching log) the LOGGED value for the tick is returned (so a golden replay is
	// deterministic even though the live clock differs). Emitted ONLY when the opt-in sense.clock knob is
	// ON AND a Clock is wired (default OFF / nil clock ⇒ no read ⇒ no event ⇒ byte-identical, time-blind).
	// Carries {value, mode (record|replay), tick}.
	PerceptionClock Kind = "perception.clock"
	// PerceptionOrient (the ORIENTATION PASS — cognitive power-cycle, Track 3, proposal
	// 2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §5 + §11 Track 3) — on the FIRST wake of a
	// resumed session the engine re-grounds BOTH layers: it injects one re-grounding GENERATED thought
	// ("Resuming. Prior focus: <gist>. Current time: <clock>. <self-state>.") into the conscious stream
	// AND writes the sensed date as a grounded BELIEF via the semantic memory (the perception->memory
	// handshake). Fires ONCE per engine when the opt-in sense.orient knob is ON AND this is a resume boot
	// (a rehydrated prior spine OR clock-sensing is enabled). Default OFF ⇒ no orientation thought, no
	// belief, no event ⇒ byte-identical. Carries {tick, gist, clock, self, belief, resume}.
	PerceptionOrient Kind = "perception.orient"
	// PerceptionWeb (the fetch_web sensor — cognitive power-cycle, follow-up #15, the OUTWARD half of
	// grounded sensing complementing the INWARD read_clock/read_host/read_event_log) — the engine SENSED
	// the world (a one-line web/news snippet) at episode-open across the injected Web seam (web.Wall at the
	// edge, web.Fake in tests). Like the clock, the sensed SNIPPET rides the replayable PERCEPT-LOG: RECORD
	// mode does the live e.web.Fetch + appends the snippet; REPLAY mode (a version/substrate-matching loaded
	// log) returns the LOGGED snippet for the tick (so a golden replay is deterministic even though a live
	// fetch would differ). BUDGETED (resolved Fork 2): at most one fetch per episode-open, never per tick. A
	// blind/failed read (Result.OK=false) still emits (the sense fired; it had nothing to voice). Emitted
	// ONLY when the opt-in sense.web knob is ON AND a Web is wired (default OFF / nil web ⇒ no fetch ⇒ no
	// event ⇒ byte-identical, web-blind). Carries {value, ok, source, mode (record|replay), tick}.
	PerceptionWeb Kind = "perception.web"
	// PerceptionSense (#19 — AUTONOMOUS standing-intent sensing, cognitive power-cycle) — the engine SENSED
	// ON ITS OWN because a standing PERCEPTUAL or INTROSPECTIVE seed root held focus this tick (the live-wire
	// of the seed root's BackedBy, which was dead-as-trigger before). This is distinct from PerceptionClock
	// (the per-tick boundary clock log), PerceptionOrient (the once-per-resume re-grounding pass), and
	// PerceptionWeb (the budgeted episode-open fetch): it is a SELF-INITIATED sense driven by which faculty
	// holds focus, with NO user prompt. A single BOUNDED read per focus (a per-(branch,tick) guard) — no
	// fan-out, no new operator/sub-agent — so it does not raise the branching plant (n). The sensed percept
	// is injected as a GENERATED thought and witnessed here. Emitted ONLY in the awake loop when the opt-in
	// conscious.activity.autonomous_sense knob is ON AND a perceptual/introspective seed root is focused
	// (default OFF ⇒ no autonomous sense ⇒ no event ⇒ byte-identical). Carries {faculty, branch, name, kind
	// (clock|self), value, tick}.
	PerceptionSense Kind = "perception.sense"
	// PerceptionSelfModel (SELF-MODEL — the baseline DECLARATIVE self-model, preagi-levels-roadmap §1.5) —
	// the engine GROUNDED a STANDING CORE self-description into the awake conscious stream because a standing
	// INTROSPECTIVE seed root held focus: its IDENTITY (Silent-Injection harness, 3 layers / 2 seams) + a
	// bounded, CONSTANT-SIZE CAPABILITY INDEX read from the REAL registries (tool categories+counts, a
	// thought graph, N specialists across M domains, K operators across F families) + RUNTIME facts (mode /
	// substrate / cwd / a key config summary). DISTINCT from PerceptionSense (#19 — the per-focus sensor
	// read of the live world/own-state): this is a DECLARATIVE WHAT-IT-IS the conscious stream re-grounds
	// on, refreshed only on a content-HASH change (standing, not per-tick, not resume-once). The per-
	// capability DETAIL is NOT carried — it is pulled LAZILY on demand (SelfModelLookup) because the roster
	// is bounded-but-GROWING (minting). The core is a single GENERATED percept APPEND (a μ-baseline
	// immigrant, NOT a fork — n unchanged), so it does not raise the branching plant. Emitted ONLY in the
	// awake loop when the opt-in sense.self_model knob is ON AND the introspective root is focused (default
	// OFF ⇒ no self-model thought ⇒ no event ⇒ byte-identical). Carries {tick, branch, hash, specialists,
	// domains, tools, operators, mode, substrate, cwd}.
	PerceptionSelfModel Kind = "perception.self_model"
	// PerceptionSelfModelReply (SELF-MODEL -> the REPLY — the live-gap fix, board SELF-MODEL) — the engine
	// FOLDED the grounded standing-core self-model into the RESPOND context for THIS reply because the user's
	// question was self-directed (an IDENTITY / SELF / CAPABILITY / LOCATION ask — "what are you / what can you
	// do / where are you running"). DISTINCT from PerceptionSelfModel (the standing grounding INTO the stream
	// on a focused introspective root): this is the DELIVER-time fold into the answer context so the model
	// answers FROM the harness self-knowledge (identity / architecture / real tools / cwd) instead of the bare-
	// model "I'm an LLM" prior. RELEVANCE-GATED (only a self-directed question — a normal answer is never
	// bloated with a self-description) and a read-only context append (no graph mutation, no fork — n
	// unchanged; the μ-baseline self-model made legible in the reply). Emitted ONLY when the opt-in
	// sense.self_model knob is ON AND the relevance gate fires (default OFF ⇒ context unchanged ⇒ no event ⇒
	// byte-identical RESPOND prompt). Carries {goal, specialists, operators, tools, mode, substrate, cwd}.
	PerceptionSelfModelReply Kind = "perception.self_model_reply"
	// ConformanceWiring (the L0 conformance WIRING SCAN — Track H, benchmark-taxonomy §1 L0) — the
	// per-engine witness that the named cognitive subsystems actually FIRED on the live loop during a run
	// (the "tests pass != feature runs" gate, made observable). When the opt-in conformance.self_check knob
	// is ON the engine attaches a PASSIVE coverage tap to its own bus that records the SET of layers it
	// emitted; EmitWiringScan renders that set as one event. A run that compiled but never exercised a
	// layer (a dead-wired subsystem) shows the layer MISSING here — the scan FAILS, exactly the wiring-gate
	// lesson. Emitted ONLY when conformance.self_check is ON (default OFF ⇒ no tap, no event ⇒
	// byte-identical). Carries {covered (sorted layers), missing (required-but-absent), events, ok}.
	ConformanceWiring Kind = "conformance.wiring"
	// ConformanceRollup (the L0 conformance ROLLUP verdict — Track H, benchmark-taxonomy §5 build-order #1)
	// — the SINGLE PASS/FAIL the rollup emits after running S1..S16 + the requirement checklist + the
	// wiring scan. It is the "does it even run as a harness" gate (deterministic, offline, no model). The
	// conformance rollup (internal/conformance) emits it on the rollup's own bus, NOT on a cognition tick —
	// the episodic/awake live loop never emits it; only the rollup front-door does. Carries {pass,
	// scenarios, checks_passed, checks_total, wiring_ok, failures}.
	ConformanceRollup Kind = "conformance.rollup"
	// dev-side PLAN-GATE (Track O / O-2, the LATHE plan-carries-a-falsifiable-gate port,
	// docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md §4 win #2 + §7 P1) — the harness dogfooding its
	// OWN event bus on the DEV side (§6 differentiator). A build plan declares a falsifiable CONTRACT
	// (producers_files: file -> symbols the change MUST land + acceptance_checks: a regex the diff MUST
	// match); before a KEEP, the gate mechanically audits the ACTUAL diff against the contract. It is
	// the structural antidote to "tests pass != feature runs / declared-not-landed": a keep is REFUSED
	// when a declared symbol is absent from the diff or an acceptance regex does not match. Pure
	// CONTROL/plumbing (a deterministic symbol-audit over the diff, NO model call). Emitted ONLY when
	// the opt-in dev.plan_gate knob is ON (default OFF => no audit, no event => byte-identical; the gate
	// runs at the dev-side keep step, NOT the cognition tick). Carries:
	//   plangate.audit   -> {kind (producer|check), file, symbol|pattern, found, reason}
	//   plangate.verdict -> {pass, producers, checks, missing, plan}
	PlanGateAudit   Kind = "plangate.audit"
	PlanGateVerdict Kind = "plangate.verdict"
	// SLAM self-state estimator (Track F / M1, docs/internal/notes/2026-06-20-slam-M1-build-spec.md §5) — the
	// explicit innovation/residual on the action->reality path, made observable. The estimator turns the
	// implicit, model-mediated "reality refuted the guess -> static -0.45 penalty" into a scalar Kalman
	// measurement update with an FEJ-anchored trust rule: belief variance shrinks ONLY on a grounded
	// observation, never on self-restatement (the §0 invariant). Pure CONTROL (control.Innovate, NO
	// model). Emitted ONLY when the opt-in slam.innovation knob is ON (default OFF ⇒ the estimator is
	// inert ⇒ no event ⇒ byte-identical). Carries:
	//   estimate.innovate -> {id, priorMean, priorVar, obs, obsPrec, innov, innovVar, gain} (the residual)
	//   estimate.correct  -> {id, postMean, postVar, deltaFromStatic} (graded correction; delta from -0.45)
	//   estimate.gate     -> {id, mahalanobis, chi2Gate, gated} (data-association reject; obs not folded in)
	// estimate.calibrate is the SLAM M9 CALIBRATION meta-estimator (Track F / G9): it LEARNS each
	// source's reliability per trust tier from the predicted-vs-actual outcome stream (the M1 residual)
	// and RE-ESTIMATES the measurement precision R the innovation update uses — the direct lever on the
	// measured same-model self-judging ceiling (the system DISCOVERS its confident self-predictions are
	// overconfident and DOWN-WEIGHTS them, instead of trusting a fixed prior forever). Pure CONTROL
	// (consumes the control.Residual M1 produced; NO model). Emitted ONLY when the opt-in slam.calibration
	// knob is ON (which itself requires slam.innovation for the residual stream) — default OFF => the
	// calibrator is inert => no event => byte-identical. Carries:
	//   estimate.calibrate -> {tier, samples, hits, hitRate, reliability, priorPrec, learnedPrec, overconfidence, measured}
	// estimate.consistency is the SLAM M5 CONSISTENCY / OBSERVABILITY INVARIANT witness (Track F / M5,
	// design §4 P2/P3 + §5 #7 + §5b): the failable check that the estimator gains NO spurious information
	// in unobservable directions (the Huang-2010 EKF-inconsistency overconfidence). It accounts every
	// variance reduction as grounded (from an associated Observe()) vs spurious (a self-restatement or a
	// gated obs that lowered P) and reports consistent iff spuriousGain==0 — the awake-durability gate
	// requirement alongside the five control-theoretic conditions. Pure CONTROL (closed-form information
	// accounting; NO model). Emitted ONLY when the opt-in slam.consistency knob is ON (which requires
	// slam.innovation for the variance trajectory) — default OFF => no accounting => no event =>
	// byte-identical. Carries:
	//   estimate.consistency -> {groundedGain, spuriousGain, groundedFraction, notes, observations, gated, violations, consistent}
	// estimate.correlate is the SLAM M2 SPARSE-COVARIANCE / Information-layer witness (Track F / M2,
	// design §3b.3 #2 + §1 non-factorization + §6 M2): when reality REFUTES a belief, every belief that
	// CO-VARIES with it (shares a grounding upstream) loses certainty — its variance is INFLATED, because
	// the shared grounding that backed it just proved unreliable. This is how the estimator catches
	// CORRELATED self-deception (two beliefs confidently wrong because one bad upstream) that no per-belief
	// scalar can see — "the correlations ARE the information" (Thm 2). The correlation graph stays SPARSE
	// (only beliefs sharing an upstream get an edge); a propagation only RAISES variance, so it stays
	// inside the §0/M5 consistency invariant (becoming less certain is never spurious information). Pure
	// CONTROL (control.CorrelatedInflation/CorrelationCoefficient; NO model). Emitted ONLY when the opt-in
	// slam.covariance knob is ON (which requires slam.innovation for the variance trajectory) — default
	// OFF => no correlation graph => no event => byte-identical. Carries:
	//   estimate.correlate -> {sibling, refuted, shared, rho, innovMag, priorVar, postVar, varInflate}
	// estimate.infogain is the SLAM M6 ACTIVE-INFERENCE next-best-observation witness (Track F / M6, design
	// §3b.3 #7 + §5 #4 + §6 M6): the directed-grounding signal that ranks the live tracked beliefs by
	// expected JOINT information gain and surfaces the one whose grounding reduces the most uncertainty —
	// "what should the harness verify NEXT" (active-SLAM next-best-view), weighting a belief's own variance
	// (M1: uncertainty to remove) AND its correlation reach (M2: leverage across co-varying siblings). It is
	// the principled explore/exploit term the awake default-mode generator's curiosity drives on, targeting
	// the measured under-grounding / give-up behaviour. PURE RANKING (control.ExpectedInfoGain; NO model) —
	// it reads the variance trajectory and NEVER alters it, so it cannot fabricate certainty (it only
	// DIRECTS the grounding that legitimately shrinks a variance). Emitted ONLY when the opt-in slam.infogain
	// knob is ON (which requires slam.innovation for the variance trajectory) — default OFF => no ranking =>
	// no event => byte-identical. Carries:
	//   estimate.infogain -> {id, priorVar, reach, gain, obsPrec, candidates}
	EstimateInnovate    Kind = "estimate.innovate"
	EstimateCorrect     Kind = "estimate.correct"
	EstimateGate        Kind = "estimate.gate"
	EstimateCalibrate   Kind = "estimate.calibrate"
	EstimateConsistency Kind = "estimate.consistency"
	EstimateCorrelate   Kind = "estimate.correlate"
	EstimateInfoGain    Kind = "estimate.infogain"
	// estimate.decay (Track F / M4) — the SLAM FRESHNESS / STALENESS-DECAY witness: a once-grounded belief
	// left un-refreshed had its variance GROWN toward the prior ceiling as a function of its un-refreshed
	// AGE (the dynamic-map process noise Q>0, P4 — "the world it described may have moved, re-observe").
	// Pure CONTROL (control.StalenessInflation, NO model); decay only RAISES variance so it stays inside the
	// §0/M5 consistency invariant. Behind the opt-in slam.staleness knob (requires slam.innovation), default
	// OFF => no decay sweep => no event => byte-identical. Carries:
	//   estimate.decay -> {id, age, q, priorVar, postVar, varInflate, ceiling}
	EstimateDecay Kind = "estimate.decay"
	// SELF-BENCHMARK loop (Track H, SB0 — benchmark-taxonomy §7). The harness owns its own fitness
	// function: at IDLE consolidation (when selfbench.enabled is ON) it runs a fixed conformance SUITE
	// against a SHADOW engine loaded from a FROZEN checkpoint of the just-consolidated learned state
	// (never the live, mutating engine — §7.2), and emits this bench.* family so the loop is visible in
	// the TUI + headless trace + ledger. The default is PROPOSE-AND-GATE (§7.5): the engine MEASURES +
	// proposes; it does NOT self-commit off its own measurement (no promote/revert here — that is the
	// later SB2 loop-closing slice, separately gated). Emitted ONLY when selfbench.enabled is ON AND the
	// run actually self-benchmarks (default OFF ⇒ no shadow engine ⇒ no event ⇒ byte-identical goldens).
	BenchStart   Kind = "bench.start"   // a self-bench began on a frozen checkpoint -> {checkpoint, suite, probes, shadow}
	BenchCell    Kind = "bench.cell"    // one probe scored on the shadow engine -> {probe, pass, value, answer, reason}
	BenchVerdict Kind = "bench.verdict" // the suite rolled up -> {checkpoint, suite, passed, total, score, verdict}
	BenchReport  Kind = "bench.report"  // the propose-and-gate disposition -> {checkpoint, score, disposition, committed}
	// IntrospectFaithfulness (Track H, benchmark-taxonomy §8 — the introspective-FAITHFULNESS witness) — the
	// engine's structured SELF-REPORT of its readable-layer state, checked against the ground truth that the
	// observability contract makes addressable. §8's question is "ask the harness what it is thinking / how
	// confident it is / what its goal is — is the answer FAITHFUL to the actual internal state?" The report is
	// built FROM the readable ground truth (the active goal, the EXPANDED branch's tip thought, the lifecycle
	// state, V(s), the own-event ring) and CHECKED against it, so a confabulated field is laundered
	// hallucination in the introspective channel (the same failure the Filter kills) and shows up as a field
	// that does NOT agree with its source. The OPAQUE subconscious (hidden-seam FILTER->GATE->TRANSFORM) layer
	// is reported as UNOBSERVABLE, never confabulated — the honest "I can't see that" (the introspective twin
	// of the DECLINE neg-control). Pure CONTROL (a deterministic read + comparison over engine state; NO model
	// call, NO RNG, NO clock). Emitted ONLY when the opt-in introspect.self_report knob is ON (default OFF ⇒ no
	// report, no event ⇒ byte-identical). Carries {goal, line, state, value, recentEvents, fields (per-field
	// {layer, reported, observed, faithful}), opaque (the unobservable layer(s)), faithful (the conjunction)}.
	IntrospectFaithfulness Kind = "introspect.faithfulness"
	// PullupCustomize (Track G, G5 — the runtime-monitor PANEL CUSTOMIZATION witness) — the TUI's `^O`
	// runtime-monitor pull-up rendered a CUSTOMIZED panel set: only the chosen panels, in the chosen
	// order, at the chosen per-panel strip horizon (the SAME panel registry the Shift+Tab analysis tabs
	// derive from). This is a VIEW-surface observability signal (the operator's chosen instrument layout),
	// not a cognition event — it is emitted onto the bus by the TUI App (a bus subscriber), never by the
	// engine's own tick, so a headless engine run never emits it and a non-TUI golden is unaffected.
	// Emitted ONLY when the opt-in tui.pullup.panels knob is ON AND the customized stack differs from the
	// canonical full order/horizon (knob OFF ⇒ the full canon stack ⇒ no event ⇒ the default surface is
	// byte-identical). De-duplicated by the App (emitted on a change of the resolved layout, not every
	// render). Carries {panels, count, horizon, total, source}.
	PullupCustomize Kind = "tui.pullup"
	// IntrospectSuite (Track H, benchmark-taxonomy docs/internal/notes/2026-06-20-benchmark-taxonomy.md §8 + §7.6
	// #5 — the standing INTROSPECTIVE-FAITHFULNESS SUITE) — the rolled-up verdict of the §8 introspection
	// suite, a STANDING runnable SET of self-report probes the harness runs against ITSELF at quiescence and
	// the TUI/headless can show. DISTINCT from the single-shot IntrospectFaithfulness witness above — this is
	// the standing SUITE rollup. Each probe poses an introspection question ("what are you thinking?" / "why
	// did you decide that?" / "how confident are you, and why?" / "what is going on in your subconscious?")
	// and is checked against the ground truth the observability contract makes addressable: the EXPANDED
	// branch tip (conscious), the Controller's last decision+reason (reasoning), V(s)+goal+lifecycle (state/
	// confidence). A probe is FAITHFUL iff the self-report agrees with its independently re-read ground
	// truth; a confabulated field is laundered hallucination in the introspective channel — the same failure
	// the Filter kills — and fails the probe. The OPAQUE subconscious hidden-seam (FILTER->GATE->TRANSFORM)
	// probe is faithful iff it HONESTLY DECLINES ("I can't see that") rather than confabulating an
	// arbitration story (the introspective twin of the DECLINE neg-control). Pure CONTROL (a deterministic
	// read + compare — NO model call, NO RNG, NO clock); it authors no conscious-stream text and injects no
	// thought. Emitted ONLY when the opt-in introspect.suite knob is ON (default OFF ⇒ no suite, no event ⇒
	// byte-identical). Carries {tick, passed, total, faithful, declined, probes:[{name,question,layer,
	// reported,observed,faithful,declined}]}.
	IntrospectSuite Kind = "introspect.suite"
	// TraceView (Track G, G6 — the TRACE/FLOW swimlane WITNESS) — the TUI's Shift+Tab analysis surface
	// rendered the G6 TRACE tab over a loaded/frozen record: the seed->thought->seam->subconscious->action
	// round-trip as a swimlane timeline with the late-injection/Reenter desync markers and the phase/freq
	// readout (trip length, retracement count, land->deliver lag). Like PullupCustomize, this is a
	// VIEW-surface observability signal emitted onto the bus BY THE TUI App (a bus subscriber), never by
	// the engine's own tick, so a headless engine run never emits it and a non-TUI golden is unaffected.
	// Emitted ONLY when the opt-in tui.trace_flow knob is ON (knob OFF ⇒ the TRACE tab keeps its "panel
	// pending" placeholder ⇒ no event ⇒ the default surface is byte-identical). De-duplicated by the App
	// (emitted once when the TRACE tab is first opened with the flag on, not every render). Carries
	// {trip_ticks, retracements, land_to_deliver, theta}.
	TraceView Kind = "tui.trace_view"
	// FlywheelCapture (Track C, docs/internal/notes/2026-06-21-harness-rl-ml-roadmap.md §6 P0 + §6.5 — the
	// OFFLINE-RL DATA FLYWHEEL): one finalised per-decision training TUPLE — (state-features, action,
	// GROUNDED outcome) — was captured into the append-only dataset. Fired once per tuple at episode CLOSE,
	// after the terminal grounded Outcome is backfilled (the Monte-Carlo return assignment over the
	// trajectory). The label is the INDEPENDENT terminal grounded signal (the §6.5 invariant), NEVER a
	// self-judgment — sourced from the grounding spine + the GOAL_MET StopKind. Pure CONTROL/observability
	// (NO model call, NO RNG, NO clock — the tick is the seeded engine tick; capture alters no decision).
	// Carries {episode, tick, step, action, value, theta, n, goal_met, greturn, grounded_obs, refuted_obs}.
	// Emitted ONLY when the opt-in flywheel.capture knob is ON (default OFF ⇒ no Recorder, no capture, no
	// event ⇒ byte-identical).
	FlywheelCapture Kind = "flywheel.capture"
)

// allKinds is the full, ordered vocabulary (mirrors the const block above). It exists so
// the init gate can assert the count, and so callers can enumerate every kind.
var allKinds = []Kind{
	SubDispatch, SubFire, SubWorkflow, SubQuiet, SubSynthesize, SubOperator, SubSubagent,
	SkillMatch, SkillMint, SubCatalogOffer, SubSolverFormalize, SubScope, SubEntry, SubSpecGate, SubSparse, SubSingleStrong, SubQueryFormulate, Goal,
	Filter, Gate, Transform, Inject, Sufficiency, BandColdStart,
	Generate, Append, MCP, XRef, SeedIntent, Attention, RPIV, Route, InboxEscalate, Engage, EngageJudge,
	UCBSelect,
	Intention, Act, Observation, Respond, Ask,
	ActionTool, ActionSandboxDeny, ActionSafetyBlock, ActionBlocked, ActionAutoApprove, ActionEscalate,
	Decision, Exhaustion, Interrupt, ResourceTrigger, AnswerVerify,
	LLM, LLMFallback,
	Value, Regulator, Stability, Schedule, Convert, PathMint, RegistryRefine, CostGate,
	Ground, Percept,
	SessionSpawn, SessionDispatch, SessionMerge, SessionTerminate,
	MemoryRecord, MemoryRecall, MemoryReflect, MemoryCompact,
	Retrieval, RetrievalSemantic, Assemble,
	ConfigLoad, ConfigToggle, ConfigSkip,
	KnowledgeRecord, KnowledgeRecall, KnowledgeInvalidate, KnowledgePromote, SubSource, SubConcretize, GraphWriteBack,
	PersistLoad, PersistSave, PersistCurate, KeyframeClose,
	EscalationFloorStands, EscalationToolSelect, EscalationForceGround,
	LegibleTag, LegibleParity, LegibleNovel,
	Port, Lifecycle, Episode, Deadline, Arousal, Tick,
	ContinuousDecision, Deliberation, GroundComplete, RoutingTier, PerceptionClock, PerceptionOrient, PerceptionWeb,
	PerceptionSense, PerceptionSelfModel, PerceptionSelfModelReply, IntrospectFaithfulness, PullupCustomize, IntrospectSuite, TraceView,
	ConformanceWiring, ConformanceRollup,
	RegistrySnapshot, RegistryReset, RegistryDiff, RegistryBatch,
	PlanGateAudit, PlanGateVerdict,
	EstimateInnovate, EstimateCorrect, EstimateGate, EstimateCalibrate, EstimateConsistency, EstimateCorrelate, EstimateInfoGain, EstimateDecay,
	BenchStart, BenchCell, BenchVerdict, BenchReport,
	SelfBenchVerdict, SelfBenchPromote, SelfBenchRevert,
	FlywheelCapture,
}

func init() {
	// The wire vocabulary is a contract: a kind dropped or duplicated by a bad merge must fail
	// loudly, not silently. This is a DERIVED check (no hand-maintained count), so two parallel
	// edits that each append a new kind do NOT collide on a magic number. The full const-block ↔
	// allKinds equivalence is enforced in tests by TestAllKindsMatchConstBlock.
	seen := make(map[Kind]bool, len(allKinds))
	for i, k := range allKinds {
		if k == "" {
			panic("events: allKinds[" + itoa(i) + "] is empty")
		}
		if seen[k] {
			panic("events: allKinds contains duplicate kind " + string(k))
		}
		seen[k] = true
	}
}

// itoa is a tiny stdlib-free int->string for the init panic message (avoids importing
// strconv into the leaf package just for a build-time assertion).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
