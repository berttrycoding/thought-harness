package persist

import (
	"math"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// CuratorConfig tunes the lifecycle/cleanup manager (representation-space-rebuild.md §4.5). Defaults are
// generous so curation is observable on small fixtures yet never trims live state on a normal run.
type CuratorConfig struct {
	HalfLife    int     // recency half-life in ticks for the decay score (default 200)
	DormantAt   float64 // score below this ⇒ dormant (not re-loaded next start) (default 0.15)
	ArchiveTTL  int     // a dormant+zero-use record older than this (ticks) ⇒ archived (default 1000)
	MaxEpisodes int     // per-artifact size cap; lowest-scoring active records archive first (default 500)
	MaxBeliefs  int
	MaxKnow     int
	MaxSkills   int
	MaxOps      int
	MaxSpecs    int
}

// DefaultCuratorConfig returns the standing curator tunables. Coupled to the §4.5 caps so a busy run
// stays bounded but a normal scenario never triggers an archive.
func DefaultCuratorConfig() CuratorConfig {
	return CuratorConfig{
		HalfLife: 200, DormantAt: 0.15, ArchiveTTL: 1000,
		MaxEpisodes: 500, MaxBeliefs: 500, MaxKnow: 500,
		MaxSkills: 200, MaxOps: 200, MaxSpecs: 200,
	}
}

// Curator is the lifecycle/cleanup manager: a PURE function over the record snapshot + the seeded tick.
// It runs at IDLE/ASLEEP consolidation (off the hot path), applying — in order — versioning, dedup,
// decay/aging, demotion of refuted entries, GC of stale, and size caps. It touches NO filesystem (the
// Store does that on Flush); every decision is deterministic given (snapshot, tick), so it is unit-
// testable with a fake tick fixture. Each action emits persist.curate.
type Curator struct {
	cfg  CuratorConfig
	emit events.Emit
}

// NewCurator builds a curator. cfg=nil ⇒ DefaultCuratorConfig; emit may be nil (silent).
func NewCurator(cfg *CuratorConfig, emit events.Emit) *Curator {
	c := DefaultCuratorConfig()
	if cfg != nil {
		c = *cfg
	}
	return &Curator{cfg: c, emit: emit}
}

// Curate applies the six curator stages to a snapshot at tick now, returning the curated snapshot (a new
// value — the input is not mutated in place beyond its own slices). The order is FIXED
// (representation-space-rebuild.md §4.5):
//
//  1. Versioning — handled at Save time (same-name re-mint with a different body bumps Version); the
//     curator only re-asserts that a superseded record stays present (append-only invalidate-not-delete).
//  2. Dedup — identical-Hash records collapse to one, summing UseCount.
//  3. Decay / aging — score = f(UseCount, recency); below DormantAt ⇒ Status=dormant (kept, not re-loaded).
//  4. Demotion of refuted entries — a belief/knowledge item with ValidTo!=0 ⇒ Status=demoted.
//  5. GC of stale — dormant + zero-use + older than ArchiveTTL ⇒ Status=archived (tombstoned).
//  6. Size caps — per-artifact, the lowest-scoring ACTIVE entries archive first when over the cap.
func (c *Curator) Curate(sn *Snapshot, now int) *Snapshot {
	if sn == nil {
		return sn
	}
	out := *sn // shallow copy of the header; the stages rebuild the slices

	out.Skills = c.curateMetas(toMetas(out.Skills), now, c.cfg.MaxSkills, "skill", func(i int) string { return out.Skills[i].Name }, applyMetasSkills(&out)).([]SkillRecord)
	out.Operators = c.curateMetas(toMetas(out.Operators), now, c.cfg.MaxOps, "operator", func(i int) string { return out.Operators[i].Name }, applyMetasOps(&out)).([]OpRecord)
	out.Specialists = c.curateMetas(toMetas(out.Specialists), now, c.cfg.MaxSpecs, "specialist", func(i int) string { return out.Specialists[i].Domain }, applyMetasSpecs(&out)).([]SpecialistRecord)
	out.Episodes = c.curateMetas(toMetas(out.Episodes), now, c.cfg.MaxEpisodes, "episode", func(i int) string { return out.Episodes[i].Goal }, applyMetasEps(&out)).([]EpisodeRecord)
	out.Beliefs = c.curateBeliefs(out.Beliefs, now)
	out.Knowledge = c.curateKnowledge(out.Knowledge, now)
	// preferences are a small bounded set keyed by trait — no caps/decay; they pass through untouched.
	return &out
}

// -- the generic meta pipeline (dedup + decay + GC + caps) for artifacts WITHOUT bi-temporal demotion --

// metaView is the per-record handle the generic pipeline needs: its identity (Hash), its curator Meta,
// and an index back into the typed slice. Avoids reflection: each typed curate* builds metaViews, runs
// the shared decisions, and the apply closure writes the decisions back into the typed slice.
type metaView struct {
	idx  int
	hash string
	meta Meta
}

// toMetas extracts metaViews from a typed record slice via a small per-type adapter. Implemented by the
// type-specific helpers below (toMetas is overloaded by closure, so this generic name takes `any`).
func toMetas(recs any) []metaView {
	switch v := recs.(type) {
	case []SkillRecord:
		return collectMetas(len(v), func(i int) (string, Meta) { return v[i].Meta.Hash, v[i].Meta })
	case []OpRecord:
		return collectMetas(len(v), func(i int) (string, Meta) { return v[i].Meta.Hash, v[i].Meta })
	case []SpecialistRecord:
		return collectMetas(len(v), func(i int) (string, Meta) { return v[i].Meta.Hash, v[i].Meta })
	case []EpisodeRecord:
		return collectMetas(len(v), func(i int) (string, Meta) { return v[i].Meta.Hash, v[i].Meta })
	}
	return nil
}

func collectMetas(n int, at func(i int) (string, Meta)) []metaView {
	out := make([]metaView, n)
	for i := 0; i < n; i++ {
		h, m := at(i)
		out[i] = metaView{idx: i, hash: h, meta: m}
	}
	return out
}

// curateMetas runs dedup → decay → GC → cap over the metaViews, returns the kept (curated) order of
// original indices, and lets the apply closure rebuild the typed slice. The returned typed slice is what
// the apply closure produces. label/idName are for the persist.curate events.
func (c *Curator) curateMetas(mvs []metaView, now, cap int, label string, idName func(i int) string, apply func(kept []metaView) any) any {
	// 2. DEDUP — collapse identical-Hash records, summing UseCount; keep the first occurrence.
	seen := map[string]int{} // hash -> position in deduped
	deduped := make([]metaView, 0, len(mvs))
	for _, mv := range mvs {
		if mv.hash == "" { // no identity ⇒ never dedup (keep)
			deduped = append(deduped, mv)
			continue
		}
		if pos, ok := seen[mv.hash]; ok {
			deduped[pos].meta.UseCount += mv.meta.UseCount
			if mv.meta.LastUsedTick > deduped[pos].meta.LastUsedTick {
				deduped[pos].meta.LastUsedTick = mv.meta.LastUsedTick
			}
			c.curated("dedup", label, idName(mv.idx), "identical hash collapsed")
			continue
		}
		seen[mv.hash] = len(deduped)
		deduped = append(deduped, mv)
	}
	// 3. DECAY/AGING + 5. GC — score each; below the floor ⇒ dormant; dormant+stale+zero-use ⇒ archived.
	for i := range deduped {
		mv := &deduped[i]
		if mv.meta.Status == StatusArchived {
			continue
		}
		score := c.score(mv.meta, now)
		if mv.meta.Status == StatusActive && score < c.cfg.DormantAt {
			mv.meta.Status = StatusDormant
			c.curated("decay", label, idName(mv.idx), "score "+f2(score)+" < dormant floor "+f2(c.cfg.DormantAt))
		}
		if mv.meta.Status == StatusDormant && mv.meta.UseCount == 0 &&
			now-mv.meta.LastUsedTick > c.cfg.ArchiveTTL {
			mv.meta.Status = StatusArchived
			c.curated("gc", label, idName(mv.idx), "dormant + stale (> TTL) + zero-use ⇒ archived")
		}
	}
	// 6. SIZE CAP — when the ACTIVE count exceeds the cap, archive the lowest-scoring active records.
	if cap > 0 {
		active := make([]int, 0, len(deduped))
		for i := range deduped {
			if deduped[i].meta.Status == StatusActive {
				active = append(active, i)
			}
		}
		if len(active) > cap {
			sort.SliceStable(active, func(a, b int) bool {
				return c.score(deduped[active[a]].meta, now) < c.score(deduped[active[b]].meta, now)
			})
			for _, i := range active[:len(active)-cap] {
				deduped[i].meta.Status = StatusArchived
				c.curated("cap", label, idName(deduped[i].idx), "over size cap; lowest-scoring archived")
			}
		}
	}
	return apply(deduped)
}

// score is the decay/aging score: f(UseCount, recency). Recency is an exponential decay on the age in
// ticks (half-life HalfLife); UseCount adds a bounded boost. A grounded high-use recent record scores
// high; an old zero-use one decays toward 0. Deterministic given (meta, now).
func (c *Curator) score(m Meta, now int) float64 {
	age := now - m.LastUsedTick
	if age < 0 {
		age = 0
	}
	hl := c.cfg.HalfLife
	if hl <= 0 {
		hl = 1
	}
	recency := math.Pow(0.5, float64(age)/float64(hl))
	use := 1.0 - math.Pow(0.5, float64(m.UseCount)) // 0 at use=0, →1 as use grows
	return 0.5*recency + 0.5*use
}

// -- bi-temporal artifacts (beliefs / knowledge): demotion of refuted entries (stage 4) ------------

// curateBeliefs applies stage 4 (demotion: ValidTo!=0 ⇒ demoted) plus decay/GC/cap over beliefs.
func (c *Curator) curateBeliefs(recs []BeliefRecord, now int) []BeliefRecord {
	// 2. dedup by hash (sum UseCount on the kept one).
	seen := map[string]int{}
	out := make([]BeliefRecord, 0, len(recs))
	for _, r := range recs {
		if r.Meta.Hash != "" {
			if pos, ok := seen[r.Meta.Hash]; ok {
				out[pos].Meta.UseCount += r.Meta.UseCount
				if r.ValidTo != 0 {
					out[pos].ValidTo = r.ValidTo
				}
				c.curated("dedup", "belief", clip(r.Statement, 40), "identical hash collapsed")
				continue
			}
			seen[r.Meta.Hash] = len(out)
		}
		out = append(out, r)
	}
	for i := range out {
		// 4. DEMOTION — a refuted (invalidated) belief flips to demoted (excluded from recall, kept for audit).
		if out[i].ValidTo != 0 && out[i].Meta.Status == StatusActive {
			out[i].Meta.Status = StatusDemoted
			c.curated("demote", "belief", clip(out[i].Statement, 40), "reality refuted (ValidTo set)")
		}
		// 3. decay (only an active record can decay to dormant).
		if out[i].Meta.Status == StatusActive && c.score(out[i].Meta, now) < c.cfg.DormantAt {
			out[i].Meta.Status = StatusDormant
			c.curated("decay", "belief", clip(out[i].Statement, 40), "decayed below floor")
		}
		// 5. GC.
		if out[i].Meta.Status == StatusDormant && out[i].Meta.UseCount == 0 &&
			now-out[i].Meta.LastUsedTick > c.cfg.ArchiveTTL {
			out[i].Meta.Status = StatusArchived
			c.curated("gc", "belief", clip(out[i].Statement, 40), "stale dormant archived")
		}
	}
	c.capBeliefs(out, now)
	return out
}

// curateKnowledge mirrors curateBeliefs for the durable domain-knowledge store.
func (c *Curator) curateKnowledge(recs []KnowledgeRecord, now int) []KnowledgeRecord {
	seen := map[string]int{}
	out := make([]KnowledgeRecord, 0, len(recs))
	for _, r := range recs {
		if r.Meta.Hash != "" {
			if pos, ok := seen[r.Meta.Hash]; ok {
				out[pos].Meta.UseCount += r.Meta.UseCount
				if r.ValidTo != 0 {
					out[pos].ValidTo = r.ValidTo
				}
				c.curated("dedup", "knowledge", clip(r.Statement, 40), "identical hash collapsed")
				continue
			}
			seen[r.Meta.Hash] = len(out)
		}
		out = append(out, r)
	}
	for i := range out {
		if out[i].ValidTo != 0 && out[i].Meta.Status == StatusActive {
			out[i].Meta.Status = StatusDemoted
			c.curated("demote", "knowledge", clip(out[i].Statement, 40), "reality refuted (ValidTo set)")
		}
		if out[i].Meta.Status == StatusActive && c.score(out[i].Meta, now) < c.cfg.DormantAt {
			out[i].Meta.Status = StatusDormant
			c.curated("decay", "knowledge", clip(out[i].Statement, 40), "decayed below floor")
		}
		if out[i].Meta.Status == StatusDormant && out[i].Meta.UseCount == 0 &&
			now-out[i].Meta.LastUsedTick > c.cfg.ArchiveTTL {
			out[i].Meta.Status = StatusArchived
			c.curated("gc", "knowledge", clip(out[i].Statement, 40), "stale dormant archived")
		}
	}
	c.capKnowledge(out, now)
	return out
}

// capBeliefs archives the lowest-scoring active beliefs over MaxBeliefs.
func (c *Curator) capBeliefs(recs []BeliefRecord, now int) {
	if c.cfg.MaxBeliefs <= 0 {
		return
	}
	active := activeIdx(len(recs), func(i int) Status { return recs[i].Meta.Status })
	if len(active) <= c.cfg.MaxBeliefs {
		return
	}
	sort.SliceStable(active, func(a, b int) bool {
		return c.score(recs[active[a]].Meta, now) < c.score(recs[active[b]].Meta, now)
	})
	for _, i := range active[:len(active)-c.cfg.MaxBeliefs] {
		recs[i].Meta.Status = StatusArchived
		c.curated("cap", "belief", clip(recs[i].Statement, 40), "over size cap")
	}
}

// capKnowledge archives the lowest-scoring active knowledge over MaxKnow.
func (c *Curator) capKnowledge(recs []KnowledgeRecord, now int) {
	if c.cfg.MaxKnow <= 0 {
		return
	}
	active := activeIdx(len(recs), func(i int) Status { return recs[i].Meta.Status })
	if len(active) <= c.cfg.MaxKnow {
		return
	}
	sort.SliceStable(active, func(a, b int) bool {
		return c.score(recs[active[a]].Meta, now) < c.score(recs[active[b]].Meta, now)
	})
	for _, i := range active[:len(active)-c.cfg.MaxKnow] {
		recs[i].Meta.Status = StatusArchived
		c.curated("cap", "knowledge", clip(recs[i].Statement, 40), "over size cap")
	}
}

// activeIdx returns the indices of the active records (status == active) via a status accessor.
func activeIdx(n int, status func(i int) Status) []int {
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if status(i) == StatusActive {
			out = append(out, i)
		}
	}
	return out
}

