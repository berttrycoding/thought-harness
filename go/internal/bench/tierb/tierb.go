// Package tierb is the Tier-B multi-turn scenario driver of the measuring stick
// (docs/internal/notes/measuring-stick-spec.md §5.3). Where the runner (internal/bench/runner) runs
// ONE prompt under ONE arm, this package runs a whole multi-turn TierBScenario under one arm:
// it threads a SINGLE engine across every scripted turn (so within-scenario state — the thought
// graph, minted specialists, the regulator's running estimates, the convertibility curve —
// persists exactly as it would in a real session), injects the out-of-band planted events on
// their schedule, collects the per-turn + end-state event trace, and scores the end-state with
// the deterministic oracle conjunction + the isolation predicate.
//
// The contract (spec §5.3):
//   - feed T1…Tn through ONE engine: SubmitDefault(turn.Text) then Run(perTurnBudget);
//   - inject the PlantedSchedule's out-of-band stimuli at their scheduled tick (a planted-event
//     hook — here a stub that writes a telemetry fixture file and/or submits a special percept);
//   - apply the EndStateOracles conjunction (reusing the DRY deterministic oracle helpers in
//     oracle.go) AND the IsolationPredicate (reusing the runner's per-mechanism witness);
//   - return one types.ScenarioResult (the §5.7 ledger row shape).
//
// Like the runner, RunScenario never errors on a normal run: an unsupported gate-off ablation
// or an engine-construction failure is surfaced in the result's RawOutput, with Pass=false.
package tierb

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	"github.com/berttrycoding/thought-harness/internal/bench/tiera"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// DefaultPerTurnBudget bounds the engine ticks spent settling ONE scripted turn. The engine
// breaks early on quiescence (Run returns when idle with no pending input); this is the safety
// budget per turn (a Tier-B scenario of n turns thus runs at most n×budget ticks). It mirrors
// runner.DefaultMaxTicks' headroom rationale: several ticks precede the first reasoning step.
const DefaultPerTurnBudget = 40

// TurnTrace is the captured event slice for one scripted turn (the events emitted between this
// turn's SubmitDefault and the end of its Run window). The per-turn split is kept so a scorer
// can attribute a clause to a specific turn (e.g. the pushback hold-rate is read off T3's
// slice); the end-state oracles read the flattened union of all turns.
type TurnTrace struct {
	// Index is the 1-based turn this slice belongs to (matches Turn.Index).
	Index int
	// Text is the user utterance that opened the turn (echoed for audit).
	Text string
	// Events are the events emitted while settling this turn, in order.
	Events []events.Event
}

// ScenarioRun is the in-process record of one arm's whole multi-turn run (the analogue of the
// runner's ArmRun, lifted to a scenario). RunScenario projects it into a types.ScenarioResult;
// it is returned alongside so a caller that wants the raw per-turn split / full trace (the
// pilot admission gate, a debugging tool) has it without re-running.
type ScenarioRun struct {
	// Arm is the arm this scenario was run under (echoed for pairing).
	Arm benchtypes.Arm
	// Mechanism is the mechanism the scenario forces (echoed).
	Mechanism benchtypes.Mechanism
	// Seed is the paired seed (echoed).
	Seed int64
	// Turns is the per-turn trace split, oldest-to-newest.
	Turns []TurnTrace
	// AllEvents is the flattened union of every turn's events (the end-state oracle substrate).
	AllEvents []events.Event
	// FinalText is the last user-facing answer the engine delivered across all turns (the
	// end-state summary the answer oracles score).
	FinalText string
	// Cost is the whole-scenario cost (summed model calls / steps / tokens across all turns).
	Cost benchtypes.Cost
	// Unsupported is true when the requested gate-off ablation has no toggle yet (the scenario
	// did not run); RawOutput/Note carries the reason.
	Unsupported bool
	// Note carries the human-readable status (the backend label, or the unsupported reason).
	Note string
}

// RetraceFixtureTag marks a PlantedEvent that is NOT an out-of-band stimulus but the failing-test
// fixture the driver materializes into the Action workspace before a multi-step-retrace scenario
// runs. Its Payload is the base64 of a `test_*.py` whose suite EXITS NON-ZERO, so the engine's
// "run the test suite" intention imports a genuine Observation.Ok=false that refutes the committed
// line (the offline heuristic-act path canned "tests pass" — Ok=true — which the retrace isolation
// predicate could never witness). RetraceFixture reads it; plantedEventsByTurn skips it as a percept.
const RetraceFixtureTag = "retrace-fixture"

