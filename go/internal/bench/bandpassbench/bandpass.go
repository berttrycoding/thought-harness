// Package bandpassbench is the OFFLINE, DETERMINISTIC targeted mechanism bench for seam.band_pass
// (#11, metric = GROUNDING PRECISION). It is one of the two "own-bench" knobs from the B1
// config-search Phase-1 sweep that the reactive-knob probe could not score (it is not a probe knob —
// it shapes the INTAKE stream over ticks, not a single-shot decision, so it needs its own intake-stream
// suite).
//
// THE MECHANISM (already wired, default-OFF, byte-identical — internal/seams/bandpass.go +
// internal/engine/band_intake.go): the hidden-seam intake band-pass passes a raw candidate through a
// per-stream LPF·HPF filter over ticks and injects it NOW only if it is persistent ENOUGH (LPF high —
// not a one-tick flash-in-the-pan) AND novel ENOUGH (HPF high — not a stale restatement). A candidate
// that became persistent only AFTER the conscious left its anchor decision node is BUFFERED as a late
// injection (→ retracement) rather than dropped.
//
// THE BENCH QUESTION (grounding precision): does ON correctly SUPPRESS transient/noise candidates at
// the intake while PRESERVING persistent grounded ones (vs OFF letting the transient straight through)?
//
// SCOPE (honest, per the B1 oracle vet): "grounding precision" is a MECHANISM PROXY measured on a
// constructed intake stream — NOT an end-to-end grounding lift on a real task (that is a downstream
// claude / B2 question this deterministic bench cannot make). "PERSISTENT" here means a signal that
// primes LOW then RISES (the LPF clears its persistence band while the HPF is still novel). A signal
// that appears HIGH on its FIRST tick and sustains is SUPPRESSED by the LEGACY cold-start
// reference-seeding (HPF = x - x = 0 on the priming tick) — the spec-vs-impl divergence the B1 oracle
// vet surfaced (§2.1's HPF passes a novel step-edge; the legacy impl seeds the reference to x[0] and
// suppresses it). That divergence is now REPAIRED behind the opt-in seam.band_pass_coldstart knob (B1f):
// see TestBandPassBench_FirstAppearanceHighSuppressed_LegacyColdStart (the default still diverges, pinned)
// and TestBandPassBench_FirstAppearanceHighInjected_ColdStartFix (the fix injects the step-edge through
// BandPassIntakeProbeColdStart). DefaultSuite stays scoped to prime-low-then-rise persistent signals so
// the SIGNAL aggregate is independent of the cold-start mode (it never feeds a first-appearance-high
// stream), so the suite's preserve-rate is unchanged by B1f.
//
// HOW IT DRIVES THE REAL MECHANISM: each case is a probe STREAM run through engine.BandPassIntakeProbe
// (the thin exported wrapper over the REAL engine.bandPassIntake + the REAL seams.BandPass filter + the
// REAL pendingInj late-injection buffer) with band_pass OFF then ON. There is NO mock of the filter —
// a mutation that bypasses the LPF/HPF, the floor gate, or the displacement-buffer arm changes the
// measured kept/buffered counts, so the bench FAILS if the mechanism is bypassed.
//
// OFFLINE + DETERMINISTIC: the test double, the tick clock, no RNG, no model. The noise floor is ZERO;
// the "signal" is whether ON produces its intended CORRECT behavioural difference (suppress the
// transient, preserve the persistent) vs OFF.
package bandpassbench

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/engine"
)

// Role is what a stream domain SHOULD do under the band-pass — the per-stream ground truth the oracle
// scores against. A case labels each domain it uses.
type Role int

const (
	// RoleTransient is a one-tick (flash-in-the-pan) signal: it appears once, with no corroborating
	// follow-up. The band-pass MUST suppress it (it is noise — a single tick is not yet real). The
	// oracle's suppressed-noise count credits a transient that is NEVER injected NOW.
	RoleTransient Role = iota
	// RolePersistent is a corroborated signal: it appears across several ticks (rising then sustained),
	// so it clears the LPF persistence band. The band-pass MUST preserve it — either inject it NOW while
	// on its anchor line, or (if its anchor node was left behind) BUFFER it as a late injection. The
	// oracle's preserved-signal count credits a persistent stream that is injected OR buffered.
	RolePersistent
)

// String renders the role for the report.
func (r Role) String() string {
	if r == RoleTransient {
		return "transient"
	}
	return "persistent"
}

// Case is one band-pass probe scenario: a named stream + per-domain ground-truth roles + the tick
// stream fed to the REAL intake. The same Stream is run OFF then ON; the oracle scores both.
type Case struct {
	ID     string
	Desc   string
	Roles  map[string]Role         // domain -> what it SHOULD do under the band-pass
	Stream []engine.BandIntakeTick // the per-tick candidate stream (drives engine.BandPassIntakeProbe)
}

