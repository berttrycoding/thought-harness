package action

import "testing"

// TestSelectTool covers the ONE shared selector that fixes the grounding bug: a read/search intent
// must win over the measure->run_tests default, file paths route to read_file, search patterns route
// to search, and a bare measure/run intent still runs the suite.
func TestSelectTool(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		kind     string
		wantTool string // "" ⇒ expect ok=false
		wantArg  string // the load-bearing arg (path / pattern / target / command); "" ⇒ not checked
		argKey   string // which arg key to read for wantArg
	}{
		// --- read intent -> read_file{path} ---
		{name: "read verb + go path", text: "read config/limits.go to confirm", kind: "",
			wantTool: "read_file", wantArg: "config/limits.go", argKey: "path"},
		{name: "open verb + yaml path", text: "open the deploy.yaml settings", kind: "",
			wantTool: "read_file", wantArg: "deploy.yaml", argKey: "path"},
		{name: "inspect inflection", text: "inspecting handlers/router.py for the route", kind: "",
			wantTool: "read_file", wantArg: "handlers/router.py", argKey: "path"},

		// --- THE BUG: the reality-sourcer's measure-kind file-read MUST read, not run tests ---
		{name: "PRECEDENCE: read beats measure default", text: "source: reading config/limits.go for the cap",
			kind: "measure", wantTool: "read_file", wantArg: "config/limits.go", argKey: "path"},

		// --- search VERB but a NAMED FILE -> read the file (you don't tree-search a file you named) ---
		{name: "find-in-file reads the file", text: "find the cap in config/limits.go", kind: "measure",
			wantTool: "read_file", wantArg: "config/limits.go", argKey: "path"},
		{name: "grep-in-file reads the file", text: "grep MaxParWidth in internal/cognition/program.go", kind: "",
			wantTool: "read_file", wantArg: "internal/cognition/program.go", argKey: "path"},

		// --- extensionless well-known files route to read_file (not run_tests) ---
		{name: "read the Makefile", text: "read the Makefile to confirm the target", kind: "measure",
			wantTool: "read_file", wantArg: "Makefile", argKey: "path"},
		{name: "read go.mod", text: "open go.mod for the module path", kind: "",
			wantTool: "read_file", wantArg: "go.mod", argKey: "path"},

		// --- search intent -> search{pattern} ---
		{name: "search verb + identifier", text: "search for handleRequest in the tree", kind: "",
			wantTool: "search", wantArg: "handleRequest", argKey: "pattern"},
		{name: "grep verb", text: "grep for SelectTool usage", kind: "",
			wantTool: "search", wantArg: "SelectTool", argKey: "pattern"},
		{name: "locate verb", text: "locate the parseConfig function", kind: "",
			wantTool: "search", wantArg: "parseConfig", argKey: "pattern"},

		// --- THE PATTERN BUG: a verbose natural-language goal must search for the SYMBOL, not the
		//     first filler word ("this"/"source"/"value"). These are the real agentic-probe goals. ---
		{name: "verbose goal -> CamelCase symbol", kind: "",
			text:     "In this Go codebase, search the source files to find and report the exact numeric value assigned to AlphaHigh.",
			wantTool: "search", wantArg: "AlphaHigh", argKey: "pattern"},
		{name: "verbose goal -> compound symbol", kind: "",
			text:     "search the source files to find and report the exact numeric value assigned to SynthOfferCap.",
			wantTool: "search", wantArg: "SynthOfferCap", argKey: "pattern"},
		{name: "value-of goal -> symbol not 'value'", kind: "",
			text: "find the value of DoneConfidence in the source", wantTool: "search", wantArg: "DoneConfidence", argKey: "pattern"},
		{name: "capitalized single-word symbol", kind: "",
			text:     "In this Go codebase, search the source files to find and report the exact numeric value assigned to Temperature.",
			wantTool: "search", wantArg: "Temperature", argKey: "pattern"},
		{name: "backtick-named symbol wins", text: "search the tree for `MergeThreshold` please", kind: "",
			wantTool: "search", wantArg: "MergeThreshold", argKey: "pattern"},

		// --- measure/test -> run_tests ---
		{name: "measure with py target", text: "measure tests/test_engine.py", kind: "measure",
			wantTool: "run_tests", wantArg: "tests/test_engine.py", argKey: "target"},
		{name: "measure with suite wording, no target", text: "run the suite and confirm", kind: "measure",
			wantTool: "run_tests"},
		{name: "bare measure", text: "verify the change holds", kind: "measure",
			wantTool: "run_tests"},

		// --- run -> run_shell (explicit command) or run_tests (default) ---
		{name: "run with backtick command", text: "run it: `go build ./...`", kind: "run",
			wantTool: "run_shell", wantArg: "go build ./...", argKey: "command"},
		{name: "run with known binary", text: "git status please", kind: "run",
			wantTool: "run_shell", wantArg: "git status please", argKey: "command"},
		{name: "run no command -> tests", text: "run it for real", kind: "run",
			wantTool: "run_tests"},

		// --- nothing matches ---
		{name: "reflect declines", text: "think it through carefully", kind: "reflect", wantTool: ""},
		{name: "send declines", text: "deliver the message", kind: "send", wantTool: ""},
		{name: "empty kind, no verb/path", text: "the result looks plausible", kind: "", wantTool: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, ok := SelectTool(tc.text, tc.kind)
			if tc.wantTool == "" {
				if ok {
					t.Fatalf("expected no tool, got %s %v", call.Name, call.Args)
				}
				return
			}
			if !ok {
				t.Fatalf("expected tool %s, got ok=false", tc.wantTool)
			}
			if call.Name != tc.wantTool {
				t.Fatalf("tool = %s, want %s (args %v)", call.Name, tc.wantTool, call.Args)
			}
			if tc.wantArg != "" {
				got, _ := call.Args[tc.argKey].(string)
				if got != tc.wantArg {
					t.Fatalf("%s arg = %q, want %q", tc.argKey, got, tc.wantArg)
				}
			}
		})
	}
}

