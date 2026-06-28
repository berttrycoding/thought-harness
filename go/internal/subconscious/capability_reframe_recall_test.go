package subconscious

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// capability_reframe_recall_test.go proves the GAP-8 REMAINING WIRE / gap-5-deeper final sub-slice: with
// BOTH convert.skill_reframe and subconscious.capability ON, the CAPABILITY recalls a reframed skill by
// goal (relevance is the Capability's job now, §3.8) and produces the workflow FROM that recalled skill's
// runtime-resolved body — recovering the recall the reframe retired (Match/MatchWithinTier return nothing
// under the reframe). Default-OFF ⇒ the legacy synth.go library.Match/Expand Program flywheel runs
// unchanged (byte-identical). This is the COGNITION the slice intends (the recall FIRES via the entry, the
// resolved prompt reaches the worker, no fresh Synthesize), not just the plumbing.

// reframedRecallLib builds a registry with the reframe flag set and one TRIGGERED reframed skill whose
// triggers match the test goal — the recall target the Capability is meant to find.
func reframedRecallLib(t *testing.T) (*cognition.SkillRegistry, string) {
	t.Helper()
	lib := cognition.NewSkillRegistry(true)
	lib.SetReframe(true)
	const skillName = "ground-claim"
	const prompt = "Restate the claim, enumerate the concrete evidence each sub-part needs, and ground it."
	if _, ok := lib.MintReframedTriggered(skillName, "composite", prompt, nil,
		[]string{"ground the claim", "verify the evidence"}, "ground a claim against evidence"); !ok {
		t.Fatal("precondition: a triggered reframed skill must mint")
	}
	if !lib.Reframed() {
		t.Fatal("precondition: the registry must report Reframed()==true")
	}
	return lib, skillName
}

// TestCapabilityRecallsReframedSkill is the central property: BOTH flags ON, the Capability RECALLS a
// reframed skill whose triggers fire for the goal — it produces a "skill:<name>" workflow resolved from
// the reframed skill's body, NOT a fresh Synthesize. The resolved prompt is REAL: it rides the minted
// per-skill operator's Intent (the content a worker actually runs), never dropped into a no-op note.
func TestCapabilityRecallsReframedSkill(t *testing.T) {
	lib, skillName := reframedRecallLib(t)
	catalog := cognition.NewOperatorRegistry()

	cap := NewCapability("episode", []string{"ground"}, catalog, backends.NewTest())
	cap.Library = lib

	goal := "ground the claim about the cache hit rate"
	wf, res, ok := cap.Produce(goal, streamOf(goal))
	if !ok || wf == nil || res == nil {
		t.Fatalf("ON+ON with a matching reframed skill must produce a recalled workflow; got ok=%v wf=%v res=%v", ok, wf, res)
	}
	// PROVENANCE: the recall carries the legacy skill-match source the engine reads (reactive.go routes a
	// "skill:" source through the Goal lifecycle + path tracking) — NOT "llm"/"heuristic" (a fresh Synthesize).
	wantSource := "skill:" + skillName
	if res.Source != wantSource {
		t.Fatalf("a recalled reframed skill must tag Source %q (the recall, not a synthesis); got %q", wantSource, res.Source)
	}
	// REALITY OF THE PROMPT: the runtime-resolved skill body must reach the worker as the minted operator's
	// Intent. The catalog must now carry the per-skill operator whose Intent IS the resolved prompt.
	opName := reframedSkillOp(skillName)
	spec, found := catalog.Get(opName)
	if !found {
		t.Fatalf("the recall must MINT the per-skill operator %q so the resolved prompt reaches a worker (no stub)", opName)
	}
	if !strings.Contains(spec.Intent, "Restate the claim") {
		t.Fatalf("the minted operator's Intent must BE the runtime-resolved reframed prompt; got %q", spec.Intent)
	}
	// The produced workflow must run that operator (the recalled skill's body), not a synthesised shape.
	if !workflowRunsOp(wf, opName) {
		t.Fatalf("the produced workflow must run the recalled skill's operator %q; phases did not include it", opName)
	}
}

// TestCapabilityReframeOffRunsLegacyRecall proves default-OFF byte-identical: with convert.skill_reframe
// OFF (a non-reframed registry), the Capability NEVER takes the reframed-recall branch — it falls to
// cognition.Synthesize, whose step-0 library.Match (the W5-validated legacy recall flywheel) runs
// unchanged. A goal hitting a legacy composite skill's triggers recalls it via "skill:<name>" exactly as
// before this slice — the new path is dead code when the reframe is off.
func TestCapabilityReframeOffRunsLegacyRecall(t *testing.T) {
	lib := cognition.NewSkillRegistry(true) // reframe OFF (default) ⇒ legacy goal-matched skills
	catalog := cognition.NewOperatorRegistry()
	if lib.Reframed() {
		t.Fatal("precondition: a fresh registry must default reframe OFF")
	}

	cap := NewCapability("episode", []string{"compare"}, catalog, backends.NewTest())
	cap.Library = lib

	// This goal self-matches a legacy composite ("evaluate-options" trigger "versus") via synth.go step-0.
	goal := "compare postgres versus mysql for this workload"
	wf, res, ok := cap.Produce(goal, streamOf(goal))
	if !ok || wf == nil || res == nil {
		t.Fatalf("OFF: the legacy synth recall must produce a workflow; got ok=%v wf=%v res=%v", ok, wf, res)
	}
	if !strings.HasPrefix(res.Source, "skill:") {
		t.Fatalf("OFF: a legacy composite trigger must recall via synth.go's library.Match (skill:<name>); got %q", res.Source)
	}
	// The reframed per-skill operator namespace must NOT exist (the reframe branch was never taken).
	if catalog.Has(reframedSkillOp(res.Source[len("skill:"):])) {
		t.Fatal("OFF: the reframed-recall branch must be dead — no skill_<name> operator minted")
	}
}

