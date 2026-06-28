package value

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// branchWithGoal builds a single-branch graph whose root goal is `goal` and appends the given
// thoughts to the active branch. The root GENERATED goal thought is part of the active branch, so
// callers testing novelty/coherence should account for it (it carries `goal`'s words; "" gives an
// empty root text).
func branchWithGoal(goal string, ts ...types.Thought) (*graph.ThoughtGraph, int) {
	g := graph.New(goal)
	for i := range ts {
		t := ts[i]
		t.ID = -1 // let Append allocate a fresh id (a 0 id would collide in g.Nodes)
		g.Append(&t, i+1)
	}
	return g, g.ActiveBranch
}

// TestBranchValueForGoalUsesPassedGoal verifies BranchValueForGoal measures goal_sim against the
// PER-BRANCH goal string, not the graph goal. The same thought scores higher against a goal whose
// words it overlaps and lower against an unrelated one — the per-branch setpoint (§1.8 G5).
func TestBranchValueForGoalUsesPassedGoal(t *testing.T) {
	// graph goal is unrelated to the thought; the per-branch goal overlaps it heavily.
	g, bid := branchWithGoal("unrelated graph objective entirely",
		types.Thought{Text: "verify the patch runs cleanly", Source: types.GENERATED, Confidence: 0.6})

	hit := NewSig().BranchValueForGoal(g, bid, "verify the patch runs cleanly").Value
	miss := NewSig().BranchValueForGoal(g, bid, "nothing in common here at all").Value
	if !(hit > miss) {
		t.Fatalf("per-branch goal should lift value: hit=%v miss=%v", hit, miss)
	}
}

// TestBranchValueForGoalEmptyFallsBackToGraphGoal verifies that passing "" makes BranchValueForGoal
// fall back to the graph goal — so it is byte-identical to the default AppraiseBranch path.
func TestBranchValueForGoalEmptyFallsBackToGraphGoal(t *testing.T) {
	g, bid := branchWithGoal("verify the patch runs cleanly",
		types.Thought{Text: "the patch should verify cleanly now", Source: types.GENERATED, Confidence: 0.8})

	vs := NewSig()
	def := vs.AppraiseBranch(g, bid).Value
	fallback := vs.BranchValueForGoal(g, bid, "").Value
	explicit := vs.BranchValueForGoal(g, bid, g.Goal).Value
	if math.Abs(def-fallback) > 1e-15 {
		t.Errorf("empty goal must equal default path: default=%v fallback=%v", def, fallback)
	}
	if math.Abs(def-explicit) > 1e-15 {
		t.Errorf("explicit graph goal must equal default path: default=%v explicit=%v", def, explicit)
	}
}

// TestBranchValueForGoalSiteSource is a guard that the goal-relative path returns an Appraisal with
// the same Site/Source as the default — it is the same signal, re-aimed.
func TestBranchValueForGoalSiteSource(t *testing.T) {
	g, bid := branchWithGoal("g",
		types.Thought{Text: "a thought", Source: types.GENERATED, Confidence: 0.5})
	def := NewSig().AppraiseBranch(g, bid)
	got := NewSig().BranchValueForGoal(g, bid, "some other goal")
	if got.Site != def.Site {
		t.Errorf("site=%q want %q", got.Site, def.Site)
	}
	if got.Source != def.Source {
		t.Errorf("source=%q want %q", got.Source, def.Source)
	}
}

// TestBranchValueForGoalEmptyBranch mirrors the default empty-branch appraisal — a branch with no
// non-METACOG thoughts is value 0.0 with reason "empty branch".
func TestBranchValueForGoalEmptyBranch(t *testing.T) {
	g := graph.New("g")
	parent := 0
	reason := "empty"
	b := g.NewBranch(&parent, &reason)
	ap := NewSig().BranchValueForGoal(g, b, "any goal")
	if ap.Value != 0.0 || ap.Reason != "empty branch" {
		t.Fatalf("empty appraisal = %+v", ap)
	}
	if ap.Signals == nil || len(ap.Signals) != 0 {
		t.Fatalf("signals must be a non-nil empty map: %v", ap.Signals)
	}
}

// TestIntrinsicValueRewardsNovelty verifies the goalless/wandering path: a branch that keeps
// introducing NEW vocabulary scores its novelty term higher than one that repeats itself (§1.8
// curiosity/novelty). No goal_sim term participates.
func TestIntrinsicValueRewardsNovelty(t *testing.T) {
	// novel: each thought brings fresh words.
	novel, nbid := branchWithGoal("",
		types.Thought{Text: "alpha beta gamma", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "delta epsilon zeta", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "eta theta iota", Source: types.GENERATED, Confidence: 0.6})
	// repetitive: the same words over and over.
	rep, rbid := branchWithGoal("",
		types.Thought{Text: "alpha beta gamma", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "alpha beta gamma", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "alpha beta gamma", Source: types.GENERATED, Confidence: 0.6})

	novSig, _ := NewSig().IntrinsicValue(novel, nbid).Signals["novelty"].(float64)
	repSig, _ := NewSig().IntrinsicValue(rep, rbid).Signals["novelty"].(float64)
	if !(novSig > repSig) {
		t.Fatalf("novelty should reward new vocabulary: novel=%v repeat=%v", novSig, repSig)
	}
}