// DefaultRetraceFixtureName is the in-workspace filename a retrace fixture is written under when the
// fixture plant does not name one. It is a `test_*.py` so `pytest -q` (the run_tests effector, no
// explicit target) collects + fails it.
const DefaultRetraceFixtureName = "test_calc.py"

// RetraceFixture returns the (filename, decoded-bytes) of the failing-test fixture a multi-step-
// retrace Tier-B scenario must materialize into its Action workspace, read from the scenario's
// RetraceFixtureTag planted event (Payload = base64 of the test file). It returns ("", nil) when the
// scenario declares no such fixture (the caller then runs on the offline path unchanged). The
// filename is the plant's TelemetrySource if it ends in .py, else DefaultRetraceFixtureName.
func RetraceFixture(scn benchtypes.TierBScenario) (string, []byte) {
	for _, pe := range scn.PlantedSchedule.Events {
		if pe.Tag != RetraceFixtureTag || strings.TrimSpace(pe.Payload) == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pe.Payload))
		if err != nil || len(raw) == 0 {
			return "", nil
		}
		name := DefaultRetraceFixtureName
		if ts := strings.TrimSpace(scn.PlantedSchedule.TelemetrySource); strings.HasSuffix(ts, ".py") {
			name = filepath.Base(ts)
		}
		return name, raw
	}
	return "", nil
}

// PlantInjector is the planted-event hook: it delivers ONE out-of-band stimulus into a running
// engine at its scheduled tick (spec §5.3 "planted-event injection"). The default
// (FixtureInjector) is a deliberately small stub — it writes the event's payload to a telemetry
// fixture file under the scenario's workspace AND submits a salient percept so a polling engine
// notices it. A real bank can swap a richer injector (a sensor that the continuous loop polls)
// without touching the driver.
type PlantInjector func(eng *engine.Engine, workspace string, pe benchtypes.PlantedEvent)

// FixtureInjector is the stock planted-event hook (the stub the deliverable asks for). For a
// planted event it (1) appends the payload as a JSONL row to the scenario's TelemetrySource
// fixture under workspace (so a polling engine sees the new reality on disk), and (2) submits
// the payload as a salient percept on the engine's port (so a reactive engine, which does not
// poll, still encounters the stimulus). Both are best-effort: a fixture write failure is
// swallowed (the percept submission is the load-bearing path on the offline test double).
func FixtureInjector(telemetrySource string) PlantInjector {
	return func(eng *engine.Engine, workspace string, pe benchtypes.PlantedEvent) {
		// (1) Write the planted row to the telemetry fixture, if one is named. This is the
		// "reality on disk" a continuous-mode engine polls; a no-source scenario skips it.
		if telemetrySource != "" && workspace != "" {
			path := filepath.Join(workspace, telemetrySource)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				row := pe.Payload
				if row == "" {
					row = pe.Tag
				}
				f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if ferr == nil {
					_, _ = f.WriteString(row + "\n")
					_ = f.Close()
				}
			}
		}
		// (2) Submit the stimulus as a salient percept so a reactive engine (which does not poll a
		// fixture) still encounters it. The tag prefixes the text so a trace reader can see which
		// planted event drove the resulting thought.
		text := "[" + pe.Tag + "] " + pe.Payload
		if pe.Payload == "" {
			text = "[" + pe.Tag + "]"
		}
		eng.Submit(strings.TrimSpace(text), true)
	}
}

// RunScenario runs one TierBScenario under one arm, paired on seed, threading a SINGLE engine
// across every scripted turn (the deliverable's core requirement). It builds the engine once
// with the arm's config (gate-on/harness = AllOn; gate-off = AllOn with the mechanism's single
// ablation toggle flipped OFF; bare = the linear no-graph loop), feeds T1…Tn through it,
// injects the planted schedule on tick, then scores the end-state. It returns a
// types.ScenarioResult (the §5.7 row) — never an error; failures land in the result.
//
// backendFactory builds a fresh backend for the arm (so the model-call tally starts at zero).
// workspace, when non-empty, is the Action sandbox + the root the telemetry fixture is written
// under; "" ⇒ the offline heuristic-act path (the unit test passes "").
func RunScenario(scn benchtypes.TierBScenario, arm benchtypes.Arm, seed int64, backendFactory runner.BackendFactory, workspace string) benchtypes.ScenarioResult {
	run := runScenarioRaw(scn, arm, seed, backendFactory, workspace, DefaultPerTurnBudget)
	run = augmentStabilityTelemetry(scn, run)
	return scoreScenario(scn, run)
}

