package cognition

import "testing"

// TestEverySeedOperatorHasAMove is the M2 move-coverage gate (representation-space-rebuild §2.3): EVERY
// seed operator carries a Move tag (the load-bearing structural change), and the functional basis is
// covered — GROUND×3+, LIFT×2+, REFRAME×2+, assess×2+ — so the synthesiser always has an operator for
// each directed step on the abstraction ladder + the judge lane.
func TestEverySeedOperatorHasAMove(t *testing.T) {
	r := NewOperatorRegistry()
	counts := map[Move]int{}
	for _, n := range r.Names() {
		m, ok := r.MoveOf(n)
		if !ok {
			t.Fatalf("operator %q not found", n)
		}
		if m == "" {
			t.Errorf("operator %q has no Move tag (every seed op must declare its direction)", n)
		}
		if m != MoveGround && m != MoveLift && m != MoveReframe && m != MoveTranscode && m != MoveAssess {
			t.Errorf("operator %q has an invalid Move %q", n, m)
		}
		counts[m]++
	}
	if counts[MoveGround] < 3 {
		t.Errorf("GROUND coverage too low: %d (want >=3)", counts[MoveGround])
	}
	if counts[MoveLift] < 2 {
		t.Errorf("LIFT coverage too low: %d (want >=2)", counts[MoveLift])
	}
	if counts[MoveReframe] < 2 {
		t.Errorf("REFRAME coverage too low: %d (want >=2)", counts[MoveReframe])
	}
	if counts[MoveAssess] < 2 {
		t.Errorf("assess coverage too low: %d (want >=2)", counts[MoveAssess])
	}
	t.Logf("move coverage: GROUND=%d LIFT=%d REFRAME=%d TRANSCODE=%d assess=%d (total ops=%d)",
		counts[MoveGround], counts[MoveLift], counts[MoveReframe], counts[MoveTranscode],
		counts[MoveAssess], len(r.Names()))
}

// TestMintedOperatorInheritsMove confirms a runtime-minted op declares (or inherits-from-family) a Move,
// so a synthesised operator is never born directionless (it gets a home for its Gate-prior lane).
func TestMintedOperatorInheritsMove(t *testing.T) {
	r := NewOperatorRegistry()
	// legacy Mint (no declared move) inherits the family's canonical move.
	if _, ok := r.Mint("frobnicate", "transformative", "do the frobnicate transform"); !ok {
		t.Fatal("mint should succeed")
	}
	if m, _ := r.MoveOf("frobnicate"); m != MoveLift {
		t.Fatalf("transformative mint should inherit LIFT, got %q", m)
	}
	// MintWithMove declares its direction explicitly.
	if _, ok := r.MintWithMove("zorch", "synthesized", "a bespoke ground move", MoveGround); !ok {
		t.Fatal("mint-with-move should succeed")
	}
	if m, _ := r.MoveOf("zorch"); m != MoveGround {
		t.Fatalf("declared-move mint should be GROUND, got %q", m)
	}
}
