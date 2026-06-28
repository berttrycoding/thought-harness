package config

import "testing"

// Every profile builds a valid config, and the awake profiles flip exactly the intended faculties.
func TestProfilesBuildAndFlipExpectedKnobs(t *testing.T) {
	if _, ok := ProfileByName("nope"); ok {
		t.Fatal("ProfileByName returned ok for an unknown name")
	}
	if got := DefaultProfileName(); got != "reactive" {
		t.Fatalf("DefaultProfileName = %q, want reactive", got)
	}

	// reactive == AllOn() (every opt-in faculty off), changing nothing but its loop mode.
	react := mustProfile(t, "reactive")
	base := react.Build()
	if base.Conscious.Activity.Forest || base.Conscious.Activity.SeedIntents || base.Conscious.Activity.Soft {
		t.Fatal("reactive profile must leave the experimental faculties OFF")
	}
	if ch := react.Changes(); len(ch) != 1 || ch[0].Label != "loop mode" || ch[0].Value != "reactive" {
		t.Fatalf("reactive profile Changes() should be just {loop mode: reactive}, got %v", ch)
	}

	// awake == the validated living-mind bundle.
	aw := mustProfile(t, "awake")
	if aw.Mode != "continuous" {
		t.Fatalf("awake profile Mode = %q, want continuous", aw.Mode)
	}
	a := aw.Build().Conscious.Activity
	if !a.Forest || !a.SeedIntents || !a.DriveAgenda || !a.Soft {
		t.Fatal("awake profile must turn on forest + seed_intents + drive_agenda + soft")
	}
	if a.SeedIntentCount != 20 {
		t.Fatalf("awake SeedIntentCount = %d, want 20 (full portfolio incl. the Validative root)", a.SeedIntentCount)
	}
	if a.BranchPropensity != 0.5 {
		t.Fatalf("awake BranchPropensity = %.2f, want 0.5 (durability dial)", a.BranchPropensity)
	}
	if !a.ConscienceCeiling {
		t.Fatal("awake profile must keep the conscience ceiling on (it acts unprompted)")
	}
	if awc := aw.Build(); !awc.Action.GateRouter {
		t.Fatal("awake profile must turn on the action gate-router (outward-action safety)")
	}
	// Changes() surfaces the loop mode as "awake" (not "continuous") and lists the flipped knobs.
	ch := aw.Changes()
	if ch[0].Label != "loop mode" || ch[0].Value != "awake" {
		t.Fatalf("awake Changes()[0] = %v, want {loop mode: awake}", ch[0])
	}
	if len(ch) < 5 {
		t.Fatalf("awake Changes() should list its flipped knobs, got only %d entries", len(ch))
	}
	if ModeLabel("continuous") != "awake" || ModeLabel("reactive") != "reactive" {
		t.Fatal("ModeLabel must map continuous->awake and reactive->reactive")
	}

	// self-contained automation: the awake profile wires proactive outreach + asks to persist memory.
	if !aw.Build().Conscious.Activity.ProactiveOutreach {
		t.Fatal("awake profile must wire proactive outreach (no env var needed)")
	}
	if !aw.Persist {
		t.Fatal("awake profile must request auto-persistence (self-contained memory)")
	}
	if mustProfile(t, "reactive").Persist {
		t.Fatal("reactive profile must NOT auto-persist")
	}
	// awake must NOT turn on the experimental learning knobs (that is awake-learning).
	if a.Learn || a.Experiment {
		t.Fatal("awake profile must leave learning OFF (that is the awake-learning profile)")
	}

	// awake-learning == awake + the learning knobs.
	la := mustProfile(t, "awake-learning").Build()
	lac := la.Conscious.Activity
	if !lac.Forest || !lac.Soft || !lac.Learn || !lac.Experiment || !lac.GoalFeedback || !lac.Retracement {
		t.Fatal("awake-learning must extend awake with learn + experiment + goal_feedback + retracement")
	}
	if !la.Convert.EvalGate {
		t.Fatal("awake-learning must turn on the mint eval-gate")
	}
}

func mustProfile(t *testing.T, name string) Profile {
	t.Helper()
	p, ok := ProfileByName(name)
	if !ok {
		t.Fatalf("profile %q not found", name)
	}
	return p
}
