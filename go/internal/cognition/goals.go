// Package cognition is the generative substrate: first-class goals, the open operator
// catalog (with runtime minting), program trees, skills, and the synthesiser. This file is the
// first-class Goal ENTITY — the conscious layer's setpoint (docs/cognition/02-conscious.md §1).
//
// REIMPLEMENTATION (slice a.5): Goal was a passive logged record (a Goal->Skill->action match, Status
// never reaching DONE). It is now a real, load-bearing setpoint: a lifecycle state machine (§1.7), a
// tree (Parent/Children, §1.8), and an Acceptance predicate (§1.6, the checkable "is the setpoint
// reached?"). The Goal holds NO subconscious pointer — the old `Skill *string` (the retired
// Goal->MATCH->Skill link) is dropped; the Capability is the goal-matcher, deep in the subconscious
// (§1.6 "No subconscious pointer"). NOTE (honest scope): this builds the entity + its state machine;
// wiring it through the engine loop (replacing the bare graph.Goal string) is a following step.
package cognition

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// GoalSource records where a goal entered the system. Mirrors Python goals.GoalSource (Enum,
// auto() 1-based). iota starts at 1 (the `_ = iota` blank) so the ordinal values match
// Python's auto(), and a name<->value table pair + String() give the .name round-trip that
// Go iota enums otherwise lack.
type GoalSource int

const (
	_           GoalSource = iota // skip 0 so values are 1-based, matching Python auto()
	GoalUser                      // arrived from the user across the perception/intake port
	GoalDrive                     // endogenous — a Drive / default-mode goal (awake mode) — the proactive seed
	GoalSubgoal                   // spawned by decomposing a parent goal
)

var goalSourceNames = map[GoalSource]string{
	GoalUser: "USER", GoalDrive: "DRIVE", GoalSubgoal: "SUBGOAL",
}
var goalSourceByName = invertGoalSource(goalSourceNames)

// String returns the member name (e.g. "USER"), matching Python GoalSource.NAME.name.
func (g GoalSource) String() string {
	if name, ok := goalSourceNames[g]; ok {
		return name
	}
	return "GoalSource(" + itoa(int(g)) + ")"
}

// ParseGoalSource maps a member name back to its value; ok=false for an unknown name
// (the Go form of Python's GoalSource[name] with a caught KeyError).
func ParseGoalSource(s string) (GoalSource, bool) { v, ok := goalSourceByName[s]; return v, ok }

// GoalStatus is a Goal's lifecycle state (§1.7). Every transition is a deliberate setpoint change;
// the legal edges form a small state machine (goalTransitions). It is a string type so the existing
// `Goal{Status: "open"}` literals keep compiling and the wire form stays human-readable.
type GoalStatus string

const (
	GoalOpen       GoalStatus = "open"       // created, not yet matched to a pursuit path
	GoalMatched    GoalStatus = "matched"    // a capability/skill path was found (subconscious afforded one)
	GoalActive     GoalStatus = "active"     // being pursued — the pursuit window; the setpoint is immutable here (§1.3)
	GoalDone       GoalStatus = "done"       // Acceptance MET (terminal)
	GoalAbandoned  GoalStatus = "abandoned"  // gave up / over budget (terminal)
	GoalRefined    GoalStatus = "refined"    // replaced by a sharper child/sibling (terminal)
	GoalSuperseded GoalStatus = "superseded" // a new top goal took over (terminal)
)

// goalTransitions is the legal edge set of the lifecycle (§1.7). A status absent as a key (the four
// terminals) has no outgoing edges. open/matched/active can each be abandoned/superseded at any time.
var goalTransitions = map[GoalStatus][]GoalStatus{
	GoalOpen:    {GoalMatched, GoalActive, GoalAbandoned, GoalSuperseded},
	GoalMatched: {GoalActive, GoalAbandoned, GoalRefined, GoalSuperseded},
	GoalActive:  {GoalDone, GoalAbandoned, GoalRefined, GoalSuperseded},
}

// IsTerminal reports whether the status is terminal (done / abandoned / refined / superseded) — a goal
// that has left its pursuit window for good. A terminal status closes the learning window (§5.2).
func (s GoalStatus) IsTerminal() bool {
	_, hasOutgoing := goalTransitions[s]
	return !hasOutgoing
}

