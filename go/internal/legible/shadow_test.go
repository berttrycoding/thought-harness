package legible

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// captureShadow builds a Shadow over the seed contract + a slice-collecting emit closure.
func captureShadow(t *testing.T) (*Shadow, *[]events.Event) {
	t.Helper()
	var got []events.Event
	emit := func(kind, summary string, data map[string]any) events.Event {
		e := events.Event{Kind: kind, Summary: summary, Data: data}
		got = append(got, e)
		return e
	}
	return NewShadow(NewTagRegistry(cognition.NewOperatorRegistry()), emit), &got
}

func count(got *[]events.Event, kind string) int {
	n := 0
	for _, e := range *got {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// TestShadowVerdictBands: the shadow turns conf into the SAME three-band admission verdict the control
// floor speaks (ADMIT ≥ 0.6, FLAG ≥ 0.32, else REJECT) — so a parity comparison is apples-to-apples.
func TestShadowVerdictBands(t *testing.T) {
	cases := []struct {
		conf float64
		want string
	}{
		{0.9, VerdictAdmit},
		{0.6, VerdictAdmit},
		{0.5, VerdictFlag},
		{0.32, VerdictFlag},
		{0.2, VerdictReject},
		{0.0, VerdictReject},
	}
	for _, c := range cases {
		if got := shadowVerdict(Tag{Conf: c.conf}); got != c.want {
			t.Errorf("shadowVerdict(conf=%v) = %q, want %q", c.conf, got, c.want)
		}
	}
}

// TestStripRemovesEnvelope: Strip removes a well-formed tag and returns the clean prose; on a missing /
// malformed tag it returns the text unchanged (the advisory net). A nil *Shadow returns text unchanged.
func TestStripRemovesEnvelope(t *testing.T) {
	s, _ := captureShadow(t)
	if got := s.Strip("⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ break it up"); got != "break it up" {
		t.Errorf("Strip kept the tag: %q", got)
	}
	const plain = "no tag here, just prose"
	if got := s.Strip(plain); got != plain {
		t.Errorf("Strip mangled untagged text: %q", got)
	}
	var nilShadow *Shadow
	if got := nilShadow.Strip(plain); got != plain {
		t.Errorf("nil Shadow.Strip must return text unchanged, got %q", got)
	}
}

// TestShadowFilterParityAgreement: a known tag whose self-conf lands in the same band as the actual
// verdict emits agree=true; a clashing one emits agree=false. The instrument records, never decides.
func TestShadowFilterParityAgreement(t *testing.T) {
	s, got := captureShadow(t)
	// conf 0.8 ⇒ shadow predicts ADMIT; actual ADMIT ⇒ agree.
	s.ShadowFilter("⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ p", VerdictAdmit)
	// conf 0.1 ⇒ shadow predicts REJECT; actual ADMIT ⇒ disagree.
	s.ShadowFilter("⟨op=compare · domain=math · conf=0.1 · act=no⟩ q", VerdictAdmit)

	if n := count(got, events.LegibleTag); n != 2 {
		t.Fatalf("want 2 legible.tag, got %d", n)
	}
	parity := []bool{}
	for _, e := range *got {
		if e.Kind == events.LegibleParity {
			parity = append(parity, e.Data["agree"].(bool))
		}
	}
	if len(parity) != 2 || parity[0] != true || parity[1] != false {
		t.Fatalf("parity agreements = %v, want [true false]", parity)
	}
}

// TestShadowFilterNoTagIsSilent: a candidate with no well-formed tag is NOT a parity observation (it fell
// back to the LLM path) — the instrument records nothing, so the fallback isn't scored as a disagreement.
func TestShadowFilterNoTagIsSilent(t *testing.T) {
	s, got := captureShadow(t)
	s.ShadowFilter("just plain prose, no tag", VerdictAdmit)
	if len(*got) != 0 {
		t.Errorf("an untagged candidate must emit nothing, got %d events", len(*got))
	}
}

// TestShadowGateOpParity: the GATE shadow agrees only when the tag's KNOWN op equals the actual winner's
// operator; an unknown op is a coverage disagreement (the gap the instrument surfaces).
func TestShadowGateOpParity(t *testing.T) {
	s, got := captureShadow(t)
	// known op == actual ⇒ agree.
	s.ShadowGate("⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ p", "decompose", "planning")
	// unknown op ⇒ Known=false ⇒ disagree-by-coverage.
	s.ShadowGate("⟨op=frobnicate · domain=other · conf=0.5 · act=no⟩ q", "frobnicate", "other")

	parity := []bool{}
	for _, e := range *got {
		if e.Kind == events.LegibleParity {
			parity = append(parity, e.Data["agree"].(bool))
		}
	}
	if len(parity) != 2 || parity[0] != true || parity[1] != false {
		t.Fatalf("gate parity = %v, want [true false] (unknown op is a coverage gap)", parity)
	}
}

// TestNilShadowIsNoOp: every method on a nil *Shadow is a safe no-op (so the seam can hold one
// unconditionally and rely on the toggle to keep it silent).
func TestNilShadowIsNoOp(t *testing.T) {
	var s *Shadow
	s.ShadowFilter("⟨op=x · domain=y · conf=0.5 · act=no⟩ p", VerdictAdmit) // must not panic
	s.ShadowGate("⟨op=x · domain=y · conf=0.5 · act=no⟩ p", "x", "y")       // must not panic
	if s.Registry() != nil {
		t.Error("nil Shadow.Registry() must be nil")
	}
}
