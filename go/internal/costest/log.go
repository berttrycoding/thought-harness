package costest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// jsonlRecord is the wire shape of one --log line (trace.JsonlSink writes exactly this:
// {tick,kind,layer,summary,data}). We only need kind (to select llm.call) and data (the
// prompt + usage payload llm.OpenAICompatBackend put on each call event).
type jsonlRecord struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

// llmCallKind is the event kind carrying a model call's prompt + usage (events.LLM). Pinned
// as a literal so this reader stays decoupled from the events package (it reads a file, not a
// live bus).
const llmCallKind = "llm.call"

// ReadLog parses a --log JSONL file into the llm.call Calls the estimators consume: one Call
// per llm.call event, projecting the event data's system/user/raw text + the (possibly
// absent, -1) usage counts. Non-llm.call lines are skipped; a malformed line is skipped (a
// log can be truncated mid-write — a crashed run leaves a partial last line). It returns an
// error only on an unreadable file, not on a bad line, so a partial pilot log still estimates.
func ReadLog(path string) ([]Call, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("costest: open log %q: %w", path, err)
	}
	defer f.Close()
	return readLog(f)
}

// readLog is the io.Reader core of ReadLog (so tests can feed a buffer without a temp file).
// The scanner buffer is grown to 8 MiB max-token so a single line carrying a large prompt +
// response (the whole point of --log) is not truncated by bufio's 64 KiB default.
func readLog(r io.Reader) ([]Call, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var calls []Call
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec jsonlRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // skip a malformed / truncated line (don't drop the whole log)
		}
		if rec.Kind != llmCallKind {
			continue
		}
		calls = append(calls, callFromData(rec.Data))
	}
	if err := sc.Err(); err != nil {
		return calls, fmt.Errorf("costest: scan log: %w", err)
	}
	return calls, nil
}

// callFromData projects one llm.call event's data map into a Call. The text fields default to
// "" when absent; the usage fields default to -1 (the cost layer's "server did not report
// this" sentinel, distinguished from a true 0) so the estimators know to fall back to text.
func callFromData(d map[string]any) Call {
	return Call{
		Role:              strField(d, "role"),
		Model:             strField(d, "model"),
		System:            strField(d, "system"),
		User:              strField(d, "user"),
		Response:          strField(d, "raw"),
		PromptTokens:      intOrAbsent(d, "prompt_tokens"),
		CompletionTokens:  intOrAbsent(d, "completion_tokens"),
		CachedInputTokens: intOrAbsent(d, "cached_input_tokens"),
	}
}

// strField reads a string value out of an event data map (absent / non-string ⇒ "").
func strField(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if v, ok := d[key].(string); ok {
		return v
	}
	return ""
}

// intOrAbsent reads an int-ish usage value (token counts arrive as float64 after a JSON
// round-trip). A MISSING key returns -1 — the "server did not report this field" sentinel
// (distinguished from a true 0). A present-but-non-numeric value also returns -1. Mirrors the
// bench runner's reader so the cost projections agree across the two paths.
func intOrAbsent(d map[string]any, key string) int {
	if d == nil {
		return -1
	}
	v, ok := d[key]
	if !ok {
		return -1
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return -1
}
