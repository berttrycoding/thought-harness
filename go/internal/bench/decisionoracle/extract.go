package decisionoracle

import (
	"strings"
)

// ExtractVerdict distils a worker's free-text RESPONSE into a machine-readable Verdict
// the oracle can score, deterministically (no model). It is the bridge from the live
// Deliberator/Verifier output (the remainder slice drives the real worker and passes
// its response here) to the ground-truth scorer. The fixture is consulted ONLY for its
// option labels (for the deliberator pick) and worker kind — never for the answer, so
// extraction cannot leak ground truth into the verdict.
//
// Deliberator: scan the response for exactly one of the fixture's option IDs/Names as a
// whole token; the response text becomes the Reasoning (for the soundness axis).
// Verifier: classify the response into accept / refuse via lexical markers, and set the
// honest flag when it uses a "cannot verify / insufficient evidence" phrasing.
//
// A response that does not yield a verdict returns the zero Verdict for that worker
// (empty PickedOption / empty Decision) — which the scorer treats as a HARD fail
// (the worker declined to decide), never a silent pass.
func ExtractVerdict(response string, fx Fixture) Verdict {
	// CONTRACT path FIRST (A2 verdict contract): if the worker STATED a well-formed `VERDICT:`
	// line, that is the verdict — the deterministic prose parser becomes a FALLBACK only, fired
	// when the worker disobeyed the contract (no/garbled VERDICT line). This removes the prose
	// parser from the common path (where the three serial false-passes lived) and isolates it to
	// the disobedience case (which the present-rate guard, #6, surfaces).
	if v, ok := parseVerdictLine(response, fx); ok {
		return v
	}
	switch fx.Worker {
	case WorkerDeliberator:
		return extractDeliberator(response, fx)
	case WorkerVerifier:
		return extractVerifier(response)
	default:
		return Verdict{}
	}
}

// verdictLinePrefix is the contract marker the worker states its verdict behind (case-insensitive
// at scan time).
const verdictLinePrefix = "verdict:"

// parseVerdictLine reads the LAST `VERDICT: <label>` line of a worker response and maps the label
// to a Verdict, deterministically and offline. It is the A2 verdict-CONTRACT reader (the worker
// STATES its verdict rather than the parser GUESSING it from prose). It returns ok=false — so
// ExtractVerdict falls back to the prose parser — when:
//
//   - no line begins with `VERDICT:` (the worker disobeyed the contract), OR
//   - the stated label maps to no valid option / decision (a garbled `VERDICT: xyz`).
//
// It reads the LAST such line so a worker that mentions the contract mid-reasoning ("I will end
// with a VERDICT: line") and then states the real verdict last is read correctly. The fixture is
// consulted ONLY for its option labels / worker kind — never the ground-truth answer.
func parseVerdictLine(response string, fx Fixture) (Verdict, bool) {
	label := lastVerdictLabel(response)
	if label == "" {
		return Verdict{}, false
	}
	switch fx.Worker {
	case WorkerDeliberator:
		low := strings.ToLower(strings.TrimSpace(label))
		if low == "undecided" {
			// An HONEST stated tie: a decision (Reasoning carried for the soundness axis) with
			// no pick. The scorer treats a fabricated pick as wrong on an undecidable fixture and
			// the honest "undecided" (no pick) as the correct abstain.
			return Verdict{Reasoning: response, Undecided: true}, true
		}
		id, matched := matchOption(label, fx.Options)
		if !matched {
			return Verdict{}, false // garbled label — fall back to the prose parser.
		}
		return Verdict{PickedOption: id, Reasoning: response}, true
	case WorkerVerifier:
		low := strings.ToLower(strings.TrimSpace(label))
		switch low {
		case "accept":
			return Verdict{Decision: DecisionAccept, Reasoning: response}, true
		case "refuse":
			return Verdict{Decision: DecisionRefuse, Reasoning: response}, true
		case "cannot-verify", "cannot verify", "cannot_verify":
			// The honest abstain: a refuse-to-ship flagged honest (the never-confabulate move).
			return Verdict{Decision: DecisionRefuse, Honest: true, Reasoning: response}, true
		default:
			return Verdict{}, false // garbled label — fall back to the prose parser.
		}
	default:
		return Verdict{}, false
	}
}

