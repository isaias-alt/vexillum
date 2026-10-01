//go:build unix

package forum_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The whiteboard only works through a three-window chain (chrome > artifact >
// frame) and a postMessage handshake across it, which nothing but a real
// browser exercises. The frame autosaves its scene once the embedder's init
// message has arrived and the Mermaid source converted, so a saved scene on
// disk proves the whole chain: ready -> init -> render -> save.
//
// Both conversions run: a flowchart becomes editable shapes, a pie chart is
// not natively convertible and embeds as an image on the same canvas.
func TestWhiteboard_RealChrome_FrameHandshakeRendersAndSavesTheScene(t *testing.T) {
	for name, source := range map[string]string{
		"flowchart (editable shapes)": "flowchart LR\n  A[Start] --> B{Ok?}\n  B -- yes --> C[Done]",
		"pie (image fallback)":        "pie title Pets\n  \"Dogs\" : 3\n  \"Cats\" : 2",
	} {
		t.Run(name, func(t *testing.T) { whiteboardSavesScene(t, source) })
	}
}

func whiteboardSavesScene(t *testing.T, source string) {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>wb</title></head><body>
<h2>Flow</h2><div class="mermaid">` + source + `
</div></body></html>`)

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--window-size=1500,800", "--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	scene := filepath.Join(env.home, "forums", key, "whiteboards", "0.json")
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if _, err := os.Stat(scene); err == nil {
			return
		}
	}
	t.Fatalf("the whiteboard never saved a scene (%s): the frame and the artifact did not complete the init handshake", scene)
}

// A whiteboard that cannot start must say so: the empty cream box it used to
// leave is indistinguishable from a broken one. Opened without the forum
// chrome (the artifact URL on its own) the init round trip fails, and the
// artifact must show why, in its own DOM, instead of staying blank.
func TestWhiteboard_RealChrome_AFrameThatCannotStartSaysWhy(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>wb</title></head><body>
<div class="mermaid">flowchart LR
  A --> B
</div>
<script>setTimeout(() => {
  const s = document.querySelector("[role=status]");
  fetch("/__status?v=" + encodeURIComponent(s ? s.textContent : "NO STATUS"), { mode: "no-cors" });
}, 6000);</script></body></html>`)

	reported := make(chan string, 1)
	inner := env.ts.Config.Handler
	env.ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__status" {
			select {
			case reported <- r.URL.Query().Get("v"):
			default:
			}
			return
		}
		inner.ServeHTTP(w, r)
	})

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/a/"+key+"/artifact.html")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	select {
	case got := <-reported:
		if !strings.Contains(got, "Could not start the whiteboard") || !strings.Contains(got, "forum chrome") {
			t.Errorf("status = %q, want the reason the whiteboard could not start", got)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("the artifact never reported")
	}
}

// The frame page names its bundle with the build id, so a browser holding an
// earlier bundle under the old URL cannot run it against this embed script.
func TestWhiteboardFrame_BundleURLsAreVersioned(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, body := env.get("/whiteboard-frame?diagramIndex=0")
	for _, want := range []string{"/whiteboard-assets/whiteboard.js?v=", "/whiteboard-assets/whiteboard.css?v="} {
		if !strings.Contains(body, want) {
			t.Errorf("frame page missing %q:\n%s", want, body)
		}
	}
}
