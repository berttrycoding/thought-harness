package realhard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// webSearchEdge, when true, makes the HARNESS arm enable the subconscious.web_search flag AND wire a
// real web.DuckDuckGo seam (the GAIA / web-lookup enabler). It is a process-wide EDGE switch set ONCE
// by cmd/realhard's --web flag (EnableWebSearch) BEFORE the suite runs, so it is read-only during the
// concurrent run (no data race). Default false ⇒ the harness arm is byte-identical to the pre-flag
// path (web-blind, no web_search registration/dispatch, no network). It is the bench-edge equivalent of
// the CLI/TUI edge wiring SetWeb(web.NewWall()) — the engine itself never constructs a network seam.
var webSearchEdge bool

// EnableWebSearch flips the process-wide web-search EDGE on for the harness arm (cmd/realhard --web /
// THOUGHT_WEB_SEARCH=1). Call it ONCE before RunAB/RunSuite. It wires a LIVE DuckDuckGo seam, so the
// caller is opting into real network reads + cost (GAIA web-lookup tasks); the offline suite/tests
// never call it (they inject web.Fake at the unit level, never this live edge).
func EnableWebSearch(on bool) { webSearchEdge = on }

// arms.go — the two-arm runner for the headroom proof.
//
//   - ARM A = BARE: the base model answering directly, ONE Generate call, no
//     graph/Controller/seams/regulator/gate AND NO TOOLS (no workspace). This is
//     the "claude -p"-equivalent reference — exactly runner.runBare's shape. It
//     gets the SAME prompt as the harness arm; it just has no way to actually
//     read the materials, so on a multi-hop/grounding task it must guess.
//   - ARM B = HARNESS: the SAME prompt through the full reactive engine
//     (config.New() = AllOn) with the task's Materials materialized into a fresh
//     per-run workspace, so the read tools can ground step by step.
//
// Both arms are scored by the SAME deterministic Score(). The backend factory is
// injected so the suite runs free on the test double and live on claude.

// Arm names.
//
//   - ArmBare      — the raw model, ONE Generate call, no scaffold (RunBare).
//   - ArmHarness   — the full scaffold (AllOn): MAY dispatch a TEAM of sub-agents.
//   - ArmSingleStrong (subagentguard.go) — the full scaffold with sub-agent FAN-OUT
//     DISABLED (Subconscious.SubAgents off, MaxParWidth 1): the strongest SINGLE
//     agent the harness can field, the baseline the team (harness) arm must BEAT for
//     the sub-agent-beats-best-member guard to PASS (RunSingleStrong).
const (
	ArmBare    = "bare"
	ArmHarness = "harness"
)

// DefaultMaxTicks bounds a harness episode (the engine breaks early on
// quiescence; this is the safety budget — mirrors runner.DefaultMaxTicks).
const DefaultMaxTicks = 60

// DefaultTemperature is the fixed cross-arm temperature (the test double + claude
// ignore it; kept for parity with the bench runner). Claude temp is not
// controllable on the bridge.
const DefaultTemperature = 0.2

// BackendFactory builds a fresh backend per arm-run so each run's call tally
// starts at zero. Mirrors runner.BackendFactory exactly so the same claude/test
// factories drop in.
type BackendFactory func(seed int64, temp float64) backends.Backend

