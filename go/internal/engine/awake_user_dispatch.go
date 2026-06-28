package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// AWAKE-DISP rung 0 — engage the subconscious on an awake user line, the way the reactive loop does.
//
// The measured bug (docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md): the awake interrupt path
// (continuous.go) handles a user turn by forking/focusing a branch onto the ongoing graph (OnInterrupt) and
// setting graph.Goal — but it NEVER synthesises a workflow for the user's goal. The reactive loop, by
// contrast, runs startEpisode per user turn, which calls cognition.Synthesize -> SetWorkflow (reactive.go).
// That synthesised workflow IS the subconscious dispatch's relevance entry (e.workflow, recognised in
// Dispatch). With no workflow wired, the awake dispatch RUNS every tick but recognises nothing and stays
// QUIET (subconscious.fire=0) — even on a focused user line holding the raw text the same workflow fires on
// 3x in reactive (the `build` workflow). The pin: focus is correct (the user branch IS active) and the raw
// text IS present in ActiveContext — the only thing missing is the synthesised workflow.
//
// maybeAwakeUserDispatch closes that gap, flag-gated and once-per-branch:
//   - awake-only, gated by conscious.activity.awake_user_dispatch (default OFF ⇒ no-op ⇒ byte-identical);
//   - fires only on a FOCUSED, UNRESOLVED user line (UnresolvedUserInput on the active branch) — not on the
//     endogenous wander, not on a resolved line;
//   - fires ONLY on a TASK-SHAPED goal (cognition.RecognizeShape) — a conversational / social / simple-Q&A
//     turn carries NO workflow shape, so it is left to answer the NORMAL awake way (the social/respond
//     specialist fires on the user line and the Controller DELIVERs fast). This is the conversational-
//     regression gate (see below);
//   - synthesises a workflow for graph.Goal exactly as the reactive LEGACY (capability-off) path does
//     (cognition.Synthesize -> subconscious.FromProgram -> SetWorkflow), so the subconscious dispatch has a
//     relevance entry to recognise; the Controller's existing DELIVER (goal-met) closes the line;
//   - guarded once per branch (awakeDispatchedBranches) so it synthesises at most ONCE per user line — it is
//     NOT a forced "always dispatch+deliver on every input" (that is the engagement ladder's rung 1).
//
// THE CONVERSATIONAL-REGRESSION GATE (the RecognizeShape pre-check, fixing the awake-bundle GO-LIVE
// regression). The measured bug: on a SIMPLE conversational turn ("hi i am here to ask you some questions")
// this used to fire cognition.Synthesize unconditionally — a wasted SynthesizeProgram model CALL whose only
// effect on a conversational goal was to perturb the live awake stream (the frontier model declines a
// program shape for chitchat, so ok=false and no workflow is set). The perturbation displaced the fast
// tick-0 social-specialist DELIVER, after which BranchExhausted -> BACKTRACK preempted the user line OFF the
// active branch before it reached the AwakeUserBudget deliver deadline — so the turn was NEVER answered
// (the awake "won't answer" bug). RecognizeShape is the DETERMINISTIC, model-free, RNG-free task-shape
// classifier (the SAME one Synthesize falls back to): if the goal has no task shape, this whole faculty is a
// complete no-op (no guard set, no model call, no stream perturbation) ⇒ a conversational turn answers
// exactly as it does with the flag OFF, byte-identical on that path. A task-shaped goal (the rate-limiter
// `design … a system` case the feature is FOR) still engages the subconscious, unchanged.
//
// Called from the continuous loop after focus is settled for the tick (the afterSelect site), BEFORE
// dispatch reads e.workflow on the same tick. Deterministic (graph + goal text only; no clock/RNG of its
// own — cognition.Synthesize on the test double is deterministic). Reactive mode never reaches here.
func (e *Engine) maybeAwakeUserDispatch(tick int) {
	if !e.awakeUserDispatchOn() || e.graph == nil || e.subconscious == nil {
		return
	}
	ab := e.graph.ActiveBranch
	// Only a focused, still-unanswered user line engages the subconscious — a resolved line or an
	// endogenous wander line does not.
	if !e.graph.UnresolvedUserInput(ab) {
		return
	}
	goal := e.graph.Goal
	if goal == "" {
		return
	}
	// CONVERSATIONAL-REGRESSION GATE: only a TASK-SHAPED goal engages the subconscious. RecognizeShape is the
	// deterministic, model-free shape classifier (no model call, no RNG, no stream perturbation). A goal with
	// no task shape — a greeting / social / simple-Q&A turn — is left to answer the normal awake way (the
	// social/respond specialist + the Controller's DELIVER). Checked BEFORE the once-per-branch guard so a
	// conversational line never even consumes the guard: the faculty is a complete no-op on it, byte-identical
	// to the flag-OFF path. This keeps the dispatch-engagement benefit for the engineering/task goals the
	// feature is for while removing the wasted SynthesizeProgram call that broke conversational delivery.
	//
	// THE ENGAGEMENT DECISION (rung 0 floor + rung 2 Pattern-C ceiling). RecognizeShape is the deterministic
	// FLOOR: a clearly task-shaped line engages, everything else does NOT by the floor alone. AWAKE-DISP rung 2
	// (awake_user_engage_judge, default OFF) adds the model CEILING over the floor's no-engage for the FUZZY
	// MIDDLE only — a SUBSTANTIVE, non-task-shaped line the lexical floor cannot classify. The model never
	// overrides a structural verdict (a task-shaped line always engages, a trivial greeting is never escalated);
	// it only LIFTS a flagged-fuzzy no-engage to engage. A non-escalation (rung-2 off / not in the fuzzy band /
	// no judge / model declines / "quiet") lets the FLOOR STAND.
	//
	// RecognizeShape is model-free (no call, no RNG, no stream perturbation), so it runs BEFORE the guard:
	// when the floor says no-engage AND rung 2 cannot escalate (off / not in the fuzzy band), the faculty is a
	// complete no-op that never even consumes the per-branch guard — byte-identical to the flag-OFF rung-0 path.
	_, shaped := cognition.RecognizeShape(goal, e.workingContext())
	if !shaped && !e.engagementCeilingEligible(goal) {
		return // floor no-engage + no eligible ceiling ⇒ no-op, no guard consumed (byte-identical)
	}
	// Once per user line: decide + (on engage) synthesise EXACTLY ONCE, then let it ride. The guard is set
	// BEFORE the model is consulted so the rung-2 ceiling fires AT MOST ONCE per branch (the cost guard: never
	// every tick) — the spec's "the model only fires on the ambiguous middle, gated by the floor, never every
	// tick". Without this the loop would re-consult the model every tick the user line holds focus.
	if _, done := e.awakeDispatchedBranches[ab]; done {
		return
	}
	e.awakeDispatchedBranches[ab] = struct{}{}
	// The floor's structural engage (a task-shaped line) synthesises directly; otherwise the rung-2 ceiling is
	// consulted once for this fuzzy line, and synthesises only on a model "engage".
	if shaped || e.engageCeiling(ab, goal) {
		e.synthesizeAwakeWorkflow(goal)
	}
}

