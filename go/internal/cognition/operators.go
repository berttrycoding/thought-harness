// operators.go ports operators.py — the open operator catalog.
//
// A *cognitive operator* is an abstract, domain-general transform on whatever is in the stream
// (decompose, compare, iterate, ...) — distinct from a domain *specialist* (math, safety). This
// catalog is seeded from an earlier capability-taxonomy catalog (the three families:
// transformative, relational, generative — plus the hypothesised L1 primitives), but it is
// **open**: the synthesiser can Mint a genuinely new operator with its own definition at runtime
// (the dynamic-rule-synthesis concept — the system as toolmaker, not just tool-user),
// after Verify checks it (the two-layer discipline: never trust a synthesised rule on the model's
// word alone). Minted operators accumulate across episodes — progressive specialisation.
//
// Each operator's Intent is a one-line domain-general definition; it doubles as the seed prompt
// when a runtime sub-agent applies the operator to context.
//
// HARD PORT #1 (runtime minting). Python grows a mutable dict[str, OperatorSpec] at runtime via
// mint() after verify(). An operator is DATA (name+family+intent), not a type — so the Go port is
// pure registry + map insertion: NO code-gen, NO reflection. The registry is guarded by a
// sync.RWMutex because it is shared/persistent across episodes and read under parallel fan-out.
package cognition

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// SynthOfferCap is the top-K cap on how many operators are offered to the synthesiser's prompt (W3).
// Passing the WHOLE catalog into every synthesis call does not scale — at 10x the catalog the prompt
// floods and the model's selection degrades. When curation is ON a goal-scored SUBSET of this size is
// offered instead.
//
// FLAG-GATED, DEFAULT OFF (byte-identical): SynthOfferCap is resolved ONCE from THOUGHT_SYNTH_CATALOG_TOPK
// and DEFAULTS to 0 == no cap == the whole catalog is offered (the legacy catalog.Names() behaviour). A
// positive override (e.g. THOUGHT_SYNTH_CATALOG_TOPK=48) turns curation ON. So with the knob unset the
// synthesis prompt is byte-identical to before this build, and the bounded subset only engages when an
// operator opts in (mirrors the THOUGHT_MAX_PAR_WIDTH / THOUGHT_BEAM_LAMBDA env-knob pattern).
var SynthOfferCap = resolveSynthTopK()