// -- apply closures: write the generic metaView decisions back into the typed slices --------------
//
// Each apply closure rebuilds the typed slice in the curated (deduped) order, copying the curated Meta
// (Status/UseCount/LastUsedTick) onto each typed record. The metaView.idx points back into the ORIGINAL
// slice, so the typed payload is fetched from there.

func applyMetasSkills(out *Snapshot) func([]metaView) any {
	orig := out.Skills
	return func(kept []metaView) any {
		res := make([]SkillRecord, 0, len(kept))
		for _, mv := range kept {
			r := orig[mv.idx]
			r.Meta = mv.meta
			res = append(res, r)
		}
		return res
	}
}

func applyMetasOps(out *Snapshot) func([]metaView) any {
	orig := out.Operators
	return func(kept []metaView) any {
		res := make([]OpRecord, 0, len(kept))
		for _, mv := range kept {
			r := orig[mv.idx]
			r.Meta = mv.meta
			res = append(res, r)
		}
		return res
	}
}

func applyMetasSpecs(out *Snapshot) func([]metaView) any {
	orig := out.Specialists
	return func(kept []metaView) any {
		res := make([]SpecialistRecord, 0, len(kept))
		for _, mv := range kept {
			r := orig[mv.idx]
			r.Meta = mv.meta
			res = append(res, r)
		}
		return res
	}
}

func applyMetasEps(out *Snapshot) func([]metaView) any {
	orig := out.Episodes
	return func(kept []metaView) any {
		res := make([]EpisodeRecord, 0, len(kept))
		for _, mv := range kept {
			r := orig[mv.idx]
			r.Meta = mv.meta
			res = append(res, r)
		}
		return res
	}
}

// curated emits persist.curate for one curator action. Nil-safe (nil emit ⇒ silent).
func (c *Curator) curated(action, artifact, id, reason string) {
	if c.emit == nil {
		return
	}
	c.emit(events.PersistCurate, "curate ["+action+"] "+artifact+" "+id, events.D{
		"action": action, "artifact": artifact, "id": id, "reason": reason,
	})
}

// f2 formats a float at 2 decimals for the curate reason strings.
func f2(x float64) string {
	return formatFloat(x)
}

// formatFloat renders x with 2 decimals (no strconv-import sprawl in this file).
func formatFloat(x float64) string {
	scaled := int64(math.Round(x * 100))
	whole := scaled / 100
	frac := scaled % 100
	if frac < 0 {
		frac = -frac
	}
	return itoa(int(whole)) + "." + pad2(int(frac))
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
