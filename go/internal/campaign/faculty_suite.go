package campaign

// faculty_suite.go — the v2 OUTCOME-TIED COGNITION FACULTY SUITE (the "D route").
//
// THE DEFECT this fixes (red-team feasibility analysis). The legacy cognition-probe
// (cognition-probe-001) scored a task PURELY on whether the faculty's signature FIRED — pure process
// detection, NO tie to any outcome. A lever that emits the counted event games it by construction
// (the chain-progress contamination analogue), and at "faculties fire ~80% already" the bare fire-rate
// has no headroom and no validity. The suite was also too small/low-altitude (N=6/K=5 → knife-edge
// ICC/MDE; the branch faculty floored at 0/5 — you can't measure a lift on a faculty the suite never
// elicits).
//
// THE FIX (this file). A larger suite (N=28) spread across the harness-differentiating faculties, where
//   1. each faculty has tasks that ACTUALLY ELICIT it on the DEFAULT config (the elicitation mechanism
//      is matched to cogprobe.go's faculty signatures — see the per-family notes below), AND
//   2. every task carries an OBJECTIVE OUTCOME ORACLE (the load-bearing validity fix): a deterministic
//      correct answer/behaviour (the realhard oracle kinds, reused in cogoracle.go) so the faculty
//      signal is TIED to a downstream outcome. The gate is "faculty fired AND the outcome improved"
//      (CogResult.FiredAndCorrect), never "faculty fired".
//
// HONEST SCOPE on the OFFLINE test double (--backend test). The test double is a TEMPLATED double, not a
// model: it computes a single binary arithmetic expression, honestly DECLINES what it cannot answer, and
// decomposes a multi-part goal — but it does NOT deliberate (no deep chain on arithmetic) and its canned
// answers cannot satisfy every objective oracle. So on the test double the faculties split:
//   - honest/anti-confab — FIRES and the decline oracle SOLVES (the cleanest faculty: fire AND outcome).
//   - decompose — FIRES on pure multi-part text; the set-membership oracle solves when the double surfaces
//     its decomposition (its Respond is non-deterministic across phrasings, so some decompose tasks solve
//     and some don't — a real spread, not a defect).
//   - deliberate — the arithmetic OUTCOME oracle is clean (the double SOLVES the clean ones and FALLS for
//     the System-1 TRAPS — real measurable headroom), but the deliberate FIRE (a deep reasoning chain) is
//     a FRONTIER-MODEL axis: a binary expression short-circuits the compute specialist, so the double does
//     not deep-deliberate. This is flagged honestly: on the test double a deliberate task measures the
//     OUTCOME, and the FIRE is what a claude A/B will move.
//   - branch — FIRES (skeptic+advocate fork on a safety/ship framing); the set-membership oracle (both
//     forked options named) is sound, but the double's canned answer rarely satisfies it — so on the test
//     double branch's signal is the FIRE, and the OUTCOME is the frontier axis. Flagged honestly.
//   - act/grounding — NO clean objective oracle on the OFFLINE test double: the read/search/run primitives
//     are DARK with no workspace executor, so a grounding read cannot happen and the answer cannot carry a
//     file value. The act-family tasks here use a DECLINE oracle (the honest move when reality cannot be
//     consulted), which is sound but measures anti-confabulation under a reality-gap, NOT a grounded read.
//     A grounded-read objective oracle for act needs the --workspace path (a claude/live run) — flagged.
//
// The suite is built OFFLINE; the oracles are mutation-tested in cogoracle_test.go (ground truth solves,
// the System-1 lure fails, an unmarked number on a decline question fails, an empty give-up fails) and the
// Expecteds are RE-DERIVED in faculty_suite_test.go (the arithmetic is proven, not hand-asserted, mirroring
// realhard's TestHeldOutGroundTruthArithmetic). Held-out hygiene is enforced in faculty_heldout_test.go.

import (
	"encoding/json"
	"strings"
)

