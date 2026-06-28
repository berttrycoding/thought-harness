// deliberative.go — THE ROBUSTNESS LEVER: cross-sample OUTCOME REDUNDANCY (self-consistency).
//
// THE GAP (the measured one). The realhard eval swings ±56pp run-to-run on claude (same flags-off
// code), reframed as a run-level ROBUSTNESS gap = σ_R, the cross-run SD of the per-task solve-rate.
// The harness has ZERO cross-sample outcome redundancy: one trajectory, one episode, one verdict.
// The only "vote" anywhere is the GATE's WITHIN-tick candidate arbitration (seams/hidden.go) — that
// concentrates one trajectory's intake, it does NOT resample the OUTCOME. So a single trajectory's
// path-luck (which template the test double draws, which fork the live model takes) shows up
// undamped as run-level variance.
//
// THE LEVER (Option B, the gated decision). Run K INDEPENDENT trajectories of the SAME config — each
// a full, separate episode on a distinct, deterministic per-sample seed — collect the K final
// answers, and RECONCILE: majority-vote on the normalized answer; tie-break by V(s) via the EXISTING
// value/rank machinery (control.Rank — the same primitive seams.Gate.Select calls), NOT a new
// scorer. Self-consistency over K independent samples concentrates the outcome distribution, so the
// per-task solve indicator becomes a majority of K draws — its variance falls (the binomial-majority
// concentration), which is exactly the run-level variance σ_R attacks.
//
// THE FLAG. THOUGHT_DELIBERATIVE_K (int env, default 1; empty/invalid/<1 → 1).
//   - K==1 → BYTE-IDENTICAL to today: the deliberative wrapper does NOT engage (no extra episode, no
//     reconciliation event). The single-episode path is the caller's own, untouched.
//   - K>1  → K independent episodes + reconcile, with one conscious.deliberation event emitted.
//
// DURABILITY (preserved by construction). K SEQUENTIAL independent episodes do NOT change per-episode
// excitation n or fan-out width — it is K episodes, not K parallel branches. Each sample is a fresh,
// fully-isolated engine on its own seed; the regulator/excitation/dead-time of each is exactly one
// episode's. The control-theoretic regime (n<1, U≤1, 0<K_reg·g<2, μ>0, bounded fan-out) is unchanged.
//
// HEADLESS-PURE: no I/O, no printing, no TUI imports. The per-sample work is supplied by the caller
// as a closure (the bench builds a fresh engine over a fresh workspace per sample); this file owns
// only the deterministic seed derivation, the reconciliation math, and the bus event.
package engine

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// deliberativeK is the THOUGHT_DELIBERATIVE_K toggle resolved ONCE at package init (mirrors the
// resolveWorkingWindow / resolveForceGround env-knob pattern). Default 1 (OFF — byte-identical):
// empty / unparsable / <1 all resolve to 1, so a fat-fingered value can never silently engage the
// wrapper or pick a degenerate K.
var deliberativeK = resolveDeliberativeK()

// resolveDeliberativeK reads THOUGHT_DELIBERATIVE_K once. It is a sample count (>=2 enables the
// deliberative reconciliation); empty / unset / non-integer / <1 all clamp to 1 (the single-episode
// default — byte-identical). The clamp is the robust-parse the brief requires.
func resolveDeliberativeK() int {
	return ParseDeliberativeK(os.Getenv("THOUGHT_DELIBERATIVE_K"))
}

// MaxDeliberativeK is the soft upper clamp on the deliberative sample count (durability-gate
// recommendation f5ca473): K is K *sequential* whole episodes, so a fat-fingered THOUGHT_DELIBERATIVE_K
// (e.g. 10000) does not violate the durable regime (each sample is one bounded episode) but WOULD
// silently launch a multi-hour, multi-thousand-call run. Self-consistency saturates well below this
// (diminishing returns past ~5-10), so 64 is generous headroom while making an absurd value a no-op
// at the ceiling rather than a runaway. It is an ops/cost guardrail, not a regime fix.
const MaxDeliberativeK = 64

// ParseDeliberativeK robustly parses a THOUGHT_DELIBERATIVE_K value: an integer in [2, MaxDeliberativeK]
// engages the deliberative wrapper; "" / whitespace / a non-integer / a negative / zero / 1 all clamp
// to 1 (OFF, byte-identical); a value above MaxDeliberativeK clamps DOWN to MaxDeliberativeK (the cost
// guardrail). Exported so the bench arm + the tests read the SAME parse (one source of truth, no
// divergent re-parse).
func ParseDeliberativeK(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	if n > MaxDeliberativeK {
		return MaxDeliberativeK
	}
	return n
}

