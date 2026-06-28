package scenarios

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// TestGoldenParityUnderParallelPhases is the STANDING cross-package regression for BOTH concurrency
// speed-up seams (07-OPTIMISATION-SURVEY.md §A.1): seam #1 (the workflow Par phase-group reason-only
// sub-agent fan-out) AND seam #2 (the per-tick base-specialist model-call fan-out, social/skeptic/advocate).
// Both seams share the THOUGHT_PARALLEL_PHASES flag, so forcing it ON in-process exercises both. It re-runs
// the SAME golden streams (every S1..S16 scenario + the two run modes — which fire the review/refactor
// shapes that light skeptic+advocate, the seam-#2 fan-out, AND the par(compare,contrast) shape, the seam-#1
// fan-out) and asserts the captured event stream is BYTE-IDENTICAL to the committed golden fixtures — the
// exact same assertParity the serial gate uses.
//
// Why this exists as code, not a one-off env-var run: the flag is resolved ONCE from env at package init,
// so a plain `THOUGHT_PARALLEL_PHASES=1 go test` reaches the path but is not a STANDING guard (it reverts
// to the default the moment someone runs `go test ./...`). This test forces the path ON via the in-process
// hook (subconscious.SetParallelPhasesForTest) so the byte-identical-under-concurrency property is
// re-checked on EVERY suite run — the regression that would otherwise only surface in a manual flag-ON run.
//
// With the flag now defaulting ON, the sibling TestGoldenParity ALREADY exercises both parallel paths; this
// test pins them EXPLICITLY (independent of the default) so a future default-flip-back cannot silently stop
// the concurrent paths from being golden-gated. It also runs the OFF (legacy serial) path against the same
// goldens, so both paths stay proven byte-identical to the committed fixtures.
func TestGoldenParityUnderParallelPhases(t *testing.T) {
	if updateGolden() {
		t.Skip("regen mode: TestGoldenParity owns the golden rewrite; this is a read-only parity guard")
	}

	for _, on := range []bool{true, false} {
		on := on
		name := "parallel-on"
		if !on {
			name = "serial-off"
		}
		t.Run(name, func(t *testing.T) {
			restore := subconscious.SetParallelPhasesForTest(on)
			defer restore()

			for _, sc := range All() {
				sc := sc
				t.Run(sc.ID, func(t *testing.T) {
					got := runScenarioCapture(t, sc.ID)
					assertParity(t, sc.ID, got)
				})
			}

			runModes := []struct {
				mode   string
				prompt string
			}{
				{"reactive", "What's 7×8?"},
				{"continuous", "(awake — no task)"},
			}
			for _, rm := range runModes {
				rm := rm
				t.Run(rm.mode, func(t *testing.T) {
					got := runModeCapture(t, rm.mode, rm.prompt)
					assertParity(t, rm.mode, got)
				})
			}
		})
	}
}
