package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// --- test doubles for the ladder ports ------------------------------------

type fakeKnowledge struct {
	hits     []knowledge.Knowledge
	recorded []knowledge.Knowledge
}

func (f *fakeKnowledge) Recall(query, kind string, n int) []knowledge.Knowledge {
	if n <= 0 || len(f.hits) == 0 {
		return nil
	}
	if n > len(f.hits) {
		n = len(f.hits)
	}
	return f.hits[:n]
}
func (f *fakeKnowledge) Record(k knowledge.Knowledge) bool {
	if !k.Grounded {
		return false
	}
	f.recorded = append(f.recorded, k)
	return true
}

type fakeMemory struct {
	fact string
	ok   bool
}

func (f fakeMemory) RecallFact(query string) (string, bool) { return f.fact, f.ok }

type fakeReality struct {
	text    string
	ok      bool
	grounds bool
	tool    string
}

func (f fakeReality) SourceReality(need FuelNeed) (string, bool, bool, string) {
	return f.text, f.ok, f.grounds, f.tool
}

type fakeGen struct{ text string }

func (f fakeGen) GenerateFuel(need FuelNeed) string { return f.text }

type fakeGraph struct {
	text     string
	ok       bool
	hops     int
	provider string
	calls    int
}

func (f *fakeGraph) RecallGraph(need FuelNeed) (string, bool, int, string) {
	f.calls++
	return f.text, f.ok, f.hops, f.provider
}

func allSources() *config.SourceToggles {
	r := config.AllOnRepr()
	return &r.Sources
}

// TestLadderResolvesInOrder asserts the strict preference order: with every rung populated, present
// wins; remove present, knowledge wins; remove knowledge, memory wins; remove memory, reality wins; and
// reality OUTRANKS generated.
func TestLadderResolvesInOrder(t *testing.T) {
	present := types.Thought{ID: 1, Text: "the deploy build pipeline runs the integration tests"}
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{{Statement: "k-fact", Kind: "fact", Grounded: true, Trust: 0.9}}}
	mem := fakeMemory{fact: "m-fact", ok: true}
	real := fakeReality{text: "r-fact", ok: true, grounds: true, tool: "run_tests"}
	gen := fakeGen{text: "g-fact"}

	need := func(ctx []types.Thought) FuelNeed {
		return FuelNeed{Query: "deploy build pipeline integration tests", Context: ctx,
			AllowReality: true, AllowGenerated: true}
	}

	// 1. present wins (the candidate's material is already in the stream).
	p := NewSourcingPolicy(kn, mem, real, gen, allSources(), nil, nil)
	if f := p.Source(need([]types.Thought{present})); f.Source != FuelPresent {
		t.Fatalf("present rung: got %s, want present", f.Source)
	}

	// 2. no present -> knowledge.
	if f := p.Source(need(nil)); f.Source != FuelKnowledge || f.Text != "k-fact" {
		t.Fatalf("knowledge rung: got %s %q, want knowledge k-fact", f.Source, f.Text)
	}

	// 3. no knowledge -> memory.
	p2 := NewSourcingPolicy(&fakeKnowledge{}, mem, real, gen, allSources(), nil, nil)
	if f := p2.Source(need(nil)); f.Source != FuelMemory || f.Text != "m-fact" {
		t.Fatalf("memory rung: got %s %q, want memory m-fact", f.Source, f.Text)
	}

	// 4. no memory -> reality (and reality OUTRANKS generated).
	p3 := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, gen, allSources(), nil, nil)
	if f := p3.Source(need(nil)); f.Source != FuelReality || f.Text != "r-fact" {
		t.Fatalf("reality rung: got %s %q, want reality r-fact (reality outranks generated)", f.Source, f.Text)
	}

	// 5. no grounded source -> generated (the low-trust floor).
	p4 := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{}, gen, allSources(), nil, nil)
	f := p4.Source(need(nil))
	if f.Source != FuelGenerated || f.Grounded || f.Trust != trustGenerated {
		t.Fatalf("generated rung: got %s grounded=%v trust=%v, want generated grounded=false trust=%v",
			f.Source, f.Grounded, f.Trust, trustGenerated)
	}
}

