package config

import "testing"

// TestSkillReframeKnobWired pins the GAP-8 reframe flag's wiring at the config layer (the wiring-gate
// lesson: a flag that exists but is not addressable is dead). It proves convert.skill_reframe is in the
// canonical knob table, is opt-in (so it is excluded from OffPaths and the config summary stays
// byte-identical regardless of value), defaults ON since the 2026-06-20 redesign go-live (recall via
// the Capability incl. legacy fallback, 8d885f1), AND that a knob set still flips the field OFF — the
// legacy path stays toggleable (its removal is the next slice). The set/get is the path the engine
// reads to call SkillRegistry.SetReframe.
func TestSkillReframeKnobWired(t *testing.T) {
	c := New()
	if !c.Convert.SkillReframe {
		t.Fatal("convert.skill_reframe must default ON since the 2026-06-20 redesign go-live (AllOn product default)")
	}
	var found bool
	for _, k := range Knobs() {
		if k.Path != "convert.skill_reframe" {
			continue
		}
		found = true
		if !k.OptIn {
			t.Fatal("convert.skill_reframe must be opt-in (excluded from OffPaths so the summary is byte-identical)")
		}
		// the legacy/OFF path must stay toggleable: flipping the knob OFF then ON must move the field.
		k.SetBool(c, false)
		if c.Convert.SkillReframe {
			t.Fatal("flipping the knob OFF must clear Convert.SkillReframe (the legacy path must stay reachable)")
		}
		k.SetBool(c, true)
	}
	if !found {
		t.Fatal("knob convert.skill_reframe is not in the canonical knob table")
	}
	if !c.Convert.SkillReframe {
		t.Fatal("flipping the knob must set Convert.SkillReframe (the field the engine reads to SetReframe)")
	}
}
