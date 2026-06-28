// merge.go is the Merge node + reduce/vote strategies (P3.6 / SR-3). When a workflow fans out (a Par
// group or several dispatched Sessions), their results must be RECOMBINED into one — that is a
// workflow-INTERNAL merge, the opposite of SR-1: competing INDEPENDENT candidates go UP as branches for
// the Controller to weigh; a PLANNED fan-out's results come back DOWN through a Merge. Merge = combine a
// plan; branch = let Conscious decide.
//
// Two strategies: REDUCE (combine + dedup near-duplicate results into one set) and VOTE (the strict
// majority wins). A vote with no majority is a genuine disagreement — not something to silently merge —
// so it is surfaced as a CONFLICT, which the engine turns into branches (SR-1).
package session

import "strings"

// MergeStrategy selects how a fan-out's results are recombined.
type MergeStrategy int

const (
	Reduce MergeStrategy = iota // combine + dedup into a unique set
	Vote                        // the strict-majority result wins
)

// MergeResult is the outcome of merging a fan-out.
type MergeResult struct {
	Combined []string // the merged result set (Reduce) or the single winner in a slice (Vote)
	Winner   string   // Vote: the majority result; "" on conflict
	Conflict bool     // genuine disagreement — surface as branches, do NOT merge
}

// normMerge is the dedup/vote key: trimmed + lower-cased + whitespace-collapsed.
func normMerge(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// Merge recombines a fan-out's results per the strategy. Reduce returns the unique results (dedup,
// first-occurrence order), never a conflict (it is combining a plan). Vote returns the strict-majority
// result (> half) as the winner; a tie / no-majority split is a Conflict the caller surfaces as
// branches. An empty input is an empty, non-conflicting result.
func Merge(results []string, strategy MergeStrategy) MergeResult {
	if len(results) == 0 {
		return MergeResult{}
	}
	switch strategy {
	case Vote:
		counts := map[string]int{}
		canon := map[string]string{} // norm -> first surface form
		for _, r := range results {
			k := normMerge(r)
			counts[k]++
			if _, ok := canon[k]; !ok {
				canon[k] = r
			}
		}
		best, bestN := "", 0
		for k, n := range counts {
			if n > bestN {
				best, bestN = k, n
			}
		}
		if bestN*2 > len(results) { // strict majority
			return MergeResult{Combined: []string{canon[best]}, Winner: canon[best]}
		}
		return MergeResult{Conflict: true} // no consensus -> branch

	default: // Reduce
		seen := map[string]bool{}
		var uniq []string
		for _, r := range results {
			k := normMerge(r)
			if seen[k] {
				continue
			}
			seen[k] = true
			uniq = append(uniq, r)
		}
		return MergeResult{Combined: uniq}
	}
}
