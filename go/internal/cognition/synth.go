// synth.go ports synth.py — the synthesiser. Construct a workflow PROGRAM on the fly, and mint NEW
// operators as needed.
//
// This is the SUBCONSCIOUS control layer made *generative* (the cap-op wiki's
// concept-dynamic-rule-synthesis: the system as toolmaker, not just tool-user). Given a goal +
// context it either:
//
//   - asks the backend to WRITE a program (the LLM path — toolmaker): a control-flow tree of
//     operators, plus any brand-new operators (name + family + intent) the problem needs; or
//   - falls back to a deterministic heuristic that recognises the SHAPE of the request and assembles
//     a matching program (series / parallel / loop).
//
// Every synthesised operator is Minted into the open catalog only after it verifies (the two-layer
// discipline — never trust a synthesised rule on the model's word). The whole program is then
// structurally VerifyProgram-ed before it is trusted; an invalid program is dropped and the heuristic
// shape is used instead. Each synthesis emits a structured, logged event so the construction +
// definitions are captured as data to later standardise and train on.
//
// HARD PORT #4 (the synthesiser). The 3-strategy fallback (skill-match -> LLM toolmaker -> heuristic
// shape) keeps the EXACT Python order, with verify-before-trust after each: mint+verify each returned
// operator FIRST so the program's operators resolve, THEN NodeFromDict parses the tree, THEN
// VerifyProgram vets it. recognize_shape is the keyword->Program ordered if-ladder (specific shapes
// before the broad design/build shape). RecognizeShapeDict is the adapter the engine wires into
// TestBackend.ShapeRecognizer — the Go break for Python's lazy `from .synth import
// recognize_shape`.
package cognition

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// SynthResult is the verified program a synthesis produced, plus its provenance. Mirrors the Python
// @dataclass SynthResult; Minted defaults to an empty list, Source defaults to "heuristic".
type SynthResult struct {
	Program Program
	Minted  []OperatorSpec // operators minted during this synthesis (nil/empty if none)
	Source  string         // "llm" | "heuristic" | "skill:<name>" | "canonical"; default "heuristic"
}

// goalText returns the lowercased text to match a shape against: the goal, or — when the goal is empty
// — the joined context-thought text. Mirrors Python _goal_text:
// `(goal or " ".join(t.text for t in ctx)).lower()`.
func goalText(goal string, ctx []types.Thought) string {
	if goal == "" {
		parts := make([]string, len(ctx))
		for i, t := range ctx {
			parts[i] = t.Text
		}
		return strings.ToLower(strings.Join(parts, " "))
	}
	return strings.ToLower(goal)
}

// domainEntry is one row of the _domain table: a domain tag and its keyword set.
type domainEntry struct {
	domain   string
	keywords []string
}

// domainTable is the rough domain-tag table for runtime sub-agents (least-context routing, not a hard
// class). Order preserved from Python _domain: the FIRST entry whose any-keyword matches wins.
var domainTable = []domainEntry{
	{"code", []string{"code", "function", "api", "endpoint", "bug", "python", "runtime", "compile"}},
	{"math", []string{"calculate", "number", "sum", "product", "equation", "probability"}},
	{"safety", []string{"safe", "risk", "secure", "break", "regress", "danger"}},
	{"planning", []string{"plan", "schedule", "design", "architect", "roadmap", "steps"}},
}

// KnownDomains returns the canonical domain tags (the domainTable tags + the "general" catch-all), in
// table order with "general" last. It is the single source of the known-domain vocabulary so a derived
// view (e.g. the legibility tag contract, design/05-LEGIBLE-GENERATION.md §5a) cannot drift from the
// domains the synthesiser actually routes on. Any domain not in this set is "other" to that contract.
func KnownDomains() []string {
	out := make([]string, 0, len(domainTable)+1)
	for _, e := range domainTable {
		out = append(out, e.domain)
	}
	out = append(out, "general")
	return out
}

