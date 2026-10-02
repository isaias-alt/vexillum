package forum_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

const agentToken = "agent-token-for-tests"

type testEnv struct {
	t    *testing.T
	home string
	hub  *forum.Hub
	ts   *httptest.Server
	file string
}

func newEnv(t *testing.T, grace time.Duration) *testEnv {
	t.Helper()
	home := t.TempDir()
	dir := t.TempDir()
	file := filepath.Join(dir, "artifact.html")
	writeFile(t, file, "<html><body><p>hello</p></body></html>")

	hub := forum.NewHub(home, forum.HubOptions{BrowserGrace: grace})
	env := &testEnv{t: t, home: home, hub: hub, file: file}
	var ts *httptest.Server
	ts = httptest.NewUnstartedServer(nil)
	srv := forum.NewServer(forum.ServerOptions{
		Hub:        hub,
		Store:      forum.NewStore(home),
		AgentToken: agentToken,
		Addr:       ts.Listener.Addr().String(),
		Shutdown:   func() {},
	})
	ts.Config.Handler = srv
	ts.Start()
	t.Cleanup(ts.Close)
	env.ts = ts
	return env
}

func (e *testEnv) agent(method, path string, body any) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+agentToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func (e *testEnv) open() forum.OpenResponse {
	e.t.Helper()
	resp, data := e.agent("POST", "/api/agent/open", map[string]any{"file": e.file})
	if resp.StatusCode != 200 {
		e.t.Fatalf("open = %d %s", resp.StatusCode, data)
	}
	var out forum.OpenResponse
	if err := json.Unmarshal(data, &out); err != nil {
		e.t.Fatalf("decoding open: %v", err)
	}
	return out
}

func TestAgentAPI_OpenReturnsSessionURL(t *testing.T) {
	env := newEnv(t, time.Minute)
	out := env.open()
	if out.Key != forum.SessionKey(env.file) {
		t.Errorf("key = %q, want the path-derived key", out.Key)
	}
	if want := "http://" + env.ts.Listener.Addr().String() + "/session/" + out.Key; out.URL != want {
		t.Errorf("url = %q, want %q", out.URL, want)
	}
	if out.Status != forum.StatusOpen || !out.Created {
		t.Errorf("open = %+v", out)
	}
	if again := env.open(); again.Created {
		t.Error("second open should resume, not create")
	}
}

func TestAgentAPI_RequiresAgentToken(t *testing.T) {
	env := newEnv(t, time.Minute)
	for _, header := range []string{"", "Bearer wrong"} {
		req, _ := http.NewRequest("POST", env.ts.URL+"/api/agent/open", strings.NewReader(`{"file":"`+env.file+`"}`))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q -> %d, want 401", header, resp.StatusCode)
		}
	}
}

func TestAgentAPI_RejectsBrowserOriginEvenWithToken(t *testing.T) {
	env := newEnv(t, time.Minute)
	req, _ := http.NewRequest("POST", env.ts.URL+"/api/agent/open", strings.NewReader(`{"file":"`+env.file+`"}`))
	req.Header.Set("Authorization", "Bearer "+agentToken)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServer_RejectsNonLoopbackHost(t *testing.T) {
	env := newEnv(t, time.Minute)
	req, _ := http.NewRequest("GET", env.ts.URL+"/healthz", nil)
	req.Host = "rebind.attacker.example:8080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a rebinding Host", resp.StatusCode)
	}
}

