// flywheel.go is the engine half of the OFFLINE-RL DATA FLYWHEEL (Track C, docs/internal/2026-06-21-harness-
// rl-ml-roadmap.md §6 Phase-0 + §6.5): it taps the LIVE decision spine to capture, per Controller decision,
// a training TUPLE — (state-features, action, GROUNDED outcome) — into an append-only dataset so a LATER
// offline learner (a contextual bandit / REINFORCE / a distilled-V head) can train WITHOUT online
// exploration. The package internal/flywheel owns the dataset writer + the backfill machinery; this file
// wires it into the engine at the three sites that make it FIRE:
//
//	startEpisode  -> flywheelOpenEpisode(processID)         a fresh trajectory begins
//	reason        -> flywheelRecordDecision(tick, decision) one (state, action) tuple per Controller decide
//	stop          -> flywheelCloseEpisode(stopKind)         backfill the terminal grounded Outcome + flush
//
// THE §6.5 INVARIANT (load-bearing): the OUTCOME label is the INDEPENDENT terminal/grounded signal, NEVER a
// self-judgment — it is sourced from the grounding spine (e.grounding: a REAL observation grounds/refutes;
// fabricated reality is rejected upstream) and the StopKind GOAL_MET (which itself requires a confirmed
// OBSERVATION, controller.GoalSatisfied). The engine marking its own speech "ok" is forbidden, so the
// label captured here is the genuine ENVIRONMENT reward — exactly the supervision a conservative learner
// must regress on, not the bootstrap critic the Filter exists to keep honest.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. The Recorder is built ONLY when the opt-in flywheel.capture knob is ON;
// otherwise e.flywheel is nil and every hook below is a nil-safe no-op (the *flywheel.Recorder methods
// guard nil). Capture is PURE OBSERVABILITY: it reads engine state, never mutates it, and alters no
// decision — so the loop is byte-identical with the knob OFF and the captured dataset is reproducible on
// the seeded test double (no clock, no RNG of its own — the seeded engine tick stamps every tuple).
package engine

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/flywheel"
	"github.com/berttrycoding/thought-harness/internal/grounding"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// flywheelEnabled reports whether the opt-in flywheel.capture knob is ON. nil features ⇒ AllOn baseline
// has the opt-in OFF (it is an opt-in instrument, not an all-on ablation), so this is false there too.
func (e *Engine) flywheelEnabled() bool {
	return e.features != nil && e.features.Flywheel.Capture
}

// SetFlywheelSink injects the dataset Sink (the JSONL sidecar at the CLI edge; an in-memory sink in tests).
// Must be called BEFORE NewEngine builds the recorder for the injected sink to take — the CLI/test sets it
// on the EngineConfig path is not used; instead the edge sets it post-construction and rebuilds, OR (the
// common path) sets it before the first episode. Because buildFlywheel runs in NewEngine, a sink injected
// afterwards is adopted by re-pointing the live recorder's sink only if the recorder exists. To keep the
// wiring simple and order-independent, SetFlywheelSink (re)builds the recorder around the new sink when
// the knob is ON; a no-op when the knob is OFF (the OFF path never records).
func (e *Engine) SetFlywheelSink(sink flywheel.Sink) {
	e.flywheelSink = sink
	if e.flywheelEnabled() {
		e.flywheel = flywheel.NewRecorder(e.flywheelSinkOrMem(), e.emitFlywheelCapture)
	}
}

// flywheelSinkOrMem returns the injected sink, or an internal in-memory sink when none was injected — so a
// knob-ON engine with no edge-wired file still buffers + emits (the flywheel.capture event fires), it just
// has no on-disk dataset. The CLI edge injects a JSONLSink to persist the corpus.
func (e *Engine) flywheelSinkOrMem() flywheel.Sink {
	if e.flywheelSink != nil {
		return e.flywheelSink
	}
	return flywheel.NewMemSink()
}

// buildFlywheel constructs the per-decision Recorder IFF the opt-in knob is ON (called from NewEngine).
// OFF ⇒ e.flywheel stays nil ⇒ every hook is a no-op ⇒ byte-identical.
func (e *Engine) buildFlywheel() {
	if !e.flywheelEnabled() {
		return
	}
	e.flywheel = flywheel.NewRecorder(e.flywheelSinkOrMem(), e.emitFlywheelCapture)
}

