package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestControllerActivityConfig verifies the live conscious.activity overlay (slice (a)): with no
// overlay the effective config is DefaultCriticConfig; a wired overlay is read live (including a
// post-wire mutation — the TUI live-flip path); a degenerate zero config falls back to the defaults.
func TestControllerActivityConfig(t *testing.T) {
	c := NewController(noEmit, nil, "control", nil) // cfg=nil -> DefaultCriticConfig
	def := DefaultCriticConfig()

	// No overlay: eff() / mergeThreshold() are the static defaults.
	if c.eff().ExhaustConf != def.ExhaustConf || c.eff().SimilarRepeat != def.SimilarRepeat {
		t.Fatalf("no-overlay eff() = %+v, want defaults", c.eff())
	}
	if c.mergeThreshold() != 0.6 {
		t.Fatalf("no-overlay mergeThreshold = %v, want 0.6", c.mergeThreshold())
	}

	// Wire a tuned overlay: eff() / mergeThreshold() reflect it.
	a := config.DefaultConsciousActivity()
	a.ExhaustConf, a.SimilarRepeat, a.MergeThreshold = 0.9, 0.5, 0.3
	c.SetActivityConfig(&a)
	if got := c.eff().ExhaustConf; got != 0.9 {
		t.Errorf("tuned ExhaustConf = %v, want 0.9", got)
	}
	if got := c.eff().SimilarRepeat; got != 0.5 {
		t.Errorf("tuned SimilarRepeat = %v, want 0.5", got)
	}
	if got := c.mergeThreshold(); got != 0.3 {
		t.Errorf("tuned mergeThreshold = %v, want 0.3", got)
	}

	// Live mutation of the shared pointer is observed with no rebuild — the TUI live-flip guarantee.
	a.ExhaustConf = 0.1
	if got := c.eff().ExhaustConf; got != 0.1 {
		t.Errorf("live ExhaustConf = %v, want 0.1", got)
	}

	// Degenerate (zero) config falls back to the static defaults (a non-AllOn HarnessConfig).
	var zero config.ConsciousActivityCfg
	c.SetActivityConfig(&zero)
	if got := c.eff().ExhaustConf; got != def.ExhaustConf {
		t.Errorf("zero-config fallback ExhaustConf = %v, want default %v", got, def.ExhaustConf)
	}
	if got := c.mergeThreshold(); got != 0.6 {
		t.Errorf("zero-config fallback mergeThreshold = %v, want 0.6", got)
	}
}