// resolveSynthTopK reads THOUGHT_SYNTH_CATALOG_TOPK once. Unset / non-numeric / <=0 => 0 (curation OFF,
// the whole catalog is offered — byte-identical default). A positive value caps the offered subset to K.
func resolveSynthTopK() int {
	if v, ok := os.LookupEnv("THOUGHT_SYNTH_CATALOG_TOPK"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// Families is the closed set of operator families. Mirrors Python FAMILIES. A seed operator
// belongs to one of the first three (+ primitive); "synthesized" is reserved for the minted set.
var Families = []string{"transformative", "relational", "generative", "primitive", "synthesized"}

// Move is the directed step an operator makes on the ABSTRACTION LADDER — the load-bearing
// structural tag of the representation-space rebuild (M2, design/representation-space-rebuild.md
// §1.2). Family says "what KIND of verb"; Move says "which DIRECTION on the abstraction axis" —
// the two are orthogonal. There are exactly four directed moves plus a non-move "assess" lane (the
// Critic's judge/order verbs that do not travel the ladder), so the synthesiser can reason "I am
// stuck at the abstract level, I need a GROUND op to make it concrete" and a minted op declares its
// move (and inherits that move's Gate-prior bias) rather than guessing by name.
type Move string

const (
	// MoveGround instantiates: abstract -> concrete (generate / hypothesize / decompose / vary).
	MoveGround Move = "ground"
	// MoveLift generalises: concrete -> abstract (generalize / abstract / compress).
	MoveLift Move = "lift"
	// MoveReframe analogises: abstract -> abstract (analogize / invert / compare / contrast).
	MoveReframe Move = "reframe"
	// MoveTranscode translates: concrete -> concrete (synonymize / iterate / combine).
	MoveTranscode Move = "transcode"
	// MoveAssess is the non-move judge/order lane (rank / validate / eliminate / measure). It does
	// not travel the ladder — it scores or orders what a move produced (the Critic's lane).
	MoveAssess Move = "assess"
)

// Moves is the closed set of move tags (the 4 directed + the assess non-move). Used by Verify and
// by config.ReprMatrix gating (M4).
var Moves = []Move{MoveGround, MoveLift, MoveReframe, MoveTranscode, MoveAssess}

// OperatorSpec is the immutable definition of one operator. Mirrors the Python
// @dataclass(frozen=True) OperatorSpec — a Go value type, copied by value (frozen == value
// semantics). ToolScope is the least-privilege tool list the operator may dispatch when
// instantiated as a sub-agent (P2); EMPTY for reason-only operators (most of them).
//
// LEGACY(redesign): the ToolScope field below — the operator's flat concrete tool name-list — is STILL the
// ToolPicker's category SOURCE today (ScopeCategory / ScopeCategories derive the §3.6/§3.7 authority
// footprint from it), so it is a deeper future slice — removable when gap-9 (OperatorSpec.ToolScope
// deletion) lands (the op then declares its move-hint category footprint directly, not a name-list).
type OperatorSpec struct {
	Name        string
	Family      string
	Intent      string   // domain-general definition + the seed prompt for applying it
	Synthesized bool     // minted at runtime vs from the seed taxonomy
	ToolScope   []string // least-privilege tools this operator may dispatch (nil/empty for reason-only)
	Move        Move     // the directed step on the abstraction ladder (M2; "" == untagged/assess-by-default)

	// FuelNeeding marks a GROUND/REFRAME operator whose move produces a shape that is MISSING its
	// concrete content (e.g. generate/hypothesize/analogize): a fuel-needing candidate must have its
	// material sourced (the §1.3 ladder) and fused in before the hidden seam sees it (M3). Data-driven,
	// not a string-sniff — so concretization keys on this flag, not the operator name.
	FuelNeeding bool

	// Keywords + Examples are ADDITIVE RETRIEVAL METADATA (W3-S3 enrichment) — domain vocabulary a goal
	// might use (synonyms / related terms) and a few DIVERSE example phrasings the operator handles. They
	// are consumed ONLY by the curation retrieval score (RetrievalText -> offerScore in offerLexical /
	// offerSemantic), so the existing lexical floor connects diverse goal phrasings to an op whose abstract
	// Intent shares no surface word with the goal (e.g. goal "product of seven and eight" -> arith op
	// "evaluate a numeric expression to a value"). This is the embedder-free at-scale recovery the W3-S3
	// semantic seam was added to handle — the enrichment recovers it without an embedder wired.
	//
	// HARD INVARIANT: these NEVER change operator behaviour. Intent ALONE is the seed/apply prompt
	// (subagent.go), the persisted+hashed definition (persist.go / persist/store.go), the synth catalog
	// line (synth.go) and the TUI detail (tui/registry_data.go); none of those read Keywords/Examples. So
	// enrichment shifts ONLY which ops a goal RECOVERS into the curated subset, never how an op executes.
	// Because the seed core is ALWAYS included whole when it fits within k (it does at any plausible cap;
	// the offer passes reserve every present seed op), every scenario golden and the existing offer tests
	// are unaffected; enrichment moves the needle only where ops actually contend for a slot at scale
	// (the minted-op fill, and the seed core at a tight k below the seed-core size).
	Keywords []string // domain synonyms / related terms a goal might use to mean this op
	Examples []string // diverse example goal phrasings this op handles (multi-phrasing -> generalise, don't memorise)
}

// RetrievalText is the enriched scoring text used by the curation retrieval floor (offerScore in
// offerLexical / offerSemantic): Name + Intent + Keywords + Examples, space-joined. This is the W3-S3
// enrichment lever — by folding the DOMAIN VOCABULARY (synonyms a goal might use) and a few DIVERSE
// example phrasings into the text the goal is scored against, the existing lexical floor connects goal
// phrasings that share NO word with the abstract Intent (e.g. goal "product of seven and eight" -> arith
// op whose Intent is "evaluate a numeric expression to a value") — removing the embedder dependency for
// at-scale recovery. It is RETRIEVAL-ONLY: no behaviour, prompt, persistence, or TUI path reads it (those
// read Intent). On an un-enriched op (Keywords/Examples nil) it degrades to exactly "Name Intent" — the
// pre-enrichment scoring text — so an op with no metadata scores identically to before this build.
func (o OperatorSpec) RetrievalText() string {
	var b strings.Builder
	b.WriteString(o.Name)
	b.WriteByte(' ')
	b.WriteString(o.Intent)
	for _, kw := range o.Keywords {
		b.WriteByte(' ')
		b.WriteString(kw)
	}
	for _, ex := range o.Examples {
		b.WriteByte(' ')
		b.WriteString(ex)
	}
	return b.String()
}

// ScopeCategory reports the operator's coarsest AUTHORITY category for the §3.3a Scope check (the #31
// Operator facet — each object type registers its own category facet, §3.10a): the MOST powerful category
// among its ToolScope — a write tool ⇒ "mutate", else a run tool ⇒ "execute", else "inspect" (a reason-only
// operator with no tools is "inspect"). A Scope ceiling filters operators by this exactly as it filters
// tools, so an operator that can mutate cannot be assembled under a read-only ceiling.
//
// LEGACY(redesign): derives the authority category from the flat ToolScope name-list — removable when
// gap-9 (OperatorSpec.ToolScope deletion) lands (it then reads the move-hint footprint, not the names).
func (o OperatorSpec) ScopeCategory() string {
	cat := "inspect"
	for _, t := range o.ToolScope {
		switch t {
		case "write_file", "edit_file":
			return "mutate" // the strongest — short-circuit
		case "run_shell", "run_tests":
			cat = "execute"
		}
	}
	return cat
}

// ScopeCategories reports the operator's OPERATION-CATEGORY FOOTPRINT — the SET of coarse tool-operation
// categories ("inspect" | "mutate" | "execute") the operator's move reaches. This is the move-hint footprint
// the §3.6/§3.7 redesign keeps on the operator AFTER its concrete tool name-list is removed (gap 9): the
// operator declares WHICH categories its move needs to reach (a coarse, safe declaration), and the SubAgent's
// Scope resolves the concrete tool NAMES within that footprint at staffing (the category-sourced pick).
//
// A reason-only operator (no ToolScope) returns an EMPTY footprint — it reaches NO tool category, so its
// staffed worker resolves to NO tools (the parity-preserving discriminator: an operator with no declared
// footprint gets no tools, even though its single ScopeCategory floor reads "inspect"). The set is derived
// TODAY from the operator's ToolScope categories (so it is faithful while ToolScope still exists), but it is
// the COARSE FOOTPRINT — distinct from the concrete name-list — that survives the gap-9 ToolScope deletion.
// Returns the categories in stable (inspect, execute, mutate) order; an empty footprint is an empty slice.
func (o OperatorSpec) ScopeCategories() []string {
	var inspect, execute, mutate bool
	// LEGACY(redesign): the footprint is DERIVED from the flat ToolScope name-list today — removable when
	// gap-9 (OperatorSpec.ToolScope deletion) lands (the op then declares its category footprint directly).
	for _, t := range o.ToolScope {
		switch t {
		case "write_file", "edit_file":
			mutate = true
		case "run_shell", "run_tests":
			execute = true
		default:
			inspect = true // a read/search-class tool (read_file, search, ...) reaches the inspect band
		}
	}
	out := []string{}
	if inspect {
		out = append(out, "inspect")
	}
	if execute {
		out = append(out, "execute")
	}
	if mutate {
		out = append(out, "mutate")
	}
	return out
}

// seedOps is the seed taxonomy (the three-families concept + the L1
// primitives). Order preserved from Python _SEED (matters for Names() determinism). Each spec is
// a value; ToolScope defaults to nil (Python tool_scope=() empty tuple) unless declared.
//
// Every op carries a Move tag (M2, §1.2): the directed step on the abstraction ladder, orthogonal to
// Family. The minimal-real FUNCTIONAL BASIS (§2.3) is the subset the synth shapes + seed skills walk
// — decompose/generate/hypothesize (GROUND×3), generalize/abstract (LIFT×2), analogize/compare
// (REFRAME×2), validate/rank/eliminate (assess×3); the rest are the RICH LIBRARY the synthesiser
// mints/accretes from (anti-filler gate §2.6: they re-enter the live set only when a path walks them).
// FuelNeeding marks the GROUND/REFRAME ops whose output is a shape missing its concrete content (M3).
//
// W3-S3 ENRICHMENT (Keywords + Examples): a SUBSET of the most goal-targeted seed ops carries additive
// RETRIEVAL metadata — domain synonyms a goal might use, plus a few DIVERSE example phrasings (different
// words AND different operations) the op handles. This is consumed ONLY by RetrievalText -> offerScore so
// the lexical floor connects diverse goal phrasings to an op whose abstract Intent shares no surface word
// with the goal — it NEVER changes Intent (the apply prompt / persisted definition / synth line / TUI
// detail), so operator behaviour is unchanged. Un-enriched ops (Keywords/Examples nil) score exactly as
// before. The enrichment spans multiple op TYPES (transform / generate / compare / rank / search-read /
// measure / triage / compress) so retrieval generalises, not memorises one phrasing.
//
// LEGACY(redesign): the per-op `ToolScope: []string{...}` declarations below (measure / expose-affordances /
// validate carry one each) are the flat concrete tool name-lists the redesign replaces with the category-
// sourced ToolPicker — removable when gap-9 (OperatorSpec.ToolScope deletion) lands.
var seedOps = []OperatorSpec{
	// transformative (12)
	{Name: "decompose", Family: "transformative", Intent: "break the problem into independent parts", Move: MoveGround,
		Keywords: []string{"split", "divide", "subdivide", "partition", "break down", "factor", "subproblems", "subtasks", "pieces", "modular"},
		Examples: []string{"split this task into smaller subtasks", "divide the project into modules", "break the request down into steps"}},
	{Name: "combine", Family: "transformative", Intent: "merge parts into a single whole", Move: MoveTranscode,
		Keywords: []string{"merge", "join", "unify", "assemble", "integrate", "consolidate", "stitch together", "aggregate"},
		Examples: []string{"merge these fragments into one document", "assemble the parts into a finished result", "unify the partial answers"}},
	{Name: "reorder", Family: "transformative", Intent: "change the sequence or priority of steps", Move: MoveTranscode,
		Keywords: []string{"resequence", "reprioritise", "reshuffle", "sort steps", "rearrange order"},
		Examples: []string{"rearrange the steps so dependencies come first", "reprioritise the plan", "change the order of operations"}},
	{Name: "eliminate", Family: "transformative", Intent: "remove a part, option, or redundancy", Move: MoveAssess,
		Keywords: []string{"remove", "delete", "prune", "drop", "discard", "cut", "rule out", "strip"},
		Examples: []string{"rule out the options that cannot work", "prune the redundant branches", "drop the dead code paths"}},
	{Name: "replicate", Family: "transformative", Intent: "duplicate a part that works", Move: MoveTranscode,
		Keywords: []string{"duplicate", "copy", "clone", "reuse a working part"}},
	{Name: "specialize", Family: "transformative", Intent: "narrow to a domain to gain performance", Move: MoveGround,
		Keywords: []string{"narrow", "tailor", "make domain-specific", "specialise to the case"}},
	{Name: "generalize", Family: "transformative", Intent: "lift a specific case to a general rule", Move: MoveLift,
		Keywords: []string{"generalise", "abstract a rule", "find the pattern", "induce the principle", "make it reusable"},
		Examples: []string{"turn these examples into a general rule", "find the principle behind the cases", "extract a reusable pattern from the instances"}},
	{Name: "delegate", Family: "transformative", Intent: "hand a sub-part to another agent", Move: MoveTranscode,
		Keywords: []string{"hand off", "assign", "dispatch", "outsource", "give to a sub-agent"}},
	{Name: "cache", Family: "transformative", Intent: "store a result so it need not be recomputed", Move: MoveTranscode,
		Keywords: []string{"memoize", "save the result", "remember", "store for reuse", "avoid recomputation"}},
	{Name: "iterate", Family: "transformative", Intent: "repeat a step to refine the result", Move: MoveTranscode,
		Keywords: []string{"loop", "repeat", "refine", "improve over rounds", "polish", "successive passes"},
		Examples: []string{"keep refining the draft until it reads well", "loop over the candidate improving it each pass", "polish the answer in rounds"}},
	{Name: "invert", Family: "transformative", Intent: "flip the problem or an assumption", Move: MoveReframe,
		Keywords: []string{"flip", "reverse", "negate the assumption", "work backwards", "consider the opposite"}},
	{Name: "leverage", Family: "transformative", Intent: "amplify the result via an existing strength", Move: MoveTranscode,
		Keywords: []string{"exploit a strength", "build on what works", "amplify"}},
	// relational (7)
	{Name: "compare", Family: "relational", Intent: "identify what two things have in common", Move: MoveReframe,
		Keywords: []string{"weigh", "evaluate options", "trade-off", "pros and cons", "versus", "which is better", "similarities", "side by side", "alternatives"},
		Examples: []string{"weigh these two approaches against each other", "which library should I pick for the job", "look at the pros and cons of each plan"}},
	{Name: "analogize", Family: "relational", Intent: "map structure from a familiar domain onto this one", Move: MoveReframe, FuelNeeding: true,
		Keywords: []string{"analogy", "like", "is similar to", "borrow from a known domain", "metaphor"},
		Examples: []string{"explain this the way you would a familiar everyday thing", "find an analogy from another field", "what is this problem like"}},
	{Name: "contrast", Family: "relational", Intent: "identify where two things differ", Move: MoveReframe,
		Keywords: []string{"difference", "differ", "distinguish between", "what sets them apart", "diff"},
		Examples: []string{"what is the difference between these two designs", "show where the two outputs diverge", "spot what sets the options apart"}},
	{Name: "synonymize", Family: "relational", Intent: "restate the same content a different way", Move: MoveTranscode,
		Keywords: []string{"rephrase", "reword", "paraphrase", "say it differently", "restate"}},
	{Name: "measure", Family: "relational", Intent: "quantify the thing against a yardstick", Move: MoveAssess,
		ToolScope: []string{"run_tests"},
		Keywords:  []string{"compute", "calculate", "evaluate", "arithmetic", "number", "numeric", "sum", "total", "product", "multiply", "add", "count", "how many", "how much", "quantity", "metric", "score", "benchmark"},
		Examples:  []string{"calculate the total cost of the order", "how many items are left in the list", "compute the average response time"}},
	{Name: "rank", Family: "relational", Intent: "order the options by a criterion", Move: MoveAssess,
		Keywords: []string{"sort", "order", "prioritise", "best first", "top n", "rate", "score and order", "leaderboard"},
		Examples: []string{"sort these candidates from best to worst", "order the tasks by priority", "give me the top three results"}},
	{Name: "expose-affordances", Family: "relational", Intent: "reveal what actions the thing permits", Move: MoveAssess,
		ToolScope: []string{"search", "read_file"},
		Keywords:  []string{"find", "locate", "search", "look up", "grep", "where is", "read the file", "open the file", "inspect the source", "find the definition", "trace where defined", "discover"},
		Examples:  []string{"find where this function is defined in the codebase", "search the repo for the config loader", "open the file and read what it does"}},
	// generative (3, provisional)
	{Name: "vary", Family: "generative", Intent: "produce variants of the current candidate", Move: MoveGround, FuelNeeding: true,
		Keywords: []string{"variations", "alternatives", "different versions", "mutate", "tweak"}},
	{Name: "sample", Family: "generative", Intent: "draw instances from a space of possibilities", Move: MoveGround, FuelNeeding: true,
		Keywords: []string{"draw examples", "pick instances", "enumerate some cases", "random selection"}},
	{Name: "hypothesize", Family: "generative", Intent: "propose a candidate explanation or answer", Move: MoveGround, FuelNeeding: true,
		Keywords: []string{"guess", "conjecture", "propose a theory", "why might", "possible cause", "explanation", "diagnose"},
		Examples: []string{"propose a likely cause of the failing test", "guess why the request is timing out", "suggest a theory for the anomaly"}},
	// L1 primitives (hypothesised)
	{Name: "distinguish", Family: "primitive", Intent: "tell two things apart", Move: MoveAssess,
		Keywords: []string{"tell apart", "discriminate", "separate the two", "classify which"}},
	{Name: "map", Family: "primitive", Intent: "apply one transform across many elements", Move: MoveTranscode,
		Keywords: []string{"apply to each", "for every item", "transform each element", "batch over the collection"},
		Examples: []string{"apply the same fix to every file", "transform each row of the dataset", "run the operation across all items"}},
	{Name: "compose", Family: "primitive", Intent: "chain transforms into one", Move: MoveTranscode,
		Keywords: []string{"chain", "pipeline", "sequence of steps", "feed one into the next"}},
	// the engine's executable verbs (kept so synthesised programs can name them directly)
	{Name: "generate", Family: "generative", Intent: "draft a concrete candidate for this part", Move: MoveGround, FuelNeeding: true,
		Keywords: []string{"write", "draft", "create", "produce", "compose text", "author", "make a first version", "synthesise content"},
		Examples: []string{"write a first draft of the summary", "create an example config file", "produce a candidate answer to the question"}},
	{Name: "validate", Family: "relational", Intent: "check the result against the requirements", Move: MoveAssess,
		ToolScope: []string{"run_tests"},
		Keywords:  []string{"verify", "check", "test", "confirm correctness", "does it meet the spec", "run the tests", "assert"},
		Examples:  []string{"check whether the output meets the requirements", "verify the answer is correct", "run the test suite to confirm it passes"}},
	{Name: "abstract", Family: "transformative", Intent: "lift to the essential structure, dropping specifics", Move: MoveLift,
		Keywords: []string{"essence", "core idea", "strip the details", "high-level shape"}},
	// standardised from real harness data (lathe + Claude Code / Codex / Hermes / OpenCode) — #20
	{Name: "triage", Family: "relational", Intent: "sort items into urgency or category buckets", Move: MoveAssess,
		Keywords: []string{"categorise", "bucket", "classify", "label by type", "group by", "urgency", "severity", "route"},
		Examples: []string{"group these bug reports by severity", "categorise the incoming requests", "label each ticket by which team owns it"}},
	{Name: "gate", Family: "transformative", Intent: "let through only what passes a check", Move: MoveAssess,
		Keywords: []string{"filter", "admit only", "block what fails", "guard", "allow if"}},
	{Name: "ratchet", Family: "relational", Intent: "keep the new result only if it strictly beats the best so far", Move: MoveAssess,
		Keywords: []string{"keep best", "monotonic improvement", "only if better", "high-water mark"}},
	{Name: "compress", Family: "transformative", Intent: "reduce size while preserving the essential meaning", Move: MoveLift,
		Keywords: []string{"summarise", "shorten", "condense", "tldr", "distil", "abridge", "make it shorter"},
		Examples: []string{"summarise this long document in a paragraph", "shorten the message while keeping the point", "give me the gist of the report"}},
	{Name: "extrapolate", Family: "generative", Intent: "project forward from current data toward an unknown", Move: MoveGround, FuelNeeding: true,
		Keywords: []string{"predict", "forecast", "project ahead", "estimate the trend", "what comes next"}},
	{Name: "curate", Family: "transformative", Intent: "select, refine, and organise items for reuse", Move: MoveAssess,
		Keywords: []string{"select the best", "organise a collection", "tidy up for reuse", "shortlist"}},
}

// toEnum maps an operator name -> the Operator enum carried on a Thought (back-compat with the
// typed model). Mirrors Python _TO_ENUM. Names absent here fall through to GENERATE in ToEnum.
var toEnum = map[string]types.Operator{
	"decompose": types.DECOMPOSE, "validate": types.VALIDATE, "measure": types.VALIDATE,
	"compare": types.COMPARE, "contrast": types.COMPARE, "generalize": types.GENERALIZE,
	"abstract": types.ABSTRACT, "simulate": types.SIMULATE, "hypothesize": types.SIMULATE,
	"generate": types.GENERATE,
}

// ToEnum maps an operator name to its Operator enum, defaulting to GENERATE for any name not in
// the table. Mirrors Python to_enum(op_name).
func ToEnum(opName string) types.Operator {
	if op, ok := toEnum[opName]; ok {
		return op
	}
	return types.GENERATE
}

// identifierRe matches a non-empty identifier of [a-z0-9] after hyphens/underscores are removed —
// the Go equivalent of Python `name.replace("-","").replace("_","").isalnum()` on a lowercased
// name. Python str.isalnum() is true only for a non-empty all-alphanumeric string; the anchored
// regexp over the stripped name reproduces that (empty stripped name => no match => reject).
var identifierRe = regexp.MustCompile(`^[a-z0-9]+$`)

// familySet is Families as a lookup set (membership check for Verify).
var familySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Families))
	for _, f := range Families {
		m[f] = struct{}{}
	}
	return m
}()

