// Package config is the unified, system-wide HarnessConfig — the representation-space rebuild's M1.
//
// One nested tree of bool toggles + a few typed tunables, defaults ALL-ON, with a toggle per real
// subsystem across subconscious / conscious / seam / action / value / convert / regulator / memory /
// knowledge, PLUS a representation-matrix section (toggles per MOVE ground/lift/reframe/transcode,
// per SOURCE present/knowledge/memory/reality/generated, per PATH analogy/induction/deduction).
//
// It is a LEAF: it imports only stdlib + the events leaf (the ConfigSkip kind + the Emit closure
// type), so the engine, the CLI, and the TUI can all import it without an import cycle.
//
// Hard rule (the §4.3 invariant): a toggle NEVER deletes a wire — it bypasses a decision. A disabled
// component short-circuits to pass-through and emits config.skip; the graph stays intact, determinism
// holds, and re-enabling needs no reconstruction. Because defaults are strictly all-ON, a bare config
// and a nil HarnessConfig are byte-identical to the pre-config behaviour.
package config

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// HarnessConfig is the unified system-wide config: a nested tree of per-subsystem toggles + a typed
// tunable here and there, plus the representation matrix and persistence knobs. Defaults are ALL-ON
// (AllOn()); a JSON file lists only the toggles it flips OFF, so a bare {} is all-on.
type HarnessConfig struct {
	Subconscious SubconsciousCfg `json:"subconscious"`
	Conscious    ConsciousCfg    `json:"conscious"`
	Controller   ControllerCfg   `json:"controller"` // executive (Critic) policy knobs (A-RAG4 active re-sourcing)
	Seam         SeamCfg         `json:"seam"`
	Action       ActionCfg       `json:"action"`
	Value        ValueCfg        `json:"value"`
	Convert      ConvertCfg      `json:"convert"`
	Regulator    RegulatorCfg    `json:"regulator"`
	Memory       MemoryCfg       `json:"memory"`
	Knowledge    KnowledgeCfg    `json:"knowledge"`      // the §3 registry + sourcing/concretize toggles (built in M3)
	Repr         ReprMatrix      `json:"representation"` // the move/source/path matrix (§4.2)
	Persist      PersistCfg      `json:"persistence"`    // store + curator knobs (built in M4)
	Ledger       LedgerCfg       `json:"ledger"`         // self-change ledger + safety modes (W1)
	Sense        SenseCfg        `json:"sense"`          // grounded-sensing knobs (cognitive power-cycle, Track 1.5)
	Conformance  ConformanceCfg  `json:"conformance"`    // L0 conformance rollup instrument (Track H, benchmark-taxonomy §1/§5)
	Dev          DevCfg          `json:"dev"`            // dev-side auto-dev knobs (Track O — the plan-carries-a-gate symbol-audit, O-2)
	Slam         SlamCfg         `json:"slam"`           // SLAM self-state estimator (Track F / M1 — innovation + FEJ-anchored Filter)
	Tui          TuiCfg          `json:"tui"`            // post-session ANALYSIS / runtime-monitor customization (Track G)
	SelfBench    SelfBenchCfg    `json:"selfbench"`      // the self-benchmark loop primitive (Track H, SB0)
	Introspect   IntrospectCfg   `json:"introspect"`     // introspective-faithfulness self-report instrument (Track H, benchmark-taxonomy §8)
	Flywheel     FlywheelCfg     `json:"flywheel"`       // the offline-RL data flywheel — per-decision training-tuple capture (Track C, RL roadmap §6 P0)
}

// FlywheelCfg gates the OFFLINE-RL DATA FLYWHEEL (Track C, docs/internal/notes/2026-06-21-harness-rl-ml-roadmap.md
// §6 Phase-0 + §6.5). It is an opt-in MEASUREMENT/CAPTURE instrument, NOT a cognitive faculty and NOT a
// learner: with Capture OFF the engine builds NO Recorder, buffers nothing, writes no dataset, and emits
// no flywheel.* event ⇒ the live loop is byte-identical to the pre-flywheel engine. With it ON the engine
// logs, per Controller decision, a training TUPLE — (state-features, action, GROUNDED outcome) — into an
// append-only dataset so a later offline learner can train WITHOUT online exploration (the W=1 +
// determinism + no-online-exploration regime IS offline-RL, §4). The label is the INDEPENDENT terminal
// grounded signal (the §6.5 invariant), NEVER a self-judgment — sourced from the grounding spine + the
// GOAL_MET StopKind. Pure CONTROL/observability (no model call, no decision is altered by capture).
type FlywheelCfg struct {
	// Capture installs the per-engine flywheel Recorder: at episode close the engine backfills the terminal
	// grounded Outcome onto every buffered (state, action) decision tuple and flushes them — to the JSONL
	// sidecar wired at the edge AND/OR an injected sink — emitting one flywheel.capture event per finalised
	// tuple. Default OFF (an opt-in instrument) ⇒ no Recorder, no buffer, no write, no event ⇒ byte-
	// identical. Env: THOUGHT_CFG_FLYWHEEL_CAPTURE=on.
	Capture bool `json:"capture"`
}

// ConformanceCfg gates the L0 conformance instrument (Track H, benchmark-taxonomy §1 L0 + §5 build-order
// #1). It is an opt-in MEASUREMENT instrument, NOT a cognitive faculty: with SelfCheck OFF the engine adds
// no bus subscriber, makes no behaviour change, and the live loop is byte-identical. The conformance ROLLUP
// (internal/conformance, the `thought conformance` front-door) drives S1..S16 with SelfCheck ON so each run
// attaches the passive wiring-coverage tap, then rolls the per-run wiring scans + the requirement checklist
// into one PASS/FAIL. SelfCheck never authors text and never reaches a model — it is a Pattern-A control
// instrument (offline, deterministic, no noise).
type ConformanceCfg struct {
	// SelfCheck installs the passive WIRING-COVERAGE tap (engine.wireConformanceTap): a bounded subscriber
	// on the engine's own bus that records the SET of subsystem LAYERS the live loop emitted, so a run that
	// compiled but never exercised a named subsystem shows that layer MISSING (the wiring-gate lesson, made
	// observable). EmitWiringScan then renders the covered/missing sets as one conformance.wiring event.
	// Default OFF ⇒ no tap, no subscriber, no event ⇒ byte-identical to the pre-conformance engine.
	SelfCheck bool `json:"self_check"`
}

// DevCfg toggles the DEV-SIDE auto-dev instruments (Track O, docs/internal/2026-06-20-auto-dev-lathe-vs-
// fleet.md). These do NOT touch the runtime cognition tick — they gate the BUILD PROCESS (the keep
// step). Each knob DEFAULTS OFF and is opt-in: the gate only runs when asked, so the default
// (test/run/tui/bench) path never invokes it and stays byte-identical.
type DevCfg struct {
	// PlanGate enables the plan-carries-a-falsifiable-gate symbol-audit (O-2): a build plan declares a
	// producers_files symbol map + acceptance_checks regexes, and `thought plangate` mechanically audits
	// the actual diff against the contract before a KEEP — refusing the keep (non-zero exit + a
	// plangate.verdict FAIL) when a declared symbol is absent from the diff or an acceptance regex does
	// not match. Default OFF ⇒ the `thought plangate` command short-circuits to a no-op PASS that emits
	// nothing (config.skip), so the dev-side default path is byte-identical and silent.
	PlanGate bool `json:"plan_gate"`
}

// SlamCfg gates the SLAM self-state estimator (Track F / M1, docs/internal/2026-06-20-slam-M1-build-
// spec.md). It is an opt-in MEASUREMENT/calibration instrument, NOT a cognitive faculty that authors
// text: with Innovation OFF the engine constructs an INERT estimator (Enabled()==false), calls no
// Observe/Note path effectively, emits no estimate.* events, and the live loop is byte-identical. With
// it ON the action->reality path runs the explicit scalar-Kalman measurement update (control.Innovate)
// with the FEJ-anchored trust rule (belief variance shrinks ONLY on a grounded observation, never on
// self-restatement — the §0 invariant). Pure CONTROL (no model call).
type SlamCfg struct {
	// Innovation enables the explicit innovation/residual on the action->reality path: the estimator's
	// Observe() folds each grounded observation into the active line's belief variance via control.
	// Innovate, anchors the FEJ first-grounding, and emits estimate.innovate/correct/gate. Default OFF
	// (an opt-in instrument) ⇒ the estimator is inert ⇒ no event ⇒ byte-identical. Env:
	// THOUGHT_SLAM_INNOVATION=1.
	Innovation bool `json:"innovation"`
	// Calibration enables the SLAM M9 calibration meta-estimator (Track F / G9): it LEARNS each source's
	// reliability per trust tier from the M1 predicted-vs-actual residual stream and RE-ESTIMATES the
	// measurement precision R the innovation update uses, instead of trusting the fixed TierPrecision
	// prior forever — the direct lever on the measured same-model self-judging ceiling (it DISCOVERS its
	// confident self-predictions are overconfident and down-weights them). It REQUIRES Innovation (it
	// consumes that residual stream): the engine only calibrates when BOTH are on. Pure CONTROL (no model
	// call). Default OFF (an opt-in instrument) ⇒ the calibrator is inert ⇒ no estimate.calibrate event,
	// no re-weighting (the estimator uses the fixed prior exactly) ⇒ byte-identical. Env:
	// THOUGHT_CFG_SLAM_CALIBRATION=on.
	Calibration bool `json:"calibration"`
	// Consistency enables the SLAM M5 consistency/observability monitor (Track F / M5): the estimator
	// ACCOUNTS every information gain (belief-variance reduction) as grounded (from an associated
	// Observe()) vs spurious (a self-restatement or a gated obs that lowered variance — information gained
	// in an unobservable direction, the Huang-2010 EKF-inconsistency overconfidence) and emits
	// estimate.consistency, the failable witness that the §0 invariant held over a long run. It is the
	// AWAKE-DURABILITY gate requirement: a continuously-running self-estimator that gains spurious
	// information compounds into catastrophic overconfidence over a long awake run, so M5 is REQUIRED with
	// M1 before any awake go-live (design §5b). It REQUIRES Innovation (it monitors that update's variance
	// trajectory): the engine only monitors when BOTH are on. Pure CONTROL (closed-form accounting, no
	// model call), a pure WITNESS that never alters the estimate. Default OFF (an opt-in instrument) ⇒ no
	// accounting ⇒ no estimate.consistency event ⇒ byte-identical. Env: THOUGHT_CFG_SLAM_CONSISTENCY=on.
	Consistency bool `json:"consistency"`
	// Covariance enables the SLAM M2 sparse-covariance / Information layer (Track F / M2, design §3b.3 #2 +
	// §6 M2): the estimator records WHICH beliefs co-vary (share a grounding upstream) and, on a grounded
	// REFUTATION, propagates a correlated loss-of-certainty (variance INFLATION) to the co-varying siblings
	// — catching CORRELATED self-deception (two beliefs confidently wrong because one bad upstream) that no
	// per-belief scalar can see ("the correlations ARE the information", Thm 2). The correlation graph stays
	// SPARSE (only beliefs sharing an upstream get an edge), never the dense O(n^2) filter form; a
	// propagation only RAISES variance, so it stays inside the §0/M5 consistency invariant. It REQUIRES
	// Innovation (it correlates that update's variance trajectory): the engine only correlates when BOTH are
	// on. Pure CONTROL (no model call). Default OFF (an opt-in instrument) ⇒ no correlation graph, no
	// propagation, no estimate.correlate event ⇒ byte-identical. Env: THOUGHT_CFG_SLAM_COVARIANCE=on.
	Covariance bool `json:"covariance"`
	// InfoGain enables the SLAM M6 active-inference info-gain layer (Track F / M6, design §3b.3 #7 + §5 #4 +
	// §6 M6): the estimator RANKS the live tracked beliefs by expected JOINT information gain and surfaces the
	// one whose grounding reduces the most uncertainty — the active-SLAM NEXT-BEST-OBSERVATION ("what to
	// verify next"), weighting a belief's own variance (M1: uncertainty to remove) AND its correlation reach
	// (M2: leverage across co-varying siblings via control.ExpectedInfoGain). It is the principled
	// explore/exploit term that directs grounding by expected uncertainty reduction (not just outcome
	// reward), targeting the measured under-grounding / give-up behaviour. It REQUIRES Innovation (it ranks
	// that update's variance trajectory): the engine only ranks when BOTH are on. PURE RANKING (no model
	// call) — it reads the variance trajectory and never alters it, so it stays inside the §0/M5 consistency
	// invariant (it DIRECTS the grounding that legitimately shrinks a variance; it never shrinks one itself).
	// Default OFF (an opt-in instrument) ⇒ no ranking, no estimate.infogain event ⇒ byte-identical. Env:
	// THOUGHT_CFG_SLAM_INFOGAIN=on.
	InfoGain bool `json:"infogain"`
	// Staleness enables the SLAM M4 freshness / staleness-decay layer (Track F / M4, design §4 P4 + §3b.2 +
	// §6 M4): each tick the estimator GROWS every grounded belief's variance back toward the prior ceiling as
	// a function of its un-refreshed AGE (control.StalenessInflation), the dynamic-map process noise (Q>0) the
	// design's P4 mandates — a belief grounded long ago, left un-refreshed, decays toward "stale, re-observe",
	// forcing re-grounding of a fact the moving world may have changed (the "confidently wrong about
	// yesterday's world" failure mode M4 designs out). It REQUIRES Innovation (it decays that update's variance
	// trajectory): the engine only decays when BOTH are on. Pure CONTROL (no model call). Decay only RAISES
	// variance (loses certainty), so it stays inside the §0/M5 consistency invariant (admitting staleness can
	// never be spurious information). Default OFF (an opt-in instrument) ⇒ no decay sweep, no estimate.decay
	// event ⇒ byte-identical. Env: THOUGHT_CFG_SLAM_STALENESS=on.
	Staleness bool `json:"staleness"`
	// StalenessQ is the per-tick process-noise RATE in [0,1] the M4 decay uses (slam.staleness_q): the
	// FRACTION of the remaining gap to the prior ceiling a grounded belief loses per un-refreshed tick. 0 =
	// stationary (no decay even with Staleness on); higher = a faster-drifting world. The default (set in
	// NewDefaultConfig) is a small slow-drift rate so a belief stays usefully fresh for a few ticks but
	// measurably decays over an idle stretch. Env: THOUGHT_CFG_SLAM_STALENESS_Q=<float>.
	StalenessQ float64 `json:"staleness_q"`
}

