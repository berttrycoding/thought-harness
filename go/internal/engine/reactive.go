package engine

import (
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/critic"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/lifecycle"
	"github.com/berttrycoding/thought-harness/internal/regulator"
	"github.com/berttrycoding/thought-harness/internal/seams"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// -- episode management --------------------------------------------------

// conversationContext builds a compact preamble of the recent conversation so a turn's thinking can
// resolve references ("that city", "the second option"). Returns "" when there is no history. Mirrors
// Python Engine._conversation_context (n_turns default 3; the watched-seam record, persisted).
func (e *Engine) conversationContext(nTurns int) string {
	n := nTurns * 2
	recent := e.transcript
	if len(recent) > n {
		recent = recent[len(recent)-n:]
	}
	if len(recent) == 0 {
		return ""
	}
	parts := make([]string, 0, len(recent))
	for _, turn := range recent {
		head := "I answered"
		if turn.Role == "user" {
			head = "You asked"
		}
		parts = append(parts, head+": "+runeSlice(strings.TrimSpace(turn.Text), 120))
	}
	return types.RecapPrefix + " — " + strings.Join(parts, " | ")
}

// startEpisode opens a fresh thinking session for a goal: a new graph + Thought MCP, a stamped
// process id, conversation-memory seeding, the goal/action match (skill | synthesised program |
// none), and the workflow wiring. fromUser is structural provenance — TRUE when the goal arrived
// from outside through the port (a user turn), FALSE when the engine seeded itself (the awake
// wander). The graph records it on the root thought (A4): the user's words are USER_INPUT, never
// relabeled as the mind's own generation — that record is what "a user is waiting" derives from.
func (e *Engine) startEpisode(goal string, fromUser bool) {
	e.graph = graph.New(goal)
	if fromUser {
		e.graph.StampGoalSource(types.USER_INPUT)
	}
	e.mcp = graph.NewThoughtMCP(e.graph, e.backend, e.bus.Emit)
	// A new cognitive process: stamp it so every entity born this episode is addressable.
	e.processSeq++
	e.processID = "ep:" + itoa(e.processSeq)
	e.bus.Emit(events.Episode, "process "+e.processID+": "+runeSlice(goal, 60),
		events.D{"process": e.processID, "goal": goal})
	e.cognitionGraph.BindGraph(e.graph) // Conscious projection lives on the unified model too

	// Seed the fresh thinking with conversation memory (each turn is bounded fresh thinking).
	if ctxline := e.conversationContext(3); ctxline != "" {
		e.graph.Append(&types.Thought{ID: -1, Text: ctxline, Source: types.GENERATED, Confidence: 0.3},
			e.bus.Tick)
	}
	e.stampEpisodeStart() // WF-G: wall-clock start when a Clock is wired (time-blind otherwise)
	// read_clock (cognitive power-cycle, Track 1.5, percept.go): sense the wall clock ONCE at episode-
	// open across the logged percept seam (record live, or replay a logged value) so the mind orients to
	// "what time is it now". A no-op (no read, no log, no event) unless sense.clock is ON and a Clock is
	// wired — default OFF ⇒ byte-identical, time-blind. The sensed value is reused by the orientation pass
	// below, so the clock is sensed at most ONCE per episode-open (no double-record / double-event).
	clockVal, clockOK := e.senseClock(e.bus.Tick)
	// fetch_web (cognitive power-cycle, follow-up #15 — the OUTWARD distal sense, web_sense.go): reset the
	// per-episode budget and sense the world ONCE at episode-open across the logged web seam (record live,
	// or replay a logged snippet) so the mind orients to "what is happening in the world right now". BUDGETED
	// (resolved Fork 2): at most one fetch per episode-open, never per tick. A no-op (no fetch, no log, no
	// event) unless sense.web is ON and a Web is wired — sense.web DEFAULTS OFF (web touches the network +
	// costs), so default ⇒ byte-identical, web-blind. The sensed snippet is folded into the orientation pass.
	e.webSensedEpisode = false
	// Re-arm the introspection-suite once-per-quiescence guard (introspect.suite, Track H §8): each fresh
	// episode produces one self-report suite at ITS OWN quiescence. Only ever read/written on the
	// introspect.suite-ON path, so the bare loop never touches it ⇒ byte-identical when the knob is off.
	e.introspectedEp = false
	webRes, webOK := e.senseWeb(e.bus.Tick)
	// ORIENTATION PASS (cognitive power-cycle, Track 3, orient.go): on the FIRST wake of a RESUMED session,
	// re-ground BOTH layers — inject one re-grounding GENERATED thought (prior focus + current time +
	// self-state) AND write the sensed date as a grounded belief (the perception->memory handshake). Fires
	// once per engine; a no-op unless sense.orient is ON AND this is a resume boot (a rehydrated prior spine
	// OR clock-sensing on) — default OFF ⇒ no orientation thought, no belief, no event ⇒ byte-identical.
	e.orientOnce(e.bus.Tick, clockVal, clockOK, webRes, webOK)
	e.branchVisits = map[int]int{} // T1.3: reset UCB visit counts each episode (no leak across episodes)
	e.actedBranches = map[int]struct{}{}
	e.forked = map[int]struct{}{}
	e.verifyBranched = map[int]struct{}{}
	e.resourcedBranches = map[int]struct{}{}      // A-RAG4: reset the once-per-branch re-source bound each episode
	e.verifiedAnswerBranches = map[int]struct{}{} // T2.1: reset the once-per-branch answer-verify bound each episode
	e.episodeGroundBase = e.grounding.Len()       // baseline: did THIS episode ground anything? (memory.record gate)
	e.episodeActsIssued = 0                       // FIX 2: ACTs that imported reality this episode (force-a-read-before-give-up)
	e.flywheelOpenEpisode()                       // OFFLINE-RL flywheel: a fresh trajectory (no-op when flywheel.capture is OFF)
	e.selfReportedEpisode = false                 // Track H §8: a fresh episode gets one self-report at its quiescence (introspect_faithfulness.go)
	e.recallMemory(goal)                          // declarative memory: recall related past episodes (P2.3)

	// The goal/action system: a first-class Goal matched to a library Skill if one fits, else a
	// freshly-synthesised workflow PROGRAM, else nil for simple Q&A. An awake self-prompt is a DRIVE
	// — classified by PROVENANCE (who seeded the episode), never by sniffing the goal text.
	src := cognition.GoalDrive
	if fromUser {
		src = cognition.GoalUser
	}
	goalObj := cognition.Goal{ID: e.processID, Text: goal, Source: src, Status: "open", Acceptance: cognition.AuthorAcceptance(goal, src)}
	e.goals = append(e.goals, goalObj)
	last := &e.goals[len(e.goals)-1]
	e.bus.Emit(events.Goal, "goal ["+last.Source.String()+"]: "+last.ShortDefault(),
		events.D{"id": last.ID, "text": goal, "source": last.Source.String()})

	e.activePath = "" // reset: this episode is not on a path unless a path skill matches below (M5)
	// slice b (§3.3): when subconscious.capability is on, a Capability PRODUCES the workflow (reuse-seed-
	// or-synthesise) and captures a Context (replacing the raw thought slice) — byte-identical workflow
	// shape, only WHO produces it changes. OFF (default) ⇒ the engine's inline Synthesize path runs.
	var (
		res        *cognition.SynthResult
		ok         bool
		wf         *subconscious.Workflow
		episScope  *subconscious.Scope
		episodeCap *subconscious.Capability // kept for the staffing-time re-capture closure (gap-2 fix)
	)
	e.episodeCap = nil // reset: only the capability-ON path sets the producing entry (gap 5-deeper)
	// W5 cost gate: reset the synth-cost tap so it captures ONLY this episode's synthesize_program decode
	// (the cost a minted skill would later avoid). nil ⇒ the cost gate is OFF ⇒ no tap ⇒ no-op.
	if e.synthCostTap != nil {
		e.synthCostTap.Store(0)
	}
	if e.features != nil && e.features.Subconscious.Capability {
		capa := subconscious.NewCapability("episode", capabilityTriggers(goal), e.catalog, e.backend)
		capa.Library, capa.Emit = e.skills, e.bus.Emit
		capa.WebLookup = e.webLookup() // subconscious.web_search: staff expose-affordances for a lookup question (off ⇒ byte-identical)
		episodeCap = capa
		e.episodeCap = capa // retain the producing entry for the dispatch-recognition wire (gap 5-deeper)
		// gap 4: SOURCE the §3.3a authority CEILING at the Capability (eager) — the least-privilege band
		// this run's tool picks resolve within. This is NewScope/WithScope's first live caller. The
		// ceiling is threaded into staffing below so a worker can never widen it (ScopedToolScope).
		episScope = e.episodeScope(goal)
		capa.WithScope(episScope)
		e.episodeContext = capa.CaptureContext(e.graph, goal) // gap 2: episode-OPEN capture (the fallback)
		wf, res, ok = capa.Produce(goal, e.workingContext())
		// gap 6 observability: the capability run's authority ceiling, emitted on the bus (the redesign's
		// "this run's authority" audit line — the Scope is invisible without it).
		e.emitCapabilityScope(episScope)
	} else {
		// LEGACY(redesign): the subconscious.capability OFF-branch — the engine's inline Synthesize path
		// (no producing Capability, no Scope ceiling, no rich staffing Context) — removable when the 4
		// redesign flags are retired (the Capability.Produce path above is then unconditional).
		//
		// WEB-SEARCH (subconscious.web_search): the web-aware variant so a factual lookup question that the
		// model declined a program for still gets a research->answer shape that staffs expose-affordances
		// (the under-staffing fix). webLookup() is false unless the flag is on ⇒ byte-identical default.
		res, ok = cognition.SynthesizeWeb(goal, e.workingContext(), e.catalog, e.backend, e.bus.Emit, e.skills, e.webLookup())
	}
	if ok && res != nil {
		if strings.HasPrefix(res.Source, "skill:") {
			last.Transition(cognition.GoalMatched) // open -> matched (the lifecycle SM, §1.7); the Goal no
			s := res.Source[len("skill:"):]        // longer records the matched skill name (one-way mirror, §1.6)
			// M5: if the matched skill is one of the three canonical PATHS (analogy/induction/deduction),
			// remember it so episode-end (stop) records the directed traversal with its grounded outcome.
			if cognition.IsPath(s) {
				e.activePath = s
			}
		} else {
			last.Status = "active" // a fresh program was synthesised (no library skill matched)
		}
		e.convert.NoteProgram(goal, res.Program) // trace->skill: track recurring programs
		// W5 cost gate: attribute this episode's synthesize_program decode to the shape (the re-synthesis
		// cost a minted skill would avoid). A library-skill RECALL spends no synthesize_program tokens, so
		// the tap reads ~0 and nothing meaningful is attributed — exactly right: a recalled shape pays no
		// further synthesis cost. nil tap ⇒ cost gate OFF ⇒ no-op.
		if e.synthCostTap != nil {
			e.convert.NoteSynthesisCost(goal, int(e.synthCostTap.Load()))
		}
		if wf == nil {
			// inline path (capability off): wrap the synthesised program — identical to the Capability's
			// FromProgram (which already wrapped it when capability is on).
			wf = subconscious.FromProgram(&res.Program, e.catalog, e.backend, e.bus.Emit, goal)
		}
		// gaps 2/3/4: when capability is on, STAFF the workflow with the run's authority ceiling (Scope)
		// and the captured rich Context, so every SubAgent Instantiate spawns reads the whole active branch
		// (not the ≤5 slice) and resolves its tool picks within the ceiling (ScopedToolScope). With
		// capability OFF, episScope+episodeCap are nil ⇒ no staffing set ⇒ byte-identical to before.
		//
		// gap-2 fix: the third arg is a STAFFING-TIME re-capture closure — Instantiate calls it so each
		// worker sees the branch AS IT IS WHEN STAFFED (which has grown past the goal root the episode-open
		// snapshot froze), the intended enrichment. It re-captures against the LIVE e.graph via the SAME
		// Capability (same backend + knowledge refs), so it is deterministic (graph-derived, no clock/RNG).
		// The episode-open e.episodeContext stays the fallback (used when the live capture is unavailable).
		if episScope != nil || e.episodeContext != nil {
			wf.WithStaffing(episScope, e.episodeContext, e.staffingRecapture(episodeCap, goal), e.episodeToolPicker())
		}
		// T1.1 (subconscious.query_formulation): stamp the query-formulation gate so a staffed web_search worker
		// formulates the query from the actual question. OFF (the default) ⇒ a no-op stamp ⇒ byte-identical.
		wf.WithQueryFormulation(e.queryFormulation())
		e.subconscious.SetWorkflow(wf)
		// gap 5-deeper: when subconscious.capability_dispatch is on (AND a producing Capability exists), the
		// dispatch loop routes its per-tick recognition THROUGH the Capability — it becomes the live relevance
		// entry that owns "does this workflow apply?", subsuming the Workflow self-trigger, deciding PERMISSIVELY
		// with the has-any `gradedRelevance>0` criterion (theta is NOT consulted — θ-gating recognition is the
		// refuted double-gate; the bespoke episode workflow short-circuits, so the episode is unchanged); nil
		// recognizer otherwise ⇒ the Workflow self-triggers (binary), unchanged.
		e.wireDispatchEntry(episodeCap)
		// gap 5-deeper PART 2: when subconscious.capability_specialists is on (AND a producing Capability
		// exists), the dispatch loop routes each base specialist's admission THROUGH the Capability — it owns
		// "does this specialist fire?" (the §3.3a Scope domain band), subsuming the bare relevance firing. The
		// episode Scope is general (empty domain) ⇒ every domain admitted ⇒ byte-identical; nil gate otherwise
		// ⇒ bare eff>theta admission, unchanged.
		e.wirePrimitiveSubAgentGate(episodeCap)
		e.openSessionTree(goal, res.Program) // the runtime: a bounded Session spawn tree (P3.3), observable
	} else {
		e.subconscious.SetWorkflow(nil)
		e.wireDispatchEntry(nil)         // no workflow ⇒ no entry to route (clear any prior episode's recognizer)
		e.wirePrimitiveSubAgentGate(nil) // no entry ⇒ clear any prior episode's specialist gate (byte-identical)
		e.sessionRoot = nil
	}
	rootSrc := e.graph.History()[0].Source.String() // the TRUE root provenance (A4), never hardcoded
	e.bus.Emit(events.Append, "#1 ["+rootSrc+"] "+goal, events.D{
		"id": 1, "source": rootSrc, "text": goal, "branch": 0, "confidence": 0.0,
	})
}

// capabilityTriggers derives the relevance triggers for the episode Capability (slice b) from the goal's
// significant words (>2 chars). These are cosmetic in the episode-production path (the Capability produces
// for the episode goal directly, so Relevance is not the gate) but give the produced-workflow event a
// meaningful trigger set and match the role the entry plays once it is dispatched by relevance.
func capabilityTriggers(goal string) []string {
	out := []string{}
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		if len(w) > 2 {
			out = append(out, w)
		}
	}
	if len(out) == 0 && goal != "" {
		out = append(out, strings.ToLower(goal))
	}
	return out
}

