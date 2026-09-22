package soldier

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// npxStub puts a fake "npx" on PATH that appends its args and
// CHROME_DEVTOOLS_AXI_SESSION to logFile, so tests can assert whether (and
// how) it was invoked without a real network call or a real browser.
func npxStub(t *testing.T, logFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$@ $CHROME_DEVTOOLS_AXI_SESSION\" >> " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "npx"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing npx stub: %v", err)
	}
	return dir
}

// B3-01: no bridge.pid on disk means stopOrphanBrowser never shells out -
// no npx, no network - the common case for a soldier that never touched a
// browser.
func TestStopOrphanBrowser_NoBridgeMeansNoInvocation(t *testing.T) {
	homeDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "npx.log")
	t.Setenv("PATH", npxStub(t, logFile)+string(os.PathListSeparator)+os.Getenv("PATH"))

	stopOrphanBrowser("task-1", homeDir)

	if _, err := os.Stat(logFile); err == nil {
		t.Error("expected npx to never be invoked when bridge.pid doesn't exist")
	}
}

// B3-02: a bridge.pid present under this task's session directory means
// stopOrphanBrowser shells out to "npx -y chrome-devtools-axi stop" with
// CHROME_DEVTOOLS_AXI_SESSION scoped to exactly this task's session name.
func TestStopOrphanBrowser_StopsWhenBridgeExists(t *testing.T) {
	homeDir := t.TempDir()
	sessionDir := filepath.Join(homeDir, ".chrome-devtools-axi", "sessions", "vx-task-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("creating session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bridge.pid"), []byte("12345 9222\n"), 0o644); err != nil {
		t.Fatalf("writing bridge.pid: %v", err)
	}
	logFile := filepath.Join(t.TempDir(), "npx.log")
	t.Setenv("PATH", npxStub(t, logFile)+string(os.PathListSeparator)+os.Getenv("PATH"))

	stopOrphanBrowser("task-1", homeDir)

	out, err := exec.Command("cat", logFile).Output()
	if err != nil {
		t.Fatalf("expected npx to be invoked, but its log is missing: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "-y chrome-devtools-axi stop") {
		t.Errorf("expected npx invoked with -y chrome-devtools-axi stop, got: %q", got)
	}
	if !strings.Contains(got, "vx-task-1") {
		t.Errorf("expected CHROME_DEVTOOLS_AXI_SESSION=vx-task-1 in the environment, got: %q", got)
	}
}

func TestChromeDevtoolsSessionName(t *testing.T) {
	if got := chromeDevtoolsSessionName("abc123"); got != "vx-abc123" {
		t.Errorf("chromeDevtoolsSessionName(\"abc123\") = %q, want \"vx-abc123\"", got)
	}
}