// RunResult is one (task, arm, replay) outcome.
type RunResult struct {
	TaskID     string
	Capability Capability
	Arm        string
	Replay     int
	Seed       int64
	Answer     string
	Verdict    Verdict
	// Value is the harness arm's active-line V(s) at episode end (eng.ActiveValue) —
	// the EXISTING value signal, captured so the deliberative reconciliation
	// (THOUGHT_DELIBERATIVE_K) can use it as the majority-vote TIE-BREAK rank key. 0
	// for the bare arm (no engine). Never a new scorer; never used by the oracle.
	Value float64
	// ModelCalls counts llm.call events for the harness arm (1 for bare). Grounded
	// reports whether the harness imported any reality observation (a read landed).
	ModelCalls int
	Grounded   bool
	// ToolSelectEscalations / ForceGroundEscalations count the grounding-fix
	// escalation events the harness arm emitted this run (escalation.tool_select =
	// the model-assisted grounding-chain tool pick; escalation.force_ground = the
	// force-a-read-before-give-up). Captured so a FUTURE A/B can confirm the
	// grounding fix actually ENGAGED on the live tick (the gap that muddied the
	// CAP-EVAL gate — we could not tell whether the fix fired). 0 for the bare arm
	// (no engine) and 0 on the offline test double (no escalation path exercised).
	ToolSelectEscalations  int
	ForceGroundEscalations int

	// Graded is the OPTIONAL answer-derived graded outcome in [0,1], populated ONLY
	// for OracleDecline (anti-confabulation) tasks from the DECLINE-ORDINAL scorer
	// (oracle_graded.go: clean honest decline = 1.0, a committed/confabulated number =
	// 0.0, ambiguous/empty/hedge = 0.0 strict). HasGraded gates it: false for every
	// non-decline task (no graded signal is defined there) and for both arms it is the
	// SAME scorer over the SAME final answer the binary oracle reads — so it is
	// arm-orthogonal (it never rewards tool-use / the harness treatment). The Bernoulli
	// estimator's graded path (BernTaskInput.{GradedMean,GradedSD,GradedN}) consumes it
	// behind the leakage guard (bernoulli.go); nothing populated it before this.
	Graded    float64
	HasGraded bool
}

// RunBare runs ARM A: a single Generate call on a fresh backend, no tools, no
// engine. The prompt is the task prompt verbatim — the honest "model alone"
// input. (The materials are NOT provided to bare: bare has no read tools, just
// as a raw claude -p with --tools "" cannot read the repo.)
func RunBare(t Task, factory BackendFactory, seed int64) RunResult {
	be := factory(seed, DefaultTemperature)
	rng := cpyrand.New(uint64(seed))
	text := strings.TrimSpace(be.Generate(t.Prompt, []types.Thought{}, rng))
	calls := 1
	if c, ok := backendCalls(be); ok {
		calls = c
	}
	graded, hasGraded := declineOrdinal(t, text)
	return RunResult{
		TaskID:     t.ID,
		Capability: t.Capability,
		Arm:        ArmBare,
		Seed:       seed,
		Answer:     text,
		Verdict:    Score(t, text),
		ModelCalls: calls,
		Grounded:   false,
		Graded:     graded,
		HasGraded:  hasGraded,
	}
}