// emitFlywheelCapture fires one flywheel.capture event per finalised tuple at episode close — the
// observability contract (the bus IS the per-subsystem log; a captured tuple is invisible without it).
// Pure CONTROL: NO model call, NO RNG, NO clock. The tick on the event is the tuple's seeded decision tick.
func (e *Engine) emitFlywheelCapture(t flywheel.DecisionTuple) {
	e.bus.Emit(events.FlywheelCapture,
		"flywheel: "+t.Episode+" step "+itoa(t.Step)+" "+t.Action+" -> "+t.Outcome.StopKind,
		events.D{
			"episode":      t.Episode,
			"tick":         t.Tick,
			"step":         t.Step,
			"action":       t.Action,
			"value":        t.State.Value,
			"theta":        t.State.Theta,
			"n":            t.State.N,
			"goal_met":     t.Outcome.GoalMet,
			"greturn":      t.Outcome.GReturn,
			"stop_kind":    t.Outcome.StopKind,
			"grounded_obs": t.Outcome.GroundedObs,
			"refuted_obs":  t.Outcome.RefutedObs,
		})
}

// flywheelOpenEpisode starts a fresh trajectory for the flywheel (called from startEpisode). No-op when the
// recorder is nil (knob OFF).
func (e *Engine) flywheelOpenEpisode() {
	e.flywheel.OpenEpisode(e.processID)
}

// flywheelState snapshots the formal-model state projection o_t (2026-06-21-harness-formal-model.md §1,
// §8) the policies observe: the active-branch scalars + the regulator + arousal + the workflow latch +
// pending-user. A pure read of live engine state (no mutation, no model, no RNG). workflowPending is
// passed in because reason computes it locally at the decision site.
func (e *Engine) flywheelState(workflowPending bool) flywheel.StateFeatures {
	g := e.graph
	ab := g.ActiveBranch
	var (
		value, epi     float64
		branchLen      int
		userUnresolved bool
	)
	if b := g.Active(); b != nil {
		value, epi = b.Value, b.Epistemic
		branchLen = len(b.ThoughtIDs)
		userUnresolved = g.UnresolvedUserInput(ab)
	}
	return flywheel.StateFeatures{
		BranchID:        ab,
		Value:           value,
		Epistemic:       epi,
		BranchLen:       branchLen,
		Depth:           g.Depth(ab),
		Frontier:        len(g.Frontier()),
		UserUnresolved:  userUnresolved,
		Theta:           e.regulator.Theta(),
		LamHat:          e.regulator.LamHat(),
		N:               e.regulator.N(),
		Mu:              e.regulator.Mu(),
		U:               e.regulator.Util(),
		Arousal:         e.arousal.String(),
		WorkflowPending: workflowPending,
		PendingUser:     g.UserWaiting(),
		Mode:            e.mode,
	}
}

// flywheelRecordDecision buffers one (state, action) decision tuple (called from reason after the
// Controller's decision is finalised). The outcome is unknown here — it is backfilled at episode close.
// No-op when the recorder is nil (knob OFF).
func (e *Engine) flywheelRecordDecision(tick int, decision types.Decision, workflowPending bool) {
	if e.flywheel == nil {
		return
	}
	e.flywheel.RecordDecision(tick, e.flywheelState(workflowPending), decision.String())
}

// flywheelCloseEpisode backfills the INDEPENDENT terminal grounded Outcome onto every buffered tuple and
// flushes them (called from stop). The label is sourced from the grounding spine + the StopKind — never a
// self-judgment (§6.5). No-op when the recorder is nil (knob OFF) or no decisions were buffered.
func (e *Engine) flywheelCloseEpisode(kind types.StopKind) {
	if e.flywheel == nil {
		return
	}
	goalMet := kind == types.GOAL_MET
	gReturn := 0.0
	if goalMet {
		gReturn = 1.0
	}
	grounded, refuted := e.episodeGroundedTally()
	e.flywheel.CloseEpisode(flywheel.Outcome{
		GReturn:         gReturn,
		GoalMet:         goalMet,
		StopKind:        kind.String(),
		EpisodeGrounded: e.grounding.Len() > e.episodeGroundBase,
		GroundedObs:     grounded,
		RefutedObs:      refuted,
	})
}

// episodeGroundedTally counts the OBSERVATIONs this episode that GROUNDED vs REFUTED a claim — the
// independent reality verdicts the env reward sums (+1 grounded / -0.5 refuted, formal-model §4.2). It
// reads the grounding ledger's tail since episode open (episodeGroundBase), so it is a deterministic read
// of the INDEPENDENT signal, never a self-grade. A fabricated tier-0 observation never entered the ledger
// (IngestObservation rejects it), so it cannot inflate either count.
func (e *Engine) episodeGroundedTally() (grounded, refuted int) {
	for _, exp := range e.grounding.Since(e.episodeGroundBase) {
		switch exp.Verdict {
		case grounding.Grounded:
			grounded++
		case grounding.Refuted:
			refuted++
		}
	}
	return grounded, refuted
}
