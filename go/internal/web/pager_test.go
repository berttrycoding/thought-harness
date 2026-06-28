package web

import (
	"strings"
	"testing"
)

// pageFixtureHTML is a synthetic-but-faithful fragment of a real HTML page: a <head> with <script>/<style>
// blocks (which must be DROPPED — their inner text is code/CSS, not readable prose), an HTML comment, and
// a <body> of prose with nested formatting tags + HTML entities (&amp; &#39;) the live page carries. The
// extractor is tested against this STRING — never the live network — so the test is deterministic+offline.
const pageFixtureHTML = `<!DOCTYPE html><html><head>
<title>First Transcontinental Railroad</title>
<style>body{font-family:sans-serif} .nav{display:none}</style>
<script>var tracking = {id: 42}; function noise(){ return "this must not leak"; }</script>
</head>
<body>
<!-- a comment that must not leak into the readable text -->
<nav class="nav"><a href="/x">Home</a></nav>
<h1>First Transcontinental Railroad</h1>
<p>The <b>First Transcontinental Railroad</b> was completed in <em>1869</em> at Promontory Summit, Utah.</p>
<p>It connected the eastern &amp; western United States &#39;coast to coast&#39;.</p>
</body></html>`

// TestExtractReadableTextStripsTagsAndScript feeds the captured HTML fixture to the EXTRACTOR ONLY (no
// network) and asserts it strips tags + script/style/comment blocks, decodes entities, collapses
// whitespace, and keeps the readable prose.
func TestExtractReadableTextStripsTagsAndScript(t *testing.T) {
	got := extractReadableText(pageFixtureHTML)
	if got == "" {
		t.Fatal("extractor returned empty on a valid prose page")
	}
	// The readable prose survives, with the <b>/<em> highlight tags stripped to text.
	if !strings.Contains(got, "First Transcontinental Railroad was completed in 1869 at Promontory Summit, Utah.") {
		t.Errorf("readable prose missing/garbled in:\n%s", got)
	}
	// HTML entities decoded: &amp; -> &, &#39; -> apostrophe.
	if !strings.Contains(got, "eastern & western United States 'coast to coast'.") {
		t.Errorf("HTML entities not decoded (&amp; / &#39;):\n%s", got)
	}
	// Script/style/comment content is DROPPED — none of it leaks into the readable text.
	for _, leak := range []string{"tracking", "this must not leak", "font-family", "a comment that must not leak", "function noise"} {
		if strings.Contains(got, leak) {
			t.Errorf("script/style/comment content leaked (%q) into:\n%s", leak, got)
		}
	}
	// No leftover HTML tags, no run-together block text ("Utah.It" would mean block text merged).
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("HTML tags leaked into the text:\n%s", got)
	}
	if strings.Contains(got, "Utah.It") {
		t.Errorf("adjacent block text ran together (no separating space):\n%s", got)
	}
}

// TestExtractReadableTextRedTeamRobustness pins the two red-team T1.4 extractor hardenings: (1) an UNCLOSED
// <script>/<style> (an opening tag with no matching close, on a malformed page) must NOT leak its code/CSS
// body as voiced prose, and (2) a literal '<'/'>' in PROSE ("5 < 10 and 10 > 5") must be PRESERVED, not
// swallowed up to the next '>'. Both fail-safe before the fix (leak noise / drop text), hardened now.
func TestExtractReadableTextRedTeamRobustness(t *testing.T) {
	// (1) unclosed <script> — the JS body must be dropped to EOL, the prose kept.
	unclosed := `<body><p>before the script</p><script>var x="MUST_NOT_LEAK"; doStuff();`
	got := extractReadableText(unclosed)
	if !strings.Contains(got, "before the script") {
		t.Errorf("prose before an unclosed <script> was lost:\n%s", got)
	}
	if strings.Contains(got, "MUST_NOT_LEAK") || strings.Contains(got, "doStuff") {
		t.Errorf("unclosed <script> body leaked into the readable text:\n%s", got)
	}
	// also a mismatched <script>…</style> must not cross-match and leak the tail.
	cross := `<p>keep</p><script>SCRIPT_LEAK</style><p>tail</p>`
	if g := extractReadableText(cross); strings.Contains(g, "SCRIPT_LEAK") {
		t.Errorf("<script>…</style> cross-match leaked code:\n%s", g)
	}

	// (2) literal angle brackets in prose are preserved (a comparison, not a tag).
	prose := `<body><p>5 < 10 and 10 > 5 is true</p></body>`
	if g := extractReadableText(prose); !strings.Contains(g, "5 < 10 and 10 > 5 is true") {
		t.Errorf("literal '<'/'>' in prose was swallowed as a tag:\n%s", g)
	}
}

