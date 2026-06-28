package zoommem

import "testing"

// T5 — TYPE-AWARE zoom. The accounting (Q3) showed "aside" thoughts hogging the working set near the
// end of an episode. A type-aware cap policy should fix that: fade asides a level faster, and protect
// verification when the current thought is a decision. The win condition: less budget on asides at
// NO cost to relevant-memory recall.

// typedCap is the kind-aware cap policy: asides fade one level faster; verify stays sharp (>=L1) when
// the focus is a decision. Everything else uses the structural capFor.
func typedCap(kind map[int]string, focusKind string) func(u, focus Unit) Level {
	return func(u, focus Unit) Level {
		base := capFor(u, focus)
		switch kind[u.ID] {
		case "aside":
			if base < L3Tag {
				base++ // one level more compressed than it would otherwise get
			}
		case "verify":
			if focusKind == "decide" && base > L1Thought {
				base = L1Thought // protect verification while deciding
			}
		}
		return base
	}
}

func asideShare(ctx Context, kind map[int]string) float64 {
	if ctx.Total == 0 {
		return 0
	}
	a := 0
	for _, s := range ctx.Shown {
		if kind[s.Unit.ID] == "aside" {
			a += s.Unit.size(s.Level)
		}
	}
	return float64(a) / float64(ctx.Total)
}

func TestDataset_T5_TypedZoom(t *testing.T) {
	var baseAside, typedAside []float64
	baseHit, typedHit, totSurface := 0, 0, 0
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		focusID := ep.Units[len(ep.Units)-1].ID
		base := Assemble(ep.Units, focusID, budget)
		typed := assembleWith(ep.Units, focusID, budget, typedCap(ep.Kind, ep.Kind[focusID]))
		baseAside = append(baseAside, asideShare(base, ep.Kind))
		typedAside = append(typedAside, asideShare(typed, ep.Kind))

		for _, p := range ep.Probes {
			b := Assemble(ep.Units, p.FocusID, budget)
			ty := assembleWith(ep.Units, p.FocusID, budget, typedCap(ep.Kind, ep.Kind[p.FocusID]))
			for _, id := range p.ShouldSurface {
				totSurface++
				if levelOf(b, id) <= L2OneLiner {
					baseHit++
				}
				if levelOf(ty, id) <= L2OneLiner {
					typedHit++
				}
			}
		}
	}
	bAside, tAside := mean(baseAside), mean(typedAside)
	bRec := float64(baseHit) / float64(totSurface)
	tRec := float64(typedHit) / float64(totSurface)
	t.Logf("T5 type-aware zoom: aside budget-share %.0f%% -> %.0f%% ; surface-recall %.0f%% -> %.0f%%",
		bAside*100, tAside*100, bRec*100, tRec*100)
	if tAside > bAside+0.005 {
		t.Errorf("T5: typed policy did not reduce aside budget-share (%.1f%% -> %.1f%%)", bAside*100, tAside*100)
	}
	if tRec < bRec-0.05 {
		t.Errorf("T5: typed policy hurt recall (%.0f%% -> %.0f%%)", bRec*100, tRec*100)
	}
}
