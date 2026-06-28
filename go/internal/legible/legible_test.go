package legible

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// newReg builds a TagRegistry over the seed operator registry — the same vocabulary the conscious is
// told it may emit. Shared by the parse tests so Known is checked against the real op set.
func newReg(t *testing.T) *TagRegistry {
	t.Helper()
	return NewTagRegistry(cognition.NewOperatorRegistry())
}

// TestParseWellFormed: a tag with a KNOWN op parses every field, sets Known=true, and strips the prose.
func TestParseWellFormed(t *testing.T) {
	r := newReg(t)
	const prose = "Break the request into the three sub-problems."
	in := "⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ " + prose

	tag, rest, ok := r.ParseTag(in)
	if !ok {
		t.Fatalf("ParseTag ok=false for a well-formed envelope")
	}
	if tag.Op != "decompose" {
		t.Errorf("Op = %q, want decompose", tag.Op)
	}
	if tag.Domain != "planning" {
		t.Errorf("Domain = %q, want planning", tag.Domain)
	}
	if tag.Conf != 0.8 {
		t.Errorf("Conf = %v, want 0.8", tag.Conf)
	}
	if tag.Act {
		t.Errorf("Act = true, want false (act=no)")
	}
	if tag.IsNovel {
		t.Errorf("IsNovel = true, want false")
	}
	if !tag.Known {
		t.Errorf("Known = false, want true (decompose is a seed operator)")
	}
	if rest != prose {
		t.Errorf("prose not stripped clean: got %q, want %q", rest, prose)
	}
}

// TestParseActYes: act=yes parses to Act=true (the watched-seam request bit), case-insensitively.
func TestParseActYes(t *testing.T) {
	r := newReg(t)
	tag, rest, ok := r.ParseTag("⟨op=generate · domain=code · conf=0.95 · act=YES⟩ write the function")
	if !ok {
		t.Fatalf("ParseTag ok=false")
	}
	if !tag.Act {
		t.Errorf("Act = false, want true (act=YES)")
	}
	if rest != "write the function" {
		t.Errorf("prose = %q", rest)
	}
}

// TestParseNovel: op=novel:<desc> sets IsNovel + NovelDesc, Known=false (fallback + mint signal).
func TestParseNovel(t *testing.T) {
	r := newReg(t)
	in := "⟨op=novel:weigh the ethical tradeoffs · domain=other · conf=0.4 · act=no⟩ This needs a new kind of move."

	tag, rest, ok := r.ParseTag(in)
	if !ok {
		t.Fatalf("ParseTag ok=false for a well-formed novel envelope")
	}
	if !tag.IsNovel {
		t.Errorf("IsNovel = false, want true for op=novel:")
	}
	if tag.NovelDesc != "weigh the ethical tradeoffs" {
		t.Errorf("NovelDesc = %q, want %q", tag.NovelDesc, "weigh the ethical tradeoffs")
	}
	if tag.Known {
		t.Errorf("Known = true, want false (novel is not in the vocabulary)")
	}
	if tag.Domain != "other" {
		t.Errorf("Domain = %q, want other", tag.Domain)
	}
	if rest != "This needs a new kind of move." {
		t.Errorf("prose not stripped clean: %q", rest)
	}
}

// TestParseUnknownOp: a well-formed envelope whose op is NOT in the registry parses ok=true but
// Known=false (advisory: the route falls back). ok (well-formed) is independent of Known (in-vocabulary).
func TestParseUnknownOp(t *testing.T) {
	r := newReg(t)
	if r.IsKnownOp("frobnicate") {
		t.Fatalf("test precondition: 'frobnicate' must not be a seed operator")
	}
	tag, rest, ok := r.ParseTag("⟨op=frobnicate · domain=general · conf=0.5 · act=no⟩ do the thing")
	if !ok {
		t.Fatalf("ParseTag ok=false, want true (the envelope is well-formed)")
	}
	if tag.Op != "frobnicate" {
		t.Errorf("Op = %q, want frobnicate", tag.Op)
	}
	if tag.Known {
		t.Errorf("Known = true, want false for an unregistered op")
	}
	if tag.IsNovel {
		t.Errorf("IsNovel = true, want false (unknown op is not the novel: escape)")
	}
	if rest != "do the thing" {
		t.Errorf("prose = %q", rest)
	}
}

// TestParseMalformedOrMissing: a missing or malformed envelope returns ok=false and the ORIGINAL text
// unchanged (the caller falls back).
func TestParseMalformedOrMissing(t *testing.T) {
	r := newReg(t)
	cases := []struct {
		name string
		in   string
	}{
		{"no envelope at all", "Just plain free prose, no tag in sight."},
		{"missing close bracket", "⟨op=decompose · domain=planning · conf=0.8 · act=no plain prose"},
		{"missing a field (no act)", "⟨op=decompose · domain=planning · conf=0.8⟩ prose"},
		{"wrong separator", "⟨op=decompose, domain=planning, conf=0.8, act=no⟩ prose"},
		{"non-numeric conf", "⟨op=decompose · domain=planning · conf=high · act=no⟩ prose"},
		{"empty string", ""},
		{"only an open bracket", "⟨ this is not a tag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tag, rest, ok := r.ParseTag(c.in)
			if ok {
				t.Errorf("ParseTag ok=true for malformed/missing input %q (tag=%+v)", c.in, tag)
			}
			if rest != c.in {
				t.Errorf("on failure the text must pass through unchanged: got %q, want %q", rest, c.in)
			}
			if (tag != Tag{}) {
				t.Errorf("on failure the Tag must be zero, got %+v", tag)
			}
		})
	}
}

