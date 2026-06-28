package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/seams"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ucbGraph builds a graph with two stashed frontier branches: A (higher value 0.90) and B (slightly
// lower value 0.80). Value-greedy always picks A; UCB can pick B once A is over-visited and B is not —
// the canonical case the exploration bonus exists to decide differently. Visit counts live OUTSIDE the
// graph (the passed-in map), so types.Branch is untouched.
func ucbGraph(t *testing.T) (g *graph.ThoughtGraph, aID, bID int) {
	t.Helper()
	g = graph.New("goal")
	aID = g.NewBranch(nil, nil)
	bID = g.NewBranch(nil, nil)
	ta := &types.Thought{ID: -1, Text: "higher-value line", Source: types.GENERATED, BranchID: &aID}
	g.Nodes[100] = ta
	g.Branches[aID].ThoughtIDs = []int{100}
	g.Branches[aID].Value = 0.90
	g.Branches[aID].Status = types.STASHED
	tb := &types.Thought{ID: -1, Text: "slightly-lower-value line", Source: types.GENERATED, BranchID: &bID}
	g.Nodes[200] = tb
	g.Branches[bID].ThoughtIDs = []int{200}
	g.Branches[bID].Value = 0.80
	g.Branches[bID].Status = types.STASHED
	return g, aID, bID
}

// TestResolveFocusBranchUCBZeroIsValueGreedy: with c=0 (and lam=0) the pick is EXACTLY Frontier()[0] —
// the higher-value branch — and the visits map is NEVER read (a heavily-visited greedy head still wins).
// This is the default, byte-identical path.
func TestResolveFocusBranchUCBZeroIsValueGreedy(t *testing.T) {
	g, aID, bID := ucbGraph(t)
	// Pile visits on A: under UCB this would penalise A, but with c=0 the map is ignored.
	visits := map[int]int{aID: 1000, bID: 0}
	pick, greedy := resolveFocusBranch(g, 0, 0, visits)
	if pick == nil || pick.ID != aID {
		t.Fatalf("c=0 must pick value-greedy frontier head %d, got %+v", aID, pick)
	}
	if greedy == nil || greedy.ID != aID {
		t.Fatalf("greedy head must be %d, got %+v", aID, greedy)
	}
	if fr := g.Frontier(); fr[0].ID != pick.ID {
		t.Fatalf("c=0 pick must equal Frontier()[0]")
	}
	_ = bID
}

// TestResolveFocusBranchUCBExplores: the COGNITION. With c>0, a LESS-visited slightly-lower-value branch
// B out-ranks the more-visited higher-value branch A — the exploration bonus works. A's bonus shrinks
// because it has been resumed many times; B's bonus is large because it is neglected.
//
//	A: 0.90 + c*sqrt(ln 20 / (1+19))  with c=1 ≈ 0.90 + sqrt(2.996/20) ≈ 0.90 + 0.387 = 1.287
//	B: 0.80 + c*sqrt(ln 20 / (1+0))   with c=1 ≈ 0.80 + sqrt(2.996/1)  ≈ 0.80 + 1.731 = 2.531  -> B wins
func TestResolveFocusBranchUCBExplores(t *testing.T) {
	g, aID, bID := ucbGraph(t)
	visits := map[int]int{aID: 19, bID: 0}
	pick, greedy := resolveFocusBranch(g, 1.0, 0, visits)
	if pick == nil || pick.ID != bID {
		t.Fatalf("c>0 with A over-visited and B neglected must explore B (%d), got %+v", bID, pick)
	}
	// The greedy head is still reported as A — so the caller can detect a NON-greedy resume and emit.
	if greedy == nil || greedy.ID != aID {
		t.Fatalf("greedy head must remain the value head %d, got %+v", aID, greedy)
	}
	if pick.ID == greedy.ID {
		t.Fatalf("expected a non-greedy UCB pick (pick != greedy)")
	}
}

// TestResolveFocusBranchUCBExploitsWhenBalanced: when neither branch is over-visited, the exploration
// bonus is equal across the frontier, so UCB falls back to exploiting the higher-value head (A). The
// policy does not pick a worse line gratuitously.
func TestResolveFocusBranchUCBExploitsWhenBalanced(t *testing.T) {
	g, aID, _ := ucbGraph(t)
	visits := map[int]int{} // both unvisited -> equal bonus -> value decides -> A
	pick, _ := resolveFocusBranch(g, 1.0, 0, visits)
	if pick == nil || pick.ID != aID {
		t.Fatalf("c>0 with equal visits must exploit the value head %d, got %+v", aID, pick)
	}
}

