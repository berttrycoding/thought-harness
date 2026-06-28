// Package llm is the real Stage-1 "all-frontier scaffold" for the language faculty: an
// OpenAI-compatible chat backend that slots in behind backends.Backend, so the CONSCIOUS
// layer writes its own thoughts, the hidden seam re-voices injections, and the Filter ESCALATES
// a borderline admission — with a REAL model. The admission FLOOR and candidate RANK are NOT here:
// they are Pattern-A control math in internal/control, called directly by the seam. Specialists
// keep their own domain competence; this is only the language faculty + the one escalation.
//
// A real model is the PRODUCT substrate (ResolveSubstrate picks a configured frontier model,
// else a reachable local one, else ERRORS — there is no offline product path; the
// deterministic test backend is a TEST DOUBLE only). This backend owns CONTENT only: on a
// transient per-call failure the CONTENT roles (generate / transform / summarize / respond /
// operator-apply) surface the gap (return "") so a test-double template never reaches the stream
// or the answer — they NEVER substitute deterministic text. There is no control role here to fall
// back: the admission FLOOR and candidate RANK are pure Pattern-A math in internal/control, called
// directly by the seam (no backend). The one model-backed control touchpoint is the Filter's
// Pattern-C ESCALATION (JudgeAdmission) — on decline it returns ok=false and the caller keeps the
// floor verdict (it never substitutes a stand-in either). Every degrade emits a visible
// llm.fallback event; the per-tick scheduler routes only foreground reasoning to the model.
//
// STDLIB-ONLY: talks to any OpenAI-compatible /v1/chat/completions endpoint with net/http, so
// the core stays dependency-free. Using a model makes runs non-deterministic; the tests pin the
// test double.
//
// Config (env): THOUGHT_LLM_BASE_URL (default http://localhost:1234/v1), THOUGHT_LLM_MODEL,
// THOUGHT_LLM_UTILITY_MODEL (small model for trivial roles), THOUGHT_LLM_API_KEY.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Defaults — mirror llm.py module-level constants (env-overridable).
var (
	defaultBaseURL = envOr("THOUGHT_LLM_BASE_URL", "http://localhost:1234/v1")
	// Default to gemma-4-26b-a4b — the DECIDED substrate (2026-06-11): ~10x faster per bench item
	// than the qwen reasoning model (non-reasoning MoE: no reasoning-chain inflation, no truncation->
	// escalation slow path), grounding lift +1.000 on the smoke, and it exercises the full cognition
	// (synthesises real programs). Pass "auto" to use whatever is currently loaded in LM Studio.
	defaultModel = envOr("THOUGHT_LLM_MODEL", "google/gemma-4-26b-a4b")
	defaultKey   = envOr("THOUGHT_LLM_API_KEY", "lm-studio")
)

// Reasoning defaults — the base-model class is now a REASONING model (it emits a reasoning trace
// BEFORE the answer), so these defaults are tuned for that. All env-overridable.
const (
	// defaultMaxTokens is the per-call completion budget. RAISED 1536 → 4096 → 8192: a reasoning
	// model fills its reasoning trace before any answer; 4096 still TRUNCATED structured roles
	// (synthesize_program hit finish_reason=length at 4096 → unparseable → control-floor fallback →
	// the read-workflow was never built → grounding silently HALLUCINATED). 8192 gives reasoning +
	// a structured payload headroom; the broadened retry-on-truncation backstops the long tail. The
	// 32K context window holds it (a single call generated ~12K tokens at parallel=4 — context was
	// never the limit, the completion budget was). Override via THOUGHT_LLM_MAX_TOKENS.
	defaultMaxTokens = 8192

	// retry-on-truncation defaults: when finish_reason=="length" AND the result is unusable — either
	// (post-salvage) content is empty/incomplete, OR a structured role's payload fails to parse —
	// retry the SAME request with a larger BOUNDED budget (×2 per retry, capped, bounded retries).
	defaultRetryMaxTokensMul = 2.0   // grow the budget 2x per retry
	defaultRetryMaxTokensCap = 16384 // never request more than this (bounded — no runaway budget)
	defaultMaxRetries        = 1     // at most ONE retry (bounded — never loop unbounded)
)

// defaultReasoningFields is the order the reasoning trace is looked for across providers: LM Studio
// / vLLM use reasoning_content; some gateways use reasoning; others thinking. Env-overridable via
// THOUGHT_LLM_REASONING_FIELDS (comma list).
var defaultReasoningFields = []string{"reasoning_content", "reasoning", "thinking"}

// thinkRe strips inline <think>...</think> reasoning some models put in `content`.
var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

// callRecord is one entry of the per-call I/O ring (the thing you need to see when a small local
// model returns malformed JSON to the Filter/Gate). Mirrors the Python self.log dict.
type callRecord struct {
	Role         string `json:"role"`
	System       string `json:"system"`
	User         string `json:"user"`
	Raw          string `json:"raw"`
	MS           int    `json:"ms"`
	OK           bool   `json:"ok"`
	Error        string `json:"error"`
	FinishReason string `json:"finish_reason"`
	// Reasoning-model observability (so the NOISY-RULER source is visible per call):
	ReasoningTokens int  `json:"reasoning_tokens"` // usage.completion_tokens_details.reasoning_tokens, -1 if absent
	SalvageUsed     bool `json:"salvage_used"`     // the answer was mined out of the reasoning trace
	RetryCount      int  `json:"retry_count"`      // retries spent on truncation before this result
	// Full usage accounting (so bench Cost.Tokens + per-role cost attribution is real, not 0).
	// All -1 when the server didn't report the field (distinguish "absent" from a true 0).
	PromptTokens      int `json:"prompt_tokens"`       // usage.prompt_tokens
	CompletionTokens  int `json:"completion_tokens"`   // usage.completion_tokens
	TotalTokens       int `json:"total_tokens"`        // usage.total_tokens
	CachedInputTokens int `json:"cached_input_tokens"` // DeepSeek prompt_cache_hit_tokens OR OpenAI prompt_tokens_details.cached_tokens
	CacheMissTokens   int `json:"cache_miss_tokens"`   // DeepSeek prompt_cache_miss_tokens (OpenAI does not report a miss count)
}

// OpenAICompatBackend is an OpenAI-compatible chat backend. It owns CONTENT only: on a per-call
// failure its CONTENT roles (generate / transform / summarize / respond / operator-apply) surface
// the gap (return "") instead of substituting a deterministic template, so test-double text never
// reaches output. It has NO control fallback — the admission floor + candidate rank are Pattern-A
// math in internal/control, never this backend's job. It implements backends.Backend (the CONTENT
// methods) + the optional capability/binder interfaces, including FilterEscalator (the Pattern-C
// admission escalation, which on decline returns ok=false so the control floor stands).
type OpenAICompatBackend struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration

	// Reasoning is the reasoning-model robustness config (multi-provider field list, salvage,
	// retry-on-truncation). Defaults are reasoning-friendly; see ReasoningOptions.
	Reasoning ReasoningOptions

	client *http.Client

	emit  events.Emit             // nil until BindEmit
	sched *scheduler.LLMScheduler // nil until BindScheduler

	// legibleFragment is the in-band control-tag instruction appended to the conscious Generate
	// system prompt when the seam.legible_generation SHADOW instrument is ON (set per-tick by the
	// engine from the live toggle). "" ⇒ no append ⇒ the Generate prompt is byte-identical to before
	// the instrument existed (the default). See backends.LegiblePrompter / 05-LEGIBLE-GENERATION §5b.
	legibleFragment string

	// personaFragment is the learned person-adaptation instruction appended to the RESPOND system
	// prompt (P7.3; backends.PersonaPrompter). Set by the engine right before an outward-facing
	// respond; "" (no learned preferences — the default) keeps the prompt byte-identical. Same
	// happens-before story as legibleFragment: written from the tick loop, never concurrently with
	// a respond call.
	personaFragment string

	// groundCompleteFragment is the grounding-completeness reading directive appended to the RESPOND
	// system prompt when THOUGHT_GROUND_COMPLETE is ON (backends.GroundCompletePrompter). It asks the
	// model — BEFORE answering — to use the value actually IN FORCE (one a later in-material statement
	// corrected/replaced/overrode wins over an earlier name-match) and to apply an in-material
	// adjustment/conversion to the base value, while PRESERVING the never-fabricate discipline (a value
	// only referenced via an unreadable external pointer ⇒ DECLINE, never invent). "" (the default ⇒
	// flag OFF) keeps the RESPOND prompt byte-identical. Same happens-before story as personaFragment:
	// written from the tick loop right before respond, never concurrently with a respond call. Because
	// the bridge (claudecode.go) is THIS backend with only the transport swapped, the fragment reaches
	// the --backend claude answer path (the bench's substrate) unchanged.
	groundCompleteFragment string

	// mu guards the per-call mutable counters/flags (Calls/Fallbacks/degradedLogged) so the shared
	// backend is race-free when several reason-only sub-agents call it concurrently under per-phase
	// parallelism (THOUGHT_PARALLEL_PHASES). It is held ONLY around the counter mutation, never across
	// the HTTP model call (the Log ring carries its own lock; the event bus is already mutex-safe).
	mu             sync.Mutex
	degradedLogged bool
	Calls          int
	Fallbacks      int

	// structuredEscalate is the optional model-escalation tier (Item 3): the LAST-RESORT attempt for
	// a STRUCTURED role (synthesize_program / form_intention) that STILL truncates-invalid after the
	// bounded retry exhausts at the max budget. nil ⇒ NO escalation tier (the common case) ⇒ ZERO
	// behaviour change. NewTiered wires it to one call against the UTILITY backend, so a primary that
	// chronically truncates a structured payload at the budget gets one final attempt against the
	// (often differently-behaved) escalation model before the caller falls to the control floor. It is
	// BOUNDED to a single call — never a loop. The role string is the SAME role tag; on success it
	// returns (raw, true) and the caller parses it; on any failure it returns ("", false) and the
	// floor stands. See escalateStructured + TieredBackend.
	structuredEscalate func(system, user, role string, validate func(string) bool) (string, bool)

	// transport, when non-nil, replaces the HTTP round-trip with an alternative model-call path
	// (the Claude Code exec bridge, claudecode.go). It receives the SAME OpenAI-shaped request
	// body chat() builds and returns a postResult under the postChat contract, so every role's
	// prompt/validate/retry/salvage/telemetry logic is reused unchanged. nil ⇒ the default HTTP
	// postChat (the common case — zero behaviour change).
	transport func(reqBody map[string]any, salvage bool) (postResult, error)

	// transportHealth, when non-nil, replaces the HTTP /models health check for a non-HTTP
	// transport (the bridge has no server to GET). nil ⇒ the default listModels probe.
	transportHealth func() HealthReport

	// displayName, when non-"", overrides the "llm:<model>" UI name (the bridge reports
	// "claude:<model>" so the TUI/doctor show which substrate is thinking).
	displayName string
	// substrateClass is the canonical substrate this backend embodies — "local" | "frontier" |
	// "session" | "claude" — stamped at CONSTRUCTION (NewOpenAICompat derives local/frontier from
	// the endpoint; the bridge constructors override). The one truth the UI reads (ClassOf) —
	// never derived by parsing display labels.
	substrateClass string

	// Log is the per-call I/O ring (maxlen 256) — full role/prompts/raw/latency/ok for debugging.
	Log *ring
}