// OperatorRegistry is an open registry of operators: seeded from the taxonomy, extensible at
// runtime. Mirrors Python OperatorRegistry. The map is guarded by a sync.RWMutex because the
// registry is shared/persistent across episodes and read under parallel fan-out (the Python
// single-threaded dict has no lock; Go's concurrency demands one — reads take RLock, Mint takes
// the write Lock).
type OperatorRegistry struct {
	mu       sync.RWMutex
	ops      map[string]OperatorSpec
	minted   []string           // operators synthesised this run (accumulate -> specialise)
	embedder retrieval.Embedder // optional semantic channel for Offer (nil -> lexical-only, the default)
}

// SetEmbedder wires an optional semantic channel into Offer (W3-S3): when set, the at-scale Offer path
// RRF-fuses the existing lexical Jaccard ranking with an embedding-cosine ranking over the minted ops —
// the SAME pattern SkillRegistry.SetEmbedder/matchHybrid uses for goal-vs-capability — so a semantically
// relevant op with NO surface-word overlap (e.g. minted `arith` "evaluate a numeric expression" for goal
// "product of seven and eight") is recovered into top-K instead of scoring 0.0 and being structurally
// dropped. nil (the default) keeps Offer purely lexical: the offline suite and every scenario golden are
// byte-identical to the validated W3-S1 path. The wiring is engine-side (engine.go sets it from the shared
// reachable embedder, beside the call that wires the skill registry); the offline test double leaves it nil.
//
// HONEST FRAMING: WITHOUT an embedder, lexical curation CANNOT bridge meaning. Only the embedder bridges
// meaning — the lexical path's MintFloorReserve net is a structural backstop, not a semantic recovery
// (see MintFloorReserve). RetrieverMode surfaces which channel is active so a silent fallback is observable.
func (r *OperatorRegistry) SetEmbedder(e retrieval.Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedder = e
}

