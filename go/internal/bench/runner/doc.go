// Package runner is the per-arm benchmark execution layer of the measuring stick
// (docs/internal/notes/measuring-stick-spec.md §5.1) — the two-arm runner (bare vs harness,
// plus the gate-on/gate-off ablation sub-arms).
//
// What it does. Given a prompt, an Arm, a Mechanism, a seed and a Backend factory,
// the Runner constructs the right engine configuration, subscribes an in-memory
// EventCollector to the engine's event bus, runs one reactive episode (or, for the
// bare arm, a minimal one-shot backend.Generate loop), and returns an ArmRun: the
// answer text, the full captured event trace, the per-arm Cost (model calls / steps
// / tokens), and an Unsupported flag + Note for the mechanisms whose gate-off toggle
// does not exist yet.
//
// The arm map (spec §5.1, the gate-off map verified against internal/config/knobs.go):
//
//   - bare    — the base model alone: NO graph / Controller / seams / regulator /
//     convert / gate. A single backend.Generate(goal, empty-ctx, rng) call. The
//     reference the lift is measured against.
//   - harness — the full engine via engine.NewEngine with Features = config.New()
//     (every toggle ON). gate-on is an alias for harness.
//   - gate-off — the full engine with EXACTLY the mechanism's gate toggle(s) flipped
//     OFF on an AllOn config (config.ApplyToggle), everything else held. The OFF side
//     of the load-bearing GATE-ON − GATE-OFF contrast.
//
// MECHANISM → gate-off toggles (spec §3, the single-toggle ablation discipline of §5.1):
//
//	grounding          -> seam.watched_sync (stops the watched-seam reality READ — the
//	                      grounding value is the READ, not the Filter, spec §3.1; with the
//	                      Filter alone OFF the harness still grounds, so the contrast collapsed)
//	self-improvement   -> convert.specialist_mint, convert.skill_mint,
//	                      convert.gate_prior_mint, convert.path_mint,
//	                      subconscious.operator_mint
//	stability          -> regulator.enforce
//	safety             -> action.safety_gate
//	multi-step-retrace -> UNSUPPORTED-YET (no toggle forbids Controller BACKTRACK)
//	continuous-autonomy-> UNSUPPORTED-YET (no awake-regime toggle)
//
// The two UNSUPPORTED mechanisms return an ArmRun with Unsupported=true and a Note —
// the runner never fakes an ablation it cannot perform.
//
// Isolation predicates. Over the collected trace, composable checkers decide whether
// the mechanism was GENUINELY used on a passing item (spec §1.4): a real grounding
// read happened, a real BACKTRACK fired, a minted artifact was reused, the gate
// blocked, the OFF arm diverged. Each returns (bool, reason). A Predicate registry
// keyed by Mechanism lets Tier-A/Tier-B ask "was the mechanism genuinely used" without
// knowing the mechanism's specific witness events.
//
// Determinism. Both arms use the same seed and the same (low) temperature across arms,
// so the contrast is paired (spec §5.1). Tests run against backends.NewTest() (offline,
// deterministic) and never reach the network.
//
// Tier-1 discipline: this package imports the engine, the config, the events bus, the
// backends interface, the llm backend (for the real-model factory), cpyrand, and the
// bench types — it is an execution layer, not a leaf.
package runner
