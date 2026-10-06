package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// strikeGitT runs git in dir and returns its trimmed output.
func strikeGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// remotelyMergedMission is a mission whose camp holds one commit that was
// merged remotely (the same content reached the base as a single commit) and
// then changed again on the base, so only GitHub's word proves it landed.
type remotelyMergedMission struct {
	project, home string
	task          state.Task
	mergeCommit   string
	campHead      string
}

func newRemotelyMergedMission(t *testing.T, status state.Status) remotelyMergedMission {
	t.Helper()
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Status = status
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	m := remotelyMergedMission{project: project, home: home, task: task}
	m.campHead = strikeGitT(t, task.CampPath, "rev-parse", "HEAD")
	for _, step := range []struct{ content, msg string }{
		{"hi\n", "squash-merge of the mission"},
		{"later\n", "later change to the same lines"},
	} {
		if err := os.WriteFile(filepath.Join(project, "change.txt"), []byte(step.content), 0o644); err != nil {
			t.Fatal(err)
		}
		strikeGitT(t, project, "add", "-A")
		strikeGitT(t, project, "commit", "-q", "-m", step.msg)
		if m.mergeCommit == "" {
			m.mergeCommit = strikeGitT(t, project, "rev-parse", "HEAD")
		}
	}
	return m
}

// ghForStrike puts a stub gh and the tools it needs on PATH. Every call is
// appended to the returned log file. auth status succeeds when loggedIn;
// pr view answers prView.
func ghForStrike(t *testing.T, loggedIn bool, prView string) (calls string) {
	t.Helper()
	dir := gitOnlyPath(t)
	calls = filepath.Join(t.TempDir(), "gh-calls")
	auth := "exit 1"
	if loggedIn {
		auth = "exit 0"
	}
	script := "#!/bin/sh\necho \"$@\" >> '" + calls + "'\n" +
		"case \"$1 $2\" in\n" +
		"  \"auth status\") " + auth + " ;;\n" +
		"  \"pr view\") cat <<'EOF'\n" + prView + "\nEOF\n ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	realCat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realCat, filepath.Join(dir, "cat")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return calls
}

func mergedPRView(m remotelyMergedMission, mergeCommit string) string {
	return `{"number":9,"state":"MERGED","isDraft":false,"mergeable":"UNKNOWN","headRefOid":"` + m.campHead + `","url":"https://github.com/x/y/pull/9","mergeCommit":{"oid":"` + mergeCommit + `"}}`
}

const openPRView = `{"number":9,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc","url":"https://github.com/x/y/pull/9","mergeCommit":null}`

func strikeOf(t *testing.T, m remotelyMergedMission, opts strikeOptions) (code int, out string) {
	t.Helper()
	var buf bytes.Buffer
	code = runStrike(m.project, m.home, m.task.ID, opts, &fakeHerdr{}, &buf, &buf)
	return code, buf.String()
}

func campLeased(t *testing.T, m remotelyMergedMission) bool {
	t.Helper()
	projectRoot, err := vxproject.Root(m.home, m.project)
	if err != nil {
		t.Fatal(err)
	}
	leased, err := camp.LeasedTasks(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	return leased[m.task.ID]
}

func readCalls(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// GitHub says the PR merged and its merge commit is on the local base: the
// content check would refuse forever (later commits touched the same lines),
// but the merge is trusted.
func TestRunStrike_ShippedTaskWithAMergedPullRequestStrikes(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	calls := ghForStrike(t, true, mergedPRView(m, m.mergeCommit))

	code, out := strikeOf(t, m, strikeOptions{})

	if code != 0 || !strings.Contains(out, "struck:") {
		t.Fatalf("expected the merged PR to strike the camp, got %d: %s", code, out)
	}
	if campLeased(t, m) {
		t.Error("expected the camp returned to the pool")
	}
	if !strings.Contains(readCalls(t, calls), "pr view "+m.task.CampBranch) {
		t.Errorf("expected gh asked about the camp branch, got: %s", readCalls(t, calls))
	}
}

// Merged on GitHub, not pulled yet: the general is told to pull.
func TestRunStrike_MergedPullRequestNotPulledYetTellsToPull(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, mergedPRView(m, "0123456789abcdef0123456789abcdef01234567"))

	code, out := strikeOf(t, m, strikeOptions{})

	if code == 0 {
		t.Fatalf("expected a refusal, got: %s", out)
	}
	for _, want := range []string{"merged on GitHub", "git pull on main"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the refusal to contain %q, got: %s", want, out)
		}
	}
	if !campLeased(t, m) {
		t.Error("a refused strike must keep the camp leased")
	}
}

