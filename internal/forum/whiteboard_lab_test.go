//go:build unix

package forum_test

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The "lab" is how the real-Chrome whiteboard tests look inside the frames.
// In production the artifact runs in a sandboxed iframe of the forum chrome and
// every board frame is a sandboxed iframe of the artifact, so nothing outside
// can read a frame's DOM. The lab keeps the real embed script, the real frame
// page and the real server API, and changes only the two things that stand in
// the way:
//
//   - the page defines window.forum.__rpc itself (the SDK then leaves it
//     alone) and forwards the four board operations to the server with the
//     session token, the way the chrome's artifact bridge does;
//   - iframes are created without their sandbox attribute, so the frames are
//     same-origin with the page and the test script can read their documents.
//
// The test script runs in the page after load and reports one JSON string
// with report("result", ...); any exception is reported as "error".
const labPrelude = `<script>
(function () {
  var setAttr = Element.prototype.setAttribute;
  Element.prototype.setAttribute = function (name, value) {
    if (name === "sandbox" && this.tagName === "IFRAME") return;
    return setAttr.call(this, name, value);
  };
  var base = "/api/s/__KEY__";
  var routes = {
    "board.list": function () { return ["GET", "/diagrams"]; },
    "board.read": function (p) { return ["GET", "/boards/" + p.ordinal]; },
    "board.write": function (p) { return ["PUT", "/boards/" + p.ordinal]; },
    "board.submit": function (p) { return ["POST", "/boards/" + p.ordinal + "/submit"]; }
  };
  window.forum = { __rpc: async function (op, payload) {
    var route = routes[op] && routes[op](payload || {});
    if (!route) throw new Error("unsupported operation");
    var init = { method: route[0], headers: { "X-Forum-Token": "__TOKEN__", "Content-Type": "application/json" } };
    if (payload && payload.body !== undefined) init.body = JSON.stringify(payload.body);
    var response = await fetch(base + route[1], init);
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response.json();
  } };
  window.report = function (key, value) {
    return fetch("/__report?k=" + key + "&v=" + encodeURIComponent(value), { mode: "no-cors" });
  };
  window.sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  window.until = async function (fn, ms, what) {
    var end = Date.now() + (ms || 40000);
    for (;;) {
      try { var v = await fn(); if (v) return v; } catch (e) { /* not there yet */ }
      if (Date.now() > end) throw new Error("timed out waiting for " + (what || "a condition"));
      await sleep(100);
    }
  };
  window.boardFrame = function (i) { return document.querySelectorAll(".vxb-slot iframe")[i]; };
  window.frameDoc = function (iframe) { return iframe.contentDocument; };
  // Resolves the board's iframe once it is ready for the reviewer: its canvas
  // is revealed and the header buttons are enabled. "which" is a board index
  // or a function returning the iframe (for the fullscreen overlay's frame).
  window.boardReady = function (which) {
    return until(function () {
      var iframe = typeof which === "function" ? which() : boardFrame(which);
      var d = iframe && iframe.contentDocument;
      var board = d && d.querySelector(".vxb-board");
      var queue = d && d.querySelector(".vxb-btn-primary");
      return board && !board.hasAttribute("data-pending") && d.querySelector(".excalidraw") && queue && !queue.disabled ? iframe : null;
    }, 60000, "a board to become ready");
  };
  window.overlayFrame = function () { return document.querySelector("#vxb-overlay iframe"); };
  window.addEventListener("error", function (e) { report("error", "page error: " + e.message); });
  report("boot", "1");
})();
</script>`

// labPage builds the artifact: the prelude, one Mermaid block per diagram and
// the test script wrapped so that its result or its failure is reported.
func labPage(key, token, theme string, diagrams []string, script string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html data-fr-theme="%s"><head><meta charset="utf-8"><title>whiteboard lab</title>%s</head><body style="margin:0">`,
		theme, strings.NewReplacer("__KEY__", key, "__TOKEN__", token).Replace(labPrelude))
	b.WriteString(`<h1>lab</h1>`)
	for _, d := range diagrams {
		fmt.Fprintf(&b, "<div class=\"mermaid\">%s\n</div>\n<p style=\"height:300px\">spacer</p>\n", d)
	}
	fmt.Fprintf(&b, `<script>(async () => { try { %s } catch (e) { report("error", String((e && e.stack) || e)); } })();</script></body></html>`, script)
	return b.String()
}

// runLab serves the page and opens it top level in headless Chrome at the
// given window size; it returns the reported "result" JSON.
func runLab(t *testing.T, env *testEnv, key, theme string, diagrams []string, script string, width, height int, within time.Duration) string {
	t.Helper()
	chrome := headlessChrome(t)
	token, err := env.hub.Token(key)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	env.setArtifact(labPage(key, token, theme, diagrams, script))
	get := captureReports(t, env)
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check",
			fmt.Sprintf("--window-size=%d,%d", width, height), "--user-data-dir="+t.TempDir(), env.ts.URL+"/a/"+key+"/artifact.html?theme="+theme)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("chrome: %v", err)
		}
		kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }
		started := time.Now()
		for time.Since(started) < within {
			if msg, ok := get("error"); ok {
				kill()
				t.Fatalf("the lab page failed: %s", msg)
			}
			if v, ok := get("result"); ok {
				kill()
				return v
			}
			// A brand-new profile's very first launch sometimes never loads the
			// page: if the page has not even booted after a while, relaunch once.
			if _, booted := get("boot"); !booted && attempt == 0 && time.Since(started) > 20*time.Second {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		kill()
		if _, booted := get("boot"); booted {
			break
		}
	}
	t.Fatal("the lab page never reported a result")
	return ""
}
