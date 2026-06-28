package engine_test

// self_model_property_test.go — cognition-property tests for the BASELINE DECLARATIVE SELF-MODEL
// (self_model.go; board SELF-MODEL; docs/internal/notes/2026-06-21-preagi-harness-levels-roadmap.md §1.5).
//
// These pin the THINKING the self-model is meant to enable, not just the plumbing:
//   - the awake mind GROUNDS a STANDING CORE self-description (its identity + a bounded capability index +
//     runtime facts) into the conscious stream when a standing INTROSPECTIVE root holds focus — read from
//     the REAL registries, not a hardcoded list (the registry-read contract: a minted specialist moves the
//     index + re-fires);
//   - it is STANDING (refreshes on a content-HASH change), not resume-once and not per-tick spam;
//   - the per-capability DETAIL is LAZILY retrievable on demand (SelfModelLookup), never eagerly dumped;
//   - DEFAULT OFF ⇒ byte-identical (no self-model thought, no perception.self_model event);
//   - BOUNDED: the grounding is a single percept append, never a fork (n unchanged — the durability claim
//     the #18 self-watch cell measures directly).
//
// Deterministic: continuous mode on the TestBackend double + cpyrand seed=7; the faculty scheduler +
// full seed-intent portfolio give the standing introspective root fair-share focus turns, and the core
// text is a fixed template over read registry counts + config (no clock/RNG/backend ⇒ byte-stable).

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// selfModelAwakeFeatures builds the awake stack that gives a standing INTROSPECTIVE root focus turns and
// turns the self-model on: seed-intent portfolio (plants the introspective "Self-monitor" root) + the
// faculty scheduler (fair-share so the introspective root is resumed) + sense.self_model.
func selfModelAwakeFeatures(on bool) *config.HarnessConfig {
	feat := config.New() // AllOn baseline
	a := &feat.Conscious.Activity
	a.SeedIntents = true
	a.SeedIntentCount = cognition.SeedPortfolioSize() // full portfolio ⇒ the introspective root is planted
	a.FacultyScheduler = true                         // fair-share so the introspective root gets focus turns
	feat.Sense.SelfModel = on                         // the SELF-MODEL opt-in knob
	feat.Validate()
	return feat
}

// TestSelfModelGroundsStandingCoreFromRegistries is the SELF-MODEL cognition-property test: with the flag
// ON, the awake mind grounds a STANDING CORE self-description — read from the REAL registries — into the
// conscious stream when the standing introspective root holds focus, with NO user prompt.
func TestSelfModelGroundsStandingCoreFromRegistries(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, selfModelAwakeFeatures(true))
	for i := 0; i < 40; i++ {
		eng.Step() // NO user input — the awake loop runs entirely on its own
	}

	// The DISCRIMINATING proof: a perception.self_model event fired WITHOUT a user prompt — the conscious
	// stream was TOLD what it is, the gap the live-observed "I'm an LLM" fallback exposed.
	models := log.of(events.PerceptionSelfModel)
	if len(models) == 0 {
		t.Fatal("sense.self_model ON: no perception.self_model event — the awake mind never grounded its self-model on a focused introspective root")
	}

	ev := models[0]
	// REGISTRY-READ: the index must carry the REAL live counts (specialists + operators are always present),
	// not zero — proving the core was assembled by reading the registries, not a hardcoded stub.
	specs, _ := ev.Data["specialists"].(int)
	ops, _ := ev.Data["operators"].(int)
	if specs <= 0 {
		t.Fatalf("self-model grounded but specialists=%d — the core did not read the live specialist roster", specs)
	}
	if ops <= 0 {
		t.Fatalf("self-model grounded but operators=%d — the core did not read the live operator catalog", ops)
	}
	if mode, _ := ev.Data["mode"].(string); mode != "continuous" {
		t.Fatalf("self-model runtime mode=%q, want continuous (the real run mode)", mode)
	}

	// The core must be a real INJECTED thought the stream reads as its own (silent-injection voicing), not
	// just a bus metric: an appended GENERATED thought carrying the identity + the capability index.
	sawCore := false
	for _, a := range log.of(events.Append) {
		txt, _ := a.Data["text"].(string)
		src, _ := a.Data["source"].(string)
		if src == types.GENERATED.String() && strings.HasPrefix(txt, "(self-model)") {
			// Identity (what it IS, not the base model) + the capability index must both be present.
			if !strings.Contains(txt, "Silent-Injection") || !strings.Contains(txt, "three layers") {
				t.Fatalf("self-model core lacks its IDENTITY (architecture): %q", txt)
			}
			if !strings.Contains(txt, "specialists") || !strings.Contains(txt, "operators") {
				t.Fatalf("self-model core lacks its CAPABILITY INDEX: %q", txt)
			}
			// The LAZY/RAG contract must be legible: detail is retrievable, not carried.
			if !strings.Contains(txt, "retrievable on demand") {
				t.Fatalf("self-model core does not signal that detail is lazily retrievable: %q", txt)
			}
			sawCore = true
			break
		}
	}
	if !sawCore {
		t.Fatal("sense.self_model ON: the standing core was never injected as a GENERATED thought — the mind never read its own self-model into the stream")
	}

	// BOUNDED: grounding the self-model must NOT fork. The seed-intent root branches are the seeding count;
	// a self-model that forked would balloon the branch set past the portfolio size.
	seededRoots := 0
	for _, b := range eng.Graph().Branches {
		if b.Reason != nil && strings.HasPrefix(*b.Reason, "seed-intent:") {
			seededRoots++
		}
	}
	if seededRoots > cognition.SeedPortfolioSize() {
		t.Fatalf("self-model ON: %d seed-intent root branches exceed the portfolio size %d — grounding the self-model forked (it must be a bounded single append, no fan-out)",
			seededRoots, cognition.SeedPortfolioSize())
	}
}

