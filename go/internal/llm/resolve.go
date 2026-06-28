package llm

import (
	"os"
	"strings"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// BackendUnavailable is returned when no reachable model can be found — and there is NO offline
// product path. The harness REQUIRES a real model as its thinking substrate; the deterministic
// TestBackend exists only as a test double. Mirrors the Python BackendUnavailable
// RuntimeError; resolve_substrate raises it, NewEngine surfaces it (no silent offline fallback).
type BackendUnavailable string

func (e BackendUnavailable) Error() string { return string(e) }

// CanonicalSubstrate normalizes every accepted substrate/backend name — ONE alias table for the
// whole harness (the TUI Settings menu, the CLI --backend flag, config files, env). The canonical
// vocabulary is exactly the Settings menu: auto | frontier | local | session | claude | test.
// ok=false for an unknown name.
func CanonicalSubstrate(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return "auto", true
	case "test", "none":
		return "test", true
	case "local", "llm", "lmstudio", "openai":
		return "local", true
	case "frontier", "api":
		return "frontier", true
	case "session", "cc":
		return "session", true
	case "claude", "claudecode", "claude-code":
		return "claude", true
	default:
		return "", false
	}
}

// SubstrateMenu is the canonical selectable menu, in display order — the single source the TUI
// Settings picker and the CLI flag help both render.
var SubstrateMenu = []string{"auto", "frontier", "local", "session", "claude", "test"}

// ClassOf reports the canonical substrate class a RUNNING backend embodies (test | local |
// frontier | session | claude), or "" for an unrecognised type. This is the one truth the UI
// reads — never derived by parsing display labels.
func ClassOf(b backends.Backend) string {
	switch x := b.(type) {
	case *backends.TestBackend:
		return "test"
	case *TieredBackend:
		return x.Primary.SubstrateClass()
	case *OpenAICompatBackend:
		return x.SubstrateClass()
	default:
		return ""
	}
}

// MakeBackend builds a backend by name — the DEV override path (`--backend`): direct
// construction, no health probe (the doctor wants to probe a sick endpoint, not be refused by
// it). Accepts the FULL canonical menu: auto/frontier delegate to ResolveSubstrate (they are
// policies, not constructors). maxTokens overrides the per-call completion budget (0 →
// env/default); the remaining reasoning knobs read env.
func MakeBackend(name, baseURL, model string, maxTokens int) (backends.Backend, error) {
	canonical, ok := CanonicalSubstrate(name)
	if !ok {
		return nil, errString("unknown backend " + pyRepr(name) + " (use one of: " +
			strings.Join(SubstrateMenu, " | ") + ")")
	}
	switch canonical {
	case "test":
		return backends.NewTest(), nil
	case "local":
		return NewOpenAICompat(Options{BaseURL: baseURL, Model: model, MaxTokens: maxTokens}), nil
	case "session":
		// The SESSION bridge (sessionbridge.go): the open Claude Code session services the
		// calls over the MCP job queue (thought mcp-serve) — no spawn, no login, no token.
		return NewSessionBridge("", maxTokens, 0), nil
	case "claude":
		// The Claude Code CLI bridge (claudecode.go): frontier substrate over the user's
		// subscription, no HTTP endpoint. --llm-model overrides the primary alias; the
		// "auto" default means "use the bridge default" here (there is no /models to probe).
		if model == "auto" {
			model = ""
		}
		return NewClaudeCode(ClaudeCodeOptions{Model: model, MaxTokens: maxTokens}), nil
	default: // "auto" / "frontier" are POLICIES — resolve them like the product does.
		return ResolveSubstrate(canonical, SubstrateConfig{BaseURL: baseURL, Model: model, MaxTokens: maxTokens})
	}
}

