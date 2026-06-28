// Package funnel is the deterministic core of the registry-scaling validation funnel
// (docs/internal/notes/registry-scaling-strategy.md §4): the gates that keep an over-generated candidate flood
// honest BEFORE the expensive model-in-the-loop lift benchmark sees it. It is registry-AGNOSTIC — an
// operator, skill, workflow, or knowledge candidate is the same Candidate here — so the one funnel
// serves every scalable registry, and it is a LEAF utility (no engine/registry imports) so anything
// can call it without an import cycle.
//
// The funnel stages, cheapest-and-earliest first (the expensive benchmark only ever sees survivors):
//
//	Stage C — Consolidate   cluster the flood, merge near-duplicates, keep one representative per cluster.
//	Tier 0  — Pre-admission  anti-filler 3-test (traceable + cross-linked + exercised) + exact/near dedup.
//	Tier 1  — Retrieval-integrity  shadow-add the batch, re-run a held-out query set, reject rank-1 regressions.
//	Tier 2  — Capability lift  (tier2.go) the BEFORE/AFTER lift RUNNER + the keep-or-revert-the-batch
//	          decision, gated on COMPLETION-tokens-per-task at held utility (cache-immune). It stays a
//	          LEAF: the real internal/bench run is INJECTED as a LiftBench (a test injects a fake), so the
//	          keep-or-revert LOGIC is provable offline; it is OPT-IN (nothing above calls it), so the
//	          default funnel behaviour is byte-identical. The per-registry adapters (adapters.go) sieve
//	          skills/operators/workflows/knowledge through the SAME pipeline via the RegistrySieve.
//	Tier 3  — Longitudinal demotion  (NOT here — reuses the persist Curator at idle ticks.)
//
// Everything in THIS package is deterministic and offline: the semantic near-dup signal is INJECTED as
// a Similarity func, defaulting to a self-contained lexical (token-Jaccard) signal so the funnel runs
// and tests with no model; a caller with a reachable embedder injects a vector-cosine Similarity for
// the real semantic cut.
package funnel

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Candidate is one proposed registry entry flowing through the funnel — registry-agnostic. The caller
// (per registry) fills the fields from its own entry shape: e.g. for an operator ClusterKey is
// Move+Family, for a skill it is goal+body-shape, for a workflow the topology, for knowledge the
// statement; Text is the canonical content used for dedup/near-dup and the "exercised" check.
type Candidate struct {
	ID         string    // stable identity (operator name / skill id / …); REQUIRED.
	Kind       string    // "operator" | "skill" | "workflow" | "knowledge" (for reporting).
	ClusterKey string    // the Stage-C bucketing key; REQUIRED (an empty key buckets alone).
	Text       string    // canonical content; REQUIRED (dedup/near-dup + anti-filler exercised).
	Provenance string    // source feeder + rationale (anti-filler test 1: traceable).
	Links      []string  // cross-links to other entries/registries (anti-filler test 2: cross-linked).
	Exercised  bool      // has been applied/run at least once, not merely declared (test 3: exercised).
	Vector     []float32 // optional cached embedding (lets a caller inject a vector-cosine Similarity).

	// Program + Triggers carry a SKILL candidate (Kind == "skill"): a converged reasoning Program (the
	// serialized cognition.Program node dict, kept as map[string]any so funnel stays a leaf) and the
	// goal triggers it matches on. Nil for non-skill kinds. This is the candidate shape the registry-
	// scaling research identified as the genuine lever (recall a converged structure → fewer
	// synthesise calls / the right cognitive faculty, vs canned content).
	Program  map[string]any `json:"program,omitempty"`
	Triggers []string       `json:"triggers,omitempty"`
}

// --- Tier 0a: the anti-filler 3-test --------------------------------------------------------------

// AntiFillerVerdict is the per-candidate Tier-0a result: Pass plus the named failed tests (so a
// rejection carries its WHY, never a bare bool).
type AntiFillerVerdict struct {
	Pass  bool
	Fails []string // e.g. "not-traceable", "not-cross-linked", "not-exercised", "empty-id", "empty-text"
}

