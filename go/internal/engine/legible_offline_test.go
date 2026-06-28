package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/legible"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// taggedGenBackend is the OFFLINE plumbing stub for the WF-E CC-1 legible-generation instrument: it is
// the deterministic test double in every respect EXCEPT Generate, which returns a crafted, well-formed
// legibility envelope + clean prose — exactly the in-band tag a real model would emit when
// seam.legible_generation is ON. It also implements LegiblePrompter (recording the fragment the engine
// pushes) so the engine treats it as the tag-emitting conscious faculty. No real model is involved: this
// proves the PLUMBING (parse -> strip -> shadow parity -> novel histogram), not the model's competence.
type taggedGenBackend struct {
	*backends.TestBackend
	tagged       string // the exact crafted Generate return (envelope + prose)
	lastFragment string // the legible prompt fragment the engine set (proves the prompt side fired)
}

// Generate returns the crafted tagged string verbatim, so the engine's generate() path parses the tag,
// shadow-compares at FILTER, and strips the envelope before voicing — all on the offline double.
func (b *taggedGenBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	return b.tagged
}

func (b *taggedGenBackend) SetLegibleFragment(fragment string) { b.lastFragment = fragment }

var _ backends.LegiblePrompter = (*taggedGenBackend)(nil)

// crafted is the tagged Generate return used by the offline tests: a well-formed envelope with a NOVEL op
// (so the novel histogram captures it) + clean prose. The prose deliberately contains NO angle brackets,
// so any envelope leak into the voiced text is unambiguous.
const craftedEnvelope = "⟨op=novel:triangulate-from-three-sources · domain=other · conf=0.8 · act=no⟩ "
const craftedProse = "I will cross-check the claim against three independent sources before trusting it."
const crafted = craftedEnvelope + craftedProse

// newTaggedEngine builds a reactive engine on the tagged-generate stub, with the legible toggle set to on.
// It returns the engine, the stub, and a slice that collects EVERY event (so the test reads the legible.*
// stream + the voiced Append/Generate text off one capture).
func newTaggedEngine(t *testing.T, legibleOn bool) (*Engine, *taggedGenBackend, *[]events.Event) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.New() // AllOn ⇒ legible OFF by default
	feat.Seam.LegibleGeneration = legibleOn
	feat.Validate()
	cfg.Features = feat

	be := &taggedGenBackend{TestBackend: backends.NewTest(), tagged: crafted}
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) { got = append(got, ev) })
	return e, be, &got
}

// runToIdle drives the engine to idle (bounded), so the crafted conscious thought is generated, screened,
// stripped, and voiced.
func runToIdle(e *Engine) {
	for i := 0; i < 40; i++ {
		res := e.Step()
		if res.Idle && !e.PortPending() {
			break
		}
	}
}

