package llm

import (
	"regexp"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// buildRespondPrompt reconstructs the EXACT (system, user) pair Respond sends, with whatever
// fragments are set on the backend — the same construction Respond does, without a network call. It
// mirrors openai.go Respond's prompt assembly so a test can assert byte-equality without a model.
func buildRespondPrompt(b *OpenAICompatBackend, goal string, ctx []types.Thought) (system, user string) {
	system, user = PromptRespond(goal, ctx)
	if b.personaFragment != "" {
		system += "\n" + b.personaFragment
	}
	if b.groundCompleteFragment != "" {
		system += "\n\n" + b.groundCompleteFragment
	}
	return system, user
}

// TestGroundCompleteOffByteIdentical: with NO ground-complete fragment set (the default ⇒ flag OFF),
// the RESPOND system prompt is BYTE-IDENTICAL to the bare PromptRespond output. This is the
// off-byte-identical proof at the prompt seam.
func TestGroundCompleteOffByteIdentical(t *testing.T) {
	ctx := []types.Thought{{Text: "active_profile is prod"}, {Text: "RetryBudget = 3 in the base block"}}
	goal := "investigate config and report the RetryBudget"

	bareSystem, bareUser := PromptRespond(goal, ctx)

	b := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	// No SetGroundCompleteFragment call ⇒ groundCompleteFragment == "" (the flag-OFF state).
	system, user := buildRespondPrompt(b, goal, ctx)
	if system != bareSystem {
		t.Fatalf("flag OFF must leave the RESPOND system prompt byte-identical.\n bare: %q\n got:  %q", bareSystem, system)
	}
	if user != bareUser {
		t.Fatalf("flag OFF must leave the RESPOND user prompt byte-identical.\n bare: %q\n got:  %q", bareUser, user)
	}
	// Explicitly clearing with "" is also byte-identical (the engine clears the fragment when OFF).
	b.SetGroundCompleteFragment("")
	system2, _ := buildRespondPrompt(b, goal, ctx)
	if system2 != bareSystem {
		t.Fatalf("SetGroundCompleteFragment(\"\") must keep the prompt byte-identical, got %q", system2)
	}
}

// TestGroundCompleteOnDirectivePresent: with the directive set (flag ON), the built RESPOND system
// prompt CONTAINS the directive text and is the bare prompt + the directive appended (nothing else
// changes).
func TestGroundCompleteOnDirectivePresent(t *testing.T) {
	ctx := []types.Thought{{Text: "thinking"}}
	goal := "report the value"
	bareSystem, _ := PromptRespond(goal, ctx)

	b := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	b.SetGroundCompleteFragment(groundCompleteTestDirective)
	system, _ := buildRespondPrompt(b, goal, ctx)
	if !strings.Contains(system, "value actually in force") {
		t.Fatalf("flag ON must put the directive into the RESPOND system prompt; got %q", system)
	}
	if system != bareSystem+"\n\n"+groundCompleteTestDirective {
		t.Fatalf("the ON prompt must be exactly the bare prompt + the appended directive, got %q", system)
	}
}

// groundCompleteTestDirective is a COPY of the engine's directive text used to exercise the append at
// the llm layer (the llm pkg must not import engine). The engine test asserts the engine's copy
// contains the same general/decline-safe properties; this test asserts the APPEND mechanics. The
// content properties (no hardcoded answer, no enumerated keyword list, decline-safe) are asserted
// against THIS string too so any drift in a copied directive is caught at this seam.
const groundCompleteTestDirective = "Before you answer, read the material COMPLETELY for the value the " +
	"question needs, not just the first thing that matches the name. First, if a later statement in " +
	"the material corrects, replaces, or overrides an earlier value, use the value actually in force — " +
	"the later one — not the first one you found. Second, if the answer is a base value that the " +
	"material states should be adjusted or converted before it is reported, apply that stated " +
	"adjustment or conversion to the base value and report the result. But never invent a value to " +
	"satisfy the question: if a value the question needs is not actually present in the material — it " +
	"is only referenced via an external file, package, or dashboard you cannot read here — DECLINE and " +
	"say it is not determinable from the material rather than guessing a number."

// TestGroundCompleteDirectiveIsGeneralAndDeclineSafe: the directive (a) carries NO hardcoded
// answer/number, (b) enumerates NO specific trigger keywords (deprecated/erratum/flag/multiplier/...),
// and (c) contains the decline-safety clause. These are the red-team's load-bearing conditions.
func TestGroundCompleteDirectiveIsGeneralAndDeclineSafe(t *testing.T) {
	d := groundCompleteTestDirective
	lower := strings.ToLower(d)

	// (a) NO hardcoded answer: the directive must contain NO digit at all (no number = no leaked
	// answer/value). It describes a reading behaviour, never a value. (The "(1)"/"(2)" list markers are
	// written as words to keep this invariant — verify there is genuinely no digit.)
	if regexp.MustCompile(`[0-9]`).MatchString(d) {
		t.Fatalf("directive must carry NO digit (no hardcoded answer/number/enumerated index); got %q", d)
	}

	// (b) NO enumerated trigger keywords — the overfitting trap the red-team flagged. The directive must
	// describe a GENERAL behaviour ("corrects, replaces, or overrides", "adjusted or converted"), never a
	// keyword lookup table. Assert none of the specific trigger words appear.
	bannedKeywords := []string{
		"deprecated", "erratum", "errata", "flag", "multiplier", "multiply by",
		"superseded", "overridden", "patch", "hotfix", "bugfix", "changelog",
		"footnote", "addendum", "revision", "v2", "version 2", "tax", "discount",
	}
	for _, kw := range bannedKeywords {
		if strings.Contains(lower, kw) {
			t.Fatalf("directive must NOT enumerate the specific trigger keyword %q (overfits); got %q", kw, d)
		}
	}

	// (c) the general language IS present (corrects/replaces/overrides + adjustment/conversion).
	for _, must := range []string{"corrects", "replaces", "overrides", "value actually in force",
		"adjusted or converted", "apply that stated adjustment or conversion"} {
		if !strings.Contains(lower, strings.ToLower(must)) {
			t.Fatalf("directive must contain the general phrase %q; got %q", must, d)
		}
	}

	// (d) DECLINE-SAFETY clause is present — the anti-confabulation protection. It must explicitly tell
	// the model to DECLINE / not invent a value that is only referenced via an unreadable external
	// pointer.
	for _, must := range []string{"never invent a value", "decline",
		"external file, package, or dashboard you cannot read", "not determinable from the material"} {
		if !strings.Contains(lower, strings.ToLower(must)) {
			t.Fatalf("directive must contain the decline-safety phrase %q; got %q", must, d)
		}
	}
}

// TestGroundCompleteReachesBothBackends: the SAME OpenAICompatBackend.Respond path that the local
// --backend llm uses is ALSO the path the --backend claude bridge uses (claudecode.go is
// OpenAICompatBackend with only the transport swapped). So a fragment set on the bridge's backend is
// appended to the RESPOND prompt the bridge sends. This test proves the bridge backend (constructed via
// NewClaudeCode → newClaudeExecBackend) is an *OpenAICompatBackend that carries the fragment.
func TestGroundCompleteReachesBothBackends(t *testing.T) {
	// The bridge: NewClaudeCode returns a TieredBackend wrapping claude-exec OpenAICompatBackends. The
	// PRIMARY tier is what voices answers (Respond). Prove it accepts + carries the fragment via the
	// GroundCompletePrompter interface — exactly as the engine pushes it.
	bridge := NewClaudeCode(ClaudeCodeOptions{Model: "sonnet", UtilityModel: "none"})
	gp, ok := bridge.(backends.GroundCompletePrompter)
	if !ok {
		t.Fatalf("the claude bridge MUST implement GroundCompletePrompter so the directive reaches the bench substrate")
	}
	gp.SetGroundCompleteFragment(groundCompleteTestDirective)

	// Unwrap to the concrete primary backend and assert the fragment is appended to its RESPOND prompt.
	primary := unwrapPrimaryOpenAICompat(t, bridge)
	bareSystem, _ := PromptRespond("g", []types.Thought{{Text: "t"}})
	system, _ := buildRespondPrompt(primary, "g", []types.Thought{{Text: "t"}})
	if system != bareSystem+"\n\n"+groundCompleteTestDirective {
		t.Fatalf("the bridge's RESPOND prompt must carry the directive (same Respond path); got %q", system)
	}

	// And a local OpenAICompatBackend (the --backend llm path) carries it identically.
	local := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	if _, ok := backends.Backend(local).(backends.GroundCompletePrompter); !ok {
		t.Fatalf("the local OpenAICompatBackend must implement GroundCompletePrompter")
	}
}

// unwrapPrimaryOpenAICompat digs the PRIMARY *OpenAICompatBackend out of whatever NewClaudeCode
// returned (a bare backend or a TieredBackend). It sets the fragment via the public interface on the
// returned wrapper, then reads it off the concrete primary — proving the wrapper forwards it.
func unwrapPrimaryOpenAICompat(t *testing.T, b backends.Backend) *OpenAICompatBackend {
	t.Helper()
	switch v := b.(type) {
	case *OpenAICompatBackend:
		return v
	case *TieredBackend:
		return v.Primary
	default:
		t.Fatalf("unexpected bridge backend type %T", b)
		return nil
	}
}

// TestGroundCompleteRespondSurfacesGapOnFailure: confirm the never-fabricate discipline is intact —
// with the directive ON and the model UNREACHABLE, Respond still surfaces the gap (returns "") rather
// than substituting any deterministic text. The directive shapes a real model's reading; it never
// manufactures an answer.
func TestGroundCompleteRespondSurfacesGapOnFailure(t *testing.T) {
	// An unreachable endpoint ⇒ every chat() degrades ⇒ Respond surfaces "".
	b := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	b.SetGroundCompleteFragment(groundCompleteTestDirective)
	out := b.Respond("report the value of MaxRetries", []types.Thought{{Text: "thinking"}})
	if out != "" {
		t.Fatalf("on model failure Respond must surface the gap (\"\"), never a substitute; got %q", out)
	}
}
