package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/interaction"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// -- continuous (awake) loop --------------------------------------------

// stepContinuous runs one tick of the awake regime: perception port + arousal + drives + default-mode
// wander + proactive outreach + awake STOP-softening. Mirrors Python Engine._step_continuous.
func (e *Engine) stepContinuous(tick int) StepResult {
	if e.graph == nil {
		// The awake mind wakes ITSELF: a queued message seeds the episode as a user turn; an empty
		// port self-seeds the wander. No synthetic "(awake)" kickoff exists anywhere — provenance
		// is which of these two paths ran (A4), never a text prefix.
		seed, ok := e.port.Pop()
		fromUser := ok && seed != ""
		if !fromUser {
			seed = "(awake — no task; mind is wandering)"
		}
		e.startEpisode(seed, fromUser)
		// B3-OUTREACH WIRING FIX (THOUGHT_WAKE_TRANSCRIPT, default OFF => byte-identical). The wake path
		// is the ONLY user-seed path that fails to persist the user turn — reactive.go:693 and the
		// percept-stream path below already append on their own paths (and Pop() FIFO-dequeued this seed,
		// so the percept-stream Stream() never re-delivers it). With the flag ON, append it here so
		// maybeReachOut's userTopics is populated and proactive outreach can fire. See waketranscript.go.
		if e.wakeTranscriptOn() && fromUser {
			e.transcript = append(e.transcript, transcriptTurn{Role: "user", Text: seed})
		}
		e.lifecycle.Transition(types.S_ACTIVE, "awake")
	}

	// C1 (§1.8): plant the standing seed-intent forest roots once, at the first awake tick (after the boot
	// episode opens), so the loop has endogenous lines to think about BEFORE any user input. Gated behind
	// conscious.activity.seed_intents (default OFF ⇒ no-op ⇒ byte-identical). Awake-only — the reactive loop
	// never calls this.
	e.seedForestIntents(tick)

	// arousal gates the regime.
	if e.arousal == types.ASLEEP {
		if e.lifecycle.State != types.IDLE {
			e.lifecycle.Transition(types.IDLE, "asleep — consolidating")
		}
		// CONFIG (M1): when ALL convert mints are OFF, skip the asleep consolidation (dreaming would
		// compile nothing). Bypass, not delete.
		if e.gates.convertConsol.Disabled() {
			e.gates.convertConsol.Skip("asleep consolidation bypassed")
		} else {
			e.convert.Consolidate()                             // dreaming = replay/compile
			e.convert.RefineRegistry()                          // GAP 11: the uniform per-registry refine pass (no-op unless convert.refine_loop)
			e.convert.ConsolidateFacts(e.knowledge, e.bus.Tick) // A-RAG5: consolidate high-V recalled facts -> priors (no-op unless convert.facts)
		}
		// Cross-session persistence + curator (M4): asleep consolidation is the awake-mode IDLE window —
		// persist + curate the learned state here too. nil store / persistence-off ⇒ no-op.
		e.curateState()
		// INTROSPECTIVE-FAITHFULNESS self-report (Track H §8, introspect_faithfulness.go): the awake ASLEEP
		// window is the continuous-mode quiescence — after the awake stream has thought, emit ONE self-report
		// checked against the addressable ground truth, the opaque subconscious layer honestly declined.
		// Bounded to once per quiescence (selfReportedEpisode); no-op unless introspect.self_report is ON ⇒
		// default OFF ⇒ no event ⇒ byte-identical.
		e.maybeSelfReport(tick)
		if e.percSalient() {
			e.arousal = types.AWAKE
			e.bus.Emit(events.Arousal, "ASLEEP -> AWAKE (salient percept)", events.D{"to": "AWAKE"})
		}
		e.idleRegulate()
		return StepResult{Tick: tick, State: e.lifecycle.State.String(), Idle: true, Note: "asleep",
			Meta: map[string]any{}}
	}

	// awake and thinking — the lifecycle reflects ACTIVE (not the episodic IDLE).
	if e.lifecycle.State != types.S_ACTIVE {
		e.lifecycle.Transition(types.S_ACTIVE, "awake")
	}
	gain := cognition.PerceptionGain(e.arousal)
	baseline := 0 // immigrant (μ) events this tick: percepts + drives + default-mode

	// 1. continuous perception (always on): percepts + async action feedback.
	for _, p := range e.percStream(gain) {
		cand := types.Candidate{Text: p.Text, Source: p.Source, Relevance: 0.9}
		v := e.filter.Admit(cand, e.graph.History(), e.valueScalar())
		if v.Admit() {
			pt := &types.Thought{ID: -1, Text: p.Text, Source: p.Source, Confidence: v.Confidence}
			e.appendThought(pt, tick)
			baseline++
			if p.Salient && p.Source == types.USER_INPUT {
				e.transcript = append(e.transcript, transcriptTurn{Role: "user", Text: p.Text})
				e.graph.Goal = p.Text
				e.mcp.Tick = tick
				e.controller.OnInterrupt(e.graph, e.mcp, *pt)
			}
		}
	}
	for _, po := range e.awatched.Poll(tick) {
		obs := po.Thought
		e.appendThought(&obs, tick)
		// Reality answered an async action: feed it into the grounding spine (N.1a) the same way the
		// synchronous path does, using the claim threaded through the polled observation.
		e.groundObservation(types.Intention{Claim: po.Claim}, obs)
		// Keep the branch marked as acted: reality answered, so INTEGRATE the result rather than
		// re-acting on the same line. Re-acting requires moving to a fresh line.
	}
	// Standing re-grounding (N.1a-cont / AR-6): poll sensors so a hallucination arising between acts is
	// refuted the moment a watcher contradicts it — no ACT. No-op until real watchers are registered.
	e.groundSensors()
	e.pruneBranches() // bound focus every awake tick, not only on fresh lines

	// 2. what to think about, in priority order:
	//    over-long line -> move on (bounded focus, Pattern-A structural; fresh line supplies μ)
	//    exhausted line -> resume a better sibling, else a drive/wander goal
	//    otherwise      -> task-driven specialist excitation
	// A SATISFIED line is NOT preempted here (A3): goal resolution belongs to the Controller's
	// decision spine — it sees the satisfied line, decides STOP, and the awake STOP-softening
	// (deliver if a user waits, resume the most valuable unfinished line, else open a fresh one)
	// follows ITS decision. The old top-of-tick satisfied->moves-on check closed lines the spine
	// never examined — including the user's.
	ctx := e.graph.ActiveContext()
	// Loop-closure / recurrence keyframe DB (Track F, F-M7): fingerprint the active line's tip into the
	// persistent recurrence index; a re-entry fires keyframe.close — the awake anti-rumination signal the
	// keyframe DB exists for (the un-persisted recurrence DB blocked it, gap G3). nil DB ⇒ inert.
	e.observeKeyframe(e.activeTip())
	// AWAKE-DISP rung 0 (awake_user_dispatch.go, conscious.activity.awake_user_dispatch, default OFF ⇒ no-op
	// ⇒ byte-identical). Focus is settled for this tick (the percept/interrupt path above already focused any
	// fresh user line). If the focused branch holds an UNRESOLVED user input and no workflow has yet been
	// synthesised for its goal, synthesise one and wire it onto the subconscious — the SAME SetWorkflow the
	// reactive loop runs per user turn (startEpisode). The measured bug: the awake interrupt path only forks/
	// focuses (OnInterrupt) and never synthesises, so the dispatch (the `else` arm below) recognises NOTHING
	// and stays QUIET on every awake user line. This wires the relevance entry BEFORE that dispatch reads
	// e.workflow this tick, so the synthesised workflow fires the way it does in reactive; the Controller's
	// existing DELIVER closes the line. Once per branch (a guard) — NOT a forced per-tick dispatch.
	e.maybeAwakeUserDispatch(tick)
	var raw []*types.Candidate
	bias := map[string]float64{}
	excitation := true // was this tick's thought driven by specialist excitation, or baseline?
	realLen := 0
	for _, t := range ctx {
		if t.Source != types.METACOG {
			realLen++
		}
	}
	if realLen > e.focusBound {
		// An over-long line is left behind — never resumed — so the awake baseline μ keeps
		// flowing: open a fresh line seeded with an endogenous goal (bounded focus, prune old).
		excitation = false
		e.mcp.Tick = tick
		reason := "mind moves on"
		fresh := e.graph.NewBranch(intPtr(e.graph.ActiveBranch), &reason)
		e.mcp.Focus(fresh)
		e.pruneBranches()
		// CONFIG (awake-regime-off): the endogenous FreshGoal is the awake baseline μ — a self-directed
		// goal minted with no user prompt. When conscious.endogenous_drive is OFF, the fresh line opens
		// (bounded-focus housekeeping stays) but seeds NO endogenous goal, so the awake loop runs only on
		// perception + task excitation — durable self-direction with no user turns is ablated.
		if e.endogenousEnabled() {
			// slice k (§7.2): when drive_agenda is on, seed a minted+conscience-gated DRIVE goal (the
			// development cross) instead of the plain musing; a conscience veto seeds nothing this tick.
			var seed *types.Thought
			if e.features != nil && e.features.Conscious.Activity.DriveAgenda {
				seed = e.mintAgendaDriveGoal()
			} else {
				seed = e.drives.FreshGoal()
			}
			if seed != nil {
				e.appendThought(seed, tick)
				baseline++
			}
		}
	} else if e.controller.BranchExhausted(e.graph) {
		excitation = false
		// Faculty attention scheduler (conscious.activity.faculty_scheduler, default OFF ⇒ this whole
		// block is skipped ⇒ byte-identical). When ON, the exhausted line resumes a standing faculty/drive
		// line by LEAST-RECENTLY-FOCUSED fair-share instead of pure frontier argmax — so every faculty gets
		// a turn (the perceptual/mnemonic starvation fix). USER lines still preempt (this path is the
		// endogenous, non-user "what next" selection; an unresolved user turn is handled upstream by the
		// percept/interrupt path). If the scheduler finds no live seeded root it returns false and the
		// existing argmax + endogenous-baseline path runs unchanged.
		if e.facultySchedulerOn() {
			if chosen, ok := e.scheduleFaculty(tick); ok {
				// RPIV capability (conscious.activity.rpiv, default OFF ⇒ skipped ⇒ byte-identical): if the
				// scheduler just focused a VALIDATIVE seed root, run its standing RPIV (research -> plan ->
				// implement -> validate) program — the VALIDATE phase closes on a grounded check (the loop's
				// independent reward signal). Any other faculty (or RPIV off) falls through unchanged.
				if e.rpivOn() && e.branchFaculty[chosen] == cognition.FacultyValidative {
					e.runRPIV(chosen, tick)
				}
				// a standing faculty line now holds focus; the loop reasons over it this tick (no extra
				// endogenous wander needed — the resumed line IS the μ baseline).
				goto afterSelect
			}
		}
		fr := e.graph.Frontier()
		if len(fr) > 0 && fr[0].Value >= e.controller.PursuitThreshold() {
			e.mcp.Tick = tick
			e.mcp.Focus(fr[0].ID) // resume a genuinely better unfinished line
		} else if e.endogenousEnabled() {
			// CONFIG (awake-regime-off): a drive-proposed goal / default-mode wander is endogenous
			// content. OFF ⇒ neither fires (the exhausted line just yields no new baseline this tick).
			g := e.drives.ProposeGoal(e.graph, e.valueScalar())
			if g != nil {
				e.appendThought(g, tick)
				baseline++
			} else {
				raw = candPtrs(e.defaultMode.Wander(e.rng))
				baseline += len(raw)
			}
		}
	} else {
		raw, bias = e.dispatch(ctx) // CONFIG (M1): gated pull-dispatch (OFF ⇒ CONSCIOUS generates)
	}

afterSelect:
	// #19 AUTONOMOUS STANDING-INTENT SENSING (autonomous_sense.go, conscious.activity.autonomous_sense,
	// default OFF ⇒ no-op ⇒ byte-identical). Focus is now settled for this tick. If the focused branch is a
	// standing PERCEPTUAL/INTROSPECTIVE seed root, fire ONE bounded sensor read FOR THAT FOCUS, on its own
	// (the live-wire of the seed root's dead-as-trigger BackedBy): perceptual → senseClock (+senseWeb when
	// sense.web is on); introspective → fold the engine's own self-state. The percept is injected as a
	// GENERATED thought + witnessed on perception.sense. BOUNDED: at most one sense per focus (a per-(branch,
	// tick) guard), no fan-out, no new operator/sub-agent — a single μ-baseline percept, so the branching
	// plant n is UNCHANGED (the #18 stability cell proves n<1 with this ON). Reactive mode never reaches here.
	e.maybeAutonomousSense(tick)

	// SELF-MODEL baseline declarative self-knowledge (self_model.go, sense.self_model, default OFF => no-op
	// => byte-identical). Focus is now settled for this tick. If the focused branch is a standing
	// INTROSPECTIVE seed root, ground the small STANDING CORE (identity + a bounded, constant-size capability
	// INDEX read from the real registries + runtime facts) as a GENERATED thought + emit perception.self_model
	// — refreshed only on a content-HASH change (standing, not per-tick, not resume-once). The per-capability
	// DETAIL is pulled LAZILY on demand (SelfModelLookup), never eagerly dumped. A single μ-baseline percept
	// APPEND, no fork — so the branching plant n is UNCHANGED (the #18 self-watch cell proves n<1 with this
	// ON). Reactive mode never reaches here.
	e.maybeGroundSelfModel(tick)

	// O-3 READ-ONLY LANE ROUTER (route_advisor.go, conscious.activity.route_advisor, default OFF => no-op =>
	// byte-identical). Focus is now settled for this tick. The router ranks the LIVE standing faculty/drive
	// lanes by V(s) under per-lane thresholds + cooldowns and emits conscious.route with the ranked next-
	// runnable + audit line. It DECIDES but NEVER DISPATCHES -- it does not change focus, seed a goal, or move
	// any operator/seed/fan-out/regulator (the plant is unchanged). The auto-dev "read-only router" brought
	// inward and run over the harness's own lanes (the dogfood-the-architecture differentiator). Reactive mode
	// never reaches here.
	e.maybeRouteAdvise(tick)

	// M3 §3.3: concretize the fuel-needing dispatched candidates before the hidden seam (the same stage
	// the reactive loop runs). Default-mode wander / self-prompt candidates carry no fuel-needing
	// operator, so they pass through; a dispatched GROUND/REFRAME move gets its fuel sourced + fused.
	raw = e.concretize(raw, ctx)

	decision := e.reason(ctx, raw, bias, tick, true, baseline, excitation)

	// Initiative: a developed, above-baseline endogenous line may be worth telling the user — the
	// engine reaches out unprompted (cooldown-gated so the awake stream stays durable).
	e.maybeReachOut(tick)

	// O-5 async inbox push channel: re-surface an UNACKNOWLEDGED prior outreach with escalating urgency
	// (durability-bounded; cleared on a user response). Default OFF ⇒ no pending item ⇒ no-op (byte-identical).
	e.maybeEscalateInbox(tick)

	// Arousal homeostasis (the cost of being awake). A mind producing thoughts is *engaged*, not
	// idle — generation/drives/specialists/branching all reset the lull. Only a sustained lull of
	// nothing-happening drowses then sleeps. A salient percept wakes it instantly.
	engaged := baseline > 0 || len(raw) > 0 || decision == types.ACT || decision == types.BRANCH ||
		decision == types.MERGE || decision == types.BACKTRACK || decision == types.DELIVER
	if engaged {
		e.lull = 0
	} else {
		e.lull++
	}
	prev := e.arousal
	switch {
	case e.percSalient() || e.lull == 0:
		e.arousal = types.AWAKE
	case e.lull >= 12:
		e.arousal = types.ASLEEP
	case e.lull >= 7:
		e.arousal = types.DROWSY
	default:
		e.arousal = types.AWAKE
	}
	if e.arousal != prev {
		e.bus.Emit(events.Arousal, prev.String()+" -> "+e.arousal.String()+" (lull="+itoa(e.lull)+")",
			events.D{"to": e.arousal.String()})
	}
	return StepResult{Tick: tick, State: e.lifecycle.State.String(), Decision: decision.String(),
		Meta: metaToMap(e.controller.LastMeta)}
}