// RunHarness runs ARM B: the full engine over a fresh workspace materialized
// with the task's files. Returns the engine's last response scored by the SAME
// oracle. workspaceRoot is a writable parent dir; a fresh per-run subdir is
// created and removed.
//
// ROBUSTNESS lever (THOUGHT_DELIBERATIVE_K): when K>1, the harness arm runs K
// INDEPENDENT trajectories (a fresh engine over a fresh workspace on a distinct,
// deterministic per-sample seed each) and reconciles their final answers by
// majority vote (V(s) tie-break) — the answer scored is the reconciled one. K==1
// (the default) is BYTE-IDENTICAL to the pre-flag single-episode path. This is
// the ONLY arms.go change for the lever: one branch on the resolved K.
func RunHarness(t Task, factory BackendFactory, seed int64, maxTicks int, workspaceRoot string) (RunResult, error) {
	if maxTicks <= 0 {
		maxTicks = DefaultMaxTicks
	}
	k := engine.DeliberativeK()
	if k <= 1 {
		// K==1: the single-episode path, untouched (byte-identical).
		return runHarnessEpisode(t, factory, seed, maxTicks, workspaceRoot, ArmHarness, nil)
	}
	// K>1: K independent trajectories reconciled by self-consistency. Each sample is a full,
	// fully-isolated episode on its own deterministic seed; the reconciliation adopts the majority
	// (V(s) tie-break) as the scored answer. The reconciliation events are dropped here (no bus is
	// threaded to the suite reducer); the engine's RunDeliberative emits them when an emit is wired
	// (the engine cognition-property tests assert on them).
	var (
		agg     RunResult
		anyErr  error
		samples = make([]RunResult, 0, k)
	)
	// The vote-equivalence key is the TASK-AWARE canonicalAnswer (it mirrors the oracle's notion of
	// "same answer" so K episodes that reach the same conclusion in different phrasings form a real
	// majority — NOT the coarse engine default, which split them into a spurious tie and degenerated
	// the lever to best-of-N-by-V(s)).
	normalize := func(a string) string { return canonicalAnswer(t, a) }
	res, err := engine.RunDeliberative(k, seed, nil, normalize, func(i int, sampleSeed int64) (engine.DeliberativeSample, error) {
		rr, e := runHarnessEpisode(t, factory, sampleSeed, maxTicks, workspaceRoot, ArmHarness, nil)
		if e != nil {
			anyErr = e
			return engine.DeliberativeSample{}, e
		}
		samples = append(samples, rr)
		return engine.DeliberativeSample{Seed: sampleSeed, Answer: rr.Answer, Value: rr.Value}, nil
	})
	if err != nil {
		if anyErr != nil {
			return RunResult{}, anyErr
		}
		return RunResult{}, err
	}
	// Adopt the reconciled answer; aggregate the per-sample telemetry (sum the calls/escalations over
	// the K episodes, OR the grounded flag) so the report reflects the true cost of the lever, but the
	// SCORED answer is the reconciled one.
	rep := samples[res.WinnerIx]
	agg = rep
	agg.Answer = res.Answer
	agg.Verdict = Score(t, res.Answer)
	// the graded score is recomputed from the RECONCILED answer (the one actually
	// scored), so it stays coherent with agg.Verdict — never the winner sample's.
	agg.Graded, agg.HasGraded = declineOrdinal(t, res.Answer)
	agg.ModelCalls = 0
	agg.ToolSelectEscalations = 0
	agg.ForceGroundEscalations = 0
	agg.Grounded = false
	for _, s := range samples {
		agg.ModelCalls += s.ModelCalls
		agg.ToolSelectEscalations += s.ToolSelectEscalations
		agg.ForceGroundEscalations += s.ForceGroundEscalations
		agg.Grounded = agg.Grounded || s.Grounded
	}
	agg.Seed = seed
	return agg, nil
}

// RunSingleStrong runs ARM SINGLE-STRONG: the same full engine as ArmHarness but with
// sub-agent FAN-OUT disabled — Subconscious.SubAgents OFF and MaxParWidth clamped to 1
// (no team dispatch). This is the "single strong agent" baseline the sub-agent-beats-
// best-member guard (subagentguard.go) requires: the harness (team) arm must BEAT it for
// the sub-agent layer to count as adding value, else it is anti-value ("Multi-Agent Teams
// Hold Experts Back"). It still grounds against the materialized workspace (read tools
// are unaffected) — it just cannot fan out into a sub-agent TEAM.
//
// PURE CONTROL/PLUMBING: it is a config FLIP on the existing engine path (no new CONTENT
// role, no new model-call shape) — the same Generate/Respond surface the harness arm
// already exercises. The deliberative lever does NOT apply here: the single-strong
// baseline is one trajectory by definition (it isolates the sub-agent axis, not the
// self-consistency axis).
func RunSingleStrong(t Task, factory BackendFactory, seed int64, maxTicks int, workspaceRoot string) (RunResult, error) {
	return runHarnessEpisode(t, factory, seed, maxTicks, workspaceRoot, ArmSingleStrong, disableSubAgentFanout)
}

// disableSubAgentFanout is the config mutator that turns the full-harness engine into the
// SINGLE-STRONG baseline: sub-agent dispatch OFF, parallel width pinned to 1 (no team
// fan-out). Everything else (graph / Controller / seams / regulator / gate / specialists /
// grounding read tools) stays ON — this isolates the sub-agent-TEAM axis the guard tests.
func disableSubAgentFanout(c *config.HarnessConfig) {
	c.Subconscious.SubAgents = false
	c.Subconscious.MaxParWidth = 1
}