// frontierConfig returns a configured frontier endpoint {base_url, api_key, model}, or ok=false.
// A frontier model is "configured" when an API key for a remote OpenAI-compatible endpoint is in
// the environment. Mirrors Python _frontier_config exactly (ANTHROPIC_API_KEY first, then a remote
// THOUGHT_LLM_BASE_URL + non-"lm-studio" key).
func frontierConfig() (Options, bool) {
	if akey := os.Getenv("ANTHROPIC_API_KEY"); akey != "" {
		return Options{
			BaseURL: envOr("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1"),
			APIKey:  akey,
			Model:   envOr("THOUGHT_LLM_MODEL", "claude-sonnet-4-6"),
		}, true
	}
	key := os.Getenv("THOUGHT_LLM_API_KEY")
	base := os.Getenv("THOUGHT_LLM_BASE_URL")
	remote := base != "" && !strings.Contains(base, "localhost") &&
		!strings.Contains(base, "127.0.0.1") && !strings.Contains(base, "0.0.0.0")
	if key != "" && key != "lm-studio" && remote {
		return Options{BaseURL: base, APIKey: key, Model: envOr("THOUGHT_LLM_MODEL", "auto")}, true
	}
	return Options{}, false
}

// SubstrateConfig carries the explicit settings (from TUI / CLI) ResolveSubstrate honours first.
// All fields optional; an empty field means "not set" (Python's None).
type SubstrateConfig struct {
	BaseURL      string
	Model        string
	APIKey       string
	UtilityModel string

	// MaxCtxModel is the MAX-CONTEXT escalation model (0 → THOUGHT_LLM_MAXCTX_MODEL). When set + loaded,
	// a call that TRUNCATES (the small/fast MIN-context primary ran out of budget) gets ONE final attempt
	// against THIS model — run by the user with a BIG context window + concurrency 1, so each truncation-
	// prone call gets the full window + GPU. MaxCtxTokens is its (bigger) completion budget. MaxCtxURL lets
	// the big model live on its OWN endpoint (a second LM Studio at concurrency 1); empty → the primary's URL.
	MaxCtxModel  string
	MaxCtxTokens int
	MaxCtxURL    string

	// MaxTokens overrides the per-call completion budget (0 → THOUGHT_LLM_MAX_TOKENS, default 4096).
	// Reasoning headroom: a too-small budget runs out mid-reasoning → empty content → the NOISY-RULER.
	MaxTokens int

	// Reasoning carries the reasoning-model knobs (multi-provider field list, salvage,
	// retry-on-truncation). The zero value normalises to reasoning-friendly defaults + env.
	Reasoning ReasoningOptions
}

// applyTo stamps the MaxTokens + Reasoning knobs onto an Options before the backend is built, so
// every backend ResolveSubstrate creates (primary, utility, frontier, local) shares the same
// reasoning config. Zero fields fall through to NewOpenAICompat's env/default normalisation.
func (c SubstrateConfig) applyTo(o Options) Options {
	o.MaxTokens = c.MaxTokens
	o.Reasoning = c.Reasoning
	return o
}

