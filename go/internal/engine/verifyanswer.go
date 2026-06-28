// verifyanswer.go — T2.1: the INDEPENDENT answer-verifier wiring (the flagship Tier-2 capability;
// docs/internal/notes/2026-06-23-cognitive-engine-capability-audit.md P1; docs/internal/2026-06-23-capability-
// enhancement-roadmap.md T2.1).
//
// THE PRINCIPLE. We MEASURED a same-model ceiling: a model re-judging its OWN reasoning chain cannot catch
// its own systematic errors, and self-consistency cannot fix a bias (Huang 2024 arXiv:2310.01798). The
// ONLY thing that breaks this is an INDEPENDENT signal — a tool / the world / a programmatic check, NOT the
// same model looking again. So before the harness COMMITS a final factual answer, this gate re-retrieves
// web evidence for the answer claim (the world is the independent signal) and checks whether that fresh
// evidence supports the committed answer:
//   - Supported   ⇒ commit (the terminal decision stands).
//   - Unsupported ⇒ do NOT commit as-is: the engine downgrades the terminal commit decision to THINK
//     (continue working the line) — using the SAME engine-level structural override mechanism as the
//     deadline / force-ground overrides (verifyAnswerDecision sits ABOVE the Controller, same authority
//     class). It NEVER silently overrides a structural fact; it surfaces critic.answer_verify.
//   - Unverifiable (no web, a non-lookup answer, or the re-retrieval returned nothing) ⇒ fall through to
//     today's behaviour, byte-identical (the gate is a no-op).
//
// INDEPENDENCE GUARANTEE. The signal checked against is the RE-RETRIEVED EVIDENCE (the verify package goes
// back to the world with a fresh query derived from the question + the candidate answer), never the model's
// own re-read of its chain. The optional model ceiling (AnswerSupportJudge) judges support against THAT
// evidence, never the original reasoning — so it is not the same-model self-correction the ceiling exists
// to break.
//
// BOUNDED. At most ONE extra re-retrieval + at most one ceiling call per answer-commit (the per-branch
// verifiedAnswerBranches marker: a branch is verified at most once, so a re-opened line that re-arrives at
// a commit does not re-verify forever). No loop, no fan-out — the regulator's plant is untouched.
//
// FLAG. controller.answer_verify (config.ControllerCfg.AnswerVerify; the event it emits is critic.answer_verify
// — the same knob-in-ControllerCfg / event-in-critic-namespace split as controller.active_resource ->
// critic.resource_trigger), default OFF ⇒ verifyAnswerEnabled is
// false ⇒ verifyAnswerDecision returns the floor decision verbatim ⇒ no fetch, no event ⇒ byte-identical.
// It is only meaningful when a web seam is ALSO wired (SetWeb); a web-blind engine verifies every answer as
// Unverifiable (the gate no-ops), so the flag alone never moves a web-blind run.
package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
	verifypkg "github.com/berttrycoding/thought-harness/internal/verify"
)

// verifyAnswerEnabled reports whether the T2.1 answer-verifier may be consulted — the opt-in
// controller.answer_verify knob is ON. Like the active-resource trigger, this is an OPT-IN default-OFF
// instrument, so OFF is a SILENT no-op (it does NOT emit config.skip — that event is for the default-ON
// ablation toggles whose bypass is the meaningful event; emitting a skip every commit would break the
// byte-identical goldens). Nil-safe (nil gate ⇒ inverted opt-in convention ⇒ OFF).
func (e *Engine) verifyAnswerEnabled() bool {
	return e.gates.answerVerify != nil && e.gates.answerVerify.Enabled()
}

// rebuildAnswerVerifier (re)builds the web-grounded verifier over the CURRENT web seam + the backend's
// optional AnswerSupportJudge ceiling. Called lazily at the verification site so a SetWeb AFTER NewEngine
// (the SetWeb-before-Run contract) is honoured — the verifier is constructed against e.web at use time,
// exactly like lazyWeb. The query formulation reuses the T1.1 FLARE wrapper-strip (subconscious.
// FormulateQuery) so the independent re-retrieval query is formed the same way the harness's other
// retrieval is — no duplicated heuristic. A nil web seam yields a web-blind verifier (every answer
// Unverifiable). The judge is the backend iff it implements AnswerSupportJudge (the test double does NOT,
// so on --backend test the floor stands — offline + deterministic).
func (e *Engine) rebuildAnswerVerifier() verifypkg.Verifier {
	var judge backends.AnswerSupportJudge
	if j, ok := e.backend.(backends.AnswerSupportJudge); ok {
		judge = j
	}
	queryFn := func(question, _ string) string { return subconscious.FormulateQuery(question) }
	e.answerVerifier = verifypkg.NewWebGrounded(e.web, judge, queryFn)
	return e.answerVerifier
}

