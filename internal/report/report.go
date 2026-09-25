// Package report resolves where a scout soldier writes its final
// deliverable: ~/.vexillum/projects/<key>/reports/<agent-name>-<task-id>.md
// - the same namespaced-per-project root internal/project computes for
// tasks/, wakes/ and camps/, so it survives 'vexillum release' the way a
// report living inside the soldier's own camp worktree wouldn't.
//
// The file is named after both the task's HerdrAgentName (internal/state.
// Task) and its task ID, not a fixed "report.md" the way firstmate's
// data/<task-id>/report.md is - this is a flat directory shared by every
// soldier ever dispatched into the project, not one directory per task, so
// the name is what keeps two concurrent soldiers' reports from colliding.
//
// HerdrAgentName alone used to be considered enough for that, on the
// theory that it was already disambiguated against every other *live*
// herdr agent before the soldier ever started (internal/soldier.
// startAgent). That held only while the agent stayed alive: once its camp
// was released, the name was freed for reuse by an unrelated later
// soldier, whose own report could then land on the exact path an earlier,
// already-written report still occupied on disk (that anti-collision check
// only ever looks at live herdr agents, never at reports/ itself). The
// task ID is what actually holds regardless - internal/state.New mints it
// from crypto/rand and it is never reused for another task - so it, not
// the agent name, is what a report path is keyed on to be unique. The
// agent name stays in the filename purely for a human skimming reports/ to
// recognize which prompt it came from.
//
// Missions never get a report - see internal/soldier.RunInHerdr, which
// only ever instructs a scout to write one, and internal/cli.runRelease,
// which only ever gates on one for a scout.
package report

import (
	"fmt"
	"os"
	"path/filepath"
)

const dirName = "reports"

// Dir returns <project root>/reports.
func Dir(projectRoot string) string {
	return filepath.Join(projectRoot, dirName)
}

// Path returns the report file a soldier named agentName, running as task
// taskID, should write to. taskID is what actually guarantees the path is
// unique - see the package doc - agentName only makes the filename
// readable.
func Path(projectRoot, agentName, taskID string) string {
	return filepath.Join(Dir(projectRoot), agentName+"-"+taskID+".md")
}

// Exists reports whether the report file for agentName/taskID is present.
// Checked live at the moment it's asked, not cached - internal/cli.
// runRelease's gate relies on this being the true, current state of the
// filesystem, not a snapshot recorded by an earlier internal/sentinel tick
// that might have run before the soldier's write actually landed.
func Exists(projectRoot, agentName, taskID string) bool {
	_, err := os.Stat(Path(projectRoot, agentName, taskID))
	return err == nil
}

// Remove deletes the report file for agentName/taskID, if any. A missing
// file is not an error - most agent names never had one to begin with
// (every mission, and any scout that hasn't finished yet).
//
// internal/cli.runRedispatch calls this before relaunching a discarded
// attempt: since the fresh run reuses the task's original, unchanged
// prompt and the same task ID, its candidate agent name (internal/soldier.
// herdrAgentName, a slug of that same prompt) will very likely be
// identical to the dead soldier's, and its task ID is exactly the dead
// soldier's - without this, a leftover report from that dead attempt would
// sit at the exact path the new soldier is about to be told to write to,
// and could be mistaken for the new run's own report (e.g. by 'vexillum
// release' gating on mere existence) even if the new soldier never got
// around to writing one itself.
func Remove(projectRoot, agentName, taskID string) error {
	path := Path(projectRoot, agentName, taskID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale report %s: %w", path, err)
	}
	return nil
}
