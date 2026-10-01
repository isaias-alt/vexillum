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

func TestArtifactStyle_InjectedBeforeTheArtifactsOwnStyles(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!doctype html><html><head><title>t</title><style>body{background:#fff}</style></head><body><p class="fr-card">hi</p></body></html>`)
	_, got := env.get("/a/" + open.Key + "/artifact.html")
	link, own := strings.Index(got, "forum-artifact.css"), strings.Index(got, "<style>body")
	if link < 0 || own < 0 || link > own {
		t.Errorf("forum-artifact.css must come before the artifact's own style (link %d, own %d):\n%s", link, own, got)
	}
	if !strings.Contains(got, "forum-tokens.css") {
		t.Error("the tokens must be linked too")
	}
}

func TestArtifactStyle_OptOutWithMetaForumStyleNone(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	for _, meta := range []string{
		`<meta name="forum-style" content="none">`,
		`<meta content="none" name="forum-style"/>`,
		`<META NAME='forum-style' CONTENT='none'>`,
	} {
		env.setArtifact(`<!doctype html><html><head>` + meta + `<title>t</title></head><body>x</body></html>`)
		_, got := env.get("/a/" + open.Key + "/artifact.html")
		if strings.Contains(got, "forum-artifact.css") || strings.Contains(got, "forum-tokens.css") {
			t.Errorf("%s: an opted-out artifact must get no forum stylesheet:\n%s", meta, got)
		}
		if !strings.Contains(got, "forum-sdk.js") {
			t.Errorf("%s: opting out of the style keeps window.forum", meta)
		}
	}
	// Other values, and other meta tags, are not an opt-out.
	for _, meta := range []string{`<meta name="forum-style" content="default">`, `<meta name="viewport" content="none">`, `<meta name="description" content="forum-style none">`} {
		env.setArtifact(`<html><head>` + meta + `</head><body>x</body></html>`)
		if _, got := env.get("/a/" + open.Key + "/artifact.html"); !strings.Contains(got, "forum-artifact.css") {
			t.Errorf("%s must not opt out", meta)
		}
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
		// Every playbook must steer away from CDN frameworks.
		if strings.Contains(file, "playbooks") && !strings.Contains(text, "forum-artifact.css") {
			t.Errorf("%s has no forum-artifact.css section", filepath.Base(file))
		}
	}
}
