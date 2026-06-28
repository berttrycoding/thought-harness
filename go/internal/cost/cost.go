// cost.go (PART 3) is the cost COMPUTATION over a run's captured event trace: read
// the token counts the llm.call events carry (PART 1 wired prompt/completion/total
// + cache hit/miss + reasoning onto each llm.call's event data) and aggregate them
// per RUN, per ROLE, and per MODEL, then price them against a RateCard.
//
// The token accounting per llm.call event (event-data keys set in internal/llm/
// openai.go):
//
//	role               the call site (e.g. "conscious.generate", "Controller.decide")
//	model              the model id the call hit
//	prompt_tokens      usage.prompt_tokens         (total input, -1 if absent)
//	completion_tokens  usage.completion_tokens     (total output, -1 if absent)
//	cached_input_tokens  the cache-HIT portion of the input (DeepSeek hit / OpenAI cached)
//	cache_miss_tokens    the cache-MISS portion of the input (DeepSeek only; -1 elsewhere)
//	reasoning_tokens     completion_tokens_details.reasoning_tokens (subset of output)
//
// The input SPLIT (uncached vs cached) is derived robustly: cached-in is the
// reported cache-hit count (clamped to >=0); uncached-in is the explicit
// cache_miss_tokens when the provider reports it (DeepSeek), else prompt_tokens
// minus cached-in (OpenAI, which reports only the cached portion). The OUT count is
// completion_tokens; reasoning_tokens is a BREAKOUT of out (already inside it),
// surfaced separately but never double-priced.
//
// cost = uncached_in*in_uncached + cached_in*in_cached + out*out  (per the rate
// card, USD/Mtok / 1e6). An UNKNOWN model leaves USD unset (HasUSD=false) so the
// report says "rates unknown" rather than billing $0.
package cost

import "sort"

// Tokens is the per-cell (run/role/model) token accounting summed off the llm.call
// events. The input is split into uncached (cache-miss, full price) and cached
// (cache-hit, discounted); Out is the completion total; Reasoning is the part of
// Out the model spent thinking (a breakout, already counted inside Out).
type Tokens struct {
	// UncachedIn is the input cache-MISS token count (billed at Rate.InUncached).
	UncachedIn int
	// CachedIn is the input cache-HIT token count (billed at Rate.InCached).
	CachedIn int
	// Out is the output/completion token count (billed at Rate.Out). Includes
	// Reasoning — reasoning tokens ARE completion tokens.
	Out int
	// Reasoning is the reasoning-trace token count: a BREAKOUT of Out (already
	// inside it), surfaced so the report can show how much output was thinking.
	Reasoning int
	// Calls is the number of llm.call events folded into this cell.
	Calls int
}

// TotalIn is the full input token count (uncached + cached).
func (t Tokens) TotalIn() int { return t.UncachedIn + t.CachedIn }

// Total is the full token count (all input + all output).
func (t Tokens) Total() int { return t.TotalIn() + t.Out }

// CacheHitFraction is cached-in / total-in (0 when there were no input tokens). The
// report renders it as the cache-hit %.
func (t Tokens) CacheHitFraction() float64 {
	in := t.TotalIn()
	if in == 0 {
		return 0
	}
	return float64(t.CachedIn) / float64(in)
}

// InputShare is input / total tokens — the input side of the I/O split (0 when no
// tokens). OutputShare is 1 - InputShare.
func (t Tokens) InputShare() float64 {
	total := t.Total()
	if total == 0 {
		return 0
	}
	return float64(t.TotalIn()) / float64(total)
}

// OutputShare is out / total tokens — the output side of the I/O split.
func (t Tokens) OutputShare() float64 {
	total := t.Total()
	if total == 0 {
		return 0
	}
	return float64(t.Out) / float64(total)
}

// ReasoningShare is reasoning / out tokens — how much of the output was thinking (0
// when there was no output).
func (t Tokens) ReasoningShare() float64 {
	if t.Out == 0 {
		return 0
	}
	return float64(t.Reasoning) / float64(t.Out)
}

// add folds another cell's tokens into this one (used to aggregate per role / per
// model / the run total).
func (t *Tokens) add(o Tokens) {
	t.UncachedIn += o.UncachedIn
	t.CachedIn += o.CachedIn
	t.Out += o.Out
	t.Reasoning += o.Reasoning
	t.Calls += o.Calls
}