// augmentStabilityTelemetry appends the deterministic regulator-response probe
// telemetry to a stability scenario's trace (spec §3.5 Tier-B end-state: "BARE
// diverges, HARNESS bounded, scored on telemetry"). The planted fan-out + broken-
// step the scenario scripts IS the regulator stress; the live reactive episode
// does NOT drive the regulator into that regime (peak n stays 0, regulator.
// stability never fires), so the stability verdict must be grounded in the probe.
// The harness/gate-on arm runs the probe CLOSED-LOOP (the regulator holds the
// regime); the bare/gate-off arm runs it OPEN-LOOP (theta frozen — it diverges).
// Non-stability scenarios, and stability scenarios with no probe spec, are
// untouched.
func augmentStabilityTelemetry(scn benchtypes.TierBScenario, run ScenarioRun) ScenarioRun {
	if scn.Mechanism != benchtypes.MechStability || run.Unsupported {
		return run
	}
	spec := stabilityProbeSpec(scn)
	if spec == "" {
		return run
	}
	closed := tiera.ProbeClosedForArm(run.Arm)
	probeEvents := tiera.ProbeEvents(spec, closed)
	// The bare arm has no event bus (AllEvents nil); the probe telemetry is still
	// the honest open-loop reference, so attach it (the telemetry oracle reads it,
	// and the bare-arm isolation is not gated regardless).
	run.AllEvents = append(run.AllEvents, probeEvents...)
	return run
}

// stabilityProbeSpec extracts the regulator-probe spec for a stability scenario: a
// planted event tagged "regulator-probe" carries the spec in its Payload; absent
// that, a "broken-step"/"cascade-injector"/"fan-out-trigger" stability scenario
// defaults to the fork-storm stimulus (the cascade the scenario plants). "" means
// "no probe" (leave the trace untouched).
func stabilityProbeSpec(scn benchtypes.TierBScenario) string {
	for _, pe := range scn.PlantedSchedule.Events {
		if pe.Tag == "regulator-probe" && strings.TrimSpace(pe.Payload) != "" {
			return strings.TrimSpace(pe.Payload)
		}
	}
	// A stability scenario that plants a cascade/fan-out/broken-step but names no
	// explicit probe defaults to the proven fork-storm stimulus.
	for _, pe := range scn.PlantedSchedule.Events {
		switch pe.Tag {
		case "broken-step", "cascade-injector", "fan-out-trigger":
			return "family=fork-storm"
		}
	}
	return ""
}

