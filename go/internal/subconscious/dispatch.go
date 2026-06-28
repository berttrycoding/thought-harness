// dispatch.go ports subconscious/dispatch.py — Subconscious dispatch, PULL, not push.
//
// Every specialist reads the active context and fires IF its (bias-adjusted) relevance crosses the
// admission threshold theta (set by the regulator). No orchestrator chooses them. A recognised
// workflow may bias firing and instantiate an ephemeral per-phase specialist at runtime.
//
// PORT NOTE (Tier 6, the engine spine of the subconscious). SubconsciousEngine is the pull-dispatch
// loop:
//
//   - The roster for one Dispatch call is the base specialists PLUS this phase's instantiated
//     SubAgents — built LOCAL to the call (`roster := slices(self.specialists) ; roster.extend(...)`),
//     never mutating the shared self.specialists. That per-call-local copy is what keeps Dispatch
//     free of a shared-state race when the same engine is reused tick after tick.
//   - Register(specialist) is the ONLY mutation of the shared roster: convertibility mints a
//     MintedPrimitiveSubAgent and appends it. This type SATISFIES convert.PrimitiveSubAgentRegistrar structurally
//     (its Register(PrimitiveSubAgent) method) — no import the other way; the engine wires it in.
//   - The seeded *cpyrand.Random is threaded into every Fire (never a package-global) so the stream
//     is reproducible AND byte-identical to CPython's random.Random draw sequence.
//
// Ported from the (now-removed) Python thought_harness/subconscious/dispatch.py. The emit-site event KINDS
// (subconscious.{workflow,fire,dispatch,quiet}), their data KEYS, and the round(x,3) scan entries are
// kept byte-identical to Python (golden-tested): SUB_FIRE/SUB_DISPATCH/SUB_QUIET carry the UNROUNDED
// theta/relevance, only the per-specialist scan entries round relevance + effective to 3 decimals.
package subconscious

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// parallelPhases is the DEFAULT-ON flag for determinism-preserving per-phase execution concurrency
// (07-OPTIMISATION-SURVEY.md §A.1). When ON (the validated default), the independent REASON-ONLY
// sub-agents of a Parallel phase-group fire CONCURRENTLY (goroutines + WaitGroup, bounded by MaxParWidth
// and a semaphore), but their candidates AND buffered events are collected and replayed in deterministic
// step-INDEX order, so the result + trace stay BYTE-IDENTICAL to serial — only wall-clock changes (the
// speed-up). When OFF (THOUGHT_PARALLEL_PHASES=0, the legacy serial path) Dispatch is byte-for-byte the
// old serial loop — the on-path is never entered. The default-ON flip is gated on: golden byte-identical
// parity (the standing scenarios cross-package regression), the determinism-equality tests (32×/-race),
// and the standing parallel-phases durability cell (the five conditions hold, fan-out ≤ W_max). Resolved
// ONCE at init, mirroring MaxParWidth's THOUGHT_MAX_PAR_WIDTH read.
//
// EXPERIMENTAL (the ON path): concurrency is applied ONLY to a Parallel group whose sub-agents are all
// reason-only (no executor/tool path, no cognition-exec path) — those are pure model calls with no
// shared mutable state. A group containing an effectful or cognition-bound sub-agent falls back to the
// serial path, because the executor emits action.* events straight to the bus (un-buffered) and reads
// shared sandbox/graph state, which cannot be replayed in index order without racing. So result + trace
// determinism is guaranteed in every case; the speedup applies to the reason-only fan-out.
var parallelPhases = resolveParallelPhases()

// resolveParallelPhases reads THOUGHT_PARALLEL_PHASES once, defaulting to TRUE (on). Accepts the same
// truthy/falsy vocabulary as the LLM env toggles (1/true/yes/on vs 0/false/no/off, case-insensitive); an
// unset or unrecognised value keeps the validated default-ON (the per-phase concurrency speed-up — proven
// byte-identical to serial on the golden gate + the determinism-equality tests, durability-gated by the
// standing parallel-phases stability cell). Set THOUGHT_PARALLEL_PHASES=0 to force the legacy serial path.
func resolveParallelPhases() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("THOUGHT_PARALLEL_PHASES"))) {
	case "0", "false", "no", "off", "n":
		return false
	default:
		return true
	}
}

// SetParallelPhasesForTest overrides the per-phase concurrency flag for the duration of a test/validation
// run and returns a restore closure (defer it). The package-level flag is resolved ONCE from env at init,
// so a downstream package's golden/parity test cannot reach it through t.Setenv; this is the only seam that
// lets a cross-package gate (e.g. the scenarios golden-parity-under-concurrency regression) force the path
// ON/OFF in-process. It is a TEST/VALIDATION hook only — production resolves the flag from env at init.
//
// NOTE: this single flag governs BOTH concurrency seams — the workflow Par phase-group fan-out (seam #1)
// AND the per-tick base-specialist model-call fan-out (seam #2). Both are RNG-free, buffer-free/index-
// ordered, byte-identical to serial; they share one switch so a single THOUGHT_PARALLEL_PHASES=0 forces
// the whole legacy serial path, and the standing golden/stability gates cover both in one toggle.
func SetParallelPhasesForTest(on bool) (restore func()) {
	prev := parallelPhases
	parallelPhases = on
	return func() { parallelPhases = prev }
}

// SubconsciousEngine is the Subconscious pull-dispatch engine: it holds the base specialist roster, the
// seeded rng, the bus closure, the optional recognised workflow, and the Action layer's executor.
// Mirrors the Python SubconsciousEngine class (fields specialists/rng/emit/workflow/executor).
type SubconsciousEngine struct {
	specialists []PrimitiveSubAgent // the base roster (the ONLY field Register mutates)
	rng         *cpyrand.Random     // seeded CPython-parity RNG threaded into every Fire (never package-global)
	emit        events.Emit         // bus closure (Python self.emit); nil ⇒ no events
	workflow    *Workflow           // the recognised workflow that biases + instantiates (nil ⇒ none)
	// The Action layer's executor — handed to each runtime sub-agent so an effectful operator's
	// tool_scope actually dispatches (nil ⇒ offline/test path: sub-agents reason only). Mirrors
	// Python's `executor: object | None`.
	executor *action.ToolExecutor
	// sched is the LLM-call scheduler (nil ⇒ none bound — the test/offline path). The parallel-phase
	// pre-fire reads its remaining BACKGROUND budget to size the concurrent set deterministically
	// (first-k in step order) instead of letting goroutine completion order race for the budget.
	sched *scheduler.LLMScheduler
	// recognizer is the GAP 5-DEEPER relevance/dispatch ENTRY: when set (subconscious.capability_dispatch
	// on), the dispatch loop routes the per-tick recognition decision THROUGH it — the producing Capability
	// owns "does this workflow apply this tick?" — instead of the Workflow self-triggering (Recognize). nil
	// (the default + the legacy path) ⇒ the Workflow self-triggers exactly as before, byte-identical. The
	// recognizer must mutate the workflow's recognized flag (the load-bearing Recognize-before-GateBias
	// ordering) and return an IDENTICAL verdict in this safe stage — only WHO calls it moves onto the entry.
	recognizer WorkflowRecognizer
	// psaGate is the GAP 5-DEEPER PART 2 SPECIALIST-firing ENTRY: when set (subconscious.capability_specialists
	// on, AND a producing Capability exists), the producing Capability OWNS the per-specialist admission decision
	// ("does THIS specialist fire this tick?"). The dispatch loop admits a specialist iff (eff>theta AND
	// psaGate.AdmitPrimitiveSubAgent(domain)), subsuming the bare relevance-firing the redesign names as the entry's
	// OTHER half (§3.3 "specialists firing on relevance — but there is no unifying Capability object"). nil (the
	// default + the legacy path) ⇒ admission is the bare eff>theta, byte-identical. The gate's safe-stage
	// predicate (Capability.AdmitPrimitiveSubAgent) gates on the run's §3.3a Scope DOMAIN band: a general (empty-domain)
	// episode scope admits every domain ⇒ the episode path is byte-identical; a domain-banded Capability admits
	// only its band's specialists (the least-privilege bite — a worker may never widen the ceiling). It is
	// applied IDENTICALLY at all three admission sites (the serial loop + both concurrency pre-fires) so the
	// default-ON parallel path stays byte-identical to serial. Pattern-A pure CONTROL (string compare + θ, no
	// model call).
	psaGate PrimitiveSubAgentGate
	// sparseDispatch turns on the SPARSEMAX admission over the base-specialist relevance field
	// (subconscious.dispatch.sparse, docs/internal/notes/2026-06-21-attention-mechanisms-litreview.md §4): the
	// dispatch loop admits a base specialist iff its sparsemax mass p_i>0 AND eff>theta (θ survives as a
	// FLOOR under the induced τ), stamps p_i as the candidate's dispatch confidence, and emits
	// subconscious.sparse. The sparsemax competes over the RELEVANCE-FIRED SPECIALIST population only — a
	// *SubAgent (a workflow's staffed worker) is authorised by produce/staffing, not pulled on relevance, so
	// it keeps the bare eff>theta admission (never sparsemax-gated). false (the default) ⇒ the legacy
	// per-key absolute admitFire, byte-identical (no event). Pattern-A pure CONTROL (closed-form simplex
	// projection, NO model). HARD BOUNDARY: this is the SUBCONSCIOUS pull only — the conscious focus stays
	// hard argmax (GWT ignition), untouched.
	sparseDispatch bool
	// singleStrong COLLAPSES the per-tick fired set to its SINGLE BEST MEMBER (highest stamped effective
	// relevance) before the candidates leave Dispatch — the "single strong agent" reference for the
	// teams-vs-best-member GUARD (subconscious.single_strong_agent; docs/internal/2026-06-21-sota-benchmark-
	// suite.md §7.6). When on AND more than one specialist fired this tick, every teammate but the strongest
	// is dropped and subconscious.single_strong is emitted. false (the default) ⇒ the full fan-out reaches
	// the gate, byte-identical, no event. Pattern-A pure CONTROL (closed-form argmax over the already-scored
	// fired field, NO model). It strictly REDUCES the fired set (fewer candidates, fan-out 1), so it can
	// never raise the branching ratio n — it makes the plant less excited, never more.
	singleStrong bool
}