// outreachStopword filters function words out of the user-topic overlap: a shared "what"/"would" is
// not topical relevance. Small and conservative — content nouns/verbs still match.
var outreachStopword = map[string]bool{
	"what": true, "that": true, "this": true, "with": true, "would": true, "could": true,
	"should": true, "about": true, "tell": true, "know": true, "think": true, "thing": true,
	"have": true, "from": true, "your": true, "where": true, "when": true, "will": true,
	"there": true, "here": true, "just": true, "like": true, "want": true, "need": true,
}

// maybeReachOut is the proactive-outreach gate — SOCIALLY AWARE. It distinguishes THINKING (private —
// lives in the MIND, never broadcast) from COMMUNICATING (a deliberate act aimed at the user): it only
// reaches out unprompted when a developed thought is RELEVANT TO THE USER'S CONVERSATION. Cooldown-gated,
// durable. Mirrors Python Engine._maybe_reach_out.
func (e *Engine) maybeReachOut(tick int) {
	if !(e.cfg.Proactive && e.mode == "continuous") {
		return
	}
	// CONFIG (awake-regime-off): proactive outreach IS the endogenous drive reaching the user
	// unprompted. When conscious.endogenous_drive is OFF the engine never initiates — durable self-
	// direction with no user turns is ablated (it still replies to user goals on the reactive STOP path).
	if !e.endogenousEnabled() {
		e.gates.endogenous.Skip("proactive outreach suppressed")
		return
	}
	if e.arousal != types.AWAKE || e.UserWaiting() {
		return // an unanswered user line takes priority (that path is a reactive answer)
	}
	if tick-e.lastOutreach < e.cfg.OutreachCooldown {
		return // rate-limit: durable, not spammy
	}
	g := e.graph
	// Social awareness: only worth saying if it connects to what the USER has been talking about.
	// No user topics yet ⇒ nothing to be relevant TO ⇒ stay quiet (think privately, in the rail).
	userTopics := map[string]struct{}{}
	for _, turn := range e.transcript {
		if turn.Role != "user" {
			continue
		}
		for _, w := range strings.Fields(strings.ToLower(turn.Text)) {
			// CONTENT words only: function words ("what", "would", "about"…) overlap everything, which
			// let outreach call any musing "relevant to the user" (the UAT spam admission path).
			if len([]rune(w)) > 3 && !outreachStopword[w] {
				userTopics[w] = struct{}{}
			}
		}
	}
	if len(userTopics) == 0 {
		return
	}
	var real []types.Thought
	for _, t := range g.ActiveContext() {
		if t.Source != types.METACOG {
			real = append(real, t)
		}
	}
	if len(real) < 3 || g.Active().Value < e.cfg.ProactivityFloor {
		return // only a developed line that cleared the share-worthiness floor
	}
	// Share only the engine's OWN endogenous thoughts — curiosity/conclusions (GENERATED), a grounded
	// result (OBSERVATION), or its mind-wandering (default-mode) — never a reactive specialist
	// injection or a raw sub-agent step.
	goalTxt := strings.TrimSpace(g.Goal)
	userSaid := map[string]struct{}{}
	for _, turn := range e.transcript {
		if turn.Role == "user" {
			userSaid[strings.TrimSpace(turn.Text)] = struct{}{}
		}
	}

	ownThought := func(t types.Thought) bool {
		txt := strings.TrimSpace(t.Text)
		if !isSubstantive(txt) || strings.HasSuffix(txt, "?") || txt == goalTxt {
			return false
		}
		if _, said := userSaid[txt]; said {
			return false
		}
		if strings.HasPrefix(txt, types.RecapPrefix) {
			return false
		}
		// SOCIAL: must be relevant to the user, not private self-talk.
		relevant := false
		for _, w := range strings.Fields(strings.ToLower(txt)) {
			if len([]rune(w)) > 3 {
				if _, ok := userTopics[w]; ok {
					relevant = true
					break
				}
			}
		}
		if !relevant {
			return false
		}
		if t.Source == types.GENERATED || t.Source == types.OBSERVATION {
			return true
		}
		return t.Source == types.INJECTED && rawDomain(t.RawReturn) == "default-mode"
	}

	var worth []types.Thought
	for _, t := range real {
		if ownThought(t) {
			worth = append(worth, t)
		}
	}
	if len(worth) == 0 {
		return
	}
	// insight = max by (confidence, id) — the most-confident, breaking ties on the highest id.
	insight := worth[0]
	for _, t := range worth[1:] {
		if t.Confidence > insight.Confidence || (t.Confidence == insight.Confidence && t.ID > insight.ID) {
			insight = t
		}
	}
	key := g.StateKey(g.ActiveBranch)
	if _, shared := e.sharedKeys[key]; shared {
		return // never re-share the same conclusion (per-line)
	}
	msg := types.StripVoice(strings.TrimSpace(insight.Text))
	// Dedup by the MESSAGE TEXT too: the branch key changes with every fresh line, so a recurring
	// endogenous thought (the same curiosity surfacing on different lines) re-shared forever — the
	// 7-identical-outreaches UAT bug (2026-06-12). Once said, never say it again verbatim.
	txtKey := "txt:" + strings.ToLower(msg)
	if _, said := e.sharedKeys[txtKey]; said {
		return
	}
	e.sharedKeys[key] = struct{}{}
	e.sharedKeys[txtKey] = struct{}{}
	e.lastOutreach = tick
	e.lastResponse = msg
	e.transcript = append(e.transcript, transcriptTurn{Role: "assistant", Text: msg}) // outreach is conversation memory
	// The summary keeps the "(unprompted)" tag for headless traces; the clean text rides Data so a
	// conversational surface renders the marker as CHROME, never baked into the mind's words.
	e.bus.Emit(events.Respond, "(unprompted) "+msg,
		events.D{"kind": "outreach", "proactive": true, "text": msg, "value": round2(g.Active().Value)})

	// O-5 async inbox push channel: when inbox_escalation is ON, this fresh outreach becomes the PENDING
	// inbox item — an unacknowledged push the engine will re-surface with escalating urgency until the
	// user responds (maybeEscalateInbox). OFF ⇒ no pending tracking (the base fire-once-then-dedup channel),
	// so the engine is byte-identical. A new outreach REPLACES any older pending item (only the freshest
	// developed line is worth escalating).
	if e.inboxEscalationEnabled() {
		e.pendingInbox = &inboxItem{
			text:       msg,
			value:      g.Active().Value,
			firstTick:  tick,
			lastTick:   tick,
			escalation: 0,
		}
	}
}

