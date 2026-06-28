package timeline

import "testing"

// ptr is a tiny test helper for the *int optional ThoughtID anchor (nil == "no node").
func ptr(i int) *int { return &i }

// TestAppendPreservesOrder is the spine: the timeline is the ordered trajectory of attention, so
// Append must keep insertion order verbatim — All() reads back exactly what went in, in sequence.
func TestAppendPreservesOrder(t *testing.T) {
	tl := New()
	tl.Append(Event{Kind: ThoughtCreated, Tick: 1, BranchID: 0})
	tl.Append(Event{Kind: FocusShifted, Tick: 2, BranchID: 1})
	tl.Append(Event{Kind: Branched, Tick: 3, BranchID: 2})

	all := tl.All()
	if len(all) != 3 {
		t.Fatalf("All() len = %d, want 3", len(all))
	}
	wantKinds := []Kind{ThoughtCreated, FocusShifted, Branched}
	for i, k := range wantKinds {
		if all[i].Kind != k {
			t.Errorf("All()[%d].Kind = %q, want %q", i, all[i].Kind, k)
		}
	}
	if tl.Len() != 3 {
		t.Errorf("Len() = %d, want 3", tl.Len())
	}
}

// TestAppendOnlyNoOverwrite asserts the load-bearing invariant from 02 §2a: thoughts are
// time-dependent and NEVER overwritten — *graph forks, timeline appends*. Re-appending an event
// that shares a tick + anchor with an earlier one MUST add a second record, never replace the first.
func TestAppendOnlyNoOverwrite(t *testing.T) {
	tl := New()
	tl.Append(Event{Kind: ThoughtCreated, Tick: 5, BranchID: 0, ThoughtID: ptr(7)})
	tl.Append(Event{Kind: ReEntered, Tick: 5, BranchID: 0, ThoughtID: ptr(7)}) // same anchor, re-thought

	all := tl.All()
	if len(all) != 2 {
		t.Fatalf("append-only violated: len = %d, want 2 (no overwrite)", len(all))
	}
	if all[0].Kind != ThoughtCreated || all[1].Kind != ReEntered {
		t.Errorf("order/identity not preserved: got %q,%q", all[0].Kind, all[1].Kind)
	}
}

// TestAllReturnsCopy guards the append-only contract at the boundary: a caller mutating the slice
// All() hands back must NOT corrupt the timeline's internal record. (Without this, "append-only"
// leaks.)
func TestAllReturnsCopy(t *testing.T) {
	tl := New()
	tl.Append(Event{Kind: ThoughtCreated, Tick: 1, BranchID: 0})
	got := tl.All()
	got[0].Kind = "tampered"
	if tl.All()[0].Kind != ThoughtCreated {
		t.Fatalf("All() exposed the backing array: mutation leaked into the timeline")
	}
}

// TestSince filters by the tick clock: Since(t) returns every event with Tick >= t, in order. This
// is the temporal-window query the Controller uses to look at recent attention.
func TestSince(t *testing.T) {
	tl := New()
	tl.Append(Event{Kind: ThoughtCreated, Tick: 1, BranchID: 0})
	tl.Append(Event{Kind: FocusShifted, Tick: 4, BranchID: 1})
	tl.Append(Event{Kind: Acted, Tick: 7, BranchID: 1})

	since4 := tl.Since(4)
	if len(since4) != 2 {
		t.Fatalf("Since(4) len = %d, want 2", len(since4))
	}
	if since4[0].Tick != 4 || since4[1].Tick != 7 {
		t.Errorf("Since(4) ticks = %d,%d, want 4,7", since4[0].Tick, since4[1].Tick)
	}
	// Boundary: a tick after every event yields nothing; tick 0 yields everything.
	if got := tl.Since(8); len(got) != 0 {
		t.Errorf("Since(8) len = %d, want 0", len(got))
	}
	if got := tl.Since(0); len(got) != 3 {
		t.Errorf("Since(0) len = %d, want 3", len(got))
	}
}

