package forum_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

func validRecord(digest string, elements ...any) map[string]any {
	if elements == nil {
		elements = []any{}
	}
	return map[string]any{
		"format":      2,
		"digest":      digest,
		"measure_gen": 1,
		"current":     map[string]any{"elements": elements, "appState": map[string]any{"theme": "dark", "scrollX": 4}, "files": map[string]any{}},
		"pristine":    map[string]any{"elements": []any{}},
	}
}

func TestBoardsAPI_ListReadWriteAndTokens(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div><div class="mermaid">pie
"a": 1</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	_, data := env.browser("GET", base+"/diagrams", open.Key, nil)
	var listing struct {
		Diagrams []struct {
			Ordinal int    `json:"ordinal"`
			Text    string `json:"text"`
			Digest  string `json:"digest"`
		} `json:"diagrams"`
	}
	if err := json.Unmarshal(data, &listing); err != nil || len(listing.Diagrams) != 2 {
		t.Fatalf("diagrams = %s (%v)", data, err)
	}
	if d := listing.Diagrams[1]; d.Ordinal != 1 || d.Text != "pie\n\"a\": 1" || d.Digest != forum.HashMermaidSource(d.Text) {
		t.Errorf("second diagram = %+v", d)
	}

	_, data = env.browser("GET", base+"/boards/0", open.Key, nil)
	if !strings.Contains(string(data), `"record":null`) {
		t.Errorf("before any save: %s", data)
	}
	if resp, data := env.browser("PUT", base+"/boards/0", open.Key, validRecord("abc", map[string]any{"id": "x"})); resp.StatusCode != 200 {
		t.Fatalf("PUT = %d %s", resp.StatusCode, data)
	}
	_, data = env.browser("GET", base+"/boards/0", open.Key, nil)
	var got struct {
		Record *forum.SavedScene `json:"record"`
	}
	_ = json.Unmarshal(data, &got)
	if got.Record == nil || got.Record.Digest != "abc" || got.Record.Format != 2 || got.Record.SavedAt.IsZero() {
		t.Fatalf("after save: %s", data)
	}
	if strings.Contains(string(got.Record.Current), "theme") || !strings.Contains(string(got.Record.Current), `"scrollX":4`) {
		t.Errorf("theme must be stripped and the rest kept: %s", got.Record.Current)
	}

	// No token, no access.
	resp, _ := env.browserWith("PUT", base+"/boards/0", validRecord("abc"), func(r *http.Request) { r.Header.Set("Origin", env.ts.URL) })
	if resp.StatusCode != 401 {
		t.Errorf("PUT without a token = %d, want 401", resp.StatusCode)
	}
	// The old routes are gone.
	for _, old := range []string{"/mermaid-sources", "/whiteboard/0"} {
		if resp, _ := env.browser("GET", base+old, open.Key, nil); resp.StatusCode == 200 {
			t.Errorf("GET %s still answers", old)
		}
	}
}

func TestBoardsAPI_InvalidOrdinalAndBadRecords(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	for _, ordinal := range []string{"abc", "-1", "1000", "1.5"} {
		for method, path := range map[string]string{"GET": "/boards/" + ordinal, "PUT": "/boards/" + ordinal, "POST": "/boards/" + ordinal + "/submit"} {
			var body any
			if method != "GET" {
				body = validRecord("d")
			}
			if resp, _ := env.browser(method, base+path, open.Key, body); resp.StatusCode != 400 && resp.StatusCode != 404 {
				t.Errorf("%s %s = %d, want 400", method, path, resp.StatusCode)
			}
		}
	}
	if resp, _ := env.browser("GET", base+"/boards/999", open.Key, nil); resp.StatusCode != 200 {
		t.Errorf("ordinal 999 = %d, want 200 (the upper bound is valid)", resp.StatusCode)
	}
	bad := validRecord("d")
	bad["format"] = 1
	if resp, _ := env.browser("PUT", base+"/boards/0", open.Key, bad); resp.StatusCode != 400 {
		t.Errorf("PUT of another record format = %d, want 400", resp.StatusCode)
	}
}

