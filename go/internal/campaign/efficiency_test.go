package campaign

// efficiency_test.go — W5-2b option (D): the OFFLINE recall-path proof. On the deterministic test double
// the completion-token magnitude is unobservable (no real usage → Completion=0), so these tests prove the
// COGNITION instead: the WARM arm (seeded skill) recalls it at synth step-0 and SHORT-CIRCUITS the LLM
// SynthesizeProgram call (Source "skill:<name>"); the COLD arm (no skill) does NOT (it synthesises). The
// completion DROP itself is the claude follow-up — see the package doc. Deterministic + mutation-sensitive:
// remove the seed and warm == cold (no skill_match, synth source not a skill).

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// synthSourceForGoal runs ONE fresh test-double engine seeded from stateDir on the goal and returns whether
// the synthesiser RECALLED a skill at step-0 (a subconscious.skill_match event fired) and the source label
// the subconscious.synthesize event reported. This is the cognition probe: a skill recall is a "skill:<name>"
// source + a skill_match event; a cold synthesis is some other source with NO skill_match.
func synthSourceForGoal(t *testing.T, stateDir, goal string) (recalled bool, source, matchedSkill string) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	if stateDir != "" {
		st, err := persist.NewJSONLStore(stateDir)
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		cfg.Store = st
	}
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SkillMatch:
			recalled = true
			if s, ok := ev.Data["skill"].(string); ok {
				matchedSkill = s
			}
		case events.SubSynthesize:
			if s, ok := ev.Data["source"].(string); ok {
				source = s
			}
		}
	})
	eng.SubmitDefault(goal)
	eng.Run(20)
	return recalled, source, matchedSkill
}

// TestWarmArmRecallsSeededSkillShortCircuitsSynthesis is the load-bearing cognitive-property test: the WARM
// arm (a state dir pre-seeded with the recurring goal's skill, faithfully via SeedRecurringSkill) recalls
// that skill at synth step-0 (source "skill:<name>") on a recurrence-regime goal, while the COLD arm (no
// seed) synthesises a fresh program (NOT a skill source, no skill_match). Mutation-sensitive: skip the seed
// and the warm arm collapses to the cold arm.
func TestWarmArmRecallsSeededSkillShortCircuitsSynthesis(t *testing.T) {
	warmDir := filepath.Join(t.TempDir(), "warm")
	if err := SeedRecurringSkill(warmDir, EfficiencyMintGoal); err != nil {
		t.Fatalf("SeedRecurringSkill: %v", err)
	}
	// the seeded skill's faithful identity (the exact name convert.Consolidate would mint).
	wantName, _ := convert.MintedSkillIdentity(EfficiencyMintGoal)

	task := EfficiencyBank()[0].Goal // a same-goal-key recurrence task

	// COLD arm: no skill present → Synthesize runs the toolmaker (SynthesizeProgram). NOT a skill source,
	// no skill_match event.
	coldRecalled, coldSource, _ := synthSourceForGoal(t, "", task)
	if coldRecalled {
		t.Fatalf("COLD arm must NOT recall a skill (empty state) — but a skill_match fired")
	}
	if strings.HasPrefix(coldSource, "skill:") {
		t.Fatalf("COLD arm synth source = %q, must NOT be a skill recall (no skill seeded)", coldSource)
	}

	// WARM arm: the seeded skill is reloaded → Synthesize recalls it at step-0 (skill:<name>), short-
	// circuiting SynthesizeProgram.
	warmRecalled, warmSource, warmSkill := synthSourceForGoal(t, warmDir, task)
	if !warmRecalled {
		t.Fatalf("WARM arm must recall the seeded skill (skill_match) — none fired (recall path broken)")
	}
	if warmSkill != wantName {
		t.Errorf("WARM arm matched skill %q, want the faithfully-seeded %q", warmSkill, wantName)
	}
	if want := "skill:" + wantName; warmSource != want {
		t.Errorf("WARM arm synth source = %q, want %q (step-0 recall short-circuits SynthesizeProgram)", warmSource, want)
	}

	// the lever IS the difference: warm recalls, cold synthesises. If these are equal the measurement is a
	// no-op (the seed did nothing) — guard against a silent regression.
	if coldSource == warmSource {
		t.Fatalf("WARM and COLD synth sources are identical (%q): the seed had no effect — the recall lever is dead", warmSource)
	}
}