func TestAgentAPI_RelativeFileRejected(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, _ := env.agent("POST", "/api/agent/open", map[string]any{"file": "relative.html"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAgentAPI_PollDeliversQueuedAndSentPrompt_StripsQueueKey(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	if _, err := env.hub.QueuePrompt(open.Key, forum.PromptInput{Prompt: "pick B", Tag: "choice", QueueKey: "q"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.hub.Send(open.Key, false); err != nil {
		t.Fatal(err)
	}
	resp, data := env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 2000})
	if resp.StatusCode != 200 {
		t.Fatalf("poll = %d %s", resp.StatusCode, data)
	}
	if strings.Contains(string(data), "queue_key") {
		t.Errorf("poll body leaks the browser-only queue key: %s", data)
	}
	var out forum.PollResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != forum.PollFeedback || len(out.Prompts) != 1 || out.Prompts[0].Prompt != "pick B" {
		t.Errorf("poll = %+v", out)
	}
}

func TestAgentAPI_PollBlocksThenWakesOnSend(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	keepBrowser(t, env.hub, open.Key)

	done := make(chan forum.PollResponse, 1)
	go func() {
		_, data := env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file})
		var out forum.PollResponse
		_ = json.Unmarshal(data, &out)
		done <- out
	}()
	select {
	case <-done:
		t.Fatal("poll returned with nothing queued")
	case <-time.After(100 * time.Millisecond):
	}
	env.hub.QueuePrompt(open.Key, forum.PromptInput{Prompt: "wake up"})
	env.hub.Send(open.Key, false)
	select {
	case out := <-done:
		if len(out.Prompts) != 1 {
			t.Errorf("poll = %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not wake")
	}
}

func TestAgentAPI_PollOverHTTPReportsBrowserDisconnected(t *testing.T) {
	env := newEnv(t, 80*time.Millisecond)
	env.open()
	_, data := env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file})
	var out forum.PollResponse
	_ = json.Unmarshal(data, &out)
	if out.Status != forum.PollBrowserDisconnected {
		t.Errorf("status = %q, want browser_disconnected (%s)", out.Status, data)
	}
}

func TestAgentAPI_PollUnknownSession404(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, data := env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 50})
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(data), "no_session") {
		t.Errorf("poll = %d %s, want 404 no_session", resp.StatusCode, data)
	}
}

func TestAgentAPI_ReplyShowsInTranscript(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	resp, data := env.agent("POST", "/api/agent/reply", map[string]any{"file": env.file, "text": "Done: **B** applied"})
	if resp.StatusCode != 200 {
		t.Fatalf("reply = %d %s", resp.StatusCode, data)
	}
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Transcript) != 1 || snap.Transcript[0].Text != "Done: **B** applied" || snap.Transcript[0].Role != forum.RoleAgent {
		t.Errorf("transcript = %+v", snap.Transcript)
	}
}

