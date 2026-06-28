// Package convert holds convertibility — learning to think cheaper (effortful -> automatic).
//
// The single mechanism behind "getting smart": a repeatedly-GENERATED pattern compiles into a
// silent PrimitiveSubAgent (fact/compute becomes injected); a repeated METACOG sequence compiles into a
// workflow / gate prior (control flow becomes automatic). Runs during IDLE/ASLEEP consolidation.
// Value-gated so it compiles signal, not noise (spec §9.5). This is the architecture's rest/sleep
// analogue.
//
// Ported from the (now-removed) Python thought_harness/convert.py. Tier 3 (depends on Tier-0/1/2 graph + events +
// types + the specialist value type). DEP-NARROW (P0-1): Python convert.py imports SubconsciousEngine
// (Tier 6) + MintedPrimitiveSubAgent (Tier 2). To stay Tier 3 and compile in order, Convertibility takes a
// PrimitiveSubAgentRegistrar interface (satisfied structurally by dispatch.SubconsciousEngine later) instead
// of importing dispatch, and constructs the MintedPrimitiveSubAgent from subconscious (Tier 2). The program
// + skills collaborators are likewise narrowed to local interfaces (Program / SkillMinter) so convert
// needs neither the program nor the skills package — the engine wires the concrete cognition types in
// (the same structural-satisfaction-later pattern as PrimitiveSubAgentRegistrar).
package convert

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/berttrycoding/thought-harness/internal/eval"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// PrimitiveSubAgentRegistrar is the narrow port convertibility needs from the subconscious engine: the
// ability to register a freshly-minted specialist. dispatch.SubconsciousEngine satisfies it
// structurally (its Register(subconscious.PrimitiveSubAgent) method) without convert importing dispatch
// (Tier 6) — the DEP-NARROW that keeps this module at Tier 3.
type PrimitiveSubAgentRegistrar interface {
	Register(subconscious.PrimitiveSubAgent)
}

// Program is the narrow port convertibility needs from a synthesised workflow program: its control-
// flow shape signature. cognition.Program satisfies it structurally (its Shape() string method), so
// convert never imports the program package.
type Program interface {
	Shape() string
}

// SkillMinter is the narrow port convertibility needs from the skill registry: promote a recurring
// Program into a named skill. cognition's skill registry satisfies it structurally (its
// Mint(name, triggers, body, description) method) — convert never imports the skills package. Mint
// returns ok=false when the mint was rejected (Python returned None), matching the engine-supplied
// registry which performs the verify+cycle-check.
type SkillMinter interface {
	Mint(name string, triggers []string, body Program, description string) (ok bool)
}

// pattern tracks a per-goal effortful-repetition record (Python _Pattern).
type pattern struct {
	goalKey     string
	triggers    []string
	generated   int     // how often this had to be worked out effortfully
	groundedVal float64 // the gated value of the branch the pattern converged on (the mint basis; max)
	lastVal     float64 // the LATEST grounded outcome (keep-or-revert): a refutation lowers it
	observed    bool    // whether lastVal has been set (so a default 0.0 isn't read as a refutation)
	minted      bool
	demoted     bool                                  // reverted by keep-or-revert after reality refuted it (P0.5)
	psa         *subconscious.MintedPrimitiveSubAgent // the minted primitive subagent (kept so revert can Demote it)
	answer      string                                // the REAL worked answer this pattern converged on (not a template)
	answerRaw   any                                   // its raw_return (a real ToolResult/observation), kept for trace-back
}

// programRun records a recurring synthesised PROGRAM shape per goal (Python program_runs dict).
type programRun struct {
	shape   string
	program Program
	count   int
	minted  bool
	// synthCost is the COMPLETION tokens spent RE-SYNTHESISING this shape across its recurrences (summed
	// from the synthesize_program llm.call stream via NoteSynthesisCost). It is the cache-immune COST a
	// minted skill would AVOID by recalling the converged Program instead of re-deriving it — the W5
	// efficiency basis. Accumulated unconditionally (cheap, signal-only); only CONSULTED by the trace->skill
	// mint when the cost gate is on. Zero on the offline test double (no llm.* events) or when no synthesis
	// cost was attributed — a cost-gate-ON run then holds the mint, the honest answer with no cost evidence.
	synthCost int
}

// tripleStat tracks a recurring (operator, source, domain) move — the M5 finest-grained convertibility
// key (representation-space-rebuild.md §1.5 + §5 M5): a hot (move + source) that keeps paying off is
// PAVED into a real primitive. Unlike `pattern` (per-goal, GENERATED-effort counting) a triple counts how
// often a GROUNDED move from a particular SOURCE was walked, so "deduction's validate@reality keeps
// holding" or "this domain's recall@memory keeps answering" compiles into an automatic specialist /
// knowledge record. The DoD-relevant fields mirror `pattern` so it rides the same value-gate + keep-or-
// revert rails.
type tripleStat struct {
	operator string  // the move (operator name)
	source   string  // the sourcing-ladder rung name the move drew from (a GROUNDED rung)
	domain   string  // the domain the move fired in
	count    int     // how often this grounded (op, source, domain) move was walked
	value    float64 // the peak grounded value it converged on (the mint basis)
	lastVal  float64 // the latest grounded outcome (keep-or-revert)
	observed bool    // whether lastVal has been set
	answer   string  // the real worked text the move produced (replayed by a minted primitive)
	minted   bool
	demoted  bool                                  // reverted by keep-or-revert after reality refuted it
	spec     *subconscious.MintedPrimitiveSubAgent // the minted specialist (kept so revert can Demote it)
}

// pathRun tracks a recurring PATH (the coarser mint key, §7 open-flag-6): a named directed traversal
// (analogy/induction/deduction) that keeps closing on a grounded DoD. It is coarser than the triple — it
// keys on the path NAME (not op@source@domain) so a path that pays off across domains is recognised. A
// hot, grounded path mints nothing new on its own here (the paths are already seed skills); it is tracked
// so the TUI/CLI can show which paths are being paved and so path-mint events surface. Kept value-gated +
// keep-or-revert like the others.
type pathRun struct {
	name     string
	count    int
	value    float64
	lastVal  float64
	observed bool
	grounded bool // the path closed on at least one grounded source this run (the DoD precondition)
	minted   bool
	demoted  bool
}

// Config is the convertibility gate (Python ConvertConfig). Defaults: mint after 3 effortful repeats,
// only patterns worth >= 0.2 (the value gate, §9.5), and a gate prior after 4 metacog ops.
type Config struct {
	MintAfter    int     // repeated effortful steps before compiling to a specialist (default 3)
	MintValue    float64 // value-gate: only compile patterns worth compiling (default 0.2, §9.5)
	MetacogAfter int     // repeated metacog ops before compiling a gate prior (default 4)
}

