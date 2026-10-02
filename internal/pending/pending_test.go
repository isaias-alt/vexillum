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

	a, err := pending.Add(root, "  dispatch the docs mission  ")
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
	b, err := pending.Add(root, "ship the PR")
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
	if _, err := pending.Add(t.TempDir(), " \n\t"); err == nil {
		t.Fatal("expected an error for blank text")
	}
}

func TestAdd_LeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := pending.Add(root, "x"); err != nil {
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
	if _, err := pending.Add(root, "ok"); err != nil {
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
