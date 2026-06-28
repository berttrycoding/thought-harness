package cognition

// offer_semantic_test.go — W3-S3 Part 1: the at-scale catalog retrieval EMBEDDER seam (synthesiser
// curation safe at 10x catalog). These tests prove (1) the RRF-fusion PLUMBING with a deterministic
// test-double embedder retains a high-cosine zero-lexical op into top-K, and (2) the at-scale SEMANTIC
// recovery with a REAL reachable embedder (model-gated; skips offline). The byte-identical default and
// every selection invariant are pinned by the W3-S1 contract tests (offer_test.go +
// catalog_offer_wire_test.go), which still pass unchanged — the embedder is additive (SetEmbedder default
// nil). See operators.go Offer/offerLexical/offerSemantic/SetEmbedder/RetrieverMode.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// stubEmbedder is a DETERMINISTIC test-double embedder: each "topic axis" owns a SET of synonym keywords;
// any text containing a synonym lights that axis. So two texts on the same topic via DIFFERENT words embed
// to the SAME vector (cosine 1.0), proving the semantic channel bridges meaning where lexical overlap is
// zero; texts on different topics embed orthogonally (cosine 0.0). It is a PLUMBING double — it proves the
// RRF wiring carries a high-cosine op into top-K, NOT that a real embedder ranks correctly (that is the
// model-gated test below). One method (the Embedder interface).
type stubEmbedder struct {
	topics [][]string // topics[i] is the synonym-set that lights axis i
}

func newStubEmbedder(synonyms ...[]string) *stubEmbedder { return &stubEmbedder{topics: synonyms} }

func (s *stubEmbedder) Embed(text string) ([]float32, error) {
	v := make([]float32, len(s.topics)+1) // +1 catch-all axis for text matching no topic
	lt := strings.ToLower(text)
	lit := false
	for i, syns := range s.topics {
		for _, kw := range syns {
			if strings.Contains(lt, kw) {
				v[i] = 1
				lit = true
				break
			}
		}
	}
	if !lit {
		v[len(s.topics)] = 1 // unrecognised text is orthogonal to every topic
	}
	return v, nil
}

// TestRetrieverModeReflectsChannel pins the observability contract: RetrieverMode reports "off" when the
// cap does not engage, and once it engages "lexical" with no embedder vs "semantic" with one wired. This
// is the value surfaced on the catalog_offer event's `mode` field — what makes a silent lexical fallback
// on a live claude run OBSERVABLE.
func TestRetrieverModeReflectsChannel(t *testing.T) {
	r := NewOperatorRegistry()
	if m := r.RetrieverMode(false); m != "off" {
		t.Errorf("RetrieverMode(capEngages=false) = %q, want off", m)
	}
	if m := r.RetrieverMode(true); m != "lexical" {
		t.Errorf("RetrieverMode(capEngages=true, no embedder) = %q, want lexical", m)
	}
	r.SetEmbedder(newStubEmbedder([]string{"x"}))
	if m := r.RetrieverMode(true); m != "semantic" {
		t.Errorf("RetrieverMode(capEngages=true, embedder wired) = %q, want semantic", m)
	}
	if m := r.RetrieverMode(false); m != "off" {
		t.Errorf("RetrieverMode(capEngages=false) must be off even with an embedder, got %q", m)
	}
}

// TestOfferByteIdenticalWhenEmbedderNil is the additive-default proof: with NO embedder wired (the
// default, incl. the offline test double), Offer at scale routes to the FROZEN W3-S1 lexical path and
// returns EXACTLY offerLexical — no perturbation. This is the byte-identical guarantee that lets the W3-S1
// contract tests stay green: the W3-S3 semantic fusion is dead code until SetEmbedder is called.
func TestOfferByteIdenticalWhenEmbedderNil(t *testing.T) {
	build := func() *OperatorRegistry {
		r := NewOperatorRegistry()
		for i := 0; i < 300; i++ {
			r.MintWithMove("mintedop"+itoaPad(i), "generative", "invent a candidate for topic "+itoaPad(i), MoveGround)
		}
		return r
	}
	const k = 48
	goal := "invent a candidate for topic 042"
	r := build()
	got := strings.Join(r.Offer(goal, k), ",")
	want := strings.Join(r.offerLexical(goal, k), ",")
	if got != want {
		t.Fatalf("Offer with no embedder must be byte-identical to the frozen W3-S1 offerLexical path:\n got=%s\nwant=%s", got, want)
	}
}

