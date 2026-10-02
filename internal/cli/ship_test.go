package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
	"github.com/isaias-alt/vexillum/internal/tribunal"
)

// shipTestProject is initDispatchTestProject plus a real "origin" remote
// pointing at a real bare repo, so a real "git push origin <branch>" can
// succeed without a real GitHub remote.
func shipTestProject(t *testing.T) string {
	t.Helper()
	project := initDispatchTestProject(t)

	bareRepo := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.Command("git", "init", "--bare", "-q", bareRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "remote", "add", "origin", bareRepo)
	cmd.Dir = project
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	return project
}

func doneMissionTask(t *testing.T, project, home string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.CampBase = c.Base
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A malformed task id must never reach state.Load/camp.Resolve's
// filepath.Join calls - runShip rejects it up front.
func TestRunShip_RejectsInvalidTaskID(t *testing.T) {
	var out bytes.Buffer
	code := runShip("/does/not/matter", "/does/not/matter", "../../etc/passwd", shipOptions{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for an invalid task id")
	}
	if !strings.Contains(out.String(), "invalid task id") {
		t.Errorf("expected the error to name the invalid task id, got: %s", out.String())
	}
}

// gitOnlyPath returns a PATH containing nothing but a real git - used to
// deterministically guarantee neither "claude" nor "gh" is found on
// PATH, regardless of what's installed on the machine running the test.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found on PATH: %v", err)
	}
	if err := os.Symlink(realGit, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("linking real git: %v", err)
	}
	return dir
}

// shipToolsPath returns a PATH with a real git, a fake "claude" whose
// review verdict is fixed to claudeVerdict regardless of the prompt it's
// given, and a fake "gh" driven by ghScript - so ship's tribunal
// pipeline (internal/tribunal's review step) and its own "gh pr
// create"/"gh pr view" calls can be exercised without a real Claude Code
// API call or a real GitHub remote.
func shipToolsPath(t *testing.T, claudeVerdict, ghScript string) string {
	t.Helper()
	dir := gitOnlyPath(t)

	// The claude stub below needs "cat" to print its fixed output -
	// symlinked in for the same reason git is: a PATH scoped to exactly
	// what the stubs need, nothing implicitly inherited from the machine
	// running the test.
	realCat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatalf("cat not found on PATH: %v", err)
	}
	if err := os.Symlink(realCat, filepath.Join(dir, "cat")); err != nil {
		t.Fatalf("linking real cat: %v", err)
	}

	claudeScript := "#!/bin/sh\ncat <<'TRIBUNAL_EOF'\n" + claudeVerdict + "\nTRIBUNAL_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(claudeScript), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}

	// A script that does not answer "pr view" describes a branch with no
	// pull request yet, the way gh does.
	noPRView := ""
	if !strings.Contains(ghScript, `"pr view"`) {
		noPRView = "if [ \"$1 $2\" = \"pr view\" ]; then echo 'no pull requests found for branch' >&2; exit 1; fi\n"
	}
	ghBody := "#!/bin/sh\n" + noPRView + ghScript + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(ghBody), 0o755); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}

	return dir
}

// passingReview is a complete, clean reviewer report for doneMissionTask's
// single changed file.
const passingReview = `reviewed, no issues.
{"findings": [], "reviewed_paths": ["change.txt"], "risk_level": "low", "risk_rationale": "tiny"}`

const ghCreatesNewPR = `case "$1 $2" in
  "pr create") echo "https://github.com/x/y/pull/1" ;;
esac`

const ghHasOpenPR = `case "$1 $2" in
  "pr view") echo '{"number":1,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/1"}' ;;
esac`

// B4-04: shipping a done mission whose tribunal pipeline passes pushes
// its camp branch to "origin" and opens a real pull request.
func TestRunShip_Success(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, passingReview, ghCreatesNewPR))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "https://github.com/x/y/pull/1") {
		t.Errorf("expected output to include the new pull request's URL, got: %s", out.String())
	}
}

