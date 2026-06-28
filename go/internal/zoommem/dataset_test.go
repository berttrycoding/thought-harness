package zoommem

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── dataset loader ──────────────────────────────────────────────────────────────────────────────
// Loads the realistic multi-domain episodes in testdata/ (built by the fan-out agents). Each episode
// is a branching thought stream with BIG paragraph units, branch/unit kinds, entities, and relevance
// probes. Units carry their entities (a stronger relevance cue than raw word overlap); the extra
// metadata (kinds, probes) rides in Episode so the Unit struct stays as the engine sees it (and the
// hand-written CookDinnerSession T1 stays green).

type rawEpisode struct {
	Domain   string `json:"domain"`
	Title    string `json:"title"`
	Goal     string `json:"goal"`
	Branches []struct {
		ID   int    `json:"id"`
		Kind string `json:"kind"`
	} `json:"branches"`
	Units []struct {
		ID       int      `json:"id"`
		Branch   int      `json:"branch"`
		Parent   int      `json:"parent"`
		Tick     int      `json:"tick"`
		Kind     string   `json:"kind"`
		Full     string   `json:"full"`
		Thought  string   `json:"thought"`
		Entities []string `json:"entities"`
	} `json:"units"`
	Probes []struct {
		FocusID       int    `json:"focus_id"`
		ShouldSurface []int  `json:"should_surface"`
		ShouldFade    []int  `json:"should_fade"`
		Why           string `json:"why"`
	} `json:"probes"`
}

type Probe struct {
	FocusID       int
	ShouldSurface []int
	ShouldFade    []int
}

type Episode struct {
	Name       string
	Domain     string
	Units      []Unit
	Kind       map[int]string // unit id -> kind
	BranchKind map[int]string // branch id -> kind
	Probes     []Probe
}

func (r rawEpisode) toEpisode(name string) Episode {
	ep := Episode{Name: name, Domain: r.Domain, Kind: map[int]string{}, BranchKind: map[int]string{}}
	for _, b := range r.Branches {
		ep.BranchKind[b.ID] = b.Kind
	}
	for _, u := range r.Units {
		ep.Units = append(ep.Units, Unit{
			ID: u.ID, Branch: u.Branch, Parent: u.Parent, Tick: u.Tick,
			Full: u.Full, Thought: u.Thought, Entities: u.Entities,
		})
		ep.Kind[u.ID] = u.Kind
	}
	sort.SliceStable(ep.Units, func(i, j int) bool { return ep.Units[i].Tick < ep.Units[j].Tick })
	for _, p := range r.Probes {
		ep.Probes = append(ep.Probes, Probe{FocusID: p.FocusID, ShouldSurface: p.ShouldSurface, ShouldFade: p.ShouldFade})
	}
	return ep
}

func loadEpisodes(t *testing.T) []Episode {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	var eps []Episode
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var raw rawEpisode
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		eps = append(eps, raw.toEpisode(strings.TrimSuffix(e.Name(), ".json")))
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Name < eps[j].Name })
	if len(eps) == 0 {
		t.Fatal("no episodes loaded")
	}
	return eps
}

// episodeBudget = a working set well below the sum of all thoughts (so compression MUST engage) but
// above the largest single full unit (so the focus always fits sharp). ~35% of summed L1 size.
func episodeBudget(units []Unit) int {
	sumL1, maxL0 := 0, 0
	for _, u := range units {
		sumL1 += u.size(L1Thought)
		if s := u.size(L0Full); s > maxL0 {
			maxL0 = s
		}
	}
	b := sumL1 * 35 / 100
	if b < maxL0+10 {
		b = maxL0 + 10
	}
	return b
}

// ── T1 at scale ─────────────────────────────────────────────────────────────────────────────────
// The Test-1 properties, now over every realistic episode, growing one unit at a time.
func TestDataset_T1_BudgetCoherence(t *testing.T) {
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		maxShelved := 0
		for n := 1; n <= len(ep.Units); n++ {
			units := ep.Units[:n]
			focusID := units[n-1].ID
			ctx := Assemble(units, focusID, budget)
			if ctx.Total > budget {
				t.Fatalf("[%s] n=%d OVERFLOW total=%d > budget=%d", ep.Name, n, ctx.Total, budget)
			}
			if fl := levelOf(ctx, focusID); fl > L1Thought {
				t.Fatalf("[%s] n=%d focus #%d faded to %s", ep.Name, n, focusID, fl)
			}
			if got := len(ctx.Shown) + len(ctx.Shelved); got != n {
				t.Fatalf("[%s] n=%d lost a thought: %d != %d", ep.Name, n, got, n)
			}
			if len(ctx.Shelved) > maxShelved {
				maxShelved = len(ctx.Shelved)
			}
		}
		if maxShelved == 0 {
			t.Errorf("[%s] compression never engaged (budget=%d too loose)", ep.Name, budget)
		}
		t.Logf("[%-22s] units=%d budget=%d maxShelved=%d  T1 OK", ep.Name, len(ep.Units), budget, maxShelved)
	}
}