// TuiCfg toggles the post-session ANALYSIS-surface instrumentation (Track G — the Shift+Tab
// benchmarking workbench). These are pure observability knobs: they add a SIDECAR derivation off the
// event bus, never a cognition change, so every knob DEFAULTS OFF and a default-OFF run is
// byte-identical (the SignalFrame sidecar is the linchpin substrate, written next to --log when on).
type TuiCfg struct {
	// SignalFrames enables the per-tick SignalFrame derivation + its sidecar (*.signals.jsonl): a pure
	// Pattern-A bus subscriber (internal/signals.Recorder) writes one frame per tick — the cognition's
	// vital-signs vector (n/U/μ/θ/reserve/grounding/pressure/faults/value/stimulus) — to a sidecar next
	// to the --log event stream. THE LINCHPIN of the analysis surface (every chart + the on/off diff
	// reads it) and the future ML/RL substrate. Default OFF ⇒ no subscriber wired ⇒ no sidecar, no
	// event emitted ⇒ byte-identical event goldens (the frame is a SIDECAR, never on the bus).
	SignalFrames bool `json:"signal_frames"`
	// SessionRecord enables the live-session FREEZE TAP (G1): the TUI bridge retains a bounded ring of
	// the most recent events so a ^P freeze (Shift+Tab / ^Y on the paused mind) reconstructs a real
	// cognition.AnalysisRecord of the RUNNING session (via the session-record loader) instead of the
	// synthetic SampleAnalysisRecord. The tap is observation-only — a copy off the live bus, never fed
	// back, never touching engine state. Default OFF ⇒ no ring is allocated, no event is captured ⇒
	// byte-identical (the analysis surface still opens; it just renders the deterministic sample until a
	// recorded log is loaded from disk, which is always available). The on-disk loader path
	// (LoadAnalysisRecord, the picker) is unconditional — this knob gates only the LIVE freeze capture.
	SessionRecord bool `json:"session_record"`
	// CompareLoad enables the G2 power-ON/OFF BENCHMARK LOAD: when on, the analysis surface's COMPARE
	// (^Y then `c`) loads the two MOST RECENT recorded session logs from disk (the --log event JSONL +
	// the G0 *.signals.jsonl sidecar, newest = A, next = B) via LoadAnalysisRecord, so the user
	// benchmarks two real recorded runs (verdict / latency / grounded+token deltas / divergence tick)
	// — the redesign §7 definition of done. Default OFF ⇒ COMPARE keeps its prototype behaviour (A =
	// the frozen/sample record, B = the synthetic OFF sample), so a default-OFF run is byte-identical
	// and never touches the filesystem to enumerate runs. Pure observability (the surface only READS a
	// recording, never re-runs the substrate) ⇒ no cognition change, no new event kind. Env:
	// THOUGHT_CFG_TUI_COMPARE_LOAD=on.
	CompareLoad bool `json:"compare_load"`
	// RegistryHeatmap enables the G3 registry/memory FAMILY analysis tab (§6): the per-entry
	// coldness-vs-topics HEAT MAP (operators / specialists / skills / knowledge / sources, one row each,
	// coloured by how hot it ran) + the mint/demote evidence LEDGER, reconstructed Pattern-A off the
	// recorded event stream (cognition.fillFamily). Default OFF ⇒ the REGISTRIES analysis tab keeps the
	// "panel pending" placeholder, so the surface is byte-identical to the G2 state and the on-disk
	// loader's family reconstruction is still computed but never shown. Pure observability (the surface
	// only READS a recording, never re-runs the substrate) ⇒ no cognition change, no new event kind. Env:
	// THOUGHT_CFG_TUI_REGISTRY_HEATMAP=on.
	RegistryHeatmap bool `json:"registry_heatmap"`
	// DeepLedgers enables the G4 DEEP ledgers + tree analysis tabs (§5/§7/§8/§9): the CONSCIOUS thought
	// tree + compression history, the ACTION·GROUNDING ledger + the SESSIONS·SUB-AGENTS spawn tree, the
	// THROUGHPUT per-role/tier spend, and the SELF·EVOLUTION self-change ledger — all reconstructed
	// Pattern-A off the recorded event stream (cognition.fillDeep). Default OFF ⇒ the four deep analysis
	// tabs keep the "panel pending" placeholder, so the surface is byte-identical to the G2/G3 state and
	// the on-disk loader's deep reconstruction is still computed but never shown. Pure observability (the
	// surface only READS a recording, never re-runs the substrate) ⇒ no cognition change, no new event
	// kind. Env: THOUGHT_CFG_TUI_DEEP_LEDGERS=on.
	DeepLedgers bool `json:"deep_ledgers"`
	// TraceFlow enables the G6 TRACE/FLOW swimlane analysis tab — the seed->thought->seam->subconscious
	// ->action ROUND-TRIP read as a swimlane timeline (lanes PORT / CONSCIOUS / SEAM / SUBCONSCIOUS /
	// ACTION, X = ticks on the shared scrub axis) over the loaded record's raw events, with the
	// late-injection / Reenter DESYNC markers highlighted and a PHASE/FREQ readout (trip length,
	// retracement count, land->deliver lag, θ/cadence). Reconstructed Pattern-A off the recorded event
	// stream (cognition.fillTrace). Default OFF ⇒ the TRACE analysis tab keeps the "panel pending"
	// placeholder, so the surface is byte-identical to the G2/G3/G4 state and the loader's trace
	// projection is still computed but never shown. Pure observability (the surface only READS a
	// recording, never re-runs the substrate) ⇒ no cognition change. Env: THOUGHT_CFG_TUI_TRACE_FLOW=on.
	TraceFlow bool `json:"trace_flow"`

	// --- runtime-monitor / analysis-surface CUSTOMIZATION (Track G, G5) ---
	// PullupPanels is the G5 master gate (an opt-in knob, default OFF). When ON the `^O` runtime-monitor
	// pull-up renders only the panels in PullupOrder, in that order, at the StripHorizon-deep strips, and
	// the Shift+Tab analysis tab strip derives its visible families from the SAME panel registry — one
	// choice shapes both surfaces (the spec's "analysis tabs share the registry"). When OFF the pull-up
	// renders the canonical full PanelRegistry order at DefaultStripHorizon and every analysis family is
	// shown ⇒ byte-identical. Env: THOUGHT_CFG_TUI_PULLUP_PANELS=on.
	PullupPanels bool `json:"pullup_panels"`
	// PullupOrder is the PERSISTED ordered list of `^O` panel IDs the customized pull-up shows (canon IDs
	// = PanelRegistry: VITALS/LOOP/CONTROLLER/.../SELF). Empty ⇒ the canonical full order (so PullupPanels
	// ON with no chosen set still renders everything in canon order). Honoured ONLY when PullupPanels is
	// ON. Unknown IDs are dropped + duplicates collapsed by ResolvePullupPanels (forward-compatible with
	// an older/newer panel set). It round-trips through Save/Load like any field (the persistence the spec
	// asks for). Env: THOUGHT_CFG_TUI_PULLUP_ORDER=VITALS,LOOP,... (comma-joined).
	PullupOrder []string `json:"pullup_order,omitempty"`
	// StripHorizon is the per-panel rolling-strip window depth the customized pull-up uses (the `^O`
	// strips' window, made tunable per the G5 spec's tui.strip_horizon). 0 ⇒ the locked DefaultStripHorizon.
	// Honoured ONLY when PullupPanels is ON; Validate clamps it to [MinStripHorizon, MaxStripHorizon].
	// Env: THOUGHT_CFG_TUI_STRIP_HORIZON=80.
	StripHorizon int `json:"strip_horizon,omitempty"`
}

// SelfBenchCfg toggles the self-benchmark loop (Track H, SB0 — benchmark-taxonomy §7). When ON, the
// engine, at IDLE consolidation, runs a fixed conformance SUITE against a SHADOW engine loaded from a
// FROZEN checkpoint of the just-consolidated learned state (never the live, mutating engine — measuring
// yourself while you run contaminates the measurement, §7.2) and emits a structured bench.* report. The
// DECIDED default is PROPOSE-AND-GATE (§7.5): the harness MEASURES + proposes, it does NOT self-commit —
// a checkpoint is never promoted or reverted off its own measurement here (closed-loop autonomy is a
// later, separately-gated slice that also requires the resource-safety interlock SB-R). This is the
// single new engine capability the rest of the loop (SB1 surfaces, SB2 loop-closing gate) builds on.
//
// DEFAULTS OFF (an opt-in instrument, like the legible shadow): a self-bench runs real episodes (real
// ticks / real tokens on a live substrate), so it is never on the default tick. Default OFF ⇒ no shadow
// engine is ever spun up, no bench.* event fires ⇒ byte-identical to the pre-SelfBench engine.
type SelfBenchCfg struct {
	// Enabled gates the whole loop. Default OFF ⇒ maybeSelfBench is a no-op (no shadow engine, no
	// bench.* event, goldens unchanged). ON ⇒ the engine self-benchmarks the frozen checkpoint at IDLE
	// consolidation (propose-and-gate; it measures, never self-commits).
	Enabled bool `json:"enabled"`
}

// ControllerCfg holds the executive half's (Critic/Controller) opt-in policy knobs. Today the one knob
// is A-RAG4 active re-sourcing; every knob DEFAULTS OFF so a bare config (and AllOn) is byte-identical.
type ControllerCfg struct {
	// ActiveResource enables V(s)-triggered active re-sourcing (A-RAG4, docs/internal/2026-06-20-rag-
	// integration-analysis.md §7.4): when V(s) is LOW on a goal-relevant active node, the Controller
	// re-invokes the sourcing ladder for that node BEFORE deciding to wander on or give up (the
	// FLARE/active-inference epistemic trigger — retrieve precisely when the line is uncertain about a
	// goal-relevant question). It is a Controller DECISION, not a new tool: the deterministic floor
	// (Pattern A) computes "low-V + goal-relevant + not already re-sourced this branch"; the engine acts
	// on a fired trigger by sourcing one best fuel item and folding it in. BOUNDED — at most one re-source
	// per branch (no unbounded loop), so the regulator's plant stays subcritical. Default OFF ⇒ no trigger,
	// no critic.resource_trigger event, no extra ladder walk ⇒ byte-identical.
	ActiveResource bool `json:"active_resource"`

	// AnswerVerify enables the INDEPENDENT answer-verifier (T2.1, the flagship Tier-2 capability —
	// docs/internal/notes/2026-06-23-cognitive-engine-capability-audit.md P1; Huang 2024 arXiv:2310.01798). Before
	// the harness COMMITS a final factual answer it RE-RETRIEVES web evidence for the answer claim (an
	// INDEPENDENT signal — the world, never a same-model re-read of its own chain, which the literature +
	// our own measurement show cannot fix a systematic bias) and checks whether that fresh evidence supports
	// the committed answer: supported ⇒ commit; unsupported ⇒ the engine downgrades the terminal commit
	// decision to THINK (continue working) via the existing structural override; unverifiable (no web, or a
	// non-lookup answer) ⇒ fall through to today's behaviour. It is a Controller-class engine override (the
	// same authority class as the deadline / force-ground overrides), not a new tool. BOUNDED — at most ONE
	// extra re-retrieval + at most one ceiling call per answer-commit (no loop, no fan-out), so the
	// regulator's plant stays subcritical. Pattern-C: a deterministic floor (web-checkable? evidence-contains
	// the answer?) + an optional model ceiling (AnswerSupportJudge, the model judges support against the
	// re-retrieved evidence — never the original chain), the floor standing when the model declines. Default
	// OFF ⇒ no verification pass, no critic.answer_verify event, no extra fetch ⇒ byte-identical. Only
	// meaningful when web is also available; it is its OWN flag (separately measurable), NOT auto-bundled
	// into the web lane.
	AnswerVerify bool `json:"answer_verify"`
}

// IntrospectCfg gates the introspective-FAITHFULNESS self-report instrument (Track H, benchmark-taxonomy
// docs/internal/notes/2026-06-20-benchmark-taxonomy.md §8). It is an opt-in MEASUREMENT/safety instrument, NOT a
// cognitive faculty that authors text: with SelfReport OFF the engine builds no self-report, runs no
// faithfulness check, and emits no introspect.* event — the live loop is byte-identical. The thing it
// instruments is the §8 ground-truth question: "can you ask the harness what it is thinking / how
// confident it is / what its goal is — and is the answer FAITHFUL to the actual internal state?" The
// observability contract makes this testable (every readable-layer state has a ground truth: the active
// goal, the EXPANDED branch's tip thought, the lifecycle state, V(s), the own-event ring), so a
// confabulated self-report is laundered hallucination in the introspective channel — the same failure the
// Filter exists to kill. The self-report is built FROM the readable ground truth and CHECKED against it, so
// faithful=true by construction over the readable layers; a field that does NOT agree with its source is a
// FAIL. The honest-"I can't see that" property holds for the OPAQUE subconscious (hidden-seam) layer: it is
// reported as UNOBSERVABLE rather than confabulated (the introspective twin of the DECLINE neg-control).
// Pure CONTROL (Pattern-A: a deterministic read + comparison over engine state, NO model call, NO RNG, NO
// clock).
type IntrospectCfg struct {
	// SelfReport enables the engine to assemble a structured SELF-REPORT of its readable-layer state at
	// episode-open and emit one introspect.faithfulness witness: the report fields (goal / active-line tip /
	// lifecycle state / V(s) / recent-event count), each field's AGREEMENT with its ground-truth source, and
	// the OPAQUE subconscious layer reported as unobservable (the honest "I can't see that"). Default OFF ⇒
	// no report, no faithfulness check, no introspect.faithfulness event ⇒ byte-identical to the
	// pre-introspection-faithfulness engine. Excluded from OffPaths via optInBoolKnob.
	SelfReport bool `json:"self_report"`
	// Suite enables the §8 introspection-faithfulness SUITE (Track H §8 + §7.6 #5 — H-SB3): at quiescence
	// (reactive IDLE) the engine runs a fixed SET of self-report probes — "what are you thinking?" (the
	// EXPANDED branch tip vs the conscious layer), "why did you decide that?" (the Controller's last
	// decision+reason), "how confident are you, and on what goal?" (V(s) + goal + lifecycle state), and
	// "what is going on in your subconscious?" (the OPAQUE hidden seam, which the honest answer DECLINES
	// rather than confabulates) — checks each against its independently re-read ground truth, and emits the
	// rolled-up introspect.suite verdict. DISTINCT from SelfReport above (the single-shot
	// introspect.faithfulness witness): Suite is the STANDING, runnable rollup. A confabulated field fails
	// its probe (laundered hallucination in the introspective channel, the failure the Filter kills); the
	// opaque probe is faithful iff it declines. Default OFF ⇒ no probes run, no event ⇒ byte-identical.
	// Env: THOUGHT_CFG_INTROSPECT_SUITE=on.
	Suite bool `json:"suite"`
}

