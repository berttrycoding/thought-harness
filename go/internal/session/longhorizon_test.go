package session

import "testing"

// TestScratchIsBounded: a long-horizon session's scratch context stays bounded (oldest evicted) — it is
// not an unbounded buffer and not the thought graph.
func TestScratchIsBounded(t *testing.T) {
	sc := NewScratch(3)
	for _, e := range []string{"a", "b", "c", "d", "e"} {
		sc.Add(e)
	}
	if sc.Len() != 3 {
		t.Fatalf("scratch must stay bounded at cap 3; got %d", sc.Len())
	}
	got := sc.Entries()
	if got[0] != "c" || got[2] != "e" {
		t.Fatalf("scratch should hold the 3 most recent (c,d,e); got %v", got)
	}
}

// TestStandingForkHeldUnderU is the P3.9 gate: standing (long-horizon) sessions are forks that consume
// U; the regulator admits them only while U ≤ 1, and every one carries a guaranteed termination.
func TestStandingForkHeldUnderU(t *testing.T) {
	const focus = 8
	mk := func(h Horizon) *Session {
		s, _ := NewSession("s", Spec{Horizon: h, Schedule: Schedule{Kind: Continuous}, TickBudget: 50})
		return s
	}

	// exactly focusCapacity standing sessions -> U == 1.0, schedulable.
	full := make([]*Session, focus)
	for i := range full {
		full[i] = mk(LongHorizon)
	}
	if u, ok := StandingLoad(full, focus); !ok || u != 1.0 {
		t.Fatalf("%d standing sessions should sit at U=1.0 (schedulable); got U=%.3f ok=%v", focus, u, ok)
	}
	// one more -> U > 1, NOT schedulable (the regulator must refuse the excess).
	over := append(full, mk(LongHorizon))
	if u, ok := StandingLoad(over, focus); ok || u <= 1.0 {
		t.Fatalf("%d standing sessions should exceed U=1 (not schedulable); got U=%.3f ok=%v", len(over), u, ok)
	}

	// transient (single-shot / bounded) sessions are NOT standing forks — they don't count toward U.
	transient := []*Session{mk(SingleShot), mk(Bounded), mk(SingleShot)}
	if u, ok := StandingLoad(transient, focus); !ok || u != 0.0 {
		t.Fatalf("transient sessions must not consume standing U; got U=%.3f ok=%v", u, ok)
	}

	// every standing agent carries a guaranteed termination.
	if !AllTerminate(full) {
		t.Fatal("every standing session must have a guaranteed-termination lifecycle")
	}
}

// TestStandingAgentTerminates: a continuous (standing) agent with a tick budget actually runs every tick
// and terminates — never immortal.
func TestStandingAgentTerminates(t *testing.T) {
	s, _ := NewSession("watcher", Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Heartbeat, Period: 2}, TickBudget: 10})
	woke := 0
	term, _ := s.Run(func(int) bool { return false }, func(int) { woke++ })
	if term != BudgetExhausted {
		t.Fatalf("a standing agent with an unmet goal must terminate by budget; got %v", term)
	}
	if woke != 5 { // heartbeat period 2 over 10 ticks -> ticks 2,4,6,8,10
		t.Fatalf("the heartbeat watcher should have woken 5 times; got %d", woke)
	}
}
