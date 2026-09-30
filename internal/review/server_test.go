package review_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/review"
)

func newTestServer(t *testing.T, artifactHTML string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "artifact.html")
	if err := os.WriteFile(file, []byte(artifactHTML), 0o644); err != nil {
		t.Fatalf("writing artifact file: %v", err)
	}
	store := review.NewStore(t.TempDir())
	srv := review.NewServer(file, store)
	return httptest.NewServer(srv)
}

func TestServer_ServesArtifactUnmodifiedWithoutMermaid(t *testing.T) {
	const html = `<html><body><p>hello</p></body></html>`
	ts := newTestServer(t, html)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if body != html {
		t.Errorf("body = %q, want unmodified %q", body, html)
	}
}

func TestServer_InjectsEmbedScriptWhenMermaidPresent(t *testing.T) {
	const html = `<html><body><div class="mermaid">graph TD; A-->B</div></body></html>`
	ts := newTestServer(t, html)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, `<script src="/whiteboard-embed.js"></script>`) {
		t.Errorf("expected embed script tag injected into body, got: %s", body)
	}
	if !strings.Contains(body, `<div class="mermaid">`) {
		t.Error("expected the original .mermaid container to survive injection")
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(data)
}

func TestServer_MermaidSourcesEndpoint(t *testing.T) {
	const html = `<div class="mermaid">graph TD; A-->B</div>`
	ts := newTestServer(t, html)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/mermaid-sources")
	if err != nil {
		t.Fatalf("GET /api/mermaid-sources: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Sources []review.MermaidSource `json:"sources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.Sources) != 1 || body.Sources[0].Source != "graph TD; A-->B" {
		t.Errorf("sources = %+v", body.Sources)
	}
}

func TestServer_WhiteboardGetMissingReturnsNull(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/whiteboard/0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Whiteboard *review.SavedScene `json:"whiteboard"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Whiteboard != nil {
		t.Errorf("expected nil whiteboard before any save, got %+v", body.Whiteboard)
	}
}

func TestServer_PutThenGetWhiteboard_RoundTrips(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	payload := `{"source_hash":"abc","text_metrics_version":1,"scene":{"elements":[{"id":"x"}]},"baseline":{"elements":[]}}`
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/whiteboard/0", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("building PUT request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", resp.StatusCode)
	}

	getResp, err := http.Get(ts.URL + "/api/whiteboard/0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer getResp.Body.Close()
	var body struct {
		Whiteboard *review.SavedScene `json:"whiteboard"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Whiteboard == nil {
		t.Fatal("expected a saved whiteboard after PUT")
	}
	if body.Whiteboard.SourceHash != "abc" {
		t.Errorf("SourceHash = %q, want %q", body.Whiteboard.SourceHash, "abc")
	}
}

func TestServer_FeedbackFilesEndpoint(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	payload := `{"scene":{"elements":[{"id":"x"}]},"pngDataUrl":""}`
	resp, err := http.Post(ts.URL+"/api/whiteboard/0/feedback-files", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		ScenePath   string `json:"scene_path"`
		PreviewPath string `json:"preview_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.ScenePath == "" {
		t.Error("expected a non-empty scene_path")
	}
	if body.PreviewPath != "" {
		t.Errorf("expected empty preview_path without a PNG, got %q", body.PreviewPath)
	}
}

func TestServer_InvalidDiagramIndexRejected(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/whiteboard/not-a-number")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServer_WhiteboardAssets_ServesGzippedJSWithHeaders(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/whiteboard-assets/whiteboard.js", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	// Go's Transport auto-decompresses (and hides Content-Encoding) unless
	// the request sets Accept-Encoding itself - set it explicitly so this
	// test observes the real wire response.
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestServer_WhiteboardAssets_ServesCSS(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whiteboard-assets/whiteboard.css")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got == "gzip" {
		t.Error("whiteboard.css should not be served gzip-encoded")
	}
}

func TestServer_WhiteboardAssets_UnknownFileNotFound(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whiteboard-assets/does-not-exist.js")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// A literal ".." segment never reaches whiteboardAssetsHandler in practice -
// net/http's ServeMux (Go 1.22+) cleans dot segments out of the request
// path and redirects before routing, so a request for
// /whiteboard-assets/../server.go lands on a completely different route
// (here, the catch-all artifact handler), not this one with ".." intact.
// The handler's own `strings.Contains(requested, "..")` guard is
// defense-in-depth for that routing behavior changing or this handler being
// reused somewhere without a cleaning mux in front of it - not otherwise
// reachable from a real HTTP client, so it is not exercised via httptest
// here.
func TestServer_PathTraversalRequestNeverLeaksRepoFiles(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whiteboard-assets/../server.go")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if strings.Contains(body, "package review") {
		t.Error("response leaked this package's own source file")
	}
}

func TestServer_WhiteboardFrameServesHTML(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whiteboard-frame?diagramIndex=0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, `/whiteboard-assets/whiteboard.js`) {
		t.Errorf("expected the frame page to load whiteboard.js, got: %s", body)
	}
}

func TestServer_EmbedScriptEndpoint(t *testing.T) {
	ts := newTestServer(t, `<div class="mermaid">graph TD; A-->B</div>`)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whiteboard-embed.js")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, "vx-whiteboard:") {
		t.Error("expected whiteboard-embed.js to reference the vx-whiteboard: postMessage protocol")
	}
}
