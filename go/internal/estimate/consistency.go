package estimate

// consistency.go is SLAM M5 — the CONSISTENCY / OBSERVABILITY INVARIANT made measurable and failable.
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §4 (P2/P3) + §5 #7 + §5b ("M5 is not
// optional for awake") + §6 (M5); docs/internal/notes/2026-06-20-slam-M1-build-spec.md §7/§9.
//
// THE FAILURE MODE IT GUARDS (Huang, Mourikis & Roumeliotis, IJRR 2010): an EKF that re-linearizes at
// its CURRENT estimate gains rank-1 of SPURIOUS information in the unobservable global-frame direction
// -> systematically OVERCONFIDENT even while perfectly stable. In cognitive terms: a Filter that
// re-judges trust from a belief's latest SELF-REINFORCED restatement fabricates certainty about its own
// global correctness, which is structurally unobservable from self-evidence. Reactive episodes are too
// short to see it; a CONTINUOUSLY-running self-estimator compounds it over a long awake run into
// catastrophic overconfidence -- which is why M5 is a genuine AWAKE-DURABILITY requirement alongside the
// five control-theoretic conditions, not a side feature.
//
// THE INVARIANT (the M1 §0 rule, restated as a conserved quantity): belief variance P (= 1/information)
// may DECREASE only through a GROUNDED Observe() — an OBSERVABLE measurement of reality. A self-
// restatement (Note()) carries NO new information about the world, so it must NEVER lower P. Therefore
// EVERY unit of information the estimator gains MUST be attributable to a grounded, ASSOCIATED
// observation; any information gained WITHOUT one is gained in an unobservable direction -- spurious --
// and is exactly the Huang-2010 inconsistency. M1 enforces this STRUCTURALLY (only an associated
// Observe() writes a smaller variance); M5 is the WITNESS that proves it held over the run and the
// failable check that catches a regression (e.g. a future edit that let Note() shrink P, or a GATED/
// unassociated observation that still shrank P) the instant it gains spurious information.
//
// This is the NEES (Normalized Estimation Error Squared) consistency test of estimation theory, recast
// for the heterogeneous belief map: an estimator is CONSISTENT iff its claimed certainty is justified by
// its observations. Pure CONTROL (closed-form information accounting, NO model call ever).

// Consistency is the M5 information-accounting witness over the run so far. Information for a scalar
// belief is 1/variance; a variance reduction from P_before to P_after is an information GAIN of
// (1/P_after - 1/P_before). The invariant: every gain is attributable to a grounded, associated
// Observe().
type Consistency struct {
	// GroundedGain is the cumulative information gained through GROUNDED, ASSOCIATED observations — the
	// only LEGITIMATE (observable) information. Accrued in Observe() when the Kalman update is applied.
	GroundedGain float64
	// SpuriousGain is the cumulative information gained in an UNOBSERVABLE direction — variance that
	// shrank WITHOUT a grounded, associated observation justifying it (a self-restatement that lowered P,
	// or a gated/unassociated observation that still shrank P). In a CONSISTENT estimator this is exactly
	// 0; any positive value is the Huang-2010 inconsistency and FAILS the invariant.
	SpuriousGain float64
	// Notes / Observations count the two write paths, for legibility (how much self-restatement vs how
	// much grounded measurement the run saw — the awake "rumination vs grounding" ratio).
	Notes        int
	Observations int
	// Gated counts data-association rejects (a refuting obs too far from the prior to be folded in). A
	// gated obs must NOT shrink P (it was not associated) — it is one of the spurious-gain tripwires.
	Gated int
	// Violations counts the number of distinct events that contributed spurious information (a refused
	// Note() reduction, a gated-obs reduction). Zero in a structurally-sound estimator.
	Violations int
}

// Consistent reports whether the invariant held: zero information gained in unobservable directions.
// A tiny epsilon absorbs float round-off so an exactly-conserving run is not flagged by 1e-16 noise.
func (c Consistency) Consistent() bool { return c.SpuriousGain <= consistencyEpsilon }

// GroundedFraction is the share of the estimator's total information gain that came from grounded
// observations: 1.0 for a perfectly consistent estimator, < 1.0 the moment any spurious gain leaks in.
// Returns 1.0 for a run that gained no information at all (vacuously consistent).
func (c Consistency) GroundedFraction() float64 {
	total := c.GroundedGain + c.SpuriousGain
	if total <= consistencyEpsilon {
		return 1.0
	}
	return c.GroundedGain / total
}

// consistencyEpsilon is the float round-off floor below which a "spurious gain" is treated as numerical
// noise, not a real inconsistency. Scalar Kalman arithmetic on values near 1.0 stays well above this.
const consistencyEpsilon = 1e-9

// infoGain returns the information gained by a variance reduction from before to after: 1/after - 1/before
// when after < before (a real reduction), else 0 (variance grew or held — no information gained). Guards
// the degenerate near-zero variance (already maximally certain) so 1/P stays finite.
func infoGain(before, after float64) float64 {
	if before <= consistencyEpsilon || after <= consistencyEpsilon || after >= before {
		return 0
	}
	return (1.0 / after) - (1.0 / before)
}
