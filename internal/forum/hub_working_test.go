package forum_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// deliveredSession leaves a session whose queued message a poll has just
// delivered, with no poll running: the gap in which the browser used to say
// "not listening" although the agent had the message and was working.
func deliveredSession(t *testing.T, home string, clock *fakeClock) (*forum.Hub, string) {
	t.Helper()
	h := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Hour, Now: clock.Now})
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, open.Key)
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "do it"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(open.Key, false); err != nil {
		t.Fatal(err)
	}
	res, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil || res.Status != forum.PollFeedback {
		t.Fatalf("poll = %+v %v, want the feedback", res, err)
	}
	ack(t, h, open.Key, res)
	return h, open.Key
}

func state(t *testing.T, h *forum.Hub, key string) forum.Snapshot {
	t.Helper()
	snap, err := h.State(context.Background(), key, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestHub_WorkingAfterDeliveryUntilTheWindowEnds(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	h, key := deliveredSession(t, t.TempDir(), clock)

	snap := state(t, h, key)
	if snap.Listening {
		t.Fatal("listening with no poll running")
	}
	if snap.WorkingUntil == nil || !snap.WorkingUntil.Equal(clock.now.Add(forum.AgentWorkingWindow)) {
		t.Fatalf("working_until = %v, want delivery + %v", snap.WorkingUntil, forum.AgentWorkingWindow)
	}
	clock.advance(forum.AgentWorkingWindow - time.Second)
	if state(t, h, key).WorkingUntil == nil {
		t.Error("working must last the whole window")
	}
	clock.advance(2 * time.Second)
	if state(t, h, key).WorkingUntil != nil {
		t.Error("past the window with no signal it is plain not-listening again")
	}
}

func TestHub_WorkingEndsWithAPollOrAReply(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	// A new poll: listening, and when it times out the agent is not "working"
	// anymore (it asked for more and none came), just not listening.
	h, key := deliveredSession(t, t.TempDir(), clock)
	// (The fake clock never reaches a poll timeout, so the poll is canceled.)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if snap := pollWhileListening(t, h, key, ctx); !snap.Listening || snap.WorkingUntil != nil {
		t.Errorf("during the next poll: %+v, want listening and not working", snap)
	}
	if snap := state(t, h, key); snap.Listening || snap.WorkingUntil != nil {
		t.Errorf("after the next poll ended: %+v, want idle", snap)
	}

	// A reply also settles it.
	h, key = deliveredSession(t, t.TempDir(), clock)
	if err := h.Reply(key, "on it"); err != nil {
		t.Fatal(err)
	}
	if state(t, h, key).WorkingUntil != nil {
		t.Error("a reply ends the working state")
	}
}

func TestHub_NeverDeliveredIsNotWorking(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	h := forum.NewHub(t.TempDir(), forum.HubOptions{BrowserGrace: time.Hour, Now: clock.Now})
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "x"})
	h.Send(open.Key, false) // sent but no poll took it: waiting, not working
	if state(t, h, open.Key).WorkingUntil != nil {
		t.Error("an undelivered message is not the agent working on it")
	}
}

func TestHub_WorkingSurvivesARestartAndEndsWithTheSession(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	home := t.TempDir()
	h, key := deliveredSession(t, home, clock)
	file := state(t, h, key).File

	clock.advance(5 * time.Minute)
	restarted := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Hour, Now: clock.Now})
	if _, err := restarted.Open(file, false); err != nil {
		t.Fatal(err)
	}
	snap := state(t, restarted, key)
	if snap.WorkingUntil == nil || !snap.WorkingUntil.After(clock.now) {
		t.Fatalf("working_until after a restart = %v, want still running", snap.WorkingUntil)
	}
	if err := restarted.End(key, forum.EndedByUser); err != nil {
		t.Fatal(err)
	}
	if state(t, restarted, key).WorkingUntil != nil {
		t.Error("an ended session is not working")
	}
}

// pollWhileListening runs a poll until ctx ends and returns the snapshot seen
// while it was running.
func pollWhileListening(t *testing.T, h *forum.Hub, key string, ctx context.Context) forum.Snapshot {
	t.Helper()
	done := make(chan struct{})
	go func() { _, _ = h.Poll(ctx, key, 0); close(done) }()
	time.Sleep(30 * time.Millisecond)
	snap := state(t, h, key)
	<-done
	return snap
}
