package forum

import (
	"net/http"
	"strconv"
	"time"
)

func (s *Server) browserRoutes() {
	s.mux.HandleFunc("GET /api/s/{key}/state", s.browserAPI(s.handleState))
	s.mux.HandleFunc("POST /api/s/{key}/queue", s.browserAPI(s.handleQueue))
	s.mux.HandleFunc("DELETE /api/s/{key}/queue/{uid}", s.browserAPI(s.handleUnqueue))
	s.mux.HandleFunc("POST /api/s/{key}/send", s.browserAPI(s.handleSend))
	s.mux.HandleFunc("POST /api/s/{key}/end", s.browserAPI(s.handleBrowserEnd))
}

const (
	defaultStateWait = 25 * time.Second
	maxStateWait     = 30 * time.Second
	stateSlice       = 2 * time.Second
)

// handleState returns the session snapshot. With ?since=<version> it blocks
// (up to ?wait_ms, default 25s) until the version changes - the browser's
// live feed, no polling on a timer.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request, key string) {
	var since int64
	wait := time.Duration(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid since")
			return
		}
		since = v
		wait = defaultStateWait
	}
	if raw := r.URL.Query().Get("wait_ms"); raw != "" {
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || ms < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid wait_ms")
			return
		}
		wait = time.Duration(ms) * time.Millisecond
	}
	if wait > maxStateWait {
		wait = maxStateWait
	}
	// The artifact file is not part of the hub's state, so the wait runs in
	// short slices and re-checks the file between them: when the agent
	// edits the artifact, the page that passed its current ?av= learns of it
	// within a slice and reloads the iframe.
	file, err := s.hub.File(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	av := r.URL.Query().Get("av")
	deadline := time.Now().Add(wait)
	for {
		slice := time.Until(deadline)
		if slice > stateSlice {
			slice = stateSlice
		}
		if slice < 0 {
			slice = 0
		}
		snap, err := s.hub.State(r.Context(), key, since, slice)
		if err != nil {
			writeHubError(w, err)
			return
		}
		current := artifactVersion(file)
		if snap.Version != since || wait <= 0 || (av != "" && current != av) || !time.Now().Before(deadline) || r.Context().Err() != nil {
			writeJSON(w, http.StatusOK, stateResponse{Snapshot: snap, ArtifactVersion: current})
			return
		}
	}
}

// stateResponse is the browser's snapshot plus the artifact fingerprint.
type stateResponse struct {
	Snapshot
	ArtifactVersion string `json:"artifact_version"`
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request, key string) {
	var in PromptInput
	if !decodeBody(w, r, &in) {
		return
	}
	p, err := s.hub.QueuePrompt(key, in)
	if err != nil {
		if isBadPrompt(err) {
			writeError(w, http.StatusBadRequest, "bad_prompt", err.Error())
			return
		}
		writeHubError(w, err)
		return
	}
	p.QueueKey = ""
	writeJSON(w, http.StatusOK, map[string]any{"prompt": p})
}

func (s *Server) handleUnqueue(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.hub.RemoveQueued(key, r.PathValue("uid")); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		End bool `json:"end"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	n, err := s.hub.Send(key, req.End)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "sent": n, "ended": req.End})
}

func (s *Server) handleBrowserEnd(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.hub.End(key, EndedByUser); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}
