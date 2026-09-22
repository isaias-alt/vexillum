package soldier

import (
	"os"
	"os/exec"
	"path/filepath"
)

// chromeDevtoolsSessionEnvVar is the environment variable
// chrome-devtools-axi reads to namespace its bridge process (verified
// against its own README: "CHROME_DEVTOOLS_AXI_SESSION=<name>"). Setting
// it unconditionally on every soldier's pane (see RunInHerdr) is cheap
// and harmless for a soldier that never touches a browser, and gives
// DiscardInHerdr a deterministic name to target for cleanup - PRD v2,
// B.3's requisito derivado on A.2: "el camp limpio no debe dejar
// procesos de browser huérfanos".
const chromeDevtoolsSessionEnvVar = "CHROME_DEVTOOLS_AXI_SESSION"

// chromeDevtoolsSessionName returns the session name vexillum assigns to
// taskID's pane - deterministic from the task id alone, so
// DiscardInHerdr can reconstruct the exact same name without persisting
// it separately on Task.
func chromeDevtoolsSessionName(taskID string) string {
	return "vx-" + taskID
}

// stopOrphanBrowser best-effort tears down taskID's chrome-devtools-axi
// bridge, if one was ever started. Detection is a plain file stat - no
// npx/network call at all - for the common case where a soldier never
// touched a browser, matching the AXI model's "on-demand, no cost until
// used" principle (PRD v2, "Decisiones de integración de AXIs"):
// ~/.chrome-devtools-axi/sessions/<name>/bridge.pid (verified against
// chrome-devtools-axi's own README) only exists if that session's bridge
// was actually started at least once. Only when that evidence exists
// does this shell out to the real CLI (via npx, same as any other
// on-demand AXI invocation) to actually tear it down. Errors are
// swallowed - same reasoning as ReleaseInHerdr/DiscardInHerdr's own
// TabClose: there's nothing left to recover from a failure here, and it
// must never block the caller.
func stopOrphanBrowser(taskID, homeDir string) {
	session := chromeDevtoolsSessionName(taskID)
	pidFile := filepath.Join(homeDir, ".chrome-devtools-axi", "sessions", session, "bridge.pid")
	if _, err := os.Stat(pidFile); err != nil {
		return
	}

	cmd := exec.Command("npx", "-y", "chrome-devtools-axi", "stop")
	cmd.Env = append(os.Environ(), chromeDevtoolsSessionEnvVar+"="+session)
	_ = cmd.Run()
}
