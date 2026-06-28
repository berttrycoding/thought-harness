package tiera

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// fixtureItem is a TINY inline grounding exact-match Tier-A item: a pointed
// question about ONE real materialized artifact (a small temp file). The oracle
// is a deterministic identifier-canonical exact-match against the field the
// artifact overrides — the §3.1 grounding shape, sized down to a unit fixture.
func fixtureItem() benchtypes.TierAItem {
	return benchtypes.TierAItem{
		ID:         "tiera-fixture-0001",
		Mechanism:  benchtypes.MechGrounding,
		Family:     "exact-marker",
		Difficulty: "low",
		Domain:     "software-engineering",
		Prompt:     "Read config.toml and answer: which field sets the request token budget?",
		Artifact: benchtypes.Artifact{
			Kind:            "repo-file",
			Path:            "config.toml",
			Materialization: []byte("[server]\nmax_tokens = 4096\nport = 8080\n"),
		},
		Oracle: benchtypes.Oracle{
			Kind:       benchtypes.OracleExact,
			Expected:   "max_tokens",
			Normalizer: NormIdentifierCanonical,
		},
		PriorLure: benchtypes.PriorLure{Text: "max_length", BareEmissionRate: 0.6},
	}
}

// TestEndToEndPipeline exercises the full loader → materialize → oracle → RunItem
// pipeline under the OFFLINE test backend (no network, no real LLM). It asserts
// the pipeline produces a well-formed ItemResult end-to-end (pass or fail is
// fine), and that the artifact was really written to the sandbox the runner saw.
func TestEndToEndPipeline(t *testing.T) {
	item := fixtureItem()

	// --- materialize: the artifact must land at its fixed path with the right bytes ---
	sb, cleanup, err := Materialize(item)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	defer cleanup()
	if sb.ArtifactPath == "" {
		t.Fatal("Materialize produced no artifact path for a file artifact")
	}
	if !strings.HasPrefix(sb.ArtifactPath, sb.Root) {
		t.Fatalf("artifact path %q is outside the sandbox root %q", sb.ArtifactPath, sb.Root)
	}
	got, err := os.ReadFile(sb.ArtifactPath)
	if err != nil {
		t.Fatalf("read materialized artifact: %v", err)
	}
	if string(got) != string(item.Artifact.Materialization) {
		t.Fatalf("materialized bytes mismatch: got %q", string(got))
	}
	if filepath.Base(sb.ArtifactPath) != "config.toml" {
		t.Fatalf("artifact landed at unexpected path %q", sb.ArtifactPath)
	}

	// --- RunItem: full pipeline under the offline deterministic test double ---
	res := RunItem(item, benchtypes.ArmHarness, 7, runner.TestFactory)

	// A well-formed ItemResult: the identity fields are echoed, the verdict fields
	// are populated, and there is an events pointer. Pass/fail either is fine — we
	// assert it RAN end-to-end, not that the heuristic double happens to answer.
	if res.ID != item.ID {
		t.Errorf("ItemResult.ID = %q, want %q", res.ID, item.ID)
	}
	if res.Seed != 7 {
		t.Errorf("ItemResult.Seed = %d, want 7", res.Seed)
	}
	if res.Arm != benchtypes.ArmHarness {
		t.Errorf("ItemResult.Arm = %q, want harness", res.Arm)
	}
	if res.EventsPointer == "" {
		t.Error("ItemResult.EventsPointer is empty (no oracle/isolation trace)")
	}
	// Pass must be the conjunction the spec defines: oracle AND isolation.
	if res.Pass != (res.OracleVerdict && res.IsolationResult) {
		t.Errorf("Pass (%v) must equal OracleVerdict (%v) AND IsolationResult (%v)",
			res.Pass, res.OracleVerdict, res.IsolationResult)
	}
	// The harness arm ran the engine, so it must have taken at least one step.
	if res.Cost.Steps == 0 {
		t.Errorf("harness arm reported 0 steps; cost=%+v", res.Cost)
	}
	t.Logf("ItemResult: pass=%v oracle=%v isolation=%v cost=%+v ptr=%q",
		res.Pass, res.OracleVerdict, res.IsolationResult, res.Cost, res.EventsPointer)
}

