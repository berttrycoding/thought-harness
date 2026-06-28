package realhard

// allflags_live_validation_test.go — LIVE-CLAUDE directional grounding A/B for the FULLY-ACTIVATED
// cognition redesign: ALL FOUR live-path flags ON TOGETHER for the first time —
// subconscious.capability + subconscious.capability_dispatch + convert.skill_reframe + convert.refine_loop.
// VALIDATION ONLY — no engine code is changed by this file; it builds engines with the four flags flipped
// as a set and drives the realhard worker-routing grounding material on the real substrate.
//
// THIS GATES THE FLAG-FLIP (the product decision). The durability half is gated in internal/stability
// (the all-flags-on plant cell + the n-diff-vs-awake-baseline). This is the BEHAVIOUR half on live claude:
//
//	(a) does the FULL WIRE fire on the real substrate? — the producing Capability sources a Scope ceiling
//	    (subconscious.scope), OWNS the permissive has-any dispatch entry (subconscious.entry), and recalls a
//	    reframed skill when one is seeded (subconscious.skill_match with reframed=true → a skill:<name> source).
//	(b) is grounding PRESERVE-OR-LIFT? — ON must NOT collapse below OFF (the gap-2 net-negative failure mode).
//
// HONEST NOISE CAVEAT: K is modest and inside the +/-56pp Bernoulli noise floor (memory
// project-realhard-per-task-p-map-measured). The GATE is DIRECTIONAL — "no net-negative + the wire fires
// live" — NOT a clean lift magnitude. A clean lift would need K>=15-20 paired (deferred).
//
// Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes). Run:
//
//	THOUGHT_LIVE_CLAUDE=1 go test ./internal/bench/realhard -run TestLiveClaudeAllFlags -v -timeout 2400s

import (
	"os"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// allFlagsTask is the worker-routing grounding task — back-0001 (the maximum-batch-size supersession: the
// obvious value is superseded later in the same materialized branch). A worker that reads the WHOLE grown
// branch (the capability rich-context staffing) can follow the supersession; a starved worker stops early.
func allFlagsTask(t *testing.T) Task {
	t.Helper()
	for _, tk := range FilterTasks(Tasks(), "back-0001") {
		return tk
	}
	t.Fatal("realhard back-0001 task not found")
	return Task{}
}

// wireFired records which redesign-wire events fired this episode (the live-firing confirm).
type wireFired struct {
	scope    bool // subconscious.scope — the Capability sourced the §3.3a authority ceiling (capability ON)
	entry    bool // subconscious.entry — the Capability owns the permissive has-any dispatch recognition (dispatch ON)
	produced bool // subconscious.synthesize from the Capability — the producing entry produced the workflow
	recall   bool // subconscious.skill_match with reframed=true — the Capability recalled a reframed skill (reframe ON)
}

// runAllFlagsEpisode runs ONE grounding episode with ALL FOUR redesign flags set to `on`, in a fresh
// materialized workspace, optionally seeding a reframed skill whose triggers match the goal so the recall
// path can engage. It returns (solved, grounded, the wire-fired record). Validation-only.
func runAllFlagsEpisode(t *testing.T, tk Task, factory BackendFactory, seed int64, maxTicks int, on bool,
	seedReframed bool) (solved, grounded bool, w wireFired) {
	t.Helper()
	ws, err := os.MkdirTemp("", "allflags-live-*")
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
	feat := config.New() // AllOn (the four redesign flags default OFF — the opt-in exception)
	feat.Subconscious.Capability = on
	feat.Subconscious.CapabilityDispatch = on
	feat.Convert.SkillReframe = on
	feat.Convert.RefineLoop = on
	cfg.Features = feat

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	// Seed a reframed (prompt-bodied) skill whose triggers match the back-0001 goal vocabulary, so the
	// Capability's recallReframed can fire on the ON path (the reframe-recall wire). OFF path ignores it.
	if on && seedReframed {
		_, ok := eng.Skills().MintReframedTriggered("batch_grounding", "general",
			"read the whole materialized branch end to end, follow any supersession, and report the FINAL active value",
			nil, []string{"batch", "ingestion", "pipeline", "maximum"}, "ground the active batch-size value")
		if !ok {
			t.Fatal("failed to seed the reframed skill for the recall wire")
		}
	}
	eng.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SubScope:
			w.scope = true
		case events.SubEntry:
			w.entry = true
		case events.SubSynthesize:
			if cap, _ := ev.Data["capability"].(string); cap != "" {
				w.produced = true
			}
		case events.SkillMatch:
			if rf, _ := ev.Data["reframed"].(bool); rf {
				w.recall = true
			}
		}
	})

	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(tk.Prompt)
	eng.Run(maxTicks)
	grounded = eng.Grounding().Len() > groundBefore
	solved = Score(tk, harnessAnswer(eng)).Solved
	return solved, grounded, w
}

// TestLiveClaudeAllFlagsGroundingAB is the DECISIVE endgame behaviour gate: ALL FOUR redesign flags ON
// vs ALL OFF (legacy), same worker-routing grounding task, K paired, on live claude. It confirms (a) the
// full wire FIRES live and (b) grounding is preserve-or-lift (no net-negative). A reframed skill is seeded
// on the ON path so the recall wire can engage. DIRECTIONAL — inside the noise floor, NOT a lift magnitude.
func TestLiveClaudeAllFlagsGroundingAB(t *testing.T) {
	factory := liveClaudeFactory(t)
	tk := allFlagsTask(t)
	const K = 4

	var offSolved, offGround, onSolved, onGround int
	var anyScope, anyEntry, anyProduced, anyRecall int
	for k := 0; k < K; k++ {
		seed := int64(200 + k)
		s, g, _ := runAllFlagsEpisode(t, tk, factory, seed, DefaultMaxTicks, false, false)
		if s {
			offSolved++
		}
		if g {
			offGround++
		}
		t.Logf("OFF replay %d (seed %d): solved=%v grounded=%v", k, seed, s, g)
	}
	for k := 0; k < K; k++ {
		seed := int64(200 + k)
		s, g, w := runAllFlagsEpisode(t, tk, factory, seed, DefaultMaxTicks, true, true)
		if s {
			onSolved++
		}
		if g {
			onGround++
		}
		if w.scope {
			anyScope++
		}
		if w.entry {
			anyEntry++
		}
		if w.produced {
			anyProduced++
		}
		if w.recall {
			anyRecall++
		}
		t.Logf("ON  replay %d (seed %d): solved=%v grounded=%v | wire scope=%v entry=%v produced=%v reframed-recall=%v",
			k, seed, s, g, w.scope, w.entry, w.produced, w.recall)
	}

	t.Logf("=== ALL-FLAGS-ON DIRECTIONAL A/B (live claude, K=%d, task=%s) ===", K, tk.ID)
	t.Logf("    OFF: solved %d/%d  grounded %d/%d", offSolved, K, offGround, K)
	t.Logf("    ON : solved %d/%d  grounded %d/%d", onSolved, K, onGround, K)
	t.Logf("    WIRE (ON path): scope %d/%d  entry %d/%d  capability-produced %d/%d  reframed-recall %d/%d",
		anyScope, K, anyEntry, K, anyProduced, K, anyRecall, K)

	// (a) THE WIRE MUST FIRE on the real substrate — else the A/B did not test the ON path.
	if anyScope == 0 {
		t.Errorf("capability ON never sourced a Scope ceiling (subconscious.scope 0/%d) — capability wire dead", K)
	}
	if anyEntry == 0 {
		t.Errorf("capability_dispatch ON never owned the dispatch entry (subconscious.entry 0/%d) — dispatch wire dead", K)
	}
	if anyProduced == 0 {
		t.Errorf("the Capability never produced the workflow (subconscious.synthesize 0/%d) — produce wire dead", K)
	}
	// recall is best-effort: it requires both the seeded reframed skill AND that the goal-derived match
	// clears within the episode tier. Log it; do not hard-fail (a miss falls through to synthesis by design).
	if anyRecall == 0 {
		t.Logf("NOTE: reframed-recall wire did not fire (0/%d) — the seeded skill did not match within tier this run; "+
			"the recall path falls through to synthesis by design (not a failure), but the wire was not exercised live", K)
	}

	// (b) THE GATE (directional): ON must not collapse below OFF — the gap-2 net-negative failure mode.
	if onSolved < offSolved {
		t.Errorf("ON solved (%d/%d) is BELOW OFF (%d/%d) — the redesign DEGRADES grounding (NET-NEGATIVE, flag-flip BLOCKED)",
			onSolved, K, offSolved, K)
	} else {
		t.Logf("DIRECTIONAL PASS: ON (%d/%d) matches-or-beats OFF (%d/%d) — preserve-or-lift, no net-negative", onSolved, K, offSolved, K)
	}
	if onSolved == 0 && offSolved > 0 {
		t.Errorf("ON COLLAPSED to 0/%d while OFF solved %d/%d — the prior collapse signature recurred", K, offSolved, K)
	}
}