// RetrieverMode reports the curation channel Offer is using, surfaced on the subconscious.catalog_offer
// event's `mode` field (W3-S3) so a live --backend claude run whose embeddings server is unreachable —
// and therefore silently fell back to lexical — is OBSERVABLE, never mistaken for active semantic curation:
//   - "semantic"  an embedder is wired AND the cap engages -> Offer RRF-fuses lexical + cosine (meaning is
//     bridged) — the at-scale semantic retrieval seam is live;
//   - "lexical"   no embedder, but the cap engages at scale -> lexical-only curation (+ the MintFloorReserve
//     net), which CANNOT bridge meaning (the honest label for a silent embedder fallback at scale);
//   - "off"       the cap is not engaging (catalog fits within k, or k<=0) -> no curation pressure.
//
// capEngages says whether the cap actually bounds this Offer (k>0 AND the catalog exceeds k); when it does
// not there is no curation pressure to characterise ("off"), regardless of whether an embedder is wired.
func (r *OperatorRegistry) RetrieverMode(capEngages bool) string {
	if !capEngages {
		return "off"
	}
	r.mu.RLock()
	hasEmb := r.embedder != nil
	r.mu.RUnlock()
	if hasEmb {
		return "semantic"
	}
	return "lexical"
}

// NewOperatorRegistry builds a registry seeded from the taxonomy. Mirrors Python
// OperatorRegistry.__init__ — self._ops = {o.name: o for o in _SEED}; self.minted = [].
func NewOperatorRegistry() *OperatorRegistry {
	ops := make(map[string]OperatorSpec, len(seedOps))
	for _, o := range seedOps {
		ops[o.Name] = o
	}
	return &OperatorRegistry{ops: ops, minted: nil}
}

// Get returns the spec for name and ok=true, or the zero spec and ok=false if absent. Mirrors
// Python get(name) -> OperatorSpec | None (the bool replaces the None signal).
func (r *OperatorRegistry) Get(name string) (OperatorSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.ops[name]
	return o, ok
}

// Has reports whether name is a registered operator. Mirrors Python has(name).
func (r *OperatorRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.ops[name]
	return ok
}

