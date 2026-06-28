// Package stats is the pure statistics layer for the benchmark (the
// "measuring stick"). It implements the shared statistical contract of
// docs/internal/notes/measuring-stick-spec.md §4 and registry-scaling-strategy.md §7:
// paired hypothesis tests reported as effect sizes with confidence intervals,
// power-derived sample sizes, FDR control, and the zero-event bounds the safety
// claim needs.
//
// The math is deliberately PURE: this package imports only the Go standard
// library + math. Nothing here calls the wall clock or unseeded randomness —
// every randomized routine (the bootstrap) takes an explicit *rand.Rand or
// seed, so results are reproducible bit-for-bit. It does not import the engine,
// the event bus, or even internal/bench/types; the bench runner adapts its own
// result rows into the small input structs below.
//
// Coverage (one function per statistical decision in the spec):
//
//   - McNemar          — paired binary lift (exact + mid-p), §4.2
//   - WilcoxonSignedRank — continuous paired delta, §4.2
//   - BootstrapBCa     — bias-corrected accelerated CI on a paired statistic, §4.3
//   - BenjaminiHochberg — two-layer FDR control, §4.4
//   - PowerN / Z       — the power-derived N solver, §4.5
//   - WilsonInterval   — score interval for a proportion (Phase-0 2AFC), §4.1
//   - ICC21            — test-retest reliability ICC(2,1), §4.1
//   - TwoAFC           — forced-choice accuracy + Wilson CI, §4.1
//   - RuleOfThree /
//     ClopperPearsonUpper — zero-event upper bound for "unsafe_executions==0", §3.6
package stats
