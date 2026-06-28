// Package knowledge is the durable DOMAIN-KNOWLEDGE registry — a sibling to internal/memory, not a
// part of it (representation-space-rebuild.md §3.1). The boundary mirrors the existing memory split:
//
//	internal/memory    = AUTOBIOGRAPHICAL — what THIS system did (Episode) and came to believe from its
//	                     own grounded experience (Belief). First-person, episode-scoped. "Have I been
//	                     here before?"
//	internal/knowledge = DOMAIN KNOWLEDGE — facts/patterns/snippets true (or reusable) independent of
//	                     this system's history. Third-person, durable, SEEDABLE as well as learned.
//	                     "What do I know about this?"
//
// They share machinery — the retrieval precision floor (retrieval.ScoreRecords), the bi-temporal
// validity rule (retrieval.ValidAt), the NEVER-FABRICATE gate, and JSONL persistence — but answer
// different questions, so they are different stores. The record type + store deliberately mirror
// memory.SemanticRegistry (the proven shape) so neither re-implements ranking or validity.
//
// NEVER-FABRICATE: Record rejects an ungrounded item (returns false) — only SOURCED knowledge is
// trusted, so a fabricated "fact" can never enter the registry and later resurface as durable
// knowledge. This is the same gate memory enforces, applied to the knowledge layer.
//
// Tier-1 leaf discipline: this package imports only the retrieval leaf + the events leaf (the emit
// closure type + the new knowledge.* kinds). It does NO I/O in the hot path (persistence is in
// persist.go, called by the injected store); it never imports the engine — the engine wires it in.
package knowledge

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// Knowledge is one durable unit of domain knowledge. ValidFrom/ValidTo are seeded ticks (bi-temporal,
// mirroring memory.Belief): ValidTo==0 means "still current"; a refuted item is invalidated (ValidTo
// set), never deleted (invalidate-not-delete). Trust is the prior the sourcing ladder stamps onto a
// candidate's relevance/Filter input — seeded/ingested knowledge is high, distilled-from-one-episode
// lower.
type Knowledge struct {
	Statement string   `json:"statement"`  // the fact / pattern body / snippet text
	Kind      string   `json:"kind"`       // "fact" | "pattern" | "snippet"
	Entities  []string `json:"entities"`   // relevance keys (like Belief.Entities)
	Source    string   `json:"source"`     // PROVENANCE: "ingest:<uri>" | "reality:<tool>" | "distilled:<episode>"
	Grounded  bool     `json:"grounded"`   // NEVER-FABRICATE gate: only sourced knowledge is trusted
	Trust     float64  `json:"trust"`      // 0..1 prior; seeded knowledge high, distilled-from-one lower
	ValidFrom int      `json:"valid_from"` // bi-temporal, seeded ticks (mirrors Belief)
	ValidTo   int      `json:"valid_to"`   // 0 == currently valid; invalidate-not-delete on refutation
	// Consolidated marks a fact that convertibility-on-facts (A-RAG5) migrated up to the durable PRIOR
	// tier (CLS hippocampus→neocortex). It is the HOT end of the HOT/WARM/COLD trust tiering — a fact
	// repeatedly recalled on high-value lines, not merely old. The Trust IS the operative prior; the flag
	// lets the TUI/curator distinguish a consolidated prior from a one-shot write-back. Persisted (so the
	// consolidation survives a restart, exactly the CLS durability story).
	Consolidated bool `json:"consolidated,omitempty"`
}

// PriorTrust is the durable neocortical-PRIOR trust tier A-RAG5 consolidates a fact UP to — the HOT end
// of the HOT/WARM/COLD tiering. It matches the reality observation prior (0.92): a fact repeatedly
// recalled on high-value lines has earned the same standing as a fresh observation (CLS: a hippocampal
// trace replayed enough becomes a neocortical prior). It is the ceiling Promote raises toward; a fact
// already at or above it is unchanged.
const PriorTrust = 0.92

// KnowledgeRegistry stores durable domain knowledge and recalls the currently-valid items by relevance
// through the shared precision floor. The embedder makes recall semantic (cosine + RRF) when one is
// reachable, else lexical-only (deterministic offline). emit is the optional bus hook (nil ⇒ silent);
// the engine wires it so knowledge.record/recall/invalidate become observable.
type KnowledgeRegistry struct {
	items    []Knowledge
	embedder retrieval.Embedder
	emit     events.Emit // nil ⇒ silent (no knowledge.* events)
}

// NewKnowledgeRegistry builds a knowledge store. embedder may be nil (lexical-only recall); emit may be
// nil (silent). The store starts empty — knowledge is earned from reality + distillation, or seeded via
// Ingest (the empty-and-earn-it grounding story is the default; a seed corpus is the §7 open flag).
func NewKnowledgeRegistry(embedder retrieval.Embedder, emit events.Emit) *KnowledgeRegistry {
	return &KnowledgeRegistry{embedder: embedder, emit: emit}
}