// lastVerdictLabel returns the text AFTER the colon on the LAST line whose first non-space token
// is `VERDICT:` (case-insensitive), trimmed of surrounding whitespace and markdown emphasis. ""
// when no such line exists. A `VERDICT:` that appears mid-line (not at the line head) is ignored —
// the contract is a line that LEADS with the marker, so a sentence merely mentioning the word does
// not count as a stated verdict.
func lastVerdictLabel(response string) string {
	out := ""
	for _, raw := range strings.Split(response, "\n") {
		line := strings.TrimLeft(raw, " \t*_>#-")
		if len(line) < len(verdictLinePrefix) {
			continue
		}
		if !strings.EqualFold(line[:len(verdictLinePrefix)], verdictLinePrefix) {
			continue
		}
		label := strings.TrimSpace(line[len(verdictLinePrefix):])
		// Strip trailing markdown emphasis / punctuation the worker may append (e.g.
		// "VERDICT: refactor**" or "VERDICT: accept.").
		label = strings.Trim(label, " \t*_`.")
		if label == "" {
			continue // a bare "VERDICT:" with no label is not a stated verdict.
		}
		out = label // keep scanning so the LAST stated label wins.
	}
	return out
}

// extractDeliberator finds the single option the response picks. If the response names
// exactly one option (by ID or Name, as a whole token) it is the pick; if it names
// more than one we take the one that appears after a decision cue ("pick"/"choose"/
// "go with"/"recommend"/"better"/"use") when that disambiguates; failing that we read a
// RANKED-LIST conclusion ("Ranked choice: X" / a numbered "1. X" #1 item) as a pick of
// the #1-ranked option; else no pick (a genuinely ambiguous answer is a non-decision — a
// hard fail, honestly).
func extractDeliberator(response string, fx Fixture) Verdict {
	v := Verdict{Reasoning: response}
	if strings.TrimSpace(response) == "" {
		return v
	}
	// All options the response mentions. An option is "mentioned" when its ID appears as
	// a whole token OR its Name appears as a CONTIGUOUS token subsequence — a multi-word
	// Name ("incremental refactor") is invisible to a single-token scan (D1), so it must
	// be located as adjacent tokens, not by normalising the whole Name to one string.
	toks := tokenize(response)
	var mentioned []string
	for _, o := range fx.Options {
		if optionPresent(toks, o) {
			mentioned = append(mentioned, o.ID)
		}
	}
	if len(mentioned) == 1 {
		v.PickedOption = mentioned[0]
		return v
	}
	if len(mentioned) > 1 {
		// Disambiguate by the option named CLOSEST AFTER a decision cue. A trade-off
		// answer typically lays out both then states the pick.
		if pick := pickAfterCue(response, fx.Options); pick != "" {
			v.PickedOption = pick
			return v
		}
	}
	// Last resort: a RANKED-LIST conclusion. Real workers often phrase the pick as a
	// ranking ("Ranked choice: X first" / a numbered "1. X") rather than a verbal cue —
	// the #1-ranked option IS the pick. This reads the #1 item HONESTLY (whatever the
	// worker ranked first, even the wrong option — the scorer then catches a wrong #1),
	// and yields "" when no unique #1 option is identifiable (still a non-decision).
	if pick := pickFromRanking(response, fx.Options); pick != "" {
		v.PickedOption = pick
	}
	return v
}

// decisionCues are the phrases that precede a stated pick in a deliberation answer.
var decisionCues = []string{
	"i recommend", "i'd recommend", "i would recommend", "i recommend going with",
	"i'd go with", "i would go with", "go with", "i'd choose", "i would choose",
	"i choose", "i'd pick", "i would pick", "i pick", "my recommendation is",
	"the better option is", "the better choice is", "better to", "we should use",
	"we should", "use the", "use a", "use ", "choose ", "pick ", "answer:", "verdict:",
}

