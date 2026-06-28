package realhard

// gap2_live_validation_test.go — LIVE-CLAUDE directional grounding re-A/B for the gap-2 Context
// capture-timing fix (commit bb85e15, behind subconscious.capability, default-OFF). VALIDATION ONLY —
// no engine code is changed by this file; it builds engines with the capability flag flipped and drives
// the realhard grounding material on the real substrate.
//
// THE PRIOR RESULT (pre-fix): capability ON STARVED a mid-episode worker — the only Context capture ran
// at episode-OPEN (goal root, 1 thought), frozen, and workerSlice blindly preferred it over the live
// tail. Grounding COLLAPSED: OFF 2/3 -> ON 0/3 (net-negative).
//
// THE FIX: capture at STAFFING time (Workflow.Instantiate re-captures against the LIVE grown graph) +
// workerSlice prefers the RICHER of {captured, live tail} (never starves).
//
// THE GATE (directional): on live claude does ON match-or-beat OFF on grounding (no longer collapse to 0)?
// K is small (matches the prior K=3) and inside the +/-56pp Bernoulli noise floor, so this proves "no
// longer net-negative + the capability path engages", NOT a clean lift magnitude. The DETERMINISTIC
// grown-branch mechanism check lives in internal/engine (TestLiveClaudeGap2StaffsGrownBranch), where the
// staffing seam's unexported collaborators are reachable.
//
// Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes). Run:
//
//	THOUGHT_LIVE_CLAUDE=1 go test ./internal/bench/realhard -run TestLiveClaudeGap2 -v -timeout 1800s

import (
	"os"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// liveClaudeFactory builds the same claude bridge cmd/realhard uses (sonnet primary + haiku utility).
func liveClaudeFactory(t *testing.T) BackendFactory {
	t.Helper()
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate validation (claude bridge — costs tokens + time)")
	}
	return func(_ int64, _ float64) backends.Backend {
		be, err := llm.MakeBackend("claude", "", "auto", 0)
		if err != nil {
			t.Fatalf("build claude backend: %v", err)
		}
		return be
	}
}

// gap2Task is the worker-routing grounding task used for the re-A/B — the back-0001 DEPRECATED/SUPERSEDED
// material (the obvious const is dead; the active value is a renamed const further down the SAME file).
// A worker that reads the WHOLE grown branch (the gap-2 fix) can follow the supersession; a starved worker
// stops at the first hit. This is exactly the worker-routing grounding shape the prior validation used.
func gap2Task(t *testing.T) Task {
	t.Helper()
	for _, tk := range FilterTasks(Tasks(), "back-0001") {
		return tk
	}
	t.Fatal("realhard back-0001 task not found")
	return Task{}
}

// runGap2Episode runs ONE grounding episode of the task on the given backend with capability set to `on`,
// in a fresh materialized workspace, and returns (solved, grounded, scopeFired). It mirrors RunHarness's
// episode shape but flips the capability flag and watches the staffing seam (subconscious.scope fires only
// when the Capability path engaged). Validation-only — no engine code is modified.
func runGap2Episode(t *testing.T, tk Task, factory BackendFactory, seed int64, maxTicks int, on bool) (solved, grounded, scopeFired bool) {
	t.Helper()
	ws, err := os.MkdirTemp("", "gap2-live-*")
	if err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	defer os.RemoveAll(ws)
	if err := materialize(ws, tk.Materials); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	be := factory(seed, DefaultTemperature)
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = int(seed)
	cfg.MaxTicks = maxTicks
	cfg.Cognition = "control"
	cfg.Workspace = ws
	feat := config.New() // AllOn (capability defaults OFF even here — the opt-in exception)
	feat.Subconscious.Capability = on
	cfg.Features = feat

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	eng.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.SubScope {
			scopeFired = true
		}
	})

	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(tk.Prompt)
	eng.Run(maxTicks)
	grounded = eng.Grounding().Len() > groundBefore
	solved = Score(tk, harnessAnswer(eng)).Solved
	return solved, grounded, scopeFired
}

// TestLiveClaudeGap2GroundingAB is the DIRECTIONAL grounding re-A/B: the same worker-routing grounding
// task, OFF vs ON, at K=3 (matches the prior K), on live claude. The GATE is directional: ON must no
// longer COLLAPSE (the prior 0/3) — match-or-beat OFF = the net-negative is fixed. K is inside the
// +/-56pp noise floor, so this is NOT a clean lift claim.
func TestLiveClaudeGap2GroundingAB(t *testing.T) {
	factory := liveClaudeFactory(t)
	tk := gap2Task(t)
	const K = 3

	var offSolved, offGround, onSolved, onGround, onScope int
	for k := 0; k < K; k++ {
		seed := int64(100 + k)
		s, g, _ := runGap2Episode(t, tk, factory, seed, DefaultMaxTicks, false)
		if s {
			offSolved++
		}
		if g {
			offGround++
		}
		t.Logf("OFF replay %d (seed %d): solved=%v grounded=%v", k, seed, s, g)
	}
	for k := 0; k < K; k++ {
		seed := int64(100 + k)
		s, g, sc := runGap2Episode(t, tk, factory, seed, DefaultMaxTicks, true)
		if s {
			onSolved++
		}
		if g {
			onGround++
		}
		if sc {
			onScope++
		}
		t.Logf("ON  replay %d (seed %d): solved=%v grounded=%v capability-engaged(scope)=%v", k, seed, s, g, sc)
	}

	t.Logf("=== GAP-2 DIRECTIONAL A/B (live claude, K=%d, task=%s) ===", K, tk.ID)
	t.Logf("    OFF: solved %d/%d  grounded %d/%d", offSolved, K, offGround, K)
	t.Logf("    ON : solved %d/%d  grounded %d/%d  capability-engaged %d/%d", onSolved, K, onGround, K, onScope, K)

	if onScope == 0 {
		t.Errorf("capability ON never engaged (subconscious.scope fired 0/%d) — the A/B did not actually test the ON path", K)
	}
	// THE GATE (directional): ON must not collapse below OFF. The prior net-negative was OFF 2/3 -> ON 0/3.
	if onSolved < offSolved {
		t.Errorf("ON solved (%d/%d) is BELOW OFF (%d/%d) — capability still degrades grounding (net-negative NOT fixed)",
			onSolved, K, offSolved, K)
	} else {
		t.Logf("DIRECTIONAL PASS: ON (%d/%d) matches-or-beats OFF (%d/%d) — no longer net-negative", onSolved, K, offSolved, K)
	}
	if onSolved == 0 && offSolved > 0 {
		t.Errorf("ON COLLAPSED to 0/%d while OFF solved %d/%d — the prior collapse signature recurred", K, offSolved, K)
	}
}
