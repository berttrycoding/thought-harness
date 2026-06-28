// capability.go — the subconscious Capability object (cognition redesign §3.3): the relevance-pulled
// ENTRY the conscious passively reaches. See docs/cognition/01-subconscious.md §3.3 / §2.1–2.2 / §6.
//
// The Capability is the unifying entry object the model is missing today. The conscious thinks; the
// subconscious WATCHES the stream and a Capability lights up by RELEVANCE (pull, not push). On its
// trigger it (a) CAPTURES the active-branch Context (§3.11, context.go), (b) PRODUCES a Workflow —
// reuse a seeded template or synthesise one on the fly (the reuse-seed-or-synthesise decision the
// entry must own, because a synthesised workflow does not exist until the Capability fires, §2.5).
//
// It WRAPS trigger + context-capture + workflow-seed. It does NOT own tools/skills and does NOT fix
// the structure (§2.2): tools/skills live on the SubAgent (the worker); the structure is the
// Workflow's. A Capability is mintable as a unit (reference + instances, §2.4) — this slice builds
// the runtime object; the registry/mint wiring is later.
//
// WHAT THIS REPLACES (integration target, wired later — §4 mapping table):
//   - Workflow.Recognize / Workflow.Triggers (workflow.go:219) — the relevance/trigger role moves OFF
//     the Workflow and ONTO the Capability. A Workflow stops self-triggering; a Capability produces it.
//     Capability.Relevance + Capability.Triggers are the replacement for that keyword-recognition.
//   - the specialist-firing-on-relevance entry — unified under one Capability object.
//
// SCOPE OF THIS SLICE. The Capability is NEW and ADDITIVE: it reuses the EXISTING Workflow / Program /
// Synthesize machinery for ProduceWorkflow (it does not re-implement synthesis), and it does not touch
// the live specialist / dispatch / engine runtime — that rewire is a later slice. The Scope ceiling
// (§3.3a) is NOT built here (a separate large slice); Capability is the trigger + context + workflow
// entry only.
package subconscious

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Capability is a relevance-pulled entry: a keyword-matched trigger plus the hooks to capture Context
// and produce a Workflow. It carries the collaborators ProduceWorkflow threads into the existing
// Synthesize / FromProgram path (catalog, backend, optional skill library, optional event bus) so a
// produced Workflow is byte-identical to one the synthesiser builds today — the Capability only owns
// the ENTRY (when to fire, what context, which workflow), never the structure or the workers.
type Capability struct {
	Name string // the capability's name (reference key when minted; logged on produce)

	// Triggers are the lower-cased keyword/phrase triggers the stream is matched against (the same
	// has-any matching Workflow.Recognize used, reused here so the trigger role transfers cleanly).
	Triggers []string

	// SeedProgram is an optional reusable workflow template (§2.5 reuse-seed-or-synthesise): when set,
	// ProduceWorkflow REUSES it instead of synthesising. nil ⇒ always synthesise on the fly.
	SeedProgram *cognition.Program

	// Knowledge are the paired knowledge-index refs (L3) this capability's context declares — handed to
	// CaptureContext so the produced Context carries the RAG-like declaration (the pull is wired later).
	Knowledge []KnowledgeRef

	// Collaborators threaded into the produced Workflow (the same ones FromProgram / Synthesize take).
	Catalog *cognition.OperatorRegistry // the operator catalog (nil ⇒ no sub-agents, like a bare workflow)
	Backend backends.Backend            // language faculty for synthesis + each sub-agent; also gists L1
	Library *cognition.SkillRegistry    // optional skill library Synthesize matches first (nil ⇒ skip)
	Emit    events.Emit                 // optional bus closure (nil ⇒ silent)

	// Scope is the AUTHORITY ceiling this Capability sets EAGER (§3.3a): the least-privilege band picks
	// resolve within at object birth. nil ⇒ unconstrained (today's behaviour — no separate ceiling). The
	// Capability is the §3.3a ceiling SOURCE; a worker may never widen it.
	Scope *Scope

	// WebLookup is the subconscious.web_search gate threaded into Produce's cognition.SynthesizeWeb call:
	// when true, a factual lookup question that hits no other shape gets a research->answer program that
	// STAFFS expose-affordances (the under-staffing fix). false (the default) ⇒ Synthesize, byte-identical.
	WebLookup bool
}