// pickAfterCue returns the option ID named first AFTER the LAST decision cue in the
// response (the conclusion usually states the pick last). "" when no cue+option pair is
// found.
//
// CAVEAT (last-cue): it anchors on the LAST cue and scans only the tail after it, so a
// response whose decisive cue+option sits earlier and is followed by a LATER cue word with
// no option after it (e.g. "...go with mutex; we should revisit this") returns "" rather
// than the earlier pick — a conservative non-decision (a hard fail), never a wrong pick.
// This is deliberate: extraction must not GUESS a pick the surface does not clearly state.
// A bare token-scan tie (>1 option, no resolving cue tail) likewise yields "" upstream.
// The case is rare on real worker output (the conclusion's cue is usually the last) and is
// the safe failure direction; tighten only if a live claude run shows it mis-scoring.
func pickAfterCue(response string, options []Option) string {
	low := strings.ToLower(response)
	bestCue := -1
	for _, cue := range decisionCues {
		if idx := strings.LastIndex(low, cue); idx > bestCue {
			bestCue = idx
		}
	}
	if bestCue < 0 {
		return ""
	}
	tail := response[bestCue:]
	tailToks := tokenize(tail)
	// The option named EARLIEST after the cue is the pick (a cue + a co-mention still
	// names the recommended option first). Use the full-occurrence position so a stray
	// shared word can't anchor a multi-word Name ahead of the real pick (D2-aligned).
	bestID, bestPos := "", -1
	for _, o := range options {
		p := optionPos(tailToks, o)
		if p < 0 {
			continue
		}
		if bestPos < 0 || p < bestPos {
			bestID, bestPos = o.ID, p
		}
	}
	return bestID
}

// leadingRankCues are inline phrases the #1-ranked (so, chosen) option follows, e.g.
// "Ranked choice: Relational SQL first" / "Top choice: the mutex" / "Most preferred: A".
// The pick is the option named FIRST after the cue (POSITION-aware: the option ranked
// first, not whichever option has more name-tokens in the span). This is what makes a
// loser-first co-mention ("Ranked choice: NoSQL first, SQL second") extract the LOSER
// (nosql) honestly rather than the longer-named winner — direction is preserved.
var leadingRankCues = []string{
	"ranked choice:", "ranked choice ", "most preferred:", "most preferred ",
	"top choice:", "top choice ", "ranking:", "ranked:",
}

// tagRankCues are inline TAGS that mark the option PRECEDING them as the #1 pick (the
// option name comes BEFORE the tag, e.g. "1. **Mutex** *(preferred)*"). They are matched
// against the option named on the cue's LINE — there is exactly one such option on a
// real "(preferred)"-tagged item, so the whole-line first-option rule is unambiguous.
var tagRankCues = []string{"(preferred)"}

// hedgeMarkers are explicit NON-DECISION conclusions ("too close to call", "it depends")
// — a ranked or numbered surface that ENDS in a hedge is not a pick, it is a refusal to
// decide. Their presence in a rank span suppresses the pick (the proven-correct "" on a
// hedged conclusion), so "Ranked: A vs B — too close to call" extracts NO pick rather
// than the first-named option.
var hedgeMarkers = []string{
	"too close to call", "it depends", "depends on", "hard to say", "could go either way",
	"no clear winner", "either could work", "either path can work", "a toss-up", "toss up",
	"cannot decide", "can't decide", "i need more data", "need more data", "more data",
	"insufficient information", "not enough information", "either way",
}

