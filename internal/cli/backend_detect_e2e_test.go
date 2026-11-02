package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// awaitRun is one "vx sentinel await" the way a Stop hook starts it, from a
// real vx binary, in a throwaway HOME and project.
type awaitRun struct {
	home    string // HOME of the process; the state lives in home/.vexillum
	cmd     *exec.Cmd
	stderr  *bytes.Buffer
	exited  chan error
	started time.Time
}

// startAwait runs "vx sentinel await" with exactly env on top of a PATH that has
// the fake herdr-core (as hc and herdr-core) and the fake herdr, in a fresh git
// project, and stops it, and the sentinel it auto-starts, when the test ends.
func startAwait(t *testing.T, vx string, env map[string]string) *awaitRun {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, proj, bin := filepath.Join(root, "home"), filepath.Join(root, "proj"), filepath.Join(root, "bin")
	for _, d := range []string{home, proj, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for link, fake := range map[string]string{"hc": "hc", "herdr-core": "hc", "herdr": "herdr"} {
		target, err := filepath.Abs(filepath.Join("..", "..", "bench", "fakes", fake))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(bin, link)); err != nil {
			t.Fatal(err)
		}
	}
	initGitRepo(t, proj)

	cmd := exec.Command(vx, "sentinel", "await")
	cmd.Dir = proj
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + bin + ":/usr/bin:/bin",
		"VXFAKE_HC_DIR=" + filepath.Join(root, "hc"),
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	run := &awaitRun{home: home, cmd: cmd, stderr: &bytes.Buffer{}, exited: make(chan error, 1), started: time.Now()}
	cmd.Stderr = run.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { run.exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if info, ok := run.sentinelInfo(); ok {
			_ = syscall.Kill(info.PID, syscall.SIGTERM)
		}
	})
	return run
}

// awaiter returns the record the running await registered, polling until it
// is there or the await has exited.
func (r *awaitRun) awaiter(t *testing.T) (sentinel.AwaiterRecord, bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(filepath.Join(r.home, ".vexillum", "sentinel-awaiters"))
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(r.home, ".vexillum", "sentinel-awaiters", e.Name()))
			var rec sentinel.AwaiterRecord
			if err == nil && json.Unmarshal(raw, &rec) == nil && rec.PID == r.cmd.Process.Pid {
				return rec, true
			}
		}
		select {
		case <-r.exited:
			return sentinel.AwaiterRecord{}, false
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("no awaiter was registered and the await did not exit; stderr: %s", r.stderr)
	return sentinel.AwaiterRecord{}, false
}

// sentinelInfo is what the sentinel the await auto-started recorded in
// sentinel.json.
func (r *awaitRun) sentinelInfo() (sentinel.Info, bool) {
	raw, err := os.ReadFile(filepath.Join(r.home, ".vexillum", "sentinel.json"))
	var info sentinel.Info
	if err != nil || json.Unmarshal(raw, &info) != nil || info.PID <= 1 {
		return sentinel.Info{}, false
	}
	return info, true
}

// waitSentinel waits for the auto-started sentinel to publish its record.
func (r *awaitRun) waitSentinel(t *testing.T) sentinel.Info {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := r.sentinelInfo(); ok {
			return info
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the await did not start a sentinel")
	return sentinel.Info{}
}

// A commander in a Herdr app terminal whose core restarted with the app has
// HC_TERMINAL_ID and no VX_BACKEND. Its Stop hook await must register and start
// an hc sentinel rather than fail on the TUI backend, and the same hook in a
// herdr TUI pane (HERDR_* set, even next to a stray HC_TERMINAL_ID) must stay on
// the TUI.
func TestSentinelAwait_BackendDetectedFromTheTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds vx and runs it against the fake binaries")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	vx := filepath.Join(t.TempDir(), "vx")
	build := exec.Command("go", "build", "-o", vx, "../../cmd/vx")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building vx: %v\n%s", err, out)
	}

	t.Run("app terminal without VX_BACKEND registers an await and an hc sentinel", func(t *testing.T) {
		run := startAwait(t, vx, map[string]string{"HC_TERMINAL_ID": "t1"})
		rec, ok := run.awaiter(t)
		if !ok {
			t.Fatalf("the await exited instead of registering: %v; stderr: %s", <-run.exited, run.stderr)
		}
		if rec.PaneID != "t1" {
			t.Errorf("awaiter pane = %q, want the app terminal t1", rec.PaneID)
		}
		if info := run.waitSentinel(t); info.Backend != "hc" {
			t.Errorf("sentinel.json backend = %q, want hc", info.Backend)
		}
		if strings.Contains(run.stderr.String(), "belongs to the Herdr app") {
			t.Errorf("the await must not refuse the app terminal: %s", run.stderr)
		}
	})

	t.Run("a herdr pane next to HC_TERMINAL_ID stays on the TUI", func(t *testing.T) {
		run := startAwait(t, vx, map[string]string{
			"HC_TERMINAL_ID": "t1", "HERDR_PANE_ID": "w1:p1", "HERDR_WORKSPACE_ID": "w1",
		})
		rec, ok := run.awaiter(t)
		if !ok {
			t.Fatalf("the await exited instead of registering: %v; stderr: %s", <-run.exited, run.stderr)
		}
		if rec.PaneID != "w1:p1" {
			t.Errorf("awaiter pane = %q, want the herdr pane w1:p1", rec.PaneID)
		}
		if info := run.waitSentinel(t); info.Backend != "" {
			t.Errorf("sentinel.json backend = %q, want the TUI (empty)", info.Backend)
		}
	})

	t.Run("HERDR_PANE_ID alone with HC_TERMINAL_ID stays on the TUI and explains itself", func(t *testing.T) {
		run := startAwait(t, vx, map[string]string{"HC_TERMINAL_ID": "t1", "HERDR_PANE_ID": "w1:p1"})
		select {
		case err := <-run.exited:
			if err != nil {
				t.Errorf("the await must exit 0, got %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("the await did not exit")
		}
		if !strings.Contains(run.stderr.String(), "HERDR_PANE_ID is set too") {
			t.Errorf("stderr should say why the TUI stays, got: %s", run.stderr)
		}
		if _, ok := run.sentinelInfo(); ok {
			t.Error("no sentinel may start from a terminal the TUI backend does not manage")
		}
	})

	t.Run("VX_BACKEND=herdr in an app terminal stays on the TUI", func(t *testing.T) {
		run := startAwait(t, vx, map[string]string{"HC_TERMINAL_ID": "t1", "VX_BACKEND": "herdr"})
		select {
		case err := <-run.exited:
			if err != nil {
				t.Errorf("the await must exit 0, got %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("the await did not exit")
		}
		if !strings.Contains(run.stderr.String(), "VX_BACKEND=herdr selects the herdr TUI") {
			t.Errorf("stderr should say the TUI was chosen explicitly, got: %s", run.stderr)
		}
	})
}