// episodeScope sources this episode's §3.3a authority CEILING (gap 4) — the least-privilege band the
// run's tool picks resolve within. It is the Capability's eager ceiling: a REAL least-privilege set, not
// "allow all". The categories are the tool-OPERATION categories (the §3.10a vocabulary toolCategory uses)
// this episode is permitted to reach:
//
//   - inspect + execute are ALWAYS admitted — a read and an in-sandbox run-to-measure are the sensing
//     band, the safe grounding probes every thinking episode may use (03 §4/§6);
//   - mutate (a WORLD-CHANGE write) is admitted ONLY when the goal LANGUAGE asks for a build/write/edit —
//     the rest of the time a worker cannot reach a write tool even if an operator listed one (the
//     least-privilege bite the redesign specifies: "a worker may never widen its ceiling").
//
// Domain stays "" (general — an episode is not domain-banded) and skillTier 0 (uncapped — the tier
// follows Goal.Level, §3.8a, which is a later slice). Deterministic (goal text only, no clock/RNG), so
// two runs of the same goal source the same ceiling.
func (e *Engine) episodeScope(goal string) *subconscious.Scope {
	cats := []string{"inspect", "execute"} // the sensing band — always least-privilege-safe
	if goalAsksForWrite(goal) {
		cats = append(cats, "mutate") // the goal authors a world-change ⇒ admit the write category
	}
	return subconscious.NewScope("", cats, 0)
}

// goalAsksForWrite reports whether a goal's LANGUAGE authorises the mutate (write) category in the run's
// ceiling — a deterministic keyword check over the lower-cased goal. A goal that only reads/analyses/
// compares never admits a write, so its workers stay read-only (least privilege); a goal that builds /
// writes / edits / implements / creates / fixes admits mutate. No model call — a Pattern-A floor.
func goalAsksForWrite(goal string) bool {
	g := strings.ToLower(goal)
	for _, kw := range []string{"write", "edit", "build", "implement", "create", "fix", "refactor",
		"modify", "patch", "add ", "update", "generate a file", "save"} {
		if strings.Contains(g, kw) {
			return true
		}
	}
	return false
}

