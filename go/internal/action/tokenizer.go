// tokenizer.go — split a compound shell command into its constituent command segments, so the
// safety content-gate (safety.go EvaluateCommand) can check EACH command, not just the whole line.
//
// Ported from lathe's safety/tokenizer.go. WHY this matters: our block rules are position-anchored
// (e.g. `rm -rf /` must end the string), so a catastrophe hidden mid-line — `echo ok && rm -rf /`,
// `ls; mkfs /dev/sda`, `$(rm -rf /)` — would slip past a whole-string check. Splitting first, then
// checking each segment, closes that. (The pipe-to-shell rules `curl|sh` / `base64|sh` are the
// exception — the pipe IS the signal — so EvaluateCommand still checks those on the WHOLE command.)
//
// It handles: `;` `&&` `||` `|` and newlines as separators; single/double quotes (operators inside
// are literal); backslash escapes; heredocs (content not split); command substitution `$()`/backtick
// and process substitution `<()`/`>()` (inner commands extracted as ADDITIONAL segments, so a
// catastrophe nested in `$(...)` is still seen). Returns nil for empty/whitespace input.
package action

import "strings"

// TokenizeShellCommand splits a compound shell command into its constituent command segments. The
// first segment is the "outer" command (substitutions left intact); any inner commands from
// $()/backtick/process-substitution are appended as additional segments.
func TokenizeShellCommand(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var segments []string
	var innerCommands []string
	var current strings.Builder

	runes := []rune(input)
	n := len(runes)
	i := 0

	for i < n {
		ch := runes[i]

		// Backslash escape — keep the next char literal.
		if ch == '\\' && i+1 < n {
			current.WriteRune(ch)
			current.WriteRune(runes[i+1])
			i += 2
			continue
		}

		// Single-quoted string — no interpretation inside.
		if ch == '\'' {
			end := scanSingleQuote(runes, i)
			writeRunes(&current, runes[i:end])
			i = end
			continue
		}

		// Double-quoted string — no operator splitting inside, but $() inside is still extracted.
		if ch == '"' {
			end := scanDoubleQuote(runes, i)
			innerCommands = append(innerCommands, extractSubstitutions(runes[i:end])...)
			writeRunes(&current, runes[i:end])
			i = end
			continue
		}

		// Heredoc (<<DELIM …) — content between delimiters is not split.
		if ch == '<' && i+1 < n && runes[i+1] == '<' && (i < 2 || runes[i-1] != '<') {
			heredocEnd, delimEnd := scanHeredoc(runes, i)
			if heredocEnd > i {
				writeRunes(&current, runes[i:heredocEnd])
				i = heredocEnd
				if delimEnd > heredocEnd {
					i = delimEnd
				}
				flushSegment(&segments, &current)
				continue
			}
		}

		// Command substitution: $()
		if ch == '$' && i+1 < n && runes[i+1] == '(' {
			end := scanBalancedParen(runes, i+1)
			writeRunes(&current, runes[i:end])
			if end > i+2 {
				innerTrimmed := strings.TrimSpace(string(runes[i+2 : end-1]))
				if innerTrimmed != "" {
					innerCommands = append(innerCommands, innerTrimmed)
					innerCommands = append(innerCommands, extractSubstitutions([]rune(innerTrimmed))...)
				}
			}
			i = end
			continue
		}

		// Backtick command substitution.
		if ch == '`' {
			end := scanBacktick(runes, i)
			writeRunes(&current, runes[i:end])
			if end > i+1 {
				innerTrimmed := strings.TrimSpace(string(runes[i+1 : end-1]))
				if innerTrimmed != "" {
					innerCommands = append(innerCommands, innerTrimmed)
				}
			}
			i = end
			continue
		}

		// Process substitution: <() or >()
		if (ch == '<' || ch == '>') && i+1 < n && runes[i+1] == '(' {
			end := scanBalancedParen(runes, i+1)
			writeRunes(&current, runes[i:end])
			if end > i+2 {
				innerTrimmed := strings.TrimSpace(string(runes[i+2 : end-1]))
				if innerTrimmed != "" {
					innerCommands = append(innerCommands, innerTrimmed)
				}
			}
			i = end
			continue
		}

		switch {
		case ch == '\n': // newline separator
			flushSegment(&segments, &current)
			i++
			continue
		case ch == ';': // semicolon separator
			flushSegment(&segments, &current)
			i++
			continue
		case ch == '&' && i+1 < n && runes[i+1] == '&': // AND
			flushSegment(&segments, &current)
			i += 2
			continue
		case ch == '|' && i+1 < n && runes[i+1] == '|': // OR
			flushSegment(&segments, &current)
			i += 2
			continue
		case ch == '|': // pipe (single)
			flushSegment(&segments, &current)
			i++
			continue
		}

		current.WriteRune(ch)
		i++
	}

	flushSegment(&segments, &current)
	segments = append(segments, innerCommands...)

	if len(segments) == 0 {
		return nil
	}
	return segments
}

