package tiera

import (
	"encoding/json"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// TestContinuousAutonomyFrozenProbe is the end-to-end attribution check for the redesigned
// continuous-autonomy Tier-A (measuring-stick-spec §3.4): each gold item, run through the harness
// arm as a frozen-snapshot single-decision probe, must (a) decode, (b) produce the expected
// forced-choice class via the engine's awake decision policy, (c) emit the continuous.decision
// isolation witness, and (d) under the gate-off ablation (endogenous drive OFF) NOT reproduce the
// endogenous answer — so gate-on != gate-off where the awake regime is the binding constraint.
func TestContinuousAutonomyFrozenProbe(t *testing.T) {
	items, err := LoadItems("../banks/pilot/continuous-autonomy-tiera.jsonl")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("want 6 continuous-autonomy gold items, got %d", len(items))
	}
	r := runner.New(runner.TestFactory, "")
	const seed int64 = 1729

	for _, it := range items {
		// materialization must decode to JSON (the loader base64-decodes the []byte field).
		var probe map[string]any
		if err := json.Unmarshal(it.Artifact.Materialization, &probe); err != nil {
			t.Fatalf("%s: materialization not JSON: %v", it.ID, err)
		}
		if it.Artifact.Kind != "frozen-snapshot" {
			t.Fatalf("%s: want frozen-snapshot artifact, got %q", it.ID, it.Artifact.Kind)
		}
		if it.TraceOracle == nil {
			t.Fatalf("%s: missing trace_oracle isolation guard", it.ID)
		}

		// HARNESS arm: the frozen probe must produce the expected answer + the witness.
		res := RunItem(it, benchtypes.ArmHarness, seed, runner.TestFactory)
		if !res.OracleVerdict {
			t.Fatalf("%s harness: oracle FAILED — got %q, want %q (%s)",
				it.ID, res.RawOutput, it.Oracle.Expected, res.EventsPointer)
		}
		if !res.IsolationResult {
			t.Fatalf("%s harness: isolation FAILED — no continuous.decision witness (%s)",
				it.ID, res.EventsPointer)
		}
		if !res.Pass {
			t.Fatalf("%s harness: Pass=false despite oracle+isolation true", it.ID)
		}

		// GATE-OFF arm: endogenous drive OFF. The deterministic safety/termination decisions
		// (NO_COMPULSIVE_ACT, QUIESCE) still hold; every ENDOGENOUS answer collapses to STAY_QUIET,
		// so for those items gate-off must NOT match the gold (isolation: the awake regime is the
		// binding constraint).
		off := r.Run(runner.Spec{
			Prompt: it.Prompt, Arm: benchtypes.ArmGateOff, Mechanism: it.Mechanism,
			Seed: seed, FrozenSnapshot: it.Artifact.Materialization,
		})
		if off.Unsupported {
			t.Fatalf("%s gate-off must be supported (endogenous_drive toggle): %s", it.ID, off.Note)
		}
		endogenousFamilies := map[string]bool{
			"RESUME_FRONTIER": true, "FRESH_CURIOSITY": true, "REACH_OUT": true, "OUTREACH_PROVENANCE": true,
		}
		if endogenousFamilies[it.Family] {
			if off.Text == it.Oracle.Expected {
				t.Fatalf("%s gate-off reproduced the endogenous answer %q — the ablation is not binding",
					it.ID, off.Text)
			}
		}
	}
}
