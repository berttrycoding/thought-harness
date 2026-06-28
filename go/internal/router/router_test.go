package router

import "testing"

// TestRankValueDescendingIdTieBreak: with the zero policy every lane is runnable; the ranking is pure
// value-descending, ties broken by ascending id — deterministic.
func TestRankValueDescendingIdTieBreak(t *testing.T) {
	lanes := []Lane{
		{ID: 3, Value: 0.5},
		{ID: 1, Value: 0.9},
		{ID: 2, Value: 0.5}, // tie with lane 3 on value -> lower id first
		{ID: 4, Value: 0.7},
	}
	r := Rank(lanes, 10, Policy{})
	want := []int{1, 4, 2, 3} // 0.9, 0.7, 0.5(id2), 0.5(id3)
	if len(r.Next) != len(want) {
		t.Fatalf("Next len = %d, want %d (%v)", len(r.Next), len(want), r.Next)
	}
	for i := range want {
		if r.Next[i] != want[i] {
			t.Fatalf("Next[%d] = %d, want %d (full %v)", i, r.Next[i], want[i], r.Next)
		}
	}
	top, ok := r.Top()
	if !ok || top != 1 {
		t.Fatalf("Top = (%d,%v), want (1,true)", top, ok)
	}
}

// TestThresholdGate: a lane below the policy threshold is held back with reason below-thresh and is NOT
// in Next, while a lane above it is runnable.
func TestThresholdGate(t *testing.T) {
	lanes := []Lane{
		{ID: 1, Value: 0.8},
		{ID: 2, Value: 0.3}, // below threshold 0.5
	}
	r := Rank(lanes, 5, Policy{Threshold: 0.5})
	if len(r.Next) != 1 || r.Next[0] != 1 {
		t.Fatalf("Next = %v, want [1]", r.Next)
	}
	// lane 2 must be reported held with below-thresh, not silently dropped.
	found := false
	for _, rk := range r.All {
		if rk.Lane.ID == 2 {
			found = true
			if rk.Runnable {
				t.Fatalf("lane 2 should be held back (below threshold)")
			}
			if rk.Reason != string(holdThreshold) {
				t.Fatalf("lane 2 reason = %q, want %q", rk.Reason, holdThreshold)
			}
		}
	}
	if !found {
		t.Fatalf("lane 2 missing from All -> a held lane was dropped silently")
	}
}

// TestCooldownGate: a hot lane that fired within the cooldown window is held back as on-cooldown; once
// the window passes it becomes runnable again. This is the anti-thrash policy.
func TestCooldownGate(t *testing.T) {
	pol := Policy{Cooldown: 4}
	// lane 1 fired at tick 8; now=10 -> 10-8=2 < 4 -> on cooldown.
	hot := []Lane{{ID: 1, Value: 0.9, LastFired: 8}}
	r := Rank(hot, 10, pol)
	if _, ok := r.Top(); ok {
		t.Fatalf("a lane on cooldown must not be runnable (Top should be false)")
	}
	if r.All[0].Reason != string(holdCooldown) {
		t.Fatalf("reason = %q, want %q", r.All[0].Reason, holdCooldown)
	}
	// now=12 -> 12-8=4 >= 4 -> off cooldown, runnable again.
	r2 := Rank(hot, 12, pol)
	if top, ok := r2.Top(); !ok || top != 1 {
		t.Fatalf("after cooldown window lane 1 should be runnable, got (%d,%v)", top, ok)
	}
	// a never-fired lane (LastFired -1) is never on cooldown.
	fresh := []Lane{{ID: 2, Value: 0.9, LastFired: -1}}
	if _, ok := Rank(fresh, 1, pol).Top(); !ok {
		t.Fatalf("a never-fired lane must not be on cooldown")
	}
}

// TestPerLaneOverrideBeatsPolicy: a per-lane Threshold/Cooldown override (when > 0) takes precedence over
// the policy default; a zero field inherits the default.
func TestPerLaneOverrideBeatsPolicy(t *testing.T) {
	// Policy threshold 0.5; lane 1 overrides to 0.9 so its 0.7 value is now below ITS threshold.
	lanes := []Lane{
		{ID: 1, Value: 0.7, Threshold: 0.9}, // override -> held
		{ID: 2, Value: 0.7},                 // inherits 0.5 -> runnable
	}
	r := Rank(lanes, 0, Policy{Threshold: 0.5})
	if len(r.Next) != 1 || r.Next[0] != 2 {
		t.Fatalf("Next = %v, want [2] (lane 1 held by its own threshold)", r.Next)
	}
}

// TestNeverDispatchesPure: Rank must not mutate the caller's input slice (read-only over live state).
func TestNeverDispatchesPure(t *testing.T) {
	lanes := []Lane{{ID: 2, Value: 0.3}, {ID: 1, Value: 0.9}}
	before := []Lane{lanes[0], lanes[1]}
	_ = Rank(lanes, 0, Policy{})
	for i := range lanes {
		if lanes[i] != before[i] {
			t.Fatalf("Rank mutated input at %d: %+v != %+v", i, lanes[i], before[i])
		}
	}
}

// TestAuditLineDeterministic: the audit line is non-empty, reports the runnable count + the top lane, and
// is identical for identical inputs.
func TestAuditLineDeterministic(t *testing.T) {
	lanes := []Lane{
		{ID: 1, Label: "alpha", Value: 0.9, LastFired: 8},
		{ID: 2, Label: "beta", Value: 0.3},
	}
	a := Rank(lanes, 10, Policy{Threshold: 0.5, Cooldown: 4}).Audit
	b := Rank(lanes, 10, Policy{Threshold: 0.5, Cooldown: 4}).Audit
	if a != b {
		t.Fatalf("audit not deterministic:\n a=%q\n b=%q", a, b)
	}
	if a == "" {
		t.Fatalf("audit line empty")
	}
	// alpha (0.9) on cooldown, beta (0.3) below threshold -> 0 runnable, next=none.
	if want := "0/2 runnable, next=none"; !contains(a, want) {
		t.Fatalf("audit %q should report %q", a, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
