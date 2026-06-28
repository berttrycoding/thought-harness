// Package trace is the headless observability layer: it subscribes to the event bus and
// renders the stream. Two sinks live here, and they are the ONLY I/O-permitted core package
// (every other engine/core package is headless-pure — emit, never print).
//
//   - ConsoleTracer — a colour-coded, layer-namespaced one-line-per-event stream (ANSI
//     strings; isatty autodetect). Stdlib-only, so `thought run` works without the TUI extras.
//     This is the textual analogue of what the TUI panels render.
//   - JsonlSink     — THE GOLDEN WRITER. A verbatim json.Marshal of {tick,kind,layer,summary,
//     data} in that key order, one record per line. This file is the cross-language
//     conformance contract (the seam the whole Python->Go migration pivots on): the Go stream
//     is golden-tested against the Python JSONL.
//
// Subscribers are plain func(events.Event); both sinks satisfy that shape via their On method.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// --- ANSI palette (mirrors Python trace._LAYER_COLOR / _RESET) ---------------------------

const reset = "\033[0m"

// layerColor maps an Event.Layer to its ANSI 256-colour SGR prefix. Ported verbatim from
// the Python trace module so the console stream is visually identical.
var layerColor = map[string]string{
	"subconscious": "\033[38;5;208m", // orange — hidden engine
	"seam":         "\033[38;5;213m", // magenta — the hidden seam
	"conscious":    "\033[38;5;81m",  // cyan — the thought stream
	"action":       "\033[38;5;46m",  // green — reality / watched seam
	"critic":       "\033[38;5;220m", // yellow — the Critic
	"value":        "\033[38;5;141m", // purple — value signal
	"regulator":    "\033[38;5;203m", // red — durability control
	"lifecycle":    "\033[38;5;245m", // grey — state machine
	"convert":      "\033[38;5;48m",  // teal — learning
	"llm":          "\033[38;5;154m", // lime — model calls
	"arousal":      "\033[38;5;245m",
	"port":         "\033[38;5;111m", // blue — inbound
	"tick":         "\033[38;5;240m",
}

// quiet is the curated "quiet" set: the spine of the loop without the per-candidate chatter.
// Ported verbatim from the Python trace._QUIET set.
var quiet = map[string]struct{}{
	events.Tick:        {},
	events.SubDispatch: {},
	events.SubQuiet:    {},
	events.Inject:      {},
	events.Generate:    {},
	events.Append:      {},
	events.MCP:         {},
	events.Intention:   {},
	events.Observation: {},
	events.Decision:    {},
	events.Value:       {},
	events.Regulator:   {},
	events.Convert:     {},
	events.Lifecycle:   {},
	events.Arousal:     {},
	events.Port:        {},
	events.LLM:         {},
	events.LLMFallback: {},
}

// --- ConsoleTracer -----------------------------------------------------------------------

// ConsoleTracer renders each event as one colour-coded line. It is a thin subscriber: the
// engine never prints, it emits; ConsoleTracer is just a Subscribe callback that formats and
// writes (it is the textual analogue of the TUI panels).
//
// Zero value is NOT ready — construct via NewConsoleTracer (so colour autodetect runs against
// the real stream). The Format method RETURNS the rendered line (no I/O), so callers that own
// their own output (tests, the TUI) can render without ConsoleTracer touching a stream; On
// writes to the configured stream.
type ConsoleTracer struct {
	Quiet  bool                // only emit events in the quiet spine set
	Color  bool                // wrap lines in ANSI colour for the layer
	Layers map[string]struct{} // if non-nil, only emit events whose layer is in this set
	out    io.Writer
}

// NewConsoleTracer constructs a tracer. color is tri-state to mirror Python's `color: bool |
// None` — pass a nil *bool to autodetect from the stream (isatty), or a concrete value to
// force colour on/off. layers nil means "all layers". out nil defaults to os.Stdout.
func NewConsoleTracer(out io.Writer, quietMode bool, color *bool, layers []string) *ConsoleTracer {
	if out == nil {
		out = os.Stdout
	}
	useColor := false
	if color == nil {
		useColor = isTTY(out)
	} else {
		useColor = *color
	}
	var layerSet map[string]struct{}
	if layers != nil {
		layerSet = make(map[string]struct{}, len(layers))
		for _, l := range layers {
			layerSet[l] = struct{}{}
		}
	}
	return &ConsoleTracer{Quiet: quietMode, Color: useColor, Layers: layerSet, out: out}
}

