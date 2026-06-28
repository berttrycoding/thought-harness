package synthfidelity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// Verdict is one fixture's structural-fidelity score: the [0,1] Score, the Pass
// decision (Score >= the fixture's PassThreshold), the per-criterion breakdown for
// the ledger/report, and a one-line Reason. Parsed=false (with Reason set) when the
// program could not be parsed/verified — a structural fail, not a silent pass.
type Verdict struct {
	// Score is the renormalised fidelity in [0,1] over the criteria the fixture
	// actually constrains.
	Score float64
	// Pass is Score >= the fixture's PassThreshold AND the program parsed+verified.
	Pass bool
	// Parsed is false when the program dict failed NodeFromDict or VerifyProgram —
	// an unparseable/invalid program scores 0 and fails, never a silent pass.
	Parsed bool
	// Criteria is the per-criterion fractional credit [0,1] (only the constrained
	// ones are present), keyed by criterion name for the report.
	Criteria map[string]float64
	// Reason is a compact human-readable explanation for the ledger pointer.
	Reason string
}

// defaultPassThreshold is the fidelity bar a fixture uses when it sets none. 0.75 is
// "the great majority of the constrained structure is right" — high enough that a
// plausible-but-wrong program (a flattened branch, a wrong-family op) lands below it,
// low enough that a faithful program with one cosmetic miss still passes.
const defaultPassThreshold = 0.75

// progFeatures is the structural fingerprint the oracle extracts from a parsed
// program tree: the multiset of operator names, the set of families/moves the
// operators carry, the set of control-flow node kinds present, the total step
// count, whether any step crosses to reality, and the union of tool-scopes. Every
// field is computed by walking the tree + consulting the operator catalog — nothing
// reads surface text — so the score is a pure function of the program's STRUCTURE.
type progFeatures struct {
	operators     map[string]int  // operator name -> count
	families      map[string]bool // operator families present
	moves         map[string]bool // abstraction-ladder moves present
	shapes        map[string]bool // control-flow node kinds present ("seq"/"par"/"loop"/"step")
	steps         int             // total operator-step count
	actsOnReality bool            // any step has a non-empty tool-scope OR a reality/compute Source
	toolScope     map[string]bool // union of all operators' tool-scopes
}