// GrantToolScope appends `tool` to operator `name`'s flat ToolScope (idempotent — a no-op if the
// operator is absent or already holds the tool), returning ok=true iff the tool is present after the
// call. It is the opt-in, flag-gated edge that widens a seed operator's least-privilege tool list at
// engine build (the WEB-SEARCH wire: grant expose-affordances the web_search tool only when the
// subconscious.web_search flag is on). The registry's per-engine map holds VALUE copies of seedOps
// (NewOperatorRegistry copies each spec in), so mutating the stored spec's ToolScope here is local to
// THIS engine's catalog — it never leaks to the shared package-level seedOps or another engine.
func (r *OperatorRegistry) GrantToolScope(name, tool string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.ops[name]
	if !ok {
		return false
	}
	for _, t := range o.ToolScope {
		if t == tool {
			return true // already granted (idempotent)
		}
	}
	o.ToolScope = append(append([]string(nil), o.ToolScope...), tool) // fresh slice — never alias the seed backing array
	r.ops[name] = o
	return true
}

// Names returns the registered operator names in insertion order (seed order first, then minted
// in mint order). Mirrors Python names() -> list(self._ops): Python dict preserves insertion
// order, so the Go port reconstructs that order from seedOps + the minted slice rather than
// ranging the unordered Go map.
func (r *OperatorRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.ops))
	for _, o := range seedOps {
		if _, ok := r.ops[o.Name]; ok {
			out = append(out, o.Name)
		}
	}
	for _, name := range r.minted {
		out = append(out, name)
	}
	return out
}

// Family returns the family of name and ok=true, or "" and ok=false if absent. Mirrors Python
// family(name) -> str | None.
func (r *OperatorRegistry) Family(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if o, ok := r.ops[name]; ok {
		return o.Family, true
	}
	return "", false
}

// MoveOf returns the abstraction-ladder move of name and ok=true, or "" and ok=false if absent (M2).
func (r *OperatorRegistry) MoveOf(name string) (Move, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if o, ok := r.ops[name]; ok {
		return o.Move, true
	}
	return "", false
}

// IsFuelNeeding reports whether name is a fuel-needing GROUND/REFRAME operator (M3) — its move
// produces a shape missing concrete content, so concretization must source + fuse fuel before the
// seam. ok=false when the operator is absent.
func (r *OperatorRegistry) IsFuelNeeding(name string) (bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if o, ok := r.ops[name]; ok {
		return o.FuelNeeding, true
	}
	return false, false
}

// Verify applies the two-layer discipline: a synthesised operator is checked structurally before
// trust. A seed operator is a frozen invariant — it may not be silently redefined. Returns
// (ok, reason). Mirrors Python verify(name, family, intent):
//   - lowercase+trim the name; reject if it is not a non-empty identifier;
//   - reject if name collides with a *seed* (non-synthesized) operator (frozen invariant);
//   - reject if family is not one of FAMILIES;
//   - reject if intent has fewer than 3 whitespace-split words.
//
// Verify takes the read lock to consult the seed-collision check; it does NOT mutate.
func (r *OperatorRegistry) Verify(name, family, intent string) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || !identifierRe.MatchString(stripWordChars(name)) {
		return false, "name must be a non-empty identifier"
	}
	r.mu.RLock()
	seed, exists := r.ops[name]
	r.mu.RUnlock()
	if exists && !seed.Synthesized {
		return false, "'" + name + "' is a seed operator (frozen invariant); cannot redefine"
	}
	if _, ok := familySet[family]; !ok {
		return false, "family must be one of " + familiesRepr()
	}
	if len(strings.Fields(intent)) < 3 {
		return false, "intent must be a real definition (>=3 words)"
	}
	return true, "ok"
}

// Mint registers a new operator if it verifies; else rejects it (returns ok=false). Idempotent on
// an already-minted name with the same definition (re-inserting is harmless; the minted slice
// guards against a duplicate entry). Mirrors Python mint(name, family, intent) -> OperatorSpec |
// None. Takes the write lock so map insertion + minted-list append are atomic under fan-out.
func (r *OperatorRegistry) Mint(name, family, intent string) (OperatorSpec, bool) {
	return r.MintWithMove(name, family, intent, "")
}

// MintWithMove is the move-declaring mint (M2, §1.2): a runtime-synthesised operator declares which
// directed step it makes on the abstraction ladder, so it inherits that move's Gate-prior lane (fixing
// opBias-by-name) instead of being born with no direction. An empty move defaults to the family's
// canonical move (moveForFamily) so the legacy Mint(name,family,intent) path still tags every minted
// op. Mirrors Python mint with the added move tag; identical lock/idempotency discipline as Mint.
func (r *OperatorRegistry) MintWithMove(name, family, intent string, move Move) (OperatorSpec, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	ok, _ := r.Verify(name, family, intent)
	if !ok {
		return OperatorSpec{}, false
	}
	if move == "" {
		move = moveForFamily(family)
	}
	spec := OperatorSpec{Name: name, Family: family, Intent: strings.TrimSpace(intent), Synthesized: true, Move: move}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops[name] = spec
	if !containsString(r.minted, name) {
		r.minted = append(r.minted, name)
	}
	return spec, true
}