// A successful ship records the task as shipped, so a later "vexillum
// land" knows to merge the real PR instead of fast-forwarding the camp.
func TestRunShip_RecordsShippedStatus(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, passingReview, ghCreatesNewPR))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status != state.StatusShipped {
		t.Errorf("expected status %q after a successful ship, got %q", state.StatusShipped, reloaded.Status)
	}
}

// A mission already shipped can be shipped again - pushing follow-up
// commits onto the same open PR is a normal continuation, not an error,
// and it must not attempt to open a second pull request for the branch.
func TestRunShip_AllowsReshippingAShippedTask(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Status = state.StatusShipped
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr view") echo '{"number":1,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/1"}' ;;
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 reshipping an already-shipped task, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "https://github.com/x/y/pull/1") {
		t.Errorf("expected output to include the existing pull request's URL, got: %s", out.String())
	}
}

// B4-05: ship refuses a task that isn't done yet.
func TestRunShip_RefusesNotDone(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a task that isn't done")
	}
	if !strings.Contains(out.String(), "not done") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}
}

// B4-06: ship refuses a scout - it should never have anything to ship.
func TestRunShip_RefusesScout(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a scout")
	}
	if !strings.Contains(out.String(), "scout") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}
}

// ship refuses up front when "gh" isn't installed - no point running the
// whole tribunal pipeline just to fail at the push step.
func TestRunShip_RefusesWhenGhNotInstalled(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", gitOnlyPath(t))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when gh isn't installed")
	}
	if !strings.Contains(out.String(), "'gh' is not installed") {
		t.Errorf("expected the error to say gh isn't installed, got: %s", out.String())
	}
}

// A failing tribunal step blocks the ship entirely - no push, no PR.
func TestRunShip_RefusesWhenTribunalFails(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, "no verdict line here at all", `case "$1 $2" in
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when a tribunal step fails")
	}
	if !strings.Contains(out.String(), "review") {
		t.Errorf("expected the error to name the failing step, got: %s", out.String())
	}
	if strings.Contains(out.String(), "pushed ") {
		t.Errorf("expected no push attempt after a failed tribunal step, got: %s", out.String())
	}

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status == state.StatusShipped {
		t.Error("expected the task to stay unshipped after a failed tribunal")
	}
}

const reviewWithFindings = `{"findings": [%s], "reviewed_paths": ["change.txt"], "risk_level": "low", "risk_rationale": "tiny"}`

func reviewFinding(severity, action string) string {
	return `{"file": "change.txt", "line": 1, "severity": "` + severity + `", "action": "` + action +
		`", "description": "SOMETHING_WRONG", "failure_scenario": "input X yields Y", "sibling_sites": []}`
}

// assertNoShipSideEffects checks a refused ship neither pushed nor opened a
// PR nor recorded the task as shipped.
func assertNoShipSideEffects(t *testing.T, project, home string, task state.Task, output string) {
	t.Helper()
	if strings.Contains(output, "pushed ") || strings.Contains(output, "PR CREATE SHOULD NOT HAVE BEEN CALLED") {
		t.Errorf("expected no push and no pull request, got: %s", output)
	}
	cmd := exec.Command("git", "ls-remote", "origin")
	cmd.Dir = project
	if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), task.CampBranch) {
		t.Errorf("expected the branch to stay off origin (err=%v): %s", err, out)
	}
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status == state.StatusShipped {
		t.Error("expected the task to stay unshipped")
	}
}

const ghMustNotCreate = `case "$1 $2" in
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`

