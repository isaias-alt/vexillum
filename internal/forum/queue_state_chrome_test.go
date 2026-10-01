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

// A decision form whose answer sits in the user's queue has its submit button
// disabled, and gets it back when the answer leaves the queue (removed, or
// sent). Real chrome > artifact chain: the chrome reports the queue keys to
// the SDK inside the sandboxed artifact.
func TestQueueState_RealChrome_FormButtonFollowsTheQueue(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>q</title></head><body>
<form data-forum-question="q1" id="f"><label><input type="radio" name="o" value="a" checked> A</label><button type="submit" id="b">Queue</button></form>
<form data-forum-queue-key="custom" id="g"><button type="submit" id="c">Other</button><button type="submit" id="own" disabled>Mine</button></form>
<script>
const b = document.getElementById("b"), c = document.getElementById("c"), own = document.getElementById("own");
const log = (v) => fetch("/__report?v=" + encodeURIComponent(v), { mode: "no-cors" });
document.getElementById("f").addEventListener("submit", (e) => {
  e.preventDefault();
  window.forum.queuePrompt("answer", { element: e.target, tag: "decision" });
});
document.getElementById("g").addEventListener("submit", (e) => e.preventDefault());
window.forum.onQueueChange((keys) => log("q1=" + b.disabled + " custom=" + c.disabled + " own=" + own.disabled + " keys=" + keys.join("|") + " queued=" + window.forum.isQueued("question:q1")));
setTimeout(() => document.getElementById("f").requestSubmit(), 700);
</script></body></html>`)

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

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	// Queued: its button is disabled; the unrelated form and the artifact's own
	// disabled button are not touched.
	waitFor("q1=true custom=false own=true keys=question:q1 queued=true")

	// Removed from the queue: the button comes back, the artifact's own stays off.
	queued := env.snapshot(key).Queued
	if len(queued) != 1 {
		t.Fatalf("queue = %+v, want the one answer", queued)
	}
	if resp, data := env.browser("DELETE", "/api/s/"+key+"/queue/"+queued[0].UID, key, nil); resp.StatusCode != 200 {
		t.Fatalf("unqueue = %d %s", resp.StatusCode, data)
	}
	waitFor("q1=false custom=false own=true keys= queued=false")
}
