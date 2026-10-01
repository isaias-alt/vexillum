package tribunal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunLint_NoMarkersSkips(t *testing.T) {
	dir := t.TempDir()
	sr, err := runLint(dir)
	if err != nil {
		t.Fatalf("runLint: %v", err)
	}
	if !sr.Passed {
		t.Error("expected a project with no go.mod/package.json to skip (pass), not fail")
	}
}

func TestRunTests_NoMarkersSkips(t *testing.T) {
	dir := t.TempDir()
	sr, err := runTests(dir)
	if err != nil {
		t.Fatalf("runTests: %v", err)
	}
	if !sr.Passed {
		t.Error("expected a project with no go.mod/package.json to skip (pass), not fail")
	}
}

// goModule writes a minimal, self-contained module (no external
// dependencies, so this runs offline) into dir.
func goModule(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "go.mod", "module tribunalfixture\n\ngo 1.21\n")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestRunLint_GoVetCatchesAFormatMismatch(t *testing.T) {
	dir := t.TempDir()
	goModule(t, dir)
	writeFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`)

	sr, err := runLint(dir)
	if err != nil {
		t.Fatalf("runLint: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected go vet to catch the Printf format mismatch")
	}
	if sr.Detail == "" {
		t.Error("expected go vet's output as Detail")
	}
}

func TestRunLint_GoVetPassesCleanCode(t *testing.T) {
	dir := t.TempDir()
	goModule(t, dir)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	sr, err := runLint(dir)
	if err != nil {
		t.Fatalf("runLint: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected clean code to pass go vet, got: %s", sr.Detail)
	}
}

func TestRunTests_FailingTestIsReported(t *testing.T) {
	dir := t.TempDir()
	goModule(t, dir)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "main_test.go", `package main

import "testing"

func TestAlwaysFails(t *testing.T) {
	t.Fatal("boom")
}
`)

	sr, err := runTests(dir)
	if err != nil {
		t.Fatalf("runTests: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected the failing test to fail the tests step")
	}
}

func TestRunTests_PassingTestPasses(t *testing.T) {
	dir := t.TempDir()
	goModule(t, dir)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "main_test.go", `package main

import "testing"

func TestAlwaysPasses(t *testing.T) {}
`)

	sr, err := runTests(dir)
	if err != nil {
		t.Fatalf("runTests: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected the passing test to pass, got: %s", sr.Detail)
	}
}
