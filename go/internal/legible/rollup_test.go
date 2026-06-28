package legible

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// tagEv / parityEv / novelEv build the three legible.* events with exactly the data keys shadow.go emits,
// so the rollup is tested against the REAL wire shape (not a guessed one).
func tagEv(known bool) events.Event {
	return events.Event{Kind: events.LegibleTag, Data: events.D{"known": known}}
}
func parityEv(site string, agree bool) events.Event {
	return events.Event{Kind: events.LegibleParity, Data: events.D{"site": site, "agree": agree}}
}
func novelEv(desc string) events.Event {
	return events.Event{Kind: events.LegibleNovel, Data: events.D{"desc": desc}}
}

// TestHitRate: the fast-path hit rate is known/tags. 3 known of 5 tags ⇒ 60%.
func TestHitRate(t *testing.T) {
	r := RollupOf([]events.Event{
		tagEv(true), tagEv(true), tagEv(false), tagEv(true), tagEv(false),
	})
	rate, ok := r.HitRate()
	if !ok {
		t.Fatal("HitRate ok=false with tags present")
	}
	if rate != 0.6 {
		t.Errorf("HitRate = %v, want 0.6", rate)
	}
	if r.Tags() != 5 || r.Known() != 3 {
		t.Errorf("Tags/Known = %d/%d, want 5/3", r.Tags(), r.Known())
	}
}

// TestHitRateNoDenominator: no tags ⇒ HitRate ok=false (n/a, never a divide-by-zero).
func TestHitRateNoDenominator(t *testing.T) {
	r := NewRollup()
	if _, ok := r.HitRate(); ok {
		t.Error("HitRate ok=true with zero tags — must be n/a")
	}
}

// TestNovelHistogramRanked: the histogram counts each novel:<desc> and ranks by count DESC, then desc ASC.
// A blank desc folds under "(unspecified)" so it still counts as a gap.
func TestNovelHistogramRanked(t *testing.T) {
	r := RollupOf([]events.Event{
		novelEv("triangulate"),
		novelEv("triangulate"),
		novelEv("triangulate"),
		novelEv("steelman"),
		novelEv("steelman"),
		novelEv("backcast"),
		novelEv(""), // blank -> (unspecified)
	})
	h := r.NovelHistogram()
	want := []NovelEntry{
		{Desc: "triangulate", Count: 3},
		{Desc: "steelman", Count: 2},
		{Desc: "(unspecified)", Count: 1},
		{Desc: "backcast", Count: 1},
	}
	if len(h) != len(want) {
		t.Fatalf("histogram len = %d, want %d (%v)", len(h), len(want), h)
	}
	for i := range want {
		if h[i] != want[i] {
			t.Errorf("rank %d = %+v, want %+v", i, h[i], want[i])
		}
	}
}

// TestPerSeamParity: parity is bucketed per site; the rate is agree/total within a site, and the rows are
// sorted by site name (filter before gate).
func TestPerSeamParity(t *testing.T) {
	r := RollupOf([]events.Event{
		parityEv("filter", true),
		parityEv("filter", true),
		parityEv("filter", false), // filter: 2/3
		parityEv("gate", true),    // gate: 1/2
		parityEv("gate", false),
	})
	sps := r.SiteParities()
	if len(sps) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sps))
	}
	if sps[0].Site != "filter" || sps[1].Site != "gate" {
		t.Fatalf("site order = %q,%q, want filter,gate", sps[0].Site, sps[1].Site)
	}
	if r0, _ := sps[0].Rate(); r0 != 2.0/3.0 {
		t.Errorf("filter parity = %v, want 2/3", r0)
	}
	if r1, _ := sps[1].Rate(); r1 != 0.5 {
		t.Errorf("gate parity = %v, want 0.5", r1)
	}
}

// TestEmptyRollupHonest: a rollup over zero legible.* events is Empty, and its Report says so in one line
// rather than printing a wall of n/a (the default OFF run).
func TestEmptyRollupHonest(t *testing.T) {
	r := RollupOf(nil)
	if !r.Empty() {
		t.Fatal("a rollup over no events must be Empty")
	}
	rep := r.Report(0)
	if !strings.Contains(rep, "no legible.* events") {
		t.Errorf("empty report must say the instrument was OFF, got:\n%s", rep)
	}
}

// TestRollupIgnoresNonLegible: a mixed stream (non-legible events interleaved) folds only the legible.*
// events — a full run can be passed straight through without filtering.
func TestRollupIgnoresNonLegible(t *testing.T) {
	r := RollupOf([]events.Event{
		{Kind: events.Generate, Data: events.D{"text": "hi"}},
		tagEv(true),
		{Kind: events.Tick},
		parityEv("filter", true),
		{Kind: events.LegibleTag}, // missing data -> counts as a tag, not known
	})
	if r.Tags() != 2 || r.Known() != 1 {
		t.Errorf("Tags/Known = %d/%d, want 2/1 (non-legible ignored, missing-data tag counts)", r.Tags(), r.Known())
	}
}

// TestReportShape: a populated report carries all three surfaces (hit rate, per-seam parity, novel
// histogram) with the topN cap honored.
func TestReportShape(t *testing.T) {
	r := RollupOf([]events.Event{
		tagEv(true), tagEv(false),
		parityEv("filter", true),
		novelEv("a"), novelEv("a"), novelEv("b"), novelEv("c"),
	})
	rep := r.Report(2)
	for _, want := range []string{"HIT RATE", "PARITY", "filter", "NOVEL-TAG histogram", "novel:a"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q:\n%s", want, rep)
		}
	}
	// topN=2 over 3 distinct novel descs ⇒ a "... more" line.
	if !strings.Contains(rep, "more") {
		t.Errorf("report should cap at topN=2 and note the remainder:\n%s", rep)
	}
	if strings.Contains(rep, "novel:c") {
		t.Errorf("topN=2 must not show the 3rd novel desc:\n%s", rep)
	}
}

// TestNilRollupSafe: every method on a nil *Rollup is a safe no-op.
func TestNilRollupSafe(t *testing.T) {
	var r *Rollup
	r.Observe(tagEv(true)) // must not panic
	if !r.Empty() {
		t.Error("nil Rollup must report Empty")
	}
	if _, ok := r.HitRate(); ok {
		t.Error("nil Rollup HitRate must be n/a")
	}
	if r.NovelHistogram() != nil || r.SiteParities() != nil {
		t.Error("nil Rollup slices must be nil")
	}
}
