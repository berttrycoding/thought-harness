// plangate.go is the DEV-SIDE keep-step gate: `thought plangate` (Track O / O-2). It is the harness
// dogfooding its own event bus on the dev side — the plan-carries-a-falsifiable-gate symbol-audit that
// `gate-commit` runs before a KEEP. A build plan declares a CONTRACT (a producers_files symbol map +
// acceptance_checks regexes); this command computes the ACTUAL diff (via `git diff` at this CLI I/O
// boundary — the engine package never shells out) and runs internal/plangate.Audit, emitting the
// plangate.* events on the bus and exiting NON-ZERO on a FAIL so an autonomous keep cannot proceed when
// a declared symbol did not land.
//
// Default OFF: the dev.plan_gate knob is opt-in (default OFF). With it OFF this command short-circuits
// to a no-op PASS (it emits a config.skip and exits 0), so the dev-side default path is byte-identical
// and silent — the gate runs ONLY when a build asks for it (--enable dev.plan_gate).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/plangate"
	"github.com/berttrycoding/thought-harness/internal/trace"
)

// cmdPlanGate runs the plan-gate symbol-audit. Flags:
//
//	--contract FILE   the contract JSON ({plan, producers_files:[{file,symbols}], acceptance_checks:[regex]})
//	--cached          audit the STAGED diff (git diff --cached) instead of the working tree
//	--enable/--disable/--config/--profile   the feature flags (the gate is behind dev.plan_gate, default OFF)
//	--log FILE.jsonl  persist the plangate.* events as JSONL
//	--quiet           suppress the console tracer
//
// Exit codes: 0 = PASS (or gate OFF / no-op), 1 = FAIL (a declared symbol/regex did not land), 2 = usage/IO error.
func cmdPlanGate(argv []string) int {
	fs := flag.NewFlagSet("plangate", flag.ContinueOnError)
	var ff featureFlags
	var lf logFlags
	addFeatureFlags(fs, &ff)
	addLogFlags(fs, &lf)
	contract := fs.String("contract", "", "contract JSON: {plan, producers_files:[{file,symbols}], acceptance_checks:[regex]}")
	cached := fs.Bool("cached", false, "audit the STAGED diff (git diff --cached) instead of the working tree")
	quiet := fs.Bool("quiet", false, "suppress the console tracer")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	cfg, err := resolveFeatures(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plangate:", err)
		return 2
	}

	// One bus per command (the engine isn't built here). Wire the console tracer + optional JSONL sink so
	// the plangate.* (and config.skip) events render exactly like any other subsystem's stream.
	bus := events.NewDefault()
	tracer := trace.NewConsoleTracer(os.Stdout, *quiet, nil, parseLayers(lf.layer))
	bus.Subscribe(tracer.On)
	if strings.TrimSpace(lf.logPath) != "" {
		sink, err := trace.NewJsonlSink(lf.logPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "plangate: --log:", err)
			return 2
		}
		defer sink.Close()
		bus.Subscribe(sink.On)
	}
	emit := bus.Emit

	// Default OFF ⇒ no-op PASS. Surface a config.skip (Rule 4: a disabled component is never silent) and
	// exit 0 so the dev-side default path is byte-identical to not having the gate at all.
	if !cfg.Dev.PlanGate {
		emit(events.ConfigSkip, "plan-gate disabled (dev.plan_gate OFF) — no-op PASS",
			events.D{"component": "plangate", "reason": "dev.plan_gate off; enable with --enable dev.plan_gate"})
		return 0
	}

	if strings.TrimSpace(*contract) == "" {
		fmt.Fprintln(os.Stderr, "plangate: --contract FILE is required when the gate is enabled")
		return 2
	}
	raw, err := os.ReadFile(*contract)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plangate: read contract:", err)
		return 2
	}
	var c plangate.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Fprintln(os.Stderr, "plangate: parse contract JSON:", err)
		return 2
	}

	// Compute the actual diff at this CLI I/O boundary (git lives here, NOT in the engine package).
	diffText, err := gitDiff(*cached)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plangate: git diff:", err)
		return 2
	}
	diff := plangate.ParseUnified(diffText)

	gate := plangate.New(emit)
	v := gate.Audit(c, diff)
	if !v.Pass {
		// The verdict event already named the misses; a stderr line makes the refusal loud for the keep step.
		fmt.Fprintf(os.Stderr, "plangate: KEEP REFUSED (%s): %d of %d audited lines did not land\n",
			v.Plan, len(v.Missing), len(v.Lines))
		return 1
	}
	return 0
}

// gitDiff returns the unified diff of the working tree (or the staged index when cached). It is the
// single git shell-out, kept at the CLI boundary so internal/plangate stays headless-pure (pure text
// in, verdict out). Untracked files are not in `git diff`; the gate audits CHANGES to tracked files,
// which is where a declared symbol must land (a brand-new file is added/staged to be tracked first).
func gitDiff(cached bool) (string, error) {
	args := []string{"diff", "--no-color"}
	if cached {
		args = append(args, "--cached")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
