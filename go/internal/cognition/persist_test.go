package cognition

import (
	"bytes"
	"strings"
	"testing"
)

// TestSkillPersistenceRoundTrip is the P7.1 gate: a minted skill survives a "restart" (save to a store,
// load into a FRESH registry) and is recallable by Match afterward — convertibility now outlives the
// process. Seed skills are never written (frozen invariants reload from code).
func TestSkillPersistenceRoundTrip(t *testing.T) {
	a := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", ""), NewStep("rank", "general", "")))
	if _, ok := a.Mint("learned-frobnicate", []string{"frobnicate"}, body, "", "learned thing"); !ok {
		t.Fatal("minting the skill must succeed")
	}

	var buf bytes.Buffer
	if err := a.SaveMinted(&buf); err != nil {
		t.Fatalf("SaveMinted: %v", err)
	}
	// only the minted skill is written — no seed skills leak into the store.
	if lines := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; lines != 1 {
		t.Fatalf("expected exactly 1 minted skill line, got %d:\n%s", lines, buf.String())
	}
	if strings.Contains(buf.String(), "diagnose") {
		t.Fatal("a seed skill leaked into the minted store")
	}

	// "restart": a fresh registry loads the store.
	b := NewSkillRegistry(true)
	if b.Has("learned-frobnicate") {
		t.Fatal("precondition: the fresh registry should not already have the minted skill")
	}
	n, err := b.LoadMinted(&buf)
	if err != nil || n != 1 {
		t.Fatalf("LoadMinted: n=%d err=%v", n, err)
	}
	// it survived + is recallable.
	got, found := b.Match("frobnicate the pipeline")
	if !found || got.Name != "learned-frobnicate" {
		t.Fatalf("the loaded skill must be recallable; found=%v name=%q", found, got.Name)
	}
	if !b.skills["learned-frobnicate"].Synthesized {
		t.Fatal("a loaded minted skill must remain Synthesized (so it persists again, never frozen as seed)")
	}
}

// TestOperatorPersistenceRoundTrip: a minted operator survives a restart through the same seam.
func TestOperatorPersistenceRoundTrip(t *testing.T) {
	a := NewOperatorRegistry()
	if _, ok := a.Mint("frobnicate", "transformative", "do the frobnicate transform"); !ok {
		t.Fatal("minting the operator must succeed")
	}
	var buf bytes.Buffer
	if err := a.SaveMinted(&buf); err != nil {
		t.Fatalf("SaveMinted: %v", err)
	}
	if strings.Contains(buf.String(), "decompose") {
		t.Fatal("a seed operator leaked into the minted store")
	}

	b := NewOperatorRegistry()
	n, err := b.LoadMinted(&buf)
	if err != nil || n != 1 {
		t.Fatalf("LoadMinted: n=%d err=%v", n, err)
	}
	if !b.Has("frobnicate") {
		t.Fatal("the loaded minted operator must be present")
	}
}

// TestLoadMintedBestEffort: a corrupt store degrades gracefully (skips bad lines, never panics).
func TestLoadMintedBestEffort(t *testing.T) {
	b := NewSkillRegistry(true)
	corrupt := strings.NewReader("not json\n{\"name\":\"x\"}\n\n")
	if _, err := b.LoadMinted(corrupt); err != nil {
		t.Fatalf("LoadMinted should not error on corrupt input; got %v", err)
	}
}
