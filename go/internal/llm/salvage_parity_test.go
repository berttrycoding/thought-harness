package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// salvageFixture is the recorded Python ground truth for the defensive-JSON salvage path. Captured
// by running the Python module-level functions in llm.py over messy / malformed model replies:
//
//	_loads_object, _strip_think, _extract_content
//
// (see the generator in the task log). The Tier-6 llm gate (PORT-PLAN §2 row 6): "llm parsing
// salvages the same content from recorded messy-model-reply fixtures (and falls back identically on
// malformed input)". This test feeds the SAME recorded replies through the GO salvage functions and
// asserts identical salvaged content + identical success/failure on malformed input.
type salvageFixture struct {
	LoadsObject    []loadsObjectCase `json:"loads_object"`
	LoadsObjectBad []loadsObjectCase `json:"loads_object_bad"`
	StripThink     []stripCase       `json:"strip_think"`
	ExtractContent []extractCase     `json:"extract_content"`
}

type loadsObjectCase struct {
	In  string         `json:"in"`
	Obj map[string]any `json:"obj"` // Python-decoded object, or null when it raised
	Err *string        `json:"err"` // Python exception class name, or null on success
}

type stripCase struct {
	In  string `json:"in"`
	Out string `json:"out"`
}

type extractCase struct {
	Msg     map[string]any `json:"msg"`
	Salvage bool           `json:"salvage"`
	Out     string         `json:"out"`
}

func loadSalvageFixture(t *testing.T) salvageFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "py_llm_salvage.json"))
	if err != nil {
		t.Fatalf("read salvage fixture: %v", err)
	}
	var f salvageFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decode salvage fixture: %v", err)
	}
	return f
}

// TestLoadsObjectSalvageParity: the Go loadsObject salvages the same object out of a messy reply
// the Python _loads_object did. Object VALUES are compared by their string projection (asString) for
// the string-valued keys Python recorded, which is what the real callers (parseVerdict/parseRank)
// read; the success/failure is the load-bearing parity (a successful slice vs a raised exception).
func TestLoadsObjectSalvageParity(t *testing.T) {
	f := loadSalvageFixture(t)
	if len(f.LoadsObject) == 0 {
		t.Fatal("no loads_object fixture cases")
	}
	for _, c := range f.LoadsObject {
		if c.Err != nil {
			t.Fatalf("fixture loads_object case unexpectedly recorded an error %q for %q", *c.Err, c.In)
		}
		obj, err := loadsObject(c.In)
		if err != nil {
			t.Errorf("loadsObject(%q) errored %v; Python salvaged %v", c.In, err, c.Obj)
			continue
		}
		// Every string-valued key Python decoded must be present + equal in the Go object.
		for k, v := range c.Obj {
			ps, isStr := v.(string)
			if !isStr {
				continue // non-string values (numbers/arrays) float64-ify in Go; covered by callers
			}
			if got := asString(obj[k]); got != ps {
				t.Errorf("loadsObject(%q)[%q] = %q, want %q (Python)", c.In, k, got, ps)
			}
		}
		// And the key SET must match (Go salvaged neither more nor fewer keys than Python).
		if len(obj) != len(c.Obj) {
			t.Errorf("loadsObject(%q) decoded %d keys, want %d (Python): %v vs %v",
				c.In, len(obj), len(c.Obj), keysOf(obj), keysOf(c.Obj))
		}
	}
}

// TestLoadsObjectFallbackParity: a malformed reply Python raised on must make Go's loadsObject
// return an error (never a panic) — the identical fallback trigger. Python distinguishes ValueError
// (no "{" found → str.index raises) from JSONDecodeError (slice doesn't parse); Go collapses both to
// a non-nil error, which is the behaviour the callers branch on (any error → heuristic fallback).
func TestLoadsObjectFallbackParity(t *testing.T) {
	f := loadSalvageFixture(t)
	if len(f.LoadsObjectBad) == 0 {
		t.Fatal("no loads_object_bad fixture cases")
	}
	for _, c := range f.LoadsObjectBad {
		if c.Err == nil {
			t.Fatalf("fixture loads_object_bad case %q should record a Python exception", c.In)
		}
		if _, err := loadsObject(c.In); err == nil {
			t.Errorf("loadsObject(%q) returned no error; Python raised %s — fallback diverges",
				c.In, *c.Err)
		}
	}
}

// TestStripThinkParity: byte-identical <think>...</think> removal + strip across the language
// boundary (Python _strip_think with re.DOTALL).
func TestStripThinkParity(t *testing.T) {
	f := loadSalvageFixture(t)
	if len(f.StripThink) == 0 {
		t.Fatal("no strip_think fixture cases")
	}
	for _, c := range f.StripThink {
		if got := stripThink(c.In); got != c.Out {
			t.Errorf("stripThink(%q) = %q, want %q (Python)", c.In, got, c.Out)
		}
	}
}

// TestExtractContentParity: byte-identical content extraction (prefer content, salvage
// reasoning_content only when enabled + content empty, strip <think>) across the boundary. Python's
// JSON null for an absent field decodes to a Go nil in the msg map, which asString treats as "" —
// matching Python's `msg.get("content") or ""`.
func TestExtractContentParity(t *testing.T) {
	f := loadSalvageFixture(t)
	if len(f.ExtractContent) == 0 {
		t.Fatal("no extract_content fixture cases")
	}
	for _, c := range f.ExtractContent {
		if got := extractContent(c.Msg, c.Salvage); got != c.Out {
			t.Errorf("extractContent(%v, salvage=%v) = %q, want %q (Python)",
				c.Msg, c.Salvage, got, c.Out)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
