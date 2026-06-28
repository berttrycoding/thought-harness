// Package legible implements the legibility tag CONTRACT between the conscious and the hidden seam —
// the compact, machine-legible control envelope the conscious emits at the head of each thought so the
// seam ROUTES and FILTERS with a regex instead of a second LLM call (design/05-LEGIBLE-GENERATION.md).
//
// The envelope (05 §5a), in fixed order, with the literal unicode angle-bracket + middle-dot frame:
//
//	⟨op=<move|novel:desc> · domain=<known|other> · conf=<0..1> · act=<yes|no>⟩ <prose>
//
// The single load-bearing constraint (05 §4b "one source of truth for the contract"): the conscious's
// PROMPT (what tags it MAY emit) and the seam's PARSER (what tags it can READ) are BOTH derived from ONE
// TagRegistry — so the two sides cannot drift. Add a tag once, in the registry, and both the prompt
// fragment and the regex regenerate together.
//
// The registry's known `op` vocabulary IS the cognition operator registry (operators by name); its known
// `domain` set is cognition.KnownDomains(). Both views (PromptFragment + the compiled regex) are derived
// from those two sets at construction time.
//
// LEAF over internal/cognition: pure stdlib + the operator/domain vocabulary. No seam, no backend, no
// engine — this package only defines + parses the contract; routing on it lives in internal/seams.
package legible

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// The literal envelope frame characters (05 §5a). Kept as named constants so the prompt fragment and the
// regex are built from the SAME glyphs — a frame change is one edit, not two.
const (
	envOpen  = "⟨" // ⟨ U+27E8 MATHEMATICAL LEFT ANGLE BRACKET
	envClose = "⟩" // ⟩ U+27E9 MATHEMATICAL RIGHT ANGLE BRACKET
	envSep   = "·" // · U+00B7 MIDDLE DOT (field separator, surrounded by spaces in the contract)
)

// novelPrefix is the open escape (05 §2/§5a): `op=novel:<short-desc>` flags a move the registry lacks —
// it routes to the fallback AND seeds a mint-candidate signal. The conscious is never told "only these N
// moves"; it is told "tag a known move if one fits, else say novel and describe it."
const novelPrefix = "novel:"

// Tag is the parsed control envelope. It is a ROUTING PRIOR, not truth (05 §9): the seam routes/pre-filters
// on it; the grounding spine still decides anything high-stakes.
type Tag struct {
	// Op is the operator/Move token the conscious self-assigned (the raw token after `op=`), e.g.
	// "decompose". For a novel tag this is the literal "novel:<desc>" payload; prefer NovelDesc for the
	// description and Op for the raw token.
	Op string
	// Domain is the domain token after `domain=` (e.g. "planning", or "other" when nothing fits).
	Domain string
	// Conf is the conscious's own confidence prior in [0,1] (clamped on parse). A prior, not truth (§9).
	Conf float64
	// Act reports whether this thought wants to cross the watched seam to reality (`act=yes`).
	Act bool

	// IsNovel is true when op was `novel:<desc>` — the open escape; routes to fallback + mint signal.
	IsNovel bool
	// NovelDesc is the free-text description after `novel:` (empty unless IsNovel).
	NovelDesc string
	// Known is true when Op is a registered operator (fast-path eligible). False (advisory) when the op is
	// unknown OR novel — the route falls back to the LLM classifier. Known is membership against the SAME
	// op set the prompt enumerates, so the prompt and this verdict cannot disagree.
	Known bool
}

// TagRegistry is the single source of truth for the legibility contract. It snapshots the known op
// vocabulary (cognition operator names) + the known domain set at construction, then derives BOTH the
// prompt fragment and the regex parser from that one snapshot.
//
// Snapshotting (rather than holding a live *OperatorRegistry) makes the contract a stable, versionable
// view: the prompt the conscious sees and the parser the seam runs describe the SAME vocabulary for the
// life of this TagRegistry. Rebuild it (NewTagRegistry) at the consolidation tick when the lexicon grows
// (05 §4b "cadence, not churn") so both views regenerate together.
type TagRegistry struct {
	ops      []string            // known operator names, in registry (insertion) order — for the prompt
	opSet    map[string]struct{} // membership set for the Known verdict
	domains  []string            // known domain tags, in canonical order — for the prompt
	envelope *regexp.Regexp      // the derived parser (one regex over the whole envelope)
}

