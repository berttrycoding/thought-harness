package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cost"
)

// ReportHeader is the run-level context the feasibility report opens with (spec
// §5.7 report renderer: backend, model, K, temp, total model-call count,
// wall-clock).
type ReportHeader struct {
	// Backend is "test" or "llm".
	Backend string
	// Model is the resolved model id (empty / "-" for the test double).
	Model string
	// LLMURL is the base URL when Backend == "llm".
	LLMURL string
	// KTierA, KTierB are the replay counts per tier.
	KTierA, KTierB int
	// Temp is the fixed sampling temperature.
	Temp float64
	// Tiers is the human label of which tiers ran ("A", "B", or "A+B").
	Tiers string
	// Mechanisms is the list of mechanisms that ran, in order.
	Mechanisms []benchtypes.Mechanism
	// ModelCalls is the total backend/model-call count across every arm × item ×
	// replay (the campaign-cost figure).
	ModelCalls int
	// Wall is the measured wall-clock of the whole run.
	Wall time.Duration
	// SeedBase is the replay seed base (seed = SeedBase + r).
	SeedBase int64
	// Cost is the campaign-wide cost breakdown (per-run total + per-role + per-model
	// token/USD aggregation, priced against the rate card). Nil ⇒ the COST section is
	// omitted (e.g. a degenerate run with no model calls).
	Cost *cost.Breakdown
	// PerTick is the per-engine-tick spend rollup (the WF-E baseline headline:
	// mean/median LLM calls + tokens per tick). Zero (Ticks==0) ⇒ the per-tick line is
	// omitted (no model call fired on any tick).
	PerTick cost.PerTick
	// RateModel is the model id chosen for the PROJECTED-$ figure (--rate-model): for
	// a LOCAL (unpriced) run, the report still shows what the same token counts would
	// cost on this API model — the local-vs-API delta. Empty ⇒ no projection line.
	RateModel string
}

// Render writes the full plain-text feasibility report (no emoji, no lipgloss —
// this is the bench layer): the header, then one per-mechanism block per
// MechResult in the order given. results is keyed display order by the caller.
func Render(h ReportHeader, results []MechResult) string {
	var b strings.Builder

	line := strings.Repeat("=", 78)
	sub := strings.Repeat("-", 78)

	b.WriteString(line + "\n")
	b.WriteString("MEASURING-STICK PILOT — FEASIBILITY REPORT\n")
	b.WriteString(line + "\n")
	model := h.Model
	if model == "" {
		model = "-"
	}
	fmt.Fprintf(&b, "backend          : %s\n", h.Backend)
	fmt.Fprintf(&b, "model            : %s\n", model)
	if h.Backend == "llm" && h.LLMURL != "" {
		fmt.Fprintf(&b, "llm-url          : %s\n", h.LLMURL)
	}
	fmt.Fprintf(&b, "tiers            : %s\n", h.Tiers)
	fmt.Fprintf(&b, "replays (K)      : Tier-A=%d  Tier-B=%d\n", h.KTierA, h.KTierB)
	fmt.Fprintf(&b, "temperature      : %.3g\n", h.Temp)
	fmt.Fprintf(&b, "seed-base        : %d\n", h.SeedBase)
	fmt.Fprintf(&b, "mechanisms       : %s\n", joinMechs(h.Mechanisms))
	fmt.Fprintf(&b, "MDE              : %.3g (binary)   feasibility gate MDE/2 = %.3g\n", MDE, FeasibilityThreshold)
	fmt.Fprintf(&b, "total model-calls: %d\n", h.ModelCalls)
	fmt.Fprintf(&b, "wall-clock       : %s\n", h.Wall.Round(time.Millisecond))
	b.WriteString(line + "\n\n")

	b.WriteString("NOTE: the pilot N is tiny (6 Tier-A items / 2 Tier-B scenarios per mechanism).\n")
	b.WriteString("CIs are EXPECTED to be wide and usually not significant. The pilot establishes\n")
	b.WriteString("sigma_noise + direction + plumbing, NOT statistical power (spec §3, §4.7).\n\n")

	for _, r := range results {
		renderMech(&b, r, sub)
	}

	// Roll-up.
	b.WriteString(line + "\n")
	b.WriteString("VERDICT SUMMARY\n")
	b.WriteString(line + "\n")
	for _, r := range results {
		fmt.Fprintf(&b, "  %-22s tier-%s : %s\n", r.Mechanism, r.Tier, r.Verdict)
	}
	b.WriteString(line + "\n")

	// COST section (PART 3): total $ + the rate-card row, the I/O split, the
	// cache-hit %, the reasoning breakout, and the per-role / per-model tables. For a
	// LOCAL (unpriced) model it shows token counts + the projected-$ under --rate-model.
	renderCost(&b, h, line, sub)

	return b.String()
}

