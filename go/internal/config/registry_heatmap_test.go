package config

import "testing"

// TestRegistryHeatmapDefaultsOffOptIn locks the G3 registry/memory FAMILY heat-map knob (Track G): it
// exists, is OptIn, defaults OFF in AllOn(), and (being off) does NOT appear in OffPaths() — so a
// default run's config.load summary + every golden are byte-identical to before the heat-map analysis
// tab existed. It is still addressable (flips on via ApplyToggle). This is the byte-identical guarantee
// for the additive observability tab: default-OFF the REGISTRIES analysis tab keeps its placeholder.
func TestRegistryHeatmapDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Tui.RegistryHeatmap {
		t.Fatal("tui.registry_heatmap must DEFAULT OFF in AllOn() (opt-in observability instrument)")
	}
	k, ok := KnobByPath("tui.registry_heatmap")
	if !ok {
		t.Fatal("tui.registry_heatmap knob must be registered")
	}
	if !k.OptIn {
		t.Error("tui.registry_heatmap must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "tui.registry_heatmap" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "tui.registry_heatmap", true) || !c.Tui.RegistryHeatmap {
		t.Error("tui.registry_heatmap must be flippable on via ApplyToggle")
	}
}
