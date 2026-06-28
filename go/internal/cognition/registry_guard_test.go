package cognition

import (
	"strconv"
	"sync"
	"testing"
)

// TestOperatorSeedCollisionRejected: a seed operator is a frozen invariant — Verify/Mint must refuse to
// redefine it, so a runtime mint can never silently overwrite a seeded capability.
func TestOperatorSeedCollisionRejected(t *testing.T) {
	r := NewOperatorRegistry()
	if ok, reason := r.Verify("decompose", "transformative", "redefine the seed operator"); ok {
		t.Fatalf("redefining seed operator 'decompose' must be rejected; got ok (reason=%q)", reason)
	}
	if _, ok := r.Mint("decompose", "transformative", "redefine the seed operator"); ok {
		t.Fatal("minting over a seed operator must fail")
	}
	// a fresh name still mints fine.
	if _, ok := r.Mint("frobnicate", "transformative", "do the frobnicate transform"); !ok {
		t.Fatal("a non-colliding mint should succeed")
	}
}

// TestSkillSeedCollisionRejected: the same frozen-seed invariant for the SkillRegistry.
func TestSkillSeedCollisionRejected(t *testing.T) {
	lib := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", "")))
	if _, ok := lib.Mint("diagnose", []string{"x"}, body, "composite", ""); ok {
		t.Fatal("minting over the seed skill 'diagnose' must be rejected")
	}
	if ok, _ := lib.Verify("diagnose", body); ok {
		t.Fatal("Verify must reject redefining a seed skill")
	}
}

// TestVerifyProgramOrderedIssues: VerifyProgram reports issues deterministically — the same program
// always yields the same issue set/order, and specific malformations produce their specific message.
func TestVerifyProgramOrderedIssues(t *testing.T) {
	cat := NewOperatorRegistry()

	// empty program.
	if ok, issues := VerifyProgram(Program{Root: NewSeq()}, cat); ok || len(issues) == 0 {
		t.Fatalf("an empty program must fail verification; ok=%v issues=%v", ok, issues)
	}

	// unknown operator.
	ok, issues := VerifyProgram(Program{Root: NewSeq(NewStep("nonsuch-op", "general", ""))}, cat)
	if ok {
		t.Fatal("a program with an unknown operator must fail")
	}
	if !containsSubstr(issues, "unknown operator 'nonsuch-op'") {
		t.Fatalf("expected an unknown-operator issue; got %v", issues)
	}

	// determinism: the SAME malformed program yields the SAME issues across runs.
	bad := Program{Root: NewSeq(NewStep("nonsuch-a", "g", ""), NewStep("nonsuch-b", "g", ""))}
	_, first := VerifyProgram(bad, cat)
	for i := 0; i < 20; i++ {
		if _, again := VerifyProgram(bad, cat); !equalStrings(first, again) {
			t.Fatalf("VerifyProgram issue order is non-deterministic: %v vs %v", first, again)
		}
	}
}

// TestConcurrentOperatorMintNoRace: minting distinct operators from many goroutines is safe under the
// registry's mutex — every mint lands, with no duplicates in Minted() and no data race (run -race).
// Also mints the SAME name from many goroutines and asserts it appears exactly once (dedup under race).
func TestConcurrentOperatorMintNoRace(t *testing.T) {
	r := NewOperatorRegistry()
	const n = 64
	var wg sync.WaitGroup

	// distinct names.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Mint("op"+strconv.Itoa(i), "synthesized", "a synthesised operator number "+strconv.Itoa(i))
		}(i)
	}
	// the SAME name, concurrently.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Mint("shared-op", "synthesized", "the shared synthesised operator")
		}()
	}
	wg.Wait()

	minted := r.Minted()
	seen := map[string]int{}
	for _, m := range minted {
		seen[m]++
	}
	for name, c := range seen {
		if c != 1 {
			t.Fatalf("operator %q appears %d times in Minted() — concurrent dedup failed", name, c)
		}
	}
	if seen["shared-op"] != 1 {
		t.Fatalf("the concurrently-minted shared-op must appear exactly once; got %d", seen["shared-op"])
	}
	for i := 0; i < n; i++ {
		if !r.Has("op" + strconv.Itoa(i)) {
			t.Fatalf("distinct concurrent mint op%d was lost", i)
		}
	}
}

func containsSubstr(xs []string, sub string) bool {
	for _, x := range xs {
		if len(x) >= len(sub) && indexOf(x, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
