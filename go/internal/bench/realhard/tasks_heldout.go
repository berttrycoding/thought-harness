package realhard

// heldOutTasks — HELD-OUT GENERALIZATION SET for grounding-completeness /
// lure-resistance (Capabilities = CapAdaptiveBacktracking + CapMultiHopGrounding).
//
// WHY THIS FILE EXISTS (the integrity contract). The in-suite backtrack /
// multi-hop fixtures (tasks_backtrack.go, tasks_multihop.go) build their traps
// from a small, recognizable keyword vocabulary: DEPRECATED / SUPERSEDED /
// ERRATUM, and a feature-FLAG MULTIPLIER. A capability fix that merely learns to
// react to those literal tokens would "lift" on those fixtures WITHOUT acquiring
// the underlying skill — keyword-matching, not grounding completeness. This
// held-out set probes the SAME capability with trap SHAPES that share NONE of
// those tokens, so a lift measured here is evidence of the GENERAL skill, not of
// overfitting to the in-suite phrasing.
//
// THE CAPABILITY (probed three ways, generally — not against any one prompt):
//   (a) RECENCY / IN-FORCE resolution — when a value is invalidated by a LATER
//       statement, use the value actually in force, not the first name-matching
//       hit. Here the invalidation is encoded by a DATE / VERSION ordering or a
//       reworded "no longer used / replaced by" note — never the in-suite tokens.
//   (b) MULTI-STEP value chain — apply a modifier / unit conversion, or follow a
//       reworded cross-file pointer, to reach the value actually asked for.
//   (c) DECLINE when genuinely absent — when the needed value lives only behind
//       an external/unreadable pointer, ground-or-decline (do NOT fabricate a
//       value to satisfy a forced supersession or a dangling pointer). This is
//       the NEGATIVE CONTROL that catches a fix which over-fires.
//
// KEYWORD HYGIENE (asserted in tasks_heldout_test.go): none of the six tasks'
// MATERIALS contain "deprecated", "superseded", "erratum", "flag", or
// "multiplier" (case-insensitive). The trap must be inferred from recency / a
// reworded note / a conversion, never matched on a banned token.
//
// Three tasks are backtrack-class (an in-file invalidation the obvious first
// reading misses) and three are multi-hop-class (a cross-file chain / a genuine
// absence). All oracles are the existing deterministic kinds (exact / numeric /
// decline) so the set is offline-vettable and mutation-tested.

