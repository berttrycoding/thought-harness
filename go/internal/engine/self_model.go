// self_model.go is the BASELINE DECLARATIVE SELF-MODEL (board SELF-MODEL; design
// docs/internal/notes/2026-06-21-preagi-harness-levels-roadmap.md §1.5). It closes the live-observed gap that in
// awake CHAT the harness lacks even baseline self-knowledge — it falls back to the bare model's "I'm an
// LLM, I can't see my cwd" prior because the conscious stream is never TOLD what it is running in.
// Self-KNOWLEDGE is BASELINE (gate the WRITE, never the UNDERSTANDING) and a prerequisite for L3
// self-direction / L4 self-improvement: you cannot improve what you do not understand.
//
// TWO TIERS — a small STANDING CORE + LAZY/RAG-retrieved DETAIL.
//
//	STANDING CORE (small, BOUNDED, always in the conscious context when grounded): the engine's IDENTITY
//	(Silent-Injection cognition harness — 3 layers Subconscious/Conscious/Action, 2 seams hidden + watched)
//	+ a bounded CAPABILITY INDEX (categories + COUNTS — "file/compute/web tools · a navigable thought graph
//	· N specialists across M domains · K operators across F families") + RUNTIME facts (mode / substrate /
//	cwd / a key config summary). The index is CONSTANT-SIZE even as the roster GROWS via minting — it
//	carries counts + category names, never the per-item detail.
//
//	LAZY / ON-DEMAND DETAIL (NOT carried in context — pulled when the stream reasons about needing it):
//	SelfModelLookup queries the REAL registries by relevance and returns one matching detail — a tool's
//	signature, a specialist's competence, an operator's intent, or a runtime fact. The capability registry
//	is a content-addressable store the thought QUERIES (the same attention-over-a-growing-store pattern as
//	the dispatch), so the bounded-but-growing roster is never eagerly dumped into context.
//
// REGISTRY-READ (the proof). The core is assembled by READING the live registries — Subconscious().
// Specialists(), the Action tool registry (Tools()), and the operator Catalog() — NOT a hardcoded list.
// Adding a specialist changes the standing index's count + domain set AND the on-demand detail; that is the
// registry-read contract (a hardcoded self-model would not move when the roster grows).
//
// WIRED STANDINGLY into the awake conscious stream (NOT resume-once). maybeGroundSelfModel runs once per
// awake tick after focus is settled (continuous.go). When the focused branch is a standing INTROSPECTIVE
// seed root, it injects the standing core as a GENERATED thought (so the stream reads it as its own
// next thought — the silent-injection voicing contract) and emits perception.self_model. It re-fires ONLY
// on a content-HASH change (a minted specialist/operator, a mode/substrate change) — so an unchanged
// self-model is grounded once, then left alone (it does not spam the stream every introspective turn).
//
// μ-BASELINE, n UNCHANGED (the durability obligation). The standing core is a SINGLE GENERATED percept
// APPEND — a μ-baseline immigrant, NOT a fork. It spawns no operator/sub-agent/branch, so it adds NO
// standing excitation source that pushes the branching ratio n→1. The #18 self-watch cell (stability.go)
// extends to PROVE n<1 with this flag on.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. selfModelOn() reads the opt-in sense.self_model knob (default OFF). With
// it OFF the awake loop never calls maybeGroundSelfModel's body, never injects a core, never emits
// perception.self_model ⇒ the live loop is byte-identical. Reactive mode never reaches stepContinuous, so
// it never grounds a self-model either way.
//
// HEADLESS-PURE + DETERMINISTIC. The core TEXT is a fixed template over read registry counts + config —
// no RNG, no backend call, no wall clock, no os.Getwd (the cwd is the configured Workspace, deterministic).
// Two identical seeded runs ground a byte-identical core.
package engine

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// selfModel is the assembled STANDING CORE — the bounded, constant-size self-description the engine grounds
// into the conscious stream. It carries the index COUNTS + category sets (the standing core), NOT the
// per-item detail (that is pulled lazily via SelfModelLookup). The Hash is the content fingerprint the
// standing-refresh keys on.
type selfModel struct {
	Specialists int      // live specialist count (Subconscious().Specialists())
	Domains     []string // the DISTINCT specialist domains, sorted (the category set, not the roster)
	Operators   int      // live operator count (Catalog().Names())
	Families    []string // the DISTINCT operator families present, sorted
	ToolCats    []string // the tool CATEGORIES present (file/shell/test/search), sorted; nil when no tools wired
	Tools       int      // live tool count (0 when no workspace/executor)
	Mode        string   // reactive | continuous
	Substrate   string   // the thinking-substrate class (test | claude | llm | …)
	Cwd         string   // the reality-facing workspace dir ("" when none — honestly reported)
	Hash        string   // the content fingerprint (FNV over the load-bearing fields) the refresh keys on
}

