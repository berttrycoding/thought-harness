package route

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// TestFloorTier: the deterministic FLOOR reproduces the pre-router tiered split exactly — only the
// trivial roles (summarize/compress) route to utility; every reasoning role stays primary.
func TestFloorTier(t *testing.T) {
	cases := []struct {
		role string
		want Tier
	}{
		{"conscious.compress", Utility},
		{"summarize", Utility},
		{"conscious.generate", Primary},
		{"seam.transform", Primary},
		{"action.respond", Primary},
		{"operator.deliberator", Primary},
		{"decide", Primary},
		{"judge_admission", Primary},
	}
	for _, c := range cases {
		if got := FloorTier(c.role); got != c.want {
			t.Errorf("FloorTier(%q) = %v, want %v", c.role, got, c.want)
		}
	}
}

// TestRouterDisabledIsFloor: with the router OFF (the default), Decide returns the FLOOR for every
// call, the policy is never consulted, and Flagged is false — the byte-identical fallback. This is the
// invariant that makes flag-OFF byte-identical.
func TestRouterDisabledIsFloor(t *testing.T) {
	r := NewRouter(false, NewValuePolicy(), true)
	// a clearly-HARD generate (high value, long prompt) STILL stays on the floor when the router is off.
	d := r.Decide(Signal{Role: "conscious.generate", Value: 0.95, PromptLen: 9000})
	if d.Tier != Primary || d.Reason != ReasonFloor || d.Flagged {
		t.Fatalf("disabled router: got tier=%v reason=%v flagged=%v, want primary/floor/false", d.Tier, d.Reason, d.Flagged)
	}
	// a clearly-EASY summarize stays on its floor (utility) too — no policy consulted.
	d = r.Decide(Signal{Role: "conscious.compress", Value: 0.05, PromptLen: 10})
	if d.Tier != Utility || d.Reason != ReasonFloor {
		t.Fatalf("disabled router (summarize): got tier=%v reason=%v, want utility/floor", d.Tier, d.Reason)
	}
	// a nil router is also the floor (defensive).
	var nilR *Router
	if d := nilR.Decide(Signal{Role: "action.respond"}); d.Tier != Primary || d.Reason != ReasonFloor {
		t.Fatalf("nil router: got tier=%v reason=%v, want primary/floor", d.Tier, d.Reason)
	}
}

// TestFlag: the deterministic escalation-eligibility gate flags exactly the uncertain cases —
// a hard utility-floored call, an easy primary-floored call, a long-prompt utility call — and leaves a
// typical call unflagged.
func TestFlag(t *testing.T) {
	// utility floor + HIGH value ⇒ flagged (escalation-eligible).
	if !Flag(Utility, Signal{Role: "summarize", Value: 0.9}) {
		t.Error("hard summarize should be flagged")
	}
	// utility floor + long prompt ⇒ flagged.
	if !Flag(Utility, Signal{Role: "summarize", PromptLen: 5000}) {
		t.Error("long-prompt summarize should be flagged")
	}
	// utility floor + low value, short prompt ⇒ NOT flagged (typical cheap call).
	if Flag(Utility, Signal{Role: "summarize", Value: 0.1, PromptLen: 50}) {
		t.Error("typical summarize should NOT be flagged")
	}
	// primary floor + LOW value ⇒ flagged (downgrade-eligible).
	if !Flag(Primary, Signal{Role: "operator.x", Value: 0.1}) {
		t.Error("easy operator call should be flagged")
	}
	// primary floor + UNKNOWN value (0) ⇒ NOT flagged (unknown is not easy — never spuriously downgrade).
	if Flag(Primary, Signal{Role: "conscious.generate", Value: 0}) {
		t.Error("unknown-value generate should NOT be flagged (0 is unknown, not easy)")
	}
	// primary floor + mid value ⇒ NOT flagged (typical reasoning call).
	if Flag(Primary, Signal{Role: "conscious.generate", Value: 0.5}) {
		t.Error("mid-value generate should NOT be flagged")
	}
}

// TestEscalateFlaggedHard: a flagged-HARD utility-floored call escalates to primary under a policy
// that wants the hard call on the big model.
func TestEscalateFlaggedHard(t *testing.T) {
	r := NewRouter(true, NewValuePolicy(), true)
	// a HARD summarize (high value) — floor=utility, flagged, the value policy escalates to primary.
	d := r.Decide(Signal{Role: "conscious.compress", Value: 0.9, PromptLen: 100})
	if d.FloorTier != Utility {
		t.Fatalf("floor should be utility, got %v", d.FloorTier)
	}
	if d.Tier != Primary || d.Reason != ReasonEscalated || !d.Flagged {
		t.Fatalf("hard summarize: got tier=%v reason=%v flagged=%v, want primary/escalated/true", d.Tier, d.Reason, d.Flagged)
	}
}

