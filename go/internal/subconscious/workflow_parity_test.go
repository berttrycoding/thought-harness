package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// wfPhase is the expected shape of one workflow phase: the operator name + enum, the schedule flags
// (parallel / loop-label / iteration), the Gate bias, and the SubAgent set (id/role/domain) the phase
// instantiates. All values are captured from the Python golden (Workflow.from_program +
// instantiate, no executor/cognition ⇒ reason-only sub-agents).
type wfPhase struct {
	opName      string
	operator    string // Phase.Operator.String()
	parallel    bool
	loop        string // "" == None
	iteration   int
	bias        map[string]float64
	subagentIDs []string // per-step "sa:<op>:<domain>@<i>.<k>"
	roles       []string
	domains     []string
}

// wfCase pins one goal's whole workflow: its name, bespoke flag, and ordered phases.
type wfCase struct {
	goal    string
	name    string
	bespoke bool
	phases  []wfPhase
}

// TestWorkflowWalksSamePhasesParity is the Tier-5 gate (PORT-PLAN §2 row 5): the Workflow built from a
// synthesised program walks the SAME phases AND instantiates the SAME SubAgent set per phase. STRONG
// (structural-exact) parity: the phase plan + sub-agent ids/roles/domains are RNG-free and template
// driven, so they compare exactly to the Python golden (captured by running
// Workflow.from_program(recognize_shape(goal), cat, be).instantiate per phase).
func TestWorkflowWalksSamePhasesParity(t *testing.T) {
	cases := []wfCase{
		{
			goal: "compare A and B", name: "seq(decompose, par(compare, contrast), rank)", bespoke: true,
			phases: []wfPhase{
				{opName: "decompose", operator: "DECOMPOSE", parallel: false, loop: "", iteration: 0,
					bias:        map[string]float64{"decompose": 0.3},
					subagentIDs: []string{"sa:decompose:general@0.0"}, roles: []string{"decompose"}, domains: []string{"general"}},
				{opName: "compare", operator: "COMPARE", parallel: true, loop: "", iteration: 0,
					bias:        map[string]float64{},
					subagentIDs: []string{"sa:compare:general@0.0", "sa:contrast:general@0.1"},
					roles:       []string{"compare", "contrast"}, domains: []string{"general", "general"}},
				{opName: "rank", operator: "GENERATE", parallel: false, loop: "", iteration: 0,
					bias:        map[string]float64{},
					subagentIDs: []string{"sa:rank:general@0.0"}, roles: []string{"rank"}, domains: []string{"general"}},
			},
		},
		{
			goal: "optimize the loop", name: "loop(seq(measure, eliminate))", bespoke: true,
			phases: []wfPhase{
				{opName: "measure", operator: "VALIDATE", parallel: false, loop: "loop@0", iteration: 0,
					bias:        map[string]float64{"skeptic": 0.3},
					subagentIDs: []string{"sa:measure:general@0.0"}, roles: []string{"measure"}, domains: []string{"general"}},
				{opName: "eliminate", operator: "GENERATE", parallel: false, loop: "loop@0", iteration: 0,
					bias:        map[string]float64{},
					subagentIDs: []string{"sa:eliminate:general@0.0"}, roles: []string{"eliminate"}, domains: []string{"general"}},
				{opName: "measure", operator: "VALIDATE", parallel: false, loop: "loop@0", iteration: 1,
					bias:        map[string]float64{"skeptic": 0.3},
					subagentIDs: []string{"sa:measure:general@0.0"}, roles: []string{"measure"}, domains: []string{"general"}},
				{opName: "eliminate", operator: "GENERATE", parallel: false, loop: "loop@0", iteration: 1,
					bias:        map[string]float64{},
					subagentIDs: []string{"sa:eliminate:general@0.0"}, roles: []string{"eliminate"}, domains: []string{"general"}},
				{opName: "measure", operator: "VALIDATE", parallel: false, loop: "loop@0", iteration: 2,
					bias:        map[string]float64{"skeptic": 0.3},
					subagentIDs: []string{"sa:measure:general@0.0"}, roles: []string{"measure"}, domains: []string{"general"}},
				{opName: "eliminate", operator: "GENERATE", parallel: false, loop: "loop@0", iteration: 2,
					bias:        map[string]float64{},
					subagentIDs: []string{"sa:eliminate:general@0.0"}, roles: []string{"eliminate"}, domains: []string{"general"}},
			},
		},
		{
			goal: "design a service", name: "seq(decompose, generate, validate)", bespoke: true,
			phases: []wfPhase{
				{opName: "decompose", operator: "DECOMPOSE", parallel: false, loop: "", iteration: 0,
					bias:        map[string]float64{"decompose": 0.3},
					subagentIDs: []string{"sa:decompose:planning@0.0"}, roles: []string{"decompose"}, domains: []string{"planning"}},
				{opName: "generate", operator: "GENERATE", parallel: false, loop: "", iteration: 0,
					bias:        map[string]float64{"advocate": 0.2},
					subagentIDs: []string{"sa:generate:planning@0.0"}, roles: []string{"generate"}, domains: []string{"planning"}},
				{opName: "validate", operator: "VALIDATE", parallel: false, loop: "", iteration: 0,
					bias:        map[string]float64{"skeptic": 0.4},
					subagentIDs: []string{"sa:validate:planning@0.0"}, roles: []string{"validate"}, domains: []string{"planning"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.goal, func(t *testing.T) {
			cat := cognition.NewOperatorRegistry()
			be := backends.NewTest()
			prog, ok := cognition.RecognizeShape(tc.goal, nil)
			if !ok {
				t.Fatalf("RecognizeShape(%q) found no shape", tc.goal)
			}
			wf := FromProgram(prog, cat, be, nil, tc.goal)

			if wf.Name != tc.name {
				t.Errorf("workflow name = %q, want %q", wf.Name, tc.name)
			}
			if wf.Bespoke != tc.bespoke {
				t.Errorf("workflow bespoke = %v, want %v", wf.Bespoke, tc.bespoke)
			}
			if len(wf.Phases) != len(tc.phases) {
				t.Fatalf("workflow has %d phases, want %d", len(wf.Phases), len(tc.phases))
			}

			for i, want := range tc.phases {
				ph := wf.Phases[i]
				if ph.OpName != want.opName {
					t.Errorf("phase[%d].OpName = %q, want %q", i, ph.OpName, want.opName)
				}
				if ph.Operator.String() != want.operator {
					t.Errorf("phase[%d].Operator = %s, want %s", i, ph.Operator.String(), want.operator)
				}
				if ph.Plan.Parallel != want.parallel {
					t.Errorf("phase[%d].Parallel = %v, want %v", i, ph.Plan.Parallel, want.parallel)
				}
				gotLoop := ""
				if ph.Plan.Loop != nil {
					gotLoop = *ph.Plan.Loop
				}
				if gotLoop != want.loop {
					t.Errorf("phase[%d].Loop = %q, want %q", i, gotLoop, want.loop)
				}
				if ph.Plan.Iteration != want.iteration {
					t.Errorf("phase[%d].Iteration = %d, want %d", i, ph.Plan.Iteration, want.iteration)
				}
				if !biasEqual(ph.Bias, want.bias) {
					t.Errorf("phase[%d].Bias = %v, want %v", i, ph.Bias, want.bias)
				}

				// SubAgent set parity: one per step (a parallel group ⇒ several), in order.
				sas := wf.Instantiate(ph, nil, nil) // reason-only (no executor/cognition)
				if len(sas) != len(want.subagentIDs) {
					t.Fatalf("phase[%d] instantiated %d sub-agents, want %d", i, len(sas), len(want.subagentIDs))
				}
				for k, sa := range sas {
					if sa.id != want.subagentIDs[k] {
						t.Errorf("phase[%d].subagent[%d].id = %q, want %q", i, k, sa.id, want.subagentIDs[k])
					}
					if sa.Role() != want.roles[k] {
						t.Errorf("phase[%d].subagent[%d].Role = %q, want %q", i, k, sa.Role(), want.roles[k])
					}
					if sa.Domain() != want.domains[k] {
						t.Errorf("phase[%d].subagent[%d].Domain = %q, want %q", i, k, sa.Domain(), want.domains[k])
					}
				}
			}
		})
	}
}

// TestWorkflowGateBiasOrderingParity confirms the load-bearing Recognize→GateBias ordering: GateBias
// returns the current phase's bias ONLY after Recognize sets the cached recognized flag, else {}
// (Python gate_bias: dict(current().bias) if recognized else {}). A bespoke workflow recognises on
// any context (it was synthesised for this goal); before Recognize, the flag is false ⇒ {}.
func TestWorkflowGateBiasOrderingParity(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	be := backends.NewTest()
	prog, ok := cognition.RecognizeShape("design a service", nil)
	if !ok {
		t.Fatal("RecognizeShape found no shape")
	}
	wf := FromProgram(prog, cat, be, nil, "design a service")

	// Before Recognize, the cached flag is false ⇒ GateBias is {} even though phase[0] has a bias.
	if got := wf.GateBias(); len(got) != 0 {
		t.Errorf("GateBias before Recognize = %v, want {} (recognized flag still false)", got)
	}
	// Recognize a bespoke workflow ⇒ true; then GateBias yields phase[0]'s bias.
	if !wf.Recognize(nil) {
		t.Fatal("a bespoke workflow must Recognize on any context")
	}
	if got := wf.GateBias(); !biasEqual(got, map[string]float64{"decompose": 0.3}) {
		t.Errorf("GateBias after Recognize = %v, want {decompose:0.3}", got)
	}
	// GateBias returns a COPY — mutating it must not corrupt the phase's bias.
	got := wf.GateBias()
	got["decompose"] = 99
	if again := wf.GateBias(); again["decompose"] != 0.3 {
		t.Errorf("GateBias must return a defensive copy; phase bias corrupted to %v", again["decompose"])
	}
}

// biasEqual compares two bias maps exactly (key set + float values).
func biasEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || bv != av {
			return false
		}
	}
	return true
}
