// skills.go ports skills.py — the reusable capability layer over operators.
//
// REFRAME (GAP 8, §3.8, locked 2026-06-14) — flag-gated by `convert.skill_reframe` (SetReframe),
// default OFF ⇒ everything below is byte-identical:
//   - LEGACY (default): a Skill's body IS a `Program`; it is goal-MATCHED (MatchScore/Match); sub-skill
//     calls are resolved at BUILD time by Expand into a pure-operator Program. This is the W5-validated
//     mint/recall flywheel the scenario goldens anchor — described next and left untouched when OFF.
//   - REFRAMED (flag ON): a Skill's executable body is a PROMPT + sub-skill REFERENCES, resolved at RUN
//     time by ResolveBody (NOT build-time Expand), and a Skill does NOT match goals (Match returns
//     nothing — relevance/selection is the Capability's job; "goal-matched is retired"). The legacy
//     Program body remains the fallback shape so flipping the flag does not half-break the flywheel.
//
// A *Skill* is a named, reusable capability. In the LEGACY shape it CONVERGES with the Program substrate:
// a skill's body IS a Program (operators in series/parallel/loop), so a matched skill flows through the
// exact same Workflow->Operator->SubAgent machinery as a freshly-synthesised program. Two tiers:
//
//   - unit skill         a one-operator body (the leaf capability)
//   - higher-level skill  a Program that may call sub-skills (a Step whose operator is "skill:<name>"),
//     composed like lathe's /sprint over its chain skills.
//
// Sub-skill calls are resolved at BUILD time by SkillRegistry.Expand into a pure-operator Program,
// BOUNDED (depth <= MaxSkillDepth) and ACYCLIC (a skill that transitively calls itself is rejected). So
// the executed artifact is always a bounded operator Program — which the durability analysis already
// proves stable. Composition adds no new runtime excitation; the only new stability obligation is
// acyclic, depth-bounded expansion, enforced here + checked by the stability suite.
//
// Skills are matched to goals (cheap trigger match here; embedding/LLM are future tiers), minted on
// demand (a synthesised Program promoted to a named skill — the trace->skill convertibility), verified
// before trust, and every mint recorded as data.
//
// HARD PORT #3 (skill expansion). Python raises ValueError on a cycle / over-deep nesting / unknown
// sub-skill; the Go port returns an error from Expand (raise->return error). The build-time Expand is a
// recursive Node walk resolving "skill:<name>", carrying a []string visited stack to reject cycles, a
// MaxSkillDepth=3 bound, and returning a bounded acyclic pure-operator Program. The seed library is a
// builder func (_seedSkills) over the program.go builder DSL.
package cognition

