package main

// compare.go — the head-to-head diagnostic (PORT-PLAN #42, Python thought_harness/compare.py).
//
// Runs the decision-rich canonical scenarios under each cognition mode (heuristic / blind-llm /
// smart-hybrid Controller) and measures:
//   - correctness   — did it still make the scenario's known-correct key decision?
//   - agreement     — how often does the model agree with the control floor? (high -> the floor is
//                     right for the clear cases, so the cheap default is justified)
//   - cost          — model decision-calls (control=0, llm=1/decision, hybrid=1/ambiguous-decision)
//   - disagreements — the exact decisions where the control floor and the model differ (what the
//                     hybrid escalates; the workflow judges whether the model was actually better there)
//
// The proof: if the heuristic is correct on the clear cases AND the model mostly agrees there, while
// the hybrid escalates only the few ambiguous decisions and matches the model's quality at a fraction
// of the cost, then "heuristic fast-path + model on the hard cases" is the right design.
//
// Lives in package main alongside the rest of the CLI (the Python module was thought_harness/compare.py
// with the argparse glue in __main__._cmd_compare). RunComparison is the exported entry the compare
// subcommand calls; the printing/JSON-dump glue stays in the subcommand handler.

import (
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/critic"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
)

// expected maps a scenario id -> the key decision a correct cognition MUST make in it (spec §8).
// Iteration order is never relied upon (Python EXPECTED is a dict read only via .get), so a plain map
// is faithful. The membership test made_expected reads it; an unknown id yields "" (no requirement).
var expected = map[string]string{
	"S1": "DELIVER", // fluent recall: the answer arrives and is SPOKEN — closing a user line is DELIVER (A2)
	"S3": "BRANCH",  // conflicting injections: fork both views
	"S5": "ACT",     // stuck: open to reality
	"S8": "ACT",     // mental-simulation hunch (the canned `simulation` fake) is DELETED (M2): the honest
	//                 cognition reasons, exhausts the closed loop, then opens to reality — ACT, not a
	//                 fork around a fabricated hunch. The "validation guard" property moved to the Filter
	//                 distrusting fabricated fuel (M3) rather than branching to verify a canned guess.
	"S6": "DELIVER", // workflow runs its phases, then the user's answer is delivered (A2)
}

// BackendFactory builds a fresh decision backend for one run (Python's `backend_factory` callable;
// the CLI's factory returns an OpenAICompatBackend). nil means "no model" — the llm/hybrid runs then
// degrade to the heuristic exactly as Python's `if mode in (...) and backend_factory` guard does.
type BackendFactory func() backends.Backend

// decisionRecord is one entry of Run.Decisions — the fields the Python per-decision dict carried
// (`{k: m.get(k) for k in ("decision","heuristic_decision","llm_decision","escalated","agree",
// "ambiguity","reason")}`). LLMSet distinguishes Python's "llm_decision is None" (not computed) from a
// real model pick, so NCompared/NAgree/Disagreements reproduce the `m.get(...) is None / is True`
// truthiness exactly (a bare false LLMDecision/Agree must not count as a comparison).
type decisionRecord struct {
	Decision          string  // the FINAL decision name (may be the escalated llm pick)
	HeuristicDecision string  // the heuristic's own pick (never overwritten)
	LLMDecision       string  // the model's pick name (only meaningful when LLMSet)
	LLMSet            bool    // whether the model was actually consulted this decision (Python: key present)
	Escalated         bool    // whether this decision was escalated to the model
	Agree             bool    // llm_decision == heuristic decision (only meaningful when LLMSet)
	Ambiguity         float64 // round(ambiguity, 2) — how close to a genuine judgment threshold
	Reason            string  // the heuristic's one-line rationale
}

// Run holds one (scenario, mode) outcome — the per-run record Python's @dataclass Run carried, with
// the computed @property methods ported as methods (MadeExpected/Disagreements/NCompared/NAgree).
type Run struct {
	Scenario    string
	Mode        string
	Decisions   []decisionRecord
	FinalState  string
	NDecisions  int
	Escalations int
	LLMCalls    int
	Fallbacks   int
}

