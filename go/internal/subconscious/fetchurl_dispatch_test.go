package subconscious

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// pageExecutor builds a real ToolExecutor holding the fetch_url tool backed by a web.FakePager (the
// deterministic offline page-fetch seam — NEVER the live network in a test). It is the action layer a
// fetch_url-scoped sub-agent dispatches through.
func pageExecutor(t *testing.T) *action.ToolExecutor {
	t.Helper()
	reg := action.NewToolRegistry([]action.Tool{action.NewFetchURL(web.NewFakePager())})
	return action.NewToolExecutor(reg, nil)
}

// TestFetchURLSubAgentDispatchesAndFoldsPage is the COGNITION/WIRE test for the subconscious.fetch_url
// dispatch path (T1.4): a sub-agent scoped to {search, read_file, fetch_url} whose GOAL CARRIES A RESULT
// URL (the emergent browse step — a URL surfaced in a prior web_search observation that became this
// worker's goal) dispatches fetch_url{url=<URL>} through the real executor and FOLDS the FakePager's page
// text into the Candidate the hidden seam will admit as grounding — proving the floorToolCall fetch_url
// branch + the fireTool fold are genuinely wired, not just present.
func TestFetchURLSubAgentDispatchesAndFoldsPage(t *testing.T) {
	url := "https://example.org/transcontinental-railroad"
	goal := "Read the page that came up: " + url + " and report when the railroad was completed."
	// expose-affordances WITH the fetch_url grant the flag adds (the engine's GrantToolScope wire).
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "fetch_url"}

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, pageExecutor(t), nil)

	// (1) THE FLOOR PICKS fetch_url{url=<URL>} — the new branch, ABOVE web_search + the local tree, when the
	//     goal names a concrete URL.
	call, ok := sa.floorToolCall()
	if !ok {
		t.Fatal("floorToolCall declined for a fetch_url-scoped goal carrying a URL")
	}
	if call.Name != "fetch_url" {
		t.Fatalf("floor picked %q, want fetch_url (a goal carrying a URL must fetch that page)", call.Name)
	}
	if u, _ := call.Args["url"].(string); u != url {
		t.Fatalf("fetch_url url = %q, want the URL named in the goal %q", u, url)
	}

	// (2) Fire DISPATCHES it and FOLDS the FakePager's page text into the Candidate text (the grounding
	//     fold). The FakePager's fixed text is what a deterministic test asserts.
	cand := sa.Fire([]types.Thought{{Text: goal}}, cpyrand.New(1))
	if cand == nil {
		t.Fatal("Fire returned nil — the fetch_url dispatch produced no Candidate")
	}
	pageText := web.NewFakePager().R.Text
	if !strings.Contains(cand.Text, pageText) {
		t.Fatalf("the FakePager page text did not fold into the Candidate grounding text:\n got: %q\n want substring: %q", cand.Text, pageText)
	}
}

// TestFetchURLOffNoDispatch is the byte-identical-OFF arm: WITHOUT fetch_url in scope (the flag-OFF /
// un-granted case), the SAME URL-carrying goal NEVER picks fetch_url — the floor falls through to its
// existing search/read_file behaviour exactly as before. This proves the fetch_url branch is fully gated
// on the scope grant, so an engine with the flag off is unchanged.
func TestFetchURLOffNoDispatch(t *testing.T) {
	url := "https://example.org/transcontinental-railroad"
	goal := "Read the page that came up: " + url + " and report when the railroad was completed."
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file"} // NO fetch_url — the default expose-affordances scope

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, pageExecutor(t), nil)
	call, ok := sa.floorToolCall()
	if ok && call.Name == "fetch_url" {
		t.Fatalf("fetch_url dispatched without the fetch_url scope grant — the OFF path is not byte-identical")
	}
}

// TestFetchURLNoURLYieldsToSearch pins the precedence guard: even with fetch_url in scope, a goal that
// names NO URL never dispatches fetch_url — the floor falls through to web_search / the local tree. This is
// what keeps the fetch_url branch firing ONLY on the emergent browse step (a URL is actually present), so a
// plain lookup goal is unaffected.
func TestFetchURLNoURLYieldsToSearch(t *testing.T) {
	goal := "What year was the first transcontinental railroad completed?" // no URL
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "fetch_url"}

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, pageExecutor(t), nil)
	call, ok := sa.floorToolCall()
	if ok && call.Name == "fetch_url" {
		t.Fatalf("a goal naming no URL must NOT dispatch fetch_url (the browse step is URL-gated); got %q", call.Name)
	}
}

// TestFirstURLExtractor pins the URL extractor the floor branch relies on (the emergent-browse-step gate):
// it finds the first http(s) URL in free prose, trims trailing sentence punctuation, and returns "" when no
// URL is present.
func TestFirstURLExtractor(t *testing.T) {
	cases := map[string]string{
		"see https://example.com/page for details": "https://example.com/page",
		"the link was https://example.com/a.html.": "https://example.com/a.html",
		"http://x.org/y, then think":               "http://x.org/y",
		"first https://a.com then https://b.com":   "https://a.com",
		"no url here, just a ratio 3/4 and a date": "",
		"a path config/limits.go is not a url":     "",
		// red-team T1.4 regression: a Wikipedia-style URL KEEPS its balanced parens (was truncated at "(").
		"read https://en.wikipedia.org/wiki/Mercury_(element) please": "https://en.wikipedia.org/wiki/Mercury_(element)",
		"trailing dot too https://en.wikipedia.org/wiki/Foo_(bar).":   "https://en.wikipedia.org/wiki/Foo_(bar)",
		// but a URL closing a parenthetical in prose sheds the UNBALANCED trailing ')'.
		"(see https://example.com/page)": "https://example.com/page",
	}
	for in, want := range cases {
		if got := firstURL(in); got != want {
			t.Errorf("firstURL(%q) = %q, want %q", in, got, want)
		}
	}
}
