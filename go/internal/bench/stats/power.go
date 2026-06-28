package stats

import "math"

// Z returns the one-sided critical value z used in the power formula:
// z_alpha = Φ⁻¹(1 - alpha) for the chosen two-sided alpha (so pass alpha/2 if
// you mean two-sided per tail — see PowerN, which takes the two-sided alpha and
// halves it internally), and z_beta = Φ⁻¹(power).
//
// It is exported as the spec's "helper Z(alpha, power)" so callers can read the
// exact z's that went into an N. The first return is z for the significance
// level argument, the second is z for the power argument — both as upper-tail
// quantiles (positive for the usual alpha<0.5 / power>0.5).
func Z(alpha, power float64) (zAlpha, zBeta float64) {
	zAlpha = normalQuantile(1 - alpha)
	zBeta = normalQuantile(power)
	return
}

// PowerN solves the continuous-outcome power formula for the paired design
// (measuring-stick §4.5):
//
//	N ≈ (z_{α} + z_{β})² · σ_diff² / MDE²
//
// where z_{α} is the TWO-SIDED critical value (so alpha=0.05 => z=1.96) and
// z_{β} = Φ⁻¹(power). `sigmaDiff` is the SD of the per-pair difference (from the
// Phase-0 pilot) and `mde` is the minimum detectable effect in the same units.
// The result is rounded UP to a whole sample (you cannot run a fractional item).
//
// alpha is two-sided here: PowerN uses z_{α/2}. Pass alpha=0.05, power=0.90,
// sigmaDiff=sqrt(0.35)≈0.5916, mde=0.15 to reproduce the grounding bank's
// N≈163.
func PowerN(alpha, power, sigmaDiff, mde float64) int {
	zA := normalQuantile(1 - alpha/2)
	zB := normalQuantile(power)
	num := (zA + zB) * (zA + zB) * sigmaDiff * sigmaDiff
	n := num / (mde * mde)
	return int(math.Ceil(n))
}

// PowerNMcNemar solves the paired-binary (McNemar) power form
// (measuring-stick §4.5):
//
//	N ≈ p_disc · (z_{α} + z_{β})² / δ²
//
// where p_disc is the expected fraction of DISCORDANT pairs, δ is the
// within-pair pass-rate difference (the McNemar-form MDE), z_{α} is two-sided.
// Rounded up. This is the form §3.2 uses (p_d≈0.45, δ≈0.40 ⇒ N≈30 discordant ⇒
// ~120 paired items after accounting for p_disc).
func PowerNMcNemar(alpha, power, pDisc, delta float64) int {
	zA := normalQuantile(1 - alpha/2)
	zB := normalQuantile(power)
	n := pDisc * (zA + zB) * (zA + zB) / (delta * delta)
	return int(math.Ceil(n))
}
