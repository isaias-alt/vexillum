package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/project"
)

func TestParseReviewArgs_FileOnly(t *testing.T) {
	file, port, noOpen, err := parseReviewArgs([]string{"artifact.html"})
	if err != nil {
		t.Fatalf("parseReviewArgs: %v", err)
	}
	if file != "artifact.html" || port != 0 || noOpen {
		t.Errorf("got (%q, %d, %v), want (%q, 0, false)", file, port, noOpen, "artifact.html")
	}
}

func TestParseReviewArgs_PortAndNoOpen(t *testing.T) {
	file, port, noOpen, err := parseReviewArgs([]string{"--port", "9000", "artifact.html", "--no-open"})
	if err != nil {
		t.Fatalf("parseReviewArgs: %v", err)
	}
	if file != "artifact.html" || port != 9000 || !noOpen {
		t.Errorf("got (%q, %d, %v)", file, port, noOpen)
	}
}

func TestParseReviewArgs_MissingFile(t *testing.T) {
	if _, _, _, err := parseReviewArgs([]string{"--no-open"}); err == nil {
		t.Fatal("expected an error for a missing file argument")
	}
}

func TestParseReviewArgs_ExtraArgument(t *testing.T) {
	if _, _, _, err := parseReviewArgs([]string{"a.html", "b.html"}); err == nil {
		t.Fatal("expected an error for a second positional argument")
	}
}

func TestParseReviewArgs_PortMissingValue(t *testing.T) {
	if _, _, _, err := parseReviewArgs([]string{"--port"}); err == nil {
		t.Fatal("expected an error when --port has no value")
	}
}

func TestParseReviewArgs_PortInvalid(t *testing.T) {
	cases := []string{"0", "-1", "70000", "not-a-number"}
	for _, raw := range cases {
		if _, _, _, err := parseReviewArgs([]string{"--port", raw, "a.html"}); err == nil {
			t.Errorf("expected an error for --port %q", raw)
		}
	}
}

func TestParsePort_Valid(t *testing.T) {
	port, err := parsePort("8080")
	if err != nil {
		t.Fatalf("parsePort: %v", err)
	}
	if port != 8080 {
		t.Errorf("port = %d, want 8080", port)
	}
}

func TestRunReview_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	projectDir := t.TempDir()
	vexillumHome := t.TempDir()
	code := runReview(context.Background(), projectDir, vexillumHome, filepath.Join(projectDir, "does-not-exist.html"), 0, true, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Error("expected an error message on stderr")
	}
}

func TestRunReview_DirectoryInsteadOfFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	projectDir := t.TempDir()
	vexillumHome := t.TempDir()
	code := runReview(context.Background(), projectDir, vexillumHome, projectDir, 0, true, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestRunReview_ServesUntilContextCanceled exercises the full success path:
// serve a real file, fetch it over HTTP, then cancel ctx (standing in for
// Ctrl-C) and confirm runReview shuts down cleanly instead of the caller
// needing to send the process a real OS signal.
func TestRunReview_ServesUntilContextCanceled(t *testing.T) {
	projectDir := t.TempDir()
	vexillumHome := t.TempDir()
	file := filepath.Join(projectDir, "artifact.html")
	if err := os.WriteFile(file, []byte(`<html><body>hi</body></html>`), 0o644); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runReview(ctx, projectDir, vexillumHome, file, 0, true, &stdout, &stderr)
	}()

	var url string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u := extractServedURL(stdout.String()); u != "" {
			url = u
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("server never reported a URL; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if root == "" {
		t.Fatal("expected a non-empty project root")
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code after cancel = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runReview did not shut down within 3s of context cancellation")
	}
}

func extractServedURL(stdout string) string {
	const marker = " at "
	idx := bytes.Index([]byte(stdout), []byte(marker))
	if idx < 0 {
		return ""
	}
	rest := stdout[idx+len(marker):]
	end := bytes.IndexByte([]byte(rest), '\n')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