// WithScope sets the Capability's eager authority ceiling (§3.3a) and returns the Capability for chaining.
func (c *Capability) WithScope(s *Scope) *Capability { c.Scope = s; return c }

// WithWebLookup sets the subconscious.web_search gate (the lookup-research staffing) and returns the
// Capability for chaining. Default false ⇒ byte-identical (no lookup shape produced).
func (c *Capability) WithWebLookup(on bool) *Capability { c.WebLookup = on; return c }

// NewCapability builds a Capability with lower-cased triggers (the same normalisation
// Workflow.NewWorkflow applies), so trigger matching is case-insensitive and consistent with the
// recognition role it replaces. The remaining fields default to their zero values (nil seed ⇒ always
// synthesise; nil library/emit ⇒ skip); set them on the returned struct or via the constructor's
// collaborators.
func NewCapability(name string, triggers []string, catalog *cognition.OperatorRegistry,
	backend backends.Backend) *Capability {
	return &Capability{
		Name:     name,
		Triggers: lowerAll(triggers),
		Catalog:  catalog,
		Backend:  backend,
	}
}

// Relevance reports how strongly this capability lights up for the current stream (0..1) — the
// relevance PULL that is the entry's whole trigger condition (§3.3 / §2.1: the subconscious reaches
// UP by relevance). It matches the stream text against the capability's triggers using the same
// has-any phrase/word-boundary matcher Workflow.Recognize used (primitive_subagent.go:63), so the trigger
// role transfers with identical semantics.
//
// The score is binary-by-design here (1.0 on a trigger match, 0.0 on a miss): a graded relevance is a
// later refinement (the dispatch already gates on a fixed relevance, primitive_subagent.go:180). A capability
// with NO triggers never fires on relevance (0.0) — an always-on capability is an explicit later knob,
// not the default, so a mis-constructed capability stays silent rather than firing on everything.
func (c *Capability) Relevance(stream []types.Thought) float64 {
	if len(c.Triggers) == 0 {
		return 0.0
	}
	if hasAny(ctxTextDefault(stream), c.Triggers) {
		return 1.0
	}
	return 0.0
}

// RelevanceText is the raw-text form of Relevance for a caller that already has the stream text (the
// §3.3 "relevance-matched stream") rather than the Thought slice — same has-any matching, lower-cased.
func (c *Capability) RelevanceText(streamText string) float64 {
	if len(c.Triggers) == 0 {
		return 0.0
	}
	if hasAny(strings.ToLower(streamText), c.Triggers) {
		return 1.0
	}
	return 0.0
}

// RecognizeWorkflow is the GAP 5-DEEPER subsumption: the Capability — not the Workflow — OWNS the
// dispatch loop's recognition decision ("does this produced workflow apply to the current stream this
// tick?"). The dispatch loop calls THIS through the engine's Recognizer port when subconscious.
// capability_dispatch is on, INSTEAD of wf.Recognize(ctx) self-triggering. So the Capability becomes
// the live relevance/dispatch ENTRY (§3.3): the entry object the subconscious reaches up through, owning
// when its produced workflow fires — the architectural delta the redesign specifies (§4 mapping table:
// "produced *by* a Capability (not self-triggering)").
//
// The recognition PREDICATE (the live behaviour on the flag-ON path) is PERMISSIVE — the has-any criterion
// `!wf.Exhausted() && (wf.Bespoke || gradedRelevance(stream, wf.Triggers) > 0)` — the SAME relevance
// criterion as the legacy binary `has-any(stream, wf.Triggers)`. Recognition answers "does this workflow
// APPLY", not "is it worth firing"; the BESPOKE short-circuit is preserved (a program synthesised for this
// very goal still applies until exhausted, §2.5). It MUTATES wf.recognized so GateBias reads the value it
// set — the load-bearing Recognize-before-GateBias ordering is preserved. The predicate is evaluated
// against the WORKFLOW's own trigger set + bespoke flag (NOT the Capability's goal-derived Triggers, which
// are cosmetic in the episode-production path).
//
// theta is carried on the signature (the live regulator threshold the dispatch loop holds) but is NOT
// consulted at recognition. DO NOT θ-gate this (`gradedRelevance >= θ`): that is the REFUTED double-gate.
// An earlier version gated recognition on θ and the paired E5-deeper live A/B REGRESSED multi-hop grounding
// 0.89→0.71 (it dropped weakly-but-genuinely-relevant non-bespoke workflows the has-any path fires) — the
// borrowed-threshold trap of reusing a specialist-FIRING admission bar as a "does-this-apply" recognition
// bar. The θ/value admission is a DOWNSTREAM gate (GateBias / the value filter). See
// Workflow.recognizeViaGraded for the live predicate + the A/B citation.
//
// Because the episode-production path produces a BESPOKE workflow (it is synthesised for the episode goal),
// the bespoke short-circuit means episode recognition is UNCHANGED vs the binary path. The has-any predicate
// equals the binary criterion for non-bespoke (canonical / template) workflows too, so the recognised set
// is unchanged. The flag check itself lives at the wire (the engine installs this recognizer ONLY when
// subconscious.capability_dispatch is ON); when no recognizer is wired the Workflow self-triggers (Recognize)
// exactly as today.
func (c *Capability) RecognizeWorkflow(wf *Workflow, ctx []types.Thought, theta float64) bool {
	return wf.recognizeViaGraded(ctx, theta)
}

