package engine

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/seams"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// band_intake.go wires the hidden-seam intake BAND-PASS (slice c, 04-seams.md §2.1) into the live relay:
// each raw candidate is passed through a per-STREAM band-pass (keyed by domain/source) over ticks. A
// candidate must be persistent ENOUGH (LPF high — not a flash-in-the-pan) AND novel ENOUGH (HPF high —
// not a stale restatement) to be injected NOW. A candidate that is persistent (corroborated) but whose
// anchor decision node has been LEFT BEHIND is buffered as a LATE injection (→ retracement, §2b / #20).
//
// Opt-in (seam.band_pass, default OFF): the filter drops first-appearance transients (a one-tick spike
// scores Passed≈0 on its priming tick), which would change goldens — so default-off keeps the intake
// byte-identical. The two cutoffs are the dev-tuned "skin"; the inject floor is seam.band_pass_floor.
//
// COLD-START FIX (seam.band_pass_coldstart, B1f, default OFF): the legacy cold-start seeds the HPF
// novelty reference to x[0] on first appearance ⇒ HPF=0 ⇒ a first-appearance-HIGH SUSTAINED signal (a
// novel grounded step-edge) is suppressed FOREVER. With this opt-in the filter cold-starts from 0 and
// suppresses only the priming tick, so a sustained first appearance injects at the step (emitting
// seam.band_coldstart) and then fades to DC. Requires seam.band_pass; OFF ⇒ the legacy path ⇒
// byte-identical.

// bandStream is one signal's band-pass state: the filter plus the active branch when it first appeared
// (its anchor decision node), whether it has already been buffered as a late injection, and how many
// ticks of this stream have been observed (so the cold-start fix can name the FIRST post-priming
// injection as the step-edge it just rescued).
type bandStream struct {
	filter       *seams.BandPass
	anchorBranch int
	buffered     bool
	ticks        int // count of Step()s seen on this stream (1 == the priming tick)
}

// bandPassIntake filters the raw candidates through the intake band-pass when seam.band_pass is on,
// returning only the candidates that clear the inject floor this tick. A persistent-but-displaced
// candidate (its anchor branch is no longer active) is buffered for retracement instead of dropped on
// the floor. seam.band_pass OFF (default) returns raw unchanged — byte-identical.
func (e *Engine) bandPassIntake(raw []*types.Candidate, tick int) []*types.Candidate {
	if e.features == nil || !e.features.Seam.BandPass {
		return raw
	}
	if e.bandStreams == nil {
		e.bandStreams = map[string]*bandStream{}
	}
	floor := e.features.Seam.BandPassFloor
	coldStart := e.features.Seam.BandPassColdStart
	active := e.graph.ActiveBranch
	kept := make([]*types.Candidate, 0, len(raw))
	for _, c := range raw {
		key := candidateStreamKey(c)
		st := e.bandStreams[key]
		if st == nil {
			cfg := seams.DefaultBandPassConfig()
			cfg.ColdStartZeroRef = coldStart
			st = &bandStream{filter: seams.NewBandPass(cfg), anchorBranch: active}
			e.bandStreams[key] = st
		}
		res := st.filter.Step(c.Relevance)
		st.ticks++
		if active != st.anchorBranch {
			// DISPLACED: the conscious has left the decision node where this signal was born. Head-novelty
			// (HPF) is the wrong measure now — the signal pertains to a PASSED node. If it has PERSISTED
			// (LPF cleared the floor — a real, corroborated signal, the "light-bulb after the calc came
			// back"), buffer it as a late injection anchored to that node (§2b / #20, once); else drop.
			if res.LowPass >= floor && !st.buffered {
				e.BufferLateInjection(c.Text, st.anchorBranch, tick)
				st.buffered = true
			}
			continue
		}
		// On the anchor line: the band-pass gates injection at the head — persistent (LPF high) AND novel
		// (HPF high). A transient (LPF low) or a stale restatement (HPF low) is suppressed.
		if res.Passed >= floor {
			// COLD-START WITNESS (B1f): when the fix is on and this is the FIRST injection a fresh stream
			// earns right after its one-tick warm-up (tick 2), it is the first-appearance step-edge the
			// legacy seed-to-x[0] would have killed (HPF=0). Make that rescue observable — it is the whole
			// point of the knob, and what a test asserts to prove the wire fires.
			if coldStart && st.ticks == 2 && e.bus != nil {
				e.bus.Emit(events.BandColdStart,
					"first-appearance step-edge injected (cold-start fix): "+runeSlice(c.Text, 48),
					events.D{
						"stream":   key,
						"passed":   round2(res.Passed),
						"low_pass": round2(res.LowPass),
						"highpass": round2(res.HighPass),
						"floor":    floor,
						"tick":     tick,
					})
			}
			kept = append(kept, c)
			st.buffered = false // re-armed: a fresh novel surge can buffer again after a later displacement
		}
	}
	return kept
}

// candidateStreamKey identifies which signal stream a candidate belongs to across ticks: its specialist
// domain when set, else its source class. The band-pass is per-stream, so corroboration/novelty are
// measured against the SAME signal over time, not the whole intake.
func candidateStreamKey(c *types.Candidate) string {
	if c.Domain != nil && *c.Domain != "" {
		return "dom:" + *c.Domain
	}
	return "src:" + c.Source.String()
}
