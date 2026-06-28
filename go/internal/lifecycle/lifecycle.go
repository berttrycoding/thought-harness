// Package lifecycle is the system lifecycle state machine (spec §5.5).
//
// Stopping is terminal (DONE) or suspended (resumable). The engine drives transitions; this
// records them, emits, enforces the stopping taxonomy, and exposes the partial-idle composite.
//
// A faithful port of the Python thought_harness/lifecycle.py. It depends only on
// internal/types (the SystemState/StopKind enums) and internal/events (the emit closure +
// the Lifecycle kind). The state machine is PURE — it does no I/O, only emits; the engine
// owns the clock.
package lifecycle

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// legal is the legal-transition table (advisory — illegal transitions are emitted as
// warnings with legal=false, NOT hard errors). Package-level so it is built once and shared,
// matching Python's module-level _LEGAL dict. The inner set is a map[...]struct{} (Python's
// set membership -> Go map lookup).
//
// Note types.S_ACTIVE is the Go identifier for SystemState.ACTIVE (the wire name stays
// "ACTIVE"; the S_ prefix only avoids colliding with Status.ACTIVE in the types package).
var legal = map[types.SystemState]map[types.SystemState]struct{}{
	types.IDLE: {types.S_ACTIVE: {}},
	types.S_ACTIVE: {
		types.AWAITING_REALITY: {}, types.AWAITING_USER: {}, types.SUSPENDED: {},
		types.DONE: {}, types.S_ACTIVE: {},
	},
	types.AWAITING_REALITY: {types.S_ACTIVE: {}},
	types.AWAITING_USER:    {types.S_ACTIVE: {}, types.IDLE: {}},
	types.SUSPENDED:        {types.S_ACTIVE: {}},
	types.DONE:             {types.IDLE: {}, types.AWAITING_USER: {}},
}

// stopTarget is the stopping taxonomy: stop kind -> lifecycle target. GOAL_MET/GIVE_UP are
// terminal -> DONE; the others are suspended/resumable. Package-level (Python's module-level
// _STOP_TARGET dict).
var stopTarget = map[types.StopKind]types.SystemState{
	types.GOAL_MET:        types.DONE,
	types.GIVE_UP:         types.DONE,
	types.BLOCKED_REALITY: types.AWAITING_REALITY,
	types.BLOCKED_USER:    types.AWAITING_USER,
	types.INTERRUPTED:     types.SUSPENDED,
}

// HistoryEntry is one (state, reason) record in the transition log — the Go form of Python's
// list[tuple[SystemState, str]].
type HistoryEntry struct {
	State  types.SystemState
	Reason string
}

// Lifecycle is the system state machine. Construct it with New; the engine drives Transition
// and Stop. It holds an emit closure (never the Bus directly), matching every other component.
type Lifecycle struct {
	emit    events.Emit
	State   types.SystemState
	History []HistoryEntry
}

// New builds a Lifecycle in the given initial state (Python's default SystemState.IDLE — use
// NewDefault for that). The history is seeded with the (state, "init") entry, exactly as
// Python's __init__.
func New(emit events.Emit, state types.SystemState) *Lifecycle {
	return &Lifecycle{
		emit:    emit,
		State:   state,
		History: []HistoryEntry{{State: state, Reason: "init"}},
	}
}

// NewDefault builds a Lifecycle starting in IDLE, matching Python's
// Lifecycle(emit, state=SystemState.IDLE) default.
func NewDefault(emit events.Emit) *Lifecycle { return New(emit, types.IDLE) }

// Transition moves to a new state, recording it, emitting the transition, and stamping the
// advisory legality. A no-op self-transition (to == current) returns early WITHOUT recording
// or emitting (Python's `if to is self.state: return`). The legal flag is advisory only: an
// illegal transition still happens and is recorded — it is surfaced as legal=false on the
// event, never refused.
func (l *Lifecycle) Transition(to types.SystemState, reason string) {
	if to == l.State {
		return
	}
	_, isLegal := legal[l.State][to]
	l.History = append(l.History, HistoryEntry{State: to, Reason: reason})
	l.emit(events.Lifecycle, l.State.String()+" -> "+to.String()+" ("+reason+")",
		events.D{"frm": l.State.String(), "to": to.String(), "reason": reason, "legal": isLegal})
	l.State = to
}

// Stop applies the stopping taxonomy: it maps the StopKind to its lifecycle target and
// transitions there with a "<KIND>: <reason>" reason, returning the target. Mirrors Python's
// stop(kind, reason). The kind is always present in stopTarget (the StopKind enum is closed),
// so no missing-key path exists.
func (l *Lifecycle) Stop(kind types.StopKind, reason string) types.SystemState {
	target := stopTarget[kind]
	l.Transition(target, kind.String()+": "+reason)
	return target
}

// CompositeIdle reports the partial-idle composite label — a derived read of the four
// component-idle flags (a pure, static helper; Python's @staticmethod composite_idle). The
// branch order is load-bearing and matches Python exactly.
func CompositeIdle(middleIdle, frontIdle, backConsolidating, pendingInput bool) string {
	switch {
	case pendingInput && middleIdle && frontIdle:
		return "transitioning -> ACTIVE"
	case middleIdle && frontIdle && backConsolidating:
		return "IDLE (background consolidating)"
	case middleIdle && frontIdle:
		return "FULLY IDLE"
	case middleIdle && !frontIdle:
		return "AWAITING_REALITY"
	default:
		return "ACTIVE"
	}
}
