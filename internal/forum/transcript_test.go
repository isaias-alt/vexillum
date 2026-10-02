package forum_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

const transcriptCapBytes = 5 << 20

func transcriptOf(t *testing.T, h *forum.Hub, key string) []forum.Message {
	t.Helper()
	snap, err := h.State(context.Background(), key, 0, 0)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	return snap.Transcript
}

func transcriptFileSize(t *testing.T, home, key string) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(home, "forums", key, "transcript.json"))
	if err != nil {
		t.Fatalf("stat transcript: %v", err)
	}
	return info.Size()
}

func TestTranscript_ItemCapKeepsTheNewest(t *testing.T) {
	home := t.TempDir()
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, filepath.Join(t.TempDir(), "a.html")).Key
	for i := 0; i < 520; i++ {
		if err := h.Reply(key, fmt.Sprintf("reply %d", i)); err != nil {
			t.Fatalf("Reply %d: %v", i, err)
		}
	}
	got := transcriptOf(t, h, key)
	if len(got) != 500 {
		t.Fatalf("transcript has %d messages, want 500", len(got))
	}
	if got[0].Text != "reply 20" || got[len(got)-1].Text != "reply 519" {
		t.Errorf("kept %q .. %q, want the newest 500 (reply 20 .. reply 519)", got[0].Text, got[len(got)-1].Text)
	}
}

func TestTranscript_ByteCapBoundsTheFileOnDisk(t *testing.T) {
	home := t.TempDir()
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, filepath.Join(t.TempDir(), "a.html")).Key
	big := strings.Repeat("é", 60000) // 2 bytes per rune: ~120 KB of JSON per reply
	for i := 0; i < 50; i++ {
		if err := h.Reply(key, fmt.Sprintf("%03d %s", i, big)); err != nil {
			t.Fatalf("Reply %d: %v", i, err)
		}
	}
	got := transcriptOf(t, h, key)
	if len(got) >= 50 || len(got) < 10 {
		t.Fatalf("transcript has %d messages, want some evicted but many kept", len(got))
	}
	if !strings.HasPrefix(got[len(got)-1].Text, "049 ") {
		t.Errorf("newest message was evicted: %.8q", got[len(got)-1].Text)
	}
	if size := transcriptFileSize(t, home, key); size > transcriptCapBytes {
		t.Errorf("transcript.json is %d bytes, over the %d cap", size, transcriptCapBytes)
	}
}

