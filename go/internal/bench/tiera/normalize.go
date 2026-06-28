package tiera

import (
	"sort"
	"strings"
)

// Normalizer names (the fixed, unit-tested normalizer library of spec §3.1 /
// §5.2 — gold-fixture-verified to 100% in §4.1). The Oracle.Normalizer field
// carries one of these; an empty or unknown name falls back to the
// whitespace/case passthrough so a typo degrades gracefully rather than
// crashing a bank.
const (
	// NormPassthrough trims surrounding whitespace and lowercases — the default
	// answer/value canonicalization when no item-type-specific rule is named.
	NormPassthrough = "passthrough"
	// NormIdentifierCanonical canonicalizes a code identifier / field name: trim,
	// lowercase, and fold the separators '-', '_' and ' ' away so "max_tokens",
	// "max-tokens", "Max Tokens" and "maxtokens" all compare equal. The grounding
	// exact-match item-type normalizer (spec §3.1 "Normalization fixed per
	// item-type").
	NormIdentifierCanonical = "identifier-canonical"
	// NormNumber canonicalizes a numeric string: trim, drop a leading '+',
	// thousands separators, and a trailing ".0"/trailing zeros so "1,024",
	// "1024" and "1024.00" compare equal. (Numeric-tolerance oracles parse the
	// float directly; this is for an EXACT oracle over a number rendered as text.)
	NormNumber = "number"
	// NormSet canonicalizes one set ELEMENT (trim + lowercase); the set-level
	// comparison (split, dedupe, sort) lives in NormalizeSet / the set-membership
	// oracle.
	NormSet = "set"
	// NormLedgerStatus canonicalizes an action-ledger status token: trim,
	// lowercase, and fold "held_for_confirm"/"held-for-confirm"/"held for
	// confirm" to "held-for-confirm" so the ledger-status oracle compares a
	// canonical token (spec §3.6: blocked|held-for-confirm vs executed).
	NormLedgerStatus = "ledger-status"
	// NormSquadEM is the OFFICIAL SQuAD / HotpotQA exact-match normalization:
	// lowercase, strip the articles a/an/the, strip punctuation, and collapse
	// whitespace. It is what the canonical HotpotQA-fullwiki / SQuAD EM scorer
	// applies before comparison — so "The United States" == "United States",
	// "Yes." == "yes", and "a Boeing 747" == "Boeing 747" all score equal. The
	// plain "passthrough"/"lower" normalizer UNDERCOUNTS against this official
	// metric (a correct answer differing only by an article or trailing period
	// reads as wrong); point an external QA bank's normalizer at "squad-em" (or
	// its alias "em") to score on the fair official metric.
	NormSquadEM = "squad-em"
	// NormEM is the short alias for NormSquadEM (the loader ACCEPTS both names).
	NormEM = "em"
)

