package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// noEmit is the Go form of the Python test's `_NOEMIT = lambda *a, **k: None` — a sink that
// discards every event (the controller spine is asserted on its RETURN, not its trace).
func noEmit(kind, summary string, data map[string]any) events.Event { return events.Event{} }

// capture is an Emit that records every (kind, data) into a slice, so a test can assert the
// critic.decision / critic.exhaustion events that DecideNext fires (the PORT-PLAN §3.3 gate
// requires the identical Decision AND the identical decision/exhaustion events). The returned
// Event is zero-valued (the engine sets Tick/Layer in the real bus; the controller never reads it).
type capturedEvent struct {
	kind string
	data map[string]any
}

func newCapture() (events.Emit, *[]capturedEvent) {
	var log []capturedEvent
	emit := func(kind, summary string, data map[string]any) events.Event {
		log = append(log, capturedEvent{kind: kind, data: data})
		return events.Event{}
	}
	return emit, &log
}

// lastEvent returns the last captured event of the given kind, and ok=false if none was emitted.
func lastEvent(log *[]capturedEvent, kind string) (capturedEvent, bool) {
	for i := len(*log) - 1; i >= 0; i-- {
		if (*log)[i].kind == kind {
			return (*log)[i], true
		}
	}
	return capturedEvent{}, false
}

// appendT is a tiny helper mirroring the Python test's `g.append(Thought(-1, text, src, conf))`.
func appendT(g *graph.ThoughtGraph, text string, src types.Source, conf float64) {
	g.Append(&types.Thought{ID: -1, Text: text, Source: src, Confidence: conf}, 0)
}

// dec is DefaultDecideOptions with the two positional Python kwargs (conflict, acted_branch) set
// — the (conflict=…, acted_branch=…) the test passes; the rest stay at their Python defaults.
func dec(conflict, acted bool) DecideOptions {
	o := DefaultDecideOptions()
	o.Conflict = conflict
	o.ActedBranch = acted
	return o
}

// TestControllerDecisionSpine ports test_cognition.py::test_controller_decision_spine — the
// decision spine (§5.3 / §9.3): each exit fires under its precondition. This is the Tier-3 gate
// (PORT-PLAN §3.3) on the Controller.
func TestControllerDecisionSpine(t *testing.T) {
	ctrl := NewController(noEmit, nil, "control", nil)

	// goal satisfied -> STOP
	g := graph.New("what's 7x8?")
	appendT(g, "7 × 8 = 56", types.INJECTED, 0.85)
	if !ctrl.GoalSatisfied(g) {
		t.Fatal("expected goal satisfied for a confident INJECTED answer")
	}
	if d := ctrl.DecideNext(g, dec(false, false)); d != types.STOP {
		t.Fatalf("goal satisfied: want STOP, got %v", d)
	}

	// conflicting injections -> BRANCH
	g2 := graph.New("is it safe?")
	appendT(g2, "weighing it", types.GENERATED, 0.5)
	if d := ctrl.DecideNext(g2, dec(true, false)); d != types.BRANCH {
		t.Fatalf("conflict: want BRANCH, got %v", d)
	}

	// branch exhausted, loop exhausted (no viable sibling), not acted -> ACT (open to reality)
	g3 := graph.New("long division")
	for i := 0; i < 5; i++ {
		appendT(g3, "grinding step "+itoa(i), types.GENERATED, 0.3)
	}
	if !(ctrl.BranchExhausted(g3) && ctrl.LoopExhausted(g3)) {
		t.Fatal("g3 should be branch- and loop-exhausted")
	}
	if d := ctrl.DecideNext(g3, dec(false, false)); d != types.ACT {
		t.Fatalf("loop exhausted: want ACT, got %v", d)
	}

	// branch exhausted but a high-value sibling exists -> BACKTRACK (internal exit before acting)
	g4 := graph.New("compare designs")
	for i := 0; i < 5; i++ {
		appendT(g4, "stuck on A "+itoa(i), types.GENERATED, 0.3)
	}
	parent := g4.ActiveBranch
	sib := g4.NewBranch(&parent, nil)
	g4.Branches[sib].Value = 0.8
	g4.Branches[sib].Status = types.STASHED
	if d := ctrl.DecideNext(g4, dec(false, false)); d != types.BACKTRACK {
		t.Fatalf("viable sibling: want BACKTRACK, got %v", d)
	}
}

