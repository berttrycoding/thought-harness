package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeJob(t *testing.T, spool string, id string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(spool, "job-"+id+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The claim protocol: oldest-first by NUMERIC id, result files never served,
// a claimed job is invisible to the next claimer, submit releases the claim.
func TestSpoolClaimProtocol(t *testing.T) {
	spool := t.TempDir()

	writeJob(t, spool, "2", `{"id":2}`)
	writeJob(t, spool, "10", `{"id":10}`)
	// A result file in the consume window must never be served as a job.
	if err := os.WriteFile(filepath.Join(spool, "job-1.result.json"), []byte(`{"content":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, ok := spoolClaimNext(spool)
	if !ok || raw != `{"id":2}` {
		t.Fatalf("first claim = %q ok=%v; want job 2 (numeric oldest, not job-10 lexical)", raw, ok)
	}
	// Job 2 is claimed: the next claim must see only job 10.
	raw, ok = spoolClaimNext(spool)
	if !ok || raw != `{"id":10}` {
		t.Fatalf("second claim = %q ok=%v; want job 10", raw, ok)
	}
	// Everything claimed: nothing pending.
	if _, ok := spoolClaimNext(spool); ok {
		t.Fatal("third claim succeeded; want none")
	}
	pending, claimed := spoolDepth(spool)
	if pending != 0 || claimed != 2 {
		t.Fatalf("depth = %d pending / %d claimed; want 0/2", pending, claimed)
	}

	// Submit releases the claim and writes the result.
	if err := spoolSubmit(spool, 2, "answer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(spool, "job-2.claimed")); !os.IsNotExist(err) {
		t.Fatal("claim marker survived submit")
	}
	if _, err := os.Stat(filepath.Join(spool, "job-2.result.json")); err != nil {
		t.Fatalf("result not written: %v", err)
	}
	pending, claimed = spoolDepth(spool)
	if pending != 0 || claimed != 1 {
		t.Fatalf("depth after submit = %d/%d; want 0/1", pending, claimed)
	}
}

// Stale-claim re-queue: a job claimed but left without a result past the staleness
// window (its worker died) is re-offered to the next claimer; a FRESH claim is not.
func TestSpoolReapStaleClaims(t *testing.T) {
	spool := t.TempDir()
	// A fresh claim (just now) and a stale claim (mtime well in the past).
	fresh := filepath.Join(spool, "job-1.claimed")
	stale := filepath.Join(spool, "job-2.claimed")
	if err := os.WriteFile(fresh, []byte(`{"id":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(`{"id":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-200 * time.Second)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	reapStaleClaims(spool, time.Now().Unix()) // 90s default window

	if _, err := os.Stat(filepath.Join(spool, "job-2.json")); err != nil {
		t.Fatalf("stale claim was not re-queued to pending: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh claim must NOT be reaped")
	}
	// The re-queued job is now claimable by the next worker.
	raw, ok := spoolClaimNext(spool)
	if !ok || raw != `{"id":2}` {
		t.Fatalf("re-queued job not claimable: %q ok=%v", raw, ok)
	}
}

// A worker on the PRE-claim protocol (read without rename) submits straight to
// the result file: spoolSubmit must clean the pending job file up too.
func TestSpoolSubmitPreClaimWorker(t *testing.T) {
	spool := t.TempDir()
	writeJob(t, spool, "5", `{"id":5}`)
	if err := spoolSubmit(spool, 5, "answer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(spool, "job-5.json")); !os.IsNotExist(err) {
		t.Fatal("pending job file survived a pre-claim submit")
	}
}

// normalizeSessionTool maps the cc_* back-compat aliases onto the canonical session_* names so one
// handler set serves any MCP worker (harness-agnostic). Unknown names pass through.
func TestNormalizeSessionTool(t *testing.T) {
	cases := map[string]string{
		"session_get_job":       "session_get_job",
		"session_submit_result": "session_submit_result",
		"session_status":        "session_status",
		"cc_get_job":            "session_get_job",       // back-compat alias
		"cc_submit_result":      "session_submit_result", // back-compat alias
		"cc_status":             "session_status",        // back-compat alias
		"something_else":        "something_else",        // unknown passes through
	}
	for in, want := range cases {
		if got := normalizeSessionTool(in); got != want {
			t.Errorf("normalizeSessionTool(%q) = %q, want %q", in, got, want)
		}
	}
}