// Options configures an OpenAICompatBackend; the zero value yields the defaults via
// NewOpenAICompat's normalisation.
type Options struct {
	BaseURL     string
	Model       string
	APIKey      string
	Temperature float64 // 0 → default 0.7
	MaxTokens   int     // 0 → THOUGHT_LLM_MAX_TOKENS (default 4096) — reasoning-model headroom
	Timeout     time.Duration

	// Reasoning carries the reasoning-model knobs (multi-provider field list, salvage,
	// retry-on-truncation). The zero value is normalised to reasoning-friendly defaults (+ env) by
	// NewOpenAICompat — leave it zero to get the defaults, set fields to override.
	Reasoning ReasoningOptions
}

// ReasoningOptions configures the reasoning-model robustness path: which message fields carry the
// reasoning trace (multi-provider), whether to SALVAGE the answer from that trace when content is
// empty, and whether to RETRY a truncated (finish_reason=length) empty completion with a larger
// bounded budget. The zero value is normalised to reasoning-friendly defaults by NewOpenAICompat.
type ReasoningOptions struct {
	// Fields is the ordered list of message keys to read the reasoning trace from (tried in order).
	// "" / nil → defaultReasoningFields (reasoning_content, reasoning, thinking), env-overridable
	// via THOUGHT_LLM_REASONING_FIELDS (comma list).
	Fields []string

	// SalvageFromReasoning: when content is empty/whitespace but a reasoning field is present, mine
	// the answer OUT of the reasoning trace (the model's OWN output, misfiled — Pattern B, not the
	// banned test-double fallback). Default true. Env THOUGHT_LLM_SALVAGE (0/false to disable).
	SalvageFromReasoning bool

	// salvageSet records whether SalvageFromReasoning was set explicitly (Go bools can't carry a
	// nil); NewOpenAICompat reads it to distinguish "left default" from "explicitly false".
	salvageSet bool

	// RetryOnTruncation: when finish_reason=="length" AND the post-salvage content is still
	// empty/clearly-incomplete, retry the SAME request with a larger budget. Default true.
	// Env THOUGHT_LLM_RETRY_ON_TRUNCATION (0/false to disable).
	RetryOnTruncation bool
	retrySet          bool // as salvageSet, for the retry default

	// RetryMaxTokensMul grows the budget per retry (0 → defaultRetryMaxTokensMul = 2.0).
	RetryMaxTokensMul float64
	// RetryMaxTokensCap caps the grown budget (0 → defaultRetryMaxTokensCap = 16384). Bounded.
	RetryMaxTokensCap int
	// MaxRetries bounds the retries (0 → defaultMaxRetries = 1; <0 → no retry). Bounded — never loops.
	MaxRetries int
	maxRetSet  bool // distinguish 0 ("use default 1") from an explicit 0 ("no retries")
}

// WithSalvage / WithRetry / WithMaxRetries are explicit setters that also flip the *Set flag, so a
// caller can set the bool to false and still override the default (Go's zero value would otherwise
// be indistinguishable from "unset"). NewOpenAICompat honours these.
func (r ReasoningOptions) WithSalvage(v bool) ReasoningOptions {
	r.SalvageFromReasoning, r.salvageSet = v, true
	return r
}
func (r ReasoningOptions) WithRetry(v bool) ReasoningOptions {
	r.RetryOnTruncation, r.retrySet = v, true
	return r
}
func (r ReasoningOptions) WithMaxRetries(n int) ReasoningOptions {
	r.MaxRetries, r.maxRetSet = n, true
	return r
}

// NewOpenAICompat builds a backend from opts, applying the Python __init__ defaults. If
// opts.Model == "auto" it auto-detects the loaded model from /models.
func NewOpenAICompat(opts Options) *OpenAICompatBackend {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		// A reasoning model fills its reasoning trace before any answer; too small a budget hits
		// finish_reason=length with EMPTY content and every role falls back. Default generous (4096).
		maxTokens = envInt("THOUGHT_LLM_MAX_TOKENS", defaultMaxTokens)
	}
	temp := opts.Temperature
	if temp == 0 {
		temp = 0.7
	}
	timeout := opts.Timeout
	if timeout == 0 {
		// Reasoning models are the DEFAULT class and are SLOW by design: a call that hits the 16384-token
		// retry budget at ~100 tok/s needs ~160s, and under bench concurrency the GPU is split further. The
		// old 60s default silently TIMED OUT those calls (err, finish="") → the caller surfaced the
		// "thinking substrate unavailable" gap (grounding items 11/14). Default big; env-overridable.
		timeout = time.Duration(envInt("THOUGHT_LLM_TIMEOUT_SECONDS", 300)) * time.Second
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	key := opts.APIKey
	if key == "" {
		key = defaultKey
	}
	model := opts.Model
	if model == "" {
		model = defaultModel
	}
	cls := "frontier"
	if isLocalURL(base) {
		cls = "local"
	}
	b := &OpenAICompatBackend{
		substrateClass: cls,
		BaseURL:        strings.TrimRight(base, "/"),
		APIKey:         key,
		Model:          model,
		Temperature:    temp,
		MaxTokens:      maxTokens,
		Timeout:        timeout,
		Reasoning:      normalizeReasoning(opts.Reasoning),
		client:         &http.Client{Timeout: timeout},
		Log:            newRing(256),
	}
	if b.Model == "auto" {
		b.Model = b.autodetectModel()
	}
	return b
}

// normalizeReasoning fills a ReasoningOptions with the reasoning-friendly defaults (and env
// fallbacks) for any field left at its zero value. Explicit setters (WithSalvage/WithRetry/
// WithMaxRetries) win over both the env and the default, so a caller can force a bool false.
func normalizeReasoning(r ReasoningOptions) ReasoningOptions {
	if len(r.Fields) == 0 {
		r.Fields = envFields("THOUGHT_LLM_REASONING_FIELDS", defaultReasoningFields)
	}
	if !r.salvageSet {
		r.SalvageFromReasoning = envBool("THOUGHT_LLM_SALVAGE", true)
	}
	if !r.retrySet {
		r.RetryOnTruncation = envBool("THOUGHT_LLM_RETRY_ON_TRUNCATION", true)
	}
	if r.RetryMaxTokensMul == 0 {
		r.RetryMaxTokensMul = envFloat("THOUGHT_LLM_RETRY_MULT", defaultRetryMaxTokensMul)
	}
	if r.RetryMaxTokensCap == 0 {
		r.RetryMaxTokensCap = envInt("THOUGHT_LLM_RETRY_CAP", defaultRetryMaxTokensCap)
	}
	if !r.maxRetSet {
		r.MaxRetries = envInt("THOUGHT_LLM_MAX_RETRIES", defaultMaxRetries)
	}
	return r
}

// AppraiserName is "llm" (P6: who appraised, tagged onto captured Appraisals). Mirrors the
// Python `appraiser_name = "llm"` class attribute.
func (b *OpenAICompatBackend) AppraiserName() string { return "llm" }

