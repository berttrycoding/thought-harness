package synthfidelity

import (
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// Result is one fixture's full A5 outcome: the verdict on the REAL synthesiser's
// output (the headline — the synthesiser's actual fidelity for this goal), plus a
// drift flag against the bank's authored SynthesiserCovers expectation. ID/Worker
// carry through for the report rollup.
type Result struct {
	// ID / Worker identify the fixture.
	ID     string
	Worker string
	// Goal is the request the synthesiser was driven on.
	Goal string
	// Synthesised is true when cognition.Synthesize produced a program for the goal
	// (false = "no workflow shape" — the synthesiser declined, a capability gap).
	Synthesised bool
	// Source is the synthesis provenance ("skill:<name>" | "heuristic" | "llm" |
	// "canonical"), recorded so a faithful program via a seeded skill vs the
	// heuristic fallback is distinguishable in the report.
	Source string
	// Shape is the one-line control-flow signature of the synthesised program (or
	// "<no-shape>" when the synthesiser declined).
	Shape string
	// Verdict is the structural-fidelity score of the synthesised program against
	// the fixture's Expect. On "no shape" it is the absent-program fail.
	Verdict Verdict
	// Drift is true when the measured outcome contradicts the bank's authored
	// SynthesiserCovers expectation (covers-but-failed OR gap-but-passed) — the
	// signal that the synthesiser's capability moved and the bank should be revisited.
	Drift bool
}

// Drive runs the REAL synthesiser (cognition.Synthesize) on the fixture's goal,
// OFFLINE and DETERMINISTICALLY, then scores its actual output with the oracle. The
// synthesiser is driven with:
//   - a fresh seeded OperatorRegistry (the gold operator catalog),
//   - a fresh seeded SkillRegistry (the gold skill/agent decompositions),
//   - a backends.TestBackend wired to cognition.RecognizeShapeDict (the offline
//     deterministic toolmaker — the SAME shape a live model would write for the
//     worked cases, with no network and no API key).
//
// This is what makes A5 offline-closable: no claude, no LM Studio, byte-identical
// across runs. The fixture's Good/Bad programs are scored separately by the
// discrimination test; Drive scores the synthesiser's OWN construction so a miss is
// a precise, rankable capability gap (target-spec §3 stretch).
func Drive(fx Fixture, w Weights) Result {
	cat := cognition.NewOperatorRegistry()
	lib := cognition.NewSkillRegistry(true)
	be := &backends.TestBackend{ShapeRecognizer: cognition.RecognizeShapeDict}

	res := Result{ID: fx.ID, Worker: fx.Worker, Goal: fx.Goal}

	synth, ok := cognition.Synthesize(fx.Goal, nil, cat, be, nil, lib)
	if !ok || synth == nil {
		res.Synthesised = false
		res.Shape = "<no-shape>"
		res.Source = "<none>"
		// No program produced -> the absent-program structural fail (score 0).
		res.Verdict = ScoreProgram(nil, fx, cat, w)
		res.Drift = fx.SynthesiserCovers // authored "covers" but produced nothing -> drift
		return res
	}

	res.Synthesised = true
	res.Source = synth.Source
	res.Shape = synth.Program.Shape()
	prog := ProgramShape(synth.Program.ToDict()["root"].(map[string]any))
	res.Verdict = ScoreProgram(prog, fx, cat, w)
	// Drift: the bank authored covers=true but the synthesiser failed the fidelity
	// bar, or authored covers=false (a known gap) but it passed. Either way the bank
	// expectation and the measured capability disagree.
	res.Drift = res.Verdict.Pass != fx.SynthesiserCovers
	return res
}

// DriveAll runs Drive over every fixture and returns the per-fixture results in
// input order (deterministic). The bench/report layer rolls these up per worker.
func DriveAll(fixtures []Fixture, w Weights) []Result {
	out := make([]Result, 0, len(fixtures))
	for _, fx := range fixtures {
		out = append(out, Drive(fx, w))
	}
	return out
}

// programShapeOf is the test/helper adapter: it turns a built cognition.Program into
// the JSON-shaped ProgramShape the oracle scores, by taking the serialised "root"
// node dict (the same form a fixture's Good/Bad program carries). Exposed so a test
// can score a Program it built with the program.go DSL without hand-writing the dict.
func programShapeOf(p cognition.Program) ProgramShape {
	return ProgramShape(p.ToDict()["root"].(map[string]any))
}
