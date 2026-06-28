// persist.go is the cross-session persistence of the MINTED library (P7.1): the skills and operators
// convertibility produces evaporate on process exit, so learning can't outlive a run. These Save/Load
// methods are the single persistence seam (see reports/2026-06-07-memory-system-audit.md §4):
//
//   - only MINTED (Synthesized) entries are written — seed entries are frozen invariants that reload
//     from code, never from disk;
//   - one JSON object per line (JSONL), append-friendly;
//   - Load is BEST-EFFORT: a malformed line is skipped, a re-mint that fails Verify/cycle-check is
//     dropped — a corrupt store degrades to "less memory", never a crash;
//   - Load re-mints through the normal Mint path, so every loaded entry is re-verified before trust.
//
// Determinism: persistence is opt-in (the caller supplies the reader/writer), so tests that don't wire
// a store stay fully deterministic.
package cognition

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// skillRecord is the on-disk shape of a minted skill. LEGACY (Program-bodied) skills write Body (the
// Program via ToDict). REFRAMED (GAP 8) skills write Prompt + SubSkillRefs instead — both `omitempty`,
// so a legacy record is byte-identical to the pre-reframe format (the new fields are simply absent). The
// loader routes by shape: a non-empty Prompt ⇒ MintReframed; else the legacy ProgramFromDict + Mint.
type skillRecord struct {
	Name         string         `json:"name"`
	Tier         string         `json:"tier"`
	Triggers     []string       `json:"triggers"`
	Description  string         `json:"description"`
	Body         map[string]any `json:"body,omitempty"`           // legacy Program body
	Prompt       string         `json:"prompt,omitempty"`         // reframed body (GAP 8): the worker prompt
	SubSkillRefs []string       `json:"sub_skill_refs,omitempty"` // reframed body: sub-skill references
}

// SaveMinted writes every minted (Synthesized) skill as one JSON line, in mint order. Seed skills are
// never written. A LEGACY skill writes its Program body; a REFRAMED skill writes prompt + sub-skill refs
// (the legacy Body is left nil/omitted) — so a registry that never mints a reframed skill produces a
// byte-identical file to the pre-reframe code.
func (r *SkillRegistry) SaveMinted(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, name := range r.minted {
		s, ok := r.skills[name]
		if !ok || !s.Synthesized {
			continue
		}
		rec := skillRecord{
			Name: s.Name, Tier: s.Tier, Triggers: s.Triggers, Description: s.Description,
		}
		if s.IsReframed() {
			rec.Prompt, rec.SubSkillRefs = s.Prompt, s.SubSkillRefs
		} else {
			rec.Body = s.Body.ToDict()
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// LoadMinted reads minted skills (one JSON object per line) and re-mints each through the normal mint
// path (re-Verify + cycle/resolve check). A record with a non-empty Prompt re-mints as a REFRAMED skill
// (MintReframed); else the legacy ProgramFromDict + Mint. Best-effort: malformed lines and re-mints that
// fail are skipped. Returns how many skills were loaded.
func (r *SkillRegistry) LoadMinted(rd io.Reader) (int, error) {
	sc := newJSONLScanner(rd)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec skillRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if strings.TrimSpace(rec.Prompt) != "" {
			if _, ok := r.MintReframed(rec.Name, rec.Tier, rec.Prompt, rec.SubSkillRefs, rec.Description); ok {
				n++
			}
			continue
		}
		body, err := ProgramFromDict(rec.Body)
		if err != nil {
			continue
		}
		body.Synthesized = true // a re-minted skill is always Synthesized=true (so Match recalls it)
		if _, ok := r.Mint(rec.Name, rec.Triggers, body, rec.Tier, rec.Description); ok {
			n++
		}
	}
	return n, sc.Err()
}

// operatorRecord is the on-disk shape of a minted operator (flat: name/family/intent).
type operatorRecord struct {
	Name   string `json:"name"`
	Family string `json:"family"`
	Intent string `json:"intent"`
}

// SaveMinted writes every minted operator as one JSON line, in mint order. Seed operators are not written.
func (r *OperatorRegistry) SaveMinted(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, name := range r.Minted() {
		spec, ok := r.Get(name)
		if !ok || !spec.Synthesized {
			continue
		}
		if err := enc.Encode(operatorRecord{Name: spec.Name, Family: spec.Family, Intent: spec.Intent}); err != nil {
			return err
		}
	}
	return nil
}

// LoadMinted reads minted operators and re-mints each (re-Verify). Best-effort. Returns the count loaded.
func (r *OperatorRegistry) LoadMinted(rd io.Reader) (int, error) {
	sc := newJSONLScanner(rd)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec operatorRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if _, ok := r.Mint(rec.Name, rec.Family, rec.Intent); ok {
			n++
		}
	}
	return n, sc.Err()
}

// newJSONLScanner returns a bufio.Scanner with a large line buffer (minted skill bodies can be big).
func newJSONLScanner(rd io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return sc
}
