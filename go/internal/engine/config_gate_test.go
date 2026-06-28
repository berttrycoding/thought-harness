package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// newConfiguredEngine builds a heuristic engine with an explicit feature config + an event recorder
// that counts config.skip / config.toggle by component/path. Returns the engine and a getter for the
// per-component skip counts.
func newConfiguredEngine(t *testing.T, feat *config.HarnessConfig) (*Engine, func() map[string]int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	skips := map[string]int{}
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.ConfigSkip {
			if comp, ok := ev.Data["component"].(string); ok {
				skips[comp]++
			}
		}
	})
	return e, func() map[string]int { return skips }
}

// TestAllOnEmitsNoConfigSkip is the byte-identical-default guard: with Features=AllOn (the same as
// Features=nil), a full episode emits ZERO config.skip — every component runs, nothing is bypassed.
func TestAllOnEmitsNoConfigSkip(t *testing.T) {
	feat := config.New() // AllOn
	e, getSkips := newConfiguredEngine(t, feat)
	e.SubmitDefault("what is 2 + 2?")
	e.Run(40)
	if got := getSkips(); len(got) != 0 {
		t.Fatalf("AllOn config must emit no config.skip, got %v", got)
	}
	if e.LastResponse() == "" {
		t.Fatal("AllOn episode should still deliver an answer")
	}
}

// TestNilFeaturesEqualsAllOn confirms Features=nil resolves to AllOn (no config.skip), the contract
// that keeps scenario goldens byte-identical.
func TestNilFeaturesEqualsAllOn(t *testing.T) {
	e, getSkips := newConfiguredEngine(t, nil)
	if e.Features() == nil {
		t.Fatal("Features=nil must resolve to a non-nil AllOn config on the engine")
	}
	e.SubmitDefault("what is 2 + 2?")
	e.Run(40)
	if got := getSkips(); len(got) != 0 {
		t.Fatalf("Features=nil (AllOn) must emit no config.skip, got %v", got)
	}
}

// TestDisabledComponentShortCircuits drives an episode with several engine-loop toggles OFF and
// asserts each disabled component emits config.skip (bypass, not delete) while the episode still
// completes — the M1 DoD's "flipping a toggle OFF makes that component short-circuit to pass-through".
// Dispatch stays ON here (the seam-stage bypasses are exercised in TestSeamStageBypass, since the
// Gate/Transform decision points are only reached when dispatch actually produces survivors).
func TestDisabledComponentShortCircuits(t *testing.T) {
	feat := config.New()
	feat.Value.Signal = false      // no rerank
	feat.Regulator.Enforce = false // no theta control
	feat.Subconscious.Dispatch = false
	feat.Memory.Recall = false
	feat.Convert.PrimitiveSubAgentMint = false
	feat.Convert.SkillMint = false
	feat.Convert.GatePriorMint = false
	feat.Convert.Facts = false // A-RAG5 (DEFAULT-ON 2026-06-21) is also a consolidation input — disable it too to bypass the pass
	feat.Validate()

	e, getSkips := newConfiguredEngine(t, feat)
	e.SubmitDefault("design, build and validate a new login endpoint")
	e.Run(60)

	skips := getSkips()
	// every disabled component reached on the hot path must have announced its bypass at least once.
	want := []string{
		"value.signal", "regulator.enforce", "subconscious.dispatch",
		"memory.recall", "convert.consolidate",
	}
	for _, comp := range want {
		if skips[comp] == 0 {
			t.Errorf("disabled %q must emit at least one config.skip; skips=%v", comp, skips)
		}
	}
	// the episode still completes — a toggle bypasses a decision, it does not break the loop.
	if e.LastResponse() == "" {
		t.Fatal("episode with toggles OFF should still deliver an answer (bypass, not delete)")
	}
}

// TestSeamStageBypass exercises the three hidden-seam stage bypasses with dispatch ON (so the seam
// actually has survivors to admit/arbitrate/voice). Filter OFF ⇒ admit-all, Gate OFF ⇒ rank-identity,
// Transform OFF ⇒ raw relay — each must emit config.skip and the episode must still answer.
func TestSeamStageBypass(t *testing.T) {
	feat := config.New()
	feat.Seam.HiddenFilter = false
	feat.Seam.HiddenGate = false
	feat.Seam.HiddenTransform = false
	// dispatch + specialists ON so the seam sees candidates.
	feat.Validate()

	e, getSkips := newConfiguredEngine(t, feat)
	// a goal that fires specialists (arithmetic) so the seam admits/arbitrates a survivor.
	e.SubmitDefault("what is 7 times 8?")
	e.Run(60)

	skips := getSkips()
	for _, comp := range []string{"seam.filter", "seam.gate", "seam.transform"} {
		if skips[comp] == 0 {
			t.Errorf("disabled %q must emit at least one config.skip; skips=%v", comp, skips)
		}
	}
	if e.LastResponse() == "" {
		t.Fatal("seam-bypassed episode should still deliver an answer")
	}
}

