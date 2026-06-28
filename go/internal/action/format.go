// format.go — turn a raw ToolResult into a concise, grounded one-line observation (ported from
// action/format.py).
//
// Both consumers of a tool result above the action layer — the watched seam's FrontActuator and a
// runtime SubAgent that dispatched a scoped tool — need the same human-readable distillation (a
// ship-check answer should read as a sentence, not a wall of pytest output; the FULL output stays
// on the ToolResult for trace-back). Keeping it in the action layer lets both call it without a
// cross-layer import.
package action

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// pytest summary line, e.g. "2 failed, 3 passed in 0.41s" -> "2 failed, 3 passed".
	// Mirrors Python r"(\d+ (?:failed|passed|error|skipped)[^\n]*?)(?:\s+in\s|$)". RE2 has no
	// backtracking; with the non-greedy [^\n]*? RE2 still finds the leftmost-longest overall match
	// while minimising the lazy group, so the captured group ends right before " in " when present
	// (and at end-of-string otherwise) — the same text Python's re.search yields on a stripped line.
	reTestSummary = regexp.MustCompile(`(\d+ (?:failed|passed|error|skipped)[^\n]*?)(?:\s+in\s|$)`)
	// a FAILED line: "FAILED path::test - reason". Group 1 = the test id, group 2 = the optional why.
	reFailed = regexp.MustCompile(`(?m)^FAILED\s+(\S+?)(?:\s+-\s+(.*))?$`)
)

// SummarizeToolResult gives a concise grounded summary of a tool's real captured output (the
// verbose content is retained on the ToolResult itself). The branch on result.Name + the three
// regexps reproduce the Python summarize_tool_result one-to-one.
func SummarizeToolResult(result ToolResult) string {
	out := strings.TrimSpace(result.Content)

	switch result.Name {
	case "run_tests":
		var line string
		if m := reTestSummary.FindStringSubmatch(out); m != nil {
			line = strings.TrimSpace(m[1])
		} else {
			line = fmt.Sprintf("exit %s", exitStr(result.ExitCode))
		}
		// First FAILED line, if any: "ran the tests: <line> — <id> (<why>)".
		if fm := reFailed.FindStringSubmatch(out); fm != nil {
			name, why := fm[1], strings.TrimSpace(fm[2])
			detail := " — " + name
			if why != "" {
				detail += " (" + why + ")"
			}
			return fmt.Sprintf("ran the tests: %s%s", line, detail)
		}
		verdict := "tests pass"
		if result.IsError {
			verdict = "tests failed"
		}
		return fmt.Sprintf("ran the tests: %s — %s", line, verdict)

	case "run_shell":
		// Keep ALL output lines up to the rune cap (newlines collapsed to " · " so the observation
		// stays one readable line), not just the first. The old first-line-only summary silently
		// DROPPED everything after line 1 — so `cat data/sets/q3.txt` ("quarter: Q3\nrevenue_usd:
		// 184320") was observed as just "quarter: Q3", losing the asked-for field: the multi-hop
		// grounding miss (grounding-A-gold-0015). A genuinely long output still clips at 240 runes.
		body := "(no output)"
		if out != "" {
			body = clipRunes(strings.ReplaceAll(out, "\n", " · "), 240)
		}
		tail := ""
		// tail unless exit_code in (0, None). nil (None) and 0 both suppress the tail.
		if result.ExitCode != nil && *result.ExitCode != 0 {
			tail = fmt.Sprintf(" (exit %d)", *result.ExitCode)
		}
		return fmt.Sprintf("ran it: %s%s", body, tail)

	case "search":
		if out == "" || out == "(no matches)" {
			return "searched: 0 match(es)"
		}
		lines := splitLines(out)
		// Surface the TOP matches (not just the first), each as its own segment up to a rune cap — the
		// `file:line:text` hits carry the answer (e.g. `const SynthOfferCap = 48`), but the DEFINITION is
		// often not hit #1 (usages in test files sort first). The old first-match-only/60-rune summary
		// DROPPED the line carrying the value, so the conscious never saw it, re-searched, and gave up:
		// the same multi-hop grounding miss the run_shell case fixed. Keep up to 6 hits / 320 runes.
		keep := lines
		if len(keep) > 6 {
			keep = keep[:6]
		}
		body := clipRunes(strings.Join(keep, " · "), 320)
		more := ""
		if len(lines) > len(keep) {
			more = fmt.Sprintf(" (+%d more)", len(lines)-len(keep))
		}
		return fmt.Sprintf("searched: %d match(es) — %s%s", len(lines), body, more)
	}

	// len()/[:240] in Python are codepoint-based; use runes so a multibyte char isn't split and
	// the threshold matches Python exactly.
	if r := []rune(out); len(r) > 240 {
		return strings.TrimRight(string(r[:240]), " \t\n\r\f\v") + " …"
	}
	if out == "" {
		return "(no output)"
	}
	return out
}

// GraphObservationText returns the text an OBSERVATION thought should carry into the thought graph —
// the conscious's re-readable, voiceable, SCORED record of what it imported from reality. It is the
// value-PRESERVING sibling of SummarizeToolResult: the one-line summary is right for the trace/console,
// but the GRAPH thought must not lose the imported value when it sits past the summary's 240/320-rune
// clip (the A1 "voicing-stability" residual — a grounded read whose const sits late in a real file was
// summarised to a headline that ended BEFORE the value, so the grounded episode still scored FALSE
// because no thought carried the value).
//
// For a value-carrying read/search/shell result it returns the FULL captured content (already capped at
// the generous 20K-rune tool-level clip in builtins.go — never unbounded), so the imported value reaches
// the graph regardless of where it sits in the file. For run_tests (and an empty/no-output result) it
// keeps the concise summary — a pytest wall is exactly what should NOT flood the graph, and its verdict
// (pass/fail + the first FAILED line) IS the grounded value. The "reality: " prefix the watched seam adds
// is applied by the caller, not here, so this mirrors SummarizeToolResult's contract one-to-one.
func GraphObservationText(result ToolResult) string {
	out := strings.TrimSpace(result.Content)
	switch result.Name {
	case "read_file", "run_shell", "search":
		// the value-import path: carry the full captured output so a late value is not clipped off the
		// scored surface. Empty output falls through to the summary (which renders "(no output)" / the
		// 0-match line). Newlines are preserved — the graph thought is read by the scorer/voicer, not a
		// one-line console row, so it does not need the " · " collapse the summary applies.
		if out != "" {
			return out
		}
	}
	// run_tests, empty results, and any other tool: the concise summary already carries the grounded value.
	return SummarizeToolResult(result)
}

// exitStr renders an *int exit code the way Python's f"exit {result.exit_code}" does — the integer,
// or "None" when nil.
func exitStr(code *int) string {
	if code == nil {
		return "None"
	}
	return fmt.Sprintf("%d", *code)
}

// firstLine returns the first line of s (Python out.splitlines()[0]).
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits on newlines the way Python str.splitlines() does for the line-count + first-hit
// reads here (the content has already been TrimSpace'd, so a trailing newline never inflates the
// count). \n and \r\n / \r are all treated as line breaks.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	// Normalise CRLF / CR to LF, then split. Matches the line boundaries Python str.splitlines()
	// recognises for the ASCII cases this function ever sees (tool output is \n-delimited).
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// clipRunes returns the first n runes of s (Python's s[:60] slice on the first hit). Rune-based so
// a multibyte char is not split mid-sequence — the Python slice is codepoint-based.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