// runScenarioRaw is the engine-driving half: build one engine, thread the turns + planted
// schedule through it, collect the trace. Separated from scoring so a caller (the pilot
// admission gate) can score gate-on and gate-off together. perTurnBudget ≤ 0 ⇒ the default.
func runScenarioRaw(scn benchtypes.TierBScenario, arm benchtypes.Arm, seed int64, backendFactory runner.BackendFactory, workspace string, perTurnBudget int) ScenarioRun {
	if perTurnBudget <= 0 {
		perTurnBudget = DefaultPerTurnBudget
	}
	temp := runner.DefaultTemperature

	// The bare arm has no engine / no multi-turn state to thread: it is the linear base-model
	// loop, run via the existing runner (one Generate per turn, concatenated). This keeps the
	// bare baseline identical to the Tier-A bare arm (spec §5.1: the same base model alone).
	if arm == benchtypes.ArmBare || arm == benchtypes.ArmBareNoTools || arm == benchtypes.ArmBareRawTools {
		return runBareScenario(scn, arm, seed, backendFactory, temp)
	}

	// Resolve the arm's config: harness/gate-on = AllOn; gate-off = AllOn with exactly the
	// mechanism's ablation toggle(s) OFF. An unsupported gate-off returns honestly.
	cfg, unsupported, note := resolveConfig(arm, scn.Mechanism)
	if unsupported {
		return ScenarioRun{Arm: arm, Mechanism: scn.Mechanism, Seed: seed, Unsupported: true, Note: note}
	}

	backend := backendFactory(seed, temp)
	mode := "reactive"
	// continuous-autonomy scenarios run the awake loop (zero user turns after T0, planted ticks).
	if scn.Mechanism == benchtypes.MechContinuousAutonomy {
		mode = "continuous"
	}
	eng, err := engine.NewEngine(&engine.EngineConfig{
		Mode:      mode,
		Seed:      int(seed),
		MaxTicks:  perTurnBudget,
		Cognition: "control",
		Workspace: workspace,
		Features:  cfg,
	}, backend)
	if err != nil {
		return ScenarioRun{
			Arm: arm, Mechanism: scn.Mechanism, Seed: seed,
			Note: "engine construction failed: " + err.Error(),
		}
	}

	// One collector for the whole scenario; we snapshot its length at each turn boundary to split
	// the trace per turn. Subscribed BEFORE the first turn so the opening trace is captured.
	coll := newSink()
	unsub := eng.Bus().Subscribe(coll.add)
	defer unsub()

	injector := FixtureInjector(scn.PlantedSchedule.TelemetrySource)
	plantedByTurn := plantedEventsByTurn(scn)

	var turns []TurnTrace
	for _, turn := range scn.Turns {
		before := coll.len()
		// Inject any planted events scheduled to land WITH this turn (Tick <= 0, or tagged to this
		// turn via the turn's PlantedEvent). Then submit the turn and settle it.
		for _, pe := range plantedByTurn[turn.Index] {
			injector(eng, workspace, pe)
		}
		if strings.TrimSpace(turn.Text) != "" {
			eng.SubmitDefault(turn.Text)
		}
		eng.Run(perTurnBudget)
		turns = append(turns, TurnTrace{Index: turn.Index, Text: turn.Text, Events: coll.since(before)})
	}

	// Tick-scheduled planted events that did not bind to a turn (the out-of-band stretch: a
	// share-worthy tick, a dry-daydream stretch, a broken pipeline step) are delivered after the
	// scripted turns, each followed by a settle window, so the engine reacts to them. This is the
	// "across ticks, not turns" stimulus the spec §3.4/§3.5 forces.
	for _, pe := range plantedByTurn[0] {
		before := coll.len()
		injector(eng, workspace, pe)
		eng.Run(perTurnBudget)
		turns = append(turns, TurnTrace{Index: 0, Text: "[planted:" + pe.Tag + "]", Events: coll.since(before)})
	}

	all := coll.all()
	return ScenarioRun{
		Arm:       arm,
		Mechanism: scn.Mechanism,
		Seed:      seed,
		Turns:     turns,
		AllEvents: all,
		FinalText: strings.TrimSpace(eng.LastResponse()),
		Cost:      deriveCost(all),
		Note:      "engine: " + arm.String() + " (" + eng.BackendLabel() + ")",
	}
}

// runBareScenario runs the bare baseline across the scripted turns: one backend.Generate per
// turn, threading the prior turns' answers as the "history" context so the bare model gets the
// strong in-context baseline the spec §3.3 calls for (the question Tier-B asks is whether the
// harness's explicit machinery beats the bare model's implicit in-context reuse). No engine, no
// event bus — Events is empty, the isolation predicate cannot witness, so the bare arm's
// IsolationResult is always false (it has no mechanism to genuinely use).
func runBareScenario(scn benchtypes.TierBScenario, arm benchtypes.Arm, seed int64, backendFactory runner.BackendFactory, temp float64) ScenarioRun {
	bareRunner := runner.New(backendFactory, "")
	var turns []TurnTrace
	var last string
	var totalCalls int
	for _, turn := range scn.Turns {
		// Thread the prior answer into the prompt as lightweight history (the in-context baseline).
		prompt := turn.Text
		if last != "" {
			prompt = "Earlier you answered: " + last + "\nNow: " + turn.Text
		}
		ar := bareRunner.Run(runner.Spec{
			Prompt:      prompt,
			Arm:         arm,
			Mechanism:   scn.Mechanism,
			Seed:        seed,
			Temperature: temp,
		})
		last = ar.Text
		totalCalls += ar.Cost.ModelCalls
		turns = append(turns, TurnTrace{Index: turn.Index, Text: turn.Text, Events: nil})
	}
	return ScenarioRun{
		Arm:       arm,
		Mechanism: scn.Mechanism,
		Seed:      seed,
		Turns:     turns,
		AllEvents: nil,
		FinalText: strings.TrimSpace(last),
		Cost:      benchtypes.Cost{ModelCalls: totalCalls, Steps: len(scn.Turns), Tokens: 0},
		Note:      "bare: per-turn Generate (no harness, in-context history)",
	}
}

