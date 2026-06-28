// Package cognition holds the COGNITION-mode view layer: the plain-data view structs the panels read
// (this file) plus the panel renderers + the options-style Dashboard composer (which ship next).
//
// CRITICAL (DESIGN §4.3): this package imports NO engine package — only plain data flows in. The
// bridge (internal/tui/bridge.go) reads the engine's read-only accessors at end-of-tick and packs the
// live fields into a SnapshotData; the panels are pure views over that snapshot + the subscribed
// event stream. Keeping the engine out of here is what makes the views side-effect-free and prevents
// an import cycle (the engine never imports the TUI; the TUI's view layer never imports the engine).
package cognition

// SnapshotData is the end-of-tick snapshot of the live engine state the Python panels.render_*
// functions read directly off `eng` (DESIGN §4.3). The bridge builds one each tick from the engine's
// read accessors (DESIGN §4.5) and folds it into the panels; the event-fed fields (subconscious,
// seam, critic-events, value, trace) come from the subscribed bus stream, not from here. This struct
// is plain data — it holds only Go primitives + the *VM view types below, never an engine type.
type SnapshotData struct {
	// session / lifecycle (the status bar + render_lifecycle)
	Tick             int      // bus tick at end of this step
	Mode             string   // "reactive" | "continuous" (eng.mode)
	LifecycleState   string   // eng.lifecycle.state.name
	LifecycleHistory []string // eng.lifecycle.history, formatted "<STATE>  <reason>" last-6
	Arousal          string   // eng.arousal.name
	UserWaiting      bool     // eng.UserWaiting() — the A4 graph-derived "a user is waiting" (LOOP monitor)
	Substrate        string   // eng.SubstrateClass() — the running substrate class (LOOP monitor)

	// CONSCIOUS — the thought graph (render_conscious / render_value)
	ActiveContext []ThoughtVM // eng.graph.active_context() (the EXPANDED branch, full resolution)
	Branches      []BranchVM  // eng.graph.branches.values() (every branch, for the tree + V rerank)
	ActiveBranch  *int        // eng.graph.active_branch (nil before an episode opens)

	// CRITIC — the executive's last decision (render_critic)
	LastMeta *ControllerMetaVM // eng.controller.last_meta (nil before any decision)

	// SUBCONSCIOUS — the live recognised workflow (render_subconscious; the scan comes from events)
	Workflow *WorkflowVM // eng.subconscious.workflow when recognised (nil ⇒ none)

	// ACTION / WATCHED seam (render_action)
	ActionOutstanding  int    // len(eng.awatched.outstanding)
	ActionLatencyTicks int    // eng.awatched.latency (the async dead-time τ)
	ActedBranches      []int  // sorted(eng.acted_branches)
	LastBridge         string // how the last observation reached reality (N.4: structured|scraped|none)
	LastFabricated     bool   // was the last observation a tier-0 fabrication (P0.6)?

	// VALUE — per-branch V(s) (render_value reads b.value off the branches; this mirrors it for the bar)
	Values map[int]float64 // branch id -> V(s)

	// DURABILITY — the regulator metrics + history + the stability checklist (render_durability)
	Theta      float64            // r.theta (admission threshold)
	LamBar     float64            // r.lam_bar (stationary rate; +Inf at the n>=1 cliff)
	LamHat     float64            // r.lam_hat (measured intensity)
	N          float64            // r.n (branching ratio)
	Mu         float64            // r.mu (baseline / immigrant rate)
	U          float64            // r.U (utilisation)
	RegHistory []RegSnapshotVM    // r.history (bounded ring, ≤240 points; the sparklines read the tail)
	Stability  []StabilityCheckVM // r.stability(mode) — the durability checklist

	// MEMORY — the learned/consolidated state (render_memory): the convertibility organ's minted
	// specialists/skills + the operator catalog's minted operators (effortful→automatic), the learned
	// gate priors (compiled control habits domain→bias), and the conversation-memory turn count.
	MintedPrimitiveSubAgents []string           // eng.convert.Minted (domains compiled into specialists)
	Demoted                  []string           // eng.convert.Demoted (mints reality refuted — keep-or-revert, P0.5)
	MintedSkills             []string           // eng.convert.MintedSkill (trace→skill names)
	MintedOperators          []string           // eng.catalog.Minted() (operators synthesised this run)
	OperatorTotal            int                // len(eng.catalog.Names()) — total operators (seed + minted), for the REGISTRIES rollup
	GatePriors               map[string]float64 // eng.convert.GatePrior (learned standing bias, domain→+bias)
	TranscriptTurns          int                // len(eng.Transcript()) — conversation memory across episodes

	// DECLARATIVE memory counts (P2.3/P6.x) — the live registries' sizes for the compact metrics row
	// (the full entries live in the Registry tab). EpisodicCount = grounded episodes recorded;
	// SemanticCount = currently-valid beliefs; PersonCount = learned preferences.
	EpisodicCount int
	SemanticCount int
	PersonCount   int
	RetrieverMode string // the shared retriever's mode: "hybrid" (embedder reachable) | "lexical"

	// CONVERTIBILITY (live) — the candidates progressing toward minting (effortful→automatic), the
	// recurring program shapes (toward skills), the gate-win tally (toward a gate prior), and the
	// thresholds, so the learning mechanism is watchable BEFORE it mints (render_convert).
	ConvCandidates []ConvCandidateVM  // tracked effortful patterns (mint candidates)
	ConvPrograms   []ProgramRunVM     // recurring synthesised program shapes (skill candidates)
	DomainTally    map[string]float64 // which domains keep winning the gate (toward a gate prior)
	MetacogRuns    int                // deliberate MCP ops this goal (toward a gate prior)
	MintAfter      int                // repeats needed to mint (the gate)
	MintValue      float64            // value needed to mint (the value-gate)
	MetacogAfter   int                // metacog ops needed to compile a gate prior
}

