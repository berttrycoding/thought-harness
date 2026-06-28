package realhard

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// instrument_validation_test.go — A3: the END-TO-END proof that the two A/B autonomy
// instruments (the METR time-horizon + the sub-agent guard) return REAL, NON-VACUOUS
// verdicts on a `--backend test` run, driven by the offline instrument-validation bank
// (InstrumentValidationTasks) the test double's REAL solver produces a genuine spread on.
//
// This is the cognition-property layer the build is "done" at: not "the reducer is
// correct on planted inputs" (subagentguard_test / timehorizon_test already pin that),
// but "the suite-driven instruments, fed real test-double behaviour, produce a real H50
// and a real (non-NA) guard verdict" — the gap the prompt's DoD names.

// TestInstrumentBankProducesRealSpreadOnDouble: the load-bearing precondition — the
// test double's REAL solver primitive solves the SHORT single-op tasks (p=1) and fails
// the LONG multi-step chains (p=0). Without this genuine, deterministic spread the
// instruments would be vacuous; with it they are real. NOT a faked oracle — the solver
// actually computes the binary ops and actually chokes on the chains.
func TestInstrumentBankProducesRealSpreadOnDouble(t *testing.T) {
	factory := func(seed int64, temp float64) backends.Backend { return backends.NewTest() }
	var solved, failed int
	for _, tk := range InstrumentValidationTasks() {
		r, err := RunHarness(tk, factory, 1729, 60, t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", tk.ID, err)
		}
		short := strings.Contains(tk.ID, "easy")
		if short && !r.Verdict.Solved {
			t.Errorf("%s (short single-op) should be SOLVED by the double's solver; got FAIL (%q)", tk.ID, r.Answer)
		}
		if !short && r.Verdict.Solved {
			t.Errorf("%s (long multi-step) should FAIL the double's solver; got SOLVE (%q)", tk.ID, r.Answer)
		}
		if r.Verdict.Solved {
			solved++
		} else {
			failed++
		}
	}
	// the genuine spread the instruments need: BOTH some solved and some failed.
	if solved == 0 || failed == 0 {
		t.Fatalf("instrument bank must produce BOTH solved and failed tasks on the double for a real spread; got solved=%d failed=%d", solved, failed)
	}
}

// TestInstrumentABMETRFitsRealHorizon: the A3(2) DoD — on a `--backend test` A/B over the
// instrument bank, the METR estimator returns a REAL H50 (Fitted, a resolvable negative
// slope, an ordered reliability band), NOT a DEGENERATE / UNIDENTIFIED NA and NOT a fake
// 0s. The horizon must land in the gap between the longest SOLVED task (8min) and the
// shortest FAILED task (30min) — the real difficulty boundary the double exposes.
func TestInstrumentABMETRFitsRealHorizon(t *testing.T) {
	rep := runInstrumentAB(t, 3)
	h := rep.Horizon
	if !h.Fitted {
		t.Fatalf("METR must FIT a real horizon on the instrument bank; got NA: %s", h.Reason)
	}
	if h.Beta1 >= 0 {
		t.Errorf("the slope must be resolvably negative (longer=harder); got Beta1=%g", h.Beta1)
	}
	if h.Horizon50 <= 0 {
		t.Fatalf("H50 must be a positive number, not the degenerate 0; got %g", h.Horizon50)
	}
	// the real boundary the double exposes: solved up to 8min, failed from 30min.
	if !(h.Horizon50 > 8 && h.Horizon50 < 30) {
		t.Errorf("H50 should land between the longest solved (8min) and shortest failed (30min) task; got %.2fmin", h.Horizon50)
	}
	// the reliability band is ordered (a higher reliability demands a shorter task).
	if !(h.Horizon80 < h.Horizon50 && h.Horizon50 < h.Horizon20) {
		t.Errorf("reliability band must be ordered H80<H50<H20; got %.2f, %.2f, %.2f", h.Horizon80, h.Horizon50, h.Horizon20)
	}
}