// AntiFiller applies the Tier-0 3-test (registry-scaling §4 Tier 0): an admitted entry must be
// TRACEABLE (has provenance), CROSS-LINKED (references at least one other entry/registry — not an
// island), and EXERCISED (has actually been applied once — a declared-but-never-run entry is filler).
// Plus the structural minimum (non-empty id + text). Deterministic; no model.
func AntiFiller(c Candidate) AntiFillerVerdict {
	var fails []string
	if strings.TrimSpace(c.ID) == "" {
		fails = append(fails, "empty-id")
	}
	if strings.TrimSpace(c.Text) == "" {
		fails = append(fails, "empty-text")
	}
	if strings.TrimSpace(c.Provenance) == "" {
		fails = append(fails, "not-traceable")
	}
	if !hasNonEmpty(c.Links) {
		fails = append(fails, "not-cross-linked")
	}
	if !c.Exercised {
		fails = append(fails, "not-exercised")
	}
	return AntiFillerVerdict{Pass: len(fails) == 0, Fails: fails}
}

// --- Tier 0b / Stage C: dedup + consolidation -----------------------------------------------------

// Similarity is a 0..1 semantic-closeness signal between two candidates (1 == identical). It is
// injected so the funnel stays offline-by-default: LexicalSimilarity (token-Jaccard) is the no-model
// fallback; a caller with an embedder injects VectorSimilarity for the real cut.
type Similarity func(a, b Candidate) float64

// LexicalSimilarity is the deterministic, offline near-dup signal: content-word Jaccard over Text. It
// needs no model, so it is the default everywhere a real embedder is not wired.
func LexicalSimilarity(a, b Candidate) float64 {
	sa, sb := tokenSet(a.Text), tokenSet(b.Text)
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// ConsolidateResult is the Stage-C output: the kept representatives (one per near-dup sub-cluster) and
// a merge map (representative ID -> the IDs folded into it), so the consolidation is auditable.
type ConsolidateResult struct {
	Kept   []Candidate         // representatives, in stable (cluster-key, id) order.
	Merged map[string][]string // representative ID -> merged-away candidate IDs.
}

// Consolidate is Stage C (registry-scaling §4 Stage C): collapse an over-generated flood to a clean
// basis BEFORE any tier pays for it. It (1) drops EXACT duplicates by normalized-text hash, then
// (2) buckets by ClusterKey (a cheap pre-filter so near-dup comparison is local), then (3) within each
// bucket greedily merges NEAR-duplicates (sim >= theta) into the first-seen representative. Order is
// deterministic: candidates are processed in (ClusterKey, ID) order, so the representative chosen and
// the result are reproducible regardless of input order.
func Consolidate(batch []Candidate, sim Similarity, theta float64) ConsolidateResult {
	if sim == nil {
		sim = LexicalSimilarity
	}
	// stable order: (ClusterKey, ID) — makes representative selection + output reproducible.
	ordered := append([]Candidate(nil), batch...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ClusterKey != ordered[j].ClusterKey {
			return ordered[i].ClusterKey < ordered[j].ClusterKey
		}
		return ordered[i].ID < ordered[j].ID
	})

	// (1) exact-text dedup (normalized) — the cheapest cut.
	seenHash := map[string]string{} // hash -> representative ID
	merged := map[string][]string{}
	var afterExact []Candidate
	for _, c := range ordered {
		h := normHash(c.Text)
		if rep, ok := seenHash[h]; ok {
			merged[rep] = append(merged[rep], c.ID)
			continue
		}
		seenHash[h] = c.ID
		afterExact = append(afterExact, c)
	}

	// (2+3) bucket by ClusterKey, near-dup merge within bucket.
	reps := map[string][]Candidate{} // ClusterKey -> kept representatives in that bucket
	var kept []Candidate
	for _, c := range afterExact {
		bucket := reps[c.ClusterKey]
		mergedInto := ""
		for _, r := range bucket {
			if sim(r, c) >= theta {
				mergedInto = r.ID
				break
			}
		}
		if mergedInto != "" {
			merged[mergedInto] = append(merged[mergedInto], c.ID)
			// c may itself have been an exact-dedup representative (merged[c.ID] holds the IDs that
			// folded into it). c is now merged away, so RE-HOME those onto the final representative —
			// otherwise merged[c.ID] survives with a key (c) that is NOT in Kept (a dangling audit chain
			// / a rejection reason pointing at a non-survivor). Keeps every merge-map key a real survivor.
			if sub, ok := merged[c.ID]; ok {
				merged[mergedInto] = append(merged[mergedInto], sub...)
				delete(merged, c.ID)
			}
			continue
		}
		reps[c.ClusterKey] = append(reps[c.ClusterKey], c)
		kept = append(kept, c)
	}
	return ConsolidateResult{Kept: kept, Merged: merged}
}

// --- Tier 0: the combined pre-admission gate ------------------------------------------------------

