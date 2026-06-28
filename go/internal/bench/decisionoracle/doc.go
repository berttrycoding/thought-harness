// Package decisionoracle is the A2 DECISION/SHIP oracle — the deterministic,
// ground-truth measuring stick for the Deliberator's and Verifier's OUTPUT
// CORRECTNESS (docs/internal/notes/2026-06-16-registry-target-spec.md §1 rows
// Deliberator/Verifier, §3 "decision quality; correct accept/refuse"; §4 build-order
// item 2 "build their decision/ship oracles").
//
// WHY THIS IS A DISTINCT ORACLE (the scope A2 fills). The harness already has two
// measuring sticks for these two workers, and NEITHER scores whether the worker
// reached the RIGHT answer:
//
//   - internal/bench/synthfidelity (A5) scores the synthesised PROGRAM STRUCTURE —
//     does the synthesiser produce par(hypothesize)→rank for a deliberation, or
//     decompose→validate@reality→gate for a verification? That is STRUCTURE-fidelity:
//     "did it build the right workflow shape?", not "did the workflow reach the right
//     verdict?".
//   - internal/campaign/cogprobe (the cognition probe) scores whether a FACULTY FIRED
//     — did a BRANCH decision / a grounding ACT happen at all? That is a fire-RATE:
//     "did the branch faculty engage?", not "did the branch pick the better option?".
//
// The DECISION/SHIP oracle in this package closes the last gap: given a fixture whose
// CORRECT answer is known by construction, score the worker's VERDICT against that
// ground truth:
//
//   - Deliberator: in a trade-off with a deterministically-computed better option,
//     did the worker PICK that option? (the decision oracle)
//   - Verifier: for a claim with a known truth-value and the evidence that settles it,
//     did the worker correctly ACCEPT a true claim and REFUSE a false one — and, when
//     the evidence is genuinely unavailable, HONESTLY refuse rather than confabulate?
//     (the ship oracle)
//
// SOUNDNESS DISCIPLINE (mirrors A5 / the A5 oracle-doctor contract). The SCORER has no
// model in it — it compares an extracted verdict to a ground truth the fixture carries
// in machine-checkable form, so it is deterministic and offline-vettable:
//
//   - the Deliberator ground truth is COMPUTED, not asserted: each option carries
//     per-criterion scores and the fixture carries the criterion weights, so the
//     better option is a pure function the test re-derives (an authored "winner" that
//     disagrees with the computed one is a bank error, caught loud);
//   - every fixture carries a GOOD verdict (correct pick / correct accept-refuse, with
//     reasoning that cites the discriminating evidence) and a BAD verdict (the wrong
//     pick / the false-accept of a bad claim / the false-refuse of a true one) — the
//     fail-discriminating control: GOOD must score >= the threshold, BAD must score
//     below it, with a margin (TestDiscrimination);
//   - a MISSING verdict (the worker declined / produced nothing extractable) is a HARD
//     fail, never a silent pass;
//   - a fixture that constrains nothing is a bank error, never a vacuous pass.
//
// A NEW oracle is NOT self-blessed: this package's discrimination control is the
// internal vet, but the SIGNAL gate before A2 can count a worker as "passing" is a
// bench-oracle-doctor pass on this oracle (the same gate A5 cleared). The live
// signal run — driving the REAL Deliberator/Verifier sub-agents on --backend claude
// and scoring their actual verdicts — is the A2 remainder slice, not this package.
package decisionoracle
