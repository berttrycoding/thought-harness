package engine

import (
	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// auto_permission_driver.go is the OFFLINE wiring-assertion driver for action.auto_permission, the
// twin of gate_router_bench.go. It proves the SECURITY-SANDBOX cognition end-to-end on the engine's
// OWN executor: with the flag ON the live executor classifies each tool call SAFE (auto-approved +
// runs, emitting action.auto_approve) or DANGEROUS (denied + escalated, emitting action.escalate),
// removing the human from the per-call approval loop while the sandbox confines it. It is additive
// (a fresh engine on the offline test double, no live loop, no default-path change) — what catches a
// "flag flips but the engine never passes the policy to the executor" wiring death.

// AutoPermissionResult is the outcome of running ONE tool call through the engine's executor with the
// auto-permission flag set: whether the tool actually RAN, whether it was DENIED, and which
// auto-permission event (if any) the engine emitted.
type AutoPermissionResult struct {
	Ran          bool // the underlying tool effect happened (a real write landed)
	Denied       bool // the executor returned a blocked/escalated denial (the tool did not run)
	AutoApproved bool // action.auto_approve was emitted (a SAFE call self-authorized, no human prompt)
	Escalated    bool // action.escalate was emitted (a DANGEROUS call deferred to review)
}

// AutoPermissionEngineDecision builds a REAL engine with action.auto_permission == on (and a real
// workspace, so buildExecutor produces a live executor jailed to it) and runs the given call through
// the engine's OWN executor, capturing the emitted auto-permission events. It is the engine-level
// witness that the policy is WIRED: a SAFE in-jail call auto-approves + runs; a DANGEROUS out-of-jail
// / non-allowlisted call escalates + is denied. With on==false the stage is inert (byte-identical).
func AutoPermissionEngineDecision(on bool, workspaceDir string, call action.ToolCall) (AutoPermissionResult, error) {
	return AutoPermissionEngineDecisionWith(on, workspaceDir, "", "", call)
}

// AutoPermissionEngineDecisionWith is the EXTENDED witness for the SECURITY-SANDBOX follow-up: it
// builds a real engine with action.auto_permission == on AND the two extension points configured
// (the per-workspace EXTENSIBLE-allowlist config file + the comma-separated PRE-AUTHORIZATION grant
// list), then runs the call through the engine's OWN executor. It is the engine-level proof that the
// extensions are WIRED, not just constructed in a unit: with "go run" granted the live executor
// auto-approves it; with no grant it escalates — exactly the slice's promotion claim. With on==false
// the whole stage is inert (byte-identical), and an empty configFile + empty preAuth reproduce the
// curated-seed floor (so AutoPermissionEngineDecision delegates here with both empty).
func AutoPermissionEngineDecisionWith(on bool, workspaceDir, configFile, preAuth string, call action.ToolCall) (AutoPermissionResult, error) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = workspaceDir
	feat := config.New() // AllOn baseline
	feat.Action.AutoPermission = on
	feat.Action.AutoPermissionConfigFile = configFile
	feat.Action.AutoPermissionPreAuth = preAuth
	feat.Action.Tools = true
	cfg.Features = feat

	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		return AutoPermissionResult{}, err
	}
	if e.executor == nil {
		return AutoPermissionResult{}, errNoExecutor
	}

	var res AutoPermissionResult
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case string(events.ActionAutoApprove):
			res.AutoApproved = true
		case string(events.ActionEscalate):
			res.Escalated = true
		}
	})

	out := e.executor.Execute(call)
	res.Denied = out.IsError && out.ErrorCode == action.ErrBlocked
	res.Ran = !out.IsError
	return res, nil
}
