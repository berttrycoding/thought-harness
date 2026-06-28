package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/legible"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// These tests lock the WF-E CC-1 legible-generation SHADOW instrument wired into the hidden seam:
//   - it is an INSTRUMENT (open/advisory) — it NEVER changes a verdict, a ranking, or the voiced text;
//   - OFF (the default, no gate / default-OFF toggle) it parses NOTHING and emits NO legible.* event;
//   - ON it parses the in-band tag at FILTER + GATE, emits legible.tag/parity/novel, and STRIPS the tag
//     before voicing.

// onGate builds an always-ON gate for seam.legible_generation (the instrument enabled). A nil gate, by
// contrast, is OFF (the opt-in default) — see legibleOn.
func onGate() *config.Gate {
	return config.NewGate("seam.legible_generation", func() bool { return true }, nil)
}

// newShadow builds a Shadow over the seed operator registry (the same contract the conscious is told it
// may emit) + the bus emit closure.
func newShadow(bus *events.Bus) *legible.Shadow {
	return legible.NewShadow(legible.NewTagRegistry(cognition.NewOperatorRegistry()), bus.Emit)
}

// TestShadowOffEmitsNothing: with the instrument OFF (nil gate — the default), a tagged candidate flows
// through Filter.Admit + Gate.Select WITHOUT a single legible.* event, and the verdict is the floor's.
func TestShadowOffEmitsNothing(t *testing.T) {
	bus, got := captureBus()
	f := NewFilter(controlMode, backends.NewTest(), bus.Emit)
	g := NewGate(bus.Emit)
	// SetLegible with a nil gate ⇒ OFF (opt-in default). The shadow is wired but never fires.
	f.SetLegible(newShadow(bus), nil)
	g.SetLegible(newShadow(bus), nil)

	tagged := types.Candidate{
		Text:   "⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ break it into three parts",
		Source: types.INJECTED, Domain: sp("planning"), Relevance: 0.9,
	}
	v := f.Admit(tagged, nil, 0.5)
	g.Select([]types.Candidate{tagged}, nil, map[string]float64{})

	for _, k := range []string{events.LegibleTag, events.LegibleParity, events.LegibleNovel} {
		if n := countKind(got, k); n != 0 {
			t.Errorf("OFF instrument emitted %d %q events, want 0 (byte-identical)", n, k)
		}
	}
	if v.Source == "llm" {
		t.Errorf("the verdict must be the floor's, never changed by the instrument; got %+v", v)
	}
}

// TestShadowFilterEmitsTagAndParity: ON, a known well-formed tag at FILTER emits legible.tag (known) +
// legible.parity, the parity carries the actual floor verdict, and the verdict itself is UNCHANGED.
func TestShadowFilterEmitsTagAndParity(t *testing.T) {
	bus, got := captureBus()
	f := NewFilter(controlMode, backends.NewTest(), bus.Emit)
	f.SetLegible(newShadow(bus), onGate())

	c := types.Candidate{
		Text:   "⟨op=decompose · domain=planning · conf=0.8 · act=no⟩ break it into three parts",
		Source: types.INJECTED, Domain: sp("planning"), Relevance: 0.9,
	}
	vWithShadow := f.Admit(c, nil, 0.5)

	if n := countKind(got, events.LegibleTag); n != 1 {
		t.Fatalf("want exactly 1 legible.tag at FILTER, got %d", n)
	}
	if n := countKind(got, events.LegibleParity); n != 1 {
		t.Fatalf("want exactly 1 legible.parity at FILTER, got %d", n)
	}
	for _, e := range *got {
		switch e.Kind {
		case events.LegibleTag:
			if e.Data["site"] != "filter" || e.Data["op"] != "decompose" || e.Data["known"] != true {
				t.Errorf("legible.tag data wrong: %+v", e.Data)
			}
			if e.Data["novel"] != false {
				t.Errorf("a known tag must not be novel: %+v", e.Data)
			}
		case events.LegibleParity:
			if e.Data["site"] != "filter" {
				t.Errorf("parity site=%v want filter", e.Data["site"])
			}
			// the actual side is the floor verdict the Filter really produced.
			if e.Data["actual"] != vWithShadow.Verdict.String() {
				t.Errorf("parity actual=%v want the real floor verdict %q", e.Data["actual"], vWithShadow.Verdict.String())
			}
			if _, ok := e.Data["agree"].(bool); !ok {
				t.Errorf("parity must carry an agree bool: %+v", e.Data)
			}
		}
	}

	// the instrument must NOT have changed the decision: re-run with no shadow and compare.
	bus2, _ := captureBus()
	f2 := NewFilter(controlMode, backends.NewTest(), bus2.Emit) // no SetLegible ⇒ off
	vNoShadow := f2.Admit(c, nil, 0.5)
	if vWithShadow.Verdict != vNoShadow.Verdict || vWithShadow.Confidence != vNoShadow.Confidence {
		t.Errorf("the SHADOW changed the verdict: with=%+v without=%+v", vWithShadow, vNoShadow)
	}
}

