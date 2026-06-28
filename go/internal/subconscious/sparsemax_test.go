package subconscious

import (
	"math"
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fixedRelSpec is a minimal PrimitiveSubAgent test double with a FIXED relevance and a one-line candidate.
// It is the controlled "key" for the sparsemax A/B: each fixture pins an exact relevance so the simplex
// projection's behaviour is deterministic and the worked example from the design doc is reproduced exactly.
type fixedRelSpec struct {
	domain string
	rel    float64
}

func (s *fixedRelSpec) Domain() string                        { return s.domain }
func (s *fixedRelSpec) Relevance(ctx []types.Thought) float64 { return s.rel }
func (s *fixedRelSpec) Fire(ctx []types.Thought, rng *cpyrand.Random) *types.Candidate {
	return cand(s.domain, s.domain+" says something", s.rel)
}

func fixedRoster(specs ...*fixedRelSpec) []PrimitiveSubAgent {
	out := make([]PrimitiveSubAgent, len(specs))
	for i, s := range specs {
		out[i] = s
	}
	return out
}

// --- the sparsemax math (closed-form, Martins & Astudillo 2016) -------------------------------------

// TestSparsemaxClosedForm checks the projection against hand-computed cases and the simplex invariants:
// the output is non-negative, sums to 1, has exact zeros, and the support is data-adaptive.
func TestSparsemaxClosedForm(t *testing.T) {
	const eps = 1e-9
	cases := []struct {
		name        string
		z           []float64
		wantSupport int
		wantP       []float64 // expected mass, in input order (nil ⇒ only check invariants)
		wantTau     float64   // expected τ (NaN ⇒ skip the τ check)
	}{
		{
			// Two dominant peers far above a weak third: the weak one is zeroed out, the two peers split
			// the mass. z=[0.95,0.95,0.5]: sorted [0.95,0.95,0.5]. j=1: 1+0.95=1.95>0.95 ✓. j=2:
			// 1+2*0.95=2.9>1.9 ✓. j=3: 1+3*0.5=2.5>2.4 ✓ — so all three would be in support... but check:
			// for j=3, cumsum=2.4, 1+3*0.5=2.5>2.4, so k=3, τ=(2.4-1)/3=0.4667, p3=0.5-0.4667=0.0333>0.
			// So at this spread the weak one is NOT fully zeroed. Use a wider spread below for the zero.
			name:        "two peers + a near weak third (support 3)",
			z:           []float64{0.95, 0.95, 0.5},
			wantSupport: 3,
			wantTau:     (0.95 + 0.95 + 0.5 - 1) / 3,
		},
		{
			// A wider spread DOES zero the weak third. z=[0.95,0.95,0.2]: j=3 cumsum=2.1, 1+3*0.2=1.6, NOT
			// >2.1, so j=3 fails; k=2, τ=(1.9-1)/2=0.45. p=[0.5,0.5,0]. The weak 0.2 is an exact zero.
			name:        "two peers dominate, weak third zeroed (support 2)",
			z:           []float64{0.95, 0.95, 0.2},
			wantSupport: 2,
			wantP:       []float64{0.5, 0.5, 0.0},
			wantTau:     0.45,
		},
		{
			// One clear winner: z=[0.9,0.3,0.1]. j=1:1+0.9=1.9>0.9✓. j=2:1+2*0.3=1.6>1.2✓. j=3:1+0.3=1.3,
			// cumsum=1.3, 1.3>1.3 is FALSE → k=2, τ=(1.2-1)/2=0.1. p=[0.8,0.2,0]. The 0.1 is zeroed.
			name:        "clear winner + runner-up (support 2)",
			z:           []float64{0.9, 0.3, 0.1},
			wantSupport: 2,
			wantP:       []float64{0.8, 0.2, 0.0},
			wantTau:     0.1,
		},
		{
			// A uniform field: every key equal ⇒ every key shares the mass (full support, no zeros).
			name:        "uniform field (full support)",
			z:           []float64{0.5, 0.5, 0.5, 0.5},
			wantSupport: 4,
			wantP:       []float64{0.25, 0.25, 0.25, 0.25},
		},
		{
			// A single key takes all the mass (degenerate but correct).
			name:        "single key",
			z:           []float64{0.7},
			wantSupport: 1,
			wantP:       []float64{1.0},
			wantTau:     0.7 - 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := sparsemax(c.z)
			// Invariants: non-negative, sums to 1, support count matches |p_i>0|.
			sum, sup := 0.0, 0
			for _, p := range res.p {
				if p < -eps {
					t.Fatalf("negative mass %v in %v", p, res.p)
				}
				sum += p
				if p > 0 {
					sup++
				}
			}
			if math.Abs(sum-1.0) > 1e-6 {
				t.Fatalf("mass sums to %v, want 1.0 (p=%v)", sum, res.p)
			}
			if sup != res.support {
				t.Fatalf("res.support=%d but |p_i>0|=%d", res.support, sup)
			}
			if res.support != c.wantSupport {
				t.Fatalf("support=%d, want %d (p=%v, τ=%v)", res.support, c.wantSupport, res.p, res.tau)
			}
			if c.wantP != nil {
				for i := range c.wantP {
					if math.Abs(res.p[i]-c.wantP[i]) > 1e-6 {
						t.Fatalf("p[%d]=%v, want %v (full p=%v)", i, res.p[i], c.wantP[i], res.p)
					}
				}
			}
			if !math.IsNaN(c.wantTau) && c.wantTau != 0 {
				if math.Abs(res.tau-c.wantTau) > 1e-6 {
					t.Fatalf("τ=%v, want %v", res.tau, c.wantTau)
				}
			}
		})
	}
}

