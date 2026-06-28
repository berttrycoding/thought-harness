package convert

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/eval"
)

// gateStick builds a mint-gate stick with the given absolute threshold (the eval-object gate of slice g).
func gateStick(threshold float64) *eval.MeasuringStick {
	return &eval.MeasuringStick{
		Name: "mint-gate", Facet: "specialist", Threshold: threshold,
		Check: func(subject any) eval.Score {
			v, _ := subject.(float64)
			return eval.Score{Pass: v >= threshold, Value: v, Reason: "grounded value"}
		},
	}
}

// TestEvalMintGate pins slice (g) / §3.19: with the eval mint gate attached, a candidate that clears the
// frequency×value heuristic (value 0.5 >= MintValue 0.2) is STILL rejected when it fails the stick's
// higher bar (0.9) — the eval object has the final say; a permissive stick (0.1) admits it. Without a gate
// the heuristic alone decides (the default path).
func TestEvalMintGate(t *testing.T) {
	// --- strict gate (0.9): the heuristic-passing candidate (0.5) is rejected ---
	regStrict := &fakeReg{}
	cStrict := New(regStrict, nil, nil, nil) // MintAfter=3, MintValue=0.2
	cStrict.SetMintGate(gateStick(0.9))
	cStrict.Observe(buildEpisode("compute the tax bracket value", 3, 0.5))
	if minted := cStrict.Consolidate(); len(minted) != 0 {
		t.Fatalf("strict gate: expected NO mint (0.5 < gate 0.9), got %v", minted)
	}
	if len(regStrict.registered) != 0 {
		t.Fatalf("strict gate: nothing should be registered, got %d", len(regStrict.registered))
	}

	// --- permissive gate (0.1): the same candidate is admitted ---
	regOK := &fakeReg{}
	cOK := New(regOK, nil, nil, nil)
	cOK.SetMintGate(gateStick(0.1))
	cOK.Observe(buildEpisode("compute the tax bracket value", 3, 0.5))
	if minted := cOK.Consolidate(); len(minted) != 1 {
		t.Fatalf("permissive gate: expected one mint, got %v", minted)
	}

	// --- no gate: the heuristic alone admits (the default, gate-free path) ---
	regNo := &fakeReg{}
	cNo := New(regNo, nil, nil, nil)
	cNo.Observe(buildEpisode("compute the tax bracket value", 3, 0.5))
	if minted := cNo.Consolidate(); len(minted) != 1 {
		t.Fatalf("no gate: expected the heuristic to mint, got %v", minted)
	}
}

// TestEvalRefineSignal pins the comparative refine signal (§3.20): admitByGate measures each candidate
// against the gate's own measurement history, so a higher-scoring candidate reads as Up vs the baseline.
func TestEvalRefineSignal(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)
	c.SetMintGate(gateStick(0.1))

	// two candidates: the second scores higher than the first -> its refine direction is Up.
	if !c.admitByGate("primitive:a", 0.3) {
		t.Fatal("first candidate should admit (0.3 >= 0.1)")
	}
	if len(c.mintHistory) != 1 {
		t.Fatalf("history not recorded: %d", len(c.mintHistory))
	}
	// the second is measured against the first's 0.3 baseline.
	m := c.mintGate.Measure("primitive:b", 0.8, 99)
	sig := eval.Measure(m, c.mintHistory, 0.0)
	if sig.Direction != eval.Up {
		t.Errorf("0.8 vs baseline 0.3 should be Up, got %s (delta %.2f)", sig.Direction, sig.Delta)
	}
}
