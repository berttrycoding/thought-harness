package realhard

// tasks_hard.go — the HARDER calibration batch (IDs realhard-hard-NNNN).
//
// WHY THIS FILE EXISTS. The standing realhard suite is mostly SATURATED on the
// frontier base model (claude sonnet): the off-pilot p-map (runs/bern-off-pilot.txt)
// shows conf-* and long-* near p=1.0 (zero outcome variance -> zero Fisher info),
// so they carry no measurement signal. Only back-0001/0002 and mhop-0001/0003 sit
// anywhere near the informative band p in (0.3, 0.7). To raise T_eff (the number
// of NON-saturated tasks) this file OVER-AUTHORS ~12 candidates that DEEPEN each
// existing family at a difficulty calibrated to land harder: more hops, longer
// chains with a mid-chain rule change, fresh backtracking surfaces, and stronger
// anti-confabulation lures. The intent is the p in (0.3, 0.7) band; difficulty is
// the ONLY thing that cannot be verified offline (it is band-selected by a
// high-K claude launch afterward). SOUNDNESS, VARIETY, and plausible HARDNESS are
// what this file guarantees offline.
//
// HYGIENE CONTRACT (asserted in tasks_hard_test.go, mirroring tasks_heldout.go):
//   - none of these tasks' MATERIALS contain a banned in-suite trap token
//     (deprecated / superseded / erratum / flag / multiplier) — the trap must be
//     inferred (recency / rollback / scope / a conversion / a cap), never matched
//     on a keyword reflex;
//   - the materials are DISJOINT from every existing fixture (no copied content),
//     so a lift here cannot be an artifact of re-using a known fixture.
//
// SOUNDNESS CONTRACT (asserted in tasks_hard_test.go, mirroring oracle_test.go):
//   - every ground truth SOLVES; every lure FAILS; a battery of wrong answers
//     FAILS; decline tasks credit an honest decline and fail a confident number;
//   - every COMPUTED Expected (the multi-hop chains, the long-horizon ledgers,
//     the CSP) is RE-DERIVED in code, so the ground truth is proven, not asserted.

func hardTasks() []Task {
	var t []Task
	t = append(t, hardMultiHopTasks()...)
	t = append(t, hardBacktrackTasks()...)
	t = append(t, hardLongHorizonTasks()...)
	t = append(t, hardAntiConfabTasks()...)
	return t
}

// ---- DEEPER MULTI-HOP GROUNDING ------------------------------------------------
// 4-5 hop chains with more interacting files and an off-by-one / conditional /
// cap clamp INSIDE the chain — strictly deeper than mhop-0001 (3 hops) and
// mhop-0003 (3 hops + one inference).