// keep returns whether this event passes the quiet/layer filters (the Python __call__ guards).
func (t *ConsoleTracer) keep(ev events.Event) bool {
	if t.Quiet {
		if _, ok := quiet[ev.Kind]; !ok {
			return false
		}
	}
	if t.Layers != nil {
		if _, ok := t.Layers[ev.Layer]; !ok {
			return false
		}
	}
	return true
}

// Format renders one event into its console line and RETURNS it (no I/O). Returns ("", false)
// when the event is filtered out. This is the I/O-free core that On wraps — callers that own
// their own output (the TUI, tests) render via Format and write themselves.
//
// Line shape matches Python exactly: "[%4d] %-20s %s" over (tick, kind, summary), optionally
// wrapped in the layer colour.
func (t *ConsoleTracer) Format(ev events.Event) (string, bool) {
	if !t.keep(ev) {
		return "", false
	}
	line := fmt.Sprintf("[%4d] %-20s %s", ev.Tick, ev.Kind, ev.Summary)
	if t.Color {
		c := layerColor[ev.Layer] // "" for an unknown layer (Python dict.get default "")
		line = c + line + reset
	}
	return line, true
}

// On is the bus subscriber: format the event and, if kept, write the line to the stream.
// Matches the Python ConsoleTracer.__call__ (print(line, file=self.stream)).
func (t *ConsoleTracer) On(ev events.Event) {
	if line, ok := t.Format(ev); ok {
		fmt.Fprintln(t.out, line)
	}
}

// isTTY reports whether w is a terminal (the Python stream.isatty() autodetect). Only the
// concrete *os.File case can be a real terminal; anything else (a buffer, a pipe) is not.
func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		info, err := f.Stat()
		if err != nil {
			return false
		}
		return info.Mode()&os.ModeCharDevice != 0
	}
	return false
}

// --- JsonlSink — THE GOLDEN WRITER -------------------------------------------------------

// JsonlSink persists every event as one JSON line for post-hoc per-subsystem debugging. It
// carries the FULL data (incl. LLM prompts/raw responses), unlike the truncated console
// summary.
//
// This is the GOLDEN WRITER: the wire contract is a VERBATIM marshal of
// {tick, kind, layer, summary, data} in exactly that key order (matching Python's insertion
// order), one record per line, flushed after each write.
//
// Two sub-contracts, honoured here exactly:
//   - NO ROUNDING IN THE SINK. Python's JsonlSink does a plain json.dumps with no rounding;
//     the ~23 round(x,3) calls live inside the components right before emit(...). This sink
//     writes whatever the components put on the wire, untouched.
//   - default=str is an ENUMERATED contract, not blanket replication. Components emit
//     already-stringified primitives (an enum is passed as its .name string, never the enum
//     object), so the data map is JSON-native. For any RESIDUAL non-primitive value, goStr
//     pins the Go output to Python str() so json.Marshal never silently differs on a wire value.
type JsonlSink struct {
	Path string
	f    *os.File
	w    io.Writer
}

