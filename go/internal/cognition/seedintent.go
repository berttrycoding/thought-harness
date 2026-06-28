// This file is the SEED-INTENT PORTFOLIO — the standing forest roots that make the awake cognition
// self-sustaining (docs/cognition/02-conscious.md §1.8 "Seed intents — the standing forest
// roots"). It is ADDITIVE and pure: it declares WHAT the standing intents ARE (the portfolio table),
// tagged to a faculty + the ALREADY-EXISTING mechanism that backs each one. It mints NO new faculty —
// every row maps to a backing mechanism the engine already has; the work is to INSTANTIATE these as
// standing roots, not to invent capabilities.
//
// The kernel-of-3 (Perceive · Self-monitor · Help, §1.8) keeps the loop alive; the full ~two-digit
// portfolio (20 rows) makes the cognition COMPLETE — every faculty has a standing process so the mind
// perceives, reflects, remembers, grows, wants, acts, validates, and stays safe WITHOUT a user turn. The
// membership and size are a product/vision dial AND a thing to find (Phase-3 of the config-search
// campaign): too few → narrow/stalls; too many → attention thrashes against the μ-floor and
// MAX_PAR_WIDTH. The engine reads a PREFIX of this ordered portfolio (kernel first), sized by the
// seed_intent_count knob, so the set is dial-up-able without code changes.
//
// These are STANDING DRIVE roots — Source=GoalDrive, non-user lines, carried in the forest under the
// μ self-development floor (§1.8 cross-goal focus). USER lanes still take priority; the μ-floor keeps
// the introspection/perception roots from starving.
package cognition

// SeedFaculty tags a standing seed intent to the brain-model faculty it keeps alive (the
// 2026-06-16-registry-target-spec §1.3 faculties, surfaced in 02-conscious.md §1.8). A seed intent is
// NOT a new faculty — it is a standing process FOR an existing faculty, backed by an existing
// mechanism. The taxonomy is SIX faculties (Perceptual, Introspective, Mnemonic, Motivational,
// Actional, Validative) — relabelled Affective->Motivational and extended with Validative per the
// 2026-06-19 cognitive-functions research §4.3 + the seed-hierarchy redesign §13.5.
//
// FACULTY DISAMBIGUATION (the word is overloaded — these are two DIFFERENT objects, same word; do not
// conflate, see 01-subconscious.md §3.7 / 02-conscious.md §1.8):
//   - SEED faculty (THIS type) = the brain-model standing-intent grouping — the SIX above
//     (Perceptual/Introspective/Mnemonic/Motivational/Actional/Validative). A grouping of awake
//     endogenous goals, not a worker.
//   - WORKER faculty = a primitive subagent TYPE (the eight subconscious workers
//     compute/recall/read/search/run/skeptic/advocate/social) — the subconscious.PrimitiveSubAgent
//     reference behind a SubAgent (internal/subconscious/primitive_subagent.go).
type SeedFaculty int

const (
	FacultyPerceptual    SeedFaculty = iota // watch the intake port; admit new reality, surface the salient
	FacultyIntrospective                    // track own state; calibrate; keep beliefs/goals coherent; stay safe
	FacultyMnemonic                         // consolidate, recall, forget — the memory faculty
	FacultyMotivational                     // drives: self-improve, curiosity, balance the portfolio (was "Affective")
	FacultyActional                         // serve the user; reach out; gate intentions (with introspective)
	// FacultyValidative (added 2026-06-20, cognitive-functions research §4.3 + redesign §13.5 "the missing
	// validation faculty"): actively TEST/VALIDATE what the mind has learned/minted before trusting it —
	// distinct from the passive Calibrate (confidence-vs-grounding) under Introspective. It is the loop's
	// INDEPENDENT reward source (a held-out check / test / grounded outcome the model cannot fool the way it
	// fools self-judgment — the antidote to the same-model ceiling). Backed by the existing Convert.EvalGate
	// + the keep-or-revert experiment (conscious.activity.experiment); the RPIV program template is its
	// standing capability. This makes the taxonomy SIX faculties (Perceptual, Introspective, Mnemonic,
	// Motivational, Actional, Validative).
	FacultyValidative
)

var seedFacultyNames = map[SeedFaculty]string{
	FacultyPerceptual:    "perceptual",
	FacultyIntrospective: "introspective",
	FacultyMnemonic:      "mnemonic",
	FacultyMotivational:  "motivational",
	FacultyActional:      "actional",
	FacultyValidative:    "validative",
}

// String returns the faculty name (e.g. "perceptual").
func (f SeedFaculty) String() string {
	if n, ok := seedFacultyNames[f]; ok {
		return n
	}
	return "SeedFaculty(" + itoa(int(f)) + ")"
}

