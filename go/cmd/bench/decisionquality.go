package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/bench/decisionoracle"
	"github.com/berttrycoding/thought-harness/internal/bench/ledger"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// decisionQualityReport is the --decision-quality mode (A2, Track A, registry-target-spec
// §1 rows Deliberator/Verifier, §3 "decision quality; correct accept/refuse", §4 build-
// order item 2): it loads the decision/ship fixtures at bankPath, drives the REAL
// Deliberator + Verifier sub-agents on each fixture's goal (the same skill→expand→workflow→
// fire machinery the engine's Subconscious layer runs), scores each worker's actual verdict
// against the VETTED-SOUND slice-1 oracle, writes one substrate-tagged measurement row per
// fixture to a per-substrate ledger, and prints a per-worker pass-rate report (the A2
// baseline number).
//
// Unlike the six task-outcome mechanisms it runs NO arm campaign and no isolation predicate
// — it scores the worker's OUTPUT correctness directly. On --backend test the workers run
// deterministically (a smoke that proves the apparatus); the REAL baseline is --backend
// claude, where each sub-agent fire is a frontier model call.
func decisionQualityReport(cfg config, bankPath string) error {
	fixtures, err := decisionoracle.LoadFixtures(bankPath)
	if err != nil {
		return err
	}
	if len(fixtures) == 0 {
		return fmt.Errorf("decision-quality bank %q is empty", bankPath)
	}

	// ONE backend for the whole run (a model bridge is reused across fixtures; the test
	// double is stateless). The factory is the consolidated per-backend builder shared with
	// the campaign path, so the substrate (test|llm|session|claude) + cost guards resolve
	// identically. Seed/temp are not meaningful to these pure-reasoning workers on the test
	// double, and are uncontrolled on the claude/session bridge; the per-fixture cpyrand
	// (DriveAll's seedBase) is what makes the test-double run reproducible.
	be := buildFactory(cfg)(cfg.seedBase, cfg.temp)
	substrate := substrateLabel(cfg)

	results := decisionoracle.DriveAll(fixtures, be, uint64(cfg.seedBase))

	// Persist one substrate-tagged measurement row per fixture to a PER-SUBSTRATE ledger,
	// so a claude baseline never mixes into a test-double or local-llm dataset (CLAUDE.md
	// substrate hygiene). The default --out lands the rows under runs/; the per-substrate
	// basename keeps the datasets separate on disk too.
	out := decisionQualityLedgerPath(cfg, substrate)
	store, finalize, err := openLedger(out)
	if err != nil {
		return err
	}
	for _, r := range results {
		appendRow(store, ledger.Record{
			Kind:           ledger.KindMeasurement,
			Tick:           int(cfg.seedBase),
			BatchID:        string(benchtypes.MechDecisionQuality) + "-baseline",
			Mechanism:      benchtypes.MechDecisionQuality,
			Tier:           benchtypes.TierAtomic,
			Arm:            benchtypes.ArmHarness, // the worker runs under full harness discipline (no ablation here)
			ItemID:         r.ID,
			Seed:           cfg.seedBase,
			Substrate:      substrate,
			RawOutput:      r.Output,
			OracleVerdict:  r.Score.Pass,
			CheckerVersion: decisionQualityCheckerVersion,
			EventsPointer:  fmt.Sprintf("worker=%s skill=%s shape=%s | %s", r.Worker, r.Skill, r.Shape, r.Score.Reason),
		})
	}
	ledgerPath, err := finalize()
	if err != nil {
		return err
	}

	report := renderDecisionQuality(bankPath, substrate, results)
	if err := writeReport(cfg.report, report); err != nil {
		return err
	}
	progressf("bench: decision-quality ledger -> %s (substrate=%s)\n", ledgerPath, substrate)
	progressf("bench: decision-quality report -> %s\n", cfg.report)
	fmt.Print(report)

	// FAIL LOUD on contract disobedience (A2 fix #6). When too many workers ignore the verdict
	// contract and the run silently leans on the prose fallback, the new instrument is not being
	// exercised and the fragile parser is back in the common path. Mirror the gpu.lock CONTAMINATED
	// pattern: a boxed banner to stderr + a non-zero exit, so a low present-rate is impossible to
	// miss rather than a quiet report column. The test double always obeys (#5), so this only fires
	// on a real substrate whose worker disobeyed the format.
	if banner, bad := verdictContractGuard(results); bad {
		fmt.Fprint(os.Stderr, banner)
		return fmt.Errorf("decision-quality: verdict-contract present-rate below %.2f — run leaned on the prose fallback (see banner)", verdictLinePresentFloor)
	}
	return nil
}

// verdictLinePresentFloor is the minimum per-worker verdict-line present-rate (A2 fix #6). Below
// it the worker is disobeying the contract often enough that the run is silently on the prose
// fallback — a LOUD failure, not a quiet column.
const verdictLinePresentFloor = 0.8

