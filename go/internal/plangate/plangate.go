// Package plangate is the DEV-SIDE plan-carries-a-falsifiable-gate (Track O / O-2 — the LATHE
// `/plan-p` -> `/verify §0/§0b` content-gate port, docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md
// §4 win #2 + §7 P1). It is the harness DOGFOODING its OWN event bus on the dev side (§6 — make the
// dev-side auto-dev an instance of the harness's own design: emit on internal/events, never print).
//
// THE PROBLEM IT KILLS. The project's most-repeated autonomous-agent failure mode is
// "declared-not-landed" / "tests pass != feature runs" (the wiring-gate lesson; project-gap2-context-
// fix-net-negative; the seed-recall break that reverted a default-flip). A plan says it will add a
// component; the tests go green; but the symbol was never actually wired at the real call site. Prose
// gates ("WIRE it into the live loop", "prove it fires") degrade under autonomy exactly when needed.
//
// THE MECHANISM. A build plan declares a FALSIFIABLE CONTRACT up front:
//   - Producers: file -> the symbols the change MUST land (the producers_files symbol map). A producer
//     is satisfied iff its file appears in the diff AND every declared symbol appears in that file's
//     ADDED lines (a removed/unchanged symbol does not count — the change must PRODUCE it).
//   - Checks: regexes the diff's added text MUST match (the acceptance_checks `grep -qE` lines). A check
//     is satisfied iff at least one added line across the whole diff matches.
//
// Before a KEEP, Audit mechanically runs the contract against the ACTUAL diff and returns a PASS/FAIL
// Verdict. A FAIL is the falsifiable refusal: the keep step blocks, naming exactly which declared
// symbol/regex did not land. The plan CARRIES the gate; the verifier cannot be told green without it.
//
// PURE CONTROL / PLUMBING. This is a deterministic symbol-audit over a diff: no model call, no RNG, no
// wall clock, no I/O (the diff is INJECTED — the CLI boundary shells out to `git diff`; this package
// never does). HEADLESS-PURE: it imports only stdlib + the events leaf, holds an injected Emit closure,
// and is nil-safe so the OFF path costs nothing. Default OFF (the opt-in dev.plan_gate knob) => no
// audit, no event => byte-identical; the gate runs at the dev-side keep step, NOT the cognition tick.
package plangate

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// Producer is one declared file -> symbols the change MUST land. The symbols are matched as plain
// substrings against the file's ADDED lines (a symbol audit, not a parse) — exactly LATHE's `grep -qE`
// discipline but scoped to the file the plan said would produce it.
type Producer struct {
	File    string   `json:"file"`
	Symbols []string `json:"symbols"`
}

// Contract is the falsifiable gate the plan carries: the file->symbol producers + the acceptance-check
// regexes. Plan is a human id for the build slice (e.g. the backlog item) carried into the events.
type Contract struct {
	Plan      string     `json:"plan"`
	Producers []Producer `json:"producers_files"`
	Checks    []string   `json:"acceptance_checks"`
}

// Diff is the ACTUAL change under audit, injected by the caller: file path -> the file's ADDED-line
// text (newline-joined). The CLI builds this from `git diff` (added lines only); tests build it
// directly. Keeping the diff an injected value (not a git shell-out here) is what makes this package
// headless-pure and deterministically testable.
type Diff map[string]string

