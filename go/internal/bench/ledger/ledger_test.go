package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/types"
)

// measurement is a small constructor for a measurement Record in a test.
func measurement(tick int, batch string, mech types.Mechanism, arm types.Arm, item, ver string, oracle, isol bool) Record {
	return Record{
		Tick:            tick,
		BatchID:         batch,
		Mechanism:       mech,
		Tier:            types.TierAtomic,
		Arm:             arm,
		ItemID:          item,
		Seed:            int64(tick * 7),
		RawOutput:       "raw-" + item,
		OracleVerdict:   oracle,
		IsolationResult: isol,
		EventsPointer:   "runs/" + item + ".jsonl#0",
		CheckerVersion:  ver,
	}
}

// TestAppendLoadRoundTrip: every appended field survives a JSONL round-trip, in
// append order, and defaults (Kind, Status) are filled.
func TestAppendLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := []Record{
		measurement(1, "b1", types.MechGrounding, types.ArmGateOn, "i1", "v1", true, true),
		measurement(2, "b1", types.MechGrounding, types.ArmGateOff, "i1", "v1", false, false),
		measurement(3, "b1", types.MechSafety, types.ArmHarness, "i2", "v1", true, true),
	}
	for _, r := range want {
		if err := st.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Load count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		// defaults applied on write
		if got[i].Kind != KindMeasurement {
			t.Errorf("row %d Kind = %q, want %q", i, got[i].Kind, KindMeasurement)
		}
		if got[i].Status != StatusActive {
			t.Errorf("row %d Status = %q, want %q", i, got[i].Status, StatusActive)
		}
		// load order == append order
		if got[i].ItemID != want[i].ItemID || got[i].Arm != want[i].Arm {
			t.Errorf("row %d = (%s,%s), want (%s,%s)", i, got[i].ItemID, got[i].Arm, want[i].ItemID, want[i].Arm)
		}
		// every named §5.7 field round-trips
		if got[i].Seed != want[i].Seed ||
			got[i].RawOutput != want[i].RawOutput ||
			got[i].OracleVerdict != want[i].OracleVerdict ||
			got[i].IsolationResult != want[i].IsolationResult ||
			got[i].EventsPointer != want[i].EventsPointer ||
			got[i].CheckerVersion != want[i].CheckerVersion ||
			got[i].Tick != want[i].Tick ||
			got[i].BatchID != want[i].BatchID ||
			got[i].Mechanism != want[i].Mechanism {
			t.Errorf("row %d did not round-trip: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// TestReopenAppendsNeverTruncate: opening an existing ledger and appending grows
// it (never truncates) — the append-only invariant across handles.
func TestReopenAppendsNeverTruncate(t *testing.T) {
	dir := t.TempDir()
	st1, _ := Open(dir)
	if err := st1.Append(measurement(1, "b1", types.MechGrounding, types.ArmBare, "i1", "v1", true, true)); err != nil {
		t.Fatal(err)
	}
	st2, _ := Open(dir) // re-open same dir
	if err := st2.Append(measurement(2, "b1", types.MechGrounding, types.ArmHarness, "i1", "v1", true, true)); err != nil {
		t.Fatal(err)
	}
	got, _ := st2.Load()
	if len(got) != 2 {
		t.Fatalf("after reopen+append, len = %d, want 2 (reopen must not truncate)", len(got))
	}
}

// TestInvalidateNotDelete: invalidation APPENDS markers, never rewrites/deletes —
// the full history (every original row + the markers) is preserved on disk, and
// only the effective status flips.
func TestInvalidateNotDelete(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)

	// Two measurement rows on checker v1, one already on v2.
	rows := []Record{
		measurement(1, "b1", types.MechGrounding, types.ArmGateOn, "i1", "v1", true, true),
		measurement(2, "b1", types.MechGrounding, types.ArmGateOff, "i1", "v1", false, false),
		measurement(3, "b1", types.MechGrounding, types.ArmGateOn, "i2", "v2", true, true),
	}
	for _, r := range rows {
		if err := st.Append(r); err != nil {
			t.Fatal(err)
		}
	}

	rawBefore, _ := os.ReadFile(filepath.Join(dir, ledgerFile))
	linesBefore := nonEmptyLines(rawBefore)
	if linesBefore != 3 {
		t.Fatalf("expected 3 lines before invalidate, got %d", linesBefore)
	}

	// Re-characterize the checker to v2 at tick 10. The two v1 rows are dependents.
	n, err := st.Invalidate("v2", 10)
	if err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if n != 2 {
		t.Fatalf("Invalidate newly-invalidated = %d, want 2 (the two v1 rows)", n)
	}

	// invalidate-not-delete: the original 3 lines are still verbatim on disk; only
	// new marker lines were appended.
	rawAfter, _ := os.ReadFile(filepath.Join(dir, ledgerFile))
	if !strings.HasPrefix(string(rawAfter), string(rawBefore)) {
		t.Fatalf("original ledger bytes were rewritten — invalidate-not-delete violated")
	}
	linesAfter := nonEmptyLines(rawAfter)
	if linesAfter != 5 { // 3 original + 2 markers
		t.Fatalf("expected 5 lines after invalidate (3+2 markers), got %d", linesAfter)
	}

	// Effective status: the two v1 measurement rows are invalidated, the v2 row
	// stays active; the raw history still contains all 3 measurement rows.
	resolved, _ := st.Resolved()
	var meas []Record
	for _, r := range resolved {
		if r.Kind == KindMeasurement {
			meas = append(meas, r)
		}
	}
	if len(meas) != 3 {
		t.Fatalf("history lost rows: %d measurement rows, want 3", len(meas))
	}
	wantStatus := map[string]Status{
		"i1/gate-on":  StatusInvalidated,
		"i1/gate-off": StatusInvalidated,
		"i2/gate-on":  StatusActive,
	}
	for _, r := range meas {
		key := r.ItemID + "/" + string(r.Arm)
		if r.Status != wantStatus[key] {
			t.Errorf("%s effective status = %q, want %q", key, r.Status, wantStatus[key])
		}
	}

	// ActiveMeasurements returns only the v2 row.
	active, _ := st.ActiveMeasurements()
	if len(active) != 1 || active[0].ItemID != "i2" {
		t.Fatalf("ActiveMeasurements = %+v, want only i2", active)
	}
}

// TestInvalidateIdempotent: calling Invalidate again for the same version does not
// re-mark already-invalidated rows (no double markers).
func TestInvalidateIdempotent(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	if err := st.Append(measurement(1, "b1", types.MechSafety, types.ArmGateOn, "i1", "v1", true, true)); err != nil {
		t.Fatal(err)
	}
	n1, err := st.Invalidate("v2", 5)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := st.Invalidate("v2", 6)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 || n2 != 0 {
		t.Fatalf("idempotency: first=%d (want 1), second=%d (want 0)", n1, n2)
	}
}

// TestInvalidateRejectsEmptyVersion: an empty checker version is a programming
// error, not a no-op.
func TestInvalidateRejectsEmptyVersion(t *testing.T) {
	st, _ := Open(t.TempDir())
	if _, err := st.Invalidate("", 1); err == nil {
		t.Fatal("Invalidate(\"\") should error")
	}
}

// TestIsValidated_KeepFlagAndDefault: IsValidated reads the latest active verdict
// row for (mechanism, iter): keep→true, flag→false, none→false.
func TestIsValidated(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)

	contrast := &types.Contrast{
		HarnessMinusBare:   types.Estimate{Point: 0.30, CILow: 0.22, CIHigh: 0.38, N: 120},
		GateOnMinusGateOff: types.Estimate{Point: 0.20, CILow: 0.16, CIHigh: 0.24, N: 120},
		IsolationRate:      types.Estimate{Point: 0.92, CILow: 0.86, CIHigh: 0.96, N: 60},
	}

	// grounding KEPT at iter 1.
	if err := st.RecordVerdict(VerdictInput{
		Tick: 100, Mechanism: types.MechGrounding, Tier: types.TierAtomic, IterK: 1,
		KeepVerdict: VerdictKeep, Contrast: contrast, RawP: 0.0003, BHP: 0.0011,
		IsolationFloor: 0.8, MDE: 0.15,
	}); err != nil {
		t.Fatal(err)
	}
	// safety FLAGGED at iter 1.
	if err := st.RecordVerdict(VerdictInput{
		Tick: 101, Mechanism: types.MechSafety, Tier: types.TierAtomic, IterK: 1,
		KeepVerdict: VerdictFlag, Contrast: contrast, RawP: 0.4, BHP: 0.6,
	}); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		mech types.Mechanism
		iter int
		want bool
	}{
		{types.MechGrounding, 1, true},  // recorded keep
		{types.MechSafety, 1, false},    // recorded flag
		{types.MechGrounding, 0, false}, // no verdict at iter 0
		{types.MechStability, 1, false}, // never recorded
	}
	for _, c := range checks {
		got, err := st.IsValidated(c.mech, c.iter)
		if err != nil {
			t.Fatalf("IsValidated(%s,%d): %v", c.mech, c.iter, err)
		}
		if got != c.want {
			t.Errorf("IsValidated(%s,%d) = %v, want %v", c.mech, c.iter, got, c.want)
		}
	}
}