// TestSelfModelIsStandingNotPerTick pins the STANDING-REFRESH contract: an UNCHANGED self-model is grounded
// ONCE, not re-injected every introspective focus turn (which would spam the stream). Over a long awake run
// with a static roster the self-model fires a SMALL bounded number of times (one per content change), not
// once per tick.
func TestSelfModelIsStandingNotPerTick(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, selfModelAwakeFeatures(true))
	for i := 0; i < 60; i++ {
		eng.Step()
	}
	groundings := len(log.of(events.PerceptionSelfModel))
	if groundings == 0 {
		t.Fatal("self-model never grounded over 60 ticks — the standing introspective root never held focus")
	}
	// The roster is static over the run, so the self-model is grounded ONCE (one content hash). A few is
	// tolerable (a transient registry change), but it must be FAR below the introspective-focus turn count
	// — never per-tick. A generous ceiling: it must not have fired every few ticks.
	if groundings > 5 {
		t.Fatalf("self-model grounded %d times over 60 ticks with a static roster — it is re-injecting per focus turn, not standing on a content-hash (spam)", groundings)
	}
}

// TestSelfModelRegistryReadMovesWhenRosterGrows is the registry-read PROOF the spec demands: adding a
// specialist in a NEW domain CHANGES the standing index (count + domain set) AND re-fires the standing
// refresh (the content hash moved). A hardcoded self-model would not move when the roster grows.
func TestSelfModelRegistryReadMovesWhenRosterGrows(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, selfModelAwakeFeatures(true))

	// Run until the first self-model grounding (the introspective root takes a few ticks to get focus).
	first := -1
	for i := 0; i < 40 && first < 0; i++ {
		eng.Step()
		if len(log.of(events.PerceptionSelfModel)) > 0 {
			first = i
		}
	}
	models := log.of(events.PerceptionSelfModel)
	if len(models) == 0 {
		t.Fatal("self-model never grounded before the roster change — cannot prove the registry read moves")
	}
	beforeSpecs, _ := models[0].Data["specialists"].(int)
	beforeDomains, _ := models[0].Data["domains"].([]string)
	beforeHash, _ := models[0].Data["hash"].(string)

	// ADD a specialist in a brand-new domain via the LIVE registry — the roster GROWS (the minting case the
	// lazy/RAG design exists for). buildSelfModel reads Subconscious().Specialists(), so this MUST move the
	// index + the hash.
	sub := eng.Subconscious()
	newDomain := "telemetry-xyz" // a domain not in the seed roster, so it adds a NEW category
	grown := append(sub.Specialists(),
		subconscious.NewMintedPrimitiveSubAgent(newDomain, []string{"telemetry"}, "telemetry reading", 0.9))
	sub.SetPrimitiveSubAgents(grown)

	// Run on until the self-model re-grounds (the hash changed ⇒ the standing refresh fires).
	groundedAfter := false
	for i := 0; i < 40 && !groundedAfter; i++ {
		eng.Step()
		after := log.of(events.PerceptionSelfModel)
		last := after[len(after)-1]
		if h, _ := last.Data["hash"].(string); h != beforeHash {
			afterSpecs, _ := last.Data["specialists"].(int)
			afterDomains, _ := last.Data["domains"].([]string)
			if afterSpecs != beforeSpecs+1 {
				t.Fatalf("added 1 specialist but the index count went %d -> %d (want +1) — the core did not re-read the live roster",
					beforeSpecs, afterSpecs)
			}
			if !contains(afterDomains, newDomain) {
				t.Fatalf("added a specialist in domain %q but the index domain set %v does not include it — the core did not re-read the live roster",
					newDomain, afterDomains)
			}
			if len(afterDomains) != len(beforeDomains)+1 {
				t.Fatalf("added a NEW domain but the domain-category set went %d -> %d (want +1)", len(beforeDomains), len(afterDomains))
			}
			groundedAfter = true
		}
	}
	if !groundedAfter {
		t.Fatal("grew the roster but the self-model never re-grounded with a changed hash — the standing index did NOT track the live registry (the registry-read contract is broken)")
	}
}