// DeliberativeK reports the resolved THOUGHT_DELIBERATIVE_K (the engaged sample count; 1 = OFF,
// single-episode, byte-identical). The bench arm reads it to decide whether to call the deliberative
// path. Read-only.
func DeliberativeK() int { return deliberativeK }

// DeliberativeSeed derives the deterministic per-sample seed for sample i (0-based) from a base seed.
// It is a fixed, reproducible offset — same (base, i) → same seed, ALWAYS — so the K trajectories are
// genuinely INDEPENDENT (distinct draw sequences) yet the whole reconciliation is fully reproducible.
// It is NOT a within-launch K-replay of one engine: each sample seeds a fresh engine, so the draws do
// not share state. Sample 0 keeps the base seed VERBATIM, so a K>1 run's first trajectory is the same
// trajectory the K==1 path would have produced on that base — the byte-identical anchor.
func DeliberativeSeed(base int64, i int) int64 {
	if i == 0 {
		return base
	}
	// A large odd stride keeps successive sample seeds far apart in the MT19937 state space (so two
	// samples do not accidentally share a near-identical draw prefix) while staying a pure function of
	// (base, i). The stride is arbitrary-but-fixed; determinism is the only requirement.
	return base + int64(i)*1_000_003
}

// DeliberativeSample is one independent trajectory's outcome — the minimum the reconciliation needs:
// the final ANSWER the episode produced (the text the oracle would score) and the active line's V(s)
// (the value appraisal the tie-break ranks on). Seed records which per-sample seed produced it (for
// the event + reproducibility audit). The caller's sample closure fills these from one episode run.
type DeliberativeSample struct {
	Seed   int64
	Answer string
	// Value is the active branch's V(s) at episode end (eng.ValueScalar()) — the EXISTING value
	// signal, used ONLY as the tie-break rank key (never as a new scorer). 0 when unavailable.
	Value float64
}

// DeliberativeResult is the reconciliation outcome the caller adopts as the episode answer.
type DeliberativeResult struct {
	K        int                  // the number of independent samples actually run
	Answer   string               // the reconciled (majority/V(s)-tie-broken) final answer
	Samples  []DeliberativeSample // every sample, in sample order (audit / event)
	Tally    map[string]int       // normalized-answer → vote count (the self-consistency histogram)
	WinnerIx int                  // index into Samples of the adopted answer's representative sample
	Tie      bool                 // true when the top vote count was shared (V(s) broke the tie)
	Reason   string               // a short why (for the event + audit)
}

// RunDeliberative runs K independent trajectories via the caller-supplied sample closure, reconciles
// their final answers (majority vote; V(s) tie-break through the existing rank machinery), emits one
// conscious.deliberation event, and returns the reconciled result.
//
//   - K<=1 is the single-episode anchor: it runs sample 0 only and returns its answer verbatim, with
//     NO conscious.deliberation event (byte-identical to the caller's own single-episode path — the
//     wrapper engages only at K>1, and at K==1 `normalize` is IGNORED). The caller is expected to NOT
//     route through here at K==1, but this guard makes the contract total.
//   - sample(i) runs trajectory i (the caller builds a fresh engine on DeliberativeSeed(base,i) and
//     runs one episode); it returns that trajectory's (answer, V(s)). An error from any sample aborts.
//   - normalize is the VOTE-EQUIVALENCE KEY: two answers vote together iff normalize(a)==normalize(b).
//     This MUST match the caller's SCORING notion of "same answer" — else K episodes that reach the
//     SAME conclusion in different phrasings split into K distinct vote keys, the majority dissolves
//     into a K-way tie, and the V(s) tie-break degenerates the mechanism to best-of-N-by-V(s) (the
//     proven defect this signature fixes). A nil normalize falls back to NumericAwareNormalizeAnswer
//     (numeric-aware: a conclusion's LAST number is the key when present, else the coarse lower/collapse)
//     so even a non-bench caller groups equivalent numeric conclusions.
//
// emit may be nil (no event). The reconciliation is PURE + deterministic given the K samples — the
// only nondeterminism is whatever the caller's per-sample episode introduces, and that is seeded.
func RunDeliberative(k int, base int64, emit events.Emit, normalize func(string) string, sample func(i int, seed int64) (DeliberativeSample, error)) (DeliberativeResult, error) {
	if k < 1 {
		k = 1
	}
	if normalize == nil {
		normalize = NumericAwareNormalizeAnswer
	}
	samples := make([]DeliberativeSample, 0, k)
	for i := 0; i < k; i++ {
		seed := DeliberativeSeed(base, i)
		s, err := sample(i, seed)
		if err != nil {
			return DeliberativeResult{}, err
		}
		s.Seed = seed
		samples = append(samples, s)
	}

	// K==1: the single-episode anchor — return the lone trajectory verbatim, no reconciliation event,
	// `normalize` ignored (no grouping happens — the lone sample IS the answer).
	if k == 1 {
		ans := ""
		if len(samples) == 1 {
			ans = samples[0].Answer
		}
		return DeliberativeResult{
			K:        1,
			Answer:   ans,
			Samples:  samples,
			Tally:    nil,
			WinnerIx: 0,
			Tie:      false,
			Reason:   "single episode (K=1, no reconciliation)",
		}, nil
	}

	res := reconcile(samples, normalize)
	res.K = k
	if emit != nil {
		emitDeliberation(emit, res)
	}
	return res, nil
}

