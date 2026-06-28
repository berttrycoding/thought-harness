package cognition

import (
	"strings"
	"testing"
)

// TestDriveTierStratification pins the 3-tier model (§7.2): Homeostatic | Affective | Conscience,
// ordered bodily -> emotional -> spiritual, with the cognitive/process drives sitting in the
// affective span and governed from above by the conscience tier.
func TestDriveTierStratification(t *testing.T) {
	tiers := DriveTiers()
	if len(tiers) != 3 {
		t.Fatalf("want 3 drive tiers, got %d", len(tiers))
	}
	// ordering: Homeostatic (bodily) < Affective (emotional) < Conscience (spiritual).
	if tiers[0] != TierHomeostatic || tiers[1] != TierAffective || tiers[2] != TierConscience {
		t.Fatalf("tier order wrong: %v", tiers)
	}
	for _, tr := range tiers {
		if tr.String() == "" {
			t.Fatalf("tier %d has empty name", int(tr))
		}
	}
	// the conscience tier governs from above — it is the top (highest ordinal) tier.
	if TierConscience <= TierAffective || TierAffective <= TierHomeostatic {
		t.Fatal("tiers must be strictly increasing bodily->emotional->spiritual")
	}
}

// TestProcessDrivesSet pins the four cognitive/developmental process drives (§7.2a):
// SelfImprovement, Curiosity, Helpfulness, Coherence — and that every one sits in the affective span.
func TestProcessDrivesSet(t *testing.T) {
	drives := ProcessDrives()
	want := map[ProcessDrive]bool{
		DriveSelfImprovement: false,
		DriveCuriosity:       false,
		DriveHelpfulness:     false,
		DriveCoherence:       false,
	}
	if len(drives) != len(want) {
		t.Fatalf("want %d process drives, got %d", len(want), len(drives))
	}
	for _, d := range drives {
		if _, ok := want[d]; !ok {
			t.Fatalf("unexpected process drive %v", d)
		}
		want[d] = true
		if d.Tier() != TierAffective {
			t.Fatalf("process drive %v must sit in the affective span, got %v", d, d.Tier())
		}
		if d.String() == "" {
			t.Fatalf("process drive %d has empty name", int(d))
		}
	}
	for d, seen := range want {
		if !seen {
			t.Fatalf("process drive %v missing from ProcessDrives()", d)
		}
	}
}

// TestDevelopmentAgenda pins the development agenda (§7.2b): STEM primary, Social balancing-but-kept.
func TestDevelopmentAgenda(t *testing.T) {
	domains := AgendaDomains()
	if len(domains) != 2 {
		t.Fatalf("want 2 agenda domains, got %d", len(domains))
	}
	if domains[0] != DomainSTEM || domains[1] != DomainSocial {
		t.Fatalf("agenda order wrong (STEM primary, Social balancing): %v", domains)
	}
	if !DomainSTEM.IsPrimary() {
		t.Fatal("STEM must be the primary developmental thrust")
	}
	if DomainSocial.IsPrimary() {
		t.Fatal("Social is balancing, not primary")
	}
	// balancing weight is kept (positive), not zeroed/optional.
	if DomainSocial.Weight() <= 0 {
		t.Fatalf("Social weight must be kept (>0), got %v", DomainSocial.Weight())
	}
	if DomainSTEM.Weight() <= DomainSocial.Weight() {
		t.Fatal("STEM weight must exceed Social (primary vs balancing)")
	}
}

// TestMintDriveGoal is the (process drive) x (domain) -> goal text product (§7.2): a concrete DRIVE
// goal is a process drive aimed at an agenda domain. The minted text must mention both factors.
func TestMintDriveGoal(t *testing.T) {
	cases := []struct {
		drive  ProcessDrive
		domain AgendaDomain
	}{
		{DriveCuriosity, DomainSTEM},
		{DriveSelfImprovement, DomainSocial},
		{DriveHelpfulness, DomainSTEM},
		{DriveCoherence, DomainSocial},
	}
	for _, c := range cases {
		text := MintDriveGoal(c.drive, c.domain)
		if strings.TrimSpace(text) == "" {
			t.Fatalf("MintDriveGoal(%v,%v) returned empty", c.drive, c.domain)
		}
		lt := strings.ToLower(text)
		// the product must reflect the domain factor (the "toward what").
		if !strings.Contains(lt, strings.ToLower(c.domain.Theme())) {
			t.Fatalf("MintDriveGoal(%v,%v)=%q missing domain theme %q", c.drive, c.domain, text, c.domain.Theme())
		}
	}
	// distinct (drive, domain) products differ — it is a real cross, not a constant.
	a := MintDriveGoal(DriveCuriosity, DomainSTEM)
	b := MintDriveGoal(DriveSelfImprovement, DomainSocial)
	if a == b {
		t.Fatalf("distinct (drive,domain) products collapsed to the same text: %q", a)
	}
}
