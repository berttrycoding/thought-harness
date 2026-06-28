package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestSoftLearnerREINFORCE is the graded-eval floor for Phase 5 (learning): a positive goal-relative
// return after the policy chose BRANCH drifts β_branch UP; a negative return drifts it DOWN; an empty
// trajectory is a safe no-op. The update is the closed-form softmax log-prob gradient (§5.2).
func TestSoftLearnerREINFORCE(t *testing.T) {
	step := softStep{tau: 0.8, zBranch: 0.4, piBranch: 0.4, choseBranch: true}

	mk := func() (*Controller, *config.ConsciousActivityCfg) {
		a := config.DefaultConsciousActivity()
		a.BranchPropensity, a.LearnRate = 1.0, 0.5
		c := NewController(noEmit, nil, "control", nil)
		c.SetActivityConfig(&a)
		return c, &a
	}

	// positive return after choosing BRANCH -> β increases.
	cPos, aPos := mk()
	cPos.traj = []softStep{step}
	cPos.LearnFromReturn(1.0)
	if aPos.BranchPropensity <= 1.0 {
		t.Errorf("positive return: β = %v, want > 1.0", aPos.BranchPropensity)
	}

	// negative return after choosing BRANCH -> β decreases.
	cNeg, aNeg := mk()
	cNeg.traj = []softStep{step}
	cNeg.LearnFromReturn(-1.0)
	if aNeg.BranchPropensity >= 1.0 {
		t.Errorf("negative return: β = %v, want < 1.0", aNeg.BranchPropensity)
	}

	// no trajectory -> no change, no panic.
	cNil, aNil := mk()
	cNil.LearnFromReturn(1.0)
	if aNil.BranchPropensity != 1.0 {
		t.Errorf("empty traj changed β to %v", aNil.BranchPropensity)
	}
}

// TestSoftLearnerTemperature is the graded-eval floor for τ-learning (§5.3): a positive return after the
// policy chose a HIGH-value move (qChosen > qExpected) SHARPENS the policy (τ down → exploit); a positive
// return after a LOW-value exploratory move (qChosen < qExpected) SOFTENS it (τ up → explore). The update
// is the temperature gradient (E_π[q] − q_chosen)/τ², baselined by the running-mean return.
func TestSoftLearnerTemperature(t *testing.T) {
	mk := func() (*Controller, *config.ConsciousActivityCfg) {
		a := config.DefaultConsciousActivity()
		a.Temperature, a.LearnRate = 0.5, 0.5
		c := NewController(noEmit, nil, "control", nil)
		c.SetActivityConfig(&a)
		return c, &a
	}

	// chose a higher-than-average-value move and it paid off -> exploit more (τ falls).
	cHi, aHi := mk()
	cHi.traj = []softStep{{tau: 0.5, zBranch: 0.4, piBranch: 0.4, choseBranch: true, qChosen: 0.8, qExpected: 0.5}}
	cHi.LearnFromReturn(1.0)
	if aHi.Temperature >= 0.5 {
		t.Errorf("good return from a high-value move: τ = %v, want < 0.5 (sharpen)", aHi.Temperature)
	}

	// chose a lower-than-average-value (exploratory) move and it paid off -> explore more (τ rises).
	cLo, aLo := mk()
	cLo.traj = []softStep{{tau: 0.5, zBranch: 0.4, piBranch: 0.4, choseBranch: true, qChosen: 0.2, qExpected: 0.5}}
	cLo.LearnFromReturn(1.0)
	if aLo.Temperature <= 0.5 {
		t.Errorf("good return from an exploratory move: τ = %v, want > 0.5 (soften)", aLo.Temperature)
	}

	// τ stays inside [0.05, 1.0] under a large advantage.
	cClamp, aClamp := mk()
	cClamp.traj = []softStep{{tau: 0.5, zBranch: 0.4, piBranch: 0.4, choseBranch: true, qChosen: 0.2, qExpected: 0.5}}
	cClamp.LearnFromReturn(50.0)
	if aClamp.Temperature > 1.0 || aClamp.Temperature < 0.05 {
		t.Errorf("τ escaped [0.05, 1.0]: %v", aClamp.Temperature)
	}
}