// maybeEscalateInbox is the async inbox push channel's REPETITION-ESCALATION half (O-5 — LATHE's
// inbox.jsonl notify-with-escalation, dogfooded inward over proactive outreach). The base channel
// (maybeReachOut) is fire-once-then-dedup-forever: a developed line the user IGNORES is never raised
// again. This re-surfaces a PENDING unacknowledged outreach with escalating urgency — but
// DURABILITY-BOUNDED so it never spams:
//   - the user RESPONDING (a new user turn ⇒ UserWaiting) ACKNOWLEDGES the item ⇒ clear it, no escalation;
//   - re-pushes are capped at InboxMaxEscalations, after which the item is dropped silently;
//   - each re-push is gated by a STRICTLY-LONGER cooldown than first contact (1.5x OutreachCooldown), so
//     re-surfacing is slower than the base channel — the bounded re-push rate keeps the awake utterance
//     count finite (the durability bound; the LATHE 7-identical-outreaches UAT bug is impossible).
//
// Runs ONLY in the awake loop when conscious.activity.inbox_escalation is ON (OFF ⇒ pendingInbox is
// always nil ⇒ this is a no-op ⇒ byte-identical). It re-pushes the SAME insight (no new content authored)
// with a louder urgency marker and emits conscious.inbox_escalate (NOT a fresh action.respond — the
// re-surface is the harness telling the user "still pending", not a new conversational turn).
func (e *Engine) maybeEscalateInbox(tick int) {
	if !e.inboxEscalationEnabled() || e.mode != "continuous" {
		return
	}
	item := e.pendingInbox
	if item == nil {
		return // nothing outstanding
	}
	// ACKNOWLEDGEMENT: a user turn arriving (waiting on a fresh line) clears the pending push — the
	// engine reached the user, the channel did its job, do NOT escalate over a live conversation.
	if e.UserWaiting() {
		e.pendingInbox = nil
		return
	}
	if e.arousal != types.AWAKE {
		return // a drowsing/asleep mind does not pester
	}
	// DURABILITY BOUND: stop after InboxMaxEscalations re-pushes — drop the item silently, never spam.
	maxEsc := e.cfg.InboxMaxEscalations
	if maxEsc < 0 {
		maxEsc = 0
	}
	if item.escalation >= maxEsc {
		e.pendingInbox = nil // exhausted the budget — let the line go (durable, not nagging)
		return
	}
	// COOLDOWN: re-surface strictly slower than first contact (1.5x the base outreach cooldown), anchored
	// on the last push. A still-fresh push waits.
	escCooldown := e.cfg.OutreachCooldown + e.cfg.OutreachCooldown/2
	if escCooldown < 1 {
		escCooldown = 1
	}
	if tick-item.lastTick < escCooldown {
		return
	}
	// Re-surface: same insight, louder urgency marker (the escalation count IS the urgency).
	item.escalation++
	item.lastTick = tick
	e.bus.Emit(events.InboxEscalate, "(still pending x"+itoa(item.escalation)+") "+item.text,
		events.D{
			"text":       item.text,
			"escalation": item.escalation,
			"max":        maxEsc,
			"value":      round2(item.value),
			"first_tick": item.firstTick,
			"since":      tick - item.firstTick,
		})
}

