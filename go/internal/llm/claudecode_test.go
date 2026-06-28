package llm

import (
	"strings"
	"testing"
)

// --- envelope parsing -------------------------------------------------------

func freshResult() postResult {
	return postResult{reasoningTokens: -1, promptTokens: -1, completionTokens: -1,
		totalTokens: -1, cachedInputTokens: -1, cacheMissTokens: -1}
}

func TestParseClaudeEnvelopeSuccess(t *testing.T) {
	data := []byte(`{"type":"result","subtype":"success","is_error":false,` +
		`"result":"  hello  ","stop_reason":"end_turn",` +
		`"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":5}}`)
	res, err := parseClaudeEnvelope(data, freshResult())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.content != "hello" {
		t.Fatalf("content = %q, want %q", res.content, "hello")
	}
	if res.finish != "end_turn" {
		t.Fatalf("finish = %q, want end_turn", res.finish)
	}
	if res.promptTokens != 12 || res.completionTokens != 3 || res.totalTokens != 15 {
		t.Fatalf("usage = (%d,%d,%d), want (12,3,15)", res.promptTokens, res.completionTokens, res.totalTokens)
	}
	if res.cachedInputTokens != 5 {
		t.Fatalf("cachedInputTokens = %d, want 5", res.cachedInputTokens)
	}
}

func TestParseClaudeEnvelopeIsError(t *testing.T) {
	data := []byte(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login",` +
		`"stop_reason":"stop_sequence","usage":{}}`)
	_, err := parseClaudeEnvelope(data, freshResult())
	if err == nil {
		t.Fatal("want error for is_error envelope")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("error should carry the CLI reason, got: %v", err)
	}
}

func TestParseClaudeEnvelopeMaxTokensMapsToLength(t *testing.T) {
	// Truncated-empty: the retry-on-truncation path keys on finish=="length" + truncatedEmpty.
	data := []byte(`{"type":"result","is_error":false,"result":"","stop_reason":"max_tokens","usage":{}}`)
	res, err := parseClaudeEnvelope(data, freshResult())
	if err == nil {
		t.Fatal("want empty-completion error")
	}
	if res.finish != "length" {
		t.Fatalf("finish = %q, want length (mapped from max_tokens)", res.finish)
	}
	if !res.truncatedEmpty {
		t.Fatal("truncatedEmpty should be set for empty max_tokens result")
	}
}

func TestParseClaudeEnvelopeGarbage(t *testing.T) {
	if _, err := parseClaudeEnvelope([]byte("not json"), freshResult()); err == nil {
		t.Fatal("want error for unparseable envelope")
	}
}

// --- argv construction ------------------------------------------------------

func TestClaudeArgsIsolation(t *testing.T) {
	args := claudeArgs("sonnet", "SYS", "USER")
	joined := strings.Join(args, "\x00")
	for _, want := range []string{"-p", "USER", "--output-format", "json",
		"--no-session-persistence", "--tools", "--model", "sonnet", "--system-prompt", "SYS"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, args)
		}
	}
	// --tools must be the EMPTY list (pure completion, no tool execution).
	for i, a := range args {
		if a == "--tools" {
			if args[i+1] != "" {
				t.Fatalf("--tools value = %q, want empty", args[i+1])
			}
		}
	}
}

func TestClaudeArgsOmitsEmptySystemAndModel(t *testing.T) {
	args := claudeArgs("", "", "USER")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--system-prompt") || strings.Contains(joined, "--model") {
		t.Fatalf("empty system/model should be omitted: %v", args)
	}
}

// --- transport dispatch through the real role logic --------------------------

