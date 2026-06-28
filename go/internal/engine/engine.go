package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/calibrate"
	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/cogngraph"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/critic"
	"github.com/berttrycoding/thought-harness/internal/estimate"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/flywheel"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/grounding"
	hostpkg "github.com/berttrycoding/thought-harness/internal/host"
	"github.com/berttrycoding/thought-harness/internal/interaction"
	"github.com/berttrycoding/thought-harness/internal/keyframe"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/legible"
	"github.com/berttrycoding/thought-harness/internal/lifecycle"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/regulator"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/seams"
	"github.com/berttrycoding/thought-harness/internal/session"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/timeline"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/value"
	verifypkg "github.com/berttrycoding/thought-harness/internal/verify"
	webpkg "github.com/berttrycoding/thought-harness/internal/web"
)

// port is the inbound-channel facet the engine drives polymorphically — the Python `self.port`
// attribute, typed InteractionPort (reactive) but reassigned to a PerceptionPort for continuous
// mode. It carries only the COMMON methods (the ones both ports expose); the continuous-only
// Stream/Salient are reached by asserting the live port to *interaction.PerceptionPort (which it
// always is while awake — set in __init__/set_mode for continuous mode), faithful to Python where
// those calls only ever happen on a PerceptionPort.
type port interface {
	Receive(message string, source types.Source, salient bool)
	Pending() bool
	Pop() (string, bool)
	Deliver(filt interaction.Admitter, history []types.Thought, value float64) *types.Thought
	PendingMessages() []interaction.QueuedMessage
	RestoreMessages(msgs []interaction.QueuedMessage)
}