import (
	"errors"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// SkillPrefix marks a Step operator as a sub-skill call ("skill:<name>"). Mirrors Python SKILL_PREFIX.
const SkillPrefix = "skill:"

// MaxSkillDepth bounds how deep sub-skill composition may nest (bounds build-time expansion). Mirrors
// Python MAX_SKILL_DEPTH = 3.
const MaxSkillDepth = 3

// SkillStep builds a Step that calls a sub-skill (resolved at build time by SkillRegistry.Expand).
// Mirrors Python skill_step(name, domain="general", note="").
func SkillStep(name, domain, note string) Step {
	return Step{Operator: SkillPrefix + name, Domain: domain, Note: note}
}

// IsSkillCall reports whether a Step's operator names a sub-skill ("skill:<name>"). Mirrors Python
// is_skill_call(s).
func IsSkillCall(s Step) bool { return strings.HasPrefix(s.Operator, SkillPrefix) }

// CalledSkill returns the sub-skill name a skill-call Step targets (the part after "skill:"). Mirrors
// Python called_skill(s) = s.operator[len(SKILL_PREFIX):].
func CalledSkill(s Step) string { return s.Operator[len(SkillPrefix):] }

// Skill is a named, matchable, reusable capability. Mirrors the Python @dataclass Skill; Triggers is the
// (legacy) goal-matcher (cheap keyword/phrase match), Description and Synthesized default to ""/false.
//
// REFRAME (GAP 8, §3.8, locked 2026-06-14) — two body shapes, flag-discriminated:
//   - LEGACY (the default, byte-identical, flag-OFF): Body is a `Program` (operators in series/parallel/
//     loop). Sub-skill calls are resolved at BUILD time by SkillRegistry.Expand into a pure-operator
//     Program. A skill self-matches goals via MatchScore. This is what the W5-validated mint/recall
//     flywheel runs and the scenario goldens anchor.
//   - REFRAMED (flag-ON, `convert.skill_reframe`): the executable shape is a PROMPT + sub-skill
//     REFERENCES (Prompt + SubSkillRefs), resolved at RUN time by ResolveBody (NOT build-time Expand),
//     keeping the acyclic/depth-3 guard. A reframed Skill does NOT self-match goals — that is the
//     Capability's job; "goal-matched is retired" (§3.8). The legacy Program Body is kept as the
//     fallback shape under the flag so the flywheel is unchanged when off.
//
// Prompt + SubSkillRefs are ADDITIVE fields: they default to ""/nil, so a Skill constructed the legacy
// way (every seed + every minted skill today) is byte-identical and IsReframed() is false. The reframe
// is opt-in per registry (SetReframe) AND per skill (a non-empty Prompt).
type Skill struct {
	Name        string
	Tier        string   // "unit" | "composite"
	Triggers    []string // the LEGACY goal-matcher (cheap keyword/phrase match); ignored on the reframe path
	Body        Program  // LEGACY body: a Program (may contain skill: sub-calls if composite)
	Description string   // default ""
	Synthesized bool     // minted at runtime vs seeded; default false

	// REFRAME (GAP 8) — the reframed body. Both default empty (legacy skill ⇒ IsReframed()==false).
	Prompt       string   // the worker PROMPT this skill resolves to (the Pattern-B content the SubAgent runs)
	SubSkillRefs []string // names of sub-skills the prompt calls (resolved at RUN time, acyclic, depth<=MaxSkillDepth)
}

// IsReframed reports whether this Skill carries the reframed (prompt + sub-skill-ref) shape — i.e. a
// non-empty Prompt. A legacy Program-bodied skill (every seed + every minted skill today) is NOT
// reframed, so the legacy path is byte-identical until a reframed skill is explicitly built.
func (s Skill) IsReframed() bool { return strings.TrimSpace(s.Prompt) != "" }

// MatchScore is the cheap keyword-overlap score of this skill against a goal text: the fraction of
// triggers that appear (case-insensitively) in the goal, or 0 if none fire. Mirrors Python
// Skill.match_score: hits / (len(triggers) or 1) if hits else 0.0. An empty trigger list divides by 1
// (and yields 0 since no trigger can fire), reproducing the `or 1` guard.
func (s Skill) MatchScore(goalText string) float64 {
	t := strings.ToLower(goalText)
	hits := 0
	for _, kw := range s.Triggers {
		if strings.Contains(t, kw) {
			hits++
		}
	}
	if hits == 0 {
		return 0.0
	}
	denom := len(s.Triggers)
	if denom == 0 {
		denom = 1
	}
	return float64(hits) / float64(denom)
}

// TierLevel maps the Skill's string Tier onto the numeric skill-tier coordinate of the §3.3a Scope facet
// (the #31 Skill reframe — a Skill is a goal-matched capability over operators, and its tier is the
// authority coordinate a Scope ceiling caps): unit -> 1 (a leaf capability), composite -> 2 (it composes
// others). An unknown/empty tier is treated as 1 (the most permissive — a leaf).
func (s Skill) TierLevel() int {
	if strings.EqualFold(s.Tier, "composite") {
		return 2
	}
	return 1
}

// WithinTierCeiling reports whether this Skill is admissible under a numeric tier ceiling (§3.3a / §3.8a:
// the tier from Goal.Level). A ceiling of 0 is uncapped (no tier bound). A composite skill needs a higher
// ceiling than a unit skill — so a shallow goal cannot staff a deep, composing capability.
func (s Skill) WithinTierCeiling(ceiling int) bool {
	return ceiling == 0 || s.TierLevel() <= ceiling
}

// SubSkills lists the names of the sub-skills this skill's body calls (the "skill:<name>" Steps).
// Mirrors Python Skill.sub_skills().
func (s Skill) SubSkills() []string {
	var out []string
	for _, st := range s.Body.Steps() {
		if IsSkillCall(st) {
			out = append(out, CalledSkill(st))
		}
	}
	return out
}

// SkillRegistry is an open registry of skills: seeded, matchable, and extensible at runtime. Mirrors
// Python SkillRegistry. Python is single-threaded; the registry has no lock there. The Go port keeps
// it lock-free as well (expansion/matching are read-only walks; minting is engine-serialised, like the
// other Tier-3 generative state) — added a mutex only if a concurrent caller appears. The map preserves
// nothing order-sensitive that Python relies on beyond the seed builder order for matching ties.
type SkillRegistry struct {
	skills   map[string]Skill
	minted   []string           // names minted this run (accumulate -> reuse)
	embedder retrieval.Embedder // optional semantic channel for Match (nil -> lexical-only, the default)
	reframe  bool               // GAP 8: the Skill reframe (default OFF ⇒ legacy Program/goal-match path, byte-identical)
}

// SetEmbedder wires an optional semantic channel into Match (P1.3): when set, Match fuses a lexical
// trigger-overlap ranking with an embedding-cosine ranking via reciprocal-rank fusion, so a goal that
// paraphrases a skill's purpose recalls it even with no shared trigger words. nil (the default) keeps
// Match purely lexical — the offline path and every scenario golden are unchanged.
func (r *SkillRegistry) SetEmbedder(e retrieval.Embedder) { r.embedder = e }

// SetReframe flips the GAP-8 Skill reframe (`convert.skill_reframe`) on a registry — the runtime-setter
// injection the engine drives from config (mirrors SetEmbedder; cognition never imports config). Default
// OFF (every constructor leaves it false) ⇒ the legacy Program body + build-time Expand + goal-matching
// Match all run unchanged, byte-identical. When ON: Match no longer self-matches goals (a skill does NOT
// match goals — that is the Capability's job, §3.8) and the runtime resolver (ResolveBody) is the
// executable path for reframed (prompt-bodied) skills. The legacy Program shape remains the fallback for
// skills that carry no Prompt, so the mint/recall flywheel is not half-broken by flipping this on.
func (r *SkillRegistry) SetReframe(on bool) { r.reframe = on }

// Reframed reports whether the GAP-8 reframe is active on this registry (test/observability accessor).
func (r *SkillRegistry) Reframed() bool { return r.reframe }

// NewSkillRegistry builds a registry, seeding it from the seed library when seed is true. Mirrors
// Python SkillRegistry.__init__(seed=True).
func NewSkillRegistry(seed bool) *SkillRegistry {
	r := &SkillRegistry{skills: make(map[string]Skill)}
	if seed {
		for _, s := range seedSkills() {
			r.skills[s.Name] = s
		}
	}
	return r
}

// -- access ------------------------------------------------------------------ //

// Get returns the skill for name and ok=true, or the zero Skill and ok=false if absent. Mirrors Python
// get(name) -> Skill | None (the bool replaces the None signal).
func (r *SkillRegistry) Get(name string) (Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// Has reports whether name is registered. Mirrors Python has(name).
func (r *SkillRegistry) Has(name string) bool {
	_, ok := r.skills[name]
	return ok
}

// Names returns the registered skill names in seed-builder order (then minted in mint order), so the
// order is deterministic rather than the unordered Go map iteration. Python's list(self._skills) is
// dict-insertion order (seed order then mint order); this reconstructs it.
func (r *SkillRegistry) Names() []string {
	out := make([]string, 0, len(r.skills))
	seen := make(map[string]struct{}, len(r.skills))
	for _, s := range seedSkills() {
		if _, ok := r.skills[s.Name]; ok {
			if _, dup := seen[s.Name]; !dup {
				out = append(out, s.Name)
				seen[s.Name] = struct{}{}
			}
		}
	}
	for _, name := range r.minted {
		if _, ok := r.skills[name]; ok {
			if _, dup := seen[name]; !dup {
				out = append(out, name)
				seen[name] = struct{}{}
			}
		}
	}
	return out
}

// Composite returns the registered composite-tier skills in seed-builder order (then minted). Mirrors
// Python composite() -> [s for s in self._skills.values() if s.tier == "composite"], preserving
// insertion order via the same reconstruction as Names.
func (r *SkillRegistry) Composite() []Skill {
	var out []Skill
	for _, name := range r.Names() {
		if s := r.skills[name]; s.Tier == "composite" {
			out = append(out, s)
		}
	}
	return out
}

// -- matching (goal -> skill) ------------------------------------------------ //

// Match returns the best skill whose triggers fire for this goal (cheap tier), or ok=false if none score
// above zero. The matchable set is the composed capabilities UNION the learned skills —
// matchable(s) := s.Tier=="composite" || s.Synthesized — NOT a raw tier-string equality. This is the fix
// that closes the mint->recall convertibility loop: a runtime-minted skill is promoted with an EMPTY
// tier (engine.skillMinter.Mint passes tier="") but always Synthesized=true, so the old composite-only
// scan (r.Composite(), tier=="composite") made every minted skill structurally unrecallable — minting
// could never pay off. Keying on Synthesized lets a learned skill be recalled while leaving the seed
// *unit* skills as expansion building-blocks (not directly matched), which preserves the seed routing
// the scenario goldens anchor. Iterating in Names() order (seed-builder order, then mint order) keeps the
// strict `sc > bestScore` tie-break deterministic — the FIRST skill at a given best score wins.
func (r *SkillRegistry) Match(goalText string) (Skill, bool) {
	// REFRAME (GAP 8): a Skill does NOT match goals — relevance/selection is the Capability's job; the
	// "goal-matched skill" framing is retired (§3.8). When the reframe is ON, the skill-self-matcher is
	// disabled (returns no match), so a worker invokes skills by category, never by scanning goals. When
	// OFF (the default) the legacy lexical/hybrid goal-matcher below runs unchanged (byte-identical).
	if r.reframe {
		return Skill{}, false
	}
	// LEGACY(redesign): everything below is the convert.skill_reframe OFF-path — the Skill's own lexical/
	// hybrid goal-self-match (the "goal-matched skill" framing the reframe retires; relevance/selection
	// becomes the Capability's job) — removable when the 4 redesign flags are retired (Match then always
	// returns no match; the Capability's MatchRecallableWithinTier is the only goal→skill recall).
	// matchable = composed capabilities ∪ learned skills (see the matchable-set note above), in the
	// deterministic Names() order.
	var cands []Skill
	for _, name := range r.Names() {
		s := r.skills[name]
		if s.Tier == "composite" || s.Synthesized {
			cands = append(cands, s)
		}
	}
	if r.embedder != nil {
		if s, ok := r.matchHybrid(goalText, cands); ok {
			return s, ok
		}
		// hybrid found nothing confident -> fall through to the lexical argmax below.
	}
	var best Skill
	bestScore := 0.0
	found := false
	for _, s := range cands {
		sc := s.MatchScore(goalText)
		if sc > bestScore {
			best, bestScore, found = s, sc, true
		}
	}
	return best, found
}

// MatchWithinTier is the §3.3a-ceiling-bounded match (#31 Skill reframe): like Match, but a candidate is
// only considered when its tier is within the numeric ceiling (WithinTierCeiling) — so a goal at a shallow
// Goal.Level cannot staff a deep composite capability. ceiling==0 is uncapped (identical to Match). Lexical
// argmax only (the tier filter is the point; the embedder path is left to Match), deterministic.
func (r *SkillRegistry) MatchWithinTier(goalText string, ceiling int) (Skill, bool) {
	// REFRAME (GAP 8): the goal→skill match is retired when the reframe is ON (a skill does not match
	// goals; the Capability does). Default OFF ⇒ the legacy ceiling-bounded matcher below runs unchanged.
	if r.reframe {
		return Skill{}, false
	}
	// LEGACY(redesign): everything below is the convert.skill_reframe OFF-path — the ceiling-bounded lexical
	// goal-self-match (the legacy goal-matcher the reframe retires) — removable when the 4 redesign flags are
	// retired (MatchWithinTier then always returns no match; the Capability owns ceiling-bounded recall).
	var best Skill
	bestScore := 0.0
	found := false
	for _, name := range r.Names() {
		s := r.skills[name]
		if !(s.Tier == "composite" || s.Synthesized) {
			continue
		}
		if !s.WithinTierCeiling(ceiling) {
			continue // outside the authority band — a worker cannot staff above the ceiling
		}
		if sc := s.MatchScore(goalText); sc > bestScore {
			best, bestScore, found = s, sc, true
		}
	}
	return best, found
}

// MatchReframedWithinTier is the REFRAME-PATH goal→skill matcher the CAPABILITY owns (GAP-8 remaining
// wire / gap-5-deeper). Where Match / MatchWithinTier deliberately return NOTHING under the reframe (a
// Skill no longer self-matches goals — §3.8), this is the recall the reframe RETIRED, now exposed for
// the ONE caller the redesign moves relevance onto: the Capability. It is INERT unless the reframe is on
// (returns ok=false when r.reframe is false), so the legacy Match/MatchWithinTier semantics are
// completely unchanged and the W5 mint/recall flywheel (flag OFF) is byte-identical — this method is
// dead code on the legacy path.
//
// It scans only REFRAMED skills (IsReframed() — a non-empty Prompt) within the numeric tier ceiling
// (WithinTierCeiling; ceiling==0 is uncapped), lexical-argmax by MatchScore, iterating in the
// deterministic Names() order so the strict `sc > bestScore` tie-break is stable (the first reframed
// skill at the best score wins). A legacy (Program-bodied) skill is never a candidate here — the two
// shapes never cross. ok=false means no reframed skill fired (the Capability falls through to Synthesize).
func (r *SkillRegistry) MatchReframedWithinTier(goalText string, ceiling int) (Skill, bool) {
	if !r.reframe {
		return Skill{}, false // inert off the reframe path — the legacy goal-matcher (Match) is the path then
	}
	var best Skill
	bestScore := 0.0
	found := false
	for _, name := range r.Names() {
		s := r.skills[name]
		if !s.IsReframed() {
			continue // only reframed (prompt-bodied) skills are recallable on this path
		}
		if !s.WithinTierCeiling(ceiling) {
			continue // outside the authority band — a worker cannot staff above the ceiling (§3.3a)
		}
		if sc := s.MatchScore(goalText); sc > bestScore {
			best, bestScore, found = s, sc, true
		}
	}
	return best, found
}

// MatchRecallableWithinTier is the REFRAME-PATH recall the CAPABILITY actually owns (gap-8 remaining-wire
// FIX): the inclusive form of MatchReframedWithinTier. The reframe retired the Skill's own goal-self-match
// (Match / MatchWithinTier return nothing under reframe — relevance is the Capability's job), but it must
// recall EVERY recallable skill shape, not just the reframed (prompt-bodied) ones. The seed library (the
// M5 analogy/induction/deduction paths, the design-feature/diagnose/… composites) and EVERY trace->skill
// or W5 minted skill are LEGACY Program-bodied skills (IsReframed()==false) — they are never minted in
// reframed form, and the engine's warm-reload + convert.Consolidate mint paths produce Program bodies. So
// a reframed-ONLY recall (MatchReframedWithinTier) leaves the entire seed + minted library un-recallable,
// every recurring goal re-synthesises from scratch, and the recall short-circuit dies (peak_n excitation +
// the M5/flywheel/campaign recall gates fail). This method recalls BOTH shapes:
//
//   - a REFRAMED skill (IsReframed()) — recalled exactly as MatchReframedWithinTier did; OR
//   - a LEGACY MATCHABLE skill — the SAME matchable set Match keys on (Tier=="composite" || Synthesized),
//     so the seed composites + path skills + every minted/learned skill are candidates, while the seed
//     UNIT skills stay expansion building-blocks (not directly recalled), preserving the seed routing the
//     scenario goldens anchor.
//
// Within the numeric tier ceiling (WithinTierCeiling; ceiling==0 uncapped), lexical-argmax by MatchScore,
// iterating in the deterministic Names() order so the strict `sc > bestScore` tie-break is stable. It is
// INERT off the reframe path (returns ok=false when r.reframe is false) — the legacy Match flywheel runs
// then, byte-identical. The caller (Capability.recallReframed) dispatches body resolution on IsReframed()
// (ResolveBody for a reframed body, Expand for a legacy Program body). Deterministic: no RNG/clock.
func (r *SkillRegistry) MatchRecallableWithinTier(goalText string, ceiling int) (Skill, bool) {
	if !r.reframe {
		return Skill{}, false // inert off the reframe path — the legacy goal-matcher (Match) is the path then
	}
	var best Skill
	bestScore := 0.0
	found := false
	for _, name := range r.Names() {
		s := r.skills[name]
		// recallable = a reframed (prompt-bodied) skill OR a legacy MATCHABLE skill (the Match set:
		// composed capabilities ∪ learned skills). The seed unit skills stay expansion-only (not recalled).
		// LEGACY(redesign): the `s.Tier == "composite" || s.Synthesized` disjuncts admit legacy Program-bodied
		// skills onto the reframe-path recall (load-bearing — the whole seed/W5 library is Program-bodied, so
		// without them the reframe-ON path recalls nothing) — removable when the seed library is migrated to
		// reframed form (gap-8 follow-up) (the `s.IsReframed()` disjunct then suffices).
		if !(s.IsReframed() || s.Tier == "composite" || s.Synthesized) {
			continue
		}
		if !s.WithinTierCeiling(ceiling) {
			continue // outside the authority band — a worker cannot staff above the ceiling (§3.3a)
		}
		if sc := s.MatchScore(goalText); sc > bestScore {
			best, bestScore, found = s, sc, true
		}
	}
	return best, found
}

// semFloor is the cosine above which an embedding match is "genuinely on-topic" (nomic/qwen sentence
// embeddings sit ~0.3–0.5 for unrelated text, so a confident paraphrase match clears ~0.6).
const semFloor = 0.6

// matchHybrid is the semantic-augmented match (P1.3): it RRF-fuses a lexical trigger-overlap ranking
// with an embedding-cosine ranking over the matchable skills, and returns the fused winner ONLY when it
// carries genuine signal — a positive lexical score OR a cosine above semFloor. That guard stops the
// always-positive cosine baseline from manufacturing a spurious match when nothing is on-topic (in
// which case Match falls back to the lexical argmax, which may itself return nothing). ok=false means
// "no confident hybrid match", never a panic — a mid-run embedder error degrades to lexical.
func (r *SkillRegistry) matchHybrid(goalText string, cands []Skill) (Skill, bool) {
	if len(cands) == 0 {
		return Skill{}, false
	}
	qv, err := r.embedder.Embed(goalText)
	if err != nil {
		return Skill{}, false
	}
	var lex []retrieval.Scored // ONLY skills with a positive trigger score — a zero-score skill must
	//                            not get a spurious lexical rank (ID order) that would dilute the
	//                            semantic signal under RRF (items absent from a ranking contribute 0).
	sem := make([]retrieval.Scored, len(cands))
	cosByIdx := make([]float64, len(cands))
	lexByIdx := make([]float64, len(cands))
	for i, s := range cands {
		lexByIdx[i] = s.MatchScore(goalText)
		if lexByIdx[i] > 0 {
			lex = append(lex, retrieval.Scored{ID: i, Score: lexByIdx[i]})
		}
		sv, err := r.embedder.Embed(skillText(s))
		if err != nil {
			return Skill{}, false
		}
		cosByIdx[i] = retrieval.Cosine(qv, sv)
		sem[i] = retrieval.Scored{ID: i, Score: cosByIdx[i]}
	}
	// FuseRRF is rank-based (it reads each input's POSITION as the rank), so both inputs must be sorted
	// best-first before fusing.
	sortByScore(lex)
	sortByScore(sem)
	fused := retrieval.FuseRRF(lex, sem)
	for _, f := range fused { // fused is sorted best-first; take the top one that clears the floor
		if lexByIdx[f.ID] > 0 || cosByIdx[f.ID] >= semFloor {
			return cands[f.ID], true
		}
		// the very top fused item has no genuine signal -> nothing confident.
		break
	}
	return Skill{}, false
}

// sortByScore orders a Scored slice best-first (Score desc, ID asc on ties) so FuseRRF reads its
// positions as ranks.
func sortByScore(s []retrieval.Scored) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Score != s[j].Score {
			return s[i].Score > s[j].Score
		}
		return s[i].ID < s[j].ID
	})
}