// TestControllerEmitsDecisionAndExhaustionEvents is the second half of the Tier-3 gate (PORT-PLAN
// §3.3): DecideNext must not only return the identical Decision, it must emit the identical
// critic.decision + critic.exhaustion events with the byte-identical data keys/values Python emits.
// (The Python cognition test asserts the RETURN; this asserts the trace the engine/TUI subscribe to —
// the observability contract. Structural assertion on the data payload, cross-checked against the
// Python controller's emit sites in controller.py.)
func TestControllerEmitsDecisionAndExhaustionEvents(t *testing.T) {
	emit, log := newCapture()
	ctrl := NewController(emit, nil, "control", nil)

	// loop-exhausted ACT case (the richest payload: branch_exhausted=loop_exhausted=true).
	g := graph.New("long division")
	for i := 0; i < 5; i++ {
		appendT(g, "grinding step "+itoa(i), types.GENERATED, 0.3)
	}
	d := ctrl.DecideNext(g, dec(false, false))
	if d != types.ACT {
		t.Fatalf("loop exhausted: want ACT, got %v", d)
	}

	// critic.decision: data{decision, reason, stop_kind}. decision name matches the return; stop_kind
	// is nil for a non-STOP move (Python `stop_kind.name if stop_kind else None`).
	de, ok := lastEvent(log, events.Decision)
	if !ok {
		t.Fatal("no critic.decision event emitted")
	}
	if de.data["decision"] != "ACT" {
		t.Fatalf("decision event: data.decision = %v, want ACT", de.data["decision"])
	}
	if _, has := de.data["reason"]; !has {
		t.Fatal("decision event: missing data.reason key")
	}
	if de.data["stop_kind"] != nil {
		t.Fatalf("decision event: data.stop_kind = %v, want nil for a non-STOP move", de.data["stop_kind"])
	}

	// critic.exhaustion: data{branch_exhausted, loop_exhausted, flagged, needs_ground_truth}.
	ee, ok := lastEvent(log, events.Exhaustion)
	if !ok {
		t.Fatal("no critic.exhaustion event emitted")
	}
	for k, want := range map[string]bool{
		"branch_exhausted":   true,
		"loop_exhausted":     true,
		"flagged":            false,
		"needs_ground_truth": false,
	} {
		got, has := ee.data[k]
		if !has {
			t.Fatalf("exhaustion event: missing data.%s key", k)
		}
		if got != want {
			t.Fatalf("exhaustion event: data.%s = %v, want %v", k, got, want)
		}
	}

	// A goal-satisfied STOP carries a non-nil stop_kind = "GOAL_MET" (the other arm of the payload).
	emit2, log2 := newCapture()
	ctrl2 := NewController(emit2, nil, "control", nil)
	g2 := graph.New("what's 7x8?")
	appendT(g2, "7 × 8 = 56", types.INJECTED, 0.85)
	if d := ctrl2.DecideNext(g2, dec(false, false)); d != types.STOP {
		t.Fatalf("goal satisfied: want STOP, got %v", d)
	}
	de2, ok := lastEvent(log2, events.Decision)
	if !ok {
		t.Fatal("no critic.decision event on STOP")
	}
	if de2.data["decision"] != "STOP" {
		t.Fatalf("STOP decision event: data.decision = %v, want STOP", de2.data["decision"])
	}
	if de2.data["stop_kind"] != "GOAL_MET" {
		t.Fatalf("STOP decision event: data.stop_kind = %v, want GOAL_MET", de2.data["stop_kind"])
	}
}

// stopBackend always says STOP — the failure mode the structural guard must protect against. It
// embeds *TestBackend so the 8 core Backend methods are inherited; only Decide is added, so
// it satisfies both Backend and backends.Decider. Mirrors the Python test's `_StopBackend`.
type stopBackend struct{ *backends.TestBackend }

func (stopBackend) Decide(goal string, ctx []types.Thought, options []string) (choice, why string) {
	return "STOP", ""
}

// TestHybridProtectsStructuralDecisions ports
// test_cognition.py::test_hybrid_protects_structural_decisions — the hybrid Controller never lets
// a model override a STRUCTURAL move (a conflict MUST fork, even though the model says STOP), and
// does not even escalate a structural decision.
func TestHybridProtectsStructuralDecisions(t *testing.T) {
	be := stopBackend{backends.NewTest()}
	ctrl := NewController(noEmit, nil, "hybrid", be)

	g := graph.New("is it safe?")
	appendT(g, "weighing it", types.GENERATED, 0.5)
	d := ctrl.DecideNext(g, dec(true, false))
	if d != types.BRANCH {
		t.Fatalf("hybrid let the model override the structural BRANCH-on-conflict: got %v", d)
	}
	if ctrl.Escalations != 0 {
		t.Fatalf("hybrid should not escalate a structural decision at all: escalations=%d", ctrl.Escalations)
	}
}

