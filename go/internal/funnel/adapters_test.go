package funnel

import (
	"strings"
	"testing"
)

// --- per-registry cluster keys --------------------------------------------

func TestClusterKeysAreRegistryDistinct(t *testing.T) {
	op := OperatorClusterKey("ground", "decompose")
	sk := SkillClusterKey("composite", "pipeline")
	wf := WorkflowClusterKey("pipeline")
	kn := KnowledgeClusterKey("fact")

	all := []string{op, sk, wf, kn}
	seen := map[string]bool{}
	for _, k := range all {
		if seen[k] {
			t.Fatalf("cluster keys collide across registries: %q", k)
		}
		seen[k] = true
	}
	// each key is namespaced by its registry kind so two registries never share a bucket.
	if !strings.HasPrefix(op, "operator:") {
		t.Errorf("operator key not namespaced: %q", op)
	}
	if !strings.HasPrefix(sk, "skill:") {
		t.Errorf("skill key not namespaced: %q", sk)
	}
	if !strings.HasPrefix(wf, "workflow:") {
		t.Errorf("workflow key not namespaced: %q", wf)
	}
	if !strings.HasPrefix(kn, "knowledge:") {
		t.Errorf("knowledge key not namespaced: %q", kn)
	}
}

func TestClusterKeyForDispatch(t *testing.T) {
	if ClusterKeyFor(KindOperator, "ground", "decompose") != OperatorClusterKey("ground", "decompose") {
		t.Errorf("ClusterKeyFor(operator) must match OperatorClusterKey")
	}
	if ClusterKeyFor(KindSkill, "composite", "pipeline") != SkillClusterKey("composite", "pipeline") {
		t.Errorf("ClusterKeyFor(skill) must match SkillClusterKey")
	}
	// distinct part tuples never collide (the separator can't appear in normalized identifiers).
	a := ClusterKeyFor(KindOperator, "a", "b")
	b := ClusterKeyFor(KindOperator, "ab", "")
	if a == b {
		t.Fatalf("distinct part tuples collided: %q == %q", a, b)
	}
}

// the per-registry bucketing must keep a NEAR-dup from merging across registry boundaries: two
// candidates with near-identical text but different registry kinds (different ClusterKey buckets) must
// stay distinct — Stage-C near-dup merge is LOCAL to a bucket, so an operator and a skill that happen to
// share a phrase are not folded together. (Note: EXACT-identical text is de-duped globally by the
// cheaper cluster-blind hash cut; this guards the SEMANTIC/near-dup cut, which is per-bucket.)
func TestSieveBucketsByRegistryKey(t *testing.T) {
	batch := []Candidate{
		{ID: "op-x", Kind: "operator", ClusterKey: OperatorClusterKey("ground", "fam"), Text: "rephrase the stated claim into plain words", Provenance: "gen", Links: []string{"x"}, Exercised: true},
		{ID: "sk-x", Kind: "skill", ClusterKey: SkillClusterKey("composite", "pipeline"), Text: "rephrase the given claim using plain words", Provenance: "gen", Links: []string{"x"}, Exercised: true},
	}
	// theta 0.5: the two texts overlap enough to merge IF they shared a bucket — they do not.
	res, err := RegistrySieve{Theta: 0.5}.Sieve(batch)
	if err != nil {
		t.Fatalf("Sieve: %v", err)
	}
	if len(res.Admitted) != 2 {
		t.Fatalf("near-dup candidates in DIFFERENT registry buckets must both survive (per-bucket near-dup), got %d: %v", len(res.Admitted), res.AdmittedIDs())
	}
	// proof the theta WOULD merge them in the SAME bucket — same kind, same cluster key.
	same := []Candidate{
		{ID: "a", Kind: "operator", ClusterKey: OperatorClusterKey("ground", "fam"), Text: "rephrase the stated claim into plain words", Provenance: "gen", Links: []string{"x"}, Exercised: true},
		{ID: "b", Kind: "operator", ClusterKey: OperatorClusterKey("ground", "fam"), Text: "rephrase the given claim using plain words", Provenance: "gen", Links: []string{"x"}, Exercised: true},
	}
	sameRes, _ := RegistrySieve{Theta: 0.5}.Sieve(same)
	if len(sameRes.Admitted) != 1 {
		t.Fatalf("same-bucket near-dups MUST merge to one representative, got %d (the theta is not the cause of the cross-bucket survival)", len(sameRes.Admitted))
	}
}

// --- RegistrySieve: Tier-0 only (no Tier-1, no Tier-2) is the additive default ---

func TestSieveTier0OnlyByDefault(t *testing.T) {
	filler := Candidate{ID: "filler", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "declared never used"} // no provenance/links/exercised
	// "aaa-good" is ID-first within the bucket, so it is the kept representative; "dup" folds into it.
	good := Candidate{ID: "aaa-good", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "an induction over the cases", Provenance: "gen", Links: []string{"y"}, Exercised: true}
	dup := Candidate{ID: "zzz-dup", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "An  Induction  over the CASES", Provenance: "gen", Links: []string{"y"}, Exercised: true} // exact dup of good (normalized)

	res, err := RegistrySieve{Kind: KindSkill}.Sieve([]Candidate{filler, good, dup})
	if err != nil {
		t.Fatalf("Sieve: %v", err)
	}
	if res.Tier1Ran {
		t.Fatalf("Tier-1 must NOT run without canonical+rankers (additive default)")
	}
	if !res.Tier1Pass {
		t.Fatalf("a skipped Tier-1 must report passed (not a regression)")
	}
	if res.LiftRun {
		t.Fatalf("Tier-2 must NOT run without an injected Tier2 runner (opt-in)")
	}
	admitted := map[string]bool{}
	for _, c := range res.Admitted {
		admitted[c.ID] = true
	}
	if !admitted["aaa-good"] {
		t.Fatalf("the good candidate must be admitted; admitted=%v", res.AdmittedIDs())
	}
	if admitted["filler"] {
		t.Fatalf("the filler candidate must be rejected by anti-filler")
	}
	if admitted["zzz-dup"] {
		t.Fatalf("the exact-dup candidate must be rejected by Stage-C")
	}
	if r := res.Rejected["filler"]; !strings.Contains(r, "anti-filler") {
		t.Fatalf("filler rejection must cite anti-filler, got %q", r)
	}
	if r := res.Rejected["zzz-dup"]; r != "near-dup-of:aaa-good" {
		t.Fatalf("dup must be rejected as near-dup-of:aaa-good, got %q", r)
	}
}