// MadeExpected reports whether the run made the scenario's known-correct key decision somewhere in its
// decision trace (Python Run.made_expected: `EXPECTED.get(scenario) in {d["decision"] for d in ...}`).
// A scenario with no expectation ("" from the map) is satisfied only if some decision was literally ""
// — which never happens — so it reports false, matching Python where `None in {...}` is false.
func (r *Run) MadeExpected() bool {
	want, ok := expected[r.Scenario]
	if !ok {
		return false // Python: EXPECTED.get(...) is None, and None is never in the decision set
	}
	for _, d := range r.Decisions {
		if d.Decision == want {
			return true
		}
	}
	return false
}

// Disagreements returns the decisions the model was consulted on AND disagreed with the heuristic —
// exactly what the hybrid escalates (Python Run.disagreements:
// `[d for d in decisions if d.get("escalated") and d.get("agree") is False]`). Note `agree is False`
// is True only when the model was consulted and disagreed; a not-consulted decision (LLMSet=false,
// Agree zero-valued) must NOT count, which Python got via `m.get("agree")` being None there.
func (r *Run) Disagreements() []decisionRecord {
	var out []decisionRecord
	for _, d := range r.Decisions {
		if d.Escalated && d.LLMSet && !d.Agree {
			out = append(out, d)
		}
	}
	return out
}

// NCompared counts the decisions where BOTH heuristic and model were computed (Python Run.n_compared:
// `sum(1 for d in decisions if d.get("llm_decision") is not None)`). LLMSet is the Go truthiness of
// "llm_decision is not None".
func (r *Run) NCompared() int {
	n := 0
	for _, d := range r.Decisions {
		if d.LLMSet {
			n++
		}
	}
	return n
}

// NAgree counts the decisions where the model agreed with the heuristic (Python Run.n_agree:
// `sum(1 for d in decisions if d.get("agree") is True)`). Only a consulted decision can agree, so the
// LLMSet guard reproduces `agree is True` (a zero-valued Agree on a not-consulted decision is not True
// in Python because the key is absent -> None).
func (r *Run) NAgree() int {
	n := 0
	for _, d := range r.Decisions {
		if d.LLMSet && d.Agree {
			n++
		}
	}
	return n
}