// answerCommitClaim returns the candidate answer claim about to be committed on a terminal decision, or ""
// when the active line holds no committable answer claim. The claim is the active line's TIP thought — the
// confident INJECTED/GENERATED conclusion that GoalSatisfied closed the line on (a restated question, an
// OBSERVATION tip, or an empty line is NOT a committable answer claim, so it returns "" and the gate
// no-ops). This is the same surface GoalSatisfied keys the goal-met decision on, so the thing verified is
// exactly the thing being committed.
func (e *Engine) answerCommitClaim() string {
	if e.graph == nil {
		return ""
	}
	tip := e.graph.Last()
	if tip == nil {
		return ""
	}
	if tip.Source != types.INJECTED && tip.Source != types.GENERATED {
		return "" // a reality-OBSERVATION tip / a user line is not a model-authored answer claim to verify
	}
	text := strings.TrimSpace(tip.Text)
	if text == "" || strings.HasSuffix(text, "?") {
		return "" // empty, or a restated question — not a committable answer
	}
	return text
}

// verifyAnswerDecision is the Pattern-C OVERRIDE at the answer-commit site (mirroring forceGroundDecision /
// the deadline override, which sit ABOVE the Controller). It returns the decision the engine should
// actually execute:
//   - flag OFF / a non-terminal decision / a non-GOAL_MET terminal ⇒ the floor decision verbatim
//     (byte-identical — only a genuine answer-COMMIT is verified, never a give-up / wander close).
//   - flag ON + a GOAL_MET commit (STOP or DELIVER) with a committable answer claim ⇒ run the INDEPENDENT
//     web-grounded verifier ONCE for this branch:
//   - Supported / Unverifiable ⇒ the commit stands (the verifier never blocks an answer it could not
//     independently refute).
//   - Unsupported ⇒ downgrade to THINK (continue working the line) — the engine re-opens the branch so
//     the line keeps going rather than committing an answer the world does not corroborate.
//
// It is BOUNDED by verifiedAnswerBranches (at most one verification per branch). Every fired verification
// emits critic.answer_verify (Pattern-C: never silent), carrying the verdict + the independent query/
// evidence/source so the check is auditable. A web-blind engine (no SetWeb) verifies every answer as
// Unverifiable ⇒ the commit always stands ⇒ byte-identical even with the flag on.
func (e *Engine) verifyAnswerDecision(floor types.Decision) types.Decision {
	if !e.verifyAnswerEnabled() {
		return floor
	}
	// Only a genuine answer COMMIT is verified: a terminal STOP/DELIVER whose StopKind is GOAL_MET (a
	// confident, satisfied answer). A give-up close, a wander close, or any non-terminal decision is NOT an
	// answer the world can corroborate, so it stands untouched.
	if floor != types.STOP && floor != types.DELIVER {
		return floor
	}
	meta := e.controller.LastMeta
	if meta.StopKind == nil || *meta.StopKind != types.GOAL_MET.String() {
		return floor
	}
	if e.graph == nil {
		return floor
	}
	branch := e.graph.ActiveBranch
	if _, done := e.verifiedAnswerBranches[branch]; done {
		return floor // already independently verified this branch — bounded, do not re-verify (no loop)
	}
	claim := e.answerCommitClaim()
	if claim == "" {
		return floor // no committable answer claim (a question / observation / empty tip) — nothing to verify
	}
	// Spend the per-branch bound now: the verification FIRES, so even if it bottoms out at Unverifiable we
	// do not re-verify this branch (one independent check per branch).
	e.verifiedAnswerBranches[branch] = struct{}{}

	v := e.rebuildAnswerVerifier()
	res := v.Verify(verifypkg.Request{Question: e.graph.Goal, Answer: claim})

	// Surface the check on the bus (Pattern-C: never silent), carrying the INDEPENDENT signal it checked
	// against so the verdict is auditable. The override only acts on Unsupported; supported/unverifiable
	// commits stand (logged, not blocked).
	overridden := res.Verdict == verifypkg.Unsupported
	e.bus.Emit(events.AnswerVerify,
		"answer verify ("+res.Verdict.String()+"): "+runeSlice(claim, 60),
		events.D{
			"verdict":        res.Verdict.String(),
			"floor_verdict":  res.FloorVerdict.String(),
			"escalated":      res.Escalated,
			"claim":          runeSlice(claim, 120),
			"query":          res.Query,
			"evidence":       res.Evidence,
			"source":         res.Source,
			"reason":         res.Reason,
			"floor_decision": floor.String(),
			"overridden":     overridden,
			"branch":         branch,
		})

	if !overridden {
		return floor // supported (commit) or unverifiable (no-op) — the answer stands
	}
	// Unsupported: do NOT commit. Re-open the active branch so the THINK path keeps working the line (the
	// branch was about to close on GoalSatisfied; clearing actedBranches lets the line continue acting/
	// thinking rather than re-deciding straight back to STOP). The Controller's structural floor is
	// untouched — this is an engine-level override of the GOAL_MET terminal only, exactly like the deadline /
	// force-ground overrides.
	delete(e.actedBranches, branch)
	return types.THINK
}
