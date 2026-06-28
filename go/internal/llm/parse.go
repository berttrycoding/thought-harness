package llm

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// This file holds the defensive JSON salvage + the small text/ring helpers that mirror the
// Python module-level functions in llm.py. Every parse is wrapped so a malformed model reply
// degrades to the heuristic — these helpers NEVER panic; they return an error / false.

// stripThink removes inline <think>...</think> reasoning some models put in `content`. Mirrors
// Python _strip_think (re.DOTALL, then strip).
func stripThink(text string) string {
	return strings.TrimSpace(thinkRe.ReplaceAllString(text, ""))
}

// parsesAsObject reports whether text yields a JSON object — the structured-role validity check the
// truncated-INVALID retry uses (synthesize_program / form_intention). A response truncated mid-JSON
// fails this, so chat retries it with a grown budget before the caller falls back to the control floor.
func parsesAsObject(text string) bool {
	_, err := loadsObject(text)
	return err == nil
}

// loadsObject leniently slices the outermost {...} out of a model reply and json-decodes it.
// Mirrors Python _loads_object: text[index("{") : rindex("}")+1]. Returns an error (never panics)
// when there is no object or the slice doesn't parse.
func loadsObject(text string) (map[string]any, error) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < 0 || end < start {
		return nil, errString("no JSON object in reply")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text[start:end+1]), &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// loadsArray leniently slices the outermost [...] out of a model reply and json-decodes it.
