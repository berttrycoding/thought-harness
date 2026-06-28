package cogngraph

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestReplayRecordedPythonStreamByteIdentical is the Tier-6 cogngraph gate (PORT-PLAN §2 row 6):
// "cogngraph reconstructs a BYTE-IDENTICAL node/edge model when fed a RECORDED Python event stream".
//
// The fixture testdata/py_s6.jsonl was captured from the Python reference:
//
//	cd thought_harness && python3 -m thought_harness scenario S6 --backend heuristic --log /tmp/py_s6.jsonl
//
// and testdata/py_s6_model.json is the canonical node/edge model the PYTHON CognitionGraph builds
// when that same JSONL is replayed back through it (Python json.loads + on_event). This test feeds
// the SAME recorded JSONL through the GO cogngraph and asserts the reconstructed model is identical
// node-for-node and edge-for-edge.
//
// The load-bearing subtlety: Python's json.loads keeps an integer literal (e.g. id=2) as an int, so
// the id-builders render "th:ep:1:2" (no ".0"). To be byte-identical, the Go JSONL loader must make
// the SAME int/float distinction (loadRecordedStream below decodes whole-valued JSON numbers as Go
// int, fractional ones as float64) — exactly what Python json.loads does. A naive json.Unmarshal
// into map[string]any would float64-ify every number and produce "th:ep:1:2.0", diverging from the
// Python reference. That is the whole point of the gate.
func TestReplayRecordedPythonStreamByteIdentical(t *testing.T) {
	stream := loadRecordedStream(t, filepath.Join("testdata", "py_s6.jsonl"))
	if len(stream) == 0 {
		t.Fatal("recorded stream is empty")
	}

	c := New()
	feed(c, stream)

	want := loadReferenceModel(t, filepath.Join("testdata", "py_s6_model.json"))

	// --- stats parity (totals + per-layer spread) -------------------------------------------
	got := c.Stats()
	if got.Nodes != want.Stats.Nodes {
		t.Errorf("node count = %d, want %d (Python)", got.Nodes, want.Stats.Nodes)
	}
	if got.Edges != want.Stats.Edges {
		t.Errorf("edge count = %d, want %d (Python)", got.Edges, want.Stats.Edges)
	}
	if got.Processes != want.Stats.Processes {
		t.Errorf("process count = %d, want %d (Python)", got.Processes, want.Stats.Processes)
	}
	for layer, n := range want.Stats.ByLayer {
		if got.ByLayer[layer] != n {
			t.Errorf("by_layer[%q] = %d, want %d (Python)", layer, got.ByLayer[layer], n)
		}
	}
	if len(got.ByLayer) != len(want.Stats.ByLayer) {
		t.Errorf("by_layer has %d layers, want %d (Python): got %v want %v",
			len(got.ByLayer), len(want.Stats.ByLayer), got.ByLayer, want.Stats.ByLayer)
	}

	// --- node-for-node parity (id/type/layer/label/process/tick), sorted by id -------------
	gotNodes := make([]refNode, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		gotNodes = append(gotNodes, refNode{
			ID: n.ID, Type: n.Type, Layer: n.Layer, Label: n.Label,
			Process: nullableProcess(n.Process), Tick: n.Tick,
		})
	}
	sort.Slice(gotNodes, func(i, j int) bool { return gotNodes[i].ID < gotNodes[j].ID })

	if len(gotNodes) != len(want.Nodes) {
		t.Fatalf("node set size %d != Python %d\n  go:     %s\n  python: %s",
			len(gotNodes), len(want.Nodes), nodeIDList(gotNodes), nodeIDList(want.Nodes))
	}
	for i := range want.Nodes {
		if !nodesEqual(gotNodes[i], want.Nodes[i]) {
			t.Errorf("node[%d] mismatch:\n  go:     %s\n  python: %s",
				i, fmtNode(gotNodes[i]), fmtNode(want.Nodes[i]))
		}
	}

	// --- edge-for-edge parity ([src,rel,dst]), sorted -------------------------------------
	gotEdges := make([][3]string, 0, len(c.Edges))
	for _, e := range c.Edges {
		gotEdges = append(gotEdges, [3]string{e.Src, e.Rel, e.Dst})
	}
	sort.Slice(gotEdges, func(i, j int) bool { return lessTriple(gotEdges[i], gotEdges[j]) })
	wantEdges := make([][3]string, len(want.Edges))
	copy(wantEdges, want.Edges)
	sort.Slice(wantEdges, func(i, j int) bool { return lessTriple(wantEdges[i], wantEdges[j]) })

	if len(gotEdges) != len(wantEdges) {
		t.Fatalf("edge set size %d != Python %d", len(gotEdges), len(wantEdges))
	}
	for i := range wantEdges {
		if gotEdges[i] != wantEdges[i] {
			t.Errorf("edge[%d] mismatch:\n  go:     %v\n  python: %v", i, gotEdges[i], wantEdges[i])
		}
	}
}