// TestDowngradeFlaggedEasy: a flagged-EASY primary-floored BACKGROUND call (operator) downgrades to
// utility under a policy that wants the easy call on the cheap model.
func TestDowngradeFlaggedEasy(t *testing.T) {
	r := NewRouter(true, NewValuePolicy(), true)
	d := r.Decide(Signal{Role: "operator.deliberator", Value: 0.1, PromptLen: 100})
	if d.FloorTier != Primary {
		t.Fatalf("floor should be primary, got %v", d.FloorTier)
	}
	if d.Tier != Utility || d.Reason != ReasonDowngraded || !d.Flagged {
		t.Fatalf("easy operator: got tier=%v reason=%v flagged=%v, want utility/downgraded/true", d.Tier, d.Reason, d.Flagged)
	}
}

// TestStructuralPinNeverDowngraded: respond (and decide) are structurally pinned to primary — a
// flagged-easy respond is NOT downgraded; the floor stands and the reason is surfaced (Rule 4).
func TestStructuralPinNeverDowngraded(t *testing.T) {
	r := NewRouter(true, NewValuePolicy(), true)
	d := r.Decide(Signal{Role: "action.respond", Value: 0.05, PromptLen: 100})
	if d.Tier != Primary {
		t.Fatalf("respond must STAY primary (structural pin), got %v", d.Tier)
	}
	if d.Reason != ReasonStructural || !d.Flagged {
		t.Fatalf("respond downgrade-attempt: got reason=%v flagged=%v, want structural/true (Rule 4 surfaced)", d.Reason, d.Flagged)
	}
	// decide is pinned too.
	d = r.Decide(Signal{Role: "critic.decide", Value: 0.05})
	if d.Tier != Primary || d.Reason != ReasonStructural {
		t.Fatalf("decide downgrade-attempt: got tier=%v reason=%v, want primary/structural", d.Tier, d.Reason)
	}
}

// TestNoUtilityTierClampsToFloor: a downgrade pick with no utility tier wired (single-model config)
// clamps to the floor (primary) and surfaces no-utility.
func TestNoUtilityTierClampsToFloor(t *testing.T) {
	r := NewRouter(true, NewValuePolicy(), false /* utility NOT wired */)
	d := r.Decide(Signal{Role: "operator.x", Value: 0.1})
	if d.Tier != Primary || d.Reason != ReasonNoUtility {
		t.Fatalf("no-utility downgrade: got tier=%v reason=%v, want primary/no-utility", d.Tier, d.Reason)
	}
}

// TestLocalPickClampsToFloor: a policy that returns the (not-yet-wired) Local tier clamps back to the
// floor — the W6 re-localization seam compiles without a live local tier.
func TestLocalPickClampsToFloor(t *testing.T) {
	r := NewRouter(true, localPolicy{}, true)
	d := r.Decide(Signal{Role: "conscious.compress", Value: 0.9}) // flagged-hard so the policy is consulted
	if d.Tier != Utility || d.Reason != ReasonFloor {
		t.Fatalf("local pick should clamp to floor (utility for summarize), got tier=%v reason=%v", d.Tier, d.Reason)
	}
}

// localPolicy always returns Local — a stand-in for a future local-tier policy.
type localPolicy struct{}

func (localPolicy) Route(Tier, Signal) (Tier, float64) { return Local, 0.5 }
func (localPolicy) Update(Tier, Signal, float64)       {}
func (localPolicy) Name() string                       { return "local-test" }

// TestThompsonDeterministicUnderSeed: the Thompson policy is reproducible — two policies on the SAME
// seed make the SAME sequence of route decisions (seeded RNG, no clock).
func TestThompsonDeterministicUnderSeed(t *testing.T) {
	seq := func() []Tier {
		r := NewRouter(true, NewThompsonPolicy(cpyrand.New(99)), true)
		var out []Tier
		for i := 0; i < 20; i++ {
			d := r.Decide(Signal{Role: "operator.x", Value: 0.1}) // flagged-easy ⇒ policy consulted
			out = append(out, d.Tier)
		}
		return out
	}
	a, b := seq(), seq()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("Thompson route decision %d diverged: %v vs %v (must be deterministic under a seed)", i, a[i], b[i])
		}
	}
}

