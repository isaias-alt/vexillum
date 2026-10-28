package piwake_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/piwake"
)

// The embedded extension is TypeScript that Pi runs, and nobody here can run
// Pi against a model. This runs the very same source, as vx writes it, under
// node (which strips the types itself) against a stand-in for Pi's extension
// API and a fake vx on PATH, to check what the extension decides on its own: an
// idle wake, a follow-up while Pi is busy, a wake Pi cannot take yet being kept
// and retried, the hand-over when a session is replaced, the backoff when the
// await comes straight back, the restart after the 55 minute timeout, and the
// cases where it must do nothing. It needs node 22.18 or newer and is skipped
// without it; nothing at install or run time needs node but Pi itself. The
// real Pi is driven by TestExtensionInRealPi.
func TestExtensionUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		t.Skipf("node --version: %v", err)
	}
	if !nodeStripsTypes(strings.TrimSpace(string(out))) {
		t.Skipf("node %s does not strip types by default", strings.TrimSpace(string(out)))
	}

	dir := t.TempDir()
	ext := filepath.Join(dir, "vexillum-wake.ts")
	driver, err := os.ReadFile(filepath.Join("testdata", "driver.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string][]byte{
		ext:                                piwake.Source(),
		filepath.Join(dir, "driver.mjs"):   driver,
		filepath.Join(dir, "package.json"): []byte(`{"type":"module"}`),
	} {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The driver's own waits end on events and carry their own deadline; this
	// only stops a driver that hung for good, well above a loaded machine.
	ctx, cancel := context.WithTimeout(context.Background(), nodeDriverTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(dir, "driver.mjs"), ext, filepath.Join(dir, "work"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		`VEXILLUM_WAKE_TUNING={"fastMs":400,"quietLimit":3,"quietRetryMs":[600,1200],"failRetryMs":[50,100],"deliverRetryMs":50}`)
	b, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("the node driver did not finish within %s:\n%s", nodeDriverTimeout, b)
	}
	if err != nil || !strings.Contains(string(b), "ok") {
		t.Fatalf("the extension failed under node: %v\n%s", err, b)
	}
}

// nodeDriverTimeout bounds the whole driver run: about 8 seconds of scripted
// waits on an idle machine, plus whatever a machine busy with many other test
// processes adds.
const nodeDriverTimeout = 5 * time.Minute

// nodeStripsTypes reports whether a "vMAJOR.MINOR.PATCH" node strips
// TypeScript types without a flag: 22.18 and newer, 24.3 and newer, and 25+.
func nodeStripsTypes(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	switch {
	case major >= 25:
		return true
	case major == 24:
		return minor >= 3
	case major == 22:
		return minor >= 18
	}
	return false
}
