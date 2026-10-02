package forum_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

const relayNotice = "Received. Forwarded to the commander, who will answer here."

// relayedSession leaves a session under project whose user sent one prompt that
// the listener (a relay poll) delivered, acknowledged and relayed, with the
// listener now polling again: the exact state in which the old overlay logic
// went back to idle.
func relayedSession(t *testing.T, h *forum.Hub, file, project string) (key string, stop func()) {
	t.Helper()
	open, err := h.OpenFor(file, false, project)
	if err != nil {
		t.Fatal(err)
	}
	keepBrowser(t, h, open.Key)
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "change the title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(open.Key, false); err != nil {
		t.Fatal(err)
	}
	res, err := h.PollAllWith(context.Background(), time.Second, forum.PollAllOptions{Relay: true})
	if err != nil || res.Status != forum.PollFeedback || res.ProjectRoot != project {
		t.Fatalf("relay poll = %+v %v, want the feedback with the project root", res, err)
	}
	ack(t, h, open.Key, res)
	if r, err := h.Relay(open.Key, forum.CommanderConnected, relayNotice); err != nil || !r.Active || !r.Posted {
		t.Fatalf("Relay = %+v %v", r, err)
	}
	// The listener re-polls at once.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.PollAllWith(ctx, time.Hour, forum.PollAllOptions{Relay: true})
	}()
	time.Sleep(30 * time.Millisecond)
	return open.Key, func() { cancel(); <-done }
}

func TestHub_RelayedStateOutlivesTheListenerRepoll(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	snap := state(t, h, key)
	if snap.AwaitingSince == nil {
		t.Fatal("the overlay must persist while the listener re-polls")
	}
	if snap.Relayed == nil || snap.Relayed.Commander != forum.CommanderConnected || snap.Relayed.Round != 1 {
		t.Fatalf("relayed = %+v, want round 1 with a commander", snap.Relayed)
	}
	if snap.Listener != "relayed" || snap.Listening {
		t.Errorf("listener = %q listening = %v, want relayed and not 'agent listening'", snap.Listener, snap.Listening)
	}
	if snap.AnsweredThrough != 0 {
		t.Errorf("answered_through = %d: the received notice must not answer the round", snap.AnsweredThrough)
	}
	var notices int
	for _, m := range snap.Transcript {
		if m.Kind == forum.MessageKindNotice {
			notices++
			if m.Text != relayNotice || m.Role != forum.RoleAgent {
				t.Errorf("notice = %+v", m)
			}
		}
	}
	if notices != 1 {
		t.Errorf("%d notices, want 1", notices)
	}
}

