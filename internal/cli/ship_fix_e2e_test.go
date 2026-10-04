package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
	"github.com/isaias-alt/vexillum/internal/tribunal"
)

// fixLoopEnv drives the real runShip code path through the review -> fix ->
// review loop without a network: real temporary git repos (the project, the
// mission's camp and a bare "origin"), a scripted fake "claude" that plays
// both the reviewer and the fixer, a fake "npm" that runs the camp's test.sh,
// and a fake "gh" that records "pr create". Nothing leaves the machine.
type fixLoopEnv struct {
	t       *testing.T
	project string
	home    string
	task    state.Task
	// data holds the scripted answers and what the stub recorded.
	data string
	// ghArgs is where the fake "gh pr create" writes its arguments.
	ghArgs string
}

// fixLoopBasePaths are the files the mission changes: the reviewer's
// reviewed_paths must list every one of them.
var fixLoopBasePaths = []string{"change.txt", "package.json", "test.sh"}

// claudeRoleScript is the fake "claude". It tells the fixer from the reviewer
// by the prompt, counts invocations per role, records each prompt and the
// camp's HEAD at call time, optionally runs a hook in the camp (the fixer's
// edits) and prints the scripted answer.
const claudeRoleScript = `#!/bin/sh
d='%s'
case "$2" in
  *"You are repairing a change after an adversarial review"*) role=fix ;;
  *) role=review ;;
esac
n=$(cat "$d/n.$role" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$d/n.$role"
printf '%%s' "$2" > "$d/prompt.$role.$n"
echo "$role $(git rev-parse HEAD)" >> "$d/calls"
[ -f "$d/$role.$n.hook" ] && . "$d/$role.$n.hook"
if [ -f "$d/$role.$n.out" ]; then cat "$d/$role.$n.out"; elif [ -f "$d/$role.last.out" ]; then cat "$d/$role.last.out"; fi
exit 0
`

// fakeNpm runs the camp's test.sh for "npm run test" and passes lint.
const fakeNpm = `#!/bin/sh
case "$2" in
  test) exec /bin/sh ./test.sh ;;
esac
exit 0
`

func newFixLoopEnv(t *testing.T) *fixLoopEnv {
	t.Helper()
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)

	// The camp gets a package.json and a test.sh so the tribunal's own
	// lint and tests steps have something real to run, folded into the
	// mission's single commit.
	c, err := camp.Resolve(project, home, task.CampSlot)
	if err != nil {
		t.Fatalf("camp.Resolve: %v", err)
	}
	for name, content := range map[string]string{
		"package.json": "{\"name\":\"x\"}\n",
		"test.sh":      "[ ! -f broken.flag ]\n",
	} {
		if err := os.WriteFile(filepath.Join(c.Path, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "--amend", "--no-edit"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = c.Path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	data := t.TempDir()
	ghArgs := filepath.Join(t.TempDir(), "gh-args")
	tools := shipToolsPath(t, "", `case "$1 $2" in
  "pr create") printf '%s\n' "$@" > '`+ghArgs+`'; echo "https://github.com/x/y/pull/1" ;;
esac`)
	if err := os.WriteFile(filepath.Join(tools, "claude"), []byte(fmt.Sprintf(claudeRoleScript, data)), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}
	realSleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("sleep not found on PATH: %v", err)
	}
	if err := os.Symlink(realSleep, filepath.Join(tools, "sleep")); err != nil {
		t.Fatalf("linking real sleep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tools, "npm"), []byte(fakeNpm), 0o755); err != nil {
		t.Fatalf("writing npm stub: %v", err)
	}
	// The fixer's commit and the stub's git calls need an identity.
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("PATH", tools)

	return &fixLoopEnv{t: t, project: project, home: home, task: task, data: data, ghArgs: ghArgs}
}

func (e *fixLoopEnv) write(name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.data, name), []byte(content), 0o644); err != nil {
		e.t.Fatalf("writing %s: %v", name, err)
	}
}

