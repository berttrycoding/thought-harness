package tui

// registry_data.go — the bridge-side assembler for the REGISTRY browser tab (cognition.RegistryCatalog).
// It is the registry analog of BuildSnapshot: the ONLY place that touches the engine's registry
// accessors, translating them into the plain-string catalog the pure view (cognition.RenderRegistry)
// renders. Every read is read-only; the engine never learns the browser exists.
//
// Discipline: this file names ONLY cognition.* types from internal/tui/cognition (the view model). The
// engine-side registry types (internal/cognition's OperatorRegistry/Skill, action's Tool, the
// specialist roster) are reached purely by type INFERENCE off the engine accessors — never named —
// because internal/cognition and internal/tui/cognition share the package name `cognition` and naming
// both would collide. Inference (`:=` + selector) sidesteps it entirely.

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// BuildRegistryCatalog assembles the whole registry inventory from the live engine. Cheap (it ranges a
// few maps/slices), so the app rebuilds it per frame while the Registry tab is open — minted entries
// then show up live. A nil engine yields a single placeholder section (the "no model" state).
func (b *EngineBridge) BuildRegistryCatalog() cognition.RegistryCatalog {
	e := b.eng.Load()
	if e == nil {
		return cognition.RegistryCatalog{Sections: []cognition.RegSection{{
			ID: "none", Title: "Operators", Count: 0,
			Note: "no engine — open /settings to configure a model",
		}}}
	}
	return cognition.RegistryCatalog{Sections: []cognition.RegSection{
		b.regOperators(e),
		b.regSubAgents(e),
		b.regSkills(e),
		b.regWorkflows(e),
		b.regTools(e),
		b.regPrompts(),
		b.regMemory(e),
		b.regKnowledge(e),
	}}
}

// regKnowledge — the M3 KNOWLEDGE registry (domain knowledge, third-person, durable). The sibling to
// memory: memory answers "have I been here before?", knowledge answers "what do I know about this?".
// Grouped by kind (fact / pattern / snippet); each entry shows its provenance (Source) + Trust prior +
// the never-fabricate Grounded flag. Starts empty (earn-it-from-reality is the purest grounding story,
// §7 open flag #2) — the empty state names how an item enters: ingest / reality write-back / distillation.
func (b *EngineBridge) regKnowledge(e *engine.Engine) cognition.RegSection {
	kr := e.Knowledge()
	byKind := map[string][]cognition.RegEntry{}
	for _, k := range kr.Current() {
		tags := []string{fmt.Sprintf("trust=%.2f", k.Trust)}
		if k.Grounded {
			tags = append(tags, "grounded")
		}
		detail := "source: " + k.Source
		entry := cognition.RegEntry{Name: clipName(k.Statement, 48), Detail: detail, Tags: tags, Status: "present"}
		byKind[k.Kind] = append(byKind[k.Kind], entry)
	}
	var groups []cognition.RegGroup
	for _, kind := range []string{"fact", "pattern", "snippet"} {
		entries := byKind[kind]
		if len(entries) == 0 {
			entries = []cognition.RegEntry{{Name: "(none yet)", Detail: knowledgeEmptyHint(kind), Status: "empty"}}
		}
		groups = append(groups, cognition.RegGroup{Label: kind, Entries: entries})
	}
	return cognition.RegSection{
		ID: "knowledge", Title: "Knowledge", Count: kr.Len(),
		Note:   "domain knowledge (M3) — third-person, durable, never-fabricate; enters via ingest / reality write-back / distillation",
		Groups: groups,
	}
}

// knowledgeEmptyHint names how an item of each kind enters the knowledge registry (the earn-it story).
func knowledgeEmptyHint(kind string) string {
	switch kind {
	case "fact":
		return "a reality observation write-backs as a fact (next time it is a rung-2 hit)"
	case "pattern":
		return "an idle-tick distillation turns a recurring grounded pattern into a pattern item"
	case "snippet":
		return "a reusable snippet ingested from a seed corpus or grounded from reality"
	default:
		return "enters via ingest / reality write-back / distillation"
	}
}

