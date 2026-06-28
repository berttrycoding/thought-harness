// executor.go — the ToolExecutor, the fixed gate pipeline every effect passes through (ported from
// action/executor.py, itself a port of lathe's tools/executor.go Executor.Execute).
//
// Order, all BEFORE the effect runs:
//
//  1. resolve the tool (unknown -> ErrUnknownTool)
//  2. block-list (a tool excluded at execution level, e.g. the orchestrator may not write) -> ErrBlocked
//  3. sandbox (file-modifying tools): the path must resolve inside the sandbox -> ErrSandboxDeny
//  4. evaluate (command tools): catastrophic commands are blocked -> ErrSafetyBlock
//  5. approve (optional human/policy callback) -> ErrBlocked
//  6. execute the tool (the only place a real effect happens)
//
// Each gate decision and the final dispatch emit a structured Event on the bus (emit-never-print):
// events.ActionTool (dispatched), events.ActionSandboxDeny, events.ActionSafetyBlock,
// events.ActionBlocked. A denied or unknown call returns a structured ToolResult — the executor
// never panics for a gate decision, so the cognition layer reads the denial as just another
// observation.
//
// This is why the action subtree imports internal/events: the executor (and only the executor in
// this subtree) emits, so it needs the Kind constants. The arrow still points the right way —
// events is the leaf both the engine and the action layer depend on; no cognition import appears.
package action

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// EvaluateFn gates a command tool: it returns a non-empty reason to BLOCK, or "" (Python None) to
// allow. Mirrors Python's `evaluate(tool_name, command) -> str | None`. A nil field turns the gate
// off (Python's `evaluate=None`); DefaultEvaluate wires it to EvaluateCommand.
type EvaluateFn func(toolName, command string) string

// ApproveFn is the optional policy/human callback: it returns true to allow the call. Mirrors
// Python's `approve(tool_name, args) -> bool`. A nil field turns the gate off (Python's
// `approve=None` — auto-approved), exactly as a dispatched sub-agent runs without a human gate.
type ApproveFn func(toolName string, args map[string]any) bool

// DefaultEvaluate is the package default for the evaluate gate (Python's _default_evaluate): it
// ignores the tool name and runs the content gate over the command. Pass it explicitly when
// building a ToolExecutor that should block catastrophic commands.
func DefaultEvaluate(toolName, command string) string { return EvaluateCommand(command) }

// ToolExecutor runs a tool through the fixed gate pipeline. It holds the registry it resolves
// against, the optional sandbox/evaluate/approve gates, an execution-level block set, and the emit
// closure each gate reports through. A nil sandbox, nil evaluate, or nil approve each turns that
// single gate off independently.
type ToolExecutor struct {
	registry *ToolRegistry
	sandbox  *Sandbox
	evaluate EvaluateFn
	approve  ApproveFn
	blocked  map[string]bool
	emit     events.Emit // may be nil (the gate events are then skipped)
	// bounds is the gate-router's conscious-set ceiling (03 §3). Non-nil ENABLES the router stage: every
	// call is classified by its (operation x reach) taxonomy and gated — a world-change needs conscious
	// authoring, a distal sense is budgeted, a self-substrate mutate is refused outright (§4). nil ⇒ the
	// router stage is OFF (today's pipeline, byte-identical). Set by the engine when action.gate_router is on.
	bounds *RouteBounds
	// autoPerm is the tiered AUTO-PERMISSION policy (autopermission.go, action.auto_permission). Non-nil
	// ENABLES the auto-permission stage as the FIRST gate: every call is classified SAFE ⇒ self-authorized
	// (no human prompt, emits action.auto_approve) or DANGEROUS ⇒ escalated (deny + action.escalate in a
	// headless/autonomous context). This is what lets autonomous/awake mode act without per-call approval
	// while the sandbox + downstream gates confine it. nil ⇒ the stage is OFF (today's pipeline,
	// byte-identical). Set by the engine when action.auto_permission is on.
	autoPerm *AutoPermissionPolicy
}

