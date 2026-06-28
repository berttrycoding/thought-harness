package funnel

import (
	"testing"
)

// a fully-valid candidate (all three anti-filler tests pass) — the base the tests mutate.
func okCand(id, key, text string) Candidate {
	return Candidate{
		ID: id, Kind: "operator", ClusterKey: key, Text: text,
		Provenance: "generator:taxonomy-cell-3", Links: []string{"operator:decompose"}, Exercised: true,
	}
}

func TestAntiFiller(t *testing.T) {
	if v := AntiFiller(okCand("a", "k", "rephrase the claim plainly")); !v.Pass {
		t.Fatalf("a valid candidate should pass anti-filler, got fails=%v", v.Fails)
	}
	// each test fails in isolation.
	cases := []struct {
		name string
		mut  func(c *Candidate)
		want string
	}{
		{"not-traceable", func(c *Candidate) { c.Provenance = "" }, "not-traceable"},
		{"not-cross-linked", func(c *Candidate) { c.Links = nil }, "not-cross-linked"},
		{"cross-linked-empty", func(c *Candidate) { c.Links = []string{"  "} }, "not-cross-linked"},
		{"not-exercised", func(c *Candidate) { c.Exercised = false }, "not-exercised"},
		{"empty-id", func(c *Candidate) { c.ID = "" }, "empty-id"},
		{"empty-text", func(c *Candidate) { c.Text = "  " }, "empty-text"},
	}
	for _, tc := range cases {
		c := okCand("a", "k", "rephrase the claim plainly")
		tc.mut(&c)
		v := AntiFiller(c)
		if v.Pass {
			t.Errorf("%s: expected fail, passed", tc.name)
		}
		found := false
		for _, f := range v.Fails {
			if f == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected fail %q in %v", tc.name, tc.want, v.Fails)
		}
	}
}

func TestLexicalSimilarity(t *testing.T) {
	a := Candidate{Text: "rephrase the claim into plain words"}
	if s := LexicalSimilarity(a, a); s != 1 {
		t.Fatalf("identical text similarity = %v, want 1", s)
	}
	b := Candidate{Text: "render the claim using plain words"} // overlaps: claim, plain, words, the
	if s := LexicalSimilarity(a, b); s <= 0 || s >= 1 {
		t.Fatalf("partial overlap similarity = %v, want in (0,1)", s)
	}
	c := Candidate{Text: "compile the kernel module quickly"}
	if s := LexicalSimilarity(a, c); s != 0 {
		t.Fatalf("disjoint text similarity = %v, want 0", s)
	}
}

func TestConsolidateExactDedup(t *testing.T) {
	batch := []Candidate{
		okCand("a", "transform/rephrase", "Rephrase  the   CLAIM plainly"), // same as b modulo case/space
		okCand("b", "transform/rephrase", "rephrase the claim plainly"),
		okCand("c", "transform/rephrase", "invert the assumption and re-test"),
	}
	res := Consolidate(batch, LexicalSimilarity, 0.95)
	// a and b are exact duplicates (normalized) -> one representative; c is distinct.
	if len(res.Kept) != 2 {
		t.Fatalf("expected 2 kept (a/b merged, c), got %d: %+v", len(res.Kept), res.Kept)
	}
	// representative is the (ClusterKey, ID)-first, i.e. "a".
	if res.Kept[0].ID != "a" {
		t.Fatalf("expected representative 'a' (id-first), got %q", res.Kept[0].ID)
	}
	if got := res.Merged["a"]; len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected b merged into a, got %v", res.Merged["a"])
	}
}

