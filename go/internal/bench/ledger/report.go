// report.go renders the ledger's per-mechanism lift table (spec §5.7, the
// "small report renderer"): harness−bare, on−off (the load-bearing contrast),
// isolation rate, the BCa CI, raw + BH-corrected p, and the keep/flag verdict.
//
// It is PLAIN TEXT — no emoji, no lipgloss, no color (CLAUDE.md "No emoji",
// "Keep the engine headless-pure"; the spec calls out "text only, no emoji,
// lipgloss-free (this is the bench layer, not the TUI)"). The renderer reads the
// LATEST active verdict per (mechanism, tier) straight off the ledger, so the
// table is the single-source-of-truth view of the campaign's keep decisions.
package ledger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/bench/types"
)

// ReportOptions tunes the rendered report. The zero value is valid (renders all
// mechanisms at the latest iteration found per mechanism, both tiers).
type ReportOptions struct {
	// IterK, if non-negative, restricts the table to verdicts recorded at that
	// campaign iteration. -1 (or any negative) means "the latest iteration present
	// per mechanism". Default via NewReportOptions is -1.
	IterK int
	// Title overrides the default report heading.
	Title string
}

// NewReportOptions returns the default options (latest iteration, default title).
func NewReportOptions() ReportOptions { return ReportOptions{IterK: -1, Title: ""} }

// Report renders the per-mechanism lift table from the ledger at dir-resolved
// state. It is the §5.7 report renderer. The output is deterministic: mechanisms
// sorted by name, then tier (A before B), reading only active verdict rows.
func (s *Store) Report(opts ReportOptions) (string, error) {
	rows, err := s.Resolved()
	if err != nil {
		return "", err
	}
	return renderReport(rows, s.path, opts), nil
}

// verdictKey identifies a verdict slot: one mechanism × tier × iteration.
type verdictKey struct {
	mech types.Mechanism
	tier types.Tier
	iter int
}

// renderReport is the pure formatter (no I/O), so it is unit-testable directly off
// a slice of resolved rows.
func renderReport(rows []Record, path string, opts ReportOptions) string {
	// Collect the latest active verdict per (mechanism, tier, iter). Append order is
	// campaign order, so a later row for the same key supersedes an earlier one.
	latest := map[verdictKey]Record{}
	// Track, per (mechanism, tier), the highest iteration seen (for IterK<0 "latest").
	maxIter := map[[2]string]int{}
	for _, r := range rows {
		if r.Kind != KindVerdict || r.Status != StatusActive {
			continue
		}
		k := verdictKey{r.Mechanism, r.Tier, r.IterK}
		latest[k] = r
		mt := [2]string{string(r.Mechanism), string(r.Tier)}
		if it, ok := maxIter[mt]; !ok || r.IterK > it {
			maxIter[mt] = r.IterK
		}
	}

	// Choose which (mechanism, tier) verdicts to show, at which iteration.
	type shown struct {
		key verdictKey
		rec Record
	}
	var picks []shown
	for k, rec := range latest {
		mt := [2]string{string(k.mech), string(k.tier)}
		want := opts.IterK
		if want < 0 {
			want = maxIter[mt]
		}
		if k.iter != want {
			continue
		}
		picks = append(picks, shown{k, rec})
	}
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].key.mech != picks[j].key.mech {
			return picks[i].key.mech < picks[j].key.mech
		}
		return picks[i].key.tier < picks[j].key.tier
	})

	var b strings.Builder
	title := opts.Title
	if title == "" {
		title = "KEEP-OR-REVERT LEDGER — per-mechanism lift"
	}
	b.WriteString(title)
	b.WriteString("\n")
	if path != "" {
		b.WriteString("source: ")
		b.WriteString(path)
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("=", 118))
	b.WriteString("\n")

	if len(picks) == 0 {
		b.WriteString("(no keep-rule verdicts recorded)\n")
		return b.String()
	}

	// Column headers. Widths are fixed so the table is monospace-aligned.
	// mechanism | tier | iter | harness-bare | on-off (BCa CI) | isol-rate | raw p | BH p | verdict
	header := fmt.Sprintf("%-20s %-4s %-4s %-13s %-26s %-11s %-9s %-9s %-7s",
		"mechanism", "tier", "iter", "harness-bare", "on-off  [BCa 95% CI]", "isol-rate", "raw p", "BH p", "verdict")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", 118))
	b.WriteString("\n")

	for _, p := range picks {
		r := p.rec
		var hb, onoff, isol string
		if r.Contrast != nil {
			hb = fmtPoint(r.Contrast.HarnessMinusBare.Point)
			onoff = fmtPoint(r.Contrast.GateOnMinusGateOff.Point) +
				fmtCI(r.Contrast.GateOnMinusGateOff.CILow, r.Contrast.GateOnMinusGateOff.CIHigh)
			isol = fmtRate(r.Contrast.IsolationRate.Point, r.IsolationFloor)
		} else {
			hb, onoff, isol = "    n/a", "          n/a", "    n/a"
		}
		row := fmt.Sprintf("%-20s %-4s %-4d %-13s %-26s %-11s %-9s %-9s %-7s",
			r.Mechanism, r.Tier, r.IterK,
			hb, onoff, isol,
			fmtP(r.RawP), fmtP(r.BHP),
			strings.ToUpper(verdictLabel(r.KeepVerdict)))
		b.WriteString(row)
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("=", 118))
	b.WriteString("\n")
	b.WriteString("KEEP = §4.6 all-conditions held (BH-sig + BCa-lower>MDE + isolation floor + no regression breach + beats best).\n")
	b.WriteString("FLAG = reportable partial / not-yet-validated (e.g. generic-scaffolding win, or a futility-stopped bank).\n")
	return b.String()
}

// fmtPoint renders a signed lift point estimate (a pass-rate difference in
// [-1,+1]) with three decimals and an explicit sign so a regression reads at a
// glance.
func fmtPoint(p float64) string {
	return fmt.Sprintf("%+.3f", p)
}

// fmtCI renders the bracketed BCa CI directly after the on-off point estimate.
func fmtCI(lo, hi float64) string {
	return fmt.Sprintf(" [%+.3f,%+.3f]", lo, hi)
}

// fmtRate renders an isolation rate and flags whether it cleared its floor. A rate
// at or above floor is plain; below floor is suffixed with "!" (the report's only
// flag glyph — ASCII, no emoji).
func fmtRate(rate, floor float64) string {
	if floor > 0 && rate < floor {
		return fmt.Sprintf("%.2f<%.2f!", rate, floor)
	}
	return fmt.Sprintf("%.2f", rate)
}

// fmtP renders a p-value, showing "—"-free "n/a" for an unset (0) value so an
// honestly-missing statistic is never read as p=0.
func fmtP(p float64) string {
	if p <= 0 {
		return "n/a"
	}
	if p < 0.001 {
		return "<0.001"
	}
	return fmt.Sprintf("%.4f", p)
}

// verdictLabel normalizes a stored verdict string to a known label, defaulting to
// the raw value so a future verdict kind still renders something honest.
func verdictLabel(v string) string {
	switch v {
	case VerdictKeep:
		return "keep"
	case VerdictFlag:
		return "flag"
	case "":
		return "n/a"
	default:
		return v
	}
}
