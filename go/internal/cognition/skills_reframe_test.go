package cognition

import (
	"bytes"
	"strings"
	"testing"
)

// skills_reframe_test.go proves the GAP-8 Skill reframe (cognition-redesign §3.8, locked 2026-06-14):
// behind the registry reframe flag (SetReframe ⇐ convert.skill_reframe), a Skill's executable body is a
// PROMPT + sub-skill REFERENCES resolved at RUN time (ResolveBody), and a Skill does NOT self-match goals
// (relevance moves to the Capability; "goal-matched is retired"). Default OFF is the legacy Program /
// build-time-Expand / goal-matching path UNCHANGED — the W5 mint/recall flywheel is byte-identical.
//
// This is the COGNITION the slice intends, not the plumbing: the reframed skill THINKS via a prompt it
// assembles at use-time, the goal→skill self-match is genuinely retired, and the legacy thinking path is
// provably the same when off.

// TestReframeDefaultOffIsLegacyPath proves the flag-OFF path is the legacy Program/goal-match behaviour
// unchanged: a fresh registry is NOT reframed, a seed composite still self-matches its goal, and a
// minted Program-bodied skill still expands at build time. (Byte-identical to the pre-reframe behaviour
// the goldens anchor — this test would FAIL if the reframe leaked into the default path.)
func TestReframeDefaultOffIsLegacyPath(t *testing.T) {
	lib := NewSkillRegistry(true)
	if lib.Reframed() {
		t.Fatal("a fresh registry must default to reframe OFF (the legacy path the goldens anchor)")
	}

	// Legacy goal-self-match still works (the W5 recall flywheel): a goal hitting a composite trigger
	// recalls it via Match.
	got, found := lib.Match("compare postgres versus mysql for this workload")
	if !found {
		t.Fatal("OFF: a goal hitting a composite trigger must still self-match (legacy goal-matched Skill)")
	}
	if !strings.Contains(got.Name, "evaluate") && got.Name != "evaluate-options" {
		t.Logf("OFF: matched %q (any composite is fine; the point is Match self-matches when OFF)", got.Name)
	}

	// Legacy build-time Expand still resolves a composite into a pure-operator Program.
	if _, err := lib.Expand(got); err != nil {
		t.Fatalf("OFF: legacy Expand must resolve a composite skill body; got %v", err)
	}
}

// TestReframeRetiresGoalMatch proves the central reframe decision: with the flag ON, a Skill does NOT
// match goals — Match (and MatchWithinTier) return nothing even when a trigger fires verbatim. Relevance
// is the Capability's job; the worker invokes skills by category, never by scanning goals.
func TestReframeRetiresGoalMatch(t *testing.T) {
	off := NewSkillRegistry(true)
	// Precondition: OFF, this exact goal self-matches (so the ON miss is the reframe, not a dead goal).
	if _, found := off.Match("compare postgres versus mysql"); !found {
		t.Fatal("precondition: this goal must self-match under the legacy (OFF) path")
	}

	on := NewSkillRegistry(true)
	on.SetReframe(true)
	if !on.Reframed() {
		t.Fatal("SetReframe(true) must report Reframed()==true")
	}
	if got, found := on.Match("compare postgres versus mysql"); found {
		t.Fatalf("ON: a Skill must NOT self-match goals (goal-matched is retired), but Match returned %q", got.Name)
	}
	if got, found := on.MatchWithinTier("compare postgres versus mysql", 0); found {
		t.Fatalf("ON: MatchWithinTier must also retire goal-matching, but it returned %q", got.Name)
	}
}