// SubstrateClass reports the canonical substrate this backend embodies (see the field).
func (b *OpenAICompatBackend) SubstrateClass() string { return b.substrateClass }

// DisplayName reports a human-readable backend name for the UI (llm.display_name).
func (b *OpenAICompatBackend) DisplayName() string {
	if b.displayName != "" {
		return b.displayName
	}
	return "llm:" + b.Model
}

// BindEmit wires the engine's event bus so LLM calls/fallbacks show up in the trace.
func (b *OpenAICompatBackend) BindEmit(emit events.Emit) { b.emit = emit }

// BindScheduler wires the LLM-call scheduler: a deferred background call returns ""/ok=false (the
// CONTENT role surfaces the gap; the Filter escalation lets the control floor stand).
func (b *OpenAICompatBackend) BindScheduler(s *scheduler.LLMScheduler) { b.sched = s }

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func (b *OpenAICompatBackend) autodetectModel() string {
	ids, err := b.listModels(min(b.Timeout, 5*time.Second))
	if err != nil || len(ids) == 0 {
		return "local-model"
	}
	return ids[0]
}

// listModels GETs /models and returns the loaded model ids (used by autodetect + Health).
func (b *OpenAICompatBackend) listModels(timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.APIKey)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// chatOpts carries the per-call knobs (Python's kwargs to _chat).
type chatOpts struct {
	temperature *float64 // nil → b.Temperature
	maxTokens   int      // 0 → b.MaxTokens
	salvage     bool     // salvage reasoning_content when content is empty
	// validate, when set, reports whether a STRUCTURED role's response is usable (e.g. "the JSON
	// parses"). When a response comes back finish_reason=="length" (truncated) AND validate fails,
	// chat retries with a grown budget — so a structured payload that truncated mid-JSON gets another
	// (bigger) attempt instead of silently falling through to the control floor. nil ⇒ no parse-gate
	// (the truncated-EMPTY retry still applies to every role).
	validate func(string) bool
}

// chat is the single model-call path. A deferred background call (the scheduler said no) spends
// no model time and returns ("", false). On any network/no-model/timeout/empty/bad-shape error it
// degrades (emits one llm.fallback) and returns ("", false). The CALLER decides what ("", false)
// means: a CONTENT role (generate/transform/summarize/respond/operator-apply) surfaces the gap (no
// test-double faking of output); the Filter escalation (judge_admission) returns ok=false so the
// deterministic control floor stands (Rule 4). On success it returns (content, true) and emits one
// llm.call. It NEVER panics.
func (b *OpenAICompatBackend) chat(system, user, role string, opt chatOpts) (string, bool) {
	// Rate/throughput actuator: a deferred background call skips the model and returns the gap
	// ("", false) — a CONTENT role then surfaces it (Pattern B), never a substituted template.
	if b.sched != nil && !b.sched.Grant(role) {
		return "", false
	}
	temp := b.Temperature
	if opt.temperature != nil {
		temp = *opt.temperature
	}
	maxTok := opt.maxTokens
	if maxTok == 0 {
		maxTok = b.MaxTokens
	}

	// SALVAGE is gated on BOTH the per-role request (opt.salvage — narrative roles disable it, JSON/
	// answer roles enable it) AND the backend-wide SalvageFromReasoning toggle. The global toggle is
	// the master kill-switch: SalvageFromReasoning=false disables salvage for EVERY role.
	salvage := opt.salvage && b.Reasoning.SalvageFromReasoning

	// RETRY-ON-TRUNCATION: a reasoning model that runs out of budget mid-reasoning returns
	// finish_reason=="length" with EMPTY (post-salvage) content. Rather than degrade — which makes
	// the same prompt pass/fail nondeterministically (the NOISY-RULER) — retry the SAME request once
	// with a larger BOUNDED budget. Bounded by MaxRetries + the budget cap: it NEVER loops unbounded.
	maxRetries := 0
	if b.Reasoning.RetryOnTruncation && b.Reasoning.MaxRetries > 0 {
		maxRetries = b.Reasoning.MaxRetries
	}

	// THINKING TOGGLE (configurable, off by default): when THOUGHT_LLM_ENABLE_THINKING is set, forward
	// chat_template_kwargs:{enable_thinking:<bool>} so a reasoning model's thinking channel is a
	// benchmark-controlled variable. Unset ⇒ nil ⇒ the field is omitted (model default). Read once.
	thinkKw, _ := thinkingKwargs()

	var (
		raw, finish string
		err         error
		res         postResult
		retryCount  int
	)
	t0 := time.Now()
	for attempt := 0; ; attempt++ {
		reqBody := map[string]any{
			"model": b.Model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
			"temperature": temp,
			"max_tokens":  maxTok,
			"stream":      false,
		}
		if thinkKw != nil {
			reqBody["chat_template_kwargs"] = thinkKw
		}
		post := b.postChat
		if b.transport != nil {
			post = b.transport
		}
		res, err = post(reqBody, salvage)
		raw, finish = res.content, res.finish
		// Retry when a TRUNCATED (finish_reason=length) completion came back UNUSABLE and a retry
		// budget remains — the budget ran out mid-output, so a bigger one is the fix. Two cases:
		//   (a) truncated-EMPTY    — empty content even after salvage (err is set); any role.
		//   (b) truncated-INVALID  — a STRUCTURED role whose payload fails its parse validator (err
		//        is nil — the model returned partial garbage). This is the synthesize_program /
		//        form_intention case: a half-written JSON program that would otherwise fall straight
		//        to the control floor (no read-workflow built → grounding hallucinates). Retry it.
		// Growing must actually raise the cap (else retrying the identical request is pointless).
		truncatedEmpty := err != nil && res.truncatedEmpty
		truncatedInvalid := err == nil && finish == "length" && opt.validate != nil && !opt.validate(raw)
		if (truncatedEmpty || truncatedInvalid) && attempt < maxRetries {
			next := b.grownBudget(maxTok)
			if next > maxTok {
				maxTok = next
				retryCount++
				continue
			}
		}
		break
	}
	ms := int(math.Round(float64(time.Since(t0).Microseconds()) / 1000.0))

	rec := callRecord{Role: role, System: system, User: user, Raw: raw, MS: ms,
		OK: err == nil, FinishReason: finish, ReasoningTokens: res.reasoningTokens,
		SalvageUsed: res.salvageUsed, RetryCount: retryCount,
		PromptTokens: res.promptTokens, CompletionTokens: res.completionTokens,
		TotalTokens: res.totalTokens, CachedInputTokens: res.cachedInputTokens,
		CacheMissTokens: res.cacheMissTokens}
	if err != nil {
		rec.Error = err.Error()
	}
	// Always record the FULL I/O so a subsystem's exact prompt + raw response is debuggable.
	b.Log.push(rec)

	// MODEL-ESCALATION TIER (Item 3): a call the MIN-context primary could NOT make usable — whatever the
	// reason — gets ONE final attempt against the configured MAX-context model before the caller surfaces
	// the gap / falls to the control floor. "Unusable" is the real signal, NOT finish_reason: a CONTENT
	// role that came back empty, OR a STRUCTURED role whose payload is empty / fails its validator. This
	// covers ALL the gap shapes, not just truncation (finish=length): a reasoning model that returned
	// empty with finish=stop, AND a call that ERRORED/TIMED OUT (err set, finish="") — the bench
	// grounding items 11/14 surfaced the "thinking substrate unavailable" gap on a 60s timeout, which the
	// old finish=="length" gate skipped, so the escalation never fired. A SUCCESSFUL call (non-empty /
	// valid raw) is never unusable ⇒ never escalates, even at finish=length (a partial answer is kept).
	// NO-OP when no escalation tier is wired (structuredEscalate == nil, the common case) ⇒ ZERO behaviour
	// change by default. BOUNDED to a single call (escalateStructured itself never loops).
	if b.structuredEscalate != nil {
		var unusable bool
		if opt.validate != nil {
			unusable = raw == "" || !opt.validate(raw) // structured: empty or parsed-but-invalid
		} else {
			unusable = strings.TrimSpace(raw) == "" // content: empty (any cause — truncation, empty-stop, error)
		}
		if unusable {
			if escRaw, escOK := b.structuredEscalate(system, user, role, opt.validate); escOK {
				return escRaw, true
			}
		}
	}

	if err == nil {
		b.mu.Lock()
		b.Calls++
		b.mu.Unlock()
		if b.emit != nil {
			// Carry the FULL prompt + raw response + reasoning-model telemetry (finish_reason,
			// reasoning_tokens, salvage_used, retry_count) AND the full usage accounting (prompt/
			// completion/total + cache hit/miss) so the NOISY-RULER source is visible AND cost is
			// real (not 0) in a --log JSONL and the TUI/trace.
			b.emit(events.LLM, "["+role+"] "+b.Model+" ("+itoa(ms)+"ms): "+head(raw, 36),
				events.D{"role": role, "model": b.Model, "ms": ms, "raw": raw,
					"system": system, "user": user, "finish_reason": finish,
					"reasoning_tokens": res.reasoningTokens, "salvage_used": res.salvageUsed,
					"retry_count": retryCount,
					// Full token accounting (so bench Cost.Tokens + per-role cost attribution is real):
					"prompt_tokens": res.promptTokens, "completion_tokens": res.completionTokens,
					"total_tokens": res.totalTokens, "cached_input_tokens": res.cachedInputTokens,
					"cache_miss_tokens": res.cacheMissTokens})
		}
		return raw, true
	}
	b.degrade(err.Error(), role)
	return "", false
}

