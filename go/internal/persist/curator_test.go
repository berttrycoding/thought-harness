package persist

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// captureEmit returns an events.Emit closure that increments *n on every emit (so a test can assert the
// curator announced its actions).
func captureEmit(n *int) events.Emit {
	return func(kind, summary string, data map[string]any) events.Event {
		*n++
		return events.Event{Kind: kind, Summary: summary, Data: data}
	}
}

// statusOf finds a record's status by id (name/statement) in a curated snapshot.
func skillStatus(sn *Snapshot, name string) Status {
	for _, r := range sn.Skills {
		if r.Name == name {
			return r.Meta.Status
		}
	}
	return ""
}

func beliefStatus(sn *Snapshot, stmt string) (Status, bool) {
	for _, r := range sn.Beliefs {
		if r.Statement == stmt {
			return r.Meta.Status, true
		}
	}
	return "", false
}

// TestCuratorDecayAndGC: a recent high-use record stays active; an old zero-use one decays to dormant
// then (past the TTL) archives — deterministically given (snapshot, tick). The fake tick fixture proves
// the curator is PURE over records + the seeded tick (no wall-clock).
func TestCuratorDecayAndGC(t *testing.T) {
	c := NewCurator(&CuratorConfig{
		HalfLife: 100, DormantAt: 0.2, ArchiveTTL: 500,
		MaxSkills: 100, MaxBeliefs: 100, MaxKnow: 100, MaxEpisodes: 100, MaxOps: 100, MaxSpecs: 100,
	}, nil)

	sn := &Snapshot{Skills: []SkillRecord{
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 950, UseCount: 5, Hash: "fresh"}, Name: "fresh"},
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 10, UseCount: 0, Hash: "stale"}, Name: "stale"},
	}}
	now := 1000

	out := c.Curate(sn, now)
	if got := skillStatus(out, "fresh"); got != StatusActive {
		t.Fatalf("recent high-use skill should stay active, got %q", got)
	}
	// the stale one is far past the archive TTL (age 990 > 500) with zero use → it decays AND archives in
	// one pass (the pipeline applies decay then GC in order).
	if got := skillStatus(out, "stale"); got != StatusArchived {
		t.Fatalf("old zero-use skill should be archived, got %q", got)
	}

	// determinism: a second identical Curate over the same input + tick yields the same statuses.
	out2 := c.Curate(sn, now)
	if skillStatus(out2, "stale") != StatusArchived || skillStatus(out2, "fresh") != StatusActive {
		t.Fatal("curator must be deterministic given (snapshot, tick)")
	}
}

// TestCuratorDedup: two records with the same content Hash collapse to one, summing UseCount.
func TestCuratorDedup(t *testing.T) {
	c := NewCurator(nil, nil)
	sn := &Snapshot{Operators: []OpRecord{
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 100, UseCount: 2, Hash: "h"}, Name: "op"},
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 100, UseCount: 3, Hash: "h"}, Name: "op"},
	}}
	out := c.Curate(sn, 100)
	if len(out.Operators) != 1 {
		t.Fatalf("identical-hash operators should collapse to 1, got %d", len(out.Operators))
	}
	if out.Operators[0].Meta.UseCount != 5 {
		t.Fatalf("dedup should sum UseCount (2+3=5), got %d", out.Operators[0].Meta.UseCount)
	}
}

// TestCuratorDemotesRefuted: a refuted belief (ValidTo!=0) flips to demoted (excluded from recall, kept
// for audit) — stage 4. A currently-valid belief stays active.
func TestCuratorDemotesRefuted(t *testing.T) {
	c := NewCurator(nil, nil)
	sn := &Snapshot{Beliefs: []BeliefRecord{
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 100, UseCount: 1, Hash: "a"}, Statement: "valid", ValidFrom: 1, ValidTo: 0},
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 100, UseCount: 1, Hash: "b"}, Statement: "refuted", ValidFrom: 1, ValidTo: 50},
	}}
	out := c.Curate(sn, 100)
	if st, _ := beliefStatus(out, "valid"); st != StatusActive {
		t.Fatalf("a currently-valid belief should stay active, got %q", st)
	}
	if st, _ := beliefStatus(out, "refuted"); st != StatusDemoted {
		t.Fatalf("a refuted belief (ValidTo set) should be demoted, got %q", st)
	}
}

// TestCuratorSizeCap: when active records exceed the cap, the LOWEST-scoring active ones archive first
// (size caps, §4.5 step 6). With a cap of 2 and 3 active episodes of descending recency, the oldest
// archives.
func TestCuratorSizeCap(t *testing.T) {
	c := NewCurator(&CuratorConfig{
		HalfLife: 100, DormantAt: 0.0, ArchiveTTL: 1 << 30, // disable decay/GC so the cap is the only actor
		MaxEpisodes: 2, MaxBeliefs: 100, MaxKnow: 100, MaxSkills: 100, MaxOps: 100, MaxSpecs: 100,
	}, nil)
	sn := &Snapshot{Episodes: []EpisodeRecord{
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 100, UseCount: 1, Hash: "e1"}, Goal: "recent"},
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 90, UseCount: 1, Hash: "e2"}, Goal: "mid"},
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 10, UseCount: 1, Hash: "e3"}, Goal: "oldest"},
	}}
	out := c.Curate(sn, 100)
	active := 0
	var archived string
	for _, r := range out.Episodes {
		if r.Meta.Status == StatusActive {
			active++
		}
		if r.Meta.Status == StatusArchived {
			archived = r.Goal
		}
	}
	if active != 2 {
		t.Fatalf("size cap of 2 should leave 2 active, got %d", active)
	}
	if archived != "oldest" {
		t.Fatalf("the lowest-scoring (oldest) episode should archive first, got %q", archived)
	}
}

// TestCuratorEmitsCurate: the curator announces each action on the bus (persist.curate), so cleanup is
// observable.
func TestCuratorEmitsCurate(t *testing.T) {
	var saw int
	c := NewCurator(&CuratorConfig{HalfLife: 100, DormantAt: 0.2, ArchiveTTL: 500, MaxBeliefs: 100}, captureEmit(&saw))
	sn := &Snapshot{Beliefs: []BeliefRecord{
		{Meta: Meta{Status: StatusActive, Grounded: true, LastUsedTick: 1, UseCount: 0, Hash: "a"}, Statement: "stale", ValidFrom: 1, ValidTo: 50},
	}}
	c.Curate(sn, 1000)
	if saw == 0 {
		t.Fatal("the curator should emit at least one persist.curate (a demotion + GC happened)")
	}
}
