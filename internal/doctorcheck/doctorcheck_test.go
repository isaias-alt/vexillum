package doctorcheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func TestBinary_Found(t *testing.T) {
	// "go" is guaranteed to be on PATH in this test environment.
	result := Binary("Go toolchain", "go", true)
	if !result.OK {
		t.Errorf("expected OK=true for a binary on PATH, got %+v", result)
	}
}

func TestBinary_MissingRequired(t *testing.T) {
	result := Binary("Nonexistent", "vexillum-does-not-exist-binary", true)
	if result.OK {
		t.Error("expected OK=false for a missing binary")
	}
	if !result.Required {
		t.Error("expected Required=true to be preserved")
	}
	if result.Detail == "" {
		t.Error("expected a detail message explaining the miss")
	}
}

func TestBinary_MissingOptional(t *testing.T) {
	result := Binary("Nonexistent", "vexillum-does-not-exist-binary", false)
	if result.OK {
		t.Error("expected OK=false for a missing binary")
	}
	if result.Required {
		t.Error("expected Required=false to be preserved")
	}
}

func TestVexillumHome(t *testing.T) {
	dir := t.TempDir()
	result := VexillumHome(dir)
	if !result.OK {
		t.Errorf("expected an existing writable directory to be OK, got %+v", result)
	}

	missing := VexillumHome(filepath.Join(dir, "does-not-exist"))
	if missing.OK {
		t.Error("expected a missing path to not be OK")
	}
}

func TestProjectInitialized_DelegatesToScaffold(t *testing.T) {
	dir := t.TempDir()

	before := ProjectInitialized(dir)
	if before.OK {
		t.Fatal("expected OK=false before the project is scaffolded")
	}

	if err := scaffold.WriteConfig(filepath.Join(dir, ".vexillum")); err != nil {
		t.Fatalf("scaffold.WriteConfig: %v", err)
	}

	after := ProjectInitialized(dir)
	if !after.OK {
		t.Error("expected OK=true once .vexillum/config.json exists")
	}
}

func TestGitRepo_DelegatesToScaffold(t *testing.T) {
	result := GitRepo(t.TempDir())
	if result.OK {
		t.Error("expected OK=false for a directory that isn't a git repository")
	}
}

func TestAXIInstalled(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	if AXIInstalled(projectDir, homeDir, "quota-tool") {
		t.Fatal("expected AXIInstalled=false before writing a SKILL.md")
	}

	skillDir := filepath.Join(homeDir, ".claude", "skills", "quota-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# quota-tool"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}

	if !AXIInstalled(projectDir, homeDir, "quota-tool") {
		t.Error("expected AXIInstalled=true once the global SKILL.md exists")
	}
}

func TestAXIStatusLine(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	a := AXI{Name: "muster", Repo: "isaias-alt/vexillum", Global: false}
	line := AXIStatusLine(a, projectDir, homeDir)
	want := "[not installed] muster - install with: npx skills add isaias-alt/vexillum --skill muster"
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}

	global := AXI{Name: "quota-tool", Repo: "upstream", Global: true}
	globalLine := AXIStatusLine(global, projectDir, homeDir)
	wantGlobal := "[not installed] quota-tool - install with: npx skills add upstream --skill quota-tool -g"
	if globalLine != wantGlobal {
		t.Errorf("got %q, want %q", globalLine, wantGlobal)
	}
}