// regOperators — the operator catalog, grouped by family (transformative / relational / generative /
// primitive), with the runtime-minted ones in their own group.
func (b *EngineBridge) regOperators(e *engine.Engine) cognition.RegSection {
	cat := e.Catalog()
	byFamily := map[string][]cognition.RegEntry{}
	order := []string{}
	for _, name := range cat.Names() {
		spec, ok := cat.Get(name)
		if !ok || spec.Synthesized {
			continue // minted operators go in their own group below
		}
		fam := spec.Family
		if _, seen := byFamily[fam]; !seen {
			order = append(order, fam)
		}
		byFamily[fam] = append(byFamily[fam], cognition.RegEntry{
			Name: name, Detail: spec.Intent, Tags: opTags(string(spec.Move), spec.FuelNeeding, spec.ToolScope),
		})
	}
	groups := make([]cognition.RegGroup, 0, len(order)+1)
	for _, fam := range order {
		groups = append(groups, cognition.RegGroup{Label: fam, Entries: byFamily[fam]})
	}
	if minted := cat.Minted(); len(minted) > 0 {
		var entries []cognition.RegEntry
		for _, name := range minted {
			detail, mv := "", ""
			if spec, ok := cat.Get(name); ok {
				detail = spec.Intent
				mv = string(spec.Move)
			}
			entries = append(entries, cognition.RegEntry{
				Name: name, Detail: detail, Minted: true, Tags: opTags(mv, false, nil),
			})
		}
		groups = append(groups, cognition.RegGroup{Label: "minted this run", Entries: entries})
	}
	return cognition.RegSection{
		ID: "operators", Title: "Operators", Count: len(cat.Names()),
		Note:   "moves on the abstraction ladder — tagged by Move (ground/lift/reframe/transcode/assess), family, tool-scope",
		Groups: groups,
	}
}

// opTags renders an operator's representation-space tags: its Move (the directed step on the abstraction
// ladder, the M2 structural change), a "fuel" mark for the GROUND/REFRAME ops whose output is a shape
// missing concrete content (so the sourcing ladder fills it, M3), and its tool-scope. nil when nothing.
func opTags(move string, fuelNeeding bool, scope []string) []string {
	var tags []string
	if move != "" {
		tags = append(tags, "▸"+move)
	}
	if fuelNeeding {
		tags = append(tags, "fuel")
	}
	if len(scope) > 0 {
		tags = append(tags, "["+strings.Join(scope, ", ")+"]")
	}
	return tags
}

// toolBackedPrimitives / modelRolePrimitives are the M2 real primitive set's two kinds (primitive_subagent.go
// DefaultPrimitiveSubAgents): five tool-backed senses+hands that carry ground truth, and two model-driven
// stance roles. Used to classify a roster domain into its real-vs-stub Status flag (the M6 truthfulness
// goal) — a domain in neither set is a runtime-minted specialist.
var (
	toolBackedPrimitives = map[string]bool{"compute": true, "recall": true, "read": true, "search": true, "run": true}
	modelRolePrimitives  = map[string]bool{"skeptic": true, "advocate": true}
)

// regSubAgents — the live SPECIALIST roster (the pull-specialists that fire by relevance in the
// subconscious). After the representation-space rebuild (M2) this is the REAL primitive set: five
// tool-backed (compute/recall/read/search/run) + two model-driven stance roles (skeptic/advocate);
// the fake simulation/safety/refactor specialists + the toy MemoryKB are deleted. M6 surfaces the
// real-vs-stub TRUTH: tool-backed primitives carry Status=present (colOk), model roles Status=present
// with a "model" tag, minted ones the accent, and a reality-REFUTED mint (Convert().Demoted) gets a
// "demoted" tag in the warn tone — so the browser agrees with the convertibility panel at a glance.
func (b *EngineBridge) regSubAgents(e *engine.Engine) cognition.RegSection {
	demotedSet := map[string]bool{}
	for _, n := range e.Convert().Demoted {
		demotedSet[n] = true
	}
	var toolBacked, roles, minted []cognition.RegEntry
	for _, sp := range e.Subconscious().Specialists() {
		dom := sp.Domain()
		entry := cognition.RegEntry{Name: dom, Detail: specialistRole(dom)}
		switch {
		case toolBackedPrimitives[dom]:
			entry.Status = "present" // a real sense/hand — carries ground truth
			entry.Tags = []string{"tool-backed"}
			toolBacked = append(toolBacked, entry)
		case modelRolePrimitives[dom]:
			entry.Status = "present" // a real model-driven stance — content from the LLM, with a reason
			entry.Tags = []string{"model-role"}
			roles = append(roles, entry)
		default:
			entry.Minted = true // a runtime-minted domain primitive (convertibility paved a hot pattern)
			if demotedSet[dom] {
				entry.Status = "partial" // reality REFUTED it — keep-or-revert demoted (agrees with the convert panel)
				entry.Tags = []string{"demoted"}
			}
			minted = append(minted, entry)
		}
	}
	groups := []cognition.RegGroup{
		{Label: "tool-backed (senses + hands — REAL ground truth)", Entries: toolBacked},
		{Label: "model-driven roles (stance, content from the LLM)", Entries: roles},
	}
	if len(minted) > 0 {
		groups = append(groups, cognition.RegGroup{Label: "minted this run (convertibility)", Entries: minted})
	}
	return cognition.RegSection{
		ID: "subagents", Title: "Sub-agents", Count: len(toolBacked) + len(roles) + len(minted),
		Note:   "real primitive set (M2): tool-backed compute/recall/read/search/run + model roles skeptic/advocate",
		Groups: groups,
	}
}

