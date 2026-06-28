// selector.go — the ONE shared, deterministic, registry-agnostic tool selector.
//
// THE BUG IT FIXES: tool selection used to be hardcoded in three divergent switches (the watched
// seam's toToolCall, the subconscious specialist primitives, the sub-agent's toolCall) that
// disagreed on which tools exist. The worst symptom: the reality-sourcer forms a file-read need as
// Text="source: <prose naming the file>" with Kind="measure", and the watched seam's switch mapped
// EVERY "measure" to run_tests — so a file READ always ran the test suite instead. read_file never
// fired from the seam.
//
// THE FIX: a single SelectTool(text, kind) the seam + subconscious both call. It is a SUPERSET of
// the three old switches (none of them loses a tool it could previously pick), and — load-bearing —
// a READ or SEARCH intent WINS over the measure->run_tests default. Pure regex/string ops, NO model
// call: this is the bounded interim grounding fix (Pattern A control, never the test double).
//
// Determinism: no clock, no randomness — the same (text, kind) always yields the same ToolCall, so
// the golden oracle and the stability ticks stay reproducible.
package action

import (
	"regexp"
	"strings"
)

var (
	// readVerbRe lights when the text asks to READ/look at an artifact. The verbs are deliberately
	// the "go look at reality" requests, not bare "output/execute" (which appear inside a FAILURE
	// observation and would create a feedback loop). The optional (?:s|ed|ing) suffix catches the
	// common inflections ("reading config/limits.go", "inspected foo.go") the reality-sourcer emits.
	readVerbRe = regexp.MustCompile(`(?i)\b(read|open|inspect|scan|examine|look|view|show)(?:s|ed|ing)?\b|\bcontents?\s+of\b|\bwhat\s+is\s+in\b`)

	// searchVerbRe lights when the text asks to FIND something in the tree.
	searchVerbRe = regexp.MustCompile(`(?i)\b(search|find|grep|locate)(?:s|es|ed|ing)?\b|\bwhere\s+is\b|\bwhere\s+in\b`)

	// filePathRe matches a token that looks like a file path: an optional dir prefix then a name with
	// one of the recognised code/config extensions. The leading boundary is a non-path char (or start)
	// so "config/limits.go" is captured whole even when embedded in prose like "reading config/limits.go.".
	filePathRe = regexp.MustCompile(`(?i)[\w./-]*\w\.(go|py|yaml|yml|md|txt|json|toml|sh|js|ts|rs|c|h|cpp|java|rb|cfg|ini|conf|env|mod|sum|lock)\b`)

	// knownFileRe matches well-known EXTENSIONLESS files the reality-sourcer asks to read (Makefile,
	// Dockerfile, ...). Without this, "read the Makefile" with Kind="measure" matches no path and falls
	// through to run_tests — the same class of mis-route the read-beats-measure fix exists to prevent.
	// Only truly extensionless names belong here: CMakeLists is intentionally OMITTED because its real
	// file is CMakeLists.txt (filePathRe already matches that) — listing the bare name would dispatch
	// read_file{path:"CMakeLists"}, a path that does not exist.
	knownFileRe = regexp.MustCompile(`(?i)\b(Makefile|Dockerfile|Gemfile|Rakefile|Procfile|Vagrantfile|Jenkinsfile)\b`)

	// pyTestTargetRe matches a python test target like "foo/bar.py" or "foo.py::test_x" — the measure
	// path's optional pytest target (mirrors the old seams.reTarget).
	pyTestTargetRe = regexp.MustCompile(`([\w./-]+\.py(?:::\w+)?)`)

	// backtickRe / knownCmdRe pull an explicit shell command from the text (mirrors the old
	// seams.reBacktick / seams.reCommand).
	backtickRe = regexp.MustCompile("`([^`]+)`")
	knownCmdRe = regexp.MustCompile(`\b((?:python3?|pytest|git|ls|cat|echo|make|npm|go|cargo)\b[^\n]*)`)

	// testWordingRe lights the run_tests path for measure/run intents that talk about tests/suite
	// without naming a .py target.
	testWordingRe = regexp.MustCompile(`(?i)\b(test|tests|suite|pytest)\b`)
)