// AddedText is the full added text across every file in the diff (newline-joined, file-sorted for
// determinism). Acceptance-check regexes run over this so a check can match an added line in any file.
func (d Diff) AddedText() string {
	files := make([]string, 0, len(d))
	for f := range d {
		files = append(files, f)
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		b.WriteString(d[f])
		if !strings.HasSuffix(d[f], "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// AuditLine is one audited contract line — a producer symbol or an acceptance check. Found is whether
// it landed; Reason names WHY it did not (the falsifiable evidence the keep step reports). Kind is
// "producer" or "check".
type AuditLine struct {
	Kind    string // "producer" | "check"
	File    string // the producer's file ("" for a check)
	Symbol  string // the producer symbol ("" for a check)
	Pattern string // the check regex ("" for a producer)
	Found   bool
	Reason  string // "" when Found; else the refusal reason
}

// Verdict is the whole-contract result. Pass is true iff every producer symbol landed AND every
// acceptance check matched. Missing is the ordered list of every line that did NOT land — the precise
// "declared-not-landed" evidence the keep step surfaces on a refusal.
type Verdict struct {
	Plan      string
	Pass      bool
	Producers int // total producer symbols audited
	Checks    int // total acceptance checks audited
	Lines     []AuditLine
	Missing   []AuditLine // the subset of Lines with Found == false
}

// Gate is the plan-gate: an injected Emit closure (nil-safe). It carries no other state — the audit is
// a pure function of (contract, diff). A nil *Gate still audits (Audit is a method only for the emit
// binding); the emit closure being nil makes every emission a no-op (the OFF path is free).
type Gate struct {
	emit events.Emit
}

// New builds a Gate over the bus emit closure. emit may be nil (the OFF / offline path): every
// emission is then a no-op, so the caller can construct one unconditionally and gate on the toggle.
func New(emit events.Emit) *Gate { return &Gate{emit: emit} }

// Audit runs the contract against the diff and returns the Verdict, emitting one plangate.audit per
// audited line and one plangate.verdict for the overall result. It is the WHOLE mechanism: a producer
// symbol must appear in its file's added lines; an acceptance regex must match somewhere in the added
// text; the verdict passes iff every line landed. Deterministic and side-effect-free apart from the
// (injected, nil-safe) emissions — same (contract, diff) always yields the same Verdict.
//
// An invalid acceptance regex is a HARD FAIL of that check (a malformed gate must not silently pass) —
// it is reported as not-found with the compile error as the reason.
func (g *Gate) Audit(c Contract, d Diff) Verdict {
	v := Verdict{Plan: c.Plan}
	added := d.AddedText()

	// 1. producers: each declared symbol must appear in ITS file's added lines.
	for _, p := range c.Producers {
		fileAdded, touched := d[p.File]
		for _, sym := range p.Symbols {
			v.Producers++
			line := AuditLine{Kind: "producer", File: p.File, Symbol: sym}
			switch {
			case !touched:
				line.Found = false
				line.Reason = "file not in diff"
			case strings.Contains(fileAdded, sym):
				line.Found = true
			default:
				line.Found = false
				line.Reason = "symbol absent from added lines"
			}
			g.emitLine(line)
			v.Lines = append(v.Lines, line)
			if !line.Found {
				v.Missing = append(v.Missing, line)
			}
		}
	}

	// 2. acceptance checks: each regex must match somewhere in the added text.
	for _, pat := range c.Checks {
		v.Checks++
		line := AuditLine{Kind: "check", Pattern: pat}
		re, err := regexp.Compile(pat)
		switch {
		case err != nil:
			line.Found = false
			line.Reason = "invalid regex: " + err.Error()
		case re.MatchString(added):
			line.Found = true
		default:
			line.Found = false
			line.Reason = "no added line matches"
		}
		g.emitLine(line)
		v.Lines = append(v.Lines, line)
		if !line.Found {
			v.Missing = append(v.Missing, line)
		}
	}

	v.Pass = len(v.Missing) == 0
	g.emitVerdict(v)
	return v
}

// emitLine emits one plangate.audit event for an audited contract line (nil-safe).
func (g *Gate) emitLine(l AuditLine) {
	if g == nil || g.emit == nil {
		return
	}
	target := l.Symbol
	if l.Kind == "check" {
		target = l.Pattern
	}
	mark := "found"
	if !l.Found {
		mark = "MISSING(" + l.Reason + ")"
	}
	summary := l.Kind + " " + l.File + " " + target + ": " + mark
	g.emit(events.PlanGateAudit, strings.TrimSpace(summary), events.D{
		"kind":    l.Kind,
		"file":    l.File,
		"symbol":  l.Symbol,
		"pattern": l.Pattern,
		"found":   l.Found,
		"reason":  l.Reason,
	})
}

// emitVerdict emits the one plangate.verdict event for the overall result (nil-safe).
func (g *Gate) emitVerdict(v Verdict) {
	if g == nil || g.emit == nil {
		return
	}
	missing := make([]string, 0, len(v.Missing))
	for _, m := range v.Missing {
		target := m.Symbol
		if m.Kind == "check" {
			target = m.Pattern
		}
		missing = append(missing, m.Kind+":"+strings.TrimSpace(m.File+" "+target))
	}
	state := "PASS"
	if !v.Pass {
		state = "FAIL"
	}
	summary := "plan-gate " + state + " (" + v.Plan + "): " +
		strconv.Itoa(v.Producers) + " producers, " + strconv.Itoa(v.Checks) + " checks, " +
		strconv.Itoa(len(v.Missing)) + " missing"
	g.emit(events.PlanGateVerdict, summary, events.D{
		"pass":      v.Pass,
		"producers": v.Producers,
		"checks":    v.Checks,
		"missing":   missing,
		"plan":      v.Plan,
	})
}