// Any error or warning finding blocks the ship, and the findings are
// printed - no push, no PR.
func TestRunShip_BlockingFindingsRefuseAndArePrinted(t *testing.T) {
	for _, tc := range []struct{ severity, action string }{{"error", "auto-fix"}, {"warning", "auto-fix"}, {"warning", "ask-user"}} {
		t.Run(tc.severity+"/"+tc.action, func(t *testing.T) {
			project := shipTestProject(t)
			home := t.TempDir()
			task := doneMissionTask(t, project, home)
			t.Setenv("PATH", shipToolsPath(t, fmt.Sprintf(reviewWithFindings, reviewFinding(tc.severity, tc.action)), ghMustNotCreate))

			var out bytes.Buffer
			code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

			if code != 1 {
				t.Fatalf("expected exit 1, got %d: %s", code, out.String())
			}
			for _, want := range []string{"[FAILED] review", "SOMETHING_WRONG", "change.txt:1", "scenario: input X yields Y", "refusing to push"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("expected output to contain %q, got: %s", want, out.String())
				}
			}
			assertNoShipSideEffects(t, project, home, task, out.String())
		})
	}
}

// Info findings do not block; they are printed and ride the PR body, under
// Tribunal notes.
func TestRunShip_InfoFindingsPassAndGoToThePRBody(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	bodyFile := filepath.Join(t.TempDir(), "gh-args")
	t.Setenv("PATH", shipToolsPath(t, fmt.Sprintf(reviewWithFindings, reviewFinding("info", "no-op")), `case "$1 $2" in
  "pr create") printf '%s\n' "$@" > '`+bodyFile+`'; echo "https://github.com/x/y/pull/1" ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 with only info findings, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "SOMETHING_WRONG") {
		t.Errorf("expected the info finding in the output, got: %s", out.String())
	}
	args, err := os.ReadFile(bodyFile)
	if err != nil {
		t.Fatalf("reading the captured gh args: %v", err)
	}
	for _, want := range []string{"## Tribunal notes", "- `change.txt:1` - SOMETHING_WRONG", "## What\n\n- change"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("expected the PR body to contain %q, got: %s", want, args)
		}
	}
}

// A clean review leaves the PR body without a notes section.
func TestRunShip_CleanReviewAddsNoNotesToThePRBody(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	bodyFile := filepath.Join(t.TempDir(), "gh-args")
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr create") printf '%s\n' "$@" > '`+bodyFile+`'; echo "https://github.com/x/y/pull/1" ;;
esac`))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	args, _ := os.ReadFile(bodyFile)
	if strings.Contains(string(args), "Tribunal notes") {
		t.Errorf("expected no review notes section, got: %s", args)
	}
}

// With --fix, ask-user findings are never auto-fixed and a fixer that
// changes nothing cannot loop: both still refuse without pushing.
func TestRunShip_FixLoopStillRefusesWithoutPushOrPR(t *testing.T) {
	for name, finding := range map[string]string{
		"ask-user never fixed":  reviewFinding("warning", "ask-user"),
		"fixer changed nothing": reviewFinding("error", "auto-fix"),
	} {
		t.Run(name, func(t *testing.T) {
			project := shipTestProject(t)
			home := t.TempDir()
			task := doneMissionTask(t, project, home)
			t.Setenv("PATH", shipToolsPath(t, fmt.Sprintf(reviewWithFindings, finding), ghMustNotCreate))

			var out bytes.Buffer
			code := runShip(project, home, task.ID, shipOptions{Tribunal: tribunal.Options{Fix: true}}, &out, &out)

			if code != 1 {
				t.Fatalf("expected exit 1, got %d: %s", code, out.String())
			}
			assertNoShipSideEffects(t, project, home, task, out.String())
		})
	}
}

