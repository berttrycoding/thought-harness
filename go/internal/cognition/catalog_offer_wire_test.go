package cognition

// catalog_offer_wire_test.go — W3 synthesiser catalog curation, the WIRING half. These tests assert
// that the bounded subset selector actually feeds the synthesis prompt through Synthesize (not just
// that Offer is correct in isolation), and that the dedicated subconscious.catalog_offer event fires
// with the right contents when curation is ON — and stays silent (byte-identical) when OFF.

import (
	"reflect"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// recordingToolmaker is a backend that captures the EXACT operator subset Synthesize handed to the
// synthesis prompt (the opNames argument), then writes a trivial valid program so the path completes.
// This is how we PROVE the curated subset reaches the prompt (wiring), not just that Offer is correct.
type recordingToolmaker struct {
	*fakeToolmaker
	gotOpNames []string
}

func (r *recordingToolmaker) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	r.gotOpNames = append([]string(nil), opNames...)
	return r.spec, true
}

// withSynthCap temporarily overrides the package-level SynthOfferCap for one test, restoring it after.
// (SynthOfferCap is resolved once from THOUGHT_SYNTH_CATALOG_TOPK at init; tests flip the resolved
// value directly so they don't depend on process env.)
func withSynthCap(t *testing.T, cap int) {
	t.Helper()
	prev := SynthOfferCap
	SynthOfferCap = cap
	t.Cleanup(func() { SynthOfferCap = prev })
}

// validToolmakerSpec is a program the synth path will accept (decompose>generate, both seed ops).
func validToolmakerSpec() map[string]any {
	return map[string]any{
		"program": map[string]any{"kind": "seq", "children": []any{
			map[string]any{"kind": "step", "operator": "decompose", "domain": "general", "note": "split"},
			map[string]any{"kind": "step", "operator": "generate", "domain": "general", "note": "draft"},
		}},
		"rationale": "toolmaker-written",
		"source":    "llm",
	}
}

// TestCurationOnFeedsBoundedSubsetToPrompt is the WIRING proof: with curation ON the synthesis prompt
// receives the bounded, goal-scored subset (== catalog.Offer), NOT the whole catalog, and the
// subconscious.catalog_offer event fires with the matching contents.
func TestCurationOnFeedsBoundedSubsetToPrompt(t *testing.T) {
	const k = 10
	withSynthCap(t, k)

	cat := NewOperatorRegistry()
	// grow the catalog far past k so curation has something to bound.
	for i := 0; i < 60; i++ {
		cat.MintWithMove("mintedop"+itoaPad(i), "generative", "invent a candidate for topic "+itoaPad(i), MoveGround)
	}
	catalogTotal := len(cat.Names())
	if catalogTotal <= k {
		t.Fatalf("precondition: catalog must exceed k, got %d", catalogTotal)
	}
	wantSubset := cat.Offer("invent a candidate for topic 042", k)
	if len(wantSubset) != k {
		t.Fatalf("precondition: Offer must bound to k=%d, got %d", k, len(wantSubset))
	}

	be := &recordingToolmaker{fakeToolmaker: &fakeToolmaker{TestBackend: backends.NewTest(), spec: validToolmakerSpec()}}
	var emitted []capturedEvent
	res, ok := Synthesize("invent a candidate for topic 042", nil, cat, be, captureEmit(&emitted), nil)
	if !ok || res == nil {
		t.Fatalf("Synthesize ok=%v res=%v, want a toolmaker program", ok, res)
	}

	// WIRING: the subset the backend actually received must be exactly the curated Offer subset.
	if !reflect.DeepEqual(be.gotOpNames, wantSubset) {
		t.Fatalf("synthesis prompt did not receive the curated subset:\n got  %v\n want %v", be.gotOpNames, wantSubset)
	}
	if len(be.gotOpNames) >= catalogTotal {
		t.Errorf("curation ON must shrink the prompt: got %d of %d operators", len(be.gotOpNames), catalogTotal)
	}

	// the dedicated event must have fired with the right contents.
	var offerEv *capturedEvent
	for i := range emitted {
		if emitted[i].kind == string(events.SubCatalogOffer) {
			offerEv = &emitted[i]
			break
		}
	}
	if offerEv == nil {
		t.Fatalf("subconscious.catalog_offer did not fire under curation; emitted=%v", kindsOf(emitted))
	}
	if got, ok := offerEv.data["offered"].([]string); !ok || !reflect.DeepEqual(got, wantSubset) {
		t.Errorf("event data[offered] = %v, want %v", offerEv.data["offered"], wantSubset)
	}
	if offerEv.data["count"] != k {
		t.Errorf("event data[count] = %v, want %d", offerEv.data["count"], k)
	}
	if offerEv.data["catalog_total"] != catalogTotal {
		t.Errorf("event data[catalog_total] = %v, want %d", offerEv.data["catalog_total"], catalogTotal)
	}
	if offerEv.data["top_k"] != k {
		t.Errorf("event data[top_k] = %v, want %d", offerEv.data["top_k"], k)
	}
	if offerEv.data["goal"] != "invent a candidate for topic 042" {
		t.Errorf("event data[goal] = %v", offerEv.data["goal"])
	}
}

