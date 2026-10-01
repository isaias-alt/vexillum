package tribunal

import (
	"strings"
	"testing"
)

func TestFindUndocumentedExports_FlagsUndocumentedFunc(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,6 @@
 package foo
+
+func Bar() int {
+	return 1
+}
`
	got := findUndocumentedExports(diff)
	if len(got) != 1 || got[0] != "foo.go:Bar" {
		t.Errorf("expected [foo.go:Bar], got %v", got)
	}
}

func TestFindUndocumentedExports_DoesNotFlagDocumentedFunc(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,7 @@
 package foo
+
+// Bar returns a constant.
+func Bar() int {
+	return 1
+}
`
	got := findUndocumentedExports(diff)
	if len(got) != 0 {
		t.Errorf("expected no findings for a documented func, got %v", got)
	}
}

func TestFindUndocumentedExports_IgnoresUnexportedAndTestFuncs(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,6 @@
 package foo
+
+func bar() int {
+	return 1
+}
diff --git a/foo_test.go b/foo_test.go
index 1111111..2222222 100644
--- a/foo_test.go
+++ b/foo_test.go
@@ -1,3 +1,7 @@
 package foo
+
+import "testing"
+
+func TestBar(t *testing.T) {}
`
	got := findUndocumentedExports(diff)
	if len(got) != 0 {
		t.Errorf("expected no findings (unexported func, and a _test.go Test func), got %v", got)
	}
}

func TestFindUndocumentedExports_FlagsUndocumentedType(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,4 @@
 package foo
+
+type Widget struct{}
`
	got := findUndocumentedExports(diff)
	if len(got) != 1 || got[0] != "foo.go:Widget" {
		t.Errorf("expected [foo.go:Widget], got %v", got)
	}
}

func TestRunDocs_SkipsNonGoProject(t *testing.T) {
	dir := t.TempDir()
	sr, err := runDocs(dir, "base")
	if err != nil {
		t.Fatalf("runDocs: %v", err)
	}
	if !sr.Passed {
		t.Error("expected a project with no go.mod to skip (pass)")
	}
}

func TestRunDocs_EndToEndFailsOnUndocumentedExport(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "go.mod", "module tribunalfixture\n\ngo 1.21\n")
	commitFile(t, dir, "foo.go", "package foo\n\nfunc Bar() int {\n\treturn 1\n}\n")

	sr, err := runDocs(dir, "base")
	if err != nil {
		t.Fatalf("runDocs: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected an undocumented exported func to fail the docs step")
	}
	if !strings.Contains(sr.Detail, "foo.go:Bar") {
		t.Errorf("expected the detail to name the offending declaration, got %q", sr.Detail)
	}
}

func TestRunDocs_EndToEndPassesWhenDocumented(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "go.mod", "module tribunalfixture\n\ngo 1.21\n")
	commitFile(t, dir, "foo.go", "package foo\n\n// Bar returns a constant.\nfunc Bar() int {\n\treturn 1\n}\n")

	sr, err := runDocs(dir, "base")
	if err != nil {
		t.Fatalf("runDocs: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected a documented export to pass, got: %s", sr.Detail)
	}
}
