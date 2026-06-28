package config

import "github.com/berttrycoding/thought-harness/internal/types"

// ReprMatrix is the representation-space gating section (§4.2): toggles per MOVE on the abstraction
// ladder, per SOURCE the fuel comes from, and per PATH the named traversal. It is config-gating over
// the existing seams — there is no Move/Source/Path runtime enum yet beyond types.Source, so the
// gating binds via reprTag (a pure, deterministic classifier). Defaults all-ON ⇒ no behavioural change.
type ReprMatrix struct {
	Moves   MoveToggles   `json:"moves"`   // ground / lift / reframe / transcode
	Sources SourceToggles `json:"sources"` // present / knowledge / memory / reality / generated
	Paths   PathToggles   `json:"paths"`   // analogy / induction / deduction
}

// MoveToggles gate the 4 directed moves on the abstraction ladder (§1.2). A disabled move suppresses
// that move's operators from the synthesiser's candidate set (M2+ wires the Move tag).
type MoveToggles struct {
	Ground    bool `json:"ground"`    // abstract -> concrete (instantiate)
	Lift      bool `json:"lift"`      // concrete -> abstract (generalize)
	Reframe   bool `json:"reframe"`   // abstract -> abstract (analogize)
	Transcode bool `json:"transcode"` // concrete -> concrete (translate)
}

// SourceToggles gate the 5 fuel wells the sourcing ladder walks (§1.3). A disabled source is skipped
// in the walk (e.g. Reality=off makes rung 4 a no-op; Generated=off is the strict-grounding posture).
type SourceToggles struct {
	Present   bool `json:"present"`   // already in the conscious stream (no-op fetch)
	Knowledge bool `json:"knowledge"` // the durable knowledge registry
	Memory    bool `json:"memory"`    // first-person grounded experience
	Reality   bool `json:"reality"`   // a tool crossed the watched seam
	Generated bool `json:"generated"` // the model invents it (the LOW-trust floor)
}

// PathToggles gate which of the 3 seed path-skills the synthesiser/Controller may choose (§1.4).
type PathToggles struct {
	Analogy   bool `json:"analogy"`   // round-trip across the top of the ladder
	Induction bool `json:"induction"` // upward, ends in a store
	Deduction bool `json:"deduction"` // downward, ends in reality
}

// AllOnRepr returns the all-enabled representation matrix (defaults all-ON ⇒ behaviour identical to
// today: every move, every source, every path permitted).
func AllOnRepr() ReprMatrix {
	return ReprMatrix{
		Moves:   MoveToggles{Ground: true, Lift: true, Reframe: true, Transcode: true},
		Sources: SourceToggles{Present: true, Knowledge: true, Memory: true, Reality: true, Generated: true},
		Paths:   PathToggles{Analogy: true, Induction: true, Deduction: true},
	}
}

// Move names the 4 directed moves on the abstraction ladder + the non-move assess lane. M2 adds a
// Move field to OperatorSpec; until then reprTag classifies by the data available (operator name +
// provenance source), so the matrix gating is one helper call per seam from M1.
type Move string

const (
	MoveGround    Move = "ground"
	MoveLift      Move = "lift"
	MoveReframe   Move = "reframe"
	MoveTranscode Move = "transcode"
	MoveAssess    Move = "assess" // rank/validate/eliminate/measure — a non-move (the Critic's lane)
	MoveUnknown   Move = ""
)

// MoveEnabled reports whether a given move is permitted by the matrix. The assess lane (a non-move)
// and an unknown move are ALWAYS permitted — the matrix only ever gates the 4 directed moves, so a
// classifier that cannot place a candidate never silently suppresses it. Nil-safe (nil ⇒ enabled).
func (m *ReprMatrix) MoveEnabled(mv Move) bool {
	if m == nil {
		return true
	}
	switch mv {
	case MoveGround:
		return m.Moves.Ground
	case MoveLift:
		return m.Moves.Lift
	case MoveReframe:
		return m.Moves.Reframe
	case MoveTranscode:
		return m.Moves.Transcode
	default:
		return true // assess / unknown -> never gated
	}
}

// SourceEnabled reports whether a given fuel source (by its types.Source provenance) is permitted by
// the matrix. The mapping is the §1.3 ladder: GENERATED ⇒ the generated rung, OBSERVATION/PERCEPT ⇒
// reality, INJECTED/METACOG ⇒ memory-or-present (grounded recombination), USER_INPUT ⇒ present.
// A source the matrix does not model is always permitted. Nil-safe (nil ⇒ enabled).
func (m *ReprMatrix) SourceEnabled(s types.Source) bool {
	if m == nil {
		return true
	}
	switch s {
	case types.GENERATED:
		return m.Sources.Generated
	case types.OBSERVATION, types.PERCEPT:
		return m.Sources.Reality
	case types.INJECTED:
		return m.Sources.Memory || m.Sources.Knowledge || m.Sources.Present
	case types.METACOG:
		return m.Sources.Present
	default:
		return true // USER_INPUT et al. -> never gated (the user is not a fuel well)
	}
}

// reprTag classifies a candidate-shaped record into its (Move, Source) on the representation space,
// deterministically and table-driven. It is the one helper a seam calls to decide whether the matrix
// gates the candidate. Move is derived from the operator NAME (the M2 Move field overrides this once
// it lands); Source is the types.Source provenance directly. An unrecognised operator ⇒ MoveUnknown
// (never gated), so the classifier is conservative: it only ever suppresses what it can confidently
// place.
func reprTag(operator string, source types.Source) (Move, types.Source) {
	return moveOfOperator(operator), source
}

// ReprTag is the exported classifier the seams call (Move + Source for a candidate). Pure +
// deterministic. The Move table mirrors the §2.3 operator-by-move catalog.
func ReprTag(operator string, source types.Source) (Move, types.Source) {
	return reprTag(operator, source)
}

// moveOfOperator maps a seed operator name to its directed move (the §1.2 / §2.3 table). Unknown
// names ⇒ MoveUnknown (never gated). This is the M1 stand-in for the M2 OperatorSpec.Move field; once
// that lands the seam reads the field directly and this table backs minted/unknown ops only.
func moveOfOperator(op string) Move {
	switch op {
	// GROUND (abstract -> concrete): instantiate
	case "generate", "hypothesize", "decompose", "vary", "instantiate", "specialize", "extrapolate":
		return MoveGround
	// LIFT (concrete -> abstract): generalize
	case "generalize", "abstract", "compress":
		return MoveLift
	// REFRAME (abstract -> abstract): analogize
	case "analogize", "invert", "compare", "contrast", "map":
		return MoveReframe
	// TRANSCODE (concrete -> concrete): translate
	case "synonymize", "iterate", "combine":
		return MoveTranscode
	// assess (a non-move): rank/validate/eliminate/measure
	case "rank", "validate", "eliminate", "measure", "recall", "curate":
		return MoveAssess
	default:
		return MoveUnknown
	}
}
