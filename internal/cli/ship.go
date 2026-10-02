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
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/ghpr"
	"github.com/isaias-alt/vexillum/internal/prbody"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
	"github.com/isaias-alt/vexillum/internal/tribunal"
)

const shipUsage = `Ship a finished mission through vexillum's own tribunal pipeline,
opening a real pull request.

Usage:
  ` + cmdname.Name + ` ship <task-id> [--fix] [--max-rounds <n>] [--timeout <duration>]
                    [--title <text>] [--body <text> | --body-file <path>]

Runs, in order, inside the mission's own camp: lint, tests, an adversarial
code review of the diff, and a docs check - stopping at the first step that
fails and reporting it, without pushing or opening anything. Every step
runs synchronously; "` + cmdname.Name + ` ship" doesn't return until the whole pipeline
has settled, there's nothing external to track afterward.

The review runs in a brand-new Claude Code session that has none of the
author's context: it reads the diff itself with git, assumes the change is
wrong and tries to break it, and reports structured findings (file, line,
severity, action, a concrete failure scenario, sibling sites) plus the
files it actually read. When the mission has a prompt, every component the
change introduced is also judged against it, followed by any instructions the
general gave afterward through '` + cmdname.Name + ` prompt' and '` + cmdname.Name + ` decide' (recorded on the
task as amendments), and anything none of them required is reported as a
warning whose remedy is removing it. Any finding of severity
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
effect outside the machine. Requires "gh" ('` + cmdname.Name + ` doctor' reports
whether it's installed).

The pull request is public, so its title and body are built from facts and
never from the mission prompt or its amendments. The title is the subject of
the branch's only commit, or of its newest feat, fix or docs commit, or of
its first commit, cut to about 72 characters. The body has a What section
(the commit subjects as a bullet list), the diff stat and the top-level
areas touched, a Verification line (the tribunal steps that passed, the
review rounds and fix rounds), the review's info findings under Tribunal
notes, and a one-line footer naming the mission. Whatever comes from the
camp or the reviewer is sanitized first: lines with an absolute home path,
a localhost port or something that looks like a secret are dropped, as is any
line quoting the mission prompt.

  --title <text>        use this pull request title instead of the derived one
  --body <text>         use this text as the What section instead of the
                        commit subjects
  --body-file <path>    same, read from a file ("-" reads standard input);
                        cannot be combined with --body

A supplied body is sanitized like everything else, the diff stat,
Verification, Tribunal notes and footer stay. A supplied title or body only
matters when the pull request is opened; re-shipping a mission whose PR
already exists leaves the PR's text alone.

A mission already shipped can be shipped again, to push follow-up commits
onto the same open PR - only "done" and "shipped" are valid starting
states; a re-ship still runs the full tribunal pipeline first. Once
shipped, land the PR with '` + cmdname.Name + ` land <task-id>' rather than '` + cmdname.Name + `
land'-ing the camp locally.
`

// Ship runs the "vx ship" command.
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
	if err == nil {
		err = opts.loadBodyFile(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	return runShip(projectDir, vexillumHome, taskID, opts, os.Stdout, os.Stderr)
}

// shipOptions are ship's flags: the tribunal's own, plus the pull request
// text overrides.
type shipOptions struct {
	Tribunal tribunal.Options
	// Title overrides the derived pull request title when not empty.
	Title string
	// Body, when HasBody, replaces the generated What section.
	Body    string
	HasBody bool
	// BodyFile is the path --body-file named, read by loadBodyFile.
	BodyFile string
}

// loadBodyFile fills Body from BodyFile, reading stdin when it is "-".
func (o *shipOptions) loadBodyFile(stdin io.Reader) error {
	if o.BodyFile == "" {
		return nil
	}
	var data []byte
	var err error
	if o.BodyFile == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(o.BodyFile)
	}
	if err != nil {
		return fmt.Errorf("reading --body-file: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf("--body-file %s is empty", o.BodyFile)
	}
	o.Body = string(data)
	o.HasBody = true
	return nil
}

// parseShipArgs splits ship's arguments into the task id and the options its
// flags set. Flags may come before or after the task id.
func parseShipArgs(args []string) (string, shipOptions, error) {
	var ship shipOptions
	opts := &ship.Tribunal
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
				return "", ship, fmt.Errorf("--fix takes no value")
			}
			opts.Fix = true
		case "--max-rounds":
			v, err := valueOf()
			if err != nil {
				return "", ship, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return "", ship, fmt.Errorf("--max-rounds needs a whole number of at least 1, got %q", v)
			}
			opts.MaxFixRounds = n
			maxRoundsSet = true
		case "--timeout":
			v, err := valueOf()
			if err != nil {
				return "", ship, err
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return "", ship, fmt.Errorf("--timeout needs a positive duration like 30m, got %q", v)
			}
			opts.Timeout = d
		case "--title":
			v, err := valueOf()
			if err != nil {
				return "", ship, err
			}
			if strings.TrimSpace(v) == "" {
				return "", ship, fmt.Errorf("--title needs a non-empty title")
			}
			ship.Title = v
		case "--body":
			v, err := valueOf()
			if err != nil {
				return "", ship, err
			}
			if strings.TrimSpace(v) == "" {
				return "", ship, fmt.Errorf("--body needs non-empty text")
			}
			ship.Body, ship.HasBody = v, true
		case "--body-file":
			v, err := valueOf()
			if err != nil {
				return "", ship, err
			}
			if v == "" {
				return "", ship, fmt.Errorf("--body-file needs a path")
			}
			ship.BodyFile = v
		default:
			if strings.HasPrefix(arg, "-") {
				return "", ship, fmt.Errorf("unknown flag %q for ship", arg)
			}
			if taskID != "" {
				return "", ship, fmt.Errorf("unexpected extra argument %q, ship takes one task id", arg)
			}
			taskID = arg
		}
	}
	if taskID == "" {
		return "", ship, fmt.Errorf("missing task id")
	}
	if maxRoundsSet && !opts.Fix {
		return "", ship, fmt.Errorf("--max-rounds only applies with --fix")
	}
	if ship.HasBody && ship.BodyFile != "" {
		return "", ship, fmt.Errorf("--body and --body-file cannot be combined")
	}
	return taskID, ship, nil
}