// AdmitPrimitiveSubAgent is the GAP 5-DEEPER PART 2 subsumption: the Capability — not the bare relevance gate —
// OWNS whether a specialist of this DOMAIN fires this tick. The dispatch loop calls THIS (through the
// engine's SpecialistGate port) when subconscious.capability_specialists is on, AFTER its own eff>theta
// relevance gate has already admitted the specialist. So the Capability becomes the live SPECIALIST-firing
// ENTRY — the OTHER half of §3.3 the Capability subsumes ("specialists firing on relevance — but there is
// no unifying Capability object"), the twin of RecognizeWorkflow (which subsumed the workflow self-trigger).
//
// The safe-stage predicate is the run's §3.3a Scope DOMAIN band: a specialist is admitted iff its domain is
// within the Capability's Scope ceiling (Scope.AllowsDomain). A general (empty-domain) episode Scope — the
// only Scope the episode path sources today (engine.episodeScope sets domain "") — admits EVERY domain, so
// the episode dispatch path is BYTE-IDENTICAL to the legacy bare-relevance firing. A domain-banded
// Capability admits only its band's specialists (the least-privilege bite the redesign specifies: a worker
// may never widen the ceiling — an off-band specialist stays dark even above θ). A nil Scope ⇒ unconstrained
// (no ceiling sourced) ⇒ admit, so a Capability constructed without a Scope is also byte-identical.
//
// It can only ever DENY a specialist the relevance gate already admitted (it is layered on top), never admit
// one relevance rejected. Pattern-A pure CONTROL: a case-insensitive domain string compare against the
// eager ceiling, no model call, no RNG/clock — so the admission set is deterministic and reproducible.
func (c *Capability) AdmitPrimitiveSubAgent(domain string) bool {
	if c.Scope == nil {
		return true // no ceiling sourced ⇒ unconstrained, byte-identical to the bare relevance gate
	}
	return c.Scope.AllowsDomain(domain)
}

// CaptureContext snapshots the active-branch Context for THIS trigger (§3.3 (a)) — the material the
// produced workflow's operators concretize against. It delegates to the package CaptureContext
// (context.go), passing the capability's backend (to gist L1) and its declared knowledge refs (L3).
// goal is the episode/sub-goal that drove the trigger (the §3.3 separate goal input, threaded down).
func (c *Capability) CaptureContext(g *graph.ThoughtGraph, goal string) *Context {
	return CaptureContext(g, c.Backend, goal, c.Knowledge)
}