// MintEnriched is MintWithMove plus the additive W3-S3 RETRIEVAL METADATA (Keywords + Examples) — the path a
// runtime-minted DOMAIN operator (e.g. an `arith` op the synthesiser invents) takes when the synthesiser can
// also describe the goal vocabulary + diverse example phrasings the op handles. The metadata is retrieval-only:
// it widens the lexical surface offerScore scores against via RetrievalText (so a diverse goal phrasing recovers
// the op into top-K at scale) WITHOUT touching Intent — the apply prompt, the persisted/hashed definition, the
// synth catalog line, and the TUI detail all read Intent alone, so behaviour is byte-identical to MintWithMove.
// Same Verify + lock/idempotency discipline. (Intent stays the canonical definition; Keywords/Examples never
// gate Verify.)
func (r *OperatorRegistry) MintEnriched(name, family, intent string, move Move, keywords, examples []string) (OperatorSpec, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	ok, _ := r.Verify(name, family, intent)
	if !ok {
		return OperatorSpec{}, false
	}
	if move == "" {
		move = moveForFamily(family)
	}
	spec := OperatorSpec{
		Name: name, Family: family, Intent: strings.TrimSpace(intent), Synthesized: true, Move: move,
		Keywords: append([]string(nil), keywords...),
		Examples: append([]string(nil), examples...),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops[name] = spec
	if !containsString(r.minted, name) {
		r.minted = append(r.minted, name)
	}
	return spec, true
}

// moveForFamily is the canonical move a minted operator inherits from its family when it declares none.
// Transformative work re-shapes what exists (LIFT/restructure), generative work invents a concrete
// candidate (GROUND), relational work judges/orders (ASSESS); a synthesised family with no signal
// defaults to the GROUND lane (the productive direction).
func moveForFamily(family string) Move {
	switch family {
	case "transformative":
		return MoveLift // the transform family's centre of mass is lift/restructure
	case "relational":
		return MoveAssess // relate/weigh/order -> the judge lane
	case "generative":
		return MoveGround // invent a concrete candidate
	case "primitive":
		return MoveTranscode
	default: // synthesized / unknown
		return MoveGround
	}
}

// MintFloorReserve is the size of the deterministic minted-op floor (W3-S3 requirement #3): when the cap
// engages at scale and there is NO embedder, a relevant minted op whose lexical Jaccard score is 0 is
// structurally excluded from the synthesis prompt — it gets ZERO protection (the seed core is reserved by
// family, the minted ops are not). This reserves a small number of the MOST-RECENTLY-MINTED ops an
// always-include slot (the recency proxy for "high-value / hot"), so a just-minted op is never dropped
// purely because the goal shares no surface words with it.
//
// HONESTY (this is a NET, not a meaning-bridge): the reserve only protects the last few minted ops by
// recency; it CANNOT tell a semantically-relevant op from an irrelevant one — only the embedder bridges
// meaning. It is a deterministic partial backstop against structural exclusion, nothing more; when an
// embedder is wired the cosine channel does the real recovery and the reserve is not applied.
const MintFloorReserve = 4

// Offer returns up to k operator names for the synthesiser's prompt — the W3 curated, bounded,
// goal-scored subset. This is the Pattern-A RETRIEVAL FLOOR: deterministic, NO model.
//
// k<=0 (the SynthOfferCap default == THOUGHT_SYNTH_CATALOG_TOPK unset) ⇒ NO cap: the whole catalog is
// offered (== Names()), byte-identical to the pre-curation behaviour. When the catalog fits within k, every
// operator is offered. The bound only shapes the subset when curation is ON and the catalog exceeds k.
//
// W3-S3 channel selection (additive, default OFF -> byte-identical):
//   - NO embedder wired (the default, incl. the offline test double) -> offerLexical: the frozen,
//     validated W3-S1 three-pass lexical algorithm (core-family reserve + Jaccard fill), unchanged.
//   - an embedder wired (engine.go on a live backend with a reachable /v1/embeddings) -> offerSemantic:
//     the seed core is reserved EXACTLY as in offerLexical, then the minted-op fill RRF-fuses lexical
//     Jaccard with embedding cosine so a semantically relevant op with no surface overlap is recovered.
//
// The dispatch keys ONLY on whether an embedder is present; the cap/no-cap gate is identical on both
// paths, so with no embedder Offer is byte-identical to before this build.
func (r *OperatorRegistry) Offer(goal string, k int) []string {
	all := r.Names()
	if k <= 0 || len(all) <= k {
		return all
	}
	r.mu.RLock()
	hasEmb := r.embedder != nil
	r.mu.RUnlock()
	if hasEmb {
		return r.offerSemantic(goal, k)
	}
	return r.offerLexical(goal, k)
}

// offerLexical is the frozen, validated W3-S1 curation path — the Pattern-A lexical floor (Jaccard over the
// goal vs name+intent) with a stable name tie-break, built in three always-deterministic passes:
//
//  1. CORE FAMILIES (always-include): one representative PER core family
//     (transformative/relational/generative/primitive) is reserved first — the most goal-relevant seed
//     op in each family — so curation can NEVER strip a whole foundational family, even at a tiny k.
//  2. The rest of the seed core, goal-scored, fills the budget (a goal that names a seed op pulls it in
//     ahead of an irrelevant one; seed order is the stable tie-break).
//  3. The minted operators, goal-scored, fill any remaining budget.
//
// This is the byte-identical default (no embedder); offer_test.go + catalog_offer_wire_test.go pin it.
func (r *OperatorRegistry) offerLexical(goal string, k int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	offered := make([]string, 0, k)
	seen := make(map[string]bool, k)
	add := func(name string) {
		if !seen[name] {
			offered = append(offered, name)
			seen[name] = true
		}
	}

	// type for a goal-scored candidate name (used in passes 2 and 3).
	type scored struct {
		name string
		s    float64
	}
	scoreOf := func(name string) float64 {
		spec := r.ops[name]
		return offerScore(goal, spec.RetrievalText())
	}
	rankStable := func(xs []scored) {
		sort.SliceStable(xs, func(i, j int) bool {
			if xs[i].s != xs[j].s {
				return xs[i].s > xs[j].s // most relevant first
			}
			return xs[i].name < xs[j].name // deterministic tie-break
		})
	}

	// PASS 1 — one representative per core family (always-include). Pick the most goal-relevant
	// PRESENT seed op in each family; iterate the closed Families list in its fixed order so the set of
	// families covered is deterministic and independent of map order. Bounded by k.
	for _, fam := range Families {
		if fam == "synthesized" {
			continue // minted-only family; no seed core to reserve
		}
		if len(offered) >= k {
			break
		}
		var bestName string
		var bestScore float64
		found := false
		for _, o := range seedOps { // seed order = the stable tie-break
			if o.Family != fam {
				continue
			}
			if _, ok := r.ops[o.Name]; !ok || seen[o.Name] {
				continue
			}
			s := scoreOf(o.Name)
			if !found || s > bestScore {
				bestName, bestScore, found = o.Name, s, true
			}
		}
		if found {
			add(bestName)
		}
	}

	// PASS 2 — the rest of the seed core, goal-scored, fills the budget.
	rest := make([]scored, 0, len(seedOps))
	for _, o := range seedOps {
		if _, ok := r.ops[o.Name]; !ok || seen[o.Name] {
			continue
		}
		rest = append(rest, scored{o.Name, scoreOf(o.Name)})
	}
	rankStable(rest)
	for _, c := range rest {
		if len(offered) >= k {
			break
		}
		add(c.name)
	}

	// PASS 3 — the minted operators, goal-scored, fill any remaining budget. This is the FROZEN W3-S1
	// ranking (Jaccard desc, ascending-name tie-break), preserved EXACTLY: offer_test.go +
	// catalog_offer_wire_test.go pin it. The W3-S3 MintFloorReserve net does NOT touch this default lexical
	// path (it would perturb the validated order); it applies only on offerSemantic's degraded fallback,
	// reachable only when an embedder was wired and then errored mid-run.
	minted := make([]scored, 0, len(r.minted))
	for _, name := range r.minted {
		if seen[name] {
			continue
		}
		minted = append(minted, scored{name, scoreOf(name)})
	}
	rankStable(minted)
	for _, m := range minted {
		if len(offered) >= k {
			break
		}
		add(m.name)
	}
	return offered
}

// reserveRecentMinted returns the set of candidate indices the MintFloorReserve net protects: the
// most-recently-minted candidates (highest recency index), capped by the reserve size and the remaining
// budget, ascending-name on a recency tie (deterministic). candName[i] is the i-th candidate; recencyOf[i]
// is its position in r.minted (mint order = recency). The reserve is a NET, NOT a meaning-bridge: it cannot
// tell a relevant op from an irrelevant one — only the embedder bridges meaning. It exists so a just-minted
// op is not structurally excluded purely because the goal shares no surface words with it on a lexical
// ranking (the degraded fallback). When the cosine channel is live the reserve is not applied — the
// semantic ranking does the real recovery.
func reserveRecentMinted(candName []string, recencyOf []int, remaining int) map[int]bool {
	reserved := make(map[int]bool)
	if remaining <= 0 || len(candName) == 0 {
		return reserved
	}
	want := MintFloorReserve
	if want > remaining {
		want = remaining
	}
	order := make([]int, len(candName))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if recencyOf[ia] != recencyOf[ib] {
			return recencyOf[ia] > recencyOf[ib] // most recent first
		}
		return candName[ia] < candName[ib]
	})
	for i := 0; i < want && i < len(order); i++ {
		reserved[order[i]] = true
	}
	return reserved
}

