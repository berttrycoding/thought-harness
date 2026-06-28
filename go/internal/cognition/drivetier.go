// This file is the DRIVE TIER model — the endogenous-drive stratification of the awake-mode engine
// (docs/cognition/02-conscious.md §7.2). It is ADDITIVE: it sits alongside the existing
// Drives / DefaultMode generators in continuous.go (which it does NOT rewrite). Those generate the
// raw curiosity/wander stream; this file types WHAT a drive IS — its tier, the process drives, the
// development agenda they aim at, and the MintDriveGoal cross that turns (drive x domain) into a
// concrete DRIVE goal text.
//
// The three tiers mirror the human bodily / emotional / spiritual structure (§7.2):
//   - Homeostatic (bodily)   — durability + budget: keep the system alive and within bounds.
//   - Affective  (emotional) — arousal + value/salience: what matters now; the cognitive/process
//     drives + the development agenda sit in THIS span.
//   - Conscience (spiritual) — the governing values layer: discern good/bad, steward as designed.
//     It governs the affective span from above (the eval gate lives in conscience.go).
package cognition

import "strings"

// DriveTier is the 3-tier stratification of drives (§7.2). Ordinals are strictly increasing
// bodily -> emotional -> spiritual so "the conscience tier governs from above" is the highest tier.
type DriveTier int

const (
	TierHomeostatic DriveTier = iota // bodily: durability + budget (the regulator) — keep alive, in-bounds
	TierAffective                    // emotional: arousal + value/salience — what matters now (process drives live here)
	TierConscience                   // spiritual: discern good/bad, steward as designed — governs from above
)

var driveTierNames = map[DriveTier]string{
	TierHomeostatic: "Homeostatic",
	TierAffective:   "Affective",
	TierConscience:  "Conscience",
}

// String returns the tier name (e.g. "Affective").
func (t DriveTier) String() string {
	if n, ok := driveTierNames[t]; ok {
		return n
	}
	return "DriveTier(" + itoa(int(t)) + ")"
}

// DriveTiers returns the three tiers in human order (bodily -> emotional -> spiritual). The conscience
// tier is last/highest because it governs the affective span from above (§7.2).
func DriveTiers() []DriveTier {
	return []DriveTier{TierHomeostatic, TierAffective, TierConscience}
}

// ProcessDrive is a cognitive/developmental motivation — the *how* a DRIVE goal is generated (§7.2a).
// All four sit in the affective span and are governed from above by the conscience tier.
type ProcessDrive int

const (
	DriveSelfImprovement ProcessDrive = iota // refine its own skills/registries/weak spots
	DriveCuriosity                           // explore unexplored questions (wandering -> goal birth)
	DriveHelpfulness                         // anticipate what the user will need (proactive outreach)
	DriveCoherence                           // resolve contradictions; consolidate memory
)

var processDriveNames = map[ProcessDrive]string{
	DriveSelfImprovement: "SelfImprovement",
	DriveCuriosity:       "Curiosity",
	DriveHelpfulness:     "Helpfulness",
	DriveCoherence:       "Coherence",
}

// processDriveVerbs is the action phrasing each process drive contributes to a minted goal — the
// *how* half of MintDriveGoal (§7.2). Kept deterministic so the minted text is reproducible.
var processDriveVerbs = map[ProcessDrive]string{
	DriveSelfImprovement: "get better at",
	DriveCuriosity:       "explore an open question in",
	DriveHelpfulness:     "anticipate what the user will need in",
	DriveCoherence:       "consolidate and reconcile",
}

// String returns the process-drive name (e.g. "Curiosity").
func (d ProcessDrive) String() string {
	if n, ok := processDriveNames[d]; ok {
		return n
	}
	return "ProcessDrive(" + itoa(int(d)) + ")"
}

// Tier reports which drive tier a process drive sits in — always the affective span (§7.2): the
// cognitive/developmental drives are governed from above by the conscience tier.
func (d ProcessDrive) Tier() DriveTier { return TierAffective }

// ProcessDrives returns the four cognitive/developmental drives (§7.2a) in declared order.
func ProcessDrives() []ProcessDrive {
	return []ProcessDrive{DriveSelfImprovement, DriveCuriosity, DriveHelpfulness, DriveCoherence}
}

// AgendaDomain is a domain the development agenda points the drives AT — the *toward what* (§7.2b).
// The portfolio is deliberately balanced so the system grows human-like breadth, not a narrow savant.
type AgendaDomain int

const (
	DomainSTEM   AgendaDomain = iota // knowledge/science/technology/maths/engineering — the PRIMARY thrust
	DomainSocial                     // social / soft skills — BALANCING (kept, not optional)
)

var agendaDomainNames = map[AgendaDomain]string{
	DomainSTEM:   "STEM",
	DomainSocial: "Social",
}

// agendaThemes is the noun phrase each domain contributes to a minted goal — the subject the drive
// aims at (§7.2b). Used by MintDriveGoal and asserted by the agenda test (the domain must appear).
var agendaThemes = map[AgendaDomain]string{
	DomainSTEM:   "science, maths and engineering",
	DomainSocial: "social and interpersonal skill",
}

// agendaWeights are the product/vision dial (§7.2b): STEM primary (heavier), Social balancing but
// KEPT (positive, never zeroed). Tune these to steer what the system becomes.
var agendaWeights = map[AgendaDomain]float64{
	DomainSTEM:   0.7,
	DomainSocial: 0.3,
}

// String returns the domain name (e.g. "STEM").
func (a AgendaDomain) String() string {
	if n, ok := agendaDomainNames[a]; ok {
		return n
	}
	return "AgendaDomain(" + itoa(int(a)) + ")"
}

// Theme returns the noun phrase the domain contributes to a minted goal (the subject aimed at).
func (a AgendaDomain) Theme() string { return agendaThemes[a] }

// Weight returns the agenda weight (§7.2b) — STEM primary, Social balancing-but-kept.
func (a AgendaDomain) Weight() float64 { return agendaWeights[a] }

// IsPrimary reports whether this domain is the primary developmental thrust (STEM, §7.2b).
func (a AgendaDomain) IsPrimary() bool { return a == DomainSTEM }

// AgendaDomains returns the development-agenda domains, primary first (STEM), then balancing (Social).
func AgendaDomains() []AgendaDomain {
	return []AgendaDomain{DomainSTEM, DomainSocial}
}

// MintDriveGoal is the (process drive) x (domain) cross that turns a drive aimed at an agenda domain
// into a concrete DRIVE goal text (§7.2): e.g. curiosity x STEM => "explore an open question in
// science, maths and engineering". The text reflects BOTH factors — the drive's *how* (verb) and the
// domain's *toward what* (theme) — so a goal minted from this is a real cross, not a constant.
//
// This produces the goal TEXT only; the engine builds the Goal entity with Source=GoalDrive and
// authors its Acceptance (the drive's satisfaction condition, §1.6). It does NOT vet the result — the
// caller must pass it through VetAction (conscience.go) before pursuit (§7.2: "no drive goal or action
// is pursued without passing its discern-good/bad check").
func MintDriveGoal(drive ProcessDrive, domain AgendaDomain) string {
	verb := processDriveVerbs[drive]
	if verb == "" {
		verb = "develop"
	}
	theme := domain.Theme()
	if theme == "" {
		theme = strings.ToLower(domain.String())
	}
	return verb + " " + theme
}
