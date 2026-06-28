package estimate

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/grounding"
)

func enabledEstimator() *Estimator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	return New(cfg, nil)
}

// TestNote_NeverLowersVariance is THE most important unit test (the §0 invariant): a self-restatement
// (Note) updates the mean but MUST NOT shrink the belief variance. Only a grounded Observe() may. If
// this ever fails, the estimator reproduces the measured overconfidence (EKF gains spurious
// information in the unobservable direction) and the whole calibration win inverts.
func TestNote_NeverLowersVariance(t *testing.T) {
	e := enabledEstimator()
	id := FromThoughtID(7)

	e.Note(id, 0.6)
	_, v0 := e.Trust(id)
	if v0 != e.cfg.PriorVar0 {
		t.Fatalf("a new belief must start at PriorVar0 (%v); got %v", e.cfg.PriorVar0, v0)
	}
	// re-state the belief MORE confidently many times — variance must NOT drop.
	for i := 0; i < 20; i++ {
		e.Note(id, 0.99)
		_, v := e.Trust(id)
		if v < v0 {
			t.Fatalf("Note must never lower variance (§0): restatement %d dropped var %v -> %v", i, v0, v)
		}
	}
}

// TestObserve_IsTheOnlyVarReducer is the dual: a grounded observation is the ONLY thing that shrinks
// the variance. After Observe the belief is more certain; restating it afterward never shrinks it
// further.
func TestObserve_IsTheOnlyVarReducer(t *testing.T) {
	e := enabledEstimator()
	id := FromThoughtID(3)
	e.Note(id, 0.8)
	_, before := e.Trust(id)

	r := e.Observe(id, 1.0, control.TierPrecision(4)) // reality confirms, at a high tier
	if r.Gated {
		t.Fatalf("a confirming obs must not be gated")
	}
	_, after := e.Trust(id)
	if after >= before {
		t.Fatalf("Observe must shrink variance: before=%v after=%v", before, after)
	}
	// a subsequent restatement must NOT shrink it further (§0).
	e.Note(id, 0.99)
	_, post := e.Trust(id)
	if post < after {
		t.Fatalf("a restatement AFTER grounding must not shrink var: %v -> %v", after, post)
	}
}

// TestTrust_FEJAnchorIgnoresLaterRestatement is the FEJ (First-Estimates-Jacobian / P2) thinking:
// Trust() reads the belief's FIRST grounding, NOT the latest self-reinforced restatement. So a
// confidently-restated-but-since-ungrounded belief keeps the trusted (anchored) variance — the Filter
// cannot be talked into trusting a laundered hallucination by restatement.
func TestTrust_FEJAnchorIgnoresLaterRestatement(t *testing.T) {
	e := enabledEstimator()
	id := FromThoughtID(11)
	e.Note(id, 0.5)
	e.Observe(id, 1.0, control.TierPrecision(3)) // FIRST grounding -> anchors
	anchorMean, anchorVar := e.Trust(id)

	// now the model "restates" the belief much more confidently (no new grounding).
	for i := 0; i < 10; i++ {
		e.Note(id, 0.999)
	}
	m, v := e.Trust(id)
	if m != anchorMean || v != anchorVar {
		t.Fatalf("Trust must read the FEJ anchor, not the restatement: anchor (%v,%v) vs trust (%v,%v)",
			anchorMean, anchorVar, m, v)
	}
}