func TestParseShipArgs(t *testing.T) {
	id, opts, err := parseShipArgs([]string{"--fix", "abc", "--max-rounds", "3", "--timeout=30m"})
	if err != nil {
		t.Fatalf("parseShipArgs: %v", err)
	}
	if id != "abc" || !opts.Tribunal.Fix || opts.Tribunal.MaxFixRounds != 3 || opts.Tribunal.Timeout != 30*time.Minute {
		t.Errorf("unexpected parse: %q %+v", id, opts)
	}

	id, opts, err = parseShipArgs([]string{"abc"})
	if err != nil || id != "abc" || !reflect.DeepEqual(opts, shipOptions{}) {
		t.Errorf("expected plain defaults, got %q %+v %v", id, opts, err)
	}

	for name, args := range map[string][]string{
		"no id":                 {"--fix"},
		"unknown flag":          {"abc", "--nope"},
		"two ids":               {"abc", "def"},
		"max-rounds w/o fix":    {"abc", "--max-rounds", "2"},
		"max-rounds zero":       {"abc", "--fix", "--max-rounds", "0"},
		"max-rounds not number": {"abc", "--fix", "--max-rounds", "x"},
		"max-rounds missing":    {"abc", "--fix", "--max-rounds"},
		"bad timeout":           {"abc", "--timeout", "soon"},
		"negative timeout":      {"abc", "--timeout", "-5m"},
		"fix with value":        {"abc", "--fix=yes"},
		"title missing":         {"abc", "--title"},
		"title blank":           {"abc", "--title", "  "},
		"body missing":          {"abc", "--body"},
		"body blank":            {"abc", "--body="},
		"body-file missing":     {"abc", "--body-file"},
		"body and body-file":    {"abc", "--body", "x", "--body-file", "f.md"},
	} {
		if _, _, err := parseShipArgs(args); err == nil {
			t.Errorf("%s: expected an error for %v", name, args)
		}
	}
}

// vx ship hands the task's amendments to the tribunal: the reviewer's prompt
// carries the dispatch prompt followed by what the general said afterward,
// read from the task state.
func TestRunShip_ReviewerSeesTheTasksAmendments(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.AddAmendment(state.AmendmentSourcePrompt, "LATER_INSTRUCTION add a retry", time.Now())
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	toolsDir := shipToolsPath(t, passingReview, ghCreatesNewPR)
	argsLog := filepath.Join(t.TempDir(), "claude-args.log")
	recorder := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '" + argsLog + "'\ncat <<'TRIBUNAL_EOF'\n" + passingReview + "\nTRIBUNAL_EOF\n"
	if err := os.WriteFile(filepath.Join(toolsDir, "claude"), []byte(recorder), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}
	t.Setenv("PATH", toolsDir)

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	logged, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("reading the recorded reviewer args: %v", err)
	}
	for _, want := range []string{"do a thing", "Instructions the general gave afterward", "via vx prompt] LATER_INSTRUCTION add a retry"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("expected the reviewer's prompt to contain %q:\n%s", want, logged)
		}
	}
}

// leakyPrompt is a dispatch prompt like the one that once ended up on a public
// pull request: internal rules and the general's local server.
const leakyPrompt = `You are a soldier in a vexillum camp (a git worktree of the vexillum repo). Read AGENTS.md first.
Never touch the general's dev server on localhost:3000, work under /Users/macuser/camps/3.
Finish with all work committed and the tree clean.`

// shipCapturingGh runs a successful ship of a done mission whose prompt is
// leakyPrompt and returns the arguments the fake "gh pr create" received (one
// per line), plus the ship's output.
func shipCapturingGh(t *testing.T, opts shipOptions, review string) (args, output string, code int) {
	t.Helper()
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Prompt = leakyPrompt
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	argsFile := filepath.Join(t.TempDir(), "gh-args")
	t.Setenv("PATH", shipToolsPath(t, review, `case "$1 $2" in
  "pr create") printf '%s\n' "$@" > '`+argsFile+`'; echo "https://github.com/x/y/pull/1" ;;
esac`))

	var out bytes.Buffer
	code = runShip(project, home, task.ID, opts, &out, &out)
	captured, _ := os.ReadFile(argsFile)
	return string(captured), out.String(), code
}

