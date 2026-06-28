// shadow.go is the SHADOW instrument over the legibility contract (05-LEGIBLE-GENERATION §4 / §5b /
// §8). It reads the conscious's in-band control tag, computes what that tag WOULD route at FILTER and
// GATE, and compares the prediction against the REAL decision the deterministic control floor already
// produced — a PARITY observation — WITHOUT ever changing routing. It is OPEN, ADVISORY, and makes NO
// COST CLAIM (§4): the cheapest first version is a measurement instrument, not a cost play. It hands the
// scaling work two numbers for free: the fast-path COVERAGE (tag known + agrees) and the NOVEL-TAG
// histogram (the ranked missing-op list, §4 / §7).
//
// SHADOW means: parse, predict, compare, log — never decide. The ACTUAL verdict the caller already
// computed is the one that routes; this instrument only records whether the tag would have agreed. So
// turning the instrument on cannot change any decision (the parity gate, §6, is satisfied by
// construction at this stage — the LLM path is the only path that acts).
//
// LEAF: pure stdlib + the events leaf + the TagRegistry (this package). No seam, no backend, no engine.
package legible

import (
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// VerdictAdmit / VerdictFlag / VerdictReject are the three-state admission verdict the seam's control
// floor produces, named as plain strings so this leaf need not import the seam/types verdict enum (the
// caller passes the floor's verdict NAME through, and the shadow predicts one of these from conf and
// compares to the actual). These names match types.Verdict.String() exactly.
const (
	VerdictAdmit  = "ADMIT"
	VerdictFlag   = "FLAG"
	VerdictReject = "REJECT"
)

// shadowAdmitThreshold / shadowFlagThreshold are the two band edges the SHADOW route uses to turn the
// conscious's self-reported conf into a predicted admission verdict. They mirror the control floor's
// own band edges (control.AdmitBandEdges: ADMIT/FLAG at 0.6, FLAG/REJECT at 0.32) so the shadow speaks
// the same language as the thing it shadows — a high self-conf predicts ADMIT, a middling one FLAG, a
// low one REJECT. They are duplicated here (not imported) to keep this an events+self leaf; if the
// floor's edges move, move these to match (they are a SHADOW prior, never the routing decision).
const (
	shadowAdmitThreshold = 0.6
	shadowFlagThreshold  = 0.32
)

// Shadow is the legible-generation SHADOW instrument: a TagRegistry (the one-source-of-truth contract)
// + the bus emit closure. Its methods PARSE the leading tag, PREDICT a route, COMPARE it to the actual
// decision, and EMIT the comparison — they return nothing the caller routes on. A nil *Shadow is a
// no-op on every method (so the seam can hold one unconditionally and the OFF path costs nothing).
type Shadow struct {
	reg  *TagRegistry
	emit events.Emit
}

// NewShadow builds the instrument over the contract registry + the emit closure. reg/emit may be nil;
// every method is nil-safe so the seam can construct one even on the offline/test path and rely on the
// toggle (checked by the caller) to keep it silent.
func NewShadow(reg *TagRegistry, emit events.Emit) *Shadow {
	return &Shadow{reg: reg, emit: emit}
}

// Registry exposes the contract registry (so the caller can derive the same PromptFragment the parser
// is built from — one source of truth for the prompt + the parser). nil-safe.
func (s *Shadow) Registry() *TagRegistry {
	if s == nil {
		return nil
	}
	return s.reg
}

// Strip removes the leading legibility envelope from text and returns the clean prose, so TRANSFORM
// voices the prose and never the internal tag (05 §5b "TRANSFORM strips the tag"). On a missing or
// malformed envelope it returns the text unchanged (the advisory safety net — a thought with no tag is
// voiced as-is). nil-safe: a nil *Shadow returns the text unchanged (no tag was ever requested).
func (s *Shadow) Strip(text string) string {
	if s == nil || s.reg == nil {
		return text
	}
	_, prose, ok := s.reg.ParseTag(text)
	if !ok {
		return text
	}
	return prose
}

// shadowVerdict turns a parsed tag into the admission verdict the SHADOW route WOULD produce. act=yes is
// a request to cross the watched seam, NOT an admission lift — admission is governed by conf alone here.
// A high self-conf predicts ADMIT, a middling one FLAG, a low one REJECT (the band edges above).
func shadowVerdict(t Tag) string {
	switch {
	case t.Conf >= shadowAdmitThreshold:
		return VerdictAdmit
	case t.Conf >= shadowFlagThreshold:
		return VerdictFlag
	default:
		return VerdictReject
	}
}

// ShadowFilter is the FILTER-point shadow (05 §5b): parse the tag, predict admit/flag from conf, and
// compare to the ACTUAL admission verdict the control floor already produced (passed in by name). It
// emits legible.tag (the parsed tag + known/novel), legible.parity (shadow vs actual, agree), and — on
// a novel:<desc> — legible.novel (the gap signal). It RETURNS nothing the caller routes on: the actual
// verdict is the one that admits; this only records whether the tag agreed. nil-safe (no emit ⇒ no-op).
//
// A missing/malformed tag (ParseTag ok=false) is NOT a parity observation — the route fell back to the
// LLM path it uses today (the advisory net, §2). The instrument records nothing in that case rather than
// scoring the fallback as a disagreement (which would corrupt the coverage number).
func (s *Shadow) ShadowFilter(text, actualVerdict string) {
	if s == nil || s.reg == nil || s.emit == nil {
		return
	}
	tag, _, ok := s.reg.ParseTag(text)
	if !ok {
		return // no well-formed tag ⇒ fell back to the LLM path; not a parity observation
	}
	s.emitTag("filter", tag)
	shadow := shadowVerdict(tag)
	s.emitParity("filter", shadow, actualVerdict)
}

// ShadowGate is the GATE-point shadow (05 §5b): parse the tag, read the op/domain it WOULD route, and
// compare to the ACTUAL operator/domain the control-floor ranking selected for the winner. The op match
// is the routing-relevant comparison (operator-mapping by lookup is what the GATE regex would replace);
// domain is carried for the histogram. Emits legible.tag + legible.parity (op agreement) + legible.novel
// on a novel op. RETURNS nothing the caller routes on. nil-safe.
//
// agree is op-equality: the tag's op equals the operator the actual winner carried. An UNKNOWN op
// (Known=false, not novel) cannot fast-path — it is recorded as a disagreement-by-coverage (the tag
// named a move the registry lacks), which is exactly the coverage gap the instrument exists to surface.
func (s *Shadow) ShadowGate(text, actualOp, actualDomain string) {
	if s == nil || s.reg == nil || s.emit == nil {
		return
	}
	tag, _, ok := s.reg.ParseTag(text)
	if !ok {
		return
	}
	s.emitTag("gate", tag)
	// the shadow op route is the tag's op; the actual is the operator the floor ranking chose.
	agree := tag.Known && tag.Op == actualOp
	s.emit(events.LegibleParity,
		"gate shadow op="+orDash(tag.Op)+" vs actual="+orDash(actualOp)+" -> "+agreeWord(agree),
		events.D{
			"site":          "gate",
			"shadow":        tag.Op,
			"actual":        actualOp,
			"agree":         agree,
			"shadow_domain": tag.Domain,
			"actual_domain": actualDomain,
		})
}

// emitTag emits legible.tag (the parsed tag + the known/novel classification) at a seam site, and — when
// the tag is the novel:<desc> escape — a legible.novel sighting (the ranked scaling gap, §4/§7).
func (s *Shadow) emitTag(site string, t Tag) {
	s.emit(events.LegibleTag,
		site+" tag op="+orDash(t.Op)+" domain="+orDash(t.Domain)+
			" conf="+f2(t.Conf)+" act="+yesNo(t.Act)+" ("+knownWord(t)+")",
		events.D{
			"site":   site,
			"op":     t.Op,
			"domain": t.Domain,
			"conf":   t.Conf,
			"act":    t.Act,
			"known":  t.Known,
			"novel":  t.IsNovel,
		})
	if t.IsNovel {
		s.emit(events.LegibleNovel,
			"novel:"+orDash(t.NovelDesc)+" (at "+site+")",
			events.D{
				"desc": t.NovelDesc,
				"op":   t.Op,
				"site": site,
			})
	}
}

// emitParity emits legible.parity: the shadow-predicted route vs the actual decision + whether they agree.
func (s *Shadow) emitParity(site, shadow, actual string) {
	agree := shadow == actual
	s.emit(events.LegibleParity,
		site+" shadow="+orDash(shadow)+" vs actual="+orDash(actual)+" -> "+agreeWord(agree),
		events.D{
			"site":   site,
			"shadow": shadow,
			"actual": actual,
			"agree":  agree,
		})
}

// -- tiny formatting helpers (kept local; this leaf imports only events + strconv) -------------------

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func agreeWord(b bool) string {
	if b {
		return "AGREE"
	}
	return "DIFFER"
}

func knownWord(t Tag) string {
	if t.IsNovel {
		return "novel"
	}
	if t.Known {
		return "known"
	}
	return "unknown"
}

// f2 renders a float with 2 decimals for the tag summary.
func f2(x float64) string {
	return strconv.FormatFloat(x, 'f', 2, 64)
}
