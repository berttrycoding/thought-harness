package engine

import (
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// features.go threads the unified system-wide HarnessConfig (the representation-space rebuild, M1)
// through the engine. The engine owns the loop and calls every component, so it holds one
// config.Gate per component decision point and consults it at the call site: when a toggle is OFF the
// engine short-circuits THAT component to its pass-through behaviour and the gate emits config.skip.
//
// THE HARD RULE (§4.3): a toggle never deletes a wire, it bypasses a decision. A disabled component
// short-circuits to pass-through — Filter OFF ⇒ admit-all, Gate-priors OFF ⇒ no prior merged,
// Transform OFF ⇒ raw relay, Value OFF ⇒ no rerank, Convert-mint OFF ⇒ observe-but-don't-mint,
// Regulator OFF ⇒ no θ control, Memory.* OFF ⇒ that store/op is skipped. The graph stays intact, the
// TUI still renders the path (as DISABLED), determinism holds, and re-enabling needs no rebuild.
//
// Because the gates read the SHARED *config.HarnessConfig pointer each call, a live TUI flip mutates
// that pointer in place and the next tick observes it — no engine reconstruction (unlike the old
// Settings popup that rebuilt the engine and dropped all learned state).

// engineGates holds the per-component config.Gate handles. Each gate's getter reads the live toggle
// off the shared *config.HarnessConfig, so a flip is seen with no rebuild. A nil gate ⇒ enabled, so a
// component with no gate behaves exactly as the pre-config code.
type engineGates struct {
	// hidden seam
	filter    *config.Gate // seam.hidden_filter
	gate      *config.Gate // seam.hidden_gate (ranking/arbitration)
	transform *config.Gate // seam.hidden_transform (re-voicing)
	gatePrior *config.Gate // seam.gate_priors (the convert-compiled standing bias merged into the Gate)
	legible   *config.Gate // seam.legible_generation (the WF-E CC-1 SHADOW instrument; DEFAULTS OFF, opt-in)

	// conscious
	generate    *config.Gate // conscious.generate (the Conscious effortful loop)
	backtrack   *config.Gate // conscious.allow_backtrack (the retrace-off ablation, installed on the Controller)
	endogenous  *config.Gate // conscious.endogenous_drive (awake Drives/Default-mode/proactive; the continuous-autonomy ablation)
	awakeEngage *config.Gate // conscious.activity.awake_user_engage (AWAKE-DISP rung-1 value floor; installed on the ValueSignal)

	// controller (executive policy)
	activeResource *config.Gate // controller.active_resource (A-RAG4 V(s)-triggered re-sourcing; installed on the Controller)
	answerVerify   *config.Gate // controller.answer_verify (T2.1 INDEPENDENT answer-verifier; consulted at the answer-commit override)

	// watched seam (reality import)
	watchedSync *config.Gate // seam.watched_sync (the synchronous ACT->reality read; the grounding-read ablation)

	// action
	safetyGate *config.Gate // action.safety_gate (the command content gate over the executor; the safety ablation)

	// value + regulator + convert
	value         *config.Gate // value.signal (the rerank pass)
	groundedRew   *config.Gate // value.grounded_reward
	regulator     *config.Gate // regulator.enforce (the θ control law)
	convertObs    *config.Gate // the episode-trace Observe→tally (feeds every mint; OFF only when ALL mints off)
	convertConsol *config.Gate // the IDLE consolidation pass (mints specialists/skills/priors)

	// subconscious
	dispatch    *config.Gate // subconscious.dispatch (the pull-dispatch pass)
	sourcing    *config.Gate // subconscious.sourcing (the §3 ordered fuel ladder)
	concretize  *config.Gate // subconscious.concretize (the §3 concretization stage before the seam)
	sufficiency *config.Gate // seam.sufficiency_gate (A-RAG1 CRAG abstain-vs-over-commit; opt-in, default OFF)
	graphRecall *config.Gate // subconscious.graph_recall (A-RAG3 graph-native recall + write-back; opt-in, default OFF)

	// memory
	memRecall  *config.Gate // memory.recall (episode-start recall)
	memRecord  *config.Gate // memory.episodic (episode-end record)
	memReflect *config.Gate // memory.reflect (idle-tick distillation)
}

// buildGates constructs every per-component gate over the shared config + emit closure. Called once
// in NewEngine after e.features is resolved; the gates close over e.features so a live flip is seen.
func (e *Engine) buildGates() {
	f := e.features
	emit := e.bus.Emit
	g := func(component string, get func() bool) *config.Gate {
		return config.NewGate(component, get, emit)
	}
	e.gates = engineGates{
		filter:         g("seam.filter", func() bool { return f.Seam.HiddenFilter }),
		gate:           g("seam.gate", func() bool { return f.Seam.HiddenGate }),
		transform:      g("seam.transform", func() bool { return f.Seam.HiddenTransform }),
		gatePrior:      g("seam.gate_priors", func() bool { return f.Seam.GatePriors }),
		legible:        g("seam.legible_generation", func() bool { return f.Seam.LegibleGeneration }),
		watchedSync:    g("seam.watched_sync", func() bool { return f.Seam.WatchedSync }),
		safetyGate:     g("action.safety_gate", func() bool { return f.Action.SafetyGate }),
		generate:       g("conscious.generate", func() bool { return f.Conscious.Generate }),
		backtrack:      g("conscious.allow_backtrack", func() bool { return f.Conscious.AllowBacktrack }),
		endogenous:     g("conscious.endogenous_drive", func() bool { return f.Conscious.EndogenousDrive }),
		awakeEngage:    g("conscious.activity.awake_user_engage", func() bool { return f.Conscious.Activity.AwakeUserEngage }),
		activeResource: g("controller.active_resource", func() bool { return f.Controller.ActiveResource }),
		answerVerify:   g("controller.answer_verify", func() bool { return f.Controller.AnswerVerify }),
		value:          g("value.signal", func() bool { return f.Value.Signal }),
		groundedRew:    g("value.grounded_reward", func() bool { return f.Value.GroundedReward }),
		regulator:      g("regulator.enforce", func() bool { return f.Regulator.Enforce }),
		convertObs:     g("convert.observe", func() bool { return e.consolidateEnabled() }),
		convertConsol:  g("convert.consolidate", func() bool { return e.consolidateEnabled() }),
		dispatch:       g("subconscious.dispatch", func() bool { return f.Subconscious.Dispatch }),
		sourcing:       g("subconscious.sourcing", func() bool { return f.Subconscious.Sourcing }),
		concretize:     g("subconscious.concretize", func() bool { return f.Subconscious.Concretize }),
		graphRecall:    g("subconscious.graph_recall", func() bool { return f.Subconscious.GraphRecall }),
		sufficiency:    g("seam.sufficiency_gate", func() bool { return f.Seam.SufficiencyGate }),
		memRecall:      g("memory.recall", func() bool { return f.Memory.Recall }),
		memRecord:      g("memory.episodic", func() bool { return f.Memory.Episodic }),
		memReflect:     g("memory.reflect", func() bool { return f.Memory.Reflect }),
	}
}

// dispatch runs the subconscious pull-dispatch pass through its config gate (M1). When
// subconscious.dispatch is OFF the engine short-circuits to NO SPECIALISTS FIRED (raw empty, empty
// bias) — so CONSCIOUS generates its own next thought instead, exactly as if no specialist crossed
// theta. Bypass, not delete: the wire stays, config.skip records it, determinism holds (the empty
// result is deterministic). Used by both the reactive and the continuous loops.
func (e *Engine) dispatch(ctx []types.Thought) ([]*types.Candidate, map[string]float64) {
	if e.gates.dispatch.Disabled() {
		e.gates.dispatch.Skip("pull-dispatch bypassed")
		return nil, map[string]float64{}
	}
	// SPARSE-DISPATCH (subconscious.dispatch.sparse) + SUB-AGENT GUARD (subconscious.single_strong_agent):
	// refresh both admission flags from the live config each tick so a TUI live-flip is honoured with no
	// rebuild (the engine reads the shared config pointer). OFF (the default) for sparse ⇒ the legacy
	// per-key absolute admission ⇒ byte-identical, no event; OFF for single-strong ⇒ the full fan-out
	// reaches the gate ⇒ byte-identical, no event. The single-strong WIRE is what makes the bench runner's
	// `single-strong` arm a genuinely NON-identical engine from the full-harness arm — the guard's A/B is
	// two different plants, not the same one.
	if e.features != nil {
		e.subconscious.SetSparseDispatch(e.features.Subconscious.SparseDispatch)
		e.subconscious.SetSingleStrong(e.features.Subconscious.SingleStrongAgent)
	}
	return e.subconscious.Dispatch(ctx, e.regulator.Theta(), e.cognitiveView())
}

// endogenousEnabled reports whether the continuous-mode endogenous drive (Drives / Default-mode
// wander / proactive outreach) may fire — the awake-regime-off ablation toggle (conscious.
// endogenous_drive). When OFF it emits config.skip once (deduped) so the bypass is observable, and the
// awake loop runs only on perception + task excitation. Nil-safe (nil gate ⇒ enabled). Used by the
// continuous loop's drive/wander sites and the proactive-outreach gate.
func (e *Engine) endogenousEnabled() bool {
	if e.gates.endogenous.Disabled() {
		e.gates.endogenous.Skip("endogenous drive bypassed -> perception/task only")
		return false
	}
	return true
}

// inboxEscalationEnabled reports whether the async inbox push channel (O-5) may re-surface an ignored
// outreach with escalating urgency — the conscious.activity.inbox_escalation knob. It is an ADDITIVE,
// awake-only feature layered on top of proactive outreach: when OFF the base channel is fire-once-then-
// dedup (no pending tracking, no re-push, no event) and the engine is byte-identical; when ON an
// unacknowledged outreach becomes a pending inbox item that is re-pushed (durability-bounded) until the
// user responds. Nil-safe (a nil features ⇒ AllOn defaults ⇒ OFF for this opt-in flag). Consulted by
// maybeReachOut (to record the pending item) and maybeEscalateInbox (to re-surface it).
func (e *Engine) inboxEscalationEnabled() bool {
	return e.features != nil && e.features.Conscious.Activity.InboxEscalation
}

// watchedSyncEnabled reports whether the synchronous watched seam may import reality — the
// grounding-read ablation toggle (seam.watched_sync). When OFF it emits config.skip once (deduped)
// so the bypass is observable, and the engine short-circuits the ACT->reality read to pass-through:
// no intention crosses the seam, no action.observation is imported, and the grounding spine sees no
// observation to ground — so the harness cannot ground a claim against reality and falls back to its
// priors (the bare-like answer). This is the §4.3 bypass-not-delete rule: the watched-seam wire and
// its panel stay, only the reality-import decision is skipped. Nil-safe (nil gate ⇒ enabled). Used by
// the reactive ACT path and the rung-4 reality sourcer. The async (awake) read is independent.
func (e *Engine) watchedSyncEnabled() bool {
	if e.gates.watchedSync.Disabled() {
		e.gates.watchedSync.Skip("watched-sync bypassed -> no reality import (answer from priors)")
		return false
	}
	return true
}

// safetyGateEnabled reports whether the Action layer's command content gate may fire — the safety
// ablation toggle (action.safety_gate). When OFF it emits config.skip once (deduped) so the bypass is
// observable, and the executor's Evaluate closure short-circuits to admit-all: a catastrophic command
// is NOT blocked, no action.safety_block fires, and the safety mechanism is genuinely absent (the
// gate-off arm of the safety ablation, spec §5.1). This is the §4.3 bypass-not-delete rule: the
// executor wire and its sandbox stay, only the content-gate decision is skipped (the sandbox/approve
// gates are independent). Nil-safe (nil gate ⇒ enabled). Consulted inside buildExecutor's Evaluate.
func (e *Engine) safetyGateEnabled() bool {
	if e.gates.safetyGate.Disabled() {
		e.gates.safetyGate.Skip("safety content gate bypassed -> command admitted unchecked")
		return false
	}
	return true
}

// slamInnovationEnabled reports whether the SLAM self-state estimator's measurement update may fire —
// the opt-in slam.innovation knob (Track F / M1). It reads the LIVE shared config (not a cached bool)
// so a TUI flip is honoured, and syncs the estimator's own Enabled() to the live flag (the estimator
// was constructed from the boot-time flag; this keeps a live flip authoritative without a rebuild).
// When OFF the engine makes ZERO observable estimator calls, so the live loop is byte-identical.
// Nil-safe.
func (e *Engine) slamInnovationEnabled() bool {
	if e.estimator == nil {
		return false
	}
	on := e.features.Slam.Innovation
	e.estimator.SetEnabled(on) // honour a live TUI flip (constructed from the boot-time flag)
	// M9: the calibration meta-estimator rides the SAME live-flip sync — it requires innovation (it
	// consumes the residual stream), so it is enabled iff BOTH knobs are on. Off => inert, byte-identical.
	e.calibrator.SetEnabled(on && e.features.Slam.Calibration)
	// M5: the consistency/observability monitor rides the SAME live-flip sync — it accounts the variance
	// trajectory of the innovation update, so it is on iff BOTH slam.innovation AND slam.consistency are
	// on. Off => no accounting, no estimate.consistency event => byte-identical.
	e.estimator.SetMonitor(on && e.features.Slam.Consistency)
	// M2: the sparse-covariance / Information layer rides the SAME live-flip sync — it correlates the
	// variance trajectory of the innovation update, so it is on iff BOTH slam.innovation AND slam.covariance
	// are on. Off => no correlation graph, no estimate.correlate event => byte-identical.
	e.estimator.SetCovariance(on && e.features.Slam.Covariance)
	// M6: the active-inference info-gain / next-best-observation layer rides the SAME live-flip sync — it
	// ranks the variance trajectory of the innovation update, so it is on iff BOTH slam.innovation AND
	// slam.infogain are on. Off => no ranking, no estimate.infogain event => byte-identical.
	e.estimator.SetInfoGain(on && e.features.Slam.InfoGain)
	// M4: the freshness / staleness-decay layer rides the SAME live-flip sync — it decays the variance
	// trajectory of the innovation update, so it is on iff BOTH slam.innovation AND slam.staleness are on.
	// Off => no decay sweep, no estimate.decay event => byte-identical. The rate Q also tracks the live knob
	// so a TUI tune is honoured without a rebuild.
	e.estimator.SetStaleness(on && e.features.Slam.Staleness)
	e.estimator.SetStalenessQ(e.features.Slam.StalenessQ)
	return on
}

// observeKeyframe folds the active thought-line's tip into the loop-closure / recurrence keyframe DB
// (Track F, F-M7 — "the HINGE") and emits keyframe.close when the engine RE-ENTERS a line it has
// explored before (the anti-rumination / loop-closure signal). The recurrence index is persistent +
// bi-temporal + substrate-tagged, so the loop-back point may lie in a PRIOR run (cross_run) — the
// cross-session recognition the un-persisted DB blocked (gap G3). Pure CONTROL — a deterministic
// content fingerprint of the tip, NO model call. Called once per LIVE tick from the reactive and
// continuous loops. Nil-safe: when persistence.keyframe_db is OFF e.keyframes is nil ⇒ no observe, no
// event ⇒ byte-identical. tip is the active line's current thought text.
func (e *Engine) observeKeyframe(tip string) {
	if e.keyframes == nil || tip == "" {
		return
	}
	cl, closed := e.keyframes.Observe(tip, e.bus.Tick, e.backendLabel)
	if !closed || cl == nil {
		return
	}
	e.bus.Emit(events.KeyframeClose, "keyframe: re-entered \""+cl.Gist+"\" ("+itoa(cl.Closures)+"x)", events.D{
		"descriptor": cl.Descriptor,
		"gist":       cl.Gist,
		"count":      cl.Count,
		"closures":   cl.Closures,
		"first_seen": cl.FirstSeenTick,
		"gap":        cl.GapTicks,
		"cross_run":  cl.CrossRun,
	})
}

// activeTip returns the active branch's tip thought text (the recurrence target the keyframe DB
// fingerprints). Empty when the graph or the active line is empty. Read-only.
func (e *Engine) activeTip() string {
	if e.graph == nil {
		return ""
	}
	if t := e.graph.Last(); t != nil {
		return t.Text
	}
	return ""
}

// consolidateEnabled reports whether the IDLE consolidation pass should run at all — it is the OR of
// the mint toggles it drives (specialist / skill / gate-prior) PLUS A-RAG5 convertibility-on-facts
// (which observes recalled-fact value at episode-trace + consolidates facts at idle). When ALL are OFF,
// the whole pass is a no-op (and the gate emits config.skip), since it would mint/consolidate nothing.
func (e *Engine) consolidateEnabled() bool {
	f := e.features
	return f.Convert.PrimitiveSubAgentMint || f.Convert.SkillMint || f.Convert.GatePriorMint || f.Convert.Facts
}

// Features exposes the shared system-wide config (read-only view for the TUI Config panel + the CLI
// summary). The returned pointer is the LIVE shared config — the TUI bridge mutates it in place via
// ApplyToggle to flip a toggle with no engine rebuild.
func (e *Engine) Features() *config.HarnessConfig { return e.features }

// FeaturesPath exposes the config file the Features were loaded from (display-only; "" ⇒ none). The TUI
// Config tab surfaces it so a surprising run config is traceable.
func (e *Engine) FeaturesPath() string { return e.cfg.FeaturesPath }

// ApplyFeatureToggle flips a bool toggle by its dotted path on the SHARED config, emits config.toggle,
// and re-validates (clamping coupled tunables). Returns ok=false on an unknown / non-bool path. This
// is the engine-side entry the TUI bridge calls for a live flip — NO rebuild, the next tick's gates
// observe the change through the shared pointer.
func (e *Engine) ApplyFeatureToggle(path string, on bool) bool {
	if !config.ApplyToggle(e.features, path, on) {
		return false
	}
	warns := e.features.Validate()
	e.bus.Emit(events.ConfigToggle, "toggle "+path+" -> "+onOff(on), events.D{
		"path": path, "on": on, "warns": warns,
	})
	return true
}

// BumpFeatureTunable nudges an INT tunable by delta on the SHARED config (the Config tab's ←/→), then
// re-validates (so subconscious.max_par_width can never exceed W_max — Validate clamps it) and emits
// config.toggle with the new value. Returns ok=false on an unknown / non-int path. NO rebuild — the
// next tick's gates read the clamped value through the shared pointer.
func (e *Engine) BumpFeatureTunable(path string, delta int) bool {
	k, ok := config.KnobByPath(path)
	if !ok || k.Kind != config.KnobInt {
		return false
	}
	cur, _ := k.GetInt(e.features)
	next := cur + delta
	if next < 0 {
		next = 0
	}
	if !config.SetTunable(e.features, path, itoa(next)) {
		return false
	}
	warns := e.features.Validate()
	v, _ := k.GetInt(e.features)
	e.bus.Emit(events.ConfigToggle, "tunable "+path+" -> "+itoa(v), events.D{
		"path": path, "value": v, "warns": warns,
	})
	return true
}

// BumpFeatureTunableFloat nudges a FLOAT tunable by delta on the shared config (the Config tab's ←/→ on
// a CfgFloat row, e.g. conscious.activity.*), clamped to [0,1] and snapped to 2 decimals to avoid float
// drift. Returns ok=false for a non-float / unknown path. The Controller reads conscious.activity.* LIVE
// off the shared config, so the change takes effect on the next decision with no rebuild.
func (e *Engine) BumpFeatureTunableFloat(path string, delta float64) bool {
	k, ok := config.KnobByPath(path)
	if !ok || k.Kind != config.KnobFloat {
		return false
	}
	cur, _ := k.GetFloat(e.features)
	next := math.Round((cur+delta)*100) / 100
	if next < 0 {
		next = 0
	}
	if next > 1 {
		next = 1
	}
	if !config.SetTunable(e.features, path, strconv.FormatFloat(next, 'f', 2, 64)) {
		return false
	}
	warns := e.features.Validate()
	v, _ := k.GetFloat(e.features)
	e.bus.Emit(events.ConfigToggle, "tunable "+path+" -> "+strconv.FormatFloat(v, 'g', -1, 64), events.D{
		"path": path, "value": v, "warns": warns,
	})
	return true
}

// reprMoveEnabled reports whether a candidate's representation MOVE is permitted by the matrix — the
// one helper a seam call site uses to decide if the matrix gates the candidate. Classified by the
// operator name + provenance (the M1 reprTag stand-in for the M2 Move field). assess/unknown moves
// are never gated. Nil-safe through the matrix accessor.
func (e *Engine) reprMoveEnabled(operator string, source types.Source) bool {
	mv, src := config.ReprTag(operator, source)
	return e.features.Repr.MoveEnabled(mv) && e.features.Repr.SourceEnabled(src)
}

// onOff renders a toggle bool as the friendly word used in summaries.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