// TestIsValidated_LatestWins: a later verdict for the same (mechanism, iter)
// overturns an earlier one without deleting it (invalidate-not-delete at the
// verdict layer).
func TestIsValidated_LatestWins(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	// first KEEP, then a re-run FLAG at the same iter — the last word wins.
	_ = st.RecordVerdict(VerdictInput{Tick: 1, Mechanism: types.MechStability, Tier: types.TierScenario, IterK: 2, KeepVerdict: VerdictKeep})
	_ = st.RecordVerdict(VerdictInput{Tick: 2, Mechanism: types.MechStability, Tier: types.TierScenario, IterK: 2, KeepVerdict: VerdictFlag})

	got, err := st.IsValidated(types.MechStability, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("latest verdict (flag) should overturn the earlier keep")
	}
	// both verdict rows still on disk.
	all, _ := st.Load()
	verdicts := 0
	for _, r := range all {
		if r.Kind == KindVerdict {
			verdicts++
		}
	}
	if verdicts != 2 {
		t.Fatalf("both verdict rows must be preserved, found %d", verdicts)
	}
}

// TestRecordVerdictRejectsBadVerdict: only keep|flag are accepted.
func TestRecordVerdictRejectsBadVerdict(t *testing.T) {
	st, _ := Open(t.TempDir())
	if err := st.RecordVerdict(VerdictInput{Tick: 1, Mechanism: types.MechGrounding, KeepVerdict: "maybe"}); err == nil {
		t.Fatal("RecordVerdict should reject an unknown verdict string")
	}
}

