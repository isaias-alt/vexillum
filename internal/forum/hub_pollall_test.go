package forum_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// sendFrom queues one prompt on key and sends it.
func sendFrom(t *testing.T, h *forum.Hub, key, text string) {
	t.Helper()
	if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: text}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(key, false); err != nil {
		t.Fatal(err)
	}
}

// twoSessions opens two sessions with a browser each.
func twoSessions(t *testing.T, h *forum.Hub) (a, b forum.OpenResult) {
	t.Helper()
	a = openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	b = openSession(t, h, filepath.Join(t.TempDir(), "b.html"))
	keepBrowser(t, h, a.Key)
	keepBrowser(t, h, b.Key)
	return a, b
}

func pollAllAsync(h *forum.Hub, timeout time.Duration) <-chan forum.PollResult {
	out := make(chan forum.PollResult, 1)
	go func() {
		res, err := h.PollAll(context.Background(), timeout)
		if err != nil {
			res.Status = "error: " + err.Error()
		}
		out <- res
	}()
	return out
}

// A poll that dies after the server handed the prompts out, before the agent
// confirmed them, loses nothing: the next poll delivers them again, flagged,
// ahead of anything newer, and once confirmed they never come back.
func TestHub_UnconfirmedDeliveryIsRedeliveredNotLost(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, open.Key)
	sendFrom(t, h, open.Key, "one")

	first, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil || len(first.Prompts) != 1 || first.Prompts[0].Redelivered {
		t.Fatalf("first poll = %+v, %v", first, err)
	}
	// The poll process died here: no Ack. The user sends more meanwhile.
	sendFrom(t, h, open.Key, "two")

	second, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil || len(second.Prompts) != 2 {
		t.Fatalf("second poll = %+v, %v", second, err)
	}
	if second.Prompts[0].UID != first.Prompts[0].UID || !second.Prompts[0].Redelivered {
		t.Errorf("the unconfirmed prompt must come back first and flagged: %+v", second.Prompts[0])
	}
	if second.Prompts[1].Prompt != "two" || second.Prompts[1].Redelivered {
		t.Errorf("a new prompt must not be flagged: %+v", second.Prompts[1])
	}

	// The first lease is stale now: acknowledging it must not release the rest.
	ack(t, h, open.Key, first)
	third, _ := h.Poll(context.Background(), open.Key, time.Second)
	if len(third.Prompts) != 2 {
		t.Fatalf("a stale ack released a newer delivery: %+v", third.Prompts)
	}
	ack(t, h, open.Key, third)
	ack(t, h, open.Key, third) // twice is harmless
	last, _ := h.Poll(context.Background(), open.Key, 50*time.Millisecond)
	if last.Status != forum.PollTimeout || len(last.Prompts) != 0 {
		t.Errorf("after the ack the prompts must be gone, got %+v", last)
	}
}

func TestHub_UnconfirmedDeliverySurvivesARestart(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")
	h1 := newHub(t, home, time.Minute)
	open := openSession(t, h1, file)
	sendFrom(t, h1, open.Key, "survive")
	if res, err := h1.Poll(context.Background(), open.Key, time.Second); err != nil || len(res.Prompts) != 1 {
		t.Fatalf("poll = %+v, %v", res, err)
	}

	h2 := newHub(t, home, time.Minute)
	keepBrowser(t, h2, open.Key)
	res, err := h2.Poll(context.Background(), open.Key, time.Second)
	if err != nil || len(res.Prompts) != 1 || !res.Prompts[0].Redelivered || res.Prompts[0].Prompt != "survive" {
		t.Fatalf("after a restart = %+v, %v", res, err)
	}
}