// PrimitiveSubAgentGate is the GAP 5-DEEPER PART 2 specialist-firing entry port: an object that OWNS the dispatch
// loop's per-specialist admission ("does this specialist fire this tick?"), the OTHER half of §3.3 the
// Capability subsumes (the first half — workflow recognition — is WorkflowRecognizer). The producing
// Capability satisfies it via AdmitPrimitiveSubAgent. The dispatch loop's admission predicate becomes
// `eff>theta && gate.AdmitPrimitiveSubAgent(domain)` at EVERY admission site (the serial loop + both concurrency
// pre-fires) when the gate is wired; a nil gate (the default + the OFF path) ⇒ admission is the bare
// eff>theta, byte-identical. The port is the seam that hands the specialist-firing entry to the Capability
// without the subconscious importing the engine — the twin of WorkflowRecognizer.
type PrimitiveSubAgentGate interface {
	// AdmitPrimitiveSubAgent reports whether the Capability admits a specialist of this DOMAIN to fire this tick.
	// The eff>theta relevance gate is applied by the dispatch loop FIRST and independently; this gate is the
	// ADDITIONAL Capability-owned authority check (§3.3a Scope domain band) layered on top — it can only ever
	// DENY a specialist the relevance gate already admitted, never admit one relevance rejected. Must be
	// deterministic (no RNG/clock) so the admission set is reproducible.
	AdmitPrimitiveSubAgent(domain string) bool
}

// WorkflowRecognizer is the relevance-entry port: an object that OWNS the dispatch loop's recognition
// decision for a produced workflow (GAP 5-DEEPER, §3.3 — the Capability is the entry, not the self-
// triggering Workflow). The Capability satisfies it via RecognizeWorkflow. RecognizeWorkflow MUST mutate
// wf's recognized flag (so GateBias reads it) and decide recognition with a PERMISSIVE has-any relevance:
//
//	!Exhausted() && (Bespoke || gradedRelevance(stream) > 0)   // has-any, NOT θ-gated
//
// with the bespoke short-circuit preserved. Recognition answers "does this workflow APPLY", which is the
// SAME criterion as the legacy binary Recognize (a non-bespoke workflow fires when at least one trigger
// matches); the supplied theta is intentionally NOT consulted at recognition — it is the DOWNSTREAM
// value/admission bar (GateBias / the value filter), the same bar the dispatch loop admits specialists at.
//
// DO NOT θ-gate recognition (`gradedRelevance >= θ`): that is the REFUTED double-gate. The paired
// E5-deeper live A/B (see Workflow.recognizeViaGraded) REGRESSED multi-hop grounding 0.89→0.71 precisely
// because gating recognition on θ dropped weakly-but-genuinely-relevant non-bespoke workflows that the
// has-any path fires and that help the grounding chain — the borrowed-threshold trap (a specialist-FIRING
// admission bar reused as a "does-this-apply" recognition bar). theta is carried on the signature only so
// an implementation MAY thread it onward to a downstream gate, never to gate recognition itself.
//
// A nil recognizer on the engine ⇒ the Workflow self-triggers (the legacy binary path); the port is the
// seam that hands the relevance entry to the Capability without the subconscious importing the engine.
type WorkflowRecognizer interface {
	RecognizeWorkflow(wf *Workflow, ctx []types.Thought, theta float64) bool
}

// NewSubconsciousEngine builds the engine with Python's keyword-construction shape:
// SubconsciousEngine(specialists, rng, emit, workflow=None, executor=None). Pass a nil workflow /
// executor for the offline path (no recognised program, sub-agents reason only).
func NewSubconsciousEngine(specialists []PrimitiveSubAgent, rng *cpyrand.Random, emit events.Emit,
	workflow *Workflow, executor *action.ToolExecutor) *SubconsciousEngine {
	return &SubconsciousEngine{
		specialists: specialists,
		rng:         rng,
		emit:        emit,
		workflow:    workflow,
		executor:    executor,
	}
}

// Register adds a specialist minted by convertibility, mutating the shared base roster. This is the
// method that satisfies convert.PrimitiveSubAgentRegistrar structurally (Register(PrimitiveSubAgent)). Mirrors
// Python register: `self.specialists.append(specialist)`.
func (e *SubconsciousEngine) Register(specialist PrimitiveSubAgent) {
	e.specialists = append(e.specialists, specialist)
}

// SetPrimitiveSubAgents replaces the base roster. The engine uses it to install the real primitive set (M2)
// AFTER constructing the engine, so the tool-backed primitives can hold the engine itself as their
// ExecutorProvider (a forward reference resolved lazily at Fire time). Pass the full roster — this is
// the only wholesale replacement; convertibility still appends via Register.
func (e *SubconsciousEngine) SetPrimitiveSubAgents(specialists []PrimitiveSubAgent) {
	e.specialists = specialists
}

// SetExecutor wires the Action-layer executor into the engine so a workflow's effectful sub-agents
// dispatch their scoped tools (nil ⇒ offline path). Mirrors Python's `self.subconscious.executor =
// self.executor` — a direct field assignment the engine performs at construction.
func (e *SubconsciousEngine) SetExecutor(executor *action.ToolExecutor) { e.executor = executor }

// SetScheduler wires the LLM-call scheduler (the same instance bound into the backend) so the
// parallel-phase pre-fire can size its concurrent set by the remaining background budget. nil ⇒
// unbounded (the test/offline path, where no call spends the model).
func (e *SubconsciousEngine) SetScheduler(s *scheduler.LLMScheduler) { e.sched = s }

// Executor returns the wired Action-layer executor (nil ⇒ offline), satisfying the ExecutorProvider
// port the tool-backed primitives (read/search/run) resolve lazily at Fire time. The engine wires the
// SAME executor here via SetExecutor AFTER construction, so a primitive built in DefaultPrimitiveSubAgents
// before the workspace executor exists still reaches it when it fires (M2).
func (e *SubconsciousEngine) Executor() *action.ToolExecutor { return e.executor }

// SetWorkflow swaps the recognised workflow that biases firing + instantiates per-phase sub-agents
// (nil ⇒ none). Mirrors Python's per-episode `self.subconscious.workflow = ...` field assignment.
func (e *SubconsciousEngine) SetWorkflow(w *Workflow) { e.workflow = w }