// TestOfferFusionRetainsHighCosineOp_Plumbing is the PLUMBING test (LABEL: plumbing-only). It uses the
// deterministic stubEmbedder to prove the RRF fusion in offerSemantic RETAINS a high-cosine minted op into
// the top-K even when that op has ZERO lexical overlap with the goal — i.e. the embedder is wired and its
// cosine channel carries the op through the fusion into the offered set.
//
// PLUMBING-ONLY: the stub assigns cosine by a hand-keyed topic axis, so this proves the WIRING (embedder
// -> cosine -> RRF -> offered set), NOT that a real embedder semantically ranks "evaluate a numeric
// expression" near "product of seven and eight". That capability claim is the model-gated test below.
func TestOfferFusionRetainsHighCosineOp_Plumbing(t *testing.T) {
	const k = 48
	r := NewOperatorRegistry()
	// Flood the catalog far past the cap with NOISE ops on a "noise" topic axis (no overlap with the goal).
	for i := 0; i < 300; i++ {
		r.MintWithMove("noiseop"+itoaPad(i), "generative", "noise filler operator number "+itoaPad(i), MoveGround)
	}
	// The target op: its NAME + INTENT share NO word with the goal text (zero lexical), and its NAME is
	// chosen to sort LAST among the zero-score minted ops (zz...) so on the lexical path's name-ascending
	// tie-break it is the one DROPPED at scale — making the cosine recovery unambiguous. Its INTENT still
	// lights the arithmetic topic axis under the stub, so cosine = 1.0 (a high-cosine hit).
	const target = "zzarith"
	const targetIntent = "evaluate a numeric expression to a value"
	r.MintWithMove(target, "synthesized", targetIntent, MoveGround)

	goal := "compute the multiplication arithmetic result" // shares the "arithmetic" topic, no word overlap with the target's text

	// Sanity: WITHOUT the embedder the target is lexically invisible. Confirm zero lexical overlap so the
	// plumbing test is honest about WHY the lexical path would drop it.
	if got := goalLexicalOverlap(goal, target+" "+targetIntent); got != 0 {
		t.Fatalf("precondition: target op must have ZERO lexical overlap with the goal, got %v", got)
	}

	// Wire the stub embedder: topic 0 (arithmetic) is lit by "arithmetic" (in the goal) OR "numeric"/"evaluate"
	// (in the target's intent) — DIFFERENT words, SAME axis, so cosine(goal, target)=1.0 with ZERO lexical
	// overlap. Topic 1 (noise) is lit by "noise" (in the noise ops only) — orthogonal to the goal. The seed
	// core embeds to the catch-all axis but is always included regardless, so it does not contend for fill slots.
	r.SetEmbedder(newStubEmbedder(
		[]string{"arithmetic", "numeric", "evaluate"}, // topic 0: the goal AND the target light this, via different words
		[]string{"noise"}, // topic 1: the noise ops light this
	))
	if r.RetrieverMode(true) != "semantic" {
		t.Fatalf("with an embedder wired RetrieverMode must be 'semantic', got %q", r.RetrieverMode(true))
	}

	offered := r.Offer(goal, k)
	if !offerContains(offered, target) {
		t.Fatalf("RRF fusion must retain the high-cosine zero-lexical op %q into top-K (plumbing), got %v", target, offered)
	}
	if len(offered) != k {
		t.Fatalf("curated Offer must still bound to k=%d, got %d", k, len(offered))
	}
	// Honest control: with NO embedder, the SAME catalog+goal drops the target on the frozen lexical path
	// (zero lexical score, name sorts last among the zero-score tie) — so the recovery above is genuinely
	// the cosine channel doing the work, not a name-ordering coincidence.
	lexical := NewOperatorRegistry()
	for i := 0; i < 300; i++ {
		lexical.MintWithMove("noiseop"+itoaPad(i), "generative", "noise filler operator number "+itoaPad(i), MoveGround)
	}
	lexical.MintWithMove(target, "synthesized", targetIntent, MoveGround)
	if offerContains(lexical.Offer(goal, k), target) {
		t.Fatalf("control: the frozen lexical path should DROP %q (zero lexical, sorts last), so the plumbing proves the cosine channel recovered it", target)
	}
}

