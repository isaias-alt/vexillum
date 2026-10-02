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

// Three decision forms, written the way agents write them. q1 queues under the
// bare question id (queueKey: "q1", not the "question:q1" forum derives); h
// queues under a custom key with no attribute saying so; "own" is a button the
// artifact disabled itself. Each state change is reported to the test.
const queueFormArtifact = `<!doctype html><html><head><meta charset="utf-8"><title>q</title></head><body>
<form data-forum-question="q1" id="f"><label><input type="radio" name="o" value="a" checked> A</label><button type="submit" id="b">Queue</button></form>
<form data-forum-question="h" id="h"><button type="submit" id="hb">Queue</button></form>
<form data-forum-queue-key="custom" id="g"><button type="submit" id="c">Other</button><button type="submit" id="own" disabled>Mine</button></form>
<script>
const $ = (id) => document.getElementById(id);
const log = (v) => fetch("/__report?v=" + encodeURIComponent(v), { mode: "no-cors" });
$("f").addEventListener("submit", (e) => { e.preventDefault(); window.forum.queuePrompt("answer", { tag: "decision", queueKey: "q1" }); });
$("h").addEventListener("submit", (e) => { e.preventDefault(); window.forum.queuePrompt("answer", { tag: "decision", queueKey: "mine" }); });
$("g").addEventListener("submit", (e) => e.preventDefault());
window.forum.onQueueChange((keys) => log("q1=" + $("b").disabled + " h=" + $("hb").disabled + " custom=" + $("c").disabled + " own=" + $("own").disabled + " keys=" + keys.join("|")));
setTimeout(() => {
  if (!window.forum.isQueued("q1")) $("f").requestSubmit();
  if (!window.forum.isQueued("mine")) $("h").requestSubmit();
}, 700);
</script></body></html>`

type queueFormRun struct {
	t       *testing.T
	env     *testEnv
	key     string
	mu      sync.Mutex
	reports []string
}

// startQueueFormRun opens the session in real headless Chrome (chrome >
// sandboxed artifact) and collects what the artifact reports.
func startQueueFormRun(t *testing.T, prequeue bool) *queueFormRun {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	run := &queueFormRun{t: t, env: env, key: env.open().Key}
	env.setArtifact(queueFormArtifact)
	if prequeue {
		for _, k := range []string{"q1", "mine"} {
			body := map[string]any{"prompt": "earlier answer", "tag": "decision", "queue_key": k}
			if resp, data := env.browser("POST", "/api/s/"+run.key+"/queue", run.key, body); resp.StatusCode != 200 {
				t.Fatalf("queue = %d %s", resp.StatusCode, data)
			}
		}
	}
	env.handle("/__report", func(w http.ResponseWriter, r *http.Request) {
		run.mu.Lock()
		run.reports = append(run.reports, r.URL.Query().Get("v"))
		run.mu.Unlock()
	})
	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+run.key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	return run
}

func (r *queueFormRun) waitFor(want string) {
	r.t.Helper()
	for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		r.mu.Lock()
		for _, report := range r.reports {
			if strings.HasPrefix(report, want) {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t.Fatalf("never saw %q; reports: %v", want, r.reports)
}

// unqueue removes the queued prompt carrying queueKey, as the user's x does.
func (r *queueFormRun) unqueue(queueKey string) {
	r.t.Helper()
	for _, p := range r.env.snapshot(r.key).Queued {
		if p.QueueKey == queueKey {
			if resp, data := r.env.browser("DELETE", "/api/s/"+r.key+"/queue/"+p.UID, r.key, nil); resp.StatusCode != 200 {
				r.t.Fatalf("unqueue = %d %s", resp.StatusCode, data)
			}
			return
		}
	}
	r.t.Fatalf("no queued prompt with key %q: %+v", queueKey, r.env.snapshot(r.key).Queued)
}

// A decision form whose answer sits in the user's queue has its submit button
// disabled, and gets it back when the answer leaves the queue. The queue key
// may be the bare question id or a custom one the form never declared; the
// unrelated form and the artifact's own disabled button are left alone.
func TestQueueState_RealChrome_FormButtonFollowsTheQueue(t *testing.T) {
	run := startQueueFormRun(t, false)
	run.waitFor("q1=true h=true custom=false own=true")
	run.unqueue("q1")
	run.waitFor("q1=false h=true custom=false own=true")
	run.unqueue("mine")
	run.waitFor("q1=false h=false custom=false own=true")
}

// Opened with the answers already queued (a reload, a second tab): the buttons
// are disabled from the start and come back when the messages are removed. A
// custom key the page has not seen submitted cannot be matched to its form,
// which is why agents must declare it with data-forum-queue-key.
func TestQueueState_RealChrome_ReloadWithAnAnswerAlreadyQueued(t *testing.T) {
	run := startQueueFormRun(t, true)
	run.waitFor("q1=true h=false custom=false own=true")
	run.unqueue("q1")
	run.waitFor("q1=false")
}
