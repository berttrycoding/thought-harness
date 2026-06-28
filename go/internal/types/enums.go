// Package types holds the symbolic types shared across the architecture — the nine enums,
// the core domain structs, and the small text helpers. Centralised here (rather than in the
// owning modules) so every layer imports them without circular dependencies. Ported from the
// (now-removed) Python thought_harness/types.py.
//
// Each enum ships a name<->value table pair + String() + Parse<Enum>, because Go iota enums
// have no .name / Enum[str] round-trip and that round-trip is load-bearing: the Controller's
// _llm_decide does Decision[choice], and appraisal/cogngraph re-parse Verdict.name /
// Source.name strings out of Event.data. iota starts at 1 (the `_ = iota` blank) so the
// ordinal values match Python's auto() (1-based).
package types

// ============================================================================
// Source — how a thought entered the stream.
// ============================================================================

// Source records a thought's provenance and which seam it crossed.
type Source int

const (
	_           Source = iota // skip 0 so values are 1-based, matching Python auto()
	INJECTED                  // a Subconscious specialist fired, re-voiced through the hidden seam
	GENERATED                 // Conscious's own serial effort (the "stuck / working it out" path)
	OBSERVATION               // reality feedback returned through the Action layer (ground truth)
	USER_INPUT                // unsolicited external input via the Interaction Port (interrupt)
	METACOG                   // produced by a Thought-MCP operation (branch/merge/rerank/...)
	PERCEPT                   // continuous-mode afferent percept (generalises USER_INPUT)
)

var sourceNames = map[Source]string{
	INJECTED: "INJECTED", GENERATED: "GENERATED", OBSERVATION: "OBSERVATION",
	USER_INPUT: "USER_INPUT", METACOG: "METACOG", PERCEPT: "PERCEPT",
}
var sourceByName = invert(sourceNames)

// String returns the member name (e.g. "INJECTED"), matching Python Source.NAME.name.
func (s Source) String() string { return nameOf(sourceNames, int(s), "Source") }

// ParseSource maps a member name back to its value; ok=false for an unknown name
// (the Go form of Python's Source[name] with a caught KeyError).
func ParseSource(s string) (Source, bool) { v, ok := sourceByName[s]; return v, ok }

// ============================================================================
// Operator — abstract, domain-general transforms (not domain specialists).
// ============================================================================

type Operator int

const (
	_ Operator = iota
	DECOMPOSE
	VALIDATE
	COMPARE
	GENERALIZE
	ABSTRACT
	SIMULATE
	GENERATE
)

var operatorNames = map[Operator]string{
	DECOMPOSE: "DECOMPOSE", VALIDATE: "VALIDATE", COMPARE: "COMPARE", GENERALIZE: "GENERALIZE",
	ABSTRACT: "ABSTRACT", SIMULATE: "SIMULATE", GENERATE: "GENERATE",
}
var operatorByName = invert(operatorNames)

func (o Operator) String() string             { return nameOf(operatorNames, int(o), "Operator") }
func ParseOperator(s string) (Operator, bool) { v, ok := operatorByName[s]; return v, ok }

// ============================================================================
// Resolution — branch detail level (bounded focus).
// ============================================================================

type Resolution int

const (
	_          Resolution = iota
	EXPANDED              // full detail — only the ACTIVE branch is allowed this
	COMPRESSED            // gist only — every stashed branch (bounded focus, lossy by design)
)

var resolutionNames = map[Resolution]string{EXPANDED: "EXPANDED", COMPRESSED: "COMPRESSED"}
var resolutionByName = invert(resolutionNames)

func (r Resolution) String() string               { return nameOf(resolutionNames, int(r), "Resolution") }
func ParseResolution(s string) (Resolution, bool) { v, ok := resolutionByName[s]; return v, ok }

// ============================================================================
// Status — branch lifecycle.
// ============================================================================

type Status int

const (
	_ Status = iota
	ACTIVE
	STASHED
	DEAD
	MERGED
)

var statusNames = map[Status]string{
	ACTIVE: "ACTIVE", STASHED: "STASHED", DEAD: "DEAD", MERGED: "MERGED",
}
var statusByName = invert(statusNames)

func (s Status) String() string           { return nameOf(statusNames, int(s), "Status") }
func ParseStatus(s string) (Status, bool) { v, ok := statusByName[s]; return v, ok }

// ============================================================================
// Decision — what the Controller (Critic, executive half) decides to do next.
// ============================================================================

type Decision int

const (
	_         Decision = iota
	THINK              // keep going on the current branch
	BRANCH             // fork (conflicting / divergent candidates)
	MERGE              // two branches are the same / should combine
	BACKTRACK          // current branch exhausted -> pop to best stashed sibling
	ACT                // closed loop exhausted -> open the channel to reality
	STOP               // done — the SILENT close (nobody is waiting on this line)
	DELIVER            // done + a user is waiting -> SPEAK the answer across the watched seam (A2)
)

