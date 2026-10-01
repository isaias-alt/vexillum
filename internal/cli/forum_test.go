package cli

import (
	"bytes"
	"context"
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

// inProcessSpawn replaces the detached background server with forum.Run in
// a goroutine, so command tests exercise the real server without
// re-executing the test binary.
func inProcessSpawn(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	prev := forumSpawn
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
	for _, want := range []string{"session: " + forum.SessionKey(file), "status: open", "url: http://127.0.0.1:", "/session/", "next_step:", "vexillum forum poll"} {
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
	if code == 0 || !strings.Contains(errb.String(), "vexillum forum "+file) {
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
