// Package seams ports the two seams of opposite transparency that connect the three
// layers. This file is the HIDDEN seam (subconscious -> conscious), invisible by design.
//
// Pipeline: FILTER (admit raw) -> GATE (order + surface survivors) -> TRANSFORM (re-voice).
// Validation happens on the *raw* candidate, before voicing — that kills laundered
// hallucination at source. The output reads as CONSCIOUS's own next thought; the raw payload
// is retained only for trace-back.
//
// SR-1 (seam-as-channel): the seam is a CHANNEL, not an arbiter. FILTER admits (a per-candidate
// quality gate). GATE orders the survivors for presentation (so the most relevant reads first) and
// reports whether MORE THAN ONE survived — i.e. competing alternatives exist — but it does NOT
// resolve the competition and it never reads Stance to do so. TRANSFORM re-voices the primary; the
// competing survivors are surfaced to CONSCIOUS, where the Controller (which holds the full graph +
// V(s) + the THINK/BRANCH/BACKTRACK/ACT/MERGE/STOP spine) decides what to pursue. Reconciling
// competing ideas is the Conscious layer's job, not the membrane's.
package seams

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/legible"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Filter is the Critic, half 1 — ADMISSION, and a Pattern-C HYBRID (floor + optional model
// ceiling), structurally identical to the Controller. It validates a RAW candidate BEFORE
// Transform voices it: the deterministic control floor (control.ScoreAdmit) ALWAYS runs and is
// always a valid verdict; the model is ESCALATED (control.AdmitAmbiguity flags the fuzzy case) only
// in llm/hybrid mode and only when the backend is a backends.FilterEscalator. The model NEVER
// overrides a structural floor reject (refuted_by_reality / contradicts_belief). A non-escalation
// is surfaced via escalation.floor_stands (Rule 4), never silent. Mirrors Python seams.hidden.Filter
// + the Controller's smart-hybrid escalation (critic/controller.go:364-394).
//
// HONEST SCOPE (what the Filter's admit/reject actually GATES — do not over-read the "kills
// laundered hallucination" framing as total coverage): admission is ENFORCED only on the
// SUBCONSCIOUS-INJECTION stream — the hidden seam's Relay (this file, ~L498-507: a rejected
// candidate is NOT a survivor) and the awake default-mode generator (engine/continuous.go:86-87,
// gated on v.Admit()). The CONSCIOUS layer's OWN effortful thought (engine.generate, reactive.go
// ~L507-521) is screened by the SAME Filter, but ONLY the verdict's confidence is read back — the
// thought is voiced REGARDLESS of admit/reject. So the conscious's self-authored next thought is
// trusted at the same level a conventional harness trusts raw model output; the Filter does not
// reject it. The membrane guards what crosses INTO consciousness, not what consciousness writes
// itself. (Doc-only honest-scope note — no logic change.)
type Filter struct {
	// mode: "control" (deterministic floor only — the default), "llm" (escalate every admission),
	// "hybrid" (escalate only flagged-fuzzy admissions). llm/hybrid require a backend that satisfies
	// backends.FilterEscalator; otherwise the mode resolves to "control" (the hasattr guard, mirror
	// NewController).
	mode      string
	escalator backends.FilterEscalator // nil ⇒ no model ceiling (the floor is authoritative)
	emit      events.Emit
	gate      *config.Gate // seam.hidden_filter; nil ⇒ always-on (the pre-config behaviour)

	// legible-generation SHADOW instrument (WF-E CC-1): when legibleGate is ON, after the REAL admission
	// verdict is computed the Filter parses the candidate's in-band control tag and emits a PARITY
	// observation (shadow-predicted admit/flag vs the actual verdict) — WITHOUT changing the verdict. nil
	// shadow / OFF (or disabled) gate ⇒ no-op ⇒ byte-identical. Set via SetLegible (nil-safe).
	shadow      *legible.Shadow
	legibleGate *config.Gate // seam.legible_generation (DEFAULTS OFF); nil ⇒ off
}

// NewFilter builds a Filter as a Pattern-C hybrid over the cognition mode + the (optional) model
// backend + the injected emit closure. Like NewController, the mode resolves to the deterministic
// floor ("control") UNLESS the backend actually satisfies backends.FilterEscalator (the
// `hasattr(backend, "judge_admission")` guard). The test double does not implement FilterEscalator,
// so under it the Filter is always the pure floor — identical behaviour to before this refactor.
func NewFilter(mode string, backend backends.Backend, emit events.Emit) *Filter {
	resolved := controlMode // the deterministic floor (the default)
	var esc backends.FilterEscalator
	if backend != nil {
		if fe, ok := backend.(backends.FilterEscalator); ok {
			esc = fe
			resolved = mode
		}
	}
	return &Filter{mode: resolved, escalator: esc, emit: emit}
}