// ResolveSubstrate resolves the thinking substrate per policy — there is NO offline product path:
//
//   - substrate "test"          -> the deterministic TestBackend (TEST DOUBLE only).
//   - explicit base_url/model/api_key (TUI settings) -> that endpoint, health-checked.
//   - "auto"     -> a configured frontier model if reachable, else a reachable local model, else ERROR.
//   - "frontier" -> require a configured, reachable frontier model (else ERROR).
//   - "local"    -> require a reachable local model (else ERROR).
//
// Returns a BackendUnavailable error when no real model can be reached — it NEVER silently degrades
// to the heuristic templates. Mirrors Python resolve_substrate exactly.
func ResolveSubstrate(substrate string, cfg SubstrateConfig) (backends.Backend, error) {
	// Normalize through the ONE alias table (empty ⇒ "auto"); an unknown name errors loudly here
	// rather than falling through to a misleading "no reachable model".
	canonical, ok := CanonicalSubstrate(substrate)
	if !ok {
		return nil, BackendUnavailable("unknown substrate " + pyRepr(substrate) + " (use one of: " +
			strings.Join(SubstrateMenu, " | ") + ")")
	}
	substrate = canonical
	if substrate == "test" {
		return backends.NewTest(), nil
	}
	// The MCP SESSION bridge (sessionbridge.go): the open Claude Code session services each CONTENT
	// call over the spool, no spawn/login. A first-class, UI-SELECTABLE substrate (the TUI Settings
	// picker + /substrate), mirroring the `--backend session` dev override (MakeBackend). The spool
	// defaults from THOUGHT_SESSION_SPOOL; a worker (tools/cc-worker.sh) must service it, else calls
	// surface the honest "no worker answered" gap. ResolveSubstrate returns immediately (no model
	// probe), so the welcome card does not block on it.
	if substrate == "session" {
		return NewSessionBridge("", cfg.MaxTokens, 0), nil
	}
	// The Claude Code CLI bridge — a first-class menu entry (it was selectable in the TUI Settings
	// but unhandled here, so picking it errored "no reachable model"). Construction is offline
	// (no endpoint to probe); auth surfaces per-call / via doctor. cfg.Model overrides the primary
	// alias ("auto" means the bridge default — there is no /models to probe).
	if substrate == "claude" {
		model := cfg.Model
		if model == "auto" {
			model = ""
		}
		return NewClaudeCode(ClaudeCodeOptions{Model: model, MaxTokens: cfg.MaxTokens}), nil
	}

	util := cfg.UtilityModel
	if util == "" {
		util = os.Getenv("THOUGHT_LLM_UTILITY_MODEL")
	}

	// tier wraps a primary reasoning model with a small utility model for trivial roles, if one is
	// configured and reachable on the same endpoint (Python _tier). The utility tier inherits the
	// same reasoning config (it may be a reasoning model too).
	maxCtxModel := cfg.MaxCtxModel
	if maxCtxModel == "" {
		maxCtxModel = os.Getenv("THOUGHT_LLM_MAXCTX_MODEL")
	}
	maxCtxURL := cfg.MaxCtxURL
	if maxCtxURL == "" {
		maxCtxURL = os.Getenv("THOUGHT_LLM_MAXCTX_URL")
	}
	// wireMaxCtx points the primary's truncation-escalation hook at the MAX-CONTEXT model (overriding the
	// small utility's) — so a call the MIN-context primary truncates retries against the big-context model.
	wireMaxCtx := func(be *OpenAICompatBackend) {
		wireMaxCtxEscalation(be, maxCtxModel, maxCtxURL, cfg.MaxCtxTokens, cfg)
	}

	tier := func(be *OpenAICompatBackend) backends.Backend {
		var wrapped backends.Backend = be
		if util != "" && util != be.Model {
			u := NewOpenAICompat(cfg.applyTo(Options{BaseURL: be.BaseURL, Model: util, APIKey: be.APIKey}))
			if containsStr(u.Health().Models, util) { // the utility model is actually loaded
				wrapped = NewTiered(be, u)
			}
		}
		wireMaxCtx(be) // MAX-CONTEXT escalation: overrides the utility's hook when configured + loaded
		return wrapped
	}

	// try builds a backend from opts and returns it (tiered) iff its endpoint is up (Python _try).
	try := func(opts Options) backends.Backend {
		be := NewOpenAICompat(cfg.applyTo(opts))
		if be.Health().Up {
			return tier(be)
		}
		return nil
	}

	// tryLocal resolves a LOCAL LM Studio endpoint: ensure a model is actually SERVED (auto-loading
	// one from the probe list when the server is up but empty — the gap that left every call falling
	// back to the heuristic), then build the backend on the served id. Returns the precise error so
	// the caller can surface why (server down / nothing loadable / lms missing). `preferred` "" -> the
	// probe list leads with the package default.
	var localErr error
	tryLocal := func(base, apiKey, preferred string) backends.Backend {
		served, err := EnsureLocalModel(base, apiKey, preferred, 5*time.Minute)
		if err != nil {
			localErr = err
			return nil
		}
		be := NewOpenAICompat(cfg.applyTo(Options{BaseURL: base, Model: served, APIKey: apiKey}))
		if be.Health().Up {
			return tier(be)
		}
		localErr = errString("served model " + served + " is not healthy at " + base)
		return nil
	}

	// Explicit settings (TUI) win. A loopback endpoint is auto-loaded; a remote one is only health-checked.
	if cfg.BaseURL != "" || cfg.APIKey != "" || cfg.Model != "" {
		base := cfg.BaseURL
		if base == "" {
			base = defaultBaseURL
		}
		if isLocalURL(base) {
			if be := tryLocal(base, cfg.APIKey, cfg.Model); be != nil {
				return be, nil
			}
			return nil, BackendUnavailable("the configured local model is not reachable/loadable: " +
				errText(localErr))
		}
		if be := try(Options{BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.APIKey}); be != nil {
			return be, nil
		}
		return nil, BackendUnavailable("the configured model is not reachable (base_url=" +
			pyRepr(cfg.BaseURL) + ")")
	}

	if substrate == "auto" || substrate == "frontier" {
		if fc, ok := frontierConfig(); ok {
			if be := try(fc); be != nil {
				return be, nil
			}
			if substrate == "frontier" {
				return nil, BackendUnavailable("frontier endpoint configured but not reachable: " + fc.BaseURL)
			}
		} else if substrate == "frontier" {
			return nil, BackendUnavailable(
				"no frontier model configured — set ANTHROPIC_API_KEY, or THOUGHT_LLM_API_KEY + a " +
					"remote THOUGHT_LLM_BASE_URL (configure it in the TUI settings)")
		}
	}

	if substrate == "auto" || substrate == "local" {
		if be := tryLocal(defaultBaseURL, "", ""); be != nil { // local LM Studio: served or auto-loaded
			return be, nil
		}
		if substrate == "local" {
			return nil, BackendUnavailable("no local model at " + defaultBaseURL + ": " + errText(localErr))
		}
	}

	tail := ""
	if localErr != nil {
		tail = " (" + errText(localErr) + ")"
	}
	return nil, BackendUnavailable(
		"no reachable model — the harness needs an LLM and has no offline path. Start a local model " +
			"(LM Studio at " + defaultBaseURL + ") or configure a frontier endpoint in the TUI settings." + tail)
}

