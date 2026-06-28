package funnel

import "math"

// VectorSimilarity is the embedding-based near-dup signal (the real semantic cut): the raw cosine of
// the candidates' cached Vectors, matching the registry-scaling §4 "cosine > θ" gate. It returns 0 when
// either vector is missing, zero, or dimension-mismatched, so a caller can safely inject it for a batch
// where only SOME candidates carry an embedding (those pairs fall to 0 and rely on the exact/lexical
// cuts instead). Pure stdlib — the funnel stays a leaf utility; the caller embeds the texts (via a
// reachable retrieval.Embedder) and stores the vectors on each Candidate before calling.
func VectorSimilarity(a, b Candidate) float64 {
	return cosine(a.Vector, b.Vector)
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