// controlMode is the deterministic-floor cognition mode (the Filter's resolved-mode default),
// matching the config default + the Controller's resolved-mode default. Named here so the floor-only
// posture has a single source of truth.
const controlMode = "control"

// SetGate wires the seam.hidden_filter config gate (M1). nil ⇒ always-on. When the toggle is OFF the
// Filter short-circuits to ADMIT-ALL (a pass-through admission), so the wire/panel stays but the
// admission decision is bypassed (config.skip). The candidate's own relevance becomes the confidence
// so downstream value/gate reads stay sensible.
func (f *Filter) SetGate(g *config.Gate) { f.gate = g }

// SetLegible wires the legible-generation SHADOW instrument (WF-E CC-1): the contract shadow + the
// seam.legible_generation toggle gate. When the gate is OFF (the default) the Filter never parses a tag
// and emits no legible.* events — byte-identical. nil-safe (nil shadow/gate ⇒ off).
func (f *Filter) SetLegible(s *legible.Shadow, g *config.Gate) { f.shadow, f.legibleGate = s, g }

// Admit is the Pattern-C admission ladder (mirrors critic/controller.go:364-394 exactly):
//  1. the deterministic control floor ALWAYS runs (control.ScoreAdmit) — Pattern-A core, always
//     a valid verdict;
//  2. compute a deterministic fuzziness flag (control.AdmitAmbiguity over the floor verdict);
//  3. escalate to the model judgment ONLY when eligible — mode=="llm", or mode=="hybrid" AND the
//     case is flagged-fuzzy (ambiguity >= threshold) — AND a model escalator is wired;
//  4. on a successful escalation the model's verdict is adopted (it may adjust trust ADMIT↔FLAG↔
//     REJECT) BUT it may NEVER lift a STRUCTURAL floor reject (refuted_by_reality /
//     contradicts_belief) — that case is not even escalated (the floor is authoritative);
//  5. on every non-escalation of an escalation-ELIGIBLE case (model declined / no model wired /
//     structural-reject) the floor STANDS and escalation.floor_stands is emitted (Rule 4 — never
//     silent). The default control mode does NOT emit it (nothing was eligible), so goldens hold.
//
// It then emits seam.filter with the FINAL verdict (the escalated one if adopted). Faithful to
// Python Filter.admit: the summary carries source/domain, the verdict name, the confidence (2dp)
// and the reason; the data carries the structured WHY (signals + who appraised) alongside the scalar.
func (f *Filter) Admit(c types.Candidate, hist []types.Thought, value float64) types.FilterVerdict {
	// CONFIG (M1): seam.hidden_filter OFF ⇒ ADMIT-ALL pass-through (the Filter does not validate). The
	// admission decision is bypassed, not the wire — the candidate passes with its own relevance as the
	// confidence, and config.skip records the bypass. No backend call, so determinism is preserved.
	if f.gate.Disabled() {
		f.gate.Skip("admit-all (filter bypassed)")
		return types.FilterVerdict{
			Verdict:    types.ADMIT,
			Confidence: c.Relevance,
			Reason:     "filter disabled (admit-all)",
			Signals:    map[string]any{},
			Source:     "config",
		}
	}

	// 1. the deterministic FLOOR always runs (Pattern-A core) — control, never the backend.
	v := control.ScoreAdmit(c, hist, value)

	// 2. the deterministic fuzziness flag (the analogue of the Controller's `ambiguity`).
	ambiguity := control.AdmitAmbiguity(v, c)

	// A structural floor FACT — reality refuted the claim (refuted_by_reality) or it contradicts a
	// confident belief (contradicts_belief) — is ground truth the model may NOT override (exactly as
	// the Controller's model may not override BRANCH/ACT/BACKTRACK/MERGE). It is never escalation-
	// eligible: AdmitAmbiguity already zeroes its fuzziness, and even in llm mode (which would
	// otherwise escalate everything) the guard keeps the floor authoritative. The guard fires on the
	// PRESENCE of the hard signal, not only on the REJECT band — a refuted FLAG still carries the
	// reality refutation, so the model must not be given the chance to lift it.
	_, refuted := v.Signals["refuted_by_reality"]
	_, contradicts := v.Signals["contradicts_belief"]
	// Grounding integrity (Item 4): an asserted-observation-without-grounding is a structural REJECT too
	// — the conscious may not voice a reality result it never observed, and the model may not lift it.
	_, assertsUngrounded := v.Signals["asserts_ungrounded_observation"]
	// Tool-affordance hallucination (#43): a refusal that denies a capability the loop just demonstrated is
	// a structural REJECT too — the model may not lift the conscious's denial of reality it already reached.
	_, deniesAccess := v.Signals["denies_available_reality"]
	structuralFact := refuted || contradicts || assertsUngrounded || deniesAccess

	// 3. escalate ONLY when eligible: llm escalates every admission; hybrid escalates only the
	// flagged-fuzzy case. Either way a structural fact is never escalated (the floor is authoritative).
	flaggedFuzzy := ambiguity >= control.AdmitAmbiguityThreshold
	eligible := f.mode == "llm" || (f.mode == "hybrid" && flaggedFuzzy)
	doEscalate := eligible && !structuralFact && f.escalator != nil

	if doEscalate {
		judged, ok := f.escalator.JudgeAdmission(c, hist, v)
		if ok {
			// 4. adopt the model's refined verdict (its trust judgment is the ceiling above the
			// floor). The seam.filter `appraiser` field (v.Source -> "llm") records the escalation;
			// no separate flag is added to the wire (the default path stays byte-identical).
			v = judged
		} else {
			// 5. the model declined/parse-failed -> the floor stands; surface it (Rule 4).
			f.emitFloorStands(v, ambiguity, "model-declined", true)
		}
	} else if f.mode != controlMode {
		// escalation-eligible situations that did NOT escalate must be surfaced (Rule 4). The pure
		// control mode (the default) never reaches here, so it emits no new event and goldens hold.
		switch {
		case structuralFact && (f.mode == "llm" || (f.mode == "hybrid" && flaggedFuzzy)):
			// would have escalated but for the structural guard — the floor is authoritative (reality/
			// belief said no; the model may not override it). llm mode would-have-escalated everything;
			// hybrid only when also flagged-fuzzy. (Note: a refuted/contradicting case has zero
			// ambiguity, so the hybrid arm is effectively llm-only here — kept explicit for clarity.)
			f.emitFloorStands(v, ambiguity, "structural-reject", false)
		case eligible && f.escalator == nil:
			// flagged-fuzzy (or llm-mode) but no model is wired to consult.
			f.emitFloorStands(v, ambiguity, "no-model", false)
		}
		// a non-flagged hybrid case is escalation-INELIGIBLE by design — not surfaced (would flood).
	}

	f.emit(
		events.Filter,
		c.Source.String()+"/"+domainOrDash(c.Domain)+": "+v.Verdict.String()+
			" ("+f2(v.Confidence)+") "+v.Reason,
		events.D{
			"verdict":    v.Verdict.String(),
			"confidence": v.Confidence,
			"reason":     v.Reason,
			// P6: capture the breakdown that produced the verdict + who appraised it
			// (control floor | llm escalation), so the decision carries a structured WHY, not just
			// the scalar. After a successful escalation the appraiser is the model's (Source "llm");
			// otherwise it is the floor's (held at "heuristic" through M1–M5, -> "control" in M6).
			"signals":   v.Signals,
			"appraiser": v.Source,
			"source":    c.Source.String(),
			"domain":    domainData(c.Domain),
			"text":      c.Text,
		},
	)
	// Legible-generation SHADOW (WF-E CC-1, OFF by default): parse the candidate's in-band control tag
	// and record whether its conf would have routed the SAME admission verdict the floor just produced —
	// a PARITY observation only, never a change to v. Gated on seam.legible_generation; OFF ⇒ no-op ⇒
	// byte-identical. The verdict v is already final; the instrument reads it, it does not feed it.
	if legibleOn(f.legibleGate) {
		f.shadow.ShadowFilter(c.Text, v.Verdict.String())
	}
	return v
}

