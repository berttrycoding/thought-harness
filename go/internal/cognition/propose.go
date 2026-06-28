// propose.go is the in-process half of the offline optimizer tier (P7.4). The optimizer itself (DSPy +
// GEPA trace-driven evolution of skills / prompts / tool-descriptions) runs OUT of process and emits
// candidate variants; this is the contract those candidates come back through. Its defining property is
// the opposite of REFINE (P7.2): a proposal is NEVER auto-promoted. ProposeRefinement evaluates a
// candidate and returns a reviewable PROPOSAL — it does NOT touch the live registry. A human/PR gate
// applies it (via Refine) only on approval, so the live system is unchanged until then. No weights are
// trained; the optimizer evolves text artifacts that the same Verify/eval discipline must clear.
package cognition

// Proposal is a reviewable optimisation candidate for a skill: the proposed variant, the fast-eval
// scores of the incumbent vs the variant, whether it is a strict improvement, and a rationale. It is an
// ARTIFACT to review, not a change that has been applied.
type Proposal struct {
	SkillName      string
	Variant        Skill
	IncumbentScore float64
	VariantScore   float64
	Improvement    bool // VariantScore strictly > IncumbentScore
	Applicable     bool // the variant also passes Verify/Expand (a clean, installable proposal)
	Rationale      string
}

// ProposeRefinement evaluates a candidate variant for a minted skill and returns a Proposal WITHOUT
// modifying the registry (the live system is unchanged). A seed skill is rejected (frozen). The proposal
// is marked Applicable only if the variant is a strict improvement AND passes Verify + Expand — i.e. it
// would be accepted by Refine if approved. Approval = the caller invoking Refine with the variant.
func (r *SkillRegistry) ProposeRefinement(name string, variant Skill, score func(Skill) float64) (Proposal, bool) {
	incumbent, ok := r.skills[name]
	if !ok || !incumbent.Synthesized {
		return Proposal{}, false // unknown or seed (frozen) — nothing to propose against
	}
	variant.Name = name
	variant.Synthesized = true
	if variant.Tier == "" {
		variant.Tier = incumbent.Tier
	}
	incScore, varScore := score(incumbent), score(variant)
	p := Proposal{
		SkillName:      name,
		Variant:        variant,
		IncumbentScore: incScore,
		VariantScore:   varScore,
		Improvement:    varScore > incScore,
	}
	verifyOK, why := r.Verify(name, variant.Body)
	_, expandErr := r.Expand(variant)
	p.Applicable = p.Improvement && verifyOK && expandErr == nil
	switch {
	case !p.Improvement:
		p.Rationale = "no improvement on the fast eval — do not promote"
	case !verifyOK:
		p.Rationale = "improves the eval but fails verification: " + why
	case expandErr != nil:
		p.Rationale = "improves the eval but does not expand: " + expandErr.Error()
	default:
		p.Rationale = "strict improvement, clean — eligible for review/approval"
	}
	return p, true
}
