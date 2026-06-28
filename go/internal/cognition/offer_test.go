package cognition

// offer_test.go — W3 synthesiser catalog curation. Offer is the Pattern-A retrieval floor: a bounded,
// goal-scored operator SUBSET so the synthesis prompt does not flood at scale, FLAG-GATED so the
// default (THOUGHT_SYNTH_CATALOG_TOPK unset ⇒ SynthOfferCap == 0) offers the WHOLE catalog
// byte-identically. These tests assert the SELECTION behaviour (bounded, core-families always present,
// goal-keyed, irrelevant op dropped past k, deterministic) plus the default-OFF whole-catalog path.

import (
	"strings"
	"testing"
)

// TestOfferDefaultOffIsWholeCatalog — the flag-gated default. With SynthOfferCap == 0 (the
// THOUGHT_SYNTH_CATALOG_TOPK unset default) curation is OFF: Offer returns the whole catalog, in
// Names() order, byte-identical to the legacy catalog.Names() behaviour. This is the byte-identical
// invariant the build promises.
func TestOfferDefaultOffIsWholeCatalog(t *testing.T) {
	if SynthOfferCap != 0 {
		t.Fatalf("precondition: SynthOfferCap default must be 0 (curation OFF), got %d", SynthOfferCap)
	}
	r := NewOperatorRegistry()
	// grow well past any plausible cap so "whole catalog" is a real claim, not a small-catalog no-op.
	for i := 0; i < 300; i++ {
		r.MintWithMove("extraop"+itoaPad(i), "generative", "invent a candidate for topic "+itoaPad(i), MoveGround)
	}
	all := r.Names()
	got := r.Offer("design a small api", SynthOfferCap)
	if strings.Join(got, ",") != strings.Join(all, ",") {
		t.Fatalf("default-OFF (cap 0) must offer the whole catalog in Names() order:\n got %d ops\nwant %d ops", len(got), len(all))
	}
	// k<=0 explicitly is the no-cap signal regardless of catalog size.
	if got0 := r.Offer("design a small api", 0); strings.Join(got0, ",") != strings.Join(all, ",") {
		t.Fatalf("Offer(goal, 0) must be the whole catalog")
	}
	if gotNeg := r.Offer("design a small api", -5); strings.Join(gotNeg, ",") != strings.Join(all, ",") {
		t.Fatalf("Offer(goal, <0) must be the whole catalog")
	}
}

// TestOfferNoOpWhenCatalogFits — even with curation ON, when the catalog fits within k every operator
// is offered (the subset only shapes things once the catalog exceeds the cap).
func TestOfferNoOpWhenCatalogFits(t *testing.T) {
	r := NewOperatorRegistry()
	all := r.Names()
	got := r.Offer("design a small api", len(all)+10)
	if len(got) != len(all) {
		t.Fatalf("when the catalog fits within k Offer must return the whole catalog: got %d, want %d", len(got), len(all))
	}
}

// TestOfferBoundsAtScale — at 10x scale with an explicit positive cap, Offer is bounded to exactly k.
func TestOfferBoundsAtScale(t *testing.T) {
	const k = 48
	r := NewOperatorRegistry()
	for i := 0; i < 300; i++ {
		r.MintWithMove("mintedop"+itoaPad(i), "generative", "invent a candidate for topic "+itoaPad(i), MoveGround)
	}
	if len(r.Names()) <= k {
		t.Fatalf("precondition: catalog should exceed the cap, got %d", len(r.Names()))
	}
	got := r.Offer("invent a candidate for topic 042", k)
	if len(got) != k {
		t.Fatalf("at scale Offer must bound to the cap %d, got %d", k, len(got))
	}
	// no duplicates in the offered subset.
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("offered subset has a duplicate: %q", n)
		}
		seen[n] = true
	}
}

// TestOfferAlwaysIncludesCoreFamilies — the always-include guarantee. Even at a TINY cap (smaller than
// the seed core), curation may NEVER strip a whole core family: every core family
// (transformative/relational/generative/primitive) has at least one representative in the subset.
func TestOfferAlwaysIncludesCoreFamilies(t *testing.T) {
	r := NewOperatorRegistry()
	// flood with minted ops so a naive selector could push the seed core out entirely.
	for i := 0; i < 200; i++ {
		r.MintWithMove("mintedop"+itoaPad(i), "synthesized", "synthesised verb number "+itoaPad(i), MoveGround)
	}
	coreFamilies := []string{"transformative", "relational", "generative", "primitive"}
	// k = 4 is exactly the number of core families: the always-include pass must spend the whole budget
	// on one representative per family.
	got := r.Offer("an unrelated goal about widgets", len(coreFamilies))
	if len(got) != len(coreFamilies) {
		t.Fatalf("Offer(_, %d) must bound to %d, got %d (%v)", len(coreFamilies), len(coreFamilies), len(got), got)
	}
	famSeen := map[string]bool{}
	for _, n := range got {
		spec, ok := r.Get(n)
		if !ok {
			t.Fatalf("offered op %q not in registry", n)
		}
		famSeen[spec.Family] = true
	}
	for _, fam := range coreFamilies {
		if !famSeen[fam] {
			t.Errorf("core family %q has no representative in the offered subset %v", fam, got)
		}
	}
}

