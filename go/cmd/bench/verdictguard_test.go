package main

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/decisionoracle"
)

// TestVerdictContractGuard pins A2 fix #6: a sub-threshold verdict-line present-rate fails LOUD
// (a boxed banner + bad=true so the caller exits non-zero), while an at/above-threshold rate is
// silent. This is the guard that stops format-disobedience from being silently reported-and-
// ignored while the prose fallback re-imports the whack-a-mole. It is mutation-sensitive: it
// asserts the EXACT bad/good split around the floor and that the banner names the offending worker.
func TestVerdictContractGuard(t *testing.T) {
	// A worker obeying the contract on every fixture (present-rate 1.0) — no banner.
	good := []decisionoracle.Result{
		{ID: "a", Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
		{ID: "b", Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
	}
	if banner, bad := verdictContractGuard(good); bad {
		t.Errorf("a 1.0 present-rate must not trip the guard; banner=%q", banner)
	}

	// A worker disobeying often (1 of 3 lines present = 0.333 < 0.8) — LOUD fail.
	bad := []decisionoracle.Result{
		{ID: "a", Worker: decisionoracle.WorkerVerifier, VerdictLinePresent: true},
		{ID: "b", Worker: decisionoracle.WorkerVerifier, VerdictLinePresent: false},
		{ID: "c", Worker: decisionoracle.WorkerVerifier, VerdictLinePresent: false},
	}
	banner, tripped := verdictContractGuard(bad)
	if !tripped {
		t.Fatalf("a 0.333 present-rate (< %.2f) must trip the guard", verdictLinePresentFloor)
	}
	if !strings.Contains(banner, "VERDICT-CONTRACT DISOBEYED") {
		t.Errorf("banner must name the failure: %q", banner)
	}
	if !strings.Contains(banner, string(decisionoracle.WorkerVerifier)) {
		t.Errorf("banner must name the offending worker: %q", banner)
	}

	// Exactly AT the floor (4 of 5 = 0.8) is NOT below it — the strict `<` floor passes here.
	atFloor := []decisionoracle.Result{
		{Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
		{Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
		{Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
		{Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: true},
		{Worker: decisionoracle.WorkerDeliberator, VerdictLinePresent: false},
	}
	if _, tripped := verdictContractGuard(atFloor); tripped {
		t.Errorf("a present-rate exactly at the %.2f floor must NOT trip (strict `<`)", verdictLinePresentFloor)
	}
}