// TestOfferSemanticRecallAtScale is the MODEL-GATED at-scale capability test. It grows the catalog to
// ~341 ops, mints an arithmetic op ("evaluate a numeric expression to a value") whose NAME sorts LAST among
// the zero-score ops (so the lexical path provably drops it), and asserts that with a REAL reachable
// embedder the goal "product of seven and eight" RECOVERS that op into the offered subset where the frozen
// lexical path drops it (zero surface overlap). SKIPS when no embeddings endpoint is reachable, so the
// offline suite stays green (mirrors skills_recall_test.go's ReachableEmbedder + Skip pattern). This is the
// CAPABILITY claim — proven only against a real embedder (W3-S3 Part 2: run with one reachable).
func TestOfferSemanticRecallAtScale(t *testing.T) {
	emb := retrieval.ReachableEmbedder()
	if emb == nil {
		t.Skip("no embeddings endpoint reachable — at-scale semantic catalog recall is model-gated (W3-S3 Part 2)")
	}
	const k = 48
	const target = "zzarith" // sorts after every filler* op, so the lexical name-tie-break drops it last
	const targetIntent = "evaluate a numeric expression to a value"

	build := func() *OperatorRegistry {
		r := NewOperatorRegistry()
		// grow to ~10x: flood with unrelated minted ops so the catalog is well past the cap (the 10x case).
		for i := 0; i < 340; i++ {
			r.MintWithMove("filler"+itoaPad(i), "generative", "unrelated filler capability "+itoaPad(i), MoveGround)
		}
		r.MintWithMove(target, "synthesized", targetIntent, MoveGround)
		return r
	}
	goal := "product of seven and eight" // semantically arithmetic; NO surface word from the target's text

	// Confirm zero lexical overlap so the recovery is genuinely semantic, not a lexical coincidence.
	if got := goalLexicalOverlap(goal, target+" "+targetIntent); got != 0 {
		t.Fatalf("precondition: goal must share no word with the target's text, got overlap %d", got)
	}

	// LEXICAL path (no embedder): the target has zero lexical score and sorts last; at scale the frozen
	// lexical path drops it.
	lexical := build()
	if offerContains(lexical.Offer(goal, k), target) {
		t.Fatalf("setup invalid: the frozen lexical path should DROP %q (it must be the semantic channel that recovers it)", target)
	}

	// SEMANTIC path: same catalog, embedder wired -> the target is recovered by cosine + RRF.
	semantic := build()
	semantic.SetEmbedder(emb)
	semOffered := semantic.Offer(goal, k)
	if !offerContains(semOffered, target) {
		t.Fatalf("semantic Offer must RECOVER %q for an arithmetic goal the lexical path dropped; got %v", target, semOffered)
	}
	t.Logf("semantic curation recovered 'arith' (%d offered of %d) where lexical dropped it", len(semOffered), len(semantic.Names()))
}

// goalLexicalOverlap reports the count of shared lowercased words between a goal and an op's text — a
// helper to assert the zero-lexical precondition.
func goalLexicalOverlap(goal, opText string) int {
	g := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		g[w] = true
	}
	n := 0
	for _, w := range strings.Fields(strings.ToLower(opText)) {
		if g[w] {
			n++
		}
	}
	return n
}

func offerContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