// declineBackend satisfies backends.Decider but always DECLINES (choice "") — the model-unavailable
// / off-list case the Pattern-C floor must survive. It embeds *TestBackend for the core methods.
type declineBackend struct{ *backends.TestBackend }

func (declineBackend) Decide(goal string, ctx []types.Thought, options []string) (choice, why string) {
	return "", "" // decline -> llmDecide returns ok=false -> the floor stands (Rule 4)
}

// TestControllerDeclinedEscalationFloorStands is the Pattern-C Rule-4 gate for the Controller (the
// analogue of the Filter test): a high-ambiguity NON-structural decision escalates, the model
// declines, and the deterministic floor must STAND and emit escalation.floor_stands (never silent).
func TestControllerDeclinedEscalationFloorStands(t *testing.T) {
	emit, log := newCapture()
	be := declineBackend{backends.NewTest()}
	ctrl := NewController(emit, nil, "hybrid", be)

	// A borderline THINK/STOP situation: a single GENERATED thought whose confidence (0.65) sits
	// inside the 0.12 window around DoneConfidence (0.7) — |0.65-0.7|/0.12 ≈ 0.42 → ambiguity ≈ 0.58
	// (>= 0.5) — so the "is the goal met?" judgment is genuinely uncalculable and doEscalate fires.
	// The decision is THINK (non-structural; a GENERATED thought below the done threshold does not
	// satisfy the goal), so the structural guard does not apply — the model is consulted, declines,
	// and the floor must stand.
	g := graph.New("estimate the answer")
	appendT(g, "leaning toward an estimate, not certain yet", types.GENERATED, 0.65)
	d := ctrl.DecideNext(g, dec(false, false))

	// the model declined, so the deterministic floor decision stands unchanged.
	if ctrl.Escalations != 0 {
		t.Fatalf("a declined escalation must not count as an escalation; escalations=%d", ctrl.Escalations)
	}
	ev, ok := lastEvent(log, events.EscalationFloorStands)
	if !ok {
		t.Fatalf("a declined escalation on an eligible decision must emit escalation.floor_stands (got decision %v)", d)
	}
	if ev.data["site"] != "critic.decide" {
		t.Errorf("floor_stands site=%v want critic.decide", ev.data["site"])
	}
	if ev.data["reason"] != "model-declined" {
		t.Errorf("floor_stands reason=%v want model-declined", ev.data["reason"])
	}
	if ev.data["model_consulted"] != true {
		t.Errorf("floor_stands model_consulted=%v want true", ev.data["model_consulted"])
	}
}

// TestControllerDefaultModeNoFloorStands pins that the default (control/heuristic) mode never emits
// escalation.floor_stands — nothing is escalation-eligible, so the goldens stay byte-identical.
func TestControllerDefaultModeNoFloorStands(t *testing.T) {
	emit, log := newCapture()
	ctrl := NewController(emit, nil, "control", nil)

	g := graph.New("is it safe?")
	appendT(g, "weighing it", types.GENERATED, 0.5)
	ctrl.DecideNext(g, dec(true, false)) // a conflict -> structural BRANCH

	if _, ok := lastEvent(log, events.EscalationFloorStands); ok {
		t.Fatalf("the default control mode must never emit escalation.floor_stands")
	}
}

// viableSiblingGraph builds the spine's "branch exhausted + a high-value stashed sibling exists"
// fixture (mirrors g4 of TestControllerDecisionSpine): the active line is exhausted and a STASHED
// sibling clears the pursuit threshold, so the default decision is BACKTRACK.
func viableSiblingGraph() *graph.ThoughtGraph {
	g := graph.New("compare designs")
	for i := 0; i < 5; i++ {
		appendT(g, "stuck on A "+itoa(i), types.GENERATED, 0.3)
	}
	parent := g.ActiveBranch
	sib := g.NewBranch(&parent, nil)
	g.Branches[sib].Value = 0.8
	g.Branches[sib].Status = types.STASHED
	return g
}