// runShip ships taskID. ship carries the flags; the camp branch and the task
// prompt are filled into the tribunal options here.
func runShip(projectDir, vexillumHome, taskID string, ship shipOptions, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": loading task %s: %v\n", taskID, err)
		return 1
	}

	if task.Kind != state.KindMission {
		fmt.Fprintf(stderr, cmdname.Name+": task %s is a scout, not a mission - a scout should never have committed anything to ship\n", taskID)
		return 1
	}
	if task.Status == state.StatusBlocked {
		fmt.Fprintln(stderr, cmdname.Name+": "+blockedHint(task))
		return 1
	}
	if task.Status != state.StatusDone && task.Status != state.StatusShipped {
		fmt.Fprintf(stderr, cmdname.Name+": task %s is %s, not done or already shipped - only a finished mission can be shipped\n", taskID, task.Status)
		return 1
	}

	// What may be published never includes the mission's own words.
	sanitizer := prbody.NewSanitizer(append([]string{task.Prompt}, amendmentTexts(task.Amendments)...)...)
	if ship.Title != "" {
		if _, err := prbody.Title(sanitizer, nil, ship.Title, ""); err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
	}

	if !ghpr.Installed() {
		fmt.Fprintln(stderr, cmdname.Name+": 'gh' is not installed - required to open a mission's pull request (https://cli.github.com)")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving camp: %v\n", err)
		return 1
	}

	opts := ship.Tribunal
	opts.Branch = c.Branch
	opts.TaskPrompt = task.Prompt
	opts.TaskAmendments = task.Amendments
	opts.Log = func(msg string) { fmt.Fprintf(stderr, "%s: %s\n", tribunal.Name, msg) }
	result, err := tribunal.Run(c.Path, task.CampBase, opts)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": running the %s pipeline: %v\n", tribunal.Name, err)
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
		fmt.Fprintf(stderr, cmdname.Name+": %s failed at %s, refusing to push or open a pull request\n", tribunal.Name, failed.Step)
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

	// Built before the push so a failure here leaves nothing half-done.
	var title, body string
	if task.Status == state.StatusShipped {
		if ship.Title != "" || ship.HasBody {
			fmt.Fprintln(stderr, cmdname.Name+": the pull request already exists, ignoring --title and --body")
		}
	} else {
		facts, err := prbody.Gather(c.Path, task.CampBase)
		if err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": reading the branch's commits and diff: %v\n", err)
			return 1
		}
		var dropped int
		title, body, dropped, err = shipPRText(sanitizer, task, ship, facts, result, notes)
		if err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
		if dropped > 0 {
			fmt.Fprintf(stderr, cmdname.Name+": dropped %d line(s) from the pull request text: home paths, localhost ports, secrets or the mission prompt must not be published\n", dropped)
		}
	}

	pushCmd := exec.Command("git", "push", "origin", c.Branch)
	pushCmd.Dir = c.Path
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": pushing %s to origin: %v\n%s\n", c.Branch, err, strings.TrimSpace(string(out)))
		return 1
	}

	var prURL string
	if task.Status == state.StatusShipped {
		pr, err := ghpr.View(projectDir, c.Branch)
		if err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": pushed follow-up commits, but couldn't look up the existing pull request: %v\n", err)
			return 1
		}
		prURL = pr.URL
	} else {
		prURL, err = ghpr.Create(projectDir, c.Branch, task.CampBase, title, body)
		if err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
			return 1
		}
	}

	task.Status = state.StatusShipped
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": opened %s, but failed to record shipped status: %v\n", prURL, err)
		return 1
	}

	fmt.Fprintf(stdout, "%s passed, pushed %s, pull request: %s\n", tribunal.Name, c.Branch, prURL)
	return 0
}

// amendmentTexts returns the text of each amendment.
func amendmentTexts(amendments []state.Amendment) []string {
	texts := make([]string, len(amendments))
	for i, a := range amendments {
		texts[i] = a.Text
	}
	return texts
}

// shipPRText builds the pull request title and body for task from the
// branch's facts and the tribunal's outcome, and how many lines the
// sanitizer dropped. Neither contains the mission prompt.
func shipPRText(s prbody.Sanitizer, task state.Task, ship shipOptions, facts prbody.Facts, result tribunal.Result, notes []string) (title, body string, dropped int, err error) {
	title, err = prbody.Title(s, facts.Subjects, ship.Title, "vexillum mission "+task.ID)
	if err != nil {
		return "", "", 0, err
	}
	var passed []string
	for _, sr := range result.Steps {
		if sr.Passed {
			passed = append(passed, string(sr.Step))
		}
	}
	description := ""
	if ship.HasBody {
		description = ship.Body
	}
	body, dropped = prbody.Body(s, prbody.Input{
		TaskID:       task.ID,
		TribunalName: tribunal.Name,
		Facts:        facts,
		Steps:        passed,
		FixRounds:    len(result.Earlier),
		Notes:        notes,
		Description:  description,
	})
	return title, body, dropped, nil
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