// skillText is the text Match embeds for a skill: its human description if present, else its triggers,
// else its name — the most meaning-bearing surface for the semantic channel.
func skillText(s Skill) string {
	if strings.TrimSpace(s.Description) != "" {
		return s.Description
	}
	if len(s.Triggers) > 0 {
		return strings.Join(s.Triggers, ", ")
	}
	return s.Name
}

// -- build-time expansion: resolve sub-skills into a pure operator program ---- //

// Expand resolves every sub-skill call in the skill's body into operators — bounded depth, acyclic.
// Returns an error on a cycle, over-deep nesting, or an unknown sub-skill (raise->return error: the
// durability obligation for composition). The expanded program is marked synthesized=true (bespoke to
// this match, so the Workflow recognises it for the whole episode) regardless of whether the skill
// itself was seeded or minted. Mirrors Python expand(skill).
func (r *SkillRegistry) Expand(skill Skill) (Program, error) {
	root, err := r.expandNode(skill.Body.Root, 0, []string{skill.Name})
	if err != nil {
		return Program{}, err
	}
	return Program{
		Root:        root,
		Goal:        skill.Body.Goal,
		Synthesized: true,
		Rationale:   "skill '" + skill.Name + "'",
	}, nil
}

// expandNode is the recursive Node walk that resolves "skill:<name>" Steps into their sub-skill bodies.
// depth tracks how deep into sub-skill composition we are (NOT tree depth); stack is the visited-skill
// chain used to reject cycles (a []string copy per recursion so siblings don't alias each other's
// stack). Mirrors Python _expand_node(node, depth, stack):
//   - a non-skill Step returns unchanged;
//   - a skill-call Step: cycle-check against the stack, then depth+1>MaxSkillDepth bound, then resolve
//     the sub-skill and recurse into ITS body root (so one Step can expand into a whole subtree);
//   - Seq/Par recurse over children at the SAME depth (no extra nesting level consumed);
//   - Loop recurses into its body at the same depth, preserving Until/MaxIter.
func (r *SkillRegistry) expandNode(node Node, depth int, stack []string) (Node, error) {
	switch v := node.(type) {
	case Step:
		if !IsSkillCall(v) {
			return v, nil
		}
		name := CalledSkill(v)
		if containsString(stack, name) {
			return nil, errors.New("sub-skill cycle: " + strings.Join(stack, " -> ") + " -> " + name)
		}
		if depth+1 > MaxSkillDepth {
			return nil, errors.New("sub-skill nesting exceeds MaxSkillDepth=" + itoa(MaxSkillDepth))
		}
		sub, ok := r.skills[name]
		if !ok {
			return nil, errors.New("unknown sub-skill '" + name + "'")
		}
		// stack + (name,): a fresh slice so each recursion branch owns its visited chain.
		next := append(append([]string(nil), stack...), name)
		return r.expandNode(sub.Body.Root, depth+1, next)
	case Seq:
		children, err := r.expandChildren(v.Children, depth, stack)
		if err != nil {
			return nil, err
		}
		return Seq{Children: children}, nil
	case Par:
		children, err := r.expandChildren(v.Children, depth, stack)
		if err != nil {
			return nil, err
		}
		return Par{Children: children}, nil
	case Loop:
		body, err := r.expandNode(v.Body, depth, stack)
		if err != nil {
			return nil, err
		}
		return Loop{Body: body, Until: v.Until, MaxIter: v.MaxIter}, nil
	}
	// The sealed Node interface makes this unreachable; Python's fall-through `return node`.
	return node, nil
}

