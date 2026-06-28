package engine_test

// analysis_compare_property_test.go — the WIRING + COGNITION gate for the G2 COMPARE benchmark (Track
// G). The wiring-gate lesson (saved): a unit that exists but never runs on the engine's actual tick is
// dead. The pure compare_test.go proves the CompareReport distills a benchmark off RECORDED fixtures;
// THIS test proves the distillation reads a benchmark off TWO REAL engine runs captured exactly as the
// TUI freeze tap captures them (EngineBridge.captureFreeze → cognition.RecordFromFrozen), then a
// CompareReport over the two reconstructed records. If the path were not reading the real bus, the
// reports would be hollow and the benchmark would be a fabricated constant — this catches that.
//
// The "thinking" asserted is the §7 definition of done: load two real recorded runs and READ the
// verdict / latency-delta / grounded-delta / divergence-tick — faithfully off the recordings, never
// re-judged and never invented. The two arms are DIFFERENT real episodes (a grounding-shaped verify
// turn vs a plain turn) so the benchmark has real, divergent material to read.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// recordRealRun drives a real reactive episode and reconstructs the AnalysisRecord the way the freeze
// tap does — off the live bus. This is the wired path: the events come from the engine's actual tick.
func recordRealRun(t *testing.T, seed int, prompt string, steps int) cognition.AnalysisRecord {
	t.Helper()
	eng, log := newSeededEngine(t, "reactive", seed)
	eng.SubmitDefault(prompt)
	for i := 0; i < steps; i++ {
		eng.Step()
	}
	rec := cognition.RecordFromFrozen(log.events, nil)
	rec.Substrate = eng.SubstrateClass()
	return rec
}

// TestCompareBenchmarkReadsTwoRealRuns — the G2 user goal proven on the live loop: a CompareReport
// built over TWO real recorded episodes reads each arm's recorded benchmark fields faithfully and
// computes the deltas off them (not a fabricated verdict). It must reconstruct a real decision spine
// for each arm, read each verdict from the trajectory, and the report's per-arm fields must MATCH the
// records they were built from (the distillation does not invent numbers).
func TestCompareBenchmarkReadsTwoRealRuns(t *testing.T) {
	a := recordRealRun(t, 11, "what is 17 * 23? verify it against a computation.", 16)
	b := recordRealRun(t, 7, "is the 3-line refactor in cache.go safe to ship?", 14)

	// the wiring gate: both arms reconstructed a real decision spine off the live bus.
	if len(a.Decisions) == 0 || len(b.Decisions) == 0 {
		t.Fatalf("a benchmark arm reconstructed ZERO decisions off the live bus — the path is not reading the real stream (A=%d B=%d)", len(a.Decisions), len(b.Decisions))
	}

	rep := cognition.BuildCompareReport(a, b)

	// the report reads each arm's RECORDED verdict, not a re-judgement.
	if rep.AVerdict != a.SolveVerdict || rep.BVerdict != b.SolveVerdict {
		t.Errorf("the report must carry each arm's recorded verdict: rep A=%q (rec %q), rep B=%q (rec %q)",
			rep.AVerdict, a.SolveVerdict, rep.BVerdict, b.SolveVerdict)
	}
	// the per-arm benchmark fields are READ off the records, never invented.
	if rep.AGrounded != a.Grounded || rep.BGrounded != b.Grounded {
		t.Errorf("grounded counts must mirror the records: rep A=%d (rec %d), rep B=%d (rec %d)",
			rep.AGrounded, a.Grounded, rep.BGrounded, b.Grounded)
	}
	if rep.GroundedDelta != a.Grounded-b.Grounded {
		t.Errorf("GroundedDelta must be A-B over the real records; got %d (A=%d B=%d)", rep.GroundedDelta, a.Grounded, b.Grounded)
	}
	if rep.ALatency != a.ImpToDeliver || rep.BLatency != b.ImpToDeliver {
		t.Errorf("latency must mirror each record's impulse-to-deliver: rep A=%d (rec %d), rep B=%d (rec %d)",
			rep.ALatency, a.ImpToDeliver, rep.BLatency, b.ImpToDeliver)
	}
	// the winner is a real verdict over the two arms (A, B, or TIE) — never empty/garbage.
	switch rep.Winner {
	case "A", "B", "TIE":
	default:
		t.Errorf("the benchmark winner must be A/B/TIE; got %q", rep.Winner)
	}
	// both arms ran on the same (test) substrate ⇒ a SOUND benchmark, not cross-substrate.
	if rep.CrossSubstrate {
		t.Errorf("both arms ran on the same substrate (%q vs %q) — the benchmark must NOT flag cross-substrate", a.Substrate, b.Substrate)
	}
	if rep.Headline == "" {
		t.Error("the benchmark over two real runs produced no headline verdict")
	}
}