// domainOf returns a rough domain tag for the given (already-lowercased) text. Mirrors Python _domain:
// the first table entry whose any-keyword is a substring of text wins; else "general".
func domainOf(text string) string {
	for _, e := range domainTable {
		for _, k := range e.keywords {
			if strings.Contains(text, k) {
				return e.domain
			}
		}
	}
	return "general"
}

// -- deterministic shape recognition (the heuristic fallback) ---------------- //

// RecognizeShape maps a request to a control-flow program. Returns (nil, false) when no workflow shape
// applies (simple Q&A handled directly by specialists). Demonstrates series, parallel, and loop
// composition. Mirrors Python recognize_shape(goal, ctx) -> Program | None — the bool replaces None.
//
// Order matters: specific shapes are matched BEFORE the broad design/build shape, and the design/build
// keywords are intent-phrases (not bare 'build'/'api') so simple Q&A -> (nil, false).
//
// This is the byte-identical, flag-OFF entry: it delegates to recognizeShape with webLookup=false, so no
// lookup-research shape is ever produced — the legacy ladder every golden anchors. The web-aware variant
// is RecognizeShapeWeb (the subconscious.web_search ON path).
func RecognizeShape(goal string, ctx []types.Thought) (*Program, bool) {
	return recognizeShape(goal, ctx, false)
}

// RecognizeShapeWeb is the subconscious.web_search ON variant of RecognizeShape: when webLookup is true
// AND the goal is a factual-LOOKUP QUESTION that hits no more-specific shape AND names no local file, it
// produces a RESEARCH->ANSWER program (Seq(expose-affordances, generate)) that STAFFS the expose-
// affordances lookup operator — the operator the engine granted the web_search tool when the flag is on
// (engine.go GrantToolScope). The staffed expose-affordances sub-agent then dispatches web_search and
// folds the snippet into grounding (subagent.floorToolCall web_search branch). With webLookup=false it is
// byte-identical to RecognizeShape (the lookup branch is unreachable).
//
// CALIBRATION (do not trade over-staffing for the under-staffing bug): the lookup shape fires ONLY for an
// interrogative, no-local-file goal that matched NONE of the specific shapes above — i.e. a plain
// external-fact QUESTION (the HotpotQA-fullwiki / GAIA-lookup case), not every goal. A statement, a
// local-file task, or a goal that already hit another shape never reaches the lookup branch, so the known
// GAIA-L1 over-grounding regime is NOT widened (the engagement "answer-vs-lookup" gate is a separate,
// out-of-scope concern). Default-OFF (webLookup=false) ⇒ byte-identical.
func RecognizeShapeWeb(goal string, ctx []types.Thought, webLookup bool) (*Program, bool) {
	return recognizeShape(goal, ctx, webLookup)
}

