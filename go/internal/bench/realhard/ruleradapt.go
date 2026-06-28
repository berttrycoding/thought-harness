package realhard

import "github.com/berttrycoding/thought-harness/internal/ruler"

// ruleradapt.go — the realhard → W5-1 ruler adapter (CAP-EVAL measurement-
// reliability). It bridges the realhard per-task replay rollup into the
// substrate-independent rows the ruler's Characterize reduces, mirroring
// ruler.FromProbe / ruler.FromCog exactly.
//
// THE BINARY AXIS IS THE WHOLE STORY HERE. The realhard oracle is a 0/1
// solve indicator (Score().Solved); the per-task K-replay solve COUNT is all
// the binary-axis characterization needs (the within-task variance of a 0/1
// indicator is p(1-p), recovered from the count — no per-replay vector). So the
// rows carry no Completions vector and the ruler's cost axis reads DEGENERATE,
// which is honest: realhard does not retain per-replay completion tokens, so its
// COST instrument is uncharacterized (the noise-floor question realhard answers
// is the SOLVE-RATE one — the ±56pp cross-run swing).
//
// Determinism: a pure projection over the Report's already-reduced per-task
// rollup; no RNG, no I/O (CLAUDE.md headless-pure).

// HarnessReplayRows projects the report's per-task HARNESS rollup into the
// ruler's substrate-independent rows (the harness arm is the one whose noise
// floor the CAP-EVAL gate characterizes — ARM B is the config under test). One
// row per task: Success = replays the harness solved, Replays = K.
func (rep Report) HarnessReplayRows() []ruler.TaskReplays {
	out := make([]ruler.TaskReplays, 0, len(rep.Tasks))
	for _, t := range rep.Tasks {
		out = append(out, ruler.TaskReplays{
			ID:      t.TaskID,
			Success: t.HarnSolved,
			Replays: t.K,
		})
	}
	return out
}

// BareReplayRows is the same projection for ARM A (bare), so a characterization
// can be run per-arm when both arms have runs. Empty when no bare runs.
func (rep Report) BareReplayRows() []ruler.TaskReplays {
	out := make([]ruler.TaskReplays, 0, len(rep.Tasks))
	for _, t := range rep.Tasks {
		out = append(out, ruler.TaskReplays{
			ID:      t.TaskID,
			Success: t.BareSolved,
			Replays: t.K,
		})
	}
	return out
}

// CharacterizeHarness runs the W5-1 ruler over the harness arm's per-task replay
// rows — the realhard convenience entrypoint analogous to ruler.CharacterizeCog.
func (rep Report) CharacterizeHarness(opts ruler.Options) ruler.Characterization {
	return ruler.Characterize(rep.HarnessReplayRows(), opts)
}
