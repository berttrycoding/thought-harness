package config

import (
	"reflect"
	"testing"
)

// pullup_panels_test.go — the G5 PANEL-CUSTOMIZATION resolver gate (Track G). It asserts the customization
// THINKING the redesign §6 Phase 5 intends: the master gate is opt-in (default OFF ⇒ byte-identical full
// canon order), an ON config honours the CHOSEN panels in the CHOSEN order, unknown IDs are dropped and
// duplicates collapsed (forward-compatible + idempotent), a horizon is clamped, and the choice round-trips
// through Save/Load (the persistence the spec asks for). The resolver is the ONE place the choice is
// resolved, so both the `^O` pull-up and the analysis tabs read it identically.

// TestPullupPanelsDefaultsOffOptIn — the byte-identical gate: the master knob defaults OFF, is OptIn, and
// is excluded from OffPaths(); with it OFF the resolver returns the canonical full order at the default
// horizon (so a default surface is unchanged).
func TestPullupPanelsDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Tui.PullupPanels {
		t.Fatal("tui.pullup.panels must DEFAULT OFF in AllOn() (opt-in View customization)")
	}
	k, ok := KnobByPath("tui.pullup.panels")
	if !ok {
		t.Fatal("tui.pullup.panels knob must be registered")
	}
	if !k.OptIn {
		t.Error("tui.pullup.panels must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "tui.pullup.panels" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	order, horizon := c.ResolvePullupPanels()
	if !reflect.DeepEqual(order, PanelRegistry) {
		t.Errorf("default (knob OFF) resolver order = %v, want the canonical full PanelRegistry %v", order, PanelRegistry)
	}
	if horizon != DefaultStripHorizon {
		t.Errorf("default (knob OFF) horizon = %d, want DefaultStripHorizon %d", horizon, DefaultStripHorizon)
	}
}

// TestResolveHonoursChosenOrderAndDropsUnknown — the core THINKING: ON + a chosen order yields exactly
// the chosen KNOWN panels in the chosen order; an unknown ID is dropped; a duplicate is collapsed to its
// first occurrence (so the resolved set never repeats a panel).
func TestResolveHonoursChosenOrderAndDropsUnknown(t *testing.T) {
	c := AllOn()
	c.Tui.PullupPanels = true
	c.Tui.PullupOrder = []string{"SEAM", "VITALS", "NOT_A_PANEL", "SEAM", "LOOP"}
	order, _ := c.ResolvePullupPanels()
	want := []string{"SEAM", "VITALS", "LOOP"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("resolved order = %v, want %v (chosen order, unknown dropped, dup collapsed)", order, want)
	}
}

// TestResolveEmptyOrderShowsEverything — ON with no chosen panels still shows the full canon order, so a
// config that only customizes the horizon does not blank the surface.
func TestResolveEmptyOrderShowsEverything(t *testing.T) {
	c := AllOn()
	c.Tui.PullupPanels = true
	c.Tui.StripHorizon = 100
	order, horizon := c.ResolvePullupPanels()
	if !reflect.DeepEqual(order, PanelRegistry) {
		t.Errorf("ON + empty order = %v, want full canon order", order)
	}
	if horizon != 100 {
		t.Errorf("horizon = %d, want the chosen 100", horizon)
	}
}

// TestResolveAllUnknownNeverBlanks — an entirely-unknown chosen set (e.g. an OLD panel vocabulary) must
// NOT blank the surface; it falls back to the full canon order.
func TestResolveAllUnknownNeverBlanks(t *testing.T) {
	c := AllOn()
	c.Tui.PullupPanels = true
	c.Tui.PullupOrder = []string{"RETIRED_A", "RETIRED_B"}
	order, _ := c.ResolvePullupPanels()
	if !reflect.DeepEqual(order, PanelRegistry) {
		t.Errorf("all-unknown order resolved to %v, want the full canon fallback (never blank)", order)
	}
}

// TestStripHorizonClamped — Validate clamps a stored horizon into [Min, Max]; 0 is left as "default".
func TestStripHorizonClamped(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},                  // 0 left as-is (means "default"); resolver maps it
		{3, MinStripHorizon},    // below the floor
		{9999, MaxStripHorizon}, // above the ceiling
		{DefaultStripHorizon, DefaultStripHorizon}, // in-range untouched
	}
	for _, tc := range cases {
		c := AllOn()
		c.Tui.StripHorizon = tc.in
		c.Validate()
		if c.Tui.StripHorizon != tc.want {
			t.Errorf("Validate() horizon %d -> %d, want %d", tc.in, c.Tui.StripHorizon, tc.want)
		}
	}
	// the resolver maps a 0 (default) field to DefaultStripHorizon when the gate is on.
	c := AllOn()
	c.Tui.PullupPanels = true
	c.Tui.StripHorizon = 0
	if _, h := c.ResolvePullupPanels(); h != DefaultStripHorizon {
		t.Errorf("resolver maps 0 horizon to %d, got %d", DefaultStripHorizon, h)
	}
}

// TestPullupCustomizationPersists — the choice round-trips through Save/Load (the persisted-customization
// the spec requires): a saved customized config reloads with the same gate, order, and horizon.
func TestPullupCustomizationPersists(t *testing.T) {
	c := AllOn()
	c.Tui.PullupPanels = true
	c.Tui.PullupOrder = []string{"SEAM", "VITALS", "LOOP"}
	c.Tui.StripHorizon = 120

	path := t.TempDir() + "/cfg.json"
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := Load(path, true, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Tui.PullupPanels {
		t.Error("reloaded config lost tui.pullup.panels=on")
	}
	if !reflect.DeepEqual(got.Tui.PullupOrder, []string{"SEAM", "VITALS", "LOOP"}) {
		t.Errorf("reloaded PullupOrder = %v, want the saved order", got.Tui.PullupOrder)
	}
	if got.Tui.StripHorizon != 120 {
		t.Errorf("reloaded StripHorizon = %d, want 120", got.Tui.StripHorizon)
	}
	// and the reloaded config resolves to the saved choice.
	order, horizon := got.ResolvePullupPanels()
	if !reflect.DeepEqual(order, []string{"SEAM", "VITALS", "LOOP"}) || horizon != 120 {
		t.Errorf("reloaded resolve = (%v, %d), want ([SEAM VITALS LOOP], 120)", order, horizon)
	}
}

// TestPullupOrderStringKnobRoundTrips — the CLI/env transport: the comma-joined string knob splits into
// the slice and joins back, and tui.strip_horizon parses as an int — so the customization is addressable
// from the env/CLI surface, not only the JSON file.
func TestPullupOrderStringKnobRoundTrips(t *testing.T) {
	c := AllOn()
	if !SetTunable(&c, "tui.pullup_order", "VITALS, LOOP ,SEAM") {
		t.Fatal("SetTunable(tui.pullup_order) failed")
	}
	if !reflect.DeepEqual(c.Tui.PullupOrder, []string{"VITALS", "LOOP", "SEAM"}) {
		t.Errorf("PullupOrder = %v, want trimmed [VITALS LOOP SEAM]", c.Tui.PullupOrder)
	}
	k, _ := KnobByPath("tui.pullup_order")
	if got, _ := k.GetString(&c); got != "VITALS,LOOP,SEAM" {
		t.Errorf("GetString = %q, want VITALS,LOOP,SEAM", got)
	}
	if !SetTunable(&c, "tui.strip_horizon", "80") {
		t.Fatal("SetTunable(tui.strip_horizon) failed")
	}
	if c.Tui.StripHorizon != 80 {
		t.Errorf("StripHorizon = %d, want 80", c.Tui.StripHorizon)
	}
}