// legibleOn reports whether the legible-generation SHADOW instrument is active. Unlike a normal
// bypass-not-delete gate (nil ⇒ on), this OPT-IN instrument is OFF unless a gate is wired AND its
// seam.legible_generation toggle is ON — so the default (no gate, or the default-OFF toggle) keeps every
// golden byte-identical (no tag parse, no legible.* events). Nil-safe.
func legibleOn(g *config.Gate) bool {
	return g != nil && g.Enabled()
}

// emitFloorStands surfaces a Pattern-C non-escalation (Rule 4): the Filter's deterministic FLOOR
// stood as the admission because the model was not consulted or declined. modelConsulted is true
// only when the model was asked and declined (vs never asked). Mirrors the new
// escalation.floor_stands emit on the Controller side.
func (f *Filter) emitFloorStands(v types.FilterVerdict, ambiguity float64, reason string, modelConsulted bool) {
	f.emit(
		events.EscalationFloorStands,
		"filter.admit floor stands ("+v.Verdict.String()+", "+reason+
			", ambiguity="+f2(ambiguity)+")",
		events.D{
			"site":            "filter.admit",
			"decision":        v.Verdict.String(),
			"floor_decision":  v.Verdict.String(),
			"ambiguity":       round2(ambiguity),
			"reason":          reason,
			"model_consulted": modelConsulted,
		},
	)
}

