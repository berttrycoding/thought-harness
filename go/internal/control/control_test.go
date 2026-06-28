package control

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// The control package is the deterministic CONTROL floor. These tests assert each floor check
// INDEPENDENTLY (not just through the backend), and pin the byte-exact scores so the M1 delegation
// stays behaviour-preserving (golden parity) and the per-op CPython float rounding is reproduced.

// --- conformance: control must be a Tier-1 leaf (no backends import) is enforced by the build +
// the §6.3 `go list -deps` check; here we assert behaviour. ---

func TestScoreAdmitMalformed(t *testing.T) {
	v := ScoreAdmit(types.Candidate{Text: "x"}, nil, 0.5)
	if v.Verdict != types.REJECT || v.Confidence != 0.0 || v.Reason != "empty/malformed" {
		t.Fatalf("malformed verdict=%+v", v)
	}
	if v.Source != Appraiser {
		t.Fatalf("floor Source must be the control Appraiser %q, got %q", Appraiser, v.Source)
	}
}

func TestScoreAdmitObservationTrusted(t *testing.T) {
	// OBSERVATION prior 0.92 -> source_prior = 0.8*0.92; value_prior = 0.2*0.5 = 0.1.
	v := ScoreAdmit(types.Candidate{Text: "reality checked out fine", Source: types.OBSERVATION}, nil, 0.5)
	if v.Verdict != types.ADMIT {
		t.Fatalf("observation should ADMIT, got %v (conf %v)", v.Verdict, v.Confidence)
	}
	if sp, ok := v.Signals["source_prior"].(float64); !ok || sp != 0.736 {
		t.Fatalf("source_prior signal=%v (want 0.736)", v.Signals["source_prior"])
	}
	if vp, ok := v.Signals["value_prior"].(float64); !ok || vp != 0.1 {
		t.Fatalf("value_prior signal=%v (want 0.1)", v.Signals["value_prior"])
	}
}

func TestScoreAdmitHedgedNeumaierExact(t *testing.T) {
	// The 3-signal hedged case the Neumaier comment pins (laundered-hallucination guard):
	// GENERATED prior 0.42 -> source_prior = 0.8*0.42 = 0.336; value_prior = 0.2*0.5 = 0.1;
	// hedged = -0.18. CPython's sum() folds these with Neumaier compensated summation to EXACTLY
	// 0.256; the naive Go left-fold gives 0.25600000000000006 (1 ULP off). The exact value below
	// proves the compensated sum is in force (byte-identical to the Python wire / goldens).
	v := ScoreAdmit(types.Candidate{Text: "maybe it works, i guess", Source: types.GENERATED}, nil, 0.5)
	if _, ok := v.Signals["hedged"]; !ok {
		t.Fatalf("hedged signal missing: %v", v.Signals)
	}
	const want = 0.256                // Neumaier compensated; the naive fold yields 0.25600000000000006
	const naive = 0.25600000000000006 // what a non-compensated left-fold would give
	if want == naive {
		t.Fatalf("test invariant broken: this case must be a Neumaier-divergence case")
	}
	if v.Confidence != want {
		t.Fatalf("hedged confidence=%v want %v (Neumaier compensated sum, not the naive %v)", v.Confidence, want, naive)
	}
	if v.Verdict != types.REJECT { // 0.256 < 0.32
		t.Fatalf("hedged verdict=%v want REJECT", v.Verdict)
	}
}

