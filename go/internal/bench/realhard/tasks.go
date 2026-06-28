// Package realhard is a REAL-WORLD-HARD cognition eval — a small suite of tasks
// pitched at the L4-L5 difficulty zone where the frontier base model (claude
// sonnet) GENUINELY FAILS answering alone, so the bare-vs-harness contrast can
// MEASURE real headroom rather than re-confirm a saturated toy probe.
//
// The motivating correction: the earlier "frontier saturates capability" claim
// was an artifact of a TOY 10-task faculty probe (cognition-probe-001). On REAL
// material it is wrong — A1's real-codebase grounding baseline was 22% solve,
// not saturated. This suite goes BEYOND the toy probe to real difficulty and
// measures the bare-model failure rate (the headroom) and the harness lift.
//
// Each task targets one of four failure families the harness's value props
// address (see Capability):
//
//   - MultiHopGrounding   — the answer needs 3+ chained reads/inferences where
//     any single read is insufficient; sonnet single-shot GUESSES from priors,
//     the harness grounds step-by-step. Maps to GROUNDING.
//   - AdaptiveBacktracking — the obvious first approach is a DEAD-END; the right
//     answer needs recognizing it and replanning; sonnet commits to the first
//     plausible answer, the harness backtracks. Maps to STRUCTURED-COGNITION.
//   - AntiConfabulation   — under genuine under-specification the right move is
//     to ground-or-decline, NOT confabulate; sonnet emits a confident wrong
//     number, the harness's Filter/grounding declines or grounds. Maps to
//     EPISTEMIC-GROUNDING.
//   - LongHorizonConsistency — many consistent steps where sonnet loses the
//     thread / contradicts itself; the harness's focus/structure holds it. Maps
//     to STRUCTURED-COGNITION (focus).
//
// Every task is self-contained: its workspace files are embedded as Materials so
// a run materializes a fresh sandbox with no external deps. The oracle is
// deterministic where possible (exact / numeric-tolerance / set-membership /
// decline) so it is offline-vettable and mutation-testable — no fuzzy match
// where exact works (oracle-doctor's standing requirement).
package realhard

import "strings"

// Capability is the failure family a task targets — the harness value prop the
// task is designed to exercise. Used for the per-capability lift breakdown.
type Capability string

const (
	// CapMultiHopGrounding — chained multi-read grounding.
	CapMultiHopGrounding Capability = "multi-hop-grounding"
	// CapAdaptiveBacktracking — recognize a dead-end first approach + replan.
	CapAdaptiveBacktracking Capability = "adaptive-backtracking"
	// CapAntiConfabulation — ground-or-decline under under-specification.
	CapAntiConfabulation Capability = "anti-confabulation"
	// CapLongHorizonConsistency — hold a long consistent chain without drift.
	CapLongHorizonConsistency Capability = "long-horizon-consistency"
)

// OracleKind tags how a task's answer is scored. All four are deterministic and
// offline-vettable (no LLM in the scorer).
type OracleKind string

const (
	// OracleExact — exact match after the named Normalizer (e.g. "number",
	// "token", "lower").
	OracleExact OracleKind = "exact"
	// OracleNumericTolerance — numeric equality within Tolerance (absolute).
	OracleNumericTolerance OracleKind = "numeric-tolerance"
	// OracleSetMembership — the answer (set of tokens) must equal the Expected
	// set exactly (order-free); used for "list every X" tasks where a confabulated
	// extra or a missed member both fail.
	OracleSetMembership OracleKind = "set-membership"
	// OracleDecline — the CORRECT answer is to DECLINE / refuse to confabulate
	// (the question is genuinely unanswerable from the material). Scored solved iff
	// the answer signals honest non-confabulation AND does NOT assert the lure.
	OracleDecline OracleKind = "decline"
)