// selfModelOn reports whether the baseline declarative self-model is enabled (the opt-in sense.self_model
// knob, default OFF). Only meaningful in the awake/continuous loop. nil features ⇒ false (the default path
// never reads the flag, so it stays byte-identical).
func (e *Engine) selfModelOn() bool {
	return e.features != nil && e.features.Sense.SelfModel
}

// buildSelfModel ASSEMBLES the standing core by READING the live registries — the registry-read contract.
// It reads Subconscious().Specialists() (count + distinct domains), Catalog() (operator count + distinct
// families), Tools() (tool count + categories, when a workspace executor is wired), plus the runtime facts
// (mode / substrate class / configured workspace cwd). Deterministic: every read is a sorted snapshot, no
// RNG / backend / clock. The Hash fingerprints the load-bearing fields so the standing refresh fires only
// on a real change. A NEW specialist/operator changes the count + category set AND the Hash — the proof the
// model is read, not hardcoded.
func (e *Engine) buildSelfModel() selfModel {
	sm := selfModel{
		Mode:      e.mode,
		Substrate: e.SubstrateClass(),
		Cwd:       e.cfg.Workspace,
	}

	// Specialists — the silent subconscious roster (always live, even offline). Count + the DISTINCT
	// domain set (the constant-size CATEGORY index — adding a 4th compute specialist does not grow the
	// "compute" category, but a specialist in a NEW domain adds one category, exactly the bounded-but-
	// growing contract).
	if e.subconscious != nil {
		specs := e.subconscious.Specialists()
		sm.Specialists = len(specs)
		seen := map[string]bool{}
		for _, s := range specs {
			d := strings.TrimSpace(s.Domain())
			if d != "" && !seen[d] {
				seen[d] = true
				sm.Domains = append(sm.Domains, d)
			}
		}
		sort.Strings(sm.Domains)
	}

	// Operators — the generative-control catalog (always live). Count + the DISTINCT family set.
	if e.catalog != nil {
		names := e.catalog.Names()
		sm.Operators = len(names)
		seen := map[string]bool{}
		for _, n := range names {
			if fam, ok := e.catalog.Family(n); ok && fam != "" && !seen[fam] {
				seen[fam] = true
				sm.Families = append(sm.Families, fam)
			}
		}
		sort.Strings(sm.Families)
	}

	// Tools — the Action-layer real-tool surface (nil offline / no workspace; honestly reported). Count +
	// the tool CATEGORIES (a constant-size grouping of the tool names), never the per-tool signature (that
	// is the lazy detail).
	if reg := e.Tools(); reg != nil {
		names := reg.Names()
		sm.Tools = len(names)
		seen := map[string]bool{}
		for _, n := range names {
			cat := toolCategory(n)
			if !seen[cat] {
				seen[cat] = true
				sm.ToolCats = append(sm.ToolCats, cat)
			}
		}
		sort.Strings(sm.ToolCats)
	}

	sm.Hash = selfModelHashOf(sm)
	return sm
}

// toolCategory maps a tool NAME to its bounded CATEGORY for the constant-size index (file / shell / test /
// search / other). The category set stays small even as named tools are added, so the index does not grow
// with the roster — the per-tool signature is the lazy detail (SelfModelLookup), not the index.
func toolCategory(name string) string {
	switch {
	case strings.Contains(name, "file") || strings.Contains(name, "read") || strings.Contains(name, "write"):
		return "file"
	case strings.Contains(name, "shell") || strings.Contains(name, "run_shell") || strings.Contains(name, "exec"):
		return "shell"
	case strings.Contains(name, "test"):
		return "test"
	case strings.Contains(name, "search") || strings.Contains(name, "grep") || strings.Contains(name, "web"):
		return "search"
	default:
		return "other"
	}
}