// TestCapabilityReframeOnNoMatchSynthesizes proves the fall-through: reframe ON but NO RECALLABLE skill's
// triggers fire for the goal ⇒ the Capability falls to cognition.Synthesize exactly as today (it does not
// invent a recall where none applies). The produced workflow is a fresh synthesis ("heuristic"/"llm"),
// not a "skill:" recall.
//
// "RECALLABLE" includes BOTH shapes the reframe-on path serves (the gap-8 remaining-wire FIX): reframed
// (prompt-bodied) skills AND legacy Program-bodied matchable skills (the seed composites + every minted
// skill). The original goal here ("compare …") fired the LEGACY composite "evaluate-options" trigger
// ("compare") — which is now correctly RECALLED (the fix), so it is no longer a no-match. The goal is now
// an optimisation-shaped one that recognises a SHAPE (so the fall-through still synthesises a workflow) but
// matches NO recallable skill trigger (reframed or legacy) — the genuine no-recall fall-through.
func TestCapabilityReframeOnNoMatchSynthesizes(t *testing.T) {
	lib, _ := reframedRecallLib(t) // reframe ON, the only reframed skill triggers on "ground the claim"
	catalog := cognition.NewOperatorRegistry()

	cap := NewCapability("episode", []string{"optimize"}, catalog, backends.NewTest())
	cap.Library = lib

	// This goal fires NEITHER the reframed skill's "ground the claim" triggers NOR any legacy composite/
	// synthesized skill trigger (the seed library has no "optimize/throughput" skill — only the synth SHAPE
	// recognises it). So the Capability's MatchRecallableWithinTier returns nothing and it falls to the
	// deterministic heuristic shape — a fresh synthesis, never a "skill:" recall.
	goal := "optimize the throughput of this queue"
	wf, res, ok := cap.Produce(goal, streamOf(goal))
	if !ok || wf == nil || res == nil {
		t.Fatalf("ON+no-match must still synthesise a workflow; got ok=%v wf=%v res=%v", ok, wf, res)
	}
	if strings.HasPrefix(res.Source, "skill:") {
		t.Fatalf("ON+no-match must NOT recall a skill (no recallable trigger fired); got a recall source %q", res.Source)
	}
}

// TestCapabilityReframeRecallRespectsTierCeiling proves the §3.3a authority bound is honoured: a Scope
// whose skill-tier ceiling is below the reframed skill's tier REFUSES the recall (a worker cannot staff
// above the ceiling), so the Capability falls through to Synthesize instead of recalling the deep skill.
func TestCapabilityReframeRecallRespectsTierCeiling(t *testing.T) {
	lib, _ := reframedRecallLib(t) // the reframed skill is tier "composite" (TierLevel 2)
	catalog := cognition.NewOperatorRegistry()

	cap := NewCapability("episode", []string{"ground"}, catalog, backends.NewTest())
	cap.Library = lib
	cap.WithScope(NewScope("", nil, 1)) // ceiling = unit (1) ⇒ a composite (2) reframed skill is out of band

	// The goal fires the reframed trigger ("ground the claim") AND recognises a shape ("compare"), so when
	// the ceiling REFUSES the recall the Capability still synthesises a workflow (the fall-through is real).
	goal := "ground the claim, then compare the two cache options"
	_, res, ok := cap.Produce(goal, streamOf(goal))
	if !ok || res == nil {
		t.Fatalf("a ceiling-refused recall must still fall through to a synthesised workflow; got ok=%v", ok)
	}
	if strings.HasPrefix(res.Source, "skill:") {
		t.Fatalf("the composite reframed skill is above the unit ceiling — recall must be refused, not %q", res.Source)
	}
}

// workflowRunsOp reports whether any phase of the workflow runs the named operator (the recalled skill's
// resolved body) — the wiring check that the resolved prompt's operator is actually scheduled.
func workflowRunsOp(wf *Workflow, opName string) bool {
	for _, ph := range wf.Phases {
		if ph.OpName == opName {
			return true
		}
		for _, st := range ph.Plan.Steps {
			if st.Operator == opName {
				return true
			}
		}
	}
	return false
}
