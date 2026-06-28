package main

// `thought mcp-serve` — the MCP face of the SESSION bridge (--backend session / "cc").
//
// A minimal stdio MCP server (newline-delimited JSON-RPC 2.0, no deps) that exposes the
// cognition job queue to the OPEN Claude Code session: the session lists/calls these as
// ordinary MCP tools — no headless spawn, no login, no token. Backed by the same spool the
// engine's session transport writes (internal/llm/sessionbridge.go):
//
//	cc_get_job       pull the oldest pending cognition job (role, system, user, model tier)
//	cc_submit_result answer a job (the session's own model output)
//	cc_status        queue depth
//
// Register in .mcp.json; loop: cc_get_job → think → cc_submit_result → repeat.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/berttrycoding/thought-harness/internal/llm"
)

func cmdMCPServe(argv []string) int {
	spool := llm.DefaultSessionSpool
	if v := os.Getenv("THOUGHT_SESSION_SPOOL"); v != "" {
		spool = v
	}
	if len(argv) > 0 && argv[0] != "" && !strings.HasPrefix(argv[0], "-") {
		spool = argv[0]
	}
	_ = os.MkdirAll(spool, 0o755)

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	reply := func(id any, result any) {
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Fprintf(out, "%s\n", resp)
		out.Flush()
	}
	text := func(s string) map[string]any {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
	}

	for in.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil || req.Method == "" {
			continue
		}
		switch req.Method {
		case "initialize":
			reply(req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "thought-cc-bridge", "version": "0.1.0"},
			})
		case "tools/list":
			// Tools are HARNESS-AGNOSTIC: the generic session_* names are advertised primary so any
			// CLI worker (opencode, codex, claude, …) services the spool the same way. The cc_* names
			// are kept as back-compat ALIASES (the original Claude Code worker prompts call them); both
			// route to the same handlers in tools/call below.
			reply(req.ID, map[string]any{"tools": []map[string]any{
				{"name": "session_get_job",
					"description": "Pull the oldest pending cognition job from the thought harness (--backend session). Returns the job JSON ({id, role, model, system, user, max_tokens}) or 'none'. Answer it with your OWN reasoning, then call session_submit_result.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
				{"name": "session_submit_result",
					"description": "Answer a cognition job: write your model output back to the harness.",
					"inputSchema": map[string]any{"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "integer", "description": "the job id"},
							"content": map[string]any{"type": "string", "description": "your answer (the role's expected format — plain text or JSON per the job's system prompt)"},
							"error":   map[string]any{"type": "string", "description": "set INSTEAD of content to surface an honest gap"},
						},
						"required": []string{"id"}}},
				{"name": "session_status",
					"description": "Queue depth of the thought harness cognition spool.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
				// back-compat aliases (Claude Code workers call these):
				{"name": "cc_get_job", "description": "Alias of session_get_job (back-compat).",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
				{"name": "cc_submit_result", "description": "Alias of session_submit_result (back-compat).",
					"inputSchema": map[string]any{"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "integer", "description": "the job id"},
							"content": map[string]any{"type": "string", "description": "your answer"},
							"error":   map[string]any{"type": "string", "description": "set INSTEAD of content to surface an honest gap"},
						},
						"required": []string{"id"}}},
				{"name": "cc_status", "description": "Alias of session_status (back-compat).",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			}})
		case "tools/call":
			var p struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			// Normalize the cc_* aliases to their session_* canonical names so one handler set serves both.
			switch normalizeSessionTool(p.Name) {
			case "session_get_job":
				spoolTouchHeartbeat(spool) // a poll == a live worker; the bridge's health probe reads its freshness (S4)
				raw, ok := spoolClaimNext(spool)
				if !ok {
					reply(req.ID, text("none"))
					break
				}
				reply(req.ID, text(raw))
			case "session_submit_result":
				spoolTouchHeartbeat(spool)
				id, _ := p.Args["id"].(float64)
				content, _ := p.Args["content"].(string)
				errMsg, _ := p.Args["error"].(string)
				if err := spoolSubmit(spool, int64(id), content, errMsg); err != nil {
					reply(req.ID, text("write failed: "+err.Error()))
					break
				}
				reply(req.ID, text("ok"))
			case "session_status":
				pending, claimed := spoolDepth(spool)
				reply(req.ID, text(fmt.Sprintf("pending=%d claimed=%d spool=%s", pending, claimed, spool)))
			default:
				reply(req.ID, text("unknown tool "+p.Name))
			}
		case "ping":
			reply(req.ID, map[string]any{})
		}
		// notifications (no id) need no reply
	}
	return 0
}

// ---------------------------------------------------------------------------
// Spool operations — the claim protocol (multi-worker safe).
//
// One spool can be serviced by N workers: cc_get_job CLAIMS the oldest pending
// job by atomically renaming job-<n>.json -> job-<n>.claimed (exactly one
// renamer wins; losers move to the next candidate). cc_submit_result writes the
// result and releases the claim. The engine only ever watches the result file,
// so claiming is invisible to it; its withdraw/consume paths clean up both the
// pending and the claimed variant (sessionbridge.go).
// ---------------------------------------------------------------------------

