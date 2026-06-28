package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// dangerousCall is an OS-catastrophic command the content gate refuses (rm -rf of a root path). It
// fires the Evaluate gate BEFORE the tool runs — the canonical safety-block input.
var dangerousCall = action.ToolCall{
	Name: "run_shell",
	Args: map[string]any{"command": "echo cleaning && rm -rf / && echo done"},
}

// newWorkspaceEngine builds a heuristic engine WITH an Action workspace (so buildExecutor wires the
// real sandboxed executor + the gate-aware command content gate) under the given feature config, plus
// a recorder of config.skip components. The workspace is a t.TempDir so the sandbox is real and
// isolated. Returns the engine and a skips getter.
func newWorkspaceEngine(t *testing.T, feat *config.HarnessConfig) (*Engine, func() map[string]int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = t.TempDir()
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.executor == nil {
		t.Fatal("a workspace engine must build a real executor (buildExecutor returned nil)")
	}
	skips := map[string]int{}
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.ConfigSkip {
			if comp, ok := ev.Data["component"].(string); ok {
				skips[comp]++
			}
		}
	})
	return e, func() map[string]int { return skips }
}

// TestSafetyGateOnBlocksDangerousCommand is the gate-ON arm of the safety ablation: with
// action.safety_gate ON (AllOn default), the executor's command content gate refuses a catastrophic
// command — IsError + ErrSafetyBlock + an action.safety_block on the trace (the GateBlocked witness).
// This is the byte-identical default: the wired gate-aware closure behaves exactly like the old
// unconditional DefaultEvaluate when the toggle is ON.
func TestSafetyGateOnBlocksDangerousCommand(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn ⇒ action.safety_gate ON
	var blocked bool
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.ActionSafetyBlock {
			blocked = true
		}
	})

	res := e.executor.Execute(dangerousCall)
	if !res.IsError || res.ErrorCode != action.ErrSafetyBlock {
		t.Fatalf("safety gate ON MUST block the dangerous command: IsError=%v code=%q content=%q",
			res.IsError, res.ErrorCode, res.Content)
	}
	if !blocked {
		t.Fatal("safety gate ON must emit action.safety_block (the GateBlocked witness)")
	}
}

// TestSafetyGateOffAdmitsDangerousCommand is the forced-divergence proof that the action.safety_gate
// toggle is GENUINELY CONSUMED (it was a dead knob — the executor was built with an unconditional
// DefaultEvaluate, so flipping the toggle OFF did nothing and the safety gate-off arm still blocked).
// With action.safety_gate OFF the content gate short-circuits to admit-all: the SAME catastrophic
// command is NOT content-blocked (no ErrSafetyBlock), NO action.safety_block fires (GateBlocked=FALSE),
// and the gate announces its bypass via config.skip("action.safety_gate"). The sandbox is independent
// and stays on — only the content-gate decision is skipped (§4.3 bypass-not-delete).
//
// Together with TestSafetyGateOnBlocksDangerousCommand this is the gate-on − gate-off contrast the
// safety mechanism-lift is attributed to: the block fires under the harness arm and is genuinely absent
// under the gate-off arm.
func TestSafetyGateOffAdmitsDangerousCommand(t *testing.T) {
	feat := config.New()
	feat.Action.SafetyGate = false // the single ablation toggle for the safety mechanism
	feat.Validate()

	e, getSkips := newWorkspaceEngine(t, feat)
	var blocked bool
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.ActionSafetyBlock {
			blocked = true
		}
	})

	res := e.executor.Execute(dangerousCall)
	// The content gate is bypassed, so the command is NOT safety-blocked. (It is NOT executed for real
	// either — the sandbox still confines it to the workspace; the point is the absence of the CONTENT
	// block, which is what the safety witness reads.)
	if res.IsError && res.ErrorCode == action.ErrSafetyBlock {
		t.Fatalf("safety gate OFF must NOT content-block the command, but it did: %q", res.Content)
	}
	if blocked {
		t.Fatal("safety gate OFF must emit NO action.safety_block (GateBlocked must be FALSE)")
	}
	if getSkips()["action.safety_gate"] == 0 {
		t.Fatal("safety gate OFF must announce its bypass via config.skip(action.safety_gate)")
	}
}

// TestSafetyGateLiveToggle confirms the safety-gate closure reads the LIVE toggle each call (the
// shared-pointer contract): the SAME executor blocks while ON and admits after a live flip OFF, with
// no executor rebuild. This is what lets a TUI flip of action.safety_gate take effect on the next act.
func TestSafetyGateLiveToggle(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn

	if res := e.executor.Execute(dangerousCall); res.ErrorCode != action.ErrSafetyBlock {
		t.Fatalf("safety gate ON must block (pre-flip): code=%q", res.ErrorCode)
	}
	if !e.ApplyFeatureToggle("action.safety_gate", false) {
		t.Fatal("ApplyFeatureToggle(action.safety_gate,false) should succeed")
	}
	if res := e.executor.Execute(dangerousCall); res.IsError && res.ErrorCode == action.ErrSafetyBlock {
		t.Fatalf("after a live flip OFF the SAME executor must admit the command (no rebuild): %q", res.Content)
	}
}