// RunOne drives ONE (scenario, mode) head-to-head. Mirrors Python compare.run_one.
//
// Controlled experiment: hold the WHOLE engine deterministic/heuristic (fast, reproducible) and vary
// ONLY the Controller's decision backend. So we measure the decision organ in isolation, not a tangle
// of every component being LLM at once — and it stays tractable on a local model. maxTicks defaults to
// 24 when <= 0 (Python's `max_ticks: int = 24`), then is clamped to the scenario's own budget.
//
// factory may be nil (Python `backend_factory=None`): the llm/hybrid modes then run with the heuristic
// Controller (NewController falls back to heuristic without a Decider), so RunOne never requires a
// model — exactly as Python's `if mode in (...) and backend_factory:` guard.
func RunOne(scenario, mode string, factory BackendFactory, maxTicks int) Run {
	if maxTicks <= 0 {
		maxTicks = 24
	}
	r := Run{Scenario: scenario, Mode: mode}

	sc, ok := scenarios.Get(scenario)
	if !ok {
		return r // unknown scenario -> an empty run (the CLI validates ids before calling)
	}

	// Build the engine on the explicit TestBackend test double, in the scenario's mode, seed 7,
	// cognition="control" — the whole engine stays on the deterministic floor; only the Controller's
	// decision backend is swapped below. (Python: EngineConfig(mode=sc.mode, cognition="control",
	// seed=7), TestBackend().)
	cfg := engine.DefaultConfig()
	cfg.Mode = sc.Mode
	cfg.Cognition = "control"
	cfg.Seed = 7
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		return r // a failed engine build -> an empty run (the test-double build does not hit the network)
	}

	// Swap the decision organ for the llm/hybrid head-to-head: a fresh model backend wired to the bus,
	// then re-enable the requested mode on the Controller (the ctor pinned control because the engine
	// backend has no Decider). If the factory is nil, the Controller stays on the control floor.
	var decisionBackend backends.Backend
	if (mode == "llm" || mode == "hybrid") && factory != nil {
		decisionBackend = factory()
		if eb, ok := decisionBackend.(backends.EmitBinder); ok {
			eb.BindEmit(eng.Bus().Emit) // bind_emit so its llm.call/llm.fallback events show on the bus
		}
		eng.Controller().Reconfigure(decisionBackend, mode)
	}

	// Submit the scenario's first prompt (the head-to-head uses only the opening episode, like Python's
	// sc.prompts[0]) and step it, capturing each new Controller decision's record.
	if len(sc.Prompts) > 0 {
		eng.SubmitDefault(sc.Prompts[0])
	}
	ctrl := eng.Controller()
	budget := maxTicks
	if sc.MaxTicks < budget {
		budget = sc.MaxTicks // Python range(min(max_ticks, sc.max_ticks))
	}
	seen := 0
	for i := 0; i < budget; i++ {
		res := eng.Step()
		if ctrl.Decisions > seen { // a new decision was made this step -> record it
			seen = ctrl.Decisions
			r.Decisions = append(r.Decisions, recordFromMeta(ctrl.LastMeta))
		}
		if res.Idle && !eng.PortPending() && !eng.HasOutstandingAction() {
			break
		}
	}

	r.FinalState = eng.LifecycleState()
	r.NDecisions = ctrl.Decisions
	r.Escalations = ctrl.Escalations
	r.LLMCalls = backendCalls(decisionBackend)      // getattr(backend, "calls", 0)
	r.Fallbacks = backendFallbacks(decisionBackend) // getattr(backend, "fallbacks", 0)
	return r
}

// recordFromMeta projects the Controller's last_meta struct onto the per-decision record Python built
// from `m.get(k)`. LLMSet mirrors Python's "the llm_decision/agree keys are present" (set only on an
// actual escalation) so the None-vs-value truthiness downstream is preserved.
func recordFromMeta(m critic.ControllerMeta) decisionRecord {
	return decisionRecord{
		Decision:          m.Decision,
		HeuristicDecision: m.HeuristicDecision,
		LLMDecision:       m.LLMDecision,
		LLMSet:            m.LLMSet,
		Escalated:         m.Escalated,
		Agree:             m.Agree,
		Ambiguity:         m.Ambiguity,
		Reason:            m.Reason,
	}
}

// RunComparison runs the full grid of (scenario × mode) and summarizes it. Mirrors Python
// compare.run_comparison(scenarios, backend_factory, modes=("control","llm","hybrid")). Pass nil
// modes for that default triple. maxTicks <= 0 uses the Python per-run default (24, clamped per scenario).
func RunComparison(scenarioIDs []string, factory BackendFactory, modes []string, maxTicks int) ComparisonResult {
	if modes == nil {
		modes = []string{"control", "llm", "hybrid"}
	}
	var runs []Run
	for _, sc := range scenarioIDs {
		for _, mode := range modes {
			runs = append(runs, RunOne(sc, mode, factory, maxTicks))
		}
	}
	return Summarize(runs)
}

// ModeSummary is the per-mode aggregate (Python summarize()'s per-mode dict). Agreement is *float64
// because Python set it to None when no decision was compared (compared==0); a nil pointer is that None.
type ModeSummary struct {
	Correct     int      // runs that made the expected key decision
	Total       int      // runs in this mode
	Decisions   int      // total Controller decisions across the runs
	Escalations int      // total escalations to the model
	LLMCalls    int      // total model decision-calls
	Fallbacks   int      // total model fallbacks to the heuristic
	Agreement   *float64 // agree/compared, or nil (Python None) when compared==0
	Compared    int      // decisions where both heuristic and model were computed
}