// Mirrors Python _loads_array: text[index("[") : rindex("]")+1]. Returns an error on no array.
func loadsArray(text string) ([]any, error) {
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start < 0 || end < 0 || end < start {
		return nil, errString("no JSON array in reply")
	}
	var arr []any
	if err := json.Unmarshal([]byte(text[start:end+1]), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// extractContent pulls the answer from a chat message, handling reasoning models: prefer
// `content`; if empty and salvage is on, salvage the answer out of the reasoning trace (walking the
// DEFAULT multi-provider field list reasoning_content → reasoning → thinking); strip inline <think>
// blocks. Narrative roles disable salvage (a clean fallback beats voicing the model's meta-reasoning);
// JSON roles enable it (the JSON often sits inside the reasoning). The salvage is NOT the banned
// test-double fallback — it is the model's OWN output in the wrong field (Pattern B: an empty content
// with a full reasoning is not a gap, it is the answer misfiled). The configurable-field-list path
// lives on the backend (extractAnswer); this package-level form uses the defaults and is the salvage
// PARITY anchor (salvage_parity_test.go).
func extractContent(msg map[string]any, salvage bool) string {
	if content := stripThink(asString(msg["content"])); content != "" {
		return content
	}
	if !salvage {
		return ""
	}
	for _, field := range defaultReasoningFields {
		if trace := stripThink(asString(msg[field])); trace != "" {
			if ans := salvageAnswer(trace); ans != "" {
				return ans
			}
		}
	}
	return ""
}

// salvageAnswer mines the usable answer out of a reasoning trace (the model's own output, just
// misfiled in a reasoning_content/reasoning/thinking field — Pattern B). It is the ONE place the
// "extract the conclusion" heuristic lives, shared by the package-level extractContent and the
// backend's extractAnswer:
//
//   - JSON roles: if the trace contains a JSON object, return that object verbatim (loadsObject
//     already leniently slices the outermost {...}); the JSON callers (parseVerdict / loadsObject)
//     re-parse it. This is the common reasoning-model case: the model reasons in prose, then emits
//     the required JSON at the END of the trace.
//   - narrative roles: return the FINAL conclusion — the text after the last reasoning-step marker
//     (a "final answer:"-style lead-in, else the last non-empty paragraph). A reasoning trace ends
//     with its conclusion; the steps before it are scaffolding, not the answer.
//
// Returns "" only when the trace is empty after trimming (truly no answer to salvage).
func salvageAnswer(trace string) string {
	trace = strings.TrimSpace(trace)
	if trace == "" {
		return ""
	}
	// JSON roles: hand back the embedded object verbatim (the caller re-parses). loadsObject is
	// lenient (outermost {...}), so a trace that reasons in prose then closes with the JSON salvages.
	if start := strings.IndexByte(trace, '{'); start >= 0 {
		end := strings.LastIndexByte(trace, '}')
		if end > start {
			candidate := trace[start : end+1]
			var probe map[string]any
			if json.Unmarshal([]byte(candidate), &probe) == nil {
				return candidate
			}
		}
	}
	// Narrative roles: prefer the text after the LAST explicit final-answer lead-in.
	if i := lastFinalAnswerIdx(trace); i >= 0 {
		if tail := strings.TrimSpace(trace[i:]); tail != "" {
			return tail
		}
	}
	// Else the last non-empty paragraph (the conclusion a reasoning trace ends on).
	if para := lastParagraph(trace); para != "" {
		return para
	}
	return trace
}

// finalAnswerLeads are the case-insensitive lead-ins a reasoning model uses to mark its conclusion;
// the text AFTER the last one is the answer (the scaffolding before it is the reasoning).
var finalAnswerLeads = []string{
	"final answer:", "final answer is", "the answer is", "answer:",
	"conclusion:", "in conclusion", "so the answer", "therefore,", "thus,",
}

// lastFinalAnswerIdx returns the byte index in trace JUST AFTER the last final-answer lead-in (so
// the caller slices the conclusion), or -1 when none is present. Case-insensitive.
func lastFinalAnswerIdx(trace string) int {
	low := strings.ToLower(trace)
	best := -1
	for _, lead := range finalAnswerLeads {
		if i := strings.LastIndex(low, lead); i >= 0 && i+len(lead) > best {
			best = i + len(lead)
		}
	}
	return best
}

// lastParagraph returns the last non-empty paragraph (blank-line-delimited block, else the last
// non-empty line) of a reasoning trace — the conclusion a trace ends on.
func lastParagraph(trace string) string {
	blocks := strings.Split(trace, "\n\n")
	for i := len(blocks) - 1; i >= 0; i-- {
		if b := strings.TrimSpace(blocks[i]); b != "" {
			return b
		}
	}
	lines := strings.Split(trace, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// parseVerdict parses {"verdict","confidence","reason"} -> FilterVerdict. Returns ok=false on any
// shape error (caught by the caller, which falls back to the heuristic). Mirrors Python
// _parse_verdict — keeps the model's full reasoning (not clipped) + tags source="llm".
func parseVerdict(text string) (types.FilterVerdict, bool) {
	if text == "" {
		return types.FilterVerdict{}, false
	}
	obj, err := loadsObject(text)
	if err != nil {
		return types.FilterVerdict{}, false
	}
	name := strings.ToUpper(strings.TrimSpace(asString(obj["verdict"])))
	verdict, ok := types.ParseVerdict(name)
	if !ok {
		return types.FilterVerdict{}, false // Python Verdict[bad] raises KeyError -> except
	}
	conf := clamp01(asFloat(obj["confidence"], 0.5))
	reason := strings.TrimSpace(asString(obj["reason"]))
	return types.FilterVerdict{Verdict: verdict, Confidence: conf, Reason: reason, Source: "llm"}, true
}

// parseRank parses [{"score","why"}] -> (scores, reasons); tolerates a bare [float] array (the
// old shape). Returns (nil, nil) when nothing parses (caller falls back). Mirrors _parse_rank.
func parseRank(text string, n int) (scores []float64, reasons []string) {
	arr, err := loadsArray(text)
	if err == nil && len(arr) > 0 {
		if _, isObj := arr[0].(map[string]any); isObj {
			s := make([]float64, 0, len(arr))
			r := make([]string, 0, len(arr))
			okShape := true
			for _, e := range arr {
				o, isMap := e.(map[string]any)
				if !isMap {
					okShape = false
					break
				}
				s = append(s, clamp01(asFloat(o["score"], 0.5)))
				r = append(r, strings.TrimSpace(asString(o["why"])))
			}
			if okShape && len(s) == n {
				return s, r
			}
		}
	}
	// back-compat: a bare float array.
	floats := parseFloats(text, n)
	if floats == nil {
		return nil, nil
	}
	return floats, make([]string, n)
}

// parseFloats parses a bare [float,...] array, clamps each to [0,1], truncates to n, then pads
// with 0.5 (Python _parse_floats). Returns nil on no array.
func parseFloats(text string, n int) []float64 {
	if text == "" {
		return nil
	}
	arr, err := loadsArray(text)
	if err != nil {
		return nil
	}
	// Python does `[float(x) for x in arr]`: float() on a non-number (e.g. a dict from the
	// {"score","why"} shape) raises TypeError, caught -> the whole parse returns None. So a single
	// non-numeric element fails the bare-float parse (it is NOT a bare-float array).
	all := make([]float64, 0, len(arr))
	for _, x := range arr {
		fv, ok := toFloat(x)
		if !ok {
			return nil
		}
		all = append(all, clamp01(fv))
	}
	vals := all
	if len(vals) > n {
		vals = vals[:n] // Python [:n] AFTER the full list comp
	}
	for len(vals) < n {
		vals = append(vals, 0.5) // pad if the model returned too few
	}
	return vals
}

// toFloat coerces a decoded JSON value to float64 like Python float(x), returning ok=false when
// float() would raise (a dict/list/null/non-numeric string).
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		if fv, err := x.Float64(); err == nil {
			return fv, true
		}
	case string:
		if fv, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return fv, true
		}
	case bool: // Python float(True)==1.0 / float(False)==0.0
		if x {
			return 1.0, true
		}
		return 0.0, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// thought joining (mirrors llm.py _join)
// ---------------------------------------------------------------------------

// joinThoughts feeds the model REAL thoughts, not structural METACOG markers: drops METACOG, takes
// the last n, formats "- {text}" per line. "(none yet)" when empty. Mirrors Python _join.
func joinThoughts(thoughts []types.Thought, n int) string {
	real := make([]types.Thought, 0, len(thoughts))
	for _, t := range thoughts {
		if t.Source != types.METACOG {
			real = append(real, t)
		}
	}
	if len(real) > n {
		real = real[len(real)-n:]
	}
	if len(real) == 0 {
		return "(none yet)"
	}
	var sb strings.Builder
	for i, t := range real {
		if i > 0 {
			sb.WriteString(" \n")
		}
		sb.WriteString("- " + t.Text)
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// the per-call I/O ring (maxlen 256)
// ---------------------------------------------------------------------------

// ring is a fixed-capacity FIFO of callRecords (Python deque(maxlen=256)). Oldest is evicted when
// full. last() returns the most recent record (the doctor probe reads it). It is guarded by a mutex
// because under per-phase concurrency (THOUGHT_PARALLEL_PHASES) several reason-only sub-agents push to
// the SAME shared backend's Log ring concurrently; the lock makes push/Len/last race-free. The lock is
// held only for the in-memory slice write/read (microseconds), never across a model call.
type ring struct {
	mu   sync.Mutex
	buf  []callRecord
	cap  int
	next int
	full bool
}

func newRing(capacity int) *ring { return &ring{buf: make([]callRecord, capacity), cap: capacity} }

// push appends a record, evicting the oldest when full.
func (r *ring) push(rec callRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = rec
	r.next = (r.next + 1) % r.cap
	if r.next == 0 {
		r.full = true
	}
}

// lenLocked reports how many records are held; the caller must hold r.mu (so last() can reuse it
// without a re-entrant lock).
func (r *ring) lenLocked() int {
	if r.full {
		return r.cap
	}
	return r.next
}

// Len reports how many records are currently held.
func (r *ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lenLocked()
}

// last returns the most recently pushed record and ok=false when empty.
func (r *ring) last() (callRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lenLocked() == 0 {
		return callRecord{}, false
	}
	idx := (r.next - 1 + r.cap) % r.cap
	return r.buf[idx], true
}

// ---------------------------------------------------------------------------
// small typed helpers (replacing Python's str()/float()/dict.get coercions)
// ---------------------------------------------------------------------------

// asString coerces a decoded JSON value to a string the way Python str(obj.get(k,"")) does for the
// values these prompts produce: a JSON string stays as-is; anything else (missing, number, bool)
// stringifies. A missing key (nil) yields "" — matching Python's `.get(k, "")` default after str().
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

// asFloat coerces a decoded JSON value to float64 (Python float(obj.get(k, def))), returning def on
// a missing/unparseable value. json.Unmarshal decodes numbers to float64; a quoted number string is
// tolerated like Python's float("0.5").
func asFloat(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		if fv, err := x.Float64(); err == nil {
			return fv
		}
	case string:
		if fv, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return fv
		}
	}
	return def
}

// asBool coerces a decoded JSON value to a bool, tolerating a bare bool or a "true"/"false"/"yes"/"no"
// string (small models sometimes quote the boolean). Returns def for anything else.
func asBool(v any, def bool) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "y", "1":
			return true
		case "false", "no", "n", "0":
			return false
		}
	}
	return def
}

