// Package tribunal is vexillum's own validation pipeline for a
// finished mission's camp branch, run synchronously by "vx ship"
// right before it pushes and opens a real pull request - replacing the
// former dependency on the third-party github.com/upstream
// binary (a local git remote fronting a pipeline in its own isolated
// worktree). Same functional scope (lint, tests, a code review, a docs
// check), no external binary, no separate worktree, no TUI to track
// afterward: every step runs in the mission's own camp and Run returns
// only once the whole pipeline has settled.
package tribunal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/state"
)

// Name is tribunal's own domain-vocabulary name, used anywhere it needs
// to identify itself in a user-facing message (doctor, ship's own output).
const Name = "tribunal"

// Step is one stage of the tribunal pipeline, run in a fixed order.
type Step string

const (
	StepLint   Step = "lint"
	StepTests  Step = "tests"
	StepReview Step = "review"
	StepDocs   Step = "docs"
	// StepFix is not part of the fixed pipeline: it only appears between
	// rounds when the opt-in fix loop is on.
	StepFix Step = "fix"
)

// steps is the fixed, ordered pipeline one round executes - lint, then
// tests, then an adversarial soldier-driven review of the diff, then a docs
// check. A round stops at the first step that doesn't pass, the same
// "abort on first failure" posture review-tool itself had (PRD v2 /
// docs/review-tool.md, "Alcance de la implementación").
var steps = []Step{StepLint, StepTests, StepReview, StepDocs}

// Default bounds of the opt-in fix loop and of each reviewer/fixer run.
const (
	DefaultMaxFixRounds = 2
	DefaultTimeout      = 20 * time.Minute
)

// Options configures a tribunal run.
type Options struct {
	// Branch is the camp branch under review, named in the reviewer's prompt.
	Branch string
	// TaskPrompt is state.Task.Prompt, the original mission statement the
	// simplification pass judges the change against. Empty skips that pass.
	TaskPrompt string
	// TaskAmendments is state.Task.Amendments: the instructions the general
	// gave after the dispatch. They follow TaskPrompt, labeled, in the intent
	// the review and the fixer see (see intent), so a component any of them
	// asks for is not reported as unrequested. They come from the task's
	// state file, never from a file in the camp.
	TaskAmendments []state.Amendment
	// Fix turns on the review -> fix -> review loop. Off, a blocking review
	// just fails the run.
	Fix bool
	// MaxFixRounds caps the fix rounds when Fix is on; 0 means
	// DefaultMaxFixRounds.
	MaxFixRounds int
	// Timeout is the absolute cap on each single reviewer or fixer
	// invocation; 0 means DefaultTimeout. A timeout fails the step.
	Timeout time.Duration
	// Log, when set, receives one-line progress messages: a run is
	// synchronous and a review can take minutes.
	Log func(msg string)
}

// intent is the mission intent the reviewer and the fixer are given: the
// original prompt followed by the general's later instructions.
func (o Options) intent() string {
	return state.ComposeIntent(o.TaskPrompt, o.TaskAmendments)
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return DefaultTimeout
	}
	return o.Timeout
}

func (o Options) maxFixRounds() int {
	if o.MaxFixRounds <= 0 {
		return DefaultMaxFixRounds
	}
	return o.MaxFixRounds
}

func (o Options) log(msg string) {
	if o.Log != nil {
		o.Log(msg)
	}
}

// StepResult is the outcome of a single Step.
type StepResult struct {
	Step   Step
	Passed bool
	// Detail explains the outcome - command output on failure, the
	// formatted review findings, or a short note on why a step was skipped
	// (e.g. no lint command detected).
	Detail string
	// Report is the reviewer's validated report, set on a review step that
	// got one (passed or blocked). Its info findings ride the PR body.
	Report *Report
	// Notes are non-blocking remarks about how the step ran, e.g. that the
	// simplification pass was skipped for lack of a task prompt.
	Notes []string
}

// Round is one superseded pass through the pipeline, kept so ship can show
// what a fix round was triggered by.
type Round struct {
	Steps []StepResult
}

// Result is the outcome of a full tribunal run.
type Result struct {
	// Steps are the final round's steps - the ones Passed and FailedStep
	// judge.
	Steps []StepResult
	// Earlier holds the rounds the fix loop superseded, oldest first. Empty
	// unless the fix loop ran.
	Earlier []Round
}

// Passed reports whether every step in Steps passed.
func (r Result) Passed() bool {
	for _, s := range r.Steps {
		if !s.Passed {
			return false
		}
	}
	return true
}

// FailedStep returns the step that stopped the run, or nil if Passed(). A
// round stops at its first failure, so that is the only failed step, except
// when the fix step failed after a blocked review: then it is the fix, the
// last failure, not the review that asked for it.
func (r Result) FailedStep() *StepResult {
	for i := len(r.Steps) - 1; i >= 0; i-- {
		if !r.Steps[i].Passed {
			return &r.Steps[i]
		}
	}
	return nil
}

