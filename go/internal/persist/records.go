// Package persist is cross-session persistence of the harness's LEARNED state (the representation-space
// rebuild, M4): minted skills/operators/specialists, the compiled gate priors, declarative memory
// (episodes/beliefs), durable domain knowledge, and learned person preferences — loaded on start, saved
// on change/exit, and CURATED (versioned/deduped/decayed/demoted/GC'd/capped) so the durable stores
// stay bounded instead of growing forever (the audit's "append-only: no dedup, decay, or size cap").
//
// Two invariants govern this package (representation-space-rebuild.md §6):
//
//   - PERSISTENCE IS INJECTED — the engine stays headless-pure. The Store does file I/O in THIS package
//     and is handed to the engine via EngineConfig.Store (nil ⇒ in-memory only, the test/heuristic
//     default, so goldens never touch disk). No engine package imports persist for I/O.
//   - NEVER-FABRICATE survives the restart. SaveEpisode/SaveBelief/SaveKnowledge reject an ungrounded
//     record (return ErrUngrounded), and Load re-admits only grounded ones — a fabricated "fact" can
//     never become durable state, even across a restart.
//
// The CURATOR is PURE: it operates over the in-memory record slices + the seeded engine tick (no
// wall-clock), so its versioning/dedup/decay/demotion/GC/cap decisions are deterministic and unit-
// testable without any disk. Only the Store flushes the result to disk.
package persist

// Status is a record's curator lifecycle state. active = live (loaded + used); demoted = a belief/
// knowledge item reality refuted (kept for audit, excluded from recall); dormant = decayed below the
// floor (kept on disk, NOT re-loaded next start); archived = GC'd past its TTL (tombstoned in an
// archive segment, never erased — invalidate-not-delete all the way down).
type Status string

const (
	StatusActive   Status = "active"
	StatusDemoted  Status = "demoted"
	StatusDormant  Status = "dormant"
	StatusArchived Status = "archived"
)

// Meta is the curator bookkeeping carried on every persisted record (representation-space-rebuild.md
// §4.4). Version supports invalidate-not-delete re-mints; Hash is the content dedup key; the tick fields
// are the seeded engine clock (deterministic, never wall time); UseCount + LastUsedTick feed the decay
// score; Status is the lifecycle state; Grounded carries the never-fabricate flag through persistence.
type Meta struct {
	Version      int    `json:"version"`        // bumped on a same-name re-mint with a different body
	Hash         string `json:"hash"`           // content dedup key (FNV over the payload's identity)
	CreatedTick  int    `json:"created_tick"`   // seeded tick at first persist
	LastUsedTick int    `json:"last_used_tick"` // seeded tick at last use (recency for decay)
	UseCount     int    `json:"use_count"`      // times the artifact fired/was reused
	Status       Status `json:"status"`         // active | demoted | dormant | archived
	Grounded     bool   `json:"grounded"`       // never-fabricate: only grounded durable records are trusted

	// Substrate is the thinking-substrate provenance tag (the backend DisplayName, e.g. "claude:sonnet",
	// "llm:qwen3.6-35b-a3b", "test") stamped at save time. It is what lets a frontier-minted dataset be
	// distinguished, re-validated under the local substrate, and reduced/adjusted for re-localization
	// (claude-code-substrate-mapping.md §6.2). NOT part of the dedup hash — the same asset minted on two
	// substrates is one asset.
	Substrate string `json:"substrate,omitempty"`
}

// SkillRecord persists one minted skill. Body is the skill's Program serialised via cognition's
// ToDict/NodeFromDict round-trip (a map the engine encodes/decodes), so persist owns no cognition import.
type SkillRecord struct {
	Meta        Meta           `json:"meta"`
	Name        string         `json:"name"`
	Tier        string         `json:"tier"`
	Triggers    []string       `json:"triggers"`
	Body        map[string]any `json:"body"` // the Program as a node dict (cognition.Program.ToDict)
	Description string         `json:"description"`
}

// OpRecord persists one minted operator (the synthesised-at-runtime catalog growth).
type OpRecord struct {
	Meta   Meta   `json:"meta"`
	Name   string `json:"name"`
	Family string `json:"family"`
	Intent string `json:"intent"`
	Move   string `json:"move"` // the abstraction-ladder move tag (GROUND/LIFT/REFRAME/TRANSCODE/assess)
}

