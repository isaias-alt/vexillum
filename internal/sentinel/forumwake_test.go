package sentinel_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/inbox"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

func putInbox(t *testing.T, root, session, uid string, ended bool) {
	t.Helper()
	e := inbox.Entry{UID: uid, Session: session, File: "/a/" + session + ".html", Tag: "feedback", Prompt: "p " + uid, Ended: ended}
	if _, err := inbox.Write(root, e); err != nil {
		t.Fatal(err)
	}
}

func TestForumWake_CoalescesPerSessionAndCountsFromTheInbox(t *testing.T) {
	root := t.TempDir()
	for _, uid := range []string{"pr_1", "pr_2", "pr_3"} {
		putInbox(t, root, "s1", uid, false)
		if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", File: "/a/s1.html", Count: 1}); err != nil {
			t.Fatal(err)
		}
	}
	putInbox(t, root, "s2", "pr_9", true)
	if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s2", File: "/a/s2.html", Count: 1, Ended: true}); err != nil {
		t.Fatal(err)
	}

	wakes, err := sentinel.DrainForum(root)
	if err != nil || len(wakes) != 2 {
		t.Fatalf("DrainForum = %+v %v, want one wake per session", wakes, err)
	}
	by := map[string]sentinel.ForumWake{}
	for _, w := range wakes {
		by[w.Session] = w
	}
	if by["s1"].Count != 3 || by["s1"].Ended || by["s2"].Count != 1 || !by["s2"].Ended {
		t.Errorf("wakes = %+v, want s1 3 messages not ended, s2 1 message ended", by)
	}
	again, _ := sentinel.DrainForum(root)
	if len(again) != 0 {
		t.Errorf("a drained wake came back: %+v", again)
	}
}

func TestForumWake_StaleOnceTheInboxIsConfirmed(t *testing.T) {
	root := t.TempDir()
	putInbox(t, root, "s1", "pr_1", false)
	if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", Count: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.MarkRead(root, []string{"pr_1"}); err != nil {
		t.Fatal(err)
	}
	wakes, err := sentinel.DrainForum(root)
	if err != nil || len(wakes) != 0 {
		t.Fatalf("DrainForum = %+v %v, want the stale wake dropped", wakes, err)
	}
	if left, _ := os.ReadDir(filepath.Join(root, "wakes", "forum")); len(left) != 0 {
		t.Errorf("stale wake file not consumed: %v", left)
	}
}

func TestForumWake_EndedIsStickyAcrossCoalescing(t *testing.T) {
	root := t.TempDir()
	putInbox(t, root, "s1", "pr_1", true)
	_ = sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", Count: 1, Ended: true, DetectedAt: time.Unix(10, 0)})
	_ = sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", Count: 2, Ended: false, DetectedAt: time.Unix(20, 0)})
	wakes, _ := sentinel.DrainForum(root)
	if len(wakes) != 1 || !wakes[0].Ended || !wakes[0].DetectedAt.Equal(time.Unix(10, 0)) {
		t.Fatalf("wakes = %+v", wakes)
	}
}

func TestForumWake_DoesNotDisturbTaskDrain(t *testing.T) {
	root := t.TempDir()
	putInbox(t, root, "s1", "pr_1", false)
	if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", Count: 1}); err != nil {
		t.Fatal(err)
	}
	wakes, err := sentinel.Drain(root)
	if err != nil || len(wakes) != 0 {
		t.Fatalf("Drain = %+v %v: a forum wake must not surface as a task wake", wakes, err)
	}
	if fw, _ := sentinel.DrainForum(root); len(fw) != 1 {
		t.Errorf("Drain consumed the forum wake: %+v", fw)
	}
}

func TestForumWake_RejectsUnsafeSession(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{"", "..", "a/b"} {
		if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: s}); err == nil {
			t.Errorf("RecordForumWake(%q) succeeded", s)
		}
	}
}

func TestForumWake_ConcurrentDrainsDeliverOnce(t *testing.T) {
	root := t.TempDir()
	putInbox(t, root, "s1", "pr_1", false)
	if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", Count: 1}); err != nil {
		t.Fatal(err)
	}
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		go func() {
			w, _ := sentinel.DrainForum(root)
			results <- len(w)
		}()
	}
	total := 0
	for i := 0; i < 8; i++ {
		total += <-results
	}
	if total != 1 {
		t.Errorf("%d drains delivered the wake, want exactly 1", total)
	}
}
