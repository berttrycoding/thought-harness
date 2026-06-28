package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// These tests drive the reasoning-model robustness path OFFLINE with an httptest.Server returning
// crafted OpenAI-compatible responses — NO real model. They cover: (a) content-only passthrough,
// (b) salvage-from-reasoning when content is empty, (c) retry-on-truncation succeeding on the second
// response, (d) retry budget exhausted → the gap surfaced honestly (Pattern B) + the observability
// emitted, and the MaxTokens default + override precedence.

// fakeChat is a scripted /v1/chat/completions server: each request pops the next response off the
// queue (it records the request bodies so a test can assert the budget grew on a retry). The last
// scripted response repeats if the queue is exhausted. Thread-safe (the client is single-threaded
// here, but be safe).
type fakeChat struct {
	t         *testing.T
	mu        sync.Mutex
	responses []string         // raw JSON response bodies, served in order
	idx       int              // next response index
	reqBodies []map[string]any // recorded request bodies (for budget assertions)
	server    *httptest.Server
	modelID   string // id reported on /models (Health/autodetect); "" → "fake-reasoner"
}

func newFakeChat(t *testing.T, responses ...string) *fakeChat {
	fc := &fakeChat{t: t, responses: responses}
	fc.server = httptest.NewServer(http.HandlerFunc(fc.handle))
	t.Cleanup(fc.server.Close)
	return fc
}

func (fc *fakeChat) handle(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/models") {
		// Health()/autodetect probe.
		id := fc.modelID
		if id == "" {
			id = "fake-reasoner"
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"`+id+`"}]}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	fc.mu.Lock()
	fc.reqBodies = append(fc.reqBodies, parsed)
	resp := fc.responses[len(fc.responses)-1]
	if fc.idx < len(fc.responses) {
		resp = fc.responses[fc.idx]
		fc.idx++
	}
	fc.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, resp)
}

// requestMaxTokens returns the max_tokens of the nth recorded request (0-based).
func (fc *fakeChat) requestMaxTokens(n int) int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if n >= len(fc.reqBodies) {
		fc.t.Fatalf("no request #%d recorded (only %d)", n, len(fc.reqBodies))
	}
	v, _ := fc.reqBodies[n]["max_tokens"].(float64)
	return int(v)
}

func (fc *fakeChat) requestCount() int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return len(fc.reqBodies)
}

