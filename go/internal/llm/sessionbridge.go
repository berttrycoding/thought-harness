package llm

// The SESSION bridge (--backend session, alias "cc") — the open Claude Code session IS the
// thinking substrate. No spawn, no login, no token: the harness POSTS each CONTENT call as a
// job file into a spool directory and blocks for the result; the developer's already-open,
// already-authenticated Claude Code session (the agent you are talking to) watches the spool,
// answers each job with its own model, and writes the result file. This is the mapping doc's
// Phase-3 inversion (claude-code-substrate-mapping.md §3) with a file spool as the v1 channel
// (an open session cannot hot-add MCP servers; files need nothing).
//
// Protocol (one file pair per call, JSON):
//
//	<spool>/job-<n>.json     {"id","role","model","system","user","max_tokens"}   harness → session
//	<spool>/job-<n>.result.json   {"content":"..."} or {"error":"..."}            session → harness
//
// The harness polls for the result every 250ms up to the backend timeout; an absent/errored
// result surfaces the usual honest gap (Pattern B — never a substituted template). Job ids are
// monotonic per backend instance; stale files from a previous run are ignored by id.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// DefaultSessionSpool is the spool directory used when THOUGHT_SESSION_SPOOL is unset.
const DefaultSessionSpool = "/tmp/thought-cc-spool"

// SessionHeartbeatFile is touched by the MCP server (cmd/thought/mcp.go) on every cc_get_job /
// cc_submit_result, i.e. on every worker poll. Its freshness is the only honest liveness signal a
// worker is actually servicing the spool — the spool DIR being creatable says nothing (S4).
const SessionHeartbeatFile = "worker.heartbeat"

// sessionHeartbeatTTL is how recent the heartbeat must be to count a worker "alive". It must exceed the
// worker's idle poll interval (each poll is a model turn, a few-to-~20s) with margin, so a slow-but-alive
// worker is never reported dead.
const sessionHeartbeatTTL = 60 * time.Second

// sessionWorkerFresh reports whether a worker is actively servicing the spool: the heartbeat file exists
// and was touched within sessionHeartbeatTTL. age is the time since the last touch (0 when absent).
func sessionWorkerFresh(spool string) (fresh bool, age time.Duration) {
	fi, err := os.Stat(filepath.Join(spool, SessionHeartbeatFile))
	if err != nil {
		return false, 0
	}
	age = time.Since(fi.ModTime())
	return age <= sessionHeartbeatTTL, age
}

// SessionSpoolDir resolves the spool directory the session bridge + worker share (THOUGHT_SESSION_SPOOL,
// else the default). Exported so the TUI can probe worker liveness before a goal hangs (S1).
func SessionSpoolDir() string { return envOr("THOUGHT_SESSION_SPOOL", DefaultSessionSpool) }

// SessionWorkerFresh is the exported worker-liveness probe (a fresh heartbeat ⇒ a worker is polling).
func SessionWorkerFresh(spool string) (bool, time.Duration) { return sessionWorkerFresh(spool) }

// sessionJob is the wire format the open session's worker loop reads.
type sessionJob struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Model     string `json:"model"` // tier hint: the primary/utility model alias
	System    string `json:"system"`
	User      string `json:"user"`
	MaxTokens int    `json:"max_tokens"`
}

// sessionResult is the wire format the worker writes back.
type sessionResult struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// NewSessionBridge builds the session-spool backend: the same OpenAICompatBackend role logic
// with the transport swapped for the job-file round-trip. Tiered like the exec bridge so role
// tier hints (primary vs utility) reach the worker.
func NewSessionBridge(spool string, maxTokens int, timeout time.Duration) backends.Backend {
	if spool == "" {
		spool = envOr("THOUGHT_SESSION_SPOOL", DefaultSessionSpool)
	}
	// The result-wait deadline is the SAFETY ceiling, not the completion signal: the transport
	// returns the instant the result file appears (async completion), and a DEAD worker is recovered
	// fast by the MCP server's stale-claim re-queue (cmd/thought/mcp.go) long before this fires. It is
	// env-configurable rather than a hardcoded magic number; 0 ⇒ the env / OpenAICompat default.
	if timeout == 0 {
		if s := envInt("THOUGHT_SESSION_TIMEOUT_SECONDS", 0); s > 0 {
			timeout = time.Duration(s) * time.Second
		}
	}
	primary := newSessionTier(spool, "session", maxTokens, timeout)
	utility := newSessionTier(spool, "session-utility", maxTokens, timeout)
	return NewTiered(primary, utility)
}

// sessionLabel builds the provenance label for a session tier: the servicing worker's identity
// (THOUGHT_SESSION_WORKER, e.g. "opencode", "cc") prefixed to the tier, or just the tier when the
// worker is unspecified (the de-branded generic default — "session" / "session-utility"). cc:* is
// produced only when explicitly requested (THOUGHT_SESSION_WORKER=cc), for continuity with old tags.
func sessionLabel(tier string) string {
	if w := os.Getenv("THOUGHT_SESSION_WORKER"); w != "" {
		return w + ":" + tier
	}
	return tier
}