// selfModelHashOf fingerprints the load-bearing fields of the standing core (FNV-1a over a stable rendering)
// so the standing refresh fires ONLY on a real content change — a minted specialist/operator/tool, or a
// mode/substrate/cwd change. Stable + deterministic (a pure read of the sorted snapshot, no clock/RNG).
func selfModelHashOf(sm selfModel) string {
	h := fnv.New64a()
	w := func(s string) { _, _ = h.Write([]byte(s)); _, _ = h.Write([]byte{0}) }
	w(strconv.Itoa(sm.Specialists))
	w(strings.Join(sm.Domains, ","))
	w(strconv.Itoa(sm.Operators))
	w(strings.Join(sm.Families, ","))
	w(strconv.Itoa(sm.Tools))
	w(strings.Join(sm.ToolCats, ","))
	w(sm.Mode)
	w(sm.Substrate)
	w(sm.Cwd)
	return strconv.FormatUint(h.Sum64(), 16)
}

// selfModelText renders the STANDING CORE as the deterministic, bounded self-description the conscious
// stream reads as its own next thought. IDENTITY first (what I am), then the CONSTANT-SIZE capability INDEX
// (categories + counts, never the per-item detail), then the runtime facts. The "(ask me for detail)"
// pointer tells the stream the per-capability detail is RETRIEVABLE on demand (SelfModelLookup), not absent
// — the lazy/RAG contract made legible.
func selfModelText(sm selfModel) string {
	var b strings.Builder
	// IDENTITY — what I am (the architecture, not the base model).
	b.WriteString("(self-model) I am the thought-harness: a Silent-Injection cognition system, not a bare model. ")
	b.WriteString("I run on three layers — Subconscious (silent specialists/dispatch/generative control), ")
	b.WriteString("Conscious (a navigable thought graph; one branch expanded, the rest compressed), and ")
	b.WriteString("Action (effortful, reality-facing) — joined by two seams: a hidden FILTER->GATE->TRANSFORM ")
	b.WriteString("intake and a watched intention/reality seam. ")
	// CAPABILITY INDEX — constant-size categories + counts (read from the live registries).
	b.WriteString("Capabilities: ")
	b.WriteString(itoa(sm.Specialists))
	b.WriteString(" specialists across ")
	b.WriteString(itoa(len(sm.Domains)))
	b.WriteString(" domains (")
	b.WriteString(joinOrNone(sm.Domains))
	b.WriteString("); ")
	b.WriteString(itoa(sm.Operators))
	b.WriteString(" operators across ")
	b.WriteString(itoa(len(sm.Families)))
	b.WriteString(" families (")
	b.WriteString(joinOrNone(sm.Families))
	b.WriteString("); ")
	if sm.Tools > 0 {
		b.WriteString(itoa(sm.Tools))
		b.WriteString(" real tools (")
		b.WriteString(joinOrNone(sm.ToolCats))
		b.WriteString("). ")
	} else {
		b.WriteString("no real tools wired this run (no workspace). ")
	}
	// RUNTIME facts — mode / substrate / cwd.
	b.WriteString("Runtime: ")
	b.WriteString(orNone(sm.Mode))
	b.WriteString(" mode on substrate ")
	b.WriteString(orNone(sm.Substrate))
	b.WriteString(", working dir ")
	b.WriteString(orNone(sm.Cwd))
	b.WriteString(". ")
	// The LAZY/RAG pointer — the per-capability detail is retrievable, not carried.
	b.WriteString("Detail (a tool's signature, a specialist's competence, a config value) is retrievable on demand, not carried here.")
	return b.String()
}

// joinOrNone renders a comma-joined category list, or "none" for an empty list (so the core reads honestly
// when a category set is empty — e.g. a degenerate roster).
func joinOrNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

