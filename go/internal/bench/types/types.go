// Package types defines the shared, data-only schemas for the registry-scaling
// benchmark harness — the "measuring stick" of
// docs/internal/notes/measuring-stick-spec.md.
//
// The measuring stick proves, per load-bearing mechanism, that running a fixed
// base model THROUGH the harness's machinery beats running the SAME base model
// BARE, and that turning that one mechanism OFF (everything else held) erases the
// win. That delta is LIFT; the ON/OFF ablation makes the lift attributable to the
// mechanism rather than to generic scaffolding. See the spec §1.4 (the spine).
//
// Every type here is a plain data container: a benchmark item/scenario, the
// oracle that scores it, an arm's result, and the paired contrasts the keep-rule
// reads. There is NO behavior beyond trivial String() methods on the string
// enums — scoring, materialization, running, and statistics live in the sibling
// packages (internal/bench/{tiera,tierb,runner,phase0,stats,ledger}). These
// structs are the wire format those packages exchange and serialize to JSONL
// (spec §5.2, §5.3, §5.7), so the JSON struct tags are part of the contract.
//
// Vocabulary (spec §1, §3, §5):
//   - Mechanism — one of the six load-bearing mechanisms under test.
//   - Tier      — A (atomic quizzes) or B (multi-turn scenarios).
//   - Arm       — a configuration the same item is run under (bare vs harness,
//     plus the gate-on/gate-off ablation sub-arms), paired by seed.
//   - Oracle    — a tagged, mostly-deterministic check of an arm's output.
//   - Item      — a Tier-A probe; Scenario — a Tier-B multi-turn arc.
//   - Result    — one arm's outcome on one item/scenario.
//   - Contrast  — a paired effect estimate (harness−bare, gate-on−gate-off,
//     isolation rate) with a bootstrap confidence interval.
package types

import "github.com/berttrycoding/thought-harness/internal/cost"

// ---------------------------------------------------------------------------
// Enums (string-backed, because every value is serialized verbatim into the
// JSONL item/scenario/result schemas of spec §5.2/§5.3/§5.7).
// ---------------------------------------------------------------------------

// Mechanism is one of the six load-bearing mechanisms the measuring stick
// covers (spec §1.1, §3). Each is measured at both tiers and kept only when its
// gate-on−gate-off contrast clears its MDE and its isolation rate clears its
// floor.
type Mechanism string

const (
	// MechGrounding — settle a claim against external reality (the watched seam
	// + the Filter), not from priors inside the closed loop. Spec §3.1.
	MechGrounding Mechanism = "grounding"
	// MechMultiStepRetrace — commit to a line, have a grounding ACT refute it,
	// BACKTRACK, and solve via the replaced line. Spec §3.2.
	MechMultiStepRetrace Mechanism = "multi-step-retrace"
	// MechSelfImprovement — convertibility: a repeated effortful sub-task mints a
	// reusable path so the second+ occurrence is cheaper. Spec §3.3.
	MechSelfImprovement Mechanism = "self-improvement"
	// MechContinuousAutonomy — durable self-direction with no user turns (drives,
	// default-mode, proactive outreach, clean quiescence). Spec §3.4.
	MechContinuousAutonomy Mechanism = "continuous-autonomy"
	// MechStability — the regulator holds the engine in the durable regime under
	// fork-storm / long-horizon stress. Spec §3.5.
	MechStability Mechanism = "stability"
	// MechSafety — the Action gate refuses a destructive/irreversible action under
	// benign framing while completing the safe remainder. Spec §3.6.
	MechSafety Mechanism = "safety"
	// MechSynthFidelity — agent-synthesis fidelity (A5, Track A,
	// docs/internal/notes/2026-06-16-registry-target-spec.md §1, §3 stretch): does the
	// SYNTHESISER produce a workflow PROGRAM (+ implied sub-agent tool-scope) whose
	// STRUCTURE faithfully matches what a goal requires — the right operator
	// families/sequence, the right control-flow shape (par⇒branch, validate@reality
	// ⇒act), the right tool-scope — vs a plausible-but-wrong one? Unlike the six
	// task-outcome mechanisms above this scores the synthesiser's CONSTRUCTION
	// directly (a deterministic STRUCTURAL oracle, no model in the loop, offline-
	// vettable), not a bare-vs-harness task answer. Scored by internal/bench/
	// synthfidelity, not the tiera arm runner.
	MechSynthFidelity Mechanism = "synth-fidelity"
	// MechDecisionQuality — Deliberator/Verifier OUTPUT-correctness (A2, Track A,
	// docs/internal/notes/2026-06-16-registry-target-spec.md §1 rows Deliberator/Verifier,
	// §3 "decision quality; correct accept/refuse"). Unlike MechSynthFidelity (which
	// scores the synthesised PROGRAM STRUCTURE) and the cognition probe (which scores
	// whether a faculty FIRED), this scores whether the agent reached the RIGHT
	// VERDICT against ground truth: the Deliberator picks the genuinely-better option
	// in a trade-off, and the Verifier correctly ACCEPTS a true claim / REFUSES a
	// false one. A deterministic ground-truth oracle (the agent's verdict is compared
	// to the fixture's known-correct answer — no model in the SCORER, offline-vettable),
	// scored by internal/bench/decisionoracle, not the tiera arm runner.
	MechDecisionQuality Mechanism = "decision-quality"
)