// Engine is the orchestrator. It owns the bus, the seeded RNG, the resolved backend, and every
// collaborator; it drives one tick per Step. Mirrors the Python Engine class field-for-field.
type Engine struct {
	cfg          EngineConfig
	bus          *events.Bus
	rng          *cpyrand.Random
	wanderRNG    *cpyrand.Random // dedicated, deterministically-seeded RNG for the TEST DOUBLE's awake-content rotation (see wander)
	rngStreams   []rngStream     // the enumerable resume registry: every advancing RNG stream, snapshottable by name (resume.go)
	stopReq      atomic.Bool     // cooperative shutdown: Run breaks between ticks (RequestStop) so the edge flushes before exit (shutdown.go)
	backend      backends.Backend
	backendLabel string
	scheduler    *scheduler.LLMScheduler
	mode         string

	// features is the SHARED system-wide HarnessConfig (the representation-space rebuild, M1). Every
	// component reads its toggle through this pointer, so a live flip (TUI / mid-run) is seen with no
	// rebuild. Never nil after NewEngine (Features=nil ⇒ config.New()/AllOn). gates holds the per-
	// component config.Gate handles the engine consults at each call site (toggle = bypass, not delete).
	features        *config.HarnessConfig
	gates           engineGates
	configAnnounced bool // config.load emitted yet? (deferred to the first Step so sinks are attached)

	// persistent-across-episodes catalogs (progressive specialisation)
	catalog *cognition.OperatorRegistry
	skills  *cognition.SkillRegistry
	goals   []cognition.Goal

	subconscious *subconscious.SubconsciousEngine

	// hidden seam
	filter *seams.Filter
	gate   *seams.Gate
	hidden *seams.HiddenSeam

	// legible-generation SHADOW instrument (WF-E CC-1): the contract registry (one source of truth for
	// the Generate prompt fragment + the seam's tag parser) + the shadow it drives. DEFAULTS OFF via
	// seam.legible_generation: when off, the Generate prompt is unchanged, no tag is stripped, and no
	// legible.* event fires (byte-identical goldens). Rebuilt from e.catalog at NewEngine.
	legibleReg    *legible.TagRegistry
	legibleShadow *legible.Shadow

	// watched seam / ACTION
	executor *action.ToolExecutor // nil when no workspace (offline path)
	front    *seams.FrontActuator
	watched  *seams.WatchedSeam
	awatched *seams.AsyncWatchedSeam

	// reality-grounding spine (SR-4): every watched observation is fed in; a real observation
	// grounds/refutes the claim it bears on, a fabricated tier-0 one is rejected. Continuous-mode
	// sensors re-ground standing claims with no ACT. Made observable on grounding.*.
	grounding         *grounding.ExperimentMemory
	sensors           []grounding.Sensor // standing percept sources polled on the awake tick (continuous)
	episodeGroundBase int                // grounding-ledger length at episode start (per-episode grounded?)
	episodeActsIssued int                // ACTs that crossed the watched seam this episode (FIX 2: force-a-read-before-give-up)
	lastBridge        string             // how the last observation reached reality (N.4: structured|scraped|none)
	lastFabricated    bool               // was the last observation a tier-0 fabrication (P0.6)? (offline stand-in)
	activePath        string             // the seed PATH skill (analogy/induction/deduction) driving this episode, "" if none (M5)

	// SLAM self-state estimator (Track F / M1): the explicit innovation/residual on the action->reality
	// path — the scalar Kalman measurement update with the FEJ-anchored trust rule. INERT (Enabled()==
	// false) unless the opt-in slam.innovation knob is ON; when inert the engine's groundObservation
	// path calls nothing on it, so the live loop is byte-identical. Made observable on estimate.*.
	estimator *estimate.Estimator

	// SLAM calibration meta-estimator (Track F / M9): LEARNS each source's reliability per trust tier
	// from the estimator's predicted-vs-actual residual stream and RE-ESTIMATES the precision R the
	// estimator's next Observe() uses — the lever on the measured same-model ceiling. INERT unless BOTH
	// slam.innovation AND slam.calibration are ON; when inert the engine feeds the estimator the fixed
	// TierPrecision prior exactly (no re-weighting, no estimate.calibrate event), so the loop is
	// byte-identical. Made observable on estimate.calibrate.
	calibrator *calibrate.Calibrator

	// the runtime: a bounded Session spawn tree mirroring the current episode's synthesised workflow
	// (P3.3). nil when no multi-phase program was synthesised (simple Q&A). Made observable on session.*.
	sessionRoot *session.Session

	// WF-G time-awareness (09 §4): the INJECTED wall-clock seam + the per-episode deadline. A nil clk
	// (the default) means TIME-BLIND — no time is ever read, behavior byte-identical to the tick-only
	// engine (the durability math and goldens never see a wall clock). Wired only via SetClock (the
	// edge constructs clock.Wall; tests a clock.Fake). episodeStart is stamped in startEpisode when a
	// clock is wired; a zero deadline disables the check even with a clock present.
	clk             clockpkg.Clock
	episodeDeadline time.Duration
	episodeStart    time.Time
	deadlineFired   bool // lifecycle.deadline emitted this episode (once-only; reset at startEpisode)
	introspectedEp  bool // introspect.suite emitted this episode's quiescence (once-only; reset at startEpisode)

	// reach=self INTROSPECTION sensors (cognitive power-cycle, Track 3 — host.go tap + introspect.go):
	// the two missing reach=self reads the orientation pass folds in. hst is the INJECTED host/runtime
	// seam (read_host = "the machine I live on / my footprint"): a nil hst (the default) is HOST-BLIND —
	// no runtime stat is ever read, byte-identical to the footprint-blind engine. Wired only via SetHost
	// (the edge constructs host.Wall; tests a host.Fake). eventRing is the bounded in-memory tap of the
	// engine's OWN event bus (read_event_log = "my own logs/traces" — the missing INBOUND introspection
	// path; events are outbound-only otherwise): a passive, fixed-cap subscriber wired ONLY when the
	// sense.event_log knob is on (eventTapUnsub holds its unsubscribe). nil ring / nil host on the
	// default engine ⇒ no read ⇒ byte-identical.
	hst           hostpkg.Host
	eventRing     *introspectRing
	eventTapUnsub func()

	// reach=world OUTWARD-perception sensor (cognitive power-cycle, follow-up #15 — web_sense.go): web is
	// the INJECTED outward web/news seam (fetch_web = "what is happening in the world right now"): a nil
	// web (the default) is WEB-BLIND — no network read is ever performed, byte-identical to the web-blind
	// engine. Wired only via SetWeb (the edge constructs web.Wall; tests a web.Fake). Like the clock, a real
	// fetch is non-deterministic, so the SNIPPET rides the percept-log (record once / replay thereafter).
	// BUDGETED per resolved Fork 2 (sensing autonomy OFF/budgeted): webSensedEpisode guards the fetch to AT
	// MOST ONCE per episode-open (not per tick) — a bounded distal sense. nil web on the default engine ⇒ no
	// fetch ⇒ byte-identical.
	web              webpkg.Web
	webSensedEpisode bool

	// pageFetcher is the INJECTED outward page-FETCH seam for the model-callable fetch_url tool
	// (subconscious.fetch_url, T1.4) — the sibling of web (the search/sense seam): where web SEARCHES,
	// pageFetcher FETCHES one specific URL's readable text. A nil pageFetcher (the default) is PAGE-BLIND:
	// no page fetch is ever performed, byte-identical to the page-blind engine. Wired only via
	// SetPageFetcher (the edge constructs web.Pager; tests a web.FakePager). It carries no net/http itself
	// (the only network read lives in the injected web.Pager, constructed at the edge), so the engine stays
	// headless-pure. The fetch_url tool reads it LAZILY (lazyPager reads e.pageFetcher at Execute time) so a
	// SetPageFetcher AFTER NewEngine is honoured, exactly like web / lazyWeb.
	pageFetcher webpkg.PageFetcher

	// L0 conformance WIRING-COVERAGE tap (Track H — conformance.go): when the opt-in conformance.self_check
	// knob is on, the engine attaches a PASSIVE subscriber to its OWN bus that records the SET of subsystem
	// LAYERS the live loop emitted this run (the "tests pass != feature runs" gate, made observable). The
	// rollup (internal/conformance) calls EmitWiringScan to render the covered/missing sets as one
	// conformance.wiring event. confTap is the recorder; confTapUnsub holds its unsubscribe. nil tap on the
	// default engine ⇒ no subscriber, no event ⇒ byte-identical.
	confTap      *conformanceTap
	confTapUnsub func()

	// Deterministic PERCEPT-LOG (cognitive power-cycle, Track 1.5 — percept.go): the once-recorded,
	// replayable boundary sensed values (read_clock now; world/host later). perceptLog accumulates this
	// run's records (RECORD mode); perceptReplay is the by-(tick,kind) lookup loaded from a
	// version/substrate-MATCHING prior log (REPLAY mode). perceptReplayOK gates replay — a divergent log
	// (version/substrate mismatch) sets it false so the engine COLD-SENSES instead of best-effort replay
	// (the divergence contract). All nil/false on the default engine ⇒ no sensing ⇒ byte-identical.
	perceptLog      []persist.PerceptEntry
	perceptReplay   map[string]string // key = perceptKey(tick, kind) -> logged value
	perceptReplayOK bool

	// declarative memory (P2.3/P6.x): episodic (past episodes) + semantic (bi-temporal beliefs) + person
	// (learned preferences). Recorded at episode-end (grounded-only), recalled at episode-start, distilled
	// on the idle tick. Made observable on memory.*.
	episodic *memory.EpisodicRegistry
	semantic *memory.SemanticRegistry
	person   *memory.PersonRegistry

	// timeline is the episodic attention trajectory (slice i, 02 §2a): append-only, fed at the
	// attention-move sites, DISTINCT from the Episodic memory registry above (current time-order vs
	// retained experience). Observability-only today; the Controller/retracement read it once wired.
	timeline *timeline.Timeline

	// branchGoals is the per-branch goal binding (G5, §1.8 / slice a.5b): the forest-aware rerank uses it
	// when conscious.activity.forest is on. nil/empty ⇒ every branch uses the single graph goal.
	branchGoals map[int]branchGoal

	// branchFaculty maps a standing seed-intent root branch to the FACULTY it keeps alive (recorded when
	// seedForestIntents forks the root). The faculty attention scheduler (conscious.activity
	// .faculty_scheduler) reads it to arbitrate focus fair-share across faculties. nil/empty ⇒ no seeded
	// roots tagged ⇒ the scheduler is a no-op (only the seed roots carry a faculty tag).
	branchFaculty map[int]cognition.SeedFaculty

	// facultyLastFocus records, per FACULTY name, the last tick its line held focus (the scheduler's
	// least-recently-focused fair-share key). A faculty never yet focused is treated as least-recent
	// (tick -1). Only written/read when conscious.activity.faculty_scheduler is on; deterministic (tick,
	// no clock). nil ⇒ no scheduler history yet.
	facultyLastFocus map[cognition.SeedFaculty]int

	// pendingInj is the hidden-seam pending-injection buffer (slice c, §2b / 04 §3.4): late injections
	// anchored to a decision node + tick. Drained each tick when conscious.activity.retracement is on —
	// the Controller fires mcp.Reenter on a passed-decision anchor. Empty ⇒ the drain is a no-op.
	pendingInj *seams.PendingInjectionBuffer

	// bandStreams is the per-signal intake band-pass state (slice c, 04 §2.1): keyed by candidate
	// domain/source, each a stateful LPF·HPF filter over ticks. Used only when seam.band_pass is on.
	bandStreams map[string]*bandStream

	// driveAgendaSeq rotates the process drive when minting awake DRIVE goals (slice k, §7.2). Used only
	// when conscious.activity.drive_agenda is on; deterministic (no clock).
	driveAgendaSeq int

	// seedIntentsDone guards the one-time seeding of the standing forest-root portfolio (C1, §1.8): the
	// seed-intent roots are planted into the forest at the FIRST awake tick (after the boot episode opens),
	// once per engine. Only consulted in the awake loop when conscious.activity.seed_intents is on; false
	// otherwise (so the bare/reactive path is byte-identical — it never reads this field).
	seedIntentsDone bool

	// oriented gates the ORIENTATION PASS (cognitive power-cycle, Track 3 — orient.go) to fire ONCE per
	// engine, at the FIRST wake. Only consulted when sense.orient is on; false otherwise (the default /
	// bare path never reads it ⇒ byte-identical). Set true the first time the orientation pass runs (or is
	// skipped because the resume-boot precondition is unmet), so it is attempted at most once.
	oriented bool

	// selfReportedEpisode bounds the INTROSPECTIVE-FAITHFULNESS self-report (Track H §8 —
	// introspect_faithfulness.go) to AT MOST ONCE per quiescence: the engine emits one introspect.faithfulness
	// witness when it reaches quiescence (reactive IDLE / awake ASLEEP), then sets this so a sustained idle/
	// asleep run cannot re-emit it every tick (a passive read each tick would spam the bus). startEpisode
	// clears it so each new episode gets one fresh self-report at its quiescence. Only consulted when
	// introspect.self_report is ON; the default/bare path never reads it ⇒ byte-identical.
	selfReportedEpisode bool

	// autoSenseGuard bounds the AUTONOMOUS sense (#19, autonomous_sense.go) to at most ONE read per focus:
	// it records the (branch, tick) of the last autonomous sense so a re-focus on the same branch within the
	// same tick cannot fire a second read (no fan-out, no standing excitation source that pushes n→1). Only
	// written/read in the awake loop when conscious.activity.autonomous_sense is on; the zero value (branch
	// -1) means "never sensed", so the default/reactive path never consults it ⇒ byte-identical.
	autoSenseBranch int
	autoSenseTick   int

	// selfModelHash is the content-HASH of the last STANDING-CORE self-model the engine grounded (SELF-MODEL,
	// self_model.go). The core is STANDING, not resume-once: it re-fires ONLY when this hash changes (a
	// specialist/tool/operator minted, mode/substrate changed) — so an unchanged self-model is grounded once,
	// not re-injected every introspective focus turn (which would spam the stream). Only written/read in the
	// awake loop when sense.self_model is on; the zero value "" means "never grounded", so the default/reactive
	// path never consults it ⇒ byte-identical.
	selfModelHash string

	// episodeContext is the Context captured by the producing Capability (slice b, §3.3) — the richer
	// snapshot (L1 gist + knowledge refs) that replaces the raw thought slice. Set per episode only when
	// subconscious.capability is on; nil otherwise (the inline path uses the thought slice directly).
	episodeContext *subconscious.Context

	// episodeCap is the Capability that PRODUCED this episode's workflow (slice b). It is retained past
	// startEpisode for the GAP 5-DEEPER relevance/dispatch ENTRY (subconscious.capability_dispatch): the
	// entry that produced the workflow is the entry the subconscious dispatch loop routes its per-tick
	// recognition through (SetRecognizer), so the Capability — not the self-triggering Workflow — owns the
	// live relevance entry. Set per episode only when subconscious.capability is on; nil otherwise (the
	// inline path self-triggers the workflow, byte-identical). Cleared when no workflow is produced.
	episodeCap *subconscious.Capability

	// priorContext is the COMPRESSED graph spine rehydrated from a prior power-cycle (cognitive
	// power-cycle, Track 2 — graph_spine.go): a subconscious.Context carrying ONLY Goal + L1 (the lossy
	// gist + the branch's thought IDs + resolution), the light-re-orientation material for "where I was"
	// (§4 + §9). POPULATED on boot only when the resume knob is ON, a non-nil Store backs it, persistence
	// is on, AND the persisted spine's version/substrate match (the divergence contract). NOTHING consumes
	// it yet — Track 3's orientation pass will read it (PriorContext accessor). nil on the default engine
	// ⇒ no consumer, no live-behaviour effect ⇒ byte-identical.
	priorContext *subconscious.Context

	// durable DOMAIN knowledge (the representation-space rebuild, M3): facts/patterns/snippets true
	// independent of this system's history (a sibling to memory, not a part of it). Sourced as rung 2 of
	// the sourcing ladder, written back from reality, distilled on the idle tick. Made observable on
	// knowledge.*. Never-fabricate (only grounded items recorded).
	knowledge *knowledge.KnowledgeRegistry
	// sourcing is the ordered fuel ladder (M3 §3.2): present→knowledge→memory→reality→generated. Any
	// fuel-needing move (a GROUND/REFRAME candidate) consults it; concretize fuses the result before the
	// hidden seam. Wired with ports to knowledge/memory/the watched seam/the backend.
	sourcing *subconscious.SourcingPolicy

	// the CRAG-style sufficiency gate (A-RAG1) — a Pattern-C escalation over the SOURCED fuel inside the
	// concretize stage that drives the harness to ABSTAIN on an insufficient recall (drop the candidate)
	// rather than over-commit a hollow one. OPT-IN behind seam.sufficiency_gate (default OFF ⇒ no-op ⇒
	// byte-identical). Built once in NewEngine over the cognition mode + backend + the gate.
	suffGate *subconscious.SufficiencyGate

	// the shared hybrid-retrieval primitive (P1.x): a reachable embedder makes recall/skill-match
	// SEMANTIC (cosine), else lexical-only (Jaccard). Probed once for a real backend; nil for the
	// heuristic test double (no network probe → deterministic tests). retrieverMode names the mode.
	embedder      retrieval.Embedder
	retrieverMode string
	// semanticAnnounce is the retrieval.semantic payload captured at NewEngine when subconscious.semantic_recall
	// is ON (A-RAG2), emitted DEFERRED on the first Step so the CLI/TUI sinks (subscribed after NewEngine)
	// actually receive it — the same deferral the config.load/persist.load announces use. nil ⇒ the knob is
	// OFF (silent, byte-identical) — no announce ever fires.
	semanticAnnounce    events.D
	semanticAnnounceMsg string

	// critic executive + organs
	controller *critic.Controller
	value      *value.ValueSignal
	convert    *convert.Convertibility
	// synthCostTap sums the synthesize_program completion tokens seen on the bus since the last reset; it
	// feeds convert.NoteSynthesisCost so the W5 cost-aware mint gate knows what a shape cost to re-derive.
	// nil ⇒ the cost gate is OFF ⇒ no bus subscriber ⇒ byte-identical (the OFF path adds NOTHING to the
	// hot loop). The counter is atomic because the bus fans out synchronously but a parallel workflow phase
	// can emit llm.call from a worker goroutine.
	synthCostTap *atomic.Int64
	lifecycle    *lifecycle.Lifecycle
	regulator    *regulator.Regulator

	// cross-session persistence + the lifecycle/cleanup curator (the representation-space rebuild, M4).
	// The Store is injected (cfg.Store; nil ⇒ in-memory only — tests/heuristic never touch disk); the
	// curator is constructed lazily on the first IDLE consolidation. Both are no-ops when persistence is
	// disabled or the store is nil, so the bare path is byte-identical to pre-M4.
	curator     *persist.Curator
	loadSummary events.D // the persist.load payload captured at NewEngine (emitted deferred on the first Step; nil ⇒ nothing loaded)

	// keyframes is the loop-closure / recurrence keyframe DB (Track F, F-M7 — "the HINGE"): a persistent,
	// bi-temporal, substrate-tagged recurrence index over the active thought-line. Per live tick the engine
	// fingerprints the tip and Observes it; a re-entry fires keyframe.close (anti-rumination), and the
	// loop-back point may lie in a PRIOR run (cross-session loop closure, gap G3). Built + seeded from the
	// store in loadState ONLY when persistence.keyframe_db is ON AND a Store is present; nil otherwise ⇒
	// no observe, no event, nothing persisted ⇒ byte-identical to the recurrence-blind engine.
	keyframes *keyframe.DB

	// Self-change ledger (W1): the running count of minted self-changes last recorded to the ledger,
	// so a consolidation only writes a LedgerEntry when the engine actually changed itself (a mint
	// GREW the total) — not every idle consolidation. ledgerBaselined gates the one-time session
	// auto-baseline snapshot (the pre-mint revert point).
	ledgerRecorded  int
	ledgerBaselined bool

	// Self-benchmark loop (Track H, SB0 — selfbench.go): the checkpoint tag last self-benchmarked, so
	// maybeSelfBench runs AT MOST ONCE per distinct consolidation checkpoint (it would otherwise re-bench
	// the same frozen state every idle tick). "" until the first self-bench. Only used when
	// selfbench.enabled is ON (default OFF ⇒ this field never advances ⇒ byte-identical).
	selfBenchedAt string

	// continuous-mode additions
	drives      *cognition.Drives
	defaultMode *cognition.DefaultMode
	arousal     types.Arousal
	port        port

	// the unified cross-layer model (assembled live from the bus)
	cognitionGraph *cogngraph.CognitionGraph

	focusBound int // awake-mode active-line length before the mind moves on (Python _FOCUS_BOUND=9)
	lull       int // consecutive disengaged ticks (drives arousal toward sleep)

	graph *graph.ThoughtGraph
	mcp   *graph.ThoughtMCP

	processSeq   int    // cognitive-process (episode) counter -> the process id
	processID    string // id of the process currently being thought
	lastResponse string // the most recent answer delivered to the user
	graphFactSeq int    // A-RAG3: monotonic counter for written-back reality fact node ids (fact:{process}:{seq})

	transcript []transcriptTurn // conversation memory: (role, text) across turns

	branchVisits            map[int]int      // T1.3: per-branch FOCUS/resume visit counts for the UCB exploration policy (env THOUGHT_UCB_C); maintained always, only READ when ucbC>0 ⇒ byte-identical when off; episode-scoped (reset per episode)
	actedBranches           map[int]struct{} // branches that have already opened to reality
	forked                  map[int]struct{} // branches whose conflict has already been forked
	verifyBranched          map[int]struct{} // branches that already spawned a verify fork
	resourcedBranches       map[int]struct{} // A-RAG4: branches that have already actively re-sourced (the once-per-branch bound)
	verifiedAnswerBranches  map[int]struct{} // T2.1: branches whose committed answer has already been independently verified (the once-per-branch bound — bounded, no loop)
	awakeDispatchedBranches map[int]struct{} // AWAKE-DISP: awake user lines whose goal has already synthesised a workflow (the once-per-branch guard; awake-only, default-OFF)

	// answerVerifier is the T2.1 INDEPENDENT answer-verifier (internal/verify) — the flagship Tier-2
	// capability. It is consulted ONLY when the opt-in critic.answer_verify knob is ON (e.gates.answerVerify),
	// at the answer-commit decision (verifyAnswerDecision), to check a committed factual answer against an
	// INDEPENDENT signal (re-retrieved web evidence — never a same-model re-read of its own chain). It is
	// REBUILT lazily (rebuildAnswerVerifier) so a SetWeb AFTER NewEngine (the SetWeb-before-Run contract) is
	// honoured. nil ⇒ web-blind verifier ⇒ every answer is Unverifiable ⇒ the gate no-ops, byte-identical.
	answerVerifier verifypkg.Verifier

	lastOutreach int                 // tick of the last proactive outreach (cooldown gate; Python -999)
	sharedKeys   map[string]struct{} // endogenous lines already shared (no re-sharing)

	// O-5 async inbox push channel: when conscious.activity.inbox_escalation is ON, the last proactive
	// outreach becomes a PENDING inbox item; an unacknowledged item is re-surfaced with escalating urgency
	// (conscious.inbox_escalate), durability-bounded by InboxMaxEscalations + a strictly-longer cooldown,
	// and CLEARED the moment the user responds. Nil/empty pending ⇒ nothing outstanding (the OFF default).
	pendingInbox *inboxItem // the outstanding unacknowledged outreach (nil ⇒ none)

	// flywheel is the OFFLINE-RL DATA FLYWHEEL recorder (Track C, RL roadmap §6 P0 — flywheel.go): it
	// buffers the current episode's per-decision (state, action) tuples and backfills the terminal grounded
	// Outcome at episode close, flushing each finalised tuple to flywheelSink + emitting flywheel.capture.
	// Built ONLY when the opt-in flywheel.capture knob is ON; nil otherwise ⇒ every RecordDecision/
	// OpenEpisode/CloseEpisode call is a no-op (the Recorder methods are nil-safe) ⇒ byte-identical. The
	// sink is injected (an in-memory sink in tests; the JSONL sidecar at the CLI edge); a nil sink at build
	// time ⇒ an internal memory sink so the recorder still buffers+emits (the event fires) without a file.
	flywheel     *flywheel.Recorder
	flywheelSink flywheel.Sink
}

