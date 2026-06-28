package subconscious

import "strings"

// scope.go is the SCOPE — the authority twin of Context (01-subconscious.md §3.3a). Where Context is the
// MATERIAL a Capability passes down, Scope is the AUTHORITY: a per-registry least-privilege filter with
// OPPOSITE timing —
//
//   - CEILING — EAGER, set by the Capability: a bounded band (domain + categories +, for skills, a tier
//     from Goal.Level) capping the MAXIMUM each registry may be drawn from. Safety-critical: a worker may
//     NEVER widen it at runtime (only an explicit gate can). This is §2.7 (bounded by construction)
//     applied to selection.
//   - PICK — LAZY, resolved just-in-time WITHIN the ceiling at the layer that has the information
//     (operators at assembly, skills/tools/workers at staffing). Nobody resolves a pick before the object
//     that needs it is born (§8 OPT-2, late binding).
//
// The discriminator: eager = the bound (safety); lazy = the pick (relevance). SubAgent.toolScope today is
// a pick with no separate ceiling and no Capability source (subagent.go:79); Scope splits the two and
// sources the ceiling at the Capability.

// Scope is a Capability-set authority ceiling with lazily-resolved picks. The ceiling fields are set once
// (eager) and never widened by a worker; Pick fills slots on demand within the ceiling. NOT goroutine-safe
// (the engine is serial).
type Scope struct {
	domain     string          // the domain band (empty ⇒ unconstrained on domain)
	categories map[string]bool // the allowed category tags (empty ⇒ unconstrained on category)
	skillTier  int             // the skill-tier ceiling (0 ⇒ no tier cap); a pick's tier must be <= this
	picks      map[string]string
}

// NewScope sets a Capability's EAGER ceiling: a domain band, the allowed category tags, and a skill-tier
// cap (0 = no cap). Categories/domain are lower-cased for case-insensitive matching. The returned Scope
// has no picks yet — they are resolved lazily.
func NewScope(domain string, categories []string, skillTier int) *Scope {
	cats := map[string]bool{}
	for _, c := range categories {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" {
			cats[c] = true
		}
	}
	return &Scope{
		domain:     strings.ToLower(strings.TrimSpace(domain)),
		categories: cats,
		skillTier:  skillTier,
		picks:      map[string]string{},
	}
}

// Domain returns the ceiling's domain band.
func (s *Scope) Domain() string { return s.domain }

// Categories returns the ceiling's allowed category tags in a STABLE (sorted) order — the read accessor
// the engine emits as "this run's authority" (the §3.3a audit line) and a wiring-gate test reads to
// confirm the sourced ceiling. An empty ceiling (unconstrained on category) returns an empty slice. The
// returned slice is fresh, so a caller cannot mutate the ceiling.
func (s *Scope) Categories() []string {
	if len(s.categories) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(s.categories))
	for c := range s.categories {
		out = append(out, c)
	}
	// insertion sort (small set; keep the leaf deterministic without pulling in sort here)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// SkillTier returns the ceiling's skill-tier cap (0 ⇒ uncapped).
func (s *Scope) SkillTier() int { return s.skillTier }

// AllowsCategory reports whether a category tag is within the ceiling. An empty ceiling (no categories
// declared) is unconstrained — everything is allowed; otherwise only declared tags pass.
func (s *Scope) AllowsCategory(category string) bool {
	if len(s.categories) == 0 {
		return true
	}
	return s.categories[strings.ToLower(strings.TrimSpace(category))]
}

// AllowsDomain reports whether a specialist's domain band is within this ceiling's domain (§3.3a applied
// to the SPECIALIST-firing entry, GAP 5-DEEPER part 2 — subconscious.capability_specialists). An EMPTY
// ceiling domain ("" — the episode/general scope) is UNCONSTRAINED: every domain passes, so the episode
// dispatch path is byte-identical to the legacy "fire on θ alone". A NON-empty ceiling domain admits only
// a specialist whose domain matches it (case-insensitive) — the least-privilege bite the redesign
// specifies for the entry: a domain-banded Capability fires only its band's specialists, the rest stay
// dark even above θ. A specialist with an empty domain (a domain-less worker) is admitted unconditionally
// (it is not domain-banded, so a domain ceiling does not exclude it). Deterministic (string compare, no
// RNG/clock), so two runs of the same scope + domain admit identically.
func (s *Scope) AllowsDomain(domain string) bool {
	if s.domain == "" {
		return true // unconstrained ceiling (the episode/general scope) — byte-identical to θ-only firing
	}
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return true // a domain-less specialist is not banded ⇒ a domain ceiling does not exclude it
	}
	return d == s.domain
}

// AllowsTier reports whether a skill tier is within the ceiling (tier <= the cap). A 0 cap is uncapped.
func (s *Scope) AllowsTier(tier int) bool {
	return s.skillTier == 0 || tier <= s.skillTier
}

// Pick resolves a LAZY pick for a registry facet WITHIN the ceiling: the candidate is admitted only if its
// category clears AllowsCategory. Returns (candidate, true) on admission (and records it so a later read is
// stable), or ("", false) if the candidate is outside the ceiling — a worker can never widen the ceiling, so
// an out-of-ceiling pick is REFUSED, not granted. A re-pick of the same facet returns the recorded pick.
func (s *Scope) Pick(facet, candidate, category string) (string, bool) {
	if existing, ok := s.picks[facet]; ok {
		return existing, true
	}
	if !s.AllowsCategory(category) {
		return "", false // outside the ceiling — refused (only an explicit gate widens, never a worker)
	}
	s.picks[facet] = candidate
	return candidate, true
}

// Picked returns the pick recorded for a facet (empty when unresolved).
func (s *Scope) Picked(facet string) (string, bool) {
	v, ok := s.picks[facet]
	return v, ok
}