// ExecutorOptions configures a ToolExecutor at construction (Python's keyword-only __init__ args).
// All fields are optional: a nil Sandbox/Approve turns that gate off; a nil Evaluate turns the
// command gate off (pass DefaultEvaluate to enable it, matching Python's evaluate=_default_evaluate
// default); a nil/empty Blocked is an empty set; a nil Emit skips the gate events.
type ExecutorOptions struct {
	Sandbox  *Sandbox
	Evaluate EvaluateFn
	Approve  ApproveFn
	Blocked  []string
	Emit     events.Emit
	// Bounds, when non-nil, enables the gate-router stage with these conscious-set ceilings (03 §3).
	// nil (the default) leaves the router off — the pipeline is byte-identical to before.
	Bounds *RouteBounds
	// AutoPerm, when non-nil, enables the tiered auto-permission stage (autopermission.go). nil (the
	// default) leaves it off — the pipeline is byte-identical to before.
	AutoPerm *AutoPermissionPolicy
}

// NewToolExecutor builds a ToolExecutor over a registry with the given options. A nil opts is the
// all-gates-permissive executor (no sandbox, no command gate, no approval, no block list).
//
// NOTE: unlike Python — whose `evaluate` defaults to _default_evaluate — the Go default is a nil
// Evaluate (gate off), because a Go zero-value field cannot encode "defaulted vs explicitly None".
// Callers that want the command gate pass Evaluate: DefaultEvaluate explicitly. Scoped() preserves
// whatever the parent had, so the engine sets it once at the root and every sub-agent inherits it.
func NewToolExecutor(registry *ToolRegistry, opts *ExecutorOptions) *ToolExecutor {
	e := &ToolExecutor{registry: registry, blocked: map[string]bool{}}
	if opts != nil {
		e.sandbox = opts.Sandbox
		e.evaluate = opts.Evaluate
		e.approve = opts.Approve
		e.emit = opts.Emit
		e.bounds = opts.Bounds
		e.autoPerm = opts.AutoPerm
		for _, n := range opts.Blocked {
			e.blocked[n] = true
		}
	}
	return e
}

// Registry exposes the tool registry this executor resolves against, read-only — the TUI registry
// browser lists the available tools (name + description). The returned pointer is the executor's own
// registry; callers must treat it as read-only (the registry's mutators are not part of this contract).
func (e *ToolExecutor) Registry() *ToolRegistry { return e.registry }

// --- helpers -------------------------------------------------------------------------------------

// pathArg reads the "path" arg as a trimmed string (Python's _path_arg: str(args.get("path","")).strip()).
// argStr lives in util.go; the .strip() is applied here.
func pathArg(args map[string]any) string { return strings.TrimSpace(argStr(args, "path")) }

// commandArg reads the "command" arg as a trimmed string (Python's _command_arg). run_shell carries
// "command"; other command tools have no injectable shell surface.
func commandArg(args map[string]any) string { return strings.TrimSpace(argStr(args, "command")) }

// deny emits the gate event and returns the structured denial ToolResult. It never panics — a gate
// decision is an observation, not an exception. Mirrors Python's _deny: the event summary is
// "<name>: <reason>" with {tool, reason, denied:true}; the result content is "[<code>] <reason>".
func (e *ToolExecutor) deny(call ToolCall, code, reason, event string) ToolResult {
	if e.emit != nil {
		e.emit(event, call.Name+": "+reason, events.D{"tool": call.Name, "reason": reason, "denied": true})
	}
	return ToolResult{
		Name:      call.Name,
		Content:   "[" + code + "] " + reason,
		IsError:   true,
		ErrorCode: code,
		CallID:    call.ID,
	}
}

// --- least-privilege view for a dispatched sub-agent ---------------------------------------------