// escalateStructured is the single-call escalation entry (Item 3): a STRUCTURED role's LAST-RESORT
// attempt against THIS backend, used as the escalation tier wired into another backend's
// structuredEscalate hook (NewTiered points the primary's hook at the utility backend's
// escalateStructured). It runs ONE chat (the SAME system/user/role + parse validator) so the
// escalation model gets the structured-payload retry headroom too, but it carries NO escalator of its
// own — so the chain is BOUNDED: primary -> one escalation call, never a loop. The "escalation" role
// suffix tags the call record/trace so the escalation is observable. On success returns (raw, true);
// on any failure ("", false) so the caller keeps the control floor.
func (b *OpenAICompatBackend) escalateStructured(system, user, role string, validate func(string) bool) (string, bool) {
	// structuredEscalate is deliberately NOT set on this backend (it is the LEAF of the chain), so the
	// chat below cannot re-escalate — the single-call bound holds by construction.
	return b.chat(system, user, role+".escalation", chatOpts{temperature: f(0.2), salvage: true, validate: validate})
}

// grownBudget returns the next (larger, bounded) max_tokens for a retry: cur * mul, capped at the
// configured cap. It only ever GROWS toward the cap — once at the cap, it returns the cap (so the
// caller sees no further increase and stops retrying). Bounded by construction.
func (b *OpenAICompatBackend) grownBudget(cur int) int {
	mul := b.Reasoning.RetryMaxTokensMul
	if mul <= 1.0 {
		mul = defaultRetryMaxTokensMul
	}
	cap := b.Reasoning.RetryMaxTokensCap
	if cap <= 0 {
		cap = defaultRetryMaxTokensCap
	}
	next := int(math.Round(float64(cur) * mul))
	if next > cap {
		next = cap
	}
	if next < cur {
		next = cur // never shrink
	}
	return next
}

// postResult is the structured outcome of one HTTP round-trip — the content plus the reasoning-model
// telemetry (finish_reason, reasoning_tokens, whether the answer was SALVAGED out of the reasoning
// trace) and the truncatedEmpty flag the retry loop branches on.
type postResult struct {
	content string
	finish  string
	// salvageUsed is true when content was empty and the answer was mined out of the reasoning trace
	// (Pattern B: the model's OWN output in the wrong field, not a substituted template).
	salvageUsed bool
	// reasoningTokens is usage.completion_tokens_details.reasoning_tokens (how much budget the model
	// spent thinking before answering); -1 when the server didn't report it.
	reasoningTokens int
	// Full usage accounting decoded from the response `usage` object; -1 when the server omitted a
	// field. promptTokens/completionTokens/totalTokens are the headline counts; cachedInputTokens +
	// cacheMissTokens carry the cache split across BOTH provider shapes (DeepSeek hit/miss counts and
	// OpenAI prompt_tokens_details.cached_tokens) so cost can be attributed (and discounted) per role.
	promptTokens      int
	completionTokens  int
	totalTokens       int
	cachedInputTokens int
	cacheMissTokens   int
	// truncatedEmpty: finish_reason=="length" AND the post-salvage content is still empty — the exact
	// "ran out of budget mid-reasoning" signal the retry-on-truncation path keys on.
	truncatedEmpty bool
}

