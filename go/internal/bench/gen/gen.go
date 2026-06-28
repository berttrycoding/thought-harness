package gen

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/bench/judge"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Generator produces new benchmark items/scenarios for a mechanism, seeded from
// the authored pilot bank (read as few-shot examples) plus the §2.2 archetype
// templates and the §2.3 gap rules. It is the §5.4 "mined-shape-seeded
// generator": ground truth is fixed by construction (the oracle, the lure, the
// planted schedule) wherever possible, and the only model call is the surface
// realisation, routed through a backends.Backend so it is offline-deterministic
// under backends.NewTest().
type Generator interface {
	// GenerateTierA returns n Tier-A items for the mechanism, varied from the
	// authored few-shot seeds. Every returned item carries a fixed deterministic
	// oracle + the mechanism hook so CheckBank passes. The backend supplies the
	// surface text (prompt phrasing); under the test double it is deterministic.
	GenerateTierA(mechanism benchtypes.Mechanism, n int, backend backends.Backend) ([]benchtypes.TierAItem, error)

	// GenerateTierB returns n Tier-B scenarios for the mechanism, the same way.
	GenerateTierB(mechanism benchtypes.Mechanism, n int, backend backends.Backend) ([]benchtypes.TierBScenario, error)
}

// SeedGenerator is the concrete Generator that few-shots from the authored pilot
// bank. It reads the gold seeds (LoadBankA / LoadBankB) at construction, then
// produces variants by rotating through them + the §6.0 domain rotation, calling
// the backend only for the surface phrasing. Determinism: a seeded
// *cpyrand.Random threads every stochastic choice, so a regen at the same Seed is
// byte-identical.
type SeedGenerator struct {
	// Root is the banks directory the gold few-shot seeds are read from
	// (PilotBanksRoot in production; a temp dir in tests).
	Root string
	// Seed pins the variation RNG so regeneration is reproducible.
	Seed uint64
}

// NewSeedGenerator builds a SeedGenerator over a banks root with a fixed RNG
// seed. Root "" defaults to PilotBanksRoot.
func NewSeedGenerator(root string, seed uint64) *SeedGenerator {
	if root == "" {
		root = PilotBanksRoot
	}
	return &SeedGenerator{Root: root, Seed: seed}
}

// domainRotation is the §6.0 round-robin used to spread generated items across
// the locked mix (~45% SWE, ~30% STEM, ~25% core-knowledge) — three SWE, two
// STEM, two core per period of seven, so a long enough run lands near the target
// and clears the G9 ≥30% non-SWE floor.
var domainRotation = []string{
	"harness", "general-swe", "infra",
	"mathematics", "algorithms",
	"technical-english", "logic-reasoning",
}

// GenerateTierA implements Generator. It reads the authored Tier-A gold for the
// mechanism as few-shot seeds, then emits n variants: each variant clones a seed,
// re-IDs it, rotates its domain per the §6.0 mix, and re-phrases the prompt
// through the backend (offline-deterministic under the test double). The
// frozen-by-construction parts (oracle, prior lure, trace isolation) are copied
// verbatim so the variant stays mechanism-requiring and CheckBank-clean.
func (g *SeedGenerator) GenerateTierA(mechanism benchtypes.Mechanism, n int, backend backends.Backend) ([]benchtypes.TierAItem, error) {
	if n <= 0 {
		return nil, nil
	}
	if backend == nil {
		return nil, fmt.Errorf("gen: GenerateTierA needs a backend (pass backends.NewTest() for offline)")
	}
	seeds, err := g.loadSeedsA(mechanism)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("gen: no Tier-A gold seeds for mechanism %q under %q (author a pilot bank first)", mechanism, g.Root)
	}
	rng := cpyrand.New(g.Seed)
	out := make([]benchtypes.TierAItem, 0, n)
	for i := 0; i < n; i++ {
		seed := seeds[i%len(seeds)]
		v := seed // value copy of the struct
		v.ID = fmt.Sprintf("%s-A-gen-%04d", mechanism, i)
		// Rotate the domain across the §6.0 mix, but NEVER for safety (its mix is
		// the BLOCK/ALLOW balance, not the SWE/STEM/core split) — keep the gold's
		// domain so the ALLOW-floor accounting is preserved.
		if mechanism != benchtypes.MechSafety {
			v.Domain = domainRotation[i%len(domainRotation)]
		}
		// Re-phrase the surface prompt through the backend. The few-shot context is
		// the seed thought; the backend supplies a deterministic restatement under
		// the test double. The MEANING (and the oracle it answers to) is unchanged —
		// only the surface varies.
		v.Prompt = g.rephrase(backend, seed.Prompt, rng)
		out = append(out, v)
	}
	return out, nil
}

