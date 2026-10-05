//go:build unix

package forum_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
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
// at the forum tokens, in both of its theme blocks, and must not carry the
// converter-era hard-coded accent of the editor's own palette.
func TestWhiteboardCSS_ThemesExcalidrawWithForumTokens(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, css := env.get("/whiteboard-assets/whiteboard.css")
	for _, want := range []string{
		"--color-primary: var(--fr-accent)",
		"--island-bg-color: var(--fr-surface)",
		"--default-bg-color: var(--fr-bg)",
		"--color-selection: var(--fr-accent)",
		"--link-color: var(--fr-accent)",
		"--text-primary-color: var(--fr-text)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("whiteboard.css does not contain %q", want)
		}
	}
	// The frame declares the scheme of its palette, so the iframe stays
	// transparent over the embed's themed backdrop while it loads.
	if !strings.Contains(css, `:root[data-fr-theme=light]{color-scheme:light}`) {
		t.Error("whiteboard.css does not declare the frame's color-scheme per palette")
	}
	// Keyboard focus is always visible.
	if !strings.Contains(css, ":focus-visible") {
		t.Error("whiteboard.css has no :focus-visible ring")
	}
	// The override has to win over both of the editor's theme blocks, so its
	// selectors are the editor's own, prefixed to be more specific.
	for _, sel := range []string{"body .vxb-board .excalidraw,", "body .vxb-board .excalidraw.theme--dark{"} {
		if !strings.Contains(css, sel) {
			t.Errorf("whiteboard.css does not override the editor with %q", sel)
		}
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

// Computed styles, in real Chrome, of everything the frame draws around the
// diagram and of Excalidraw's own accent and surfaces: they have to be the
// forum tokens of the active theme.
const frameStylesScript = `
const boardFrame0 = await boardReady(0);
const d = boardFrame0.contentDocument, w = boardFrame0.contentWindow;
const q = (sel) => d.querySelector(sel);
const css = (el, name) => w.getComputedStyle(el)[name];
const prop = (el, name) => w.getComputedStyle(el).getPropertyValue(name).trim().toLowerCase();
const excalidraw = q(".excalidraw");
report("result", JSON.stringify({
  body: css(d.body, "backgroundColor"),
  bodyText: css(d.body, "color"),
  bar: css(q(".vxb-bar"), "backgroundColor"),
  barLine: css(q(".vxb-bar"), "borderBottomColor"),
  remarkLine: css(q(".vxb-remark"), "borderTopColor"),
  remarkBg: css(q(".vxb-remark"), "backgroundColor"),
  queueBg: css(q(".vxb-btn-primary"), "backgroundColor"),
  queueText: css(q(".vxb-btn-primary"), "color"),
  fullBg: css(q(".vxb-btn:not(.vxb-btn-primary)"), "backgroundColor"),
  fullText: css(q(".vxb-btn:not(.vxb-btn-primary)"), "color"),
  primary: prop(excalidraw, "--color-primary"),
  island: prop(excalidraw, "--island-bg-color"),
  paper: prop(excalidraw, "--default-bg-color"),
  ink: prop(excalidraw, "--text-primary-color"),
  host: css(q(".vxb-board"), "backgroundColor"),
  hint: q(".HintViewer") ? css(q(".HintViewer"), "color") : "no hint",
  darkClass: excalidraw.classList.contains("theme--dark"),
}));
`

func TestWhiteboardFrame_RealChrome_UsesForumTokensInBothThemes(t *testing.T) {
	type want = map[string]string
	for theme, expect := range map[string]want{
		"dark": {
			"body": "rgb(21, 23, 26)", "bodyText": "rgb(233, 234, 236)", "bar": "rgb(32, 36, 42)", "barLine": "rgb(41, 45, 51)",
			"remarkLine": "rgb(58, 63, 71)", "remarkBg": "rgb(28, 31, 36)",
			"queueBg": "rgb(111, 161, 203)", "queueText": "rgb(15, 34, 51)", "fullBg": "rgb(28, 31, 36)", "fullText": "rgb(233, 234, 236)", "hint": "rgb(160, 163, 169)",
			"primary": "#6fa1cb", "island": "#1c1f24", "paper": "#15171a", "ink": "#e9eaec",
		},
		"light": {
			"body": "rgb(242, 241, 236)", "bodyText": "rgb(26, 29, 34)", "bar": "rgb(236, 235, 228)", "barLine": "rgb(218, 218, 212)",
			"remarkLine": "rgb(194, 192, 184)", "remarkBg": "rgb(255, 255, 255)",
			"queueBg": "rgb(31, 78, 121)", "queueText": "rgb(255, 255, 255)", "fullBg": "rgb(255, 255, 255)", "fullText": "rgb(26, 29, 34)", "hint": "rgb(90, 94, 102)",
			"primary": "#1f4e79", "island": "#ffffff", "paper": "#f2f1ec", "ink": "#1a1d22",
		},
	} {
		t.Run(theme, func(t *testing.T) {
			env := newEnv(t, time.Minute)
			key := env.open().Key
			got := runLab(t, env, key, theme, []string{"flowchart LR\n  A --> B"}, frameStylesScript, 1300, 900, 90*time.Second)
			t.Logf("%s styles: %s", theme, got)
			for name, value := range expect {
				if !strings.Contains(got, `"`+name+`":"`+value+`"`) {
					t.Errorf("%s: %s is not %s in %s", theme, name, value, got)
				}
			}
			wantDark := "false"
			if theme == "dark" {
				wantDark = "true"
			}
			if !strings.Contains(got, `"darkClass":`+wantDark) {
				t.Errorf("%s: the editor is in the wrong theme: %s", theme, got)
			}
			// The editor's own accent (and the forum's old gold) must be gone.
			for _, old := range []string{"rgb(105, 101, 219)", "rgb(168, 165, 255)", "#6965db", "#a8a5ff", "rgb(244, 201, 93)"} {
				if strings.Contains(strings.ToLower(got), old) {
					t.Errorf("%s: the editor's own colour %s is still drawn: %s", theme, old, got)
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

// Every text/background pairing the whiteboard CSS and the embed use, computed
// from the shipped tokens of each theme: at least 4.5:1 (WCAG AA for normal
// text). Soft (translucent) fills are composited over the surface they sit on.
func TestWhiteboardTextContrast_MeetsAAInBothThemes(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, css := env.get("/forum-assets/forum-tokens.css")
	lightAt := strings.Index(css, `:root[data-fr-theme="light"]`)
	if lightAt < 0 {
		t.Fatal("no light token block")
	}
	themes := map[string]map[string][4]float64{"dark": parseColourTokens(css[:lightAt]), "light": parseColourTokens(css[lightAt:])}
	// The light block only redefines what differs from dark; the fonts and
	// radii are not colours, and every colour is redefined, so no fallback.
	pairs := []struct{ fg, bg, over string }{
		{"text", "bg", ""}, {"text", "surface", ""}, {"text", "surface-sunken", ""},
		{"text-secondary", "bg", ""}, {"text-secondary", "surface", ""},
		{"accent-contrast", "accent", ""}, {"accent-contrast", "accent-hover", ""},
		{"danger", "surface", ""}, {"danger", "bg", ""},
		{"success", "surface", ""},
		{"text", "bronze-soft", "bg"}, {"text", "bronze-soft", "surface-sunken"},
		{"text", "accent-soft", "bg"},
	}
	for theme, tokens := range themes {
		for _, p := range pairs {
			fg, bg := tokens[p.fg], tokens[p.bg]
			if fg == ([4]float64{}) || bg == ([4]float64{}) {
				t.Fatalf("%s: token --fr-%s or --fr-%s not found", theme, p.fg, p.bg)
			}
			if p.over != "" {
				bg = compositeOver(bg, tokens[p.over])
			}
			if ratio := contrastRatio(fg, bg); ratio < 4.5 {
				t.Errorf("%s: --fr-%s on --fr-%s %s is %.2f:1, want at least 4.5:1", theme, p.fg, p.bg, p.over, ratio)
			}
		}
	}
}

var colourToken = regexp.MustCompile(`--fr-([a-z-]+):\s*(#[0-9A-Fa-f]{6}|rgba\([^)]*\))`)

// parseColourTokens reads "--fr-name: #RRGGBB" and "rgba(r,g,b,a)" values into
// [r g b a] with r, g, b in 0..255.
func parseColourTokens(block string) map[string][4]float64 {
	out := map[string][4]float64{}
	for _, m := range colourToken.FindAllStringSubmatch(block, -1) {
		v := m[2]
		var c [4]float64
		if strings.HasPrefix(v, "#") {
			var r, g, b int
			_, _ = fmt.Sscanf(v[1:], "%02x%02x%02x", &r, &g, &b)
			c = [4]float64{float64(r), float64(g), float64(b), 1}
		} else {
			var r, g, b, a float64
			_, _ = fmt.Sscanf(strings.NewReplacer(" ", "", "rgba(", "", ")", "").Replace(v), "%f,%f,%f,%f", &r, &g, &b, &a)
			c = [4]float64{r, g, b, a}
		}
		out[m[1]] = c
	}
	return out
}

func compositeOver(top, below [4]float64) [4]float64 {
	a := top[3]
	return [4]float64{top[0]*a + below[0]*(1-a), top[1]*a + below[1]*(1-a), top[2]*a + below[2]*(1-a), 1}
}

func luminance(c [4]float64) float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func contrastRatio(a, b [4]float64) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
