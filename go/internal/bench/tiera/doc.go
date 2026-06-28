// Package tiera is the Tier-A atomic-item layer of the measuring stick
// (docs/internal/notes/measuring-stick-spec.md §5.2) — the JSONL item loader, the
// per-item artifact materializer, the deterministic normalizers + oracle
// evaluator, and the end-to-end RunItem that drives one item under one arm via
// internal/bench/runner and projects the result into a types.ItemResult.
//
// What it does (the §5.2 build-checklist bullets, made concrete):
//
//   - Loader. LoadItems / LoadItemsReader read a JSONL file (one types.TierAItem
//     per line) into memory, skipping blank lines, failing loud on a malformed
//     line (the line number is in the error so a bad bank is debuggable).
//
//   - Artifact materialization. Materialize writes an item's Artifact into a
//     fresh per-item temp sandbox directory (the file at its fixed in-sandbox
//     Path, the run-record, the fixture) so the grounding tools have something
//     REAL to read. It returns the sandbox root + a cleanup func; the caller
//     defers the cleanup. A zero-valued Artifact (a model-only probe) yields an
//     empty sandbox, never an error.
//
//   - Normalizers. The deterministic, unit-tested normalizer library
//     (identifier-canonical, number, set, ledger-status, passthrough) applied
//     before comparison (spec §3.1, §4.1: gold-fixture-verified to 100%). Each
//     normalizer is pure and total; an unknown name falls back to the
//     whitespace/case passthrough so a typo degrades gracefully rather than
//     crashing a bank.
//
//   - Oracle evaluator. Evaluate is a tagged dispatch on types.OracleKind:
//     exact-match, numeric-tolerance, set-membership, ledger-status, and
//     event-presence. The event-presence oracle (and a TraceRequirement AND'd
//     onto an answer oracle) reads the captured event trace through the
//     event-key matcher (kind or "kind.field=value", e.g.
//     "critic.decision=BACKTRACK", "action.observation.ok=false"). Rubric is the
//     non-deterministic minority and is NOT scored here (it routes through the
//     judge pipeline, spec §5.4) — Evaluate reports it Unsupported so a bank that
//     leans on it fails loud rather than silently passing.
//
//   - Pass rule. The item-level pass is
//     oracle(answer, artifact) AND (if a trace_oracle is set) the runner's
//     isolation predicate holds — spec §1.4: a harness answer that is right
//     WITHOUT genuine mechanism use is a mechanism-bypass, excluded from the lift
//     numerator. The answer-oracle verdict (OracleVerdict) and the isolation
//     verdict (IsolationResult) are recorded separately on the ItemResult so the
//     stats layer can read each.
//
// Determinism. The materializer uses the OS temp dir + a deterministic per-item
// name; the run path uses internal/bench/runner with the offline test double in
// tests (TestFactory), so the whole loader → materialize → oracle pipeline is
// reproducible and never touches the network.
package tiera