// inboxItem is one outstanding proactive-outreach push awaiting user acknowledgement (O-5). It is the
// harness-runtime form of LATHE's locked inbox.jsonl entry: the message text + when it was first pushed
// + how many times it has been re-surfaced. Escalation count starts at 0 (the first push is the base
// outreach itself); each re-surface increments it, capped at InboxMaxEscalations.
type inboxItem struct {
	text       string  // the developed-line insight (already Filter-passed, re-voiced)
	value      float64 // the line's value at first push (V(s) — the share-worthiness it cleared)
	firstTick  int     // the tick the base outreach first pushed it
	lastTick   int     // the tick of the most-recent push (base or escalation) — the cooldown anchor
	escalation int     // how many times it has been RE-surfaced (0 ⇒ only the base push has happened)
}

// transcriptTurn is one (role, text) conversation-memory entry (Python's (role, text) tuple).
type transcriptTurn struct {
	Role string // "user" | "assistant"
	Text string
}

// NewEngine wires every collaborator and resolves the thinking substrate, returning a ready Engine.
// An explicit backend (tests, the doctor) wins; otherwise the substrate is resolved from config — a
// REAL model by default, the test double ONLY when the substrate is explicitly "test". It RETURNS the
// ResolveSubstrate error (no silent offline fallback). Pass cfg=nil for the
// Python defaults; pass backend=nil to resolve from config. Mirrors Python Engine.__init__.
func NewEngine(cfg *EngineConfig, backend backends.Backend) (*Engine, error) {
	c := DefaultConfig()
	if cfg != nil {
		c = *cfg
	}
	e := &Engine{cfg: c}
	e.bus = events.NewDefault()
	e.rng = cpyrand.New(uint64(c.Seed))
	// wanderRNG is a SEPARATE deterministically-seeded stream (offset from the main seed) used ONLY by
	// the TEST DOUBLE's Wander to rotate its offline content pool — diverse + reproducible. It is kept
	// OFF the main e.rng stream on purpose: in PRODUCTION the model authors Wander and consumes NO
	// engine RNG, so threading e.rng into the test double's rotation would perturb the awake loop's
	// control trajectory (the Controller's value-driven moves) in a way the real substrate never would.
	// A dedicated wander RNG keeps the offline awake trajectory faithful to the production loop while
	// still satisfying "deterministic + varied" (the diversity property + the goldens).
	e.wanderRNG = cpyrand.New(uint64(c.Seed) ^ 0x5741_4e44 /* "WAND" */)
	// Resume registry (deterministic power-cycle, resume.go): register every advancing engine RNG
	// stream so a snapshot captures the FULL cursor — an unregistered stream silently escapes the
	// snapshot and breaks resume. The route RNG registers itself in wireTierRouter (it exists only
	// when the tier router is on). The soft policy + subconscious SHARE e.rng (same pointer), so one
	// registration covers every consumer of that stream.
	e.registerRNG("main", e.rng)
	e.registerRNG("wander", e.wanderRNG)

	// Resolve the system-wide feature config (the representation-space rebuild, M1). Features=nil ⇒
	// config.New()/AllOn (every component ON, byte-identical to the pre-config behaviour). Validate
	// clamps coupled tunables (MaxParWidth ≤ W_max) so a config flip cannot break the durable regime.
	// The gates close over this SHARED pointer, so a live TUI flip is observed with no rebuild.
	if c.Features != nil {
		e.features = c.Features
	} else if c.Mode == "continuous" {
		// B4 GO-LIVE (user-gated, β=0.5, 2026-06-21): a bare continuous/AWAKE-mode engine (no explicit
		// --profile / --config / Features) runs the VALIDATED awake default config — the living mind is
		// ON by default in awake mode (the standing forest of intents, soft policy, drive agenda,
		// proactive outreach, β=0.5, safety-gated). The reactive default is UNTOUCHED (the branch below).
		// An explicit Features (a profile/config pick, a measurement harness) wins and skips this — only
		// the bare default flips. Single source of truth: config.ApplyAwakeDefaults (the awake profile).
		f := config.New()
		config.ApplyAwakeDefaults(f)
		e.features = f
	} else {
		e.features = config.New()
	}
	e.features.Validate()
	e.buildGates()

	// reach=self read_event_log tap (introspect.go): install the passive, bounded own-event ring IFF the
	// opt-in sense.event_log knob is on. Default OFF ⇒ no subscriber, no ring ⇒ byte-identical. Wired here
	// (after the bus + features resolve) so the tap captures every event from the engine's first emit.
	e.wireEventTap()

	// L0 conformance WIRING-COVERAGE tap (conformance.go): install the passive, bounded coverage subscriber
	// IFF the opt-in conformance.self_check knob is on. Default OFF ⇒ no subscriber ⇒ byte-identical. Wired
	// here (with wireEventTap) so it sees every layer the engine emits from its first tick.
	e.wireConformanceTap()

	// OFFLINE-RL DATA FLYWHEEL recorder (flywheel.go): build the per-decision training-tuple Recorder IFF
	// the opt-in flywheel.capture knob is ON. Default OFF ⇒ nil Recorder ⇒ every hook is a nil-safe no-op
	// ⇒ byte-identical. No bus subscriber (the recorder is driven directly from the decision/close sites).
	e.buildFlywheel()

	// The thinking substrate. An explicit backend wins; else resolve from config (a real model by
	// default, the test double only when substrate=="test"). RETURN the error — no offline path.
	if backend != nil {
		e.backend = backend
	} else {
		b, err := llm.ResolveSubstrate(c.Substrate, llm.SubstrateConfig{
			BaseURL:      c.LLMBaseURL,
			Model:        c.LLMModel,
			APIKey:       c.LLMAPIKey,
			UtilityModel: c.LLMUtilityModel,
			MaxTokens:    c.LLMMaxTokens, // 0 → env/default (4096); the reasoning knobs read env
			MaxCtxModel:  c.LLMMaxCtxModel,
			MaxCtxURL:    c.LLMMaxCtxURL,
			MaxCtxTokens: c.LLMMaxCtxTokens,
		})
		if err != nil {
			return nil, err
		}
		e.backend = b
	}

	// backend_label = getattr(backend, "display_name", "test") — the DisplayNamer optional iface.
	e.backendLabel = "test"
	if dn, ok := e.backend.(backends.DisplayNamer); ok {
		e.backendLabel = dn.DisplayName()
	}
	emit := e.bus.Emit
	// let an LLM backend log calls/fallbacks to the bus (EmitBinder: hasattr bind_emit).
	if eb, ok := e.backend.(backends.EmitBinder); ok {
		eb.BindEmit(emit)
	}
	// The LLM-call scheduler: orders WHICH work spends the scarce model (V(s)-keyed budget). Engages
	// only the LLM backend (SchedulerBinder: hasattr bind_scheduler).
	e.scheduler = scheduler.New(emit, nil)
	if sb, ok := e.backend.(backends.SchedulerBinder); ok {
		sb.BindScheduler(e.scheduler)
	}
	// COST-AWARE TIER ROUTER (subconscious.tier_router, internal/route — RouteLLM-class Pattern-C): when
	// the knob is ON AND the backend is a TIERED LLM backend (Primary+Utility — the claude bridge / a
	// local two-tier setup), install the router so each CONTENT call picks its substrate tier (the
	// deterministic per-role FLOOR + the learned CEILING). A single-tier or test backend has no second
	// tier, so wireTierRouter is a no-op there. Default OFF ⇒ the router is never installed ⇒ the
	// TieredBackend's per-role FLOOR is the silent decision, byte-identical to the pre-router dispatch.
	e.wireTierRouter()
	e.mode = c.Mode

	// WIRE the ShapeRecognizer: when the backend is the TestBackend, hand it the cognition/synth
	// deterministic toolmaker (RecognizeShapeDict) so its SynthesizeProgram no longer defers — the Go
	// break for Python's lazy `from .synth import recognize_shape` (the one Tier1->Tier4 inversion).
	//
	// WEB-SEARCH (subconscious.web_search ON): hand the web-AWARE recogniser (RecognizeShapeWebDict) instead,
	// so step 1 of Synthesize (the toolmaker) ALSO produces the lookup-research program that staffs expose-
	// affordances for a factual question (the under-staffing fix on the offline path). Default OFF ⇒
	// RecognizeShapeDict ⇒ byte-identical (no lookup shape ever produced).
	if hb, ok := e.backend.(*backends.TestBackend); ok {
		hb.ShapeRecognizer = cognition.RecognizeShapeDict
		if e.features != nil && e.features.Subconscious.WebSearch {
			hb.ShapeRecognizer = cognition.RecognizeShapeWebDict
		}
	}

	// The shared hybrid-retrieval embedder (P1.x): probe for a reachable embedding server ONLY for a real
	// backend — the test double stays offline/deterministic (no network probe), so recall +
	// skill-match are lexical-only there. A reachable embedder lifts both to semantic (cosine + RRF).
	e.embedder, e.retrieverMode = nil, "lexical"
	_, testBackend := e.backend.(*backends.TestBackend)
	switch {
	case e.features != nil && e.features.Subconscious.SemanticRecall:
		// A-RAG2 (subconscious.semantic_recall ON): the embeddings SIDECAR is an INTENTIONAL, OBSERVABLE
		// wiring. Honor an INJECTED embedder verbatim (the test seam — no network dial) else PROBE the
		// /v1/embeddings sidecar; either way CAPTURE the outcome and ANNOUNCE it (deferred to the first
		// Step, like config.load/persist.load, so the CLI/TUI sinks subscribed after NewEngine receive it)
		// so a silent lexical fallback is never mistaken for a lit dense channel. The CONTENT substrate
		// (claude) need not host embeddings — point THOUGHT_LLM_BASE_URL/THOUGHT_EMBED_MODEL at a sidecar.
		if c.Embedder != nil {
			e.embedder, e.retrieverMode = c.Embedder, "hybrid"
			e.semanticAnnounceMsg = "semantic recall lit up: hybrid (injected embedder)"
			e.semanticAnnounce = events.D{"mode": "hybrid", "source": "injected"}
		} else if testBackend {
			// the test double is offline-deterministic by contract: never dial a sidecar probe (that would
			// make the suite network-dependent + flaky). Announce the deterministic lexical fallback so the
			// wiring is still observable; a real semantic lift needs an injected embedder or a live sidecar.
			e.semanticAnnounceMsg = "semantic recall fell back to lexical: test double is offline (no sidecar probe)"
			e.semanticAnnounce = events.D{"mode": "lexical", "source": "test_double", "reason": "offline test double"}
		} else {
			emb, probe := retrieval.ProbeEmbedder()
			if emb != nil {
				e.embedder, e.retrieverMode = emb, "hybrid"
				e.semanticAnnounceMsg = "semantic recall lit up: hybrid (" + itoa(probe.Dims) + "-d " + probe.Model + ")"
				e.semanticAnnounce = events.D{"mode": "hybrid", "source": "sidecar", "dims": probe.Dims,
					"model": probe.Model, "base_url": probe.BaseURL}
			} else {
				e.semanticAnnounceMsg = "semantic recall fell back to lexical: " + probe.Reason
				e.semanticAnnounce = events.D{"mode": "lexical", "source": "sidecar", "reason": probe.Reason,
					"model": probe.Model, "base_url": probe.BaseURL}
			}
		}
	case c.Embedder != nil:
		// An injected embedder is honored even on the legacy path (a tool can hand the engine one), but it
		// stays SILENT (no announce) so the default-OFF wire vocabulary holds.
		if !testBackend {
			e.embedder, e.retrieverMode = c.Embedder, "hybrid"
		}
	default:
		// LEGACY (semantic_recall OFF): the incidental silent probe — a non-test backend lifts to hybrid
		// iff a server happens to answer, with NO announce event. Byte-identical to pre-A-RAG2.
		if !testBackend {
			if emb := retrieval.ReachableEmbedder(); emb != nil {
				e.embedder, e.retrieverMode = emb, "hybrid"
			}
		}
	}

	// The operator catalog + skill library are persistent across episodes (progressive specialisation).
	e.catalog = cognition.NewOperatorRegistry()
	// WEB-SEARCH wire (subconscious.web_search, default-OFF): grant the expose-affordances lookup operator
	// the web_search tool so a staffed sub-agent scoped to it dispatches a real web search for a lookup-
	// shaped goal (alongside its search/read_file local tools). Flag-gated + idempotent + local to THIS
	// engine's catalog (GrantToolScope copies the spec) ⇒ OFF leaves expose-affordances at {search,
	// read_file} ⇒ byte-identical. The tool itself is registered in buildExecutor (also flag-gated); the
	// floorToolCall web_search branch issues the call. Double-gated on a wired Web seam at dispatch time.
	if e.features != nil && e.features.Subconscious.WebSearch {
		e.catalog.GrantToolScope("expose-affordances", "web_search")
	}
	// FETCH-URL wire (subconscious.fetch_url, default-OFF, T1.4): grant the expose-affordances lookup
	// operator the fetch_url tool so a staffed sub-agent scoped to it fetches a specific result page (a URL
	// it found in a prior web_search observation, or one named in the goal) alongside its search/read_file
	// local tools. The browse loop (web_search -> see URL -> fetch_url -> think) is EMERGENT from the thought
	// graph — each fetch is one independent dispatch, NO hardcoded multi-step loop ⇒ the plant/fan-out is
	// unchanged. Flag-gated + idempotent + local to THIS engine's catalog (GrantToolScope copies the spec)
	// ⇒ OFF leaves expose-affordances at {search, read_file} ⇒ byte-identical. The tool itself is registered
	// in buildExecutor (also flag-gated); the floorToolCall fetch_url branch issues the call when the goal
	// carries a URL. Double-gated on a wired PageFetcher seam at dispatch time.
	if e.features != nil && e.features.Subconscious.FetchURL {
		e.catalog.GrantToolScope("expose-affordances", "fetch_url")
	}
	// EDIT-FILE wire (subconscious.edit_file, default-OFF, T1.2): grant the expose-affordances operator the
	// edit_file tool scope so a staffed sub-agent scoped to it can surgically edit a file it has already read
	// (the read -> see the exact text -> edit_file -> verify loop is EMERGENT from the thought graph, each
	// edit one independent model-authored dispatch — NO hardcoded multi-step loop ⇒ the plant/fan-out is
	// unchanged). edit_file is model-authored exactly like write_file (it carries old_string/new_string the
	// deterministic floor cannot supply, so the floor falls through to the model — subagent.go P3). Flag-gated
	// + idempotent + local to THIS engine's catalog (GrantToolScope copies the spec) ⇒ OFF leaves
	// expose-affordances at {search, read_file} ⇒ byte-identical. The tool itself is registered in
	// buildExecutor (also flag-gated). Granting edit_file lifts expose-affordances to a mutate scope
	// category (ScopeCategory short-circuits to "mutate"), matching write_file's authority.
	if e.features != nil && e.features.Subconscious.EditFile {
		e.catalog.GrantToolScope("expose-affordances", "edit_file")
	}
	// READ-DOCUMENT wire (subconscious.read_document, default-OFF, T2.3): grant the expose-affordances lookup
	// operator the read_document tool so a staffed sub-agent scoped to it can extract a document's text (a PDF
	// or office file named in the goal, or one it found on disk) alongside its read_file/search local tools.
	// read_document carries a path the deterministic floor cannot infer beyond a bare name, so it is a P3
	// model-authored call like read_file — each read one independent dispatch, NO hardcoded multi-step loop ⇒
	// the plant/fan-out is unchanged. Flag-gated + idempotent + local to THIS engine's catalog (GrantToolScope
	// copies the spec) ⇒ OFF leaves expose-affordances at {search, read_file} ⇒ byte-identical. The tool
	// itself is registered in buildExecutor (also flag-gated). read_document is a READ, so the scope stays an
	// inspect category (no mutate lift, unlike edit_file).
	if e.features != nil && e.features.Subconscious.ReadDocument {
		e.catalog.GrantToolScope("expose-affordances", "read_document")
	}
	e.catalog.SetEmbedder(e.embedder)           // W3-S3: semantic catalog Offer when an embedder is reachable
	e.skills = cognition.NewSkillRegistry(true) // seed the library
	e.skills.SetEmbedder(e.embedder)            // hybrid skill-match when an embedder is reachable (P1.3)
	if e.features != nil && e.features.Convert.SkillReframe {
		// GAP 8 (§3.8): flip the Skill reframe on the registry — the prompt + runtime-resolver shape, and
		// retire the skill's goal-self-match. Default OFF ⇒ this branch is dead and the legacy Program /
		// build-time-Expand / goal-matching path is byte-identical (the W5 mint/recall flywheel untouched).
		//
		// LEGACY(redesign): the convert.skill_reframe OFF-path keeps the registry in its legacy
		// Program-bodied / goal-self-match mode (SetReframe stays false) — the SkillRegistry.Match /
		// MatchWithinTier lexical goal-matchers stay LIVE — removable when the 4 redesign flags are retired
		// (the reframe is then unconditional; the legacy goal-self-matchers in skills.go retire with it).
		e.skills.SetReframe(true)
	}
	e.goals = nil

	// Legible-generation SHADOW instrument (WF-E CC-1): the contract registry is snapshotted from the
	// operator catalog (one source of truth: the Generate prompt fragment + the seam's tag parser are
	// both derived from it). The shadow it drives parses tags + emits parity/novel events. Both are
	// inert until seam.legible_generation is flipped ON (it DEFAULTS OFF) — until then the Generate
	// prompt is unchanged, no tag is stripped, and no legible.* event fires. (Rebuild the registry at a
	// consolidation tick once the lexicon grows — the versioned-contract discipline, 05 §4b — deferred.)
	e.legibleReg = legible.NewTagRegistry(e.catalog)
	e.legibleShadow = legible.NewShadow(e.legibleReg, emit)

	// Subconscious (persistent across episodes). The workflow is synthesised per episode (see startEpisode).
	// The REAL primitive roster (M2) is installed AFTER the memory registries + executor are wired
	// (below), because the tool-backed primitives (read/search/run) hold this engine as their
	// ExecutorProvider and the `recall` primitive reads the real memory store via the engine's
	// MemoryRecaller — both forward references resolved lazily at Fire time. Start with an empty roster.
	e.subconscious = subconscious.NewSubconsciousEngine(nil, e.rng, emit, nil, nil)

	// Hidden seam. The Filter is a Pattern-C hybrid (deterministic floor + optional model ceiling),
	// so it takes the cognition MODE + the backend (it escalates only when the backend satisfies
	// backends.FilterEscalator AND the case is flagged-fuzzy in llm/hybrid mode) — the SAME mode that
	// governs the Controller's escalation, so both hybrid escalators share one posture. The Gate is
	// pure Pattern A (control.Rank); it takes no backend.
	e.filter = seams.NewFilter(c.Cognition, e.backend, emit)
	e.gate = seams.NewGate(emit)
	e.hidden = seams.NewHiddenSeam(e.gate, e.filter, e.backend, emit)
	// CONFIG (M1): wire the three hidden-seam stage gates (Filter/Gate/Transform). Each nil-safe gate
	// reads the live shared config, so a toggle OFF short-circuits that stage to its pass-through and a
	// live flip is observed with no rebuild. The same *Filter is used by generate()/continuous, so its
	// gate applies there too.
	e.hidden.SetGates(e.gates.filter, e.gates.gate, e.gates.transform)
	// Legible-generation SHADOW (WF-E CC-1): wire the contract shadow + the seam.legible_generation
	// toggle into the seam and its inner Filter + Gate in one call. OFF (the default) ⇒ no tag parse, no
	// strip, no legible.* events ⇒ byte-identical. The SAME *Filter is used by generate()/continuous, so
	// the FILTER shadow covers the conscious's own tagged thought too.
	e.hidden.SetLegible(e.legibleShadow, e.gates.legible)

	// Watched seam / ACTION. A configured workspace dispatches REAL sandboxed tools; none -> offline.
	e.executor = e.buildExecutor(emit)
	// Hand the same Action-layer executor to the Subconscious so a workflow's effectful sub-agents
	// dispatch their scoped tools, not just the Action layer (Python self.subconscious.executor = ...).
	e.subconscious.SetExecutor(e.executor)
	// the parallel-phase pre-fire sizes its concurrent set by the scheduler's remaining background
	// budget (deterministic first-k allocation; 07 §A.1 task #9 part 2). Wired HERE — after the
	// subconscious engine exists (wiring it at scheduler.New, before construction, nil-dereferenced).
	e.subconscious.SetScheduler(e.scheduler)
	// SPARSE-DISPATCH (subconscious.dispatch.sparse): wire the sparsemax admission flag from the live config.
	// Default OFF ⇒ the legacy per-key absolute eff>theta admission ⇒ byte-identical, no subconscious.sparse
	// event. The e.dispatch() wrapper refreshes this each tick so a live TUI flip is honoured with no rebuild.
	if e.features != nil {
		e.subconscious.SetSparseDispatch(e.features.Subconscious.SparseDispatch)
	}
	// The FrontActuator takes a watched-seam ToolExecutor interface; nil executor stays a NIL
	// interface (not a non-nil interface wrapping a nil pointer) so Act's `Executor != nil` is faithful.
	if e.executor != nil {
		e.front = seams.NewFrontActuator(e.executor)
	} else {
		e.front = seams.NewFrontActuator(nil)
	}
	e.watched = seams.NewWatchedSeam(e.front, emit)
	e.awatched = seams.NewAsyncWatchedSeam(e.front, emit, 2) // Python latency=2

	// Reality-grounding spine (SR-4): the watched seam feeds every observation in (groundObservation),
	// continuous-mode sensors re-ground standing claims with no ACT. Empty until reality is opened to.
	e.grounding = grounding.NewExperimentMemory()

	// SLAM self-state estimator (Track F / M1): the explicit innovation/residual on the action->reality
	// path. Enabled() iff the opt-in slam.innovation knob is ON; otherwise INERT — groundObservation
	// short-circuits the Note/Observe calls so the live loop stays byte-identical. The estimator reads
	// the SHARED config Enabled at construction; a TUI live-flip rebuilds via this constructor.
	estCfg := estimate.DefaultConfig()
	estCfg.Enabled = e.features.Slam.Innovation
	// SLAM M5 (Track F): the consistency/observability monitor — the failable witness that the estimator
	// gains NO spurious information in unobservable directions (the awake-durability gate requirement). It
	// only accounts when innovation is also on (it monitors that update's variance trajectory). OFF (the
	// default) => no accounting, no estimate.consistency event => byte-identical.
	estCfg.Monitor = e.features.Slam.Innovation && e.features.Slam.Consistency
	// SLAM M2 (Track F): the sparse-covariance / Information layer — record which beliefs co-vary (share a
	// grounding upstream) and, on a grounded REFUTATION, inflate the co-varying siblings' variance (catch
	// correlated self-deception). It only correlates when innovation is also on (it correlates that update's
	// variance trajectory). OFF (the default) => no correlation graph, no estimate.correlate event =>
	// byte-identical.
	estCfg.Covariance = e.features.Slam.Innovation && e.features.Slam.Covariance
	// SLAM M6 (Track F): the active-inference info-gain layer — rank the live tracked beliefs by expected
	// JOINT information gain and surface the next-best-observation (what to verify next). It only ranks when
	// innovation is also on (it ranks that update's variance trajectory). OFF (the default) => no ranking,
	// no estimate.infogain event => byte-identical.
	estCfg.InfoGain = e.features.Slam.Innovation && e.features.Slam.InfoGain
	// SLAM M4 (Track F): the freshness / staleness-decay layer — each tick GROW every grounded belief's
	// variance back toward the prior ceiling as a function of its un-refreshed age (the dynamic-map process
	// noise Q>0, P4). It only decays when innovation is also on (it decays that update's variance
	// trajectory). OFF (the default) => no decay sweep, no estimate.decay event => byte-identical. The rate
	// Q rides the slam.staleness_q knob (a small slow-drift default).
	estCfg.Staleness = e.features.Slam.Innovation && e.features.Slam.Staleness
	estCfg.StalenessQ = e.features.Slam.StalenessQ
	e.estimator = estimate.New(estCfg, emit)

	// SLAM calibration meta-estimator (Track F / M9): learns R per source/tier from the residual stream.
	// Enabled() iff BOTH slam.innovation AND slam.calibration are ON (it consumes the M1 residual, so it
	// is meaningless without the innovation update producing one); otherwise INERT — slamObserve feeds the
	// estimator the fixed TierPrecision prior exactly, so the live loop stays byte-identical.
	calCfg := calibrate.DefaultConfig()
	calCfg.Enabled = e.features.Slam.Innovation && e.features.Slam.Calibration
	e.calibrator = calibrate.New(calCfg, emit)

	// Declarative memory (P2.3/P6.x): the hybrid retriever's embedder makes recall semantic when one is
	// reachable, else lexical. Recorded grounded-only at episode-end, recalled at episode-start.
	e.episodic = memory.NewEpisodicRegistry(e.embedder)
	e.semantic = memory.NewSemanticRegistry(e.embedder)
	e.person = memory.NewPersonRegistry(2) // a default learned after 2 consistent overrides

	// Durable DOMAIN knowledge (M3 §3.1): the third-person registry beside memory, sharing the retrieval
	// precision floor + the never-fabricate gate. Starts empty (empty-and-earn-it is the default grounding
	// story; the reality write-back + idle distillation grow it). The embedder lifts recall to semantic.
	e.knowledge = knowledge.NewKnowledgeRegistry(e.embedder, emit)

	// Install the REAL primitive roster (M2) now that the memory store + executor are wired. The `recall`
	// primitive reads the real Semantic/EpisodicRegistry through this engine's MemoryRecaller (the worst-
	// gap fix — accumulated grounded knowledge is finally reachable); read/search/run hold the
	// SubconsciousEngine as their ExecutorProvider (the executor was SetExecutor'd above); skeptic/
	// advocate fire only when the backend is a SpecialistCaller model (a real reason, never a canned
	// opinion). The deleted simulation/safety/refactor fakes + the toy MemoryKB are gone.
	caller, _ := e.backend.(backends.SpecialistCaller)
	// comprehender is the LLM "to_operator" port for the read/search/run senses (Pattern-C ceiling over the
	// keyword-trigger + regex floor): a real model fires read/search/run on the EXPRESSED need AND on the
	// target it INTENDS (incl. a self-corrected path), in ONE Comprehend call. A TestBackend is NOT a
	// RealityComprehender, so this is nil and the floor stands — goldens unchanged.
	comprehender, _ := e.backend.(backends.RealityComprehender)
	// solverFormalizer is the Pattern-B shape-writer port for the OPT-IN 5th-axis classical solver
	// specialist (subconscious.solver_specialist, default OFF). A TestBackend is NOT a StructureFormalizer,
	// so this is nil and the specialist (when the knob is on) stays dark — goldens unchanged. The specialist
	// is only APPENDED to the roster when the knob is on; when off the roster is byte-identical.
	solverFormalizer, _ := e.backend.(backends.StructureFormalizer)
	e.subconscious.SetPrimitiveSubAgents(subconscious.DefaultPrimitiveSubAgents(
		e, e.subconscious, caller, emit, comprehender, solverFormalizer, e.features.Subconscious.SolverPrimitiveSubAgent))

	// The sourcing ladder (M3 §3.2): present→knowledge→memory→reality→generated, with ports to the live
	// knowledge registry (rung 2), this engine's MemoryRecaller (rung 3), the watched-seam reality port
	// (rung 4, gated/observed/Fabricated-aware), and the backend generator (rung 5, the low-trust floor).
	// The §4.2 Source toggles + the subconscious.sourcing gate gate the walk. Concretize consults it.
	e.sourcing = subconscious.NewSourcingPolicy(
		e.knowledge, e, &realitySourcer{e}, &fuelGenerator{e},
		&e.features.Repr.Sources, e.gates.sourcing, emit)

	// The CRAG-style sufficiency gate (A-RAG1) — a Pattern-C escalation over the SOURCED fuel inside
	// concretize. It takes the cognition MODE + the backend (it escalates only when the backend satisfies
	// backends.SufficiencyJudge AND the case is flagged-fuzzy in llm/hybrid mode) — the SAME mode that
	// governs the Filter/Controller, so all the hybrid escalators share one posture. The seam.sufficiency_
	// gate toggle (opt-in, default OFF) makes Grade a no-op pass-through ⇒ byte-identical.
	e.suffGate = subconscious.NewSufficiencyGate(c.Cognition, e.backend, e.gates.sufficiency, emit)

	// Critic executive, organs.
	cc := critic.DefaultCriticConfig()
	e.controller = critic.NewController(emit, &cc, c.Cognition, e.backend)
	e.controller.SetBacktrackGate(e.gates.backtrack)               // CONFIG (retrace-off): forbid BACKTRACK when conscious.allow_backtrack is OFF
	e.controller.SetActivityConfig(&e.features.Conscious.Activity) // CONFIG (slice (a)): live conscious.activity decision thresholds
	e.controller.SetActiveResourceGate(e.gates.activeResource)     // A-RAG4: gate V(s)-triggered active re-sourcing (default OFF)
	e.controller.SetRNG(e.rng)                                     // slice (d): seed the soft policy's Boltzmann sampling
	e.value = value.New(emit)
	e.value.SetGroundedRewardGate(e.gates.groundedRew) // CONFIG (M1): gate the grounded-reward term
	// AWAKE-DISP rung 1: the engagement value floor (conscious.activity.awake_user_engage, default OFF).
	// When ON in the AWAKE loop, a focused unresolved user line's V(s) carries an additive boost (the
	// awake_user_engage_weight knob) so it out-competes the endogenous wander. The weight getter reads the
	// LIVE shared config (a TUI live-flip is honoured with no rebuild); the awake predicate gates it to the
	// continuous loop (the value signal is shared with reactive). nil features ⇒ inert. Pattern-A, no model.
	e.value.SetAwakeEngage(e.gates.awakeEngage,
		func() float64 {
			if e.features == nil {
				return 0
			}
			return e.features.Conscious.Activity.AwakeUserEngageWeight
		},
		func() bool { return e.mode == "continuous" })
	e.convert = convert.New(e.subconscious, emit, nil, skillMinter{e.skills})
	if e.features != nil && (e.features.Convert.EvalGate || e.features.Convert.RefineLoop) {
		// slice g: route mints through the eval mint gate. The per-registry refine loop (GAP 11) reuses
		// the SAME stick as the registry's reference, so attaching it is a prerequisite for the loop.
		e.convert.SetMintGate(mintGateStick(e.convert.MintValue()))
	}
	// LEGACY(redesign): the convert.refine_loop OFF-path runs NO per-registry refine loop at idle
	// consolidation (the loop is additive + signal-only — no legacy branch to fall back to, the registries
	// simply never get the standing improve/keep/prune SIGNAL) — removable when the 4 redesign flags are
	// retired (EnableRefineLoop is then unconditional). NOTE line 532's gate is COMPOUND (EvalGate is NOT a
	// redesign flag) so it does NOT become unconditional at retire — only this dedicated gate does.
	if e.features != nil && e.features.Convert.RefineLoop { // GAP 11: the uniform per-registry refine loop (signal-only)
		e.convert.EnableRefineLoop(true, refineEpsilon)
	}
	// W5: the cost-aware trace->skill mint gate (gate registry growth on the COST/efficiency ruler). When
	// on, a recurring program shape is only promoted to a skill once its accumulated re-synthesis cost
	// (NoteSynthesisCost, summed from the synthesize_program llm.call stream below) clears the floor. Default
	// OFF ⇒ no cost consultation, no event ⇒ byte-identical (the count×value heuristic alone decides). The
	// synthCostTap subscriber sums synthesize_program completion tokens off the bus so the engine can
	// attribute the per-episode re-synthesis cost to the shape at the NoteProgram site (reactive.go).
	if e.features != nil && e.features.Convert.CostGate {
		e.convert.EnableCostGate(true, 0) // 0 ⇒ DefaultMintCostFloor (calibrated value rides W5-1's cost ruler)
		e.synthCostTap = &atomic.Int64{}
		tap := e.synthCostTap
		e.bus.Subscribe(func(ev events.Event) {
			if ev.Kind != events.LLM {
				return
			}
			if role, _ := ev.Data["role"].(string); role != "synthesize_program" {
				return
			}
			tap.Add(int64(eventInt(ev.Data, "completion_tokens")))
		})
	}
	// A-RAG5 convertibility-on-facts (default OFF ⇒ no-op ⇒ byte-identical). When on, the sourcing ladder's
	// rung-2 knowledge hits feed NoteFactRecall (the consolidation candidate), episode-close feeds the
	// converged value (AttributeFactValue), and idle Consolidate promotes/reverts via the knowledge registry.
	if e.features != nil && e.features.Convert.Facts {
		e.convert.EnableFactConvert(true)
		// the sourcing policy notifies the fact tracker on every rung-2 (knowledge) hit, where the VERBATIM
		// statement is in hand (it is lost downstream when fuseFuel paraphrases it). nil-safe: only set when on.
		e.sourcing.SetFactRecallNoter(e.convert.NoteFactRecall)
	}
	e.lifecycle = lifecycle.NewDefault(emit)
	e.regulator = regulator.New(emit, nil)
	e.timeline = timeline.New() // slice (i): the episodic attention trajectory
	e.branchGoals = map[int]branchGoal{}
	e.branchFaculty = map[int]cognition.SeedFaculty{}
	e.facultyLastFocus = map[cognition.SeedFaculty]int{}
	e.autoSenseBranch = -1 // #19: "never sensed" sentinel (the autonomous-sense per-focus guard)
	e.autoSenseTick = -1
	e.pendingInj = seams.NewPendingInjectionBuffer(8) // slice (c): late-injection buffer (drained when retracement is on)

	// Continuous-mode additions.
	e.drives = cognition.NewDrivesWithThreshold(emit, e.controller.PursuitThreshold())
	e.defaultMode = cognition.NewDefaultMode(emit)
	// Wire the CONTENT author for the awake idle content (curiosity / association / develop). The
	// awake-mind's idle thoughts are CONTENT (= the model), NOT hardcoded pools — so Drives and
	// DefaultMode author their text via backend.Wander. The closure reads the graph context + rng
	// LAZILY at call time (the graph is created per-run, after construction). On a model decline
	// Wander returns "" and the generators go DARK (the closure threads that through verbatim) — there
	// is no canned fallback in the production path (the cognition guard test asserts a nil author is
	// also dark). The TEST DOUBLE rotates a deterministic offline pool by the rng (goldens stay
	// deterministic + varied).
	e.drives.SetAuthor(e.wander)
	e.defaultMode.SetAuthor(e.wander)
	e.arousal = types.AWAKE
	if e.mode == "continuous" {
		e.port = interaction.NewPerceptionPort(emit)
	} else {
		e.port = interaction.NewInteractionPort(emit)
	}

	// The unified cross-layer model: one addressable graph assembled live from the bus.
	e.cognitionGraph = cogngraph.New()
	e.cognitionGraph.Attach(e.bus)

	// A-RAG3: install the GRAPH-NATIVE recall port (the FuelGraph rung between memory and reality) over the
	// now-constructed cognition graph, gated by subconscious.graph_recall. With the gate OFF (default) the
	// rung is skipped ⇒ byte-identical; with it ON the sourcing ladder traverses the cogngraph for a
	// multi-hop grounded fact before paying for a reality read. The write-back half rides the rung-4 path
	// (realitySourcer), folding each imported reality fact into the graph as a `fact` node + `grounds` edge.
	e.sourcing.SetGraphRecaller(&graphRecaller{e}, e.gates.graphRecall)

	e.focusBound = 9
	e.lull = 0
	e.processSeq = 0
	e.lastOutreach = -999
	e.branchVisits = map[int]int{} // T1.3: UCB visit counts (episode-scoped; reset in startEpisode)
	e.actedBranches = map[int]struct{}{}
	e.forked = map[int]struct{}{}
	e.verifyBranched = map[int]struct{}{}
	e.resourcedBranches = map[int]struct{}{}
	e.verifiedAnswerBranches = map[int]struct{}{}  // T2.1: per-branch once-only answer-verify marker (bounded)
	e.awakeDispatchedBranches = map[int]struct{}{} // AWAKE-DISP: persists across the awake episode (NOT reset per startEpisode — the awake graph is one long episode)
	e.sharedKeys = map[string]struct{}{}
	e.pendingInbox = nil // O-5: no outstanding inbox push at boot

	// Cross-session persistence (M4): when a Store is injected, re-seed every registry from disk BEFORE
	// the first episode — minted skills/operators/specialists, gate priors, episodes, beliefs, knowledge,
	// person prefs are restored (never-fabricate re-applied). nil store ⇒ no-op (the test/heuristic
	// default), so the bare path is byte-identical to pre-M4.
	e.loadState()
	// Deterministic resume (cognitive power-cycle, resume.go): when the resume knob is ON, restore the
	// RNG cursor + tick from the store so the seeded stream CONTINUES instead of restarting at position
	// 0. Default OFF ⇒ cold-boot, byte-identical. Runs AFTER loadState (learned state first).
	e.loadResume()
	// Deterministic percept-log (cognitive power-cycle, Track 1.5, percept.go): when sensing is ON,
	// restore the replayable boundary percepts (the divergence contract REFUSES a version/substrate-
	// mismatched log → cold-sense). Default OFF / nil clock ⇒ no replay ⇒ byte-identical. Runs AFTER the
	// resume cursor (process state first, then the perception record).
	e.loadPerceptLog()
	// Compressed graph spine (cognitive power-cycle, Track 2, graph_spine.go): when the resume knob is
	// ON, rehydrate the lossy L1 spine of the prior line into e.priorContext so a resumed session can
	// re-ground in "where I was" (§4 + §9, light re-orientation). The divergence contract REFUSES a
	// version/substrate-mismatched spine → as-if-cold. Default OFF ⇒ e.priorContext stays nil and nothing
	// reads it ⇒ byte-identical. Runs AFTER the resume cursor + percept-log (process + perception state
	// first, then the orientation spine). NOTHING consumes priorContext yet — Track 3's orientation pass will.
	e.loadGraphSpine()
	return e, nil
}