// -- port helpers (continuous mode runs on a PerceptionPort) -------------

// percStream returns the gain-scaled percept batch when the live port is a PerceptionPort (it always
// is while awake; reactive never reaches the continuous loop). Mirrors Python's `self.port.stream(gain)`.
func (e *Engine) percStream(gain float64) []interaction.QueuedMessage {
	if pp, ok := e.port.(*interaction.PerceptionPort); ok {
		return pp.Stream(gain)
	}
	return nil
}

// percSalient reports whether any queued percept is salient (Python `self.port.salient()`). Falls back
// to false when the port is not a PerceptionPort (unreachable in the continuous loop).
func (e *Engine) percSalient() bool {
	if pp, ok := e.port.(*interaction.PerceptionPort); ok {
		return pp.Salient()
	}
	return false
}

// rawDomain extracts the domain off a thought's raw return (the default-mode tag check). Mirrors
// Python's `getattr(t.raw_return, "domain", None) == "default-mode"`. The relay stores the winning
// *Candidate on RawReturn (the closed union), so the domain is read off that.
func rawDomain(raw any) string {
	switch r := raw.(type) {
	case *types.Candidate:
		if r.Domain != nil {
			return *r.Domain
		}
	case types.Candidate:
		if r.Domain != nil {
			return *r.Domain
		}
	}
	return ""
}
