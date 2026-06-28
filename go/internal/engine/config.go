// Package engine is the orchestrator — the main loop, wrapped in the lifecycle state machine
// (Tier 7, PORT-PLAN #38, the HARDEST overall: the integration spec).
//
// It wires every subsystem together and drives one TICK at a time, emitting events throughout
// (it never prints). mode="reactive" is the episodic regime (§7.1); mode="continuous" is the
// awake regime with arousal, drives, default-mode and async action. The two share an intake +
// decide core (_reason, in reactive.go). Construct one with NewEngine, Submit a prompt, then
// Step or Run.
//
// Ported from the (now-removed) Python thought_harness/engine.py. config.go holds EngineConfig + StepResult;
// engine.go holds the Engine struct + the constructor that binds ~20 collaborators; reactive.go
// holds _step_reactive + the shared _reason core; continuous.go holds _step_continuous.
package engine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// DefaultStateDir returns the auto-persistence directory for a self-contained profile (e.g. the awake
// profiles, which persist their learned state so a session's memory survives). It is SUBSTRATE-TAGGED —
// each substrate class gets its own dir — to honour the substrate-hygiene rule (never mix a frontier
// registry with local-minted state in one store). Returns "" for the offline test double (no point
// persisting deterministic state), so the caller skips persistence entirely.
func DefaultStateDir(substrate string) string {
	s := strings.TrimSpace(substrate)
	if s == "" || s == "test" || s == "none" {
		return ""
	}
	return filepath.Join("data", "state", s)
}

