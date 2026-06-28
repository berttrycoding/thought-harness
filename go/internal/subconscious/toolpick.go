// toolpick.go — the CATEGORY-SOURCED tool-pick resolver (cognition redesign §3.3a pick / §3.6 / §3.7,
// gap-5 load-bearing half + the gap-9 prerequisite).
//
// THE REWIRE. Today a staffed SubAgent inherits the operator's flat concrete tool name-list
// (OperatorSpec.ToolScope, workflow.go staffing). The redesign says the WORKER owns its tools, sourced by
// CATEGORY at staffing from the run's Capability/Scope — NOT from the operator's name-list (§3.6: "owns its
// own category-scoped tools/skills instead of inheriting the operator's name-list"). This resolver is that
// source: given the operator's coarse OPERATION-CATEGORY FOOTPRINT (OperatorSpec.ScopeCategories — the
// move-hint set that survives the gap-9 ToolScope deletion) and the WORKER-FACULTY tool footprint (§3.7 — a
// faculty's tool-categories), it resolves the concrete tool-NAME set from the LIVE action registry, matching
// each tool on its OWN Tool.Category() tag (gap 6). The operator's flat ToolScope is no longer the SOURCE.
//
// PARITY (the hard invariant). The resolved set MUST be UNCHANGED for the seed ops so flipping the flag
// preserves behaviour. The seed catalog's only tool-bearing operators are:
//
//	measure / validate          ToolScope {run_tests}        ScopeCategories {execute}
//	expose-affordances          ToolScope {search,read_file} ScopeCategories {inspect}
//	every other (reason-only)    ToolScope {}                 ScopeCategories {}        → no tools
//
// The resolver reproduces these EXACTLY because the worker-faculty footprint, intersected with the live
// registry per operation category, is the curated pick:
//
//	inspect  worker faculties {read→read_file, search→search} ∩ registry inspect {read_file,search} = {read_file,search}
//	execute  worker faculty   {run→run_tests}                 ∩ registry execute {run_shell,run_tests} = {run_tests}
//
// — so expose-affordances resolves {read_file,search}, measure/validate resolve {run_tests}, and a
// reason-only op (empty footprint) resolves {} — byte-identical to the flat ToolScope it replaces. The
// faculty footprint (not "every tool in the category") is what keeps run_shell OUT of an execute pick: no
// worker faculty carries run_shell, so it is never picked, matching the curated seed scope.
//
// Determinism: the resolved set is sorted by name; the registry/roster scans are over deterministic,
// sorted collections; no clock, no RNG. Each pick goes through Scope.Pick so the §3.3a ceiling REFUSES an
// out-of-band pick (a worker never widens its authority) and the lazy facet is recorded.
package subconscious

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// toolFootprinter is the worker-faculty capability-footprint accessor (§3.7): a tool-backed primitive
// reports the concrete tool NAMES it may dispatch (its least-privilege scope). The resolver builds the
// per-operation-category faculty footprint from the roster's footprinters, so the source of truth is the
// LIVE faculty roster, not a duplicated constant. A model-driven / minted primitive carries no tools and
// does not implement it (it contributes nothing to the footprint).
type toolFootprinter interface{ ToolFootprint() []string }

// ToolPicker resolves a staffed worker's concrete tool-name set from the run's category footprint instead
// of the operator's flat ToolScope (the gap-5 load-bearing rewire). It is built once per episode (the live
// registry + faculty roster do not change mid-episode) and called per operator at staffing. nil ⇒ the
// workflow falls back to the operator's flat ToolScope (the byte-identical default-OFF posture).
type ToolPicker struct {
	// byCategory maps an operation category ("inspect"|"mutate"|"execute") to the worker-faculty tool NAMES
	// of that category that are ALSO present in the live registry — the curated, registry-confirmed pick set
	// per category. Built from the LIVE faculty roster's footprints classified by the tools' OWN Category().
	byCategory map[string][]string
}

