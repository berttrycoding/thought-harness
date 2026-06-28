package decisionoracle

import (
	"strings"
	"testing"
)

// bankPath is the A2 decision/ship fixture bank, relative to this package dir.
const bankPath = "../banks/decision-quality/decision-quality.jsonl"

func loadBank(t *testing.T) []Fixture {
	t.Helper()
	fx, err := LoadFixtures(bankPath)
	if err != nil {
		t.Fatalf("load bank %q: %v", bankPath, err)
	}
	if len(fx) == 0 {
		t.Fatalf("bank %q is empty", bankPath)
	}
	return fx
}

func effThreshold(fx Fixture) float64 {
	if fx.PassThreshold > 0 {
		return fx.PassThreshold
	}
	switch fx.Worker {
	case WorkerVerifier:
		return defaultVerifierThreshold
	default:
		return defaultDeliberatorThreshold
	}
}

// TestDiscrimination is THE fail-discriminating control: on every fixture, the GOOD
// verdict (the correct pick / correct accept-refuse) must score >= the threshold (a
// PASS) and the BAD verdict (the wrong pick / the false-accept of a bad claim / the
// false-refuse of a true one) must score below it (a FAIL), with a clear margin. An
// oracle that scored both the same could not tell a right decision from a wrong one
// and would be worthless.
func TestDiscrimination(t *testing.T) {
	fixtures := loadBank(t)
	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.ID, func(t *testing.T) {
			threshold := effThreshold(fx)
			good := ScoreVerdict(fx.GoodVerdict, fx)
			bad := ScoreVerdict(fx.BadVerdict, fx)

			if !good.Decided {
				t.Fatalf("GOOD verdict did not decide: %s", good.Reason)
			}
			if !good.Pass {
				t.Errorf("GOOD verdict should PASS but scored %.3f < threshold %.3f\n  reason: %s",
					good.Score, threshold, good.Reason)
			}
			if bad.Pass {
				t.Errorf("BAD verdict should FAIL but scored %.3f >= threshold %.3f\n  reason: %s",
					bad.Score, threshold, bad.Reason)
			}
			if good.Score <= bad.Score {
				t.Errorf("no discrimination: GOOD %.3f <= BAD %.3f\n  good: %s\n  bad:  %s",
					good.Score, bad.Score, good.Reason, bad.Reason)
			}
			if margin := good.Score - bad.Score; margin < 0.15 {
				t.Errorf("discrimination margin too thin: GOOD %.3f - BAD %.3f = %.3f (< 0.15)",
					good.Score, bad.Score, margin)
			}
			// The BAD verdict must fail by a real distance below the threshold, not by a
			// hair — else a future weight retune silently lets a wrong decision slip past.
			if gap := threshold - bad.Score; gap < 0.05 {
				t.Errorf("BAD-to-threshold gap too thin: threshold %.3f - BAD %.3f = %.3f (< 0.05) — weight-fragile",
					threshold, bad.Score, gap)
			}
			t.Logf("OK [%s] good=%.3f bad=%.3f margin=%.3f thr=%.2f", fx.Worker, good.Score, bad.Score, good.Score-bad.Score, threshold)
		})
	}
}

// TestBankWinnersAreComputedNotAsserted re-derives each deliberator fixture's winner
// from its options+weights and proves it is UNTIED and that the GOOD verdict picks it.
// This is the soundness guarantee that the ground truth is a pure function the test can
// reproduce — never an unchecked authored label.
func TestBankWinnersAreComputedNotAsserted(t *testing.T) {
	for _, fx := range loadBank(t) {
		if fx.Worker != WorkerDeliberator {
			continue
		}
		fx := fx
		t.Run(fx.ID, func(t *testing.T) {
			winner, tied, ok := BetterOption(fx)
			if !ok {
				t.Fatalf("no computable winner (missing options/weights)")
			}
			// A fixture that DECLARES itself genuinely tied (correct_verdict=undecided) must
			// ACTUALLY tie under its weights, and its GOOD verdict must be the honest "undecided"
			// abstain — never a pick. This is the soundness guarantee for the abstain fixture: the
			// "no winner" ground truth is a pure function the test re-derives, not an authored label.
			if fx.CorrectVerdict == "undecided" {
				if !tied {
					t.Errorf("fixture declares correct_verdict=undecided but its options do NOT tie under the weights (bank error)")
				}
				if !fx.GoodVerdict.Undecided {
					t.Errorf("a tied fixture's GOOD verdict must be the honest undecided abstain, got picked_option=%q undecided=%v", fx.GoodVerdict.PickedOption, fx.GoodVerdict.Undecided)
				}
				t.Logf("genuinely tied (correct_verdict=undecided); GOOD verdict is the honest abstain")
				return
			}
			if tied {
				t.Fatalf("options tie under the fixture weights — no determinable winner (bank error)")
			}
			id, matched := matchOption(fx.GoodVerdict.PickedOption, fx.Options)
			if !matched {
				t.Fatalf("GOOD pick %q matches no option", fx.GoodVerdict.PickedOption)
			}
			if id != winner {
				t.Errorf("GOOD verdict picks %q but the COMPUTED winner is %q — bank authored truth disagrees with the math", id, winner)
			}
			t.Logf("computed winner=%s, GOOD picks=%s", winner, id)
		})
	}
}