// recognizeShape is the shared implementation: webLookup gates the trailing lookup-research shape (the
// most permissive branch). webLookup=false ⇒ the legacy ladder ending in (nil, false), byte-identical.
func recognizeShape(goal string, ctx []types.Thought, webLookup bool) (*Program, bool) {
	t := goalText(goal, ctx)
	dom := domainOf(t)
	has := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(t, k) {
				return true
			}
		}
		return false
	}

	if has("compare", "contrast", "versus", " vs ", "trade-off", "tradeoff", "which is better",
		"which one") {
		// decompose, then weigh similarities AND differences in PARALLEL, then rank
		root := NewSeq(
			NewStep("decompose", dom, "find the dimensions"),
			NewPar(
				NewStep("compare", dom, "what they share"),
				NewStep("contrast", dom, "where they differ"),
			),
			NewStep("rank", dom, "order by the criterion"),
		)
		p := Program{Root: root, Goal: goal, Synthesized: true,
			Rationale: "comparison shape -> parallel compare||contrast then rank"}
		return &p, true
	}

	if has("optimize", "optimise", "improve", "refine", "tune", "make it faster", "minimise",
		"minimize", "maximise", "maximize", "speed up") {
		// refine in a bounded LOOP until good enough
		root := NewLoop(
			NewSeq(
				NewStep("measure", dom, "score the current candidate"),
				NewStep("eliminate", dom, "drop the weakest part"),
			),
			"good enough", 3,
		)
		p := Program{Root: root, Goal: goal, Synthesized: true,
			Rationale: "optimisation shape -> loop(measure>eliminate)"}
		return &p, true
	}

	if has("analyze", "analyse", "investigate", "diagnose", "why is", "why are", "why does",
		"root cause", "debug") {
		root := NewSeq(
			NewStep("decompose", dom, "break the question down"),
			NewStep("hypothesize", dom, "propose an explanation"),
			NewStep("measure", dom, "test it against evidence"),
		)
		p := Program{Root: root, Goal: goal, Synthesized: true,
			Rationale: "analysis shape -> decompose>hypothesize>measure"}
		return &p, true
	}

	if has("design", "implement", "architect", "build a", "build an", "create a", "create an",
		"scaffold", "an api", "a service", "a system") {
		// the canonical design-build-validate (series) — spec scenario S6. Domain is the content
		// domain (dom), consistent with the other shapes; the per-phase Gate bias is keyed by the
		// operator name, not by the sub-agent's domain.
		root := NewSeq(
			NewStep("decompose", dom, "split into parts"),
			NewStep("generate", dom, "draft each part"),
			NewStep("validate", dom, "check it holds together"),
		)
		p := Program{Root: root, Goal: goal, Synthesized: true,
			Rationale: "design/build shape -> decompose>generate>validate"}
		return &p, true
	}

	// LOOKUP-RESEARCH shape (subconscious.web_search ON only): the under-staffing fix. A factual external-
	// fact QUESTION that hit none of the specific shapes above (so no comparison/analysis/design program
	// staffs a worker) gets a RESEARCH->ANSWER program that STAFFS expose-affordances — the lookup operator
	// the engine granted the web_search tool. Without this, such a goal recognised NO shape, the engine
	// staffed NO sub-agent, and web_search never had a turn (the measured ~1/N firing on lookup benchmarks).
	// expose-affordances researches (dispatches web_search, folds the snippet); generate answers from it.
	// Gated on isLookupQuestion (interrogative + no local file) so it does NOT over-staff on statements,
	// local-file tasks, or non-question goals — the GAIA-L1 over-grounding regime is left untouched.
	if webLookup && isLookupQuestion(goal, t) {
		root := NewSeq(
			NewStep("expose-affordances", dom, "research the external facts the question needs"),
			NewStep("generate", dom, "answer from what the lookup surfaced"),
		)
		p := Program{Root: root, Goal: goal, Synthesized: true,
			Rationale: "lookup shape -> expose-affordances(web_search research)>generate(grounded answer)"}
		return &p, true
	}

	return nil, false // no workflow shape — simple Q&A handled directly by specialists
}

