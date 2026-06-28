package events

import (
	"encoding/json"
	"testing"
)

func TestKindVocabularySmoke(t *testing.T) {
	// No magic-number count gate here — that literal, repeated across three files, was a merge
	// hotspot. The DERIVED gate is TestAllKindsMatchConstBlock (const block ↔ allKinds). This is a
	// smoke check: the vocabulary is populated and every kind derives a non-empty wire layer.
	if len(allKinds) == 0 {
		t.Fatal("allKinds is empty")
	}
	b := New(4)
	for _, k := range allKinds {
		if ev := b.Emit(k, "", nil); ev.Layer == "" {
			t.Fatalf("kind %q derives an empty layer", k)
		}
	}
}

func TestLayerDerivation(t *testing.T) {
	b := New(10)
	cases := map[string]string{
		Filter:    "seam", // "seam.filter" -> "seam"
		SubFire:   "subconscious",
		Tick:      "tick", // bare kind -> whole string
		Port:      "port", // bare kind -> whole string
		Lifecycle: "lifecycle",
	}
	for kind, wantLayer := range cases {
		ev := b.Emit(kind, "s", nil)
		if ev.Layer != wantLayer {
			t.Errorf("kind %q: want layer %q, got %q", kind, wantLayer, ev.Layer)
		}
	}
}

func TestEmitDoesNotBumpTick(t *testing.T) {
	b := New(10)
	b.Tick = 7
	ev := b.Emit(Filter, "s", nil)
	if ev.Tick != 7 || b.Tick != 7 {
		t.Fatalf("Emit must not change Tick: ev.Tick=%d bus.Tick=%d", ev.Tick, b.Tick)
	}
}

func TestWireShape(t *testing.T) {
	b := New(10)
	b.Tick = 3
	ev := b.Emit(Filter, "admit", D{"confidence": 0.9})
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	// field order: tick, kind, layer, summary, data
	want := `{"tick":3,"kind":"seam.filter","layer":"seam","summary":"admit","data":{"confidence":0.9}}`
	if string(out) != want {
		t.Fatalf("wire shape mismatch:\n got %s\nwant %s", out, want)
	}
}

func TestNilDataIsEmptyMap(t *testing.T) {
	b := New(10)
	ev := b.Emit(Tick, "", nil)
	out, _ := json.Marshal(ev)
	want := `{"tick":0,"kind":"tick","layer":"tick","summary":"","data":{}}`
	if string(out) != want {
		t.Fatalf("nil data should marshal as {}: got %s", out)
	}
}

func TestSubscribeUnsubscribe(t *testing.T) {
	b := New(10)
	var got int
	unsub := b.Subscribe(func(Event) { got++ })
	b.Emit(Filter, "", nil)
	unsub()
	unsub() // idempotent
	b.Emit(Filter, "", nil)
	if got != 1 {
		t.Fatalf("want 1 delivery before unsubscribe, got %d", got)
	}
}

func TestRecentAndRing(t *testing.T) {
	b := New(3) // ring evicts beyond 3
	for i := 0; i < 5; i++ {
		b.Emit(Filter, "", D{"i": i})
	}
	r := b.Recent(10, nil)
	if len(r) != 3 {
		t.Fatalf("ring maxlen 3: want 3 retained, got %d", len(r))
	}
	if r[0].Data["i"].(int) != 2 || r[2].Data["i"].(int) != 4 {
		t.Fatalf("ring should hold the last 3 (i=2,3,4), got %v..%v", r[0].Data["i"], r[2].Data["i"])
	}
	// layer filter
	seam := "seam"
	if len(b.Recent(10, &seam)) != 3 {
		t.Fatalf("layer filter seam should match all 3")
	}
	other := "nope"
	if len(b.Recent(10, &other)) != 0 {
		t.Fatalf("layer filter nope should match 0")
	}
}
