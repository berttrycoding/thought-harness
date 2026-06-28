package realhard

// multiHopTasks — MULTI-HOP GROUNDING (Capability = CapMultiHopGrounding).
//
// Each answer requires chaining 3+ reads/inferences where any single read is
// insufficient: read an index/router to learn WHICH file holds the answer, read
// that file to learn a pointer/override, then read/compute the final value. A
// single-shot bare model cannot follow the chain — it has no tools, so it
// GUESSES the documented default (the lure). The harness reads step by step.
//
// The chains are deliberately built so the FIRST file a naive reader would open
// (the README / the documented default) holds the LURE, and the authoritative
// value lives at the end of an override chain. Ground-truth is a single number,
// scored exact/numeric — no fuzzy match.

func multiHopTasks() []Task {
	return []Task{
		// ---- Hop chain: env -> service map -> the active override -------------
		// README documents pool=50 (the lure). But config/env.yaml says
		// profile=prod; config/profiles/prod.yaml overrides the checkout pool to
		// 12 via a service-specific block. 3 reads to reach 12.
		{
			ID:         "realhard-mhop-0001",
			Capability: CapMultiHopGrounding,
			Prompt: "In this repo, what is the effective database connection-pool size " +
				"for the `checkout` service in the ACTIVE deployment profile? Do not assume " +
				"the documented default — resolve the active profile and any service-specific " +
				"override. Start from config/env.yaml. Report a single integer.",
			Materials: map[string]string{
				"README.md": "# Service Platform\n\n" +
					"The default database connection-pool size is **50** per service " +
					"(see ops runbook). Individual profiles may override this.\n\n" +
					"Active profile is selected in `config/env.yaml`.\n",
				"config/env.yaml": "# env.yaml -- selects the active deployment profile.\n" +
					"active_profile: prod\n" +
					"profiles_dir: config/profiles\n" +
					"# NOTE: staging profile pins pool=50; prod overrides per-service.\n",
				"config/profiles/staging.yaml": "profile: staging\n" +
					"db:\n  default_pool: 50\n" +
					"services:\n  checkout:\n    pool: 50\n",
				"config/profiles/prod.yaml": "profile: prod\n" +
					"db:\n  default_pool: 30   # prod raises the global default\n" +
					"services:\n" +
					"  search:\n    pool: 20\n" +
					"  checkout:\n    pool: 12   # checkout is rate-limited upstream; small pool on purpose\n" +
					"  billing:\n    pool: 30\n",
			},
			Oracle:     OracleExact,
			Expected:   "12",
			Normalizer: "number",
			PriorLure:  "50",
			Why: "Bare reads the documented default (50) or the staging pin (50); the " +
				"authoritative value is the prod profile's checkout override (12), reachable " +
				"only by chaining env.yaml -> prod.yaml -> the service block.",
		},
		// ---- Hop chain: alias -> canonical -> arithmetic ----------------------
		// The prompt asks for retries for the `orders` queue. messaging.yaml maps
		// orders -> alias of "high_priority"; queues.yaml defines high_priority
		// max_attempts=5, of which 1 is the initial try, so RETRIES = 4.
		{
			ID:         "realhard-mhop-0002",
			Capability: CapMultiHopGrounding,
			Prompt: "How many RETRIES (not total attempts) does the `orders` message queue " +
				"perform on failure in this config? Resolve any queue aliasing first. " +
				"Start from config/messaging.yaml. Report a single integer.",
			Materials: map[string]string{
				"config/messaging.yaml": "# messaging.yaml -- queue topology + aliases.\n" +
					"aliases:\n" +
					"  orders: high_priority   # the orders queue is an alias of high_priority\n" +
					"  audit: low_priority\n" +
					"queues_file: config/queues.yaml\n",
				"config/queues.yaml": "# queues.yaml -- canonical queue definitions.\n" +
					"# max_attempts INCLUDES the initial attempt; retries = max_attempts - 1.\n" +
					"queues:\n" +
					"  high_priority:\n    max_attempts: 5\n    backoff_ms: 200\n" +
					"  low_priority:\n    max_attempts: 2\n    backoff_ms: 1000\n",
				"README.md": "Most queues retry **3** times by default.\n",
			},
			Oracle:     OracleExact,
			Expected:   "4",
			Normalizer: "number",
			PriorLure:  "3",
			Why: "Bare guesses the documented default (3) or returns max_attempts (5) " +
				"without resolving the alias and the attempts-vs-retries off-by-one. The " +
				"chain is alias(orders->high_priority) -> max_attempts=5 -> retries=4.",
		},
		// ---- Hop chain: feature flag -> tier table -> rate -------------------
		// What is the per-minute rate limit applied to a "gold" tenant when the
		// burst_v2 flag is ON? flags.yaml burst_v2: on. tiers.yaml gold base=600.
		// burst.yaml: when burst_v2 on, gold multiplier=2 -> 1200.
		{
			ID:         "realhard-mhop-0003",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the effective per-minute API rate limit for a `gold`-tier " +
				"tenant in THIS deployment, accounting for the active feature flags? " +
				"Start from config/flags.yaml. Report a single integer (requests/minute).",
			Materials: map[string]string{
				"config/flags.yaml": "# feature flags for this deployment.\n" +
					"flags:\n  burst_v2: on      # doubles burst headroom for gold/platinum\n" +
					"  legacy_throttle: off\n" +
					"tiers_file: config/tiers.yaml\n" +
					"burst_file: config/burst.yaml\n",
				"config/tiers.yaml": "# base per-minute rate limits by tier.\n" +
					"tiers:\n  silver: 200\n  gold: 600\n  platinum: 1500\n",
				"config/burst.yaml": "# burst multipliers applied IFF burst_v2 flag is on.\n" +
					"burst_v2_multipliers:\n  gold: 2\n  platinum: 2\n  silver: 1\n",
				"README.md": "Gold tenants are limited to **600** requests/minute.\n",
			},
			Oracle:     OracleExact,
			Expected:   "1200",
			Normalizer: "number",
			PriorLure:  "600",
			Why: "Bare reads the documented base (600); the effective limit needs the " +
				"flag (burst_v2 on) -> the gold multiplier (2) -> 600*2=1200. Three reads " +
				"plus an inference.",
		},
	}
}