// isLookupQuestion reports whether goal is a factual-LOOKUP QUESTION whose answer needs EXTERNAL retrieval
// — the only goal shape the lookup-research branch staffs. It is the calibration that keeps expose-
// affordances from being staffed indiscriminately (the GAIA-L1 over-grounding risk): a goal qualifies iff
//
//	(1) it names NO concrete LOCAL file (a local-file task reads the file, not the web — synthFilePath),
//	    and
//	(2) it READS as a question — it ends with "?" OR opens with a factual interrogative lead
//	    (who/what/when/where/which/whose/whom/why/how, or an auxiliary lead did/was/were/is/are/does/do/
//	    has/have/can/could/will/would/should/name/list the / how many|much).
//
// goal is the raw goal; lc is its lowercased form (goalText, already computed by the caller). A statement,
// a command without a question, or a local-file task returns false ⇒ no lookup shape ⇒ the goal falls
// through to (nil, false) exactly as before.
func isLookupQuestion(goal, lc string) bool {
	g := strings.TrimSpace(goal)
	if g == "" {
		return false
	}
	// (1) a concrete local-file target wins the read (the precedence guard floorToolCall also honours): a
	// goal naming a file is NOT an external-fact lookup.
	if synthFilePath(g) != "" {
		return false
	}
	// (2a) a literal question mark anywhere is the strongest signal.
	if strings.Contains(g, "?") {
		return true
	}
	// (2b) else an interrogative / lookup-imperative lead word.
	l := strings.TrimSpace(strings.ToLower(lc))
	first := l
	if i := strings.IndexByte(l, ' '); i > 0 {
		first = l[:i]
	}
	switch first {
	case "who", "what", "when", "where", "which", "whose", "whom", "why", "how",
		"did", "was", "were", "is", "are", "does", "do", "has", "have",
		"can", "could", "will", "would", "should":
		return true
	}
	// a few lookup-imperative leads that are not interrogatives ("name the …", "list the …", "find out …",
	// "look up …") — explicit retrieval requests, still no local file.
	if strings.HasPrefix(l, "name the ") || strings.HasPrefix(l, "list the ") ||
		strings.HasPrefix(l, "find out ") || strings.HasPrefix(l, "look up ") {
		return true
	}
	return false
}

// programStaffsExposeAffordances reports whether any step in the program is the expose-affordances lookup
// operator — the operator the engine granted the web_search tool (engine.go GrantToolScope). It is the
// idempotence guard for the lookup-FORCE (step 3 of synthesize): a program that already staffs expose-
// affordances (the step-2 RecognizeShapeWeb shape, an RPIV program, or a model program that did include it)
// is NOT double-prepended. It walks the resolved tree via Program.Steps (the same walk VerifyProgram uses).
func programStaffsExposeAffordances(p Program) bool {
	for _, s := range p.Steps() {
		if s.Operator == "expose-affordances" {
			return true
		}
	}
	return false
}

// synthFilePath is the cognition-tier file-path detector for the lookup-question calibration: the first
// whitespace token whose SHAPE is a real local-file path. It mirrors subconscious.filePath (the
// floorToolCall precedence guard) but lives here because cognition is a lower tier than subconscious and
// cannot import it — keeping the lookup gate and the dispatch precedence guard agreeing on what "names a
// local file" means.
//
// FIX (#35, the slash false-positive). The old detector treated ANY token containing "/" as a path, so a
// bare "word/word" inside a goal — "yes/no", "and/or", "either/or", a date "12/25", a ratio "3/4", a URL
// "http://example.com" — read as a local file. That made isLookupQuestion return false (a question with a
// slash looked like a local-file task, so the web lookup was suppressed) and could mis-route the local-file
// read precedence onto a non-path. A real path now REQUIRES a recognized file EXTENSION on the leaf
// (looksLikePath): "dir/file.go", "config.json", "./x/y.py" still qualify; "yes/no", "and/or", "12/25",
// "3/4", "http://host/page" do not.
func synthFilePath(text string) string {
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, "\"'`.,;:()[]{}")
		if looksLikePath(w) {
			return w
		}
	}
	return ""
}

// pathExts is the recognized local-file extension set a path-shaped leaf must carry. It mirrors the
// curated extension list in action.selector (filePathRe) so the cognition lookup gate, the subconscious
// dispatch precedence guard, and the shared SelectTool agree on what a local file looks like. A bare
// "word/word" slash with no recognized extension is NOT a path.
var pathExts = map[string]bool{
	"go": true, "py": true, "yaml": true, "yml": true, "md": true, "txt": true,
	"json": true, "toml": true, "sh": true, "js": true, "ts": true, "rs": true,
	"c": true, "h": true, "cpp": true, "java": true, "rb": true, "cfg": true,
	"ini": true, "conf": true, "env": true, "mod": true, "sum": true, "lock": true,
}

