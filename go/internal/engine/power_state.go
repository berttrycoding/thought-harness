package engine

import "github.com/berttrycoding/thought-harness/internal/types"

// power_state.go composes the two coexisting state machines — arousal (AWAKE/DROWSY/ASLEEP) and the
// lifecycle (IDLE/ACTIVE/AWAITING_*/SUSPENDED/DONE) — into ONE legible "power state" for observability
// and the cognition-model panel (proposal 2026-06-20 §2.3: "compose arousal + lifecycle into one power
// state"). PURE READ-ONLY projection: it derives a label from the live machines, changes NO behavior, and
// emits NO event, so goldens are unaffected.

// PowerState is the single-axis power label projected from (arousal, lifecycle) — the hardware-style
// power-cycle view: booting -> awake/drowsy/asleep -> waiting -> off.
type PowerState string

const (
	PowerBooting PowerState = "booting" // constructed, no tick yet
	PowerAwake   PowerState = "awake"   // arousal AWAKE, thinking
	PowerDrowsy  PowerState = "drowsy"  // arousal DROWSY (throttled loop)
	PowerAsleep  PowerState = "asleep"  // arousal ASLEEP (consolidating / dreaming)
	PowerWaiting PowerState = "waiting" // blocked on reality or the user
	PowerOff     PowerState = "off"     // episode DONE / quiescent / idle
)

// PowerState projects the live arousal + lifecycle into one label. Arousal wins for the low-power states
// (drowsy/asleep gate the loop regardless of lifecycle); otherwise the lifecycle distinguishes
// waiting/off from active thinking. Pure read-only — safe to call from a renderer at any time.
func (e *Engine) PowerState() PowerState {
	if e.bus == nil || e.bus.Tick == 0 {
		return PowerBooting
	}
	switch e.arousal {
	case types.ASLEEP:
		return PowerAsleep
	case types.DROWSY:
		return PowerDrowsy
	}
	switch e.lifecycle.State {
	case types.AWAITING_REALITY, types.AWAITING_USER:
		return PowerWaiting
	case types.IDLE, types.DONE:
		return PowerOff
	}
	return PowerAwake
}
