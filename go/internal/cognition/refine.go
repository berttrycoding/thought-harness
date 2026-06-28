// refine.go is the REFINE op for synthesized skills (P7.2): the "improve a learned capability" half of
// the self-improvement lifecycle (mint → persist → REFINE → retire). A minted skill is tuned toward a
// variant — sharper triggers, a re-derived/evolved body — and the variant is KEPT only if it STRICTLY
// beats the incumbent on a fast eval; otherwise it is reverted. This is the same keep-or-revert
// discipline as convertibility's demote (P0.5), applied to deliberate improvement.
//
// Two invariants: a SEED skill is a frozen invariant and can never be refined; and "strictly beats"
// (not "ties") means refinement only ever moves quality up or stays put — it can't drift sideways.
package cognition

// Refine tunes a minted skill toward variant, keeping the variant ONLY if score(variant) strictly
// exceeds score(incumbent) on the supplied fast-eval; otherwise the incumbent stands (revert). Returns
// (kept, reason). Rejects (kept=false) an unknown skill, a SEED skill (frozen), or a variant that fails
// Verify (so a refinement can't install a malformed body). The variant keeps the incumbent's name and
// stays Synthesized (so it persists + can be refined again).
func (r *SkillRegistry) Refine(name string, variant Skill, score func(Skill) float64) (kept bool, reason string) {
	incumbent, ok := r.skills[name]
	if !ok {
		return false, "no such skill '" + name + "'"
	}
	if !incumbent.Synthesized {
		return false, "'" + name + "' is a seed skill (frozen); cannot refine"
	}

	// the variant always carries the incumbent's identity and stays minted.
	variant.Name = name
	variant.Synthesized = true
	if variant.Tier == "" {
		variant.Tier = incumbent.Tier
	}
	if ok, why := r.Verify(name, variant.Body); !ok {
		return false, "variant rejected: " + why
	}
	// a refinement that would not expand (cycle / over-deep) is rejected.
	if _, err := r.Expand(variant); err != nil {
		return false, "variant does not expand: " + err.Error()
	}

	if score(variant) <= score(incumbent) {
		return false, "variant did not strictly beat the incumbent — reverted"
	}
	r.skills[name] = variant
	if !containsString(r.minted, name) {
		r.minted = append(r.minted, name)
	}
	return true, "refined: variant strictly beat the incumbent"
}