// DefaultConfig returns Python ConvertConfig's defaults.
func DefaultConfig() Config { return Config{MintAfter: 3, MintValue: 0.2, MetacogAfter: 4} }

// DefaultMintCostFloor is the per-shape COMPLETION-token floor the cost-aware trace->skill mint requires
// when the cost gate is on (W5: gate registry growth on the COST/efficiency ruler). A program shape is
// only worth promoting into an automatic skill if re-synthesising it has demonstrably cost real decode —
// below this floor the recurrence is cheap and a skill is filler. It is a conservative, deterministic
// floor (one synthesize_program call typically decodes hundreds of completion tokens); the calibrated
// value is the W5-1 cost-ruler MDE and overrides it via EnableCostGate(on, floor) when the campaign has
// it. Chosen so a single trivial synthesis never clears it but a genuinely effortful recurring shape does.
const DefaultMintCostFloor = 300.0

// Convertibility is the IDLE-time learner. It watches episode traces (Observe), notes recurring
// synthesised programs (NoteProgram), and at consolidation mints specialists / gate priors / skills
// for the converged patterns. Mirrors Python Convertibility.
//
// Insertion-order tracking: Python iterates dicts (patterns / program_runs / domain_tally) in
// insertion order, and max(d, key=d.get) breaks ties on the first-inserted key. Go map iteration is
// randomised, so a parallel order-slice is kept per map to reproduce the exact iteration + tie order.
type Convertibility struct {
	subconscious PrimitiveSubAgentRegistrar
	emit         events.Emit
	cfg          Config
	skills       SkillMinter // the skill registry (for trace->skill minting); nil disables it

	patterns     map[string]*pattern
	patternOrder []string // insertion order of pattern keys (Python patterns dict order)
	metacogRuns  int
	Minted       []string // domains of minted specialists (Python self.minted)
	Demoted      []string // domains of minted specialists reverted by keep-or-revert (P0.5)
	MintedSkill  []string // names of minted skills (Python self.minted_skills)
	programRuns  map[string]*programRun
	programOrder []string // insertion order of program_runs keys

	counted map[int]struct{} // thought ids already counted (idempotent Observe)

	// GatePrior is a learned standing bias domain -> +bias, compiled from recurring control habits
	// and CONSUMED by the engine (merged into the Gate's bias). A real prior, not just an event.
	GatePrior   map[string]float64
	domainTally map[string]float64 // which domains have been winning the gate
	domainOrder []string           // insertion order of domain_tally keys

	// the M5 (operator, source, domain) triple tally + the coarser path tally. tripleOrder/pathOrder
	// reproduce insertion order for deterministic iteration + tie-breaking (the same discipline as the
	// pattern/program/domain maps). MintedTriple/MintedPath surface what was paved (read by the TUI/CLI).
	triples      map[string]*tripleStat
	tripleOrder  []string
	paths        map[string]*pathRun
	pathOrder    []string
	MintedTriple []string         // domains of primitives minted from a hot grounded triple (op@source@domain)
	MintedPath   []string         // names of paths recognised as hot+grounded (the coarser mint key)
	countedTri   map[int]struct{} // thought ids already counted into the triple tally (idempotent)

	// mintGate is the eval-object MINT GATE (slice g, 01-subconscious.md §3.19): when set, every mint
	// candidate is additionally run through this MeasuringStick before Register — the uniform "does it
	// belong?" admission gate of record. nil ⇒ the frequency×value heuristic alone decides (today's
	// behaviour). mintHistory accumulates the gate's measurements so the comparative refine signal (§3.20)
	// reads a per-stick baseline; consolidateSeq is the deterministic logical tick (no wall clock).
	mintGate       *eval.MeasuringStick
	mintHistory    []eval.Measurement
	consolidateSeq int64

	// refineLoop is the uniform PER-REGISTRY self-improvement loop (§3.17/§3.20) applied to THIS
	// registry's minted entries — the generalisation of the mint gate (§3.19) from a one-shot
	// admission check into a STANDING refine pass. When set (the engine wires it behind
	// convert.refine_loop, default OFF), RefineRegistry runs it at idle consolidation: every minted
	// entry is measured against the mint-gate stick (absolute "does it still belong?") AND comparatively
	// vs its own past measurements (instance-eval) → an improve/keep/prune SIGNAL surfaced on the bus.
	// It is SIGNAL-ONLY: it never mutates the registry (keep-or-revert demotion stays the existing,
	// separately-gated mechanism). nil ⇒ no refine pass ⇒ byte-identical.
	refineLoop *eval.RefineLoop

	// costGate (W5 — gate registry growth on the COST/efficiency ruler, at the RUNTIME trace->skill mint):
	// when on, the trace->skill mint additionally requires the accumulated synthesis COST of a recurring
	// shape (synthCost, completion tokens re-derived) to clear mintCostFloor — the harness only AUTOMATES a
	// shape worth automating, declining a cheap recurrence even when it crosses MintAfter. It emits the cost
	// evidence (convert.cost_gate admit/hold). OFF ⇒ the count×value heuristic alone decides, no synthCost
	// consultation, no event ⇒ byte-identical (today's behaviour). Wired behind convert.cost_gate (default OFF).
	costGate      bool
	mintCostFloor float64

	// facts is the A-RAG5 convertibility-ON-FACTS state (docs/internal/notes/2026-06-20-rag-integration-analysis.md
	// §7.5): the CLS hippocampus→neocortex consolidation applied to RETRIEVED knowledge facts, not just
	// procedures. factsOn gates it (behind convert.facts, default OFF ⇒ nil tracking ⇒ byte-identical). The
	// loop is the SAME value × frequency gate + keep-or-revert rails the specialist/triple mints ride: a
	// knowledge fact RECALLED on enough high-value lines is migrated up to a durable PRIOR trust tier; a
	// promoted fact whose latest line reality refutes is reverted. factStats/factOrder reproduce insertion
	// order for deterministic iteration (the same discipline as the other tallies); recalledThisEpisode is
	// the per-episode recall set (a fact recalled MULTIPLE times in one episode counts ONCE — the unit is
	// the high-value EXPERIENCE the fact was active in, the CLS consolidation basis). FactsPromoted/
	// FactsReverted surface what was consolidated (read by the TUI/CLI/tests).
	factsOn             bool
	factStats           map[string]*factStat
	factOrder           []string
	recalledThisEpisode map[string]struct{}
	FactsPromoted       []string // statements consolidated into a prior (A-RAG5)
	FactsReverted       []string // promoted statements reverted by keep-or-revert (reality refuted the line)
}

