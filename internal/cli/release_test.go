package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// releaseGitT runs git in dir and returns its trimmed output.
func releaseGitT(t *testing.T, dir string, args ...string) string {
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
	m.campHead = releaseGitT(t, task.CampPath, "rev-parse", "HEAD")
	for _, step := range []struct{ content, msg string }{
		{"hi\n", "squash-merge of the mission"},
		{"later\n", "later change to the same lines"},
	} {
		if err := os.WriteFile(filepath.Join(project, "change.txt"), []byte(step.content), 0o644); err != nil {
			t.Fatal(err)
		}
		releaseGitT(t, project, "add", "-A")
		releaseGitT(t, project, "commit", "-q", "-m", step.msg)
		if m.mergeCommit == "" {
			m.mergeCommit = releaseGitT(t, project, "rev-parse", "HEAD")
		}
	}
	return m
}

// ghForRelease puts a stub gh and the tools it needs on PATH. Every call is
// appended to the returned log file. auth status succeeds when loggedIn;
// pr view answers prView.
func ghForRelease(t *testing.T, loggedIn bool, prView string) (calls string) {
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

func releaseOf(t *testing.T, m remotelyMergedMission, opts releaseOptions) (code int, out string) {
	t.Helper()
	var buf bytes.Buffer
	code = runRelease(m.project, m.home, t.TempDir(), m.task.ID, opts, &fakeHerdr{}, &buf, &buf)
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
func TestRunRelease_ShippedTaskWithAMergedPullRequestReleases(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	calls := ghForRelease(t, true, mergedPRView(m, m.mergeCommit))

	code, out := releaseOf(t, m, releaseOptions{})

	if code != 0 || !strings.Contains(out, "released:") {
		t.Fatalf("expected the merged PR to release the camp, got %d: %s", code, out)
	}
	if campLeased(t, m) {
		t.Error("expected the camp returned to the pool")
	}
	if !strings.Contains(readCalls(t, calls), "pr view "+m.task.CampBranch) {
		t.Errorf("expected gh asked about the camp branch, got: %s", readCalls(t, calls))
	}
}

// Merged on GitHub, not pulled yet: the general is told to pull.
func TestRunRelease_MergedPullRequestNotPulledYetTellsToPull(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForRelease(t, true, mergedPRView(m, "0123456789abcdef0123456789abcdef01234567"))

	code, out := releaseOf(t, m, releaseOptions{})

	if code == 0 {
		t.Fatalf("expected a refusal, got: %s", out)
	}
	for _, want := range []string{"merged on GitHub", "git pull on main"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the refusal to contain %q, got: %s", want, out)
		}
	}
	if !campLeased(t, m) {
		t.Error("a refused release must keep the camp leased")
	}
}

// A shipped task whose PR is still open: the refusal says to merge it and pull.
func TestRunRelease_ShippedRefusalSaysToMergeThePullRequestAndPull(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForRelease(t, true, openPRView)

	code, out := releaseOf(t, m, releaseOptions{})

	if code == 0 {
		t.Fatalf("expected a refusal, got: %s", out)
	}
	for _, want := range []string{"merge the pull request", "git pull on main", "then retry"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the refusal to contain %q, got: %s", want, out)
		}
	}
}

// gh is best effort: logged out or missing, release falls back to the
// local checks and still gives the merge-and-pull message.
func TestRunRelease_GhUnavailableFallsBackToTheLocalChecks(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		m := newRemotelyMergedMission(t, state.StatusShipped)
		calls := ghForRelease(t, false, mergedPRView(m, m.mergeCommit))

		code, out := releaseOf(t, m, releaseOptions{})

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

		code, out := releaseOf(t, m, releaseOptions{})

		if code == 0 || !strings.Contains(out, "git pull on main") {
			t.Fatalf("expected the local refusal with the pull hint, got %d: %s", code, out)
		}
	})
	t.Run("lookup fails", func(t *testing.T) {
		m := newRemotelyMergedMission(t, state.StatusShipped)
		ghForRelease(t, true, "not json at all")

		code, out := releaseOf(t, m, releaseOptions{})

		if code == 0 || !strings.Contains(out, "git pull on main") {
			t.Fatalf("expected the local refusal with the pull hint, got %d: %s", code, out)
		}
	})
}

// No network call for a task that is not shipped.
func TestRunRelease_NonShippedTaskNeverCallsGh(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusDone)
	calls := ghForRelease(t, true, mergedPRView(m, m.mergeCommit))

	code, out := releaseOf(t, m, releaseOptions{})

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

func TestRunRelease_DiscardPrintsWhatItThrewAway(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForRelease(t, true, openPRView)
	subjectHash := releaseGitT(t, m.task.CampPath, "log", "-1", "--format=%h")
	if err := os.WriteFile(filepath.Join(m.task.CampPath, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := releaseOf(t, m, releaseOptions{Discard: true})

	if code != 0 || !strings.Contains(out, "released:") {
		t.Fatalf("expected --discard to release, got %d: %s", code, out)
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
func TestRunRelease_UncommittedChangesRefuseWithoutDiscard(t *testing.T) {
	m := newRemotelyMergedMission(t, state.StatusShipped)
	ghForRelease(t, true, mergedPRView(m, m.mergeCommit))
	if err := os.WriteFile(filepath.Join(m.task.CampPath, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := releaseOf(t, m, releaseOptions{})

	if code == 0 || !strings.Contains(out, "uncommitted changes") {
		t.Fatalf("expected the dirty camp refused, got %d: %s", code, out)
	}
	if !campLeased(t, m) {
		t.Error("a refused release must keep the camp leased")
	}
}