// SelectTool maps a free-text intent (+ its optional Kind: "measure"/"run"/"reflect"/"send"/"") to a
// concrete ToolCall. It returns ok=false when nothing matches (the caller then declines / reasons /
// falls back). It is the single source of truth shared by the watched seam and the subconscious.
//
// PRECEDENCE (the whole bug fix): a READ or SEARCH intent is evaluated BEFORE the measure->run_tests
// default, so a file-read need (Kind="measure", text naming a file with a read verb) dispatches
// read_file{path} — not run_tests. The order is:
//  1. read_file  — a read/open/inspect/... verb AND a named file path
//  2. search     — a search/find/grep/... verb AND a usable pattern
//  3. run_tests  — Kind=="measure" with a .py target or test/suite wording
//  4. run_shell  — Kind=="run" with an explicit backtick/known-binary command
//  5. run_tests  — Kind=="run" with no explicit command (the "run it for real" default)
func SelectTool(text, kind string) (ToolCall, bool) {
	text = strings.TrimSpace(text)

	// 1. READ — wins over everything below (the grounding fix). A read verb (read/open/inspect/...),
	//    OR a SEARCH verb (find/locate/grep) that NAMES A CONCRETE FILE, dispatches read_file: the named
	//    file is the ground-truth source, so "find the cap in config/limits.go" / "locate it in foo.go"
	//    READS the file rather than tree-searching (you don't search the tree for a file you already
	//    named). A search verb with NO named file still falls to the tree-search in (2).
	if readVerbRe.MatchString(text) || searchVerbRe.MatchString(text) {
		if path := selectFilePath(text); path != "" {
			return ToolCall{Name: "read_file", Args: map[string]any{"path": path}}, true
		}
	}

	// 2. SEARCH — a find/grep verb with no named file + a usable identifier/pattern (tree-search).
	if searchVerbRe.MatchString(text) {
		if pat := selectPattern(text); pat != "" {
			return ToolCall{Name: "search", Args: map[string]any{"pattern": pat}}, true
		}
	}

	switch kind {
	case "measure":
		// 3. MEASURE — a probe of reality. A .py target / test wording runs the suite (optionally
		// targeted). This is the measure default the read/search branches above deliberately preempt.
		return ToolCall{Name: "run_tests", Args: targetArgs(selectTarget(text))}, true
	case "run":
		// 4/5. RUN — an explicit command runs the shell; otherwise "run it for real" runs the suite.
		if cmd := selectCommand(text); cmd != "" {
			return ToolCall{Name: "run_shell", Args: map[string]any{"command": cmd}}, true
		}
		return ToolCall{Name: "run_tests", Args: map[string]any{}}, true
	}

	return ToolCall{}, false
}

// selectFilePath returns the file path the intent names, with a CANONICAL-FIRST preference (#43): when
// the text names MORE THAN ONE path-shaped token (e.g. a search-result observation lists both
// `config/limits.go` and `internal/regulator/regulator_test.go`), a PRODUCTION (non-test) path is chosen
// over a test/fixture path so the conscious reads the symbol's real declaration, not a test override. With
// a single named path the result is byte-identical to the old first-match behaviour. Returns "" on no match.
func selectFilePath(text string) string {
	if all := filePathRe.FindAllString(text, -1); len(all) > 0 {
		var firstTest string
		for _, m := range all {
			p := strings.Trim(m, "\"'`.,;:()[]{}")
			if p == "" {
				continue
			}
			if isTestPath(p) {
				if firstTest == "" {
					firstTest = p
				}
				continue
			}
			return p // first PRODUCTION path wins
		}
		if firstTest != "" {
			return firstTest // only test paths named — read the one the intent gave
		}
	}
	if m := knownFileRe.FindString(text); m != "" {
		return strings.Trim(m, "\"'`.,;:()[]{}")
	}
	return ""
}

// isTestPath reports whether a file path lives in a TEST or FIXTURE location — a Go/Python/JS/Rust test
// file (foo_test.go / test_foo.py / foo.test.ts / foo_spec.rb) or anything under a test/tests/testdata/
// fixtures/golddata/mocks/__tests__ directory. selectFilePath uses it to prefer the canonical declaration
// over a test override when the intent names multiple paths (the #43 read-disambiguation gap). Pure string
// ops, deterministic.
func isTestPath(path string) bool {
	return testPathRe.MatchString(path)
}

// testPathRe is the path-only test/fixture matcher (a leaf-name or dir-segment test marker). It is the
// selector-side twin of builtins.reTestPath, but anchored on a bare PATH (no trailing ":line:" segment),
// since selectFilePath already extracted a clean path token.
var testPathRe = regexp.MustCompile(`(?i)(?:^|[\\/])(?:tests?|testdata|fixtures?|golddata|mocks?|__tests__|spec)[\\/]|(?:^|[\\/])test_[^\\/]*|_test\.[a-z0-9]+$|\.(?:test|spec)\.[a-z0-9]+$|_spec\.[a-z0-9]+$`)

