package persist

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestJSONLStoreRoundTrip: every artifact saved into a JSONL store reconstructs EXACTLY after a reopen —
// the cross-session persistence contract (M4 §4.4). A fresh store at the same dir loads the saved set.
func TestJSONLStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	grounded := Meta{Grounded: true, LastUsedTick: 5, UseCount: 1, Status: StatusActive}

	must(t, s.SaveSkill(SkillRecord{Meta: grounded, Name: "diagnose-x", Tier: "composite",
		Triggers: []string{"diagnose"}, Body: map[string]any{"kind": "step", "operator": "hypothesize"}, Description: "learned"}))
	must(t, s.SaveOperator(OpRecord{Meta: grounded, Name: "triangulate", Family: "relational", Intent: "cross three sources", Move: "reframe"}))
	must(t, s.SaveSpecialist(SpecialistRecord{Meta: grounded, Domain: "learned:cache ttl", GoalKey: "cache ttl",
		Triggers: []string{"cache"}, Answer: "the TTL is 60s", Relevance: 0.9, Generated: 3, Value: 0.8}))
	must(t, s.SaveGatePriors(map[string]float64{"compute": 0.2}, 5))
	must(t, s.SaveEpisode(EpisodeRecord{Meta: grounded, Goal: "what's 9x9", Outcome: "81", Value: 0.9, Tick: 4}))
	must(t, s.SaveBelief(BeliefRecord{Meta: grounded, Statement: "9x9 is 81", Source: "compute", ValidFrom: 4}))
	must(t, s.SaveKnowledge(KnowledgeRecord{Meta: grounded, Statement: "go fmt is idempotent", Kind: "fact", Source: "reality:run_shell", Trust: 0.9, ValidFrom: 4}))
	must(t, s.SavePreference(PreferenceRecord{Meta: grounded, Trait: "verbosity", Value: "terse", Count: 3, Learned: true}))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// reopen a FRESH store at the same dir and load — the saved set must reconstruct verbatim.
	s2, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("reopen NewJSONLStore: %v", err)
	}
	snap, err := s2.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Skills) != 1 || snap.Skills[0].Name != "diagnose-x" {
		t.Fatalf("skill not round-tripped: %+v", snap.Skills)
	}
	if len(snap.Operators) != 1 || snap.Operators[0].Move != "reframe" {
		t.Fatalf("operator not round-tripped: %+v", snap.Operators)
	}
	if len(snap.Specialists) != 1 || snap.Specialists[0].Answer != "the TTL is 60s" {
		t.Fatalf("specialist not round-tripped: %+v", snap.Specialists)
	}
	if snap.Priors == nil || snap.Priors.Priors["compute"] != 0.2 {
		t.Fatalf("gate priors not round-tripped: %+v", snap.Priors)
	}
	if len(snap.Episodes) != 1 || snap.Episodes[0].Outcome != "81" {
		t.Fatalf("episode not round-tripped: %+v", snap.Episodes)
	}
	if len(snap.Beliefs) != 1 || snap.Beliefs[0].Statement != "9x9 is 81" {
		t.Fatalf("belief not round-tripped: %+v", snap.Beliefs)
	}
	if len(snap.Knowledge) != 1 || snap.Knowledge[0].Kind != "fact" {
		t.Fatalf("knowledge not round-tripped: %+v", snap.Knowledge)
	}
	if len(snap.Preferences) != 1 || !snap.Preferences[0].Learned {
		t.Fatalf("preference not round-tripped: %+v", snap.Preferences)
	}
}

// TestStoreNeverFabricate: SaveEpisode/SaveBelief/SaveKnowledge REJECT an ungrounded record — a
// fabricated record never reaches disk (the §6 never-fabricate invariant, enforced at the persistence
// boundary), and Load drops any ungrounded row that somehow exists.
func TestStoreNeverFabricate(t *testing.T) {
	s, _ := NewJSONLStore(t.TempDir())
	ungrounded := Meta{Grounded: false, Status: StatusActive}
	if err := s.SaveEpisode(EpisodeRecord{Meta: ungrounded, Goal: "g", Outcome: "o"}); !errors.Is(err, ErrUngrounded) {
		t.Fatalf("SaveEpisode must reject ungrounded, got %v", err)
	}
	if err := s.SaveBelief(BeliefRecord{Meta: ungrounded, Statement: "b"}); !errors.Is(err, ErrUngrounded) {
		t.Fatalf("SaveBelief must reject ungrounded, got %v", err)
	}
	if err := s.SaveKnowledge(KnowledgeRecord{Meta: ungrounded, Statement: "k"}); !errors.Is(err, ErrUngrounded) {
		t.Fatalf("SaveKnowledge must reject ungrounded, got %v", err)
	}
	snap := s.Snapshot()
	if len(snap.Episodes)+len(snap.Beliefs)+len(snap.Knowledge) != 0 {
		t.Fatalf("ungrounded records must not be stored: %+v", snap)
	}
}

// TestStoreDedupAndVersion: an identical re-mint is idempotent (no growth); a same-name re-mint with a
// DIFFERENT body versions the record (Version+1), keeping the created tick (versioning, §4.5 step 1).
func TestStoreDedupAndVersion(t *testing.T) {
	s, _ := NewJSONLStore(t.TempDir())
	m := Meta{Grounded: true, LastUsedTick: 1, Status: StatusActive}
	sk := SkillRecord{Meta: m, Name: "s1", Tier: "unit", Triggers: []string{"x"}, Body: map[string]any{"kind": "step", "operator": "generate"}}
	must(t, s.SaveSkill(sk))
	must(t, s.SaveSkill(sk)) // identical: idempotent
	if n := len(s.Snapshot().Skills); n != 1 {
		t.Fatalf("identical re-mint should be idempotent, got %d skills", n)
	}
	sk2 := sk
	sk2.Body = map[string]any{"kind": "step", "operator": "decompose"} // body changed
	must(t, s.SaveSkill(sk2))
	skills := s.Snapshot().Skills
	if len(skills) != 1 || skills[0].Meta.Version != 1 {
		t.Fatalf("a body-changed re-mint should version in place (Version=1), got %+v", skills)
	}
}

// TestEmptyDirRejected: a JSONL store cannot be built on an empty dir (a clear config error, not a
// silent CWD write).
func TestEmptyDirRejected(t *testing.T) {
	if _, err := NewJSONLStore(""); err == nil {
		t.Fatal("NewJSONLStore must reject an empty dir")
	}
}

// TestColdStartIsNotAnError: loading a fresh (empty) dir is a clean cold start — no error, empty snapshot.
func TestColdStartIsNotAnError(t *testing.T) {
	s, _ := NewJSONLStore(filepath.Join(t.TempDir(), "fresh"))
	snap, err := s.Load()
	if err != nil {
		t.Fatalf("cold start must not error: %v", err)
	}
	if snap == nil || len(snap.Skills) != 0 {
		t.Fatalf("cold start snapshot must be empty, got %+v", snap)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
}
