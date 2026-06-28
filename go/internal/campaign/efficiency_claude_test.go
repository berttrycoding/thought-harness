//go:build claudebench

package campaign

// efficiency_claude_test.go — W5-2b STEP 1: the CLAUDE-backed warm-vs-cold completion-token re-measure.
//
// SCOPE. The offline efficiency_test.go proves the RECALL PATH (warm arm seeds a skill → synth step-0
// short-circuits SynthesizeProgram); it cannot observe the token magnitude (the test double emits no real
// usage → Completion=0). This file closes the only open W5-2b question: ON --backend claude, does the WARM
// arm spend FEWER completion (decode) tokens than the COLD arm? CompletionDelta() < 0 (warm cheaper) at HELD
// grounded-success is the first positive W5 efficiency data point; >= 0 is the real "synthesis-skip is
// offset by the recall seam cost" finding (honest either way).
//
// WHY IT IS OFF THE DEFAULT SUITE. Two gates: the `claudebench` BUILD TAG (so `go build ./... && go test
// ./...` never compiles it) AND a runtime env gate (THOUGHT_CAMPAIGN_BACKEND=claude) so even a tagged build
// skips unless explicitly armed. It spawns real `claude -p` per CONTENT call (metered, ~minutes) — it must
// never run in CI / the offline suite.
//
// RUN:
//   THOUGHT_CAMPAIGN_BACKEND=claude go test -tags claudebench -run TestWarmVsColdCompletionDelta_Claude \
//     -timeout 60m -v ./internal/campaign/
//
// Substrate: claude (tiered sonnet primary + haiku utility). Temperature is NOT controllable on this
// substrate — never compare these rows against local rows as if it were. Rows land substrate-tagged in a
// SEPARATE state dir (data/registry-claude/w5-2b-efficiency) per the substrate-hygiene rule.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// claudeReplaysDefault is K — the per-arm replay count (>=5 per the brief). Each replay is one full episode
// per task per arm, so the call budget is roughly 2 arms x len(EfficiencyBank) x K episodes worth of CONTENT
// calls — and on sonnet each episode is several seconds. MEASURED: K=5 on the full 3-task EfficiencyBank
// OVERRAN the <35 min budget (the cold arm alone — 15 synthesis episodes — took ~34 min), so a bounded
// re-run scopes this down via THOUGHT_W52B_K (replays) + THOUGHT_W52B_TASKS (first-N tasks) without
// touching the committed default. A re-run of K=2 on 1 task fits the budget and still yields the paired
// cold-vs-warm completion-token data point (the magnitude is the deliverable, not the sample size).
const claudeReplaysDefault = 5

// claudeReplays reads K from THOUGHT_W52B_K (else the default), so a bounded re-run fits the wall-clock
// budget. envInt clamps to >=1.
func claudeReplays() int { return envInt("THOUGHT_W52B_K", claudeReplaysDefault) }

// claudeTasks returns the first-N EfficiencyBank tasks (THOUGHT_W52B_TASKS; 0/unset = all) — the budget
// lever that drops the heavy cold arm from 3 tasks to 1 for a re-run that completes.
func claudeTasks() []HeldOutTask {
	all := EfficiencyBank()
	n := envInt("THOUGHT_W52B_TASKS", 0)
	if n <= 0 || n > len(all) {
		return all
	}
	return all[:n]
}

// envInt parses a positive int from env, falling back to def (and clamping non-positive to def).
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// efficiencyStateRoot is the SEPARATE, substrate-tagged state dir (substrate hygiene): claude-derived
// efficiency rows never mix with local-minted registry state. Overridable via env for a custom landing dir.
func efficiencyStateRoot() string {
	if d := os.Getenv("THOUGHT_W52B_STATE"); d != "" {
		return d
	}
	return filepath.FromSlash("data/registry-claude/w5-2b-efficiency")
}

