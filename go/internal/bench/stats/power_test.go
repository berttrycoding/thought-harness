package stats

import (
	"math"
	"testing"
)

func TestZ(t *testing.T) {
	// z_alpha for two-sided 0.05 via Z(0.025,*) = 1.959963..., z_beta(0.90)=1.2816.
	za, _ := Z(0.025, 0.90)
	if !approx(za, 1.959963984540054, 1e-6) {
		t.Errorf("z_alpha(0.025) = %v, want 1.95996", za)
	}
	_, zb := Z(0.05, 0.90)
	if !approx(zb, 1.2815515594, 1e-6) {
		t.Errorf("z_beta(power=0.90) = %v, want 1.28155", zb)
	}
	_, zb80 := Z(0.05, 0.80)
	if !approx(zb80, 0.8416212336, 1e-6) {
		t.Errorf("z_beta(power=0.80) = %v, want 0.84162", zb80)
	}
}

func TestPowerN(t *testing.T) {
	// Grounding bank (measuring-stick §3.1): alpha=0.05 two-sided, power=0.90,
	// sigma_diff^2 = p_disc = 0.35, MDE = 0.15. The formula gives N≈163.4 -> 164
	// after ceil (the spec quotes ≈163, rounding the same z's; the bank rounds
	// up to 200).
	n := PowerN(0.05, 0.90, math.Sqrt(0.35), 0.15)
	if n != 164 {
		t.Errorf("grounding PowerN = %d, want 164 (~163 in spec)", n)
	}
	// Halving the MDE quadruples N (the spec's "lowering MDE multiplies N
	// quadratically").
	nHalf := PowerN(0.05, 0.90, math.Sqrt(0.35), 0.075)
	if nHalf < 4*n-4 || nHalf > 4*n+4 {
		t.Errorf("halving MDE should ~4x N: got %d vs 4*%d", nHalf, n)
	}
}

func TestPowerNMcNemar(t *testing.T) {
	// Retrace bank (measuring-stick §3.2): p_disc≈0.45, delta≈0.40, two-sided
	// 0.05, power 0.90 -> ~30 discordant pairs.
	n := PowerNMcNemar(0.05, 0.90, 0.45, 0.40)
	if n != 30 {
		t.Errorf("retrace discordant PowerNMcNemar = %d, want 30", n)
	}
}
