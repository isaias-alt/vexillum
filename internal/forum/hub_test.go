package forum_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func newHub(t *testing.T, home string, grace time.Duration) *forum.Hub {
	t.Helper()
	return forum.NewHub(home, forum.HubOptions{BrowserGrace: grace})
}

func openSession(t *testing.T, h *forum.Hub, file string) forum.OpenResult {
	t.Helper()
	res, err := h.Open(file, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return res
}

// keepBrowser holds a browser state request open so the session counts as
// connected for the duration of the test.
func keepBrowser(t *testing.T, h *forum.Hub, key string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Mirror the real browser: re-issue the long-poll with the latest
	// version every time it returns.
	go func() {
		var since int64
		for ctx.Err() == nil {
			snap, err := h.State(ctx, key, since, time.Hour)
			if err != nil {
				return
			}
			since = snap.Version
		}
	}()
	time.Sleep(20 * time.Millisecond)
}

func pollAsync(h *forum.Hub, key string, timeout time.Duration) <-chan forum.PollResult {
	out := make(chan forum.PollResult, 1)
	go func() {
		res, err := h.Poll(context.Background(), key, timeout)
		if err != nil {
			res.Status = "error: " + err.Error()
		}
		out <- res
	}()
	return out
}

func waitResult(t *testing.T, ch <-chan forum.PollResult) forum.PollResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(3 * time.Second):
		t.Fatal("poll did not return")
		return forum.PollResult{}
	}
}

// ack confirms res's delivery the way the CLI does once it has written the
// poll output.
func ack(t *testing.T, h *forum.Hub, key string, res forum.PollResult) {
	t.Helper()
	if err := h.Ack(key, res.Delivery); err != nil {
		t.Fatalf("Ack: %v", err)
	}
}

func TestHub_QueueSendPoll_DeliversOnceAndConsumes(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	file := filepath.Join(t.TempDir(), "a.html")
	open := openSession(t, h, file)
	keepBrowser(t, h, open.Key)

	p, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "use B", Tag: "choice", Selector: "#b", Text: "Option B"})
	if err != nil {
		t.Fatalf("QueuePrompt: %v", err)
	}
	if p.UID == "" {
		t.Error("prompt has no uid")
	}

	poll := pollAsync(h, open.Key, 0)
	select {
	case <-poll:
		t.Fatal("poll returned before the prompt was sent")
	case <-time.After(60 * time.Millisecond):
	}
	if n, err := h.Send(open.Key, false); err != nil || n != 1 {
		t.Fatalf("Send = %d, %v", n, err)
	}
	res := waitResult(t, poll)
	if res.Status != forum.PollFeedback || len(res.Prompts) != 1 || res.Prompts[0].Prompt != "use B" {
		t.Fatalf("poll result = %+v", res)
	}
	if res.Prompts[0].Tag != "choice" || res.Prompts[0].Selector != "#b" || res.Prompts[0].Text != "Option B" {
		t.Errorf("prompt fields lost: %+v", res.Prompts[0])
	}

	// Consumed once the agent confirmed it: a second poll must not see it again.
	ack(t, h, open.Key, res)
	again, err := h.Poll(context.Background(), open.Key, 80*time.Millisecond)
	if err != nil {
		t.Fatalf("second Poll: %v", err)
	}
	if again.Status != forum.PollTimeout || len(again.Prompts) != 0 {
		t.Errorf("second poll = %+v, want an empty timeout", again)
	}
}

func TestHub_QueueKeyReplacesUnsentPromptInPlace(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))

	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "first", QueueKey: "q1"})
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "other"})
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "second", QueueKey: "q1"})

	snap, err := h.State(context.Background(), open.Key, 0, 0)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(snap.Queued) != 2 || snap.Queued[0].Prompt != "second" || snap.Queued[1].Prompt != "other" {
		t.Errorf("queue = %+v, want [second other]", snap.Queued)
	}
}

func TestHub_RemoveQueued(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	p, _ := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "x"})
	if err := h.RemoveQueued(open.Key, p.UID); err != nil {
		t.Fatalf("RemoveQueued: %v", err)
	}
	if err := h.RemoveQueued(open.Key, p.UID); !errors.Is(err, forum.ErrNoPrompt) {
		t.Errorf("second remove = %v, want ErrNoPrompt", err)
	}
	if _, err := h.Send(open.Key, false); !errors.Is(err, forum.ErrNothingToSend) {
		t.Errorf("Send of an empty queue = %v, want ErrNothingToSend", err)
	}
}