// flushSegment trims the builder and appends it if non-empty, then resets it.
func flushSegment(segments *[]string, current *strings.Builder) {
	if s := strings.TrimSpace(current.String()); s != "" {
		*segments = append(*segments, s)
	}
	current.Reset()
}

// scanSingleQuote returns the index after the closing single quote (or end on unterminated).
func scanSingleQuote(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == '\'' {
			return i + 1
		}
	}
	return len(runes)
}

// scanDoubleQuote returns the index after the closing double quote (escapes skipped).
func scanDoubleQuote(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if runes[i] == '"' {
			return i + 1
		}
	}
	return len(runes)
}

// scanBalancedParen scans from an opening '(' and returns the index after the matching ')'.
func scanBalancedParen(runes []rune, start int) int {
	depth := 0
	for i := start; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if runes[i] == '\'' {
			i = scanSingleQuote(runes, i) - 1
			continue
		}
		if runes[i] == '"' {
			i = scanDoubleQuote(runes, i) - 1
			continue
		}
		if runes[i] == '(' {
			depth++
		} else if runes[i] == ')' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(runes)
}

// scanBacktick returns the index after the closing backtick.
func scanBacktick(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if runes[i] == '`' {
			return i + 1
		}
	}
	return len(runes)
}

// scanHeredoc detects a heredoc starting at i (the first '<'). Returns (heredocEnd, delimEnd).
func scanHeredoc(runes []rune, start int) (int, int) {
	if start+1 >= len(runes) || runes[start+1] != '<' {
		return start, start
	}
	i := start + 2
	if i < len(runes) && runes[i] == '-' {
		i++
	}
	for i < len(runes) && runes[i] == ' ' {
		i++
	}
	var delim string
	if i < len(runes) && (runes[i] == '\'' || runes[i] == '"') {
		quote := runes[i]
		i++
		dStart := i
		for i < len(runes) && runes[i] != quote {
			i++
		}
		delim = string(runes[dStart:i])
		if i < len(runes) {
			i++
		}
	} else {
		dStart := i
		for i < len(runes) && runes[i] != '\n' && runes[i] != ' ' && runes[i] != ')' && runes[i] != '"' {
			i++
		}
		delim = string(runes[dStart:i])
	}
	if delim == "" {
		return start, start
	}
	for i < len(runes) && runes[i] != '\n' {
		i++
	}
	if i < len(runes) {
		i++
	}
	for i < len(runes) {
		lineStart := i
		for i < len(runes) && runes[i] != '\n' {
			i++
		}
		line := strings.TrimSpace(string(runes[lineStart:i]))
		if i < len(runes) {
			i++
		}
		if line == delim {
			return i, i
		}
	}
	return len(runes), len(runes)
}

// extractSubstitutions returns the inner commands of any $()/backtick/process-substitution found in
// a rune slice (used for substitutions nested inside quotes).
func extractSubstitutions(runes []rune) []string {
	var results []string
	n := len(runes)
	for i := 0; i < n; i++ {
		if runes[i] == '\\' && i+1 < n {
			i++
			continue
		}
		if runes[i] == '\'' {
			i = scanSingleQuote(runes, i) - 1
			continue
		}
		if runes[i] == '"' {
			continue // look for $() inside double quotes too
		}
		if runes[i] == '$' && i+1 < n && runes[i+1] == '(' {
			end := scanBalancedParen(runes, i+1)
			if end > i+2 {
				if inner := strings.TrimSpace(string(runes[i+2 : end-1])); inner != "" {
					results = append(results, inner)
					results = append(results, extractSubstitutions([]rune(inner))...)
				}
			}
			i = end - 1
			continue
		}
		if runes[i] == '`' {
			end := scanBacktick(runes, i)
			if end > i+1 {
				if inner := strings.TrimSpace(string(runes[i+1 : end-1])); inner != "" {
					results = append(results, inner)
				}
			}
			i = end - 1
			continue
		}
		if (runes[i] == '<' || runes[i] == '>') && i+1 < n && runes[i+1] == '(' {
			end := scanBalancedParen(runes, i+1)
			if end > i+2 {
				if inner := strings.TrimSpace(string(runes[i+2 : end-1])); inner != "" {
					results = append(results, inner)
				}
			}
			i = end - 1
			continue
		}
	}
	return results
}

// writeRunes appends a rune slice to a strings.Builder.
func writeRunes(b *strings.Builder, runes []rune) {
	for _, r := range runes {
		b.WriteRune(r)
	}
}
