package campaign

// efficiency_grounded_test.go — W5-2c (offline half): prove the GROUNDED efficiency bank GROUNDS (so the
// cost win can be re-measured at held-POSITIVE utility) AND the WARM arm still recalls the seeded skill /
// short-circuits synthesis on the grounding goals.
//
// THE TEST-DOUBLE CAVEAT (honest scope). Grounding on the OFFLINE path requires (a) a real executor (a wired
// cfg.Workspace) AND (b) a RealityComprehender backend that names the read/search target (engine/knowledge.go
// SourceReality). The bare TestBackend is NOT a RealityComprehender, so it cannot ground by itself — it would
// fabricate every act (which never grounds). So these tests wrap the double with a SCRIPTED comprehender
// (the same search->read handoff stand-in as engine/a1_searchread_repro_test.go) to prove the GROUNDING PATH
// is real with the workspace wired. The deterministic comprehender stands in for the real model's
// comprehension; on the claude re-run the real model supplies it, and THAT is where the held-positive cost
// magnitude is measured. So: this file proves (1) the bank grounds + the oracle scores with the workspace +
// a comprehender, and (2) the warm arm recalls + short-circuits synthesis on the grounding goal — the
// completion-token DROP at held-positive grounded-success is the claude follow-up (see efficiency_claude_test.go).

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// groundedComprehendBackend wraps the deterministic TestBackend and ADDS a scripted RealityComprehender so
// the offline grounded-efficiency test exercises the real search->read handoff (the A1 path) without a model:
// when no source file is yet named in context it SEARCHES for the symbol; once a search hit names the file it
// READS that file. This isolates the grounding WIRING (does the workspace read import a non-fabricated
// observation that grounds + lands the value in a thought) from whether a real model makes the right call.
type groundedComprehendBackend struct {
	*backends.TestBackend
	symbol string
	pathRe *regexp.Regexp
}

func newGroundedComprehendBackend(symbol string) *groundedComprehendBackend {
	return &groundedComprehendBackend{
		TestBackend: backends.NewTest(),
		symbol:      symbol,
		// a search hit looks like "admit.go:4:const AdmitAmbiguityThreshold = 0.4271" — lift the path.
		pathRe: regexp.MustCompile(`([\w./-]+\.go):\d+:`),
	}
}

func (b *groundedComprehendBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	joined := ""
	for _, t := range ctx {
		joined += " " + t.Text
	}
	if m := b.pathRe.FindStringSubmatch(joined); m != nil {
		return "read", m[1], true // a source is named but not yet quoted -> read it
	}
	return "search", b.symbol, true // nothing concrete yet -> find where the symbol lives
}

var _ backends.RealityComprehender = (*groundedComprehendBackend)(nil)

// groundedEngineFactory builds a workspace-wired test-double engine whose backend is a scripted comprehender
// for `symbol`, seeded from stateDir — the offline mirror of the A1 probe's `--workspace ..` engine and the
// claude re-run's workspace factory. cfg.Workspace makes buildExecutor wire the REAL executor (so an act
// crosses the watched seam against the fixture files), cfg.Features=AllOn enables watched_sync (the rung-4
// reality port), and the comprehender names the read/search target so the value actually grounds.
func groundedEngineFactory(workspace, symbol string) func(stateDir string) (*engine.Engine, error) {
	return func(stateDir string) (*engine.Engine, error) {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Workspace = workspace // the REAL executor (the agentic/reality-access axis)
		cfg.Features = nil        // nil == AllOn (watched_sync on, all specialists) — byte-identical default
		if stateDir != "" {
			st, err := persist.NewJSONLStore(stateDir)
			if err != nil {
				return nil, err
			}
			cfg.Store = st
		}
		return engine.NewEngine(&cfg, newGroundedComprehendBackend(symbol))
	}
}

// TestGroundedEfficiencyGoalGroundsOnTheDouble proves the held-positive-utility PREREQUISITE: a grounded
// efficiency goal, driven through a workspace-wired engine with a scripted comprehender, IMPORTS reality
// (grounded-success > 0) and the fixture VALUE reaches a thought (the Expect oracle scores). Without this the
// W5-2c cost re-measure would still be at zero grounding — the exact caveat W5-2c exists to remove. It uses
// the FIRST bank symbol (the one the mint goal also targets). Mutation-sensitive: a workspace that does NOT
// carry the value, or a bare (non-comprehender) double, would fail to ground here.
func TestGroundedEfficiencyGoalGroundsOnTheDouble(t *testing.T) {
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}
	bank := GroundedEfficiencyBank()
	if len(bank) == 0 {
		t.Fatal("GroundedEfficiencyBank is empty")
	}
	task := bank[0]
	sym := groundedSymbols[0]

	b := EngineBencher{MaxTicks: 40, NewEngine: groundedEngineFactory(ws, sym.Symbol)}
	b.Tasks = []HeldOutTask{task}
	rows := b.ProbeReplays("", 1)
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	r := rows[0]
	t.Logf("grounded probe: solved=%d/%d grounded=%d/%d goal=%q expect=%q",
		r.Solved, r.Replays, r.Grounded, r.Replays, task.Goal, task.Expect)

	// (1) GROUNDED-SUCCESS > 0: the episode crossed the watched seam and imported a NON-fabricated reality
	// observation from the fixture — the held-positive-utility condition the empty-oracle EfficiencyBank lacks.
	if r.Grounded == 0 {
		t.Fatal("W5-2c PREREQUISITE FAILED: the grounded efficiency goal did not import reality (grounded=0) " +
			"— the workspace/comprehender grounding path is not wired, so the cost re-measure would still be at zero grounding")
	}
	// (2) the oracle scores: the verbatim fixture value reached the answer or a thought (held-positive utility,
	// not the empty-oracle caveat). Robust scoring (scoreSolvedEngine) accepts the value in any thought.
	if r.Solved == 0 {
		t.Fatalf("the grounded goal imported reality but the value %q never scored (solved=0) — the oracle is not met at held grounding", task.Expect)
	}
}