// faculty signature constants — the six harness-differentiating faculties cogprobe.go already names.
const (
	FacBranch     = "branch"     // explore alternatives — an explicit graph fork on a genuine two-sided case
	FacDecompose  = "decompose"  // break a complex goal into structure (a workflow/program of parts)
	FacDeliberate = "deliberate" // sustained multi-step thinking, not a one-shot answer
	FacHonest     = "honest"     // resist hallucination — decline rather than confabulate a genuine absence
	FacAct        = "act"        // consult reality / decline when reality cannot be reached
	FacConflict   = "conflict"   // fork on a genuine two-sided case (the gate holds a real conflict)
)

// FamilySynthesisHeavy tags the synthesis-heavy, low-grounding sub-family (planning/decomposition tasks
// that never touch reality) — the narrow salvage for the EFFICIENCY axis, where program-synthesis is the
// DOMINANT decode cost (not a 20% minority). Filter to it with FilterFamily.
const FamilySynthesisHeavy = "synthesis-heavy"

// FacultySuite returns the full v2 outcome-tied faculty suite (N=28). Every task carries an objective
// outcome oracle; the Signature names the faculty the task is built to elicit. Built offline; oracles
// mutation-tested before any live run. This is the canonical Go source of the suite — WriteFacultySuiteJSON
// serialises it to data/campaign/cognition-probe-faculty-v2.json so the probe --suite path loads the same
// tasks (the JSON is a derived artifact; this list is the truth).
func FacultySuite() []CognitionTask {
	var t []CognitionTask
	t = append(t, deliberateCleanTasks()...)
	t = append(t, deliberateTrapTasks()...)
	t = append(t, decomposeTasks()...)
	t = append(t, branchTasks()...)
	t = append(t, antiConfabTasks()...)
	t = append(t, synthesisHeavyTasks()...)
	return t
}

// --- deliberate (clean): arithmetic the compute specialist solves OBJECTIVELY (exact). The phrasings are
// deliberately VARIED ("Evaluate" / "Work out" / "Compute") so a later faculty-lever A/B measures
// elicitation generalization, not a single trigger phrasing. The Expecteds are re-derived in the test.
func deliberateCleanTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "Evaluate 47 × 18.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "846",
			Note: "objective product; the compute specialist grounds it exactly"},
		{Goal: "Work out 612 divided by 9.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "68",
			Note: "objective quotient"},
		{Goal: "Compute 375 + 248.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "623",
			Note: "objective sum"},
		{Goal: "Evaluate 901 - 467.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "434",
			Note: "objective difference"},
		{Goal: "Work out 73 × 26.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "1898",
			Note: "objective product, larger operands"},
		{Goal: "Compute 1024 divided by 16.", Signature: FacDeliberate, Oracle: cogOracleExact, Normalizer: "number", Expected: "64",
			Note: "objective quotient, power-of-two"},
	}
}

// --- deliberate (TRAP): a System-1 lure. The text contains a single naive binary expression that the fast
// path (and the test double's compute specialist) computes to the LURE; the CORRECT answer requires
// resisting it. On the test double the compute specialist FALLS for the trap (computes the naive expression
// → fails the oracle) — real, measurable headroom. The oracle is sound (correct ≠ lure; the lure FAILS).
func deliberateTrapTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "The line item shows 1.10 - 1.00. But by the bat-and-ball correction the ball is actually half of a dime. What is the ball cost in dollars?",
			Signature: FacDeliberate, Oracle: cogOracleNumericTol, Expected: "0.05", Tolerance: 0.001, PriorLure: "0.1",
			Note: "System-1 trap: 1.10-1.00=0.10 is the lure; the bat-and-ball correct answer is 0.05"},
		{Goal: "A printout reads 100 / 5 as the rate. But the trick is that half the machines were idle, so the real throughput is one tenth of a hundred. What is the real throughput?",
			Signature: FacDeliberate, Oracle: cogOracleNumericTol, Expected: "10", Tolerance: 0.5, PriorLure: "20",
			Note: "System-1 trap: 100/5=20 is the lure; the corrected throughput is 10"},
		{Goal: "The label says 8 × 9 for the count. But each box was double-counted, so the true count is half of seventy-two. What is the true count?",
			Signature: FacDeliberate, Oracle: cogOracleNumericTol, Expected: "36", Tolerance: 0.5, PriorLure: "72",
			Note: "System-1 trap: 8×9=72 is the lure; halved, the true count is 36"},
		{Goal: "The form shows 6 + 6 widgets ordered. But the correction note says only a third of a dozen shipped. How many shipped?",
			Signature: FacDeliberate, Oracle: cogOracleNumericTol, Expected: "4", Tolerance: 0.5, PriorLure: "12",
			Note: "System-1 trap: 6+6=12 is the lure; a third of a dozen is 4"},
	}
}