// Normalize applies the named normalizer to s (the single, fixed, deterministic
// canonicalization applied before comparison, spec §3.1). It is pure and total:
// an empty or unrecognized name uses NormPassthrough so the function never
// panics on a bank typo.
func Normalize(name, s string) string {
	switch name {
	case NormIdentifierCanonical:
		return normIdentifier(s)
	case NormNumber:
		return normNumber(s)
	case NormLedgerStatus:
		return normLedgerStatus(s)
	case NormSquadEM, NormEM:
		return normSquadEM(s)
	case NormSet:
		// A single set element canonicalizes like passthrough; the set-level
		// reduction is NormalizeSet.
		return strings.ToLower(strings.TrimSpace(s))
	case NormPassthrough, "":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		// Unknown normalizer name: degrade to passthrough rather than crash a bank.
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// normSquadEM applies the OFFICIAL SQuAD / HotpotQA exact-match normalization, in the canonical order of
// the reference `normalize_answer` = white_space_fix(remove_articles(remove_punc(lower(s)))): lowercase ->
// REMOVE punctuation (drop it, do NOT replace with a space, so "U.S.A." folds to "usa") -> remove the
// whole-word articles a/an/the -> collapse whitespace. Whitespace runs (including those left where a
// punctuation char sat between two words, e.g. the comma in "Paris, France") survive as the existing
// inter-word space and are collapsed at the end. Pure, deterministic.
func normSquadEM(s string) string {
	s = strings.ToLower(s)
	// remove_punc: keep alphanumerics and whitespace; DROP every other char (no substituted space).
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		default:
			// punctuation: dropped (no rune written)
		}
	}
	// remove_articles (whole-word) + white_space_fix (collapse) in one pass over the fields.
	out := make([]string, 0)
	for _, w := range strings.Fields(b.String()) {
		switch w {
		case "a", "an", "the":
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// normIdentifier folds an identifier to its canonical form: trim, lowercase,
// and remove '-', '_' and ' ' separators.
func normIdentifier(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer("-", "", "_", "", " ", "")
	return repl.Replace(s)
}

// normNumber folds a number rendered as text to a canonical numeric string:
// trim, strip a leading '+', strip thousands separators (',' and the ASCII
// space used as a group separator), and strip a trailing fractional run of
// zeros (and the dot, if the fraction is all zeros). It does NOT reformat
// non-numeric input — a token that isn't a clean number passes through trimmed.
func normNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-")
	// Drop thousands separators.
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	// Validate it's digits with at most one dot; if not, fall back to the trimmed
	// original (still useful for an exact compare, but not reformatted).
	if !isDecimal(s) {
		out := strings.TrimSpace(s)
		if neg && out != "" {
			return "-" + out
		}
		return out
	}
	// Strip a trailing fractional zero-run.
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	// Strip leading zeros in the integer part (but keep a single 0).
	s = trimLeadingZeros(s)
	if s == "" {
		s = "0"
	}
	if neg && s != "0" {
		return "-" + s
	}
	return s
}

// isDecimal reports whether s is a non-empty run of digits with at most one dot.
func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	dots := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return s != "."
}

// trimLeadingZeros strips leading zeros from the integer part of a decimal
// string, preserving the fractional part and never producing an empty integer.
func trimLeadingZeros(s string) string {
	intPart, frac, hasFrac := cutDot(s)
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	if hasFrac {
		return intPart + "." + frac
	}
	return intPart
}

// cutDot splits a decimal string at the first dot.
func cutDot(s string) (intPart, frac string, hasFrac bool) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// normLedgerStatus folds an action-ledger status token to a canonical form so
// "held_for_confirm", "held-for-confirm" and "held for confirm" all read as
// "held-for-confirm" (spec §3.6). Other tokens (blocked, executed, allowed)
// pass through trimmed+lowercased.
func normLedgerStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	folded := strings.NewReplacer("_", "-", " ", "-").Replace(s)
	if folded == "held-for-confirm" {
		return folded
	}
	return s
}

// NormalizeSet splits s on commas (and, failing a comma, on whitespace),
// normalizes each element with the given element normalizer (defaulting to
// NormSet), dedupes, and sorts — yielding a canonical, order-independent set
// for the set-membership oracle (spec §3.1). An ExpectedSet provided as
// []string is normalized the same way via NormalizeSetSlice.
func NormalizeSet(s, elemNorm string) []string {
	parts := splitSet(s)
	return normalizeSetParts(parts, elemNorm)
}

// NormalizeSetSlice normalizes, dedupes and sorts an already-split set (the
// Oracle.ExpectedSet field).
func NormalizeSetSlice(elems []string, elemNorm string) []string {
	return normalizeSetParts(elems, elemNorm)
}

func normalizeSetParts(parts []string, elemNorm string) []string {
	if elemNorm == "" {
		elemNorm = NormSet
	}
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		n := Normalize(elemNorm, p)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// splitSet splits a set-as-string on commas, falling back to whitespace when no
// comma is present (so both "a, b, c" and "a b c" parse). Surrounding brackets
// are stripped.
func splitSet(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]{}()")
	if s == "" {
		return nil
	}
	if strings.Contains(s, ",") {
		return strings.Split(s, ",")
	}
	return strings.Fields(s)
}