// looksLikePath reports whether a (punctuation-trimmed) whitespace token has the SHAPE of a real local-
// file path: a leaf with a recognized file extension (looksLikeFileLeaf), OR a path containing a separator
// whose final segment is such a leaf. A bare "word/word" slash, a URL, a date "12/25", or a ratio "3/4"
// are NOT paths (their leaf has no recognized extension). The single source of the path-shape rule for
// #35; the subconscious twin (filePath) calls identical logic over its own copy of the rule.
func looksLikePath(w string) bool {
	if w == "" {
		return false
	}
	// A URL scheme ("http://", "https://", "ftp://") is never a local file.
	if strings.Contains(w, "://") {
		return false
	}
	leaf := w
	if i := strings.LastIndexByte(w, '/'); i >= 0 {
		leaf = w[i+1:]
	}
	return looksLikeFileLeaf(leaf)
}

// looksLikeFileLeaf reports whether a leaf (a single path segment, no '/') is a filename with a recognized
// extension: a '.' that is not trailing and a recognized extension in pathExts. "file.go" / "config.json"
// qualify; "yes" / "no" / "12" / "README" (no recognized ext) do not. (The caller's punctuation-trim
// strips a surrounding '.', so a bare dotfile leaf like ".env" arrives as "env" and is NOT treated as a
// path — pre-existing trim behaviour, unchanged by #35.)
func looksLikeFileLeaf(leaf string) bool {
	i := strings.LastIndexByte(leaf, '.')
	if i < 0 || i == len(leaf)-1 {
		return false // no dot, or trailing dot (no extension)
	}
	ext := strings.ToLower(leaf[i+1:])
	return pathExts[ext]
}

// RecognizeShapeDict is the adapter wired into TestBackend.ShapeRecognizer (the Go break for
// Python's lazy `from .synth import recognize_shape`). It runs RecognizeShape and returns the program
// serialised to the raw {goal, synthesized, rationale, root} dict (Program.ToDict()), so the backend's
// SynthesizeProgram can return the same raw map[string]any a model would. (nil, false) when no shape
// applies. Its signature matches backends.ShapeRecognizer exactly.
func RecognizeShapeDict(goal string, ctx []types.Thought) (map[string]any, bool) {
	p, ok := RecognizeShape(goal, ctx)
	if !ok {
		return nil, false
	}
	return p.ToDict(), true
}

// RecognizeShapeWebDict is the web-aware adapter (subconscious.web_search ON): the same {goal, synthesized,
// rationale, root} serialisation as RecognizeShapeDict but over RecognizeShapeWeb(goal, ctx, true), so the
// test double's SynthesizeProgram (step 1 of Synthesize) ALSO produces the lookup-research program for a
// factual question — not only the heuristic fallback (step 2). The engine wires this onto
// TestBackend.ShapeRecognizer in place of RecognizeShapeDict only when the flag is on, so the OFF path
// keeps RecognizeShapeDict and stays byte-identical. Its signature matches backends.ShapeRecognizer.
func RecognizeShapeWebDict(goal string, ctx []types.Thought) (map[string]any, bool) {
	p, ok := RecognizeShapeWeb(goal, ctx, true)
	if !ok {
		return nil, false
	}
	return p.ToDict(), true
}

// canonicalDesignBuildValidate is the fail-safe canonical program (Python
// canonical_design_build_validate). Used only when a heuristic shape somehow fails verification (which
// should never happen). synthesized=false — it is a template, not a bespoke synthesis.
func canonicalDesignBuildValidate(goal string) Program {
	root := NewSeq(
		NewStep("decompose", "decompose", ""),
		NewStep("generate", "build", ""),
		NewStep("validate", "validate", ""),
	)
	return Program{Root: root, Goal: goal, Synthesized: false, Rationale: "canonical design-build-validate"}
}

