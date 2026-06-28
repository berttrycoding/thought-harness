package engine_test

// rpiv_test.go — the COGNITION-property test for the VALIDATIVE faculty + its RPIV (Research -> Plan ->
// Implement -> Validate) standing capability (docs/internal/notes/2026-06-19-seed-intent-hierarchy-redesign.md
// §13.5 "the missing validation faculty"; cognitive-functions research §4.3).
//
// It pins the THINKING the spec intends, not the plumbing:
//   - the Validative faculty FIRES under the fair-share scheduler (its standing root gets focus), AND
//   - when its root is focused with the RPIV knob ON, the RPIV program runs its FOUR ordered phases
//     (research, plan, implement, validate), AND
//   - the VALIDATE phase closes on a GROUNDED check (a keep-or-revert verdict against a floor — the loop's
//     independent reward signal, NOT same-model self-judgment).
//
// Deterministic: the TestBackend test double + cpyrand seed=7, no model tokens, no clock, no unseeded RNG.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// rpivFeatures builds the awake-profile feature set with the faculty scheduler + RPIV optionally on.
func rpivFeatures(rpiv bool) *config.HarnessConfig {
	c := config.New()
	a := &c.Conscious.Activity
	a.Forest = true
	a.SeedIntents = true
	a.SeedIntentCount = cognition.SeedPortfolioSize() // full portfolio — the Validative root is on the bench
	a.DriveAgenda = true
	a.Soft = true
	a.BranchPropensity = 0.5
	a.FacultyScheduler = true // RPIV is wired on the scheduler's focus path
	a.AttentionWidth = 1
	a.RPIV = rpiv
	c.Validate()
	return c
}

// TestValidativeFacultyFires proves the new Validative faculty is a real, schedulable standing root: with
// the full portfolio + the fair-share scheduler, the validative line gets focus (it is not a dead enum
// value). This is the new-faculty cognition claim — the faculty competes for the workspace on its own
// behalf (the §3 criterion test 3), it is not just a label.
func TestValidativeFacultyFires(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, rpivFeatures(false))
	for i := 0; i < 60; i++ {
		eng.Step()
	}
	sawValidative := false
	for _, ev := range log.of(events.Attention) {
		if f, _ := ev.Data["faculty"].(string); f == "validative" {
			sawValidative = true
			break
		}
	}
	if !sawValidative {
		t.Fatal("the Validative faculty never got focus under the fair-share scheduler — it is a dead enum, not a live faculty")
	}
}

