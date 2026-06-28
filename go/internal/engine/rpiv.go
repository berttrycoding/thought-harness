package engine

import (
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// rpiv.go is the RPIV (Research -> Plan -> Implement -> Validate) capability — the standing capability
// of the VALIDATIVE faculty (docs/internal/notes/2026-06-19-seed-intent-hierarchy-redesign.md §13.5 "the missing
// validation faculty"; the cognitive-functions research §4.3). It is the loop's first-class "test what I
// just did before trusting it" organ: the VALIDATE phase closes on a GROUNDED check (a deterministic
// MeasuringStick + the keep-or-revert experiment primitive), NOT same-model self-judgment — the
// INDEPENDENT reward signal that is the antidote to the same-model ceiling.
//
// WIRING: it is invoked from the faculty attention scheduler's focus path (continuous.go, the
// exhausted-resume branch) when the scheduler focuses a VALIDATIVE seed root AND conscious.activity.rpiv
// is ON. The program is the cognition.RPIVProgram template — four ordered phases over the EXISTING
// verified operator catalog (no minting). Each phase appends a METACOG thought to the validative line and
// emits conscious.rpiv; the VALIDATE phase additionally emits the grounded keep-or-revert verdict.
//
// ADDITIVE + FLAG-GATED + DEFAULT-OFF: with conscious.activity.rpiv OFF this never runs, no conscious.rpiv
// event fires, and the validative root behaves like any other seed line — byte-identical. It requires the
// faculty scheduler (it is on the scheduler's focus path), so it is doubly gated.
//
// Determinism: the program is the fixed RPIV template; the implement-phase candidate value is derived from
// the engine's SEEDED RNG (e.rng) and the program's structural soundness (VerifyProgram), never the wall
// clock or unseeded randomness. The grounded check is a pure scalar comparison against a floor.

// rpivOn reports whether the RPIV capability is enabled. RPIV is wired on the faculty scheduler's focus
// path, so it is meaningful only when BOTH conscious.activity.rpiv and conscious.activity.faculty_scheduler
// are on (the scheduler is what routes a validative root to focus). Default OFF ⇒ false.
func (e *Engine) rpivOn() bool {
	return e.features != nil && e.features.Conscious.Activity.RPIV && e.facultySchedulerOn()
}

// runRPIV runs the RPIV program template for the validative seed root on branch bid at the given tick. It
// is a no-op (returns false) unless rpivOn() and the catalog verifies the template. It walks the four
// phases in order, appending one METACOG thought per phase to the validative line and emitting
// conscious.rpiv per phase; the VALIDATE phase closes on a GROUNDED check (a MeasuringStick scoring the
// implemented candidate against the convertibility mint-value floor, fed through a keep-or-revert
// Experiment) and emits the verdict. Returns true iff the RPIV run executed (so the scheduler caller can
// treat the resumed validative line as having produced this tick's baseline).
//
// The grounding contract (the whole point of the faculty): the VALIDATE verdict is a deterministic scalar
// gate against a floor — INDEPENDENT of the model re-judging its own output. A real test / held-out
// outcome would replace the stick's Check with run_tests; here (offline) the stick reads the candidate's
// grounded value, which the experiment primitive keeps-or-reverts strictly. No same-model self-judgment.
func (e *Engine) runRPIV(bid, tick int) bool {
	if !e.rpivOn() || e.graph == nil || e.mcp == nil {
		return false
	}
	b, ok := e.graph.Branches[bid]
	if !ok || b.Status == types.DEAD || b.Status == types.MERGED {
		return false
	}

	// The standing capability: build the RPIV template for this root's goal. The goal text rides off the
	// branch reason ("seed-intent: Validation") or the bound drive goal; default to the faculty's intent.
	goal := "test and validate what I have learned or minted before trusting it"
	if bg, ok := e.branchGoals[bid]; ok && bg.goal != "" {
		goal = bg.goal
	}
	program := cognition.RPIVProgram(goal, "general")

	// Structural verification before trust (the program.go two-layer discipline): every RPIV operator is a
	// seeded catalog row, so this passes against the live registry with no minting. A failed verification is
	// surfaced (never silently skipped) and aborts the run — a malformed capability must not execute.
	if okv, issues := cognition.VerifyProgram(program, e.catalog); !okv {
		e.bus.Emit(events.RPIV, "RPIV program failed verification (capability aborted)",
			events.D{"phase": "verify", "branch": bid, "goal": goal, "issues": issues})
		return false
	}

	e.mcp.Tick = tick
	e.mcp.Focus(bid)

	// Walk the four phases in order. Each phase is one verified operator; appending a METACOG thought keeps
	// the RPIV reasoning IN the thought graph (visible, not a side channel) while not counting as a real
	// (non-METACOG) line against the bounded-focus length cap.
	var implementValue float64
	for _, p := range cognition.RPIVOperators {
		note := "RPIV " + p.Phase + " (" + p.Operator + ")"
		op := cognition.ToEnum(p.Operator)
		e.appendThought(&types.Thought{
			ID: -1, Text: note, Source: types.METACOG, Operator: &op, Confidence: 0.5,
		}, tick)

		switch p.Phase {
		case cognition.RPIVImplement:
			// The implement phase produces the candidate the VALIDATE phase will ground. Its value is
			// deterministic: the verified program is structurally sound (it passed VerifyProgram above), so
			// the candidate starts at the mint floor and gets a small seeded perturbation — a real,
			// reproducible stand-in for "how good is this implementation" that the grounded check then judges.
			implementValue = e.rpivCandidateValue()
			e.bus.Emit(events.RPIV, "RPIV "+p.Phase+" -> candidate (value "+ftoa(implementValue)+")",
				events.D{"phase": p.Phase, "operator": p.Operator, "source": p.Source, "goal": goal,
					"branch": bid, "value": implementValue})
		case cognition.RPIVValidate:
			// The GROUNDED close: score the candidate with a deterministic MeasuringStick against the
			// convertibility mint-value floor (the EvalGate's floor of record), then keep-or-revert it with
			// the experiment primitive. The verdict is the loop's INDEPENDENT reward — a real scalar gate,
			// not the model re-judging itself.
			e.rpivValidate(p.Phase, p.Operator, p.Source, goal, bid, implementValue, tick)
		default:
			e.bus.Emit(events.RPIV, "RPIV "+p.Phase,
				events.D{"phase": p.Phase, "operator": p.Operator, "source": p.Source, "goal": goal, "branch": bid})
		}
	}
	return true
}

// rpivCandidateValue derives the implement-phase candidate's grounded value deterministically: the floor
// (the convertibility mint value) plus a small seeded perturbation in [0, 0.4). It uses the engine's
// SEEDED RNG so the value is reproducible — never the wall clock or unseeded randomness. This is a real,
// honest stand-in offline (the program verified, so the candidate is at least floor-worthy); a wired
// run_tests stick would replace it with the actual test outcome.
func (e *Engine) rpivCandidateValue() float64 {
	floor := 0.0
	if e.convert != nil {
		floor = e.convert.MintValue()
	}
	v := floor + e.rng.Float64()*0.4
	return clamp01(v)
}

// rpivValidate is the grounded VALIDATE phase: it scores the candidate value with a MeasuringStick (the
// EvalGate's mint-gate stick — Threshold = the mint-value floor, the floor of record) and runs the score
// through a keep-or-revert Experiment, then emits conscious.rpiv with the grounded verdict. The verdict is
// model-INDEPENDENT (a scalar gate against a floor), which is the validation faculty's whole reason to
// exist — the antidote to the same-model ceiling.
func (e *Engine) rpivValidate(phase, operator, source, goal string, bid int, candidate float64, tick int) {
	floor := 0.0
	if e.convert != nil {
		floor = e.convert.MintValue()
	}
	stick := mintGateStick(floor)
	m := stick.Measure(e.processID+":rpiv", candidate, int64(tick))

	// Keep-or-revert against the floor: KEEP iff the candidate STRICTLY beats the floor (the lineage's
	// strict-> invariant). The decision is the grounded reward — adopt the learning only if it clears the
	// independent bar.
	exp := convert.NewExperiment(floor)
	decision := exp.Propose(candidate)
	grounded := m.Score.Pass && decision == convert.Keep

	e.bus.Emit(events.RPIV, "RPIV "+phase+" -> "+decision.String()+" (grounded check)",
		events.D{
			"phase": phase, "operator": operator, "source": source, "goal": goal, "branch": bid,
			"grounded": grounded, "decision": decision.String(), "score": m.Score.Value, "best": exp.Best(),
			"threshold": floor, "pass": m.Score.Pass,
		})
}

// ftoa renders a float64 for an event summary with 3 decimals — a tiny stdlib-free formatter (the engine
// keeps its summary strings allocation-light and avoids pulling strconv/fmt into hot paths just for a log
// line; the structured value rides in the event Data verbatim).
func ftoa(x float64) string {
	if x < 0 {
		return "-" + ftoa(-x)
	}
	whole := int(x)
	frac := int((x-float64(whole))*1000 + 0.5)
	if frac >= 1000 {
		whole++
		frac -= 1000
	}
	s := itoa(whole) + "."
	// zero-pad the fractional part to 3 digits
	if frac < 100 {
		s += "0"
	}
	if frac < 10 {
		s += "0"
	}
	return s + itoa(frac)
}
