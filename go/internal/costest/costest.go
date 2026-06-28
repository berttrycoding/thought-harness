// Package costest is the PART-4 NO-PAID-RUN projector: given a --log JSONL of llm.call
// events (each carrying its full system+user PROMPT), it estimates two things a local /
// usage-less pilot run cannot read off the wire, so the DeepSeek $ of a workload can be
// projected BEFORE spending a cent:
//
//  1. PREFIX-REUSE cache-hit % (prefix.go) — DeepSeek caches on an EXACT prompt-PREFIX
//     match across requests. Walk the calls in order; for each call, find the longest
//     token prefix it shares with ANY prior call's prompt (a trie of prefixes), and count
//     those shared leading tokens as cache HITS. The estimate is total-hit-tokens /
//     total-input-tokens over the log.
//
//  2. TOKEN counts (tokens.go) — for a provider/log that reports NO usage (LM Studio
//     pilots, the offline test double), estimate prompt/completion token counts from the
//     prompt+response TEXT via a chars/CPT heuristic, CALIBRATED against any calls in the
//     SAME log that DO carry real usage (so the estimate is anchored to the real tokenizer
//     when even one ground-truth sample exists).
//
// The two feed Project (project.go): estimated (in, out, cache-hit%) → priced against a
// cost.RateCard row (e.g. deepseek-reasoner) → the projected $ for the workload. It NEVER
// makes a network call and NEVER needs an API key — it reads a captured log and does math.
//
// STDLIB-ONLY (the core stays dependency-free): encoding/json + strings + the sibling
// internal/cost rate card. The CALL it reads is richer than cost.LLMCall — it keeps the
// PROMPT text (System+User) and the raw response, which the prefix + token estimators
// need and the pure $ layer (internal/cost) does not.
package costest

// Call is one llm.call projected for ESTIMATION: the role/model, the full prompt text
// (System+User, the prefix-cache substrate), the raw response text (the completion the
// token estimator sizes), and whatever real usage the server DID report (-1 = absent, the
// cost layer's "not reported" sentinel). The prompt is the concatenation the provider
// actually hashes a prefix of: System then User, joined exactly as Concat builds it.
type Call struct {
	Role  string
	Model string
	// System / User are the two halves of the chat prompt (the llm.call event's "system"
	// and "user" data fields). Concat() is the single ordering the prefix + token estimators
	// agree on, so the cache trie and the token count see the same bytes.
	System string
	User   string
	// Response is the model's raw output text (the llm.call event's "raw" field) — the
	// completion the token estimator sizes when the server reports no completion_tokens.
	Response string

	// PromptTokens / CompletionTokens are the server-reported usage (usage.prompt_tokens /
	// completion_tokens). -1 = absent. When present they are GROUND TRUTH: the token
	// estimator calibrates its chars-per-token against them, and the prefix estimator uses
	// the real prompt size as the per-call input denominator.
	PromptTokens     int
	CompletionTokens int
	// CachedInputTokens is the server-reported cache-HIT input portion (DeepSeek hit /
	// OpenAI cached). -1 = absent. When present it is the REAL cache hit for that call (the
	// prefix estimate is only used where the server didn't report one).
	CachedInputTokens int
}

// Concat is the single canonical prompt string the prefix-cache and token estimators both
// read: System then User, newline-joined (the provider hashes a prefix of the rendered
// chat; System leads, so a shared System block is a shared prefix). An empty System or User
// is dropped so the join never starts/ends with a stray newline (which would shift every
// downstream prefix by one token).
func (c Call) Concat() string {
	switch {
	case c.System == "":
		return c.User
	case c.User == "":
		return c.System
	default:
		return c.System + "\n" + c.User
	}
}

// hasUsage reports whether the server gave this call REAL token usage (a non-negative
// prompt_tokens). The token estimator uses these calls to CALIBRATE; the prefix estimator
// uses the real prompt size as the denominator for them.
func (c Call) hasUsage() bool { return c.PromptTokens >= 0 }
