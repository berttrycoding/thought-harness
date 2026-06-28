package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestCapabilityIsLiveDispatchEntry is the GAP 5-DEEPER wiring gate (subconscious side): when a
// recognizer is wired onto the SubconsciousEngine, the dispatch loop's per-tick workflow-recognition
// routes THROUGH it — the Capability is the live relevance/dispatch ENTRY, not the self-triggering
// Workflow. The proof is a SPY recognizer: the engine must call RecognizeWorkflow on it (the entry
// fires), the Workflow's own Recognize must NOT be the source of truth, yet the recognized flag (and
// thus GateBias) must end up IDENTICAL to the self-trigger verdict (the safe-stage byte-identity).
func TestCapabilityIsLiveDispatchEntry(t *testing.T) {
	wf := triggeredWorkflow(t, []string{"refactor"})
	spy := &spyRecognizer{}
	eng := NewSubconsciousEngine(nil, cpyrand.New(1), noopEmit, wf, nil)
	eng.SetRecognizer(spy)
	if eng.Recognizer() == nil {
		t.Fatal("Recognizer() must return the wired entry (the live-entry accessor)")
	}

	// A stream the workflow triggers on -> the entry must be the one that recognises it.
	ctx := dispatchCtx([]string{"should we refactor this module"})
	eng.Dispatch(ctx, 0.3, nil)

	if spy.calls == 0 {
		t.Fatal("the dispatch loop did NOT route recognition through the wired entry (the Capability is not the live entry)")
	}
	if spy.lastWf != wf {
		t.Fatalf("the entry was handed the wrong workflow: got %p want %p", spy.lastWf, wf)
	}
	// The dispatch loop must thread the LIVE θ (its admission threshold) into the entry on the signature, so
	// an implementation MAY thread it onward to a downstream value/admission gate. (Recognition itself is
	// permissive has-any and does NOT consult θ — θ-gating recognition is the refuted double-gate; this only
	// checks the live θ reaches the port, not a hardcoded value.)
	if spy.lastTheta != 0.3 {
		t.Fatalf("the entry was handed θ=%v, want the dispatch loop's live θ=0.3 (live θ not threaded to the port)", spy.lastTheta)
	}
	// The recognizer mutated the workflow's recognized flag (so GateBias reads it) — the load-bearing
	// Recognize-before-GateBias ordering is preserved through the entry.
	if !wf.Recognized() {
		t.Fatal("the entry's recognition must mutate wf.recognized (GateBias depends on it)")
	}
}

// TestNoRecognizerIsLegacySelfTrigger is the flag-OFF half: with NO recognizer wired (the default),
// the Workflow self-triggers (Workflow.Recognize) exactly as before — the entry seam is inert. The
// recognized flag and the fired result must match what the self-trigger path produces.
func TestNoRecognizerIsLegacySelfTrigger(t *testing.T) {
	wf := triggeredWorkflow(t, []string{"refactor"})
	eng := NewSubconsciousEngine(nil, cpyrand.New(1), noopEmit, wf, nil)
	if eng.Recognizer() != nil {
		t.Fatal("a fresh engine must have NO recognizer (the legacy self-trigger path is the default)")
	}
	ctx := dispatchCtx([]string{"should we refactor this module"})
	eng.Dispatch(ctx, 0.3, nil)
	if !wf.Recognized() {
		t.Fatal("the legacy path must still recognise a triggered workflow (Workflow.Recognize)")
	}
}

