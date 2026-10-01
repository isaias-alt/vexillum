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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	data := chromeData{Name: name, Src: src, Boot: chromeBoot{Key: key, Token: token, File: file, Name: name, ArtifactSrc: src}}
	if err := chromeTemplate.Execute(w, data); err != nil {
		// Headers are already out; nothing better than logging.
		s.hub.logf("forum: rendering session page: %v", err)
	}
}

func (s *Server) handleChromeAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var contentType string
	switch name {
	case "forum.css":
		contentType = "text/css; charset=utf-8"
	case "forum-chrome.js", "forum-sdk.js":
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
		s.serveArtifactHTML(w, file)
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

func (s *Server) serveArtifactHTML(w http.ResponseWriter, file string) {
	source, err := readArtifactFile(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc := injectSDK(source)
	if len(ExtractMermaidSources(doc)) > 0 {
		doc = injectEmbedScript(doc)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(doc))
}

const (
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
