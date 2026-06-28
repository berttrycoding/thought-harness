package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// awake_engage_test.go — the AWAKE-DISP rung-1 cognition gate
// (docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md, the "Rung 1" section).
//
// Rung 0 (awake_user_dispatch) gives a focused awake user line a synthesised workflow so the subconscious
// FIRES on it. Rung 1 (conscious.activity.awake_user_engage) is the deterministic VALUE FLOOR on top: a
// FOCUSED, UNRESOLVED user line's V(s) carries an additive engagement boost (the awake_user_engage_weight
// knob) so the line RELIABLY OUT-COMPETES the endogenous wander / default-mode lines and WINS the
// produce-competition (the frontier rerank + pursuit-threshold resume in continuous.go). "Smart, not
// forced" — it lifts the line the user is waiting on RIGHT NOW; a set-aside / resolved line is left to the
// standing pendingUserTerm. Pattern-A: a pure deterministic value computation, NO model call.
//
// THE COGNITION (what these tests assert, not just the plumbing):
//   - With the flag OFF the focused user line's V(s) is the standing pendingUserTerm-only value, which
//     DECAYS as the line accrues lower-confidence generated thoughts (recent_conf falls) — so on a longer
//     line it can sink toward / below a competing wander line and LOSE the competition.
//   - With the flag ON the focused user line's V(s) is pinned at the top of the [0,1] range by the boost,
//     so it stays STRICTLY ABOVE every endogenous wander line every tick it holds focus — it reliably WINS
//     the produce-competition within a bounded number of ticks. The conscious.engage event witnesses it.
//   - Byte-identical OFF: the per-tick value stream + the awake stream are unchanged with the flag OFF
//     (the value floor is inert), so the goldens are untouched. A direct OFF A/B is asserted here too.

// engageInput is a deliberately leisurely, open-ended thinking prompt: a line that DEVELOPS over several
// ticks rather than resolving in one — so the OFF-path V(s) decay (and thus the ON-path boost holding it
// up) is observable across the produce-competition window.
const engageInput = "ponder slowly: what is the most elegant way to model a token-bucket rate limiter, and why"

// newAwakeEngineWithEngage builds an awake engine on the validated awake defaults (ApplyAwakeDefaults),
// with the rung-1 engagement flag set explicitly (and the conservative default weight). The test double
// fires specialists / reranks FOR REAL (only CONTENT is canned), so the value-floor question — does the
// focused user line out-compete the wander? — is answered deterministically offline.
func newAwakeEngineWithEngage(t *testing.T, awakeUserEngage bool) (*engine.Engine, *eventLog) {
	t.Helper()
	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Conscious.Activity.AwakeUserEngage = awakeUserEngage
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// engageTrace wakes the awake stream, submits engageInput mid-wander, runs the produce-competition for a
// bounded window, and returns per-tick: the focused unresolved user line's max V(s) (or -1 when no
// unresolved user line is live that tick), the max V(s) over all NON-user (wander/drive) lines, and the
// running conscious.engage count.
type engageTick struct {
	userV, otherV float64
	hasUser       bool
	engageCount   int
}

func engageTrace(t *testing.T, awakeUserEngage bool, ticks int) []engageTick {
	t.Helper()
	eng, _ := newAwakeEngineWithEngage(t, awakeUserEngage)
	var engageCount int
	eng.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Engage {
			engageCount++
		}
	})
	for i := 0; i < 4; i++ {
		eng.Step() // already awake + wandering before the user turn lands
	}
	eng.SubmitDefault(engageInput)
	out := make([]engageTick, 0, ticks)
	for i := 0; i < ticks; i++ {
		eng.Step()
		g := eng.Graph()
		userV, otherV := -1.0, -1.0
		hasUser := false
		for bid, b := range g.Branches {
			if g.UnresolvedUserInput(bid) {
				hasUser = true
				if b.Value > userV {
					userV = b.Value
				}
			} else if b.Value > otherV {
				otherV = b.Value
			}
		}
		out = append(out, engageTick{userV: userV, otherV: otherV, hasUser: hasUser, engageCount: engageCount})
	}
	return out
}

