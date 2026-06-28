package cognition

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// In awake mode most ticks take a non-dispatch branch, so the last scan event lags the snapshot tick.
// The specialist panel must LABEL that staleness, not render the old scan as if it were this tick (E2).
func TestSubconsciousLabelsStaleScan(t *testing.T) {
	scan := []map[string]any{{"domain": "arith", "relevance": 0.0, "effective": 0.0, "fired": false}}
	ev := events.Event{Tick: 3, Kind: events.SubQuiet, Data: map[string]any{"theta": 0.5, "scan": toAnySlice(scan)}}
	vm := ViewModel{Width: 72, Snap: SnapshotData{Tick: 17}, Events: []events.Event{ev}} // 14 ticks newer

	if age, ok := scanStaleTicks(vm); !ok || age != 14 {
		t.Fatalf("scanStaleTicks = (%d,%v), want (14,true)", age, ok)
	}
	body := renderSubconscious(vm).Body
	if !strings.Contains(body, "idle") {
		t.Fatalf("overview did not label the stale scan:\n%s", body)
	}
	// a same-tick scan is NOT labelled stale.
	vm.Snap.Tick = 3
	if _, ok := scanStaleTicks(vm); ok {
		t.Fatal("a same-tick scan must not be flagged stale")
	}
	if strings.Contains(renderSubconscious(vm).Body, "idle ") {
		t.Fatal("same-tick scan should not show an idle note")
	}
}

func toAnySlice(ms []map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}
