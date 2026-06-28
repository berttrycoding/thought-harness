// pager.go is the OUTWARD, MODEL-CALLABLE page-FETCH backend — the BrowseComp browse-loop edge
// (capability-enhancement T1.4). It is the sibling of duckduckgo.go: where DuckDuckGo SEARCHES (a query
// -> the top results' title+snippet), a Pager FETCHES one SPECIFIC result page (a URL -> its readable
// text). Together they make the browse loop EMERGENT from the thought graph — a sub-agent can web_search,
// SEE a promising result URL in the observation, then fetch_url that URL and ground on the page. There is
// NO hardcoded multi-step loop here: a Pager fetches exactly one page; each browse step is an independent
// dispatch driven by what the conscious stream already holds.
//
// A SEPARATE, FOCUSED INTERFACE (PageFetcher), NOT a new method on Web. The Web seam is search-only
// (Fetch(query) -> a search snippet); page-fetching by URL is a distinct op with a distinct argument
// (a URL, not a query), so adding it as its own one-method interface keeps the existing search seam
// (Wall / Fake / DuckDuckGo) completely undisturbed (no implementer churn, no risk to the byte-identical
// fetch_web / web_search paths). Result is REUSED — a page read is the same frozen {Text,OK,Source}
// snapshot a search read is.
//
// STDLIB-ONLY (CLAUDE.md: core engine no third-party deps). The fetch is net/http; the HTML is reduced to
// readable text by regexp/strings over the same shallow-scrape shape duckduckgo.go uses (drop
// script/style, strip tags, decode entities via the stdlib html package, collapse whitespace, cap length)
// — NO third-party HTML parser, NO x/net/html. It is deliberately a SHALLOW, best-effort extraction (good
// enough to ground a lookup on a page's prose), not a faithful DOM render.
//
// BEST-EFFORT BY CONTRACT (the same contract as Web). A page fetch reaches the open network, so any
// transport error, timeout, non-2xx, an empty body, an oversized body, or a page that reduces to no
// readable text yields Result{OK:false} with empty Text — NEVER a crash, NEVER a partial/garbage read
// voiced as fact (the Filter exists to kill laundered hallucination; a failed fetch must read as "no
// reading", not a confident answer).
//
// EDGE-ONLY. Construct Pager ONLY at the edge (CLI / bench wiring), exactly like Wall / DuckDuckGo —
// never inside engine logic. The engine stays headless-pure; the only net/http in the fetch-page path
// lives here. Tests inject FakePager (fixed page text) — never the live endpoint — so the suite stays
// deterministic and offline. The HTML->text EXTRACTOR is unit-tested against a captured HTML fixture
// STRING, not the live network.
package web

import (
	"context"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// pageTimeout is the short, hard wall-clock budget for a page fetch — a distal read must never stall the
// loop. Best-effort: exceeding it yields OK=false.
const pageTimeout = 8 * time.Second

// pageReadBodyCap bounds how many bytes are read off the wire before giving up — a hard ceiling so a
// hostile/huge page cannot exhaust memory. A page is larger than a search-result page, but still bounded;
// an over-cap body is treated as a failure (best-effort).
const pageReadBodyCap = 2 << 20 // 2 MiB

// pageTextCap bounds the extracted page text (code points) so a fetch can never dump an unbounded page
// into grounding. A page read carries more than a search snippet (it is the document's prose, not a
// one-line cue), but it is still capped — ~4000 chars is enough to ground a lookup on the readable body
// without flooding the conscious stream.
const pageTextCap = 4000

// PageFetcher is the one outward page-fetch port — the model-callable fetch_url tool's seam. Production
// wires Pager (a real HTTP GET + HTML->text extraction); tests wire FakePager (fixed text). The interface
// IS the injected seam — the engine never calls net/http directly. Distinct from Web (search-only) so the
// existing search seam is undisturbed; a nil PageFetcher anywhere means PAGE-BLIND (no fetch is ever
// performed and the fetch_url tool errors honestly).
type PageFetcher interface {
	FetchPage(url string) Result
}

// Pager does a real, best-effort HTTP GET of a specific URL and reduces the response to readable text.
// It implements PageFetcher. Construct ONLY at the edge with NewPager(); tests use FakePager instead. A
// nil Client defaults to a short-timeout client. Best-effort: any failure path returns Result{OK:false}.
type Pager struct {
	// Client is the HTTP client; nil ⇒ a short-timeout client (pageTimeout). A bounded timeout is
	// mandatory — a distal fetch must never block the cognitive loop on a slow page.
	Client *http.Client
}

// NewPager builds a page fetcher with a short-timeout client. Construct at the edge only (CLI / bench),
// never inside engine logic.
func NewPager() *Pager { return &Pager{Client: &http.Client{Timeout: pageTimeout}} }

// FetchPage does the real GET and extracts the page's readable text. Best-effort: on ANY error (empty
// url, bad request build, transport failure, timeout, non-2xx, empty/oversized body, OR a page that
// reduces to NO readable text) it returns Result{OK:false} — never a panic, never a partial read voiced
// as fact. On success it returns the extracted, collapsed, capped page text with OK=true and the URL host
// as the source. The caller (the fetch_url tool) is responsible for rejecting a non-http(s) url BEFORE
// reaching here; FetchPage also defends the same scheme requirement so a direct caller can never make it
// GET a file:// / data: / other-scheme target.
func (p *Pager) FetchPage(rawURL string) Result {
	u := strings.TrimSpace(rawURL)
	if !isHTTPURL(u) {
		return Result{}
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: pageTimeout}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}
	}
	// A polite, realistic User-Agent: many sites serve a challenge page to an empty UA (which the
	// extractor would then reduce to no useful text — read as a failed fetch, never voiced).
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; thought-harness/fetch_url; +stdlib)")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, pageReadBodyCap+1))
	if err != nil || len(body) == 0 || len(body) > pageReadBodyCap {
		return Result{}
	}
	text := extractReadableText(string(body))
	if text == "" {
		// A 2xx page that reduces to no readable text (an empty body, a pure-markup / JS-only shell, a
		// challenge page) is best-effort: report a failed read, never voice the raw markup.
		return Result{}
	}
	return Result{Text: text, OK: true, Source: hostOf(u)}
}

