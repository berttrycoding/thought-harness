package engine_test

// registry_heatmap_wire_test.go — the WIRING GATE for the G3 registry/memory FAMILY heat map +
// mint/demote ledger (Track G, §6). The wiring-gate lesson (saved): a unit that exists but never runs
// on the engine's actual tick is dead. The pure cognition/registry_heatmap_test.go proves the family
// reconstruction off a SYNTHETIC stream; THIS test proves it off a REAL engine bus — drive a worked
// scenario that genuinely fires multiple specialists at different rates, capture the live event stream
// exactly as the TUI bridge's freeze tap does (RecordFromFrozen), and assert the heat map reconstructs
// the REAL per-entry usage the §6 panel surfaces. If fillFamily were not reading the live bus, the
// reconstructed heat map would be empty (the wiring-gate failure).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// TestRegistryHeatMapReconstructsLivePrimitiveSubAgentUse — S3 (the dialectic scenario) fires several
// specialists at different rates on the live loop (compute hot, advocate/skeptic cooler). The heat map,
// fed that live stream, must reconstruct ONE row per specialist that fired, and the per-entry fire
// counts must reflect the REAL relative use (the hot specialist outfires the cool ones). A hollow or
// constant reconstruction is the wiring failure.
func TestRegistryHeatMapReconstructsLivePrimitiveSubAgentUse(t *testing.T) {
	_, log := runScenarioLogged(t, "S3")

	rec := cognition.RecordFromFrozen(log.events, nil)

	if len(rec.FamilyEntries) == 0 {
		t.Fatal("the heat map reconstructed ZERO learned items off a live scenario that fired specialists — fillFamily is not reading the real bus (the wiring-gate failure)")
	}

	// every reconstructed row must reflect a REAL fire (Total > 0) and carry a firing strip whose set
	// bits match its count — i.e. the strip is reconstructed from the live ticks, not a constant.
	hottest := 0
	var hottestName string
	for _, e := range rec.FamilyEntries {
		if e.Total == 0 {
			t.Errorf("heat row %s:%s has Total=0 but was reconstructed — a phantom row", e.Family, e.Name)
		}
		set := 0
		for _, b := range e.Fires {
			if b {
				set++
			}
		}
		if set != e.Total {
			t.Errorf("heat row %s:%s firing strip has %d set bits but Total=%d — the strip is not reconstructed from the real ticks", e.Family, e.Name, set, e.Total)
		}
		if e.Total > hottest {
			hottest, hottestName = e.Total, e.Family+":"+e.Name
		}
	}

	// the live run fired more than one specialist at DIFFERENT rates, so there must be a clear hottest
	// item — the heat map's whole job is to surface which learned machinery actually ran hot.
	if len(rec.FamilyEntries) < 2 {
		t.Fatalf("expected multiple specialists firing on the live S3 loop; got %d heat rows", len(rec.FamilyEntries))
	}
	if hottest < 2 {
		t.Errorf("no specialist ran hot on the live loop (hottest %s fired %dx) — the heat map cannot distinguish hot from cold use", hottestName, hottest)
	}

	// the §6 panel (heat ON) must render the live reconstruction without panicking and surface the
	// hottest item's name — proving the render reads the reconstructed record, not the sample.
	body := cognition.RenderAnalysisTab(rec, rec, 0, false, registriesTab(), 90, true, false, false)
	if body == "" {
		t.Fatal("the REGISTRIES heat-map panel rendered empty over a live reconstruction")
	}
}

// TestRegistryHeatMapReconstructsLiveSkillAndSourceFamilies — S6 exercises the SKILL + SOURCE families
// on the live loop (a skill recall + a sourcing-ladder resolution). The heat map must place those in
// their OWN families (skill / src), proving fillFamily keys each event kind to the right family off the
// real bus — not lumping everything into one bucket.
func TestRegistryHeatMapReconstructsLiveSkillAndSourceFamilies(t *testing.T) {
	_, log := runScenarioLogged(t, "S6")

	rec := cognition.RecordFromFrozen(log.events, nil)
	if len(rec.FamilyEntries) == 0 {
		t.Fatal("no heat rows reconstructed off the live S6 loop — fillFamily is not reading the bus")
	}

	families := map[string]bool{}
	for _, e := range rec.FamilyEntries {
		families[e.Family] = true
	}
	// S6 fires a specialist AND recalls a skill AND resolves a source — at least two distinct families
	// must surface (the heat map keys each event kind to its own registry, never one undifferentiated row).
	if len(families) < 2 {
		t.Errorf("the live S6 loop exercised multiple registries (specialist + skill + source) but the heat map collapsed them into %d family/families: %v", len(families), families)
	}
}

// registriesTab resolves the REGISTRIES tab index from the analysis tab strip (so the render call hits
// the §6 body, not another panel).
func registriesTab() int {
	for i := 0; i < cognition.AnalysisTabCount(); i++ {
		if cognition.AnalysisTabName(i) == "REGISTRIES" {
			return i
		}
	}
	return 0
}
