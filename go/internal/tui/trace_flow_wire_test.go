package tui

// trace_flow_wire_test.go — the G6 TRACE/FLOW swimlane WIRING gate (Track G). The wiring-gate lesson: a
// renderer that exists but never runs on the App's actual key path is dead. So this drives the REAL ^Y
// keystroke + Tab navigation through Update and asserts the swimlane THINKING actually shapes what
// renders + witnesses itself on the bus:
//   - knob OFF ⇒ the TRACE analysis tab keeps the "panel pending" placeholder, no tui.trace_view event
//     fires (byte-identical to the pre-G6 surface);
//   - knob ON ⇒ landing on the TRACE tab renders the five-lane swimlane (NOT the placeholder) AND a
//     tui.trace_view witness event fires carrying the round-trip phase numbers (the observability
//     contract that makes the unit visible + testable).
// This is the cognition-equivalent for a View slice: it asserts the swimlane DECISION the spec intends
// (the round-trip placed by lane×tick + the phase readout), not merely that the loop runs.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"

	tea "github.com/charmbracelet/bubbletea"
)

// newTraceApp builds a real reactive App whose engine carries the given tui.trace_flow setting, with a
// loaded analysis record that has a real round-trip (so the TRACE tab has something to place). The
// record is built through the REAL loader path (RecordFromFrozen -> fillTrace) off a recorded stream,
// so the wiring test exercises the actual data path, not a hand-set field.
func newTraceApp(t *testing.T, on bool) *App {
	t.Helper()
	feat := config.New()
	feat.Tui.TraceFlow = on
	feat.Validate()

	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.w, a.h = 120, 40
	a.recomputeLayout()
	return a
}

// tripRecord builds the loaded analysis record the wiring test scrubs — a recorded round-trip through
// the REAL loader path (RecordFromFrozen -> fillTrace): PORT(t0) -> SUBCONSCIOUS fire + CONSCIOUS
// generate -> SEAM filter+inject -> ACTION act + ground + DELIVER(t6), then a LATE inject(t7, desync)
// and a conscious reenter(t8, retracement).
func tripRecord() cognition.AnalysisRecord {
	evs := []events.Event{
		mkEv(0, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe?"}),
		mkEv(1, events.SubFire, map[string]any{"domain": "verify"}),
		mkEv(1, events.Generate, map[string]any{"text": "open the line"}),
		mkEv(2, events.Filter, map[string]any{"verdict": "ADMIT"}),
		mkEv(2, events.Inject, map[string]any{"text": "the suite is ground truth"}),
		mkEv(4, events.Act, map[string]any{"tool": "run"}),
		mkEv(5, events.Ground, map[string]any{"verdict": "grounded", "claim": "12/12"}),
		mkEv(6, events.Respond, map[string]any{"text": "yes"}),
		mkEv(7, events.Inject, map[string]any{"text": "late afterthought"}),
		mkEv(8, events.MCP, map[string]any{"op": "reenter", "branch": 2}),
	}
	rec := cognition.RecordFromFrozen(evs, nil)
	rec.Name = "wire-trip"
	return rec
}

// mkEv mirrors the cognition test helper (tick/kind/data -> Event) so this package can build a recorded
// stream without reaching into the cognition test fixtures.
func mkEv(tick int, kind string, data map[string]any) events.Event {
	layer := kind
	if i := strings.IndexByte(kind, '.'); i >= 0 {
		layer = kind[:i]
	}
	if data == nil {
		data = map[string]any{}
	}
	return events.Event{Tick: tick, Kind: kind, Layer: layer, Data: data}
}

// traceTabIndex resolves the TRACE tab's index in the analysis strip (so the test can Tab to it).
func traceTabIndex(t *testing.T) int {
	t.Helper()
	for i := 0; ; i++ {
		n := cognition.AnalysisTabName(i)
		if n == "" {
			break
		}
		if n == "TRACE" {
			return i
		}
	}
	t.Fatal("no TRACE tab in the analysis strip")
	return -1
}

// drives ^Y to open the analysis surface, injects the loaded round-trip record (the ^Y handler resets
// anRecA to the frozen/sample record, so the loaded record is set AFTER open — the picker/load path the
// G1 loader feeds), then Tabs to the given tab index along the REAL key path (KeyTab repeatedly), so
// the App's own Tab handler runs emitTraceView when it lands on TRACE.
func openAtTab(t *testing.T, a *App, idx int) {
	t.Helper()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if !a.anPreview {
		t.Fatal("^Y must open the analysis preview")
	}
	a.anRecA = tripRecord()
	a.anRecB = a.anRecA
	for a.anTab != idx {
		a.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
}

// TestTraceFlowOffKeepsPlaceholder — the byte-identical gate: with tui.trace_flow OFF, landing on the
// TRACE analysis tab keeps the "panel pending" placeholder and emits NO tui.trace_view event.
func TestTraceFlowOffKeepsPlaceholder(t *testing.T) {
	a := newTraceApp(t, false)
	var seen int
	a.bridge.Engine().Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.TraceView) {
			seen++
		}
	})

	idx := traceTabIndex(t)
	openAtTab(t, a, idx)

	body := cognition.RenderAnalysisTab(a.anRecA, a.anRecB, a.anCursor, a.anCompare, a.anTab, 100,
		a.registryHeatEnabled(), a.deepLedgersEnabled(), a.traceFlowEnabled())
	if !strings.Contains(body, "panel pending") {
		t.Error("knob OFF must keep the TRACE 'panel pending' placeholder (byte-identical)")
	}
	if strings.Contains(body, "phase / freq") {
		t.Error("knob OFF rendered the swimlane phase readout — the gate leaked")
	}
	if seen != 0 {
		t.Errorf("knob OFF must emit NO tui.trace_view event; got %d", seen)
	}
}