// TestTransportDispatch proves chat() routes through the injected transport (not HTTP) and the
// role logic consumes the result unchanged — the whole point of the seam.
func TestTransportDispatch(t *testing.T) {
	b := newClaudeExecBackend("claude-bin-not-invoked", "sonnet", 0, 0)
	var gotModel, gotSystem, gotUser string
	b.transport = func(reqBody map[string]any, _ bool) (postResult, error) {
		gotModel, _ = reqBody["model"].(string)
		if msgs, ok := reqBody["messages"].([]map[string]string); ok {
			for _, m := range msgs {
				if m["role"] == "system" {
					gotSystem = m["content"]
				}
				if m["role"] == "user" {
					gotUser = m["content"]
				}
			}
		}
		res := freshResult()
		res.content = "BRIDGED ANSWER"
		res.finish = "end_turn"
		return res, nil
	}
	out := b.Respond("the goal", nil)
	if out != "BRIDGED ANSWER" {
		t.Fatalf("Respond = %q, want the transport's content", out)
	}
	if gotModel != "sonnet" {
		t.Fatalf("transport saw model %q, want sonnet", gotModel)
	}
	if gotSystem == "" || gotUser == "" {
		t.Fatal("transport should receive the role's system+user prompts")
	}
}

// TestTransportErrorSurfacesGap proves a transport failure surfaces the gap ("" — Pattern B),
// never a substituted template.
func TestTransportErrorSurfacesGap(t *testing.T) {
	b := newClaudeExecBackend("claude-bin-not-invoked", "sonnet", 0, 0)
	b.transport = func(map[string]any, bool) (postResult, error) {
		return freshResult(), errString("claude: Not logged in")
	}
	if out := b.Respond("the goal", nil); out != "" {
		t.Fatalf("Respond on transport error = %q, want empty (surfaced gap)", out)
	}
	if b.Fallbacks == 0 {
		t.Fatal("a transport failure must count as a fallback")
	}
}

// --- constructor wiring ------------------------------------------------------

func TestNewClaudeCodeTieredDefault(t *testing.T) {
	be := NewClaudeCode(ClaudeCodeOptions{Bin: "claude-bin-not-invoked"})
	tb, ok := be.(*TieredBackend)
	if !ok {
		t.Fatalf("default NewClaudeCode should be tiered (primary+utility), got %T", be)
	}
	if tb.Primary.Model != "sonnet" || tb.Utility.Model != "haiku" {
		t.Fatalf("tier models = (%s,%s), want (sonnet,haiku)", tb.Primary.Model, tb.Utility.Model)
	}
	if got := tb.Primary.DisplayName(); got != "claude:sonnet" {
		t.Fatalf("primary DisplayName = %q, want claude:sonnet", got)
	}
	if tb.Primary.transport == nil || tb.Utility.transport == nil {
		t.Fatal("both tiers must carry the exec transport")
	}
}

func TestNewClaudeCodeUtilityNone(t *testing.T) {
	be := NewClaudeCode(ClaudeCodeOptions{Bin: "claude-bin-not-invoked", UtilityModel: "none"})
	if _, ok := be.(*OpenAICompatBackend); !ok {
		t.Fatalf("UtilityModel none should yield a single backend, got %T", be)
	}
}

func TestMakeBackendClaude(t *testing.T) {
	be, err := MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("MakeBackend(claude): %v", err)
	}
	tb, ok := be.(*TieredBackend)
	if !ok {
		t.Fatalf("MakeBackend(claude) should be tiered, got %T", be)
	}
	// "auto" must resolve to the bridge default, never an HTTP autodetect.
	if tb.Primary.Model != "sonnet" {
		t.Fatalf("auto should map to sonnet, got %s", tb.Primary.Model)
	}
}

func TestClaudeHealthNoHTTP(t *testing.T) {
	b := newClaudeExecBackend("definitely-not-a-real-binary-xyz", "sonnet", 0, 0)
	h := b.Health()
	if h.Up {
		t.Fatal("health should be down when the CLI binary is absent")
	}
	if !strings.Contains(h.Error, "claude CLI not found") {
		t.Fatalf("health error should name the missing CLI, got %q", h.Error)
	}
}
