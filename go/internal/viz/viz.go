// Package viz renders the workflow/session runtime as text — the core of the workflow visualization tab
// (P3.7): a program tree, a phase timeline, and a session spawn tree. These are pure renderers (string
// in, string out) so they are unit-testable without the TUI; the TUI tab wires them over the live event
// bus (the data they render — the synthesised Program, the dispatched Session tree — already flows as
// events). No emoji, Unicode box-drawing only (matches the harness convention).
package viz

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/session"
)

// RenderPhaseTimeline renders a synthesised Program as a numbered phase timeline: each scheduled
// phase-group on its own line, marking parallel fan-out and unrolled loop iterations. This is the
// "what runs, in what order" view of a workflow.
func RenderPhaseTimeline(prog cognition.Program) string {
	plans := prog.Schedule()
	if len(plans) == 0 {
		return "(empty program)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "workflow: %s\n", prog.Shape())
	for i, p := range plans {
		marker := "→"
		if p.Parallel {
			marker = "⇉" // parallel fan-out
		}
		ops := make([]string, len(p.Steps))
		for j, s := range p.Steps {
			role := ""
			if s.IsScript() {
				role = " [script]"
			}
			ops[j] = s.Operator + role
		}
		line := fmt.Sprintf("  %2d %s %s", i+1, marker, strings.Join(ops, " ‖ "))
		if p.Loop != nil {
			line += fmt.Sprintf("   (loop %s · iter %d)", *p.Loop, p.Iteration)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderSessionTree renders a Session's spawn tree as an indented tree, each node showing its goal,
// horizon, and (if metered) its token budget — the "who dispatched whom" view, bounded by design.
func RenderSessionTree(s *session.Session) string {
	var b strings.Builder
	b.WriteString(sessionLine(s, "") + "\n") // the root has no connector
	renderChildren(&b, s, "")
	return strings.TrimRight(b.String(), "\n")
}

// renderChildren writes each child's line (prefix + connector) then recurses with the descendant prefix.
func renderChildren(b *strings.Builder, s *session.Session, prefix string) {
	for i, c := range s.Children {
		connector, descend := "├─ ", prefix+"│  "
		if i == len(s.Children)-1 {
			connector, descend = "└─ ", prefix+"   "
		}
		b.WriteString(sessionLine(c, prefix+connector) + "\n")
		renderChildren(b, c, descend)
	}
}

// sessionLine formats one session node: <linePrefix><goal>  [horizon · budget].
func sessionLine(s *session.Session, linePrefix string) string {
	horizon := [...]string{"single-shot", "bounded", "long-horizon"}[s.Spec.Horizon]
	line := fmt.Sprintf("%s%s  [%s", linePrefix, truncate(s.Goal, 48), horizon)
	if s.Budget != nil {
		line += fmt.Sprintf(" · %d/%d tok", s.Budget.Spent, s.Budget.TokenCap)
	}
	return line + "]"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
