package forum_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func (e *testEnv) get(path string) (*http.Response, string) {
	e.t.Helper()
	resp, err := http.Get(e.ts.URL + path)
	if err != nil {
		e.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func (e *testEnv) setArtifact(html string) { writeFile(e.t, e.file, html) }

func TestSessionPage_ServesChromeWithTokenAndStrictHeaders(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	resp, body := env.get("/session/" + open.Key)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'self'") || !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("the session page carries the token and must never be CORS-readable")
	}
	m := regexp.MustCompile(`<script type="application/json" id="forum-boot">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no boot block in page:\n%s", body)
	}
	var boot struct {
		Key, Token, File, Name string
		ArtifactSrc            string `json:"artifact_src"`
	}
	if err := json.Unmarshal([]byte(m[1]), &boot); err != nil {
		t.Fatalf("boot is not valid JSON: %v (%s)", err, m[1])
	}
	token, _ := env.hub.Token(open.Key)
	if boot.Token != token || boot.Key != open.Key || boot.ArtifactSrc != "/a/"+open.Key+"/artifact.html" {
		t.Errorf("boot = %+v", boot)
	}
	for _, want := range []string{`/forum-assets/forum.css`, `/forum-assets/forum-chrome.js`, `sandbox="allow-scripts`, `Your agent is not listening. Ask it to poll for updates.`, `Send to Agent`, `Send &amp; End`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "allow-same-origin") {
		t.Error("the artifact iframe must not be granted allow-same-origin")
	}
	if strings.Contains(body, "https://") || strings.Contains(body, "cdn") {
		t.Error("the chrome must not reference any external host")
	}
}

func TestSessionPage_FileNameCannotInjectMarkup(t *testing.T) {
	env := newEnv(t, time.Minute)
	evil := filepath.Join(filepath.Dir(env.file), `"><img src=x onerror=alert(1)>.html`)
	writeFile(t, evil, "<p>x</p>")
	_, data := env.agent("POST", "/api/agent/open", map[string]any{"file": evil})
	var open forum.OpenResponse
	_ = json.Unmarshal(data, &open)
	_, body := env.get("/session/" + open.Key)
	if strings.Contains(body, "<img src=x onerror") {
		t.Errorf("file name reached the page unescaped")
	}
}

func TestSessionPage_UnknownSession404(t *testing.T) {
	env := newEnv(t, time.Minute)
	for _, key := range []string{"0123456789abcdef", "nope"} {
		if resp, _ := env.get("/session/" + key); resp.StatusCode != 404 {
			t.Errorf("/session/%s = %d, want 404", key, resp.StatusCode)
		}
	}
}

func TestChromeAssets_ServedAndSelfContained(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, css := env.get("/forum-assets/forum.css")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("css = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"--forum-bg:", "--forum-accent:", "--forum-radius-md:", "--forum-space-3:", "prefers-color-scheme: dark"} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") || strings.Contains(css, "@import") {
		t.Error("the stylesheet must not pull anything from the network")
	}
	// Components must use tokens, not raw colors (the only literals live in :root blocks).
	rules := css[strings.Index(css, "*, *::before"):]
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).MatchString(rules) {
		t.Error("a component rule hardcodes a color instead of a custom property")
	}
	for _, name := range []string{"forum-chrome.js", "forum-sdk.js"} {
		if resp, body := env.get("/forum-assets/" + name); resp.StatusCode != 200 || body == "" {
			t.Errorf("%s = %d", name, resp.StatusCode)
		}
	}
	if resp, _ := env.get("/forum-assets/chrome.html"); resp.StatusCode != 404 {
		t.Errorf("template must not be served raw, got %d", resp.StatusCode)
	}
	if resp, _ := env.get("/forum-assets/..%2fsession"); resp.StatusCode == 200 {
		t.Error("asset route accepted a traversal name")
	}
}

func TestArtifact_ServesSiblingAssetsAndConfinesToItsDirectory(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	dir := filepath.Dir(env.file)
	writeFile(t, filepath.Join(dir, "style.css"), "body{color:red}")
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "img", "a.svg"), "<svg/>")
	writeFile(t, filepath.Join(dir, ".secret"), "TOKEN=1")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "passwd"), "root:x")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}

	base := "/a/" + open.Key + "/"
	resp, body := env.get(base + "style.css")
	if resp.StatusCode != 200 || body != "body{color:red}" || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("style.css = %d %q cors=%q", resp.StatusCode, body, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp, _ := env.get(base + "img/a.svg"); resp.StatusCode != 200 {
		t.Errorf("nested asset = %d", resp.StatusCode)
	}
	for _, bad := range []string{".secret", "escape/passwd", "img", "missing.png", "%2e%2e/%2e%2e/etc/passwd", "..%2f..%2fetc%2fpasswd", "img/%2e%2e/%2e%2e/x"} {
		resp, body := env.get(base + bad)
		if resp.StatusCode == 200 || strings.Contains(body, "root:x") || strings.Contains(body, "TOKEN=1") {
			t.Errorf("GET %s = %d %q, want it refused", bad, resp.StatusCode, body)
		}
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

func TestState_ReportsArtifactVersionAndWakesWhenTheFileChanges(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key
	_, data := env.browser("GET", base+"/state", open.Key, nil)
	var first struct {
		Version         int64  `json:"version"`
		ArtifactVersion string `json:"artifact_version"`
	}
	if err := json.Unmarshal(data, &first); err != nil || first.ArtifactVersion == "" {
		t.Fatalf("state = %s (%v)", data, err)
	}

	got := make(chan string, 1)
	go func() {
		_, data := env.browser("GET", base+"/state?since="+strconv.FormatInt(first.Version, 10)+"&av="+first.ArtifactVersion, open.Key, nil)
		var s struct {
			ArtifactVersion string `json:"artifact_version"`
		}
		_ = json.Unmarshal(data, &s)
		got <- s.ArtifactVersion
	}()
	time.Sleep(100 * time.Millisecond)
	env.setArtifact("<html><body><p>edited by the agent, longer than before</p></body></html>")
	select {
	case v := <-got:
		if v == first.ArtifactVersion {
			t.Error("artifact version did not change after the edit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("state long-poll did not notice the artifact edit")
	}
}
