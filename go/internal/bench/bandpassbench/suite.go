package bandpassbench

import "github.com/berttrycoding/thought-harness/internal/engine"

// suite.go is the DETERMINISTIC band-pass intake suite. Each case is a tick-stream of raw candidates
// with a per-domain ground-truth role; the same stream is run OFF then ON through the REAL intake.
//
// The cases cover the three behaviours the spec (04-seams.md §2.1 / engine/band_intake.go) intends:
//   (a) a one-tick TRANSIENT spike must be SUPPRESSED on its priming tick (LPF·HPF both low);
//   (b) a PERSISTENT (rising-then-sustained) signal must be PRESERVED (injected after priming, while
//       its HPF is still novel and its LPF has cleared the floor);
//   (c) MIXED: a transient and a persistent stream together — ON suppresses the transient while
//       preserving the persistent;
//   (d) a persistent signal DISPLACED (its anchor decision node left behind by a branch move) must be
//       PRESERVED via a late-injection BUFFER (the retracement arm), not dropped.
//
// Floor reasoning (default 0.05): the band-pass output is min(LPF, HPF). On a stream's FIRST tick both
// EMAs are seeded to x so HPF = x - x = 0 -> Passed = 0 < floor -> suppressed (the priming tick is the
// transient-kill mechanism). A signal that rises on the next tick has HPF = x[t] - LPF_high > 0 and a
// built-up LPF, so it clears the floor -> injected.

// tk is a one-domain tick at relevance rel (most ticks present a single stream).
func tk(domain string, rel float64) engine.BandIntakeTick {
	return engine.BandIntakeTick{Cands: []engine.BandIntakeCand{{Domain: domain, Relevance: rel}}}
}

// tkMulti is a multi-domain tick (the mixed case presents several streams at once).
func tkMulti(cands ...engine.BandIntakeCand) engine.BandIntakeTick {
	return engine.BandIntakeTick{Cands: cands}
}

// cand is a shorthand candidate.
func cand(domain string, rel float64) engine.BandIntakeCand {
	return engine.BandIntakeCand{Domain: domain, Relevance: rel}
}

// DefaultSuite is the standing band-pass mechanism suite.
func DefaultSuite() []Case {
	return []Case{
		// (a) ONE-TICK TRANSIENT: a single high-relevance spike that never recurs. ON must suppress it
		// (priming tick: HPF=0 -> Passed=0 < floor). OFF passes it straight through.
		{
			ID:    "a_transient_spike",
			Desc:  "one-tick high-relevance spike, never recurs",
			Roles: map[string]Role{"flash": RoleTransient},
			Stream: []engine.BandIntakeTick{
				tk("flash", 0.95),
			},
		},

		// (a2) TRANSIENT then SILENCE then a DIFFERENT transient: two unrelated one-tick spikes on two
		// streams. Both are transient noise; ON must suppress both (each is a priming tick on its own
		// stream). OFF passes both.
		{
			ID:    "a2_two_transients",
			Desc:  "two unrelated one-tick spikes on two streams",
			Roles: map[string]Role{"flashX": RoleTransient, "flashY": RoleTransient},
			Stream: []engine.BandIntakeTick{
				tk("flashX", 0.9),
				tk("flashY", 0.85),
			},
		},

		// (b) PERSISTENT RISING signal: a stream that primes low then rises and sustains. ON must preserve
		// it (it clears the floor once novel-and-persistent after priming). OFF passes every tick.
		{
			ID:    "b_persistent_rising",
			Desc:  "rising-then-sustained corroborated signal",
			Roles: map[string]Role{"grounded": RolePersistent},
			Stream: []engine.BandIntakeTick{
				tk("grounded", 0.30), // prime low
				tk("grounded", 0.60), // rise -> novel + persistent -> passes
				tk("grounded", 0.70), // sustain
				tk("grounded", 0.75),
			},
		},

		// (c) MIXED: a transient spike alongside a persistent rising signal, same ticks. ON must suppress
		// the transient while preserving the persistent. OFF passes both.
		{
			ID:   "c_mixed_transient_and_persistent",
			Desc: "a transient spike alongside a persistent rising stream",
			Roles: map[string]Role{
				"noise":  RoleTransient,
				"signal": RolePersistent,
			},
			Stream: []engine.BandIntakeTick{
				tkMulti(cand("signal", 0.30), cand("noise", 0.95)), // both prime; noise only here
				tkMulti(cand("signal", 0.60)),                      // signal rises -> passes; noise gone
				tkMulti(cand("signal", 0.70)),
				tkMulti(cand("signal", 0.72)),
			},
		},

		// (d) DISPLACED PERSISTENT: build a persistent stream on the anchor line, then BRANCH the conscious
		// away (its anchor node is left behind), then present a sustained tick. ON must PRESERVE the signal
		// via a late-injection BUFFER (the retracement arm), not drop it. OFF has no buffer concept and
		// simply passes the tick. The persistent role is satisfied OFF (injected) and ON (buffered).
		{
			ID:    "d_displaced_persistent_buffers",
			Desc:  "persistent stream displaced by a branch move -> buffered late injection",
			Roles: map[string]Role{"latebulb": RolePersistent},
			Stream: []engine.BandIntakeTick{
				tk("latebulb", 0.30), // prime
				tk("latebulb", 0.60), // rise (persistent, novel)
				tk("latebulb", 0.70), // sustain (LPF high, HPF fading)
				tk("latebulb", 0.72),
				// move the conscious to a fresh branch: the latebulb anchor node is now left behind.
				{MoveBranch: true, Cands: []engine.BandIntakeCand{cand("latebulb", 0.72)}}, // displaced + persistent -> BUFFER
			},
		},
	}
}