// SetRecognizer wires the GAP 5-DEEPER relevance/dispatch ENTRY: the producing Capability that OWNS the
// per-tick recognition decision (subconscious.capability_dispatch). The engine sets it ALONGSIDE the
// episode workflow (SetWorkflow) so the entry that PRODUCED the workflow is also the entry that triggers
// it. nil (the default + when the flag is off) ⇒ the Workflow self-triggers (Recognize), byte-identical.
// The recognizer must own a recognition that mutates wf.recognized and (this safe stage) returns the
// IDENTICAL verdict — see WorkflowRecognizer.
func (e *SubconsciousEngine) SetRecognizer(r WorkflowRecognizer) { e.recognizer = r }

// Recognizer returns the currently-wired relevance entry (nil ⇒ the Workflow self-triggers) — the read
// accessor a wiring-gate test reads to confirm the Capability is the live entry when the flag is on.
func (e *SubconsciousEngine) Recognizer() WorkflowRecognizer { return e.recognizer }

// SetPrimitiveSubAgentGate wires the GAP 5-DEEPER PART 2 specialist-firing ENTRY: the producing Capability that
// OWNS the per-specialist admission decision (subconscious.capability_specialists). The engine sets it
// ALONGSIDE the episode workflow/recognizer (SetWorkflow/SetRecognizer) so the entry that PRODUCED the
// workflow is also the authority that admits the firing specialists. nil (the default + when the flag is
// off) ⇒ admission is the bare eff>theta (byte-identical). See PrimitiveSubAgentGate.
func (e *SubconsciousEngine) SetPrimitiveSubAgentGate(g PrimitiveSubAgentGate) { e.psaGate = g }

// PrimitiveSubAgentGate returns the currently-wired specialist-firing entry (nil ⇒ bare eff>theta admission) —
// the read accessor a wiring-gate test reads to confirm the Capability owns specialist firing when the flag
// is on.
func (e *SubconsciousEngine) PrimitiveSubAgentGate() PrimitiveSubAgentGate { return e.psaGate }

// SetSparseDispatch turns the SPARSEMAX admission over the base-specialist relevance field on/off
// (subconscious.dispatch.sparse). The engine wires it at construction from the live config flag. ON ⇒ the
// dispatch loop admits a base specialist iff its sparsemax mass p_i>0 AND eff>theta (θ as a floor under the
// induced τ), stamps p_i as the candidate's dispatch confidence, and emits subconscious.sparse. OFF (the
// default) ⇒ the legacy per-key absolute admitFire, byte-identical (no event). See the sparseDispatch field.
func (e *SubconsciousEngine) SetSparseDispatch(on bool) { e.sparseDispatch = on }

// SparseDispatch returns whether sparsemax admission is on — the read accessor a wiring-gate test reads to
// confirm the sparse path is the live admission when the flag is on.
func (e *SubconsciousEngine) SparseDispatch() bool { return e.sparseDispatch }

// SetSingleStrong turns on the SINGLE-STRONG-AGENT collapse: when on, Dispatch keeps only the single
// highest-effective-relevance fired candidate per tick (the "single strong agent" reference for the
// teams-vs-best-member guard, subconscious.single_strong_agent). false (the default) ⇒ the full fan-out
// reaches the gate, byte-identical. The engine refreshes this each tick from the live config so a TUI
// live-flip is honoured with no rebuild. See the singleStrong field.
func (e *SubconsciousEngine) SetSingleStrong(on bool) { e.singleStrong = on }

// SingleStrong reports whether the single-strong-agent collapse is on — the read accessor the wiring-gate
// test reads to confirm the engine wired the flag onto the subconscious engine (the live side of the wire).
func (e *SubconsciousEngine) SingleStrong() bool { return e.singleStrong }

// admitPrimitiveSubAgent is the dispatch loop's admission predicate, applied IDENTICALLY at all three sites (the
// serial loop + both concurrency pre-fires) so the default-ON parallel path stays byte-identical to serial.
// The relevance gate (eff>theta) is ALWAYS first and independent; the wired PrimitiveSubAgentGate (the producing
// Capability — subconscious.capability_specialists on) is the ADDITIONAL Capability-owned authority check
// layered on top, which can only DENY (never admit one relevance rejected). A nil gate (the default + OFF
// path) ⇒ the predicate is the bare eff>theta, byte-identical. Pattern-A pure CONTROL (string compare + θ).
func (e *SubconsciousEngine) admitPrimitiveSubAgent(domain string, eff, theta float64) bool {
	if eff <= theta {
		return false
	}
	if e.psaGate != nil {
		return e.psaGate.AdmitPrimitiveSubAgent(domain)
	}
	return true
}

// admitFire is the roster-member admission the dispatch loop applies, distinguishing the two firing
// populations the entry governs:
//
//   - a *SubAgent (a workflow's staffed worker, instantiated for the recognised phase) is NEVER specialist-
//     gated: it was authorised by the Capability's produce/staffing path (its tools resolve within the run's
//     Scope CATEGORY/skill ceiling), so admission is the bare eff>theta — gating it by DOMAIN would deny the
//     workflow's own slots. Byte-identical to before.
//   - a base SPECIALIST (anything else in the roster — social/skeptic/compute/recall/…) is what the redesign
//     means by "specialists firing on relevance"; THIS is the population the entry subsumes, so it goes
//     through admitPrimitiveSubAgent (the relevance gate AND, when the Capability owns specialist firing, its §3.3a
//     domain-band authority check). A nil gate ⇒ bare eff>theta, byte-identical.
func (e *SubconsciousEngine) admitFire(s PrimitiveSubAgent, eff, theta float64) bool {
	if _, isSub := s.(*SubAgent); isSub {
		return eff > theta // a workflow worker is authorised by produce/staffing, not the specialist gate
	}
	return e.admitPrimitiveSubAgent(s.Domain(), eff, theta)
}

// Workflow returns the currently-wired workflow (nil ⇒ none), the read side of the Python
// `self.subconscious.workflow` attribute the engine consults each tick.
func (e *SubconsciousEngine) Workflow() *Workflow { return e.workflow }

// Specialists returns a copy of the base roster (the persistent specialists, including any minted by
// convertibility). Read-only — the TUI registry browser reads it to list the live specialist roster;
// a copy is returned so a caller cannot mutate the shared base slice.
func (e *SubconsciousEngine) Specialists() []PrimitiveSubAgent {
	out := make([]PrimitiveSubAgent, len(e.specialists))
	copy(out, e.specialists)
	return out
}

