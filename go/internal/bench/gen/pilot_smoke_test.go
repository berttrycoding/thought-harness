package gen

// pilot_smoke_test.go — the OFFLINE validation harness for the authored PILOT
// banks (internal/bench/banks/pilot/). It proves, with no real model and a fixed
// seed, that:
//
//  1. every JSONL line in every mechanism×tier bank parses into the correct
//     internal/bench/types struct (LoadBankA / LoadBankB), with per-mechanism
//     counts;
//  2. gen.CheckBankA / CheckBankB run clean (any domain-mix / gap-rule finding is
//     surfaced as a t.Log warning, never a build failure — the banks are a tiny
//     pilot and the soft proportions are expected to wobble);
//  3. the runner pipeline runs end-to-end OFFLINE: a handful of Tier-A items and
//     one Tier-B scenario through tiera.RunItem / tierb.RunScenario under the
//     deterministic runner.TestFactory, for the bare and harness arms, returning
//     well-formed ItemResult / ScenarioResult (the pass/fail VALUE is not asserted —
//     the plumbing running clean IS the assertion), and whether the isolation
//     predicate fires on the harness arm.
//
// It stays green in CI because it never touches the network (runner.TestFactory is
// the offline deterministic test double) and is paired on a fixed seed.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	"github.com/berttrycoding/thought-harness/internal/bench/tiera"
	"github.com/berttrycoding/thought-harness/internal/bench/tierb"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// pilotMechanisms is the canonical roster the pilot banks cover (one Tier-A bank +
// one Tier-B bank per mechanism). A missing bank file is a hard failure.
var pilotMechanisms = []benchtypes.Mechanism{
	benchtypes.MechGrounding,
	benchtypes.MechMultiStepRetrace,
	benchtypes.MechSelfImprovement,
	benchtypes.MechContinuousAutonomy,
	benchtypes.MechStability,
	benchtypes.MechSafety,
}

// pilotSmokeSeed is the fixed paired seed used across arms in the smoke run so the
// offline pipeline is reproducible.
const pilotSmokeSeed int64 = 1729