// TestThompsonLearnsFromReward: the contextual bandit shifts toward the tier that earns reward. Reward
// the UTILITY arm heavily in a bucket and the posterior mean for utility must rise above primary —
// the keep-or-revert-gated "learn the cost-cheap tier where it earns its quality" mechanism.
func TestThompsonLearnsFromReward(t *testing.T) {
	p := NewThompsonPolicy(cpyrand.New(7))
	s := Signal{Role: "operator.x", Value: 0.1} // a fixed bucket
	// utility wins (reward 1.0), primary loses (reward 0.0) in this bucket, many times.
	for i := 0; i < 200; i++ {
		p.Update(Utility, s, 1.0)
		p.Update(Primary, s, 0.0)
	}
	a := p.armOf(s)
	uMean := a.alpha[0] / (a.alpha[0] + a.beta[0])
	pMean := a.alpha[1] / (a.alpha[1] + a.beta[1])
	if uMean <= pMean {
		t.Fatalf("after rewarding utility, its posterior mean (%.3f) should exceed primary's (%.3f)", uMean, pMean)
	}
	// and sampling should now strongly prefer utility in that bucket.
	util := 0
	for i := 0; i < 200; i++ {
		if tier, _ := p.Route(Primary, s); tier == Utility {
			util++
		}
	}
	if util < 150 {
		t.Fatalf("after learning, utility should be picked the large majority; got %d/200", util)
	}
}

// TestCostDisciplineCognition is the COGNITIVE-PROPERTY test (the *thinking* the router intends, not
// just the plumbing): the RouteLLM cognition is "the CHEAP floor carries the common case; the strong
// model is reached only on the genuinely-hard call". Over a realistic MIX of CONTENT calls (mostly
// typical-difficulty), the policy is consulted ONLY on the flagged minority, and the common typical
// call rides the FLOOR untouched (zero cost added on the hot path). A router that flagged everything
// (consulting the policy on every call) or escalated everything (defeating the cost win) would FAIL
// this — those are the failure modes the intent guards against.
func TestCostDisciplineCognition(t *testing.T) {
	// A policy that COUNTS how often it is consulted — the cognition we assert is that the floor, not
	// the policy, decides the common case.
	counting := &countingPolicy{inner: NewValuePolicy()}
	r := NewRouter(true, counting, true)

	// A realistic mix: 100 typical calls (mid value, normal prompt) + a handful of genuinely-hard /
	// genuinely-easy ones. The typical calls must NOT consult the policy (the floor carries them).
	typical := 100
	for i := 0; i < typical; i++ {
		d := r.Decide(Signal{Role: "conscious.generate", Value: 0.5, PromptLen: 300})
		if d.Reason != ReasonFloor || d.Flagged {
			t.Fatalf("a TYPICAL reasoning call must ride the floor unflagged (no policy cost on the hot path); got reason=%v flagged=%v", d.Reason, d.Flagged)
		}
	}
	if counting.calls != 0 {
		t.Fatalf("cost discipline: the policy must NOT be consulted on typical calls (the floor carries them); it was consulted %d times", counting.calls)
	}

	// Now the genuinely-hard minority — these SHOULD reach the policy (and escalate). The cognition:
	// the strong model is reached ONLY here, not on the common case.
	hard := 5
	escalated := 0
	for i := 0; i < hard; i++ {
		d := r.Decide(Signal{Role: "conscious.compress", Value: 0.9, PromptLen: 100}) // hard summarize
		if d.Tier == Primary && d.Reason == ReasonEscalated {
			escalated++
		}
	}
	if counting.calls != hard {
		t.Fatalf("the policy should be consulted EXACTLY on the %d flagged-hard calls, got %d", hard, counting.calls)
	}
	if escalated != hard {
		t.Fatalf("every genuinely-hard call should escalate to the strong model, got %d/%d", escalated, hard)
	}
	// The headline cost-discipline ratio: the policy touched only the hard minority — the cheap floor
	// carried 100 of 105 decisions with zero added cost. That IS the RouteLLM intent.
	if frac := float64(counting.calls) / float64(typical+hard); frac > 0.2 {
		t.Fatalf("cost discipline violated: the policy was consulted on %.0f%% of calls; the floor must carry the large majority", frac*100)
	}
}

// countingPolicy wraps a RoutePolicy and counts Route consultations — to assert the cost-discipline
// cognition (the floor, not the policy, carries the common case).
type countingPolicy struct {
	inner RoutePolicy
	calls int
}

func (c *countingPolicy) Route(floor Tier, s Signal) (Tier, float64) {
	c.calls++
	return c.inner.Route(floor, s)
}
func (c *countingPolicy) Update(t Tier, s Signal, r float64) { c.inner.Update(t, s, r) }
func (c *countingPolicy) Name() string                       { return "counting(" + c.inner.Name() + ")" }

// TestBetaSampleBounded: the Beta sampler returns a value in (0,1) for a range of shapes and never
// loops unbounded (the bounded gamma rejection exit) — a guard against an unseeded/runaway draw.
func TestBetaSampleBounded(t *testing.T) {
	rng := cpyrand.New(3)
	for _, ab := range [][2]float64{{1, 1}, {0.5, 2}, {10, 1}, {1, 10}, {0.1, 0.1}} {
		for i := 0; i < 100; i++ {
			v := betaSample(rng, ab[0], ab[1])
			if v < 0 || v > 1 {
				t.Fatalf("betaSample(%v) = %f out of [0,1]", ab, v)
			}
		}
	}
}
