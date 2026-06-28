package action

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/web"
)

// FetchURL is the OUTWARD, MODEL-CALLABLE page-FETCH tool — the BrowseComp browse-loop primitive
// (capability-enhancement T1.4) and the sibling of WebSearch. Where web_search SEARCHES (a query -> the
// top results' title+snippet), fetch_url FETCHES one SPECIFIC result page (a URL -> its readable text)
// across the injected web.PageFetcher seam (web.Pager at the edge, web.FakePager in tests). Together they
// make the browse loop EMERGENT from the thought graph: a sub-agent web_search-es, SEES a promising URL in
// the observation, then fetch_url-s that URL and grounds on the page. There is NO hardcoded multi-step loop
// — each fetch is one independent dispatch driven by what the conscious stream already holds.
//
// Category inspect/external (identical to web_search): a network READ that mutates nothing — so the
// gate-router routes it like any other external read and the §3.3a scope admits it to a read-scoped
// sub-agent. It is NOT a world-change (no Authored requirement).
//
// Best-effort by contract (inherited from web.PageFetcher): a nil seam (page-blind), a missing/empty url,
// a non-http(s) url, or a failed read yields IsError with no Content — never a crash, never a fabricated
// page voiced as a result. The bound (body cap + returned-text cap) lives in the seam (web.Pager).
type FetchURL struct{ pager web.PageFetcher }

// NewFetchURL wraps an injected page-fetch seam. Construct at the edge with web.NewPager() (real) or in
// tests with web.NewFakePager() (deterministic); a nil seam is page-blind (every call errors honestly).
func NewFetchURL(p web.PageFetcher) *FetchURL { return &FetchURL{pager: p} }

func (t *FetchURL) Name() string { return "fetch_url" }

// Category: a page read senses the network and changes nothing (inspect/external) — the same tag
// web_search carries, so it routes + scopes identically.
func (t *FetchURL) Category() TaxClass { return TaxClass{Op: OpInspect, Reach: ReachExternal} }

func (t *FetchURL) Description() string {
	return "Fetch a specific web page by URL and return its readable text (a network read; mutates nothing). Use after web_search to read a promising result page."
}

func (t *FetchURL) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "description": "the http(s) URL of the page to fetch"},
		},
		"required": []any{"url"},
	}
}

// Execute validates the url (non-empty, http(s)-schemed), then fetches the page through the injected seam.
// A page-blind seam (nil) or an empty/missing url is a blind read (web_blind_or_empty_url). A non-http(s)
// url is rejected up front (fetch_url_bad_url) — the fetcher is never steered at a file:// / data: target.
// A failed read (transport error / non-2xx / empty / oversized / unreadable page) is web_read_failed. On
// success the bounded extracted page text is the Content the hidden seam admits as grounding.
func (t *FetchURL) Execute(args map[string]any) ToolResult {
	u, _ := args["url"].(string)
	u = strings.TrimSpace(u)
	if t.pager == nil || u == "" {
		return ToolResult{Name: "fetch_url", IsError: true, ErrorCode: "web_blind_or_empty_url"}
	}
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return ToolResult{Name: "fetch_url", IsError: true, ErrorCode: "fetch_url_bad_url"}
	}
	res := t.pager.FetchPage(u)
	if !res.OK {
		return ToolResult{Name: "fetch_url", IsError: true, ErrorCode: "web_read_failed"}
	}
	return ToolResult{Name: "fetch_url", Content: res.Text}
}