// clamp01 clamps to [0,1] (Python max(0.0, min(1.0, x))).
func clamp01(x float64) float64 { return math.Max(0.0, math.Min(1.0, x)) }

// contains reports whether xs contains s (Python `s in options`).
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// head returns the first n runes of s (Python s[:n]) — used for console summaries / log clips.
func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pyRepr renders a Python repr() of a string for the parse-fail summary (Python `{raw[:48]!r}`):
// single-quoted, backslash-escaped. Only the cases the parse-fail summary needs are handled.
func pyRepr(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// ftoa2 formats a float with 2 decimals (Python f"{value:.2f}").
func ftoa2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// itoa formats an int (Python str(i)). Kept local so the package needs no strconv at every site.
func itoa(n int) string { return strconv.Itoa(n) }

// ---------------------------------------------------------------------------
// tiny error helpers (stdlib errors, no fmt — keep the leaf small)
// ---------------------------------------------------------------------------

// errString is a minimal string error (avoids pulling fmt into hot paths).
type errString string

func (e errString) Error() string { return string(e) }

// httpError carries a non-2xx status so the degrade message reads like the Python urllib HTTPError.
type httpError struct{ status int }

func (e *httpError) Error() string { return "HTTP " + itoa(e.status) }

// envInt reads an int env var, returning def on missing/unparseable (Python int(os.environ.get(...))).
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// envFloat reads a float env var, returning def on missing/unparseable.
func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if fv, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return fv
		}
	}
	return def
}

