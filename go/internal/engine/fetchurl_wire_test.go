package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// toolCallFetchURL builds a fetch_url ToolCall for the given URL (the dispatch shape the floor issues).
func toolCallFetchURL(url string) action.ToolCall {
	return action.ToolCall{Name: "fetch_url", Args: map[string]any{"url": url}}
}

// TestFetchURLFlagOnWiresToolAndScope is the engine WIRING-GATE test for subconscious.fetch_url (T1.4):
// with the flag ON the engine (1) registers the fetch_url tool in the action registry and (2) grants the
// expose-affordances operator the fetch_url tool scope. Both are flag-gated edges (engine.go +
// operators.GrantToolScope) — a built-but-unwired feature would pass neither. The page-fetch seam is wired
// via SetPageFetcher (web.FakePager — deterministic, NEVER the live network), so the registered tool
// reaches a real seam and dispatches deterministic page text.
func TestFetchURLFlagOnWiresToolAndScope(t *testing.T) {
	feat := config.New() // AllOn
	feat.Subconscious.FetchURL = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat)
	e.SetPageFetcher(web.NewFakePager()) // the deterministic offline seam (the edge would wire web.NewPager())

	// (1) the fetch_url tool is REGISTERED + DISPATCHABLE: it runs through the same gated executor as
	//     read_file/web_search and returns the FakePager page text (lazyPager reaches e.pageFetcher at
	//     Execute time).
	res := e.executor.Execute(toolCallFetchURL("https://example.org/page"))
	if res.IsError {
		t.Fatalf("fetch_url ON must be registered + dispatch through the executor; got IsError code=%q", res.ErrorCode)
	}
	if !strings.Contains(res.Content, web.NewFakePager().R.Text) {
		t.Fatalf("fetch_url ON must return the wired seam's page text; got %q", res.Content)
	}

	// (2) the expose-affordances operator was GRANTED the fetch_url scope (the lookup operator can now fetch
	//     a result page alongside its search/read_file local tools).
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if !containsStr(spec.ToolScope, "fetch_url") {
		t.Fatalf("flag ON: expose-affordances ToolScope must include fetch_url; got %v", spec.ToolScope)
	}
}

// TestFetchURLFlagOffByteIdentical is the byte-identical-OFF arm: with the flag OFF (the default), the
// fetch_url tool is NOT registered (a dispatch errors as an unknown tool) AND the expose-affordances scope
// is unchanged ({search, read_file}). No registration, no scope add, no network — the pipeline is
// byte-identical to the pre-flag engine.
func TestFetchURLFlagOffByteIdentical(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn, fetch_url OFF (default)

	// (1) fetch_url is NOT in the registry — an unknown-tool dispatch errors (never a fabricated result).
	res := e.executor.Execute(toolCallFetchURL("https://example.org/page"))
	if !res.IsError {
		t.Fatalf("fetch_url OFF must NOT be registered (dispatch must error); got content=%q", res.Content)
	}

	// (2) expose-affordances keeps its default scope — no fetch_url granted.
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if containsStr(spec.ToolScope, "fetch_url") {
		t.Fatalf("flag OFF: expose-affordances must NOT carry fetch_url (byte-identical); got %v", spec.ToolScope)
	}
}

// TestFetchURLFlagOnNoSeamIsInert pins the double-gate: the flag ON but NO page-fetch seam wired
// (SetPageFetcher never called — the go-test default) makes fetch_url a BLIND read (IsError, no content) —
// it never fabricates a result and never dials the network. This is why the offline suite stays
// byte-identical in BEHAVIOUR even when the knob is on with no edge wired.
func TestFetchURLFlagOnNoSeamIsInert(t *testing.T) {
	feat := config.New()
	feat.Subconscious.FetchURL = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat) // NO SetPageFetcher — page-blind

	res := e.executor.Execute(toolCallFetchURL("https://example.org/page"))
	if !res.IsError {
		t.Fatalf("fetch_url with no seam wired must be a blind read (IsError), not a fabricated result; got %q", res.Content)
	}
}
