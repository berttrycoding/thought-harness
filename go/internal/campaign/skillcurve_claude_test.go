//go:build claudebench

package campaign

// skillcurve_claude_test.go — A3 STEP 2: the CLAUDE-backed SELF-IMPROVEMENT CURVE pilot.
//
// SCOPE. The offline skillcurve_test.go proves the autonomous mint+recall flywheel fires at the right
// exposures (the curve's SHAPE) but the test double emits no usage (Completion=0) so it cannot read the cost
// MAGNITUDE. This file closes that: ON --backend claude, drive a STREAM of exposures of the recurring grounded
// family through ONE shared persisted state dir and measure, PER EXPOSURE, the completion (decode) tokens +
// the faculty fire + the recall/mint flags — the genuine self-improvement curve. Does the curve BEND DOWN
// (tokens fall once the skill mints and recall short-circuits synthesis) at HELD-POSITIVE grounded utility?
//
// DURABILITY (the claude-measurement-agent death rule). Each exposure's row is written to a DURABLE JSONL
// ledger in the substrate-tagged state dir IMMEDIATELY after that exposure completes — so a death mid-stream
// (the ~60-70min socket-close that kills claude measurement agents) loses at most the in-flight exposure, not
// the whole curve. Run it FOREGROUND to a durable log; never background-pipe. Verify no orphaned workers after.
//
// WHY IT IS OFF THE DEFAULT SUITE. The `claudebench` BUILD TAG (so `go build ./... && go test ./...` never
// compiles it) AND the THOUGHT_CAMPAIGN_BACKEND=claude runtime gate. It spawns real `claude -p` per CONTENT
// call (metered, ~minutes/exposure).
//
// RUN (pilot — small N; do NOT auto-scale):
//   THOUGHT_CAMPAIGN_BACKEND=claude THOUGHT_A3_EXPOSURES=6 \
//     go test -tags claudebench -run TestSkillCurve_Claude -timeout 90m -v ./internal/campaign/ \
//     > runs/a3-curve-pilot.log 2>&1
//
// Substrate: claude (tiered sonnet primary + haiku utility). Temperature NOT controllable. Rows land
// substrate-tagged in data/registry-claude/a3-skillcurve (NEVER mixed with local-minted state).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// a3StateRoot is the SEPARATE, substrate-tagged A3 curve state dir (substrate hygiene): claude-derived curve
// rows never mix with local-minted state OR the W5-2b/2c efficiency rows. Overridable via env.
func a3StateRoot() string {
	if d := os.Getenv("THOUGHT_A3_STATE"); d != "" {
		return d
	}
	return filepath.FromSlash("data/registry-claude/a3-skillcurve")
}

// a3Exposures is N — the number of stream exposures (the curve length). Pilot default 6 (> MintAfter=3, so
// there are pre-mint AND post-recall cohorts). THOUGHT_A3_EXPOSURES overrides for a bounded re-run.
func a3Exposures() int { return envInt("THOUGHT_A3_EXPOSURES", 6) }

// a3CurveFamily is the recurring grounded family the claude curve drives: the grounded-investigator goals
// (each grounds via the workspace + the real model as RealityComprehender), all sharing the goal key
// "investigate source files" (so the minted skill recalls across them), each carrying its symbol's Expect
// oracle (held-positive utility) and the "act" faculty signature (grounding elicits reality-import). The
// stream CYCLES the bank symbols so the surface goals VARY (not the literally-identical string) while the
// goal key stays constant — the honest recurrence regime.
func a3CurveFamily() []CurveTask {
	out := make([]CurveTask, 0, len(groundedSymbols))
	for _, s := range groundedSymbols {
		out = append(out, CurveTask{
			Goal:      "investigate the source files in this codebase and report the exact numeric value assigned to " + s.Symbol,
			Expect:    s.Value,
			Signature: "act",
		})
	}
	return out
}