// TestSparsemaxEmpty handles the empty input (no specialists scored) — a zero-valued result, support 0.
func TestSparsemaxEmpty(t *testing.T) {
	res := sparsemax(nil)
	if res.support != 0 || len(res.p) != 0 || res.tau != 0 {
		t.Fatalf("empty input must give zero result, got %+v", res)
	}
}

// TestSparsemaxDeterministicTies asserts the projection is order-stable under ties: two equal scores get
// the SAME mass regardless of input order, and the support is reproducible (the stable-sort + index
// tiebreak). This is the determinism the dispatch loop relies on (no RNG, no clock).
func TestSparsemaxDeterministicTies(t *testing.T) {
	a := sparsemax([]float64{0.8, 0.8, 0.3})
	b := sparsemax([]float64{0.8, 0.8, 0.3})
	for i := range a.p {
		if a.p[i] != b.p[i] {
			t.Fatalf("non-deterministic on identical input: %v vs %v", a.p, b.p)
		}
	}
	// equal scores ⇒ equal mass
	if a.p[0] != a.p[1] {
		t.Fatalf("tied scores got unequal mass: %v", a.p)
	}
}

// --- the dispatch A/B (hard θ-gate vs sparsemax, SAME ctx) ------------------------------------------

// TestSparseDispatchABFiresCompetitiveFew is the core behavioural proof (design §4 worked example): on the
// SAME ctx and θ, the HARD per-key absolute gate admits a WEAK specialist purely because it clears the
// fixed bar, while SPARSEMAX zeros it out because two strong competitors dominate the simplex projection —
// "fire a few relative to the field". Roster: compute(0.95), social(0.95), skeptic(0.2); θ=0.15 (a low
// absolute bar all three clear). Hard gate ⇒ all 3 fire; sparse ⇒ only the two peers (skeptic zeroed).
func TestSparseDispatchABFiresCompetitiveFew(t *testing.T) {
	roster := func() []PrimitiveSubAgent {
		return fixedRoster(
			&fixedRelSpec{domain: "compute", rel: 0.95},
			&fixedRelSpec{domain: "social", rel: 0.95},
			&fixedRelSpec{domain: "skeptic", rel: 0.2},
		)
	}
	ctx := dispatchCtx([]string{"a query that lights several faculties"})
	const theta = 0.15

	// HARD path (sparse OFF — the absolute gate).
	hard := NewSubconsciousEngine(roster(), cpyrand.New(1), noopEmit, nil, nil)
	hardFired, _ := hard.Dispatch(ctx, theta, nil)
	hardSet := firedDomains(hardFired)

	// SPARSE path (same ctx, same θ).
	sparse := NewSubconsciousEngine(roster(), cpyrand.New(1), noopEmit, nil, nil)
	sparse.SetSparseDispatch(true)
	sparseFired, _ := sparse.Dispatch(ctx, theta, nil)
	sparseSet := firedDomains(sparseFired)

	// The hard gate fires a WEAK-ABSOLUTE set: all three (everyone over the fixed bar).
	if len(hardSet) != 3 {
		t.Fatalf("HARD θ-gate: want 3 fired (the weak-absolute set), got %d: %v", len(hardSet), hardSet)
	}
	// Sparsemax fires a COMPETITIVE FEW: the two peers only; the weak skeptic is zeroed by the projection.
	if len(sparseSet) != 2 {
		t.Fatalf("SPARSE: want 2 fired (the competitive few), got %d: %v", len(sparseSet), sparseSet)
	}
	wantSparse := []string{"compute", "social"}
	if !equalStrs(sparseSet, wantSparse) {
		t.Fatalf("SPARSE fired %v, want %v (the two dominant peers, skeptic zeroed)", sparseSet, wantSparse)
	}
	// The sparse set must be a STRICT SUBSET of the hard set here (sparse is more selective on this field).
	if len(sparseSet) >= len(hardSet) {
		t.Fatalf("expected sparse (%v) to be MORE selective than hard (%v)", sparseSet, hardSet)
	}

	// The surviving candidates carry the stamped sparsemax dispatch confidence p_i in (0,1], summing to ~1.
	var massSum float64
	for _, c := range sparseFired {
		if c.DispatchWeight <= 0 || c.DispatchWeight > 1 {
			t.Fatalf("fired candidate %q has out-of-range DispatchWeight %v", deref(c.Domain), c.DispatchWeight)
		}
		massSum += c.DispatchWeight
	}
	if math.Abs(massSum-1.0) > 1e-6 {
		t.Fatalf("stamped dispatch confidence over the fired set sums to %v, want ~1.0", massSum)
	}
}