func TestHub_PollAll_DeliversAnySessionWithItsFile(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	a, b := twoSessions(t, h)

	poll := pollAllAsync(h, 0)
	select {
	case res := <-poll:
		t.Fatalf("returned with nothing sent: %+v", res)
	case <-time.After(60 * time.Millisecond):
	}
	sendFrom(t, h, b.Key, "from b")
	res := waitResult(t, poll)
	if res.Status != forum.PollFeedback || res.Key != b.Key || res.File != b.File || len(res.Prompts) != 1 || res.Prompts[0].Prompt != "from b" {
		t.Fatalf("poll --all = %+v, want b's feedback with its file", res)
	}
	if res.Delivery == "" {
		t.Error("no delivery lease to acknowledge")
	}
	ack(t, h, b.Key, res)

	// Nothing is lost and nothing is double-delivered across several sessions.
	sendFrom(t, h, a.Key, "a first")
	time.Sleep(5 * time.Millisecond)
	sendFrom(t, h, b.Key, "b second")
	first, _ := h.PollAll(context.Background(), time.Second)
	if first.Key != a.Key || first.OtherPending != 1 {
		t.Fatalf("first = %+v, want a (waiting longest) with 1 other pending", first)
	}
	ack(t, h, first.Key, first)
	second, _ := h.PollAll(context.Background(), time.Second)
	if second.Key != b.Key || second.OtherPending != 0 || second.Prompts[0].Prompt != "b second" {
		t.Fatalf("second = %+v, want b", second)
	}
	ack(t, h, second.Key, second)
	empty, _ := h.PollAll(context.Background(), 50*time.Millisecond)
	if empty.Status != forum.PollTimeout {
		t.Errorf("third = %+v, want a timeout", empty)
	}
}

func TestHub_PollAll_CoversEverySessionAsListening(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	a, b := twoSessions(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = h.PollAll(ctx, 0); close(done) }()
	time.Sleep(60 * time.Millisecond)
	for _, key := range []string{a.Key, b.Key} {
		if snap := state(t, h, key); !snap.Listening || snap.Listener != "listening" {
			t.Errorf("session %s during poll --all: %+v", key, snap)
		}
	}
	cancel()
	<-done
	for _, key := range []string{a.Key, b.Key} {
		if snap := state(t, h, key); snap.Listening {
			t.Errorf("session %s still listening after the poll ended", key)
		}
	}
}

func TestHub_PollAll_NoSessionsAndEndedSessions(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	if res, err := h.PollAll(context.Background(), time.Second); err != nil || res.Status != forum.PollNoSessions {
		t.Fatalf("with nothing open = %+v, %v", res, err)
	}
	a, b := twoSessions(t, h)
	poll := pollAllAsync(h, 0)
	time.Sleep(40 * time.Millisecond)
	if err := h.End(a.Key, forum.EndedByAgent); err != nil {
		t.Fatal(err)
	}
	res := waitResult(t, poll)
	if res.Status != forum.PollEnded || res.Key != a.Key || res.EndedBy != forum.EndedByAgent {
		t.Fatalf("an ending session = %+v, want it reported as ended", res)
	}
	// Reported once: the next poll covers only the session still open.
	sendFrom(t, h, b.Key, "still here")
	next, _ := h.PollAll(context.Background(), time.Second)
	if next.Key != b.Key || next.Status != forum.PollFeedback {
		t.Fatalf("next = %+v, want b's feedback", next)
	}
}