// resolvePilotRoot finds the on-disk pilot banks directory from the test's working
// directory (go test runs with cwd = the package dir, internal/bench/gen). It walks
// up looking for the module root (go.mod) and joins gen.PilotBanksRoot, then falls
// back to the sibling ../banks/pilot. A missing root fails the test loud.
func resolvePilotRoot(t *testing.T) string {
	t.Helper()
	// Walk up from cwd to the module root (the dir holding go.mod), then join the
	// canonical PilotBanksRoot (which is module-root-relative).
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			root := filepath.Join(d, PilotBanksRoot)
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				return root
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	// Fallback: the banks are a sibling of the gen package (../banks/pilot).
	if fallback := filepath.Join(dir, "..", "banks", "pilot"); dirExists(fallback) {
		return fallback
	}
	t.Fatalf("could not locate pilot banks root (looked for %s up the tree from %s)", PilotBanksRoot, dir)
	return ""
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// TestPilotBanksLoad asserts every authored pilot bank file parses cleanly into the
// correct types struct (LoadBankA → []TierAItem, LoadBankB → []TierBScenario) and
// reports the per-mechanism per-tier counts. A malformed line (LoadBank fails loud
// with its line number) is a hard failure — the report names the bad bank.
func TestPilotBanksLoad(t *testing.T) {
	root := resolvePilotRoot(t)
	t.Logf("pilot banks root: %s", root)

	var totalA, totalB int
	for _, m := range pilotMechanisms {
		pathA := BankFileA(root, m)
		itemsA, err := LoadBankA(pathA)
		if err != nil {
			t.Errorf("[%s] Tier-A LoadBankA(%s): %v", m, pathA, err)
		} else {
			// Every loaded item must carry the bank's own mechanism (a self-describing
			// bank). A mismatch is a malformed item to fix.
			for _, it := range itemsA {
				if it.Mechanism != m {
					t.Errorf("[%s] Tier-A item %q carries mechanism %q (bank/item mismatch)", m, it.ID, it.Mechanism)
				}
				if it.ID == "" {
					t.Errorf("[%s] Tier-A item has empty ID (malformed)", m)
				}
			}
			totalA += len(itemsA)
		}

		pathB := BankFileB(root, m)
		scnsB, err := LoadBankB(pathB)
		if err != nil {
			t.Errorf("[%s] Tier-B LoadBankB(%s): %v", m, pathB, err)
		} else {
			for _, s := range scnsB {
				if s.Mechanism != m {
					t.Errorf("[%s] Tier-B scenario %q carries mechanism %q (bank/scenario mismatch)", m, s.ID, s.Mechanism)
				}
				if s.ID == "" {
					t.Errorf("[%s] Tier-B scenario has empty ID (malformed)", m)
				}
				if len(s.Turns) == 0 {
					t.Errorf("[%s] Tier-B scenario %q has no turns (malformed)", m, s.ID)
				}
			}
			totalB += len(scnsB)
		}

		t.Logf("[%s] Tier-A items=%d  Tier-B scenarios=%d", m, len(itemsA), len(scnsB))
	}
	t.Logf("TOTAL pilot items: Tier-A=%d  Tier-B=%d  (mechanisms=%d)", totalA, totalB, len(pilotMechanisms))

	if totalA == 0 || totalB == 0 {
		t.Fatalf("pilot banks loaded zero items (A=%d B=%d) — banks missing or empty", totalA, totalB)
	}
}

// TestPilotBanksCheck runs gen.CheckBankA / CheckBankB on every mechanism bank and
// reports findings. A FATAL violation (a hard floor / structural breach: unknown
// domain, mechanism inconsistency, G9 non-SWE floor, G7 trivial cap, the per-
// mechanism gap rules) is logged but does NOT fail the build per the task contract
// (warn, do not fail) — except the empty-bank / mechanism-consistency structural
// breaches, which indicate a genuinely broken bank file and are surfaced as test
// failures so they can't rot silently. Soft domain-mix warnings are t.Log only.
func TestPilotBanksCheck(t *testing.T) {
	root := resolvePilotRoot(t)
	for _, m := range pilotMechanisms {
		itemsA, err := LoadBankA(BankFileA(root, m))
		if err != nil {
			t.Errorf("[%s] Tier-A load for CheckBank: %v", m, err)
			continue
		}
		repA := CheckBankA(itemsA)
		logCheckReport(t, repA)

		scnsB, err := LoadBankB(BankFileB(root, m))
		if err != nil {
			t.Errorf("[%s] Tier-B load for CheckBank: %v", m, err)
			continue
		}
		repB := CheckBankB(scnsB)
		logCheckReport(t, repB)
	}
}

// logCheckReport prints a CheckReport's status + every violation as test logs
// (warnings, per the task contract). The structural breaches that mean a genuinely
// broken bank file (empty-bank / mechanism-consistency) are escalated to t.Errorf so
// a corrupt bank is caught, while domain-mix / gap-rule findings stay warn-only.
func logCheckReport(t *testing.T, rep CheckReport) {
	t.Helper()
	t.Logf("CheckBank[%s/tier%s] N=%d ok=%v", rep.Mechanism, rep.Tier, rep.N, rep.OK())
	for _, v := range rep.Violations {
		switch v.Rule {
		case "empty-bank", "mechanism-consistency":
			// A genuinely broken bank file — not a tuning warning. Fail loud.
			t.Errorf("  [%s/tier%s] STRUCTURAL: %s", rep.Mechanism, rep.Tier, v.String())
		default:
			t.Logf("  [%s/tier%s] %s", rep.Mechanism, rep.Tier, v.String())
		}
	}
}

// TestPilotRunnerSmoke runs the runner pipeline end-to-end OFFLINE: 3 Tier-A items
// (from 3 different mechanism banks) and 1 Tier-B scenario, each under the bare and
// harness arms at the fixed pilot seed, against runner.TestFactory (the deterministic
// offline test double — NO network, NO real model). It asserts only that the pipeline
// returns a WELL-FORMED ItemResult / ScenarioResult (the right ID/Arm/Seed echoed, a
// boolean Pass, a Cost) — the pass/fail value is not the point; the plumbing running
// clean IS. It records whether the isolation predicate fired on the harness arm.
func TestPilotRunnerSmoke(t *testing.T) {
	root := resolvePilotRoot(t)

	// --- Tier-A: pick the first item from three different mechanism banks. ---
	tierAMechs := []benchtypes.Mechanism{
		benchtypes.MechGrounding,
		benchtypes.MechMultiStepRetrace,
		benchtypes.MechSafety,
	}
	arms := []benchtypes.Arm{benchtypes.ArmBare, benchtypes.ArmHarness}

	for _, m := range tierAMechs {
		items, err := LoadBankA(BankFileA(root, m))
		if err != nil {
			t.Fatalf("[%s] load Tier-A for smoke: %v", m, err)
		}
		if len(items) == 0 {
			t.Fatalf("[%s] Tier-A bank is empty — cannot smoke", m)
		}
		item := items[0]
		for _, arm := range arms {
			res := tiera.RunItem(item, arm, pilotSmokeSeed, runner.TestFactory)
			assertItemResultWellFormed(t, item.ID, arm, res)
			isoNote := "n/a (bare arm has no trace)"
			if arm == benchtypes.ArmHarness {
				isoNote = boolStr(res.IsolationResult, "FIRED", "did-not-fire")
			}
			t.Logf("Tier-A smoke [%s/%s] id=%s pass=%v oracle=%v isolation=%s calls=%d steps=%d",
				m, arm, item.ID, res.Pass, res.OracleVerdict, isoNote, res.Cost.ModelCalls, res.Cost.Steps)
		}
	}

	// --- Tier-B: one scenario (grounding bank, first scenario) under both arms. ---
	scnMech := benchtypes.MechGrounding
	scns, err := LoadBankB(BankFileB(root, scnMech))
	if err != nil {
		t.Fatalf("[%s] load Tier-B for smoke: %v", scnMech, err)
	}
	if len(scns) == 0 {
		t.Fatalf("[%s] Tier-B bank is empty — cannot smoke", scnMech)
	}
	scn := scns[0]
	for _, arm := range arms {
		// Workspace "" ⇒ the offline heuristic-act path (no real sandboxed tools).
		res := tierb.RunScenario(scn, arm, pilotSmokeSeed, runner.TestFactory, "")
		assertScenarioResultWellFormed(t, scn.ID, arm, res)
		isoNote := "n/a (bare arm has no trace)"
		if arm == benchtypes.ArmHarness {
			isoNote = boolStr(res.IsolationResult, "FIRED", "did-not-fire")
		}
		t.Logf("Tier-B smoke [%s/%s] id=%s pass=%v oracle=%v isolation=%s calls=%d steps=%d",
			scnMech, arm, scn.ID, res.Pass, res.OracleVerdict, isoNote, res.Cost.ModelCalls, res.Cost.Steps)
	}
}

// assertItemResultWellFormed checks the runner returned a structurally complete
// ItemResult: the ID + Arm + Seed are echoed back, and a non-negative Cost. The Pass
// value is deliberately NOT asserted — the plumbing running clean is the point.
func assertItemResultWellFormed(t *testing.T, wantID string, wantArm benchtypes.Arm, res benchtypes.ItemResult) {
	t.Helper()
	if res.ID != wantID {
		t.Errorf("ItemResult.ID = %q, want %q", res.ID, wantID)
	}
	if res.Arm != wantArm {
		t.Errorf("ItemResult.Arm = %q, want %q", res.Arm, wantArm)
	}
	if res.Seed != pilotSmokeSeed {
		t.Errorf("ItemResult.Seed = %d, want %d", res.Seed, pilotSmokeSeed)
	}
	if res.Cost.ModelCalls < 0 || res.Cost.Steps < 0 {
		t.Errorf("ItemResult.Cost has negative fields: %+v", res.Cost)
	}
}

// assertScenarioResultWellFormed is the Tier-B analogue.
func assertScenarioResultWellFormed(t *testing.T, wantID string, wantArm benchtypes.Arm, res benchtypes.ScenarioResult) {
	t.Helper()
	if res.ID != wantID {
		t.Errorf("ScenarioResult.ID = %q, want %q", res.ID, wantID)
	}
	if res.Arm != wantArm {
		t.Errorf("ScenarioResult.Arm = %q, want %q", res.Arm, wantArm)
	}
	if res.Seed != pilotSmokeSeed {
		t.Errorf("ScenarioResult.Seed = %d, want %d", res.Seed, pilotSmokeSeed)
	}
	if res.Cost.ModelCalls < 0 || res.Cost.Steps < 0 {
		t.Errorf("ScenarioResult.Cost has negative fields: %+v", res.Cost)
	}
}

func boolStr(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}
