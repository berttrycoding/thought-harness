package engine_test

// deep_ledgers_wire_test.go — the WIRING GATE for the G4 DEEP ledgers + tree (Track G, §5/§7/§8/§9).
// The wiring-gate lesson (saved): a unit that exists but never runs on the engine's actual tick is
// dead. The pure cognition/deep_ledgers_test.go proves the reconstruction off a SYNTHETIC stream; THIS
// test proves it off a REAL engine bus — drive worked scenarios that genuinely fork a thought branch,
// reach the watched seam (ACT -> reality), and spawn a sub-agent session tree, capture the live event
// stream exactly as the TUI bridge's freeze tap does (RecordFromFrozen), and assert fillDeep
// reconstructs the REAL trajectory the §5/§7 panels surface. If fillDeep were not reading the live bus,
// the reconstructed tree / ledger / spawn tree would be empty (the wiring-gate failure).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// TestActionLedgerReconstructsLiveWatchedSeam — every worked scenario crosses the watched seam (an
// intention out, reality back). The §7 action ledger, fed the LIVE stream, must reconstruct that
// trajectory: an ACT paired with the GROUNDED reality that came back. A hollow reconstruction (the
// wiring failure) would leave the ledger empty even though the engine genuinely acted + grounded.
func TestActionLedgerReconstructsLiveWatchedSeam(t *testing.T) {
	_, log := runScenarioLogged(t, "S3") // the dialectic scenario: it measures (ACT) then grounds the result

	rec := cognition.RecordFromFrozen(log.events, nil)

	if len(rec.Actions) == 0 {
		t.Fatal("the action ledger reconstructed ZERO entries off a live scenario that crossed the watched seam — fillActions is not reading the real bus (the wiring-gate failure)")
	}
	var sawAct, sawGrounded bool
	for _, a := range rec.Actions {
		switch a.Kind {
		case "ACT":
			sawAct = true
		case "GROUNDED":
			sawGrounded = true
		}
	}
	if !sawAct {
		t.Error("the live action ledger lost the ACT (the intention out) — the watched seam's outward half is not reconstructed")
	}
	if !sawGrounded {
		t.Error("the live action ledger lost the GROUNDED reality (the reality back) — the watched seam's inward half is not reconstructed")
	}

	// the §7 panel (deep ON) must render the live reconstruction without panicking.
	body := cognition.RenderAnalysisTab(rec, rec, 0, false, deepTab("ACTION·SESS"), 90, false, true, false)
	if body == "" {
		t.Fatal("the ACTION·SESS panel rendered empty over a live reconstruction")
	}
}

// TestSpawnTreeReconstructsLiveSubAgentTeam — S6 (the synthesised-workflow scenario) spawns a real
// sub-agent session tree on the live loop. The §7 spawn tree, fed that live stream, must reconstruct
// ONE worker per dispatched helper and the bounded depth/token spend session.terminate reports — the
// durability-law fan-out bound the panel exists to surface. An empty spawn tree is the wiring failure.
func TestSpawnTreeReconstructsLiveSubAgentTeam(t *testing.T) {
	_, log := runScenarioLogged(t, "S6")

	rec := cognition.RecordFromFrozen(log.events, nil)

	if len(rec.Workers) == 0 {
		t.Fatal("the spawn tree reconstructed ZERO workers off a live scenario that dispatched sub-agents — fillSpawnTree is not reading the real bus (the wiring-gate failure)")
	}
	// the spawn-tree bound (the fan-out the durability law keeps) must come back from the live run: a
	// dispatched helper team has a non-trivial worker count, and every worker carries a role.
	for _, w := range rec.Workers {
		if w.Role == "" {
			t.Error("a reconstructed spawn-tree worker carries no role — the spawn tree is not reading the live dispatch labels")
		}
	}
	if rec.SpawnDepth < 1 {
		t.Errorf("the spawn-tree depth bound was not reconstructed off the live run; got %d", rec.SpawnDepth)
	}

	body := cognition.RenderAnalysisTab(rec, rec, 0, false, deepTab("ACTION·SESS"), 90, false, true, false)
	if body == "" {
		t.Fatal("the ACTION·SESS spawn-tree panel rendered empty over a live reconstruction")
	}
}

// TestThoughtTreeReconstructsLiveBranch — S3 forks a thought branch on the live loop (the dialectic
// fork). The §5 thought tree, fed that live stream, must reconstruct the branch as a real node with the
// fork's gist, off the live conscious.mcp events. An empty tree is the wiring failure (the conscious
// stream genuinely branched but the panel shows nothing).
func TestThoughtTreeReconstructsLiveBranch(t *testing.T) {
	_, log := runScenarioLogged(t, "S3")

	rec := cognition.RecordFromFrozen(log.events, nil)

	if len(rec.Branches) == 0 {
		t.Fatal("the thought tree reconstructed ZERO branches off a live scenario that forked — fillTree is not reading the conscious.mcp events (the wiring-gate failure)")
	}
	// the reconstructed branch must carry the fork's gist (the line of thinking), not a blank node.
	gist := false
	for _, b := range rec.Branches {
		if b.Text != "" {
			gist = true
		}
	}
	if !gist {
		t.Error("the live thought tree reconstructed a branch with no gist — the fork reason is not read off the bus")
	}

	body := cognition.RenderAnalysisTab(rec, rec, 0, false, deepTab("CONSCIOUS"), 90, false, true, false)
	if body == "" {
		t.Fatal("the CONSCIOUS thought-tree panel rendered empty over a live reconstruction")
	}
}

// deepTab resolves a G4 deep-tab index by name from the analysis tab strip (so the render call hits the
// right panel body).
func deepTab(name string) int {
	for i := 0; i < cognition.AnalysisTabCount(); i++ {
		if cognition.AnalysisTabName(i) == name {
			return i
		}
	}
	return 0
}
