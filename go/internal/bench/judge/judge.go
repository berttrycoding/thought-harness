// Package judge is the LLM-judge ruler — ONE implementation of the rubric-clause judge shared by
// the generator pipeline (internal/bench/gen) and the Tier-A scorer (internal/bench/tiera). It was
// extracted from gen so the tiera scorer can reconcile a det-oracle-vs-trace disagreement (the
// measuring-stick "MEASURED" method) without an import cycle, and so the pass-detection lives in
// exactly one place (one truth). It is a leaf: it imports only backends/types/cpyrand.
package judge

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Verdict is the result of judging one output against one rubric clause: the binary pass, a [0,1]
// confidence the judge reports, the raw rationale (kept for Phase-0 replay + audit), and Backed —
// false when the backend declined / produced an off-shape answer, in which case the caller treats
// it as abstain (the deterministic floor stands), per measuring-stick-spec §5.4.
type Verdict struct {
	// Pass is the judge's binary verdict on the rubric clause.
	Pass bool
	// Confidence is the judge's self-reported [0,1] confidence (0 when not backed).
	Confidence float64
	// Rationale is the judge's one-line reason (for audit + Phase-0 replay).
	Rationale string
	// Backed is true when the verdict came from the backend; false = the backend declined (the
	// caller must fall back to the deterministic floor, never silently pass).
	Backed bool
}

// Run judges scenarioOutput against the rubric clause through a backends.Backend at the backend's
// own (runner-pinned-low) temperature. Under backends.NewTest() it is deterministic and offline.
// The judge reads the criterion as the goal and the output as the sole context, then maps the
// response to pass/fail by an affirmative marker — conservative, so an ambiguous response abstains
// (Backed=true, Pass=false) rather than laundering a maybe into a pass.
func Run(scenarioOutput, rubric string, backend backends.Backend) Verdict {
	if backend == nil {
		return Verdict{Backed: false, Rationale: "no judge backend"}
	}
	goal := "As an impartial judge, decide PASS or FAIL: does the OUTPUT satisfy the criterion? Criterion: " + rubric
	ctx := []types.Thought{{Text: "OUTPUT: " + scenarioOutput, Source: types.INJECTED, Confidence: 1.0}}
	rng := cpyrand.New(seedFrom(scenarioOutput, rubric))
	raw := strings.TrimSpace(backend.Generate(goal, ctx, rng))
	if raw == "" {
		return Verdict{Backed: false, Rationale: "judge backend declined"}
	}
	pass := affirmsPass(raw)
	return Verdict{
		Pass:       pass,
		Confidence: confidence(raw, pass),
		Rationale:  raw,
		Backed:     true,
	}
}

// seedFrom derives a stable RNG seed from the judged content so the test double is reproducible per
// (output, rubric) pair (the Phase-0 test-retest property).
func seedFrom(output, rubric string) uint64 {
	const fnvOffset = 1469598103934665603
	const fnvPrime = 1099511628211
	h := uint64(fnvOffset)
	for _, b := range []byte(output + "\x00" + rubric) {
		h ^= uint64(b)
		h *= fnvPrime
	}
	return h
}

// affirmsPass reads a judge response for an affirmative verdict. Conservative: only an explicit
// affirmative marker counts as PASS; a "fail"/"no" anywhere vetoes (even if "pass" also appears).
// The deterministic test double never emits these markers, so under it Run returns a stable
// Backed=true / Pass=false abstain — the safe default for an un-characterized judge.
func affirmsPass(raw string) bool {
	low := strings.ToLower(raw)
	if strings.Contains(low, "fail") || strings.HasPrefix(low, "no") {
		return false
	}
	for _, marker := range []string{"pass", "yes", "correct", "satisfied", "criterion met"} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

// confidence is a coarse confidence proxy (Phase-0 replaces it with characterized ICC/2AFC
// numbers): a decisive PASS marker → 0.9, an abstain → 0.5.
func confidence(raw string, pass bool) float64 {
	if pass {
		return 0.9
	}
	return 0.5
}