// reconcile is the PURE self-consistency reduction over K samples: majority vote on the normalized
// answer; ties broken by the GROUP's summed V(s) ranked through control.Rank (the EXISTING value/rank
// floor seams.Gate.Select calls — NOT a new scorer). Deterministic: groups are visited in stable
// first-appearance order, the vote argmax is the first group reaching the max count, and the tie-break
// rank is the deterministic control floor. Same samples → same winner, always.
//
// normalize is the VOTE-EQUIVALENCE KEY (caller-supplied, never nil here — RunDeliberative defaults
// it). It MUST match the caller's scoring notion of "same answer" so equivalent conclusions form a
// majority; a coarse key that mismatches the scorer reintroduces the proven best-of-N degeneracy.
func reconcile(samples []DeliberativeSample, normalize func(string) string) DeliberativeResult {
	// Group the samples by their NORMALIZED answer (the vote key) — this is the SAME notion of "same
	// answer" the caller's scorer uses (case/space/punct-insensitive at minimum, numeric- or
	// oracle-aware when the caller supplies it), so two trajectories that landed on the same conclusion
	// in different phrasings vote together rather than splitting into a spurious tie.
	tally := map[string]int{}
	groupSum := map[string]float64{} // group key → summed V(s) (the tie-break rank input)
	groupFirstIx := map[string]int{} // group key → index of its first sample (the representative)
	groupOrder := []string{}         // stable first-appearance order of group keys
	seen := map[string]bool{}
	for i, s := range samples {
		key := normalize(s.Answer)
		if !seen[key] {
			seen[key] = true
			groupOrder = append(groupOrder, key)
			groupFirstIx[key] = i
		}
		tally[key]++
		groupSum[key] += s.Value
	}

	// Find the top vote count.
	top := 0
	for _, k := range groupOrder {
		if tally[k] > top {
			top = tally[k]
		}
	}
	// The contenders are every group sharing the top vote count, in stable order.
	var contenders []string
	for _, k := range groupOrder {
		if tally[k] == top {
			contenders = append(contenders, k)
		}
	}

	tie := len(contenders) > 1
	winnerKey := contenders[0]
	reason := "majority vote (" + strconv.Itoa(top) + "/" + strconv.Itoa(len(samples)) + ")"
	if tie {
		// Tie-break by V(s) THROUGH the existing rank machinery: build one candidate per tied group
		// carrying the group's summed V(s) as its Relevance (the field control.Rank weights), rank
		// them with control.Rank (the deterministic value floor seams.Gate.Select uses), and adopt
		// the highest-ranked group. A further tie in the rank score keeps stable first-appearance
		// order (the contenders slice is already in that order, and the argmax below is first-wins).
		cands := make([]types.Candidate, len(contenders))
		for i, key := range contenders {
			cands[i] = types.Candidate{
				Text:      samples[groupFirstIx[key]].Answer,
				Source:    types.INJECTED,
				Relevance: control.Clamp01(groupSum[key]),
			}
		}
		scores, _ := control.Rank(cands, nil)
		bestIx, bestScore := 0, scores[0]
		for i := 1; i < len(scores); i++ {
			if scores[i] > bestScore {
				bestIx, bestScore = i, scores[i]
			}
		}
		winnerKey = contenders[bestIx]
		reason = "tie at " + strconv.Itoa(top) + "/" + strconv.Itoa(len(samples)) +
			" broken by V(s) (rank " + strconv.FormatFloat(bestScore, 'f', 3, 64) + ")"
	}

	winnerIx := groupFirstIx[winnerKey]
	return DeliberativeResult{
		Answer:   samples[winnerIx].Answer,
		Samples:  samples,
		Tally:    tally,
		WinnerIx: winnerIx,
		Tie:      tie,
		Reason:   reason,
	}
}

