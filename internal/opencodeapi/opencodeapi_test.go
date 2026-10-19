package opencodeapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub writes an opencode executable that runs script, recording each call's
// arguments one per line in the returned log file, and returns its path.
func stub(t *testing.T, script string) (binary, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	binary = filepath.Join(dir, "opencode")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n" + script + "\n"
	if err := os.WriteFile(binary, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

const running = `if [ "$1 $2" = "service status" ]; then echo http://127.0.0.1:49778; exit 0; fi`

func TestServiceRunning(t *testing.T) {
	for name, tc := range map[string]struct {
		script  string
		want    bool
		wantErr string
	}{
		"url":     {`echo http://127.0.0.1:49778`, true, ""},
		"stopped": {`echo stopped`, false, ""},
		"odd":     {`echo what`, false, "unexpected answer"},
		"fails":   {`echo boom >&2; exit 3`, false, "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			bin, _ := stub(t, tc.script)
			got, err := CLI{Binary: bin}.ServiceRunning()
			if got != tc.want || (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("ServiceRunning = (%v, %v), want (%v, %q)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestInfo_ParsesTheServiceAnswer(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"version":"2.0.32","pid":123,"urls":["http://127.0.0.1:49778"],"capabilities":{}}'`)
	info, err := CLI{Binary: bin}.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "2.0.32" || info.PID != 123 || len(info.URLs) != 1 {
		t.Errorf("info = %+v", info)
	}
	if got := calls(t, log); len(got) != 2 || got[1] != "api GET /api/info" {
		t.Errorf("calls = %q", got)
	}
}

func TestPrompt_PostsTheTextQueuedToTheSession(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"ok":true}'`)
	text := "A vexillum soldier's status changed:\n- mission t1: running -> done\n\"quoted\" $(not run) `nor this`"
	if err := (CLI{Binary: bin}).Prompt("ses_abc123", text); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if len(got) < 2 || !strings.HasPrefix(got[1], "api POST /api/session/ses_abc123/prompt -d ") {
		t.Fatalf("calls = %q", got)
	}
	// The body is one JSON argument; the multi-line text is in the log across
	// lines, so read it back from the call as a whole.
	raw, _ := os.ReadFile(log)
	_, body, _ := strings.Cut(string(raw), "api POST /api/session/ses_abc123/prompt -d ")
	var sent struct {
		Text     string `json:"text"`
		Delivery string `json:"delivery"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &sent); err != nil {
		t.Fatalf("body %q is not JSON: %v", body, err)
	}
	if sent.Text != text || sent.Delivery != "queue" {
		t.Errorf("sent %+v", sent)
	}
}

func TestPrompt_NeverStartsAService(t *testing.T) {
	bin, log := stub(t, `echo stopped`)
	err := CLI{Binary: bin}.Prompt("ses_abc", "x")
	if !errors.Is(err, ErrServiceStopped) {
		t.Fatalf("Prompt = %v, want ErrServiceStopped", err)
	}
	for _, c := range calls(t, log) {
		if strings.HasPrefix(c, "api ") {
			t.Errorf("opencode api was run against a stopped service: %q", c)
		}
	}
}

func TestPrompt_HTTPErrorsKeepTheirStatus(t *testing.T) {
	bin, _ := stub(t, running+`
echo '{"_tag":"SessionNotFoundError","message":"Session not found: ses_x"}'
echo 'HTTP 404 Not Found' >&2
exit 1`)
	err := CLI{Binary: bin}.Prompt("ses_x", "hi")
	var h *HTTPError
	if !errors.As(err, &h) || h.Status != 404 || !strings.Contains(h.Body, "SessionNotFoundError") {
		t.Fatalf("Prompt = %v", err)
	}
	if !IsSessionNotFound(err) {
		t.Error("a 404 is a session that is not known")
	}
	if IsSessionNotFound(&HTTPError{Status: 409}) {
		t.Error("a 409 is not a missing session")
	}
}

func TestPrompt_AFailureWithoutAStatusIsAPlainError(t *testing.T) {
	// OpenCode 1.x has no "api" command: it exits non-zero with usage text.
	bin, _ := stub(t, running+`
echo 'unknown command: api' >&2
exit 1`)
	err := CLI{Binary: bin}.Prompt("ses_x", "hi")
	var h *HTTPError
	if err == nil || errors.As(err, &h) || !strings.Contains(err.Error(), "unknown command: api") {
		t.Fatalf("Prompt = %v", err)
	}
}

func TestPrompt_RefusesAnIdThatIsNotASession(t *testing.T) {
	bin, log := stub(t, running)
	for _, id := range []string{"", "abc", "ses_", "ses_a/../../x", "ses_a b", "ses_a?x=1"} {
		if err := (CLI{Binary: bin}).Prompt(id, "x"); err == nil {
			t.Errorf("session id %q was accepted", id)
		}
	}
	if got := calls(t, log); len(got) != 0 {
		t.Errorf("opencode ran for a bad id: %q", got)
	}
}

func TestRun_ATimeoutIsAnError(t *testing.T) {
	bin, _ := stub(t, `exec sleep 5`)
	start := time.Now()
	_, err := CLI{Binary: bin, Timeout: 100 * time.Millisecond}.ServiceRunning()
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the call was not cut short")
	}
}
