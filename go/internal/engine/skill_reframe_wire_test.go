package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestSkillReframeReachesRegistry is the GAP-8 wiring gate (the wiring-gate lesson: a flag that exists
// but is not on the live path is dead). It proves the convert.skill_reframe knob THREADS through
// NewEngine into the seeded SkillRegistry via SetReframe — not just that the config field exists. OFF
// (the default): the registry is the legacy goal-matching/Program-Expand path (Reframed()==false), so a
// seed composite still self-matches. ON: the registry reports Reframed()==true and a Skill no longer
// self-matches goals.
func TestSkillReframeReachesRegistry(t *testing.T) {
	build := func(on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn baseline
		feat.Convert.SkillReframe = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine(reframe=%v): %v", on, err)
		}
		return e
	}

	// OFF (default): the live registry is NOT reframed — the legacy flywheel path.
	off := build(false)
	if off.Skills().Reframed() {
		t.Fatal("reframe OFF: the live SkillRegistry must NOT be reframed (legacy goal-matching path)")
	}
	if _, found := off.Skills().Match("compare postgres versus mysql"); !found {
		t.Fatal("reframe OFF: a goal hitting a composite trigger must still self-match (legacy flywheel)")
	}

	// ON: the flag reached the registry — it is reframed AND a Skill no longer self-matches goals.
	on := build(true)
	if !on.Skills().Reframed() {
		t.Fatal("reframe ON: the flag must reach the live SkillRegistry (SetReframe) — it did not")
	}
	if got, found := on.Skills().Match("compare postgres versus mysql"); found {
		t.Fatalf("reframe ON: a Skill must not self-match goals (goal-matched retired); got %q", got.Name)
	}
}