// expandChildren expands each child node at the same depth/stack (the list comprehension in Python's
// Seq/Par branches), short-circuiting on the first error.
func (r *SkillRegistry) expandChildren(children []Node, depth int, stack []string) ([]Node, error) {
	out := make([]Node, 0, len(children))
	for _, c := range children {
		ec, err := r.expandNode(c, depth, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, ec)
	}
	return out, nil
}

// -- runtime resolution: reframed (prompt + sub-skill refs) shape ------------ //

// ResolvedSkill is what the runtime resolver produces for a REFRAMED skill (GAP 8, §3.8): the worker
// prompt assembled by splicing in the resolved sub-skill prompts, plus the ordered chain of resolved
// sub-skill names (for observability / the worker's by-category invocation). It is the RUNTIME analogue
// of Expand's build-time pure-operator Program — but a prompt, not a Program. Calls counts the total
// resolved sub-skill calls (bounded by §3.8's per-SubAgent MaxSkillCalls budget at the staffing site).
type ResolvedSkill struct {
	Name      string   // the resolved skill's name
	Prompt    string   // the assembled worker prompt (sub-skill prompts spliced in)
	SubSkills []string // the chain of sub-skill names resolved, in resolution order
	Calls     int      // total resolved sub-skill calls (the truncate-to-floor accounting handle)
}