// TestLiveToggleNoRebuild asserts a live ApplyFeatureToggle is observed on the next tick with no
// engine reconstruction (the shared-pointer contract that fixes the live-session data-loss bug).
func TestLiveToggleNoRebuild(t *testing.T) {
	e, getSkips := newConfiguredEngine(t, config.New())
	e.SubmitDefault("what is 2 + 2?")
	e.Step() // start the episode (all-on so far)
	// flip the Filter OFF live (no rebuild).
	if !e.ApplyFeatureToggle("seam.hidden_filter", false) {
		t.Fatal("ApplyFeatureToggle(seam.hidden_filter,false) should succeed")
	}
	if e.Features().Seam.HiddenFilter {
		t.Fatal("the live flip must mutate the shared config in place")
	}
	for i := 0; i < 40; i++ {
		res := e.Step()
		if res.Idle && !e.PortPending() {
			break
		}
	}
	if getSkips()["seam.filter"] == 0 {
		t.Error("after a live flip, the Filter must short-circuit and emit config.skip with no rebuild")
	}
}

// TestApplyFeatureToggleRejectsUnknown asserts a bad path is rejected (ok=false), not silently
// applied.
func TestApplyFeatureToggleRejectsUnknown(t *testing.T) {
	e, _ := newConfiguredEngine(t, config.New())
	if e.ApplyFeatureToggle("no.such.toggle", false) {
		t.Error("ApplyFeatureToggle on an unknown path must return false")
	}
	if e.ApplyFeatureToggle("subconscious.max_par_width", false) {
		t.Error("ApplyFeatureToggle on a non-bool knob must return false")
	}
}

// legiblePromptBackend embeds the test double and records the fragment the engine SETS on it (so the
// test can assert the Generate-prompt side of the legible instrument without a live model).
type legiblePromptBackend struct {
	*backends.TestBackend
	lastFragment string
}

func (b *legiblePromptBackend) SetLegibleFragment(fragment string) { b.lastFragment = fragment }

var _ backends.LegiblePrompter = (*legiblePromptBackend)(nil)

// TestLegibleFragmentGatedByToggle: the engine appends the registry-derived control-tag fragment to the
// Generate prompt ONLY when seam.legible_generation is ON, and clears it ("") when OFF — the prompt side
// of the WF-E CC-1 SHADOW instrument. Default OFF ⇒ no fragment ⇒ byte-identical Generate prompt.
func TestLegibleFragmentGatedByToggle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = config.New() // AllOn ⇒ seam.legible_generation OFF (the opt-in exception)
	be := &legiblePromptBackend{TestBackend: backends.NewTest()}
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// default OFF: an open-ended goal drives conscious generate(), which must CLEAR the fragment.
	be.lastFragment = "sentinel-off" // prove generate() overwrites it to ""
	e.SubmitDefault("brainstorm names for a new coffee shop")
	for i := 0; i < 30; i++ {
		res := e.Step()
		if res.Idle && !e.PortPending() {
			break
		}
	}
	if be.lastFragment != "" {
		t.Errorf("with the instrument OFF the Generate fragment must be cleared, got %q", be.lastFragment)
	}

	// flip ON live (no rebuild): a fresh episode's generate ticks must set the registry-derived fragment.
	if !e.ApplyFeatureToggle("seam.legible_generation", true) {
		t.Fatal("ApplyFeatureToggle(seam.legible_generation,true) should succeed")
	}
	be.lastFragment = "sentinel-on" // prove the next generate tick overwrites it with the real fragment
	e.SubmitDefault("brainstorm names for a new tea house")
	for i := 0; i < 30; i++ {
		res := e.Step()
		if res.Idle && !e.PortPending() {
			break
		}
	}
	if be.lastFragment == "" || be.lastFragment == "sentinel-on" {
		t.Fatalf("with the instrument ON the engine must set a non-empty registry fragment, got %q", be.lastFragment)
	}
	// the fragment IS the contract prompt (one source of truth) — it offers the novel: escape + a known op.
	if !containsSub(be.lastFragment, "op=") || !containsSub(be.lastFragment, "novel:") {
		t.Errorf("the fragment must be the legibility contract prompt (op=/novel:), got %q", be.lastFragment)
	}
}

// TestLegibleDefaultEmitsNoConfigSkip is the byte-identical guard for the opt-in instrument: an all-on
// episode (legible OFF by default) emits ZERO config.skip for seam.legible_generation — an opt-in
// instrument sitting at its OFF baseline never announces a bypass (that would change the trace).
func TestLegibleDefaultEmitsNoConfigSkip(t *testing.T) {
	e, getSkips := newConfiguredEngine(t, config.New())
	e.SubmitDefault("what is 7 times 8?")
	e.Run(60)
	if n := getSkips()["seam.legible_generation"]; n != 0 {
		t.Fatalf("the opt-in legible instrument must emit no config.skip when off, got %d", n)
	}
}

// containsSub is a tiny substring check (the engine test package keeps imports minimal).
func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