// emitCapabilityScope makes the §3.3a Scope ceiling observable (gap 4) — "this run's authority" as an
// auditable bus line (the redesign's audit requirement; the Scope is invisible without it). A nil scope
// (capability off) is silent. Only fires on the capability-ON path, so default-OFF goldens are unaffected.
func (e *Engine) emitCapabilityScope(sc *subconscious.Scope) {
	if sc == nil || e.bus == nil {
		return
	}
	e.bus.Emit(events.SubScope, "capability scope: domain="+orAny(sc.Domain())+" cats="+itoa(len(sc.Categories())),
		events.D{
			"domain":     sc.Domain(),
			"categories": sc.Categories(),
			"skill_tier": sc.SkillTier(),
		})
}

// wireDispatchEntry installs (or clears) the GAP 5-DEEPER relevance/dispatch ENTRY on the subconscious
// engine: when subconscious.capability_dispatch is ON AND a producing Capability exists (capa != nil),
// the dispatch loop routes its per-tick workflow-recognition THROUGH that Capability (it becomes the live
// relevance entry that owns "does this workflow apply?", subsuming the Workflow self-trigger, deciding
// PERMISSIVELY with the has-any `gradedRelevance>0` criterion — theta is NOT consulted, since θ-gating
// recognition is the refuted double-gate) and the wire is made observable via subconscious.entry. OFF (the default) OR no producing Capability ⇒ a nil recognizer is installed, so the
// Workflow self-triggers (Workflow.Recognize, the binary path) exactly as today — the recognition verdict,
// the GateBias read, and the trace are byte-identical on the OFF path. Called every episode-open alongside
// SetWorkflow so a prior episode's recognizer never leaks into one that produced no Capability.
func (e *Engine) wireDispatchEntry(capa *subconscious.Capability) {
	if capa == nil || e.features == nil || !e.features.Subconscious.CapabilityDispatch {
		// LEGACY(redesign): the subconscious.capability_dispatch OFF-branch — install a nil recognizer so the
		// Workflow self-triggers (the binary Recognize path) instead of routing through the Capability entry —
		// removable when the 4 redesign flags are retired (SetRecognizer(capa) below is then unconditional).
		e.subconscious.SetRecognizer(nil) // OFF / no entry ⇒ the Workflow self-triggers (byte-identical)
		return
	}
	e.subconscious.SetRecognizer(capa) // the Capability owns the relevance entry this episode
	e.emitDispatchEntry(capa)
}

// emitDispatchEntry makes the relevance/dispatch entry wire observable (the §3.3 subsumption is invisible
// otherwise — the recognition verdict is byte-identical, so only this event distinguishes the Capability-
// entry path from the self-trigger path). Emitted only on the capability_dispatch-ON path; nil bus/wf ⇒
// silent.
func (e *Engine) emitDispatchEntry(capa *subconscious.Capability) {
	if capa == nil || e.bus == nil {
		return
	}
	wf := e.subconscious.Workflow()
	wfName := ""
	if wf != nil {
		wfName = wf.Name
	}
	e.bus.Emit(events.SubEntry, "capability '"+capa.Name+"' is the dispatch entry for workflow: "+wfName,
		events.D{
			"capability": capa.Name,
			"workflow":   wfName,
			"triggers":   capa.Triggers,
		})
}

// wirePrimitiveSubAgentGate installs (or clears) the GAP 5-DEEPER PART 2 SPECIALIST-firing ENTRY on the
// subconscious engine: when subconscious.capability_specialists is ON AND a producing Capability exists
// (capa != nil), the dispatch loop routes each base specialist's admission THROUGH that Capability (it
// becomes the live SPECIALIST-firing entry that owns "does this specialist fire?", deciding with the §3.3a
// Scope domain band — AdmitSpecialist), and the wire is made observable via subconscious.spec_gate. OFF
// (the default) OR no producing Capability ⇒ a nil gate is installed, so admission is the bare eff>theta
// (byte-identical). Called every episode-open alongside wireDispatchEntry so a prior episode's gate never
// leaks into one that produced no Capability.
func (e *Engine) wirePrimitiveSubAgentGate(capa *subconscious.Capability) {
	if capa == nil || e.features == nil || !e.features.Subconscious.CapabilityPrimitiveSubAgents {
		// LEGACY(redesign): the subconscious.capability_specialists OFF-branch — install a nil gate so
		// admission is the bare eff>theta (the legacy relevance firing) instead of routing through the
		// Capability — removable when the redesign flags are retired (SetPrimitiveSubAgentGate(capa) is then
		// unconditional).
		e.subconscious.SetPrimitiveSubAgentGate(nil) // OFF / no entry ⇒ bare eff>theta admission (byte-identical)
		return
	}
	e.subconscious.SetPrimitiveSubAgentGate(capa) // the Capability owns specialist firing this episode
	e.emitPrimitiveSubAgentGate(capa)
}

// emitPrimitiveSubAgentGate makes the specialist-firing entry wire observable (the §3.3 PART-2 subsumption is
// invisible otherwise on the general-Scope episode path — the admission set is byte-identical there, so
// only this event distinguishes the Capability-gated path from the bare relevance firing). It reports the
// run's §3.3a Scope DOMAIN band the gate enforces, so a trace shows WHICH authority is now gating the
// specialist roster. Emitted only on the capability_specialists-ON path; nil bus/capa ⇒ silent.
func (e *Engine) emitPrimitiveSubAgentGate(capa *subconscious.Capability) {
	if capa == nil || e.bus == nil {
		return
	}
	domain := ""
	if capa.Scope != nil {
		domain = capa.Scope.Domain()
	}
	e.bus.Emit(events.SubSpecGate,
		"capability '"+capa.Name+"' owns specialist firing (domain band="+orAny(domain)+")",
		events.D{
			"capability": capa.Name,
			"domain":     domain,
		})
}

// staffingRecapture builds the STAFFING-TIME Context re-capture closure (gap-2 fix part 1) the workflow
// calls inside Instantiate — so each staffed worker sees the active branch AS IT IS WHEN STAFFED, not the
// goal-root snapshot the episode-OPEN CaptureContext (reactive.go) froze when the branch was just the goal.
//
// THE BUG IT FIXES. The only capture site ran at episode-open, when the active branch was the goal root (1
// thought); that frozen 1-thought snapshot was then preferred over the live ≤5 tail (subagent.go), so a
// mid-episode worker was pinned to the goal root and STARVED (live-claude: grounding OFF 2/3 → ON 0/3).
// Re-capturing here against the LIVE e.graph (which has grown as the conscious kept thinking) hands the
// worker the WHOLE grown branch — the intended enrichment.
//
// Deterministic: it re-derives from the time-based thought graph via the SAME Capability (same backend
// gist + declared knowledge refs) — no clock, no RNG. A nil capability/graph yields a nil closure / a nil
// capture, in which case the workflow falls back to the episode-open context (so this never starves either).
func (e *Engine) staffingRecapture(capa *subconscious.Capability, goal string) func() *subconscious.Context {
	if capa == nil {
		return nil
	}
	return func() *subconscious.Context {
		if e.graph == nil {
			return nil // no live graph this staffing ⇒ the workflow uses the episode-open fallback
		}
		// Re-capture against the LIVE active branch (grown past the goal root). BranchThoughts returns value
		// copies, so the snapshot does not alias the live graph (later thinking never mutates it).
		return capa.CaptureContext(e.graph, goal)
	}
}

// episodeToolPicker builds the §3.6/§3.7 CATEGORY-SOURCED tool-pick resolver (gap-5 load-bearing half) the
// workflow staffs every worker with — when the subconscious.capability flag is ON. The resolver sources each
// worker's concrete tool set by CATEGORY from the LIVE action registry + the LIVE worker-faculty footprint
// (subconscious.NewToolPicker), so the operator's flat OperatorSpec.ToolScope stops being the tool SOURCE
// (the gap-9 prerequisite). The resolved set is byte-identical to the flat ToolScope for the seed ops
// (toolpick.go parity), so flipping the flag preserves behaviour.
//
// Returns nil — so the workflow keeps the flat ToolScope (byte-identical) — UNLESS the capability flag is on
// AND a real executor (a workspace) is wired: nil features / capability OFF ⇒ nil (the default-OFF posture);
// no executor (the offline/no-workspace path) ⇒ nil (no live registry to source from). Deterministic: it
// reads the sorted registry + the deterministic faculty roster, no clock/RNG.
func (e *Engine) episodeToolPicker() *subconscious.ToolPicker {
	if e.features == nil || !e.features.Subconscious.Capability {
		// LEGACY(redesign): the subconscious.capability OFF-branch — return nil so the workflow keeps the flat
		// OperatorSpec.ToolScope as the tool SOURCE instead of the category-sourced picker — removable when the
		// 4 redesign flags are retired (the flat-ToolScope source itself fully disappears at gap-9).
		return nil // capability OFF (or nil features) ⇒ the flat ToolScope source (byte-identical)
	}
	if e.executor == nil {
		return nil // no workspace executor ⇒ no live registry to source the category footprint from
	}
	// WEB-SEARCH (subconscious.web_search): union web_search into its inspect category so a staffed
	// expose-affordances worker picks it on the capability-ON path (the picker otherwise drops the granted
	// tool — no faculty footprints it). webLookup() false unless the flag is on ⇒ byte-identical picker.
	return subconscious.NewToolPicker(e.subconscious.Specialists(), e.executor.Registry(), e.webLookup())
}

