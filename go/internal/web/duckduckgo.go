// duckduckgo.go is the OUTWARD, MODEL-CALLABLE web SEARCH backend behind the web.Web seam — the GAIA-
// enablement edge. Unlike Wall (the ambient one-line fetch_web SENSOR that points at a plain-text
// endpoint and caps at 240), DuckDuckGo is the on-demand SEARCH a sub-agent dispatches via the
// web_search tool: it GETs the DuckDuckGo HTML endpoint for a query and extracts the top results'
// title+snippet so the harness can ground a web-lookup question.
//
// STDLIB-ONLY (CLAUDE.md: core engine no third-party deps). The fetch is net/http + net/url; the HTML
// is parsed by regex/strings over DuckDuckGo's stable result__a / result__snippet markup, with HTML
// entities decoded by the stdlib html package — NO third-party HTML parser, NO x/net/html. This is
// deliberately a SHALLOW, best-effort scrape (the markup is the search engine's, not ours), not a
// general HTML DOM — see the caveats below.
//
// BEST-EFFORT BY CONTRACT (inherited from web.Web). A web search reaches the open network, so it is
// the most failure-prone read of all: any transport error, timeout, non-2xx, an empty body, a
// bot-challenge / rate-limit page that yields NO parseable results ⇒ Result{OK:false} with an empty
// snippet — NEVER a crash, NEVER a partial/garbage page voiced as fact (the Filter exists to kill
// laundered hallucination; a failed read must read as "no reading", not as a confident answer).
//
// EDGE-ONLY. Construct DuckDuckGo ONLY at the edge (CLI / bench wiring), exactly like Wall — never
// inside engine logic. The engine stays headless-pure; the only net/http in the search path lives here.
// Tests inject web.Fake (a fixed snippet) — never the live endpoint — so the suite stays deterministic
// and offline. The PARSER is unit-tested against a captured HTML fixture STRING, not the live network.
package web

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ddgEndpoint is the DuckDuckGo HTML (non-JS) search endpoint. It returns server-rendered HTML with
// result__a (title anchor) + result__snippet (snippet) markup that the stdlib parser below scrapes —
// no JavaScript, no API key. The query is URL-encoded onto ?q=.
const ddgEndpoint = "https://html.duckduckgo.com/html/"

// ddgTimeout is the short, hard wall-clock budget for a search — a distal read must never stall the
// loop. Best-effort: exceeding it yields OK=false.
const ddgTimeout = 6 * time.Second

// ddgResultCap bounds how many top results are folded into the snippet — a search is a few grounding
// cues, not a page dump.
const ddgResultCap = 5

// ddgTextCap bounds the joined snippet length (code points) so a search can never dump an unbounded
// page into grounding. ~1500 chars holds the top-5 title:snippet lines comfortably.
const ddgTextCap = 1500

// ddgReadBodyCap bounds how many bytes are read off the wire before giving up — a hard ceiling so a
// hostile/huge response cannot exhaust memory. DDG HTML result pages are well under this; an over-cap
// body is treated as a failure (best-effort).
const ddgReadBodyCap = 1 << 20 // 1 MiB

// DuckDuckGo is a real, best-effort web search across the DuckDuckGo HTML endpoint. It implements
// web.Web. Construct ONLY at the edge with NewDuckDuckGo(); tests use web.Fake instead. A nil Client
// defaults to a short-timeout client. Best-effort: any failure path returns Result{OK:false}.
type DuckDuckGo struct {
	// Endpoint is the search endpoint (defaults to ddgEndpoint when empty). Configurable for a test
	// that wants to point a fixture HTTP server at it (the parser itself is tested directly, offline).
	Endpoint string
	// Client is the HTTP client; nil ⇒ a short-timeout client (ddgTimeout). A bounded timeout is
	// mandatory — a distal search must never block the cognitive loop on a slow endpoint.
	Client *http.Client
}

// NewDuckDuckGo builds a DuckDuckGo search backend at the default endpoint + a short-timeout client.
// Construct at the edge only (CLI / bench), never inside engine logic.
func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{Endpoint: ddgEndpoint, Client: &http.Client{Timeout: ddgTimeout}}
}

