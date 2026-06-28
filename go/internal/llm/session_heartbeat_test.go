package llm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The session bridge's health must read a WORKER HEARTBEAT, not merely that the spool dir exists (S4):
// no heartbeat ⇒ not fresh; a just-touched heartbeat ⇒ fresh; a stale one ⇒ not fresh (with its age).
func TestSessionWorkerFresh(t *testing.T) {
	spool := t.TempDir()

	// no heartbeat file yet — a worker is not servicing the spool.
	if fresh, age := SessionWorkerFresh(spool); fresh || age != 0 {
		t.Fatalf("absent heartbeat: want (false,0), got (%v,%v)", fresh, age)
	}

	// a fresh heartbeat ⇒ alive.
	hb := filepath.Join(spool, SessionHeartbeatFile)
	if err := os.WriteFile(hb, []byte("alive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := SessionWorkerFresh(spool); !fresh {
		t.Fatal("a just-written heartbeat must read fresh")
	}

	// backdate it past the TTL ⇒ stale, with a non-zero age.
	old := time.Now().Add(-sessionHeartbeatTTL - time.Minute)
	if err := os.Chtimes(hb, old, old); err != nil {
		t.Fatal(err)
	}
	if fresh, age := SessionWorkerFresh(spool); fresh || age <= sessionHeartbeatTTL {
		t.Fatalf("stale heartbeat: want (false, age>TTL), got (%v,%v)", fresh, age)
	}
}
