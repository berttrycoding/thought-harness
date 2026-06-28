package config

import "testing"

// TestFloatKnobRoundTrip exercises the KnobFloat infrastructure (the float-tunable plumbing the
// conscious.activity.* knobs build on): construct → SetFloat/GetFloat → SetFromString parse + error →
// and the cross-kind guard (GetFloat on a non-float knob reports ok=false).
func TestFloatKnobRoundTrip(t *testing.T) {
	var v float64
	k := floatKnob("conscious.activity.test", "Test float",
		func(*HarnessConfig) float64 { return v },
		func(_ *HarnessConfig, f float64) { v = f })

	if k.Kind != KnobFloat {
		t.Fatalf("Kind = %v, want KnobFloat", k.Kind)
	}

	var c HarnessConfig

	// SetFloat / GetFloat round-trip.
	if !k.SetFloat(&c, 0.42) {
		t.Fatal("SetFloat returned false")
	}
	if got, ok := k.GetFloat(&c); !ok || got != 0.42 {
		t.Fatalf("GetFloat = (%v, %v), want (0.42, true)", got, ok)
	}

	// SetFromString parses a float and applies it.
	if err := k.SetFromString(&c, "0.75"); err != nil {
		t.Fatalf("SetFromString(0.75): %v", err)
	}
	if v != 0.75 {
		t.Fatalf("after SetFromString, v = %v, want 0.75", v)
	}

	// SetFromString rejects a non-float.
	if err := k.SetFromString(&c, "not-a-number"); err == nil {
		t.Fatal("SetFromString(not-a-number) = nil, want error")
	}

	// Cross-kind guard: GetFloat on a bool knob reports ok=false (and vice-versa for GetBool).
	bk := boolKnob("x", "X", func(*HarnessConfig) bool { return true }, func(*HarnessConfig, bool) {})
	if _, ok := bk.GetFloat(&c); ok {
		t.Fatal("GetFloat on a bool knob returned ok=true")
	}
	if _, ok := k.GetBool(&c); ok {
		t.Fatal("GetBool on a float knob returned ok=true")
	}
}
