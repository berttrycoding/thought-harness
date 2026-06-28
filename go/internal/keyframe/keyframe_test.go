package keyframe

import "testing"

func TestDescriptorStableUnderRevoicing(t *testing.T) {
	// the recurrence key must be the SAME for two re-voicings of the same line (loop closure depends
	// on it) — case, punctuation, and whitespace are normalised away.
	a := Descriptor("The capital of France is Paris.")
	b := Descriptor("the  capital of france is paris")
	c := Descriptor("THE CAPITAL OF FRANCE IS PARIS!!!")
	if a == "" {
		t.Fatal("non-empty text yielded an empty descriptor")
	}
	if a != b || a != c {
		t.Fatalf("re-voicings did not map to the same descriptor: %q %q %q", a, b, c)
	}
	if Descriptor("a completely different thought") == a {
		t.Fatal("distinct text collided to the same descriptor")
	}
	if Descriptor("   ") != "" || Descriptor("") != "" {
		t.Fatal("blank text must yield an empty (no) descriptor")
	}
}

func TestFirstSightingIsNotAClosure(t *testing.T) {
	d := New(2)
	if cl, closed := d.Observe("exploring whether to use a hash map", 1, "test"); closed || cl != nil {
		t.Fatalf("first sighting reported a closure: %v", cl)
	}
	if d.Len() != 1 {
		t.Fatalf("first sighting did not record a keyframe: len=%d", d.Len())
	}
}

func TestReEntryFiresLoopClosure(t *testing.T) {
	d := New(2)
	d.Observe("exploring whether to use a hash map", 1, "test")
	// a within-gap re-read of the same developing line is folded, not a closure.
	if cl, closed := d.Observe("exploring whether to use a hash map", 2, "test"); closed {
		t.Fatalf("within-gap re-read fired a spurious closure: %v", cl)
	}
	// a genuine re-entry minGap ticks later IS a loop closure (anti-rumination).
	cl, closed := d.Observe("exploring whether to use a hash map", 9, "test")
	if !closed || cl == nil {
		t.Fatal("genuine re-entry did not fire a loop closure")
	}
	if cl.Count < 2 || cl.Closures != 1 {
		t.Fatalf("closure counts wrong: count=%d closures=%d", cl.Count, cl.Closures)
	}
	if cl.FirstSeenTick != 1 {
		t.Fatalf("loop-back point wrong: FirstSeenTick=%d, want 1", cl.FirstSeenTick)
	}
	if cl.CrossRun {
		t.Fatal("a same-run closure was wrongly tagged cross-run")
	}
}

func TestCrossRunClosureFromSeed(t *testing.T) {
	d := New(2)
	// restore a descriptor from a PRIOR run (FirstSeenTick=5, seedTick boundary 40).
	desc := Descriptor("the same line I thought about last session")
	d.Seed([]Keyframe{{Descriptor: desc, Gist: "prior line", Count: 1, FirstSeenTick: 5, LastSeenTick: 5, Substrate: "test"}}, 40)
	if d.SeededLen() != 1 {
		t.Fatalf("seed did not restore the keyframe: seededLen=%d", d.SeededLen())
	}
	// re-entering the seeded line THIS run is a durable, cross-session loop closure (what F-M7 unlocks).
	cl, closed := d.Observe("the same line I thought about last session", 100, "test")
	if !closed || cl == nil {
		t.Fatal("re-entering a seeded descriptor did not fire a closure")
	}
	if !cl.CrossRun {
		t.Fatal("re-entering a PRIOR-RUN descriptor must be a CROSS-RUN closure")
	}
	if cl.FirstSeenTick != 5 {
		t.Fatalf("cross-run loop-back point wrong: FirstSeenTick=%d, want 5", cl.FirstSeenTick)
	}
}

func TestSeededReEntryClosesEvenWhenTickRestarts(t *testing.T) {
	// the cross-run case the engine actually hits: a NEW run restarts its tick counter, so a re-entry of
	// a prior-run descriptor lands at a SMALL tick (gap from the prior FirstSeen <= 0). A seeded descriptor
	// must STILL close — it was explored last session, so the within-run minGap guard does not apply.
	d := New(5) // a large minGap that would suppress a same-run re-read
	desc := Descriptor("the line I explored last session at tick 8")
	d.Seed([]Keyframe{{Descriptor: desc, Count: 1, FirstSeenTick: 8, LastSeenTick: 8, Substrate: "test"}}, 8)
	// re-enter at tick 2 (the new run's early tick): gap = 2 - 8 = -6 < minGap, but it is CROSS-RUN.
	cl, closed := d.Observe("the line I explored last session at tick 8", 2, "test")
	if !closed || cl == nil {
		t.Fatal("a seeded descriptor re-entered on an early new-run tick did not close (cross-run gap guard wrong)")
	}
	if !cl.CrossRun {
		t.Fatal("the seeded re-entry was not tagged cross_run")
	}
}

func TestExportIsDeterministicAndBiTemporal(t *testing.T) {
	d := New(1)
	d.Observe("beta line", 3, "claude:sonnet")
	d.Observe("alpha line", 1, "claude:sonnet")
	d.Observe("alpha line", 7, "claude:sonnet") // re-entry: bumps count + LastSeenTick
	got := d.Export()
	if len(got) != 2 {
		t.Fatalf("export len=%d, want 2", len(got))
	}
	// descriptor-ascending order is deterministic across runs (persistence determinism).
	if got[0].Descriptor > got[1].Descriptor {
		t.Fatal("export not in deterministic descriptor order")
	}
	// find the alpha frame: it must carry the bi-temporal window + substrate tag.
	alpha := Descriptor("alpha line")
	var found bool
	for _, f := range got {
		if f.Descriptor == alpha {
			found = true
			if f.FirstSeenTick != 1 || f.LastSeenTick != 7 {
				t.Fatalf("bi-temporal window wrong: first=%d last=%d", f.FirstSeenTick, f.LastSeenTick)
			}
			if f.Count != 2 {
				t.Fatalf("re-entry did not bump count: %d", f.Count)
			}
			if f.Substrate != "claude:sonnet" {
				t.Fatalf("substrate tag lost: %q", f.Substrate)
			}
		}
	}
	if !found {
		t.Fatal("alpha keyframe missing from export")
	}
}

func TestSeedMergeKeepsEarliestFirstSeen(t *testing.T) {
	d := New(2)
	desc := Descriptor("recurring line")
	d.Observe("recurring line", 50, "test") // this-run sighting at tick 50
	// a later Seed of the SAME descriptor from an even-earlier run must keep the earliest FirstSeen.
	d.Seed([]Keyframe{{Descriptor: desc, Count: 3, FirstSeenTick: 2, LastSeenTick: 9, Substrate: "test"}}, 40)
	for _, f := range d.Export() {
		if f.Descriptor == desc {
			if f.FirstSeenTick != 2 {
				t.Fatalf("merge did not keep earliest FirstSeen: %d", f.FirstSeenTick)
			}
			if f.Count != 4 { // 1 (this run) + 3 (seeded)
				t.Fatalf("merge did not sum counts: %d", f.Count)
			}
		}
	}
}
