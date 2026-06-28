package estimate

// covariance.go is SLAM M2 — the SPARSE BELIEF COVARIANCE / Information layer, the off-diagonal
// structure that turns the M1 bag of independent scalar variances into a JOINT posterior.
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #2 (Information / correlations) +
// §1 (the non-factorization argument: "all map errors share a common source — the pose error at the
// moment each was observed") + §6 M2. Pure CONTROL (the propagation math is control.CorrelatedInflation /
// control.CorrelationCoefficient; this file holds the sparse correlation GRAPH that is ticked across the
// run and calls them). No model call ever.
//
// THE THING M1 THROWS AWAY (Durrant-Whyte & Bailey, Part I, Thm 2): two beliefs derived from a COMMON
// upstream (a shared grounding / a shared ancestor thought) are wrong in a CORRELATED way when that
// upstream is wrong — and that correlation is exactly the information needed to detect it. M1 keeps each
// belief's variance independently; M2 records WHICH beliefs co-vary (because they share an upstream) and,
// when reality REFUTES one, propagates a correlated loss-of-certainty to its co-varying siblings: their
// variance RISES because the shared grounding that backed them just proved unreliable. This is how the
// estimator catches CORRELATED SELF-DECEPTION (two beliefs confidently wrong because one bad root), which
// no per-belief scalar can see.
//
// SPARSITY IS LOAD-BEARING (Dellaert & Kaess, Square-Root SAM): the structure MUST stay sparse (a
// factor-graph adjacency keyed on shared upstreams), never the dense O(n^2) covariance matrix the filter
// form pays for. We store an edge ONLY between beliefs that share at least one upstream — independent
// beliefs have no edge and cost nothing, so an episode with no shared grounding is byte-identical to M1.
//
// CONSISTENCY (stays inside the M1 §0 / M5 invariant): a correlated propagation may ONLY RAISE a
// sibling's variance (LOSE certainty), never lower it. Becoming less certain can never be spurious
// information, so M2 cannot violate the consistency invariant the M5 witness guards — only a direct
// grounded Observe() may ever shrink a variance.

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// UpstreamID identifies a grounding ancestor a belief was derived from (the shared "pose at first
// sighting" in SLAM terms). Two beliefs that list the same UpstreamID co-vary through it. M2 keys this
// on the conscious thought id of an ancestor in the active line (mirroring how BeliefID keys on the tip).
type UpstreamID string

// FromUpstreamThoughtID builds an UpstreamID from a conscious ancestor-thought id — the ONE place that
// mapping lives, so a later milestone can change the upstream identity without touching the engine.
func FromUpstreamThoughtID(id int) UpstreamID { return UpstreamID("u" + itoa(id)) }

// covGraph is the sparse correlation structure: per-belief upstream sets + the derived per-upstream
// member lists. It is NOT a dense matrix — an edge exists implicitly between two beliefs iff they appear
// together under some upstream. Held inside the Estimator (constructed lazily on the first Link).
type covGraph struct {
	// upstreamsOf maps a belief to the set of upstreams it was derived from (used as a set; the value is
	// unused). Sparse: a belief with no recorded upstream is absent.
	upstreamsOf map[BeliefID]map[UpstreamID]struct{}
	// membersOf maps an upstream to the beliefs derived from it — the adjacency that lets a refutation
	// find every co-varying sibling in O(neighbours), never O(all beliefs).
	membersOf map[UpstreamID]map[BeliefID]struct{}
}

func newCovGraph() *covGraph {
	return &covGraph{
		upstreamsOf: map[BeliefID]map[UpstreamID]struct{}{},
		membersOf:   map[UpstreamID]map[BeliefID]struct{}{},
	}
}

// link records that belief id was derived from upstream up. Idempotent; builds both directions of the
// sparse adjacency. Self-links (a belief listing its own thought as upstream) are dropped — a belief does
// not co-vary with itself.
func (g *covGraph) link(id BeliefID, up UpstreamID) {
	if UpstreamID(id) == up {
		return
	}
	if g.upstreamsOf[id] == nil {
		g.upstreamsOf[id] = map[UpstreamID]struct{}{}
	}
	g.upstreamsOf[id][up] = struct{}{}
	if g.membersOf[up] == nil {
		g.membersOf[up] = map[BeliefID]struct{}{}
	}
	g.membersOf[up][id] = struct{}{}
}

// sharedCount returns how many upstreams beliefs a and b have in common — the input to the correlation
// coefficient. Iterates the SMALLER upstream set so the cost is bounded by a belief's own (small)
// ancestor count, never the whole graph.
func (g *covGraph) sharedCount(a, b BeliefID) int {
	ua, ub := g.upstreamsOf[a], g.upstreamsOf[b]
	if len(ua) == 0 || len(ub) == 0 {
		return 0
	}
	small, large := ua, ub
	if len(ub) < len(ua) {
		small, large = ub, ua
	}
	n := 0
	for up := range small {
		if _, ok := large[up]; ok {
			n++
		}
	}
	return n
}

