package llm

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fixtures shared across the parse tests.
func ctxFixture() []types.Thought {
	return []types.Thought{{ID: 1, Text: "should I rewrite the parser?", Source: types.GENERATED}}
}

// TestLoadsObjectSalvage: the lenient object slice tolerates prose / fences around the JSON.
func TestLoadsObjectSalvage(t *testing.T) {
	cases := []struct {
		in        string
		wantKey   string
		wantValue string
	}{
		{`{"verdict":"ADMIT","confidence":0.9}`, "verdict", "ADMIT"},
		{"Sure! Here is the JSON:\n```json\n{\"verdict\": \"REJECT\"}\n```", "verdict", "REJECT"},
		{"prose {\"verdict\":\"FLAG\",\"reason\":\"x\"} trailing", "verdict", "FLAG"},
	}
	for _, c := range cases {
		obj, err := loadsObject(c.in)
		if err != nil {
			t.Fatalf("loadsObject(%q) errored: %v", c.in, err)
		}
		if got := asString(obj[c.wantKey]); got != c.wantValue {
			t.Errorf("loadsObject(%q)[%q] = %q, want %q", c.in, c.wantKey, got, c.wantValue)
		}
	}
	if _, err := loadsObject("no object here"); err == nil {
		t.Error("loadsObject on non-JSON should error, not panic")
	}
}

// TestStripThink removes inline reasoning blocks (multi-line).
func TestStripThink(t *testing.T) {
	got := stripThink("<think>\nlots of\nreasoning\n</think>the answer")
	if got != "the answer" {
		t.Errorf("stripThink = %q, want %q", got, "the answer")
	}
}

// TestExtractContentSalvage: prefer content; salvage reasoning_content only when enabled + content empty.
func TestExtractContentSalvage(t *testing.T) {
	// content present → used, reasoning ignored.
	msg := map[string]any{"content": "hello", "reasoning_content": "ignored"}
	if got := extractContent(msg, true); got != "hello" {
		t.Errorf("extractContent content = %q, want hello", got)
	}
	// content empty, salvage on → reasoning_content used.
	msg = map[string]any{"content": "", "reasoning_content": "salvaged answer"}
	if got := extractContent(msg, true); got != "salvaged answer" {
		t.Errorf("extractContent salvage = %q, want salvaged answer", got)
	}
	// content empty, salvage off → empty (clean fallback beats voicing meta-reasoning).
	if got := extractContent(msg, false); got != "" {
		t.Errorf("extractContent no-salvage = %q, want empty", got)
	}
}

// TestParseVerdict parses the JSON shape + clamps + rejects bad verdict names.
func TestParseVerdict(t *testing.T) {
	v, ok := parseVerdict(`{"verdict":"FLAG","confidence":1.5,"reason":"hedged"}`)
	if !ok {
		t.Fatal("parseVerdict should succeed on a well-formed object")
	}
	if v.Verdict != types.FLAG {
		t.Errorf("verdict = %v, want FLAG", v.Verdict)
	}
	if v.Confidence != 1.0 { // clamped from 1.5
		t.Errorf("confidence = %v, want 1.0 (clamped)", v.Confidence)
	}
	if v.Source != "llm" {
		t.Errorf("source = %q, want llm", v.Source)
	}
	if _, ok := parseVerdict(`{"verdict":"NONSENSE"}`); ok {
		t.Error("parseVerdict should reject an unknown verdict name")
	}
	if _, ok := parseVerdict("garbage"); ok {
		t.Error("parseVerdict should fail (not panic) on non-JSON")
	}
}

// TestParseRank handles the object-array shape, the bare-float shape, and length mismatch.
func TestParseRank(t *testing.T) {
	scores, reasons := parseRank(`[{"score":0.8,"why":"strong"},{"score":0.2,"why":"weak"}]`, 2)
	if scores == nil || len(scores) != 2 || scores[0] != 0.8 || scores[1] != 0.2 {
		t.Errorf("parseRank objects scores = %v", scores)
	}
	if reasons[0] != "strong" || reasons[1] != "weak" {
		t.Errorf("parseRank objects reasons = %v", reasons)
	}
	// bare float array (back-compat) → reasons all empty.
	scores, reasons = parseRank(`[0.9, 0.1]`, 2)
	if scores == nil || scores[0] != 0.9 || scores[1] != 0.1 {
		t.Errorf("parseRank bare floats = %v", scores)
	}
	if reasons[0] != "" || reasons[1] != "" {
		t.Errorf("parseRank bare-float reasons should be empty, got %v", reasons)
	}
	// wrong-length object array → not used; falls back to float parse (also wrong shape) → nil.
	if s, _ := parseRank(`[{"score":0.5,"why":"x"}]`, 2); s != nil {
		t.Errorf("parseRank length mismatch should yield nil, got %v", s)
	}
	if s, _ := parseRank("garbage", 2); s != nil {
		t.Error("parseRank on garbage should yield nil (caller falls back)")
	}
}

