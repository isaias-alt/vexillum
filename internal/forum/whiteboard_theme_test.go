//go:build unix

package forum_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Every colour the old upstream whiteboard hard-coded: the gold of its
// buttons and accents, its cream paper and the dark/cream pairs of its link
// dialog. None of them may be in anything the forum serves for the
// whiteboard - the whiteboard follows the forum's --fr-* tokens instead.
var upstreamColours = []string{
	"#f4c95d", "#bf9455", "#fffbf3", "#17130a", "#f7f3ea", "#0f1115", "#1a1d23",
	"244, 201, 93", "191, 148, 85", "23, 19, 10",
}

func TestWhiteboardAssets_NoUpstreamColourRemains(t *testing.T) {
	env := newEnv(t, time.Minute)
	for _, path := range []string{"/whiteboard-embed.js", "/whiteboard-assets/whiteboard.css", "/whiteboard-assets/whiteboard.js", "/whiteboard-frame", "/favicon.svg"} {
		resp, body := env.get(path)
		if resp.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("GET %s = %d (%d bytes)", path, resp.StatusCode, len(body))
		}
		lower := strings.ToLower(body)
		for _, colour := range upstreamColours {
			if strings.Contains(lower, colour) {
				t.Errorf("%s still contains the old upstream value %q", path, colour)
			}
		}
	}
}

// The whiteboard stylesheet must repoint Excalidraw's own accent and surfaces
// at the forum tokens, in both of its theme blocks.
func TestWhiteboardCSS_ThemesExcalidrawWithForumTokens(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, css := env.get("/whiteboard-assets/whiteboard.css")
	for _, want := range []string{
		"--color-primary: var(--fr-accent)",
		"--island-bg-color: var(--fr-surface)",
		"--default-bg-color: var(--fr-bg)",
		"--color-selection: var(--fr-accent)",
		"--link-color: var(--fr-accent)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("whiteboard.css does not contain %q", want)
		}
	}
	if !strings.Contains(css, "body .excalidraw.theme--dark") {
		t.Error("the dark Excalidraw block is not overridden")
	}
}