// TestAwakeUserEngageWinsCompetition is the rung-1 COGNITION gate: with the flag ON the focused awake user
// line's V(s) is pinned at the top by the engagement boost, so it STRICTLY OUT-RANKS every endogenous
// wander line for every tick it holds focus (it wins the produce-competition), and the conscious.engage
// event fires within a bounded number of ticks. With the flag OFF the user line's V(s) DECAYS (the boost
// is absent) and no engage event ever fires.
func TestAwakeUserEngageWinsCompetition(t *testing.T) {
	const ticks = 6
	off := engageTrace(t, false, ticks)
	on := engageTrace(t, true, ticks)

	// FLAG ON: conscious.engage must fire within a bounded number of ticks (the floor activated on the
	// focused user line). FLAG OFF: it must NEVER fire (the floor is inert).
	if off[len(off)-1].engageCount != 0 {
		t.Fatalf("flag OFF: conscious.engage fired %d times — the value floor must be inert when off", off[len(off)-1].engageCount)
	}
	firstEngage := -1
	for i, tk := range on {
		if tk.engageCount > 0 {
			firstEngage = i
			break
		}
	}
	if firstEngage < 0 {
		t.Fatalf("flag ON: conscious.engage never fired across %d ticks — the value floor did not engage the user line", ticks)
	}
	t.Logf("flag ON: first conscious.engage at tick %d (engage total=%d)", firstEngage, on[len(on)-1].engageCount)

	// FLAG ON COGNITION: for every tick the focused user line is live, its V(s) must STRICTLY out-rank the
	// best wander/drive line — it reliably WINS the produce-competition (the frontier resume always picks
	// it over self-directed wander). At least one such tick must exist (the line must actually be live).
	sawUser := 0
	for i, tk := range on {
		if !tk.hasUser {
			continue
		}
		sawUser++
		if tk.userV <= tk.otherV {
			t.Fatalf("flag ON tick %d: focused user V(s)=%.3f did NOT strictly out-rank the best wander line V(s)=%.3f — "+
				"the engagement floor must make the user line win the produce-competition", i, tk.userV, tk.otherV)
		}
	}
	if sawUser == 0 {
		t.Fatalf("flag ON: the user line was never live across %d ticks — the test scenario did not exercise the competition", ticks)
	}

	// THE DISCRIMINATOR (the boost is real, not cosmetic): on at least one matched tick where BOTH paths
	// hold the user line, the ON-path V(s) must be STRICTLY GREATER than the OFF-path V(s). OFF decays; ON
	// is pinned high by the boost. This proves the boost lands on the focused user line and lifts its value
	// above what pendingUserTerm alone yields — the mechanism the competition advantage is built on.
	lifted := false
	for i := range on {
		if i >= len(off) {
			break
		}
		if on[i].hasUser && off[i].hasUser && on[i].userV > off[i].userV+1e-9 {
			lifted = true
			t.Logf("tick %d: user V(s) OFF=%.3f -> ON=%.3f (engagement boost lifted the focused line)", i, off[i].userV, on[i].userV)
			break
		}
	}
	if !lifted {
		t.Fatalf("flag ON never lifted the focused user line's V(s) above the OFF path on any matched tick — "+
			"the engagement boost did not change the value of the line it is meant to engage (OFF=%v ON=%v)", off, on)
	}
}

// TestAwakeUserEngageOffByteIdentical is the byte-identical-OFF gate: flipping the rung-1 flag OFF must
// leave the per-tick value stream of the awake run unchanged from a run with the flag absent entirely (the
// value floor is inert when off). Paired with the committed continuous/reactive goldens (untouched by this
// change), this is the additive-default-OFF guarantee for the value path. (The engage event is asserted
// absent in TestAwakeUserEngageWinsCompetition; here we assert the VALUE numbers are unperturbed.)
func TestAwakeUserEngageOffByteIdentical(t *testing.T) {
	const ticks = 8
	// Two OFF traces: one via the explicit flag=false set, one via a config that never touches the flag at
	// all (default). They must produce identical per-tick user/other V(s) — the floor adds nothing when off.
	a := engageTrace(t, false, ticks)

	// b: a config that never sets the rung-1 flag (default OFF), to confirm the explicit-false path and the
	// untouched-default path are the same — no accidental dependence on the field being addressed.
	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var engageCount int
	eng.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Engage {
			engageCount++
		}
	})
	for i := 0; i < 4; i++ {
		eng.Step()
	}
	eng.SubmitDefault(engageInput)
	for i := 0; i < ticks; i++ {
		eng.Step()
		g := eng.Graph()
		userV, otherV := -1.0, -1.0
		hasUser := false
		for bid, bb := range g.Branches {
			if g.UnresolvedUserInput(bid) {
				hasUser = true
				if bb.Value > userV {
					userV = bb.Value
				}
			} else if bb.Value > otherV {
				otherV = bb.Value
			}
		}
		if a[i].hasUser != hasUser || a[i].userV != userV || a[i].otherV != otherV {
			t.Fatalf("tick %d: OFF value stream not byte-identical between explicit-false and default-off: "+
				"explicit{hasUser=%v userV=%.6f otherV=%.6f} default{hasUser=%v userV=%.6f otherV=%.6f}",
				i, a[i].hasUser, a[i].userV, a[i].otherV, hasUser, userV, otherV)
		}
	}
	if engageCount != 0 {
		t.Fatalf("default-OFF: conscious.engage fired %d times — the value floor must be inert when the flag is off", engageCount)
	}
}

// TestAwakeUserEngageReactiveExempt confirms the floor is AWAKE-ONLY: in reactive mode the engagement boost
// is never applied and conscious.engage never fires, EVEN with the flag set on. The value signal is shared
// between the loops; the awake predicate (e.mode == "continuous") gates the boost to the awake loop, so a
// reactive run carries the standing pendingUserTerm path unchanged.
func TestAwakeUserEngageReactiveExempt(t *testing.T) {
	feat := config.New() // reactive => AllOn baseline, no ApplyAwakeDefaults
	feat.Conscious.Activity.AwakeUserEngage = true
	feat.Conscious.Activity.AwakeUserEngageWeight = 0.5
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = feat
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var engageCount int
	eng.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Engage {
			engageCount++
		}
	})
	eng.SubmitDefault(engageInput)
	for i := 0; i < 12; i++ {
		eng.Step()
	}
	if engageCount != 0 {
		t.Fatalf("reactive mode with the flag ON: conscious.engage fired %d times — the engagement floor must be awake-only", engageCount)
	}
}