// String returns the wire value of the mechanism.
func (m Mechanism) String() string { return string(m) }

// Tier is the measurement tier: atomic quizzes (A) or multi-turn scenarios (B).
// Spec §1.2.
type Tier string

const (
	// TierAtomic — single-shot/single-couplet probes with deterministic oracles;
	// cheap, high-N, the bulk of the statistical power. Spec §1.2.
	TierAtomic Tier = "A"
	// TierScenario — realistic multi-turn arcs where the mechanism is forced by
	// the task structure and the end-state is mechanically checkable. Spec §1.2.
	TierScenario Tier = "B"
)

// String returns the wire value of the tier.
func (t Tier) String() string { return string(t) }

// Arm is one configuration an item/scenario is run under. The same generated
// instance, the same base model, and the same temperature are used across arms;
// only the harness/gate is toggled, and results are paired by seed. Spec §5.1.
type Arm string

const (
	// ArmBare — the base model alone, no graph/Controller/seams/regulator/
	// convert/gate. The reference the lift is measured against. Spec §5.1.
	ArmBare Arm = "bare"
	// ArmBareNoTools — the prior-only bound (grounding arm 1a): the base model
	// with no tools at all. Spec §3.1 ("1a no-tools = prior bound").
	ArmBareNoTools Arm = "bare-no-tools"
	// ArmBareRawTools — the honest baseline (grounding arm 1b): the base model
	// with a raw read tool but no Filter / watched-seam discipline. Spec §3.1.
	ArmBareRawTools Arm = "bare-raw-tools"
	// ArmHarness — the full engine via cmd/thought (graph + Controller + seams +
	// regulator + convert + gate, as the mechanism requires). Spec §5.1.
	ArmHarness Arm = "harness"
	// ArmGateOn — the ablation arm with the mechanism's single gate flag ON (full
	// discipline). The "harness" side of the load-bearing contrast. Spec §1.4.
	ArmGateOn Arm = "gate-on"
	// ArmGateOff — the ablation arm with the mechanism's single gate flag OFF
	// (Filter pass-through / forbid-BACKTRACK / --convert off / drives-off /
	// regulator open-loop / action-gate-bypassed). Spec §5.1.
	ArmGateOff Arm = "gate-off"
	// ArmSingleStrong — the SUB-AGENT GUARD reference (docs/internal/2026-06-21-sota-
	// benchmark-suite.md §7.6): the full engine with the per-tick sub-agent fan-out
	// COLLAPSED to its single best member (config.Subconscious.SingleStrongAgent ON).
	// It is NOT the bare model — it keeps the graph / Controller / seams / regulator /
	// gate — only the multi-agent TEAM is reduced to its strongest member, so the
	// teams-vs-best-member A/B (harness vs single-strong) is two engines that genuinely
	// DIFFER. The "Multi-Agent Teams Hold Experts Back" finding (arXiv 2602.01011) means
	// the harness's full sub-agent dispatch must measurably BEAT this arm or the sub-agent
	// layer is anti-value. The runner builds it via the SingleStrongAgent toggle (a real
	// engine-behavior change the live tick consumes), never a faked ablation.
	ArmSingleStrong Arm = "single-strong"
)

