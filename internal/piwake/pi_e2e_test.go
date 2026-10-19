package piwake_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/inbox"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// TestExtensionInRealPi runs the extension the way it runs for the general: in
// the installed Pi, in a terminal, loaded from a project that "vx init
// --commander pi" scaffolded, with the REAL "vx sentinel await" as its child and
// a scripted model (testdata/fakemodel.ts) standing in for a provider, so what
// the model is sent and when can be read back. Everything lives in a temporary
// HOME, Pi agent directory and project; nothing here touches the real ones.
//
// What it shows, in one session: Pi asks to trust the folder and the extension
// is not loaded until it is; the await it starts belongs to the Pi process; a
// wake reaches an idle model as a user message; the general's unsent draft is
// still in the editor afterwards and goes out as its own message; a wake that
// arrives mid-turn is held until the turn ends; quitting ends the await. A second
// session, in a soldier's camp, shows the extension doing nothing there.
//
// It needs pi and python3 (for the pseudo terminal) and takes about a minute, so
// it is skipped without either and under -short. What it cannot show is whether
// a real model acts on the wake text: see docs/verification/pi-commander-checklist.md.
func TestExtensionInRealPi(t *testing.T) {
	if testing.Short() {
		t.Skip("drives the real Pi for about a minute")
	}
	for _, tool := range []string{"pi", "python3", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, agent, proj := filepath.Join(root, "home"), filepath.Join(root, "agent"), filepath.Join(root, "proj")
	vexillumHome := filepath.Join(home, ".vexillum")
	for _, d := range []string{home, agent, proj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	vx := buildVX(t, filepath.Join(root, "bin"))
	t.Setenv("PI_CODING_AGENT_DIR", agent) // for doctorcheck.PiTrust below
	env := append(os.Environ(),
		"HOME="+home,
		"PI_CODING_AGENT_DIR="+agent,
		"PI_OFFLINE=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HERDR_WORKSPACE_ID=w-e2e",
		"FAKE_LOG="+filepath.Join(root, "model.jsonl"),
		"FAKE_SLOW_MS=9000",
		"TERM=xterm-256color",
		"PATH="+filepath.Dir(vx)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	// Whatever the machine's herdr says, this Pi is not in a pane of it.
	env = withoutEnv(env, "HERDR_ENV", "HERDR_PANE_ID", "HERDR_SOCKET_PATH")
	t.Cleanup(func() { stopSentinelAndCheckLeaks(t, vexillumHome, root) })

	run := func(dir string, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run(proj, "git", "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(proj, "README.md"), []byte("# e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(proj, vx, "init", "--yes", "--commander", "pi")
	run(proj, "git", "add", "-A")
	run(proj, "git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "init")
	projectRoot, err := project.Root(vexillumHome, proj)
	if err != nil {
		t.Fatal(err)
	}
	piArgs := []string{"pi", "--tui-mode", "regular", "-e", testdataPath(t, "fakemodel.ts"), "--model", "fakevx/scripted"}
	modelLog := filepath.Join(root, "model.jsonl")

	// Before any answer Pi would ask, and doctor says so.
	if r := doctorcheck.PiTrust(proj, home); !r.Warn || !strings.Contains(r.Detail, "Pi will ask") {
		t.Errorf("before the first run: %+v", r)
	}

	pi := startHost(t, proj, env, piArgs...)
	until(t, 60*time.Second, "Pi to ask whether to trust the folder", func() bool {
		return strings.Contains(pi.tail(), "Trust project folder")
	})
	time.Sleep(2 * time.Second)
	if entries, _ := os.ReadDir(filepath.Join(vexillumHome, "sentinel-awaiters")); len(entries) != 0 {
		t.Fatalf("the extension ran before the folder was trusted: %v", entries)
	}
	pi.send("\r") // the first choice is "Trust"
	var awaiter sentinel.AwaiterRecord
	until(t, 60*time.Second, "the extension to start the await once trusted", func() bool {
		awaiters := readAwaiters(t, vexillumHome)
		if len(awaiters) != 1 {
			return false
		}
		awaiter = awaiters[0]
		return true
	})
	if awaiter.OwnerPID != pi.pid() {
		t.Errorf("the await's owner is %d, want the Pi process %d", awaiter.OwnerPID, pi.pid())
	}
	if awaiter.ProjectRoot != projectRoot {
		t.Errorf("the await watches %s, want %s", awaiter.ProjectRoot, projectRoot)
	}
	// What Pi wrote is what doctor reads.
	if r := doctorcheck.PiTrust(proj, home); !r.OK || !strings.Contains(r.Detail, "trusted") {
		t.Errorf("after trusting: %+v", r)
	}
	time.Sleep(time.Second)

	// 1. A wake while Pi is idle reaches the model as a user message.
	wake(t, projectRoot, "sess-a", "plan-a.html")
	call := untilCall(t, modelLog, 1, "the idle wake")
	if got := call.Users[len(call.Users)-1]; !strings.HasPrefix(got, "Forum feedback is waiting for you:") || !strings.Contains(got, "plan-a.html") || !strings.Contains(got, "vx forum inbox") {
		t.Errorf("the model was sent %q", got)
	}
	untilEvent(t, modelLog, 1, "end")

	// 2. The general has typed something and not sent it: a wake does not
	// touch it, and it goes out as its own message afterwards.
	pi.send("DRAFT")
	time.Sleep(500 * time.Millisecond)
	wake(t, projectRoot, "sess-b", "plan-b.html")
	call = untilCall(t, modelLog, 2, "the wake with a draft in the editor")
	if got := call.Users[len(call.Users)-1]; !strings.Contains(got, "plan-b.html") || strings.Contains(got, "DRAFT") {
		t.Errorf("the wake was sent as %q", got)
	}
	untilEvent(t, modelLog, 2, "end")
	pi.send("\r")
	call = untilCall(t, modelLog, 3, "the draft being sent")
	if got := call.Users[len(call.Users)-1]; got != "DRAFT" {
		t.Errorf("the draft went out as %q, want DRAFT on its own", got)
	}
	untilEvent(t, modelLog, 3, "end")

	// 3. A wake in the middle of a turn waits for the turn to end.
	pi.send("SLOW\r")
	slow := untilCall(t, modelLog, 4, "the slow turn")
	wake(t, projectRoot, "sess-c", "plan-c.html")
	after := untilCall(t, modelLog, 5, "the wake after the slow turn")
	for _, u := range slow.Users {
		if strings.Contains(u, "plan-c.html") {
			t.Fatalf("the mid-turn wake was in the slow turn already: %q", u)
		}
	}
	if got := after.Users[len(after.Users)-1]; !strings.Contains(got, "plan-c.html") {
		t.Errorf("the model was sent %q", got)
	}
	if end := eventTime(t, modelLog, 4, "end"); after.T < end {
		t.Errorf("the wake started a call (%d) before the slow turn ended (%d)", after.T, end)
	}
	untilEvent(t, modelLog, 5, "end")

	// 4. Quitting ends the await with the session.
	pi.send("/quit\r")
	until(t, 30*time.Second, "Pi to quit", func() bool { return !pi.alive() })
	until(t, 30*time.Second, "the await to be gone", func() bool { return len(readAwaiters(t, vexillumHome)) == 0 })

	// 5. In a soldier's camp, with the same committed files and --approve as a
	// soldier is launched with, the extension does nothing: no await, and a
	// wake for the project is not delivered there.
	camp := filepath.Join(vexillumHome, "projects", "e2e-camp", "camps", "1", "proj")
	run(proj, "git", "worktree", "add", "-q", "-b", "camp-1", camp)
	campLog := filepath.Join(root, "camp.jsonl")
	campEnv := append(append([]string{}, env...), "FAKE_LOG="+campLog)
	soldier := startHost(t, camp, campEnv, append(append([]string{}, piArgs...), "--approve", "hello")...)
	untilCall(t, campLog, 1, "the camp session's first call")
	time.Sleep(7 * time.Second) // longer than the await's own poll
	wake(t, projectRoot, "sess-d", "plan-d.html")
	time.Sleep(7 * time.Second)
	if n := len(readAwaiters(t, vexillumHome)); n != 0 {
		t.Errorf("%d await(s) registered from a camp", n)
	}
	if calls := readLog(t, campLog); len(calls) != 2 { // start and end of the one call
		t.Errorf("the camp session was woken: %+v", calls)
	}
	if out := psLines(t, "sentinel await"); containsAny(out, root) {
		t.Errorf("an await is running from the camp session:\n%s", strings.Join(out, "\n"))
	}
	soldier.send("/quit\r")
	until(t, 30*time.Second, "the camp Pi to quit", func() bool { return !soldier.alive() })
}

// --- helpers -------------------------------------------------------------

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// buildVX builds this repository's vx into dir.
func buildVX(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "vx")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/vx")
	cmd.Dir = filepath.Join("..", "..")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func withoutEnv(env []string, names ...string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			drop = drop || k == n
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

func until(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", d, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func readAwaiters(t *testing.T, vexillumHome string) []sentinel.AwaiterRecord {
	t.Helper()
	dir := filepath.Join(vexillumHome, "sentinel-awaiters")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sentinel.AwaiterRecord
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r sentinel.AwaiterRecord
		if json.Unmarshal(data, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// wake leaves what the forum listener leaves when feedback arrives: an inbox
// entry and the doorbell for it.
func wake(t *testing.T, projectRoot, session, file string) {
	t.Helper()
	if _, err := inbox.Write(projectRoot, inbox.Entry{UID: "u-" + session, Session: session, File: file, Tag: "t", Prompt: "please look"}); err != nil {
		t.Fatal(err)
	}
	if err := sentinel.RecordForumWake(projectRoot, sentinel.ForumWake{Session: session, File: file, Count: 1}); err != nil {
		t.Fatal(err)
	}
}

type modelEntry struct {
	Call  int      `json:"call"`
	Event string   `json:"event"`
	Users []string `json:"users"`
	T     int64    `json:"t"`
}

func readLog(t *testing.T, path string) []modelEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []modelEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e modelEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func untilCall(t *testing.T, path string, n int, what string) modelEntry {
	t.Helper()
	var got modelEntry
	until(t, 60*time.Second, what, func() bool {
		for _, e := range readLog(t, path) {
			if e.Call == n && e.Event == "start" {
				got = e
				return true
			}
		}
		return false
	})
	return got
}

func untilEvent(t *testing.T, path string, n int, event string) {
	t.Helper()
	until(t, 60*time.Second, fmt.Sprintf("model call %d to %s", n, event), func() bool {
		for _, e := range readLog(t, path) {
			if e.Call == n && e.Event == event {
				return true
			}
		}
		return false
	})
}

func eventTime(t *testing.T, path string, n int, event string) int64 {
	t.Helper()
	for _, e := range readLog(t, path) {
		if e.Call == n && e.Event == event {
			return e.T
		}
	}
	t.Fatalf("no %s event for call %d", event, n)
	return 0
}

// host is a command running in a pseudo terminal (testdata/ptyhost.py).
type host struct {
	t   *testing.T
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	mu  sync.Mutex
}

func startHost(t *testing.T, cwd string, env []string, argv ...string) *host {
	t.Helper()
	cmd := exec.Command("python3", append([]string{"-I", testdataPath(t, "ptyhost.py"), cwd}, argv...)...)
	cmd.Env = env
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &host{t: t, cmd: cmd, in: in, out: bufio.NewReader(out)}
	t.Cleanup(func() {
		in.Close() // the host kills the command when stdin closes
		_ = cmd.Wait()
	})
	return h
}

func (h *host) call(req map[string]any) map[string]any {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	b, _ := json.Marshal(req)
	if _, err := h.in.Write(append(b, '\n')); err != nil {
		h.t.Fatalf("talking to the pty host: %v", err)
	}
	line, err := h.out.ReadBytes('\n')
	if err != nil {
		h.t.Fatalf("reading from the pty host: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		h.t.Fatalf("pty host said %q: %v", line, err)
	}
	return resp
}

func (h *host) send(s string) { h.call(map[string]any{"op": "send", "data": s}) }
func (h *host) tail() string {
	return h.call(map[string]any{"op": "tail", "n": 20000})["text"].(string)
}
func (h *host) alive() bool { return h.call(map[string]any{"op": "alive"})["alive"].(bool) }
func (h *host) pid() int    { return int(h.call(map[string]any{"op": "pid"})["pid"].(float64)) }

// psLines lists the user's processes whose command line contains substr.
func psLines(t *testing.T, substr string) []string {
	t.Helper()
	out, err := exec.Command("ps", "-x", "-o", "pid=,command=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, substr) {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return lines
}

func containsAny(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// stopSentinelAndCheckLeaks stops the sentinel the await auto-started in the
// temporary HOME (checking it is this test's binary before it signals it), then
// fails the test if any process that came from the temporary directory is left.
func stopSentinelAndCheckLeaks(t *testing.T, vexillumHome, root string) {
	t.Helper()
	if info, ok := sentinel.LiveInfo(vexillumHome); ok {
		if cmd := psLines(t, fmt.Sprintf("%d ", info.PID)); containsAny(cmd, root) {
			_ = syscall.Kill(info.PID, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	var left []string
	for time.Now().Before(deadline) {
		left = psLines(t, root)
		if len(left) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("processes left over from the test:\n%s", strings.Join(left, "\n"))
}
