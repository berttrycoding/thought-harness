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

// webExecutor builds a real ToolExecutor holding the web_search tool backed by a web.Fake (the
// deterministic offline seam — NEVER the live network in a test). It is the action layer a web_search-
// scoped sub-agent dispatches through.
func webExecutor(t *testing.T) *action.ToolExecutor {
	t.Helper()
	reg := action.NewToolRegistry([]action.Tool{action.NewWebSearch(web.NewFake())})
	return action.NewToolExecutor(reg, nil)
}

// TestWebSearchSubAgentDispatchesAndFoldsResult is the COGNITION/WIRE test for the subconscious.web_search
// dispatch path: a sub-agent scoped to {search, read_file, web_search} on a QUESTION-shaped goal (no local
// file named) dispatches web_search{query=<goal>} through the real executor and FOLDS the Fake snippet into
// the Candidate the hidden seam will admit as grounding — proving the new floorToolCall branch + the
// fireTool fold are genuinely wired, not just present.
func TestWebSearchSubAgentDispatchesAndFoldsResult(t *testing.T) {
	goal := "What year did Marie Curie win her first Nobel Prize?"
	// expose-affordances WITH the web_search grant the flag adds (the engine's GrantToolScope wire).
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "web_search"}

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, webExecutor(t), nil)

	// (1) THE FLOOR PICKS web_search{query=goal} — the new branch, above the local search, on a goal that
	//     names no local file.
	call, ok := sa.floorToolCall()
	if !ok {
		t.Fatal("floorToolCall declined for a web_search-scoped lookup goal")
	}
	if call.Name != "web_search" {
		t.Fatalf("floor picked %q, want web_search (a lookup goal must reach the web, not the local tree)", call.Name)
	}
	if q, _ := call.Args["query"].(string); q != goal {
		t.Fatalf("web_search query = %q, want the whole goal %q", q, goal)
	}

	// (2) Fire DISPATCHES it and FOLDS the Fake snippet into the Candidate text (the grounding fold). The
	//     Fake's fixed snippet is what a deterministic test asserts.
	cand := sa.Fire([]types.Thought{{Text: goal}}, cpyrand.New(1))
	if cand == nil {
		t.Fatal("Fire returned nil — the web_search dispatch produced no Candidate")
	}
	fakeSnippet := web.NewFake().R.Text
	if !strings.Contains(cand.Text, fakeSnippet) {
		t.Fatalf("the Fake web snippet did not fold into the Candidate grounding text:\n got: %q\n want substring: %q", cand.Text, fakeSnippet)
	}
}

// TestWebSearchOffNoDispatch is the byte-identical-OFF arm: WITHOUT web_search in scope (the flag-OFF /
// un-granted case), the SAME lookup goal NEVER picks web_search — the floor falls through to its existing
// search/read_file behaviour exactly as before (here, a local search on the goal keyword). This proves the
// web_search branch is fully gated on the scope grant, so an engine with the flag off is unchanged.
func TestWebSearchOffNoDispatch(t *testing.T) {
	goal := "What year did Marie Curie win her first Nobel Prize?"
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file"} // NO web_search — the default expose-affordances scope

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, webExecutor(t), nil)
	call, ok := sa.floorToolCall()
	if ok && call.Name == "web_search" {
		t.Fatalf("web_search dispatched without the web_search scope grant — the OFF path is not byte-identical")
	}
}

// TestWebSearchYieldsToNamedLocalFile pins the precedence guard: even with web_search in scope, a goal that
// names a CONCRETE LOCAL file still reads the file (the genuinely-local target wins over the web). This is
// what keeps named-file grounding intact when the flag is on.
func TestWebSearchYieldsToNamedLocalFile(t *testing.T) {
	goal := "read the value assigned to ActionMargin in regulator.go"
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "web_search"}

	sa := NewSubAgent(spec, "general", goal, nil, nil, "sa:expose-affordances", scope, webExecutor(t), nil)
	call, ok := sa.floorToolCall()
	if !ok {
		t.Fatal("floorToolCall declined for a named-file lookup goal")
	}
	if call.Name == "web_search" {
		t.Fatalf("a goal naming a concrete local file (regulator.go) must read the file, not web_search; got %q", call.Name)
	}
	if call.Name != "read_file" {
		t.Fatalf("expected read_file for a named local file, got %q", call.Name)
	}
}