// What the user sent and the agent has not polled yet lives in the outbox, not
// in the transcript, so evicting its transcript mirror cannot lose it - nor
// the images it carries.
func TestTranscript_EvictionNeverLosesWhatIsPendingDelivery(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, file).Key

	att, err := h.AddAttachment(key, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: "pending with an image", Attachments: []string{att.ID}}); err != nil {
		t.Fatal(err)
	}
	queuedAtt, err := h.AddAttachment(key, jpgBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(key, false); err != nil {
		t.Fatal(err)
	}
	// Still unsent in the queue, with its own image.
	if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: "drafted, not sent", Attachments: []string{queuedAtt.ID}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 520; i++ {
		if err := h.Reply(key, fmt.Sprintf("filler %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range transcriptOf(t, h, key) {
		if m.Role == forum.RoleUser {
			t.Fatal("the sent prompt's transcript mirror should have been evicted by now")
		}
	}

	// Even a restart in between changes nothing.
	h2 := newHub(t, home, time.Minute)
	if _, err := h2.Open(file, false); err != nil {
		t.Fatal(err)
	}
	res, err := h2.Poll(context.Background(), key, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != forum.PollFeedback || len(res.Prompts) != 1 || res.Prompts[0].Prompt != "pending with an image" {
		t.Fatalf("poll = %+v, want the pending prompt", res)
	}
	if p := h2.AttachmentPath(key, att.ID); p == "" {
		t.Error("the pending prompt's image was freed with its transcript mirror")
	}
	if p := h2.AttachmentPath(key, queuedAtt.ID); p == "" {
		t.Error("the queued prompt's image was freed")
	}
	snap, _ := h2.State(context.Background(), key, 0, 0)
	if len(snap.Queued) != 1 || snap.Queued[0].Prompt != "drafted, not sent" {
		t.Errorf("queued after eviction and restart = %+v", snap.Queued)
	}
}

// An image nothing references any more (its message left the transcript and
// was delivered) is freed, once it is old enough to be sure.
func TestTranscript_EvictedImagesAreFreedOnceDelivered(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	h := forum.NewHub(home, forum.HubOptions{BrowserGrace: time.Minute, Now: func() time.Time { return now }})
	key := openSession(t, h, filepath.Join(t.TempDir(), "a.html")).Key

	att, err := h.AddAttachment(key, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.QueuePrompt(key, forum.PromptInput{Prompt: "look", Attachments: []string{att.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(key, false); err != nil {
		t.Fatal(err)
	}
	res, err := h.Poll(context.Background(), key, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ack(t, h, key, res)
	if h.AttachmentPath(key, att.ID) == "" {
		t.Fatal("a delivered image still in the transcript must stay")
	}
	now = now.Add(2 * time.Hour)
	for i := 0; i < 510; i++ {
		if err := h.Reply(key, fmt.Sprintf("filler %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if p := h.AttachmentPath(key, att.ID); p != "" {
		t.Errorf("image of an evicted, delivered message is still on disk: %s", p)
	}
}

func TestTranscript_BoundedStateSurvivesARestart(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, file).Key
	big := strings.Repeat("x", 60000)
	for i := 0; i < 90; i++ {
		if err := h.Reply(key, fmt.Sprintf("%03d %s", i, big)); err != nil {
			t.Fatal(err)
		}
	}
	before := transcriptOf(t, h, key)

	h2 := newHub(t, home, time.Minute)
	if _, err := h2.Open(file, false); err != nil {
		t.Fatal(err)
	}
	after := transcriptOf(t, h2, key)
	if len(after) != len(before) || after[0].ID != before[0].ID || after[len(after)-1].ID != before[len(before)-1].ID {
		t.Fatalf("restart changed the transcript: %d -> %d messages", len(before), len(after))
	}
	// Writing after the restart keeps the bound.
	if err := h2.Reply(key, "after restart "+big); err != nil {
		t.Fatal(err)
	}
	if size := transcriptFileSize(t, home, key); size > transcriptCapBytes {
		t.Errorf("transcript.json is %d bytes after more writes, over the cap", size)
	}
	last := transcriptOf(t, h2, key)
	if !strings.HasPrefix(last[len(last)-1].Text, "after restart") {
		t.Error("the newest message is missing after the restart")
	}
}

// A transcript left on disk by a looser cap is trimmed when the session loads.
func TestTranscript_OversizedFileIsTrimmedOnLoad(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, file).Key

	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 700; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"m_%04d","role":"agent","text":"old %d","at":"2026-01-01T00:00:00Z"}`, i, i)
	}
	b.WriteString("]")
	if err := os.WriteFile(filepath.Join(home, "forums", key, "transcript.json"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := newHub(t, home, time.Minute)
	if _, err := h2.Open(file, false); err != nil {
		t.Fatal(err)
	}
	got := transcriptOf(t, h2, key)
	if len(got) != 500 || got[len(got)-1].ID != "m_0699" {
		t.Errorf("loaded %d messages ending at %q, want the newest 500", len(got), got[len(got)-1].ID)
	}
}

// Trimming on load must persist (the file shrinks) and free the images of the
// messages it dropped, which would otherwise sit in the quota forever.
func TestTranscript_TrimOnLoadPersistsAndSweepsOrphanedImages(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "a.html")
	h := newHub(t, home, time.Minute)
	key := openSession(t, h, file).Key
	att, err := h.AddAttachment(key, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	path := h.AttachmentPath(key, att.ID)
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	b.WriteString("[")
	fmt.Fprintf(&b, `{"id":"m_old","role":"user","text":"with image","at":"2026-01-01T00:00:00Z","attachments":[{"id":%q,"mime":"image/png","bytes":72}]}`, att.ID)
	for i := 0; i < 520; i++ {
		fmt.Fprintf(&b, `,{"id":"m_%04d","role":"agent","text":"x","at":"2026-01-01T00:00:00Z"}`, i)
	}
	b.WriteString("]")
	tfile := filepath.Join(home, "forums", key, "transcript.json")
	if err := os.WriteFile(tfile, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	h2 := newHub(t, home, time.Minute)
	if _, err := h2.Open(file, false); err != nil {
		t.Fatal(err)
	}
	if got := len(transcriptOf(t, h2, key)); got != 500 {
		t.Fatalf("loaded %d messages", got)
	}
	if p := h2.AttachmentPath(key, att.ID); p != "" {
		t.Errorf("the orphaned image of a trimmed message is still on disk: %s", p)
	}
	data, _ := os.ReadFile(tfile)
	if strings.Contains(string(data), "m_old") || strings.Count(string(data), `"id": "m_`) != 500 {
		t.Errorf("the trim was not persisted: the file still has %d messages", strings.Count(string(data), `"id": "m_`))
	}
}
