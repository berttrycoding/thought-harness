package tiera

import "testing"

// normalize_squadem_test.go — the OFFICIAL SQuAD/HotpotQA EM normalizer (the fair-metric fix). The loader
// must ACCEPT "squad-em" (and the alias "em") as a valid normalizer name, and the normalization must match
// the official reference (lowercase, strip articles, strip punctuation, collapse whitespace).

func TestNormalizeSquadEMEquivalence(t *testing.T) {
	// pairs that the OFFICIAL EM metric scores EQUAL but a plain "lower" would not.
	equal := []struct{ a, b string }{
		{"The United States", "United States"},
		{"Yes.", "yes"},
		{"a Boeing 747", "Boeing 747"},
		{"  the   White   House ", "White House"},
		{"The U.S.A.", "usa"},
		{"An apple", "apple"},
		{"Paris, France", "paris france"},
	}
	for _, name := range []string{NormSquadEM, NormEM} {
		for _, p := range equal {
			na := Normalize(name, p.a)
			nb := Normalize(name, p.b)
			if na != nb {
				t.Fatalf("Normalize(%q): %q -> %q vs %q -> %q, want equal", name, p.a, na, p.b, nb)
			}
		}
	}
}

func TestNormalizeSquadEMDistinguishesGenuineDifference(t *testing.T) {
	// genuinely-different answers must NOT collapse (the metric still discriminates).
	na := Normalize(NormSquadEM, "the United States")
	nb := Normalize(NormSquadEM, "the United Kingdom")
	if na == nb {
		t.Fatalf("squad-em collapsed two distinct answers: %q == %q", na, nb)
	}
}

// TestSquadEMNotPlainLower documents that squad-em is STRICTLY MORE LENIENT than the default lower (it
// recovers correct answers that "lower" would mark wrong) — the whole point of the fair-metric fix.
func TestSquadEMNotPlainLower(t *testing.T) {
	answer, expected := "The United States.", "United States"
	if Normalize(NormPassthrough, answer) == Normalize(NormPassthrough, expected) {
		t.Fatal("precondition: plain lower should NOT equate these (it undercounts)")
	}
	if Normalize(NormSquadEM, answer) != Normalize(NormSquadEM, expected) {
		t.Fatalf("squad-em must equate %q and %q (the official EM metric does)", answer, expected)
	}
}
