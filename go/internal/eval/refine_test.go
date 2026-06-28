package eval

// refine_test.go proves the uniform per-registry refine loop (§3.17 / §3.20):
// the generic loop measures a registry's entries against its stick (reference-
// eval, absolute) AND comparatively vs each entry's OWN history (instance-eval),
// derives improve / keep / prune per entry, and NEVER mutates the registry.

import "testing"

// fakeRegistry is a minimal RefinableRegistry over an in-memory entry set, used
// to drive the loop without importing any concrete cognition/subconscious
// registry (the eval package stays a leaf).
type fakeRegistry struct {
	name    string
	stick   MeasuringStick
	hasS    bool
	entries []RefineEntry
}

func (r *fakeRegistry) Name() string                  { return r.name }
func (r *fakeRegistry) Stick() (MeasuringStick, bool) { return r.stick, r.hasS }
func (r *fakeRegistry) Entries() []RefineEntry        { return r.entries }

// TestRefineLoopNoStickIsNoOp: a registry with no attached stick yields an empty
// report (the loop is additive — never panics, never mutates).
func TestRefineLoopNoStickIsNoOp(t *testing.T) {
	reg := &fakeRegistry{name: "operators", hasS: false,
		entries: []RefineEntry{{ID: "a", Subject: scoreSubject{v: 0.9}}}}
	rep := NewRefineLoop(0).Refine(reg)
	if rep.Registry != "operators" {
		t.Fatalf("report should carry the registry name; got %q", rep.Registry)
	}
	if len(rep.Entries) != 0 {
		t.Fatalf("no stick => no entry reports; got %d", len(rep.Entries))
	}
}

// TestRefineLoopBenchmarkPrune: an entry that FAILS the stick's absolute bar is
// flagged Prune (reference-eval reject — it no longer belongs); one that clears
// the bar is Keep on the first pass (no history yet for an Improve).
func TestRefineLoopBenchmarkPrune(t *testing.T) {
	reg := &fakeRegistry{name: "skills", hasS: true, stick: gradedStick("belong", 0.5),
		entries: []RefineEntry{
			{ID: "good", Subject: scoreSubject{v: 0.8}}, // clears 0.5 -> Keep
			{ID: "bad", Subject: scoreSubject{v: 0.2}},  // below 0.5 -> Prune
		}}
	rep := NewRefineLoop(0).Refine(reg)
	if len(rep.Entries) != 2 {
		t.Fatalf("want 2 entry reports; got %d", len(rep.Entries))
	}
	if rep.Entries[0].Verdict != Keep || !rep.Entries[0].Pass {
		t.Fatalf("the passing entry should be Keep+Pass; got %+v", rep.Entries[0])
	}
	if rep.Entries[1].Verdict != Prune || rep.Entries[1].Pass {
		t.Fatalf("the failing entry should be Prune+!Pass; got %+v", rep.Entries[1])
	}
	if p := rep.Prunable(); len(p) != 1 || p[0] != "bad" {
		t.Fatalf("Prunable should be [bad]; got %v", p)
	}
	improve, keep, prune := rep.Counts()
	if improve != 0 || keep != 1 || prune != 1 {
		t.Fatalf("counts mismatch; improve=%d keep=%d prune=%d", improve, keep, prune)
	}
}

