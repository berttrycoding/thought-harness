package engine_test

// A-RAG2 cognition-property tests — does the embeddings SIDECAR actually LIGHT UP the dense half of the
// shared hybrid retriever, and is that decision OBSERVABLE? These pin the *thinking* the spec intends:
// the DENSE channel only fires when the subconscious.semantic_recall knob is ON; with an embedder
// present the retriever goes HYBRID (the conscious stream's recall + the subconscious Offer can now pull
// on semantic similarity, not just word overlap); with no embedder it HONESTLY falls back to lexical and
// SAYS SO; and with the knob OFF the wiring is silent + byte-identical (no announce, the legacy probe).
//
// This is a CONTROL/plumbing slice: the embedder is a retrieval SIGNAL, never a CONTENT author, so a
// deterministic injected double is sufficient proof (no live substrate needed). A real semantic LIFT
// against a live sidecar is a deferred, user-authorized config-search A/B.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// fakeEmbedder is a deterministic, offline retrieval.Embedder double: it maps text to a fixed-width
// vector by hashing each word into a bucket (a bag-of-words sketch). Same text -> same vector, no
// network — the test seam that proves the dense channel lights up WITHOUT a live sidecar.
type fakeEmbedder struct{ dims int }

func (f fakeEmbedder) Embed(text string) ([]float32, error) {
	v := make([]float32, f.dims)
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		h := 2166136261
		for _, r := range word {
			h = (h ^ int(r)) * 16777619
		}
		if h < 0 {
			h = -h
		}
		v[h%f.dims] += 1
		word = word[:0]
	}
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' {
			flush()
			continue
		}
		word = append(word, r)
	}
	flush()
	return v, nil
}

// semanticEngine builds an engine on the offline TestBackend with the semantic_recall knob set and NO
// injected embedder, returning the engine. The retrieval.semantic announce fires DURING NewEngine, so
// the test reads it back off the bus replay ring (a subscriber added after construction would miss a
// construction-time emit).
func semanticEngine(t *testing.T, knobOn bool) *engine.Engine {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.AllOn()
	feat.Subconscious.SemanticRecall = knobOn
	cfg.Features = &feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// retrievalSemanticAnnounce drives one Step (the retrieval.semantic announce is DEFERRED to the first
// Step, like config.load/persist.load, so the CLI/TUI sinks subscribed after NewEngine receive it — the
// SAME pattern the live `--log` trace relies on) then reads the announce off the bus replay ring
// (oldest-to-newest), returning ok=false if none fired.
func retrievalSemanticAnnounce(e *engine.Engine) (events.Event, bool) {
	e.Step()
	for _, ev := range e.Bus().Recent(4000, nil) {
		if ev.Kind == string(events.RetrievalSemantic) {
			return ev, true
		}
	}
	return events.Event{}, false
}

// TestSemanticRecallLightsUpDenseChannelWhenKnobOnAndEmbedderInjected: the CORE claim. With the knob ON
// and an embedder present, the shared hybrid retriever goes HYBRID — the dense (cosine) channel is live,
// so recall/Offer rank on semantic similarity and not word-overlap alone. And the decision is OBSERVABLE:
// one retrieval.semantic announce fires reporting mode=hybrid.
func TestSemanticRecallLightsUpDenseChannelWhenKnobOnAndEmbedderInjected(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.AllOn()
	feat.Subconscious.SemanticRecall = true
	cfg.Features = &feat
	cfg.Embedder = fakeEmbedder{dims: 16}
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if got := e.RetrieverMode(); got != "hybrid" {
		t.Fatalf("with semantic_recall ON + an injected embedder the retriever must be hybrid, got %q", got)
	}
	ev, ok := retrievalSemanticAnnounce(e)
	if !ok {
		t.Fatal("semantic_recall ON must emit a retrieval.semantic announce at construction")
	}
	if mode, _ := ev.Data["mode"].(string); mode != "hybrid" {
		t.Fatalf("announce mode = %v, want hybrid (the dense channel lit up)", ev.Data["mode"])
	}
	if src, _ := ev.Data["source"].(string); src != "injected" {
		t.Fatalf("announce source = %v, want injected", ev.Data["source"])
	}

	// behavioral proof the dense channel THREADS DOWNSTREAM (not just a top-level flag): the operator
	// catalog's Offer retriever reports "semantic" when an embedder is wired (capEngages=true), so the
	// at-scale curation now bridges MEANING and not word-overlap alone — the injected embedder reached
	// the registry the conscious stream actually pulls through.
	if got := e.Catalog().RetrieverMode(true); got != "semantic" {
		t.Fatalf("the injected embedder must thread into the operator catalog (RetrieverMode=semantic), got %q", got)
	}
}

// TestSemanticRecallHonestLexicalFallbackWhenNoSidecar: the no-fabrication property. With the knob ON but
// NO embedder reachable (offline test double, no injection, no sidecar), the retriever must NOT pretend
// to be hybrid — it falls back to lexical AND says so on the announce, so a silent degrade is never
// mistaken for a lit dense channel. The test double never dials the network (deterministic, offline).
func TestSemanticRecallHonestLexicalFallbackWhenNoSidecar(t *testing.T) {
	e := semanticEngine(t, true)

	if got := e.RetrieverMode(); got != "lexical" {
		t.Fatalf("with semantic_recall ON but no embedder reachable, the retriever must honestly be lexical, got %q", got)
	}
	ev, ok := retrievalSemanticAnnounce(e)
	if !ok {
		t.Fatal("semantic_recall ON must emit a retrieval.semantic announce even on fallback (the decision is never silent)")
	}
	if mode, _ := ev.Data["mode"].(string); mode != "lexical" {
		t.Fatalf("announce mode = %v, want lexical (honest fallback, not a faked hybrid)", ev.Data["mode"])
	}
	if reason, _ := ev.Data["reason"].(string); reason == "" {
		t.Fatal("a lexical fallback announce must carry a non-empty reason (why the dense channel is dark)")
	}
}

// TestSemanticRecallOffIsSilentAndByteIdentical: the default-OFF guard. With the knob OFF, the new wiring
// is INERT — no retrieval.semantic announce fires (the legacy incidental silent probe path runs), so the
// default run's wire vocabulary is byte-identical to pre-A-RAG2. Even an injected embedder stays silent.
func TestSemanticRecallOffIsSilentAndByteIdentical(t *testing.T) {
	e := semanticEngine(t, false)
	if _, ok := retrievalSemanticAnnounce(e); ok {
		t.Fatal("semantic_recall OFF must emit NO retrieval.semantic event (silent, byte-identical default)")
	}

	// and with the knob OFF the injected embedder is NOT announced either (the announce is the knob's,
	// not the embedder's) — the off-path stays silent on the wire.
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.AllOn()
	feat.Subconscious.SemanticRecall = false
	cfg.Features = &feat
	cfg.Embedder = fakeEmbedder{dims: 16}
	e2, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if _, ok := retrievalSemanticAnnounce(e2); ok {
		t.Fatal("semantic_recall OFF must stay silent even with an injected embedder")
	}
}