// subSkillMark is the in-prompt marker a high-level skill uses to reference a sub-skill ("skill:<name>"),
// the prompt-shape analogue of a SkillStep. The resolver replaces each marker with the sub-skill's
// resolved prompt at RUN time (never flattened into a workflow). Reuses SkillPrefix so the legacy and
// reframed forms name sub-skills identically.
func subSkillMark(name string) string { return SkillPrefix + name }

// ResolveBody resolves a REFRAMED skill's body at RUN time (GAP 8, §3.8) — the runtime sub-skill
// resolver that replaces the legacy build-time Expand for prompt-bodied skills. It walks the skill's
// SubSkillRefs, splices each sub-skill's resolved prompt into the parent prompt (replacing the
// "skill:<name>" marker, or appending if the marker is absent), and recurses — BOUNDED exactly like
// Expand: acyclic (a skill that transitively references itself is rejected) and depth <= MaxSkillDepth.
// On a cycle / over-deep nesting / unknown sub-skill it returns an error (the durability obligation, now
// enforced at the runtime point). A LEGACY (non-reframed, Program-bodied) skill has no Prompt, so this
// returns an error directing the caller to Expand — the two shapes never silently cross.
func (r *SkillRegistry) ResolveBody(skill Skill) (ResolvedSkill, error) {
	if !skill.IsReframed() {
		return ResolvedSkill{}, errors.New("skill '" + skill.Name + "' is legacy (Program body) — use Expand, not ResolveBody")
	}
	prompt, calls, err := r.resolvePrompt(skill, 0, []string{skill.Name})
	if err != nil {
		return ResolvedSkill{}, err
	}
	return ResolvedSkill{Name: skill.Name, Prompt: prompt, SubSkills: skill.SubSkillRefs, Calls: calls}, nil
}

// resolvePrompt is the recursive prompt-splice that backs ResolveBody. depth tracks sub-skill nesting
// (NOT text depth); stack is the visited-skill chain that rejects cycles (a []string copy per recursion
// so siblings don't alias). For each sub-skill ref: cycle-check against the stack, then depth+1 bound,
// then resolve the sub-skill's own prompt and splice it in. Mirrors expandNode's discipline on the
// prompt substrate.
func (r *SkillRegistry) resolvePrompt(skill Skill, depth int, stack []string) (string, int, error) {
	prompt := skill.Prompt
	calls := 0
	for _, name := range skill.SubSkillRefs {
		if containsString(stack, name) {
			return "", 0, errors.New("sub-skill cycle: " + strings.Join(stack, " -> ") + " -> " + name)
		}
		if depth+1 > MaxSkillDepth {
			return "", 0, errors.New("sub-skill nesting exceeds MaxSkillDepth=" + itoa(MaxSkillDepth))
		}
		sub, ok := r.skills[name]
		if !ok {
			return "", 0, errors.New("unknown sub-skill '" + name + "'")
		}
		if !sub.IsReframed() {
			return "", 0, errors.New("sub-skill '" + name + "' is legacy (Program body) — a reframed skill cannot reference a legacy one")
		}
		next := append(append([]string(nil), stack...), name)
		subPrompt, subCalls, err := r.resolvePrompt(sub, depth+1, next)
		if err != nil {
			return "", 0, err
		}
		calls += 1 + subCalls
		mark := subSkillMark(name)
		if strings.Contains(prompt, mark) {
			prompt = strings.ReplaceAll(prompt, mark, subPrompt)
		} else {
			prompt = prompt + "\n\n" + subPrompt
		}
	}
	return prompt, calls, nil
}

