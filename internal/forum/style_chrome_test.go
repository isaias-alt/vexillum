//go:build unix

package forum_test

import (
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// measuresHeadings loads the artifact in headless Chrome through the forum
// server (so the forum stylesheet is injected like in a real session) and
// returns, for each h2, [after, before] in px: the gap to the element it
// introduces and the gap to the element above it (-1 when it is first). The
// artifact reports through a request the test intercepts.
func measuresHeadings(t *testing.T, artifact string) [][2]int {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(artifact)

	reported := make(chan string, 1)
	inner := env.ts.Config.Handler
	env.ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__gaps" {
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

	var raw string
	select {
	case raw = <-reported:
	case <-time.After(45 * time.Second):
		t.Fatal("the artifact never reported its measurements")
	}
	var gaps [][2]int
	for _, pair := range regexp.MustCompile(`(-?\d+):(-?\d+)`).FindAllStringSubmatch(raw, -1) {
		after, _ := strconv.Atoi(pair[1])
		before, _ := strconv.Atoi(pair[2])
		gaps = append(gaps, [2]int{after, before})
	}
	return gaps
}

const measureScript = `<script>addEventListener("load",()=>{const r=e=>e.getBoundingClientRect();const g=[];
document.querySelectorAll("h2").forEach(h=>{const n=h.nextElementSibling,p=h.previousElementSibling;
g.push(Math.round(r(n).top-r(h).bottom)+":"+(p?Math.round(r(h).top-r(p).bottom):-1))});
fetch("/__gaps?v="+encodeURIComponent(g.join(" ")))})</script>`

// A heading sits closer to the body it introduces than to the block above,
// both inside a .fr-stack (the layout the commander's artifacts use) and in
// plain flow.
func TestArtifactStyle_RealChrome_HeadingIsCloserToItsBodyThanToWhatPrecedesIt(t *testing.T) {
	gaps := measuresHeadings(t, `<!doctype html><html><head><meta charset="utf-8"><title>r</title></head><body>
<main class="fr-page fr-stack">
<section class="fr-stack"><h2>One</h2><ul><li>a</li><li>b</li></ul></section>
<section class="fr-stack"><h2>Two</h2><p>text</p></section>
<form class="fr-form"><h2>Decision</h2><div class="fr-choices"><label class="fr-choice"><input type="radio" name="a"> x</label></div></form>
</main>
<h2>Flow one</h2><p>body</p>
<h2>Flow two</h2><ul><li>x</li></ul>
`+measureScript+`</body></html>`)
	if len(gaps) != 5 {
		t.Fatalf("measured %d headings, want 5: %v", len(gaps), gaps)
	}
	for i, g := range gaps {
		after, before := g[0], g[1]
		if after > 12 {
			t.Errorf("heading %d: %dpx between it and its own body, want at most 12 (spacing counted twice)", i, after)
		}
		if before >= 0 && before < after*2 {
			t.Errorf("heading %d: %dpx above vs %dpx below, the title must sit clearly closer to its body", i, before, after)
		}
	}
}