// synthesizeAwakeWorkflow mirrors the reactive LEGACY (subconscious.capability OFF) synth path: build a
// verified program for the goal, wrap it as a Workflow, and wire it onto the subconscious as the dispatch's
// relevance entry. The awake default has capability off, so this is the live awake path; the capability-ON
// staffing (Scope / rich Context) is reactive's startEpisode concern and is intentionally not duplicated
// here (rung 0 is the minimal engagement fix, not a second episode-open).
func (e *Engine) synthesizeAwakeWorkflow(goal string) {
	// WEB-SEARCH (subconscious.web_search): the web-aware variant so an awake user lookup question that hit
	// no other shape staffs expose-affordances (the under-staffing fix); webLookup() false unless the flag
	// is on ⇒ byte-identical awake default.
	res, ok := cognition.SynthesizeWeb(goal, e.workingContext(), e.catalog, e.backend, e.bus.Emit, e.skills, e.webLookup())
	if !ok || res == nil {
		// No workflow shape applies (a simple Q&A goal) — leave the subconscious as it was. The base
		// specialists still fire by relevance on the user line; this only adds the synthesised-workflow
		// relevance entry when the goal HAS a program shape (the `build`/engineering case the bug measured).
		return
	}
	e.convert.NoteProgram(goal, res.Program) // trace->skill: track recurring programs (same as reactive)
	wf := subconscious.FromProgram(&res.Program, e.catalog, e.backend, e.bus.Emit, goal)
	// T1.1 (subconscious.query_formulation): stamp the query-formulation gate so an awake web_search worker
	// formulates the query from the actual question. OFF (the default) ⇒ a no-op stamp ⇒ byte-identical.
	wf.WithQueryFormulation(e.queryFormulation())
	e.subconscious.SetWorkflow(wf)
	e.wireDispatchEntry(nil) // capability-off awake path ⇒ the Workflow self-triggers (binary), unchanged
}

// engagementCeilingEligible is the model-free pre-check the live loop runs BEFORE the once-per-branch guard:
// is the rung-2 ceiling on AND the line in the deterministically-flagged FUZZY band (substantive but not
// task-shaped)? It is closed-form (no model call, no event, no RNG) — so a flag-OFF or obvious-trivial line
// is a complete no-op that never consumes the per-branch guard (byte-identical to the rung-0 path). The
// actual model consultation (engageCeiling) only runs once the guard is claimed, so the model fires at most
// once per user line (the cost guard: never every tick).
func (e *Engine) engagementCeilingEligible(goal string) bool {
	if e.features == nil || !e.features.Conscious.Activity.AwakeUserEngageJudge {
		return false // rung 2 off ⇒ not eligible (no event, no model call)
	}
	// A trivial greeting / very short ambient line is an OBVIOUS no-engage the floor already handles — it is
	// NOT escalated (the model is consulted only on the genuinely ambiguous middle).
	return engageFuzzyBand(goal)
}