func newSessionTier(spool, model string, maxTokens int, timeout time.Duration) *OpenAICompatBackend {
	b := NewOpenAICompat(Options{BaseURL: "session-spool", Model: model, MaxTokens: maxTokens, Timeout: timeout})
	// The session bridge is HARNESS-AGNOSTIC — any CLI worker (opencode, codex, claude, …) can service
	// the spool, so the provenance label is no longer hardcoded "cc:" (Claude Code). It is the servicing
	// worker's identity from THOUGHT_SESSION_WORKER (set when you launch the run knowing which worker
	// will answer); the default is the de-branded tier name itself. This is the Meta.Substrate tag, so a
	// run answered by an opencode worker is provenance-tagged "opencode:session", not mislabeled claude.
	b.displayName = sessionLabel(model)
	b.substrateClass = "session"

	b.transport = sessionSpoolTransport(spool, b)
	b.transportHealth = func() HealthReport {
		if err := os.MkdirAll(spool, 0o755); err != nil {
			return HealthReport{Up: false, Error: "spool unavailable: " + err.Error(), Models: []string{}}
		}
		// Up iff a worker is actually servicing the spool (a fresh heartbeat), not merely that the dir
		// exists — else the first call would hang on a worker that isn't there (S4).
		if fresh, age := sessionWorkerFresh(spool); !fresh {
			why := "no worker servicing the spool — start tools/cc-worker.sh"
			if age > 0 {
				why = "worker stale (last seen " + age.Round(time.Second).String() + " ago) — restart tools/cc-worker.sh"
			}
			return HealthReport{Up: false, Error: why, Models: []string{model}}
		}
		return HealthReport{Up: true, Models: []string{model}}
	}
	return b
}

// sessionSeq numbers jobs across all tiers in-process so files never collide.
var sessionSeq atomic.Int64

// sessionSpoolTransport implements the postChat contract over the job-file round-trip, with
// BOUNDED RETRY: a per-attempt deadline detects a transient worker gap (a keeper-respawn window,
// an overloaded pool under sustained load) and RE-POSTS the identical job under a fresh id rather
// than losing the call. This is transport-level REDELIVERY to a live worker — NOT fabrication (the
// honest gap still surfaces, but only after every attempt times out). A worker that answers with an
// ERROR is a real result, returned immediately (never retried). Without this, sustained high-
// concurrency runs lost ~half their misses to "substrate unavailable" timeouts (measured: K=3/C=4).
// Env: THOUGHT_SESSION_ATTEMPT_TIMEOUT_SECONDS (per-attempt, default 90), THOUGHT_SESSION_MAX_ATTEMPTS
// (default 3). The legacy single-deadline behaviour is THOUGHT_SESSION_MAX_ATTEMPTS=1.
func sessionSpoolTransport(spool string, b *OpenAICompatBackend) func(map[string]any, bool) (postResult, error) {
	attemptTimeout := time.Duration(envInt("THOUGHT_SESSION_ATTEMPT_TIMEOUT_SECONDS", 90)) * time.Second
	if b.Timeout > 0 && b.Timeout < attemptTimeout {
		attemptTimeout = b.Timeout // an explicit shorter ceiling wins
	}
	maxAttempts := envInt("THOUGHT_SESSION_MAX_ATTEMPTS", 3)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return func(reqBody map[string]any, _ bool) (postResult, error) {
		res := postResult{reasoningTokens: -1, promptTokens: -1, completionTokens: -1,
			totalTokens: -1, cachedInputTokens: -1, cacheMissTokens: -1}
		if err := os.MkdirAll(spool, 0o755); err != nil {
			return res, errString("spool unavailable: " + err.Error())
		}
		// Build the job content once; each attempt re-posts it under a fresh id.
		tmpl := sessionJob{}
		tmpl.Model, _ = reqBody["model"].(string)
		if msgs, ok := reqBody["messages"].([]map[string]string); ok {
			for _, m := range msgs {
				switch m["role"] {
				case "system":
					tmpl.System = m["content"]
				case "user":
					tmpl.User = m["content"]
				}
			}
		}
		if mt, ok := reqBody["max_tokens"].(int); ok {
			tmpl.MaxTokens = mt
		}

		for attempt := 0; attempt < maxAttempts; attempt++ {
			job := tmpl
			job.ID = sessionSeq.Add(1)
			base := filepath.Join(spool, "job-"+strconv.FormatInt(job.ID, 10))
			resultPath := base + ".result.json"
			data, err := json.Marshal(job)
			if err != nil {
				return res, err
			}
			// Write-then-rename so the worker never reads a half-written job.
			if err := os.WriteFile(base+".json.tmp", data, 0o644); err != nil {
				return res, errString("spool write: " + err.Error())
			}
			if err := os.Rename(base+".json.tmp", base+".json"); err != nil {
				return res, errString("spool write: " + err.Error())
			}
			deadline := time.Now().Add(attemptTimeout)
			for time.Now().Before(deadline) {
				if raw, err := os.ReadFile(resultPath); err == nil {
					_ = os.Remove(base + ".json")
					_ = os.Remove(base + ".claimed") // release the worker's claim marker (multi-worker protocol)
					_ = os.Remove(resultPath)
					var sr sessionResult
					if jerr := json.Unmarshal(raw, &sr); jerr != nil {
						return res, errString("session result unparseable: " + jerr.Error())
					}
					if sr.Error != "" {
						return res, errString("session: " + head(sr.Error, 160))
					}
					res.content = strings.TrimSpace(sr.Content)
					res.finish = "stop"
					if res.content == "" {
						return res, errString("empty completion (session)")
					}
					return res, nil
				}
				time.Sleep(250 * time.Millisecond)
			}
			// This attempt timed out: withdraw its job (+ any orphaned claim) and re-post on the next loop.
			_ = os.Remove(base + ".json")
			_ = os.Remove(base + ".claimed")
		}
		return res, errString("session: no worker answered after " + strconv.Itoa(maxAttempts) +
			" attempts (" + attemptTimeout.String() + " each)")
	}
}