// scoreScenario applies the end-state oracle conjunction + the isolation predicate to a
// completed ScenarioRun and projects it into a types.ScenarioResult (the §5.7 ledger row). The
// Pass is the conjunction of every EndStateOracle AND'd with the isolation predicate where the
// mechanism requires it (a harness pass that bypasses the mechanism is excluded from the
// numerator — spec §1.4). An unsupported run is a clean Pass=false with the reason in RawOutput.
func scoreScenario(scn benchtypes.TierBScenario, run ScenarioRun) benchtypes.ScenarioResult {
	if run.Unsupported {
		return benchtypes.ScenarioResult{
			ID: scn.ID, Seed: run.Seed, Arm: run.Arm,
			Pass: false, OracleVerdict: false, IsolationResult: false,
			RawOutput: "UNSUPPORTED: " + run.Note, Cost: run.Cost,
		}
	}

	// End-state oracle conjunction (deterministic). Every oracle must pass.
	oracleVerdict := true
	var reasons []string
	for i, o := range scn.EndStateOracles {
		ok, reason := EvalOracle(o, run.FinalText, run.AllEvents)
		reasons = append(reasons, "oracle["+itoa(i)+"]("+o.Kind.String()+"): "+reason)
		if !ok {
			oracleVerdict = false
		}
	}
	// A scenario with no declared oracles is a contract gap — never a silent pass.
	if len(scn.EndStateOracles) == 0 {
		oracleVerdict = false
		reasons = append(reasons, "no end-state oracles declared (cannot pass)")
	}

	// Isolation: was the mechanism genuinely used? The bare arm has no trace to witness, so its
	// isolation is reported false but does NOT gate the bare pass (bare is scored on the answer
	// alone — it has no mechanism to use). The harness/gate arms must satisfy the isolation
	// predicate to count toward the lift numerator.
	isolation := isolationFor(scn, run)

	pass := oracleVerdict
	if isHarnessSide(run.Arm) {
		// On the harness side, a correct end-state that bypassed the mechanism is a bypass, excluded.
		pass = oracleVerdict && isolation.OK
	}

	return benchtypes.ScenarioResult{
		ID:              scn.ID,
		Seed:            run.Seed,
		Arm:             run.Arm,
		Pass:            pass,
		RawOutput:       run.FinalText + "\n--- " + strings.Join(reasons, "; ") + "\nisolation: " + isolation.Reason,
		OracleVerdict:   oracleVerdict,
		IsolationResult: isolation.OK,
		Cost:            run.Cost,
		// Per-call token usage for the cost report's per-ROLE / per-MODEL aggregation.
		Calls: runner.LLMCallsFromEvents(run.AllEvents),
	}
}

// isolationFor runs the scenario's IsolationPredicate over the run's trace. It prefers the
// scenario's explicit RequiredEvents (a TraceOracle-shaped check) when the scenario declares
// them; otherwise it falls back to the runner's per-mechanism witness registry (the DRY reuse
// the deliverable asks for). A bare arm (no trace) returns OK=false with a clear reason.
func isolationFor(scn benchtypes.TierBScenario, run ScenarioRun) runner.IsolationResult {
	if run.AllEvents == nil {
		return runner.IsolationResult{OK: false, Reason: "no event trace (bare arm has no mechanism to witness)"}
	}
	// Scenario-declared required-events take precedence (the scenario knows its own witness).
	if len(scn.IsolationPredicate.RequiredEvents) > 0 {
		req := benchtypes.TraceOracle{RequiredEvents: scn.IsolationPredicate.RequiredEvents}
		ok, reason := evalTrace(req, run.AllEvents)
		return runner.IsolationResult{OK: ok, Reason: "scenario-isolation(" + scn.IsolationPredicate.Kind + "): " + reason}
	}
	// Fall back to the runner's per-mechanism witness (grounding read / BACKTRACK / mint-reused /
	// regulator engaged / gate blocked).
	pred, ok := runner.PredicateFor(scn.Mechanism)
	if !ok {
		return runner.IsolationResult{OK: false, Reason: "no isolation predicate for " + scn.Mechanism.String()}
	}
	return pred(run.AllEvents)
}