// TestResolveFocusBranchUCBDeterministic: same graph + same visits + same c ⇒ same pick, every time
// (UCBFrontier sorts stably; no wall clock, no RNG, no map-iteration-order dependence in the result).
func TestResolveFocusBranchUCBDeterministic(t *testing.T) {
	want := -1
	for i := 0; i < 50; i++ {
		g, aID, _ := ucbGraph(t)
		visits := map[int]int{aID: 19} // A over-visited -> B should win, repeatably
		pick, _ := resolveFocusBranch(g, 1.0, 0, visits)
		if pick == nil {
			t.Fatalf("iter %d: nil pick", i)
		}
		if want == -1 {
			want = pick.ID
		} else if pick.ID != want {
			t.Fatalf("iter %d: non-deterministic pick %d != %d", i, pick.ID, want)
		}
	}
}

// TestResolveFocusBranchUCBEmptyFrontier: no open branches -> nil pick under the UCB policy too (the
// BACKTRACK case then does nothing, exactly as before).
func TestResolveFocusBranchUCBEmptyFrontier(t *testing.T) {
	g := graph.New("goal")
	pick, greedy := resolveFocusBranch(g, 1.0, 0, map[int]int{})
	if pick != nil || greedy != nil {
		t.Fatalf("empty frontier must be nil/nil, got pick=%+v greedy=%+v", pick, greedy)
	}
}

// TestResolveFocusBranchBeamStillWorksWhenUCBOff: c<=0 falls through to the existing beam path. With
// lam>0 the grounded branch wins (mirrors the beam test), proving UCB and beam are mutually exclusive
// and beam is unchanged when UCB is off.
func TestResolveFocusBranchBeamStillWorksWhenUCBOff(t *testing.T) {
	g, _, bID := beamGraph(t) // B carries an OBSERVATION (better grounded), A is pure GENERATED
	pick, _ := resolveFocusBranch(g, 0 /*ucb off*/, 0.5 /*beam on*/, map[int]int{})
	if pick == nil || pick.ID != bID {
		t.Fatalf("c=0,lam=0.5 must use beam and pick grounded branch %d, got %+v", bID, pick)
	}
}

// --- engine-level wiring (the visits map increments + the event fires through the real BACKTRACK path)

// ucbWiredEngine builds a real engine on the test double whose graph carries an active root plus two
// STASHED frontier branches (A higher-value, B lower-value), so a BACKTRACK resume goes through the
// wired resolveFocusBranch -> visit-increment -> (optional) UCBSelect emit -> mcp.Focus path. Returns
// the engine, the captured log, and the two branch ids.
func ucbWiredEngine(t *testing.T) (e *Engine, log *eventSink, aID, bID int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	eng, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// The graph + mcp are built per-episode (startEpisode); stand them up directly for this focused
	// BACKTRACK test, mirroring reactive.go's construction, and arm the episode-scoped visit map.
	eng.graph = graph.New("goal")
	eng.mcp = graph.NewThoughtMCP(eng.graph, eng.backend, eng.bus.Emit)
	eng.branchVisits = map[int]int{}
	g := eng.graph
	aID = g.NewBranch(nil, nil)
	bID = g.NewBranch(nil, nil)
	g.Nodes[100] = &types.Thought{ID: -1, Text: "higher-value line", Source: types.GENERATED, BranchID: &aID}
	g.Branches[aID].ThoughtIDs = []int{100}
	g.Branches[aID].Value = 0.90
	g.Branches[aID].Status = types.STASHED
	g.Nodes[200] = &types.Thought{ID: -1, Text: "lower-value line", Source: types.GENERATED, BranchID: &bID}
	g.Branches[bID].ThoughtIDs = []int{200}
	g.Branches[bID].Value = 0.80
	g.Branches[bID].Status = types.STASHED
	log = &eventSink{}
	eng.bus.Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return eng, log, aID, bID
}

// eventSink is a minimal captured event stream for the internal-package wiring test.
type eventSink struct{ events []events.Event }

