package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// leaseGh answers "pr view" with an open pull request and records every
// "pr edit", so a re-ship can reuse the PR and refresh its text.
const leaseGh = `case "$1 $2" in
  "pr view") echo '{"number":7,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/7"}' ;;
  "pr create") echo "https://github.com/x/y/pull/7" ;;
  "pr edit") echo edited >> "$EDIT_LOG" ;;
esac`

// leaseScenario is a done mission whose branch has been shipped once to a
// local bare origin that logs every ref update it receives.
type leaseScenario struct {
	t           *testing.T
	project     string
	home        string
	task        state.Task
	campPath    string
	origin      string
	updatesLog  string
	editLog     string
	firstTipSHA string
}

func newLeaseScenario(t *testing.T) *leaseScenario {
	t.Helper()
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	origin := runGit(t, project, "remote", "get-url", "origin")

	// The base branch is on origin too, so a push that touched it would show.
	runGit(t, project, "push", "origin", "main")

	// Log "<ref>" for every ref update origin receives from now on.
	updatesLog := filepath.Join(t.TempDir(), "updates.log")
	hook := "#!/bin/sh\nwhile read old new ref; do echo \"$ref\" >> '" + updatesLog + "'; done\n"
	if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatalf("writing pre-receive hook: %v", err)
	}

	s := &leaseScenario{
		t: t, project: project, home: home, task: task,
		campPath: task.CampPath, origin: origin,
		updatesLog: updatesLog, editLog: filepath.Join(t.TempDir(), "edits.log"),
	}
	t.Setenv("EDIT_LOG", s.editLog)
	out, code := s.ship()
	if code != 0 {
		t.Fatalf("first ship failed (%d): %s", code, out)
	}
	s.reload()
	s.firstTipSHA = s.originTip(task.CampBranch)
	if s.firstTipSHA == "" || s.task.LastPushedSHA != s.firstTipSHA {
		t.Fatalf("first ship should record the pushed tip %q, task has %q", s.firstTipSHA, s.task.LastPushedSHA)
	}
	if got := s.campTip(); got != s.firstTipSHA {
		t.Fatalf("origin tip %s is not the camp tip %s", s.firstTipSHA, got)
	}
	return s
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (s *leaseScenario) ship() (string, int) {
	s.t.Helper()
	s.t.Setenv("PATH", shipToolsPath(s.t, passingReview, leaseGh))
	var out bytes.Buffer
	code := runShip(s.project, s.home, s.task.ID, shipOptions{}, &out, &out)
	return out.String(), code
}

func (s *leaseScenario) reload() {
	s.t.Helper()
	root, err := vxproject.Root(s.home, s.project)
	if err != nil {
		s.t.Fatalf("project.Root: %v", err)
	}
	s.task, err = state.Load(root, s.task.ID)
	if err != nil {
		s.t.Fatalf("state.Load: %v", err)
	}
}

func (s *leaseScenario) originTip(branch string) string {
	s.t.Helper()
	out := runGit(s.t, s.origin, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return out
}

func (s *leaseScenario) campTip() string {
	return runGit(s.t, s.campPath, "rev-parse", "HEAD")
}

// rebase moves the base branch forward and rebases the camp onto it, the way
// a soldier asked to catch up with main does.
func (s *leaseScenario) rebase() {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.project, "base.txt"), []byte("base\n"), 0o644); err != nil {
		s.t.Fatalf("writing base.txt: %v", err)
	}
	runGit(s.t, s.project, "add", "base.txt")
	runGit(s.t, s.project, "commit", "-q", "-m", "base moved")
	runGit(s.t, s.campPath, "rebase", "main")
	if s.campTip() == s.firstTipSHA {
		s.t.Fatal("the rebase should have rewritten the branch tip")
	}
}

func (s *leaseScenario) updatedRefs() []string {
	data, err := os.ReadFile(s.updatesLog)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func (s *leaseScenario) edits() int {
	data, _ := os.ReadFile(s.editLog)
	return strings.Count(string(data), "edited")
}

// The real case: the soldier rebased a branch ship had pushed. The tip origin
// holds is the one ship recorded, so the push is retried with a lease, says so,
// records the new tip and refreshes the pull request.
func TestRunShip_RebasedBranchIsRewrittenWithALease(t *testing.T) {
	s := newLeaseScenario(t)
	baseBefore := s.originTip("main")
	s.rebase()
	newTip := s.campTip()

	out, code := s.ship()

	if code != 0 {
		t.Fatalf("expected the rebased branch to ship, got %d: %s", code, out)
	}
	if got := s.originTip(s.task.CampBranch); got != newTip {
		t.Errorf("origin's %s should be the rebased tip %s, got %s", s.task.CampBranch, newTip, got)
	}
	for _, want := range []string{"rewrote the PR branch " + s.task.CampBranch, s.firstTipSHA, newTip, "https://github.com/x/y/pull/7"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to contain %q, got: %s", want, out)
		}
	}
	s.reload()
	if s.task.LastPushedSHA != newTip {
		t.Errorf("expected the new tip %s recorded, got %q", newTip, s.task.LastPushedSHA)
	}
	if s.task.Status != state.StatusShipped {
		t.Errorf("expected the task shipped, got %q", s.task.Status)
	}
	if s.edits() != 2 {
		t.Errorf("expected the pull request refreshed on both ships, got %d edits", s.edits())
	}

	// Only the mission branch was ever pushed: the base branch is untouched.
	for _, ref := range s.updatedRefs() {
		if ref != "refs/heads/"+s.task.CampBranch {
			t.Errorf("origin received an update to %s, only the mission branch may be pushed", ref)
		}
	}
	if got := s.originTip("main"); got != baseBefore {
		t.Errorf("the base branch moved on origin from %s to %s", baseBefore, got)
	}
}

