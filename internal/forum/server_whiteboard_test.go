package forum_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func TestArtifact_MermaidAddsWhiteboardEmbed(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<html><head></head><body><div class="mermaid">graph TD; A-->B</div></body></html>`)
	_, body := env.get("/a/" + open.Key + "/artifact.html")
	if !strings.Contains(body, `<script src="/whiteboard-embed.js"></script>`+"\n</body>") {
		t.Errorf("embed not injected before </body>:\n%s", body)
	}
	if !strings.Contains(body, `<div class="mermaid">`) {
		t.Error("the original container must survive injection")
	}
}

func TestWhiteboardAPI_RoundTripRequiresToken(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	_, data := env.browser("GET", base+"/mermaid-sources", open.Key, nil)
	var sources struct {
		Sources []forum.MermaidSource `json:"sources"`
	}
	_ = json.Unmarshal(data, &sources)
	if len(sources.Sources) != 1 || sources.Sources[0].Source != "graph TD; A-->B" {
		t.Errorf("sources = %s", data)
	}

	_, data = env.browser("GET", base+"/whiteboard/0", open.Key, nil)
	if !strings.Contains(string(data), `"whiteboard":null`) {
		t.Errorf("before any save: %s", data)
	}
	put := map[string]any{"source_hash": "abc", "text_metrics_version": 1, "scene": map[string]any{"elements": []any{map[string]any{"id": "x"}}}, "baseline": map[string]any{"elements": []any{}}}
	if resp, data := env.browser("PUT", base+"/whiteboard/0", open.Key, put); resp.StatusCode != 200 {
		t.Fatalf("PUT = %d %s", resp.StatusCode, data)
	}
	_, data = env.browser("GET", base+"/whiteboard/0", open.Key, nil)
	var got struct {
		Whiteboard *forum.SavedScene `json:"whiteboard"`
	}
	_ = json.Unmarshal(data, &got)
	if got.Whiteboard == nil || got.Whiteboard.SourceHash != "abc" {
		t.Errorf("after save: %s", data)
	}

	// No token, no whiteboard access.
	resp, _ := env.browserWith("PUT", base+"/whiteboard/0", put, func(r *http.Request) { r.Header.Set("Origin", env.ts.URL) })
	if resp.StatusCode != 401 {
		t.Errorf("PUT without a token = %d, want 401", resp.StatusCode)
	}
	if resp, _ := env.browser("GET", base+"/whiteboard/abc", open.Key, nil); resp.StatusCode != 400 {
		t.Errorf("bad index = %d, want 400", resp.StatusCode)
	}
}

func TestWhiteboardFeedback_WritesFilesAndQueuesWhiteboardPrompt(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div><div class="mermaid">graph TD; C-->D</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	// "iVBORw0KGgo=" is the PNG magic; content is irrelevant to the server.
	body := map[string]any{
		"scene":        map[string]any{"elements": []any{map[string]any{"id": "x"}}},
		"pngDataUrl":   "data:image/png;base64,iVBORw0KGgo=",
		"summaryLines": []string{"Added node E", "Moved node B", ""},
	}
	resp, data := env.browser("POST", base+"/whiteboard/1/feedback-files", open.Key, body)
	if resp.StatusCode != 200 {
		t.Fatalf("feedback = %d %s", resp.StatusCode, data)
	}
	var out struct {
		ScenePath   string `json:"scene_path"`
		PreviewPath string `json:"preview_path"`
	}
	_ = json.Unmarshal(data, &out)
	for _, p := range []string{out.ScenePath, out.PreviewPath} {
		if _, err := os.Stat(p); p == "" || err != nil {
			t.Errorf("feedback file %q not written: %v", p, err)
		}
	}
	if !strings.HasSuffix(out.ScenePath, ".excalidraw") || !strings.HasSuffix(out.PreviewPath, ".png") {
		t.Errorf("paths = %q, %q", out.ScenePath, out.PreviewPath)
	}

	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 1 {
		t.Fatalf("queued = %+v, want the whiteboard prompt", snap.Queued)
	}
	p := snap.Queued[0]
	if p.Tag != "whiteboard" {
		t.Errorf("tag = %q", p.Tag)
	}
	for _, want := range []string{"diagram 2 of 2", "- Added node E", "- Moved node B", out.ScenePath, out.PreviewPath, "Mermaid source"} {
		if !strings.Contains(p.Prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, p.Prompt)
		}
	}
	if strings.Contains(p.Prompt, "\n- \n") {
		t.Error("blank summary lines must be dropped")
	}

	// Queueing the same diagram again replaces the unsent prompt.
	body["summaryLines"] = []string{"Second pass"}
	if resp, data := env.browser("POST", base+"/whiteboard/1/feedback-files", open.Key, body); resp.StatusCode != 200 {
		t.Fatalf("second feedback = %d %s", resp.StatusCode, data)
	}
	snap, _ = env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 1 || !strings.Contains(snap.Queued[0].Prompt, "Second pass") {
		t.Errorf("queue after a second queue of the same diagram = %+v", snap.Queued)
	}

	// And it reaches the agent through the normal send + poll.
	env.browser("POST", base+"/send", open.Key, map[string]any{})
	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 2000})
	var polled forum.PollResponse
	_ = json.Unmarshal(data, &polled)
	if len(polled.Prompts) != 1 || polled.Prompts[0].Tag != "whiteboard" || !strings.Contains(string(polled.Prompts[0].Target), "scenePath") {
		t.Errorf("poll = %s", data)
	}
}

func TestWhiteboardFeedback_BoundsSummaryAndRefusesWhenEnded(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	lines := make([]string, 120)
	for i := range lines {
		lines[i] = strings.Repeat("x", 900)
	}
	resp, data := env.browser("POST", base+"/whiteboard/0/feedback-files", open.Key, map[string]any{"scene": map[string]any{}, "summaryLines": lines})
	if resp.StatusCode != 200 {
		t.Fatalf("feedback = %d %s", resp.StatusCode, data)
	}
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if got := strings.Count(snap.Queued[0].Prompt, "\n- "); got != 50 {
		t.Errorf("%d summary lines kept, want the 50-line cap", got)
	}
	if len(snap.Queued[0].Prompt) > 50*(300+3)+1500 {
		t.Errorf("prompt is %d bytes, summary not bounded", len(snap.Queued[0].Prompt))
	}

	env.hub.End(open.Key, forum.EndedByUser)
	if resp, _ := env.browser("POST", base+"/whiteboard/0/feedback-files", open.Key, map[string]any{"scene": map[string]any{}}); resp.StatusCode != http.StatusConflict {
		t.Errorf("feedback to an ended session = %d, want 409", resp.StatusCode)
	}
}

func TestWhiteboardStaticRoutes(t *testing.T) {
	env := newEnv(t, time.Minute)
	if resp, body := env.get("/whiteboard-frame?diagramIndex=0"); resp.StatusCode != 200 || !strings.Contains(body, "/whiteboard-assets/whiteboard.js") {
		t.Errorf("frame = %d", resp.StatusCode)
	}
	if resp, body := env.get("/whiteboard-embed.js"); resp.StatusCode != 200 || !strings.Contains(body, "vx-whiteboard:") || strings.Contains(body, "fetch(") {
		t.Errorf("embed = %d (must go through the chrome, not fetch)", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", env.ts.URL+"/whiteboard-assets/whiteboard.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("whiteboard.js headers = %v", resp.Header)
	}
	if resp, _ := env.get("/whiteboard-assets/does-not-exist.js"); resp.StatusCode != 404 {
		t.Errorf("unknown asset = %d", resp.StatusCode)
	}
	if resp, _ := env.get("/whiteboard-assets/css/../../x"); resp.StatusCode == 200 {
		t.Error("traversal served something")
	}
}

// The frame bundle must talk to its direct parent, the artifact page that
// holds the channel (whiteboard-embed.js). Upstream nests the frame one level
// deeper and posts to window.top, which here is the forum chrome: the "ready"
// message went to a window that ignores it, init never came back, and every
// diagram stayed a blank box.
func TestWhiteboardBundle_TalksToItsParentNotTheTop(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, js := env.get("/whiteboard-assets/whiteboard.js")
	if strings.Contains(js, "window.top.postMessage") || strings.Contains(js, "!==window.top)return") {
		t.Error("the whiteboard frame addresses window.top (the forum chrome) instead of its embedder")
	}
	if !strings.Contains(js, "window.parent.postMessage") {
		t.Error("the whiteboard frame never posts to window.parent")
	}
}