// TestLoadMissingIsEmpty: loading a never-written ledger is an empty history, not
// an error.
func TestLoadMissingIsEmpty(t *testing.T) {
	st, _ := Open(t.TempDir())
	rows, err := st.Load()
	if err != nil {
		t.Fatalf("Load of empty ledger: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty ledger Load len = %d, want 0", len(rows))
	}
}

// TestLoadSkipsMalformedLine: a corrupt JSONL line is skipped, never a crash (the
// repo-wide best-effort discipline).
func TestLoadSkipsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	_ = st.Append(measurement(1, "b1", types.MechGrounding, types.ArmBare, "i1", "v1", true, true))
	// inject garbage between valid lines.
	f, _ := os.OpenFile(filepath.Join(dir, ledgerFile), os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString("{not valid json\n\n")
	_ = f.Close()
	_ = st.Append(measurement(2, "b1", types.MechGrounding, types.ArmHarness, "i1", "v1", true, true))

	rows, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("malformed line not skipped: len = %d, want 2", len(rows))
	}
}

// TestReport_Deterministic: the report renders a keep and a flag row, in sorted
// order, plain-text, with the §5.7 columns and no emoji; output is byte-identical
// across renders.
func TestReport(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	contrast := &types.Contrast{
		HarnessMinusBare:   types.Estimate{Point: 0.30, CILow: 0.22, CIHigh: 0.38, N: 120},
		GateOnMinusGateOff: types.Estimate{Point: 0.20, CILow: 0.16, CIHigh: 0.24, N: 120},
		IsolationRate:      types.Estimate{Point: 0.92, CILow: 0.86, CIHigh: 0.96, N: 60},
	}
	lowIsol := &types.Contrast{
		HarnessMinusBare:   types.Estimate{Point: 0.10},
		GateOnMinusGateOff: types.Estimate{Point: 0.02, CILow: -0.05, CIHigh: 0.09},
		IsolationRate:      types.Estimate{Point: 0.40},
	}
	_ = st.RecordVerdict(VerdictInput{Tick: 1, Mechanism: types.MechSafety, Tier: types.TierAtomic, IterK: 1, KeepVerdict: VerdictFlag, Contrast: lowIsol, RawP: 0.4, BHP: 0.6, IsolationFloor: 0.8, MDE: 0.15})
	_ = st.RecordVerdict(VerdictInput{Tick: 2, Mechanism: types.MechGrounding, Tier: types.TierAtomic, IterK: 1, KeepVerdict: VerdictKeep, Contrast: contrast, RawP: 0.0003, BHP: 0.0011, IsolationFloor: 0.8, MDE: 0.15})

	rep, err := st.Report(NewReportOptions())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	// grounding sorts before safety.
	gi := strings.Index(rep, "grounding")
	si := strings.Index(rep, "safety")
	if gi < 0 || si < 0 || gi > si {
		t.Fatalf("mechanisms not in sorted order:\n%s", rep)
	}
	// the load-bearing columns + verdicts are present.
	for _, want := range []string{"harness-bare", "on-off", "isol-rate", "raw p", "BH p", "verdict", "KEEP", "FLAG", "+0.200", "[+0.160,+0.240]", "0.40<0.80!"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q:\n%s", want, rep)
		}
	}
	// no emoji / no lipgloss escape codes.
	if strings.ContainsRune(rep, '\x1b') {
		t.Error("report contains an ANSI escape (lipgloss/color leaked into the bench layer)")
	}
	// deterministic.
	rep2, _ := st.Report(NewReportOptions())
	if rep != rep2 {
		t.Error("report is not deterministic across renders")
	}
}

// TestReport_Empty: a ledger with no verdicts renders the empty notice, not a crash.
func TestReportEmpty(t *testing.T) {
	st, _ := Open(t.TempDir())
	rep, err := st.Report(NewReportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep, "no keep-rule verdicts") {
		t.Fatalf("empty report missing notice:\n%s", rep)
	}
}

// nonEmptyLines counts non-blank lines in a JSONL blob.
func nonEmptyLines(b []byte) int {
	n := 0
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	return n
}