// ── T3 — the controller surfaces the RIGHT thoughts ─────────────────────────────────────────────
// surfaced := shown at <= L2 (one-liner or better). Across all probes: recall of should_surface, and
// the relevance-over-recency invariant (a should_fade unit is never less faded than the best
// should_surface).
func TestDataset_T3_RelevanceSurfacing(t *testing.T) {
	totSurface, hitSurface := 0, 0
	totFade, fadeViolations := 0, 0
	probes := 0
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		for _, p := range ep.Probes {
			probes++
			ctx := Assemble(ep.Units, p.FocusID, budget)
			bestSurface := L4Pointer // most-surfaced (smallest) level among should_surface
			for _, id := range p.ShouldSurface {
				totSurface++
				lv := levelOf(ctx, id)
				if lv <= L2OneLiner {
					hitSurface++
				}
				if lv < bestSurface {
					bestSurface = lv
				}
			}
			for _, id := range p.ShouldFade {
				totFade++
				if levelOf(ctx, id) < bestSurface { // an irrelevant-recent beat the relevant-old
					fadeViolations++
				}
			}
		}
	}
	recall := float64(hitSurface) / float64(totSurface)
	fadeOK := 1.0 - float64(fadeViolations)/float64(totFade)
	t.Logf("T3 over %d probes: surface-recall=%.0f%% (%d/%d), relevance-beats-recency=%.0f%% (%d/%d fade ok)",
		probes, recall*100, hitSurface, totSurface, fadeOK*100, totFade-fadeViolations, totFade)
	if recall < 0.60 {
		t.Errorf("T3 surface-recall %.0f%% < 60%% — relevant old thoughts not reliably surfaced", recall*100)
	}
	if fadeOK < 0.70 {
		t.Errorf("T3 relevance-beats-recency %.0f%% < 70%%", fadeOK*100)
	}
}

// ── T2 — multi-level beats single-active-branch ─────────────────────────────────────────────────
// Baseline: only the focus's own branch is visible (today's single-active-branch model). Zoomable:
// the full graph at mixed zoom. Compare recall of should_surface (which mostly lives on OTHER
// branches). Multi-level must do strictly better — that is what proves the faded cross-branch gists
// earn their keep.
func TestDataset_T2_MultiLevelValue(t *testing.T) {
	var zoomRec, baseRec []float64
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		for _, p := range ep.Probes {
			if len(p.ShouldSurface) == 0 {
				continue
			}
			focus := find(ep.Units, p.FocusID)
			// baseline: restrict to the focus branch, then assemble.
			var branchOnly []Unit
			for _, u := range ep.Units {
				if u.Branch == focus.Branch {
					branchOnly = append(branchOnly, u)
				}
			}
			baseRec = append(baseRec, recallSurface(Assemble(branchOnly, p.FocusID, budget), p.ShouldSurface))
			zoomRec = append(zoomRec, recallSurface(Assemble(ep.Units, p.FocusID, budget), p.ShouldSurface))
		}
	}
	z, b := mean(zoomRec), mean(baseRec)
	t.Logf("T2: should_surface recall — zoomable=%.0f%% vs single-active-branch=%.0f%% (n=%d probes)",
		z*100, b*100, len(zoomRec))
	if z <= b {
		t.Errorf("T2 FAIL: multi-level (%.0f%%) did not beat single-branch (%.0f%%)", z*100, b*100)
	}
}

