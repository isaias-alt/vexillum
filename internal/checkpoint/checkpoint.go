// Package checkpoint is vexillum's own validation pipeline for a
// finished mission's camp branch, run synchronously by "vexillum ship"
// right before it pushes and opens a real pull request - replacing the
// former dependency on the third-party github.com/upstream
// binary (a local git remote fronting a pipeline in its own isolated
// worktree). Same functional scope (lint, tests, a code review, a docs
// check), no external binary, no separate worktree, no TUI to track
// afterward: every step runs in the mission's own camp and Run returns
// only once the whole pipeline has settled.
//
// "checkpoint" is a working name for this mechanism, chosen because the
// general hasn't settled on the final vocabulary yet (see the product
// AGENTS.md's domain vocabulary - commander, soldier, mission, scout,
// camp, sentinel). Name is the single place that name lives, so renaming
// it later is a one-line change plus a `gofmt`-safe package rename, not a
// hunt through call sites.
package checkpoint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Name is checkpoint's own domain-vocabulary name, used anywhere it needs
// to identify itself in a user-facing message (doctor, ship's own output).
// See the package doc comment for why this is a working name.
const Name = "checkpoint"

// Step is one stage of the checkpoint pipeline, run in a fixed order.
type Step string

const (
	StepLint   Step = "lint"
	StepTests  Step = "tests"
	StepReview Step = "review"
	StepDocs   Step = "docs"
)

// steps is the fixed, ordered pipeline Run executes - lint, then tests,
// then a soldier-driven review of the diff, then a docs check. Run stops
// at the first step that doesn't pass, the same "abort on first failure"
// posture review-tool itself had (PRD v2 / docs/review-tool.md, "Alcance
// de la implementación").
var steps = []Step{StepLint, StepTests, StepReview, StepDocs}

// StepResult is the outcome of a single Step.
type StepResult struct {
	Step   Step
	Passed bool
	// Detail explains the outcome - command output on failure, or a short
	// note on why a step was skipped (e.g. no lint command detected).
	Detail string
}

// Result is the outcome of a full checkpoint run.
type Result struct {
	Steps []StepResult
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

// FailedStep returns the first failed step, or nil if Passed().
func (r Result) FailedStep() *StepResult {
	for i := range r.Steps {
		if !r.Steps[i].Passed {
			return &r.Steps[i]
		}
	}
	return nil
}

// Run executes the checkpoint pipeline against campPath - a mission's
// camp worktree - stopping at the first step that fails. base is the
// branch campPath's branch was forked from (state.Task.CampBase), used
// to scope the review and docs steps to only what this mission actually
// changed, the same way camp.HasNewCommits scopes its own commit count.
//
// A returned error means the pipeline itself couldn't run (a step's
// tooling misbehaved in a way that isn't a normal pass/fail finding, e.g.
// the review soldier's process couldn't be started at all) - distinct
// from Result.Passed()==false, which means the pipeline ran fully and a
// step reported a real finding.
func Run(campPath, base string) (Result, error) {
	var result Result
	for _, step := range steps {
		var sr StepResult
		var err error
		switch step {
		case StepLint:
			sr, err = runLint(campPath)
		case StepTests:
			sr, err = runTests(campPath)
		case StepReview:
			sr, err = runReview(campPath, base)
		case StepDocs:
			sr, err = runDocs(campPath, base)
		}
		if err != nil {
			return result, fmt.Errorf("checkpoint %s step: %w", step, err)
		}
		result.Steps = append(result.Steps, sr)
		if !sr.Passed {
			break
		}
	}
	return result, nil
}

// hasFile reports whether name exists directly inside dir.
func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// gitDiff returns the diff of campPath's current HEAD against base,
// restricted to pathspec when non-empty - the same base..HEAD range
// camp.HasNewCommits counts commits over, so a checkpoint step only ever
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
// failure - the checkpoint pipeline has no separate "tooling missing"
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