// regSkills — the goal-matched skill layer (unit + composite), grouped by tier, minted skills tagged.
// M6 surfaces each skill's PROGRAM shape (s.Body.Shape() — the one-line control-flow signature, e.g.
// "seq(decompose, generate, validate)") so the browser shows WHAT a skill actually runs, not just its
// name. The shape leads the detail; the description follows.
func (b *EngineBridge) regSkills(e *engine.Engine) cognition.RegSection {
	sk := e.Skills()
	var unit, composite []cognition.RegEntry
	for _, name := range sk.Names() {
		s, ok := sk.Get(name)
		if !ok {
			continue
		}
		entry := cognition.RegEntry{
			Name:   name,
			Detail: skillProgramDetail(s.Body.Shape(), s.Description, s.Tier),
			Tags:   triggerTags(s.Triggers),
			Minted: s.Synthesized,
		}
		if s.Tier == "composite" {
			composite = append(composite, entry)
		} else {
			unit = append(unit, entry)
		}
	}
	return cognition.RegSection{
		ID: "skills", Title: "Skills", Count: len(unit) + len(composite),
		Note: "named capability over operators — each is a Program (its shape shown); unit = one move, composite = calls sub-skills",
		Groups: []cognition.RegGroup{
			{Label: "unit", Entries: unit},
			{Label: "composite", Entries: composite},
		},
	}
}

// regWorkflows — workflows are synthesised per goal (named by control-flow shape), so there is no seed
// list; show the deterministic-control recognised shapes + any live program runs the convertibility
// organ is tracking.
func (b *EngineBridge) regWorkflows(e *engine.Engine) cognition.RegSection {
	shapes := []cognition.RegEntry{
		{Name: "design-build-validate", Detail: "decompose > generate > validate (the canonical fallback)"},
		{Name: "compare-contrast-rank", Detail: "for comparison / which-is-better questions"},
		{Name: "measure-eliminate-loop", Detail: "loop(measure > eliminate) for optimisation"},
		{Name: "decompose-hypothesize-measure", Detail: "for analysis / diagnosis"},
	}
	var runs []cognition.RegEntry
	for _, pr := range e.Convert().ProgramRuns() {
		tags := []string{"×" + itoa(pr.Count)}
		runs = append(runs, cognition.RegEntry{Name: pr.Shape, Detail: "observed program shape", Tags: tags, Minted: pr.Minted})
	}
	groups := []cognition.RegGroup{{Label: "recognised shapes (control)", Entries: shapes}}
	if len(runs) > 0 {
		groups = append(groups, cognition.RegGroup{Label: "live program runs (toward minting)", Entries: runs})
	}
	return cognition.RegSection{
		ID: "workflows", Title: "Workflows", Count: len(shapes) + len(runs),
		Note:   "a program of operators in series/parallel/loop, synthesised per goal",
		Groups: groups,
	}
}

