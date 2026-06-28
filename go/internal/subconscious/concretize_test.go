package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// genCand builds a fuel-needing abstract candidate (an INJECTED reason output, no payload).
func genCand(text string) *types.Candidate {
	dom := "build"
	op := types.GENERATE
	return &types.Candidate{Text: text, Source: types.INJECTED, Domain: &dom, Relevance: 0.5, Operator: &op}
}

// alwaysFuelNeeding classifies every candidate as a fuel-needing `generate` move (test classifier).
func alwaysFuelNeeding(c *types.Candidate) (string, bool, string) { return "generate", true, "" }

// neverFuelNeeding classifies nothing as fuel-needing (the pass-through control).
func neverFuelNeeding(c *types.Candidate) (string, bool, string) { return "", false, "" }

// identityFuse returns the fuel text unchanged (a deterministic stand-in for the model fuse).
func identityFuse(c *types.Candidate, f Fuel) string { return f.Text }

func allReprSources() *config.SourceToggles {
	r := config.AllOnRepr()
	return &r.Sources
}

// TestConcretizeStampsGroundedProvenance asserts a candidate fused from a GROUNDED rung (knowledge)
// records a FuelProvenance{Grounded:true}, keeps its INJECTED source, and blends in the fuel's trust.
func TestConcretizeStampsGroundedProvenance(t *testing.T) {
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{{Statement: "grounded fact", Kind: "fact", Grounded: true, Trust: 0.9}}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	c := genCand("[generate] draft placeholder")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, nil, nil, nil)
	if len(out) != 1 {
		t.Fatalf("grounded candidate was dropped: %d", len(out))
	}
	if out[0].Source != types.INJECTED {
		t.Fatalf("grounded candidate source = %v, want INJECTED", out[0].Source)
	}
	prov, ok := out[0].Payload.(types.FuelProvenance)
	if !ok || !prov.Grounded || prov.Source != "knowledge" {
		t.Fatalf("provenance not stamped: %+v ok=%v", out[0].Payload, ok)
	}
	if out[0].Relevance < 0.9 { // blended up to the fuel's trust
		t.Fatalf("relevance not blended with trust: %v", out[0].Relevance)
	}
	if out[0].Text != "grounded fact" {
		t.Fatalf("fused text = %q, want the grounded fact", out[0].Text)
	}
}

// TestConcretizeFabricatedKeepsGENERATED is the load-bearing invariant: a candidate fused from the
// GENERATED rung reaches the seam still stamped types.GENERATED (so the Filter distrusts it at 0.42) —
// concretize FEEDS the seam, it never launders the guess into a grounded source.
func TestConcretizeFabricatedKeepsGENERATED(t *testing.T) {
	// nothing grounded is sourceable -> the ladder bottoms out at the generated rung.
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{}, fakeGen{text: "an invented guess"},
		allReprSources(), nil, nil)
	c := genCand("[generate] draft placeholder")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, nil, nil, nil)
	if len(out) != 1 {
		t.Fatalf("generated candidate dropped: %d", len(out))
	}
	if out[0].Source != types.GENERATED {
		t.Fatalf("fabricated-fuel candidate source = %v, want GENERATED (the Filter must distrust it)", out[0].Source)
	}
	prov, ok := out[0].Payload.(types.FuelProvenance)
	if !ok || prov.Grounded || prov.Source != "generated" {
		t.Fatalf("generated provenance not stamped low-trust: %+v ok=%v", out[0].Payload, ok)
	}
}

// TestConcretizeDropsUnsourcedUnderStrictGrounding asserts that when nothing is sourceable AND generation
// is forbidden (the strict-grounding posture), the candidate is DROPPED — never voiced as a hollow shape.
func TestConcretizeDropsUnsourcedUnderStrictGrounding(t *testing.T) {
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{}, fakeGen{text: "guess"},
		allReprSources(), nil, nil)
	c := genCand("[generate] draft placeholder")
	// allowGenerated=false -> strict grounding: an unsourced fuel-needing candidate is dropped.
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, false, false, nil, nil, nil)
	if len(out) != 0 {
		t.Fatalf("unsourced candidate under strict grounding should be dropped, got %d", len(out))
	}
}

// TestConcretizePassesThroughNonFuelNeeding asserts a non-fuel-needing candidate (tool-backed ground
// truth / a stance) passes through UNTOUCHED — concretize only re-shapes the abstract moves.
func TestConcretizePassesThroughNonFuelNeeding(t *testing.T) {
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	c := genCand("compute: 6 × 7 = 42")
	before := c.Text
	out := Concretize([]*types.Candidate{c}, nil, p, neverFuelNeeding, identityFuse, true, true, nil, nil, nil)
	if len(out) != 1 || out[0].Text != before || out[0].Payload != nil {
		t.Fatalf("non-fuel-needing candidate was modified: %+v", out[0])
	}
}

// TestConcretizeGateBypass asserts a disabled subconscious.concretize gate bypasses the stage entirely
// (raw relay — the candidates reach the seam exactly as they fired).
func TestConcretizeGateBypass(t *testing.T) {
	off := false
	gate := config.NewGate("subconscious.concretize", func() bool { return off }, nil)
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{}, fakeGen{text: "g"}, allReprSources(), nil, nil)
	c := genCand("[generate] draft placeholder")
	before := c.Text
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, nil, gate, nil)
	if len(out) != 1 || out[0].Text != before {
		t.Fatalf("disabled concretize gate should raw-relay untouched, got %+v", out)
	}
}