// Gate is the arbiter — it decides what surfaces and FORKS rather than discards. It runs on
// admitted survivors. Ranking is Pattern A (pure control): Select calls control.Rank DIRECTLY and
// NEVER calls a model — value/relevance ordering is closed-form math, so a model would add latency
// and nondeterminism without adding correctness. The Gate therefore holds no backend. Mirrors
// Python seams.hidden.Gate.
type Gate struct {
	emit events.Emit
	gate *config.Gate // seam.hidden_gate; nil ⇒ always-on (the pre-config behaviour)

	// legible-generation SHADOW instrument (WF-E CC-1): when legibleGate is ON, after the REAL winner is
	// ranked the Gate parses the winner's in-band control tag and emits a PARITY observation (shadow op/
	// domain vs the actual winner's operator/domain) — WITHOUT changing the ranking. OFF ⇒ no-op ⇒
	// byte-identical. Set via SetLegible (nil-safe).
	shadow      *legible.Shadow
	legibleGate *config.Gate // seam.legible_generation (DEFAULTS OFF); nil ⇒ off
}

// NewGate builds a Gate over the injected emit closure. It takes NO backend — ranking is the
// deterministic control floor (control.Rank), never a model call.
func NewGate(emit events.Emit) *Gate {
	return &Gate{emit: emit}
}

// SetGate wires the seam.hidden_gate config gate (M1). nil ⇒ always-on. When OFF the Gate
// short-circuits to RANK-IDENTITY: the winner is the first survivor (input order kept), no backend
// rank/bias is applied; conflict is still surfaced (>1 survivor) so the Controller's BRANCH spine is
// untouched. The arbitration decision is bypassed, not the wire (config.skip records it).
func (g *Gate) SetGate(cg *config.Gate) { g.gate = cg }

// SetLegible wires the legible-generation SHADOW instrument (WF-E CC-1) into the Gate: the contract
// shadow + the seam.legible_generation toggle gate. OFF (the default) ⇒ no tag parse, no legible.*
// events ⇒ byte-identical. nil-safe.
func (g *Gate) SetLegible(s *legible.Shadow, cg *config.Gate) { g.shadow, g.legibleGate = s, cg }

// scored pairs a candidate (by index into the survivor slice) with its rank score. The index
// is the IDENTITY handle — the winner is later compared by index/pointer, never by value
// equality (two distinct candidates may compare equal field-by-field, which would corrupt the
// loser set).
type scored struct {
	idx     int     // index into the candidates slice passed to Select (identity)
	score   float64 // SORT KEY: backend rank score + bias for the candidate's domain
	rawRank float64 // the UNBIASED backend rank score — what the emitted `scores` dict carries
	//                (Python emits `s` from zip(candidates, scores), i.e. the raw score, not the
	//                bias-augmented sort key; the bias only orders the ranking, it is not displayed).
}

