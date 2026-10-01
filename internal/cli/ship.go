package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/ghpr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
	"github.com/isaias-alt/vexillum/internal/tribunal"
)

const shipUsage = `Ship a finished mission through vexillum's own tribunal pipeline,
opening a real pull request.

Usage:
  vexillum ship <task-id>

Runs, in order, inside the mission's own camp: lint, tests, a soldier-
driven code review of the diff, and a docs check - stopping at the first
step that fails and reporting it, without pushing or opening anything.
Every step runs synchronously; "vexillum ship" doesn't return until the
whole pipeline has settled, there's nothing external to track afterward.

Once every step passes, pushes the mission's camp branch to the real
remote ("origin") and opens the pull request itself with "gh pr create" -
deterministically, no soldier or agent judgment decides whether or when
this happens, since it's the one vexillum action with a real, irreversible
effect outside the machine. Requires "gh" ('vexillum doctor' reports
whether it's installed).

A mission already shipped can be shipped again, to push follow-up commits
onto the same open PR - only "done" and "shipped" are valid starting
states; a re-ship still runs the full tribunal pipeline first. Once
shipped, land the PR with 'vexillum land <task-id>' rather than 'vexillum
land'-ing the camp locally.
`

// Ship runs the "vexillum ship" command.
func Ship(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(shipUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(shipUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	return runShip(projectDir, vexillumHome, args[0], os.Stdout, os.Stderr)
}

func runShip(projectDir, vexillumHome, taskID string, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: loading task %s: %v\n", taskID, err)
		return 1
	}

	if task.Kind != state.KindMission {
		fmt.Fprintf(stderr, "vexillum: task %s is a scout, not a mission - a scout should never have committed anything to ship\n", taskID)
		return 1
	}
	if task.Status != state.StatusDone && task.Status != state.StatusShipped {
		fmt.Fprintf(stderr, "vexillum: task %s is %s, not done or already shipped - only a finished mission can be shipped\n", taskID, task.Status)
		return 1
	}

	if !ghpr.Installed() {
		fmt.Fprintln(stderr, "vexillum: 'gh' is not installed - required to open a mission's pull request (https://cli.github.com)")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving camp: %v\n", err)
		return 1
	}

	result, err := tribunal.Run(c.Path, task.CampBase, tribunal.Options{Branch: c.Branch, TaskPrompt: task.Prompt})
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: running the %s pipeline: %v\n", tribunal.Name, err)
		return 1
	}
	for _, sr := range result.Steps {
		status := "ok"
		if !sr.Passed {
			status = "FAILED"
		}
		fmt.Fprintf(stdout, "[%s] %s\n", status, sr.Step)
	}
	if failed := result.FailedStep(); failed != nil {
		fmt.Fprintf(stderr, "vexillum: %s failed at %s, refusing to push or open a pull request\n", tribunal.Name, failed.Step)
		if failed.Detail != "" {
			fmt.Fprintln(stderr, failed.Detail)
		}
		return 1
	}

	pushCmd := exec.Command("git", "push", "origin", c.Branch)
	pushCmd.Dir = c.Path
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, "vexillum: pushing %s to origin: %v\n%s\n", c.Branch, err, strings.TrimSpace(string(out)))
		return 1
	}

	var prURL string
	if task.Status == state.StatusShipped {
		pr, err := ghpr.View(projectDir, c.Branch)
		if err != nil {
			fmt.Fprintf(stderr, "vexillum: pushed follow-up commits, but couldn't look up the existing pull request: %v\n", err)
			return 1
		}
		prURL = pr.URL
	} else {
		prURL, err = ghpr.Create(projectDir, c.Branch, task.CampBase, shipPRTitle(task), shipPRBody(task))
		if err != nil {
			fmt.Fprintf(stderr, "vexillum: %v\n", err)
			return 1
		}
	}

	task.Status = state.StatusShipped
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		fmt.Fprintf(stderr, "vexillum: opened %s, but failed to record shipped status: %v\n", prURL, err)
		return 1
	}

	fmt.Fprintf(stdout, "%s passed, pushed %s, pull request: %s\n", tribunal.Name, c.Branch, prURL)
	return 0
}

// shipPRTitle derives a pull request title from task's prompt - its
// first line, since a mission's prompt is often multiple paragraphs of
// context the PR title has no room for.
func shipPRTitle(task state.Task) string {
	title := strings.TrimSpace(strings.SplitN(task.Prompt, "\n", 2)[0])
	if title == "" {
		title = "vexillum mission " + task.ID
	}
	return title
}

// shipPRBody derives a pull request body from task's full prompt.
func shipPRBody(task state.Task) string {
	return fmt.Sprintf("%s\n\n---\nvexillum mission %s, verified by %s: lint, tests, review, and docs all passed.", task.Prompt, task.ID, tribunal.Name)
}
