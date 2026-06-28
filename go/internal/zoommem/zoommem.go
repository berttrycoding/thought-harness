// Package zoommem is a THROWAWAY prototype for "Zoomable Memory" (Test 1 in
// docs/internal/archive/reports/registry-redesign-worklog.md). It is intentionally standalone — it imports NOTHING from the
// engine/graph, so it cannot destabilise the main build — and it is fully deterministic and offline
// (no model), so the test is reproducible.
//
// The idea it demonstrates: every thought is kept in FULL forever; at each moment a controller shows
// each thought at one of several ZOOM LEVELS (full -> committed thought -> one-liner -> tag -> bare
// pointer) so the assembled working-context fits a token budget while staying coherent (the focus and
// its neighbours stay sharp, relevant older thoughts are surfaced, nothing is silently dropped).
//
// In the real system the shorter forms would be model rewrites; here they are produced by CUTTING
// (extractive, deterministic) — which is enough to test the *controller* (selection + budget), which
// is what Test 1 is about.
package zoommem

import (
	"fmt"
	"sort"
	"strings"
)

// Level is how zoomed-in a thought is shown. Smaller == richer/bigger.
type Level int

const (
	L0Full     Level = iota // the full raw thinking
	L1Thought               // the committed 1-2 sentence thought
	L2OneLiner              // a single short sentence — the point
	L3Tag                   // a few words / a label
	L4Pointer               // not shown — just the id (full text shelved, fetchable)
)

func (l Level) String() string { return [...]string{"L0", "L1", "L2", "L3", "L4"}[l] }

// scoring weights — relevance leads so a relevant OLD thought can beat an irrelevant RECENT one
// (that is the coherence property Test 1 checks). Tunable later by the control/ML layer.
const (
	wRelevance = 0.50
	wRecency   = 0.25
	wAdjacency = 0.25

	shelvedLineCost = 4 // the single folded "+N shelved" line, reserved so the budget never overflows

	retrievalReservePct = 30 // share of the post-focus budget set aside for relevant cross-branch memories
)

// Unit is one thought, kept in FULL forever. Shorter forms are derived on demand — nothing is lost.
type Unit struct {
	ID       int
	Branch   int
	Parent   int // -1 == root
	Tick     int
	Full     string   // L0
	Thought  string   // L1 (the committed thought)
	Entities []string // salient terms, a stronger relevance cue than raw word overlap (optional)
}

// render returns the unit's text at a zoom level (L2/L3 by extractive cutting).
func (u Unit) render(l Level) string {
	switch l {
	case L0Full:
		return u.Full
	case L1Thought:
		return u.Thought
	case L2OneLiner:
		return firstWords(u.Thought, 8)
	case L3Tag:
		return firstWords(u.Thought, 4)
	default: // L4Pointer
		return fmt.Sprintf("#%d", u.ID)
	}
}

// size approximates token cost by word count (good enough for a prototype budget).
func (u Unit) size(l Level) int {
	if l == L4Pointer {
		return 1
	}
	return len(strings.Fields(u.render(l)))
}

// Shown is one unit displayed at a chosen level.
type Shown struct {
	Unit  Unit
	Level Level
}

// Text is the unit's text at its chosen zoom level — the exported accessor a consumer (the engine's
// working-context wiring) uses to read the compressed text without reaching into render.
func (s Shown) Text() string { return s.Unit.render(s.Level) }

// Context is the assembled working-memory snapshot for one tick.
type Context struct {
	Shown   []Shown // in display order (oldest..newest)
	Shelved []int   // unit ids shown only as pointers (folded into one line)
	Total   int     // word-size of everything shown (incl. the folded shelved line)
	Budget  int
}

// Assemble is the controller: it picks a zoom level for every unit so the working context fits the
// budget and stays coherent. Policy: start everyone shelved (a pointer — never silently dropped), pin
// the focus sharp, then upgrade the rest by score (relevance + recency + adjacency), each capped by a
// structural rule so one old unit can't eat the whole budget. Bounded by construction: Total <= Budget.
func Assemble(units []Unit, focusID, budget int) Context {
	return assembleWith(units, focusID, budget, capFor)
}

