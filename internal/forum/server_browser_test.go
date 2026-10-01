package forum_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// browser issues a request the way the session page's own fetch would: with
// its own Origin and the session token.
func (e *testEnv) browser(method, path, key string, body any) (*http.Response, []byte) {
	e.t.Helper()
	return e.browserWith(method, path, body, func(req *http.Request) {
		token, err := e.hub.Token(key)
		if err != nil {
			e.t.Fatalf("Token: %v", err)
		}
		req.Header.Set("X-Forum-Token", token)
		req.Header.Set("Origin", e.ts.URL)
	})
}

func (e *testEnv) browserWith(method, path string, body any, mutate func(*http.Request)) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	mutate(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestBrowserAPI_QueueSendAndAgentPollEndToEnd(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key

	resp, data := env.browser("POST", base+"/queue", open.Key, map[string]any{
		"prompt": "Use the Pro plan", "tag": "choice", "selector": "form#plan", "text": "Plan: Pro",
		"target": map[string]any{"row": 2}, "queue_key": "question:plan",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("queue = %d %s", resp.StatusCode, data)
	}
	if strings.Contains(string(data), "queue_key") {
		t.Errorf("queue response should not echo the browser-only key: %s", data)
	}

	resp, data = env.browser("GET", base+"/state", open.Key, nil)
	var snap forum.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil || resp.StatusCode != 200 {
		t.Fatalf("state = %d %s (%v)", resp.StatusCode, data, err)
	}
	if len(snap.Queued) != 1 || snap.Queued[0].Prompt != "Use the Pro plan" {
		t.Errorf("queued = %+v", snap.Queued)
	}

	if resp, data = env.browser("POST", base+"/send", open.Key, map[string]any{}); resp.StatusCode != 200 {
		t.Fatalf("send = %d %s", resp.StatusCode, data)
	}

	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 2000})
	var polled forum.PollResponse
	if err := json.Unmarshal(data, &polled); err != nil {
		t.Fatal(err)
	}
	if polled.Status != forum.PollFeedback || len(polled.Prompts) != 1 {
		t.Fatalf("poll = %s", data)
	}
	p := polled.Prompts[0]
	if p.Prompt != "Use the Pro plan" || p.Tag != "choice" || p.Selector != "form#plan" || p.Text != "Plan: Pro" || string(p.Target) != `{"row":2}` {
		t.Errorf("delivered prompt = %+v", p)
	}
}

