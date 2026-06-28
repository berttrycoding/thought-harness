// rollup.go is the TELEMETRY ROLLUP over the legible.* event stream (WF-E CC-1 part 3): the read-only
// reduction the SHADOW instrument exists to feed (05-LEGIBLE-GENERATION §4 / §7). It folds the three
// legible.* event kinds (legible.tag / legible.parity / legible.novel) into the three numbers the
// registry-scaling work reads after a real-model run:
//
//   - FAST-PATH HIT RATE — the share of parsed tags whose op is KNOWN (in the registry) — i.e. the share
//     of thoughts the fast path could ROUTE without falling back to the LLM classifier. Unknown + novel
//     tags miss (they route to the fallback + a mint signal). This is the coverage number §4 names.
//   - NOVEL-TAG HISTOGRAM — the ranked count of each novel:<desc> sighting. This IS the gap list that
//     feeds registry scaling (§4 / §7): the most-requested missing move is the next operator to mint.
//   - PER-SEAM PARITY RATE — the share of parity observations (per site: filter / gate) where the shadow
//     route AGREED with the actual control-floor decision. High parity is the precondition for ever
//     promoting the shadow to a real route (§6); this rollup only MEASURES it.
//
// This is a MEASUREMENT instrument, not a cost play, and makes NO claim beyond what the events carry: a
// rollup over ZERO legible.* events (the default OFF run) is the empty rollup, and its report says so
// honestly rather than dividing by zero. It is pure over the event slice — no I/O, no bus, no engine.
//
// LEAF: pure stdlib + the events leaf (for the kind constants + the Event shape). No seam, no backend.
package legible

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// Rollup is the reduced telemetry over a legible.* event stream. Build it with NewRollup (empty) and feed
// events via Observe, or fold a whole slice in one call with RollupOf. Every field is a plain counter so
// the rates are derived (not stored) — Report computes them on demand, and a zero-denominator rate reads
// as "n/a" rather than NaN.
type Rollup struct {
	// tags is the total number of legible.tag events seen (one per parsed in-band tag at a seam site).
	// It is the HIT-RATE denominator: every parsed tag either fast-paths (known) or falls back.
	tags int
	// known is the number of those tags whose op is a registered operator (Known=true) — the fast-path
	// HITS. The hit rate is known/tags.
	known int
	// novelHist counts each novel:<desc> payload (the ranked scaling gap). Key is the description text;
	// an empty description folds under "(unspecified)" so a blank novel: tag still counts as one gap.
	novelHist map[string]int
	// parity buckets parity observations PER SITE (filter / gate): for each site, total observations and
	// the subset that AGREED. The per-seam parity rate is agree/total within a site.
	parity map[string]*parityCount
}

// parityCount is the per-site parity accumulator (total observations + agreements).
type parityCount struct {
	total int
	agree int
}

// NewRollup returns an empty rollup ready to Observe events.
func NewRollup() *Rollup {
	return &Rollup{
		novelHist: map[string]int{},
		parity:    map[string]*parityCount{},
	}
}

// RollupOf folds a whole slice of events into one rollup in a single call (the common case: reduce a
// captured run). Non-legible events are ignored, so a full mixed stream can be passed straight through.
func RollupOf(evs []events.Event) *Rollup {
	r := NewRollup()
	for _, e := range evs {
		r.Observe(e)
	}
	return r
}

// Observe folds ONE event into the rollup. It dispatches on Kind and reads the documented data keys off
// the event's Data map (the same keys shadow.go emits). Any event that is not a legible.* kind — or a
// legible.* event missing its key fields — is ignored, so a partial/mixed stream never panics and never
// corrupts a counter. nil-safe: Observe on a nil *Rollup is a no-op.
func (r *Rollup) Observe(e events.Event) {
	if r == nil {
		return
	}
	switch e.Kind {
	case events.LegibleTag:
		r.tags++
		if boolField(e.Data, "known") {
			r.known++
		}
	case events.LegibleNovel:
		desc := strings.TrimSpace(stringField(e.Data, "desc"))
		if desc == "" {
			desc = "(unspecified)"
		}
		r.novelHist[desc]++
	case events.LegibleParity:
		site := stringField(e.Data, "site")
		if site == "" {
			site = "(unknown)"
		}
		pc := r.parity[site]
		if pc == nil {
			pc = &parityCount{}
			r.parity[site] = pc
		}
		pc.total++
		if boolField(e.Data, "agree") {
			pc.agree++
		}
	}
}

// Tags returns the total number of parsed tags observed (the hit-rate denominator).
func (r *Rollup) Tags() int { return r.tags }

// Known returns the number of fast-path HITS (tags whose op is a known operator).
func (r *Rollup) Known() int { return r.known }

// HitRate is the fast-path hit rate: the fraction (0..1) of parsed tags whose op is KNOWN and could
// fast-path. Returns (0, false) when no tags were observed (no denominator — render as "n/a").
func (r *Rollup) HitRate() (rate float64, ok bool) {
	if r == nil || r.tags == 0 {
		return 0, false
	}
	return float64(r.known) / float64(r.tags), true
}

// NovelEntry is one ranked row of the novel-tag histogram: a missing-move description + its sighting
// count. The slice NovelHistogram returns is sorted by count DESC, then description ASC (stable, so the
// gap list is deterministic across runs).
type NovelEntry struct {
	Desc  string
	Count int
}

