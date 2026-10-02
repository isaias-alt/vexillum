package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func TestParseForumOpenArgs(t *testing.T) {
	a, err := parseForumOpenArgs([]string{"--port", "9000", "artifact.html", "--no-open", "--reopen"})
	if err != nil {
		t.Fatalf("parseForumOpenArgs: %v", err)
	}
	if a.file != "artifact.html" || a.port != 9000 || !a.noOpen || !a.reopen {
		t.Errorf("got %+v", a)
	}
	for _, bad := range [][]string{{"--no-open"}, {"a.html", "b.html"}, {"--port"}, {"--port", "0", "a.html"}, {"--port", "70000", "a.html"}, {"--nope", "a.html"}} {
		if _, err := parseForumOpenArgs(bad); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

func TestParseForumPollArgs(t *testing.T) {
	a, err := parseForumPollArgs([]string{"a.html", "--reply", "done", "--timeout", "30s"})
	if err != nil {
		t.Fatalf("parseForumPollArgs: %v", err)
	}
	if a.file != "a.html" || a.reply != "done" || !a.hasReply || a.timeout != 30*time.Second {
		t.Errorf("got %+v", a)
	}
	a, err = parseForumPollArgs([]string{"--reply-file", "-", "a.html"})
	if err != nil || a.replyFile != "-" || !a.hasReply {
		t.Errorf("--reply-file - parsed as %+v, %v", a, err)
	}
	for _, bad := range [][]string{{"a.html", "--reply"}, {"--reply", "x", "--reply-file", "y", "a.html"}, {"a.html", "--timeout", "soon"}, {"a.html", "--timeout", "-1s"}, {}} {
		if _, err := parseForumPollArgs(bad); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

func TestParseForumPollArgs_All(t *testing.T) {
	a, err := parseForumPollArgs([]string{"--all", "--timeout", "1m"})
	if err != nil || !a.all || a.file != "" || a.timeout != time.Minute {
		t.Errorf("--all parsed as %+v, %v", a, err)
	}
	a, err = parseForumPollArgs([]string{"--all", "--reply-to", "a.html", "--reply", "done"})
	if err != nil || a.replyTo != "a.html" || a.reply != "done" || !a.hasReply {
		t.Errorf("--all --reply-to parsed as %+v, %v", a, err)
	}
	for _, bad := range [][]string{
		{"--all", "a.html"},                // a file and --all
		{"--all", "--reply", "x"},          // a reply with no session to go to
		{"--all", "--reply-to", "a.html"},  // a destination with no reply
		{"a.html", "--reply-to", "b.html"}, // --reply-to only goes with --all
	} {
		if _, err := parseForumPollArgs(bad); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

// browserSend does what the reviewer's browser does: queue one prompt on the
// session and press Send to Agent. The server is the one the command layer
// started under home, found through its discovery file.
func browserSend(t *testing.T, home, file, text string) {
	t.Helper()
	key := forum.SessionKey(file)
	var state forum.ServerState
	readJSON(t, filepath.Join(home, "forum", "server.json"), &state)
	var rec struct {
		Token string `json:"token"`
	}
	readJSON(t, filepath.Join(home, "forums", key, "session.json"), &rec)
	post := func(path, body string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, "http://"+state.Addr+"/api/s/"+key+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forum-Token", rec.Token)
		req.Header.Set("Origin", "http://"+state.Addr)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s = %d", path, resp.StatusCode)
		}
	}
	quoted, _ := json.Marshal(text)
	post("/queue", `{"prompt":`+string(quoted)+`}`)
	post("/send", `{}`)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestForumPollAll_NoSessionsReportsNoSessions(t *testing.T) {
	inProcessSpawn(t)
	var out, errb bytes.Buffer
	code := runForumPoll(context.Background(), t.TempDir(), []string{"--all", "--timeout", "5s"}, strings.NewReader(""), &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "status: no_sessions") || strings.Contains(out.String(), "session:") {
		t.Errorf("exit %d, stderr %q, output:\n%s", code, errb.String(), out.String())
	}
}

// One listener for several sessions: poll --all delivers the feedback of
// whichever session has some, names its file, confirms the delivery so it is
// consumed, and a reply goes to the session --reply-to names. A poll that never
// confirmed (it died) gets redelivered, flagged.
func TestForumPollAll_DeliversNamesTheFileAndConfirms(t *testing.T) {
	inProcessSpawn(t)
	home := t.TempDir()
	a, b := writeArtifact(t), writeArtifact(t)
	var errb bytes.Buffer
	for _, f := range []string{a, b} {
		var out bytes.Buffer
		if code := runForumOpen(context.Background(), home, []string{f, "--no-open"}, &out, &errb); code != 0 {
			t.Fatalf("open exit %d: %s", code, errb.String())
		}
	}
	browserSend(t, home, b, "change b")

	var out bytes.Buffer
	if code := runForumPoll(context.Background(), home, []string{"--all", "--timeout", "5s"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("poll exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"session: " + forum.SessionKey(b), "file: " + b, "status: feedback", "prompt: change b", "poll --all --reply-to " + b} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "redelivered") {
		t.Errorf("a first delivery must not be flagged:\n%s", out.String())
	}

	// Confirmed by the command, so the same call finds nothing left. It also
	// answers b in the browser through --reply-to.
	out.Reset()
	code := runForumPoll(context.Background(), home, []string{"--all", "--reply-to", b, "--reply", "applied", "--timeout", "300ms"}, strings.NewReader(""), &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "status: timeout") || strings.Contains(out.String(), "change b") {
		t.Fatalf("second poll exit %d, stderr %q, output:\n%s", code, errb.String(), out.String())
	}
	transcript, err := os.ReadFile(filepath.Join(home, "forums", forum.SessionKey(b), "transcript.json"))
	if err != nil || !strings.Contains(string(transcript), "applied") {
		t.Errorf("the --reply-to answer is not in b's transcript: %v\n%s", err, transcript)
	}

	// A poll that died after the server handed the prompts out: no confirmation.
	browserSend(t, home, a, "change a")
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := client.PollAll(context.Background(), 5*time.Second); err != nil || len(res.Prompts) != 1 {
		t.Fatalf("unconfirmed poll = %+v, %v", res, err)
	}
	out.Reset()
	if code := runForumPoll(context.Background(), home, []string{"--all", "--timeout", "5s"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("poll exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"file: " + a, "redelivered: true", "prompt: change a", "skip any uid you already applied"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("redelivery output missing %q:\n%s", want, out.String())
		}
	}
}

// inProcessSpawn replaces the detached background server with forum.Run in
// a goroutine, so command tests exercise the real server without
// re-executing the test binary.
func inProcessSpawn(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	prev := forumSpawn
	prevListener := ensureForumListener
	// No detached listener from a test: the listener has its own tests, and
	// the command tests that need one run it in-process.
	ensureForumListener = func(string) error { return nil }
	forumSpawn = func(home string, port int) error {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = forum.Run(ctx, forum.RunOptions{Home: home, Port: port})
		}()
		return nil
	}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		forumSpawn = prev
		ensureForumListener = prevListener
	})
}

func writeArtifact(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "artifact.html")
	if err := os.WriteFile(file, []byte("<html><body><p>hi</p></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestForumOpen_PrintsSessionURLAndNextStepThenReturns(t *testing.T) {
	inProcessSpawn(t)
	home := t.TempDir()
	file := writeArtifact(t)
	var out, errb bytes.Buffer

	done := make(chan int, 1)
	go func() { done <- runForumOpen(context.Background(), home, []string{file, "--no-open"}, &out, &errb) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("forum <file> did not return (it must not block on the server)")
	}
	text := out.String()
	for _, want := range []string{"session: " + forum.SessionKey(file), "status: open", "url: http://127.0.0.1:", "/session/", "next_step:", "vx forum poll"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestForumOpen_MissingFile(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runForumOpen(context.Background(), t.TempDir(), []string{"/definitely/not/here.html", "--no-open"}, &out, &errb); code == 0 {
		t.Error("expected a non-zero exit for a missing file")
	}
}

func TestForumPoll_ReportsEndedSession(t *testing.T) {
	inProcessSpawn(t)
	home := t.TempDir()
	file := writeArtifact(t)
	var out, errb bytes.Buffer
	if code := runForumOpen(context.Background(), home, []string{file, "--no-open"}, &out, &errb); code != 0 {
		t.Fatalf("open exit %d: %s", code, errb.String())
	}
	// Feedback queued from a browser is covered by the forum package's
	// server tests; here the command layer is exercised end to end with the
	// agent-side lifecycle: end, then poll reports the ended session.
	var endOut bytes.Buffer
	if code := runForumEnd(context.Background(), home, []string{file}, &endOut, &errb); code != 0 {
		t.Fatalf("end exit %d: %s", code, errb.String())
	}
	var pollOut bytes.Buffer
	code := runForumPoll(context.Background(), home, []string{file, "--timeout", "5s"}, strings.NewReader(""), &pollOut, &errb)
	if code != 0 {
		t.Fatalf("poll exit %d: %s", code, errb.String())
	}
	if !strings.Contains(pollOut.String(), "status: ended") || !strings.Contains(pollOut.String(), "prompts[0]:") {
		t.Errorf("poll output:\n%s", pollOut.String())
	}
}

func TestForumPoll_NoSessionExplainsHowToOpen(t *testing.T) {
	inProcessSpawn(t)
	home := t.TempDir()
	file := writeArtifact(t)
	var out, errb bytes.Buffer
	code := runForumPoll(context.Background(), home, []string{file, "--timeout", "1s"}, strings.NewReader(""), &out, &errb)
	if code == 0 || !strings.Contains(errb.String(), "vx forum "+file) {
		t.Errorf("exit %d, stderr %q; want a failure that names the open command", code, errb.String())
	}
}

func TestForumStop_WhenNothingRunning(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runForumStop(context.Background(), t.TempDir(), &out, &errb); code != 0 {
		t.Errorf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "no forum server is running") {
		t.Errorf("output %q", out.String())
	}
}
