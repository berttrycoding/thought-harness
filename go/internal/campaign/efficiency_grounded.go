package campaign

// efficiency_grounded.go — W5-2c (offline half): the GROUNDED efficiency bank so the W5-2b cost win can be
// re-measured AT HELD-POSITIVE UTILITY (not the zero-grounding caveat).
//
// THE GAP W5-2c CLOSES. W5-2b proved WARM (a trace->skill recall short-circuits SynthesizeProgram) is ~72%
// cheaper in completion-tokens — but on the existing EfficiencyBank, whose goals ("optimize the database
// query latency …") have EMPTY oracles and NEVER import reality (0/2 grounded-success on both arms). The
// rigorous W5 gate is "completion-tokens fall AT HELD POSITIVE UTILITY" — so a cost win on goals neither arm
// solves is not yet the win. W5-2c supplies goals that ACTUALLY GROUND (both arms import reality + carry a
// real oracle) AND stay in the recurrence regime (the warm-arm goals share the goal key with the seeded mint
// goal so library.Match recalls the skill), so the SAME WarmVsCold instrument now measures cost at a held
// grounded-success > 0.
//
// THE GROUNDING SHAPE (reused, not invented). The proven grounding mechanism is the A1 grounded-investigator
// probe: "investigate the source files in this codebase and report the exact numeric value assigned to
// <Symbol>", grounded via --workspace (a real executor reads/searches a source file carrying the value, the
// observation is non-fabricated => it grounds, and the verbatim value lands in a thought => the Expect oracle
// scores). See engine/a1_searchread_repro_test.go for the search->read handoff this depends on.
//
// THE TWO CONSTRAINTS, SATISFIED TOGETHER.
//  1. GOAL KEY (recurrence regime): every bank goal AND the mint goal reduce to the goal key "investigate
//     source files" (convert.goalKey: first three alpha CONTENT words >2 chars, stopwords dropped). So the
//     seeded skill's triggers [investigate source files] fire on every warm-arm goal — the SAME same-goal
//     recurrence discipline as EfficiencyBank, now over grounding goals.
//  2. SHAPE FAMILY (mints + synthesises): "investigate" hits the ANALYSIS family in cognition.RecognizeShape
//     (synth.go:150), so on the COLD arm Synthesize runs the toolmaker (decode tokens spent) and the mint goal
//     yields a real program to seed — without a shape there would be no skill to recall (SeedRecurringSkill
//     errors), making the warm arm a silent no-op.
//
// WHY THE WORKSPACE IS THE CALLER'S JOB. WarmVsCold runs whatever engine NewEngine builds; the grounding only
// happens when that factory wires cfg.Workspace (the real executor) AND the backend is a RealityComprehender
// that names the read/search target (engine/knowledge.go SourceReality). The bare TestBackend is NOT a
// RealityComprehender, so the OFFLINE suite proves grounding with a scripted comprehender double (see
// efficiency_grounded_test.go) and the magnitude/held-positive win is the claude follow-up (the bridge wires
// --workspace + a real model that comprehends). GroundedEfficiencyWorkspace writes the fixture so any caller
// — the offline test OR the claude re-run — grounds against the same source files.

import (
	"os"
	"path/filepath"
)

// GroundedEfficiencyMintGoal is the canonical recurring goal the warm arm pre-seeds for the GROUNDED bank.
// It (a) hits the ANALYSIS shape family ("investigate", RecognizeShape synth.go:150) so a real program is
// minted, and (b) its goal key (first three alpha CONTENT words, convert.goalKey) is "investigate source
// files" — the trigger set the warm-arm bank goals share. The trailing symbol is one of the bank symbols so
// the mint goal is itself a well-formed grounding goal (the recurrence is honest: the mint goal is the same
// shape as the tasks, only the symbol differs). (Verified: goalKey reduces this to "investigate source
// files"; RecognizeShape returns the analysis program.)
const GroundedEfficiencyMintGoal = "investigate the source files in this codebase and report the exact numeric value assigned to AdmitAmbiguityThreshold"

// groundedSymbol pairs a source-symbol with the value the fixture assigns it (the Expect oracle). Each is a
// distinct grounding goal sharing the "investigate source files" goal key; the value is what a grounded
// episode must surface (in the answer or any thought) to score solved.
type groundedSymbol struct {
	Symbol string // the const identifier the investigator searches for
	Value  string // the literal value assigned in the fixture — the answer oracle (Expect)
	File   string // the fixture filename the const lives in (one symbol per file, so search names it cleanly)
}

// groundedSymbols are the W5-2c bank symbols. Distinct values (no two share a substring that would let an
// oracle pass on the wrong file) and plain `const Name = Value` declarations the search->read handoff lands
// verbatim. The mint goal targets the first (AdmitAmbiguityThreshold) so it is itself a bank-shaped goal.
var groundedSymbols = []groundedSymbol{
	{Symbol: "AdmitAmbiguityThreshold", Value: "0.4271", File: "admit.go"},
	{Symbol: "MaxParallelFanWidth", Value: "0.8163", File: "fanout.go"},
	{Symbol: "QuiescenceDecayMargin", Value: "0.1374", File: "quiescence.go"},
}

// GroundedEfficiencyBank builds the GROUNDED same-goal recurrence bank: each task is an A1-shaped grounded-
// investigator goal that (a) shares the goal key "investigate source files" with GroundedEfficiencyMintGoal
// (so the seeded skill's triggers fire on the warm arm), (b) hits the ANALYSIS shape (so Synthesize runs on
// the cold arm and the mint goal yields a skill), and (c) carries a REAL Expect oracle = the value the
// fixture assigns the symbol (so a grounded episode is scored at held-positive utility, not the empty-oracle
// caveat). Drive it through an engine wired with GroundedEfficiencyWorkspace (the real executor) so the
// goals actually ground.
func GroundedEfficiencyBank() []HeldOutTask {
	out := make([]HeldOutTask, 0, len(groundedSymbols))
	for _, s := range groundedSymbols {
		out = append(out, HeldOutTask{
			Goal:   "investigate the source files in this codebase and report the exact numeric value assigned to " + s.Symbol,
			Expect: s.Value, // the answer oracle — the held-positive-utility check (not an empty oracle)
		})
	}
	return out
}

// GroundedEfficiencyWorkspace writes the fixture source files the bank grounds against into dir: one plain
// `const <Symbol> = <Value>` per file, so the search->read handoff names the file (search hit) then reads the
// verbatim value (read_file) — the exact A1 grounding path. Returns the dir for convenience. Idempotent: it
// (re)writes every fixture file. The SAME fixture is used by the offline test and the claude re-run, so both
// arms ground against identical reality (the cost comparison is apples-to-apples).
func GroundedEfficiencyWorkspace(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, s := range groundedSymbols {
		body := "package config\n\n// " + s.Symbol + " is a W5-2c grounding fixture constant.\nconst " + s.Symbol + " = " + s.Value + "\n"
		if err := os.WriteFile(filepath.Join(dir, s.File), []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}
