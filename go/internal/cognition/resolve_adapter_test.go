package cognition

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/resolve"
)

// TestResolveClosesLoopOnRealRegistry is the P2.2 gate: the uniform Resolve spine closes the
// reuse-or-create loop on a REAL SkillRegistry from a cold start — first ask synthesises + mints, the
// next ask reuses the minted skill (no second create).
func TestResolveClosesLoopOnRealRegistry(t *testing.T) {
	reg := NewSkillRegistry(true)
	creates := 0
	// the "synthesiser": builds a minted skill whose triggers are the goal's words, so a re-ask matches.
	create := func(goal string) (Skill, bool) {
		creates++
		return Skill{
			Name:     "learned-" + strings.ReplaceAll(strings.Fields(goal)[0], "-", ""),
			Tier:     "composite",
			Triggers: strings.Fields(goal),
			Body:     seedProgramSynth(NewSeq(NewStep("decompose", "general", ""), NewStep("generate", "general", ""))),
		}, true
	}
	res := reg.Resolver(create)

	goal := "frobnicate the widget pipeline"

	it, out, _ := resolve.Resolve[Skill](res, goal)
	if out != resolve.Created {
		t.Fatalf("a cold registry must CREATE on the first miss; got %v", out)
	}
	if creates != 1 {
		t.Fatalf("create should have run once; ran %d", creates)
	}
	if !reg.Has(it.Name) {
		t.Fatalf("the created skill should be minted into the library; %q missing", it.Name)
	}

	// re-ask the same goal -> reuse the minted skill, no second create (the loop closed).
	_, out2, _ := resolve.Resolve[Skill](res, goal)
	if out2 != resolve.Reused {
		t.Fatalf("a re-ask must REUSE the minted skill; got %v", out2)
	}
	if creates != 1 {
		t.Fatalf("reuse must not synthesise again; creates=%d", creates)
	}
}

// TestResolveReuseOnlyWhenNoCreator: with no synthesiser, Resolve reuses a match but cannot create —
// it returns Failed on a miss (never fabricates a capability).
func TestResolveReuseOnlyWhenNoCreator(t *testing.T) {
	reg := NewSkillRegistry(true)
	res := reg.Resolver(nil) // reuse-only

	// a seeded composite is reused.
	if _, out, _ := resolve.Resolve[Skill](res, "why is the service down — diagnose the outage"); out != resolve.Reused {
		t.Fatalf("a goal matching a seed composite should reuse; got %v", out)
	}
	// a goal with no match and no creator fails (no fabrication).
	if _, out, _ := resolve.Resolve[Skill](res, "zzqq nonsense goal xyz"); out != resolve.Failed {
		t.Fatalf("no match + no creator must Fail, not fabricate; got %v", out)
	}
}
