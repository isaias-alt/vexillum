package forum

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Server) pageRoutes() {
	s.mux.HandleFunc("GET /session/{key}", s.handleSessionPage)
	s.mux.HandleFunc("GET /favicon.svg", s.handleFavicon)
	s.mux.HandleFunc("GET /forum-assets/{name}", s.handleChromeAsset)
	s.mux.HandleFunc("GET /a/{key}/{path...}", s.handleArtifactFile)
}

var chromeTemplate = template.Must(template.New("chrome").Parse(chromeHTML))

// chromeBoot is the data the session page hands its script. It is rendered
// into a non-executed application/json block, so it is data, not code.
type chromeBoot struct {
	Key         string `json:"key"`
	Token       string `json:"token"`
	File        string `json:"file"`
	Name        string `json:"name"`
	ArtifactSrc string `json:"artifact_src"`
}

type chromeData struct {
	Name string
	Boot chromeBoot
	Src  string
}

// handleSessionPage serves the review chrome for one session: the artifact
// in a sandboxed iframe next to the conversation panel. The page carries
// the session token, which is why it is only ever served as a navigation to
// this origin and never with permissive CORS.
func (s *Server) handleSessionPage(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !ValidSessionKey(key) {
		http.NotFound(w, r)
		return
	}
	token, err := s.hub.Token(key)
	if err != nil {
		if errors.Is(err, ErrNoSession) {
			http.Error(w, "No forum session here. Ask your agent to run `vexillum forum <file>` again.", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	file, err := s.hub.File(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := filepath.Base(file)
	src := "/a/" + key + "/" + url.PathEscape(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data: blob:; frame-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	data := chromeData{Name: name, Src: src, Boot: chromeBoot{Key: key, Token: token, File: file, Name: name, ArtifactSrc: src}}
	if err := chromeTemplate.Execute(w, data); err != nil {
		// Headers are already out; nothing better than logging.
		s.hub.logf("forum: rendering session page: %v", err)
	}
}

// handleFavicon serves the vexillum icon for the chrome and for artifacts
// that do not declare their own.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(assetsFS, "assets/favicon.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func (s *Server) handleChromeAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var contentType string
	switch name {
	case "forum.css", "forum-tokens.css", "forum-artifact.css":
		contentType = "text/css; charset=utf-8"
	case "forum-chrome.js", "forum-sdk.js", "forum-theme.js", "forum-prefs.js", "forum-frame.js":
		contentType = "text/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(assetsFS, "assets/chrome/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The SDK is loaded by the sandboxed artifact (an opaque origin).
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(data)
}

// handleArtifactFile serves the artifact itself (with window.forum, and the
// whiteboard embed when it has Mermaid diagrams, injected) and, next to it,
// the local assets it references by relative path - confined to the
// artifact's own directory by os.Root, which also refuses symlinks that
// leave it. Dot-files are never served.
func (s *Server) handleArtifactFile(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !ValidSessionKey(key) {
		http.NotFound(w, r)
		return
	}
	file, err := s.hub.File(key)
	if err != nil {
		if errors.Is(err, ErrNoSession) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rel := path.Clean("/" + r.PathValue("path"))[1:]
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")

	if rel == "" || rel == filepath.Base(file) {
		s.serveArtifactHTML(w, file, r.URL.Query().Get("theme"))
		return
	}
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			http.NotFound(w, r)
			return
		}
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	// The artifact runs in a sandboxed iframe (opaque origin), so fonts and
	// module scripts it loads from here are cross-origin requests.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func readArtifactFile(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("reading artifact %s: %w", file, err)
	}
	return string(data), nil
}

// serveArtifactHTML serves the artifact with window.forum and, unless the
// artifact opts out, the forum content stylesheet injected. theme is the
// chrome's current theme ("dark" or "light", anything else means dark), put on
// <html> in the response itself so the first paint already has it.
func (s *Server) serveArtifactHTML(w http.ResponseWriter, file, theme string) {
	source, err := readArtifactFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc := injectSDK(source)
	if wantsForumStyle(doc) {
		doc = injectStyles(doc, theme)
	}
	doc = setThemeAttr(doc, theme)
	doc = injectFavicon(doc)
	if len(ExtractMermaidSources(doc)) > 0 {
		doc = injectEmbedScript(doc)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(doc))
}

const (
	stylesTags     = `<link rel="stylesheet" href="/forum-assets/forum-tokens.css"><link rel="stylesheet" href="/forum-assets/forum-artifact.css">`
	faviconTag     = `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`
	sdkScriptTag   = `<script src="/forum-assets/forum-sdk.js"></script>`
	embedScriptTag = `<script src="/whiteboard-embed.js"></script>`
)

var (
	headOpenPattern = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlOpenPattern = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
	doctypePattern  = regexp.MustCompile(`(?i)<!doctype[^>]*>`)
)

// injectSDK puts the window.forum script as early as it can go - right
// after <head>, else <html>, else the doctype (never before it, which would
// push the page into quirks mode) - so the API exists before the artifact's
// own scripts run.
func injectSDK(doc string) string {
	for _, re := range []*regexp.Regexp{headOpenPattern, htmlOpenPattern, doctypePattern} {
		if loc := re.FindStringIndex(doc); loc != nil {
			return doc[:loc[1]] + sdkScriptTag + doc[loc[1]:]
		}
	}
	return sdkScriptTag + doc
}

var (
	metaTagPattern    = regexp.MustCompile(`(?i)<meta\b[^>]*>`)
	forumStyleName    = regexp.MustCompile(`(?i)\bname\s*=\s*["']?forum-style["']?[\s/>]`)
	forumStyleContent = regexp.MustCompile(`(?i)\bcontent\s*=\s*["']?([a-z]+)["']?[\s/>]`)
	styleBlockPattern = regexp.MustCompile(`(?i)<style[\s>]`)
	linkTagPattern    = regexp.MustCompile(`(?i)<link\b[^>]*>`)
	stylesheetRel     = regexp.MustCompile(`(?i)\brel\s*=\s*["']?[^"'>]*\bstylesheet\b`)
	scriptSrcPattern  = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=\s*["']?([^"'\s>]+)`)
	cssFrameworkURL   = regexp.MustCompile(`(?i)tailwind|daisyui|bootstrap|bulma|unocss|twind|windicss|materialize|semantic-ui|picocss|water\.css|mvp\.css`)
	htmlTagThemeAttr  = regexp.MustCompile(`(?i)\sdata-fr-theme\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	iconLinkPattern   = regexp.MustCompile(`(?i)<link\b[^>]*\brel\s*=\s*["']?(?:shortcut\s+)?icon\b`)
	headClosePattern  = regexp.MustCompile(`(?i)</head\s*>`)
)

// injectFavicon gives an artifact the vexillum icon unless it declares its
// own. The link goes just before </head>; a document with no head is left
// alone (the browser then asks for /favicon.ico, as it would unreviewed).
func injectFavicon(doc string) string {
	if iconLinkPattern.MatchString(doc) {
		return doc
	}
	if loc := headClosePattern.FindStringIndex(doc); loc != nil {
		return doc[:loc[0]] + faviconTag + doc[loc[0]:]
	}
	return doc
}

// wantsForumStyle decides whether the artifact gets the forum content
// stylesheet. <meta name="forum-style" content="none"> always opts out and
// content="on" always forces it. Otherwise the artifact gets it only when it
// brings no styling of its own (see bringsOwnStyle): forum gives an unstyled
// artifact an identity, and never competes with one that already has a look.
func wantsForumStyle(doc string) bool {
	switch forumStyleMeta(doc) {
	case "none":
		return false
	case "on":
		return true
	}
	return !bringsOwnStyle(doc)
}

// forumStyleMeta returns the lowercased content of <meta name="forum-style">
// ("none", "on") or "". If several are present, "none" wins over "on".
func forumStyleMeta(doc string) string {
	mode := ""
	for _, tag := range metaTagPattern.FindAllString(doc, -1) {
		tag += " "
		if !forumStyleName.MatchString(tag) {
			continue
		}
		m := forumStyleContent.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		switch strings.ToLower(m[1]) {
		case "none":
			return "none"
		case "on":
			mode = "on"
		}
	}
	return mode
}

// bringsOwnStyle reports whether the artifact styles itself: a <style> block,
// a stylesheet <link>, or a CSS framework loaded from a CDN (Tailwind,
// daisyUI, Bootstrap and the like are a <script> or <link> that styles the
// page, so they count). Inline style="" attributes alone do not: they are the
// recommended way to tweak a forum-styled page.
func bringsOwnStyle(doc string) bool {
	if styleBlockPattern.MatchString(doc) {
		return true
	}
	for _, tag := range linkTagPattern.FindAllString(doc, -1) {
		if stylesheetRel.MatchString(tag) {
			return true
		}
	}
	for _, m := range scriptSrcPattern.FindAllStringSubmatch(doc, -1) {
		if cssFrameworkURL.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// Canvas colors (the --fr-bg token of each theme, see forum-tokens.css; a test
// keeps them in step) for the inline boot style: the page paints in its theme
// before the linked stylesheets have arrived.
const (
	bootBgDark  = "#15171A"
	bootBgLight = "#F2F1EC"
)

// bootStyle is the first thing in a forum-styled artifact: the document's
// canvas color and color-scheme, inline, so the first paint is already in the
// chrome's theme instead of the browser's default white. It joins the same
// low-priority layer as the stylesheet it precedes.
func bootStyle(theme string) string {
	bg, scheme := bootBgDark, "dark"
	if theme == "light" {
		bg, scheme = bootBgLight, "light"
	}
	return "<style>@layer forum-artifact{html{background:" + bg + ";color-scheme:" + scheme + "}}</style>"
}

// injectStyles links the forum tokens and content stylesheet at the very top
// of the document, ahead of everything the artifact brings (after the inline
// boot style that makes the first paint themed). The stylesheet sits in a
// low-priority cascade layer, so the artifact's own styles win whatever their
// order or specificity.
func injectStyles(doc, theme string) string {
	tags := bootStyle(theme) + stylesTags
	for _, re := range []*regexp.Regexp{headOpenPattern, htmlOpenPattern, doctypePattern} {
		if loc := re.FindStringIndex(doc); loc != nil {
			return doc[:loc[1]] + tags + doc[loc[1]:]
		}
	}
	return tags + doc
}

// setThemeAttr puts data-fr-theme on <html>, so the tokens resolve to the
// chrome's theme from the first paint. A document with no <html> tag is left
// alone: the SDK applies the theme as soon as the chrome tells it.
func setThemeAttr(doc, theme string) string {
	if theme != "light" {
		theme = "dark"
	}
	loc := htmlOpenPattern.FindStringSubmatchIndex(doc)
	if loc == nil {
		return doc
	}
	tag := doc[loc[0]:loc[1]]
	tag = htmlTagThemeAttr.ReplaceAllString(tag, "")
	tag = strings.TrimSuffix(tag, ">") + ` data-fr-theme="` + theme + `">`
	return doc[:loc[0]] + tag + doc[loc[1]:]
}

// injectEmbedScript appends the whiteboard embed just before </body> (or at
// the end, if the document has no closing body tag).
func injectEmbedScript(doc string) string {
	if idx := strings.LastIndex(strings.ToLower(doc), "</body>"); idx >= 0 {
		return doc[:idx] + embedScriptTag + "\n" + doc[idx:]
	}
	return doc + "\n" + embedScriptTag
}

// artifactVersion fingerprints the artifact file (mtime + size) so the
// session page can reload the iframe when the agent edits it.
func artifactVersion(file string) string {
	info, err := os.Stat(file)
	if err != nil {
		return "missing"
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatInt(info.Size(), 10)
}