func TestHub_RelayNoticeOncePerRoundAndCommanderRefresh(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	// Redelivery after a listener crash relays the same round again.
	if r, err := h.Relay(key, forum.CommanderConnected, relayNotice); err != nil || r.Posted || !r.Active {
		t.Fatalf("second Relay = %+v %v, want active and no second notice", r, err)
	}
	// The commander session goes away: only the status changes.
	if r, err := h.Relay(key, forum.CommanderNone, relayNotice); err != nil || r.Posted || !r.Active {
		t.Fatalf("refresh Relay = %+v %v", r, err)
	}
	snap := state(t, h, key)
	if snap.Relayed == nil || snap.Relayed.Commander != forum.CommanderNone {
		t.Fatalf("relayed = %+v, want commander none", snap.Relayed)
	}
	count := 0
	for _, m := range snap.Transcript {
		if m.Kind == forum.MessageKindNotice {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d notices after redelivery, want 1", count)
	}
}

func TestHub_RelayedEndsOnCommanderReply(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	if err := h.Reply(key, "done, title changed"); err != nil {
		t.Fatal(err)
	}
	snap := state(t, h, key)
	if snap.AwaitingSince != nil || snap.Relayed != nil || snap.AnsweredThrough != 1 {
		t.Fatalf("after a real reply: awaiting %v relayed %v answered %d", snap.AwaitingSince, snap.Relayed, snap.AnsweredThrough)
	}
	if r, err := h.Relay(key, forum.CommanderConnected, relayNotice); err != nil || r.Active {
		t.Errorf("Relay of an answered round = %+v %v, want inactive", r, err)
	}
	if snap.Listener != "forwarding" {
		t.Errorf("listener = %q, want forwarding while only the listener polls", snap.Listener)
	}
}

func TestHub_RelayedEndsOnArtifactChange(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	if state(t, h, key).Relayed == nil {
		t.Fatal("not relayed before the edit")
	}
	time.Sleep(10 * time.Millisecond)
	writeFile(t, file, "<p>a changed</p>")
	if err := os.Chtimes(file, time.Now().Add(time.Minute), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	snap := state(t, h, key)
	if snap.Relayed != nil || snap.AwaitingSince != nil {
		t.Fatalf("an artifact change answers the round: relayed %v awaiting %v", snap.Relayed, snap.AwaitingSince)
	}
}

func TestHub_RelayedEndsOnStopWaiting(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	if ok, err := h.StopWaiting(key); err != nil || !ok {
		t.Fatalf("StopWaiting = %v %v", ok, err)
	}
	snap := state(t, h, key)
	if snap.Relayed != nil || snap.AwaitingSince != nil {
		t.Fatalf("after Stop waiting: relayed %v awaiting %v", snap.Relayed, snap.AwaitingSince)
	}
}

func TestHub_AgentPollEndsRelayedWait(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	key, stop := relayedSession(t, h, file, "/p/root")
	defer stop()

	// The commander polls by hand with nothing pending: it is attending.
	_, _ = h.Poll(context.Background(), key, 20*time.Millisecond)
	if snap := state(t, h, key); snap.Relayed != nil || snap.AwaitingSince != nil {
		t.Fatalf("a manual poll keeps its old meaning: relayed %v awaiting %v", snap.Relayed, snap.AwaitingSince)
	}
}

func TestHub_RelayPollLeavesProjectlessSessionsUndelivered(t *testing.T) {
	h := newHub(t, t.TempDir(), time.Hour)
	file := filepath.Join(t.TempDir(), "loose.html")
	writeFile(t, file, "<p>a</p>")
	open := openSession(t, h, file) // no project root
	keepBrowser(t, h, open.Key)
	if _, err := h.QueuePrompt(open.Key, forum.PromptInput{Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(open.Key, false); err != nil {
		t.Fatal(err)
	}

	res, err := h.PollAllWith(context.Background(), time.Second, forum.PollAllOptions{Relay: true})
	if err != nil || res.Status != forum.PollNoSessions {
		t.Fatalf("relay poll = %+v %v, want no_sessions: nothing it may deliver", res, err)
	}
	if snap := state(t, h, open.Key); snap.Pending != 1 || snap.Listener == "forwarding" || snap.Listening {
		t.Errorf("pending %d listener %q: the session must stay undelivered and not look covered", snap.Pending, snap.Listener)
	}
	// A manual poll still gets it.
	manual, err := h.Poll(context.Background(), open.Key, time.Second)
	if err != nil || manual.Status != forum.PollFeedback || len(manual.Prompts) != 1 {
		t.Fatalf("manual poll = %+v %v", manual, err)
	}
}

func TestHub_OpenRecordsAndUpdatesTheProjectRoot(t *testing.T) {
	home := t.TempDir()
	h := newHub(t, home, time.Hour)
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>a</p>")
	if res, _ := h.OpenFor(file, false, "/p/one"); res.ProjectRoot != "/p/one" {
		t.Fatalf("project root = %q", res.ProjectRoot)
	}
	// A plain open (no root) keeps what was recorded; a new root replaces it.
	if res, _ := h.OpenFor(file, false, ""); res.ProjectRoot != "/p/one" {
		t.Errorf("project root = %q after an open without one", res.ProjectRoot)
	}
	if res, _ := h.OpenFor(file, false, "/p/two"); res.ProjectRoot != "/p/two" {
		t.Errorf("project root = %q, want the latest opener's", res.ProjectRoot)
	}
	// It survives a restart.
	h2 := newHub(t, home, time.Hour)
	if res, _ := h2.OpenFor(file, false, ""); res.ProjectRoot != "/p/two" {
		t.Errorf("project root = %q after a restart", res.ProjectRoot)
	}
}