// reviewOut scripts what the nth reviewer prints (n=0: every review without
// a more specific answer). extra are changed files beyond the mission's own,
// e.g. the ones a fixer added.
func (e *fixLoopEnv) reviewOut(n int, findings []string, extra ...string) {
	e.t.Helper()
	e.write(role("review", n)+".out", fixLoopReport(findings, "", extra...))
}

// reviewOutWithTitle is reviewOut with a pr_title in the report.
func (e *fixLoopEnv) reviewOutWithTitle(n int, title string, findings []string, extra ...string) {
	e.t.Helper()
	e.write(role("review", n)+".out", fixLoopReport(findings, title, extra...))
}

// fixHook scripts the shell snippet the nth fixer runs inside the camp.
func (e *fixLoopEnv) fixHook(n int, snippet string) {
	e.t.Helper()
	e.write("fix."+strconv.Itoa(n)+".hook", snippet+"\n")
}

func role(name string, n int) string {
	if n == 0 {
		return name + ".last"
	}
	return name + "." + strconv.Itoa(n)
}

func fixLoopReport(findings []string, title string, extra ...string) string {
	paths := append(append([]string{}, fixLoopBasePaths...), extra...)
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = strconv.Quote(p)
	}
	pr := ""
	if title != "" {
		pr = `, "pr_title": ` + strconv.Quote(title) + `, "pr_description": "Adds change.txt with a test harness."`
	}
	return "Reviewed the change.\n" + `{"findings": [` + strings.Join(findings, ",") + `], "reviewed_paths": [` +
		strings.Join(quoted, ",") + `], "risk_level": "low", "risk_rationale": "small change"` + pr + `}`
}

// ship runs vx ship on the mission and returns the exit code and both streams.
func (e *fixLoopEnv) ship(opts tribunal.Options) (code int, stdout, stderr string) {
	e.t.Helper()
	var out, errOut bytes.Buffer
	code = runShip(e.project, e.home, e.task.ID, shipOptions{Tribunal: opts}, &out, &errOut)
	return code, out.String(), errOut.String()
}

// calls returns the stub's invocation log, one "role sha" entry per call.
func (e *fixLoopEnv) calls() []string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.data, "calls"))
	if err != nil {
		return nil
	}
	return strings.Fields(strings.Join(strings.Split(strings.TrimSpace(string(b)), "\n"), " "))
}

// callRoles returns just the roles, in order: e.g. review fix review.
func (e *fixLoopEnv) callRoles() []string {
	var roles []string
	for i, f := range e.calls() {
		if i%2 == 0 {
			roles = append(roles, f)
		}
	}
	return roles
}

func (e *fixLoopEnv) prompt(role string, n int) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.data, "prompt."+role+"."+strconv.Itoa(n)))
	if err != nil {
		e.t.Fatalf("no %s prompt %d recorded: %v", role, n, err)
	}
	return string(b)
}

func (e *fixLoopEnv) campGit(args ...string) string {
	e.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", e.task.CampPath}, args...)...).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v in the camp: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *fixLoopEnv) savedTask() state.Task {
	e.t.Helper()
	root, err := vxproject.Root(e.home, e.project)
	if err != nil {
		e.t.Fatalf("project.Root: %v", err)
	}
	task, err := state.Load(root, e.task.ID)
	if err != nil {
		e.t.Fatalf("state.Load: %v", err)
	}
	return task
}

func (e *fixLoopEnv) prCreated() bool {
	_, err := os.Stat(e.ghArgs)
	return err == nil
}

