package forum_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// eventStream is one open tab: a client reading /events the way the chrome does.
type eventStream struct {
	t      *testing.T
	cancel context.CancelFunc
	events chan forum.Snapshot
	lines  chan string
	status int
}

func (e *testEnv) openStream(key string) *eventStream {
	e.t.Helper()
	token, _ := e.hub.Token(key)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+"/api/s/"+key+"/events", nil)
	req.Header.Set("X-Forum-Token", token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		e.t.Fatalf("events: %v", err)
	}
	s := &eventStream{t: e.t, cancel: cancel, events: make(chan forum.Snapshot, 64), lines: make(chan string, 256), status: resp.StatusCode}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		cancel()
		resp.Body.Close()
		return s
	}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			s.lines <- line
			if strings.HasPrefix(line, "data: ") {
				var snap forum.Snapshot
				if json.Unmarshal([]byte(line[6:]), &snap) == nil {
					s.events <- snap
				}
			}
		}
	}()
	e.t.Cleanup(cancel)
	return s
}

// next waits for the first snapshot satisfying ok (earlier ones are skipped).
func (s *eventStream) next(ok func(forum.Snapshot) bool) forum.Snapshot {
	s.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case snap := <-s.events:
			if ok(snap) {
				return snap
			}
		case <-deadline:
			s.t.Fatal("no matching event arrived")
			return forum.Snapshot{}
		}
	}
}

func TestEvents_TwoTabsSeeQueueTranscriptListeningAndEnd(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	tabA, tabB := env.openStream(key), env.openStream(key)

	// Each tab gets the current snapshot on connect.
	for _, tab := range []*eventStream{tabA, tabB} {
		first := tab.next(func(forum.Snapshot) bool { return true })
		if first.Key != key || first.Status != forum.StatusOpen || len(first.Queued) != 0 {
			t.Fatalf("first event = %+v", first)
		}
	}

	// Tab A queues; tab B sees it without reloading (and so does A).
	if resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "from tab A", "tag": "message"}); resp.StatusCode != 200 {
		t.Fatalf("queue = %d %s", resp.StatusCode, body)
	}
	for name, tab := range map[string]*eventStream{"A": tabA, "B": tabB} {
		got := tab.next(func(s forum.Snapshot) bool { return len(s.Queued) == 1 })
		if got.Queued[0].Prompt != "from tab A" {
			t.Errorf("tab %s queue = %+v", name, got.Queued)
		}
	}

	// The agent starts listening: both tabs flip to listening.
	pollDone := pollAsync(env.hub, key, 5*time.Second)
	for name, tab := range map[string]*eventStream{"A": tabA, "B": tabB} {
		if !tab.next(func(s forum.Snapshot) bool { return s.Listening }).Listening {
			t.Errorf("tab %s never saw the agent listening", name)
		}
	}

	// Send puts the message in the transcript and wakes the poll.
	env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{})
	waitResult(t, pollDone)
	for name, tab := range map[string]*eventStream{"A": tabA, "B": tabB} {
		got := tab.next(func(s forum.Snapshot) bool { return len(s.Transcript) == 1 })
		if got.Transcript[0].Text != "from tab A" || len(got.Queued) != 0 {
			t.Errorf("tab %s transcript = %+v", name, got.Transcript)
		}
		tab.next(func(s forum.Snapshot) bool { return !s.Listening })
	}

	// The agent replies, then ends the session: both tabs see both.
	if resp, _ := env.agent("POST", "/api/agent/reply", map[string]any{"file": env.file, "text": "done"}); resp.StatusCode != 200 {
		t.Fatal("reply failed")
	}
	for _, tab := range []*eventStream{tabA, tabB} {
		tab.next(func(s forum.Snapshot) bool { return len(s.Transcript) == 2 && s.Transcript[1].Role == forum.RoleAgent })
	}
	env.agent("POST", "/api/agent/end", map[string]any{"file": env.file})
	for name, tab := range map[string]*eventStream{"A": tabA, "B": tabB} {
		got := tab.next(func(s forum.Snapshot) bool { return s.Status == forum.StatusEnded })
		if got.EndedBy != forum.EndedByAgent {
			t.Errorf("tab %s ended_by = %q", name, got.EndedBy)
		}
	}
}

