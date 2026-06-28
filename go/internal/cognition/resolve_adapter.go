// resolve_adapter.go brings the concrete registries to MINIMAL COMPLETION (P2.2) by adopting the
// uniform Resolve spine (P1's internal/resolve): a registry plus its CREATE step satisfies
// resolve.Registry[T], so the same search→reuse-or-create→verify→store loop runs over a real library.
// "Minimal completion" = the seed spans the space AND the reuse-or-create loop closes from a cold start.
//
// SkillRegistry is wired here (the capability family): Find=Match, Create=the injected synthesiser,
// Verify=Verify, Store=Mint. OperatorRegistry / the memory registries follow the identical shape; this
// is the proof the spine drives a live registry, not just a test double.
package cognition

import "github.com/berttrycoding/thought-harness/internal/resolve"

// skillResolver adapts a SkillRegistry + a create step to the uniform resolve.Registry[Skill] contract.
type skillResolver struct {
	reg    *SkillRegistry
	create func(goal string) (Skill, bool) // the synthesiser (reuse-or-CREATE): build a skill for a goal
}

func (s skillResolver) Find(goal string) (Skill, bool) { return s.reg.Match(goal) }
func (s skillResolver) Create(goal string) (Skill, bool) {
	if s.create == nil {
		return Skill{}, false
	}
	return s.create(goal)
}
func (s skillResolver) Verify(sk Skill) (bool, string) { return s.reg.Verify(sk.Name, sk.Body) }
func (s skillResolver) Store(sk Skill) {
	s.reg.Mint(sk.Name, sk.Triggers, sk.Body, sk.Tier, sk.Description)
}

// Resolver returns a resolve.Registry[Skill] view of this registry: reuse a matching library skill, else
// synthesise one via create, verify, and mint it. create may be nil (reuse-only). This is how the engine
// runs `resolve.Resolve(reg.Resolver(synth), goal)` to get library-first, synthesise-fallback behaviour.
func (r *SkillRegistry) Resolver(create func(goal string) (Skill, bool)) resolve.Registry[Skill] {
	return skillResolver{reg: r, create: create}
}