// resolveConfig returns the Features config for an arm + mechanism: AllOn for harness/gate-on;
// AllOn with the mechanism's single ablation toggle(s) OFF for gate-off. An unsupported gate-off
// (no toggle yet — multi-step-retrace, continuous-autonomy) returns unsupported=true with the
// TODO note, mirroring runner.runGateOff so the two stay in lockstep.
func resolveConfig(arm benchtypes.Arm, mech benchtypes.Mechanism) (cfg *config.HarnessConfig, unsupported bool, note string) {
	switch arm {
	case benchtypes.ArmHarness, benchtypes.ArmGateOn:
		return config.New(), false, ""
	case benchtypes.ArmGateOff:
		toggles := runner.GateOffToggles(mech)
		if len(toggles) == 0 {
			return nil, true, "UNSUPPORTED-YET: no gate-off toggle for " + mech.String() +
				" — TODO: add the ablation flag (forbid Controller BACKTRACK / awake-regime off) " +
				"per measuring-stick-spec §5.8; the driver must NOT fake this arm"
		}
		c := config.New()
		for _, path := range toggles {
			if !config.ApplyToggle(c, path, false) {
				return nil, true, "gate-off: toggle path not found: " + path + " (config knob drift)"
			}
		}
		return c, false, ""
	case benchtypes.ArmSingleStrong:
		// the SUB-AGENT GUARD reference: the FULL engine with the per-tick sub-agent fan-out collapsed to its
		// single best member. ApplyToggle (not a direct field set) so a knob-path drift fails loudly rather
		// than silently running the full-fan-out (== harness) engine and making the guard vacuous — mirroring
		// runner.runSingleStrong so the two stay in lockstep.
		c := config.New()
		if !config.ApplyToggle(c, "subconscious.single_strong_agent", true) {
			return nil, true, "single-strong: toggle path not found: subconscious.single_strong_agent (config knob drift)"
		}
		return c, false, ""
	default:
		return config.New(), false, ""
	}
}

// plantedEventsByTurn buckets the scenario's planted events by the turn they attach to. An event
// whose Tick maps to a scripted turn's Index attaches to that turn (delivered before the turn's
// text); everything else goes to bucket 0 (delivered after the scripted turns, each in its own
// settle window). Turns carrying their own PlantedEvent tag also seed a bucket-0 stimulus so the
// turn-tagged plant (pushback/redirect/cascade) is injected even without a schedule entry.
func plantedEventsByTurn(scn benchtypes.TierBScenario) map[int][]benchtypes.PlantedEvent {
	out := map[int][]benchtypes.PlantedEvent{}
	turnIndices := map[int]bool{}
	for _, t := range scn.Turns {
		turnIndices[t.Index] = true
	}
	for _, pe := range scn.PlantedSchedule.Events {
		// A setup-fixture plant (RetraceFixtureTag) is NOT an out-of-band stimulus — it is the
		// failing-test bytes the driver materializes into the Action workspace BEFORE the run (see
		// RetraceFixture). It must never be injected as a percept, so skip it here.
		if pe.Tag == RetraceFixtureTag {
			continue
		}
		if pe.Tick > 0 && turnIndices[pe.Tick] {
			out[pe.Tick] = append(out[pe.Tick], pe)
		} else {
			out[0] = append(out[0], pe)
		}
	}
	// Keep bucket-0 in a stable (tick) order so injection is deterministic.
	sort.SliceStable(out[0], func(i, j int) bool { return out[0][i].Tick < out[0][j].Tick })
	return out
}

// isHarnessSide reports whether an arm is a harness/gate arm (whose pass is isolation-gated) as
// opposed to a bare baseline (scored on the answer alone). The single-strong arm (the sub-agent guard
// reference) IS a full engine — it keeps the graph/Controller/seams/gate, only collapsing the sub-agent
// fan-out — so it is isolation-gated like the other engine arms.
func isHarnessSide(arm benchtypes.Arm) bool {
	switch arm {
	case benchtypes.ArmHarness, benchtypes.ArmGateOn, benchtypes.ArmGateOff, benchtypes.ArmSingleStrong:
		return true
	default:
		return false
	}
}

// deriveCost computes the whole-scenario Cost from the flattened trace by delegating to the
// runner's shared DeriveCost — ONE source of truth for the token split (ModelCalls, Steps, and
// the uncached/cached/out/reasoning token breakdown). Kept as a thin local alias so the call
// sites read naturally.
func deriveCost(evs []events.Event) benchtypes.Cost {
	return runner.DeriveCost(evs)
}

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

// ensure the types import is used even if the bare-path percept source changes (the engine's
// Submit uses USER_INPUT internally; this keeps the import explicit for a future PERCEPT path).
var _ = types.USER_INPUT

// ensure backends is referenced (BackendFactory builds a backends.Backend); keeps the import
// list honest if the factory signature is inlined later.
var _ backends.Backend