// offerSemantic is the W3-S3 at-scale retrieval seam (engaged ONLY when an embedder is wired). It reserves
// the seed core EXACTLY as offerLexical does (PASS 1 family reserve + PASS 2 lexical seed fill — the same
// validated invariants: a core family is never stripped, seed order is the stable tie-break), then fills
// the remaining budget with the minted ops ranked by a HYBRID score: lexical Jaccard RRF-fused with
// embedding cosine. A semantically-relevant minted op with no surface-word overlap is thereby recovered
// into top-K instead of scoring 0.0 and being dropped. Deterministic throughout: cached vectors (the real
// embedder caches), stable FuseRRF, ascending-name tie-break. A mid-run embedder error degrades cleanly to
// the lexical minted ranking (never a hard failure), matching skills.go matchHybrid.
func (r *OperatorRegistry) offerSemantic(goal string, k int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	offered := make([]string, 0, k)
	seen := make(map[string]bool, k)
	add := func(name string) {
		if !seen[name] {
			offered = append(offered, name)
			seen[name] = true
		}
	}

	type scored struct {
		name string
		s    float64
	}
	scoreOf := func(name string) float64 {
		spec := r.ops[name]
		return offerScore(goal, spec.RetrievalText())
	}
	rankStable := func(xs []scored) {
		sort.SliceStable(xs, func(i, j int) bool {
			if xs[i].s != xs[j].s {
				return xs[i].s > xs[j].s
			}
			return xs[i].name < xs[j].name
		})
	}

	// PASS 1 — one representative per core family (always-include), IDENTICAL to offerLexical.
	for _, fam := range Families {
		if fam == "synthesized" {
			continue
		}
		if len(offered) >= k {
			break
		}
		var bestName string
		var bestScore float64
		found := false
		for _, o := range seedOps {
			if o.Family != fam {
				continue
			}
			if _, ok := r.ops[o.Name]; !ok || seen[o.Name] {
				continue
			}
			s := scoreOf(o.Name)
			if !found || s > bestScore {
				bestName, bestScore, found = o.Name, s, true
			}
		}
		if found {
			add(bestName)
		}
	}

	// PASS 2 — the rest of the seed core, goal-scored lexically, IDENTICAL to offerLexical (the seed core
	// is the foundational vocabulary; its ordering is the validated lexical one).
	rest := make([]scored, 0, len(seedOps))
	for _, o := range seedOps {
		if _, ok := r.ops[o.Name]; !ok || seen[o.Name] {
			continue
		}
		rest = append(rest, scored{o.Name, scoreOf(o.Name)})
	}
	rankStable(rest)
	for _, c := range rest {
		if len(offered) >= k {
			break
		}
		add(c.name)
	}

	// PASS 3 — the minted operators, HYBRID-ranked (lexical Jaccard RRF-fused with embedding cosine), fill
	// any remaining budget. candName[i] is the i-th minted candidate not already offered; candRecency[i] is
	// its position in r.minted (mint order = the recency proxy for the MintFloorReserve net).
	candName := make([]string, 0, len(r.minted))
	candRecency := make([]int, 0, len(r.minted))
	for i, name := range r.minted {
		if seen[name] {
			continue
		}
		candName = append(candName, name)
		candRecency = append(candRecency, i)
	}
	if len(candName) == 0 {
		return offered
	}

	// lexByIdx[i] is the lexical Jaccard of candidate i; lex holds ONLY positive-score candidates (a
	// zero-score op must not get a spurious lexical rank that would dilute the semantic signal under RRF —
	// the same guard skills.go matchHybrid uses). sem holds the cosine over EVERY candidate.
	lexByIdx := make([]float64, len(candName))
	var lex []retrieval.Scored
	var sem []retrieval.Scored
	if qv, err := r.embedder.Embed(goal); err == nil {
		ok := true
		sem = make([]retrieval.Scored, 0, len(candName))
		for i, name := range candName {
			spec := r.ops[name]
			sv, errE := r.embedder.Embed(spec.RetrievalText())
			if errE != nil {
				ok = false
				break // a mid-run embedder error -> degrade to lexical-only (never a hard failure)
			}
			sem = append(sem, retrieval.Scored{ID: i, Score: retrieval.Cosine(qv, sv)})
		}
		if !ok {
			sem = nil
		}
	}
	for i, name := range candName {
		spec := r.ops[name]
		lexByIdx[i] = offerScore(goal, spec.RetrievalText())
		if lexByIdx[i] > 0 {
			lex = append(lex, retrieval.Scored{ID: i, Score: lexByIdx[i]})
		}
	}

	var ranked []retrieval.Scored
	semLive := len(sem) > 0
	if semLive {
		sortByScore(lex) // FuseRRF reads POSITION as rank -> sort both best-first first
		sortByScore(sem)
		ranked = retrieval.FuseRRF(lex, sem)
	} else {
		// no semantic channel (embedder error mid-run) -> lexical Jaccard over ALL candidates with an
		// ascending-name tie-break, matching the lexical minted ranking.
		ranked = make([]retrieval.Scored, len(candName))
		for i := range candName {
			ranked[i] = retrieval.Scored{ID: i, Score: lexByIdx[i]}
		}
		sort.SliceStable(ranked, func(a, b int) bool {
			if ranked[a].Score != ranked[b].Score {
				return ranked[a].Score > ranked[b].Score
			}
			return candName[ranked[a].ID] < candName[ranked[b].ID]
		})
	}

	// The MintFloorReserve net (#3) applies ONLY on the degraded (lexical-only) fallback — when the cosine
	// channel is live it does the real recovery and the net is not needed. The reserve keeps the most-recent
	// minted ops from being structurally excluded on a lexical ranking; reserved ops are admitted first (in
	// ranked order) so a hot op survives even when the goal shares no surface words with it.
	reserved := map[int]bool{}
	if !semLive {
		reserved = reserveRecentMinted(candName, candRecency, k-len(offered))
	}
	for _, s := range ranked {
		if len(offered) >= k {
			break
		}
		if reserved[s.ID] {
			add(candName[s.ID])
		}
	}
	for _, s := range ranked {
		if len(offered) >= k {
			break
		}
		if !reserved[s.ID] {
			add(candName[s.ID])
		}
	}
	return offered
}

