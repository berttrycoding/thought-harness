package synthfidelity

import (
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// Expect is the structural spec a synthesised Program must satisfy to be FAITHFUL
// to a goal. Every field is a STRUCTURAL property of the Program tree (operator
// names, cognitive families/moves, control-flow node kinds, implied tool-scope) —
// nothing about surface text — so the oracle is a pure function of the tree and is
// fully deterministic and offline-vettable.
//
// The fields are derived from the operator catalog (cognition.seedOps: each op's
// Family + Move + ToolScope) and the seeded skill/agent specs (the gold
// decompositions, target-spec §1). The "structure-forces-faculty" principle (§1.1)
// is encoded here: RequireShapes naming "par" is how the Deliberator's BRANCH
// faculty is required; an ActOnReality step with a tool-scope is how the
// Investigator/Verifier's ACT faculty is required.
type Expect struct {
	// MustOperators are operator names that MUST appear as a Step somewhere in the
	// tree (e.g. "decompose", "compare", "contrast", "rank"). The single strongest
	// signal that the right MOVES were chosen. Empty = no per-operator requirement.
	MustOperators []string `json:"must_operators,omitempty"`
	// ForbidOperators are operator names that MUST NOT appear — the wrong move a
	// plausible-but-wrong synthesis would pick (e.g. a comparison goal that wrongly
	// reaches for "generate" instead of "compare/contrast"). Naming a forbidden op
	// is the cheapest discriminator a bad program trips.
	ForbidOperators []string `json:"forbid_operators,omitempty"`
	// MustFamilies are operator FAMILIES (transformative/relational/generative/
	// primitive/synthesized) at least one of whose members must appear. This scores
	// the cognitive FACULTY mix at one rung above bare operator names — a faithful
	// comparison uses a "relational" op, a faithful design uses a "generative" one —
	// so a near-miss that picked a same-family sibling still scores partial credit.
	MustFamilies []string `json:"must_families,omitempty"`
	// MustMoves are abstraction-ladder MOVES (ground/lift/reframe/transcode/assess)
	// at least one of whose ops must appear. The faculty signal the target-spec's
	// representation-space track keys on: a design forces a GROUND, a deliberation
	// forces a REFRAME, every workflow that closes a loop forces an ASSESS.
	MustMoves []string `json:"must_moves,omitempty"`
	// RequireShapes are control-flow node KINDS ("seq"|"par"|"loop") that MUST be
	// present in the tree. This is the load-bearing structure-forces-faculty check:
	// "par" REQUIRES the Deliberator's branch, "loop" REQUIRES the refine cycle. A
	// bad program that flattens a par into a seq fails here even with the right ops.
	RequireShapes []string `json:"require_shapes,omitempty"`
	// ForbidShapes are control-flow node kinds that MUST NOT appear — e.g. a
	// straight design-build pipeline should NOT contain a loop. Empty = unconstrained.
	ForbidShapes []string `json:"forbid_shapes,omitempty"`
	// MinSteps / MaxSteps bound the total operator-step count, so a degenerate
	// one-step "synthesis" or a runaway one is caught structurally. 0 = unbounded on
	// that side.
	MinSteps int `json:"min_steps,omitempty"`
	MaxSteps int `json:"max_steps,omitempty"`
	// ActOnReality, when true, REQUIRES at least one Step that crosses to reality —
	// either an operator carrying a non-empty ToolScope (a real tool dispatch) OR a
	// Step whose Source is "reality"/"compute" (the watched-seam/grounded rung). This
	// is how the Grounded Investigator's and the Verifier's ACT faculty is required:
	// a synthesis that only reasons (no act) is plausible-but-ungrounded and fails.
	ActOnReality bool `json:"act_on_reality,omitempty"`
	// MustToolScope are tool names the synthesised sub-agents (the Steps' operators)
	// must collectively be able to dispatch — the sub-agent tool-scope fidelity check
	// (§3 stretch "matching tool-scope"). Each named tool must be in the ToolScope of
	// at least one operator the program uses. Empty = no tool-scope requirement (a
	// pure-cognition worker like the Deliberator).
	MustToolScope []string `json:"must_tool_scope,omitempty"`
}

// Weights are the per-criterion weights the oracle sums into a [0,1] fidelity
// score. Each weight applies only when its criterion is actually constrained by the
// fixture's Expect (an empty MustOperators contributes neither weight nor credit),
// and the realised weights are renormalised to the constrained set so a fixture
// that constrains only shape+ops is still scored on [0,1]. The defaults front-load
// the two load-bearing signals — the right operators and the right control-flow
// shape — because those are what "structure-forces-faculty" turns on.
type Weights struct {
	Operators float64 `json:"operators,omitempty"`  // MustOperators coverage
	Forbid    float64 `json:"forbid,omitempty"`     // ForbidOperators / ForbidShapes respected
	Families  float64 `json:"families,omitempty"`   // MustFamilies coverage
	Moves     float64 `json:"moves,omitempty"`      // MustMoves coverage
	Shapes    float64 `json:"shapes,omitempty"`     // RequireShapes coverage
	Steps     float64 `json:"steps,omitempty"`      // step-count bounds satisfied
	Act       float64 `json:"act,omitempty"`        // ActOnReality satisfied
	ToolScope float64 `json:"tool_scope,omitempty"` // MustToolScope coverage
}

// DefaultWeights front-loads operators + shape (the structure-forces-faculty
// signals) and the forbid criterion (the discriminator a bad program trips), with
// families/moves/act/tool-scope/steps as supporting signal. They need not sum to 1
// — the oracle renormalises over the criteria a given fixture actually constrains.
func DefaultWeights() Weights {
	return Weights{
		Operators: 3.0,
		Forbid:    3.0,
		Shapes:    3.0,
		Families:  1.0,
		Moves:     1.0,
		Act:       2.0,
		ToolScope: 2.0,
		Steps:     0.5,
	}
}

// ProgramShape is a JSON-serializable program tree the fixture carries as its
// known-GOOD or known-BAD synthesis. It mirrors the "kind"-discriminated node dict
// cognition.NodeToDict / NodeFromDict round-trips, so a fixture's GoodProgram is
// parsed by the SAME decoder a live LLM-written program goes through — the oracle
// never sees a Go type the engine would not. A nil/empty map means "no program of
// this kind on this fixture" (used by Drive() for the synthesiser's actual output).
type ProgramShape map[string]any

// Fixture is one goal -> expected-synthesis case. The PassThreshold is the fidelity
// score at or above which the synthesis is judged FAITHFUL (a structural pass). The
// Good/Bad programs are the fail-discriminating control: Good must score >= the
// threshold, Bad must score < it.
type Fixture struct {
	// ID is the stable fixture identifier (e.g. "sf-deliberator-0001").
	ID string `json:"id"`
	// Worker is the target-spec agent this fixture probes ("deliberator" |
	// "grounded-investigator" | "verifier" | "design-build" | "analysis" | "refine"),
	// for the report's per-worker rollup.
	Worker string `json:"worker"`
	// Goal is the request handed to cognition.Synthesize (Drive() runs the REAL
	// synthesiser on it; the oracle scores the resulting program).
	Goal string `json:"goal"`
	// Expect is the structural spec a faithful synthesis must satisfy.
	Expect Expect `json:"expect"`
	// PassThreshold is the fidelity score >= which the synthesis is a structural
	// pass (default 0.75 when zero — set per fixture for a sharper/looser bar).
	PassThreshold float64 `json:"pass_threshold,omitempty"`
	// GoodProgram is a hand-written FAITHFUL synthesis (the gold decomposition) — it
	// MUST score >= PassThreshold. The fail-discriminating control's positive side.
	GoodProgram ProgramShape `json:"good_program"`
	// BadProgram is a hand-written PLAUSIBLE-BUT-WRONG synthesis (right vocabulary,
	// wrong structure) — it MUST score < PassThreshold. The control's negative side.
	BadProgram ProgramShape `json:"bad_program"`
	// SynthesiserCovers records (in the bank, as authored truth) whether the REAL
	// synthesiser is EXPECTED to produce a faithful program for this goal on the
	// offline test double. false = a KNOWN capability GAP (the synthesiser has no
	// shape for this goal yet — the §3 "a miss is a precise, rankable capability
	// gap"). Drive() measures the actual fidelity; this is the authored expectation
	// the report flags drift against.
	SynthesiserCovers bool `json:"synthesiser_covers"`
	// Note is a human-readable one-liner on what the fixture probes / why the bad
	// program is a plausible miss.
	Note string `json:"note,omitempty"`
}

// Mechanism is the benchtypes tag every synth-fidelity fixture/result carries, so
// the bench registration + report rollup can switch on it exactly as the six
// task-outcome mechanisms do.
func (Fixture) Mechanism() benchtypes.Mechanism { return benchtypes.MechSynthFidelity }