// orAny renders an empty domain band as "(any)" for the human-readable summary (the empty domain is the
// unconstrained ceiling, §3.3a).
func orAny(s string) string {
	if s == "" {
		return "(any)"
	}
	return s
}

// valueScalar is the active branch's EPISTEMIC V(s) — content quality without the conversational-
// priority term (A1 refinement). Filter-trust, the scheduler, and the drives consume this: a
// waiting user makes the line urgent (Branch.Value, rerank), not its candidates more credible.
func (e *Engine) valueScalar() float64 {
	if e.graph == nil {
		return 0.0
	}
	return e.graph.Active().Epistemic
}

// formIntention forms the action: the model decides it when available (its own seam-crossing
// decision), else the heuristic regex router. Mirrors Python Engine._form_intention.
func (e *Engine) formIntention() types.Intention {
	if intender, ok := e.backend.(backends.Intender); ok {
		text, kind, ok := intender.Intention(e.graph.Goal, e.workingContext())
		// Python: `if r: return Intention(text=r[1], kind=r[0], branch_id=active)`. A swallowed
		// backend error becomes ok=false here (the Go backend never raises), which falls through.
		if ok {
			ab := e.graph.ActiveBranch
			return types.Intention{Text: text, Kind: kind, BranchID: &ab}
		}
	}
	return e.graph.FormIntention()
}

// branchesLive counts the live branches (ACTIVE or STASHED) — the schedulability load. Mirrors
// Python Engine._branches_live.
func (e *Engine) branchesLive() int {
	if e.graph == nil {
		return 0
	}
	n := 0
	for _, b := range e.graph.Branches {
		if b.Status == types.ACTIVE || b.Status == types.STASHED {
			n++
		}
	}
	return n
}

// appendThought appends a thought to the graph and emits conscious.append (Python Engine._append).
// The confidence is rounded to 2 at the emit site (Python round(t.confidence, 2)).
func (e *Engine) appendThought(t *types.Thought, tick int) *types.Thought {
	t = e.graph.Append(t, tick)
	e.bus.Emit(events.Append, "#"+itoa(t.ID)+" ["+t.Source.String()+"] "+t.ShortDefault(),
		events.D{
			"id": t.ID, "source": t.Source.String(), "text": t.Text,
			"branch": derefIntOr(t.BranchID, 0), "confidence": round2(t.Confidence),
		})
	e.tlThought(tick, e.graph.ActiveBranch, t.ID) // slice (i): record the attention move on the timeline
	return t
}

// generate is the MIDDLE serial effortful loop: the backend writes the next GENERATED thought, the
// Filter screens it, and the confidence is read back from the verdict. Mirrors Python Engine._generate.
func (e *Engine) generate(ctx []types.Thought) *types.Thought {
	// CONFIG (M1): conscious.generate OFF ⇒ the effortful MIDDLE loop produces nothing (no backend
	// generate). The tick becomes a no-op upstream (reason() handles a nil thought honestly). Bypass,
	// not delete — the wire stays, config.skip records it.
	if e.gates.generate.Disabled() {
		e.gates.generate.Skip("effortful generate bypassed")
		return nil
	}
	// Legible-generation SHADOW (WF-E CC-1, OFF by default): when the instrument is ON, ask the conscious
	// to emit the in-band control tag by appending the registry-derived fragment to the Generate prompt;
	// when OFF, clear it so the prompt is byte-identical. Set per-tick from the LIVE toggle (it may flip
	// mid-run). Only the LLM backend implements LegiblePrompter (the test double ignores it), so the
	// heuristic/test path is unaffected either way.
	e.applyLegibleFragment()
	text := e.backend.Generate(e.graph.Goal, ctx, e.rng)
	if text == "" {
		// OUTPUT role: the substrate produced no thought (model unavailable/declined). Do NOT
		// fabricate one from a heuristic template — return nil so `reason` skips this tick rather
		// than append a blank/faked thought. The backend already emitted the llm.fallback cause.
		return nil
	}
	cand := types.Candidate{Text: text, Source: types.GENERATED, Relevance: 0.4}
	// The Filter computes the REAL admission verdict AND (when the instrument is ON) shadow-parses the
	// tag in `text` for a FILTER parity observation — the same *Filter the hidden seam holds, so this one
	// call covers the conscious's own tagged thought. The verdict is unchanged by the shadow.
	//
	// HONEST SCOPE: only the verdict's CONFIDENCE is consumed here (read back into the Thought below) —
	// the admit/REJECT decision is NOT enforced on the conscious's own thought. Unlike the injection
	// stream (seams/hidden.go Relay gates on v.Admit()) and the awake generator (continuous.go gates on
	// v.Admit()), this thought is voiced REGARDLESS of the verdict. So the conscious's self-authored
	// thought is trusted as-is (the same trust a conventional harness gives raw model output); the Filter
	// guards what crosses INTO the stream, not what the stream writes itself. (Doc-only — no logic change.)
	verdict := e.filter.Admit(cand, e.graph.History(), e.valueScalar())
	// Legible-generation SHADOW: STRIP the tag before the thought is voiced into the stream — the tag is
	// internal routing only, never shown (05 §5b). OFF ⇒ text unchanged ⇒ byte-identical. The shadow
	// already read the tag off the un-stripped text inside Admit above.
	voiced := text
	if e.gates.legible.Enabled() {
		voiced = e.legibleShadow.Strip(text)
	}
	e.bus.Emit(events.Generate, "effortful: "+runeSlice(voiced, 54),
		events.D{"text": voiced, "confidence": round2(verdict.Confidence)})
	return &types.Thought{ID: -1, Text: voiced, Source: types.GENERATED, Confidence: verdict.Confidence}
}

// applyLegibleFragment sets (when seam.legible_generation is ON) or clears (when OFF) the registry-
// derived control-tag fragment on the backend's Generate prompt — the WF-E CC-1 legible-generation
// instrument's prompt side (05 §5b/§5c). It is a no-op unless the backend implements
// backends.LegiblePrompter (only the LLM backend does; the test double ignores the Generate prompt), so
// the heuristic/test path is byte-identical. Called once per generate tick so a live toggle flip is
// observed. The fragment is the ONE source of truth the seam's parser is also derived from.
func (e *Engine) applyLegibleFragment() {
	lp, ok := e.backend.(backends.LegiblePrompter)
	if !ok {
		return
	}
	if e.gates.legible.Enabled() && e.legibleReg != nil {
		lp.SetLegibleFragment(e.legibleReg.PromptFragment())
	} else {
		lp.SetLegibleFragment("")
	}
}

// -- the shared intake + decide core ------------------------------------

