package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// judgeBackend embeds the deterministic test double and adds the ConscienceJudge ceiling, so the engine's
// e.backend satisfies backends.ConscienceJudge. It refuses any action whose text mentions "secret".
type judgeBackend struct {
	*backends.TestBackend
	decided bool
}

func (j judgeBackend) JudgeConscience(text string) (allow bool, reason string, ok bool) {
	if !j.decided {
		return true, "", false // model declined -> floor stands
	}
	if containsLower(text, "secret") {
		return false, "exfiltrating a secret is wrong", true
	}
	return true, "", true
}

// JudgeAcceptance is the acceptance model ceiling: when it decides, it declares the goal "met".
func (j judgeBackend) JudgeAcceptance(goal string, ctx []types.Thought) (string, bool) {
	if !j.decided {
		return "", false // model declined -> floor stands
	}
	return "met", true
}

// JudgeEngagement is the AWAKE-DISP rung-2 engagement ceiling double: when it decides, it engages a line
// that mentions "consider" (the worth-engaging signal — deliberately NOT a cognition.RecognizeShape task
// keyword, so the ceiling, not the floor, is the deciding layer in the rung-2 tests) and quiets everything
// else.
func (j judgeBackend) JudgeEngagement(goal, recentContext, floorVerdict string) (string, bool) {
	if !j.decided {
		return "", false // model declined -> floor stands
	}
	if containsLower(goal, "consider") {
		return "engage", true
	}
	return "quiet", true
}

func containsLower(s, sub string) bool {
	return len(s) >= len(sub) && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// TestConscienceCeiling pins slice (k) CEILING / §7.2 (Pattern-C): with conscience_ceiling on and a
// ConscienceJudge backend, a flagged-fuzzy action the floor ALLOWED is escalated and the model may TIGHTEN
// (refuse). A non-fuzzy action is never escalated; a declined model lets the floor stand; the test double
// (no judge) lets the floor stand (byte-identical).
func TestConscienceCeiling(t *testing.T) {
	mk := func(be backends.Backend, on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New()
		feat.Conscious.Activity.ConscienceCeiling = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, be)
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		return e
	}

	// judge that decides + a fuzzy action mentioning a secret -> the ceiling REFUSES.
	e := mk(judgeBackend{TestBackend: backends.NewTest(), decided: true}, true)
	if refuse, why := e.conscienceCeilingRefuses("delete and publish the secret keys"); !refuse || why == "" {
		t.Errorf("fuzzy + secret: expected the ceiling to refuse, got refuse=%v why=%q", refuse, why)
	}
	// a fuzzy action WITHOUT a secret -> the model allows -> not refused.
	if refuse, _ := e.conscienceCeilingRefuses("delete the temp file"); refuse {
		t.Error("fuzzy + benign: the ceiling should allow")
	}
	// a NON-fuzzy action -> never escalated, never refused.
	if refuse, _ := e.conscienceCeilingRefuses("read the config file"); refuse {
		t.Error("non-fuzzy: must not escalate or refuse")
	}

	// model DECLINES (decided=false) -> floor stands + escalation.floor_stands surfaced.
	eDecline := mk(judgeBackend{TestBackend: backends.NewTest(), decided: false}, true)
	stood := false
	eDecline.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.EscalationFloorStands) {
			stood = true
		}
	})
	if refuse, _ := eDecline.conscienceCeilingRefuses("publish the secret"); refuse {
		t.Error("model declined: floor must stand (not refused)")
	}
	if !stood {
		t.Error("model declined: expected escalation.floor_stands")
	}

	// no judge (the plain test double) -> floor stands, byte-identical.
	ePlain := mk(backends.NewTest(), true)
	if refuse, _ := ePlain.conscienceCeilingRefuses("publish the secret"); refuse {
		t.Error("no judge: floor must stand")
	}

	// ceiling OFF -> never escalates regardless of judge.
	eOff := mk(judgeBackend{TestBackend: backends.NewTest(), decided: true}, false)
	if refuse, _ := eOff.conscienceCeilingRefuses("publish the secret"); refuse {
		t.Error("ceiling OFF: must not escalate")
	}
}

// TestAcceptanceCeiling pins the Acceptance model ceiling (#29, §1.6): a flagged-fuzzy goal (no checkable
// predicate -> AcceptUserConfirm) is escalated to a backends.AcceptanceJudge when acceptance_ceiling is on,
// and the model's "met" verdict is adopted. OFF / no judge / model declined -> the floor (Pending) stands.
func TestAcceptanceCeiling(t *testing.T) {
	mk := func(be backends.Backend, on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New()
		feat.Conscious.Activity.AcceptanceCeiling = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, be)
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		e.startEpisode("think about something open-ended", true) // fuzzy: AcceptUserConfirm, no marker
		return e
	}

	g := func(e *Engine) *cognition.Goal { return e.activeGoal() }

	// ON + a deciding judge -> the fuzzy floor (Pending) is lifted to the model's "met".
	e := mk(judgeBackend{TestBackend: backends.NewTest(), decided: true}, true)
	if out := e.acceptanceCeiling(g(e)); out != cognition.AcceptanceMet {
		t.Errorf("ceiling ON + judge: want Met, got %v", out)
	}

	// model declines -> floor stands (Pending).
	eDecline := mk(judgeBackend{TestBackend: backends.NewTest(), decided: false}, true)
	if out := eDecline.acceptanceCeiling(g(eDecline)); out != cognition.AcceptancePending {
		t.Errorf("model declined: want Pending (floor stands), got %v", out)
	}

	// no judge (plain double) -> floor stands.
	ePlain := mk(backends.NewTest(), true)
	if out := ePlain.acceptanceCeiling(g(ePlain)); out != cognition.AcceptancePending {
		t.Errorf("no judge: want Pending (floor stands), got %v", out)
	}

	// OFF -> never escalates.
	eOff := mk(judgeBackend{TestBackend: backends.NewTest(), decided: true}, false)
	if out := eOff.acceptanceCeiling(g(eOff)); out != cognition.AcceptancePending {
		t.Errorf("ceiling OFF: want Pending, got %v", out)
	}
}
