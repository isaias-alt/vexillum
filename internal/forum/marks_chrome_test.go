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

// A self-styled artifact (no forum look, no tokens), so the badges must stand
// on their own: a plain decision form, two annotated paragraphs and a long
// page. Every 150ms it reports what the badge layer shows and whether the
// badges moved anything of the artifact's own. The artifact sits in an iframe
// that starts loading before the chrome around it has laid out, so a read taken
// right after navigation or reload can see a 0x0 frame. A size is therefore
// only reported once it is non-empty and identical across two animation
// frames; until then the report says size=unsettled.
func marksArtifact(extra string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>m</title><style>body{font:16px sans-serif;margin:0;padding:24px}</style></head><body>
` + extra + `
<form data-forum-question="q1" id="f"><label><input type="radio" name="o" value="a"> A</label><button type="button" id="b">Queue</button></form>
<p id="p1">First annotated paragraph.</p>
<p id="p2">Second paragraph, will not be annotated.</p>
<div style="height:2000px"></div>
<script>
const rawSizes = () => document.documentElement.scrollWidth + "x" + document.documentElement.scrollHeight + "/" + Math.round(document.getElementById("p1").getBoundingClientRect().top);
const settledSizes = () => new Promise((resolve) => requestAnimationFrame(() => {
  const a = rawSizes();
  requestAnimationFrame(() => { const b = rawSizes(); resolve(a === b && !a.startsWith("0x0/") ? a : "unsettled"); });
}));
let last = "";
setInterval(async () => {
  const size = await settledSizes();
  const host = document.querySelector("[data-forum-ui=marks]");
  const marks = host ? [...host.shadowRoot.querySelectorAll(".mark")].map((m) => m.dataset.kind + ":" + m.textContent) : [];
  const layer = host ? getComputedStyle(host.shadowRoot.querySelector(".layer")).pointerEvents + "/" + getComputedStyle(host).pointerEvents + "/" + getComputedStyle(host).position : "-";
  const now = "marks=" + marks.join("|") + " layer=" + layer + " size=" + size;
  if (now !== last) { last = now; fetch("/__report?v=" + encodeURIComponent(now), { mode: "no-cors" }); }
}, 150);
</script></body></html>`
}

// Decision forms and annotations the user already sent carry a badge in the
// artifact: "Sent in round N", then "Answered in round N" once the agent
// replies. It survives the artifact being rewritten, skips (silently) a
// selector that no longer matches, and never changes the artifact's layout or
// takes pointer events away from it.
func TestMarks_RealChrome_BadgesOnFormsAndAnnotationsSurviveReload(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(marksArtifact(""))

	var mu sync.Mutex
	var reports []string
	env.handle("/__report", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reports = append(reports, r.URL.Query().Get("v"))
		mu.Unlock()
	})
	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })

	find := func(settled bool, want ...string) string {
		t.Helper()
		for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			mu.Lock()
			for _, r := range reports {
				ok := !settled || !strings.Contains(r, "size=unsettled")
				for _, w := range want {
					ok = ok && strings.Contains(r, w)
				}
				if ok {
					mu.Unlock()
					return r
				}
			}
			mu.Unlock()
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("never saw %q (settled size required: %v); reports: %v", want, settled, reports)
		return ""
	}
	waitFor := func(want ...string) string { t.Helper(); return find(false, want...) }
	// waitSettled wants a report whose size was measured on a settled layout
	// (non-empty and stable across two frames), so size comparisons never
	// involve a half-laid-out frame.
	waitSettled := func(want ...string) string { t.Helper(); return find(true, want...) }
	sizeOf := func(report string) string { return report[strings.Index(report, "size="):] }

	initial := waitSettled("marks= layer=-")
	baseline := sizeOf(initial)

	queue := func(body map[string]any) {
		if resp, data := env.browser("POST", "/api/s/"+key+"/queue", key, body); resp.StatusCode != 200 {
			t.Fatalf("queue = %d %s", resp.StatusCode, data)
		}
	}
	queue(map[string]any{"prompt": "answer A", "tag": "decision", "queue_key": "question:q1", "selector": "form#f > button"})
	queue(map[string]any{"prompt": "shorten this", "selector": "#p1", "text": "First"})
	queue(map[string]any{"prompt": "a comment on something since removed", "selector": "#gone"})
	env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{})

	sent := waitSettled("decision:Sent in round 1", "comment:Sent in round 1")
	if strings.Count(sent, "Sent in round 1") != 2 {
		t.Errorf("want exactly two badges (the form and #p1; #gone no longer matches): %s", sent)
	}
	if !strings.Contains(sent, "layer=none/none/fixed") {
		t.Errorf("the badge layer must be fixed and pointer-transparent: %s", sent)
	}
	if sizeOf(sent) != baseline {
		t.Errorf("the badges changed the artifact's layout: %s vs %s", sizeOf(sent), baseline)
	}

	if err := env.hub.Reply(key, "done"); err != nil {
		t.Fatal(err)
	}
	waitFor("decision:✓ Answered in round 1", "comment:✓ Answered in round 1")

	// The agent rewrites the artifact (here: a banner pushes everything down and
	// #p1 loses its place); the badges come back on the reloaded page.
	env.setArtifact(marksArtifact(`<h1 id="new">A new heading above everything</h1>`))
	var reloaded string
	for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		mu.Lock()
		last := reports[len(reports)-1]
		mu.Unlock()
		if !strings.Contains(last, "size=unsettled") && sizeOf(last) != baseline && strings.Count(last, "Answered in round 1") == 2 {
			reloaded = last
			break
		}
	}
	if reloaded == "" {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("badges did not re-attach on the rewritten artifact; reports: %v", reports)
	}
}
