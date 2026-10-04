package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var amendTime = time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC)

func TestAddAmendment_RecordsTrimmedTextWithSourceAndTime(t *testing.T) {
	task := Task{Prompt: "p"}
	task.AddAmendment(AmendmentSourcePrompt, "  restyle the header \n", amendTime)

	if len(task.Amendments) != 1 {
		t.Fatalf("expected one amendment, got %+v", task.Amendments)
	}
	a := task.Amendments[0]
	if a.Text != "restyle the header" || a.Source != AmendmentSourcePrompt || !a.At.Equal(amendTime) {
		t.Errorf("unexpected amendment: %+v", a)
	}
	if task.Prompt != "p" {
		t.Errorf("the dispatch prompt must stay untouched, got %q", task.Prompt)
	}
}

func TestAddAmendment_IgnoresBlankText(t *testing.T) {
	var task Task
	task.AddAmendment(AmendmentSourcePrompt, " \n\t ", amendTime)
	if len(task.Amendments) != 0 {
		t.Errorf("expected blank text to record nothing, got %+v", task.Amendments)
	}
}

func TestAddAmendment_CapsTextLength(t *testing.T) {
	var task Task
	task.AddAmendment(AmendmentSourceDecide, strings.Repeat("é", MaxAmendmentRunes*3), amendTime)

	text := task.Amendments[0].Text
	if !utf8.ValidString(text) {
		t.Fatal("truncation must not split a rune")
	}
	if got, max := utf8.RuneCountInString(text), MaxAmendmentRunes+utf8.RuneCountInString(amendmentTruncatedMarker); got != max {
		t.Errorf("expected %d runes (cap plus marker), got %d", max, got)
	}
	if !strings.HasSuffix(text, amendmentTruncatedMarker) {
		t.Errorf("expected the truncation marker, got suffix %q", text[len(text)-20:])
	}

	// Exactly at the cap is kept whole, with no marker.
	task = Task{}
	full := strings.Repeat("x", MaxAmendmentRunes)
	task.AddAmendment(AmendmentSourceDecide, full, amendTime)
	if task.Amendments[0].Text != full {
		t.Error("text at exactly the cap must be kept in full")
	}
}

func TestAddAmendment_KeepsOnlyTheNewestWhenOverTheCountCap(t *testing.T) {
	var task Task
	for i := 0; i < MaxAmendments+5; i++ {
		task.AddAmendment(AmendmentSourcePrompt, "n"+string(rune('a'+i)), amendTime.Add(time.Duration(i)*time.Minute))
	}
	if len(task.Amendments) != MaxAmendments {
		t.Fatalf("expected %d amendments, got %d", MaxAmendments, len(task.Amendments))
	}
	if task.Amendments[0].Text != "nf" || task.Amendments[MaxAmendments-1].Text != "n"+string(rune('a'+MaxAmendments+4)) {
		t.Errorf("expected the oldest dropped and order kept, got first=%q last=%q", task.Amendments[0].Text, task.Amendments[MaxAmendments-1].Text)
	}
}

func TestComposeIntent_NoAmendmentsIsJustThePrompt(t *testing.T) {
	if got := ComposeIntent("  add a file \n", nil); got != "add a file" {
		t.Errorf("got %q", got)
	}
	if got := ComposeIntent("  ", []Amendment{{Text: "x", At: amendTime, Source: AmendmentSourcePrompt}}); got != "" {
		t.Errorf("amendments alone are not a mission statement, got %q", got)
	}
}

func TestComposeIntent_AppendsLabeledAmendmentsInOrder(t *testing.T) {
	task := Task{Prompt: "add a retry to the client"}
	task.AddAmendment(AmendmentSourcePrompt, "restyle the banner", amendTime)
	task.AddAmendment(AmendmentSourceDecide, "Asked \"drop the shortcut?\", the general answered: yes", amendTime.Add(time.Hour))

	want := "add a retry to the client\n\n" +
		"Instructions the general gave afterward, in the order given (they are part of the mission's intent, and what they ask for is required):\n" +
		"1. [2026-10-02T12:30:00Z, via vx prompt] restyle the banner\n" +
		"2. [2026-10-02T13:30:00Z, via vx decide] Asked \"drop the shortcut?\", the general answered: yes"
	if got := task.Intent(); got != want {
		t.Errorf("intent mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestAmendments_RoundTripThroughDisk(t *testing.T) {
	root := t.TempDir()
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatal(err)
	}
	task.AddAmendment(AmendmentSourcePrompt, "more", amendTime)
	if err := Save(root, task); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 1 || got.Amendments[0].Text != "more" || !got.Amendments[0].At.Equal(amendTime) || got.Amendments[0].Source != AmendmentSourcePrompt {
		t.Errorf("amendments did not survive a save/load: %+v", got.Amendments)
	}
}

// A task file written before amendments existed must keep loading, with no
// amendments. It carries the legacy schema_version 3.
func TestLoad_OlderTaskFileWithoutAmendments(t *testing.T) {
	root := t.TempDir()
	id := "0123456789abcdef"
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"schema_version":3,"id":"` + id + `","kind":"mission","prompt":"p","status":"done","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(root, "tasks", id+".json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Amendments != nil || got.Intent() != "p" {
		t.Errorf("expected no amendments and the plain prompt as intent, got %+v / %q", got.Amendments, got.Intent())
	}
}