// pickFromRanking reads a RANKED-LIST conclusion and returns the #1-ranked option's ID,
// or "" when none is uniquely identifiable. It is POSITION-aware (the option RANKED first,
// not whichever option a span name-overlaps most) and DECISION-aware (a hedged conclusion
// or a non-ranking decomposition step yields no pick). It considers, in order:
//
//   - an inline LEADING rank cue ("Ranked choice: X first") — the option named FIRST after
//     the cue within the span is the #1 pick (unless the span hedges);
//   - an inline TAG cue ("(preferred)") — the option named on the cue's line is the pick;
//   - a numbered "1." / "1)" list line that is part of an ACTUAL ranking block (a sibling
//     "2." line that names a DIFFERENT option must exist) — the FIRST option named on the
//     #1 line is the pick. Interrogative / decomposition-step "1." lines (a "?" ending, or
//     no sibling rank line that names a distinct option) are SKIPPED, never read as a pick.
//
// A genuine tie / no-option / hedged conclusion / lone decomposition step returns ""
// (a conservative non-decision, never a guessed or direction-blind pick).
func pickFromRanking(response string, options []Option) string {
	low := strings.ToLower(response)
	// 1) An inline LEADING rank cue: the FIRST option after the cue is the #1 pick.
	for _, cue := range leadingRankCues {
		idx := strings.Index(low, cue)
		if idx < 0 {
			continue
		}
		span := rankSpan(response, idx)
		if containsAny(strings.ToLower(span), hedgeMarkers) {
			return "" // a hedged ranking is a non-decision, never the first-named option.
		}
		after := span[len(cue):] // skip the cue text; rank from what follows it.
		if pick := firstOptionInSpan(after, options); pick != "" {
			return pick
		}
	}
	// 2) An inline TAG cue ("(preferred)"): the option on the cue's LINE is the pick.
	for _, cue := range tagRankCues {
		idx := strings.Index(low, cue)
		if idx < 0 {
			continue
		}
		line := rankSpan(response, lineStart(response, idx))
		if containsAny(strings.ToLower(line), hedgeMarkers) {
			return ""
		}
		if pick := firstOptionInSpan(line, options); pick != "" {
			return pick
		}
	}
	// 3) A numbered "1." / "1)" #1 line that is part of an ACTUAL ranking block. A response
	// can contain SEVERAL "1." lines — a decomposition list near the top AND the conclusion's
	// ranking near the bottom — so we take the LAST "1." line that is a real ranking #1 (the
	// conclusion ranking is stated last). A "1." line is a real ranking #1 only if a sibling
	// "2." line that names a DIFFERENT option exists; an interrogative ("...?") or lone-step
	// "1." line is a decomposition step, not a ranking, and is SKIPPED — never guessed.
	lines := strings.Split(response, "\n")
	pick := ""
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !isFirstRankedLine(line) {
			continue
		}
		if isInterrogativeLine(line) {
			continue
		}
		if containsAny(strings.ToLower(line), hedgeMarkers) {
			continue
		}
		// A real ranking item NAMES its option as the line HEAD ("1. **Mutex** — ..."),
		// whereas a decomposition STEP leads with an imperative verb and merely mentions
		// the option in its predicate ("1. Assess the incremental refactor's blast radius").
		// Requiring the option at the head separates a ranking #1 from a decompose step.
		first := lineHeadOption(line, options)
		if first == "" {
			continue
		}
		// A POSITIVE ranking signal is required on the #1 line (D3): an article-led
		// decompose step ("1. The refactor's blast radius must be assessed") puts an
		// option at the head AND has a distinct "2." sibling, so head+sibling ALONE
		// fabricates a ranking #1 out of a pure to-investigate list. A real ranking
		// item carries a rank cue ("preferred"/"best"/"choice"), a "(preferred)" tag,
		// or a verdict-shaped conclusion separator (an em-dash / colon introducing the
		// rationale, the "1. **Mutex** — ..." shape). A decompose list with no such
		// signal yields NO pick.
		if !hasRankingSignal(line) {
			continue
		}
		if !hasSiblingRankLine(lines, i, first, options) {
			continue // a lone "1." with no distinct "2." sibling is not a ranking block.
		}
		pick = first
	}
	return pick
}

// rankingSignalCues are positive cue words/tags that mark a numbered #1 line as an actual
// RANKING conclusion (not a decompose step): a preference word or an explicit pick verb.
var rankingSignalCues = []string{
	"preferred", "best", "top choice", "winner", "recommend", "my pick", "the pick",
	"choose", "i'd go with", "go with", "favour", "favor", "wins", "strongest",
	"most appropriate", "first choice", "clear choice",
}

// hasRankingSignal reports whether a numbered #1 line carries a POSITIVE ranking signal
// (D3): a rank cue word, OR a verdict-shaped conclusion separator (an em-dash or a colon
// that introduces a rationale clause, the "1. **X** — <why it wins>" shape real workers
// use). A bare imperative/declarative decompose step ("1. The refactor's blast radius
// must be assessed") carries none and is NOT read as a ranking #1.
func hasRankingSignal(line string) bool {
	low := strings.ToLower(line)
	if containsAny(low, rankingSignalCues) {
		return true
	}
	// A verdict-shaped conclusion separator: an em-dash (—, the common ranking-item
	// "X — why" shape) or a colon introducing a rationale. The hyphen-minus is NOT
	// accepted (it is the markdown bullet/range char), only the em/en dash.
	return strings.ContainsAny(line, "—–") || strings.Contains(line, ":")
}

// lineHeadOption returns the option named at the HEAD of a numbered "1." line body — the
// option whose first-named word sits at token position 0 or 1 (allowing one leading
// article/qualifier) once the "1."/"1)" marker and markdown emphasis are stripped. ""
// when the head names no option (so an imperative decomposition step, whose option is
// buried after a leading verb, is NOT read as a ranking #1). Position-aware: a head
// co-mention takes the EARLIER-named option.
func lineHeadOption(line string, options []Option) string {
	body := strings.TrimLeft(line, "*-_ \t")
	switch {
	case strings.HasPrefix(body, "1."):
		body = body[2:]
	case strings.HasPrefix(body, "1)"):
		body = body[2:]
	}
	body = strings.TrimLeft(body, " \t*_")
	toks := tokenize(body)
	if len(toks) == 0 {
		return ""
	}
	const headWindow = 2 // option must begin within the first 2 tokens (article + name).
	bestID, bestPos := "", -1
	for _, o := range options {
		p := optionPos(toks, o) // full-occurrence position (D2): not a stray shared word.
		if p < 0 || p >= headWindow {
			continue
		}
		if bestPos < 0 || p < bestPos {
			bestID, bestPos = o.ID, p
		}
	}
	return bestID
}

