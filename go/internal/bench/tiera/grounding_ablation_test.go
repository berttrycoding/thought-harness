package tiera

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// TestGroundingGateOffDoesNotGround is the offline attribution check for the grounding ablation
// (measuring-stick-spec §3.1/§5.1). It runs ONE grounding gold item (a pointed question whose
// answer lives only in a materialized artifact) under the harness arm and the gate-off arm on the
// deterministic test backend, then asserts the grounding-read witness:
//
//   - harness  ⇒ GroundingReadHappened = TRUE  (the watched seam imports reality: action.observation)
//   - gate-off ⇒ GroundingReadHappened = FALSE (seam.watched_sync OFF — no reality import; priors only)
//
// This is the property the previous gate-off (seam.hidden_filter alone) violated: with only the
// Filter off, the harness STILL performed the watched-seam READ, so gate-off still grounded and the
// gate-on − gate-off contrast collapsed. seam.watched_sync OFF makes the gate-off arm genuinely
// unable to ground, so the mechanism-lift is cleanly attributable to the read.
func TestGroundingGateOffDoesNotGround(t *testing.T) {
	// The grounding gold map flips exactly seam.watched_sync (pin the contract).
	if toggles := runner.GateOffToggles(benchtypes.MechGrounding); len(toggles) != 1 || toggles[0] != "seam.watched_sync" {
		t.Fatalf("grounding gate-off toggles want [seam.watched_sync], got %v", toggles)
	}

	// grounding-A-gold-0004 (inlined to keep this test self-contained / cycle-free): a pointed
	// question whose grounded answer lives only in the materialized artifact, with a plausible
	// prior lure — the canonical "read reality, don't answer from priors" grounding shape.
	item := benchtypes.TierAItem{
		ID: "grounding-ablation-probe", Mechanism: benchtypes.MechGrounding,
		Prompt: "What port does the service actually bind to according to the deploy manifest? Read it; the README is stale.",
		Artifact: benchtypes.Artifact{
			Kind: "repo-file", Path: "deploy/service.yaml",
			Materialization: []byte("service:\n  name: api\n  port: 8443\n"),
		},
	}
	sb, cleanup, err := Materialize(item)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}

	r := runner.New(runner.TestFactory, sb.Root)
	const seed int64 = 7

	harness := r.Run(runner.Spec{
		Prompt: item.Prompt, Arm: benchtypes.ArmHarness, Mechanism: benchtypes.MechGrounding,
		Seed: seed, MaxTicks: 40,
	})
	if harness.Unsupported {
		t.Fatalf("harness arm must not be Unsupported: %s", harness.Note)
	}
	if hr := runner.GroundingReadHappened(harness.Events); !hr.OK {
		t.Fatalf("harness arm MUST ground (the watched seam imports reality): %s", hr.Reason)
	}

	gateOff := r.Run(runner.Spec{
		Prompt: item.Prompt, Arm: benchtypes.ArmGateOff, Mechanism: benchtypes.MechGrounding,
		Seed: seed, MaxTicks: 40,
	})
	if gateOff.Unsupported {
		t.Fatalf("grounding gate-off must be supported: %s", gateOff.Note)
	}
	if gr := runner.GroundingReadHappened(gateOff.Events); gr.OK {
		t.Fatalf("gate-off arm MUST NOT ground (seam.watched_sync OFF ⇒ no reality import), but it did: %s", gr.Reason)
	}
}