// The pull request is public: neither its title nor its body carry the
// dispatch prompt, whatever it says.
func TestRunShip_PRTextNeverContainsThePrompt(t *testing.T) {
	args, output, code := shipCapturingGh(t, shipOptions{}, passingReview)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, output)
	}
	for _, banned := range []string{"You are a soldier", "AGENTS.md", "localhost", "/Users/", "dev server", "tree clean"} {
		if strings.Contains(args, banned) {
			t.Errorf("the pull request text must not contain %q:\n%s", banned, args)
		}
	}
	for _, want := range []string{"--title\nchange\n", "## What\n\n- change", "## Changes\n\n1 file changed, +1 -0. Areas touched: `(repo root)`.", "## Verification\n\nTribunal steps passed: lint, tests, review, docs. 1 review round, 0 fix rounds.", "vexillum mission "} {
		if !strings.Contains(args, want) {
			t.Errorf("expected the pull request text to contain %q:\n%s", want, args)
		}
	}
	if !strings.Contains(args, "verified by tribunal: lint, tests, review, and docs all passed.") {
		t.Errorf("expected the factual footer:\n%s", args)
	}
}

// --title and --body replace the derived title and What section; the rest of
// the body stays, and the supplied text is sanitized like everything else.
func TestRunShip_TitleAndBodyOverrides(t *testing.T) {
	opts := shipOptions{
		Title:   "feat: a title the commander chose",
		Body:    "Hand written summary.\nIt ran against localhost:8080 by accident.\n" + leakyPrompt,
		HasBody: true,
	}
	args, output, code := shipCapturingGh(t, opts, fmt.Sprintf(reviewWithFindings, reviewFinding("info", "no-op")))
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, output)
	}
	for _, want := range []string{"--title\nfeat: a title the commander chose\n", "## What\n\nHand written summary.\n\n## Changes", "## Tribunal notes", "- `change.txt:1` - SOMETHING_WRONG", "verified by tribunal"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected the pull request text to contain %q:\n%s", want, args)
		}
	}
	for _, banned := range []string{"- change\n", "localhost", "You are a soldier", "/Users/"} {
		if strings.Contains(args, banned) {
			t.Errorf("the pull request text must not contain %q:\n%s", banned, args)
		}
	}
	if !strings.Contains(output, "dropped 4 line(s)") {
		t.Errorf("expected ship to say it dropped lines, got: %s", output)
	}
}

// A --title that is not safe to publish is refused before the tribunal runs:
// no review, no push, no pull request.
func TestRunShip_RefusesUnsafeTitle(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, "no verdict line here at all", ghMustNotCreate))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{Title: "fix: works on localhost:3000"}, &out, &out)

	if code != 1 {
		t.Fatalf("expected exit 1, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--title") || strings.Contains(out.String(), "[FAILED] review") {
		t.Errorf("expected a refusal naming --title before the tribunal ran, got: %s", out.String())
	}
	assertNoShipSideEffects(t, project, home, task, out.String())
}

// Re-shipping a mission whose PR exists refreshes the PR's text from the new
// review, never opening a second PR, and honors --title and --body.
func TestRunShip_ReshipRefreshesTheTitleAndBody(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Status = state.StatusShipped
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	argsFile := filepath.Join(t.TempDir(), "gh-edit-args")
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr view") echo '{"number":7,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/7"}' ;;
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
  "pr edit") printf '%s\n' "$@" > '`+argsFile+`' ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, shipOptions{Title: "feat: a refreshed title", Body: "Refreshed summary.", HasBody: true}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	edited, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("expected the pull request to be edited: %v\n%s", err, out.String())
	}
	for _, want := range []string{"pr\nedit\n7\n", "--title\nfeat: a refreshed title\n", "## What\n\nRefreshed summary.\n", "## Verification"} {
		if !strings.Contains(string(edited), want) {
			t.Errorf("expected gh pr edit to receive %q:\n%s", want, edited)
		}
	}
	if strings.Contains(out.String(), "ignoring") {
		t.Errorf("--title and --body are no longer ignored, got: %s", out.String())
	}
}

