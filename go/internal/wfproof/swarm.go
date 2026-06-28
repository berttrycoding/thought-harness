package wfproof

// swarm.go encodes lathe's /swarm -> /sprint -> chain hierarchy in the unified structure, using ALL
// four roles (skill / script / hook / dispatch). This is the by-construction proof that our substrate
// captures lathe's WHOLE workflow — not just the skills.
//
// The shape, from the lathe survey:
//
//   swarm    Par over N disjoint clusters; each cluster SPAWNS a sprint session (dispatch, not inline)
//   sprint   Seq of waves; each wave is a Par over items; each item SPAWNS its kind->chain
//   chain    the kind->chain of steps: skills (research/implement/critic), scripts (contract-check/
//            verify/done), a bounded recovery loop, with hooks gating it (attendance block, contract
//            floor, inject-consults, chain-invariants on stop)
//
// Each level is its OWN bounded workflow. The swarm is large; no single workflow is.

// skill builds an Agentic step (a sub-agent applies an operator), optionally with hooks.
func skill(op, domain string, guards ...Guard) Step {
	return Step{Op: op, Kind: Agentic, Domain: domain, Guards: guards}
}

// script builds a Deterministic step (a pure calculable function), optionally with hooks.
func script(op, domain string, guards ...Guard) Step {
	return Step{Op: op, Kind: Deterministic, Domain: domain, Guards: guards}
}

// LatheSwarm builds the full library and returns it plus the root workflow name. Dispatch resolves
// names against this library; VerifyLibrary checks the whole thing is bounded + acyclic.
func LatheSwarm() (Library, string) {
	lib := Library{}

	// --- chain:feature — the kind->chain for a feature item (dispatch-map.yaml) ----------------
	// skills do the thinking; scripts do the mechanical checks; hooks gate the chain.
	lib["chain:feature"] = &Workflow{
		Name: "chain:feature",
		Root: Seq{Children: []Node{
			skill("research", "feature"),
			skill("plan-p", "feature"),
			skill("implement", "feature"),
			// the critic step is gated by TWO hooks: a mechanical contract-check that floors the
			// verdict (no LLM override), and the post-critic attendance gate that blocks if skipped.
			skill("critic", "feature",
				Guard{On: OnPre, Check: "contract-check", Action: Floor,
					Note: "mechanical structural mismatch floors the verdict; LLM cannot override"},
				Guard{On: OnPre, Check: "post-critic-attendance", Action: Block,
					Note: "halt (exit 2) if a kind that requires critic skipped it (BUG-1025)"}),
			// recovery loop: bounded retries until the critic passes (lathe's <=2 retries).
			Loop{Body: Seq{Children: []Node{skill("recover", "feature")}},
				Until: "critic ok", MaxIter: 2},
			script("verify", "feature"),
			script("done", "feature"),
		}},
		Hooks: []Guard{
			{On: OnPre, Check: "inject-consults", Action: Inject,
				Note: "inject pending sprint consults into the session context"},
			{On: OnStop, Check: "chain-invariants", Action: Floor,
				Note: "I3/I4 chain-step pairing + claim freshness; floors on violation"},
		},
	}

	// --- chain:bug — a shorter chain, proving the kind->chain map is just many named workflows -----
	lib["chain:bug"] = &Workflow{
		Name: "chain:bug",
		Root: Seq{Children: []Node{
			skill("bugfix", "bug"),
			skill("critic", "bug",
				Guard{On: OnPre, Check: "contract-check", Action: Floor}),
			script("verify", "bug"),
			script("done", "bug"),
		}},
		Hooks: []Guard{
			{On: OnStop, Check: "chain-invariants", Action: Floor},
		},
	}

	// --- sprint — waves of items; each item SPAWNS its chain (dispatch, not inline) ----------------
	// wave 1 (two feature items, parallel) then wave 2 (a feature + a bug, parallel). depends_on is
	// modelled by the wave boundary: wave 2 runs only after wave 1.
	lib["sprint"] = &Workflow{
		Name: "sprint",
		Root: Seq{Children: []Node{
			Par{Children: []Node{
				Dispatch{Workflow: "chain:feature", Over: "item T-1001"},
				Dispatch{Workflow: "chain:feature", Over: "item T-1002"},
			}},
			Par{Children: []Node{
				Dispatch{Workflow: "chain:feature", Over: "item T-1003"},
				Dispatch{Workflow: "chain:bug", Over: "item T-1004"},
			}},
		}},
		Hooks: []Guard{
			{On: OnStop, Check: "sprint-validate", Action: Floor,
				Note: "six structural assertions: retro present, no orphan claims, tracker==JSONL"},
		},
	}

	// --- swarm — N parallel sprint SESSIONS over disjoint clusters (dispatch, not inline) ----------
	lib["swarm"] = &Workflow{
		Name: "swarm",
		Root: Par{Children: []Node{
			Dispatch{Workflow: "sprint", Over: "cluster A"},
			Dispatch{Workflow: "sprint", Over: "cluster B"},
			Dispatch{Workflow: "sprint", Over: "cluster C"},
		}},
		Hooks: []Guard{
			{On: OnStop, Check: "watchdog", Action: Block,
				Note: "25-min watchdog kills a stuck session"},
		},
	}

	return lib, "swarm"
}