// renderCost writes the COST block: the campaign-wide $ total (or "rates unknown"
// for a local model), the input/output token split, the cache-hit %, the
// reasoning-token breakout, and the per-ROLE / per-MODEL aggregation — plus, for an
// unpriced (local) run, the PROJECTED $ under --rate-model so the local-vs-API
// delta is visible. A nil breakdown or a zero-token run omits the block.
func renderCost(b *strings.Builder, h ReportHeader, line, sub string) {
	bd := h.Cost
	if bd == nil || bd.Total.Tokens.Calls == 0 {
		return
	}
	t := bd.Total.Tokens

	b.WriteString("\n" + line + "\n")
	b.WriteString("COST\n")
	b.WriteString(line + "\n")
	if bd.Card != nil {
		fmt.Fprintf(b, "rate card        : %s\n", bd.Card.Source)
	}

	// Total $ (or the explicit unknown marker — never a silent $0).
	if bd.Total.HasUSD {
		fmt.Fprintf(b, "total $          : $%.6f  (%d model-calls, %d tokens)\n",
			bd.Total.USD, t.Calls, t.Total())
	} else {
		fmt.Fprintf(b, "total $          : rates unknown for %s — token counts only (%d model-calls, %d tokens)\n",
			strings.Join(bd.Total.UnknownModels, ", "), t.Calls, t.Total())
		// Projected $ under the chosen API rate model: the local-vs-API delta.
		if h.RateModel != "" {
			if usd, ok := bd.ProjectUSD(h.RateModel); ok {
				fmt.Fprintf(b, "projected $      : $%.6f  (this run's tokens priced as %s via --rate-model)\n",
					usd, h.RateModel)
			} else {
				fmt.Fprintf(b, "projected $      : --rate-model %q not in the rate card (cannot project)\n", h.RateModel)
			}
		} else {
			fmt.Fprintf(b, "projected $      : (pass --rate-model deepseek-reasoner to project this local run's $)\n")
		}
	}

	// I/O split (input vs output token share) + cache-hit % + reasoning breakout.
	fmt.Fprintf(b, "I/O split        : input %d (%.1f%%)  output %d (%.1f%%)\n",
		t.TotalIn(), 100*t.InputShare(), t.Out, 100*t.OutputShare())
	fmt.Fprintf(b, "  input detail   : uncached(miss) %d  cached(hit) %d\n", t.UncachedIn, t.CachedIn)
	fmt.Fprintf(b, "cache-hit %%      : %.1f%%  (cached_in %d / total_in %d)\n",
		100*t.CacheHitFraction(), t.CachedIn, t.TotalIn())
	fmt.Fprintf(b, "reasoning tokens : %d  (%.1f%% of output)\n", t.Reasoning, 100*t.ReasoningShare())

	// PER-TICK spend (the WF-E baseline headline: model spend per engine tick). Mean +
	// median over every populated (run, tick) cell — a tick that fired >=1 model call.
	if pt := h.PerTick; pt.Ticks > 0 {
		fmt.Fprintf(b, "per-tick spend   : calls/tick mean %.2f median %.1f   tokens/tick mean %.1f median %.1f   (over %d populated ticks)\n",
			pt.MeanCalls, pt.MedianCalls, pt.MeanTokens, pt.MedianTokens, pt.Ticks)
	}

	// Per-MODEL table.
	fmt.Fprintf(b, "%s\n", sub)
	fmt.Fprintf(b, "  per MODEL:\n")
	fmt.Fprintf(b, "    %-28s %8s %8s %8s %8s   %s\n", "model", "calls", "in", "out", "reason", "$")
	for _, m := range bd.SortedModels() {
		p := bd.ByModel[m]
		fmt.Fprintf(b, "    %-28s %8d %8d %8d %8d   %s\n",
			trunc(m, 28), p.Tokens.Calls, p.Tokens.TotalIn(), p.Tokens.Out, p.Tokens.Reasoning, usd(p))
	}

	// Per-ROLE table.
	fmt.Fprintf(b, "  per ROLE:\n")
	fmt.Fprintf(b, "    %-28s %8s %8s %8s %8s   %s\n", "role", "calls", "in", "out", "reason", "$")
	for _, role := range bd.SortedRoles() {
		p := bd.ByRole[role]
		fmt.Fprintf(b, "    %-28s %8d %8d %8d %8d   %s\n",
			trunc(role, 28), p.Tokens.Calls, p.Tokens.TotalIn(), p.Tokens.Out, p.Tokens.Reasoning, usd(p))
	}
	b.WriteString(line + "\n")
}

// usd renders a Priced cell's $ figure, or the explicit "rates-unknown" marker when
// the model is not in the rate card (never a silent $0.00).
func usd(p cost.Priced) string {
	if p.HasUSD {
		return fmt.Sprintf("$%.6f", p.USD)
	}
	return "rates-unknown"
}

