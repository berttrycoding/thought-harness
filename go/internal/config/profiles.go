package config

import "strconv"

// profiles.go — named cognition PROFILES. A profile is a one-pick preset of the harness knobs, so a
// user flips the whole cognition config at once (the TUI Settings "profile" picker / the `--profile`
// CLI flag) instead of toggling ~20 individual opt-in knobs by hand. A profile is built over AllOn()
// (the validated all-components-on baseline, with the experimental opt-in faculties OFF) plus a set of
// deliberate overrides; Mode is the loop regime the profile implies (the caller applies it to the
// EngineConfig.Mode, which lives outside the HarnessConfig).
//
// Adding or editing a profile is a single-table edit here. The runtime VALUE of each profile (does the
// awake forest actually think/learn better) is measured SEPARATELY — this file only declares the knob
// bundles.

// Profile is a named knob bundle.
type Profile struct {
	Name    string // stable id used by --profile and the TUI picker (e.g. "awake")
	Title   string // human label for the picker
	Desc    string // one-line description shown under the picker
	Mode    string // "reactive" | "continuous" — the loop regime this profile runs in
	Persist bool   // auto-persist learned state (memory/skills/…) so the session's memory survives
	apply   func(*HarnessConfig)
}

// Build returns a fresh HarnessConfig for the profile: AllOn() + the profile's overrides, validated.
// (Mode is NOT part of HarnessConfig — the caller applies Profile.Mode to the EngineConfig separately.)
func (p Profile) Build() *HarnessConfig {
	c := AllOn()
	if p.apply != nil {
		p.apply(&c)
	}
	c.Validate()
	return &c
}

// ApplyAwakeDefaults is the validated-durable "living mind" knob set (Track B/C rows B3/B4): a
// self-sustaining forest of standing intents, self-directed, safety-gated. It is the B4 GO-LIVE awake
// default-config — the single source of truth for "what the awake/continuous mind runs by default."
// It is consulted in exactly two places: (1) the named `awake`/`awake-learning` PROFILES (below), and
// (2) the engine's continuous-mode default (NewEngine, when no explicit Features/profile was given) —
// so a bare `--mode continuous` launch drops straight into the validated awake mind. Reactive mode
// never calls this, so the reactive default stays byte-identical to AllOn().
//
// B4 GO-LIVE (user-gated, β=0.5, 2026-06-21): durability-gated DURABLE at the shipped β=0.5 / 20-root
// portfolio — every-tick peak_n≈0.89 (600t) / 0.925 (2000t, never crosses the n=1 cliff), U≤1, μ>0,
// fan-out=1≤8. β=0.5 is the safer of the two characterized margins (β=1.0 → 0.948 at 2000t).
func ApplyAwakeDefaults(c *HarnessConfig) {
	a := &c.Conscious.Activity
	a.Forest = true            // per-branch goal rerank — the forest itself
	a.SeedIntents = true       // standing forest roots
	a.SeedIntentCount = 20     // full portfolio — keeps all 6 faculties alive (B3); engine clamps to SeedPortfolioSize
	a.DriveAgenda = true       // self-directed drive goals (conscience-gated)
	a.Soft = true              // the softmax activity policy (the measured faculty lift)
	a.BranchPropensity = 0.5   // durability dial — restores n-headroom under Soft (B4 finding, β=0.5 go-live margin)
	a.ProactiveOutreach = true // wire the wake-path transcript so the mind reaches out unprompted (THOUGHT_WAKE_TRANSCRIPT)
	a.ConscienceCeiling = true // it acts + reaches out unprompted now — gate the outward action
	c.Action.GateRouter = true // op×reach outward-action safety
	// AWAKE BUNDLE GO-LIVE (re-flip 2026-06-22, with the conversational-regression FIX in place — `5aa77a8`
	// RecognizeShape pre-gate, so awake_user_dispatch no-ops on chitchat; live re-confirmed conversation
	// answered@tick=0 + multi-hop engages; combined durability DURABLE 22/22):
	a.AwakeUserDispatch = true           // engage the subconscious on a TASK-shaped awake user line (no-ops on chitchat)
	c.Sense.SelfModel = true             // standing self-model + reply-fold — the awake mind knows what it is
	c.Subconscious.SparseDispatch = true // sparsemax competitive dispatch admission
}

// applyAwakeBase preserves the profile-side spelling; it delegates to the shared ApplyAwakeDefaults so
// the profile and the engine continuous-mode default never drift.
func applyAwakeBase(c *HarnessConfig) { ApplyAwakeDefaults(c) }

