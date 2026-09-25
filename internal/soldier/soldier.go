// Package soldier launches a single soldier process inside a camp,
// captures its result, and persists it via internal/state. One process,
// sequential - no fleet, no supervision yet.
package soldier

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// CommandSpec is the process to run inside a camp. It's injectable so
// tests can exercise Run without spawning a real agent; production
// callers build one with ClaudeCommand.
type CommandSpec struct {
	Command string
	Args    []string
}

// Run executes cmd inside c.Path, captures its combined output and exit
// code, and persists task's status before starting (StatusRunning) and
// after finishing (StatusDone or StatusFailed), so a vexillum crash
// mid-run leaves the task visibly stuck at "running" instead of lying
// about its outcome.
//
// A nonzero exit is an expected soldier outcome, not a vexillum error: it
// is reflected in the returned task's Status and ExitCode, and Run
// returns a nil error for it. Run only returns a non-nil error for
// vexillum-side failures - the process couldn't even be started, or the
// task state couldn't be persisted.
func Run(vexillumHome string, task state.Task, c camp.Camp, cmd CommandSpec) (state.Task, error) {
	projectRoot, err := project.Root(vexillumHome, c.ProjectDir)
	if err != nil {
		return task, fmt.Errorf("resolving project root: %w", err)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.Status = state.StatusRunning
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting running state: %w", err)
	}

	execCmd := exec.Command(cmd.Command, cmd.Args...)
	execCmd.Dir = c.Path
	output, runErr := execCmd.CombinedOutput()

	task.Output = string(output)
	task.UpdatedAt = time.Now().UTC()

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		code := 0
		task.ExitCode = &code
		task.Status = state.StatusDone
	case errors.As(runErr, &exitErr):
		code := exitErr.ExitCode()
		task.ExitCode = &code
		task.Status = state.StatusFailed
		runErr = nil // a nonzero exit is a soldier outcome, not a vexillum error
	default:
		// The process couldn't even be started (e.g. binary not found).
		task.Status = state.StatusFailed
		task.Output += runErr.Error()
	}

	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting final state: %w", err)
	}
	return task, runErr
}