// TestGroundedWarmArmRecallsAndShortCircuits proves the recurrence regime HOLDS on the grounded bank: the
// WARM arm (a state dir pre-seeded with GroundedEfficiencyMintGoal's skill, faithfully via SeedRecurringSkill)
// recalls that skill at synth step-0 (source "skill:<name>") on a grounding-bank goal that shares its goal
// key, while the COLD arm (no seed) synthesises. This is the W5-2b recall lever, re-proven on the GROUNDING
// goals — so the cost re-measure isolates the same lever, now at held-positive utility. The synth source is
// observed on a workspace-wired engine (the same shape the cost re-measure runs), so the recall path is
// asserted in the grounded configuration, not a stripped-down one.
func TestGroundedWarmArmRecallsAndShortCircuits(t *testing.T) {
	ws, err := GroundedEfficiencyWorkspace(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("GroundedEfficiencyWorkspace: %v", err)
	}
	warmDir := filepath.Join(t.TempDir(), "warm")
	if err := SeedRecurringSkill(warmDir, GroundedEfficiencyMintGoal); err != nil {
		t.Fatalf("SeedRecurringSkill(grounded mint goal): %v", err)
	}
	wantName, wantTriggers := convert.MintedSkillIdentity(GroundedEfficiencyMintGoal)
	if len(wantTriggers) < 2 {
		t.Fatalf("the grounded mint goal must yield a MULTI-word trigger set (got %v) — a single trigger inflates the recall win", wantTriggers)
	}

	bank := GroundedEfficiencyBank()
	task := bank[0].Goal
	sym := groundedSymbols[0].Symbol

	cold := observeSynthSource(t, groundedEngineFactory(ws, sym), "", task)
	if cold.recalled {
		t.Fatalf("COLD arm must NOT recall a skill (empty state) — but a skill_match fired (source %q)", cold.source)
	}
	if strings.HasPrefix(cold.source, "skill:") {
		t.Fatalf("COLD arm synth source = %q, must NOT be a skill recall (no skill seeded)", cold.source)
	}

	warm := observeSynthSource(t, groundedEngineFactory(ws, sym), warmDir, task)
	if !warm.recalled {
		t.Fatal("WARM arm must recall the seeded skill (skill_match) on the grounding goal — none fired (recall path broken)")
	}
	if warm.matched != wantName {
		t.Errorf("WARM arm matched skill %q, want the faithfully-seeded %q", warm.matched, wantName)
	}
	if want := "skill:" + wantName; warm.source != want {
		t.Errorf("WARM arm synth source = %q, want %q (step-0 recall short-circuits SynthesizeProgram)", warm.source, want)
	}
	// the lever IS the difference: warm recalls, cold synthesises. Equal sources => the seed did nothing.
	if cold.source == warm.source {
		t.Fatalf("WARM and COLD synth sources are identical (%q): the seed had no effect — the recall lever is dead on the grounding bank", warm.source)
	}
}

// synthObservation captures what the synthesiser did on one episode: whether it recalled a skill at step-0
// (a skill_match fired), the synth source label, and which skill it matched.
type synthObservation struct {
	recalled bool
	source   string
	matched  string
}

// observeSynthSource runs ONE fresh engine (built by `factory`) seeded from stateDir on the goal and reports
// whether the synthesiser recalled a skill at step-0 (subconscious.skill_match) + the subconscious.synthesize
// source label. The factory is workspace-wired so the recall is observed in the SAME configuration the cost
// re-measure uses (not a stripped-down one).
func observeSynthSource(t *testing.T, factory func(string) (*engine.Engine, error), stateDir, goal string) synthObservation {
	t.Helper()
	eng, err := factory(stateDir)
	if err != nil {
		t.Fatalf("engine factory: %v", err)
	}
	var obs synthObservation
	eng.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SkillMatch:
			obs.recalled = true
			if s, ok := ev.Data["skill"].(string); ok {
				obs.matched = s
			}
		case events.SubSynthesize:
			if s, ok := ev.Data["source"].(string); ok {
				obs.source = s
			}
		}
	})
	eng.SubmitDefault(goal)
	eng.Run(20)
	return obs
}
