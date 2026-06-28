package action

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/web"
)

// TestWebSearchToolDropsIntoTheToolSystem is the PROOF that a web tool is a SMALL change in the
// subconscious's tool system: it implements the 5-method Tool contract, registers with one call, and
// dispatches a real query through the existing web.Web seam — using the SAME machinery (registry +
// executor + gate-router + sub-agent toolScope) that already drives read_file/search/run_shell.
func TestWebSearchToolDropsIntoTheToolSystem(t *testing.T) {
	tool := NewWebSearch(web.NewFake()) // deterministic seam (the offline double)

	// (1) It satisfies the Tool contract + declares the right taxonomy (a network READ).
	var _ Tool = tool
	if tool.Name() != "web_search" {
		t.Fatalf("name = %q", tool.Name())
	}
	if c := tool.Category(); c.Op != OpInspect || c.Reach != ReachExternal {
		t.Fatalf("category = %s, want inspect/external", c)
	}

	// (2) It registers with ONE call and is retrievable by name (the registry path).
	r := NewToolRegistry([]Tool{tool})
	got, ok := r.Get("web_search")
	if !ok {
		t.Fatal("web_search not in the registry after Register")
	}

	// (3) A dispatch flows query -> web seam -> result content (the Fake's fixed snippet).
	res := got.Execute(map[string]any{"query": "June 2022 arXiv AI regulation paper"})
	if res.IsError || res.Content == "" {
		t.Fatalf("expected a snippet from the seam, got IsError=%v content=%q", res.IsError, res.Content)
	}

	// (4) Best-effort honesty: web-blind or empty query => IsError, never a crash or fabricated fact.
	if blind := (&WebSearch{}).Execute(map[string]any{"query": "x"}); !blind.IsError {
		t.Error("nil web seam (web-blind) must return IsError, not a fabricated result")
	}
	if empty := tool.Execute(map[string]any{}); !empty.IsError {
		t.Error("empty query must return IsError")
	}
}