// Select ranks the admitted survivors, applies the per-domain bias, and returns the winner,
// the (ordered) losers, whether the survivor set is in conflict, AND the winner's INDEX into
// the input `cands` slice.
//
// The trailing winnerIdx is the IDENTITY handle. Python compared `c is winner` on the SAME
// object reference; Go Candidates are value types, so identity is carried explicitly as the
// survivor-slot index — the caller reads the winner's verdict back by that index, never by
// value equality (two distinct candidates may be field-for-field equal, which would corrupt
// the read-back).
//
// Faithful to Python Gate.select:
//   - score = backend rank score + bias.get(domain or "", 0.0);
//   - a STABLE sort descending by that biased score (Python's sorted is stable; sort.SliceStable
//     here) — equal scores keep input order, which is load-bearing for determinism;
//   - the winner is rank-0 by index; the loser slice is the remaining ranked slots in order.
//
// The emitted seam.gate carries the per-candidate WHY (reasons) alongside the scores.
func (g *Gate) Select(
	cands []types.Candidate, hist []types.Thought, bias map[string]float64,
) (winner types.Candidate, losers []types.Candidate, conflict bool, winnerIdx int) {
	// CONFIG (M1): seam.hidden_gate OFF ⇒ RANK-IDENTITY pass-through. No backend rank, no bias — the
	// first survivor wins (input order kept), the rest are losers in order, conflict is still
	// >1-survivor so the Controller still forks on competing alternatives. The arbitration is bypassed,
	// not the wire. No backend call, so determinism holds.
	if g.gate.Disabled() {
		g.gate.Skip("rank-identity (gate bypassed)")
		losers = make([]types.Candidate, 0, len(cands)-1)
		if len(cands) > 1 {
			losers = append(losers, cands[1:]...)
		}
		return cands[0], losers, len(cands) > 1, 0
	}
	// Pattern A: ranking is the deterministic control floor (control.Rank), called DIRECTLY — NO
	// model, ever. value/relevance ordering is closed-form math; a model would add latency and
	// nondeterminism without adding correctness. The Gate holds no backend (the rank no longer
	// routes through one).
	scores, reasons := control.Rank(cands, hist)

	// Pair each candidate (by index) with its biased SORT KEY (and its raw rank score for display),
	// then STABLE-sort descending by the sort key.
	ranked := make([]scored, len(cands))
	for i := range cands {
		ranked[i] = scored{
			idx:     i,
			score:   scores[i] + bias[domainKey(cands[i].Domain)],
			rawRank: scores[i],
		}
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		// reverse=True: descending by biased score. Stable sort keeps equal scores in input
		// order (matching Python sorted(reverse=True), which is stable on the original order).
		return ranked[a].score > ranked[b].score
	})

	winnerIdx = ranked[0].idx // identity handle into cands (NOT a value comparison)
	winner = cands[winnerIdx]
	losers = make([]types.Candidate, 0, len(ranked)-1)
	for _, rs := range ranked[1:] {
		losers = append(losers, cands[rs.idx])
	}
	// SR-1 (seam-as-channel): "conflict" is content-NEUTRAL — it is simply whether more than one
	// candidate survived admission, i.e. competing alternatives exist for the single conscious
	// continuation slot. It is NO LONGER read from Stance (the old Gate.Conflicting counted distinct
	// hand-set stances). The seam does not resolve the competition; it surfaces the survivors and lets
	// the Controller decide (it owns the BRANCH spine). Stance survives only as descriptive metadata.
	conflict = len(cands) > 1

	// P6: capture the per-candidate WHY alongside the scores. reason_by is keyed in the
	// ORIGINAL candidate order over zip(candidates, reasons or []); when reasons is short/nil
	// the zip stops at the shorter side.
	reasonBy := map[string]any{}
	n := len(reasons)
	if len(cands) < n {
		n = len(cands)
	}
	for i := 0; i < n; i++ {
		reasonBy[domainOrIndex(cands[i].Domain, i)] = reasons[i]
	}

	// scores dict is keyed in RANKED order (Python builds it from `ranked` with enumerate i) and
	// carries the RAW (unbiased) rank score — Python's `round(s, 3)` over `s` from zip(candidates,
	// scores), NOT the bias-augmented sort key. The bias decides the ranking; it is never displayed.
	scoreMap := map[string]any{}
	for i, rs := range ranked {
		scoreMap[domainOrIndex(cands[rs.idx].Domain, i)] = round3(rs.rawRank)
	}

	summary := "winner=" + domainStr(winner.Domain) + " (of " + strconv.Itoa(len(cands)) + ")"
	if conflict {
		summary += " [CONFLICT->fork losers]"
	}
	g.emit(
		events.Gate,
		summary,
		events.D{
			"winner":   domainData(winner.Domain),
			"conflict": conflict,
			"losers":   domainList(losers),
			"scores":   scoreMap,
			"reasons":  reasonBy,
			// Pattern A: ranking is always the deterministic control floor, so the appraiser is the
			// control floor's name (control.Appraiser — "control" since M6; was "heuristic" through
			// M1–M5). It is no longer read off a backend.
			"appraiser": control.Appraiser,
		},
	)
	// Legible-generation SHADOW (WF-E CC-1, OFF by default): parse the WINNER's in-band control tag and
	// record whether its op would have routed to the SAME operator the floor ranking selected — a PARITY
	// observation only, never a change to the ranking. Gated on seam.legible_generation; OFF ⇒ no-op ⇒
	// byte-identical. (A candidate with no tag — the common case today — parses ok=false ⇒ no observation.)
	if legibleOn(g.legibleGate) {
		g.shadow.ShadowGate(winner.Text, operatorName(winner.Operator), domainKey(winner.Domain))
	}
	return winner, losers, conflict, winnerIdx
}

