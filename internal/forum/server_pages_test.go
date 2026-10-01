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
	for _, want := range []string{`/forum-assets/forum.css`, `/forum-assets/forum-chrome.js`, `sandbox="allow-scripts`, `Your agent is not listening. Ask it to poll for updates.`, `Send to Agent`, `Send &amp; End`, `id="themeSwitch"`, `id="annotateSwitch"`, `/forum-assets/forum-prefs.js`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// The theme script is synchronous and ahead of the stylesheets, so the
	// saved theme lands before the first paint.
	theme, tokens := strings.Index(body, "/forum-assets/forum-theme.js"), strings.Index(body, "/forum-assets/forum-tokens.css")
	if theme < 0 || tokens < 0 || theme > tokens || strings.Contains(body[theme-20:theme+60], "defer") || strings.Contains(body[theme-20:theme+60], "async") {
		t.Error("forum-theme.js must be a blocking script ahead of the stylesheets")
	}
	// Both toggles are real switches: role=switch with aria-checked, a text
	// label and a visible On/Off. Annotation starts On (aria-checked="true").
	for _, id := range []string{"annotateSwitch", "themeSwitch"} {
		m := regexp.MustCompile(`<button[^>]*id="` + id + `"[^>]*>`).FindString(body)
		if !strings.Contains(m, `role="switch"`) || !strings.Contains(m, "aria-checked=") || strings.Contains(m, "aria-pressed") {
			t.Errorf("%s must be a role=switch with aria-checked: %s", id, m)
		}
	}
	if m := regexp.MustCompile(`<button[^>]*id="annotateSwitch"[^>]*>`).FindString(body); !strings.Contains(m, `aria-checked="true"`) {
		t.Error("annotation must start On")
	}
	if !strings.Contains(body, `class="switch-label">Annotate`) || !strings.Contains(body, `class="switch-label">Light theme`) {
		t.Error("the switches need visible text labels")
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
	resp, tokens := env.get("/forum-assets/forum-tokens.css")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("tokens = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// The design system's tokens, with light, OS-dark and manual-dark wiring.
	for _, want := range []string{"--fr-bg:", "--fr-accent:", "--fr-selection:", "--fr-radius-md:", "--fr-space-3:", `:root[data-fr-theme="light"]`} {
		if !strings.Contains(tokens, want) {
			t.Errorf("tokens missing %q", want)
		}
	}
	// Dark is the default and ignores the OS: the base :root block holds the
	// dark tokens and light is only reachable through the attribute.
	if strings.Contains(tokens, "prefers-color-scheme: dark") {
		t.Error("the default theme must not depend on prefers-color-scheme")
	}
	base := tokens[:strings.Index(tokens, `:root[data-fr-theme="light"]`)]
	if !strings.Contains(base, "--fr-bg: #15171A;") || strings.Contains(tokens, "color-scheme:") {
		t.Error("the base :root block must carry the dark tokens, and the tokens must not set color-scheme (it would leak into artifacts)")
	}
	for name, sheet := range map[string]string{"forum.css": css, "forum-tokens.css": tokens} {
		if strings.Contains(sheet, "http://") || strings.Contains(sheet, "https://") || strings.Contains(sheet, "@import") {
			t.Errorf("%s must not pull anything from the network", name)
		}
	}
	// Components must use tokens, not raw colors (the only literals live in the :root blocks).
	rules := css[strings.Index(css, "*, *::before"):]
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`).MatchString(rules) {
		t.Error("a component rule hardcodes a color instead of a custom property")
	}
	if strings.Contains(css, "--forum-") {
		t.Error("forum.css still references the provisional --forum-* palette")
	}
	for _, name := range []string{"forum-chrome.js", "forum-sdk.js", "forum-layout.js", "forum-theme.js"} {
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