// Scoped returns a least-privilege executor exposing ONLY the named tools (lathe's per-sub-agent
// tool filtering). A dispatched sub-agent is trusted to RUN its scoped tools — approve is dropped
// (auto-approved, no human gate, matching Python's approve=None) — but is NOT trusted to escape:
// the same sandbox and command evaluate gates still fire on every effect. Tools outside the scope
// are simply absent from the new registry (an out-of-scope call returns ErrUnknownTool), so the
// scope is the privilege boundary. The emit closure is shared so the sub-agent's effects stay
// visible on the same bus.
func (e *ToolExecutor) Scoped(names []string) *ToolExecutor {
	sub := make([]Tool, 0, len(names))
	for _, n := range names {
		if t, ok := e.registry.Get(n); ok {
			sub = append(sub, t)
		}
	}
	return &ToolExecutor{
		registry: NewToolRegistry(sub),
		sandbox:  e.sandbox,
		evaluate: e.evaluate,
		approve:  nil, // auto-approved (Python scoped(): approve=None)
		blocked:  map[string]bool{},
		emit:     e.emit,
		bounds:   e.bounds,   // a sub-agent inherits the conscious-set ceiling (the router gate still fires)
		autoPerm: e.autoPerm, // a sub-agent inherits the auto-permission policy (the tier gate still fires)
	}
}

// --- the pipeline --------------------------------------------------------------------------------