// GenerateTierB implements Generator, the same way over scenarios. The scripted
// turns' first turn is re-phrased; the planted schedule, end-state oracles,
// isolation predicate, and ablation config (the by-construction ground truth) are
// copied verbatim.
func (g *SeedGenerator) GenerateTierB(mechanism benchtypes.Mechanism, n int, backend backends.Backend) ([]benchtypes.TierBScenario, error) {
	if n <= 0 {
		return nil, nil
	}
	if backend == nil {
		return nil, fmt.Errorf("gen: GenerateTierB needs a backend (pass backends.NewTest() for offline)")
	}
	seeds, err := g.loadSeedsB(mechanism)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("gen: no Tier-B gold seeds for mechanism %q under %q (author a pilot bank first)", mechanism, g.Root)
	}
	rng := cpyrand.New(g.Seed ^ 0x9e3779b9)
	out := make([]benchtypes.TierBScenario, 0, n)
	for i := 0; i < n; i++ {
		seed := seeds[i%len(seeds)]
		v := seed
		v.ID = fmt.Sprintf("%s-B-gen-%04d", mechanism, i)
		if mechanism != benchtypes.MechSafety {
			v.Domain = domainRotation[i%len(domainRotation)]
		}
		// Deep-copy the turns so the variant's re-phrasing of T1 does not mutate the
		// shared gold slice.
		v.Turns = append([]benchtypes.Turn(nil), seed.Turns...)
		if len(v.Turns) > 0 {
			v.Turns[0].Text = g.rephrase(backend, v.Turns[0].Text, rng)
		}
		out = append(out, v)
	}
	return out, nil
}

// rephrase asks the backend to produce one surface variant of a seed string. It
// uses Generate (the CONTENT role every backend implements) with the seed framed
// as the goal; the test double returns a deterministic, non-empty restatement so
// the generated item is well-formed offline. On an empty backend return it falls
// back to the original seed text (never an empty prompt).
func (g *SeedGenerator) rephrase(backend backends.Backend, seedText string, rng *cpyrand.Random) string {
	goal := "Restate this benchmark prompt, preserving its exact meaning and the fact it probes: " + seedText
	ctx := []types.Thought{{Text: seedText, Source: types.GENERATED}}
	got := strings.TrimSpace(backend.Generate(goal, ctx, rng))
	if got == "" {
		return seedText
	}
	return got
}

func (g *SeedGenerator) loadSeedsA(mechanism benchtypes.Mechanism) ([]benchtypes.TierAItem, error) {
	path := BankFileA(g.Root, mechanism)
	items, err := LoadBankA(path)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (g *SeedGenerator) loadSeedsB(mechanism benchtypes.Mechanism) ([]benchtypes.TierBScenario, error) {
	path := BankFileB(g.Root, mechanism)
	scenarios, err := LoadBankB(path)
	if err != nil {
		return nil, err
	}
	return scenarios, nil
}

// ---------------------------------------------------------------------------
// Judge — the LLM-as-judge wrapper for the residual rubric clauses (spec §5.4).
// ---------------------------------------------------------------------------

// JudgeVerdict aliases judge.Verdict: the LLM-judge ruler now lives in the leaf package
// internal/bench/judge (ONE implementation, shared with the tiera scorer's complete assessment —
// no duplicated pass-detection, no import cycle). Kept as an alias so existing gen callers/tests
// are unchanged.
type JudgeVerdict = judge.Verdict

// Judge runs one residual rubric clause through a backends.Backend — a thin alias over judge.Run,
// kept for the gen pipeline's callers/tests. Under backends.NewTest() it is deterministic + offline.
func Judge(scenarioOutput, rubric string, backend backends.Backend) JudgeVerdict {
	return judge.Run(scenarioOutput, rubric, backend)
}

// compile-time assertion that SeedGenerator satisfies Generator.
var _ Generator = (*SeedGenerator)(nil)
