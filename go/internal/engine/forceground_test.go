package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// mkForceGroundEngine builds a minimal reactive engine on the test backend (NOT a RealityComprehender)
// with watched_sync ON (config.New) so there IS a reality path the override can force.
func mkForceGroundEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = config.New()
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// TestForceGroundOverridesGiveUp is the FIX 2 COGNITION test: with THOUGHT_FORCE_GROUND ON, a
// grounding-shaped goal whose Controller floor decided a PRE-grounding GIVE_UP stop (zero acts issued)
// is OVERRIDDEN to ACT — the engine forces a reality import before it lets the line give up from
// priors. With the flag OFF the give-up STOP stands (byte-identical). The override is the engine-level
// twin of the deadline override, and it is observable on escalation.force_ground.
func TestForceGroundOverridesGiveUp(t *testing.T) {
	prev := forceGroundEnabled
	defer func() { forceGroundEnabled = prev }()

	// A minimal engine on the test backend (NOT a RealityComprehender, so the model-driven act path is
	// dark and the only way to reach reality is the forced ACT). watched_sync is ON (config.New/AllOn)
	// so there IS a reality path to force.
	mk := func() *Engine { return mkForceGroundEngine(t) }

	// FLAG OFF: the give-up stands. Drive a grounding goal; record whether escalation.force_ground ever
	// fires (it must not) and whether the override turned a STOP into an ACT (it must not).
	forceGroundEnabled = false
	eOff := mk()
	var offForced int
	eOff.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.EscalationForceGround {
			offForced++
		}
	})
	if got := eOff.forceGroundDecision(types.STOP); got != types.STOP {
		t.Fatalf("flag OFF: forceGroundDecision(STOP) = %v, want STOP (byte-identical)", got)
	}
	if offForced != 0 {
		t.Fatal("flag OFF: escalation.force_ground fired — the give-up path is NOT byte-identical")
	}

	// FLAG ON: stage a grounding episode at the give-up moment (a GIVE_UP stop-kind, no acts issued) and
	// assert the override downgrades the floor STOP to ACT and emits escalation.force_ground.
	forceGroundEnabled = true
	eOn := mk()
	var onForced int
	var onFloor, onForcedTo string
	eOn.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.EscalationForceGround {
			onForced++
			onFloor, _ = ev.Data["floor_decision"].(string)
			onForcedTo, _ = ev.Data["forced"].(string)
		}
	})
	// Stage the exact give-up situation: a grounding-shaped goal, the Controller's last decision a
	// GIVE_UP stop, zero acts issued this episode.
	eOn.startEpisode("investigate the source files and report the value of MaxRetries", true)
	giveUp := types.GIVE_UP.String()
	eOn.controller.LastMeta.StopKind = &giveUp
	eOn.episodeActsIssued = 0

	got := eOn.forceGroundDecision(types.STOP)
	if got != types.ACT {
		t.Fatalf("flag ON: a grounding give-up with zero acts was NOT forced to ACT; got %v", got)
	}
	if onForced != 1 {
		t.Fatalf("flag ON: escalation.force_ground emitted %d times, want exactly 1", onForced)
	}
	if onFloor != "STOP" || onForcedTo != "ACT" {
		t.Fatalf("flag ON: force_ground event floor=%q forced=%q, want STOP->ACT", onFloor, onForcedTo)
	}
	// The active branch must be re-opened (unacted) so the ACT path actually runs next.
	if _, acted := eOn.actedBranches[eOn.graph.ActiveBranch]; acted {
		t.Fatal("flag ON: the active branch was not re-opened — the forced ACT cannot run")
	}
}

// TestForceGroundDoesNotBlockLegitimateStops is the precision control: the override must NEVER block a
// fact-based STOP. A GOAL_MET stop, a non-grounding goal, and a give-up AFTER an act already ran all
// stand even with the flag ON.
func TestForceGroundDoesNotBlockLegitimateStops(t *testing.T) {
	prev := forceGroundEnabled
	defer func() { forceGroundEnabled = prev }()
	forceGroundEnabled = true

	// 1. GOAL_MET stop on a grounding goal -> stands (a legitimate fact-based close).
	e1 := mkForceGroundEngine(t)
	e1.startEpisode("investigate the source files and report the value of MaxRetries", true)
	goalMet := types.GOAL_MET.String()
	e1.controller.LastMeta.StopKind = &goalMet
	e1.episodeActsIssued = 0
	if got := e1.forceGroundDecision(types.STOP); got != types.STOP {
		t.Fatalf("GOAL_MET grounding stop was overridden to %v — a legitimate close must stand", got)
	}

	// 2. GIVE_UP on a NON-grounding goal -> stands (nothing to import).
	e2 := mkForceGroundEngine(t)
	e2.startEpisode("what is the capital of France", true)
	giveUp := types.GIVE_UP.String()
	e2.controller.LastMeta.StopKind = &giveUp
	e2.episodeActsIssued = 0
	if got := e2.forceGroundDecision(types.STOP); got != types.STOP {
		t.Fatalf("a non-grounding give-up was overridden to %v — only grounding goals force a read", got)
	}

	// 3. GIVE_UP on a grounding goal AFTER an act already ran -> stands (reality was already consulted).
	e3 := mkForceGroundEngine(t)
	e3.startEpisode("investigate the source files and report the value of MaxRetries", true)
	e3.controller.LastMeta.StopKind = &giveUp
	e3.episodeActsIssued = 1 // reality was already imported once
	if got := e3.forceGroundDecision(types.STOP); got != types.STOP {
		t.Fatalf("a give-up after an act was overridden to %v — the give-up is honest once reality was consulted", got)
	}
}

// TestResolveForceGroundParsing pins the env-knob parser (default OFF keeps the byte-identical path).
func TestResolveForceGroundParsing(t *testing.T) {
	cases := map[string]bool{"": false, "0": false, "false": false, "garbage": false,
		"1": true, "true": true, "On": true, "yes": true}
	for val, want := range cases {
		t.Setenv("THOUGHT_FORCE_GROUND", val)
		if got := resolveForceGround(); got != want {
			t.Errorf("resolveForceGround(%q) = %v, want %v", val, got, want)
		}
	}
}