// TestByAnchor retraces one branch's slice of the trajectory: ByAnchor(b) returns every event whose
// BranchID == b, in order — "what happened on this line, when?" (the retracement query, 02 §2b).
func TestByAnchor(t *testing.T) {
	tl := New()
	tl.Append(Event{Kind: ThoughtCreated, Tick: 1, BranchID: 0})
	tl.Append(Event{Kind: Branched, Tick: 2, BranchID: 1})
	tl.Append(Event{Kind: FocusShifted, Tick: 3, BranchID: 1})
	tl.Append(Event{Kind: Acted, Tick: 4, BranchID: 0})

	b1 := tl.ByAnchor(1)
	if len(b1) != 2 {
		t.Fatalf("ByAnchor(1) len = %d, want 2", len(b1))
	}
	if b1[0].Tick != 2 || b1[1].Tick != 3 {
		t.Errorf("ByAnchor(1) ticks = %d,%d, want 2,3", b1[0].Tick, b1[1].Tick)
	}
	b0 := tl.ByAnchor(0)
	if len(b0) != 2 || b0[0].Kind != ThoughtCreated || b0[1].Kind != Acted {
		t.Errorf("ByAnchor(0) = %+v, want [ThoughtCreated, Acted]", b0)
	}
	if got := tl.ByAnchor(99); len(got) != 0 {
		t.Errorf("ByAnchor(99) len = %d, want 0", len(got))
	}
}

// TestTickCorrelation is the reason the log carries a tick: it must align with external action-event
// ticks so the conscious can ask "did reality confirm the belief I held at the moment I decided?".
// We model a decision at tick 4 and a reality observation that became ready at tick 6 (the watched
// seam's readyTick), and join them by anchor + tick window.
func TestTickCorrelation(t *testing.T) {
	tl := New()
	// the moment of decision (a branch picked an ACT at tick 4)
	tl.Append(Event{Kind: Acted, Tick: 4, BranchID: 2, ThoughtID: ptr(11)})
	// later attention on the same branch
	tl.Append(Event{Kind: FocusShifted, Tick: 5, BranchID: 2})

	// An action observation came back from reality (readyTick = 6). Correlate it to the decision:
	// the latest event on the deciding branch at-or-before the observation tick is the belief held
	// when we decided.
	const obsTick = 6
	const obsBranch = 2
	decision := latestOnBranchAtOrBefore(tl, obsBranch, obsTick)
	if decision == nil {
		t.Fatal("no correlated decision found for the observation")
	}
	if decision.Kind != FocusShifted || decision.Tick != 5 {
		t.Errorf("correlated decision = %q@%d, want FocusShifted@5", decision.Kind, decision.Tick)
	}
	// And the ACT decision itself is retrievable by anchor + tick for the "at the moment I decided" join.
	acts := tl.ByAnchor(obsBranch)
	if len(acts) == 0 || acts[0].Kind != Acted || acts[0].Tick != 4 {
		t.Errorf("decision-node ACT not joinable: %+v", acts)
	}
}

// latestOnBranchAtOrBefore is the correlation primitive the engine would run: of a branch's events,
// the last one with Tick <= cutoff. Defined in the test (not the package) to prove the public
// queries (ByAnchor) suffice to express the action-time join.
func latestOnBranchAtOrBefore(tl *Timeline, branchID, cutoff int) *Event {
	var found *Event
	for _, e := range tl.ByAnchor(branchID) {
		if e.Tick <= cutoff {
			ev := e
			found = &ev
		}
	}
	return found
}

// TestEmptyTimeline asserts the zero/empty cases are well-behaved (non-nil empty slices, Len 0).
func TestEmptyTimeline(t *testing.T) {
	tl := New()
	if tl.Len() != 0 {
		t.Errorf("Len() = %d, want 0", tl.Len())
	}
	if got := tl.All(); got == nil || len(got) != 0 {
		t.Errorf("All() on empty = %v, want non-nil empty", got)
	}
	if got := tl.Since(0); got == nil || len(got) != 0 {
		t.Errorf("Since(0) on empty = %v, want non-nil empty", got)
	}
	if got := tl.ByAnchor(0); got == nil || len(got) != 0 {
		t.Errorf("ByAnchor(0) on empty = %v, want non-nil empty", got)
	}
}