// ProduceWorkflow produces the Workflow this capability runs for the given goal + trigger context
// (§3.3 (b)): the reuse-seed-or-synthesise decision the entry owns (§2.5). It REUSES the existing
// Workflow / Program / Synthesize machinery rather than re-implementing it:
//
//   - SeedProgram set ⇒ REUSE that template (wrapped via FromProgram — the verified, scheduled form).
//   - else ⇒ SYNTHESISE on the fly via cognition.Synthesize (skill-match → LLM toolmaker → heuristic),
//     then wrap the resulting Program via FromProgram. ok=false (no workflow shape applies — the goal
//     is handled directly by specialists) is propagated as (nil, false): the entry does not invent a
//     workflow where the synthesiser declines one.
//
// The produced Workflow is byte-identical to one the engine builds today for the same program — the
// Capability changes WHO produces it (the entry, not the workflow self-triggering), not its shape.
// stream is the trigger context (passed to Synthesize, which reads it when the goal is empty).
func (c *Capability) ProduceWorkflow(goal string, stream []types.Thought) (*Workflow, bool) {
	wf, _, ok := c.Produce(goal, stream)
	return wf, ok
}

// GAP-8 REMAINING WIRE — BUILT (gap-5-deeper final sub-slice, behind convert.skill_reframe ×
// subconscious.capability, default-OFF ⇒ byte-identical). The redesign moves goal→skill relevance ONTO
// the Capability ("the Capability owns goal→skill relevance", §3.8 / audit gap 8). Gap 8 retired the
// Skill's own goal-self-match (when convert.skill_reframe is on, SkillRegistry.Match / MatchWithinTier
// return NO match) — so the reframe-on path LOST skill recall (Synthesize's step-0 skill match no longer
// fired, the goal fell through to LLM synthesis). This sub-slice RECOVERS that recall by routing it
// through the Capability (the relevance entry the redesign makes the only goal-matcher), via recallReframed:
//
//	In Produce, when c.Library.Reframed() (the reframe is on), the CAPABILITY does the goal→skill match
//	the registry abandoned — c.Library.MatchReframedWithinTier(goal, ceiling) with ceiling =
//	c.Scope.SkillTier() (the §3.3a skill-tier ceiling the Capability already sources) — BEFORE falling to
//	cognition.Synthesize. A hit RESOLVES the matched reframed skill's body at RUN time (ResolveBody, the
//	gap-8 bounded acyclic depth≤3 resolver — no RNG/clock) and produces a single-phase Workflow that runs
//	the resolved worker prompt, Source "skill:<name>" (the same provenance the legacy step-0 skill-match
//	tags, so the engine's Goal-lifecycle/path tracking is unchanged). A miss falls through to Synthesize
//	exactly as now. This makes the Capability the ONLY goal-matcher (the redesign target) and routes recall
//	through the relevance entry.
//
// DEFAULT-OFF byte-identical: recallReframed is reached only when c.Library.Reframed() (convert.skill_reframe
// ON), and Produce is reached only when subconscious.capability is ON (else the engine's inline Synthesize
// runs). With the reframe OFF the new branch is skipped entirely and the legacy synth.go library.Match /
// Expand Program flywheel runs unchanged (the W5-validated mint/recall path the scenario goldens anchor).
// With the reframe ON but no reframed skill matching, Produce falls through to Synthesize as today.
//
// Produce is ProduceWorkflow plus the SynthResult bookkeeping a caller needs (the Program for
// trace->skill / session-tree wiring and the Source for the skill-match / path logic) from a SINGLE
// synthesis — so the Capability can FULLY replace the engine's inline Synthesize+FromProgram+SetWorkflow
// without synthesising twice. A seeded template returns a "seed"-sourced result wrapping the seed program.
// (nil, nil, false) when the synthesiser declines a workflow (§2.5 — specialists handle the goal directly).
func (c *Capability) Produce(goal string, stream []types.Thought) (*Workflow, *cognition.SynthResult, bool) {
	if c.SeedProgram != nil {
		// Reuse the seeded template: wrap it through the same FromProgram the synthesiser path uses.
		wf := FromProgram(c.SeedProgram, c.Catalog, c.Backend, c.Emit, goal)
		c.emitProduced(wf, "seed")
		return wf, &cognition.SynthResult{Program: *c.SeedProgram, Source: "seed"}, true
	}
	// GAP-8 remaining wire (flag-gated, default-OFF byte-identical): when the Skill reframe is on, the
	// Capability — the redesign's sole goal-matcher — recalls a reframed skill BEFORE synthesising,
	// recovering the recall the reframe retired (Match returns nothing under the reframe). A hit produces
	// the workflow from the recalled skill's runtime-resolved body; a miss falls through to Synthesize.
	if c.Library != nil && c.Library.Reframed() {
		if wf, res, ok := c.recallReframed(goal); ok {
			return wf, res, true
		}
	}
	// Synthesise on the fly — reuse cognition.SynthesizeWeb exactly (no re-implementation). c.WebLookup is
	// false by default ⇒ identical to cognition.Synthesize (byte-identical); ON ⇒ a factual lookup question
	// that hit no other shape staffs expose-affordances (the under-staffing fix, subconscious.web_search).
	res, ok := cognition.SynthesizeWeb(goal, stream, c.Catalog, c.Backend, c.Emit, c.Library, c.WebLookup)
	if !ok {
		return nil, nil, false // no workflow shape — specialists handle the goal directly (§2.5 / synth.go)
	}
	prog := res.Program
	wf := FromProgram(&prog, c.Catalog, c.Backend, c.Emit, goal)
	c.emitProduced(wf, res.Source)
	return wf, res, true
}