// ReviewReport returns the final round's review report, or nil if the
// review step never produced one.
func (r Result) ReviewReport() *Report {
	for _, s := range r.Steps {
		if s.Step == StepReview && s.Report != nil {
			return s.Report
		}
	}
	return nil
}

// Run executes the tribunal pipeline against campPath - a mission's
// camp worktree - stopping at the first step that fails. base is the
// branch campPath's branch was forked from (state.Task.CampBase), used
// to scope the review and docs steps to only what this mission actually
// changed, the same way camp.HasNewCommits scopes its own commit count.
//
// With opts.Fix, a review that fails only on auto-fix findings hands them
// to a fixer, commits its work, and runs the whole pipeline again (lint and
// tests included, since the fixer's code is new) with a brand-new reviewer
// that is told the fixer's commits are unreviewed code - up to
// opts.MaxFixRounds rounds. Findings that need a human (ask-user) are never
// fixed: the run fails with them. Running out of rounds fails the run.
//
// A returned error means the pipeline itself couldn't run (a step's
// tooling misbehaved in a way that isn't a normal pass/fail finding, e.g.
// the review soldier's process couldn't be started at all) - distinct
// from Result.Passed()==false, which means the pipeline ran fully and a
// step reported a real finding.
func Run(campPath, base string, opts Options) (Result, error) {
	var result Result
	var fixStartSHA string
	for round := 0; ; round++ {
		current, err := runRound(campPath, base, opts, fixStartSHA)
		if err != nil {
			result.Steps = current
			return result, err
		}
		result.Steps = current

		failed := result.FailedStep()
		if failed == nil || failed.Step != StepReview || !opts.Fix {
			return result, nil
		}
		findings, ok := fixable(*failed)
		if !ok {
			return result, nil
		}
		if round >= opts.maxFixRounds() {
			failed.Notes = append(failed.Notes, fmt.Sprintf("fix loop exhausted after %d round(s) without a passing review", round))
			return result, nil
		}

		if fixStartSHA == "" {
			if fixStartSHA, err = gitOutput(campPath, "rev-parse", "HEAD"); err != nil {
				return result, fmt.Errorf("tribunal fix step: %w", err)
			}
		}
		opts.log(fmt.Sprintf("fix round %d/%d: fixing %d finding(s)", round+1, opts.maxFixRounds(), len(findings)))
		fixResult, err := runFix(campPath, findings, opts, round+1)
		if err != nil {
			return result, fmt.Errorf("tribunal %s step: %w", StepFix, err)
		}
		result.Steps = append(result.Steps, fixResult)
		if !fixResult.Passed {
			return result, nil
		}
		result.Earlier = append(result.Earlier, Round{Steps: result.Steps})
		result.Steps = nil
	}
}

// runRound executes the fixed pipeline once, returning the steps that ran
// (the last one is the first to fail, if any).
func runRound(campPath, base string, opts Options, fixStartSHA string) ([]StepResult, error) {
	var out []StepResult
	for _, step := range steps {
		var sr StepResult
		var err error
		switch step {
		case StepLint:
			sr, err = runLint(campPath)
		case StepTests:
			sr, err = runTests(campPath)
		case StepReview:
			opts.log("reviewing the diff (adversarial review, a fresh session)...")
			sr, err = runReview(campPath, base, opts, fixStartSHA)
		case StepDocs:
			sr, err = runDocs(campPath, base)
		}
		if err != nil {
			return out, fmt.Errorf("tribunal %s step: %w", step, err)
		}
		out = append(out, sr)
		if !sr.Passed {
			break
		}
	}
	return out, nil
}

// hasFile reports whether name exists directly inside dir.
func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// gitDiff returns the diff of campPath's current HEAD against base,
// restricted to pathspec when non-empty - the same base..HEAD range
// camp.HasNewCommits counts commits over, so a tribunal step only ever
// looks at what this mission's camp actually introduced.
func gitDiff(campPath, base, pathspec string) (string, error) {
	args := []string{"diff", base + "..HEAD"}
	if pathspec != "" {
		args = append(args, "--", pathspec)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = campPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("diffing %s against %s: %w\n%s", campPath, base, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// runCommand runs name with args inside dir and reports a StepResult for
// step: passed if the command exits zero, failed with its combined
// output as Detail otherwise. A command that isn't found at all (rather
// than one that ran and failed) is treated the same as any other
// failure - the tribunal pipeline has no separate "tooling missing"
// state for a step whose command is simply absent.
func runCommand(step Step, dir, name string, args ...string) (StepResult, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			// The command never produced output at all - e.g. name isn't
			// installed, so it never started - fall back to the exec
			// error itself rather than an empty, unexplained failure.
			detail = err.Error()
		}
		return StepResult{Step: step, Passed: false, Detail: detail}, nil
	}
	return StepResult{Step: step, Passed: true}, nil
}