// curveRow is the substrate-tagged, durably-persisted record of ONE exposure — written immediately after the
// exposure so a mid-stream death loses nothing.
type curveRow struct {
	Substrate  string `json:"substrate"`
	Model      string `json:"model"`
	Exposure   int    `json:"exposure"`
	Goal       string `json:"goal"`
	Completion int    `json:"completion_tokens"` // the cache-immune decode cost — the curve y-axis
	Calls      int    `json:"calls"`
	Recalled   bool   `json:"recalled"` // synth step-0 recalled a minted skill (cost-falling mechanism)
	Minted     bool   `json:"minted"`   // the idle Consolidate minted a skill this exposure (the inflection)
	Grounded   bool   `json:"grounded"` // imported reality (held-positive-utility check)
	Solved     bool   `json:"solved"`   // answer oracle / grounded-success (utility held?)
	Signature  string `json:"signature"`
	Fired      bool   `json:"fired"` // the faculty fired (capability axis, caveated)
	Timestamp  string `json:"ts"`
}

// claudeCurveEngineFactory builds a workspace-wired claude-backed engine seeded from stateDir — the claude
// mirror of curveEngineFactory. cfg.Workspace wires the REAL executor (the bank grounds), cfg.Features=nil ==
// config.New() AllOn (SkillMint + Persist + watched_sync ON so the autonomous flywheel runs), the real model
// is the RealityComprehender. The shared stateDir is what makes the recurrence counter persist across the
// per-exposure fresh engines.
func claudeCurveEngineFactory(model, workspace string) func(stateDir string) (*engine.Engine, error) {
	return func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Workspace = workspace
		cfg.Features = nil // == config.New() AllOn: SkillMint + Persist + watched_sync ON
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

// TestSkillCurve_Claude drives the metered self-improvement curve. It runs the recurring grounded family over
// a3Exposures() exposures sharing ONE persisted state dir on claude, recording each exposure's completion
// tokens + faculty fire + recall/mint flags, persisting each row DURABLY as it lands, and printing the curve.
// It does NOT hard-fail on a NO-SIGNAL/flat cost result (the magnitude IS the finding) — but it DOES fail
// loudly if the family never grounded (held-positive utility unmet) or the mint never fired (the flywheel is
// dead), because then the curve is meaningless.
func TestSkillCurve_Claude(t *testing.T) {
	if os.Getenv("THOUGHT_CAMPAIGN_BACKEND") != "claude" {
		t.Skip("set THOUGHT_CAMPAIGN_BACKEND=claude to arm the metered A3 curve pilot (off by default)")
	}
	model := os.Getenv("THOUGHT_CLAUDE_MODEL") // "" → bridge default (sonnet primary + haiku utility)
	substrate := "claude:sonnet+haiku"
	if model != "" {
		substrate = "claude:" + model
	}
	t.Logf("SUBSTRATE: %s (tiered; temperature NOT controllable on this substrate)", substrate)

	root := a3StateRoot()
	stateDir := filepath.Join(root, "state") // the SHARED persisted state dir the counter accumulates in
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir %s: %v", stateDir, err)
	}
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}

	exposures := a3Exposures()
	stream := CurveStream(a3CurveFamily(), exposures)
	t.Logf("A3 curve pilot: %d exposures over the recurring grounded family (MintAfter=3); workspace %s; state %s",
		exposures, ws, stateDir)
	t.Logf("rows persist DURABLY per-exposure to %s (a mid-stream death loses at most the in-flight exposure)",
		filepath.Join(root, "curve-rows.jsonl"))

	b := EngineBencher{
		MaxTicks:  20,
		NewEngine: claudeCurveEngineFactory(model, ws),
		// serial (Concurrency 0) — and SkillCurve is inherently serial (each exposure must persist before the
		// next reloads the accumulated counter); gentle on the claude rate limit.
	}

	// DURABLE per-exposure persist: SkillCurveStreamed fires onPoint the moment each exposure completes, so a
	// mid-stream death loses at most the in-flight exposure (appendCurveRow fsyncs). The row is also logged as
	// it lands so the live log shows progress.
	start := time.Now()
	pts, err := b.SkillCurveStreamed(stream, stateDir, func(p CurvePoint) {
		t.Logf("exposure %d | completion=%d calls=%d recalled=%v minted=%v grounded=%v solved=%v fired(act)=%v",
			p.Exposure, p.Completion, p.Calls, p.Recalled, p.Minted, p.Grounded, p.Solved, p.Fired)
		row := curveRow{
			Substrate: substrate, Model: model, Exposure: p.Exposure, Goal: p.Goal,
			Completion: p.Completion, Calls: p.Calls, Recalled: p.Recalled, Minted: p.Minted,
			Grounded: p.Grounded, Solved: p.Solved, Signature: p.Signature, Fired: p.Fired,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := appendCurveRow(root, row); err != nil {
			t.Errorf("persist curve row exposure %d: %v", p.Exposure, err)
		}
	})
	if err != nil {
		t.Fatalf("SkillCurveStreamed on claude: %v", err)
	}
	elapsed := time.Since(start)

	// --- the curve summary (rows already persisted durably above) ---
	var firstMint, firstRecall, groundedN, solvedN = -1, -1, 0, 0
	t.Logf("=== A3 SELF-IMPROVEMENT CURVE (substrate=%s, N=%d exposures) ===", substrate, len(pts))
	for _, p := range pts {
		if p.Minted && firstMint < 0 {
			firstMint = p.Exposure
		}
		if p.Recalled && firstRecall < 0 {
			firstRecall = p.Exposure
		}
		if p.Grounded {
			groundedN++
		}
		if p.Solved {
			solvedN++
		}
	}

	// the cohort cost bend (pre-recall vs post-recall mean completion) — the headline the ruler characterizes.
	preComp, preN, postComp, postN := 0, 0, 0, 0
	for _, p := range pts {
		if firstRecall >= 0 && p.Exposure >= firstRecall {
			postComp += p.Completion
			postN++
		} else {
			preComp += p.Completion
			preN++
		}
	}
	preMean, postMean := 0.0, 0.0
	if preN > 0 {
		preMean = float64(preComp) / float64(preN)
	}
	if postN > 0 {
		postMean = float64(postComp) / float64(postN)
	}
	t.Logf("--- CURVE SUMMARY (N=%d) ---", len(pts))
	t.Logf("first mint exposure   = %d", firstMint)
	t.Logf("first recall exposure = %d", firstRecall)
	t.Logf("PRE-recall  mean completion = %.1f tok/exposure (n=%d)", preMean, preN)
	t.Logf("POST-recall mean completion = %.1f tok/exposure (n=%d)", postMean, postN)
	t.Logf("COST BEND (pre-post) = %+.1f tok  [%s]", preMean-postMean, bendLabel(preMean-postMean))
	t.Logf("grounded %d/%d  solved %d/%d (held-positive utility check)", groundedN, len(pts), solvedN, len(pts))
	t.Logf("elapsed: %s; rows persisted under %s", elapsed.Round(time.Second), root)
	t.Logf("FEED THESE ROWS TO ruler.CharacterizeCurve FOR THE COST VERDICT (offline, STEP 3).")

	// HELD-POSITIVE-UTILITY GATE: the curve is meaningless if it never grounded.
	if groundedN == 0 {
		t.Fatalf("A3 HELD-POSITIVE-UTILITY UNMET: the curve grounded 0 times — the cost bend is at zero grounding " +
			"(the W5-2b caveat), not held-positive utility. Check the workspace executor + the model's search->read.")
	}
	// FLYWHEEL GATE: the curve is meaningless if the mint never fired (no inflection to bend at).
	if firstMint < 0 {
		t.Fatalf("A3 FLYWHEEL DEAD: the skill never minted across %d exposures — the recurrence counter did not "+
			"accumulate to MintAfter(3). No self-improvement curve exists.", len(pts))
	}
}

// appendCurveRow writes one JSON row to the durable, substrate-tagged curve ledger — flushed per exposure.
func appendCurveRow(root string, row curveRow) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	path := filepath.Join(root, "curve-rows.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(row); err != nil {
		return err
	}
	return f.Sync() // durable: fsync per exposure so a death loses at most the in-flight row
}

func bendLabel(bend float64) string {
	switch {
	case bend > 0:
		return "curve BENT DOWN (recall cheaper — the W5 self-improvement signal)"
	case bend < 0:
		return "curve BENT UP (NEGATIVE — recall costlier; synthesis-skip offset by grounding decode)"
	default:
		return "FLAT"
	}
}

var _ = fmt.Sprintf
