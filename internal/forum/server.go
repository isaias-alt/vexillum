package forum

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Server serves a single artifact file for one `vexillum forum` process
// lifetime - see the package doc for why there is no session id in any
// route.
type Server struct {
	key      string
	filePath string
	store    *Store
	mux      *http.ServeMux
}

// NewServer builds a Server for filePath (an absolute path to the HTML
// artifact to serve), persisting whiteboard state under store.
func NewServer(filePath string, store *Store) *Server {
	s := &Server{
		key:      SessionKey(filePath),
		filePath: filePath,
		store:    store,
	}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleArtifact)
	s.mux.Handle("GET /whiteboard-assets/", whiteboardAssetsHandler())
	s.mux.HandleFunc("GET /whiteboard-embed.js", s.handleEmbedScript)
	s.mux.HandleFunc("GET /whiteboard-frame", s.handleWhiteboardFrame)
	s.mux.HandleFunc("GET /api/mermaid-sources", s.handleMermaidSources)
	s.mux.HandleFunc("GET /api/whiteboard/{index}", s.handleGetWhiteboard)
	s.mux.HandleFunc("PUT /api/whiteboard/{index}", s.handlePutWhiteboard)
	s.mux.HandleFunc("POST /api/whiteboard/{index}/feedback-files", s.handleFeedbackFiles)
}

func (s *Server) readArtifact() (string, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return "", fmt.Errorf("reading artifact %s: %w", s.filePath, err)
	}
	return string(data), nil
}

// handleArtifact serves the artifact file as-is, except that when it
// contains at least one `.mermaid` container, whiteboard-embed.js is
// injected just before </body> (or appended, if the document has no
// closing body tag) so those containers become editable whiteboards. An
// artifact with no `.mermaid` container is served completely unmodified.
func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	html, err := s.readArtifact()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(ExtractMermaidSources(html)) > 0 {
		html = injectEmbedScript(html)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(html))
}

const embedScriptTag = `<script src="/whiteboard-embed.js"></script>`

func injectEmbedScript(artifactHTML string) string {
	if idx := strings.LastIndex(strings.ToLower(artifactHTML), "</body>"); idx >= 0 {
		return artifactHTML[:idx] + embedScriptTag + "\n" + artifactHTML[idx:]
	}
	return artifactHTML + "\n" + embedScriptTag
}

func (s *Server) handleEmbedScript(w http.ResponseWriter, r *http.Request) {
	script, err := whiteboardEmbedScript()
	if err != nil {
		http.Error(w, "internal error reading whiteboard-embed.js", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(script)
}

const whiteboardFrameHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Whiteboard</title>
<link rel="stylesheet" href="/whiteboard-assets/whiteboard.css">
</head>
<body>
<script src="/whiteboard-assets/whiteboard.js"></script>
</body>
</html>
`

// handleWhiteboardFrame serves the frame page hosted inside the sandboxed
// iframe (allow-scripts allow-popups, no allow-same-origin) that runs the
// Excalidraw editor - both the inline embed and the fullscreen overlay
// point at this same route, differing only in the diagramIndex query
// parameter whiteboard-frame.js reads at boot.
func (s *Server) handleWhiteboardFrame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(whiteboardFrameHTML))
}

func (s *Server) handleMermaidSources(w http.ResponseWriter, r *http.Request) {
	html, err := s.readArtifact()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": ExtractMermaidSources(html)})
}

func parseDiagramIndex(r *http.Request) (int, error) {
	raw := r.PathValue("index")
	index, err := strconv.Atoi(raw)
	if err != nil || !ValidDiagramIndex(index) {
		return 0, fmt.Errorf("invalid diagram index %q", raw)
	}
	return index, nil
}

func (s *Server) handleGetWhiteboard(w http.ResponseWriter, r *http.Request) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scene, err := s.store.LoadScene(s.key, index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"whiteboard": scene})
}

type putWhiteboardRequest struct {
	SourceHash         string          `json:"source_hash"`
	TextMetricsVersion int             `json:"text_metrics_version"`
	Scene              json.RawMessage `json:"scene"`
	Baseline           json.RawMessage `json:"baseline"`
}

func (s *Server) handlePutWhiteboard(w http.ResponseWriter, r *http.Request) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req putWhiteboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.store.SaveScene(s.key, index, req.SourceHash, req.TextMetricsVersion, req.Scene, req.Baseline); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type feedbackFilesRequest struct {
	Scene      json.RawMessage `json:"scene"`
	PngDataURL string          `json:"pngDataUrl"`
}

const pngDataURLPrefix = "data:image/png;base64,"

func decodePNGDataURL(dataURL string) ([]byte, error) {
	if dataURL == "" {
		return nil, nil
	}
	if !strings.HasPrefix(dataURL, pngDataURLPrefix) {
		return nil, fmt.Errorf("unsupported preview data URL")
	}
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, pngDataURLPrefix))
}

func (s *Server) handleFeedbackFiles(w http.ResponseWriter, r *http.Request) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req feedbackFilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	png, err := decodePNGDataURL(req.PngDataURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	paths, err := s.store.WriteFeedbackFiles(s.key, index, req.Scene, png)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"scene_path":   paths.ScenePath,
		"preview_path": paths.PreviewPath,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