func TestBoardsAPI_AStoredRecordInAnotherFormatReadsAsNull(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	dir := filepath.Join(env.home, "forums", open.Key, "whiteboards")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"source_hash":"x","text_metrics_version":2,"updated_at":"2025-01-01T00:00:00Z","scene":{"elements":[{"id":"a"}]},"baseline":{"elements":[]}}`
	if err := os.WriteFile(filepath.Join(dir, "0.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, data := env.browser("GET", "/api/s/"+open.Key+"/boards/0", open.Key, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(data), `"record":null`) {
		t.Errorf("old-format record = %d %s, want 200 and a null record", resp.StatusCode, data)
	}
}

func TestBoardsAPI_OversizedBodiesAreRefused(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key
	huge := validRecord("d")
	huge["current"] = map[string]any{"elements": []any{}, "blob": strings.Repeat("x", 33<<20)}
	if resp, _ := env.browser("PUT", base+"/boards/0", open.Key, huge); resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized PUT = %d, want a refusal", resp.StatusCode)
	}
	if resp, _ := env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{"blob": strings.Repeat("x", 33<<20)}}); resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized submit = %d, want a refusal", resp.StatusCode)
	}
}

func TestBoardSubmit_WritesFilesAndQueuesWhiteboardPrompt(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div><div class="mermaid">graph TD; C-->D</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	// "iVBORw0KGgo=" is the PNG magic; content is irrelevant to the server.
	body := map[string]any{
		"current":    map[string]any{"elements": []any{map[string]any{"id": "x"}}, "appState": map[string]any{"theme": "dark"}},
		"png":        "data:image/png;base64,iVBORw0KGgo=",
		"edit_lines": []string{"Added node E", "Moved node B", ""},
		"remark":     "please keep the retry loop",
	}
	resp, data := env.browser("POST", base+"/boards/1/submit", open.Key, body)
	if resp.StatusCode != 200 {
		t.Fatalf("submit = %d %s", resp.StatusCode, data)
	}
	var out struct {
		ScenePath   string `json:"scene_path"`
		PreviewPath string `json:"preview_path"`
		PromptUID   string `json:"prompt_uid"`
	}
	_ = json.Unmarshal(data, &out)
	for _, p := range []string{out.ScenePath, out.PreviewPath} {
		if _, err := os.Stat(p); p == "" || err != nil {
			t.Errorf("feedback file %q not written: %v", p, err)
		}
	}
	if !strings.HasSuffix(out.ScenePath, "1.excalidraw") || !strings.HasSuffix(out.PreviewPath, "1.png") || out.PromptUID == "" {
		t.Errorf("response = %s", data)
	}
	if scene, _ := os.ReadFile(out.ScenePath); strings.Contains(string(scene), `"theme"`) {
		t.Errorf("the published scene still carries the theme: %s", scene)
	}

	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 1 {
		t.Fatalf("queued = %+v, want the whiteboard prompt", snap.Queued)
	}
	p := snap.Queued[0]
	if p.Tag != "whiteboard" || p.Text != "Whiteboard: diagram 2" || p.QueueKey != "whiteboard:1" {
		t.Errorf("prompt = tag %q text %q key %q", p.Tag, p.Text, p.QueueKey)
	}
	for _, want := range []string{"diagram 2 of 2", "- Added node E", "- Moved node B", "Reviewer remark: please keep the retry loop", out.ScenePath, out.PreviewPath, "Mermaid source"} {
		if !strings.Contains(p.Prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, p.Prompt)
		}
	}
	if strings.Contains(p.Prompt, "\n- \n") {
		t.Error("blank summary lines must be dropped")
	}

	// Queueing the same diagram again replaces the unsent prompt.
	body["edit_lines"] = []string{"Second pass"}
	body["remark"] = ""
	if resp, data := env.browser("POST", base+"/boards/1/submit", open.Key, body); resp.StatusCode != 200 {
		t.Fatalf("second submit = %d %s", resp.StatusCode, data)
	}
	snap, _ = env.hub.State(context.Background(), open.Key, 0, 0)
	if len(snap.Queued) != 1 || !strings.Contains(snap.Queued[0].Prompt, "Second pass") || strings.Contains(snap.Queued[0].Prompt, "Reviewer remark") {
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

// The reviewer's remark must reach the agent: it was once collected and
// dropped on the way. This fails if the handler ignores the field.
func TestBoardSubmit_TheRemarkReachesTheAgentOnItsOwnLabeledLine(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	resp, data := env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{
		"current": map[string]any{"elements": []any{}}, "edit_lines": []string{"Moved node B"}, "remark": "  rename\nB to\tGateway  ",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("submit = %d %s", resp.StatusCode, data)
	}
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	prompt := snap.Queued[0].Prompt
	if !strings.Contains(prompt, "\nReviewer remark: rename B to Gateway\n") {
		t.Errorf("remark not delivered as its own line:\n%s", prompt)
	}

	// Bounded, and an empty or blank remark adds no line.
	env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{}, "remark": strings.Repeat("r", 5000)})
	snap, _ = env.hub.State(context.Background(), open.Key, 0, 0)
	for _, line := range strings.Split(snap.Queued[0].Prompt, "\n") {
		if strings.HasPrefix(line, "Reviewer remark: ") && len(line) > len("Reviewer remark: ")+1003 {
			t.Errorf("remark line is %d bytes, not bounded", len(line))
		}
	}
	if !strings.Contains(snap.Queued[0].Prompt, "Reviewer remark: rrr") {
		t.Errorf("long remark missing:\n%s", snap.Queued[0].Prompt)
	}
	env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{}, "remark": " \n "})
	snap, _ = env.hub.State(context.Background(), open.Key, 0, 0)
	if strings.Contains(snap.Queued[0].Prompt, "Reviewer remark") {
		t.Errorf("a blank remark added a line:\n%s", snap.Queued[0].Prompt)
	}
}

func TestBoardSubmit_PNGIsOptional(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	resp, data := env.browser("POST", "/api/s/"+open.Key+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{"elements": []any{}}})
	if resp.StatusCode != 200 {
		t.Fatalf("submit without a PNG = %d %s", resp.StatusCode, data)
	}
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if strings.Contains(snap.Queued[0].Prompt, "Preview (PNG)") {
		t.Errorf("no PNG was sent, yet the prompt names one:\n%s", snap.Queued[0].Prompt)
	}
	if resp, _ := env.browser("POST", "/api/s/"+open.Key+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{}, "png": "data:image/jpeg;base64,AAAA"}); resp.StatusCode != 400 {
		t.Errorf("a non-PNG preview = %d, want 400", resp.StatusCode)
	}
}

func TestBoardSubmit_BoundsSummaryAndRefusesWhenEnded(t *testing.T) {
	env := newEnv(t, time.Minute)
	env.setArtifact(`<div class="mermaid">graph TD; A-->B</div>`)
	open := env.open()
	base := "/api/s/" + open.Key

	lines := make([]string, 120)
	for i := range lines {
		lines[i] = strings.Repeat("x", 900)
	}
	resp, data := env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{}, "edit_lines": lines})
	if resp.StatusCode != 200 {
		t.Fatalf("submit = %d %s", resp.StatusCode, data)
	}
	snap, _ := env.hub.State(context.Background(), open.Key, 0, 0)
	if got := strings.Count(snap.Queued[0].Prompt, "\n- "); got != 50 {
		t.Errorf("%d summary lines kept, want the 50-line cap", got)
	}
	if len(snap.Queued[0].Prompt) > 50*(300+3)+1500 {
		t.Errorf("prompt is %d bytes, summary not bounded", len(snap.Queued[0].Prompt))
	}

	env.hub.End(open.Key, forum.EndedByUser)
	if resp, _ := env.browser("POST", base+"/boards/0/submit", open.Key, map[string]any{"current": map[string]any{}}); resp.StatusCode != http.StatusConflict {
		t.Errorf("submit to an ended session = %d, want 409", resp.StatusCode)
	}
}

func TestWhiteboardStaticRoutes(t *testing.T) {
	env := newEnv(t, time.Minute)
	if resp, body := env.get("/whiteboard-frame?slot=0&palette=dark"); resp.StatusCode != 200 || !strings.Contains(body, "/whiteboard-assets/whiteboard.js") {
		t.Errorf("frame = %d", resp.StatusCode)
	}
	if resp, body := env.get("/whiteboard-embed.js"); resp.StatusCode != 200 || !strings.Contains(body, "vxb1.") || strings.Contains(body, "fetch(") {
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

// The frame bundle talks to its direct parent, the artifact page that holds
// the channel (whiteboard-embed.js), never to the top window (the forum
// chrome, which ignores it).
func TestWhiteboardBundle_TalksToItsParentNotTheTop(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, js := env.get("/whiteboard-assets/whiteboard.js")
	if strings.Contains(js, "window.top.postMessage") {
		t.Error("the whiteboard frame addresses window.top instead of its embedder")
	}
	if !strings.Contains(js, "window.parent.postMessage") {
		t.Error("the whiteboard frame never posts to window.parent")
	}
}
