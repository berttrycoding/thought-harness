// Package scenarios holds the worked scenarios S1..S16 (spec §8) — the oracle fixtures of the port.
//
// Each Scenario is a small driver: a prompt (or sequence of episodes), an optional mid-run
// interrupt, a mode, and the subsystems it exercises. RunScenario builds (or reuses) an Engine,
// feeds it the scripted submits, and steps it — the trace (events on the bus) is the output, and
// for the golden-equivalence oracle (PORT-PLAN §3) that event stream is the cross-language
// conformance contract.
//
// Mirrors the Python scenarios/scenarios.py 1:1: the same 16 entries (titles, exercises, prompts,
// modes, injects, max_ticks) and the same drive loop (submit the first episode up front; on idle
// with nothing pending and no outstanding action, submit the next repeated episode or stop; fire an
// inject when the step's tick matches). Insertion order S1..S16 is preserved (Order/All), because the
// Python dict iterates in insertion order and the CLI `scenario --list` depends on it.
package scenarios

import "github.com/berttrycoding/thought-harness/internal/engine"

// Scenario is one worked scenario (Python @dataclass Scenario). Prompts are submitted as successive
// episodes (a new episode each); Injects are (afterTick, text) interrupts fired when a step returns
// that tick. The zero value of Mode means reactive and MaxTicks 0 means the default 28 — but every
// constructed Scenario here sets both explicitly, matching how Python's defaults bake in at build.
type Scenario struct {
	ID        string
	Title     string
	Exercises string
	Prompts   []string // episodes submitted in order (a new episode each)
	Mode      string   // "reactive" | "continuous"
	Injects   []Inject // mid-run interrupts
	MaxTicks  int      // step budget for this scenario
}

// Inject is a mid-run interrupt: submit Text after the step whose tick == AfterTick. Mirrors the
// Python (after_tick, text) tuple in Scenario.injects.
type Inject struct {
	AfterTick int
	Text      string
}