// TestRealityWriteBack asserts a reality-sourced fact is written BACK to the knowledge registry so the
// next identical need is a rung-2 (knowledge) hit (the ladder teaches the registry).
func TestRealityWriteBack(t *testing.T) {
	kn := &fakeKnowledge{} // empty -> reality must be reached
	real := fakeReality{text: "12 of 12 tests pass", ok: true, grounds: true, tool: "run_tests"}
	p := NewSourcingPolicy(kn, fakeMemory{}, real, nil, allSources(), nil, nil)

	need := FuelNeed{Query: "do the tests pass", AllowReality: true, Entities: []string{"tests"}}
	f := p.Source(need)
	if f.Source != FuelReality {
		t.Fatalf("expected reality, got %s", f.Source)
	}
	if len(kn.recorded) != 1 || kn.recorded[0].Statement != "12 of 12 tests pass" || !kn.recorded[0].Grounded {
		t.Fatalf("reality fact was not written back to knowledge: %+v", kn.recorded)
	}
	if kn.recorded[0].Source != "reality:run_tests" {
		t.Fatalf("write-back provenance: got %q, want reality:run_tests", kn.recorded[0].Source)
	}
}

// TestFabricatedRealityNotSourced asserts a FABRICATED observation (grounds=false) is NOT sourced — the
// ladder falls through to generated rather than laundering a fake reality.
func TestFabricatedRealityNotSourced(t *testing.T) {
	real := fakeReality{text: "fake pass", ok: true, grounds: false, tool: "run_tests"}
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, fakeGen{text: "g"}, allSources(), nil, nil)
	f := p.Source(FuelNeed{Query: "do the tests pass", AllowReality: true, AllowGenerated: true})
	if f.Source == FuelReality {
		t.Fatal("a fabricated observation was sourced as reality (fake reality laundered)")
	}
	if f.Source != FuelGenerated {
		t.Fatalf("fabricated reality should fall through to generated, got %s", f.Source)
	}
}

// TestSourceTogglesSkipRungs asserts a disabled source is skipped in the walk (Reality=off -> rung 4 is
// a no-op; Generated=off is the strict-grounding posture -> FuelNone when nothing grounded is sourced).
func TestSourceTogglesSkipRungs(t *testing.T) {
	src := config.SourceToggles{Present: true, Knowledge: true, Memory: true, Reality: false, Generated: false}
	real := fakeReality{text: "r", ok: true, grounds: true, tool: "run_tests"}
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, fakeGen{text: "g"}, &src, nil, nil)
	// reality off, generated off, nothing grounded earlier -> FuelNone (strict grounding never guesses).
	f := p.Source(FuelNeed{Query: "anything", AllowReality: true, AllowGenerated: true})
	if f.Source != FuelNone {
		t.Fatalf("strict-grounding posture: got %s, want none (reality+generated off)", f.Source)
	}
}

// TestSourcingGateBypass asserts a disabled subconscious.sourcing gate bypasses the whole ladder to
// FuelNone and emits config.skip (toggle = bypass, not delete).
func TestSourcingGateBypass(t *testing.T) {
	var skipped bool
	emit := func(kind, summary string, data map[string]any) events.Event {
		if kind == events.ConfigSkip {
			skipped = true
		}
		return events.Event{Kind: kind}
	}
	off := false
	gate := config.NewGate("subconscious.sourcing", func() bool { return off }, emit)
	p := NewSourcingPolicy(&fakeKnowledge{hits: []knowledge.Knowledge{{Statement: "x", Grounded: true}}},
		fakeMemory{}, fakeReality{}, fakeGen{}, allSources(), gate, emit)
	f := p.Source(FuelNeed{Query: "x"})
	if f.Source != FuelNone {
		t.Fatalf("disabled sourcing gate should bypass to none, got %s", f.Source)
	}
	if !skipped {
		t.Fatal("disabled sourcing gate did not emit config.skip")
	}
}

