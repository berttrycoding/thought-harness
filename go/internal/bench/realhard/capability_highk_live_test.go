package realhard

// capability_highk_live_test.go — the HIGH-K PAIRED LIVE-CLAUDE A/B that GATES the E5-deeper flag-flip.
//
// THE QUESTION (the user's product gate): does making the Capability the LIVE dispatch entry
// (subconscious.capability + subconscious.capability_dispatch ON — the rich-Context staffing replacing the
// ≤5 worker slice + the Capability-owned recognition entry) deliver grounding solve-rate AS WELL AS / BETTER
// THAN the legacy Workflow.Recognize + ≤5-slice path, on the real claude substrate?
//
// WHY HIGH-K (the measured noise reality, memory project-realhard-per-task-p-map-measured): a claude episode
// outcome is a coin with a FIXED true p; the run-to-run swing is per-call-INDEPENDENT Bernoulli (±56pp at
// K=1), NOT a launch shock. So a single run (or K=3-4, the prior directional tests) is untrustworthy. The
// noise is BEATEN by HIGH replays in ONE launch: Var(rate)=p(1−p) is a known function of the mean, so p̂ is
// pinned cheaply at high K and the aggregate ON−OFF effect gets a valid Newcombe/Wilson CI (bernoulli.go).
// The GATE believes a delta ONLY if its aggregate CI clears 0 (BernFeasible); a CI straddling 0 is
// NOISE-DOMINATED-INCONCLUSIVE (BernNoisy) — a valid, honest result that leaves the flip deferred.
//
// THE SUITE (NON-saturated only — saturated p≈0/1 carry ~0 Fisher info, zero discrimination, wasted spend):
// the worker-routing GROUNDING fixtures where reading the WHOLE grown branch vs the ≤5 tail is load-bearing
// — the multi-hop chains (mhop-0001/0002/0003) + the supersession backtrack (back-0001/0002). These are the
// fixtures the rich-Context staffing is DESIGNED to help (gap-2). The offline characterization
// (TestCapabilityOfflineCharacterization) confirmed the flag is offline-INERT on every fixture (the double
// ignores the richer input) — so the delta, if any, lives entirely in how the live model uses the richer
// context, and ONLY a live A/B can measure it.
//
// COST CAP (real claude spend): a hard global call budget aborts the run before it overruns. Default K and
// the budget are env-tunable so the run can be sized to the available budget:
//
//	THOUGHT_AB_K           — replays per arm per task (default 15; the noise-clearing floor)
//	THOUGHT_AB_TASKS       — comma task-ID substrings (default the 5 non-saturated worker-routing fixtures)
//	THOUGHT_AB_MAXTICKS    — per-episode tick budget (default 40; bounds per-episode call cost)
//	THOUGHT_AB_CALL_BUDGET — hard global llm-call cap; abort before overrun (default 6000)
//
// Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes). Run:
//
//	THOUGHT_LIVE_CLAUDE=1 THOUGHT_AB_K=15 go test ./internal/bench/realhard \
//	    -run TestLiveClaudeCapabilityHighKAB -v -timeout 9000s

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// absPath resolves a (possibly relative) ledger path to an absolute one so the test's package-dir CWD
// never silently redirects the ledger (the checkpoint-path bug that lost a run's resumability).
func absPath(p string) (string, error) { return filepath.Abs(p) }

// abCheckpoint is one task's completed-arms result, persisted to a JSONL ledger so the long live A/B is
// KILL-RESILIENT and RESUMABLE: a task already in the ledger is skipped on a re-run (the prior result is
// reused), so a kill mid-run loses at most the in-flight task. Invalidate-not-delete: rows are appended.
type abCheckpoint struct {
	TaskID    string `json:"task_id"`
	Cap       string `json:"capability"`
	K         int    `json:"k"`
	OffSolved int    `json:"off_solved"`
	OnSolved  int    `json:"on_solved"`
	OnScope   int    `json:"on_scope"`
	Calls     int    `json:"calls"`
}