// Synthesize constructs a verified program for this goal, minting new operators as needed. It returns
// (nil, false) when no workflow shape applies (the caller handles the goal directly). Mirrors Python
// synthesize(goal, ctx, catalog, backend, emit=None, library=None) -> SynthResult | None.
//
// Order (kept exactly): (0) MATCH a library skill — a named, reusable Program; else (1) the LLM
// toolmaker path; else (2) the deterministic heuristic shape.
//
// emit may be nil (Python's `emit=None`); library may be nil (Python's `library=None`). The backend is
// the core Backend interface — its SynthesizeProgram is the toolmaker entry, returning the raw program
// dict (or ok=false to defer to the heuristic shape).
//
// This is the byte-identical, flag-OFF entry: webLookup=false, so the step-2 heuristic fallback never
// produces a lookup-research shape. SynthesizeWeb is the subconscious.web_search ON variant.
func Synthesize(goal string, ctx []types.Thought, catalog *OperatorRegistry, backend backends.Backend,
	emit events.Emit, library *SkillRegistry) (*SynthResult, bool) {
	return synthesize(goal, ctx, catalog, backend, emit, library, false)
}

// SynthesizeWeb is the subconscious.web_search ON variant of Synthesize: when webLookup is true, the
// step-2 heuristic fallback (RecognizeShapeWeb) may produce a lookup-research program that STAFFS expose-
// affordances for a factual question that hit no other shape — the live-claude safety net for when the
// model declines a program (the test-double step-1 path is covered by wiring RecognizeShapeWebDict onto
// the recogniser). webLookup=false ⇒ byte-identical to Synthesize.
func SynthesizeWeb(goal string, ctx []types.Thought, catalog *OperatorRegistry, backend backends.Backend,
	emit events.Emit, library *SkillRegistry, webLookup bool) (*SynthResult, bool) {
	return synthesize(goal, ctx, catalog, backend, emit, library, webLookup)
}