// TestShadowNovelEmitsNovel: a novel:<desc> tag at FILTER emits legible.tag (novel) AND legible.novel
// carrying the description (the ranked scaling gap signal).
func TestShadowNovelEmitsNovel(t *testing.T) {
	bus, got := captureBus()
	f := NewFilter(controlMode, backends.NewTest(), bus.Emit)
	f.SetLegible(newShadow(bus), onGate())

	c := types.Candidate{
		Text:   "⟨op=novel:weigh the ethical tradeoffs · domain=other · conf=0.4 · act=no⟩ this needs a new move",
		Source: types.INJECTED, Relevance: 0.5,
	}
	f.Admit(c, nil, 0.5)

	if n := countKind(got, events.LegibleNovel); n != 1 {
		t.Fatalf("want exactly 1 legible.novel for a novel: tag, got %d", n)
	}
	for _, e := range *got {
		if e.Kind == events.LegibleNovel {
			if e.Data["desc"] != "weigh the ethical tradeoffs" {
				t.Errorf("legible.novel desc=%v want the free-text description", e.Data["desc"])
			}
			if e.Data["site"] != "filter" {
				t.Errorf("legible.novel site=%v want filter", e.Data["site"])
			}
		}
		if e.Kind == events.LegibleTag && e.Data["novel"] != true {
			t.Errorf("a novel tag must set novel=true on legible.tag: %+v", e.Data)
		}
	}
}

// TestShadowGateEmitsParity: ON, the Gate parses the winner's tag and emits a legible.parity comparing
// the tag op to the actual winner's operator — and the ranking is unchanged by the instrument.
func TestShadowGateEmitsParity(t *testing.T) {
	bus, got := captureBus()
	g := NewGate(bus.Emit)
	g.SetLegible(newShadow(bus), onGate())

	cands := []types.Candidate{
		{Text: "⟨op=compare · domain=math · conf=0.9 · act=no⟩ a solid grounded answer with real substance",
			Source: types.INJECTED, Domain: sp("math"), Relevance: 0.9},
		{Text: "maybe this could possibly work", Source: types.INJECTED, Domain: sp("weak"), Relevance: 0.3},
	}
	winner, _, _, idx := g.Select(cands, nil, map[string]float64{})

	if idx != 0 || winner.Relevance != 0.9 {
		t.Fatalf("the instrument must not change the ranking; got idx=%d winner.rel=%v", idx, winner.Relevance)
	}
	if n := countKind(got, events.LegibleParity); n != 1 {
		t.Fatalf("want exactly 1 legible.parity at GATE (the winner had a tag), got %d", n)
	}
	for _, e := range *got {
		if e.Kind == events.LegibleParity {
			if e.Data["site"] != "gate" || e.Data["shadow"] != "compare" {
				t.Errorf("gate parity data wrong: %+v", e.Data)
			}
		}
	}
}

// TestShadowStripsTagBeforeVoicing: ON, the seam relays a tagged winner with the tag STRIPPED — the
// voiced thought + the seam.transform `raw`/`voiced` fields carry no envelope glyph.
func TestShadowStripsTagBeforeVoicing(t *testing.T) {
	bus, got := captureBus()
	f := NewFilter(controlMode, backends.NewTest(), bus.Emit)
	g := NewGate(bus.Emit)
	h := NewHiddenSeam(g, f, backends.NewTest(), bus.Emit)
	// SetGates with all-on gates (the inner organs run), then SetLegible ON.
	h.SetGates(nil, nil, nil) // nil ⇒ always-on stages (pre-config behaviour)
	h.SetLegible(newShadow(bus), onGate())

	tagged := types.Candidate{
		Text:   "⟨op=decompose · domain=planning · conf=0.9 · act=no⟩ break the request into three sub-problems",
		Source: types.INJECTED, Domain: sp("planning"), Relevance: 0.95,
	}
	res := h.Relay([]types.Candidate{tagged}, nil, map[string]float64{}, 0.5)

	if res.Thought == nil {
		t.Fatal("a high-relevance tagged candidate should be admitted + voiced")
	}
	if containsEnvelope(res.Thought.Text) {
		t.Errorf("the voiced thought still carries the tag envelope: %q", res.Thought.Text)
	}
	// the seam.transform event's raw field must also be the stripped prose (the tag is never shown).
	for _, e := range *got {
		if e.Kind == events.Transform {
			if raw, _ := e.Data["raw"].(string); containsEnvelope(raw) {
				t.Errorf("seam.transform raw still carries the envelope: %q", raw)
			}
		}
	}
}

// TestShadowOffDoesNotStrip: OFF (the default), the winner's tag is voiced verbatim (no strip) — proving
// the strip is gated and the default path is byte-identical (the tag-bearing case is only an instrument
// concern; with the instrument off, the seam behaves exactly as before).
func TestShadowOffDoesNotStrip(t *testing.T) {
	bus, _ := captureBus()
	f := NewFilter(controlMode, backends.NewTest(), bus.Emit)
	g := NewGate(bus.Emit)
	h := NewHiddenSeam(g, f, backends.NewTest(), bus.Emit)
	h.SetGates(nil, nil, nil)
	// no SetLegible (or a nil gate) ⇒ OFF. The transform may re-voice; force raw relay to inspect the
	// exact winner text by disabling transform via an OFF gate.
	transformOff := config.NewGate("seam.transform", func() bool { return false }, bus.Emit)
	h.SetGates(nil, nil, transformOff)

	tagged := types.Candidate{
		Text:   "⟨op=decompose · domain=planning · conf=0.9 · act=no⟩ break the request into three sub-problems",
		Source: types.INJECTED, Domain: sp("planning"), Relevance: 0.95,
	}
	res := h.Relay([]types.Candidate{tagged}, nil, map[string]float64{}, 0.5)
	if res.Thought == nil {
		t.Fatal("expected a voiced thought")
	}
	// raw relay + instrument OFF ⇒ the tag is NOT stripped (voiced verbatim).
	if !containsEnvelope(res.Thought.Text) {
		t.Errorf("with the instrument OFF the tag must NOT be stripped (byte-identical); got %q", res.Thought.Text)
	}
}

// containsEnvelope reports whether s holds the legibility envelope open glyph (a quick leak check).
func containsEnvelope(s string) bool {
	for _, r := range s {
		if r == '⟨' || r == '⟩' {
			return true
		}
	}
	return false
}
