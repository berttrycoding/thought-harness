package action

import "testing"

// TestProtectedCorePredicate pins the anti-wireheading predicate (#30, §2.8): a mutate targeting a
// protected-core root is refused; a read of it is allowed (introspection); a non-core path is not
// protected; segment boundaries are respected.
func TestProtectedCorePredicate(t *testing.T) {
	cases := []struct {
		op     Operation
		target string
		want   bool
	}{
		{OpMutate, ".thought/identity/covenant.md", true},        // immutable identity
		{OpMutate, "data/registry/eval-core/honesty.json", true}, // a core measuring stick
		{OpMutate, "data/core/regulator.json", true},             // generic kernel root
		{OpInspect, ".thought/identity/covenant.md", false},      // a READ of the core is allowed (introspection)
		{OpMutate, "data/core-extra/x", false},                   // segment boundary: not the core
		{OpMutate, "src/main.go", false},                         // an ordinary world write
		{OpMutate, "data/registry/specialists.jsonl", false},     // self-substrate, not protected core (refused elsewhere)
	}
	for _, c := range cases {
		if got := RefuseProtectedCoreMutation(c.op, c.target); got != c.want {
			t.Errorf("RefuseProtectedCoreMutation(%s, %q) = %v, want %v", c.op, c.target, got, c.want)
		}
	}
}

// TestProtectedCoreExecutorRefusal pins the live wiring: with the gate-router on, a write to a
// protected-core path is refused with the anti-wireheading reason BEFORE the tool runs — even when the
// conscious authored it (the core is read-only to every loop).
func TestProtectedCoreExecutorRefusal(t *testing.T) {
	called := false
	exec := NewToolExecutor(
		NewToolRegistry([]Tool{mockWrite{name: "write_file", called: &called}}),
		&ExecutorOptions{Bounds: &RouteBounds{}},
	)
	res := exec.Execute(ToolCall{
		Name:     "write_file",
		Args:     map[string]any{"path": ".thought/identity/covenant.md"},
		Authored: true, // even an authored write cannot touch the core
	})
	if !res.IsError || res.ErrorCode != ErrBlocked {
		t.Fatalf("expected an ErrBlocked refusal, got IsError=%v code=%q", res.IsError, res.ErrorCode)
	}
	if called {
		t.Fatal("the protected-core write executed — the anti-wireheading refusal did NOT fire")
	}
}
