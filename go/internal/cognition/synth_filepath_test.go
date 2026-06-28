package cognition

import "testing"

// synth_filepath_test.go — FIX #35: synthFilePath must not false-positive on a bare "word/word" slash.
//
// The bug: the old detector returned ANY token containing "/", so a yes/no, and/or, a date, a ratio, or a
// URL inside a goal read as a local-file path. That suppressed isLookupQuestion (a lookup question with a
// slash looked like a local-file task -> no web lookup) and could mis-route the read precedence. A real
// path now requires a recognized file extension on the leaf. These tests pin BOTH halves: the
// false-positives are NOT paths, and real paths STILL are.

func TestSynthFilePathRejectsBareSlash(t *testing.T) {
	// Each goal contains a slash-bearing token that is NOT a local file. synthFilePath must return "".
	notPaths := []struct {
		name string
		goal string
	}{
		{"yes/no", "Is the answer yes/no for this question?"},
		{"and/or", "Should we use a cache and/or a queue here?"},
		{"either/or", "Is it an either/or decision between A and B?"},
		{"date", "What happened on 12/25 in that year?"},
		{"ratio", "What is the 3/4 majority threshold used for?"},
		{"fraction", "Compute 1/2 plus 1/4 of the total."},
		{"url-http", "Summarise the page at http://example.com/article please."},
		{"url-https", "What does https://en.wikipedia.org/wiki/Go say about it?"},
		{"slash-words", "Compare the win/loss record across seasons."},
		{"bare-leaf-no-ext", "Read the README and the LICENSE."}, // no recognized ext, no slash
	}
	for _, tc := range notPaths {
		t.Run(tc.name, func(t *testing.T) {
			if got := synthFilePath(tc.goal); got != "" {
				t.Fatalf("synthFilePath(%q) = %q, want \"\" (not a local-file path)", tc.goal, got)
			}
		})
	}
}

func TestSynthFilePathAcceptsRealPaths(t *testing.T) {
	// Each goal names a real local file; synthFilePath must return that path token.
	paths := []struct {
		name string
		goal string
		want string
	}{
		{"dir-file-go", "Open dir/file.go and report the value.", "dir/file.go"},
		{"config-json", "What does config.json set for the port?", "config.json"},
		// "./x/y.py" — the shared punctuation-trim strips the surrounding "." (pre-existing behaviour),
		// leaving "/x/y.py" which is still a valid path (leaf y.py). The point is it is STILL detected.
		{"relative-py", "Read ./x/y.py for the function body.", "/x/y.py"},
		{"nested-yaml", "Inspect deploy/prod/values.yaml for the replica count.", "deploy/prod/values.yaml"},
		{"go-mod", "Check go.mod for the module path.", "go.mod"},
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			if got := synthFilePath(tc.goal); got != tc.want {
				t.Fatalf("synthFilePath(%q) = %q, want %q (real path must still be detected)", tc.goal, got, tc.want)
			}
		})
	}
}

// TestIsLookupQuestionNoLongerSuppressedBySlash is the BEHAVIORAL delta: a factual lookup QUESTION that
// merely contains a slash (yes/no, and/or, a ratio) is now correctly recognized as a lookup question
// (web-lookup eligible), where the old slash-as-path false-positive marked it a local-file task and
// suppressed the lookup. A genuine local-file task still returns false (the path wins).
func TestIsLookupQuestionNoLongerSuppressedBySlash(t *testing.T) {
	lookups := []string{
		"Were Scott Derrickson and Ed Wood the same nationality, yes/no?",
		"Is the Eiffel Tower taller and/or older than Big Ben?",
		"What is the 3/4 majority rule named after?",
	}
	for _, g := range lookups {
		if !isLookupQuestion(g, lc(g)) {
			t.Fatalf("isLookupQuestion(%q) = false, want true (a question with a bare slash is NOT a local-file task)", g)
		}
	}
	// a genuine local-file task still suppresses the lookup (the path wins, as designed).
	fileTask := "Read config.json and report the port."
	if isLookupQuestion(fileTask, lc(fileTask)) {
		t.Fatalf("isLookupQuestion(%q) = true, want false (a real local-file task is not an external lookup)", fileTask)
	}
}

// lc lowercases like goalText does for the lc argument isLookupQuestion expects.
func lc(s string) string { return goalText(s, nil) }
