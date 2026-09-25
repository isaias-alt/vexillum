package herdr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// herdrStub puts a fake "herdr" binary on PATH whose behavior is driven by
// script, a POSIX shell body dispatching on herdr's own two-word
// subcommands ($1 $2, e.g. "tab create", "agent get"). CLI shells out to
// this fake exactly the way it shells out to the real herdr binary, so
// these tests exercise CLI's own argument construction, JSON field
// extraction, and error decoding without a real herdr install or a real
// Claude Code session - the same technique internal/cli's land_merge_test.go
// and ship_test.go already use for "gh" and "no-mistakes".
func herdrStub(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte(body), 0o755); err != nil {
		t.Fatalf("writing herdr stub: %v", err)
	}
	return dir
}

func TestCLI_CreateTab_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "tab create") echo '{"result":{"tab":{"tab_id":"tab-1"},"root_pane":{"pane_id":"pane-1"}}}' ;;
esac`))

	tabID, paneID, err := CLI{}.CreateTab("ws1", "/some/cwd", "soldier-1", "FOO=bar")
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if tabID != "tab-1" || paneID != "pane-1" {
		t.Errorf("CreateTab = (%q, %q), want (tab-1, pane-1)", tabID, paneID)
	}
}

// A "successful" response missing the fields CreateTab extracts is still
// an error, named clearly, not a silently empty tab/pane id.
func TestCLI_CreateTab_MalformedResponse(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "tab create") echo '{"result":{}}' ;;
esac`))

	_, _, err := CLI{}.CreateTab("ws1", "/some/cwd", "soldier-1")
	if err == nil {
		t.Fatal("expected an error for a response missing tab_id/pane_id")
	}
	if !strings.Contains(err.Error(), "unexpected tab create response") {
		t.Errorf("expected error naming the unexpected response, got: %v", err)
	}
}

func TestCLI_AgentStart_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent start") echo '{"result":{}}' ;;
esac`))

	if err := (CLI{}).AgentStart("soldier-1", "claude", "pane-1", "--dangerously-skip-permissions"); err != nil {
		t.Fatalf("AgentStart: %v", err)
	}
}

func TestCLI_AgentSendKeys_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent send-keys") echo '{"result":{}}' ;;
esac`))

	if err := (CLI{}).AgentSendKeys("soldier-1", "down", "enter"); err != nil {
		t.Fatalf("AgentSendKeys: %v", err)
	}
}

func TestCLI_AgentReady_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") echo '{"result":{"agent":{"interactive_ready":true,"agent_status":"idle"}}}' ;;
esac`))

	ready, err := (CLI{}).AgentReady("soldier-1")
	if err != nil {
		t.Fatalf("AgentReady: %v", err)
	}
	if !ready {
		t.Error("expected AgentReady to report true")
	}
}

func TestCLI_AgentStatus_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") echo '{"result":{"agent":{"agent_status":"working"}}}' ;;
esac`))

	status, err := (CLI{}).AgentStatus("soldier-1")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	if status != "working" {
		t.Errorf("AgentStatus = %q, want %q", status, "working")
	}
}

func TestCLI_AgentPrompt_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent prompt") echo '{"result":{"agent":{"agent_status":"done"}}}' ;;
esac`))

	status, err := (CLI{}).AgentPrompt("soldier-1", "do the thing", 5000)
	if err != nil {
		t.Fatalf("AgentPrompt: %v", err)
	}
	if status != "done" {
		t.Errorf("AgentPrompt = %q, want %q", status, "done")
	}
}

// AgentRead is the one command whose stdout is plain transcript text, not
// a JSON envelope - it must be returned verbatim, not run through run()'s
// JSON decoder.
func TestCLI_AgentRead_PlainTextStdout(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent read") printf 'human: do the thing\nassistant: done\n' ;;
esac`))

	out, err := (CLI{}).AgentRead("soldier-1", 200)
	if err != nil {
		t.Fatalf("AgentRead: %v", err)
	}
	want := "human: do the thing\nassistant: done\n"
	if out != want {
		t.Errorf("AgentRead = %q, want %q", out, want)
	}
}

// A failing agent read still returns whatever combined output herdr
// produced, alongside the wrapped error - AgentRead never JSON-decodes
// its stderr the way run() does.
func TestCLI_AgentRead_ErrorIncludesOutput(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent read") echo "agent not found" >&2; exit 1 ;;
esac`))

	out, err := (CLI{}).AgentRead("ghost", 200)
	if err == nil {
		t.Fatal("expected an error for a failing agent read")
	}
	if !strings.Contains(err.Error(), "reading soldier transcript") {
		t.Errorf("expected error to name the operation, got: %v", err)
	}
	if !strings.Contains(out, "agent not found") {
		t.Errorf("expected the combined output to be returned even on failure, got: %q", out)
	}
}

func TestCLI_TabClose_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "tab close") echo '{"result":{}}' ;;
esac`))

	if err := (CLI{}).TabClose("tab-1"); err != nil {
		t.Fatalf("TabClose: %v", err)
	}
}

// run()'s exit-code-1 stderr JSON decode path: herdr's structured API
// errors decode into *APIError, recognized by the Is* classifiers.
func TestCLI_run_ExitCode1_DecodesStderrJSONError(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") echo '{"error":{"code":"agent_not_found","message":"no such agent"}}' >&2; exit 1 ;;
esac`))

	_, err := (CLI{}).AgentStatus("ghost")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *APIError, got: %v (%T)", err, err)
	}
	if apiErr.Code != "agent_not_found" || apiErr.Message != "no such agent" {
		t.Errorf("APIError = %+v, want code=agent_not_found message=%q", apiErr, "no such agent")
	}
	if !IsNotFound(err) {
		t.Error("expected IsNotFound to recognize this error")
	}
}

// When exit status 1's stderr isn't (or doesn't decode as) herdr's JSON
// error envelope, run() falls back to a plain error that still surfaces
// herdr's raw stderr instead of swallowing it.
func TestCLI_run_ExitCode1_NonJSONStderr_FallsBackToPlainError(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") echo "boom: something broke" >&2; exit 1 ;;
esac`))

	_, err := (CLI{}).AgentStatus("soldier-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("expected a plain error, not an *APIError, got: %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "boom: something broke") {
		t.Errorf("expected the error to include herdr's stderr, got: %v", err)
	}
}

// A non-JSON, non-error-shaped success response is still a decode
// failure, not a silent empty result.
func TestCLI_run_MalformedSuccessJSON(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") echo 'not json' ;;
esac`))

	_, err := (CLI{}).AgentStatus("soldier-1")
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parsing herdr response") {
		t.Errorf("expected error to mention parsing, got: %v", err)
	}
}

// Without "herdr" on PATH at all, run() fails clearly instead of a raw
// exec error reaching the caller unwrapped.
func TestCLI_run_HerdrNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, _, err := (CLI{}).CreateTab("ws1", "/cwd", "label")
	if err == nil {
		t.Fatal("expected an error when herdr isn't on PATH")
	}
	if !strings.Contains(err.Error(), "running herdr") {
		t.Errorf("expected the error to name the failed run, got: %v", err)
	}
}
