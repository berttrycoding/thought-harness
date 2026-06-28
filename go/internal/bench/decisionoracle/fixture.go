package decisionoracle

import (
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// Worker is the A2 sub-agent a fixture probes. The scorer switches on it: a
// Deliberator fixture is scored by the decision oracle (right pick), a Verifier
// fixture by the ship oracle (right accept/refuse).
type Worker string

const (
	// WorkerDeliberator — the trade-off worker (target-spec §1): given options with
	// per-criterion ground-truth scores, pick the genuinely-better one.
	WorkerDeliberator Worker = "deliberator"
	// WorkerVerifier — the ship-safety worker (target-spec §1): accept a true claim,
	// refuse a false one, and refuse honestly when the evidence is unavailable.
	WorkerVerifier Worker = "verifier"
)

// Option is one alternative in a Deliberator trade-off. Its quality is NOT asserted
// as a single number — it is per-criterion Scores, each in [0,1] (higher = better on
// that criterion), so the better option is COMPUTED from the fixture's CriteriaWeights
// (a pure function the oracle re-derives). DiscriminatingCriteria names the criteria a
// SOUND deliberation must reason about to separate the options (used to score whether
// a right pick was made for the right reason, not by coin-flip).
type Option struct {
	// ID is the option's stable label, e.g. "A" / "mutex" / "rewrite". The worker's
	// pick is matched against these IDs (and the Names below) when extracting a verdict.
	ID string `json:"id"`
	// Name is a human-readable alias the worker may use instead of the ID in prose
	// (e.g. "incremental refactor") — both ID and Name are accepted at extraction.
	Name string `json:"name,omitempty"`
	// Scores is the per-criterion ground-truth quality of this option, each in [0,1].
	// Keyed by the same criterion names CriteriaWeights uses.
	Scores map[string]float64 `json:"scores"`
}

// VerifierTruth is the ground-truth status of a Verifier claim.
type VerifierTruth string

const (
	// ClaimTrue — the claim is correct; the SOUND verdict is ACCEPT (ship it).
	ClaimTrue VerifierTruth = "true"
	// ClaimFalse — the claim is wrong; the SOUND verdict is REFUSE (block the ship).
	// A false-ACCEPT here is the dangerous failure (ship a bug), weighted accordingly.
	ClaimFalse VerifierTruth = "false"
	// ClaimUnknowable — the evidence needed to settle the claim is NOT available; the
	// SOUND verdict is an HONEST refusal ("cannot verify"), NOT a confident accept and
	// NOT a confabulated answer. This is the never-fabricate discipline as a verdict.
	ClaimUnknowable VerifierTruth = "unknowable"
)

// Fixture is one A2 decision/ship case with its ground truth and the GOOD/BAD verdict
// control. Deliberator fields (Options / CriteriaWeights / DiscriminatingCriteria) are
// set for a deliberator fixture; Verifier fields (Truth / Evidence) for a verifier
// fixture. The Good/Bad verdicts are the fail-discriminating control: GoodVerdict must
// score >= PassThreshold, BadVerdict must score below it.
type Fixture struct {
	// ID is the stable fixture identifier (e.g. "do-deliberator-0001").
	ID string `json:"id"`
	// Worker selects the oracle (deliberator|verifier).
	Worker Worker `json:"worker"`
	// Goal is the request that would be handed to the worker (carried for the report +
	// the live-drive remainder slice; the offline scorer does not parse it).
	Goal string `json:"goal"`
	// PassThreshold is the score >= which a verdict is judged CORRECT (default 1.0 for
	// a deliberator — the pick is right or it is not; default per-fixture for a verifier
	// where partial credit on the honesty axis applies). 0 => the per-worker default.
	PassThreshold float64 `json:"pass_threshold,omitempty"`

	// --- Deliberator ground truth ------------------------------------------------
	// Options are the alternatives; the better one is COMPUTED from CriteriaWeights.
	Options []Option `json:"options,omitempty"`
	// CriteriaWeights weights each criterion in the weighted-sum that ranks the options
	// (so the ground-truth winner is a pure function, re-derived by the oracle — never
	// just an authored label). Empty for a verifier fixture.
	CriteriaWeights map[string]float64 `json:"criteria_weights,omitempty"`
	// DiscriminatingCriteria names the criteria whose difference actually separates the
	// top two options (the reasoning a SOUND deliberation must surface). Used to score
	// the soundness axis: a pick that names a discriminating criterion is reasoned, a
	// bare pick is a coin-flip. Empty => soundness axis not scored for this fixture.
	DiscriminatingCriteria []string `json:"discriminating_criteria,omitempty"`

	// CorrectVerdict, when set to "undecided", marks a deliberator fixture as GENUINELY TIED:
	// the options' weighted scores are within epsilon, so there is no determinable winner and the
	// CORRECT verdict is an honest "undecided" abstain (Undecided=true). A confident pick of
	// EITHER option on such a fixture is the failure (a fabricated decision). Empty => the fixture
	// has a determinable winner (the default; a tie is then a bank error). This is the deliberator
	// analogue of the verifier's ClaimUnknowable -> cannot-verify honest-abstain path.
	CorrectVerdict string `json:"correct_verdict,omitempty"`

	// --- Verifier ground truth ---------------------------------------------------
	// Truth is the claim's ground-truth status (true|false|unknowable). The SOUND
	// verdict is accept / refuse / honest-refuse respectively.
	Truth VerifierTruth `json:"truth,omitempty"`
	// Evidence is the cited fact that settles the claim (e.g. "the test suite fails on
	// the new sort"); a SOUND verifier verdict references it. Used for the soundness
	// axis the same way DiscriminatingCriteria is for the deliberator. Empty =>
	// soundness axis not scored.
	Evidence []string `json:"evidence,omitempty"`

	// --- the fail-discriminating control -----------------------------------------
	// GoodVerdict is a CORRECT worker verdict (the right pick / the right accept-refuse,
	// citing the discriminating evidence) — MUST score >= PassThreshold.
	GoodVerdict Verdict `json:"good_verdict"`
	// BadVerdict is a PLAUSIBLE-BUT-WRONG verdict (the wrong pick / the false-accept of
	// a bad claim / the false-refuse of a true one) — MUST score below PassThreshold.
	BadVerdict Verdict `json:"bad_verdict"`

	// Note is a one-liner on what the fixture probes / why the bad verdict is a
	// plausible miss.
	Note string `json:"note,omitempty"`
}

// Mechanism is the benchtypes tag every decision-oracle fixture/result carries.
func (Fixture) Mechanism() benchtypes.Mechanism { return benchtypes.MechDecisionQuality }

// Verdict is a worker's extracted OUTPUT — what the scorer compares to ground truth.
// It is the machine-readable distillate of the worker's response (the live-drive
// remainder slice fills it from the real worker's output; a fixture's Good/Bad
// verdicts fill it by hand for the discrimination control). Fields not relevant to a
// worker are left zero.
type Verdict struct {
	// PickedOption is the Deliberator's chosen option ID or Name (matched case- and
	// space-insensitively against the fixture's Options). Empty => no pick extracted
	// (a hard fail for a deliberator fixture — the worker declined to decide).
	PickedOption string `json:"picked_option,omitempty"`

	// Undecided, for a Deliberator, is true when the worker HONESTLY stated the options are
	// genuinely tied (`VERDICT: undecided`) — a deliberate abstain, distinct from a missing
	// pick (declined to decide). On a TIED fixture (CorrectVerdict == "undecided") this is the
	// CORRECT answer; on a fixture with a determinable winner a fabricated pick is wrong and an
	// honest "undecided" scores 0 (it failed to make a decidable call) but is NOT a vacuous pass.
	Undecided bool `json:"undecided,omitempty"`

	// Decision is the Verifier's ship verdict: "accept" | "refuse". Empty => no verdict
	// extracted (a hard fail for a verifier fixture).
	Decision ShipDecision `json:"decision,omitempty"`
	// Honest, for a Verifier, is true when the refusal is an HONEST "cannot verify /
	// insufficient evidence" (the right move on an unknowable claim) rather than a
	// confident refusal asserting a counter-claim. Distinguishes honest-refuse from
	// guess-refuse on the ClaimUnknowable case.
	Honest bool `json:"honest,omitempty"`

	// Reasoning is the free-text rationale the worker gave (the oracle scans it for the
	// discriminating criteria / cited evidence — the soundness axis). Empty => the
	// soundness axis scores 0 (an unreasoned verdict).
	Reasoning string `json:"reasoning,omitempty"`
}

// ShipDecision is a Verifier's accept/refuse verdict.
type ShipDecision string

const (
	// DecisionAccept — the Verifier judges the claim correct / safe to ship.
	DecisionAccept ShipDecision = "accept"
	// DecisionRefuse — the Verifier blocks the ship (the claim is wrong, OR the
	// evidence is insufficient — the Honest flag distinguishes which).
	DecisionRefuse ShipDecision = "refuse"
)
