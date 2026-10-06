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
	s.mux.HandleFunc("GET /api/s/{key}/diagrams", s.browserAPI(s.handleDiagrams))
	s.mux.HandleFunc("GET /api/s/{key}/boards/{ordinal}", s.browserAPI(s.handleGetBoard))
	s.mux.HandleFunc("PUT /api/s/{key}/boards/{ordinal}", s.browserAPI(s.handlePutBoard))
	s.mux.HandleFunc("POST /api/s/{key}/boards/{ordinal}/submit", s.browserAPI(s.handleSubmitBoard))
}

// maxSceneBytes bounds a stored record or a submit body: Excalidraw scenes
// embed images, so they legitimately dwarf every other request.
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
<html data-fr-theme="%[2]s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Whiteboard</title>
<link rel="stylesheet" href="/forum-assets/forum-tokens.css">
<link rel="stylesheet" href="/whiteboard-assets/whiteboard.css?v=%[1]s">
</head>
<body>
<script src="/whiteboard-assets/whiteboard.js?v=%[1]s"></script>
</body>
</html>
`

// handleWhiteboardFrame serves the frame page hosted inside the sandboxed
// iframe (allow-scripts allow-popups, no allow-same-origin) that runs the
// Excalidraw editor - both the inline placement and the fullscreen overlay
// point at this same route. The frame script reads two query parameters at
// boot: slot (the board's ordinal) and palette (dark or light). The page is
// blank until that script runs, but its <html> already carries the palette,
// so the tokens and the color-scheme resolve from the first paint: the iframe
// is transparent over the embed's themed backdrop and nothing of the wrong
// theme is ever visible. Anything but "light" means dark.
func (s *Server) handleWhiteboardFrame(w http.ResponseWriter, r *http.Request) {
	palette := "dark"
	if r.URL.Query().Get("palette") == "light" {
		palette = "light"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, whiteboardFrameHTML, buildID(), palette)
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

func (s *Server) handleDiagrams(w http.ResponseWriter, r *http.Request, key string) {
	diagrams, err := s.artifactSources(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagrams": diagrams})
}

func parseOrdinal(r *http.Request) (int, error) {
	raw := r.PathValue("ordinal")
	ordinal, err := strconv.Atoi(raw)
	if err != nil || !ValidDiagramIndex(ordinal) {
		return 0, fmt.Errorf("invalid board ordinal %q", raw)
	}
	return ordinal, nil
}

// handleGetBoard returns the stored record, or null when there is none or it
// is of another format (see Store.LoadScene).
func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request, key string) {
	ordinal, err := parseOrdinal(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	record, err := s.store.LoadScene(key, ordinal)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": record})
}

// putBoardRequest is the record the frame stores; saved_at is the server's.
type putBoardRequest struct {
	Format     int             `json:"format"`
	Digest     string          `json:"digest"`
	MeasureGen int             `json:"measure_gen"`
	Current    json.RawMessage `json:"current"`
	Pristine   json.RawMessage `json:"pristine"`
}

func (s *Server) handlePutBoard(w http.ResponseWriter, r *http.Request, key string) {
	ordinal, err := parseOrdinal(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var req putBoardRequest
	if !decodeBodyLimit(w, r, &req, maxSceneBytes) {
		return
	}
	if req.Format != sceneRecordFormat {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("unsupported record format %d", req.Format))
		return
	}
	if err := s.store.SaveScene(key, ordinal, req.Digest, req.MeasureGen, req.Current, req.Pristine); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

type submitBoardRequest struct {
	Current   json.RawMessage `json:"current"`
	PNG       string          `json:"png"`
	EditLines []string        `json:"edit_lines"`
	Remark    string          `json:"remark"`
}

const pngDataURLPrefix = "data:image/png;base64,"

func pngFromDataURL(dataURL string) ([]byte, error) {
	if dataURL == "" {
		return nil, nil
	}
	if !strings.HasPrefix(dataURL, pngDataURLPrefix) {
		return nil, fmt.Errorf("unsupported preview data URL")
	}
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, pngDataURLPrefix))
}

// handleSubmitBoard is "Queue feedback" on a whiteboard: it writes the
// .excalidraw scene and PNG preview to disk and queues a prompt tagged
// "whiteboard" carrying a bounded summary of the edits, the reviewer's
// optional remark and those two paths, so the whiteboard's feedback travels
// the same queue (and the same Send to Agent) as everything else. A newer
// queue of the same diagram replaces the unsent earlier one.
func (s *Server) handleSubmitBoard(w http.ResponseWriter, r *http.Request, key string) {
	index, err := parseOrdinal(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var req submitBoardRequest
	if !decodeBodyLimit(w, r, &req, maxSceneBytes) {
		return
	}
	png, err := pngFromDataURL(req.PNG)
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
	paths, err := s.store.WriteFeedbackFiles(key, index, req.Current, png)
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
		Prompt:   whiteboardPrompt(index, len(sources), req.EditLines, req.Remark, paths),
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
	maxRemarkChars      = 1000
)

// whiteboardPrompt is the text the agent receives for a whiteboard: which
// diagram, a bounded summary of what the reviewer changed, the reviewer's
// remark on its own labeled line (when they typed one), and where the full
// scene and preview are. The agent reads the summary first and opens
// the files only when it needs to.
func whiteboardPrompt(index, total int, summary []string, remark string, paths FeedbackFiles) string {
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
	if remark = strings.Join(strings.Fields(remark), " "); remark != "" {
		fmt.Fprintf(&b, "Reviewer remark: %s\n", clip(remark, maxRemarkChars))
	}
	fmt.Fprintf(&b, "Scene (.excalidraw JSON): %s\n", paths.ScenePath)
	if paths.PreviewPath != "" {
		fmt.Fprintf(&b, "Preview (PNG): %s\n", paths.PreviewPath)
	}
	b.WriteString("The summary above is usually enough; open the scene or preview files only when it leaves a question open. Make the requested change in the Mermaid source inside the artifact page itself, because the scene file is read-only output and nothing reads it back.")
	return b.String()
}