// TestGradedThetaAdmissionThroughDispatchLoop is the SUB-SLICE-2 wiring-gate property at the LIVE wire:
// with a recognizer set (flag-ON path) and a NON-bespoke workflow that PARTIALLY matches the stream
// (gradedRelevance = 0.5), the dispatch loop's RECOGNITION flips on θ — recognised when θ is below the
// graded score, NOT recognised when θ is above it. This proves the regulator's θ now GATES recognition
// through the live Dispatch loop (graded, not binary), and that the SAME θ Dispatch admits specialists at
// is the bar recognition clears. The binary path would have recognised in BOTH cases (a single keyword
// matched) — so this is the graded behaviour, not the old all-or-nothing one.
func TestGradedThetaAdmissionThroughDispatchLoop(t *testing.T) {
	// two triggers, the stream matches exactly ONE ⇒ gradedRelevance = 1/2 = 0.5.
	const stream = "please refactor this thing"
	mkWf := func() *Workflow { return bespokeWorkflow(t, []string{"refactor", "module"}, false) }
	ctx := dispatchCtx([]string{stream})

	// partial (has-any) match at low θ=0.3 ⇒ RECOGNISED through the entry (has-any; θ is not consulted).
	below := mkWf()
	engBelow := NewSubconsciousEngine(nil, cpyrand.New(1), noopEmit, below, nil)
	engBelow.SetRecognizer(&spyRecognizer{})
	engBelow.Dispatch(ctx, 0.3, nil)
	if !below.Recognized() {
		t.Error("θ=0.3, partial (has-any) match: the workflow MUST be recognised through the dispatch loop")
	}

	// θ above the graded score (0.6 > 0.5) ⇒ STILL recognised. CORRECTED (E5-deeper live A/B): recognition
	// is PERMISSIVE (has-any), NOT θ-gated — θ-gating recognition regressed multi-hop grounding (ON 0.71 vs
	// OFF 0.89); the θ/value bar admits DOWNSTREAM, not here. So a partial match is recognised at high θ too.
	above := mkWf()
	engAbove := NewSubconsciousEngine(nil, cpyrand.New(1), noopEmit, above, nil)
	engAbove.SetRecognizer(&spyRecognizer{})
	engAbove.Dispatch(ctx, 0.6, nil)
	if !above.Recognized() {
		t.Error("θ=0.6, partial (has-any) match: recognition MUST be permissive (recognised), not θ-gated")
	}

	// FLAG-OFF (legacy binary) byte-identity: NO recognizer ⇒ Workflow.Recognize, which is binary has-any —
	// a single keyword match ⇒ recognised at BOTH θ (the binary path ignores θ entirely). This is exactly
	// the legacy behaviour the OFF path must preserve, distinct from the graded ON path above.
	for _, theta := range []float64{0.3, 0.6} {
		legacy := mkWf()
		engLegacy := NewSubconsciousEngine(nil, cpyrand.New(1), noopEmit, legacy, nil)
		engLegacy.Dispatch(ctx, theta, nil)
		if !legacy.Recognized() {
			t.Errorf("flag-OFF (binary self-trigger), θ=%v: a single-keyword match MUST recognise (legacy byte-identity)", theta)
		}
	}
}

// TestCapabilityRecognizeIsGradedThetaAdmission is the cognition property at the recognition predicate
// level: Capability.RecognizeWorkflow decides PERMISSIVELY with the has-any criterion —
// `gradedRelevance(stream, Triggers) > 0`, the SAME relevance criterion as the legacy binary has-any
// keyword match — while the structural short-circuits (Exhausted ⇒ never; Bespoke ⇒ always-until-exhausted)
// are PRESERVED. theta is carried on the signature but NOT consulted at recognition. The cases pin the
// regimes:
//
//   - a FULL match (every trigger present) ⇒ recognised at any θ (same as binary);
//   - a NO match (no trigger present) scores 0 ⇒ never recognised (same as binary);
//   - a PARTIAL match (some-but-not-all triggers present) scores a FRACTION > 0 ⇒ STILL recognised, even at
//     a high θ — recognition answers "does this apply", not "is it worth firing". CORRECTED (E5-deeper live
//     A/B): θ-gating recognition (`gradedRelevance >= θ`) is the REFUTED double-gate — it dropped
//     weakly-but-genuinely-relevant non-bespoke workflows and regressed multi-hop grounding 0.89→0.71 (the
//     borrowed-threshold trap). The θ/value bar is a DOWNSTREAM admission gate, not a recognition gate.
//
// Exhausted/bespoke cases confirm the short-circuits are untouched. It also MUTATES wf.recognized in lockstep
// with the verdict (GateBias depends on it).
func TestCapabilityRecognizeIsGradedThetaAdmission(t *testing.T) {
	cases := []struct {
		name     string
		triggers []string
		bespoke  bool
		exhaust  bool
		stream   []string
		theta    float64
		want     bool
	}{
		// full match: gradedRelevance = 2/2 = 1.0 > 0 ⇒ recognised (theta not consulted).
		{"full-match-clears", []string{"refactor", "module"}, false, false, []string{"refactor this module"}, 0.4, true},
		// no match: gradedRelevance = 0/2 = 0.0, not > 0 ⇒ never recognised.
		{"no-match-dark", []string{"refactor", "module"}, false, false, []string{"write a poem"}, 0.4, false},
		// PARTIAL match (1 of 2 ⇒ 0.5) at high θ=0.6 ⇒ STILL recognised. CORRECTED (E5-deeper live A/B):
		// recognition is permissive (has-any, >0); θ does NOT gate it (θ-gating regressed multi-hop grounding
		// 0.89→0.71 — the refuted double-gate). The high θ is here only to prove θ is ignored at recognition.
		{"partial-recognised-high-theta", []string{"refactor", "module"}, false, false, []string{"refactor this thing"}, 0.6, true},
		// PARTIAL match (1 of 2 ⇒ 0.5) at low θ=0.3 ⇒ recognised (has-any; θ ignored either way).
		{"partial-recognised-low-theta", []string{"refactor", "module"}, false, false, []string{"refactor this thing"}, 0.3, true},
		// bespoke short-circuit: applies regardless of triggers/θ (synthesised for this goal).
		{"bespoke-applies", []string{"refactor"}, true, false, []string{"unrelated text"}, 0.9, true},
		// exhausted: never recognised, even bespoke, even a full keyword match.
		{"exhausted-bespoke", []string{"refactor"}, true, true, []string{"please refactor it"}, 0.1, false},
		{"exhausted-keyword", []string{"refactor"}, false, true, []string{"please refactor it"}, 0.1, false},
		// empty triggers ⇒ gradedRelevance 0 ⇒ a non-bespoke workflow stays dark (never fires on everything).
		{"empty-triggers-dark", []string{}, false, false, []string{"anything at all"}, 0.0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := dispatchCtx(tc.stream)
			via := bespokeWorkflow(t, tc.triggers, tc.bespoke)
			if tc.exhaust {
				exhaustWorkflow(via)
			}
			capa := NewCapability("episode", []string{"unrelated-cosmetic"}, cognition.NewOperatorRegistry(), backends.NewTest())
			got := capa.RecognizeWorkflow(via, ctx, tc.theta)
			if got != tc.want {
				t.Errorf("recognition verdict = %v, want %v (permissive has-any gradedRelevance>0 broken)", got, tc.want)
			}
			if via.Recognized() != tc.want {
				t.Errorf("recognized flag = %v, want %v (GateBias would diverge from the verdict)", via.Recognized(), tc.want)
			}
		})
	}
}

