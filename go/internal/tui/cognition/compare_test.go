package cognition

// compare_test.go — the G2 COMPARE benchmark's PROPERTY tests (the THINKING the power-ON/OFF diff must
// read, not just that it computes). Each test asserts a benchmark CLAIM the redesign §7 definition of
// done depends on: who won, by how much faster, how much more grounded, how many more tokens, and
// WHERE the two trajectories forked. Driven off the loader's recorded-stream fixtures (onSession =
// power-ON that SOLVED, offSession = power-OFF that gave up) so the verdict is pinned deterministically.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestCompareDecisiveWinOnBeatsOff — the headline thinking: a power-ON run that SOLVED beats a
// power-OFF run that gave up. A is the ON arm, B the OFF arm; the report must name A the winner, mark
// the win DECISIVE (exactly one arm solved), and read each arm's recorded verdict — NOT re-judge.
func TestCompareDecisiveWinOnBeatsOff(t *testing.T) {
	a := RecordFromFrozen(onSession(), nil)
	b := RecordFromFrozen(offSession(), nil)
	rep := BuildCompareReport(a, b)

	if rep.Winner != "A" {
		t.Errorf("the SOLVED ON arm must win; Winner=%q (A verdict %q, B verdict %q)", rep.Winner, rep.AVerdict, rep.BVerdict)
	}
	if !rep.VerdictDecisive {
		t.Error("exactly one arm SOLVED — the win must read DECISIVE")
	}
	if rep.AVerdict != "SOLVED" || rep.BVerdict != "UNSOLVED" {
		t.Errorf("verdicts read from the recorded trajectory, not re-judged: A=%q B=%q (want SOLVED, UNSOLVED)", rep.AVerdict, rep.BVerdict)
	}
	if !strings.Contains(rep.Headline, "A wins") || !strings.Contains(rep.Headline, "SOLVED") {
		t.Errorf("the headline must state the decisive solve; got %q", rep.Headline)
	}
}

// TestCompareGroundingDelta — the anti-hallucination signal: the ON arm imported reality (grounded)
// where the OFF arm never did, so the report's grounded delta must be positive (A imported more).
func TestCompareGroundingDelta(t *testing.T) {
	a := RecordFromFrozen(onSession(), nil)
	b := RecordFromFrozen(offSession(), nil)
	rep := BuildCompareReport(a, b)

	if rep.AGrounded <= rep.BGrounded {
		t.Errorf("the ON arm grounded more than the OFF arm; A=%d B=%d", rep.AGrounded, rep.BGrounded)
	}
	if rep.GroundedDelta != rep.AGrounded-rep.BGrounded {
		t.Errorf("GroundedDelta must be A-B; got %d (A=%d B=%d)", rep.GroundedDelta, rep.AGrounded, rep.BGrounded)
	}
	if rep.GroundedDelta <= 0 {
		t.Errorf("the ON arm must read as importing more reality; GroundedDelta=%d", rep.GroundedDelta)
	}
}

// TestCompareDivergenceTickIdentified — the redesign §7 "the tick where the two trajectories diverged
// + why". onSession ACTs at t6 (imports ground truth); offSession BACKTRACKs at t6 (keeps guessing) —
// the first move-level fork. The report must point to that tick with a non-empty why.
func TestCompareDivergenceTickIdentified(t *testing.T) {
	a := RecordFromFrozen(onSession(), nil)
	b := RecordFromFrozen(offSession(), nil)
	rep := BuildCompareReport(a, b)

	if rep.DivergenceTick < 0 {
		t.Fatal("the two trajectories DO fork (ACT vs BACKTRACK at t6) — the divergence finder must report a tick")
	}
	if rep.DivergenceWhy == "" {
		t.Error("a divergence tick with no WHY is not actionable — the report must say what forked")
	}
}

