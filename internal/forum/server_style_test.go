package forum_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestArtifactStyle_RouteServesTheContentStylesheetInAnInertLayer(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, css := env.get("/forum-assets/forum-artifact.css")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("forum-artifact.css = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// Everything is in one cascade layer, so any style the artifact writes wins; a
	// rule outside it would beat (or break) the artifact's own look.
	if !strings.Contains(css, "@layer forum-artifact {") {
		t.Fatal("the stylesheet must live in the forum-artifact layer")
	}
	if strings.Count(css, "@layer") != 1 || strings.Count(strings.Split(css, "@layer forum-artifact {")[0], "{") != 0 {
		t.Error("nothing may sit outside the layer")
	}
	for _, want := range []string{".fr-card", ".fr-badge", ".fr-btn--primary", ".fr-callout", ".fr-choice", ".fr-form", ".fr-table-wrap", ".fr-figure", ".fr-node", ".fr-grid", "var(--fr-accent)"} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	if strings.Contains(css, "var(--fr-selection") {
		t.Error("tyrian is reserved for annotations and must not be used for content")
	}
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") || strings.Contains(css, "@import") {
		t.Error("the stylesheet must not pull anything from the network")
	}
	if strings.Contains(css, "max-width: 72rem") && !strings.Contains(css, ".fr-page { max-width") {
		t.Error("page layout must stay opt-in, behind a class")
	}
}

// The three branches of the default: an artifact with no styling of its own
// gets the forum look (auto); one that brings its own is left alone (auto), a
// meta forces either way.

func (e *testEnv) styled(doc string) bool {
	e.t.Helper()
	open := e.open()
	e.setArtifact(doc)
	_, got := e.get("/a/" + open.Key + "/artifact.html")
	if !strings.Contains(got, "forum-sdk.js") {
		e.t.Fatalf("window.forum must always be injected:\n%s", got)
	}
	has := strings.Contains(got, "forum-artifact.css")
	if has != strings.Contains(got, "forum-tokens.css") {
		e.t.Errorf("tokens and stylesheet must come together:\n%s", got)
	}
	return has
}

func TestArtifactStyle_AutoInjectsOnlyIntoUnstyledArtifacts(t *testing.T) {
	env := newEnv(t, time.Minute)
	for name, doc := range map[string]string{
		"plain page":             `<!doctype html><html><head><title>t</title></head><body><h1>hi</h1></body></html>`,
		"fragment":               `<p>bare</p>`,
		"inline style attribute": `<html><head></head><body><p style="margin:0">x</p></body></html>`,
		"icon and preconnect":    `<html><head><link rel="icon" href="/x.png"><link rel="preconnect" href="https://fonts.gstatic.com"><link rel="icon" href="/foundation.ico"></head><body>x</body></html>`,
		"non-style script":       `<html><head><script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script><script src="app.js"></script></head><body>x</body></html>`,
		"unrelated meta":         `<html><head><meta name="viewport" content="width=device-width"><meta name="description" content="none"></head><body>x</body></html>`,
		"forum-style default":    `<html><head><meta name="forum-style" content="default"></head><body>x</body></html>`,
	} {
		if !env.styled(doc) {
			t.Errorf("%s: an artifact with no styling of its own must get the forum style", name)
		}
	}
}