// normalizeSessionTool maps a worker's tool name to its canonical session_* form, so the
// harness-agnostic session_* names and the back-compat cc_* aliases route to one handler set.
// An unknown name passes through unchanged (falls to the default "unknown tool" reply).
func normalizeSessionTool(name string) string {
	switch name {
	case "cc_get_job":
		return "session_get_job"
	case "cc_submit_result":
		return "session_submit_result"
	case "cc_status":
		return "session_status"
	default:
		return name
	}
}

// spoolJob is one pending job file, keyed by its numeric id for oldest-first
// ordering (lexical sort would put job-10 before job-2).
type spoolJob struct {
	path string
	id   int64
}

// spoolPending lists pending (unclaimed) job files oldest-first by numeric id.
// The glob job-*.json also matches job-<n>.result.json — results are filtered.
func spoolPending(spool string) []spoolJob {
	paths, _ := filepath.Glob(filepath.Join(spool, "job-*.json"))
	jobs := make([]spoolJob, 0, len(paths))
	for _, p := range paths {
		if strings.HasSuffix(p, ".result.json") {
			continue
		}
		idStr := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "job-"), ".json")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		jobs = append(jobs, spoolJob{path: p, id: id})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].id < jobs[j].id })
	return jobs
}

// staleClaimSeconds is how long a CLAIMED job may sit without a result before it is
// treated as abandoned (the worker that claimed it died) and re-queued so another
// worker re-serves it. It MUST exceed the worst-case single-call think time (~20s
// observed) with margin, so a slow-but-alive worker is never robbed mid-job. Env
// THOUGHT_SESSION_STALE_CLAIM_SECONDS overrides; 0 disables reaping.
func staleClaimSeconds() int64 {
	if v := os.Getenv("THOUGHT_SESSION_STALE_CLAIM_SECONDS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 90
}

// reapStaleClaims re-queues abandoned claims: a job-N.claimed whose mtime is older
// than the staleness window had its worker die mid-job (a healthy worker submits in
// seconds). Atomically rename it back to job-N.json so the normal pending-claim path
// re-serves it — async failure recovery, so a dead worker no longer wedges a job until
// the engine's full timeout. The rename is the lock: exactly one server wins per file.
func reapStaleClaims(spool string, nowUnix int64) {
	stale := staleClaimSeconds()
	if stale <= 0 {
		return
	}
	claimed, _ := filepath.Glob(filepath.Join(spool, "job-*.claimed"))
	for _, c := range claimed {
		fi, err := os.Stat(c)
		if err != nil {
			continue
		}
		if nowUnix-fi.ModTime().Unix() < stale {
			continue
		}
		pending := strings.TrimSuffix(c, ".claimed") + ".json"
		_ = os.Rename(c, pending) // atomic re-queue; a loser just sees it gone
	}
}

// spoolClaimNext claims the oldest pending job and returns its JSON. It first re-queues
// any abandoned claims (a worker that claimed then died), then claims oldest-first.
// Losing the rename race (another worker claimed it, or the engine withdrew it on
// timeout) just moves to the next candidate.
func spoolClaimNext(spool string) (string, bool) {
	// mcp-serve is I/O infrastructure (a separate process from the seeded engine), so the wall clock
	// is permitted here — claim staleness is real elapsed time, not a deterministic tick.
	reapStaleClaims(spool, time.Now().Unix())
	for _, j := range spoolPending(spool) {
		claimed := strings.TrimSuffix(j.path, ".json") + ".claimed"
		if err := os.Rename(j.path, claimed); err != nil {
			continue
		}
		raw, err := os.ReadFile(claimed)
		if err != nil {
			continue
		}
		return string(raw), true
	}
	return "", false
}

// spoolSubmit writes the result file and releases the claim. The pending-file
// variant is removed too so a worker on the pre-claim protocol still cleans up.
func spoolSubmit(spool string, id int64, content, errMsg string) error {
	res, _ := json.Marshal(map[string]string{"content": content, "error": errMsg})
	base := filepath.Join(spool, fmt.Sprintf("job-%d", id))
	if err := os.WriteFile(base+".result.json", res, 0o644); err != nil {
		return err
	}
	_ = os.Remove(base + ".claimed")
	_ = os.Remove(base + ".json")
	return nil
}

// spoolTouchHeartbeat stamps the worker-liveness file (llm.SessionHeartbeatFile) to "now". Called on
// every cc_get_job / cc_submit_result — i.e. on every worker poll — so the session bridge's health probe
// can tell a worker is actually servicing the spool, not merely that the dir exists (S4). Best-effort:
// a failed touch never blocks serving a job.
func spoolTouchHeartbeat(spool string) {
	path := filepath.Join(spool, llm.SessionHeartbeatFile)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.WriteFile(path, []byte("alive"), 0o644) // create it the first time
	}
}

// spoolDepth counts pending (unclaimed) and claimed (in-flight) jobs.
func spoolDepth(spool string) (pending, claimed int) {
	all, _ := filepath.Glob(filepath.Join(spool, "job-*"))
	for _, p := range all {
		switch {
		case strings.HasSuffix(p, ".result.json"):
		case strings.HasSuffix(p, ".claimed"):
			claimed++
		case strings.HasSuffix(p, ".json"):
			pending++
		}
	}
	return pending, claimed
}
