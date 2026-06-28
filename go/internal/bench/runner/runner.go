package runner

import (
	"os"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/cost"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// DefaultTemperature is the low, fixed sampling temperature used across ALL arms so a
// real-model contrast stays as paired as a stochastic model allows (spec §5.1: same base
// model + temperature across arms, only the harness/gate toggled). The test double is
// deterministic and ignores it.
const DefaultTemperature = 0.2

// DefaultMaxTicks bounds a reactive episode for the harness/gate arms. The engine breaks
// early on quiescence; this is the safety budget (the spec's step-cap is 25, but the
// engine counts ticks, several of which precede the first reasoning step — 60 leaves
// headroom while still terminating a runaway).
const DefaultMaxTicks = 60

// BackendFactory builds a fresh Backend for one arm. It is called once per arm so the
// model-call tally on an LLM backend starts at zero (the runner reads the delta as
// ModelCalls). The seed is threaded so a factory may pin determinism; temp is the fixed
// cross-arm temperature.
//
// The two stock factories are TestFactory (offline deterministic, for tests/UAT) and
// LLMFactory (the real OpenAI-compatible substrate).
type BackendFactory func(seed int64, temp float64) backends.Backend

// TestFactory builds the offline, deterministic test double (backends.NewTest). It ignores
// seed + temp (the engine threads its own seeded RNG; the double is deterministic). Use it
// for the smoke test and any UAT path that must not touch the network.
func TestFactory(seed int64, temp float64) backends.Backend { return backends.NewTest() }

// LLMFactory returns a BackendFactory bound to one OpenAI-compatible endpoint/model. The
// returned factory builds a fresh OpenAICompatBackend per arm at the fixed temperature, so
// each arm's Calls tally starts at zero. baseURL/model "" fall back to the llm package
// defaults (LM Studio).
func LLMFactory(baseURL, model string) BackendFactory {
	return func(seed int64, temp float64) backends.Backend {
		be := llm.NewOpenAICompat(llm.Options{
			BaseURL:     baseURL,
			Model:       model,
			Temperature: temp,
		})
		// MIN/MAX-context routing: honor THOUGHT_LLM_MAXCTX_* so a truncated call escalates to the
		// big-context model (no-op when unset). The bench/trprobe build the backend directly, bypassing
		// ResolveSubstrate where this is normally wired.
		llm.WireMaxCtxFromEnv(be)
		return be
	}
}

// Spec is one unit of work for the Runner: a prompt run under one Arm to probe one
// Mechanism, paired on Seed. Temperature/MaxTicks default when zero.
type Spec struct {
	// Prompt is the question/request fed to the arm (the Tier-A item prompt or the first
	// Tier-B turn).
	Prompt string
	// Arm is the configuration to run under (bare / harness / gate-on / gate-off).
	Arm benchtypes.Arm
	// Mechanism selects which gate toggle(s) the gate-off arm flips (and which isolation
	// predicate the caller will ask for). Ignored by the bare arm.
	Mechanism benchtypes.Mechanism
	// Seed is the paired RNG seed — the SAME value across arms makes the contrast paired.
	Seed int64
	// Temperature is the fixed cross-arm sampling temperature (0 ⇒ DefaultTemperature).
	Temperature float64
	// MaxTicks bounds a harness/gate-arm episode (0 ⇒ DefaultMaxTicks). Ignored by the
	// bare arm (one Generate call).
	MaxTicks int
	// FrozenSnapshot, when non-empty, is a serialized frozen engine state for a SINGLE-DECISION
	// probe rather than a full episode (measuring-stick-spec §3.4, continuous-autonomy Tier-A).
	// On a harness/gate arm the runner decodes it and runs ONE awake-regime decision
	// (Engine.DecideContinuousFromSnapshot) instead of SubmitDefault + Run(maxTicks) — fast,
	// deterministic, and it actually exercises the awake decision spine the item probes. The bare
	// arm ignores it (it answers the prompt with one Generate call, reading the snapshot itself).
	FrozenSnapshot []byte
	// Exposures, when > 1, drives the SAME goal through ONE harness engine that many times
	// (re-entrant episodes, the convertibility couplet of measuring-stick-spec §3.3 A1: X then
	// X' … in ONE session). Self-improvement is a SECOND-occurrence property — a single episode
	// mints at the trailing IDLE consolidate but can never REUSE the mint in the same trace, so a
	// one-shot Tier-A item can never witness mint→reuse. Recurring the goal lets the early
	// exposures mint and the later ones resolve via the minted artifact (cheaper) — the learning
	// curve the isolation predicate (MintThenReused) reads. The scored answer is the LAST
	// exposure's. 0/1 ⇒ a single episode (every other mechanism is unchanged). The bare arm
	// ignores it (one Generate call — it has no cross-episode state to learn from, the honest
	// baseline). Harness/gate arms only.
	Exposures int
}

// ArmRun is one arm's outcome on one Spec: the raw answer text, the full captured event
// trace, the per-arm Cost, and the Unsupported flag + Note for the gate-off arms whose
// toggle does not exist yet. It is the in-process record the Tier-A/Tier-B scorers read
// (they project it into types.ItemResult / types.ScenarioResult).
type ArmRun struct {
	// Arm is the arm this run was produced under (echoed back for pairing).
	Arm benchtypes.Arm
	// Mechanism is the mechanism the run probed (echoed back).
	Mechanism benchtypes.Mechanism
	// Seed is the paired seed (echoed back).
	Seed int64
	// Text is the arm's raw answer: the last user-facing Respond/answer text for the
	// harness arms, or the single Generate return for the bare arm. The oracle scores it.
	Text string
	// Events is the full captured event trace (every bus event during the run, in order).
	// The isolation predicates read this — genuine mechanism use is witnessed here, never
	// inferred.
	Events []events.Event
	// Cost is the per-arm resource cost: ModelCalls (from llm.* events / the backend Calls
	// tally), Steps (engine tick count), Tokens (best-effort from llm.* event data).
	Cost benchtypes.Cost
	// Unsupported is true when the requested gate-off arm cannot be built because the
	// mechanism's ablation toggle does not exist yet (multi-step-retrace, continuous-
	// autonomy). The run still returns a Note; Text/Events/Cost are zero.
	Unsupported bool
	// Note carries the human-readable reason for an Unsupported run (the TODO + which
	// toggle is missing), or a short status string otherwise.
	Note string
}

// gateOffToggles is the MECHANISM → gate-off toggle map (spec §3, the single-toggle
// ablation of §5.1). A gate-off arm builds an AllOn config and flips exactly these paths
// OFF. A mechanism whose entry is the sentinel `unsupported` has no toggle yet — the
// runner returns an ArmUnsupported result rather than faking the ablation.
var gateOffToggles = map[benchtypes.Mechanism][]string{
	// grounding → seam.watched_sync OFF stops the harness importing reality through the
	// synchronous watched seam (the ACT→reality read AND the rung-4 reality sourcer). The
	// grounding VALUE is the READ, not the Filter (spec §3.1): with the Filter alone OFF the
	// harness STILL performs the watched-seam read and still grounds, so gate-off would still
	// pass — collapsing the mechanism-lift contrast. With watched_sync OFF the gate-off arm
	// emits no action.observation / grounding.ground (verify: GroundingReadHappened=false) and
	// answers from priors (bare-like), so the gate-on − gate-off contrast cleanly attributes
	// the lift to the grounding read.
	benchtypes.MechGrounding: {"seam.watched_sync"},
	benchtypes.MechSelfImprovement: {
		"convert.specialist_mint",
		"convert.skill_mint",
		"convert.gate_prior_mint",
		"convert.path_mint",
		"subconscious.operator_mint",
	},
	benchtypes.MechStability: {"regulator.enforce"},
	benchtypes.MechSafety:    {"action.safety_gate"},
	// SUPPORTED (the two ablation toggles added per measuring-stick-spec §5.8):
	//   multi-step-retrace → conscious.allow_backtrack OFF forbids the Controller's BACKTRACK move,
	//     so a refuted line cannot retrace and the graph degrades to a single line.
	//   continuous-autonomy → conscious.endogenous_drive OFF disables the awake Drives / Default-mode
	//     wander / proactive outreach, so durable self-direction with no user turns cannot occur.
	benchtypes.MechMultiStepRetrace:   {"conscious.allow_backtrack"},
	benchtypes.MechContinuousAutonomy: {"conscious.endogenous_drive"},
}

// SupportedGateOff reports whether mech has a gate-off ablation toggle wired (so a
// gate-off / gate-on arm can be built). The two UNSUPPORTED-YET mechanisms return false.
func SupportedGateOff(mech benchtypes.Mechanism) bool {
	toggles, known := gateOffToggles[mech]
	return known && len(toggles) > 0
}

// GateOffToggles returns the toggle paths the gate-off arm flips OFF for mech (a copy),
// or nil for an UNSUPPORTED-YET mechanism. Exposed so a caller can render/audit exactly
// which single ablation a run performed (spec §5.1: the ablation must be one toggle so
// the OFF→ON contrast is attributable).
func GateOffToggles(mech benchtypes.Mechanism) []string {
	t := gateOffToggles[mech]
	if len(t) == 0 {
		return nil
	}
	out := make([]string, len(t))
	copy(out, t)
	return out
}

// Runner executes one Spec under one arm against a Backend the factory builds. It holds no
// per-run state (each Run is independent), so one Runner is safe to reuse across arms/seeds.
type Runner struct {
	// Factory builds a fresh backend per arm (so each arm's model-call tally starts at 0).
	Factory BackendFactory
	// Workspace, when set, is handed to the harness/gate engine so the Action layer
	// dispatches REAL sandboxed tools (safety/grounding arms need a real read/run). "" ⇒
	// the offline heuristic-act path (the smoke test uses ""). The bare arm never uses it.
	Workspace string
}

// New builds a Runner over the given backend factory (and an optional Action workspace).
func New(factory BackendFactory, workspace string) *Runner {
	return &Runner{Factory: factory, Workspace: workspace}
}

// Run executes spec under spec.Arm and returns the ArmRun. It never errors for a normal
// run: a build failure on the harness path is surfaced in Note (Unsupported stays false
// only for genuine engine-construction failures, which carry the error in Note). A
// gate-off / gate-on request for an UNSUPPORTED-YET mechanism returns Unsupported=true.
func (r *Runner) Run(spec Spec) ArmRun {
	temp := spec.Temperature
	if temp == 0 {
		temp = DefaultTemperature
	}
	switch spec.Arm {
	case benchtypes.ArmBare, benchtypes.ArmBareNoTools, benchtypes.ArmBareRawTools:
		return r.runBare(spec, temp)
	case benchtypes.ArmHarness, benchtypes.ArmGateOn:
		// harness / gate-on: the full engine, every toggle ON.
		return r.runEngine(spec, temp, config.New())
	case benchtypes.ArmGateOff:
		return r.runGateOff(spec, temp)
	case benchtypes.ArmSingleStrong:
		return r.runSingleStrong(spec, temp)
	default:
		return ArmRun{
			Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
			Note: "runner: unknown arm " + spec.Arm.String(),
		}
	}
}

// runBare is the bare arm: NO graph / Controller / seams / regulator / convert / gate. It
// calls backend.Generate(goal, empty-ctx, seeded-rng) ONCE and returns the text (spec
// §5.1: the base model alone, the reference the lift is measured against). There is no
// event bus on this path, so Events is empty and Steps is 1.
func (r *Runner) runBare(spec Spec, temp float64) ArmRun {
	backend := r.Factory(spec.Seed, temp)
	rng := cpyrand.New(uint64(spec.Seed))
	// A bare model with no harness has no thought graph — an empty context is the honest
	// "base model alone" input. Generate is the CONSCIOUS effortful next-thought role; one
	// call is the single-shot answer.
	text := backend.Generate(spec.Prompt, []types.Thought{}, rng)
	calls := 1
	if c, ok := backendCalls(backend); ok {
		calls = c // a real LLM backend reports its own call tally
	}
	return ArmRun{
		Arm:       spec.Arm,
		Mechanism: spec.Mechanism,
		Seed:      spec.Seed,
		Text:      strings.TrimSpace(text),
		Events:    nil,
		Cost:      benchtypes.Cost{ModelCalls: calls, Steps: 1, Tokens: 0},
		Note:      "bare: single Generate call (no harness)",
	}
}

// runGateOff builds the gate-off arm: an AllOn config with EXACTLY the mechanism's gate
// toggle(s) flipped OFF (spec §5.1, the single-toggle ablation). An UNSUPPORTED-YET
// mechanism returns Unsupported=true with a TODO note — never a faked ablation.
func (r *Runner) runGateOff(spec Spec, temp float64) ArmRun {
	toggles, known := gateOffToggles[spec.Mechanism]
	if !known {
		return ArmRun{
			Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
			Unsupported: true,
			Note:        "gate-off: unknown mechanism " + spec.Mechanism.String(),
		}
	}
	if len(toggles) == 0 {
		// multi-step-retrace / continuous-autonomy: the ablation toggle does not exist.
		return ArmRun{
			Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
			Unsupported: true,
			Note: "UNSUPPORTED-YET: no gate-off toggle for " + spec.Mechanism.String() +
				" — TODO: add the ablation flag (forbid Controller BACKTRACK / awake-regime off) " +
				"per measuring-stick-spec §5.8; the runner must NOT fake this arm",
		}
	}
	cfg := config.New() // AllOn
	for _, path := range toggles {
		if !config.ApplyToggle(cfg, path, false) {
			// A toggle path that no longer resolves is a contract drift — surface it loudly
			// rather than silently running a non-ablated arm.
			return ArmRun{
				Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
				Unsupported: true,
				Note:        "gate-off: toggle path not found: " + path + " (config knob drift)",
			}
		}
	}
	return r.runEngine(spec, temp, cfg)
}

// singleStrongTogglePath is the ONE config knob the single-strong arm flips ON over an AllOn config —
// the engine-level collapse of the per-tick sub-agent fan-out to its single best member. It is applied via
// config.ApplyToggle so a knob-path drift fails LOUDLY (an Unsupported run) rather than silently building
// the FULL-fan-out engine, which would make the single-strong arm IDENTICAL to the harness arm and turn the
// teams-vs-best-member guard vacuous (the failure mode the BENCH-SUITE-A2 residue exists to close).
const singleStrongTogglePath = "subconscious.single_strong_agent"

// runSingleStrong builds the SUB-AGENT GUARD reference arm (docs/internal/notes/2026-06-21-sota-benchmark-suite.md
// §7.6): the FULL engine (graph / Controller / seams / regulator / gate all ON) with EXACTLY the
// single-strong-agent collapse flipped ON, so the per-tick sub-agent fan-out is reduced to its single best
// member. This is what makes the guard's A/B — harness (full fan-out) vs single-strong (best member only) —
// two engines that genuinely DIFFER: the flag is a real engine-behavior change the live dispatch tick
// consumes (features.go refreshes e.subconscious.SetSingleStrong each tick), NOT a no-op config field. The
// "Multi-Agent Teams Hold Experts Back" finding means the harness must measurably BEAT this arm or the
// sub-agent layer is anti-value. ApplyToggle is used (not a direct field set) so a knob-path drift surfaces
// loudly rather than silently running the full-fan-out (== harness) engine and making the guard vacuous.
func (r *Runner) runSingleStrong(spec Spec, temp float64) ArmRun {
	cfg := config.New() // AllOn (full harness)
	if !config.ApplyToggle(cfg, singleStrongTogglePath, true) {
		return ArmRun{
			Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
			Unsupported: true,
			Note: "single-strong: toggle path not found: " + singleStrongTogglePath +
				" (config knob drift — the runner must NOT fall back to the full-fan-out engine, " +
				"which would make the teams-vs-best-member guard vacuous)",
		}
	}
	return r.runEngine(spec, temp, cfg)
}

// runEngine constructs the full engine with the given Features config, subscribes an
// EventCollector to the bus BEFORE the run (so the opening trace is captured), drives one
// reactive episode (SubmitDefault + Run), and assembles the ArmRun from the collected
// trace + the engine's post-run state. The collected events are the substrate the
// isolation predicates read.
func (r *Runner) runEngine(spec Spec, temp float64, features *config.HarnessConfig) ArmRun {
	backend := r.Factory(spec.Seed, temp)
	maxTicks := spec.MaxTicks
	if maxTicks <= 0 {
		maxTicks = DefaultMaxTicks
	}
	cfg := &engine.EngineConfig{
		Mode:      "reactive",
		Seed:      int(spec.Seed),
		MaxTicks:  maxTicks,
		Cognition: "control",
		Workspace: r.Workspace,
		Features:  features,
	}
	// THOUGHT_BENCH_REGISTRY_STATE (the campaign's Tier-2 ablation knob): when set to a persist state
	// dir, every bench engine constructs WITH that registry snapshot loaded (minted operators/skills
	// seeded before the first episode) — the with-batch arm of a registry lift A/B. Unset ⇒ nil Store ⇒
	// seed registries only, byte-identical to before. Env (not a param) so the A/B is two bench
	// invocations differing ONLY in this variable.
	if dir := os.Getenv("THOUGHT_BENCH_REGISTRY_STATE"); dir != "" {
		if st, err := persist.NewJSONLStore(dir); err == nil {
			cfg.Store = st
		}
	}
	eng, err := engine.NewEngine(cfg, backend)
	if err != nil {
		// Engine construction can fail (e.g. a real substrate that won't resolve). Surface
		// it; the arm did not run. This is a genuine error, not an UNSUPPORTED ablation.
		return ArmRun{
			Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
			Note: "engine construction failed: " + err.Error(),
		}
	}

	coll := newCollector()
	unsub := eng.Bus().Subscribe(coll.add)
	defer unsub()

	// Frozen-snapshot single-decision probe (measuring-stick-spec §3.4, continuous-autonomy
	// Tier-A): decode the serialized awake state and run ONE awake-regime decision rather than a
	// full episode. This is fast, deterministic, and it exercises the awake decision spine the item
	// actually probes — the prior full-episode path free-thought about the prompt text, never
	// loaded the snapshot, and produced no signal. The decision text is the arm's answer; the
	// emitted continuous.decision event is the isolation witness.
	if len(spec.FrozenSnapshot) > 0 {
		answer, derr := eng.DecideContinuousFromSnapshot(spec.FrozenSnapshot)
		if derr != nil {
			return ArmRun{
				Arm: spec.Arm, Mechanism: spec.Mechanism, Seed: spec.Seed,
				Note: "frozen-snapshot decode failed: " + derr.Error(),
			}
		}
		evs := coll.events()
		return ArmRun{
			Arm:       spec.Arm,
			Mechanism: spec.Mechanism,
			Seed:      spec.Seed,
			Text:      strings.TrimSpace(answer),
			Events:    evs,
			Cost:      DeriveCost(evs),
			Note:      "engine(frozen-probe): " + spec.Arm.String() + " (" + eng.BackendLabel() + ")",
		}
	}

	// Re-entrant recurrence (measuring-stick-spec §3.3 A1): drive the SAME goal through this ONE
	// engine `Exposures` times so the convertibility couplet completes IN-session — early
	// exposures mint the effortful path at the trailing IDLE consolidate, later exposures resolve
	// via the minted artifact (cheaper). A single episode mints but can never reuse the mint in
	// its own trace, so the mint→reuse isolation witness needs ≥2 exposures. Exposures ≤ 1 is one
	// episode (the default for every other mechanism — byte-identical behavior).
	exposures := spec.Exposures
	if exposures < 1 {
		exposures = 1
	}
	for i := 0; i < exposures; i++ {
		eng.SubmitDefault(spec.Prompt)
		eng.Run(maxTicks)
	}

	evs := coll.events()
	note := "engine: " + spec.Arm.String() + " (" + eng.BackendLabel() + ")"
	if exposures > 1 {
		note = "engine(" + strconv.Itoa(exposures) + "x-recurrence): " + spec.Arm.String() + " (" + eng.BackendLabel() + ")"
	}
	return ArmRun{
		Arm:       spec.Arm,
		Mechanism: spec.Mechanism,
		Seed:      spec.Seed,
		Text:      strings.TrimSpace(eng.LastResponse()),
		Events:    evs,
		Cost:      DeriveCost(evs),
		Note:      note,
	}
}

// DeriveCost computes the per-arm Cost from the captured trace (spec §3.3: ModelCalls is
// the primary cost metric, Steps the tie-break, Tokens the finest). ModelCalls counts
// llm.call events (the real model's per-call event); on the offline test double there are
// no llm.* events, so ModelCalls falls back to the count of conscious.generate events (the
// effortful-thought calls that WOULD be model calls under a real substrate) — keeping the
// metric meaningful on the deterministic path. Steps is the engine tick count (tick
// events).
//
// The TOKEN SPLIT (PART 3 money layer): each llm.call event carries the full usage
// accounting (PART 1 wired prompt/completion/cache-hit/cache-miss/reasoning onto the event
// data). LLMCallsFromEvents projects those into cost.LLMCall, and cost.Compute folds them
// into the input cache hit/miss split + the output/reasoning counts. We stash that split on
// Cost (UncachedInTokens / CachedInTokens / OutTokens / ReasoningTokens) and keep Tokens as
// the total (= cached-in + uncached-in + out) so the §3.3 tie-break is unchanged. All splits
// are 0 on the offline double (no llm.* events). Exported so the Tier-B scenario runner shares
// the SAME cost derivation (one source of truth for the token split).
func DeriveCost(evs []events.Event) benchtypes.Cost {
	var modelCalls, genCalls, ticks int
	for _, ev := range evs {
		switch ev.Kind {
		case events.LLM:
			modelCalls++
		case events.Generate:
			genCalls++
		case events.Tick:
			ticks++
		}
	}
	if modelCalls == 0 {
		// Offline / heuristic path: no llm.* events. The effortful generate calls are the
		// would-be model calls — use them so cost(N) curves still register on the double.
		modelCalls = genCalls
	}
	// Fold the per-call token usage into the run total via the cost layer (one source of
	// truth for the uncached/cached/out/reasoning split). The default rate card is fine here
	// — deriveCost only needs the TOKEN totals; the $ pricing happens at report time against
	// the campaign's chosen card.
	bd := cost.Compute(LLMCallsFromEvents(evs), cost.Default())
	t := bd.Total.Tokens
	return benchtypes.Cost{
		ModelCalls:       modelCalls,
		Steps:            ticks,
		Tokens:           t.Total(),
		CachedInTokens:   t.CachedIn,
		UncachedInTokens: t.UncachedIn,
		OutTokens:        t.Out,
		ReasoningTokens:  t.Reasoning,
	}
}

// LLMCallsFromEvents projects a captured event trace into the cost layer's per-call usage
// records, one per llm.call event (PART 1 carries role/model + the full token usage on each
// event's data). It is the single bridge from the event bus to the cost computation —
// deriveCost uses it for the per-arm token split, and the bench report uses it (via the
// per-arm trace) for the per-ROLE / per-MODEL aggregation. A -1 sentinel on any usage field
// means the server omitted it (distinguished from a true 0); intOrAbsent preserves that.
func LLMCallsFromEvents(evs []events.Event) []cost.LLMCall {
	var calls []cost.LLMCall
	for _, ev := range evs {
		if ev.Kind != events.LLM {
			continue
		}
		calls = append(calls, cost.LLMCall{
			// Tick is the bus tick the call fired at — the per-TICK rollup key (cost.
			// PerTickSpend buckets a run's calls by it). It is NOT a pricing input.
			Tick:              ev.Tick,
			Role:              strFromData(ev.Data, "role"),
			Model:             strFromData(ev.Data, "model"),
			PromptTokens:      intOrAbsent(ev.Data, "prompt_tokens"),
			CompletionTokens:  intOrAbsent(ev.Data, "completion_tokens"),
			CachedInputTokens: intOrAbsent(ev.Data, "cached_input_tokens"),
			CacheMissTokens:   intOrAbsent(ev.Data, "cache_miss_tokens"),
			ReasoningTokens:   intOrAbsent(ev.Data, "reasoning_tokens"),
		})
	}
	return calls
}

// intOrAbsent reads an int-ish value out of an event's data map (token counts may arrive as
// int or float64 after a JSONL round-trip). A MISSING key returns -1 — the cost layer's
// "the server did not report this field" sentinel (distinguished from a true 0). A present
// key with a non-numeric value also returns -1 (treat unparseable as absent).
func intOrAbsent(d map[string]any, key string) int {
	if d == nil {
		return -1
	}
	v, ok := d[key]
	if !ok {
		return -1
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return -1
}

// backendCalls reads a real LLM backend's own model-call tally (it implements the optional
// Calls field as part of OpenAICompatBackend). The test double does not, so ok=false and
// the bare arm uses its single-call count.
func backendCalls(b backends.Backend) (int, bool) {
	if lb, ok := b.(*llm.OpenAICompatBackend); ok {
		return lb.Calls, true
	}
	return 0, false
}
