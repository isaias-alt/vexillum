package forumlisten_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
	"github.com/isaias-alt/vexillum/internal/forumlisten"
	"github.com/isaias-alt/vexillum/internal/inbox"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

type env struct {
	t      *testing.T
	home   string
	root   string // the project root the sessions belong to
	hub    *forum.Hub
	client *forum.Client
	logbuf *syncBuffer
	commOK atomic.Bool
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newEnv runs a forum server in-process on an ephemeral loopback port with an
// isolated home: nothing here touches ~/.vexillum or a real server.
func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	hub := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Hour})
	srv := forum.NewServer(forum.ServerOptions{Hub: hub, Store: forum.NewStore(home), AgentToken: "tok"})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	e := &env{t: t, home: home, root: t.TempDir(), hub: hub, logbuf: &syncBuffer{}}
	e.client = forum.NewClient(strings.TrimPrefix(ts.URL, "http://"), "tok")
	e.commOK.Store(true)
	return e
}

// session opens a session for a new artifact under project (empty for none),
// holds a browser tab on it, and returns its key and file.
func (e *env) session(project string) (key, file string) {
	e.t.Helper()
	file = filepath.Join(e.t.TempDir(), "plan.html")
	if err := os.WriteFile(file, []byte("<p>plan</p>"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	open, err := e.hub.OpenFor(file, false, project)
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	go func() {
		var since int64
		for ctx.Err() == nil {
			snap, err := e.hub.State(ctx, open.Key, since, time.Hour)
			if err != nil {
				return
			}
			since = snap.Version
		}
	}()
	time.Sleep(20 * time.Millisecond)
	return open.Key, file
}

func (e *env) send(key, text string, end bool) {
	e.t.Helper()
	if text != "" {
		if _, err := e.hub.QueuePrompt(key, forum.PromptInput{Prompt: text, Tag: "feedback", Selector: "#hero", Text: "Hero"}); err != nil {
			e.t.Fatal(err)
		}
	}
	if _, err := e.hub.Send(key, end); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) options() forumlisten.Options {
	return forumlisten.Options{
		Home:            e.home,
		Connect:         func(context.Context) (*forum.Client, error) { return e.client, nil },
		PollTimeout:     40 * time.Millisecond,
		NoSessionsGrace: 60 * time.Millisecond,
		RetryDelay:      5 * time.Millisecond,
		Commander:       func(string) bool { return e.commOK.Load() },
		Log:             e.logbuf,
		RefreshEvery:    time.Millisecond,
	}
}

// run starts the listener and returns a channel with its result.
func (e *env) run(ctx context.Context, opts forumlisten.Options) <-chan error {
	done := make(chan error, 1)
	go func() { done <- forumlisten.Run(ctx, opts) }()
	return done
}

// runUntil runs the listener until cond holds, then stops it and waits for it
// to exit cleanly: an open session never ends by itself, so the listener keeps
// polling for as long as the test looks.
func (e *env) runUntil(what string, opts forumlisten.Options, cond func() bool) {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := e.run(ctx, opts)
	e.eventually(what, cond)
	cancel()
	if err := e.wait(done); err != nil {
		e.t.Fatalf("Run = %v after cancel, want nil", err)
	}
}

func (e *env) wait(done <-chan error) error {
	e.t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		e.t.Fatalf("the listener did not exit; log:\n%s", e.logbuf.String())
		return nil
	}
}

func (e *env) eventually(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s; log:\n%s", what, e.logbuf.String())
}

func (e *env) snap(key string) forum.Snapshot {
	e.t.Helper()
	s, err := e.hub.State(context.Background(), key, 0, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func pending(t *testing.T, root string) []inbox.Entry {
	t.Helper()
	es, err := inbox.Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestListener_StoresWakesRelaysAndAcks(t *testing.T) {
	e := newEnv(t)
	key, file := e.session(e.root)
	e.send(key, "move the title up", false)

	ctx, cancel := context.WithCancel(context.Background())
	done := e.run(ctx, e.options())
	e.eventually("the prompt in the inbox", func() bool { return len(pending(t, e.root)) == 1 })
	e.eventually("the relayed state", func() bool { return e.snap(key).Relayed != nil })

	es := pending(t, e.root)
	got := es[0]
	if got.Session != key || got.File != file || got.Prompt != "move the title up" || got.Tag != "feedback" ||
		got.Selector != "#hero" || got.Text != "Hero" || got.Ended || got.UID == "" {
		t.Errorf("entry = %+v, want every field of the prompt", got)
	}
	e.eventually("the wake", func() bool {
		ws, err := sentinel.DrainForum(e.root)
		if err != nil {
			t.Fatal(err)
		}
		return len(ws) == 1 && ws[0].Session == key && ws[0].Count == 1 && !ws[0].Ended
	})

	snap := e.snap(key)
	if snap.Relayed.Commander != forum.CommanderConnected || snap.AnsweredThrough != 0 || snap.AwaitingSince == nil {
		t.Errorf("snapshot = relayed %+v answered %d awaiting %v: the notice must not answer the round", snap.Relayed, snap.AnsweredThrough, snap.AwaitingSince)
	}
	if snap.Pending != 0 || snap.Listening {
		t.Errorf("pending %d listening %v: the delivery must be acknowledged and the listener is not the agent", snap.Pending, snap.Listening)
	}
	if !strings.Contains(e.logbuf.String(), "stored 1 prompt") {
		t.Errorf("log = %q", e.logbuf.String())
	}
	cancel()
	if err := e.wait(done); err != nil {
		t.Errorf("Run = %v after cancel, want nil", err)
	}
}

func TestListener_CrashBetweenDeliveryAndAckLosesNothing(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session(e.root)
	e.send(key, "first", false)

	// A listener that died right after the server handed the prompt over: it
	// leased it and never wrote or acknowledged anything.
	res, err := e.client.PollRelay(context.Background(), time.Second)
	if err != nil || res.Status != forum.PollFeedback || len(res.Prompts) != 1 {
		t.Fatalf("simulated delivery = %+v %v", res, err)
	}
	if len(pending(t, e.root)) != 0 {
		t.Fatal("nothing was stored yet")
	}

	e.runUntil("the redelivered prompt acknowledged", e.options(), func() bool {
		return len(pending(t, e.root)) == 1 && e.snap(key).Relayed != nil && e.snap(key).Pending == 0
	})
	if pending(t, e.root)[0].UID != res.Prompts[0].UID {
		t.Errorf("stored uid %q, want the redelivered %q", pending(t, e.root)[0].UID, res.Prompts[0].UID)
	}
	if snap := e.snap(key); snap.Pending != 0 {
		t.Errorf("pending = %d after the listener acknowledged", snap.Pending)
	}
}

func TestListener_CrashAfterStoringBeforeAckDoesNotDuplicate(t *testing.T) {
	e := newEnv(t)
	key, file := e.session(e.root)
	e.send(key, "only once", false)

	res, err := e.client.PollRelay(context.Background(), time.Second)
	if err != nil || len(res.Prompts) != 1 {
		t.Fatalf("simulated delivery = %+v %v", res, err)
	}
	// It wrote the inbox file and rang the wake, then died before the ack.
	p := res.Prompts[0]
	if _, err := inbox.Write(e.root, inbox.Entry{UID: p.UID, Session: key, File: file, Tag: p.Tag, Prompt: p.Prompt}); err != nil {
		t.Fatal(err)
	}
	if err := sentinel.RecordForumWake(e.root, sentinel.ForumWake{Session: key, File: file, Count: 1}); err != nil {
		t.Fatal(err)
	}

	e.runUntil("the redelivery acknowledged", e.options(), func() bool { return e.snap(key).Pending == 0 && e.snap(key).Relayed != nil })
	if es := pending(t, e.root); len(es) != 1 {
		t.Fatalf("%d entries, want exactly 1 after the redelivery", len(es))
	}
	if ws, _ := sentinel.DrainForum(e.root); len(ws) != 1 || ws[0].Count != 1 {
		t.Errorf("wakes = %+v, want the one coalesced wake", ws)
	}
}

func TestListener_ConfirmedPromptIsNotResurrectedByRedelivery(t *testing.T) {
	e := newEnv(t)
	key, file := e.session(e.root)
	e.send(key, "handled", false)

	res, err := e.client.PollRelay(context.Background(), time.Second)
	if err != nil || len(res.Prompts) != 1 {
		t.Fatal(res, err)
	}
	p := res.Prompts[0]
	// The commander already read and confirmed it (stored earlier, ack lost).
	if _, err := inbox.Write(e.root, inbox.Entry{UID: p.UID, Session: key, File: file, Prompt: p.Prompt}); err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.MarkRead(e.root, []string{p.UID}); err != nil {
		t.Fatal(err)
	}

	e.runUntil("the redelivery acknowledged", e.options(), func() bool { return e.snap(key).Pending == 0 })
	if es := pending(t, e.root); len(es) != 0 {
		t.Errorf("a confirmed prompt came back: %+v", es)
	}
	if ws, _ := sentinel.DrainForum(e.root); len(ws) != 0 {
		t.Errorf("no wake for a message that was already handled: %+v", ws)
	}
}

func TestListener_SeveralMessagesCoalesceIntoOneWake(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session(e.root)
	e.send(key, "one", false)
	e.send(key, "two", false)
	e.send(key, "three", false)

	e.runUntil("all three stored", e.options(), func() bool { return len(pending(t, e.root)) == 3 && e.snap(key).Pending == 0 })
	if len(pending(t, e.root)) != 3 {
		t.Fatalf("entries = %+v, want 3", pending(t, e.root))
	}
	ws, _ := sentinel.DrainForum(e.root)
	if len(ws) != 1 || ws[0].Count != 3 {
		t.Errorf("wakes = %+v, want one wake for 3 messages", ws)
	}
	notices := 0
	for _, m := range e.snap(key).Transcript {
		if m.Kind == forum.MessageKindNotice {
			notices++
		}
	}
	if notices > 3 || notices < 1 {
		t.Errorf("%d notices", notices)
	}
}

func TestListener_SessionEndedWithFinalPrompts(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session(e.root)
	e.send(key, "ship it with these changes", true) // Send & End

	done := e.run(context.Background(), e.options())
	// After the final prompts there is nothing left: it exits on its own.
	if err := e.wait(done); err != nil {
		t.Fatal(err)
	}
	es := pending(t, e.root)
	if len(es) != 1 || !es[0].Ended || es[0].EndedBy != forum.EndedByUser {
		t.Fatalf("entries = %+v, want the final prompt marked ended by the user", es)
	}
	ws, _ := sentinel.DrainForum(e.root)
	if len(ws) != 1 || !ws[0].Ended || ws[0].Count != 1 {
		t.Errorf("wakes = %+v, want an ended wake", ws)
	}
	snap := e.snap(key)
	if snap.Status != forum.StatusEnded || snap.Relayed != nil {
		t.Errorf("status %q relayed %v: an ended session has nothing to relay", snap.Status, snap.Relayed)
	}
	for _, m := range snap.Transcript {
		if m.Kind == forum.MessageKindNotice {
			t.Errorf("a notice was posted to an ended session: %+v", m)
		}
	}
	if snap.Pending != 0 {
		t.Errorf("pending = %d, the final delivery must be acknowledged", snap.Pending)
	}
}

func TestListener_ProjectlessSessionStaysUndelivered(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session("") // opened outside any project
	e.send(key, "anyone there?", false)

	done := e.run(context.Background(), e.options())
	if err := e.wait(done); err != nil {
		t.Fatalf("Run = %v, want a clean exit: nothing it may cover", err)
	}
	if snap := e.snap(key); snap.Pending != 1 {
		t.Fatalf("pending = %d, want the prompt left for a manual poll", snap.Pending)
	}
	manual, err := e.hub.Poll(context.Background(), key, time.Second)
	if err != nil || manual.Status != forum.PollFeedback || len(manual.Prompts) != 1 {
		t.Fatalf("manual poll = %+v %v", manual, err)
	}
}

func TestListener_ExitsWhenNoSessionsRemainAfterTheGrace(t *testing.T) {
	e := newEnv(t)
	start := time.Now()
	done := e.run(context.Background(), e.options())
	if err := e.wait(done); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Errorf("exited after %v, before the grace", time.Since(start))
	}
}

func TestListener_ASessionOpenedDuringTheGraceIsStillCovered(t *testing.T) {
	e := newEnv(t)
	opts := e.options()
	opts.NoSessionsGrace = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := e.run(ctx, opts)
	time.Sleep(150 * time.Millisecond) // the listener has seen no_sessions
	key, _ := e.session(e.root)
	e.send(key, "late", false)
	e.eventually("the late prompt", func() bool { return len(pending(t, e.root)) == 1 })
	cancel()
	if err := e.wait(done); err != nil {
		t.Fatal(err)
	}
}

func TestListener_RefreshesTheCommanderStatusOfAnOpenRound(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session(e.root)
	e.commOK.Store(false)
	e.send(key, "hello?", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := e.run(ctx, e.options())
	e.eventually("relayed with no commander", func() bool {
		r := e.snap(key).Relayed
		return r != nil && r.Commander == forum.CommanderNone
	})
	e.commOK.Store(true) // a commander session starts
	e.eventually("the refreshed commander status", func() bool {
		r := e.snap(key).Relayed
		return r != nil && r.Commander == forum.CommanderConnected
	})
	notices := 0
	for _, m := range e.snap(key).Transcript {
		if m.Kind == forum.MessageKindNotice {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("%d notices after a refresh, want 1", notices)
	}
	cancel()
	_ = e.wait(done)
}

func TestListener_CommanderReplyEndsTheRoundAndListenerKeepsForwarding(t *testing.T) {
	e := newEnv(t)
	key, _ := e.session(e.root)
	e.send(key, "round one", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := e.run(ctx, e.options())
	e.eventually("round one relayed", func() bool { return e.snap(key).Relayed != nil })

	if err := e.hub.Reply(key, "done"); err != nil {
		t.Fatal(err)
	}
	if s := e.snap(key); s.Relayed != nil || s.AwaitingSince != nil {
		t.Fatalf("after the real reply: relayed %v awaiting %v", s.Relayed, s.AwaitingSince)
	}
	e.send(key, "round two", false)
	e.eventually("round two stored", func() bool { return len(pending(t, e.root)) == 2 })
	e.eventually("round two relayed", func() bool {
		s := e.snap(key)
		return s.Relayed != nil && s.Relayed.Round == 2
	})
	cancel()
	_ = e.wait(done)
}

func TestListener_FailsWhenTheServerIsGone(t *testing.T) {
	e := newEnv(t)
	opts := e.options()
	opts.MaxFailures = 2
	dead := forum.NewClient("127.0.0.1:1", "tok")
	opts.Connect = func(context.Context) (*forum.Client, error) { return dead, nil }
	done := e.run(context.Background(), opts)
	if err := e.wait(done); err == nil {
		t.Fatal("Run succeeded against a server that is gone")
	}
}