// Dispatch runs one pull-dispatch pass: build the per-call-LOCAL roster (base specialists + this
// phase's instantiated SubAgents), score every specialist's bias-adjusted relevance, fire the ones
// over theta, and emit the scan. It returns the fired candidates and the gate bias the workflow
// applied (so the Gate downstream can use it). Mirrors Python dispatch.
//
// cognitionView mirrors Python's `cognition: object | None` — it is forwarded to the workflow's
// Instantiate so the cognition operators (rank/eliminate/decompose) compute against the live graph;
// nil ⇒ those sub-agents reason only.
func (e *SubconsciousEngine) Dispatch(ctx []types.Thought, theta float64,
	cognitionView *CognitiveView) ([]*types.Candidate, map[string]float64) {
	bias := map[string]float64{}
	// PER-CALL-LOCAL roster: copy the base specialists so a recognised workflow's ephemeral
	// SubAgents extend THIS call only — never the shared self.specialists (no shared-state race).
	// Mirrors Python `roster: list[PrimitiveSubAgent] = list(self.specialists)`.
	roster := make([]PrimitiveSubAgent, len(e.specialists))
	copy(roster, e.specialists)

	// preFired holds per-phase sub-agents whose Fire() was already run CONCURRENTLY (the default-ON
	// per-phase concurrency path, seam #1). nil ⇒ the serial path: every roster entry fires inline in the
	// loop below exactly as before. A non-nil entry carries the pre-computed candidate + its buffered events
	// to flush in index order — keyed by the *SubAgent pointer so the serial loop's per-domain ordering,
	// scan, and event stream stay byte-identical to the all-serial path.
	var preFired map[*SubAgent]preFiredResult

	// preFiredSpec holds BASE specialists whose Fire() was already run CONCURRENTLY (the default-ON per-tick
	// base-specialist fan-out, seam #2). nil/empty ⇒ every base specialist fires inline in the loop below.
	// Keyed by ROSTER INDEX (the position the loop walks) so the candidate is slotted back exactly where the
	// serial loop would have fired it — order/scan/event stream byte-identical. Only the reason-only model-call
	// specialists (parallelSafePrimitiveSubAgent: social/skeptic/advocate) are in it; pure (compute/recall/minted) and
	// effectful (read/search/run/solver) specialists are ABSENT ⇒ they fire serially exactly as before.
	var preFiredSpec map[int]preFiredResult

	// A recognised workflow biases the gate and instantiates this phase's ephemeral worker.
	// Recognize MUTATES the cached recognized flag (read by GateBias) — the ordering is load-bearing.
	//
	// GAP 5-DEEPER (subconscious.capability_dispatch): when a recognizer is wired, the producing Capability
	// OWNS this recognition decision (e.recognize routes through it) — the Capability is the live relevance/
	// dispatch ENTRY, not the self-triggering Workflow. The recognizer decides PERMISSIVELY with the graded
	// `gradedRelevance(stream)>0` has-any criterion (bespoke short-circuit preserved) — the SAME relevance
	// criterion as the binary path; it mutates the same recognized flag GateBias reads. theta is NOT
	// consulted at recognition (it is the downstream value/admission bar — gating recognition on θ is the
	// refuted double-gate, see Workflow.recognizeViaGraded). The recognition set therefore EQUALS the binary
	// has-any set, so a recognised phase fires exactly as often as the binary path. A nil recognizer (the
	// default + the flag-OFF path) ⇒ e.recognize falls back to wf.Recognize, the legacy binary self-trigger,
	// byte-identical.
	if e.workflow != nil && e.recognize(e.workflow, ctx, theta) {
		for k, v := range e.workflow.GateBias() {
			bias[k] = v
		}
		phase := e.workflow.Current()
		// Hand sub-agents the executor (so a scoped operator dispatches a tool for real) and the
		// cognition view (so rank/eliminate/decompose compute against the live graph).
		subAgents := e.workflow.Instantiate(phase, e.executor, cognitionView)
		for _, sa := range subAgents {
			roster = append(roster, sa)
		}
		e.emitWorkflow(phase)
		// Per-phase concurrency (flag-gated, EXPERIMENTAL): a Parallel group of >=2 reason-only
		// sub-agents may fire concurrently. Pre-computing here keeps the serial roster loop's order
		// (scan + fire events) byte-identical; only the model calls overlap. OFF ⇒ preFired stays nil.
		if parallelPhases && phase.Plan.Parallel {
			// theta + bias thread in so the pre-fire fires ONLY the serial-admitted set (gap #1) and
			// grants the background budget in index order (gap #2) — see preFireParallel.
			preFired = e.preFireParallel(ctx, subAgents, theta, bias)
		}
	}

	// SEAM #2 (per-tick base-specialist model-call fan-out, 07-OPTIMISATION-SURVEY.md §A.1 item 3): the
	// reason-only model-call base specialists admitted this tick (social/skeptic/advocate) fire their model
	// calls CONCURRENTLY here, BEFORE the serial roster loop, and are slotted back by roster index. Same
	// discipline as seam #1: theta-respecting (only the serial-admitted set), index-ordered background-budget
	// grant (fire-vs-defer a function of index, not goroutine completion), bounded by MaxParWidth + a
	// semaphore, RNG-free (so the seeded stream is untouched). nil/empty ⇒ every base specialist fires serially.
	if parallelPhases {
		preFiredSpec = e.preFirePrimitiveSubAgents(roster, ctx, theta, bias)
	}

	// SPARSE-DISPATCH (subconscious.dispatch.sparse, design §4): when on, compute the SPARSEMAX over the
	// base-specialist relevance field BEFORE the admission loop, so the per-key absolute eff>theta gate is
	// replaced by the competitive relative one (admit iff p_i>0 AND eff>theta). sd is nil-or-empty on the
	// OFF path ⇒ admission stays the legacy admitFire, byte-identical. The projection reads the SAME eff
	// the loop computes (effectiveRelevance over Relevance+bias), so the pre-pass and the loop agree.
	var sd *sparseAdmission
	if e.sparseDispatch {
		sd = e.computeSparseAdmission(roster, ctx, bias, theta)
	}

	fired := []*types.Candidate{}
	scan := []events.D{}
	for i, s := range roster {
		rel := s.Relevance(ctx)
		boost := bias[s.Domain()] // Python bias.get(s.domain, 0.0): a missing key is the 0.0 zero value
		eff := effectiveRelevance(rel, boost)
		entry := events.D{
			"domain":    s.Domain(),
			"relevance": round3(rel),
			"effective": round3(eff),
			"fired":     false,
		}
		// GAP 5-DEEPER PART 2: admission is the relevance gate (eff>theta) AND — when the producing
		// Capability owns specialist firing (subconscious.capability_specialists on) — its authority check
		// (the §3.3a Scope domain band). A nil gate ⇒ bare eff>theta, byte-identical. A SubAgent (a workflow
		// worker, *SubAgent) is NEVER gated here — the gate governs the relevance-fired SPECIALIST roster, not
		// the workflow's own staffed slots (those are authorised by the Capability's produce/staffing path).
		//
		// SPARSE-DISPATCH: when sd is non-nil, admitSparse replaces the per-key absolute eff>theta admission
		// for a BASE specialist with the competitive sparsemax one (p_i>0 AND eff>theta), θ surviving as a
		// floor under the induced τ. A *SubAgent worker is never sparsemax-gated (it keeps admitFire); sd is
		// nil on the OFF path ⇒ admission is exactly admitFire, byte-identical.
		if e.admitDispatch(i, s, eff, theta, sd) {
			c := e.fireRosterEntry2(i, s, ctx, preFired, preFiredSpec)
			if c != nil {
				c.Relevance = eff // stamp the bias-adjusted effective relevance (unrounded)
				if sd != nil {
					c.DispatchWeight = sd.weight(i) // stamp the sparsemax mass p_i (a V(s)/rerank prior)
				}
				fired = append(fired, c)
				entry["fired"] = true
				if sd != nil {
					entry["sparse_p"] = round3(sd.weight(i)) // the per-key mass, in the scan for the trace
				}
				e.emitFire(s.Domain(), eff, c)
			}
		}
		scan = append(scan, entry)
	}

	// SPARSE-DISPATCH observability: emit the competitive admission decision (the induced τ, the θ floor,
	// the support size, the per-key weights) so the relative gate is visible on the bus. Only on the ON
	// path; OFF ⇒ sd nil ⇒ no event ⇒ byte-identical.
	if sd != nil {
		e.emitSparse(sd, theta)
	}

	// SUB-AGENT GUARD (subconscious.single_strong_agent, docs/internal/notes/2026-06-21-sota-benchmark-suite.md §7.6):
	// when on, COLLAPSE the fired set to its single best member — the strongest fired candidate this tick
	// survives, every teammate is dropped BEFORE the candidates reach the gate. This is the "single strong
	// agent" reference the teams-vs-best-member guard runs the full harness against. OFF (the default) ⇒
	// fired is unchanged, byte-identical, no event. It strictly reduces the fired set (fan-out -> 1), so it
	// never raises the branching ratio n. The collapse happens AFTER the full scan/fire pass so the scan
	// trace (who lit up / who fired) and every fired specialist's event are byte-identical to the full path
	// up to this point — only WHICH survivors continue downstream changes.
	if e.singleStrong && len(fired) > 1 {
		fired = e.collapseToBestMember(fired)
	}

	if len(fired) > 0 {
		e.emitDispatch(len(fired), theta, scan)
	} else {
		e.emitQuiet(theta, scan)
	}
	return fired, bias
}