// chatResponse builds an OpenAI-compatible response body with the given content + reasoning_content
// + finish_reason + optional reasoning_tokens (nil = omit the usage detail).
func chatResponse(content, reasoning, finish string, reasoningTokens *int) string {
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	choice := map[string]any{"message": msg, "finish_reason": finish}
	body := map[string]any{"choices": []any{choice}}
	if reasoningTokens != nil {
		body["usage"] = map[string]any{
			"completion_tokens_details": map[string]any{"reasoning_tokens": *reasoningTokens},
		}
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// chatResponseField builds a response that puts the reasoning trace in an ARBITRARY field (for the
// multi-provider field-list test): reasoning / thinking instead of reasoning_content.
func chatResponseField(field, reasoning, finish string) string {
	msg := map[string]any{"role": "assistant", "content": "", field: reasoning}
	choice := map[string]any{"message": msg, "finish_reason": finish}
	body := map[string]any{"choices": []any{choice}}
	b, _ := json.Marshal(body)
	return string(b)
}

// newTestBackend builds an OpenAICompatBackend pointed at the fake server, with an emit recorder.
func newTestBackend(t *testing.T, fc *fakeChat, opts Options) (*OpenAICompatBackend, *[]events.Event) {
	opts.BaseURL = fc.server.URL + "/v1"
	opts.Timeout = 2 * time.Second
	be := NewOpenAICompat(opts)
	var emitted []events.Event
	be.BindEmit(func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		emitted = append(emitted, ev)
		return ev
	})
	return be, &emitted
}

func ctxR() []types.Thought {
	return []types.Thought{{ID: 1, Text: "should I rewrite the parser?", Source: types.GENERATED}}
}

// (a) content-only response → returned as-is, no salvage, no retry.
func TestReasoningContentOnlyPassthrough(t *testing.T) {
	fc := newFakeChat(t, chatResponse("the parser is fine as-is", "", "stop", nil))
	be, emitted := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	got := be.Generate("goal", ctxR(), cpyrand.New(0))
	if got != "the parser is fine as-is" {
		t.Fatalf("content-only Generate = %q, want the content verbatim", got)
	}
	if fc.requestCount() != 1 {
		t.Errorf("content-only should make exactly 1 request, made %d", fc.requestCount())
	}
	// Exactly one llm.call, with salvage_used=false / retry_count=0.
	rec, ok := be.Log.last()
	if !ok || rec.SalvageUsed || rec.RetryCount != 0 {
		t.Errorf("content-only call record = %+v, want salvage_used=false retry_count=0", rec)
	}
	if got := lastLLMEvent(*emitted); got == nil {
		t.Fatal("no llm.call event emitted for a successful content-only call")
	} else if got.Data["salvage_used"] != false || got.Data["retry_count"] != 0 {
		t.Errorf("llm.call data = %v, want salvage_used=false retry_count=0", got.Data)
	}
}

// (b) empty content + reasoning_content with a clear answer → salvaged (the answer mined out of the
// reasoning trace; salvage_used=true). This is a CONTENT role (Respond) which enables salvage.
func TestReasoningSalvageFromReasoning(t *testing.T) {
	reasoning := "Let me think. The user asked X. I'll weigh the options.\n\n" +
		"Final answer: yes, the rewrite is worth it."
	fc := newFakeChat(t, chatResponse("", reasoning, "stop", nil))
	be, emitted := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	got := be.Respond("goal", ctxR())
	if !strings.Contains(got, "yes, the rewrite is worth it") {
		t.Fatalf("salvaged Respond = %q, want the final conclusion mined out of the reasoning", got)
	}
	rec, _ := be.Log.last()
	if !rec.SalvageUsed {
		t.Errorf("salvage path should set salvage_used=true, got %+v", rec)
	}
	if ev := lastLLMEvent(*emitted); ev == nil || ev.Data["salvage_used"] != true {
		t.Errorf("llm.call should carry salvage_used=true, got %v", evData(ev))
	}
}

// (b') the JSON-role salvage: a JSON role (Controller.decide) with empty content but the JSON object
// sitting in the reasoning trace is salvaged + parsed.
func TestReasoningSalvageJSONRole(t *testing.T) {
	reasoning := "I should keep thinking on this line, nothing is conflicting.\n" +
		`{"decision":"THINK","why":"the line is still productive"}`
	fc := newFakeChat(t, chatResponse("", reasoning, "stop", nil))
	be, _ := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	choice, why := be.Decide("goal", ctxR(), []string{"THINK", "STOP"})
	if choice != "THINK" {
		t.Fatalf("salvaged JSON decision = %q, want THINK (parsed out of the reasoning trace)", choice)
	}
	if !strings.Contains(why, "productive") {
		t.Errorf("salvaged why = %q, want the model's reason", why)
	}
}

// (c) finish_reason=length + empty content, THEN a second response with content → retry succeeds, and
// the retry uses a LARGER budget (2x by default). retry_count==1.
func TestReasoningRetryOnTruncationSucceeds(t *testing.T) {
	truncated := chatResponse("", "thinking thinking thinking and never finishing", "length", nil)
	full := chatResponse("the rewrite is worth it after all", "", "stop", nil)
	fc := newFakeChat(t, truncated, full)
	// salvage OFF so the truncated reasoning does NOT salvage — forces the retry path. Generate is a
	// narrative role (salvage off by default) so this is realistic.
	be, emitted := newTestBackend(t, fc, Options{Model: "fake-reasoner", MaxTokens: 1000})

	got := be.Generate("goal", ctxR(), cpyrand.New(0))
	if got != "the rewrite is worth it after all" {
		t.Fatalf("retry Generate = %q, want the second (full) response", got)
	}
	if fc.requestCount() != 2 {
		t.Fatalf("retry-on-truncation should make 2 requests, made %d", fc.requestCount())
	}
	// The retry budget GREW 2x (1000 → 2000).
	if mt0, mt1 := fc.requestMaxTokens(0), fc.requestMaxTokens(1); mt0 != 1000 || mt1 != 2000 {
		t.Errorf("budgets = (%d, %d), want (1000, 2000) — retry should 2x the budget", mt0, mt1)
	}
	rec, _ := be.Log.last()
	if rec.RetryCount != 1 {
		t.Errorf("retry_count = %d, want 1", rec.RetryCount)
	}
	if ev := lastLLMEvent(*emitted); ev == nil || ev.Data["retry_count"] != 1 {
		t.Errorf("llm.call should carry retry_count=1, got %v", evData(ev))
	}
}

// (d) retry budget exhausted (both responses truncated-empty) → the gap surfaces honestly (empty for
// a CONTENT role, Pattern B) and one llm.fallback is emitted; the call record carries the
// observability (finish_reason=length, retry_count=1).
func TestReasoningRetryExhaustedSurfacesGap(t *testing.T) {
	truncated := chatResponse("", "endless reasoning", "length", nil)
	fc := newFakeChat(t, truncated, truncated, truncated)
	be, emitted := newTestBackend(t, fc, Options{Model: "fake-reasoner", MaxTokens: 1000})

	got := be.Generate("goal", ctxR(), cpyrand.New(0)) // CONTENT role
	if got != "" {
		t.Fatalf("exhausted Generate (content role) should surface the gap (\"\"), got %q", got)
	}
	// 1 initial + 1 retry (MaxRetries default 1) = 2 requests, then it stops (bounded).
	if fc.requestCount() != 2 {
		t.Errorf("bounded retry should make exactly 2 requests (1 + 1 retry), made %d", fc.requestCount())
	}
	rec, _ := be.Log.last()
	if rec.OK {
		t.Error("exhausted call record should be OK=false")
	}
	if rec.FinishReason != "length" {
		t.Errorf("exhausted finish_reason = %q, want length (observable noise source)", rec.FinishReason)
	}
	if rec.RetryCount != 1 {
		t.Errorf("exhausted retry_count = %d, want 1 (one retry spent before giving up)", rec.RetryCount)
	}
	// Exactly one llm.fallback (the gap surfaced honestly, never a substituted template).
	var fallbacks int
	for _, ev := range *emitted {
		if ev.Kind == events.LLMFallback {
			fallbacks++
		}
	}
	if fallbacks != 1 {
		t.Errorf("expected exactly 1 llm.fallback on the exhausted gap, got %d", fallbacks)
	}
}

// (e) reasoning_tokens from usage.completion_tokens_details flows to the call record + event.
func TestReasoningTokensObservability(t *testing.T) {
	rt := 812
	fc := newFakeChat(t, chatResponse("done", "", "stop", &rt))
	be, emitted := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	_ = be.Generate("goal", ctxR(), cpyrand.New(0))
	rec, _ := be.Log.last()
	if rec.ReasoningTokens != 812 {
		t.Errorf("reasoning_tokens in call record = %d, want 812", rec.ReasoningTokens)
	}
	if ev := lastLLMEvent(*emitted); ev == nil || ev.Data["reasoning_tokens"] != 812 {
		t.Errorf("llm.call should carry reasoning_tokens=812, got %v", evData(ev))
	}
}

// (f) the multi-provider field list: a trace in `reasoning` (not reasoning_content) salvages by
// default (defaultReasoningFields includes it), and a configured single-field list that EXCLUDES the
// present field does NOT salvage.
func TestReasoningMultiProviderFieldList(t *testing.T) {
	// Default field list includes "reasoning" → salvages.
	fc := newFakeChat(t, chatResponseField("reasoning", "Final answer: ship it.", "stop"))
	be, _ := newTestBackend(t, fc, Options{Model: "fake-reasoner"})
	if got := be.Respond("goal", ctxR()); !strings.Contains(got, "ship it") {
		t.Errorf("default field list should salvage from `reasoning`, got %q", got)
	}

	// A field list that does NOT include `thinking` → the trace in `thinking` is invisible → gap.
	fc2 := newFakeChat(t, chatResponseField("thinking", "Final answer: ship it.", "stop"))
	be2, _ := newTestBackend(t, fc2, Options{
		Model:     "fake-reasoner",
		Reasoning: ReasoningOptions{Fields: []string{"reasoning_content"}},
	})
	if got := be2.Respond("goal", ctxR()); got != "" {
		t.Errorf("a field list excluding `thinking` should NOT salvage it, got %q", got)
	}
}

// (g) SalvageFromReasoning=false (explicit) disables salvage even on a CONTENT role → gap surfaced.
func TestReasoningSalvageDisabled(t *testing.T) {
	fc := newFakeChat(t, chatResponse("", "Final answer: yes.", "stop", nil))
	be, _ := newTestBackend(t, fc, Options{
		Model:     "fake-reasoner",
		Reasoning: ReasoningOptions{}.WithSalvage(false),
	})
	if got := be.Respond("goal", ctxR()); got != "" {
		t.Errorf("salvage disabled should surface the gap, got %q", got)
	}
}

// (h) RetryOnTruncation=false (explicit) → no retry even on a truncated-empty completion.
func TestReasoningRetryDisabled(t *testing.T) {
	truncated := chatResponse("", "endless", "length", nil)
	full := chatResponse("would have salvaged on retry", "", "stop", nil)
	fc := newFakeChat(t, truncated, full)
	be, _ := newTestBackend(t, fc, Options{
		Model:     "fake-reasoner",
		Reasoning: ReasoningOptions{}.WithRetry(false),
	})
	if got := be.Generate("goal", ctxR(), cpyrand.New(0)); got != "" {
		t.Errorf("retry disabled should surface the gap on truncation, got %q", got)
	}
	if fc.requestCount() != 1 {
		t.Errorf("retry disabled should make exactly 1 request, made %d", fc.requestCount())
	}
}

// (i) the budget cap bounds the retry growth: with a small cap, the grown budget is capped, and once
// at the cap a further retry does not exceed it.
func TestReasoningRetryBudgetCap(t *testing.T) {
	truncated := chatResponse("", "endless", "length", nil)
	full := chatResponse("ok", "", "stop", nil)
	fc := newFakeChat(t, truncated, full)
	be, _ := newTestBackend(t, fc, Options{
		Model:     "fake-reasoner",
		MaxTokens: 1000,
		Reasoning: ReasoningOptions{RetryMaxTokensCap: 1200, RetryMaxTokensMul: 2.0}.WithMaxRetries(1),
	})
	_ = be.Generate("goal", ctxR(), cpyrand.New(0))
	// 1000 * 2 = 2000, capped at 1200.
	if mt1 := fc.requestMaxTokens(1); mt1 != 1200 {
		t.Errorf("retry budget = %d, want 1200 (capped), not 2000", mt1)
	}
}

// (j) MaxTokens default is 8192; an explicit option AND THOUGHT_LLM_MAX_TOKENS env override win.
func TestReasoningMaxTokensDefaultAndOverride(t *testing.T) {
	if be := NewOpenAICompat(Options{}); be.MaxTokens != defaultMaxTokens {
		t.Errorf("default MaxTokens = %d, want %d (8192)", be.MaxTokens, defaultMaxTokens)
	}
	if defaultMaxTokens != 8192 {
		t.Errorf("defaultMaxTokens = %d, want 8192 (reasoning + structured-payload headroom)", defaultMaxTokens)
	}
	// Explicit option wins over env+default.
	t.Setenv("THOUGHT_LLM_MAX_TOKENS", "5000")
	if be := NewOpenAICompat(Options{MaxTokens: 7777}); be.MaxTokens != 7777 {
		t.Errorf("explicit MaxTokens option = %d, want 7777 (option beats env)", be.MaxTokens)
	}
	// Env wins over the default when no option is set.
	if be := NewOpenAICompat(Options{}); be.MaxTokens != 5000 {
		t.Errorf("env MaxTokens = %d, want 5000 (env beats default)", be.MaxTokens)
	}
}

// (j2) truncated-INVALID retry: a STRUCTURED role (synthesize_program) that comes back
// finish_reason=length with partial UNPARSEABLE JSON must retry with a grown budget — NOT fall
// straight to the control floor (the truncation→hallucination bug). The retry then parses + succeeds.
func TestReasoningRetryOnTruncatedInvalidStructured(t *testing.T) {
	// 1st: truncated mid-JSON (has '{' but never closes) → parsesAsObject=false, finish=length.
	truncated := chatResponse(`{"program": {"kind":"step","operator":"rea`, "", "length", nil)
	// 2nd (bigger budget): complete, valid program JSON.
	full := chatResponse(`{"program":{"kind":"step","operator":"read","domain":"code"},"rationale":"read the file"}`, "", "stop", nil)
	fc := newFakeChat(t, truncated, full)
	be, _ := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	obj, ok := be.SynthesizeProgram("read config/limits.go", ctxR(), []string{"read"})
	if !ok {
		t.Fatal("SynthesizeProgram should SUCCEED after the truncated-invalid retry, got ok=false (fell to the floor)")
	}
	if obj["program"] == nil {
		t.Fatalf("program should be present after retry, got %v", obj)
	}
	if fc.requestCount() != 2 {
		t.Fatalf("truncated-invalid should trigger ONE retry (2 requests), made %d", fc.requestCount())
	}
	if a, b := fc.requestMaxTokens(0), fc.requestMaxTokens(1); b <= a {
		t.Errorf("retry budget should GROW: req0 max_tokens=%d, req1=%d (want req1>req0)", a, b)
	}
}

// (j3) a truncated-invalid response with NO retry budget left surfaces the floor honestly (ok=false)
// rather than looping — the retry is bounded.
func TestReasoningRetryTruncatedInvalidBounded(t *testing.T) {
	truncated := chatResponse(`{"program": {"kind":"step","operator":"rea`, "", "length", nil)
	fc := newFakeChat(t, truncated, truncated, truncated) // every attempt truncates
	be, _ := newTestBackend(t, fc, Options{Model: "fake-reasoner"})

	if _, ok := be.SynthesizeProgram("goal", ctxR(), []string{"read"}); ok {
		t.Error("SynthesizeProgram should fail (ok=false) when every attempt truncates")
	}
	if n := fc.requestCount(); n != 2 { // initial + 1 bounded retry (defaultMaxRetries=1)
		t.Errorf("bounded retry: want 2 requests (initial + 1), made %d", n)
	}
}

// (j4) the MODEL-ESCALATION TIER (Item 3): a STRUCTURED role (synthesize_program) that STILL
// truncates-invalid AFTER the primary's bounded retry exhausts at the max budget escalates to the
// configured UTILITY backend, which succeeds + parses. The escalation is wired by NewTiered (it points
// the primary's structuredEscalate hook at the utility's escalateStructured). It is BOUNDED to one
// escalation call.
func TestModelEscalationTierStructuredSucceeds(t *testing.T) {
	// PRIMARY: every attempt comes back truncated mid-JSON (finish=length, never closes the object) — so
	// the bounded retry exhausts at the max budget without ever parsing.
	truncated := chatResponse(`{"program": {"kind":"step","operator":"rea`, "", "length", nil)
	primaryFC := newFakeChat(t, truncated, truncated, truncated, truncated)
	primary, _ := newTestBackend(t, primaryFC, Options{Model: "fake-primary", MaxTokens: 1000})

	// UTILITY (the escalation tier): returns a complete, valid program JSON on its single attempt.
	full := chatResponse(`{"program":{"kind":"step","operator":"read","domain":"code"},"rationale":"read the file"}`, "", "stop", nil)
	utilityFC := newFakeChat(t, full)
	utility, _ := newTestBackend(t, utilityFC, Options{Model: "fake-utility", MaxTokens: 1000})

	tiered := NewTiered(primary, utility)

	obj, ok := tiered.SynthesizeProgram("read config/limits.go", ctxR(), []string{"read"})
	if !ok {
		t.Fatal("escalation tier: SynthesizeProgram should SUCCEED against the utility backend after the primary truncates-invalid at the budget")
	}
	if obj["program"] == nil {
		t.Fatalf("escalation tier: program should be present (from the utility), got %v", obj)
	}
	// the primary tried (initial + 1 bounded retry = 2); the utility was escalated to EXACTLY once.
	if n := primaryFC.requestCount(); n != 2 {
		t.Errorf("escalation tier: primary should make 2 requests (initial + 1 bounded retry), made %d", n)
	}
	if n := utilityFC.requestCount(); n != 1 {
		t.Fatalf("escalation tier: utility should be escalated to EXACTLY once (bounded), made %d", n)
	}
	// the escalation is observable — the utility call record carries the .escalation role suffix.
	if rec, ok := utility.Log.last(); !ok || !strings.HasSuffix(rec.Role, ".escalation") {
		t.Errorf("escalation tier: utility call should carry a .escalation role tag, got %+v", rec)
	}
}

// (j5) NO escalation tier wired (a bare backend, the common single-model case) ⇒ a truncated-invalid
// structured role falls straight to the control floor (ok=false) — ZERO behaviour change, no extra call.
func TestModelEscalationTierNoOpWhenUnconfigured(t *testing.T) {
	truncated := chatResponse(`{"program": {"kind":"step","operator":"rea`, "", "length", nil)
	fc := newFakeChat(t, truncated, truncated, truncated)
	be, _ := newTestBackend(t, fc, Options{Model: "fake-reasoner"}) // bare backend: structuredEscalate is nil

	if _, ok := be.SynthesizeProgram("goal", ctxR(), []string{"read"}); ok {
		t.Error("no escalation tier: SynthesizeProgram should fail (ok=false) — the floor stands")
	}
	// initial + 1 bounded retry only; NO escalation call (there is no escalator wired).
	if n := fc.requestCount(); n != 2 {
		t.Errorf("no escalation tier: want 2 requests (initial + 1 retry), made %d (no escalation may be added)", n)
	}
}

// (j6) the escalation tier also fires for form_intention (the other STRUCTURED role) and is bounded.
func TestModelEscalationTierFormIntention(t *testing.T) {
	truncated := chatResponse(`{"kind":"run","text":"go test ./inte`, "", "length", nil)
	primaryFC := newFakeChat(t, truncated, truncated, truncated)
	primary, _ := newTestBackend(t, primaryFC, Options{Model: "fake-primary", MaxTokens: 1000})

	full := chatResponse(`{"kind":"run","text":"go test ./internal/..."}`, "", "stop", nil)
	utilityFC := newFakeChat(t, full)
	utility, _ := newTestBackend(t, utilityFC, Options{Model: "fake-utility", MaxTokens: 1000})

	tiered := NewTiered(primary, utility)

	text, kind, ok := tiered.Intention("run the tests", ctxR())
	if !ok {
		t.Fatal("escalation tier: form_intention should SUCCEED against the utility after the primary truncates-invalid")
	}
	if kind != "run" || !strings.Contains(text, "go test") {
		t.Fatalf("escalation tier: intention = (%q, %q), want a run intention from the utility", text, kind)
	}
	if n := utilityFC.requestCount(); n != 1 {
		t.Errorf("escalation tier: utility should be escalated to exactly once for form_intention, made %d", n)
	}
}

// (j7) MIN/MAX-context routing: a CONTENT role (Generate — opt.validate == nil) that the MIN-context
// primary truncates to EMPTY (the qwen "thinking substrate unavailable" gap: the verbose reasoner ran
// out of budget mid-thought) escalates to the wired MAX-context backend, whose bigger window returns a
// real answer instead of the surfaced gap. Proves the escalation fires for CONTENT roles too (not only
// structured ones) and is bounded to a single max-ctx call. This is the truncation-uncap fix.
func TestMaxCtxEscalationOnContentTruncation(t *testing.T) {
	// MIN-context primary: every attempt truncates to empty (finish=length, nothing salvageable).
	minFC := newFakeChat(t, chatResponse("", "endless reasoning that never lands on an answer", "length", nil))
	minBE, _ := newTestBackend(t, minFC, Options{Model: "min-ctx", MaxTokens: 1000})

	// MAX-context backend: the real answer, immediately (the bigger window let it finish).
	maxFC := newFakeChat(t, chatResponse("the budget is 25000", "", "stop", nil))
	maxBE, _ := newTestBackend(t, maxFC, Options{Model: "max-ctx", MaxTokens: 24000})

	// Wire the routing the resolver's wireMaxCtx does: the primary's truncation-escalation hook -> max-ctx.
	minBE.structuredEscalate = maxBE.escalateStructured

	got := minBE.Generate("read config/risk.yaml and report the budget", ctxR(), cpyrand.New(0))
	if got != "the budget is 25000" {
		t.Fatalf("content-role escalation Generate = %q, want the max-ctx answer (the gap was uncapped)", got)
	}
	// the MIN primary tried (initial + 1 bounded retry = 2); the MAX-ctx backend was escalated to once.
	if n := minFC.requestCount(); n != 2 {
		t.Errorf("min primary should make 2 requests (initial + 1 bounded retry), made %d", n)
	}
	if n := maxFC.requestCount(); n != 1 {
		t.Fatalf("max-ctx backend should be escalated to EXACTLY once (bounded), made %d", n)
	}
	// the escalation is observable — the max-ctx call record carries the .escalation role suffix.
	if rec, ok := maxBE.Log.last(); !ok || !strings.HasSuffix(rec.Role, ".escalation") {
		t.Errorf("max-ctx escalation should carry a .escalation role tag, got %+v", rec)
	}
}

// (j7b) MIN/MAX-context routing fires on the NON-truncation gap too: a CONTENT role that comes back EMPTY
// with finish=STOP (a reasoning model that "finished" but wrote nothing to content — not finish=length) is
// still unusable and must escalate. This is the bench grounding items 11/14 gap that the old finish=="length"
// gate skipped (there the empty came from a 60s timeout / empty-stop, never a clean truncation). The trigger
// keys on UNUSABLE, not finish_reason.
func TestMaxCtxEscalationOnEmptyStopGap(t *testing.T) {
	// MIN primary: empty content, finish=STOP, no salvageable reasoning → unusable but NOT finish=length.
	minFC := newFakeChat(t, chatResponse("", "", "stop", nil))
	minBE, _ := newTestBackend(t, minFC, Options{Model: "min-ctx", MaxTokens: 1000})

	maxFC := newFakeChat(t, chatResponse("the API binds to port 8037", "", "stop", nil))
	maxBE, _ := newTestBackend(t, maxFC, Options{Model: "max-ctx", MaxTokens: 24000})
	minBE.structuredEscalate = maxBE.escalateStructured

	got := minBE.Respond("compute the bound port", ctxR())
	if got != "the API binds to port 8037" {
		t.Fatalf("empty-stop gap should escalate; Respond = %q, want the max-ctx answer", got)
	}
	if n := maxFC.requestCount(); n != 1 {
		t.Errorf("max-ctx backend should be escalated to once on an empty-stop gap, got %d", n)
	}
}

// (j8) the MAX-context escalation must NOT fire when the MIN primary SUCCEEDS (a non-truncated content
// answer) — the bigger model is reserved for the truncation case, not spent on every call. Guards
// against the escalation becoming an always-on second call (cost + the model/GPU lock).
func TestMaxCtxEscalationNotFiredOnSuccess(t *testing.T) {
	minFC := newFakeChat(t, chatResponse("the budget is 25000", "", "stop", nil)) // succeeds first try
	minBE, _ := newTestBackend(t, minFC, Options{Model: "min-ctx", MaxTokens: 1000})

	maxFC := newFakeChat(t, chatResponse("should never be reached", "", "stop", nil))
	maxBE, _ := newTestBackend(t, maxFC, Options{Model: "max-ctx", MaxTokens: 24000})
	minBE.structuredEscalate = maxBE.escalateStructured

	got := minBE.Generate("read config/risk.yaml and report the budget", ctxR(), cpyrand.New(0))
	if got != "the budget is 25000" {
		t.Fatalf("non-truncated Generate = %q, want the primary's own answer", got)
	}
	if n := maxFC.requestCount(); n != 0 {
		t.Errorf("max-ctx backend must NOT be hit when the primary succeeds, got %d requests", n)
	}
}

// (j9) wireMaxCtxEscalation (the resolver's min/max routing): with a max-context model configured on its
// OWN endpoint and actually loaded, the primary's escalation hook is wired AND a content-role truncation
// then escalates end-to-end to that separate endpoint. Proves the resolver-level wiring + the loaded-check.
func TestWireMaxCtxEscalationWiresSeparateEndpoint(t *testing.T) {
	// MIN-context primary endpoint: truncates to empty.
	minFC := newFakeChat(t, chatResponse("", "endless reasoning", "length", nil))
	minFC.modelID = "min-ctx"
	minBE := NewOpenAICompat(Options{BaseURL: minFC.server.URL + "/v1", Model: "min-ctx", Timeout: 2 * time.Second})

	// MAX-context endpoint (a SEPARATE server — the second LM Studio at concurrency 1): a real answer.
	maxFC := newFakeChat(t, chatResponse("the budget is 25000", "", "stop", nil))
	maxFC.modelID = "max-ctx"

	if minBE.structuredEscalate != nil {
		t.Fatal("precondition: a bare primary should have no escalation hook")
	}
	wireMaxCtxEscalation(minBE, "max-ctx", maxFC.server.URL+"/v1", 24000, SubstrateConfig{})
	if minBE.structuredEscalate == nil {
		t.Fatal("wireMaxCtxEscalation should wire the escalation hook when the max-ctx model is loaded")
	}

	// End-to-end: a content-role truncation on the primary escalates to the separate max-ctx endpoint.
	got := minBE.Generate("read the config and report the budget", ctxR(), cpyrand.New(0))
	if got != "the budget is 25000" {
		t.Fatalf("routed Generate = %q, want the max-ctx endpoint's answer", got)
	}
	if n := maxFC.requestCount(); n != 1 {
		t.Errorf("separate max-ctx endpoint should be escalated to exactly once, got %d", n)
	}
}

// (j10) wireMaxCtxEscalation is a NO-OP when the max-context model is NOT loaded at the target endpoint
// (the server reports a different id) — the hook stays nil, so a configured-but-absent big model never
// silently routes to the wrong model. Mirrors the loaded-check that guards the utility tier.
func TestWireMaxCtxEscalationNoOpWhenNotLoaded(t *testing.T) {
	minFC := newFakeChat(t, chatResponse("", "endless", "length", nil))
	minFC.modelID = "min-ctx"
	minBE := NewOpenAICompat(Options{BaseURL: minFC.server.URL + "/v1", Model: "min-ctx", Timeout: 2 * time.Second})

	// The endpoint serves "other", NOT the requested "max-ctx" → not loaded → no wiring.
	otherFC := newFakeChat(t, chatResponse("unused", "", "stop", nil))
	otherFC.modelID = "other"

	wireMaxCtxEscalation(minBE, "max-ctx", otherFC.server.URL+"/v1", 24000, SubstrateConfig{})
	if minBE.structuredEscalate != nil {
		t.Error("wireMaxCtxEscalation should be a no-op when the max-ctx model isn't loaded at the endpoint")
	}
}

// (j11) wireMaxCtxEscalation is a NO-OP when the configured max-context model IS the primary on the SAME
// endpoint — there is nothing to escalate to (you can't grow the window by re-calling the same model).
func TestWireMaxCtxEscalationNoOpSameModelSameEndpoint(t *testing.T) {
	fc := newFakeChat(t, chatResponse("", "endless", "length", nil))
	fc.modelID = "same"
	be := NewOpenAICompat(Options{BaseURL: fc.server.URL + "/v1", Model: "same", Timeout: 2 * time.Second})

	wireMaxCtxEscalation(be, "same", fc.server.URL+"/v1", 24000, SubstrateConfig{})
	if be.structuredEscalate != nil {
		t.Error("same model on the same endpoint should not wire an escalation hook (nothing to escalate to)")
	}
}

// (k) the reasoning defaults are normalised from env when left unset; explicit setters beat env.
func TestReasoningOptionsEnvDefaults(t *testing.T) {
	// Defaults: salvage on, retry on, fields = the multi-provider default, 1 retry.
	r := normalizeReasoning(ReasoningOptions{})
	if !r.SalvageFromReasoning || !r.RetryOnTruncation {
		t.Errorf("default reasoning opts = %+v, want salvage+retry on", r)
	}
	if len(r.Fields) != 3 || r.Fields[0] != "reasoning_content" {
		t.Errorf("default fields = %v, want the multi-provider default", r.Fields)
	}
	if r.MaxRetries != 1 || r.RetryMaxTokensCap != defaultRetryMaxTokensCap {
		t.Errorf("default retry knobs = %+v", r)
	}

	// Env disables salvage; an explicit WithSalvage(true) beats it.
	t.Setenv("THOUGHT_LLM_SALVAGE", "0")
	t.Setenv("THOUGHT_LLM_REASONING_FIELDS", "reasoning,thinking")
	if r := normalizeReasoning(ReasoningOptions{}); r.SalvageFromReasoning {
		t.Error("THOUGHT_LLM_SALVAGE=0 should disable salvage by default")
	}
	if r := normalizeReasoning(ReasoningOptions{}.WithSalvage(true)); !r.SalvageFromReasoning {
		t.Error("explicit WithSalvage(true) should beat THOUGHT_LLM_SALVAGE=0")
	}
	if r := normalizeReasoning(ReasoningOptions{}); len(r.Fields) != 2 || r.Fields[1] != "thinking" {
		t.Errorf("THOUGHT_LLM_REASONING_FIELDS env field list = %v", r.Fields)
	}
}

// lastLLMEvent returns the last events.LLM (llm.call) event, or nil.
func lastLLMEvent(evs []events.Event) *events.Event {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == events.LLM {
			return &evs[i]
		}
	}
	return nil
}

func evData(ev *events.Event) any {
	if ev == nil {
		return nil
	}
	return ev.Data
}