// SpecialistRecord persists one minted specialist (a repeated GENERATED pattern compiled into an
// automatic injection). Answer is the REAL worked answer captured at convergence — never fabricated.
type SpecialistRecord struct {
	Meta      Meta     `json:"meta"`
	Domain    string   `json:"domain"`
	GoalKey   string   `json:"goal_key"`
	Triggers  []string `json:"triggers"`
	Answer    string   `json:"answer"`
	Relevance float64  `json:"relevance"`
	Generated int      `json:"generated"`
	Value     float64  `json:"value"`
	Demoted   bool     `json:"demoted"`
}

// ProgramRunRecord persists one recurring synthesised-PROGRAM run per goal family (convert.programRuns):
// the per-goalKey COUNT of how often a given control-flow Shape was synthesised. It is the trace->skill
// flywheel's recurrence counter, made durable — without it the count resets to 1 every fresh engine
// (ProbeReplays / episodic runs use a new engine per episode), so the >= MintAfter mint never fires.
// Persisting the count lets it accumulate 1->2->3 across episodes that share a stateDir, so the skill
// mints on the 3rd. Body is the Program serialised via cognition's Program.ToDict/ProgramFromDict
// round-trip (the same whole-program envelope SkillRecord.Body uses), so persist owns no cognition
// import. Mirrors SpecialistRecord's plain-data shape exactly.
type ProgramRunRecord struct {
	Meta    Meta           `json:"meta"`
	GoalKey string         `json:"goal_key"` // the coarse per-goal key (convert.goalKey)
	Shape   string         `json:"shape"`    // the control-flow shape signature (Program.Shape())
	Count   int            `json:"count"`    // how often this shape was synthesised for this goal (mints at >= MintAfter)
	Minted  bool           `json:"minted"`   // whether the recurring program already minted a skill (so a re-run doesn't re-mint)
	Body    map[string]any `json:"body"`     // the Program as a whole-program dict (cognition.Program.ToDict)
}

// EpisodeRecord persists one declarative-memory episode (what was attempted, on what, how it turned
// out). Grounded on Meta gates the never-fabricate admission.
type EpisodeRecord struct {
	Meta     Meta     `json:"meta"`
	Goal     string   `json:"goal"`
	Entities []string `json:"entities"`
	Outcome  string   `json:"outcome"`
	Value    float64  `json:"value"`
	Tick     int      `json:"tick"`
}

// BeliefRecord persists one semantic belief (a distilled fact). ValidFrom/ValidTo carry the bi-temporal
// validity so an invalidation reconstructs exactly across a restart.
type BeliefRecord struct {
	Meta      Meta     `json:"meta"`
	Statement string   `json:"statement"`
	Entities  []string `json:"entities"`
	Source    string   `json:"source"`
	ValidFrom int      `json:"valid_from"`
	ValidTo   int      `json:"valid_to"`
}

// KnowledgeRecord persists one durable domain-knowledge item. Mirrors knowledge.Knowledge + the
// bi-temporal fields; Trust is the sourcing-ladder prior.
type KnowledgeRecord struct {
	Meta      Meta     `json:"meta"`
	Statement string   `json:"statement"`
	Kind      string   `json:"kind"`
	Entities  []string `json:"entities"`
	Source    string   `json:"source"`
	Trust     float64  `json:"trust"`
	ValidFrom int      `json:"valid_from"`
	ValidTo   int      `json:"valid_to"`
}

// PreferenceRecord persists one learned person preference (a user adaptation). Count is the evidence
// count; Learned is whether it crossed the learning threshold (so a learned adaptation is active again
// next start without re-accumulating evidence).
type PreferenceRecord struct {
	Meta    Meta   `json:"meta"`
	Trait   string `json:"trait"`
	Value   string `json:"value"`
	Count   int    `json:"count"`
	Learned bool   `json:"learned"`
}

// PriorsRecord persists the compiled gate priors as one record (domain -> +bias). A single record
// (not append-only per-domain) so the latest snapshot wins — gate priors are a small, bounded map.
type PriorsRecord struct {
	Meta   Meta               `json:"meta"`
	Priors map[string]float64 `json:"priors"`
}