// --- RegistrySieve: Tier-1 wired (retrieval integrity gates before Tier-2) ---

func TestSieveTier1RegressionShortCircuitsTier2(t *testing.T) {
	good := Candidate{ID: "new", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "a fresh distinct move", Provenance: "gen", Links: []string{"z"}, Exercised: true}
	canonical := []Query{{Text: "split a goal", ExpectedID: "decompose"}}
	baseline := func(q string) []string { return []string{"decompose", "compare"} }
	// the shadow displaces the correct rank-1 -> Tier-1 regression.
	shadow := func(q string) []string { return []string{"new", "decompose"} }

	// a Tier-2 runner whose bench would PANIC if called — proves the short-circuit (Tier-2 must NOT run).
	panicBench := LiftBenchFunc(func(stateDir string) (ArmStats, error) {
		t.Fatalf("Tier-2 bench was called despite a Tier-1 regression (no short-circuit)")
		return ArmStats{}, nil
	})

	res, err := RegistrySieve{
		Kind:          KindSkill,
		Canonical:     canonical,
		Baseline:      baseline,
		Shadow:        shadow,
		Tier2:         NewTier2Runner(panicBench),
		BatchStateDir: "batch-dir",
	}.Sieve([]Candidate{good})
	if err != nil {
		t.Fatalf("Sieve: %v", err)
	}
	if !res.Tier1Ran {
		t.Fatalf("Tier-1 must run when canonical+rankers are supplied")
	}
	if res.Tier1Pass {
		t.Fatalf("Tier-1 must FAIL on a rank-1 regression")
	}
	if len(res.Tier1Regressions) != 1 {
		t.Fatalf("expected one Tier-1 regression, got %d", len(res.Tier1Regressions))
	}
	if res.LiftRun {
		t.Fatalf("Tier-2 must be short-circuited by a Tier-1 regression")
	}
}

// the full pipeline: Tier-0 admits, Tier-1 passes, Tier-2 runs the lift and keeps a cheaper batch.
func TestSieveFullPipelineKeepsCheaperBatch(t *testing.T) {
	good := Candidate{ID: "minted-skill", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "a converged program for the family", Provenance: "convert", Links: []string{"op:decompose"}, Exercised: true}
	canonical := []Query{{Text: "split a goal", ExpectedID: "decompose"}}
	baseline := func(q string) []string { return []string{"decompose", "compare"} }
	shadow := func(q string) []string { return []string{"decompose", "minted-skill", "compare"} } // no rank-1 displacement

	pass := []bool{true, true, true, true, true, true}
	bench := fakeBench{
		baseline:  ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)},
		withBatch: ArmStats{PerItem: pass, CompletionPerItem: repeatInt(30, 6)}, // cheaper at flat capability
	}

	res, err := RegistrySieve{
		Kind:          KindSkill,
		Canonical:     canonical,
		Baseline:      baseline,
		Shadow:        shadow,
		Tier2:         NewTier2Runner(bench),
		BatchStateDir: "staged-batch",
	}.Sieve([]Candidate{good})
	if err != nil {
		t.Fatalf("Sieve: %v", err)
	}
	if !res.Tier1Ran || !res.Tier1Pass {
		t.Fatalf("Tier-1 must run and pass; ran=%v pass=%v", res.Tier1Ran, res.Tier1Pass)
	}
	if !res.LiftRun {
		t.Fatalf("Tier-2 must run when Tier-1 passes and a runner is wired")
	}
	if res.Lift.Verdict.Decision != LiftKeep {
		t.Fatalf("the cheaper batch must be KEPT, got %s (%s)", res.Lift.Verdict.Decision, res.Lift.Verdict.Reason)
	}
}

// an empty admitted set short-circuits Tier-2 (nothing to lift-test).
func TestSieveEmptyAdmittedShortCircuits(t *testing.T) {
	filler := Candidate{ID: "f", Kind: "skill", ClusterKey: SkillClusterKey("composite", "p"), Text: "x"} // fails anti-filler
	panicBench := LiftBenchFunc(func(string) (ArmStats, error) {
		t.Fatalf("Tier-2 bench called with no admitted candidates")
		return ArmStats{}, nil
	})
	res, err := RegistrySieve{Kind: KindSkill, Tier2: NewTier2Runner(panicBench)}.Sieve([]Candidate{filler})
	if err != nil {
		t.Fatalf("Sieve: %v", err)
	}
	if len(res.Admitted) != 0 {
		t.Fatalf("expected nothing admitted, got %d", len(res.Admitted))
	}
	if res.LiftRun {
		t.Fatalf("Tier-2 must not run on an empty admitted set")
	}
}
