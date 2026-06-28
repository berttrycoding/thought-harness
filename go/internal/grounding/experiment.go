// experiment.go is the unified experiment/validation memory (tracker N.1e): the grounding loop's own
// long-term store. Every grounding attempt — the claim, how it was grounded, the verdict, the trust
// tier, and validity — is persisted, and the loop CHECKS MEMORY FIRST: a claim already validated is
// reused, never re-run. It is the tier-1 source of the SR-4 grounding spine and the substrate for
// epistemic self-awareness (KNOW / BELIEVE / HEARD).
//
// It obeys the two memory-spine invariants (memory-system-audit §2): NEVER-FABRICATE (only a real
// grounding result is stored — a tier-0 fabrication is rejected, ties to P0.6) and it is the structured
// experiment form of M1 (recall) + T1 (validity) + P7.1 (persistence). The trust-tier ORDERING /
// conflict-override is layered on top by N.1d.
package grounding

import "strings"

// TrustTier ranks how much a grounding source can be trusted. Higher = more trustworthy; on a conflict
// the higher tier wins (the ordering/override itself is N.1d). "math doesn't lie", so a deterministic
// computation outranks anything observed once and everything merely heard.
type TrustTier int

const (
	TierTestimony            TrustTier = iota // HEARD — asserted by another person/agent, unverified
	TierWeb                                   // a web / wiki / external reference
	TierAuthoritativeRef                      // an authoritative doc/spec
	TierFirsthandObservation                  // we observed it once (a single real observation)
	TierDeterministic                         // computed / proven (N.1b compute, formal) — not opinion
	TierFirsthandValidated                    // we tested it ourselves and it held (the gold tier)
)

func (t TrustTier) String() string {
	switch t {
	case TierFirsthandValidated:
		return "firsthand-validated"
	case TierDeterministic:
		return "deterministic"
	case TierFirsthandObservation:
		return "firsthand-observation"
	case TierAuthoritativeRef:
		return "authoritative-ref"
	case TierWeb:
		return "web"
	default:
		return "testimony"
	}
}

// Epistemic is the system's self-awareness of HOW it knows a claim.
type Epistemic int

const (
	Unknown Epistemic = iota // never grounded
	Heard                    // only testimony / low-tier
	Believe                  // grounded once / mid-tier, or refuted
	Know                     // deterministically proven or firsthand-validated, and it held
)

func (e Epistemic) String() string {
	switch e {
	case Know:
		return "KNOW"
	case Believe:
		return "BELIEVE"
	case Heard:
		return "HEARD"
	default:
		return "UNKNOWN"
	}
}

// Experiment is one persisted grounding attempt.
type Experiment struct {
	Claim   string
	Method  string  // how it was grounded ("compute", "test", "observation", "web", …)
	Verdict Verdict // grounded / refuted / not-computable
	Tier    TrustTier
	Real    bool // never-fabricate: true only if the result came from a real grounding source (not tier-0)
	Tick    int
}

// normalizeClaim is the claim key: lower-cased with ALL whitespace removed, so "12 * 31 = 372" and
// "12*31=372" resolve to the same experiment (arithmetic is whitespace-insensitive; natural-language
// claims still key consistently).
func normalizeClaim(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "")
}

// ExperimentMemory is the grounding loop's validation store.
type ExperimentMemory struct {
	byClaim map[string]Experiment // newest validated experiment per normalized claim
	all     []Experiment          // every recorded attempt, in order (history)
}

// NewExperimentMemory builds an empty store.
func NewExperimentMemory() *ExperimentMemory {
	return &ExperimentMemory{byClaim: map[string]Experiment{}}
}

// Record stores a grounding attempt. NEVER-FABRICATE: a non-real (tier-0 / could-not-observe) result is
// rejected (returns false) — it can never enter validation memory. The CURRENT verdict for a claim is
// the HIGHEST-TRUST source seen (N.1d conflict resolution): a deterministic computation overrides a
// firsthand observation overrides a web reference overrides hearsay; an equal tier is latest-wins. So
// when sources disagree, the more trustworthy one is what the claim resolves to.
func (m *ExperimentMemory) Record(e Experiment) bool {
	if !e.Real || e.Verdict == NotComputable {
		return false
	}
	m.all = append(m.all, e)
	key := normalizeClaim(e.Claim)
	if cur, ok := m.byClaim[key]; !ok || e.Tier >= cur.Tier {
		m.byClaim[key] = e
	}
	return true
}