// recallReframed is the GAP-8 reframed-skill recall the Capability owns (only reached when the reframe is
// on, c.Library.Reframed()): the reframe-path analogue of synth.go's legacy library.Match → Expand →
// FromProgram flywheel. It (1) goal→skill matches a REFRAMED skill within the §3.3a skill-tier ceiling
// the Capability sources (MatchReframedWithinTier, ceiling = c.Scope.SkillTier()); (2) RESOLVES the
// matched skill's body at run time (ResolveBody — the gap-8 bounded acyclic depth≤3 prompt resolver, no
// RNG/clock) into the worker prompt; (3) builds a single-phase Program that RUNS that resolved prompt and
// wraps it via the SAME FromProgram every produce path uses, with Source "skill:<name>" (the provenance
// the legacy skill-match tags, so the engine's Goal-lifecycle / path tracking is unchanged).
//
// ok=false (fall through to Synthesize) when: no reframed skill matched the goal within the ceiling; or
// the matched skill failed to resolve (a malformed sub-skill chain — the durability guard rejects it). A
// nil c.Library never reaches here (the caller gates on Reframed()). c.Scope may be nil (no ceiling
// sourced) ⇒ an uncapped (0) tier ceiling. Deterministic: the match (lexical argmax, Names() order) and
// the resolver (bounded recursion) carry no RNG/clock.
func (c *Capability) recallReframed(goal string) (*Workflow, *cognition.SynthResult, bool) {
	if c.Catalog == nil {
		return nil, nil, false // no catalog to mint the per-skill operator into — defer to Synthesize
	}
	// FIX (gap-8 remaining-wire): recall BOTH shapes the reframe-on path must serve — reframed
	// (prompt-bodied) skills AND legacy Program-bodied skills (the seed library, the M5 path skills, and
	// every trace->skill / W5 minted skill, none of which is ever minted in reframed form). A
	// reframed-ONLY match (MatchReframedWithinTier) left the whole library un-recallable, so the recall
	// short-circuit died — every recurring goal re-synthesised, exciting peak_n and failing the recall
	// gates. MatchRecallableWithinTier is the inclusive matcher (the Match set ∪ reframed skills); it is
	// inert off the reframe path, so the legacy synth.go flywheel is byte-identical when the flag is OFF.
	skill, found := c.Library.MatchRecallableWithinTier(goal, c.skillTierCeiling())
	if !found {
		return nil, nil, false // no recallable skill fired — Synthesize handles the goal
	}
	// LEGACY Program-bodied skill: resolve its body the EXACT way synth.go's legacy step-0 recall does —
	// Expand the (bounded, acyclic) sub-skill calls into a pure-operator Program, VerifyProgram it, then
	// wrap via the same FromProgram. The executable shape stays the operator Program every seed + minted
	// skill carries; the Capability only owns WHO recalled it (the redesign target). On an Expand/verify
	// failure, defer to Synthesize (never a stub), exactly as synth.go falls through.
	//
	// LEGACY(redesign): the recall of a legacy Program-bodied (non-reframed) skill through the Capability —
	// load-bearing for the redesign path ITSELF (the seed library, the M5 path skills, and every W5 minted
	// skill are Program-bodied, never reframed, so the reframe-ON path MUST still recall them) — removable
	// when the seed library is migrated to reframed form (gap-8 follow-up).
	if !skill.IsReframed() {
		return c.recallLegacy(goal, skill)
	}
	resolved, err := c.Library.ResolveBody(skill)
	if err != nil {
		return nil, nil, false // malformed sub-skill chain (durability guard) — defer to Synthesize, never a stub
	}
	// MAKE THE RESOLVED PROMPT REAL: the reframed skill's executable body is a PROMPT, and a worker reads
	// its content directive from its operator's Intent (subagent.go fireReason → OperatorApply(... spec.Intent ...)).
	// A reframed skill carries no operator structure, so we MINT a per-skill generative operator whose
	// Intent IS the runtime-resolved prompt (the convertibility story: a recalled reframed skill becomes a
	// named, automatic operator). Mint is idempotent (re-recall reuses it) and namespaced (skill_<name>) so
	// it never collides with a frozen seed operator. A mint failure (a degenerate prompt that fails Verify —
	// e.g. <3 words) falls through to Synthesize, never a stub that drops the prompt.
	opName := reframedSkillOp(skill.Name)
	if _, ok := c.Catalog.Mint(opName, "generative", resolved.Prompt); !ok {
		return nil, nil, false // the resolved prompt is not a verifiable operator intent — defer to Synthesize
	}
	// Build the single-phase Program that RUNS the recalled skill's resolved prompt via that operator.
	// Synthesized=true ⇒ the workflow is bespoke (recognised for the whole episode, like a matched legacy
	// skill's expanded program); Source "skill:<name>" preserves the legacy skill-match provenance the engine
	// reads (reactive.go: a "skill:" source drives the Goal lifecycle + path tracking).
	prog := cognition.Program{
		Root:        cognition.NewSeq(cognition.NewStep(opName, "general", "runtime-resolved reframed skill body")),
		Goal:        goal,
		Synthesized: true,
		Rationale:   "reframed skill '" + skill.Name + "' (runtime-resolved prompt)",
	}
	wf := FromProgram(&prog, c.Catalog, c.Backend, c.Emit, goal)
	source := "skill:" + skill.Name
	c.emitReframedRecall(skill, resolved, wf)
	c.emitProduced(wf, source)
	return wf, &cognition.SynthResult{Program: prog, Source: source}, true
}