func TestHub_RejectsEmptyPromptAndSanitizesTag(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "   "}); err == nil {
		t.Error("expected an error for a blank prompt")
	}
	p, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "x", Tag: "bad tag\nwith newline"})
	if err != nil {
		t.Fatalf("QueuePrompt: %v", err)
	}
	if p.Tag != "feedback" {
		t.Errorf("tag = %q, want the default for an unusable tag", p.Tag)
	}
}

func TestHub_SendAndEnd_DeliversFinalFeedbackOnceThenEnded(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "last word"})
	if _, err := h.Send(open.Key, true); err != nil {
		t.Fatalf("Send(end): %v", err)
	}

	res, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if res.Status != forum.PollEnded || res.EndedBy != forum.EndedByUser || len(res.Prompts) != 1 {
		t.Fatalf("first poll = %+v, want ended with the final prompt", res)
	}
	ack(t, h, open.Key, res)
	res, _ = h.Poll(context.Background(), open.Key, time.Second)
	if res.Status != forum.PollEnded || len(res.Prompts) != 0 {
		t.Errorf("second poll = %+v, want ended with nothing", res)
	}
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "late"}); err == nil {
		t.Error("queueing into an ended session should fail")
	} else {
		var ended *forum.ErrSessionEnded
		if !errors.As(err, &ended) {
			t.Errorf("error = %v, want *ErrSessionEnded", err)
		}
	}
}

func TestHub_PollReportsBrowserDisconnectedAfterGrace(t *testing.T) {
	h := newHub(t, t.TempDir(), 80*time.Millisecond)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))

	start := time.Now()
	res, err := h.Poll(context.Background(), open.Key, 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if res.Status != forum.PollBrowserDisconnected {
		t.Fatalf("status = %q, want browser_disconnected", res.Status)
	}
	if time.Since(start) < 70*time.Millisecond {
		t.Errorf("returned after %v, before the grace period", time.Since(start))
	}
	// The session stays resumable.
	if res2 := openSession(t, h, filepath.Join(t.TempDir(), "other.html")); res2.Status != forum.StatusOpen {
		t.Errorf("unrelated open status = %q", res2.Status)
	}
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "still works"}); err != nil {
		t.Errorf("session not resumable after browser_disconnected: %v", err)
	}
}

func TestHub_ConnectedBrowserSuppressesDisconnect(t *testing.T) {
	h := newHub(t, t.TempDir(), 60*time.Millisecond)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, open.Key)

	res, err := h.Poll(context.Background(), open.Key, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if res.Status != forum.PollTimeout {
		t.Errorf("status = %q, want timeout (the browser is connected)", res.Status)
	}
}

func TestHub_ListeningTracksActivePoll(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	keepBrowser(t, h, open.Key)

	snap, _ := h.State(context.Background(), open.Key, 0, 0)
	if snap.Listening {
		t.Error("listening before any poll")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = h.Poll(ctx, open.Key, 0); close(done) }()
	time.Sleep(40 * time.Millisecond)
	snap, _ = h.State(context.Background(), open.Key, 0, 0)
	if !snap.Listening {
		t.Error("not listening during an active poll")
	}
	cancel()
	<-done
	snap, _ = h.State(context.Background(), open.Key, 0, 0)
	if snap.Listening {
		t.Error("still listening after the poll ended")
	}
}

func TestHub_CanceledPollConsumesNothing(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "keep me"})
	h.Send(open.Key, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Poll(ctx, open.Key, 0); err == nil {
		t.Fatal("expected a canceled poll to fail")
	}
	res, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil || len(res.Prompts) != 1 {
		t.Fatalf("prompt lost by a canceled poll: %+v, %v", res, err)
	}
}

