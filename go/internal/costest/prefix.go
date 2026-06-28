package costest

import "strings"

// ---------------------------------------------------------------------------
// PREFIX-REUSE cache-hit estimator.
//
// DeepSeek (and OpenAI prompt-cache) serve a request's leading tokens from cache when those
// tokens EXACTLY match the prefix of an EARLIER request — the cache is keyed on the prompt
// PREFIX, at a block granularity (DeepSeek counts hits in 64-token blocks of the matching
// prefix; the unmatched tail is a cache MISS, billed full price). So the cache-hit fraction
// of a workload is, per call, "how many leading tokens does this prompt share with something
// already seen", summed and divided by total input tokens.
//
// We estimate that WITHOUT a tokenizer by:
//   - tokenizing each prompt into WORDS (whitespace split) — a deterministic, stable proxy
//     for token boundaries (BPE keeps word boundaries; a shared word-prefix is a shared
//     token-prefix to within the chars/token ratio);
//   - inserting every prompt's word sequence into a TRIE of prefixes seen so far;
//   - for each call, walking the trie to find the longest word-prefix already present from a
//     PRIOR call (the running set), then ROUNDING that match DOWN to the provider's block
//     size (a partial trailing block is not a cache hit) — that many leading words are HITS;
//   - converting shared WORDS → shared TOKENS via the call's own chars/word→token scaling so
//     the hit count lives in the same token space as the input total, and dividing.
//
// The first time a prefix is seen it is all MISS (nothing to match yet); the second identical
// (or prefix-sharing) request is where the hit lands — exactly the provider's behaviour.
// ---------------------------------------------------------------------------

// defaultCacheBlockTokens is DeepSeek's prompt-cache block granularity: a hit is counted in
// whole 64-token blocks of the matching prefix; a partial trailing block does not hit. We
// round each call's matched-prefix length DOWN to this many tokens so the estimate matches
// the provider's block accounting (and never over-counts a near-miss as a full hit).
const defaultCacheBlockTokens = 64

// prefixNode is one node of the prompt-prefix trie: its children keyed by the next word, and
// whether any inserted prompt's prefix ENDS at this node is irrelevant (we only need
// reachability — a word path exists ⇒ some prior prompt had that leading word sequence).
type prefixNode struct {
	children map[string]*prefixNode
}

func newPrefixNode() *prefixNode { return &prefixNode{children: map[string]*prefixNode{}} }

// prefixTrie accumulates the word-prefixes of every prompt inserted so far. matchLen returns
// the longest leading run of words already present; insert adds a prompt's full word path.
type prefixTrie struct{ root *prefixNode }

func newPrefixTrie() *prefixTrie { return &prefixTrie{root: newPrefixNode()} }

// matchLen returns how many LEADING words of `words` already exist as a path from the root
// (i.e. the longest prefix this prompt shares with any previously inserted prompt). 0 ⇒ not
// even the first word was seen before (a cold prompt).
func (t *prefixTrie) matchLen(words []string) int {
	node := t.root
	n := 0
	for _, w := range words {
		next, ok := node.children[w]
		if !ok {
			break
		}
		node = next
		n++
	}
	return n
}

// insert adds the full word path of `words` into the trie (so a LATER call can match against
// it). Idempotent for an already-present prefix.
func (t *prefixTrie) insert(words []string) {
	node := t.root
	for _, w := range words {
		next, ok := node.children[w]
		if !ok {
			next = newPrefixNode()
			node.children[w] = next
		}
		node = next
	}
}

// tokenizeWords splits a prompt into whitespace-delimited words — the deterministic proxy for
// token boundaries the prefix trie keys on. strings.Fields collapses all runs of whitespace,
// so two prompts that differ only in trailing whitespace still share their full word prefix.
func tokenizeWords(s string) []string { return strings.Fields(s) }