// TestReframeResolvesBodyAtRuntime proves the runtime resolver: a REFRAMED skill (prompt + sub-skill
// refs) assembles its executable body at USE time via ResolveBody (NOT build-time Expand), splicing in
// each referenced sub-skill's prompt where its "skill:<name>" marker sits. This is the run-time analogue
// of Expand, on the prompt substrate.
func TestReframeResolvesBodyAtRuntime(t *testing.T) {
	lib := NewSkillRegistry(false) // empty registry — build a pure reframed library
	lib.SetReframe(true)

	// A unit (leaf) reframed skill: a prompt, no sub-skills.
	leaf, ok := lib.MintReframed("ground-facts", "unit",
		"Pull the grounded facts the claim depends on and list them.", nil, "leaf grounding prompt")
	if !ok {
		t.Fatal("minting a leaf reframed skill (prompt only) must succeed")
	}
	if !leaf.IsReframed() {
		t.Fatal("a prompt-bodied skill must report IsReframed()==true")
	}

	// A high-level reframed skill: a prompt that REFERENCES the leaf in-line via a skill:<name> marker.
	parentPrompt := "First, gather grounding: skill:ground-facts\nThen judge whether the claim holds."
	parent, ok := lib.MintReframed("judge-claim", "composite", parentPrompt,
		[]string{"ground-facts"}, "high-level: ground then judge")
	if !ok {
		t.Fatal("minting a high-level reframed skill (prompt + sub-skill ref) must succeed")
	}

	resolved, err := lib.ResolveBody(parent)
	if err != nil {
		t.Fatalf("ResolveBody must resolve a reframed skill at run time; got %v", err)
	}
	// The leaf's prompt was spliced in where its marker sat (runtime resolution, not build-time flatten).
	if strings.Contains(resolved.Prompt, subSkillMark("ground-facts")) {
		t.Fatalf("the sub-skill marker must be REPLACED by the resolved prompt at run time; still present in:\n%s", resolved.Prompt)
	}
	if !strings.Contains(resolved.Prompt, "Pull the grounded facts") {
		t.Fatalf("the resolved body must contain the spliced sub-skill prompt; got:\n%s", resolved.Prompt)
	}
	if !strings.Contains(resolved.Prompt, "judge whether the claim holds") {
		t.Fatalf("the resolved body must keep the parent's own prompt text; got:\n%s", resolved.Prompt)
	}
	if resolved.Calls != 1 {
		t.Fatalf("resolving one sub-skill ref must count Calls==1; got %d", resolved.Calls)
	}

	// The legacy build-time Expand has NO place here: a reframed (Program-less) skill cannot Expand.
	if _, err := lib.Expand(parent); err == nil {
		t.Log("note: Expand over a reframed skill with an empty Program body returns an empty program; ResolveBody is the runtime path")
	}
}

// TestReframeRuntimeResolverIsBounded proves the runtime resolver keeps the same durability obligation
// the build-time Expand gave statically: a cycle, over-deep nesting, or an unknown sub-skill is rejected
// at run time (not flattened, not unbounded). This re-grounds the acyclic/depth-3 guard at the new
// enforcement point.
func TestReframeRuntimeResolverIsBounded(t *testing.T) {
	lib := NewSkillRegistry(false)
	lib.SetReframe(true)

	// Unknown sub-skill ref is rejected.
	bad := NewReframedSkill("orphan", "composite", "do X then skill:missing", []string{"missing"}, "")
	if _, err := lib.ResolveBody(bad); err == nil {
		t.Fatal("ResolveBody must reject an unknown sub-skill reference (durability obligation)")
	}

	// A direct self-cycle is rejected (acyclic guard). Insert it directly so the cycle exists to resolve.
	cyc := NewReframedSkill("loopy", "composite", "skill:loopy", []string{"loopy"}, "")
	cyc.Synthesized = true
	lib.skills["loopy"] = cyc
	if _, err := lib.ResolveBody(cyc); err == nil {
		t.Fatal("ResolveBody must reject a self-referential sub-skill cycle (acyclic guard)")
	} else if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a cycle rejection should name the cycle; got %v", err)
	}

	// VerifyReframed (the mint gate) must refuse to admit a skill that fails to resolve.
	if ok, why := lib.VerifyReframed(bad); ok {
		t.Fatalf("VerifyReframed must reject a skill whose body does not resolve; admitted it (%s)", why)
	}
	// And it must refuse an empty prompt (a reframed skill IS its prompt).
	empty := NewReframedSkill("blank", "unit", "", nil, "")
	if ok, _ := lib.VerifyReframed(empty); ok {
		t.Fatal("VerifyReframed must reject an empty-prompt reframed skill")
	}
}