// TestProseStrippedClean: the envelope (frame + all fields) is fully removed and the single contract space
// after ⟩ is trimmed, with no leftover bracket/separator glyphs in the prose.
func TestProseStrippedClean(t *testing.T) {
	r := newReg(t)
	_, rest, ok := r.ParseTag("⟨op=compare · domain=math · conf=0.7 · act=no⟩   leading spaces then prose")
	if !ok {
		t.Fatalf("ParseTag ok=false")
	}
	if rest != "leading spaces then prose" {
		t.Errorf("prose = %q, want clean with leading whitespace trimmed", rest)
	}
	for _, glyph := range []string{envOpen, envClose, "op=", "domain=", "conf=", "act="} {
		if strings.Contains(rest, glyph) {
			t.Errorf("prose still contains envelope glyph %q: %q", glyph, rest)
		}
	}
}

// TestConfClamped: a conf outside [0,1] is clamped (it is a prior, not truth).
func TestConfClamped(t *testing.T) {
	r := newReg(t)
	hi, _, ok := r.ParseTag("⟨op=generate · domain=code · conf=1.5 · act=no⟩ x")
	if !ok || hi.Conf != 1.0 {
		t.Errorf("conf=1.5 -> %v (ok=%v), want clamped to 1.0", hi.Conf, ok)
	}
}

// TestContractIsOneSourceOfTruth: the prompt fragment and the parser's Known verdict agree on the
// vocabulary — every op the prompt enumerates is IsKnownOp-true, and every known domain appears in the
// prompt. This is the "one source of truth, two derived views cannot drift" invariant (05 §4b).
func TestContractIsOneSourceOfTruth(t *testing.T) {
	r := newReg(t)
	frag := r.PromptFragment()

	if len(r.KnownOps()) == 0 {
		t.Fatalf("no known ops")
	}
	for _, op := range r.KnownOps() {
		if !r.IsKnownOp(op) {
			t.Errorf("op %q enumerated by the contract is not IsKnownOp", op)
		}
		if !strings.Contains(frag, op) {
			t.Errorf("prompt fragment omits known op %q (prompt + parser would drift)", op)
		}
	}
	for _, d := range r.KnownDomains() {
		if !strings.Contains(frag, d) {
			t.Errorf("prompt fragment omits known domain %q", d)
		}
	}
	// The open escape must always be offered (never hard-close the vocabulary, 05 §2).
	if !strings.Contains(frag, novelPrefix) {
		t.Errorf("prompt fragment omits the novel: escape hatch")
	}
	// A novel tag the prompt invites must parse back through the same contract.
	if _, _, ok := r.ParseTag("⟨op=novel:some new move · domain=other · conf=0.3 · act=no⟩ p"); !ok {
		t.Errorf("a novel: tag the prompt invites does not parse through the contract")
	}
}

// TestKnownOpsSourcedFromRegistry: a freshly-minted operator becomes Known only after the contract is
// REBUILT (the versioned-contract / snapshot discipline) — proving the op vocabulary is sourced from the
// operator registry, not a hardcoded list.
func TestKnownOpsSourcedFromRegistry(t *testing.T) {
	ops := cognition.NewOperatorRegistry()
	r0 := NewTagRegistry(ops)
	if r0.IsKnownOp("steelman") {
		t.Fatalf("precondition: 'steelman' not a seed op")
	}
	if _, ok := ops.Mint("steelman", "relational", "argue the strongest version of the opposing case"); !ok {
		t.Fatalf("mint failed")
	}
	// Old snapshot still excludes it (stable contract within its version).
	if r0.IsKnownOp("steelman") {
		t.Errorf("old snapshot should not know a post-snapshot mint")
	}
	// Rebuild -> both views (prompt + parser) pick it up together.
	r1 := NewTagRegistry(ops)
	if !r1.IsKnownOp("steelman") {
		t.Errorf("rebuilt contract does not know the minted op")
	}
	if !strings.Contains(r1.PromptFragment(), "steelman") {
		t.Errorf("rebuilt prompt fragment omits the minted op")
	}
	tag, _, ok := r1.ParseTag("⟨op=steelman · domain=other · conf=0.6 · act=no⟩ argue the other side")
	if !ok || !tag.Known {
		t.Errorf("minted op not Known after rebuild: ok=%v known=%v", ok, tag.Known)
	}
}
