package tiera

import (
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/bench/judge"
	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// SelfImprovementExposures is how many times a self-improvement Tier-A item's goal is driven
// through ONE harness engine (the convertibility couplet of measuring-stick-spec §3.3 A1, in
// one session). The mint gate is MintAfter=3 effortful repeats (internal/convert), and the
// mint fires at the IDLE consolidate AFTER an episode, so the FIRST reuse of the minted
// artifact can only happen on a LATER exposure: 5 exposures give the early ones room to mint
// (≥3 effortful repeats accrue) and leave ≥2 later exposures that resolve via the mint — the
// cost drop + MintThenReused witness the isolation predicate reads. The bare arm ignores this
// (single Generate call).
const SelfImprovementExposures = 5

// RunItem runs ONE Tier-A item under ONE arm at a paired seed and returns a
// fully-formed types.ItemResult (spec §5.2, §5.7): it materializes the item's
// artifact into a per-item sandbox, drives the arm through internal/bench/runner
// against the backend the factory builds, scores the answer with the
// deterministic oracle, AND's the isolation guard where the item carries a
// trace_oracle, and projects the per-arm Cost + an events pointer onto the row.
// The sandbox is created and cleaned up within the call — the result is
// self-contained.
//
// The pass rule (spec §1.4): Pass = oracle(answer, artifact) AND (if the item
// has a TraceOracle) the runner's isolation predicate holds. The answer-oracle
// verdict (OracleVerdict) and the isolation verdict (IsolationResult) are
// recorded separately so the stats layer can read each — a pass that is right
// WITHOUT genuine mechanism use is a mechanism-bypass and is excluded from the
// lift numerator by the conjunction.
//
// The isolation guard is applied to the harness/gate arms only; the bare arm has
// no event trace (spec §3.2: "bare has no trace, so only the answer check
// applies"), so its IsolationResult is left true and Pass is the oracle verdict
// alone. An UNSUPPORTED gate-off arm (no ablation toggle yet) yields a result
// with Pass=false and the runner's TODO note in the events pointer.
func RunItem(item benchtypes.TierAItem, arm benchtypes.Arm, seed int64, factory runner.BackendFactory) benchtypes.ItemResult {
	// Regulator-response probe (stability Tier-A, spec §3.5): NO model in the loop.
	// The mechanism under test is the regulator, not the model's reading of a
	// telemetry table. Drive the REAL regulator deterministically (closed-loop for
	// the harness arm, open-loop for the bare/gate-off reference) and score the
	// emitted telemetry. Branch BEFORE touching the model engine/sandbox.
	if IsRegulatorProbe(item) {
		return RunRegulatorProbe(item, arm, seed)
	}
	// Materialize the artifact into a per-item sandbox so the grounding tools read
	// real bytes. A zero-valued artifact yields an empty sandbox (no error).
	sb, cleanup, err := Materialize(item)
	defer cleanup()
	if err != nil {
		return benchtypes.ItemResult{
			ID: item.ID, Seed: seed, Arm: arm,
			Pass: false, OracleVerdict: false, IsolationResult: false,
			EventsPointer: "materialize-failed: " + err.Error(),
		}
	}

	// Hand the runner the sandbox root ONLY when the item actually materialized a file
	// artifact: a non-empty Workspace flips the engine onto the REAL-action path (sandboxed
	// tool dispatch), which a model-only / reasoning probe (artifact kind "none") neither needs
	// nor wants — for those the engine should take the heuristic-act path so the effortful
	// reasoning the item probes is what runs (the empty temp dir is no artifact at all). Items
	// WITH a file artifact still get the sandbox so the grounding tools read real bytes.
	workspace := sb.Root
	if sb.ArtifactPath == "" {
		workspace = ""
	}
	r := runner.New(factory, workspace)
	spec := runner.Spec{
		Prompt:    item.Prompt,
		Arm:       arm,
		Mechanism: item.Mechanism,
		Seed:      seed,
	}
	// continuous-autonomy Tier-A is a FROZEN-SNAPSHOT single-decision probe (measuring-stick-spec
	// §3.4), NOT a full awake episode: hand the harness/gate arms the serialized state so they run
	// ONE awake-regime decision over it. The bare arm ignores FrozenSnapshot and answers the prompt
	// with a single Generate call (the snapshot is inlined in the prompt for it to read). Only the
	// frozen-snapshot artifact kind triggers this — a different continuous-autonomy artifact would
	// fall through to the normal episode path.
	if item.Mechanism == benchtypes.MechContinuousAutonomy && item.Artifact.Kind == "frozen-snapshot" {
		spec.FrozenSnapshot = item.Artifact.Materialization
	}
	// self-improvement Tier-A is a CONVERTIBILITY COUPLET (measuring-stick-spec §3.3 A1: the same
	// effortful sub-task X presented X then X' … in ONE session), NOT a one-shot. Convertibility is a
	// SECOND-occurrence property: a single episode mints the effortful path at the trailing IDLE
	// consolidate but can never REUSE that mint in its own trace, so a one-shot Tier-A item can never
	// witness mint→reuse (the §1.4 isolation gate) — every harness pass was being discarded as a
	// mechanism-bypass and the cell read NO-SIGNAL. Recurring the SAME goal through ONE engine lets
	// the early exposures mint and the later ones resolve via the minted artifact (cheaper), which is
	// the learning curve MintThenReused reads. SelfImprovementExposures (>1) only fires for this
	// mechanism on a harness/gate arm; the bare arm answers once (no cross-episode state — the honest
	// baseline) and every other mechanism is untouched.
	if item.Mechanism == benchtypes.MechSelfImprovement && isHarnessArm(arm) {
		spec.Exposures = SelfImprovementExposures
	}
	run := r.Run(spec)

	if run.Unsupported {
		return benchtypes.ItemResult{
			ID: item.ID, Seed: seed, Arm: arm,
			Pass: false, OracleVerdict: false, IsolationResult: false,
			RawOutput:     run.Text,
			Cost:          run.Cost,
			EventsPointer: "UNSUPPORTED: " + run.Note,
		}
	}

	// Score the answer with the deterministic oracle (reading the trace for the
	// event-presence / ledger-status / trace-requirement cases). For numeric-tolerance this
	// now includes the presence fallback (the grounded value present as any token).
	oracle := Evaluate(item.Oracle, run.Text, run.Events)
	oracleVerdict := oracle.OK && !oracle.Unsupported

	// TRACE WITNESS (the isolation guard): did the harness genuinely USE the mechanism — a real
	// grounding.ground / action.observation for grounding — vs answer from a prior? For the
	// GROUNDING mechanism this runs UNCONDITIONALLY on harness arms: grounding always requires a
	// read, so the witness is MANDATORY, not opt-in per item.TraceOracle (which the bank generator
	// forgets to set, silently degrading the verdict to prose-only — the 2026-06-13 root cause).
	// Other mechanisms keep the legacy trace_oracle gate until each is validated the same way. The
	// bare arm answers without a trace (vacuously true).
	witnessOK := true
	witnessReason := "no trace witness (bare arm / ungated mechanism)"
	groundingComplete := item.Mechanism == benchtypes.MechGrounding && isHarnessArm(arm)
	runWitness := groundingComplete || (item.TraceOracle != nil && isHarnessArm(arm))
	if runWitness {
		ir := runner.CheckIsolation(item.Mechanism, run)
		witnessOK = ir.OK
		witnessReason = ir.Reason
	}

	// COMPLETE ASSESSMENT (grounding): the deterministic oracle and the trace witness are two
	// independent signals run TOGETHER. On AGREEMENT the verdict is confident (both pass → pass;
	// both fail → fail). On DISAGREEMENT — a correct answer with no witnessed read (a possible
	// lucky-prior, OR a real grounding the compute/observation witness missed, e.g. a derived
	// value), or a witnessed read whose stated answer the extractor still misread — the LLM-JUDGE
	// RULER reconciles by reading the answer against the expected value + source (the cc-lane
	// MEASURED method, now landed in code). Bounded: the judge runs ONLY on a grounding harness-arm
	// disagreement — at most one extra model call per such cell.
	pass := oracleVerdict
	judgeReason := ""
	judgeCalls := 0
	if groundingComplete {
		if oracleVerdict == witnessOK {
			pass = oracleVerdict && witnessOK
		} else {
			jv := judgeGrounding(item, run.Text, factory, seed)
			judgeCalls = 1
			if jv.Backed {
				pass = jv.Pass
				judgeReason = " | judge: " + judgeWord(jv.Pass) + " (" + truncRune(jv.Rationale, 70) + ")"
			} else {
				pass = oracleVerdict && witnessOK // judge declined → conservative floor (false on a disagreement)
				judgeReason = " | judge: declined -> floor"
			}
		}
	} else if runWitness {
		pass = oracleVerdict && witnessOK
	}

	// The judge is a separate backend, so its call is not in run.Events; add it to ModelCalls so the
	// cost guard sees it (its tokens are not captured — bounded to disagreement cells, so minor).
	cost := run.Cost
	cost.ModelCalls += judgeCalls

	return benchtypes.ItemResult{
		ID:              item.ID,
		Seed:            seed,
		Arm:             arm,
		Pass:            pass,
		RawOutput:       run.Text,
		OracleVerdict:   oracleVerdict,
		IsolationResult: witnessOK,
		Cost:            cost,
		// Carry the per-call token usage (one per llm.call) so the bench cost report can
		// aggregate per-ROLE / per-MODEL. In-process only; nil on the offline double.
		Calls:         runner.LLMCallsFromEvents(run.Events),
		EventsPointer: "oracle: " + oracle.Reason + " | witness: " + witnessReason + judgeReason,
	}
}

// judgeGrounding runs the LLM-judge ruler to reconcile a grounding det-oracle vs trace-witness
// DISAGREEMENT (measuring-stick MEASURED method): it builds a fresh judge backend from the same
// factory and asks whether the answer correctly reports the expected value GROUNDED IN THE SOURCE
// (not a documented default / prior). Backed=false when the backend declines — the caller falls
// back to the deterministic floor, never a silent pass.
func judgeGrounding(item benchtypes.TierAItem, answer string, factory runner.BackendFactory, seed int64) judge.Verdict {
	if factory == nil {
		return judge.Verdict{Backed: false, Rationale: "no judge factory"}
	}
	return judge.Run(answer, groundingRubric(item), factory(seed, judgeTemperature))
}

// judgeTemperature pins the judge low (deterministic-leaning) across arms; ignored by substrates
// with no temperature knob (claude/session) and by the offline double.
const judgeTemperature = 0.0

// groundingRubric builds the judge criterion from the item: the expected value + the QUESTION +
// the SOURCE excerpt (the materialized fixture + any chained files), with the must-be-grounded
// instruction so the judge scores "reflects the source", not "matches a prior".
func groundingRubric(item benchtypes.TierAItem) string {
	extra := ""
	for p, c := range item.Artifact.Files {
		extra += "\n--- " + p + " ---\n" + c
	}
	return "The OUTPUT must answer the QUESTION with the value GROUNDED IN THE SOURCE below (the " +
		"actual file contents), NOT a documented default or prior knowledge. Expected canonical " +
		"value: " + strconv.Quote(item.Oracle.Expected) + ". QUESTION: " + item.Prompt +
		"\nSOURCE:\n" + truncRune(string(item.Artifact.Materialization), 1200) + truncRune(extra, 800)
}

// judgeWord renders the judge's binary verdict for the ledger pointer.
func judgeWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

// truncRune truncates s to at most n runes (a trailing marker when cut), for the ledger pointer
// and the bounded judge prompt.
func truncRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// isHarnessArm reports whether the arm runs the full engine (and thus emits a
// trace the isolation predicate can read). The bare arms answer with a single
// Generate call and have no trace. The single-strong arm (the sub-agent guard
// reference) IS a full engine — it keeps the graph/Controller/seams/gate and only
// collapses the sub-agent fan-out — so it emits a trace and counts as a harness arm.
func isHarnessArm(arm benchtypes.Arm) bool {
	switch arm {
	case benchtypes.ArmHarness, benchtypes.ArmGateOn, benchtypes.ArmGateOff, benchtypes.ArmSingleStrong:
		return true
	default:
		return false
	}
}