// ComparisonResult is the whole head-to-head outcome (Python summarize()'s
// {"summary","runs","disagreements"} dict). Disagreements flattens every run's disagreements with the
// scenario+mode prepended, matching Python's `{"scenario": r.scenario, "mode": r.mode, **d}`.
type ComparisonResult struct {
	Summary       map[string]ModeSummary
	Runs          []Run
	Disagreements []Disagreement
}

// Disagreement is one escalated decision where the model disagreed with the heuristic — the rows the
// CLI prints under "Disagreements". It carries the scenario/mode plus the decision record's fields the
// Python output read (heuristic_decision / llm_decision / ambiguity / reason).
type Disagreement struct {
	Scenario          string
	Mode              string
	Decision          string
	HeuristicDecision string
	LLMDecision       string
	Ambiguity         float64
	Reason            string
}

// Summarize aggregates the runs by mode and collects the disagreements. Mirrors Python compare.summarize.
// Mode iteration order: it bins by mode in first-seen order (Python by_mode.setdefault preserves run
// order); the map is keyed by mode so the CLI reads a known mode triple regardless of order.
func Summarize(runs []Run) ComparisonResult {
	byMode := map[string][]Run{}
	for i := range runs {
		r := runs[i]
		byMode[r.Mode] = append(byMode[r.Mode], r)
	}
	summary := make(map[string]ModeSummary, len(byMode))
	for mode, rs := range byMode {
		compared, agree := 0, 0
		ms := ModeSummary{Total: len(rs)}
		for i := range rs {
			r := &rs[i]
			compared += r.NCompared()
			agree += r.NAgree()
			if r.MadeExpected() {
				ms.Correct++
			}
			ms.Decisions += r.NDecisions
			ms.Escalations += r.Escalations
			ms.LLMCalls += r.LLMCalls
			ms.Fallbacks += r.Fallbacks
		}
		ms.Compared = compared
		if compared > 0 { // Python: (agree / compared) if compared else None
			a := float64(agree) / float64(compared)
			ms.Agreement = &a
		}
		summary[mode] = ms
	}

	var disagreements []Disagreement
	for i := range runs {
		r := &runs[i]
		for _, d := range r.Disagreements() {
			disagreements = append(disagreements, Disagreement{
				Scenario:          r.Scenario,
				Mode:              r.Mode,
				Decision:          d.Decision,
				HeuristicDecision: d.HeuristicDecision,
				LLMDecision:       d.LLMDecision,
				Ambiguity:         d.Ambiguity,
				Reason:            d.Reason,
			})
		}
	}
	return ComparisonResult{Summary: summary, Runs: runs, Disagreements: disagreements}
}

// backendCalls reads the model's decision-call count (Python `getattr(backend, "calls", 0)`). Only the
// concrete OpenAI-compat backend (bare or tiered) carries it; anything else (nil / heuristic) is 0.
func backendCalls(b backends.Backend) int {
	switch v := b.(type) {
	case *llm.OpenAICompatBackend:
		return v.Calls
	case *llm.TieredBackend:
		return v.Primary.Calls // Python __getattr__ forwarded tiered.calls to the primary
	default:
		return 0
	}
}

// backendFallbacks reads the model's fallback count (Python `getattr(backend, "fallbacks", 0)`) —
// the times a model call did not complete. The CONTROL path no longer falls back through this
// backend (rank is pure control; the admission FLOOR is control.ScoreAdmit; the Filter escalation
// declining lets the floor stand, not a backend fallback). For this Controller head-to-head it
// counts the Controller.decide calls that the model declined and the heuristic decision stood.
func backendFallbacks(b backends.Backend) int {
	switch v := b.(type) {
	case *llm.OpenAICompatBackend:
		return v.Fallbacks
	case *llm.TieredBackend:
		return v.Primary.Fallbacks
	default:
		return 0
	}
}
