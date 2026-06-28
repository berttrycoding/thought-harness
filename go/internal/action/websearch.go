package action

import "github.com/berttrycoding/thought-harness/internal/web"

// WebSearch is the OUTWARD, MODEL-CALLABLE search tool — the GAIA-enablement primitive. It issues a
// real query across the SAME injected web.Web seam the fetch_web SENSOR uses (web.Wall at the edge,
// web.Fake in tests), but unlike the sensor (one ambient, goal-templated, episode-open read) it is a
// TOOL a sub-agent dispatches ON DEMAND with a query the floor/model formulates (subagent.toolCall →
// modelSelectCall). Category inspect/external: a network READ that mutates nothing — so the gate-router
// routes it like any other external read and the §3.3a scope admits it to a read-scoped sub-agent.
//
// Best-effort by contract (inherited from web.Web): a nil seam (web-blind) or empty query or failed
// read yields IsError with no Content — never a crash, never a fabricated fact voiced as a result.
type WebSearch struct{ web web.Web }

// NewWebSearch wraps an injected web seam. Construct at the edge with web.NewWall() (real) or in tests
// with web.NewFake() (deterministic); a nil seam is web-blind (every call errors honestly).
func NewWebSearch(w web.Web) *WebSearch { return &WebSearch{web: w} }

func (t *WebSearch) Name() string { return "web_search" }

// Category: a web read senses the network and changes nothing (inspect/external).
func (t *WebSearch) Category() TaxClass { return TaxClass{Op: OpInspect, Reach: ReachExternal} }

func (t *WebSearch) Description() string {
	return "Search the web for a query and return a text snippet of the result (a network read; mutates nothing)."
}

func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "the search query"},
		},
		"required": []any{"query"},
	}
}

func (t *WebSearch) Execute(args map[string]any) ToolResult {
	q, _ := args["query"].(string)
	if t.web == nil || q == "" {
		return ToolResult{Name: "web_search", IsError: true, ErrorCode: "web_blind_or_empty_query"}
	}
	res := t.web.Fetch(q)
	if !res.OK {
		return ToolResult{Name: "web_search", IsError: true, ErrorCode: "web_read_failed"}
	}
	return ToolResult{Name: "web_search", Content: res.Text}
}