// Somebody else pushed to the branch since ship did: nothing is forced, and the
// message says what is on the remote and how to decide.
func TestRunShip_ThirdPartyPushIsNeverForced(t *testing.T) {
	s := newLeaseScenario(t)

	other := filepath.Join(t.TempDir(), "other")
	runGit(t, t.TempDir(), "clone", "-q", "-b", s.task.CampBranch, s.origin, other)
	if err := os.WriteFile(filepath.Join(other, "theirs.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatalf("writing theirs.txt: %v", err)
	}
	runGit(t, other, "add", "theirs.txt")
	runGit(t, other, "commit", "-q", "-m", "third party")
	runGit(t, other, "push", "-q", "origin", s.task.CampBranch)
	theirs := s.originTip(s.task.CampBranch)

	s.rebase()
	out, code := s.ship()

	if code == 0 {
		t.Fatalf("expected the ship to refuse, got: %s", out)
	}
	for _, want := range []string{"will not force", theirs, s.firstTipSHA, "git fetch origin", "--force-with-lease=" + s.task.CampBranch + ":" + theirs} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the message to contain %q, got: %s", want, out)
		}
	}
	if got := s.originTip(s.task.CampBranch); got != theirs {
		t.Errorf("the third party's commit must stay on origin: %s, got %s", theirs, got)
	}
	s.reload()
	if s.task.LastPushedSHA != s.firstTipSHA {
		t.Errorf("a refused push must not change the recorded tip, got %q", s.task.LastPushedSHA)
	}
	if s.edits() != 1 {
		t.Errorf("a refused ship must not refresh the pull request again, got %d edits", s.edits())
	}
}

// A task with no recorded push (shipped before this was tracked) cannot prove
// the remote tip is its own, so it is never forced.
func TestRunShip_NoRecordedPushIsNeverForced(t *testing.T) {
	s := newLeaseScenario(t)
	s.task.LastPushedSHA = ""
	root, err := vxproject.Root(s.home, s.project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(root, s.task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	s.rebase()
	out, code := s.ship()

	if code == 0 {
		t.Fatalf("expected the ship to refuse, got: %s", out)
	}
	for _, want := range []string{"will not force", "no push of this task is recorded", s.firstTipSHA} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the message to contain %q, got: %s", want, out)
		}
	}
	if got := s.originTip(s.task.CampBranch); got != s.firstTipSHA {
		t.Errorf("origin's branch must stay %s, got %s", s.firstTipSHA, got)
	}
	if refs := s.updatedRefs(); len(refs) != 1 {
		t.Errorf("only the first ship may have pushed anything, got updates %v", refs)
	}
}

// A plain follow-up commit still fast-forwards without any lease or rewrite
// message.
func TestRunShip_FastForwardNeedsNoLease(t *testing.T) {
	s := newLeaseScenario(t)
	if err := os.WriteFile(filepath.Join(s.campPath, "change.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatalf("writing change.txt: %v", err)
	}
	runGit(t, s.campPath, "add", "change.txt")
	runGit(t, s.campPath, "commit", "-q", "-m", "follow-up")

	out, code := s.ship()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out)
	}
	if strings.Contains(out, "rewrote") {
		t.Errorf("a fast-forward is not a rewrite, got: %s", out)
	}
	s.reload()
	if s.task.LastPushedSHA != s.campTip() {
		t.Errorf("expected the new tip recorded, got %q", s.task.LastPushedSHA)
	}
}

// The base branch and an empty name are never pushed, whatever the caller
// hands in.
func TestPushShipBranch_RefusesTheBaseBranch(t *testing.T) {
	for _, branch := range []string{"main", "", "-f"} {
		var out bytes.Buffer
		if _, err := pushShipBranch(t.TempDir(), branch, "main", "abc", &out); err == nil {
			t.Errorf("expected pushing %q to be refused", branch)
		}
	}
}