// TestSparseDispatchThetaFloorPreservesQuiet asserts θ SURVIVES AS A FLOOR under the induced τ: a uniformly
// WEAK field whose every member is BELOW θ goes QUIET under sparsemax (→ Conscious generates), exactly as it
// does under the hard gate. Without the floor, sparsemax would always admit at least one (it sums to 1), so
// a weak tick could never be quiet — the floor preserves the emitQuiet path.
func TestSparseDispatchThetaFloorPreservesQuiet(t *testing.T) {
	roster := fixedRoster(
		&fixedRelSpec{domain: "compute", rel: 0.10},
		&fixedRelSpec{domain: "social", rel: 0.08},
		&fixedRelSpec{domain: "skeptic", rel: 0.05},
	)
	ctx := dispatchCtx([]string{"a weak query nobody is confident about"})
	const theta = 0.3 // every score is below θ

	sparse := NewSubconsciousEngine(roster, cpyrand.New(1), noopEmit, nil, nil)
	sparse.SetSparseDispatch(true)
	fired, _ := sparse.Dispatch(ctx, theta, nil)
	if len(fired) != 0 {
		t.Fatalf("uniformly-weak field below θ must go QUIET under the floor, but %d fired: %v",
			len(fired), firedDomains(fired))
	}
}

// TestSparseDispatchWeakFieldStillFires asserts the OTHER half of the floor story: when the whole field is
// modest but ABOVE θ, the best one(s) still fire — sparsemax does NOT produce a dead tick on a weak-but-
// admissible field (τ drops with the field).
func TestSparseDispatchWeakFieldStillFires(t *testing.T) {
	roster := fixedRoster(
		&fixedRelSpec{domain: "compute", rel: 0.55},
		&fixedRelSpec{domain: "social", rel: 0.50},
	)
	ctx := dispatchCtx([]string{"a modest but admissible query"})
	const theta = 0.3

	sparse := NewSubconsciousEngine(roster, cpyrand.New(1), noopEmit, nil, nil)
	sparse.SetSparseDispatch(true)
	fired, _ := sparse.Dispatch(ctx, theta, nil)
	if len(fired) == 0 {
		t.Fatal("a weak-but-above-θ field must still fire the best one(s) — no dead tick")
	}
}

