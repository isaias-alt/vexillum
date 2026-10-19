package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/piask"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// askQuestionID is the id the fake pi gives its question.
const askQuestionID = "0123456789abcdef0123456789abcdef"

// askExtensionArgs is the tail of the argv a pi soldier is started with: the
// vexillum-owned ask_user extension (under the project's state directory,
// never the camp) and the directory of the task's questions.
func askExtensionArgs(t *testing.T, project, home, taskID string) string {
	t.Helper()
	root, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	return "-e\n" + piask.ExtensionPath(root) + "\n--vx-ask-dir\n" + piask.Dir(root, taskID)
}

// fakePiAsking is a fake pi that emulates the ask_user protocol, not a model:
// started with the extension's flags it records a question the way the
// extension does (atomically, before anything else), then waits in the
// background for an answer file addressed to that question's id. Taking it
// copies it to answer.received and clears the files, as the extension does;
// with consume false it never takes it, like a session that hung.
func fakePiAsking(t *testing.T, consume bool) (rec string) {
	t.Helper()
	bin := fakeBinDir(t, "herdr")
	rec = t.TempDir()
	// The PATH of these tests holds only herdr and git: give the stub the
	// few tools it runs.
	for _, tool := range []string{"mkdir", "mv", "grep", "sleep", "cp", "rm"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s not found on PATH: %v", tool, err)
		}
		if err := os.Symlink(real, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	take := `cp "$dir/answer.json" "$d/answer.received"; rm -f "$dir/question.json" "$dir/answer.json"`
	if !consume {
		take = `:`
	}
	writeStub(t, bin, "pi", `d='`+rec+`'
printf '%s\n' "$@" > "$d/args.1"
dir=
while [ $# -gt 0 ]; do
  case "$1" in --vx-ask-dir) dir="$2"; shift;; esac
  shift
done
[ -n "$dir" ] || exit 0
mkdir -p "$dir"
printf '%s\n' '{"version":1,"id":"`+askQuestionID+`","question":"Which database?","options":["Postgres","SQLite"],"asked_at":"2026-01-02T03:04:05Z"}' > "$dir/question.json.tmp"
mv "$dir/question.json.tmp" "$dir/question.json"
( n=0; until grep -q '`+askQuestionID+`' "$dir/answer.json" 2>/dev/null; do n=$((n+1)); [ "$n" -le 600 ] && [ -d "$d" ] && [ ! -e "$d/stop" ] || exit 0; sleep 0.05; done; `+take+` ) >/dev/null 2>&1 &
echo "pi started"`)
	t.Setenv("PATH", bin)
	t.Cleanup(func() { requireNoLeakedProcs(t, rec, filepath.Join(bin, "pi")) })
	return rec
}

// requireNoLeakedProcs fails the test if a process the fake pi started is
// still alive once the test is over. The fake waits in the background for an
// answer that some tests never send, so it is told to stop (the stop file,
// also implied by rec going away) and given a moment to exit; a straggler is
// killed by pid and reported. Only processes whose command line holds this
// test's own pi stub path are considered, never anything else.
func requireNoLeakedProcs(t *testing.T, rec, stubPath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(rec, "stop"), nil, 0o644); err != nil {
		t.Errorf("writing the stop file: %v", err)
	}
	alive := func() []string {
		out, err := exec.Command("/bin/ps", "-eo", "pid,command").Output()
		if err != nil {
			t.Errorf("ps: %v", err)
			return nil
		}
		var pids []string
		for _, line := range strings.Split(string(out), "\n") {
			if f := strings.Fields(line); len(f) > 1 && strings.Contains(line, stubPath) {
				pids = append(pids, f[0])
			}
		}
		return pids
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(alive()) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if left := alive(); len(left) > 0 {
		for _, pid := range left {
			if n, err := strconv.Atoi(pid); err == nil {
				_ = syscall.Kill(n, syscall.SIGKILL)
			}
		}
		t.Errorf("the fake pi left processes running after the test: pids %v", left)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// dispatchBlockedOnAsk dispatches a pi mission whose fake pi asks a question
// at startup, with herdr reporting it blocked, as it would once the stock Pi
// integration saw the extension's herdr:blocked event.
func dispatchBlockedOnAsk(t *testing.T, consume bool) (project, home, rec string, task state.Task, client *piHerdr) {
	t.Helper()
	rec = fakePiAsking(t, consume)
	project, home = piDispatchProject(t)
	client = newPiHerdr()
	client.promptStatus = "blocked"
	client.readOutput = "pi is waiting on ask_user"

	var out bytes.Buffer
	if code := runDispatch(project, home, "w1", "pick a database", state.KindMission, "", "", "pi", client, &out, &out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	return project, home, rec, dispatchedTask(t, project, home), client
}

// The whole path for a soldier blocked in ask_user: the extension is loaded
// from vexillum's state directory with the task's question directory; the
// task is blocked with the structured question and its options; vx decide
// answers by option number through the file, and nothing is typed into the
// pane. The soldier's prompt tells it to call the tool.
func TestPiAsk_BlockedThenAnsweredThroughTheFile(t *testing.T) {
	project, home, rec, task, client := dispatchBlockedOnAsk(t, true)

	wantArgs := "--approve\n--tui-mode\nregular\n" + askExtensionArgs(t, project, home, task.ID)
	if got := recorded(t, rec, "args", 1); !strings.HasPrefix(got, wantArgs) {
		t.Errorf("pi was started with:\n%s\nwant it to start with:\n%s", got, wantArgs)
	}
	argv := strings.Split(recorded(t, rec, "args", 1), "\n")
	extPath := argv[slicesIndex(argv, "-e")+1]
	if strings.HasPrefix(extPath, task.CampPath) || strings.HasPrefix(extPath, project) {
		t.Errorf("the extension %s lives inside the camp or the project", extPath)
	}
	if _, err := os.Stat(extPath); err != nil {
		t.Errorf("the extension was not written: %v", err)
	}
	if len(client.promptCalls) != 1 || !strings.Contains(client.promptCalls[0], "call your ask_user tool") || !strings.Contains(client.promptCalls[0], "needs-decision:") {
		t.Errorf("the soldier's prompt: %q", client.promptCalls)
	}

	if task.Status != state.StatusBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	d := task.Decision
	if d == nil || d.Kind != state.DecisionKindAsk || d.QuestionID != askQuestionID || d.Question != "Which database?" || len(d.Options) != 2 {
		t.Fatalf("decision = %+v", d)
	}

	var status bytes.Buffer
	if code := runStatus(project, home, true, &status, &status); code != 0 || !strings.Contains(status.String(), `"status": "blocked"`) || !strings.Contains(status.String(), `"question_id": "`+askQuestionID+`"`) {
		t.Errorf("status --json (exit %d) does not show the blocked ask:\n%s", code, status.String())
	}

	// herdr stops reporting it blocked once the extension has the answer.
	client.promptStatus = "working"
	client.waitStatus = "done"
	var out bytes.Buffer
	if code := runDecide(project, home, task.ID, "2", client, &out, &out); code != 0 {
		t.Fatalf("decide: exit %d: %s", code, out.String())
	}

	received := readFileT(t, filepath.Join(rec, "answer.received"))
	for _, want := range []string{`"id": "` + askQuestionID + `"`, `"answer": "SQLite"`, `"option": 2`} {
		if !strings.Contains(received, want) {
			t.Errorf("the extension received %s, missing %s", received, want)
		}
	}
	if len(client.keys) != 0 || len(client.promptCalls) != 1 {
		t.Errorf("something was typed into the pane: keys %v, prompts %q", client.keys, client.promptCalls)
	}
	after := dispatchedTask(t, project, home)
	if after.Status != state.StatusDone || after.Decision == nil || after.Decision.Answer != "2" || after.Decision.AnsweredAt.IsZero() {
		t.Errorf("after the answer: status %s, decision %+v", after.Status, after.Decision)
	}
	if len(after.Amendments) != 1 || !strings.Contains(after.Amendments[0].Text, `Asked "Which database?", the general answered: SQLite`) {
		t.Errorf("amendments = %+v", after.Amendments)
	}
}

// Free text is delivered as written, with no option number.
func TestPiAsk_FreeTextAnswer(t *testing.T) {
	project, home, rec, task, client := dispatchBlockedOnAsk(t, true)
	client.promptStatus = "working"
	client.waitStatus = "done"

	var out bytes.Buffer
	if code := runDecide(project, home, task.ID, "Use DuckDB, it is only a script", client, &out, &out); code != 0 {
		t.Fatalf("decide: exit %d: %s", code, out.String())
	}
	received := readFileT(t, filepath.Join(rec, "answer.received"))
	if !strings.Contains(received, `"answer": "Use DuckDB, it is only a script"`) || strings.Contains(received, `"option"`) {
		t.Errorf("the extension received %s", received)
	}
}

// The question was already closed in the pane (the general answered there):
// nothing is delivered, and the error points at --dismiss.
func TestPiAsk_QuestionNoLongerOpen(t *testing.T) {
	project, home, rec, task, client := dispatchBlockedOnAsk(t, true)
	root, _ := vxproject.Root(home, project)
	if err := os.Remove(filepath.Join(piask.Dir(root, task.ID), "question.json")); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runDecide(project, home, task.ID, "2", client, &out, &errOut); code == 0 {
		t.Fatalf("decide succeeded on a closed question: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "no longer open") || !strings.Contains(errOut.String(), "--dismiss") {
		t.Errorf("error = %q", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(rec, "answer.received")); err == nil {
		t.Error("an answer was delivered to a closed question")
	}
	if after := dispatchedTask(t, project, home); after.Status != state.StatusBlocked || !after.Decision.AnsweredAt.IsZero() {
		t.Errorf("task changed: status %s, decision %+v", after.Status, after.Decision)
	}
}

// A session that never takes the answer: it is withdrawn after the timeout,
// so it cannot answer late, and the task is left blocked and unanswered.
func TestPiAsk_AnswerNotPickedUp(t *testing.T) {
	old := piask.ConsumeTimeout
	piask.ConsumeTimeout = 400 * time.Millisecond
	t.Cleanup(func() { piask.ConsumeTimeout = old })

	project, home, _, task, client := dispatchBlockedOnAsk(t, false)
	var out, errOut bytes.Buffer
	if code := runDecide(project, home, task.ID, "1", client, &out, &errOut); code == 0 {
		t.Fatalf("decide succeeded though nothing took the answer: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "did not pick the answer up") {
		t.Errorf("error = %q", errOut.String())
	}
	root, _ := vxproject.Root(home, project)
	if _, err := os.Stat(filepath.Join(piask.Dir(root, task.ID), "answer.json")); err == nil {
		t.Error("the unclaimed answer was left in place")
	}
	if after := dispatchedTask(t, project, home); after.Status != state.StatusBlocked || !after.Decision.AnsweredAt.IsZero() {
		t.Errorf("task changed: status %s, decision %+v", after.Status, after.Decision)
	}
}

// Without the extension loaded (a pi that does not know the flag, or one
// that never asked) nothing is recorded, so a blocked task keeps the prose
// decision and the free-text path: see
// TestPiLifecycle_BlockedQuestionIsAnsweredAsFreeText.
func TestPiAsk_NoQuestionFileFallsBackToProse(t *testing.T) {
	fakePi(t)
	project, home := piDispatchProject(t)
	client := newPiHerdr()
	client.promptStatus = "blocked"
	client.readOutput = "Which database should this use?\n"

	var out bytes.Buffer
	if code := runDispatch(project, home, "w1", "pick a database", state.KindMission, "", "", "pi", client, &out, &out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	task := dispatchedTask(t, project, home)
	if task.Decision == nil || task.Decision.Kind != state.DecisionKindProse {
		t.Errorf("decision = %+v, want a prose one", task.Decision)
	}
}

func slicesIndex(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