// assembleWith is Assemble parameterised by the cap policy (capFn returns the richest level a unit may
// reach). The default is the structural capFor; T5 passes a kind-aware policy.
func assembleWith(units []Unit, focusID, budget int, capFn func(u, focus Unit) Level) Context {
	if len(units) == 0 {
		return Context{Budget: budget}
	}
	focus := find(units, focusID)
	goal := units[0].Thought // the root thought stands in for the episode goal
	maxTick := 0
	for _, u := range units {
		if u.Tick > maxTick {
			maxTick = u.Tick
		}
	}

	level := make(map[int]Level, len(units))
	for _, u := range units {
		level[u.ID] = L4Pointer
	}

	remaining := budget - shelvedLineCost // reserve the folded shelved line up front
	if remaining < 0 {
		remaining = 0
	}

	// pin the focus first — it must stay sharp (never fade below a one-liner).
	fLevel := L2OneLiner
	for lv := L0Full; lv <= L2OneLiner; lv++ {
		if focus.size(lv) <= remaining {
			fLevel = lv
			break
		}
	}
	level[focus.ID] = fLevel
	remaining -= focus.size(fLevel)
	if remaining < 0 {
		remaining = 0
	}

	// RETRIEVAL RESERVE — set aside a slice of the budget for the most-relevant CROSS-BRANCH memories,
	// so keeping the active branch sharp (coherence) doesn't starve retrieval. Without this, recent
	// same-branch thoughts (adjacency bonus + bigger slot) crowd out relevant far thoughts entirely.
	reserve := remaining * retrievalReservePct / 100
	far := append([]Unit(nil), units...)
	sort.SliceStable(far, func(i, j int) bool {
		ri, rj := relevanceOnly(far[i], focus, goal), relevanceOnly(far[j], focus, goal)
		if ri != rj {
			return ri > rj
		}
		return far[i].ID < far[j].ID
	})
	for _, u := range far {
		if u.Branch == focus.Branch || u.ID == focus.ID {
			continue
		}
		if relevanceOnly(u, focus, goal) <= 0 {
			break // only spend the reserve on genuinely relevant memories
		}
		if cost := u.size(L2OneLiner); cost <= reserve {
			reserve -= cost
			remaining -= cost
			level[u.ID] = L2OneLiner
		}
	}

	// upgrade the rest by score, richest-allowed-first, while it fits (skip anything already placed).
	order := append([]Unit(nil), units...)
	sort.SliceStable(order, func(i, j int) bool {
		si, sj := score(order[i], focus, goal, maxTick), score(order[j], focus, goal, maxTick)
		if si != sj {
			return si > sj
		}
		return order[i].ID < order[j].ID
	})
	for _, u := range order {
		if level[u.ID] != L4Pointer { // focus + reserved units already have a slot
			continue
		}
		for lv := capFn(u, focus); lv <= L3Tag; lv++ {
			if cost := u.size(lv); cost <= remaining {
				remaining -= cost
				level[u.ID] = lv
				break
			}
		}
	}

	// emit in display (tick) order.
	disp := append([]Unit(nil), units...)
	sort.SliceStable(disp, func(i, j int) bool { return disp[i].Tick < disp[j].Tick })
	ctx := Context{Budget: budget}
	for _, u := range disp {
		if level[u.ID] == L4Pointer {
			ctx.Shelved = append(ctx.Shelved, u.ID)
			continue
		}
		ctx.Shown = append(ctx.Shown, Shown{Unit: u, Level: level[u.ID]})
		ctx.Total += u.size(level[u.ID])
	}
	if len(ctx.Shelved) > 0 {
		ctx.Total += shelvedLineCost
	}
	return ctx
}

// capFor enforces the coherence SHAPE: the focus may be full; its neighbours may reach the committed
// thought; everything else caps at a one-liner. This keeps the context readable.
func capFor(u, focus Unit) Level {
	switch {
	case u.ID == focus.ID:
		return L0Full
	case u.Branch == focus.Branch, u.ID == focus.Parent, focus.ID == u.Parent:
		return L1Thought
	default:
		return L2OneLiner
	}
}