func TestArtifactStyle_AutoLeavesSelfStyledArtifactsAlone(t *testing.T) {
	env := newEnv(t, time.Minute)
	for name, doc := range map[string]string{
		"style block":            `<html><head><style>body{background:#fff}</style></head><body>x</body></html>`,
		"style block with attrs": `<html><head><STYLE media="screen">p{}</STYLE></head><body>x</body></html>`,
		"stylesheet link":        `<html><head><link rel="stylesheet" href="site.css"></head><body>x</body></html>`,
		"stylesheet, href first": `<html><head><link href="https://fonts.googleapis.com/css2?family=Inter" rel='stylesheet'></head><body>x</body></html>`,
		"alternate stylesheet":   `<html><head><link rel="alternate stylesheet" href="dark.css"></head><body>x</body></html>`,
		"tailwind play cdn":      `<html><head><script src="https://cdn.tailwindcss.com"></script></head><body class="p-4">x</body></html>`,
		"tailwind browser v4":    `<html><head><script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4.2.4/dist/index.global.js"></script></head><body>x</body></html>`,
		"daisyui stylesheet":     `<html><head><link href="https://cdn.jsdelivr.net/npm/daisyui@5/dist/full.css" rel="stylesheet"></head><body>x</body></html>`,
		"bootstrap script":       `<html><head><script src="https://cdn.jsdelivr.net/npm/bootstrap@5/dist/js/bootstrap.bundle.min.js"></script></head><body>x</body></html>`,
		"style in the body":      `<html><head></head><body><style>.a{}</style>x</body></html>`,
	} {
		if env.styled(doc) {
			t.Errorf("%s: a self-styled artifact must not get the forum style", name)
		}
	}
}

func TestArtifactStyle_MetaForcesOnOrOffWhateverTheArtifactBrings(t *testing.T) {
	env := newEnv(t, time.Minute)
	own := `<style>body{background:#fff}</style>`
	for name, c := range map[string]struct {
		doc  string
		want bool
	}{
		"on, own style":            {`<html><head><meta name="forum-style" content="on">` + own + `</head><body>x</body></html>`, true},
		"on, content before name":  {`<html><head><meta content="on" name="forum-style"/>` + own + `</head><body>x</body></html>`, true},
		"on, tailwind":             {`<html><head><META NAME='forum-style' CONTENT='ON'><script src="https://cdn.tailwindcss.com"></script></head><body>x</body></html>`, true},
		"on, nothing else":         {`<html><head><meta name="forum-style" content="on"></head><body>x</body></html>`, true},
		"none, nothing else":       {`<html><head><meta name="forum-style" content="none"></head><body>x</body></html>`, false},
		"none, content first":      {`<html><head><meta content="none" name="forum-style"/></head><body>x</body></html>`, false},
		"none, single quotes":      {`<html><head><META NAME='forum-style' CONTENT='none'></head><body>x</body></html>`, false},
		"none beats on":            {`<html><head><meta name="forum-style" content="on"><meta name="forum-style" content="none"></head><body>x</body></html>`, false},
		"other meta saying on":     {`<html><head><meta name="viewport" content="on">` + own + `</head><body>x</body></html>`, false},
		"on does not pick a theme": {`<html><head><meta name="forum-style" content="on"></head><body>x</body></html>`, true},
	} {
		if got := env.styled(c.doc); got != c.want {
			t.Errorf("%s: styled = %v, want %v", name, got, c.want)
		}
	}
}