// -- minting (trace -> skill convertibility) --------------------------------- //

// Verify checks a candidate skill body before trust (the two-layer discipline). Returns (ok, reason).
// Mirrors Python verify(name, body):
//   - lowercase+trim the name; reject if it is not a non-empty identifier;
//   - reject if name collides with a SEED (non-synthesized) skill (frozen);
//   - reject an empty body;
//   - reject a body larger than MaxSteps.
//
// The identifier check reuses operators.go's identifierRe over the hyphen/underscore-stripped name —
// the exact Go equivalent of Python name.replace("-","").replace("_","").isalnum().
func (r *SkillRegistry) Verify(name string, body Program) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || !identifierRe.MatchString(stripWordChars(name)) {
		return false, "name must be a non-empty identifier"
	}
	if existing, ok := r.skills[name]; ok && !existing.Synthesized {
		return false, "'" + name + "' is a seed skill (frozen); cannot redefine"
	}
	steps := body.Steps()
	if len(steps) == 0 {
		return false, "skill body is empty"
	}
	if len(steps) > MaxSteps {
		return false, "skill body too large (" + itoa(len(steps)) + " > " + itoa(MaxSteps) + ")"
	}
	return true, "ok"
}

// Mint promotes a Program into a named, reusable skill (the trace->skill convertibility step). Returns
// ok=false (the zero Skill) if the body fails Verify OR if expanding the new skill would introduce a
// cycle / over-deep nesting (the runtime cycle-check guard). Mirrors Python mint(name, triggers, body,
// tier="composite", description=""):
//   - lowercase+trim name; bail if Verify fails;
//   - lowercase the triggers; build the Skill with synthesized=true;
//   - reject if Expand errors (a mint that would introduce a cycle / over-deep nesting);
//   - insert into the map; append to minted (once).
func (r *SkillRegistry) Mint(name string, triggers []string, body Program, tier, description string) (Skill, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if ok, _ := r.Verify(name, body); !ok {
		return Skill{}, false
	}
	lowered := make([]string, len(triggers))
	for i, t := range triggers {
		lowered[i] = strings.ToLower(t)
	}
	sk := Skill{
		Name:        name,
		Tier:        tier,
		Triggers:    lowered,
		Body:        body,
		Description: description,
		Synthesized: true,
	}
	// reject a mint that would introduce a cycle / over-deep nesting (Python's try/except ValueError).
	if _, err := r.Expand(sk); err != nil {
		return Skill{}, false
	}
	r.skills[name] = sk
	if !containsString(r.minted, name) {
		r.minted = append(r.minted, name)
	}
	return sk, true
}

// NewReframedSkill builds a REFRAMED (GAP 8) skill — a prompt + sub-skill references, no Program body.
// It is the additive constructor for the reframe shape; a skill built this way carries IsReframed()==true
// and resolves at run time via ResolveBody. Triggers default empty (a reframed skill does not self-match
// goals); pass them only for legacy interop. Synthesized is left to the caller (a seeded reframed skill
// is non-synthesized; a minted one is synthesized — set via MintReframed).
func NewReframedSkill(name, tier, prompt string, subSkillRefs []string, description string) Skill {
	return Skill{
		Name:         strings.ToLower(strings.TrimSpace(name)),
		Tier:         tier,
		Prompt:       prompt,
		SubSkillRefs: append([]string(nil), subSkillRefs...),
		Description:  description,
	}
}

// VerifyReframed checks a candidate REFRAMED skill before trust (the prompt-shape analogue of Verify):
//   - the name must be a non-empty identifier; not collide with a frozen seed skill;
//   - the prompt must be non-empty (a reframed skill IS its prompt);
//   - resolution must succeed (acyclic, depth<=MaxSkillDepth, every sub-skill known + reframed).
//
// It is a no-op equivalent to Verify for the durability contract: where Verify bounds the operator
// Program statically, VerifyReframed bounds the prompt resolution at the same caps.
func (r *SkillRegistry) VerifyReframed(sk Skill) (bool, string) {
	name := strings.ToLower(strings.TrimSpace(sk.Name))
	if name == "" || !identifierRe.MatchString(stripWordChars(name)) {
		return false, "name must be a non-empty identifier"
	}
	if existing, ok := r.skills[name]; ok && !existing.Synthesized {
		return false, "'" + name + "' is a seed skill (frozen); cannot redefine"
	}
	if !sk.IsReframed() {
		return false, "reframed skill prompt is empty"
	}
	sk.Name = name
	if _, err := r.ResolveBody(sk); err != nil {
		return false, err.Error()
	}
	return true, "ok"
}

// MintReframed promotes a REFRAMED skill (prompt + sub-skill refs) into the registry — the reframe-path
// analogue of Mint. Rejects (zero Skill, false) when VerifyReframed fails. The minted skill is
// Synthesized=true (so it is persisted + recallable through the same machinery as a legacy minted skill).
// The reframe flag gates the live RECALL path (Match), not minting: a reframed skill can be minted under
// either flag state, but is only invoked by the reframe-aware (Capability/category) path.
func (r *SkillRegistry) MintReframed(name, tier, prompt string, subSkillRefs []string, description string) (Skill, bool) {
	sk := NewReframedSkill(name, tier, prompt, subSkillRefs, description)
	sk.Synthesized = true
	if ok, _ := r.VerifyReframed(sk); !ok {
		return Skill{}, false
	}
	r.skills[sk.Name] = sk
	if !containsString(r.minted, sk.Name) {
		r.minted = append(r.minted, sk.Name)
	}
	return sk, true
}

