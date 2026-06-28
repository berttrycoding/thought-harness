// Package synthfidelity is the A5 agent-synthesis-fidelity benchmark (Track A,
// docs/internal/notes/2026-06-16-registry-target-spec.md §1, §3 stretch family): it
// measures whether the SYNTHESISER produces a workflow PROGRAM (and the sub-agent
// tool-scope it implies) that FAITHFULLY matches what a goal requires — the right
// operator families, the right control-flow STRUCTURE, the right tool-scope — vs a
// plausible-but-wrong one.
//
// # Why a separate mechanism (not a tiera arm)
//
// The six measuring-stick mechanisms (internal/bench/tiera) score the OUTPUT of a
// full bare-vs-harness engine run on a task. A5 is a different probe: it scores the
// synthesiser's CONSTRUCTION directly — the Program tree cognition.Synthesize emits
// for a goal — against an expected structural spec. There is no "answer" and no
// arm to compare; the unit under test is the decomposition, not the task outcome.
// So A5 lives in its own package with its own fixture + oracle, sharing the
// benchtypes vocabulary (the Mechanism tag) only at the registration boundary.
//
// # Why a DETERMINISTIC / STRUCTURAL oracle (offline-vettable, no claude)
//
// The target-spec's load-bearing lesson is "structure-forces-faculty" (§1.1): a
// minted capability lifts a faculty BECAUSE OF its Program structure, not its text
// — a par node FORCES a branch, a validate@reality step FORCES an act. The target
// set is therefore specified AS STRUCTURES (the "Workflow STRUCTURE" column, §1)
// and the synthesiser is judged on whether it PRODUCES those structures. That is
// exactly what a deterministic structural oracle scores: operator presence,
// operator family/move coverage, control-flow shape (seq/par/loop), and the implied
// tool-scope. None of that needs an LLM judge, so A5 is offline-closable.
//
// The synthesiser is driven OFFLINE and DETERMINISTICALLY by Drive(): a
// backends.TestBackend wired to cognition.RecognizeShapeDict produces the SAME
// deterministic shape a live model would for the worked cases, with no network and
// no API key. The fixtures' expected properties are derived from the operator
// catalog + the seeded skill/agent specs (the gold decompositions), so they are
// themselves offline-vettable by bench-oracle-doctor.
//
// # The fail-discriminating control
//
// An oracle that cannot tell a good synthesis from a bad one is worthless. So every
// fixture carries a GoodProgram (a faithful synthesis — the gold structure) AND a
// BadProgram (a plausible-but-wrong synthesis — right vocabulary, wrong structure:
// a branch flattened to a sequence, a wrong operator family, a missing act). The
// discrimination test (oracle_test.go) asserts the oracle scores GoodProgram high
// (a pass) and BadProgram low (a fail) on the SAME fixture. The bench also drives
// the REAL synthesiser per fixture and records its fidelity — a miss there is a
// precise, rankable capability gap (the §3 stretch intent), not a bug in the oracle.
package synthfidelity