func recallSurface(ctx Context, want []int) float64 {
	if len(want) == 0 {
		return 0
	}
	hit := 0
	for _, id := range want {
		if levelOf(ctx, id) <= L2OneLiner {
			hit++
		}
	}
	return float64(hit) / float64(len(want))
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// ── T-ratio — big units + high compression ratio (Topic A) ──────────────────────────────────────
func TestDataset_TRatio_Compression(t *testing.T) {
	var l0, l1, l2, l3 int
	nUnits := 0
	for _, ep := range loadEpisodes(t) {
		for _, u := range ep.Units {
			l0 += u.size(L0Full)
			l1 += u.size(L1Thought)
			l2 += u.size(L2OneLiner)
			l3 += u.size(L3Tag)
			nUnits++
		}
	}
	a0 := float64(l0) / float64(nUnits)
	t.Logf("T-ratio: avg unit size  L0=%.0f  L1=%.0f  L2=%.0f  L3=%.0f words (%d units)",
		a0, float64(l1)/float64(nUnits), float64(l2)/float64(nUnits), float64(l3)/float64(nUnits), nUnits)
	t.Logf("T-ratio: zoom-out ratios  L0:L1=%.1fx  L0:L2=%.1fx  L0:L3=%.1fx  (L0:L4 pointer = ~%.0fx)",
		float64(l0)/float64(l1), float64(l0)/float64(l2), float64(l0)/float64(l3), a0)

	// effective squeeze: a full episode's complete reasoning (sum L0) vs the assembled working set.
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		full := 0
		for _, u := range ep.Units {
			full += u.size(L0Full)
		}
		ctx := Assemble(ep.Units, ep.Units[len(ep.Units)-1].ID, budget)
		t.Logf("[%-22s] full reasoning=%d words -> working set=%d words (%.1fx squeeze, budget=%d)",
			ep.Name, full, ctx.Total, float64(full)/float64(ctx.Total), budget)
		if float64(full)/float64(ctx.Total) < 3.0 {
			t.Errorf("[%s] effective squeeze < 3x — big units not compressing enough", ep.Name)
		}
	}
}

// ── T-accounting — per-kind / per-branch working-set breakdown (Q3) ──────────────────────────────
func TestDataset_TAccounting_Breakdown(t *testing.T) {
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		focusID := ep.Units[len(ep.Units)-1].ID
		ctx := Assemble(ep.Units, focusID, budget)
		byKind := map[string]int{}
		byBranch := map[int]int{}
		for _, s := range ctx.Shown {
			byKind[ep.Kind[s.Unit.ID]] += s.Unit.size(s.Level)
			byBranch[s.Unit.Branch] += s.Unit.size(s.Level)
		}
		t.Logf("[%s] working-set %d/%d words at focus #%d (%s branch):", ep.Name, ctx.Total, budget, focusID, ep.BranchKind[find(ep.Units, focusID).Branch])
		t.Logf("    by KIND:   %s", fmtCounts(byKind))
		t.Logf("    by BRANCH: %s", fmtBranchCounts(byBranch, ep.BranchKind))
	}
}

func fmtCounts(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v })
	var parts []string
	for _, e := range s {
		parts = append(parts, fmt.Sprintf("%s=%d", e.k, e.v))
	}
	return strings.Join(parts, "  ")
}

func fmtBranchCounts(m map[int]int, kinds map[int]string) string {
	var ids []int
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] })
	var parts []string
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("b%d(%s)=%d", id, kinds[id], m[id]))
	}
	return strings.Join(parts, "  ")
}

// ── T4 — the level-matrix stays in the durable regime ───────────────────────────────────────────
// Reuses the REAL stability types + threshold (stability.NMargin). Zoomable Memory bears on two of
// the durability conditions: U<=1 (the working set is schedulable — it never exceeds the budget) and
// n<1 (the thought graph's branching is subcritical). The remaining conditions (regulator gain,
// async dead-time, awake baseline mu>0) belong to the engine's regulator, not this memory layer.
func TestDataset_T4_Stability(t *testing.T) {
	for _, ep := range loadEpisodes(t) {
		budget := episodeBudget(ep.Units)
		peakU := 0.0
		for n := 1; n <= len(ep.Units); n++ {
			ctx := Assemble(ep.Units[:n], ep.Units[n-1].ID, budget)
			if u := float64(ctx.Total) / float64(budget); u > peakU {
				peakU = u
			}
		}
		branches := map[int]bool{}
		for _, u := range ep.Units {
			branches[u.Branch] = true
		}
		n := float64(len(branches)-1) / float64(len(ep.Units)) // forks per thought

		// The two durability conditions Zoomable Memory bears on (the rest belong to the engine's
		// regulator). nMargin mirrors stability.NMargin (the n<1 subcritical cliff) inline, so this
		// memory-layer test does not import the stability suite (which imports the engine — a cycle now
		// that the engine wires zoommem in, P4.1).
		const nMargin = 1.0
		uOK := peakU <= 1.0+1e-9 // working set schedulable (never exceeds the budget)
		nOK := n < nMargin       // subcritical branching
		t.Logf("zoommem/%s: U<=1 peak=%.2f (%v) · n<1 n=%.2f (%v)", ep.Name, peakU, uOK, n, nOK)
		if !uOK || !nOK {
			t.Errorf("[%s] zoomable-memory dynamics left the durable regime", ep.Name)
		}
	}
}
