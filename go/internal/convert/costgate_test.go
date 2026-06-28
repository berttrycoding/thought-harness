package convert

// costgate_test.go proves the COGNITION of the W5 cost-aware trace->skill mint gate (gate registry
// growth on the COST/efficiency ruler, at the RUNTIME mint). It asserts the THINKING the spec intends,
// not the plumbing: the harness only AUTOMATES a recurring program shape worth automating — one whose
// re-synthesis has demonstrably cost real decode. A cheap recurring shape is DECLINED (held) even though
// it recurs >= MintAfter and converged on value; an expensive recurring shape is ADMITTED and minted; the
// admit/hold cost evidence is surfaced on the bus; and with the gate OFF (the default) the decision is
// byte-identical to the count×value heuristic (no cost consultation, no event). This is the W5 efficiency
// discrimination — "is this worth a skill?" decided on cost, not just frequency.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// mintEligible recurs a SAME-shape program MintAfter times for goal and observes a valuable episode, so
// the candidate clears the existing count×value gate — the ONLY remaining question is the cost gate.
func mintEligible(c *Convertibility, goal string, prog fakeProg) {
	for i := 0; i < c.MintAfter(); i++ {
		c.NoteProgram(goal, prog)
	}
	c.Observe(buildEpisode(goal, 3, 0.8)) // value 0.8 >= MintValue, generated 3 >= MintAfter
}

// TestCostGateDeclinesCheapRecurringShape is the load-bearing cognition: a shape that recurs and is
// valuable but was CHEAP to re-synthesise (below the cost floor) is NOT promoted to a skill when the cost
// gate is on — automating it would mint filler that saves nothing. The gate discriminates on COST, and it
// says so on the bus.
func TestCostGateDeclinesCheapRecurringShape(t *testing.T) {
	rec := &recEmit{}
	fm := &fakeMinter{}
	c := New(&fakeReg{}, rec.emit, nil, fm)
	c.EnableCostGate(true, 300) // floor 300 completion-tok

	goal := "tidy the small config file here"
	mintEligible(c, goal, fakeProg{shape: "seq(generate)"})
	// CHEAP: only 120 completion-tok was ever spent re-synthesising this shape — below the 300 floor.
	c.NoteSynthesisCost(goal, 120)

	c.Consolidate()

	if len(fm.minted) != 0 {
		t.Fatalf("a cheap-to-resynthesise shape must NOT be auto-minted under the cost gate; minted=%v", fm.minted)
	}
	if len(c.MintedSkill) != 0 {
		t.Fatalf("a held mint must not be recorded in MintedSkill; got %v", c.MintedSkill)
	}
	holds := rec.withKind(events.CostGate)
	if len(holds) == 0 {
		t.Fatal("the cost gate must SURFACE its hold decision (convert.cost_gate), not silently skip")
	}
	if k, _ := holds[0].Data["kind"].(string); k != "hold" {
		t.Fatalf("expected a HOLD cost-gate event for a sub-floor shape; got kind=%q", k)
	}
	if cost, _ := holds[0].Data["cost"].(int); cost != 120 {
		t.Fatalf("the hold event must carry the accumulated re-synthesis cost (120); got %v", holds[0].Data["cost"])
	}
}

// TestCostGateAdmitsExpensiveRecurringShape is the other half: a shape whose re-synthesis cost CLEARED the
// floor IS worth automating — the gate admits it, the skill mints, and the admit evidence is on the bus.
func TestCostGateAdmitsExpensiveRecurringShape(t *testing.T) {
	rec := &recEmit{}
	fm := &fakeMinter{}
	c := New(&fakeReg{}, rec.emit, nil, fm)
	c.EnableCostGate(true, 300)

	goal := "derive the recursive partition bound"
	mintEligible(c, goal, fakeProg{shape: "seq(decompose,generate,validate)"})
	// EXPENSIVE: re-synthesising this shape repeatedly cost 1500 completion-tok — well above the floor.
	c.NoteSynthesisCost(goal, 1500)

	c.Consolidate()

	if len(fm.minted) == 0 {
		t.Fatal("an expensive recurring shape that cleared the cost floor MUST be promoted to a skill")
	}
	admits := rec.withKind(events.CostGate)
	if len(admits) == 0 {
		t.Fatal("the cost gate must surface its admit decision (convert.cost_gate)")
	}
	if k, _ := admits[0].Data["kind"].(string); k != "admit" {
		t.Fatalf("expected an ADMIT cost-gate event for an above-floor shape; got kind=%q", k)
	}
}