// recallLegacy is the reframe-path recall of a LEGACY (Program-bodied) skill — the seed library, the M5
// analogy/induction/deduction path skills, and every trace->skill / W5 minted skill (none of which is ever
// in reframed form). It RESOLVES the skill's body the EXACT way synth.go's legacy step-0 recall does:
// Expand the (bounded, acyclic, depth<=3) sub-skill calls into a pure-operator Program, structurally verify
// it (VerifyProgram), then wrap it via the SAME FromProgram every produce path uses, with Source
// "skill:<name>" (the provenance the engine reads — reactive.go: a "skill:" source drives the Goal
// lifecycle + path tracking, e.g. IsPath → activePath). So the executable artifact, the workflow shape, and
// the trace provenance are byte-identical to the legacy flag-OFF recall — the Capability only owns WHO
// recalled it. On an Expand/verify failure it defers to Synthesize (never a stub), exactly as synth.go's
// step-0 falls through to the toolmaker. It emits the SkillMatch + produced events so the recall reads in a
// trace identically to synth.go's legacy step-0 (the M5/campaign recall gates read Data["skill"]).
//
// LEGACY(redesign): the reframe-path recall of a legacy Program-bodied skill — it Expands the sub-skill
// chain into a pure-operator Program and wraps it via FromProgram (the legacy executable shape). This is
// load-bearing for the redesign path itself until the seed/W5 skills exist in reframed (prompt-bodied) form
// — removable when the seed library is migrated to reframed form (gap-8 follow-up).
func (c *Capability) recallLegacy(goal string, skill cognition.Skill) (*Workflow, *cognition.SynthResult, bool) {
	prog, err := c.Library.Expand(skill) // resolve sub-skills (bounded, acyclic) into a pure-operator Program
	if err != nil {
		return nil, nil, false // a malformed sub-skill chain (durability guard) — defer to Synthesize
	}
	if ok, _ := cognition.VerifyProgram(prog, c.Catalog); !ok {
		return nil, nil, false // the expanded program does not verify — defer to Synthesize, never a stub
	}
	prog.Goal = goal // anchor the recalled program on this episode's goal (Expand carries skill.Body.Goal)
	wf := FromProgram(&prog, c.Catalog, c.Backend, c.Emit, goal)
	source := "skill:" + skill.Name
	c.emitLegacyRecall(skill, prog, wf)
	c.emitProduced(wf, source)
	return wf, &cognition.SynthResult{Program: prog, Source: source}, true
}