var decisionNames = map[Decision]string{
	THINK: "THINK", BRANCH: "BRANCH", MERGE: "MERGE", BACKTRACK: "BACKTRACK", ACT: "ACT", STOP: "STOP",
	DELIVER: "DELIVER",
}
var decisionByName = invert(decisionNames)

func (d Decision) String() string             { return nameOf(decisionNames, int(d), "Decision") }
func ParseDecision(s string) (Decision, bool) { v, ok := decisionByName[s]; return v, ok }

// ============================================================================
// Verdict — Filter admission outcome (Critic, admission half).
// ============================================================================

type Verdict int

const (
	_ Verdict = iota
	ADMIT
	REJECT
	FLAG // admit, but low-confidence -> likely to trigger BRANCH / ACT
)

var verdictNames = map[Verdict]string{ADMIT: "ADMIT", REJECT: "REJECT", FLAG: "FLAG"}
var verdictByName = invert(verdictNames)

func (v Verdict) String() string            { return nameOf(verdictNames, int(v), "Verdict") }
func ParseVerdict(s string) (Verdict, bool) { val, ok := verdictByName[s]; return val, ok }

// ============================================================================
// StopKind — why an episode terminated.
// ============================================================================

type StopKind int

const (
	_ StopKind = iota
	GOAL_MET
	GIVE_UP
	BLOCKED_REALITY
	BLOCKED_USER
	INTERRUPTED
)

var stopKindNames = map[StopKind]string{
	GOAL_MET: "GOAL_MET", GIVE_UP: "GIVE_UP", BLOCKED_REALITY: "BLOCKED_REALITY",
	BLOCKED_USER: "BLOCKED_USER", INTERRUPTED: "INTERRUPTED",
}
var stopKindByName = invert(stopKindNames)

func (s StopKind) String() string             { return nameOf(stopKindNames, int(s), "StopKind") }
func ParseStopKind(s string) (StopKind, bool) { v, ok := stopKindByName[s]; return v, ok }

// ============================================================================
// SystemState — the lifecycle state machine.
// ============================================================================

type SystemState int

const (
	_                SystemState = iota
	IDLE                         // no goal; background consolidation may run
	S_ACTIVE                     // thinking loop running (S_ prefix: ACTIVE is taken by Status)
	AWAITING_REALITY             // suspended on a FRONT action's feedback
	AWAITING_USER                // turn handed back to the user
	SUSPENDED                    // paused mid-task (interrupt / budget cap), resumable
	DONE                         // episode terminal -> IDLE
)

// systemStateNames maps S_ACTIVE to the wire name "ACTIVE" (Python SystemState.ACTIVE.name)
// — the Go identifier is prefixed only to avoid colliding with Status.ACTIVE in the package
// namespace; the round-trip string stays exactly "ACTIVE".
var systemStateNames = map[SystemState]string{
	IDLE: "IDLE", S_ACTIVE: "ACTIVE", AWAITING_REALITY: "AWAITING_REALITY",
	AWAITING_USER: "AWAITING_USER", SUSPENDED: "SUSPENDED", DONE: "DONE",
}
var systemStateByName = invert(systemStateNames)

func (s SystemState) String() string                { return nameOf(systemStateNames, int(s), "SystemState") }
func ParseSystemState(s string) (SystemState, bool) { v, ok := systemStateByName[s]; return v, ok }

// ============================================================================
// Arousal — continuous-mode wakefulness.
// ============================================================================

type Arousal int

const (
	_      Arousal = iota
	AWAKE          // fast continuous loop, full perception gain
	DROWSY         // slowed loop, dampened perception, more spontaneous
	ASLEEP         // foreground halted; consolidation dominates ("dreaming")
)

var arousalNames = map[Arousal]string{AWAKE: "AWAKE", DROWSY: "DROWSY", ASLEEP: "ASLEEP"}
var arousalByName = invert(arousalNames)

func (a Arousal) String() string            { return nameOf(arousalNames, int(a), "Arousal") }
func ParseArousal(s string) (Arousal, bool) { v, ok := arousalByName[s]; return v, ok }

// ============================================================================
// Shared table helpers.
// ============================================================================

// invert builds the name->value reverse of a value->name table.
func invert[V comparable](m map[V]string) map[string]V {
	out := make(map[string]V, len(m))
	for v, name := range m {
		out[name] = v
	}
	return out
}

// nameOf returns the member name for an int-valued enum, or a Python-repr-style fallback
// for an out-of-range value (e.g. "Source(0)") so a bad value is still printable, never
// silently empty.
func nameOf[K ~int](m map[K]string, v int, enum string) string {
	if name, ok := m[K(v)]; ok {
		return name
	}
	return enum + "(" + itoa(v) + ")"
}

// itoa is a tiny stdlib-free int->string (keeps the leaf types package from importing
// strconv just for enum fallbacks).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
