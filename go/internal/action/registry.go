// registry.go — a name -> Tool registry (ported from action/registry.py, itself a port of lathe's
// registry/map.go + tools registration).
//
// Deterministic: Names() and Definitions() return sorted order so a schema handed to a model (or a
// test) is stable. This is load-bearing for the golden oracle — the Go ordering must match the
// Python sorted() ordering exactly.
package action

import "sort"

// ToolRegistry maps a tool name to its Tool. The zero value is NOT usable; build one with
// NewToolRegistry.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry builds a registry from an initial set of tools (Python's __init__(tools)).
func NewToolRegistry(tools []Tool) *ToolRegistry {
	r := &ToolRegistry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

// Register installs a tool under its own name (overwriting any prior tool of that name, matching
// Python's dict assignment).
func (r *ToolRegistry) Register(tool Tool) { r.tools[tool.Name()] = tool }

// Get returns the tool registered under name, or (nil, false) if absent (Python dict.get -> None).
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the registered tool names in sorted order (Python sorted(self._tools)).
func (r *ToolRegistry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// List returns the tools in sorted-name order (Python list()).
func (r *ToolRegistry) List() []Tool {
	names := r.Names()
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n])
	}
	return out
}

// Definitions returns the tool schema list handed to a model for tool-calling: one
// {name, description, parameters} map per tool, in sorted-name order (Python definitions()).
func (r *ToolRegistry) Definitions() []map[string]any {
	tools := r.List()
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"parameters":  t.Parameters(),
		})
	}
	return defs
}