// A shipped task whose PR is still open: the refusal says to merge it and pull.
func TestRunStrike_ShippedRefusalSaysToMergeThePullRequestAndPull(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, openPRView)

	code, out := strikeOf(t, m, strikeOptions{})

	if code == 0 {
		t.Fatalf("expected a refusal, got: %s", out)
	}
	for _, want := range []string{"merge the pull request", "git pull on main", "then retry"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the refusal to contain %q, got: %s", want, out)
		}
	}
}

// gh is best effort: logged out or missing, strike falls back to the
// local checks and still gives the merge-and-pull message.
func TestRunStrike_GhUnavailableFallsBackToTheLocalChecks(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		m := newRemotelyMergedMission(t, state.StatusShipped)
		calls := ghForStrike(t, false, mergedPRView(m, m.mergeCommit))

		code, out := strikeOf(t, m, strikeOptions{})

		if code == 0 || !strings.Contains(out, "git pull on main") {
			t.Fatalf("expected the local refusal with the pull hint, got %d: %s", code, out)
		}
		if strings.Contains(readCalls(t, calls), "pr view") {
			t.Errorf("a logged-out gh must not be asked about the PR, got: %s", readCalls(t, calls))
		}
	})
	t.Run("not installed", func(t *testing.T) {
		m := newRemotelyMergedMission(t, state.StatusShipped)
		t.Setenv("PATH", gitOnlyPath(t))

		code, out := strikeOf(t, m, strikeOptions{})

		if code == 0 || !strings.Contains(out, "git pull on main") {
			t.Fatalf("expected the local refusal with the pull hint, got %d: %s", code, out)
		}
	})
	t.Run("lookup fails", func(t *testing.T) {
		m := newRemotelyMergedMission(t, state.StatusShipped)
		ghForStrike(t, true, "not json at all")

		code, out := strikeOf(t, m, strikeOptions{})

		if code == 0 || !strings.Contains(out, "git pull on main") {
			t.Fatalf("expected the local refusal with the pull hint, got %d: %s", code, out)
		}
	})
}

// No network call for a task that is not shipped.
func TestRunStrike_NonShippedTaskNeverCallsGh(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusDone)
	calls := ghForStrike(t, true, mergedPRView(m, m.mergeCommit))

	code, out := strikeOf(t, m, strikeOptions{})

	if code == 0 {
		t.Fatalf("expected the content check to refuse a done task, got: %s", out)
	}
	if strings.Contains(out, "shipped as a pull request") {
		t.Errorf("a done task is not told it shipped a PR, got: %s", out)
	}
	if got := readCalls(t, calls); got != "" {
		t.Errorf("expected no gh call for a task that is not shipped, got: %s", got)
	}
}

func TestRunStrike_DiscardPrintsWhatItThrewAway(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, openPRView)
	subjectHash := strikeGitT(t, m.task.CampPath, "log", "-1", "--format=%h")
	if err := os.WriteFile(filepath.Join(m.task.CampPath, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := strikeOf(t, m, strikeOptions{Discard: true})

	if code != 0 || !strings.Contains(out, "struck:") {
		t.Fatalf("expected --discard to strike, got %d: %s", code, out)
	}
	for _, want := range []string{"discarded 1 unlanded commit(s)", subjectHash + " change", "discarded 1 uncommitted change(s)", "scratch.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to contain %q, got: %s", want, out)
		}
	}
	if campLeased(t, m) {
		t.Error("expected the camp returned to the pool")
	}
}

