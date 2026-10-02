package forum_test

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// bootStyleDark is the inline style that makes the first paint themed (dark by default).
const bootStyleDark = `<style>@layer forum-artifact{html{background:#15171A;color-scheme:dark}}</style>`

// sdkTags are the scripts injected into every artifact: window.forum and the passive layout audit.
const sdkTags = `<script src="/forum-assets/forum-sdk.js"></script><script src="/forum-assets/forum-layout.js"></script>`

// versionless drops the artifact version the layout script's address carries
// (?av=<mtime:size>), which changes with the file, so the tests can compare
// the rest of the injection exactly.
var avParam = regexp.MustCompile(`\?av=[^"]*`)

func versionless(doc string) string { return avParam.ReplaceAllString(doc, "") }

const stylesTags = bootStyleDark + `<link rel="stylesheet" href="/forum-assets/forum-tokens.css"><link rel="stylesheet" href="/forum-assets/forum-artifact.css">`

func TestArtifact_SDKInjectedAfterDoctypeAndHead(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>t</title></head><body><header>h</header></body></html>`)
	resp, body := env.get("/a/" + open.Key + "/artifact.html")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body = versionless(body)
	if !strings.HasPrefix(body, `<!DOCTYPE html>`) {
		t.Errorf("the doctype must stay first (quirks mode otherwise): %.60s", body)
	}
	// The stylesheets come first (the artifact's own styles still win: they sit in a low-priority layer), then the sdk.
	if !strings.Contains(body, `<head>`+stylesTags+sdkTags+`<meta charset`) {
		t.Errorf("styles and sdk not injected right after <head>:\n%s", body)
	}
	if strings.Contains(body, "whiteboard-embed.js") {
		t.Error("no mermaid container, so no whiteboard embed")
	}
	if strings.Count(body, "forum-sdk.js") != 1 || strings.Count(body, "forum-layout.js") != 1 {
		t.Error("sdk or layout audit injected more than once (e.g. into <header>)")
	}
}

func TestArtifact_SDKInjectionFallbacks(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	// A document with no <head> still gets the vexillum icon, first, where the
	// parser files it under an implicit head (its content-hash query is
	// normalised away here; the favicon tests pin it).
	icon := `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`
	cases := map[string]string{
		`<!doctype html><p>no head</p>`: `<!doctype html>` + icon + stylesTags + sdkTags + `<p>no head</p>`,
		`<html><body>x</body></html>`:   `<html data-fr-theme="dark">` + icon + stylesTags + sdkTags + `<body>x</body></html>`,
		`<p>bare fragment</p>`:          icon + stylesTags + sdkTags + `<p>bare fragment</p>`,
	}
	faviconVersion := regexp.MustCompile(`/favicon\.svg\?v=[0-9a-f]{12}`)
	for in, want := range cases {
		env.setArtifact(in)
		if _, got := env.get("/a/" + open.Key + "/artifact.html"); versionless(faviconVersion.ReplaceAllString(got, "/favicon.svg")) != want {
			t.Errorf("injection of %q\n got: %s\nwant: %s", in, got, want)
		}
	}
}

func TestFavicon_ServedAndInjectedUnlessArtifactHasOwn(t *testing.T) {
	env := newEnv(t, time.Minute)
	resp, body := env.get("/favicon.svg")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/svg+xml" || !strings.Contains(body, "<svg") {
		t.Fatalf("favicon = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	open := env.open()
	if _, page := env.get("/session/" + open.Key); !strings.Contains(page, `<link rel="icon" type="image/svg+xml" href="/favicon.svg?v=`) {
		t.Error("the chrome page does not reference the favicon")
	}
	env.setArtifact(`<!doctype html><html><head><title>t</title></head><body>x</body></html>`)
	if _, got := env.get("/a/" + open.Key + "/artifact.html"); !strings.Contains(got, `<link rel="icon" type="image/svg+xml" href="/favicon.svg?v=`) || !strings.Contains(got, `"></head>`) {
		t.Errorf("favicon not injected into an artifact without one:\n%s", got)
	}
	for _, own := range []string{`<link rel="icon" href="/mine.png">`, `<link href="/mine.ico" rel='shortcut icon'>`} {
		env.setArtifact(`<html><head>` + own + `</head><body>x</body></html>`)
		if _, got := env.get("/a/" + open.Key + "/artifact.html"); strings.Contains(got, "/favicon.svg") {
			t.Errorf("artifact with its own favicon %q was overridden", own)
		}
	}
}

// Which document a layout pass came from must not depend on when it arrives:
// the audit script's address carries the version of the file this very
// response was rendered from, and a later edit changes it.
func TestArtifact_LayoutScriptCarriesTheVersionItWasServedWith(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!doctype html><html><head><title>t</title></head><body>one</body></html>`)
	version := func() string {
		_, body := env.get("/a/" + open.Key + "/artifact.html")
		m := regexp.MustCompile(`forum-layout\.js\?av=([^"]+)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no versioned layout script in:\n%s", body)
		}
		return m[1]
	}
	first := version()
	if again := version(); again != first {
		t.Errorf("the version changed with no edit: %q -> %q", first, again)
	}
	time.Sleep(10 * time.Millisecond)
	env.setArtifact(`<!doctype html><html><head><title>t</title></head><body>two, longer</body></html>`)
	if second := version(); second == first {
		t.Errorf("an edited artifact kept version %q", first)
	}
}
