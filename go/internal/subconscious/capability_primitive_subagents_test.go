package subconscious

import (
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// TestPrimitiveSubAgentGateIsLiveFiringEntry is the GAP 5-DEEPER PART 2 wiring gate (subconscious side): when a
// PrimitiveSubAgentGate is wired onto the SubconsciousEngine, the dispatch loop routes EVERY base specialist's
// admission THROUGH it — the Capability is the live SPECIALIST-firing ENTRY, not the bare relevance gate.
// The proof is a SPY gate: the engine must call AdmitPrimitiveSubAgent on it for the specialists the relevance
// gate already admitted (the entry fires). With no gate (the default), the spy is never consulted (the
// bare relevance firing).
func TestPrimitiveSubAgentGateIsLiveFiringEntry(t *testing.T) {
	// a real roster: compute (always live, lights on a binary expression) + the two stance roles, so several
	// domains fire at a low theta.
	caller := &fakeCaller{out: "a reasoned stance", ok: true}
	roster := DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false)

	spy := &spySpecGate{admit: true}
	eng := NewSubconsciousEngine(roster, cpyrand.New(1234), noopEmit, nil, nil)
	eng.SetPrimitiveSubAgentGate(spy)
	if eng.PrimitiveSubAgentGate() == nil {
		t.Fatal("PrimitiveSubAgentGate() must return the wired entry (the live-entry accessor)")
	}

	ctx := dispatchCtx([]string{"is it safe to refactor; also what is 2 + 2?"})
	fired, _ := eng.Dispatch(ctx, 0.3, nil)
	if len(fired) == 0 {
		t.Fatal("the real roster should fire >=1 specialist at theta=0.3 (fixture precondition)")
	}
	if spy.calls == 0 {
		t.Fatal("the dispatch loop did NOT route specialist admission through the wired entry (the Capability is not the live firing entry)")
	}
	// every domain that fired must have been a domain the gate was consulted on (the entry owns the firing).
	for _, c := range fired {
		if c.Domain == nil {
			continue
		}
		if !spy.consulted[*c.Domain] {
			t.Errorf("specialist %q fired without the entry being consulted (admission bypassed the gate)", *c.Domain)
		}
	}
}

// TestNoPrimitiveSubAgentGateIsBareRelevanceFiring is the flag-OFF half: with NO gate wired (the default), the
// bare relevance gate (eff>theta) fires every admitted specialist — the entry seam is inert. The fired set
// must be IDENTICAL to the no-gate baseline.
func TestNoPrimitiveSubAgentGateIsBareRelevanceFiring(t *testing.T) {
	caller := &fakeCaller{out: "a reasoned stance", ok: true}
	mkEng := func() *SubconsciousEngine {
		roster := DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false)
		return NewSubconsciousEngine(roster, cpyrand.New(1234), noopEmit, nil, nil)
	}
	ctx := dispatchCtx([]string{"is it safe to refactor; also what is 2 + 2?"})

	base := mkEng()
	if base.PrimitiveSubAgentGate() != nil {
		t.Fatal("a fresh engine must have NO specialist gate (the legacy bare-relevance firing is the default)")
	}
	baseFired, _ := base.Dispatch(ctx, 0.3, nil)

	// an ALWAYS-ADMIT gate must produce the SAME fired set as no gate (it only ever DENIES; admit-all is a
	// no-op layered on top of the relevance gate — byte-identical to the bare firing).
	gated := mkEng()
	gated.SetPrimitiveSubAgentGate(&spySpecGate{admit: true})
	gatedFired, _ := gated.Dispatch(ctx, 0.3, nil)

	if !equalStrings(firedDomains(baseFired), firedDomains(gatedFired)) {
		t.Errorf("admit-all gate changed the fired set: no-gate=%v admit-all=%v (the gate must only DENY)",
			firedDomains(baseFired), firedDomains(gatedFired))
	}
}

