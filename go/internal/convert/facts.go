// facts.go is convertibility-ON-FACTS (A-RAG5, docs/internal/notes/2026-06-20-rag-integration-analysis.md §7.5):
// the Complementary-Learning-Systems (CLS) consolidation — McClelland/McNaughton/O'Reilly 1995 — applied
// to RETRIEVED knowledge FACTS, not just procedures. The convertibility loop already runs effortful->
// automatic on patterns (specialists), program shapes (skills), and grounded moves (triples); A-RAG5 runs
// the SAME value x frequency gate + keep-or-revert rails on knowledge facts:
//
//	hippocampus (episodic recall)  --consolidate on recall x value-->  neocortex (durable prior)
//
// A knowledge fact RECALLED on enough HIGH-VALUE conscious lines is migrated up to the durable PRIOR trust
// tier (knowledge.PriorTrust) — the HOT end of the HOT/WARM/COLD tiering, justified by recall x value, not
// age. A promoted prior whose latest line reality REFUTES is reverted (the discredited prior stops being
// trusted as a prior) — the keep-or-revert that already guards the specialist/triple mints.
//
// The consolidation BASIS is the high-value EXPERIENCE: NoteFactRecall marks a fact active this episode;
// AttributeFactValue at episode-close credits the episode's converged value to every fact that was active
// in it (and counts the episode iff it cleared the value floor — only a fact present during a SUCCESSFUL
// line consolidates, never one merely recalled often on failing lines). ConsolidateFacts promotes/reverts
// at idle, OFF the hot path, the same place the specialist/skill consolidation runs.
//
// Default OFF (convert.facts) => factsOn false => NoteFactRecall/AttributeFactValue/ConsolidateFacts are
// pure no-ops (no tally, no event, no promote) => byte-identical.
package convert

// factPriorTrust is the durable PRIOR tier A-RAG5 consolidates a fact UP to — mirrors knowledge.PriorTrust
// (0.92, the reality-observation prior). Defined here so convert stays decoupled from knowledge (the
// FactPromoter port is structural); the value is asserted equal to knowledge.PriorTrust by a cross-package
// test so the two never drift.
const factPriorTrust = 0.92

// factWarmTrust is the WARM tier a REVERTED prior is re-priced down to (the trustKnowledge default, 0.85):
// a refuted consolidation drops back to ordinary durable-knowledge standing, not below it (the fact is
// still grounded — only its PRIOR status was discredited).
const factWarmTrust = 0.85

// EnableFactConvert turns A-RAG5 convertibility-on-facts on/off (the engine wires it from the convert.facts
// knob; nothing else flips it). OFF (the default) => every fact-consolidation entry point is a no-op =>
// byte-identical. It is idempotent.
func (c *Convertibility) EnableFactConvert(on bool) { c.factsOn = on }

// NoteFactRecall marks that a durable KNOWLEDGE fact (its verbatim statement) was RECALLED on the active
// line THIS episode. The engine calls it at the rung-2 (knowledge) sourcing hit, where the verbatim
// statement is in hand. It is recorded into a per-episode set so a fact recalled MULTIPLE times in one
// episode counts ONCE — the consolidation unit is the high-value EXPERIENCE the fact was active in, not the
// recall multiplicity (the CLS basis). A no-op when facts-convert is off or the statement is empty.
func (c *Convertibility) NoteFactRecall(statement string) {
	if !c.factsOn || statement == "" {
		return
	}
	c.recalledThisEpisode[statement] = struct{}{}
}

