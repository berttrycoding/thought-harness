package config

import "testing"

// TestConsciousActivityDefaults pins the activity-threshold defaults to the Controller's historical
// hardcoded values (DefaultCriticConfig + the merge literal 0.6), so AllOn() stays byte-identical to
// pre-config behaviour. (The critic-side test asserts the reverse mapping; config can't import critic.)
func TestConsciousActivityDefaults(t *testing.T) {
	a := AllOn().Conscious.Activity
	floats := []struct {
		name      string
		got, want float64
	}{
		{"DoneConfidence", a.DoneConfidence, 0.7},
		{"FlagThreshold", a.FlagThreshold, 0.6},
		{"ExhaustConf", a.ExhaustConf, 0.5},
		{"PursuitThreshold", a.PursuitThreshold, 0.4},
		{"SimilarRepeat", a.SimilarRepeat, 0.72},
		{"MergeThreshold", a.MergeThreshold, 0.6},
	}
	for _, f := range floats {
		if f.got != f.want {
			t.Errorf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
	if a.ExhaustAfter != 4 {
		t.Errorf("ExhaustAfter = %d, want 4", a.ExhaustAfter)
	}
	if a.MaxSteps != 16 {
		t.Errorf("MaxSteps = %d, want 16", a.MaxSteps)
	}
}

// TestConsciousActivityKnobs exercises the registered activity knobs through the public knob surface
// (the same path CLI/env/TUI use): float + int tunables round-trip, and are discoverable by path.
func TestConsciousActivityKnobs(t *testing.T) {
	c := AllOn()

	if !SetTunable(&c, "conscious.activity.exhaust_conf", "0.3") {
		t.Fatal("SetTunable(exhaust_conf) failed")
	}
	if c.Conscious.Activity.ExhaustConf != 0.3 {
		t.Errorf("ExhaustConf = %v, want 0.3", c.Conscious.Activity.ExhaustConf)
	}

	if !SetTunable(&c, "conscious.activity.exhaust_after", "6") {
		t.Fatal("SetTunable(exhaust_after) failed")
	}
	if c.Conscious.Activity.ExhaustAfter != 6 {
		t.Errorf("ExhaustAfter = %d, want 6", c.Conscious.Activity.ExhaustAfter)
	}

	if k, ok := KnobByPath("conscious.activity.merge_threshold"); !ok || k.Kind != KnobFloat {
		t.Errorf("merge_threshold knob: ok=%v kind=%v, want ok=true KnobFloat", ok, k.Kind)
	}
}
