package cognition

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// sampleCatalog is a synthetic inventory exercising every render branch: families/tiers as groups,
// minted (accent + "*"), status tones, tool-scope/trigger tags, and a long detail that must WRAP.
func sampleCatalog() RegistryCatalog {
	return RegistryCatalog{Sections: []RegSection{
		{
			ID: "operators", Title: "Operators", Count: 3,
			Note: "domain-general cognitive transforms",
			Groups: []RegGroup{
				{Label: "transformative", Entries: []RegEntry{
					{Name: "decompose", Detail: "break the problem into independent parts"},
					{Name: "measure", Detail: "quantify the thing against a yardstick", Tags: []string{"[run_tests]"}},
				}},
				{Label: "minted this run", Entries: []RegEntry{
					{Name: "fast-mul", Detail: "a synthesised operator with a deliberately very long intent that should wrap cleanly under the hanging indent instead of overflowing the detail column", Minted: true},
				}},
			},
		},
		{
			ID: "memory", Title: "Memory", Count: 2,
			Note: "PROPOSED registry — implicit today",
			Groups: []RegGroup{
				{Label: "stores", Entries: []RegEntry{
					{Name: "episodic", Status: "present", Detail: "this session's graph + transcript", Tags: []string{"4 turns"}},
					{Name: "decay", Status: "idea", Detail: "age out low-value entries"},
				}},
			},
		},
	}}
}

func TestRenderRegistry_LayoutInvariants(t *testing.T) {
	cat := sampleCatalog()
	for _, w := range []int{60, 80, 120, 200} {
		for sel := range cat.Sections {
			body := RenderRegistry(cat, sel, w)
			if strings.TrimSpace(body) == "" {
				t.Fatalf("w=%d sel=%d: empty body", w, sel)
			}
			// the no-overflow rule: no rendered line may exceed the width (else lipgloss reflows it and
			// the two-column layout shears). This is THE layout invariant.
			for i, ln := range strings.Split(body, "\n") {
				if got := lipgloss.Width(ln); got > w {
					t.Fatalf("w=%d sel=%d line %d width %d > %d: %q", w, sel, i, got, w, ln)
				}
			}
			// the divider must be present (the two-pane composition) and the selected section's title shown.
			if !strings.Contains(body, "│") {
				t.Errorf("w=%d sel=%d: no divider", w, sel)
			}
			if !strings.Contains(body, strings.ToUpper(cat.Sections[sel].Title)) {
				t.Errorf("w=%d sel=%d: selected title %q absent", w, sel, cat.Sections[sel].Title)
			}
		}
	}
}

func TestRenderRegistry_SelectionAndContent(t *testing.T) {
	cat := sampleCatalog()
	// sel=0 shows operators (decompose, the run_tests scope tag, the minted star); NOT the memory-only entries.
	body := RenderRegistry(cat, 0, 100)
	for _, want := range []string{"REGISTRIES", "Operators", "Memory", "decompose", "[run_tests]", "fast-mul*"} {
		if !strings.Contains(body, want) {
			t.Errorf("sel=0: missing %q", want)
		}
	}
	if strings.Contains(body, "age out low-value") {
		t.Errorf("sel=0: leaked memory-section detail into the operators detail pane")
	}
	// sel=1 shows the memory section's entries instead.
	body = RenderRegistry(cat, 1, 100)
	if !strings.Contains(body, "episodic") || !strings.Contains(body, "age out low-value") {
		t.Errorf("sel=1: memory entries not shown")
	}
}

func TestRenderRegistry_OutOfRangeAndEmpty(t *testing.T) {
	cat := sampleCatalog()
	// out-of-range selection clamps rather than panics.
	if got := RenderRegistry(cat, 99, 80); strings.TrimSpace(got) == "" {
		t.Error("out-of-range sel produced empty body")
	}
	if got := RenderRegistry(cat, -5, 80); strings.TrimSpace(got) == "" {
		t.Error("negative sel produced empty body")
	}
	// an empty catalog renders the placeholder, not a panic.
	if got := RenderRegistry(RegistryCatalog{}, 80, 80); !strings.Contains(got, "no registries") {
		t.Errorf("empty catalog: want placeholder, got %q", got)
	}
}
