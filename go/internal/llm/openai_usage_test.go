package llm

import (
	"encoding/json"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// These tests drive the usage/cost-accounting decode path OFFLINE with an httptest.Server (the shared
// fakeChat helper, see openai_reasoning_test.go) returning crafted OpenAI-compatible responses with a
// full `usage` block — NO real model. They assert prompt/completion/total/reasoning + the CACHE split
// decode into BOTH the per-call callRecord AND the llm.call event Data map, in BOTH provider shapes:
//   - DeepSeek: usage.prompt_cache_hit_tokens + usage.prompt_cache_miss_tokens
//   - OpenAI:   usage.prompt_tokens_details.cached_tokens (no miss count)
// This is what makes the bench Cost.Tokens real (it was hard-wired to 0) and lets cost be attributed
// per role (the llm role tag stays on the record + event).

// usageBlock is the full usage shape both providers populate; nil pointers omit the field entirely so
// a test can craft "this provider didn't send that key".
type usageBlock struct {
	PromptTokens          *int
	CompletionTokens      *int
	TotalTokens           *int
	ReasoningTokens       *int // nested under completion_tokens_details
	PromptCacheHitTokens  *int // DeepSeek shape
	PromptCacheMissTokens *int // DeepSeek shape
	CachedTokens          *int // OpenAI shape, nested under prompt_tokens_details
}

// chatResponseUsage builds an OpenAI-compatible response with `content` + a fully-crafted `usage`
// object (only the non-nil fields appear). Mirrors chatResponse but for the usage-accounting path.
func chatResponseUsage(content, finish string, u usageBlock) string {
	msg := map[string]any{"role": "assistant", "content": content}
	choice := map[string]any{"message": msg, "finish_reason": finish}
	usage := map[string]any{}
	if u.PromptTokens != nil {
		usage["prompt_tokens"] = *u.PromptTokens
	}
	if u.CompletionTokens != nil {
		usage["completion_tokens"] = *u.CompletionTokens
	}
	if u.TotalTokens != nil {
		usage["total_tokens"] = *u.TotalTokens
	}
	if u.ReasoningTokens != nil {
		usage["completion_tokens_details"] = map[string]any{"reasoning_tokens": *u.ReasoningTokens}
	}
	if u.PromptCacheHitTokens != nil {
		usage["prompt_cache_hit_tokens"] = *u.PromptCacheHitTokens
	}
	if u.PromptCacheMissTokens != nil {
		usage["prompt_cache_miss_tokens"] = *u.PromptCacheMissTokens
	}
	if u.CachedTokens != nil {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": *u.CachedTokens}
	}
	body := map[string]any{"choices": []any{choice}, "usage": usage}
	b, _ := json.Marshal(body)
	return string(b)
}

func ip(v int) *int { return &v }

// evInt pulls an int out of an event Data map (JSON round-trips numbers to float64 in some paths, but
// the emit here passes Go ints straight through, so compare as int). Fails the test on a type mismatch.
func evInt(t *testing.T, data map[string]any, key string) int {
	t.Helper()
	v, ok := data[key]
	if !ok {
		t.Fatalf("llm.call event Data missing key %q (have %v)", key, data)
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		t.Fatalf("llm.call Data[%q] = %v (type %T), want an int", key, v, v)
		return 0
	}
}

// (a) DeepSeek cache shape: prompt/completion/total + reasoning + prompt_cache_hit/miss_tokens all
// decode into the callRecord AND the llm.call event Data map. The role tag stays on both.
func TestUsageDeepSeekCacheShapeDecodes(t *testing.T) {
	fc := newFakeChat(t, chatResponseUsage("done", "stop", usageBlock{
		PromptTokens:          ip(1200),
		CompletionTokens:      ip(340),
		TotalTokens:           ip(1540),
		ReasoningTokens:       ip(256),
		PromptCacheHitTokens:  ip(900),
		PromptCacheMissTokens: ip(300),
	}))
	be, emitted := newTestBackend(t, fc, Options{Model: "deepseek-chat"})

	if got := be.Generate("goal", ctxR(), cpyrand.New(0)); got != "done" {
		t.Fatalf("Generate = %q, want the content verbatim", got)
	}

	// callRecord.
	rec, ok := be.Log.last()
	if !ok {
		t.Fatal("no call record pushed")
	}
	if rec.Role != "conscious.generate" {
		t.Errorf("call record role = %q, want conscious.generate (the role tag must survive)", rec.Role)
	}
	if rec.PromptTokens != 1200 || rec.CompletionTokens != 340 || rec.TotalTokens != 1540 {
		t.Errorf("callRecord prompt/completion/total = %d/%d/%d, want 1200/340/1540",
			rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
	if rec.ReasoningTokens != 256 {
		t.Errorf("callRecord reasoning_tokens = %d, want 256", rec.ReasoningTokens)
	}
	if rec.CachedInputTokens != 900 {
		t.Errorf("callRecord cached_input_tokens = %d, want 900 (DeepSeek hit)", rec.CachedInputTokens)
	}
	if rec.CacheMissTokens != 300 {
		t.Errorf("callRecord cache_miss_tokens = %d, want 300 (DeepSeek miss)", rec.CacheMissTokens)
	}

	// llm.call event Data.
	ev := lastLLMEvent(*emitted)
	if ev == nil {
		t.Fatal("no llm.call event emitted")
	}
	if ev.Data["role"] != "conscious.generate" {
		t.Errorf("event role = %v, want conscious.generate (role tag for per-role cost attribution)", ev.Data["role"])
	}
	if got := evInt(t, ev.Data, "prompt_tokens"); got != 1200 {
		t.Errorf("event prompt_tokens = %d, want 1200", got)
	}
	if got := evInt(t, ev.Data, "completion_tokens"); got != 340 {
		t.Errorf("event completion_tokens = %d, want 340", got)
	}
	if got := evInt(t, ev.Data, "total_tokens"); got != 1540 {
		t.Errorf("event total_tokens = %d, want 1540", got)
	}
	if got := evInt(t, ev.Data, "reasoning_tokens"); got != 256 {
		t.Errorf("event reasoning_tokens = %d, want 256", got)
	}
	if got := evInt(t, ev.Data, "cached_input_tokens"); got != 900 {
		t.Errorf("event cached_input_tokens = %d, want 900", got)
	}
	if got := evInt(t, ev.Data, "cache_miss_tokens"); got != 300 {
		t.Errorf("event cache_miss_tokens = %d, want 300", got)
	}
}

// (b) OpenAI cache shape: prompt/completion/total decode AND prompt_tokens_details.cached_tokens
// maps to cached_input_tokens; there is no miss count, so cache_miss_tokens stays -1 (absent).
func TestUsageOpenAICachedTokensShapeDecodes(t *testing.T) {
	fc := newFakeChat(t, chatResponseUsage("ok", "stop", usageBlock{
		PromptTokens:     ip(800),
		CompletionTokens: ip(120),
		TotalTokens:      ip(920),
		CachedTokens:     ip(640), // nested under prompt_tokens_details
	}))
	be, emitted := newTestBackend(t, fc, Options{Model: "gpt-4o"})

	if got := be.Generate("goal", ctxR(), cpyrand.New(0)); got != "ok" {
		t.Fatalf("Generate = %q, want the content verbatim", got)
	}

	rec, _ := be.Log.last()
	if rec.PromptTokens != 800 || rec.CompletionTokens != 120 || rec.TotalTokens != 920 {
		t.Errorf("callRecord prompt/completion/total = %d/%d/%d, want 800/120/920",
			rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
	if rec.CachedInputTokens != 640 {
		t.Errorf("callRecord cached_input_tokens = %d, want 640 (OpenAI cached_tokens)", rec.CachedInputTokens)
	}
	if rec.CacheMissTokens != -1 {
		t.Errorf("callRecord cache_miss_tokens = %d, want -1 (OpenAI reports no miss count)", rec.CacheMissTokens)
	}

	ev := lastLLMEvent(*emitted)
	if ev == nil {
		t.Fatal("no llm.call event emitted")
	}
	if got := evInt(t, ev.Data, "prompt_tokens"); got != 800 {
		t.Errorf("event prompt_tokens = %d, want 800", got)
	}
	if got := evInt(t, ev.Data, "cached_input_tokens"); got != 640 {
		t.Errorf("event cached_input_tokens = %d, want 640 (OpenAI shape)", got)
	}
	if got := evInt(t, ev.Data, "cache_miss_tokens"); got != -1 {
		t.Errorf("event cache_miss_tokens = %d, want -1 (absent for OpenAI)", got)
	}
}

// (c) a usage block with NO cache fields at all (a plain local model) → the headline counts decode but
// both cache fields stay -1 (absent, not a misleading 0).
func TestUsageNoCacheFieldsStayAbsent(t *testing.T) {
	fc := newFakeChat(t, chatResponseUsage("hi", "stop", usageBlock{
		PromptTokens:     ip(50),
		CompletionTokens: ip(10),
		TotalTokens:      ip(60),
	}))
	be, _ := newTestBackend(t, fc, Options{Model: "local-model"})

	_ = be.Generate("goal", ctxR(), cpyrand.New(0))
	rec, _ := be.Log.last()
	if rec.PromptTokens != 50 || rec.TotalTokens != 60 {
		t.Errorf("callRecord prompt/total = %d/%d, want 50/60", rec.PromptTokens, rec.TotalTokens)
	}
	if rec.CachedInputTokens != -1 || rec.CacheMissTokens != -1 {
		t.Errorf("no-cache usage should leave cache fields -1, got cached=%d miss=%d",
			rec.CachedInputTokens, rec.CacheMissTokens)
	}
}