func TestHub_ReplyAppearsInTranscriptAndRefusesWhenEnded(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	if err := h.Reply(open.Key, "**done** with step 1"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if err := h.Reply(open.Key, "  "); err == nil {
		t.Error("expected an error for an empty reply")
	}
	snap, _ := h.State(context.Background(), open.Key, 0, 0)
	if len(snap.Transcript) != 1 || snap.Transcript[0].Role != forum.RoleAgent || snap.Transcript[0].Text != "**done** with step 1" {
		t.Errorf("transcript = %+v", snap.Transcript)
	}
	h.End(open.Key, forum.EndedByAgent)
	if err := h.Reply(open.Key, "late"); err == nil {
		t.Error("reply to an ended session should fail")
	}
}

func TestHub_UserEndedSessionNeedsExplicitReopen(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	file := filepath.Join(t.TempDir(), "a.html")
	open := openSession(t, h, file)
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "bye"})
	h.Send(open.Key, true)

	res, err := h.Open(file, false)
	if err != nil || res.Status != forum.OpenUserEnded {
		t.Fatalf("plain reopen = %+v, %v; want user_ended", res, err)
	}
	res, err = h.Open(file, true)
	if err != nil || res.Status != forum.StatusOpen {
		t.Fatalf("--reopen = %+v, %v; want open", res, err)
	}
	if res.Pending != 1 {
		t.Errorf("pending = %d, want the undelivered final feedback kept", res.Pending)
	}
}

func TestHub_AgentEndedSessionReopensPlainly(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	file := filepath.Join(t.TempDir(), "a.html")
	open := openSession(t, h, file)
	h.End(open.Key, forum.EndedByAgent)
	res, err := h.Open(file, false)
	if err != nil || res.Status != forum.StatusOpen {
		t.Fatalf("reopen after agent end = %+v, %v", res, err)
	}
}

// The core restart-proof guarantee: queue, outbox, transcript and status
// all come back from disk in a brand new Hub.
func TestHub_PersistsAcrossRestart(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")

	h1 := newHub(t, home, time.Minute)
	open := openSession(t, h1, file)
	h1.QueuePrompt(open.Key, forum.PromptInput{Prompt: "sent before crash"})
	h1.Send(open.Key, false)
	h1.QueuePrompt(open.Key, forum.PromptInput{Prompt: "drafted before crash", Tag: "note"})
	h1.Reply(open.Key, "agent said hi")
	token1, _ := h1.Token(open.Key)

	h2 := newHub(t, home, time.Minute)
	reopened := openSession(t, h2, file)
	if reopened.Key != open.Key || reopened.Created {
		t.Fatalf("restart open = %+v, want the same session resumed", reopened)
	}
	if reopened.Pending != 1 {
		t.Errorf("pending = %d, want 1", reopened.Pending)
	}
	if token2, _ := h2.Token(open.Key); token2 != token1 {
		t.Error("session token changed across a restart")
	}
	snap, _ := h2.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 1 || snap.Queued[0].Prompt != "drafted before crash" || snap.Queued[0].Tag != "note" {
		t.Errorf("queued after restart = %+v", snap.Queued)
	}
	if len(snap.Transcript) != 2 {
		t.Errorf("transcript after restart = %+v, want the sent prompt and the reply", snap.Transcript)
	}
	res, err := h2.Poll(context.Background(), open.Key, time.Second)
	if err != nil || len(res.Prompts) != 1 || res.Prompts[0].Prompt != "sent before crash" {
		t.Fatalf("poll after restart = %+v, %v", res, err)
	}
}

func TestHub_StateLongPollWakesOnChange(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	first, _ := h.State(context.Background(), open.Key, 0, 0)

	got := make(chan forum.Snapshot, 1)
	go func() {
		snap, _ := h.State(context.Background(), open.Key, first.Version, 5*time.Second)
		got <- snap
	}()
	time.Sleep(40 * time.Millisecond)
	h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "ping"})
	select {
	case snap := <-got:
		if len(snap.Queued) != 1 || snap.Version == first.Version {
			t.Errorf("snapshot = %+v", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("state long-poll did not wake on a queue change")
	}
}

func TestHub_UnknownSession(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	if _, err := h.Poll(context.Background(), "0123456789abcdef", time.Millisecond); !errors.Is(err, forum.ErrNoSession) {
		t.Errorf("Poll = %v, want ErrNoSession", err)
	}
	if _, err := h.Token("not-a-key"); err == nil {
		t.Error("expected an error for a malformed key")
	}
}

func TestHub_QueueBounded(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Minute)
	open := openSession(t, h, filepath.Join(t.TempDir(), "a.html"))
	var last error
	for i := 0; i < 250; i++ {
		_, last = h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "x"})
		if last != nil {
			break
		}
	}
	if !errors.Is(last, forum.ErrQueueFull) {
		t.Errorf("error after 250 prompts = %v, want ErrQueueFull", last)
	}
}