// loadCheckpoints reads the per-task ledger (THOUGHT_AB_LEDGER), POOLING all rows for a task (sum the
// solved/K/scope/calls across rows). Pooling — not last-wins — is what lets MANY SHORT bursts ACCUMULATE
// replays for the same task: each burst appends K more replays of the same iid coin, and the pooled p̂
// tightens. (Invalidate-not-delete: every burst's row is preserved on disk; the load sums them.) A missing
// file ⇒ empty map.
func loadCheckpoints(path string) map[string]abCheckpoint {
	out := map[string]abCheckpoint{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cp abCheckpoint
		if json.Unmarshal([]byte(line), &cp) == nil && cp.TaskID != "" && cp.K > 0 {
			acc := out[cp.TaskID]
			acc.TaskID = cp.TaskID
			acc.Cap = cp.Cap
			acc.K += cp.K
			acc.OffSolved += cp.OffSolved
			acc.OnSolved += cp.OnSolved
			acc.OnScope += cp.OnScope
			acc.Calls += cp.Calls
			out[cp.TaskID] = acc // POOL: sum this row into the task's running total
		}
	}
	return out
}

// appendCheckpoint appends one task's result to the ledger. It MkdirAll's the parent so a missing dir
// (the silent-failure bug: O_CREATE cannot make a file in a non-existent dir) never drops a checkpoint —
// the whole point of the ledger is kill-resilience. A write error is logged via the returned error.
func appendCheckpoint(path string, cp abCheckpoint) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// abDefaultTasks is the non-saturated worker-routing-grounding A/B suite (the fixtures where the rich-Context
// staffing is load-bearing AND p is in the informative band per the measured per-task p-map).
var abDefaultTasks = []string{
	"mhop-0001", "mhop-0002", "mhop-0003", "back-0001", "back-0002",
}

func abEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// callBudget is a thread-safe global llm-call counter + hard cap. abort() reports whether the cap is hit.
type callBudget struct {
	used atomic.Int64
	cap  int64
}

func (b *callBudget) add(n int)    { b.used.Add(int64(n)) }
func (b *callBudget) abort() bool  { return b.used.Load() >= b.cap }
func (b *callBudget) spent() int64 { return b.used.Load() }

// runCapEpisode runs ONE grounding episode of a task with capability+dispatch set to `on`, in a fresh
// materialized workspace, counting the live llm.* calls into the shared budget. Returns (solved, grounded,
// scopeFired, calls). Validation-only — no engine code is modified.
func runCapEpisode(t *testing.T, tk Task, factory BackendFactory, seed int64, maxTicks int, on bool,
	budget *callBudget) (solved, grounded, scopeFired bool, calls int) {
	t.Helper()
	ws, err := os.MkdirTemp("", "cap-highk-*")
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
	feat := config.New() // AllOn (the two capability flags default OFF — the opt-in exception)
	feat.Subconscious.Capability = on
	feat.Subconscious.CapabilityDispatch = on
	cfg.Features = feat

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	eng.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.LLM:
			calls++
			budget.add(1)
		case events.SubScope:
			scopeFired = true
		}
	})

	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(tk.Prompt)
	eng.Run(maxTicks)
	grounded = eng.Grounding().Len() > groundBefore
	solved = Score(tk, harnessAnswer(eng)).Solved
	return solved, grounded, scopeFired, calls
}