// factStat tracks a per-FACT recall × value record (A-RAG5). It mirrors the pattern/triple shape so it
// rides the SAME value-gate + keep-or-revert rails: recalls counts how many high-value EPISODES the fact
// was active in (the consolidation frequency), peakValue is the best line value it served (the mint
// basis), lastVal is the LATEST line value (keep-or-revert: a refutation lowers it), promoted/reverted are
// the consolidation lifecycle flags.
type factStat struct {
	statement string
	recalls   int     // distinct high-value episodes the fact was recalled in (the frequency gate)
	peakValue float64 // best line value the fact served (the mint basis)
	lastVal   float64 // latest line value (keep-or-revert: a reality refutation lowers it)
	observed  bool    // whether lastVal has been set (so a default 0.0 is not read as a refutation)
	promoted  bool
	reverted  bool
}

// FactPromoter is the narrow port convertibility-on-facts needs from the durable knowledge registry: the
// ability to migrate a recalled fact UP to a prior trust tier (Promote) and to REVERT that on refutation
// (DemoteFact). knowledge.KnowledgeRegistry satisfies it structurally — convert never imports knowledge
// (the same DEP-NARROW pattern as PrimitiveSubAgentRegistrar / SkillMinter). A nil promoter disables the
// consolidate pass (the engine wires the live registry behind convert.facts).
type FactPromoter interface {
	Promote(statement string, toTrust float64, recalls int, value float64) (n int, fromTrust, gotTrust float64)
	DemoteFact(statement string, toTrust float64, nowTick int) int
}

// New builds a Convertibility bound to the subconscious registrar + emit closure. Pass cfg=nil for
// the Python defaults; pass skills=nil to disable trace->skill minting. Mirrors Python
// Convertibility.__init__.
func New(back PrimitiveSubAgentRegistrar, emit events.Emit, cfg *Config, skills SkillMinter) *Convertibility {
	c := DefaultConfig()
	if cfg != nil {
		c = *cfg
	}
	return &Convertibility{
		subconscious: back,
		emit:         emit,
		cfg:          c,
		skills:       skills,
		patterns:     map[string]*pattern{},
		programRuns:  map[string]*programRun{},
		counted:      map[int]struct{}{},
		GatePrior:    map[string]float64{},
		domainTally:  map[string]float64{},
		Minted:       []string{},
		Demoted:      []string{},
		MintedSkill:  []string{},
		triples:      map[string]*tripleStat{},
		paths:        map[string]*pathRun{},
		MintedTriple: []string{},
		MintedPath:   []string{},
		countedTri:   map[int]struct{}{},

		factStats:           map[string]*factStat{},
		recalledThisEpisode: map[string]struct{}{},
		FactsPromoted:       []string{},
		FactsReverted:       []string{},
	}
}

// NoteProgram records that a workflow PROGRAM of a given shape was synthesised for this goal. A
// program shape that recurs (same goal family, same control-flow shape) is a candidate to promote
// into a named SKILL — the trace->skill convertibility (effortful synthesis -> automatic recall).
// Mirrors Python note_program.
func (c *Convertibility) NoteProgram(goal string, program Program) {
	key := goalKey(goal)
	shape := program.Shape()
	rec := c.programRuns[key]
	if rec == nil || rec.shape != shape {
		if rec == nil {
			c.programOrder = append(c.programOrder, key)
		}
		c.programRuns[key] = &programRun{shape: shape, program: program, count: 1, minted: false}
	} else {
		rec.count++
		rec.program = program
	}
}

// EnableCostGate turns the W5 cost-aware trace->skill mint gate on/off and sets the per-shape
// completion-token floor (floor<=0 ⇒ DefaultMintCostFloor). When on, Consolidate's trace->skill mint
// additionally requires a recurring shape's accumulated synthesis cost (NoteSynthesisCost) to clear the
// floor before it is promoted to a skill, emitting convert.cost_gate admit/hold. OFF ⇒ byte-identical
// (the count×value heuristic alone decides, no cost consultation, no event). The engine wires this from
// the convert.cost_gate knob; nothing else flips it.
func (c *Convertibility) EnableCostGate(on bool, floor float64) {
	c.costGate = on
	if floor <= 0 {
		floor = DefaultMintCostFloor
	}
	c.mintCostFloor = floor
}

// NoteSynthesisCost attributes COMPLETION tokens spent RE-SYNTHESISING a program shape to that shape's
// recurring run (keyed by goal). The engine calls it after a fresh synthesis with the synthesize_program
// completion tokens spent this episode (summed from the llm.call stream); a library-skill RECALL spends
// none, so nothing is attributed and the recalled shape pays no further cost. It is always safe to call —
// it only ACCUMULATES the cost (signal-only); the trace->skill mint CONSULTS it only when the cost gate is
// on. A NoteSynthesisCost for a goal with no tracked run yet (no preceding NoteProgram) is dropped — the
// run is the unit the cost attaches to.
func (c *Convertibility) NoteSynthesisCost(goal string, completionTokens int) {
	if completionTokens <= 0 {
		return
	}
	if rec := c.programRuns[goalKey(goal)]; rec != nil {
		rec.synthCost += completionTokens
	}
}

// stopwords are common English function words dropped from goalKey (and thus from the derived skill
// triggers). Without this filter a goal led by a stopword ("the quick brown fox") mints a skill whose
// trigger set includes "the", and because skills.MatchScore is a case-insensitive SUBSTRING test that
// "the" trigger fires on almost any goal — the minted skill OVER-FIRES on unrelated work and short-
// circuits Synthesize spuriously. Dropping function words keeps a learned trigger CONTENTful. The set
// is fixed/deterministic (no RNG, no locale): the closed-class English function words a goal restating
// a task rarely carries as distinguishing content. Determinism is preserved — a map literal is content,
// not iteration-order, since goalKey only membership-tests it.
var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {}, "from": {}, "into": {},
	"are": {}, "was": {}, "were": {}, "has": {}, "had": {}, "have": {}, "will": {}, "would": {},
	"can": {}, "could": {}, "should": {}, "than": {}, "then": {}, "them": {}, "they": {}, "their": {},
	"its": {}, "our": {}, "your": {}, "you": {}, "but": {}, "not": {}, "all": {}, "any": {},
	"some": {}, "such": {}, "what": {}, "when": {}, "which": {}, "while": {}, "who": {}, "whom": {},
	"how": {}, "why": {}, "where": {}, "over": {}, "under": {}, "out": {}, "off": {}, "per": {},
	"via": {}, "onto": {}, "upon": {}, "about": {}, "above": {}, "below": {}, "between": {}, "each": {},
	"both": {}, "more": {}, "most": {}, "other": {}, "only": {}, "same": {}, "very": {}, "just": {},
}