func TestBrowserAPI_SendAndEnd_FinalFeedbackOnceThenEnded(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key
	env.browser("POST", base+"/queue", open.Key, map[string]any{"prompt": "final word"})
	resp, data := env.browser("POST", base+"/send", open.Key, map[string]any{"end": true})
	if resp.StatusCode != 200 {
		t.Fatalf("send&end = %d %s", resp.StatusCode, data)
	}
	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file})
	var first forum.PollResponse
	_ = json.Unmarshal(data, &first)
	if first.Status != forum.PollEnded || first.EndedBy != "user" || len(first.Prompts) != 1 {
		t.Fatalf("first poll = %s", data)
	}
	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file})
	var second forum.PollResponse
	_ = json.Unmarshal(data, &second)
	if second.Status != forum.PollEnded || len(second.Prompts) != 0 {
		t.Errorf("second poll = %s, want the final feedback delivered only once", data)
	}
	// The ended session no longer accepts prompts.
	resp, data = env.browser("POST", base+"/queue", open.Key, map[string]any{"prompt": "too late"})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"ended"`) {
		t.Errorf("queue after end = %d %s, want 409 ended", resp.StatusCode, data)
	}
}

func TestBrowserAPI_RemoveQueuedAndNothingToSend(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key
	_, data := env.browser("POST", base+"/queue", open.Key, map[string]any{"prompt": "oops"})
	var queued struct {
		Prompt forum.Prompt `json:"prompt"`
	}
	_ = json.Unmarshal(data, &queued)
	if resp, data := env.browser("DELETE", base+"/queue/"+queued.Prompt.UID, open.Key, nil); resp.StatusCode != 200 {
		t.Fatalf("remove = %d %s", resp.StatusCode, data)
	}
	if resp, _ := env.browser("DELETE", base+"/queue/"+queued.Prompt.UID, open.Key, nil); resp.StatusCode != 404 {
		t.Errorf("second remove = %d, want 404", resp.StatusCode)
	}
	if resp, _ := env.browser("POST", base+"/send", open.Key, map[string]any{}); resp.StatusCode != 400 {
		t.Errorf("send of an empty queue = %d, want 400", resp.StatusCode)
	}
	if resp, _ := env.browser("POST", base+"/queue", open.Key, map[string]any{"prompt": "  "}); resp.StatusCode != 400 {
		t.Errorf("blank prompt = %d, want 400", resp.StatusCode)
	}
}

func TestBrowserAPI_BrowserEndIsUserEnded(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	if resp, data := env.browser("POST", "/api/s/"+open.Key+"/end", open.Key, nil); resp.StatusCode != 200 {
		t.Fatalf("end = %d %s", resp.StatusCode, data)
	}
	if out := env.open(); out.Status != forum.OpenUserEnded {
		t.Errorf("open after a browser end = %+v, want user_ended", out)
	}
}

func TestBrowserAPI_SecurityMatrix(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	other := forum.SessionKey("/some/other/artifact.html")
	_, _ = env.hub.Open("/some/other/artifact.html", false)
	otherToken, _ := env.hub.Token(other)
	token, _ := env.hub.Token(open.Key)
	path := "/api/s/" + open.Key + "/queue"
	body := map[string]any{"prompt": "injected instruction"}

	cases := []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"valid", func(r *http.Request) { r.Header.Set("X-Forum-Token", token); r.Header.Set("Origin", env.ts.URL) }, 200},
		{"valid via Sec-Fetch-Site", func(r *http.Request) {
			r.Header.Set("X-Forum-Token", token)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		}, 200},
		{"no token", func(r *http.Request) { r.Header.Set("Origin", env.ts.URL) }, 401},
		{"wrong token", func(r *http.Request) { r.Header.Set("X-Forum-Token", "nope"); r.Header.Set("Origin", env.ts.URL) }, 401},
		{"other session's token", func(r *http.Request) { r.Header.Set("X-Forum-Token", otherToken); r.Header.Set("Origin", env.ts.URL) }, 401},
		{"agent token is not a session token", func(r *http.Request) { r.Header.Set("X-Forum-Token", agentToken); r.Header.Set("Origin", env.ts.URL) }, 401},
		{"foreign Origin with a valid token", func(r *http.Request) {
			r.Header.Set("X-Forum-Token", token)
			r.Header.Set("Origin", "http://evil.example")
		}, 403},
		{"opaque null Origin", func(r *http.Request) { r.Header.Set("X-Forum-Token", token); r.Header.Set("Origin", "null") }, 403},
		{"cross-site fetch metadata", func(r *http.Request) {
			r.Header.Set("X-Forum-Token", token)
			r.Header.Set("Origin", env.ts.URL)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}, 403},
		{"POST without any proof of origin", func(r *http.Request) { r.Header.Set("X-Forum-Token", token) }, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := env.browserWith("POST", path, body, tc.mutate)
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d (%s)", resp.StatusCode, tc.want, data)
			}
		})
	}

	// Only the one valid case per variant may have queued anything.
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 2 {
		t.Errorf("%d prompts were queued, want exactly the 2 valid requests", len(snap.Queued))
	}
}

func TestBrowserAPI_UnknownSessionAndMalformedKey(t *testing.T) {
	env := newEnv(t, time.Minute)
	for _, key := range []string{"0123456789abcdef", "not-a-key", "..%2f..%2fetc"} {
		resp, _ := env.browserWith("GET", "/api/s/"+key+"/state", nil, func(r *http.Request) { r.Header.Set("X-Forum-Token", "x") })
		if resp.StatusCode != 404 {
			t.Errorf("key %q -> %d, want 404", key, resp.StatusCode)
		}
	}
}

func TestBrowserAPI_StateLongPollWakesOnQueue(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key
	_, data := env.browser("GET", base+"/state", open.Key, nil)
	var first forum.Snapshot
	_ = json.Unmarshal(data, &first)

	got := make(chan forum.Snapshot, 1)
	go func() {
		_, data := env.browser("GET", base+"/state?since="+strconv.FormatInt(first.Version, 10), open.Key, nil)
		var snap forum.Snapshot
		_ = json.Unmarshal(data, &snap)
		got <- snap
	}()
	select {
	case <-got:
		t.Fatal("state long-poll returned before anything changed")
	case <-time.After(100 * time.Millisecond):
	}
	env.browser("POST", base+"/queue", open.Key, map[string]any{"prompt": "live"})
	select {
	case snap := <-got:
		if len(snap.Queued) != 1 {
			t.Errorf("snapshot = %+v", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("state long-poll did not wake")
	}
}

func TestBrowserAPI_ListeningIndicatorReflectsAgentPoll(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key
	_, data := env.browser("GET", base+"/state", open.Key, nil)
	var snap forum.Snapshot
	_ = json.Unmarshal(data, &snap)
	if snap.Listening {
		t.Fatal("listening with no poll")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = env.hub.Poll(ctx, open.Key, 0) }()
	time.Sleep(60 * time.Millisecond)
	_, data = env.browser("GET", base+"/state", open.Key, nil)
	_ = json.Unmarshal(data, &snap)
	if !snap.Listening {
		t.Error("not listening during an agent poll")
	}
}
