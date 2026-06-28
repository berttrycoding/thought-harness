package convert

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fakeMinter is a SkillMinter test double recording the skill names convertibility promotes.
type fakeMinter struct{ minted []string }

func (f *fakeMinter) Mint(name string, triggers []string, body Program, description string) bool {
	f.minted = append(f.minted, name)
	return true
}

// fakeProg implements the narrow convert.Program port (just Shape()).
type fakeProg struct{ shape string }

func (p fakeProg) Shape() string { return p.shape }

func sp(s string) *string { return &s }

// TestGoalKey pins the per-goal key extraction: the first three alpha CONTENT words >2 chars, lower-
// cased, with common function words dropped (the stopword filter — so a stopword-led goal does not mint
// an over-firing trigger like "the", which substring-matches almost any goal).
func TestGoalKey(t *testing.T) {
	cases := map[string]string{
		"Design a small TODO service": "design small todo",
		// "why is the service crashing": "is" is 2 chars (dropped), and "why"/"the" are stopwords
		// (dropped by the over-fire filter) -> the contentful "service crashing" remains. (Pre-filter
		// this was "why the service" — a "the" trigger that over-fired on unrelated goals.)
		"why is the service crashing": "service crashing",
		"":                            "",
		"12 34 56":                    "12 34 56", // no alpha words -> first 24 chars fallback
	}
	for in, want := range cases {
		if got := goalKey(in); got != want {
			t.Errorf("goalKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestConsolidateMintsRecurringSkill: a program shape that recurs (>= MintAfter) for a goal whose
// pattern is valuable (>= MintValue) is promoted to a named library Skill (trace -> skill).
func TestConsolidateMintsRecurringSkill(t *testing.T) {
	reg := &fakeReg{}
	fm := &fakeMinter{}
	c := New(reg, nil, nil, fm)

	goal := "design a small todo service"
	prog := fakeProg{shape: "seq(decompose,generate,validate)"}
	for i := 0; i < c.MintAfter(); i++ { // recur the SAME shape MintAfter times
		c.NoteProgram(goal, prog)
	}
	c.Observe(buildEpisode(goal, 3, 0.8)) // value 0.8 >= MintValue, generated 3 >= MintAfter

	c.Consolidate()
	if len(fm.minted) == 0 {
		t.Fatal("a recurring, valuable program should be promoted to a Skill")
	}
	if len(c.MintedSkill) == 0 {
		t.Fatal("the minted skill must be recorded in MintedSkill")
	}
}

// TestConsolidateSkipsLowValueProgram: a recurring program that never converged on value is NOT minted
// (the §9.5 value gate — compile signal, not noise).
func TestConsolidateSkipsLowValueProgram(t *testing.T) {
	reg := &fakeReg{}
	fm := &fakeMinter{}
	c := New(reg, nil, nil, fm)

	goal := "doodle something pointless here"
	prog := fakeProg{shape: "seq(generate)"}
	for i := 0; i < c.MintAfter(); i++ {
		c.NoteProgram(goal, prog)
	}
	c.Observe(buildEpisode(goal, 3, 0.05)) // value 0.05 < MintValue -> below the gate

	c.Consolidate()
	if len(fm.minted) != 0 {
		t.Fatalf("a low-value program must not mint a skill; got %v", fm.minted)
	}
}

// TestConsolidateCompilesGatePrior: enough METACOG ops with a recurring gate-winning domain compile a
// standing gate prior (METACOG -> automatic control habit), bounded at 0.3.
func TestConsolidateCompilesGatePrior(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)

	g := graph.New("keep the deploy safe to ship")
	// MetacogAfter METACOG thoughts + an INJECTED thought whose domain wins the gate tally.
	for i := 0; i < c.MetacogAfter(); i++ {
		g.Append(&types.Thought{ID: -1, Text: "reflect on the plan", Source: types.METACOG}, 1)
	}
	g.Append(&types.Thought{ID: -1, Text: "this is risky", Source: types.INJECTED,
		RawReturn: &types.Candidate{Domain: sp("safety")}}, 1)
	g.Active().Value = 0.5
	g.Active().Epistemic = 0.5

	c.Observe(g)
	c.Consolidate()

	if c.GatePrior["safety"] <= 0 {
		t.Fatalf("a recurring gate-winning domain should compile a positive gate prior; GatePrior=%v", c.GatePrior)
	}
	if c.GatePrior["safety"] > 0.3 {
		t.Fatalf("the gate prior must be bounded at 0.3; got %v", c.GatePrior["safety"])
	}
}
