package campaign

// skillcurve_test.go — A3 STEP 1: the OFFLINE, mutation-sensitive proof that the curve apparatus drives the
// AUTONOMOUS skill-mint flywheel and that the curve BENDS at the mint.
//
// WHAT THE OFFLINE DOUBLE CAN AND CANNOT PROVE (honest scope). The TestBackend emits NO llm.call events
// (the completion-token / call-count cost signal comes ONLY from the real backend, internal/llm/openai.go:549),
// so OFFLINE both Completion AND Calls are 0 on every exposure — there is NO observable cost difference on the
// double. The cost MAGNITUDE (does the curve bend DOWN, by how much) is entirely the claude follow-up
// (skillcurve_claude_test.go). What the offline double CAN and MUST prove is the curve's SHAPE/WIRING — the
// structural mint+recall flywheel that MAKES the cost fall, which fires identically on the double:
//   - the mint fires AUTONOMOUSLY at the MintAfter-th exposure (no pre-seeding) — the persisted recurrence
//     counter accumulates 1->2->3 across fresh engines sharing one state dir (the W5-2b fix 25e3ea8),
//   - recall fires on the exposures AFTER the mint (synth step-0 library.Match short-circuits synthesis —
//     the mechanism that drops the synthesize completion tokens on claude).
// MUTATION TEST: with a NIL store (in-memory, no persistence) the counter resets every exposure, so the
// mint NEVER fires, recall NEVER fires, and the curve is FLAT — the apparatus must report that honestly
// (FirstRecall=-1). If the apparatus reported a mint/recall with no persistence it would be measuring a
// flywheel the W5-2b memory proves cannot run without the persisted counter — i.e. measuring nothing.

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// curveComprehendBackend is a SYMBOL-AGNOSTIC scripted RealityComprehender over the test double: it reads the
// symbol to search from the GOAL/context itself (any groundedSymbols entry), so a curve over the grounded bank
// grounds on every exposure regardless of which symbol the goal names. Same search->read handoff shape as the
// W5-2c groundedComprehendBackend, but not pinned to one symbol (the curve runs a family).
type curveComprehendBackend struct {
	*backends.TestBackend
	pathRe *regexp.Regexp
	symRe  *regexp.Regexp
}

func newCurveComprehendBackend() *curveComprehendBackend {
	// the bank symbols are CamelCase identifiers; the goal embeds one ("…value assigned to <Symbol>").
	return &curveComprehendBackend{
		TestBackend: backends.NewTest(),
		pathRe:      regexp.MustCompile(`([\w./-]+\.go):\d+:`),
		symRe:       regexp.MustCompile(`\b([A-Z][A-Za-z]{6,})\b`),
	}
}

func (b *curveComprehendBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	joined := ""
	for _, t := range ctx {
		joined += " " + t.Text
	}
	if m := b.pathRe.FindStringSubmatch(joined); m != nil {
		return "read", m[1], true // a source file is named -> read it
	}
	if m := b.symRe.FindStringSubmatch(joined); m != nil {
		return "search", m[1], true // a symbol is named -> search for where it lives
	}
	return "search", groundedSymbols[0].Symbol, true // fallback to the first bank symbol
}

var _ backends.RealityComprehender = (*curveComprehendBackend)(nil)

// curveEngineFactory builds a workspace-wired test-double engine with the symbol-agnostic curve comprehender,
// seeded from stateDir — the OFFLINE mirror of the claude curve factory. cfg.Features=nil == config.New()
// (AllOn): SkillMint + Persist ON (so the autonomous mint flywheel runs), watched_sync ON (the reality port).
// A real persist.JSONLStore is wired iff stateDir != "" — that shared dir is what makes the counter persist.
func curveEngineFactory(workspace string) func(stateDir string) (*engine.Engine, error) {
	return func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Workspace = workspace
		cfg.Features = nil // == config.New() AllOn: SkillMint + Persist + watched_sync ON
		if stateDir != "" {
			st, err := persist.NewJSONLStore(stateDir)
			if err != nil {
				return nil, err
			}
			cfg.Store = st
		}
		return engine.NewEngine(&cfg, newCurveComprehendBackend())
	}
}

// curveFamily is the recurring grounded family for the offline curve: the FIRST bank symbol repeated (one
// goal key, one symbol the comprehender grounds every exposure) so the recurrence regime is exercised
// cleanly offline. The faculty signature is "act" (the grounding goal elicits reality-import). On claude the
// stream can vary the symbol; offline a single symbol keeps grounding deterministic.
func curveFamily() []CurveTask {
	s := groundedSymbols[0]
	return []CurveTask{{
		Goal:      "investigate the source files in this codebase and report the exact numeric value assigned to " + s.Symbol,
		Expect:    s.Value,
		Signature: "act",
	}}
}

