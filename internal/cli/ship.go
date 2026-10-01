package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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
  vexillum ship <task-id> [--fix] [--max-rounds <n>] [--timeout <duration>]

Runs, in order, inside the mission's own camp: lint, tests, an adversarial
code review of the diff, and a docs check - stopping at the first step that
fails and reporting it, without pushing or opening anything. Every step
runs synchronously; "vexillum ship" doesn't return until the whole pipeline
has settled, there's nothing external to track afterward.

The review runs in a brand-new Claude Code session that has none of the
author's context: it reads the diff itself with git, assumes the change is
wrong and tries to break it, and reports structured findings (file, line,
severity, action, a concrete failure scenario, sibling sites) plus the
files it actually read. When the mission has a prompt, every component the
change introduced is also judged against it, and anything not required is
reported as a warning whose remedy is removing it. Any finding of severity
error or warning blocks the ship (including those that need a human
decision); info findings do not block and are added to the pull request
body. A changed file the reviewer did not report reading, an answer that
is not valid findings JSON after retries, or a timeout all fail the step -
never a pass. Findings are printed in this command's output.

By default a blocking review only rejects. With --fix, findings the
reviewer marked auto-fix are handed to a headless fixer in the camp, its
work is committed, and lint, tests and the review run again with a new
reviewer that treats the fixer's commits as unreviewed code. Findings that
need a human decision (ask-user) are never fixed: the ship is rejected
with them. The loop is bounded by --max-rounds (fix rounds, default 2);
exhausting it without a passing review rejects the ship.

  --fix                 enable the review -> fix -> review loop
  --max-rounds <n>      maximum fix rounds with --fix (default 2)
  --timeout <duration>  absolute cap on each reviewer or fixer run, e.g.
                        30m (default 20m); exceeding it fails the step

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

	taskID, opts, err := parseShipArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	return runShip(projectDir, vexillumHome, taskID, opts, os.Stdout, os.Stderr)
}

// parseShipArgs splits ship's arguments into the task id and the tribunal
// options its flags set. Flags may come before or after the task id.
func parseShipArgs(args []string) (string, tribunal.Options, error) {
	var opts tribunal.Options
	var taskID string
	maxRoundsSet := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		valueOf := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--fix":
			if hasValue {
				return "", opts, fmt.Errorf("--fix takes no value")
			}
			opts.Fix = true
		case "--max-rounds":
			v, err := valueOf()
			if err != nil {
				return "", opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return "", opts, fmt.Errorf("--max-rounds needs a whole number of at least 1, got %q", v)
			}
			opts.MaxFixRounds = n
			maxRoundsSet = true
		case "--timeout":
			v, err := valueOf()
			if err != nil {
				return "", opts, err
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return "", opts, fmt.Errorf("--timeout needs a positive duration like 30m, got %q", v)
			}
			opts.Timeout = d
		default:
			if strings.HasPrefix(arg, "-") {
				return "", opts, fmt.Errorf("unknown flag %q for ship", arg)
			}
			if taskID != "" {
				return "", opts, fmt.Errorf("unexpected extra argument %q, ship takes one task id", arg)
			}
			taskID = arg
		}
	}
	if taskID == "" {
		return "", opts, fmt.Errorf("missing task id")
	}
	if maxRoundsSet && !opts.Fix {
		return "", opts, fmt.Errorf("--max-rounds only applies with --fix")
	}
	return taskID, opts, nil
}

// runShip ships taskID. opts carries the tribunal flags; the camp branch and
// the task prompt are filled in here.
func runShip(projectDir, vexillumHome, taskID string, opts tribunal.Options, stdout, stderr io.Writer) int {
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

	opts.Branch = c.Branch
	opts.TaskPrompt = task.Prompt
	opts.Log = func(msg string) { fmt.Fprintf(stderr, "%s: %s\n", tribunal.Name, msg) }
	result, err := tribunal.Run(c.Path, task.CampBase, opts)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: running the %s pipeline: %v\n", tribunal.Name, err)
		return 1
	}
	for i, round := range result.Earlier {
		printSteps(stdout, fmt.Sprintf("round %d: ", i+1), round.Steps)
	}
	prefix := ""
	if len(result.Earlier) > 0 {
		prefix = fmt.Sprintf("round %d: ", len(result.Earlier)+1)
	}
	printSteps(stdout, prefix, result.Steps)
	for _, sr := range result.Steps {
		for _, note := range sr.Notes {
			fmt.Fprintf(stdout, "note (%s): %s\n", sr.Step, note)
		}
	}
	if failed := result.FailedStep(); failed != nil {
		fmt.Fprintf(stderr, "vexillum: %s failed at %s, refusing to push or open a pull request\n", tribunal.Name, failed.Step)
		for _, sr := range result.Steps {
			if !sr.Passed && sr.Detail != "" {
				fmt.Fprintln(stderr, sr.Detail)
			}
		}
		return 1
	}

	var notes []string
	if report := result.ReviewReport(); report != nil {
		if info := report.Info(); len(info) > 0 {
			fmt.Fprintf(stdout, "review findings (info, non-blocking):\n%s\n", tribunal.FormatFindings(info))
			for _, f := range info {
				notes = append(notes, f.Note())
			}
		}
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
		prURL, err = ghpr.Create(projectDir, c.Branch, task.CampBase, shipPRTitle(task), shipPRBody(task, notes))
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

// shipPRBody derives a pull request body from task's full prompt, followed
// by the review's non-blocking (info) findings when there are any.
func shipPRBody(task state.Task, reviewNotes []string) string {
	return fmt.Sprintf("%s%s\n\n---\nvexillum mission %s, verified by %s: lint, tests, review, and docs all passed.", task.Prompt, ghpr.ReviewNotesSection(reviewNotes), task.ID, tribunal.Name)
}

// printSteps writes one "[ok|FAILED] step" line per step, each preceded by
// prefix (the round label when the fix loop ran more than one round).
func printSteps(w io.Writer, prefix string, steps []tribunal.StepResult) {
	for _, sr := range steps {
		status := "ok"
		if !sr.Passed {
			status = "FAILED"
		}
		fmt.Fprintf(w, "%s[%s] %s\n", prefix, status, sr.Step)
	}
}