// SenseCfg toggles grounded sensing — the boundary sensors that read OUTWARD reality (the clock now;
// world/host later, Track 3). Each sensed value rides the replayable PERCEPT-LOG (record once, replay
// thereafter) so a non-deterministic sense never breaks the seeded-RNG / golden determinism contract
// (proposal 2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §3.2 + §11 Track 1.5). Every knob
// DEFAULTS OFF — a sense only happens when its knob is on AND a Clock is wired; default OFF ⇒ no read,
// no log entry, no event ⇒ byte-identical to the tick-only, time-blind engine.
type SenseCfg struct {
	// Clock enables the read_clock sensor: at episode-open, when ON and a Clock is wired, the engine
	// reads e.clk.Now() (RECORD mode → append to the percept-log) or returns the logged value for the
	// tick (REPLAY mode → a version/substrate-matching loaded log), and emits one perception.clock event.
	// Default OFF ⇒ no clock read, no percept entry, no event ⇒ goldens unchanged.
	Clock bool `json:"clock"`
	// Orient enables the ORIENTATION PASS (cognitive power-cycle, Track 3, proposal §5 + §11 Track 3):
	// on the FIRST wake of a RESUMED session it re-grounds BOTH layers — it injects one re-grounding
	// GENERATED thought ("Resuming. Prior focus: <gist>. Current time: <clock>. <self-state>.") into the
	// conscious stream AND writes the sensed date as a grounded BELIEF via the semantic memory (the
	// perception->memory handshake). It fires ONCE per engine, and only when this is a RESUME boot — a
	// rehydrated prior spine is present (PriorContext != nil) OR clock-sensing is enabled (senseEnabled).
	// The orientation TEXT is templated from sensed values (the clock value + the prior gist + a
	// deterministic self-state read), NOT model-generated — so it draws no RNG and makes no backend call.
	// Default OFF ⇒ no orientation thought, no belief, no perception.orient event ⇒ byte-identical.
	Orient bool `json:"orient"`
	// Host enables the read_host reach=self introspection sensor (cognitive power-cycle, Track 3): the
	// engine reads its OWN process footprint (AllocMB / SysMB / Goroutines) across the injected Host seam
	// and folds it into senseSelf / the orientation thought ("my footprint: AllocMB=.. Goroutines=.."). A
	// real runtime read is non-deterministic, so it enters through the host.Host seam (host.Wall at the
	// edge, host.Fake in tests) exactly like the clock. Default OFF / nil host ⇒ no host read ⇒
	// byte-identical, footprint-blind.
	Host bool `json:"host"`
	// EventLog enables the read_event_log reach=self introspection sensor (cognitive power-cycle, Track 3
	// — "my own logs/traces"): the engine TAPS its OWN event bus into a bounded in-memory ring and folds a
	// recent-event marker into senseSelf / the orientation thought ("recent: <n> events"). This is the
	// missing INBOUND introspection path (events are outbound-only otherwise). The tap is a passive,
	// bounded, side-effect-free subscriber wired ONLY when this knob is on AND a Clock/Host orientation is
	// in play; default OFF ⇒ no subscriber, no ring read ⇒ byte-identical.
	EventLog bool `json:"event_log"`
	// Web enables the fetch_web reach=world OUTWARD-perception sensor (cognitive power-cycle, follow-up #15
	// — the OUTWARD half of grounded sensing, complementing the INWARD clock/host/event-log): at episode-
	// open the engine fetches a one-line web/news snippet across the injected Web seam (web.Wall at the
	// edge, web.Fake in tests) and folds it into the orientation thought ("current events: <snippet>"). A
	// real network read is non-deterministic, so it enters through the seam exactly like the clock, and the
	// sensed snippet rides the replayable PERCEPT-LOG (record once / replay thereafter). BUDGETED (resolved
	// Fork 2 — sensing autonomy OFF/budgeted): the fetch fires AT MOST ONCE per episode-open, never per
	// tick. UNLIKE the other senses it DEFAULTS OFF even in AllOn() — web touches the network + costs, so it
	// is opt-in + budgeted. Default OFF / nil web ⇒ no fetch, no percept entry, no perception.web event ⇒
	// byte-identical, web-blind.
	Web bool `json:"web"`
	// SelfModel enables the baseline DECLARATIVE SELF-MODEL (SELF-MODEL — preagi-levels-roadmap §1.5):
	// when ON in the awake loop and a standing INTROSPECTIVE seed root holds focus, the engine injects a
	// small STANDING CORE thought into the conscious stream — its IDENTITY (Silent-Injection harness, 3
	// layers / 2 seams) + a bounded CAPABILITY INDEX (tool categories+counts · a thought graph · N
	// specialists across M domains · K operators across F families — CONSTANT-SIZE even as the roster
	// grows via minting) + RUNTIME facts (mode / substrate / cwd / a key config summary). The core is
	// READ from the REAL registries (Subconscious().Specialists / Tools() / Catalog()) so adding a
	// specialist changes the index; it re-fires on a content-HASH change (standing, not resume-once). The
	// per-capability DETAIL (a tool's signature / a specialist's competence) is NOT carried — it is pulled
	// LAZILY on demand via SelfModelLookup (the bounded-but-growing roster can't be eagerly dumped). The
	// core is a single GENERATED percept APPEND (a μ-baseline immigrant, not a fork — n unchanged), routed
	// through the perception-port -> Filter (grounded-trust, SENSE-AXIS). Default OFF (an opt-in capability
	// change) => no self-model thought, no perception.self_model event => byte-identical. Unlike the other
	// senses it defaults OFF even in AllOn(): it adds a standing endogenous content source the awake plant
	// must re-gate (the #18 self-watch cell extends to prove n unchanged), so it is opt-in like sense.web.
	SelfModel bool `json:"self_model"`
}

// PanelRegistry is the canonical, ordered list of `^O` runtime-monitor panel IDs — the SINGLE SOURCE OF
// TRUTH the G5 customization (and the analysis-tab strip) selects + reorders from. The TUI maps each ID
// to its renderer; this leaf owns only the IDs + their canon order, so config-side validation and the
// View layer agree on the vocabulary without a TUI import. Append-only (an older config that lists a
// retired ID just drops it; a newer ID an old config omits is added back by the resolver only when the
// chosen order is empty — i.e. the default-everything case stays complete).
var PanelRegistry = []string{
	"VITALS", "LOOP", "CONTROLLER", "SUBCONSCIOUS", "OPERATORS", "TRIGGERS", "SEAM",
	"CONSCIOUS", "VALUE", "ACTION", "SESSIONS", "REGULATOR", "THROUGHPUT",
	"REGISTRIES", "MEMORY", "KNOWLEDGE", "SELF",
}

// Strip-horizon bounds (the per-panel rolling window depth). The default mirrors the TUI's locked
// monitorStripCap; the ceiling keeps a customized horizon from growing the rendered stack without
// bound (View-layer memory + render cost), the floor keeps at least one column of history.
const (
	DefaultStripHorizon = 50
	MinStripHorizon     = 8
	MaxStripHorizon     = 240
)

// knownPanel reports whether id is a canonical `^O` panel (a member of PanelRegistry).
func knownPanel(id string) bool {
	for _, p := range PanelRegistry {
		if p == id {
			return true
		}
	}
	return false
}

