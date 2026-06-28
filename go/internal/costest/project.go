package costest

import "github.com/berttrycoding/thought-harness/internal/cost"

// Projection is the full NO-PAID-RUN estimate for a workload, priced against ONE chosen
// rate-card model: the token totals, the prefix-reuse cache split, and the projected USD. It
// is what `costest --log … --rate-model …` prints.
type Projection struct {
	// Tokens is the in/out token estimate (real where the server reported it, chars/token
	// where it didn't) and the model used.
	Tokens TokenEstimate
	// Cache is the prefix-reuse cache-hit split (estimated hit/miss input tokens, the hit %).
	Cache CacheEstimate
	// RateModel is the rate-card row the USD was priced against (e.g. "deepseek-reasoner").
	RateModel string
	// USD is the projected cost of the workload at RateModel's rates: cache-MISS input at the
	// uncached rate, cache-HIT input at the (heavily discounted) cached rate, output at the
	// out rate. Meaningful only when HasUSD is true.
	USD float64
	// HasUSD is false when RateModel is not in the rate card (the cost layer's "rates unknown"
	// contract — the tool says so rather than print $0). USDNoCacheFloor is the SAME workload
	// priced as if NOTHING cached (all input at the uncached rate) — the upper bound, so the
	// report can show what prefix-reuse SAVED.
	HasUSD          bool
	USDNoCacheFloor float64
}

// Savings is the fraction of input-side spend the estimated prefix cache saves versus the
// no-cache upper bound: (USDNoCache - USD) / USDNoCache (0 when the no-cache cost is 0 or
// rates are unknown). The report renders it as "prefix reuse saves ~X%".
func (p Projection) Savings() float64 {
	if !p.HasUSD || p.USDNoCacheFloor <= 0 {
		return 0
	}
	s := (p.USDNoCacheFloor - p.USD) / p.USDNoCacheFloor
	if s < 0 {
		return 0
	}
	return s
}

// Project runs the full estimate over a log's calls and prices it against rateModel in card.
// It is the single entry point the CLI calls: estimate tokens (calibrated), estimate the
// prefix-reuse cache split (block-accurate), then price MISS input + HIT input + output at
// the chosen model's rates. card==nil ⇒ cost.Default() (the embedded DeepSeek card). block<=0
// ⇒ the DeepSeek default (64-token blocks).
func Project(calls []Call, card *cost.RateCard, rateModel string, block int) Projection {
	if card == nil {
		card = cost.Default()
	}
	tokens := EstimateTokens(calls)
	cache := EstimateCacheHits(calls, tokens.Model, block)

	p := Projection{Tokens: tokens, Cache: cache, RateModel: rateModel}

	rate, ok := card.Lookup(rateModel)
	if !ok {
		// Unknown model → "rates unknown" (never a silent $0); the token + cache estimates are
		// still meaningful and are returned for display.
		return p
	}
	p.HasUSD = true
	// Priced split: cache-MISS input at the full (uncached) rate, cache-HIT input at the
	// discounted (cached) rate, output at the out rate. cost.Rate.Price takes
	// (uncachedIn, cachedIn, out) — exactly our (MissTokens, HitTokens, OutTokens).
	p.USD = rate.Price(cache.MissTokens, cache.HitTokens, tokens.OutTokens)
	// Upper bound: the SAME workload with NO cache (all input billed at the uncached rate) —
	// what the run would cost cold, so the report can show the prefix-reuse saving.
	p.USDNoCacheFloor = rate.Price(tokens.InTokens, 0, tokens.OutTokens)
	return p
}
