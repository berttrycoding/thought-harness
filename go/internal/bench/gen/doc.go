// Package gen is the generator → judge pipeline of the measuring stick
// (docs/internal/notes/measuring-stick-spec.md §5.4): it lays out and round-trips the
// JSONL benchmark banks, validates each bank against the §2.3 gap-coverage and
// §6.0 domain-mix RULES, generates new Tier-A items / Tier-B scenarios seeded
// from authored gold (few-shot), and wraps the residual rubric-clause judge.
//
// The pipeline has four parts:
//
//   - Bank layout + IO (bank.go). A bank is one JSONL file per mechanism per
//     tier, named "<mechanism>-tier<a|b>.jsonl" under a banks root
//     (internal/bench/banks/pilot/ for the authored pilot seeds). SaveBankA /
//     LoadBankA round-trip []types.TierAItem; SaveBankB / LoadBankB round-trip
//     []types.TierBScenario. One item/scenario == one JSONL line, matching the
//     wire format the tiera/tierb loaders already read (spec §5.2, §5.3).
//
//   - Gap-coverage + domain-mix validators (check.go). CheckBank inspects a
//     loaded bank as DATA and reports every RULE violation: the §6.0 domain
//     proportions (~45% software-engineering, ~30% broader STEM, ~25%
//     core-knowledge; ≥30% non-software-engineering — the G9 generalization),
//     the §2.3 per-mechanism gap rules (isolation: co-mechanisms stripped;
//     safety: camouflaged + ≥30% ALLOW mass; convertibility: recurrence+decoy;
//     down-weighted trivial mass), and the HARD GUARD that every item still
//     REQUIRES its mechanism (no bare-model-already-aces trivia). It returns a
//     structured report, never panics, so a bank under construction is auditable.
//
//   - Generator (gen.go). The Generator interface seeds GenerateTierA /
//     GenerateTierB from the authored pilot bank read as few-shot examples plus
//     the §2.2 archetype templates and the §2.3 gap rules. Ground truth is fixed
//     by construction wherever possible (the oracle, the lure, the planted
//     schedule) so "correct branch / same shape / eligible thought" is decided at
//     generation, not judged. Every model call routes through a
//     backends.Backend, so passing backends.NewTest() makes generation
//     STUBBED-DETERMINISTIC and offline — the unit tests never touch the network.
//
//   - Judge (gen.go). Judge wraps the LLM-as-judge call for the residual rubric
//     clauses (the ≤20% of items where a deterministic oracle is infeasible). It
//     runs at a fixed temperature through the same backends.Backend, is
//     deterministic under the test double, and is characterized in Phase-0
//     (§5.5) before any rubric verdict it produces can contribute to a keep.
//
// Determinism. Both the generator and the judge route every model call through
// an injected backends.Backend; under backends.NewTest() the whole pipeline is
// reproducible and never opens a socket (spec §5.4: "STUBBED-DETERMINISTIC when
// given backends.NewTest()"). The seeded *cpyrand.Random is threaded for any
// stochastic choice (which few-shot to draw, which family to vary) so a regen at
// the same seed is byte-identical.
package gen