// The case that motivates reusing a PR: a shipped soldier was prompted, so the
// task is done again but its branch already has an open PR. Ship pushes to the
// same branch, edits that PR and never opens another; the task is shipped again.
func TestRunShip_DoneTaskWhoseBranchHasAnOpenPRReusesIt(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	argsFile := filepath.Join(t.TempDir(), "gh-edit-args")
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr view") echo '{"number":7,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/7"}' ;;
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
  "pr edit") printf '%s\n' "$@" > '`+argsFile+`' ;;
esac`))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "https://github.com/x/y/pull/7") {
		t.Errorf("expected the existing pull request's URL, got: %s", out.String())
	}
	if _, err := os.Stat(argsFile); err != nil {
		t.Errorf("expected the existing pull request to be edited: %v", err)
	}
	projectRoot, _ := vxproject.Root(home, project)
	got, err := state.Load(projectRoot, task.ID)
	if err != nil || got.Status != state.StatusShipped {
		t.Errorf("expected the task shipped again, got %q err=%v", got.Status, err)
	}
	// The commits reached the same branch on origin.
	lsRemote := exec.Command("git", "ls-remote", "origin", task.CampBranch)
	lsRemote.Dir = project
	if refs, err := lsRemote.CombinedOutput(); err != nil || !strings.Contains(string(refs), task.CampBranch) {
		t.Errorf("expected %s pushed to origin (err=%v): %s", task.CampBranch, err, refs)
	}
}

// A pull request that can no longer take commits stops the ship before the
// tribunal runs: nothing is pushed, edited or created.
func TestRunShip_RefusesAMergedOrClosedPullRequest(t *testing.T) {
	for _, prState := range []string{"MERGED", "CLOSED"} {
		t.Run(prState, func(t *testing.T) {
			project := shipTestProject(t)
			home := t.TempDir()
			task := doneMissionTask(t, project, home)
			t.Setenv("PATH", shipToolsPath(t, "no verdict line here at all", `case "$1 $2" in
  "pr view") echo '{"number":7,"state":"`+prState+`","isDraft":false,"mergeable":"UNKNOWN","headRefOid":"abc123","url":"https://github.com/x/y/pull/7"}' ;;
  "pr create"|"pr edit") echo "NOTHING SHOULD HAVE BEEN TOUCHED" >&2; exit 1 ;;