// TestNoVerdictFailsHard proves a missing verdict (no pick for a deliberator, no
// accept/refuse for a verifier) is a HARD fail (Decided=false, Score 0), never a
// silent pass.
func TestNoVerdictFailsHard(t *testing.T) {
	del := Fixture{ID: "p", Worker: WorkerDeliberator,
		CriteriaWeights: map[string]float64{"x": 1},
		Options:         []Option{{ID: "a", Scores: map[string]float64{"x": 1}}, {ID: "b", Scores: map[string]float64{"x": 0}}}}
	s := ScoreVerdict(Verdict{}, del)
	if s.Decided || s.Pass || s.Score != 0 {
		t.Errorf("deliberator with no pick should hard-fail: decided=%v pass=%v score=%.3f", s.Decided, s.Pass, s.Score)
	}

	ver := Fixture{ID: "q", Worker: WorkerVerifier, Truth: ClaimTrue}
	s = ScoreVerdict(Verdict{}, ver)
	if s.Decided || s.Pass || s.Score != 0 {
		t.Errorf("verifier with no decision should hard-fail: decided=%v pass=%v score=%.3f", s.Decided, s.Pass, s.Score)
	}
}

// TestDangerousFalseAcceptCappedAtZero proves the most important Verifier property: a
// confident, well-reasoned ACCEPT of a FALSE claim (shipping a bug) is capped at 0 and
// can NEVER be rescued by a sound-sounding rationale.
func TestDangerousFalseAcceptCappedAtZero(t *testing.T) {
	fx := Fixture{ID: "fa", Worker: WorkerVerifier, Truth: ClaimFalse,
		Evidence: []string{"index out of range"}}
	// Accept, with reasoning that even cites the settling evidence — must still cap at 0.
	v := Verdict{Decision: DecisionAccept, Reasoning: "I see the index out of range note but it looks fine to me, ship it."}
	s := ScoreVerdict(v, fx)
	if s.Pass || s.Score != 0 {
		t.Errorf("false-accept of a false claim must cap at 0: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
	}
	if !strings.Contains(s.Reason, "DANGEROUS") {
		t.Errorf("false-accept reason should name the danger: %s", s.Reason)
	}
}

// TestUnknowableNeedsHonestRefuse proves the never-confabulate verdict: on a claim with
// no available evidence, a confident ACCEPT is dangerous (0), a refuse WITHOUT the
// honesty flag is wrong (0, blocked the ship but for a confabulated reason), and only a
// refuse WITH the honest "cannot verify" flag scores correct.
func TestUnknowableNeedsHonestRefuse(t *testing.T) {
	fx := Fixture{ID: "u", Worker: WorkerVerifier, Truth: ClaimUnknowable}

	accept := ScoreVerdict(Verdict{Decision: DecisionAccept, Reasoning: "yes it's 42"}, fx)
	if accept.Pass || accept.Score != 0 {
		t.Errorf("confident accept of an unknowable claim must fail: pass=%v score=%.3f", accept.Pass, accept.Score)
	}
	guessRefuse := ScoreVerdict(Verdict{Decision: DecisionRefuse, Honest: false, Reasoning: "no, it's actually 7"}, fx)
	if guessRefuse.Pass {
		t.Errorf("refuse-by-counter-claim (not honest) on an unknowable claim must fail: score=%.3f", guessRefuse.Score)
	}
	honestRefuse := ScoreVerdict(Verdict{Decision: DecisionRefuse, Honest: true, Reasoning: "I cannot verify this private constant"}, fx)
	if !honestRefuse.Pass {
		t.Errorf("honest refuse of an unknowable claim should pass: score=%.3f reason=%s", honestRefuse.Score, honestRefuse.Reason)
	}
}

// TestUnknownWorkerNeverPasses proves a fixture with an unknown worker (a bank error)
// scores 0 and fails loud — never a vacuous pass.
func TestUnknownWorkerNeverPasses(t *testing.T) {
	fx := Fixture{ID: "bad", Worker: Worker("frobnicator")}
	s := ScoreVerdict(Verdict{PickedOption: "a"}, fx)
	if s.Pass || s.Score != 0 {
		t.Errorf("unknown-worker fixture must never pass: pass=%v score=%.3f", s.Pass, s.Score)
	}
	if !strings.Contains(s.Reason, "unknown worker") {
		t.Errorf("reason should name the bank error: %s", s.Reason)
	}
}

// TestVacuousFixtureNeverPasses proves a fixture that constrains nothing (no options /
// no truth) scores 0 and fails — a bank error must never read as a vacuous pass.
func TestVacuousFixtureNeverPasses(t *testing.T) {
	del := Fixture{ID: "vd", Worker: WorkerDeliberator}
	if s := ScoreVerdict(Verdict{PickedOption: "a"}, del); s.Pass || s.Score != 0 {
		t.Errorf("deliberator with no options must never pass: pass=%v score=%.3f", s.Pass, s.Score)
	}
	ver := Fixture{ID: "vv", Worker: WorkerVerifier}
	if s := ScoreVerdict(Verdict{Decision: DecisionAccept}, ver); s.Pass || s.Score != 0 {
		t.Errorf("verifier with no truth must never pass: pass=%v score=%.3f", s.Pass, s.Score)
	}
}