// RecordSource is the convenience for grounding a claim from an EXTERNAL source at a given trust tier —
// a web/wiki reference, an authoritative doc, a single firsthand observation, or another agent's
// testimony. It feeds the same conflict resolution as a computed result. Returns whether it was stored
// (never-fabricate still applies — pass real=true only for a genuine source).
func (m *ExperimentMemory) RecordSource(claim string, verdict Verdict, tier TrustTier, method string, tick int) bool {
	return m.Record(Experiment{Claim: claim, Method: method, Verdict: verdict, Tier: tier, Real: true, Tick: tick})
}

// Recall returns the current validated experiment for a claim (exact, normalized match), if any.
func (m *ExperimentMemory) Recall(claim string) (Experiment, bool) {
	e, ok := m.byClaim[normalizeClaim(claim)]
	return e, ok
}

// Ground is the check-first grounding loop for a claim: if it was already grounded, REUSE that result
// (reused=true, never re-run); otherwise ground it deterministically (the N.1b compute evaluator), and
// store a real result so the next ask is reused. A claim with no deterministic handle is returned
// NotComputable and NOT stored (it stays ungrounded — never fabricated). reused reports whether the
// result came from memory.
func (m *ExperimentMemory) Ground(claim string, tick int) (Result, bool) {
	// Reuse only a DETERMINISTIC-or-higher prior — it is authoritative, no need to recompute. A merely
	// heard/web/observed prior must NOT short-circuit a stronger (deterministic) grounding (N.1d): we
	// compute, and highest-tier-wins lets the result override the weaker belief.
	if e, ok := m.Recall(claim); ok && e.Tier >= TierDeterministic {
		return Result{Verdict: e.Verdict, Detail: "reused validated experiment (" + e.Tier.String() + ")"}, true
	}
	res := EvaluateCompute(claim)
	if res.Verdict != NotComputable {
		m.Record(Experiment{Claim: claim, Method: "compute", Verdict: res.Verdict,
			Tier: TierDeterministic, Real: true, Tick: tick})
		return res, false
	}
	// compute can't help — fall back to a prior lower-tier belief if we have one (still grounded once).
	if e, ok := m.Recall(claim); ok {
		return Result{Verdict: e.Verdict, Detail: "no computation; prior " + e.Tier.String() + " stands"}, true
	}
	return res, false
}

// IngestObservation wires a watched-seam observation INTO the grounding loop (N.1a, external-reality
// grounding): a REAL observation grounds the claim it bears on (Grounded if it succeeded, Refuted if it
// failed) at the firsthand-observation tier — so a confident claim is overturned the moment reality
// contradicts it. A FABRICATED observation (P0.6 tier-0) is REJECTED here: fake reality can never
// ground, so the grounding loop can't be turned into a hallucination amplifier. Returns whether it was
// ingested. fabricated comes straight off types.Observation.Fabricated.
func (m *ExperimentMemory) IngestObservation(claim string, ok, fabricated bool, tick int) bool {
	if fabricated || strings.TrimSpace(claim) == "" {
		return false // tier-0 / no claim — never grounds
	}
	v := Grounded
	if !ok {
		v = Refuted
	}
	return m.RecordSource(claim, v, TierFirsthandObservation, "observation", tick)
}

// Status reports the system's epistemic stance on a claim from what it has grounded.
func (m *ExperimentMemory) Status(claim string) Epistemic {
	e, ok := m.Recall(claim)
	if !ok {
		return Unknown
	}
	if e.Verdict == Refuted {
		return Believe // we know something about it (it's false), but not a positive KNOW
	}
	switch e.Tier {
	case TierFirsthandValidated, TierDeterministic:
		return Know
	case TierFirsthandObservation, TierAuthoritativeRef:
		return Believe
	default:
		return Heard
	}
}

// Len reports how many attempts have been recorded (history length).
func (m *ExperimentMemory) Len() int { return len(m.all) }

// Since returns the grounding attempts recorded after history position base (m.all[base:]) — the per-
// episode tail when base = the Len() captured at episode open. It is a read-only view used by the offline-
// RL flywheel to tally this episode's INDEPENDENT grounded/refuted verdicts (never a self-grade — only
// REAL recorded results are in m.all; a tier-0 fabrication was rejected at Record). An out-of-range base
// clamps to a safe empty/whole slice. The caller must not mutate the returned Experiments.
func (m *ExperimentMemory) Since(base int) []Experiment {
	if base < 0 {
		base = 0
	}
	if base >= len(m.all) {
		return nil
	}
	return m.all[base:]
}