// Task is one real-world-hard eval item.
type Task struct {
	// ID is a stable identifier (realhard-<cap-short>-NNNN).
	ID string
	// Capability is the failure family this task targets.
	Capability Capability
	// Prompt is the exact request fed to both arms (bare + harness). It NAMES the
	// material files where grounding is required, and pins the values so the
	// answer is the file's, not a documented default.
	Prompt string
	// Materials are the workspace files the harness's read tools can ground
	// against (in-sandbox relative path -> contents). Both arms get the SAME
	// prompt; only the harness has tools to actually read these. Empty for a pure
	// reasoning task (long-horizon) where no file is needed.
	Materials map[string]string
	// Oracle is how the answer is scored.
	Oracle OracleKind
	// Expected is the ground-truth answer (a single token/number for exact /
	// numeric, the canonical set joined by " " for set-membership, empty for
	// decline).
	Expected string
	// Normalizer names the canonicalizer applied before an exact compare
	// ("number" | "token" | "lower" | ""). Numeric-tolerance always parses a
	// number; set-membership always lowers+tokenizes.
	Normalizer string
	// Tolerance is the absolute numeric tolerance for OracleNumericTolerance.
	Tolerance float64
	// PriorLure is the confident WRONG answer bare-sonnet is expected to emit from
	// priors / the documented-but-stale value / the obvious-but-dead-end approach.
	// It is the headroom hypothesis: if bare aces the task, the lure was too weak
	// and the task is too easy (recalibrate harder). For OracleDecline a non-empty
	// PriorLure that the answer ASSERTS is an automatic fail (confabulation).
	PriorLure string
	// Why is a one-line note on why this is hard for bare and where the harness
	// should help — documentation for the report, not used in scoring.
	Why string
	// HumanMin is the OPTIONAL estimated skilled-human task length (minutes) for the
	// METR time-horizon x-axis. 0 (the default for every built-in suite task) falls
	// back to humanMinutesFor's per-capability difficulty heuristic, so the built-in
	// suite is byte-identical. A non-zero value (set by a converted EXTERNAL bank's
	// human_min field, or the offline instrument-validation set) places the task on a
	// precise length — the spread the logistic fit needs to identify a slope.
	HumanMin float64
}

// Tasks returns the full hard suite. Built offline; oracles are mutation-tested
// in tasks_test.go before any live run.
func Tasks() []Task {
	var t []Task
	t = append(t, multiHopTasks()...)
	t = append(t, backtrackTasks()...)
	t = append(t, antiConfabTasks()...)
	t = append(t, longHorizonTasks()...)
	t = append(t, heldOutTasks()...)
	t = append(t, hardTasks()...)
	return t
}

// FilterTasks returns the subset of tasks whose ID contains ANY of the
// comma-separated substrings in `only` (case-sensitive, on the stable IDs). An
// empty/whitespace-only `only` returns tasks UNCHANGED (same backing slice) so a
// default run is byte-identical to the unfiltered path. Order is preserved.
//
// This is the deterministic, offline task selector behind cmd/realhard's
// --only-task flag: a cheap way to measure just the held-out set (--only-task
// held) or just the collateral in-suite set (--only-task back,mhop) without
// spending tokens on the whole suite.
func FilterTasks(tasks []Task, only string) []Task {
	subs := splitTaskFilter(only)
	if len(subs) == 0 {
		return tasks
	}
	out := make([]Task, 0, len(tasks))
	for _, tk := range tasks {
		if matchesAnyTask(tk.ID, subs) {
			out = append(out, tk)
		}
	}
	return out
}

// matchesAnyTask reports whether id contains any of the substrings.
func matchesAnyTask(id string, subs []string) bool {
	for _, s := range subs {
		if strings.Contains(id, s) {
			return true
		}
	}
	return false
}

// splitTaskFilter splits a comma-separated filter into trimmed, non-empty
// substrings. "" / "  " / ", ," -> nil (the "all tasks" sentinel).
func splitTaskFilter(only string) []string {
	var out []string
	for _, p := range strings.Split(only, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