func TestHub_PollAll_SendAndEndDeliversFinalFeedbackOnce(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	a, _ := twoSessions(t, h)
	if _, err := h.QueuePrompt(a.Key, forum.PromptInput{Prompt: "final"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(a.Key, true); err != nil {
		t.Fatal(err)
	}
	res, _ := h.PollAll(context.Background(), time.Second)
	if res.Status != forum.PollEnded || res.Key != a.Key || len(res.Prompts) != 1 {
		t.Fatalf("final feedback = %+v", res)
	}
	// Unconfirmed: an ended session still holding it stays covered.
	again, _ := h.PollAll(context.Background(), time.Second)
	if len(again.Prompts) != 1 || !again.Prompts[0].Redelivered {
		t.Fatalf("unconfirmed final feedback must be redelivered: %+v", again)
	}
	ack(t, h, a.Key, again)
	done, _ := h.PollAll(context.Background(), 50*time.Millisecond)
	if len(done.Prompts) != 0 || done.Key == a.Key {
		t.Errorf("confirmed final feedback came back: %+v", done)
	}
}

func TestHub_PollAll_SessionOpenedWhileWaitingJoins(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	a := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, a.Key)
	poll := pollAllAsync(h, 0)
	time.Sleep(40 * time.Millisecond)

	late := openSession(t, h, filepath.Join(t.TempDir(), "late.html"))
	keepBrowser(t, h, late.Key)
	time.Sleep(40 * time.Millisecond)
	if snap := state(t, h, late.Key); !snap.Listening {
		t.Error("a session opened mid-poll is not covered by it")
	}
	sendFrom(t, h, late.Key, "hello")
	if res := waitResult(t, poll); res.Key != late.Key || res.Status != forum.PollFeedback {
		t.Fatalf("poll = %+v, want the late session's feedback", res)
	}
}

func TestHub_PollAll_BrowserDisconnectedOnlyWhenEveryWindowIsGone(t *testing.T) {
	h := newHub(t, t.TempDir(), 120*time.Millisecond)
	a := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	openSession(t, h, filepath.Join(t.TempDir(), "b.html")) // no browser at all
	keepBrowser(t, h, a.Key)

	res, _ := h.PollAll(context.Background(), 400*time.Millisecond)
	if res.Status != forum.PollTimeout {
		t.Fatalf("one window is still open, so no disconnect: %+v", res)
	}
	// With a's browser gone too, nothing is left to review.
	h2 := newHub(t, t.TempDir(), 120*time.Millisecond)
	openSession(t, h2, filepath.Join(t.TempDir(), "c.html"))
	openSession(t, h2, filepath.Join(t.TempDir(), "d.html"))
	res, _ = h2.PollAll(context.Background(), 2*time.Second)
	if res.Status != forum.PollBrowserDisconnected || res.Key != "" {
		t.Fatalf("every window gone = %+v, want browser_disconnected", res)
	}
}

func TestHub_PollAll_AdoptsRecentOpenSessionsAfterARestart(t *testing.T) {
	home := t.TempDir()
	h1 := newHub(t, home, time.Minute)
	a := openSession(t, h1, filepath.Join(t.TempDir(), "a.html"))
	sendFrom(t, h1, a.Key, "waiting on disk")

	h2 := newHub(t, home, time.Minute) // nothing loaded yet
	res, err := h2.PollAll(context.Background(), time.Second)
	if err != nil || res.Key != a.Key || len(res.Prompts) != 1 {
		t.Fatalf("after a restart = %+v, %v", res, err)
	}
}

// The panel's indicator: a listener heartbeat with a grace window, so the
// warning does not flicker between a reply (or a poll ending) and the next poll.
func TestHub_ListenerStatesAndGraceWindow(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	h := forum.NewHub(t.TempDir(), forum.HubOptions{BrowserGrace: time.Hour, ListenerGrace: 10 * time.Second, Now: clock.Now})
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, open.Key)
	want := func(label, listener string, wantUntil bool) {
		t.Helper()
		snap := state(t, h, open.Key)
		if snap.Listener != listener || (snap.ListenerUntil != nil) != wantUntil {
			t.Errorf("%s: listener %q until %v, want %q (until set: %v)", label, snap.Listener, snap.ListenerUntil, listener, wantUntil)
		}
	}

	want("just opened: the agent is about to poll", "waiting", true)
	clock.advance(11 * time.Second)
	want("opened, never polled, grace over", "none", false)

	// An open poll is listening, however long it runs.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = h.Poll(ctx, open.Key, 0); close(done) }()
	time.Sleep(50 * time.Millisecond)
	clock.advance(time.Hour)
	want("poll open", "listening", false)
	cancel()
	<-done
	want("poll just ended", "waiting", true)
	clock.advance(9 * time.Second)
	want("inside the grace", "waiting", true)
	clock.advance(2 * time.Second)
	want("grace over", "none", false)

	// Delivered: working wins over waiting; a reply restarts the grace.
	sendFrom(t, h, open.Key, "do it")
	res, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ack(t, h, open.Key, res)
	want("delivered", "working", false)
	clock.advance(5 * time.Minute)
	if err := h.Reply(open.Key, "done"); err != nil {
		t.Fatal(err)
	}
	want("replied, next poll not started yet", "waiting", true)
	clock.advance(11 * time.Second)
	want("replied and then silence", "none", false)
}

func TestHub_RestartedServerGivesTheListenerAGrace(t *testing.T) {
	home := t.TempDir()
	h1 := newHub(t, home, time.Minute)
	open := openSession(t, h1, filepath.Join(t.TempDir(), "a.html"))
	h2 := newHub(t, home, time.Minute)
	if snap := state(t, h2, open.Key); snap.Listener != "waiting" {
		t.Errorf("after a server restart the poll is reconnecting: %q, want waiting", snap.Listener)
	}
}