// armResult is one arm's (OFF or ON) measured behaviour on a case: per-domain whether it was ever
// injected NOW, ever buffered late, and the final buffered-late total.
type armResult struct {
	injectedNow   map[string]bool
	bufferedLate  map[string]bool
	finalBuffered int
}

// runArm runs a case's stream through the REAL intake with band_pass on==on and reduces the per-tick
// verdicts into per-domain injected/buffered facts. floor is the inject floor (the live default 0.05).
func runArm(c Case, on bool, floor float64) (armResult, error) {
	res, err := engine.BandPassIntakeProbe(c.Stream, on, floor)
	if err != nil {
		return armResult{}, err
	}
	ar := armResult{injectedNow: map[string]bool{}, bufferedLate: map[string]bool{}}
	prevBuffered := 0
	// Track which domain was presented at each tick so a buffered-late increment is attributed to the
	// domain(s) live at that tick (the displacement arm buffers the persistent displaced stream).
	for ti, tr := range res {
		for _, d := range tr.Kept {
			ar.injectedNow[d] = true
		}
		if tr.BufferedLateTot > prevBuffered {
			for _, cand := range c.Stream[ti].Cands {
				ar.bufferedLate[cand.Domain] = true
			}
		}
		prevBuffered = tr.BufferedLateTot
		ar.finalBuffered = tr.BufferedLateTot
	}
	return ar, nil
}

// preserved reports whether a persistent stream's signal SURVIVED this arm: it was injected NOW at
// least once OR buffered as a late injection (both are "preserved" — the signal reached the conscious,
// now or via retracement).
func (ar armResult) preserved(domain string) bool {
	return ar.injectedNow[domain] || ar.bufferedLate[domain]
}

// CaseScore is the per-case OFF/ON scoring: the suppressed-noise and preserved-signal counts for each
// arm, plus the per-case correctness verdict.
type CaseScore struct {
	ID   string
	Desc string

	Transients int // # transient domains in the case
	Persistent int // # persistent domains in the case

	OffSuppressed int // transients OFF correctly suppressed (OFF passes them -> usually 0)
	OnSuppressed  int // transients ON correctly suppressed
	OffPreserved  int // persistent OFF preserved
	OnPreserved   int // persistent ON preserved

	// Correct is the per-case structural verdict the oracle asserts: ON suppressed EVERY transient AND
	// preserved EVERY persistent stream. This is the exact behaviour the spec intends; it is a clean
	// structural check (counts), no fuzzy matching.
	Correct bool
}

// scoreCase runs a case OFF then ON through the REAL intake and scores the suppress/preserve contrast.
func scoreCase(c Case, floor float64) (CaseScore, error) {
	off, err := runArm(c, false, floor)
	if err != nil {
		return CaseScore{}, err
	}
	on, err := runArm(c, true, floor)
	if err != nil {
		return CaseScore{}, err
	}

	sc := CaseScore{ID: c.ID, Desc: c.Desc}
	allTransientSuppressed := true
	allPersistentPreserved := true
	for domain, role := range c.Roles {
		switch role {
		case RoleTransient:
			sc.Transients++
			// "suppressed" = NOT injected now (the transient must not reach the conscious as a live inject).
			if !off.injectedNow[domain] {
				sc.OffSuppressed++
			}
			if !on.injectedNow[domain] {
				sc.OnSuppressed++
			} else {
				allTransientSuppressed = false
			}
		case RolePersistent:
			sc.Persistent++
			if off.preserved(domain) {
				sc.OffPreserved++
			}
			if on.preserved(domain) {
				sc.OnPreserved++
			} else {
				allPersistentPreserved = false
			}
		}
	}
	sc.Correct = allTransientSuppressed && allPersistentPreserved
	return sc, nil
}

// Result is the whole bench reduction: per-case scores + the aggregate OFF/ON delta on the two
// precision rates.
type Result struct {
	Floor float64
	Cases []CaseScore

	TotalTransient  int
	TotalPersistent int

	OffSuppressed int
	OnSuppressed  int
	OffPreserved  int
	OnPreserved   int

	// The two precision-style rates per arm.
	OffSuppressRate float64 // OFF: transients suppressed / total transients
	OnSuppressRate  float64 // ON:  transients suppressed / total transients
	OffPreserveRate float64 // OFF: persistent preserved / total persistent
	OnPreserveRate  float64 // ON:  persistent preserved / total persistent

	CorrectCases int // cases where ON behaved exactly as the spec intends
}