// AdmitResult is the Tier-0 output over a batch: the admitted candidates plus a per-rejection reason
// keyed by candidate ID (anti-filler fails are joined; a dedup drop reads "near-dup-of:<repID>").
type AdmitResult struct {
	Admitted []Candidate
	Rejected map[string]string // candidate ID -> reason
}

// Admit runs the full Tier-0 pre-admission: anti-filler 3-test on every candidate, then Stage-C
// consolidation (exact + near-dup dedup) over the survivors. The result is the clean, de-duplicated,
// non-filler set that Tier 1 then checks for retrieval confusion. Deterministic; no model.
func Admit(batch []Candidate, sim Similarity, theta float64) AdmitResult {
	rejected := map[string]string{}
	var passed []Candidate
	for _, c := range batch {
		if v := AntiFiller(c); !v.Pass {
			rejected[c.ID] = "anti-filler:" + strings.Join(v.Fails, ",")
			continue
		}
		passed = append(passed, c)
	}
	con := Consolidate(passed, sim, theta)
	for rep, mergedIDs := range con.Merged {
		for _, id := range mergedIDs {
			rejected[id] = "near-dup-of:" + rep
		}
	}
	return AdmitResult{Admitted: con.Kept, Rejected: rejected}
}

// --- Tier 1: retrieval-integrity ------------------------------------------------------------------

// Query is one held-out canonical retrieval probe: a Text that SHOULD surface ExpectedID as rank-1 in
// the registry. The set is built against the EXISTING registry (entries that already rank correctly).
type Query struct {
	Text       string
	ExpectedID string
}

// Ranker returns the registry's ranked entry IDs for a query text, best-first. Tier 1 needs two: the
// BASELINE ranker (the existing registry) and a SHADOW ranker (existing + the candidate batch).
type Ranker func(queryText string) []string

// IntegrityResult is the Tier-1 output: Pass iff the batch caused NO rank-1 regression on the
// canonical set; Regressions lists each query whose correct rank-1 was displaced by the batch.
type IntegrityResult struct {
	Pass        bool
	Checked     int
	Regressions []Query
}

// RetrievalIntegrity is Tier 1 (registry-scaling §4 Tier 1) — the direct anti-CONFUSION gate, the
// most-skipped one. For each canonical query that the BASELINE already answers correctly at rank-1, it
// asserts the SHADOW ranker (registry + batch) STILL answers it at rank-1. A query the baseline already
// got wrong is skipped (the batch is not blamed for a pre-existing miss). Any displaced rank-1 is a
// regression → the batch dilutes its own registry's retrieval and must revert. Deterministic; the model
// (if any) lives inside the injected rankers, not here.
func RetrievalIntegrity(canonical []Query, baseline, shadow Ranker) IntegrityResult {
	res := IntegrityResult{Pass: true}
	for _, q := range canonical {
		base := baseline(q.Text)
		if len(base) == 0 || base[0] != q.ExpectedID {
			continue // baseline already misses this — not the batch's fault, don't gate on it.
		}
		res.Checked++
		sh := shadow(q.Text)
		if len(sh) == 0 || sh[0] != q.ExpectedID {
			res.Pass = false
			res.Regressions = append(res.Regressions, q)
		}
	}
	return res
}

// --- small deterministic helpers ------------------------------------------------------------------

func hasNonEmpty(ss []string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// normHash is the exact-dedup key: lower-cased, whitespace-collapsed text hashed. Two candidates whose
// text differs only by case/spacing are exact duplicates.
func normHash(text string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// tokenSet is the CONTENT-word set for lexical similarity: lower-cased fields, punctuation trimmed,
// trivial 1-char tokens AND common function words (stopwords) dropped — so the Jaccard reflects shared
// MEANING, not shared "the/of/and". Deterministic.
func tokenSet(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, w := range strings.Fields(strings.ToLower(text)) {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`")
		if len(w) <= 1 {
			continue
		}
		if _, stop := stopwords[w]; stop {
			continue
		}
		out[w] = struct{}{}
	}
	return out
}

// stopwords are the common function words excluded from content-word Jaccard (a compact, deterministic
// list — enough to stop "the/of/and" from inflating near-dup overlap, not a full NLP stoplist).
var stopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "of": {}, "to": {}, "in": {}, "into": {}, "on": {}, "and": {},
	"or": {}, "is": {}, "are": {}, "be": {}, "by": {}, "for": {}, "with": {}, "as": {}, "at": {},
	"it": {}, "its": {}, "this": {}, "that": {}, "from": {}, "using": {}, "via": {}, "use": {},
}