// maybeGroundSelfModel is the SELF-MODEL live-wire, called once per awake tick AFTER focus is settled
// (continuous.go, alongside maybeAutonomousSense). It is a NO-OP (returns immediately) unless:
//   - the flag is ON (selfModelOn), AND
//   - the currently-focused branch is a standing INTROSPECTIVE seed root (e.branchFaculty[active] ==
//     FacultyIntrospective) — the "track my own state / what I am" standing watch, AND
//   - the assembled standing core's content HASH has CHANGED since the last grounding (the standing-refresh
//     guard — an unchanged self-model is grounded once, not re-injected every introspective focus turn).
//
// On a fire it injects the standing core as a GENERATED percept thought (so the stream reads it as its own
// next thought — the silent-injection voicing contract) and emits perception.self_model carrying the index
// counts + runtime facts (the witness on the bus). It is a SINGLE append (a μ-baseline immigrant, no fork),
// so it does not raise the branching plant n. So the default/reactive path is byte-identical (the body
// never runs) and the on path adds one standing percept per content-change, not per tick.
func (e *Engine) maybeGroundSelfModel(tick int) {
	if !e.selfModelOn() || e.graph == nil || len(e.branchFaculty) == 0 {
		return
	}
	bid := e.graph.ActiveBranch
	fac, tagged := e.branchFaculty[bid]
	if !tagged || fac != cognition.FacultyIntrospective {
		return // only a standing INTROSPECTIVE root grounds the self-model (the "what am I" watch)
	}

	sm := e.buildSelfModel()
	// STANDING-REFRESH guard: an unchanged self-model is grounded once, then left alone. Re-fire ONLY when
	// the content hash changed (a minted specialist/operator/tool, or a mode/substrate/cwd change).
	if sm.Hash == e.selfModelHash {
		return
	}
	e.selfModelHash = sm.Hash

	name := seedRootName(e, bid)
	text := selfModelText(sm)

	// Inject the standing core as a GENERATED thought — the mind's own next thought re-grounding on what it
	// is. A moderate confidence (0.6) marks it as a grounded re-orientation (it is READ from the real
	// registries + config, not invented), the same level orientOnce / autonomous-sense use. A single append
	// (a μ-baseline immigrant), NOT a fork — so it does not raise the branching plant n.
	e.appendThought(&types.Thought{
		ID:         -1,
		Text:       text,
		Source:     types.GENERATED,
		Confidence: 0.6,
	}, tick)

	// The witness on the bus — the index counts + runtime facts, so the grounding is visible + testable.
	e.bus.Emit(events.PerceptionSelfModel,
		"self-model grounded "+orNoneShort(name)+": "+itoa(sm.Specialists)+" specialists / "+
			itoa(sm.Operators)+" operators / "+itoa(sm.Tools)+" tools, "+orNone(sm.Mode)+" on "+orNone(sm.Substrate),
		events.D{
			"tick":        tick,
			"branch":      bid,
			"name":        name,
			"hash":        sm.Hash,
			"specialists": sm.Specialists,
			"domains":     sm.Domains,
			"operators":   sm.Operators,
			"families":    sm.Families,
			"tools":       sm.Tools,
			"tool_cats":   sm.ToolCats,
			"mode":        sm.Mode,
			"substrate":   sm.Substrate,
			"cwd":         sm.Cwd,
		})
}

// SelfModelLookup is the LAZY / ON-DEMAND DETAIL retrieval — the RAG half of the self-model. The standing
// core carries the bounded INDEX (categories + counts); the per-capability DETAIL is NOT carried — it is
// PULLED here when the conscious stream (or the TUI / a test) reasons about needing it. It queries the REAL
// registries BY RELEVANCE to the query and returns the single best-matching detail line + ok, or ("",false)
// on a miss (never a fabricated answer — the never-fabricate floor):
//   - a TOOL's signature (name + description + parameter names), when the query matches a tool;
//   - a SPECIALIST's competence (its domain), when the query matches a specialist domain;
//   - an OPERATOR's intent + family, when the query matches an operator;
//   - a RUNTIME fact (mode / substrate / cwd / counts), when the query matches a runtime term.
//
// This is the "attention over a growing store" the design calls for: the capability registry is a content-
// addressable store the thought queries on demand, so a bounded-but-GROWING roster (minting) is never
// eagerly dumped into context. v1 is a simple relevance-ranked registry lookup (lexical overlap); full
// knowledge-layer RAG integration (internal/retrieval + the sourcing ladder) is a follow-up. Deterministic:
// a pure read of the sorted registry snapshots + the config, no RNG / backend / clock.
func (e *Engine) SelfModelLookup(query string) (string, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "", false
	}
	terms := strings.Fields(q)

	type hit struct {
		score  int
		detail string
	}
	best := hit{}
	consider := func(score int, detail string) {
		if score > best.score {
			best = hit{score: score, detail: detail}
		}
	}

	// TOOLS — a tool's signature (name + description + parameter names). Only when an executor is wired.
	if reg := e.Tools(); reg != nil {
		for _, t := range reg.List() {
			hay := strings.ToLower(t.Name() + " " + t.Description())
			score := overlapScore(terms, hay)
			if score > 0 {
				params := paramNames(t.Parameters())
				detail := "tool " + t.Name() + ": " + oneLine(t.Description())
				if params != "" {
					detail += " (params: " + params + ")"
				}
				consider(score, detail)
			}
		}
	}

	// SPECIALISTS — a specialist's competence (its domain). Domain is the relevance key.
	if e.subconscious != nil {
		for _, s := range e.subconscious.Specialists() {
			d := strings.TrimSpace(s.Domain())
			if d == "" {
				continue
			}
			score := overlapScore(terms, strings.ToLower(d))
			if score > 0 {
				consider(score, "specialist domain "+d+": a silent subconscious primitive that fires on relevance to "+d+" context")
			}
		}
	}

	// OPERATORS — an operator's intent + family.
	if e.catalog != nil {
		for _, n := range e.catalog.Names() {
			spec, ok := e.catalog.Get(n)
			if !ok {
				continue
			}
			hay := strings.ToLower(spec.Name + " " + spec.Intent + " " + spec.Family)
			score := overlapScore(terms, hay)
			if score > 0 {
				consider(score, "operator "+spec.Name+" ["+spec.Family+"]: "+oneLine(spec.Intent))
			}
		}
	}

	// RUNTIME facts — mode / substrate / cwd / counts, on a matching runtime term.
	runtimeFacts := []struct{ key, detail string }{
		{"mode reactive continuous awake", "runtime mode: " + orNone(e.mode)},
		{"substrate backend model claude test llm", "thinking substrate: " + orNone(e.SubstrateClass())},
		{"cwd workspace directory dir path where running", "working directory: " + orNone(e.cfg.Workspace)},
	}
	for _, rf := range runtimeFacts {
		score := overlapScore(terms, rf.key)
		if score > 0 {
			consider(score, rf.detail)
		}
	}

	if best.score == 0 {
		return "", false
	}
	return best.detail, true
}

