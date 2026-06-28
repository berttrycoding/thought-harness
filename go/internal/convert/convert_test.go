package convert

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fakeReg is a PrimitiveSubAgentRegistrar test double: it just records what convertibility registers, so a
// test can inspect the minted specialist (and whether keep-or-revert later demoted it).
type fakeReg struct {
	registered []subconscious.PrimitiveSubAgent
}

func (f *fakeReg) Register(s subconscious.PrimitiveSubAgent) { f.registered = append(f.registered, s) }

// buildEpisode builds a one-branch graph for goal with n effortful (GENERATED) thoughts beyond the
// root and the given grounded value on the active branch — the trace convertibility.Observe reads.
func buildEpisode(goal string, n int, value float64) *graph.ThoughtGraph {
	g := graph.New(goal)
	for i := 0; i < n; i++ {
		g.Append(&types.Thought{ID: -1, Text: "worked step", Source: types.GENERATED, Confidence: 0.6}, 1)
	}
	g.Active().Value = value
	g.Active().Epistemic = value // the mint gate reads the epistemic projection (quality, not priority)
	return g
}

func mintedPrimitiveSubAgent(reg *fakeReg) *subconscious.MintedPrimitiveSubAgent {
	for _, s := range reg.registered {
		if m, ok := s.(*subconscious.MintedPrimitiveSubAgent); ok {
			return m
		}
	}
	return nil
}

// TestKeepOrRevertDemotesRefutedMint is the P0.5 gate: a pattern that practice minted into a
// specialist, but reality LATER refuted (a grounded outcome below the mint floor), is reverted — the
// specialist is demoted (stops firing) and the pattern's standing value drops below the floor.
func TestKeepOrRevertDemotesRefutedMint(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil) // default cfg: MintAfter=3, MintValue=0.2

	goal := "compute the tax bracket threshold value"

	// 1. Worked out effortfully (3 GENERATED) and converged HIGH (0.9) -> mints a specialist.
	c.Observe(buildEpisode(goal, 3, 0.9))
	minted := c.Consolidate()
	if len(minted) != 1 || len(c.Minted) != 1 {
		t.Fatalf("expected exactly one mint; minted=%v Minted=%v", minted, c.Minted)
	}
	sp := mintedPrimitiveSubAgent(reg)
	if sp == nil {
		t.Fatal("no MintedPrimitiveSubAgent was registered")
	}
	if sp.Demoted() {
		t.Fatal("a fresh mint must not be demoted")
	}
	if sp.Relevance([]types.Thought{{Text: goal}}) <= 0 {
		t.Fatal("the fresh mint should fire for its own trigger before any refutation")
	}

	// 2. Reality REFUTES it: a re-encounter grounds out below the floor (0.0) -> keep-or-revert.
	c.Observe(buildEpisode(goal, 1, 0.0))
	c.Consolidate()

	if len(c.Demoted) != 1 {
		t.Fatalf("a refuted mint must be demoted; Demoted=%v", c.Demoted)
	}
	if !sp.Demoted() {
		t.Fatal("the minted specialist must be reverted (Demoted) after refutation")
	}
	if rel := sp.Relevance([]types.Thought{{Text: goal}}); rel != 0 {
		t.Fatalf("a demoted specialist must no longer fire (relevance 0); got %v", rel)
	}
}

// TestKeepOrRevertKeepsValidatedMint guards the other side: a mint that reality keeps CONFIRMING
// (re-encounters stay above the floor) is NOT demoted — keep-or-revert reverts refutations, not
// every re-encounter.
func TestKeepOrRevertKeepsValidatedMint(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)

	goal := "compute the tax bracket threshold value"
	c.Observe(buildEpisode(goal, 3, 0.9))
	c.Consolidate()
	// Re-encounter, still grounded well above the floor -> the mint stands.
	c.Observe(buildEpisode(goal, 1, 0.85))
	c.Consolidate()

	if len(c.Demoted) != 0 {
		t.Fatalf("a still-validated mint must NOT be demoted; Demoted=%v", c.Demoted)
	}
	if sp := mintedPrimitiveSubAgent(reg); sp == nil || sp.Demoted() {
		t.Fatalf("the validated mint must remain live; sp=%v", sp)
	}
}

