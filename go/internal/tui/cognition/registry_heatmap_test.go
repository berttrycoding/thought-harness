package cognition

// registry_heatmap_test.go — the G3 registry/memory FAMILY heat-map + mint/demote ledger PROPERTY
// tests (the THINKING the §6 analysis tab must read back, not just that it renders). Each test asserts
// a cognition claim the "is the learned machinery real AND in use?" view depends on (mock §6): the
// heat map reconstructs WHEN each learned item fired over the session, sorts the hottest first, flags a
// cold item as a prune candidate, and the ledger records every mint/demote with the engine's own
// evidence. Pure: deterministic event fixtures, no engine, no model, no clock.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// learnedSession is a recorded stream exercising the §6 family: an operator is MINTED then fires; a
// "compute" specialist fires hot across the run; a "social" specialist fires once early then goes cold
// (the prune candidate); a skill is minted and fires; a knowledge fact is recalled; and a specialist is
// DEMOTED by keep-or-revert. This is the trajectory the heat map + ledger reconstruct.
func learnedSession() []events.Event {
	es := []events.Event{
		ev(1, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe?"}),
		// the social specialist fires once early, then never again -> cold-since prune candidate.
		ev(2, events.SubFire, map[string]any{"domain": "social"}),
		// a runtime-minted operator (birth + ledger MINTED).
		ev(3, events.SubOperator, map[string]any{"name": "GROUND", "family": "grounding"}),
	}
	// the compute specialist fires hot across the whole run; GROUND fires repeatedly after its mint.
	for _, t := range []int{4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28} {
		es = append(es, ev(t, events.SubFire, map[string]any{"domain": "compute"}))
	}
	for _, t := range []int{5, 9, 13, 17, 21, 25, 29} {
		es = append(es, ev(t, events.SubFire, map[string]any{"domain": "GROUND"}))
	}
	es = append(es,
		// a knowledge fact recalled twice (it powered a decision).
		ev(11, events.KnowledgeRecall, map[string]any{"top": "cache is warm in prod", "hits": 1.0}),
		ev(19, events.KnowledgeRecall, map[string]any{"top": "cache is warm in prod", "hits": 1.0}),
		// a skill minted from repeated programs, then it fires.
		ev(15, events.SkillMint, map[string]any{"kind": "skill", "name": "verify", "count": 3.0}),
		ev(23, events.SkillMatch, map[string]any{"skill": "verify"}),
		// a specialist demoted by keep-or-revert (reality refuted it).
		ev(27, events.Convert, map[string]any{"kind": "demote", "domain": "simulation"}),
		ev(30, events.Decision, map[string]any{"decision": "STOP", "stop_kind": "GOAL_MET", "reason": "done"}),
		ev(30, events.Respond, nil),
	)
	return es
}

// findEntry returns the reconstructed FamilyEntry for (family, name) or nil.
func findEntry(rec AnalysisRecord, fam, name string) *FamilyEntry {
	for i := range rec.FamilyEntries {
		if rec.FamilyEntries[i].Family == fam && rec.FamilyEntries[i].Name == name {
			return &rec.FamilyEntries[i]
		}
	}
	return nil
}

// TestHeatMapReconstructsRealPerEntryUse — the §6 heat map must answer "is this learned item USED, not
// just collected?": each item that fired gets a row whose firing history reflects the recorded ticks
// (a hot item has many fires, a cold one one). A hollow/constant reconstruction (the wiring failure)
// would NOT track the real per-entry counts. THIS is the cognition the panel exists to surface.
func TestHeatMapReconstructsRealPerEntryUse(t *testing.T) {
	rec := RecordFromFrozen(learnedSession(), nil)

	if len(rec.FamilyEntries) == 0 {
		t.Fatal("the heat map reconstructed ZERO learned items off a stream that fired specialists/skills/knowledge — not reading the real registry use")
	}

	compute := findEntry(rec, "spec", "compute")
	if compute == nil {
		t.Fatal("the hot 'compute' specialist never surfaced as a heat-map row")
	}
	social := findEntry(rec, "spec", "social")
	if social == nil {
		t.Fatal("the cold 'social' specialist never surfaced as a heat-map row")
	}
	// the heat map must track the REAL difference: compute ran hot (many fires), social ran cold (one).
	if compute.Total <= social.Total {
		t.Errorf("the heat map did not track real per-entry use: compute fires=%d must exceed social fires=%d", compute.Total, social.Total)
	}
	if social.Total != 1 {
		t.Errorf("the social specialist fired once in the recording; got Total=%d", social.Total)
	}
	// the firing history must mark the actual ticks (not a constant): social fired at t2 only.
	if !social.Fires[2] {
		t.Error("the social specialist's firing strip does not mark its real fire tick (t2) — the strip is not reconstructed from the events")
	}

	// the minted operator + skill + recalled knowledge fact must each surface in their family.
	if findEntry(rec, "op", "GROUND") == nil {
		t.Error("the minted operator 'GROUND' never surfaced in the op family")
	}
	if findEntry(rec, "skill", "verify") == nil {
		t.Error("the minted skill 'verify' never surfaced in the skill family")
	}
	if findEntry(rec, "know", "cache is warm in prod") == nil {
		t.Error("the recalled knowledge fact never surfaced in the know family")
	}
}

// TestHeatMapSortsHottestFirstAndFlagsColdPrune — the redesign's "sort so the top of the scroll is what
// matters": within the specialist family, the hot item ranks above the cold one, and the cold item is
// flagged as a prune candidate in the rendered row. The render must read the reconstruction, not a
// constant.
func TestHeatMapSortsHottestFirstAndFlagsColdPrune(t *testing.T) {
	rec := RecordFromFrozen(learnedSession(), nil)

	// within the spec family, compute (hot) must come before social (cold).
	var iCompute, iSocial = -1, -1
	for i, e := range rec.FamilyEntries {
		if e.Family == "spec" && e.Name == "compute" {
			iCompute = i
		}
		if e.Family == "spec" && e.Name == "social" {
			iSocial = i
		}
	}
	if iCompute < 0 || iSocial < 0 {
		t.Fatalf("missing spec rows: compute=%d social=%d", iCompute, iSocial)
	}
	if iCompute > iSocial {
		t.Errorf("hottest-first ordering broken: compute (hot) at %d must precede social (cold) at %d", iCompute, iSocial)
	}

	// the rendered REGISTRIES body (heat ON) must flag the cold specialist as a prune candidate.
	body := renderRegistriesBody(rec, rec.Ticks/2, 80)
	if !strings.Contains(body, "prune?") {
		t.Error("the cold 'social' specialist was not flagged as a prune candidate (◀ prune?) in the rendered heat map")
	}
	// the freshly-minted skill must read NEW (born this session), proving the birth-tag wiring.
	if !strings.Contains(body, "NEW") {
		t.Error("the freshly-minted skill did not read NEW (minted tNN) in the rendered heat map")
	}
}

// TestLedgerRecordsMintsAndDemotesWithEvidence — the §6 mint/demote ledger is the audit "did each item
// enter/leave for a logged reason?": a mint records the evidence (the repeat count), and a demote
// records the cause (reality refuted it). A ledger that recorded only one direction (mints but no
// demotes) would miss the keep-or-revert half the benchmark reads.
func TestLedgerRecordsMintsAndDemotesWithEvidence(t *testing.T) {
	rec := RecordFromFrozen(learnedSession(), nil)

	if len(rec.Ledger) == 0 {
		t.Fatal("the ledger is empty off a stream with a mint AND a demote — not reading the registry-change audit")
	}

	var mintedSkill, demotedSpec bool
	for _, le := range rec.Ledger {
		if le.Action == "MINTED" && le.Family == "skill" && le.Name == "verify" {
			mintedSkill = true
			if !strings.Contains(le.Evidence, "3") {
				t.Errorf("the skill mint ledger line dropped its evidence (the repeat count); evidence=%q", le.Evidence)
			}
		}
		if le.Action == "DEMOTED" && le.Family == "spec" && le.Name == "simulation" {
			demotedSpec = true
			if strings.TrimSpace(le.Evidence) == "" {
				t.Error("the specialist demote ledger line dropped its evidence (the cause)")
			}
		}
	}
	if !mintedSkill {
		t.Error("the skill mint is missing from the ledger")
	}
	if !demotedSpec {
		t.Error("the specialist demote (keep-or-revert) is missing from the ledger — only one direction recorded")
	}

	// the demoted specialist's heat row must be flagged Demoted (crossed off in the heat map).
	if e := findEntry(rec, "spec", "simulation"); e != nil && !e.Demoted {
		t.Error("the demoted specialist's heat row was not flagged Demoted")
	}
}

// TestRegistryHeatRenderGatedOff — the flag-OFF guarantee at the render seam: with registryHeat=false,
// the REGISTRIES analysis tab keeps the "panel pending" placeholder and shows NONE of the heat-map /
// ledger content, so a default-OFF surface is byte-identical to the G2 state. With it ON, the family
// content renders. The active-tab index of REGISTRIES is resolved from the tab strip.
func TestRegistryHeatRenderGatedOff(t *testing.T) {
	rec := RecordFromFrozen(learnedSession(), nil)
	regTab := -1
	for i, name := range analysisTabs {
		if name == "REGISTRIES" {
			regTab = i
		}
	}
	if regTab < 0 {
		t.Fatal("no REGISTRIES tab in the analysis tab strip")
	}

	off := RenderAnalysisTab(rec, rec, rec.Ticks/2, false, regTab, 80, false, false, false)
	if strings.Contains(off, "mint / demote ledger") || strings.Contains(off, "prune?") {
		t.Error("registryHeat=OFF still rendered the heat-map/ledger content — the flag-off placeholder is not byte-identical")
	}
	if !strings.Contains(off, "panel pending") {
		t.Error("registryHeat=OFF did not keep the 'panel pending' placeholder for REGISTRIES")
	}

	on := RenderAnalysisTab(rec, rec, rec.Ticks/2, false, regTab, 80, true, false, false)
	if !strings.Contains(on, "mint / demote ledger") {
		t.Error("registryHeat=ON did not render the §6 mint/demote ledger")
	}
}
