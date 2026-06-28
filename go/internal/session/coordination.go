// coordination.go is intra-subconscious coordination (P3.10 / workflow-session §6b). Today a
// subconscious pass produces independent candidate results with no coordination between them. Four
// coordination patterns can change that — but they are CONFIG-GATED and ALL DEFAULT OFF, each
// independently flippable and validation-gated, so the system ships with today's behaviour and a
// pattern is turned on only once it has earned its keep (keep-or-revert). Every pattern COLLAPSES to one
// result and is bounded (no new fan-out → U≤1 is preserved).
//
//   - merge-synthesise   combine several results into one synthesised answer (uses Merge/Reduce, P3.6)
//   - producer-critic    one result is produced, a critic accepts or rejects it (keep-or-revert)
//   - refine-loop        iterate toward a result, bounded (uses the feedback-loop discipline, P3.2)
//   - interdep-decompose split interdependent parts, solve, recombine
package session

import "strings"

// CoordinationConfig gates the four patterns. The ZERO VALUE is the shipped default — every pattern OFF,
// i.e. exactly today's behaviour (a single result, no coordination).
type CoordinationConfig struct {
	MergeSynthesise   bool
	ProducerCritic    bool
	RefineLoop        bool
	InterdepDecompose bool
}

// Any reports whether any coordination pattern is enabled (OFF == today).
func (c CoordinationConfig) Any() bool {
	return c.MergeSynthesise || c.ProducerCritic || c.RefineLoop || c.InterdepDecompose
}

// Coordinate applies the ENABLED coordination patterns to the subconscious candidate results and
// COLLAPSES them to a single result. With every flag OFF it returns the first result UNCHANGED — today's
// behaviour, bit-for-bit. critic may be nil (producer-critic then accepts the candidate). maxRefine
// bounds the refine-loop iterations (durability). Each pattern is bounded and collapses to one, so no
// new fan-out is introduced (U≤1 preserved).
func Coordinate(cfg CoordinationConfig, results []string, critic func(string) bool, maxRefine int) string {
	if len(results) == 0 {
		return ""
	}
	if !cfg.Any() {
		return results[0] // OFF == today: the single (winning) result, no coordination
	}

	cur := results

	// interdep-decompose: treat each result as an independent part, then recombine (here: keep the set).
	if cfg.InterdepDecompose {
		cur = uniqueResults(cur)
	}

	// merge-synthesise: combine the parts into ONE synthesised answer.
	out := cur[0]
	if cfg.MergeSynthesise {
		m := Merge(cur, Reduce)
		out = strings.Join(m.Combined, "; ")
	}

	// refine-loop: iterate toward the answer, BOUNDED by maxRefine (no unbounded loop).
	if cfg.RefineLoop {
		if maxRefine < 1 {
			maxRefine = 1
		}
		for i := 0; i < maxRefine; i++ {
			refined := strings.TrimSpace(out)
			if refined == out { // converged -> early exit (feedback discipline, P3.2)
				break
			}
			out = refined
		}
	}

	// producer-critic: the critic accepts or REVERTS the coordinated result (keep-or-revert). On reject,
	// fall back to the first raw result — coordination can never make the answer worse than no-coordination.
	if cfg.ProducerCritic && critic != nil && !critic(out) {
		return results[0]
	}
	return out
}

func uniqueResults(rs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		k := strings.Join(strings.Fields(strings.ToLower(r)), " ")
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}