func TestAgentAPI_EndThenReplyConflicts(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.open()
	if resp, data := env.agent("POST", "/api/agent/end", map[string]any{"file": env.file}); resp.StatusCode != 200 {
		t.Fatalf("end = %d %s", resp.StatusCode, data)
	}
	resp, data := env.agent("POST", "/api/agent/reply", map[string]any{"file": env.file, "text": "late"})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"ended"`) {
		t.Errorf("reply after end = %d %s, want 409 ended", resp.StatusCode, data)
	}
	// A plain reopen revives an agent-ended session.
	if out := env.open(); out.Status != forum.StatusOpen {
		t.Errorf("reopen = %+v", out)
	}
}

func TestAgentAPI_UserEndedRefusesPlainReopen(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.hub.End(open.Key, forum.EndedByUser)
	if out := env.open(); out.Status != forum.OpenUserEnded {
		t.Fatalf("open after user end = %+v, want user_ended", out)
	}
	_, data := env.agent("POST", "/api/agent/open", map[string]any{"file": env.file, "reopen": true})
	var out forum.OpenResponse
	_ = json.Unmarshal(data, &out)
	if out.Status != forum.StatusOpen {
		t.Errorf("reopen = %+v", out)
	}
}

func TestFormatPoll_StableTextFormat(t *testing.T) {
	res := forum.PollResponse{
		Session: "0123456789abcdef",
		File:    "/tmp/a.html",
		Status:  forum.PollFeedback,
		Prompts: []forum.Prompt{
			{UID: "pr_1", Tag: "choice", Prompt: "Use the Pro plan", Selector: "form > input", Text: "Plan: Pro"},
			{UID: "pr_2", Tag: "feedback", Prompt: "line one\nline two\n\nline four"},
		},
	}
	got := forum.FormatPoll("/tmp/a.html", res)
	want := `session: 0123456789abcdef
file: /tmp/a.html
status: feedback
prompts[2]:
  - uid: pr_1
    tag: choice
    prompt: Use the Pro plan
    selector: form > input
    text: Plan: Pro
  - uid: pr_2
    tag: feedback
    prompt: |
      line one
      line two

      line four
next_step: Apply this feedback, then run ` + "`vx forum poll /tmp/a.html --reply \"<what you did>\"`" + ` to answer in the browser and keep waiting for more.
`
	if got != want {
		t.Errorf("FormatPoll mismatch.\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatPoll_EmptyStatuses(t *testing.T) {
	cases := map[string]string{
		forum.PollEnded:               "ended",
		forum.PollBrowserDisconnected: "ask the user",
		forum.PollTimeout:             "again",
	}
	for status, needle := range cases {
		out := forum.FormatPoll("/tmp/a.html", forum.PollResponse{Session: "k", Status: status})
		if !strings.Contains(out, "prompts[0]:\n") || !strings.Contains(out, "status: "+status+"\n") || !strings.Contains(strings.ToLower(out), strings.ToLower(needle)) {
			t.Errorf("status %s output unexpected:\n%s", status, out)
		}
	}
}

// POST /api/agent/poll with all covers every open session and names the file;
// the delivery stays leased until POST /api/agent/ack confirms it.
func TestAgentAPI_PollAllLeasesUntilAcknowledged(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	other := filepath.Join(t.TempDir(), "other.html")
	writeFile(t, other, "<p>other</p>")
	if resp, data := env.agent("POST", "/api/agent/open", map[string]any{"file": other}); resp.StatusCode != 200 {
		t.Fatalf("open other = %d %s", resp.StatusCode, data)
	}
	env.browser("POST", "/api/s/"+open.Key+"/queue", open.Key, map[string]any{"prompt": "tweak it"})
	env.browser("POST", "/api/s/"+open.Key+"/send", open.Key, map[string]any{})

	poll := func() forum.PollResponse {
		t.Helper()
		_, data := env.agent("POST", "/api/agent/poll", map[string]any{"all": true, "timeout_ms": 300})
		var out forum.PollResponse
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("decoding %s: %v", data, err)
		}
		return out
	}
	first := poll()
	if !first.All || first.Status != forum.PollFeedback || first.File != env.file || first.Session != open.Key || len(first.Prompts) != 1 || first.Delivery == "" {
		t.Fatalf("first = %+v", first)
	}
	// The client never confirmed (it died): the same prompt comes back, flagged.
	second := poll()
	if len(second.Prompts) != 1 || !second.Prompts[0].Redelivered || second.Prompts[0].UID != first.Prompts[0].UID {
		t.Fatalf("second = %+v, want the same prompt redelivered", second)
	}
	resp, data := env.agent("POST", "/api/agent/ack", map[string]any{"file": env.file, "delivery": second.Delivery})
	if resp.StatusCode != 200 {
		t.Fatalf("ack = %d %s", resp.StatusCode, data)
	}
	if third := poll(); third.Status != forum.PollTimeout || len(third.Prompts) != 0 {
		t.Errorf("after the ack = %+v, want a timeout", third)
	}
	if resp, _ := env.agent("POST", "/api/agent/ack", map[string]any{"delivery": "x"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ack without a file = %d, want 400", resp.StatusCode)
	}
}

func TestFormatPoll_MultiplexedResult(t *testing.T) {
	res := forum.PollResponse{
		Session: "abc", File: "/x/a b.html", Status: forum.PollFeedback, All: true, OtherPending: 2,
		Prompts: []forum.Prompt{{UID: "pr_1", Tag: "message", Prompt: "hi", Redelivered: true}},
	}
	out := forum.FormatPoll("", res)
	for _, want := range []string{
		"session: abc\n", "file: /x/a b.html\n", "status: feedback\n", "other_sessions_pending: 2\n",
		"  - uid: pr_1\n    redelivered: true\n    tag: message\n",
		"`vx forum poll --all --reply-to '/x/a b.html' --reply \"<what you did>\"`",
		"2 other session(s)", "skip any uid you already applied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	none := forum.FormatPoll("", forum.PollResponse{Status: forum.PollNoSessions, All: true})
	if strings.Contains(none, "session:") || strings.Contains(none, "file:") || !strings.Contains(none, "status: no_sessions\nprompts[0]:\n") {
		t.Errorf("a result about no session must not name one:\n%s", none)
	}
	// The per-file format is unchanged: no redelivery noise on a first delivery.
	single := forum.FormatPoll("/x/a.html", forum.PollResponse{Session: "abc", Status: forum.PollTimeout})
	if strings.Contains(single, "other_sessions_pending") || !strings.Contains(single, "vx forum poll /x/a.html") {
		t.Errorf("single poll output:\n%s", single)
	}
}
