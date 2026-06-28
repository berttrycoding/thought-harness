// autopermission_config.go — the DETERMINISTIC loaders for the two SECURITY-SANDBOX follow-up
// extension points on AutoPermissionPolicy:
//
//  1. the PER-WORKSPACE EXTENSIBLE ALLOWLIST — a project grants its own build/test tooling into the
//     SAFE tier via a committed config file (so `mvn`/`gradle`/`tsc`/`dotnet` auto-pass in THAT
//     workspace without a code change); merged ONTO the curated seed set, never shrinking it;
//  2. the HIGHER-AUTONOMY PRE-AUTHORIZATION channel — a human grants a specific DANGEROUS class
//     (`go run`, `make`, `npm install`) ahead of time so the harness self-authorizes that class
//     instead of always-escalating (the L4-autonomy hook).
//
// Both are EXPLICIT and default-empty: with no config file and no grant the policy is byte-identical
// to the curated-seed-only behaviour (escalate-everything-dangerous, only-seed-programs-safe). The
// loaders are pure CONTROL (no model, no clock, no network): a JSON file read + a comma-split, both
// validated. A malformed file is a hard error the engine surfaces — never a silent loosening.
package action

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceAutoPermission is the on-disk schema for the per-workspace auto-permission config file.
// A project commits this (e.g. at .thought/auto-permission.json) to grant its own tooling. Both
// fields are optional; an empty file grants nothing (the curated-seed floor).
type WorkspaceAutoPermission struct {
	// AllowedCommands are additional program names to admit into the SAFE tier (the EXTENSIBLE
	// allowlist). Program NAMES only (no argv) — e.g. "mvn", "gradle", "tsc", "dotnet". A name is
	// normalized (lowercased, basename of any path) the same way the classifier normalizes a program.
	// The hardened-destructive / redirection / dangerous-subcommand / jail checks still fire on these.
	AllowedCommands []string `json:"allowed_commands,omitempty"`
	// PreAuthorizedClasses are specific DANGEROUS classes to pre-authorize (the L4 channel). Class keys
	// are the canonical `<prog> <subcommand>` vocabulary the classifier emits — e.g. "go run", "make",
	// "npm install", "git push". An unknown class string is inert (matches nothing). This is an EXPLICIT
	// grant: listing a class here is the human pre-authorizing the harness to self-approve it.
	PreAuthorizedClasses []string `json:"pre_authorized_classes,omitempty"`
}

// errEmptyAllowedCommand / errEmptyPreAuthClass guard against a malformed file (an empty/whitespace
// entry is a config bug, surfaced loudly rather than silently dropped).
var (
	errEmptyAllowedCommand = errors.New("auto-permission config: empty allowed_commands entry")
	errEmptyPreAuthClass   = errors.New("auto-permission config: empty pre_authorized_classes entry")
)

// LoadWorkspaceAutoPermission reads + validates the per-workspace config file at path (resolved
// against workspaceDir if relative). A non-existent file is NOT an error — it returns an empty config
// (the curated-seed floor), so a project that ships no file is unaffected. A present-but-malformed
// file IS an error (a config bug must be loud, never a silent loosening). The returned maps are
// ready to assign onto AutoPermissionPolicy.{ExtraAllowlist,PreAuthClasses}; both nil when the file
// is absent or empty.
func LoadWorkspaceAutoPermission(workspaceDir, path string) (extra map[string]bool, preAuth map[string]bool, err error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil, nil
	}
	resolved := path
	if !filepath.IsAbs(resolved) && workspaceDir != "" {
		resolved = filepath.Join(workspaceDir, resolved)
	}
	data, readErr := os.ReadFile(resolved)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			// No file ⇒ the curated-seed floor (additive: a missing grant grants nothing).
			return nil, nil, nil
		}
		return nil, nil, readErr
	}
	var wc WorkspaceAutoPermission
	if jsonErr := json.Unmarshal(data, &wc); jsonErr != nil {
		return nil, nil, errors.New("auto-permission config: malformed JSON in " + resolved + ": " + jsonErr.Error())
	}
	extra, preAuth, err = wc.compile()
	return extra, preAuth, err
}

// compile validates + normalizes a parsed config into the two grant maps. A program name is
// normalized like the classifier normalizes argv[0] (lowercased, path-basename), so a project may
// write "/usr/bin/mvn" or "MVN" and it still matches the running program. A pre-auth class is
// trimmed + lowercased + internal-whitespace-collapsed so "go  run" and "go run" both match the
// canonical "go run" the classifier emits.
func (wc WorkspaceAutoPermission) compile() (extra map[string]bool, preAuth map[string]bool, err error) {
	for _, raw := range wc.AllowedCommands {
		name := normalizeProgramName(raw)
		if name == "" {
			// An EXPLICIT empty/whitespace entry in the file is a config bug — surface it loudly.
			return nil, nil, errEmptyAllowedCommand
		}
		if extra == nil {
			extra = map[string]bool{}
		}
		extra[name] = true
	}
	for _, raw := range wc.PreAuthorizedClasses {
		class := normalizeClass(raw)
		if class == "" {
			// An EXPLICIT empty/whitespace class entry in the file is a config bug — surface it loudly.
			return nil, nil, errEmptyPreAuthClass
		}
		if preAuth == nil {
			preAuth = map[string]bool{}
		}
		preAuth[class] = true
	}
	return extra, preAuth, nil
}

// ParsePreAuth parses the comma-separated pre-auth grant list (the config knob
// action.auto_permission_pre_auth) into the grant map, normalizing each class. An empty string ⇒ nil
// (no grant — the escalate-everything-dangerous floor). Blank entries (a trailing comma) are skipped
// — that is forgiving on the CLI/flag surface, not a malformed-file path. Deterministic. (It returns
// an error for contract symmetry with LoadWorkspaceAutoPermission; the flag surface never errors.)
func ParsePreAuth(list string) (map[string]bool, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(list, ",") {
		class := normalizeClass(part)
		if class == "" {
			continue // a blank/trailing-comma entry on the flag surface — skip, do not error
		}
		out[class] = true
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// MergePreAuth unions two grant maps (the config-file grants + the flag grants). Either may be nil.
// nil when both are empty (the floor).
func MergePreAuth(a, b map[string]bool) map[string]bool {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// normalizeProgramName mirrors the classifier's argv[0] normalization (lowercase + path-basename) so a
// granted "/usr/bin/mvn" or "MVN" matches the running `mvn`.
func normalizeProgramName(raw string) string {
	p := strings.ToLower(strings.TrimSpace(raw))
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		p = p[idx+1:]
	}
	return p
}

// normalizeClass canonicalizes a pre-auth class string: trim, lowercase, collapse internal runs of
// whitespace to a single space — so "Go  Run" and "go run" both match the canonical "go run".
func normalizeClass(raw string) string {
	return strings.ToLower(strings.Join(strings.Fields(raw), " "))
}