// skillMinter adapts cognition.SkillRegistry to convert.SkillMinter: convert mints a recurring
// Program into a named skill via Mint(name, triggers, body, description), but the registry's Mint
// also takes a tier (defaulted to "" — the unit tier — here, matching how Python's note->mint path
// promotes a trace-derived skill). Body is convert.Program (a Shape()-only port); the concrete
// *cognition.Program is what convert always passes through, so the assertion succeeds.
type skillMinter struct{ r *cognition.SkillRegistry }

// Mint promotes a recurring Program into a named skill, returning ok=false on rejection (Python
// returned None). The body is the concrete *cognition.Program; an unexpected shape-only port is
// rejected (ok=false) rather than panicking.
func (m skillMinter) Mint(name string, triggers []string, body convert.Program, description string) bool {
	prog, ok := body.(cognition.Program)
	if !ok {
		if p, okp := body.(*cognition.Program); okp {
			prog, ok = *p, true
		}
	}
	if !ok {
		return false
	}
	_, minted := m.r.Mint(name, triggers, prog, "", description)
	return minted
}

// buildExecutor constructs the real-tool executor for the Action layer, sandboxed to the workspace.
// Returns nil when no workspace is configured (the offline heuristic-act path used by tests/CI).
// Mirrors Python Engine._build_executor.
func (e *Engine) buildExecutor(emit events.Emit) *action.ToolExecutor {
	ws := e.cfg.Workspace
	if ws == "" {
		return nil
	}
	ws = expandUser(ws)
	abs, err := filepath.Abs(ws)
	if err == nil {
		ws = abs
	}
	timeout := time.Duration(e.cfg.ToolTimeout * float64(time.Second))
	tools := action.DefaultTools(ws, timeout)
	// WEB-SEARCH (subconscious.web_search, default-OFF): register the model-callable web_search tool so a
	// sub-agent scoped to expose-affordances can dispatch a real web search through the injected web.Web
	// seam (web.DuckDuckGo at the edge, web.Fake in tests). The seam is read LAZILY (lazyWeb reads e.web at
	// Execute time) so a later SetWeb — the edge wires the seam AFTER NewEngine, per the SetWeb-before-Run
	// contract — is honoured; a nil seam (no edge wired: the go-test path) makes web_search a blind read
	// (IsError, no content), so registering it with the flag on but no Web is INERT. OFF (default) ⇒ the
	// tool is not registered ⇒ no web_search in the registry ⇒ byte-identical to the pre-flag pipeline.
	if e.features != nil && e.features.Subconscious.WebSearch {
		tools = append(tools, action.NewWebSearch(lazyWeb{e: e}))
	}
	// FETCH-URL (subconscious.fetch_url, default-OFF, T1.4): register the model-callable fetch_url tool so a
	// sub-agent scoped to expose-affordances can fetch a specific result page through the injected
	// web.PageFetcher seam (web.Pager at the edge, web.FakePager in tests). The seam is read LAZILY
	// (lazyPager reads e.pageFetcher at Execute time) so a later SetPageFetcher — the edge wires the seam
	// AFTER NewEngine, per the SetWeb-before-Run contract — is honoured; a nil seam (no edge wired: the
	// go-test path) makes fetch_url a blind read (IsError, no content), so registering it with the flag on
	// but no seam is INERT. OFF (default) ⇒ the tool is not registered ⇒ no fetch_url in the registry ⇒
	// byte-identical to the pre-flag pipeline.
	if e.features != nil && e.features.Subconscious.FetchURL {
		tools = append(tools, action.NewFetchURL(lazyPager{e: e}))
	}
	// EDIT-FILE (subconscious.edit_file, default-OFF, T1.2): register the model-callable edit_file tool so a
	// mutate-capable sub-agent can surgically str-replace an EXISTING workspace file (the str_replace-editor
	// shape) instead of overwriting it with write_file. Unlike web_search/fetch_url it is a PURE file-op tool
	// scoped to the same workspace as write_file (no injected seam, so no double-gate) — and edit_file is
	// already in action.FileModifyTools, so the executor's sandbox + the gate-router treat it as a local-world
	// mutation identically to write_file the moment it is registered. OFF (default) ⇒ the tool is not
	// registered ⇒ no edit_file in the registry (the DefaultTools 5-tool set is unchanged) ⇒ byte-identical
	// to the pre-flag pipeline.
	if e.features != nil && e.features.Subconscious.EditFile {
		tools = append(tools, action.NewEditFile(ws))
	}
	// READ-DOCUMENT (subconscious.read_document, default-OFF, T2.3): register the model-callable read_document
	// tool so a sub-agent can extract TEXT from a non-plaintext document (PDF/xlsx/docx/…) by shelling out to a
	// host parser (poppler's pdftotext / LibreOffice headless — the same shape as run_tests shelling pytest).
	// Like edit_file it is a PURE file-op tool scoped to the same workspace as read_file (no injected seam, so
	// no double-gate); and read_document is a READ (inspect/local — NOT in action.FileModifyTools), so the
	// executor's sandbox + the gate-router treat it as a free local sense identically to read_file the moment
	// it is registered. Best-effort by contract — a text file reads directly (deterministic), a binary type
	// with no installed parser returns a clear error (never a crash). OFF (default) ⇒ the tool is not
	// registered ⇒ no read_document in the registry (the DefaultTools 5-tool set is unchanged) ⇒ byte-identical
	// to the pre-flag pipeline.
	if e.features != nil && e.features.Subconscious.ReadDocument {
		tools = append(tools, action.NewReadDocument(ws, timeout))
	}
	registry := action.NewToolRegistry(tools)
	// Gate-router (slice j, 03 §3): set the conscious-set ceiling ONLY when action.gate_router is on —
	// non-nil bounds enable the router stage on the executor. Offline-safe default: network policy off /
	// quota 0 (distal sense declined → local fallback). OFF (default) leaves bounds nil → pipeline
	// byte-identical. A self-substrate mutate is refused regardless of network policy (§4).
	var bounds *action.RouteBounds
	if e.features != nil && e.features.Action.GateRouter {
		bounds = &action.RouteBounds{NetworkEnabled: false, NetworkQuota: 0}
	}
	sandbox := action.NewSandbox([]string{ws})
	// Auto-permission (SECURITY-SANDBOX, roadmap §1.5): set the tiered policy ONLY when
	// action.auto_permission is on — non-nil enables the SAFE-auto-approve / DANGEROUS-escalate stage,
	// jailed to the same workspace sandbox. OFF (default) leaves it nil → pipeline byte-identical.
	var autoPerm *action.AutoPermissionPolicy
	if e.features != nil && e.features.Action.AutoPermission {
		autoPerm = &action.AutoPermissionPolicy{Sandbox: sandbox}
		// Per-workspace EXTENSIBLE allowlist + the HIGHER-AUTONOMY PRE-AUTHORIZATION channel
		// (SECURITY-SANDBOX follow-up). Load the project's extra-allowed programs + file-granted
		// pre-auth classes from the workspace config file, and merge in the flag-granted classes
		// (action.auto_permission_pre_auth). All EXPLICIT + default-empty ⇒ with no file + no flag the
		// policy is the curated-seed floor (byte-identical to the slice-1 behaviour). A MALFORMED config
		// file is surfaced (config.skip) and the grants are DROPPED — falling back to the strict floor,
		// NEVER a silent loosening.
		fileExtra, filePreAuth, lerr := action.LoadWorkspaceAutoPermission(ws, e.features.Action.AutoPermissionConfigFile)
		flagPreAuth, ferr := action.ParsePreAuth(e.features.Action.AutoPermissionPreAuth)
		switch {
		case lerr != nil:
			if emit != nil {
				emit(events.ConfigSkip, "auto-permission workspace config skipped (using strict floor)",
					events.D{"component": "action.auto_permission", "reason": lerr.Error()})
			}
		case ferr != nil:
			if emit != nil {
				emit(events.ConfigSkip, "auto-permission pre-auth grant skipped (using strict floor)",
					events.D{"component": "action.auto_permission", "reason": ferr.Error()})
			}
		default:
			autoPerm.ExtraAllowlist = fileExtra
			autoPerm.PreAuthClasses = action.MergePreAuth(filePreAuth, flagPreAuth)
		}
	}
	return action.NewToolExecutor(registry, &action.ExecutorOptions{
		Sandbox:  sandbox,
		Bounds:   bounds,
		AutoPerm: autoPerm,
		// Command content gate: block catastrophic shell commands (rm -rf /, mkfs, curl|sh, …) BEFORE
		// they run — including ones hidden in a compound line (the two-phase tokenized EvaluateCommand).
		// Without this the live path was sandboxed by PATH but not by CONTENT, so run_shell was ungated.
		//
		// CONFIG (safety ablation): the closure consults the LIVE action.safety_gate toggle each call.
		// When OFF (safetyGateEnabled()==false) it returns "" (admit-all) so no command is content-blocked
		// and no action.safety_block fires — the gate-off arm of the safety ablation (spec §5.1). The
		// sandbox stays on regardless; only the content gate is bypassed (the §4.3 bypass-not-delete rule).
		Evaluate: func(toolName, command string) string {
			if !e.safetyGateEnabled() {
				return ""
			}
			return action.DefaultEvaluate(toolName, command)
		},
		Emit: emit,
	})
}

