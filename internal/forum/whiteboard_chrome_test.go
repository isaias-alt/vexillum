//go:build unix

package forum_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The whiteboard only works through a three-window chain (chrome > artifact >
// frame) and a postMessage handshake across it, which nothing but a real
// browser exercises. The frame autosaves its scene once the embedder's init
// message has arrived and the Mermaid source converted, so a saved scene on
// disk proves the whole chain: ready -> init -> render -> save.
func TestWhiteboard_RealChrome_FrameHandshakeRendersAndSavesTheScene(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>wb</title></head><body>
<h2>Flow</h2><div class="mermaid">flowchart LR
  A[Start] --> B{Ok?}
  B -- yes --> C[Done]
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