// regTools — the Action-layer tools the executor resolves (the real effects a scoped sub-agent dispatches).
func (b *EngineBridge) regTools(e *engine.Engine) cognition.RegSection {
	reg := e.Tools()
	var entries []cognition.RegEntry
	note := "real effects, gated by sandbox + evaluate + approve"
	if reg != nil {
		for _, t := range reg.List() {
			// a live executor: a real, dispatchable effect — Status=present (colOk) so the browser shows
			// reality is wired (M6 truthfulness: real vs stub at a glance).
			entries = append(entries, cognition.RegEntry{Name: t.Name(), Detail: t.Description(), Status: "present"})
		}
	} else {
		// no executor (no --workspace): show the static built-in DEFINITIONS so the catalog is still
		// complete, flagged inert (Status=idea/colFaint — defined but nothing dispatches). Name/Description
		// are tool identity (independent of the workdir arg).
		for _, t := range action.DefaultTools(".", 0) {
			entries = append(entries, cognition.RegEntry{Name: t.Name(), Detail: t.Description(),
				Status: "idea", Tags: []string{"inert"}})
		}
		note = "definitions only — no --workspace bound, so nothing dispatches"
	}
	return cognition.RegSection{
		ID: "tools", Title: "Tools", Count: len(entries), Note: note,
		Groups: []cognition.RegGroup{{Label: "built-in", Entries: entries}},
	}
}

// regPrompts — the backend's prompt-producing roles. There is NO prompt registry (prompts are inline
// per backend method); this lists the fixed contract of roles, grouped narrative vs structured (JSON).
func (b *EngineBridge) regPrompts() cognition.RegSection {
	narrative := []cognition.RegEntry{
		{Name: "generate", Detail: "Conscious drafts its next thought"},
		{Name: "transform", Detail: "the hidden seam re-voices an injection as Conscious's own"},
		{Name: "summarize", Detail: "compress a branch to a one-line gist"},
		{Name: "respond", Detail: "compose the user-facing answer"},
		{Name: "operator_apply", Detail: "a sub-agent applies one named operator to context"},
		{Name: "specialist", Detail: "a domain-scoped sub-agent's contribution"},
	}
	structured := []cognition.RegEntry{
		{Name: "judge_admission", Detail: "Filter Pattern-C escalation: is this a laundered hallucination? (floor in internal/control)", Tags: []string{"JSON"}},
		{Name: "decide", Detail: "the Controller's next move (THINK/BRANCH/ACT/STOP…)", Tags: []string{"JSON"}},
		{Name: "intention", Detail: "the watched seam's single concrete world action", Tags: []string{"JSON"}},
		{Name: "synthesize_program", Detail: "the toolmaker writes a program tree", Tags: []string{"JSON"}},
	}
	return cognition.RegSection{
		ID: "prompts", Title: "Prompts", Count: len(narrative) + len(structured),
		Note: "NO registry — inline per Backend method (backends.go / llm.go)",
		Groups: []cognition.RegGroup{
			{Label: "narrative roles", Entries: narrative},
			{Label: "structured roles (JSON)", Entries: structured},
		},
	}
}

