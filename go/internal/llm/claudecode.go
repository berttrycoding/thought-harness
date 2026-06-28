package llm

// The Claude Code CLI bridge — the subscription-riding frontier substrate (--backend claude).
//
// This is NOT a second backend implementation: it is OpenAICompatBackend with the HTTP
// round-trip swapped for a headless `claude -p` exec (docs/internal/notes/claude-code-substrate-mapping.md
// §3 Phase 1). Every role's prompt construction, validation, bounded retry, scheduler grant and
// event telemetry is reused unchanged; only the transport differs. The bridge exists for harness
// DEV + ARCHITECTURE VALIDATION on a frontier model without the local GPU; the product default
// substrate stays local / API-key (resolve.go).
//
// Isolation: every call runs `claude --bare --no-session-persistence --tools ""` from a neutral
// working directory, so bridge calls never load this repo's CLAUDE.md/skills/MCP servers, never
// persist sessions, and never execute tools — each call is a pure completion.
//
// Auth: the spawned CLI resolves credentials exactly like an interactive `claude` run (login
// keychain / CLAUDE_CODE_OAUTH_TOKEN from `claude setup-token` / ANTHROPIC_API_KEY). The bridge
// only inherits the environment; it never reads or stores credentials itself.
//
// Capability-parity note (mapping doc §7): the CLI exposes no temperature knob, so per-role
// temperature overrides are IGNORED on this substrate — runs must be substrate-tagged and never
// compared against local rows as if temperature were controlled.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// ClaudeCodeOptions configures the bridge. Zero values resolve to env, then defaults.
type ClaudeCodeOptions struct {
	Bin          string        // claude binary (default env THOUGHT_CLAUDE_BIN, else "claude")
	Model        string        // primary model alias/id (default env THOUGHT_CLAUDE_MODEL, else "sonnet")
	UtilityModel string        // utility tier for trivial roles (default env THOUGHT_CLAUDE_UTILITY_MODEL, else "haiku"; "none" disables the tier)
	MaxTokens    int           // per-call completion budget (0 → the shared THOUGHT_LLM_MAX_TOKENS default)
	Timeout      time.Duration // per-call wall clock (0 → the shared THOUGHT_LLM_TIMEOUT_SECONDS default)
}

// NewClaudeCode builds the bridge backend: a PRIMARY claude-exec backend plus (by default) a
// haiku UTILITY tier behind the existing TieredBackend, so trivial roles (summarize) and the
// structured-escalation tier ride the cheaper model — the same hybrid-cognition shape as the
// local two-tier setup.
func NewClaudeCode(opts ClaudeCodeOptions) backends.Backend {
	bin := firstNonEmpty(opts.Bin, os.Getenv("THOUGHT_CLAUDE_BIN"), "claude")
	model := firstNonEmpty(opts.Model, os.Getenv("THOUGHT_CLAUDE_MODEL"), "sonnet")
	util := firstNonEmpty(opts.UtilityModel, os.Getenv("THOUGHT_CLAUDE_UTILITY_MODEL"), "haiku")
	primary := newClaudeExecBackend(bin, model, opts.MaxTokens, opts.Timeout)
	if util == "" || util == "none" || util == model {
		return primary
	}
	utility := newClaudeExecBackend(bin, util, opts.MaxTokens, opts.Timeout)
	return NewTiered(primary, utility)
}

// newClaudeExecBackend builds one claude-exec tier: the standard OpenAICompatBackend with the
// transport + health + display overrides installed.
func newClaudeExecBackend(bin, model string, maxTokens int, timeout time.Duration) *OpenAICompatBackend {
	b := NewOpenAICompat(Options{
		// BaseURL is a label only — the transport never dials it; it keeps log lines honest.
		BaseURL:   "claude-code",
		Model:     model,
		MaxTokens: maxTokens,
		Timeout:   timeout,
		// The CLI returns clean content (no reasoning_content misfiling), so salvage/retry knobs
		// stay at their defaults and simply never fire on the salvage path.
	})
	b.displayName = "claude:" + model
	b.substrateClass = "claude"
	b.transport = claudeExecTransport(bin, b)
	b.transportHealth = func() HealthReport {
		if _, err := exec.LookPath(bin); err != nil {
			return HealthReport{Up: false, Error: "claude CLI not found: " + err.Error(), Models: []string{}}
		}
		// Binary present ⇒ report up with the configured model; auth problems surface on the
		// first call as an honest is_error result (and doctor probes a real call anyway).
		return HealthReport{Up: true, Models: []string{model}}
	}
	return b
}

