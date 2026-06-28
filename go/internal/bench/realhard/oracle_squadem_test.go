package realhard

import "testing"

// oracle_squadem_test.go — the realhard oracle's SQuAD/HotpotQA EM normalizer (the fair-metric fix). A
// Task with Normalizer "squad-em" (or "em") scores on the OFFICIAL exact-match metric, so a correct
// answer differing only by an article / casing / trailing punctuation still counts.

func emTask(expected string) Task {
	return Task{Oracle: OracleExact, Normalizer: "squad-em", Expected: expected}
}

func TestScoreSquadEMSolvesOnArticleAndPunctuationVariation(t *testing.T) {
	cases := []struct{ expected, answer string }{
		{"United States", "The United States"},
		{"yes", "Yes."},
		{"Boeing 747", "a Boeing 747"},
		{"White House", "the   White House "},
	}
	for _, c := range cases {
		v := Score(emTask(c.expected), c.answer)
		if !v.Solved {
			t.Fatalf("squad-em: answer %q should solve expected %q (official EM), got %q", c.answer, c.expected, v.Reason)
		}
	}
}

func TestScoreSquadEMFailsGenuineMismatch(t *testing.T) {
	v := Score(emTask("United States"), "United Kingdom")
	if v.Solved {
		t.Fatalf("squad-em must FAIL a genuine mismatch (United States vs United Kingdom); got solved=%v reason=%q", v.Solved, v.Reason)
	}
}

// TestScoreSquadEMAliasEM proves the "em" alias scores identically to "squad-em".
func TestScoreSquadEMAliasEM(t *testing.T) {
	answer, expected := "The United States.", "United States"
	em := Task{Oracle: OracleExact, Normalizer: "em", Expected: expected}
	if v := Score(em, answer); !v.Solved {
		t.Fatalf("\"em\" alias must score like squad-em; answer %q expected %q got %q", answer, expected, v.Reason)
	}
}
