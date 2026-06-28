package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// TestConfigView_Smoke assembles the Config view from a REAL engine (heuristic test double) so the whole
// config data flow is exercised end-to-end, and confirms the section set, the all-on baseline, and that a
// LIVE flip mutates the shared config in place (no rebuild) and surfaces in the next view + the engine.
func TestConfigView_Smoke(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	b := NewBridge(e)

	cv := b.ConfigView()
	// the section set must match the spec's left-rail order (one section per cfgSectionSpecs entry).
	if len(cv.Sections) != len(cfgSectionSpecs) {
		t.Fatalf("want %d sections, got %d", len(cfgSectionSpecs), len(cv.Sections))
	}
	wantIDs := map[string]bool{}
	for _, s := range cfgSectionSpecs {
		wantIDs[s.id] = true
	}
	for _, s := range cv.Sections {
		if !wantIDs[s.ID] {
			t.Errorf("unexpected section id %q", s.ID)
		}
	}

	// every knob in the canonical table must be projected exactly once (no knob dropped, none doubled).
	got := 0
	for _, s := range cv.Sections {
		got += len(s.Rows)
	}
	if want := len(config.Knobs()); got != want {
		t.Fatalf("projected %d rows, want %d (the knob table)", got, want)
	}

	// the all-on baseline: nothing is non-default, nothing is OFF.
	if cv.NonDefault != 0 || cv.OffCount != 0 {
		t.Fatalf("fresh engine: want all-on (0 non-default, 0 off), got %d non-default / %d off", cv.NonDefault, cv.OffCount)
	}

	// a LIVE flip: turn seam.hidden_transform OFF — it must mutate the shared config (no rebuild) and the
	// next view must report it OFF + non-default.
	if !b.ApplyConfigToggle("seam.hidden_transform", false) {
		t.Fatal("ApplyConfigToggle returned false for a known bool path")
	}
	if e.Features().Seam.HiddenTransform {
		t.Fatal("flip did not mutate the shared config (seam.hidden_transform still on)")
	}
	cv2 := b.ConfigView()
	if cv2.OffCount != 1 {
		t.Fatalf("after one flip: want OffCount=1, got %d", cv2.OffCount)
	}
	// the flipped row must read OFF in its section.
	var seamRow *cognition.CfgRow
	for i := range cv2.Sections {
		for j := range cv2.Sections[i].Rows {
			if cv2.Sections[i].Rows[j].Path == "seam.hidden_transform" {
				seamRow = &cv2.Sections[i].Rows[j]
			}
		}
	}
	if seamRow == nil {
		t.Fatal("seam.hidden_transform row absent after flip")
	}
	if seamRow.On || seamRow.Default {
		t.Errorf("flipped row: want On=false Default=false, got On=%v Default=%v", seamRow.On, seamRow.Default)
	}

	// a tunable bump: max_par_width is clamped to W_max=8, so bumping UP from the default 8 stays 8.
	if !b.BumpConfigTunable("subconscious.max_par_width", +5) {
		t.Fatal("BumpConfigTunable returned false for a known int path")
	}
	if w := e.Features().Subconscious.MaxParWidth; w != config.WMax {
		t.Errorf("max_par_width bumped past W_max: got %d, want clamp to %d", w, config.WMax)
	}

	// a FLOAT tunable bump (the conscious.activity.* knobs): lower exhaust_conf from 0.5 by 0.05 -> 0.45,
	// live on the shared config (clamped [0,1], snapped to 2 decimals).
	if !b.BumpConfigTunableFloat("conscious.activity.exhaust_conf", -0.05) {
		t.Fatal("BumpConfigTunableFloat returned false for a known float path")
	}
	if v := e.Features().Conscious.Activity.ExhaustConf; v != 0.45 {
		t.Errorf("exhaust_conf bumped: got %v, want 0.45", v)
	}
	// the float path rejects a non-float (int) knob.
	if b.BumpConfigTunableFloat("subconscious.max_par_width", -0.05) {
		t.Error("BumpConfigTunableFloat accepted an int path")
	}

	// a nil-engine bridge yields the placeholder, not a panic.
	nb := &EngineBridge{}
	if got := nb.ConfigView(); len(got.Sections) != 1 || got.Sections[0].ID != "none" {
		t.Errorf("nil engine: want single placeholder section, got %+v", got.Sections)
	}
}
