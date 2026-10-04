package cli

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runYoloT(t *testing.T, projectDir, action string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runYolo(projectDir, action, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestYoloIsOffByDefault(t *testing.T) {
	// Both an initialized project and a bare directory report off, exit 0.
	for name, dir := range map[string]string{
		"initialized": initializedProject(t),
		"bare":        t.TempDir(),
	} {
		code, out, errOut := runYoloT(t, dir, "status")
		if code != 0 || out != "off\n" || errOut != "" {
			t.Errorf("%s: status = %d, %q, %q; want 0, \"off\\n\", \"\"", name, code, out, errOut)
		}
	}
}

func TestYoloOnOffStatus(t *testing.T) {
	dir := initializedProject(t)

	code, out, errOut := runYoloT(t, dir, "on")
	if code != 0 || out != "on\n" {
		t.Fatalf("on = %d, %q, %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "Commit it") {
		t.Errorf("on does not remind the general to commit the file: %q", errOut)
	}
	if code, out, _ := runYoloT(t, dir, "status"); code != 0 || out != "on\n" {
		t.Errorf("status after on = %d, %q", code, out)
	}

	if code, out, _ := runYoloT(t, dir, "off"); code != 0 || out != "off\n" {
		t.Fatalf("off = %d, %q", code, out)
	}
	if code, out, _ := runYoloT(t, dir, "status"); code != 0 || out != "off\n" {
		t.Errorf("status after off = %d, %q", code, out)
	}
}

func TestYoloOnNeedsAnInitializedProject(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := runYoloT(t, dir, "on")
	if code != 1 || out != "" || !strings.Contains(errOut, "init") {
		t.Fatalf("on in a bare dir = %d, %q, %q", code, out, errOut)
	}
	if code, out, _ := runYoloT(t, dir, "off"); code != 0 || out != "off\n" {
		t.Errorf("off in a bare dir = %d, %q", code, out)
	}
	if code, out, _ := runYoloT(t, dir, "status"); code != 0 || out != "off\n" {
		t.Errorf("status after a refused on = %d, %q", code, out)
	}
}

func TestYoloRejectsBadInput(t *testing.T) {
	dir := initializedProject(t)
	if code, _, errOut := runYoloT(t, dir, "maybe"); code != 1 || !strings.Contains(errOut, "unknown yolo action") {
		t.Errorf("unknown action = %d, %q", code, errOut)
	}
	if code := Yolo([]string{"on", "off"}); code != 1 {
		t.Errorf("two arguments = %d, want 1", code)
	}
	if code := Yolo([]string{"--help"}); code != 0 {
		t.Errorf("--help = %d, want 0", code)
	}

	// A corrupt file is an error, never a silent off.
	writeFileT(t, filepath.Join(dir, ".vexillum", "yolo.json"), "yes")
	if code, out, errOut := runYoloT(t, dir, "status"); code != 1 || out != "" || !strings.Contains(errOut, "yolo.json") {
		t.Errorf("status on a corrupt file = %d, %q, %q", code, out, errOut)
	}
}

// yolo.json is committed like models.json: the scaffolded .vexillum/.gitignore
// must not hide it.
func TestYoloFileIsNotGitIgnored(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	if code, _, errOut := runYoloT(t, projectDir, "on"); code != 0 {
		t.Fatal(errOut)
	}
	cmd := exec.Command("git", "check-ignore", "-q", ".vexillum/yolo.json")
	cmd.Dir = projectDir
	if err := cmd.Run(); err == nil {
		t.Error(".vexillum/yolo.json is git-ignored")
	}
}

func TestDoctor_Yolo(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)

	run := func() (int, string) {
		var out bytes.Buffer
		code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
		return code, out.String()
	}

	if code, out := run(); code != 0 || !strings.Contains(out, "[ok] yolo mode - off") {
		t.Errorf("default: exit %d\n%s", code, out)
	}
	if code, _, errOut := runYoloT(t, projectDir, "on"); code != 0 {
		t.Fatal(errOut)
	}
	if code, out := run(); code != 0 || !strings.Contains(out, "[ok] yolo mode - on,") {
		t.Errorf("on: exit %d\n%s", code, out)
	}
	writeFileT(t, filepath.Join(projectDir, ".vexillum", "yolo.json"), "nope")
	code, out := run()
	if code != 0 || !strings.Contains(out, "[warn] yolo mode - invalid:") {
		t.Errorf("invalid must only warn: exit %d\n%s", code, out)
	}
}