// eventsByKind collects every event of one kind from the capture.
func eventsByKind(got *[]events.Event, kind string) []events.Event {
	var out []events.Event
	for _, e := range *got {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// TestLegibleOfflinePlumbingOn is the WF-E CC-1 part-2 OFFLINE plumbing test (toggle ON, no real model):
// drive one engine episode on the tagged-generate stub and assert the whole legible pipeline fires —
//   - the tag is PARSED (a legible.tag event with the crafted op),
//   - the voiced thought is the STRIPPED prose (no envelope frame leaks into Append/Generate),
//   - the shadow PARITY event fired at the FILTER site,
//   - the NOVEL histogram captures the crafted novel: tag.
func TestLegibleOfflinePlumbingOn(t *testing.T) {
	e, be, got := newTaggedEngine(t, true)
	e.SubmitDefault("how should I verify an uncertain claim?")
	runToIdle(e)

	// The engine pushed the registry-derived contract fragment onto the stub's Generate prompt (prompt
	// side of the instrument) — proof the ON path drove the conscious to tag.
	if be.lastFragment == "" || !strings.Contains(be.lastFragment, "op=") {
		t.Fatalf("legible ON: the engine must set the contract fragment on Generate, got %q", be.lastFragment)
	}

	// (1) the tag is PARSED — a legible.tag event carrying the crafted op + the novel/known classification.
	tags := eventsByKind(got, events.LegibleTag)
	if len(tags) == 0 {
		t.Fatal("legible ON: expected at least one legible.tag (the parsed in-band tag)")
	}
	var sawCraftedOp bool
	for _, ev := range tags {
		if op, _ := ev.Data["op"].(string); strings.HasPrefix(op, "novel:triangulate") {
			sawCraftedOp = true
			if novel, _ := ev.Data["novel"].(bool); !novel {
				t.Errorf("the crafted novel op must classify novel=true, got %v", ev.Data["novel"])
			}
		}
	}
	if !sawCraftedOp {
		t.Errorf("legible.tag events did not carry the crafted op; got %d tag events", len(tags))
	}

	// (2) the voiced thought is the STRIPPED prose — no envelope frame in any Generate/Append text.
	assertNoEnvelopeLeak(t, got)
	if !voicedTextSeen(got, craftedProse) {
		t.Errorf("the stripped prose was never voiced into the stream (Generate/Append): looked for %q", craftedProse)
	}

	// (3) the shadow PARITY event fired at the FILTER site (shadow route vs the actual floor verdict).
	parities := eventsByKind(got, events.LegibleParity)
	if !hasParitySite(parities, "filter") {
		t.Errorf("legible ON: expected a filter-site legible.parity observation, got %d parity events", len(parities))
	}

	// (4) the NOVEL histogram (the rollup over the captured stream) captures the crafted novel: tag.
	roll := legible.RollupOf(*got)
	hist := roll.NovelHistogram()
	var foundNovel bool
	for _, en := range hist {
		if strings.HasPrefix(en.Desc, "triangulate-from-three-sources") {
			foundNovel = true
			if en.Count < 1 {
				t.Errorf("novel histogram count for the crafted tag = %d, want >=1", en.Count)
			}
		}
	}
	if !foundNovel {
		t.Errorf("the rollup's novel histogram did not capture the crafted novel: tag; hist=%v", hist)
	}
}

// TestLegibleOfflineDecisionUnchanged is the parity-of-DECISION half of part-2: the SHADOW instrument
// never changes routing, so the controller's decision sequence is byte-identical whether the toggle is ON
// or OFF. (The instrument only observes; the verdict the floor produced is the one that routes.)
func TestLegibleOfflineDecisionUnchanged(t *testing.T) {
	decisionsOff := captureDecisions(t, false)
	decisionsOn := captureDecisions(t, true)
	if len(decisionsOff) == 0 {
		t.Fatal("expected at least one decision in the OFF run")
	}
	if len(decisionsOff) != len(decisionsOn) {
		t.Fatalf("decision count changed by the shadow: off=%d on=%d (%v vs %v)",
			len(decisionsOff), len(decisionsOn), decisionsOff, decisionsOn)
	}
	for i := range decisionsOff {
		if decisionsOff[i] != decisionsOn[i] {
			t.Errorf("decision %d differs: off=%q on=%q — the shadow must not change routing",
				i, decisionsOff[i], decisionsOn[i])
		}
	}
}

// TestLegibleOfflineOffIsSilent is the byte-identical guard: with the toggle OFF (the default), the SAME
// crafted tagged Generate flows through the engine WITHOUT a single legible.* event — the goldens hold.
// (And the tag is NOT stripped off the prompt-less double path: with the instrument off the test double
// never receives a fragment, so the only difference vs a normal run is the crafted Generate return.)
func TestLegibleOfflineOffIsSilent(t *testing.T) {
	e, be, got := newTaggedEngine(t, false)
	e.SubmitDefault("how should I verify an uncertain claim?")
	runToIdle(e)

	// OFF: the engine clears the fragment (no contract pushed onto the conscious).
	if be.lastFragment != "" {
		t.Errorf("legible OFF: the Generate fragment must be cleared, got %q", be.lastFragment)
	}
	for _, kind := range []string{events.LegibleTag, events.LegibleParity, events.LegibleNovel} {
		if n := len(eventsByKind(got, kind)); n != 0 {
			t.Errorf("legible OFF must emit zero %s events, got %d", kind, n)
		}
	}
	// And the rollup over the OFF stream is Empty (the honest no-events report).
	if !legible.RollupOf(*got).Empty() {
		t.Error("legible OFF: the rollup over the stream must be Empty (no legible.* events)")
	}
}

// captureDecisions runs one episode on the tagged stub (toggle = legibleOn) and returns the ordered
// controller decisions (the conscious.decision event stream), so two runs can be compared for parity.
func captureDecisions(t *testing.T, legibleOn bool) []string {
	t.Helper()
	e, _, got := newTaggedEngine(t, legibleOn)
	e.SubmitDefault("how should I verify an uncertain claim?")
	runToIdle(e)
	var decisions []string
	for _, ev := range eventsByKind(got, events.Decision) {
		if d, ok := ev.Data["decision"].(string); ok {
			decisions = append(decisions, d)
		}
	}
	return decisions
}

// assertNoEnvelopeLeak fails if any voiced text (Generate or Append) contains the envelope frame glyphs —
// proof TRANSFORM/strip voiced the clean prose and the internal tag never reached the stream.
func assertNoEnvelopeLeak(t *testing.T, got *[]events.Event) {
	t.Helper()
	for _, kind := range []string{events.Generate, events.Append} {
		for _, ev := range eventsByKind(got, kind) {
			text, _ := ev.Data["text"].(string)
			if strings.ContainsAny(text, "⟨⟩") {
				t.Errorf("%s leaked the envelope into the voiced text: %q", kind, text)
			}
		}
	}
}

// voicedTextSeen reports whether the stripped prose was voiced (the Generate/Append text carried it).
func voicedTextSeen(got *[]events.Event, want string) bool {
	for _, kind := range []string{events.Generate, events.Append} {
		for _, ev := range eventsByKind(got, kind) {
			if text, _ := ev.Data["text"].(string); strings.Contains(text, want) {
				return true
			}
		}
	}
	return false
}

// hasParitySite reports whether any parity event carries the given site.
func hasParitySite(parities []events.Event, site string) bool {
	for _, ev := range parities {
		if s, _ := ev.Data["site"].(string); s == site {
			return true
		}
	}
	return false
}
