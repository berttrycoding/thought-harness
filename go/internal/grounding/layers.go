// layers.go is the layered grounding architecture and the extension points for the DEFERRED grounding
// layers (N.1b+ rich simulators, N.1c neuro-symbolic). The SR-4 spine grounds against several sources;
// they are tried in DESCENDING trust-tier order and the first that can handle a claim wins. Layer 2a
// (deterministic compute, N.1b) is BUILT. Layer 2b (physics/render simulators) and Layer 3
// (neuro-symbolic / proof) are DEFERRED — "no validated design yet / pull forward when a domain needs
// it" — so they ship as registered EXTENSION POINTS that currently DEFER (handled=false). The whole
// point: the architecture is complete and a real simulator or prover is a drop-in (set the Solve func),
// not a re-architecture. Building the physics engine / theorem prover itself is explicitly out of scope.
package grounding

// Layer is one grounding source: it either HANDLES a claim (returns its verdict) or DEFERS to a
// lower-tier layer (handled=false). Layers are tried highest-tier first.
type Layer interface {
	Name() string
	Tier() TrustTier
	Ground(claim string) (verdict Verdict, handled bool)
}

// ComputeLayer is grounding layer 2a (N.1b, BUILT): deterministic arithmetic.
type ComputeLayer struct{}

func (ComputeLayer) Name() string    { return "compute" }
func (ComputeLayer) Tier() TrustTier { return TierDeterministic }
func (ComputeLayer) Ground(claim string) (Verdict, bool) {
	r := EvaluateCompute(claim)
	if r.Verdict == NotComputable {
		return NotComputable, false // not arithmetic — defer to another layer
	}
	return r.Verdict, true
}

// SimulatorLayer is grounding layer 2b (N.1b+, DEFERRED): internal simulation — a physics engine, a
// renderer, a light-path tracer — for a claim whose ground truth is a deterministic SIMULATION. It is
// an extension point: with Solve nil it DEFERS (no validated simulator yet); plug in a per-domain Solve
// and the same loop grounds simulated claims. Tier = deterministic (a correct simulation doesn't lie).
type SimulatorLayer struct {
	Solve func(claim string) (Verdict, bool) // nil => deferred
}

func (SimulatorLayer) Name() string    { return "simulator" }
func (SimulatorLayer) Tier() TrustTier { return TierDeterministic }
func (s SimulatorLayer) Ground(claim string) (Verdict, bool) {
	if s.Solve == nil {
		return NotComputable, false // DEFERRED — pull forward when a domain needs simulated ground truth
	}
	return s.Solve(claim)
}

// ProofLayer is grounding layer 3 (N.1c, DEFERRED): neuro-symbolic / formal proof for a claim provable
// by a formal method. Extension point: with Prove nil it DEFERS (research frontier, no validated method
// yet); plug in a prover and the loop grounds provable claims. Tier = firsthand-validated (a proof is
// the strongest ground truth there is).
type ProofLayer struct {
	Prove func(claim string) (Verdict, bool) // nil => deferred
}

func (ProofLayer) Name() string    { return "proof" }
func (ProofLayer) Tier() TrustTier { return TierFirsthandValidated }
func (p ProofLayer) Ground(claim string) (Verdict, bool) {
	if p.Prove == nil {
		return NotComputable, false // DEFERRED — pull forward when a credible method exists
	}
	return p.Prove(claim)
}

// GroundWithLayers tries the layers in DESCENDING trust tier (highest first); the first that HANDLES the
// claim wins, and its layer name is returned. If every layer defers, the result is NotComputable with an
// empty layer — the claim has no grounding handle (it stays ungrounded, never fabricated).
func GroundWithLayers(claim string, layers []Layer) (Verdict, string) {
	// stable selection by tier desc (no sort import needed for a tiny slice; pick the best available
	// handler each pass).
	tried := make([]bool, len(layers))
	for done := 0; done < len(layers); done++ {
		best := -1
		for i, l := range layers {
			if tried[i] {
				continue
			}
			if best == -1 || l.Tier() > layers[best].Tier() {
				best = i
			}
		}
		if best == -1 {
			break
		}
		tried[best] = true
		if v, handled := layers[best].Ground(claim); handled {
			return v, layers[best].Name()
		}
	}
	return NotComputable, ""
}