// KeyframeRecord persists one loop-closure / recurrence keyframe (Track F, F-M7 — "the HINGE"). The
// Descriptor is the stable content fingerprint of a past thought-line (the recurrence key); the count
// + closure totals accumulate across runs that share the state dir, so the harness can recognise "I
// already explored this thought" (anti-rumination / convertibility) NEXT run — the cross-session loop
// closure the un-persisted DB blocked (gap G3). FirstSeenTick/LastSeenTick are the BI-TEMPORAL validity
// bounds; Meta.Substrate carries the SUBSTRATE TAG. The descriptor is the dedup key (Meta.Hash).
type KeyframeRecord struct {
	Meta          Meta   `json:"meta"`
	Descriptor    string `json:"descriptor"`      // the recurrence key (content fingerprint of the line)
	Gist          string `json:"gist"`            // a short human-readable label (observability)
	Count         int    `json:"count"`           // total observations across all runs sharing the DB
	Closures      int    `json:"closures"`        // observations that were loop closures (anti-rumination)
	FirstSeenTick int    `json:"first_seen_tick"` // bi-temporal: when the line was first recorded
	LastSeenTick  int    `json:"last_seen_tick"`  // bi-temporal: when it was last observed
}

// Snapshot is the full loaded state handed back from Store.Load — every artifact's record slice, ready
// for the engine to re-seed its registries. Nil slices are the cold-start (no prior state).
type Snapshot struct {
	Skills      []SkillRecord
	Operators   []OpRecord
	Specialists []SpecialistRecord
	ProgramRuns []ProgramRunRecord // the trace->skill recurrence counters (durable mint-from-recurrence)
	Episodes    []EpisodeRecord
	Beliefs     []BeliefRecord
	Knowledge   []KnowledgeRecord
	Preferences []PreferenceRecord
	Priors      *PriorsRecord    // nil ⇒ no persisted gate priors
	Keyframes   []KeyframeRecord // the loop-closure / recurrence keyframe DB (F-M7, bi-temporal, substrate-tagged)
}

// --- Named Snapshots (Registry Ledger: W1) ---------------------------------

// SnapshotMeta describes a named, immutable snapshot of the entire learned state.
// It is substrate-tagged so frontier-minted and local-minted state never mix.
type SnapshotMeta struct {
	Name        string `json:"name"`         // user-given name (e.g. "baseline", "batch-003")
	CreatedAt   int64  `json:"created_at"`   // unix nano timestamp
	CreatedTick int    `json:"created_tick"` // seeded engine tick at creation
	Substrate   string `json:"substrate"`    // backend DisplayName (e.g. "claude:sonnet", "llm:qwen", "test")
	Version     int    `json:"version"`      // schema version for forward compatibility
	// Counts for quick listing without loading full data
	SkillCount             int  `json:"skill_count"`
	OperatorCount          int  `json:"operator_count"`
	PrimitiveSubAgentCount int  `json:"specialist_count"`
	EpisodeCount           int  `json:"episode_count"`
	BeliefCount            int  `json:"belief_count"`
	KnowledgeCount         int  `json:"knowledge_count"`
	PreferenceCount        int  `json:"preference_count"`
	HasPriors              bool `json:"has_priors"`
}

// SnapshotRecord is the on-disk shape of a named snapshot (meta + full data).
type SnapshotRecord struct {
	Meta SnapshotMeta `json:"meta"`
	Data Snapshot     `json:"data"`
}

// SnapshotDiff is a human-readable diff between two snapshots.
type SnapshotDiff struct {
	FromName string         `json:"from_name"`
	ToName   string         `json:"to_name"`
	Added    map[string]int `json:"added"`   // artifact -> count
	Removed  map[string]int `json:"removed"` // artifact -> count
	Changed  map[string]int `json:"changed"` // artifact -> count
}

// --- Self-Change Ledger (W1: scope ladder + safety modes) --------------------