// NewToolPicker builds the resolver from the LIVE faculty roster + the LIVE action registry (gap 5/6): it
// reads each tool-backed worker faculty's tool footprint (§3.7), classifies each named tool by its OWN
// Tool.Category() operation tag (gap 6 — the tool owns its taxonomy), and keeps only tools the registry
// actually holds. The result is the per-category, registry-confirmed worker-faculty pick set the staffing
// resolver draws from. A nil registry ⇒ a nil picker (the caller then keeps the flat ToolScope — the
// offline/no-workspace path, byte-identical).
//
// WEB-SEARCH (subconscious.web_search): when webSearch is true AND the registry holds web_search, the tool
// is UNIONED into its OWN category (inspect — the same gap-6 Tool.Category() classification read_file/search
// get), so a staffed expose-affordances worker (which reaches the inspect category) picks it. This is the
// capability-ON half of the web_search staffing fix: no worker FACULTY footprints web_search (it is a
// flag-gated, model-callable tool, not a standing faculty), so without this union the category-sourced
// picker would DROP the granted web_search (the measured under-firing on the capability-ON bench path).
// webSearch=false ⇒ the union is skipped ⇒ byte-identical to the pre-flag picker.
func NewToolPicker(roster []PrimitiveSubAgent, registry *action.ToolRegistry, webSearch bool) *ToolPicker {
	if registry == nil {
		return nil
	}
	byCat := map[string]map[string]bool{}
	add := func(name string) {
		tool, found := registry.Get(name)
		if !found {
			return // a tool the live registry does not hold ⇒ skip (least privilege)
		}
		cat := tool.Category().Op.String() // the tool's OWN operation tag (gap 6, not a name-switch)
		if byCat[cat] == nil {
			byCat[cat] = map[string]bool{}
		}
		byCat[cat][name] = true
	}
	for _, p := range roster {
		fp, ok := p.(toolFootprinter)
		if !ok {
			continue // model-driven / minted faculties carry no tools (§3.7 footprint is empty)
		}
		for _, name := range fp.ToolFootprint() {
			add(name)
		}
	}
	// WEB-SEARCH union (flag-gated): web_search is a model-callable tool, not a standing faculty, so it is
	// in no roster footprint — union it into its own category here so the category-sourced staffing picker
	// can reach it for an inspect-category operator (expose-affordances). Idempotent + registry-gated.
	if webSearch {
		add("web_search")
	}
	// FETCH-URL union (T1.4): fetch_url is likewise a model-callable tool (no faculty footprints it), so
	// union it into its own (inspect) category so a staffed expose-affordances worker can reach it. It is
	// REGISTRY-GATED (add() is a no-op unless the registry actually holds fetch_url), and the engine only
	// registers fetch_url when subconscious.fetch_url is ON — so with the flag OFF the tool is absent from
	// the registry and this union is a no-op ⇒ byte-identical to the pre-flag picker (no separate bool gate
	// needed). The sibling of the web_search union above.
	add("fetch_url")
	out := &ToolPicker{byCategory: map[string][]string{}}
	for cat, set := range byCat {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out.byCategory[cat] = names
	}
	return out
}

// Resolve returns the concrete tool-NAME set a worker for `spec` is staffed with — sourced from the
// operator's coarse category FOOTPRINT (spec.ScopeCategories), NOT its flat ToolScope (the gap-5 rewire).
// For each operation category the operator's move reaches, it admits the registry-confirmed worker-faculty
// tools of that category; each is run through the run's Scope.Pick so the §3.3a ceiling can REFUSE an
// out-of-band tool (a worker never widens its authority) and the lazy pick facet is recorded. The result
// is sorted, deduplicated, and DETERMINISTIC.
//
// A nil Scope ⇒ no ceiling to filter against, so every footprint tool is admitted (the picker's own
// per-category sets already encode the least-privilege faculty footprint). An operator with an EMPTY
// footprint (reason-only) resolves to an empty set — no tools — exactly as its nil ToolScope did.
func (p *ToolPicker) Resolve(spec cognition.OperatorSpec, scope *Scope) []string {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, cat := range spec.ScopeCategories() {
		for _, name := range p.byCategory[cat] {
			if seen[name] {
				continue
			}
			// Run the pick through the run's §3.3a Scope: the ceiling REFUSES a tool whose category is out of
			// band (a worker may never widen its authority — only an explicit gate can). The facet key is the
			// tool name so a re-pick is stable. nil scope ⇒ admit (the per-category footprint already bounds it).
			if scope != nil {
				if _, ok := scope.Pick("tool:"+name, name, cat); !ok {
					continue // outside the run's authority ceiling — refused, not granted
				}
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