// LLMCall is the minimal projection of one llm.call event the tally reads: the
// role, the model, and the raw usage counts (a -1 means the provider omitted the
// field, distinguished from a true 0). The bench runner builds these off the
// captured event trace; tests construct them directly.
type LLMCall struct {
	Role  string
	Model string
	// Tick is the bus tick the llm.call event was emitted at (events.Event.Tick).
	// It is the per-TICK rollup key: PerTickSpend buckets a run's calls by Tick to
	// report calls/tokens-per-tick. It is NEVER a pricing input (cost is per-token,
	// independent of when the call fired); 0 on the offline double / a test that
	// leaves it unset, which is harmless to the per-role / per-model aggregation.
	Tick int
	// PromptTokens / CompletionTokens are usage.prompt_tokens / completion_tokens
	// (total input / total output). -1 = absent.
	PromptTokens     int
	CompletionTokens int
	// CachedInputTokens is the cache-HIT input portion (DeepSeek hit / OpenAI
	// cached). -1 = absent (treated as 0 cached).
	CachedInputTokens int
	// CacheMissTokens is the explicit cache-MISS input portion (DeepSeek only). -1 =
	// not reported, in which case uncached-in is derived as prompt - cached.
	CacheMissTokens int
	// ReasoningTokens is the reasoning-trace token count (subset of completion). -1 =
	// absent (treated as 0 reasoning).
	ReasoningTokens int
}

// tokens projects one LLMCall into its Tokens contribution, deriving the
// uncached/cached input split robustly across provider shapes:
//   - cached-in  = max(CachedInputTokens, 0)            (a -1/absent => 0 cached)
//   - uncached-in = CacheMissTokens if reported (>=0),  (DeepSeek's explicit miss)
//     else max(PromptTokens - cached-in, 0)             (OpenAI: prompt minus cached)
//
// Out = max(CompletionTokens, 0); Reasoning = max(ReasoningTokens, 0). A call with
// no usage at all (every field -1) still counts as 1 Call with 0 tokens — the
// model was called but the server reported no usage.
func (c LLMCall) tokens() Tokens {
	cached := c.CachedInputTokens
	if cached < 0 {
		cached = 0
	}
	var uncached int
	switch {
	case c.CacheMissTokens >= 0:
		// Provider reported an explicit miss count (DeepSeek) — trust it directly.
		uncached = c.CacheMissTokens
	case c.PromptTokens >= 0:
		// Only a total + cached portion (OpenAI) — the rest of the prompt is uncached.
		uncached = c.PromptTokens - cached
		if uncached < 0 {
			uncached = 0
		}
	default:
		uncached = 0
	}
	out := c.CompletionTokens
	if out < 0 {
		out = 0
	}
	reasoning := c.ReasoningTokens
	if reasoning < 0 {
		reasoning = 0
	}
	return Tokens{UncachedIn: uncached, CachedIn: cached, Out: out, Reasoning: reasoning, Calls: 1}
}

// Breakdown is the full per-run cost aggregation: the run total, the per-ROLE
// split, and the per-MODEL split, each with its priced USD (when the model is in
// the rate card). It is what the report renders.
type Breakdown struct {
	// Total is the run-wide token + USD total across every model.
	Total Priced
	// ByRole maps a call role -> its token + USD subtotal (summed across models).
	ByRole map[string]Priced
	// ByModel maps a model id -> its token + USD subtotal (summed across roles).
	ByModel map[string]Priced
	// Card is the rate card the USD figures were priced against (its Source is shown
	// in the report so the reader knows which card row applied).
	Card *RateCard
}

// Priced bundles a Tokens accounting with its USD cost and whether that USD is
// KNOWN. HasUSD=false is the "rates unknown" marker — the model was used but is not
// in the rate card, so USD is meaningless (the report says so rather than show $0).
// For a multi-model aggregate (the run total / a per-role row spanning models),
// HasUSD is true only if EVERY model folded in was priced; UnknownModels lists any
// that were not, so the report can flag a partially-unpriced total.
type Priced struct {
	// Tokens is the token accounting for this cell (run / role / model).
	Tokens Tokens
	// USD is the priced cost. Meaningful only when HasUSD is true.
	USD float64
	// HasUSD is false when at least one model in this cell is not in the rate card
	// (the "rates unknown" marker). The report must NOT print USD as $0 then.
	HasUSD bool
	// UnknownModels lists the model ids in this cell that the rate card did not
	// price (empty when HasUSD is true). Sorted, de-duplicated.
	UnknownModels []string
}

