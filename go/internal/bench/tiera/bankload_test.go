package tiera

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGroundingBankLoadsAndMultiFileMaterializes pins that the pilot grounding bank parses and that a
// MULTI-FILE item (read an index, then the file it points to) materializes ALL its files into the sandbox.
func TestGroundingBankLoadsAndMultiFileMaterializes(t *testing.T) {
	items, err := LoadItems("../banks/pilot/grounding-tiera.jsonl")
	if err != nil {
		t.Fatalf("load grounding bank: %v", err)
	}
	if len(items) < 16 {
		t.Fatalf("grounding bank should have >=16 items after the expansion, got %d", len(items))
	}
	var multi *int
	for i := range items {
		if items[i].ID == "grounding-A-gold-0014" {
			multi = &i
		}
	}
	if multi == nil {
		t.Fatal("multi-file item grounding-A-gold-0014 not found")
	}
	sb, cleanup, err := Materialize(items[*multi])
	defer cleanup()
	if err != nil {
		t.Fatalf("materialize multi-file item: %v", err)
	}
	for _, f := range []string{"config/active.json", "config/profiles/prod.yaml", "config/profiles/dev.yaml"} {
		if _, st := os.Stat(filepath.Join(sb.Root, f)); st != nil {
			t.Errorf("multi-file: %s was not materialized into the sandbox", f)
		}
	}
}
