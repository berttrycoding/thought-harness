package realhard

// longHorizonTasks — LONG-HORIZON CONSISTENCY (Capability =
// CapLongHorizonConsistency).
//
// Many consistent steps where a single-shot bare model loses the thread, drops a
// constraint, or contradicts an earlier deduction. The harness's focus / graph
// structure should hold the chain. The answer is a single deterministic value
// reachable only by carrying EVERY constraint through to the end — drop one and
// you land on the lure.
//
// CALIBRATION NOTE (2026-06-18): an earlier, shorter version of these tasks was
// ACED by bare sonnet (3/3) — too easy, no headroom. Per the brief ("if sonnet
// does not fail, go harder + report it") these were hardened: longer chains,
// more interacting constraints, and a mid-chain rule-CHANGE so a one-shot that
// applies the early rule throughout lands on the lure. Re-derived ground truth
// is proven in code in oracle_test.go (TestLongHorizonGroundTruthArithmetic).
//
// These are pure reasoning tasks (no file reads needed); the material is in the
// prompt. Ground-truth is exact / numeric.

func longHorizonTasks() []Task {
	return []Task{
		// ---- A 12-event ledger with TWO reversals, a rule-CHANGE on fees, and a
		// percentage interest step. The lure applies the original flat fee
		// throughout and forgets the second reversal.
		{
			ID:         "realhard-long-0001",
			Capability: CapLongHorizonConsistency,
			Prompt: "Reconcile account A through these 12 events IN ORDER. A starts at 0. " +
				"Track ONLY account A's integer balance. Read every rule carefully; one rule " +
				"CHANGES partway through. Report A's FINAL balance as an integer.\n" +
				"Fee rule (v1): each 'fee' event charges a FLAT 10 to A.\n" +
				"E1: deposit 200 into A.\n" +
				"E2: transfer 40 from A to B.\n" +
				"E3: fee charged to A.\n" +
				"E4: transfer 25 from C to A.\n" +
				"E5: deposit 60 into A.\n" +
				"E6: REVERSE event E2 entirely (undo the E2 transfer).\n" +
				"E7: fee charged to A.\n" +
				"E8: RULE CHANGE: from this point on, a 'fee' event charges 5 (not 10). " +
				"This does NOT retroactively change E3 or E7.\n" +
				"E9: transfer 70 from A to B.\n" +
				"E10: fee charged to A.\n" +
				"E11: REVERSE event E9 entirely (undo the E9 transfer).\n" +
				"E12: deposit 15 into A.\n" +
				"Report A's final balance.",
			Oracle:     OracleExact,
			Expected:   "275",
			Normalizer: "number",
			PriorLure:  "270",
			Why: "0 +200 -40 -10(E3) +25 +60 +40(E6 reverse) -10(E7) [E8 rule change: fee now 5] " +
				"-70 -5(E10) +70(E11 reverse) +15 = 275 (proven in oracle_test.go). The lure " +
				"(270) applies the v1 flat-10 fee to E10 (forgetting the E8 rule change: 275-5=270). " +
				"A 12-step chain with a mid-chain rule change is where a single shot drifts; the " +
				"harness must hold every event.",
		},
		// ---- A multi-stage unit + rate chain with a deliberate binary-vs-decimal
		// trap and an intermediate cap. The lure uses 1024 once or drops the cap.
		{
			ID:         "realhard-long-0002",
			Capability: CapLongHorizonConsistency,
			Prompt: "Carry the units carefully at EVERY step; one wrong unit changes the " +
				"answer. A pipeline has THREE stages, run in series, 8 hours per day:\n" +
				"Stage 1 ingests 4 files/second, each file 200 KB.\n" +
				"Stage 2 compresses Stage-1 output by exactly 50% (output bytes = half of " +
				"input bytes).\n" +
				"Stage 3 replicates Stage-2 output to 3 copies (output bytes = 3x its input).\n" +
				"Storage is billed in GB where 1 GB = 1000 MB and 1 MB = 1000 KB (DECIMAL, " +
				"NOT 1024). How many whole GB does ONE day of Stage-3 output produce? " +
				"Report the GB as an integer, rounded DOWN (floor).",
			Oracle:     OracleExact,
			Expected:   "34",
			Normalizer: "number",
			PriorLure:  "32",
			Why: "Stage1: 4*200=800 KB/s * 28800 s = 23,040,000 KB. Stage2 *0.5 = 11,520,000 " +
				"KB. Stage3 *3 = 34,560,000 KB = 34,560 MB = 34.56 GB -> floor 34. The lure " +
				"(32) comes from using 1024 in one conversion (binary trap) or mis-ordering " +
				"the compress/replicate stages. Three sequential stage transforms + a unit " +
				"trap: a one-shot slips one; the harness carries them.",
		},
		// ---- A constraint-satisfaction seating/ordering puzzle with 7 interacting
		// rules. The answer is a single position number. The lure satisfies 6 of 7
		// rules (drops the last transitive constraint).
		{
			ID:         "realhard-long-0003",
			Capability: CapLongHorizonConsistency,
			Prompt: "Six services S1..S6 must be deployed in a single ordered sequence " +
				"(positions 1..6, each service deployed exactly once). Satisfy ALL of these " +
				"constraints, then report the POSITION (1..6) of service S4 as a single " +
				"integer.\n" +
				"R1: S6 is deployed first (position 1).\n" +
				"R2: S5 is deployed last (position 6).\n" +
				"R3: S1 is deployed before S2.\n" +
				"R4: S3 is deployed immediately after S1 (S3's position = S1's position + 1).\n" +
				"R5: S2 is deployed before S4.\n" +
				"R6: S4 is deployed immediately before S5 (S4's position = S5's position - 1).\n" +
				"There is exactly one ordering satisfying all six rules. Report S4's position.",
			Oracle:     OracleExact,
			Expected:   "5",
			Normalizer: "number",
			PriorLure:  "4",
			Why: "Unique solution (brute-forced + asserted in oracle_test.go): " +
				"S6,S1,S3,S2,S4,S5 -> S4 at position 5. R1 S6=1, R2 S5=6, R6 forces S4=5. " +
				"The S1,S3 adjacent pair (R4) with S1<S2 (R3) and S2<S4 (R5) then fixes " +
				"S1=2,S3=3,S2=4. The lure (4) puts S4 right after S2 (satisfying R3-R5) but " +
				"violates R6's 'immediately before S5'. A 6-rule CSP is where a single shot " +
				"drops a transitive constraint; the harness must hold all six.",
		},
	}
}
