package realhard

// backtrackTasks — ADAPTIVE BACKTRACKING (Capability = CapAdaptiveBacktracking).
//
// The OBVIOUS first approach is a dead-end; the correct answer requires
// recognizing the dead-end and replanning. A single-shot bare model commits to
// the first plausible reading and emits the dead-end answer (the lure). The
// harness, grounding step by step, hits the contradiction and replans.
//
// The trap is built INTO the material: the surface/obvious file says one thing,
// but a marker in that same file (a "DEPRECATED", "SUPERSEDED", "DO NOT USE")
// invalidates it and points to the real source. A reader who stops at the first
// hit fails; a reader who reads to the end of the file (or notices the marker)
// and follows the pointer succeeds.

func backtrackTasks() []Task {
	return []Task{
		// ---- The obvious config is DEPRECATED; follow the SUPERSEDED-BY pointer.
		// limits.go has TWO consts: the old one (first, with the obvious name) is
		// marked DEPRECATED, superseded by a renamed const further down with the
		// real value.
		{
			ID:         "realhard-back-0001",
			Capability: CapAdaptiveBacktracking,
			Prompt: "What is the maximum batch size the ingestion pipeline uses in this " +
				"codebase? Read config/limits.go. Report a single integer.",
			Materials: map[string]string{
				"config/limits.go": "package config\n\n" +
					"// MaxBatchSize is the ingestion batch cap.\n" +
					"// DEPRECATED (2026-02): superseded by IngestBatchLimit below — the\n" +
					"// pipeline no longer reads this constant. Left only for an old test.\n" +
					"const MaxBatchSize = 500\n\n" +
					"// IngestBatchLimit is the ACTIVE cap the ingestion pipeline reads as of\n" +
					"// the v3 rewrite. This is the value in force.\n" +
					"const IngestBatchLimit = 128\n",
			},
			Oracle:     OracleExact,
			Expected:   "128",
			Normalizer: "number",
			PriorLure:  "500",
			Why: "The obvious const (MaxBatchSize=500) matches the question's wording but " +
				"is DEPRECATED; the active value is the renamed IngestBatchLimit=128. Bare " +
				"stops at the first plausible hit (500); the harness must read past the " +
				"DEPRECATED marker and backtrack to the superseding const.",
		},
		// ---- The first formula in the doc is WRONG (corrected by an erratum).
		// pricing.md gives a formula, then an "ERRATUM" later in the same file that
		// corrects it. Compute with the corrected formula.
		{
			ID:         "realhard-back-0002",
			Capability: CapAdaptiveBacktracking,
			Prompt: "Using the pricing rules in pricing.md, compute the total charge for " +
				"an order of 10 units at the list unit price, applying the volume discount. " +
				"Read the WHOLE file. Report the total as a number (dollars).",
			Materials: map[string]string{
				"pricing.md": "# Pricing\n\n" +
					"List unit price: $20.\n\n" +
					"## Volume discount (v1)\n" +
					"Orders of 10+ units get a flat 25% off the line total.\n" +
					"So 10 units = 10 * 20 * 0.75 = $150.\n\n" +
					"## ERRATUM (v2, supersedes v1 above)\n" +
					"The v1 25% discount was a typo. The CORRECT volume discount for 10+ " +
					"units is **10% off**, NOT 25%. All v1 worked examples are wrong; use " +
					"10%. (10 units = 10 * 20 * 0.90 = $180.)\n",
			},
			Oracle:     OracleNumericTolerance,
			Expected:   "180",
			Normalizer: "number",
			Tolerance:  0.5,
			PriorLure:  "150",
			Why: "The first, fully-worked formula (25% -> $150) is the obvious answer and " +
				"matches a confident single-shot. An ERRATUM later in the file supersedes it " +
				"(10% -> $180). Bare commits to the first worked example; the harness must " +
				"read on, hit the erratum, and recompute.",
		},
	}
}