// TestGradedReducesToBinaryOnSingleTrigger pins the compatibility edge the design notes: for a
// SINGLE-trigger workflow the graded score is exactly 1.0 (match) or 0.0 (no match), so at any θ in
// (0,1) the graded verdict equals the binary has-any verdict — the binary path is graded recognition's
// degenerate single-trigger case. This guards that the generalisation did not perturb the common
// single-keyword workflow (most canonical triggers are one word).
func TestGradedReducesToBinaryOnSingleTrigger(t *testing.T) {
	const theta = 0.5
	for _, stream := range [][]string{{"please refactor it"}, {"just write a function"}} {
		ctx := dispatchCtx(stream)
		ref := bespokeWorkflow(t, []string{"refactor"}, false)
		binary := ref.Recognize(ctx)
		via := bespokeWorkflow(t, []string{"refactor"}, false)
		capa := NewCapability("episode", nil, cognition.NewOperatorRegistry(), backends.NewTest())
		graded := capa.RecognizeWorkflow(via, ctx, theta)
		if graded != binary {
			t.Errorf("stream %v: graded=%v != binary=%v (single-trigger compatibility broken)", stream, graded, binary)
		}
	}
}

// --- helpers ----------------------------------------------------------------------------------

// spyRecognizer is a WorkflowRecognizer test double that records every call (incl. the live θ it was
// handed) and delegates the verdict to the SAME graded predicate the real entry uses (recognizeViaGraded)
// so the dispatch loop behaves identically while we observe that the entry was the one consulted with the
// regulator's θ.
type spyRecognizer struct {
	calls     int
	lastWf    *Workflow
	lastTheta float64
}

func (s *spyRecognizer) RecognizeWorkflow(wf *Workflow, ctx []types.Thought, theta float64) bool {
	s.calls++
	s.lastWf = wf
	s.lastTheta = theta
	return wf.recognizeViaGraded(ctx, theta)
}

// triggeredWorkflow builds a minimal non-bespoke keyword-triggered workflow with one phase.
func triggeredWorkflow(t *testing.T, triggers []string) *Workflow {
	t.Helper()
	return bespokeWorkflow(t, triggers, false)
}

// bespokeWorkflow builds a one-phase workflow with the given triggers and bespoke flag.
func bespokeWorkflow(t *testing.T, triggers []string, bespoke bool) *Workflow {
	t.Helper()
	wf := NewWorkflow("test", []Phase{{Operator: cognition.ToEnum("generate"), OpName: "generate"}},
		bespoke, nil, triggers, cognition.NewOperatorRegistry(), backends.NewTest(), "", noopEmit)
	return wf
}

// exhaustWorkflow advances the cursor past the end so Exhausted() is true (recognition must then be
// false regardless of triggers/bespoke).
func exhaustWorkflow(wf *Workflow) {
	for !wf.Exhausted() {
		wf.Advance()
	}
}