// claudeArgs builds the headless argv for one completion call. Split out for tests.
// --no-session-persistence + --tools "" + the neutral cwd (claudeExecTransport sets cmd.Dir to
// os.TempDir()) make the call a pure, isolated completion. NOTE: --bare is deliberately NOT used —
// it bypasses the interactive keychain login, so a spawned call returns "Not logged in" unless
// CLAUDE_CODE_OAUTH_TOKEN is exported (verified 2026-06-13: --bare → not-logged-in/empty content on
// every role). The neutral cwd already prevents project-context leakage, so isolation holds without it.
func claudeArgs(model, system, user string) []string {
	args := []string{"-p", user,
		"--output-format", "json",
		"--no-session-persistence",
		"--tools", ""}
	if model != "" {
		args = append(args, "--model", model)
	}
	if system != "" {
		args = append(args, "--system-prompt", system)
	}
	return args
}

// claudeExecTransport returns the postChat-contract transport that runs one `claude -p` per
// chat() call. The reqBody is the exact OpenAI-shaped body chat() builds; temperature is
// ignored (no CLI knob), max_tokens maps to CLAUDE_CODE_MAX_OUTPUT_TOKENS.
func claudeExecTransport(bin string, b *OpenAICompatBackend) func(map[string]any, bool) (postResult, error) {
	return func(reqBody map[string]any, _ bool) (postResult, error) {
		res := postResult{reasoningTokens: -1, promptTokens: -1, completionTokens: -1,
			totalTokens: -1, cachedInputTokens: -1, cacheMissTokens: -1}
		model, _ := reqBody["model"].(string)
		var system, user string
		if msgs, ok := reqBody["messages"].([]map[string]string); ok {
			for _, m := range msgs {
				switch m["role"] {
				case "system":
					system = m["content"]
				case "user":
					user = m["content"]
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), b.Timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, claudeArgs(model, system, user)...)
		// Neutral cwd: belt-and-braces with --bare so no project context leaks into the call.
		cmd.Dir = os.TempDir()
		env := os.Environ()
		if maxTok, ok := reqBody["max_tokens"].(int); ok && maxTok > 0 {
			env = append(env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS="+strconv.Itoa(maxTok))
		}
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		runErr := cmd.Run()
		if stdout.Len() == 0 {
			// No envelope at all — a spawn/timeout/crash failure. Surface stderr's first line.
			msg := "claude exec failed"
			if runErr != nil {
				msg += ": " + runErr.Error()
			}
			if line := firstLine(stderr.String()); line != "" {
				msg += ": " + line
			}
			return res, errString(msg)
		}
		// A non-zero exit with an envelope on stdout still parses (is_error carries the reason).
		return parseClaudeEnvelope(stdout.Bytes(), res)
	}
}

// parseClaudeEnvelope maps the `claude -p --output-format json` result envelope onto the
// postChat contract: .result is the content, is_error is an honest gap (never substituted),
// stop_reason "max_tokens" maps to the finish_reason "length" the bounded-retry path keys on.
func parseClaudeEnvelope(data []byte, res postResult) (postResult, error) {
	var envl struct {
		Type       string `json:"type"`
		IsError    bool   `json:"is_error"`
		Result     string `json:"result"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens          *int `json:"input_tokens"`
			OutputTokens         *int `json:"output_tokens"`
			CacheReadInputTokens *int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &envl); err != nil {
		return res, errString("claude envelope unparseable: " + err.Error() + "; head: " + head(string(data), 80))
	}
	if pt := envl.Usage.InputTokens; pt != nil {
		res.promptTokens = *pt
	}
	if ct := envl.Usage.OutputTokens; ct != nil {
		res.completionTokens = *ct
	}
	if cached := envl.Usage.CacheReadInputTokens; cached != nil {
		res.cachedInputTokens = *cached
	}
	if res.promptTokens >= 0 && res.completionTokens >= 0 {
		res.totalTokens = res.promptTokens + res.completionTokens
	}
	res.finish = envl.StopReason
	if envl.StopReason == "max_tokens" {
		res.finish = "length"
	}
	if envl.IsError {
		return res, errString("claude: " + head(strings.TrimSpace(envl.Result), 160))
	}
	res.content = strings.TrimSpace(envl.Result)
	if res.content == "" {
		res.truncatedEmpty = res.finish == "length"
		return res, errString("empty completion (stop_reason=" + envl.StopReason + ")")
	}
	return res, nil
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