// TestInstrumentABGuardReturnsRealVerdict: the A3(1) DoD (honestly scoped) — on a
// `--backend test` A/B over the instrument bank, the sub-agent guard returns a REAL,
// NON-NOT-APPLICABLE verdict from REAL paired per-task counts (the single-strong arm
// genuinely ran: disableSubAgentFanout is wired). On the deterministic double the two
// engine arms produce byte-identical answers (sub-agent fan-out is outcome-neutral by
// design — the goldens stay identical regardless of fan-out width), so the honest
// verdict is INCONCLUSIVE: the arms run, are paired, and the CI is computed — it just
// cannot resolve to PASS/HOLDS-BACK without a live model where fan-out changes the
// reasoning (subagentguard_test.go pins PASS/HOLDS-BACK on planted inputs; that is the
// live-claude verdict).
func TestInstrumentABGuardReturnsRealVerdict(t *testing.T) {
	rep := runInstrumentAB(t, 3)
	g := rep.Guard
	if g.Verdict == SubAgentNotApplicable {
		t.Fatalf("guard must NOT be NOT-APPLICABLE — the single-strong arm ran; got %s (%s)", g.Verdict, g.Reason)
	}
	if !g.HasBaseline {
		t.Error("the single-strong baseline arm must have produced counts (HasBaseline)")
	}
	if g.NTasks != len(InstrumentValidationTasks()) {
		t.Errorf("guard should pair every instrument task; NTasks=%d want %d", g.NTasks, len(InstrumentValidationTasks()))
	}
	// real per-task counts: the solved-task rate must be non-zero on BOTH arms (the double
	// genuinely solves the short ops), confirming the counts are real, not all-zero.
	if g.SingleStrongRate <= 0 || g.TeamRate <= 0 {
		t.Errorf("both arms should solve the short ops -> non-zero rates; single=%g team=%g", g.SingleStrongRate, g.TeamRate)
	}
	// the honest ceiling on the double: the arms are outcome-identical -> INCONCLUSIVE.
	if g.Verdict != SubAgentInconclusive {
		t.Errorf("on the deterministic double the arms are outcome-identical (fan-out is golden-preserving) -> INCONCLUSIVE; got %s. (PASS/HOLDS-BACK is a live-claude verdict)", g.Verdict)
	}
}

// TestMETRUnidentifiedOnAllSaturated: the instrument fix — an ALL-SATURATED dataset
// (every task p=0, or every task p=1) has no difficulty gradient; the slope is
// numerically zero and the horizon is UNIDENTIFIED. This guards the degenerate fit that
// previously slipped through (a -1e-15 FP slope read as "fitted" with a meaningless
// H50=0s). Both signs of the FP noise must report the SAME honest NA.
func TestMETRUnidentifiedOnAllSaturated(t *testing.T) {
	lengths := []float64{3, 10, 20, 40}
	build := func(p float64) []THTask {
		var ts []THTask
		for i, m := range lengths {
			ts = append(ts, THTask{TaskID: string(rune('a' + i)), HumanMin: m, PHat: p, K: 3})
		}
		return ts
	}
	for _, p := range []float64{0.0, 1.0} {
		r := TimeHorizon(build(p))
		if r.Fitted {
			t.Errorf("all-saturated p=%g must be UNIDENTIFIED (no gradient), not fitted; got H50=%g", p, r.Horizon50)
		}
		if r.Horizon50 != 0 {
			t.Errorf("an unidentified fit must report H50=0 (the NA sentinel), not a fabricated number; got %g", r.Horizon50)
		}
		if !strings.Contains(r.Reason, "UNIDENTIFIED") {
			t.Errorf("all-saturated reason should name UNIDENTIFIED; got %q", r.Reason)
		}
	}
}

// runInstrumentAB drives the full bare+harness+single-strong A/B over the instrument bank
// on the offline double and returns the assembled report. Helper for the A3 DoD tests.
func runInstrumentAB(t *testing.T, replays int) ABReport {
	t.Helper()
	factory := func(seed int64, temp float64) backends.Backend { return backends.NewTest() }
	rep, armErrs, err := RunAB(ABConfig{
		Factory:      factory,
		Replays:      replays,
		SeedBase:     1729,
		MaxTicks:     60,
		Concurrency:  1,
		Tasks:        InstrumentValidationTasks(),
		PassK:        2,
		Substrate:    "test",
		SingleStrong: true,
	})
	if err != nil {
		t.Fatalf("instrument A/B run: %v", err)
	}
	if len(armErrs) > 0 {
		t.Fatalf("instrument A/B had %d arm failures (expected clean on the double): %v", len(armErrs), armErrs)
	}
	return rep
}
