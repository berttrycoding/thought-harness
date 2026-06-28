package grounding

import "testing"

// defaultLayers is the shipped grounding stack: compute (built) + the two deferred extension points.
func defaultLayers() []Layer {
	return []Layer{ComputeLayer{}, SimulatorLayer{}, ProofLayer{}}
}

// TestLayeredGroundingComputeHandles: an arithmetic claim is grounded by the compute layer; the deferred
// layers correctly defer, so the loop still resolves it.
func TestLayeredGroundingComputeHandles(t *testing.T) {
	v, layer := GroundWithLayers("12 * 31 = 372", defaultLayers())
	if v != Grounded || layer != "compute" {
		t.Fatalf("arithmetic should be grounded by the compute layer; got %v from %q", v, layer)
	}
	if v, _ := GroundWithLayers("12 * 31 = 999", defaultLayers()); v != Refuted {
		t.Fatalf("a wrong computation should be refuted; got %v", v)
	}
}

// TestDeferredLayersDefer is the N.1b+ / N.1c gate: the simulator and proof layers are registered
// extension points that currently DEFER (no validated implementation) — a claim only they could handle
// stays UNGROUNDED (NotComputable), never fabricated, until a real implementation is plugged in.
func TestDeferredLayersDefer(t *testing.T) {
	// a physics claim no built layer can ground.
	v, layer := GroundWithLayers("the projectile lands at 42.7 meters", defaultLayers())
	if v != NotComputable || layer != "" {
		t.Fatalf("an un-handlable claim must stay ungrounded (deferred), not fabricated; got %v from %q", v, layer)
	}
}

// TestSimulatorPullForward proves the extension point: plugging in a simulator Solve makes the SAME loop
// ground a simulated claim — a drop-in, no re-architecture. (This is what "pull forward when a domain
// needs it" means.)
func TestSimulatorPullForward(t *testing.T) {
	sim := SimulatorLayer{Solve: func(claim string) (Verdict, bool) {
		if claim == "the projectile lands at 42.7 meters" {
			return Grounded, true // a (stub) physics sim says yes
		}
		return NotComputable, false
	}}
	v, layer := GroundWithLayers("the projectile lands at 42.7 meters", []Layer{ComputeLayer{}, sim, ProofLayer{}})
	if v != Grounded || layer != "simulator" {
		t.Fatalf("a plugged-in simulator should ground the claim; got %v from %q", v, layer)
	}
}

// TestProofLayerOutranksOnPullForward: a plugged-in prover sits at the highest tier, so it is tried
// first — the strongest ground truth wins.
func TestProofLayerOutranksOnPullForward(t *testing.T) {
	prover := ProofLayer{Prove: func(claim string) (Verdict, bool) { return Grounded, true }}
	// compute would also handle "2 + 2 = 4", but the proof layer (higher tier) is tried first.
	_, layer := GroundWithLayers("2 + 2 = 4", []Layer{ComputeLayer{}, prover})
	if layer != "proof" {
		t.Fatalf("the higher-tier proof layer should be tried first; grounded by %q", layer)
	}
}
