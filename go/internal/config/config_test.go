package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestAllOnIsAllOn asserts AllOn() enables every all-on BOOL toggle and sets the default tunables — the
// always-on baseline the M1 DoD requires (Features=nil ⇒ AllOn ⇒ byte-identical to pre-config). An
// OPT-IN knob (baseline OFF, e.g. seam.legible_generation, the SHADOW instrument) is the deliberate
// exception: it defaults OFF and so is excluded from both the all-on check and OffPaths() — that is
// what keeps the all-on default byte-identical even with an opt-in instrument present.
func TestAllOnIsAllOn(t *testing.T) {
	c := AllOn()
	for _, k := range Knobs() {
		if k.Kind != KnobBool || k.OptIn {
			continue
		}
		if v, _ := k.GetBool(&c); !v {
			t.Errorf("AllOn(): toggle %q is OFF, want ON", k.Path)
		}
	}
	if len(c.OffPaths()) != 0 {
		t.Errorf("AllOn().OffPaths() = %v, want empty", c.OffPaths())
	}
	if c.Subconscious.MaxParWidth != 8 {
		t.Errorf("AllOn().Subconscious.MaxParWidth = %d, want 8", c.Subconscious.MaxParWidth)
	}
	if c.Persist.Backend != "jsonl" {
		t.Errorf("AllOn().Persist.Backend = %q, want jsonl", c.Persist.Backend)
	}
}