// reason is the spine — the shared intake + decide core both loops run. In EXACT order: build the
// gate prior, relay through the hidden seam, append, rerank by value, decide_next, execute, advance
// the workflow, then fold the EXACT durability accounting into the regulator. Mirrors Python
// Engine._reason.
//
// excitation defaults to true and baseline to 0 in Python (the reactive call passes neither); the Go
// call sites pass them explicitly.
func (e *Engine) reason(ctx []types.Thought, raw []*types.Candidate, bias map[string]float64,
	tick int, asyncAct bool, baseline int, excitation bool) types.Decision {
	e.mcp.Tick = tick
	// Retracement (slice c, §2b): drain any late injections buffered from prior ticks BEFORE this tick
	// thinks — a passed-decision injection re-opens that node (the Controller fires mcp.Reenter), so the
	// active branch may be repositioned before the relay/decide below. conscious.activity.retracement OFF
	// (default) ⇒ the buffer is never drained ⇒ byte-identical.
	if e.retracementEnabled() {
		e.drainRetracements(tick)
	}
	// Merge the learned gate prior (a compiled control habit from convertibility) into the Gate's
	// bias so a metacog-compiled prior actually privileges its domain at arbitration.
	// CONFIG (M1): seam.gate_priors OFF ⇒ do not merge the compiled prior (the bias stays the
	// workflow's only; the wire stays, the prior is simply not consulted). config.skip records it.
	if e.gates.gatePrior.Disabled() {
		if len(e.convert.GatePrior) > 0 {
			e.gates.gatePrior.Skip("gate prior not merged")
		}
	} else if len(e.convert.GatePrior) > 0 {
		merged := make(map[string]float64, len(bias)+len(e.convert.GatePrior))
		for k, v := range bias {
			merged[k] = v
		}
		for dom, w := range e.convert.GatePrior {
			merged[dom] += w
		}
		bias = merged
	}
	// Intake band-pass (slice c, 04 §2.1): suppress the flash-in-the-pan + the stale restatement before
	// the relay; a persistent-but-displaced candidate is buffered for retracement instead. seam.band_pass
	// OFF (default) returns raw unchanged ⇒ byte-identical.
	raw = e.bandPassIntake(raw, tick)
	// The relay takes the raw candidates BY VALUE (Python passes the list of Candidate objects).
	rawVals := make([]types.Candidate, len(raw))
	for i, c := range raw {
		rawVals[i] = *c
	}
	relay := e.hidden.Relay(rawVals, e.graph.History(), bias, e.valueScalar())
	var thought *types.Thought
	if relay.Thought != nil {
		thought = relay.Thought
	} else {
		thought = e.generate(ctx)
	}
	if thought == nil {
		// No content this tick: the substrate is unavailable and we refuse to fabricate one from a
		// heuristic template (output comes from the model or not at all). Surface it and make this
		// tick a no-op — the bounded loop ends honestly rather than emitting blank/faked thoughts.
		e.bus.Emit(events.LLMFallback, "no conscious thought this tick — thinking substrate unavailable",
			events.D{"role": "conscious.generate", "unavailable": true})
		return types.THINK
	}
	e.appendThought(thought, tick)
	e.groundClaim(thought.Text) // SR-4: compute-ground a voiced computable claim (offline, deterministic)

	// CONFIG (M1): value.signal OFF ⇒ skip the rerank pass (V over branches is not recomputed; the
	// existing Branch.value stays). The wire/panel stays; only the recompute decision is bypassed.
	if e.gates.value.Disabled() {
		e.gates.value.Skip("rerank bypassed")
	} else {
		e.rerank(e.graph) // rerank: recompute V over branches (forest-aware when conscious.activity.forest)
	}

	ab := e.graph.ActiveBranch
	wf := e.subconscious.Workflow()
	workflowPending := wf != nil && wf.Recognized() && !wf.Complete()

	// A-RAG4: V(s)-triggered active re-sourcing. With controller.active_resource ON, ask the Controller
	// (now that V over branches is fresh from the rerank) whether the active node is a LOW-V, goal-relevant
	// line worth RE-SOURCING — and if so, re-invoke the sourcing ladder for it (FLARE/active-inference).
	// Default OFF ⇒ a no-op (no trigger, no ladder walk) ⇒ byte-identical. Runs BEFORE DecideNext so a
	// freshly-sourced grounded fact is in the active context for the very decision it informs.
	e.maybeActiveResource(ab, tick)

	// SLAM M6 (Track F): the active-inference next-best-observation. With slam.infogain ON, rank the
	// tracked beliefs by expected JOINT uncertainty reduction and surface the one most worth grounding next
	// (directed grounding by expected info gain, not just outcome reward). Runs alongside the re-source
	// decision — both answer "what should the harness do about its uncertainty". A pure ranking signal that
	// never alters a belief; default OFF ⇒ no ranking, no estimate.infogain event ⇒ byte-identical.
	e.slamNextBestObservation()

	opts := critic.DefaultDecideOptions()
	opts.Conflict = relay.Conflict
	opts.ActedBranch = setHas(e.actedBranches, ab)
	opts.VerifiedBranch = setHas(e.verifyBranched, ab)
	opts.AlreadyForked = setHas(e.forked, ab)
	opts.WorkflowPending = workflowPending
	opts.BudgetStop = e.mode == "reactive"      // the give-up budget is episodic only
	opts.ActOnExhaustion = e.mode == "reactive" // awake wandering moves on, doesn't act
	if e.mode == "continuous" {
		// The awake user line gets a bounded deliver deadline (the wander stays unbounded): a user
		// turn must be answered even on a substrate that never trips GoalSatisfied for it (a greeting /
		// chitchat). See EngineConfig.AwakeUserBudget and Controller.choose.
		opts.AwakeUserBudget = e.cfg.AwakeUserBudget
	}
	decision := e.controller.DecideNext(e.graph, opts)
	// WF-G deadline (09 §4): an episode past its wall-clock budget STOPs — answer best-so-far. The
	// override sits ABOVE the Controller (out-of-time is a hard real-world fact, same authority class
	// as a structural floor fact: the model/Controller may not extend a missed deadline). Time-blind
	// (nil clock / zero deadline, the default) this is a constant false — byte-identical behavior.
	if decision != types.STOP && e.deadlineExceeded() {
		decision = types.STOP
	}
	// FIX 2 (force-a-grounding-read-before-give-up): on a grounding-shaped goal, a PRE-grounding give-up
	// STOP is downgraded to ACT (import reality first) — the override sits ABOVE the Controller, same
	// authority class as the deadline override. Flag OFF / non-give-up / already-acted / non-grounding
	// goal ⇒ the floor decision stands (byte-identical). It re-opens the active branch + emits
	// escalation.force_ground when it fires.
	decision = e.forceGroundDecision(decision)

	// T2.1: the INDEPENDENT answer-verifier. With critic.answer_verify ON, before COMMITTING a final
	// factual answer (a GOAL_MET STOP/DELIVER), re-retrieve web evidence for the answer claim (the world is
	// the INDEPENDENT signal — never a same-model re-read of its own chain, which the literature + our own
	// measurement show cannot fix a systematic bias) and check support: supported/unverifiable ⇒ the commit
	// stands; UNSUPPORTED ⇒ downgrade to THINK (keep working the line). The override sits ABOVE the
	// Controller, same authority class as the deadline / force-ground overrides. Flag OFF / web-blind /
	// non-commit ⇒ a no-op (byte-identical). It emits critic.answer_verify (Pattern-C: never silent).
	decision = e.verifyAnswerDecision(decision)

	// OFFLINE-RL flywheel (Track C, RL roadmap §6 P0): capture this (state, action) decision tuple — the
	// formal-model state projection o_t + the FINAL Controller action — into the per-episode buffer. The
	// grounded OUTCOME is unknown here; it is backfilled at episode close (the Monte-Carlo return). A pure
	// read of engine state that alters NO decision and emits NO event yet (the event fires at close).
	// No-op when flywheel.capture is OFF (nil recorder) ⇒ byte-identical.
	e.flywheelRecordDecision(tick, decision, workflowPending)

	e.execute(decision, relay, tick, asyncAct)
	if decision == types.BRANCH {
		e.forked[ab] = struct{}{}
		if strings.Contains(e.controller.LastMeta.Reason, "verify") {
			e.verifyBranched[ab] = struct{}{}
		}
	}

	if wf := e.subconscious.Workflow(); wf != nil && wf.Recognized() {
		// Loop as a feedback operator (P3.2): if the goal is already met, early-exit the loop's
		// remaining iterations. This matters precisely inside a workflow — WorkflowPending blocks the
		// controller's STOP, so a satisfied loop would otherwise grind to its MaxIter bound. The unroll
		// stays the hard upper bound; this just stops sooner when the work is done.
		wf.SkipLoopIfSatisfied(func(string) bool { return e.controller.GoalSatisfied(e.graph) })
		wf.Advance()
	}

	// Durability accounting: specialist injections are excitation (offspring -> n); drives,
	// default-mode and percepts are immigrants (baseline -> μ). Keeping them separate is what lets
	// the dashboard show n<1 held while μ>0 (the awake-durable regime).
	admitted := 0
	for _, vp := range relay.Verdicts {
		if vp.Verdict.Admit() {
			admitted++
		}
	}
	// n is the recursive branching ratio = forks actually created this tick (a BRANCH spawns one
	// new branch). A parallel fan-out's breadth flows into λ̂/U (intensity/schedulability), not n.
	forks := 0
	if decision == types.BRANCH {
		forks = 1
	}
	uo := regulator.UpdateOpts{
		Fired:             0,
		Admitted:          0,
		Forked:            forks,
		Baseline:          baseline,
		BranchesLive:      e.branchesLive(),
		ActionOutstanding: e.awatched.OutstandingCount(),
		// dead-time τ only applies to async (awake) action; reactive ACT blocks, so τ=0.
		FeedbackLatency: 0.0,
	}
	if excitation {
		uo.Fired = len(raw)
		uo.Admitted = admitted
	}
	if asyncAct {
		uo.FeedbackLatency = float64(e.awatched.Latency)
	}
	// CONFIG (M1): regulator.enforce OFF ⇒ skip the θ control law (the admission threshold is not
	// adjusted; Theta() holds its last value). This is regime-affecting — Validate() warns and the
	// stability suite would fail this regime — but the wire stays and the bypass is observable.
	if e.gates.regulator.Disabled() {
		e.gates.regulator.Skip("durability control bypassed")
	} else {
		e.regulator.Update(uo)
	}
	return decision
}