// runHarnessEpisode runs ONE engine episode (one trajectory) over a fresh workspace + a fresh
// engine seeded with `seed`. It is the single-trajectory work shared by the K==1 harness path
// (called once), the deliberative K>1 harness path (called K times on distinct per-sample seeds),
// AND the single-strong baseline (called once with the fan-out-disabling mutator). The arm name
// labels the result; mutateCfg (nil for the harness arm) lets the single-strong arm flip the
// fan-out knobs off the AllOn baseline. The body is otherwise the pre-flag RunHarness verbatim.
func runHarnessEpisode(t Task, factory BackendFactory, seed int64, maxTicks int, workspaceRoot, arm string, mutateCfg func(*config.HarnessConfig)) (RunResult, error) {
	ws, err := os.MkdirTemp(workspaceRoot, "rh-ws-")
	if err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(ws)
	if err := materialize(ws, t.Materials); err != nil {
		return RunResult{}, err
	}

	be := factory(seed, DefaultTemperature)
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = int(seed)
	cfg.MaxTicks = maxTicks
	cfg.Cognition = "control"
	cfg.Workspace = ws          // non-empty -> the engine builds a real executor with read tools
	cfg.Features = config.New() // AllOn: graph + Controller + seams + regulator + convert + gate
	// WEB-SEARCH edge (--web): enable the subconscious.web_search flag so the engine registers the
	// web_search tool + grants expose-affordances the web_search scope. The live DuckDuckGo seam is wired
	// post-construction via SetWeb below (the engine never builds a network seam itself). Default OFF ⇒
	// byte-identical (web-blind harness arm). It rides the AllOn base, so the single-strong mutator below
	// still applies on top — both arms get web_search when --web is set, isolating only the fan-out axis.
	if webSearchEdge {
		cfg.Features.Subconscious.WebSearch = true
		// FETCH-URL edge (T1.4): the same --web edge also enables the fetch_url tool so the browse loop
		// (web_search -> see a result URL -> fetch_url the page -> ground) is reachable on the harness arm.
		// The live page-fetch seam (web.Pager) is wired post-construction via SetPageFetcher below. Default
		// OFF ⇒ byte-identical (page-blind harness arm); the loop stays EMERGENT (no hardcoded multi-step).
		cfg.Features.Subconscious.FetchURL = true
		// QUERY-FORMULATION edge (T1.1): on the same --web edge, formulate the web_search query from the
		// actual question (strip the bench's instruction wrapper) instead of searching the whole goal — the
		// measured fix for the wrapper-pollution that returned benchmark meta-pages. Default OFF ⇒ byte-identical.
		cfg.Features.Subconscious.QueryFormulation = true
	}
	if mutateCfg != nil {
		mutateCfg(cfg.Features) // e.g. the single-strong arm flips sub-agent fan-out OFF
	}

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		return RunResult{}, err
	}
	// WEB-SEARCH edge: wire the live DuckDuckGo seam AFTER NewEngine (the SetWeb-before-Run contract), so
	// the lazily-bound web_search tool reaches the real network. Only when --web is set ⇒ default OFF
	// keeps the harness arm web-blind (SetWeb is never called ⇒ e.web stays nil ⇒ web_search is a blind
	// read even if the flag were somehow on).
	if webSearchEdge {
		eng.SetWeb(web.NewDuckDuckGo())
		// FETCH-URL edge (T1.4): wire the live page fetcher AFTER NewEngine (the SetWeb-before-Run contract)
		// so the lazily-bound fetch_url tool reaches the real network. Only when --web is set ⇒ default OFF
		// keeps the harness arm page-blind.
		eng.SetPageFetcher(web.NewPager())
	}

	var ctr runCounters
	ctr.subscribe(eng.Bus())
	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(t.Prompt)
	eng.Run(maxTicks)
	grounded := eng.Grounding().Len() > groundBefore
	calls := ctr.calls

	answer := harnessAnswer(eng)
	if calls == 0 {
		calls = 1 // offline double emits no llm.* — count the episode as one logical call
	}
	graded, hasGraded := declineOrdinal(t, answer)
	return RunResult{
		TaskID:                 t.ID,
		Capability:             t.Capability,
		Arm:                    arm,
		Seed:                   seed,
		Answer:                 answer,
		Verdict:                Score(t, answer),
		Value:                  eng.ActiveValue(),
		ModelCalls:             calls,
		Grounded:               grounded,
		ToolSelectEscalations:  ctr.toolSelectEsc,
		ForceGroundEscalations: ctr.forceGroundEsc,
		Graded:                 graded,
		HasGraded:              hasGraded,
	}, nil
}

