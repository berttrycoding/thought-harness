package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// toolCallWeb builds a web_search ToolCall for the given query (the dispatch shape the floor issues).
func toolCallWeb(query string) action.ToolCall {
	return action.ToolCall{Name: "web_search", Args: map[string]any{"query": query}}
}

// TestWebSearchFlagOnWiresToolAndScope is the engine WIRING-GATE test for subconscious.web_search: with
// the flag ON the engine (1) registers the web_search tool in the action registry and (2) grants the
// expose-affordances operator the web_search tool scope. Both are flag-gated edges (engine.go +
// operators.GrantToolScope) — a built-but-unwired feature would pass neither. The web seam is wired via
// SetWeb (web.Fake — deterministic, NEVER the live network), so the registered tool reaches a real seam
// and dispatches a deterministic snippet.
func TestWebSearchFlagOnWiresToolAndScope(t *testing.T) {
	feat := config.New() // AllOn
	feat.Subconscious.WebSearch = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat)
	e.SetWeb(web.NewFake()) // the deterministic offline seam (the edge would wire web.NewDuckDuckGo())

	// (1) the web_search tool is REGISTERED + DISPATCHABLE: it runs through the same gated executor as
	//     read_file/search and returns the Fake snippet (the lazyWeb seam reaches e.web at Execute time).
	res := e.executor.Execute(toolCallWeb("the population of France"))
	if res.IsError {
		t.Fatalf("web_search ON must be registered + dispatch through the executor; got IsError code=%q", res.ErrorCode)
	}
	if !strings.Contains(res.Content, web.NewFake().R.Text) {
		t.Fatalf("web_search ON must return the wired seam's snippet; got %q", res.Content)
	}

	// (2) the expose-affordances operator was GRANTED the web_search scope (the lookup operator can now
	//     reach the web alongside its search/read_file local tools).
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if !containsStr(spec.ToolScope, "web_search") {
		t.Fatalf("flag ON: expose-affordances ToolScope must include web_search; got %v", spec.ToolScope)
	}
}

// TestWebSearchFlagOffByteIdentical is the byte-identical-OFF arm: with the flag OFF (the default), the
// web_search tool is NOT registered (a dispatch errors as an unknown tool) AND the expose-affordances
// scope is unchanged ({search, read_file}). No registration, no scope add, no network — the pipeline is
// byte-identical to the pre-flag engine.
func TestWebSearchFlagOffByteIdentical(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn, web_search OFF (default)

	// (1) web_search is NOT in the registry — an unknown-tool dispatch errors (never a fabricated result).
	res := e.executor.Execute(toolCallWeb("the population of France"))
	if !res.IsError {
		t.Fatalf("web_search OFF must NOT be registered (dispatch must error); got content=%q", res.Content)
	}

	// (2) expose-affordances keeps its default {search, read_file} scope — no web_search granted.
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if containsStr(spec.ToolScope, "web_search") {
		t.Fatalf("flag OFF: expose-affordances must NOT carry web_search (byte-identical); got %v", spec.ToolScope)
	}
}

// TestWebSearchFlagOnNoSeamIsInert pins the double-gate: the flag ON but NO web seam wired (SetWeb never
// called — the go-test default) makes web_search a BLIND read (IsError, no content) — it never fabricates
// a result and never dials the network. This is why the offline suite stays byte-identical in BEHAVIOUR
// even when the knob is on with no edge wired.
func TestWebSearchFlagOnNoSeamIsInert(t *testing.T) {
	feat := config.New()
	feat.Subconscious.WebSearch = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat) // NO SetWeb — web-blind

	res := e.executor.Execute(toolCallWeb("anything"))
	if !res.IsError {
		t.Fatalf("web_search with no seam wired must be a blind read (IsError), not a fabricated result; got %q", res.Content)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