// operatorName renders the winner's operator as its registry name (the token the tag's `op` is compared
// against), or "" when the candidate carries no operator (nil). Used only by the legible SHADOW.
func operatorName(op *types.Operator) string {
	if op == nil {
		return ""
	}
	return op.String()
}

// TransformToNarrative re-voices the raw return into the self-narrative, conditioned on prior
// thought. Mirrors Python transform_to_narrative (a thin pass-through to backend.Transform).
func TransformToNarrative(backend backends.Backend, c types.Candidate, hist []types.Thought) string {
	return backend.Transform(c, hist)
}

// verdictPair retains a candidate alongside the Filter verdict it received. Python carried
// this as a (Candidate, FilterVerdict) tuple list; the explicit pair keeps the candidate
// IDENTITY (the same struct value the survivor/winner comparison uses).
type verdictPair struct {
	Candidate types.Candidate
	Verdict   types.FilterVerdict
}

// RelayResult is the outcome of one pass through the hidden seam. Thought is nil when nothing
// was admitted (CONSCIOUS then generates its own next thought). Mirrors Python RelayResult.
type RelayResult struct {
	Thought  *types.Thought
	Winner   *types.Candidate
	Losers   []types.Candidate
	Verdicts []verdictPair
	Conflict bool
}

// HiddenSeam runs the FILTER -> GATE -> TRANSFORM pipeline. Mirrors Python HiddenSeam.
type HiddenSeam struct {
	gate      *Gate
	filter    *Filter
	backend   backends.Backend
	emit      events.Emit
	transform *config.Gate // seam.hidden_transform; nil ⇒ always-on (the pre-config behaviour)

	// legible-generation SHADOW instrument (WF-E CC-1): when legibleGate is ON, the winner's in-band
	// control tag is STRIPPED before voicing (the tag is internal, never shown — 05 §5b). OFF (the
	// default) ⇒ no strip ⇒ byte-identical. Set via SetLegible (nil-safe).
	shadow      *legible.Shadow
	legibleGate *config.Gate // seam.legible_generation (DEFAULTS OFF); nil ⇒ off
}

// NewHiddenSeam wires the seam from its Gate, Filter, backend and emit closure.
func NewHiddenSeam(gate *Gate, filt *Filter, backend backends.Backend, emit events.Emit) *HiddenSeam {
	return &HiddenSeam{gate: gate, filter: filt, backend: backend, emit: emit}
}

// SetGates wires the three hidden-seam stage gates (M1) in one call: Filter (admission), Gate
// (arbitration), and Transform (re-voicing). Each is nil-safe (nil ⇒ always-on). When Transform is
// OFF the seam relays the winner's RAW text un-revoiced (the winner already passed the Filter), so the
// re-voicing decision is bypassed, not the wire (config.skip). Filter/Gate gates are forwarded to the
// inner organs. Determinism holds: a disabled stage skips a backend call, never reorders.
func (h *HiddenSeam) SetGates(filter, gate, transform *config.Gate) {
	if h.filter != nil {
		h.filter.SetGate(filter)
	}
	if h.gate != nil {
		h.gate.SetGate(gate)
	}
	h.transform = transform
}

// SetLegible wires the legible-generation SHADOW instrument (WF-E CC-1) into the seam AND its inner
// Filter + Gate in one call: the contract shadow + the seam.legible_generation toggle gate. The seam
// strips the winner's tag before voicing; the Filter/Gate shadow-parse at their decision points. OFF
// (the default) ⇒ no strip, no tag parse, no legible.* events ⇒ byte-identical. nil-safe throughout.
func (h *HiddenSeam) SetLegible(s *legible.Shadow, g *config.Gate) {
	h.shadow, h.legibleGate = s, g
	if h.filter != nil {
		h.filter.SetLegible(s, g)
	}
	if h.gate != nil {
		h.gate.SetLegible(s, g)
	}
}