// Without the flag, uncommitted changes still refuse.
func TestRunStrike_UncommittedChangesRefuseWithoutDiscard(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, mergedPRView(m, m.mergeCommit))
	if err := os.WriteFile(filepath.Join(m.task.CampPath, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := strikeOf(t, m, strikeOptions{})

	if code == 0 || !strings.Contains(out, "uncommitted changes") {
		t.Fatalf("expected the dirty camp refused, got %d: %s", code, out)
	}
	if !campLeased(t, m) {
		t.Error("a refused strike must keep the camp leased")
	}
}

func projectHasBranch(t *testing.T, m remotelyMergedMission) bool {
	t.Helper()
	return strikeGitT(t, m.project, "branch", "--list", m.task.CampBranch) != ""
}

// A merged pull request is not an ancestor of the base, so git branch -d
// would refuse; the merge GitHub confirmed is what lets the branch go.
func TestRunStrike_MergedPullRequestDeletesTheBranch(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, mergedPRView(m, m.mergeCommit))

	code, out := strikeOf(t, m, strikeOptions{})

	if code != 0 || !strings.Contains(out, "pruned: deleted branch "+m.task.CampBranch) {
		t.Fatalf("expected the branch pruned, got %d: %s", code, out)
	}
	if projectHasBranch(t, m) {
		t.Error("expected the branch deleted")
	}
}

// Without gh, the content check lets the strike through but proves nothing
// about the branch, so it stays and the output says what to do.
func TestRunStrike_ContentOnlyLandingKeepsTheBranch(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusDone)
	// Make the content check pass: the base must hold the camp's content
	// at its tip.
	if err := os.WriteFile(filepath.Join(m.project, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	strikeGitT(t, m.project, "commit", "-q", "-am", "back to the mission content")

	code, out := strikeOf(t, m, strikeOptions{})

	if code != 0 {
		t.Fatalf("expected the strike to pass, got %d: %s", code, out)
	}
	if !projectHasBranch(t, m) || !strings.Contains(out, "kept branch "+m.task.CampBranch) || !strings.Contains(out, "git branch -D "+m.task.CampBranch) {
		t.Errorf("expected the branch kept with a hint, got: %s", out)
	}
}

// --discard never deletes an unlanded branch: its commits stay reachable.
func TestRunStrike_DiscardKeepsAnUnlandedBranch(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForStrike(t, true, openPRView)

	code, out := strikeOf(t, m, strikeOptions{Discard: true})

	if code != 0 || !projectHasBranch(t, m) || strings.Contains(out, "deleted branch") {
		t.Fatalf("expected the unlanded branch kept, got %d: %s", code, out)
	}
}

// A fast-forward landed branch is an ancestor: plain branch -d semantics.
func TestRunStrike_LandedBranchIsDeleted(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	strikeGitT(t, project, "merge", "--ff-only", task.CampBranch)
	m := remotelyMergedMission{project: project, home: home, task: task}

	code, out := strikeOf(t, m, strikeOptions{})

	if code != 0 || projectHasBranch(t, m) || !strings.Contains(out, "pruned: deleted branch "+task.CampBranch) {
		t.Fatalf("expected the landed branch deleted, got %d: %s", code, out)
	}
}

// interruptedMissionWithGonePane is a mission stopped by hand: its task is
// interrupted, its herdr tab no longer exists, and its camp holds one
// unlanded commit plus an uncommitted file.
func interruptedMissionWithGonePane(t *testing.T) (remotelyMergedMission, *fakeHerdr) {
	t.Helper()
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Status = state.StatusInterrupted
	task.HerdrTabID = "w11:t1F"
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.CampPath, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeHerdr{tabCloseErr: &herdr.APIError{Code: "tab_not_found", Message: "tab w11:t1F not found"}}
	return remotelyMergedMission{project: project, home: home, task: task}, client
}

func strikeWith(t *testing.T, m remotelyMergedMission, client *fakeHerdr, opts strikeOptions) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	code := runStrike(m.project, m.home, m.task.ID, opts, client, &buf, &buf)
	return code, buf.String()
}