// TestRefineLoopComparativeImprove: across passes the loop measures an entry vs
// its OWN accumulating history (instance-eval, §3.20). A reference that trends up
// reads Improve; a dip that still clears the absolute bar stays Keep (a dip is
// not an eviction); only failing the bar prunes.
func TestRefineLoopComparativeImprove(t *testing.T) {
	loop := NewRefineLoop(0.01)
	mk := func(v float64) *fakeRegistry {
		return &fakeRegistry{name: "operators", hasS: true, stick: gradedStick("belong", 0.3),
			entries: []RefineEntry{{ID: "ref", Subject: scoreSubject{v: v}}}}
	}

	// pass 1: no history -> Flat -> Keep (clears 0.3).
	r1 := loop.Refine(mk(0.40))
	if r1.Entries[0].Verdict != Keep || r1.Entries[0].Refine.Direction != Flat {
		t.Fatalf("first pass should be Keep/Flat; got %+v", r1.Entries[0])
	}
	// pass 2: above the running baseline -> Improve.
	r2 := loop.Refine(mk(0.70))
	if r2.Entries[0].Verdict != Improve || r2.Entries[0].Refine.Direction != Up {
		t.Fatalf("an improving reference should read Improve/Up; got %+v", r2.Entries[0])
	}
	// pass 3: a DIP that still clears the absolute bar (0.35 >= 0.3) -> Keep, NOT Prune.
	r3 := loop.Refine(mk(0.35))
	if r3.Entries[0].Verdict != Keep {
		t.Fatalf("a dip above the bar must stay Keep (not evict); got %+v", r3.Entries[0])
	}
	if r3.Entries[0].Refine.Direction != Down {
		t.Fatalf("the dip should still register Down as the comparative signal; got %+v", r3.Entries[0].Refine)
	}
	// the per-reference scorecard accumulated all three measurements in tick order.
	hist := loop.History("belong", "ref")
	if len(hist) != 3 {
		t.Fatalf("history should hold 3 measurements; got %d", len(hist))
	}
	if hist[0].Score.Value != 0.40 || hist[2].Score.Value != 0.35 {
		t.Fatalf("history order/content wrong; got %+v", hist)
	}
}

// TestRefineLoopPerEntryBaseline: two entries in the SAME registry are measured
// against their OWN histories, not pooled — entry "a" trending up does not lift
// entry "b" (the §3.20 per-reference comparison).
func TestRefineLoopPerEntryBaseline(t *testing.T) {
	loop := NewRefineLoop(0)
	reg := func(va, vb float64) *fakeRegistry {
		return &fakeRegistry{name: "operators", hasS: true, stick: gradedStick("belong", 0.1),
			entries: []RefineEntry{
				{ID: "a", Subject: scoreSubject{v: va}},
				{ID: "b", Subject: scoreSubject{v: vb}},
			}}
	}
	loop.Refine(reg(0.20, 0.90)) // seed histories: a=0.20, b=0.90
	rep := loop.Refine(reg(0.30, 0.50))
	// a: 0.30 vs own baseline 0.20 -> Up (Improve).
	if rep.Entries[0].ID != "a" || rep.Entries[0].Verdict != Improve {
		t.Fatalf("entry a should Improve vs its OWN 0.20 baseline; got %+v", rep.Entries[0])
	}
	// b: 0.50 vs own baseline 0.90 -> Down, but clears the 0.1 bar -> Keep (not pooled with a).
	if rep.Entries[1].ID != "b" || rep.Entries[1].Verdict != Keep || rep.Entries[1].Refine.Direction != Down {
		t.Fatalf("entry b should be Keep/Down vs its OWN 0.90 baseline; got %+v", rep.Entries[1])
	}
}

// TestRefineLoopDeterministic: two loops fed the same passes produce identical
// reports — the loop reads no wall clock (logical ticks only).
func TestRefineLoopDeterministic(t *testing.T) {
	mk := func() *fakeRegistry {
		return &fakeRegistry{name: "operators", hasS: true, stick: gradedStick("belong", 0.4),
			entries: []RefineEntry{
				{ID: "x", Subject: scoreSubject{v: 0.6}},
				{ID: "y", Subject: scoreSubject{v: 0.2}},
			}}
	}
	a := NewRefineLoop(0).Refine(mk())
	b := NewRefineLoop(0).Refine(mk())
	if a.Stick != b.Stick || len(a.Entries) != len(b.Entries) {
		t.Fatalf("reports diverged: %+v vs %+v", a, b)
	}
	for i := range a.Entries {
		if a.Entries[i] != b.Entries[i] {
			t.Fatalf("entry %d diverged: %+v vs %+v", i, a.Entries[i], b.Entries[i])
		}
	}
}
