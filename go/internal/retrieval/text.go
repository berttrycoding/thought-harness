package retrieval

import "strings"

// stopwords are dropped before lexical comparison so overlap reflects CONTENT, not glue.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "and": true, "or": true, "is": true,
	"it": true, "in": true, "on": true, "for": true, "with": true, "that": true, "this": true,
	"as": true, "at": true, "by": true, "be": true, "are": true, "was": true, "your": true, "you": true,
	"how": true, "why": true, "what": true, "out": true, "so": true, "if": true, "into": true,
	"more": true, "most": true, "than": true, "do": true, "does": true, "not": true, "we": true,
	"our": true, "i": true, "my": true, "they": true, "their": true, "from": true, "up": true,
	"down": true, "off": true, "over": true, "then": true, "when": true, "which": true, "its": true,
}

// contentWords lowercases s, splits on non-alphanumerics, and drops stopwords + 1-2 char tokens —
// the content-word set the lexical scorer and the paraphrase gate both compare over.
func contentWords(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(w) <= 2 || stopwords[w] {
			continue
		}
		set[w] = true
	}
	return set
}

// jaccardSets is |A∩B| / |A∪B| over two content-word sets.
func jaccardSets(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// jaccard is the content-word Jaccard overlap of two raw strings.
func jaccard(a, b string) float64 { return jaccardSets(contentWords(a), contentWords(b)) }

// itemTokens is an item's content-word set, including its entity tags (a stronger relevance cue than
// raw prose, per the memory-stack spec).
func itemTokens(it Item) map[string]bool {
	set := contentWords(it.Text)
	for _, e := range it.Entities {
		for w := range contentWords(e) {
			set[w] = true
		}
	}
	return set
}

// LexicalScore is the word-overlap relevance of an item to a query: Jaccard of the query's content
// words against the item's (text + entities). This is the lexical baseline — strong on shared
// vocabulary, blind to paraphrase.
func LexicalScore(query string, it Item) float64 {
	return jaccardSets(contentWords(query), itemTokens(it))
}