// postChat performs the HTTP round-trip and extracts the content. The returned err is non-nil on a
// transport failure, a non-2xx status, a missing choice, or an EMPTY (post-salvage) completion; the
// postResult always carries the telemetry (finish, reasoning_tokens, salvage_used, truncatedEmpty)
// even on the empty-completion error, so the caller can decide to retry-on-truncation and emit it.
func (b *OpenAICompatBackend) postChat(reqBody map[string]any, salvage bool) (postResult, error) {
	res := postResult{reasoningTokens: -1, promptTokens: -1, completionTokens: -1,
		totalTokens: -1, cachedInputTokens: -1, cacheMissTokens: -1}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return res, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/chat/completions",
		bytes.NewReader(data))
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.APIKey)
	resp, err := b.client.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return res, &httpError{status: resp.StatusCode}
	}
	var parsed struct {
		Choices []struct {
			Message      map[string]any `json:"message"`
			FinishReason string         `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
			TotalTokens      *int `json:"total_tokens"`
			// DeepSeek context-cache shape: explicit hit/miss split of the prompt tokens.
			PromptCacheHitTokens    *int `json:"prompt_cache_hit_tokens"`
			PromptCacheMissTokens   *int `json:"prompt_cache_miss_tokens"`
			CompletionTokensDetails struct {
				ReasoningTokens *int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
			// OpenAI prompt-cache shape: cached portion of the prompt nested under details.
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return res, err
	}
	u := parsed.Usage
	if rt := u.CompletionTokensDetails.ReasoningTokens; rt != nil {
		res.reasoningTokens = *rt
	}
	if pt := u.PromptTokens; pt != nil {
		res.promptTokens = *pt
	}
	if ct := u.CompletionTokens; ct != nil {
		res.completionTokens = *ct
	}
	if tt := u.TotalTokens; tt != nil {
		res.totalTokens = *tt
	}
	// CACHE — try both provider shapes. DeepSeek reports an explicit hit/miss split
	// (prompt_cache_hit_tokens / prompt_cache_miss_tokens); OpenAI reports only the cached
	// portion (prompt_tokens_details.cached_tokens) and no miss count. Prefer whichever the
	// server actually sent; the cached-input count unifies both as "cached prompt tokens".
	if hit := u.PromptCacheHitTokens; hit != nil {
		res.cachedInputTokens = *hit
	} else if cached := u.PromptTokensDetails.CachedTokens; cached != nil {
		res.cachedInputTokens = *cached
	}
	if miss := u.PromptCacheMissTokens; miss != nil {
		res.cacheMissTokens = *miss
	}
	if len(parsed.Choices) == 0 {
		return res, errString("no choices in completion")
	}
	choice := parsed.Choices[0]
	res.finish = choice.FinishReason
	// SALVAGE (Pattern B): prefer the answer field (content); when it is empty and salvage is on,
	// mine the answer out of the reasoning trace (the model's OWN output, just misfiled — an empty
	// content with a full reasoning is NOT a gap, it is the answer in the wrong field). This is NOT
	// the banned test-double fallback: no deterministic template is ever substituted; the salvaged
	// text is the model's own reasoning conclusion. extractContent does the field walk + JSON/answer
	// extraction; salvageUsed reports whether it fired so the noise source is observable.
	content, salvageUsed := b.extractAnswer(choice.Message, salvage)
	res.content, res.salvageUsed = content, salvageUsed
	if content == "" {
		res.truncatedEmpty = res.finish == "length"
		return res, errString("empty completion (finish_reason=" + res.finish + ")")
	}
	return res, nil
}

// extractAnswer pulls the answer from a chat message and reports whether it was SALVAGED out of the
// reasoning trace. It prefers `content`; when that is empty/whitespace and salvage is on, it walks
// the configured reasoning fields (reasoning_content → reasoning → thinking) and mines the answer
// (the final conclusion / the JSON object for JSON roles) from the first non-empty one. The bool is
// true only when the returned text came from a reasoning field — the call site records it as
// salvage_used so the NOISY-RULER source (answer-in-the-wrong-field) is visible per call.
func (b *OpenAICompatBackend) extractAnswer(msg map[string]any, salvage bool) (string, bool) {
	if content := stripThink(asString(msg["content"])); content != "" {
		return content, false
	}
	if !salvage {
		return "", false
	}
	for _, field := range b.Reasoning.Fields {
		trace := stripThink(asString(msg[field]))
		if trace == "" {
			continue
		}
		// The reasoning trace IS the model's output, misfiled. salvageAnswer extracts the usable
		// answer from it (the JSON object for a JSON role, else the final conclusion).
		if ans := salvageAnswer(trace); ans != "" {
			return ans, true
		}
	}
	return "", false
}

// degrade records a connectivity / server-side failure (down, no model, timeout). One-time
// warning so a whole offline run doesn't spam, but every fallback still increments the counter.
func (b *OpenAICompatBackend) degrade(errMsg, role string) {
	b.mu.Lock()
	b.Fallbacks++
	first := !b.degradedLogged
	b.degradedLogged = true
	b.mu.Unlock()
	if first && b.emit != nil {
		b.emit(events.LLMFallback,
			"["+role+"] LLM unavailable ("+head(errMsg, 50)+"). "+
				"Load a model in LM Studio at "+b.BaseURL+".",
			events.D{"role": role, "error": errMsg})
	}
}

// parseFail records a parse failure: the model RESPONDED but its output didn't parse (the common
// small-model failure for the JSON-structured Pattern-C roles — Filter.judge_admission /
// Controller.decide / form_intention / synthesize_program). Logged every time WITH the raw output.
// It records ("", false)/(_, false) at the call site, so the deterministic FLOOR stands (the Filter's
// admission floor, the Controller's decision floor, the regex intention router, the shape recogniser)
// — NEVER a substituted heuristic guess. The narration says "the floor stands", not "-> heuristic".
func (b *OpenAICompatBackend) parseFail(role, raw string) {
	b.mu.Lock()
	b.Fallbacks++
	b.mu.Unlock()
	if b.emit != nil {
		b.emit(events.LLMFallback, "["+role+"] unparseable model output -> the control floor stands; "+
			"raw: "+pyRepr(head(raw, 48)),
			events.D{"role": role, "raw": raw, "parse_fail": true})
	}
}

// Health reports whether the server is up and which models are loaded (for `thought doctor`).
// Mirrors the Python health() dict via the typed HealthReport.
func (b *OpenAICompatBackend) Health() HealthReport {
	if b.transportHealth != nil {
		return b.transportHealth()
	}
	ids, err := b.listModels(min(b.Timeout, 5*time.Second))
	if err != nil {
		return HealthReport{Up: false, Error: err.Error(), Models: []string{}}
	}
	return HealthReport{Up: true, Models: ids}
}

// HealthReport is the typed replacement for the Python health() dict {up, models, error?}.
type HealthReport struct {
	Up     bool
	Models []string
	Error  string
}

// ---------------------------------------------------------------------------
// the 8 core Backend methods
// ---------------------------------------------------------------------------

// Generate is the CONSCIOUS serial effortful loop — the next GENERATED thought.
func (b *OpenAICompatBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	system, user := PromptGenerate(goal, ctx)
	// Legible-generation SHADOW (05 §5b): when the instrument is ON the engine has set a registry-derived
	// control-tag fragment; append it so the conscious is asked to emit the in-band tag (the seam strips
	// it before voicing and shadow-parses it for parity). "" ⇒ no change ⇒ the prompt is byte-identical.
	if b.legibleFragment != "" {
		system = system + "\n\n" + b.legibleFragment
	}
	out, ok := b.chat(system, user, "conscious.generate", chatOpts{temperature: f(0.8), salvage: false})
	if !ok {
		return "" // OUTPUT role: surface the gap; never a heuristic template (HEURISTICS.md)
	}
	return out
}

// Wander is the AWAKE-mode idle content role — ONE short first-person idle thought (curiosity /
// association / develop). It is a creative CONTENT role: on model decline it surfaces the gap
// (returns ""), exactly like Generate; the caller goes DARK (no canned substitute). The rng is part
// of the interface (the test double rotates its offline pool with it) but a real model ignores it.
func (b *OpenAICompatBackend) Wander(kind, hint string, ctx []types.Thought, rng *cpyrand.Random) string {
	system, user := PromptWander(kind, hint, ctx)
	out, ok := b.chat(system, user, "conscious.wander", chatOpts{temperature: f(0.9), salvage: false})
	if !ok {
		return "" // CONTENT role: surface the gap; the awake stream goes dark this tick (no faking)
	}
	return out
}

// SetLegibleFragment implements backends.LegiblePrompter: the engine sets the registry-derived control-
// tag instruction (appended to the Generate system prompt) when the SHADOW instrument is ON, and clears
// it ("") when OFF. Idempotent; no I/O. See 05-LEGIBLE-GENERATION §5b.
func (b *OpenAICompatBackend) SetLegibleFragment(fragment string) { b.legibleFragment = fragment }

// SetPersonaFragment implements backends.PersonaPrompter: the engine sets the learned-preference
// adaptation (P7.3) the RESPOND prompt should carry; "" clears it (no learned preferences).
func (b *OpenAICompatBackend) SetPersonaFragment(fragment string) { b.personaFragment = fragment }

// SetGroundCompleteFragment implements backends.GroundCompletePrompter: the engine sets the
// grounding-completeness reading directive the RESPOND prompt should carry when
// THOUGHT_GROUND_COMPLETE is ON; "" clears it (the default ⇒ byte-identical RESPOND prompt).
func (b *OpenAICompatBackend) SetGroundCompleteFragment(fragment string) {
	b.groundCompleteFragment = fragment
}

// Transform re-voices a raw specialist return as the system's OWN next first-person thought.
func (b *OpenAICompatBackend) Transform(c types.Candidate, hist []types.Thought) string {
	system, user := PromptTransform(c, hist)
	out, ok := b.chat(system, user, "seam.transform", chatOpts{temperature: f(0.7), salvage: false})
	if !ok {
		return "" // OUTPUT role: surface the gap; the caller shows the raw return un-revoiced
	}
	return out
}

// Summarize compresses a line of thinking into a one-line gist.
func (b *OpenAICompatBackend) Summarize(ts []types.Thought) string {
	system, user := PromptSummarize(ts)
	// maxTokens 1024 (was 200): the default substrate class emits a REASONING trace before the
	// answer (feedback-reasoning-models-are-default — gemma included), and 200 tokens were consumed
	// ENTIRELY by reasoning -> finish_reason=length with EMPTY content (the one E7 Column-B floor
	// gap). The gist itself stays <=14 words; the budget buys thinking room, not longer output.
	// Salvage stays off (a narrative role never mines its answer out of the reasoning channel).
	out, ok := b.chat(system, user, "conscious.compress",
		chatOpts{temperature: f(0.3), maxTokens: 1024, salvage: false})
	if !ok {
		return "" // OUTPUT role: surface the gap; an empty gist is harmless branch metadata
	}
	return "gist: " + out
}

// JudgeAdmission is the Filter's Pattern-C ESCALATION (the model CEILING above the deterministic
// floor). It is consulted ONLY on a flagged-fuzzy admission the floor was unsure about (the Filter
// gates the call on control.AdmitAmbiguity). It is GIVEN the floor's own verdict in the prompt so
// the model REFINES the borderline call rather than re-deriving it from scratch: "the deterministic
// check returned <verdict> at <conf> because <reason>; do you see a laundered hallucination it
// missed?". On a deferred/declined/parse-failed call it returns (_, false) — the caller keeps the
// floor verdict (Rule 4). It NEVER substitutes a deterministic stand-in (no b.fallback call).
func (b *OpenAICompatBackend) JudgeAdmission(c types.Candidate, hist []types.Thought, floor types.FilterVerdict) (types.FilterVerdict, bool) {
	system := `You are the Filter (admission ESCALATION). A deterministic check has ALREADY scored ` +
		`this raw candidate; you are the second opinion on a borderline case. Decide whether to ` +
		`admit it into the stream BEFORE it is voiced — weigh trust, coherence, and contradiction ` +
		`with recent thoughts, and especially whether it is a LAUNDERED HALLUCINATION the lexical ` +
		`check missed. Reply with ONLY JSON: ` +
		`{"verdict":"ADMIT|FLAG|REJECT","confidence":0.0-1.0,"reason":"..."}. ` +
		`ADMIT=trustworthy, FLAG=admit but low-confidence, REJECT=drop.`
	dom := ""
	if c.Domain != nil {
		dom = *c.Domain
	}
	user := "Candidate (" + c.Source.String() + "/" + dom + "): " + c.Text +
		"\nRecent thoughts:\n" + joinThoughts(hist, 5) +
		"\nThe deterministic check returned " + floor.Verdict.String() + " at " +
		ftoa2(floor.Confidence) + " because: " + floor.Reason +
		"\nDo you see a laundered hallucination it missed? JSON:"
	raw, ok := b.chat(system, user, "Filter.judge_admission", chatOpts{temperature: f(0.0), salvage: true})
	if !ok {
		return floor, false // model deferred/unavailable -> the floor stands (Rule 4)
	}
	verdict, parsed := parseVerdict(raw)
	if !parsed {
		b.parseFail("Filter.judge_admission", raw)
		return floor, false // unparseable -> the floor stands (Rule 4)
	}
	return verdict, true
}

// JudgeSufficiency is the CRAG-style sufficiency gate's Pattern-C model CEILING above the deterministic
// coverage FLOOR (A-RAG1). It is consulted ONLY on a flagged-fuzzy retrieval the floor could not decide
// (the gate gates the call on control.SufficiencyAmbiguity). It is GIVEN the need, the recalled fuel, and
// the floor's own verdict so the model REFINES the borderline grading rather than re-deriving it: "given
// this NEED, does this RECALLED FUEL actually cover it well enough to commit on, or should the harness
// ABSTAIN?". This is the structural fix for the abstention paradox — the model is asked to JUDGE coverage,
// not to "be careful" (a directive that backfired, see project-grounding-completeness-directive-rejected).
// Reply is ONLY one of the three verdict words. On a deferred/declined/parse-failed call it returns
// ("",false) — the caller keeps the floor verdict (Rule 4). It NEVER substitutes a deterministic stand-in.
func (b *OpenAICompatBackend) JudgeSufficiency(query, fuelText, floorVerdict string) (string, bool) {
	system := `You are the sufficiency gate (retrieval ESCALATION). A deterministic check has ALREADY ` +
		`graded whether some RECALLED material covers a NEED; you are the second opinion on a borderline ` +
		`case. Judge ONLY coverage: does the recalled material actually answer the need well enough to ` +
		`commit on, or is it off-topic / partial / about something else (in which case the system should ` +
		`ABSTAIN rather than over-commit a hollow recall)? Do NOT add caveats; just grade coverage. ` +
		`Reply with ONLY one word: sufficient, ambiguous, or insufficient.`
	user := "NEED: " + query +
		"\nRECALLED MATERIAL: " + fuelText +
		"\nThe deterministic check graded this " + floorVerdict + "." +
		"\nDoes the recalled material cover the need? One word:"
	raw, ok := b.chat(system, user, "Sufficiency.judge", chatOpts{temperature: f(0.0), salvage: true})
	if !ok {
		return "", false // model deferred/unavailable -> the floor stands (Rule 4)
	}
	v := parseSufficiencyWord(raw)
	if v == "" {
		b.parseFail("Sufficiency.judge", raw)
		return "", false // unparseable -> the floor stands (Rule 4)
	}
	return v, true
}

// parseSufficiencyWord extracts the sufficiency verdict word from a model reply (case-insensitive,
// substring-tolerant; "insufficient" CONTAINS "sufficient" so it is tested first). "" on no match.
func parseSufficiencyWord(raw string) string {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "insufficient"):
		return "insufficient"
	case strings.Contains(low, "ambiguous"):
		return "ambiguous"
	case strings.Contains(low, "sufficient"):
		return "sufficient"
	default:
		return ""
	}
}

// JudgeConscience is the conscience gate's Pattern-C model CEILING above the deterministic VetAction
// floor (§7.2). The floor has ALREADY allowed the action (no hard prohibition); this is the nuanced
// good/bad second opinion on a flagged-fuzzy action, and it may only TIGHTEN (refuse). allow=true keeps
// the act; allow=false refuses it with a reason. On a deferred/declined/parse-failed call it returns
// (true,"",false) — the caller lets the floor's allow stand (Rule 4). NEVER a deterministic stand-in.
func (b *OpenAICompatBackend) JudgeConscience(actionText string) (allow bool, reason string, ok bool) {
	system := `You are the conscience (action ESCALATION). A deterministic floor has ALREADY allowed ` +
		`this action (it matched no hard prohibition). You are the nuanced second opinion on a ` +
		`borderline, possibly-harmful action: weigh whether it is GOOD to do — honest, within authority, ` +
		`reversible, not destructive of something it should not destroy. You may only TIGHTEN (refuse); ` +
		`the floor never loosens. Reply with ONLY JSON: {"allow":true|false,"reason":"..."}. ` +
		`allow=true keeps it, allow=false refuses it.`
	user := "Proposed action: " + actionText + "\nIs this good to do, or should it be refused? JSON:"
	raw, okCall := b.chat(system, user, "conscience.judge", chatOpts{temperature: f(0.0), salvage: true})
	if !okCall {
		return true, "", false // model deferred/unavailable -> the floor's allow stands (Rule 4)
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("conscience.judge", raw)
		return true, "", false // unparseable -> the floor stands
	}
	return asBool(obj["allow"], true), strings.TrimSpace(asString(obj["reason"])), true
}

// JudgeAcceptance is the goal-Acceptance Pattern-C model CEILING above the deterministic markers (§1.6).
// When the markers are ambiguous, the model judges the outcome: "met" / "unmeetable" / "continue". On a
// deferred/declined/parse-failed call it returns ("",false) — the marker verdict stands. NEVER a stand-in.
func (b *OpenAICompatBackend) JudgeAcceptance(goal string, ctx []types.Thought) (string, bool) {
	system := `You are judging whether a GOAL is DONE, from the thinking so far. Reply with ONLY JSON: ` +
		`{"outcome":"met|unmeetable|continue"}. met=the goal is genuinely satisfied; ` +
		`unmeetable=a constraint makes it infeasible (revise it); continue=not done, keep working.`
	user := "Goal: " + goal + "\nRecent thinking:\n" + joinThoughts(ctx, 6) +
		"\nIs the goal met, unmeetable, or should it continue? JSON:"
	raw, okCall := b.chat(system, user, "acceptance.judge", chatOpts{temperature: f(0.0), salvage: true})
	if !okCall {
		return "", false // model deferred/unavailable -> the marker verdict stands
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("acceptance.judge", raw)
		return "", false // unparseable -> the floor stands
	}
	out := strings.ToLower(strings.TrimSpace(asString(obj["outcome"])))
	switch out {
	case "met", "unmeetable", "continue":
		return out, true
	default:
		return "", false // off-shape -> the floor stands
	}
}

// JudgeEngagement is the awake-engagement Pattern-C model CEILING above the deterministic engagement
// floor (AWAKE-DISP rung 2). The deterministic floor (cognition.RecognizeShape) decided the obvious
// cases: a clearly task-shaped line engages the subconscious, a trivial greeting does not. This judges
// only the flagged-fuzzy MIDDLE — a substantive, non-task-shaped, still-unresolved awake user line —
// "is this worth engaging the subconscious on (a full round-trip), or is it ambient and best answered
// the light way?". It returns "engage" or "quiet". On a deferred/declined/parse-failed/off-shape call it
// returns ("", false) and the caller lets the floor stand (Rule 4: escalation.floor_stands, never
// silent). It NEVER substitutes a deterministic stand-in.
func (b *OpenAICompatBackend) JudgeEngagement(goal, recentContext, floorVerdict string) (string, bool) {
	system := `You are the awake-engagement gate (engagement ESCALATION). The mind is awake and pursuing ` +
		`its own thoughts; a user line just arrived. A deterministic floor has ALREADY decided the obvious ` +
		`cases — a clearly task-shaped request engages a full work effort, a trivial greeting does not — and ` +
		`it left THIS one as a borderline call. Judge ONLY whether it is worth engaging the subconscious on a ` +
		`full round-trip (real work / a multi-step effort) versus noting it lightly and continuing. Reply ` +
		`with ONLY one word: engage, or quiet. engage=worth real work now; quiet=ambient, answer it lightly.`
	user := "User line: " + goal +
		"\nRecent awake context:\n" + recentContext +
		"\nThe deterministic floor graded this " + floorVerdict + "." +
		"\nIs this worth engaging the subconscious on? One word:"
	raw, ok := b.chat(system, user, "engagement.judge", chatOpts{temperature: f(0.0), salvage: true})
	if !ok {
		return "", false // model deferred/unavailable -> the floor stands (Rule 4)
	}
	v := parseEngagementWord(raw)
	if v == "" {
		b.parseFail("engagement.judge", raw)
		return "", false // unparseable -> the floor stands (Rule 4)
	}
	return v, true
}

// JudgeAnswerSupport is the INDEPENDENT-verifier Pattern-C model CEILING (capability-enhancement T2.1)
// above the deterministic web-support floor. The floor already decided the clear cases (the re-retrieved
// evidence lexically contains the answer ⇒ Supported; topical-but-absent ⇒ it left the fuzzy band). This
// judges ONLY that flagged-fuzzy band: given the QUESTION, the candidate ANSWER, and the INDEPENDENTLY
// RE-RETRIEVED EVIDENCE (from the world, NOT the model's own reasoning chain — that is the whole point;
// a same-model re-read of its own chain cannot fix a systematic bias, Huang 2024), does the evidence
// SUPPORT the answer? It must judge AGAINST THE EVIDENCE, not its own prior knowledge (else the
// independence is lost). Returns one of "supported" / "unsupported" / "unverifiable" (ParseVerdict maps
// it). On a deferred/declined/parse-failed/off-shape call it returns ("", false) and the caller lets the
// deterministic floor stand (Rule 4: escalation.floor_stands, never silent). It NEVER substitutes a
// deterministic stand-in.
func (b *OpenAICompatBackend) JudgeAnswerSupport(question, answer, evidence, floorVerdict string) (string, bool) {
	system := `You are an INDEPENDENT answer verifier. A harness is about to commit an answer to a ` +
		`question, and has RE-RETRIEVED outside evidence to check it. Judge ONLY whether the PROVIDED ` +
		`EVIDENCE supports the proposed answer. Rely on the evidence, NOT your own prior knowledge — if the ` +
		`evidence does not establish the answer, it is not supported even if you believe the answer is ` +
		`correct. Reply with ONLY one word: supported, unsupported, or unverifiable. ` +
		`supported=the evidence corroborates the answer; unsupported=the evidence is on-topic but does NOT ` +
		`establish the answer (or contradicts it); unverifiable=the evidence is too thin/off-topic to judge.`
	user := "Question: " + question +
		"\nProposed answer: " + answer +
		"\nIndependently re-retrieved evidence:\n" + evidence +
		"\nThe deterministic floor graded this " + floorVerdict + "." +
		"\nDoes the evidence support the answer? One word:"
	raw, ok := b.chat(system, user, "answer_support.judge", chatOpts{temperature: f(0.0), salvage: true})
	if !ok {
		return "", false // model deferred/unavailable -> the floor stands (Rule 4)
	}
	v := parseSupportWord(raw)
	if v == "" {
		b.parseFail("answer_support.judge", raw)
		return "", false // unparseable -> the floor stands (Rule 4)
	}
	return v, true
}

// parseSupportWord extracts the answer-support verdict word from a model reply (case-insensitive,
// substring-tolerant). "" on no match. "unsupported" is tested before "supported" (it CONTAINS the
// substring "supported"), and "unverifiable" first, so a non-committal reply lands on the conservative
// side — the floor stands rather than a false "supported" committing an unchecked answer.
func parseSupportWord(raw string) string {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "unverifiable"):
		return "unverifiable"
	case strings.Contains(low, "unsupported"):
		return "unsupported"
	case strings.Contains(low, "supported"):
		return "supported"
	default:
		return ""
	}
}

// parseEngagementWord extracts the engagement verdict word from a model reply (case-insensitive,
// substring-tolerant). "" on no match. "quiet" is tested first so a reply that mentions both lands quiet
// (the conservative direction — when the model is non-committal the floor's no-engage stays).
func parseEngagementWord(raw string) string {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "quiet"):
		return "quiet"
	case strings.Contains(low, "engage"):
		return "engage"
	default:
		return ""
	}
}

// Respond synthesises the user-facing answer from the resolved thought graph (the Action-layer respond).
func (b *OpenAICompatBackend) Respond(goal string, ctx []types.Thought) string {
	system, user := PromptRespond(goal, ctx)
	if b.personaFragment != "" { // P7.3: learned user adaptation, outward-facing surface only
		system += "\n" + b.personaFragment
	}
	if b.groundCompleteFragment != "" { // THOUGHT_GROUND_COMPLETE: grounding-completeness reading directive
		system += "\n\n" + b.groundCompleteFragment
	}
	out, ok := b.chat(system, user, "action.respond", chatOpts{temperature: f(0.4), salvage: true})
	if !ok {
		return "" // OUTPUT role: surface the gap; the caller speaks an honest "unavailable", not a template
	}
	return out
}

// OperatorApply lets a runtime sub-agent apply ONE operator to the active line — its scoped move.
func (b *OpenAICompatBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	system, user := PromptOperatorApply(role, responsibility, domain, goal, ctx)
	out, ok := b.chat(system, user, "operator."+role, chatOpts{temperature: f(0.4), salvage: false})
	if !ok {
		// CONTENT role (Pattern B): surface the gap, never a substituted template. The caller
		// decides the typed surface — fireReason re-derives "[role] intent" (the operator's own
		// declared metadata, not manufactured intelligence); the concretize-fusion path keeps the
		// raw un-fused candidate (Concretize: text=="" ⇒ keep the raw). So the gap is surfaced in
		// ONE place (the caller), not faked here.
		return ""
	}
	return out
}

// EmitVerdict is the decision-CONCLUSION role (A2): after the worker reasoned, state the final
// verdict in the fixed `VERDICT: <label>` shape. CONTENT role (Pattern B): on model decline it
// surfaces the gap (returns "") — never a substituted verdict — so the caller's parseVerdictLine
// finds no line and the present-rate guard sees the disobedience.
func (b *OpenAICompatBackend) EmitVerdict(worker, goal string, optionLabels []string, priorReasoning string) string {
	system, user := PromptEmitVerdict(worker, goal, optionLabels, priorReasoning)
	out, ok := b.chat(system, user, "emit_verdict", chatOpts{temperature: f(0.2), salvage: true})
	if !ok {
		return ""
	}
	return out
}

// SynthesizeProgram is the toolmaker path: WRITE the workflow program for this goal, and define
// any new operators it needs. Returns the parsed dict + true, or (nil, false) to defer.
func (b *OpenAICompatBackend) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	ops := strings.Join(opNames, ", ")
	system := "You are the control planner of a thinking system. Given a goal, design a workflow as a " +
		"control-flow tree of cognitive operators. Node kinds: " +
		`{"kind":"step","operator":<name>,"domain":<rough domain>}, ` +
		`{"kind":"seq","children":[...]}, {"kind":"par","children":[...]} (do these at once), ` +
		`{"kind":"loop","body":<node>,"until":<text>,"max_iter":<=6}. ` +
		"Prefer operators from this catalog: " + ops + ". If a genuinely new move is needed, define it " +
		`under "operators":[{"name","family":"transformative|relational|generative","intent"}]. ` +
		"Keep it small (<=8 steps). If the goal is a simple question needing no workflow, return " +
		`{"program":null}. Output ONLY JSON: {"operators":[...]?,"program":<node|null>,"rationale":<text>}.`
	user := "Goal: " + goal + "\nContext:\n" + joinThoughts(ctx, 4) + "\nProgram JSON:"
	// validate=parsesAsObject: a half-written JSON program truncated at the budget would otherwise
	// return ok and then fail loadsObject below → control-floor fallback → no read-workflow → grounding
	// hallucinates. The truncated-INVALID retry gives it a bigger budget first.
	raw, ok := b.chat(system, user, "synthesize_program", chatOpts{temperature: f(0.2), salvage: true, validate: parsesAsObject})
	if !ok {
		return nil, false
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("synthesize_program", raw)
		return nil, false
	}
	if v, present := obj["program"]; !present || v == nil {
		return nil, false
	}
	return obj, true
}

// ---------------------------------------------------------------------------
// optional capability interfaces (Decider / Intender / SpecialistCaller)
// ---------------------------------------------------------------------------

// Decide is the Controller's executive decision via the model — choose the next cognitive move.
// Returns (choice, why): choice=="" is Python's None (model declined / off-list). The model's
// reasoning is CAPTURED (P6), not discarded. Note: the engine wires a DecideState before the
// call; here we accept the minimal (goal, ctx, options) interface and surface no situation notes
// when none are supplied (the engine-side variant lives in DecideWithState).
func (b *OpenAICompatBackend) Decide(goal string, ctx []types.Thought, options []string) (choice, why string) {
	return b.DecideWithState(goal, ctx, options, nil)
}

// DecideState mirrors the Python `state` dict surfaced to the model (the same machine-computed
// situation the heuristic sees, never the heuristic's own choice). nil → no situation line.
type DecideState struct {
	Conflict  bool
	Flagged   bool
	Exhausted bool
	Acted     bool
}

// moveDescriptions describes ONLY the allowed moves — a reasoning model over-deliberates when
// handed options it cannot pick, blowing its token budget; keep the menu tight.
var moveDescriptions = map[string]string{
	"THINK":     "THINK=continue this line.",
	"BRANCH":    "BRANCH=fork (candidates conflict or an alternative is worth exploring).",
	"MERGE":     "MERGE=two lines are the same point.",
	"BACKTRACK": "BACKTRACK=this line is spent, return to a better stashed one.",
	"ACT":       "ACT=you are stuck and must open to reality to import ground truth.",
	"STOP":      "STOP=the goal is met or it's hopeless.",
}

// DecideWithState is the full Controller.decide port (the engine surfaces the situation state).
func (b *OpenAICompatBackend) DecideWithState(goal string, ctx []types.Thought, options []string, state *DecideState) (choice, why string) {
	var moves strings.Builder
	for _, o := range options {
		if d, ok := moveDescriptions[o]; ok {
			if moves.Len() > 0 {
				moves.WriteByte(' ')
			}
			moves.WriteString(d)
		}
	}
	system := "You are the Controller of a thinking system; choose the SINGLE best next move. " +
		moves.String() +
		" Decide promptly; do not over-deliberate. " +
		`Reply with ONLY JSON: {"decision":"<one of the allowed>","why":"<one sentence>"}.`
	// Informed but NOT anchored: surface the same machine-computed situation the heuristic uses.
	var notes []string
	if state != nil {
		if state.Conflict {
			notes = append(notes, "the latest specialists returned CONFLICTING verdicts")
		}
		if state.Flagged {
			notes = append(notes, "the latest injection is FLAGGED as low-confidence / not yet trusted")
		}
		if state.Exhausted {
			notes = append(notes, "this line of thought appears exhausted")
		}
		if state.Acted {
			notes = append(notes, "you have already opened to reality on this line")
		}
	}
	situation := ""
	if len(notes) > 0 {
		situation = "Situation: " + strings.Join(notes, "; ") + ".\n"
	}
	user := "Goal: " + goal + "\nRecent thoughts:\n" + joinThoughts(ctx, 6) + "\n" + situation +
		"Allowed moves: " + strings.Join(options, ", ") + "\nYour decision JSON:"
	raw, ok := b.chat(system, user, "Controller.decide", chatOpts{temperature: f(0.0), salvage: true})
	if !ok {
		return "", ""
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("Controller.decide", raw)
		return "", ""
	}
	d := strings.ToUpper(strings.TrimSpace(asString(obj["decision"])))
	whyStr := strings.TrimSpace(asString(obj["why"]))
	if !contains(options, d) {
		return "", "" // off-list → Python None
	}
	return d, whyStr
}

// Intention distils the active branch into the single concrete world-action to take (the watched
// seam). Returns (text, kind, ok); ok=false is Python's None (fall back to the regex router).
func (b *OpenAICompatBackend) Intention(goal string, ctx []types.Thought) (text, kind string, ok bool) {
	system, user := PromptIntention(goal, ctx)
	raw, granted := b.chat(system, user, "form_intention", chatOpts{temperature: f(0.0), salvage: true, validate: parsesAsObject})
	if !granted {
		return "", "", false
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("form_intention", raw)
		return "", "", false
	}
	k := strings.ToLower(strings.TrimSpace(asString(obj["kind"])))
	if k != "run" && k != "send" && k != "measure" {
		k = "run"
	}
	t := strings.TrimSpace(asString(obj["text"]))
	if t == "" {
		return "", "", false
	}
	return t, k, true
}

// PrimitiveSubAgent is a domain-scoped sub-agent call: the {domain} specialist reads the stream and
// contributes one short observation from its domain. Returns (output, ok); ok=false is Python's
// None (fall back to the deterministic operator application).
func (b *OpenAICompatBackend) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	system, user := PromptSpecialist(domain, description, ctx)
	return b.chat(system, user, "specialist."+domain, chatOpts{temperature: f(0.6), salvage: false})
}

// Comprehend is the LLM "to_operator" step (backends.RealityComprehender): ONE structured call that
// translates the live thinking into the reality observation it needs now — the capability AND the
// concrete target. This is the unification that replaces the per-capability recognizers (it was making
// a separate yes/no call per primitive) AND the regex target extraction: the AGENT names what to observe
// and on WHAT (the file path / pattern / command it INTENDS — including a path it just self-corrected to).
// JSON role {"need":"read|search|run|none","target":"..."}, salvaged + truncation-retried. ok=false ⇒
// declined / no usable answer ⇒ the caller keeps the keyword-trigger + regex floor.
func (b *OpenAICompatBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	system := "You translate the current thinking into the ONE observation of reality it needs RIGHT NOW, " +
		"if any, and its concrete target:\n" +
		"  read   - it must READ a file; target = the exact file path to read.\n" +
		"  search - it must SEARCH the tree; target = the pattern/identifier to find.\n" +
		"  run    - it must RUN the tests/execute; target = the command (empty = the suite).\n" +
		"  none   - it does NOT need to observe reality this step (a plan, a hedge, pure reasoning, or it " +
		"already has what it needs).\n" +
		"Use the path/pattern the thinking INTENDS — if it just corrected the path (e.g. to config/x.yaml), " +
		"use THAT. If the thinking NAMES a file it should read/consult, or says it needs to read/access/open " +
		"a specific file — EVEN if it claims it lacks access or asks for a permission/connector/upload — that " +
		"IS a 'read' with that file as the target. YOU are the read capability; the file is already on disk, " +
		"so never answer 'none' just because the thinking thinks it cannot reach the file. A source named but " +
		"not yet quoted has NOT been read yet.\n" +
		`Output ONLY JSON: {"need":"read|search|run|none","target":"..."}.`
	user := "Thinking:\n" + joinThoughts(ctx, 6) + "\n\nThe one reality observation it needs now (JSON):"
	raw, granted := b.chat(system, user, "comprehend",
		chatOpts{temperature: f(0.0), salvage: true, validate: parsesAsObject})
	if !granted {
		return "", "", false
	}
	obj, err := loadsObject(raw)
	if err != nil {
		b.parseFail("comprehend", raw)
		return "", "", false
	}
	need = strings.ToLower(strings.TrimSpace(asString(obj["need"])))
	target = strings.TrimSpace(asString(obj["target"]))
	switch need {
	case "read", "search", "run", "none": // a recognized capability
	default:
		need = "none"
	}
	return need, target, true
}

// FormalizeExpression is the 5th-axis classical solver's Pattern-B formalization step
// (backends.StructureFormalizer; the specialized-component-registry-axis §5). The model writes ONLY the
// expression SHAPE (operators + named placeholders a,b,c,...) plus the ordered operand names with a
// one-line description; the SolverPrimitiveSubAgent then binds each declared operand POSITIONALLY to a grounded
// read and a math/big evaluator computes EXACTLY. THE HARD CONTRACT: the shape must carry NO numeric
// literal (the AST validator hard-rejects one; we also pre-reject defensively here so the dark/ok=false
// path is exercised cleanly and a number never reaches the binder). It is a JSON role, salvaged +
// truncation-retried (a reasoning model that leaves content empty has the answer in its reasoning trace;
// a half-written JSON shape gets a bigger budget before falling through). ok=false ⇒ declined / no model /
// off-shape / NONE / a literal-bearing shape ⇒ the specialist fires NOTHING (no deterministic stand-in
// number — that would be the manufactured intelligence the safety boundary exists to forbid).
func (b *OpenAICompatBackend) FormalizeExpression(ctx []types.Thought) (expr string, operands []string, ok bool) {
	system, user := PromptFormalizeExpression(ctx)
	raw, granted := b.chat(system, user, "solver.formalize",
		chatOpts{temperature: f(0.0), salvage: true, validate: parsesAsObject})
	if !granted {
		return "", nil, false // model deferred/unavailable ⇒ the specialist fires nothing
	}
	expr, operands, parsed := parseFormalization(raw)
	if !parsed {
		// NONE / off-shape / a smuggled literal / no operands ⇒ fire nothing. We do NOT call parseFail
		// here: a NONE is the EXPECTED dark-path answer on a non-compute task, not a model malfunction —
		// flagging it as a parse failure would be misleading noise. The solver simply stays dark.
		return "", nil, false
	}
	return expr, operands, true
}

// containsDigit reports whether s carries any decimal digit — the defensive pre-check for the
// no-literal contract (the AST validator is the hard reject; this surfaces the same gap as a clean
// ok=false at the role boundary so a literal-bearing shape never reaches the binder).
func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// parseFormalization robustly extracts the expression SHAPE + the ordered operand names from the
// model's reply. It tolerates the model's formatting (code fences / preamble) via loadsObject's lenient
// outermost-{...} slice. It returns parsed=false (the solver fires nothing) when:
//
//   - there is no JSON object, or expr is missing/empty;
//   - expr is the NONE sentinel (any case, with surrounding quotes/whitespace) ⇒ a non-compute task;
//   - expr carries a numeric LITERAL (the hard no-numbers rule — caught here AND by the AST validator);
//   - no operand names can be recovered (operands declared as objects {name,desc} OR a bare string list).
//
// operands are returned in the model's DECLARED order (the SolverPrimitiveSubAgent binds them positionally to
// the grounded reads in reading order), de-duplicated, lower-cased to match the AST operand identifiers.
func parseFormalization(raw string) (expr string, operands []string, ok bool) {
	obj, err := loadsObject(raw)
	if err != nil {
		return "", nil, false
	}
	expr = strings.TrimSpace(asString(obj["expr"]))
	// NONE sentinel (the explicit dark path) — tolerate quoting/case/whitespace and a few synonyms a
	// model reaches for. An empty/null expr is the same dark path.
	switch strings.ToUpper(strings.Trim(expr, `"'`)) {
	case "", "NONE", "NULL", "N/A", "NA":
		return "", nil, false
	}
	// THE HARD RULE (defensive): a numeric literal anywhere in the shape ⇒ fire nothing. The AST
	// validator (parseSolverExpr) is the load-bearing reject; catching it here keeps a number out of the
	// binder and gives the role boundary a clean ok=false.
	if containsDigit(expr) {
		return "", nil, false
	}
	operands = parseOperandNames(obj["operands"])
	if len(operands) == 0 {
		return "", nil, false // no operands recovered ⇒ nothing to bind ⇒ fire nothing
	}
	return expr, operands, true
}