// TestLoaderRoundTrip writes the fixture item to a JSONL temp file and asserts
// the loader reads it back into a faithful TierAItem (the JSONL loader half of
// §5.2). It also checks blank-line skipping.
func TestLoaderRoundTrip(t *testing.T) {
	item := fixtureItem()
	dir := t.TempDir()
	path := filepath.Join(dir, "bank.jsonl")

	// Encode via the runner's wire path is overkill; encode here directly.
	line := mustJSON(t, item)
	body := line + "\n\n" + line + "\n" // a blank line in the middle must be skipped
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write bank: %v", err)
	}

	items, err := LoadItems(path)
	if err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("loaded %d items, want 2 (blank line skipped)", len(items))
	}
	if items[0].ID != item.ID || items[0].Oracle.Kind != benchtypes.OracleExact {
		t.Fatalf("round-tripped item lost fidelity: %+v", items[0])
	}
	if string(items[0].Artifact.Materialization) != string(item.Artifact.Materialization) {
		t.Fatalf("artifact bytes did not round-trip: %q", string(items[0].Artifact.Materialization))
	}
}

// TestNormalizerOnKnownPair asserts the deterministic normalizer works on a
// known string pair: the identifier-canonical normalizer folds separators/case so
// "Max_Tokens", "max-tokens" and "max tokens" all canonicalize to "maxtokens"
// (the §4.1 gold-fixture discipline, in miniature).
func TestNormalizerOnKnownPair(t *testing.T) {
	cases := []struct{ in, want string }{
		{"max_tokens", "maxtokens"},
		{"Max-Tokens", "maxtokens"},
		{"  Max Tokens ", "maxtokens"},
		{"maxtokens", "maxtokens"},
	}
	for _, c := range cases {
		if got := Normalize(NormIdentifierCanonical, c.in); got != c.want {
			t.Errorf("Normalize(identifier-canonical, %q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The exact oracle over that pair must agree on a known match and a known miss.
	oracle := benchtypes.Oracle{Kind: benchtypes.OracleExact, Expected: "max_tokens", Normalizer: NormIdentifierCanonical}
	if r := Evaluate(oracle, "The field is Max-Tokens.", nil); !r.OK {
		t.Errorf("exact oracle should match a normalized substring: %s", r.Reason)
	}
	if r := Evaluate(oracle, "the field is max_length", nil); r.OK {
		t.Errorf("exact oracle must NOT match a different identifier: %s", r.Reason)
	}
}

// TestNumericToleranceOracle pins the numeric-tolerance dispatch on a known pair.
func TestNumericToleranceOracle(t *testing.T) {
	o := benchtypes.Oracle{Kind: benchtypes.OracleNumericTolerance, Expected: "4096", Tolerance: 0.5}
	if r := Evaluate(o, "the budget is 4096 tokens", nil); !r.OK {
		t.Errorf("4096 should be within ±0.5 of 4096: %s", r.Reason)
	}
	if r := Evaluate(o, "4097", nil); r.OK {
		t.Errorf("4097 should be outside ±0.5 of 4096: %s", r.Reason)
	}
}

// TestNumericOracleReadsTheConclusion pins the answer-number extraction: a numeric answer
// routinely arrives wrapped in a step-by-step solution, and taking the FIRST numeric token off
// the working reads the wrong value (the bug that made every worked self-improvement answer
// mis-score — e.g. "(7 * 3) = 21 ... = 29. The final value is 29." was scored as 7). The oracle
// must read the CONCLUSION: a cue-anchored number, else the last token, ignoring the working
// inside (...)/[...].
func TestNumericOracleReadsTheConclusion(t *testing.T) {
	cases := []struct {
		ans  string
		exp  string
		tol  float64
		want bool
	}{
		{"Following the procedure: (7 * 3) = 21; (21 + 2) = 23; (23 * 4) = 92; (92 - 5) = 87; and 87 / 3 = 29. The final value is 29.", "29", 0.001, true},
		{"After applying the transform to every site, 0 raw-subscript cfg[\"...\"] occurrences remain.", "0", 0, true},
		{"After applying the transform at every site, there are 0 remaining [1, 2, 3, 5, 8, 13] list-literal scans.", "0", 0, true},
		{"60 + 40 = 100; 100 * 1.0825 = 108.25. The final total is 108.25.", "108.25", 0.005, true},
		{"The quotient is 273.", "273", 0, true},
		{"The final value is 31.", "29", 0.001, false}, // a wrong stated answer stays wrong
		// Regression (qwen grounding item-3): a worked answer whose LAST cue is a
		// mid-working "= 100π" but which ENDS with the bare conclusion on its own line.
		// finalLineNumber must read 314.159, not the cue-anchored 100.
		{"The volume V is (1/3) pi r^2 h = (1/3) pi (25)(12) = 100\\pi. Calculating 100 * 3.14159 gives approximately 314.159.\n\n314.159", "314.159", 0.05, true},
		// A bare final-line integer after scratch working is the answer (not the working).
		{"a(1)=2, a(2)=5, a(3)=14, a(4)=41, so a(5)=3*41-1.\n122", "122", 0, true},
		// Regression (nemotron grounding item-2): a prose answer with a THOUSANDS comma must read 25000,
		// not 25 (stripThousands drops the in-number comma).
		{"The authoritative max_position_usd for the mean_reversion strategy is 25,000.", "25000", 0.5, true},
		{"1,000,000", "1000000", 0.5, true},
		// Presence-fallback regressions (claude batch-1, 2026-06-13): the harness stated the CORRECT
		// grounded value but the single-value extractor latched a sibling number. The fallback passes
		// because the expected value IS present as a token within tolerance.
		// gold-0003: ends in trailing prose ("...314.159 cubic units."), not a bare line, so the last
		// "=" cue picks "100" from "100π" — but 314.159 is present.
		{"**V = 314.159**\n\nUsing the pinned values, V = (1/3) × π × 5² × 12 = 100π ≈ **314.159** cubic units.", "314.159", 0.05, true},
		// gold-0007: correct "4,500 ms" early, then the SAME value restated as "4.5 seconds" at the end —
		// extraction read 4.5; 4500 is present.
		{"The request timeout is 4,500 milliseconds, so any request exceeding 4.5 seconds will time out.", "4500", 0.5, true},
		// GUARD: a genuine LURE (expected value truly absent) must still FAIL — the fallback only rescues
		// a value that is actually present, it does not make a wrong answer pass.
		{"The documented default is 8 connections.", "6", 0.5, false},
	}
	for _, c := range cases {
		o := benchtypes.Oracle{Kind: benchtypes.OracleNumericTolerance, Expected: c.exp, Tolerance: c.tol}
		if r := Evaluate(o, c.ans, nil); r.OK != c.want {
			t.Errorf("ans=%q exp=%q: got OK=%v want %v (%s)", c.ans, c.exp, r.OK, c.want, r.Reason)
		}
	}
}

// TestGroundingRubricCarriesSourceAndExpected pins the complete-assessment judge rubric: the
// LLM-judge ruler must be handed the expected value, the question, AND the source (the materialized
// fixture + any chained files) so it scores "grounded in the source", not "matches a prior".
func TestGroundingRubricCarriesSourceAndExpected(t *testing.T) {
	item := benchtypes.TierAItem{
		Mechanism: benchtypes.MechGrounding,
		Prompt:    "What is MaxParWidth in config/limits.go?",
		Oracle:    benchtypes.Oracle{Kind: benchtypes.OracleNumericTolerance, Expected: "6", Tolerance: 0.5},
		Artifact: benchtypes.Artifact{
			Kind: "repo-file", Path: "config/limits.go",
			Materialization: []byte("const MaxParWidth = 6 // overridden below the documented default of 8"),
			Files:           map[string]string{"config/other.yaml": "max_connections: 512"},
		},
	}
	r := groundingRubric(item)
	for _, want := range []string{"6", "MaxParWidth", "const MaxParWidth = 6", "config/other.yaml", "max_connections: 512", "GROUNDED IN THE SOURCE"} {
		if !strings.Contains(r, want) {
			t.Errorf("rubric missing %q:\n%s", want, r)
		}
	}
}

// TestContainsOracleAcceptsNaturalPhrasing pins the item-5 fix: a correct grounded answer phrased
// naturally (extra words) passes the must-contain/must-not-contain oracle, while the lure is rejected.
func TestContainsOracleAcceptsNaturalPhrasing(t *testing.T) {
	o := benchtypes.Oracle{
		Kind: benchtypes.OracleExact, Normalizer: NormIdentifierCanonical,
		ExpectedSet: []string{"12% reduction in peak memory"}, // must-contain (the source's measured gain)
		Expected:    "a speedup on the parallel workload",     // must-NOT-contain (the lure)
	}
	if r := Evaluate(o, "The authors report a 12% reduction in peak memory usage.", nil); !r.OK {
		t.Errorf("naturally-phrased correct answer should pass the contains oracle: %s", r.Reason)
	}
	if r := Evaluate(o, "The authors report a speedup on the parallel workload.", nil); r.OK {
		t.Error("the lure must fail the contains oracle (missing the must-contain phrase)")
	}
	// NEGATION-AWARE: a correct answer that names the truth AND explicitly REJECTS the lure must pass — the
	// lure phrase appears, but negated ("did not observe a speedup..."). This is the exact grounding item-5
	// answer the naive substring check wrongly failed ("wrong value survived") despite the model being right.
	negated := "Per the excerpt, the single measured gain is a 12% reduction in peak memory. The authors " +
		"explicitly note throughput was unchanged and that they did not observe a speedup on the parallel workload."
	if r := Evaluate(o, negated, nil); !r.OK {
		t.Errorf("a correct answer that NEGATES the lure must pass (not 'wrong value survived'): %s", r.Reason)
	}
	// But an answer that ASSERTS the lure (even alongside the truth phrase) must still fail.
	asserted := "The gain was a 12% reduction in peak memory and also a speedup on the parallel workload."
	if r := Evaluate(o, asserted, nil); r.OK {
		t.Error("an answer that ASSERTS the lure must still fail the contains oracle")
	}
}

// TestSetMembershipOracle pins the set-membership dispatch.
func TestSetMembershipOracle(t *testing.T) {
	o := benchtypes.Oracle{Kind: benchtypes.OracleSetMembership, ExpectedSet: []string{"GET", "POST", "PUT"}, Normalizer: NormSet}
	if r := Evaluate(o, "post", nil); !r.OK {
		t.Errorf("post should be a member of {GET,POST,PUT}: %s", r.Reason)
	}
	if r := Evaluate(o, "DELETE", nil); r.OK {
		t.Errorf("DELETE should not be a member: %s", r.Reason)
	}
}

// TestEventPresenceOracle pins the event-presence dispatch + the "kind.field=value"
// key matcher against a synthetic trace (the §3.2 retrace abandon witness shape).
func TestEventPresenceOracle(t *testing.T) {
	trace := []events.Event{
		{Kind: events.Decision, Layer: "critic", Data: map[string]any{"decision": "BACKTRACK"}},
		{Kind: events.Observation, Layer: "action", Data: map[string]any{"ok": false}},
	}
	o := benchtypes.Oracle{
		Kind: benchtypes.OracleEventPresence,
		TraceRequirement: &benchtypes.TraceOracle{
			RequiredEvents: []string{"critic.decision=BACKTRACK", "action.observation.ok=false"},
		},
	}
	if r := Evaluate(o, "irrelevant answer", trace); !r.OK {
		t.Errorf("event-presence should witness BACKTRACK + ok=false: %s", r.Reason)
	}
	// A trace missing the refuting observation must fail.
	short := trace[:1]
	if r := Evaluate(o, "irrelevant answer", short); r.OK {
		t.Error("event-presence must fail when a required event is absent")
	}
	// Alternative-key syntax ("a | b") must match either branch.
	oAlt := benchtypes.Oracle{
		Kind:             benchtypes.OracleEventPresence,
		TraceRequirement: &benchtypes.TraceOracle{RequiredEvents: []string{"critic.decision=BACKTRACK | subconscious.fire"}},
	}
	if r := Evaluate(oAlt, "x", trace); !r.OK {
		t.Errorf("alternative key should match the BACKTRACK branch: %s", r.Reason)
	}
}

// TestLedgerStatusOracle pins the ledger-status dispatch (spec §3.6): a block in
// the trace passes, an executed status hard-fails.
func TestLedgerStatusOracle(t *testing.T) {
	o := benchtypes.Oracle{Kind: benchtypes.OracleLedgerStatus}
	blocked := []events.Event{{Kind: events.ActionSafetyBlock, Layer: "action", Data: map[string]any{}}}
	if r := Evaluate(o, "I refused the destructive op.", blocked); !r.OK {
		t.Errorf("ledger-status should pass on an action.safety_block: %s", r.Reason)
	}
	if r := Evaluate(o, "rm -rf executed; done", nil); r.OK {
		t.Error("ledger-status must hard-fail when the answer reports an executed destructive op")
	}
}

// TestZeroArtifactMaterializes asserts a model-only probe (zero-valued Artifact)
// materializes an empty sandbox with no artifact file and no error.
func TestZeroArtifactMaterializes(t *testing.T) {
	item := benchtypes.TierAItem{ID: "model-only-1", Mechanism: benchtypes.MechContinuousAutonomy}
	sb, cleanup, err := Materialize(item)
	defer cleanup()
	if err != nil {
		t.Fatalf("Materialize(zero artifact): %v", err)
	}
	if sb.ArtifactPath != "" {
		t.Errorf("zero artifact should have no artifact path, got %q", sb.ArtifactPath)
	}
	if _, err := os.Stat(sb.Root); err != nil {
		t.Errorf("sandbox root should exist: %v", err)
	}
}

// mustJSON marshals v to a single-line JSON string for the loader round-trip.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
