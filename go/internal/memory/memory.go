// Package memory makes long-term memory an EXPLICIT subsystem (memory-stack §3 / P2.3): two typed
// registries over the records we already produce, retrieved through the shared hybrid retriever
// (internal/retrieval). Today memory is coupled — spread across the thought graph, convertibility, and
// the raw transcript, with recall a five-fact toy KB. These registries are the untangling: a first-class
// EpisodicRegistry (what was attempted, on what, how it turned out) and SemanticRegistry (distilled
// beliefs/facts), each recalled by relevance and each obeying NEVER-FABRICATE — only reality-grounded
// records are trusted, so a fabricated "observation" can never become stored knowledge.
//
// Retrieval is relevance-GATED: an unrelated query surfaces NOTHING (the precision floor), because a
// recalled-but-irrelevant memory is worse than none. Offline (no embedder) it is lexical and fully
// deterministic; with an embedder the semantic channel bridges paraphrase. Storage here is in-memory;
// cross-session persistence is P7.1 (JSONL), and bi-temporal validity is P6.1 — both hooked here.
package memory

import (
	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// scoreRecords delegates to the shared precision floor in the retrieval leaf (lifted there in M3 so the
// memory + knowledge registries reuse one floor — representation-space-rebuild.md §3.1). It ranks
// records (given their retrieval Items) against a query, returning the indices of the relevant ones
// best-first; an unrelated query yields an empty slice (the precision floor). embedder may be nil.
func scoreRecords(query string, items []retrieval.Item, embedder retrieval.Embedder) []int {
	return retrieval.ScoreRecords(query, items, embedder)
}

// ---- Episodic memory ----

// Episode is a past episode: the goal pursued, the entities it touched, the worked outcome, and whether
// that outcome was reality-grounded (the never-fabricate flag) with its appraised value + tick.
type Episode struct {
	Goal     string
	Entities []string
	Outcome  string
	Grounded bool
	Value    float64
	Tick     int
}

// EpisodicRegistry stores past episodes and recalls related ones by relevance.
type EpisodicRegistry struct {
	episodes []Episode
	embedder retrieval.Embedder
}

// NewEpisodicRegistry builds an episodic store. embedder may be nil (lexical-only recall).
func NewEpisodicRegistry(embedder retrieval.Embedder) *EpisodicRegistry {
	return &EpisodicRegistry{embedder: embedder}
}

// Record stores a grounded episode. Never-fabricate: an ungrounded episode is NOT stored as trustworthy
// memory (it returns false) — only outcomes that actually came from reality become recallable knowledge.
func (r *EpisodicRegistry) Record(e Episode) bool {
	if !e.Grounded {
		return false
	}
	r.episodes = append(r.episodes, e)
	return true
}

// Seed re-admits a persisted episode verbatim (cross-session persistence, M4): it preserves the
// episode's fields exactly (it does not re-run Record's value defaulting) and re-applies never-fabricate
// (an ungrounded row is rejected). Used by the engine to restore episodic memory from the store at start.
func (r *EpisodicRegistry) Seed(e Episode) bool {
	if !e.Grounded {
		return false
	}
	r.episodes = append(r.episodes, e)
	return true
}

// Len reports how many episodes are stored.
func (r *EpisodicRegistry) Len() int { return len(r.episodes) }

// All returns a copy of every stored episode, newest last (a read-only view for the TUI registry browser).
func (r *EpisodicRegistry) All() []Episode { return append([]Episode(nil), r.episodes...) }

// Recall returns up to k past episodes related to goal, best-first; an unrelated goal returns nil
// (precision floor). Relevance is over the episode's goal + outcome text + entities.
func (r *EpisodicRegistry) Recall(goal string, k int) []Episode {
	items := make([]retrieval.Item, len(r.episodes))
	for i, e := range r.episodes {
		items[i] = retrieval.Item{ID: i, Text: e.Goal + " " + e.Outcome, Entities: e.Entities}
	}
	idxs := scoreRecords(goal, items, r.embedder)
	out := make([]Episode, 0, k)
	for _, i := range idxs {
		out = append(out, r.episodes[i])
		if len(out) == k {
			break
		}
	}
	return out
}

// ---- Semantic memory (beliefs) ----

// Belief is a first-class distilled belief/fact. ValidFrom/ValidTo are seeded ticks (bi-temporal, P6.1):
// ValidTo==0 means "still current"; an overturned belief is invalidated (ValidTo set) not deleted.
type Belief struct {
	Statement string
	Entities  []string
	Source    string
	Grounded  bool
	ValidFrom int
	ValidTo   int // 0 == currently valid
}

// SemanticRegistry stores grounded beliefs and recalls the currently-valid ones by relevance.
type SemanticRegistry struct {
	beliefs  []Belief
	embedder retrieval.Embedder
}

// NewSemanticRegistry builds a belief store. embedder may be nil (lexical-only recall).
func NewSemanticRegistry(embedder retrieval.Embedder) *SemanticRegistry {
	return &SemanticRegistry{embedder: embedder}
}

// Record stores a belief. Never-fabricate: an ungrounded belief is rejected (returns false) — a fact the
// system never actually grounded must not enter semantic memory and later resurface as "knowledge".
func (r *SemanticRegistry) Record(b Belief) bool {
	if !b.Grounded {
		return false
	}
	r.beliefs = append(r.beliefs, b)
	return true
}

// Seed re-admits a persisted belief verbatim (cross-session persistence, M4): it preserves ValidFrom/
// ValidTo EXACTLY (so a bi-temporal invalidation survives the restart — invalidate-not-delete) and
// re-applies never-fabricate. Unlike Record (which stores a currently-valid belief), Seed keeps the
// belief's existing validity window, so a refuted belief stays refuted across a restart.
func (r *SemanticRegistry) Seed(b Belief) bool {
	if !b.Grounded {
		return false
	}
	r.beliefs = append(r.beliefs, b)
	return true
}

// Invalidate marks every currently-valid belief whose statement matches as invalid as of nowTick
// (invalidate-not-delete). Returns how many were invalidated. (Bi-temporal correctness is P6.1; this is
// the hook.)
func (r *SemanticRegistry) Invalidate(statement string, nowTick int) int {
	n := 0
	for i := range r.beliefs {
		if r.beliefs[i].ValidTo == 0 && r.beliefs[i].Statement == statement {
			r.beliefs[i].ValidTo = nowTick
			n++
		}
	}
	return n
}

// Len reports how many beliefs are stored (including invalidated ones).
func (r *SemanticRegistry) Len() int { return len(r.beliefs) }

// AllForPersist returns a copy of EVERY stored belief, including invalidated ones (ValidTo!=0) — the full
// bi-temporal history the persistence layer (M4) must save so a refutation reconstructs exactly across a
// restart. Unlike Current (which drops invalidated rows), this keeps them. Read-only (a copy).
func (r *SemanticRegistry) AllForPersist() []Belief { return append([]Belief(nil), r.beliefs...) }

// Current returns a copy of the currently-valid beliefs (ValidTo==0) — a read-only view for the TUI
// registry browser. Invalidated beliefs are kept (bi-temporal history) but excluded here.
func (r *SemanticRegistry) Current() []Belief {
	var out []Belief
	for _, b := range r.beliefs {
		if b.ValidTo == 0 {
			out = append(out, b)
		}
	}
	return out
}

// Recall returns up to k currently-valid beliefs related to query, best-first; an unrelated query
// returns nil. Invalidated beliefs (ValidTo!=0) never surface as current.
func (r *SemanticRegistry) Recall(query string, k int) []Belief {
	return r.recallValidAt(query, k, currentTick)
}

// currentTick is the sentinel asOf value meaning "now" — a belief is current iff ValidTo==0. Aliases
// the shared retrieval.CurrentTick (the validity rule was lifted to the retrieval leaf in M3).
const currentTick = retrieval.CurrentTick

// RecallAsOf is the BI-TEMPORAL query (P6.1): it returns up to k beliefs that were valid AT tick
// asOfTick, related to query, best-first. A belief was valid at T iff ValidFrom <= T and (it is still
// current OR T < ValidTo). So a belief overturned at t2 still answers a query "as of t1<t2" with the
// OLD value, while a current query never returns it — history is reconstructable, nothing stale
// surfaces as current. Ticks are the seeded engine clock (deterministic), never wall time.
func (r *SemanticRegistry) RecallAsOf(query string, k, asOfTick int) []Belief {
	return r.recallValidAt(query, k, asOfTick)
}

// recallValidAt is the shared temporal+relevance recall: keep the beliefs valid at asOf (currentTick =>
// only ValidTo==0), then relevance-rank them through the retriever.
func (r *SemanticRegistry) recallValidAt(query string, k, asOf int) []Belief {
	var valid []Belief
	for _, b := range r.beliefs {
		if validAt(b, asOf) {
			valid = append(valid, b)
		}
	}
	items := make([]retrieval.Item, len(valid))
	for i, b := range valid {
		items[i] = retrieval.Item{ID: i, Text: b.Statement, Entities: b.Entities}
	}
	idxs := scoreRecords(query, items, r.embedder)
	out := make([]Belief, 0, k)
	for _, i := range idxs {
		out = append(out, valid[i])
		if len(out) == k {
			break
		}
	}
	return out
}

// validAt reports whether belief b held at tick asOf — delegating to the shared bi-temporal rule in the
// retrieval leaf (lifted there in M3 so beliefs + durable knowledge share one validity rule).
func validAt(b Belief, asOf int) bool {
	return retrieval.ValidAt(b.ValidFrom, b.ValidTo, asOf)
}
