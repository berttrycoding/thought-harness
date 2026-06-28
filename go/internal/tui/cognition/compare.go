package cognition

// compare.go — the G2 COMPARE benchmark distillation: the pure structured READ a power-ON vs
// power-OFF session pair delivers (mock 2026-06-20-tui-mockups-analysis.md §2; redesign
// 2026-06-20-shift-tab-analysis-redesign.md §7 — the definition of done). The §2 renderer
// (renderCompareBody) showed the diff but had no single SOURCE OF TRUTH for the benchmark VERDICT:
// "did ON beat OFF, by how much, and where did the two trajectories fork?" CompareReport IS that
// verdict — a deterministic function of two AnalysisRecords (PATTERN A: no model, no clock, no RNG,
// the surface never re-runs the substrate) — so the headline the user reads is one computed thing the
// renderer displays and the test asserts, not three scattered ad-hoc subtractions.
//
// Why a struct and not just more render lines: the user goal (redesign §7) is "load A + B and READ the
// verdict / latency delta / grounded+token deltas / decision fingerprint / divergence tick + why."
// Each of those is a CLAIM the benchmark makes; a struct lets the cognition-property test assert each
// claim against a REAL recorded ON/OFF pair (engine/analysis_compare_property_test.go) rather than
// scraping rendered ANSI. The renderer becomes a pure view over the report.

import (
	"fmt"
)

// CompareReport is the distilled power-ON/OFF benchmark over two recorded sessions (A vs B). Every
// field is the answer to one question the §2 mock asks. Pure-derived; A is conventionally the ON arm,
// B the OFF arm (the picker marks them), but the report is symmetric — Winner names whichever arm read
// better, so a mislabelled pair still reads honestly.
type CompareReport struct {
	// Winner is "A", "B", or "TIE" — which arm read better on the composite benchmark (outcome first,
	// then grounding, then responsiveness, the §2 reading order). TIE only when no axis separates them.
	Winner string

	// the outcome axis (the headline): did the arm reach a real solve?
	AVerdict, BVerdict string // "SOLVED" | "UNSOLVED" (the recorded verdict, never re-judged)
	VerdictDecisive    bool   // true when exactly one arm SOLVED — the cleanest possible win

	// the responsiveness axis: ticks from the user stimulus to DELIVER (lower is faster). Zero means the
	// arm never delivered (an UNSOLVED give-up), which reads as "no latency" — handled in FasterArm.
	ALatency, BLatency int
	LatencyDeltaTicks  int    // A - B (negative => A faster); 0 when one arm never delivered
	FasterArm          string // "A" | "B" | "" (empty when neither delivered or it's a wash)
	FasterPct          int    // |delta| / slower, as a percent — the "+47% faster" headline

	// the grounding axis: how much reality each arm imported (the anti-hallucination signal).
	AGrounded, BGrounded int
	ARefuted, BRefuted   int
	GroundedDelta        int // A - B

	// the cost axis: completion tokens out (lower is cheaper for the same outcome).
	ATokOut, BTokOut int
	TokenDeltaPct    int // (B - A) / A as a percent — "B used +56% tokens"; sign relative to A

	// the divergence: the first decision tick where the two trajectories chose differently, and a
	// one-line WHY. DivergenceTick is -1 when the two histories never forked (identical move order).
	DivergenceTick int
	DivergenceWhy  string

	// CrossSubstrate flags an UNSOUND pair: A and B ran on different substrates, so the diff measures
	// the backend, not the mechanism (the CLAUDE.md substrate-hygiene rule, §2 "COMPARE warns…"). A
	// sound benchmark holds the substrate fixed and flips one knob.
	CrossSubstrate bool

	// Headline is the one-sentence verdict the surface leads with — a deterministic template over the
	// fields above (NOT model-authored: this is the Pattern-A read; the §"generated assessment" prose is
	// a separate, deferred Pattern-B feature). Empty inputs => a neutral "no separation" line.
	Headline string
}