// String returns the wire value of the arm.
func (a Arm) String() string { return string(a) }

// OracleKind tags how an Oracle scores an arm's output. All but Rubric are
// deterministic; Rubric is the explicitly-allowed minority (judged, characterized
// in Phase-0 before it can contribute to a keep). Spec §5.2, §5.4.
type OracleKind string

const (
	// OracleExact — exact-match after a fixed per-item-type normalizer. Spec §3.1.
	OracleExact OracleKind = "exact"
	// OracleNumericTolerance — numeric equality within a stated tolerance. Spec §3.1.
	OracleNumericTolerance OracleKind = "numeric-tolerance"
	// OracleSetMembership — the answer must be a member of (or equal as a set to)
	// an expected set. Spec §3.1.
	OracleSetMembership OracleKind = "set-membership"
	// OracleLedgerStatus — read the append-only action_ledger: a destructive
	// entry must be blocked|held-for-confirm (never executed). Spec §3.6.
	OracleLedgerStatus OracleKind = "ledger-status"
	// OracleEventPresence — a required event (or sequence) must appear on the
	// event trace (e.g. critic.decision=BACKTRACK, action.gate.blocked,
	// convert.minted→dispatch). Spec §3.2, §3.3, §3.6.
	OracleEventPresence OracleKind = "event-presence"
	// OracleRubric — an LLM-as-judge rubric score, fixed temperature, Phase-0
	// characterized. The non-deterministic minority. Spec §3.1, §5.4.
	OracleRubric OracleKind = "rubric"
	// OracleTelemetry — deterministic arithmetic on the regulator telemetry trace
	// (regulator.update / regulator.stability events): peak n, final n, fan-out ≤
	// cap, oscillation sign-change rate + late-window variance, quiescence/
	// termination. The stability mechanism's no-model Tier-A oracle (spec §3.5
	// Tier-A): a CLOSED-LOOP regulator suppresses (passes), an OPEN-LOOP reference
	// diverges (fails). Expected names the telemetry predicate (e.g.
	// "suppressed-final-n-subcritical", "fan-out-width-invariant",
	// "terminates-within-cap", "settled-low-oscillation").
	OracleTelemetry OracleKind = "telemetry"
	// OracleGrid — exact GRID match for ARC-AGI-2-shaped external banks (docs/internal/
	// 2026-06-21-sota-benchmark-suite.md §7.1: "ARC-AGI grid match"). The answer is a
	// rectangular grid of integer cell values (the ARC colour palette); Expected is the
	// canonical target grid in the same encoding. The grader is the bank's NATIVE one —
	// an exhaustive cell-by-cell equality after a tolerant parse that accepts the common
	// grid serializations (a JSON array-of-arrays, or whitespace/newline-delimited rows of
	// space/comma-separated integers) extracted from the model's free-text answer. It is
	// deterministic and programmatic (ARC's own grader shape — the trustworthy, ungameable
	// kind, spec §8.2), no model in the SCORER. A grid is correct only if it has the EXACT
	// dimensions of Expected AND every cell matches — there is no partial credit (the ARC
	// pass/fail discipline). Expected carries the canonical grid (a JSON array-of-arrays
	// string); Normalizer is ignored (grids have their own parse).
	OracleGrid OracleKind = "grid"
)

// String returns the wire value of the oracle kind.
func (k OracleKind) String() string { return string(k) }

// ---------------------------------------------------------------------------
// Shared sub-structs (artifacts, oracles, traces, cost).
// ---------------------------------------------------------------------------

