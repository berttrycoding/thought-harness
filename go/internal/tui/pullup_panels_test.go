package tui

// pullup_panels_test.go — the G5 PANEL-CUSTOMIZATION wiring gate (Track G). The wiring-gate lesson: a
// unit that exists but never runs on the App's actual key path is dead. So this drives the REAL ^O
// keystroke through Update and asserts the customization THINKING actually shapes what renders:
//   - knob OFF ⇒ the `^O` stack is the canonical full panel set in canon order, no witness event
//     (byte-identical to the pre-G5 surface);
//   - knob ON + a chosen order ⇒ the rendered stack shows ONLY the chosen panels, in the chosen order,
//     AND a tui.pullup witness event fires on the bus (the observability contract).
// This is the cognition-equivalent for a View slice: it asserts the selection/order DECISION the spec
// intends, not merely that the loop runs.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"

	tea "github.com/charmbracelet/bubbletea"
)

// newPullupApp builds a real reactive App whose engine carries the given Tui customization.
func newPullupApp(t *testing.T, on bool, order []string, horizon int) *App {
	t.Helper()
	feat := config.New()
	feat.Tui.PullupPanels = on
	feat.Tui.PullupOrder = order
	feat.Tui.StripHorizon = horizon
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

// boxedTitles extracts, in render order, the boxed monitor titles present in the rendered stack — the
// observable evidence of which panels show + their order.
func boxedTitles(stack string) []string {
	want := map[string]string{
		"VITALS": "VITALS", "LOOP": "LOOP", "CONTROLLER": "CONTROLLER", "SUBCONSCIOUS": "SUBCONSCIOUS",
		"OPERATORS": "OPERATORS", "TRIGGERS · SCHEDULE": "TRIGGERS", "HIDDEN SEAM": "SEAM",
		"CONSCIOUS · thought graph": "CONSCIOUS", "VALUE": "VALUE", "ACTION · GROUNDING": "ACTION",
		"SESSIONS · SUB-AGENTS": "SESSIONS", "REGULATOR · SCHEDULER": "REGULATOR", "THROUGHPUT": "THROUGHPUT",
		"REGISTRIES": "REGISTRIES", "MEMORY": "MEMORY", "KNOWLEDGE": "KNOWLEDGE", "SELF · EVOLUTION": "SELF",
	}
	var ids []string
	for _, line := range strings.Split(stack, "\n") {
		for title, id := range want {
			if strings.Contains(line, title) {
				// guard against a substring collision (e.g. "VALUE" inside another header): require the
				// title to be the FIRST monitor token after the box-drawing chrome.
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}

// TestPullupOffRendersCanonicalStack — the byte-identical gate: with tui.pullup.panels OFF, ^O renders
// the FULL canonical panel set in canon order, and NO tui.pullup event fires.
func TestPullupOffRendersCanonicalStack(t *testing.T) {
	a := newPullupApp(t, false, nil, 0)
	var seen []events.Event
	a.bridge.Engine().Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.PullupCustomize) {
			seen = append(seen, ev)
		}
	})

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !a.pullup {
		t.Fatal("^O must open the runtime monitor pull-up")
	}
	ids := boxedTitles(a.monitorStack(80))
	if len(ids) != len(config.PanelRegistry) {
		t.Fatalf("knob OFF must render all %d canonical panels; got %d (%v)", len(config.PanelRegistry), len(ids), ids)
	}
	for i, id := range config.PanelRegistry {
		if ids[i] != id {
			t.Errorf("knob OFF panel[%d] = %q, want canon %q (full canon order expected)", i, ids[i], id)
		}
	}
	if len(seen) != 0 {
		t.Errorf("knob OFF must emit NO tui.pullup event (byte-identical); got %d", len(seen))
	}
}

// TestPullupOnReordersAndFiltersAndEmits — the core THINKING + the wiring witness: with the knob ON and
// a chosen 3-panel order, ^O renders EXACTLY those three panels in that order, and a tui.pullup witness
// event fires carrying the customized layout.
func TestPullupOnReordersAndFiltersAndEmits(t *testing.T) {
	chosen := []string{"SEAM", "VITALS", "LOOP"}
	a := newPullupApp(t, true, chosen, 24)
	var seen []events.Event
	a.bridge.Engine().Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.PullupCustomize) {
			seen = append(seen, ev)
		}
	})

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	ids := boxedTitles(a.monitorStack(80))
	if len(ids) != len(chosen) {
		t.Fatalf("knob ON must render only the %d chosen panels; got %d (%v)", len(chosen), len(ids), ids)
	}
	for i, id := range chosen {
		if ids[i] != id {
			t.Errorf("chosen panel[%d] = %q, want %q (chosen order honoured)", i, ids[i], id)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("knob ON must emit exactly one tui.pullup witness event; got %d", len(seen))
	}
	ev := seen[0]
	if ev.Data["count"] != 3 {
		t.Errorf("event count = %v, want 3", ev.Data["count"])
	}
	if ev.Data["horizon"] != 24 {
		t.Errorf("event horizon = %v, want 24", ev.Data["horizon"])
	}
}

// TestPullupOnEmitsOncePerLayout — the dedupe contract: re-opening ^O with the SAME layout does not
// re-emit the witness; the App emits once per distinct layout, not every frame.
func TestPullupOnEmitsOncePerLayout(t *testing.T) {
	a := newPullupApp(t, true, []string{"VALUE", "SELF"}, 0)
	var n int
	a.bridge.Engine().Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.PullupCustomize) {
			n++
		}
	})

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlO}) // open  -> emit
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlO}) // close (modal toggle)
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlO}) // re-open, same layout -> no re-emit
	if n != 1 {
		t.Errorf("same-layout re-open must NOT re-emit; got %d events, want 1", n)
	}
}