func heldOutTasks() []Task {
	return []Task{
		// ============================================================== (1)
		// RECENCY BY VERSION — two consts, one "since v1.0", one "since v3.2".
		// The prompt asks for the value the build "currently" uses. The reader
		// must INFER that v3.2 is newer than v1.0 and is therefore in force —
		// there is no "deprecated"/"superseded" token; only the version dates
		// distinguish the stale const from the live one.
		{
			ID:         "realhard-held-0001",
			Capability: CapAdaptiveBacktracking,
			Prompt: "Read config/timeouts.go. The build CURRENTLY in production uses the " +
				"most recently introduced request-timeout value. What is the request timeout, " +
				"in seconds, that the current build uses? Report a single integer.",
			Materials: map[string]string{
				"config/timeouts.go": "package config\n\n" +
					"import \"time\"\n\n" +
					"// RequestTimeout was the original request budget.\n" +
					"// since v1.0 (2021-03)\n" +
					"const RequestTimeoutLegacy = 30 * time.Second\n\n" +
					"// RequestTimeoutCurrent is the budget introduced with the latency rework.\n" +
					"// since v3.2 (2025-09)\n" +
					"const RequestTimeoutCurrent = 8 * time.Second\n",
			},
			Oracle:     OracleExact,
			Expected:   "8",
			Normalizer: "number",
			PriorLure:  "30",
			Why: "Two consts carry version stamps: RequestTimeoutLegacy (since v1.0) = 30s and " +
				"RequestTimeoutCurrent (since v3.2) = 8s. There is NO 'deprecated' marker — the " +
				"reader must infer that v3.2 post-dates v1.0, so the 8s const is the one in force. " +
				"Bare grabs the first name-matching value (30); the harness must read both stamps " +
				"and pick the more recent.",
		},

		// ============================================================== (2)
		// UNIT-CONVERSION MODIFIER — a base limit stated in MB, plus an in-file
		// note that limits are now expressed in GB and must be divided by 1024.
		// A multi-step value chain (read base -> apply conversion) with NO
		// "multiplier"/"flag" wording; the modifier is a plain unit instruction.
		{
			ID:         "realhard-held-0002",
			Capability: CapAdaptiveBacktracking,
			Prompt: "Read storage/quota.md. What is the per-tenant upload limit, expressed " +
				"in GIGABYTES, that this service enforces? Apply any conversion stated in the " +
				"file. Report the limit in GB as an integer.",
			Materials: map[string]string{
				"storage/quota.md": "# Per-tenant upload quota\n\n" +
					"The raw quota constant is stored as `UPLOAD_LIMIT = 2048` (megabytes).\n\n" +
					"## Reporting convention (effective this release)\n" +
					"All tenant-facing quota limits are now reported in GIGABYTES, not megabytes. " +
					"To convert the raw constant to the reported figure, divide the megabyte value " +
					"by 1024. The reported per-tenant upload limit is therefore the converted GB " +
					"value, not the raw 2048.\n",
			},
			Oracle:     OracleExact,
			Expected:   "2",
			Normalizer: "number",
			PriorLure:  "2048",
			Why: "The raw constant is 2048 (MB); the file states limits are now reported in GB via " +
				"divide-by-1024, so the enforced figure is 2048/1024 = 2 GB. A multi-step chain (read " +
				"the base, then apply the conversion) with no 'multiplier' token. Bare reports the raw " +
				"2048; the harness must apply the stated conversion.",
		},

		// ============================================================== (3)
		// SECOND RECENCY VARIANT (different surface) — a CHANGELOG with two dated
		// entries for the same setting. The later date raises the value; the
		// prompt asks for the value "as of the latest release". Recency by an
		// explicit DATE ordering this time (not a version tag), reworded again so
		// it shares no surface with task (1).
		{
			ID:         "realhard-held-0003",
			Capability: CapAdaptiveBacktracking,
			Prompt: "Read CHANGELOG.md. As of the LATEST release recorded in this changelog, " +
				"how many worker threads does the scheduler start? Report a single integer.",
			Materials: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n" +
					"## 2023-04-10\n" +
					"- Scheduler now starts 4 worker threads on boot.\n" +
					"- Added a health endpoint.\n\n" +
					"## 2024-11-02\n" +
					"- Raised the scheduler worker-thread count to 16 to handle the new fan-out.\n" +
					"- Tuned the backoff curve.\n",
			},
			Oracle:     OracleExact,
			Expected:   "16",
			Normalizer: "number",
			PriorLure:  "4",
			Why: "The changelog has two dated entries touching the worker-thread count: 2023-04-10 " +
				"(4 threads) and 2024-11-02 (raised to 16). The latest release (2024-11-02) sets 16. " +
				"Recency is encoded by the DATE headers, with no invalidation keyword. Bare reads the " +
				"first entry (4); the harness must order the dates and take the latest (16).",
		},

		// ============================================================== (4)
		// REWORDED POINTER CHAIN — the asked figure is not in the entry file; the
		// entry file says the authoritative figure "lives in" an appendix, and a
		// cross-reference fixes which row. A real two-file hop with NO
		// "superseded-by"/"see _file:" mechanical wording — a plain-English
		// pointer the reader must follow.
		{
			ID:         "realhard-held-0004",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the committed monthly uptime target (as a percentage) for the " +
				"`payments` service under the ENTERPRISE plan? Start from docs/sla.md and follow " +
				"any pointer it gives. Report the percentage as a number (e.g. 99.9).",
			Materials: map[string]string{
				"docs/sla.md": "# Service Level Agreement\n\n" +
					"The headline uptime target is 99.0% for all services on the standard plan.\n\n" +
					"Per-plan and per-service commitments are NOT listed here; the authoritative " +
					"figures live in the appendix at docs/appendix-sla.md. For the enterprise plan, " +
					"cross-reference the row for the service you care about in that appendix.\n",
				"docs/appendix-sla.md": "# Appendix: per-plan uptime commitments\n\n" +
					"Each row is `service | standard | enterprise`.\n\n" +
					"search   | 99.0 | 99.9\n" +
					"payments | 99.5 | 99.99\n" +
					"audit    | 98.0 | 99.5\n",
			},
			Oracle:     OracleNumericTolerance,
			Expected:   "99.99",
			Normalizer: "number",
			Tolerance:  0.001,
			PriorLure:  "99.0",
			Why: "The entry file (sla.md) gives only the standard headline (99.0) and POINTS to the " +
				"appendix in plain prose ('the authoritative figures live in the appendix at " +
				"docs/appendix-sla.md'). The reader must hop to the appendix and read the payments " +
				"row's enterprise column (99.99). No mechanical 'see _file:' token. Bare returns the " +
				"headline 99.0; the harness must follow the pointer and read the right cell.",
		},

		// ============================================================== (5)
		// SECOND REWORDED MULTI-HOP — a 3-file chain with different phrasing: an
		// index names which subsystem owns a setting, the owner file points to a
		// defaults file, and the defaults file holds the value. The entry file's
		// own "typical" number is the lure. No keyword overlap with task (4).
		{
			ID:         "realhard-held-0005",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the maximum number of in-flight requests the gateway permits for " +
				"the `ingest` route in this service? Start from routing/index.md, find which " +
				"component owns that limit, and read its configured value. Report a single integer.",
			Materials: map[string]string{
				"routing/index.md": "# Routing index\n\n" +
					"Typical routes allow about 64 concurrent in-flight requests.\n\n" +
					"Concurrency limits are not set here. The component that owns per-route " +
					"in-flight caps is the admission controller, described in " +
					"routing/admission.md.\n",
				"routing/admission.md": "# Admission controller\n\n" +
					"The admission controller enforces per-route in-flight caps. The actual " +
					"numeric caps are kept out of this document and loaded from the defaults " +
					"file routing/admission-defaults.yaml at startup.\n",
				"routing/admission-defaults.yaml": "# per-route in-flight caps loaded by the admission controller\n" +
					"routes:\n" +
					"  default:\n    inflight_cap: 64\n" +
					"  search:\n    inflight_cap: 100\n" +
					"  ingest:\n    inflight_cap: 12   # ingest is intentionally throttled\n" +
					"  export:\n    inflight_cap: 40\n",
			},
			Oracle:     OracleExact,
			Expected:   "12",
			Normalizer: "number",
			PriorLure:  "64",
			Why: "Three hops: index.md ('typical ~64', the lure) points to admission.md, which " +
				"points to admission-defaults.yaml, where the ingest route's inflight_cap is 12. " +
				"The entry file's 'typical 64' is the value a non-grounding reader returns. The " +
				"harness must chain index -> admission -> defaults and read the ingest row.",
		},

		// ============================================================== (6)
		// NEGATIVE CONTROL — TWO traps at once:
		//   (i) A nearby invalidated-LOOKING but IRRELEVANT marker: the file marks
		//       a DIFFERENT key as removed; a fix that over-fires on any "removed"
		//       cue could wrongly discard the actually-correct first value.
		//   (ii) The value the prompt asks for is genuinely ABSENT — it lives only
		//       behind an external, unreadable pointer. The correct move is to
		//       DECLINE, not to fabricate a number to "resolve" the pointer.
		// The FIRST-named value in the file (for the OTHER key) is correct and is
		// NOT what is asked; the asked value is undeterminable -> decline. This
		// catches a fix that forces a spurious supersession OR confabulates to
		// satisfy a dangling cross-reference.
		{
			ID:         "realhard-held-0006",
			Capability: CapMultiHopGrounding,
			Prompt: "Read config/cache.md. What is the configured time-to-live, in seconds, for " +
				"the `session` cache in THIS service? Report the number, or state plainly if it " +
				"is not determinable from the material.",
			Materials: map[string]string{
				"config/cache.md": "# Cache configuration\n\n" +
					"This service runs two caches.\n\n" +
					"The `asset` cache uses a TTL of 300 seconds. (An older `thumbnail` cache was " +
					"removed in the last cleanup and no longer runs; ignore it.)\n\n" +
					"The `session` cache TTL is NOT defined in this repository. It is provisioned " +
					"by the platform team and read at runtime from the managed secret store at " +
					"`vault://platform/session-cache/ttl`, which is not part of this codebase and " +
					"cannot be read here.\n",
			},
			Oracle:    OracleDecline,
			Expected:  "", // correct answer is a decline
			PriorLure: "300",
			Why: "The prompt asks for the SESSION cache TTL, which the file says is provisioned " +
				"externally (vault://...) and is not in the codebase — genuinely undeterminable. " +
				"Two traps: (i) the file names a 300s TTL for a DIFFERENT cache (asset) — the " +
				"first/obvious number, which a lure-following reader reports; (ii) it mentions a " +
				"'removed' thumbnail cache that an over-firing supersession fix might wrongly act " +
				"on. The correct move is to DECLINE (the asked value is absent); asserting 300 " +
				"(the asset TTL) is the confabulation this control catches.",
		},
	}
}