// renderMech writes one per-mechanism × tier block (spec §5.7 per-mechanism lift
// table + the §4.1 feasibility line).
func renderMech(b *strings.Builder, r MechResult, sub string) {
	fmt.Fprintf(b, "%s\n", sub)
	fmt.Fprintf(b, "MECHANISM: %s   TIER: %s   (K=%d replays, %d items)\n", r.Mechanism, r.Tier, r.K, r.Items)
	fmt.Fprintf(b, "%s\n", sub)

	// Phase-0 sigma_noise.
	avg := r.Phase0.SigmaHarnessAveraged()
	fmt.Fprintf(b, "  Phase-0 sigma_noise (within-item pass-indicator SD):\n")
	fmt.Fprintf(b, "    harness arm        : %.4f\n", r.Phase0.SigmaHarness)
	fmt.Fprintf(b, "    bare arm           : %.4f\n", r.Phase0.SigmaBare)
	fmt.Fprintf(b, "    paired (harn-bare) : %.4f\n", r.Phase0.SigmaPairedDiff)
	fmt.Fprintf(b, "    K-averaged (sigma/sqrt(K)) : %.4f\n", avg)
	gate := "PASS"
	if avg >= FeasibilityThreshold {
		gate = "FLAG (increase K or lower temp)"
	}
	fmt.Fprintf(b, "    feasibility gate (< %.3g) : %s\n", FeasibilityThreshold, gate)

	// Lure calibration (bare-arm failure rate per item; flag below the 0.5 floor).
	fmt.Fprintf(b, "  Lure calibration (bare-arm per-item failure rate; admit floor %.2f):\n", LureAdmitFloor)
	ids := sortedKeys(r.Phase0.LureRates)
	if len(ids) == 0 {
		fmt.Fprintf(b, "    (no bare-arm replays recorded)\n")
	}
	for _, id := range ids {
		flag := ""
		if r.Phase0.LureRates[id] < LureAdmitFloor {
			flag = "  <-- BELOW FLOOR (does not discriminate)"
		}
		fmt.Fprintf(b, "    %-32s %.2f%s\n", trunc(id, 32), r.Phase0.LureRates[id], flag)
	}
	if n := len(r.Phase0.LureBelowFloor); n > 0 {
		fmt.Fprintf(b, "    => %d item(s) below the admit floor\n", n)
	}

	// Lift contrasts.
	fmt.Fprintf(b, "  LIFT (majority-vote pass per item, paired):\n")
	renderLift(b, "harness - bare", r.HarnessMinusBare, true)
	if r.GateOffSupported {
		renderLift(b, "gate-on - gate-off", r.GateOnMinusGateOff, true)
	} else {
		fmt.Fprintf(b, "    gate-on - gate-off : UNSUPPORTED (no ablation toggle for %s yet; spec §5.8)\n", r.Mechanism)
	}

	// Isolation rate.
	if r.Isolation.Passes == 0 {
		fmt.Fprintf(b, "  Isolation rate       : n/a (0 harness passes to isolate)\n")
	} else {
		fmt.Fprintf(b, "  Isolation rate       : %.2f (%d/%d harness passes used the mechanism)\n",
			r.Isolation.Rate, r.Isolation.Isolated, r.Isolation.Passes)
	}

	// Verdict.
	fmt.Fprintf(b, "  VERDICT              : %s\n\n", r.Verdict)
}

// renderLift writes one contrast line: pass rates, the difference + BCa CI, and
// the McNemar discordant split + p.
func renderLift(b *strings.Builder, label string, l Lift, showMcNemar bool) {
	fmt.Fprintf(b, "    %-18s : passA=%.2f passB=%.2f  diff=%s  CI95=[%s, %s]  N=%d\n",
		label, l.PassA, l.PassB, f(l.Diff.Point), f(l.Diff.CILow), f(l.Diff.CIHigh), l.NPairs)
	if showMcNemar {
		fmt.Fprintf(b, "    %-18s   McNemar b=%d c=%d  p_exact=%.4f  p_mid=%.4f  OR=%s\n",
			"", l.McNemar.B, l.McNemar.C, l.McNemar.PExact, l.McNemar.PMid, f(l.McNemar.OddsRatio))
	}
}

// ---------------------------------------------------------------------------
// formatting helpers.
// ---------------------------------------------------------------------------

func f(x float64) string {
	switch {
	case math.IsNaN(x):
		return "n/a"
	case math.IsInf(x, 1):
		return "+inf"
	case math.IsInf(x, -1):
		return "-inf"
	default:
		return fmt.Sprintf("%+.3f", x)
	}
}

func joinMechs(ms []benchtypes.Mechanism) string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = m.String()
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
