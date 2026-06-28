package subconscious

import (
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// latencyBackend embeds the deterministic TestBackend and injects a fixed per-call delay into
// OperatorApply (the reason-only sub-agent's model call). It is the wall-clock instrument: with the
// per-phase concurrency flag OFF the two reason-only sub-agents of a Par group call OperatorApply
// SERIALLY (their delays add); with it ON they call it CONCURRENTLY (their delays overlap). The content
// is the embedded TestBackend's deterministic string, so the OUTCOME is unchanged — only the wall-clock
// differs. This isolates the seam's speed-up from a live substrate's per-call latency variance (the
// claude bridge's uncontrollable temperature makes a noisy episode-level wall-clock; this is the clean,
// deterministic proof the overlap is REAL).
type latencyBackend struct {
	*backends.TestBackend
	delay time.Duration
}

func newLatencyBackend(delay time.Duration) *latencyBackend {
	return &latencyBackend{TestBackend: backends.NewTest(), delay: delay}
}

func (b *latencyBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	time.Sleep(b.delay) // stand in for a model call's decode latency
	return b.TestBackend.OperatorApply(role, responsibility, intent, domain, goal, ctx)
}

// TestParallelPhasesOverlapsModelCalls is the WALL-CLOCK proof for seam #1 (07-OPTIMISATION-SURVEY.md
// §A.1): the per-phase concurrency flag must make the two reason-only sub-agents of a Par phase-group
// (compare||contrast) run their model calls CONCURRENTLY, not serially. With a fixed per-call delay D
// injected into OperatorApply, the serial (flag-OFF) path spends ~2D in the Par group (the two calls add)
// while the parallel (flag-ON) path spends ~D (they overlap) — so the ON dispatch of the Par phase is
// SUBSTANTIALLY faster. The outcome is byte-identical (TestParallelPhasesDeterministicEquality proves
// that); this proves the SPEED-UP the byte-identical guarantee makes free.
//
// This is the deterministic, offline twin of the live-claude wall-clock bench: it removes the substrate's
// per-call latency variance so the overlap is a clean, failable measurement (a regression that serialised
// the path — e.g. a stray lock or an accidental wg.Wait per call — would show the ON time climb back to
// ~2D and fail here).
func TestParallelPhasesOverlapsModelCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("wall-clock timing test (sleeps); skipped under -short")
	}
	const delay = 120 * time.Millisecond

	timePar := func(parallel bool) time.Duration {
		be := newLatencyBackend(delay)
		start := time.Now()
		// runParallelDispatchCfg drives ONE Dispatch positioned on the par(compare,contrast) phase with
		// the given flag state; the two reason-only sub-agents each call OperatorApply once (delay D).
		fired, _ := runParallelDispatchCfg(t, parDispatchCfg{parallel: parallel, theta: 0.3, backend: be})
		elapsed := time.Since(start)
		if len(fired) < 2 {
			t.Fatalf("fixture did not fire both sub-agents (fired %d) — the timing is meaningless", len(fired))
		}
		return elapsed
	}

	serial := timePar(false)
	parallel := timePar(true)

	// The serial path runs the two calls back-to-back (>= 2D); the parallel path overlaps them (~D). Assert
	// the parallel dispatch is comfortably under the serial one — a 1.5D ceiling is well clear of the 2D
	// serial floor and the ~1D parallel target, with slack for scheduler/goroutine overhead. A regression
	// that re-serialised the calls would push parallel back toward 2D and trip this.
	ceiling := 3 * delay / 2 // 1.5 * D
	if parallel >= ceiling {
		t.Fatalf("parallel Par-group dispatch (%v) did not overlap the model calls (>= %v ceiling); "+
			"serial was %v (expected serial ~2D=%v, parallel ~D=%v with D=%v) — the concurrency seam may have "+
			"re-serialised", parallel, ceiling, serial, 2*delay, delay, delay)
	}
	// Sanity: the serial path really did pay ~2D (both calls counted), else the test is vacuous.
	if serial < 2*delay {
		t.Fatalf("serial Par-group dispatch (%v) was under 2D=%v — the fixture did not run both calls serially "+
			"(the comparison is vacuous)", serial, 2*delay)
	}
	t.Logf("seam #1 overlap: serial=%v parallel=%v (D=%v per call) — speed-up ~%.1f%% of the Par group's call cost",
		serial, parallel, delay, 100*(1-float64(parallel)/float64(serial)))
}
