package engine

// gap2_live_mechanism_test.go — the DETERMINISTIC live-claude mechanism check for the gap-2 Context
// capture-timing fix (commit bb85e15, behind subconscious.capability, default-OFF). VALIDATION ONLY — no
// engine code is changed by this file.
//
// This is the live-substrate twin of the offline TestCapabilityStaffsWorkerWithContextAndScope: instead of
// SYNTHETIC graph.Appends, it drives a REAL grounding episode on claude (a workspace + Submit + Step) so
// the conscious branch grows via real thinking, then — while the workflow is live each tick — stages the
// staffing exactly as the per-tick Dispatch does (workflow.Instantiate against the live grown graph) and
// records what the staffed worker receives. The staffed worker must read the GROWN branch — strictly MORE
// than the goal-root snapshot the episode-open capture froze (the PRE-FIX worker was pinned to that
// 1-thought snapshot and STARVED).
//
// The recapture is graph-derived (no model gate), so this is the decisive, deterministic check the task
// asks for — "does the worker get the grown branch on live claude now (yes/no)".
//
// Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes). Run:
//
//	THOUGHT_LIVE_CLAUDE=1 go test ./internal/engine -run TestLiveClaudeGap2 -v -timeout 1800s

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// buildLiveCapEngine builds a reactive engine on the claude bridge with subconscious.capability ON and a
// real workspace (so the read tools can ground) holding the gap-2 worker-routing material.
func buildLiveCapEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate validation (claude bridge — costs tokens + time)")
	}
	ws, err := os.MkdirTemp("", "gap2-live-mech-*")
	if err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	// the back-0001 DEPRECATED/SUPERSEDED material: the obvious const is dead, the active value is a
	// renamed const further down the SAME file. A worker that reads the GROWN branch can follow the
	// supersession; a starved worker stops at the first hit.
	limits := "package config\n\n" +
		"// MaxBatchSize is the ingestion batch cap.\n" +
		"// DEPRECATED (2026-02): superseded by IngestBatchLimit below — the\n" +
		"// pipeline no longer reads this constant. Left only for an old test.\n" +
		"const MaxBatchSize = 500\n\n" +
		"// IngestBatchLimit is the ACTIVE cap the ingestion pipeline reads as of\n" +
		"// the v3 rewrite. This is the value in force.\n" +
		"const IngestBatchLimit = 128\n"
	if err := os.MkdirAll(filepath.Join(ws, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "config", "limits.go"), []byte(limits), 0o644); err != nil {
		t.Fatalf("write limits.go: %v", err)
	}

	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Workspace = ws // non-empty -> the engine builds a real executor with read tools
	feat := config.New()
	feat.Subconscious.Capability = true
	cfg.Features = feat
	e, err := NewEngine(&cfg, be)
	if err != nil {
		os.RemoveAll(ws)
		t.Fatalf("NewEngine(claude): %v", err)
	}
	return e, ws
}

// TestLiveClaudeGap2StaffsGrownBranch is the deterministic mechanism check on the real substrate. It runs a
// real grounding episode on live claude with capability ON (via the proper Submit -> Step flow with a
// workspace), and each tick — while the workflow is live — stages the staffing exactly as the live dispatch
// does. It records the goal-root baseline (the episode-open snapshot, what the PRE-FIX worker was pinned to)
// and the MAX context a staffed worker received across the episode. The fix passes iff a staffed worker
// read strictly MORE than the goal root (the GROWN branch) at least once.
func TestLiveClaudeGap2StaffsGrownBranch(t *testing.T) {
	e, ws := buildLiveCapEngine(t)
	defer os.RemoveAll(ws)
	goal := "What is the maximum batch size the ingestion pipeline uses in this codebase? " +
		"Read config/limits.go. Report a single integer."

	var scopeFired bool
	e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.SubScope {
			scopeFired = true
		}
	})

	e.SubmitDefault(goal)

	goalRoot := -1     // the episode-open snapshot size (set once the episode opens)
	workerMax := 0     // the max context a staffed worker received across the episode
	maxLiveBranch := 0 // the largest active-branch length observed (proof the conscious grew)
	staffedAtLeastOnce := false

	// drive real ticks; each tick, if a workflow is live, stage the staffing the way Dispatch does and
	// inspect what the worker receives against the live grown graph. A modest tick budget keeps spend tight.
	for i := 0; i < 14; i++ {
		res := e.Step()
		if e.episodeContext != nil && goalRoot < 0 {
			goalRoot = len(e.episodeContext.WorkerContext()) // the frozen episode-open goal-root snapshot
		}
		if e.graph != nil {
			if n := len(e.graph.BranchThoughts(e.graph.ActiveBranch)); n > maxLiveBranch {
				maxLiveBranch = n
			}
		}
		wf := e.subconscious.Workflow()
		if wf != nil && !wf.Exhausted() {
			phase := wf.Current()
			subs := wf.Instantiate(phase, e.executor, e.cognitiveView())
			for _, sa := range subs {
				staffedAtLeastOnce = true
				if c := sa.Context(); c != nil {
					if n := len(c.WorkerContext()); n > workerMax {
						workerMax = n
					}
				}
			}
		}
		if res.Idle && !e.port.Pending() {
			break
		}
	}

	t.Logf("LIVE MECHANISM (capability ON): goal-root snapshot=%d  max live active-branch=%d  staffed-worker max context=%d  capability-engaged(scope)=%v  staffed=%v",
		goalRoot, maxLiveBranch, workerMax, scopeFired, staffedAtLeastOnce)

	if !scopeFired {
		t.Error("capability ON: subconscious.scope never fired — the capability path did not engage on the live substrate")
	}
	if !staffedAtLeastOnce {
		t.Fatal("no worker was ever staffed across the live episode — cannot evaluate the gap-2 mechanism (re-run / raise the tick budget)")
	}
	if maxLiveBranch <= goalRoot {
		t.Fatalf("the conscious branch never grew past the goal root on the live substrate (max=%d, goal-root=%d) — "+
			"the episode quiesced before thinking; the mechanism cannot be exercised this run (re-run)", maxLiveBranch, goalRoot)
	}
	if workerMax == 0 {
		t.Fatal("every staffed worker carried an EMPTY Context (gap-2 wire dead) — the worker would fall back to the <=5 slice")
	}
	if workerMax <= goalRoot {
		t.Fatalf("STARVED: the best-staffed worker got %d thoughts, not MORE than the goal-root snapshot (%d) — "+
			"the staffing-time recapture did NOT win on the live substrate (gap-2 fix dead, the prior failure recurs)",
			workerMax, goalRoot)
	}
	t.Logf("PASS: a staffed worker read the GROWN branch (%d > goal-root %d) on live claude — the gap-2 recapture fires on the real substrate",
		workerMax, goalRoot)
}
