package graph

import (
	"regexp"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// diagnosticRE matches a question that asks for *understanding* (why/what/how/which/causes…)
// rather than commanding an effect on the world. These are answered by REASONING, never by
// fabricating an action + a ground-truth result. Word-anchored (\b both ends) so it can't fire on
// an unrelated substring. Mirrors Python graph._DIAGNOSTIC exactly.
var diagnosticRE = regexp.MustCompile(
	`\b(why|what|which|whether|how|cause|causes|reason|explain|diagnos\w*|likely|difference|` +
		`compare|tell .* apart|should i|is it|are there)\b`)

// imperativeRE matches imperatives that genuinely call for a world-facing action (not a generic
// substring like the "check" inside "checkout"). Word-anchored. Mirrors Python graph._IMPERATIVE
// exactly.
var imperativeRE = regexp.MustCompile(
	`\b(send|email|post|reply|run|execute|compile|deploy|refactor|test|tests|benchmark|` +
		`measure|optimi[sz]\w*|improv\w*|fix|ship)\b`)

// FormIntention distils the active branch into a single effectful action to take on the world.
//
// A *diagnostic question* ("why am I getting 500 errors?", "DB pool or memory leak?") asks for
// understanding, not an effect — it routes to "reflect" (reason), never to a fabricated action.
// Only a genuine imperative (run/test/refactor/send …) opens the watched seam. Keyword matching is
// word-anchored so "check" can no longer fire on the "check" inside "checkout".
//
// Mirrors Python ThoughtGraph.form_intention. EXPORTED — the watched seam reads it to open to
// reality.
func (g *ThoughtGraph) FormIntention() types.Intention {
	ctx := g.ActiveContext()
	var focus string
	if len(ctx) > 0 {
		focus = ctx[len(ctx)-1].Short(60)
	} else {
		focus = g.Goal
	}
	low := strings.ToLower(g.Goal + " " + focus)
	diagnostic := diagnosticRE.MatchString(low)
	imperative := imperativeRE.MatchString(low)

	// has reports whether ANY of the given words occurs at a word boundary in low. Faithful to
	// Python's `re.search(rf"\b{w}", low)`: a LEADING word boundary only (no trailing \b), so the
	// word acts as a PREFIX match — "divi" fires inside "division", "comput" inside "compute".
	has := func(words ...string) bool {
		for _, w := range words {
			if wordPrefixRE(w).MatchString(low) {
				return true
			}
		}
		return false
	}

	var kind, text string
	switch {
	case has("send", "email", "message", "post") || (has("reply") && !diagnostic):
		kind, text = "send", "send: "+focus
	case has("divi", "comput", "calcul", "arithmetic", "multipl") || strings.Contains(low, "by hand"):
		kind, text = "measure", "work the arithmetic through carefully and check it"
	case diagnostic && !imperative:
		// A question seeking understanding with no action to take: reason it through.
		kind, text = "reflect", "reason about it as far as I can"
	case has("refactor", "regress", "benchmark", "test", "measure", "optimi", "improv") ||
		strings.Contains(low, "safe"):
		kind, text = "measure", "run the test suite to confirm behaviour is preserved"
	case has("run", "execute", "compile", "script", "program", "runtime"):
		kind, text = "run", "run it for real: "+focus
	default:
		// No real-world action available (an abstract/reasoning question) — don't run code.
		kind, text = "reflect", "reason about it as far as I can"
	}
	ab := g.ActiveBranch
	return types.Intention{Text: text, Kind: kind, BranchID: &ab}
}

// wordPrefixCache memoises the per-word `\b<word>` matchers built by has(). The keyword tables are
// fixed, so this is a small bounded cache; building these once avoids recompiling the same pattern
// every FormIntention call. The patterns are leading-boundary-only (a PREFIX match), faithful to
// Python's `re.search(rf"\b{w}", low)`.
//
// The cache is package-global and may be hit concurrently (e.g. the bench driver runs many
// independent engines in parallel), so it is guarded by a mutex. The compiled regex for a given
// word is identical no matter who builds it, so a benign double-build under a race would be
// harmless — but a concurrent map read/write is a hard data race, so the lock is required.
var (
	wordPrefixMu    sync.RWMutex
	wordPrefixCache = map[string]*regexp.Regexp{}
)

func wordPrefixRE(word string) *regexp.Regexp {
	wordPrefixMu.RLock()
	re, ok := wordPrefixCache[word]
	wordPrefixMu.RUnlock()
	if ok {
		return re
	}
	re = regexp.MustCompile(`\b` + regexp.QuoteMeta(word))
	wordPrefixMu.Lock()
	wordPrefixCache[word] = re
	wordPrefixMu.Unlock()
	return re
}