// TestSelfModelLazyLookupRetrievesDetailFromRegistries pins the LAZY/RAG half: the standing core carries the
// bounded index, but the per-capability DETAIL is PULLED on demand from the real registries — a tool's
// signature, a specialist's competence, an operator's intent, a runtime fact — and a miss returns honestly
// (never a fabricated answer).
func TestSelfModelLazyLookupRetrievesDetailFromRegistries(t *testing.T) {
	eng, _ := newContinuousEngineWithFeatures(t, selfModelAwakeFeatures(true))
	eng.Step() // open the boot episode so the registries are live

	// SPECIALIST competence — "compute" is a base specialist domain; the on-demand detail names it.
	if detail, ok := eng.SelfModelLookup("compute"); !ok || !strings.Contains(detail, "compute") {
		t.Fatalf("lazy lookup of a specialist domain failed: ok=%v detail=%q", ok, detail)
	}

	// OPERATOR intent — "decompose" is a seed operator; the on-demand detail names it + its intent.
	if detail, ok := eng.SelfModelLookup("decompose"); !ok || !strings.Contains(detail, "operator decompose") {
		t.Fatalf("lazy lookup of an operator failed: ok=%v detail=%q", ok, detail)
	}

	// RUNTIME fact — "where am I running" resolves to the working directory / substrate runtime fact.
	if detail, ok := eng.SelfModelLookup("substrate model"); !ok || !strings.Contains(detail, "substrate") {
		t.Fatalf("lazy lookup of a runtime fact failed: ok=%v detail=%q", ok, detail)
	}

	// A genuine MISS returns honestly — never a fabricated answer (the never-fabricate floor).
	if detail, ok := eng.SelfModelLookup("quantum chromodynamics"); ok {
		t.Fatalf("lazy lookup of an unknown query fabricated a detail instead of missing: %q", detail)
	}
}

// TestSelfModelOffIsByteIdentical pins the DEFAULT-OFF byte-identical contract: with the same awake stack
// but sense.self_model OFF, the awake loop fires NO perception.self_model event and injects NO self-model
// thought — nothing about the stream changes unless the knob is flipped on.
func TestSelfModelOffIsByteIdentical(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, selfModelAwakeFeatures(false)) // the knob OFF (the default)
	for i := 0; i < 40; i++ {
		eng.Step()
	}
	if n := len(log.of(events.PerceptionSelfModel)); n != 0 {
		t.Fatalf("sense.self_model OFF: must emit no perception.self_model events, got %d (not byte-identical)", n)
	}
	for _, a := range log.of(events.Append) {
		txt, _ := a.Data["text"].(string)
		if strings.HasPrefix(txt, "(self-model)") {
			t.Fatal("sense.self_model OFF: a self-model thought leaked into the stream (not byte-identical)")
		}
	}
}

// contains reports whether xs holds s (a tiny test helper for the domain-set assertions).
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