// selectPattern returns the SYMBOL the request is about — the token you would actually grep for —
// not the first filler word.
//
// THE BUG IT FIXES (agentic find->read->extract loop break): the old version returned the first
// >3-char non-verb token, so a natural-language goal like "In this Go codebase, search the source
// files to find ... the value assigned to AlphaHigh" searched for "this"; "...SynthOfferCap"
// searched for "source"; "find the value of DoneConfidence" searched for "value". The grep then
// returned noise, the conscious never saw the value line, and it gave up or hallucinated
// "no filesystem access". (Measured: these were the dominant failures in the powered agentic probe.)
//
// THE FIX is a deterministic preference ladder over the >3-char, non-stopword tokens:
//  1. an identifier-SHAPED token (interior capital / underscore / digit — CamelCase like AlphaHigh,
//     snake_case, name+digit) — the distinctive shape of a symbol; longest wins.
//  2. else a Capitalized token (e.g. "Temperature", "Tolerance") — a single-word exported symbol that
//     tier 1 misses; sentence-initial filler is removed by the broadened stopword set.
//  3. else the first ordinary non-stopword word (the old behaviour, for the seam's prose intents).
//
// A backtick-quoted token wins outright (the author named it explicitly). Pure string ops,
// deterministic — the golden oracle and the stability ticks stay reproducible.
func selectPattern(text string) string {
	// 0. an explicit backtick-quoted token is the named target.
	if m := backtickRe.FindStringSubmatch(text); m != nil {
		if tok := identCore(m[1]); len([]rune(tok)) > 3 && !searchStopWords[strings.ToLower(tok)] {
			return tok
		}
	}
	var bestIdent, bestCap, bestWord string
	for _, w := range strings.Fields(text) {
		tok := identCore(w)
		if len([]rune(tok)) <= 3 {
			continue
		}
		if searchStopWords[strings.ToLower(tok)] {
			continue
		}
		switch {
		case isIdentifierShaped(tok):
			if len([]rune(tok)) >= len([]rune(bestIdent)) { // longest; tie -> later (the symbol usually trails the verb)
				bestIdent = tok
			}
		case tok[0] >= 'A' && tok[0] <= 'Z':
			if bestCap == "" {
				bestCap = tok
			}
		default:
			if bestWord == "" {
				bestWord = tok
			}
		}
	}
	if bestIdent != "" {
		return bestIdent
	}
	if bestCap != "" {
		return bestCap
	}
	return bestWord
}

// identCore extracts the longest run of identifier chars ([A-Za-z0-9_]) from a raw whitespace token,
// stripping surrounding prose punctuation ("AlphaHigh." -> "AlphaHigh", "`SynthOfferCap`" ->
// "SynthOfferCap"). Underscores are kept so a snake_case symbol survives.
func identCore(w string) string {
	best, cur := "", strings.Builder{}
	flush := func() {
		if cur.Len() > len(best) {
			best = cur.String()
		}
		cur.Reset()
	}
	for _, r := range w {
		if isSelectAlnum(r) || r == '_' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return best
}

// isIdentifierShaped reports whether tok has the distinctive shape of a code symbol — an INTERIOR
// uppercase letter (CamelCase/PascalCase: AlphaHigh, handleRequest, GEst), an underscore (snake_case),
// or a digit. A plain Capitalized word ("Temperature") is NOT identifier-shaped — it is caught by the
// capitalized tier instead. This is what lets the symbol win over English filler.
func isIdentifierShaped(tok string) bool {
	for i, r := range tok {
		switch {
		case r == '_':
			return true
		case r >= '0' && r <= '9':
			return true
		case r >= 'A' && r <= 'Z' && i > 0:
			return true
		}
	}
	return false
}

// searchStopWords are the search-verb / connector / filler words selectPattern skips so the pattern is
// the real symbol, not the request scaffolding. Beyond the search verbs it now drops the natural-
// language filler that surrounds a "find the value of X" goal (this/source/value/exact/...), so a
// verbose goal still resolves to its symbol.
var searchStopWords = map[string]bool{
	// search verbs / connectors (the original set)
	"search": true, "find": true, "grep": true, "locate": true, "where": true, "look": true,
	// natural-language filler in a "report the value assigned to X" goal
	"this": true, "that": true, "these": true, "those": true, "report": true, "value": true,
	"source": true, "sources": true, "file": true, "files": true, "exact": true, "numeric": true,
	"assigned": true, "codebase": true, "function": true, "usage": true, "tree": true,
	"from": true, "into": true, "with": true, "their": true, "which": true, "there": true,
}

// selectTarget returns a python test target (foo/bar.py or foo.py::test_x) from the text, or "".
func selectTarget(text string) string {
	if m := pyTestTargetRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// selectCommand pulls an explicit shell command: a backtick-quoted span first, else a leading
// known-binary command line, else "".
func selectCommand(text string) string {
	if m := backtickRe.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := knownCmdRe.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// targetArgs builds the run_tests args, omitting an empty target (an empty "target" arg is harmless
// but the old seam emitted {"target": ""} — keep that exact shape so existing goldens are stable).
func targetArgs(target string) map[string]any {
	return map[string]any{"target": target}
}

// isSelectAlnum reports whether r is ASCII alphanumeric — the chars a pattern token keeps.
func isSelectAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
