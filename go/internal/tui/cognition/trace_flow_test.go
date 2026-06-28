package cognition

// trace_flow_test.go — the §G6 TRACE/FLOW swimlane PROPERTY tests (the round-trip the TRACE tab must
// read back, not merely that it renders). The fixture is a recorded reactive episode that runs the
// seed->thought->seam->subconscious->action ROUND-TRIP with the two phase-misalignment signals the view
// exists to surface: a LATE seam.inject (one that arrives AFTER the trip reached ACTION — the silent
// injection re-opening a passed node) and a conscious RETRACEMENT (a reenter that reopens a folded
// line). The tests assert the loader places each event in the right lane×tick, the desync markers fire
// on exactly those two events, and the phase readout computes the trip length / retracement count /
// land->deliver lag correctly off the fixture. Pure: deterministic event fixtures, no engine/model/clock.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// tripSession is the recorded round-trip the §G6 swimlane reconstructs: a user prompt enters at PORT
// (t0), a specialist fires in SUBCONSCIOUS (t1), the hidden seam filters+injects a re-voiced thought
// into CONSCIOUS (t2), the line ACTs and reality comes back (t4), the engine DELIVERs (t6); THEN a LATE
// inject lands at t7 (after the trip already reached ACTION — the desync) and a conscious reenter
// reopens a folded line at t8 (a retracement). One clean trip + the two phase-misalignment signals.
func tripSession() []events.Event {
	return []events.Event{
		ev(0, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe to ship?"}),
		ev(1, events.SubFire, map[string]any{"domain": "verify"}),                          // SUBCONSCIOUS
		ev(1, events.Generate, map[string]any{"text": "open the line on the refactor"}),    // CONSCIOUS
		ev(2, events.Filter, map[string]any{"verdict": "ADMIT"}),                           // SEAM
		ev(2, events.Inject, map[string]any{"text": "the suite is the ground truth"}),      // SEAM inject (in time)
		ev(4, events.Act, map[string]any{"tool": "run"}),                                   // ACTION out
		ev(5, events.Ground, map[string]any{"verdict": "grounded", "claim": "12/12 pass"}), // ACTION in
		ev(6, events.Respond, map[string]any{"text": "yes — the suite passes"}),            // ACTION deliver (DELIVER)
		// the two phase-misalignment signals AFTER the trip reached ACTION:
		ev(7, events.Inject, map[string]any{"text": "late afterthought"}), // LATE inject (desync)
		ev(8, events.MCP, map[string]any{"op": "reenter", "branch": 2}),   // conscious retracement (desync)
	}
}

// TestTraceFlowLanePlacement asserts the loader projects each event onto the right SWIMLANE lane at its
// tick (the diagonal read the view depends on): the user prompt on PORT@t0, the seam filter+inject on
// SEAM@t2, the specialist on SUBCONSCIOUS@t1, the ACT/GROUND/RESPOND on ACTION, the generate+reenter on
// CONSCIOUS. A control organ (regulator/value) carries NO lane and is dropped (the swimlane is the
// five-layer trip, not the whole bus).
func TestTraceFlowLanePlacement(t *testing.T) {
	var rec AnalysisRecord
	rec.fillTrace(tripSession())

	if len(rec.TraceEvents) == 0 {
		t.Fatal("fillTrace placed no events on the swimlane")
	}

	// the expected (kind -> lane) placement for each laned event.
	want := map[string]string{
		"port":               "PORT",
		"subconscious.fire":  "SUBCONSCIOUS",
		"conscious.generate": "CONSCIOUS",
		"seam.filter":        "SEAM",
		"seam.inject":        "SEAM",
		"action.act":         "ACTION",
		"grounding.ground":   "ACTION",
		"action.respond":     "ACTION",
		"conscious.mcp":      "CONSCIOUS",
	}
	for _, te := range rec.TraceEvents {
		w, ok := want[te.Kind]
		if !ok {
			t.Errorf("unexpected laned event kind %q (an off-lane organ leaked onto the swimlane)", te.Kind)
			continue
		}
		if te.Lane != w {
			t.Errorf("kind %q placed on lane %q, want %q", te.Kind, te.Lane, w)
		}
	}

	// the PORT arrival is the trip origin at t0.
	port := findTrace(rec.TraceEvents, "port", 0)
	if port == nil {
		t.Fatal("the user prompt was not placed on PORT@t0 (the trip never opens)")
	}

	// a control organ must NOT be on a lane — verify a regulator event is dropped.
	withReg := append(tripSession(), ev(3, events.Regulator, map[string]any{"n": 0.2, "theta": 0.5}))
	var rec2 AnalysisRecord
	rec2.fillTrace(withReg)
	for _, te := range rec2.TraceEvents {
		if te.Kind == "regulator.update" {
			t.Error("a regulator.update (control organ) leaked onto the swimlane — only the five-layer trip belongs")
		}
	}
}

// TestTraceFlowDesyncMarkers asserts the two phase-misalignment signals fire on EXACTLY the right
// events: the LATE seam.inject (t7, after the trip reached ACTION) is flagged Desync, the IN-TIME inject
// (t2, before any ACT) is NOT, and the conscious reenter (t8) is flagged Desync (a retracement).
func TestTraceFlowDesyncMarkers(t *testing.T) {
	var rec AnalysisRecord
	rec.fillTrace(tripSession())

	inEarly := findTrace(rec.TraceEvents, "seam.inject", 2)
	inLate := findTrace(rec.TraceEvents, "seam.inject", 7)
	reenter := findTrace(rec.TraceEvents, "conscious.mcp", 8)

	if inEarly == nil || inLate == nil || reenter == nil {
		t.Fatal("the fixture inject/reenter events were not all placed on the swimlane")
	}
	if inEarly.Desync {
		t.Error("the IN-TIME inject@t2 (before any ACT) was wrongly flagged as a late-injection desync")
	}
	if !inLate.Desync {
		t.Error("the LATE inject@t7 (after the trip reached ACTION) was NOT flagged as a desync — the re-opening-a-passed-node signal is missing")
	}
	if !reenter.Desync {
		t.Error("the conscious reenter@t8 was NOT flagged as a retracement desync")
	}
}

// TestTraceFlowPhaseReadout asserts the phase/frequency math BuildTraceReport computes off the fixture:
// the trip length is PORT@t0 -> DELIVER@t6 = 6 ticks; one conscious retracement (the reenter@t8); one
// late inject (t7); and the land->deliver lag is the LAST useful injection that landed at/before the
// DELIVER (the in-time inject@t2) -> DELIVER@t6 = 4 ticks. This is the operating-frequency number.
func TestTraceFlowPhaseReadout(t *testing.T) {
	var rec AnalysisRecord
	rec.fillTrace(tripSession())
	rec.Theta = []float64{0.5, 0.5, 0.6, 0.6, 0.6, 0.6, 0.7, 0.7, 0.7}

	rep := BuildTraceReport(rec, 6)

	if rep.TripStartTick != 0 {
		t.Errorf("trip start = t%d, want t0 (the PORT arrival)", rep.TripStartTick)
	}
	if rep.DeliverTick != 6 {
		t.Errorf("deliver tick = t%d, want t6 (the action.respond)", rep.DeliverTick)
	}
	if rep.TripTicks != 6 {
		t.Errorf("trip length = %d ticks, want 6 (PORT@t0 -> DELIVER@t6)", rep.TripTicks)
	}
	if rep.Retracements != 1 {
		t.Errorf("retracements = %d, want 1 (the conscious reenter@t8)", rep.Retracements)
	}
	if rep.LateInjects != 1 {
		t.Errorf("late injects = %d, want 1 (the inject@t7 after ACTION)", rep.LateInjects)
	}
	if !rep.HasInject {
		t.Error("HasInject = false, want true (the fixture injected twice)")
	}
	// the last useful injection AT OR BEFORE the deliver is the in-time inject@t2; the late inject@t7 is
	// AFTER the deliver so it is excluded — lag = 6 - 2 = 4.
	if rep.LandToDeliver != 4 {
		t.Errorf("land->deliver lag = %d ticks, want 4 (last in-time inject@t2 -> DELIVER@t6)", rep.LandToDeliver)
	}
	if rep.Theta != 0.7 {
		t.Errorf("theta@cursor6 = %.2f, want 0.70", rep.Theta)
	}
}

// TestTraceFlowRenderGatedAndPlaces asserts the END-TO-END render contract: with the tui.trace_flow flag
// OFF the TRACE tab keeps the "panel pending" placeholder (byte-identical, no swimlane); with it ON the
// swimlane renders, names all five lanes, and surfaces the phase readout (trip length + the desync lag).
func TestTraceFlowRenderGatedAndPlaces(t *testing.T) {
	rec := AnalysisRecord{Ticks: 9, Theta: []float64{0.5, 0.5, 0.6, 0.6, 0.6, 0.6, 0.7, 0.7, 0.7}}
	rec.fillTrace(tripSession())

	traceTab := -1
	for i, name := range analysisTabs {
		if name == "TRACE" {
			traceTab = i
		}
	}
	if traceTab < 0 {
		t.Fatal("no TRACE tab in the analysis tab strip — the G6 tab is not registered")
	}

	off := RenderAnalysisTab(rec, rec, 6, false, traceTab, 100, false, false, false)
	if !strings.Contains(off, "panel pending") {
		t.Error("traceFlow=OFF did not keep the 'panel pending' placeholder — the flag-off surface is not byte-identical")
	}
	if strings.Contains(off, "phase / freq") {
		t.Error("traceFlow=OFF still rendered the swimlane phase readout — the gate leaked")
	}

	on := RenderAnalysisTab(rec, rec, 6, false, traceTab, 100, false, false, true)
	if strings.Contains(on, "panel pending") {
		t.Error("traceFlow=ON still showed the 'panel pending' placeholder — the swimlane did not render")
	}
	for _, lane := range []string{"port", "conscious", "seam", "subconscious", "action"} {
		if !strings.Contains(on, lane) {
			t.Errorf("traceFlow=ON swimlane is missing the %q lane row", lane)
		}
	}
	// the phase readout surfaces the trip length + the land->deliver lag (the resonance numbers).
	if !strings.Contains(on, "phase / freq") || !strings.Contains(on, "trip length") || !strings.Contains(on, "land-lag") {
		t.Error("traceFlow=ON did not render the §G6 phase/freq readout (trip length + land->deliver lag)")
	}
}

// findTrace returns the first TraceEvent matching (kind, tick), or nil. A test helper.
func findTrace(evs []TraceEvent, kind string, tick int) *TraceEvent {
	for i := range evs {
		if evs[i].Kind == kind && evs[i].Tick == tick {
			return &evs[i]
		}
	}
	return nil
}