// TestCurationOffPromptIsWholeCatalogAndSilent is the byte-identical default proof: with curation OFF
// (SynthOfferCap == 0, the default), the synthesis prompt receives the WHOLE catalog (== Names()) and
// the subconscious.catalog_offer event is NEVER emitted — so every golden holds.
func TestCurationOffPromptIsWholeCatalogAndSilent(t *testing.T) {
	withSynthCap(t, 0) // the default — explicit for clarity

	cat := NewOperatorRegistry()
	for i := 0; i < 60; i++ {
		cat.MintWithMove("mintedop"+itoaPad(i), "generative", "invent a candidate for topic "+itoaPad(i), MoveGround)
	}
	wantWhole := cat.Names()

	be := &recordingToolmaker{fakeToolmaker: &fakeToolmaker{TestBackend: backends.NewTest(), spec: validToolmakerSpec()}}
	var emitted []capturedEvent
	_, ok := Synthesize("invent a candidate for topic 042", nil, cat, be, captureEmit(&emitted), nil)
	if !ok {
		t.Fatalf("Synthesize must succeed")
	}

	// WIRING (default): the backend received the WHOLE catalog, in Names() order — byte-identical.
	if !reflect.DeepEqual(be.gotOpNames, wantWhole) {
		t.Fatalf("default-OFF prompt must be the whole catalog:\n got  %d ops\n want %d ops", len(be.gotOpNames), len(wantWhole))
	}
	// the curation event must be SILENT on the default path.
	for _, e := range emitted {
		if e.kind == string(events.SubCatalogOffer) {
			t.Fatalf("subconscious.catalog_offer must NOT fire when curation is OFF (default); emitted=%v", kindsOf(emitted))
		}
	}
}

// TestCurationOnButCatalogFitsIsSilent — even with curation ON, when the catalog fits within k the
// whole catalog is offered (Offer is a no-op), so the event stays silent (it only narrates a genuine
// narrowing). Pins that the event tracks the SUBSET-actually-shaped condition, not merely "flag on".
func TestCurationOnButCatalogFitsIsSilent(t *testing.T) {
	cat := NewOperatorRegistry()
	withSynthCap(t, len(cat.Names())+50) // cap above the catalog ⇒ Offer == whole catalog

	be := &recordingToolmaker{fakeToolmaker: &fakeToolmaker{TestBackend: backends.NewTest(), spec: validToolmakerSpec()}}
	var emitted []capturedEvent
	_, ok := Synthesize("design a small service", nil, cat, be, captureEmit(&emitted), nil)
	if !ok {
		t.Fatalf("Synthesize must succeed")
	}
	if !reflect.DeepEqual(be.gotOpNames, cat.Names()) {
		t.Errorf("when the catalog fits within k the whole catalog must be offered")
	}
	for _, e := range emitted {
		if e.kind == string(events.SubCatalogOffer) {
			t.Fatalf("catalog_offer must stay silent when no narrowing happened; emitted=%v", kindsOf(emitted))
		}
	}
}