// goalKey is the coarse per-goal key (Python Convertibility._goal_key): the first three alphabetic
// CONTENT words longer than 2 chars, lower-cased, with common function words (the/and/for/with/…)
// dropped (the stopword filter, so a stopword-led goal does not mint an over-firing trigger); else the
// first 24 chars of the lower-cased goal.
func goalKey(goal string) string {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		if len([]rune(w)) <= 2 || !isAlpha(w) {
			continue
		}
		if _, stop := stopwords[w]; stop {
			continue // drop a common function word — it is not distinguishing content
		}
		words = append(words, w)
	}
	if len(words) > 0 {
		if len(words) > 3 {
			words = words[:3]
		}
		return strings.Join(words, " ")
	}
	low := strings.ToLower(goal)
	r := []rune(low)
	if len(r) > 24 {
		return string(r[:24])
	}
	return low
}

// isAlpha reports whether w is non-empty and every rune is a unicode letter (Python str.isalpha —
// true only for a non-empty all-alphabetic string).
func isAlpha(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// Observe watches an episode's trace: count effortful generation and metacog usage per goal, tally
// the gate-winning domain, attribute the gated value, and capture the REAL worked conclusion. Mirrors
// Python observe.
func (c *Convertibility) Observe(g *graph.ThoughtGraph) {
	key := goalKey(g.Goal)
	p := c.patterns[key]
	if p == nil {
		p = &pattern{goalKey: key, triggers: strings.Fields(key)}
		c.patterns[key] = p
		c.patternOrder = append(c.patternOrder, key)
	}
	goalID := minNodeID(g)
	for _, t := range g.ActiveContext() {
		// M5: tally the (operator, source, domain) triple for every GROUNDED move, idempotently. This is
		// SEPARATE from the GENERATED-effort counting below (a grounded move is the opposite of effort —
		// it is the source paying off) so it uses its own counted set and runs before the skip-if-counted
		// guard returns.
		if _, seenTri := c.countedTri[t.ID]; !seenTri {
			c.countedTri[t.ID] = struct{}{}
			c.tallyTriple(t, g.Active().Epistemic) // pattern worth = content quality, never the priority term
		}
		if _, seen := c.counted[t.ID]; seen {
			continue // idempotent: don't re-count on a repeated/soft STOP
		}
		c.counted[t.ID] = struct{}{}
		if t.Source == types.GENERATED && t.ID != goalID {
			p.generated++
		}
		if t.Source == types.METACOG {
			c.metacogRuns++
		}
		// tally which domain has been WINNING the gate (the INJECTED thought's source domain) — the
		// recurring winner is what a compiled gate prior will privilege. Python reads
		// getattr(t.raw_return, "domain", None) on an INJECTED thought; the Go raw_return for an
		// injected thought is a *types.Candidate, whose Domain is *string.
		if t.Source == types.INJECTED {
			if dom := injectedDomain(t.RawReturn); dom != "" {
				if _, seen := c.domainTally[dom]; !seen {
					c.domainOrder = append(c.domainOrder, dom)
				}
				c.domainTally[dom] += 1.0
			}
		}
	}
	// attribute value from the branch the pattern actually converged on (the active branch), not the
	// best unrelated sibling — the gated signal must reflect the pattern's worth. The EPISTEMIC
	// projection (no pending-user term): a failure episode on an urgent line must never mint — the
	// priority-inflated Value once compiled "couldn't reach an answer" into a learned reflex.
	av := g.Active().Epistemic
	if av > p.groundedVal {
		p.groundedVal = av
	}
	// keep-or-revert (P0.5): also remember the LATEST grounded outcome. groundedVal is the monotonic
	// peak that GATED the mint; lastVal is what reality said THIS time — if a re-encounter of an
	// already-minted pattern comes back below the floor, Consolidate reverts the mint.
	p.lastVal = av
	p.observed = true
	// Capture the REAL worked answer this pattern converged on (so a minted specialist later replays
	// the genuine prior work, not a placeholder template).
	if concl := conclusion(g); concl != nil {
		p.answer = types.StripVoice(concl.Text)
		p.answerRaw = concl.RawReturn
	}
}

// injectedDomain returns the domain tag carried on an INJECTED thought's raw_return, or "". The
// Python raw_return for an injected thought is the producing Candidate; getattr(.., "domain", None)
// reads its domain. Here the closed RawReturn union holds a *types.Candidate.
func injectedDomain(raw any) string {
	if cnd, ok := raw.(*types.Candidate); ok && cnd != nil && cnd.Domain != nil {
		return *cnd.Domain
	}
	return ""
}

// tallyTriple records a GROUNDED move into the (operator, source, domain) tally (M5). A move counts iff
// it is grounded — an OBSERVATION (reality, source #4) or an INJECTED candidate whose concretization
// stamped a GROUNDED FuelProvenance (present/knowledge/memory/reality). The bare GENERATED rung is NOT
// grounded and is excluded (it is effort, counted by `pattern`, not a source paying off). The value is
// the active branch's gated value — the same mint basis the pattern tally uses. Deterministic.
func (c *Convertibility) tallyTriple(t types.Thought, branchValue float64) {
	op, source, domain, text, ok := tripleOf(t)
	if !ok {
		return // not a grounded sourced move
	}
	key := op + "|" + source + "|" + domain
	tr := c.triples[key]
	if tr == nil {
		tr = &tripleStat{operator: op, source: source, domain: domain}
		c.triples[key] = tr
		c.tripleOrder = append(c.tripleOrder, key)
	}
	tr.count++
	if branchValue > tr.value {
		tr.value = branchValue
	}
	tr.lastVal = branchValue
	tr.observed = true
	if strings.TrimSpace(text) != "" {
		tr.answer = types.StripVoice(text)
	}
}

// tripleOf extracts a grounded (operator, source, domain) move from a thought, with ok=false when the
// thought is not a grounded sourced move. It reads the closed RawReturn/Payload union:
//
//   - an OBSERVATION thought is reality (source #4): operator "observe" unless its producing candidate
//     carried one; domain from the candidate if present.
//   - an INJECTED thought whose producing *types.Candidate.Payload is a GROUNDED FuelProvenance is a
//     sourced move: operator from the candidate's Operator, source = the FuelProvenance rung, domain from
//     the candidate. A GENERATED-rung (ungrounded) provenance is excluded.
//
// Everything else (a bare GENERATED thought, a METACOG note) returns ok=false.
func tripleOf(t types.Thought) (op, source, domain, text string, ok bool) {
	switch t.Source {
	case types.OBSERVATION:
		// reality ground truth — the highest-trust source.
		op = "observe"
		if cnd, isC := t.RawReturn.(*types.Candidate); isC && cnd != nil {
			if cnd.Operator != nil {
				op = opName(*cnd.Operator)
			}
			if cnd.Domain != nil {
				domain = *cnd.Domain
			}
		}
		return op, "reality", domain, t.Text, true
	case types.INJECTED:
		cnd, isC := t.RawReturn.(*types.Candidate)
		if !isC || cnd == nil {
			return "", "", "", "", false
		}
		prov, isP := cnd.Payload.(types.FuelProvenance)
		if !isP || !prov.Grounded {
			return "", "", "", "", false // not a grounded sourced move (the generated rung is excluded)
		}
		op = "generate"
		if cnd.Operator != nil {
			op = opName(*cnd.Operator)
		}
		if cnd.Domain != nil {
			domain = *cnd.Domain
		}
		return op, prov.Source, domain, t.Text, true
	default:
		return "", "", "", "", false
	}
}

// opName maps an Operator enum to a stable lowercase name for the triple key. The enum is coarse (it does
// not round-trip every operator name), so this is a representative-name table for the keys the tally cares
// about; an unmapped enum falls back to "move".
func opName(o types.Operator) string {
	switch o {
	case types.DECOMPOSE:
		return "decompose"
	case types.GENERATE:
		return "generate"
	case types.VALIDATE:
		return "validate"
	case types.COMPARE:
		return "compare"
	case types.GENERALIZE:
		return "generalize"
	case types.ABSTRACT:
		return "abstract"
	case types.SIMULATE:
		return "hypothesize"
	default:
		return "move"
	}
}

// NotePath records that a named PATH (analogy/induction/deduction) ran this episode and whether it closed
// on a GROUNDED source (the DoD precondition). The engine calls it when a path skill drove the program;
// value is the path's converged gated value. A hot, grounded path is surfaced at Consolidate (the coarser
// mint key, §7 open-flag-6) — value-gated + keep-or-revert like the triple.
func (c *Convertibility) NotePath(name string, grounded bool, value float64) {
	pr := c.paths[name]
	if pr == nil {
		pr = &pathRun{name: name}
		c.paths[name] = pr
		c.pathOrder = append(c.pathOrder, name)
	}
	pr.count++
	if value > pr.value {
		pr.value = value
	}
	pr.lastVal = value
	pr.observed = true
	if grounded {
		pr.grounded = true
	}
}

// conclusion returns the thought a pattern converged ON — prefer a reality OBSERVATION (ground
// truth), else the most confident concluding thought. Skips METACOG bookkeeping and a bare goal
// restatement. Mirrors Python _conclusion.
func conclusion(g *graph.ThoughtGraph) *types.Thought {
	goal := strings.TrimSpace(g.Goal)
	real := []types.Thought{}
	for _, t := range g.ActiveContext() {
		txt := strings.TrimSpace(t.Text)
		if t.Source != types.METACOG && txt != "" && txt != goal {
			real = append(real, t)
		}
	}
	if len(real) == 0 {
		return nil
	}
	// obs = [t for t in real if OBSERVATION]; Python returns obs[-1] (the LAST observation) if any.
	var lastObs *types.Thought
	for i := range real {
		if real[i].Source == types.OBSERVATION {
			lastObs = &real[i]
		}
	}
	if lastObs != nil {
		return lastObs
	}
	// else max(real, key=confidence). Python max keeps the running best only on a strictly-greater
	// comparison, so it returns the FIRST element on a confidence tie.
	best := &real[0]
	for i := 1; i < len(real); i++ {
		if real[i].Confidence > best.Confidence {
			best = &real[i]
		}
	}
	return best
}

// minNodeID returns the smallest node id in the graph (Python min(graph.nodes), the root/goal id).
func minNodeID(g *graph.ThoughtGraph) int {
	first := true
	m := 0
	for id := range g.Nodes {
		if first || id < m {
			m = id
			first = false
		}
	}
	return m
}

// Consolidate is the IDLE-time compilation: mint specialists / gate priors / skills for converged
// patterns. Returns the domains minted on THIS pass (Python consolidate returns minted_now). Mirrors
// Python consolidate exactly, including iteration order.
// SetMintGate attaches the eval-object mint gate (slice g, §3.19): when set, Consolidate runs every mint
// candidate through this stick before Register — the principled "does it belong?" admission gate, plus a
// comparative refine signal (§3.20) against the gate's own measurement history. Pass nil to remove it
// (the frequency×value heuristic alone then decides).
func (c *Convertibility) SetMintGate(gate *eval.MeasuringStick) { c.mintGate = gate }

// EnableRefineLoop turns the uniform per-registry refine loop on/off for THIS registry (§3.17/§3.20).
// When on, RefineRegistry runs a standing eval.RefineLoop over the minted entries at idle
// consolidation — measurement-only, no mutation. The engine wires this behind convert.refine_loop
// (default OFF ⇒ no loop ⇒ byte-identical). epsilon is the comparative dead-band (noise floor).
// Idempotent: a second enable preserves the accumulated per-reference history.
func (c *Convertibility) EnableRefineLoop(on bool, epsilon float64) {
	if !on {
		c.refineLoop = nil
		return
	}
	if c.refineLoop == nil {
		c.refineLoop = eval.NewRefineLoop(epsilon)
	}
}

// mintedStickRegistry adapts THIS convertibility's minted-specialist entries to eval.RefinableRegistry
// so the generic refine loop can measure them. The subject of each entry is the pattern's LATEST
// grounded value (lastVal once observed, else the mint basis groundedVal) — the same float64 the
// mint-gate stick reads, so the registry's mint gate IS its refine stick (§3.19 = the absolute bar of
// the same loop). Entries are emitted in pattern insertion order (deterministic, no map iteration).
type mintedStickRegistry struct{ c *Convertibility }

func (m mintedStickRegistry) Name() string { return "specialist" }

func (m mintedStickRegistry) Stick() (eval.MeasuringStick, bool) {
	if m.c.mintGate == nil {
		return eval.MeasuringStick{}, false
	}
	return *m.c.mintGate, true
}

func (m mintedStickRegistry) Entries() []eval.RefineEntry {
	var out []eval.RefineEntry
	for _, key := range m.c.patternOrder {
		p := m.c.patterns[key]
		if p == nil || !p.minted || p.demoted {
			continue // only live minted entries belong to the registry
		}
		v := p.groundedVal
		if p.observed {
			v = p.lastVal // the latest grounded outcome is the current standing of the reference
		}
		out = append(out, eval.RefineEntry{ID: "learned:" + p.goalKey, Subject: v})
	}
	return out
}

// RefineRegistry runs ONE uniform per-registry refine pass over the minted-specialist registry
// (§3.17): measure every live minted entry against the mint-gate stick (absolute) AND comparatively
// vs its own past (instance-eval), and surface the per-entry improve/keep/prune SIGNAL on the bus. It
// is SIGNAL-ONLY — it never mutates the registry (the keep-or-revert demotion path stays separate). A
// no-op when the loop is disabled or no mint gate is attached (⇒ byte-identical). Returns the report
// for callers/tests; emits a per-registry summary + one event per non-Keep entry.
func (c *Convertibility) RefineRegistry() (eval.RefineReport, bool) {
	if c.refineLoop == nil || c.mintGate == nil {
		return eval.RefineReport{}, false
	}
	rep := c.refineLoop.Refine(mintedStickRegistry{c})
	if len(rep.Entries) == 0 {
		return rep, false
	}
	improve, keep, prune := rep.Counts()
	if c.emit != nil {
		c.emit(events.RegistryRefine,
			"registry '"+rep.Registry+"' refine: "+strconv.Itoa(improve)+" improve / "+
				strconv.Itoa(keep)+" keep / "+strconv.Itoa(prune)+" prune",
			events.D{"kind": "registry_refine", "registry": rep.Registry, "stick": rep.Stick,
				"improve": improve, "keep": keep, "prune": prune, "prunable": rep.Prunable()})
		for _, e := range rep.Entries {
			if e.Verdict == eval.Keep {
				continue // surface only the actionable signals (improve / prune), not the steady state
			}
			c.emit(events.RegistryRefine,
				"refine entry '"+e.ID+"' "+e.Verdict.String()+" (value "+format2f(e.Measurement.Score.Value)+
					", refine "+e.Refine.Direction.String()+")",
				events.D{"kind": "refine_entry", "id": e.ID, "pass": e.Pass,
					"verdict": e.Verdict.String(), "refine": e.Refine.Direction.String(), "delta": e.Refine.Delta})
		}
	}
	return rep, true
}

// admitByGate runs a mint candidate (identified by domain, scored by its grounded value) through the eval
// mint gate (§3.19) and surfaces the comparative refine signal (§3.20). Returns true to admit. A no-op
// admit (true) when no gate is attached — the heuristic gate upstream already decided. On a measurement it
// emits a convert event carrying the verdict + the refine direction, and records the measurement so the
// next candidate's baseline includes it.
//
// This is the FIRST CONCRETE INSTANCE of the uniform §3.17 refine loop (the convert/specialist registry's
// self-improvement step): it runs eval.SingleRefine — the SAME atomic benchmark+comparative kernel the
// generic eval.RefineLoop runs — with the convert registry's own history-grouping POLICY (a single pooled
// mintHistory keyed by the one "mint-gate" stick, i.e. each candidate measured against ALL past mints, not
// per-subject). The generalisation SUBSUMES this path rather than duplicating it.
func (c *Convertibility) admitByGate(domain string, groundedVal float64) bool {
	if c.mintGate == nil {
		return true
	}
	c.consolidateSeq++
	// comparative signal vs the gate's own pooled history (§3.20); epsilon 0 = strict (convert's policy).
	admit, refine, m := eval.SingleRefine(*c.mintGate, domain, groundedVal, c.consolidateSeq, c.mintHistory, 0.0)
	c.mintHistory = append(c.mintHistory, m)
	if c.emit != nil {
		verdict := "admit"
		if !admit {
			verdict = "reject"
		}
		c.emit(events.Convert,
			"mint gate "+verdict+" '"+domain+"' (value "+format2f(m.Score.Value)+", refine "+refine.Direction.String()+")",
			events.D{"kind": "mint_gate", "domain": domain, "admit": admit,
				"value": m.Score.Value, "refine": refine.Direction.String(), "delta": refine.Delta})
	}
	return admit
}

func (c *Convertibility) Consolidate() []string {
	mintedNow := []string{}
	// Iterate patterns in insertion order — Python dict values() is insertion-ordered.
	for _, key := range c.patternOrder {
		p := c.patterns[key]
		// keep-or-revert (P0.5): a mint that reality LATER refuted — the latest grounded outcome fell
		// below the mint floor — is reverted. Its specialist stops firing and the pattern's standing
		// value drops to the refuting outcome (below the floor), so the discredited answer is no longer
		// injected. A repeated GENERATED pattern earned the mint; a grounded refutation takes it back.
		if p.minted && !p.demoted && p.observed && p.psa != nil && p.lastVal < c.cfg.MintValue {
			p.psa.Demote()
			p.demoted = true
			p.groundedVal = p.lastVal // the mint's standing value drops below the floor (reverted)
			c.Demoted = append(c.Demoted, p.psa.Domain())
			if c.emit != nil {
				// "Specialist" in the SUMMARY text + the "kind":"specialist" data value are stable WIRE
				// values (golden-pinned + the convert.mint event vocabulary), NOT renamed with the Go
				// PrimitiveSubAgent symbol. See the rename note on the iface.
				c.emit(events.Convert,
					"demoted Specialist '"+p.psa.Domain()+"' — reality refuted it (value "+
						format2f(p.lastVal)+" < floor "+format2f(c.cfg.MintValue)+") (keep-or-revert)",
					events.D{"kind": "demote", "domain": p.psa.Domain(),
						"value": p.lastVal, "floor": c.cfg.MintValue})
			}
			continue
		}
		// mint = frequency × value (spec §9.5): repeated AND worth compiling, else it's noise.
		if p.minted || p.generated < c.cfg.MintAfter || p.groundedVal < c.cfg.MintValue {
			continue
		}
		domain := "learned:" + p.goalKey
		// slice g (§3.19): the eval mint gate has the FINAL say on admission when attached — a candidate
		// that clears frequency×value still must clear the stick. nil gate ⇒ admit (heuristic decided).
		if !c.admitByGate(domain, p.groundedVal) {
			continue
		}
		// Replay the REAL worked answer captured when the pattern converged — not a placeholder.
		answer := p.answer
		if answer == "" {
			answer = "(recalled, now automatic) the worked answer for '" + p.goalKey + "'"
		}
		sp := subconscious.NewMintedPrimitiveSubAgent(domain, p.triggers, answer, 0.9)
		c.subconscious.Register(sp)
		p.psa = sp // kept so keep-or-revert can Demote it if reality later refutes the pattern
		p.minted = true
		c.Minted = append(c.Minted, domain)
		mintedNow = append(mintedNow, domain)
		if c.emit != nil {
			c.emit(events.Convert,
				"minted Specialist '"+domain+"' from "+strconv.Itoa(p.generated)+
					" effortful repeats (GENERATED -> INJECTED)",
				events.D{"kind": "specialist", "domain": domain, "generated": p.generated})
		}
	}
	// M5: PAVE a hot grounded (operator, source, domain) triple into a real primitive specialist (the
	// "convertibility compiles a hot (move + source) into a real primitive" rail, §1.5). A triple that
	// recurred (>= MintAfter), converged on real value (>= MintValue), and is GROUNDED mints a specialist
	// that replays the move's worked answer automatically next time. Value-gated + keep-or-revert: a
	// re-encounter below the floor reverts the mint (the same Demote lineage as the pattern path).
	for _, key := range c.tripleOrder {
		tr := c.triples[key]
		// keep-or-revert: a minted triple that reality later refuted (latest grounded value < floor) is
		// reverted — its specialist stops firing and its standing value drops below the floor.
		if tr.minted && !tr.demoted && tr.observed && tr.spec != nil && tr.lastVal < c.cfg.MintValue {
			tr.spec.Demote()
			tr.demoted = true
			tr.value = tr.lastVal
			c.Demoted = append(c.Demoted, tr.spec.Domain())
			if c.emit != nil {
				c.emit(events.Convert,
					"demoted primitive '"+tr.spec.Domain()+"' — reality refuted the "+tr.source+
						" move (value "+format2f(tr.lastVal)+" < floor "+format2f(c.cfg.MintValue)+")",
					events.D{"kind": "demote", "domain": tr.spec.Domain(), "source": tr.source,
						"value": tr.lastVal, "floor": c.cfg.MintValue})
			}
			continue
		}
		if tr.minted || tr.count < c.cfg.MintAfter || tr.value < c.cfg.MintValue {
			continue
		}
		domain := "primitive:" + tr.operator + "@" + tr.source
		if tr.domain != "" {
			domain += ":" + tr.domain
		}
		// slice g (§3.19): the same eval mint gate gates a paved primitive.
		if !c.admitByGate(domain, tr.value) {
			continue
		}
		answer := tr.answer
		if answer == "" {
			answer = "(paved, now automatic) the " + tr.source + " " + tr.operator + " move"
		}
		triggers := tripleTriggers(tr)
		sp := subconscious.NewMintedPrimitiveSubAgent(domain, triggers, answer, 0.9)
		c.subconscious.Register(sp)
		tr.spec = sp
		tr.minted = true
		c.MintedTriple = append(c.MintedTriple, domain)
		mintedNow = append(mintedNow, domain)
		if c.emit != nil {
			c.emit(events.Convert,
				"paved primitive '"+domain+"' from "+strconv.Itoa(tr.count)+" grounded "+tr.source+
					" moves (hot triple -> automatic)",
				events.D{"kind": "triple", "domain": domain, "operator": tr.operator,
					"source": tr.source, "count": tr.count, "value": round3(tr.value)})
		}
	}
	// M5: surface a hot GROUNDED path (the coarser mint key, §7 open-flag-6). A path that recurred,
	// converged on value, AND closed on a grounded source is "paved" — it emits convert.path_mint so the
	// TUI/CLI can show which directed traversals keep paying off. The seed paths are already library
	// skills, so this records-and-surfaces (it does not re-mint the skill); keep-or-revert still applies.
	for _, name := range c.pathOrder {
		pr := c.paths[name]
		if pr.minted && !pr.demoted && pr.observed && pr.lastVal < c.cfg.MintValue {
			pr.demoted = true
			pr.value = pr.lastVal
			if c.emit != nil {
				c.emit(events.PathMint,
					"demoted path '"+name+"' — it stopped closing on a grounded DoD (value "+
						format2f(pr.lastVal)+" < floor "+format2f(c.cfg.MintValue)+")",
					events.D{"kind": "demote", "path": name, "value": pr.lastVal, "floor": c.cfg.MintValue})
			}
			continue
		}
		if pr.minted || pr.count < c.cfg.MintAfter || pr.value < c.cfg.MintValue || !pr.grounded {
			continue // a model-only-DoD path is recorded but never paved (recombination can be wrong)
		}
		pr.minted = true
		c.MintedPath = append(c.MintedPath, name)
		if c.emit != nil {
			c.emit(events.PathMint,
				"paved path '"+name+"' from "+strconv.Itoa(pr.count)+" grounded traversals",
				events.D{"kind": "path", "path": name, "count": pr.count,
					"grounded": pr.grounded, "value": round3(pr.value)})
		}
	}
	if c.metacogRuns >= c.cfg.MetacogAfter && len(c.domainTally) > 0 {
		// Compile a REAL gate prior: privilege the domain that has been winning the gate (a standing
		// bias the engine merges into the Gate). Bounded so a habit can't dominate admission. This is
		// the METACOG -> automatic-control-habit path, made measurable.
		top := c.topDomain()
		c.GatePrior[top] = round3(math.Min(0.3, c.GatePrior[top]+0.1))
		if c.emit != nil {
			c.emit(events.Convert,
				"compiled a gate prior from "+strconv.Itoa(c.metacogRuns)+" metacog ops: +bias to '"+
					top+"' (now "+format2f(c.GatePrior[top])+") (METACOG -> gate habit)",
				events.D{"kind": "gate_prior", "metacog": c.metacogRuns, "domain": top,
					"bias": c.GatePrior[top]})
		}
		c.metacogRuns = 0
	}
	// trace -> skill: a program shape that recurred (and is worth it) becomes a named library skill,
	// so the next matching goal recalls it instead of re-synthesising.
	if c.skills != nil {
		for _, key := range c.programOrder {
			rec := c.programRuns[key]
			worth := c.patterns[key]
			if rec.minted || rec.count < c.cfg.MintAfter ||
				(worth != nil && worth.groundedVal < c.cfg.MintValue) {
				continue
			}
			// W5 COST GATE: a shape may recur and converge on value yet be CHEAP to re-synthesise — promoting
			// it then mints filler that saves nothing (the W5 efficiency basis: a skill is worth minting only
			// when it AVOIDS real re-synthesis cost). When the cost gate is on, hold the mint until the
			// accumulated synthesis cost clears the floor, emitting the cost evidence either way. OFF ⇒ the
			// block below this is unchanged (no synthCost consultation, no event) ⇒ byte-identical.
			if c.costGate {
				if float64(rec.synthCost) < c.mintCostFloor {
					if c.emit != nil {
						c.emit(events.CostGate,
							"held trace->skill mint for '"+key+"': re-synthesis cost "+strconv.Itoa(rec.synthCost)+
								" completion-tok below floor "+format2f(c.mintCostFloor)+" (too cheap to automate)",
							events.D{"kind": "hold", "goal": key, "cost": rec.synthCost,
								"floor": round3(c.mintCostFloor), "count": rec.count})
					}
					continue
				}
				if c.emit != nil {
					c.emit(events.CostGate,
						"admitted trace->skill mint for '"+key+"': re-synthesis cost "+strconv.Itoa(rec.synthCost)+
							" completion-tok cleared floor "+format2f(c.mintCostFloor)+" (worth automating)",
						events.D{"kind": "admit", "goal": key, "cost": rec.synthCost,
							"floor": round3(c.mintCostFloor), "count": rec.count})
				}
			}
			name := "learned-" + truncate(strings.ReplaceAll(key, " ", "-"), 24)
			triggers := strings.Fields(key)
			if c.skills.Mint(name, triggers, rec.program,
				"learned from "+strconv.Itoa(rec.count)+" repeated programs") {
				rec.minted = true
				c.MintedSkill = append(c.MintedSkill, name)
				if c.emit != nil {
					c.emit(events.SkillMint,
						"minted Skill '"+name+"' from "+strconv.Itoa(rec.count)+
							" repeated programs (synthesised -> automatic)",
						events.D{"kind": "skill", "name": name, "count": rec.count})
				}
			}
		}
	}
	return mintedNow
}

// tripleTriggers derives the context words that light up a triple's minted primitive: its domain (the
// strongest cue) plus its operator name, so a later move in the same domain recalls the paved primitive.
// Deterministic (no map iteration).
func tripleTriggers(tr *tripleStat) []string {
	var trig []string
	if tr.domain != "" {
		trig = append(trig, strings.ToLower(tr.domain))
	}
	trig = append(trig, tr.operator)
	return trig
}

// topDomain returns the domain with the highest tally; Python max(d, key=d.get) breaks ties by the
// FIRST-inserted key (it keeps the running best only on a strictly-greater comparison, iterating in
// insertion order). domainOrder reproduces that insertion order deterministically.
func (c *Convertibility) topDomain() string {
	top := ""
	best := math.Inf(-1)
	for _, dom := range c.domainOrder {
		if v := c.domainTally[dom]; v > best {
			best = v
			top = dom
		}
	}
	return top
}

// -- read-only introspection (the convertibility mechanism made observable) ------------------------
//
// These expose the LIVE learning state — the patterns/programs being tracked toward minting and the
// gate's domain tally — so a UI can watch effortful→automatic happen (not just the minted results).

// PatternStat is a plain-data snapshot of one tracked effortful pattern (a mint candidate).
type PatternStat struct {
	GoalKey   string
	Generated int     // effortful repeats so far (mints at >= MintAfter)
	Value     float64 // the grounded value it converged on (value-gate at >= MintValue)
	Minted    bool
	Triggers  []string
}

// Patterns returns the tracked effortful patterns in insertion order (Python patterns dict order).
func (c *Convertibility) Patterns() []PatternStat {
	out := make([]PatternStat, 0, len(c.patternOrder))
	for _, k := range c.patternOrder {
		p := c.patterns[k]
		if p == nil {
			continue
		}
		out = append(out, PatternStat{
			GoalKey: p.goalKey, Generated: p.generated, Value: p.groundedVal,
			Minted: p.minted, Triggers: append([]string(nil), p.triggers...),
		})
	}
	return out
}

// ProgramStat is a plain-data snapshot of one recurring synthesised program shape (a skill candidate).
type ProgramStat struct {
	Shape  string
	Count  int // recurrences so far (mints at >= MintAfter)
	Minted bool
}

// ProgramRuns returns the recurring program shapes in insertion order (Python program_runs order).
func (c *Convertibility) ProgramRuns() []ProgramStat {
	out := make([]ProgramStat, 0, len(c.programOrder))
	for _, k := range c.programOrder {
		r := c.programRuns[k]
		if r == nil {
			continue
		}
		out = append(out, ProgramStat{Shape: r.shape, Count: r.count, Minted: r.minted})
	}
	return out
}

// DomainTally returns a copy of the gate-win tally (which domains keep winning → a compiled gate prior).
func (c *Convertibility) DomainTally() map[string]float64 {
	out := make(map[string]float64, len(c.domainTally))
	for d, v := range c.domainTally {
		out[d] = v
	}
	return out
}

// TripleStat is a plain-data snapshot of one tracked (operator, source, domain) move (M5) — a primitive
// mint candidate. It exposes the live tally so a UI / a test can watch a hot grounded move get paved.
type TripleStat struct {
	Operator string
	Source   string
	Domain   string
	Count    int     // grounded recurrences so far (paves at >= MintAfter)
	Value    float64 // the grounded value it converged on (value-gate at >= MintValue)
	Minted   bool
	Demoted  bool
}

// Triples returns the tracked (operator, source, domain) moves in insertion order (M5).
func (c *Convertibility) Triples() []TripleStat {
	out := make([]TripleStat, 0, len(c.tripleOrder))
	for _, k := range c.tripleOrder {
		tr := c.triples[k]
		if tr == nil {
			continue
		}
		out = append(out, TripleStat{
			Operator: tr.operator, Source: tr.source, Domain: tr.domain, Count: tr.count,
			Value: tr.value, Minted: tr.minted, Demoted: tr.demoted,
		})
	}
	return out
}

// PathStat is a plain-data snapshot of one tracked path (analogy/induction/deduction) (M5).
type PathStat struct {
	Name     string
	Count    int
	Value    float64
	Grounded bool
	Minted   bool
	Demoted  bool
}

// Paths returns the tracked paths in insertion order (M5) — which directed traversals keep paying off.
func (c *Convertibility) Paths() []PathStat {
	out := make([]PathStat, 0, len(c.pathOrder))
	for _, k := range c.pathOrder {
		pr := c.paths[k]
		if pr == nil {
			continue
		}
		out = append(out, PathStat{
			Name: pr.name, Count: pr.count, Value: pr.value,
			Grounded: pr.grounded, Minted: pr.minted, Demoted: pr.demoted,
		})
	}
	return out
}

// MetacogRuns / MintAfter / MintValue / MetacogAfter expose the counters + gate thresholds so the panel
// can show progress toward a mint (e.g. "2/3 repeats", "value 0.18 < 0.20 gate").
func (c *Convertibility) MetacogRuns() int   { return c.metacogRuns }
func (c *Convertibility) MintAfter() int     { return c.cfg.MintAfter }
func (c *Convertibility) MintValue() float64 { return c.cfg.MintValue }
func (c *Convertibility) MetacogAfter() int  { return c.cfg.MetacogAfter }

// MintedSkillIdentity returns the EXACT name + trigger list that Consolidate's trace->skill mint would
// produce for a goal whose recurring program is being promoted (convert.go:701-704): the name is
// "learned-" + the hyphenated, 24-rune-truncated goal key, and the triggers are the goal key's fields.
// It reuses goalKey + truncate (the SAME private helpers Consolidate calls), so a FAITHFUL pre-seed of a
// minted skill (the W5 efficiency warm arm) can never drift from the real mint path — and it must NOT
// hand-pick a single trigger, which would inflate the recall win (the full Fields(goalKey) trigger set is
// what the autonomous mint uses). The minted skill is always Tier "" + Synthesized=true (the caller seeds
// those constants); this helper owns the goal-derived half (name + triggers), the part that drifts.
func MintedSkillIdentity(goal string) (name string, triggers []string) {
	key := goalKey(goal)
	name = "learned-" + truncate(strings.ReplaceAll(key, " ", "-"), 24)
	triggers = strings.Fields(key)
	return name, triggers
}

// truncate slices s to at most n runes (Python s[:n] on the ASCII goal-key corpus).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// round3 reproduces Python's round(x, 3) at the gate-prior emit site (round-half-to-even via the
// FormatFloat/ParseFloat round-trip). Mirrors value/regulator round3 (per-emit-site obligation).
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return r
}

// format2f reproduces Python f"{x:.2f}" — fixed 2 decimals — for the gate-prior summary string.
func format2f(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }
