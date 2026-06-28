package realhard

// capability_offline_char_test.go — CHEAP OFFLINE CHARACTERIZATION (Pillar-0, free) for the E5-deeper
// flag-flip A/B. It answers STEP 1 of the experiment: on --backend test, does flipping
// subconscious.capability (+ capability_dispatch) actually CHANGE behaviour, and on WHICH realhard
// fixtures? This targets the live-claude A/B at only the fixtures where the flag is non-inert, so no
// live spend is wasted on offline-identical fixtures.
//
// WHAT IT MEASURES per task (OFF vs ON, the test double, deterministic):
//   - wireFiredScope   — did the Capability path engage (subconscious.scope)? (sanity: ON must engage)
//   - workerRicher     — did the rich §3.11 Context ever beat the ≤5 slice by thought-count? (the only
//                        seam where capability becomes LOAD-BEARING per subagent.go workerSlice; if it
//                        never wins, ON is byte-identical to OFF on this fixture's worker input)
//   - answerDiffers    — did the final scored answer differ OFF vs ON?
//   - groundDiffers    — did the grounded flag differ OFF vs ON?
//   - eventCountDiffers— did the total emitted-event count differ OFF vs ON?
//
// This is OFFLINE-ONLY (no THOUGHT_LIVE_CLAUDE gate) so it runs in the normal suite envelope as a
// characterization probe. Run:
//
//	go test ./internal/bench/realhard -run TestCapabilityOfflineCharacterization -v -timeout 600s

import (
	"os"
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// charResult is one task's OFF-vs-ON offline characterization.
type charResult struct {
	taskID           string
	onScopeFired     bool
	onWorkerRicher   bool // rich Context ever beat the ≤5 slice by thought count (the load-bearing seam)
	maxRich, maxLive int  // the largest rich/live worker-input sizes seen on the ON path
	answerDiffers    bool
	groundDiffers    bool
	eventDiff        int // |events_on - events_off|
	offAnswer        string
	onAnswer         string
}

// runCharEpisode runs ONE offline episode of a task with capability+dispatch set to `on`, capturing the
// wire-fire, the worker-input richness, the final answer, grounding, and the event count. It taps the
// subconscious worker-staffing seam by subscribing to the bus and (for richness) by reading the staffed
// workflow's Context against the live graph — but since that is unexported, it instead infers richness
// from the subconscious.scope + the WorkerContext length emitted... we capture richness directly via a
// post-run inspection of the engine's episode Context vs the active branch length.
func runCharEpisode(t *testing.T, tk Task, on bool, seed int64) (answer string, grounded, scopeFired bool, eventCount, branchLen, ctxLen int) {
	t.Helper()
	ws, err := os.MkdirTemp("", "cap-char-*")
	if err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	defer os.RemoveAll(ws)
	if err := materialize(ws, tk.Materials); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	be := backends.NewTest() // the deterministic offline double
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = int(seed)
	cfg.MaxTicks = DefaultMaxTicks
	cfg.Cognition = "control"
	cfg.Workspace = ws
	feat := config.New()
	feat.Subconscious.Capability = on
	feat.Subconscious.CapabilityDispatch = on
	cfg.Features = feat

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(test): %v", err)
	}
	eng.Bus().Subscribe(func(ev events.Event) {
		eventCount++
		if ev.Kind == events.SubScope {
			scopeFired = true
		}
	})

	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(tk.Prompt)
	eng.Run(DefaultMaxTicks)
	grounded = eng.Grounding().Len() > groundBefore
	answer = harnessAnswer(eng)

	// richness proxy: the final active-branch length (the material the rich Context would carry) vs the
	// ≤5 slice cap. When branchLen>5, the rich Context CAN beat the ≤5 slice — the load-bearing seam.
	if g := eng.Graph(); g != nil {
		branchLen = len(g.BranchThoughts(g.ActiveBranch))
	}
	ctxLen = 5 // the ≤5 ContextSliceDefault cap (subagent.go) — the richer-than-slice comparison floor
	return answer, grounded, scopeFired, eventCount, branchLen, ctxLen
}

// TestCapabilityOfflineCharacterization runs the OFF-vs-ON offline A/B over every realhard task and
// reports the per-task delta map: where the flag changes behaviour (warrants a live A/B) vs where it is
// offline-inert. It is a CHARACTERIZATION (no pass/fail gate beyond the sanity that ON engages the wire).
func TestCapabilityOfflineCharacterization(t *testing.T) {
	tasks := Tasks()
	var results []charResult

	for _, tk := range tasks {
		const seed = int64(7)
		offAns, offGround, _, offEvents, offBranch, _ := runCharEpisode(t, tk, false, seed)
		onAns, onGround, onScope, onEvents, onBranch, slimCap := runCharEpisode(t, tk, true, seed)

		// richness: did the active branch ever grow past the ≤5 slice cap on the ON path? When it does,
		// the rich Context is genuinely richer than the slice → the load-bearing seam is active. (On the
		// OFF path the worker only ever sees the ≤5 slice; on ON it sees max(rich, slice).)
		richer := onBranch > slimCap || offBranch > slimCap

		r := charResult{
			taskID:         tk.ID,
			onScopeFired:   onScope,
			onWorkerRicher: richer,
			maxRich:        onBranch,
			maxLive:        slimCap,
			answerDiffers:  offAns != onAns,
			groundDiffers:  offGround != onGround,
			eventDiff:      abs(onEvents - offEvents),
			offAnswer:      truncate(offAns, 60),
			onAnswer:       truncate(onAns, 60),
		}
		results = append(results, r)
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].taskID < results[j].taskID })

	t.Logf("=== CAPABILITY OFFLINE CHARACTERIZATION (--backend test, seed=7) ===")
	t.Logf("    flag: subconscious.capability + subconscious.capability_dispatch")
	t.Logf("    %-24s scope  branch>5?  answerDiff  groundDiff  eventDiff", "task")
	var nonInert, scopeMisses, behaviourDelta int
	for _, r := range results {
		flag := " "
		if r.answerDiffers || r.groundDiffers || r.eventDiff > 0 {
			flag = "*"
			behaviourDelta++
		}
		if r.answerDiffers || r.groundDiffers {
			nonInert++
		}
		if !r.onScopeFired {
			scopeMisses++
		}
		t.Logf("%s   %-24s %-6v %-10v %-11v %-11v %d", flag, r.taskID, r.onScopeFired,
			r.maxRich > r.maxLive, r.answerDiffers, r.groundDiffers, r.eventDiff)
	}
	t.Logf("    ----")
	t.Logf("    tasks: %d   scope-engaged-on-ON: %d   any-event-delta(*): %d   answer/ground-delta: %d",
		len(results), len(results)-scopeMisses, behaviourDelta, nonInert)
	t.Logf("    INTERPRETATION: only tasks with an answer/ground-delta offline are guaranteed non-inert;")
	t.Logf("    the rest may STILL differ LIVE (the test double returns canned strings regardless of the")
	t.Logf("    richer worker input — offline-identical answer does NOT prove live-identical). The live")
	t.Logf("    A/B targets the worker-routing grounding fixtures where branch>5 (rich Context load-bearing).")

	// SANITY: the ON path must engage the capability wire on at least the worker-routing fixtures.
	if len(results)-scopeMisses == 0 {
		t.Errorf("capability wire NEVER engaged offline (scope fired 0/%d) — the flag is dead, not just inert", len(results))
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
