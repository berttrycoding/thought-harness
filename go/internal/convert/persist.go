// persist.go is the cross-session persistence of convertibility's LEARNED state (the representation-
// space rebuild, M4): the compiled GATE PRIORS (a standing per-domain bias) and the MINTED SPECIALISTS
// (a repeated GENERATED pattern compiled into an automatic injection). Before M4 these evaporated on
// exit — convert held them in memory with no Save/Load (the audit's "gate priors: no persistence code
// at all"). Here they round-trip as plain-data records the engine's injected persist.Store writes/reads.
//
// Tier discipline: convert depends on subconscious (Tier 2) but does NO file I/O — it only exports
// plain-data records and re-applies them. The engine's persist.Store (a higher tier) does the disk I/O,
// so convert stays a pure learner. Never-fabricate is preserved: a minted specialist carries the REAL
// worked answer captured when its pattern converged (convert.Observe), never a fabricated template, so
// re-seeding it injects only what reality actually produced.
package convert

import "github.com/berttrycoding/thought-harness/internal/subconscious"

// SpecialistRecord is the plain-data round-trip form of one minted specialist. The engine maps it to/
// from its persist.Store record (the curator metadata — Version/Hash/UseCount — lives on the Store
// record, not here; convert only owns the cognition payload). Demoted survives so a keep-or-revert
// reversion is not silently un-done across a restart (a refuted reflex stays refuted).
type SpecialistRecord struct {
	Domain    string   // the minted specialist's domain tag ("learned:<goalkey>")
	GoalKey   string   // the pattern's coarse goal key (so the bookkeeping reconstructs)
	Triggers  []string // the context words that light it up
	Answer    string   // the REAL worked answer it injects (captured at convergence; never fabricated)
	Relevance float64  // the standing relevance it fires at
	Generated int      // effortful repeats that earned the mint (kept so the gate stays consistent)
	Value     float64  // the grounded value it converged on (the mint basis)
	Demoted   bool     // reverted by keep-or-revert (a refuted reflex stays refuted across a restart)
}

// ProgramRunRecord is the plain-data round-trip form of one recurring synthesised-program run
// (convert.programRun): the per-goalKey COUNT of how often a control-flow Shape was synthesised, plus
// the Program body so a re-loaded run can re-mint the same skill once it crosses MintAfter. It is the
// durable trace->skill recurrence counter — without it the count resets to 1 every fresh engine
// (ProbeReplays / per-episode engines), so the >= MintAfter mint never fires. The engine maps it to/from
// its persist.Store ProgramRunRecord (the curator metadata lives on the Store record, not here; convert
// owns only the recurrence payload). Mirrors SpecialistRecord's shape.
type ProgramRunRecord struct {
	GoalKey string  // the coarse per-goal key (so the tally reconstructs under the same key)
	Shape   string  // the control-flow shape signature (so a shape change resets the count, as live)
	Count   int     // how often this shape was synthesised for this goal (mints at >= MintAfter)
	Minted  bool    // whether the recurring program already minted a skill (so a re-run doesn't re-mint)
	Program Program // the Program body (Shape()-only port; the engine seeds the concrete *cognition.Program)
}

// ExportProgramRuns returns the recurring synthesised-program runs as plain-data records, in insertion
// order (programOrder), so a restart re-seeds them in the same deterministic order. Every tracked run is
// exported (not only minted ones) — the WHOLE POINT is to persist an in-progress count (1, 2) so it can
// reach MintAfter across episodes; an un-minted candidate IS the durable state here. A run with no
// captured Program (defensive: should not happen) is skipped (it cannot re-mint).
func (c *Convertibility) ExportProgramRuns() []ProgramRunRecord {
	var out []ProgramRunRecord
	for _, key := range c.programOrder {
		r := c.programRuns[key]
		if r == nil || r.program == nil {
			continue
		}
		out = append(out, ProgramRunRecord{
			GoalKey: key,
			Shape:   r.shape,
			Count:   r.count,
			Minted:  r.minted,
			Program: r.program,
		})
	}
	return out
}

