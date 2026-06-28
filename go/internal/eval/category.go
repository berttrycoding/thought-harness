package eval

import "github.com/berttrycoding/thought-harness/internal/action"

// Category (§3.10a) is a growable TAG that objects (tools, skills, the category-
// scopes of primitive subagents) reference for matching. Categories are their
// own registry — one shared, FACET-DISCRIMINATED (tool | skill | …) registry,
// SEEDED with the starter sets and MINTABLE + refinable (split/merge/rename via
// the self-improvement loop), NOT a hardcoded enum.
//
// A Category is a REFERENCE-style object (a tag): it gets reference-eval (is it
// useful / distinct?), and has no instances. It lives in this package because
// the eval gate consumes facets and categories share the seeded+mintable shape;
// 03-action references the taxonomy, it does not re-decide it (§3.10a owner-of-
// record).

// Category is one tag within one facet.
type Category struct {
	// Facet discriminates the axis this tag lives on (e.g. "tool-operation",
	// "tool-reach", "skill"). Two categories with the same Name but different
	// Facets are distinct.
	Facet string
	// Name is the tag value (e.g. "inspect", "external", "reasoning").
	Name string
	// Minted is false for a seeded category, true for one minted at runtime.
	Minted bool
}

// Key is the stable registry key — facet-qualified so the same name can live in
// two facets without colliding.
func (c Category) Key() string { return c.Facet + ":" + c.Name }

// CategoryRegistry is the one shared, facet-discriminated Category registry
// (§3.10a). It is seeded via Seed* below and grows via Mint. It is generic +
// deterministic (no wall clock); the map gives stable lookup, and listing is
// done in a stable order so callers stay reproducible.
type CategoryRegistry struct {
	byKey map[string]Category
}

// NewCategoryRegistry returns an empty registry. Use SeedCategories to load the
// starter sets, or NewSeededCategoryRegistry for the seed in one call.
func NewCategoryRegistry() *CategoryRegistry {
	return &CategoryRegistry{byKey: map[string]Category{}}
}

// seedSets are the starter categories (§3.10a "Locked this pass"):
//
//	tool operation {inspect, mutate, execute} × tool reach {self, local, external}
//	— the two axes the action gate routes on (03 §2/§3); and
//	skill {reasoning, analysis, synthesis, verification}.
//
// This SUPERSEDES the earlier flat {inspect, mutate, execute, external} (which
// conflated operation and reach).
//
// GAP 7 RECONCILIATION — ONE source of truth for the tool taxonomy. The tool-operation
// and tool-reach facets are SEEDED FROM the action gate's canonical wire strings
// (action.OperationWireValues / action.ReachWireValues), not a second hardcoded copy:
// the action gate is the in-code OWNER of the two axes (it routes on them), this
// registry is the growable §3.10a owner-of-record that holds the same seed set + the
// mint/refine loop. Deriving the facets removes the silent duplication the audit flagged
// (gap 7) — the action enum and this registry can no longer drift
// (TestCategoryRegistrySeedsFromActionTaxonomy pins the agreement). The skill facet has
// no action twin, so it stays a local literal.
var seedSets = map[string][]string{
	"tool-operation": action.OperationWireValues,
	"tool-reach":     action.ReachWireValues,
	"skill":          {"reasoning", "analysis", "synthesis", "verification"},
}

// NewSeededCategoryRegistry returns a registry preloaded with the starter sets.
func NewSeededCategoryRegistry() *CategoryRegistry {
	r := NewCategoryRegistry()
	r.Seed()
	return r
}

// Seed loads the locked starter sets (idempotent — re-seeding does not duplicate
// and never overwrites a minted tag of the same key).
func (r *CategoryRegistry) Seed() {
	for facet, names := range seedSets {
		for _, name := range names {
			c := Category{Facet: facet, Name: name, Minted: false}
			if _, exists := r.byKey[c.Key()]; !exists {
				r.byKey[c.Key()] = c
			}
		}
	}
}

// Find looks up a category by facet + name.
func (r *CategoryRegistry) Find(facet, name string) (Category, bool) {
	c, ok := r.byKey[Category{Facet: facet, Name: name}.Key()]
	return c, ok
}

// Mint adds a runtime-created category (split/merge/rename or a brand-new tag).
// It is the §3.15 mint for the category type: it refuses an empty facet/name and
// refuses to overwrite an existing key (mint is additive, not a clobber).
// Returns the stored category + whether it was newly minted.
func (r *CategoryRegistry) Mint(facet, name string) (Category, bool) {
	if facet == "" || name == "" {
		return Category{}, false
	}
	c := Category{Facet: facet, Name: name, Minted: true}
	if existing, exists := r.byKey[c.Key()]; exists {
		return existing, false
	}
	r.byKey[c.Key()] = c
	return c, true
}

// Facet returns all categories of one facet, in a stable (Name-sorted) order so
// callers are reproducible.
func (r *CategoryRegistry) Facet(facet string) []Category {
	var out []Category
	for _, c := range r.byKey {
		if c.Facet == facet {
			out = append(out, c)
		}
	}
	sortCategories(out)
	return out
}

// All returns every category in a stable (Facet then Name) order.
func (r *CategoryRegistry) All() []Category {
	out := make([]Category, 0, len(r.byKey))
	for _, c := range r.byKey {
		out = append(out, c)
	}
	sortCategories(out)
	return out
}

// Len is the number of categories currently in the registry.
func (r *CategoryRegistry) Len() int { return len(r.byKey) }

// sortCategories orders by Facet then Name (insertion sort — the registry is
// small and we avoid pulling in sort for a leaf package's determinism guarantee).
func sortCategories(cs []Category) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && less(cs[j], cs[j-1]); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func less(a, b Category) bool {
	if a.Facet != b.Facet {
		return a.Facet < b.Facet
	}
	return a.Name < b.Name
}
