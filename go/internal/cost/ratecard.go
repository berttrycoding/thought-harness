// Package cost is the bench's money layer: a RATE CARD (USD per 1e6 tokens, per
// model id) plus the COST COMPUTATION that turns a run's measured token counts
// into dollars, aggregated per RUN, per ROLE, and per MODEL.
//
// The two halves:
//
//   - ratecard.go (PART 2) — the RateCard: a model-id -> Rate map loaded from a
//     JSON file (config/rates.json, embedded as the default seed). Lookup is
//     EXPLICIT: an unknown model id returns ok=false (a "rates unknown" marker),
//     NEVER a silent zero — the report must SAY rates are unknown rather than
//     print $0.00 and imply free.
//   - cost.go (PART 3) — Tally/Compute: read token counts (uncached-in, cached-in,
//     out, reasoning) off the captured event trace and price them against the card.
//
// Rate units: every Rate field is USD per 1,000,000 tokens (per-Mtok, the unit the
// providers publish). Cost divides by tokensPerUnit (1e6). Seeded with the current
// published DeepSeek per-Mtok numbers (see config/rates.json _sources): deepseek-
// chat / deepseek-reasoner / deepseek-v4-flash share $0.14 miss / $0.0028 hit /
// $0.28 out; deepseek-v4-pro is $1.74 / $0.0145 / $3.48. OpenAI/local rows are
// illustrative.
//
// STDLIB-ONLY: encoding/json + embed. No third-party deps (CLAUDE.md: the core
// stays dependency-free).
package cost

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// tokensPerUnit is the denominator the per-Mtok rate is expressed against: a Rate
// of 0.14 means $0.14 per 1,000,000 tokens, so the per-token price is 0.14/1e6.
const tokensPerUnit = 1_000_000.0

// Rate is one model's price card: USD per 1e6 tokens for each token class. The
// input split mirrors the providers' prompt-cache pricing — InUncached is the
// cache-MISS price (the full input rate), InCached is the cache-HIT price (the
// discounted rate, typically ~1/10 of the miss). Out is the completion rate.
// Reasoning tokens are billed at the Out rate (they are completion tokens the
// model emitted), so there is no separate reasoning rate field — they are surfaced
// as a breakout in the report, priced at Out.
type Rate struct {
	// InUncached is the input cache-MISS price (USD / 1e6 tokens) — the full,
	// undiscounted prompt-token rate.
	InUncached float64 `json:"in_uncached"`
	// InCached is the input cache-HIT (prompt-cache) price (USD / 1e6 tokens) — the
	// discounted rate for prompt tokens served from a cached prefix.
	InCached float64 `json:"in_cached"`
	// Out is the output/completion price (USD / 1e6 tokens). Reasoning tokens are
	// completion tokens and are billed at this rate.
	Out float64 `json:"out"`
}

// RateCard maps a model id to its Rate. It is loaded from JSON (config/rates.json
// is the embedded seed). Lookup via Lookup is EXPLICIT — an unknown id returns
// ok=false so the report can say "rates unknown" rather than silently bill $0.
type RateCard struct {
	// Source labels where the card was loaded from ("embedded:config/rates.json" or
	// the on-disk path) — surfaced in the report so the reader knows which card row
	// priced the run.
	Source string
	// rates is the model-id -> Rate index. Private so callers go through Lookup
	// (which is the only place the unknown-id contract is enforced).
	rates map[string]Rate
}

// rateCardFile is the on-disk JSON schema of config/rates.json: a rates object
// plus free-form _comment / _sources fields (ignored on decode). The schema is
// {"rates": {"<model-id>": {"in_uncached":N,"in_cached":N,"out":N}, ...}}.
type rateCardFile struct {
	Rates map[string]Rate `json:"rates"`
}

// embeddedRates is the seed rate card compiled into the binary so cost pricing is
// self-contained (no working-directory dependency for the default card). LoadFile
// overrides it from an explicit --rates PATH when given.
//
//go:embed default_rates.json
var embeddedRates []byte

// Default returns the embedded seed RateCard (config/rates.json, baked in at build
// time). It never errors — the embedded JSON is validated by the package test.
func Default() *RateCard {
	c, err := parse(embeddedRates, "embedded:config/rates.json")
	if err != nil {
		// The embedded card is a compile-time constant validated by the package
		// test; a parse failure here is a build-integrity bug, not a runtime input
		// error. Return an empty card so every model reports "rates unknown" rather
		// than panicking a bench run.
		return &RateCard{Source: "embedded(PARSE-FAILED): " + err.Error(), rates: map[string]Rate{}}
	}
	return c
}

// LoadFile reads a RateCard from a JSON file at path (the schema of
// config/rates.json). An empty path returns Default() (the embedded seed). A
// missing/!-parseable file is a real error the caller surfaces — the rate card is
// load-bearing, so a typo'd --rates path must fail loudly, not silently fall back.
func LoadFile(path string) (*RateCard, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cost: read rate card %q: %w", path, err)
	}
	return parse(data, path)
}

// parse decodes a rate-card JSON blob into a RateCard tagged with source.
func parse(data []byte, source string) (*RateCard, error) {
	var f rateCardFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("cost: parse rate card (%s): %w", source, err)
	}
	if f.Rates == nil {
		f.Rates = map[string]Rate{}
	}
	return &RateCard{Source: source, rates: f.Rates}, nil
}

// Lookup returns the Rate for a model id and whether it is KNOWN. ok=false is the
// explicit "rates unknown" marker — the caller MUST NOT treat the zero Rate as a
// real $0 price; the report renders "rates unknown (model not in rate card)" for
// it. This is the package's central contract: an unpriced model is visible, never
// a silent zero (a $0.00 total would falsely imply a free run).
func (c *RateCard) Lookup(model string) (Rate, bool) {
	r, ok := c.rates[model]
	return r, ok
}

// Models returns the model ids the card prices, sorted (for a deterministic
// "available rate-card rows" listing in the report / diagnostics).
func (c *RateCard) Models() []string {
	out := make([]string, 0, len(c.rates))
	for m := range c.rates {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Price returns the USD cost of (uncachedIn, cachedIn, out) tokens at this Rate.
// Reasoning tokens are part of the out count (they are completion tokens) and are
// already included in out by the caller — Price does not double-count them. Each
// class is (tokens * perMtokRate / 1e6).
func (r Rate) Price(uncachedIn, cachedIn, out int) float64 {
	return float64(uncachedIn)*r.InUncached/tokensPerUnit +
		float64(cachedIn)*r.InCached/tokensPerUnit +
		float64(out)*r.Out/tokensPerUnit
}