// Run executes the bench over a suite at the given inject floor and returns the reduction. floor==0
// uses the live default (0.05).
func Run(suite []Case, floor float64) (Result, error) {
	if floor <= 0 {
		floor = 0.05
	}
	r := Result{Floor: floor}
	for _, c := range suite {
		sc, err := scoreCase(c, floor)
		if err != nil {
			return Result{}, err
		}
		r.Cases = append(r.Cases, sc)
		r.TotalTransient += sc.Transients
		r.TotalPersistent += sc.Persistent
		r.OffSuppressed += sc.OffSuppressed
		r.OnSuppressed += sc.OnSuppressed
		r.OffPreserved += sc.OffPreserved
		r.OnPreserved += sc.OnPreserved
		if sc.Correct {
			r.CorrectCases++
		}
	}
	r.OffSuppressRate = rate(r.OffSuppressed, r.TotalTransient)
	r.OnSuppressRate = rate(r.OnSuppressed, r.TotalTransient)
	r.OffPreserveRate = rate(r.OffPreserved, r.TotalPersistent)
	r.OnPreserveRate = rate(r.OnPreserved, r.TotalPersistent)
	return r, nil
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Verdict is the honest per-mechanism conclusion: SIGNAL iff ON measurably + CORRECTLY improves the
// metric over OFF (it suppresses more noise AND preserves the persistent signal AND every case behaves
// as the spec intends), else NO-SIGNAL.
func (r Result) Verdict() (signal bool, line string) {
	noiseGain := r.OnSuppressRate - r.OffSuppressRate
	preserveOK := r.OnPreserveRate >= r.OffPreserveRate // ON must not LOSE persistent signal
	allCorrect := r.CorrectCases == len(r.Cases) && len(r.Cases) > 0
	if noiseGain > 0 && preserveOK && allCorrect {
		return true, fmt.Sprintf(
			"SIGNAL — ON suppresses %.0f%% of transient noise (OFF %.0f%%, +%.0f pp) while preserving "+
				"%.0f%% of persistent signal (OFF %.0f%%); every case (%d/%d) behaves exactly as the spec intends.",
			r.OnSuppressRate*100, r.OffSuppressRate*100, noiseGain*100,
			r.OnPreserveRate*100, r.OffPreserveRate*100, r.CorrectCases, len(r.Cases))
	}
	return false, fmt.Sprintf(
		"NO-SIGNAL — ON noise-suppress %.0f%% (OFF %.0f%%), preserve %.0f%% (OFF %.0f%%), %d/%d cases correct.",
		r.OnSuppressRate*100, r.OffSuppressRate*100, r.OnPreserveRate*100, r.OffPreserveRate*100,
		r.CorrectCases, len(r.Cases))
}

// Report renders the full plain-text bench report (no emoji, fixed-width columns).
func (r Result) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "SEAM.BAND_PASS MECHANISM BENCH (#11, metric = grounding precision) — OFFLINE/DETERMINISTIC\n")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 92))
	fmt.Fprintf(&b, "inject floor = %.3g   (drives the REAL engine.bandPassIntake + seams.BandPass; test double, no model)\n\n", r.Floor)

	fmt.Fprintf(&b, "%-26s %5s %5s | %-18s | %-18s | %s\n",
		"case", "tran", "pers", "suppress OFF/ON", "preserve OFF/ON", "correct")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 92))
	cases := make([]CaseScore, len(r.Cases))
	copy(cases, r.Cases)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	for _, c := range cases {
		fmt.Fprintf(&b, "%-26s %5d %5d | %7d / %-8d | %7d / %-8d | %s\n",
			truncStr(c.ID, 26), c.Transients, c.Persistent,
			c.OffSuppressed, c.OnSuppressed, c.OffPreserved, c.OnPreserved, yesno(c.Correct))
	}
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 92))
	fmt.Fprintf(&b, "AGGREGATE (suppressed-noise-rate / preserved-signal-rate):\n")
	fmt.Fprintf(&b, "  transients=%d  persistent=%d\n", r.TotalTransient, r.TotalPersistent)
	fmt.Fprintf(&b, "  suppress-noise   OFF %.0f%%  ->  ON %.0f%%   (delta %+.0f pp)\n",
		r.OffSuppressRate*100, r.OnSuppressRate*100, (r.OnSuppressRate-r.OffSuppressRate)*100)
	fmt.Fprintf(&b, "  preserve-signal  OFF %.0f%%  ->  ON %.0f%%   (delta %+.0f pp)\n",
		r.OffPreserveRate*100, r.OnPreserveRate*100, (r.OnPreserveRate-r.OffPreserveRate)*100)
	fmt.Fprintf(&b, "  cases exactly correct (ON suppresses all noise + preserves all signal): %d/%d\n",
		r.CorrectCases, len(r.Cases))
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 92))
	_, verdict := r.Verdict()
	fmt.Fprintf(&b, "VERDICT: %s\n", verdict)
	return b.String()
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "NO"
}

func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "~"
}
