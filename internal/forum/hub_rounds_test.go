package forum_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// roundSession opens a session on a real artifact file and keeps a browser
// connected, so tests can drive sends, polls and replies.
func roundSession(t *testing.T, home string) (*forum.Hub, string, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>v1</p>")
	h := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Hour})
	open := openSession(t, h, file)
	keepBrowser(t, h, open.Key)
	return h, open.Key, file
}

func sendFeedback(t *testing.T, h *forum.Hub, key, text, queueKey string) {
	t.Helper()
	if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: text, QueueKey: queueKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(key, false); err != nil {
		t.Fatal(err)
	}
}

// Every Send with feedback starts a round, the sent messages carry it, and the
// agent's reply carries the round it answers.
func TestHub_RoundsCountSendsAndTagMessages(t *testing.T) {
	h, key, _ := roundSession(t, t.TempDir())
	if snap := state(t, h, key); snap.Round != 0 || snap.AwaitingSince != nil {
		t.Fatalf("fresh session: round %d awaiting %v", snap.Round, snap.AwaitingSince)
	}
	sendFeedback(t, h, key, "first", "question:q1")
	if err := h.Reply(key, "did it"); err != nil {
		t.Fatal(err)
	}
	sendFeedback(t, h, key, "second", "")

	snap := state(t, h, key)
	if snap.Round != 2 || snap.AnsweredThrough != 1 {
		t.Fatalf("round %d answered %d, want 2 and 1", snap.Round, snap.AnsweredThrough)
	}
	var got [][2]any
	for _, m := range snap.Transcript {
		got = append(got, [2]any{m.Role, m.Round})
	}
	want := [][2]any{{"user", 1}, {"agent", 1}, {"user", 2}}
	if len(got) != len(want) {
		t.Fatalf("transcript %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %v, want %v", i, got[i], want[i])
		}
	}
	if snap.Transcript[0].QueueKey != "question:q1" {
		t.Errorf("sent message lost its queue key: %+v", snap.Transcript[0])
	}
}

// The round counter survives a restart of the server.
func TestHub_RoundSurvivesRestartButAwaitingDoesNot(t *testing.T) {
	home := t.TempDir()
	h, key, file := roundSession(t, home)
	sendFeedback(t, h, key, "a", "")
	sendFeedback(t, h, key, "b", "")
	if state(t, h, key).AwaitingSince == nil {
		t.Fatal("not awaiting after a send")
	}
	h2 := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Hour})
	if _, err := h2.Open(file, false); err != nil {
		t.Fatal(err)
	}
	snap := state(t, h2, key)
	if snap.Round != 2 {
		t.Errorf("round after restart = %d, want 2", snap.Round)
	}
	if snap.AwaitingSince != nil {
		t.Error("a restarted server still blocks on an agent it knows nothing about")
	}
}

// The wait ends when the agent replies, when it polls again, when the artifact
// changes, when the user stops waiting, and when the session ends - and not
// when the poll that delivers the feedback starts.
func TestHub_AwaitingEndsOnEveryAnswer(t *testing.T) {
	awaiting := func(h *forum.Hub, key string) bool { return state(t, h, key).AwaitingSince != nil }

	t.Run("reply", func(t *testing.T) {
		h, key, _ := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		if !awaiting(h, key) {
			t.Fatal("not awaiting after Send")
		}
		if err := h.Reply(key, "ok"); err != nil {
			t.Fatal(err)
		}
		if awaiting(h, key) || state(t, h, key).AnsweredThrough != 1 {
			t.Fatal("a reply must end the wait and answer the round")
		}
	})
	t.Run("delivering poll keeps waiting, the next one ends it", func(t *testing.T) {
		h, key, _ := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		res, err := h.Poll(context.Background(), key, time.Second)
		if err != nil || res.Status != forum.PollFeedback {
			t.Fatalf("poll = %+v %v", res, err)
		}
		ack(t, h, key, res)
		if !awaiting(h, key) {
			t.Fatal("the poll that delivered the feedback must not end the wait")
		}
		if _, err := h.Poll(context.Background(), key, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if awaiting(h, key) {
			t.Fatal("the agent is polling again: the wait must end")
		}
		if state(t, h, key).AnsweredThrough != 0 {
			t.Error("resuming to poll is not an answer")
		}
	})
	t.Run("artifact change", func(t *testing.T) {
		h, key, file := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		if !awaiting(h, key) {
			t.Fatal("not awaiting")
		}
		writeFile(t, file, "<p>v2, a longer document</p>")
		snap := state(t, h, key)
		if snap.AwaitingSince != nil || snap.AnsweredThrough != 1 {
			t.Fatalf("a changed artifact answers the round: awaiting %v answered %d", snap.AwaitingSince, snap.AnsweredThrough)
		}
	})
	t.Run("artifact change after the agent resumed polling still answers", func(t *testing.T) {
		h, key, file := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		res, err := h.Poll(context.Background(), key, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ack(t, h, key, res)
		if _, err := h.Poll(context.Background(), key, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("<p>changed later</p>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := state(t, h, key).AnsweredThrough; got != 1 {
			t.Fatalf("answered_through = %d, want 1", got)
		}
	})
	t.Run("stop waiting", func(t *testing.T) {
		h, key, _ := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		stopped, err := h.StopWaiting(key)
		if err != nil || !stopped {
			t.Fatalf("StopWaiting = %v %v", stopped, err)
		}
		snap := state(t, h, key)
		if snap.AwaitingSince != nil || snap.WorkingUntil != nil || snap.AnsweredThrough != 0 {
			t.Fatalf("stopping gives the surface back and says nothing was answered: %+v", snap)
		}
		if stopped, _ := h.StopWaiting(key); stopped {
			t.Error("stopping twice reports a second stop")
		}
	})
	t.Run("session end", func(t *testing.T) {
		h, key, _ := roundSession(t, t.TempDir())
		sendFeedback(t, h, key, "x", "")
		if err := h.End(key, forum.EndedByAgent); err != nil {
			t.Fatal(err)
		}
		if awaiting(h, key) {
			t.Fatal("an ended session never blocks")
		}
	})
	t.Run("send and end", func(t *testing.T) {
		h, key, _ := roundSession(t, t.TempDir())
		if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: "last"}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Send(key, true); err != nil {
			t.Fatal(err)
		}
		if snap := state(t, h, key); snap.AwaitingSince != nil || snap.Round != 1 {
			t.Fatalf("send & end: awaiting %v round %d", snap.AwaitingSince, snap.Round)
		}
	})
}

// A poll already waiting when the user sends is the one that delivers: the wait
// starts at the send and is not cancelled by that poll.
func TestHub_SendToAnOpenPollKeepsAwaiting(t *testing.T) {
	h, key, _ := roundSession(t, t.TempDir())
	done := make(chan forum.PollResult, 1)
	go func() {
		res, _ := h.Poll(context.Background(), key, 5*time.Second)
		done <- res
	}()
	time.Sleep(50 * time.Millisecond)
	sendFeedback(t, h, key, "x", "")
	if res := <-done; res.Status != forum.PollFeedback {
		t.Fatalf("poll = %+v", res)
	}
	if state(t, h, key).AwaitingSince == nil {
		t.Fatal("the delivering poll ended the wait")
	}
}
