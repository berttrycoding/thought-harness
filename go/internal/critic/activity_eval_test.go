package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// withSibling stashes a high-value sibling on g's active branch — a viable BACKTRACK target (value
// above the pursuit threshold), the same setup the spine test uses.
func withSibling(g *graph.ThoughtGraph, value float64) {
	parent := g.ActiveBranch
	sib := g.NewBranch(&parent, nil)
	g.Branches[sib].Value = value
	g.Branches[sib].Status = types.STASHED
}

// TestActivityKnobsShiftDecisions is the graded-eval FLOOR for build slice (a) (the activity knobs):
// each case is a graph state where two conscious.activity configs yield DIFFERENT Controller decisions
// — proving the knobs measurably move BRANCH / BACKTRACK / STOP behaviour end-to-end through DecideNext,
// not merely through the wiring. The default config reproduces today's behaviour; the tuned config
// shifts it. Each row asserts wantDef != wantTune (so it genuinely demonstrates a shift).
func TestActivityKnobsShiftDecisions(t *testing.T) {
	distinct := []string{"alpha line", "beta line", "gamma line", "delta line", "epsilon line"}
	build := func(goal string, n int, src types.Source, conf float64, sibling bool) func() *graph.ThoughtGraph {
		return func() *graph.ThoughtGraph {
			g := graph.New(goal)
			for i := 0; i < n; i++ {
				appendT(g, distinct[i], src, conf)
			}
			if sibling {
				withSibling(g, 0.8)
			}
			return g
		}
	}

	cases := []struct {
		name     string
		build    func() *graph.ThoughtGraph
		opts     DecideOptions
		tune     func(*config.ConsciousActivityCfg)
		wantDef  types.Decision // decision under the DEFAULT activity config
		wantTune types.Decision // decision under the TUNED activity config
	}{
		{
			// 5 thoughts @0.6 + a viable sibling: default floor 0.5 ⇒ not exhausted ⇒ THINK;
			// raise the floor above 0.6 ⇒ exhausted + sibling ⇒ BACKTRACK.
			name:     "exhaust_conf raises BACKTRACK",
			build:    build("compare the designs", 5, types.GENERATED, 0.6, true),
			opts:     dec(false, false),
			tune:     func(a *config.ConsciousActivityCfg) { a.ExhaustConf = 0.9 },
			wantDef:  types.THINK,
			wantTune: types.BACKTRACK,
		},
		{
			// 5 thoughts @0.3 + a viable sibling: default ⇒ exhausted ⇒ BACKTRACK; raise the
			// length gate above 5 ⇒ not yet exhausted ⇒ THINK.
			name:     "exhaust_after suppresses BACKTRACK",
			build:    build("compare the designs", 5, types.GENERATED, 0.3, true),
			opts:     dec(false, false),
			tune:     func(a *config.ConsciousActivityCfg) { a.ExhaustAfter = 10 },
			wantDef:  types.BACKTRACK,
			wantTune: types.THINK,
		},
		{
			// 5 thoughts @0.3, NO sibling: default ⇒ loop-exhausted ⇒ ACT (import ground truth);
			// cut the step budget below 5 ⇒ over budget ⇒ give-up STOP.
			name:     "max_steps forces give-up STOP",
			build:    build("compare the designs", 5, types.GENERATED, 0.3, false),
			opts:     dec(false, false),
			tune:     func(a *config.ConsciousActivityCfg) { a.MaxSteps = 3 },
			wantDef:  types.ACT,
			wantTune: types.STOP,
		},
		{
			// 1 low-confidence INJECTED thought: default flag-band 0.6 ⇒ flagged ⇒ BRANCH (verify);
			// lower the flag floor below 0.55 ⇒ not flagged ⇒ THINK.
			name:     "flag_threshold raises BRANCH",
			build:    build("review the plan", 1, types.INJECTED, 0.55, false),
			opts:     dec(false, false),
			tune:     func(a *config.ConsciousActivityCfg) { a.FlagThreshold = 0.5 },
			wantDef:  types.BRANCH,
			wantTune: types.THINK,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.wantDef == c.wantTune {
				t.Fatalf("bad case: wantDef == wantTune (%v) — does not demonstrate a shift", c.wantDef)
			}

			defCfg := config.DefaultConsciousActivity()
			ctrlDef := NewController(noEmit, nil, "control", nil)
			ctrlDef.SetActivityConfig(&defCfg)
			if d := ctrlDef.DecideNext(c.build(), c.opts); d != c.wantDef {
				t.Errorf("default activity: got %v, want %v", d, c.wantDef)
			}

			tuneCfg := config.DefaultConsciousActivity()
			c.tune(&tuneCfg)
			ctrlTune := NewController(noEmit, nil, "control", nil)
			ctrlTune.SetActivityConfig(&tuneCfg)
			if d := ctrlTune.DecideNext(c.build(), c.opts); d != c.wantTune {
				t.Errorf("tuned activity: got %v, want %v", d, c.wantTune)
			}
		})
	}
}