// regMemory — memory has no registry today (it is implicit: the thought graph + convertibility +
// transcript). This surfaces the PROPOSED memory primitive registry (docs/internal/design/registry-redesign.md §3): 3
// stores + 7 operations, each tagged present/partial/idea, with the live numbers woven in.
func (b *EngineBridge) regMemory(e *engine.Engine) cognition.RegSection {
	turns := len(e.Transcript())

	// DECLARATIVE memory (P2.3/P6.x) — the LIVE registries: recorded grounded episodes, currently-valid
	// bi-temporal beliefs, and learned person preferences. Never-fabricate: only grounded outcomes here.
	var episodes []cognition.RegEntry
	for _, ep := range e.Episodic().All() {
		tags := []string{fmt.Sprintf("V=%.2f", ep.Value)}
		if ep.Grounded {
			tags = append(tags, "grounded")
		}
		episodes = append(episodes, cognition.RegEntry{Name: clipName(ep.Goal, 40), Detail: ep.Outcome, Tags: tags})
	}
	if len(episodes) == 0 {
		episodes = []cognition.RegEntry{{Name: "(none yet)", Detail: "a grounded episode is recorded at episode-end", Status: "empty"}}
	}
	var beliefs []cognition.RegEntry
	for _, bl := range e.Semantic().Current() {
		beliefs = append(beliefs, cognition.RegEntry{Name: clipName(bl.Statement, 48), Detail: "source: " + bl.Source, Tags: []string{"valid"}})
	}
	if len(beliefs) == 0 {
		beliefs = []cognition.RegEntry{{Name: "(none yet)", Detail: "an idle-tick reflection distils a high-value grounded episode into a belief", Status: "empty"}}
	}
	var prefs []cognition.RegEntry
	for _, p := range e.Person().Applied() {
		prefs = append(prefs, cognition.RegEntry{Name: p.Trait + " = " + p.Value, Detail: itoa(p.Evidence) + " consistent overrides", Minted: true})
	}
	if len(prefs) == 0 {
		prefs = []cognition.RegEntry{{Name: "(none yet)", Detail: "a consistently-overridden default becomes a learned preference", Status: "empty"}}
	}

	// procedural / working memory: the in-run context + the convertibility-minted capability (the OTHER
	// two memory kinds, kept for the full picture).
	cv := e.Convert()
	nSpec, nSkill, nOp := len(cv.Minted), len(cv.MintedSkill), len(e.Catalog().Minted())
	procedural := []cognition.RegEntry{
		{Name: "working", Status: "present", Detail: "the active branch's context window (bounded focus)", Tags: []string{itoa(turns) + " turns"}},
		{Name: "procedural", Status: "present", Detail: "convertibility-minted capability (effortful → automatic)",
			Tags: []string{itoa(nSpec) + " spec", itoa(nSkill) + " skill", itoa(nOp) + " op"}},
	}
	return cognition.RegSection{
		ID: "memory", Title: "Memory", Count: e.Episodic().Len() + e.Semantic().Len() + len(e.Person().Applied()),
		Note: "declarative memory is LIVE (P2.3/P6.x) — never-fabricate, relevance-gated, bi-temporal beliefs",
		Groups: []cognition.RegGroup{
			{Label: "episodic (grounded episodes)", Entries: episodes},
			{Label: "semantic (currently-valid beliefs)", Entries: beliefs},
			{Label: "person (learned preferences)", Entries: prefs},
			{Label: "working + procedural", Entries: procedural},
		},
	}
}

// clipName truncates a registry entry name to n runes for the index column.
func clipName(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// -- small static lookups + tag helpers ------------------------------------------------------------

// specialistRole maps a specialist domain to a one-line role for the browser. After the
// representation-space rebuild (M2) the roster is the REAL primitive set — five tool-backed
// (compute/recall/read/search/run, the senses+hands that carry ground truth) and two model-driven
// stance roles (skeptic/advocate, content from the LLM, with a reason). The deleted
// simulation/safety/refactor fakes + the toy MemoryKB are gone, so there are no more CANNED labels.
func specialistRole(domain string) string {
	switch domain {
	case "compute":
		return "exact computation — a real calculator (tool-backed primitive)"
	case "recall":
		return "fact recall — real retrieval over the live memory store (tool-backed)"
	case "read":
		return "reads a real file via the gated executor — grounds a claim (tool-backed)"
	case "search":
		return "greps the real tree via the gated executor — finds where (tool-backed)"
	case "run":
		return "runs the suite for real — don't guess 'it runs', read reality (tool-backed)"
	case "skeptic":
		return "flags risk with a reason — MODEL-driven stance (forks vs advocate)"
	case "advocate":
		return "argues a change is sound with a reason — MODEL-driven stance (forks vs skeptic)"
	default:
		return "minted from a repeated generated pattern"
	}
}

// skillDetail prefers the skill's own Description, falling back to a tier hint.
func skillDetail(description, tier string) string {
	if d := strings.TrimSpace(description); d != "" {
		return d
	}
	return "a " + tier + " skill"
}

// skillProgramDetail leads with the skill's PROGRAM shape (the one-line control-flow signature) then the
// description — so the browser shows what the skill RUNS, not just what it is named (the M6 detail-panel
// requirement). The shape is the load-bearing half; the prose follows in parentheses.
func skillProgramDetail(shape, description, tier string) string {
	d := skillDetail(description, tier)
	if shape = strings.TrimSpace(shape); shape != "" {
		return shape + "  —  " + d
	}
	return d
}

// triggerTags renders the first few trigger phrases of a skill as a faint tag (the goal-matcher hint).
func triggerTags(triggers []string) []string {
	if len(triggers) == 0 {
		return nil
	}
	show := triggers
	if len(show) > 3 {
		show = show[:3]
	}
	return []string{"triggers: " + strings.Join(show, " / ")}
}