// runCounters taps the engine event bus for the per-episode tallies the report
// surfaces: the model-CALL count (events.LLM) and the two grounding-fix
// ENGAGEMENT signals (escalation.tool_select / escalation.force_ground). It is
// the single source of the harness arm's counts so the wiring is testable: a test
// installs it on a real bus, emits the escalation kinds, and asserts the tallies
// (mutation-sensitive on the event-kind constants and the switch).
type runCounters struct {
	calls          int
	toolSelectEsc  int
	forceGroundEsc int
}

// subscribe installs the counting handler on the bus. Called once before the
// episode runs; the handler fires synchronously on every emit.
func (c *runCounters) subscribe(bus *events.Bus) {
	bus.Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.LLM:
			c.calls++
		case events.EscalationToolSelect:
			c.toolSelectEsc++
		case events.EscalationForceGround:
			c.forceGroundEsc++
		}
		// WEB-SEARCH chain trace (env-gated diagnostic): surface staffing + tool dispatch so a live
		// web_search non-firing can be pinpointed (staffed? scoped? dispatched?). Off by default.
		if os.Getenv("THOUGHT_WEB_TRACE") != "" {
			if strings.Contains(ev.Kind, "subagent") || strings.Contains(ev.Kind, "tool") || strings.Contains(ev.Kind, "workflow") || strings.Contains(ev.Kind, "synthesize") {
				fmt.Fprintf(os.Stderr, "[WEBTRACE] kind=%s summary=%q tool=%v op=%v\n", ev.Kind, ev.Summary, ev.Data["tool"], ev.Data["operator"])
			}
		}
	})
}

