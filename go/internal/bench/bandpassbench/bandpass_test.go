package bandpassbench

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/engine"
)

// TestBandPassBench_Signal is the mutation-sensitive behavioural test: it runs the standing suite
// through the REAL intake band-pass and asserts the mechanism's intended behaviour — ON suppresses the
// transient noise that OFF lets through, ON preserves every persistent signal, and every case is
// exactly correct. If the band-pass were bypassed (OFF==ON), this FAILS, because the whole signal is
// the OFF/ON contrast.
func TestBandPassBench_Signal(t *testing.T) {
	r, err := Run(DefaultSuite(), 0.05)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// OFF must let transients through (no suppression) — the band-pass is the thing that suppresses.
	if r.OffSuppressed != 0 {
		t.Errorf("band_pass OFF should suppress NO transient (it passes the intake unchanged); got %d/%d suppressed",
			r.OffSuppressed, r.TotalTransient)
	}
	// ON must suppress EVERY transient — the priming-tick HPF=0 kill is the mechanism.
	if r.OnSuppressed != r.TotalTransient {
		t.Errorf("band_pass ON should suppress EVERY transient; got %d/%d", r.OnSuppressed, r.TotalTransient)
	}
	// ON must not LOSE any persistent signal (inject-now OR buffer-late both count as preserved).
	if r.OnPreserved != r.TotalPersistent {
		t.Errorf("band_pass ON should preserve EVERY persistent signal; got %d/%d", r.OnPreserved, r.TotalPersistent)
	}
	// Every case must behave exactly as the spec intends.
	if r.CorrectCases != len(r.Cases) {
		t.Errorf("every case should be exactly correct under ON; got %d/%d", r.CorrectCases, len(r.Cases))
	}
	// The aggregate verdict must read SIGNAL.
	signal, line := r.Verdict()
	if !signal {
		t.Errorf("expected SIGNAL verdict; got: %s", line)
	}
}

// TestBandPassBench_DisplacedBuffers pins the retracement arm specifically: a persistent stream
// displaced by a branch move must be PRESERVED via a late-injection buffer under ON (not dropped),
// and OFF must NOT use the buffer (it has no displacement concept). This proves the displacement arm of
// the real mechanism is exercised, not just the floor gate.
func TestBandPassBench_DisplacedBuffers(t *testing.T) {
	var disp Case
	found := false
	for _, c := range DefaultSuite() {
		if c.ID == "d_displaced_persistent_buffers" {
			disp = c
			found = true
		}
	}
	if !found {
		t.Fatal("displaced-persistent case missing from the suite")
	}

	on, err := runArm(disp, true, 0.05)
	if err != nil {
		t.Fatalf("runArm ON: %v", err)
	}
	if !on.bufferedLate["latebulb"] {
		t.Error("ON: a displaced persistent signal must buffer a late injection (retracement arm not exercised)")
	}
	if on.finalBuffered == 0 {
		t.Error("ON: the late-injection buffer should hold the displaced persistent signal")
	}

	off, err := runArm(disp, false, 0.05)
	if err != nil {
		t.Fatalf("runArm OFF: %v", err)
	}
	if off.finalBuffered != 0 {
		t.Errorf("OFF: the band-pass is off, so NO late injection should buffer; got %d", off.finalBuffered)
	}
}

// TestBandPassBench_FirstTickTransientSuppressed pins the precise priming-tick mechanism through the
// raw probe driver: a one-tick spike on a fresh stream scores Passed=0 (HPF = x - x = 0) under ON and
// is NOT kept, while OFF keeps it. This is the floor of the whole bench.
func TestBandPassBench_FirstTickTransientSuppressed(t *testing.T) {
	stream := []engine.BandIntakeTick{
		{Cands: []engine.BandIntakeCand{{Domain: "flash", Relevance: 0.95}}},
	}
	off, err := engine.BandPassIntakeProbe(stream, false, 0.05)
	if err != nil {
		t.Fatalf("probe OFF: %v", err)
	}
	if len(off[0].Kept) != 1 {
		t.Errorf("OFF: a transient must pass the intake unchanged; kept %v", off[0].Kept)
	}
	on, err := engine.BandPassIntakeProbe(stream, true, 0.05)
	if err != nil {
		t.Fatalf("probe ON: %v", err)
	}
	if len(on[0].Kept) != 0 {
		t.Errorf("ON: a first-appearance transient must be suppressed; kept %v", on[0].Kept)
	}
}