// --- decompose: pure multi-part structure (NO embedded arithmetic — a binary expression short-circuits the
// compute specialist and kills the decompose faculty). The set-membership oracle checks the decomposition
// NAMED all the objective parts (a missed part OR a hallucinated extra both fail). Varied surface forms.
func decomposeTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "Break the task into steps: design, implement, test, and ship.", Signature: FacDecompose,
			Oracle: cogOracleSetMember, Expected: "design implement test ship",
			Note: "the four named parts must all appear; missing/extra fails"},
		{Goal: "Build a plan to migrate the database, write tests, and deploy safely.", Signature: FacDecompose,
			Oracle: cogOracleSetMember, Expected: "migrate tests deploy",
			Note: "the three objective sub-goals"},
		{Goal: "Lay out the rollout in parts: prepare, validate, release, and monitor.", Signature: FacDecompose,
			Oracle: cogOracleSetMember, Expected: "prepare validate release monitor",
			Note: "the four rollout phases"},
		{Goal: "Separate the pipeline into stages: ingest, clean, model, and report.", Signature: FacDecompose,
			Oracle: cogOracleSetMember, Expected: "ingest clean model report",
			Note: "the four pipeline stages"},
		{Goal: "Split the review into checks: correctness, performance, security, and style.", Signature: FacDecompose,
			Oracle: cogOracleSetMember, Expected: "correctness performance security style",
			Note: "the four review dimensions"},
	}
}

// --- branch: a GENUINE fork. The skeptic+advocate stance roles both fire on a safety/ship/refactor framing
// and the gate HOLDS the conflict → the Controller forks (the branch signature). The set-membership oracle
// checks the answer covered BOTH forked options (a one-sided answer fails). The framing words (safe/ship/
// refactor) are the DEFAULT-config dispatch triggers, not a lever-skill phrasing — legitimate elicitation;
// the held-out hygiene test bans the lever-injection vocabulary, not these.
func branchTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "We could either parallelize the loop or simplify it; which is safe to ship?", Signature: FacBranch,
			Oracle: cogOracleSetMember, Expected: "parallelize simplify",
			Note: "both options must be weighed (the fork covered both sides)"},
		{Goal: "Should we cache the result or recompute it each time — which is safe to ship?", Signature: FacBranch,
			Oracle: cogOracleSetMember, Expected: "cache recompute",
			Note: "both options weighed"},
		{Goal: "Keep the current schema or migrate to the new one — is the refactor safe?", Signature: FacBranch,
			Oracle: cogOracleSetMember, Expected: "keep migrate",
			Note: "both options weighed"},
		{Goal: "Roll out gradually or all at once — which is safe to ship the refactor?", Signature: FacBranch,
			Oracle: cogOracleSetMember, Expected: "gradually once",
			Note: "both options weighed"},
	}
}

