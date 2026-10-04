package forum

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// assetsFS embeds the favicon (assets/favicon.svg, a copy of the vexillum
// icon in site/app/icon.svg, and assets/favicon.ico, the same mark rasterised
// to 16/32/48px PNG entries for the browsers and requests that fall back to
// /favicon.ico), the review chrome (assets/chrome/), the whiteboard browser bundle (assets/whiteboard/ - built
// by tools/whiteboard-bundle/build.js, never at vexillum's own build or
// install time, see that directory's README) and whiteboard-embed.js, the
// small hand-written chrome-side integration script served unbundled.
//
//go:embed assets/whiteboard assets/whiteboard-embed.js assets/chrome assets/favicon.svg assets/favicon.ico
var assetsFS embed.FS

// chromeHTML is the session page's template (assets/chrome/chrome.html);
// its styles live in assets/chrome/forum.css and its behavior in
// forum-chrome.js, so the whole look of the chrome is that one stylesheet.
//
//go:embed assets/chrome/chrome.html
var chromeHTML string

const whiteboardAssetsPrefix = "/whiteboard-assets/"

// whiteboardJSGzip is whiteboard.js pre-gzipped at build time
// (tools/whiteboard-bundle/build.js), served with Content-Encoding: gzip
// instead of gzipping ~7.3 MB on every request. Every modern browser sends
// Accept-Encoding: gzip, so this handler does not bother negotiating it.
const whiteboardJSGzipName = "whiteboard.js.gz"

// whiteboardAssetsHandler serves whiteboard.js/.css/fonts/* from the
// embedded bundle under /whiteboard-assets/. Access-Control-Allow-Origin: *
// is required because the whiteboard frame runs in a sandboxed iframe
// without allow-same-origin (an opaque, "null" origin), and a canvas font
// fetch from an opaque origin is CORS-gated.
func whiteboardAssetsHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets/whiteboard")
	if err != nil {
		// assets/whiteboard is embedded above; a failure here means the
		// embed itself is broken, which go build would already have caught.
		panic(fmt.Sprintf("forum: assets/whiteboard not embedded correctly: %v", err))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(r.URL.Path, whiteboardAssetsPrefix)
		if requested == "" || strings.Contains(requested, "..") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")

		if requested == "whiteboard.js" {
			serveGzipFile(w, r, sub, whiteboardJSGzipName, "text/javascript; charset=utf-8")
			return
		}

		info, err := fs.Stat(sub, path.Clean(requested))
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(sub, path.Clean(requested))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, requested, time.Time{}, bytes.NewReader(data))
	})
}

func serveGzipFile(w http.ResponseWriter, r *http.Request, sub fs.FS, name, contentType string) {
	data, err := fs.ReadFile(sub, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// whiteboardEmbedScript returns whiteboard-embed.js's source, injected into
// the served artifact page before </body> when it contains at least one
// `.mermaid` container - see server.go.
func whiteboardEmbedScript() ([]byte, error) {
	return assetsFS.ReadFile("assets/whiteboard-embed.js")
}