// expandUser expands a leading ~ to the user's home dir (Python os.path.expanduser). A bare "~" or
// "~/..." is expanded; "~user" forms are left unchanged (rare; not in the workspace use).
func expandUser(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// cognitiveView returns a live view of the thinking state (branches + V(s)) handed to dispatch so
// the cognition operators (rank/eliminate/decompose) execute against real state. nil before an
// episode. Mirrors Python Engine._cognitive_view.
func (e *Engine) cognitiveView() *subconscious.CognitiveView {
	if e.graph == nil {
		return nil
	}
	return subconscious.NewCognitiveView(e.graph, e.value)
}

// eventInt reads a positive integer field from an event's Data, tolerating the int / int64 / float64
// shapes the same value takes in-memory vs after a JSON log round-trip (mirrors campaign.intData). Used
// by the W5 synth-cost tap to read completion_tokens off the bus. Returns 0 when absent or non-positive.
func eventInt(d events.D, key string) int {
	switch v := d[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return 0
}

// -- public API ----------------------------------------------------------

// Emit exposes the bus emit closure (Python's `emit` property). Used by wiring/tests.
func (e *Engine) Emit() events.Emit { return e.bus.Emit }

// Bus exposes the event bus (so the CLI/TUI can subscribe sinks). Not a Python attribute but the
// Go callers need the concrete bus to wire trace sinks.
func (e *Engine) Bus() *events.Bus { return e.bus }

// BackendLabel reports the resolved backend's display name (Python self.backend_label).
func (e *Engine) BackendLabel() string { return e.backendLabel }

// SubstrateClass reports the canonical class of the RUNNING substrate (test | local | frontier |
// session | claude), derived from the backend's own stamp — never by parsing display labels.
// "" only for an unrecognised externally-injected backend.
func (e *Engine) SubstrateClass() string { return llm.ClassOf(e.backend) }

// Mode reports the current loop regime (Python self.mode).
func (e *Engine) Mode() string { return e.mode }

// LastResponse reports the most recent answer delivered to the user (Python self.last_response).
func (e *Engine) LastResponse() string { return e.lastResponse }

// Controller exposes the Critic's executive half so the compare diagnostic (PORT-PLAN #42) can swap
// its decision backend/mode and read its decision tallies/last_meta. Python reaches eng.controller
// directly; the field is unexported in Go, so this accessor is the seam. Used only off the hot path.
func (e *Engine) Controller() *critic.Controller { return e.controller }

// LifecycleState reports the current lifecycle state name (Python eng.lifecycle.state.name). The
// lifecycle field is unexported; this accessor stands in for that attribute read.
func (e *Engine) LifecycleState() string { return e.lifecycle.State.String() }

// Regulator exposes the homeostatic regulator so the stability suite can re-derive the durability
// checklist (regulator.stability) over a finished run. Python reads engine.regulator directly; the
// field is unexported in Go, so this accessor stands in for that attribute access. Read-only.
func (e *Engine) Regulator() *regulator.Regulator { return e.regulator }

// Catalog exposes the persistent operator catalog so the stability suite can count minted operators
// over a run (Python reads engine.catalog.minted directly). Read-only.
func (e *Engine) Catalog() *cognition.OperatorRegistry { return e.catalog }

// Backend exposes the resolved thinking substrate so the CLI summary can read its call/fallback
// tallies (Python's `hasattr(eng.backend, "calls")` probe on the LLM backend). Read-only.
func (e *Engine) Backend() backends.Backend { return e.backend }

// CognitionGraph exposes the unified cross-layer model assembled live from the event bus, so the
// `cognition` CLI / TUI can render it (X.5 — it was built + bus-attached but had no read accessor).
// Read-only.
func (e *Engine) CognitionGraph() *cogngraph.CognitionGraph { return e.cognitionGraph }

// Skills exposes the persistent skill registry (seeded + minted) so the TUI registry browser can list
// the goal-matched capability layer over operators. Read-only.
func (e *Engine) Skills() *cognition.SkillRegistry { return e.skills }

// Grounding exposes the reality-grounding experiment memory (SR-4): the validation ledger of every
// claim the harness has grounded/refuted against reality, with its trust tier + epistemic status. The
// TUI Grounding tab reads it for the experiment ledger. Read-only.
func (e *Engine) Grounding() *grounding.ExperimentMemory { return e.grounding }

// AddSensor registers a standing percept source (a file/test/build/log watcher) polled on every awake
// tick to re-ground claims with no ACT (N.1a-cont / AR-6). Continuous mode wires real watchers here.
func (e *Engine) AddSensor(s grounding.Sensor) { e.sensors = append(e.sensors, s) }

// EstimatorVitals exposes the SLAM self-state estimator's compact calibration readout (Track F / M1):
// the count of tracked beliefs, how many are reality-GROUNDED (have a FEJ anchor), and the mean belief
// variance (high = mostly self-derived; low = mostly grounded/calibrated). It is the data the Ctrl+O
// runtime monitor's "EST:" calibration-vitals line renders next to VALUE — the estimator being V(s)'s
// uncertainty twin. Returns zeros when the slam.innovation knob is OFF (the estimator is inert).
// Read-only; ok=false when there is no estimator.
func (e *Engine) EstimatorVitals() (beliefs, grounded int, meanVar float64, ok bool) {
	if e.estimator == nil {
		return 0, 0, 0, false
	}
	b, g, mv := e.estimator.Vitals()
	return b, g, mv, true
}

// EstimatorConsistency exposes the SLAM M5 consistency/observability witness (Track F / M5): the
// information the estimator gained from grounded observations vs spuriously (in unobservable directions),
// the grounded fraction, the write counts, and whether the invariant held over the run. It is what the
// offline durability check (internal/stability.ConsistencyInvariantHolds) and the Ctrl+O runtime monitor
// read to verify the estimator is not gaining spurious information — the awake-durability requirement.
// Returns the zero Consistency (vacuously consistent) + ok=false when the slam.consistency monitor is OFF
// or there is no estimator. Read-only.
func (e *Engine) EstimatorConsistency() (estimate.Consistency, bool) {
	if e.estimator == nil {
		return estimate.Consistency{}, false
	}
	c := e.estimator.ConsistencyState()
	return c, e.features != nil && e.features.Slam.Innovation && e.features.Slam.Consistency
}

// CalibrationVitals exposes the SLAM M9 calibration meta-estimator's compact readout (Track F / M9):
// how many trust tiers have an IDENTIFIED (enough-sampled) learned reliability, the worst (most
// overconfident) tier, and its confident-refute fraction — the headline "I am confidently wrong against
// an independent source" alarm that the same-model ceiling produces. Returns zeros + ok=false when the
// slam.calibration knob is OFF (the calibrator is inert) or there is no calibrator. Read-only.
func (e *Engine) CalibrationVitals() (identifiedTiers, worstTier int, worstOverconfidence float64, ok bool) {
	if e.calibrator == nil || !e.calibrator.Enabled() {
		return 0, -1, 0, false
	}
	id, wt, wo := e.calibrator.Vitals()
	return id, wt, wo, true
}

// Sessions exposes the current episode's runtime spawn tree — the bounded Session tree mirroring the
// synthesised workflow (P3.3), with per-session budgets + the lifecycle horizon. nil when no multi-phase
// program was synthesised (simple Q&A). The TUI Runtime tab renders it (viz.RenderSessionTree). Read-only.
func (e *Engine) Sessions() *session.Session { return e.sessionRoot }

// Episodic / Semantic / Person expose the declarative memory registries (P2.3/P6.x) so the TUI Registry
// tab can list the recorded episodes, the currently-valid beliefs, and the learned preferences. Read-only.
func (e *Engine) Episodic() *memory.EpisodicRegistry { return e.episodic }
func (e *Engine) Semantic() *memory.SemanticRegistry { return e.semantic }
func (e *Engine) Person() *memory.PersonRegistry     { return e.person }

// RetrieverMode reports the shared retriever's mode — "hybrid" (a reachable embedder ⇒ lexical+semantic
// fused by RRF) or "lexical" (Jaccard only, the offline default). The TUI shows it as an always-visible
// status so the retrieval primitive is observable even when no recall has fired. Read-only.
func (e *Engine) RetrieverMode() string { return e.retrieverMode }

// LastBridge / LastFabricated report how the most recent observation reached reality (N.4 bridge:
// structured|scraped|none) and whether it was a tier-0 FABRICATION (P0.6 — the offline heuristic stand-in
// makes these up). The action panel surfaces them so a faked reality is visible. Read-only.
func (e *Engine) LastBridge() string   { return e.lastBridge }
func (e *Engine) LastFabricated() bool { return e.lastFabricated }

// Tools exposes the Action-layer tool registry the executor resolves against, so the TUI registry
// browser can list the available real tools. nil on the offline/no-workspace path (no executor).
// Read-only.
func (e *Engine) Tools() *action.ToolRegistry {
	if e.executor == nil {
		return nil
	}
	return e.executor.Registry()
}

// Arousal reports the current arousal level name (Python eng.arousal.name), shown in the CLI
// summary. The arousal field is unexported; this accessor stands in for that attribute read.
func (e *Engine) Arousal() types.Arousal { return e.arousal }

// Graph exposes the active thought graph so the CLI summary can report node/branch counts (Python
// reads eng.graph directly). nil before the first episode opens. Read-only.
func (e *Engine) Graph() *graph.ThoughtGraph { return e.graph }

// ActiveValue reports the active line's V(s) (the epistemic scalar — content quality, urgency
// excluded) at the current state, the SAME signal the scheduler/filter-trust consume. The
// deliberative reconciliation (THOUGHT_DELIBERATIVE_K) reads it per-sample as the majority-vote
// TIE-BREAK rank key — never as a new scorer (it is the existing value.ValueSignal output). 0.0
// before the first episode opens. Read-only.
func (e *Engine) ActiveValue() float64 { return e.valueScalar() }

// UserWaiting reports whether a user turn is still awaiting an answer — DERIVED from graph state
// (a live branch holds a USER_INPUT thought beyond the delivery high-water mark), never a sticky
// flag (A4). Consumed by the scheduler (budget when a user waits), the outreach gate (never muse
// over an unanswered question), and the awake STOP path (an answered satisfied line is spoken).
func (e *Engine) UserWaiting() bool {
	if e.graph == nil {
		return false
	}
	return e.graph.UserWaiting() // ONE definition, shared with the Controller's DELIVER fork
}

// Convert exposes the convertibility organ so the CLI summary can list minted specialists (Python
// reads eng.convert.minted directly). Read-only.
func (e *Engine) Convert() *convert.Convertibility { return e.convert }

// Transcript returns the conversation-memory turns recorded across episodes, in order, as
// (role, text) pairs (Python reads eng.transcript directly — a list of (role, text) tuples). The
// cognition gate's multi-turn-memory property asserts on its length. A fresh copy is returned so a
// caller cannot mutate the engine's transcript. Read-only.
func (e *Engine) Transcript() [][2]string {
	out := make([][2]string, len(e.transcript))
	for i, t := range e.transcript {
		out[i] = [2]string{t.Role, t.Text}
	}
	return out
}

// -- TUI read accessors (DESIGN §4.5) -------------------------------------
// These stand in for the direct attribute reads the Python panels make off `eng` (see
// the removed Python `tui/panels.py`). They are read-only and add NO behaviour:
// each returns a lower-tier type (or a copy), so they introduce no import cycle and the engine
// stays headless-pure. The TUI bridge reads them at end-of-tick to assemble a snapshot.

// Subconscious exposes the Subconscious engine so the TUI can read the live recognised workflow
// (eng.subconscious.workflow in Python's render_subconscious). Read-only.
func (e *Engine) Subconscious() *subconscious.SubconsciousEngine { return e.subconscious }

// Lifecycle exposes the lifecycle state machine so the TUI reads state + the transition history
// (Python render_lifecycle reads eng.lifecycle.state / eng.lifecycle.history). Read-only.
func (e *Engine) Lifecycle() *lifecycle.Lifecycle { return e.lifecycle }

// Workflow returns the currently-recognised workflow (nil ⇒ none) — the read side of the Python
// `eng.subconscious.workflow` attribute the render_subconscious panel consults. Read-only.
func (e *Engine) Workflow() *subconscious.Workflow { return e.subconscious.Workflow() }

// ActionOutstanding reports how many async watched-seam actions are still awaiting feedback
// (Python `len(eng.awatched.outstanding)` in render_action). Read-only.
func (e *Engine) ActionOutstanding() int { return e.awatched.OutstandingCount() }

// ActionLatency reports the async watched-seam dead-time in ticks (Python `eng.awatched.latency`
// in render_action). Read-only.
func (e *Engine) ActionLatency() int { return e.awatched.Latency }

// ActedBranches returns the ids of branches that have already opened to reality, ascending (Python
// `sorted(eng.acted_branches)` in render_action). A fresh slice — safe to retain. Read-only.
func (e *Engine) ActedBranches() []int {
	out := make([]int, 0, len(e.actedBranches))
	for id := range e.actedBranches {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

// Submit queues an inbound prompt/percept on the port. Mirrors Python submit(text, *, salient=True):
// the salient keyword defaults to True; source defaults to USER_INPUT (the port's Receive default).
func (e *Engine) Submit(text string, salient bool) {
	e.port.Receive(text, types.USER_INPUT, salient)
}

// SubmitDefault is the Python default call: salient=True.
func (e *Engine) SubmitDefault(text string) { e.Submit(text, true) }

// PortPending reports whether the inbound port holds an unconsumed message (Python eng.port.pending()).
// Exposed so an external driver (the scenario runner, PORT-PLAN #40) can replicate the engine's own
// idle-and-empty quiescence test without reaching into the unexported port.
func (e *Engine) PortPending() bool { return e.port.Pending() }

// HasOutstandingAction reports whether an async watched-seam action is still in flight awaiting its
// observation (Python eng.awatched.has_outstanding()). Exposed for the scenario runner's idle test.
func (e *Engine) HasOutstandingAction() bool { return e.awatched.HasOutstanding() }

// SetMode toggles reactive<->continuous, carrying the inbox over to the right port type. Mirrors
// Python set_mode. A no-op when already in the requested mode.
func (e *Engine) SetMode(mode string) {
	if mode == e.mode {
		return
	}
	e.mode = mode
	pending := e.port.PendingMessages()
	if mode == "continuous" {
		e.port = interaction.NewPerceptionPort(e.bus.Emit)
	} else {
		e.port = interaction.NewInteractionPort(e.bus.Emit)
	}
	e.port.RestoreMessages(pending)
	e.arousal = types.AWAKE
	if wf := e.subconscious.Workflow(); wf != nil { // don't carry a recognised workflow across a switch
		wf.Reset()
	}
	e.bus.Emit(events.Arousal, "mode -> "+mode, events.D{"mode": mode})
}

// Run steps until quiescent/idle with no pending input, or the budget is exhausted. Mirrors Python
// run(max_ticks=None): the budget is max_ticks or cfg.max_ticks. Pass <=0 for the config default.
func (e *Engine) Run(maxTicks int) {
	budget := maxTicks
	if budget <= 0 {
		budget = e.cfg.MaxTicks
	}
	for i := 0; i < budget; i++ {
		if e.stopReq.Load() { // cooperative shutdown (shutdown.go): the edge requested stop; flush happens there
			break
		}
		res := e.Step()
		if res.Idle && !e.port.Pending() && !e.awatched.HasOutstanding() {
			break
		}
	}
}

// RunDefault runs with the config's max_ticks budget (Python run() with max_ticks=None).
func (e *Engine) RunDefault() { e.Run(0) }

// Step advances the bus tick, refreshes the model-call budget, emits the tick event, and dispatches
// to the reactive / continuous loop on mode. Mirrors Python step().
func (e *Engine) Step() StepResult {
	e.bus.Tick++
	tick := e.bus.Tick
	// SLAM M1: stamp the seeded loop tick on the self-state estimator so a FEJ anchor records the real
	// observation tick (not 0). Pure setter, emits nothing — byte-identical whether the knob is on/off.
	e.estimator.SetTick(tick)
	e.calibrator.SetTick(tick) // M9: same deterministic-tick stamp for the calibration wire
	// SLAM M4 (Track F): the per-tick FRESHNESS / STALENESS-DECAY sweep — GROW every grounded belief's
	// variance back toward the prior ceiling as a function of its un-refreshed age (the dynamic-map process
	// noise Q>0, P4). slamInnovationEnabled() first syncs the live SLAM sub-flags (so a TUI flip of
	// slam.staleness / slam.staleness_q is honoured); then Decay() runs the sweep, which is a no-op (no
	// event) unless BOTH slam.innovation AND slam.staleness are on — so an un-monitored run is byte-identical.
	// Decay only RAISES variance (loses certainty), so it stays inside the §0/M5 consistency invariant.
	if e.slamInnovationEnabled() {
		e.estimator.Decay()
	}
	// A non-default config is never silent (§4.1): announce which toggles are OFF, deferred to the first
	// Step so the CLI/TUI sinks (subscribed after NewEngine) actually receive it. Emit ONLY when
	// something is OFF — the all-on default (and Features=nil) stays byte-identical (zero off-paths ⇒
	// no emit ⇒ scenario goldens unchanged).
	if !e.configAnnounced {
		e.configAnnounced = true
		if off := e.features.OffPaths(); len(off) > 0 {
			e.bus.Emit(events.ConfigLoad, "config: "+itoa(len(off))+" toggle(s) OFF",
				events.D{"off": off, "count": len(off)})
		}
		// PERSIST (M4): announce the loaded learned state (deferred from NewEngine so the sinks receive
		// it). Emitted only when something was restored — a cold start / nil store stays silent.
		if e.loadSummary != nil {
			e.bus.Emit(events.PersistLoad, "persist: restored learned state from disk", e.loadSummary)
		}
		// A-RAG2: announce whether the embeddings SIDECAR lit up the dense retrieval channel (deferred from
		// NewEngine so the sinks receive it). nil ⇒ subconscious.semantic_recall is OFF (silent, byte-identical).
		if e.semanticAnnounce != nil {
			e.bus.Emit(events.RetrievalSemantic, e.semanticAnnounceMsg, e.semanticAnnounce)
		}
	}
	// Refresh the model-call budget for this tick (more when a user waits / the active line is
	// valuable; less when idle). Foreground reasoning is never throttled; background overflows.
	if e.graph != nil {
		e.scheduler.TickReset(e.valueScalar(), e.UserWaiting())
	} else {
		e.scheduler.TickReset(0.5, e.UserWaiting())
	}
	e.bus.Emit(events.Tick, "tick "+itoa(tick)+" ["+e.mode+"] state="+e.lifecycle.State.String()+
		" arousal="+e.arousal.String(), events.D{})
	var res StepResult
	if e.mode == "continuous" {
		res = e.stepContinuous(tick)
	} else {
		res = e.stepReactive(tick)
	}
	// SLAM M5 (Track F): emit the consistency/observability witness at the END of the tick — after every
	// grounded observation this tick has folded in — so the trace/monitor show, live, that the self-
	// estimator gained no spurious information in unobservable directions (the Huang-2010 EKF-inconsistency
	// overconfidence that compounds over a long awake run). No-op (no event) unless BOTH slam.innovation
	// AND slam.consistency are on, so an unmonitored run is byte-identical. This is the awake-durability
	// gate's runtime half; the offline half is internal/stability.ConsistencyInvariantHolds.
	e.estimator.CheckConsistency()
	return res
}