esac`))

			var out bytes.Buffer
			code := runShip(project, home, task.ID, shipOptions{}, &out, &out)

			if code != 1 {
				t.Fatalf("expected exit 1, got %d: %s", code, out.String())
			}
			if !strings.Contains(out.String(), "https://github.com/x/y/pull/7") || strings.Contains(out.String(), "[FAILED]") || strings.Contains(out.String(), "[ok]") {
				t.Errorf("expected a refusal naming the pull request before any tribunal step, got: %s", out.String())
			}
			assertNoShipSideEffects(t, project, home, task, out.String())
		})
	}
}

// A gh failure that is not "no pull request" must never be read as "none":
// that would open a second pull request on top of the first.
func TestRunShip_LookupFailureIsNotReadAsNoPullRequest(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr view") echo 'HTTP 502 from api.github.com' >&2; exit 1 ;;
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 1 {
		t.Fatalf("expected exit 1, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "looking up the pull request") {
		t.Errorf("expected the lookup failure to be reported, got: %s", out.String())
	}
	assertNoShipSideEffects(t, project, home, task, out.String())
}

func TestParseShipArgs_TitleAndBodyFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want shipOptions
	}{
		"title space":     {[]string{"abc", "--title", "feat: x"}, shipOptions{Title: "feat: x"}},
		"title equals":    {[]string{"--title=fix: a=b", "abc"}, shipOptions{Title: "fix: a=b"}},
		"body":            {[]string{"abc", "--body", "text"}, shipOptions{Body: "text", HasBody: true}},
		"body equals":     {[]string{"abc", "--body=text"}, shipOptions{Body: "text", HasBody: true}},
		"body-file":       {[]string{"abc", "--body-file", "d.md"}, shipOptions{BodyFile: "d.md"}},
		"body-file stdin": {[]string{"abc", "--body-file=-"}, shipOptions{BodyFile: "-"}},
	} {
		t.Run(name, func(t *testing.T) {
			id, got, err := parseShipArgs(tc.args)
			if err != nil || id != "abc" {
				t.Fatalf("parseShipArgs(%v) = %q, %v", tc.args, id, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestShipOptionsLoadBodyFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	opts := shipOptions{BodyFile: write("body.md", "from a file\n")}
	if err := opts.loadBodyFile(strings.NewReader("")); err != nil || !opts.HasBody || opts.Body != "from a file\n" {
		t.Errorf("file body: %+v, %v", opts, err)
	}

	opts = shipOptions{BodyFile: "-"}
	if err := opts.loadBodyFile(strings.NewReader("from stdin")); err != nil || !opts.HasBody || opts.Body != "from stdin" {
		t.Errorf("stdin body: %+v, %v", opts, err)
	}

	opts = shipOptions{}
	if err := opts.loadBodyFile(strings.NewReader("ignored")); err != nil || opts.HasBody {
		t.Errorf("no body file should change nothing: %+v, %v", opts, err)
	}

	for name, o := range map[string]shipOptions{
		"missing file": {BodyFile: filepath.Join(dir, "nope.md")},
		"empty file":   {BodyFile: write("empty.md", " \n")},
	} {
		if err := o.loadBodyFile(strings.NewReader("")); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// reviewWithPRText is a clean review that also proposes the pull request text.
func reviewWithPRText(title, description string) string {
	return fmt.Sprintf(`{"findings": [], "reviewed_paths": ["change.txt"], "risk_level": "low", "risk_rationale": "tiny", "pr_title": %q, "pr_description": %q}`, title, description)
}

// The reviewer's title and description are the default: the title as is, the
// description as the What section, with no commit-derived What and no
// override needed.
func TestRunShip_UsesTheReviewersTitleAndDescription(t *testing.T) {
	review := reviewWithPRText("feat: add change.txt for the greeting", "Adds a greeting file.\nIt gives the tests something to read.")
	args, output, code := shipCapturingGh(t, shipOptions{}, review)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, output)
	}
	for _, want := range []string{"--title\nfeat: add change.txt for the greeting\n", "## What\n\nAdds a greeting file.\nIt gives the tests something to read.\n\n## Changes", "## Verification", "verified by tribunal"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected the pull request text to contain %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "- change\n") || strings.Contains(args, "You are a soldier") {
		t.Errorf("expected neither the commit bullets nor the prompt:\n%s", args)
	}
	if strings.Contains(output, "no usable") {
		t.Errorf("expected no fallback note when the reviewer text is valid, got: %s", output)
	}
}

// Missing or invalid reviewer fields fall back, per field, to the text
// derived from the commits.
func TestRunShip_FallsBackWhenTheReviewerTextIsMissingOrInvalid(t *testing.T) {
	for name, tc := range map[string]struct {
		review        string
		wantTitle     string
		wantWhat      string
		titleFallback bool
		whatFallback  bool
	}{
		"both missing":           {passingReview, "--title\nchange\n", "## What\n\n- change", true, true},
		"title only valid":       {reviewWithPRText("fix: good title", ""), "--title\nfix: good title\n", "## What\n\n- change", false, true},
		"description only valid": {reviewWithPRText("", "A fine description."), "--title\nchange\n", "## What\n\nA fine description.", true, false},
		"not conventional":       {reviewWithPRText("Add things", "ok"), "--title\nchange\n", "## What\n\nok", true, false},
		"multi line title":       {reviewWithPRText("feat: a\nb", "ok"), "--title\nchange\n", "## What\n\nok", true, false},
		"heading in description": {reviewWithPRText("feat: ok", "## What\nstuff"), "--title\nfeat: ok\n", "## What\n\n- change", false, true},
		"wrong json types":       {`{"findings": [], "reviewed_paths": ["change.txt"], "risk_level": "low", "risk_rationale": "tiny", "pr_title": 3, "pr_description": {"a": 1}}`, "--title\nchange\n", "## What\n\n- change", true, true},
	} {
		t.Run(name, func(t *testing.T) {
			args, output, code := shipCapturingGh(t, shipOptions{}, tc.review)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d: %s", code, output)
			}
			for _, want := range []string{tc.wantTitle, tc.wantWhat} {
				if !strings.Contains(args, want) {
					t.Errorf("expected the pull request text to contain %q:\n%s", want, args)
				}
			}
			if got := strings.Contains(output, "no usable pr_title"); got != tc.titleFallback {
				t.Errorf("title fallback note = %v, want %v: %s", got, tc.titleFallback, output)
			}
			if got := strings.Contains(output, "no usable pr_description"); got != tc.whatFallback {
				t.Errorf("description fallback note = %v, want %v: %s", got, tc.whatFallback, output)
			}
		})
	}
}

// The sanitizer applies to the reviewer's text: anything unsafe is refused and
// the derived text takes its place, so nothing of it is published.
func TestRunShip_SanitizesTheReviewersText(t *testing.T) {
	for name, tc := range map[string]struct{ title, description string }{
		"home path":      {"fix: read /Users/macuser/x", "Adds x.\nSee /Users/macuser/camps/3."},
		"localhost port": {"fix: serve on localhost:3000", "Adds x.\nThe dev server on localhost:3000 stays up."},
		"secret":         {"fix: use ghp_abcdefghijklmnopqrstuvwxyz0123456789", "Adds x.\nAPI_KEY=abcd1234efgh5678"},
		"prompt copy":    {"feat: Never touch the general's dev server on localhost:3000, work under /Users/macuser/camps/3.", "Finish with all work committed and the tree clean."},
	} {
		t.Run(name, func(t *testing.T) {
			args, output, code := shipCapturingGh(t, shipOptions{}, reviewWithPRText(tc.title, tc.description))
			if code != 0 {
				t.Fatalf("expected exit 0, got %d: %s", code, output)
			}
			for _, banned := range []string{"/Users/", "localhost", "ghp_", "API_KEY", "tree clean", "Adds x."} {
				if strings.Contains(args, banned) {
					t.Errorf("the pull request text must not contain %q:\n%s", banned, args)
				}
			}
			for _, want := range []string{"--title\nchange\n", "## What\n\n- change"} {
				if !strings.Contains(args, want) {
					t.Errorf("expected the derived text %q:\n%s", want, args)
				}
			}
		})
	}
}

// --title and --body win over what the reviewer proposed, each on its own.
func TestRunShip_OverridesBeatTheReviewersText(t *testing.T) {
	review := reviewWithPRText("feat: the reviewer title", "The reviewer description.")

	args, output, code := shipCapturingGh(t, shipOptions{Title: "fix: commander title"}, review)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, output)
	}
	if !strings.Contains(args, "--title\nfix: commander title\n") || strings.Contains(args, "the reviewer title") || !strings.Contains(args, "The reviewer description.") {
		t.Errorf("expected --title to win and the reviewer description to stay:\n%s", args)
	}

	args, output, code = shipCapturingGh(t, shipOptions{Body: "Commander body.", HasBody: true}, review)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, output)
	}
	if !strings.Contains(args, "--title\nfeat: the reviewer title\n") || !strings.Contains(args, "## What\n\nCommander body.\n") || strings.Contains(args, "The reviewer description.") {
		t.Errorf("expected --body to win and the reviewer title to stay:\n%s", args)
	}
}