// harnessAnswer returns the text the oracle scores for the harness arm: the
// engine's last user-facing response, but if that is empty (the engine surfaced
// an honest gap), fall back to scanning the thought-graph history for the
// grounded conclusion — the same two-source read scoreSolvedEngine uses, so a
// value that reached a thought but was not re-voiced still scores. An empty
// last-response with no graph hit stays "" (the honest-gap surface the
// anti-confabulation oracle credits as a decline).
func harnessAnswer(eng *engine.Engine) string {
	if r := strings.TrimSpace(eng.LastResponse()); r != "" {
		return r
	}
	// concatenate the graph history so a value that landed in a thought (but was
	// not re-voiced into the final response) is still visible to the oracle.
	if g := eng.Graph(); g != nil {
		var b strings.Builder
		for _, th := range g.History() {
			b.WriteString(th.Text)
			b.WriteString("\n")
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// materialize writes the task's files into the workspace, creating parent dirs.
func materialize(ws string, files map[string]string) error {
	for rel, content := range files {
		full := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// backendCalls reads a real LLM backend's own call tally if it exposes one (the
// OpenAICompat/claude backends do). Mirrors runner.backendCalls.
func backendCalls(be backends.Backend) (int, bool) {
	type counter interface{ Calls() int }
	if c, ok := be.(counter); ok {
		return c.Calls(), true
	}
	return 0, false
}

// ---- the suite driver -----------------------------------------------------

// SuiteConfig parameterizes a full bare-vs-harness run.
type SuiteConfig struct {
	Factory       BackendFactory
	Replays       int             // K replays per task per arm (>=1)
	SeedBase      int64           // per-replay seed = SeedBase + replay
	MaxTicks      int             // harness episode cap (0 -> DefaultMaxTicks)
	WorkspaceRoot string          // writable parent for per-run workspaces ("" -> os.TempDir)
	Concurrency   int             // task-level parallelism (1 = serial); arms within a task run serially
	OnResult      func(RunResult) // optional progress callback (called under a lock; nil ok)
	// OnlyArm restricts the RUN (not just the report) to one arm — "bare" or
	// "harness" — so a calibration pass spends tokens on only the arm under test.
	// Empty runs BOTH arms (the headroom proof).
	OnlyArm string
	// TaskFilter is a comma-separated list of task-ID substrings (FilterTasks):
	// only tasks whose ID contains any of them run. Empty => ALL tasks
	// (byte-identical to the unfiltered path). The cheap-subset selector behind
	// cmd/realhard's --only-task.
	TaskFilter string
	// Tasks OVERRIDES the bank: when non-nil the suite runs these tasks instead of
	// the built-in Tasks(). Nil => the built-in suite (byte-identical to the
	// pre-flag path). The hook the offline instrument-validation set
	// (InstrumentValidationTasks) and a converted EXTERNAL bank (banks/external)
	// flow through the SAME runner with no duplicated driver. TaskFilter still
	// applies on top.
	Tasks []Task
	// IncludeSingleStrong, when true, ALSO runs the SINGLE-STRONG baseline arm
	// (RunSingleStrong: the full engine with sub-agent fan-out disabled) per task per
	// replay, so the sub-agent-beats-best-member guard has its baseline. Default false
	// ⇒ byte-identical to the pre-flag two-arm path (the third arm is opt-in, behind
	// cmd/realhard's --ab mode). Honoured only when OnlyArm is empty (a single-arm
	// calibration pass never adds the baseline).
	IncludeSingleStrong bool
}

// ArmError records ONE (task, arm, replay) run that failed (an error return from an
// engine episode, or a recovered panic), so the suite can KEEP GOING (the run does not
// abort on a single bad cell) while still surfacing every failure honestly: the live AB
// run prints each one at the failure site and the suite returns the full list. This is
// the robustness contract — a single arm/episode failure is LOGGED and recorded, NOT a
// silent whole-run abort (the bug this fixes: the live --ab run died after the bare arm
// with no diagnostic because the first errored arm short-circuited the entire run via a
// swallowed firstErr and threw away every collected result + wrote no report).
type ArmError struct {
	TaskID     string
	Capability Capability
	Arm        string
	Replay     int
	Seed       int64
	Err        string // the error text (or the recovered panic message + stack head)
}

// Error renders one arm failure as a single human line (used by the AB runner's
// per-failure log and the partial-completion summary).
func (e ArmError) Error() string {
	return fmt.Sprintf("[%s] %s %s r%d (seed=%d): %s", e.Capability, e.TaskID, e.Arm, e.Replay, e.Seed, e.Err)
}

// safeArmRun runs one arm episode under a panic recover so a panic inside the engine on
// the LIVE substrate (which would otherwise crash the whole process in a worker goroutine
// — often the silent "exit 1, no error" — and lose every collected result) is converted
// into an ordinary error the suite records and CONTINUES past. Pure plumbing — it never
// alters a successful run; it only catches the failure path.
func safeArmRun(run func() (RunResult, error)) (rr RunResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("PANIC: %v\n%s", r, head(string(debug.Stack()), 1200))
		}
	}()
	return run()
}

// head truncates s to n bytes (for a bounded panic-stack capture in an ArmError).
func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// failResult builds a synthetic FAIL RunResult for an arm-run that errored or panicked,
// so the report STILL accounts for the cell (a missing run would silently inflate the
// surviving arm's solve-rate by shrinking its denominator) and the per-arm progress line
// prints at the failure site with the reason. It is scored FAIL with the error as the
// Verdict.Reason — never a fabricated SOLVE.
func failResult(t Task, arm string, replay int, seed int64, errText string) RunResult {
	return RunResult{
		TaskID:     t.ID,
		Capability: t.Capability,
		Arm:        arm,
		Replay:     replay,
		Seed:       seed,
		Answer:     "",
		Verdict:    Verdict{Solved: false, Reason: "RUN-ERROR: " + errText},
		ModelCalls: 0,
		Grounded:   false,
	}
}

// RunSuite runs every task under both arms over K replays and returns every
// per-run result (flat). Tasks run through a bounded worker pool; the two arms
// of one task run serially within a worker. Results are appended under a lock so
// the slice is complete on return (order is not guaranteed — the reducer sorts).
//
// ROBUSTNESS: a per-arm error (an engine episode error or a recovered panic) does NOT
// abort the run. The failed cell is recorded as a synthetic FAIL RunResult (so the report
// still accounts for it and OnResult prints it at the failure site) and logged into the
// returned []ArmError; the worker CONTINUES to the next arm/task. The error return is
// non-nil ONLY when NOTHING completed (every run failed) — a total wipe-out is a real
// fail-loud, a partial one is reported but the report is still built. This converts the
// old silent whole-run abort (a swallowed firstErr that discarded all collected results
// and wrote no report) into a robust, self-diagnosing run.
func RunSuite(cfg SuiteConfig) ([]RunResult, []ArmError, error) {
	bank := cfg.Tasks
	if bank == nil {
		bank = Tasks()
	}
	tasks := FilterTasks(bank, cfg.TaskFilter)
	replays := cfg.Replays
	if replays < 1 {
		replays = 1
	}
	wsRoot := cfg.WorkspaceRoot
	if wsRoot == "" {
		wsRoot = os.TempDir()
	}
	n := cfg.Concurrency
	if n < 1 {
		n = 1
	}

	var (
		mu       sync.Mutex
		results  []RunResult
		armErrs  []ArmError
		attempts int // total arm-runs attempted (the denominator for the total-wipe-out check)
	)
	emit := func(r RunResult) {
		mu.Lock()
		results = append(results, r)
		attempts++
		if cfg.OnResult != nil {
			cfg.OnResult(r)
		}
		mu.Unlock()
	}
	// runArm runs ONE arm episode (under panic-recover), then EITHER emits its result OR,
	// on error/panic, records the failure (as an ArmError) AND emits a synthetic FAIL
	// RunResult — so the report still accounts for the cell and OnResult prints the reason
	// at the failure site. It never aborts the run: the worker continues to the next arm.
	runArm := func(task Task, arm string, replay int, seed int64, run func() (RunResult, error)) {
		rr, err := safeArmRun(run)
		if err != nil {
			ae := ArmError{TaskID: task.ID, Capability: task.Capability, Arm: arm, Replay: replay, Seed: seed, Err: err.Error()}
			mu.Lock()
			armErrs = append(armErrs, ae)
			mu.Unlock()
			fr := failResult(task, arm, replay, seed, err.Error())
			emit(fr) // records the FAIL cell + prints the reason via OnResult
			return
		}
		rr.Replay = replay
		emit(rr)
	}

	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for ti := range in {
				task := tasks[ti]
				for r := 0; r < replays; r++ {
					seed := cfg.SeedBase + int64(r)
					// ARM A (bare). The bare arm is a single Generate; it does not return an
					// error, but route it through runArm so a panic on the live substrate is
					// still caught + recorded rather than crashing the whole worker.
					if cfg.OnlyArm != ArmHarness {
						task, r, seed := task, r, seed
						runArm(task, ArmBare, r, seed, func() (RunResult, error) {
							return RunBare(task, cfg.Factory, seed), nil
						})
					}
					// ARM B (harness)
					if cfg.OnlyArm != ArmBare {
						task, r, seed := task, r, seed
						runArm(task, ArmHarness, r, seed, func() (RunResult, error) {
							return RunHarness(task, cfg.Factory, seed, cfg.MaxTicks, wsRoot)
						})
					}
					// ARM SINGLE-STRONG (the sub-agent-guard baseline) — opt-in, run
					// ONLY when both arms run (a single-arm calibration never adds it).
					if cfg.IncludeSingleStrong && cfg.OnlyArm == "" {
						task, r, seed := task, r, seed
						runArm(task, ArmSingleStrong, r, seed, func() (RunResult, error) {
							return RunSingleStrong(task, cfg.Factory, seed, cfg.MaxTicks, wsRoot)
						})
					}
				}
			}
		}()
	}
	for ti := range tasks {
		in <- ti
	}
	close(in)
	wg.Wait()

	// Fail LOUD only on a TOTAL wipe-out (every attempted run failed) — then there is no
	// signal to report and the caller should abort. A PARTIAL failure returns nil error
	// (the report is still built from what completed) with the failures in armErrs so the
	// caller can log + annotate the report. attempts==0 means no tasks/arms ran at all.
	if attempts > 0 && len(armErrs) == attempts {
		return results, armErrs, fmt.Errorf("ALL %d arm-runs failed; first: %s", attempts, armErrs[0].Error())
	}
	return results, armErrs, nil
}
