package piask_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/piask"
)

const goodID = "0123456789abcdef0123456789abcdef"

func writeQuestion(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "question.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The extension is installed outside any camp, owner-only, and an install
// that finds the same content leaves the file (and a running soldier's copy)
// alone.
func TestInstall(t *testing.T) {
	root := t.TempDir()
	path, err := piask.Install(root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != piask.ExtensionPath(root) || !strings.HasPrefix(path, root) {
		t.Errorf("Install = %q, want a path under %q", path, root)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(piask.Source()) {
		t.Fatalf("installed content differs from the embedded source (err %v)", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	before, _ := os.Stat(path)
	time.Sleep(10 * time.Millisecond)
	if _, err := piask.Install(root); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged extension was rewritten")
	}

	if err := os.WriteFile(path, []byte("// edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := piask.Install(root); err != nil {
		t.Fatalf("Install over an edited copy: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(piask.Source()) {
		t.Error("an edited copy was not restored")
	}
}

// The embedded source has the pieces vexillum depends on: the tool name, the
// flag, the event the stock herdr integration reads and a header comment.
func TestSourceContract(t *testing.T) {
	src := string(piask.Source())
	for _, want := range []string{`name: "` + piask.ToolName + `"`, `"` + piask.Flag + `"`, `"herdr:blocked"`, "question.json", "answer.json"} {
		if !strings.Contains(src, want) {
			t.Errorf("the extension does not mention %s", want)
		}
	}
	if strings.ContainsRune(src, '—') {
		t.Error("the extension contains an em dash")
	}
}

func TestReadQuestion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "asks", "task")

	if _, err := piask.ReadQuestion(dir); !errors.Is(err, piask.ErrNoQuestion) {
		t.Fatalf("no file: err = %v, want ErrNoQuestion", err)
	}

	writeQuestion(t, dir, `{"version":1,"id":"`+goodID+`","question":"Which db?","options":["pg","sqlite"],"asked_at":"2026-01-02T03:04:05Z"}`)
	q, err := piask.ReadQuestion(dir)
	if err != nil {
		t.Fatalf("ReadQuestion: %v", err)
	}
	if q.ID != goodID || q.Question != "Which db?" || len(q.Options) != 2 || q.Options[1] != "sqlite" || q.AskedAt.Year() != 2026 {
		t.Errorf("question = %+v", q)
	}

	for name, body := range map[string]string{
		"unknown version": `{"version":2,"id":"` + goodID + `","question":"q"}`,
		"bad id":          `{"version":1,"id":"../etc","question":"q"}`,
		"empty question":  `{"version":1,"id":"` + goodID + `","question":""}`,
		"not json":        `{"version":1,`,
		"too large":       `{"version":1,"id":"` + goodID + `","question":"` + strings.Repeat("x", 300<<10) + `"}`,
	} {
		writeQuestion(t, dir, body)
		if _, err := piask.ReadQuestion(dir); err == nil || errors.Is(err, piask.ErrNoQuestion) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

// The answer is addressed to the question's id and replaced atomically: no
// temp file is left behind, and a second answer replaces the first.
func TestWriteAnswer(t *testing.T) {
	dir := t.TempDir()
	q := piask.Question{ID: goodID}

	if err := piask.WriteAnswer(dir, q, "", 0); err == nil {
		t.Error("an empty answer was accepted")
	}
	if err := piask.WriteAnswer(dir, q, "first", 0); err != nil {
		t.Fatal(err)
	}
	if err := piask.WriteAnswer(dir, q, "sqlite", 2); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "answer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(1) || got["id"] != goodID || got["answer"] != "sqlite" || got["option"] != float64(2) {
		t.Errorf("answer.json = %s", data)
	}
	if info, _ := os.Stat(filepath.Join(dir, "answer.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want just answer.json", len(entries))
	}

	piask.Withdraw(dir)
	if _, err := os.Stat(filepath.Join(dir, "answer.json")); !os.IsNotExist(err) {
		t.Error("Withdraw left the answer in place")
	}
}

func TestWaitConsumed(t *testing.T) {
	dir := t.TempDir()
	writeQuestion(t, dir, `{"version":1,"id":"`+goodID+`","question":"q"}`)
	q, _ := piask.ReadQuestion(dir)

	if piask.WaitConsumed(dir, q, 150*time.Millisecond) {
		t.Error("a question still open was reported as consumed")
	}

	go func() {
		time.Sleep(120 * time.Millisecond)
		os.Remove(filepath.Join(dir, "question.json"))
	}()
	if !piask.WaitConsumed(dir, q, 3*time.Second) {
		t.Error("a question that was removed was not reported as consumed")
	}

	// A different question in the same place means ours is closed.
	writeQuestion(t, dir, `{"version":1,"id":"fedcba9876543210fedcba9876543210","question":"next"}`)
	if !piask.WaitConsumed(dir, q, time.Second) {
		t.Error("a newer question was taken for the old one")
	}
}
