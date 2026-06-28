// Package action is the effect layer — the reality-facing surface of the harness.
//
// It is the Tier-0/1/2 "lathe-derived effect layer" and is deliberately self-contained: it
// imports only stdlib + internal/types + internal/events (for the Kind constants the executor
// emits). It NEVER imports a cognition package — the dependency arrow points the other way (the
// cognition layer reads a tool result as an observation). This keeps the action subtree a leaf
// the rest of the tree can depend on without a cycle.
//
// tool.go is the Tier-0 contract (ported from action/tools.py, itself a port of lathe's
// pkg/api/tool/tool.go): a Tool is a name + description + JSON-shape parameters + an
// Execute(args) -> ToolResult. The harness owns the wire format; a tool receives already-parsed
// args (a map) and returns a structured ToolResult — it never returns an error for an EXPECTED
// failure (a non-zero exit, a missing file, a denied path are all results, not errors), so the
// cognition layer can read them as observations.
package action

// ErrorCode groups the structured error codes carried on a failed ToolResult (lathe's Err*
// prefixes). They let the cognition layer distinguish KINDS of failure: a real test failure (the
// world refuting a guess) is NOT an error code — it is a successful observation with IsError set
// because the exit code was non-zero. An error code marks that the tool could not run at all.
//
// Python carried these as class attributes on ErrorCode; Go uses package-level string consts (a
// named-string vocabulary), matching the wire values exactly.
const (
	ErrUnknownTool = "unknown_tool"
	ErrBadArgs     = "bad_args"
	ErrSandboxDeny = "sandbox_deny"
	ErrSafetyBlock = "safety_block"
	ErrBlocked     = "blocked"     // excluded at execution level (e.g. orchestrator may not write)
	ErrTimeout     = "timeout"     //
	ErrUnavailable = "unavailable" // a dependency (binary, venv) is missing
)

// ToolCall is a request to run a tool: its name + parsed args + a correlation id. Python's
// frozen dataclass becomes a Go value struct (passed/copied by value, never mutated).
type ToolCall struct {
	Name string
	Args map[string]any // Python field(default_factory=dict) — allocate per instance
	ID   string
	// Authored marks that the CONSCIOUS authored this act (03 §3/§5): a world-change tool requires it.
	// The gate-router (action.gate_router, off by default) refuses an unauthored world-change. A read /
	// sense call is unaffected (no authoring needed). Default false — the reactive ACT path sets it true.
	Authored bool
}

// ToolResult is the outcome of a tool call. Content is the real captured output (stdout+stderr,
// the file bytes, the search hits). IsError is true when the effect did not succeed — either the
// tool could not run (ErrorCode set) or the underlying command exited non-zero (a genuine reality
// signal: ErrorCode empty, ExitCode carries the real code).
//
// Python's frozen dataclass becomes a Go value struct. ExitCode is *int so the Python `int | None`
// "no exit code recorded" state round-trips (nil == Python None).
type ToolResult struct {
	Name      string
	Content   string
	IsError   bool
	ErrorCode string
	ExitCode  *int // nil == Python None
	CallID    string
}

// Tool is the contract every registered tool implements. Concrete tools live in builtins.go.
//
// Python's runtime_checkable Protocol becomes a Go interface. The base method set is name/description/
// parameters/execute (the executor's gate pipeline, the registry's deterministic ordering, and the
// model-facing schema all read through this surface).
//
// Category() (cognition redesign §3.9/§3.10a, gap 6) is the tool-OWNED two-axis taxonomy tag
// (operation x reach) — replacing the two name->category GUESS switches (subagent.go toolCategory +
// gateroute.go classifyCall) with a tag the tool itself declares. The subconscious category-scope
// (§3.3a Scope) matches on the operation; the action gate-router routes on (operation x reach). A tool
// is the owner of its own taxonomy, so a new tool ships its category rather than being guessed at by a
// hardcoded switch (§3.10a: "Categories are their own registry … not a hardcoded enum").
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(args map[string]any) ToolResult
	// Category returns the tool's (operation, reach) taxonomy tag — the §3.10a category wire strings
	// "inspect|mutate|execute" x "self|local|external". The Scope ceiling (§3.3a) matches on operation;
	// the gate-router (Route) routes on the pair. Owner-of-record is the eval.CategoryRegistry seed set,
	// derived in lock-step from these axes (eval/category.go seeds from the action taxonomy).
	Category() TaxClass
}

// workdirer is the optional facet the executor's sandbox gate probes (Python's
// `getattr(tool, "workdir", None)`): a file-modifying tool exposes the workspace root its relative
// paths resolve against, so the sandbox can check the path the tool will ACTUALLY touch rather
// than one resolved against the process CWD. The builtins satisfy it; the executor (Tier 2)
// asserts it with `if w, ok := tool.(workdirer); ok`. Defined here so both sides share the shape.
type workdirer interface{ Workdir() string }