// execute carries out the Controller's decision via the Thought MCP + the watched seam. Mirrors
// Python Engine._execute (the ladder order is load-bearing).
func (e *Engine) execute(decision types.Decision, relay seams.RelayResult, tick int, asyncAct bool) {
	g, mcp := e.graph, e.mcp
	switch decision {
	case types.THINK:
		return
	case types.BRANCH:
		src := g.ActiveBranch
		if relay.Conflict && len(relay.Losers) > 0 {
			loser := relay.Losers[0]
			seed := &types.Thought{ID: -1, Text: loser.Text, Source: types.INJECTED,
				Confidence: loser.Relevance, RawReturn: &loser}
			newID := mcp.Branch("conflicting injection -> fork loser", seed)
			// The fork holds the contradicting view — record a first-class CONTRADICTS edge.
			if g.AddXref(src, "CONTRADICTS", newID) {
				e.bus.Emit(events.XRef, "b"+itoa(src)+" CONTRADICTS b"+itoa(newID),
					events.D{"src": src, "kind": "CONTRADICTS", "dst": newID})
			}
		} else {
			mcp.Branch("deliberate: explore / verify alternative", nil)
		}
		return
	case types.MERGE:
		if target := e.controller.MergeTarget(g); target != nil {
			mcp.Merge(g.ActiveBranch, *target)
		}
		return
	case types.BACKTRACK:
		mcp.Rerank()
		// Which open branch to resume (resolution order in resolveFocusBranch): value-greedy
		// Frontier()[0] by default (THOUGHT_UCB_C=0 AND THOUGHT_BEAM_LAMBDA=0 ⇒ byte-identical); the
		// UCB exploration policy (search.UCBFrontier, T1.3) when THOUGHT_UCB_C>0; else the verifier-
		// guided beam (search.BeamBest, 07 §D #1) when THOUGHT_BEAM_LAMBDA>0.
		best, greedy := resolveFocusBranch(g, ucbC, beamLambda, e.branchVisits)
		if best != nil {
			// T1.3: a focus/resume is a visit — increment deterministically so a repeatedly-resumed
			// branch accrues visits and its UCB exploration bonus shrinks. Maintained unconditionally
			// (cheap), but only READ when ucbC>0, so it cannot change the default-path output.
			e.branchVisits[best.ID]++
			// Observability: when the UCB policy is on AND it resumed a NON-greedy line (the exploration
			// bonus overtook the value head), surface it. Default (ucbC=0) ⇒ no event, byte-identical.
			if ucbC > 0 && greedy != nil && best.ID != greedy.ID {
				e.bus.Emit(events.UCBSelect, "UCB resumed under-explored b"+itoa(best.ID)+" over greedy b"+itoa(greedy.ID),
					events.D{"branch": best.ID, "greedy": greedy.ID, "value": best.Value,
						"greedy_value": greedy.Value, "visits": e.branchVisits[best.ID] - 1, "c": ucbC})
			}
			mcp.Focus(best.ID)
		}
		return
	case types.ACT:
		// CONFIG (grounding-read ablation): seam.watched_sync OFF ⇒ the synchronous watched seam does
		// NOT import reality. The ACT short-circuits to pass-through — no intention crosses the seam, no
		// action.observation, nothing fed to the grounding spine — so the harness cannot ground against
		// reality and answers from priors (the gate-off arm of the grounding ablation, spec §5.1). The
		// branch is still marked acted so the Controller moves the line on (it does not re-ACT forever).
		if !e.watchedSyncEnabled() {
			e.actedBranches[g.ActiveBranch] = struct{}{}
			return
		}
		intention := e.formIntention()
		if allow, why := cognition.VetAction(intention.Text); !allow {
			// slice (k): the conscience floor refuses this action — it does not cross the watched seam.
			e.bus.Emit(events.ActionBlocked, "conscience refused: "+why, events.D{"reason": why, "text": intention.Text})
			e.actedBranches[g.ActiveBranch] = struct{}{} // mark acted so the line moves on (no re-ACT loop)
			return
		}
		// slice (k) CEILING (§7.2, Pattern-C): the floor ALLOWED this action; on a flagged-fuzzy case the
		// conscience model CEILING may TIGHTEN (refuse). A non-escalation lets the floor stand (surfaced).
		if refuse, why := e.conscienceCeilingRefuses(intention.Text); refuse {
			e.bus.Emit(events.ActionBlocked, "conscience ceiling refused: "+why,
				events.D{"reason": why, "text": intention.Text, "ceiling": true})
			e.actedBranches[g.ActiveBranch] = struct{}{}
			return
		}
		e.tlActed(tick, g.ActiveBranch) // slice (i): an act crosses the watched seam (the action-time anchor)
		e.episodeActsIssued++           // FIX 2: a grounding ACT was issued this episode (gates the force-a-read-before-give-up override)
		// slice (f): bound async dead-time — at the MAX_OUTSTANDING cap, fall to the sync path (block on
		// reality) rather than fire another outstanding action. Default cap (8) ⇒ normal runs unaffected.
		if asyncAct && e.regulator.OutstandingAllowed(e.awatched.OutstandingCount()) {
			e.awatched.Fire(intention, tick) // fire & keep thinking
			e.actedBranches[g.ActiveBranch] = struct{}{}
		} else {
			e.lifecycle.Transition(types.AWAITING_REALITY, "ACT: open to reality")
			obs := e.watched.OpenToReality(intention)
			e.appendThought(&obs, tick)
			e.groundObservation(intention, obs) // feed reality into the grounding spine (N.1a)
			e.actedBranches[g.ActiveBranch] = struct{}{}
			e.rerank(g)
			e.lifecycle.Transition(types.S_ACTIVE, "observation returned")
		}
		return
	case types.DELIVER:
		// A2: the EXECUTIVE decided to speak — the engine only actuates. Delivery resolves the
		// user line (the high-water mark), then the line closes exactly like a STOP: reactive
		// runs the stopping housekeeping (its deliver guard sees the line resolved and stays
		// silent); awake softens and moves on by value.
		e.deliverResponse()
		if e.mode == "continuous" {
			e.softenStop(g, mcp, tick, false)
			return
		}
		e.stop(tick)
	case types.STOP:
		if e.mode == "continuous" {
			// Stamp the one conversationally-relevant case: the mind is leaving the USER's line
			// WITHOUT having answered it (set aside). With DELIVER first-class this is rare on the
			// STOP path (the spine speaks before closing a user line) — the stamp remains for the
			// presence layer's honesty. It must reflect an UNRESOLVED user turn (id beyond the
			// delivered high-water mark), not merely a USER_INPUT thought still physically present in
			// the context after it was answered — else an already-answered turn is re-labelled
			// "unanswered" on every subsequent STOP (the "set aside — unanswered" spam).
			setAside := g.UnresolvedUserInput(g.ActiveBranch)
			e.softenStop(g, mcp, tick, setAside)
			return
		}
		e.stop(tick)
	}
}

// softenStop is the awake close: no hard stop — finish this line and move on (bounded focus),
// rather than re-deciding on the same satisfied line. WHERE the mind goes next is value-driven
// (§5.6 maintenance): the most valuable unfinished frontier line, if one clears the pursuit
// threshold — an unanswered user line holds 1.0 there via the A1 pending term — else a fresh
// endogenous line. Shared by the silent close (STOP) and the close-after-speech (DELIVER).
func (e *Engine) softenStop(g *graph.ThoughtGraph, mcp *graph.ThoughtMCP, tick int, setAside bool) {
	mcp.Tick = tick
	// A-RAG5: the awake close. Credit the just-finished line's value to every knowledge fact recalled on
	// it, BEFORE refocusing (Active() still points at the closing line here). No-op when convert.facts is
	// OFF ⇒ byte-identical. The reactive close runs the same attribution in stop(); softenStop is the awake
	// twin, so consolidation works in both regimes.
	e.convert.AttributeFactValue(g.Active().Epistemic)
	if fr := g.Frontier(); len(fr) > 0 && fr[0].Value >= e.controller.PursuitThreshold() {
		mcp.Focus(fr[0].ID)
		e.pruneBranches()
		e.bus.Emit(events.Decision, "STOP softened (awake): finished a line, resuming the most valuable unfinished line",
			events.D{"decision": "STOP", "soft": true, "set_aside": setAside, "resume": fr[0].ID})
		return
	}
	reason := "line done; moving on"
	fresh := g.NewBranch(intPtr(g.ActiveBranch), &reason)
	mcp.Focus(fresh)
	e.pruneBranches()
	e.appendThought(e.drives.FreshGoal(), tick)
	e.bus.Emit(events.Decision, "STOP softened (awake): finished a line, moving on",
		events.D{"decision": "STOP", "soft": true, "set_aside": setAside})
}