// envBool reads a bool env var (0/1, true/false, yes/no, on/off — case-insensitive), returning def
// on missing/unparseable. Used for the reasoning toggles (THOUGHT_LLM_SALVAGE etc).
func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on", "y":
		return true
	case "0", "false", "no", "off", "n":
		return false
	default:
		return def
	}
}

// thinkingKwargs reads THOUGHT_LLM_ENABLE_THINKING and returns the chat_template_kwargs payload
// that toggles a reasoning model's thinking channel, plus a tri-state label for logging.
//
//	unset / unknown -> (nil, "default"): omit the field entirely; use the model's own default.
//	true            -> ({"enable_thinking": true},  "on")
//	false           -> ({"enable_thinking": false}, "off")
//
// The map is forwarded as the OpenAI-compatible `chat_template_kwargs` field, which LM Studio /
// vLLM splice into the loaded model's Jinja chat template (Qwen3 honours `enable_thinking`; a
// template without the variable simply ignores it — and that no-op is itself the measurement: the
// reasoning-token % in the run report shows whether the toggle actually took). This makes thinking a
// benchmark-controlled variable instead of a GUI fiddle (the "robust + configurable reasoning" goal).
func thinkingKwargs() (map[string]any, string) {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("THOUGHT_LLM_ENABLE_THINKING"))) {
	case "1", "true", "yes", "on", "y":
		return map[string]any{"enable_thinking": true}, "on"
	case "0", "false", "no", "off", "n":
		return map[string]any{"enable_thinking": false}, "off"
	default:
		return nil, "default"
	}
}

// envFields reads a comma-separated list env var (e.g. THOUGHT_LLM_REASONING_FIELDS), returning a
// COPY of def when missing/empty so callers never share the package-level default slice.
func envFields(key string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		out := make([]string, len(def))
		copy(out, def)
		return out
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = make([]string, len(def))
		copy(out, def)
	}
	return out
}
