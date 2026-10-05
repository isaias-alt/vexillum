package forum

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (s *Server) browserRoutes() {
	s.mux.HandleFunc("GET /api/s/{key}/state", s.browserAPI(s.handleState))
	s.mux.HandleFunc("GET /api/s/{key}/events", s.browserAPI(s.handleEvents))
	s.mux.HandleFunc("POST /api/s/{key}/queue", s.browserAPI(s.handleQueue))
	s.mux.HandleFunc("DELETE /api/s/{key}/queue/{uid}", s.browserAPI(s.handleUnqueue))
	s.mux.HandleFunc("POST /api/s/{key}/send", s.browserAPI(s.handleSend))
	s.mux.HandleFunc("POST /api/s/{key}/end", s.browserAPI(s.handleBrowserEnd))
	s.mux.HandleFunc("POST /api/s/{key}/stop-waiting", s.browserAPI(s.handleStopWaiting))
	s.mux.HandleFunc("POST /api/s/{key}/attachments", s.browserAPI(s.handleAttachmentUpload))
	s.mux.HandleFunc("POST /api/s/{key}/layout/diagnostics", s.browserAPI(s.handleLayoutDiagnostics))
	s.mux.HandleFunc("POST /api/s/{key}/layout/queue", s.browserAPI(s.handleLayoutQueue))
	s.mux.HandleFunc("POST /api/s/{key}/layout/dismiss", s.browserAPI(s.handleLayoutDismiss))
	s.mux.HandleFunc("GET /api/s/{key}/attachments/{id}", s.browserAPI(s.handleAttachmentGet))
	s.mux.HandleFunc("DELETE /api/s/{key}/attachments/{id}", s.browserAPI(s.handleAttachmentDelete))
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

// handleStopWaiting is the overlay's "Stop waiting": the user gives up on the
// agent answering, and every tab gets the review surface back.
func (s *Server) handleStopWaiting(w http.ResponseWriter, r *http.Request, key string) {
	stopped, err := s.hub.StopWaiting(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	status := "idle"
	if stopped {
		status = "stopped"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// handleAttachmentUpload stores one image sent as the raw request body. The
// type is decided from the bytes (what the client calls the file means
// nothing), the size is capped while reading, and the stored name is
// generated here.
func (s *Server) handleAttachmentUpload(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.hub.RequireOpen(key); err != nil {
		writeHubError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "image is larger than "+strconv.Itoa(maxAttachmentBytes>>20)+" MB")
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the image")
		return
	}
	att, err := s.hub.AddAttachment(key, data)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachment": att})
}

// handleAttachmentGet serves a stored image back to the chrome (which fetches
// it with its token and shows it as a thumbnail). The type comes from the
// stored extension, never from anything the client said, and the response
// can run no script even if opened directly.
func (s *Server) handleAttachmentGet(w http.ResponseWriter, r *http.Request, key string) {
	path, mime, err := s.hub.AttachmentFile(key, r.PathValue("id"))
	if err != nil {
		writeHubError(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_attachment", ErrNoAttachment.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "no_attachment", ErrNoAttachment.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (s *Server) handleAttachmentDelete(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.hub.RemoveAttachment(key, r.PathValue("id")); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleLayoutDiagnostics records one passive browser audit. It answers
// "recorded" and nothing more: a diagnostic pass never produces a prompt, so
// the agent can neither be woken by it nor see it in a poll.
func (s *Server) handleLayoutDiagnostics(w http.ResponseWriter, r *http.Request, key string) {
	var report AuditReport
	if !decodeBody(w, r, &report) {
		return
	}
	if err := s.hub.RecordLayoutAudit(key, report); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// handleLayoutQueue queues the user's selected layout issues as one ordinary
// prompt (tag layout-warnings). Sending it is a separate, explicit step.
func (s *Server) handleLayoutQueue(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	p, err := s.hub.QueueLayoutIssues(key, req.IDs)
	if err != nil {
		writeHubError(w, err)
		return
	}
	p.QueueKey = ""
	writeJSON(w, http.StatusOK, map[string]any{"prompt": p})
}

func (s *Server) handleLayoutDismiss(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	changed, err := s.hub.DismissLayoutIssue(key, req.ID)
	if err != nil {
		writeHubError(w, err)
		return
	}
	status := "unchanged"
	if changed {
		status = "dismissed"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

const defaultEventsHeartbeat = 15 * time.Second

// handleEvents is the browser's live feed (Server-Sent Events): the full
// snapshot right away, then again whenever anything about the session
// changes - the queue, the transcript, whether the agent is listening, the
// session's end, the artifact file - so every open tab of the same session
// shows the same thing without reloading. The stream is authenticated like
// every other browser route (the chrome reads it with fetch, which can send
// the token header; EventSource cannot, and a token in the URL would leak
// into logs). It lasts until the client goes away, which is also what frees
// its goroutine: the request context ends with the connection.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, key string) {
	file, err := s.hub.File(key)
	if err != nil {
		writeHubError(w, err)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	var (
		lastVersion int64 = -1
		lastAV      string
		lastWrite   = time.Now()
	)
	_ = s.hub.Watch(r.Context(), key, time.Second, func(snap Snapshot) error {
		av := artifactVersion(file)
		now := time.Now()
		switch {
		case snap.Version != lastVersion || av != lastAV:
			data, err := json.Marshal(stateResponse{Snapshot: snap, ArtifactVersion: av})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: state\ndata: %s\n\n", snap.Version, data); err != nil {
				return err
			}
			lastVersion, lastAV, lastWrite = snap.Version, av, now
		case now.Sub(lastWrite) >= s.heartbeat:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return err
			}
			lastWrite = now
		default:
			return nil
		}
		return rc.Flush()
	})
}
