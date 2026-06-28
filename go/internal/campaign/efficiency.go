package campaign

// efficiency.go — W5-2b option (D): the WARM-vs-COLD efficiency-measurement mode for the probe.
//
// THE LEVER (decision-arbiter + red-team verified). The W5 efficiency win is the synthesiser's step-0
// `library.Match(goal)` returning a minted SKILL (Source "skill:<name>"), which SHORT-CIRCUITS the LLM
// `SynthesizeProgram` toolmaker call (cognition/synth.go:222) — so on a RECURRING goal the decode
// (completion) tokens drop. The autonomous mint that creates that skill is currently blocked (its
// recurrence counter is not persisted across episodes), so we cannot yet observe a self-minted recall.
// Option (D) ISOLATES the recall lever from the (blocked) mint: it PRE-SEEDS the recurring goal's skill
// FAITHFULLY (exactly as convert.Consolidate would mint it) and measures cold (no skill → Synthesize runs
// SynthesizeProgram) vs warm (skill present → step-0 recall) completion-tokens.
//
// WHAT THIS MEASURES — HONEST SCOPE. This is SAME-GOAL recall: the warm-arm goals SHARE the first-three
// alpha words (the goal key, convert.go:229) with the seeded mint goal, so the lexical trigger match fires.
// It does NOT measure PARAPHRASE generalization (a goal that means the same but shares no trigger words) —
// that needs the embedder seam (cognition/skills.go:136, SetEmbedder), which the offline test double does
// not wire. So a green offline test here proves the RECALL PATH short-circuits synthesis; the agentic
// completion-token DROP itself is UNMEASURED until the claude follow-up (the offline double emits no real
// usage → Completion=0). See EfficiencyBank for the recurrence-regime construction.