// TestTraceFlowOnRendersSwimlaneAndEmits — the core THINKING + the wiring witness: with the knob ON,
// landing on the TRACE tab renders the five-lane swimlane (NOT the placeholder) and a tui.trace_view
// witness event fires carrying the round-trip phase numbers.
func TestTraceFlowOnRendersSwimlaneAndEmits(t *testing.T) {
	a := newTraceApp(t, true)
	var seen []events.Event
	a.bridge.Engine().Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.TraceView) {
			seen = append(seen, ev)
		}
	})

	idx := traceTabIndex(t)
	openAtTab(t, a, idx)

	body := cognition.RenderAnalysisTab(a.anRecA, a.anRecB, a.anCursor, a.anCompare, a.anTab, 100,
		a.registryHeatEnabled(), a.deepLedgersEnabled(), a.traceFlowEnabled())
	if strings.Contains(body, "panel pending") {
		t.Fatal("knob ON still showed the 'panel pending' placeholder — the swimlane did not render")
	}
	for _, lane := range []string{"port", "conscious", "seam", "subconscious", "action"} {
		if !strings.Contains(body, lane) {
			t.Errorf("knob ON swimlane is missing the %q lane row", lane)
		}
	}
	if !strings.Contains(body, "phase / freq") {
		t.Error("knob ON did not render the phase/freq readout")
	}

	if len(seen) == 0 {
		t.Fatal("knob ON must emit a tui.trace_view witness event when the TRACE tab is opened")
	}
	ev := seen[len(seen)-1]
	// the round-trip is PORT@t0 -> DELIVER@t6 = 6 ticks; one conscious retracement (the reenter@t8).
	if ev.Data["trip_ticks"] != 6 {
		t.Errorf("tui.trace_view trip_ticks = %v, want 6", ev.Data["trip_ticks"])
	}
	if ev.Data["retracements"] != 1 {
		t.Errorf("tui.trace_view retracements = %v, want 1", ev.Data["retracements"])
	}
}