// synthesize is the shared implementation. webLookup gates ONLY the step-2 heuristic fallback's lookup-
// research shape; steps 0 (skill match) and 1 (LLM toolmaker) are unaffected.
func synthesize(goal string, ctx []types.Thought, catalog *OperatorRegistry, backend backends.Backend,
	emit events.Emit, library *SkillRegistry, webLookup bool) (*SynthResult, bool) {
	minted := []OperatorSpec{}
	var result *SynthResult

	// 0. Skill match: a goal that matches a library skill runs that skill's (expanded) Program.
	if library != nil {
		if skill, found := library.Match(goal); found {
			prog, err := library.Expand(skill) // resolve sub-skills (bounded, acyclic)
			ok := err == nil
			if ok {
				if okVerify, _ := VerifyProgram(prog, catalog); !okVerify {
					ok = false
				}
			}
			if ok {
				if emit != nil {
					emit(events.SkillMatch,
						"goal matched skill '"+skill.Name+"': "+prog.Shape(),
						events.D{
							"skill":      skill.Name,
							"tier":       skill.Tier,
							"shape":      prog.Shape(),
							"sub_skills": skill.SubSkills(),
							"program":    prog.ToDict(),
						})
				}
				res := &SynthResult{Program: prog, Minted: minted, Source: "skill:" + skill.Name}
				if emit != nil {
					emit(events.SubSynthesize,
						"synthesised program (skill:"+skill.Name+"): "+prog.Shape(),
						events.D{
							"shape":     prog.Shape(),
							"source":    res.Source,
							"rationale": prog.Rationale,
							"program":   prog.ToDict(),
							"minted":    []string{},
						})
				}
				return res, true
			}
		}
	}

	// 1. LLM toolmaker path: the backend writes the program (+ any new operators). SynthesizeProgram
	// is a core Backend method (Python `hasattr(backend, "synthesize_program")` is always true now —
	// every backend declares it; ok=false is the "defer to the heuristic shape" signal).
	//
	// W3 synthesiser catalog curation: offer a goal-scored, bounded SUBSET to the synthesis prompt
	// instead of the whole catalog. FLAG-GATED, DEFAULT OFF: SynthOfferCap defaults to 0
	// (THOUGHT_SYNTH_CATALOG_TOPK unset) ⇒ Offer returns the whole catalog == the legacy behaviour,
	// byte-identical. A positive cap turns curation ON. The dedicated SubCatalogOffer event fires ONLY
	// when the subset is actually shaped (curation ON and the catalog exceeds the cap), so the default
	// whole-catalog path stays silent and every golden holds.
	catalogTotal := len(catalog.Names())
	offered := catalog.Offer(goal, SynthOfferCap)
	if emit != nil && len(offered) < catalogTotal {
		// W3-S3: surface the retrieval CHANNEL on the event so a live run that silently fell back to
		// lexical (no embeddings server) is observable, never mistaken for active semantic curation. The
		// event fires only when the cap actually narrowed, so the cap is engaged here (capEngages=true):
		// "semantic" (embedder wired) | "lexical" (no embedder). This is an ADDITIVE data field — the five
		// W3-S1 fields below are unchanged, so no event-vocab gate bump (no new Kind).
		emit(events.SubCatalogOffer,
			"offered "+itoa(len(offered))+" of "+itoa(catalogTotal)+" operators (top-k="+itoa(SynthOfferCap)+", "+catalog.RetrieverMode(true)+")",
			events.D{
				"offered":       append([]string(nil), offered...),
				"count":         len(offered),
				"catalog_total": catalogTotal,
				"top_k":         SynthOfferCap,
				"goal":          goal,
				"mode":          catalog.RetrieverMode(true),
			})
	}
	if spec, ok := backend.SynthesizeProgram(goal, ctx, offered); ok && spec != nil {
		// mint+verify any new operators FIRST so the program's operators resolve.
		for _, od := range operatorsOf(spec) {
			name := dictStr(od, "name", "")
			family := dictStr(od, "family", "synthesized")
			intent := dictStr(od, "intent", "")
			if m, minted2 := catalog.Mint(name, family, intent); minted2 {
				minted = append(minted, m)
				if emit != nil {
					emit(events.SubOperator,
						"minted operator '"+m.Name+"' ("+m.Family+"): "+truncate(m.Intent, 48),
						events.D{
							"name":        m.Name,
							"family":      m.Family,
							"intent":      m.Intent,
							"synthesized": true,
						})
				}
			}
		}
		// parse the program tree, then structurally verify it before trust.
		if progDict, okDict := spec["program"].(map[string]any); okDict {
			if root, err := NodeFromDict(progDict); err == nil {
				prog := Program{Root: root, Goal: goal, Synthesized: true,
					Rationale: dictStr(spec, "rationale", "llm-synthesised")}
				if okVerify, _ := VerifyProgram(prog, catalog); okVerify {
					// Honest provenance: the heuristic toolmaker tags its own programs "heuristic";
					// only a model-written program is "llm". (Both flow through this same seam now.)
					result = &SynthResult{Program: prog, Minted: minted, Source: dictStr(spec, "source", "llm")}
				}
			}
		}
	}

	// 2. heuristic shape (deterministic fallback) when the LLM path didn't yield a valid program. With
	// webLookup ON (subconscious.web_search), a factual lookup question that hit no other shape gets a
	// research->answer program that staffs expose-affordances; OFF ⇒ RecognizeShapeWeb(...,false) ==
	// RecognizeShape ⇒ byte-identical.
	if result == nil {
		p, ok := RecognizeShapeWeb(goal, ctx, webLookup)
		if !ok {
			return nil, false // no workflow shape — specialists handle it directly
		}
		prog := *p
		if okVerify, _ := VerifyProgram(prog, catalog); !okVerify {
			// a malformed heuristic shape should never happen; fail safe to canonical
			prog = canonicalDesignBuildValidate(goal)
		}
		result = &SynthResult{Program: prog, Minted: minted, Source: "heuristic"}
	}

	// 3. LOOKUP-FORCE (subconscious.web_search ON only) — the SUBSTRATE-INDEPENDENT half of the
	// under-staffing fix. Steps 1 and 2 alone do NOT guarantee expose-affordances is staffed on the LIVE
	// path: the step-2 lookup-research shape (RecognizeShapeWeb) only runs when step-1 yielded NO valid
	// program — but the LIVE backend's SynthesizeProgram (the model) routinely returns a VALID program for
	// a lookup question that simply OMITS expose-affordances (the model writes a decompose/generate shape
	// from its own discretion). That sets `result` in step 1, step 2 is skipped, and web_search never gets
	// a turn — the measured ~1/N firing on HotpotQA-fullwiki that the offline test (which runs the
	// deterministic RecognizeShapeWebDict as the toolmaker) could not catch. So: REGARDLESS of which path
	// produced the program, if the flag is ON AND the goal is a factual lookup question AND the resolved
	// program does NOT already staff expose-affordances, PREPEND an expose-affordances research step. This
	// is Pattern-A (deterministic, NOT model-discretion) and fires on EVERY backend identically.
	//
	// CALIBRATION (unchanged): gated on isLookupQuestion — interrogative + no local file — so it does NOT
	// widen the GAIA-L1 over-grounding regime; a statement, a local-file task, or a non-question goal is
	// never touched. webLookup=false ⇒ this whole block is skipped ⇒ byte-identical OFF.
	if webLookup && result != nil && isLookupQuestion(goal, goalText(goal, ctx)) &&
		!programStaffsExposeAffordances(result.Program) {
		dom := domainOf(goalText(goal, ctx))
		forced := Program{
			Root: NewSeq(
				NewStep("expose-affordances", dom, "research the external facts the question needs (forced: lookup goal)"),
				result.Program.Root,
			),
			Goal:        goal,
			Synthesized: true,
			Rationale:   result.Program.Rationale + " + forced expose-affordances (lookup goal, web_search ON)",
		}
		// re-verify the wrapped program (expose-affordances is a seed operator, so this holds for any
		// already-valid inner program within the structural bounds); on the rare verify failure leave the
		// original program untouched rather than ship an invalid forced shape.
		if okVerify, _ := VerifyProgram(forced, catalog); okVerify {
			result.Program = forced
			if emit != nil {
				emit(events.SubSynthesize,
					"forced expose-affordances (lookup goal, web_search ON): "+forced.Shape(),
					events.D{
						"shape":     forced.Shape(),
						"source":    "lookup-force",
						"rationale": forced.Rationale,
						"program":   forced.ToDict(),
						"goal":      goal,
					})
			}
		}
	}

	if emit != nil {
		emit(events.SubSynthesize,
			"synthesised program ("+result.Source+"): "+result.Program.Shape(),
			events.D{
				"shape":     result.Program.Shape(),
				"source":    result.Source,
				"rationale": result.Program.Rationale,
				"program":   result.Program.ToDict(),
				"minted":    mintedNames(minted),
				// W3 visibility (the OPERATORS panel's `offered N of M` row): how many operators the
				// scored subset put in the synthesis prompt vs the full catalog — the bounded-prompt
				// invariant as a live number.
				"offered":       len(offered),
				"catalog_total": len(catalog.Names()),
			})
	}
	return result, true
}

// operatorsOf returns the spec's "operators" list as []map[string]any, mirroring Python
// `spec.get("operators", []) or []` — a missing key, a nil value, or a non-list yields no operators.
func operatorsOf(spec map[string]any) []map[string]any {
	raw, ok := spec["operators"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// dictStr returns d[key] as a string, or def if absent/not-a-string — the Go form of
// `od.get(key, def)` where the value is expected to be a str.
func dictStr(d map[string]any, key, def string) string {
	if v, ok := d[key].(string); ok {
		return v
	}
	return def
}

// mintedNames returns the names of the minted operators, in mint order — the Python
// `[m.name for m in minted]` carried on the final synthesise event.
func mintedNames(minted []OperatorSpec) []string {
	out := make([]string, len(minted))
	for i, m := range minted {
		out[i] = m.Name
	}
	return out
}
