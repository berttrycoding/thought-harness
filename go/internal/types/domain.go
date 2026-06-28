package types

// This file holds the core domain structs + the duck-typed RawReturn/Payload closed union.
//
// Mutable-default discipline: Python's dataclass field(default_factory=dict/list) allocates
// a FRESH container per instance. Go maps/slices are nil by default; callers that need a
// non-nil map (Signals) get one from the constructor helpers below or allocate at the use
// site. NEVER share a package-level map/slice as a default — that is the Python "mutable
// default argument" bug the per-instance rule guards against.

// ============================================================================
// Thought / Branch / Candidate
// ============================================================================

// Thought is an indexed, addressable node in the graph.
type Thought struct {
	ID         int       // the "thought count" — every thought is an ADDRESSABLE node
	Text       string    // first-person narrative content (post-transform if INJECTED)
	Source     Source    //
	Confidence float64   // set by Filter / Controller (Python default 0.0)
	Operator   *Operator // nil == Python None
	Parent     *int      // graph edge to the thought it continued from (nil == None)
	BranchID   *int      // nil == None
	Tick       int       //
	RawReturn  any       // pre-transform payload (INJECTED only) — kept for trace-back; the
	//                    central duck-typed field (see the closed union below).
}

// Short collapses + truncates the text to width (Python Thought.short).
func (t Thought) Short(width int) string { return Ellipsize(t.Text, width) }

// ShortDefault truncates at the Python default width of 72.
func (t Thought) ShortDefault() string { return Ellipsize(t.Text, 72) }

// Branch is a line of reasoning — an ordered set of thought ids with a rerank value.
type Branch struct {
	ID           int
	ThoughtIDs   []int      // Python field(default_factory=list) — allocate per instance
	Resolution   Resolution // Python default Resolution.EXPANDED
	Summary      *string    // the gist, populated when COMPRESSED (nil == None)
	Value        float64    // rerank priority — DRIVEN BY THE VALUE SIGNAL
	Epistemic    float64    // V(s) WITHOUT the conversational-priority term: content quality alone (A1 refinement) — what trust/scheduler/drives consume; urgency is not evidence
	Status       Status     // Python default Status.ACTIVE
	ParentBranch *int       // nil == None
	Reason       *string    // why this branch was forked (nil == None)
}

// NewBranch builds a Branch with Python's dataclass defaults (EXPANDED / ACTIVE, an empty
// non-nil ThoughtIDs slice).
func NewBranch(id int) Branch {
	return Branch{ID: id, ThoughtIDs: []int{}, Resolution: EXPANDED, Status: ACTIVE}
}

// Candidate is a raw return flowing through the intake (before it becomes a Thought).
// Specialists, Conscious generation, observations and user input all arrive as Candidates so
// the Filter can screen them uniformly *before* the Transform voices them.
type Candidate struct {
	Text      string
	Source    Source
	Domain    *string   // which specialist produced it (INJECTED); nil == None
	Relevance float64   // how strongly the producing specialist lit up
	Stance    *string   // for conflict detection (e.g. "safe" vs "unsafe"); nil == None
	Operator  *Operator // nil == None
	Payload   any       // raw structured payload, retained for trace-back (duck-typed; see union)
	// DispatchWeight is the SPARSEMAX dispatch confidence p_i in [0,1] over the specialist field, stamped
	// by the opt-in subconscious.dispatch.sparse admission path (a free normalized signal for V(s)/rerank).
	// 0 (the zero value) on the legacy absolute-gate path AND in any golden that does not serialize it —
	// it carries NO json tag and is set only when the sparse dispatch is on, so it is byte-identical when
	// the flag is OFF.
	DispatchWeight float64
}

// ============================================================================
// FilterVerdict / Appraisal
// ============================================================================

// FilterVerdict is the Filter's admission outcome (Critic, admission half).
type FilterVerdict struct {
	Verdict    Verdict
	Confidence float64
	Reason     string
	Signals    map[string]any // the breakdown that PRODUCED confidence (not collapsed); per-instance
	Source     string         // who appraised: heuristic | llm | hybrid (Python default "heuristic")
}

// Admit reports whether the candidate is admitted (ADMIT or FLAG). Mirrors the Python
// FilterVerdict.admit property.
func (v FilterVerdict) Admit() bool { return v.Verdict == ADMIT || v.Verdict == FLAG }

// AsAppraisal surfaces a FilterVerdict as the unified reasoning-capture record: a
// FilterVerdict IS a Filter-specialised Appraisal. The site defaults to "filter.admit" in
// Python; pass that explicitly (use AsAppraisalDefault for the default). Signals is copied
// so the Appraisal does not alias the verdict's map.
func (v FilterVerdict) AsAppraisal(site string) Appraisal {
	return Appraisal{
		Site:          site,
		Value:         v.Confidence,
		Verdict:       ptr(v.Verdict.String()),
		Reason:        v.Reason,
		Signals:       copyMap(v.Signals),
		Source:        v.Source,
		AppraiserConf: 1.0, // Python Appraisal default appraiser_conf=1.0
	}
}

// AsAppraisalDefault uses the Python default site "filter.admit".
func (v FilterVerdict) AsAppraisalDefault() Appraisal { return v.AsAppraisal("filter.admit") }

