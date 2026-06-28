package engine_test

import (
	"sort"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// safeDomains is the seam-#2 parallel-safe BASE-specialist set: the only fired domains that are a
// DETERMINISTIC function of the prompt+theta on the live substrate (a keyword-trigger relevance gate, no
// model call in the firing decision). The workflow SUB-AGENT domains are model-SYNTHESISED (SynthesizeProgram
// picks the step domains) and so vary run-to-run with claude's uncontrollable temperature — they are NOT a
// valid parity signal for the per-tick base-specialist seam. We compare ONLY this deterministic base set.
var safeDomains = map[string]bool{"social": true, "skeptic": true, "advocate": true}

// TestLiveClaudePrimitiveSubAgentFanoutParity is the live-substrate parity + wall-clock gate for SEAM #2 (the
// per-tick base-specialist model-call fan-out, 07-OPTIMISATION-SURVEY.md §A.1 item 3). It runs the SAME
// review-shaped workload (a refactor-safety question that fires BOTH model-call stance roles, skeptic +
// advocate, on the same tick) on the LIVE claude bridge with the concurrency flag OFF, OFF again (the
// substrate-noise CONTROL), then ON, and:
//
//   - PARITY (the load-bearing assertion): the BASE parallel-safe specialist set (social/skeptic/advocate —
//     deterministic on claude, a keyword-trigger gate with no model call in the firing decision) MUST be
//     identical OFF vs ON. The seam changes timing, never WHICH base faculties fire. The two OFF runs are the
//     CONTROL: workflow sub-agent domains (model-synthesised) and exact outputs vary with claude temperature
//     run-to-run REGARDLESS of the flag, so byte-identical OUTPUT is NOT assertable on claude — that is why
//     the parity is restricted to the deterministic base set, and why we show OFF-vs-OFF diverges the same
//     way OFF-vs-ON does on the model-synthesised parts (proving any such divergence is substrate noise, not
//     the seam).
//   - WALL-CLOCK: reports OFF vs ON episode wall-clock + the speedup %. The win is the overlap of the
//     skeptic+advocate model calls on the multi-specialist tick; on claude it is MODEST/NOISY (per-call
//     latency dominates a short episode, the fan-out is only 2 wide, and the bridge's per-call latency
//     variance is large) — the offline TestPrimitiveSubAgentFanoutOverlapsModelCalls is the clean, deterministic
//     ~50% proof; this run confirms the seam does not REGRESS wall-clock and that parity holds on the
//     frontier substrate. Honest: a 2-wide fan-out inside a single tick is a small slice of a 6-tick episode,
//     so the episode-level claude number is within the substrate's wall-clock noise (do not over-read it).
//
// Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes). Run:
//
//	THOUGHT_LIVE_CLAUDE=1 go test ./internal/engine -run TestLiveClaudePrimitiveSubAgentFanoutParity -v -timeout 900s
func TestLiveClaudePrimitiveSubAgentFanoutParity(t *testing.T) {
	const prompt = "Is this refactor safe to ship, weighing both sides?"
	const ticks = 6

	runOnce := func(parallel bool) (baseFired []string, responses, llmCalls int, elapsed time.Duration) {
		restore := subconscious.SetParallelPhasesForTest(parallel)
		defer restore()

		eng, log := newLiveEngine(t, "reactive", 7) // skips unless THOUGHT_LIVE_CLAUDE=1
		eng.SubmitDefault(prompt)

		start := time.Now()
		for i := 0; i < ticks; i++ {
			eng.Step()
		}
		elapsed = time.Since(start)

		// Collect ONLY the deterministic base parallel-safe domains (the seam-#2 set); the model-synthesised
		// workflow sub-agent domains are substrate-nondeterministic and not a valid parity signal.
		seen := map[string]bool{}
		for _, ev := range log.of(events.SubFire) {
			if d, ok := ev.Data["domain"].(string); ok && safeDomains[d] {
				seen[d] = true
			}
		}
		for d := range seen {
			baseFired = append(baseFired, d)
		}
		sort.Strings(baseFired)
		responses = len(respondsOf(log))
		llmCalls = len(log.of(events.LLM))
		return
	}

	off1Fired, _, off1Calls, off1Time := runOnce(false)
	off2Fired, _, off2Calls, off2Time := runOnce(false) // substrate-noise CONTROL (same flag state)
	onFired, _, onCalls, onTime := runOnce(true)

	t.Logf("seam #2 LIVE claude parity (%q, %d ticks) — BASE parallel-safe domains only:", prompt, ticks)
	t.Logf("  OFF#1: base_fired=%v llm_calls=%d wall=%v", off1Fired, off1Calls, off1Time)
	t.Logf("  OFF#2: base_fired=%v llm_calls=%d wall=%v   (substrate-noise control)", off2Fired, off2Calls, off2Time)
	t.Logf("  ON   : base_fired=%v llm_calls=%d wall=%v", onFired, onCalls, onTime)
	if off1Time > 0 {
		t.Logf("  wall-clock ON vs OFF#1: %.1f%%   (NOISY on claude — see offline TestPrimitiveSubAgentFanoutOverlapsModelCalls for the clean ~50%% proof)",
			100*(1-float64(onTime)/float64(off1Time)))
	}

	// PARITY (load-bearing): the deterministic base parallel-safe specialist set MUST be identical OFF vs ON.
	// The seam changes timing, never which base faculties fire. (If the two OFF runs themselves diverge on
	// this set, the substrate is too noisy even on the deterministic signal — surfaced as a separate failure.)
	if !equalStringSet(off1Fired, off2Fired) {
		t.Fatalf("the two OFF (control) runs disagree on the DETERMINISTIC base set (%v vs %v) — the keyword-trigger "+
			"gate should be substrate-stable; investigate before trusting the OFF-vs-ON parity", off1Fired, off2Fired)
	}
	if !equalStringSet(off1Fired, onFired) {
		t.Fatalf("seam #2 changed the base parallel-safe specialist SET on the live substrate (a behaviour change, "+
			"not a speed-up):\n  OFF=%v\n  ON =%v", off1Fired, onFired)
	}
	// Both runs must actually exercise the multi-specialist fan-out (skeptic + advocate fired), else vacuous.
	if !containsStr(onFired, "skeptic") || !containsStr(onFired, "advocate") {
		t.Fatalf("the workload did not fire the skeptic+advocate fan-out (base_fired=%v) — the live parity is vacuous", onFired)
	}
	// Model-call count parity within tolerance (claude temperature varies decode/retries, never the structure).
	// Compare ON to the OFF SPREAD: the divergence must be no larger than the OFF-vs-OFF substrate spread + a
	// small margin, so the seam is not credited/blamed for substrate noise.
	offSpread := abs(off1Calls - off2Calls)
	if d := abs(off1Calls - onCalls); d > offSpread+3 {
		t.Fatalf("seam #2 changed the live model-call count beyond the substrate spread (ON=%d vs OFF#1=%d, "+
			"|Δ|=%d > OFF-spread %d + 3) — the seam should overlap calls, not add/skip them", onCalls, off1Calls, d, offSpread)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