// TestGraphRungRanksBetweenMemoryAndReality is the A-RAG3 cognition claim at the LADDER level: when the
// lexical stores (present/knowledge/memory) MISS but the graph recaller HITS, the ladder resolves at the
// new FuelGraph rung — cheaper than touching reality (no tool call; the cogngraph extraction cost is
// already sunk) yet grounded. It must rank ABOVE reality (reality is only reached on a graph miss) and
// be GROUNDED at the graph trust prior.
func TestGraphRungRanksBetweenMemoryAndReality(t *testing.T) {
	graph := &fakeGraph{text: "ActionMargin: 1.5", ok: true, hops: 2, provider: "graph:grounds@2"}
	real := fakeReality{text: "reality-fact", ok: true, grounds: true, tool: "run_tests"}
	// empty knowledge + empty memory -> the walk reaches the graph rung; reality would also hit but must
	// be OUTRANKED by graph.
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, nil, allSources(), nil, nil)
	on := true
	p.SetGraphRecaller(graph, config.NewGate("subconscious.graph_recall", func() bool { return on }, nil))

	f := p.Source(FuelNeed{Query: "deploy margin", AllowReality: true})
	if f.Source != FuelGraph {
		t.Fatalf("graph rung: got %s, want graph (graph hit must outrank reality)", f.Source)
	}
	if f.Text != "ActionMargin: 1.5" || !f.Grounded || f.Trust != trustGraph {
		t.Fatalf("graph fuel: got text=%q grounded=%v trust=%v, want the recalled fact grounded at %v",
			f.Text, f.Grounded, f.Trust, trustGraph)
	}
	if graph.calls != 1 {
		t.Fatalf("graph recaller called %d times, want exactly 1", graph.calls)
	}
}

// TestGraphRungSkippedWhenGateOff asserts the default-OFF posture: with the graph_recall gate DISABLED
// the FuelGraph rung is skipped ENTIRELY (the recaller is never consulted) and the ladder falls through
// to reality — byte-identical to the pre-A-RAG3 walk.
func TestGraphRungSkippedWhenGateOff(t *testing.T) {
	graph := &fakeGraph{text: "graph-fact", ok: true, hops: 2}
	real := fakeReality{text: "reality-fact", ok: true, grounds: true, tool: "run_tests"}
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, nil, allSources(), nil, nil)
	off := false
	p.SetGraphRecaller(graph, config.NewGate("subconscious.graph_recall", func() bool { return off }, nil))

	f := p.Source(FuelNeed{Query: "anything", AllowReality: true})
	if f.Source != FuelReality {
		t.Fatalf("gate OFF: got %s, want reality (the graph rung must be skipped)", f.Source)
	}
	if graph.calls != 0 {
		t.Fatalf("gate OFF: the graph recaller was consulted %d times, want 0 (skipped, not just discarded)", graph.calls)
	}
}

// TestGraphRungMissFallsThrough asserts the never-fabricate discipline at the graph rung: a graph MISS
// (ok=false) falls through to reality rather than surfacing a stand-in.
func TestGraphRungMissFallsThrough(t *testing.T) {
	graph := &fakeGraph{ok: false}
	real := fakeReality{text: "reality-fact", ok: true, grounds: true, tool: "run_tests"}
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, real, nil, allSources(), nil, nil)
	on := true
	p.SetGraphRecaller(graph, config.NewGate("subconscious.graph_recall", func() bool { return on }, nil))

	f := p.Source(FuelNeed{Query: "anything", AllowReality: true})
	if f.Source != FuelReality {
		t.Fatalf("graph miss: got %s, want reality (a miss must fall through)", f.Source)
	}
	if graph.calls != 1 {
		t.Fatalf("graph recaller called %d times, want 1 (consulted then missed)", graph.calls)
	}
}