func hardMultiHopTasks() []Task {
	return []Task{
		// ============================================================== (1)
		// FIVE hops + an OFF-BY-ONE at the end:
		//   topology.yaml (active_region) -> regions/us-east.yaml (primary_cluster)
		//   -> clusters/blue.yaml (worker pool node_class + node count)
		//   -> node-classes.yaml (vcpu_per_node) -> apply the per-cluster
		//   control-plane reservation (1 node is NOT a worker).
		// Worker vCPU = (count - 1 reserved) * vcpu_per_node = (6-1)*8 = 40.
		// The off-by-one (forgetting the reserved control-plane node) gives 48; the
		// README "typical 80" and the secondary cluster's 10 nodes are weaker lures.
		{
			ID:         "realhard-hard-0001",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the total number of WORKER vCPUs provisioned for the PRIMARY " +
				"cluster of the ACTIVE region in this infra repo? Resolve the active region, " +
				"its primary cluster, that cluster's worker node class and node count, the " +
				"vCPU-per-node for that class, and apply any per-cluster reservation. Start " +
				"from infra/topology.yaml. Report a single integer.",
			Materials: map[string]string{
				"infra/topology.yaml": "# topology.yaml -- selects the active region.\n" +
					"active_region: us-east\n" +
					"regions_dir: infra/regions\n" +
					"# NOTE: us-west is provisioned but standby; do not size against it.\n",
				"infra/regions/us-east.yaml": "region: us-east\n" +
					"primary_cluster: blue\n" +
					"secondary_cluster: green   # warm spare, larger, NOT primary\n" +
					"clusters_dir: infra/clusters\n",
				"infra/regions/us-west.yaml": "region: us-west\n" +
					"primary_cluster: red\n",
				"infra/clusters/blue.yaml": "cluster: blue\n" +
					"worker_pool:\n" +
					"  node_class: c5.2xlarge\n" +
					"  node_count: 6        # ONE of these is reserved for the control plane\n" +
					"reserve_control_plane_nodes: 1   # this many nodes are NOT workers\n",
				"infra/clusters/green.yaml": "cluster: green\n" +
					"worker_pool:\n" +
					"  node_class: c5.2xlarge\n" +
					"  node_count: 10\n" +
					"reserve_control_plane_nodes: 1\n",
				"infra/node-classes.yaml": "# vCPU per node by class.\n" +
					"classes:\n" +
					"  c5.xlarge:   { vcpu_per_node: 4 }\n" +
					"  c5.2xlarge:  { vcpu_per_node: 8 }\n" +
					"  c5.4xlarge:  { vcpu_per_node: 16 }\n",
				"README.md": "A typical production cluster runs about 80 worker vCPUs.\n",
			},
			Oracle:     OracleExact,
			Expected:   "40",
			Normalizer: "number",
			PriorLure:  "48",
			Why: "Five reads plus an off-by-one: topology(active=us-east) -> us-east(primary=blue) " +
				"-> blue(node_count=6, node_class=c5.2xlarge, reserve 1) -> node-classes(c5.2xlarge=8) " +
				"-> worker vCPU = (6-1)*8 = 40. The off-by-one lure (48) forgets the control-plane " +
				"reservation; the README 'typical 80' and green's 10 nodes are decoys for a " +
				"shallow reader. Strictly deeper than mhop-0001's 3 hops.",
		},

		// ============================================================== (2)
		// A CONDITIONAL BRANCH in the chain:
		//   jobs.yaml (analytics -> tier=by_lookup, schedule=nightly)
		//   -> tiers.yaml (analytics = heavy)  [README lure: "standard"]
		//   -> pricing.yaml (heavy=0.40/hr, standard=0.12/hr)
		//   -> runtime.yaml: a CONDITIONAL -- nightly heavy jobs run 3h/night over a
		//      30-night month; nightly STANDARD jobs would run 1h/night.
		// cost = 30 nights * 3 h * 0.40 = 36. Wrong-branch (standard) gives 10.8.
		{
			ID:         "realhard-hard-0002",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the monthly compute cost, in dollars, for the `analytics` job in " +
				"this config? Resolve the job's tier, the per-hour price for that tier, and the " +
				"runtime rule that applies to a job of that tier and schedule. Start from " +
				"config/jobs.yaml. Report the cost as a number.",
			Materials: map[string]string{
				"config/jobs.yaml": "# jobs.yaml -- job definitions.\n" +
					"jobs:\n" +
					"  analytics:\n    schedule: nightly\n    tier: by_lookup   # resolve via tiers.yaml\n" +
					"  cleanup:\n    schedule: nightly\n    tier: standard\n" +
					"tiers_file: config/tiers.yaml\n" +
					"pricing_file: config/pricing.yaml\n" +
					"runtime_file: config/runtime.yaml\n",
				"config/tiers.yaml": "# tiers.yaml -- per-job tier assignment.\n" +
					"tiers:\n  analytics: heavy   # analytics was promoted to heavy after the data growth\n" +
					"  reporting: standard\n",
				"config/pricing.yaml": "# pricing.yaml -- per-hour price by tier (USD).\n" +
					"prices:\n  standard: 0.12\n  heavy: 0.40\n",
				"config/runtime.yaml": "# runtime.yaml -- how long a job runs, by tier and schedule.\n" +
					"# A 'nightly' schedule runs once per night over a 30-night month.\n" +
					"rules:\n" +
					"  - if: { schedule: nightly, tier: heavy }\n    hours_per_run: 3\n" +
					"  - if: { schedule: nightly, tier: standard }\n    hours_per_run: 1\n",
				"README.md": "Analytics is a standard-tier nightly job.\n",
			},
			Oracle:     OracleNumericTolerance,
			Expected:   "36",
			Normalizer: "number",
			Tolerance:  0.01,
			PriorLure:  "10.8",
			Why: "A conditional branch in the chain: analytics tier resolves to HEAVY (tiers.yaml), " +
				"NOT the README's 'standard'. Heavy nightly -> 3 h/run; 30 nights * 3 h * $0.40 = $36. " +
				"The wrong branch (standard: 30*1*0.12 = $10.80) is the lure a reader who trusts the " +
				"README takes. Four reads + a tier-conditional runtime rule.",
		},

		// ============================================================== (3)
		// FOUR hops + a CAP CLAMP (min):
		//   topology.yaml (env=prod -> ring file) -> rings/prod.yaml (events keyspace
		//   base_shards + split_factor) -> caps.yaml (max shards per keyspace).
		//   effective = min(base*split, cap) = min(16*2, 24) = 24.
		// Lures: no-cap 32, base-only 16.
		{
			ID:         "realhard-hard-0003",
			Capability: CapMultiHopGrounding,
			Prompt: "What is the EFFECTIVE shard count for the `events` keyspace in the active " +
				"environment? Resolve the active environment's ring file, the keyspace's base " +
				"shards and split factor, and apply any global cap. Start from store/topology.yaml. " +
				"Report a single integer.",
			Materials: map[string]string{
				"store/topology.yaml": "# topology.yaml -- selects the active environment ring.\n" +
					"active_env: prod\n" +
					"rings:\n  prod: store/rings/prod.yaml\n  staging: store/rings/staging.yaml\n",
				"store/rings/prod.yaml": "# prod ring -- per-keyspace sharding.\n" +
					"keyspaces:\n" +
					"  events:\n    base_shards: 16\n    split_factor: 2   # events is a hot keyspace, split 2x\n" +
					"  users:\n    base_shards: 8\n    split_factor: 1\n" +
					"caps_file: store/caps.yaml\n",
				"store/rings/staging.yaml": "keyspaces:\n  events:\n    base_shards: 4\n    split_factor: 1\n",
				"store/caps.yaml": "# caps.yaml -- hard ceilings applied to every keyspace.\n" +
					"max_shards_per_keyspace: 24   # operational ceiling; effective = min(base*split, this)\n",
				"README.md": "Each keyspace defaults to 16 shards.\n",
			},
			Oracle:     OracleExact,
			Expected:   "24",
			Normalizer: "number",
			PriorLure:  "32",
			Why: "Four reads + a clamp: topology(active=prod) -> prod ring(events base=16, split=2) " +
				"-> caps(ceiling 24) -> effective = min(16*2, 24) = 24. The no-cap lure (32) skips " +
				"the ceiling; the base-only reader returns 16 (the README default). The cap clamp is " +
				"the step a single shot drops.",
		},
	}
}

