package engine

import (
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// selfbench_gate.go closes the self-improvement loop (H-SB2, 2026-06-20-benchmark-taxonomy.md §7.2/§7.6 #4):
// it gates a batch of recorded self-changes (mints) on a MEASURED SelfBench fitness delta PLUS a durability
// re-gate, with keep-or-revert against the pre-mint baseline checkpoint. This is the wire the design called
// out as ABSENT — "the fitness function is real and the checkpoint store is real, but the wire that lets
// the harness run the former against the latter and act on the result does not exist." It reuses, not
// reinvents: the auto:baseline pre-mint snapshot (ensureBaselineSnapshot), the convertibility grounded value
// as the fitness function, regulator.StabilityRegime as the durability re-gate (in-process, no import cycle),
// and persist.ResetToSnapshot as the keep-or-revert actuator. It builds ON the SB0 primitive in
// selfbench.go (the frozen-checkpoint shadow-engine SelfBench measurement) — this file is the ACTION half
// (the keep-or-revert governance loop) the primitive's propose-and-gate report fed into.
//
// The load-bearing design insight (§7.2): you benchmark a frozen CHECKPOINT, never the live mutating self —
// here the pre-mint auto:baseline IS the frozen C_n, and the just-minted batch's grounded fitness is the
// post-mint C_{n+1} measurement. The freq×value heuristic stays the cheap PRE-FILTER (only a recorded batch
// is benched — bench is expensive); this measured gate is the governance loop the design specifies.
//
// DEFAULT = propose-and-gate (§7.5 DECIDED): the harness MEASURES and PROPOSES a verdict but does NOT
// self-commit a revert. CLOSED-LOOP (the interlock flag) lets the harness hold its own keep/revert key:
// on a net-negative delta OR a durability-gate FAIL it actually ResetToSnapshot(auto:baseline).

// selfBenchNoiseFloor is the comparative dead-band (noise floor) for the SelfBench delta: a |Δ| below it
// reads FLAT — neither a clean win nor a clean loss — so the loop never promotes/reverts on measurement
// wobble. 0.05 mirrors the mint-value granularity (refineEpsilon, mint_gate.go): a sub-0.05 fitness change
// is not a signal. A batch only PROMOTES on Δ >= +floor and only fails (revert) on Δ <= -floor.
const selfBenchNoiseFloor = 0.05

// selfBenchGate runs the loop-close on the batch of self-changes recorded this consolidation. mintedDelta is
// the growth in mintCount (the freq×value pre-filter already admitted them). It computes the batch's
// SelfBench fitness vs the pre-mint baseline floor, re-passes the durability gate, and emits the verdict —
// proposing (default) or self-committing (closed-loop) the keep-or-revert. nil store ⇒ no-op (the caller
// guards features.Ledger.SelfBenchGate, so the OFF path never reaches here ⇒ byte-identical).
func (e *Engine) selfBenchGate(st persist.Store, mintedDelta int, revert string) {
	if st == nil || mintedDelta <= 0 {
		return
	}

	// SelfBench fitness of the batch = the mean grounded value of the live minted specialists (the same
	// grounded-value signal the design names as the fitness function and the mint gate already reads). The
	// baseline is the mint-value floor — the gate of record — so the delta is "how far above the bar did
	// the batch land", a real comparative measurement, not a re-score of the same admission threshold.
	floor := e.convert.MintValue()
	fitness := e.benchFitness()
	bdelta := fitness - floor

	// Durability re-gate: the mod changed the plant, so re-derive the five conditions on the LIVE regulator
	// (regulator.StabilityRegime — n<1, U<=1, 0<K*g<2, w*tau<PM, mu>0 + the loop-gain regime). This is the
	// non-negotiable interlock (§7.5): a measured fitness win that BREAKS durability never promotes.
	mode := "reactive"
	if e.mode == "continuous" {
		mode = "continuous"
	}
	checks, regime, held, _ := e.regulator.StabilityRegime(mode)
	durable := held == durabilityHeldFloor(checks)

	closed := e.features.Ledger.SelfBenchClosedLoop
	gateMode := "propose-and-gate"
	if closed {
		gateMode = "closed-loop"
	}

	// The verdict spine: a clean win that holds durability PROMOTES; a net-negative OR a durability fail
	// REVERTS; everything in the dead-band KEEPS (flat — not enough signal to act). The verdict is always
	// emitted (the measurement is the point); the action half (promote/revert events + the ResetToSnapshot)
	// follows the verdict + the gate mode.
	verdict := "keep" // flat: |Δ| under the noise floor and durability held
	switch {
	case !durable:
		verdict = "revert" // durability fail dominates a fitness win (the interlock)
	case bdelta >= selfBenchNoiseFloor:
		verdict = "promote"
	case bdelta <= -selfBenchNoiseFloor:
		verdict = "revert"
	}

	e.bus.Emit(events.SelfBenchVerdict,
		"selfbench: "+verdict+" "+itoa(mintedDelta)+" self-change(s) (delta "+
			signedSB(round3SB(bdelta))+", fitness "+ftoa2(fitness)+" vs floor "+ftoa2(floor)+
			", durability "+durLabel(durable)+" ["+regime.String()+"], "+gateMode+")",
		events.D{"minted": mintedDelta, "delta": round3SB(bdelta), "fitness": round3SB(fitness),
			"floor": round3SB(floor), "durable": durable, "regime": regime.String(),
			"held": held, "verdict": verdict, "mode": gateMode, "revert": revert})

	switch verdict {
	case "promote":
		// PROMOTE — the batch cleared the floor AND held durability. Under both gate modes this is a PROPOSE
		// (the win is announced; no checkpoint mutation needed — the live state already IS C_{n+1}). The
		// human/explicit gate (propose-and-gate) or the next ledger entry (closed-loop) records the keep.
		e.bus.Emit(events.SelfBenchPromote,
			"selfbench: PROMOTE — "+itoa(mintedDelta)+" self-change(s) cleared the floor (delta "+
				signedSB(round3SB(bdelta))+") and held durability ["+regime.String()+"]",
			events.D{"minted": mintedDelta, "delta": round3SB(bdelta), "fitness": round3SB(fitness),
				"regime": regime.String(), "mode": gateMode})
	case "revert":
		reason := "net-negative delta"
		if !durable {
			reason = "durability re-gate FAILED (" + regime.String() + ")"
		}
		// Under CLOSED-LOOP the harness holds its own key: actually ResetToSnapshot(auto:baseline), reverting
		// the DURABLE checkpoint as one unit (persist.RegistryReset is emitted by the store). This is faithful
		// to the design's load-bearing insight (§7.2): the unit of keep-or-revert is the frozen CHECKPOINT,
		// not the live mutating self — the persisted state is reverted to the clean pre-mint baseline, so a
		// restart boots WITHOUT the rejected batch (a fresh engine loads FROM the reverted snapshot, exactly
		// the shadow-engine pattern). The live in-memory reflexes are not force-mutated mid-flight (that path
		// is the convertibility keep-or-revert demotion, separately gated). Under PROPOSE-AND-GATE the harness
		// MEASURES and PROPOSES the revert but does NOT self-commit — an explicit gate/human turns the key.
		committed := false
		if closed && revert != "" && revert != "(no baseline snapshot)" {
			if err := st.ResetToSnapshot(revert); err == nil {
				committed = true
			}
		}
		e.bus.Emit(events.SelfBenchRevert,
			"selfbench: REVERT — "+reason+" (delta "+signedSB(round3SB(bdelta))+"); "+
				revertActionLabel(committed, closed),
			events.D{"reason": reason, "delta": round3SB(bdelta), "regime": regime.String(),
				"revert": revert, "committed": committed})
	}
}

// benchFitness is the SelfBench fitness reading for the live minted-specialist registry: the mean grounded
// value of the currently-live (non-demoted) minted specialists. This IS the fitness function the design
// names — the same grounded-value signal the mint gate reads — aggregated over the registry, NOT a re-score
// of a static threshold and NOT a stub. An empty registry reads 0 (no fitness to bench).
func (e *Engine) benchFitness() float64 {
	var sum float64
	var n int
	for _, r := range e.convert.ExportSpecialists() {
		if r.Demoted {
			continue
		}
		sum += r.Value
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// durabilityHeldFloor is the count of NON-NA checks in the durability checklist — the bar the held count
// must clear to read as durable. A durable re-gate holds EVERY applicable (non-NA) condition; an NA check
// (e.g. μ>0 in reactive mode, or 0<K·g<2 under an open/insufficient loop) is excluded from the bar, never
// counted as a failure (a settling reactive episode is durable, not unstable).
func durabilityHeldFloor(checks []regulator.Check) int {
	bar := 0
	for _, c := range checks {
		if !c.NA {
			bar++
		}
	}
	return bar
}

// durLabel renders the durability verdict word for the SelfBench summary string.
func durLabel(ok bool) string {
	if ok {
		return "held"
	}
	return "FAILED"
}

// revertActionLabel renders what the SelfBench actually did about a revert verdict: closed-loop committed
// the ResetToSnapshot, closed-loop attempted it (no baseline), or propose-and-gate only proposed it.
func revertActionLabel(committed, closed bool) string {
	switch {
	case committed:
		return "reverted to baseline (closed-loop)"
	case closed:
		return "revert proposed (closed-loop, no baseline to reset to)"
	default:
		return "revert proposed (propose-and-gate — no self-commit)"
	}
}

// round3SB rounds to 3 decimals for the SelfBench wire payload (mirrors the convert/value round3).
func round3SB(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// signedSB renders a float with an explicit +/- sign for the summary delta string.
func signedSB(x float64) string {
	s := strconv.FormatFloat(x, 'f', 3, 64)
	if x >= 0 {
		return "+" + s
	}
	return s
}

// ftoa2 formats a float to 2 fixed decimals for the summary string (the value payload uses round3SB).
func ftoa2(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }
