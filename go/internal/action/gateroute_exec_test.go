package action

import "testing"

// mockWrite stands in for a file-modifying (mutate) tool so these tests never touch the filesystem. It
// records whether it ran — letting us prove the gate-router denies an unauthored world-change BEFORE the
// tool runs. mockRead is an inspect tool (a free local sense).
type mockWrite struct {
	name   string
	called *bool
}

func (m mockWrite) Name() string               { return m.name }
func (m mockWrite) Description() string        { return "mock write (test double — never executes)" }
func (m mockWrite) Parameters() map[string]any { return map[string]any{} }
func (m mockWrite) Category() TaxClass         { return classifyCall(m.name) } // gap 6: by real builtin name
func (m mockWrite) Execute(map[string]any) ToolResult {
	*m.called = true
	return ToolResult{Name: m.name, Content: "(mock ran)"}
}

type mockRead struct{ called *bool }

func (m mockRead) Name() string               { return "read_file" }
func (m mockRead) Description() string        { return "mock read" }
func (m mockRead) Parameters() map[string]any { return map[string]any{} }
func (m mockRead) Category() TaxClass         { return classifyCall("read_file") } // gap 6: inspect/local
func (m mockRead) Execute(map[string]any) ToolResult {
	*m.called = true
	return ToolResult{Name: "read_file", Content: "ok"}
}

// TestGateRouter_AuthoringAndRefusals pins slice (j) / 03 §3: with the router enabled (Bounds set),
//   - an UNAUTHORED world-change (write_file) is denied before it runs,
//   - the SAME write AUTHORED by the conscious passes the gate,
//   - a self-substrate mutate is refused regardless of authoring (§4 invariant),
//   - a read (local sense) is free,
//
// and with the router OFF (nil Bounds) an unauthored write runs (byte-identical to before).
func TestGateRouter_AuthoringAndRefusals(t *testing.T) {
	mkExec := func(called *bool, withRouter bool) *ToolExecutor {
		// write_file is a real FileModifyTool name (mutate); the sandbox is off here so only the router gates.
		tools := []Tool{mockWrite{name: "write_file", called: called}}
		opts := &ExecutorOptions{}
		if withRouter {
			opts.Bounds = &RouteBounds{NetworkEnabled: false, NetworkQuota: 0}
		}
		return NewToolExecutor(NewToolRegistry(tools), opts)
	}

	// --- unauthored world-change -> denied, tool never runs ---
	called := false
	res := mkExec(&called, true).Execute(ToolCall{Name: "write_file", Args: map[string]any{"path": "out.txt"}})
	if !res.IsError || res.ErrorCode != ErrBlocked {
		t.Fatalf("unauthored write: expected an ErrBlocked denial, got IsError=%v code=%q", res.IsError, res.ErrorCode)
	}
	if called {
		t.Fatal("unauthored write executed — the router did NOT block before execution")
	}

	// --- authored world-change -> passes the router, runs ---
	called = false
	res = mkExec(&called, true).Execute(ToolCall{Name: "write_file", Args: map[string]any{"path": "out.txt"}, Authored: true})
	if res.IsError {
		t.Fatalf("authored write: expected to pass the gate, got denial code=%q content=%q", res.ErrorCode, res.Content)
	}
	if !called {
		t.Fatal("authored write: the tool should have run")
	}

	// --- self-substrate mutate -> refused even when authored (§4) ---
	called = false
	res = mkExec(&called, true).Execute(ToolCall{Name: "write_file", Args: map[string]any{"path": "data/registry/specialists.jsonl"}, Authored: true})
	if !res.IsError || res.ErrorCode != ErrBlocked {
		t.Fatalf("self-mutate: expected an ErrBlocked refusal, got IsError=%v code=%q", res.IsError, res.ErrorCode)
	}
	if called {
		t.Fatal("self-substrate write executed — the §4 refusal did NOT fire")
	}

	// --- a read is a free local sense -> runs without authoring ---
	rcalled := false
	rres := NewToolExecutor(NewToolRegistry([]Tool{mockRead{called: &rcalled}}),
		&ExecutorOptions{Bounds: &RouteBounds{}}).Execute(ToolCall{Name: "read_file", Args: map[string]any{"path": "x.txt"}})
	if rres.IsError || !rcalled {
		t.Fatalf("read: expected a free local-sense run, got IsError=%v called=%v", rres.IsError, rcalled)
	}

	// --- router OFF (nil Bounds) -> an unauthored write runs (byte-identical to before) ---
	offCalled := false
	offRes := mkExec(&offCalled, false).Execute(ToolCall{Name: "write_file", Args: map[string]any{"path": "out.txt"}})
	if offRes.IsError && offRes.ErrorCode == ErrBlocked {
		t.Fatal("router OFF: an unauthored write must not be router-blocked")
	}
	if !offCalled {
		t.Fatal("router OFF: the tool should have run (pipeline unchanged)")
	}
}