// firstOptionInSpan returns the ID of the option whose ID or FULL Name is named EARLIEST
// (by token position) in span — POSITION-aware, so a co-mention extracts the FIRST-ranked
// option, not the longest-named. "" when the span names no option. A multi-word Name is
// anchored on its FULL CONTIGUOUS occurrence (all its words present and adjacent), NOT on
// its first present word (D2): so "from a relational mindset, NoSQL first, relational SQL
// second" ranks NoSQL ahead of "relational SQL" — a stray earlier "relational" (in an
// aside) can no longer steal the position for sql.
func firstOptionInSpan(span string, options []Option) string {
	toks := tokenize(span)
	bestID, bestPos := "", -1
	for _, o := range options {
		p := optionPos(toks, o)
		if p < 0 {
			continue
		}
		if bestPos < 0 || p < bestPos {
			bestID, bestPos = o.ID, p
		}
	}
	return bestID
}

// optionPos returns the earliest token position at which an option is named as a WHOLE
// occurrence — the min of (its ID token position, the START position of its Name's full
// CONTIGUOUS token subsequence) over toks — or -1 when it is not named. Anchoring a
// multi-word Name on its full contiguous occurrence (not its first shared word) is the
// D2 fix: position is where the option ACTUALLY appears, so a stray earlier word of a
// multi-word Name can no longer hijack the ranking.
func optionPos(toks []string, o Option) int {
	best := -1
	consider := func(p int) {
		if p < 0 {
			return
		}
		if best < 0 || p < best {
			best = p
		}
	}
	if idTok := tokenize(o.ID); len(idTok) == 1 {
		// A single-token ID ("sql", "nosql", "mutex"): match it as a whole token.
		for i, t := range toks {
			if tokenMatches(t, idTok[0]) {
				consider(i)
				break
			}
		}
	} else if len(idTok) > 1 {
		consider(subsequencePos(toks, idTok))
	}
	if name := tokenize(o.Name); len(name) > 0 {
		consider(subsequencePos(toks, name))
	}
	return best
}

// optionPresent reports whether an option is named in toks — its ID as a whole token OR
// its Name as a contiguous token subsequence. This is the presence test the `mentioned`
// gate uses (D1): a multi-word Name is detected as adjacent tokens, never lost to a
// single-token scan that can only ever match a single-word Name or the ID.
func optionPresent(toks []string, o Option) bool {
	return optionPos(toks, o) >= 0
}

