package pending_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/pending"
)

func TestAddListClear_RoundTrip(t *testing.T) {
	root := t.TempDir()

	a, err := pending.Add(root, "  dispatch the docs mission  ", nil, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if a.Text != "dispatch the docs mission" {
		t.Errorf("Text = %q, want it trimmed", a.Text)
	}
	if err := pending.ValidateID(a.ID); err != nil {
		t.Errorf("generated id %q is not valid: %v", a.ID, err)
	}
	time.Sleep(2 * time.Millisecond)
	b, err := pending.Add(root, "ship the PR", nil, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Reload strictly from disk: oldest first.
	items, err := pending.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 || items[0].ID != a.ID || items[1].ID != b.ID {
		t.Fatalf("List = %+v, want [%s %s]", items, a.ID, b.ID)
	}
	if items[0].SchemaVersion != pending.SchemaVersion || items[0].CreatedAt.IsZero() {
		t.Errorf("item missing schema version or created_at: %+v", items[0])
	}

	if err := pending.Clear(root, a.ID); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	items, err = pending.List(root)
	if err != nil {
		t.Fatalf("List after clear: %v", err)
	}
	if len(items) != 1 || items[0].ID != b.ID {
		t.Fatalf("List after clear = %+v, want only %s", items, b.ID)
	}
}

func TestList_MissingDirIsEmpty(t *testing.T) {
	items, err := pending.List(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(items) != 0 {
		t.Fatalf("List = %v, %v; want empty, nil", items, err)
	}
}

func TestAdd_RejectsEmptyText(t *testing.T) {
	if _, err := pending.Add(t.TempDir(), " \n\t", nil, 0); err == nil {
		t.Fatal("expected an error for blank text")
	}
}

func TestAdd_LeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := pending.Add(root, "x", nil, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "pending"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("unexpected file %s", e.Name())
		}
	}
}

func TestClear_UnknownAndInvalidIDs(t *testing.T) {
	root := t.TempDir()
	if err := pending.Clear(root, "deadbeef"); !errors.Is(err, pending.ErrNotFound) {
		t.Errorf("Clear unknown = %v, want ErrNotFound", err)
	}
	for _, id := range []string{"", "../tasks/x", "DEADBEEF", "dead", "deadbeef.json"} {
		if err := pending.Clear(root, id); err == nil || errors.Is(err, pending.ErrNotFound) {
			t.Errorf("Clear(%q) = %v, want an invalid-id error", id, err)
		}
	}
}

func TestList_CorruptFileNamesIt(t *testing.T) {
	root := t.TempDir()
	if _, err := pending.Add(root, "ok", nil, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	bad := filepath.Join(root, "pending", "00000000.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := pending.List(root)
	if err == nil || !strings.Contains(err.Error(), "00000000.json") {
		t.Fatalf("List error = %v, want one naming the corrupt file", err)
	}
}

func TestAdd_WithOptionsRoundTrips(t *testing.T) {
	root := t.TempDir()
	a, err := pending.Add(root, "ship the docs PR?", []string{"  ship now ", "hold"}, 1)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(a.Options) != 2 || a.Options[0] != "ship now" || a.Recommended != 1 {
		t.Fatalf("Add = %+v, want trimmed options and recommended 1", a)
	}
	items, err := pending.List(root)
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %v, %v", items, err)
	}
	if got := items[0]; len(got.Options) != 2 || got.Options[1] != "hold" || got.Recommended != 1 {
		t.Errorf("reloaded item = %+v", got)
	}
}

func TestAdd_RejectsBadOptions(t *testing.T) {
	cases := []struct {
		name        string
		options     []string
		recommended int
	}{
		{"blank option", []string{"a", "  "}, 0},
		{"repeated option", []string{"a", "a"}, 0},
		{"recommend without options", nil, 1},
		{"recommend too high", []string{"a", "b"}, 3},
		{"recommend negative", []string{"a", "b"}, -1},
	}
	for _, c := range cases {
		root := t.TempDir()
		if _, err := pending.Add(root, "q", c.options, c.recommended); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
		if items, _ := pending.List(root); len(items) != 0 {
			t.Errorf("%s: a rejected item was saved: %+v", c.name, items)
		}
	}
}

// An item written before options existed has neither field; it must still
// load, with no options and no recommendation, and re-encode without them.
func TestList_ReadsItemWithoutOptions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"schema_version":1,"id":"1a2b3c4d","text":"old item","created_at":"2026-09-24T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "1a2b3c4d.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := pending.List(root)
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %v, %v", items, err)
	}
	if items[0].Text != "old item" || len(items[0].Options) != 0 || items[0].Recommended != 0 {
		t.Errorf("old item = %+v, want no options and no recommendation", items[0])
	}
}

func TestList_RejectsRecommendedOutOfRange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"schema_version":1,"id":"1a2b3c4d","text":"x","options":["a"],"recommended":2,"created_at":"2026-09-24T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "1a2b3c4d.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.List(root); err == nil || !strings.Contains(err.Error(), "1a2b3c4d.json") {
		t.Fatalf("List error = %v, want one naming the file", err)
	}
}