// firstAppearanceHighStream is a constant-HIGH stream that appears at 0.95 on tick 0 and SUSTAINS — the
// first-appearance step-edge at the heart of B1f (a novel grounded fact the conscious has never seen).
func firstAppearanceHighStream() []engine.BandIntakeTick {
	return []engine.BandIntakeTick{
		{Cands: []engine.BandIntakeCand{{Domain: "novelfact", Relevance: 0.95}}},
		{Cands: []engine.BandIntakeCand{{Domain: "novelfact", Relevance: 0.95}}},
		{Cands: []engine.BandIntakeCand{{Domain: "novelfact", Relevance: 0.95}}},
		{Cands: []engine.BandIntakeCand{{Domain: "novelfact", Relevance: 0.95}}},
	}
}

// TestBandPassBench_FirstAppearanceHighSuppressed_LegacyColdStart pins the LEGACY cold-start (the B1f fix
// OFF — the byte-identical default): the spec-vs-impl divergence the B1 oracle vet surfaced STILL holds
// when seam.band_pass_coldstart is off. It is deliberately NOT part of DefaultSuite / the SIGNAL aggregate
// (the suite's persistent cases all prime LOW then rise) — its job is to pin the boundary so the DEFAULT
// path cannot silently regress.
//
// THE LEGACY BEHAVIOUR: a signal that appears HIGH on its FIRST tick and SUSTAINS high is suppressed
// FOREVER under band_pass ON. The legacy filter seeds both EMAs to x[0] on the priming tick, so
// HPF = x - x = 0; a constant-high stream never rises, so Passed = min(LPF, 0) = 0 < floor on every tick
// -> never injected NOW, never buffered. (The B1f fix repairs exactly this — see the next test.)
func TestBandPassBench_FirstAppearanceHighSuppressed_LegacyColdStart(t *testing.T) {
	stream := firstAppearanceHighStream()
	// OFF passes a first-appearance-high signal straight through (it is kept on every tick).
	off, err := engine.BandPassIntakeProbe(stream, false, 0.05)
	if err != nil {
		t.Fatalf("probe OFF: %v", err)
	}
	offKept := 0
	for _, tr := range off {
		offKept += len(tr.Kept)
	}
	if offKept == 0 {
		t.Error("OFF: a first-appearance-high signal must pass the intake unchanged (band_pass is off)")
	}
	// ON with the LEGACY cold-start suppresses it on EVERY tick (the documented divergence).
	on, err := engine.BandPassIntakeProbe(stream, true, 0.05)
	if err != nil {
		t.Fatalf("probe ON: %v", err)
	}
	onKept := 0
	finalBuffered := 0
	for _, tr := range on {
		onKept += len(tr.Kept)
		finalBuffered = tr.BufferedLateTot
	}
	if onKept != 0 {
		t.Errorf("ON (LEGACY cold-start, coldstart-fix OFF): a first-appearance-high SUSTAINED signal is suppressed on every tick; got %d kept", onKept)
	}
	if finalBuffered != 0 {
		t.Errorf("ON (legacy): a constant-high first-appearance signal is not displaced, so it must not be buffered either; got %d buffered", finalBuffered)
	}
}

// TestBandPassBench_FirstAppearanceHighInjected_ColdStartFix pins the B1f FIX through the SAME real
// intake path (BandPassIntakeProbeColdStart): with seam.band_pass_coldstart ON, the first-appearance
// HIGH+SUSTAINED step-edge the legacy cold-start suppressed forever is now INJECTED at the step — the
// spec's HPF intent (04-seams §2.1: "inject only what ADDS information"). The priming tick is still
// suppressed (the one-tick warm-up keeps a flash from injecting), so it injects from tick 2 on, then the
// reference catches up and it fades to DC. This is the spec-divergence repaired, asserted on the engine.
func TestBandPassBench_FirstAppearanceHighInjected_ColdStartFix(t *testing.T) {
	stream := firstAppearanceHighStream()
	on, err := engine.BandPassIntakeProbeColdStart(stream, true, 0.05)
	if err != nil {
		t.Fatalf("probe ON (cold-start fix): %v", err)
	}
	if len(on) == 0 {
		t.Fatal("cold-start probe returned no ticks")
	}
	// The priming tick (tick 0) is the one-tick warm-up: still suppressed.
	if len(on[0].Kept) != 0 {
		t.Errorf("cold-start FIX: the priming tick must still be suppressed (one-tick warm-up); got %v kept", on[0].Kept)
	}
	// From tick 1 on, the SUSTAINED first-appearance step-edge clears the floor and is injected at least
	// once — the rescue the legacy cold-start could never make.
	postPriming := 0
	for _, tr := range on[1:] {
		postPriming += len(tr.Kept)
	}
	if postPriming == 0 {
		t.Errorf("cold-start FIX: a SUSTAINED first appearance must INJECT at the step after priming; got 0 kept post-priming (the divergence is NOT repaired)")
	}
}