// TestParseFloatsPad clamps, truncates, and pads to n.
func TestParseFloatsPad(t *testing.T) {
	got := parseFloats(`[2.0, -1.0]`, 4) // clamps to [0,1], pads with 0.5
	want := []float64{1.0, 0.0, 0.5, 0.5}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseFloats[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestJoinThoughts drops METACOG, takes the last n, and reports (none yet) when empty.
func TestJoinThoughts(t *testing.T) {
	ts := []types.Thought{
		{Text: "first", Source: types.GENERATED},
		{Text: "meta", Source: types.METACOG},
		{Text: "second", Source: types.INJECTED},
	}
	got := joinThoughts(ts, 6)
	want := "- first \n- second"
	if got != want {
		t.Errorf("joinThoughts = %q, want %q", got, want)
	}
	if got := joinThoughts([]types.Thought{{Text: "x", Source: types.METACOG}}, 6); got != "(none yet)" {
		t.Errorf("joinThoughts all-metacog = %q, want (none yet)", got)
	}
}

// TestFallbackNeverPanics: with no server reachable, no core method panics or hangs beyond the
// (short) probe timeout. The CONTROL roles are no longer this backend's job (the admission FLOOR +
// candidate RANK are Pattern-A math in internal/control); the ONE model-backed control touchpoint,
// the Filter ESCALATION (judge_admission, Pattern C), declines (ok=false) so the floor stands. The
// OUTPUT roles (generate / transform / summarize / respond) surface the gap by returning "" — they
// must NEVER fake the model with a test-double template (intelligence is the model's, the
// deterministic control is architecture only).
func TestFallbackNeverPanics(t *testing.T) {
	// An unreachable endpoint (RFC5737 TEST-NET) + a tiny timeout forces the degrade path fast.
	be := NewOpenAICompat(Options{BaseURL: "http://192.0.2.1:1/v1", Timeout: 50_000_000}) // 50ms
	var emitted []events.Event
	be.BindEmit(func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		emitted = append(emitted, ev)
		return ev
	})
	ctx := ctxFixture()
	dom := "safety"
	stance := "unsafe"
	c := types.Candidate{Text: "this is risky", Source: types.INJECTED, Domain: &dom,
		Relevance: 0.7, Stance: &stance}

	rng := cpyrand.New(0)
	// OUTPUT roles surface the gap ("") on an unreachable model — never a heuristic template.
	if got := be.Generate("goal", ctx, rng); got != "" {
		t.Errorf("Generate (output role) should return \"\" when unreachable, got %q", got)
	}
	if got := be.Transform(c, ctx); got != "" {
		t.Errorf("Transform (output role) should return \"\" when unreachable, got %q", got)
	}
	if got := be.Summarize(ctx); got != "" {
		t.Errorf("Summarize (output role) should return \"\" when unreachable, got %q", got)
	}
	if got := be.Respond("goal", ctx); got != "" {
		t.Errorf("Respond (output role) should return \"\" when unreachable, got %q", got)
	}
	// CONTROL roles are no longer this backend's job (M3): the admission FLOOR + candidate RANK are
	// Pattern-A math in internal/control, called directly by the seam — the backend has no ScoreAdmit/
	// Rank to fall back to. The ONE model-backed control touchpoint is the Filter ESCALATION
	// (JudgeAdmission, Pattern C): on an unreachable model it returns ok=false and the caller keeps the
	// floor verdict UNCHANGED (Rule 4: the floor stands, never a substituted stand-in).
	floor := control.ScoreAdmit(c, ctx, 0.5)
	v, refined := be.JudgeAdmission(c, ctx, floor)
	if refined {
		t.Errorf("JudgeAdmission should decline (refined=false) when unreachable")
	}
	if v.Verdict != floor.Verdict || v.Confidence != floor.Confidence || v.Source != floor.Source {
		t.Errorf("declined escalation must return the floor UNCHANGED, got %+v want %+v", v, floor)
	}
	// OperatorApply is a Pattern-B CONTENT role: on an unreachable model it surfaces the gap ("")
	// like Generate/Transform/Summarize/Respond — it never substitutes a template here. The caller
	// (fireReason) re-derives the "[role] intent" typed surface; the concretize-fusion path keeps
	// the raw un-fused candidate. The gap is surfaced in ONE place (the caller), not faked here.
	if op := be.OperatorApply("decompose", "resp", "intent", "dom", "goal", ctx); op != "" {
		t.Errorf("OperatorApply (content role) should return \"\" when unreachable, got %q", op)
	}
	if _, ok := be.SynthesizeProgram("goal", ctx, []string{"DECOMPOSE"}); ok {
		t.Error("SynthesizeProgram should defer (ok=false) when the model is unreachable")
	}

	// Optional capabilities decline (Python None) on an unreachable model.
	if choice, _ := be.Decide("goal", ctx, []string{"THINK", "STOP"}); choice != "" {
		t.Errorf("Decide should decline (empty) when unreachable, got %q", choice)
	}
	if _, _, ok := be.Intention("goal", ctx); ok {
		t.Error("Intention should decline (ok=false) when unreachable")
	}
	if _, ok := be.Specialist("safety", "you flag risks", ctx); ok {
		t.Error("PrimitiveSubAgent should decline (ok=false) when unreachable")
	}

	if be.Fallbacks == 0 {
		t.Error("expected fallbacks to be counted")
	}
	// The first connectivity failure emits exactly one llm.fallback (one-time degrade warning).
	var degradeCount int
	for _, ev := range emitted {
		if ev.Kind == events.LLMFallback {
			degradeCount++
		}
	}
	if degradeCount != 1 {
		t.Errorf("expected exactly 1 one-time degrade llm.fallback event, got %d", degradeCount)
	}
}

// TestResolveSubstrateTest: the explicit "test" substrate returns the test double; no network needed
// (the only non-error offline path).
func TestResolveSubstrateTest(t *testing.T) {
	be, err := ResolveSubstrate("test", SubstrateConfig{})
	if err != nil {
		t.Fatalf("ResolveSubstrate(test) errored: %v", err)
	}
	if be.AppraiserName() != "test" {
		t.Errorf("substrate appraiser = %q, want test", be.AppraiserName())
	}
}

// TestResolveSubstrateNoModelErrors: an unreachable local substrate returns BackendUnavailable —
// NEVER a silent offline fallback (the no-offline-product-path invariant).
func TestResolveSubstrateNoModelErrors(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("THOUGHT_LLM_API_KEY", "lm-studio")
	t.Setenv("THOUGHT_LLM_BASE_URL", "http://192.0.2.1:1/v1") // unreachable
	cfg := SubstrateConfig{BaseURL: "http://192.0.2.1:1/v1"}
	be, err := ResolveSubstrate("local", cfg)
	if err == nil {
		t.Fatalf("ResolveSubstrate(local, unreachable) should error, got backend %v", be)
	}
	if _, ok := err.(BackendUnavailable); !ok {
		t.Errorf("expected BackendUnavailable, got %T: %v", err, err)
	}
}

// TestMakeBackend covers the name dispatch (test / llm / unknown).
func TestMakeBackend(t *testing.T) {
	if be, err := MakeBackend("test", "", "", 0); err != nil || be.AppraiserName() != "test" {
		t.Errorf("MakeBackend(test) = %v, %v", be, err)
	}
	if be, err := MakeBackend("llm", "http://x/v1", "m", 0); err != nil || be.AppraiserName() != "llm" {
		t.Errorf("MakeBackend(llm) = %v, %v", be, err)
	}
	// --llm-max-tokens override threads through MakeBackend onto the backend.
	if be, err := MakeBackend("llm", "http://x/v1", "m", 2222); err != nil {
		t.Errorf("MakeBackend(llm, maxTokens) = %v, %v", be, err)
	} else if mt := be.(*OpenAICompatBackend).MaxTokens; mt != 2222 {
		t.Errorf("MakeBackend maxTokens override = %d, want 2222", mt)
	}
	if _, err := MakeBackend("bogus", "", "", 0); err == nil {
		t.Error("MakeBackend(bogus) should error")
	}
}

// TestTieredEmbedding: a TieredBackend promotes the primary's core methods and routes ONLY
// Summarize to the utility tier.
func TestTieredEmbedding(t *testing.T) {
	primary := NewOpenAICompat(Options{BaseURL: "http://192.0.2.1:1/v1", Model: "big", Timeout: 50_000_000})
	utility := NewOpenAICompat(Options{BaseURL: "http://192.0.2.1:1/v1", Model: "small", Timeout: 50_000_000})
	tb := NewTiered(primary, utility)
	// Promoted: AppraiserName comes from the embedded primary.
	if tb.AppraiserName() != "llm" {
		t.Errorf("tiered AppraiserName = %q, want llm", tb.AppraiserName())
	}
	if tb.DisplayName() != "llm:big (+util:small)" {
		t.Errorf("tiered DisplayName = %q", tb.DisplayName())
	}
	// Summarize routes to the utility backend (its Fallbacks bump, not the primary's).
	pf0, uf0 := primary.Fallbacks, utility.Fallbacks
	_ = tb.Summarize(ctxFixture())
	if utility.Fallbacks <= uf0 {
		t.Error("tiered Summarize did not route to the utility tier")
	}
	if primary.Fallbacks != pf0 {
		t.Error("tiered Summarize wrongly hit the primary tier")
	}
	// A reasoning role (Generate) routes to the primary tier.
	_ = tb.Generate("g", ctxFixture(), cpyrand.New(0))
	if primary.Fallbacks == pf0 {
		t.Error("tiered Generate did not route to the primary tier")
	}
	// Interface satisfaction.
	var _ backends.Backend = tb
	var _ backends.Decider = tb
}

// TestRingEviction: the call-log ring keeps the last `cap` records and `last()` is the newest.
func TestRingEviction(t *testing.T) {
	r := newRing(3)
	if _, ok := r.last(); ok {
		t.Error("empty ring last() should be ok=false")
	}
	for i := 0; i < 5; i++ {
		r.push(callRecord{Role: itoa(i)})
	}
	if r.Len() != 3 {
		t.Errorf("ring Len = %d, want 3 (capped)", r.Len())
	}
	if rec, _ := r.last(); rec.Role != "4" {
		t.Errorf("ring last role = %q, want 4 (newest)", rec.Role)
	}
}

// TestProbeBackendNeverPanics: doctor probes run against an unreachable LLM backend and report
// fell-back + call-log fields without panicking. After M3 the probe set distinguishes the
// model-backed roles (3 CONTENT + 1 Filter ESCALATION + 2 specialist — these fall back / carry a
// call log against an unreachable model) from the deterministic CONTROL roles (the admission FLOOR
// + candidate RANK — pure internal/control math: no model call, so no call log and no fall-back).
func TestProbeBackendNeverPanics(t *testing.T) {
	be := NewOpenAICompat(Options{BaseURL: "http://192.0.2.1:1/v1", Timeout: 50_000_000})
	results := ProbeBackend(be)
	// 3 content + 2 control (floor/rank) + 1 escalation + 2 specialist = 8.
	if len(results) != 8 {
		t.Fatalf("ProbeBackend returned %d results, want 8", len(results))
	}
	controlProbes, modelBacked := 0, 0
	for _, r := range results {
		isControl := strings.Contains(r.Subsystem, "control")
		if isControl {
			controlProbes++
			// CONTROL roles are deterministic: no model touched -> no call log, never fell back.
			if r.HasCallLog {
				t.Errorf("control probe %s must NOT carry a call log (no model call)", r.Subsystem)
			}
			if r.FellBack {
				t.Errorf("control probe %s must NOT fall back (it is pure math)", r.Subsystem)
			}
			continue
		}
		modelBacked++
		// Model-backed roles fall back against an unreachable model and carry the call-log fields.
		if !r.FellBack {
			t.Errorf("probe %s should have fallen back against an unreachable model", r.Subsystem)
		}
		if !r.HasCallLog {
			t.Errorf("probe %s should carry the LLM call-log fields", r.Subsystem)
		}
	}
	if controlProbes != 2 {
		t.Errorf("expected 2 deterministic control probes, got %d", controlProbes)
	}
	if modelBacked != 6 { // 3 content + 1 escalation + 2 specialist
		t.Errorf("expected 6 model-backed probes, got %d", modelBacked)
	}

	// The test double has no call log / no specialist / no escalation probes: 3 content + 2
	// control = 5 results, none carrying a call log (the control probes are deterministic).
	hres := ProbeBackend(backends.NewTest())
	if len(hres) != 5 {
		t.Errorf("test-double ProbeBackend returned %d, want 5", len(hres))
	}
	for _, r := range hres {
		if r.HasCallLog {
			t.Errorf("test-double probe %s should not carry a call log", r.Subsystem)
		}
	}
}