// Fetch does the real search GET and extracts the top results. Best-effort: on ANY error (empty query,
// bad request build, transport failure, timeout, non-2xx, empty/oversized body, OR an HTML page that
// yields no parseable results — a challenge / rate-limit / no-results page) it returns Result{OK:false}
// — never a panic, never a partial read voiced as fact. On success it returns the joined top-N
// "title: snippet" lines (collapsed, capped) with OK=true and "duckduckgo" as the source.
func (d *DuckDuckGo) Fetch(query string) Result {
	q := strings.TrimSpace(query)
	if q == "" {
		return Result{}
	}
	endpoint := d.Endpoint
	if endpoint == "" {
		endpoint = ddgEndpoint
	}
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: ddgTimeout}
	}
	ctx, cancel := context.WithTimeout(context.Background(), ddgTimeout)
	defer cancel()
	u := endpoint + "?q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}
	}
	// A polite, realistic User-Agent: the HTML endpoint serves the lite result markup to a plain agent;
	// an empty UA is more likely to draw a challenge page (which the parser then reads as no results).
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; thought-harness/web_search; +stdlib)")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, ddgReadBodyCap+1))
	if err != nil || len(body) == 0 || len(body) > ddgReadBodyCap {
		return Result{}
	}
	text := parseDDGResults(string(body))
	if text == "" {
		// A 2xx page that yields NO parseable results is a bot-challenge / no-results / layout-changed
		// page — best-effort: report a failed read, never voice the raw page.
		return Result{}
	}
	return Result{Text: text, OK: true, Source: "duckduckgo"}
}

var (
	// reResultAnchor matches a result TITLE anchor: <a ... class="...result__a...">TITLE</a>. The class
	// attribute is matched loosely (DDG also emits result__a result--more__btn etc.); the captured group
	// is the inner HTML, stripped of nested tags by cleanHTMLText below. Non-greedy + DOTALL so a title
	// that spans a line break is captured whole.
	reResultAnchor = regexp.MustCompile(`(?is)<a\b[^>]*\bclass="[^"]*\bresult__a\b[^"]*"[^>]*>(.*?)</a>`)

	// reResultSnippet matches a result SNIPPET: <a|td ... class="...result__snippet...">SNIPPET</a|td>.
	// DDG renders the snippet as an <a class="result__snippet"> in the HTML endpoint; allow either tag
	// defensively so a minor markup change still scrapes.
	reResultSnippet = regexp.MustCompile(`(?is)<(?:a|td)\b[^>]*\bclass="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</(?:a|td)>`)

	// reTag strips any HTML tag (the title/snippet inner HTML carries <b> highlight tags etc.).
	reTag = regexp.MustCompile(`(?s)<[^>]*>`)
)

// parseDDGResults extracts the top results from a DuckDuckGo HTML result page and joins them into one
// capped snippet. It is the STDLIB-ONLY scrape: it pulls the result__a titles + result__snippet
// snippets by regex, decodes their HTML entities + strips highlight tags, pairs them positionally
// (the i-th title with the i-th snippet — DDG renders them in matching order), and joins the top
// ddgResultCap as "title: snippet" lines, capped to ddgTextCap code points. Returns "" when the page
// holds no result__a anchors (a challenge / no-results / changed-layout page) — the best-effort
// failure signal Fetch turns into OK=false. Pure string ops, deterministic (no clock/RNG/network).
func parseDDGResults(htmlBody string) string {
	titles := extractAll(reResultAnchor, htmlBody)
	if len(titles) == 0 {
		return ""
	}
	snippets := extractAll(reResultSnippet, htmlBody)

	var lines []string
	for i := 0; i < len(titles) && len(lines) < ddgResultCap; i++ {
		title := titles[i]
		if title == "" {
			continue
		}
		line := title
		if i < len(snippets) && snippets[i] != "" {
			line = title + ": " + snippets[i]
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return capRunes(strings.Join(lines, "\n"), ddgTextCap)
}

// extractAll pulls every capture-group-1 match of re from s, strips its inner HTML tags, decodes HTML
// entities, and collapses whitespace — yielding the clean text of each result element in document order.
// Empty cleaned strings are dropped so a positional pair never lines up against blank text.
func extractAll(re *regexp.Regexp, s string) []string {
	matches := re.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		clean := cleanHTMLText(m[1])
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

// cleanHTMLText strips HTML tags, unescapes HTML entities (&amp; &#39; &quot; …) via the stdlib html
// package, and collapses runs of whitespace to single spaces — turning a result element's inner HTML
// into plain one-line text.
func cleanHTMLText(s string) string {
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// capRunes caps s to n code points (never splitting a multibyte rune), trimming surrounding space.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return strings.TrimSpace(string(r))
}
