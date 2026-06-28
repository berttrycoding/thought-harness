// Package session holds the Session/long-horizon-agent lifecycle declared at the spec level
// (workflow-session-architecture §3b / P3.8). Today every sub-agent is single-shot; the lifecycle QUAD —
// horizon × schedule × state × terminate — makes the other settings expressible (heartbeat monitors,
// async actors, continuous watchers), all scheduled in deterministic TICKS (never wall-clock) and all
// GUARANTEED to terminate. The load-bearing durability rule: a long-horizon agent is a standing fork
// that consumes schedulability every tick it lives, so it MUST carry a tick budget + at least one
// terminate condition — an immortal agent is a durability hazard (a μ that never decays).
package session

import "fmt"

// Horizon — how long a session lives.
type Horizon int

const (
	SingleShot  Horizon = iota // one move, then gone (today's sub-agent)
	Bounded                    // runs a whole sub-workflow, then gone
	LongHorizon                // persists across ticks holding state — bounded multi-tick, never unbounded
)

// ScheduleKind — WHEN a session runs.
type ScheduleKind int

const (
	OnDemand   ScheduleKind = iota // instantiated, fires, done
	Heartbeat                      // wakes every Period ticks (a monitor)
	Async                          // fires, polls the result Latency ticks later
	Continuous                     // runs every tick (awake default-mode / perception)
)

// StateKind — what a session remembers (scratch = bounded ctx, NOT the thought graph).
type StateKind int

const (
	Stateless StateKind = iota
	Scratch
	Persistent
)

// Terminate — WHY a session goes away (any one fires).
type Terminate int

const (
	NotTerminated Terminate = iota
	GoalMet
	BudgetExhausted
	Quiescence
	RefutedByReality
	Superseded
	WatchdogTimeout
	ParentEnded
)

func (t Terminate) String() string {
	return [...]string{"not-terminated", "goal_met", "budget_exhausted", "quiescence",
		"refuted_by_reality", "superseded", "watchdog_timeout", "parent_ended"}[t]
}

// Schedule is the WHEN axis with its parameters (Period for heartbeat, Latency for async; both in ticks).
type Schedule struct {
	Kind    ScheduleKind
	Period  int // heartbeat period (ticks); >=1 for Heartbeat
	Latency int // async feedback latency (ticks); >=0 for Async
}

// Spec is a session's lifecycle declaration.
type Spec struct {
	Horizon    Horizon
	Schedule   Schedule
	State      StateKind
	TickBudget int // the HARD termination cap (ticks) — the guaranteed-termination invariant
}

// Valid enforces the durability invariant: any multi-tick (bounded / long_horizon) session, and any
// schedule that keeps waking (heartbeat / async / continuous), MUST carry a positive TickBudget — a
// guaranteed termination. A heartbeat must have a positive period; an async a non-negative latency.
func (s Spec) Valid() (bool, string) {
	multiTick := s.Horizon != SingleShot ||
		s.Schedule.Kind == Heartbeat || s.Schedule.Kind == Async || s.Schedule.Kind == Continuous
	if multiTick && s.TickBudget <= 0 {
		return false, "a multi-tick / standing session MUST carry a positive TickBudget (guaranteed termination)"
	}
	if s.Schedule.Kind == Heartbeat && s.Schedule.Period < 1 {
		return false, "heartbeat schedule needs Period >= 1"
	}
	if s.Schedule.Kind == Async && s.Schedule.Latency < 0 {
		return false, "async schedule needs Latency >= 0"
	}
	return true, ""
}

// scheduledAt reports whether the session does a step on tick t (1-based), per its schedule.
func (s Spec) scheduledAt(t int) bool {
	switch s.Schedule.Kind {
	case OnDemand:
		return t == 1
	case Heartbeat:
		return t%s.Schedule.Period == 0
	case Async:
		return t == 1 || t == 1+s.Schedule.Latency // fire, then poll
	case Continuous:
		return true
	}
	return false
}

// Lifecycle drives a Spec deterministically in ticks.
type Lifecycle struct {
	Spec       Spec
	Tick       int
	Steps      int
	Terminated Terminate
}

// New builds a Lifecycle for a valid spec (panics on an invalid spec — call Spec.Valid first).
func New(s Spec) *Lifecycle {
	if ok, why := s.Valid(); !ok {
		panic(fmt.Sprintf("invalid session spec: %s", why))
	}
	return &Lifecycle{Spec: s}
}

// Run advances the session tick-by-tick up to its TickBudget, invoking step on each SCHEDULED tick and
// checking goalMet each tick. It is GUARANTEED to terminate: goal_met if goalMet fires, single-shot
// after its one move, else budget_exhausted at the cap. Returns the terminate reason + the final tick.
// (goalMet/step may be nil.)
func (l *Lifecycle) Run(goalMet func(tick int) bool, step func(tick int)) (Terminate, int) {
	budget := l.Spec.TickBudget
	if l.Spec.Horizon == SingleShot && budget <= 0 {
		budget = 1 // single-shot is capped at one move
	}
	for l.Tick = 1; l.Tick <= budget; l.Tick++ {
		if l.Spec.scheduledAt(l.Tick) {
			if step != nil {
				step(l.Tick)
			}
			l.Steps++
			if l.Spec.Horizon == SingleShot {
				l.Terminated = GoalMet // single-shot: the one move IS completion
				return l.Terminated, l.Tick
			}
		}
		if goalMet != nil && goalMet(l.Tick) {
			l.Terminated = GoalMet
			return l.Terminated, l.Tick
		}
	}
	l.Tick = budget
	l.Terminated = BudgetExhausted // the hard cap guarantees we always get here if nothing else fired
	return l.Terminated, l.Tick
}