// deliverResponse is the outbound Action: synthesise the answer from the resolved graph and
// speak it to the user across the watched seam. Mirrors Python Engine._deliver_response.
func (e *Engine) deliverResponse() string {
	e.emitAssembledView()           // SR-2: the responder consumes an EXECUTIVE-template view of the context
	e.applyPersonaFragment()        // P7.3: learned user adaptation rides the outward-facing respond only
	e.applyGroundCompleteFragment() // THOUGHT_GROUND_COMPLETE: grounding-completeness reading directive (flag OFF ⇒ no-op)
	// SELF-MODEL -> the REPLY (board SELF-MODEL): when sense.self_model is ON and the user asked a self-
	// directed question (an identity / self / capability / location ask — the relevance gate), fold the
	// grounded standing-core self-model + a targeted SelfModelLookup into the RESPOND context so the model
	// answers FROM the harness self-knowledge, not its bare-model "I'm an LLM" prior. Default OFF / a normal
	// question ⇒ context unchanged ⇒ byte-identical RESPOND prompt.
	ctx := e.selfModelReplyContext(e.workingContext())
	answer := e.backend.Respond(e.graph.Goal, ctx)
	if answer == "" {
		// OUTPUT role: the substrate could not synthesise an answer. Speak the failure honestly
		// rather than a blank turn or a heuristic template — output goes through the model or not
		// at all.
		answer = "(I couldn't produce an answer — the thinking substrate is unavailable. " +
			"Load a model and try again.)"
	}
	e.lastResponse = answer
	e.graph.MarkDelivered() // resolution is graph state: releases the V(s) pending-user term (A1)
	e.transcript = append(e.transcript, transcriptTurn{Role: "assistant", Text: answer})
	e.bus.Emit(events.Respond, answer, events.D{"kind": "respond", "goal": e.graph.Goal})
	return answer
}

// stop applies the stopping taxonomy: deliver the answer, feed the trace to convertibility, map the
// StopKind to a lifecycle target, and (if DONE and quiescent) fall to IDLE. Mirrors Python Engine._stop.
// activeGoal returns the current (last-created) Goal entity, or nil if none. The active goal is the
// last in e.goals (one episode = one top goal until per-line goal binding, §1.8 G5).
func (e *Engine) activeGoal() *cognition.Goal {
	if len(e.goals) == 0 {
		return nil
	}
	return &e.goals[len(e.goals)-1]
}

func (e *Engine) stop(tick int) {
	meta := e.controller.LastMeta
	kindName := "GOAL_MET"
	if meta.StopKind != nil && *meta.StopKind != "" {
		kindName = *meta.StopKind
	}
	kind, _ := types.ParseStopKind(kindName)
	if g := e.activeGoal(); g != nil {
		met := kind == types.GOAL_MET
		// Acceptance model CEILING (#29, §1.6): when acceptance_ceiling is on, the model may refine the
		// conclude outcome on a flagged-fuzzy goal (floor stands otherwise). OFF (default) ⇒ unchanged.
		if e.features.Conscious.Activity.AcceptanceCeiling {
			switch e.acceptanceCeiling(g) {
			case cognition.AcceptanceMet:
				met = true
			case cognition.AcceptanceUnmeetable:
				met = false
			}
		}
		g.Conclude(met) // §1.7: the goal lifecycle finally reaches a terminal state
		// Goal feedback (§1.9): a give-up (not met) on a SUBGOAL is an unmeetable signal — propagate it
		// up the tree and drive the parent's revision. goal_feedback OFF / a top goal ⇒ a no-op.
		if !met && !g.IsTopGoal() {
			e.reviseGoalOnUnmeetable(g.ID)
		}
	}
	if e.features.Conscious.Activity.Learn { // Phase 5: REINFORCE β_branch from the goal-relative return
		gReturn := 0.0
		if kind == types.GOAL_MET {
			gReturn = 1.0
		}
		e.controller.LearnFromReturn(gReturn)
		// slice h (§5.3 OUTER loop): feed the same return into the activity-θ keep-or-revert bandit — at a
		// window's close it KEEPs the inner-loop drift iff J strictly improved, else REVERTs θ.
		if e.features.Conscious.Activity.Experiment {
			e.controller.ExperimentWindow(gReturn)
		}
	}
	// OFFLINE-RL flywheel (Track C, RL roadmap §6 P0): the episode closed — backfill the INDEPENDENT
	// terminal grounded Outcome (the StopKind + the grounding spine's grounded/refuted tally, NEVER a
	// self-judgment, §6.5) onto every buffered (state, action) decision tuple and flush + emit them. The
	// label is the genuine ENVIRONMENT reward the offline learner regresses on. No-op when flywheel.capture
	// is OFF (nil recorder) ⇒ byte-identical.
	e.flywheelCloseEpisode(kind)
	// Speak only when a user is still unanswered: the spine's DELIVER decision is the normal
	// speech path (A2); this guard covers the closes the spine could not see the user line on
	// (e.g. the episode ends while focused on a child branch). Never a second reply for one turn.
	if e.UserWaiting() {
		e.deliverResponse()
	}
	// CONFIG (M1): when ALL convert mints are OFF, skip the episode-trace Observe (no pattern tally,
	// nothing to mint later). Any single mint enabled keeps observing. Bypass, not delete.
	if e.gates.convertObs.Disabled() {
		e.gates.convertObs.Skip("episode-trace observe bypassed")
	} else {
		e.convert.Observe(e.graph) // episode trace -> convertibility detector
		// A-RAG5: credit this episode's converged line value to every knowledge fact recalled in it (the CLS
		// consolidation basis — a fact active during a high-value experience consolidates). No-op when
		// convert.facts is OFF (no facts were noted) ⇒ byte-identical. EPISTEMIC value (no pending-user term),
		// matching the pattern/triple mint basis so a failure episode on an urgent line never consolidates.
		e.convert.AttributeFactValue(e.graph.Active().Epistemic)
		// M5: if a PATH skill drove this episode, record the directed traversal with its grounded outcome
		// (the path closed on a grounded source iff THIS episode grounded a claim). A hot, grounded path is
		// surfaced at the next Consolidate (convert.path_mint). Reset so it isn't re-counted on a soft STOP.
		if e.activePath != "" {
			grounded := e.grounding.Len() > e.episodeGroundBase
			e.convert.NotePath(e.activePath, grounded, e.graph.Active().Epistemic)
			e.activePath = ""
		}
	}
	e.terminateSessionTree(kindName) // close the episode's runtime spawn tree (P3.8 guaranteed-termination)
	e.recordEpisode()                // declarative memory: record the grounded episode (P2.3, never-fabricate)
	e.reflectMemory()                // idle consolidation: distil a high-value grounded episode -> belief (P6.2)
	target := e.lifecycle.Stop(kind, meta.Reason)
	if target == types.DONE && e.controller.Quiescent(e.graph, e.port.Pending(), e.awatched.HasOutstanding()) {
		e.lifecycle.Transition(types.IDLE, "quiescent -> idle (consolidate)")
	}
	// Cross-session persistence (M4): on lifecycle→DONE, persist + flush the curated learned state so it
	// survives the exit (the §4.4 Flush-on-DONE). nil store ⇒ no-op.
	if target == types.DONE {
		e.flushState()
	}
}

// -- reactive (episodic) loop -------------------------------------------