// TestSparseDispatchEmitsEvent confirms the sparse admission is OBSERVABLE: subconscious.sparse carries the
// induced τ, the θ floor, the support, the candidate count, and the per-key weights. (The OFF path emits
// none of these.)
func TestSparseDispatchEmitsEvent(t *testing.T) {
	roster := fixedRoster(
		&fixedRelSpec{domain: "compute", rel: 0.95},
		&fixedRelSpec{domain: "social", rel: 0.95},
		&fixedRelSpec{domain: "skeptic", rel: 0.2},
	)
	ctx := dispatchCtx([]string{"a query"})

	var got []events.Event
	rec := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		got = append(got, ev)
		return ev
	}
	eng := NewSubconsciousEngine(roster, cpyrand.New(1), rec, nil, nil)
	eng.SetSparseDispatch(true)
	eng.Dispatch(ctx, 0.15, nil)

	var sparseEv *events.Event
	for i := range got {
		if got[i].Kind == events.SubSparse {
			sparseEv = &got[i]
			break
		}
	}
	if sparseEv == nil {
		t.Fatal("sparse dispatch must emit subconscious.sparse")
	}
	if sparseEv.Data["support"] != 2 {
		t.Fatalf("sparse event support=%v, want 2", sparseEv.Data["support"])
	}
	if sparseEv.Data["candidates"] != 3 {
		t.Fatalf("sparse event candidates=%v, want 3", sparseEv.Data["candidates"])
	}
	if _, ok := sparseEv.Data["tau"]; !ok {
		t.Fatal("sparse event must carry the induced τ")
	}
	if _, ok := sparseEv.Data["weights"]; !ok {
		t.Fatal("sparse event must carry the per-key weights")
	}
}

// TestSparseDispatchOffIsByteIdentical is the flag-OFF half: with sparse OFF (the default), the fired SET,
// the stamped Relevance, and the ZERO DispatchWeight are IDENTICAL to a plain engine — and NO
// subconscious.sparse event fires. (The OFF path must be exactly the legacy absolute admission.)
func TestSparseDispatchOffIsByteIdentical(t *testing.T) {
	mk := func() []PrimitiveSubAgent {
		return fixedRoster(
			&fixedRelSpec{domain: "compute", rel: 0.95},
			&fixedRelSpec{domain: "social", rel: 0.7},
			&fixedRelSpec{domain: "skeptic", rel: 0.4},
		)
	}
	ctx := dispatchCtx([]string{"a query"})
	const theta = 0.3

	var offEvents []string
	rec := func(kind, summary string, data events.D) events.Event {
		offEvents = append(offEvents, kind)
		return events.Event{}
	}
	// the baseline (no setter ever called) and an explicitly-OFF engine must agree
	base := NewSubconsciousEngine(mk(), cpyrand.New(1), noopEmit, nil, nil)
	off := NewSubconsciousEngine(mk(), cpyrand.New(1), rec, nil, nil)
	off.SetSparseDispatch(false)

	baseFired, _ := base.Dispatch(ctx, theta, nil)
	offFired, _ := off.Dispatch(ctx, theta, nil)

	if !equalStrs(firedDomains(baseFired), firedDomains(offFired)) {
		t.Fatalf("OFF fired set %v != baseline %v", firedDomains(offFired), firedDomains(baseFired))
	}
	for _, c := range offFired {
		if c.DispatchWeight != 0 {
			t.Fatalf("OFF path stamped a non-zero DispatchWeight %v on %q", c.DispatchWeight, deref(c.Domain))
		}
	}
	for _, k := range offEvents {
		if k == string(events.SubSparse) {
			t.Fatal("OFF path must NOT emit subconscious.sparse")
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac, bc := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
