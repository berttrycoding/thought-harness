// Package campaign holds the W5 scaling-campaign decision logic: given a batch's bench A/B result
// (baseline arm vs with-batch arm) it applies the keep-or-revert rule — the formalization of "does
// this batch make the harness SMARTER or CHEAPER, with statistical significance?". Deterministic and
// model-free: the bench produces the numbers, this decides on them. The orchestration that runs the
// bench + funnel + ledger sequences this; the rule itself is the testable core.
package campaign

import (
	"fmt"
	"math"
)

// Decision is the verdict a batch faces after the A/B bench.
type Decision int

const (
	Revert Decision = iota // the batch is reverted (Tier-1 regression, a real capability regression, or no value)
	Keep                   // the batch is kept (significantly smarter, or cheaper at flat capability)
	Margin                 // ambiguous (a promising-but-unproven lean) — the human decides
)

func (d Decision) String() string {
	switch d {
	case Keep:
		return "KEEP"
	case Margin:
		return "MARGIN"
	default:
		return "REVERT"
	}
}

// ArmResult is ONE bench arm's outcome on the held-out task suite. PerItem is the per-task pass/fail
// in the SAME item order as the other arm (the pairing McNemar's test needs). Tokens/Calls are the
// arm's total spend (for the efficiency axis).
type ArmResult struct {
	PerItem          []bool // task i solved?  (paired with the other arm)
	Tokens           int    // prompt + completion (total)
	CompletionTokens int    // completion ONLY — the CACHE-IMMUNE efficiency signal (cached input dominates
	//                         total on a high-cache substrate like claude; a minted skill saves completion)
	Calls int
}

// CompletionPerSolved is the cache-immune efficiency metric (the research finding): completion tokens per
// task solved. On a high-cache substrate the total-token delta is mostly cached INPUT noise; a real skill
// saving shows in COMPLETION/synthesise tokens. +Inf when nothing solved.
func (a ArmResult) CompletionPerSolved() float64 {
	s := a.Solved()
	if s == 0 {
		return math.Inf(1)
	}
	return float64(a.CompletionTokens) / float64(s)
}

// Solved counts the passed tasks.
func (a ArmResult) Solved() int {
	n := 0
	for _, ok := range a.PerItem {
		if ok {
			n++
		}
	}
	return n
}

// Total is the number of tasks attempted.
func (a ArmResult) Total() int { return len(a.PerItem) }

// PassRate is the capability score (fraction solved); 0 for an empty suite.
func (a ArmResult) PassRate() float64 {
	if a.Total() == 0 {
		return 0
	}
	return float64(a.Solved()) / float64(a.Total())
}

// TokensPerSolved is the efficiency metric — tokens spent per task SOLVED. +Inf when nothing solved
// (you cannot be efficient at zero capability), so a 0-solved arm never reads as "cheaper".
func (a ArmResult) TokensPerSolved() float64 {
	s := a.Solved()
	if s == 0 {
		return math.Inf(1)
	}
	return float64(a.Tokens) / float64(s)
}

// KeepRule is the decision configuration: the significance level for the paired capability test and
// the minimum tokens-per-solved improvement that counts as "cheaper" (so float noise is not a win).
type KeepRule struct {
	Alpha         float64 // significance level for McNemar (default 0.05)
	MinEfficiency float64 // min tokens-per-solved reduction to count as cheaper (default 1.0)
}

// DefaultKeepRule is the standard configuration.
func DefaultKeepRule() KeepRule { return KeepRule{Alpha: 0.05, MinEfficiency: 1.0} }

// Verdict is the full decision with the numbers behind it (the ledger evidence + the human-readable
// reason).
type Verdict struct {
	Decision        Decision
	Lift            float64 // capability lift: with-batch pass-rate − baseline pass-rate
	EfficiencyDelta float64 // tokens-per-solved REDUCTION (positive = cheaper)
	McNemarP        float64 // two-sided p of the paired capability change (1.0 when no discordant pairs)
	Fixed, Broke    int     // tasks the batch FIXED (baseline-fail→batch-pass) / BROKE (baseline-pass→batch-fail)
	Tier1Regressed  bool
	Reason          string
}

