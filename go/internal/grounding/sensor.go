// sensor.go is continuous perception (N.1a-cont / AR-6): unsolicited SENSOR SOURCES feeding the
// grounding loop. The PerceptionPort mechanism already exists (always-on, arousal-gain + salience); what
// was missing is independent SOURCES that push observations the system did NOT ACT to get — a file
// watcher, a test/build watcher, a log/monitor stream — and the wire from those percepts into the
// grounding loop. The payoff is STANDING re-grounding: in awake / long-horizon mode a claim is refuted
// the moment a percept contradicts it, instead of staying ungrounded until the next ACT, so a
// hallucination arising between acts can't propagate.
package grounding

// Percept is an UNSOLICITED observation from a continuous sensor — the system did not act to get it. It
// bears on a claim (Ok = the claim held when the sensor looked).
type Percept struct {
	Claim  string
	Ok     bool
	Source string
	// Fabricated marks a percept that is NOT a real reading (a synthetic / placeholder / offline
	// stand-in sensor). The grounding spine never grounds a fabricated percept — IngestObservation
	// rejects it — so a future synthetic sensor cannot launder manufactured "reality" into the ledger.
	// A real watcher leaves this false (the common case).
	Fabricated bool
}

// Sensor is a continuous, independent source of percepts (a file / test / build / log watcher). Poll
// returns the percepts available at a tick. Deterministic in ticks (never the wall clock).
type Sensor interface {
	Poll(tick int) []Percept
}

// IngestSensor feeds a sensor's percepts at a tick into the grounding loop with NO ACT — the standing
// re-grounding step (run every tick in awake mode). Each percept is a REAL observation (not the offline
// stand-in), so it grounds via IngestObservation at the firsthand-observation tier. Returns how many
// percepts grounded.
func (m *ExperimentMemory) IngestSensor(s Sensor, tick int) int {
	n := 0
	for _, p := range s.Poll(tick) {
		// Pass the percept's own Fabricated flag through (not a hardcoded false): a sensor that knows
		// its reading is synthetic marks it, and IngestObservation refuses to ground a fabricated claim.
		if m.IngestObservation(p.Claim, p.Ok, p.Fabricated, tick) {
			n++
		}
	}
	return n
}

// ScriptedSensor is a deterministic test sensor: a fixed schedule of tick -> percepts. It stands in for
// a real watcher so the continuous-grounding behaviour is reproducible.
type ScriptedSensor struct {
	Schedule map[int][]Percept
}

// Poll returns the scripted percepts for this tick.
func (s ScriptedSensor) Poll(tick int) []Percept { return s.Schedule[tick] }