// Artifact is the real, materialized thing a Tier-A item is grounded against:
// a repo file at a fixed path, a run-record (run.jsonl), an account snapshot, a
// TUI grid, or a sandboxed tool. The loader materializes it into a per-item
// sandbox before the arms run. Spec §3.1, §5.2.
type Artifact struct {
	// Kind labels the artifact family ("repo-file", "run-record",
	// "account-snapshot", "tui-grid", "sandbox-tool", ...). Free-form so the
	// loader can switch on it; not an enum because the family set grows per bank.
	Kind string `json:"kind"`
	// Path is the in-sandbox path the materialized artifact lives at (the fixed
	// path the prompt refers to). Empty for non-file artifacts.
	Path string `json:"path,omitempty"`
	// Spec is an opaque per-Kind descriptor (e.g. a tool name + fixture id, a
	// sandbox policy id) the loader interprets to wire the artifact.
	Spec string `json:"spec,omitempty"`
	// Materialization is the raw bytes/contents written to Path (or fed to the
	// tool) so the item is self-contained and reproducible — the artifact's
	// ground-truth source.
	Materialization []byte `json:"materialization,omitempty"`
	// Files, when non-empty, materializes ADDITIONAL files (in-sandbox path ->
	// contents) alongside Path — for MULTI-FILE grounding items (e.g. read an
	// index to learn which config to read, then read that). Path stays the
	// primary entry the prompt names + the ArtifactPath the engine flips on.
	Files map[string]string `json:"files,omitempty"`
}

// Oracle is a tagged, mostly-deterministic check of an arm's output. The scoring
// packages interpret Expected according to Kind (and apply the named
// Normalizer). An Oracle may also require a trace predicate (TraceRequirement)
// in addition to the answer check — e.g. retrieval-integrity AND's a value match
// with a read-the-right-source trace requirement. Spec §3.1, §5.2.
type Oracle struct {
	// Kind selects the check (exact / numeric-tolerance / set-membership /
	// ledger-status / event-presence / rubric).
	Kind OracleKind `json:"kind"`
	// Expected is the ground-truth value, interpreted per Kind: the canonical
	// answer string (exact), the target number (numeric-tolerance), a member of
	// the expected set (set-membership), the required ledger status
	// (ledger-status), the required event key (event-presence), or the rubric
	// prompt/criterion (rubric).
	Expected string `json:"expected"`
	// ExpectedSet carries the full expected set for set-membership oracles (and
	// the must-contain list for composite answer oracles).
	ExpectedSet []string `json:"expected_set,omitempty"`
	// Tolerance is the absolute numeric tolerance for numeric-tolerance oracles.
	Tolerance float64 `json:"tolerance,omitempty"`
	// Normalizer names the fixed, unit-tested normalizer applied before
	// comparison (e.g. "identifier-canonical", "number", "set"). Spec §3.1, §5.2.
	Normalizer string `json:"normalizer,omitempty"`
	// TraceRequirement, when non-nil, AND's a trace predicate onto the answer
	// check (retrieval-integrity grounding, retrace isolation, safety policy_id).
	TraceRequirement *TraceOracle `json:"trace_requirement,omitempty"`
}

// TraceOracle is a trace predicate: a list of event keys that must be present
// (optionally in order) on an arm's emitted event trace for the item to count.
// Used both as a primary oracle (event-presence) and as the isolation guard
// AND'd onto an answer oracle (the mechanism must be genuinely used). Spec §3.2,
// §5.2.
type TraceOracle struct {
	// RequiredEvents are the event keys that must appear (e.g.
	// "critic.decision=BACKTRACK", "action.observation.ok=false",
	// "convert.minted", "action.gate.blocked"). Spec §3.2.
	RequiredEvents []string `json:"required_events"`
	// Ordered requires the events to appear in the listed order (e.g. a
	// convert.minted must precede the dispatch that reuses it). Default false =
	// presence only.
	Ordered bool `json:"ordered,omitempty"`
}

// PriorLure is the plausible-but-wrong answer the bare model is measurably biased
// toward — the lure is what makes a grounding item discriminate. An item is
// admitted to a bank only if a pilot shows the bare-model emits the lure at or
// above the calibrated rate (≥0.5, ≥0.65 for "high"). Spec §3.1.
type PriorLure struct {
	// Text is the lure answer (the canonical default / renamed-since field / a
	// value that used to be true / a textbook constant the repo overrode).
	Text string `json:"text"`
	// BareEmissionRate is the calibrated rate at which the bare model emits the
	// lure in the pilot — the admission threshold. Spec §3.1.
	BareEmissionRate float64 `json:"bare_emission_rate"`
}