// TestOfferGoalKeyed — the subset is goal-scored. A goal that names a specific MINTED operator pulls it
// into the subset; an unrelated minted operator is dropped past k. (The seed core is foundational and
// always offered; the discriminating signal is which MINTED ops survive.)
func TestOfferGoalKeyed(t *testing.T) {
	r := NewOperatorRegistry()
	seedCount := len(r.Names())
	// one obviously-relevant minted op + many irrelevant ones.
	r.MintWithMove("photosynthesis", "generative", "model the photosynthesis reaction pathway", MoveGround)
	for i := 0; i < 200; i++ {
		r.MintWithMove("noiseop"+itoaPad(i), "generative", "unrelated filler verb number "+itoaPad(i), MoveGround)
	}
	// cap leaves a few minted slots beyond the seed core, but far fewer than the 201 minted ops, so the
	// selector MUST rank — it cannot offer them all.
	k := seedCount + 3
	got := r.Offer("explain the photosynthesis reaction pathway in a plant", k)

	offered := map[string]bool{}
	for _, n := range got {
		offered[n] = true
	}
	if !offered["photosynthesis"] {
		t.Errorf("the goal-relevant minted op 'photosynthesis' was not offered: %v", got)
	}
	// an arbitrary irrelevant minted op must have been dropped (there is not room for all 200 + the seed
	// core within k).
	droppedAny := false
	for i := 0; i < 200; i++ {
		if !offered["noiseop"+itoaPad(i)] {
			droppedAny = true
			break
		}
	}
	if !droppedAny {
		t.Errorf("no irrelevant minted op was dropped — curation is not bounding (k=%d, offered=%d)", k, len(got))
	}
}

// TestOfferDeterministic — the offered subset must be identical across runs (no map-iteration-order
// leak), so the same goal + catalog always shapes the same prompt.
func TestOfferDeterministic(t *testing.T) {
	build := func() *OperatorRegistry {
		r := NewOperatorRegistry()
		for i := 0; i < 100; i++ {
			r.MintWithMove("op"+itoaPad(i), "relational", "relate instances of kind "+itoaPad(i), MoveAssess)
		}
		return r
	}
	const k = 48
	a := build().Offer("relate instances of kind 010", k)
	b := build().Offer("relate instances of kind 010", k)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("Offer must be deterministic:\n a=%v\n b=%v", a, b)
	}
	// run it many times — a map-order leak shows up as an occasional reorder.
	want := strings.Join(a, ",")
	for i := 0; i < 20; i++ {
		got := strings.Join(build().Offer("relate instances of kind 010", k), ",")
		if got != want {
			t.Fatalf("Offer non-deterministic on run %d:\n got %s\nwant %s", i, got, want)
		}
	}
}

// TestOfferTieBreakIsStableByName — when scores tie, the name tie-break is deterministic ascending, so
// the ordering does not depend on mint/insertion order between two equally-scored minted ops.
func TestOfferTieBreakIsStableByName(t *testing.T) {
	r := NewOperatorRegistry()
	seedCount := len(r.Names())
	// two minted ops with identical intent (equal Jaccard score to any goal) but different names,
	// minted in NON-alphabetical order (zeta before alpha) so a stable sort that didn't reorder would
	// leave them in mint order.
	r.MintWithMove("zeta", "synthesized", "identical filler intent here", MoveGround)
	r.MintWithMove("alpha", "synthesized", "identical filler intent here", MoveGround)
	// a third, deliberately LOWER-scoring minted op so the catalog exceeds the budget (curation's
	// ranked passes actually run) while both tied ops still fit.
	r.MintWithMove("zzfiller", "synthesized", "wholly unrelated padding verb", MoveGround)
	// budget = seed core + 2 ⇒ the two tied ops fit, the third (lower-scoring, but here also 0) is the
	// overflow; the catalog (seedCount+3) exceeds the budget so the ranked path runs.
	got := r.Offer("a goal that shares nothing", seedCount+2)
	// both fit, but among the two tied minted ops 'alpha' must precede 'zeta' (name ascending).
	ai, zi := -1, -1
	for i, n := range got {
		if n == "alpha" {
			ai = i
		}
		if n == "zeta" {
			zi = i
		}
	}
	if ai == -1 || zi == -1 {
		t.Fatalf("both tied minted ops should be offered, got %v", got)
	}
	if ai > zi {
		t.Errorf("tie-break must be name-ascending: 'alpha'(%d) should precede 'zeta'(%d) in %v", ai, zi, got)
	}
	// sanity: no unexpected duplication in the offered subset.
	dedup := map[string]bool{}
	for _, n := range got {
		if dedup[n] {
			t.Fatalf("duplicate in offered subset: %q (%v)", n, got)
		}
		dedup[n] = true
	}
}

// itoaPad renders i as a zero-padded 3-digit string so lexical and numeric order agree in tests.
func itoaPad(i int) string {
	s := ""
	for _, d := range []int{100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}