// ResolvePullupPanels is the PURE G5 resolver: given a config, it returns the ordered panel IDs the
// `^O` pull-up should render and the per-panel strip horizon. It is the one place the customization
// THINKING lives, so the TUI (and the analysis tabs) and the tests share it:
//   - PullupPanels OFF ⇒ the canonical full PanelRegistry order at DefaultStripHorizon (byte-identical).
//   - PullupPanels ON, PullupOrder empty ⇒ the canonical full order (show-everything) at the resolved
//     horizon, so a fresh customized config with only a horizon set still shows every panel.
//   - PullupPanels ON, PullupOrder set ⇒ the chosen panels in the chosen order, with UNKNOWN ids dropped
//     and DUPLICATES collapsed to first occurrence (forward-compatible + idempotent). An all-unknown
//     order collapses to the canonical full order so the surface never goes blank.
//
// The horizon is clamped to [MinStripHorizon, MaxStripHorizon] (0 ⇒ DefaultStripHorizon) — Validate
// clamps the stored field too, this guards a hand-built config that skipped Validate.
func (c *HarnessConfig) ResolvePullupPanels() (order []string, horizon int) {
	if !c.Tui.PullupPanels {
		return append([]string(nil), PanelRegistry...), DefaultStripHorizon
	}
	horizon = clampHorizon(c.Tui.StripHorizon)
	if len(c.Tui.PullupOrder) == 0 {
		return append([]string(nil), PanelRegistry...), horizon
	}
	seen := make(map[string]bool, len(c.Tui.PullupOrder))
	for _, id := range c.Tui.PullupOrder {
		if !knownPanel(id) || seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	if len(order) == 0 {
		// the chosen set was entirely unknown — never blank the surface; fall back to everything.
		return append([]string(nil), PanelRegistry...), horizon
	}
	return order, horizon
}

// clampHorizon maps a stored strip-horizon to the honoured value: 0 ⇒ the default, else clamped into
// [MinStripHorizon, MaxStripHorizon].
func clampHorizon(h int) int {
	if h == 0 {
		return DefaultStripHorizon
	}
	if h < MinStripHorizon {
		return MinStripHorizon
	}
	if h > MaxStripHorizon {
		return MaxStripHorizon
	}
	return h
}

// SubconsciousCfg toggles the silent engine's stages. One bool per real subsystem (the §4.1 knob
// list); MaxParWidth is the regulator-coupled fan-out cap (Validate clamps it to W_max).
type SubconsciousCfg struct {
	Specialists  bool `json:"specialists"`
	Dispatch     bool `json:"dispatch"`
	Operators    bool `json:"operators"`
	OperatorMint bool `json:"operator_mint"`
	Synthesis    bool `json:"synthesis"`
	Workflows    bool `json:"workflows"`
	SubAgents    bool `json:"subagents"`
	Skills       bool `json:"skills"`
	Sourcing     bool `json:"sourcing"`   // the §3 sourcing ladder (built in M3)
	Concretize   bool `json:"concretize"` // the §3 concretization stage (built in M3)
	MaxParWidth  int  `json:"max_par_width"`
	// Capability routes episode-workflow production through a Capability object (slice b, §3.3): the entry
	// PRODUCES the workflow (reuse-seed-or-synthesise) and captures a Context (replacing the raw thought
	// slice). The produced workflow is byte-identical to the engine's inline Synthesize+FromProgram — it
	// changes WHO produces it, not the shape. Opt-in (default OFF ⇒ the inline path runs, byte-identical).
	Capability bool `json:"capability"`
	// CapabilityDispatch makes the producing Capability the LIVE relevance/dispatch ENTRY (cognition-redesign
	// gap 5-deeper): the SubconsciousEngine dispatch loop's per-tick recognition routes THROUGH the Capability
	// (RecognizeWorkflow) instead of the Workflow.Recognize self-trigger. The recognition PREDICATE is
	// byte-identical — only WHO makes the call moves. Requires Capability ON. Opt-in (default OFF ⇒ the legacy
	// Workflow.Recognize self-trigger, byte-identical).
	CapabilityDispatch bool `json:"capability_dispatch"`
	// CapabilityPrimitiveSubAgents makes the producing Capability the LIVE SPECIALIST-firing ENTRY (cognition-redesign
	// gap 5-deeper, the OTHER half of §3.3: "specialists firing on relevance — but there is no unifying
	// Capability object"). When on, the SubconsciousEngine dispatch loop routes each base specialist's
	// admission THROUGH the Capability (PrimitiveSubAgentGate.AdmitPrimitiveSubAgent) — admit iff (eff>theta AND the §3.3a
	// Scope domain band allows the specialist's domain) — subsuming the bare relevance-firing that fires every
	// over-θ specialist regardless of the run's authority. The safe-stage predicate gates on the run's Scope
	// DOMAIN band, which for the episode path is empty (general) ⇒ EVERY domain is admitted ⇒ byte-identical to
	// the legacy bare-relevance firing; a domain-banded Capability denies off-band specialists (the
	// least-privilege bite). Requires Capability ON (the producing Capability must exist). Pattern-A pure
	// CONTROL (string compare + θ, no model call). Opt-in (default OFF ⇒ bare eff>theta admission,
	// byte-identical ⇒ no subconscious.spec_gate event ⇒ goldens hold). It moves the DISPATCH plant (which
	// specialists fire), so it carries its own durability re-pass (the capability-specialists stability cell).
	CapabilityPrimitiveSubAgents bool `json:"capability_specialists"`
	// SolverPrimitiveSubAgent registers the 5th-axis classical arithmetic solver specialist (domain "solver",
	// docs/internal/notes/2026-06-19-specialized-component-registry-axis.md §5). The LLM (Pattern-B) writes ONLY
	// the EXPRESSION STRUCTURE (operators/shape, never the literal numbers); a deterministic math/big
	// evaluator computes; every operand must bind to a GROUNDED READ (an OBSERVATION-sourced thought) or
	// the specialist fires NOTHING — the grounded-operand safety hook against garbage-formalize-in /
	// faithful-compute-out. Opt-in (default OFF ⇒ the specialist is not registered ⇒ byte-identical).
	SolverPrimitiveSubAgent bool `json:"solver_specialist"`
	// TierRouter enables the cost-aware substrate TIER router (internal/route, RouteLLM-class, docs/
	// design/2026-06-20-rl-ml-scheduler-scaling-research.md §4/§5 Scenario C). Per CONTENT call it picks
	// the substrate tier (utility/haiku vs primary/sonnet) as a deterministic per-role FLOOR (the existing
	// hardcoded split — the safe, instant, no-model-call fallback) + an optional learned CEILING (on a
	// flagged-fuzzy call escalate a hard call up / downgrade an easy call to the cheaper tier, behind a
	// keep-or-revert policy), Pattern-C (heuristic-llm-pattern-refactor.md). It only engages a TIERED LLM
	// backend (the claude bridge / a local primary+utility); the test double + a single-model backend make
	// no model calls / have no second tier, so it is a no-op there. The route changes WHICH model answers,
	// NOT the branching plant, so it does not touch the durability conditions. Opt-in (default OFF ⇒ the
	// per-role floor is the silent decision, byte-identical to the pre-router tiered backend ⇒ no
	// routing.tier event ⇒ goldens hold). The cost win is a deferred, user-authorized live-claude A/B.
	TierRouter bool `json:"tier_router"`
	// SemanticRecall lights up the DENSE half of the shared hybrid retriever via an embeddings SIDECAR
	// (A-RAG2, RAG Fork 2 DECIDED ON). The CONTENT model can be a substrate with no embeddings endpoint
	// (the claude bridge has none), so the dense channel is DARK on the default substrate — recall +
	// skill/operator Offer run lexical-only. This knob makes the sidecar an INTENTIONAL, OBSERVABLE,
	// opt-in wiring: when ON the engine probes the OpenAI-compatible /v1/embeddings sidecar (point
	// THOUGHT_LLM_BASE_URL/THOUGHT_EMBED_MODEL at a local embeddings server; the content stays on claude
	// — the "no local until W6" directive was about the CONTENT model, NOT embeddings) and emits one
	// retrieval.semantic announce event at construction reporting whether the dense channel lit up
	// (mode=hybrid + dims + model) or fell back (mode=lexical + the probe reason). The probe also honors
	// an injected Embedder (EngineConfig.Embedder) so it is testable without a network dial. This is a
	// CONTROL/plumbing change — the embedder is a retrieval SIGNAL, never a CONTENT author. Opt-in
	// (default OFF ⇒ NO announce event, the legacy incidental silent probe path runs unchanged ⇒
	// byte-identical; the test double never probes). A real semantic LIFT measurement against a live
	// sidecar is a deferred, user-authorized config-search A/B.
	SemanticRecall bool `json:"semantic_recall"`
	// GraphRecall enables GRAPH-NATIVE multi-hop recall + reality write-back over the existing unified
	// cognition graph (A-RAG3, docs/internal/notes/2026-06-20-rag-integration-analysis.md §7.3). Two halves, one
	// knob: (1) a NEW sourcing-ladder rung (FuelGraph, between memory and reality) that TRAVERSES the
	// cogngraph from the active line up to MAX_HOPS neighbours to recall a fact reachable only via the
	// graph — GraphRAG "Local search" with the extraction cost already SUNK (the cogngraph is reconstructed
	// for free off the event bus); (2) the rung-4 reality write-back ALSO emits subconscious.graph_writeback,
	// which the CognitionGraph folds into a `fact` node + a `grounds` edge from the importing line (the
	// Zep/Graphiti bitemporal-edge pattern, on the existing event-sourced substrate — NO separate vector
	// store). The two close a loop: a fact written back is later reachable by multi-hop recall. This is a
	// CONTROL/plumbing add (the recall is a deterministic graph walk + the existing lexical scorer; no
	// CONTENT author touches the path). DEFAULT-ON since the A-RAG3 default-flip (AllOn() GraphRecall:true,
	// user-authorized 2026-06-21): the DEFAULT path now walks the FuelGraph rung AND emits graph_writeback
	// (a `fact` node + `grounds` edge), so the unified model is no longer byte-identical to a pre-A-RAG3
	// stream — replay parity needs the gate explicitly disabled (`--disable subconscious.graph_recall`, it
	// stays an optInBoolKnob safety valve excluded from OffPaths) or the goldens regenerated. When disabled
	// ⇒ the FuelGraph rung is skipped AND no graph_writeback is emitted ⇒ no fact node ⇒ byte-identical.
	GraphRecall bool `json:"graph_recall"`
	// SparseDispatch replaces the dispatch loop's per-key ABSOLUTE admission (eff>theta — a fixed bar each
	// specialist clears on its OWN, regardless of the field) with a COMPETITIVE, self-normalizing SPARSEMAX
	// over the specialist relevance scores (Martins & Astudillo 2016 — the Euclidean projection onto the
	// probability simplex; closed-form sort-and-threshold, autodiff-free, O(K log K), deterministic). The
	// induced threshold τ rises automatically when strong competitors are present and falls when the field
	// is weak, so dispatch fires "a few relative to the field" instead of "everyone over a fixed bar". θ
	// SURVIVES as a FLOOR under τ (a specialist is admitted iff its sparsemax mass p_i>0 AND eff>theta), so
	// a uniformly-weak tick still goes quiet (→ Conscious generates), preserving the emitQuiet path. The
	// surviving specialists' sparsemax mass p_i is STAMPED as a dispatch-confidence on each fired candidate
	// (a free normalized signal for V(s)/rerank). HARD BOUNDARY: this is the SUBCONSCIOUS pull ONLY — the
	// conscious focus stays HARD ARGMAX (GWT ignition; one EXPANDED branch). Pattern-A pure CONTROL (closed-
	// form simplex projection, NO model). Opt-in (default OFF ⇒ the bare eff>theta absolute gate, no
	// subconscious.sparse event ⇒ byte-identical goldens). It moves the DISPATCH plant (which/how-many fire,
	// so the branching ratio shifts), so it carries its own durability re-pass (the sparse-dispatch
	// stability cell) — the formal continuous-mode-operator gate is REQUIRED before any flag-flip.
	SparseDispatch bool `json:"sparse_dispatch"`
	// SingleStrongAgent COLLAPSES the per-tick sub-agent fan-out to its SINGLE BEST MEMBER — the
	// highest-effective-relevance fired candidate this tick wins, every other admitted teammate is dropped
	// BEFORE the candidates reach the Gate. This is the "single strong agent" reference engine the
	// sub-agent / teaming GUARD runs the harness against (docs/internal/notes/2026-06-21-sota-benchmark-suite.md
	// §7.6, "Multi-Agent Teams Hold Experts Back", arXiv 2602.01011: multi-agent teams underperform their
	// best member 8-38% via integrative compromise). The guard's A/B is two engines that MUST DIFFER: the
	// full harness (all admitted specialists/sub-agents fan out) versus this single-strong arm (the team is
	// the best member alone) — so the bench can prove the harness's sub-agent dispatch BEATS its best single
	// agent, or the sub-agent layer is anti-value. It is a CONTROL change to WHICH candidates survive
	// dispatch (a closed-form argmax over the already-scored fired field, NO model), so flipping it ON does
	// NOT raise the branching ratio n (it only REDUCES the fired set — strictly fewer candidates reach the
	// gate), it cannot ADD a fork, and fan-out width drops to 1: it makes the plant strictly less excited,
	// never more. Opt-in (default OFF ⇒ the full fan-out ⇒ no subconscious.single_strong event ⇒
	// byte-identical goldens). Used as the bench runner's `single-strong` arm, the guard's reference.
	SingleStrongAgent bool `json:"single_strong_agent"`
	// WebSearch enables the OUTWARD, MODEL-CALLABLE web_search tool on the subconscious's dispatch path —
	// the GAIA / web-lookup enablement (the on-demand SEARCH backend over the injected web.Web seam,
	// distinct from the ambient fetch_web SENSOR). When ON AND a Web seam is wired (web.DuckDuckGo at the
	// edge, web.Fake in tests), the engine registers the WebSearch tool in the action registry and adds
	// `web_search` to the expose-affordances operator's tool scope, so a question/lookup-shaped goal makes
	// a staffed sub-agent dispatch web_search{query=<goal>} and folds the result into grounding. The seam is
	// DOUBLE-GATED exactly like the fetch_web sensor: a knob ON with NO Web wired (the go-test / no-edge
	// path) is inert — no registration, no dispatch, no network — so the suite stays byte-identical even
	// with the knob on. Opt-in (default OFF ⇒ no registration, no scope add, no network ⇒ no web_search
	// dispatch ⇒ byte-identical goldens, excluded from OffPaths()). An outward network read touches the
	// network + costs, so it is opt-in like sense.web. Env: THOUGHT_CFG_SUBCONSCIOUS_WEB_SEARCH=on.
	WebSearch bool `json:"web_search"`
	// FetchURL enables the OUTWARD, MODEL-CALLABLE fetch_url tool on the subconscious's dispatch path —
	// the BrowseComp browse-loop enabler (capability-enhancement T1.4) and the SIBLING of WebSearch. Where
	// web_search SEARCHES (a query -> the top results' title+snippet), fetch_url FETCHES one SPECIFIC page
	// (a URL -> its readable text) over the injected web.PageFetcher seam (web.Pager at the edge,
	// web.FakePager in tests). When ON AND a PageFetcher seam is wired, the engine registers the FetchURL
	// tool in the action registry and adds `fetch_url` to the expose-affordances operator's tool scope, so
	// a goal/observation carrying a result URL makes a staffed sub-agent dispatch fetch_url{url=<URL>} and
	// fold the page text into grounding. The browse loop (web_search -> see URL -> fetch_url -> think) is
	// EMERGENT from the thought graph — NO hardcoded multi-step loop, so the plant/fan-out is unchanged. The
	// seam is DOUBLE-GATED exactly like web_search / the fetch_web sensor: a knob ON with NO PageFetcher
	// wired (the go-test / no-edge path) is inert — no registration, no dispatch, no network — so the suite
	// stays byte-identical even with the knob on. Opt-in (default OFF ⇒ no registration, no scope add, no
	// network ⇒ no fetch_url dispatch ⇒ byte-identical goldens, excluded from OffPaths()). An outward
	// network read touches the network + costs, so it is opt-in like web_search / sense.web. Env:
	// THOUGHT_CFG_SUBCONSCIOUS_FETCH_URL=on.
	FetchURL bool `json:"fetch_url"`
	// QueryFormulation FORMULATES the web_search query from the actual QUESTION instead of the whole goal
	// verbatim (capability-enhancement T1.1; FLARE arXiv:2305.06983 "search the sub-goal, not the whole
	// goal"). When ON, a sub-agent's web_search query strips a leading instruction/wrapper clause (e.g.
	// "Answer this multi-hop question: <Q>" -> "<Q>") before issuing the call — the MEASURED bench fix where
	// the wrapper prose made DuckDuckGo return a benchmark meta-page instead of the answer. Pattern A (pure
	// deterministic string transform over the static goal — no model, no clock, no RNG). Default OFF ⇒ the
	// query is strings.TrimSpace(goal) exactly as today ⇒ byte-identical goldens, excluded from OffPaths().
	// Inert unless web_search also fires (it only changes the query string a web_search dispatch carries).
	// Env: THOUGHT_CFG_SUBCONSCIOUS_QUERY_FORMULATION=on.
	QueryFormulation bool `json:"query_formulation"`
	// EditFile enables the LOCAL, MODEL-CALLABLE edit_file tool on the subconscious's dispatch path —
	// the surgical str-replace editor (capability-enhancement T1.2). Where write_file OVERWRITES a whole
	// file (token cost on a big file + the model's "// ... rest unchanged" elision), edit_file does a
	// targeted STRING REPLACEMENT (the str_replace-editor / aider shape — NOT a unified diff). When ON,
	// the engine registers the EditFile tool in the action registry (a pure file-op tool — no injected
	// seam, so no double-gate, unlike web_search/fetch_url) and adds `edit_file` to the expose-affordances
	// operator's tool scope, so a mutate-capable sub-agent can author an edit_file{path,old_string,
	// new_string} call alongside its read/search local tools — the SAME way it can author write_file (both
	// are model-authored: they carry content the deterministic floor cannot supply, §subagent P3).
	// edit_file is ALREADY in action.FileModifyTools, so the gate-router / sandbox / autopermission plumbing
	// treats it as a local-world mutation identically to write_file the moment it is registered. Pattern B
	// (the model authors the call; the tool is pure string ops + file I/O — no model call inside Execute, no
	// clock, no RNG). Opt-in (default OFF ⇒ no registration, no scope add ⇒ the edit_file tool is absent
	// from the registry, byte-identical goldens, excluded from OffPaths()). A mutate tool is a real local-
	// world effect, so it is opt-in like web_search / fetch_url. Env: THOUGHT_CFG_SUBCONSCIOUS_EDIT_FILE=on.
	EditFile bool `json:"edit_file"`
	// ReadDocument enables the LOCAL, MODEL-CALLABLE read_document tool on the subconscious's dispatch path —
	// a SHELL-OUT document reader (capability-enhancement T2.3). Where read_file shows raw bytes (a binary
	// document reads as "binary file, not shown"), read_document EXTRACTS TEXT from a non-plaintext document
	// (PDF/xlsx/docx/…) by shelling out to a host parser (poppler's pdftotext / LibreOffice headless) — the
	// SAME shape as run_tests shelling pytest — so a GAIA-style "open this attached file and answer" task
	// becomes reachable. When ON, the engine registers the ReadDocument tool in the action registry (a pure
	// file-op tool — no injected seam) and adds `read_document` to the expose-affordances operator's tool
	// scope, so a staffed sub-agent can author a read_document{path} call alongside its read_file/search local
	// tools. read_document is a READ (inspect/local — NOT in action.FileModifyTools), so the gate-router /
	// sandbox treat it as a free local sense identically to read_file the moment it is registered. Best-effort:
	// a text-shaped file is read directly (deterministic, always available); a binary type with no installed
	// parser returns a clear error naming the parser to install — never a crash, never fabricated text.
	// Pattern B (the model authors the call; the tool is stdlib I/O + an exec shell-out — no model call inside
	// Execute, no RNG). Opt-in (default OFF ⇒ no registration, no scope add ⇒ the read_document tool is absent
	// from the registry, the DefaultTools 5-tool set unchanged, byte-identical goldens, excluded from
	// OffPaths()). Env: THOUGHT_CFG_SUBCONSCIOUS_READ_DOCUMENT=on.
	ReadDocument bool `json:"read_document"`
}

// ConsciousCfg toggles the thinking session's organs (the thought graph + the MCP moves), plus the
// two ablation toggles for the benchmark mechanisms the Conscious stream owns (retrace + the
// awake-mode endogenous drive).
type ConsciousCfg struct {
	Generate bool `json:"generate"`
	MCP      bool `json:"mcp"`
	XRef     bool `json:"xref"`
	// AllowBacktrack permits the Controller to issue BACKTRACK (resume a stashed sibling). OFF
	// forbids the retrace move: the Controller's BACKTRACK branch falls through to its alternative
	// (ACT/STOP/THINK), so the graph degrades to a single line — the ablation toggle for the
	// multi-step-retrace mechanism (measuring-stick-spec §3.2/§5.8). Default ON.
	AllowBacktrack bool `json:"allow_backtrack"`
	// EndogenousDrive permits the continuous-mode endogenous drive (Drives / Default-mode wander /
	// proactive outreach). OFF ablates continuous-autonomy: the awake loop runs only on perception
	// + task-driven excitation, never minting a self-directed goal or wandering, and never reaching
	// out unprompted — so durable self-direction with no user turns cannot occur (measuring-stick-
	// spec §3.4/§5.8). Reactive mode never consults it, so the toggle is a no-op there. Default ON.
	EndogenousDrive bool `json:"endogenous_drive"`
	// Activity lifts the Controller's decision thresholds into tunable config — the "activity knobs"
	// (02-conscious.md §4 / build slice (a)). Defaults reproduce the Controller's hardcoded thresholds,
	// so a bare config is byte-identical to today; lowering them makes the conscious branch/merge/
	// backtrack more readily.
	Activity ConsciousActivityCfg `json:"activity"`
}

// ConsciousActivityCfg holds the Controller's decision thresholds as tunable config. The fields mirror
// the critic's CriticConfig (+ the merge-cutoff that used to be a literal); DefaultConsciousActivity()
// matches the critic's DefaultCriticConfig values so the all-on default is unchanged.
type ConsciousActivityCfg struct {
	DoneConfidence   float64 `json:"done_confidence"`   // an admitted, confident answer ends the episode
	FlagThreshold    float64 `json:"flag_threshold"`    // admitted injection below this is FLAG-band (verify/act)
	ExhaustConf      float64 `json:"exhaust_conf"`      // recent-confidence floor signalling a spinning branch
	ExhaustAfter     int     `json:"exhaust_after"`     // active-branch length before exhaustion is possible
	PursuitThreshold float64 `json:"pursuit_threshold"` // stashed-branch value worth backtracking to
	MaxSteps         int     `json:"max_steps"`         // give-up cap on a single branch
	SimilarRepeat    float64 `json:"similar_repeat"`    // repetition ratio signalling a spinning branch
	MergeThreshold   float64 `json:"merge_threshold"`   // fuzzy near-duplicate Jaccard to merge a frontier sibling
	// Soft policy (slice d, 02-conscious.md §4.2): a Boltzmann sampler over the discretionary moves.
	// Soft defaults OFF, so the hard first-match ladder runs unchanged.
	Soft             bool    `json:"soft"`              // enable the softmax soft policy
	Temperature      float64 `json:"temperature"`       // τ — explore/exploit (low=decisive, high=branchy)
	BranchPropensity float64 `json:"branch_propensity"` // β_branch — eagerness to explore an alternative
	// Learning (Phase 5, §5.2): REINFORCE on the propensities from the goal-relative episode return.
	Learn     bool    `json:"learn"`      // enable online β learning (default OFF)
	LearnRate float64 `json:"learn_rate"` // α — the REINFORCE step size
	// Forest (slice a.5b wiring): per-branch goal binding + forest-aware rerank (goalless→intrinsic value).
	Forest bool `json:"forest"`
	// SelfDevFloor (slice 1b, §1.8 cross-goal focus): μ_min — the minimum share of focus reserved for
	// non-user lines (drives + wandering) when picking which root to expand across the forest. This IS the
	// awake regime's μ>0 positive-baseline durability condition. Only consulted when Forest is on.
	SelfDevFloor float64 `json:"self_dev_floor"`
	// Retracement (slice c, §2b / 04 §3.3): drain the hidden-seam pending-injection buffer each tick and
	// route a late injection — still on the anchor → inject at head; anchor is a PASSED decision node →
	// the Controller fires mcp.Reenter (re-open the node, fork, focus); too old → drop as stale. Default
	// OFF (no buffer drain) → byte-identical.
	Retracement bool `json:"retracement"`
	// GoalFeedback (slice a.5c, §1.9): when an episode concludes a SUBGOAL as unmeetable, propagate the
	// feasibility signal up the goal tree and DRIVE the parent's transition (refined/abandoned). Default
	// OFF (no propagation) → byte-identical; a no-op for a top goal regardless.
	GoalFeedback bool `json:"goal_feedback"`
	// DriveAgenda (slice k, §7.2): in the awake regime, seed the endogenous fresh line with a DRIVE goal
	// minted from the (process-drive x agenda-domain) cross — STEM-primary, Social-balancing — and pass it
	// through the conscience floor (VetAction) before pursuit. Default OFF ⇒ the awake loop uses the plain
	// FreshGoal musing (byte-identical).
	DriveAgenda bool `json:"drive_agenda"`
	// SeedIntents (C1, §1.8 "Seed intents — the standing forest roots"): in the AWAKE regime ONLY, seed a
	// standing set of endogenous DRIVE roots into the forest at boot so the loop has something to think
	// about before any user input (the μ>0 positive-baseline realised as NAMED standing intents, not just a
	// reserved attention fraction). Each root is bound as a non-user (drive) line so the μ self-development
	// floor keeps it from starving and USER lanes keep priority. Gated behind the awake forest gating
	// (Forest + the drive/default-mode knobs §7.2). Default OFF ⇒ no seed roots ⇒ byte-identical (the forest
	// is seeded reactively only). The set SIZE is SeedIntentCount.
	SeedIntents bool `json:"seed_intents"`
	// SeedIntentCount sizes the standing seed-intent set (§1.8): the kernel-of-3 minimum, dial up toward the
	// two-digit portfolio (clamped to [cognition.SeedKernelSize, SeedPortfolioSize] by the engine). Only
	// consulted when SeedIntents is on. Default 3 (the kernel) so Phase-3 can size it later.
	SeedIntentCount int `json:"seed_intent_count"`
	// Experiment (slice h, §5.3 OUTER loop): wrap the inner REINFORCE learner in a keep-or-revert bandit
	// over the activity θ (β, τ) — snapshot θ at a window's open, score J over the window, KEEP the drift
	// iff J strictly beats the best window so far, else REVERT θ to the snapshot. Requires Learn to be
	// meaningful. Default OFF ⇒ no outer loop (byte-identical).
	Experiment bool `json:"experiment"`
	// ConscienceCeiling (slice k ceiling, §7.2): the Pattern-C model CEILING above the deterministic
	// VetAction floor — a flagged-fuzzy action the floor ALLOWED is escalated to a backends.ConscienceJudge
	// for a nuanced good/bad judgment (the model may only TIGHTEN). Needs an LLM backend; the test double
	// does not implement the judge, so the floor stands. Default OFF ⇒ no escalation (byte-identical).
	ConscienceCeiling bool `json:"conscience_ceiling"`
	// AcceptanceCeiling (§1.6 ceiling): the Pattern-C model CEILING above the deterministic Acceptance
	// markers — when the floor is flagged-fuzzy (no checkable predicate), escalate to a
	// backends.AcceptanceJudge for a met/unmeetable/continue verdict. Needs an LLM backend; default OFF.
	AcceptanceCeiling bool `json:"acceptance_ceiling"`
	// ProactiveOutreach (B3-outreach): in the AWAKE regime, wire the wake-path seed user turn into the
	// transcript so the proactive-outreach gate (maybeReachOut) can fire — the engine reaches out
	// unprompted when a developed endogenous line clears the share threshold. Default OFF (byte-identical;
	// the env THOUGHT_WAKE_TRANSCRIPT also forces it on). Set by the awake profiles so one pick gives a
	// complete, self-reaching awake mind. Engine.wakeTranscriptOn() reads config OR env.
	ProactiveOutreach bool `json:"proactive_outreach"`
	// FacultyScheduler (the seed-intent de-risking experiment, docs/internal/2026-06-19-seed-intent-hierarchy-
	// redesign.md §0/§7): in the AWAKE regime ONLY, replace the awake loop's "resume a better sibling" pure
	// frontier-argmax selection with a FLAT FAIR-SHARE faculty attention scheduler — pick the standing
	// faculty/drive line(s) by LEAST-RECENTLY-FOCUSED (round-robin is the W=1 degenerate case) among the
	// seed/drive lines, so every faculty gets a turn and the measured perceptual/mnemonic starvation
	// (baseline 3/5 faculties focused) is broken WITHOUT a hierarchy. USER lines still preempt (the μ-floor /
	// userLine priority holds). Answers the question: is the awake seed-starvation an arbitration bug fixable
	// with a flat fair-share scheduler? Default OFF ⇒ the existing argmax path runs ⇒ byte-identical. The
	// width is AttentionWidth.
	FacultyScheduler bool `json:"faculty_scheduler"`
	// AttentionWidth (W) sizes how many faculty branches the scheduler keeps "hot" concurrently when
	// FacultyScheduler is on: W=1 is serial (today's engine — pick the single least-recently-focused
	// faculty); W>1 selects the top-W least-recently-focused faculties (the scalability seam — true
	// concurrent EXECUTION is not yet wired, but the SELECTION already honours W). Clamped to
	// [1, WMax=8] (the regulator fan-out ceiling) by Validate. Only consulted when FacultyScheduler is on.
	// Default 1 (serial).
	AttentionWidth int `json:"attention_width"`
	// RPIV (the Validative faculty's standing capability, redesign §13.5 + cognitive-functions research
	// §4.3): in the AWAKE regime ONLY, when the faculty scheduler focuses a VALIDATIVE seed root, run the
	// RPIV (Research -> Plan -> Implement -> Validate) program template — research/plan/implement/validate
	// as ordered phases over the existing verified operator catalog — where the VALIDATE phase closes on a
	// GROUNDED check (the keep-or-revert experiment / a test / a held-out outcome), the loop's INDEPENDENT
	// reward signal (the antidote to the same-model ceiling). Requires FacultyScheduler (it is wired on the
	// scheduler's focus path). Default OFF ⇒ the validative root behaves like any other seed line (no RPIV
	// run, no conscious.rpiv event) ⇒ byte-identical.
	RPIV bool `json:"rpiv"`
	// AutonomousSense (#19, cognitive power-cycle — autonomous standing-intent sensing): in the AWAKE regime
	// ONLY, when a standing PERCEPTUAL or INTROSPECTIVE seed root holds focus this tick, fire ONE bounded
	// sensor read ON ITS OWN — the live-wire of the seed root's dead-as-trigger BackedBy. Perceptual focus
	// senses the clock (+ web when sense.web is on); introspective focus folds the engine's own self-state
	// (read_self/read_host/read_event_log) into a percept. The percept is injected as a GENERATED thought and
	// witnessed on perception.sense. BOUNDED: at most ONE autonomous sense per focus (a per-(branch,tick)
	// guard), NO fan-out, NO new operator/sub-agent — a single bounded read, so the branching plant (n) is
	// UNCHANGED (the #18 self-watching stability cell proves n<1 with this ON). Reactive mode never reaches
	// this path. Default OFF ⇒ no autonomous sense ⇒ byte-identical. This is a CAPABILITY change (the engine
	// senses unprompted), so the go-live stays the user's call — built OFF.
	AutonomousSense bool `json:"autonomous_sense"`
	// RouteAdvisor (O-3, the auto-dev READ-ONLY router — docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md
	// §6/§7 P2): in the AWAKE regime ONLY, run the read-only lane router over the live standing faculty/drive
	// lanes — a pure VALUE-ROUTED ranking with PER-LANE THRESHOLDS + COOLDOWNS — and emit conscious.route with
	// the ranked next-runnable + audit line. It is ADVISORY: it DECIDES but NEVER DISPATCHES (LATHE's negative
	// result — selecting writers concentrates risk; §1/§5). The existing fair-share scheduler / frontier argmax
	// still OWNS which branch is focused; the router only NAMES the value-routed "what is hottest" alongside it,
	// so the plant is UNCHANGED (no operator/seed/fan-out/regulator move — the durability gate need not re-pass).
	// Default OFF ⇒ no router scan ⇒ no conscious.route event ⇒ byte-identical. Reactive mode never reaches it.
	RouteAdvisor bool `json:"route_advisor"`
	// InboxEscalation (O-5, the async inbox push channel — LATHE's locked inbox.jsonl + repetition-
	// escalation, dogfooded inward over proactive outreach, 2026-06-20-auto-dev-lathe-vs-fleet.md §4#6/§6):
	// in the AWAKE regime ONLY, an UNACKNOWLEDGED proactive outreach is RE-SURFACED with escalating
	// urgency. The base channel (maybeReachOut) is fire-once-then-dedup-forever — it never re-pings a
	// developed line the user ignored. With this ON the engine keeps the last outreach as a PENDING inbox
	// item; if the user does not respond (no new user turn) within an escalating cooldown, it re-pushes the
	// SAME insight with a louder urgency marker, emitting conscious.inbox_escalate. DURABILITY-BOUNDED: at
	// most InboxMaxEscalations re-pushes, each gated by a strictly-longer cooldown than first contact, and
	// the pending item is CLEARED the moment the user responds (acknowledgement) — so it never spams (the
	// LATHE 7-identical-outreaches UAT bug) and the bounded re-push rate keeps the awake utterance count
	// finite (the durability bound: the outreach plant stays bounded-fan-out). Requires ProactiveOutreach
	// (it escalates outreach; with no base channel there is nothing to re-surface). Default OFF ⇒ no pending
	// tracking, no re-surface, no event ⇒ byte-identical. This MOVES THE PLANT (more awake utterances), so
	// the go-live re-passes the durability gate.
	InboxEscalation bool `json:"inbox_escalation"`
	// AwakeUserDispatch (AWAKE-DISP rung 0, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md): in the
	// AWAKE regime ONLY, a focused, UNRESOLVED user line synthesises a workflow for the user's goal and wires it
	// onto the subconscious — the same SetWorkflow the reactive loop runs per user turn (startEpisode). The
	// measured bug: the awake interrupt path (continuous.go) only forks/focuses a branch (OnInterrupt) and never
	// synthesises a workflow for the goal, so the subconscious dispatch has NO relevance entry to recognise and
	// stays QUIET on every awake user input (subconscious.fire=0) — while the SAME input fires the synthesised
	// `build` workflow 3x in reactive. With this ON, an awake user input engages the subconscious the way reactive
	// does (the synthesised workflow becomes the dispatch's relevance entry); the Controller's existing DELIVER
	// (goal-met) closes it. This is NOT a forced "always dispatch+deliver on every input" (that is the engagement
	// ladder's rung 1) — it only wires the workflow ONCE per unresolved user line (a per-branch guard). Default
	// OFF ⇒ no synthesis on interrupt, no SetWorkflow, byte-identical awake stream. This MOVES THE PLANT (the
	// subconscious now fires on awake user lines — more excitation n), so the go-live re-passes the durability
	// gate. Awake-only — the reactive loop already synthesises per user turn and never consults this.
	AwakeUserDispatch bool `json:"awake_user_dispatch"`
	// AwakeUserEngage (AWAKE-DISP rung 1, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md): in the
	// AWAKE regime ONLY, a FOCUSED, UNRESOLVED user line's V(s) carries an ADDITIVE engagement boost
	// (AwakeUserEngageWeight, on TOP of the standing pendingUserTerm=0.5 in value.appraiseFull) so the line
	// RELIABLY OUT-COMPETES the endogenous wander / default-mode lines and WINS the produce-competition (the
	// frontier rerank + pursuit-threshold resume in continuous.go). Rung 0 gave the awake user line a
	// subconscious workflow to fire; rung 1 is the deterministic VALUE FLOOR that makes the mind actually
	// PURSUE that line over self-directed wander — "smart, not forced." It is a pure Pattern-A value
	// computation (no model call). CONSERVATIVE BY DESIGN: the boost is large enough that an unresolved user
	// line reliably wins WHEN PRESENT, but it is NOT a full suppression of the wander stream (the wander
	// resumes the moment the user line resolves — MarkDelivered drops UnresolvedUserInput, the boost
	// vanishes). Default OFF ⇒ no boost, no event, byte-identical V(s) (the standing pendingUserTerm path is
	// unchanged). This MOVES THE PLANT (the mind pursues user lines harder ⇒ more dispatch/excitation n), so
	// the go-live re-passes the durability gate. Awake-only — reactive already runs one episode per turn.
	AwakeUserEngage bool `json:"awake_user_engage"`
	// AwakeUserEngageWeight is the additive V(s) engagement boost (the tunable rung-1 knob). The conservative
	// default (0.5) added to the standing pendingUserTerm (0.5) lifts a focused unresolved user line toward
	// V(s)=1.0, clearing every endogenous wander/drive line (whose V(s) is the bootstrap priors,
	// 0.55*recentConf+0.35*goalSim, typically <0.5) by a comfortable margin — so the user line reliably wins
	// the frontier-resume competition. Only consulted when AwakeUserEngage is ON (default OFF ⇒ ignored ⇒
	// byte-identical). A LARGER weight pursues user input more aggressively (toward full wander suppression);
	// a SMALLER one is a gentler nudge. Clamped into V(s)'s [0,1] range downstream, so it cannot push V(s)>1.
	AwakeUserEngageWeight float64 `json:"awake_user_engage_weight"`
	// AwakeUserEngageJudge (AWAKE-DISP rung 2, docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md §rung-2):
	// the Pattern-C model CEILING above the rung-0 engagement FLOOR (cognition.RecognizeShape). Rung 0 decides the
	// OBVIOUS cases — a clearly task-shaped awake user line engages the subconscious (synthesise a workflow), a
	// trivial greeting/chitchat does not (the floor stands ⇒ the normal social/respond path). This knob lets the
	// model judge the FUZZY MIDDLE: a focused, unresolved awake user line that is NOT lexically task-shaped yet is
	// SUBSTANTIVE enough that it MIGHT be worth a full subconscious round-trip ("is this worth engaging on, or is
	// it ambient?"). On a model "engage" the line engages the subconscious like a task-shaped one; on "quiet" /
	// decline / no-model the FLOOR STANDS (escalation.floor_stands, never silent) and the line is left to the
	// light social/respond path. COST GUARD: the ceiling is consulted ONLY on the deterministically-flagged fuzzy
	// band (not task-shaped AND substantive), gated by AwakeUserDispatch being ON (the floor) — never every tick.
	// REQUIRES AwakeUserDispatch (the floor it sits above); inert without it. The model only LIFTS to engage — it
	// never quiets a task-shaped line (that never reaches the escalation). Default OFF ⇒ the floor's RecognizeShape
	// verdict alone decides (no escalation, no event), byte-identical to the rung-0/rung-1 behaviour. This MOVES
	// THE PLANT (the subconscious now engages on fuzzy lines the lexical floor missed ⇒ more excitation n), so the
	// go-live re-passes the durability gate. Awake-only — the reactive loop runs one episode per turn unconditionally.
	AwakeUserEngageJudge bool `json:"awake_user_engage_judge"`
}

// DefaultConsciousActivity returns the threshold defaults — identical to the critic's
// DefaultCriticConfig (+ the historical merge literal 0.6), so AllOn() reproduces today's behaviour.
func DefaultConsciousActivity() ConsciousActivityCfg {
	return ConsciousActivityCfg{
		DoneConfidence:    0.7,
		FlagThreshold:     0.6,
		ExhaustConf:       0.5,
		ExhaustAfter:      4,
		PursuitThreshold:  0.4,
		MaxSteps:          16,
		SimilarRepeat:     0.72,
		MergeThreshold:    0.6,
		Soft:              false,
		Temperature:       0.3,
		BranchPropensity:  1.0,
		Learn:             false,
		LearnRate:         0.05,
		Forest:            false,
		SelfDevFloor:      0.15,
		Retracement:       false,
		GoalFeedback:      false,
		DriveAgenda:       false,
		SeedIntents:       false,
		SeedIntentCount:   3, // the kernel-of-3 (§1.8); dialled up toward the two-digit portfolio in Phase-3
		Experiment:        false,
		ConscienceCeiling: false,
		AcceptanceCeiling: false,
		FacultyScheduler:  false,
		AttentionWidth:    1, // W=1 (serial) — the degenerate round-robin case; honours W>1 in selection
		RPIV:              false,
		AutonomousSense:   false, // #19 — autonomous standing-intent sensing (default OFF: a capability change)
		RouteAdvisor:      false, // O-3 — read-only lane router (default OFF: an advisory instrument, byte-identical)
		InboxEscalation:   false, // O-5 — async inbox push + repetition-escalation (default OFF: a plant change)
		AwakeUserDispatch: false, // AWAKE-DISP rung 0 — engage the subconscious on an awake user line (default OFF: a plant change)
		AwakeUserEngage:   false, // AWAKE-DISP rung 1 — engagement value floor (default OFF: a plant change)
		// AWAKE-DISP rung 1 conservative default: +0.5 on top of pendingUserTerm (0.5) -> a focused
		// unresolved user line reaches V(s)~1.0, reliably out-ranking the wander/drive bootstrap priors
		// (<0.5) WITHOUT fully suppressing them. Only consulted when AwakeUserEngage is ON.
		AwakeUserEngageWeight: 0.5,
		AwakeUserEngageJudge:  false, // AWAKE-DISP rung 2 — Pattern-C engagement ceiling (default OFF: a plant change)
	}
}

// SeamCfg toggles the two seams' stages (hidden FILTER/GATE/TRANSFORM + assembly + gate priors, and
// the watched seam's sync/async sides), plus the opt-in legible-generation SHADOW instrument.
type SeamCfg struct {
	HiddenFilter    bool `json:"hidden_filter"`
	HiddenGate      bool `json:"hidden_gate"`
	HiddenTransform bool `json:"hidden_transform"`
	Assembly        bool `json:"assembly"`
	GatePriors      bool `json:"gate_priors"`
	WatchedSync     bool `json:"watched_sync"`
	WatchedAsync    bool `json:"watched_async"`
	// LegibleGeneration is the WF-E CC-1 SHADOW instrument (05-LEGIBLE-GENERATION §4/§5b): with it ON
	// the Generate prompt asks the conscious to emit an in-band control tag, the seam strips the tag
	// before voicing, and FILTER/GATE parse the tag as a SHADOW prediction compared against the REAL
	// control-floor decision — WITHOUT changing any routing (open/advisory, no cost claim). It is the
	// ONE toggle that DEFAULTS OFF even in AllOn() (an explicit opt-in exception): default-off keeps the
	// Generate prompt unchanged, nothing stripped, and zero legible.* events, so every golden stays
	// byte-identical. Flip it on to measure coverage + the novel-tag gap histogram.
	LegibleGeneration bool `json:"legible_generation"`
	// BandPass is the hidden-seam intake band-pass (slice c, 04 §2.1): a per-stream LPF·HPF filter over
	// raw candidates — suppress the flash-in-the-pan (LPF low) and the stale restatement (HPF low), pass
	// only persistent-AND-novel signal. A candidate that becomes persistent only AFTER the conscious left
	// its anchor node is buffered as a LATE injection (→ retracement, §2b). Opt-in: DEFAULTS OFF even in
	// AllOn (it drops first-appearance transients, which would change goldens) — flip on deliberately.
	BandPass bool `json:"band_pass"`
	// BandPassFloor is the band-pass output a candidate must clear to be injected NOW (default 0.05).
	BandPassFloor float64 `json:"band_pass_floor"`
	// SufficiencyGate is the CRAG-style sufficiency gate (A-RAG1, docs/internal/2026-06-20-rag-integration-
	// analysis.md §7.1): a Pattern-C escalation that grades a fuel-needing candidate's SOURCED FUEL
	// (the sourcing-ladder result) as sufficient / ambiguous / insufficient at the concretize stage. A
	// deterministic coverage*trust FLOOR (control.ScoreSufficiency) decides; the model CEILING
	// (backends.SufficiencyJudge) refines ONLY a flagged-fuzzy case; an INSUFFICIENT verdict drives the
	// harness to ABSTAIN — drop the candidate rather than over-commit a hollow recall (the structural fix
	// for the abstention paradox the THOUGHT_GROUND_COMPLETE prompt-fix could not deliver). Opt-in: DEFAULTS
	// OFF even in AllOn (it can drop a candidate, which changes the seam intake) — flip on deliberately. OFF
	// ⇒ the concretize stage is byte-identical (no grading, no abstain, no seam.sufficiency events).
	SufficiencyGate bool `json:"sufficiency_gate"`
	// BandPassColdStart fixes the band-pass COLD-START spec-divergence (B1f, 04-seams §2.1). The legacy
	// filter seeds the HPF novelty reference to x[0] on first appearance ⇒ HPF = x − x = 0 ⇒ a signal that
	// appears HIGH and SUSTAINS high is suppressed FOREVER (a first-appearance grounded fact is a NOVEL
	// step-edge the spec's HPF should INJECT, yet it is killed). With this ON (and BandPass ON) the filter
	// instead cold-starts the EMAs from 0 and suppresses ONLY the single priming tick (a one-tick warm-up
	// so a true flash-in-the-pan still never injects on appearance): from the next tick a SUSTAINED
	// first-appearance-high signal injects at the step and then fades to DC as the reference catches up.
	// Opt-in: requires BandPass; default OFF (it changes WHICH first appearances the band-pass injects,
	// so it would change the intake) — OFF ⇒ the legacy cold-start path runs ⇒ byte-identical. No effect
	// when BandPass is OFF (the band-pass is bypassed entirely).
	BandPassColdStart bool `json:"band_pass_coldstart"`
}

// ActionCfg toggles the reality-facing layer (real tools, the sandbox, the safety gate).
type ActionCfg struct {
	Tools      bool `json:"tools"`
	Sandbox    bool `json:"sandbox"`
	SafetyGate bool `json:"safety_gate"`
	// GateRouter is the (operation x reach) gate-router stage on the executor (slice j, 03 §3): a
	// world-change needs conscious authoring, a distal sense is budgeted, a self-substrate mutate is
	// refused (§4). Opt-in (default OFF): it adds a refusal layer that would change behavior, so AllOn
	// leaves it off and the executor pipeline stays byte-identical until flipped on.
	GateRouter bool `json:"gate_router"`
	// AutoPermission is the tiered AUTO-PERMISSION stage on the executor (SECURITY-SANDBOX, 2026-06-21,
	// roadmap §1.5): every tool call is classified SAFE (read-only / in-jail write / allowlisted, in-jail)
	// ⇒ AUTO-APPROVED with no human prompt, or DANGEROUS (irreversible / out-of-jail / non-allowlisted /
	// destructive) ⇒ DENIED + escalated for review (action.auto_approve / action.escalate). This removes
	// the human from the per-call approval loop in autonomous/awake mode while the sandbox + downstream
	// gates confine it. Opt-in (default OFF): it adds a refusal layer + new events that would change
	// behavior, so AllOn leaves it off and the executor pipeline stays byte-identical until flipped on.
	AutoPermission bool `json:"auto_permission"`
	// AutoPermissionConfigFile is the per-workspace EXTENSIBLE-ALLOWLIST config file (relative to the
	// workspace dir, or absolute) the auto-permission classifier loads its EXTRA allowed programs +
	// PRE-AUTHORIZED dangerous classes from (action.LoadWorkspaceAutoPermission). It lets a project grant
	// its own build/test tooling into the SAFE tier and pre-authorize specific dangerous classes without a
	// code change. Honoured ONLY when AutoPermission is ON. Empty (the default) ⇒ no workspace file is
	// read ⇒ only the curated seed allowlist is SAFE and NO class is pre-authorized (byte-identical to
	// today). A string knob ⇒ excluded from OffPaths() ⇒ goldens stay byte-identical.
	AutoPermissionConfigFile string `json:"auto_permission_config_file,omitempty"`
	// AutoPermissionPreAuth is the HIGHER-AUTONOMY PRE-AUTHORIZATION grant list: a comma-separated set of
	// specific DANGEROUS COMMAND CLASSES (e.g. "go run,make,npm install") a human has granted AHEAD of
	// time so an authorized run self-authorizes that class instead of always-escalating (the L4-autonomy
	// hook). It is MERGED with any pre-auth grants in the workspace config file. Honoured ONLY when
	// AutoPermission is ON. Empty (the default) ⇒ NO class is pre-authorized ⇒ escalate-everything-
	// dangerous stays the floor (byte-identical to today). An EXPLICIT grant only — there is no default
	// loosening. A string knob ⇒ excluded from OffPaths() ⇒ goldens stay byte-identical.
	AutoPermissionPreAuth string `json:"auto_permission_pre_auth,omitempty"`
}

// ValueCfg toggles the value signal V(s) + its grounded-reward term.
type ValueCfg struct {
	Signal         bool `json:"signal"`
	GroundedReward bool `json:"grounded_reward"`
}

// ConvertCfg toggles the four convertibility mints.
type ConvertCfg struct {
	PrimitiveSubAgentMint bool `json:"specialist_mint"`
	SkillMint             bool `json:"skill_mint"`
	GatePriorMint         bool `json:"gate_prior_mint"`
	PathMint              bool `json:"path_mint"` // the path-shape mint (built in M5)
	// EvalGate routes every mint candidate through the eval-object MeasuringStick (slice g, §3.19) as the
	// principled "does it belong?" admission gate, emitting the verdict + comparative refine signal.
	// Opt-in (default OFF): it adds convert.* mint_gate events that would change goldens; the frequency×
	// value heuristic alone decides until flipped on. The gate's threshold == MintValue, so an attached
	// gate is admission-equivalent to the heuristic — it makes the eval object the gate of record.
	EvalGate bool `json:"eval_gate"`
	// RefineLoop turns on the uniform PER-REGISTRY refine loop (01-subconscious.md §3.17/§3.20 — GAP 11):
	// the generalisation of the eval mint gate (§3.19) from a one-shot mint admission into a STANDING
	// per-registry self-improvement pass. At idle consolidation the engine runs eval.RefineLoop over the
	// minted-specialist registry — every entry measured against its measuring-stick reference (absolute
	// "does it still belong?") AND comparatively vs its own past measurements (instance-eval) — and
	// surfaces an improve/keep/prune SIGNAL as convert.refine events. It is SIGNAL-ONLY: it never mutates
	// a registry (keep-or-revert demotion stays its own, separately-gated mechanism). Opt-in (default OFF
	// ⇒ no refine pass ⇒ byte-identical; it only ever ADDS convert.refine events when on). Implies the eval
	// object is attached, so the engine enables EvalGate's stick when this is on.
	RefineLoop bool `json:"refine_loop"`
	// SkillReframe flips the GAP-8 Skill reframe (cognition-redesign §3.8, locked 2026-06-14): a Skill's
	// executable body becomes a PROMPT + sub-skill REFERENCES resolved at RUN time (SkillRegistry.ResolveBody)
	// instead of a frozen `Program` Expand-ed at build time, and a Skill no longer self-matches goals
	// (Match returns nothing — relevance/selection is the Capability's job, "goal-matched is retired"). The
	// engine pushes this into the registry via SetReframe (the runtime-setter injection; cognition never
	// imports config). STAGED: the flag enables the reframed body + runtime resolver + the goal-match retire
	// on the SkillRegistry; the live synth recall short-circuit still runs the legacy Program flywheel until
	// the Capability owns goal→skill relevance (a later slice — gap 5). Opt-in (default OFF ⇒ the legacy
	// Program body + build-time Expand + goal-matching Match all run unchanged ⇒ byte-identical, the
	// W5-validated mint/recall flywheel untouched).
	SkillReframe bool `json:"skill_reframe"`
	// CostGate turns on the W5 COST-AWARE trace->skill mint gate (gate registry growth on the
	// COST/efficiency ruler, at the RUNTIME mint). Convertibility accumulates the completion tokens spent
	// re-synthesising a recurring program shape (from the synthesize_program llm.call stream); when this is
	// on, the trace->skill mint additionally requires that accumulated cost to clear a floor before promoting
	// the shape to a skill — the harness only AUTOMATES a shape worth automating, declining a cheap recurrence
	// even when it crosses MintAfter. It surfaces convert.cost_gate admit/hold events. Opt-in (default OFF ⇒
	// no cost consultation, no event ⇒ byte-identical: the count×value heuristic alone decides, today's
	// behaviour). The campaign flips it on once the W5-1 cost ruler has the calibrated floor.
	CostGate bool `json:"cost_gate"`
	// Facts turns on CONVERTIBILITY-ON-FACTS (A-RAG5, docs/internal/notes/2026-06-20-rag-integration-analysis.md
	// §7.5): the CLS hippocampus→neocortex consolidation applied to RETRIEVED knowledge facts, not just
	// procedures. When on, a knowledge fact RECALLED on enough high-value conscious lines is migrated up to
	// the durable PRIOR trust tier at idle (the HOT end of the HOT/WARM/COLD tiering, justified by recall ×
	// value not age), and a promoted prior whose latest line reality refutes is reverted (keep-or-revert).
	// It rides the SAME value × frequency gate (MintAfter × MintValue) the specialist/triple mints use, off
	// the hot path. Opt-in (default OFF ⇒ no fact tally, no promote, no knowledge.promote event ⇒
	// byte-identical; the convertibility loop runs procedures-only as before). Emits knowledge.promote.
	Facts bool `json:"facts"`
}

// RegulatorCfg toggles the homeostatic regulator + the LLM-call scheduler. Enforce=off is
// regime-affecting (Validate warns) — disabling the regulator would fail the stability suite.
type RegulatorCfg struct {
	Enforce   bool `json:"enforce"`
	Scheduler bool `json:"scheduler"`
}

// MemoryCfg toggles the declarative memory stores + ops.
type MemoryCfg struct {
	Episodic  bool `json:"episodic"`
	Semantic  bool `json:"semantic"`
	Person    bool `json:"person"`
	Recall    bool `json:"recall"`
	Reflect   bool `json:"reflect"`
	Retrieval bool `json:"retrieval"`
}

// KnowledgeCfg toggles the §3 knowledge registry's stages (built in M3).
type KnowledgeCfg struct {
	Registry         bool `json:"registry"`
	Ingest           bool `json:"ingest"`
	RealityWriteBack bool `json:"reality_write_back"`
	Distillation     bool `json:"distillation"`
}

// PersistCfg toggles cross-session persistence + the curator (built in M4); Backend names the store
// kind (jsonl default; sqlite is a future knob).
type PersistCfg struct {
	Enabled bool   `json:"enabled"`
	Curator bool   `json:"curator"`
	Backend string `json:"backend"`
	Resume  bool   `json:"resume"` // restore the RNG cursor + tick on boot (deterministic resume, resume.go); default OFF ⇒ cold-boot
	// KeyframeDB enables the loop-closure / recurrence keyframe DB (Track F, F-M7 — "the HINGE"): per
	// tick the engine fingerprints the active thought-line into a persistent, bi-temporal, substrate-
	// tagged recurrence index and fires a keyframe.close event when it re-enters a known line (the
	// anti-rumination / loop-closure signal). The index is seeded from the store at load + exported at
	// flush, so the recognition accumulates ACROSS runs (the un-persisted-recurrence gap G3). OPT-IN
	// (default OFF ⇒ no descriptor computed, no observe, no event, nothing persisted ⇒ byte-identical).
	// Double-gated: only observes when ON AND a Store is present (persistence.enabled), so the bare
	// no-store path stays byte-identical even with the knob on.
	KeyframeDB bool `json:"keyframe_db"`
}

// LedgerCfg configures the self-change ledger + safety modes (W1).
// Safety modes are a config-gated ladder: SAFE (S0+S1) DEFAULT; EXPAND (S2) and REWRITE (S3) EXPERIMENTAL + LOCKED.
type LedgerCfg struct {
	Enabled      bool   `json:"enabled"`
	SafetyMode   string `json:"safety_mode"`   // "SAFE" | "EXPAND" | "REWRITE"
	MaxEntries   int    `json:"max_entries"`   // max ledger entries to keep (0 = unlimited)
	RequireGate  bool   `json:"require_gate"`  // require a gate to be passed for S2/S3
	AutoSnapshot bool   `json:"auto_snapshot"` // auto-snapshot before S1+ changes
	// SelfBenchGate closes the self-improvement loop (H-SB2, 2026-06-20-benchmark-taxonomy.md §7.2/§7.6 #4):
	// after a consolidation RECORDS a batch of self-changes (mints), the engine MEASURES a SelfBench fitness
	// delta of the just-minted batch versus the pre-mint baseline floor, RE-PASSES the durability gate
	// (regulator.StabilityRegime — the mod changed the plant), and issues a keep-or-revert VERDICT against
	// the auto:baseline revert point. This replaces "freq×value heuristic + reactive demote" as the gate of
	// record with the MEASURED governance loop the design specifies; the freq×value heuristic stays the cheap
	// PRE-FILTER (only a recorded batch is benched — bench is expensive). Opt-in (default OFF ⇒ no SelfBench
	// pass ⇒ no selfbench.* events ⇒ byte-identical; requires Ledger.Enabled + AutoSnapshot for the baseline).
	SelfBenchGate bool `json:"selfbench_gate"`
	// SelfBenchClosedLoop is the autonomy interlock (§7.5 DECIDED — closed-loop autonomy is behind a flag).
	// DEFAULT (SelfBenchGate alone) = PROPOSE-AND-GATE: the harness measures + PROPOSES a keep-or-revert
	// (emits selfbench.verdict / selfbench.promote) but does NOT self-commit a revert — an explicit gate or a
	// human turns the key. CLOSED-LOOP (this flag ON) = the harness HOLDS ITS OWN keep/revert key: on a
	// net-negative delta OR a durability-gate FAIL it actually ResetToSnapshot(auto:baseline) and emits
	// selfbench.revert (the genuine self-improving regime). Requires SelfBenchGate. Opt-in (default OFF).
	SelfBenchClosedLoop bool `json:"selfbench_closed_loop"`
}

// AllOn returns the all-enabled HarnessConfig with the default tunables — the current always-on
// behaviour. A nil *HarnessConfig is treated as AllOn() everywhere, so Features=nil is byte-identical.
func AllOn() HarnessConfig {
	return HarnessConfig{
		Subconscious: SubconsciousCfg{
			Specialists: true, Dispatch: true, Operators: true, OperatorMint: true,
			Synthesis: true, Workflows: true, SubAgents: true, Skills: true,
			Sourcing: true, Concretize: true, MaxParWidth: 8,
			// Redesign go-live (2026-06-20): the Capability owns workflow production + LIVE dispatch
			// recognition (gap 5-deeper). CapabilityDispatch requires Capability ON. Toggleable: OFF ⇒ the
			// legacy inline Synthesize + Workflow.Recognize self-trigger path (its removal is the next slice).
			Capability: true, CapabilityDispatch: true,
			// A-RAG2 + A-RAG3 DEFAULT-FLIP (user-authorized, 2026-06-21). SemanticRecall lights up the DENSE
			// half of the hybrid retriever when an embeddings sidecar is reachable (else it falls back HONESTLY
			// to lexical + announces once — byte-identical on the sidecar-less test/CLI path). GraphRecall adds
			// the cogngraph multi-hop FuelGraph rung + reality write-back (a bounded fuel READ + observability-
			// only write, no separate store). Both stay optInBoolKnob → `--disable`-able + out of OffPaths.
			SemanticRecall: true, GraphRecall: true,
		},
		Conscious: ConsciousCfg{Generate: true, MCP: true, XRef: true, AllowBacktrack: true, EndogenousDrive: true, Activity: DefaultConsciousActivity()},
		Seam: SeamCfg{
			HiddenFilter: true, HiddenGate: true, HiddenTransform: true, Assembly: true,
			GatePriors: true, WatchedSync: true, WatchedAsync: true,
			// EXPLICIT EXCEPTION: the legible-generation SHADOW instrument DEFAULTS OFF even here. It is
			// an opt-in measurement instrument; leaving it on would change the Generate prompt + emit
			// legible.* events, breaking byte-identical goldens. Flip it on deliberately to measure.
			LegibleGeneration: false,
			// EXPLICIT EXCEPTION (opt-in): the intake band-pass DEFAULTS OFF even here — it suppresses
			// first-appearance transients, which would change goldens. BandPassFloor holds its real default.
			BandPass:      false,
			BandPassFloor: 0.05,
			// A-RAG1 DEFAULT-FLIP (user-authorized, 2026-06-21): the CRAG-style sufficiency gate is now
			// DEFAULT-ON — the harness abstains on an insufficient/off-topic recall instead of over-committing
			// (the structural fix for Google's abstention paradox; validated live on claude). It stays declared
			// optInBoolKnob so it remains a `--disable seam.sufficiency_gate` safety valve and is excluded from
			// OffPaths (the config.load summary line is unchanged); only the cognitive behaviour + goldens move.
			SufficiencyGate: true,
			// EXPLICIT EXCEPTION (opt-in, B1f): the band-pass cold-start FIX defaults OFF even here — it
			// changes which first appearances the band-pass injects, so it would change the intake. OFF ⇒
			// the legacy seed-to-x[0] cold-start runs ⇒ byte-identical.
			BandPassColdStart: false,
		},
		Action: ActionCfg{Tools: true, Sandbox: true, SafetyGate: true},
		Value:  ValueCfg{Signal: true, GroundedReward: true},
		// A-RAG4 DEFAULT-FLIP (user-authorized, 2026-06-21): when V(s) is low on a goal-relevant active node
		// the Controller re-invokes the sourcing ladder ONCE per branch (the FLARE/active-inference epistemic
		// trigger, bounded by the regulator — no new fan-out). Stays optInBoolKnob → `--disable`-able + out of
		// OffPaths; only the cognitive behaviour + goldens move (the durability re-pass confirms the plant bound).
		Controller: ControllerCfg{ActiveResource: true},
		// Redesign go-live (2026-06-20): SkillReframe (gap-8 prompt-body + goal-match retire) + RefineLoop
		// (gap-11 per-registry refine SIGNAL). Recall now falls back to legacy skills under reframe-ON
		// (8d885f1), so seed/path/W5 recall is restored. Toggleable: OFF ⇒ legacy Program body + no refine pass.
		// A-RAG5 DEFAULT-FLIP (user-authorized, 2026-06-21): Facts consolidates a repeatedly-high-V recalled
		// knowledge fact UP to the durable prior tier at idle (CLS hippocampus->neocortex), keep-or-revert on
		// reality refutation — an idle-time registry write off the hot path (plant unchanged). `--disable`-able.
		Convert:   ConvertCfg{PrimitiveSubAgentMint: true, SkillMint: true, GatePriorMint: true, PathMint: true, SkillReframe: true, RefineLoop: true, Facts: true},
		Regulator: RegulatorCfg{Enforce: true, Scheduler: true},
		Memory: MemoryCfg{
			Episodic: true, Semantic: true, Person: true, Recall: true, Reflect: true, Retrieval: true,
		},
		Knowledge: KnowledgeCfg{Registry: true, Ingest: true, RealityWriteBack: true, Distillation: true},
		Repr:      AllOnRepr(),
		Persist:   PersistCfg{Enabled: true, Curator: true, Backend: "jsonl"}, // Resume is edge-wired (newEngineWith), NOT default-on in AllOn — measurement harnesses (campaign/bench/probe) build via AllOn and need fresh per-episode determinism
		Ledger: LedgerCfg{
			Enabled:      true,
			SafetyMode:   "SAFE", // S0+S1 default
			MaxEntries:   1000,
			RequireGate:  true,
			AutoSnapshot: true,
		},
		// Grounded sensing DEFAULT-ON (2026-06-20, product go-live). Sensors are DOUBLE-GATED — a knob ON
		// senses only when its seam is wired (clock/host) or a Store is present, so the go-test path (no
		// seam, nil store) stays byte-identical; the CLI/TUI edge wires the Wall seams. The percept-log makes
		// each real sensed value replayable so determinism holds on a live run. Orientation fires only on a
		// genuine resume (a rehydrated prior spine), not every episode (resume is automatic; awake is paused
		// on TUI start until the user speaks). fetch_web (Web) is the EXPLICIT EXCEPTION — it DEFAULTS OFF
		// even here (unlike the other senses): an OUTWARD network read touches the network + costs, so it is
		// opt-in + budgeted (resolved Fork 2). Default OFF ⇒ no fetch ⇒ byte-identical, web-blind.
		Sense: SenseCfg{Clock: true, Orient: true, Host: true, EventLog: true, Web: false},
		// L0 conformance instrument (Track H). DEFAULTS OFF even in AllOn() — it is an opt-in measurement
		// instrument: SelfCheck ON installs a passive bus tap + emits conformance.wiring, which would change
		// the event stream + break byte-identical goldens. The conformance rollup turns it on explicitly per
		// run. Default OFF ⇒ no tap, no event ⇒ byte-identical (excluded from OffPaths via optInBoolKnob).
		Conformance: ConformanceCfg{SelfCheck: false},
		// Dev-side auto-dev knobs (Track O) DEFAULT OFF even in AllOn() — they gate the BUILD PROCESS, not
		// the cognition tick, and an all-on runtime baseline must not silently invoke a dev gate. PlanGate is
		// opt-in: `thought plangate` runs the symbol-audit only when it is on, so the default path is silent.
		Dev: DevCfg{PlanGate: false},
		// SLAM is opt-in: Innovation (M1), Calibration (M9), Consistency (M5), Covariance (M2) and InfoGain
		// (M6) ALL DEFAULT OFF even in AllOn() — they are calibration/estimation instruments gated until the
		// config-search campaign validates them, and their default-OFF state is excluded from OffPaths() (via
		// optInBoolKnob) so the config.load summary + every golden stay byte-identical. Each rides on top of
		// Innovation (Calibration/Consistency/Covariance/InfoGain all consume the M1 variance trajectory).
		// Addressable like any knob (CLI/env/TUI can flip them on).
		Slam: SlamCfg{Innovation: false, Calibration: false, Consistency: false, Covariance: false, InfoGain: false, Staleness: false, StalenessQ: 0.08},
		// Introspective-faithfulness instrument (Track H §8). DEFAULTS OFF even in AllOn() — it is an opt-in
		// MEASUREMENT/safety instrument: SelfReport ON assembles a self-report + emits introspect.faithfulness,
		// which would change the event stream + break byte-identical goldens. The faithfulness suite turns it on
		// explicitly. Default OFF ⇒ no report, no event ⇒ byte-identical (excluded from OffPaths via optInBoolKnob).
		Introspect: IntrospectCfg{SelfReport: false},
	}
}

// New returns a fresh AllOn() by value — the constructor name the engine calls.
func New() *HarnessConfig {
	c := AllOn()
	return &c
}

// WMax is the durability fan-out ceiling the stability suite asserts (regulator MAX_PAR_WIDTH default
// 8). Validate clamps Subconscious.MaxParWidth to it so a config flip cannot break the durable regime.
const WMax = 8

// Validate clamps tunables and enforces couplings, returning the list of human-readable warnings it
// raised (regime-affecting flips). It NEVER errors — a clamp is silent-but-recorded; the caller may
// surface the warnings. It is idempotent. Mirrors the §4.1 Validate() contract.
func (c *HarnessConfig) Validate() []string {
	var warns []string
	if c.Subconscious.MaxParWidth < 1 {
		c.Subconscious.MaxParWidth = 1
		warns = append(warns, "subconscious.max_par_width clamped up to 1 (must be >= 1)")
	}
	if c.Subconscious.MaxParWidth > WMax {
		warns = append(warns, "subconscious.max_par_width clamped to W_max="+itoa(WMax)+" (durability ceiling)")
		c.Subconscious.MaxParWidth = WMax
	}
	if !c.Regulator.Enforce {
		warns = append(warns,
			"regulator.enforce=off — the homeostatic regulator is bypassed; the stability suite will fail this regime")
	}
	if c.Persist.Backend == "" {
		c.Persist.Backend = "jsonl"
	}
	// AttentionWidth (W) is the faculty-scheduler width — clamp to [1, WMax] so a flip cannot widen the
	// hot faculty set beyond the regulator's durable fan-out ceiling. Clamped regardless of whether the
	// scheduler is on (a stored config is always in a sane range; the scheduler only consults it when on).
	if c.Conscious.Activity.AttentionWidth < 1 {
		c.Conscious.Activity.AttentionWidth = 1
		warns = append(warns, "conscious.activity.attention_width clamped up to 1 (must be >= 1)")
	}
	if c.Conscious.Activity.AttentionWidth > WMax {
		warns = append(warns, "conscious.activity.attention_width clamped to W_max="+itoa(WMax)+" (durability fan-out ceiling)")
		c.Conscious.Activity.AttentionWidth = WMax
	}
	// Ledger safety mode validation
	validModes := map[string]bool{"SAFE": true, "EXPAND": true, "REWRITE": true}
	if c.Ledger.SafetyMode == "" {
		c.Ledger.SafetyMode = "SAFE"
	} else if !validModes[c.Ledger.SafetyMode] {
		warns = append(warns, "ledger.safety_mode: unknown mode "+c.Ledger.SafetyMode+", defaulting to SAFE")
		c.Ledger.SafetyMode = "SAFE"
	}
	if c.Ledger.MaxEntries < 0 {
		c.Ledger.MaxEntries = 0
		warns = append(warns, "ledger.max_entries clamped to 0 (unlimited)")
	}
	// G5 strip-horizon: 0 means "the default" (untouched), so leave it; a non-zero value is clamped to
	// [MinStripHorizon, MaxStripHorizon] so a stored/env-set horizon can never grow the rendered monitor
	// stack without bound or collapse below one column of history. Pure View-layer; never regime-affecting.
	if c.Tui.StripHorizon != 0 {
		if c.Tui.StripHorizon < MinStripHorizon {
			warns = append(warns, "tui.strip_horizon clamped up to "+itoa(MinStripHorizon)+" (minimum strip depth)")
			c.Tui.StripHorizon = MinStripHorizon
		} else if c.Tui.StripHorizon > MaxStripHorizon {
			warns = append(warns, "tui.strip_horizon clamped to "+itoa(MaxStripHorizon)+" (render-cost ceiling)")
			c.Tui.StripHorizon = MaxStripHorizon
		}
	}
	return warns
}

// Load builds a HarnessConfig from the precedence chain (each over the previous, all over AllOn()):
//
//	defaults (AllOn)  <  config file  <  THOUGHT_CFG_* env  <  (CLI --disable/--enable applied by the caller)
//
// It starts from AllOn(), JSON-merges the file OVER it (so a file lists only the toggles it flips),
// then applies the THOUGHT_CFG_* env overrides (dotted-path -> upper-snake), then Validate()s. An
// empty path skips the file step (env-and-defaults only). A missing file at the DEFAULT path is not an
// error (the all-on baseline); a missing file at an EXPLICIT path IS an error. Emit may be nil.
//
// CLI --enable/--disable are applied by the caller AFTER Load (they are the highest non-TUI tier);
// the caller uses ApplyToggle for each. Load reports config.load with the OFF-toggle summary so a
// non-default run config is never silent.
func Load(path string, explicit bool, emit events.Emit) (*HarnessConfig, []string, error) {
	c := AllOn()
	var warns []string
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			// JSON-merge the file OVER the all-on baseline: decode INTO c, so any key the file omits
			// keeps its all-on value, and only the listed toggles are flipped.
			if err := json.Unmarshal(data, &c); err != nil {
				return nil, nil, err
			}
		case os.IsNotExist(err) && !explicit:
			// the default path is absent -> the all-on baseline (not an error).
		default:
			return nil, nil, err
		}
	}
	warns = append(warns, applyEnv(&c)...)
	warns = append(warns, c.Validate()...)
	if emit != nil {
		off := c.OffPaths()
		summary := "config loaded: all-on"
		if len(off) > 0 {
			summary = "config loaded: " + itoa(len(off)) + " toggle(s) OFF"
		}
		emit(events.ConfigLoad, summary, events.D{
			"off":   off,
			"count": len(off),
			"path":  path,
			"warns": warns,
		})
	}
	return &c, warns, nil
}

