package engine

// Internal (package engine) test for the OFFLINE-RL flywheel's REFUTED-label arm (§6.5): the engine maps a
// grounding-spine REFUTATION to RefutedObs in the backfilled Outcome. This cannot be driven through the
// offline content double (it always voices the TRUE arithmetic regardless of the prompt), so it is pinned
// here by feeding a REAL (non-fabricated) refuting observation directly into the grounding spine, then
// asserting the close-path tally reports it — proving episodeGroundedTally reads the INDEPENDENT grounding
// verdict, never a self-judgment.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/flywheel"
	"github.com/berttrycoding/thought-harness/internal/types"
)

func TestFlywheelEpisodeGroundedTallyCountsRefutation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.AllOn()
	feat.Flywheel.Capture = true
	cfg.Features = &feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	mem := flywheel.NewMemSink()
	e.SetFlywheelSink(mem)

	// open an episode (sets episodeGroundBase) and capture one decision so the buffer is non-empty.
	e.startEpisode("verify a claim against reality", true)
	e.flywheel.RecordDecision(e.bus.Tick, e.flywheelState(false), "ACT")

	// feed a REAL refuting observation (ok=false, fabricated=false) AND a real grounding one into the
	// grounding spine — the INDEPENDENT reality verdicts the tally must count. IngestObservation records
	// a Refuted / Grounded experiment; a fabricated one would be rejected (never inflating the tally).
	if !e.grounding.IngestObservation("the build passes with the new flag", false, false, e.bus.Tick) {
		t.Fatal("a real refuting observation must be ingested")
	}
	if !e.grounding.IngestObservation("12 * 31 = 372", true, false, e.bus.Tick) {
		t.Fatal("a real grounding observation must be ingested")
	}

	g, r := e.episodeGroundedTally()
	if r != 1 {
		t.Fatalf("episodeGroundedTally refuted = %d, want 1 (the independent refutation must be counted)", r)
	}
	if g != 1 {
		t.Fatalf("episodeGroundedTally grounded = %d, want 1", g)
	}

	// close on a GIVE_UP (greturn=0, goal not met): the backfilled label must carry the refuted/grounded
	// tally + EpisodeGrounded, independent of the goal verdict.
	e.flywheelCloseEpisode(types.GIVE_UP)
	tuples := mem.All()
	if len(tuples) != 1 {
		t.Fatalf("got %d finalised tuples, want 1", len(tuples))
	}
	out := tuples[0].Outcome
	if out.RefutedObs != 1 || out.GroundedObs != 1 {
		t.Fatalf("backfilled label tally = (g=%d r=%d), want (g=1 r=1)", out.GroundedObs, out.RefutedObs)
	}
	if !out.EpisodeGrounded {
		t.Fatal("EpisodeGrounded must be true (the grounding ledger grew this episode)")
	}
	if out.GoalMet || out.GReturn != 0 || out.StopKind != "GIVE_UP" {
		t.Fatalf("GIVE_UP close label = {met=%v gr=%v kind=%q}, want {false 0 GIVE_UP}", out.GoalMet, out.GReturn, out.StopKind)
	}
}
