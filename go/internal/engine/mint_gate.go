package engine

import "github.com/berttrycoding/thought-harness/internal/eval"

// mint_gate.go builds the eval-object MINT GATE the engine attaches to convertibility (slice g, #22 /
// 01-subconscious.md §3.19). The stick scores a mint candidate by its grounded value against the same
// MintValue floor the heuristic uses, so an attached gate is admission-equivalent — its job is to make
// the eval object the principled "does it belong?" gate of record (with a comparative refine signal),
// not to change which candidates pass.

// refineEpsilon is the comparative dead-band (noise floor) for the per-registry refine loop (GAP 11,
// §3.20): a |Delta| below it reads Flat, so the loop refines on a real trend, not measurement noise.
// 0.05 mirrors the mint-value granularity — a sub-0.05 wobble is not a refine signal.
const refineEpsilon = 0.05

// mintGateStick returns the MeasuringStick used as the convertibility mint gate: Threshold = the mint
// value floor; the check reads the candidate's grounded value (a float64) and reports it as the score.
func mintGateStick(mintValue float64) *eval.MeasuringStick {
	return &eval.MeasuringStick{
		Name:      "mint-gate",
		Facet:     "specialist",
		Threshold: mintValue,
		Check: func(subject any) eval.Score {
			v, _ := subject.(float64)
			return eval.Score{Pass: v >= mintValue, Value: clamp01(v), Reason: "grounded mint value"}
		},
	}
}

// clamp01 clamps x to [0,1] (the eval Score.Value domain).
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