// envelopeRe is the ONE regex that parses the whole envelope. It is intentionally GENERAL over the op and
// domain tokens (it matches any non-separator token) rather than baking the vocabulary into an alternation:
//   - the op token is captured raw and classified against opSet (so Known cannot drift from the prompt, and
//     a freshly-minted op needs no regex change — only a registry rebuild);
//   - `novel:<desc>` is matched as the bounded WRAPPER `novel:`, routing the free-text payload onward — the
//     regex detects the SHAPE of "I don't know this", never the novelty itself (05 §4c).
//
// Fixed field order, small + anchored at the head of the text (^\s*). Tokens may not contain the separator,
// the close bracket, or surrounding whitespace; conf is a bare number; act is yes/no (case-insensitive).
// Built once at construction from the frame constants so the glyphs match the prompt's glyphs exactly.
func buildEnvelopeRe() *regexp.Regexp {
	o := regexp.QuoteMeta(envOpen)
	c := regexp.QuoteMeta(envClose)
	sep := regexp.QuoteMeta(envSep)
	// A field token: one-or-more chars that are not whitespace, not the separator dot, not the close
	// bracket. (`novel:` payloads can hold spaces — so op uses a looser token; see below.)
	tok := `[^\s` + sep + c + `]+`
	// The op field allows spaces in the novel:<desc> payload, so it greedily takes everything up to the
	// ` · ` separator that introduces the domain field. The non-greedy capture stops at the first sep.
	opField := `(.+?)`
	field := func(name, pat string) string {
		return name + `=` + pat
	}
	// ⟨ op=<...> · domain=<tok> · conf=<num> · act=<yes|no> ⟩
	pattern := `^\s*` + o + `\s*` +
		field("op", opField) + `\s*` + sep + `\s*` +
		field("domain", `(`+tok+`)`) + `\s*` + sep + `\s*` +
		field("conf", `([0-9]*\.?[0-9]+)`) + `\s*` + sep + `\s*` +
		field("act", `(?i:(yes|no))`) + `\s*` + c
	return regexp.MustCompile(pattern)
}

// NewTagRegistry snapshots the contract from the operator registry (known op vocabulary, by name) + the
// canonical known-domain set (cognition.KnownDomains()). Both derived views — PromptFragment and the regex
// — are produced from this one snapshot. Pass the live *cognition.OperatorRegistry whose names are the op
// vocabulary; the snapshot is taken now (later mints require a rebuild — the versioned-contract discipline,
// 05 §4b).
func NewTagRegistry(ops *cognition.OperatorRegistry) *TagRegistry {
	names := ops.Names()
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return &TagRegistry{
		ops:      names,
		opSet:    set,
		domains:  cognition.KnownDomains(),
		envelope: buildEnvelopeRe(),
	}
}

// KnownOps returns the known operator vocabulary, in registry order (the op set the prompt enumerates and
// the Known verdict checks against — one list, two uses).
func (r *TagRegistry) KnownOps() []string {
	out := make([]string, len(r.ops))
	copy(out, r.ops)
	return out
}

// KnownDomains returns the known domain vocabulary, in canonical order.
func (r *TagRegistry) KnownDomains() []string {
	out := make([]string, len(r.domains))
	copy(out, r.domains)
	return out
}

// IsKnownOp reports whether op is a registered operator (the fast-path membership test).
func (r *TagRegistry) IsKnownOp(op string) bool {
	_, ok := r.opSet[op]
	return ok
}