// TestRPIVRunsFourPhases is the headline RPIV cognition test: with conscious.activity.rpiv ON, focusing the
// Validative root runs the RPIV program through its FOUR ordered phases (research -> plan -> implement ->
// validate), each emitting conscious.rpiv, and the VALIDATE phase consults a GROUNDED check (a keep-or-revert
// verdict against a floor). This asserts the program actually RAN (not that the loop merely ticked) and that
// the validation phase grounds on an independent signal — the antidote to the same-model ceiling.
func TestRPIVRunsFourPhases(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, rpivFeatures(true))
	for i := 0; i < 80; i++ {
		eng.Step()
	}

	rpivEvents := log.of(events.RPIV)
	if len(rpivEvents) == 0 {
		t.Fatal("RPIV ON: no conscious.rpiv events — the RPIV capability is not wired into the live awake loop")
	}

	// Every one of the four phases must have fired at least once (the program ran end-to-end).
	phasesSeen := map[string]int{}
	for _, ev := range rpivEvents {
		if p, _ := ev.Data["phase"].(string); p != "" {
			phasesSeen[p]++
		}
	}
	for _, want := range []string{cognition.RPIVResearch, cognition.RPIVPlan, cognition.RPIVImplement, cognition.RPIVValidate} {
		if phasesSeen[want] == 0 {
			t.Fatalf("RPIV phase %q never fired — the program did not run all four phases (saw %v)", want, phasesSeen)
		}
	}

	// The phases must appear in ORDER within a single run (research before plan before implement before
	// validate) — RPIV is a SEQ, not an unordered fan-out. Check the first complete cycle.
	order := []string{cognition.RPIVResearch, cognition.RPIVPlan, cognition.RPIVImplement, cognition.RPIVValidate}
	oi := 0
	for _, ev := range rpivEvents {
		p, _ := ev.Data["phase"].(string)
		if oi < len(order) && p == order[oi] {
			oi++
		}
	}
	if oi < len(order) {
		t.Fatalf("RPIV phases did not appear in research->plan->implement->validate order (matched %d/%d)", oi, len(order))
	}

	// The VALIDATE phase is the load-bearing one: it must carry a GROUNDED keep-or-revert verdict (the
	// independent reward signal), NOT a bare narrative. Assert the verdict fields are present and sane.
	sawGroundedValidate := false
	for _, ev := range rpivEvents {
		if p, _ := ev.Data["phase"].(string); p != cognition.RPIVValidate {
			continue
		}
		dec, hasDec := ev.Data["decision"].(string)
		_, hasGrounded := ev.Data["grounded"].(bool)
		score, hasScore := ev.Data["score"].(float64)
		if !hasDec || !hasGrounded || !hasScore {
			t.Fatalf("RPIV validate event missing the grounded-check verdict fields {decision, grounded, score}: %v", ev.Data)
		}
		if dec != "keep" && dec != "revert" {
			t.Fatalf("RPIV validate decision must be keep|revert (a real keep-or-revert verdict), got %q", dec)
		}
		if score < 0 || score > 1 {
			t.Fatalf("RPIV validate score must be a graded [0,1] value (the grounded magnitude), got %v", score)
		}
		// the validate phase must privilege a grounded SOURCE (reality), not bare recombination — this is what
		// makes it an independent check rather than self-judgment.
		if src, _ := ev.Data["source"].(string); src != cognition.SourceReality {
			t.Fatalf("RPIV validate must source from reality (the grounded close), got source=%q", src)
		}
		sawGroundedValidate = true
	}
	if !sawGroundedValidate {
		t.Fatal("RPIV never produced a grounded VALIDATE verdict — the validation faculty's whole point (an independent reward signal) is missing")
	}
}

// TestRPIVOffByteIdentical guards the default-OFF contract: with conscious.activity.rpiv OFF (the default)
// the validative root behaves like any other seed line — NO conscious.rpiv event fires, even though the
// scheduler still focuses the validative faculty. A flag-gated capability that leaks when off would break
// the additive contract.
func TestRPIVOffByteIdentical(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, rpivFeatures(false))
	for i := 0; i < 80; i++ {
		eng.Step()
	}
	if n := len(log.of(events.RPIV)); n != 0 {
		t.Fatalf("RPIV OFF: a conscious.rpiv event leaked (%d) — the capability is not properly flag-gated", n)
	}
}

// TestRPIVProgramTemplateVerifies pins the program-machinery contract: the RPIV template is built over the
// EXISTING verified operator catalog (no minting needed), so VerifyProgram passes against a fresh registry,
// the shape is the four-phase SEQ in order, and the VALIDATE step is annotated SourceReality (the grounded
// close). This is the pure-unit half (the engine test above is the wired half).
func TestRPIVProgramTemplateVerifies(t *testing.T) {
	prog := cognition.RPIVProgram("test and validate what I have learned", "general")
	cat := cognition.NewOperatorRegistry()
	if ok, issues := cognition.VerifyProgram(prog, cat); !ok {
		t.Fatalf("RPIV template must verify against the seed catalog with no minting, got issues: %v", issues)
	}
	steps := prog.Steps()
	if len(steps) != 4 {
		t.Fatalf("RPIV template must have exactly 4 phases, got %d (%v)", len(steps), prog.Shape())
	}
	wantOps := []string{"expose-affordances", "decompose", "generate", "validate"}
	for i, op := range wantOps {
		if steps[i].Operator != op {
			t.Fatalf("RPIV phase %d operator = %q, want %q", i, steps[i].Operator, op)
		}
	}
	// the implement/validate sourcing: validate closes on reality (the grounded check), implement is model.
	if steps[3].Source != cognition.SourceReality {
		t.Fatalf("RPIV validate step must source from reality (the grounded close), got %q", steps[3].Source)
	}
	if !prog.Synthesized {
		t.Fatal("RPIV template should be marked Synthesized (constructed on the fly for the goal)")
	}
}