// admitDispatch is the dispatch loop's per-roster-member admission, routing to either the legacy absolute
// gate (admitFire) or the SPARSEMAX competitive gate, depending on whether sparse dispatch is on (sd != nil)
// AND the member is a relevance-fired BASE specialist:
//
//   - sd == nil (the OFF path) ⇒ exactly admitFire(s, eff, theta), byte-identical.
//   - a *SubAgent (a workflow's staffed worker) ⇒ always admitFire (the bare eff>theta) — it is authorised
//     by produce/staffing, NOT pulled on relevance, so the sparsemax competition (over the pulled specialist
//     field) never gates it. (This mirrors admitFire's own *SubAgent carve-out.)
//   - a base specialist with sd != nil ⇒ the sparsemax gate: admit iff p_i>0 (it survived the simplex
//     projection over the field) AND eff>theta (θ survives as a FLOOR under the induced τ, so a uniformly-
//     weak tick still goes quiet — the emitQuiet → Conscious-generates path is preserved). The §3.3a
//     PrimitiveSubAgentGate authority check still applies on top (deny-only), exactly as in admitPrimitiveSubAgent.
func (e *SubconsciousEngine) admitDispatch(i int, s PrimitiveSubAgent, eff, theta float64, sd *sparseAdmission) bool {
	if sd == nil {
		return e.admitFire(s, eff, theta)
	}
	if _, isSub := s.(*SubAgent); isSub {
		return e.admitFire(s, eff, theta) // a staffed worker is never sparsemax-gated
	}
	// Base specialist: the competitive gate. p_i>0 AND eff>theta (the θ floor under τ), then the deny-only
	// §3.3a Capability authority check (when wired) — never admit one the relevance/sparse gate rejected.
	if sd.weight(i) <= 0 || eff <= theta {
		return false
	}
	if e.psaGate != nil {
		return e.psaGate.AdmitPrimitiveSubAgent(s.Domain())
	}
	return true
}

// collapseToBestMember keeps only the single strongest fired candidate (the highest stamped effective
// Relevance) and drops every other admitted teammate — the single-strong-agent guard's "best member"
// reference. It is a closed-form argmax over the ALREADY-SCORED fired field (Pattern-A pure CONTROL, NO
// model): the candidates already carry the bias-adjusted effective relevance the dispatch loop stamped, so
// the best member is deterministic with no re-scoring. Ties break on the earliest fired index (the lower
// roster position) so the survivor is reproducible. It emits subconscious.single_strong recording the
// collapse (fired count, the kept domain, the dropped count) so the guard is observable. Caller guarantees
// len(fired) > 1, so a survivor always exists.
func (e *SubconsciousEngine) collapseToBestMember(fired []*types.Candidate) []*types.Candidate {
	best := fired[0]
	for _, c := range fired[1:] {
		if c.Relevance > best.Relevance {
			best = c
		}
	}
	e.emitSingleStrong(len(fired), best)
	return []*types.Candidate{best}
}

// recognize is the GAP 5-DEEPER relevance-entry seam: the per-tick recognition decision routes THROUGH
// the wired recognizer (the producing Capability — subconscious.capability_dispatch on) when present, so
// the Capability is the live entry that owns "does this workflow apply?". The recognizer decides
// PERMISSIVELY with the has-any criterion (gradedRelevance>0; bespoke short-circuit preserved) — the SAME
// relevance criterion as the legacy binary path. theta is threaded on the signature but NOT consulted at
// recognition (gating recognition on θ is the refuted double-gate — see Workflow.recognizeViaGraded; the
// θ/value bar is a downstream admission gate). With NO recognizer (the default + legacy path), it is exactly
// wf.Recognize(ctx) — the binary self-trigger, byte-identical, and theta is likewise unused. Both arms
// mutate wf's recognized flag, so the dispatch branch + GateBias read a consistent value.
func (e *SubconsciousEngine) recognize(wf *Workflow, ctx []types.Thought, theta float64) bool {
	if e.recognizer != nil {
		return e.recognizer.RecognizeWorkflow(wf, ctx, theta)
	}
	// LEGACY(redesign): the nil-recognizer fallback (subconscious.capability_dispatch OFF, or no producing
	// Capability) — the Workflow self-triggers via the legacy binary Recognize, and theta is unused (the
	// binary predicate has no θ gate) — removable when the 4 redesign flags are retired (the recognizer is
	// then always wired and RecognizeWorkflow is the only path).
	return wf.Recognize(ctx)
}

// effectiveRelevance is the bias-adjusted relevance the dispatch loop admits on: eff = min(1, rel+boost)
// only when rel>0, else rel (a dark specialist stays dark). Mirrors Python
// `min(1.0, rel + boost) if rel > 0 else rel`. Factored out so the serial loop AND the parallel
// pre-fire compute the SAME admission predicate (eff>theta) — that identical computation is what makes
// the pre-fire fire EXACTLY the serial-admitted set (gap #1), never an extra step.
func effectiveRelevance(rel, boost float64) float64 {
	if rel > 0 {
		return math.Min(1.0, rel+boost)
	}
	return rel
}

// preFiredResult is one sub-agent's pre-computed Fire() output under the per-phase concurrency path:
// the candidate it produced (nil ⇒ it fired nothing) plus the s.emit events buffered during its
// concurrent fire, to be flushed in index order at the serial loop's fire site.
type preFiredResult struct {
	candidate *types.Candidate
	events    []bufferedEvent
}

// goroutinePanic captures a panic that fired INSIDE a fan-out worker goroutine so it can be re-raised
// on the DISPATCHING goroutine after wg.Wait(), instead of crashing the whole process. A panic in a
// child goroutine is NOT recoverable by the parent's recover() — it unwinds that goroutine's stack and
// takes the process down (the silent "exit 1, no error" the bench saw: the per-arm safeArmRun recover
// runs on the worker goroutine, never on the engine's fan-out children). By recovering in the worker,
// stashing the value+stack here, and re-panicking on the dispatching goroutine, the panic becomes a
// SYNCHRONOUS one that unwinds Dispatch -> tick loop -> Run -> the bench's runHarnessEpisode and is
// finally caught by safeArmRun, where it is recorded as an ordinary ArmError (a logged FAIL cell) and
// the run CONTINUES — exactly the robustness contract. Deterministic: the re-panic carries the LOWEST-
// INDEX worker panic (panicCollector.firstByIndex), so which panic surfaces does NOT depend on goroutine
// completion timing.
type goroutinePanic struct {
	idx   int // the worker's branch index (the concurrent-set position) — used to pick the lowest deterministically
	value any
	stack string
}

// panicCollector gathers fan-out worker panics under a mutex (the workers race to it) and yields the
// LOWEST-INDEX one, so the surfaced panic is timing-independent. A zero value is ready to use.
type panicCollector struct {
	mu  sync.Mutex
	all []goroutinePanic
}

// record stashes one recovered worker panic (idx = the worker's branch index, p = the recovered value,
// stack = its debug.Stack()). Called from each worker's deferred recover.
func (c *panicCollector) record(idx int, p any, stack string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.all = append(c.all, goroutinePanic{idx: idx, value: p, stack: stack})
}

// firstByIndex returns the captured panic with the LOWEST branch index (timing-independent), and ok=false
// when no worker panicked. No lock needed: the workers have all joined (wg.Wait) before the caller reads.
func (c *panicCollector) firstByIndex() (goroutinePanic, bool) {
	if len(c.all) == 0 {
		return goroutinePanic{}, false
	}
	best := c.all[0]
	for _, gp := range c.all[1:] {
		if gp.idx < best.idx {
			best = gp
		}
	}
	return best, true
}

// repanic re-raises the captured fan-out worker panic on the DISPATCHING goroutine (after wg.Wait),
// preserving the original value and the worker's stack head so safeArmRun's ArmError carries the real
// crash site. No-op when no worker panicked. It re-panics the lowest-index value, annotated so the
// trail back to the fan-out goroutine is not lost.
func (c *panicCollector) repanic() {
	gp, ok := c.firstByIndex()
	if !ok {
		return
	}
	panic(fmt.Sprintf("subconscious fan-out worker panic: %v\n(worker goroutine stack)\n%s",
		gp.value, gp.stack))
}