// TestCompareBenchmarkDivergesOnRealForkOrGroundedReads — the benchmark's discriminating power on real
// runs: two DIFFERENT real episodes must produce SOME readable difference the surface can score on —
// either a move-level fork (a divergence tick) OR a difference in what reality each imported (the
// grounded ledger). A benchmark that read two distinct runs as identical on every axis would be blind.
func TestCompareBenchmarkDivergesOnRealForkOrGroundedReads(t *testing.T) {
	a := recordRealRun(t, 11, "what is 17 * 23? verify it against a computation.", 16)
	b := recordRealRun(t, 7, "is the 3-line refactor in cache.go safe to ship?", 14)
	rep := cognition.BuildCompareReport(a, b)

	forked := rep.DivergenceTick >= 0
	groundedDiff := rep.GroundedDelta != 0
	latencyDiff := rep.LatencyDeltaTicks != 0
	verdictDiff := rep.AVerdict != rep.BVerdict
	if !forked && !groundedDiff && !latencyDiff && !verdictDiff {
		t.Errorf("two distinct real episodes read as identical on every benchmark axis — the surface is blind (divergeTick=%d groundedΔ=%d latencyΔ=%d verdicts A=%q B=%q)",
			rep.DivergenceTick, rep.GroundedDelta, rep.LatencyDeltaTicks, rep.AVerdict, rep.BVerdict)
	}
	if forked && rep.DivergenceWhy == "" {
		t.Error("a reported divergence tick must carry a non-empty WHY (the §7 'where + why')")
	}
}

// TestCompareLoadFlagOffByteIdentical — the additive/default-OFF guarantee for the WIRING: the
// tui.compare_load knob default-OFF must not change the engine's emitted event stream (the COMPARE
// load is a pure View-layer disk read, never an engine concern). Two engines on the same seed — the
// default config — emit the same event kinds in the same order; flipping the knob ON in config does
// not alter the engine tick (the knob is read only by the TUI on a keypress, never by the engine).
func TestCompareLoadFlagOffByteIdentical(t *testing.T) {
	run := func(compareLoad bool) []events.Event {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Features = config.New()
		cfg.Features.Tui.CompareLoad = compareLoad
		e, err := engine.NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		var captured []events.Event
		e.Bus().Subscribe(func(ev events.Event) { captured = append(captured, ev) })
		e.SubmitDefault("is the refactor safe to ship?")
		for i := 0; i < 10; i++ {
			e.Step()
		}
		return captured
	}
	off := run(false)
	on := run(true)
	if len(off) != len(on) {
		t.Fatalf("flipping tui.compare_load changed the engine event count: off=%d on=%d (the knob is View-only, the tick must be byte-identical)", len(off), len(on))
	}
	for i := range off {
		if off[i].Kind != on[i].Kind || off[i].Tick != on[i].Tick {
			t.Fatalf("event %d diverged when tui.compare_load flipped: off=%s@%d on=%s@%d",
				i, off[i].Kind, off[i].Tick, on[i].Kind, on[i].Tick)
		}
	}
}