// TestCompareLatencyFasterArm — when both arms delivered, the report names the faster-to-DELIVER arm
// and the percentage. A delivered (ImpToDeliver>0) and B never delivered (gave up, ImpToDeliver==0),
// so latency is UNcomparable — the report must NOT crown a faster arm off a one-sided delivery (a
// give-up has no latency, it simply did not finish; comparing would be a divide-by-zero lie).
func TestCompareLatencyFasterArm(t *testing.T) {
	a := RecordFromFrozen(onSession(), nil)  // delivers
	b := RecordFromFrozen(offSession(), nil) // gives up, never delivers
	rep := BuildCompareReport(a, b)

	if b.ImpToDeliver != 0 {
		t.Fatalf("fixture precondition: the OFF arm must never deliver; got ImpToDeliver=%d", b.ImpToDeliver)
	}
	if rep.FasterArm != "" {
		t.Errorf("a one-sided delivery has no honest latency comparison; FasterArm must be empty, got %q", rep.FasterArm)
	}

	// now a synthetic both-delivered pair: A faster than B.
	fastA := AnalysisRecord{SolveVerdict: "SOLVED", ImpToDeliver: 31, Grounded: 9, Substrate: "cc:sonnet"}
	slowB := AnalysisRecord{SolveVerdict: "SOLVED", ImpToDeliver: 58, Grounded: 9, Substrate: "cc:sonnet"}
	rep2 := BuildCompareReport(fastA, slowB)
	if rep2.FasterArm != "A" {
		t.Errorf("A delivered in 31t vs B 58t — A is faster; got FasterArm=%q", rep2.FasterArm)
	}
	if rep2.FasterPct != (58-31)*100/58 {
		t.Errorf("FasterPct must be |delta|/slower; got %d want %d", rep2.FasterPct, (58-31)*100/58)
	}
	// outcome + grounding tied, A faster ⇒ A wins on speed.
	if rep2.Winner != "A" {
		t.Errorf("with outcome+grounding tied and A faster, A must win on speed; Winner=%q", rep2.Winner)
	}
}

// TestCompareTokenDelta — the cost axis: the report reads B's token spend relative to A's. A used 9k,
// B used 14k ⇒ B is +55% over A (the §2 "B +56%" headline, the recorded numbers driving it).
func TestCompareTokenDelta(t *testing.T) {
	a := AnalysisRecord{TokOut: 9000, Substrate: "cc:sonnet"}
	b := AnalysisRecord{TokOut: 14000, Substrate: "cc:sonnet"}
	rep := BuildCompareReport(a, b)
	want := (14000 - 9000) * 100 / 9000
	if rep.TokenDeltaPct != want {
		t.Errorf("TokenDeltaPct must be (B-A)/A as a percent; got %d want %d", rep.TokenDeltaPct, want)
	}
	if rep.TokenDeltaPct <= 0 {
		t.Error("B spent more than A — the token delta must read positive (B costs more)")
	}
}

// TestCompareCrossSubstrateCaveat — the substrate-hygiene rule (CLAUDE.md / §2): a cross-substrate
// pair is an UNSOUND benchmark (it measures the backend, not the mechanism). The report must flag it
// and the headline must carry the caveat so a misread is never silent.
func TestCompareCrossSubstrateCaveat(t *testing.T) {
	a := AnalysisRecord{SolveVerdict: "SOLVED", Substrate: "cc:sonnet", ImpToDeliver: 31, Grounded: 9}
	b := AnalysisRecord{SolveVerdict: "UNSOLVED", Substrate: "test", ImpToDeliver: 0}
	rep := BuildCompareReport(a, b)
	if !rep.CrossSubstrate {
		t.Error("A=cc:sonnet vs B=test is a cross-substrate pair — it must be flagged")
	}
	if !strings.Contains(rep.Headline, "CAVEAT") {
		t.Errorf("the cross-substrate caveat must reach the headline; got %q", rep.Headline)
	}
	// the headline tone is the alarm (red) for an unsound pair, not the calm green of a clean win.
	if line := CompareHeadlineLine(rep); line == "" {
		t.Error("CompareHeadlineLine must render the caveat row")
	}
}

// TestCompareSameSubstrateNotFlagged — the sound benchmark (one knob flipped, substrate held fixed) is
// NOT flagged cross-substrate; the headline carries no caveat.
func TestCompareSameSubstrateNotFlagged(t *testing.T) {
	a := AnalysisRecord{SolveVerdict: "SOLVED", Substrate: "cc:sonnet", ImpToDeliver: 31, Grounded: 9}
	b := AnalysisRecord{SolveVerdict: "UNSOLVED", Substrate: "cc:sonnet", ImpToDeliver: 0}
	rep := BuildCompareReport(a, b)
	if rep.CrossSubstrate {
		t.Error("same-substrate (cc:sonnet vs cc:sonnet) must NOT flag cross-substrate")
	}
	if strings.Contains(rep.Headline, "CAVEAT") {
		t.Errorf("a sound same-substrate pair must carry no caveat; got %q", rep.Headline)
	}
}