// TestSelectToolReadBeatsMeasure isolates the precedence rule that IS the fix: with Kind="measure"
// (which the old switch mapped unconditionally to run_tests), a read intent that names a file now
// dispatches read_file. This is the exact shape the reality-sourcer (engine/knowledge.go) emits.
func TestSelectToolReadBeatsMeasure(t *testing.T) {
	call, ok := SelectTool("source: reading internal/config/limits.go", "measure")
	if !ok {
		t.Fatal("expected a tool call")
	}
	if call.Name != "read_file" {
		t.Fatalf("the measure-kind read intention must select read_file, got %s", call.Name)
	}
	if got, _ := call.Args["path"].(string); got != "internal/config/limits.go" {
		t.Fatalf("path = %q, want internal/config/limits.go", got)
	}
}

// TestSelectFilePathPrefersCanonical is the #43 test-vs-production read-disambiguation gap: when an intent
// (e.g. a search-result observation the conscious is reading from) names BOTH a test/fixture path AND the
// production path, selectFilePath must pick the PRODUCTION path so the conscious reads the symbol's real
// declaration (`LearnRate = 0.05` in config), not the test override (`LearnRate = 0.5` in a regulator test).
// BEFORE the fix selectFilePath returned the FIRST path-shaped token regardless of test-vs-prod, so a hit
// list that happened to put the test file first dispatched read_file on the test file -> the wrong value.
func TestSelectFilePathPrefersCanonical(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		// test file named FIRST, production second -> production wins (the load-bearing case).
		{"test-then-prod (go _test)", "read internal/regulator/regulator_test.go and config/limits.go for LearnRate", "config/limits.go"},
		// testdata dir then production.
		{"testdata-dir then prod", "found it in internal/seams/testdata/py_s6.go and internal/regulator/gain.go", "internal/regulator/gain.go"},
		// python test_ prefix then production.
		{"py test_ prefix then prod", "tests/test_engine.py mocks it; the real one is engine/config.py", "engine/config.py"},
		// only a test path named -> read the one the intent gave (no production alternative).
		{"only a test path", "open internal/regulator/regulator_test.go", "internal/regulator/regulator_test.go"},
		// single production path -> unchanged (byte-identical to the old first-match behaviour).
		{"single prod path", "read config/limits.go to confirm", "config/limits.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectFilePath(tc.text); got != tc.want {
				t.Fatalf("selectFilePath(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestSelectToolReadDisambiguation drives the same gap through the public SelectTool entry: a read intent
// naming both a test override and the canonical file must dispatch read_file on the CANONICAL one.
func TestSelectToolReadDisambiguation(t *testing.T) {
	call, ok := SelectTool("read regulator_test.go (overrides) and internal/regulator/gain.go for the LamStar value", "")
	if !ok || call.Name != "read_file" {
		t.Fatalf("expected read_file, got %s ok=%v", call.Name, ok)
	}
	if got, _ := call.Args["path"].(string); got != "internal/regulator/gain.go" {
		t.Fatalf("path = %q, want internal/regulator/gain.go (the canonical declaration, not the test override)", got)
	}
}

// TestSelectToolDeterminism: the same (text, kind) always yields the same call — no clock/randomness.
func TestSelectToolDeterminism(t *testing.T) {
	for i := 0; i < 50; i++ {
		call, ok := SelectTool("read config/limits.go", "measure")
		if !ok || call.Name != "read_file" || call.Args["path"] != "config/limits.go" {
			t.Fatalf("non-deterministic selection on iter %d: %v ok=%v", i, call, ok)
		}
	}
}
