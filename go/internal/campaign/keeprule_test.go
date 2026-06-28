package campaign

import (
	"math"
	"testing"
)

// arm builds an ArmResult from a per-item pass pattern + token total.
func arm(pass []bool, tokens int) ArmResult { return ArmResult{PerItem: pass, Tokens: tokens} }

// bits builds a pass slice: n total, the first k true.
func bits(k, n int) []bool {
	out := make([]bool, n)
	for i := 0; i < k && i < n; i++ {
		out[i] = true
	}
	return out
}

func TestKeepRuleTier1RegressionReverts(t *testing.T) {
	// even a big lift is reverted if Tier-1 retrieval regressed.
	base := arm(bits(5, 20), 10000)
	with := arm(bits(18, 20), 10000)
	v := Evaluate(base, with, false /* tier1 regressed */, DefaultKeepRule())
	if v.Decision != Revert {
		t.Fatalf("Tier-1 regression must REVERT, got %s (%s)", v.Decision, v.Reason)
	}
}

func TestKeepRuleSignificantLiftKeeps(t *testing.T) {
	// baseline solves the first 5; with-batch solves 5 + 12 MORE (all fixes, no breaks).
	base := ArmResult{PerItem: bits(5, 30), Tokens: 30000}
	wp := bits(5, 30)
	for i := 5; i < 17; i++ {
		wp[i] = true
	}
	with := ArmResult{PerItem: wp, Tokens: 30000}
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Keep {
		t.Fatalf("a significant lift must KEEP, got %s (%s)", v.Decision, v.Reason)
	}
	if v.Fixed != 12 || v.Broke != 0 {
		t.Errorf("discordant wrong: fixed=%d broke=%d", v.Fixed, v.Broke)
	}
	if v.Lift <= 0 || v.McNemarP >= 0.05 {
		t.Errorf("expected significant positive lift, lift=%.3f p=%.3f", v.Lift, v.McNemarP)
	}
}

func TestKeepRuleSignificantRegressionReverts(t *testing.T) {
	// the batch breaks 12 of the baseline's wins and fixes none — a real regression.
	base := ArmResult{PerItem: bits(17, 30), Tokens: 30000}
	with := ArmResult{PerItem: bits(5, 30), Tokens: 30000}
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Revert {
		t.Fatalf("a significant regression must REVERT, got %s (%s)", v.Decision, v.Reason)
	}
	if v.Broke <= v.Fixed {
		t.Errorf("expected broke>fixed, got broke=%d fixed=%d", v.Broke, v.Fixed)
	}
}

func TestKeepRuleFlatButCheaperKeeps(t *testing.T) {
	// SAME tasks solved (capability flat), but the with-batch arm spends far fewer tokens — the
	// convertibility win (a minted skill replaces N generate calls). Client-confirmed: keep it.
	pass := bits(10, 20)
	base := ArmResult{PerItem: pass, Tokens: 20000} // 2000 tok/solved
	with := ArmResult{PerItem: pass, Tokens: 12000} // 1200 tok/solved — 800 cheaper
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Keep {
		t.Fatalf("flat-capability + cheaper must KEEP, got %s (%s)", v.Decision, v.Reason)
	}
	if v.EfficiencyDelta < 700 {
		t.Errorf("efficiency delta should be ~800 tokens/solved, got %.0f", v.EfficiencyDelta)
	}
}

func TestKeepRuleFlatButCostlierReverts(t *testing.T) {
	pass := bits(10, 20)
	base := ArmResult{PerItem: pass, Tokens: 12000}
	with := ArmResult{PerItem: pass, Tokens: 20000} // costlier, no capability gain
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Revert {
		t.Fatalf("flat-capability + costlier must REVERT, got %s (%s)", v.Decision, v.Reason)
	}
}

func TestKeepRuleNoGainReverts(t *testing.T) {
	// identical capability, identical cost — no value, filler.
	pass := bits(10, 20)
	base := ArmResult{PerItem: pass, Tokens: 20000}
	with := ArmResult{PerItem: pass, Tokens: 20000}
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Revert {
		t.Fatalf("no gain must REVERT, got %s (%s)", v.Decision, v.Reason)
	}
}

func TestKeepRulePromisingLeanIsMargin(t *testing.T) {
	// a small positive lean (fixes 2, breaks 0) that is NOT significant, at FLAT tokens-per-solved
	// (more tasks solved but proportionally more tokens spent: 40000/10 == 48000/12 == 4000) → human
	// decides. (Solving more for the SAME tokens would itself be an efficiency win → KEEP.)
	base := ArmResult{PerItem: bits(10, 40), Tokens: 40000}
	wp := bits(10, 40)
	wp[10], wp[11] = true, true // +2 fixes, 0 breaks — not enough for significance
	with := ArmResult{PerItem: wp, Tokens: 48000}
	v := Evaluate(base, with, true, DefaultKeepRule())
	if v.Decision != Margin {
		t.Fatalf("a non-significant positive lean at flat cost should be MARGIN, got %s (%s)", v.Decision, v.Reason)
	}
	if v.McNemarP < 0.05 {
		t.Errorf("2 fixes should not be significant, p=%.3f", v.McNemarP)
	}
}

func TestKeepRuleUnpairedArmsRevert(t *testing.T) {
	v := Evaluate(arm(bits(2, 5), 100), arm(bits(2, 8), 100), true, DefaultKeepRule())
	if v.Decision != Revert {
		t.Fatalf("unpaired arms must REVERT (never a silent keep), got %s", v.Decision)
	}
}

func TestTokensPerSolvedInfiniteAtZero(t *testing.T) {
	a := arm([]bool{false, false}, 5000)
	if !math.IsInf(a.TokensPerSolved(), 1) {
		t.Error("zero solved must be +Inf tokens/solved (never reads as cheap)")
	}
}