// engageCeiling is the AWAKE-DISP rung-2 Pattern-C engagement CEILING — the actual model consultation,
// called ONCE per user line (after the once-per-branch guard is claimed) when the rung-0 deterministic floor
// (RecognizeShape) said NOT task-shaped on a focused unresolved user line and engagementCeilingEligible held.
// It returns true ("engage the subconscious on this line anyway") only when an EngagementJudge backend is
// wired and the model judges it worth engaging ("engage"). Every other path returns false and lets the FLOOR
// STAND — surfaced via escalation.floor_stands when the model was consulted-and-declined or unavailable while
// in the fuzzy band (Rule 4, never silent). Callers gate it on engagementCeilingEligible first; it re-checks
// the flag + fuzzy band defensively so a direct call (the property test) is self-contained.
func (e *Engine) engageCeiling(branch int, goal string) bool {
	if !e.engagementCeilingEligible(goal) {
		return false // not eligible ⇒ the floor stands silently (not an escalation)
	}
	judge, ok := e.backend.(backends.EngagementJudge)
	if !ok {
		e.bus.Emit(events.EscalationFloorStands, "engage floor stands (no model ceiling)",
			events.D{"branch": branch, "site": "engage", "floor_decision": "quiet"})
		return false // no judge (e.g. the test double) ⇒ the floor stands, surfaced
	}
	ctxBlob := ""
	if e.graph != nil {
		ctxBlob = engageContextBlob(e.workingContext())
	}
	verdict, decided := judge.JudgeEngagement(goal, ctxBlob, "quiet")
	if !decided || verdict != "engage" {
		e.bus.Emit(events.EscalationFloorStands, "engage floor stands (model "+floorStandReason(decided, verdict)+")",
			events.D{"branch": branch, "site": "engage", "floor_decision": "quiet", "model_consulted": true})
		return false // declined / "quiet" / off-shape ⇒ the floor's no-engage stands, surfaced
	}
	// The model LIFTED the floor's no-engage to engage on a fuzzy substantive line — witness it, then engage.
	e.bus.Emit(events.EngageJudge, "engage ceiling lifted b"+itoa(branch)+": quiet -> engage",
		events.D{"branch": branch, "goal": goal, "floor": "quiet", "verdict": "engage"})
	return true
}

// floorStandReason names why the rung-2 ceiling did not escalate (for the escalation.floor_stands message).
func floorStandReason(decided bool, verdict string) string {
	if !decided {
		return "declined"
	}
	return "quiet" // the model was consulted and judged the line not worth engaging
}

// engageFuzzyBand is the deterministic rung-2 fuzzy-band classifier: is a non-task-shaped awake user line
// SUBSTANTIVE enough to warrant a model engage/quiet judgment, vs an obvious trivial/ambient no-engage the
// floor already handles? Pattern-A: closed-form, model-free, RNG-free. The band is "substantive" = either a
// genuine question (has a question mark or opens with an interrogative) OR a longer line (>= a word threshold)
// that the lexical task-shape classifier did not catch — the ambiguous middle between a clearly task-shaped
// request and a one-word greeting. A short greeting / acknowledgement is NOT in the band (the floor stands
// silently). Kept conservative so the model fires only on genuinely fuzzy cases (the cost guard).
func engageFuzzyBand(goal string) bool {
	t := strings.TrimSpace(strings.ToLower(goal))
	if t == "" {
		return false
	}
	// A question is substantive enough to escalate (the floor's lexical task shapes miss many real questions).
	if strings.Contains(t, "?") {
		return true
	}
	for _, w := range []string{"how ", "what ", "why ", "when ", "where ", "which ", "who ", "can you", "could you", "should i", "should we", "is it", "are there", "do you", "would you"} {
		if strings.HasPrefix(t, w) || strings.Contains(t, " "+w) {
			return true
		}
	}
	// A longer line the task-shape classifier did not catch is the ambiguous middle worth a judgment; a short
	// line (a greeting / one-liner ack) is the obvious no-engage the floor handles.
	return len(strings.Fields(t)) >= engageFuzzyMinWords
}

// engageFuzzyMinWords is the word-count floor for the "substantive longer line" arm of the fuzzy band — a
// conservative threshold so a greeting / short ack ("hi there", "thanks, sounds good") never escalates while
// a genuine multi-clause line the lexical task-shapes missed does.
const engageFuzzyMinWords = 8

// engageContextBlob renders the recent awake working context into a compact text block for the rung-2
// engagement judgment prompt (the model's situational read of the awake stream around the user line).
func engageContextBlob(ctx []types.Thought) string {
	const max = 6
	if len(ctx) > max {
		ctx = ctx[len(ctx)-max:]
	}
	parts := make([]string, 0, len(ctx))
	for _, t := range ctx {
		if s := strings.TrimSpace(t.Text); s != "" {
			parts = append(parts, "- "+s)
		}
	}
	return strings.Join(parts, "\n")
}

// awakeUserDispatchOn reports whether the AWAKE-DISP rung-0 engagement is enabled (the opt-in knob
// conscious.activity.awake_user_dispatch, default OFF). Only meaningful in the awake/continuous loop; nil-safe.
func (e *Engine) awakeUserDispatchOn() bool {
	return e.features != nil && e.features.Conscious.Activity.AwakeUserDispatch
}
