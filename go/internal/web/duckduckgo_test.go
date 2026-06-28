package web

import (
	"strings"
	"testing"
)

// ddgFixtureHTML is a synthetic-but-faithful fragment of a DuckDuckGo HTML (html.duckduckgo.com/html/)
// result page: the result__a title anchors + result__snippet snippets in DDG's real shape, with the
// nested <b> highlight tags and HTML entities (&#x27; &amp;) the live page carries. The parser is
// tested against this STRING — never the live network — so the test is deterministic + offline.
const ddgFixtureHTML = `<!DOCTYPE html><html><body>
<div class="result results_links results_links_deep web-result ">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="https://example.org/curie">Marie Curie - Wikipedia</a>
    </h2>
    <a class="result__snippet" href="https://example.org/curie">Marie <b>Curie</b> won the Nobel Prize in Physics in 1903 &amp; Chemistry in 1911, the first person to win in two sciences.</a>
  </div>
</div>
<div class="result results_links results_links_deep web-result ">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="https://example.org/nobel">The Nobel Prize&#x27;s history</a>
    </h2>
    <a class="result__snippet" href="https://example.org/nobel">A list of every laureate from 1901 onward, with their fields and years.</a>
  </div>
</div>
<div class="result results_links">
  <h2 class="result__title">
    <a rel="nofollow" class="result__a" href="https://example.org/phys">Physics laureates</a>
  </h2>
  <a class="result__snippet" href="https://example.org/phys">Notable winners include Einstein (1921) and Bohr (1922).</a>
</div>
</body></html>`

// TestParseDDGResultsExtractsTitleAndSnippet feeds the captured HTML fixture to the PARSER ONLY (no
// network) and asserts it extracts the top results' title + snippet, strips the highlight tags,
// decodes the entities, and pairs them positionally.
func TestParseDDGResultsExtractsTitleAndSnippet(t *testing.T) {
	got := parseDDGResults(ddgFixtureHTML)
	if got == "" {
		t.Fatal("parser returned empty on a valid result page")
	}
	// First result: title joined to snippet, <b> stripped, &amp; decoded to "&".
	if !strings.Contains(got, "Marie Curie - Wikipedia: Marie Curie won the Nobel Prize") {
		t.Errorf("missing/garbled first result title:snippet pairing in:\n%s", got)
	}
	if !strings.Contains(got, "Physics in 1903 & Chemistry in 1911") {
		t.Errorf("HTML entity &amp; not decoded to &:\n%s", got)
	}
	// Second result: numeric entity &#x27; decoded to an apostrophe.
	if !strings.Contains(got, "The Nobel Prize's history") {
		t.Errorf("numeric entity &#x27; not decoded:\n%s", got)
	}
	// Third result present (3 < cap of 5).
	if !strings.Contains(got, "Physics laureates: Notable winners include Einstein") {
		t.Errorf("third result missing:\n%s", got)
	}
	// No leftover HTML tags.
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("HTML tags leaked into the snippet:\n%s", got)
	}
}

// TestParseDDGResultsCapsResultCount asserts only the top ddgResultCap (5) results are folded in, even
// when the page holds more — a search is a few grounding cues, not a page dump.
func TestParseDDGResultsCapsResultCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString(`<a class="result__a" href="x">Title `)
		b.WriteString(strings.Repeat("n", 1))
		b.WriteString(`</a><a class="result__snippet" href="x">snippet body here</a>`)
	}
	got := parseDDGResults(b.String())
	if n := strings.Count(got, "\n") + 1; n != ddgResultCap {
		t.Errorf("folded %d result lines, want cap %d", n, ddgResultCap)
	}
}

// TestParseDDGResultsNoResultsIsEmpty asserts a page with NO result__a anchors (a bot-challenge /
// no-results / changed-layout page) yields "" — the best-effort failure signal Fetch turns into
// Result{OK:false}, never the raw page voiced as fact.
func TestParseDDGResultsNoResultsIsEmpty(t *testing.T) {
	for _, page := range []string{
		"",
		"<html><body>If this error persists, please let us know. Anomaly detected.</body></html>",
		"<html><body><div class='no-results'>No results found.</div></body></html>",
	} {
		if got := parseDDGResults(page); got != "" {
			t.Errorf("challenge/no-results page parsed to non-empty %q (want \"\")", got)
		}
	}
}

// TestParseDDGResultsTitleOnly asserts a result with a title but no snippet still yields the title (the
// snippet is optional — never a crash on the positional pairing).
func TestParseDDGResultsTitleOnly(t *testing.T) {
	page := `<a class="result__a" href="x">A bare title with no snippet element</a>`
	got := parseDDGResults(page)
	if got != "A bare title with no snippet element" {
		t.Errorf("title-only result = %q", got)
	}
}

// TestDuckDuckGoEmptyQueryIsBlind asserts an empty query never dials the network — it is a blind read
// (OK=false), best-effort.
func TestDuckDuckGoEmptyQueryIsBlind(t *testing.T) {
	d := NewDuckDuckGo()
	if res := d.Fetch("   "); res.OK {
		t.Errorf("empty query returned OK=true (must be a blind read)")
	}
}