// TestExtractReadableTextCapsLength asserts the extracted text is capped to pageTextCap code points — a
// page read can be larger than a search snippet but MUST be bounded (never an unbounded dump into
// grounding).
func TestExtractReadableTextCapsLength(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 0; i < 2000; i++ {
		b.WriteString("<p>word and more words here </p>")
	}
	b.WriteString("</body></html>")
	got := extractReadableText(b.String())
	if n := len([]rune(got)); n > pageTextCap {
		t.Errorf("extracted text len = %d runes, want <= cap %d", n, pageTextCap)
	}
	if got == "" {
		t.Fatal("a large prose page must still extract (capped), not be dropped")
	}
}

// TestExtractReadableTextEmptyOnMarkupOnly asserts a page that reduces to NO readable text (empty body, a
// pure-markup/JS shell) yields "" — the best-effort failure signal FetchPage turns into Result{OK:false},
// never the raw markup voiced as fact.
func TestExtractReadableTextEmptyOnMarkupOnly(t *testing.T) {
	for _, page := range []string{
		"",
		"<html><head><script>var x = 1;</script></head><body></body></html>",
		"<html><body>   \n\t   </body></html>",
		"<!-- only a comment -->",
	} {
		if got := extractReadableText(page); got != "" {
			t.Errorf("markup-only/empty page extracted to non-empty %q (want \"\")", got)
		}
	}
}

// TestPagerRejectsNonHTTPScheme asserts FetchPage NEVER dials a non-http(s) target (file://, data:, ftp://,
// a bare word) — it is a blind read (OK=false), so the fetcher can never be steered at a local-file/data
// read. No network is touched (the scheme is rejected before any GET).
func TestPagerRejectsNonHTTPScheme(t *testing.T) {
	p := NewPager()
	for _, u := range []string{
		"",
		"   ",
		"file:///etc/passwd",
		"data:text/html,<h1>x</h1>",
		"ftp://example.com/file",
		"example.com/page", // no scheme
		"javascript:alert(1)",
	} {
		if res := p.FetchPage(u); res.OK {
			t.Errorf("non-http(s) url %q returned OK=true (must be a blind read)", u)
		}
	}
}

// TestFakePagerDeterministic asserts the test double returns its fixed text regardless of url (the
// byte-stable offline stand-in a page-fetch test asserts).
func TestFakePagerDeterministic(t *testing.T) {
	f := NewFakePager()
	a := f.FetchPage("https://example.org/a")
	b := f.FetchPage("https://example.org/totally-different")
	if !a.OK || a.Text == "" {
		t.Fatal("FakePager must return a fixed OK page text")
	}
	if a.Text != b.Text || a.Source != b.Source {
		t.Errorf("FakePager not deterministic across urls: %q/%q vs %q/%q", a.Text, a.Source, b.Text, b.Source)
	}
}

// TestIsHTTPURL pins the scheme guard the tool + the fetcher both rely on.
func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"http://x.com":  true,
		"https://x.com": true,
		"HTTPS://X.COM": true,
		"  https://x  ": true,
		"ftp://x.com":   false,
		"file:///x":     false,
		"x.com":         false,
		"":              false,
	}
	for u, want := range cases {
		if got := isHTTPURL(u); got != want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", u, got, want)
		}
	}
}