// Execute runs call through the fixed gate pipeline and returns a structured ToolResult. Every exit
// path is a ToolResult: a gate denial is the world refusing the effect, read by the cognition layer
// as an observation, never an error/panic.
func (e *ToolExecutor) Execute(call ToolCall) ToolResult {
	tool, ok := e.registry.Get(call.Name)
	if !ok {
		return e.deny(call, ErrUnknownTool, "unknown tool: "+call.Name, events.ActionBlocked)
	}

	if e.blocked[call.Name] {
		return e.deny(call, ErrBlocked,
			call.Name+" is not available here; delegate to a sub-agent", events.ActionBlocked)
	}

	// Auto-permission stage (autopermission.go, action.auto_permission) — ENABLED only when autoPerm
	// is set. The FIRST gate: classify the call SAFE/DANGEROUS, removing the human from the per-call
	// approval loop while the sandbox + downstream gates confine it.
	//   - SAFE (read-only / in-jail write / allowlisted, in-jail) ⇒ AUTO-APPROVE: emit
	//     action.auto_approve and FALL THROUGH to the concrete gates (which still fire — auto-approval
	//     is not a bypass; it self-authorizes the human-approval step only);
	//   - DANGEROUS (irreversible / out-of-jail / non-allowlisted / destructive) ⇒ ESCALATE: in this
	//     headless/autonomous context = DENY + emit action.escalate for later human / higher-autonomy
	//     review.
	// nil autoPerm ⇒ this whole stage is skipped (byte-identical to today).
	if e.autoPerm != nil {
		tc := classifyTool(tool)
		pd := e.autoPerm.ClassifyPermission(call, tc)
		if pd.Tier == PermDangerous {
			if e.emit != nil {
				e.emit(events.ActionEscalate, call.Name+": "+pd.Reason,
					events.D{"tool": call.Name, "tier": pd.Tier.String(), "reason": pd.Reason, "denied": true})
			}
			return ToolResult{
				Name:      call.Name,
				Content:   "[" + ErrBlocked + "] escalated for review: " + pd.Reason,
				IsError:   true,
				ErrorCode: ErrBlocked,
				CallID:    call.ID,
			}
		}
		if e.emit != nil {
			e.emit(events.ActionAutoApprove, call.Name+": "+pd.Reason,
				events.D{"tool": call.Name, "tier": pd.Tier.String(), "reason": pd.Reason})
		}
	}

	// Gate-router stage (03 §3, action.gate_router) — ENABLED only when bounds is set. Classify the call
	// by its (operation x reach) taxonomy and apply the routing discipline BEFORE the concrete gates:
	//   - a self-substrate mutate is REFUSED outright (§4 invariant — the hardest stop);
	//   - a world-change needs the conscious to have AUTHORED the act (else deny);
	//   - a distal sense with the network policy off / quota spent is declined (offline-safe).
	// A local sense (read / in-sandbox run) routes free. nil bounds ⇒ this whole stage is skipped.
	if e.bounds != nil {
		tc := classifyTool(tool) // gap 6: route on the tool's OWN category tag (not a name-switch guess)
		// PROTECTED CORE (01 §2.8, anti-wireheading): the immutable kernel is read-only to EVERY loop —
		// checked first so a core target gets the distinct anti-wireheading reason, not the generic §4 one.
		if RefuseProtectedCoreMutation(tc.Op, pathArg(call.Args)) {
			return e.deny(call, ErrBlocked,
				"refused: the protected core is immutable (anti-wireheading, 01 §2.8)", events.ActionBlocked)
		}
		if RefuseSelfMutation(tc.Op, pathArg(call.Args)) {
			return e.deny(call, ErrBlocked,
				"refused: an action tool may not target the self-substrate (03 §4)", events.ActionBlocked)
		}
		dec := Route(tc, *e.bounds)
		if dec.NeedsConsciousAuthor && !call.Authored {
			return e.deny(call, ErrBlocked,
				"world-change requires conscious authorization (03 §3)", events.ActionBlocked)
		}
		if dec.QuotaExceeded {
			return e.deny(call, ErrBlocked,
				"distal sense declined: network policy off / quota spent (offline fallback, 03 §7)", events.ActionBlocked)
		}
	}

	// Sandbox gate — file-modifying tools only. Hard block.
	if e.sandbox != nil && FileModifyTools[call.Name] {
		path := pathArg(call.Args)
		if path != "" {
			// Resolve a relative path against the tool's own workdir so the sandbox checks the path
			// the tool will ACTUALLY touch (not one resolved against the process CWD). Python:
			// getattr(tool, "workdir", None) -> the workdirer optional facet here.
			resolved := path
			if !filepath.IsAbs(path) {
				if w, isW := tool.(workdirer); isW {
					if wd := w.Workdir(); wd != "" {
						resolved = filepath.Join(wd, path)
					}
				}
			}
			if reason := e.sandbox.Check(resolved); reason != "" {
				return e.deny(call, ErrSandboxDeny, reason, events.ActionSandboxDeny)
			}
		}
	}

	// Evaluate gate — command tools only. Hard block, no interaction.
	if e.evaluate != nil && CommandTools[call.Name] {
		command := commandArg(call.Args)
		if command != "" {
			if reason := e.evaluate(call.Name, command); reason != "" {
				return e.deny(call, ErrSafetyBlock, reason, events.ActionSafetyBlock)
			}
		}
	}

	// Approval gate — optional policy/human callback.
	if e.approve != nil && !e.approve(call.Name, call.Args) {
		return e.deny(call, ErrBlocked, "denied by approval policy", events.ActionBlocked)
	}

	// The effect — the only place a real effect happens.
	result := tool.Execute(call.Args)

	summary := call.Name + " -> "
	if result.IsError {
		summary += "ERROR"
	} else {
		summary += "ok"
	}
	if result.ExitCode != nil {
		summary += " (exit " + strconv.Itoa(*result.ExitCode) + ")"
	}
	if e.emit != nil {
		e.emit(events.ActionTool, summary, events.D{
			"tool":       call.Name,
			"ok":         !result.IsError,
			"exit_code":  result.ExitCode, // nil == Python None
			"error_code": result.ErrorCode,
		})
	}
	return ToolResult{
		Name:      call.Name,
		Content:   result.Content,
		IsError:   result.IsError,
		ErrorCode: result.ErrorCode,
		ExitCode:  result.ExitCode,
		CallID:    call.ID,
	}
}
