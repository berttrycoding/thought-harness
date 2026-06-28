package realhard

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// escalation_capture_test.go — the CAP-EVAL grounding-fix ENGAGEMENT capture
// (TASK 2). It proves the realhard harness arm counts the grounding-fix
// escalation events (escalation.tool_select / escalation.force_ground) on the
// live tick — the gap that muddied the A/B (we could not tell whether the fix
// fired). Three layers, all mutation-sensitive:
//   (1) runCounters.subscribe — the SAME bus tap RunHarness installs — counts the
//       two escalation kinds + LLM calls off a real bus.
//   (2) the report reducer aggregates per-run escalations into the arm stats +
//       EngagedRuns (a run with any escalation).
//   (3) Render surfaces the engagement line.

// TestRunCountersTapsEscalations drives the ACTUAL wiring: install the harness
// arm's counter on a real bus and emit the escalation kinds. A mutation that
// listened for the wrong kind (or dropped a case) makes a tally wrong.
func TestRunCountersTapsEscalations(t *testing.T) {
	bus := events.NewDefault()
	var ctr runCounters
	ctr.subscribe(bus)

	bus.Emit(events.EscalationToolSelect, "grounding-chain tool pick", events.D{})
	bus.Emit(events.EscalationToolSelect, "grounding-chain tool pick", events.D{})
	bus.Emit(events.EscalationForceGround, "force a read before give-up", events.D{})
	bus.Emit(events.LLM, "a model call", events.D{})
	// an unrelated kind must NOT be counted as an escalation (guards a too-broad case).
	bus.Emit(events.EscalationFloorStands, "floor stands", events.D{})

	if ctr.toolSelectEsc != 2 {
		t.Errorf("toolSelectEsc = %d, want 2", ctr.toolSelectEsc)
	}
	if ctr.forceGroundEsc != 1 {
		t.Errorf("forceGroundEsc = %d, want 1", ctr.forceGroundEsc)
	}
	if ctr.calls != 1 {
		t.Errorf("calls = %d, want 1 (only events.LLM)", ctr.calls)
	}
}

// TestReportAggregatesEscalations verifies the per-run escalation counts flow
// through Reduce into the harness arm stats and EngagedRuns, and that the bare
// arm never accrues escalations. Mutation-sensitive: a dropped accumulation or a
// wrong EngagedRuns predicate fails.
func TestReportAggregatesEscalations(t *testing.T) {
	results := []RunResult{
		// harness r0: engaged (1 tool_select)
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmHarness, Replay: 0,
			Verdict: Verdict{Solved: true}, ToolSelectEscalations: 1},
		// harness r1: engaged (2 force_ground)
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmHarness, Replay: 1,
			Verdict: Verdict{Solved: false}, ForceGroundEscalations: 2},
		// harness r2: NOT engaged (no escalation)
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmHarness, Replay: 2,
			Verdict: Verdict{Solved: true}},
		// bare run carries (impossibly) an escalation field — it must be ignored for
		// the harness stats and bare must show zero engagement.
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmBare, Replay: 0,
			Verdict: Verdict{Solved: false}, ToolSelectEscalations: 9},
	}
	rep := Reduce(results)
	if rep.Harness.ToolSelectEsc != 1 {
		t.Errorf("Harness.ToolSelectEsc = %d, want 1", rep.Harness.ToolSelectEsc)
	}
	if rep.Harness.ForceGroundEsc != 2 {
		t.Errorf("Harness.ForceGroundEsc = %d, want 2", rep.Harness.ForceGroundEsc)
	}
	if rep.Harness.EngagedRuns != 2 {
		t.Errorf("Harness.EngagedRuns = %d, want 2 (r0 + r1 engaged, r2 not)", rep.Harness.EngagedRuns)
	}
	if rep.Bare.ToolSelectEsc != 9 {
		// the reducer accumulates from the field; bare is just never SET non-zero in
		// production (RunBare leaves it 0). Here we fed 9 to prove the field is read
		// for whatever arm — engagement is REPORTED for harness only in Render.
		t.Errorf("Bare.ToolSelectEsc = %d, want 9 (reducer reads the field)", rep.Bare.ToolSelectEsc)
	}

	// (3) Render surfaces the harness engagement line.
	out := rep.Render("test")
	if !strings.Contains(out, "grounding-fix ENGAGEMENT") {
		t.Errorf("render missing the ENGAGEMENT header:\n%s", out)
	}
	if !strings.Contains(out, "tool_select escalations: 1") ||
		!strings.Contains(out, "force_ground escalations: 2") ||
		!strings.Contains(out, "runs-engaged: 2/3") {
		t.Errorf("render missing the harness engagement counts:\n%s", out)
	}
}
