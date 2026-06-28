package cognition

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// TestMatchSemanticRecallParaphrase is the P1.3 SkillRegistry half (model-gated): a goal that
// PARAPHRASES a composite skill's purpose — with none of its trigger words — recalls it once an
// embedder is wired, where the lexical-only Match finds nothing. Skips when no embedder is reachable;
// the nil-embedder (offline) path is the unchanged lexical Match covered by the tests above.
func TestMatchSemanticRecallParaphrase(t *testing.T) {
	emb := retrieval.ReachableEmbedder()
	if emb == nil {
		t.Skip("no embeddings endpoint reachable — semantic Match recall is model-gated")
	}
	goal := "figure out the root reason the service keeps crashing" // paraphrases diagnose/debug-system

	lexical := NewSkillRegistry(true)
	if got, ok := lexical.Match(goal); ok {
		t.Fatalf("lexical Match should miss this paraphrase, but matched %q", got.Name)
	}

	semantic := NewSkillRegistry(true)
	semantic.SetEmbedder(emb)
	got, ok := semantic.Match(goal)
	if !ok {
		t.Fatal("semantic Match should recall a fault-finding skill the lexical match missed")
	}
	if got.Name != "diagnose" && got.Name != "debug-system" {
		t.Fatalf("semantic Match recalled %q, want diagnose or debug-system", got.Name)
	}
	t.Logf("semantic Match recalled %q for a paraphrased goal lexical missed", got.Name)
}

// TestMintedSkillIsRecalled is the convertibility-loop gate (tracker P0.2): a skill minted at runtime
// must be RECALLABLE by Match. The engine mints recurring programs with an EMPTY tier
// (engine.skillMinter.Mint passes tier=""), so the old composite-only scan (Match over r.Composite(),
// tier=="composite") could never return them — the mint->recall loop was structurally dead. This proves
// the full round trip: mint with tier="" (the real engine path), then Match a goal carrying the trigger
// returns exactly that minted skill.
func TestMintedSkillIsRecalled(t *testing.T) {
	lib := NewSkillRegistry(true)

	// A valid recurring program: pure operators (real catalog ops), so it passes Verify + Expand.
	body := seedProgramSynth(NewSeq(
		NewStep("measure", "general", ""),
		NewStep("rank", "general", ""),
	))
	// Mint exactly as the engine does — tier="" (NOT "composite"). This is the case the bug missed.
	minted, ok := lib.Mint("learned-frobnicate", []string{"frobnicate"}, body, "", "learned from 3 repeats")
	if !ok {
		t.Fatal("minting a valid recurring program must succeed")
	}
	if minted.Tier == "composite" {
		t.Fatal("precondition: this test exercises the empty-tier mint path; got tier=composite")
	}

	// Document the bug's mechanism: an empty-tier minted skill is NOT in Composite(), so the old
	// composite-only Match would have missed it entirely.
	for _, c := range lib.Composite() {
		if c.Name == "learned-frobnicate" {
			t.Fatal("setup invalid: minted skill must not be a composite (it would not exercise the fix)")
		}
	}

	// The fix: Match routes on matchability (the trigger fires), not tier — so the minted skill recalls.
	got, found := lib.Match("frobnicate the pipeline")
	if !found {
		t.Fatal("a minted skill whose trigger fires must be recalled by Match (the convertibility loop)")
	}
	if got.Name != "learned-frobnicate" {
		t.Fatalf("Match returned %q, want the minted skill 'learned-frobnicate'", got.Name)
	}
}

// TestSeedUnitStaysBuildingBlock pins the deliberate scope of the fix: a seed UNIT skill (e.g.
// "research", Synthesized=false, tier="unit") is an expansion building-block, NOT a directly-matched
// capability — so a goal hitting only a unit trigger does NOT route to that unit. This keeps the seed
// routing the scenario goldens anchor unchanged; the fix opens recall for LEARNED skills only. (A
// learned skill minted with the SAME trigger word would be recalled — that is TestMintedSkillIsRecalled.)
func TestSeedUnitStaysBuildingBlock(t *testing.T) {
	lib := NewSkillRegistry(true)
	research, ok := lib.Get("research")
	if !ok || research.Tier != "unit" || research.Synthesized {
		t.Fatalf("precondition: 'research' must be a non-synthesized seed unit; got ok=%v tier=%q synth=%v",
			ok, research.Tier, research.Synthesized)
	}
	// "research the auth flow" hits only the 'research' unit trigger and no composite — so Match misses,
	// exactly as before the fix (units remain building-blocks, not directly-matched capabilities).
	if got, found := lib.Match("research the auth flow"); found {
		t.Fatalf("a seed unit must not be directly matched, but Match returned %q", got.Name)
	}
}