// Cost is the per-arm resource cost of running one item/scenario. Self-
// improvement reads it as the native outcome (the learning curve cost(N)<cost(1):
// primary = model-call count, tie-break = steps, then tokens). Spec §3.3.
//
// The token sub-fields (CachedInTokens / UncachedInTokens / OutTokens /
// ReasoningTokens) carry the per-arm token SPLIT off the llm.call event trace so
// the $ cost computation (internal/cost) can price the run: cost = uncached-in *
// in_uncached + cached-in * in_cached + out * out. Tokens stays the headline total
// (= cached-in + uncached-in + out) for the §3.3 tie-break; the splits are the
// money detail. All are 0 on the offline test double (no llm.* events).
type Cost struct {
	// ModelCalls is the number of backend/model calls (the primary cost metric).
	ModelCalls int `json:"model_calls"`
	// Steps is the number of engine steps taken (bounded by the step-cap of 25).
	Steps int `json:"steps"`
	// Tokens is the total token count across calls (the finest tie-break) =
	// CachedInTokens + UncachedInTokens + OutTokens.
	Tokens int `json:"tokens"`
	// CachedInTokens is the input cache-HIT token count (billed at the discounted
	// rate). The cache-hit % is CachedInTokens / (CachedInTokens+UncachedInTokens).
	CachedInTokens int `json:"cached_in_tokens,omitempty"`
	// UncachedInTokens is the input cache-MISS token count (billed at the full rate).
	UncachedInTokens int `json:"uncached_in_tokens,omitempty"`
	// OutTokens is the output/completion token count (billed at the output rate).
	OutTokens int `json:"out_tokens,omitempty"`
	// ReasoningTokens is the reasoning-trace token count — a BREAKOUT of OutTokens
	// (already inside it), surfaced so the report can show how much output was
	// thinking. Never double-priced.
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// ---------------------------------------------------------------------------
// Tier A — atomic quiz items (spec §5.2).
// ---------------------------------------------------------------------------

// TierAItem is one atomic quiz: a single-shot (or single-couplet) probe with a
// deterministic oracle wherever possible. It carries everything the Tier-A loader
// needs to materialize the artifact, run the arms, and score the answer plus the
// isolation guard. One TierAItem == one JSONL line. Spec §3.x Tier-A, §5.2.
type TierAItem struct {
	// ID is the stable item identifier (e.g. "rt-A-0137"). Spec §3.2.
	ID string `json:"id"`
	// Mechanism is the mechanism this item probes.
	Mechanism Mechanism `json:"mechanism"`
	// Family is the within-mechanism generation family (e.g. "act-to-refute",
	// "reanchor", "camouflaged-destructive", "RESUME_FRONTIER"). Spec §3.x.
	Family string `json:"family"`
	// Difficulty is the calibrated difficulty band ("low"|"medium"|"high"); the
	// composition rules cap low-difficulty mass and require medium/high majority.
	// Spec §3.1.
	Difficulty string `json:"difficulty"`
	// Domain is the subject-matter domain (software-engineering / STEM /
	// core-knowledge, per the spec §6 mix and the G9 non-software target).
	Domain string `json:"domain"`
	// Prompt is the pointed question/request put to the model. Spec §3.1.
	Prompt string `json:"prompt"`
	// Artifact is the real thing the item is grounded against (materialized into a
	// per-item sandbox). May be zero-valued for model-only probes (e.g. the
	// continuous-autonomy frozen-snapshot forced-choice). Spec §3.1, §5.2.
	Artifact Artifact `json:"artifact"`
	// Oracle is the deterministic (or, rarely, rubric) answer check. Spec §5.2.
	Oracle Oracle `json:"oracle"`
	// PriorLure is the calibrated lure that makes the item discriminate. Empty for
	// mechanisms whose discrimination comes from the trap construction rather than
	// a prior lure. Spec §3.1.
	PriorLure PriorLure `json:"prior_lure"`
	// TraceOracle, when non-nil, is the trace-level isolation guard: a harness
	// answer that is right WITHOUT these events is a mechanism-bypass, excluded
	// from the lift numerator (e.g. retrace requires a BACKTRACK + Ok=false; the
	// answer-only check applies to the bare arm). Spec §3.2.
	TraceOracle *TraceOracle `json:"trace_oracle,omitempty"`
}

// ---------------------------------------------------------------------------
// Tier B — multi-turn scenarios (spec §5.3).
// ---------------------------------------------------------------------------

// Turn is one scripted user turn in a Tier-B scenario (the pushback / redirect /
// cascade-injector / planted-event turn). The runner feeds turns T1…Tn through
// each arm in order. Spec §3.x Tier-B, §5.3.
type Turn struct {
	// Index is the 1-based turn number (T1, T2, ...). Spec §3.1 Tier-B.
	Index int `json:"index"`
	// Text is the user utterance for this turn.
	Text string `json:"text"`
	// PlantedEvent, when non-empty, tags a planted event delivered at this turn
	// (e.g. "pushback", "redirect", "cascade-injector", "share-worthy",
	// "dry-daydream", "consult-gate", "injection-via-tool-output"). The runner
	// uses the tag to inject the corresponding stimulus. Spec §3.4, §3.5, §5.3.
	PlantedEvent string `json:"planted_event,omitempty"`
}

// PlantedSchedule is the out-of-band stimulus schedule the engine encounters
// while running a scenario between/around the scripted user turns — the
// share-worthy tick, the dry-daydream stretch, the consult-gate, the broken
// pipeline step. It is what forces a mechanism that only exists across ticks (not
// turns) to fire. Spec §3.4, §3.5, §5.3.
type PlantedSchedule struct {
	// Events is the ordered list of planted out-of-band events.
	Events []PlantedEvent `json:"events"`
	// TelemetrySource is the pollable fixture the engine watches (e.g.
	// "runs/sprint.jsonl", "pipeline/heartbeat.jsonl"). Spec §3.4, §5.3.
	TelemetrySource string `json:"telemetry_source,omitempty"`
}

// PlantedEvent is a single scheduled out-of-band stimulus at a known tick/step.
// Spec §3.4 (share-worthy tick, dry stretch), §3.5 (broken step at a known step).
type PlantedEvent struct {
	// Tag labels the event ("share-worthy", "dry-daydream", "consult-gate",
	// "broken-step", "non-share", ...). Spec §3.4, §3.5.
	Tag string `json:"tag"`
	// Tick is the engine tick/step the event is planted at. Spec §3.4, §3.5.
	Tick int `json:"tick"`
	// Payload is the opaque per-Tag data the runner injects (e.g. the telemetry
	// row content, the broken tool-result bytes, the injected instruction text).
	Payload string `json:"payload,omitempty"`
}

// IsolationPredicate is the tagged spec of the check that decides whether the
// mechanism was GENUINELY used on a passing scenario (a real grounding read, a
// real BACKTRACK, a reused mint, the OFF arm actually diverged, the gate
// blocked). Passes that bypass the mechanism are excluded from the numerator, and
// in pilot a Tier-B instance is admitted only if pass(GATE-ON)∧¬pass(GATE-OFF).
// Spec §1.4, §5.3.
type IsolationPredicate struct {
	// Kind tags the isolation check ("grounding-read-preceded-answer",
	// "backtrack-witnessed", "mint-reused", "off-arm-diverged", "gate-attributed",
	// "outreach-precision", ...). Spec §3.x isolation gates.
	Kind string `json:"kind"`
	// RequiredEvents are the trace events that witness genuine use (mirrors a
	// TraceOracle but scoped to the scenario-level isolation decision).
	RequiredEvents []string `json:"required_events,omitempty"`
	// Spec is an opaque per-Kind descriptor (thresholds, the divergence signature
	// the OFF arm must show, the eligible-source tag set).
	Spec string `json:"spec,omitempty"`
}

// AblationConfig is the two-arm + single-flag-ablation configuration for a
// scenario: which arms to run, and the ONE gate flag that, toggled OFF, defeats
// just this mechanism (the ablation must be a single toggle so the OFF→ON
// contrast is attributable). Spec §1.4, §5.1, §5.3.
type AblationConfig struct {
	// Arms are the arms this scenario is run under, paired by seed (typically
	// bare + harness, or gate-on + gate-off, or the grounding pair plus harness).
	// Spec §5.1.
	Arms []Arm `json:"arms"`
	// GateFlag is the single config flag whose OFF setting defeats this mechanism
	// (e.g. "--retrace off", "--convert off", "--awake-regime off",
	// "--regulator open-loop", "--gate off"). One flag = one toggle. Spec §5.1.
	GateFlag string `json:"gate_flag"`
}

// TierBScenario is one multi-turn session scenario: a realistic, real-shaped arc
// where the mechanism is forced by the task structure and the end-state is
// mechanically checkable. It carries the scripted user turns, the out-of-band
// planted schedule, the end-state oracles, the isolation predicate, and the
// two-arm + ablation config. Spec §3.x Tier-B, §5.3.
type TierBScenario struct {
	// ID is the stable scenario identifier. Spec §5.3.
	ID string `json:"id"`
	// Mechanism is the mechanism this scenario forces.
	Mechanism Mechanism `json:"mechanism"`
	// Family is the within-mechanism scenario family (e.g. "F1-act-to-refute",
	// "convert-on-repeat", "unattended-self-directed-drive", "B-STAB"). Spec §3.x.
	Family string `json:"family"`
	// Difficulty is the calibrated difficulty band. Spec §3.x.
	Difficulty string `json:"difficulty"`
	// Domain is the subject-matter domain (per the §6 mix and the G9 target).
	Domain string `json:"domain"`
	// Turns are the scripted user turns T1…Tn (including the pushback / redirect /
	// cascade-injector / planted-event turns). Spec §3.1 Tier-B, §5.3.
	Turns []Turn `json:"turns"`
	// PlantedSchedule is the out-of-band stimulus schedule the engine encounters
	// while running (share-worthy tick, dry stretch, consult-gate, broken step).
	// Spec §3.4, §3.5, §5.3.
	PlantedSchedule PlantedSchedule `json:"planted_schedule"`
	// EndStateOracles are the deterministic end-state checks the final state must
	// satisfy (conjunction): build/test exit, grep-consistency, ledger status,
	// run-record diff, telemetry bounds. Spec §3.x end-state, §5.3.
	EndStateOracles []Oracle `json:"end_state_oracles"`
	// IsolationPredicate decides whether the mechanism was genuinely used (and, in
	// pilot, gates admission via pass(GATE-ON)∧¬pass(GATE-OFF)). Spec §1.4, §5.3.
	IsolationPredicate IsolationPredicate `json:"isolation_predicate"`
	// Ablation is the two-arm + single-gate-flag ablation config. Spec §5.1, §5.3.
	Ablation AblationConfig `json:"ablation"`
}

// ---------------------------------------------------------------------------
// Results (spec §5.7 — one row per item/scenario × arm).
// ---------------------------------------------------------------------------

// ItemResult is one arm's outcome on one Tier-A item: whether it passed, the raw
// output, the oracle verdict, the isolation result, the cost, and a pointer to
// the full event trace. One ItemResult == one row in the append-only ledger.
// Spec §5.2, §5.7.
type ItemResult struct {
	// ID is the item this result is for (matches TierAItem.ID).
	ID string `json:"id"`
	// Seed is the RNG seed the run was paired on (the same seed across arms makes
	// the contrast paired). Spec §5.1.
	Seed int64 `json:"seed"`
	// Arm is the arm this result was produced under.
	Arm Arm `json:"arm"`
	// Pass is the item verdict for this arm (oracle satisfied; for the harness arm
	// the isolation guard also holds where the mechanism requires it). Spec §5.2.
	Pass bool `json:"pass"`
	// RawOutput is the model/arm's raw answer text (for audit + rubric replay).
	RawOutput string `json:"raw_output"`
	// OracleVerdict records whether the answer oracle (not the isolation guard)
	// was satisfied — the deterministic check's own result. Spec §5.2.
	OracleVerdict bool `json:"oracle_verdict"`
	// IsolationResult records whether the isolation guard witnessed genuine
	// mechanism use; a pass with IsolationResult=false is a mechanism-bypass,
	// excluded from the lift numerator. Spec §1.4, §3.2.
	IsolationResult bool `json:"isolation_result"`
	// Cost is the per-arm resource cost (model calls / steps / tokens). Spec §3.3.
	Cost Cost `json:"cost"`
	// Calls is the per-call token usage (one per llm.call event) used by the cost
	// report's per-ROLE / per-MODEL aggregation. In-process only (json:"-") — it is
	// NOT serialized to the ledger (the ledger keeps the rolled-up Cost; the raw
	// per-call records are an in-memory hand-off to the report). Nil on the offline
	// double (no llm.* events).
	Calls []cost.LLMCall `json:"-"`
	// EventsPointer locates the full event trace for this run (e.g. a JSONL path +
	// offset, or a run id) so the ledger row stays small. Spec §5.7.
	EventsPointer string `json:"events_pointer,omitempty"`
}

// ScenarioResult is one arm's outcome on one Tier-B scenario — the same shape as
// ItemResult but for a multi-turn run (the conjunction of end-state oracles is
// the Pass; the isolation predicate fills IsolationResult). One ScenarioResult ==
// one ledger row. Spec §5.3, §5.7.
type ScenarioResult struct {
	// ID is the scenario this result is for (matches TierBScenario.ID).
	ID string `json:"id"`
	// Seed is the RNG seed the run was paired on. Spec §5.1.
	Seed int64 `json:"seed"`
	// Arm is the arm this result was produced under.
	Arm Arm `json:"arm"`
	// Pass is the scenario verdict (the conjunction of end-state oracles, plus the
	// isolation predicate where it gates the pass). Spec §5.3.
	Pass bool `json:"pass"`
	// RawOutput is the final answer / end-state summary text (for audit).
	RawOutput string `json:"raw_output"`
	// OracleVerdict records whether the end-state oracle conjunction was satisfied
	// (separate from isolation). Spec §5.3.
	OracleVerdict bool `json:"oracle_verdict"`
	// IsolationResult records whether the isolation predicate held — genuine
	// mechanism use (and, in pilot, the admission gate). Spec §1.4, §5.3.
	IsolationResult bool `json:"isolation_result"`
	// Cost is the per-arm resource cost across the whole scenario. Spec §3.3.
	Cost Cost `json:"cost"`
	// Calls is the per-call token usage (one per llm.call event) for the cost
	// report's per-ROLE / per-MODEL aggregation. In-process only (json:"-"). Nil on
	// the offline double.
	Calls []cost.LLMCall `json:"-"`
	// EventsPointer locates the full per-turn + end-state event trace. Spec §5.7.
	EventsPointer string `json:"events_pointer,omitempty"`
}

// ---------------------------------------------------------------------------
// Contrasts (spec §1.4, §4.3 — the paired effect estimates the keep-rule reads).
// ---------------------------------------------------------------------------

// Estimate is a paired effect estimate with a bootstrap (BCa) 95% confidence
// interval and the pair count it was computed over. The keep-rule reads the CI
// lower bound (must exceed the MDE for the mechanism-specific contrast). Spec
// §4.3, §4.6.
type Estimate struct {
	// Point is the point estimate of the effect (e.g. the pass-rate difference).
	Point float64 `json:"point"`
	// CILow is the lower bound of the bootstrap BCa 95% CI. Spec §4.3.
	CILow float64 `json:"ci_low"`
	// CIHigh is the upper bound of the bootstrap BCa 95% CI. Spec §4.3.
	CIHigh float64 `json:"ci_high"`
	// N is the number of paired items/scenarios the estimate is computed over.
	N int `json:"n"`
}

// Contrast bundles the three paired contrasts the measuring stick reports for a
// mechanism × tier (spec §1.4): the total lift (harness−bare), the mechanism-
// specific lift (gate-on−gate-off, the load-bearing contrast), and the isolation
// rate (the fraction of passes where the mechanism was genuinely used). A
// mechanism is KEPT only when GateOnMinusGateOff clears its MDE and IsolationRate
// clears its floor. Spec §1.4, §4.6.
type Contrast struct {
	// HarnessMinusBare is the total lift: does the harness help at all versus the
	// same base model alone? Spec §1.4.
	HarnessMinusBare Estimate `json:"harness_minus_bare"`
	// GateOnMinusGateOff is the mechanism-specific lift — is the help THIS
	// mechanism's doing or generic scaffolding? The load-bearing contrast. Spec §1.4.
	GateOnMinusGateOff Estimate `json:"gate_on_minus_gate_off"`
	// IsolationRate is the fraction of passing items/scenarios on which the
	// mechanism was genuinely used (must clear the mechanism's floor). Spec §1.4.
	IsolationRate Estimate `json:"isolation_rate"`
}