// CacheEstimate is the prefix-reuse result the report prints: the estimated cache-HIT input
// tokens, the total input tokens they are a fraction of, and the derived hit %, plus how the
// estimate was formed (block size, how many calls already carried a REAL server hit count we
// deferred to instead of estimating).
type CacheEstimate struct {
	// HitTokens is the estimated count of input tokens that would be served from the prefix
	// cache across the whole workload; MissTokens is the rest of the input. HitTokens +
	// MissTokens == total input tokens.
	HitTokens  int
	MissTokens int
	// BlockTokens is the provider block granularity used to round each match down (64 for
	// DeepSeek). ServerHitCalls is how many calls already carried a real cached_input_tokens
	// from the server, which we used DIRECTLY instead of estimating (0 on a usage-less pilot).
	BlockTokens    int
	ServerHitCalls int
	Calls          int
}

// HitFraction is HitTokens / (HitTokens+MissTokens) — the estimated cache-hit % (0 when there
// were no input tokens). This is the headline number the tool prints.
func (e CacheEstimate) HitFraction() float64 {
	in := e.HitTokens + e.MissTokens
	if in == 0 {
		return 0
	}
	return float64(e.HitTokens) / float64(in)
}

// EstimateCacheHits walks the calls IN ORDER (cache state is causal — a call can only hit a
// prefix an EARLIER call established) and estimates the prefix-reuse cache-hit tokens.
//
// For each call:
//   - its input-token size is the server's real prompt_tokens when present, else the
//     calibrated text estimate (model.Estimate(prompt)) — the same token model the totals use,
//     so HitTokens and the input total live in one space;
//   - if the server already reported a real cached_input_tokens, that is the truth — use it
//     directly (and still insert the prompt so later calls can match it);
//   - otherwise estimate the hit: longest shared WORD-prefix with any prior prompt (the trie),
//     scaled WORDS→TOKENS by this call's tokens-per-word, then floored to whole BlockTokens
//     blocks (the provider's block accounting). That many leading tokens are hits; the rest
//     are misses;
//   - finally insert this prompt's words so subsequent calls can match against it.
//
// model is the calibrated chars/token model (CalibrateCPT) so the estimated path agrees with
// EstimateTokens. block is the provider block size (<=0 ⇒ defaultCacheBlockTokens).
func EstimateCacheHits(calls []Call, model TokenModel, block int) CacheEstimate {
	if block <= 0 {
		block = defaultCacheBlockTokens
	}
	trie := newPrefixTrie()
	est := CacheEstimate{BlockTokens: block, Calls: len(calls)}

	for _, c := range calls {
		prompt := c.Concat()
		words := tokenizeWords(prompt)

		// This call's input-token size (real or estimated) — the denominator contribution.
		inTokens := c.PromptTokens
		if inTokens < 0 {
			inTokens = model.Estimate(prompt)
		}

		var hit int
		switch {
		case c.hasUsage() && c.CachedInputTokens >= 0:
			// The server reported REAL usage AND a cache split for this call — defer to it (a
			// cache-hit count only arrives alongside prompt usage; an isolated CachedInputTokens
			// on a usage-less call is the struct zero-value, not a server "0 hits"). On a
			// usage-less pilot we estimate instead (the case below).
			hit = c.CachedInputTokens
			if hit > inTokens {
				hit = inTokens
			}
			est.ServerHitCalls++
		default:
			// Estimate from the longest shared word-prefix against everything seen so far.
			matchWords := trie.matchLen(words)
			if matchWords > 0 && len(words) > 0 {
				// Scale shared WORDS → shared TOKENS by this prompt's own tokens-per-word, so a
				// shared 100-word System block maps to its real token weight. tokensPerWord =
				// inTokens / len(words); shared-tokens = matchWords * tokensPerWord.
				tokensPerWord := float64(inTokens) / float64(len(words))
				sharedTokens := int(float64(matchWords)*tokensPerWord + 0.5)
				// Round DOWN to whole provider blocks — a partial trailing block is not a hit.
				hit = (sharedTokens / block) * block
				if hit > inTokens {
					hit = inTokens
				}
			}
		}

		est.HitTokens += hit
		est.MissTokens += inTokens - hit
		// Make this prompt matchable by later calls.
		trie.insert(words)
	}
	return est
}