// TestWarmVsColdPairsAndIsOfflineZeroCost drives the end-to-end WarmVsCold harness on the test double:
// every task pairs a cold and a warm row over K replays, the completion vectors are retained (length K),
// and offline the cost is a constant 0 (the double emits no usage) — so the per-task CompletionDelta is 0
// here, and the magnitude is the claude follow-up. The WIRING (vectors, pairing, replays) is what this
// asserts; TestWarmArmRecallsSeededSkillShortCircuitsSynthesis proves the recall path that WOULD drop it.
func TestWarmVsColdPairsAndIsOfflineZeroCost(t *testing.T) {
	const k = 2
	warmDir := filepath.Join(t.TempDir(), "warm")
	b := EngineBencher{MaxTicks: 20, NewEngine: testEngineFactory}
	tasks := EfficiencyBank()

	rows, err := b.WarmVsCold(tasks, EfficiencyMintGoal, "", warmDir, k)
	if err != nil {
		t.Fatalf("WarmVsCold: %v", err)
	}
	if len(rows) != len(tasks) {
		t.Fatalf("rows = %d, want %d (one per task, order preserved)", len(rows), len(tasks))
	}
	for i, r := range rows {
		if r.Goal != tasks[i].Goal {
			t.Errorf("row %d goal = %q, want %q (order preserved)", i, r.Goal, tasks[i].Goal)
		}
		if r.Cold.Replays != k || r.Warm.Replays != k {
			t.Errorf("row %d replays cold=%d warm=%d, want %d each", i, r.Cold.Replays, r.Warm.Replays, k)
		}
		if len(r.Cold.Completions) != k || len(r.Warm.Completions) != k {
			t.Errorf("row %d completion vectors cold=%d warm=%d, want %d each (one sample/replay)",
				i, len(r.Cold.Completions), len(r.Warm.Completions), k)
		}
		// offline (test double): no real usage → both arms cost 0 → delta 0. The magnitude is the claude
		// follow-up; this asserts the cost plumbing is wired, not the win.
		if r.Cold.Completion != 0 || r.Warm.Completion != 0 {
			t.Errorf("row %d offline cost must be 0 (no real usage), got cold=%d warm=%d",
				i, r.Cold.Completion, r.Warm.Completion)
		}
		if r.CompletionDelta() != 0 {
			t.Errorf("row %d offline CompletionDelta = %v, want 0 (the double emits no usage)", i, r.CompletionDelta())
		}
	}
}

// TestSeedRecurringSkillIsFaithful asserts the seed identity matches convert.Consolidate's mint EXACTLY
// (the red-team load-bearing fix): the persisted skill's name + the FULL Fields(goalKey) trigger set, not
// a hand-picked single trigger. It reads the record straight back from the store, so a drift in the seed
// path (wrong name, a truncated trigger set) fails here.
func TestSeedRecurringSkillIsFaithful(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seed")
	if err := SeedRecurringSkill(dir, EfficiencyMintGoal); err != nil {
		t.Fatalf("SeedRecurringSkill: %v", err)
	}
	wantName, wantTriggers := convert.MintedSkillIdentity(EfficiencyMintGoal)
	// the faithful trigger set is the WHOLE goal key, not one cherry-picked word.
	if len(wantTriggers) < 2 {
		t.Fatalf("the recurrence-regime mint goal must yield a MULTI-word trigger set (got %v) — a single trigger inflates the recall win", wantTriggers)
	}

	st, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	snap, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var got *persist.SkillRecord
	for i := range snap.Skills {
		if snap.Skills[i].Name == wantName {
			got = &snap.Skills[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("seeded skill %q not found in the store (names: %v)", wantName, skillNames(snap.Skills))
	}
	if got.Tier != "" {
		t.Errorf("seeded skill Tier = %q, want \"\" (the minted-skill tier convert.Consolidate uses)", got.Tier)
	}
	if !got.Meta.Grounded || got.Meta.Status != persist.StatusActive {
		t.Errorf("seeded skill must be active+grounded to be re-seeded into the live library, got status=%q grounded=%v",
			got.Meta.Status, got.Meta.Grounded)
	}
	if strings.Join(got.Triggers, " ") != strings.Join(wantTriggers, " ") {
		t.Errorf("seeded triggers = %v, want the full faithful set %v (no hand-picked single trigger)", got.Triggers, wantTriggers)
	}
	if got.Body == nil {
		t.Errorf("seeded skill body is nil — the recalled program would be empty")
	}
}

// TestSeedRecurringSkillRejectsShapelessGoal guards the no-op trap: a goal that hits no RecognizeShape
// family mints nothing, so seeding it would silently make the warm arm equal the cold arm — that must be a
// loud error, not a quiet no-op.
func TestSeedRecurringSkillRejectsShapelessGoal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "noshape")
	// a bare lookup question hits no shape family (RecognizeShape returns false) — see synth.go.
	if err := SeedRecurringSkill(dir, "blue"); err == nil {
		t.Fatalf("a shapeless mint goal must error (it would mint no skill), got nil")
	}
}

func skillNames(rs []persist.SkillRecord) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}