// AttributeFactValue credits the episode's converged line value to every fact recalled this episode and
// resets the per-episode set. The engine calls it at episode close (alongside Observe) with the active
// branch's gated value. A fact counts toward consolidation (recalls++) ONLY when the episode cleared the
// value floor (cfg.MintValue) — a fact present on a FAILING line is NOT a consolidation candidate, the same
// value gate the specialist mint uses. peakValue tracks the best line it served (the mint basis); lastVal
// tracks the LATEST (keep-or-revert: a later refuting line lowers it below the floor and reverts the prior).
// A no-op when facts-convert is off.
func (c *Convertibility) AttributeFactValue(value float64) {
	if !c.factsOn {
		return
	}
	highValue := value >= c.cfg.MintValue
	for stmt := range c.recalledThisEpisode {
		f := c.factStats[stmt]
		if f == nil {
			f = &factStat{statement: stmt}
			c.factStats[stmt] = f
			c.factOrder = append(c.factOrder, stmt)
		}
		// lastVal/peakValue are updated on EVERY episode the fact was active in (so a refuting line is seen
		// by keep-or-revert); recalls (the frequency gate) increments ONLY on a high-value episode.
		if value > f.peakValue {
			f.peakValue = value
		}
		f.lastVal = value
		f.observed = true
		if highValue {
			f.recalls++
		}
	}
	// reset for the next episode (a fresh map, not clear, so a stale key can never leak across episodes).
	c.recalledThisEpisode = map[string]struct{}{}
}

// ConsolidateFacts runs the fact-consolidation pass at idle (the engine calls it from Consolidate's idle
// site, off the hot path). For each tracked fact, in insertion order (deterministic):
//
//   - keep-or-revert FIRST: a fact already PROMOTED whose LATEST line value fell below the floor (reality
//     refuted the line the prior kept serving) is REVERTED — DemoteFact re-prices it down to the WARM tier
//     and clears its Consolidated flag; its standing recall basis drops below the floor so it does not
//     immediately re-promote.
//   - then PROMOTE: a fact not yet promoted that cleared BOTH the recall frequency gate (recalls >=
//     cfg.MintAfter) AND the value gate (peakValue >= cfg.MintValue) is consolidated — Promote migrates its
//     trust up to the durable PRIOR tier (factPriorTrust) and marks it Consolidated.
//
// promoter==nil (no knowledge registry wired) or facts-convert OFF => no-op. Returns the statements
// promoted THIS pass (for the caller's logging/tests); FactsPromoted/FactsReverted accumulate the history.
func (c *Convertibility) ConsolidateFacts(promoter FactPromoter, nowTick int) []string {
	if !c.factsOn || promoter == nil {
		return nil
	}
	promotedNow := []string{}
	for _, stmt := range c.factOrder {
		f := c.factStats[stmt]
		// keep-or-revert: a promoted prior the latest line refuted is reverted (mirrors the specialist demote).
		if f.promoted && !f.reverted && f.observed && f.lastVal < c.cfg.MintValue {
			if promoter.DemoteFact(f.statement, factWarmTrust, nowTick) > 0 {
				f.reverted = true
				f.peakValue = f.lastVal // the consolidation's standing basis drops below the floor (reverted)
				c.FactsReverted = append(c.FactsReverted, f.statement)
			}
			continue
		}
		// promote: recall frequency x value cleared, not yet a prior.
		if f.promoted || f.recalls < c.cfg.MintAfter || f.peakValue < c.cfg.MintValue {
			continue
		}
		n, _, _ := promoter.Promote(f.statement, factPriorTrust, f.recalls, f.peakValue)
		if n == 0 {
			continue // the fact is gone (invalidated) or already at/above the prior tier — nothing consolidated
		}
		f.promoted = true
		c.FactsPromoted = append(c.FactsPromoted, f.statement)
		promotedNow = append(promotedNow, f.statement)
	}
	return promotedNow
}

// FactStat is the read-only view of one tracked fact's consolidation state (for the TUI/CLI/tests).
type FactStat struct {
	Statement string
	Recalls   int
	PeakValue float64
	LastVal   float64
	Promoted  bool
	Reverted  bool
}

// Facts returns the tracked fact-consolidation records in insertion order (deterministic) — a read-only
// view for the TUI registry browser / tests. Empty when facts-convert never tracked anything.
func (c *Convertibility) Facts() []FactStat {
	out := make([]FactStat, 0, len(c.factOrder))
	for _, stmt := range c.factOrder {
		f := c.factStats[stmt]
		out = append(out, FactStat{
			Statement: f.statement, Recalls: f.recalls, PeakValue: f.peakValue,
			LastVal: f.lastVal, Promoted: f.promoted, Reverted: f.reverted,
		})
	}
	return out
}