// ---- HARDER ADAPTIVE BACKTRACKING ----------------------------------------------
// Fresh invalidation SURFACES distinct from the in-suite DEPRECATED / ERRATUM
// fixtures: a ROLLBACK (which INVERTS the recency reflex), a SCOPE trap (the
// obvious const applies to a different code path), and a DISABLED-toggle (the
// configured value is not in force; a fallback is).

func hardBacktrackTasks() []Task {
	return []Task{
		// ============================================================== (1)
		// ROLLBACK surface (inverts recency). Two values: the original 512, then a
		// later note saying it was raised to 2048, then a STILL-later note saying
		// that raise was ROLLED BACK after an OOM regression and 512 restored. A
		// recency reflex grabs 2048 (the newest-looking number); the value in force
		// is the rolled-back-to original 512. No deprecated/erratum tokens.
		{
			ID:         "realhard-hard-0004",
			Capability: CapAdaptiveBacktracking,
			Prompt: "What heap size, in MB, does the worker run with in production RIGHT NOW? " +
				"Read runtime/heap.md fully and report the value actually in force. Report a " +
				"single integer.",
			Materials: map[string]string{
				"runtime/heap.md": "# Worker heap sizing\n\n" +
					"The worker has run with a 512 MB heap since launch.\n\n" +
					"## perf branch (2025-06)\n" +
					"We raised the heap to 2048 MB to cut GC pauses under load.\n\n" +
					"## production note (2025-08)\n" +
					"The 2048 MB heap was ROLLED BACK after it caused OOM kills on the smaller " +
					"prod instances. The worker was reverted to its original 512 MB heap, which " +
					"is what production runs today. Do not use the 2048 figure.\n",
			},
			Oracle:     OracleExact,
			Expected:   "512",
			Normalizer: "number",
			PriorLure:  "2048",
			Why: "Inverts the recency reflex: the NEWEST-looking number (2048, the perf-branch " +
				"raise) was ROLLED BACK; the value in force is the restored original 512. A reader " +
				"who grabs the most recent change lands on the lure 2048; the correct read follows " +
				"the rollback note to 512. A distinct surface from DEPRECATED/ERRATUM (it is the " +
				"newest change that is invalid, not the oldest).",
		},

		// ============================================================== (2)
		// SCOPE trap + a small computation. Two timeout consts: DefaultTimeout (5s,
		// scoped to INTERACTIVE requests by a comment) and BatchTimeout (120s, the
		// batch path). The prompt asks for the BATCH path budget across 3 sequential
		// calls. The obvious DefaultTimeout (5s, first/most-prominent) is the wrong
		// SCOPE; correct is 3*120 = 360. Lure 15 (= 3*5, wrong scope).
		{
			ID:         "realhard-hard-0005",
			Capability: CapAdaptiveBacktracking,
			Prompt: "A batch job makes 3 sequential downstream calls, each bounded by the " +
				"per-call timeout for the BATCH path. Using config/timeouts.go, what is the " +
				"job's worst-case total timeout budget, in seconds? Report a single integer.",
			Materials: map[string]string{
				"config/timeouts.go": "package config\n\n" +
					"import \"time\"\n\n" +
					"// DefaultTimeout bounds INTERACTIVE (user-facing) request handlers only.\n" +
					"// The batch path does NOT use this value.\n" +
					"const DefaultTimeout = 5 * time.Second\n\n" +
					"// BatchTimeout bounds each downstream call made on the batch/ingest path.\n" +
					"const BatchTimeout = 120 * time.Second\n",
			},
			Oracle:     OracleExact,
			Expected:   "360",
			Normalizer: "number",
			PriorLure:  "15",
			Why: "A SCOPE trap: DefaultTimeout (5s) is the first/most-prominent const but is scoped " +
				"to INTERACTIVE handlers; the batch path uses BatchTimeout (120s). Worst-case = " +
				"3 sequential calls * 120 = 360. The lure (15 = 3*5) applies the wrong-scope " +
				"DefaultTimeout. No invalidation keyword — the trap is the scope comment a reader " +
				"must honor.",
		},

		// ============================================================== (3)
		// DISABLED-toggle surface (no 'flag' token). A config block sets cache_size
		// to 256, but a note says that block is gated behind a capability that is
		// currently turned OFF in prod, so the documented FALLBACK of 64 is what runs.
		// Obvious value 256; in force 64.
		{
			ID:         "realhard-hard-0006",
			Capability: CapAdaptiveBacktracking,
			Prompt: "What cache size (number of entries) does the service actually use in " +
				"production currently? Read config/cache.go fully and report the value in " +
				"force. Report a single integer.",
			Materials: map[string]string{
				"config/cache.go": "package config\n\n" +
					"// The large-cache capability sets the cache to 256 entries.\n" +
					"const LargeCacheSize = 256\n\n" +
					"// The large-cache capability is currently TURNED OFF in production (it is\n" +
					"// gated behind a rollout toggle that has not been enabled in prod). While it\n" +
					"// is off, the service uses the fallback size below — this is the value in\n" +
					"// force today.\n" +
					"const FallbackCacheSize = 64\n",
			},
			Oracle:     OracleExact,
			Expected:   "64",
			Normalizer: "number",
			PriorLure:  "256",
			Why: "The configured LargeCacheSize (256) is the obvious/prominent value, but the note " +
				"says that capability is TURNED OFF in prod, so the FallbackCacheSize (64) is in " +
				"force. A reader who stops at the configured block returns 256; the correct read " +
				"honors the disabled-toggle note and uses 64. Fresh surface (a disabled capability, " +
				"not a deprecation).",
		},
	}
}

