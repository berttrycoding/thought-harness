package cognition

import "testing"

func TestConsciousViewFromSnapshot(t *testing.T) {
	ab := 4
	d := SnapshotData{
		ActiveBranch:  &ab,
		ActiveContext: []ThoughtVM{{Text: "is this safe?", Source: "GENERATED"}, {Text: "m", Source: "METACOG"}},
		Branches: []BranchVM{
			{ID: 4, Value: 0.71, Status: "ACTIVE"},
			{ID: 2, Value: 0.69, Status: "STASHED", Gist: "value pull"},
			{ID: 0, Value: 0.10, Status: "DEAD"},
		},
		Values: map[int]float64{4: 0.71, 2: 0.69},
	}
	v := ConsciousViewFromSnapshot(d)
	if v.ActiveID != 4 || v.ActiveText != "is this safe?" || v.Thoughts != 1 {
		t.Errorf("active wrong: %+v", v)
	}
	if v.LiveBranches != 2 || v.DeadBranches != 1 {
		t.Errorf("branch counts wrong: live=%d dead=%d", v.LiveBranches, v.DeadBranches)
	}
	if v.BestID != 2 || v.BestValue != 0.69 {
		t.Errorf("best line wrong: %+v", v)
	}
}

func TestRegulatorViewFromSnapshot(t *testing.T) {
	v := RegulatorViewFromSnapshot(SnapshotData{N: 0.07, U: 0.75, Mu: 0.28, Theta: 0.8})
	if v.N != 0.07 || v.U != 0.75 || v.Mu != 0.28 || v.Theta != 0.8 {
		t.Errorf("regulator map wrong: %+v", v)
	}
}

func TestControllerViewFromSnapshot(t *testing.T) {
	d := SnapshotData{Tick: 9, LastMeta: &ControllerMetaVM{
		Decision: "ACT", Mode: "control", NeedsGroundTruth: true, LoopExhausted: false, Ambiguity: 0.42, Reason: "needs truth",
	}}
	v := ControllerViewFromSnapshot(d)
	if v.Mode != "control" || !v.NeedsTruth || v.GoalMet || v.Ambiguity != 0.42 {
		t.Errorf("controller map wrong: %+v", v)
	}
	// a STOP decision reads goal met.
	d.LastMeta.Decision = "DELIVER"
	if !ControllerViewFromSnapshot(d).GoalMet {
		t.Error("DELIVER should read goal met")
	}
}
