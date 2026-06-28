package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// phaseFor finds the workflow phase that runs the named operator (nil if none).
func phaseFor(wf *Workflow, opName string) *Phase {
	for i := range wf.Phases {
		if wf.Phases[i].OpName == opName {
			return &wf.Phases[i]
		}
	}
	return nil
}

// TestMintedOperatorInheritsFamilyBias is the P0.4 (sweep #4) gate: a runtime-MINTED operator that has
// no specific opBias entry must inherit its FAMILY prior so its Gate phase is not born with zero bias.
// A 'transformative'-family mint inherits {advocate:0.2} (the real-primitive roster, M2); a bare
// 'synthesized'-family mint inherits the synthesized default. Without the fix both would have an empty
// bias map.
func TestMintedOperatorInheritsFamilyBias(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	if _, ok := cat.Mint("frobnicate", "transformative", "do the frobnicate transform"); !ok {
		t.Fatal("minting a transformative operator must succeed")
	}
	if _, ok := cat.Mint("zorch", "synthesized", "a bespoke synthesised move"); !ok {
		t.Fatal("minting a synthesized operator must succeed")
	}

	prog := &cognition.Program{
		Root: cognition.NewSeq(
			cognition.NewStep("frobnicate", "general", ""),
			cognition.NewStep("zorch", "general", ""),
		),
		Synthesized: true,
	}
	wf := FromProgram(prog, cat, backends.NewTest(), nil, "do a bespoke thing")

	fro := phaseFor(wf, "frobnicate")
	if fro == nil {
		t.Fatal("no phase for the minted 'frobnicate' operator")
	}
	if len(fro.Bias) == 0 {
		t.Fatal("minted op got zero Gate bias — the family prior was not inherited")
	}
	if fro.Bias["advocate"] != 0.2 {
		t.Fatalf("transformative mint bias=%v want {advocate:0.2}", fro.Bias)
	}

	zo := phaseFor(wf, "zorch")
	if zo == nil || len(zo.Bias) == 0 {
		t.Fatalf("a 'synthesized'-family mint must inherit the synthesized prior; got %v", zo)
	}
}

// TestSeedOperatorBiasUnchanged guards the scope: a SEED operator with no named opBias entry stays
// UNBIASED (it does NOT pick up a family prior), so the seed Gate behaviour the scenario goldens anchor
// is preserved. 'combine' is a transformative seed op with no opBias row — its bias must be empty,
// while 'decompose' (which HAS a row) keeps its specific bias.
func TestSeedOperatorBiasUnchanged(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	prog := &cognition.Program{
		Root: cognition.NewSeq(
			cognition.NewStep("combine", "general", ""),   // transformative seed, no opBias row
			cognition.NewStep("decompose", "general", ""), // seed with a specific opBias row
		),
		Synthesized: true,
	}
	wf := FromProgram(prog, cat, backends.NewTest(), nil, "seed-only program")

	if p := phaseFor(wf, "combine"); p == nil || len(p.Bias) != 0 {
		t.Fatalf("a seed op with no named bias must stay unbiased; got %v", p)
	}
	if p := phaseFor(wf, "decompose"); p == nil || p.Bias["decompose"] != 0.3 {
		t.Fatalf("a seed op with a named bias must keep it; got %v", p)
	}
}

// TestSourceAwareBiasIsAdditive is the M5 source-aware-Gate gate (representation-space-rebuild.md §1.4):
// a step that declares a Step.Source adds a nudge toward that source's lane ON TOP of the operator's move
// bias. A validate@reality step is tilted toward BOTH the skeptic/validate checking lane (opBias) AND
// reality (sourceBias) — the path's grounded close gets a doubled evidence nudge; a memory-sourced step
// privileges recall. A default (model) step gets ONLY the operator bias (so goldens are unchanged).
func TestSourceAwareBiasIsAdditive(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	prog := &cognition.Program{
		Root: cognition.NewSeq(
			cognition.SourceStep("analogize", "general", "", cognition.SourceMemory), // memory-sourced reframe
			cognition.SourceStep("validate", "general", "", cognition.SourceReality), // reality-sourced assess
			cognition.NewStep("rank", "general", ""),                                 // default (model) — no source
		),
		Synthesized: true,
	}
	wf := FromProgram(prog, cat, backends.NewTest(), nil, "source-annotated program")

	// memory-sourced step privileges the recall lane.
	if p := phaseFor(wf, "analogize"); p == nil || p.Bias["recall"] != 0.3 {
		t.Fatalf("analogize@memory must add a recall nudge (0.3); got %v", p)
	}
	// reality-sourced validate is doubled: skeptic from opBias (0.4) PLUS skeptic from sourceBias (0.2),
	// and a validate nudge from sourceBias (0.2).
	pv := phaseFor(wf, "validate")
	if pv == nil {
		t.Fatal("no phase for validate")
	}
	if d := pv.Bias["skeptic"] - 0.6; d > 1e-9 || d < -1e-9 {
		t.Fatalf("validate@reality skeptic bias must sum opBias+sourceBias (~0.6); got %v", pv.Bias)
	}
	if pv.Bias["validate"] != 0.2 {
		t.Fatalf("validate@reality must add a validate nudge (0.2); got %v", pv.Bias)
	}
	// a default (model) step gets ONLY the operator bias (rank has none) — no source nudge, goldens safe.
	if p := phaseFor(wf, "rank"); p == nil || len(p.Bias) != 0 {
		t.Fatalf("a model-sourced step must carry only its operator bias (none here); got %v", p)
	}
}
