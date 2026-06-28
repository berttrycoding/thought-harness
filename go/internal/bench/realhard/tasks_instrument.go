package realhard

// tasks_instrument.go — the OFFLINE INSTRUMENT-VALIDATION task set (A3).
//
// WHY IT EXISTS. The two A/B autonomy instruments — the sub-agent-beats-best-member
// GUARD (subagentguard.go) and the METR-style TIME-HORIZON (timehorizon.go) — are pure
// reducers, unit-tested against PLANTED inputs. But on an END-TO-END `--backend test`
// run of the live realhard suite they are VACUOUS, for a structural reason: every
// realhard task is pitched at the L4-L5 frontier (it needs a real model to answer), so
// the deterministic TEST DOUBLE (canned content, no cognition) FAILS all of them —
// every task scores p=0 on every arm. With no spread of solve-rates:
//   - the METR fit collapses to a flat zero slope (H50 -> 0s, a degenerate non-fit), and
//   - the guard has no per-task signal to resolve (the arms are all 0/K).
//
// THIS SET FIXES THE OFFLINE READ HONESTLY. It is a small bank of DOUBLE-SOLVABLE
// arithmetic tasks the test double's REAL `solver` primitive sub-agent computes
// deterministically — NOT a faked oracle, NOT a canned answer. The solver does ONE
// binary operation deterministically (12 + 30 = 42) and chokes on a 3+-operand /
// nested / mixed-precedence chain (it evaluates only the first pair). That is a GENUINE,
// measured difficulty gradient on the double:
//   - SHORT tasks = a single binary op the solver gets        -> p = 1 (solved)
//   - LONG  tasks = a multi-step chain the solver misses       -> p = 0 (failed)
// Placed on a SPREAD of human-minutes (HumanMin) — short=solved, long=failed — this
// gives the METR logistic a real NEGATIVE slope (longer = harder) and a real, non-
// degenerate H50; and it gives the guard real per-task (solved, K) counts to pair.
//
// THE HONEST CEILING (read before reading the guard verdict). The two ENGINE arms
// (harness vs single-strong) produce BYTE-IDENTICAL answers on the deterministic
// double: sub-agent fan-out (MaxParWidth / Subconscious.SubAgents — what
// disableSubAgentFanout flips) is OUTCOME-NEUTRAL by design (the goldens stay identical
// regardless of fan-out width; the speedup is concurrency, not a different conclusion —
// dispatch.go). So on `--backend test` the guard correctly resolves to INCONCLUSIVE (a
// REAL, non-NOT-APPLICABLE verdict: the arms run, are paired, the CI is computed) — it
// CANNOT honestly resolve to PASS/HOLDS-BACK, because that requires a LIVE model where
// fan-out changes the reasoning. PASS/HOLDS-BACK is proven on PLANTED inputs
// (subagentguard_test.go) and is a live-claude verdict. This set's job is to make the
// offline instruments NON-VACUOUS and the wiring provable, not to manufacture an
// arm difference the double cannot honestly express.
//
// IT IS NOT IN Tasks(). InstrumentValidationTasks is a SEPARATE bank, never appended to
// Tasks(), so it NEVER pollutes the live-claude headroom measurement (bare claude would
// ace this trivial arithmetic, diluting the headroom). It is consumed only via the
// SuiteConfig.Tasks / ABConfig.Tasks override (cmd/realhard --bank instrument).

// InstrumentValidationTasks returns the offline instrument-validation bank: a length-
// diverse set the test double's solver produces a real solved/failed SPREAD on, so the
// METR time-horizon fits a real H50 and the sub-agent guard returns a real verdict on a
// `--backend test` run. Deterministic; no model, no RNG.
func InstrumentValidationTasks() []Task {
	num := func(id, prompt, expected string, humanMin float64) Task {
		return Task{
			ID:         id,
			Capability: CapMultiHopGrounding, // a label only; HumanMin drives the METR x-axis here
			Prompt:     prompt,
			Oracle:     OracleNumericTolerance,
			Expected:   expected,
			Normalizer: "number",
			Tolerance:  0.5,
			HumanMin:   humanMin,
		}
	}
	return []Task{
		// ---- SHORT, single binary op: the solver computes these -> p = 1 (solved) ----
		num("realhard-instr-easy-01", "Compute 12 + 30. Report only the number.", "42", 1),
		num("realhard-instr-easy-02", "Compute 50 - 8. Report only the number.", "42", 2),
		num("realhard-instr-easy-03", "What is 7 * 6? Report only the number.", "42", 4),
		num("realhard-instr-easy-04", "Compute 84 / 2. Report only the number.", "42", 8),
		// ---- LONG, multi-step chain: the solver evaluates only the first pair -> p = 0 (failed) ----
		num("realhard-instr-hard-01", "Compute 100 + 200 + 300. Report only the total.", "600", 30),
		num("realhard-instr-hard-02", "Compute 2 * 3 + 4. Report only the number.", "10", 60),
		num("realhard-instr-hard-03", "Compute (2 + 3) * 4. Report only the number.", "20", 120),
		num("realhard-instr-hard-04", "Compute 1 + 2 + 3 + 4. Report only the total.", "10", 240),
	}
}