// subsequencePos returns the START token index of the EARLIEST contiguous occurrence of
// the want token sequence inside toks (each want token matched by tokenMatches, so a
// gerund/inflection like "refactoring" matches the stem "refactor"), or -1 when want
// does not appear contiguously. An empty want never matches.
func subsequencePos(toks, want []string) int {
	if len(want) == 0 || len(want) > len(toks) {
		return -1
	}
	for i := 0; i+len(want) <= len(toks); i++ {
		ok := true
		for j, w := range want {
			if !tokenMatches(toks[i+j], w) {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// tokenMatches reports whether a response token tok satisfies a wanted option-name token
// want: an exact match, OR — for a wanted stem of length >= 4 — tok having want as a
// prefix (so an inflection like "refactoring"/"refactored" matches the Name word
// "refactor"). The length floor avoids spurious short-prefix collisions (e.g. "sql" must
// match exactly, not prefix "sqlite"); both are compared lowercased (tokenize already
// lowercases).
func tokenMatches(tok, want string) bool {
	if tok == want {
		return true
	}
	const stemFloor = 4
	return len(want) >= stemFloor && len(tok) > len(want) && strings.HasPrefix(tok, want)
}

// lineStart returns the byte index of the start of the line containing idx.
func lineStart(response string, idx int) int {
	if nl := strings.LastIndexByte(response[:idx], '\n'); nl >= 0 {
		return nl + 1
	}
	return 0
}

// isInterrogativeLine reports whether a #1 line is a question (a decomposition
// sub-question), i.e. it ends in "?" — such a line is a step to investigate, not a
// ranking #1, and must not be read as a pick.
func isInterrogativeLine(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t*_"), "?")
}

// hasSiblingRankLine reports whether a real "2." (or higher) ranking sibling exists AFTER
// the #1 line at index i that names a DIFFERENT option than first — the signal that the
// "1." line is genuinely the head of a ranking block rather than a lone decomposition
// step. A "2." line that names the SAME option (or no option) does not count.
func hasSiblingRankLine(lines []string, i int, first string, options []Option) bool {
	for j := i + 1; j < len(lines); j++ {
		line := strings.TrimSpace(lines[j])
		if !isLaterRankedLine(line) {
			continue
		}
		other := firstOptionInSpan(line, options)
		if other != "" && other != first {
			return true
		}
	}
	return false
}

// isLaterRankedLine reports whether a trimmed line is a #2+ ranking item ("2." / "2)" ..
// "9." / "9)") once leading markdown emphasis is stripped.
func isLaterRankedLine(line string) bool {
	line = strings.TrimLeft(line, "*-_ \t")
	if len(line) < 2 {
		return false
	}
	d := line[0]
	if d < '2' || d > '9' {
		return false
	}
	return line[1] == '.' || line[1] == ')'
}

// rankSpan returns the text from the rank-cue position to the end of its line (or the
// rest of the response if the cue runs to the end) — the span that names the picked
// option for an inline rank cue.
func rankSpan(response string, idx int) string {
	tail := response[idx:]
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		return tail[:nl]
	}
	return tail
}

// isFirstRankedLine reports whether a trimmed line is the #1 item of a numbered ranking,
// i.e. it starts with "1." or "1)" once leading markdown emphasis ("*", "-") is stripped.
func isFirstRankedLine(line string) bool {
	line = strings.TrimLeft(line, "*-_ \t")
	return strings.HasPrefix(line, "1.") || strings.HasPrefix(line, "1)")
}

// tokenize splits text into lowercase whole alnum tokens (the same boundary rule
// containsToken uses), so position/overlap counting is word-boundary-safe.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// refuseMarkers signal a Verifier blocked the ship (the claim is wrong OR cannot be
// confirmed).
var refuseMarkers = []string{
	"do not ship", "don't ship", "not safe to ship", "not safe", "will crash",
	"will fail", "is incorrect", "is wrong", "is not correct", "refuse", "block",
	"out of range", "off-by-one", "off by one", "bug", "should not", "must not",
	"cannot ship", "can't ship", "no, ", "no.",
}

// acceptMarkers signal a Verifier judged the claim correct / safe to ship.
var acceptMarkers = []string{
	"safe to ship", "is correct", "is safe", "looks correct", "ship it", "approve",
	"accept", "is fine", "will run correctly", "passes", "confirmed", "yes, ", "yes.",
	"good to ship", "ready to ship", "no issues",
}

// honestMarkers signal an HONEST inability to verify (the never-confabulate move) — the
// SAME family the cognition probe's honestUnknownMarkers uses.
var honestMarkers = []string{
	"cannot verify", "can't verify", "cannot confirm", "can't confirm",
	"insufficient", "no access", "don't have access", "cannot determine",
	"can't determine", "unable to", "without access", "not able to verify",
	"no accessible source", "cannot be verified", "i do not know", "i don't know",
}

// extractVerifier classifies a verifier response into accept / refuse + the honest
// flag. The honest signal and any refuse marker imply REFUSE (an honest "cannot verify"
// is a refusal-to-ship). Otherwise the stronger of accept/refuse marker presence wins;
// a tie or no marker yields no decision (a hard fail — the worker did not decide).
func extractVerifier(response string) Verdict {
	low := strings.ToLower(response)
	honest := containsAny(low, honestMarkers)
	refuse := containsAny(low, refuseMarkers)
	accept := containsAny(low, acceptMarkers)

	v := Verdict{Reasoning: response, Honest: honest}
	switch {
	case honest:
		// An honest "cannot verify" is a refusal-to-ship, regardless of other markers.
		v.Decision = DecisionRefuse
	case refuse && !accept:
		v.Decision = DecisionRefuse
	case accept && !refuse:
		v.Decision = DecisionAccept
	default:
		// Ambiguous / no marker -> no decision extracted (hard fail at scoring).
	}
	return v
}

// containsAny reports whether the (already-lowercased) text contains any marker.
func containsAny(low string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