// TestPrimitiveSubAgentGateDeniesOffBandDomains is the GAP 5-DEEPER PART 2 COGNITION property (not plumbing): a
// DOMAIN-BANDED Capability owns specialist firing and fires ONLY its band's specialists — an off-band
// specialist the bare relevance gate WOULD have admitted is DENIED (it stays dark even above theta). This
// is the architectural delta the keystone names: specialist firing is no longer "fire everything over
// theta", it is "fire what the producing Capability's §3.3a authority admits". It pins the THREE regimes:
//
//   - a GENERAL (empty-domain) Capability admits EVERY domain ⇒ byte-identical to the bare firing (the
//     episode path);
//   - a Capability banded to "compute" admits compute, DENIES the off-band stance roles;
//   - a Capability banded to a domain NOTHING in the roster carries fires NOTHING (the whole roster is
//     off-band) — the least-privilege bite is total.
func TestPrimitiveSubAgentGateDeniesOffBandDomains(t *testing.T) {
	caller := &fakeCaller{out: "a reasoned stance", ok: true}
	mkEng := func() *SubconsciousEngine {
		roster := DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false)
		return NewSubconsciousEngine(roster, cpyrand.New(1234), noopEmit, nil, nil)
	}
	// a stream that fires compute (the "2 + 2") AND the two stance roles (the "safe to refactor" review shape).
	ctx := dispatchCtx([]string{"is it safe to refactor; also what is 2 + 2?"})

	// the bare-firing baseline (no gate) — what the relevance gate admits before any authority check.
	baseFired, _ := mkEng().Dispatch(ctx, 0.3, nil)
	baseDomains := firedDomains(baseFired)
	if len(baseDomains) < 2 {
		t.Fatalf("fixture precondition: the bare firing must admit >=2 domains, got %v", baseDomains)
	}
	hasCompute := false
	for _, d := range baseDomains {
		if d == "compute" {
			hasCompute = true
		}
	}
	if !hasCompute {
		t.Fatalf("fixture precondition: compute must be in the bare-firing set %v", baseDomains)
	}

	cases := []struct {
		name     string
		capScope *Scope
		want     []string // the fired domains under the gate
	}{
		// a GENERAL ceiling (empty domain) admits every domain ⇒ identical to the bare firing.
		{"general-scope-byte-identical", NewScope("", nil, 0), baseDomains},
		// banded to "compute": compute fires, the off-band stance roles are DENIED.
		{"banded-compute-only", NewScope("compute", nil, 0), []string{"compute"}},
		// banded to a domain no specialist carries: the whole roster is off-band ⇒ NOTHING fires.
		{"banded-unknown-fires-nothing", NewScope("nonexistent-domain", nil, 0), nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			capa := &Capability{Name: "banded", Scope: tc.capScope}
			eng := mkEng()
			eng.SetPrimitiveSubAgentGate(capa)
			fired, _ := eng.Dispatch(ctx, 0.3, nil)
			got := firedDomains(fired)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !equalStrings(got, want) {
				t.Errorf("domain-banded gate %q fired %v, want %v (the §3.3a domain band did not gate firing)", tc.name, got, want)
			}
		})
	}
}

// TestCapabilityAdmitPrimitiveSubAgentGatesOnScopeDomain is the GAP 5-DEEPER PART 2 cognition property at the
// predicate level: Capability.AdmitPrimitiveSubAgent gates a specialist on the run's §3.3a Scope DOMAIN band — a
// general (empty-domain) Scope and a nil Scope admit every domain (byte-identical to bare firing), a banded
// Scope admits only its band (case-insensitive) and a domain-less specialist is never excluded by a domain
// band. This pins the safe-stage predicate the dispatch loop layers on top of the relevance gate.
func TestCapabilityAdmitPrimitiveSubAgentGatesOnScopeDomain(t *testing.T) {
	cases := []struct {
		name   string
		scope  *Scope
		domain string
		want   bool
	}{
		{"nil-scope-admits-all", nil, "compute", true},
		{"general-scope-admits-all", NewScope("", nil, 0), "skeptic", true},
		{"banded-admits-matching", NewScope("compute", nil, 0), "compute", true},
		{"banded-admits-matching-case-insensitive", NewScope("Compute", nil, 0), "compute", true},
		{"banded-denies-off-band", NewScope("compute", nil, 0), "skeptic", false},
		{"banded-admits-domainless-worker", NewScope("compute", nil, 0), "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			capa := &Capability{Name: "c", Scope: tc.scope}
			if got := capa.AdmitPrimitiveSubAgent(tc.domain); got != tc.want {
				t.Errorf("AdmitPrimitiveSubAgent(%q) with scope %+v = %v, want %v", tc.domain, tc.scope, got, tc.want)
			}
		})
	}
}

// spySpecGate is a PrimitiveSubAgentGate test double that records every AdmitPrimitiveSubAgent call (which domains were
// consulted) and returns a fixed verdict, so a wiring-gate test can observe the entry was the one consulted.
type spySpecGate struct {
	admit     bool
	calls     int
	consulted map[string]bool
}

func (s *spySpecGate) AdmitPrimitiveSubAgent(domain string) bool {
	s.calls++
	if s.consulted == nil {
		s.consulted = map[string]bool{}
	}
	s.consulted[domain] = true
	return s.admit
}
