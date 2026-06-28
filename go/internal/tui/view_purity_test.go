package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// View must be PURE: rendering must not mutate model state (D3). We drive the model into a degenerate
// state (an over-bound cognition scroll + selections), render twice, and assert (a) the scroll/selection
// fields are untouched by View and (b) two consecutive renders are byte-identical (no render-order
// dependence). The Update-side clamps (clampCogScroll/clampCogSelections) own the bounds, not View.
func TestViewIsPure(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.w, a.h, a.ready = 120, 40, true
	a.mode = ModeCognition
	a.recomputeLayout()

	// over-bound everything: View must tolerate and NOT write these back.
	a.cogScroll = 1 << 30
	a.cogCfgSel = 999
	a.cogCfgRow = 999
	a.cogRegSel = 999

	first := a.View()
	if a.cogScroll != 1<<30 || a.cogCfgSel != 999 || a.cogCfgRow != 999 || a.cogRegSel != 999 {
		t.Fatalf("View mutated model state: scroll=%d cfgSel=%d cfgRow=%d regSel=%d",
			a.cogScroll, a.cogCfgSel, a.cogCfgRow, a.cogRegSel)
	}
	second := a.View()
	if first != second {
		t.Fatal("two consecutive renders differ — View is not pure / order-dependent")
	}

	// the Update-side clamp brings the over-bound offset back into range.
	a.clampCogScroll()
	a.clampCogSelections()
	if a.cogScroll == 1<<30 {
		t.Fatal("clampCogScroll did not bound the offset")
	}
	if a.cogCfgSel == 999 || a.cogRegSel == 999 {
		t.Fatalf("clampCogSelections did not bound the selections: cfgSel=%d regSel=%d", a.cogCfgSel, a.cogRegSel)
	}
}
