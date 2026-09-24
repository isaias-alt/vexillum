// Package report resolves where a scout soldier writes its final
// deliverable: ~/.vexillum/projects/<key>/reports/<agent-name>.md - the
// same namespaced-per-project root internal/project computes for tasks/,
// wakes/ and camps/, so it survives 'vexillum release' the way a report
// living inside the soldier's own camp worktree wouldn't.
//
// The file is named after the task's HerdrAgentName (internal/state.Task),
// not a fixed "report.md" the way firstmate's data/<task-id>/report.md
// is - this is a flat directory shared by every soldier ever dispatched
// into the project, not one directory per task, so the name is what keeps
// two concurrent soldiers' reports from colliding. HerdrAgentName is
// already the exact string a collision would have been disambiguated
// against before a soldier ever starts (internal/soldier.startAgent), so
// reusing it here comes for free: two soldiers can never end up racing to
// write the same report path any more than they can end up sharing a live
// herdr agent name.
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

// Path returns the report file a soldier named agentName should write to.
func Path(projectRoot, agentName string) string {
	return filepath.Join(Dir(projectRoot), agentName+".md")
}

// Exists reports whether agentName's report file is present. Checked live
// at the moment it's asked, not cached - internal/cli.runRelease's gate
// relies on this being the true, current state of the filesystem, not a
// snapshot recorded by an earlier internal/sentinel tick that might have
// run before the soldier's write actually landed.
func Exists(projectRoot, agentName string) bool {
	_, err := os.Stat(Path(projectRoot, agentName))
	return err == nil
}

// Remove deletes agentName's report file, if any. A missing file is not
// an error - most agent names never had one to begin with (every mission,
// and any scout that hasn't finished yet).
//
// internal/cli.runRedispatch calls this before relaunching a discarded
// attempt: since the fresh run reuses the task's original, unchanged
// prompt, its slug-derived candidate agent name (internal/soldier.
// herdrAgentName) will very likely be identical to the dead soldier's -
// without this, a leftover report from that dead attempt would sit at the
// exact path the new soldier is about to be told to write to, and could
// be mistaken for the new run's own report (e.g. by 'vexillum release'
// gating on mere existence) even if the new soldier never got around to
// writing one itself.
func Remove(projectRoot, agentName string) error {
	path := Path(projectRoot, agentName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale report %s: %w", path, err)
	}
	return nil
}