// TestLiveClaudeCapabilityHighKAB is the DECISIVE high-K paired A/B that gates the flag-flip. Per task, it
// runs K paired OFF then K ON episodes (same per-replay seeds across arms ⇒ paired-by-task), feeds the
// per-task (solved, K) counts to the Bernoulli estimator for per-task Wilson CIs + the aggregate ON−OFF
// Newcombe-CI paired diff, and prints a clear VERDICT off the estimator's resolution.
func TestLiveClaudeCapabilityHighKAB(t *testing.T) {
	factory := liveClaudeFactory(t) // also skips unless THOUGHT_LIVE_CLAUDE=1

	K := abEnvInt("THOUGHT_AB_K", 15)
	maxTicks := abEnvInt("THOUGHT_AB_MAXTICKS", 40)
	budget := &callBudget{cap: int64(abEnvInt("THOUGHT_AB_CALL_BUDGET", 6000))}

	taskSel := abDefaultTasks
	if v := os.Getenv("THOUGHT_AB_TASKS"); v != "" {
		taskSel = strings.Split(v, ",")
	}
	var tasks []Task
	for _, sub := range taskSel {
		found := FilterTasks(Tasks(), strings.TrimSpace(sub))
		if len(found) == 0 {
			t.Fatalf("A/B task substring %q matched no realhard task", sub)
		}
		tasks = append(tasks, found...)
	}

	t.Logf("=== CAPABILITY HIGH-K PAIRED A/B (live claude) ===")
	t.Logf("    flags: subconscious.capability + subconscious.capability_dispatch (OFF=legacy, ON=Capability-entry)")
	t.Logf("    K=%d per arm  maxTicks=%d  call-budget=%d  tasks=%d", K, maxTicks, budget.cap, len(tasks))

	ledger := os.Getenv("THOUGHT_AB_LEDGER") // per-task JSONL checkpoint (resumable / kill-resilient)
	if ledger != "" {
		if abs, err := absPath(ledger); err == nil {
			ledger = abs // resolve relative to an absolute path so the test's package-dir CWD never surprises
		}
		t.Logf("    ledger (resumable, kill-resilient): %s", ledger)
	}
	done := loadCheckpoints(ledger)
	if len(done) > 0 {
		t.Logf("    RESUME: %d task(s) already in the ledger — reusing, not re-measuring", len(done))
	}

	// TARGET K is the per-task replay GOAL pooled across ALL bursts; K (THOUGHT_AB_K) is THIS burst's CAP.
	// A burst measures up to K MORE replays, topping the task up toward targetK; many short bursts pool to
	// targetK (kill-resilient: each replay is checkpointed the instant it completes).
	targetK := abEnvInt("THOUGHT_AB_TARGET_K", K)

	var offIns, onIns []BernTaskInput
	var aborted bool
	for _, tk := range tasks {
		cp := done[tk.ID] // pooled prior replays (zero value when none)
		priorK := cp.K
		offSolved, onSolved, onScope, taskCalls := cp.OffSolved, cp.OnSolved, cp.OnScope, cp.Calls
		need := targetK - priorK // replays still needed to reach the target
		if need < 0 {
			need = 0
		}
		if need > K {
			need = K // this burst's cap
		}
		burstPairs := 0
		// INTERLEAVE OFF/ON per replay (paired by seed) so a kill leaves BALANCED arms: a pair counts only
		// when BOTH episodes complete. Each completed pair is CHECKPOINTED IMMEDIATELY (per-replay row) so a
		// mid-task kill keeps every finished pair — the ledger pools them on the next burst.
		for j := 0; j < need; j++ {
			if budget.abort() {
				aborted = true
				break
			}
			seed := int64(1000 + priorK + j) // distinct seed per pooled replay (no overlap across bursts)
			so, _, _, co := runCapEpisode(t, tk, factory, seed, maxTicks, false, budget)
			if budget.abort() {
				aborted = true
				break
			}
			sn, _, sc, cn := runCapEpisode(t, tk, factory, seed, maxTicks, true, budget)
			burstPairs++
			rowOff, rowOn, rowScope := 0, 0, 0
			if so {
				offSolved++
				rowOff = 1
			}
			if sn {
				onSolved++
				rowOn = 1
			}
			if sc {
				onScope++
				rowScope = 1
			}
			taskCalls += co + cn
			// per-replay checkpoint (K=1 row) — the finest-grained kill-resilience: pooled on the next load.
			if err := appendCheckpoint(ledger, abCheckpoint{
				TaskID: tk.ID, Cap: string(tk.Capability), K: 1,
				OffSolved: rowOff, OnSolved: rowOn, OnScope: rowScope, Calls: co + cn,
			}); err != nil {
				t.Logf("    WARNING: per-replay checkpoint to %q FAILED (%v) — NOT kill-resilient", ledger, err)
			}
		}
		totalK := priorK + burstPairs
		offIns = append(offIns, BernTaskInput{TaskID: tk.ID, Capability: tk.Capability, Solved: offSolved, K: totalK})
		onIns = append(onIns, BernTaskInput{TaskID: tk.ID, Capability: tk.Capability, Solved: onSolved, K: totalK})
		reuse := ""
		if priorK > 0 {
			reuse = " (" + strconv.Itoa(priorK) + " pooled from prior bursts + " + strconv.Itoa(burstPairs) + " this burst)"
		}
		t.Logf("    %-22s OFF %d/%d  ON %d/%d  (scope-engaged %d/%d)  calls(this burst)=%d  budget-spent=%d%s",
			tk.ID, offSolved, totalK, onSolved, totalK, onScope, totalK, taskCalls-cp.Calls, budget.spent(), reuse)
		if onScope == 0 && totalK > 0 {
			t.Errorf("capability ON never engaged on %s (scope 0/%d) — the A/B did not test the ON path", tk.ID, totalK)
		}
		if aborted {
			t.Logf("    *** CALL BUDGET (%d) REACHED — stopping the A/B early at task %s (partial result below)", budget.cap, tk.ID)
			break
		}
	}

	// Aggregate via the Bernoulli paired-by-task estimator (Wilson per-task CIs + Newcombe aggregate diff).
	rep := EstimateBernoulli(offIns, onIns, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	t.Logf("\n%s", rep.Render())

	// THE VERDICT — off the estimator's resolution of the aggregate ON−OFF effect.
	t.Logf("=== VERDICT (E5-deeper flag-flip gate) ===")
	t.Logf("    total live llm-calls spent: %d  (cap %d, aborted-early=%v)", budget.spent(), budget.cap, aborted)
	switch {
	case rep.Verdict == BernOverdispersed:
		t.Logf("    OVERDISPERSED — the iid-Bernoulli precondition failed; aggregate CI not trustworthy. INCONCLUSIVE.")
	case rep.TaskEff == 0:
		t.Logf("    DEGENERATE — every A/B task saturated (p̂∈{0,1} on both arms); zero discrimination. Re-pick the suite.")
	case rep.MeanDiffCILo > 0:
		t.Logf("    ON CLEARS THE BAR: aggregate ON−OFF = %+.3f, 95%% CI [%+.3f,%+.3f] — LOWER BOUND > 0.",
			rep.MeanDiff, rep.MeanDiffCILo, rep.MeanDiffCIHi)
		t.Logf("    The Capability-as-dispatch-entry DELIVERS BETTER than the legacy path. WARRANTS THE FLIP.")
	case rep.MeanDiffCIHi < 0:
		t.Logf("    ON REGRESSES: aggregate ON−OFF = %+.3f, 95%% CI [%+.3f,%+.3f] — UPPER BOUND < 0.",
			rep.MeanDiff, rep.MeanDiffCILo, rep.MeanDiffCIHi)
		t.Logf("    The Capability-as-dispatch-entry DEGRADES grounding. FLIP STAYS DEFERRED (net-negative).")
	default:
		t.Logf("    NOISE-DOMINATED-INCONCLUSIVE: aggregate ON−OFF = %+.3f, 95%% CI [%+.3f,%+.3f] — CI STRADDLES 0.",
			rep.MeanDiff, rep.MeanDiffCILo, rep.MeanDiffCIHi)
		t.Logf("    The delta does not clear the noise band at K=%d. FLIP STAYS DEFERRED (no resolvable signal).", K)
		if len(rep.Allocation) > 0 {
			t.Logf("    adaptive-K recommends total %d replays (vs uniform %d) to resolve a %.2f effect.",
				rep.AdaptiveTotalK, rep.UniformTotalK, rep.Delta)
		}
	}

	// A wash (CI straddles 0) is a VALID result — never a hard fail. Only a true regression (CI upper < 0)
	// or a dead wire is a test failure; the rest is reported as signal for the user's product call.
	if rep.MeanDiffCIHi < 0 {
		t.Errorf("REGRESSION: ON degrades grounding (aggregate CI upper bound %+.3f < 0) — flip blocked", rep.MeanDiffCIHi)
	}
}
