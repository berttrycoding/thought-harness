package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestMintAgendaDriveGoal pins slice (k) / §7.2: the awake mint produces a real (process-drive x
// agenda-domain) cross goal, it passes the conscience floor, and the agenda domain is drawn weighted
// (STEM-primary, Social balancing-but-kept — a positive share, never zeroed).
func TestMintAgendaDriveGoal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	feat := config.New()
	feat.Conscious.Activity.DriveAgenda = true
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	stem, social := 0, 0
	seenDrives := map[string]bool{}
	for i := 0; i < 400; i++ {
		g := e.mintAgendaDriveGoal()
		if g == nil {
			t.Fatal("a development-agenda goal must pass the conscience floor (got a veto)")
		}
		lt := strings.ToLower(g.Text)
		switch {
		case strings.Contains(lt, "science") || strings.Contains(lt, "maths") || strings.Contains(lt, "engineering"):
			stem++
		case strings.Contains(lt, "social") || strings.Contains(lt, "interpersonal"):
			social++
		default:
			t.Fatalf("minted goal names no agenda domain: %q", g.Text)
		}
		// the goal text opens with the drive verb — record the first word to confirm the drive rotates.
		seenDrives[strings.Fields(lt)[0]] = true
	}

	if stem == 0 || social == 0 {
		t.Fatalf("agenda must keep BOTH domains positive (STEM=%d, Social=%d) — Social is balancing-but-kept", stem, social)
	}
	if stem <= social {
		t.Errorf("STEM is the primary thrust: STEM=%d should exceed Social=%d (weights 0.7/0.3)", stem, social)
	}
	if len(seenDrives) < 2 {
		t.Errorf("the process drive should rotate, saw only %d distinct verbs", len(seenDrives))
	}
}
