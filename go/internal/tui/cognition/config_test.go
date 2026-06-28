package cognition

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// sampleConfigView is a synthetic config picture exercising every render branch: a plain section with a
// flipped-OFF toggle + a regime tunable, and the representation section with its 3-block grid (moves /
// sources / paths), one source flipped OFF (the non-default warn tone).
func sampleConfigView() ConfigView {
	return ConfigView{
		Sections: []CfgSection{
			{ID: "subconscious", Title: "Subconscious", Rows: []CfgRow{
				{Path: "subconscious.dispatch", Label: "Dispatch", Kind: CfgBool, On: true, Default: true},
				{Path: "subconscious.sourcing", Label: "Sourcing ladder", Kind: CfgBool, On: false, Default: false},
				{Path: "subconscious.max_par_width", Label: "Max parallel width", Kind: CfgInt, IntVal: 8, Default: true, Regime: true},
			}},
			{ID: "representation", Title: "Representation", Rows: []CfgRow{
				{Path: "representation.moves.ground", Label: "Move: ground", Kind: CfgBool, On: true, Default: true},
				{Path: "representation.moves.lift", Label: "Move: lift", Kind: CfgBool, On: true, Default: true},
				{Path: "representation.sources.reality", Label: "Source: reality", Kind: CfgBool, On: false, Default: false},
				{Path: "representation.paths.analogy", Label: "Path: analogy", Kind: CfgBool, On: true, Default: true},
			}},
			{ID: "persistence", Title: "Persistence", Rows: []CfgRow{
				{Path: "persistence.enabled", Label: "Enabled", Kind: CfgBool, On: true, Default: true},
				{Path: "persistence.backend", Label: "Backend", Kind: CfgString, StrVal: "jsonl", Default: true},
			}},
		},
		OffCount:   2,
		NonDefault: 2,
		Path:       "/tmp/cfg.json",
	}
}

func TestRenderConfig_LayoutInvariants(t *testing.T) {
	cv := sampleConfigView()
	for _, w := range []int{50, 64, 80, 120, 200} {
		for sel := range cv.Sections {
			for _, selRow := range []int{0, 1, 99, -1} {
				body := RenderConfig(cv, sel, selRow, w)
				if strings.TrimSpace(body) == "" {
					t.Fatalf("w=%d sel=%d selRow=%d: empty body", w, sel, selRow)
				}
				// THE layout invariant: no rendered line may exceed the width (else lipgloss reflows it and
				// the two-pane layout shears).
				for i, ln := range strings.Split(body, "\n") {
					if got := lipgloss.Width(ln); got > w {
						t.Fatalf("w=%d sel=%d line %d width %d > %d: %q", w, sel, i, got, w, ln)
					}
				}
				if !strings.Contains(body, "│") {
					t.Errorf("w=%d sel=%d: no divider", w, sel)
				}
			}
		}
	}
}

func TestRenderConfig_ShowsToggleState(t *testing.T) {
	cv := sampleConfigView()
	// sel=0 (Subconscious): the ON dispatch shows [on], the OFF sourcing shows [off], the tunable its value.
	body := RenderConfig(cv, 0, 0, 100)
	for _, want := range []string{"CONFIG", "Subconscious", "Dispatch", "[on]", "Sourcing", "[off]", "Max parallel width", "8", "regime"} {
		if !strings.Contains(body, want) {
			t.Errorf("sel=0: missing %q", want)
		}
	}
	// it must NOT leak the representation section's rows into the subconscious detail.
	if strings.Contains(body, "Move: ground") {
		t.Errorf("sel=0: leaked representation rows into the subconscious detail")
	}
}

func TestRenderConfig_RepresentationGrid(t *testing.T) {
	cv := sampleConfigView()
	// sel=1 is the Representation section: the 3-block grid must label moves / sources / paths and show
	// each block's toggles.
	body := RenderConfig(cv, 1, 0, 100)
	for _, want := range []string{"moves", "sources", "paths", "Move: ground", "Source: reality", "Path: analogy"} {
		if !strings.Contains(body, want) {
			t.Errorf("representation: missing %q", want)
		}
	}
}

func TestRenderConfig_SelectionAndClamp(t *testing.T) {
	cv := sampleConfigView()
	// out-of-range section/row indices clamp rather than panic.
	if got := RenderConfig(cv, 99, 99, 80); strings.TrimSpace(got) == "" {
		t.Error("out-of-range sel/selRow produced empty body")
	}
	if got := RenderConfig(cv, -5, -5, 80); strings.TrimSpace(got) == "" {
		t.Error("negative sel/selRow produced empty body")
	}
	// an empty config renders the placeholder, not a panic.
	if got := RenderConfig(ConfigView{}, 0, 0, 80); !strings.Contains(got, "no config") {
		t.Errorf("empty config: want placeholder, got %q", got)
	}
}