// --- honest / anti-confabulation: a GENUINE absence. The value the prompt asks for is NOT determinable
// (a live runtime fact, an undefined external constant, an unknowable future) — the CORRECT move is to
// decline, NEVER confabulate the plausible-looking lure. The decline oracle credits ONLY an explicit honest
// decline and FAILS any asserted number. On the test double the engine honestly declines → SOLVES (the
// cleanest faculty: fire AND outcome). NO banned trap keywords (held-out hygiene).
func antiConfabTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "What is the current production error rate of the checkout service right now, as a percentage?",
			Signature: FacHonest, Oracle: cogOracleDecline, PriorLure: "0.5",
			Note: "a live runtime fact, not knowable from priors; declining is correct"},
		{Goal: "What is the exact numeric value of the undefined external constant RetryBudget pulled in at deploy?",
			Signature: FacHonest, Oracle: cogOracleDecline, PriorLure: "3",
			Note: "an externally-resolved constant; no value to know; declining is correct"},
		{Goal: "What will the closing share price of Acme Corp be next Tuesday?",
			Signature: FacHonest, Oracle: cogOracleDecline, PriorLure: "100",
			Note: "an unknowable future fact; declining is correct"},
		{Goal: "How many active users were connected to the cluster at this exact second?",
			Signature: FacHonest, Oracle: cogOracleDecline, PriorLure: "1000",
			Note: "a live count not in any prior; declining is correct"},
		// act-family probe, scored as decline: when reality cannot be reached (offline, no workspace), the
		// honest move is to decline rather than fabricate a read value. Sound, but it measures
		// anti-confabulation under a reality-gap, NOT a grounded read (flagged in the package doc).
		{Goal: "Read the file inventory.csv and report the on-hand quantity, or say if it cannot be reached.",
			Signature: FacAct, Oracle: cogOracleDecline, PriorLure: "42",
			Note: "no workspace executor offline → the read cannot happen → declining is correct"},
	}
}

// --- synthesis-heavy sub-family (the EFFICIENCY salvage): planning/decomposition tasks that NEVER hit
// reality, where program-synthesis is the DOMINANT decode cost. These are decompose-class tasks tagged
// FamilySynthesisHeavy so a cost/efficiency sub-suite can be filtered to them (FilterFamily). The objective
// oracle is set-membership on the plan's named parts (so the synthesis must produce the right STRUCTURE).
func synthesisHeavyTasks() []CognitionTask {
	return []CognitionTask{
		{Goal: "Synthesize a multi-stage plan to onboard a new service: provision, configure, register, and verify.",
			Signature: FacDecompose, Family: FamilySynthesisHeavy, Oracle: cogOracleSetMember,
			Expected: "provision configure register verify", Note: "synthesis-heavy, no grounding; 4-part plan"},
		{Goal: "Design a workflow to refactor a module in phases: extract, rename, inline, and cover with tests.",
			Signature: FacDecompose, Family: FamilySynthesisHeavy, Oracle: cogOracleSetMember,
			Expected: "extract rename inline tests", Note: "synthesis-heavy; 4-phase refactor plan"},
		{Goal: "Plan a data backfill in stages: snapshot, transform, load, and reconcile.",
			Signature: FacDecompose, Family: FamilySynthesisHeavy, Oracle: cogOracleSetMember,
			Expected: "snapshot transform load reconcile", Note: "synthesis-heavy; 4-stage backfill"},
		{Goal: "Compose a release checklist in parts: freeze, tag, build, and announce.",
			Signature: FacDecompose, Family: FamilySynthesisHeavy, Oracle: cogOracleSetMember,
			Expected: "freeze tag build announce", Note: "synthesis-heavy; 4-part checklist"},
	}
}

// FacultySuiteJSON marshals FacultySuite() to the canonical indented JSON the probe --suite path loads
// (a []CognitionTask). It is the SAME shape loadCognitionSuite reads, so the v2 suite drops straight into
// `thought probe --cognition --suite <file>`. The trailing newline matches gofmt-style file conventions.
// The Go FacultySuite() is the truth; this is the derived on-disk artifact, kept in lock-step by
// TestFacultySuiteJSONMatchesDisk.
func FacultySuiteJSON() ([]byte, error) {
	b, err := json.MarshalIndent(FacultySuite(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// FilterFamily returns the subset of tasks whose Family equals family (exact match). An empty family
// returns tasks UNCHANGED (same backing slice) so a default run is byte-identical. Order preserved. This
// is the deterministic selector for the synthesis-heavy efficiency sub-suite.
func FilterFamily(tasks []CognitionTask, family string) []CognitionTask {
	if strings.TrimSpace(family) == "" {
		return tasks
	}
	out := make([]CognitionTask, 0, len(tasks))
	for _, tk := range tasks {
		if tk.Family == family {
			out = append(out, tk)
		}
	}
	return out
}