// remoteTip returns what origin holds for the mission branch, "" when it has
// none.
func (e *fixLoopEnv) remoteTip() string {
	e.t.Helper()
	out, err := exec.Command("git", "-C", e.project, "ls-remote", "origin", "refs/heads/"+e.task.CampBranch).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git ls-remote: %v\n%s", err, out)
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// assertRefused checks a ship that was rejected left no trace outside the
// camp: nothing pushed, no pull request, the task still done.
func (e *fixLoopEnv) assertRefused(code int) {
	e.t.Helper()
	if code != 1 {
		e.t.Errorf("expected exit 1, got %d", code)
	}
	if tip := e.remoteTip(); tip != "" {
		e.t.Errorf("expected nothing pushed, origin holds %s", tip)
	}
	if e.prCreated() {
		e.t.Error("expected no pull request to be opened")
	}
	if got := e.savedTask().Status; got != state.StatusDone {
		e.t.Errorf("expected the task to stay %q, got %q", state.StatusDone, got)
	}
}

func equalStrings(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

// Findings fixed in one round, then the re-review approves: ship pushes the
// fixer's commit and opens the pull request.
func TestShipFixLoop_FixedInOneRoundThenApproved(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOutWithTitle(1, "feat: add change.txt", []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, "echo fixed > fixed.txt")
	e.reviewOutWithTitle(2, "feat: add change.txt", nil, "fixed.txt")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if want := []string{"review", "fix", "review"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	if !strings.Contains(stdout, "round 1: [ok] lint") || !strings.Contains(stdout, "round 2: [ok] docs") {
		t.Errorf("expected both rounds' steps in the output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "round 1 review findings (handed to the fixer):") || !strings.Contains(stdout, "SOMETHING_WRONG") {
		t.Errorf("expected the findings the fixer was given to be printed, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "fix round 1/2: fixing 1 finding(s)") {
		t.Errorf("expected the fix round to be announced, got:\n%s", stderr)
	}
	head := e.campGit("rev-parse", "HEAD")
	if tip := e.remoteTip(); tip != head {
		t.Errorf("expected origin to hold the camp's head %s (fixer commit included), got %q", head, tip)
	}
	if subject := e.campGit("log", "-1", "--format=%s"); !strings.Contains(subject, "round 1") {
		t.Errorf("expected the fixer's commit on top, got %q", subject)
	}
	// The re-review is a fresh reviewer that is told the fixer's commits are
	// unreviewed code.
	if p := e.prompt("review", 2); !strings.Contains(p, "Fix-round provenance") {
		t.Error("expected the re-review prompt to carry the fix-round provenance clause")
	}
	if p := e.prompt("fix", 1); !strings.Contains(p, "SOMETHING_WRONG") {
		t.Errorf("expected the fixer to be handed the finding, got:\n%s", p)
	}
	task := e.savedTask()
	if task.Status != state.StatusShipped || task.LastPushedSHA != head {
		t.Errorf("expected shipped at %s, got %q at %q", head, task.Status, task.LastPushedSHA)
	}
	args, err := os.ReadFile(e.ghArgs)
	if err != nil {
		t.Fatalf("expected gh pr create to run: %v", err)
	}
	if !strings.Contains(string(args), "feat: add change.txt") {
		t.Errorf("expected the reviewer's title on the PR, got:\n%s", args)
	}
	if !strings.Contains(string(args), "1 fix round") {
		t.Errorf("expected the PR body to record the fix round, got:\n%s", args)
	}
}

// A fix that needs two rounds still converges within the default limit, and
// the second round's finding (new code the first fixer introduced) is fixed
// too.
func TestShipFixLoop_ConvergesInTwoRounds(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOutWithTitle(1, "feat: add change.txt", []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, "echo one > fix1.txt")
	e.reviewOutWithTitle(2, "feat: add change.txt", []string{reviewFinding("warning", "auto-fix")}, "fix1.txt")
	e.fixHook(2, "echo two > fix2.txt")
	e.reviewOutWithTitle(3, "feat: add change.txt", nil, "fix1.txt", "fix2.txt")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if want := []string{"review", "fix", "review", "fix", "review"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	if !strings.Contains(stdout, "round 3: [ok] docs") {
		t.Errorf("expected three rounds in the output, got:\n%s", stdout)
	}
	if tip := e.remoteTip(); tip != e.campGit("rev-parse", "HEAD") {
		t.Errorf("expected origin at the camp's head, got %q", tip)
	}
}

// Findings that persist block the ship once the round limit is spent: the
// loop ends, nothing is pushed, and the message says why and shows what is
// still wrong.
func TestShipFixLoop_PersistingFindingsBlockAtTheRoundLimit(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(0, []string{reviewFinding("error", "auto-fix")}, "fix1.txt", "fix2.txt")
	e.fixHook(1, "echo 1 > fix1.txt")
	e.fixHook(2, "echo 2 > fix2.txt")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true, MaxFixRounds: 2})

	e.assertRefused(code)
	// review, fix, review, fix, review: two fix rounds, then it stops.
	if want := []string{"review", "fix", "review", "fix", "review"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	for _, want := range []string{"fix loop exhausted after 2 round(s)", "SOMETHING_WRONG"} {
		if !strings.Contains(stdout+stderr, want) {
			t.Errorf("expected the output to say %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
	}
	if !strings.Contains(stderr, "failed at review") {
		t.Errorf("expected the refusal to name the review, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "2 fix round(s) stay on "+e.task.CampBranch) {
		t.Errorf("expected the refusal to say the fixer's commits stay on the branch, got:\n%s", stderr)
	}
}

// Without --max-rounds the loop is bounded by the default, never unbounded.
func TestShipFixLoop_DefaultRoundLimitIsBounded(t *testing.T) {
	e := newFixLoopEnv(t)
	paths := []string{"f1.txt", "f2.txt", "f3.txt", "f4.txt"}
	e.reviewOut(0, []string{reviewFinding("error", "auto-fix")}, paths...)
	for i := 1; i <= 4; i++ {
		e.fixHook(i, "echo x > f"+strconv.Itoa(i)+".txt")
	}

	code, _, _ := e.ship(tribunal.Options{Fix: true})

	e.assertRefused(code)
	if n := len(e.callRoles()); n != 2*tribunal.DefaultMaxFixRounds+1 {
		t.Errorf("expected %d claude calls for the default limit, got %d (%v)", 2*tribunal.DefaultMaxFixRounds+1, n, e.callRoles())
	}
}

// A fix that breaks the tests fails the ship at the tests step: no re-review
// is spent on broken code, nothing is pushed, and the message says the
// breakage came from a fix round.
func TestShipFixLoop_FixThatBreaksTestsBlocks(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(1, []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, ": > broken.flag")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

	e.assertRefused(code)
	if want := []string{"review", "fix"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v (no re-review of broken code), got %v", want, e.callRoles())
	}
	if !strings.Contains(stdout, "round 2: [ok] lint") || !strings.Contains(stdout, "round 2: [FAILED] tests") {
		t.Errorf("expected round 2 to fail at tests, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "failed at tests") {
		t.Errorf("expected the refusal to name the tests step, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "followed fix round 1") || !strings.Contains(stderr, "stay on "+e.task.CampBranch) {
		t.Errorf("expected the refusal to say the failure followed fix round 1 and that its commit stays, got:\n%s", stderr)
	}
}

// An ask-user finding needs a human: the loop never starts, even when the
// other blocker is auto-fix, and the finding is printed for the general.
func TestShipFixLoop_AskUserStopsTheLoop(t *testing.T) {
	for name, findings := range map[string][]string{
		"alone":               {reviewFinding("warning", "ask-user")},
		"next to an auto-fix": {reviewFinding("error", "auto-fix"), reviewFinding("warning", "ask-user")},
	} {
		t.Run(name, func(t *testing.T) {
			e := newFixLoopEnv(t)
			e.reviewOut(0, findings)
			e.fixHook(1, "echo should-not-run > fixed.txt")

			code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

			e.assertRefused(code)
			if want := []string{"review"}; !equalStrings(e.callRoles(), want) {
				t.Errorf("expected only the review to run, got %v", e.callRoles())
			}
			if !strings.Contains(stdout+stderr, "SOMETHING_WRONG") || !strings.Contains(stdout+stderr, "ask-user") {
				t.Errorf("expected the ask-user finding to be printed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if !strings.Contains(stderr, "need a human decision") || !strings.Contains(stderr, "prompt "+e.task.ID) {
				t.Errorf("expected the refusal to say a human decision is needed and how to give it, got:\n%s", stderr)
			}
			if _, err := os.Stat(filepath.Join(e.task.CampPath, "fixed.txt")); err == nil {
				t.Error("expected no fixer to have touched the camp")
			}
		})
	}
}

// A fixer's own edit can raise a question only a human can answer: the loop
// stops right there instead of fixing around it.
func TestShipFixLoop_AskUserAfterAFixStopsTheLoop(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(1, []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, "echo one > fix1.txt")
	e.reviewOut(2, []string{reviewFinding("warning", "ask-user")}, "fix1.txt")
	e.fixHook(2, "echo two > fix2.txt")

	code, _, stderr := e.ship(tribunal.Options{Fix: true})

	e.assertRefused(code)
	if want := []string{"review", "fix", "review"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	if !strings.Contains(stderr, "need a human decision") {
		t.Errorf("expected the refusal to say a human decision is needed, got:\n%s", stderr)
	}
}

// A fixer that changes nothing cannot loop: the ship stops with a message
// that names the fixer.
func TestShipFixLoop_FixerThatChangesNothingStops(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(0, []string{reviewFinding("error", "auto-fix")})

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

	e.assertRefused(code)
	if want := []string{"review", "fix"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	if !strings.Contains(stdout+stderr, "the fixer made no changes") {
		t.Errorf("expected the fixer's no-op to be reported\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "failed at fix") {
		t.Errorf("expected the refusal to name the fix step, got:\n%s", stderr)
	}
}

// After a fix round and no title from the reviewer, the pull request title
// comes from the mission's own commit, never from the fixer's.
func TestShipFixLoop_PRTitleIsNeverTheFixersCommit(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(1, []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, "echo fixed > fixed.txt")
	e.reviewOut(2, nil, "fixed.txt")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true})

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	args, err := os.ReadFile(e.ghArgs)
	if err != nil {
		t.Fatalf("expected gh pr create to run: %v", err)
	}
	lines := strings.Split(string(args), "\n")
	title := ""
	for i, l := range lines {
		if l == "--title" && i+1 < len(lines) {
			title = lines[i+1]
		}
	}
	if title != "change" {
		t.Errorf("expected the title to come from the mission's commit, got %q", title)
	}
	if strings.Contains(string(args), "address tribunal review findings") {
		t.Errorf("expected the PR text to leave out the fixer's commit, got:\n%s", args)
	}
}

// A fixer that times out may have edited files already. Nothing is committed
// or pushed, and the refusal says the partial edits are still in the camp.
func TestShipFixLoop_FixerTimeoutLeavesAMessageAboutItsPartialEdits(t *testing.T) {
	e := newFixLoopEnv(t)
	e.reviewOut(0, []string{reviewFinding("error", "auto-fix")})
	e.fixHook(1, "echo partial > partial.txt\nexec sleep 30")

	code, stdout, stderr := e.ship(tribunal.Options{Fix: true, Timeout: 4 * time.Second})

	e.assertRefused(code)
	if want := []string{"review", "fix"}; !equalStrings(e.callRoles(), want) {
		t.Errorf("expected claude calls %v, got %v", want, e.callRoles())
	}
	if !strings.Contains(stderr, "failed at fix") || !strings.Contains(stdout+stderr, "timed out") {
		t.Errorf("expected the refusal to blame the fixer's timeout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "uncommitted") {
		t.Errorf("expected the refusal to say the fixer's partial edits are left uncommitted in the camp, got:\n%s", stderr)
	}
}