// verdictContractGuard rolls up the per-worker verdict-line present-rate and, when any worker is
// below verdictLinePresentFloor, returns a boxed CONTAMINATED-style banner and bad=true (the
// caller exits non-zero). The test double obeys the contract deterministically (#5), so an offline
// `--backend test` run never trips this; it guards the real-substrate baseline.
func verdictContractGuard(results []decisionoracle.Result) (banner string, bad bool) {
	stats := decisionoracle.Rollup(results)
	var below []decisionoracle.WorkerStat
	for _, s := range stats {
		if s.Total > 0 && s.VerdictLineRate() < verdictLinePresentFloor {
			below = append(below, s)
		}
	}
	if len(below) == 0 {
		return "", false
	}
	var b strings.Builder
	bar := strings.Repeat("#", 78)
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprintf(&b, "# VERDICT-CONTRACT DISOBEYED — present-rate below %.2f (A2 fix #6)\n", verdictLinePresentFloor)
	fmt.Fprintf(&b, "# The worker(s) below did NOT state a `VERDICT:` line often enough; the run is\n")
	fmt.Fprintf(&b, "# silently leaning on the FRAGILE prose fallback — the instrument is not honest.\n")
	for _, s := range below {
		fmt.Fprintf(&b, "#   %-12s verdict-line present-rate=%.3f (%d/%d)\n",
			s.Worker, s.VerdictLineRate(), s.VerdictLines, s.Total)
	}
	fmt.Fprintf(&b, "# FIX: tighten the EmitVerdict prompt for this substrate, do NOT trust this baseline.\n")
	fmt.Fprintf(&b, "%s\n", bar)
	return b.String(), true
}

// decisionQualityCheckerVersion tags every decision-quality ledger row with the oracle/Drive
// version, so a re-characterised oracle invalidates (never deletes) the prior rows.
const decisionQualityCheckerVersion = "a2-decision-quality-v1"

// decisionQualityLedgerPath derives the per-substrate ledger out path: if the user named an
// explicit --out it is honoured as-is (the campaign-style override); otherwise it lands a
// substrate-suffixed basename under the --out directory so a claude run and a test run never
// share a file (substrate hygiene on disk). The substrate tag is sanitised to a filesystem-
// safe token (":" → "-").
func decisionQualityLedgerPath(cfg config, substrate string) string {
	dir := filepath.Dir(cfg.out)
	if dir == "" || dir == "." {
		dir = "runs"
	}
	safe := strings.NewReplacer(":", "-", "/", "-", " ", "_").Replace(substrate)
	return filepath.Join(dir, "decision-quality-"+safe+".jsonl")
}

// renderDecisionQuality builds the per-worker A2 baseline report: a per-fixture line (the
// worker, the skill that staffed it, the program shape, whether a verdict was extracted, the
// oracle score + pass) and a per-worker-kind pass-rate rollup (the headline baseline number).
func renderDecisionQuality(bankPath, substrate string, results []decisionoracle.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "DECISION-QUALITY (A2 — Deliberator/Verifier OUTPUT correctness) — %s\n", bankPath)
	fmt.Fprintf(&b, "substrate=%s\n", substrate)
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 84))
	fmt.Fprintf(&b, "%-22s %-12s %-16s %7s %-8s %5s %6s\n", "fixture", "worker", "skill", "decided", "source", "pass", "score")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 84))
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-12s %-16s %7s %-8s %5s %6.3f\n",
			trunc(r.ID, 22), trunc(string(r.Worker), 12), trunc(r.Skill, 16),
			yesNo(r.Score.Decided), verdictSourceMark(r.VerdictSource), passMark(r.Score.Pass), r.Score.Score)
	}
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 84))
	fmt.Fprintf(&b, "source: line=stated VERDICT contract line | fallback=prose parser | none=no verdict\n")

	stats := decisionoracle.Rollup(results)
	fmt.Fprintf(&b, "PER-WORKER BASELINE (correct verdicts / total, mean oracle score, contract-line rate):\n")
	total, passed := 0, 0
	for _, s := range stats {
		total += s.Total
		passed += s.Passed
		fmt.Fprintf(&b, "  %-12s %d/%d correct   pass-rate=%.3f   mean-score=%.3f   (decided %d/%d, verdict-line %d/%d = %.3f)\n",
			s.Worker, s.Passed, s.Total, s.PassRate(), s.MeanScore(), s.Decided, s.Total,
			s.VerdictLines, s.Total, s.VerdictLineRate())
	}
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 84))
	overall := 0.0
	if total > 0 {
		overall = float64(passed) / float64(total)
	}
	fmt.Fprintf(&b, "TOTAL: %d fixtures | %d correct | overall pass-rate=%.3f\n", total, passed, overall)
	if substrate == "test" {
		fmt.Fprintf(&b, "NOTE: --backend test is the OFFLINE DOUBLE — its EmitVerdict states a deterministic\n"+
			"`VERDICT: <first label>` line (so source=line and the contract path is exercised), but the\n"+
			"label is a fixed first-option pick, NOT a reasoned one, so the oracle score is incidental.\n"+
			"This run proves the Drive + verdict-contract apparatus; the REAL baseline is --backend claude.\n")
	}
	return b.String()
}

// yesNo renders a decided/undecided flag for the report column.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// verdictSourceMark renders the verdict-source token for the report column ("line" | "fallback" |
// "none"), defaulting to "none" for an unset value.
func verdictSourceMark(src string) string {
	switch src {
	case "line", "fallback", "none":
		return src
	default:
		return "none"
	}
}