// CanTransition reports whether from->to is a legal lifecycle edge (§1.7).
func CanTransition(from, to GoalStatus) bool {
	for _, allowed := range goalTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// AcceptanceKind records HOW a goal's "done" is authored/checked (Pattern C, §1.6): a deterministic
// floor (an explicit marker, or a grounded observation, or an explicit user confirmation) plus a model
// ceiling for the flagged-fuzzy case.
type AcceptanceKind string

const (
	AcceptUserConfirm AcceptanceKind = "user_confirm" // fall-back floor: the user confirms done (never self-declare on a fuzzy goal)
	AcceptObservation AcceptanceKind = "observation"  // a grounded OK observation answers it
	AcceptMarker      AcceptanceKind = "marker"       // an explicit success marker in the request ("until tests pass")
	AcceptModel       AcceptanceKind = "model"        // a model ceiling judges acceptance on a flagged-fuzzy case
)

// Acceptance is a Goal's success predicate — the checkable "is the setpoint reached?" (§1.6). It is the
// piece the passive record lacked; the RL reward (§5) keys `goal_met` off it. Checked CONTINUOUSLY, it
// yields MET (→ done) or UNMEETABLE (a constraint/infeasibility surfaced → revise the setpoint, §1.9),
// so it is both the stop signal and a feedback input.
type Acceptance struct {
	Kind      AcceptanceKind // how it is authored/checked
	Predicate string         // the checkable condition ("tests pass", "returns X"); "" => user-confirms-done
}

// AcceptanceOutcome is the result of checking a Goal's Acceptance (§1.6).
type AcceptanceOutcome int

const (
	AcceptancePending    AcceptanceOutcome = iota // not yet satisfied, still feasible
	AcceptanceMet                                 // reached → transition to done
	AcceptanceUnmeetable                          // a constraint/infeasibility surfaced → revise (§1.9)
)

// acceptanceMarkers are explicit success-condition cues the intake floor recognises (the Pattern-C
// FLOOR of §1.6). Ordered so authoring is deterministic (first match wins).
var acceptanceMarkers = []struct{ cue, predicate string }{
	{"test", "the tests pass"},
	{"compile", "it compiles"},
	{"build", "it builds"},
	{"run", "it runs"},
	{"work", "it works"},
	{"correct", "it is correct"},
	{"pass", "it passes"},
	{"fix", "the fix holds"},
	{"return", "it returns the value"},
}

// AuthorAcceptance authors a Goal's Acceptance predicate at intake (§1.6) — the Pattern-C FLOOR only (a
// model ceiling is escalated later, where a backend is available). A goal whose text carries an explicit
// success cue gets an AcceptMarker predicate; otherwise it falls back to AcceptUserConfirm (never
// self-declare done on a fuzzy goal).
func AuthorAcceptance(goalText string, src GoalSource) *Acceptance {
	lt := strings.ToLower(goalText)
	for _, m := range acceptanceMarkers {
		if strings.Contains(lt, m.cue) {
			return &Acceptance{Kind: AcceptMarker, Predicate: m.predicate}
		}
	}
	return &Acceptance{Kind: AcceptUserConfirm}
}

// infeasibilityCues flag a goal UNMEETABLE at the floor (a constraint/infeasibility surfaced in the
// working context → revise the setpoint, §1.9). Deterministic, first-match.
var infeasibilityCues = []string{
	"impossible", "cannot be done", "can't be done", "infeasible", "no such", "does not exist",
	"blocked by", "unsupported", "not feasible", "ruled out",
}

// CheckAcceptanceFloor is the Pattern-C deterministic FLOOR of the continuous Acceptance check (§1.6):
// given a Goal's Acceptance and the current working-context text, it returns the outcome plus whether the
// case is flagged-FUZZY (worth the model CEILING). An infeasibility cue in context → Unmeetable; a
// checkable predicate found in context → Met; an AcceptUserConfirm goal (no checkable predicate) with no
// confirmation → Pending + fuzzy (the model ceiling may judge it met); a marker predicate not yet present
// → Pending, NOT fuzzy (keep working — the floor is confident it is not done). nil acceptance → fuzzy.
func CheckAcceptanceFloor(acc *Acceptance, ctxText string) (AcceptanceOutcome, bool) {
	lt := strings.ToLower(ctxText)
	for _, cue := range infeasibilityCues {
		if strings.Contains(lt, cue) {
			return AcceptanceUnmeetable, false
		}
	}
	if acc == nil {
		return AcceptancePending, true
	}
	if acc.Predicate != "" && strings.Contains(lt, strings.ToLower(acc.Predicate)) {
		return AcceptanceMet, false
	}
	if acc.Kind == AcceptUserConfirm {
		return AcceptancePending, true // no checkable predicate → the flagged-fuzzy case
	}
	return AcceptancePending, false // a marker not yet present → keep working
}

// Goal is the conscious layer's SETPOINT (§1.6): a real, transition-gated, tree-structured entity. It
// holds NO reference to any subconscious object (one-way mirror, §1.6) — it shapes the stream, which
// affords Capabilities by relevance; it never names one.
type Goal struct {
	ID         string      // stable handle
	Text       string      // the objective statement
	Source     GoalSource  // USER | DRIVE | SUBGOAL
	Status     GoalStatus  // lifecycle state (§1.7)
	Parent     *string     // the goal this was decomposed from — nil for a top goal (the tree edge)
	Children   []string    // subgoal IDs (the tree; graded-stability leaves, §1.8)
	Acceptance *Acceptance // the success predicate (§1.6); nil until authored
	Level      int         // tree depth — drives graded stability (top stable, leaf fluid)
}

// NewGoal builds a top-level USER Goal at status open, level 0, no acceptance yet.
func NewGoal(id, text string) Goal {
	return Goal{ID: id, Text: text, Source: GoalUser, Status: GoalOpen}
}

// NewSubgoal builds a SUBGOAL of parent: status open, Level = parent.Level+1, Parent set. The caller
// appends the new id to parent.Children (Decompose does both).
func NewSubgoal(id, text string, parent *Goal) Goal {
	pid := parent.ID
	return Goal{ID: id, Text: text, Source: GoalSubgoal, Status: GoalOpen, Parent: &pid, Level: parent.Level + 1}
}

// Transition moves the goal to a new lifecycle status if the edge is legal (§1.7); ok=false on an
// illegal edge (e.g. out of a terminal state, or a self-loop). A deliberate setpoint change.
func (g *Goal) Transition(to GoalStatus) bool {
	if !CanTransition(g.Status, to) {
		return false
	}
	g.Status = to
	return true
}

// Decompose registers child as a subgoal of g (links the tree both ways, §1.8). The child should be
// built via NewSubgoal(.., g) so its Parent/Level are already set; this records the back-edge.
func (g *Goal) Decompose(child *Goal) {
	g.Children = append(g.Children, child.ID)
}

// Conclude drives the goal to a terminal lifecycle state at episode end (§1.7): done when the setpoint
// was reached, else abandoned. A no-op if already terminal. This is what finally makes the lifecycle
// reach DONE — the legacy passive record never did. It snaps to active first (pursuit did happen) so the
// active→{done|abandoned} edge is legal.
func (g *Goal) Conclude(met bool) {
	if g.Status.IsTerminal() {
		return
	}
	g.Status = GoalActive
	if met {
		g.Transition(GoalDone)
	} else {
		g.Transition(GoalAbandoned)
	}
}

// IsTopGoal reports whether this is a root of the forest (no parent, §1.8).
func (g *Goal) IsTopGoal() bool { return g.Parent == nil }

// Short collapses whitespace and truncates the goal text to n runes. Mirrors Python
// Goal.short(n=60) -> ellipsize(self.text, n).
func (g Goal) Short(n int) string { return types.Ellipsize(g.Text, n) }

// ShortDefault truncates at the Python default width of 60 (Goal.short()).
func (g Goal) ShortDefault() string { return g.Short(60) }

// invertGoalSource builds the name->value reverse of a value->name table.
func invertGoalSource(m map[GoalSource]string) map[string]GoalSource {
	out := make(map[string]GoalSource, len(m))
	for v, name := range m {
		out[name] = v
	}
	return out
}