// Evaluate applies the keep-or-revert rule (W5). The order is the gate ladder:
//  1. Tier-1 retrieval regressed            → REVERT (the batch dilutes its own registry's retrieval)
//  2. a SIGNIFICANT capability regression    → REVERT (it breaks more than it fixes, for real)
//  3. a SIGNIFICANT capability lift          → KEEP   (smarter)
//  4. flat capability, CHEAPER               → KEEP   (cheaper at equal capability — client-confirmed)
//  5. flat capability, COSTLIER              → REVERT (no gain, costs more)
//  6. a non-significant positive lean, flat $ → MARGIN (promising but unproven — the human decides)
//  7. otherwise (no smarter, no cheaper)      → REVERT (filler)
//
// PerItem slices must be paired (same length, same item order). A length mismatch is a programming
// error and yields REVERT with a clear reason (never a silent keep).
func Evaluate(baseline, withBatch ArmResult, tier1Pass bool, rule KeepRule) Verdict {
	if rule.Alpha <= 0 {
		rule.Alpha = 0.05
	}
	if rule.MinEfficiency <= 0 {
		rule.MinEfficiency = 1.0
	}
	v := Verdict{Tier1Regressed: !tier1Pass}

	if baseline.Total() != withBatch.Total() || baseline.Total() == 0 {
		v.Decision = Revert
		v.Reason = "unpaired or empty A/B arms — cannot evaluate"
		return v
	}

	v.Lift = withBatch.PassRate() - baseline.PassRate()
	v.EfficiencyDelta = baseline.TokensPerSolved() - withBatch.TokensPerSolved()
	if math.IsInf(v.EfficiencyDelta, 0) || math.IsNaN(v.EfficiencyDelta) {
		v.EfficiencyDelta = 0 // an Inf (one arm solved nothing) is not a measurable efficiency change
	}
	v.Fixed, v.Broke = discordant(baseline.PerItem, withBatch.PerItem)
	v.McNemarP = mcNemarP(v.Fixed, v.Broke)
	significant := v.McNemarP < rule.Alpha

	// 1. Tier-1 retrieval integrity is the hard gate.
	if !tier1Pass {
		v.Decision = Revert
		v.Reason = "Tier-1 retrieval regressed (the batch displaced a correct rank-1)"
		return v
	}
	// 2/3. a significant capability change decides on its direction.
	if significant {
		if v.Broke > v.Fixed {
			v.Decision = Revert
			v.Reason = fmt.Sprintf("significant capability regression (broke %d, fixed %d, p=%.3f)", v.Broke, v.Fixed, v.McNemarP)
		} else {
			v.Decision = Keep
			v.Reason = fmt.Sprintf("SMARTER: significant lift (fixed %d, broke %d, p=%.3f, +%.1f%% pass)", v.Fixed, v.Broke, v.McNemarP, v.Lift*100)
		}
		return v
	}
	// 4/5. capability is flat — decide on efficiency.
	if v.EfficiencyDelta >= rule.MinEfficiency {
		v.Decision = Keep
		v.Reason = fmt.Sprintf("CHEAPER at flat capability (−%.0f tokens/solved)", v.EfficiencyDelta)
		return v
	}
	if v.EfficiencyDelta <= -rule.MinEfficiency {
		v.Decision = Revert
		v.Reason = fmt.Sprintf("no capability gain and COSTLIER (+%.0f tokens/solved)", -v.EfficiencyDelta)
		return v
	}
	// 6. a non-significant positive capability lean at flat cost — promising but unproven.
	if v.Fixed > v.Broke {
		v.Decision = Margin
		v.Reason = fmt.Sprintf("promising but unproven (fixed %d, broke %d, p=%.3f) — needs the human or more data", v.Fixed, v.Broke, v.McNemarP)
		return v
	}
	// 7. neither smarter nor cheaper.
	v.Decision = Revert
	v.Reason = "no measurable gain (capability flat, cost flat) — filler"
	return v
}

// discordant counts the McNemar discordant pairs over paired pass/fail: fixed = baseline-fail →
// batch-pass (the batch helped), broke = baseline-pass → batch-fail (the batch hurt).
func discordant(baseline, batch []bool) (fixed, broke int) {
	for i := range baseline {
		switch {
		case !baseline[i] && batch[i]:
			fixed++
		case baseline[i] && !batch[i]:
			broke++
		}
	}
	return fixed, broke
}

// mcNemarP is the two-sided p-value of the paired change via the continuity-corrected normal
// approximation of McNemar's test over the discordant pairs (fixed=c, broke=b). With no discordant
// pairs the change is undefined-significant → p=1 (not significant). For small discordant counts the
// normal approximation is conservative, which is the safe direction (it under-claims significance,
// so a batch must clear a real bar to be auto-kept).
func mcNemarP(fixed, broke int) float64 {
	n := fixed + broke
	if n == 0 {
		return 1.0
	}
	z := (math.Abs(float64(fixed-broke)) - 1.0) / math.Sqrt(float64(n))
	if z < 0 {
		z = 0 // the continuity correction can push a 1-pair difference negative; clamp (p→1)
	}
	return math.Erfc(z / math.Sqrt2) // two-sided
}