// Relay admits each raw return, arbitrates the survivors, voices the winner, and returns the
// result. Faithful to Python HiddenSeam.relay:
//   - verdicts are taken over EVERY raw return (the full intake record);
//   - survivors are the admitted candidates (verdict.admit); if none, return early (nothing
//     voiced — CONSCIOUS generates);
//   - the Gate picks the winner; Transform re-voices it; the winner's confidence is read back
//     from the verdict list BY IDENTITY (`c is winner` — the same struct, compared by index,
//     not by value equality);
//   - the voiced Thought is INJECTED, id=-1 (the engine assigns the real id on append).
func (h *HiddenSeam) Relay(
	rawReturns []types.Candidate, hist []types.Thought, bias map[string]float64, value float64,
) RelayResult {
	verdicts := make([]verdictPair, len(rawReturns))
	survivors := make([]types.Candidate, 0, len(rawReturns))
	// survivorIdx tracks, for each survivor, its index in rawReturns/verdicts — the identity
	// handle used to read the winner's verdict back (Python's `c is winner` identity test).
	survivorIdx := make([]int, 0, len(rawReturns))
	for i, c := range rawReturns {
		v := h.filter.Admit(c, hist, value)
		verdicts[i] = verdictPair{Candidate: c, Verdict: v}
		if v.Admit() {
			survivors = append(survivors, c)
			survivorIdx = append(survivorIdx, i)
		}
	}
	if len(survivors) == 0 {
		// nothing admitted -> CONSCIOUS generates
		return RelayResult{Thought: nil, Verdicts: verdicts}
	}

	winner, losers, conflict, winnerSurvivorIdx := h.gate.Select(survivors, hist, bias)
	// Read the winner's confidence back BY IDENTITY: the Gate returns the winner's index into
	// the survivor slice; survivorIdx maps that to the rawReturns/verdicts index. This mirrors
	// Python's `next(v.confidence for c, v in verdicts if c is winner)` (an `is` identity test),
	// NOT a value-equality scan that could match the wrong duplicate.
	conf := verdicts[survivorIdx[winnerSurvivorIdx]].Verdict.Confidence

	// Legible-generation SHADOW (WF-E CC-1, OFF by default): STRIP the winner's in-band control tag
	// before voicing — the tag is internal routing only and is never shown (05 §5b). OFF ⇒ winner.Text
	// unchanged ⇒ byte-identical. The stripped text is what TRANSFORM re-voices and what the raw-relay /
	// raw: trace shows (so no envelope glyph ever reaches the stream). The Candidate handed to
	// TransformToNarrative carries the stripped text too, so the re-voicing prompt sees clean prose.
	winnerText := winner.Text
	if legibleOn(h.legibleGate) {
		winnerText = h.shadow.Strip(winner.Text)
	}
	transformIn := winner
	transformIn.Text = winnerText

	// CONFIG (M1): seam.hidden_transform OFF ⇒ RAW RELAY. The winning return is voiced un-revoiced
	// (the winner already passed the Filter), so the re-voicing decision is bypassed, not the wire. No
	// backend Transform call, so determinism is preserved.
	var text string
	if h.transform.Disabled() {
		h.transform.Skip("raw relay (transform bypassed)")
		text = winnerText
	} else {
		text = TransformToNarrative(h.backend, transformIn, hist)
	}
	if text == "" {
		// OUTPUT role: the substrate could not re-voice the winning return. Show its REAL text
		// un-revoiced (the winner already passed the Filter) rather than a heuristic template or a
		// blank thought — no faked intelligence reaches the stream.
		text = winnerText
	}
	// RawReturn holds the winning *Candidate (the closed union's `case *Candidate:` form that
	// value/controller type-switch on); Python stored the Candidate object itself.
	rawWinner := winner
	thought := &types.Thought{
		ID:         -1,
		Text:       text,
		Source:     types.INJECTED,
		Confidence: conf,
		RawReturn:  &rawWinner,
		Operator:   winner.Operator,
	}
	h.emit(
		events.Transform,
		"raw: "+pyRepr(runeSlice(winnerText, 42))+" -> voiced: "+pyRepr(runeSlice(text, 42)),
		events.D{"raw": winnerText, "voiced": text, "domain": domainData(winner.Domain)},
	)
	h.emit(
		events.Inject,
		"INJECTED ("+domainStr(winner.Domain)+", conf="+f2(conf)+")",
		events.D{"domain": domainData(winner.Domain), "confidence": conf},
	)
	return RelayResult{
		Thought:  thought,
		Winner:   &rawWinner, // same object as thought.RawReturn (Python: winner is winner)
		Losers:   losers,
		Verdicts: verdicts,
		Conflict: conflict,
	}
}