// TestRetraceOffForbidsBacktrack pins the retrace-off ablation (conscious.allow_backtrack OFF,
// measuring-stick-spec §5.8): on the SAME viable-sibling fixture that yields BACKTRACK by default,
// disabling the gate forbids the retrace move so the ladder falls through to the external exit (ACT),
// degrading the graph to a single line. It also asserts the bypass is observable (config.skip) and
// that the default (gate enabled / unset) still yields BACKTRACK — so the toggle is a true no-op ON.
func TestRetraceOffForbidsBacktrack(t *testing.T) {
	// default (no gate installed): BACKTRACK, byte-identical to the spine.
	base := NewController(noEmit, nil, "control", nil)
	if d := base.DecideNext(viableSiblingGraph(), dec(false, false)); d != types.BACKTRACK {
		t.Fatalf("gate unset: want BACKTRACK (default), got %v", d)
	}

	// gate enabled (AllowBacktrack=true): still BACKTRACK.
	cfgOn := config.AllOn()
	onCtrl := NewController(noEmit, nil, "control", nil)
	onCtrl.SetBacktrackGate(config.NewGate("conscious.allow_backtrack",
		func() bool { return cfgOn.Conscious.AllowBacktrack }, noEmit))
	if d := onCtrl.DecideNext(viableSiblingGraph(), dec(false, false)); d != types.BACKTRACK {
		t.Fatalf("gate ON: want BACKTRACK, got %v", d)
	}

	// gate disabled (AllowBacktrack=false): the retrace move is forbidden -> ACT (single-line graph).
	cfgOff := config.AllOn()
	cfgOff.Conscious.AllowBacktrack = false
	emit, log := newCapture()
	offCtrl := NewController(noEmit, nil, "control", nil)
	offCtrl.SetBacktrackGate(config.NewGate("conscious.allow_backtrack",
		func() bool { return cfgOff.Conscious.AllowBacktrack }, emit))
	if d := offCtrl.DecideNext(viableSiblingGraph(), dec(false, false)); d == types.BACKTRACK {
		t.Fatal("gate OFF: BACKTRACK must be forbidden (retrace-off ablation)")
	}
	if _, ok := lastEvent(log, events.ConfigSkip); !ok {
		t.Fatal("gate OFF must emit config.skip (the bypass must be observable, never silent)")
	}

	// live re-enable through the shared pointer: BACKTRACK is available again with no rebuild.
	cfgOff.Conscious.AllowBacktrack = true
	if d := offCtrl.DecideNext(viableSiblingGraph(), dec(false, false)); d != types.BACKTRACK {
		t.Fatalf("live re-enable: want BACKTRACK again, got %v", d)
	}
}

// itoa is a tiny int->string for the test's step labels (avoids importing strconv into the test
// just for the fixture text).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
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

// A2 (mandate 2026-06-12): DELIVER is a first-class executive decision — closing a line the user
// is waiting on means SPEAKING, and the spine itself decides it. STOP remains the silent close
// (no unresolved user input on the active line). Delivery resolving the line (the high-water
// mark) flips the same situation back to STOP — speech is never decided twice for one turn.
func TestSpineDeliversBeforeStoppingOnUserLine(t *testing.T) {
	c := NewController(noEmit, nil, "control", nil)

	// a satisfied line the user asked for: USER_INPUT root + a confident injected answer.
	g := graph.New("what is 12 * 7?")
	g.StampGoalSource(types.USER_INPUT)
	appendT(g, "the product works out to 84", types.INJECTED, 0.93)
	appendT(g, "confirmed: 12 * 7 = 84", types.INJECTED, 0.95)

	d := c.DecideNext(g, dec(false, false))
	if d != types.DELIVER {
		t.Fatalf("satisfied line with a waiting user: want DELIVER, got %v (%s)", d, c.LastMeta.Reason)
	}
	if c.LastMeta.StopKind == nil || *c.LastMeta.StopKind != "GOAL_MET" {
		t.Fatalf("DELIVER must carry the close kind in meta, got %v", c.LastMeta.StopKind)
	}

	// the answer was delivered: the SAME situation is now a silent close.
	g.MarkDelivered()
	d = c.DecideNext(g, dec(false, false))
	if d != types.STOP {
		t.Fatalf("satisfied line, user already answered: want STOP, got %v (%s)", d, c.LastMeta.Reason)
	}

	// a satisfied line nobody asked for (the mind's own) closes silently too.
	w := graph.New("(awake — no task; mind is wandering)")
	appendT(w, "an interesting structure emerges here", types.INJECTED, 0.93)
	appendT(w, "concluded: the structure holds", types.INJECTED, 0.95)
	if d := c.DecideNext(w, dec(false, false)); d != types.STOP {
		t.Fatalf("satisfied endogenous line: want STOP, got %v (%s)", d, c.LastMeta.Reason)
	}
}