// jsonlRecord is the wire record. Field order (tick, kind, layer, summary, data) reproduces
// Python's dict insertion order, so encoding/json (struct fields in declaration order)
// emits keys in the golden order.
type jsonlRecord struct {
	Tick    int            `json:"tick"`
	Kind    string         `json:"kind"`
	Layer   string         `json:"layer"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

// NewJsonlSink opens (truncating) the file at path and returns a sink that writes one JSON
// record per event. The caller is responsible for Close.
func NewJsonlSink(path string) (*JsonlSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &JsonlSink{Path: path, f: f, w: f}, nil
}

// NewJsonlSinkTo builds a sink that writes to an arbitrary io.Writer (no file, no Close
// obligation on a real fd) — handy for the golden test harness, which captures into a buffer.
func NewJsonlSinkTo(w io.Writer) *JsonlSink {
	return &JsonlSink{w: w}
}

// On is the bus subscriber: marshal the event into a JSON line and write it. It flushes the
// underlying file after each write (matching Python's self._f.flush()), so a crashed run still
// leaves a complete prefix of the stream on disk.
func (s *JsonlSink) On(ev events.Event) {
	rec := jsonlRecord{
		Tick:    ev.Tick,
		Kind:    ev.Kind,
		Layer:   ev.Layer,
		Summary: ev.Summary,
		Data:    normalizeData(ev.Data),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		// A marshal failure means a value slipped through that goStr did not normalise.
		// Mirror Python's robustness (default=str never raises): fall back to a goStr pass
		// over the whole record so a single bad value can never drop the line.
		b = marshalFallback(rec)
	}
	// json.Marshal never emits a trailing newline; add one (Python: write(... + "\n")).
	_, _ = s.w.Write(b)
	_, _ = io.WriteString(s.w, "\n")
	if s.f != nil {
		_ = s.f.Sync()
	}
}

// Close closes the underlying file if this sink owns one. Safe to call on a writer-only sink
// (no-op). Errors are swallowed, matching Python's best-effort close.
func (s *JsonlSink) Close() error {
	if s.f != nil {
		err := s.f.Close()
		s.f = nil
		return err
	}
	return nil
}

// normalizeData replaces any RESIDUAL non-JSON-native value in the data map with goStr(v),
// reproducing Python's default=str. Native values (string/bool/nil/the numeric kinds/nested
// maps+slices of natives) pass through untouched so json.Marshal handles them as-is. This is
// the enumerated-contract half: components already emit stringified primitives; this catches
// only the residue.
func normalizeData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = normalizeValue(v)
	}
	return out
}

// normalizeValue passes JSON-native values through and stringifies the residue via goStr.
// Containers (maps, slices) are walked so a non-native value nested inside is still caught
// (Python's json.dumps recurses, applying default=str only at the non-serialisable leaf).
func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		json.Number:
		return x
	case map[string]any:
		return normalizeData(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeValue(e)
		}
		return out
	default:
		// Any TYPED slice ([]events.D, []string, []int, []float64, …) is a JSON array, not a
		// scalar — Python emits these as arrays. The non-interface-element typed slices don't
		// match the []any case above, so walk them reflectively into a normalised []any so the
		// JSON encoder structures them (e.g. scan -> array of {domain,…} objects, NOT a Go
		// map-string). Each element recurses, so a nested non-native leaf is still caught.
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice {
			out := make([]any, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = normalizeValue(rv.Index(i).Interface())
			}
			return out
		}
		// A residual non-primitive (an enum value that wasn't pre-stringified, a custom
		// struct, a Go set-as-map of an unusual key type, etc.). Pin it to Python str().
		return goStr(v)
	}
}

// goStr reproduces Python's str() for the residual non-primitive values that can reach the
// wire under default=str. The enumerated contract means most values are pre-stringified at
// the emit site; goStr is the safety net for the rest, keeping the Go output byte-identical
// to what Python's str() would produce.
//
//   - fmt.Stringer -> its String() (the Go analogue of a __str__; this is how enum ports that
//     implement String() surface as their .name, e.g. Source -> "INJECTED").
//   - error        -> its Error() (Python str(exc)).
//   - everything else -> fmt's default formatting (%v), the closest general str() analogue.
func goStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None" // Python str(None)
	case bool:
		if x {
			return "True" // Python str(True)
		}
		return "False"
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// marshalFallback is the last-resort encoder: a goStr pass over every data value, then marshal.
// It only runs when the primary json.Marshal failed, so a single unmarshalable value can never
// drop a whole golden line (mirrors Python default=str's "never raises" guarantee).
func marshalFallback(rec jsonlRecord) []byte {
	safe := make(map[string]any, len(rec.Data))
	for k, v := range rec.Data {
		safe[k] = goStr(v)
	}
	rec.Data = safe
	// Marshal the keys deterministically (json.Marshal already sorts map keys); if even this
	// fails, emit a minimal record so the line count stays aligned with the event count.
	if b, err := json.Marshal(rec); err == nil {
		return b
	}
	// Build a minimal valid record by hand (kind+tick are always JSON-safe primitives).
	keys := make([]string, 0, len(safe))
	for k := range safe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(`{"tick":`)
	sb.WriteString(strconv.Itoa(rec.Tick))
	sb.WriteString(`,"kind":`)
	kb, _ := json.Marshal(rec.Kind)
	sb.Write(kb)
	sb.WriteString(`,"layer":`)
	lb, _ := json.Marshal(rec.Layer)
	sb.Write(lb)
	sb.WriteString(`,"summary":`)
	sm, _ := json.Marshal(rec.Summary)
	sb.Write(sm)
	sb.WriteString(`,"data":{}}`)
	return []byte(sb.String())
}
