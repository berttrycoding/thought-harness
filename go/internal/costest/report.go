package costest

import (
	"fmt"
	"sort"
	"strings"
)

// Report renders a Projection as a plain-text block (no emoji, Unicode box-drawing only — the
// house style). It prints: the token totals + how they were derived (calibrated vs chars/4),
// the prefix-reuse cache-hit % + the block accounting, and the projected DeepSeek $ for the
// workload (with the no-cache upper bound + the saving). `card` source is shown so the reader
// knows which rate row priced it.
func Report(p Projection, calls []Call, cardSource string) string {
	var b strings.Builder
	f := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	f("costest — DeepSeek cost projection (no paid run)\n")
	f("════════════════════════════════════════════════\n")
	f("calls analysed : %d llm.call events\n", p.Tokens.Calls)
	f("rate model     : %s   (card: %s)\n", p.RateModel, cardSource)

	// --- tokens ---
	tm := p.Tokens.Model
	method := fmt.Sprintf("chars/token heuristic (~%.2f, uncalibrated)", tm.CharsPerToken)
	if tm.Calibrated {
		method = fmt.Sprintf("calibrated %.2f chars/token (from %d usage-bearing sample(s) in the log)", tm.CharsPerToken, tm.Samples)
	}
	f("\nTOKENS (in / out)\n")
	f("  input tokens   : %d\n", p.Tokens.InTokens)
	f("  output tokens  : %d\n", p.Tokens.OutTokens)
	f("  total tokens   : %d\n", p.Tokens.InTokens+p.Tokens.OutTokens)
	if p.Tokens.EstimatedCalls > 0 {
		f("  estimated      : %d/%d calls had NO server usage → sized from text via %s\n",
			p.Tokens.EstimatedCalls, p.Tokens.Calls, method)
	} else {
		f("  estimated      : 0 calls — every call carried real server usage\n")
	}

	// --- prefix-reuse cache ---
	c := p.Cache
	f("\nPREFIX-REUSE CACHE (DeepSeek caches on exact prompt-PREFIX match)\n")
	f("  est. cache-hit : %.1f%%   (%d hit / %d total input tokens)\n",
		c.HitFraction()*100, c.HitTokens, c.HitTokens+c.MissTokens)
	f("  block size     : %d tokens (matched prefix floored to whole blocks)\n", c.BlockTokens)
	if c.ServerHitCalls > 0 {
		f("  server hits    : %d/%d calls carried a REAL cache-hit count (used directly)\n", c.ServerHitCalls, c.Calls)
	} else {
		f("  server hits    : 0 calls reported a cache split → all hits ESTIMATED by prefix reuse\n")
	}

	// --- projected $ ---
	f("\nPROJECTED COST  (@ %s)\n", p.RateModel)
	if !p.HasUSD {
		f("  rates unknown — %q is not in the rate card (no $ projected; not billed as $0)\n", p.RateModel)
	} else {
		f("  with prefix cache : $%.6f\n", p.USD)
		f("  no-cache upper    : $%.6f\n", p.USDNoCacheFloor)
		f("  prefix reuse saves: ~%.1f%% of cost vs cold (no-cache)\n", p.Savings()*100)
	}

	// --- top roles by input volume (where the prompt cost concentrates) ---
	if rs := topRoles(calls, 6); len(rs) > 0 {
		f("\nTOP ROLES BY PROMPT VOLUME (input chars — where the prefix lives)\n")
		for _, r := range rs {
			f("  %-26s %d calls, %d prompt chars\n", r.role, r.calls, r.chars)
		}
	}
	return b.String()
}

// roleAgg is one row of the per-role prompt-volume summary.
type roleAgg struct {
	role  string
	calls int
	chars int
}

// topRoles aggregates prompt chars per role and returns the top n by char volume (ties broken
// by role name for determinism). This shows WHERE the input cost — and thus the prefix-cache
// opportunity — concentrates (a fat, repeated System block is the prime cache target).
func topRoles(calls []Call, n int) []roleAgg {
	by := map[string]*roleAgg{}
	for _, c := range calls {
		r := c.Role
		if r == "" {
			r = "(unknown)"
		}
		a := by[r]
		if a == nil {
			a = &roleAgg{role: r}
			by[r] = a
		}
		a.calls++
		a.chars += len([]rune(c.Concat()))
	}
	out := make([]roleAgg, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].chars != out[j].chars {
			return out[i].chars > out[j].chars
		}
		return out[i].role < out[j].role
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
