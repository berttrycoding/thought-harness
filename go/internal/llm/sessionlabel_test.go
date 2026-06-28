package llm

// sessionlabel_test.go — the session bridge is harness-agnostic: its provenance label is the
// servicing worker's identity (THOUGHT_SESSION_WORKER), de-branded "session" by default — never
// the hardcoded "cc:" (Claude Code) it used to be.

import "testing"

func TestSessionLabelHarnessAgnostic(t *testing.T) {
	t.Setenv("THOUGHT_SESSION_WORKER", "")
	if got := sessionLabel("session"); got != "session" {
		t.Errorf("default label = %q, want de-branded \"session\" (not cc:*)", got)
	}
	if got := sessionLabel("session-utility"); got != "session-utility" {
		t.Errorf("default utility label = %q, want \"session-utility\"", got)
	}

	t.Setenv("THOUGHT_SESSION_WORKER", "opencode")
	if got := sessionLabel("session"); got != "opencode:session" {
		t.Errorf("opencode worker label = %q, want \"opencode:session\"", got)
	}

	t.Setenv("THOUGHT_SESSION_WORKER", "cc")
	if got := sessionLabel("session"); got != "cc:session" {
		t.Errorf("legacy cc label = %q, want \"cc:session\" (continuity when explicitly requested)", got)
	}
}

// The session bridge's class is "session" regardless of the worker label (ClassOf reads the
// stamped class, never the label) — so a harness-agnostic label change cannot break substrate
// resolution.
func TestSessionBridgeClassStableAcrossWorkers(t *testing.T) {
	t.Setenv("THOUGHT_SESSION_WORKER", "opencode")
	be := NewSessionBridge("", 0, 0)
	if got := ClassOf(be); got != "session" {
		t.Errorf("ClassOf(session bridge) = %q, want \"session\" regardless of worker", got)
	}
}