// ConvCandidateVM is a tracked effortful pattern progressing toward minting a specialist.
type ConvCandidateVM struct {
	GoalKey   string  // the coarse goal bucket
	Generated int     // effortful repeats so far (mints at >= MintAfter)
	Value     float64 // grounded value converged on (value-gate at >= MintValue)
	Minted    bool
}

// ProgramRunVM is a recurring synthesised program shape progressing toward minting a skill.
type ProgramRunVM struct {
	Shape  string
	Count  int
	Minted bool
}

// ThoughtVM is the plain-data view of one graph thought (Python Thought fields the panels read).
// Source is the wire NAME ("INJECTED", ...) so the renderer keys the SrcColor map by name without
// importing types here.
type ThoughtVM struct {
	ID         int     // #id shown in render_conscious
	Text       string  // th.text (truncated at render to 58)
	Source     string  // th.source.name — the tone key
	SourceTag  string  // th.source.name[:3] — the 3-letter tag
	Confidence float64 // th.confidence (blank when 0)
}

// BranchVM is the plain-data view of one branch (Python Branch fields the conscious + value panels
// read). Resolution is the wire name; Status is the wire name (ACTIVE/STASHED/DEAD/MERGED).
type BranchVM struct {
	ID           int     // b.id
	Resolution   string  // b.resolution.name — "EXPANDED" -> "E", else "C"
	Value        float64 // b.value — V(s) for the rerank bar + the tree label
	Status       string  // b.status.name — STASHED draws the "○" / muted tone
	Gist         string  // the branch's summary (COMPRESSED gist) or its latest thought (the frontier read)
	Depth        int     // tree depth (A* search depth) — g.Depth(bid)
	ThoughtCount int     // len(b.ThoughtIDs) — the cost g so far on this line
	Reason       string  // why this branch was forked (b.Reason; "" == None)
}

// ControllerMetaVM is the plain-data view of the Controller's last decision (Python last_meta dict
// keys the render_critic panel reads). Decision is the wire name (the tone key); the four flags are
// the validate-before-trust signals.
type ControllerMetaVM struct {
	Decision         string // last_meta["decision"] — the FINAL move (the tone key, DecColorFor)
	StopKind         string // last_meta["stop_kind"] — when decision==STOP: GOAL_MET/GIVE_UP/BLOCKED_* ("" otherwise)
	Reason           string // last_meta["reason"]
	BranchExhausted  bool   // last_meta["branch_exhausted"]
	LoopExhausted    bool   // last_meta["loop_exhausted"]
	Flagged          bool   // last_meta["flagged"]
	NeedsGroundTruth bool   // last_meta["needs_ground_truth"]

	// the decision-mode + smart-hybrid escalation (the executive's WHY behind a possibly-overridden move).
	Mode              string  // controller mode: "control" | "llm" | "hybrid"
	Ambiguity         float64 // how ambiguous the choice was (drives a hybrid escalation)
	Escalated         bool    // did the Controller escalate this decision to the model?
	HeuristicDecision string  // the heuristic's own pick (never overwritten)
	LLMDecision       string  // the model's pick (when escalated)
	Agree             bool    // did the model agree with the heuristic? (when escalated)
}

// WorkflowVM is the plain-data view of the live recognised workflow (render_subconscious shows
// "workflow: <name> · phase <i> (<op_name>)"). Recognized gates the line.
type WorkflowVM struct {
	Name       string // wf.name
	PhaseIndex int    // wf.i — the current phase cursor
	OpName     string // wf.current().operator.name
	Recognized bool   // wf.recognized — only show the line when recognised
}

// RegSnapshotVM is one regulator history point (Python's per-tick snap dict; the sparklines read
// theta + n over the tail). Plain floats so the durability panel sparks without importing regulator.
type RegSnapshotVM struct {
	Theta float64 // snap["theta"]
	N     float64 // snap["n"]
}

// StabilityCheckVM is one durability-condition verdict (Python regulator.Check; the render_durability
// panel draws ✓ / · / ~). NA marks the μ>0 check in reactive mode (where Pass is meaningless).
type StabilityCheckVM struct {
	Name string // the condition label (e.g. "n<1 (subcritical)")
	Pass bool   // held (✓) vs not-held (·)
	NA   bool   // not-applicable (~) — the reactive-mode μ>0 entry
}
