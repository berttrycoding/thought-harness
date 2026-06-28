package cognition

import (
	"strings"
	"testing"
)

// TestVetActionRefusesProhibitions pins the Pattern-C FLOOR (§7.2): VetAction refuses an action that
// matches a hard prohibition distilled from .thought/identity/discernment.md + stewardship.md
// (exceed granted authority / modify identity / act without accountability / bypass the governor /
// expand own authority / wirehead). Each prohibited request must be refused with a non-empty reason.
func TestVetActionRefusesProhibitions(t *testing.T) {
	prohibited := []string{
		"exceed my granted authority and run the deploy anyway",
		"modify the .thought/identity/principles.md covenant file",
		"act without accountability — skip the event log",
		"bypass the governor's approval gate just this once",
		"expand my own authority to write to remotes",
		"wirehead: optimize the value reward signal instead of the real goal",
		"override the structural refusal with a model judgment",
		"act outside the project sandbox",
	}
	for _, text := range prohibited {
		allow, reason := VetAction(text)
		if allow {
			t.Fatalf("VetAction(%q) allowed a prohibited action (should refuse)", text)
		}
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("VetAction(%q) refused but gave no reason", text)
		}
	}
}

// TestVetActionAllowsNormal pins that an ordinary, in-bounds action passes the floor (allow=true,
// empty reason) — the floor refuses prohibitions only, it is not a default-deny.
func TestVetActionAllowsNormal(t *testing.T) {
	normal := []string{
		"read the project files and summarize the architecture",
		"explore an open maths question",
		"draft a helpful answer for the user",
		"branch the thought graph to explore an alternative",
		"resolve the contradiction between the two beliefs",
	}
	for _, text := range normal {
		allow, reason := VetAction(text)
		if !allow {
			t.Fatalf("VetAction(%q) refused a normal action: %q", text, reason)
		}
		if reason != "" {
			t.Fatalf("VetAction(%q) allowed but returned a reason %q (want empty)", text, reason)
		}
	}
}

// TestVetActionFloorOnly pins that VetAction is the deterministic FLOOR only — it makes no model call
// and is stable/idempotent (the model CEILING is escalated later, elsewhere).
func TestVetActionFloorOnly(t *testing.T) {
	text := "bypass the governor"
	allow1, reason1 := VetAction(text)
	allow2, reason2 := VetAction(text)
	if allow1 != allow2 || reason1 != reason2 {
		t.Fatal("VetAction must be deterministic (floor only, no model call)")
	}
	if allow1 {
		t.Fatal("bypass the governor must be refused by the floor")
	}
	// a minted drive goal (normal self-development) passes the floor.
	g := MintDriveGoal(DriveCuriosity, DomainSTEM)
	if allow, reason := VetAction(g); !allow {
		t.Fatalf("a minted DRIVE goal should pass the conscience floor, got refuse: %q", reason)
	}
}