// stepReactive runs one tick of the episodic loop. Mirrors Python Engine._step_reactive.
func (e *Engine) stepReactive(tick int) StepResult {
	ls := e.lifecycle
	if e.graph == nil || ls.State == types.IDLE || ls.State == types.DONE {
		// CONFIG (M1): when ALL convert mints are OFF, skip the IDLE consolidation pass (it would mint
		// nothing). Bypass, not delete — the idle wire stays.
		if e.gates.convertConsol.Disabled() {
			e.gates.convertConsol.Skip("idle consolidation bypassed")
		} else {
			e.convert.Consolidate()                             // idle-time compilation
			e.convert.RefineRegistry()                          // GAP 11: the uniform per-registry refine pass (no-op unless convert.refine_loop)
			e.convert.ConsolidateFacts(e.knowledge, e.bus.Tick) // A-RAG5: consolidate high-V recalled facts -> priors (no-op unless convert.facts)
		}
		// Cross-session persistence + curator (M4): at IDLE consolidation (off the hot path), persist the
		// learned state and run the PURE Curator (versioning/dedup/decay/demotion/GC/caps). nil store /
		// persistence-off ⇒ no-op, so the bare path is byte-identical.
		e.curateState()
		// Self-benchmark loop (Track H, SB0 — selfbench.go): when selfbench.enabled is ON, self-benchmark
		// a FROZEN checkpoint of the just-consolidated state on a SHADOW engine (propose-and-gate; it
		// measures, never self-commits). Default OFF ⇒ a no-op (no shadow engine, no bench.* event) ⇒
		// byte-identical. Runs AFTER persist+curate so the checkpoint reflects this consolidation.
		e.maybeSelfBench()
		if !e.port.Pending() {
			// INTROSPECTIVE-FAITHFULNESS self-report (Track H §8, introspect_faithfulness.go): at quiescence —
			// after the episode has thought, so the readable layers (goal / active line / V(s) / recent events)
			// are meaningful — emit ONE self-report checked against the addressable ground truth, with the
			// opaque subconscious layer honestly declined. Bounded to once per episode-end (selfReportedEpisode);
			// a no-op unless introspect.self_report is ON ⇒ default OFF ⇒ no event ⇒ byte-identical.
			e.maybeSelfReport(tick)
			// INTROSPECTIVE-FAITHFULNESS SUITE (Track H §8, introspect_suite.go — H-SB3): at quiescence — after
			// the episode has thought, so the readable layers (active line / Controller why / V(s)+goal+state)
			// are meaningful — run the standing SET of self-report probes against the addressable ground truth
			// and emit the rolled-up introspect.suite verdict, with the opaque subconscious layer honestly
			// declined. Bounded to once per episode-end (introspectedEp); a no-op unless introspect.suite is ON
			// ⇒ default OFF ⇒ no probes, no event ⇒ byte-identical.
			e.maybeIntrospectSuite(tick)
			if ls.State != types.IDLE {
				ls.Transition(types.IDLE, "quiescent")
			}
			e.idleRegulate()
			composite := lifecycle.CompositeIdle(true, true, true, false)
			e.bus.Emit(events.Lifecycle, "partial-idle composite: "+composite, events.D{"composite": composite})
			return StepResult{Tick: tick, State: ls.State.String(), Idle: true, Note: "idle", Meta: map[string]any{}}
		}
		goal, _ := e.port.Pop()
		if strings.TrimSpace(goal) == "" { // ignore empty/blank input
			e.idleRegulate()
			return StepResult{Tick: tick, State: ls.State.String(), Idle: true,
				Note: "blank input ignored", Meta: map[string]any{}}
		}
		e.observePersonFeedback(goal) // P7.3: style feedback on the PREVIOUS answer (before this turn is recorded)
		e.startEpisode(goal, true)    // reactive goals always arrive from outside (a user turn)
		e.transcript = append(e.transcript, transcriptTurn{Role: "user", Text: goal})
		ls.Transition(types.S_ACTIVE, "woke on user input")
		return StepResult{Tick: tick, State: ls.State.String(), Note: "start: " + runeSlice(goal, 40),
			Meta: map[string]any{}}
	}

	// AWAITING_USER: a new turn continues the same graph.
	if ls.State == types.AWAITING_USER && e.port.Pending() {
		ls.Transition(types.S_ACTIVE, "next user turn")
	}

	// 0. interrupt: unsolicited input preempts the current branch.
	if e.port.Pending() && ls.State == types.S_ACTIVE {
		u := e.port.Deliver(e.filter, e.graph.History(), e.valueScalar())
		if u != nil {
			e.observePersonFeedback(u.Text)                                                 // P7.3: an interrupt can be style feedback too
			e.transcript = append(e.transcript, transcriptTurn{Role: "user", Text: u.Text}) // record the interrupting turn (B1)
			e.graph.Goal = u.Text                                                           // the new input becomes the goal so respond targets it
			ls.Transition(types.SUSPENDED, "interrupt: compress active branch")
			e.controller.OnInterrupt(e.graph, e.mcp, *u)
			ls.Transition(types.S_ACTIVE, "refocused on new goal")
		}
	}

	ctx := e.graph.ActiveContext()
	// Loop-closure / recurrence keyframe DB (Track F, F-M7): fingerprint the active line's tip into the
	// persistent recurrence index; a re-entry fires keyframe.close (anti-rumination). nil DB ⇒ inert.
	e.observeKeyframe(e.activeTip())
	raw, bias := e.dispatch(ctx) // CONFIG (M1): gated pull-dispatch (OFF ⇒ CONSCIOUS generates)
	// M3 §3.3: source + fuse the fuel-needing candidates' missing material via the ladder BEFORE the
	// hidden seam — a fabricated-fuel candidate reaches the Filter still marked GENERATED (the seam is
	// fed, never bypassed). A non-fuel-needing candidate (tool-backed ground truth, a stance) passes
	// through. Bypass when subconscious.concretize is OFF (raw relay).
	raw = e.concretize(raw, ctx)
	decision := e.reason(ctx, raw, bias, tick, false, 0, true)
	return StepResult{Tick: tick, State: ls.State.String(), Decision: decision.String(),
		Meta: metaToMap(e.controller.LastMeta)}
}

// idleRegulate folds an idle tick into the regulator (no excitation, no baseline). Mirrors Python
// Engine._idle_regulate.
func (e *Engine) idleRegulate() {
	uo := regulator.DefaultUpdateOpts()
	uo.Forked = 0
	uo.BranchesLive = e.branchesLive()
	e.regulator.Update(uo)
}

// pruneBranches bounds focus utilisation: DEAD the lowest-value stashed branches beyond capacity,
// reserving two slots (the ACTIVE branch + one a BRANCH may add this tick) so live ≤ capacity (U ≤ 1)
// even at the measurement point inside reason. A SUPERSEDED branch is dropped before a merely-low-value
// one. Mirrors Python Engine._prune_branches.
func (e *Engine) pruneBranches() {
	keepStashed := e.regulator.FocusCapacity() - 2
	if keepStashed < 1 {
		keepStashed = 1
	}
	superseded := e.graph.Superseded()
	stashed := e.graph.StashedBranches()

	// Faculty-scheduler prune-protection (conscious.activity.faculty_scheduler, default OFF ⇒ this is a
	// no-op ⇒ byte-identical). The flat fair-share scheduler can only give a faculty a turn if a live root
	// for it survives the U≤1 prune cap — and the starved faculties (perceptual/mnemonic) are exactly the
	// LOWEST-value lines, so the unprotected cap kills them FIRST, before arbitration ever runs (the
	// measured root cause: the starvation is upstream of the arbiter). So when the scheduler is on, exempt
	// the highest-value live seed root PER FACULTY from being dropped. The exemption is bounded by the
	// SAME keepStashed budget (the 6 faculties == keepStashed=6 == FocusCapacity-2), so U≤1 still holds — it
	// re-allocates the kept budget toward faculty diversity, it does not widen it. Deterministic: the
	// per-faculty winner is value-then-id, the faculty scan is the canonical enum order.
	protected := map[int]struct{}{}
	if e.facultySchedulerOn() && len(e.branchFaculty) > 0 {
		bestPerFac := map[cognition.SeedFaculty]*types.Branch{}
		for _, b := range stashed {
			fac, ok := e.branchFaculty[b.ID]
			if !ok {
				continue // not a seed root
			}
			cur := bestPerFac[fac]
			if cur == nil || b.Value > cur.Value || (b.Value == cur.Value && b.ID > cur.ID) {
				bestPerFac[fac] = b
			}
		}
		// Protect at most keepStashed roots (one per faculty), most-starved faculties first so the diversity
		// the scheduler needs is the diversity that survives.
		facs := make([]cognition.SeedFaculty, 0, len(bestPerFac))
		for fac := range bestPerFac {
			facs = append(facs, fac)
		}
		sort.SliceStable(facs, func(i, j int) bool {
			li, lj := e.facultyLastFocus[facs[i]], e.facultyLastFocus[facs[j]]
			liSeen, ljSeen := false, false
			if v, ok := e.facultyLastFocus[facs[i]]; ok {
				li, liSeen = v, true
			}
			if v, ok := e.facultyLastFocus[facs[j]]; ok {
				lj, ljSeen = v, true
			}
			if liSeen != ljSeen {
				return !liSeen // never-focused (most starved) protected first
			}
			if li != lj {
				return li < lj
			}
			return facs[i] < facs[j]
		})
		for i, fac := range facs {
			if i >= keepStashed {
				break
			}
			protected[bestPerFac[fac].ID] = struct{}{}
		}
	}

	// sort key (protected last, b.id not in superseded, b.value): a protected seed root sorts LAST (never
	// dropped); otherwise a superseded branch (false<true) sorts first, then by value ascending — the
	// lowest-value / superseded branches lead, to be dropped first.
	sort.SliceStable(stashed, func(i, j int) bool {
		_, iProt := protected[stashed[i].ID]
		_, jProt := protected[stashed[j].ID]
		if iProt != jProt {
			return jProt // a protected branch sorts AFTER (later) — dropped last
		}
		_, iSup := superseded[stashed[i].ID]
		_, jSup := superseded[stashed[j].ID]
		// Python sorts by the tuple (not-superseded, value) ascending: False (superseded) before True.
		if iSup != jSup {
			return iSup // iSup==true => "not superseded"==False => sorts first
		}
		return stashed[i].Value < stashed[j].Value
	})
	drop := len(stashed) - keepStashed
	for i := 0; i < drop && i < len(stashed); i++ {
		if _, ok := protected[stashed[i].ID]; ok {
			continue // never DEAD a protected faculty root (bounded ≤ keepStashed, so U≤1 holds)
		}
		stashed[i].Status = types.DEAD
	}
}
