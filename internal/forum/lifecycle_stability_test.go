package forum_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// freePort returns a loopback port nobody holds right now.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func lastPort(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "forum", "last-port"))
	if err != nil {
		t.Fatalf("last-port file: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// ---------------------------------------------------------------- the port

func TestRun_ReusesTheLastPortAcrossARestart(t *testing.T) {
	home := t.TempDir()
	first, stop := startServer(t, home, forum.RunOptions{Port: freePort(t)})
	if got := lastPort(t, home); got != strconv.Itoa(portOf(t, first.Addr)) {
		t.Fatalf("last-port = %s, want the bound port of %s", got, first.Addr)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	// No explicit port this time: the recorded one must come back.
	second, _ := startServer(t, home, forum.RunOptions{})
	if second.Addr != first.Addr {
		t.Errorf("restart bound %s, want the previous %s", second.Addr, first.Addr)
	}
}

func TestRun_FallsBackToAFreePortAndRecordsIt(t *testing.T) {
	home := t.TempDir()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	takenPort := taken.Addr().(*net.TCPAddr).Port
	if err := os.MkdirAll(filepath.Join(home, "forum"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "forum", "last-port"), strconv.Itoa(takenPort))

	st, _ := startServer(t, home, forum.RunOptions{PortRetry: 100 * time.Millisecond})
	if got := portOf(t, st.Addr); got == takenPort {
		t.Fatalf("bound the port another process holds (%d)", got)
	}
	if got := lastPort(t, home); got != strconv.Itoa(portOf(t, st.Addr)) {
		t.Errorf("last-port = %s, want the fallback %s recorded", got, st.Addr)
	}
}

// The usual reason the last port is busy is the previous server still
// releasing it; the next one waits it out instead of moving the tabs' address.
func TestRun_WaitsOutABusyLastPortBeforeFallingBack(t *testing.T) {
	home := t.TempDir()
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := holder.Addr().(*net.TCPAddr).Port
	if err := os.MkdirAll(filepath.Join(home, "forum"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "forum", "last-port"), strconv.Itoa(port))
	go func() {
		time.Sleep(300 * time.Millisecond)
		holder.Close()
	}()
	st, _ := startServer(t, home, forum.RunOptions{PortRetry: 5 * time.Second})
	if got := portOf(t, st.Addr); got != port {
		t.Errorf("bound %d, want the last port %d once it freed up", got, port)
	}
}

func TestRun_AnExplicitBusyPortFails(t *testing.T) {
	home := t.TempDir()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	err = forum.Run(context.Background(), forum.RunOptions{Home: home, Port: port, PortRetry: -1})
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("Run on a busy explicit port = %v, want an error naming port %d", err, port)
	}
}

func TestRun_IgnoresACorruptLastPort(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "forum"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "forum", "last-port"), "not a port")
	st, _ := startServer(t, home, forum.RunOptions{})
	if got := lastPort(t, home); got != strconv.Itoa(portOf(t, st.Addr)) {
		t.Errorf("last-port = %s, want it rewritten to %s", got, st.Addr)
	}
}

// ------------------------------------------------------------ status route

func TestStatus_ReportsBuildProtocolAndActivity(t *testing.T) {
	home := t.TempDir()
	startServer(t, home, forum.RunOptions{Build: "some-build"})
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	idle, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if idle.Build != "some-build" || idle.Protocol != forum.ProtocolVersion || idle.PID != os.Getpid() || idle.Busy() {
		t.Errorf("idle status = %+v", idle)
	}
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>x</p>")
	if _, err := client.Open(ctx, file, false); err != nil {
		t.Fatal(err)
	}
	busy, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if busy.Sessions != 1 || !busy.Busy() {
		t.Errorf("status with an open session = %+v", busy)
	}
	if err := client.End(ctx, file); err != nil {
		t.Fatal(err)
	}
	ended, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ended.Busy() {
		t.Errorf("an ended session with no tab still counts as busy: %+v", ended)
	}
}