// ParseTag parses the leading legibility envelope of text. On success it returns the parsed Tag, the prose
// with the envelope STRIPPED clean (leading whitespace trimmed), and ok=true. On a missing or malformed
// envelope it returns the zero Tag, the original text unchanged, and ok=false — so the caller falls back to
// the LLM path it uses today (legibility is an optimisation with a safety net, never a hard gate — 05 §2).
//
// Op classification:
//   - `op=novel:<desc>` -> IsNovel=true, NovelDesc=<desc>, Known=false (fallback + mint signal).
//   - `op=<name>` where name is registered -> Known=true (fast-path eligible).
//   - `op=<name>` not in the registry -> Known=false (advisory: the route falls back).
//
// Note ok reports a well-FORMED envelope (the syntax parsed), independent of Known (whether the op is in
// the vocabulary). A well-formed tag with an unknown/novel op is ok=true, Known=false.
func (r *TagRegistry) ParseTag(text string) (Tag, string, bool) {
	loc := r.envelope.FindStringSubmatchIndex(text)
	if loc == nil {
		return Tag{}, text, false
	}
	groups := r.envelope.FindStringSubmatch(text)
	// groups: [full, op, domain, conf, act]
	if len(groups) < 5 {
		return Tag{}, text, false
	}
	rawOp := strings.TrimSpace(groups[1])
	domain := strings.TrimSpace(groups[2])
	confStr := strings.TrimSpace(groups[3])
	actStr := strings.ToLower(strings.TrimSpace(groups[4]))

	conf, err := strconv.ParseFloat(confStr, 64)
	if err != nil {
		// The regex already constrained conf to a numeric shape; a parse failure here means a malformed
		// envelope — fall back rather than guess.
		return Tag{}, text, false
	}
	if conf < 0 {
		conf = 0
	} else if conf > 1 {
		conf = 1
	}

	tag := Tag{
		Op:     rawOp,
		Domain: domain,
		Conf:   conf,
		Act:    actStr == "yes",
	}
	if rest, isNovel := strings.CutPrefix(rawOp, novelPrefix); isNovel {
		tag.IsNovel = true
		tag.NovelDesc = strings.TrimSpace(rest)
		tag.Known = false // novel is by definition not in the vocabulary -> fallback + mint signal
	} else {
		tag.Known = r.IsKnownOp(rawOp)
	}

	// Strip the envelope: everything after the matched region is the prose. loc[1] is the end index of the
	// whole match (through the closing bracket). Trim the single separating space the contract puts after ⟩.
	prose := strings.TrimLeft(text[loc[1]:], " \t")
	return tag, prose, true
}

// PromptFragment renders the legibility-contract instruction injected into the Generate prompt (05 §5c
// "prompt-shaped"). It is derived from the SAME snapshot the parser uses — the known op vocabulary and the
// known domain set — so what the conscious is told it MAY emit is exactly what the seam can READ. The
// `novel:<desc>` escape is always offered (open vocabulary, 05 §2): the conscious is never hard-closed onto
// the current set, it is told to reach for a known move if one fits, else say novel and describe it.
func (r *TagRegistry) PromptFragment() string {
	var b strings.Builder
	b.WriteString("Begin every thought with a one-line control tag in this exact format, then your prose:\n")
	b.WriteString("  ")
	b.WriteString(envOpen)
	b.WriteString("op=<move> ")
	b.WriteString(envSep)
	b.WriteString(" domain=<domain> ")
	b.WriteString(envSep)
	b.WriteString(" conf=<0..1> ")
	b.WriteString(envSep)
	b.WriteString(" act=<yes|no>")
	b.WriteString(envClose)
	b.WriteString(" <your thought>\n")
	b.WriteString("  - op: the move you are making. Use ONE of the known moves if one fits:\n    ")
	b.WriteString(strings.Join(r.ops, ", "))
	b.WriteString("\n    If none fits, write op=")
	b.WriteString(novelPrefix)
	b.WriteString("<short description of the move> instead (do not force a wrong move).\n")
	b.WriteString("  - domain: ONE of: ")
	b.WriteString(strings.Join(r.domains, ", "))
	b.WriteString("; or 'other' if none fits.\n")
	b.WriteString("  - conf: your confidence in this thought, a number from 0 to 1.\n")
	b.WriteString("  - act: yes if this thought should act on the outside world, else no.\n")
	b.WriteString("The tag is internal routing only and is stripped before anything is shown; keep your prose clean and unconstrained.")
	return b.String()
}