func TestFavicon_ICOFallbackAndVersionedLinkResolveToTheBlueMark(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, ico := env.get("/favicon.ico")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/x-icon" {
		t.Fatalf("/favicon.ico = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	assertBlueMarkICO(t, []byte(ico))

	// The session page and every artifact (with or without a <head>) link the
	// same content-versioned address, and it serves the SVG.
	open := env.open()
	link := regexp.MustCompile(`href="(/favicon\.svg\?v=[0-9a-f]{12})"`)
	_, page := env.get("/session/" + open.Key)
	m := link.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the session page has no versioned favicon link:\n%s", firstLines(page, 12))
	}
	href := m[1]
	for name, doc := range map[string]string{
		"with head":    `<!doctype html><html><head><title>t</title></head><body>x</body></html>`,
		"without head": `<!doctype html><html><body>x</body></html>`,
		"bare":         `<p>x</p>`,
	} {
		env.setArtifact(doc)
		_, got := env.get("/a/" + open.Key + "/artifact.html")
		if !strings.Contains(got, `href="`+href+`"`) {
			t.Errorf("artifact %s: favicon %s not injected:\n%s", name, href, got)
		}
	}
	resp, svg := env.get(href)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("%s = %d %q", href, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(svg, "#6FA1CB") || !strings.Contains(svg, "#15171A") {
		t.Errorf("the favicon is not the blue mark on #15171A:\n%s", svg)
	}
	lower := strings.ToLower(svg)
	for _, colour := range append([]string{"#c9a15a", "#c9a227"}, upstreamColours...) {
		if strings.Contains(lower, colour) {
			t.Errorf("the favicon contains the gold value %q", colour)
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// assertBlueMarkICO parses the .ico, decodes every PNG entry and checks the
// mark is blue (lapis family: blue clearly above red) and no pixel is gold
// (red and green well above blue).
func assertBlueMarkICO(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		t.Fatalf("not an ICO file (%d bytes)", len(data))
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 {
		t.Fatal("the ICO has no images")
	}
	for i := 0; i < count; i++ {
		entry := data[6+16*i : 6+16*(i+1)]
		size := binary.LittleEndian.Uint32(entry[8:12])
		offset := binary.LittleEndian.Uint32(entry[12:16])
		img, err := png.Decode(bytes.NewReader(data[offset : offset+size]))
		if err != nil {
			t.Fatalf("ICO image %d is not a PNG: %v", i, err)
		}
		blue, gold := 0, 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, a := img.At(x, y).RGBA()
				if a < 0x8000 {
					continue
				}
				r, g, bl = r>>8, g>>8, bl>>8
				if bl > r+40 && bl > 150 {
					blue++
				}
				if r > bl+40 && g > bl+20 {
					gold++
				}
			}
		}
		if blue == 0 {
			t.Errorf("ICO image %d (%v) has no blue mark pixels", i, imageSize(img))
		}
		if gold != 0 {
			t.Errorf("ICO image %d (%v) has %d gold pixels", i, imageSize(img), gold)
		}
	}
}

func imageSize(img image.Image) image.Point { return img.Bounds().Size() }

// The page the browser tab shows, in real Chrome: an artifact opened top level
// (no forum chrome around it) resolves its icon link to the versioned blue SVG,
// and /favicon.ico - what a tab with no link falls back to - is the blue mark
// too instead of a 404.
func TestFavicon_RealChrome_TopLevelArtifactResolvesTheBlueMark(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><body><h1>top level</h1>
<script>
const report = (k, v) => fetch("/__report?k=" + k + "&v=" + encodeURIComponent(v), { mode: "no-cors" });
(async () => {
  const link = document.querySelector('link[rel~="icon"]');
  const svg = await fetch(link.href, { cache: "reload" });
  const ico = await fetch("/favicon.ico", { cache: "reload" });
  report("icon", JSON.stringify({ href: link.href, svgStatus: svg.status, svgType: svg.headers.get("content-type"), svg: await svg.text(), icoStatus: ico.status, icoType: ico.headers.get("content-type") }));
})();
</script></body></html>`)
	reports := captureReports(t, env)
	got := reportFromChrome(t, chrome, env.ts.URL+"/a/"+key+"/artifact.html", reports, "icon")
	for _, want := range []string{`"href":"` + env.ts.URL + `/favicon.svg?v=`, `"svgStatus":200`, `"svgType":"image/svg+xml"`, `#6FA1CB`, `"icoStatus":200`, `"icoType":"image/x-icon"`} {
		if !strings.Contains(got, want) {
			t.Errorf("tab icon report lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "#c9a15a") {
		t.Errorf("a gold mark was served: %s", got)
	}
}

// Computed styles in real Chrome for everything the whiteboard frame draws
// around the diagram: the frame page itself, the note field, the "Click to
// edit" hint, the banners, and Excalidraw's own accent and surfaces. They
// have to be the forum tokens of the active theme.
const whiteboardThemeArtifact = `<!doctype html><html data-fr-theme="__THEME__"><head><meta charset="utf-8"><title>wb</title>
<link rel="stylesheet" href="/forum-assets/forum-tokens.css">
<link rel="stylesheet" href="/whiteboard-assets/whiteboard.css">
</head><body style="margin:0">
<div id="wbHeader"><input id="wbNote" value="x"></div>
<div class="wb-banner" id="b1">banner</div>
<div class="wb-status" id="b2">status</div>
<span class="wb-activate-label" id="hint">Click to edit</span>
<div class="excalidraw __DARK__" id="ex"><i id="primary" style="color:var(--color-primary);background:var(--island-bg-color)">p</i><b id="surface" style="background:var(--default-bg-color);color:var(--text-primary-color)">s</b></div>
<script>
const report = (k, v) => fetch("/__report?k=" + k + "&v=" + encodeURIComponent(v), { mode: "no-cors" });
const cs = (id) => getComputedStyle(document.getElementById(id));
setTimeout(() => report("styles", JSON.stringify({
  body: getComputedStyle(document.body).backgroundColor,
  bodyText: getComputedStyle(document.body).color,
  noteBorder: cs("wbNote").borderTopColor, noteBg: cs("wbNote").backgroundColor,
  header: cs("wbHeader").backgroundColor,
  banner: cs("b1").backgroundColor, status: cs("b2").backgroundColor,
  hintBg: cs("hint").backgroundColor, hintBorder: cs("hint").borderTopColor,
  primary: cs("primary").color, island: cs("primary").backgroundColor,
  exBg: cs("surface").backgroundColor, exText: cs("surface").color,
})), 800);
</script></body></html>`

func TestWhiteboardFrame_RealChrome_UsesForumTokensInBothThemes(t *testing.T) {
	chrome := headlessChrome(t)
	for theme, want := range map[string]map[string]string{
		"dark": {
			"body": "rgb(21, 23, 26)", "bodyText": "rgb(233, 234, 236)", "noteBorder": "rgb(58, 63, 71)", "noteBg": "rgb(28, 31, 36)",
			"header": "rgb(32, 36, 42)", "hintBg": "rgb(28, 31, 36)", "primary": "rgb(111, 161, 203)", "island": "rgb(28, 31, 36)",
			"exBg": "rgb(21, 23, 26)", "exText": "rgb(233, 234, 236)",
		},
		"light": {
			"body": "rgb(242, 241, 236)", "bodyText": "rgb(26, 29, 34)", "noteBorder": "rgb(194, 192, 184)", "noteBg": "rgb(255, 255, 255)",
			"header": "rgb(236, 235, 228)", "hintBg": "rgb(255, 255, 255)", "primary": "rgb(31, 78, 121)", "island": "rgb(255, 255, 255)",
			"exBg": "rgb(242, 241, 236)", "exText": "rgb(26, 29, 34)",
		},
	} {
		t.Run(theme, func(t *testing.T) {
			env := newEnv(t, time.Minute)
			key := env.open().Key
			dark := ""
			if theme == "dark" {
				dark = "theme--dark"
			}
			env.setArtifact(strings.NewReplacer("__THEME__", theme, "__DARK__", dark).Replace(whiteboardThemeArtifact))
			reports := captureReports(t, env)
			got := reportFromChrome(t, chrome, env.ts.URL+"/a/"+key+"/artifact.html?theme="+theme, reports, "styles")
			t.Logf("%s styles: %s", theme, got)
			for name, value := range want {
				if !strings.Contains(got, `"`+name+`":"`+value+`"`) {
					t.Errorf("%s: %s is not %s in %s", theme, name, value, got)
				}
			}
			for _, cream := range []string{"rgb(255, 251, 243)", "rgb(244, 201, 93)", "rgb(105, 101, 219)", "rgb(168, 165, 255)"} {
				if strings.Contains(got, cream) {
					t.Errorf("%s: an old upstream/Excalidraw colour %s is still drawn: %s", theme, cream, got)
				}
			}
		})
	}
}

// captureReports wraps the test server so /__report?k=&v= stores a value.
func captureReports(t *testing.T, env *testEnv) func(string) (string, bool) {
	t.Helper()
	var mu sync.Mutex
	reports := map[string]string{}
	env.handle("/__report", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reports[r.URL.Query().Get("k")] = r.URL.Query().Get("v")
		mu.Unlock()
	})
	return func(k string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		v, ok := reports[k]
		return v, ok
	}
}

// reportFromChrome opens url in headless Chrome and waits for the page to
// report key. A freshly created profile's very first launch sometimes never
// loads the page, so a silent first attempt is relaunched once.
func reportFromChrome(t *testing.T, chrome, url string, get func(string) (string, bool), key string) string {
	t.Helper()
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check",
			"--window-size=1300,900", "--user-data-dir="+t.TempDir(), url)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("chrome: %v", err)
		}
		kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if v, ok := get(key); ok {
				kill()
				return v
			}
		}
		kill()
	}
	t.Fatalf("never got the %q report", key)
	return ""
}