func TestSourcePriorBands(t *testing.T) {
	cases := []struct {
		src  types.Source
		want float64
	}{
		{types.OBSERVATION, 0.92},
		{types.USER_INPUT, 0.88},
		{types.PERCEPT, 0.82},
		{types.GENERATED, 0.42},
		{types.METACOG, 0.72},
	}
	for _, tc := range cases {
		if got := SourcePrior(types.Candidate{Source: tc.src}); got != tc.want {
			t.Fatalf("SourcePrior(%v)=%v want %v", tc.src, got, tc.want)
		}
	}
	// INJECTED scales with relevance and uses the compiler-opaque round so it matches CPython's
	// per-op double rounding: 0.5 + roundf64(0.45*0.85) = 0.8825000000000001 (NOT 0.8825).
	const wantInj = 0.8825000000000001
	if got := SourcePrior(types.Candidate{Source: types.INJECTED, Relevance: 0.85}); got != wantInj {
		t.Fatalf("INJECTED SourcePrior(rel=0.85)=%v want %v", got, wantInj)
	}
}

func TestHedgedAndOverConfident(t *testing.T) {
	if !Hedged("Maybe this is right") { // case-insensitive
		t.Fatalf("Hedged should detect 'Maybe'")
	}
	if Hedged("this is a grounded fact") {
		t.Fatalf("Hedged false-positive")
	}
	if !OverConfident("this is DEFINITELY the answer") {
		t.Fatalf("OverConfident should detect 'DEFINITELY'")
	}
	if OverConfident("a measured, careful claim") {
		t.Fatalf("OverConfident false-positive")
	}
}

func TestContradicts(t *testing.T) {
	prior := "safe"
	priorCand := &types.Candidate{Stance: &prior}
	hist := []types.Thought{{Confidence: 0.8, RawReturn: priorCand}}
	now := "unsafe"
	c := types.Candidate{Text: "this is clearly unsafe", Source: types.INJECTED, Stance: &now, Relevance: 0.9}
	if !Contradicts(c, hist) {
		t.Fatalf("opposing stance to a confident belief should contradict")
	}
	// no stance on the candidate -> never contradicts
	if Contradicts(types.Candidate{Text: "no stance here"}, hist) {
		t.Fatalf("a stance-less candidate must not contradict")
	}
	// the prior belief is low-confidence (< 0.6) -> not a belief worth defending
	weakHist := []types.Thought{{Confidence: 0.4, RawReturn: priorCand}}
	if Contradicts(c, weakHist) {
		t.Fatalf("a low-confidence prior must not trigger contradiction")
	}
}

func TestRefutedByReality(t *testing.T) {
	failHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: false, Text: "tests failed"},
	}}
	stance := "runs"
	c := types.Candidate{Text: "it runs cleanly now", Source: types.INJECTED, Stance: &stance, Relevance: 0.9}
	if !RefutedByReality(c, failHist) {
		t.Fatalf("re-asserting success after a failed observation must be refuted")
	}
	// no recent failure -> not refuted
	okHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: true, Text: "tests passed"},
	}}
	if RefutedByReality(c, okHist) {
		t.Fatalf("a passing observation must not refute")
	}
	// failure present but the candidate does not re-assert success -> not refuted
	neutral := types.Candidate{Text: "let me investigate the failure", Source: types.INJECTED, Relevance: 0.9}
	if RefutedByReality(neutral, failHist) {
		t.Fatalf("a non-success-claim must not be refuted")
	}
}

func TestScoreAdmitRefutedRejects(t *testing.T) {
	failHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: false, Text: "tests failed"},
	}}
	stance := "runs"
	c := types.Candidate{Text: "it runs cleanly now", Source: types.INJECTED, Stance: &stance, Relevance: 0.9}
	v := ScoreAdmit(c, failHist, 0.5)
	if _, ok := v.Signals["refuted_by_reality"].(float64); !ok {
		t.Fatalf("expected refuted_by_reality signal: %v", v.Signals)
	}
	if v.Signals["refuted_by_reality"].(float64) != -0.45 {
		t.Fatalf("refuted penalty=%v want -0.45", v.Signals["refuted_by_reality"])
	}
}

