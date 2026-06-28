package web

import "testing"

// TestFakeReturnsFixedValues: the deterministic test double returns a FIXED, OK result independent of
// the query (the byte-stable stand-in for a real fetch — the web analogue of clock.Fake / host.Fake).
func TestFakeReturnsFixedValues(t *testing.T) {
	f := NewFake()
	a := f.Fetch("anything")
	b := f.Fetch("something else entirely")
	if a != b {
		t.Fatalf("Fake must be query-independent + stable: %+v != %+v", a, b)
	}
	if !a.OK || a.Text == "" {
		t.Fatalf("Fake should report a successful, non-empty read, got %+v", a)
	}
	if a.Source != "fake" {
		t.Fatalf("Fake source = %q, want \"fake\"", a.Source)
	}
}

// TestFakeAtSetValue: a Fake constructed at an explicit Result returns exactly that — so a test can pin
// the snippet it asserts on (used by the engine record/replay test to set value A).
func TestFakeAtSetValue(t *testing.T) {
	want := Result{Text: "PINNED-SNIPPET-A", OK: true, Source: "fake"}
	f := &Fake{R: want}
	if got := f.Fetch("q"); got != want {
		t.Fatalf("Fake at fixed value = %+v, want %+v", got, want)
	}
}

// TestCollapseCapsAndCollapses: the snippet renderer collapses whitespace runs to single spaces, trims,
// and caps at snippetCap code points — so a raw body can never dump an unbounded multi-line page into
// cognition (a distal sense is a one-line cue).
func TestCollapseCapsAndCollapses(t *testing.T) {
	if got := collapse("  hello \n\t world  \r\n  again "); got != "hello world again" {
		t.Fatalf("collapse whitespace = %q, want \"hello world again\"", got)
	}
	long := make([]rune, snippetCap+50)
	for i := range long {
		long[i] = 'x'
	}
	if got := collapse(string(long)); len([]rune(got)) != snippetCap {
		t.Fatalf("collapse cap = %d runes, want %d", len([]rune(got)), snippetCap)
	}
}

// TestHostOfExtractsHost: the provenance label is the URL host (best-effort cosmetics on the percept).
func TestHostOfExtractsHost(t *testing.T) {
	cases := map[string]string{
		"https://wttr.in/?format=3&": "wttr.in",
		"http://example.com/path":    "example.com",
		"news.example.org":           "news.example.org",
		"":                           "web",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Fatalf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWallEmptyEndpointDefaults: a zero-value Wall (no endpoint, no client) falls back to the defaults
// without panicking on the build path — a malformed request build returns a blind (OK=false) result,
// never a crash. This exercises the best-effort contract offline (no real network call is asserted).
func TestWallBestEffortNoCrash(t *testing.T) {
	// An obviously-unreachable endpoint: the transport fails fast (no network in the test env), and the
	// contract is (Text:"", OK:false) — a blind read, never a panic. Uses a tiny client timeout so the
	// test never hangs even if some resolver is present.
	w := Wall{Endpoint: "http://127.0.0.1:1/", Client: nil}
	got := w.Fetch("q")
	if got.OK || got.Text != "" {
		t.Fatalf("unreachable endpoint: want a blind (\"\", false) read, got %+v", got)
	}
}
