package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// TestRegistrySnapshotSeedsCatalog proves the Tier-2 ablation path end-to-end at the engine level: a
// persist state dir holding minted-operator records (the bootstrap-001 conversion shape) constructs an
// engine whose live catalog CONTAINS those operators — the with-batch arm the bench's
// THOUGHT_BENCH_REGISTRY_STATE knob produces.
func TestRegistrySnapshotSeedsCatalog(t *testing.T) {
	dir := t.TempDir()
	rec := `{"meta":{"version":1,"status":"active","grounded":true,"use_count":1,"substrate":"claude-code:bootstrap:test"},"name":"unify","family":"relational","intent":"merge two related instances into the single covering abstraction that explains both","move":"lift"}`
	if err := os.WriteFile(filepath.Join(dir, "operators.jsonl"), []byte(rec+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Store = st
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	spec, ok := e.Catalog().Get("unify")
	if !ok {
		t.Fatal("the snapshot operator must be minted into the live catalog at construction")
	}
	if !spec.Synthesized || string(spec.Move) != "lift" {
		t.Fatalf("minted spec wrong: %+v", spec)
	}
}