// TestDeniesAvailableReality is the #43 tool-affordance hallucination guard: a candidate that DENIES it
// can reach reality ("no filesystem access" / "type /mcp"), AFTER the loop has already reached reality (a
// real grounded observation is in history), is a refusal that contradicts a demonstrated capability — a
// STRUCTURAL REJECT. The guard must NOT fire on the offline path (no real observation yet) or on an honest
// "I cannot determine X" (no access/tool/file noun).
func TestDeniesAvailableReality(t *testing.T) {
	// a REAL (non-fabricated) observation in history -> the loop demonstrably reached reality.
	groundedHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: true, Text: "reality: const AlphaHigh = 0.7", Fabricated: false},
	}}
	// a FABRICATED observation (offline stand-in) -> reality was NOT reached.
	fabricatedHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: true, Text: "reality: ran the suite", Fabricated: true},
	}}

	deny := func(text string) types.Candidate {
		return types.Candidate{Text: text, Source: types.INJECTED, Relevance: 0.9}
	}

	// FIRES: a denial that contradicts demonstrated reality.
	for _, txt := range []string{
		"I don't have direct shell or filesystem access in this environment.",
		"I wasn't able to directly read the file contents here.",
		"There's no filesystem access, so I can't confirm the value.",
		"You'll need to type `/mcp` to give me tool access.",
	} {
		if !DeniesAvailableReality(deny(txt), groundedHist) {
			t.Errorf("expected DeniesAvailableReality=true for %q (reality was reached)", txt)
		}
	}

	// DOES NOT FIRE: no real observation in history (offline / first-turn) — the denial may be honest.
	if DeniesAvailableReality(deny("I don't have filesystem access here."), nil) {
		t.Errorf("must NOT fire with no real observation (offline path / honest no-tools)")
	}
	if DeniesAvailableReality(deny("I don't have filesystem access here."), fabricatedHist) {
		t.Errorf("a FABRICATED observation does not count as reality reached")
	}
	// DOES NOT FIRE: an honest unknown that names no access/tool/file noun.
	if DeniesAvailableReality(deny("I cannot determine the exact value of that symbol."), groundedHist) {
		t.Errorf("an honest 'cannot determine' (no access/tool/file noun) must not fire")
	}
	// DOES NOT FIRE: a real OBSERVATION candidate is not policed (it came from a tool).
	obs := types.Candidate{Text: "I don't have filesystem access", Source: types.OBSERVATION, Relevance: 0.9}
	if DeniesAvailableReality(obs, groundedHist) {
		t.Errorf("an OBSERVATION-source candidate is not the membrane's concern")
	}
}

// TestScoreAdmitDeniesAccessRejects proves the guard WIRES into the admission floor: a tool-affordance
// refusal after reality was reached is a STRUCTURAL REJECT (verdict override), carries the
// denies_available_reality signal, and has its escalation fuzziness zeroed (the model may not lift it).
func TestScoreAdmitDeniesAccessRejects(t *testing.T) {
	groundedHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: true, Text: "reality: const AlphaHigh = 0.7", Fabricated: false},
	}}
	c := types.Candidate{Text: "I don't have direct filesystem access, so I can't confirm AlphaHigh.",
		Source: types.INJECTED, Relevance: 0.95}
	v := ScoreAdmit(c, groundedHist, 0.6)
	if _, ok := v.Signals["denies_available_reality"].(float64); !ok {
		t.Fatalf("expected denies_available_reality signal: %v", v.Signals)
	}
	if v.Verdict != types.REJECT {
		t.Fatalf("a tool-affordance refusal (reality reached) must be a STRUCTURAL REJECT, got %v", v.Verdict)
	}
	// the structural fact must zero the escalation fuzziness so the model is never asked to lift it.
	if amb := AdmitAmbiguity(v, c); amb != 0.0 {
		t.Fatalf("a denies_available_reality verdict must zero ambiguity; got %v", amb)
	}
	// the BYTE-IDENTICAL guarantee: an identical refusal with NO real observation (offline) is NOT rejected
	// by this guard — its verdict is decided by the ordinary score, not forced.
	v2 := ScoreAdmit(c, nil, 0.6)
	if _, ok := v2.Signals["denies_available_reality"]; ok {
		t.Fatalf("offline (no real observation) must not stamp the denial signal: %v", v2.Signals)
	}
}