// fireRosterEntry fires one roster member at the serial loop's fire site. For a base specialist (or
// any non-pre-fired entry) this is the unchanged `s.Fire(ctx, e.rng)` — the shared RNG is consumed in
// order, so base-specialist behaviour is byte-identical. For a *SubAgent whose Fire() was already run
// concurrently (preFired), it returns the pre-computed candidate AND flushes that branch's buffered
// events onto the bus HERE, in the loop's deterministic order — so the trace is identical to serial.
//
// Retained for the budget/parity tests that pin seam #1 directly (the *SubAgent slot path). The dispatch
// loop now calls fireRosterEntry2, which also consults the seam-#2 base-specialist map.
func (e *SubconsciousEngine) fireRosterEntry(s PrimitiveSubAgent, ctx []types.Thought,
	preFired map[*SubAgent]preFiredResult) *types.Candidate {
	return e.fireRosterEntry2(-1, s, ctx, preFired, nil)
}

// fireRosterEntry2 fires one roster member at the serial loop's fire site, consulting BOTH concurrency
// seams. idx is the roster index (the seam-#2 key); pass -1 when no base-specialist map is in play.
//
//   - seam #1 (preFired, keyed by *SubAgent pointer): a pre-fired sub-agent returns its pre-computed
//     candidate AND flushes its buffered events here, in the loop's deterministic order.
//   - seam #2 (preFiredSpec, keyed by roster INDEX): a pre-fired BASE specialist returns its pre-computed
//     candidate. Its Fire makes NO direct bus emit (parallelSafePrimitiveSubAgent property (c)), so there is
//     nothing to flush — the dispatch-level subconscious.fire is emitted by the loop AFTER this returns,
//     in index order, exactly as serial. So the trace is byte-identical without any buffering.
//   - otherwise: the unchanged `s.Fire(ctx, e.rng)` — base specialists keep the shared RNG to themselves,
//     consumed in roster order, so behaviour is byte-identical.
//
// The two maps are disjoint by construction: seam #1 only ever holds *SubAgent entries, seam #2 only ever
// holds base-specialist (non-*SubAgent) indices, so a roster member is in at most one.
func (e *SubconsciousEngine) fireRosterEntry2(idx int, s PrimitiveSubAgent, ctx []types.Thought,
	preFired map[*SubAgent]preFiredResult, preFiredSpec map[int]preFiredResult) *types.Candidate {
	if preFired != nil {
		if sa, ok := s.(*SubAgent); ok {
			if pr, found := preFired[sa]; found {
				for _, be := range pr.events {
					sa.emit(be.kind, be.summary, be.data) // flush in index order at this site
				}
				return pr.candidate
			}
		}
	}
	if preFiredSpec != nil && idx >= 0 {
		if pr, found := preFiredSpec[idx]; found {
			return pr.candidate // a pre-fired base specialist makes no in-Fire emit ⇒ nothing to flush
		}
	}
	return s.Fire(ctx, e.rng)
}