// ---- LONGER-HORIZON CONSISTENCY ------------------------------------------------
// Longer chains than long-0001 (12 events): a 16-event inventory ledger with a
// PERCENTAGE-then-FLAT audit rule change and two reversals; a balance/interest
// ledger with a mid-chain rate change and a reversal whose interest is NOT
// recomputed; and a 7-task ordering CSP (one more task + one more constraint than
// long-0003's 6-service CSP). All Expecteds re-derived in tasks_hard_test.go.

func hardLongHorizonTasks() []Task {
	return []Task{
		// ============================================================== (1)
		// 16-event inventory ledger. Audit rule v1 = remove 10% (floor); a mid-chain
		// RULE CHANGE makes audits a FLAT 5; two reversals. The lure keeps the 10%
		// rule throughout (forgets the change) and lands on 91; correct = 108.
		{
			ID:         "realhard-hard-0007",
			Capability: CapLongHorizonConsistency,
			Prompt: "Track the integer quantity in warehouse W through these 16 events IN ORDER. " +
				"W starts at 0. One rule CHANGES partway through; apply each rule only from where " +
				"it takes effect (no retroactive change). All audit removals FLOOR (round down). " +
				"Report W's FINAL quantity as an integer.\n" +
				"Audit rule (v1): each 'audit' event REMOVES 10% of the current quantity, floored.\n" +
				"E1: receive 120 into W.\n" +
				"E2: ship 30 from W.\n" +
				"E3: ship 30 from W.\n" +
				"E4: receive 50 into W.\n" +
				"E5: audit W.\n" +
				"E6: receive 40 into W.\n" +
				"E7: ship 30 from W.\n" +
				"E8: REVERSE event E7 entirely (undo the E7 shipment).\n" +
				"E9: RULE CHANGE: from this point on, an 'audit' event removes a FLAT 5 (not 10%). " +
				"This does NOT retroactively change E5.\n" +
				"E10: audit W.\n" +
				"E11: ship 24 from W.\n" +
				"E12: REVERSE event E11 entirely (undo the E11 shipment).\n" +
				"E13: receive 16 into W.\n" +
				"E14: audit W.\n" +
				"E15: ship 45 from W.\n" +
				"E16: receive 8 into W.\n" +
				"Report W's final quantity.",
			Oracle:     OracleExact,
			Expected:   "108",
			Normalizer: "number",
			PriorLure:  "91",
			Why: "0 +120 -30 -30 +50 =110; E5 audit 10% floor -> -11 =99; +40 -30 =109; E8 reverse " +
				"E7 (+30) =139; [E9 rule change: audit now flat 5] E10 -5 =134; -24 +24 (E12 reverse) " +
				"=134; +16 =150; E14 flat-5 -> 145; -45 =100; +8 =108 (re-derived in tasks_hard_test.go). " +
				"The lure (91) applies the 10% audit at E10/E14 (forgets the E9 rule change). A 16-step " +
				"chain with a percentage->flat rule change and two reversals is where a single shot drifts.",
		},

		// ============================================================== (2)
		// Balance/interest ledger. Interest rule v1 = 10% of current; a mid-chain
		// rate change to 5%; a reversal (E8 undoes E4's withdrawal) whose ALREADY-
		// COMPUTED interest is NOT recomputed. Correct = 1711; lure 1848 keeps 10%
		// throughout.
		{
			ID:         "realhard-hard-0008",
			Capability: CapLongHorizonConsistency,
			Prompt: "Track account balance B through these 9 events IN ORDER. B starts at 1000. " +
				"Interest is computed on the CURRENT balance at the moment of the interest event " +
				"and FLOORED (round down); already-applied interest is never recomputed. One rate " +
				"CHANGES partway through. A reversal undoes only that event's principal movement, " +
				"NOT any interest already computed. Report B's FINAL balance as an integer.\n" +
				"Interest rule (v1): an 'interest' event ADDS 10% of the current balance, floored.\n" +
				"E1: deposit 500 into B.\n" +
				"E2: withdraw 200 from B.\n" +
				"E3: interest event on B.\n" +
				"E4: withdraw 430 from B.\n" +
				"E5: RULE CHANGE: from this point on, an 'interest' event adds 5% (not 10%), floored. " +
				"This does NOT retroactively change E3.\n" +
				"E6: interest event on B.\n" +
				"E7: deposit 150 into B.\n" +
				"E8: REVERSE event E4 entirely (undo the E4 withdrawal). Do NOT recompute E6's interest.\n" +
				"E9: interest event on B.\n" +
				"Report B's final balance.",
			Oracle:     OracleExact,
			Expected:   "1711",
			Normalizer: "number",
			PriorLure:  "1848",
			Why: "1000 +500 -200 =1300; E3 interest 10% floor +130 =1430; -430 =1000; [E5 rate->5%] " +
				"E6 interest 5% +50 =1050; +150 =1200; E8 reverse E4 (+430) =1630; E9 interest 5% " +
				"+81 (1630*0.05=81.5 floor) =1711 (re-derived in tasks_hard_test.go). The lure (1848) " +
				"keeps the 10% rate at E6/E9 (forgets the E5 change). The reversal-does-not-recompute " +
				"rule plus a mid-chain rate change is the consistency trap.",
		},

		// ============================================================== (3)
		// 7-task ordering CSP (one more task + one more rule than long-0003's 6).
		// Unique, NON-identity solution (3,6,1,4,7,5,2) -> T5 at position 6. The lure
		// (5) is the off-by-one from misapplying R6's adjacency ('immediately before').
		// Brute-forced + uniqueness asserted in tasks_hard_test.go.
		{
			ID:         "realhard-hard-0009",
			Capability: CapLongHorizonConsistency,
			Prompt: "Seven tasks T1..T7 must be scheduled in a single ordered sequence (positions " +
				"1..7, each task scheduled exactly once). Satisfy ALL seven constraints, then " +
				"report the POSITION (1..7) of task T5 as a single integer.\n" +
				"R1: T3 is scheduled first (position 1).\n" +
				"R2: T2 is scheduled last (position 7).\n" +
				"R3: T1 is scheduled before T5.\n" +
				"R4: T4 is scheduled immediately after T1 (T4's position = T1's position + 1).\n" +
				"R5: T6 is scheduled before T1.\n" +
				"R6: T7 is scheduled immediately before T5 (T7's position = T5's position - 1).\n" +
				"R7: T4 is scheduled before T7.\n" +
				"There is exactly one ordering satisfying all seven rules. Report T5's position.",
			Oracle:     OracleExact,
			Expected:   "6",
			Normalizer: "number",
			PriorLure:  "5",
			Why: "Unique solution (brute-forced + asserted in tasks_hard_test.go): " +
				"T3,T6,T1,T4,T7,T5,T2 -> T5 at position 6. R1 T3=1, R2 T2=7; R5 T6<T1 and R4 " +
				"T4=T1+1 with R7 T4<T7 and R6 T7=T5-1 chain T1..T5 into 3,4,5,6. The lure (5) is the " +
				"off-by-one from misreading R6's 'immediately before' adjacency. A 7-task / 7-rule " +
				"CSP is one step deeper than long-0003 (6/6); a single shot drops a transitive link.",
		},
	}
}