// parseOperandNames recovers the ordered operand names from the decoded "operands" value, tolerating BOTH
// shapes the contract / a model may produce: a list of objects [{"name":"a","desc":...}, ...] (the
// documented shape) OR a bare string list ["a","b","c"]. Names are trimmed and de-duplicated in first-seen
// order; their CASE is preserved verbatim — the downstream bind matches a declared operand against the
// identifier the AST collected from the SAME expr (parseSolverExpr is case-as-written), so lower-casing
// here would skew "a" against an expr that wrote "A" and spuriously reject a valid shape. A non-list /
// empty value yields nil (the caller fires nothing).
func parseOperandNames(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, el := range arr {
		switch x := el.(type) {
		case string:
			add(x)
		case map[string]any:
			add(asString(x["name"]))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// compile-time interface checks
// ---------------------------------------------------------------------------

var (
	_ backends.Backend                = (*OpenAICompatBackend)(nil)
	_ backends.StructureFormalizer    = (*OpenAICompatBackend)(nil)
	_ backends.Decider                = (*OpenAICompatBackend)(nil)
	_ backends.Intender               = (*OpenAICompatBackend)(nil)
	_ backends.SpecialistCaller       = (*OpenAICompatBackend)(nil)
	_ backends.RealityComprehender    = (*OpenAICompatBackend)(nil)
	_ backends.FilterEscalator        = (*OpenAICompatBackend)(nil)
	_ backends.SufficiencyJudge       = (*OpenAICompatBackend)(nil)
	_ backends.EmitBinder             = (*OpenAICompatBackend)(nil)
	_ backends.SchedulerBinder        = (*OpenAICompatBackend)(nil)
	_ backends.DisplayNamer           = (*OpenAICompatBackend)(nil)
	_ backends.LegiblePrompter        = (*OpenAICompatBackend)(nil)
	_ backends.PersonaPrompter        = (*OpenAICompatBackend)(nil)
	_ backends.GroundCompletePrompter = (*OpenAICompatBackend)(nil)
)

// f returns a pointer to a float64 literal (for the chatOpts temperature override).
func f(v float64) *float64 { return &v }

// min returns the smaller of two durations (Go 1.21+ has the builtin, but spell it out for clarity).
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// envOr returns the env value for key or def.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
