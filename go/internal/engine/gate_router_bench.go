package engine

import (
	"path/filepath"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// gate_router_bench.go is the OFFLINE wiring-assertion driver for action.gate_router. The
// gaterouterbench package scores the REAL action.ToolExecutor.Execute pipeline directly (the gate
// mechanism), but the executor only RECEIVES the conscious-set ceiling (RouteBounds) when the engine
// wires it — config.Action.GateRouter ON => buildExecutor sets bounds (engine.go:507-509). This driver
// proves that wiring end-to-end: it builds a REAL engine with a workspace and the flag OFF/ON, then runs
// an UNAUTHORED world-change through the engine's OWN executor and reports whether the gate refused it.
//
// It is additive: a fresh engine on the offline test double, no live loop, no default-path change. It is
// what catches the "flag flips but the engine never passes Bounds to the executor" mutation — the gate
// could be perfect while the wiring is dead, and a pure action-package bench would not see it.

// GateRouterEngineRefusesUnauthored builds a real engine with action.gate_router == on (and a real
// workspace, so buildExecutor produces a live executor) and runs an UNAUTHORED write through the
// engine's own executor. It returns refused=true iff the engine's executor blocked the unauthored
// world-change (the gate-router stage fired), and a build error if the engine could not be constructed.
//
// Expected: on==false -> refused==false (router off, the write would route through — byte-identical);
// on==true -> refused==true (the wired router refuses the unauthored world-change). workspaceDir is a
// real (e.g. t.TempDir()) directory so the write target resolves in-sandbox and only the router gates.
func GateRouterEngineRefusesUnauthored(on bool, workspaceDir string) (refused bool, err error) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = workspaceDir // non-empty -> buildExecutor builds a live executor
	feat := config.New()         // AllOn baseline
	feat.Action.GateRouter = on
	feat.Action.Tools = true
	cfg.Features = feat

	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		return false, err
	}
	if e.executor == nil {
		return false, errNoExecutor
	}

	// An UNAUTHORED world-change to an in-scope (in-sandbox) path: only the gate-router can refuse it.
	// (The sandbox would allow it — the target is inside the workspace — so a refusal is the router's.)
	target := filepath.Join(workspaceDir, "out.txt")
	res := e.executor.Execute(action.ToolCall{
		Name:     "write_file",
		Args:     map[string]any{"path": target, "content": "x"},
		Authored: false,
	})
	return res.IsError && res.ErrorCode == action.ErrBlocked, nil
}

// errNoExecutor is returned when the engine built no executor (no workspace) — a configuration error the
// caller surfaces rather than a silent false.
var errNoExecutor = errNoExec("engine built no executor (workspace missing)")

type errNoExec string

func (e errNoExec) Error() string { return string(e) }
