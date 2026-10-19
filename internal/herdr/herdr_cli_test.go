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
// and ship_test.go already use for "gh" and "claude".
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

	tabID, paneID, err := CLI{}.CreateTab("ws1", "/some/cwd", "soldier-1")
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if tabID != "tab-1" || paneID != "pane-1" {
		t.Errorf("CreateTab = (%q, %q), want (tab-1, pane-1)", tabID, paneID)
	}
}

// Each env entry becomes a --env flag of tab create, so the pane's shell and
// the agent started in it inherit it.
func TestCLI_CreateTab_PassesEnvAsFlags(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "tab create") printf '%s\n' "$@" > '`+rec+`'; echo '{"result":{"tab":{"tab_id":"tab-1"},"root_pane":{"pane_id":"pane-1"}}}' ;;
esac`))

	if _, _, err := (CLI{}).CreateTab("ws1", "/some/cwd", "soldier-1", "A=1", "B=two words"); err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	raw, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	want := []string{"tab", "create", "--workspace", "ws1", "--cwd", "/some/cwd", "--label", "soldier-1", "--no-focus", "--env", "A=1", "--env", "B=two words"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("herdr args = %q, want %q", got, want)
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

// AgentReadVisible, like AgentRead, is plain text - but requests the
// "visible"/"--ansi" source instead of the default recent-unwrapped
// scrollback.
func TestCLI_AgentReadVisible_PlainTextStdout(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent read")
    seen=0
    for a in "$@"; do
      if [ "$prev" = "--source" ] && [ "$a" = "visible" ]; then seen=1; fi
      if [ "$a" = "--ansi" ]; then ansi=1; fi
      prev="$a"
    done
    if [ "$seen" != "1" ] || [ "$ansi" != "1" ]; then
      echo "wrong args: $@" >&2
      exit 1
    fi
    printf '\x1b[38;2;74;165;240mSelected\x1b[0m\n'
    ;;
esac`))

	out, err := (CLI{}).AgentReadVisible("soldier-1")
	if err != nil {
		t.Fatalf("AgentReadVisible: %v", err)
	}
	if !strings.Contains(out, "Selected") {
		t.Errorf("expected the raw ANSI-intact capture returned, got %q", out)
	}
}

func TestCLI_AgentReadVisible_ErrorIncludesOutput(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent read") echo "agent not found" >&2; exit 1 ;;
esac`))

	out, err := (CLI{}).AgentReadVisible("ghost")
	if err == nil {
		t.Fatal("expected an error for a failing visible read")
	}
	if !strings.Contains(err.Error(), "reading soldier's visible pane") {
		t.Errorf("expected error to name the operation, got: %v", err)
	}
	if !strings.Contains(out, "agent not found") {
		t.Errorf("expected the combined output to be returned even on failure, got: %q", out)
	}
}

func TestCLI_AgentWait_Success(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent wait") echo '{"result":{"agent":{"agent_status":"blocked"}}}' ;;
esac`))

	status, err := (CLI{}).AgentWait("soldier-1", nil, 5000)
	if err != nil {
		t.Fatalf("AgentWait: %v", err)
	}
	if status != "blocked" {
		t.Errorf("AgentWait = %q, want %q", status, "blocked")
	}
}

// AgentWait passes each of until as its own --until flag, alongside the
// caller's timeout.
func TestCLI_AgentWait_PassesUntilFlags(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent wait")
    count=0
    for a in "$@"; do
      if [ "$a" = "--until" ]; then count=$((count+1)); fi
    done
    if [ "$count" != "2" ]; then
      echo "expected 2 --until flags, got $count ($@)" >&2
      exit 1
    fi
    echo '{"result":{"agent":{"agent_status":"idle"}}}'
    ;;
esac`))

	if _, err := (CLI{}).AgentWait("soldier-1", []string{"idle", "blocked"}, 5000); err != nil {
		t.Fatalf("AgentWait: %v", err)
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

// AgentGet takes a pane id as well as a name and reads the harness kind, pane
// and working directories out of the agent object.
func TestCLI_AgentGet_ReadsTheAgent(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent get") printf '%s\n' "$@" > '`+rec+`'; echo '{"result":{"agent":{"agent":"opencode","pane_id":"w1:p2","name":"cmd","cwd":"/p","foreground_cwd":"/p/sub","focused":true},"type":"agent_info"}}' ;;
esac`))

	got, err := CLI{}.AgentGet("w1:p2")
	if err != nil {
		t.Fatalf("AgentGet: %v", err)
	}
	want := AgentInfo{Kind: "opencode", PaneID: "w1:p2", Name: "cmd", Cwd: "/p", ForegroundCwd: "/p/sub", Focused: true}
	if got != want {
		t.Errorf("AgentGet = %+v, want %+v", got, want)
	}
	raw, _ := os.ReadFile(rec)
	if strings.TrimSpace(strings.ReplaceAll(string(raw), "\n", " ")) != "agent get w1:p2" {
		t.Errorf("herdr was called with %q", raw)
	}
}

// agent_status and agent_session come through; a session that is not an id (or
// no session at all, an OpenCode pane before its first message) reads empty.
func TestCLI_AgentGet_ReadsStatusAndSession(t *testing.T) {
	for name, tc := range map[string]struct {
		agent       string
		wantStatus  string
		wantSession string
	}{
		"id":      {`"agent_status":"idle","agent_session":{"agent":"opencode","kind":"id","source":"herdr:opencode","value":"ses_abc"}`, "idle", "ses_abc"},
		"none":    {`"agent_status":"idle"`, "idle", ""},
		"null":    {`"agent_status":"working","agent_session":null`, "working", ""},
		"a path":  {`"agent_status":"blocked","agent_session":{"kind":"path","value":"/x/y.jsonl"}`, "blocked", ""},
		"no data": {`"pane_id":"w1:p2"`, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PATH", herdrStub(t, `echo '{"result":{"agent":{"agent":"opencode",`+tc.agent+`},"type":"agent_info"}}'`))
			got, err := CLI{}.AgentGet("w1:p2")
			if err != nil {
				t.Fatalf("AgentGet: %v", err)
			}
			if got.Status != tc.wantStatus || got.Session != tc.wantSession {
				t.Errorf("status=%q session=%q, want %q and %q", got.Status, got.Session, tc.wantStatus, tc.wantSession)
			}
		})
	}
}

func TestCLI_AgentGet_UnknownTargetIsNotFound(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `echo '{"error":{"code":"agent_not_found","message":"agent target x not found"}}' >&2; exit 1`))
	_, err := CLI{}.AgentGet("x")
	if !IsNotFound(err) {
		t.Fatalf("AgentGet error = %v, want agent_not_found", err)
	}
}

func TestCLI_AgentList_ReadsEveryAgent(t *testing.T) {
	t.Setenv("PATH", herdrStub(t, `case "$1 $2" in
  "agent list") echo '{"result":{"agents":[{"agent":"claude","pane_id":"w1:p1","cwd":"/a"},{"agent":"pi","pane_id":"w1:p2","name":"vx-x","cwd":"/b"}],"type":"agent_list"}}' ;;
esac`))

	got, err := CLI{}.AgentList()
	if err != nil {
		t.Fatalf("AgentList: %v", err)
	}
	want := []AgentInfo{{Kind: "claude", PaneID: "w1:p1", Cwd: "/a"}, {Kind: "pi", PaneID: "w1:p2", Name: "vx-x", Cwd: "/b"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("AgentList = %+v, want %+v", got, want)
	}
}
