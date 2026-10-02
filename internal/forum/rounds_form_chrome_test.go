//go:build unix

package forum_test

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A decision form and a form of the artifact's own, both reporting the
// attributes the SDK puts on them, every time they change.
const roundsFormArtifact = `<!doctype html><html><head><meta charset="utf-8"><title>q</title></head><body>
<form data-forum-question="q1" id="f"><button type="submit">Queue</button></form>
<form id="plain"><button type="submit">Other</button></form>
<script>
const f = document.getElementById("f"), p = document.getElementById("plain");
let last = "";
setInterval(() => {
  const now = "q1=" + f.getAttribute("data-forum-sent") + "/" + f.getAttribute("data-forum-round") + " plain=" + p.getAttribute("data-forum-sent") + " api=" + JSON.stringify(window.forum.sentStatus("question:q1"));
  if (now !== last) { last = now; fetch("/__report?v=" + encodeURIComponent(now), { mode: "no-cors" }); }
}, 100);
</script></body></html>`

// Once the user has sent a decision form's answer the form says so, with the
// round, and says it was answered when the agent replies; a form that was
// never sent is left alone. Real Chrome: chrome > sandboxed artifact.
func TestRounds_RealChrome_DecisionFormShowsSentAndAnswered(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(roundsFormArtifact)

	var mu sync.Mutex
	var reports []string
	inner := env.ts.Config.Handler
	env.ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__report" {
			mu.Lock()
			reports = append(reports, r.URL.Query().Get("v"))
			mu.Unlock()
			return
		}
		inner.ServeHTTP(w, r)
	})
	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })

	waitFor := func(want string) {
		t.Helper()
		for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			mu.Lock()
			for _, r := range reports {
				if strings.HasPrefix(r, want) {
					mu.Unlock()
					return
				}
			}
			mu.Unlock()
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("never saw %q; reports: %v", want, reports)
	}

	waitFor("q1=null/null plain=null api=null")
	env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "go with A", "queue_key": "question:q1"})
	env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{})
	waitFor(`q1=sent/1 plain=null api={"round":1,"state":"sent"}`)
	if err := env.hub.Reply(key, "done"); err != nil {
		t.Fatal(err)
	}
	waitFor("q1=answered/1 plain=null")
}