// TestCompareTieNoSeparation — two records that read the same on every axis tie; the report says so
// plainly (no fabricated winner).
func TestCompareTieNoSeparation(t *testing.T) {
	r := AnalysisRecord{SolveVerdict: "SOLVED", Substrate: "cc:sonnet", ImpToDeliver: 30, Grounded: 5}
	rep := BuildCompareReport(r, r)
	if rep.Winner != "TIE" {
		t.Errorf("identical records must TIE; Winner=%q", rep.Winner)
	}
	if !strings.Contains(rep.Headline, "no separation") {
		t.Errorf("a tie headline must say there is no separation; got %q", rep.Headline)
	}
}

// TestLoadComparePairNewestFirst — the G2 disk-load path: FindRecentRecords lists *.jsonl event logs
// newest-first and EXCLUDES the *.signals.jsonl sidecars; LoadComparePair loads the two newest into
// A/B. Fewer than two records ⇒ ok=false (the caller keeps the prototype pair). Pure I/O on a temp dir.
func TestLoadComparePairNewestFirst(t *testing.T) {
	dir := t.TempDir()
	// write three event logs + a sidecar; the sidecar must NOT be treated as a record.
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	on := `{"tick":1,"kind":"port","data":{"source":"USER_INPUT","text":"ship it?"}}
{"tick":2,"kind":"critic.decision","data":{"decision":"ACT","reason":"needs ground truth"}}
{"tick":3,"kind":"action.tool","data":{"tool":"run"}}
{"tick":3,"kind":"grounding","data":{"verdict":"grounded","claim":"12/12 pass"}}
{"tick":4,"kind":"critic.decision","data":{"decision":"STOP","stop_kind":"GOAL_MET"}}
{"tick":4,"kind":"action.respond","data":{}}`
	off := `{"tick":1,"kind":"port","data":{"source":"USER_INPUT","text":"ship it?"}}
{"tick":2,"kind":"critic.decision","data":{"decision":"THINK","reason":"keep guessing"}}
{"tick":3,"kind":"critic.decision","data":{"decision":"STOP","stop_kind":"GIVE_UP"}}`

	// write OFF first (oldest), then ON (newest) — newest-first ⇒ ON = A, OFF = B.
	pOff := write("run-off.jsonl", off)
	write("run-off.signals.jsonl", `{"schema":1,"tick":1,"n":0.1}`) // a sidecar — must be ignored
	mustTouchAfter(t, pOff)
	write("run-on.jsonl", on)

	paths := FindRecentRecords(dir)
	if len(paths) != 2 {
		t.Fatalf("two event logs expected (the sidecar excluded); got %d: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "run-on.jsonl") {
		t.Errorf("newest-first: the most recent log (run-on) must be A; got %q", paths[0])
	}
	for _, p := range paths {
		if strings.HasSuffix(p, ".signals.jsonl") {
			t.Errorf("a SignalFrame sidecar leaked into the record list: %q", p)
		}
	}

	a, b, ok := LoadComparePair(dir)
	if !ok {
		t.Fatal("two records present — LoadComparePair must succeed")
	}
	if a.SolveVerdict != "SOLVED" {
		t.Errorf("A (newest, the ON run) must read SOLVED; got %q", a.SolveVerdict)
	}
	if b.SolveVerdict != "UNSOLVED" {
		t.Errorf("B (the OFF run) must read UNSOLVED; got %q", b.SolveVerdict)
	}
	rep := BuildCompareReport(a, b)
	if rep.Winner != "A" {
		t.Errorf("the benchmark over the two loaded records must name the SOLVED ON arm the winner; Winner=%q", rep.Winner)
	}
}

// TestLoadComparePairTooFewRecords — fewer than two records ⇒ ok=false, so the caller falls back to
// the prototype A/B pair (never a half-empty compare).
func TestLoadComparePairTooFewRecords(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/only.jsonl", []byte(`{"tick":1,"kind":"port","data":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := LoadComparePair(dir); ok {
		t.Error("one record is not a benchmark pair — LoadComparePair must report ok=false")
	}
	if _, _, ok := LoadComparePair(dir + "/does-not-exist"); ok {
		t.Error("a missing dir must report ok=false, not panic")
	}
}

// mustTouchAfter bumps a file's mod-time to ensure a later-written file sorts as newer even when the
// filesystem's mod-time granularity is coarse (some filesystems round to the second). It backdates the
// EARLIER file so the ordering is unambiguous for the newest-first assertion.
func mustTouchAfter(t *testing.T, path string) {
	t.Helper()
	// backdate by a minute so a same-second write of the newer file still sorts after it.
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