// profileList is the ordered registry (the order the picker cycles through). The first entry is the
// default.
var profileList = []Profile{
	{
		Name: "reactive", Title: "Reactive (episodic, on-demand)", Mode: "reactive",
		Desc:  "Answers a task then idles. All components on, every experimental faculty OFF — the safe default.",
		apply: nil, // AllOn() unchanged
	},
	{
		Name: "awake", Title: "Awake (living mind, validated)", Mode: "continuous", Persist: true,
		Desc:  "Always-on stream: a self-sustaining forest of standing intents, self-directed, safety-gated. Durability-validated (B3/B4).",
		apply: applyAwakeBase,
	},
	{
		Name: "awake-learning", Title: "Awake + Learning (experimental)", Mode: "continuous", Persist: true,
		Desc: "The awake mind that also learns its branching policy across episodes. Needs --state; durability re-pass pending.",
		// NOTE: the awake profiles run in the "continuous" loop mode — which the product calls AWAKE
		// (config.ModeLabel). "continuous" stays the internal enum value; the user only ever sees "awake".
		apply: func(c *HarnessConfig) {
			applyAwakeBase(c)
			a := &c.Conscious.Activity
			a.Learn = true            // REINFORCE the branching policy (β)
			a.Experiment = true       // outer keep-or-revert loop over the activity θ
			a.GoalFeedback = true     // unmeetable subgoal → revise the parent (goal-tree coherence)
			a.Retracement = true      // reconsider a closed node on late evidence
			c.Convert.EvalGate = true // quality-gate what the mind mints
		},
	},
}

// Profiles returns the profile registry in display order (a copy — callers must not mutate it).
func Profiles() []Profile { return append([]Profile(nil), profileList...) }

// ProfileNames returns the profile names in display order (for the CLI flag help + the TUI picker).
func ProfileNames() []string {
	out := make([]string, len(profileList))
	for i, p := range profileList {
		out[i] = p.Name
	}
	return out
}

// ProfileByName looks up a profile by its stable name; ok=false for an unknown name.
func ProfileByName(name string) (Profile, bool) {
	for _, p := range profileList {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// DefaultProfileName is the profile a bare launch uses (the first entry).
func DefaultProfileName() string { return profileList[0].Name }

// ModeLabel is the consolidated USER-FACING name for a loop mode. The code keeps "continuous" as the
// enum value (deep in the engine, tests, goldens), but the product vocabulary has exactly two regime
// words — "reactive" and "awake" — so the CLI + TUI name each regime the same way everywhere. Use this
// at every display site instead of the raw mode string.
func ModeLabel(mode string) string {
	if mode == "continuous" {
		return "awake"
	}
	return mode // "reactive" (and any future value) shows as-is
}

// ProfileChange is one knob a profile sets away from the AllOn() baseline — the human-readable
// "what this profile actually does", for the TUI preview panel.
type ProfileChange struct {
	Label string // the knob's human label (or "loop mode")
	Value string // the value it sets ("on" / "off" / "19" / "awake")
}

// Changes returns the profile's loop mode plus every knob it flips away from the AllOn() baseline —
// the diff a user sees when picking the profile, so "awake" is no longer opaque. The baseline/reactive
// profile changes nothing, so it returns just its loop mode.
func (p Profile) Changes() []ProfileChange {
	base := AllOn()
	got := *p.Build()
	out := []ProfileChange{{Label: "loop mode", Value: ModeLabel(p.Mode)}}
	for _, k := range Knobs() {
		if v, changed := knobDiff(k, &base, &got); changed {
			out = append(out, ProfileChange{Label: k.Label, Value: v})
		}
	}
	return out
}

// knobDiff reports whether knob k differs between base and got, and the formatted got-value if so.
func knobDiff(k Knob, base, got *HarnessConfig) (string, bool) {
	if bv, ok := k.GetBool(base); ok {
		gv, _ := k.GetBool(got)
		if bv != gv {
			if gv {
				return "on", true
			}
			return "off", true
		}
		return "", false
	}
	if bv, ok := k.GetInt(base); ok {
		gv, _ := k.GetInt(got)
		if bv != gv {
			return strconv.Itoa(gv), true
		}
		return "", false
	}
	if bv, ok := k.GetFloat(base); ok {
		gv, _ := k.GetFloat(got)
		if bv != gv {
			return strconv.FormatFloat(gv, 'g', -1, 64), true
		}
		return "", false
	}
	if bv, ok := k.GetString(base); ok {
		gv, _ := k.GetString(got)
		if bv != gv {
			return gv, true
		}
	}
	return "", false
}