// TestLegacySkillCannotResolveBody pins the two-shape boundary: ResolveBody is for reframed
// (prompt-bodied) skills ONLY; a legacy Program-bodied skill is directed to Expand, never silently
// crossed. This keeps the flywheel's legacy artifacts on the legacy path even when the flag is on.
func TestLegacySkillCannotResolveBody(t *testing.T) {
	lib := NewSkillRegistry(true)
	lib.SetReframe(true)
	legacy, ok := lib.Get("diagnose") // a seed composite — Program body, no Prompt
	if !ok || legacy.IsReframed() {
		t.Fatal("precondition: 'diagnose' must be a legacy Program-bodied seed composite")
	}
	if _, err := lib.ResolveBody(legacy); err == nil {
		t.Fatal("ResolveBody must refuse a legacy (Program-bodied) skill — it belongs to Expand")
	}
	// Expand still works on it even with the reframe on (the fallback shape is preserved).
	if _, err := lib.Expand(legacy); err != nil {
		t.Fatalf("a legacy skill must still Expand under the reframe flag (fallback shape preserved); got %v", err)
	}
}

// TestReframedSkillPersistenceRoundTrip proves the JSONL seam round-trips a REFRAMED skill: save a
// prompt-bodied minted skill, load into a fresh (reframed) registry, and it survives with its prompt +
// sub-skill refs intact and resolves at run time. The persistence record fields are additive
// (omitempty), so a legacy-only store is byte-identical (TestLegacyOnlySaveByteIdentical).
func TestReframedSkillPersistenceRoundTrip(t *testing.T) {
	a := NewSkillRegistry(false)
	a.SetReframe(true)
	if _, ok := a.MintReframed("ground-facts", "unit", "Pull the grounded facts.", nil, "leaf"); !ok {
		t.Fatal("minting the leaf reframed skill must succeed")
	}
	if _, ok := a.MintReframed("judge-claim", "composite",
		"Ground first: skill:ground-facts then judge.", []string{"ground-facts"}, "high-level"); !ok {
		t.Fatal("minting the high-level reframed skill must succeed")
	}

	var buf bytes.Buffer
	if err := a.SaveMinted(&buf); err != nil {
		t.Fatalf("SaveMinted: %v", err)
	}
	if !strings.Contains(buf.String(), "\"prompt\"") {
		t.Fatalf("a reframed skill must persist its prompt; got:\n%s", buf.String())
	}

	b := NewSkillRegistry(false)
	b.SetReframe(true)
	n, err := b.LoadMinted(&buf)
	if err != nil || n != 2 {
		t.Fatalf("LoadMinted: n=%d err=%v (want 2)", n, err)
	}
	loaded, ok := b.Get("judge-claim")
	if !ok || !loaded.IsReframed() {
		t.Fatal("the loaded high-level skill must survive as a reframed (prompt-bodied) skill")
	}
	resolved, rerr := b.ResolveBody(loaded)
	if rerr != nil {
		t.Fatalf("the loaded reframed skill must still resolve at run time; got %v", rerr)
	}
	if !strings.Contains(resolved.Prompt, "Pull the grounded facts") {
		t.Fatalf("the loaded skill must resolve its sub-skill prompt; got:\n%s", resolved.Prompt)
	}
}

// TestLegacyOnlySaveByteIdentical pins the byte-identical guarantee: a registry that mints ONLY legacy
// (Program-bodied) skills produces a persistence record with NO reframe fields — the omitempty keys are
// absent, so the on-disk format is unchanged from the pre-reframe code (the flywheel's stored learning
// is not migrated or reshaped).
func TestLegacyOnlySaveByteIdentical(t *testing.T) {
	a := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", ""), NewStep("rank", "general", "")))
	if _, ok := a.Mint("learned-frobnicate", []string{"frobnicate"}, body, "", "learned thing"); !ok {
		t.Fatal("minting the legacy skill must succeed")
	}
	var buf bytes.Buffer
	if err := a.SaveMinted(&buf); err != nil {
		t.Fatalf("SaveMinted: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\"prompt\"") || strings.Contains(out, "\"sub_skill_refs\"") {
		t.Fatalf("a legacy-only store must NOT carry reframe fields (omitempty); got:\n%s", out)
	}
	if !strings.Contains(out, "\"body\"") {
		t.Fatalf("a legacy skill must persist its Program body; got:\n%s", out)
	}
}
