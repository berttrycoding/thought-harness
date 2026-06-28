// Package web is the INJECTED outward-perception seam for the reach=world fetch_web sensor (cognitive
// power-cycle, power-cycle follow-up #15 — the OUTWARD half of grounded sensing, complementing the
// shipped INWARD sensors read_clock / read_host / read_event_log): the engine is deliberately
// headless-pure (CLAUDE.md: no I/O, no net/http, no unseeded reads in engine logic; determinism + the
// durability math forbid a real network read in engine logic), so a real-world web/news read enters the
// same way time and the host footprint do — through an injected interface with a deterministic test
// double, exactly like internal/clock.Clock and internal/host.Host. A nil Web anywhere means WEB-BLIND:
// no network read is ever performed and behavior is byte-identical to the web-blind engine (the default).
//
// DISTAL SENSE, BEST-EFFORT. A web fetch reaches OUTSIDE the harness (the network), so it is the most
// non-deterministic + most failure-prone sensor. The contract is therefore best-effort: any error
// (no network, timeout, non-2xx, an oversized body) yields OK=false and an empty snippet, never a
// crash and never a partial/garbage read voiced as fact. The engine records the SNIPPET to the
// percept-log so a live read is replayable (record once / replay thereafter), exactly like read_clock —
// that is what keeps a non-deterministic distal sense from breaking the seeded-RNG / golden contract.
//
// STDLIB-ONLY. The real impl (Wall) uses only net/http with a short timeout; no third-party deps. The
// Fake returns FIXED values offline (the byte-stable stand-in for the suite + UAT).
package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Result is one outward-perception read: a small, frozen web/news snippet. It carries no live handles
// (the body is read + copied at the fetch), so it is a frozen snapshot the orientation/percept path can
// fold in safely. Text is a one-line snippet (whitespace-collapsed, length-capped). OK reports whether
// the read succeeded — a failed/blind read is (Text:"", OK:false), never a partial fact. Source names
// where it came from (the endpoint host, or "fake" for the double) so the percept is auditable.
type Result struct {
	Text   string // a one-line news/web snippet (collapsed whitespace, capped) — "" on any failure
	OK     bool   // true only on a real successful read (a clean 2xx with a non-empty body)
	Source string // provenance: the endpoint host (Wall) or "fake" (the double); "" when blind
}

// snippetCap bounds the snippet length (code points) so an outward read can never dump an unbounded
// page into the conscious stream — a distal sense is a one-line "current events" cue, not a document.
const snippetCap = 240

// readBodyCap bounds how many bytes Wall reads off the wire before giving up — a hard ceiling so a
// hostile/huge response cannot exhaust memory (best-effort: an over-cap body is treated as a failure).
const readBodyCap = 64 * 1024

// Web is the one outward-perception port. Production wires Wall (a real HTTP GET); tests wire a Fake
// with fixed values. The interface IS the injected seam — the engine never calls net/http directly.
type Web interface {
	Fetch(query string) Result
}

// Wall does a real, best-effort HTTP GET. Construct it ONLY at the edge (CLI/TUI wiring) — never inside
// engine logic — so the engine's web-blindness stays the default and greppable. The only net/http in the
// sensor path lives here. It is best-effort by contract: any error path returns (Text:"", OK:false), so
// a network outage / timeout / bad status never crashes the engine and never voices a partial read.
type Wall struct {
	// Endpoint is the base URL the query is appended to (a simple GET of "<Endpoint><query>"). When empty
	// it defaults to DefaultEndpoint. Configurable so a deployment can point at a news-ish text endpoint.
	Endpoint string
	// Client is the HTTP client; when nil a short-timeout client is used (DefaultTimeout). A bounded
	// timeout is mandatory — a distal sense must never block the cognitive loop on a slow endpoint.
	Client *http.Client
}

// DefaultEndpoint is a stdlib-only, news-ish text endpoint the Wall GETs the query against by default.
// It is a plain-text service (wttr.in returns a one-line text report), so the snippet reads as a
// human-legible "current events"-style cue with no HTML/JSON parsing. Override Wall.Endpoint to point
// elsewhere. The query is appended verbatim (the caller passes a short, URL-safe-ish cue).
const DefaultEndpoint = "https://wttr.in/?format=3&"

// DefaultTimeout is the short, hard wall-clock budget for a Wall fetch — a distal sense is bounded so it
// can never stall the loop. Best-effort: exceeding it yields OK=false.
const DefaultTimeout = 4 * time.Second

// NewWall builds a Wall at the default endpoint + a short-timeout client. Construct at the edge only.
func NewWall() *Wall {
	return &Wall{Endpoint: DefaultEndpoint, Client: &http.Client{Timeout: DefaultTimeout}}
}

// Fetch does the real GET. It is best-effort: on ANY error (bad request build, transport failure,
// timeout, non-2xx status, empty body, oversized body) it returns (Text:"", OK:false) — never a panic,
// never a partial read voiced as fact. On success it returns the collapsed, capped one-line snippet with
// OK=true and the endpoint host as the source. The query is appended to the endpoint verbatim.
func (w Wall) Fetch(query string) Result {
	endpoint := w.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+query, nil)
	if err != nil {
		return Result{}
	}
	// A polite, plain-text User-Agent so a text endpoint returns its text form (some services sniff it).
	req.Header.Set("User-Agent", "thought-harness/fetch_web (text)")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}
	}
	// Read at most readBodyCap+1 bytes: if we hit the +1 the body is over the cap -> treat as a failure
	// (best-effort: never fold an unbounded read into cognition).
	body, err := io.ReadAll(io.LimitReader(resp.Body, readBodyCap+1))
	if err != nil || len(body) == 0 || len(body) > readBodyCap {
		return Result{}
	}
	text := collapse(string(body))
	if text == "" {
		return Result{}
	}
	return Result{Text: text, OK: true, Source: hostOf(endpoint)}
}

// Fake is the deterministic test double: it returns a FIXED snippet, so an outward-sensing test is
// exactly reproducible (the web analogue of clock.Fake / host.Fake and the seeded RNG). A real network
// read would vary run-to-run and break golden determinism — the Fake is the offline, byte-stable
// stand-in. The fixed result is independent of the query (the percept-log captures the SNIPPET, so the
// stable value is what the replay test asserts).
type Fake struct {
	R Result
}

// NewFake builds a Fake at a fixed, arbitrary snippet (determinism needs stability, not realism).
func NewFake() *Fake {
	return &Fake{R: Result{Text: "Calm and clear across the wire; nothing urgent in the feed.", OK: true, Source: "fake"}}
}

// Fetch returns the fake's fixed result (ignoring the query — the value is what makes a test stable).
func (f *Fake) Fetch(string) Result { return f.R }

// collapse renders a raw body into a one-line snippet: collapse all runs of whitespace to single spaces,
// trim, then cap to snippetCap code points. Deterministic + allocation-light; no locale/RNG/clock.
func collapse(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > snippetCap {
		r = r[:snippetCap]
	}
	return strings.TrimSpace(string(r))
}

// hostOf extracts a coarse provenance label (the host) from a URL for the Source field, without pulling
// net/url into the hot path's allocations — it is best-effort cosmetics on the percept, not security.
func hostOf(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "web"
	}
	return s
}
