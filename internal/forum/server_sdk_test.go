package forum_test

import (
	"strings"
	"testing"
	"time"
)

// bootStyleDark is the inline style that makes the first paint themed (dark by default).
const bootStyleDark = `<style>@layer forum-artifact{html{background:#15171A;color-scheme:dark}}</style>`

// sdkTags are the scripts injected into every artifact: window.forum and the passive layout audit.
const sdkTags = `<script src="/forum-assets/forum-sdk.js"></script><script src="/forum-assets/forum-layout.js"></script>`

const stylesTags = bootStyleDark + `<link rel="stylesheet" href="/forum-assets/forum-tokens.css"><link rel="stylesheet" href="/forum-assets/forum-artifact.css">`

func TestArtifact_SDKInjectedAfterDoctypeAndHead(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	env.setArtifact(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>t</title></head><body><header>h</header></body></html>`)
	resp, body := env.get("/a/" + open.Key + "/artifact.html")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
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
	cases := map[string]string{
		`<!doctype html><p>no head</p>`: `<!doctype html>` + stylesTags + sdkTags + `<p>no head</p>`,
		`<html><body>x</body></html>`:   `<html data-fr-theme="dark">` + stylesTags + sdkTags + `<body>x</body></html>`,
		`<p>bare fragment</p>`:          stylesTags + sdkTags + `<p>bare fragment</p>`,
	}
	for in, want := range cases {
		env.setArtifact(in)
		if _, got := env.get("/a/" + open.Key + "/artifact.html"); got != want {
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
	if _, page := env.get("/session/" + open.Key); !strings.Contains(page, `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`) {
		t.Error("the chrome page does not reference the favicon")
	}
	env.setArtifact(`<!doctype html><html><head><title>t</title></head><body>x</body></html>`)
	if _, got := env.get("/a/" + open.Key + "/artifact.html"); !strings.Contains(got, `href="/favicon.svg"></head>`) {
		t.Errorf("favicon not injected into an artifact without one:\n%s", got)
	}
	for _, own := range []string{`<link rel="icon" href="/mine.png">`, `<link href="/mine.ico" rel='shortcut icon'>`} {
		env.setArtifact(`<html><head>` + own + `</head><body>x</body></html>`)
		if _, got := env.get("/a/" + open.Key + "/artifact.html"); strings.Contains(got, "/favicon.svg") {
			t.Errorf("artifact with its own favicon %q was overridden", own)
		}
	}
}