// claudeEngineFactory builds a FRESH claude-backed engine seeded from stateDir — the SAME factory shape
// cmd/thought/campaign.go uses (reactive, Seed 7, JSONL store), but on the claude bridge so each WarmVsCold
// arm runs against the real frontier substrate. "" = the cold baseline (no skill); the warm dir is the
// SeedRecurringSkill-written state the warm arm reloads the recurring skill from.
func claudeEngineFactory(model string) func(stateDir string) (*engine.Engine, error) {
	return func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		if stateDir != "" {
			st, err := persist.NewJSONLStore(stateDir)
			if err != nil {
				return nil, err
			}
			cfg.Store = st
		}
		be, err := llm.MakeBackend("claude", "", model, 0)
		if err != nil {
			return nil, err
		}
		return engine.NewEngine(&cfg, be)
	}
}

// claudeGroundedEngineFactory is the W5-2c factory: claudeEngineFactory PLUS a wired cfg.Workspace (the real
// executor), so the grounded efficiency bank actually IMPORTS reality on the claude bridge. The real model is
// the RealityComprehender (it names the read/search target itself), so the grounding works without the
// scripted offline double — and the cost re-measure lands at HELD-POSITIVE grounded-success. The SAME fixture
// (GroundedEfficiencyWorkspace) the offline test grounds against, so both arms read identical reality.
func claudeGroundedEngineFactory(model, workspace string) func(stateDir string) (*engine.Engine, error) {
	return func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Workspace = workspace // the REAL executor — the agentic/reality-access axis (grounds the bank)
		if stateDir != "" {
			st, err := persist.NewJSONLStore(stateDir)
			if err != nil {
				return nil, err
			}
			cfg.Store = st
		}
		be, err := llm.MakeBackend("claude", "", model, 0)
		if err != nil {
			return nil, err
		}
		return engine.NewEngine(&cfg, be)
	}
}

// efficiencyRow is the substrate-tagged, persisted record of one task's warm-vs-cold outcome — written to
// the separate claude state dir so the magnitude is a durable, auditable artifact (not just test stdout).
type efficiencyRow struct {
	Substrate       string  `json:"substrate"`
	Model           string  `json:"model"`
	Goal            string  `json:"goal"`
	MintGoal        string  `json:"mint_goal"`
	Replays         int     `json:"replays"`
	ColdCompletion  int     `json:"cold_completion_total"`
	WarmCompletion  int     `json:"warm_completion_total"`
	ColdMean        float64 `json:"cold_completion_mean"`
	WarmMean        float64 `json:"warm_completion_mean"`
	CompletionDelta float64 `json:"completion_delta"` // warm-minus-cold mean; <0 = warm cheaper (the win)
	ColdVec         []int   `json:"cold_completion_vec"`
	WarmVec         []int   `json:"warm_completion_vec"`
	ColdGrounded    int     `json:"cold_grounded"`
	WarmGrounded    int     `json:"warm_grounded"`
	ColdSolved      int     `json:"cold_solved"`
	WarmSolved      int     `json:"warm_solved"`
	Timestamp       string  `json:"ts"`
}

