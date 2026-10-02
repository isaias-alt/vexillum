package inbox

import (
	"path/filepath"
	"testing"
	"time"
)

func entry(session, uid, prompt string) Entry {
	return Entry{UID: uid, Session: session, File: "/a/" + session + ".html", Tag: "feedback", Prompt: prompt, QueuedAt: time.Unix(1, 0).UTC()}
}

func TestWrite_IsIdempotentPerUID(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		if ok, err := Write(root, entry("s1", "pr_1", "hello")); err != nil || !ok {
			t.Fatalf("write %d = %v %v", i, ok, err)
		}
	}
	es, err := Pending(root)
	if err != nil || len(es) != 1 || es[0].Prompt != "hello" {
		t.Fatalf("pending = %+v %v, want exactly one", es, err)
	}
}

func TestWrite_RedeliveryKeepsReceiptOrderAndLearnsTheEnd(t *testing.T) {
	root := t.TempDir()
	first := entry("s1", "pr_1", "one")
	first.ReceivedAt = time.Unix(100, 0).UTC()
	if _, err := Write(root, first); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, entry("s1", "pr_2", "two")); err != nil {
		t.Fatal(err)
	}
	again := entry("s1", "pr_1", "one")
	again.Ended, again.EndedBy = true, "user"
	again.ReceivedAt = time.Now().Add(time.Hour).UTC()
	if _, err := Write(root, again); err != nil {
		t.Fatal(err)
	}
	es, _ := Pending(root)
	if len(es) != 2 || es[0].UID != "pr_1" || !es[0].Ended || es[0].EndedBy != "user" || !es[0].ReceivedAt.Equal(first.ReceivedAt) {
		t.Fatalf("pending = %+v, want pr_1 first, ended, original receipt time", es)
	}
}

func TestMarkRead_ConfirmsAndSurvivesRedelivery(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, entry("s1", "pr_1", "one")); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, entry("s2", "pr_2", "two")); err != nil {
		t.Fatal(err)
	}
	if n, err := MarkRead(root, []string{"pr_1", "pr_unknown"}); err != nil || n != 1 {
		t.Fatalf("MarkRead = %d %v, want 1", n, err)
	}
	if n, _ := MarkRead(root, []string{"pr_1"}); n != 0 {
		t.Errorf("confirming twice moved %d", n)
	}
	// The hub redelivers pr_1 (the listener crashed before its ack): it must stay read.
	if ok, err := Write(root, entry("s1", "pr_1", "one")); err != nil || ok {
		t.Fatalf("rewrite of a confirmed entry = %v %v, want not written", ok, err)
	}
	es, _ := Pending(root)
	if len(es) != 1 || es[0].UID != "pr_2" {
		t.Fatalf("pending = %+v, want only pr_2", es)
	}
	sum, _ := Unread(root, "s1")
	if sum.Count != 0 {
		t.Errorf("s1 unread = %d", sum.Count)
	}
}

func TestMarkRead_PrunesOldConfirmedEntries(t *testing.T) {
	root := t.TempDir()
	var uids []string
	for i := 0; i < maxRead+5; i++ {
		uid := "pr_" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		uids = append(uids, uid)
		if _, err := Write(root, entry("s1", uid, "x")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := MarkRead(root, uids); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(sessionDir(root, "s1"), readDirName, "*.json"))
	if len(files) != maxRead {
		t.Errorf("%d confirmed files kept, want %d", len(files), maxRead)
	}
}

func TestWrite_RejectsUnsafeNames(t *testing.T) {
	root := t.TempDir()
	for _, e := range []Entry{entry("../x", "pr_1", "a"), entry("s", "../pr", "a"), entry("s", "", "a"), entry("", "pr", "a")} {
		if _, err := Write(root, e); err == nil {
			t.Errorf("Write(%q,%q) succeeded", e.Session, e.UID)
		}
	}
	if _, err := Write("", entry("s", "pr_1", "a")); err == nil {
		t.Error("Write without a project root succeeded")
	}
}

func TestSessions_SummarizesUnread(t *testing.T) {
	root := t.TempDir()
	a := entry("s1", "pr_1", "one")
	b := entry("s1", "pr_2", "two")
	b.Ended = true
	for _, e := range []Entry{a, b, entry("s2", "pr_3", "three")} {
		if _, err := Write(root, e); err != nil {
			t.Fatal(err)
		}
	}
	ss, err := Sessions(root)
	if err != nil || len(ss) != 2 || ss[0].Session != "s1" || ss[0].Count != 2 || !ss[0].Ended || ss[1].Count != 1 || ss[1].Ended {
		t.Fatalf("sessions = %+v %v", ss, err)
	}
}