func TestStopIfIdle_RefusesWhileBusy(t *testing.T) {
	home := t.TempDir()
	_, stop := startServer(t, home, forum.RunOptions{})
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>x</p>")
	if _, err := client.Open(ctx, file, false); err != nil {
		t.Fatal(err)
	}
	err = client.StopIfIdle(ctx)
	var ae *forum.APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict || ae.Code != "busy" {
		t.Fatalf("StopIfIdle on a busy server = %v, want 409 busy", err)
	}
	if _, err := forum.Discover(home); err != nil {
		t.Fatalf("a refused stop must leave the server running: %v", err)
	}
	if err := client.End(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err := client.StopIfIdle(ctx); err != nil {
		t.Fatalf("StopIfIdle on an idle server: %v", err)
	}
	if err := stop(); err != nil {
		t.Errorf("server did not exit after StopIfIdle: %v", err)
	}
}

// ------------------------------------------------- replacing (or not) a server

// ensureWith runs EnsureServer with a spawn that starts an in-process server,
// counting spawns. cancel + wait stop whatever it started.
type ensureHarness struct {
	t       *testing.T
	home    string
	mu      sync.Mutex
	spawned int
	runs    sync.WaitGroup
	cancel  context.CancelFunc
	ctx     context.Context
}

func newEnsureHarness(t *testing.T, home string) *ensureHarness {
	ctx, cancel := context.WithCancel(context.Background())
	h := &ensureHarness{t: t, home: home, ctx: ctx, cancel: cancel}
	t.Cleanup(func() { cancel(); h.runs.Wait() })
	return h
}

func (h *ensureHarness) spawn() error {
	h.mu.Lock()
	h.spawned++
	h.mu.Unlock()
	h.runs.Add(1)
	go func() {
		defer h.runs.Done()
		_ = forum.Run(h.ctx, forum.RunOptions{Home: h.home})
	}()
	return nil
}

func (h *ensureHarness) spawns() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.spawned
}

func (h *ensureHarness) ensure() (*forum.Client, error) {
	return forum.EnsureServer(h.ctx, h.home, h.spawn, 10*time.Second)
}

func runningBuild(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "forum", "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st forum.ServerState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st.Build
}

func openForumSession(t *testing.T, home, name string) (file, key string) {
	t.Helper()
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(t.TempDir(), name)
	writeFile(t, file, "<p>x</p>")
	res, err := client.Open(context.Background(), file, false)
	if err != nil {
		t.Fatal(err)
	}
	return file, res.Key
}

