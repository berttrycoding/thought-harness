package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

func bandCand(domain string, rel float64) *types.Candidate {
	d := domain
	return &types.Candidate{Text: "cand:" + domain, Source: types.INJECTED, Domain: &d, Relevance: rel}
}

// TestBandPassIntake pins slice (c) / 04 §2.1: with seam.band_pass OFF the intake is untouched; ON, a
// flash-in-the-pan (first-appearance spike) is suppressed, a RISING (persistent-and-novel) signal passes,
// and a persistent-but-displaced signal (its anchor decision node left behind) is buffered as a late
// injection instead of dropped.
func TestBandPassIntake(t *testing.T) {
	mk := func(on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Seam.BandPass = on
		feat.Seam.BandPassFloor = 0.05
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		e.startEpisode("solve x", true)
		return e
	}

	// --- OFF: the intake is returned unchanged (byte-identical) ---
	eOff := mk(false)
	raw := []*types.Candidate{bandCand("alpha", 0.9)}
	if got := eOff.bandPassIntake(raw, 0); len(got) != 1 {
		t.Errorf("band-pass OFF: intake changed (%d candidates, want 1)", len(got))
	}

	// --- ON: a first-appearance spike is suppressed (Passed≈0 on the priming tick) ---
	e := mk(true)
	if got := e.bandPassIntake([]*types.Candidate{bandCand("alpha", 0.9)}, 0); len(got) != 0 {
		t.Errorf("band-pass ON: a first-appearance transient must be suppressed, got %d kept", len(got))
	}
	// a RISING signal on a fresh stream becomes persistent-and-novel and passes after priming.
	e2 := mk(true)
	e2.bandPassIntake([]*types.Candidate{bandCand("beta", 0.3)}, 0) // prime low
	got := e2.bandPassIntake([]*types.Candidate{bandCand("beta", 0.6)}, 1)
	if len(got) == 0 {
		t.Error("band-pass ON: a rising (persistent+novel) signal must pass after priming")
	}

	// --- ON: a persistent-but-displaced signal buffers a late injection (→ retracement) ---
	e3 := mk(true)
	// build the gamma stream up on the anchor branch (rising then steady -> LPF high, HPF fading).
	for tick, rel := range []float64{0.3, 0.6, 0.7, 0.7} {
		e3.bandPassIntake([]*types.Candidate{bandCand("gamma", rel)}, tick)
	}
	// move the conscious to a NEW branch — the gamma anchor node is now left behind.
	nb := e3.mcp.Branch("alt", nil)
	e3.mcp.Focus(nb)
	before := e3.pendingInj.Len()
	// a steady gamma tick now: HPF has faded (not novel) but LPF is high (persistent) and the anchor is
	// passed -> buffered as a late injection rather than injected or dropped.
	e3.bandPassIntake([]*types.Candidate{bandCand("gamma", 0.7)}, 5)
	if e3.pendingInj.Len() <= before {
		t.Errorf("band-pass ON: a persistent displaced signal must buffer a late injection (len %d -> %d)", before, e3.pendingInj.Len())
	}
}

// TestBandPassColdStartFix is the B1f cognition-property test on the LIVE intake path (e.bandPassIntake,
// the same method reactive.go calls at the head of the relay). It asserts the THINKING the spec intends
// (04-seams §2.1): a novel first-appearance grounded FACT — a signal that appears HIGH and SUSTAINS high,
// information the conscious has never seen — must be SURFACED to the stream, not silently killed. It also
// proves the wire FIRES (the seam.band_coldstart event) and that the knob is a TRUE opt-in (OFF ⇒ the
// legacy suppress-forever behaviour AND zero events ⇒ byte-identical).
//
//   - knob OFF (default): a first-appearance-high sustained signal is suppressed on EVERY tick (the legacy
//     cold-start seeds the HPF reference to x[0], HPF=0) — and NO seam.band_coldstart event is emitted.
//   - knob ON: the priming tick is still suppressed (one-tick warm-up — a flash never injects), but from
//     the next tick the step-edge clears the floor and is INJECTED, emitting seam.band_coldstart. The
//     conscious now sees the novel fact instead of never hearing it.
func TestBandPassColdStartFix(t *testing.T) {
	mk := func(coldStart bool) (*Engine, *[]events.Event) {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Seam.BandPass = true
		feat.Seam.BandPassFloor = 0.05
		feat.Seam.BandPassColdStart = coldStart
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		log := &[]events.Event{}
		e.Bus().Subscribe(func(ev events.Event) { *log = append(*log, ev) })
		e.startEpisode("surface a novel grounded fact", true)
		return e, log
	}

	countColdStart := func(log *[]events.Event) int {
		n := 0
		for _, ev := range *log {
			if ev.Kind == string(events.BandColdStart) {
				n++
			}
		}
		return n
	}

	// A first-appearance HIGH signal that SUSTAINS over several ticks (the novel grounded fact).
	feed := func(e *Engine) (primingKept, totalKept int) {
		for tick := 0; tick < 6; tick++ {
			kept := e.bandPassIntake([]*types.Candidate{bandCand("novelfact", 0.95)}, tick)
			if tick == 0 {
				primingKept = len(kept)
			}
			totalKept += len(kept)
		}
		return
	}

	// --- knob OFF (default cold-start): the divergence persists; the fact is killed forever; no event ---
	eOff, logOff := mk(false)
	_, offTotal := feed(eOff)
	if offTotal != 0 {
		t.Errorf("knob OFF (legacy cold-start): a first-appearance-high sustained signal must be suppressed on every tick; got %d kept", offTotal)
	}
	if n := countColdStart(logOff); n != 0 {
		t.Errorf("knob OFF: no seam.band_coldstart event may be emitted (byte-identical); got %d", n)
	}

	// --- knob ON: the priming tick is still suppressed, then the step-edge injects + emits the witness ---
	eOn, logOn := mk(true)
	onPriming, onTotal := feed(eOn)
	if onPriming != 0 {
		t.Errorf("knob ON: the priming tick must still be suppressed (one-tick warm-up — a flash never injects); got %d kept at tick 0", onPriming)
	}
	if onTotal == 0 {
		t.Error("knob ON (cold-start FIX): a SUSTAINED first-appearance grounded fact must be SURFACED to the conscious (injected at the step), not silently killed; got 0 kept")
	}
	if n := countColdStart(logOn); n == 0 {
		t.Error("knob ON: the cold-start rescue must FIRE the seam.band_coldstart witness on the live intake path (the wire is dead if it never emits)")
	}
}