// EngineConfig mirrors the Python @dataclass EngineConfig. The defaults are applied by
// DefaultConfig (Go has no field defaults); the substrate default reads THOUGHT_SUBSTRATE
// exactly as Python's field(default_factory=...).
type EngineConfig struct {
	Mode      string // reactive | continuous (Python default "reactive")
	Seed      int    // seeded RNG (Python default 7)
	MaxTicks  int    // run() budget (Python default 60)
	Cognition string // Controller mode: control | llm | hybrid (default "control")

	// Profile is the active cognition PROFILE name (config.Profiles()) — a one-pick knob bundle. Set
	// by `--profile` / the TUI Settings picker; it determines Features + Mode. "" ⇒ no named profile
	// (the raw Features/Mode). Display + round-trip only — the behaviour comes from Features/Mode.
	Profile string

	// AwakeUserBudget is the continuous-mode deliver DEADLINE for a user turn. The endless awake
	// stream never gives up, but a line that holds an UNANSWERED user turn is a BOUNDED
	// conversational obligation: after this many awake steps working it without the goal being
	// satisfied, the mind ANSWERS the user (give-up -> DELIVER) instead of wandering off and setting
	// the turn aside forever. Without it, a conversational turn a real substrate never trips
	// GoalSatisfied on (a greeting / chitchat) is thought about and set aside indefinitely — the
	// awake-mode "won't answer" bug. It guards ONLY a user-holding line; the wander stays unbounded.
	// Keep it < the focus bound (9) so the deliver fires before bounded-focus would preempt the line.
	// 0 ⇒ disabled. Reactive mode ignores it (the episodic MaxSteps give-up already guarantees a reply).
	AwakeUserBudget int

	// ContextBudget caps the working context (in word-size) via the zoomable working set (P4.1). 0 (the
	// default) is OFF — the engine sees the raw active-branch context, byte-identical to before. When
	// set, the context the synthesiser/responder see is the budget-bounded, multi-level-compressed
	// working set (relevant-old kept over irrelevant-recent; nothing silently dropped — folded to a
	// pointer). Off by default so it is opt-in and the scenario goldens are unchanged.
	ContextBudget int

	// Proactive outreach (awake mode): the engine may reach out to the user UNPROMPTED when an
	// endogenous line concludes with above-baseline value — cooldown-gated so it stays durable, not
	// spammy. Off in reactive mode (where every response is a reply to a user goal).
	Proactive        bool    // Python default true
	OutreachCooldown int     // min ticks between two proactive outreach messages (Python default 8)
	ProactivityFloor float64 // the concluded line must clear this value to be worth sharing (Python default 0.2)

	// InboxMaxEscalations bounds the async inbox push channel (O-5): when conscious.activity.inbox_escalation
	// is ON, an unacknowledged proactive outreach is re-surfaced with escalating urgency AT MOST this many
	// times before it is dropped silently — the durability bound that keeps the awake utterance count finite
	// (the LATHE 7-identical-outreaches UAT bug is structurally impossible). Each re-push is gated by a
	// strictly-longer cooldown than first contact (1.5x OutreachCooldown), so re-surfacing is slower than the
	// base channel. Default 2. Only consulted when the inbox_escalation knob is ON.
	InboxMaxEscalations int

	// The Action layer's real-tool workspace. When set, the watched seam's FrontActuator dispatches
	// real effects (run tests/code, read/write files) scoped+sandboxed to this dir, so "open to
	// reality" imports genuine ground truth. "" -> the offline heuristic fallback (tests/CI).
	Workspace   string  // Python default None ("")
	ToolTimeout float64 // per-command wall-clock cap for real tool execution (seconds; Python default 30.0)

	// The thinking substrate. The product DEFAULT is a real model (auto: frontier-if-configured,
	// else local, else a hard error — there is NO offline product path). "test" is a TEST
	// DOUBLE only; the tests pin it via THOUGHT_SUBSTRATE=test, never the product.
	Substrate  string // Python field(default_factory=lambda: os.environ.get("THOUGHT_SUBSTRATE","auto"))
	LLMBaseURL string // explicit model endpoint (TUI settings) — overrides auto (Python None)
	LLMModel   string // the PRIMARY (reasoning) model (Python None)
	LLMAPIKey  string // Python None
	// A small UTILITY model for trivial roles (summaries / progress) — the big reasoning model is
	// wasted on a compaction gist. "" -> the primary model handles everything. (Tiered cognition.)
	LLMUtilityModel string // Python None

	// MIN/MAX-context routing: the PRIMARY is the small/fast MIN-context model (high concurrency); a
	// call it TRUNCATES (ran out of budget mid-thought — the reasoning-model gap) escalates ONCE to the
	// MAX-context model. The big model is run by the user with a BIG window + concurrency 1, optionally
	// on its OWN endpoint (LLMMaxCtxURL). "" -> no escalation tier (the truncation gap surfaces as-is).
	LLMMaxCtxModel  string // the big-context escalation model ("" -> THOUGHT_LLM_MAXCTX_MODEL / disabled)
	LLMMaxCtxURL    string // its endpoint ("" -> THOUGHT_LLM_MAXCTX_URL, else the primary's)
	LLMMaxCtxTokens int    // its (bigger) completion budget (0 -> THOUGHT_LLM_MAXCTX_TOKENS, default 24000)

	// LLMMaxTokens overrides the per-call completion budget. 0 → THOUGHT_LLM_MAX_TOKENS (default
	// 4096) — reasoning headroom so the model doesn't run out mid-reasoning (the NOISY-RULER). The
	// reasoning-model robustness path (multi-provider field salvage, retry-on-truncation) reads its
	// remaining knobs from env (THOUGHT_LLM_SALVAGE / _RETRY_ON_TRUNCATION / _REASONING_FIELDS / …),
	// which the substrate threads through; this field is the one budget knob exposed on the engine.
	LLMMaxTokens int

	// Features is the unified system-wide HarnessConfig — the per-component on/off toggles + the
	// representation matrix (the representation-space rebuild, M1). nil ⇒ config.AllOn() (every
	// component ON, the pre-config behaviour, so scenario goldens are byte-identical with Features=nil).
	// Each component reads its toggle through a config.Gate and short-circuits to pass-through (emitting
	// config.skip) when it is OFF — a toggle never deletes a wire, it bypasses a decision.
	Features *config.HarnessConfig

	// FeaturesPath is the config FILE the Features were loaded from (display-only metadata — the TUI
	// Config tab shows it so a surprising run config is traceable, §4.6). "" ⇒ defaults/env only, no
	// file. It never affects engine behaviour; it is carried purely so the TUI can surface it.
	FeaturesPath string

	// Embedder is the INJECTED dense-retrieval embedder seam (A-RAG2). nil (the default) ⇒ the engine
	// resolves the embedder itself: behind subconscious.semantic_recall it PROBES the OpenAI-compatible
	// /v1/embeddings sidecar (retrieval.ProbeEmbedder), else (the legacy path) the incidental
	// ReachableEmbedder probe runs for a non-test backend. When set, the injected embedder is used
	// verbatim (no network dial) and reported on the retrieval.semantic announce — the test seam that
	// proves the dense channel lights up without a live sidecar. The engine stays headless-pure; the
	// embedder does its own network I/O in its package, the engine only calls Embed.
	Embedder retrieval.Embedder

	// Store is the INJECTED cross-session persistence port (the representation-space rebuild, M4). nil ⇒
	// in-memory only (the test/heuristic default — goldens never touch disk). When set, NewEngine calls
	// Store.Load() and seeds the registries (minted skills/operators/specialists, gate priors, episodes,
	// beliefs, knowledge, person prefs) BEFORE the first episode; each mint site also persists beside its
	// emit; Flush() runs on lifecycle→DONE; the persist.Curator runs at IDLE consolidation. The engine
	// stays headless-pure — the Store does its file I/O in its own package; the engine only calls it.
	Store persist.Store
}