// SeedProgramRuns re-loads persisted recurring-program counts into the live tally at construction
// (before the first episode), so a fresh-engine episode RESUMES the count instead of resetting to 1.
// Additive over the (empty) live map; a duplicate goalKey is skipped (keep the live one). The seeded
// run carries its Program body so a later NoteProgram that pushes it past MintAfter mints the SAME
// skill. It NEVER mints here (Consolidate owns minting) — it only restores the counter state.
func (c *Convertibility) SeedProgramRuns(records []ProgramRunRecord) {
	for _, rec := range records {
		if rec.Program == nil {
			continue
		}
		key := rec.GoalKey
		if key == "" {
			continue
		}
		if _, present := c.programRuns[key]; present {
			continue // already tracked this run (a re-entered goal) — keep the live one
		}
		c.programRuns[key] = &programRun{
			shape:   rec.Shape,
			program: rec.Program,
			count:   rec.Count,
			minted:  rec.Minted,
		}
		c.programOrder = append(c.programOrder, key)
	}
}

// ExportGatePriors returns a copy of the compiled gate priors (domain -> +bias) so the engine can
// persist them. A copy, so the caller cannot mutate the live map.
func (c *Convertibility) ExportGatePriors() map[string]float64 {
	out := make(map[string]float64, len(c.GatePrior))
	for d, v := range c.GatePrior {
		out[d] = v
	}
	return out
}

// ExportSpecialists returns the minted specialists as plain-data records, in mint order (patternOrder),
// so a restart re-seeds them in the same deterministic order. Only patterns that actually minted a
// specialist are exported (an un-minted candidate is still being earned and is not durable state).
func (c *Convertibility) ExportSpecialists() []SpecialistRecord {
	var out []SpecialistRecord
	for _, key := range c.patternOrder {
		p := c.patterns[key]
		if p == nil || !p.minted || p.psa == nil {
			continue
		}
		out = append(out, SpecialistRecord{
			Domain:    p.psa.Domain(),
			GoalKey:   p.goalKey,
			Triggers:  p.psa.Triggers(),
			Answer:    p.psa.Answer(),
			Relevance: p.psa.RelevanceValue(),
			Generated: p.generated,
			Value:     p.groundedVal,
			Demoted:   p.demoted,
		})
	}
	return out
}

// SeedGatePriors merges persisted gate priors into the live map at construction (before the first
// episode). It is additive over the (empty) live map, so a re-loaded prior is exactly the saved bias.
func (c *Convertibility) SeedGatePriors(priors map[string]float64) {
	for d, v := range priors {
		c.GatePrior[d] = v
		if _, seen := c.domainTally[d]; !seen {
			c.domainOrder = append(c.domainOrder, d)
		}
	}
}

// SeedPrimitiveSubAgents re-registers persisted minted specialists into the subconscious roster and rebuilds
// the pattern bookkeeping so keep-or-revert still applies on the next run (a re-encounter below the
// floor still reverts a re-loaded mint). It NEVER re-emits a mint event (the mint already happened on a
// prior run) — it restores state silently. A demoted record is re-registered already Demote()d, so a
// refuted reflex stays dark. Idempotent on the domain (a duplicate record is skipped).
func (c *Convertibility) SeedPrimitiveSubAgents(records []SpecialistRecord) {
	for _, rec := range records {
		key := rec.GoalKey
		if key == "" {
			key = goalKey(rec.Domain)
		}
		if p := c.patterns[key]; p != nil && p.minted {
			continue // already present (a duplicate / re-entered pattern) — keep the live one
		}
		sp := subconscious.NewMintedPrimitiveSubAgent(rec.Domain, rec.Triggers, rec.Answer, rec.Relevance)
		if rec.Demoted {
			sp.Demote()
		}
		c.subconscious.Register(sp)
		p := c.patterns[key]
		if p == nil {
			p = &pattern{goalKey: key, triggers: rec.Triggers}
			c.patterns[key] = p
			c.patternOrder = append(c.patternOrder, key)
		}
		p.psa = sp
		p.answer = rec.Answer
		p.generated = rec.Generated
		p.groundedVal = rec.Value
		p.minted = true
		p.demoted = rec.Demoted
		if !containsStr(c.Minted, rec.Domain) {
			c.Minted = append(c.Minted, rec.Domain)
		}
		if rec.Demoted && !containsStr(c.Demoted, rec.Domain) {
			c.Demoted = append(c.Demoted, rec.Domain)
		}
	}
}

// containsStr reports whether s is in xs (the dedup guard for re-seeded Minted/Demoted lists).
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