// Record stores a knowledge item. NEVER-FABRICATE: an ungrounded item is rejected (returns false) — a
// fact the system never actually sourced must not enter the registry and later resurface as durable
// knowledge. A clamped Trust prior is stamped (0..1). On accept it emits knowledge.record.
func (r *KnowledgeRegistry) Record(k Knowledge) bool {
	if !k.Grounded {
		return false // never-fabricate: only sourced knowledge is trusted
	}
	if k.Trust < 0 {
		k.Trust = 0
	}
	if k.Trust > 1 {
		k.Trust = 1
	}
	if k.Kind == "" {
		k.Kind = "fact"
	}
	r.items = append(r.items, k)
	if r.emit != nil {
		r.emit(events.KnowledgeRecord, "knowledge ["+k.Kind+"]: "+clip(k.Statement, 56), events.D{
			"kind": k.Kind, "source": k.Source, "entities": k.Entities,
			"grounded": k.Grounded, "trust": round2(k.Trust),
		})
	}
	return true
}

// Recall returns up to n currently-valid knowledge items related to query (optionally filtered to a
// kind — "" matches any), best-first; an unrelated query returns nil (the precision floor). Invalidated
// items (ValidTo!=0) never surface as current. On a hit it emits knowledge.recall. Reuses
// retrieval.ScoreRecords so the floor is identical to memory's.
func (r *KnowledgeRegistry) Recall(query, kind string, n int) []Knowledge {
	var valid []Knowledge
	for _, k := range r.items {
		if !retrieval.ValidAt(k.ValidFrom, k.ValidTo, retrieval.CurrentTick) {
			continue
		}
		if kind != "" && k.Kind != kind {
			continue
		}
		valid = append(valid, k)
	}
	items := make([]retrieval.Item, len(valid))
	for i, k := range valid {
		items[i] = retrieval.Item{ID: i, Text: k.Statement, Entities: k.Entities}
	}
	idxs := retrieval.ScoreRecords(query, items, r.embedder)
	out := make([]Knowledge, 0, n)
	for _, i := range idxs {
		out = append(out, valid[i])
		if len(out) == n {
			break
		}
	}
	if len(out) > 0 && r.emit != nil {
		r.emit(events.KnowledgeRecall, "knowledge recall: "+itoa(len(out))+" of "+itoa(len(r.items)), events.D{
			"query": clip(query, 48), "kind": kind, "hits": len(out), "top": clip(out[0].Statement, 56),
		})
	}
	return out
}

// Invalidate marks every currently-valid item whose statement matches as invalid as of nowTick
// (invalidate-not-delete, mirroring memory.SemanticRegistry.Invalidate). Returns how many were
// invalidated. On a non-zero count it emits knowledge.invalidate. This is the refutation hook the
// deduction path's reality-refutes branch calls (representation-space-rebuild.md §1.4).
func (r *KnowledgeRegistry) Invalidate(statement string, nowTick int) int {
	n := 0
	for i := range r.items {
		if r.items[i].ValidTo == 0 && r.items[i].Statement == statement {
			r.items[i].ValidTo = nowTick
			n++
		}
	}
	if n > 0 && r.emit != nil {
		r.emit(events.KnowledgeInvalidate, "knowledge invalidated: "+clip(statement, 56), events.D{
			"statement": statement, "count": n, "now_tick": nowTick,
		})
	}
	return n
}

// Promote CONSOLIDATES a fact into a durable prior (A-RAG5, CLS hippocampus→neocortex): it raises the
// trust of every currently-valid item whose statement matches up TOWARD toTrust (clamped 0..1, never
// LOWERED — consolidation is monotone trust-up, the keep-or-revert demotion path uses Invalidate) and
// marks it Consolidated. It returns how many were promoted and the from/to trust of the first match (for
// the convertibility caller's event/log). Only a fact that ALREADY EXISTS, is grounded (it always is —
// the registry only holds grounded items), and is currently valid is promotable: Promote never CREATES a
// fact, so a fabricated statement can never be conjured into a high-trust prior (the never-fabricate
// discipline holds — promotion only re-prices what was already sourced). On a non-zero promotion it emits
// knowledge.promote carrying the recall × value basis the caller passes (recalls, value).
func (r *KnowledgeRegistry) Promote(statement string, toTrust float64, recalls int, value float64) (n int, fromTrust, gotTrust float64) {
	if toTrust < 0 {
		toTrust = 0
	}
	if toTrust > 1 {
		toTrust = 1
	}
	for i := range r.items {
		if r.items[i].ValidTo != 0 || r.items[i].Statement != statement {
			continue
		}
		if n == 0 {
			fromTrust = r.items[i].Trust // remember the first match's pre-promotion trust for the event
		}
		if toTrust > r.items[i].Trust { // monotone: consolidation only RAISES trust, never lowers it
			r.items[i].Trust = toTrust
		}
		r.items[i].Consolidated = true
		gotTrust = r.items[i].Trust
		n++
	}
	if n > 0 && r.emit != nil {
		r.emit(events.KnowledgePromote,
			"knowledge consolidated -> prior ("+ftoa2(fromTrust)+"->"+ftoa2(gotTrust)+"): "+clip(statement, 48),
			events.D{
				"statement": statement, "recalls": recalls, "value": round2(value),
				"from_trust": round2(fromTrust), "to_trust": round2(gotTrust), "count": n,
			})
	}
	return n, fromTrust, gotTrust
}

