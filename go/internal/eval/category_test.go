package eval

import "testing"

// TestSeedCategoriesLoadsStarterSets: the seeded registry carries the locked
// two-axis tool sets + the skill set (§3.10a "Locked this pass").
func TestSeedCategoriesLoadsStarterSets(t *testing.T) {
	r := NewSeededCategoryRegistry()

	// the two tool axes + the four skill tags = 3 + 3 + 4 = 10 categories.
	if got := r.Len(); got != 10 {
		t.Fatalf("seeded registry should have 10 categories; got %d", got)
	}

	for _, want := range []struct{ facet, name string }{
		{"tool-operation", "inspect"}, {"tool-operation", "mutate"}, {"tool-operation", "execute"},
		{"tool-reach", "self"}, {"tool-reach", "local"}, {"tool-reach", "external"},
		{"skill", "reasoning"}, {"skill", "analysis"}, {"skill", "synthesis"}, {"skill", "verification"},
	} {
		c, ok := r.Find(want.facet, want.name)
		if !ok {
			t.Fatalf("seed should contain %s:%s", want.facet, want.name)
		}
		if c.Minted {
			t.Fatalf("a seeded category must not be marked minted: %s", c.Key())
		}
	}
}

// TestSeedIsIdempotent: re-seeding does not duplicate.
func TestSeedIsIdempotent(t *testing.T) {
	r := NewSeededCategoryRegistry()
	n := r.Len()
	r.Seed()
	if r.Len() != n {
		t.Fatalf("re-seeding must not duplicate; was %d now %d", n, r.Len())
	}
}

// TestMintCategoryGrows: a runtime category is minted (marked Minted), additive,
// and refuses an empty facet/name or a clobber of an existing key.
func TestMintCategoryGrows(t *testing.T) {
	r := NewSeededCategoryRegistry()

	c, ok := r.Mint("skill", "planning")
	if !ok || !c.Minted {
		t.Fatalf("a new tag should mint as Minted; got ok=%v %+v", ok, c)
	}
	if _, found := r.Find("skill", "planning"); !found {
		t.Fatalf("minted category should be findable")
	}

	// minting an existing key is a no-op (returns the existing, ok=false).
	existing, ok := r.Mint("skill", "reasoning")
	if ok {
		t.Fatalf("minting an existing key must not overwrite; ok should be false")
	}
	if existing.Minted {
		t.Fatalf("the existing seeded 'reasoning' must stay non-minted")
	}

	// empty facet/name is refused.
	if _, ok := r.Mint("", "x"); ok {
		t.Fatalf("empty facet must be refused")
	}
	if _, ok := r.Mint("skill", ""); ok {
		t.Fatalf("empty name must be refused")
	}
}

// TestFacetFilteringAndStableOrder: Facet returns only that facet's tags in a
// stable Name-sorted order (deterministic for reproducible callers).
func TestFacetFilteringAndStableOrder(t *testing.T) {
	r := NewSeededCategoryRegistry()
	op := r.Facet("tool-operation")
	if len(op) != 3 {
		t.Fatalf("tool-operation facet should have 3; got %d", len(op))
	}
	// sorted by name: execute, inspect, mutate.
	want := []string{"execute", "inspect", "mutate"}
	for i, c := range op {
		if c.Name != want[i] {
			t.Fatalf("facet order mismatch at %d: want %q got %q", i, want[i], c.Name)
		}
	}
}

// TestCategoryKeyIsFacetQualified: the same name in two facets does not collide.
func TestCategoryKeyIsFacetQualified(t *testing.T) {
	r := NewCategoryRegistry()
	a, _ := r.Mint("tool-reach", "shared")
	b, _ := r.Mint("skill", "shared")
	if a.Key() == b.Key() {
		t.Fatalf("same name in different facets must have distinct keys")
	}
	if r.Len() != 2 {
		t.Fatalf("two facet-distinct tags should both store; got %d", r.Len())
	}
}
