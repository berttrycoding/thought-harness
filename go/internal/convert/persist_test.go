package convert

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestPrimitiveSubAgentExportSeedRoundTrip: a minted specialist exported from one Convertibility and seeded into
// a fresh one is re-registered with the SAME triggers/answer and fires for its own trigger — the M4
// cross-session round-trip for the convert-learned state. The pattern bookkeeping is rebuilt so
// keep-or-revert still applies.
func TestPrimitiveSubAgentExportSeedRoundTrip(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)
	goal := "compute the tax bracket threshold value"
	c.Observe(buildEpisode(goal, 3, 0.9))
	if minted := c.Consolidate(); len(minted) != 1 {
		t.Fatalf("expected one mint, got %v", minted)
	}

	recs := c.ExportSpecialists()
	if len(recs) != 1 {
		t.Fatalf("expected one exported specialist record, got %d", len(recs))
	}
	if recs[0].Answer == "" || len(recs[0].Triggers) == 0 {
		t.Fatalf("the exported record must carry the real answer + triggers: %+v", recs[0])
	}

	// seed a FRESH Convertibility (new run) from the records — the specialist must re-register and fire.
	reg2 := &fakeReg{}
	c2 := New(reg2, nil, nil, nil)
	c2.SeedPrimitiveSubAgents(recs)
	sp := mintedPrimitiveSubAgent(reg2)
	if sp == nil {
		t.Fatal("SeedSpecialists must re-register the minted specialist")
	}
	if sp.Relevance([]types.Thought{{Text: goal}}) <= 0 {
		t.Fatal("the re-seeded specialist should fire for its own trigger after a restart")
	}
	if len(c2.Minted) != 1 {
		t.Fatalf("the re-seeded mint should be tracked in Minted, got %v", c2.Minted)
	}
}

// TestDemotedPrimitiveSubAgentStaysDemotedAcrossRestart: a refuted (demoted) specialist re-seeds already dark —
// keep-or-revert is not silently un-done by a restart.
func TestDemotedPrimitiveSubAgentStaysDemotedAcrossRestart(t *testing.T) {
	reg := &fakeReg{}
	c := New(reg, nil, nil, nil)
	goal := "estimate the disk cache hit rate value"
	c.Observe(buildEpisode(goal, 3, 0.9))
	c.Consolidate()
	c.Observe(buildEpisode(goal, 1, 0.0)) // reality refutes
	c.Consolidate()                       // keep-or-revert demotes

	recs := c.ExportSpecialists()
	if len(recs) != 1 || !recs[0].Demoted {
		t.Fatalf("the exported record should be marked demoted, got %+v", recs)
	}
	reg2 := &fakeReg{}
	c2 := New(reg2, nil, nil, nil)
	c2.SeedPrimitiveSubAgents(recs)
	sp := mintedPrimitiveSubAgent(reg2)
	if sp == nil || !sp.Demoted() {
		t.Fatalf("a demoted specialist must re-seed already demoted (stays dark)")
	}
	if sp.Relevance([]types.Thought{{Text: goal}}) != 0 {
		t.Fatal("a re-seeded demoted specialist must not fire")
	}
}

// TestProgramRunExportSeedRoundTrip is the W5-2b durable-recurrence-counter round-trip: a recurring
// program run (count 2, not yet minted) exported from one Convertibility and seeded into a FRESH one
// RESUMES the count — so a later NoteProgram that pushes it to MintAfter (3) mints the skill, proving
// the counter is what crosses the threshold across episodes (not in-memory survival).
func TestProgramRunExportSeedRoundTrip(t *testing.T) {
	fm := &fakeMinter{}
	c := New(&fakeReg{}, nil, nil, fm)
	goal := "analyze why payments service is slow"
	prog := fakeProg{shape: "seq(decompose, hypothesize, measure)"}
	c.NoteProgram(goal, prog) // count 1
	c.NoteProgram(goal, prog) // count 2 (still below MintAfter=3)

	recs := c.ExportProgramRuns()
	if len(recs) != 1 || recs[0].Count != 2 || recs[0].Minted {
		t.Fatalf("expected one un-minted run at count 2, got %+v", recs)
	}
	if recs[0].Program == nil || recs[0].Program.Shape() != prog.Shape() {
		t.Fatalf("the exported run must carry the Program body: %+v", recs[0])
	}

	// a FRESH Convertibility (new run) seeded from the records must RESUME the count, not reset to 1.
	fm2 := &fakeMinter{}
	c2 := New(&fakeReg{}, nil, nil, fm2)
	c2.SeedProgramRuns(recs)
	c2.NoteProgram(goal, prog) // count 2 -> 3 (resumed), now at MintAfter
	c2.Observe(buildEpisode(goal, 3, 0.8))
	c2.Consolidate()
	if len(fm2.minted) == 0 {
		t.Fatal("the resumed count should reach MintAfter and mint the skill (the counter did not persist)")
	}
	if len(c2.MintedSkill) == 0 {
		t.Fatalf("the minted skill must be recorded, got %v", c2.MintedSkill)
	}
}

// TestProgramRunSeedSkipsNilBodyAndDuplicate: a record with no Program body or a duplicate goalKey is
// skipped (it cannot re-mint / keep the live one), never a panic.
func TestProgramRunSeedSkipsNilBodyAndDuplicate(t *testing.T) {
	c := New(&fakeReg{}, nil, nil, &fakeMinter{})
	c.SeedProgramRuns([]ProgramRunRecord{{GoalKey: "k", Count: 5, Program: nil}}) // nil body -> skipped
	if len(c.ProgramRuns()) != 0 {
		t.Fatalf("a nil-body run must be skipped, got %v", c.ProgramRuns())
	}
	prog := fakeProg{shape: "seq(a)"}
	c.SeedProgramRuns([]ProgramRunRecord{{GoalKey: "k", Count: 2, Program: prog}})
	c.SeedProgramRuns([]ProgramRunRecord{{GoalKey: "k", Count: 9, Program: prog}}) // duplicate key -> skipped
	runs := c.ProgramRuns()
	if len(runs) != 1 || runs[0].Count != 2 {
		t.Fatalf("a duplicate goalKey must keep the live run (count 2), got %+v", runs)
	}
}

// TestGatePriorExportSeedRoundTrip: compiled gate priors export + re-seed verbatim.
func TestGatePriorExportSeedRoundTrip(t *testing.T) {
	c := New(&fakeReg{}, nil, nil, nil)
	c.GatePrior["compute"] = 0.2
	c.GatePrior["recall"] = 0.1
	out := c.ExportGatePriors()
	if out["compute"] != 0.2 || out["recall"] != 0.1 {
		t.Fatalf("gate priors export mismatch: %+v", out)
	}
	c2 := New(&fakeReg{}, nil, nil, nil)
	c2.SeedGatePriors(out)
	if c2.GatePrior["compute"] != 0.2 || c2.GatePrior["recall"] != 0.1 {
		t.Fatalf("gate priors did not re-seed: %+v", c2.GatePrior)
	}
}