// ----------------------------------------------------------------------------
// small formatting helpers — faithful to the Python f-string / repr formatting at the SAME
// emit sites, so the JSONL wire stays byte-identical.
// ----------------------------------------------------------------------------

// domainOrDash renders `candidate.domain or '-'`: None or empty -> "-".
func domainOrDash(d *string) string {
	if d == nil || *d == "" {
		return "-"
	}
	return *d
}

// domainKey renders the bias-map lookup key `candidate.domain or ""` (Python
// `bias.get(c.domain or "", 0.0)` — None coerces to "" before the lookup).
func domainKey(d *string) string {
	if d == nil {
		return ""
	}
	return *d
}

// domainOrIndex renders `c.domain or f"c{i}"`: None or empty -> "c{i}" (the scores/reasons
// dict keys must be strings, so the fallback names the slot by index).
func domainOrIndex(d *string, i int) string {
	if d == nil || *d == "" {
		return "c" + strconv.Itoa(i)
	}
	return *d
}

// domainStr renders `f"{winner.domain}"`: a Python f-string of None prints "None"; a string
// prints itself. Used in the seam.gate / seam.inject summaries.
func domainStr(d *string) string {
	if d == nil {
		return "None"
	}
	return *d
}

// domainData renders `candidate.domain` as a DATA value: None -> nil (JSON null), else the
// string. Must NOT be a *string (the JsonlSink stringifies a residual pointer to its address);
// resolve to a string-or-nil interface here at the emit site.
func domainData(d *string) any {
	if d == nil {
		return nil
	}
	return *d
}

// domainList renders `[c.domain for c in losers]` — a slice whose elements are each candidate's
// domain (string or nil), resolved to data values (never *string).
func domainList(cands []types.Candidate) []any {
	out := make([]any, len(cands))
	for i, c := range cands {
		out[i] = domainData(c.Domain)
	}
	return out
}

// runeSlice reproduces Python str slicing `s[:n]` — by code point (rune), not byte.
func runeSlice(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// f2 reproduces Python f"{x:.2f}" — fixed 2 decimals, round-half-to-even (Go's %.2f and
// CPython's format both round half-to-even).
func f2(x float64) string {
	return strconv.FormatFloat(x, 'f', 2, 64)
}

// round3 reproduces Python round(x, 3) at the emit site (the sink does NOT round): format to 3
// fixed decimals (round-half-to-even) and parse back so the emitted value matches the Python
// wire byte-for-byte. +-Inf/NaN pass through unchanged.
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// round2 reproduces Python round(x, 2) at the emit site — the escalation.floor_stands ambiguity is
// rounded to 2dp (matching the Controller's round2(ambiguity)). +-Inf/NaN pass through unchanged.
func round2(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

// pyRepr reproduces CPython's str repr (the f-string `!r` conversion) for the TRANSFORM
// summary, so `raw: 'x' -> voiced: 'y'` is byte-identical to Python.
//
// Algorithm (CPython unicode_repr): pick the quote — single by default, but double when the
// string contains a single quote and no double quote; escape backslash, the chosen quote,
// \n \r \t, and non-printable code points as \xHH / \uHHHH / \UHHHHHHHH.
func pyRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case isPyPrintable(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(`\x`)
			b.WriteString(hex2(uint32(r), 2))
		case r < 0x10000:
			b.WriteString(`\u`)
			b.WriteString(hex2(uint32(r), 4))
		default:
			b.WriteString(`\U`)
			b.WriteString(hex2(uint32(r), 8))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// isPyPrintable approximates Python str.isprintable() for the repr escape decision: a rune is
// printable unless it is a "non-printable" code point (C0/C1 controls, the various separator
// and "other" categories). For the ASCII-and-common-text the seam handles this is exact;
// beyond that it errs toward printing (matching Python for the overwhelming majority of text,
// and only diverging on exotic separator/format code points that do not arise here).
func isPyPrintable(r rune) bool {
	if r < 0x20 || r == 0x7f {
		return false // C0 controls + DEL
	}
	if r >= 0x80 && r <= 0xa0 {
		return false // C1 controls + NBSP boundary (Python treats NBSP/0xa0 as non-printable)
	}
	return true
}

// hex2 formats v as a lower-case hex string left-padded with zeros to width (Python's
// `\xHH`/`\uHHHH`/`\UHHHHHHHH` use lower-case hex digits).
func hex2(v uint32, width int) string {
	s := strconv.FormatUint(uint64(v), 16)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