// A different build with active sessions and a compatible protocol is reused,
// not replaced.
func TestEnsureServer_ReusesABusyServerFromAnotherBuild(t *testing.T) {
	home := t.TempDir()
	old, _ := startServer(t, home, forum.RunOptions{Build: "an-older-build"})
	openForumSession(t, home, "a.html")

	h := newEnsureHarness(t, home)
	client, err := h.ensure()
	if err != nil || client == nil {
		t.Fatalf("EnsureServer = %v, want the running server reused", err)
	}
	if h.spawns() != 0 {
		t.Errorf("spawned %d servers, want none", h.spawns())
	}
	if got := runningBuild(t, home); got != "an-older-build" {
		t.Errorf("running build = %q, want the busy server left alone", got)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.PID != old.PID || status.Sessions != 1 {
		t.Errorf("reused client status = %+v, %v", status, err)
	}
}

// Same, with only a tab connected (the session itself ended).
func TestEnsureServer_ReusesAServerWithAConnectedTab(t *testing.T) {
	home := t.TempDir()
	startServer(t, home, forum.RunOptions{Build: "an-older-build"})
	file, key := openForumSession(t, home, "a.html")
	client, _ := forum.Discover(home)
	if err := client.End(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	closeTab := openTab(t, home, key)

	h := newEnsureHarness(t, home)
	if _, err := h.ensure(); err != nil {
		t.Fatalf("EnsureServer: %v", err)
	}
	if h.spawns() != 0 || runningBuild(t, home) != "an-older-build" {
		t.Fatalf("a server with a connected tab was replaced (spawns %d, build %s)", h.spawns(), runningBuild(t, home))
	}

	// Once the tab is gone and nothing else is open, the server is replaceable.
	closeTab()
	waitFor(t, "the tab to be seen as gone", func() bool {
		st, err := client.Status(context.Background())
		return err == nil && !st.Busy()
	})
	if _, err := h.ensure(); err != nil {
		t.Fatalf("EnsureServer after the tab closed: %v", err)
	}
	if got := runningBuild(t, home); got != forum.Build() {
		t.Errorf("running build = %q, want it replaced by this binary's %q", got, forum.Build())
	}
}

// An incompatible protocol with sessions in use is never replaced: it fails
// with a message that says how to proceed.
func TestEnsureServer_FailsClearlyOnABusyIncompatibleServer(t *testing.T) {
	home := t.TempDir()
	startServer(t, home, forum.RunOptions{Build: "a-future-build", Protocol: forum.ProtocolVersion + 1})
	openForumSession(t, home, "a.html")

	h := newEnsureHarness(t, home)
	_, err := h.ensure()
	var inc *forum.ErrIncompatibleServer
	if !errors.As(err, &inc) {
		t.Fatalf("EnsureServer = %v, want *ErrIncompatibleServer", err)
	}
	if inc.Protocol != forum.ProtocolVersion+1 || inc.Activity.Sessions != 1 {
		t.Errorf("error = %+v", inc)
	}
	for _, want := range []string{"vx forum stop", "1 active session", "protocol"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
	if h.spawns() != 0 || runningBuild(t, home) != "a-future-build" {
		t.Errorf("the busy server was disturbed (spawns %d, build %s)", h.spawns(), runningBuild(t, home))
	}
}

// With no session and no tab, even an incompatible server is replaced.
func TestEnsureServer_ReplacesAnIdleIncompatibleServer(t *testing.T) {
	home := t.TempDir()
	_, stopOld := startServer(t, home, forum.RunOptions{Build: "a-future-build", Protocol: forum.ProtocolVersion + 1})

	h := newEnsureHarness(t, home)
	if _, err := h.ensure(); err != nil {
		t.Fatalf("EnsureServer = %v", err)
	}
	if got := runningBuild(t, home); got != forum.Build() {
		t.Errorf("running build = %q, want %q", got, forum.Build())
	}
	if err := stopOld(); err != nil {
		t.Errorf("the replaced server did not exit cleanly: %v", err)
	}
}

// A session that was ended and has no tab does not hold a server hostage.
func TestEnsureServer_ReplacesAServerWhoseSessionsAreEnded(t *testing.T) {
	home := t.TempDir()
	startServer(t, home, forum.RunOptions{Build: "an-older-build"})
	file, _ := openForumSession(t, home, "a.html")
	client, _ := forum.Discover(home)
	if err := client.End(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	h := newEnsureHarness(t, home)
	if _, err := h.ensure(); err != nil {
		t.Fatal(err)
	}
	if got := runningBuild(t, home); got != forum.Build() {
		t.Errorf("running build = %q, want %q", got, forum.Build())
	}
}

// A server that predates the status route cannot say whether it is in use, so
// it is reused, never stopped.
func TestEnsureServer_ReusesAServerThatCannotReportActivity(t *testing.T) {
	home := t.TempDir()
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"app":"vexillum-forum","ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer legacy.Close()
	if err := os.MkdirAll(filepath.Join(home, "forum"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(forum.ServerState{
		PID: os.Getpid(), Addr: strings.TrimPrefix(legacy.URL, "http://"), AgentToken: "t", Build: "legacy-build",
	})
	writeFile(t, filepath.Join(home, "forum", "server.json"), string(state))

	h := newEnsureHarness(t, home)
	client, err := h.ensure()
	if err != nil || client == nil {
		t.Fatalf("EnsureServer = %v, want the legacy server reused", err)
	}
	if h.spawns() != 0 {
		t.Errorf("spawned %d servers over a legacy one that may be in use", h.spawns())
	}
}

// ------------------------------------------------------ tabs across restarts

// openTab connects an events stream the way a browser tab does, with the
// session token the page was served with, and returns a func closing it.
func openTab(t *testing.T, home, key string) (closeTab func()) {
	t.Helper()
	body, status, cancel := tabStream(t, home, key)
	if status != http.StatusOK {
		cancel()
		t.Fatalf("tab events stream = %d", status)
	}
	// Wait for the first snapshot so the server has counted the tab.
	line := make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(body.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				line <- struct{}{}
				return
			}
		}
		if err := sc.Err(); err != nil {
			t.Errorf("reading tab stream: %v", err)
		}
	}()
	select {
	case <-line:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("no snapshot on the tab stream")
	}
	t.Cleanup(cancel)
	return cancel
}

func sessionToken(t *testing.T, home, key string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "forums", key, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &rec); err != nil || rec.Token == "" {
		t.Fatalf("session token: %v", err)
	}
	return rec.Token
}

func serverAddr(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "forum", "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st forum.ServerState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st.Addr
}

func tabStream(t *testing.T, home, key string) (*http.Response, int, context.CancelFunc) {
	t.Helper()
	ctx, cancelReq := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+serverAddr(t, home)+"/api/s/"+key+"/events", nil)
	req.Header.Set("X-Forum-Token", sessionToken(t, home, key))
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancelReq()
		t.Fatalf("tab events: %v", err)
	}
	return resp, resp.StatusCode, func() { cancelReq(); resp.Body.Close() }
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A server restart is invisible to a tab that keeps reconnecting: same port
// (so the page's origin still works), same session token, and the queue it
// built before the restart is still there.
func TestRestart_TabReconnectsToTheSamePortWithItsSession(t *testing.T) {
	home := t.TempDir()
	first, stopFirst := startServer(t, home, forum.RunOptions{Port: freePort(t)})
	file, key := openForumSession(t, home, "a.html")
	token := sessionToken(t, home, key)

	queue, _ := http.NewRequest(http.MethodPost, "http://"+first.Addr+"/api/s/"+key+"/queue",
		strings.NewReader(`{"prompt":"keep this across the restart"}`))
	queue.Header.Set("Content-Type", "application/json")
	queue.Header.Set("X-Forum-Token", token)
	queue.Header.Set("Origin", "http://"+first.Addr)
	resp, err := http.DefaultClient.Do(queue)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("queue before the restart: %v %v", resp, err)
	}
	resp.Body.Close()

	client, _ := forum.Discover(home)
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stopFirst(); err != nil {
		t.Fatal(err)
	}

	second, _ := startServer(t, home, forum.RunOptions{})
	if second.Addr != first.Addr {
		t.Fatalf("restart bound %s, want the tab's address %s", second.Addr, first.Addr)
	}

	// The tab's next reconnect, with the token it already holds.
	stream, status, cancel := tabStream(t, home, key)
	defer cancel()
	if status != http.StatusOK {
		t.Fatalf("reconnect after the restart = %d, want 200 (token and session must survive)", status)
	}
	if sessionToken(t, home, key) != token {
		t.Error("the session token changed across the restart")
	}
	sc := bufio.NewScanner(stream.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var snap forum.Snapshot
	found := false
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			if err := json.Unmarshal([]byte(line[6:]), &snap); err != nil {
				t.Fatal(err)
			}
			found = true
			break
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("no snapshot after the reconnect")
	}
	if snap.File != file || len(snap.Queued) != 1 || snap.Queued[0].Prompt != "keep this across the restart" {
		t.Errorf("restored snapshot = file %q queued %+v", snap.File, snap.Queued)
	}
}