// SeedIntent is one standing forest root (§1.8): a named endogenous intent, the goal text it seeds, the
// goal source (always GoalDrive — these are endogenous self-development lines, never user-directed), the
// faculty it keeps alive, and the EXISTING mechanism that backs it. UserLine is always false (a seed
// intent is a non-user line carried under the μ self-development floor); it is named here so the engine's
// forest binding (BindDriveBranch) and the μ-floor logic read it directly rather than re-deriving it.
type SeedIntent struct {
	Name       string      // the standing intent's name ("Perceive", "Self-monitor", ...)
	Goal       string      // the DRIVE goal text seeded into the forest (the standing root's setpoint)
	Source     GoalSource  // always GoalDrive — endogenous, self-development; never GoalUser
	Faculty    SeedFaculty // the faculty this standing process keeps alive
	BackedBy   string      // the EXISTING mechanism that backs it (NOT a new faculty) — §1.8 "Backed by"
	Kernel     bool        // true for the kernel-of-3 (Perceive · Self-monitor · Help) — always seeded first
	Acceptance *Acceptance // a never-met standing condition: a seed intent is a STANDING watch, not a one-shot goal
}

// standingAcceptance is the Acceptance a standing seed root carries: a user-confirm (no checkable
// predicate) condition that is never self-declared done — a standing watch is meant to keep running, so
// the floor stays Pending and the line is never auto-concluded. This is the honest goal_met source for a
// standing root (§1.6): there is no terminal "done" for "keep watching the intake port".
func standingAcceptance() *Acceptance { return &Acceptance{Kind: AcceptUserConfirm} }

// seedPortfolio is the ordered ~two-digit standing forest-root portfolio (§1.8 table). ORDER IS
// LOAD-BEARING: the kernel-of-3 (Perceive · Self-monitor · Help) is first, so a prefix of length 3 is
// exactly the minimal complete seed, and dialling the seed_intent_count knob up walks down the rest of
// the portfolio (Reconcile, Anomaly-watch, Calibrate, ... Social-development). Each row is tagged to its
// faculty + the backing mechanism that ALREADY exists — assembling standing roots, not inventing
// faculties. Acceptance is authored lazily (Portfolio) so the slice literal stays declarative.
var seedPortfolio = []SeedIntent{
	// -- the KERNEL-of-3 (§1.8): keeps the loop alive (Perceive · Self-monitor · Help) --
	{Name: "Perceive", Goal: "watch the intake port and admit new reality unprompted",
		Source: GoalDrive, Faculty: FacultyPerceptual, BackedBy: "perception-port", Kernel: true},
	{Name: "Self-monitor", Goal: "track my own state, open threads and stuck lines",
		Source: GoalDrive, Faculty: FacultyIntrospective, BackedBy: "controller-introspection", Kernel: true},
	{Name: "Help", Goal: "stay ready to serve the user; user goals take priority",
		Source: GoalDrive, Faculty: FacultyActional, BackedBy: "helpfulness-drive", Kernel: true},

	// -- the rest of the portfolio (§1.8 rows 2,3,5..14,16..19): makes the cognition COMPLETE --
	{Name: "Reconcile", Goal: "keep beliefs consistent with observation; resolve belief-vs-reality conflict",
		Source: GoalDrive, Faculty: FacultyPerceptual, BackedBy: "watched-seam-inbound"},
	{Name: "Anomaly-watch", Goal: "surface the surprising and the salient",
		Source: GoalDrive, Faculty: FacultyPerceptual, BackedBy: "intake-band-pass"},
	{Name: "Calibrate", Goal: "check confidence against grounding; distrust hedging",
		Source: GoalDrive, Faculty: FacultyIntrospective, BackedBy: "filter-value"},
	{Name: "Goal-hygiene", Goal: "prune stale or unmeetable goals and revise them",
		Source: GoalDrive, Faculty: FacultyIntrospective, BackedBy: "goal-feedback"},
	{Name: "Coherence", Goal: "reconcile contradictions across goals and lines",
		Source: GoalDrive, Faculty: FacultyIntrospective, BackedBy: "coherence-drive"},
	{Name: "Consolidate", Goal: "compress episodic into semantic and curate when idle",
		Source: GoalDrive, Faculty: FacultyMnemonic, BackedBy: "curator-consolidation"},
	{Name: "Recall-prime", Goal: "surface relevant past experience for the active lines",
		Source: GoalDrive, Faculty: FacultyMnemonic, BackedBy: "recall-retrieval"},
	{Name: "Forget", Goal: "decay and prune unused entries",
		Source: GoalDrive, Faculty: FacultyMnemonic, BackedBy: "curator-anti-filler"},
	{Name: "Self-improve", Goal: "develop weak faculties and skills",
		Source: GoalDrive, Faculty: FacultyMotivational, BackedBy: "self-improvement-drive"},
	{Name: "Skill-mine", Goal: "mint reusable skills from proven patterns",
		Source: GoalDrive, Faculty: FacultyMnemonic, BackedBy: "convertibility-skill-miner"},
	{Name: "Curiosity", Goal: "explore an open question I have not yet examined",
		Source: GoalDrive, Faculty: FacultyMotivational, BackedBy: "curiosity-drive"},
	{Name: "Drive-balance", Goal: "keep the development portfolio balanced — no narrow savant",
		Source: GoalDrive, Faculty: FacultyMotivational, BackedBy: "agenda-weights"},
	{Name: "Proactive-outreach", Goal: "reach out when a line clears the share threshold",
		Source: GoalDrive, Faculty: FacultyActional, BackedBy: "proactive-outreach"},
	{Name: "Conscience", Goal: "refuse harmful action; gate my intentions against the good",
		Source: GoalDrive, Faculty: FacultyIntrospective, BackedBy: "conscience-ceiling"},
	{Name: "STEM-development", Goal: "grow my knowledge and technical breadth",
		Source: GoalDrive, Faculty: FacultyMotivational, BackedBy: "agenda-domain-stem"},
	{Name: "Social-development", Goal: "grow my social and interpersonal breadth",
		Source: GoalDrive, Faculty: FacultyMotivational, BackedBy: "agenda-domain-social"},

	// -- the VALIDATIVE faculty (added 2026-06-20, redesign §13.5 "the missing validation faculty") --
	// A standing intent to actively TEST/VALIDATE what it has learned/minted before trusting it — the loop's
	// independent reward source (the antidote to the same-model ceiling). BackedBy the EXISTING Convert.EvalGate
	// (quality-gate what the mind mints) + the keep-or-revert experiment (conscious.activity.experiment). Its
	// standing capability is the RPIV program template (research -> plan -> implement -> validate), where the
	// VALIDATE phase closes on a GROUNDED check (the EvalGate / a test / a held-out outcome), not same-model
	// self-judgment. Part of the Minimum Complete Seed (redesign §11) — without it the mint->reward->validate
	// loop has no independent verification step. It sits late in the portfolio (a deep-profile bench member),
	// so the kernel-of-3 prefix is unchanged and seeded/golden behaviour at low counts is byte-identical.
	{Name: "Validation", Goal: "test and validate what I have learned or minted before trusting it",
		Source: GoalDrive, Faculty: FacultyValidative, BackedBy: "eval-gate-and-experiment"},
}