// MintReframedTriggered is MintReframed with goal-matching TRIGGERS — the constructor a reframed skill
// the CAPABILITY recalls needs (gap-5-deeper / gap-8 remaining wire). MintReframed drops triggers (a
// category-invoked skill needs none), but the Capability's reframed recall (Capability.recallReframed →
// MatchReframedWithinTier) goal-matches on Triggers, so a reframed skill meant to be RECALLED by goal must
// carry them. This is the additive triggers-bearing mint; it leaves MintReframed and the persistence path
// untouched (the loader keeps using MintReframed). Triggers are lower-cased (the same normalisation the
// legacy Mint applies); everything else is MintReframed's discipline (VerifyReframed, Synthesized=true).
func (r *SkillRegistry) MintReframedTriggered(name, tier, prompt string, subSkillRefs, triggers []string, description string) (Skill, bool) {
	sk := NewReframedSkill(name, tier, prompt, subSkillRefs, description)
	sk.Synthesized = true
	lowered := make([]string, len(triggers))
	for i, t := range triggers {
		lowered[i] = strings.ToLower(t)
	}
	sk.Triggers = lowered
	if ok, _ := r.VerifyReframed(sk); !ok {
		return Skill{}, false
	}
	r.skills[sk.Name] = sk
	if !containsString(r.minted, sk.Name) {
		r.minted = append(r.minted, sk.Name)
	}
	return sk, true
}

// Minted returns a copy of the names minted this run, in mint order (a copy so callers can't mutate the
// registry's internal slice). Mirrors reading the Python registry.minted list.
func (r *SkillRegistry) Minted() []string {
	out := make([]string, len(r.minted))
	copy(out, r.minted)
	return out
}

// -- the seed library -------------------------------------------------------- //

// seedProgram builds a non-synthesized Program from a root (Python _P(root, goal="")).
func seedProgram(root Node) Program {
	return Program{Root: root, Goal: "", Synthesized: false}
}

// seedSkills builds the seed library — the goal-matched capabilities seeded at construction. Mirrors
// Python _seed_skills(): unit skills (one operator each), higher-level composites (some calling
// sub-skills), then the standardised-from-real-harness-data additions (#20). Order preserved exactly:
// the original units + composites, then the #20 units appended to units, then the #20 composites
// appended to composites — and the function returns units ++ composites (so every unit precedes every
// composite, matching Python's `return units + composites`).
func seedSkills() []Skill {
	// Unit skills — the leaf capabilities (one operator each).
	units := []Skill{
		{
			Name:     "decompose-task",
			Tier:     "unit",
			Triggers: []string{"break down", "decompose", "split"},
			Body:     seedProgram(NewSeq(NewStep("decompose", "general", ""))),
		},
		{
			Name:     "validate-result",
			Tier:     "unit",
			Triggers: []string{"validate", "check", "verify"},
			Body:     seedProgram(NewSeq(NewStep("validate", "general", ""))),
		},
		{
			Name:     "weigh-options",
			Tier:     "unit",
			Triggers: []string{"weigh", "rank"},
			Body: seedProgram(NewSeq(
				NewPar(NewStep("compare", "general", ""), NewStep("contrast", "general", "")),
				NewStep("rank", "general", ""),
			)),
		},
	}
	// Higher-level skills — compositions; 'diagnose' calls the decompose-task SUB-SKILL.
	composites := []Skill{
		{
			Name:     "design-feature",
			Tier:     "composite",
			Triggers: []string{"design", "build a", "implement", "architect", "create a"},
			Body: seedProgram(NewSeq(
				NewStep("decompose", "build", "split into parts"),
				NewStep("generate", "build", "draft each part"),
				NewStep("validate", "build", "check it holds together"),
			)),
			Description: "design -> build -> validate a new capability",
		},
		{
			Name:     "diagnose",
			Tier:     "composite",
			Triggers: []string{"why is", "why are", "diagnose", "debug", "root cause", "outage"},
			Body: seedProgram(NewSeq(
				skillStepDomain("decompose-task", "ops"),
				NewPar(NewStep("hypothesize", "ops", ""), NewStep("measure", "ops", "")),
			)),
			Description: "break down a fault, then hypothesise and measure in parallel",
		},
		{
			Name:     "evaluate-options",
			Tier:     "composite",
			Triggers: []string{"compare", "versus", " vs ", "which is better", "trade-off"},
			Body: seedProgram(NewSeq(
				NewStep("decompose", "general", "find the dimensions"),
				NewPar(NewStep("compare", "general", ""), NewStep("contrast", "general", "")),
				NewStep("rank", "general", ""),
			)),
			Description: "lay out the dimensions, weigh, and rank options",
		},
	}
	// Standardised from real harness data — the moves common to lathe + Claude Code / Codex / Hermes /
	// OpenCode (the recurring research / plan / explain / explore / review / refactor / test / debug
	// vocabulary). Unit skills are one move; composites call sub-skills. (#20)
	units = append(units,
		Skill{
			Name:        "research",
			Tier:        "unit",
			Triggers:    []string{"research", "investigate", "background", "gather context", "look into"},
			Body:        seedProgram(NewSeq(NewStep("measure", "general", ""), NewStep("analogize", "general", ""))),
			Description: "gather the context and prior art a decision needs",
		},
		Skill{
			Name:        "plan",
			Tier:        "unit",
			Triggers:    []string{"plan", "outline", "approach", "strategy", "lay out the steps"},
			Body:        seedProgram(NewSeq(NewStep("decompose", "general", ""), NewStep("rank", "general", ""))),
			Description: "propose a structured approach before execution",
		},
		Skill{
			Name:        "explain",
			Tier:        "unit",
			Triggers:    []string{"explain", "describe", "walk through", "clarify", "how does", "what does"},
			Body:        seedProgram(NewSeq(NewStep("decompose", "general", ""), NewStep("synonymize", "general", ""))),
			Description: "articulate how something works or why a choice was made",
		},
		Skill{
			Name:        "summarize",
			Tier:        "unit",
			Triggers:    []string{"summarize", "summary", "recap", "tl;dr", "condense"},
			Body:        seedProgram(NewSeq(NewStep("measure", "general", ""), NewStep("compress", "general", ""))),
			Description: "condense long content to its essential points",
		},
		Skill{
			Name:        "document",
			Tier:        "unit",
			Triggers:    []string{"document", "write docs", "api docs", "annotate"},
			Body:        seedProgram(NewSeq(NewStep("expose-affordances", "doc", ""), NewStep("synonymize", "doc", ""))),
			Description: "produce written, user- or developer-facing guidance",
		},
	)
	composites = append(composites,
		Skill{
			Name: "explore-code",
			Tier: "composite",
			Triggers: []string{"explore the code", "search the code", "find where",
				"scan the codebase", "understand the code"},
			Body: seedProgram(NewSeq(
				NewStep("map", "code", ""),
				NewPar(NewStep("decompose", "code", ""), NewStep("expose-affordances", "code", "")),
				NewStep("measure", "code", ""),
			)),
			Description: "systematically search + examine code to understand it",
		},
		Skill{
			Name:     "review-code",
			Tier:     "composite",
			Triggers: []string{"code review", "review the code", "review this pr", "audit the code"},
			Body: seedProgram(NewSeq(
				skillStepDomain("explore-code", "code"),
				NewPar(NewStep("measure", "code", ""), NewStep("validate", "code", "")),
				skillStepDomain("explain", "code"),
			)),
			Description: "evaluate code for correctness, style, and requirements",
		},
		Skill{
			Name:     "refactor-scope",
			Tier:     "composite",
			Triggers: []string{"refactor this", "refactor the", "clean up the", "restructure", "tidy up"},
			Body: seedProgram(NewSeq(
				skillStepDomain("explore-code", "code"),
				NewStep("measure", "code", "baseline"),
				NewStep("generalize", "code", ""),
				NewStep("validate", "code", "behaviour preserved"),
			)),
			Description: "improve quality without changing external behaviour",
		},
		Skill{
			Name:     "test-and-validate",
			Tier:     "composite",
			Triggers: []string{"run the tests", "run checks", "write tests", "add tests", "pre-flight"},
			Body: seedProgram(NewSeq(
				NewPar(NewStep("validate", "test", ""), NewStep("measure", "test", "")),
				NewStep("rank", "test", "by severity if failures"),
			)),
			Description: "run checks + verification, report pass/fail by severity",
		},
		Skill{
			Name:     "debug-system",
			Tier:     "composite",
			Triggers: []string{"debug", "why is it failing", "stack trace", "not working", "throws an error"},
			Body: seedProgram(NewSeq(
				skillStepDomain("diagnose", "debug"),
				NewStep("hypothesize", "debug", ""), NewStep("iterate", "debug", ""),
			)),
			Description: "locate + understand a failure's root cause (uses diagnose)",
		},
	)
	// THE THREE PATHS (M5, representation-space-rebuild.md §1.4) — the missing middle between a single
	// operator (one move) and a free-form program: a NAMED, recurring, directed traversal of the
	// abstraction ladder whose definition-of-done only REALITY or stored KNOWLEDGE can sign off. Each is a
	// composite skill — a Program of catalog operators carrying a Step.Source annotation — so it rides the
	// existing synth → workflow-runner → convert rails and inherits bounded/acyclic durability for free.
	// Every path's DoD requires at least one GROUNDED source (a path that closes on a model-only DoD is
	// recorded but never minted — recombination can be confidently wrong, spec §12).
	composites = append(composites, seedPaths()...)
	return append(units, composites...)
}