// DefaultConfig returns EngineConfig with the Python dataclass defaults applied — including the
// THOUGHT_SUBSTRATE env read (default "auto") that Python performs via field(default_factory=...).
func DefaultConfig() EngineConfig {
	substrate := os.Getenv("THOUGHT_SUBSTRATE")
	if substrate == "" {
		substrate = "auto"
	}
	return EngineConfig{
		Mode:                "reactive",
		Seed:                7,
		MaxTicks:            60,
		Cognition:           "control",
		AwakeUserBudget:     6, // < focusBound (9): answer an awake user turn before bounded-focus preempts it
		Proactive:           true,
		OutreachCooldown:    8,
		ProactivityFloor:    0.2,
		InboxMaxEscalations: 2, // O-5: at most 2 re-pushes of an ignored outreach (durability bound)
		Workspace:           "",
		ToolTimeout:         30.0,
		Substrate:           substrate,
	}
}

// StepResult is the per-tick outcome handed back to the caller (Python @dataclass StepResult). Meta
// is a per-instance map (mutable-default discipline) — a copy of the Controller's last_meta.
type StepResult struct {
	Tick     int
	State    string
	Decision string // "" == Python None (no decision this tick)
	Note     string
	Idle     bool
	Meta     map[string]any // Python field(default_factory=dict)
}

// ----------------------------------------------------------------------------
// substantive-message screening for proactive outreach (engine-local, Python module scope)
// ----------------------------------------------------------------------------

// nonInsight are the empty fallbacks, the awake seed, and the internal-monologue / effortful
// templates — none worth broadcasting to the user as a "thought". Mirrors Python _NON_INSIGHT
// (substring matches, lower-cased).
var nonInsight = []string{
	"couldn't work that out", "couldn't reach a confident", "(awake",
	"mind is wandering", "don't have a grounded", "no specialist fired",
	"working it out from first principles", "grinding through it", "effortful step",
	"let me reason toward", "what does '",
}

// isSubstantive reports whether a message is worth saying to the user — not an empty/fallback
// non-answer, the awake seed, or internal monologue. Keeps proactive outreach quiet when there is
// nothing real to share. Mirrors Python _is_substantive: de-voice + lower-case, require >12 chars
// and none of the nonInsight substrings.
func isSubstantive(msg string) bool {
	low := strings.ToLower(types.StripVoice(strings.TrimSpace(msg)))
	if low == "" || len([]rune(low)) <= 12 {
		return false
	}
	for _, b := range nonInsight {
		if strings.Contains(low, b) {
			return false
		}
	}
	return true
}