// DemoteFact REVERTS a consolidation (A-RAG5 keep-or-revert): a fact that was promoted to a prior but
// whose latest high-value line was REFUTED by reality is re-priced DOWN to toTrust (clamped, never RAISED
// — this is the reverse of Promote) and its Consolidated flag is cleared, so a discredited prior stops
// being trusted as a prior. It is the trust-tier analogue of the specialist Demote: a grounded refutation
// takes back the consolidation a recall × value streak earned. It does NOT delete the fact (Invalidate is
// the harder reality-refutes-the-fact-itself path); it only steps the trust back to the WARM tier. Returns
// how many were demoted. A fact at or below toTrust is left unchanged.
func (r *KnowledgeRegistry) DemoteFact(statement string, toTrust float64, nowTick int) int {
	if toTrust < 0 {
		toTrust = 0
	}
	if toTrust > 1 {
		toTrust = 1
	}
	n := 0
	for i := range r.items {
		if r.items[i].ValidTo != 0 || r.items[i].Statement != statement || !r.items[i].Consolidated {
			continue
		}
		if toTrust < r.items[i].Trust { // monotone-down: a demotion only LOWERS the trust
			r.items[i].Trust = toTrust
		}
		r.items[i].Consolidated = false
		n++
	}
	if n > 0 && r.emit != nil {
		r.emit(events.KnowledgePromote,
			"knowledge prior REVERTED (reality refuted): "+clip(statement, 48),
			events.D{"statement": statement, "to_trust": round2(toTrust), "count": n, "now_tick": nowTick, "demote": true})
	}
	return n
}

// Seed re-admits a persisted knowledge item verbatim (cross-session persistence, M4): it preserves
// ValidFrom/ValidTo + Trust EXACTLY (so a bi-temporal invalidation survives the restart) and re-applies
// never-fabricate (an ungrounded item is rejected). Unlike Record it does NOT emit knowledge.record (the
// item was recorded on a prior run; re-seeding is silent state restoration, the same discipline as Load).
func (r *KnowledgeRegistry) Seed(k Knowledge) bool {
	if !k.Grounded {
		return false
	}
	r.items = append(r.items, k)
	return true
}

// Len reports how many items are stored (including invalidated ones).
func (r *KnowledgeRegistry) Len() int { return len(r.items) }

// AllForPersist returns a copy of EVERY stored item, including invalidated ones (ValidTo!=0) — the full
// bi-temporal history the persistence layer (M4) saves so a refutation reconstructs exactly across a
// restart. Unlike Current (which drops invalidated rows), this keeps them. Read-only (a copy).
func (r *KnowledgeRegistry) AllForPersist() []Knowledge { return append([]Knowledge(nil), r.items...) }

// Current returns a copy of the currently-valid items (ValidTo==0) — a read-only view for the TUI
// registry browser. Invalidated items are kept (bi-temporal history) but excluded here.
func (r *KnowledgeRegistry) Current() []Knowledge {
	var out []Knowledge
	for _, k := range r.items {
		if k.ValidTo == 0 {
			out = append(out, k)
		}
	}
	return out
}

// itoa is a tiny stdlib-free int->string for the summaries (keeps the leaf from importing strconv).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// clip truncates s to n runes (adding an ellipsis), so a long statement stays one summary line.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// round2 rounds to 2 decimals for the trust value carried on knowledge.record (readable summaries; the
// stored Trust keeps full precision).
func round2(x float64) float64 {
	return float64(int64(x*100+sign(x)*0.5)) / 100
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

// ftoa2 renders x to two decimals stdlib-free (keeps the leaf off strconv), for the knowledge.promote
// summary line. Negative is handled; the value is a clamped trust (0..1) in practice, so two decimals is
// exact enough for the summary (the stored Trust keeps full precision).
func ftoa2(x float64) string {
	neg := x < 0
	if neg {
		x = -x
	}
	scaled := int64(x*100 + 0.5)
	whole := scaled / 100
	frac := scaled % 100
	s := itoa(int(whole)) + "." + twoDigits(int(frac))
	if neg {
		s = "-" + s
	}
	return s
}

// twoDigits renders 0..99 as a zero-padded two-character fraction for ftoa2.
func twoDigits(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 99 {
		n = 99
	}
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}