// score rates how much a unit deserves DETAIL now: relevant to the goal/focus, recent, and near the
// focus all push it up.
func score(u, focus Unit, goal string, maxTick int) float64 {
	recency := 0.0
	if maxTick > 0 {
		recency = float64(u.Tick) / float64(maxTick)
	}
	return wRelevance*relevanceOnly(u, focus, goal) + wRecency*recency + wAdjacency*adjacency(u, focus)
}

// relevanceOnly is how on-topic u is to the current goal+focus: the STRONGER of raw word overlap on
// the gist and entity overlap (entities can only help, never dilute). NOTE: a grep over the candidate's
// FULL text was tried and REGRESSED (66%->53%) — a 120-word paragraph mentions too many terms in
// passing, so the compressed gist is a cleaner matching surface. The next channel is a small-LLM
// rerank (bridges synonyms the lexical signal can't), not more lexical matching.
func relevanceOnly(u, focus Unit, goal string) float64 {
	rel := overlap(u.Thought, goal+" "+focus.Thought)
	if len(u.Entities) > 0 && len(focus.Entities) > 0 {
		if j := jaccard(u.Entities, focus.Entities); j > rel {
			rel = j
		}
	}
	return rel
}

// jaccard is the overlap of two term sets (|A∩B| / |A∪B|), case-insensitive.
func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	sa := map[string]bool{}
	for _, x := range a {
		sa[strings.ToLower(x)] = true
	}
	sb := map[string]bool{}
	inter := 0
	for _, y := range b {
		y = strings.ToLower(y)
		if sb[y] {
			continue
		}
		sb[y] = true
		if sa[y] {
			inter++
		}
	}
	if union := len(sa) + len(sb) - inter; union > 0 {
		return float64(inter) / float64(union)
	}
	return 0
}

func adjacency(u, focus Unit) float64 {
	switch {
	case u.ID == focus.ID:
		return 1.0
	case u.Branch == focus.Branch:
		return 0.8
	case u.ID == focus.Parent, focus.ID == u.Parent:
		return 0.6
	default:
		return 0.15
	}
}

// overlap is the fraction of a's content words that also appear in b (lexical relevance, offline).
func overlap(a, b string) float64 {
	aw := contentSet(a)
	if len(aw) == 0 {
		return 0
	}
	bw := contentSet(b)
	hit := 0
	for w := range aw {
		if bw[w] {
			hit++
		}
	}
	return float64(hit) / float64(len(aw))
}

func contentSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if clean := strings.Trim(w, ".,!?;:'\"()"); len(clean) > 3 {
			m[clean] = true
		}
	}
	return m
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

func find(units []Unit, id int) Unit {
	for _, u := range units {
		if u.ID == id {
			return u
		}
	}
	return units[0]
}

// Render is a human-readable snapshot of an assembled context (for the eyeball test).
func Render(ctx Context) string {
	var b strings.Builder
	for _, s := range ctx.Shown {
		fmt.Fprintf(&b, "  [%s b%d #%-2d] %s\n", s.Level, s.Unit.Branch, s.Unit.ID, s.Unit.render(s.Level))
	}
	if len(ctx.Shelved) > 0 {
		fmt.Fprintf(&b, "  [L4 shelved ] +%d older thoughts, fetchable by id %v\n", len(ctx.Shelved), ctx.Shelved)
	}
	u := 0.0
	if ctx.Budget > 0 {
		u = float64(ctx.Total) / float64(ctx.Budget)
	}
	fmt.Fprintf(&b, "  -- %d/%d words (U=%.2f) | %d shown, %d shelved\n",
		ctx.Total, ctx.Budget, u, len(ctx.Shown), len(ctx.Shelved))
	return b.String()
}