// overlapScore counts how many query terms appear in the haystack (lexical relevance — the v1 registry
// lookup). 0 ⇒ no relevance (a miss). A pure, deterministic read.
func overlapScore(terms []string, haystack string) int {
	score := 0
	for _, t := range terms {
		if len(t) >= 3 && strings.Contains(haystack, t) {
			score++
		}
	}
	return score
}

// paramNames renders a tool's parameter NAMES from its JSON-schema-ish Parameters() map (the
// {properties: {name: …}} shape), comma-joined + sorted, for the on-demand signature. "" when no named
// properties are present. A pure read — no RNG/clock.
func paramNames(params map[string]any) string {
	if params == nil {
		return ""
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return ""
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// oneLine collapses a (possibly multi-line) description to a single bounded line for the on-demand detail.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return runeSlice(s, 160)
}

// ---------------------------------------------------------------------------
// SELF-MODEL -> the REPLY (the gap fix): wire the grounded self-knowledge into the answer.
//
// THE LIVE GAP (TestLiveClaudeSelfModelGroundsWhatItIs FAILED). The standing-core self-model IS grounded
// into the stream (maybeGroundSelfModel injects it + emits perception.self_model), but the RESPOND path
// ignored it: when the user asked "what are you / what can you do / where are you running", the answer
// fell back to the bare-model prior ("I'm Claude, an AI assistant made by Anthropic …") instead of the
// harness identity/architecture/tools the self-model holds. The self-model thought was in the GRAPH but
// the Respond context (the active LINE the user's turn opened) did not carry it, so the model never saw
// the grounded self-knowledge when it composed the reply.
//
// THE FIX. deliverResponse folds the standing-core self-model TEXT + a targeted SelfModelLookup for the
// question into the RESPOND context — but ONLY when it is RELEVANT (an identity / self / capability /
// location question), so a normal answer is NOT bloated with self-description on every reply. The
// self-model thought rides the SAME context slice the responder already reads (workingContext ->
// Respond), so it reaches EVERY backend's prompt (llm / claude / test) by construction — no backend-
// specific optional interface — which is exactly what makes it provable offline against the test double.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. selfModelReplyContext returns the context UNCHANGED unless the
// sense.self_model knob is ON, so the default/reactive RESPOND prompt is byte-identical and the scenario
// goldens are untouched. CONTENT-PATH, not a new fork: it appends one read-only context unit to the slice
// the responder consumes (no graph mutation, no operator/sub-agent/branch), so it does not move the plant
// (n unchanged) — it is the μ-baseline self-model made legible in the reply.

// selfModelReplyRelevant is the RELEVANCE GATE: it reports whether THIS reply should carry the grounded
// self-model. True only for an IDENTITY / SELF / CAPABILITY / LOCATION question (what/who am I, what can
// I do, where am I running, etc.) — so a normal answer is never bloated with a self-description. It reads
// the active goal (the user's question, e.graph.Goal — set to the user turn even mid-wander, continuous.go)
// plus the active context: a question is self-directed when it pairs a self-reference (you / your / I / this
// system / harness) with an identity/capability/location term. Pure + deterministic (a lexical read, no
// RNG / clock / backend).
func (e *Engine) selfModelReplyRelevant(ctx []types.Thought) bool {
	if e.graph == nil {
		return false
	}
	q := strings.ToLower(e.graph.Goal)
	// A self-reference: the question must be ABOUT this system, not a third-party "what is X".
	selfRef := strings.Contains(q, "you") || strings.Contains(q, "your") ||
		strings.Contains(q, " i ") || strings.HasPrefix(q, "i ") ||
		strings.Contains(q, " am i") || strings.Contains(q, "yourself") ||
		strings.Contains(q, "this system") || strings.Contains(q, "this harness") ||
		strings.Contains(q, "harness")
	if !selfRef {
		return false
	}
	// An identity / capability / location facet: "what/who are you", "what can you do", "where are you
	// running", "what tools do you have", "how do you work", etc.
	return strings.Contains(q, "what are you") || strings.Contains(q, "who are you") ||
		strings.Contains(q, "what can you") || strings.Contains(q, "what do you do") ||
		strings.Contains(q, "what can i do") || strings.Contains(q, "where are you") ||
		strings.Contains(q, "where am i") || strings.Contains(q, "where do you run") ||
		strings.Contains(q, "running") || strings.Contains(q, "capabilit") ||
		strings.Contains(q, "what tools") || strings.Contains(q, "your tools") ||
		strings.Contains(q, "how do you work") || strings.Contains(q, "how are you built") ||
		strings.Contains(q, "what kind of") || strings.Contains(q, "identity") ||
		strings.Contains(q, "describe yourself") || strings.Contains(q, "tell me about yourself")
}

// selfModelReplyContext is the WIRE: it returns the RESPOND context the responder should see. With the
// flag OFF — or when the question is not self-directed (the relevance gate) — it returns ctx UNCHANGED
// (byte-identical). When ON and relevant it PREPENDS one GENERATED self-model context unit carrying the
// standing-core text (the harness identity + the bounded capability index + runtime facts, read live from
// the registries) PLUS a targeted SelfModelLookup detail for the facet asked, so the model answers FROM
// the grounded self-knowledge instead of its bare-model prior. It emits perception.self_model_reply so the
// fold is observable (never silent). A read-only context append (no graph mutation, no fork).
func (e *Engine) selfModelReplyContext(ctx []types.Thought) []types.Thought {
	if !e.selfModelOn() || !e.selfModelReplyRelevant(ctx) {
		return ctx // default / not-self-directed ⇒ unchanged (byte-identical)
	}
	sm := e.buildSelfModel()
	text := selfModelText(sm)
	// A targeted on-demand DETAIL for the specific facet asked (the lazy/RAG half): if the question
	// matches a tool / specialist / operator / runtime fact, fold the single best detail in too, so the
	// answer can be specific (e.g. the real cwd, a tool's signature) and not only the bounded index.
	if detail, ok := e.SelfModelLookup(e.graph.Goal); ok {
		text += " " + detail + "."
	}
	// Prepend the self-model unit so the responder reads it FIRST (the grounding it answers from), ahead of
	// the active line. A GENERATED, high-confidence read-only unit (it is read from the real registries +
	// config, not invented) — id -1 marks it as not a graph node (it never enters the graph).
	core := types.Thought{ID: -1, Text: text, Source: types.GENERATED, Confidence: 0.9}
	out := make([]types.Thought, 0, len(ctx)+1)
	out = append(out, core)
	out = append(out, ctx...)

	e.bus.Emit(events.PerceptionSelfModelReply,
		"self-model folded into the reply (identity/capability question): "+itoa(sm.Specialists)+" specialists / "+
			itoa(sm.Operators)+" operators / "+itoa(sm.Tools)+" tools, "+orNone(sm.Mode)+" on "+orNone(sm.Substrate),
		events.D{
			"goal":        runeSlice(e.graph.Goal, 120),
			"specialists": sm.Specialists,
			"operators":   sm.Operators,
			"tools":       sm.Tools,
			"mode":        sm.Mode,
			"substrate":   sm.Substrate,
			"cwd":         sm.Cwd,
		})
	return out
}