// NormalizeAnswer canonicalizes a final answer for the self-consistency vote: trim, lower-case, and
// collapse internal whitespace runs to a single space. This is intentionally the COARSE notion of
// "same answer" — two trajectories that reach the same conclusion in different spacing/case vote
// together — and is deterministic (no RNG, no locale). It is NOT the oracle (which is the bench's
// deterministic scorer); it is only the vote key, so the reconciliation never needs to know a task's
// oracle kind to group equivalent answers.
func NormalizeAnswer(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// delibNumberRe extracts a numeric literal (optionally with thousands commas) from an answer for the
// numeric-aware vote key. It mirrors the realhard oracle's numberRe so the engine default groups
// numeric conclusions the SAME way the bench scorer does — but the engine owns its own copy (it must
// not import the bench package). Two-place, but deliberately so: the realhard caller passes its own
// oracle-faithful canonicalAnswer; this is only the engine's standalone fallback.
var delibNumberRe = regexp.MustCompile(`-?\d{1,3}(?:,\d{3})+(?:\.\d+)?|-?\d+(?:\.\d+)?`)

// NumericAwareNormalizeAnswer is the engine's DEFAULT deliberative vote key (the RunDeliberative
// nil-default). It is numeric-aware: when the answer contains at least one number it keys on the LAST
// number's canonical form (the conclusion a trajectory lands on — "after tracing, the answer is 12."
// and "I computed 12" both key on "12"), so differently-phrased numeric conclusions vote together
// instead of splitting into a spurious tie (the proven best-of-N defect). When there is no number it
// falls back to the coarse NormalizeAnswer (lower + collapse whitespace). Canonical form = strconv of
// the parsed float, so "12", "12.0", "12,0"-style thousands all canonicalize identically. Deterministic
// (no RNG, no locale). It is NOT the oracle — it is only the vote key — but it now MATCHES the dominant
// (numeric) scoring notion so a non-bench caller benefits without supplying its own normalizer.
func NumericAwareNormalizeAnswer(s string) string {
	nums := delibNumberRe.FindAllString(s, -1)
	if len(nums) > 0 {
		clean := strings.ReplaceAll(nums[len(nums)-1], ",", "")
		if v, err := strconv.ParseFloat(clean, 64); err == nil {
			return strconv.FormatFloat(v, 'g', -1, 64)
		}
	}
	return NormalizeAnswer(s)
}

// emitDeliberation emits the conscious.deliberation event carrying the full reconciliation WHY: the
// K candidate answers (truncated for the wire), the vote tally, the winner, and whether V(s) broke a
// tie. Deterministic wire: the tally is rendered in stable group-key order and the candidate list in
// sample order, so the event stream is reproducible.
func emitDeliberation(emit events.Emit, res DeliberativeResult) {
	cands := make([]any, len(res.Samples))
	for i, s := range res.Samples {
		cands[i] = map[string]any{
			"seed":   s.Seed,
			"answer": runeHead(s.Answer, 80),
			"value":  round3del(s.Value),
		}
	}
	// tally rendered in stable key order (sorted) so the wire is deterministic regardless of map order.
	keys := make([]string, 0, len(res.Tally))
	for k := range res.Tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tally := make(map[string]any, len(res.Tally))
	for _, k := range keys {
		tally[k] = res.Tally[k]
	}
	emit(events.Deliberation,
		"deliberation: K="+strconv.Itoa(res.K)+" -> "+res.Reason,
		events.D{
			"k":          res.K,
			"candidates": cands,
			"tally":      tally,
			"winner":     runeHead(res.Answer, 80),
			"winner_ix":  res.WinnerIx,
			"tie":        res.Tie,
			"why":        res.Reason,
		})
}

// runeHead returns the first n runes of s (a code-point-safe head for the wire), appending an
// ellipsis when truncated. Used only for the event summary/data (never for the vote key).
func runeHead(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// round3del rounds to 3 decimals for the wire (the same per-emit-site rounding obligation the value
// signal uses). Kept local to avoid importing the value package's unexported helper.
func round3del(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}