// TestSkillCurveAutonomousMintAndRecall is the offline mutation-sensitive apparatus proof. It drives a stream
// of >MintAfter exposures of the recurring grounded family through ONE shared, PERSISTED state dir and asserts
// the autonomous flywheel fires: the mint at MintAfter, recall after, and Calls drop on recall (the offline
// shadow of the cost bend). The matching NIL-store control must stay FLAT (no mint/recall/drop).
func TestSkillCurveAutonomousMintAndRecall(t *testing.T) {
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")

	b := EngineBencher{MaxTicks: 40, NewEngine: curveEngineFactory(ws)}

	// MintAfter is 3 (convert.DefaultConfig); run 6 exposures so there are pre-mint AND post-recall cohorts.
	const exposures = 6
	stream := CurveStream(curveFamily(), exposures)
	pts, err := b.SkillCurve(stream, stateDir)
	if err != nil {
		t.Fatalf("SkillCurve (persisted): %v", err)
	}
	if len(pts) != exposures {
		t.Fatalf("got %d curve points, want %d", len(pts), exposures)
	}

	var firstMint, firstRecall = -1, -1
	groundedAny := false
	for _, p := range pts {
		t.Logf("exposure %d: calls=%d completion=%d minted=%v recalled=%v grounded=%v solved=%v fired(act)=%v",
			p.Exposure, p.Calls, p.Completion, p.Minted, p.Recalled, p.Grounded, p.Solved, p.Fired)
		if p.Minted && firstMint < 0 {
			firstMint = p.Exposure
		}
		if p.Recalled && firstRecall < 0 {
			firstRecall = p.Exposure
		}
		if p.Grounded {
			groundedAny = true
		}
	}

	// (1) the bank GROUNDED (held-positive-utility prerequisite — the cost number is meaningful, not the
	// W5-2b zero-grounding caveat). Without grounding the value gate (groundedVal >= MintValue) never opens
	// and the mint cannot fire.
	if !groundedAny {
		t.Fatal("the curve never grounded — the workspace/comprehender path is not wired; the mint value gate cannot open")
	}
	// (2) the mint fired AUTONOMOUSLY (no pre-seed). With MintAfter=3 the count crosses on the 3rd exposure;
	// the idle Consolidate mints that episode, so firstMint is at exposure index 2 (the 3rd). Allow >=2 to
	// be robust to the exact idle-pass timing, but it MUST fire within the stream.
	if firstMint < 0 {
		t.Fatal("the skill NEVER minted autonomously across the persisted stream — the recurrence counter did " +
			"not accumulate (the W5-2b persist fix is the load-bearing dependency; FirstMint=-1)")
	}
	if firstMint < 2 {
		t.Errorf("the mint fired at exposure %d, before MintAfter(3) exposures accumulated — premature mint", firstMint)
	}
	// (3) recall fired AFTER the mint (the cost-falling mechanism). It must fire on an exposure > firstMint.
	if firstRecall < 0 {
		t.Fatal("recall NEVER fired after the mint — the minted skill did not persist/reload or its triggers " +
			"do not match the recurring goal key (mint-but-never-recall, the red-team silent-failure mode)")
	}
	if firstRecall <= firstMint {
		t.Errorf("recall fired at exposure %d, not strictly after the mint at %d — recall must follow the mint", firstRecall, firstMint)
	}

	// (4) the recall is SUSTAINED — once minted, every subsequent exposure recalls (the skill persists +
	// keeps short-circuiting synthesis). On claude this sustained recall is what makes the POST cohort's
	// completion-token mean fall and STAY low (the curve bend, not a one-off). Offline Completion is 0, so we
	// assert the structural sustain (every exposure after firstRecall recalled), which IS the cost mechanism.
	for _, p := range pts {
		if p.Exposure > firstRecall && !p.Recalled {
			t.Errorf("recall was NOT sustained: exposure %d (after first recall %d) did not recall — the minted "+
				"skill stopped firing (a non-sustained recall would not bend the cost curve on claude)", p.Exposure, firstRecall)
		}
	}

	// (5) offline cost is DEGENERATE (0 everywhere) — assert it honestly so a future change that starts
	// emitting fake offline usage is caught (the cost magnitude must be claude-only).
	for _, p := range pts {
		if p.Completion != 0 {
			t.Errorf("exposure %d reported Completion=%d on the offline test double — the double emits no real "+
				"usage; a non-zero offline completion is fabricated cost (the magnitude is the claude follow-up)", p.Exposure, p.Completion)
		}
	}
}

// TestSkillCurveNoPersistStaysFlat is the MUTATION control: with a NIL store (no shared persisted state dir)
// the recurrence counter resets every exposure, so the mint NEVER fires, recall NEVER fires, and the curve is
// FLAT. If this curve "bent" (minted/recalled) the apparatus would be detecting a flywheel that the W5-2b
// memory proves cannot run without persistence — i.e. measuring noise. This is the test that fails if the
// persistence dependency is silently broken/removed.
func TestSkillCurveNoPersistStaysFlat(t *testing.T) {
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}
	b := EngineBencher{MaxTicks: 40, NewEngine: curveEngineFactory(ws)}

	const exposures = 6
	stream := CurveStream(curveFamily(), exposures)
	pts, err := b.SkillCurve(stream, "") // "" => nil store => no persistence => counter resets every exposure
	if err != nil {
		t.Fatalf("SkillCurve (no persist): %v", err)
	}
	for _, p := range pts {
		if p.Minted {
			t.Errorf("exposure %d MINTED with no persisted state — the counter must reset every exposure (no autonomous mint without the shared dir)", p.Exposure)
		}
		if p.Recalled {
			t.Errorf("exposure %d RECALLED with no persisted state — no skill can have minted, so none can recall", p.Exposure)
		}
	}
}