// preFireParallel runs the REASON-ONLY sub-agents of a Parallel phase-group CONCURRENTLY and returns
// their pre-computed results keyed by pointer (EXPERIMENTAL; the flag default is OFF). Determinism is
// preserved because:
//
//   - It is entered ONLY when every sub-agent in the group is reason-only (no executor/tool path, no
//     cognition-exec path). A group with an effectful/cognition sub-agent returns nil ⇒ the whole group
//     fires serially in the loop, exactly as before (the conservative, always-correct fallback).
//   - GAP #1 (pre-fire respects theta): it pre-fires ONLY the sub-agents the serial loop would admit
//     (effectiveRelevance(rel,boost) > theta) — the SAME predicate the dispatch loop applies — so the
//     parallel path never makes an extra model call nor spends background budget on a step serial skips.
//     A sub-agent below theta is left OUT of the returned map ⇒ the serial loop sees no pre-fired entry
//     and, because eff<=theta, never fires it either (byte-identical to serial: it is skipped both ways).
//   - GAP #2 (deterministic background-budget grant in INDEX order): the reason path's backend call is a
//     BACKGROUND role (operator.*), so a fan-out wider than the per-tick budget defers some calls — and
//     WHICH defer must not depend on goroutine completion order. We size the concurrent set to the
//     remaining budget UP FRONT, first-k of the theta-admitted set in step-INDEX order: those k fire
//     concurrently (all granted — the budget covers them by construction, and nothing else grants
//     between the read and wg.Wait since the dispatching tick thread is parked there), and the [k:]
//     remainder are left OUT of the map ⇒ they fall to the SERIAL loop, where the now-exhausted budget
//     denies them deterministically (the backend returns "" ⇒ the fallback candidate — exactly what
//     budget exhaustion produces serially). So fire-vs-defer is a function of INDEX, never timing.
//   - Each branch's Fire() is buffered (fireBuffered) so it never touches the shared bus in its
//     goroutine; the buffers are replayed in step-INDEX order at the serial loop, so the trace matches.
//   - The reason path consumes NO RNG (SubAgent.Fire ignores its rng arg), so completion order cannot
//     reorder the seeded stream — the base specialists keep the shared e.rng entirely to themselves.
//   - Concurrency is bounded by MaxParWidth AND a semaphore (cap = min(MaxParWidth, len)), so the GPU /
//     backend is never oversubscribed beyond the documented fan-out budget.
//
// NOTE the documented semantic difference vs pure-serial under budget pressure: the pre-fired sub-agents
// consume the background budget BEFORE the roster's base specialists (serial consumes strictly in roster
// order, base specialists first). Deterministic both ways, but the allocation PRIORITY differs when a
// base specialist also makes a background call in the same tick; the flag is opt-in and gated on the
// lift ruler. (In the common parallel-group fixture no base specialist fires, so the two coincide.)
//
// A group with <2 admitted sub-agents needs no concurrency (returns nil ⇒ serial — a single call is not
// faster, and 0 admitted is nothing to do).
func (e *SubconsciousEngine) preFireParallel(ctx []types.Thought, subAgents []*SubAgent,
	theta float64, bias map[string]float64) map[*SubAgent]preFiredResult {
	if len(subAgents) < 2 {
		return nil
	}
	for _, sa := range subAgents {
		if !sa.reasonOnly() {
			return nil // a non-reason branch ⇒ keep the whole group serial (un-buffered effects)
		}
	}
	// GAP #1: restrict the concurrent set to the steps the serial loop would actually dispatch — the
	// eff>theta admission set, computed with the IDENTICAL predicate (effectiveRelevance). Preserve
	// step-INDEX order (the append order the roster loop walks) so the budget grant below is by index.
	admitted := make([]*SubAgent, 0, len(subAgents))
	for _, sa := range subAgents {
		rel := sa.Relevance(ctx)
		// admitFire so all three admission sites share ONE predicate. A *SubAgent (a workflow worker) is
		// never specialist-gated (admitFire returns the bare eff>theta for it), so this is byte-identical to
		// the prior `effectiveRelevance>theta` — the specialist gate governs the relevance-fired specialist
		// roster (seam #2 / the serial loop), not the workflow's own staffed slots.
		if e.admitFire(sa, effectiveRelevance(rel, bias[sa.Domain()]), theta) {
			admitted = append(admitted, sa)
		}
	}
	if len(admitted) < 2 {
		return nil // 0/1 admitted ⇒ no concurrency worth having; the serial loop handles all
	}
	// GAP #2: of the admitted set, only the first-k (by index) get the background budget; the [k:]
	// remainder fall to the serial loop's deterministic denial. k<2 ⇒ no concurrency worth having.
	concurrent := admitted
	if e.sched != nil {
		k := e.sched.BackgroundRemaining()
		if k < len(admitted) {
			if k < 2 {
				return nil // 0/1 grantable ⇒ let the serial loop fire+deny all in roster order
			}
			concurrent = admitted[:k]
		}
	}
	// FAN-OUT BOUND (durability hard constraint): the concurrent CALL SET never exceeds MAX_PAR_WIDTH
	// (=8). The program scheduler already enforces len(Par.Children) <= MaxParWidth (program.checkPar),
	// so concurrent is <=8 by construction here; this is the explicit, self-contained backstop so the
	// concurrency path itself bounds the plant's fan-out — the control-theory gate re-passes on it. Any
	// admitted steps past the cap fall to the serial loop (deterministic by index, exactly like a budget
	// deferral). MaxParWidth<1 (operator error) ⇒ no concurrency.
	maxWidth := cognition.MaxParWidth
	if maxWidth < 1 {
		return nil
	}
	if len(concurrent) > maxWidth {
		concurrent = concurrent[:maxWidth]
		if len(concurrent) < 2 {
			return nil
		}
	}
	results := make([]preFiredResult, len(concurrent))
	width := maxWidth
	if width > len(concurrent) {
		width = len(concurrent)
	}
	sem := make(chan struct{}, width) // bound concurrency to the fan-out budget; never oversubscribe
	var (
		wg     sync.WaitGroup
		panics panicCollector
	)
	for i, sa := range concurrent {
		wg.Add(1)
		go func(i int, sa *SubAgent) {
			defer wg.Done()
			// RECOVER per worker: a panic inside a child goroutine is NOT caught by the dispatching
			// goroutine's recover and would crash the WHOLE process (the bench's silent "exit 1, no
			// error" at the single-strong arm). Capture it here and re-raise it synchronously on the
			// dispatching goroutine after wg.Wait so it unwinds back to the bench's safeArmRun as an
			// ordinary FAIL. (The sem release in this same defer stack still runs.)
			defer func() {
				if r := recover(); r != nil {
					panics.record(i, r, string(debug.Stack()))
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			// The reason path consumes no RNG, so the rng arg is unused; pass nil rather than share the
			// engine's stream across goroutines (which would be a data race even though unread).
			c, buf := sa.fireBuffered(ctx, nil)
			results[i] = preFiredResult{candidate: c, events: buf}
		}(i, sa)
	}
	wg.Wait()
	panics.repanic() // a worker panic -> a SYNCHRONOUS panic the caller's recover catches (never a process crash)
	// COLLECT in deterministic step-index order: the map is keyed by pointer, but the loop that reads it
	// walks the roster (= subAgents) in append order, so every read is index-deterministic. Only the
	// concurrently-fired (admitted ∩ first-k) sub-agents are in the map; theta-skipped and budget-deferred
	// ones are ABSENT ⇒ the serial loop handles them exactly as it would with the flag off.
	out := make(map[*SubAgent]preFiredResult, len(concurrent))
	for i, sa := range concurrent {
		out[sa] = results[i]
	}
	return out
}

// specRosterEntry pairs a roster index with the parallel-safe base specialist at that position — the
// unit the per-tick base-specialist fan-out (seam #2) collects + slots back by INDEX. The index is the
// position in the dispatch roster's serial walk, so the candidate replaces exactly the serial fire.
type specRosterEntry struct {
	idx  int
	spec PrimitiveSubAgent
}

// preFirePrimitiveSubAgents runs the REASON-ONLY MODEL-CALL base specialists of THIS tick CONCURRENTLY and
// returns their pre-computed candidates keyed by ROSTER INDEX (07-OPTIMISATION-SURVEY.md §A.1 item 3,
// seam #2 — the per-tick base-specialist "fire on relevance" fan-out). It is the base-specialist twin of
// preFireParallel (the seam #1 Par sub-agent fan-out), with the IDENTICAL determinism discipline:
//
//   - SAFE SET ONLY: a base specialist joins the concurrent set only if it implements parallelSafePrimitiveSubAgent
//     (social/skeptic/advocate — RNG-free, effect-free, bus-silent-in-Fire, one background model call). A
//     SubAgent is NEVER in this set (seam #1 owns the workflow sub-agents — they are filtered out here so a
//     roster member is pre-fired by at most one seam). Pure specialists (compute/recall/minted, no model
//     call) and effectful ones (read/search/run/solver — sandbox + un-buffered action.* / solver_formalize
//     emits) are left to the serial loop, byte-identical.
//   - GAP #1 (respect theta): only specialists the serial loop would admit (effectiveRelevance>theta, the
//     SAME predicate) are pre-fired — so the parallel path never makes an extra model call. A below-theta
//     specialist is ABSENT from the map ⇒ the serial loop sees no slot and (eff<=theta) never fires it.
//   - GAP #2 (deterministic budget grant in INDEX order): the backend.Specialist call is a BACKGROUND role
//     ("specialist.<domain>"), so a fan-out wider than the per-tick budget defers some. We size the concurrent
//     set to the remaining background budget UP FRONT, first-k of the theta-admitted set in ROSTER-INDEX order:
//     those k fire concurrently (all granted — the budget covers them, and nothing else grants between the read
//     and wg.Wait since the tick thread is parked there), the remainder fall to the SERIAL loop where the now-
//     exhausted budget denies them deterministically (backend returns ("",false) ⇒ the specialist fires nothing,
//     exactly its serial budget-exhausted outcome). Fire-vs-defer is a function of index, never timing.
//   - Each branch's Fire makes NO direct bus emit (parallelSafePrimitiveSubAgent property (c)), so — unlike the
//     SubAgent path — there is nothing to buffer: the dispatch-level subconscious.fire is emitted by the
//     serial loop AFTER the slot returns, in index order. So the trace is byte-identical with no buffering.
//   - The reason path consumes NO RNG (every parallelSafePrimitiveSubAgent's Fire ignores its rng arg), so completion
//     order cannot reorder the seeded stream — the base specialists' rng is never touched by the concurrent set
//     (nil is passed, never the shared e.rng, which would be a data race even though unread).
//   - Concurrency is bounded by MaxParWidth AND a semaphore (cap = min(MaxParWidth, len)), so the backend /
//     GPU is never oversubscribed beyond the documented fan-out budget (the durability hard constraint).
//
// roster is the full per-call roster (base specialists + this phase's SubAgents); bias is the workflow's
// gate bias so the admission predicate matches the serial loop's exactly. A set of <2 admitted+grantable
// safe specialists needs no concurrency (returns nil ⇒ the serial loop fires all — a single model call is
// not faster concurrently).
func (e *SubconsciousEngine) preFirePrimitiveSubAgents(roster []PrimitiveSubAgent, ctx []types.Thought,
	theta float64, bias map[string]float64) map[int]preFiredResult {
	// SAFE SET, in roster-INDEX order: the parallel-safe (reason-only model-call) base specialists the serial
	// loop would admit (eff>theta). A SubAgent is excluded (seam #1 owns it); an unmarked specialist is excluded
	// (serial-by-default). Preserving index order makes the budget grant below by index (gap #2).
	admitted := make([]specRosterEntry, 0, len(roster))
	for i, s := range roster {
		if _, isSub := s.(*SubAgent); isSub {
			continue // seam #1 territory — never double-fire a sub-agent here
		}
		if _, safe := s.(parallelSafePrimitiveSubAgent); !safe {
			continue // pure or effectful specialist ⇒ serial (byte-identical)
		}
		rel := s.Relevance(ctx)
		eff := effectiveRelevance(rel, bias[s.Domain()])
		// GAP 5-DEEPER PART 2 (D1-collision care): the concurrent set must match the serial loop's admission
		// EXACTLY, so the SAME specialist gate (admitFire ⇒ admitPrimitiveSubAgent for a base specialist) decides here.
		// A specialist the Capability DENIES is left OUT of the concurrent set AND the serial loop's admitFire
		// denies it too — so it fires NOWHERE, byte-identical to serial. With no gate wired this reduces to the
		// prior eff>theta, so the default-ON parallel path is unchanged.
		if e.admitFire(s, eff, theta) {
			admitted = append(admitted, specRosterEntry{idx: i, spec: s})
		}
	}
	if len(admitted) < 2 {
		return nil // 0/1 admitted ⇒ no concurrency worth having; the serial loop fires all
	}
	// GAP #2: of the admitted set, only the first-k (by index) get the background budget; the [k:] remainder
	// fall to the serial loop's deterministic denial. k<2 ⇒ no concurrency worth having.
	concurrent := admitted
	if e.sched != nil {
		k := e.sched.BackgroundRemaining()
		if k < len(admitted) {
			if k < 2 {
				return nil // 0/1 grantable ⇒ let the serial loop fire+deny all in roster order
			}
			concurrent = admitted[:k]
		}
	}
	// FAN-OUT BOUND (durability hard constraint): the concurrent CALL SET never exceeds MAX_PAR_WIDTH (=8).
	// The safe set is small (today social/skeptic/advocate = 3 ≤ 8), but bound it explicitly so the seam
	// itself caps the plant's fan-out — the control-theory gate re-passes on it. Any admitted specialists past
	// the cap fall to the serial loop (deterministic by index, exactly like a budget deferral). MaxParWidth<1
	// (operator error) ⇒ no concurrency.
	maxWidth := cognition.MaxParWidth
	if maxWidth < 1 {
		return nil
	}
	if len(concurrent) > maxWidth {
		concurrent = concurrent[:maxWidth]
		if len(concurrent) < 2 {
			return nil
		}
	}
	results := make([]preFiredResult, len(concurrent))
	width := maxWidth
	if width > len(concurrent) {
		width = len(concurrent)
	}
	sem := make(chan struct{}, width) // bound concurrency to the fan-out budget; never oversubscribe
	var (
		wg     sync.WaitGroup
		panics panicCollector
	)
	for j, entry := range concurrent {
		wg.Add(1)
		go func(j int, s PrimitiveSubAgent) {
			defer wg.Done()
			// RECOVER per worker: a child-goroutine panic is not caught by the dispatching goroutine's
			// recover and would crash the whole process. Capture it and re-raise it synchronously after
			// wg.Wait (see preFireParallel for the full rationale — this is its base-specialist twin).
			defer func() {
				if r := recover(); r != nil {
					panics.record(j, r, string(debug.Stack()))
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			// The reason path consumes no RNG (parallelSafePrimitiveSubAgent ignores it); pass nil rather than share
			// the engine's stream across goroutines (a data race even though unread). No bus emit happens inside
			// Fire (property (c)), so there is nothing to buffer.
			results[j] = preFiredResult{candidate: s.Fire(ctx, nil)}
		}(j, entry.spec)
	}
	wg.Wait()
	panics.repanic() // a worker panic -> a SYNCHRONOUS panic the caller's recover catches (never a process crash)
	// COLLECT keyed by ROSTER INDEX: the dispatch loop walks the roster by index, so every read is index-
	// deterministic. Only the concurrently-fired (admitted ∩ first-k ∩ width-cap) specialists are in the map;
	// theta-skipped, budget-deferred, and over-cap ones are ABSENT ⇒ the serial loop handles them exactly as
	// it would with the flag off.
	out := make(map[int]preFiredResult, len(concurrent))
	for j, entry := range concurrent {
		out[entry.idx] = results[j]
	}
	return out
}

// --- emit helpers (kept byte-identical to the Python emit sites) ----------------------------------

// emitWorkflow emits subconscious.workflow for the recognised phase. Summary + data mirror Python's
// SUB_WORKFLOW emit (workflow/phase/operator/op_name/parallel keys; the " [parallel]" suffix). The
// phase index reads the workflow's cursor i directly (same package; Python self.workflow.i).
func (e *SubconsciousEngine) emitWorkflow(phase Phase) {
	if e.emit == nil {
		return
	}
	i := e.workflow.i
	// Python summary interpolates phase.operator.name (the UPPERCASE enum member name), not op_name.
	summary := "workflow '" + e.workflow.Name + "' phase " + strconv.Itoa(i) + " (" + phase.Operator.String() + ")"
	if phase.Plan.Parallel {
		summary += " [parallel]"
	}
	e.emit(events.SubWorkflow, summary, events.D{
		"workflow": e.workflow.Name,
		"phase":    i,
		"operator": phase.Operator.String(), // Python phase.operator.name (the enum member NAME, uppercase)
		"op_name":  phase.OpName,            // Python phase.op_name (the lowercase operator string)
		"parallel": phase.Plan.Parallel,
	})
}

// emitFire emits subconscious.fire for one fired specialist. Summary uses the .2f effective and the
// first 48 runes of the candidate text (Python c.text[:48]); data carries the UNROUNDED relevance
// (Python relevance=eff), text, and stance ("" ⇒ Python None on the wire).
func (e *SubconsciousEngine) emitFire(domain string, eff float64, c *types.Candidate) {
	if e.emit == nil {
		return
	}
	summary := domain + " fired (" + format2f(eff) + "): " + clipRunes(c.Text, 48)
	e.emit(events.SubFire, summary, events.D{
		"domain":    domain,
		"relevance": eff,
		"text":      c.Text,
		"stance":    stanceWire(c.Stance),
	})
}

// emitSparse emits subconscious.sparse — the SPARSEMAX competitive admission decision (design §4). Carries
// the induced threshold τ, the surviving θ floor, the support size (specialists with p_i>0), the scored
// field size, and the per-base-specialist weights ({domain, eff, p}) in roster order. round3 the floats so
// the trace matches the round(.,3) discipline of the scan. Emitted ONLY on the sparse-ON path; nil bus or
// nil sd ⇒ silent.
func (e *SubconsciousEngine) emitSparse(sd *sparseAdmission, theta float64) {
	if e.emit == nil || sd == nil {
		return
	}
	weights := make([]events.D, len(sd.scan))
	for i, r := range sd.scan {
		weights[i] = events.D{
			"domain": r.domain,
			"eff":    round3(r.eff),
			"p":      round3(r.p),
		}
	}
	summary := "sparsemax admission: " + strconv.Itoa(sd.support) + " of " + strconv.Itoa(sd.candidates) +
		" specialist(s) (τ=" + format2f(sd.tau) + ", floor θ=" + format2f(theta) + ")"
	e.emit(events.SubSparse, summary, events.D{
		"tau":        sd.tau,
		"floor":      theta,
		"support":    sd.support,
		"admitted":   sd.support, // |p_i>0|; the θ floor may reduce the final fired set (see scan "fired")
		"candidates": sd.candidates,
		"weights":    weights,
	})
}

// emitSingleStrong emits subconscious.single_strong when the single-strong-agent guard collapsed a
// multi-member fired set to its best member. firedCount is the number admitted BEFORE the collapse, best
// is the surviving candidate (the strongest by stamped effective relevance). dropped = firedCount-1 is the
// number of teammates discarded — the size of the "team" the guard is collapsing away. Pure CONTROL: this
// is the observability of a closed-form argmax (no model). Only fires when a teammate was actually dropped
// (the caller guarantees firedCount>1).
func (e *SubconsciousEngine) emitSingleStrong(firedCount int, best *types.Candidate) {
	if e.emit == nil {
		return
	}
	kept := candidateDomain(best)
	summary := "single-strong collapse: kept " + kept + " (" + format2f(best.Relevance) + "), dropped " +
		strconv.Itoa(firedCount-1) + " teammate(s)"
	e.emit(events.SubSingleStrong, summary, events.D{
		"fired":   firedCount,
		"kept":    kept,
		"dropped": firedCount - 1,
	})
}

// candidateDomain reads a candidate's producing-specialist domain off its *string Domain field (nil ⇒
// "" ⇒ the Python None on the wire), for the single-strong guard's "kept" key.
func candidateDomain(c *types.Candidate) string {
	if c == nil || c.Domain == nil {
		return ""
	}
	return *c.Domain
}

// emitDispatch emits subconscious.dispatch when at least one specialist fired. theta is UNROUNDED on
// the wire (Python theta=theta); scan entries already carry their round(.,3) relevance/effective.
func (e *SubconsciousEngine) emitDispatch(count int, theta float64, scan []events.D) {
	if e.emit == nil {
		return
	}
	summary := strconv.Itoa(count) + " specialist(s) fired (θ=" + format2f(theta) + ")"
	e.emit(events.SubDispatch, summary, events.D{
		"count": count,
		"theta": theta,
		"scan":  scan,
	})
}

// emitQuiet emits subconscious.quiet when no specialist fired (⇒ Conscious will generate). theta is
// UNROUNDED on the wire (Python theta=theta).
func (e *SubconsciousEngine) emitQuiet(theta float64, scan []events.D) {
	if e.emit == nil {
		return
	}
	summary := "no specialist fired (θ=" + format2f(theta) + ") -> Conscious will generate"
	e.emit(events.SubQuiet, summary, events.D{
		"theta": theta,
		"scan":  scan,
	})
}

// stanceWire maps a *string stance to its wire value: the dereferenced string, or nil for the Python
// None (an absent stance ⇒ JSON null, matching Python c.stance being None).
func stanceWire(stance *string) any {
	if stance == nil {
		return nil
	}
	return *stance
}