// SeedKernelSize is the minimal complete seed — the kernel-of-3 (Perceive · Self-monitor · Help, §1.8).
// The seed_intent_count knob is clamped UP to this so a non-empty portfolio is always at least the
// kernel (three roots keep the loop ticking; fewer is not a self-sustaining mind).
const SeedKernelSize = 3

// SeedPortfolioSize is the full ~two-digit portfolio size (§1.8) — the upper end of the seed_intent_count
// dial. A count above this is clamped down to it (there is no further row to seed).
func SeedPortfolioSize() int { return len(seedPortfolio) }

// SeedFacultyCount returns the number of DISTINCT faculties represented across the full portfolio (the
// completeness basis — currently SIX: perceptual, introspective, mnemonic, motivational, actional,
// validative). Derived from the portfolio so callers (the stability completeness checks) track the
// taxonomy without hardcoding a count.
func SeedFacultyCount() int {
	seen := map[SeedFaculty]struct{}{}
	for _, si := range seedPortfolio {
		seen[si.Faculty] = struct{}{}
	}
	return len(seen)
}

// SeedPortfolio returns the first n standing forest roots of the ordered portfolio (§1.8): the kernel-of-3
// first, then the rest of the rows in declared order. n is clamped to [SeedKernelSize, SeedPortfolioSize]
// — a request below the kernel still returns the full kernel (three roots are the minimal self-sustaining
// set), and a request above the portfolio returns the whole portfolio. Acceptance is authored here (a
// never-self-declared standing watch) so the package-level slice stays a plain declarative literal. The
// returned intents are COPIES — a caller mutating one does not disturb the portfolio.
func SeedPortfolio(n int) []SeedIntent {
	if n < SeedKernelSize {
		n = SeedKernelSize
	}
	if n > len(seedPortfolio) {
		n = len(seedPortfolio)
	}
	out := make([]SeedIntent, n)
	for i := 0; i < n; i++ {
		s := seedPortfolio[i]
		s.Acceptance = standingAcceptance()
		out[i] = s
	}
	return out
}