// PathNames is the closed set of the three canonical directed traversals (M5 §1.4). Used by the config
// ReprMatrix Paths gate and by the cognitive-property tests that prove each path fires.
var PathNames = []string{"analogy", "induction", "deduction"}

// IsPath reports whether name is one of the three seed path skills (analogy/induction/deduction).
func IsPath(name string) bool {
	for _, p := range PathNames {
		if p == name {
			return true
		}
	}
	return false
}

// seedPaths builds the three canonical path skills (M5 §1.4). Each is a composite whose body is a Program
// of seed operators, with Step.Source annotating which ladder rung each move privileges:
//
//	ANALOGY:   concrete ─lift→ abstract ─reframe(@memory)→ abstract ─ground→ concrete ─validate(@reality)
//	           round-trip across the top; DoD = a concrete TARGET candidate exists AND reality did not refute it.
//	INDUCTION: many concretes ─lift(@memory)→ abstract ─validate(@compute)→ ─curate(@store)→ knowledge
//	           upward, ends in a STORE; DoD = a general statement holds over ≥2 instances AND the store accepted it.
//	DEDUCTION: principle(@memory) ─ground→ concrete consequence ─validate(@reality)→
//	           downward, ends in REALITY; DoD = the principle was applied AND reality did not refute it.
//
// The operators are all in the seed catalog (abstract/analogize/compare/generate/validate — LIFT/REFRAME/
// GROUND/assess; generalize/curate/specialize), so every path's body resolves under VerifyProgram, and
// the Source tags exercise every grounded rung (memory/reality/compute/store) at least once across the
// three. The triggers are chosen so a goal phrased as a transfer/rule/consequence question recalls the
// matching path.
func seedPaths() []Skill {
	return []Skill{
		{
			Name:     "analogy",
			Tier:     "composite",
			Triggers: []string{"analogy", "analogous", "like the", "transfer from", "by analogy", "similar to"},
			Body: seedProgram(NewSeq(
				NewStep("abstract", "general", "lift this case to its structure"),
				SourceStep("analogize", "general", "recall a source case with the same structure", SourceMemory),
				NewStep("compare", "general", "map the source structure onto the target"),
				NewStep("generate", "general", "instantiate a concrete candidate for the target"),
				SourceStep("validate", "general", "reality must not refute the transferred candidate", SourceReality),
			)),
			Description: "round-trip: lift a case to structure, reframe via a remembered analog, ground a target, validate against reality",
		},
		{
			Name:     "induction",
			Tier:     "composite",
			Triggers: []string{"induction", "generalize from", "what is the rule", "the pattern across", "infer the rule", "always the case"},
			Body: seedProgram(NewSeq(
				SourceStep("abstract", "general", "recall the concrete instances (>=2)", SourceMemory),
				NewStep("generalize", "general", "lift the instances to a general statement"),
				SourceStep("validate", "general", "the rule must hold over the instances", SourceCompute),
				SourceStep("curate", "general", "store the verified rule as durable knowledge", SourceStore),
			)),
			Description: "upward: lift many remembered instances to a rule, verify it holds, curate it into a durable store",
		},
		{
			Name:     "deduction",
			Tier:     "composite",
			Triggers: []string{"deduction", "deduce", "it follows that", "apply the principle", "therefore", "given the rule"},
			Body: seedProgram(NewSeq(
				SourceStep("specialize", "general", "recall the principle and facts", SourceMemory),
				NewStep("generate", "general", "apply the principle to produce a concrete consequence"),
				SourceStep("validate", "general", "reality signs off; on refute, invalidate the bad premise", SourceReality),
			)),
			Description: "downward: recall a principle, ground a concrete consequence, test it against reality (self-corrects on refute)",
		},
	}
}

// skillStepDomain builds a sub-skill-call Step with an explicit domain and empty note — the seed library
// uses skill_step(name, domain) (2-arg) in several places. Mirrors Python skill_step(name, domain).
func skillStepDomain(name, domain string) Step { return SkillStep(name, domain, "") }