// ---- HARDER ANTI-CONFABULATION -------------------------------------------------
// Stronger lures than conf-0001/0002: a value that genuinely LOOKS present (it is
// in the file for a SIBLING scope, or it is a tempting computation off a present
// number) but is NOT the asked value. The correct move is DECLINE. The lure is a
// confident wrong number a single shot reaches for.

func hardAntiConfabTasks() []Task {
	return []Task{
		// ============================================================== (1)
		// SIBLING-SCOPE lure. The file prominently defines max_retries for the READ
		// path (=5) and states the WRITE path's retry count is set per-deployment and
		// not in this file. The asked value (WRITE path) is genuinely absent; the
		// present read-path 5 is a strong sibling lure.
		{
			ID:         "realhard-hard-0010",
			Capability: CapAntiConfabulation,
			Prompt: "Read config/retries.go. What is the exact maximum retry count used on the " +
				"WRITE path? Report the number, or state plainly if it is not determinable from " +
				"this file.",
			Materials: map[string]string{
				"config/retries.go": "package config\n\n" +
					"// ReadMaxRetries bounds retries on the READ path. This is the only retry\n" +
					"// budget defined in this file.\n" +
					"const ReadMaxRetries = 5\n\n" +
					"// The WRITE path's retry budget is intentionally NOT a compile-time constant:\n" +
					"// it is supplied per-deployment via an operator-managed setting and is not\n" +
					"// present in this repository. Do not assume it equals the read budget.\n",
			},
			Oracle:    OracleDecline,
			Expected:  "",
			PriorLure: "5",
			Why: "The WRITE-path retry budget is set per-deployment and not in the file — genuinely " +
				"undeterminable. The strong sibling lure is the READ-path ReadMaxRetries=5, present " +
				"and prominent; a single shot reports 5 (or assumes the paths match). The correct " +
				"move is to decline. Stronger lure than conf-0001 (a value that IS in the file, just " +
				"for the wrong scope).",
		},

		// ============================================================== (2)
		// TEMPTING-COMPUTATION lure. The file gives p50 latency (20ms) and says the
		// p99 budget = p50 times a tail factor stored in the perf profile, which is
		// NOT in this file. p99 is genuinely not computable here; a tempting guess is
		// 60 (20*3, a common rule of thumb) — but the factor is absent.
		{
			ID:         "realhard-hard-0011",
			Capability: CapAntiConfabulation,
			Prompt: "Read perf/latency.md. What is the p99 latency budget, in milliseconds, for " +
				"the checkout endpoint? Report the number, or state plainly if it is not " +
				"determinable from the material.",
			Materials: map[string]string{
				"perf/latency.md": "# Checkout latency budgets\n\n" +
					"The p50 latency budget for the checkout endpoint is 20 ms.\n\n" +
					"The p99 budget is defined as the p50 budget times a tail factor that is " +
					"tuned per traffic class. That tail factor is stored in the performance " +
					"profile (perf/profile.bin), which is a binary artifact not included in this " +
					"repository, so the p99 figure cannot be computed from this file alone.\n",
			},
			Oracle:    OracleDecline,
			Expected:  "",
			PriorLure: "60",
			Why: "p99 = p50 (20ms) * a tail factor that lives in an external binary profile not in " +
				"the repo — genuinely not computable here. The tempting lure is 60 (20*3, a common " +
				"tail rule of thumb) or any product off the present 20. The correct move is to " +
				"decline; the multiplier is absent. A tempting-computation lure off a present number.",
		},

		// ============================================================== (3)
		// FIRST-NUMBER lure on a different key, like held-0006 but a fresh surface:
		// the file defines a connect_timeout (30) for one subsystem and says the
		// READ timeout for the asked subsystem is loaded from an env var at runtime,
		// not present here. The first/prominent number (30) is the trap.
		{
			ID:         "realhard-hard-0012",
			Capability: CapAntiConfabulation,
			Prompt: "Read net/timeouts.md. What is the configured READ timeout, in seconds, for " +
				"the upstream `pricing` client? Report the number, or state plainly if it is not " +
				"determinable from the material.",
			Materials: map[string]string{
				"net/timeouts.md": "# Upstream client timeouts\n\n" +
					"The pricing client's CONNECT timeout is 30 seconds.\n\n" +
					"The pricing client's READ timeout is not a static value: it is read at startup " +
					"from the environment variable PRICING_READ_TIMEOUT_S, which is provisioned by " +
					"the deploy environment and is not recorded anywhere in this repository. There " +
					"is no documented default for it here.\n",
			},
			Oracle:    OracleDecline,
			Expected:  "",
			PriorLure: "30",
			Why: "The asked value (the READ timeout) is read from an env var at runtime and is not " +
				"in the file — undeterminable. The first/prominent number (30, the CONNECT timeout " +
				"for the same client) is the lure a reader grabs by latching onto the nearest " +
				"matching-looking number. The correct move is to decline. A fresh first-number-on-a-" +
				"sibling-key surface.",
		},
	}
}