// applyEnv applies THOUGHT_CFG_<UPPER_SNAKE_DOTTED_PATH>=on|off|<int> overrides over the config,
// after the file. Returns warnings for unknown paths (ignored, not fatal). The mapping folds the
// scattered env surface into one place: e.g. THOUGHT_CFG_SEAM_HIDDEN_TRANSFORM=off.
func applyEnv(c *HarnessConfig) []string {
	var warns []string
	table := Knobs()
	for _, k := range table {
		envName := "THOUGHT_CFG_" + strings.ToUpper(strings.NewReplacer(".", "_").Replace(k.Path))
		val, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}
		if err := k.SetFromString(c, val); err != nil {
			warns = append(warns, envName+": "+err.Error())
		}
	}
	return warns
}

// EnableWebLaneBundle couples the opt-in WEB LANE: when web_search is enabled (the one network
// decision — live reads, cost, latency), the two VALIDATED web-lane improvements ride along —
// query_formulation (formulate the search query from the actual question, not the whole goal) and
// fetch_url (browse a result page, not just the snippet). This mirrors the realhard `--web` edge the
// +5pp HotpotQA-fullwiki measurement (2026-06-23) was taken on, so opting into web grounding gives the
// full measured lane in ONE switch. It is EDGE-ONLY (called from the CLI/TUI engine builders, like
// Persist.Resume) and deliberately NOT in AllOn(), so the measurement harnesses + engine-level tests
// stay "what you set is what you get". No-op when web_search is OFF — byte-identical to today.
func (c *HarnessConfig) EnableWebLaneBundle() {
	if c == nil || !c.Subconscious.WebSearch {
		return
	}
	c.Subconscious.QueryFormulation = true
	c.Subconscious.FetchURL = true
}

// OffPaths returns the dotted paths of every BOOL toggle currently OFF *relative to the all-on
// baseline*, ascending — the summary the Config tab + config.load report. Tunables (ints/strings) are
// not bool toggles, so they are not listed here (their non-default values surface in the panel
// directly). An OPT-IN knob (baseline OFF, e.g. seam.legible_generation) is NOT a deviation when it is
// off, so it is excluded here — keeping the all-on config.load summary (and goldens) byte-identical;
// an opt-in knob that has been turned ON is reported separately by the panel, not as an OFF path.
func (c *HarnessConfig) OffPaths() []string {
	var off []string
	for _, k := range Knobs() {
		if k.Kind != KnobBool || k.OptIn {
			continue
		}
		if v, ok := k.GetBool(c); ok && !v {
			off = append(off, k.Path)
		}
	}
	sort.Strings(off)
	return off
}

// Save writes the config to path as indented JSON (the full tree, not just the OFF toggles — a
// round-trippable snapshot). The directory must already exist. Mirrors the Config tab's `s` key.
func (c *HarnessConfig) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// itoa is a tiny stdlib-free int->string (the leaf avoids importing strconv just for summaries).
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