// wireMaxCtxEscalation points be's truncation-escalation hook at the MAX-CONTEXT model — the min/max-
// context routing. When a max-context model is configured (maxCtxModel) and ACTUALLY loaded, a call the
// MIN-context primary truncates (the qwen "thinking substrate unavailable" gap, or a truncated-invalid
// structured payload) gets ONE final attempt against the bigger model. The big model may live on its OWN
// endpoint (maxCtxURL — a second LM Studio at concurrency 1 with a big window) or the primary's; an empty
// maxCtxURL means the primary's. maxCtxTokens is its (bigger) completion budget (0 → the env default).
//
// It is a deliberate NO-OP — the hook stays nil, ZERO behaviour change — when: no max-context model is
// configured, the configured model is the SAME model on the SAME endpoint (nothing to escalate TO), or
// the model isn't actually loaded at the target endpoint. Extracted from ResolveSubstrate so the wiring
// (incl. the loaded-check) is unit-testable without the local-resolve / lms machinery.
func wireMaxCtxEscalation(be *OpenAICompatBackend, maxCtxModel, maxCtxURL string, maxCtxTokens int, cfg SubstrateConfig) {
	if maxCtxModel == "" {
		return
	}
	url := maxCtxURL
	if url == "" {
		url = be.BaseURL
	}
	if maxCtxModel == be.Model && url == be.BaseURL { // same model on the same endpoint ⇒ nothing to escalate to
		return
	}
	o := cfg.applyTo(Options{BaseURL: url, Model: maxCtxModel, APIKey: be.APIKey})
	o.MaxTokens = maxCtxTokens
	if o.MaxTokens == 0 {
		o.MaxTokens = envInt("THOUGHT_LLM_MAXCTX_TOKENS", 24000) // big-budget default for the big model
	}
	mc := NewOpenAICompat(o)
	if containsStr(mc.Health().Models, maxCtxModel) { // the max-context model is actually loaded
		be.structuredEscalate = mc.escalateStructured
	}
}

