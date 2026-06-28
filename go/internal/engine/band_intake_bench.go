package engine

import (
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// band_intake_bench.go is the OFFLINE measurement driver for the seam.band_pass mechanism
// (config Seam.BandPass / Seam.BandPassFloor) — the thin EXPORTED wrapper the bandpassbench
// package drives so it can exercise the REAL intake band-pass (bandPassIntake, band_intake.go)
// from outside the engine package WITHOUT re-implementing the mechanism. It is additive: it
// reads no new state, changes no default path, and is never called by the live loop.
//
// The bench builds a stream of per-tick candidates, feeds each tick through the REAL
// bandPassIntake (with band_pass OFF then ON), and records what the intake kept this tick plus
// the running count of LATE injections it buffered (the displacement/retracement arm). The
// oracle then scores the OFF/ON delta on a precision-style suppress/preserve contrast.

// BandIntakeTick is one tick of a band-pass probe stream: the candidates presented at this tick
// (each with its per-stream relevance sample) plus an optional MoveBranch flag that forks the
// conscious onto a fresh branch BEFORE this tick's intake — the way a real displacement happens
// (the anchor decision node is left behind, so a persisted signal must buffer as a late injection
// rather than inject at the head). Domain keys the per-stream filter; reuse a domain across ticks
// to feed the SAME signal over time.
type BandIntakeTick struct {
	MoveBranch bool             // fork the conscious onto a new branch before this tick's intake
	Cands      []BandIntakeCand // the raw candidates presented at this tick
}

// BandIntakeCand is one raw candidate at a tick: its per-stream domain and its relevance sample
// (the band-pass input x[t], in [0,1]). Text is derived from the domain so a buffered late
// injection is identifiable.
type BandIntakeCand struct {
	Domain    string
	Relevance float64
}

// BandIntakeTickResult is the REAL intake's verdict for one probe tick: which candidate domains
// the intake KEPT (cleared the floor and injected NOW) and the cumulative number of LATE
// injections buffered so far (the displacement arm). Deterministic — no clock, no RNG.
type BandIntakeTickResult struct {
	Kept            []string // the domains the intake injected this tick
	BufferedLateTot int      // cumulative late injections buffered up to and including this tick
}

// BandPassIntakeProbe runs a probe stream through the REAL intake band-pass and returns the
// per-tick verdicts. on selects band_pass OFF (the byte-identical default — every candidate is
// returned unchanged) vs ON (the LPF·HPF filter + floor gate + displacement buffering). floor is
// the inject floor (Seam.BandPassFloor; the live default is 0.05). It is a fresh engine per call
// on the offline test double — no model, no network, fully deterministic.
//
// This drives the actual e.bandPassIntake method (band_intake.go) and the actual
// e.BufferLateInjection / e.pendingInj buffer, so a mutation that bypasses the filter, the floor,
// or the displacement arm is observable in the result — the bench is mutation-sensitive against
// the real mechanism, not a mock of it.
func BandPassIntakeProbe(stream []BandIntakeTick, on bool, floor float64) ([]BandIntakeTickResult, error) {
	return bandPassIntakeProbe(stream, on, floor, false)
}

// BandPassIntakeProbeColdStart is the B1f variant: it drives the SAME real intake band-pass with the
// COLD-START fix engaged (seam.band_pass_coldstart ON, requires seam.band_pass ON). It lets the bench
// prove the fix end-to-end through the REAL engine path — a first-appearance HIGH+SUSTAINED step-edge
// now INJECTS at the step the legacy seed-to-x[0] cold-start suppressed forever. floor is the inject
// floor; on selects band_pass OFF (byte-identical) vs ON+coldstart.
func BandPassIntakeProbeColdStart(stream []BandIntakeTick, on bool, floor float64) ([]BandIntakeTickResult, error) {
	return bandPassIntakeProbe(stream, on, floor, on)
}

// bandPassIntakeProbe is the shared driver: it runs the stream through the REAL e.bandPassIntake with
// the band_pass + (optional) cold-start knobs set, on the offline test double — deterministic, no model.
func bandPassIntakeProbe(stream []BandIntakeTick, on bool, floor float64, coldStart bool) ([]BandIntakeTickResult, error) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.New() // AllOn baseline
	feat.Seam.BandPass = on
	feat.Seam.BandPassFloor = floor
	feat.Seam.BandPassColdStart = coldStart
	cfg.Features = feat

	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		return nil, err
	}
	e.startEpisode("band-pass intake probe", true)

	out := make([]BandIntakeTickResult, 0, len(stream))
	for tick, st := range stream {
		if st.MoveBranch {
			// Fork the conscious onto a fresh branch and focus it: the anchor decision node of every
			// already-seen stream is now LEFT BEHIND (displaced), exactly as a real BRANCH/BACKTRACK move
			// displaces a signal's anchor. The next intake of a displaced-but-persistent stream must buffer
			// a late injection rather than inject at the head.
			nb := e.mcp.Branch("probe-alt", nil)
			e.mcp.Focus(nb)
		}
		raw := make([]*types.Candidate, 0, len(st.Cands))
		for _, c := range st.Cands {
			d := c.Domain
			raw = append(raw, &types.Candidate{
				Text:      "cand:" + c.Domain,
				Source:    types.INJECTED,
				Domain:    &d,
				Relevance: c.Relevance,
			})
		}
		kept := e.bandPassIntake(raw, tick)
		keptDomains := make([]string, 0, len(kept))
		for _, c := range kept {
			if c.Domain != nil {
				keptDomains = append(keptDomains, *c.Domain)
			}
		}
		out = append(out, BandIntakeTickResult{
			Kept:            keptDomains,
			BufferedLateTot: e.pendingInj.Len(),
		})
	}
	return out, nil
}
