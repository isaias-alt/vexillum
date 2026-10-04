package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runPendingT(t *testing.T, project, home string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = runPending(project, home, args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestPending_AddListClearEndToEnd(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()

	code, out, errOut := runPendingT(t, project, home, "list")
	if code != 0 || strings.TrimSpace(out) != "no pending decisions." {
		t.Fatalf("empty list = %d %q %q", code, out, errOut)
	}

	code, out, errOut = runPendingT(t, project, home, "add", "dispatch", "the", "docs mission")
	if code != 0 {
		t.Fatalf("add = %d %q", code, errOut)
	}
	id := strings.TrimSpace(strings.TrimPrefix(out, "pending_id="))
	if len(id) != 8 {
		t.Fatalf("add output %q has no 8-char id", out)
	}

	code, out, _ = runPendingT(t, project, home, "list")
	if code != 0 || !strings.Contains(out, id) || !strings.Contains(out, "dispatch the docs mission") {
		t.Fatalf("list = %d %q, want the added item", code, out)
	}

	code, out, _ = runPendingT(t, project, home, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json = %d", code)
	}
	var snap struct {
		SchemaVersion int `json:"schema_version"`
		Pending       []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"pending"`
	}
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("list --json is not valid JSON: %v\n%s", err, out)
	}
	if snap.SchemaVersion != 1 || len(snap.Pending) != 1 || snap.Pending[0].ID != id || snap.Pending[0].Text != "dispatch the docs mission" {
		t.Fatalf("snapshot = %+v", snap)
	}

	// Another project's namespace never sees it.
	if code, out, _ := runPendingT(t, t.TempDir(), home, "list"); code != 0 || !strings.Contains(out, "no pending") {
		t.Errorf("other project list = %d %q, want empty", code, out)
	}

	if code, _, errOut := runPendingT(t, project, home, "clear", id); code != 0 {
		t.Fatalf("clear = %d %q", code, errOut)
	}
	code, out, _ = runPendingT(t, project, home, "list", "--json")
	if code != 0 || !strings.Contains(out, `"pending": []`) {
		t.Fatalf("list --json after clear = %d %q, want an empty array not null", code, out)
	}
	if code, _, errOut := runPendingT(t, project, home, "clear", id); code != 1 || !strings.Contains(errOut, "already cleared") {
		t.Errorf("second clear = %d %q, want a not-found failure", code, errOut)
	}
}

func TestPending_UsageErrors(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	cases := [][]string{
		{"bogus"},
		{"add"},
		{"add", "  "},
		{"clear"},
		{"clear", "a", "b"},
		{"clear", "../x"},
		{"list", "--nope"},
		{"add", "q", "--options", "a"},
		{"add", "q", "--option"},
		{"add", "q", "--option", "a", "--recommend", "2"},
		{"add", "q", "--recommend", "1"},
		{"add", "q", "--option", "a", "--recommend", "0"},
		{"add", "q", "--option", "a", "--recommend", "x"},
		{"add", "q", "--option", "a", "--option", "a"},
		{"add", "q", "--option", " "},
		{"add", "--option", "a"},
	}
	for _, args := range cases {
		if code, _, errOut := runPendingT(t, project, home, args...); code != 1 || errOut == "" {
			t.Errorf("%v = %d %q, want exit 1 with a message", args, code, errOut)
		}
	}
}

func TestPending_AddWithOptions(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()

	// Flags may sit anywhere among the text words; both spellings work.
	code, out, errOut := runPendingT(t, project, home, "add", "Ship", "--option", "ship now", "the docs PR?", "--option=hold", "--recommend=1")
	if code != 0 {
		t.Fatalf("add = %d %q", code, errOut)
	}
	id := strings.TrimSpace(strings.TrimPrefix(out, "pending_id="))

	// A second item without options, to check the shapes side by side.
	if code, _, errOut := runPendingT(t, project, home, "add", "plain question"); code != 0 {
		t.Fatalf("plain add = %d %q", code, errOut)
	}

	code, out, _ = runPendingT(t, project, home, "list")
	if code != 0 || !strings.Contains(out, "Ship the docs PR?") ||
		!strings.Contains(out, "1. ship now (recommended)") || !strings.Contains(out, "2. hold\n") {
		t.Fatalf("list = %d %q, want numbered options with the recommended one marked", code, out)
	}

	code, out, _ = runPendingT(t, project, home, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json = %d", code)
	}
	var snap struct {
		SchemaVersion int `json:"schema_version"`
		Pending       []struct {
			ID          string   `json:"id"`
			Text        string   `json:"text"`
			Options     []string `json:"options"`
			Recommended int      `json:"recommended"`
		} `json:"pending"`
	}
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("list --json is not valid JSON: %v\n%s", err, out)
	}
	if snap.SchemaVersion != 1 || len(snap.Pending) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	with, without := snap.Pending[0], snap.Pending[1]
	if with.ID != id || with.Text != "Ship the docs PR?" || len(with.Options) != 2 ||
		with.Options[0] != "ship now" || with.Options[1] != "hold" || with.Recommended != 1 {
		t.Errorf("item with options = %+v", with)
	}
	if without.Text != "plain question" || len(without.Options) != 0 || without.Recommended != 0 {
		t.Errorf("item without options = %+v", without)
	}
}

func TestPending_AddDoubleDashEndsFlags(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	if code, _, errOut := runPendingT(t, project, home, "add", "--option", "yes", "--", "drop the --force flag?"); code != 0 {
		t.Fatalf("add = %d %q", code, errOut)
	}
	_, out, _ := runPendingT(t, project, home, "list")
	if !strings.Contains(out, "drop the --force flag?") {
		t.Errorf("list = %q, want the text after -- kept verbatim", out)
	}
}