// WireMaxCtxFromEnv wires be's truncation-escalation hook from the THOUGHT_LLM_MAXCTX_* env vars — the
// min/max-context routing — for callers that build a backend DIRECTLY (the bench runner / trprobe) rather
// than through ResolveSubstrate. It is the env-only entry to wireMaxCtxEscalation: a NO-OP (zero behaviour
// change) when THOUGHT_LLM_MAXCTX_MODEL is unset, the model isn't loaded at the target endpoint, or it is
// the same model on the same endpoint. The completion budget defaults to THOUGHT_LLM_MAXCTX_TOKENS (24000).
func WireMaxCtxFromEnv(be *OpenAICompatBackend) {
	wireMaxCtxEscalation(be, os.Getenv("THOUGHT_LLM_MAXCTX_MODEL"), os.Getenv("THOUGHT_LLM_MAXCTX_URL"), 0, SubstrateConfig{})
}

// errText renders an error as a string, or "unknown" for nil (used to fold the precise local-resolve
// failure into the BackendUnavailable message).
func errText(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

// ---------------------------------------------------------------------------
// doctor: ProbeBackend
// ---------------------------------------------------------------------------

// ProbeResult is one subsystem's probe outcome (the Python per-subsystem dict). For an LLM backend
// the call-log fields (MS/ModelAnswered/Raw/CallError/Finish) are populated from the last call.
type ProbeResult struct {
	Subsystem     string
	Output        string // the method's return rendered to text ("" when the method has no string return)
	FellBack      bool
	Exception     string
	HasCallLog    bool // true when the LLM call-log fields below are meaningful
	MS            int
	ModelAnswered bool
	Raw           string
	CallError     string
	Finish        string
}

// ProbeBackend exercises each Backend method once with a representative input, isolating each
// subsystem. Returns per-subsystem results incl. (for an LLM backend) latency, whether the model
// answered, whether it fell back, and the FULL raw model output. Used by `thought doctor`. Mirrors
// Python probe_backend (same probe set + ordering).
func ProbeBackend(backend backends.Backend) []ProbeResult {
	rng := cpyrand.New(0)
	// Two opposing stance candidates — the doctor's probe fixtures for the admission floor + rank (do
	// they trust + rank as expected) and the Filter escalation. After M2 the stance roles are
	// skeptic/advocate (the deleted safety/refactor).
	ctx := []types.Thought{{ID: 1, Text: "should I rewrite the parser?", Source: types.GENERATED}}
	skepticDom := "skeptic"
	advocateDom := "advocate"
	unsafe := "unsafe"
	safe := "safe"
	risky := types.Candidate{Text: "this is risky — there are edge cases that could regress",
		Source: types.INJECTED, Domain: &skepticDom, Relevance: 0.75, Stance: &unsafe}
	safeC := types.Candidate{Text: "the change looks safe — behaviour is preserved",
		Source: types.INJECTED, Domain: &advocateDom, Relevance: 0.72, Stance: &safe}

	llmBe := asLLM(backend) // nil for the test double
	fallbacks := func() int {
		if llmBe != nil {
			return llmBe.Fallbacks
		}
		return 0
	}

	type probe struct {
		name string
		run  func() string
	}
	// The CONTENT roles are model-backed (they show OK/FALLBACK on the LLM backend). The CONTROL
	// roles (the admission FLOOR + candidate RANK) are pure Pattern-A math in internal/control —
	// they make NO model call, so they are probed directly and reported as deterministic control.
	probes := []probe{
		{"conscious.generate", func() string { return backend.Generate("should I rewrite the parser?", ctx, rng) }},
		{"seam.transform", func() string { return backend.Transform(risky, ctx) }},
		{"conscious.compress", func() string { return backend.Summarize(ctx) }},
	}

	var results []ProbeResult
	for _, p := range probes {
		fb0 := fallbacks()
		out := p.run() // a Backend method should never panic; defensive recovery is a belt below
		entry := ProbeResult{Subsystem: p.name, Output: out, FellBack: fallbacks() > fb0}
		fillCallLog(&entry, llmBe)
		results = append(results, entry)
	}

	// CONTROL probes — deterministic, no backend, no model call (Pattern A). The admission FLOOR
	// and candidate RANK live in internal/control; report them so the doctor shows them as control,
	// not as a model-backed role that could fall back. FellBack is always false (no model touched).
	floor := control.ScoreAdmit(risky, ctx, 0.5)
	results = append(results, ProbeResult{
		Subsystem: "Filter.score_admit (control floor)",
		Output:    floor.Verdict.String() + " " + ftoa2(floor.Confidence) + " [deterministic, no model call]",
	})
	rankScores, _ := control.Rank([]types.Candidate{risky, safeC}, ctx)
	results = append(results, ProbeResult{
		Subsystem: "Gate.rank (control)",
		Output:    floatsToText(rankScores) + " [deterministic, no model call]",
	})

	// The Filter ESCALATION is the one model-backed admission touchpoint (Pattern C): probe it only
	// when the backend implements FilterEscalator (the LLM backend). It is GIVEN the floor verdict so
	// the model REFINES rather than re-derives; refined=false means the model declined and the floor
	// stands (Rule 4) — which the doctor surfaces as FellBack (no escalation happened).
	if esc, ok := backend.(backends.FilterEscalator); ok && llmBe != nil {
		fb0 := fallbacks()
		v, refined := esc.JudgeAdmission(risky, ctx, floor)
		out := v.Verdict.String() + " " + ftoa2(v.Confidence)
		if !refined {
			out += " (floor stood — model declined/unavailable)"
		}
		entry := ProbeResult{Subsystem: "Filter.judge_admission (escalation)", Output: out,
			FellBack: !refined || fallbacks() > fb0}
		fillCallLog(&entry, llmBe)
		results = append(results, entry)
	}

	// sub-agent (specialist) probes — the domain-scoped LLM calls (only the LLM backend has them).
	// After M2 the model-driven roles are the two stance primitives skeptic/advocate (the fork-on-
	// conflict pair); they replace the deleted safety/refactor canned specialists.
	if sc, ok := backend.(backends.SpecialistCaller); ok && llmBe != nil {
		for _, ds := range []struct{ dom, desc string }{
			{"skeptic", "you flag risks and edge cases, with a reason"},
			{"advocate", "you judge whether behaviour is preserved, with a reason"},
		} {
			fb0 := fallbacks()
			out, _ := sc.Specialist(ds.dom, ds.desc, ctx)
			entry := ProbeResult{Subsystem: "specialist." + ds.dom, Output: out,
				FellBack: fallbacks() > fb0}
			fillCallLog(&entry, llmBe)
			results = append(results, entry)
		}
	}
	return results
}

// fillCallLog populates the LLM call-log fields of a ProbeResult from the backend's last call (only
// for an LLM backend with a non-empty ring). Mirrors Python `if is_llm and backend.log`.
func fillCallLog(entry *ProbeResult, be *OpenAICompatBackend) {
	if be == nil {
		return
	}
	if rec, ok := be.Log.last(); ok {
		entry.HasCallLog = true
		entry.MS = rec.MS
		entry.ModelAnswered = rec.OK
		entry.Raw = rec.Raw
		entry.CallError = rec.Error
		entry.Finish = rec.FinishReason
	}
}

// asLLM returns the underlying OpenAICompatBackend (the call-log carrier) for a bare or tiered LLM
// backend, or nil for the test double. Python tested `hasattr(backend, 'log')`; here the call log
// lives on the primary OpenAICompatBackend.
func asLLM(b backends.Backend) *OpenAICompatBackend {
	switch x := b.(type) {
	case *OpenAICompatBackend:
		return x
	case *TieredBackend:
		return x.Primary
	default:
		return nil
	}
}

// containsStr reports whether xs contains s.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// floatsToText renders a score slice for the doctor probe output (a compact "[0.80 0.72]").
func floatsToText(xs []float64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(ftoa2(x))
	}
	b.WriteByte(']')
	return b.String()
}