func TestRankScoresAndReasons(t *testing.T) {
	cands := []types.Candidate{
		{Text: "a solid grounded answer with substance", Relevance: 0.8},
		{Text: "maybe it could be this", Relevance: 0.3},
	}
	scores, reasons := Rank(cands, nil)
	if len(scores) != 2 || len(reasons) != 2 {
		t.Fatalf("rank lengths scores=%d reasons=%d", len(scores), len(reasons))
	}
	if scores[0] <= scores[1] {
		t.Fatalf("higher-relevance non-hedged should outrank: %v", scores)
	}
	if reasons[0] != "relevance 0.80" {
		t.Fatalf("non-hedged reason=%q", reasons[0])
	}
	if reasons[1] != "relevance 0.30, hedged" {
		t.Fatalf("hedged reason=%q", reasons[1])
	}
	for _, s := range scores {
		if s < 0 || s > 1 {
			t.Fatalf("score out of [0,1]: %v", s)
		}
	}
}

func TestAdmitAmbiguityStructuralFactZeroes(t *testing.T) {
	// A refuted_by_reality / contradicts_belief floor verdict is a STRUCTURAL FACT — never fuzzy,
	// never escalation-eligible (the model may not override reality/belief). Even at a band-edge
	// confidence, the hard signal must zero the ambiguity.
	refuted := types.FilterVerdict{
		Verdict: types.FLAG, Confidence: 0.6, // sitting ON the ADMIT band edge
		Signals: map[string]any{"refuted_by_reality": -0.45},
	}
	if amb := AdmitAmbiguity(refuted, types.Candidate{Source: types.INJECTED}); amb != 0.0 {
		t.Fatalf("refuted_by_reality must zero ambiguity; got %v", amb)
	}
	contra := types.FilterVerdict{
		Verdict: types.FLAG, Confidence: 0.32, // ON the REJECT band edge
		Signals: map[string]any{"contradicts_belief": -0.30},
	}
	if amb := AdmitAmbiguity(contra, types.Candidate{Source: types.GENERATED}); amb != 0.0 {
		t.Fatalf("contradicts_belief must zero ambiguity; got %v", amb)
	}
}

func TestAdmitAmbiguityNearBandEdge(t *testing.T) {
	// Confidence sitting ON a band edge (0.6) → maximal band fuzziness (1.0) regardless of source.
	onEdge := types.FilterVerdict{Verdict: types.ADMIT, Confidence: 0.6, Signals: map[string]any{}}
	if amb := AdmitAmbiguity(onEdge, types.Candidate{Source: types.OBSERVATION}); amb != 1.0 {
		t.Fatalf("on a band edge ambiguity must be 1.0; got %v", amb)
	}
	// Far from both edges + a NON-policed source → low, below the escalation threshold.
	clear := types.FilterVerdict{Verdict: types.ADMIT, Confidence: 0.9, Signals: map[string]any{}}
	if amb := AdmitAmbiguity(clear, types.Candidate{Source: types.OBSERVATION}); amb >= AdmitAmbiguityThreshold {
		t.Fatalf("a clearly-trusted, far-from-edge candidate must NOT be flagged-fuzzy; got %v", amb)
	}
}

