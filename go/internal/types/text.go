package types

import "strings"

// VoicePrefixes are the seam-voicing prefixes the Transform prepends + the reality marker.
// Stored lowercase and matched case-insensitively so a thought reads as plain prose once the
// seam's voicing is stripped. Mirrors Python VOICE_PREFIXES (order preserved — StripVoice
// strips the FIRST match).
var VoicePrefixes = []string{
	"oh — ",
	"right, that gives ",
	"it comes to me: ",
	"i can see it — ",
	"reality: ",
}

// RecapPrefix is the marker a recalled cross-episode memory leads with — produced once
// (the engine's recap) and filtered out of context in several readers, so it lives here as
// the single source of truth. Mirrors Python RECAP_PREFIX.
const RecapPrefix = "Earlier in this conversation"

// Ellipsize collapses whitespace and truncates to width with an ellipsis. The single
// width-truncate helper used by Thought.Short / Goal.Short / the backend's fragmenting.
// Faithful to Python ellipsize: replace "\n"->" ", strip, then truncate on RUNE count
// (Python len/slicing is code-point based) using the same "…" (U+2026) ellipsis.
func Ellipsize(text string, width int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	r := []rune(text)
	if len(r) <= width {
		return text
	}
	if width < 1 {
		width = 1
	}
	return string(r[:width-1]) + "…"
}

// EllipsizeDefault truncates at the Python default width of 72.
func EllipsizeDefault(text string) string { return Ellipsize(text, 72) }

// StripVoice strips a leading seam-voice prefix + a leading [operator] sub-agent tag, then
// sentence-cases. The single canonical de-voicer (used by the engine's outreach filter and
// the backend's respond). Faithful to Python strip_voice:
//   - trim, lowercase-match the FIRST VoicePrefix and drop it,
//   - drop a leading "[...] " tag if "] " occurs within the first 24 chars,
//   - upper-case the first rune.
func StripVoice(text string) string {
	text = strings.TrimSpace(text)
	low := strings.ToLower(text)
	for _, p := range VoicePrefixes {
		if strings.HasPrefix(low, p) {
			text = text[len(p):]
			break
		}
	}
	// a leading [operator] sub-agent tag: Python checks `text.startswith("[") and "] " in text[:24]`
	if strings.HasPrefix(text, "[") {
		head := text
		if len(head) > 24 {
			head = head[:24]
		}
		if i := strings.Index(head, "] "); i >= 0 {
			text = text[i+2:]
		}
	}
	if text == "" {
		return text
	}
	r := []rune(text)
	r[0] = upperFirstRune(r[0])
	return string(r)
}

// Jaccard is the shared word-overlap similarity (the _sim / _similar helper duplicated
// across value/controller/graph — consolidated here once). Lower-cases, splits on
// whitespace into word SETS, and returns |a∩b| / |a∪b|, or 0 when either side is empty.
// Faithful to Python: set(a.lower().split()) uses str.split() (splits on runs of any
// whitespace and drops empties), which strings.Fields matches exactly.
func Jaccard(a, b string) float64 {
	wa := wordSet(a)
	wb := wordSet(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0.0
	}
	inter := 0
	for w := range wa {
		if _, ok := wb[w]; ok {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	return float64(inter) / float64(union)
}

// wordSet lower-cases and splits on whitespace into a set of words (Python
// set(s.lower().split())). strings.Fields drops empty fields and splits on any unicode
// whitespace, matching str.split() with no argument.
func wordSet(s string) map[string]struct{} {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
}

// upperFirstRune upper-cases a single rune the way Python str[0].upper() does for the
// common case. strings.ToUpper on a one-rune string handles the full-rune mapping.
func upperFirstRune(r rune) rune {
	u := []rune(strings.ToUpper(string(r)))
	if len(u) == 0 {
		return r
	}
	return u[0]
}