// TestCostGateDiscriminatesWithinOneConsolidate is the core THINKING in one pass: presented two equally
// recurrent, equally valuable shapes that differ ONLY in re-synthesis cost, the gate mints the EXPENSIVE
// one and declines the CHEAP one. Frequency alone would mint both; the cost ruler is what tells them apart.
func TestCostGateDiscriminatesWithinOneConsolidate(t *testing.T) {
	rec := &recEmit{}
	fm := &fakeMinter{}
	c := New(&fakeReg{}, rec.emit, nil, fm)
	c.EnableCostGate(true, 300)

	cheapGoal := "rename one trivial helper symbol"
	mintEligible(c, cheapGoal, fakeProg{shape: "seq(generate)"})
	c.NoteSynthesisCost(cheapGoal, 90) // below floor

	costlyGoal := "plan the multistage migration strategy"
	mintEligible(c, costlyGoal, fakeProg{shape: "par(decompose,generate),validate"})
	c.NoteSynthesisCost(costlyGoal, 2200) // above floor

	c.Consolidate()

	if len(fm.minted) != 1 {
		t.Fatalf("exactly the EXPENSIVE shape should mint; minted=%v", fm.minted)
	}
	// The minted skill name is derived from the COSTLY goal's key ("plan multistage migration"), never the
	// cheap one's ("rename trivial helper") — proving the gate kept the right one.
	if !strings.HasPrefix(fm.minted[0], "learned-plan") {
		t.Fatalf("the minted skill must be the COSTLY shape's (learned-plan...); got %q", fm.minted[0])
	}
	// Both decisions must be on the bus: one admit, one hold.
	var admit, hold int
	for _, e := range rec.withKind(events.CostGate) {
		switch e.Data["kind"] {
		case "admit":
			admit++
		case "hold":
			hold++
		}
	}
	if admit != 1 || hold != 1 {
		t.Fatalf("expected one admit + one hold cost-gate decision; got admit=%d hold=%d", admit, hold)
	}
}

// TestCostGateOffIsByteIdentical is the default-OFF contract: with the cost gate OFF (the default), a
// cheap recurring valuable shape mints EXACTLY as it does today (the count×value heuristic alone), and NO
// convert.cost_gate event is emitted. Cost is irrelevant to the decision until the gate is flipped on.
func TestCostGateOffIsByteIdentical(t *testing.T) {
	rec := &recEmit{}
	fm := &fakeMinter{}
	c := New(&fakeReg{}, rec.emit, nil, fm) // gate OFF (never EnableCostGate'd)

	goal := "tidy the small config file here"
	mintEligible(c, goal, fakeProg{shape: "seq(generate)"})
	c.NoteSynthesisCost(goal, 1) // a tiny cost: would be held if the gate were on, but it is off

	c.Consolidate()

	if len(fm.minted) == 0 {
		t.Fatal("with the cost gate OFF, the cheap shape must mint exactly as the count×value heuristic dictates")
	}
	if got := rec.withKind(events.CostGate); len(got) != 0 {
		t.Fatalf("the cost gate OFF must emit NO convert.cost_gate events (byte-identical); got %d", len(got))
	}
}

// TestNoteSynthesisCostDropsUnknownGoal: attributing cost for a goal with no tracked program run yet is a
// no-op (the run is the unit the cost attaches to). No panic, no phantom run.
func TestNoteSynthesisCostDropsUnknownGoal(t *testing.T) {
	c := New(&fakeReg{}, nil, nil, &fakeMinter{})
	c.NoteSynthesisCost("a goal that was never synthesised", 5000) // must not panic / create a run
	if len(c.ProgramRuns()) != 0 {
		t.Fatalf("NoteSynthesisCost for an unknown goal must not create a program run; got %d", len(c.ProgramRuns()))
	}
}