func TestEvents_ArtifactEditsAreAnnounced(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	tab := env.openStream(key)
	var first struct {
		V string `json:"artifact_version"`
	}
	line := <-tab.lines
	for !strings.HasPrefix(line, "data: ") {
		line = <-tab.lines
	}
	_ = json.Unmarshal([]byte(line[6:]), &first)
	time.Sleep(20 * time.Millisecond)
	env.setArtifact("<html><body><p>edited, and longer</p></body></html>")
	deadline := time.After(4 * time.Second)
	for {
		select {
		case line := <-tab.lines:
			var now struct {
				V string `json:"artifact_version"`
			}
			if strings.HasPrefix(line, "data: ") && json.Unmarshal([]byte(line[6:]), &now) == nil && now.V != first.V {
				return
			}
		case <-deadline:
			t.Fatal("the edited artifact was never announced")
		}
	}
}

func TestEvents_HeartbeatKeepsAnIdleStreamAlive(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "a.html")
	writeFile(t, file, "<p>x</p>")
	hub := forum.NewHub(home, forum.HubOptions{})
	ts := httptest.NewUnstartedServer(nil)
	ts.Config.Handler = forum.NewServer(forum.ServerOptions{Hub: hub, Store: forum.NewStore(home), AgentToken: agentToken, Addr: ts.Listener.Addr().String(), EventsHeartbeat: 50 * time.Millisecond})
	ts.Start()
	t.Cleanup(ts.Close)
	env := &testEnv{t: t, home: home, hub: hub, ts: ts, file: file}
	key := env.open().Key

	tab := env.openStream(key)
	deadline := time.After(3 * time.Second)
	pings := 0
	for pings < 2 {
		select {
		case line := <-tab.lines:
			if line == ": ping" {
				pings++
			}
		case <-deadline:
			t.Fatalf("only %d heartbeats arrived", pings)
		}
	}
}

func TestEvents_SameGuaranteesAsEveryBrowserRoute(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	path := "/api/s/" + key + "/events"
	token, _ := env.hub.Token(key)
	for name, mutate := range map[string]func(*http.Request){
		"no token":     func(r *http.Request) {},
		"wrong token":  func(r *http.Request) { r.Header.Set("X-Forum-Token", "nope") },
		"cross origin": func(r *http.Request) { r.Header.Set("X-Forum-Token", token); r.Header.Set("Origin", "http://evil.example") },
		"cross site":   func(r *http.Request) { r.Header.Set("X-Forum-Token", token); r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		if resp, _ := env.browserWith("GET", path, nil, mutate); resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Errorf("%s: status %d, want rejection", name, resp.StatusCode)
		}
	}
	if resp, _ := env.agent("GET", path, nil); resp.StatusCode == 200 {
		t.Error("the agent token must not open the browser stream")
	}
	bad := "/api/s/0123456789abcdef/events"
	if resp, _ := env.browserWith("GET", bad, nil, func(r *http.Request) { r.Header.Set("X-Forum-Token", token) }); resp.StatusCode != 404 {
		t.Errorf("unknown session = %d, want 404", resp.StatusCode)
	}
}

// An open stream counts as the browser being connected (the agent's poll does
// not report browser_disconnected), and closing it releases everything.
func TestEvents_StreamCountsAsBrowserAndLeaksNothingOnClose(t *testing.T) {
	env := newEnv(t, 80*time.Millisecond)
	key := env.open().Key

	tab := env.openStream(key)
	tab.next(func(forum.Snapshot) bool { return true })
	time.Sleep(200 * time.Millisecond) // past the grace period
	res, err := env.hub.Poll(context.Background(), key, 150*time.Millisecond)
	if err != nil || res.Status != forum.PollTimeout {
		t.Fatalf("poll with a stream open = %+v %v, want timeout (browser still connected)", res, err)
	}
	tab.cancel()
	time.Sleep(200 * time.Millisecond)
	res, err = env.hub.Poll(context.Background(), key, time.Second)
	if err != nil || res.Status != forum.PollBrowserDisconnected {
		t.Fatalf("poll after the stream closed = %+v %v, want browser_disconnected", res, err)
	}

	// Many connects and disconnects leave no goroutines behind.
	http.DefaultClient.CloseIdleConnections()
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 30; i++ {
		s := env.openStream(key)
		s.next(func(forum.Snapshot) bool { return true })
		s.cancel()
	}
	var after int
	for i := 0; i < 50; i++ {
		http.DefaultClient.CloseIdleConnections()
		time.Sleep(40 * time.Millisecond)
		if after = runtime.NumGoroutine(); after <= before+3 {
			return
		}
	}
	t.Fatalf("goroutines grew from %d to %d after 30 closed streams", before, after)
}