// emitLegacyRecall logs a legacy (Program-bodied) skill recalled through the Capability on the reframe path
// — it mirrors synth.go's legacy step-0 SkillMatch event (same kind, same Data["skill"]/tier/shape/
// sub_skills/program fields) so the reframe-path recall reads identically in a trace. A nil Emit is silent.
func (c *Capability) emitLegacyRecall(skill cognition.Skill, prog cognition.Program, wf *Workflow) {
	if c.Emit == nil || wf == nil {
		return
	}
	c.Emit(events.SkillMatch,
		"capability '"+c.Name+"' recalled skill '"+skill.Name+"': "+prog.Shape(),
		events.D{
			"capability": c.Name,
			"skill":      skill.Name,
			"tier":       skill.Tier,
			"shape":      prog.Shape(),
			"sub_skills": skill.SubSkills(),
			"program":    prog.ToDict(),
			"reframed":   false,
		})
}

// reframedSkillOp namespaces the per-skill operator a reframed recall mints (skill_<name>) so the
// runtime-resolved prompt rides a real catalog operator's Intent without colliding with a frozen seed
// operator. The name stays a valid operator identifier ([a-z0-9] after the registry strips -/_).
func reframedSkillOp(skillName string) string { return "skill_" + skillName }

// skillTierCeiling is the §3.3a skill-tier ceiling this Capability sources for recall — c.Scope.SkillTier()
// when a Scope is set, else 0 (uncapped). nil-safe so a Capability constructed without a Scope still recalls.
func (c *Capability) skillTierCeiling() int {
	if c.Scope == nil {
		return 0
	}
	return c.Scope.SkillTier()
}

// emitReframedRecall logs the reframed-skill recall on the bus (the recall is invisible otherwise). It
// REUSES the existing SkillMatch kind (no new event kind — the count gate is shared with concurrent work),
// mirroring synth.go's legacy step-0 skill-match event so the reframe-path recall reads the same in a
// trace. A nil Emit is silent. resolved carries the sub-skill chain + call count for observability.
func (c *Capability) emitReframedRecall(skill cognition.Skill, resolved cognition.ResolvedSkill, wf *Workflow) {
	if c.Emit == nil || wf == nil {
		return
	}
	c.Emit(events.SkillMatch,
		"capability '"+c.Name+"' recalled reframed skill '"+skill.Name+"' (resolved): "+wf.Name,
		events.D{
			"capability": c.Name,
			"skill":      skill.Name,
			"tier":       skill.Tier,
			"sub_skills": resolved.SubSkills,
			"calls":      resolved.Calls,
			"reframed":   true,
		})
}

// emitProduced logs that the capability produced a workflow (the entry's one decision: which workflow,
// from where). A nil Emit is silent (the §ProduceWorkflow callers can run head-less). source is "seed"
// for a reused template, or the synthesiser's source ("llm" | "heuristic" | "skill:<name>").
func (c *Capability) emitProduced(wf *Workflow, source string) {
	if c.Emit == nil || wf == nil {
		return
	}
	c.Emit(events.SubSynthesize,
		"capability '"+c.Name+"' produced workflow ("+source+"): "+wf.Name,
		events.D{
			"capability": c.Name,
			"workflow":   wf.Name,
			"source":     source,
			"bespoke":    wf.Bespoke,
			"phases":     len(wf.Phases),
		})
}