// SafetyMode is the config-gated scope ladder for self-modification (client decision 2026-06-12).
// SAFE (S0+S1) is the default; S2 structure and S3 code are EXPERIMENTAL and LOCKED (they change
// the plant itself — no validated control-theory gates yet). Every self-change is a ledger entry.
type SafetyMode string

const (
	SafetyModeSafe    SafetyMode = "SAFE"    // S0 parameters + S1 registry content (DEFAULT)
	SafetyModeExpand  SafetyMode = "EXPAND"  // S2 structure (EXPERIMENTAL, locked)
	SafetyModeRewrite SafetyMode = "REWRITE" // S3 code (EXPERIMENTAL, locked)
)

// LedgerScope is the scope of a self-change ledger entry.
type LedgerScope string

const (
	LedgerScopeS0 LedgerScope = "S0" // parameter changes (config toggles, thresholds)
	LedgerScopeS1 LedgerScope = "S1" // registry content (skills, operators, specialists, priors)
	LedgerScopeS2 LedgerScope = "S2" // structure (new component types, graph topology)
	LedgerScopeS3 LedgerScope = "S3" // code (self-code-update)
)

// LedgerEntry is one entry in the self-change ledger. Every self-modification is recorded here
// with scope + evidence + gate passed + revert handle. The SELF·EVOLUTION panel renders this.
type LedgerEntry struct {
	Timestamp    int64       `json:"timestamp"`     // unix nano
	Tick         int         `json:"tick"`          // engine tick at entry
	Scope        LedgerScope `json:"scope"`         // S0 | S1 | S2 | S3
	SafetyMode   SafetyMode  `json:"safety_mode"`   // SAFE | EXPAND | REWRITE
	Description  string      `json:"description"`   // human-readable what changed
	Evidence     string      `json:"evidence"`      // what evidence justified the change (test results, bench, etc.)
	GatePassed   string      `json:"gate_passed"`   // which gate was passed (stability, bench, UAT, etc.)
	RevertHandle string      `json:"revert_handle"` // how to revert (snapshot name, git commit, etc.)
	Substrate    string      `json:"substrate"`     // backend DisplayName that authored the change
	SubmittedBy  string      `json:"submitted_by"`  // "cognition" | "cli" | "tui"
}

// LedgerConfig configures the self-change ledger behavior.
type LedgerConfig struct {
	Enabled      bool       `json:"enabled"`       // whether the ledger is active
	SafetyMode   SafetyMode `json:"safety_mode"`   // current safety mode (SAFE default)
	MaxEntries   int        `json:"max_entries"`   // max ledger entries to keep (0 = unlimited)
	RequireGate  bool       `json:"require_gate"`  // require a gate to be passed for S2/S3
	AutoSnapshot bool       `json:"auto_snapshot"` // auto-snapshot before S1+ changes
}

// DefaultLedgerConfig returns the default ledger configuration.
func DefaultLedgerConfig() LedgerConfig {
	return LedgerConfig{
		Enabled:      true,
		SafetyMode:   SafetyModeSafe,
		MaxEntries:   1000,
		RequireGate:  true,
		AutoSnapshot: true,
	}
}

// ScopeAllowed returns true if the given scope is allowed under the current safety mode.
func (c LedgerConfig) ScopeAllowed(scope LedgerScope) bool {
	switch c.SafetyMode {
	case SafetyModeSafe:
		return scope == LedgerScopeS0 || scope == LedgerScopeS1
	case SafetyModeExpand:
		return scope == LedgerScopeS0 || scope == LedgerScopeS1 || scope == LedgerScopeS2
	case SafetyModeRewrite:
		return true // all scopes allowed
	default:
		return false
	}
}

// ScopeRequiresGate returns true if the given scope requires a gate to be passed.
func (c LedgerConfig) ScopeRequiresGate(scope LedgerScope) bool {
	if !c.RequireGate {
		return false
	}
	return scope == LedgerScopeS2 || scope == LedgerScopeS3
}

// ScopeRequiresSnapshot returns true if the given scope requires an auto-snapshot before applying.
func (c LedgerConfig) ScopeRequiresSnapshot(scope LedgerScope) bool {
	if !c.AutoSnapshot {
		return false
	}
	return scope == LedgerScopeS1 || scope == LedgerScopeS2 || scope == LedgerScopeS3
}
