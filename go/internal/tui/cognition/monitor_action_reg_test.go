package cognition

import (
	"strings"
	"testing"
)

func TestRenderActionMonitor(t *testing.T) {
	v := ActionView{
		Horizon:   10,
		Intention: "run the test suite to confirm behaviour is preserved",
		Acts:      []bool{true, false, false, false, true, false, false, false, false, true},
		Returned:  3, Fired: 4, DueTick: 186,
		Grounded: 2, Refuted: 1, Fabricated: 0,
		LastVerdict: "REFUTED", LastTick: 176, LastReason: "believed X · reality: test FAILED",
	}
	out := stripANSI(RenderActionMonitor(v))
	for _, s := range []string{"intention", "run the test suite", "acts", "3 in 10t", "returned", "3 of 4",
		"1 pending", "due tick 186", "verdicts", "grounded 2", "refuted 1", "fabricated 0",
		"last", "REFUTED · tick 176", "reason"} {
		if !strings.Contains(out, s) {
			t.Errorf("action monitor missing %q:\n%s", s, out)
		}
	}
}

func TestRenderActionMonitorFabricationAlarm(t *testing.T) {
	// fabricated>0 is the alarm; the renderer must surface the count (tone is red, asserted via presence).
	v := ActionView{Fired: 1, Returned: 1, Fabricated: 2, Acts: []bool{true}}
	out := stripANSI(RenderActionMonitor(v))
	if !strings.Contains(out, "fabricated 2") {
		t.Errorf("fabrication alarm count missing:\n%s", out)
	}
}

func TestRenderRegistriesMonitor(t *testing.T) {
	v := RegistriesView{
		Horizon: 10,
		Rows: []RegistryRow{
			{Name: "operators", Seed: 34, Minted: 2, InUse: 8, UseVerb: "fired"},
			{Name: "specialists", Seed: 9, Minted: 2, InUse: 5, UseVerb: "fired"},
			{Name: "knowledge", Seed: 41, InUse: 6, UseVerb: "recalled"},
			{Name: "memory", Seed: 22, InUse: 8, UseVerb: "recalled", Extra: "12 episodes · 7 beliefs · 3 prefs"},
		},
		Used:      []bool{false, true, false, true, false, false, true, false, true, false},
		LastEvent: "MINTED", LastDetail: "specialist \"learned:verify\"", LastTick: 142,
		LastReason: "3 grounded repeats of the same workflow",
	}
	out := stripANSI(RenderRegistriesMonitor(v))
	for _, s := range []string{"operators", "34 + 2 minted", "fired 8", "specialists", "knowledge",
		"41", "recalled 6", "memory", "12 episodes", "used", "MINTED", "learned:verify", "tick 142",
		"3 grounded repeats"} {
		if !strings.Contains(out, s) {
			t.Errorf("registries monitor missing %q:\n%s", s, out)
		}
	}
	// the used strip is exactly the horizon width.
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "used") {
			if on, off := strings.Count(l, "█"), strings.Count(l, "_"); on+off != 10 {
				t.Errorf("used strip width = %d, want 10", on+off)
			}
		}
	}
}