// BuildCompareReport distills the A/B benchmark from two recorded sessions. Pure: a deterministic
// function of the two records, no model / clock / RNG (the analysis surface reads a recording, it
// never re-runs the substrate). A is the ON arm by convention; the report stays honest if swapped.
func BuildCompareReport(a, b AnalysisRecord) CompareReport {
	r := CompareReport{
		AVerdict:       a.SolveVerdict,
		BVerdict:       b.SolveVerdict,
		ALatency:       a.ImpToDeliver,
		BLatency:       b.ImpToDeliver,
		AGrounded:      a.Grounded,
		BGrounded:      b.Grounded,
		ARefuted:       a.Refuted,
		BRefuted:       b.Refuted,
		GroundedDelta:  a.Grounded - b.Grounded,
		ATokOut:        a.TokOut,
		BTokOut:        b.TokOut,
		DivergenceTick: -1,
		CrossSubstrate: a.Substrate != "" && b.Substrate != "" && a.Substrate != b.Substrate,
	}

	// outcome axis — the headline. SOLVED outranks UNSOLVED; one-and-only-one solve is the decisive win.
	aSolved := a.SolveVerdict == "SOLVED"
	bSolved := b.SolveVerdict == "SOLVED"
	r.VerdictDecisive = aSolved != bSolved

	// responsiveness axis — both must have delivered for the latency comparison to mean anything; a
	// never-delivered arm has no latency, so it cannot be the FASTER arm (it simply did not finish).
	if a.ImpToDeliver > 0 && b.ImpToDeliver > 0 {
		r.LatencyDeltaTicks = a.ImpToDeliver - b.ImpToDeliver
		switch {
		case r.LatencyDeltaTicks < 0:
			r.FasterArm = "A"
			r.FasterPct = (-r.LatencyDeltaTicks) * 100 / b.ImpToDeliver
		case r.LatencyDeltaTicks > 0:
			r.FasterArm = "B"
			r.FasterPct = r.LatencyDeltaTicks * 100 / a.ImpToDeliver
		}
	}

	// cost axis — relative to A's spend (the ON arm is the reference the report is built around).
	if a.TokOut > 0 {
		r.TokenDeltaPct = (b.TokOut - a.TokOut) * 100 / a.TokOut
	}

	// the divergence tick + why (reuse the §2 helper, the single fork-finder).
	r.DivergenceTick, r.DivergenceWhy = divergenceTick(a.Decisions, b.Decisions)

	r.Winner = pickWinner(aSolved, bSolved, r.FasterArm, a, b)
	r.Headline = composeHeadline(r)
	return r
}

// pickWinner applies the §2 reading order: a decisive solve wins outright; with the outcome tied, the
// arm that imported more reality (grounded more) wins (the anti-guessing signal); with grounding tied,
// the faster-to-deliver arm wins; otherwise it is a TIE. This is a fixed precedence, not a learned
// weight — the surface reports a verdict, it does not optimise one.
func pickWinner(aSolved, bSolved bool, faster string, a, b AnalysisRecord) string {
	if aSolved != bSolved {
		if aSolved {
			return "A"
		}
		return "B"
	}
	if a.Grounded != b.Grounded {
		if a.Grounded > b.Grounded {
			return "A"
		}
		return "B"
	}
	if faster != "" {
		return faster
	}
	return "TIE"
}

// composeHeadline writes the one-line Pattern-A verdict. Deterministic template (NOT a model call):
// it states who won and on which axis, and appends the substrate-hygiene caveat when the pair is
// cross-substrate (so a misread is never silent — the §2 / CLAUDE.md rule).
func composeHeadline(r CompareReport) string {
	var s string
	switch {
	case r.Winner == "TIE":
		s = "no separation — A and B read the same on every axis"
	case r.VerdictDecisive:
		win, lose := r.Winner, "B"
		if r.Winner == "B" {
			lose = "A"
		}
		s = fmt.Sprintf("%s wins: %s SOLVED while %s did not", r.Winner, win, lose)
		if r.FasterArm == r.Winner && r.FasterPct > 0 {
			s += fmt.Sprintf(", and %d%% faster to DELIVER", r.FasterPct)
		}
	default:
		// outcome tied — the win is on grounding or speed.
		switch {
		case r.GroundedDelta != 0:
			s = fmt.Sprintf("%s wins on grounding (imported %d more reality checks)", r.Winner, abs(r.GroundedDelta))
		case r.FasterArm != "":
			s = fmt.Sprintf("%s wins on speed (%d%% faster to DELIVER)", r.Winner, r.FasterPct)
		default:
			s = r.Winner + " wins narrowly"
		}
	}
	if r.CrossSubstrate {
		s += " — CAVEAT: cross-substrate pair, the diff measures the backend, not the mechanism"
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// CompareHeadlineLine renders the report's one-line verdict as a tone-coded row for the §2 panel: the
// winning verdict reads green when there is a decisive solve, amber when the win is marginal, red on
// the cross-substrate caveat. Pure.
func CompareHeadlineLine(r CompareReport) string {
	tone := colOk
	switch {
	case r.CrossSubstrate:
		tone = colErr
	case r.Winner == "TIE" || !r.VerdictDecisive:
		tone = colWarn
	}
	return label("verdict") + txt(r.Headline, tone).render()
}

// fmtTokK formats a token count as "9k" (rounded thousands), the §2 token row convention.
func fmtTokK(n int) string { return fmt.Sprintf("%dk", n/1000) }

// fingerprintLine renders one arm's decision fingerprint ("A THINK×7 BRANCH×4 …"), the §2 decision row.
func fingerprintLine(arm string, ds []DecisionEvent) string {
	return txt(arm+" "+moveCounts(ds), colSubtext).render()
}