// groundedEpisode builds a one-branch graph whose active context carries n GROUNDED sourced moves (each
// an INJECTED thought stamped with a grounded FuelProvenance for op@source in domain) at the given branch
// value — the trace the M5 triple tally counts. Fresh ids per call so Observe counts each (idempotent).
func groundedEpisode(goal, source, domain string, n int, value float64, baseID int) *graph.ThoughtGraph {
	g := graph.New(goal)
	op := types.GENERATE
	for i := 0; i < n; i++ {
		d := domain
		g.Append(&types.Thought{
			ID: baseID + i, Text: "grounded " + source + " move", Source: types.INJECTED, Confidence: 0.8,
			RawReturn: &types.Candidate{
				Text: "fact", Source: types.INJECTED, Domain: &d, Operator: &op, Relevance: 0.8,
				Payload: types.FuelProvenance{Source: source, Provider: source + ":x", Grounded: true},
			},
		}, 1)
	}
	g.Active().Value = value
	g.Active().Epistemic = value // the mint gate reads the epistemic projection (quality, not priority)
	return g
}

// TestM5TripleTallyCountsOnlyGroundedMoves is the M5 triple-tally gate: Observe counts a GROUNDED
// (operator, source, domain) move into the triple tally, and EXCLUDES the ungrounded GENERATED rung (that
// is effort, counted by `pattern`, not a source paying off).
func TestM5TripleTallyCountsOnlyGroundedMoves(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)

	// a grounded memory move recurs twice; an ungrounded GENERATED episode contributes NO triple.
	c.Observe(groundedEpisode("hold the setpoint with feedback", "memory", "thermo", 2, 0.7, 10))
	c.Observe(buildEpisode("an unrelated effortful task entirely", 3, 0.6)) // GENERATED only -> no triple

	tris := c.Triples()
	if len(tris) != 1 {
		t.Fatalf("only the grounded move should tally a triple; got %d: %+v", len(tris), tris)
	}
	tr := tris[0]
	if tr.Operator != "generate" || tr.Source != "memory" || tr.Domain != "thermo" {
		t.Fatalf("triple key wrong: %+v", tr)
	}
	if tr.Count != 2 {
		t.Fatalf("the grounded move recurred twice; got count=%d", tr.Count)
	}
}

// TestM5HotTriplePavesPrimitive is the M5 pave gate (convert side): a hot grounded triple (>= MintAfter,
// >= MintValue) is compiled into a real primitive specialist, surfaced in MintedTriple.
func TestM5HotTriplePavesPrimitive(t *testing.T) {
	reg := &fakeReg{}
	cfg := Config{MintAfter: 3, MintValue: 0.2, MetacogAfter: 99}
	c := New(reg, nil, &cfg, nil)

	c.Observe(groundedEpisode("hold the setpoint with feedback", "memory", "thermo", 3, 0.7, 20))
	c.Consolidate()

	if len(c.MintedTriple) != 1 {
		t.Fatalf("a hot grounded triple must pave exactly one primitive; MintedTriple=%v", c.MintedTriple)
	}
	if mintedPrimitiveSubAgent(reg) == nil {
		t.Fatal("the paved primitive was not registered in the subconscious roster")
	}
}

// TestM5PathTrackingAndKeepOrRevert is the M5 path-tally gate: NotePath records a grounded path; a hot,
// grounded path is paved (convert.path_mint surfaced via MintedPath); and a refuted re-encounter reverts
// it (keep-or-revert). A path that never closed on a grounded source is NEVER paved.
func TestM5PathTrackingAndKeepOrRevert(t *testing.T) {
	reg := &fakeReg{}
	cfg := Config{MintAfter: 3, MintValue: 0.2, MetacogAfter: 99}
	c := New(reg, nil, &cfg, nil)

	// a GROUNDED deduction path recurs three times above the floor -> paved.
	for i := 0; i < 3; i++ {
		c.NotePath("deduction", true, 0.7)
	}
	// an UNGROUNDED analogy path recurs three times -> recorded but NEVER paved (model-only DoD).
	for i := 0; i < 3; i++ {
		c.NotePath("analogy", false, 0.9)
	}
	c.Consolidate()

	if len(c.MintedPath) != 1 || c.MintedPath[0] != "deduction" {
		t.Fatalf("only the grounded path should be paved; MintedPath=%v", c.MintedPath)
	}
	// the path stats reflect the tracking.
	var ded, ana PathStat
	for _, p := range c.Paths() {
		switch p.Name {
		case "deduction":
			ded = p
		case "analogy":
			ana = p
		}
	}
	if !ded.Grounded || !ded.Minted {
		t.Fatalf("deduction should be grounded+minted; got %+v", ded)
	}
	if ana.Minted {
		t.Fatalf("an ungrounded path must never be paved; got %+v", ana)
	}

	// keep-or-revert: the paved path stops closing on a grounded DoD (value below floor) -> demoted.
	c.NotePath("deduction", true, 0.05)
	c.Consolidate()
	for _, p := range c.Paths() {
		if p.Name == "deduction" && !p.Demoted {
			t.Fatal("a path that stopped closing on a grounded DoD must be demoted (keep-or-revert)")
		}
	}
}
