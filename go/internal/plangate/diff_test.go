package plangate

import (
	"strings"
	"testing"
)

// TestParseUnifiedAddedLinesOnly: the parser keeps ONLY added (`+`) lines, per file, dropping context,
// removed lines, hunk headers, and the `+++ ` header itself. This is the input the gate audits — a
// symbol counts only if the change PRODUCED it.
func TestParseUnifiedAddedLinesOnly(t *testing.T) {
	diff := `diff --git a/internal/foo/bar.go b/internal/foo/bar.go
index 1111111..2222222 100644
--- a/internal/foo/bar.go
+++ b/internal/foo/bar.go
@@ -1,3 +1,4 @@
 package foo
-func Old() {}
+func New() {}
+const Added = 1
 // trailing context
`
	d := ParseUnified(diff)
	added, ok := d["internal/foo/bar.go"]
	if !ok {
		t.Fatalf("expected the file to be in the diff; got keys %v", keysOf(d))
	}
	if !strings.Contains(added, "func New()") || !strings.Contains(added, "const Added = 1") {
		t.Fatalf("added lines missing: %q", added)
	}
	if strings.Contains(added, "func Old()") {
		t.Fatalf("removed line must NOT appear in added text: %q", added)
	}
	if strings.Contains(added, "package foo") || strings.Contains(added, "trailing context") {
		t.Fatalf("context lines must NOT appear in added text: %q", added)
	}
	if strings.Contains(added, "+++ ") || strings.Contains(added, "bar.go b/") {
		t.Fatalf("the +++ header must NOT be counted as an added line: %q", added)
	}
}

// TestParseUnifiedMultiFile: each file's added text is keyed separately, so a producer symbol is
// scoped to the file the plan said would produce it.
func TestParseUnifiedMultiFile(t *testing.T) {
	diff := `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -0,0 +1 @@
+symbolX
diff --git a/y.go b/y.go
--- a/y.go
+++ b/y.go
@@ -0,0 +1 @@
+symbolY
`
	d := ParseUnified(diff)
	if !strings.Contains(d["x.go"], "symbolX") || strings.Contains(d["x.go"], "symbolY") {
		t.Fatalf("x.go must contain only symbolX, got %q", d["x.go"])
	}
	if !strings.Contains(d["y.go"], "symbolY") || strings.Contains(d["y.go"], "symbolX") {
		t.Fatalf("y.go must contain only symbolY, got %q", d["y.go"])
	}
}

// TestParseUnifiedNewFile: a brand-new file (old side /dev/null) is attributed to its new path, so its
// symbols audit normally.
func TestParseUnifiedNewFile(t *testing.T) {
	diff := `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package newpkg
+func Fresh() {}
`
	d := ParseUnified(diff)
	if !strings.Contains(d["new.go"], "func Fresh()") {
		t.Fatalf("new file symbols must be attributed to the new path, got %v", d)
	}
}

func keysOf(d Diff) []string {
	var out []string
	for k := range d {
		out = append(out, k)
	}
	return out
}