// TestWarmVsColdCompletionDelta_Claude is the metered re-measure. It drives WarmVsCold(EfficiencyBank,
// EfficiencyMintGoal, coldDir, warmDir, K) on claude, reports the per-task COLD vs WARM completion-token
// vectors + means + CompletionDelta(), persists a substrate-tagged row per task to the separate state dir,
// and prints the aggregate cost-axis table. It does NOT assert a win/loss (the magnitude is the finding —
// the verdict is read from the printed table); it only fails on a HARNESS error (no skill minted, factory
// build error), so a NO-SIGNAL/NEGATIVE result is a clean pass with the data on stdout + on disk.
func TestWarmVsColdCompletionDelta_Claude(t *testing.T) {
	if os.Getenv("THOUGHT_CAMPAIGN_BACKEND") != "claude" {
		t.Skip("set THOUGHT_CAMPAIGN_BACKEND=claude to arm the metered claude re-measure (off by default)")
	}
	model := os.Getenv("THOUGHT_CLAUDE_MODEL") // "" → bridge default (sonnet primary + haiku utility)
	substrate := "claude:sonnet+haiku"
	if model != "" {
		substrate = "claude:" + model
	}
	t.Logf("SUBSTRATE: %s (tiered; temperature NOT controllable on this substrate)", substrate)

	root := efficiencyStateRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir state root %s: %v", root, err)
	}
	coldDir := filepath.Join(root, "cold") // empty baseline state (no seeded skill)
	warmDir := filepath.Join(root, "warm") // SeedRecurringSkill writes the recurring skill here
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		t.Fatalf("mkdir cold dir: %v", err)
	}

	b := EngineBencher{
		MaxTicks:  20,
		NewEngine: claudeEngineFactory(model),
		// serial (Concurrency 0) — bounded, ordered, and gentle on the claude rate limit.
	}
	k := claudeReplays()
	tasks := claudeTasks()

	t.Logf("W5-2b claude re-measure: %d tasks x %d replays x 2 arms; mint goal %q",
		len(tasks), k, EfficiencyMintGoal)
	start := time.Now()
	rows, err := b.WarmVsCold(tasks, EfficiencyMintGoal, coldDir, warmDir, k)
	if err != nil {
		t.Fatalf("WarmVsCold on claude: %v", err)
	}
	elapsed := time.Since(start)

	// --- per-task table + persisted rows ---
	var sumColdMean, sumWarmMean float64
	var sumColdGr, sumWarmGr, sumColdSolved, sumWarmSolved, n int
	t.Logf("=== W5-2b WARM-vs-COLD completion-token re-measure (substrate=%s, K=%d) ===", substrate, k)
	t.Logf("%-46s | %10s | %10s | %9s | %s", "goal", "COLD(mean)", "WARM(mean)", "Δ(w-c)", "grounded c/w")
	for _, r := range rows {
		cm, wm := r.Cold.MeanCompletion(), r.Warm.MeanCompletion()
		t.Logf("%-46s | %10.1f | %10.1f | %+9.1f | %d/%d",
			truncGoal(r.Goal, 46), cm, wm, r.CompletionDelta(),
			r.Cold.Grounded, r.Warm.Grounded)
		t.Logf("    cold vec=%v  warm vec=%v  (cold total=%d warm total=%d)",
			r.Cold.Completions, r.Warm.Completions, r.Cold.Completion, r.Warm.Completion)

		row := efficiencyRow{
			Substrate: substrate, Model: model, Goal: r.Goal, MintGoal: EfficiencyMintGoal,
			Replays:        k,
			ColdCompletion: r.Cold.Completion, WarmCompletion: r.Warm.Completion,
			ColdMean: cm, WarmMean: wm, CompletionDelta: r.CompletionDelta(),
			ColdVec: r.Cold.Completions, WarmVec: r.Warm.Completions,
			ColdGrounded: r.Cold.Grounded, WarmGrounded: r.Warm.Grounded,
			ColdSolved: r.Cold.Solved, WarmSolved: r.Warm.Solved,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := appendEfficiencyRow(root, row); err != nil {
			t.Errorf("persist row for %q: %v", r.Goal, err)
		}

		sumColdMean += cm
		sumWarmMean += wm
		sumColdGr += r.Cold.Grounded
		sumWarmGr += r.Warm.Grounded
		sumColdSolved += r.Cold.Solved
		sumWarmSolved += r.Warm.Solved
		n++
	}

	if n == 0 {
		t.Fatalf("no rows produced")
	}
	avgCold, avgWarm := sumColdMean/float64(n), sumWarmMean/float64(n)
	aggDelta := avgWarm - avgCold
	t.Logf("--- AGGREGATE over %d tasks (K=%d) ---", n, k)
	t.Logf("mean COLD completion = %.1f tok/replay", avgCold)
	t.Logf("mean WARM completion = %.1f tok/replay", avgWarm)
	t.Logf("aggregate CompletionDelta (warm-cold) = %+.1f tok/replay  [%s]", aggDelta, winLabel(aggDelta))
	t.Logf("grounded-success HELD?  cold=%d/%d  warm=%d/%d (of %d task-replays each)",
		sumColdGr, n*k, sumWarmGr, n*k, n*k)
	t.Logf("solved (oracle/grounded): cold=%d warm=%d", sumColdSolved, sumWarmSolved)
	t.Logf("elapsed: %s; rows persisted under %s", elapsed.Round(time.Second), root)
}

