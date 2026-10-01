package forum

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) whiteboardRoutes() {
	s.mux.Handle("GET /whiteboard-assets/", whiteboardAssetsHandler())
	s.mux.HandleFunc("GET /whiteboard-embed.js", s.handleEmbedScript)
	s.mux.HandleFunc("GET /whiteboard-frame", s.handleWhiteboardFrame)
	s.mux.HandleFunc("GET /api/s/{key}/mermaid-sources", s.browserAPI(s.handleMermaidSources))
	s.mux.HandleFunc("GET /api/s/{key}/whiteboard/{index}", s.browserAPI(s.handleGetWhiteboard))
	s.mux.HandleFunc("PUT /api/s/{key}/whiteboard/{index}", s.browserAPI(s.handlePutWhiteboard))
	s.mux.HandleFunc("POST /api/s/{key}/whiteboard/{index}/feedback-files", s.browserAPI(s.handleFeedbackFiles))
}

// maxSceneBytes bounds a whiteboard scene or feedback body: Excalidraw
// scenes embed images, so they legitimately dwarf every other request.
const maxSceneBytes = 32 << 20

func (s *Server) handleEmbedScript(w http.ResponseWriter, r *http.Request) {
	script, err := whiteboardEmbedScript()
	if err != nil {
		http.Error(w, "internal error reading whiteboard-embed.js", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(script)
}

// The asset URLs carry the build id, so a browser that kept an earlier
// bundle under the old URL can never run it against this embed script.
const whiteboardFrameHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Whiteboard</title>
<link rel="stylesheet" href="/whiteboard-assets/whiteboard.css?v=%[1]s">
</head>
<body>
<script src="/whiteboard-assets/whiteboard.js?v=%[1]s"></script>
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
	_, _ = fmt.Fprintf(w, whiteboardFrameHTML, buildID())
}

func (s *Server) artifactSources(key string) ([]MermaidSource, error) {
	file, err := s.hub.File(key)
	if err != nil {
		return nil, err
	}
	html, err := readArtifactFile(file)
	if err != nil {
		return nil, err
	}
	return ExtractMermaidSources(html), nil
}

func (s *Server) handleMermaidSources(w http.ResponseWriter, r *http.Request, key string) {
	sources, err := s.artifactSources(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func parseDiagramIndex(r *http.Request) (int, error) {
	raw := r.PathValue("index")
	index, err := strconv.Atoi(raw)
	if err != nil || !ValidDiagramIndex(index) {
		return 0, fmt.Errorf("invalid diagram index %q", raw)
	}
	return index, nil
}

func (s *Server) handleGetWhiteboard(w http.ResponseWriter, r *http.Request, key string) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	scene, err := s.store.LoadScene(key, index)
	if err != nil {
		writeHubError(w, err)
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

func (s *Server) handlePutWhiteboard(w http.ResponseWriter, r *http.Request, key string) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var req putWhiteboardRequest
	if !decodeBodyLimit(w, r, &req, maxSceneBytes) {
		return
	}
	if err := s.store.SaveScene(key, index, req.SourceHash, req.TextMetricsVersion, req.Scene, req.Baseline); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

type feedbackFilesRequest struct {
	Scene        json.RawMessage `json:"scene"`
	PngDataURL   string          `json:"pngDataUrl"`
	SummaryLines []string        `json:"summaryLines"`
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

// handleFeedbackFiles is "Queue feedback" on a whiteboard: it keeps writing
// the .excalidraw scene and PNG preview to disk, and now also queues a
// prompt tagged "whiteboard" carrying a bounded summary of the edits and
// those two paths, so the whiteboard's feedback travels the same queue (and
// the same Send to Agent) as everything else. A newer queue of the same
// diagram replaces the unsent earlier one.
func (s *Server) handleFeedbackFiles(w http.ResponseWriter, r *http.Request, key string) {
	index, err := parseDiagramIndex(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var req feedbackFilesRequest
	if !decodeBodyLimit(w, r, &req, maxSceneBytes) {
		return
	}
	png, err := decodePNGDataURL(req.PngDataURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// Refuse before writing anything if no agent will ever read this.
	if err := s.hub.RequireOpen(key); err != nil {
		writeHubError(w, err)
		return
	}
	sources, err := s.artifactSources(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	paths, err := s.store.WriteFeedbackFiles(key, index, req.Scene, png)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	target, _ := json.Marshal(map[string]any{
		"whiteboard":  index + 1,
		"scenePath":   paths.ScenePath,
		"previewPath": paths.PreviewPath,
	})
	prompt, err := s.hub.QueuePrompt(key, PromptInput{
		Prompt:   whiteboardPrompt(index, len(sources), req.SummaryLines, paths),
		Tag:      "whiteboard",
		Text:     fmt.Sprintf("Whiteboard: diagram %d", index+1),
		Selector: fmt.Sprintf(".mermaid:nth-of-type(%d)", index+1),
		Target:   target,
		QueueKey: fmt.Sprintf("whiteboard:%d", index),
	})
	if err != nil {
		var bad *errBadPrompt
		if errors.As(err, &bad) {
			writeError(w, http.StatusBadRequest, "bad_prompt", err.Error())
			return
		}
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scene_path":   paths.ScenePath,
		"preview_path": paths.PreviewPath,
		"prompt_uid":   prompt.UID,
	})
}

const (
	maxSummaryLines     = 50
	maxSummaryLineChars = 300
)

// whiteboardPrompt is the text the agent receives for a whiteboard: which
// diagram, a bounded summary of what the reviewer changed, and where the
// full scene and preview are. The agent reads the summary first and opens
// the files only when it needs to.
func whiteboardPrompt(index, total int, summary []string, paths FeedbackFiles) string {
	var b strings.Builder
	if total > 0 {
		fmt.Fprintf(&b, "Whiteboard feedback for diagram %d of %d.\n", index+1, total)
	} else {
		fmt.Fprintf(&b, "Whiteboard feedback for diagram %d.\n", index+1)
	}
	lines := make([]string, 0, len(summary))
	for _, line := range summary {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, "- "+clip(line, maxSummaryLineChars))
		}
		if len(lines) == maxSummaryLines {
			break
		}
	}
	if len(lines) > 0 {
		b.WriteString("Edit summary:\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	} else {
		b.WriteString("No edit summary was provided.\n")
	}
	fmt.Fprintf(&b, "Scene (.excalidraw JSON): %s\n", paths.ScenePath)
	if paths.PreviewPath != "" {
		fmt.Fprintf(&b, "Preview (PNG): %s\n", paths.PreviewPath)
	}
	b.WriteString("Read the summary first and open the files only if you need more detail, then apply the edits by updating the Mermaid source in the artifact (never try to write the scene back).")
	return b.String()
}