func (s *eventSink) count(kind events.Kind) int {
	n := 0
	for _, e := range s.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// TestUCBVisitsIncrementOnRepeatedFocus: driving BACKTRACK repeatedly increments the per-branch visit
// count for whichever branch is resumed (the new engine state UCB needs). Maintained regardless of c.
func TestUCBVisitsIncrementOnRepeatedFocus(t *testing.T) {
	e, _, aID, _ := ucbWiredEngine(t)
	// c=0,lam=0: greedy resumes A. After two BACKTRACKs (the active flips back and forth), A accrues
	// visits — proving the map is maintained on every resume, deterministically.
	for i := 0; i < 4; i++ {
		e.execute(types.BACKTRACK, seams.RelayResult{}, i, false)
	}
	if e.branchVisits[aID] < 1 {
		t.Fatalf("repeated focus must accrue visits on the resumed branch; branchVisits=%v", e.branchVisits)
	}
	total := 0
	for _, v := range e.branchVisits {
		total += v
	}
	if total < 1 {
		t.Fatalf("expected at least one recorded visit across BACKTRACKs, got %v", e.branchVisits)
	}
}

// TestUCBOffEmitsNoEvent (byte-identity guard): with ucbC=0 (the default the whole suite runs under) a
// BACKTRACK resume emits NO conscious.ucb_select event — the policy is off and invisible.
func TestUCBOffEmitsNoEvent(t *testing.T) {
	if ucbC != 0 {
		t.Skipf("ucbC=%v (THOUGHT_UCB_C set in env) — this guard asserts the default OFF path", ucbC)
	}
	e, log, _, _ := ucbWiredEngine(t)
	e.execute(types.BACKTRACK, seams.RelayResult{}, 0, false)
	if n := log.count(events.UCBSelect); n != 0 {
		t.Fatalf("ucbC=0 must emit no UCBSelect event, got %d", n)
	}
}

// TestUCBOnEmitsEventOnNonGreedyResume: with the policy ON and A over-visited, a BACKTRACK resumes the
// neglected branch B (non-greedy) and SURFACES it via conscious.ucb_select carrying the greedy head it
// overtook. Drives the real wired path (resolveFocusBranch -> emit -> Focus).
func TestUCBOnEmitsEventOnNonGreedyResume(t *testing.T) {
	old := ucbC
	ucbC = 1.0
	defer func() { ucbC = old }()
	e, log, aID, bID := ucbWiredEngine(t)
	// Pre-load A with visits so its exploration bonus is small; B is neglected -> B wins the resume.
	e.branchVisits[aID] = 30
	e.execute(types.BACKTRACK, seams.RelayResult{}, 0, false)
	evs := log.events
	var found *events.Event
	for i := range evs {
		if evs[i].Kind == events.UCBSelect {
			found = &evs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("UCB-on non-greedy resume must emit conscious.ucb_select; events=%d", len(evs))
	}
	if got := found.Data["branch"]; got != bID {
		t.Fatalf("UCBSelect branch = %v, want neglected B %d", got, bID)
	}
	if got := found.Data["greedy"]; got != aID {
		t.Fatalf("UCBSelect greedy = %v, want value head A %d", got, aID)
	}
	// The resumed branch's visit count incremented through the same path.
	if e.branchVisits[bID] < 1 {
		t.Fatalf("the UCB-resumed branch must have accrued a visit; got %v", e.branchVisits[bID])
	}
}

// TestResolveUCBCParsing: unset/garbage/negative/zero -> 0 (off); positive parses through; no upper
// clamp (c is a free exploration weight). (The package-level ucbC is resolved from the test process env
// — unset — so every other engine test in this package exercises the default OFF path.)
func TestResolveUCBCParsing(t *testing.T) {
	t.Setenv("THOUGHT_UCB_C", "")
	if v := resolveUCBC(); v != 0 {
		t.Fatalf("unset -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_UCB_C", "garbage")
	if v := resolveUCBC(); v != 0 {
		t.Fatalf("garbage -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_UCB_C", "-1.5")
	if v := resolveUCBC(); v != 0 {
		t.Fatalf("negative -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_UCB_C", "0")
	if v := resolveUCBC(); v != 0 {
		t.Fatalf("zero -> %v, want 0", v)
	}
	t.Setenv("THOUGHT_UCB_C", "1.41")
	if v := resolveUCBC(); v != 1.41 {
		t.Fatalf("1.41 -> %v", v)
	}
	t.Setenv("THOUGHT_UCB_C", "5")
	if v := resolveUCBC(); v != 5 {
		t.Fatalf("5 (no upper clamp) -> %v", v)
	}
}
