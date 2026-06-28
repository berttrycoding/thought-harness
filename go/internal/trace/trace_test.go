package trace

import (
	"bytes"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// stringerSource is a stand-in for a ported enum that implements String() (e.g. types.Source).
type stringerSource struct{ name string }

func (s stringerSource) String() string { return s.name }

func TestJsonlSinkGoldenKeyOrder(t *testing.T) {
	var buf bytes.Buffer
	s := NewJsonlSinkTo(&buf)
	s.On(events.Event{
		Tick:    7,
		Kind:    "seam.filter",
		Layer:   "seam",
		Summary: "admit candidate",
		Data:    events.D{"confidence": 0.9, "verdict": "ADMIT"},
	})
	line := strings.TrimRight(buf.String(), "\n")

	// Key order must be exactly tick, kind, layer, summary, data (Python insertion order).
	wantPrefix := `{"tick":7,"kind":"seam.filter","layer":"seam","summary":"admit candidate","data":`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("golden key order wrong:\n got: %s\nwant prefix: %s", line, wantPrefix)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("each record must end in a newline")
	}
}

func TestJsonlSinkNoRoundingInSink(t *testing.T) {
	// The sink must NOT round — it writes whatever the component put on the wire.
	var buf bytes.Buffer
	s := NewJsonlSinkTo(&buf)
	s.On(events.Event{Tick: 1, Kind: "value.update", Layer: "value", Summary: "v",
		Data: events.D{"v": 0.123456789}})
	if !strings.Contains(buf.String(), "0.123456789") {
		t.Fatalf("sink rounded a value it must not round: %s", buf.String())
	}
}

func TestJsonlSinkResidualStringer(t *testing.T) {
	// A residual non-primitive (a Stringer enum) is pinned via goStr to its String().
	var buf bytes.Buffer
	s := NewJsonlSinkTo(&buf)
	s.On(events.Event{Tick: 1, Kind: "conscious.append", Layer: "conscious", Summary: "x",
		Data: events.D{"source": stringerSource{name: "INJECTED"}}})
	if !strings.Contains(buf.String(), `"source":"INJECTED"`) {
		t.Fatalf("residual Stringer not stringified to its name: %s", buf.String())
	}
}

func TestJsonlSinkNilData(t *testing.T) {
	var buf bytes.Buffer
	s := NewJsonlSinkTo(&buf)
	s.On(events.Event{Tick: 0, Kind: "tick", Layer: "tick", Summary: "", Data: nil})
	if !strings.Contains(buf.String(), `"data":null`) {
		t.Fatalf("nil data should marshal as null: %s", buf.String())
	}
}

func TestConsoleTracerFormat(t *testing.T) {
	tr := NewConsoleTracer(&bytes.Buffer{}, false, boolPtr(false), nil)
	line, ok := tr.Format(events.Event{Tick: 12, Kind: "critic.decision", Layer: "critic",
		Summary: "THINK"})
	if !ok {
		t.Fatal("event unexpectedly filtered")
	}
	// Python: f"[{tick:>4}] {kind:<20} {summary}"
	want := "[  12] critic.decision      THINK"
	if line != want {
		t.Fatalf("console line mismatch:\n got %q\nwant %q", line, want)
	}
}

func TestConsoleTracerQuietFilter(t *testing.T) {
	tr := NewConsoleTracer(&bytes.Buffer{}, true, boolPtr(false), nil)
	if _, ok := tr.Format(events.Event{Kind: events.SubFire, Layer: "subconscious"}); ok {
		t.Fatal("subconscious.fire is not in the quiet set; should be filtered")
	}
	if _, ok := tr.Format(events.Event{Kind: events.Decision, Layer: "critic"}); !ok {
		t.Fatal("critic.decision is in the quiet set; should pass")
	}
}

func TestConsoleTracerLayerFilter(t *testing.T) {
	tr := NewConsoleTracer(&bytes.Buffer{}, false, boolPtr(false), []string{"llm", "seam"})
	if _, ok := tr.Format(events.Event{Kind: events.Decision, Layer: "critic"}); ok {
		t.Fatal("critic not in layer set; should be filtered")
	}
	if _, ok := tr.Format(events.Event{Kind: events.Filter, Layer: "seam"}); !ok {
		t.Fatal("seam is in layer set; should pass")
	}
}

func TestConsoleTracerColorWrap(t *testing.T) {
	tr := NewConsoleTracer(&bytes.Buffer{}, false, boolPtr(true), nil)
	line, _ := tr.Format(events.Event{Tick: 1, Kind: "seam.filter", Layer: "seam", Summary: "x"})
	if !strings.HasPrefix(line, "\033[38;5;213m") || !strings.HasSuffix(line, reset) {
		t.Fatalf("seam layer should be wrapped in its ANSI colour: %q", line)
	}
}

func boolPtr(b bool) *bool { return &b }