func TestAdmitAmbiguityPolicedSourceRaises(t *testing.T) {
	// A clear-confidence verdict from a POLICED source (INJECTED/GENERATED — the membrane's targets)
	// is held flagged-fuzzy at >=0.5 even far from a band edge: that is where laundered claims hide.
	clear := types.FilterVerdict{Verdict: types.ADMIT, Confidence: 0.9, Signals: map[string]any{}}
	if amb := AdmitAmbiguity(clear, types.Candidate{Source: types.INJECTED}); amb < AdmitAmbiguityThreshold {
		t.Fatalf("a policed-source candidate must reach the escalation threshold; got %v", amb)
	}
	if amb := AdmitAmbiguity(clear, types.Candidate{Source: types.GENERATED}); amb < AdmitAmbiguityThreshold {
		t.Fatalf("a GENERATED candidate must reach the escalation threshold; got %v", amb)
	}
}

func TestClamp01(t *testing.T) {
	if Clamp01(-0.5) != 0.0 || Clamp01(1.5) != 1.0 || Clamp01(0.3) != 0.3 {
		t.Fatalf("Clamp01 bounds wrong")
	}
}

func TestRealThoughtsDropsMetacog(t *testing.T) {
	ctx := []types.Thought{
		{Text: "real one", Source: types.GENERATED},
		{Text: "[branch] bookkeeping", Source: types.METACOG},
		{Text: "real two", Source: types.OBSERVATION},
	}
	out := RealThoughts(ctx)
	if len(out) != 2 || out[0].Text != "real one" || out[1].Text != "real two" {
		t.Fatalf("RealThoughts should drop METACOG: %+v", out)
	}
}