// ScoreProgram is the oracle: it parses prog (a "kind"-discriminated node dict, the
// SAME shape NodeFromDict round-trips for an LLM-written program), VERIFIES it
// structurally (cognition.VerifyProgram — bounded loops, known operators, bounded
// size), extracts its structural fingerprint against the operator catalog, and
// scores it against the fixture's Expect with the given weights. An unparseable or
// invalid program is a hard structural fail (Parsed=false, Score 0).
//
// The score is the weighted mean of the per-criterion fractional credits over only
// the criteria the fixture actually constrains (an empty MustOperators contributes
// nothing — neither weight nor credit), so a fixture that constrains only shape+ops
// is still scored on a clean [0,1].
func ScoreProgram(prog ProgramShape, fx Fixture, cat *cognition.OperatorRegistry, w Weights) Verdict {
	feats, ok, reason := featuresOf(prog, cat)
	if !ok {
		return Verdict{Score: 0, Pass: false, Parsed: false, Criteria: map[string]float64{}, Reason: reason}
	}

	type crit struct {
		name   string
		weight float64
		credit float64
		note   string
	}
	var crits []crit
	add := func(name string, weight, credit float64, note string) {
		crits = append(crits, crit{name, weight, credit, note})
	}

	exp := fx.Expect

	// MustOperators — fraction of required operators present (the strongest signal).
	if len(exp.MustOperators) > 0 {
		hit := 0
		var missing []string
		for _, op := range exp.MustOperators {
			if feats.operators[op] > 0 {
				hit++
			} else {
				missing = append(missing, op)
			}
		}
		credit := float64(hit) / float64(len(exp.MustOperators))
		note := fmt.Sprintf("operators %d/%d", hit, len(exp.MustOperators))
		if len(missing) > 0 {
			note += " missing " + strings.Join(missing, ",")
		}
		add("operators", w.Operators, credit, note)
	}

	// ForbidOperators / ForbidShapes — binary: any forbidden op OR shape present
	// zeroes this criterion (the discriminator a bad program most often trips).
	if len(exp.ForbidOperators) > 0 || len(exp.ForbidShapes) > 0 {
		credit := 1.0
		var tripped []string
		for _, op := range exp.ForbidOperators {
			if feats.operators[op] > 0 {
				credit = 0
				tripped = append(tripped, "op:"+op)
			}
		}
		for _, sh := range exp.ForbidShapes {
			if feats.shapes[sh] {
				credit = 0
				tripped = append(tripped, "shape:"+sh)
			}
		}
		note := "no forbidden structure"
		if len(tripped) > 0 {
			note = "FORBIDDEN present: " + strings.Join(tripped, ",")
		}
		add("forbid", w.Forbid, credit, note)
	}

	// MustFamilies — fraction of required families present.
	if len(exp.MustFamilies) > 0 {
		hit := 0
		for _, f := range exp.MustFamilies {
			if feats.families[f] {
				hit++
			}
		}
		add("families", w.Families, float64(hit)/float64(len(exp.MustFamilies)),
			fmt.Sprintf("families %d/%d", hit, len(exp.MustFamilies)))
	}

	// MustMoves — fraction of required abstraction-ladder moves present.
	if len(exp.MustMoves) > 0 {
		hit := 0
		for _, m := range exp.MustMoves {
			if feats.moves[m] {
				hit++
			}
		}
		add("moves", w.Moves, float64(hit)/float64(len(exp.MustMoves)),
			fmt.Sprintf("moves %d/%d", hit, len(exp.MustMoves)))
	}

	// RequireShapes — fraction of required control-flow node kinds present (the
	// load-bearing structure-forces-faculty check).
	if len(exp.RequireShapes) > 0 {
		hit := 0
		var missing []string
		for _, sh := range exp.RequireShapes {
			if feats.shapes[sh] {
				hit++
			} else {
				missing = append(missing, sh)
			}
		}
		note := fmt.Sprintf("shapes %d/%d", hit, len(exp.RequireShapes))
		if len(missing) > 0 {
			note += " missing " + strings.Join(missing, ",")
		}
		add("shapes", w.Shapes, float64(hit)/float64(len(exp.RequireShapes)), note)
	}

	// Steps — binary: the step count is within [MinSteps, MaxSteps] (0 = unbounded).
	if exp.MinSteps > 0 || exp.MaxSteps > 0 {
		credit := 1.0
		within := true
		if exp.MinSteps > 0 && feats.steps < exp.MinSteps {
			within = false
		}
		if exp.MaxSteps > 0 && feats.steps > exp.MaxSteps {
			within = false
		}
		if !within {
			credit = 0
		}
		add("steps", w.Steps, credit, fmt.Sprintf("steps=%d in [%d,%d]=%v", feats.steps, exp.MinSteps, exp.MaxSteps, within))
	}

	// Act — binary: at least one step crosses to reality (a tool-scope or a
	// reality/compute Source). The Investigator/Verifier ACT faculty requirement.
	if exp.ActOnReality {
		credit := 0.0
		if feats.actsOnReality {
			credit = 1.0
		}
		add("act", w.Act, credit, fmt.Sprintf("acts_on_reality=%v", feats.actsOnReality))
	}

	// ToolScope — fraction of required tools the program's operators can dispatch.
	if len(exp.MustToolScope) > 0 {
		hit := 0
		var missing []string
		for _, t := range exp.MustToolScope {
			if feats.toolScope[t] {
				hit++
			} else {
				missing = append(missing, t)
			}
		}
		note := fmt.Sprintf("tools %d/%d", hit, len(exp.MustToolScope))
		if len(missing) > 0 {
			note += " missing " + strings.Join(missing, ",")
		}
		add("tool_scope", w.ToolScope, float64(hit)/float64(len(exp.MustToolScope)), note)
	}

	// Weighted mean over the constrained criteria, renormalised so the score is
	// always on [0,1]. A fixture that constrains NOTHING (no Expect set) is a bank
	// error — score 0, fail, loud reason — never a vacuous pass.
	if len(crits) == 0 {
		return Verdict{Score: 0, Pass: false, Parsed: true, Criteria: map[string]float64{},
			Reason: "fixture has no Expect constraints (bank error — never a vacuous pass)"}
	}
	var sumW, sumWC float64
	criteria := make(map[string]float64, len(crits))
	notes := make([]string, 0, len(crits))
	for _, c := range crits {
		sumW += c.weight
		sumWC += c.weight * c.credit
		criteria[c.name] = c.credit
		notes = append(notes, c.name+"="+trim2(c.credit)+" ("+c.note+")")
	}
	score := 0.0
	if sumW > 0 {
		score = sumWC / sumW
	}
	threshold := fx.PassThreshold
	if threshold <= 0 {
		threshold = defaultPassThreshold
	}
	pass := score >= threshold
	sort.Strings(notes) // stable, deterministic report ordering
	reasonStr := fmt.Sprintf("score=%s thr=%s -> %s | %s", trim2(score), trim2(threshold), passWord(pass), strings.Join(notes, "; "))
	return Verdict{Score: score, Pass: pass, Parsed: true, Criteria: criteria, Reason: reasonStr}
}