// CookDinnerSession is a scripted ~17-thought episode: a planning line that splits into a recipe
// branch (b1) and a logistics branch (b2), with the focus moving around. #3 plants salmon/lemon/butter
// (relevant later); #15 plants an irrelevant-recent aside; #16 is the shopping list (the relevance
// probe). Ticks == ids.
func CookDinnerSession() []Unit {
	s := []Unit{
		{0, 0, -1, 0, "I want to cook a really nice dinner for a few friends coming over tonight, something memorable but relaxed.", "cook a nice memorable dinner for friends coming over tonight", []string{"dinner", "friends"}},
		{1, 0, 0, 1, "Let me break this down into the two things that actually matter: choosing the dish and sorting out the ingredients.", "break it into choosing the dish and sorting the ingredients", []string{"dish", "ingredients"}},
		{2, 0, 1, 2, "So there are two open questions to settle in parallel: which recipe to cook, and where to buy what I need.", "two questions: which recipe to cook and where to buy things", []string{"recipe", "buy"}},
		{3, 1, 2, 3, "For the main course I am leaning toward pan-seared salmon with a lemon butter sauce, it feels special but is quick.", "the main is pan-seared salmon with lemon butter sauce", []string{"salmon", "lemon", "butter", "sauce"}},
		{4, 1, 3, 4, "Salmon is a good call because it cooks fast, looks impressive on the plate, and most guests are happy with fish.", "salmon cooks fast looks impressive and most guests like fish", []string{"salmon", "fish"}},
		{5, 1, 4, 5, "I should pair the fish with a light side, maybe roasted asparagus and some small baby potatoes on the side.", "pair the salmon with roasted asparagus and baby potatoes", []string{"asparagus", "potatoes"}},
		{6, 1, 5, 6, "For dessert keep it simple and on theme, a lemon tart would echo the lemon in the salmon sauce nicely.", "dessert is a simple lemon tart to echo the sauce", []string{"dessert", "lemon", "tart"}},
		{7, 0, 2, 7, "Alright, the dish is roughly decided, so now I can switch over and think about the logistics of actually shopping.", "the dish is decided, now think about the shopping logistics", []string{"shopping", "logistics"}},
		{8, 2, 7, 8, "The first problem is where I actually buy fresh fish this late in the afternoon without it being picked over.", "where do I buy fresh fish this late in the afternoon", []string{"fish", "buy"}},
		{9, 2, 8, 9, "The fish counter at the market closes at six, so I genuinely need to leave fairly soon to make it in time.", "the fish counter closes at six so I must leave soon", []string{"fish", "market", "time"}},
		{10, 2, 9, 10, "The regular grocery store nearby has the vegetables and the dairy I need for the sauce and the side dishes.", "the grocery has the vegetables and dairy for the sauce", []string{"grocery", "vegetables", "dairy"}},
		{11, 2, 10, 11, "I can pick up butter and cream and a few lemons at that same grocery in a single convenient trip.", "get butter cream and lemons at the grocery in one trip", []string{"butter", "cream", "lemon", "grocery"}},
		{12, 0, 7, 12, "On timing: I should shop first, then make the sauce, and sear the salmon at the very last minute before serving.", "timing: shop first, make sauce, sear salmon just before serving", []string{"salmon", "sauce", "timing"}},
		{13, 0, 12, 13, "I have roughly three hours before guests arrive, which is enough time if I get the shopping started right now.", "about three hours left, enough if I start shopping now", []string{"shopping", "time"}},
		{14, 1, 6, 14, "Actually I want to double-check the salmon portion size per person so I buy the right amount of fillets.", "double-check the salmon portion size so I buy enough fillets", []string{"salmon", "fillets"}},
		{15, 0, 13, 15, "Random aside, I wonder if it might rain tomorrow for the weekend hike I have loosely planned with others.", "wonder if it might rain tomorrow for the weekend hike", []string{"rain", "hike", "weekend"}},
		{16, 2, 11, 16, "Let me write the actual shopping list now: salmon fillets, lemon, butter, cream, asparagus, and potatoes.", "shopping list: salmon lemon butter cream asparagus potatoes", []string{"salmon", "lemon", "butter", "cream", "asparagus", "potatoes"}},
	}
	return s
}
