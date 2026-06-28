package action

import "testing"

// mockShell stands in for run_shell so these tests NEVER run a real shell. It only records whether it
// was executed — which lets us prove the safety gate denies a dangerous command BEFORE the tool runs.
// The dangerous command string is therefore only ever *data* handed to the gate; no filesystem is
// ever touched, even in the impossible case that the gate failed to fire.
type mockShell struct{ called *bool }

func (m mockShell) Name() string               { return "run_shell" } // a CommandTool ⇒ the evaluate gate applies
func (m mockShell) Description() string        { return "mock shell (test double — never executes)" }
func (m mockShell) Parameters() map[string]any { return map[string]any{} }
func (m mockShell) Category() TaxClass         { return classifyCall("run_shell") } // gap 6: execute/local
func (m mockShell) Execute(map[string]any) ToolResult {
	*m.called = true
	return ToolResult{Name: "run_shell", Content: "(mock ran — must NOT happen for a blocked command)"}
}

// TestExecutor_SafetyGate_BlocksCompoundBeforeExecution proves the WIRING: a ToolExecutor built with
// the command gate (Evaluate: DefaultEvaluate) denies a dangerous COMPOUND command through the real
// executor pipeline — and does so BEFORE the tool runs (the mock is never called). This is the live
// end-to-end check the engine's buildExecutor now satisfies, done safely via the mock.
func TestExecutor_SafetyGate_BlocksCompoundBeforeExecution(t *testing.T) {
	called := false
	exec := NewToolExecutor(
		NewToolRegistry([]Tool{mockShell{called: &called}}),
		&ExecutorOptions{Evaluate: DefaultEvaluate},
	)

	// A catastrophe hidden in a compound line — exactly the case the tokenizer added. Only data.
	res := exec.Execute(ToolCall{
		Name: "run_shell",
		Args: map[string]any{"command": "rm -rf / && echo done"},
	})

	if !res.IsError || res.ErrorCode != ErrSafetyBlock {
		t.Fatalf("expected a safety_block denial, got IsError=%v code=%q content=%q",
			res.IsError, res.ErrorCode, res.Content)
	}
	if called {
		t.Fatal("UNSAFE: the tool executed — the gate did NOT block before execution")
	}
}

// TestExecutor_NoGate_DoesNotBlock documents the bug the wiring fixes: with NO Evaluate gate (the
// previous live state), the same dangerous command is NOT denied and the (mock) tool runs. This is
// what made run_shell content-ungated. Still safe — it's the mock, not a real shell.
func TestExecutor_NoGate_DoesNotBlock(t *testing.T) {
	called := false
	exec := NewToolExecutor(
		NewToolRegistry([]Tool{mockShell{called: &called}}),
		&ExecutorOptions{}, // no Evaluate — the unwired state
	)

	res := exec.Execute(ToolCall{
		Name: "run_shell",
		Args: map[string]any{"command": "rm -rf / && echo done"},
	})

	if res.ErrorCode == ErrSafetyBlock {
		t.Fatal("did not expect a safety_block with the gate off")
	}
	if !called {
		t.Fatal("expected the unguarded (mock) tool to run — confirming the gate is what protects us")
	}
}