func TestLastN(t *testing.T) {
	xs := []types.Thought{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	got := LastN(xs, 2)
	if len(got) != 2 || got[0].ID != 3 || got[1].ID != 4 {
		t.Fatalf("LastN(xs,2)=%+v", got)
	}
	// n larger than the slice returns the whole slice
	if g := LastN(xs, 10); len(g) != 4 {
		t.Fatalf("LastN over-length should return all: %+v", g)
	}
}

func TestContainsAny(t *testing.T) {
	if !ContainsAny("a maybe answer", Hedges) {
		t.Fatalf("ContainsAny should find 'maybe'")
	}
	if ContainsAny("a definite grounded fact", Hedges) {
		t.Fatalf("ContainsAny false-positive on Hedges")
	}
}

// ---------------------------------------------------------------------------
// CRAG-style sufficiency floor (A-RAG1) — assert each band INDEPENDENTLY + the
// structural ungrounded cap + the ambiguity gate (mutation-sensitive).
// ---------------------------------------------------------------------------

// TestScoreSufficiencyCovered: high content-overlap GROUNDED fuel reads SUFFICIENT.
func TestScoreSufficiencyCovered(t *testing.T) {
	s := ScoreSufficiency(
		"capital city of france paris location",
		"the capital city of france is paris located on the seine",
		trustHigh, true)
	if s.Verdict != SuffSufficient {
		t.Fatalf("covered grounded fuel verdict=%v reason=%q, want sufficient", s.Verdict, s.Reason)
	}
	if s.Source != Appraiser {
		t.Fatalf("floor appraiser=%q, want %q (Pattern-A, no model)", s.Source, Appraiser)
	}
}

// TestScoreSufficiencyOffTopic: a recall that shares NO content words with the need is INSUFFICIENT
// (the abstain-vs-over-commit case — the harness must drop it rather than voice an off-topic recall).
func TestScoreSufficiencyOffTopic(t *testing.T) {
	s := ScoreSufficiency(
		"capital city of france paris location",
		"the recipe needs flour butter sugar and three large eggs",
		trustHigh, true)
	if s.Verdict != SuffInsufficient {
		t.Fatalf("off-topic recall verdict=%v (coverage=%v), want insufficient -> abstain", s.Verdict, s.Coverage)
	}
}

// TestScoreSufficiencyEmptyFuel: empty fuel text is INSUFFICIENT outright (no fuel = nothing to commit on).
func TestScoreSufficiencyEmptyFuel(t *testing.T) {
	if s := ScoreSufficiency("anything at all here", "   ", trustHigh, true); s.Verdict != SuffInsufficient {
		t.Fatalf("empty fuel verdict=%v, want insufficient", s.Verdict)
	}
}

// TestScoreSufficiencyUngroundedCap is the laundered-hallucination guard applied to retrieval: even a
// perfectly-COVERING fuel cannot read SUFFICIENT when it is UNGROUNDED (the GENERATED rung) — it is
// capped at AMBIGUOUS so an invented "fact" never passes as sufficient grounding. The mutation guard:
// drop the cap and this fuel would read sufficient; flip grounded=true and it must.
func TestScoreSufficiencyUngroundedCap(t *testing.T) {
	q := "capital city of france paris location"
	fuel := "the capital city of france is paris located on the seine"
	ungrounded := ScoreSufficiency(q, fuel, trustHigh, false)
	if ungrounded.Verdict == SuffSufficient {
		t.Fatalf("ungrounded (GENERATED) fuel must NOT read sufficient, got %v", ungrounded.Verdict)
	}
	grounded := ScoreSufficiency(q, fuel, trustHigh, true)
	if grounded.Verdict != SuffSufficient {
		t.Fatalf("the SAME fuel grounded must read sufficient (proves the cap is provenance-driven), got %v", grounded.Verdict)
	}
}

// TestScoreSufficiencyLowTrust: a covering fuel from a LOW-trust rung is pulled down off the sufficient
// band (combined = coverage * trust), proving trust is load-bearing, not cosmetic.
func TestScoreSufficiencyLowTrust(t *testing.T) {
	q := "capital city of france paris location"
	fuel := "the capital city of france is paris located on the seine"
	hi := ScoreSufficiency(q, fuel, trustHigh, true)
	lo := ScoreSufficiency(q, fuel, 0.30, true)
	if hi.Verdict != SuffSufficient {
		t.Fatalf("high-trust covering fuel should be sufficient, got %v", hi.Verdict)
	}
	if lo.Verdict == SuffSufficient {
		t.Fatalf("low-trust covering fuel should NOT be sufficient (trust must pull it down), got %v", lo.Verdict)
	}
}

// TestSufficiencyAmbiguityGate: the floor's AMBIGUOUS verdict is flagged-fuzzy (escalate); a CLEAR
// sufficient/insufficient is NOT (the floor is authoritative, the model is not consulted).
func TestSufficiencyAmbiguityGate(t *testing.T) {
	// an ambiguous floor verdict is maximally fuzzy -> escalation-eligible.
	amb := Sufficiency{Verdict: SuffAmbiguous, Coverage: 0.4, Trust: 0.8}
	if SufficiencyAmbiguity(amb) < SufficiencyAmbiguityThreshold {
		t.Fatalf("ambiguous verdict must be escalation-eligible, ambiguity=%v", SufficiencyAmbiguity(amb))
	}
	// a clear-insufficient (near-zero coverage) is structural -> NOT escalation-eligible.
	clearNo := Sufficiency{Verdict: SuffInsufficient, Coverage: 0.0, Trust: 0.9}
	if SufficiencyAmbiguity(clearNo) >= SufficiencyAmbiguityThreshold {
		t.Fatalf("clear-insufficient must NOT escalate (floor authoritative), ambiguity=%v", SufficiencyAmbiguity(clearNo))
	}
	// a clear-sufficient (high coverage*trust, well past the band) is structural -> NOT escalation-eligible.
	clearYes := Sufficiency{Verdict: SuffSufficient, Coverage: 1.0, Trust: 0.92}
	if SufficiencyAmbiguity(clearYes) >= SufficiencyAmbiguityThreshold {
		t.Fatalf("clear-sufficient must NOT escalate (floor authoritative), ambiguity=%v", SufficiencyAmbiguity(clearYes))
	}
}

// trustHigh is the reality/knowledge-rung trust the sourcing ladder stamps (matches subconscious.trustReality).
const trustHigh = 0.92