// featuresOf parses prog through the SAME NodeFromDict decoder a live program uses,
// VERIFIES it with cognition.VerifyProgram (so an invalid program — unbounded loop,
// unknown operator, runaway size — is a hard structural fail), then walks the tree
// + consults the catalog to build the structural fingerprint. ok=false (with a
// reason) on a parse OR verify failure; the caller scores it 0 and fails.
func featuresOf(prog ProgramShape, cat *cognition.OperatorRegistry) (progFeatures, bool, string) {
	if len(prog) == 0 {
		return progFeatures{}, false, "empty/absent program (no synthesis produced)"
	}
	root, err := cognition.NodeFromDict(map[string]any(prog))
	if err != nil {
		return progFeatures{}, false, "unparseable program: " + err.Error()
	}
	p := cognition.Program{Root: root}
	if okV, issues := cognition.VerifyProgram(p, cat); !okV {
		return progFeatures{}, false, "program failed structural verification: " + strings.Join(issues, "; ")
	}

	feats := progFeatures{
		operators: map[string]int{},
		families:  map[string]bool{},
		moves:     map[string]bool{},
		shapes:    map[string]bool{},
		toolScope: map[string]bool{},
	}
	collectShapes(root, feats.shapes)
	steps := p.Steps()
	feats.steps = len(steps)
	for _, st := range steps {
		feats.operators[st.Operator]++
		// Source-level reality crossing: a step that draws from reality or a tool-
		// backed compute source acts on reality even before its operator's tool-scope.
		if st.Source == cognition.SourceReality || st.Source == cognition.SourceCompute {
			feats.actsOnReality = true
		}
		spec, ok := cat.Get(st.Operator)
		if !ok {
			// Verified above, so this should not happen; be defensive — skip.
			continue
		}
		if spec.Family != "" {
			feats.families[spec.Family] = true
		}
		if spec.Move != "" {
			feats.moves[string(spec.Move)] = true
		}
		if len(spec.ToolScope) > 0 {
			feats.actsOnReality = true
			for _, t := range spec.ToolScope {
				feats.toolScope[t] = true
			}
		}
	}
	return feats, true, "ok"
}

// collectShapes records which control-flow node kinds appear in the tree. A Step is
// "step"; Seq/Par/Loop record their own kind and recurse. Used for RequireShapes /
// ForbidShapes — the structure-forces-faculty check.
func collectShapes(n cognition.Node, into map[string]bool) {
	switch v := n.(type) {
	case cognition.Step:
		into["step"] = true
	case cognition.Seq:
		into["seq"] = true
		for _, c := range v.Children {
			collectShapes(c, into)
		}
	case cognition.Par:
		into["par"] = true
		for _, c := range v.Children {
			collectShapes(c, into)
		}
	case cognition.Loop:
		into["loop"] = true
		collectShapes(v.Body, into)
	}
}

// trim2 renders a float to 2 decimals without trailing-zero noise in the report.
func trim2(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	return s
}

// passWord renders a pass/fail token for the ledger pointer.
func passWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