// TestLegibleGenerationDefaultsOffOptIn locks the explicit exception: the legible-generation SHADOW
// instrument's knob exists, is OptIn, defaults OFF in AllOn(), and (being off) does NOT appear in
// OffPaths() — so a default run's config.load summary is byte-identical to before the instrument existed.
func TestLegibleGenerationDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Seam.LegibleGeneration {
		t.Fatal("seam.legible_generation must DEFAULT OFF in AllOn() (opt-in instrument)")
	}
	k, ok := KnobByPath("seam.legible_generation")
	if !ok {
		t.Fatal("seam.legible_generation knob must be registered")
	}
	if !k.OptIn {
		t.Error("seam.legible_generation must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "seam.legible_generation" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	// it is still addressable: flipping it on works and is then visible to the gate getter.
	if !ApplyToggle(&c, "seam.legible_generation", true) || !c.Seam.LegibleGeneration {
		t.Error("seam.legible_generation must be flippable on via ApplyToggle")
	}
}

// TestSolverPrimitiveSubAgentDefaultsOffOptIn locks the 5th-axis classical solver knob: it exists, is OptIn,
// defaults OFF in AllOn(), and (being off) does NOT appear in OffPaths() — so a default run's config.load
// summary and every golden are byte-identical to before the specialist existed. It is still addressable.
func TestSolverPrimitiveSubAgentDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Subconscious.SolverPrimitiveSubAgent {
		t.Fatal("subconscious.solver_specialist must DEFAULT OFF in AllOn() (opt-in 5th-axis component)")
	}
	k, ok := KnobByPath("subconscious.solver_specialist")
	if !ok {
		t.Fatal("subconscious.solver_specialist knob must be registered")
	}
	if !k.OptIn {
		t.Error("subconscious.solver_specialist must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "subconscious.solver_specialist" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "subconscious.solver_specialist", true) || !c.Subconscious.SolverPrimitiveSubAgent {
		t.Error("subconscious.solver_specialist must be flippable on via ApplyToggle")
	}
}

// TestConformanceSelfCheckDefaultsOffOptIn locks the L0 conformance instrument knob (Track H): it exists,
// is OptIn, defaults OFF in AllOn(), and (being off) does NOT appear in OffPaths() — so a default run's
// config.load summary and every golden are byte-identical to before the instrument existed. It is still
// addressable (the conformance rollup flips it on per run).
func TestConformanceSelfCheckDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Conformance.SelfCheck {
		t.Fatal("conformance.self_check must DEFAULT OFF in AllOn() (opt-in measurement instrument)")
	}
	k, ok := KnobByPath("conformance.self_check")
	if !ok {
		t.Fatal("conformance.self_check knob must be registered")
	}
	if !k.OptIn {
		t.Error("conformance.self_check must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "conformance.self_check" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "conformance.self_check", true) || !c.Conformance.SelfCheck {
		t.Error("conformance.self_check must be flippable on via ApplyToggle")
	}
}

// TestSlamCovarianceDefaultsOffOptIn locks the SLAM M2 sparse-covariance knob (Track F): it exists, is
// OptIn, defaults OFF in AllOn(), and (being off) does NOT appear in OffPaths() — so a default run's
// config.load summary and every golden are byte-identical to before the Information layer existed. It is
// still addressable (the config-search campaign flips it on per run). It rides on slam.innovation (it
// correlates that update's variance trajectory) but is its own flippable knob.
func TestSlamCovarianceDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Slam.Covariance {
		t.Fatal("slam.covariance must DEFAULT OFF in AllOn() (opt-in Information-layer instrument)")
	}
	k, ok := KnobByPath("slam.covariance")
	if !ok {
		t.Fatal("slam.covariance knob must be registered")
	}
	if !k.OptIn {
		t.Error("slam.covariance must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "slam.covariance" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "slam.covariance", true) || !c.Slam.Covariance {
		t.Error("slam.covariance must be flippable on via ApplyToggle")
	}
}

// TestSlamStalenessDefaultsOffOptIn locks the SLAM M4 freshness / staleness-decay knob (Track F): it
// exists, is OptIn, defaults OFF in AllOn(), and (being off) does NOT appear in OffPaths() — so a default
// run's config.load summary + every golden are byte-identical to before the staleness layer existed. The
// rate knob slam.staleness_q exists and is tunable, and the default rate is positive (so the layer-on case
// actually decays). It rides on slam.innovation (it decays that update's variance trajectory).
func TestSlamStalenessDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Slam.Staleness {
		t.Fatal("slam.staleness must DEFAULT OFF in AllOn() (opt-in dynamic-map process-noise instrument)")
	}
	if c.Slam.StalenessQ <= 0 {
		t.Fatalf("slam.staleness_q must default to a POSITIVE rate so the layer-on case decays; got %v", c.Slam.StalenessQ)
	}
	k, ok := KnobByPath("slam.staleness")
	if !ok {
		t.Fatal("slam.staleness knob must be registered")
	}
	if !k.OptIn {
		t.Error("slam.staleness must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "slam.staleness" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "slam.staleness", true) || !c.Slam.Staleness {
		t.Error("slam.staleness must be flippable on via ApplyToggle")
	}
	if _, ok := KnobByPath("slam.staleness_q"); !ok {
		t.Fatal("slam.staleness_q rate knob must be registered")
	}
}

// TestKeyframeDBDefaultsOffOptIn locks the F-M7 loop-closure / recurrence keyframe DB knob: it exists,
// is OptIn, defaults OFF in AllOn() (even though persistence itself is on), and (being off) does NOT
// appear in OffPaths() — so a default run's config.load summary + every golden are byte-identical to
// before the keyframe DB existed. It is still addressable (CLI/env/TUI can flip it on).
func TestKeyframeDBDefaultsOffOptIn(t *testing.T) {
	c := AllOn()
	if c.Persist.KeyframeDB {
		t.Fatal("persistence.keyframe_db must DEFAULT OFF in AllOn() (opt-in recurrence index)")
	}
	k, ok := KnobByPath("persistence.keyframe_db")
	if !ok {
		t.Fatal("persistence.keyframe_db knob must be registered")
	}
	if !k.OptIn {
		t.Error("persistence.keyframe_db must be marked OptIn (baseline OFF)")
	}
	for _, p := range c.OffPaths() {
		if p == "persistence.keyframe_db" {
			t.Error("a default-OFF opt-in knob must NOT appear in OffPaths() (would break byte-identical goldens)")
		}
	}
	if !ApplyToggle(&c, "persistence.keyframe_db", true) || !c.Persist.KeyframeDB {
		t.Error("persistence.keyframe_db must be flippable on via ApplyToggle")
	}
}

// TestNilGateEnabled asserts a nil *Gate (the Features=nil default) reports Enabled and Skip is a
// no-op — the invariant that makes the all-on default a true byte-identical no-op.
func TestNilGateEnabled(t *testing.T) {
	var g *Gate
	if !g.Enabled() {
		t.Fatal("nil Gate must report Enabled()==true")
	}
	if g.Disabled() {
		t.Fatal("nil Gate must report Disabled()==false")
	}
	g.Skip("any") // must not panic
}

// TestGateSkipsWhenOff asserts a disabled toggle short-circuits and emits config.skip exactly once
// per reason (deduped), and re-enabling is observed live with no reconstruction.
func TestGateSkipsWhenOff(t *testing.T) {
	c := AllOn()
	c.Seam.HiddenTransform = false
	var skips int
	emit := func(kind, summary string, data map[string]any) events.Event {
		if kind == "config.skip" {
			skips++
		}
		return events.Event{}
	}
	g := NewGate("seam.transform", func() bool { return c.Seam.HiddenTransform }, emit)
	if g.Enabled() {
		t.Fatal("gate must be disabled when toggle is OFF")
	}
	g.Skip("raw relay")
	g.Skip("raw relay") // deduped — no second emit
	if skips != 1 {
		t.Fatalf("config.skip emitted %d times, want exactly 1 (deduped)", skips)
	}
	// live re-enable: mutate the shared config; the gate observes it with no rebuild.
	c.Seam.HiddenTransform = true
	if !g.Enabled() {
		t.Fatal("gate must observe a live re-enable through the shared config pointer")
	}
}

// TestLoadFileMergesOverAllOn asserts a config file lists only the toggles it flips OFF (a bare {} is
// all-on); the merge is OVER the all-on baseline.
func TestLoadFileMergesOverAllOn(t *testing.T) {
	dir := t.TempDir()
	// a file that flips only two toggles + one tunable.
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{
      "seam": {"hidden_transform": false},
      "memory": {"reflect": false},
      "subconscious": {"max_par_width": 4}
    }`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(path, true, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Seam.HiddenTransform {
		t.Error("seam.hidden_transform should be OFF from the file")
	}
	if c.Memory.Reflect {
		t.Error("memory.reflect should be OFF from the file")
	}
	if c.Subconscious.MaxParWidth != 4 {
		t.Errorf("max_par_width = %d, want 4 from the file", c.Subconscious.MaxParWidth)
	}
	// everything NOT listed stays ON (merge over all-on).
	if !c.Seam.HiddenFilter || !c.Memory.Recall || !c.Value.Signal {
		t.Error("unlisted toggles must remain ON (merge over AllOn)")
	}
	off := c.OffPaths()
	if len(off) != 2 {
		t.Errorf("OffPaths() = %v, want exactly the 2 flipped toggles", off)
	}
}

// TestLoadBareIsAllOn asserts a bare {} file is all-on (the precedence baseline).
func TestLoadBareIsAllOn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(path, true, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.OffPaths()) != 0 {
		t.Errorf("bare {} OffPaths() = %v, want empty (all-on)", c.OffPaths())
	}
}

// TestLoadMissingDefaultPathIsAllOn asserts a missing DEFAULT path is not an error (the all-on
// baseline), while a missing EXPLICIT path IS an error.
func TestLoadMissingDefaultPathIsAllOn(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if _, _, err := Load(missing, false, nil); err != nil {
		t.Errorf("missing default path must not error, got %v", err)
	}
	if _, _, err := Load(missing, true, nil); err == nil {
		t.Error("missing explicit path must error")
	}
}

// TestLoadPrecedenceEnvOverFile asserts the precedence chain file < env: a THOUGHT_CFG_* env override
// wins over the file, and over the all-on baseline.
func TestLoadPrecedenceEnvOverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// file says hidden_transform ON (by omission) and hidden_gate OFF.
	if err := os.WriteFile(path, []byte(`{"seam": {"hidden_gate": false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// env flips hidden_transform OFF (over the all-on baseline) and hidden_gate back ON (over the file).
	t.Setenv("THOUGHT_CFG_SEAM_HIDDEN_TRANSFORM", "off")
	t.Setenv("THOUGHT_CFG_SEAM_HIDDEN_GATE", "on")
	c, _, err := Load(path, true, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Seam.HiddenTransform {
		t.Error("env THOUGHT_CFG_SEAM_HIDDEN_TRANSFORM=off must win over the all-on baseline")
	}
	if !c.Seam.HiddenGate {
		t.Error("env THOUGHT_CFG_SEAM_HIDDEN_GATE=on must win over the file's OFF")
	}
}

// TestApplyToggleAndCLITier asserts ApplyToggle (the CLI --enable/--disable + TUI live-flip tier)
// flips a bool knob by path and rejects unknown / non-bool paths.
func TestApplyToggleAndCLITier(t *testing.T) {
	c := AllOn()
	if !ApplyToggle(&c, "value.signal", false) {
		t.Fatal("ApplyToggle(value.signal,false) should succeed")
	}
	if c.Value.Signal {
		t.Error("value.signal should be OFF after ApplyToggle")
	}
	if ApplyToggle(&c, "no.such.path", false) {
		t.Error("ApplyToggle on an unknown path must fail")
	}
	if ApplyToggle(&c, "subconscious.max_par_width", false) {
		t.Error("ApplyToggle on a non-bool knob must fail")
	}
}

// TestValidateClampsMaxParWidth asserts Validate clamps MaxParWidth to W_max (the durability ceiling)
// and warns when the regulator is disabled — the M1 DoD coupling check.
func TestValidateClampsMaxParWidth(t *testing.T) {
	c := AllOn()
	c.Subconscious.MaxParWidth = 99
	warns := c.Validate()
	if c.Subconscious.MaxParWidth != WMax {
		t.Errorf("MaxParWidth = %d after Validate, want clamped to %d", c.Subconscious.MaxParWidth, WMax)
	}
	if len(warns) == 0 {
		t.Error("Validate should warn when clamping MaxParWidth")
	}

	c2 := AllOn()
	c2.Subconscious.MaxParWidth = 0
	c2.Validate()
	if c2.Subconscious.MaxParWidth != 1 {
		t.Errorf("MaxParWidth = %d after Validate, want clamped up to 1", c2.Subconscious.MaxParWidth)
	}

	c3 := AllOn()
	c3.Regulator.Enforce = false
	w := c3.Validate()
	found := false
	for _, s := range w {
		if contains(s, "regulator.enforce") {
			found = true
		}
	}
	if !found {
		t.Error("Validate should warn when regulator.enforce is OFF (regime-affecting)")
	}
}

// TestReprMatrixGating asserts the matrix gates moves + sources via the classifier, defaults all-ON =
// no gating, and the assess lane / unknown ops are never gated (conservative classifier).
func TestReprMatrixGating(t *testing.T) {
	m := AllOnRepr()
	// all-on: nothing gated.
	if !m.MoveEnabled(MoveGround) || !m.SourceEnabled(types.GENERATED) {
		t.Fatal("AllOnRepr must permit every move + source")
	}
	// flip GROUND off -> ground operators gated, others not.
	m.Moves.Ground = false
	if m.MoveEnabled(MoveGround) {
		t.Error("MoveGround should be gated when Moves.Ground=false")
	}
	if !m.MoveEnabled(MoveLift) {
		t.Error("MoveLift should stay enabled")
	}
	// assess + unknown are NEVER gated.
	if !m.MoveEnabled(MoveAssess) || !m.MoveEnabled(MoveUnknown) {
		t.Error("assess/unknown moves must never be gated")
	}
	// flip Generated off -> the generated rung is gated (strict-grounding posture).
	m2 := AllOnRepr()
	m2.Sources.Generated = false
	if m2.SourceEnabled(types.GENERATED) {
		t.Error("GENERATED source should be gated when Sources.Generated=false")
	}
	if !m2.SourceEnabled(types.OBSERVATION) {
		t.Error("OBSERVATION (reality) must stay enabled")
	}
	// nil matrix is always enabled.
	var nilm *ReprMatrix
	if !nilm.MoveEnabled(MoveGround) || !nilm.SourceEnabled(types.GENERATED) {
		t.Error("nil ReprMatrix must permit everything")
	}
}

// TestReprTagClassifier asserts the classifier places operators by move (the §2.3 table) and passes
// the provenance source through, and that an unknown operator is MoveUnknown (never gated).
func TestReprTagClassifier(t *testing.T) {
	cases := []struct {
		op   string
		want Move
	}{
		{"generate", MoveGround},
		{"generalize", MoveLift},
		{"analogize", MoveReframe},
		{"synonymize", MoveTranscode},
		{"rank", MoveAssess},
		{"validate", MoveAssess},
		{"totally-made-up-op", MoveUnknown},
	}
	for _, c := range cases {
		mv, src := ReprTag(c.op, types.GENERATED)
		if mv != c.want {
			t.Errorf("ReprTag(%q).move = %q, want %q", c.op, mv, c.want)
		}
		if src != types.GENERATED {
			t.Errorf("ReprTag(%q).source = %v, want GENERATED (pass-through)", c.op, src)
		}
	}
}

// TestSaveLoadRoundTrip asserts a saved config reloads identically (the Config tab `s`/`r` keys).
func TestSaveLoadRoundTrip(t *testing.T) {
	c := AllOn()
	c.Seam.HiddenGate = false
	c.Subconscious.MaxParWidth = 3
	c.Persist.Backend = "sqlite"
	path := filepath.Join(t.TempDir(), "saved.json")
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := Load(path, true, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Seam.HiddenGate {
		t.Error("round-trip lost seam.hidden_gate=false")
	}
	if got.Subconscious.MaxParWidth != 3 {
		t.Errorf("round-trip max_par_width = %d, want 3", got.Subconscious.MaxParWidth)
	}
	if got.Persist.Backend != "sqlite" {
		t.Errorf("round-trip backend = %q, want sqlite", got.Persist.Backend)
	}
}

// TestConfigLoadEmitsSummary asserts Load emits config.load with the OFF-toggle summary (a non-default
// config is never silent — the M1 DoD).
func TestConfigLoadEmitsSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"value": {"signal": false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var loaded bool
	var offCount int
	emit := func(kind, summary string, data map[string]any) events.Event {
		if kind == "config.load" {
			loaded = true
			if n, ok := data["count"].(int); ok {
				offCount = n
			}
		}
		return events.Event{}
	}
	if _, _, err := Load(path, true, emit); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded {
		t.Fatal("Load must emit config.load")
	}
	if offCount != 1 {
		t.Errorf("config.load reported %d OFF toggles, want 1", offCount)
	}
}

// -- tiny test scaffolding -------------------------------------------------

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestEnableWebLaneBundle pins the edge-only web-lane coupling (capability-enhancement T1.1/T1.4):
// opting into web_search pulls in query_formulation + fetch_url (the validated lane), but it is a
// strict no-op when web_search is off — byte-identical to today, and the other two are never forced on
// independently.
func TestEnableWebLaneBundle(t *testing.T) {
	// web_search OFF (the default) -> the bundle is a no-op: nothing is forced on.
	off := New()
	off.Subconscious.WebSearch = false
	off.Subconscious.QueryFormulation = false
	off.Subconscious.FetchURL = false
	off.EnableWebLaneBundle()
	if off.Subconscious.QueryFormulation || off.Subconscious.FetchURL {
		t.Fatalf("bundle must be a no-op when web_search is OFF; got qf=%v fetch=%v",
			off.Subconscious.QueryFormulation, off.Subconscious.FetchURL)
	}
	// web_search ON -> the two validated improvements ride along.
	on := New()
	on.Subconscious.WebSearch = true
	on.Subconscious.QueryFormulation = false
	on.Subconscious.FetchURL = false
	on.EnableWebLaneBundle()
	if !on.Subconscious.QueryFormulation || !on.Subconscious.FetchURL {
		t.Fatalf("web_search ON must bundle query_formulation + fetch_url; got qf=%v fetch=%v",
			on.Subconscious.QueryFormulation, on.Subconscious.FetchURL)
	}
	// nil receiver is safe (no panic).
	var nilCfg *HarnessConfig
	nilCfg.EnableWebLaneBundle()
}