// Appraisal is the unified reasoning-capture record (P6). Every heuristic/decision site can
// return one. Python's @dataclass(frozen=True) becomes a Go value type — passed/returned by
// value, never mutated. Data, not control: carrying an Appraisal changes no decision.
type Appraisal struct {
	Site          string
	Value         float64
	Verdict       *string        // the discrete verdict if any (nil == Python None)
	Reason        string         // the LLM's why or the heuristic's rule trace
	Signals       map[string]any // the structured breakdown that produced the value; per-instance
	Source        string         // heuristic | llm | hybrid | tool (Python default "heuristic")
	AppraiserConf float64        // Python default 1.0
}

// Intention is a single effectful action to take on the world (distilled from the active
// branch).
type Intention struct {
	Text     string
	Kind     string // run | send | measure | edit ... (Python default "run")
	BranchID *int   // nil == None
	// Structured intention->tool bridge (N.4): formed at the Controller's ACT decision by the model that
	// KNOWS what it wants to do, instead of being scraped at the seam. When Tool is set, the watched seam
	// dispatches it DIRECTLY (the whole tool surface is reachable, not just run_tests/run_shell); when it
	// is "", the seam falls back to the regex scraper. Claim is the assertion this action grounds.
	Tool  string
	Args  map[string]any
	Claim string
}

// ============================================================================
// The duck-typed RawReturn / Payload closed union (DESIGN §7)
// ============================================================================
//
// Thought.RawReturn and Candidate.Payload are `any`. Every Python isinstance(x, dict) /
// getattr(raw, 'stance', …) becomes a type switch over a CLOSED, explicitly-declared set,
// defined here so controller/value/backends/watched_seam all branch on the same union:
//
//	switch r := t.RawReturn.(type) {
//	case Observation:      // watched-seam observation: r.Ok, r.Text
//	case *Candidate:       // r.Stance, r.Domain
//	case *action.ToolResult: // (held as any in r.Result; asserted in the action-aware layers)
//	case nil:              // no payload
//	}
//
// Observation is the typed replacement for the Python heterogeneous watched-seam dict
// {ok, text, tool?, exit_code?, result?}. It is the value WatchedSeam.act() returns. value.reward's
// `isinstance(raw, dict) and raw.get('ok')` becomes `if o, ok := r.(Observation); ok && o.Ok`.
//
// Result holds the captured *action.ToolResult. It is typed `any` here (not
// *action.ToolResult) deliberately: types is a Tier-0 leaf and must not import the action
// package. The action-aware layers (watched seam, value) assert Result.(*action.ToolResult).
type Observation struct {
	Ok       bool
	Text     string
	Tool     string // the tool name (empty when the heuristic fallback produced the obs)
	ExitCode *int   // nil == Python None / absent key
	Result   any    // the *action.ToolResult, or nil; asserted by the action-aware layers
	// Fabricated marks an observation the offline stand-in MADE UP (no real tool ran). It is the
	// tier-0 breadcrumb (P0.6 / SR-4 grounding-integrity): such an "observation" did not come from
	// reality, so it MUST NOT validate, refute, or be stored as grounded knowledge — it is not a
	// grounding source. A real executor's result leaves this false. See GroundsReality.
	Fabricated bool
	// Bridge records HOW the intention reached this tool (N.4): "structured" (the intention carried a
	// Tool+Args, dispatched directly — the whole tool surface), "scraped" (the regex fallback mapped it),
	// or "none" (the bridge could not map it — an explicit grounding-bridge failure, NOT a silent drop).
	Bridge string
}

// GroundsReality reports whether this observation may serve as GROUND TRUTH — i.e. it actually came
// from reality (a real tool/executor), not the offline stand-in. A fabricated observation is tier-0
// and returns false: the grounding loop / experiment memory must reject it (a fabricated "reality:
// 12/12 pass" can never validate a claim, a fabricated failure can never refute one). This is the
// single check the grounding-integrity rule keys on.
func (o Observation) GroundsReality() bool { return !o.Fabricated }

// FuelProvenance is the trace-back stamp the concretization step (representation-space-rebuild.md §3.3)
// leaves on a Candidate.Payload when it sources + fuses fuel before the hidden seam. It records WHICH
// rung of the sourcing ladder the material came from (Source), the concrete provider string
// ("knowledge:fact" | "memory:semantic" | "reality:run_tests" | "generated" | "conscious:t42"), and
// whether the fuel was GROUNDED (rungs 1-4) or invented (the generated rung, which keeps types.GENERATED
// so the Filter distrusts it at 0.42). It is part of the closed Payload union — the action-aware /
// trace layers branch on it; it is DATA, never control (carrying it changes no decision).
type FuelProvenance struct {
	Source   string // the ladder rung name: "present" | "knowledge" | "memory" | "reality" | "generated"
	Provider string // the concrete provider, e.g. "knowledge:fact" | "memory:semantic" | "reality:run_tests"
	Grounded bool   // true for rungs 1-4 (sourced); false for the generated rung (invented, low-trust)
}

// ============================================================================
// small helpers
// ============================================================================

// ptr returns a pointer to v (for the *string optional fields).
func ptr[T any](v T) *T { return &v }

// copyMap returns a shallow copy of m (nil-safe), so a derived record never aliases the
// source map — preserving the per-instance mutable-default discipline.
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