func TestConsolidateNearDupWithinClusterOnly(t *testing.T) {
	batch := []Candidate{
		okCand("a", "transform/rephrase", "rephrase the claim into plain words"),
		okCand("b", "transform/rephrase", "rephrase the claim using plain words"), // near-dup of a
		okCand("c", "relational/compare", "rephrase the claim into plain words"),  // SAME text, DIFFERENT cluster
	}
	res := Consolidate(batch, LexicalSimilarity, 0.7)
	// a/b merge (same cluster, high lexical overlap). c shares text with a but is a DIFFERENT cluster,
	// so it is NOT merged across the bucket boundary -> kept.
	ids := map[string]bool{}
	for _, k := range res.Kept {
		ids[k.ID] = true
	}
	if ids["a"] == ids["b"] { // exactly one of a/b survives
		t.Fatalf("expected exactly one of a/b kept, got kept=%v", ids)
	}
	if !ids["c"] {
		t.Fatalf("c (different cluster, not a cross-bucket dup) must be kept; kept=%v", ids)
	}
}

func TestConsolidateDeterministicOrderIndependent(t *testing.T) {
	mk := func() []Candidate {
		return []Candidate{
			okCand("z", "k1", "alpha beta gamma delta"),
			okCand("a", "k1", "alpha beta gamma delta epsilon"), // near-dup of z
			okCand("m", "k2", "completely unrelated content here"),
		}
	}
	forward := Consolidate(mk(), LexicalSimilarity, 0.6)
	rev := mk()
	rev[0], rev[2] = rev[2], rev[0] // shuffle input order
	reversed := Consolidate(rev, LexicalSimilarity, 0.6)
	if len(forward.Kept) != len(reversed.Kept) {
		t.Fatalf("consolidation not order-independent: %d vs %d kept", len(forward.Kept), len(reversed.Kept))
	}
	for i := range forward.Kept {
		if forward.Kept[i].ID != reversed.Kept[i].ID {
			t.Fatalf("kept order differs by input order at %d: %q vs %q", i, forward.Kept[i].ID, reversed.Kept[i].ID)
		}
	}
	// representative within the a/z near-dup pair is the id-first, "a".
	if forward.Kept[0].ID != "a" {
		t.Fatalf("expected representative 'a' (id-first within cluster k1), got %q", forward.Kept[0].ID)
	}
}

func TestAdmitCombinesAntiFillerAndDedup(t *testing.T) {
	filler := okCand("filler", "k", "x declared never used")
	filler.Exercised = false // fails anti-filler
	batch := []Candidate{
		okCand("a", "k", "rephrase the claim plainly"),
		okCand("b", "k", "rephrase the claim plainly"), // exact dup of a
		filler,
		okCand("c", "k", "an entirely separate move about induction over cases"),
	}
	res := Admit(batch, LexicalSimilarity, 0.95)
	admitted := map[string]bool{}
	for _, c := range res.Admitted {
		admitted[c.ID] = true
	}
	if !admitted["a"] || !admitted["c"] {
		t.Fatalf("a and c should be admitted, got %v", admitted)
	}
	if admitted["b"] {
		t.Fatalf("b is an exact dup of a, should be rejected")
	}
	if admitted["filler"] {
		t.Fatalf("filler fails anti-filler, should be rejected")
	}
	if r := res.Rejected["filler"]; r == "" {
		t.Fatalf("filler should carry a rejection reason")
	}
	if r := res.Rejected["b"]; r != "near-dup-of:a" {
		t.Fatalf("b should be rejected as near-dup-of:a, got %q", r)
	}
}

