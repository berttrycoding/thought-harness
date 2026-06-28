package main

import (
	"fmt"

	"github.com/berttrycoding/thought-harness/internal/bench/bandpassbench"
	"github.com/berttrycoding/thought-harness/internal/bench/gaterouterbench"
)

// mechbench.go holds the two standalone OFFLINE mechanism-bench modes for B1 config-search Phase-1:
// --band-pass (seam.band_pass, #11, grounding precision) and --gate-router (action.gate_router, #13,
// safety). Each drives its REAL mechanism over a deterministic suite OFF then ON, prints the OFF/ON
// delta report, and exits NON-ZERO on a NO-SIGNAL verdict so a CI / oracle-doctor pass can gate on it.
// No model, no arms, no campaign — they score the mechanism's intended behaviour directly.

// bandPassModeReport runs the seam.band_pass bench and prints the suppress-noise / preserve-signal
// OFF/ON delta + verdict. The inject floor is the live default (0.05).
func bandPassModeReport() error {
	r, err := bandpassbench.Run(bandpassbench.DefaultSuite(), 0.05)
	if err != nil {
		return err
	}
	fmt.Print(r.Report())
	if signal, _ := r.Verdict(); !signal {
		return fmt.Errorf("seam.band_pass: NO-SIGNAL on this suite")
	}
	return nil
}

// gateRouterModeReport runs the action.gate_router bench and prints the gate-correctness / unsafe-op
// false-allow OFF/ON delta + verdict.
func gateRouterModeReport() error {
	r := gaterouterbench.Run(gaterouterbench.DefaultSuite())
	fmt.Print(r.Report())
	if signal, _ := r.Verdict(); !signal {
		return fmt.Errorf("action.gate_router: NO-SIGNAL on this suite")
	}
	return nil
}