// groundedEfficiencyStateRoot is the SEPARATE, substrate-tagged W5-2c landing dir (substrate hygiene):
// grounded claude rows never mix with local-minted state OR the W5-2b empty-oracle rows. Overridable via env.
func groundedEfficiencyStateRoot() string {
	if d := os.Getenv("THOUGHT_W52C_STATE"); d != "" {
		return d
	}
	return filepath.FromSlash("data/registry-claude/w5-2c-grounded-efficiency")
}

// groundedClaudeTasks returns the first-N GroundedEfficiencyBank tasks (THOUGHT_W52C_TASKS; 0/unset = all).
func groundedClaudeTasks() []HeldOutTask {
	all := GroundedEfficiencyBank()
	n := envInt("THOUGHT_W52C_TASKS", 0)
	if n <= 0 || n > len(all) {
		return all
	}
	return all[:n]
}

// groundedClaudeReplays reads K from THOUGHT_W52C_K (else >=5 per the W5 brief).
func groundedClaudeReplays() int { return envInt("THOUGHT_W52C_K", 5) }

// TestWarmVsColdGroundedCompletionDelta_Claude is the W5-2c metered re-measure: the W5-2b cost comparison run
// on the GROUNDED bank, so the warm-minus-cold completion delta is read AT HELD-POSITIVE grounded-success
// (not the zero-grounding caveat). It wires cfg.Workspace to the GroundedEfficiencyWorkspace fixture (the
// real executor + the real model as RealityComprehender), drives WarmVsCold(GroundedEfficiencyBank,
// GroundedEfficiencyMintGoal, ...) on claude, persists substrate-tagged rows to the separate W5-2c state dir,
// and prints the per-task + aggregate cost table together with the grounded/solved counts that establish the
// held-positive utility. It does NOT hard-fail on a NEGATIVE/NO-SIGNAL cost result (the magnitude is the
// finding) — but it DOES fail loudly if the bank did not ground (grounded==0 on both arms), because then the
// "held-positive utility" claim is unmet and the cost number is meaningless (the exact W5-2c failure mode).
func TestWarmVsColdGroundedCompletionDelta_Claude(t *testing.T) {
	if os.Getenv("THOUGHT_CAMPAIGN_BACKEND") != "claude" {
		t.Skip("set THOUGHT_CAMPAIGN_BACKEND=claude to arm the metered W5-2c grounded re-measure (off by default)")
	}
	model := os.Getenv("THOUGHT_CLAUDE_MODEL")
	substrate := "claude:sonnet+haiku"
	if model != "" {
		substrate = "claude:" + model
	}
	t.Logf("SUBSTRATE: %s (tiered; temperature NOT controllable on this substrate)", substrate)

	root := groundedEfficiencyStateRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir state root %s: %v", root, err)
	}
	coldDir := filepath.Join(root, "cold")
	warmDir := filepath.Join(root, "warm")
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		t.Fatalf("mkdir cold dir: %v", err)
	}
	// the workspace fixture both arms ground against (one const per file, the A1 search->read shape).
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}

	b := EngineBencher{
		MaxTicks:  20,
		NewEngine: claudeGroundedEngineFactory(model, ws), // workspace-wired: the bank grounds
	}
	k := groundedClaudeReplays()
	tasks := groundedClaudeTasks()

	t.Logf("W5-2c GROUNDED claude re-measure: %d tasks x %d replays x 2 arms; mint goal %q; workspace %s",
		len(tasks), k, GroundedEfficiencyMintGoal, ws)
	start := time.Now()
	rows, err := b.WarmVsCold(tasks, GroundedEfficiencyMintGoal, coldDir, warmDir, k)
	if err != nil {
		t.Fatalf("WarmVsCold (grounded) on claude: %v", err)
	}
	elapsed := time.Since(start)

	var sumColdMean, sumWarmMean float64
	var sumColdGr, sumWarmGr, sumColdSolved, sumWarmSolved, n int
	t.Logf("=== W5-2c GROUNDED WARM-vs-COLD completion-token re-measure (substrate=%s, K=%d) ===", substrate, k)
	t.Logf("%-46s | %10s | %10s | %9s | %s", "goal", "COLD(mean)", "WARM(mean)", "Δ(w-c)", "grounded c/w")
	for _, r := range rows {
		cm, wm := r.Cold.MeanCompletion(), r.Warm.MeanCompletion()
		t.Logf("%-46s | %10.1f | %10.1f | %+9.1f | %d/%d",
			truncGoal(r.Goal, 46), cm, wm, r.CompletionDelta(), r.Cold.Grounded, r.Warm.Grounded)
		t.Logf("    cold vec=%v  warm vec=%v  (cold total=%d warm total=%d)  solved c/w=%d/%d",
			r.Cold.Completions, r.Warm.Completions, r.Cold.Completion, r.Warm.Completion, r.Cold.Solved, r.Warm.Solved)

		row := efficiencyRow{
			Substrate: substrate, Model: model, Goal: r.Goal, MintGoal: GroundedEfficiencyMintGoal,
			Replays:        k,
			ColdCompletion: r.Cold.Completion, WarmCompletion: r.Warm.Completion,
			ColdMean: cm, WarmMean: wm, CompletionDelta: r.CompletionDelta(),
			ColdVec: r.Cold.Completions, WarmVec: r.Warm.Completions,
			ColdGrounded: r.Cold.Grounded, WarmGrounded: r.Warm.Grounded,
			ColdSolved: r.Cold.Solved, WarmSolved: r.Warm.Solved,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := appendEfficiencyRow(root, row); err != nil {
			t.Errorf("persist row for %q: %v", r.Goal, err)
		}

		sumColdMean += cm
		sumWarmMean += wm
		sumColdGr += r.Cold.Grounded
		sumWarmGr += r.Warm.Grounded
		sumColdSolved += r.Cold.Solved
		sumWarmSolved += r.Warm.Solved
		n++
	}
	if n == 0 {
		t.Fatalf("no rows produced")
	}
	avgCold, avgWarm := sumColdMean/float64(n), sumWarmMean/float64(n)
	aggDelta := avgWarm - avgCold
	t.Logf("--- AGGREGATE over %d tasks (K=%d) ---", n, k)
	t.Logf("mean COLD completion = %.1f tok/replay", avgCold)
	t.Logf("mean WARM completion = %.1f tok/replay", avgWarm)
	t.Logf("aggregate CompletionDelta (warm-cold) = %+.1f tok/replay  [%s]", aggDelta, winLabel(aggDelta))
	t.Logf("grounded-success HELD?  cold=%d/%d  warm=%d/%d (of %d task-replays each)",
		sumColdGr, n*k, sumWarmGr, n*k, n*k)
	t.Logf("solved (oracle/grounded): cold=%d warm=%d", sumColdSolved, sumWarmSolved)
	t.Logf("elapsed: %s; rows persisted under %s", elapsed.Round(time.Second), root)

	// W5-2c HELD-POSITIVE-UTILITY GATE: the cost number is only meaningful if the bank GROUNDED. If neither
	// arm imported reality on ANY replay, the "held-positive utility" claim is unmet — fail loudly (this is
	// the precise failure mode W5-2c exists to expose, vs the W5-2b zero-grounding caveat).
	if sumColdGr == 0 && sumWarmGr == 0 {
		t.Fatalf("W5-2c HELD-POSITIVE-UTILITY UNMET: the grounded bank grounded 0 times on BOTH arms — the cost "+
			"delta %.1f is at zero grounding (the W5-2b caveat), not held-positive utility. Check the workspace "+
			"executor + the model's comprehension (search->read handoff).", aggDelta)
	}
}

// appendEfficiencyRow writes one JSON row to the durable, substrate-tagged ledger in the separate state dir.
func appendEfficiencyRow(root string, row efficiencyRow) error {
	path := filepath.Join(root, "efficiency-rows.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(row)
}

func winLabel(delta float64) string {
	switch {
	case delta < 0:
		return "warm CHEAPER (candidate WIN)"
	case delta > 0:
		return "warm COSTLIER (NEGATIVE — synthesis-skip offset by recall seam cost)"
	default:
		return "EQUAL"
	}
}

func truncGoal(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var _ = fmt.Sprintf // keep fmt imported for ad-hoc debug formatting if needed
