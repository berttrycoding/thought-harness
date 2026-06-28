package cognition

import "testing"

// TestOperatorScopeCategory pins the #31 Operator facet (§3.10a/§3.3a): an operator's coarsest authority
// category is the most powerful tool in its scope — a reason-only op is "inspect", a runner is "execute",
// any writer is "mutate". This is the coordinate a Scope ceiling filters operators by.
func TestOperatorScopeCategory(t *testing.T) {
	cases := []struct {
		tools []string
		want  string
	}{
		{nil, "inspect"}, // reason-only
		{[]string{"read_file", "search"}, "inspect"},    // reads only
		{[]string{"run_tests"}, "execute"},              // a runner
		{[]string{"read_file", "run_shell"}, "execute"}, // read + run -> execute
		{[]string{"read_file", "write_file"}, "mutate"}, // any writer -> mutate (strongest)
		{[]string{"edit_file", "run_tests"}, "mutate"},  // writer dominates a runner
	}
	for _, c := range cases {
		got := OperatorSpec{Name: "op", ToolScope: c.tools}.ScopeCategory()
		if got != c.want {
			t.Errorf("ScopeCategory(%v) = %q, want %q", c.tools, got, c.want)
		}
	}
}