// NovelHistogram returns the novel-tag gap list, ranked by count DESC then description ASC. Empty when no
// novel: tag was seen.
func (r *Rollup) NovelHistogram() []NovelEntry {
	if r == nil {
		return nil
	}
	out := make([]NovelEntry, 0, len(r.novelHist))
	for desc, n := range r.novelHist {
		out = append(out, NovelEntry{Desc: desc, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Desc < out[j].Desc
	})
	return out
}

// SiteParity is one per-seam parity row: the site (filter / gate), the agreement count, the total
// observations, and the derived rate.
type SiteParity struct {
	Site  string
	Agree int
	Total int
}

// Rate is the agreement rate for this site (0..1). Returns (0, false) when the site has no observations.
func (p SiteParity) Rate() (float64, bool) {
	if p.Total == 0 {
		return 0, false
	}
	return float64(p.Agree) / float64(p.Total), true
}

// SiteParities returns the per-seam parity rows, sorted by site name ASC (so filter precedes gate
// deterministically). Empty when no parity observation was seen.
func (r *Rollup) SiteParities() []SiteParity {
	if r == nil {
		return nil
	}
	sites := make([]string, 0, len(r.parity))
	for s := range r.parity {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	out := make([]SiteParity, 0, len(sites))
	for _, s := range sites {
		pc := r.parity[s]
		out = append(out, SiteParity{Site: s, Agree: pc.agree, Total: pc.total})
	}
	return out
}

// Empty reports whether the rollup saw NO legible.* events at all (the default OFF run). Report renders a
// one-line "instrument OFF / no events" message in this case rather than a wall of "n/a".
func (r *Rollup) Empty() bool {
	if r == nil {
		return true
	}
	return r.tags == 0 && len(r.novelHist) == 0 && len(r.parity) == 0
}

// Report renders the rollup as a plain-text block (no ANSI, no emoji — matches the trace/cost report
// style). It is the human-facing telemetry surface: the fast-path hit rate, the per-seam parity table,
// and the ranked novel-tag gap list. topN caps the histogram (<=0 ⇒ all rows). A rate with no
// denominator reads "n/a (no observations)"; an empty rollup reads one honest line.
func (r *Rollup) Report(topN int) string {
	var b strings.Builder
	b.WriteString("LEGIBLE-GENERATION TELEMETRY (WF-E CC-1 shadow instrument)\n")
	b.WriteString(strings.Repeat("-", 58) + "\n")
	if r.Empty() {
		b.WriteString("no legible.* events — the instrument was OFF (the default).\n")
		b.WriteString("flip seam.legible_generation ON and run a real-model pass to populate this.\n")
		return b.String()
	}

	// FAST-PATH HIT RATE
	if rate, ok := r.HitRate(); ok {
		b.WriteString(fmt.Sprintf("fast-path HIT RATE : %5.1f%%  (%d/%d tags op-Known and routable)\n",
			rate*100, r.known, r.tags))
	} else {
		b.WriteString("fast-path HIT RATE : n/a (no tags parsed)\n")
	}

	// PER-SEAM PARITY
	b.WriteString("\nper-seam PARITY (shadow route vs actual control-floor decision):\n")
	sps := r.SiteParities()
	if len(sps) == 0 {
		b.WriteString("  (no parity observations)\n")
	} else {
		for _, p := range sps {
			if rate, ok := p.Rate(); ok {
				b.WriteString(fmt.Sprintf("  %-8s %5.1f%%  (%d/%d agree)\n", p.Site, rate*100, p.Agree, p.Total))
			} else {
				b.WriteString(fmt.Sprintf("  %-8s n/a    (0 observations)\n", p.Site))
			}
		}
	}

	// NOVEL-TAG HISTOGRAM (the ranked scaling gap list)
	hist := r.NovelHistogram()
	b.WriteString(fmt.Sprintf("\nNOVEL-TAG histogram (the ranked registry-scaling gap list, %d distinct):\n", len(hist)))
	if len(hist) == 0 {
		b.WriteString("  (no novel: tags — every move mapped to a known op)\n")
	} else {
		shown := hist
		if topN > 0 && len(hist) > topN {
			shown = hist[:topN]
		}
		for i, e := range shown {
			b.WriteString(fmt.Sprintf("  %2d. %4d  novel:%s\n", i+1, e.Count, e.Desc))
		}
		if len(shown) < len(hist) {
			b.WriteString(fmt.Sprintf("  ... (%d more)\n", len(hist)-len(shown)))
		}
	}
	return b.String()
}

// -- tiny typed-field readers over the events.D map (nil-safe, type-guarded) -------------------------

// stringField reads a string-valued data key, returning "" when absent or not a string. Both the live
// bus path (Go-native string) and the JSONL-replay path (json string) land here as a string.
func stringField(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if v, ok := d[key].(string); ok {
		return v
	}
	return ""
}

// boolField reads a bool-valued data key, returning false when absent or not a bool. The live bus emits a
// Go bool; a JSONL replay decodes JSON true/false to a Go bool as well, so both paths land here directly.
func boolField(d map[string]any, key string) bool {
	if d == nil {
		return false
	}
	if v, ok := d[key].(bool); ok {
		return v
	}
	return false
}
