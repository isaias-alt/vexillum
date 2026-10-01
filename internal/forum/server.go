package forum

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// ServerOptions configure a Server.
type ServerOptions struct {
	Hub   *Hub
	Store *Store
	// AgentToken authenticates the agent-facing commands (open, poll, reply,
	// end, stop). It lives only in the 0600 discovery file, which a web page
	// cannot read.
	AgentToken string
	// Addr is the host:port the server listens on, used to build session
	// URLs.
	Addr string
	// Shutdown asks the process to stop (the `vexillum forum stop` route).
	Shutdown func()
}

// Server is the single local forum server: it multiplexes every session of
// the user, keyed by the SHA-256 of the artifact's absolute path. Two
// audiences use it with two different credentials:
//
//   - the agent, through `vexillum forum` subcommands -> /api/agent/*,
//     authenticated by the agent token;
//   - the reviewer's browser -> /session/{key}, /a/{key}/..., /api/s/{key}/...,
//     authenticated by a per-session token plus a same-origin check (see
//     security.go).
//
// Every route also rejects a Host that is not loopback, which is what stops
// a DNS-rebinding page from talking to it.
type Server struct {
	hub        *Hub
	store      *Store
	agentToken string
	addr       string
	shutdown   func()
	mux        *http.ServeMux
}

// NewServer builds the server's handler.
func NewServer(opts ServerOptions) *Server {
	s := &Server{
		hub:        opts.Hub,
		store:      opts.Store,
		agentToken: opts.AgentToken,
		addr:       opts.Addr,
		shutdown:   opts.Shutdown,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !loopbackHost(r.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /api/agent/open", s.agentOnly(s.handleAgentOpen))
	s.mux.HandleFunc("POST /api/agent/poll", s.agentOnly(s.handleAgentPoll))
	s.mux.HandleFunc("POST /api/agent/reply", s.agentOnly(s.handleAgentReply))
	s.mux.HandleFunc("POST /api/agent/end", s.agentOnly(s.handleAgentEnd))
	s.mux.HandleFunc("POST /api/agent/stop", s.agentOnly(s.handleAgentStop))
	s.browserRoutes()
}

// SessionURL is the page the reviewer opens for key.
func (s *Server) SessionURL(key string) string {
	return "http://" + s.addr + "/session/" + key
}

// apiError is the JSON error body every API route returns; Code is stable
// and machine-readable, Error is for humans.
type apiError struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	EndedBy string `json:"ended_by,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

// writeHubError maps a Hub error to its HTTP response.
func writeHubError(w http.ResponseWriter, err error) {
	var ended *ErrSessionEnded
	switch {
	case errors.As(err, &ended):
		writeJSON(w, http.StatusConflict, apiError{Error: ended.Error(), Code: "ended", EndedBy: ended.EndedBy})
	case errors.Is(err, ErrNoSession):
		writeError(w, http.StatusNotFound, "no_session", err.Error())
	case errors.Is(err, ErrNoPrompt):
		writeError(w, http.StatusNotFound, "no_prompt", err.Error())
	case errors.Is(err, ErrNothingToSend):
		writeError(w, http.StatusBadRequest, "nothing_to_send", err.Error())
	case errors.Is(err, ErrQueueFull):
		writeError(w, http.StatusConflict, "queue_full", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

const maxBodyBytes = 1 << 20

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"app": "vexillum-forum", "ok": true})
}

// agentOnly gates a handler behind the agent token. A request carrying an
// Origin header came from a browser page, never from the CLI, so it is
// refused even if (impossibly) it knew the token.
func (s *Server) agentOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			writeError(w, http.StatusForbidden, "forbidden", "agent API is not available to web pages")
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.agentToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.agentToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid agent token")
			return
		}
		s.hub.Touch()
		next(w, r)
	}
}

type agentFileRequest struct {
	File      string `json:"file"`
	Reopen    bool   `json:"reopen"`
	Text      string `json:"text"`
	TimeoutMS int64  `json:"timeout_ms"`
}

// agentKey validates the absolute artifact path an agent command names and
// returns its session key.
func agentKey(w http.ResponseWriter, req agentFileRequest) (string, bool) {
	if req.File == "" || !filepath.IsAbs(req.File) {
		writeError(w, http.StatusBadRequest, "bad_request", "file must be an absolute path")
		return "", false
	}
	return SessionKey(filepath.Clean(req.File)), true
}

// OpenResponse is the body of POST /api/agent/open.
type OpenResponse struct {
	Key              string `json:"key"`
	File             string `json:"file"`
	URL              string `json:"url"`
	Status           string `json:"status"`
	Created          bool   `json:"created"`
	BrowserConnected bool   `json:"browser_connected"`
	Pending          int    `json:"pending"`
}

func (s *Server) handleAgentOpen(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, ok := agentKey(w, req); !ok {
		return
	}
	res, err := s.hub.Open(req.File, req.Reopen)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OpenResponse{
		Key:              res.Key,
		File:             res.File,
		URL:              s.SessionURL(res.Key),
		Status:           res.Status,
		Created:          res.Created,
		BrowserConnected: res.BrowserConnected,
		Pending:          res.Pending,
	})
}

// PollResponse is the body of POST /api/agent/poll.
type PollResponse struct {
	Session string   `json:"session"`
	File    string   `json:"file"`
	Status  string   `json:"status"`
	EndedBy string   `json:"ended_by,omitempty"`
	Prompts []Prompt `json:"prompts"`
}

func (s *Server) handleAgentPoll(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key, ok := agentKey(w, req)
	if !ok {
		return
	}
	res, err := s.hub.Poll(r.Context(), key, time.Duration(req.TimeoutMS)*time.Millisecond)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeHubError(w, err)
		return
	}
	prompts := res.Prompts
	if prompts == nil {
		prompts = []Prompt{}
	}
	// QueueKey is browser-only bookkeeping; the agent never sees it.
	delivered := make([]Prompt, len(prompts))
	for i, p := range prompts {
		p.QueueKey = ""
		delivered[i] = p
	}
	writeJSON(w, http.StatusOK, PollResponse{Session: res.Key, File: res.File, Status: res.Status, EndedBy: res.EndedBy, Prompts: delivered})
	// A client that vanished before it could read what was just consumed
	// gets it back in the outbox, so the next poll delivers it.
	if len(prompts) > 0 {
		if flushErr := http.NewResponseController(w).Flush(); flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) || r.Context().Err() != nil {
			if rerr := s.hub.Restore(key, prompts); rerr != nil {
				s.hub.logf("forum: restoring undelivered prompts for %s: %v", key, rerr)
			}
		}
	}
}

func (s *Server) handleAgentReply(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key, ok := agentKey(w, req)
	if !ok {
		return
	}
	if err := s.hub.Reply(key, req.Text); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleAgentEnd(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key, ok := agentKey(w, req)
	if !ok {
		return
	}
	if err := s.hub.End(key, EndedByAgent); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
	if s.shutdown != nil {
		go s.shutdown()
	}
}

// loopbackHost reports whether a Host header names this machine's loopback
// (any port): 127.0.0.1, localhost or [::1]. Anything else - notably a
// DNS-rebinding name that resolves to 127.0.0.1 - is not ours to serve.
func loopbackHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}