// siblings returns the distinct beliefs that share at least one upstream with id (excluding id itself),
// in a DETERMINISTIC order (sorted) so the event stream and any golden are reproducible — the engine
// loop is seeded/deterministic and the propagation must be too.
func (g *covGraph) siblings(id BeliefID) []BeliefID {
	ups := g.upstreamsOf[id]
	if len(ups) == 0 {
		return nil
	}
	set := map[BeliefID]struct{}{}
	for up := range ups {
		for m := range g.membersOf[up] {
			if m != id {
				set[m] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]BeliefID, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Link records that a belief was derived from a grounding ancestor (a shared upstream). It is the M2
// wire from the conscious lineage into the Information layer: the engine calls it for each ancestor of
// the active tip when the covariance knob is on. It builds the SPARSE correlation structure but changes
// NO estimate (variance/mean are untouched), so it cannot itself violate the consistency invariant.
//
// No-op when the estimator is disabled OR the covariance layer is off (M2 rides slam.covariance on top
// of slam.innovation), so the OFF path stays byte-identical. Lazily constructs the graph on first use.
func (e *Estimator) Link(id BeliefID, up UpstreamID) {
	if !e.correlating() {
		return
	}
	if e.cov == nil {
		e.cov = newCovGraph()
	}
	e.cov.link(id, up)
}

// PropagateRefutation is the M2 measurement-update side-effect: after a GROUNDED refutation of belief id
// (reality said -1), every belief that CO-VARIES with id (shares an upstream) loses certainty — its
// variance is INFLATED by control.CorrelatedInflation scaled by the correlation strength and the
// refutation's magnitude. This is "detect correlated self-deception": the shared grounding that backed
// the siblings just proved unreliable, so they become less trustworthy without a fresh observation.
//
// innovMag is |nu| of the refutation (how hard reality contradicted id). It emits estimate.correlate per
// inflated sibling and returns the count inflated (for the caller / tests). A correlated inflation ONLY
// RAISES variance, so it adds NO information — the M5 witness is untouched (no grounded/spurious gain).
//
// No-op (returns 0, emits nothing) when the covariance layer is off or there are no siblings — so an
// episode with no shared grounding is byte-identical to M1.
func (e *Estimator) PropagateRefutation(id BeliefID, innovMag float64) int {
	if !e.correlating() || e.cov == nil || innovMag <= 0 {
		return 0
	}
	sibs := e.cov.siblings(id)
	if len(sibs) == 0 {
		return 0
	}
	inflated := 0
	for _, sib := range sibs {
		shared := e.cov.sharedCount(id, sib)
		rho := control.CorrelationCoefficient(shared)
		if rho <= 0 {
			continue
		}
		before := e.varOf(sib)
		after := control.CorrelatedInflation(before, rho, innovMag)
		if after <= before {
			continue // nothing changed (guarded) — don't emit a no-op edge
		}
		// Raise the sibling's variance. This is the ONLY place M2 writes a variance, and it always GROWS
		// it (loses certainty), so it can never gain spurious information (the §0/M5 invariant holds).
		e.varByID[sib] = after
		inflated++
		e.emit(events.EstimateCorrelate, "correlate "+string(sib)+" <- refute "+string(id), events.D{
			"sibling":    string(sib),
			"refuted":    string(id),
			"shared":     shared,
			"rho":        round3(rho),
			"innovMag":   round3(innovMag),
			"priorVar":   round3(before),
			"postVar":    round3(after),
			"varInflate": round3(after - before),
		})
	}
	return inflated
}

// correlating reports whether the M2 sparse-covariance layer should account this write: the estimator is
// active (slam.innovation, so there IS a variance trajectory to correlate) AND the slam.covariance knob
// is on. When false, Link/PropagateRefutation are no-ops, so a covariance-OFF run does exactly the M1
// work and is byte-identical.
func (e *Estimator) correlating() bool { return e.Enabled() && e.cfg.Covariance }

// Correlating is the exported guard the engine checks BEFORE doing the (otherwise-wasted) lineage walk
// that feeds Link — so the OFF path adds zero graph work and is byte-identical. Nil-safe.
func (e *Estimator) Correlating() bool { return e != nil && e.correlating() }

// SetCovariance honours a live flip of the slam.covariance knob (the M2 Information layer). The layer
// only does anything when the estimator is also Enabled (it correlates the M1 variance trajectory);
// flipping it OFF freezes the accumulated correlation graph (a re-flip-ON resumes from it). Nil-safe.
func (e *Estimator) SetCovariance(on bool) {
	if e == nil {
		return
	}
	e.cfg.Covariance = on
}

// CovarianceEdges returns the number of distinct co-varying belief PAIRS the sparse correlation graph
// currently holds (an observability readout for the Ctrl+O monitor / tests — "how much structure has the
// Information layer found"). Counts each unordered pair once. Returns 0 when the layer is off or empty.
func (e *Estimator) CovarianceEdges() int {
	if !e.correlating() || e.cov == nil {
		return 0
	}
	seen := map[[2]BeliefID]struct{}{}
	for id := range e.cov.upstreamsOf {
		for _, sib := range e.cov.siblings(id) {
			pair := [2]BeliefID{id, sib}
			if sib < id {
				pair = [2]BeliefID{sib, id}
			}
			seen[pair] = struct{}{}
		}
	}
	return len(seen)
}

// itoa is a tiny stdlib-free int->string for the UpstreamID key (the estimate package already imports
// strconv in estimate.go via FromThoughtID, but keeping this local avoids a second import site churn).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