func TestArtifactStyle_WhenForcedOnItComesBeforeTheArtifactsOwnStyles(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!doctype html><html><head><meta name="forum-style" content="on"><title>t</title><style>body{background:#fff}</style></head><body><p class="fr-card">hi</p></body></html>`)
	_, got := env.get("/a/" + open.Key + "/artifact.html")
	link, own := strings.Index(got, "forum-artifact.css"), strings.Index(got, "<style>body")
	if link < 0 || own < 0 || link > own {
		t.Errorf("forum-artifact.css must come before the artifact's own style (link %d, own %d):\n%s", link, own, got)
	}
}

func TestArtifactStyle_ThemeIsRenderedFromTheQueryAndReachesTheIframe(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!doctype html><html lang="en" data-fr-theme="stale"><head><title>t</title></head><body>x</body></html>`)
	base := "/a/" + open.Key + "/artifact.html"
	for query, want := range map[string]string{"": "dark", "?theme=dark": "dark", "?theme=light": "light", "?theme=<script>": "dark", "?theme=purple": "dark"} {
		_, got := env.get(base + query)
		if !strings.Contains(got, `<html lang="en" data-fr-theme="`+want+`">`) || strings.Count(got, "data-fr-theme") != 1 {
			t.Errorf("%q: html tag should carry exactly one data-fr-theme=%q:\n%.200s", query, want, got)
		}
	}

	// The chrome starts the artifact in its theme, and keeps the iframe in step.
	_, page := env.get("/session/" + open.Key)
	if strings.Contains(page, ` src="/a/`) || !strings.Contains(page, `data-src="/a/`) || !strings.Contains(page, "/forum-assets/forum-frame.js") {
		t.Error("the iframe must be started by forum-frame.js with the theme, not by a static src")
	}
	_, frame := env.get("/forum-assets/forum-frame.js")
	if !strings.Contains(frame, `"?theme=" + window.forumTheme.current()`) {
		t.Error("forum-frame.js must put the chrome's theme in the artifact's address")
	}
	_, chrome := env.get("/forum-assets/forum-chrome.js")
	if !strings.Contains(chrome, `type: "forum:theme"`) || !strings.Contains(chrome, `?theme=`) {
		t.Error("the chrome must send the theme to the artifact and reload it with the theme")
	}
	_, sdk := env.get("/forum-assets/forum-sdk.js")
	if !strings.Contains(sdk, `message.type === "forum:theme"`) || !strings.Contains(sdk, `"data-fr-theme"`) {
		t.Error("the sdk must apply a forum:theme message to the artifact's <html>")
	}
}

// The skill and the playbooks tell agents which classes to use. A class that
// is documented but not defined (or renamed in the stylesheet only) would
// silently produce an unstyled artifact, so every documented fr-* class must
// exist in the stylesheet.
func TestArtifactStyle_DocumentedClassesExist(t *testing.T) {
	cssBytes, err := os.ReadFile("assets/chrome/forum-artifact.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	files, err := filepath.Glob("../../skills/forum/playbooks/*.md")
	if err != nil || len(files) != 5 {
		t.Fatalf("playbooks = %v (%v), want the 5 ported playbooks", files, err)
	}
	files = append(files, "../../skills/forum/SKILL.md")
	// Not preceded by a hyphen or word character, so data-fr-theme and --fr-* tokens are not classes.
	classRe := regexp.MustCompile(`(?:^|[^-\w])(fr-[a-z][a-z0-9-]*)`)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, m := range classRe.FindAllStringSubmatch(text, -1) {
			if class := m[1]; !strings.Contains(css, "."+class) {
				t.Errorf("%s documents class %q, which forum-artifact.css does not define", filepath.Base(file), class)
			}
		}
		// Every playbook must steer away from CDN frameworks and explain when the styles apply.
		if strings.Contains(file, "playbooks") && (!strings.Contains(text, "forum-artifact.css") || !strings.Contains(text, `content="on"`)) {
			t.Errorf("%s needs a forum-artifact.css section that mentions the forum-style meta", filepath.Base(file))
		}
		if strings.HasSuffix(file, "SKILL.md") {
			for _, want := range []string{`content="on"`, `content="none"`, "When the forum styles apply", "<style>", "CDN"} {
				if !strings.Contains(text, want) {
					t.Errorf("SKILL.md must document the style rule: missing %q", want)
				}
			}
		}
	}
}

// Vertical rhythm: a stack's gap is its only spacing and a heading pulls its
// body up, so spacing is never counted twice (margins in a flex column do not
// collapse: gap + heading margin + body margin made the heading look further
// from its own body than from the block above it).
func TestArtifactStyle_StackSpacingIsNotCountedTwice(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, css := env.get("/forum-assets/forum-artifact.css")
	for _, want := range []string{
		".fr-stack > * { margin-block: 0; }",
		".fr-stack > :is(h1, h2, h3, h4, h5, h6) + * { margin-top: calc(var(--fr-space-2) - var(--fr-stack-gap)); }",
		":is(h1, h2, h3, h4, h5, h6) + * { margin-top: 0; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing the rhythm rule %q", want)
		}
	}
}