func loadTask(t *testing.T, m remotelyMergedMission) state.Task {
	t.Helper()
	projectRoot, err := vxproject.Root(m.home, m.project)
	if err != nil {
		t.Fatal(err)
	}
	task, err := state.Load(projectRoot, m.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The general's report: --discard on an interrupted task whose tab is gone
// reset the camp, then aborted on the missing tab and left a half state.
func TestRunStrike_DiscardWithGonePaneClosesTheTask(t *testing.T) {
	m, client := interruptedMissionWithGonePane(t)

	code, out := strikeWith(t, m, client, strikeOptions{Discard: true})

	if code != 0 || !strings.Contains(out, "struck:") {
		t.Fatalf("expected the strike to complete despite the missing tab, got %d: %s", code, out)
	}
	if campLeased(t, m) {
		t.Error("expected the slot released")
	}
	if got := loadTask(t, m).Status; got != state.StatusStruck {
		t.Errorf("expected the task marked %s, got %s", state.StatusStruck, got)
	}
	if !projectHasBranch(t, m) {
		t.Error("the strike must never delete an unlanded branch")
	}
	if branch := strikeGitT(t, m.task.CampPath, "branch", "--show-current"); branch != "" {
		t.Errorf("expected the pool worktree detached, still on %q", branch)
	}
	want := "kept branch " + m.task.CampBranch + " with 1 unlanded commit(s)"
	if !strings.Contains(out, want) {
		t.Errorf("expected the branch hint %q, got: %s", want, out)
	}
}

// A second strike after the first one finished (or half finished) must
// clean up instead of refusing with "not leased".
func TestRunStrike_SecondStrikeIsIdempotent(t *testing.T) {
	m, client := interruptedMissionWithGonePane(t)
	if code, out := strikeWith(t, m, client, strikeOptions{Discard: true}); code != 0 {
		t.Fatalf("first strike: %d: %s", code, out)
	}
	// Simulate the half state the general hit: slot free, task not closed.
	projectRoot, err := vxproject.Root(m.home, m.project)
	if err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, m)
	task.Status = state.StatusInterrupted
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	code, out := strikeWith(t, m, client, strikeOptions{Discard: true})

	if code != 0 || strings.Contains(out, "not leased") {
		t.Fatalf("expected the second strike to succeed, got %d: %s", code, out)
	}
	if got := loadTask(t, m).Status; got != state.StatusStruck {
		t.Errorf("expected the task marked %s, got %s", state.StatusStruck, got)
	}
}

// A slot now leased to another task is never reset, detached or released.
func TestRunStrike_SecondStrikeNeverTouchesACampLeasedToAnotherTask(t *testing.T) {
	m, client := interruptedMissionWithGonePane(t)
	if code, out := strikeWith(t, m, client, strikeOptions{Discard: true}); code != 0 {
		t.Fatalf("first strike: %d: %s", code, out)
	}
	other, err := state.New(state.KindMission, "other")
	if err != nil {
		t.Fatal(err)
	}
	c, err := camp.Acquire(m.project, m.home, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "other.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branchBefore := strikeGitT(t, c.Path, "branch", "--show-current")

	strikeWith(t, m, client, strikeOptions{Discard: true})

	if _, err := os.Stat(filepath.Join(c.Path, "other.txt")); err != nil {
		t.Errorf("another task's camp was reset: %v", err)
	}
	if got := strikeGitT(t, c.Path, "branch", "--show-current"); got != branchBefore {
		t.Errorf("another task's camp moved from %q to %q", branchBefore, got)
	}
	projectRoot, _ := vxproject.Root(m.home, m.project)
	leased, err := camp.LeasedTasks(projectRoot)
	if err != nil || !leased[other.ID] {
		t.Errorf("another task's lease was released: %v %v", leased, err)
	}
}

// Other herdr errors still fail the strike, but never leave a half state:
// the slot is free and the task is closed, and the error says so.
func TestRunStrike_OtherHerdrErrorLeavesACoherentState(t *testing.T) {
	m, client := interruptedMissionWithGonePane(t)
	client.tabCloseErr = errors.New("herdr socket closed")

	code, out := strikeWith(t, m, client, strikeOptions{Discard: true})

	if code == 0 || !strings.Contains(out, "closing soldier pane") {
		t.Fatalf("expected the pane error reported, got %d: %s", code, out)
	}
	if campLeased(t, m) {
		t.Error("expected the slot released")
	}
	if got := loadTask(t, m).Status; got != state.StatusStruck {
		t.Errorf("expected the task marked %s, got %s", state.StatusStruck, got)
	}
	client.tabCloseErr = nil
	if code, out := strikeWith(t, m, client, strikeOptions{}); code != 0 {
		t.Errorf("expected a retry to close the pane, got %d: %s", code, out)
	}
}