// stopWords is the deterministic, closed STOP-WORD SET filtered out of BOTH the goal and the op text before
// offerScore counts coverage (W3-S3 ungameability fix). It is the common English FUNCTION-WORD vocabulary —
// articles, prepositions, conjunctions, auxiliaries, pronouns, and the highest-frequency filler verbs/adverbs
// that carry no retrieval signal. Removing them means (1) a function word can never DECIDE a winner (two ops
// that share only "the"/"of"/"a" with the goal tie at 0, so the genuine-content tie-break stands), and
// (2) a KEYWORD-STUFFED op cannot win by padding its description with common words a goal happens to use — the
// stuffed function words are stripped from the goal's denominator, so stuffing them buys exactly zero coverage.
// It is a FIXED literal set (no RNG, no clock, no locale lookup) so the score stays a pure deterministic
// Pattern-A function. It is INTENTIONALLY conservative — only words with no domain meaning — so it never
// strips a real query term ("read", "find", "add", "sort" etc. are NOT stop words; they are retrieval signal).
var stopWords = map[string]struct{}{
	// articles / determiners
	"a": {}, "an": {}, "the": {}, "this": {}, "that": {}, "these": {}, "those": {}, "some": {}, "any": {},
	"each": {}, "every": {}, "all": {}, "no": {}, "such": {}, "another": {}, "other": {}, "both": {},
	// pronouns
	"i": {}, "me": {}, "my": {}, "we": {}, "us": {}, "our": {}, "you": {}, "your": {}, "it": {}, "its": {},
	"he": {}, "she": {}, "him": {}, "her": {}, "his": {}, "they": {}, "them": {}, "their": {}, "who": {},
	"whom": {}, "which": {}, "what": {}, "whose": {},
	// prepositions
	"of": {}, "in": {}, "on": {}, "at": {}, "to": {}, "from": {}, "by": {}, "with": {}, "without": {},
	"for": {}, "about": {}, "into": {}, "onto": {}, "over": {}, "under": {}, "between": {}, "among": {},
	"through": {}, "during": {}, "before": {}, "after": {}, "above": {}, "below": {}, "up": {}, "down": {},
	"out": {}, "off": {}, "near": {}, "per": {}, "via": {}, "within": {}, "across": {}, "around": {},
	"against": {}, "toward": {}, "towards": {}, "upon": {},
	// conjunctions
	"and": {}, "or": {}, "but": {}, "nor": {}, "so": {}, "yet": {}, "if": {}, "then": {}, "else": {},
	"because": {}, "while": {}, "as": {}, "than": {}, "though": {}, "although": {}, "whether": {},
	// auxiliary / copula verbs
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {}, "am": {}, "do": {},
	"does": {}, "did": {}, "doing": {}, "done": {}, "have": {}, "has": {}, "had": {}, "having": {},
	"will": {}, "would": {}, "shall": {}, "should": {}, "can": {}, "could": {}, "may": {}, "might": {},
	"must": {}, "ought": {}, "need": {},
	// high-frequency adverbs / fillers with no retrieval signal
	"not": {}, "very": {}, "just": {}, "only": {}, "also": {}, "too": {}, "now": {}, "here": {},
	"there": {}, "again": {}, "ever": {}, "even": {}, "still": {}, "well": {}, "much": {}, "many": {},
	"more": {}, "most": {}, "less": {}, "least": {}, "quite": {}, "rather": {}, "really": {}, "please": {},
	// generic question/instruction scaffolding (no domain meaning on its own)
	"how": {}, "when": {}, "where": {}, "why": {}, "want": {}, "let": {}, "make": {}, "get": {},
	"give": {}, "go": {}, "got": {},
}

// isStopWord reports whether w (already lowercased) is a function/stop word with no retrieval signal.
func isStopWord(w string) bool {
	_, ok := stopWords[w]
	return ok
}

// offerScore is the goal-vs-operator LEXICAL similarity used by the curation retrieval floor (offerLexical /
// offerSemantic, over each op's enriched RetrievalText). Pulled out as a named function so the retrieval
// mechanism is one identifiable place to reason about. Deterministic; NO model (Pattern A).
//
// It is GOAL-WORD COVERAGE — |goal ∩ opText| / |goal CONTENT words| — over CONTENT words only, NOT symmetric
// Jaccard. This is the load-bearing choice for the W3-S3 enrichment: coverage measures "how much of what the
// goal ASKED FOR does this op's description cover", normalised by the GOAL length, so it is INVARIANT to the
// op-description length. Symmetric Jaccard (types.Jaccard) divides by the UNION, so folding domain vocabulary +
// examples into a description balloons the union and DILUTES the score — enrichment would paradoxically DEMOTE a
// relevant op as the catalog/op-text grows. Coverage cannot: adding keywords can only RAISE the intersection of
// goal-words covered, never lower it, so enrichment monotonically helps recovery. (types.Jaccard is left
// UNTOUCHED for its value/critic callers; only this retrieval-floor score is coverage-based.)
//
// STOP-WORD FILTER (deterministic): common English FUNCTION/STOP words (stopWords) are removed from BOTH the
// goal words and the op text BEFORE coverage is counted, so (1) a function word can never DECIDE a winner — two
// ops that overlap with the goal only on "the"/"of"/"a" both score 0, so the deterministic name tie-break
// (genuine relevance) stands; and (2) padding a description with the goal's FUNCTION words buys zero coverage.
// The filter never strips a real query term (only meaning-free function words are listed), so a genuinely
// relevant op's CONTENT-word coverage is unchanged.
//
// HONEST SCOPE — what this floor does and does NOT defend (the word "ungameable" is NOT earned and is NOT used):
//   - DEFENDED: MEANINGLESS / FUNCTION-WORD stuffing only. The stop-word filter zeros any overlap that is
//     purely function words, so stuffing "the/of/a/to/and" into a description buys exactly zero coverage and a
//     function word can never decide a winner.
//   - NOT DEFENDED (FATAL #1, owned by offerSemantic / the embedder seam): CONTENT-WORD stuffing. A junk op
//     whose text simply ECHOES the goal's content words (the purest content-word stuffing) reaches coverage 1.0
//     and can out-score a genuinely relevant op that covers fewer goal words. A coverage-based LEXICAL score
//     fundamentally cannot tell "contains the goal's words" from "means the thing" — only a meaning-aware
//     embedder (semantic similarity) can. This is the same residual as general recovery of NOVEL phrasings that
//     share no content word with the metadata: both are the embedder seam's job, not the lexical floor's.
//     RetrieverMode surfaces which channel is live so a silent lexical fallback is never mistaken for semantic
//     recovery. (TestStopWordFilterMakesScoreRobustToStuffing pins both sides: function-word stuffing is
//     zeroed; content-word / goal-echo stuffing is NOT defended and is asserted positively so the scope is
//     auditable.)
//
// An empty goal — or a goal that is ALL stop words — scores 0 (no content to cover). On an un-enriched op
// RetrievalText == "Name Intent", so the coverage over the bare CONTENT words is the relevant-op-still-ranks-in
// scoring the W3-S1 contract tests assert (structural).
func offerScore(goal, opText string) float64 {
	op := make(map[string]struct{})
	for _, w := range strings.Fields(strings.ToLower(opText)) {
		if isStopWord(w) {
			continue // stuffing a description with function words buys no coverage
		}
		op[w] = struct{}{}
	}
	seen := make(map[string]struct{})
	covered := 0
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		if isStopWord(w) {
			continue // a function word can never decide a winner
		}
		if _, dup := seen[w]; dup {
			continue // count each distinct CONTENT goal word once (set semantics)
		}
		seen[w] = struct{}{}
		if _, ok := op[w]; ok {
			covered++
		}
	}
	if len(seen) == 0 {
		return 0 // no content goal words to cover (empty or all-stop-word goal)
	}
	return float64(covered) / float64(len(seen))
}

// Minted returns a copy of the names minted this run, in mint order. Mirrors reading the Python
// registry.minted list (a copy so callers can't mutate the registry's internal slice).
func (r *OperatorRegistry) Minted() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.minted))
	copy(out, r.minted)
	return out
}

// stripWordChars removes hyphens and underscores, mirroring Python
// name.replace("-","").replace("_","") before the isalnum() check.
func stripWordChars(name string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(name)
}

// containsString reports whether s is in xs (the `name not in self.minted` guard).
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// familiesRepr renders the Families slice the way Python's f"{FAMILIES}" does for the tuple, so
// the Verify reason string matches the reference message. Python prints a tuple repr:
// ('transformative', 'relational', 'generative', 'primitive', 'synthesized').
func familiesRepr() string {
	parts := make([]string, len(Families))
	for i, f := range Families {
		parts[i] = "'" + f + "'"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// itoa is a tiny int->string used by enum String() fallbacks in this package (keeps the leaf
// cognition Tier-1 files from importing strconv just for a fallback path).
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
