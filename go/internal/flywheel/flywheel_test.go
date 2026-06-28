package flywheel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestRecorderBackfillsTerminalOutcome is the core invariant: a decision's grounded outcome is unknown at
// decision time and is BACKFILLED onto every tuple of the trajectory at episode close (the Monte-Carlo
// return assignment) — and the label is the INDEPENDENT terminal signal, never set per-decision.
func TestRecorderBackfillsTerminalOutcome(t *testing.T) {
	mem := NewMemSink()
	rec := NewRecorder(mem, nil)

	rec.OpenEpisode("ep-1")
	rec.RecordDecision(1, StateFeatures{BranchID: 1, Value: 0.5, Mode: "reactive"}, "THINK")
	rec.RecordDecision(2, StateFeatures{BranchID: 1, Value: 0.6}, "BRANCH")
	rec.RecordDecision(3, StateFeatures{BranchID: 2, Value: 0.7}, "ACT")
	if got := rec.Pending(); got != 3 {
		t.Fatalf("Pending() = %d before close, want 3", got)
	}
	if got := len(mem.All()); got != 0 {
		t.Fatalf("sink has %d tuples before close, want 0 (outcome not yet known)", got)
	}

	out := Outcome{GReturn: 1.0, GoalMet: true, StopKind: "GOAL_MET", EpisodeGrounded: true, GroundedObs: 2, RefutedObs: 0}
	rec.CloseEpisode(out)

	all := mem.All()
	if len(all) != 3 {
		t.Fatalf("got %d tuples after close, want 3", len(all))
	}
	for i, tup := range all {
		if !tup.Filled {
			t.Errorf("tuple[%d] not Filled after close", i)
		}
		if tup.Outcome != out {
			t.Errorf("tuple[%d] outcome = %+v, want %+v (the terminal label must backfill uniformly)", i, tup.Outcome, out)
		}
		if tup.Episode != "ep-1" {
			t.Errorf("tuple[%d] episode = %q, want ep-1", i, tup.Episode)
		}
		if tup.Step != i {
			t.Errorf("tuple[%d] step = %d, want %d (within-episode ordering)", i, tup.Step, i)
		}
	}
	// the action sequence is preserved in capture order
	wantActions := []string{"THINK", "BRANCH", "ACT"}
	for i, a := range wantActions {
		if all[i].Action != a {
			t.Errorf("tuple[%d] action = %q, want %q", i, all[i].Action, a)
		}
	}
}

// TestRecorderEmpty: an episode that decides nothing produces no rows (close is a no-op).
func TestRecorderEmpty(t *testing.T) {
	mem := NewMemSink()
	rec := NewRecorder(mem, nil)
	rec.OpenEpisode("ep-empty")
	rec.CloseEpisode(Outcome{GReturn: 0.0, StopKind: "GIVE_UP"})
	if got := len(mem.All()); got != 0 {
		t.Fatalf("empty episode produced %d tuples, want 0", got)
	}
}

// TestRecorderInterruptedEpisodeFlushesUnfilled: an episode that opens a new one without closing the prior
// flushes the prior's decisions as UNFILLED so nothing is silently dropped.
func TestRecorderInterruptedEpisodeFlushesUnfilled(t *testing.T) {
	mem := NewMemSink()
	rec := NewRecorder(mem, nil)
	rec.OpenEpisode("ep-1")
	rec.RecordDecision(1, StateFeatures{BranchID: 1}, "THINK")
	// no CloseEpisode — a fresh episode supersedes
	rec.OpenEpisode("ep-2")
	all := mem.All()
	if len(all) != 1 {
		t.Fatalf("got %d flushed tuples, want 1 (the interrupted episode's decision)", len(all))
	}
	if all[0].Filled {
		t.Errorf("interrupted tuple should be UNFILLED (no terminal outcome), got Filled=true")
	}
	if all[0].Episode != "ep-1" {
		t.Errorf("flushed tuple episode = %q, want ep-1", all[0].Episode)
	}
}

// TestNilRecorderIsNoOp: the OFF path builds a nil Recorder; every method must be a safe no-op (the
// byte-identical default — the engine calls these unconditionally on the spine).
func TestNilRecorderIsNoOp(t *testing.T) {
	var rec *Recorder
	rec.OpenEpisode("x")
	rec.RecordDecision(1, StateFeatures{}, "THINK")
	rec.CloseEpisode(Outcome{})
	if rec.Pending() != 0 {
		t.Fatalf("nil recorder Pending() != 0")
	}
}

// TestJSONLSinkDeterministic: the JSONL sink writes one JSON object per line with stable field order, and
// the dataset is reproducible (same input ⇒ byte-identical output) — the determinism contract (§5).
func TestJSONLSinkDeterministic(t *testing.T) {
	write := func() string {
		var buf bytes.Buffer
		sink := NewJSONLSink(&buf)
		rec := NewRecorder(sink, nil)
		rec.OpenEpisode("ep-det")
		rec.RecordDecision(1, StateFeatures{BranchID: 1, Value: 0.5, Theta: 0.3, N: 0.2, Mu: 0.1, Arousal: "AWAKE", Mode: "continuous"}, "THINK")
		rec.RecordDecision(2, StateFeatures{BranchID: 1, Value: 0.6, PendingUser: true}, "DELIVER")
		rec.CloseEpisode(Outcome{GReturn: 1.0, GoalMet: true, StopKind: "GOAL_MET", EpisodeGrounded: true, GroundedObs: 1})
		_ = sink.Flush()
		return buf.String()
	}
	a, b := write(), write()
	if a != b {
		t.Fatalf("JSONL output not reproducible:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	lines := strings.Split(strings.TrimRight(a, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d JSONL lines, want 2", len(lines))
	}
	// each line is valid JSON and round-trips to a DecisionTuple
	for i, ln := range lines {
		var tup DecisionTuple
		if err := json.Unmarshal([]byte(ln), &tup); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
		if !tup.Filled {
			t.Errorf("line %d not Filled", i)
		}
		if tup.Outcome.StopKind != "GOAL_MET" {
			t.Errorf("line %d StopKind = %q, want GOAL_MET", i, tup.Outcome.StopKind)
		}
	}
}

// TestEmitHookFiresPerFinalisedTuple: when an emit hook is set, it fires once per finalised tuple at close
// (the observability contract — flywheel.capture on the bus).
func TestEmitHookFiresPerFinalisedTuple(t *testing.T) {
	var fired []DecisionTuple
	rec := NewRecorder(NewMemSink(), func(t DecisionTuple) { fired = append(fired, t) })
	rec.OpenEpisode("ep-emit")
	rec.RecordDecision(1, StateFeatures{}, "THINK")
	rec.RecordDecision(2, StateFeatures{}, "STOP")
	if len(fired) != 0 {
		t.Fatalf("emit fired %d times before close, want 0", len(fired))
	}
	rec.CloseEpisode(Outcome{StopKind: "GOAL_MET", GReturn: 1, GoalMet: true})
	if len(fired) != 2 {
		t.Fatalf("emit fired %d times at close, want 2", len(fired))
	}
	for i, tup := range fired {
		if !tup.Filled {
			t.Errorf("emitted tuple[%d] not Filled", i)
		}
	}
}