// scenarioList is the ordered S1..S16 table, in Python insertion order. byID indexes it for lookup.
// Both are built once at package init from the single source of truth (scenarioList) so the order and
// the map can never drift apart.
var scenarioList = []Scenario{
	{
		ID: "S1", Title: "Fluent recall (injection only)",
		Exercises: "Subconscious, Hidden Seam, Gate(singleton), Filter",
		Prompts:   []string{"What's 7×8?"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S2", Title: "Stuck / effortful (generation)",
		Exercises: "generation path, Critic, eventual BACKTRACK or ACT",
		Prompts:   []string{"Do the long division of 8472 by 31 by hand, step by step"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S3", Title: "Conflicting injections (branch)",
		Exercises: "Gate(forking), MCP.branch, Branch states",
		Prompts:   []string{"Is this refactor safe to ship?"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S4", Title: "Exhaustion -> backtrack (internal exit)",
		Exercises: "rerank, focus, compress/expand, value signal",
		Prompts:   []string{"Is this refactor safe to ship, weighing both sides?"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S5", Title: "Exhaustion -> act (external exit)",
		Exercises: "Watched Seam, Action, OBSERVATION, the opening",
		Prompts:   []string{"Will this code run correctly at runtime?"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S6", Title: "Workflow execution (design-build-validate)",
		Exercises: "Workflow, Operators, runtime instantiation, gate bias",
		Prompts:   []string{"Design a small API for a todo service"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S7", Title: "Convertibility (effortful -> automatic)",
		Exercises: "Convertibility subsystem, GENERATED->PrimitiveSubAgent",
		Prompts: []string{
			"Do the long division of 8472 by 31 step by step",
			"Do the long division of 8472 by 31 step by step",
			"Do the long division of 8472 by 31 step by step",
			"Do the long division of 8472 by 31 step by step",
		},
		Mode: "reactive", MaxTicks: 28,
	},
	{
		ID: "S8", Title: "Bad transform caught (validation guard)",
		Exercises: "Filter guarding the hidden seam, value as trust",
		Prompts:   []string{"Mentally simulate whether this expression simplifies — trust the hunch?"},
		Mode:      "reactive", MaxTicks: 28,
	},
	{
		ID: "S9", Title: "Merge (convergent branches)",
		Exercises: "MERGE decision, Branch MERGED terminal state",
		Prompts:   []string{"Is this refactor safe to ship, weighing both sides?"},
		Mode:      "reactive", MaxTicks: 22,
	},
	{
		ID: "S10", Title: "Metacognitive deliberate control",
		Exercises: "Thought MCP as conscious interface",
		Prompts:   []string{"Is this refactor safe to ship?"},
		Mode:      "reactive", MaxTicks: 10,
	},
	{
		ID: "S11", Title: "User interrupt mid-task",
		Exercises: "Interaction Port, interrupt policy, SUSPENDED, value re-seed",
		Prompts:   []string{"Is this refactor safe to ship, weighing both sides?"},
		Mode:      "reactive", MaxTicks: 28,
		Injects: []Inject{{AfterTick: 3, Text: "actually — stop, what's 7×8 first?"}},
	},
	{
		ID: "S12", Title: "Reaching idle + consolidation",
		Exercises: "lifecycle, quiescence, stopping taxonomy, idle consolidation",
		Prompts:   []string{"What's 12×6?"},
		Mode:      "reactive", MaxTicks: 12,
	},
	{
		ID: "S13", Title: "Bad input rejected at intake",
		Exercises: "Filter as admission guard, validate-before-voice",
		Prompts:   []string{"Is this refactor safe to ship, weighing both sides?"},
		Mode:      "reactive", MaxTicks: 28,
		// blank/malformed -> Filter REJECT at intake
		Injects: []Inject{{AfterTick: 3, Text: "   "}},
	},
	{
		ID: "S14", Title: "Mind-wandering -> unprompted idea (continuous)",
		Exercises: "Arousal, Drives, default-mode, self-direction",
		Prompts:   []string{},
		Mode:      "continuous", MaxTicks: 24,
	},
	{
		ID: "S15", Title: "Act while thinking (async action, continuous)",
		Exercises: "async watched seam, Perception Port, OUTSTANDING action",
		Prompts:   []string{"Will this code run correctly at runtime?"},
		Mode:      "continuous", MaxTicks: 20,
	},
	{
		ID: "S16", Title: "Falling asleep / waking (arousal transitions)",
		Exercises: "arousal-driven lifecycle, consolidation, perception gain",
		Prompts:   []string{},
		Mode:      "continuous", MaxTicks: 30,
		Injects: []Inject{{AfterTick: 14, Text: "wake up — is this refactor safe?"}},
	},
}

// byID indexes scenarioList by uppercase id for O(1) lookup (Python's SCENARIOS dict).
var byID = func() map[string]Scenario {
	m := make(map[string]Scenario, len(scenarioList))
	for _, sc := range scenarioList {
		m[sc.ID] = sc
	}
	return m
}()

// All returns the scenarios in insertion order S1..S16 (Python SCENARIOS.values()). The returned
// slice is a fresh copy, so a caller cannot mutate the package table.
func All() []Scenario {
	out := make([]Scenario, len(scenarioList))
	copy(out, scenarioList)
	return out
}

// Get looks up a scenario by id (case-insensitive). The second return is false when the id is
// unknown — the Go idiom for Python's KeyError, leaving the CLI to format its own message. Mirrors
// scenarios.get / SCENARIOS.get(name.upper()).
func Get(name string) (Scenario, bool) {
	sc, ok := byID[upper(name)]
	return sc, ok
}

// RunScenario drives a scenario to completion on a fresh (or supplied) engine, returning the engine
// (its bus holds the trace). Pass eng=nil to build a fresh heuristic-free engine from config; pass an
// engine to reuse one already wired to trace sinks (the CLI path). Mirrors Python run_scenario.
//
// It returns an error only on an unknown id or a failed engine build (Go's NewEngine can fail to
// resolve the substrate) — Python raised KeyError / let __init__ raise; the Go caller decides.
func RunScenario(name string, eng *engine.Engine) (*engine.Engine, error) {
	sc, ok := Get(name)
	if !ok {
		return nil, &UnknownScenarioError{Name: name}
	}

	if eng == nil {
		cfg := engine.DefaultConfig()
		cfg.Mode = sc.Mode
		built, err := engine.NewEngine(&cfg, nil)
		if err != nil {
			return nil, err
		}
		eng = built
	}
	if eng.Mode() != sc.Mode {
		eng.SetMode(sc.Mode)
	}

	// Build the after-tick -> text inject map (Python {t: msg for t, msg in sc.injects}).
	injects := make(map[int]string, len(sc.Injects))
	for _, in := range sc.Injects {
		injects[in.AfterTick] = in.Text
	}

	// Remaining prompts (episodes); the first is submitted up front, the rest when idle. An awake
	// scenario may have NO prompts at all — the engine self-seeds its wander (no synthetic kickoff).
	prompts := make([]string, len(sc.Prompts))
	copy(prompts, sc.Prompts)
	if len(prompts) > 0 {
		eng.SubmitDefault(prompts[0]) // salient=True (the Python submit default)
		prompts = prompts[1:]
	}

	for i := 0; i < sc.MaxTicks; i++ {
		res := eng.Step()
		if msg, ok := injects[res.Tick]; ok {
			eng.SubmitDefault(msg)
		}
		if res.Idle && !eng.PortPending() && !eng.HasOutstandingAction() {
			if len(prompts) > 0 {
				next := prompts[0]
				prompts = prompts[1:]
				eng.SubmitDefault(next) // next repeated episode (S7)
			} else {
				break
			}
		}
	}
	return eng, nil
}

// UnknownScenarioError is returned by Get-backed callers when an id is not S1..S16. It mirrors the
// Python KeyError message ("unknown scenario 'x'; known: S1, S2, ...") so the CLI text matches.
type UnknownScenarioError struct{ Name string }

func (e *UnknownScenarioError) Error() string {
	known := make([]byte, 0, len(scenarioList)*5)
	for i, sc := range scenarioList {
		if i > 0 {
			known = append(known, ", "...)
		}
		known = append(known, sc.ID...)
	}
	return "unknown scenario " + pyRepr(e.Name) + "; known: " + string(known)
}

// upper uppercases an ASCII scenario id (the ids are ASCII "S1".."S16"); a tiny local helper avoids a
// strings import for one call and keeps the lookup allocation-free for the common already-upper case.
func upper(s string) string {
	hasLower := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			hasLower = true
			break
		}
	}
	if !hasLower {
		return s
	}
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

// pyRepr wraps a string in single quotes the way Python's repr does for the error message (the ids
// here never contain quotes, so a plain wrap is faithful).
func pyRepr(s string) string { return "'" + s + "'" }