// TestConsolidateMergeMapNoDangling: when an exact-dedup representative is ITSELF later folded into a
// near-dup representative, the merge map must re-home its entries onto the final survivor — no key may
// point at a non-Kept candidate (the audit-chain fix). Scenario: a<b<c in one cluster; c is an exact
// dup of b; b is a near-dup of a.
func TestConsolidateMergeMapNoDangling(t *testing.T) {
	batch := []Candidate{
		okCand("a", "k", "alpha beta gamma delta"),
		okCand("b", "k", "alpha beta gamma delta epsilon"), // near-dup of a, distinct hash
		okCand("c", "k", "alpha beta gamma delta epsilon"), // EXACT dup of b
	}
	res := Consolidate(batch, LexicalSimilarity, 0.6)
	if len(res.Kept) != 1 || res.Kept[0].ID != "a" {
		t.Fatalf("expected only 'a' kept, got %+v", res.Kept)
	}
	if _, dangling := res.Merged["b"]; dangling {
		t.Fatalf("merge map has a dangling key 'b' (not in Kept): %v", res.Merged)
	}
	// every merge-map key must be a kept survivor, and a + its merges must cover b and c.
	kept := map[string]bool{"a": true}
	for rep := range res.Merged {
		if !kept[rep] {
			t.Fatalf("merge-map key %q is not a survivor", rep)
		}
	}
	// Admit must point both b and c's rejection at the real survivor a.
	ar := Admit(batch, LexicalSimilarity, 0.6)
	if ar.Rejected["b"] != "near-dup-of:a" || ar.Rejected["c"] != "near-dup-of:a" {
		t.Fatalf("rejections must point at survivor a: b=%q c=%q", ar.Rejected["b"], ar.Rejected["c"])
	}
}

func TestRetrievalIntegrity(t *testing.T) {
	canonical := []Query{
		{Text: "how do I split a goal", ExpectedID: "decompose"},
		{Text: "weigh two options", ExpectedID: "compare"},
		{Text: "a query the baseline already misses", ExpectedID: "ungettable"},
	}
	// baseline answers the first two correctly, misses the third.
	baseline := func(q string) []string {
		switch q {
		case "how do I split a goal":
			return []string{"decompose", "compare"}
		case "weigh two options":
			return []string{"compare", "decompose"}
		default:
			return []string{"something-else"}
		}
	}
	// a GOOD batch: shadow still answers the first two at rank-1 -> Pass, and the baseline-miss is skipped.
	good := func(q string) []string {
		switch q {
		case "how do I split a goal":
			return []string{"decompose", "new-entry", "compare"}
		case "weigh two options":
			return []string{"compare", "new-entry"}
		default:
			return []string{"new-entry", "something-else"}
		}
	}
	if r := RetrievalIntegrity(canonical, baseline, good); !r.Pass {
		t.Fatalf("good batch should pass; regressions=%v", r.Regressions)
	} else if r.Checked != 2 {
		t.Fatalf("expected 2 checked (third skipped as baseline-miss), got %d", r.Checked)
	}
	// a CONFUSING batch: a new entry displaces the correct rank-1 for "split a goal" -> regression.
	confusing := func(q string) []string {
		if q == "how do I split a goal" {
			return []string{"shiny-new-distractor", "decompose"} // rank-1 displaced
		}
		return baseline(q)
	}
	r := RetrievalIntegrity(canonical, baseline, confusing)
	if r.Pass {
		t.Fatalf("confusing batch should FAIL retrieval-integrity")
	}
	if len(r.Regressions) != 1 || r.Regressions[0].ExpectedID != "decompose" {
		t.Fatalf("expected one regression on 'decompose', got %v", r.Regressions)
	}
}

func TestVectorSimilarity(t *testing.T) {
	a := Candidate{Vector: []float32{1, 0, 0}}
	b := Candidate{Vector: []float32{1, 0, 0}}
	c := Candidate{Vector: []float32{0, 1, 0}}
	if s := VectorSimilarity(a, b); s < 0.999 {
		t.Fatalf("identical vectors cosine = %v, want ~1", s)
	}
	if s := VectorSimilarity(a, c); s != 0 {
		t.Fatalf("orthogonal vectors cosine = %v, want 0", s)
	}
	if s := VectorSimilarity(a, Candidate{}); s != 0 {
		t.Fatalf("missing vector cosine = %v, want 0", s)
	}
	if s := VectorSimilarity(a, Candidate{Vector: []float32{1, 0}}); s != 0 {
		t.Fatalf("dim-mismatch cosine = %v, want 0", s)
	}
}