// TestIntrinsicValueHasNoGoalSim asserts the wandering path carries NO goal_sim signal (it is the
// goalless value) but DOES carry the intrinsic drivers.
func TestIntrinsicValueHasNoGoalSim(t *testing.T) {
	g, bid := branchWithGoal("a standing goal here",
		types.Thought{Text: "wander into something new and connected", Source: types.GENERATED, Confidence: 0.7},
		types.Thought{Text: "connected to something new wandering", Source: types.GENERATED, Confidence: 0.7})
	ap := NewSig().IntrinsicValue(g, bid)
	if _, ok := ap.Signals["goal_sim"]; ok {
		t.Errorf("intrinsic value must NOT carry a goal_sim term: %v", ap.Signals)
	}
	if _, ok := ap.Signals["novelty"]; !ok {
		t.Errorf("intrinsic value must carry a novelty term: %v", ap.Signals)
	}
	if _, ok := ap.Signals["coherence"]; !ok {
		t.Errorf("intrinsic value must carry a coherence term: %v", ap.Signals)
	}
}

// TestIntrinsicValueRewardsCoherence verifies a line that knits its pieces together (each thought
// shares vocabulary with the line so far) scores coherence higher than disjoint fragments (§1.8).
func TestIntrinsicValueRewardsCoherence(t *testing.T) {
	// coherent: overlapping vocabulary thread.
	coh, cbid := branchWithGoal("",
		types.Thought{Text: "the engine runs the loop", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "the loop drives the engine forward", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "forward the engine loop continues", Source: types.GENERATED, Confidence: 0.6})
	// disjoint: no shared vocabulary at all.
	dis, dbid := branchWithGoal("",
		types.Thought{Text: "aaa bbb ccc", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "ddd eee fff", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "ggg hhh iii", Source: types.GENERATED, Confidence: 0.6})

	cohSig, _ := NewSig().IntrinsicValue(coh, cbid).Signals["coherence"].(float64)
	disSig, _ := NewSig().IntrinsicValue(dis, dbid).Signals["coherence"].(float64)
	if !(cohSig > disSig) {
		t.Fatalf("coherence should reward knit-together lines: coherent=%v disjoint=%v", cohSig, disSig)
	}
}

// TestIntrinsicValueBounded confirms the goalless value is clamped to [0,1] like the default V(s).
func TestIntrinsicValueBounded(t *testing.T) {
	g, bid := branchWithGoal("",
		types.Thought{Text: "high confidence novel coherent line", Source: types.GENERATED, Confidence: 1.0},
		types.Thought{Text: "another high confidence distinct novel line entirely", Source: types.GENERATED, Confidence: 1.0})
	v := NewSig().IntrinsicValue(g, bid).Value
	if v < 0.0 || v > 1.0 {
		t.Fatalf("intrinsic value must be in [0,1]: %v", v)
	}
}

// TestIntrinsicValueEmptyBranch mirrors the default empty-branch behaviour for the goalless path.
func TestIntrinsicValueEmptyBranch(t *testing.T) {
	g := graph.New("g")
	parent := 0
	reason := "empty"
	b := g.NewBranch(&parent, &reason)
	ap := NewSig().IntrinsicValue(g, b)
	if ap.Value != 0.0 || ap.Reason != "empty branch" {
		t.Fatalf("empty appraisal = %+v", ap)
	}
	if ap.Signals == nil || len(ap.Signals) != 0 {
		t.Fatalf("signals must be a non-nil empty map: %v", ap.Signals)
	}
}

// TestIntrinsicValueGroundedAndPendingStillApply confirms the wandering path keeps the reality
// (grounded) and pending-user terms — only goal_sim is swapped for the intrinsic drivers.
func TestIntrinsicValueGroundedAndPendingStillApply(t *testing.T) {
	g, bid := branchWithGoal("",
		types.Thought{Text: "explored a fresh idea here", Source: types.GENERATED, Confidence: 0.6},
		types.Thought{Text: "ran the check", Source: types.OBSERVATION, Confidence: 0.9,
			RawReturn: types.Observation{Ok: true, Text: "passed"}})
	ap := NewSig().IntrinsicValue(g, bid)
	if gr, ok := ap.Signals["grounded_reality"].(float64); !ok || gr <= 0 {
		t.Errorf("grounded term should survive on a confirmed observation: %v", ap.Signals)
	}
}

// TestDefaultPathUnchanged is the additivity guard: AppraiseBranch on a fixture with a non-empty
// graph goal is unaffected by the new code paths (it never calls them).
func TestDefaultPathUnchanged(t *testing.T) {
	g, bid := branchWithGoal("verify the patch runs cleanly",
		types.Thought{Text: "the patch should run cleanly now", Source: types.GENERATED, Confidence: 0.8})
	// recent_conf over [goal(conf 0), thought(conf 0.8)] = (0+0.8)/2 = 0.4 -> 0.55*0.4 = 0.22
	// goal_sim(thought, goal) = Jaccard("the patch should run cleanly now","verify the patch runs cleanly")
	//   thought {the,patch,should,run,cleanly,now}, goal {verify,the,patch,runs,cleanly}
	//   inter = {the,patch,cleanly} = 3 ; union = 6+5-3 = 8 ; sim = 0.375 -> 0.35*0.375 = 0.13125
	// V = 0.22 + 0.13125 = 0.35125
	got := NewSig().AppraiseBranch(g, bid).Value
	want := 0.55*0.4 + 0.35*0.375
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("default AppraiseBranch changed: got=%v want=%v", got, want)
	}
}
