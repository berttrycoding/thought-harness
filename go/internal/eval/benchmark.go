package eval

// Reference-eval = BENCHMARK (§3.19): the ABSOLUTE pass/fail mode. A generic
// stick every minted-or-primitive reference must pass — "does this belong in
// the registry?" This is the mint gate, generalised: a runtime-created thing is
// measured against the stick once, and the binary verdict decides admission.

// Benchmark applies a stick to a subject in ABSOLUTE mode and returns the
// pass/fail verdict plus the measurement that produced it. The verdict is:
//
//   - if the stick has a graded bar (Threshold > 0): Score.Value >= Threshold;
//   - otherwise: the check's own Score.Pass.
//
// This is "does this belong?" — there is no comparison to other subjects, only
// the fixed bar. It is the building block of the mint gate.
func Benchmark(stick MeasuringStick, subjectID string, subject any, tick int64) (pass bool, m Measurement) {
	m = stick.Measure(subjectID, subject, tick)
	return stick.passes(m.Score), m
}

// passes is the absolute verdict for one score against this stick's bar.
func (s MeasuringStick) passes(sc Score) bool {
	if s.Threshold > 0 {
		return sc.Value >= s.Threshold
	}
	return sc.Pass
}

// MintGate is the benchmark mode wired as a registry admission gate (§3.19,
// §3.15): a candidate reference is admitted into its registry iff it clears the
// stick. Returns the verdict + the measurement so the caller can record the
// gating decision in the trace / ledger. A candidate that fails is NOT minted.
//
// A registry attaches one stick (its mint-gate stick) and runs every candidate
// through this before Store — the uniform "does it belong?" gate.
func MintGate(gate MeasuringStick, candidateID string, candidate any, tick int64) (admit bool, m Measurement) {
	return Benchmark(gate, candidateID, candidate, tick)
}