// --- recorded-stream loader (Python json.loads parity) ------------------------------------------

// refNode is the comparable projection of a node the Python reference model dumps. Process is a
// *string so the Python null (a node created before any episode) compares correctly.
type refNode struct {
	ID      string
	Type    string
	Layer   string
	Label   string
	Process *string
	Tick    int
}

type refStats struct {
	Nodes     int            `json:"nodes"`
	Edges     int            `json:"edges"`
	Processes int            `json:"processes"`
	ByLayer   map[string]int `json:"by_layer"`
}

type refModel struct {
	Nodes []refNode
	Edges [][3]string
	Stats refStats
}

// rawNode mirrors the JSON shape the Python dumper writes (process is a JSON string-or-null).
type rawNode struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Layer   string  `json:"layer"`
	Label   string  `json:"label"`
	Process *string `json:"process"`
	Tick    int     `json:"tick"`
}

func loadReferenceModel(t *testing.T, path string) refModel {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reference model %s: %v", path, err)
	}
	var raw struct {
		Nodes []rawNode  `json:"nodes"`
		Edges [][]string `json:"edges"`
		Stats refStats   `json:"stats"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("decode reference model: %v", err)
	}
	m := refModel{Stats: raw.Stats}
	for _, n := range raw.Nodes {
		m.Nodes = append(m.Nodes, refNode{
			ID: n.ID, Type: n.Type, Layer: n.Layer, Label: n.Label, Process: n.Process, Tick: n.Tick,
		})
	}
	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].ID < m.Nodes[j].ID })
	for _, e := range raw.Edges {
		if len(e) != 3 {
			t.Fatalf("reference edge is not a triple: %v", e)
		}
		m.Edges = append(m.Edges, [3]string{e[0], e[1], e[2]})
	}
	return m
}

// loadRecordedStream parses a recorded Python --log JSONL into Go events.Event values, decoding
// JSON numbers exactly as CPython's json.loads does: a whole-valued literal becomes a Go int, a
// fractional one a float64. This is what makes pyStr(d["id"]) render "2" (not "2.0"), so the Go
// reconstruction is byte-identical to the Python reference. (Go's plain json.Unmarshal would
// float64-ify every number — the divergence the gate is designed to catch.)
func loadRecordedStream(t *testing.T, path string) []events.Event {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded stream %s: %v", path, err)
	}
	var out []events.Event
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode jsonl line %q: %v", line, err)
		}
		ev := events.Event{
			Tick:    intField(rec["tick"]),
			Kind:    strField(rec["kind"]),
			Layer:   strField(rec["layer"]),
			Summary: strField(rec["summary"]),
			Data:    pyNumbers(rec["data"]).(map[string]any),
		}
		if ev.Data == nil {
			ev.Data = map[string]any{}
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan recorded stream: %v", err)
	}
	return out
}

// pyNumbers recursively rewrites every json.Number in a decoded payload to a Go int (whole value)
// or float64 (fractional), matching CPython json.loads. Strings, bools, nil pass through; nested
// maps/slices are walked. This is the single transform that aligns Go's numeric typing with
// Python's for the id-builders and labels.
func pyNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			if i, err := x.Int64(); err == nil {
				return int(i)
			}
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = pyNumbers(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = pyNumbers(e)
		}
		return out
	default:
		return v
	}
}

func intField(v any) int {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

func strField(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func nullableProcess(p string) *string {
	if p == "" {
		return nil
	}
	return &p
}

// nodesEqual compares two refNodes by VALUE, dereferencing Process (a *string so the Python null
// distinguishes "created before any episode" from process ""). Struct == would compare the
// pointers, not the strings.
func nodesEqual(a, b refNode) bool {
	if a.ID != b.ID || a.Type != b.Type || a.Layer != b.Layer || a.Label != b.Label || a.Tick != b.Tick {
		return false
	}
	switch {
	case a.Process == nil && b.Process == nil:
		return true
	case a.Process == nil || b.Process == nil:
		return false
	default:
		return *a.Process == *b.Process
	}
}

func fmtNode(n refNode) string {
	proc := "null"
	if n.Process != nil {
		proc = *n.Process
	}
	return "{id=" + n.ID + " type=" + n.Type + " layer=" + n.Layer +
		" process=" + proc + " label=" + n.Label + "}"
}

func lessTriple(a, b [3]string) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func nodeIDList(ns []refNode) string {
	ids := make([]string, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
	}
	return strings.Join(ids, ", ")
}
