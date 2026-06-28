package tui

// config_data.go — the bridge-side assembler for the CONFIG browser tab (cognition.ConfigView). It is
// the config analog of BuildRegistryCatalog / BuildSnapshot: the ONLY place that touches the engine's
// shared HarnessConfig + the canonical knob table, translating them into the plain-data view the pure
// panel (cognition.RenderConfig) renders. Reads are off the LIVE shared config; a live flip goes back
// through ApplyToggle, which mutates that same shared pointer (no engine rebuild — the §4.3 hard rule).
//
// The non-default mark is computed against config.AllOn() (the defaults the spec pins): a knob differs
// from default ⇒ it surfaces in the panel's non-default count + the warn tone. Determinism is irrelevant
// here (this is a read-side UI projection), but it stays a pure copy — the panel never mutates config.

import (
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// cfgSectionSpec maps a section id (the rail bucket) to its title + the dotted-path PREFIX that selects
// its knobs from the one knob table. Order is the spec's left-rail order (§4.6). One table edit adds a
// section; the knob table (config.Knobs) is the single source for the rows.
var cfgSectionSpecs = []struct {
	id, title, prefix string
}{
	{"subconscious", "Subconscious", "subconscious."},
	{"conscious", "Conscious", "conscious."},
	{"controller", "Controller", "controller."},
	{"seam", "Seam", "seam."},
	{"action", "Action", "action."},
	{"value", "Value", "value."},
	{"convert", "Convert", "convert."},
	{"regulator", "Regulator", "regulator."},
	{"memory", "Memory", "memory."},
	{"knowledge", "Knowledge", "knowledge."},
	{"representation", "Representation", "representation."},
	{"persistence", "Persistence", "persistence."},
	{"ledger", "Ledger", "ledger."},
	{"sense", "Sense", "sense."},
	{"conformance", "Conformance", "conformance."},
	{"dev", "Dev (auto-dev)", "dev."},
	{"slam", "SLAM self-state", "slam."},
	{"tui", "Analysis", "tui."},
	{"selfbench", "Self-bench", "selfbench."},
	{"introspect", "Introspect", "introspect."},
	{"flywheel", "Flywheel (offline-RL)", "flywheel."},
}

// regimeKnobs are the durability-affecting knobs the spec wants flagged in the Config tab (§4.5
// "regime-affecting"): the regulator enforcement + the parallel fan-out cap that Validate clamps to
// W_max. A flip here can break the stability suite, so the panel marks them.
var regimeKnobs = map[string]bool{
	"regulator.enforce":          true,
	"subconscious.max_par_width": true,
}

// ConfigView assembles the whole config picture from the live engine's shared HarnessConfig. Cheap (it
// ranges the knob table once per section), so the app rebuilds it per frame while the Config tab is
// open — a live flip then shows up immediately. A nil engine yields a single placeholder section.
func (b *EngineBridge) ConfigView() cognition.ConfigView {
	e := b.eng.Load()
	if e == nil {
		return cognition.ConfigView{Sections: []cognition.CfgSection{{
			ID: "none", Title: "Config", Rows: nil,
		}}}
	}
	cfg := e.Features()
	def := config.AllOn()
	knobs := config.Knobs()

	var sections []cognition.CfgSection
	nonDefault := 0
	offCount := 0
	for _, spec := range cfgSectionSpecs {
		var rows []cognition.CfgRow
		for _, k := range knobs {
			if !hasPrefix(k.Path, spec.prefix) {
				continue
			}
			row := cognition.CfgRow{
				Path:   k.Path,
				Label:  k.Label,
				Regime: regimeKnobs[k.Path],
			}
			switch k.Kind {
			case config.KnobBool:
				on, _ := k.GetBool(cfg)
				d, _ := k.GetBool(&def)
				row.Kind = cognition.CfgBool
				row.On = on
				row.Default = on == d
				// Count a toggle as OFF only when it is off AND that is a DEVIATION from its baseline.
				// An opt-in instrument (baseline OFF, e.g. seam.legible_generation) sitting at its default
				// is not "an off toggle" in the all-on sense — matching config.OffPaths(), so a fresh
				// engine still reports 0 off (byte-identical to before the instrument existed).
				if !on && !row.Default {
					offCount++
				}
			case config.KnobInt:
				v, _ := k.GetInt(cfg)
				d, _ := k.GetInt(&def)
				row.Kind = cognition.CfgInt
				row.IntVal = v
				row.Default = v == d
			case config.KnobString:
				v, _ := k.GetString(cfg)
				d, _ := k.GetString(&def)
				row.Kind = cognition.CfgString
				row.StrVal = v
				row.Default = v == d
			case config.KnobFloat:
				v, _ := k.GetFloat(cfg)
				d, _ := k.GetFloat(&def)
				row.Kind = cognition.CfgFloat
				row.FloatVal = v
				row.Default = v == d
			}
			if !row.Default {
				nonDefault++
			}
			rows = append(rows, row)
		}
		sections = append(sections, cognition.CfgSection{ID: spec.id, Title: spec.title, Rows: rows})
	}
	return cognition.ConfigView{
		Sections:   sections,
		OffCount:   offCount,
		NonDefault: nonDefault,
		Warnings:   cfg.Validate(),
		Path:       e.FeaturesPath(),
	}
}

// ApplyConfigToggle flips a BOOL toggle by its dotted path on the engine's SHARED config (no rebuild —
// the next tick's gates observe it through the shared pointer). Returns ok=false on an unknown / non-bool
// path or no engine. The app calls this from the Config tab's Space/Enter; live flips never reconstruct
// the engine, so learned state survives (the §4.3 / §4.4 contract).
func (b *EngineBridge) ApplyConfigToggle(path string, on bool) bool {
	e := b.eng.Load()
	if e == nil {
		return false
	}
	return e.ApplyFeatureToggle(path, on)
}

// BumpConfigTunable nudges an int tunable by delta on the shared config (the Config tab's ←/→), clamping
// via the engine's Validate (so max_par_width can never exceed W_max). Returns ok=false for a non-int /
// unknown path or no engine.
func (b *EngineBridge) BumpConfigTunable(path string, delta int) bool {
	e := b.eng.Load()
	if e == nil {
		return false
	}
	return e.BumpFeatureTunable(path, delta)
}

// BumpConfigTunableFloat nudges a float tunable by delta on the shared config (the Config tab's ←/→ on a
// CfgFloat row), clamped to [0,1] by the engine. Returns ok=false for a non-float / unknown path or no
// engine.
func (b *EngineBridge) BumpConfigTunableFloat(path string, delta float64) bool {
	e := b.eng.Load()
	if e == nil {
		return false
	}
	return e.BumpFeatureTunableFloat(path, delta)
}

// hasPrefix is a tiny stdlib-free prefix check kept local so this file imports only the two packages it
// projects between (config + the view model), matching registry_data.go's minimal-import discipline.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