// Compute aggregates the calls per role and per model, prices each against the
// card, and rolls up the run total. It is the single entry point PART 3 exposes to
// the bench runner: feed it the run's llm.call projections + a rate card, get back
// the per-run / per-role / per-model breakdown the report renders.
func Compute(calls []LLMCall, card *RateCard) Breakdown {
	if card == nil {
		card = Default()
	}
	bd := Breakdown{
		ByRole:  map[string]Priced{},
		ByModel: map[string]Priced{},
		Card:    card,
	}

	// First pass: sum tokens per role and per model (per-model is where pricing
	// happens, since the rate is per model).
	roleTokens := map[string]Tokens{}
	modelTokens := map[string]Tokens{}
	for _, c := range calls {
		t := c.tokens()
		rt := roleTokens[c.Role]
		rt.add(t)
		roleTokens[c.Role] = rt
		mt := modelTokens[c.Model]
		mt.add(t)
		modelTokens[c.Model] = mt
	}

	// Per-model: price directly off the model's own rate-card row.
	for model, t := range modelTokens {
		rate, known := card.Lookup(model)
		p := Priced{Tokens: t}
		if known {
			p.USD = rate.Price(t.UncachedIn, t.CachedIn, t.Out)
			p.HasUSD = true
		} else {
			p.UnknownModels = []string{model}
		}
		bd.ByModel[model] = p
		bd.Total = mergePriced(bd.Total, p)
	}

	// Per-role: a role may span models, so re-price each role by walking its calls'
	// models (a role's USD is the sum of its calls priced at each call's model rate;
	// HasUSD only if every model the role used is priced).
	rolePerModel := map[string]map[string]Tokens{}
	for _, c := range calls {
		if rolePerModel[c.Role] == nil {
			rolePerModel[c.Role] = map[string]Tokens{}
		}
		mt := rolePerModel[c.Role][c.Model]
		mt.add(c.tokens())
		rolePerModel[c.Role][c.Model] = mt
	}
	for role := range roleTokens {
		var rp Priced
		first := true
		for model, t := range rolePerModel[role] {
			rate, known := card.Lookup(model)
			seg := Priced{Tokens: t}
			if known {
				seg.USD = rate.Price(t.UncachedIn, t.CachedIn, t.Out)
				seg.HasUSD = true
			} else {
				seg.UnknownModels = []string{model}
			}
			if first {
				rp = seg
				first = false
			} else {
				rp = mergePriced(rp, seg)
			}
		}
		bd.ByRole[role] = rp
	}

	return bd
}

// mergePriced folds segment b into a: tokens add, USD adds, HasUSD is the AND (a
// total is only fully priced if every part was), and UnknownModels unions. The
// zero Priced is the identity (merging into an empty cell yields b).
func mergePriced(a, b Priced) Priced {
	out := a
	out.Tokens.add(b.Tokens)
	out.USD += b.USD
	// HasUSD: a fresh (zero) accumulator with no calls is the identity — adopt b's
	// flag; otherwise both sides must be priced for the merged USD to be meaningful.
	if a.Tokens.Calls == 0 && len(a.UnknownModels) == 0 {
		out.HasUSD = b.HasUSD
	} else {
		out.HasUSD = a.HasUSD && b.HasUSD
	}
	out.UnknownModels = unionSorted(a.UnknownModels, b.UnknownModels)
	return out
}