// FakePager is the deterministic test double for PageFetcher: it returns a FIXED page text, so a
// page-fetch test is exactly reproducible (the fetch_url analogue of web.Fake). A real network read would
// vary run-to-run and break golden determinism — the FakePager is the offline, byte-stable stand-in. The
// fixed result is independent of the url (the test asserts the stable value).
type FakePager struct {
	R Result
}

// NewFakePager builds a FakePager at a fixed, arbitrary page text (determinism needs stability, not
// realism). The value reads as a short readable page body so a grounding test sees plausible page text.
func NewFakePager() *FakePager {
	return &FakePager{R: Result{Text: "The fetched page reports: the first transcontinental railroad was completed in 1869 at Promontory Summit.", OK: true, Source: "fake"}}
}

// FetchPage returns the fake's fixed result (ignoring the url — the value is what makes a test stable).
func (f *FakePager) FetchPage(string) Result { return f.R }

var (
	// reScriptStyle drops PAIRED <script>...</script> and <style>...</style> blocks WHOLE (their inner text
	// is code/CSS, not readable prose). Non-greedy + DOTALL so a multi-line block is removed entirely. The
	// two are matched as SEPARATE alternatives so <script>…</style> can NOT cross-match the wrong closer.
	reScriptStyle = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>`)

	// reScriptStyleOpen drops an UNCLOSED <script/<style (an opening tag with no matching close) to the end
	// of the input. Applied AFTER the paired removal, so only a genuinely-unclosed block remains — without
	// this a malformed page ("<p>hi</p><script>var x=…") would leak its JS/CSS body as voiced prose.
	reScriptStyleOpen = regexp.MustCompile(`(?is)<(script|style)\b.*$`)

	// reHTMLComment drops <!-- ... --> comments (never readable prose).
	reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

	// reAnyTag strips any remaining HTML tag, leaving its inner text. Replaced with a space so adjacent
	// block elements do not run their text together ("<p>a</p><p>b</p>" -> "a b", not "ab"). The '<' must be
	// followed by a letter, '/', '!' or '?' to count as a tag open, so a literal '<' in prose ("5 < 10 and
	// 10 > 5") is PRESERVED rather than swallowed up to the next '>'.
	reAnyTag = regexp.MustCompile(`(?s)</?[a-zA-Z!?][^>]*>`)
)

// extractReadableText reduces a raw HTML page to plain, readable, collapsed, length-capped text — the
// STDLIB-ONLY shallow extraction (the same shape as duckduckgo.cleanHTMLText, generalized to a whole
// page): drop script/style/comment blocks, strip every remaining tag (to a space so block text does not
// merge), decode HTML entities via the stdlib html package, collapse runs of whitespace to single spaces,
// and cap to pageTextCap code points. Returns "" when the page holds no readable text (an empty / markup-
// only / JS-shell page) — the best-effort failure signal FetchPage turns into OK=false. Pure string ops,
// deterministic (no clock/RNG/network), so it is unit-testable against a captured HTML fixture string.
func extractReadableText(htmlBody string) string {
	s := reScriptStyle.ReplaceAllString(htmlBody, " ")
	s = reScriptStyleOpen.ReplaceAllString(s, " ") // drop any UNCLOSED <script/<style to EOL (no code leak)
	s = reHTMLComment.ReplaceAllString(s, " ")
	s = reAnyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return capRunes(s, pageTextCap)
}

// isHTTPURL reports whether u is an http(s):// URL — the only schemes fetch_url / Pager will GET. A
// non-http(s) target (file://, data:, ftp://, a bare word, a relative path) is rejected so the fetcher
// can never be steered at a local-file / data-scheme read. Pure string op, deterministic.
func isHTTPURL(u string) bool {
	u = strings.TrimSpace(u)
	low := strings.ToLower(u)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}
