package opencodeosc_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/opencodeosc"
)

// The plugin is installed outside any camp, owner-only, and an install that
// finds the same content leaves the file (and a running soldier's copy) alone.
func TestInstall(t *testing.T) {
	root := t.TempDir()
	dir, err := opencodeosc.Install(root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if dir != opencodeosc.Dir(root) || !strings.HasPrefix(dir, root) {
		t.Errorf("Install = %q, want a directory under %q", dir, root)
	}
	path := filepath.Join(dir, "tui.js")
	if path != opencodeosc.Path(root) {
		t.Errorf("Path = %q, want %q", opencodeosc.Path(root), path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(opencodeosc.Source()) {
		t.Fatalf("installed content differs from the embedded source (err %v)", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", info.Mode().Perm())
	}

	before, _ := os.Stat(path)
	time.Sleep(10 * time.Millisecond)
	if _, err := opencodeosc.Install(root); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged plugin was rewritten")
	}

	if err := os.WriteFile(path, []byte("// edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := opencodeosc.Install(root); err != nil {
		t.Fatalf("Install over an edited copy: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != string(opencodeosc.Source()) {
		t.Error("an edited plugin was not restored")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the plugin directory holds %d entries, want only tui.js (a temp file was left behind)", len(entries))
	}
}

func TestInstall_Unwritable(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "opencode")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := opencodeosc.Install(root); err == nil {
		t.Error("Install under a file succeeded")
	}
}

// The value is the TUI's whole plugins list, and nothing else.
func TestConfigContent(t *testing.T) {
	dir := `/tmp/with "quotes"/and space/osc7501`
	var got map[string][]string
	if err := json.Unmarshal([]byte(opencodeosc.ConfigContent(dir)), &got); err != nil {
		t.Fatalf("ConfigContent is not JSON: %v", err)
	}
	if len(got) != 1 || len(got["plugins"]) != 1 || got["plugins"][0] != dir {
		t.Errorf("ConfigContent = %v, want only plugins: [%s]", got, dir)
	}
	if opencodeosc.EnvVar != "OPENCODE_CLI_CONFIG_CONTENT" {
		t.Errorf("EnvVar = %q: the TUI-only variable, never OPENCODE_CONFIG_CONTENT", opencodeosc.EnvVar)
	}
}

func TestInspect(t *testing.T) {
	root := t.TempDir()
	st, err := opencodeosc.Inspect(root)
	if err != nil || st != opencodeosc.Missing {
		t.Fatalf("nothing installed: %v, %v; want missing", st, err)
	}
	if _, err := opencodeosc.Install(root); err != nil {
		t.Fatal(err)
	}
	if st, err = opencodeosc.Inspect(root); err != nil || st != opencodeosc.Current {
		t.Fatalf("installed: %v, %v; want current", st, err)
	}
	if err := os.WriteFile(opencodeosc.Path(root), []byte("// old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err = opencodeosc.Inspect(root); err != nil || st != opencodeosc.Stale {
		t.Fatalf("different content: %v, %v; want stale", st, err)
	}
	for s, want := range map[opencodeosc.State]string{opencodeosc.Missing: "missing", opencodeosc.Current: "current", opencodeosc.Stale: "stale"} {
		if s.String() != want {
			t.Errorf("State(%d).String() = %q, want %q", int(s), s.String(), want)
		}
	}
}

// The embedded plugin is JavaScript that OpenCode runs, and nobody here can run
// OpenCode against a model. This runs the very same source under node against a
// stand-in for the TUI plugin API, and checks the bytes it writes: every state,
// the permission debounce, scoping to the pane's own session family, silence
// outside an hc terminal, and that only app, state and kind are ever sent. It is
// skipped without node; nothing at install or run time needs node but OpenCode.
func TestPluginUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	dir := t.TempDir()
	plugin := filepath.Join(dir, "tui.js")
	if err := os.WriteFile(plugin, opencodeosc.Source(), 0o644); err != nil {
		t.Fatal(err)
	}
	// ES module syntax in a .js file needs a package that says so.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join("testdata", "driver.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(dir, "driver.mjs")
	if err := os.WriteFile(driverPath, driver, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(node, driverPath, plugin)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(b), "ok") {
		t.Fatalf("the plugin failed under node: %v\n%s", err, b)
	}
}