// TestDisabled_IsInertAndSilent: a disabled estimator does nothing observable — Note/Observe are
// no-ops, Trust/Vitals report zero. (The byte-identical-OFF guarantee at the unit level.)
func TestDisabled_IsInertAndSilent(t *testing.T) {
	emits := 0
	cfg := DefaultConfig() // Enabled: false
	e := New(cfg, func(kind, summary string, d map[string]any) events.Event { emits++; _ = kind; return events.Event{} })
	id := FromThoughtID(1)
	e.Note(id, 0.9)
	r := e.Observe(id, -1.0, control.TierPrecision(5))
	if r != (control.Residual{}) {
		t.Fatalf("a disabled Observe must return the zero residual; got %+v", r)
	}
	if emits != 0 {
		t.Fatalf("a disabled estimator must emit nothing; emitted %d", emits)
	}
	if b, g, mv := e.Vitals(); b != 0 || g != 0 || mv != 0 {
		t.Fatalf("a disabled estimator's vitals must be zero; got %d/%d/%v", b, g, mv)
	}
}

// TestObserve_EmitsResidualAndCorrection: the observability contract — a grounded observation emits
// estimate.innovate + estimate.correct; a Mahalanobis-gated one emits estimate.innovate + estimate.gate
// (and NOT correct, because nothing was folded in).
func TestObserve_EmitsResidualAndCorrection(t *testing.T) {
	var kinds []string
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg, func(kind, summary string, d map[string]any) events.Event {
		kinds = append(kinds, kind)
		return events.Event{}
	})

	// associated grounding.
	id := FromThoughtID(2)
	e.Note(id, 0.7)
	e.Observe(id, -1.0, control.TierPrecision(3))
	if !has(kinds, "estimate.innovate") || !has(kinds, "estimate.correct") {
		t.Fatalf("an associated obs must emit innovate+correct; got %v", kinds)
	}
	if has(kinds, "estimate.gate") {
		t.Fatalf("an associated obs must not emit gate; got %v", kinds)
	}

	// gated grounding: a tight, certain prior contradicted hard.
	kinds = nil
	id2 := FromThoughtID(99)
	e.Note(id2, 0.95)
	e.Observe(id2, 1.0, control.TierPrecision(5))  // ground it -> tight
	e.Observe(id2, -1.0, control.TierPrecision(5)) // then contradict hard -> gated
	if !has(kinds, "estimate.gate") {
		t.Fatalf("a Mahalanobis-rejected obs must emit gate; got %v", kinds)
	}
}

// TestDeterministic: identical inputs -> identical state (no RNG, no clock).
func TestDeterministic(t *testing.T) {
	run := func() (float64, float64) {
		e := enabledEstimator()
		id := FromThoughtID(5)
		e.Note(id, 0.6)
		e.Observe(id, 1.0, control.TierPrecision(2))
		e.Note(id, 0.7)
		e.Observe(id, -1.0, control.TierPrecision(4))
		return e.Trust(id)
	}
	m1, v1 := run()
	m2, v2 := run()
	if m1 != m2 || v1 != v2 {
		t.Fatalf("estimator is not deterministic: (%v,%v) vs (%v,%v)", m1, v1, m2, v2)
	}
}

// TestTierPrecisionMatchesGroundingTiers is the cross-package gate: the leaf-purity table in
// internal/control (indexed by ORDINAL) must cover EVERY grounding.TrustTier in iota order. This test
// lives here (it imports grounding) so control stays a leaf while the table can never silently drift
// from the trust ladder it mirrors.
func TestTierPrecisionMatchesGroundingTiers(t *testing.T) {
	tiers := []grounding.TrustTier{
		grounding.TierTestimony,
		grounding.TierWeb,
		grounding.TierAuthoritativeRef,
		grounding.TierFirsthandObservation,
		grounding.TierDeterministic,
		grounding.TierFirsthandValidated,
	}
	if len(tiers) != control.TierCount() {
		t.Fatalf("control.TierCount()=%d but grounding has %d tiers — the leaf table drifted", control.TierCount(), len(tiers))
	}
	// the ordinals must be 0..n-1 in order (the table is indexed by int(tier)).
	for i, tier := range tiers {
		if int(tier) != i {
			t.Fatalf("grounding.TrustTier ordinal mismatch: tiers[%d]=%d (the control table assumes iota order)", i, int(tier))
		}
	}
}

func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