import (
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// EfficiencyResult is the paired warm-vs-cold outcome for ONE recurring task over K replays: the cold arm
// (no seeded skill → Synthesize calls SynthesizeProgram) and the warm arm (seeded skill → step-0 recall,
// SynthesizeProgram skipped). The two ProbeStability rows carry the per-replay completion-token vector
// (the cache-immune cost samples), so the warm-minus-cold completion delta IS the W5 efficiency signal —
// negative (warm cheaper) on a real substrate, zero offline (the test double emits no usage).
type EfficiencyResult struct {
	Goal string
	Cold ProbeStability // empty stateDir: Synthesize runs the LLM toolmaker (SynthesizeProgram)
	Warm ProbeStability // seeded stateDir: Synthesize recalls the skill at step-0, skips SynthesizeProgram
}

// CompletionDelta is warm-minus-cold mean completion tokens — the per-task efficiency signal. NEGATIVE
// means the warm (recall) arm decoded fewer tokens (the W5 win); 0 offline (no real usage on the double).
func (r EfficiencyResult) CompletionDelta() float64 {
	return r.Warm.MeanCompletion() - r.Cold.MeanCompletion()
}

// SeedRecurringSkill writes the recurring goal's minted skill into stateDir FAITHFULLY — exactly as
// convert.Consolidate's trace->skill mint produces it (the red-team load-bearing fix): the body is the
// SAME program the synthesiser would have built for mintGoal (derived from cognition.RecognizeShape, the
// shape the toolmaker/heuristic produces), and the identity is convert.MintedSkillIdentity(mintGoal) — the
// full Fields(goalKey) trigger set, NOT a hand-picked single trigger (which would inflate the recall win,
// the way TestMintedSkillIsRecalled cheats). Tier is "" and Synthesized is true — the exact constants the
// engine's warm-reload Mint path consumes (engine/persist.go:69, skills.Mint(name, triggers, body, "", …)),
// so the reloaded skill is recallable by SkillRegistry.Match (which keys on Synthesized, skills.go:221).
//
// Returns an error if mintGoal hits NO RecognizeShape family (a goal with no shape mints nothing — the
// caller must use a goal in the recurrence regime, see EfficiencyBank), or if persistence fails.
func SeedRecurringSkill(stateDir, mintGoal string) error {
	prog, ok := cognition.RecognizeShape(mintGoal, nil)
	if !ok {
		return errNoShape(mintGoal)
	}
	name, triggers := convert.MintedSkillIdentity(mintGoal)

	st, err := persist.NewJSONLStore(stateDir)
	if err != nil {
		return err
	}
	rec := persist.SkillRecord{
		Meta: persist.Meta{
			Status:   persist.StatusActive, // only active skills are re-seeded into the live library (persist.go)
			Grounded: true,                 // a trace->skill mint promotes a recurred, value-cleared program
		},
		Name:     name,
		Tier:     "",       // the engine's Mint passes tier="" for a minted skill (skills.go:210 note)
		Triggers: triggers, // the FULL goal-key field set (faithful: no hand-picked single trigger)
		// Body is the WHOLE-PROGRAM ToDict envelope ({goal, synthesized, rationale, root}) — the SAME shape
		// the real mint path writes (engine/persist.go saves sk.Body.ToDict()) and the engine's warm-reload
		// now parses via cognition.ProgramFromDict (the W5-2b save/load round-trip fix). Writing the bare
		// root-node dict here (the old NodeFromDict load contract) would now fail to reload ("unknown
		// program node kind: None"), so the seed must use ToDict to stay faithful to the real mint.
		Body: prog.ToDict(),
	}
	if err := st.SaveSkill(rec); err != nil {
		return err
	}
	return st.Flush() // durable write: the warm-arm engine reloads it from this stateDir
}

// WarmVsCold runs each recurring task K times on BOTH arms and returns the paired completion-cost rows.
// COLD arm = the empty baseline state (no skill; Synthesize runs SynthesizeProgram). WARM arm = a fresh
// pre-seeded state dir per task, written by SeedRecurringSkill(mintGoal) so the recurring goal's skill is
// present and recalled at step-0. The mintGoal MUST be in the same recurrence regime as the tasks (share
// the goal key) — EfficiencyBank constructs both halves consistently. coldStateDir is normally "" (the
// default baseline state); warmSeedDir is a writable directory the seed is written into (one per call).
//
// The per-task completion vectors (ProbeStability.Completions) are the cost samples; offline they are all
// zero (the test double emits no usage), so this proves the RECALL PATH wiring, not the token magnitude —
// the magnitude is the claude follow-up. Reuses ProbeReplays for both arms (the W5-0b cost instrument),
// so the cost accounting is the single shared addLLMCost source, not a fork.
func (b EngineBencher) WarmVsCold(tasks []HeldOutTask, mintGoal, coldStateDir, warmSeedDir string, replays int) ([]EfficiencyResult, error) {
	if err := SeedRecurringSkill(warmSeedDir, mintGoal); err != nil {
		return nil, err
	}
	cb := b
	cb.Tasks = tasks
	cold := cb.ProbeReplays(coldStateDir, replays)
	warm := cb.ProbeReplays(warmSeedDir, replays)

	out := make([]EfficiencyResult, len(tasks))
	for i := range tasks {
		out[i] = EfficiencyResult{Goal: tasks[i].Goal, Cold: cold[i], Warm: warm[i]}
	}
	return out, nil
}

// errNoShape is the "this goal mints nothing" error — a goal that hits no RecognizeShape family would seed
// no skill, so the warm arm would equal the cold arm (a silent no-op the caller must not get by accident).
type errNoShape string

func (e errNoShape) Error() string {
	return "campaign: mint goal " + string(e) + " hits no RecognizeShape family — it would mint no skill (use a goal in a recognised shape family; see EfficiencyBank)"
}

// EfficiencyMintGoal is the canonical recurring goal the warm arm pre-seeds. It hits the OPTIMIZE shape
// family (RecognizeShape, synth.go:135 — "optimize") so a skill is actually minted, and its goal key
// (first three alpha CONTENT words, convert.goalKey) is "optimize database query" — the trigger set the
// warm-arm tasks share ("the" is dropped by the stopword filter, the W5-2b over-fire fix; "query" is the
// third content word). (Verified: goalKey("optimize the database query latency") == "optimize database
// query".)
const EfficiencyMintGoal = "optimize the database query latency"

// EfficiencyBank builds the SAME-GOAL recurrence bank (red-team fixes #2,#3). Every task (a) hits a
// RecognizeShape family so Synthesize actually runs the toolmaker (and would mint), and (b) shares the
// goal-key content words ("optimize", "database") with EfficiencyMintGoal — so the seeded skill's
// triggers fire on the warm arm. This is the REAL same-goal recurrence regime: distinct surface goals
// that the harness reduces to the same goal key (the condition under which the autonomous mint WOULD
// fire). It deliberately does NOT include a paraphrase that shares meaning but no trigger words — that
// would need the embedder seam and is out of scope for this offline recall proof.
//
// The Expect oracles are intentionally empty (grounded-success scoring) — the efficiency axis is about
// HOW the program was sourced (recall vs synthesise), not the answer; an answer oracle would only add
// substrate-dependent flakiness to a cost measurement.
func EfficiencyBank() []HeldOutTask {
	return []HeldOutTask{
		{Goal: "optimize the database query latency under load"},
		{Goal: "optimize the database write throughput for batch inserts"},
		{Goal: "optimize the database index rebuild time"},
	}
}