// unionSorted returns the sorted, de-duplicated union of two model-id lists.
func unionSorted(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		seen[s] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// SortedRoles returns the role keys of the breakdown, sorted (for a deterministic
// per-role report block).
func (bd Breakdown) SortedRoles() []string {
	out := make([]string, 0, len(bd.ByRole))
	for r := range bd.ByRole {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// SortedModels returns the model keys of the breakdown, sorted.
func (bd Breakdown) SortedModels() []string {
	out := make([]string, 0, len(bd.ByModel))
	for m := range bd.ByModel {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// PER-TICK spend (the WF-E baseline HEADLINE: LLM calls + tokens per engine tick).
//
// The per-CALL accounting above answers "what did the campaign cost in total / per
// role / per model". The per-TICK rollup answers the headline WF-E cuts: how much
// model spend does ONE engine tick incur — mean/median LLM calls per tick + tokens
// per tick. It is read-only over the SAME llm.call usage records; it adds no cost
// and changes no behavior.
//
// The unit is the (run, tick) cell. A bench campaign concatenates MANY independent
// engine runs (each a fresh engine whose tick counter restarts at 0), so a raw tick
// number is NOT unique across the campaign — tick 5 of run A and tick 5 of run B are
// different cells. PerTickSpend therefore takes ONE call-list per run and buckets
// each run's calls by their own Tick, so run boundaries are respected. The mean and
// median are taken over every POPULATED (run, tick) cell — i.e. only ticks on which
// at least one model call actually fired (a silent tick spends nothing and would
// only dilute the headline the meter exists to surface).
// ---------------------------------------------------------------------------

// PerTick is the per-engine-tick spend rollup: the mean and median of the LLM-call
// count and the token total taken over every populated (run, tick) cell. Ticks is
// the number of those populated cells the statistics were taken over (0 ⇒ no model
// calls on any tick, and every figure is 0).
type PerTick struct {
	// Ticks is the count of populated (run, tick) cells — ticks on which >=1 model
	// call fired. The mean/median below are taken over exactly these cells.
	Ticks int
	// MeanCalls / MedianCalls are the LLM-call count per populated tick.
	MeanCalls   float64
	MedianCalls float64
	// MeanTokens / MedianTokens are the total token count (input + output) per
	// populated tick.
	MeanTokens   float64
	MedianTokens float64
}

// PerTickSpend rolls the per-call usage up to the per-TICK headline. runs is one
// call-list per engine run (each run's calls carry that run's own restarting Tick);
// it buckets each run's calls by Tick, sums calls + tokens per (run, tick) cell, and
// reports the mean/median over every populated cell. An empty input (or one with no
// calls) yields a zero PerTick. It prices nothing and mutates nothing.
func PerTickSpend(runs [][]LLMCall) PerTick {
	var callsPerTick, tokensPerTick []float64
	for _, run := range runs {
		// Bucket THIS run's calls by tick (a fresh map per run so tick numbers never
		// collide across runs — the (run, tick) cell is the unit).
		callsByTick := map[int]int{}
		tokensByTick := map[int]int{}
		for _, c := range run {
			callsByTick[c.Tick]++
			tokensByTick[c.Tick] += c.tokens().Total()
		}
		for tick, n := range callsByTick {
			callsPerTick = append(callsPerTick, float64(n))
			tokensPerTick = append(tokensPerTick, float64(tokensByTick[tick]))
		}
	}
	return PerTick{
		Ticks:        len(callsPerTick),
		MeanCalls:    meanF(callsPerTick),
		MedianCalls:  medianF(callsPerTick),
		MeanTokens:   meanF(tokensPerTick),
		MedianTokens: medianF(tokensPerTick),
	}
}

// meanF is the arithmetic mean of xs (0 for an empty slice).
func meanF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// medianF is the median of xs (0 for empty; the average of the two middle values
// for an even count). It sorts a COPY so the caller's slice is left untouched.
func medianF(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	cp := make([]float64, n)
	copy(cp, xs)
	sort.Float64s(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// ProjectUSD prices the run's TOTAL tokens against ONE chosen model's rate
// (regardless of which model actually ran) — the local-vs-API delta: a local model
// (gemma) has no rate, but its token counts projected under, say, deepseek-reasoner
// show what the same run WOULD cost on the API. Returns (usd, ok): ok=false when
// rateModel is not in the card.
func (bd Breakdown) ProjectUSD(rateModel string) (float64, bool) {
	rate, ok := bd.Card.Lookup(rateModel)
	if !ok {
		return 0, false
	}
	t := bd.Total.Tokens
	return rate.Price(t.UncachedIn, t.CachedIn, t.Out), true
}
