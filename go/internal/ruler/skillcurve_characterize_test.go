package ruler

// skillcurve_characterize_test.go — A3 STEP 3 (offline): characterize a PILOT curve-rows.jsonl through the
// ruler. This reads the durable substrate-tagged rows the claude pilot wrote (skillcurve_claude_test.go)
// and prints the cost verdict (COST-RELIABLE / NOISY / DEGENERATE) + the capability direction, so the pilot
// is characterized with NO further claude calls (pure offline reduction). It is GUARDED on a row-file env
// var, so the default suite skips it (no fixed fixture path baked in).
//
// RUN (after the pilot lands its rows):
//   THOUGHT_A3_ROWS=data/registry-claude/a3-skillcurve/curve-rows.jsonl \
//     go test -run TestCharacterizePilotCurveRows -v ./internal/ruler/

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/campaign"
)

// pilotCurveRow mirrors the curveRow the claude pilot persists (the JSON tags must match
// campaign/skillcurve_claude_test.go's curveRow). Only the fields CharacterizeCurve needs are read.
type pilotCurveRow struct {
	Exposure   int    `json:"exposure"`
	Completion int    `json:"completion_tokens"`
	Calls      int    `json:"calls"`
	Recalled   bool   `json:"recalled"`
	Minted     bool   `json:"minted"`
	Grounded   bool   `json:"grounded"`
	Solved     bool   `json:"solved"`
	Signature  string `json:"signature"`
	Fired      bool   `json:"fired"`
}

// TestCharacterizePilotCurveRows reads THOUGHT_A3_ROWS (a curve-rows.jsonl) and characterizes it on the cost
// + capability axes. It asserts nothing about the magnitude (the curve IS the finding) — it prints the ruler
// verdict so the pilot's "does it bend, is it floor-clearing?" question is answered offline. Skips when the
// env var / file is absent.
func TestCharacterizePilotCurveRows(t *testing.T) {
	path := os.Getenv("THOUGHT_A3_ROWS")
	if path == "" {
		t.Skip("set THOUGHT_A3_ROWS=<curve-rows.jsonl> to characterize a pilot curve (offline)")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open rows %s: %v", path, err)
	}
	defer f.Close()

	var pts []campaign.CurvePoint
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r pilotCurveRow
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("parse row %q: %v", string(line), err)
		}
		pts = append(pts, campaign.CurvePoint{
			Exposure: r.Exposure, Completion: r.Completion, Calls: r.Calls,
			Recalled: r.Recalled, Minted: r.Minted, Grounded: r.Grounded, Solved: r.Solved,
			Signature: r.Signature, Fired: r.Fired,
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan rows: %v", err)
	}
	if len(pts) == 0 {
		t.Fatalf("no curve rows in %s", path)
	}

	cc := CharacterizeCurve(pts, Options{})
	t.Logf("=== A3 PILOT CURVE CHARACTERIZATION (N=%d exposures, source %s) ===", cc.Exposures, path)
	for _, p := range pts {
		t.Logf("exp %d: completion=%d calls=%d recalled=%v minted=%v grounded=%v solved=%v fired=%v",
			p.Exposure, p.Completion, p.Calls, p.Recalled, p.Minted, p.Grounded, p.Solved, p.Fired)
	}
	t.Logf("first mint exposure   = %d", cc.FirstMintExposure)
	t.Logf("first recall exposure = %d", cc.FirstRecallExposure)
	t.Logf("--- COST axis (PRIMARY) ---")
	t.Logf("PRE-recall  mean completion = %.1f tok/exposure", cc.PreMeanCompletion)
	t.Logf("POST-recall mean completion = %.1f tok/exposure", cc.PostMeanCompletion)
	t.Logf("COST BEND (pre-post) = %+.1f tok", cc.CostBend)
	t.Logf("within-cohort cost-σ = %.1f  cost-MDE = %.1f  cost-band(2σ) = %.1f",
		cc.Cost.CostSigmaWithin, cc.Cost.CostMDE, cc.Cost.CostBandHalfWidth)
	t.Logf("COST VERDICT = %s   FLOOR-CLEARED = %v", cc.Cost.CostVerdict, cc.CostFloorCleared)
	t.Logf("--- CAPABILITY axis (caveated, saturated) ---")
	t.Logf("PRE fire-rate = %.2f  POST fire-rate = %.2f  direction = %s",
		cc.PreFireRate, cc.PostFireRate, cc.FacultyDirection)
	t.Logf("--- utility (held-positive?) ---")
	t.Logf("PRE solved-rate = %.2f  POST solved-rate = %.2f  groundedAny = %v",
		cc.PreSolvedRate, cc.PostSolvedRate, cc.GroundedAny)
}
