package forum

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
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
	// Build and Protocol are the identity the status route reports; a zero
	// Protocol means ProtocolVersion.
	Build    string
	Protocol int
	// Shutdown asks the process to stop (the `vx forum stop` route).
	Shutdown func()
	// EventsHeartbeat is how often an idle event stream sends a comment so a
	// dead connection is noticed; zero means 15 seconds. Tests shrink it.
	EventsHeartbeat time.Duration
}

// Server is the single local forum server: it multiplexes every session of
// the user, keyed by the SHA-256 of the artifact's absolute path. Two
// audiences use it with two different credentials:
//
//   - the agent, through `vx forum` subcommands -> /api/agent/*,
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
	build      string
	protocol   int
	shutdown   func()
	heartbeat  time.Duration
	mux        *http.ServeMux
}

// NewServer builds the server's handler.
func NewServer(opts ServerOptions) *Server {
	s := &Server{
		hub:        opts.Hub,
		store:      opts.Store,
		agentToken: opts.AgentToken,
		addr:       opts.Addr,
		build:      opts.Build,
		protocol:   opts.Protocol,
		shutdown:   opts.Shutdown,
		heartbeat:  opts.EventsHeartbeat,
		mux:        http.NewServeMux(),
	}
	if s.protocol == 0 {
		s.protocol = ProtocolVersion
	}
	if s.heartbeat <= 0 {
		s.heartbeat = defaultEventsHeartbeat
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
	s.mux.HandleFunc("POST /api/agent/ack", s.agentOnly(s.handleAgentAck))
	s.mux.HandleFunc("POST /api/agent/reply", s.agentOnly(s.handleAgentReply))
	s.mux.HandleFunc("POST /api/agent/relay", s.agentOnly(s.handleAgentRelay))
	s.mux.HandleFunc("POST /api/agent/end", s.agentOnly(s.handleAgentEnd))
	s.mux.HandleFunc("POST /api/agent/stop", s.agentOnly(s.handleAgentStop))
	s.mux.HandleFunc("GET /api/agent/status", s.agentOnly(s.handleAgentStatus))
	s.browserRoutes()
	s.pageRoutes()
	s.whiteboardRoutes()
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
	case errors.Is(err, ErrNoQueueableIssues):
		writeError(w, http.StatusConflict, "nothing_to_queue", err.Error())
	case errors.Is(err, ErrIssueTooLargeToQueue):
		writeError(w, http.StatusConflict, "issue_too_large", err.Error())
	case errors.Is(err, ErrQueueFull):
		writeError(w, http.StatusConflict, "queue_full", err.Error())
	case errors.Is(err, ErrUnsupportedImage):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_type", err.Error())
	case errors.Is(err, ErrNoAttachment):
		writeError(w, http.StatusNotFound, "no_attachment", err.Error())
	case errors.Is(err, ErrAttachmentInUse):
		writeError(w, http.StatusConflict, "attachment_in_use", err.Error())
	case errors.Is(err, ErrTooManyAttachments):
		writeError(w, http.StatusBadRequest, "too_many_attachments", err.Error())
	case errors.Is(err, ErrTooManyStaged), errors.Is(err, ErrAttachmentsFull):
		writeError(w, http.StatusConflict, "attachments_full", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

const maxBodyBytes = 1 << 20

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeBodyLimit(w, r, v, maxBodyBytes)
}

func decodeBodyLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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
	// All polls every open session instead of File's (poll only).
	All bool `json:"all"`
	// Delivery is the lease a poll handed out (ack only).
	Delivery string `json:"delivery"`
	// ProjectRoot is the project the opener belongs to (open only).
	ProjectRoot string `json:"project_root"`
	// Relay makes a multiplexed poll the listener's (poll with All only).
	Relay bool `json:"relay"`
	// Commander is CommanderConnected or CommanderNone (relay only); Text is
	// the notice to post.
	Commander string `json:"commander"`
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
	// ProjectRoot echoes the project root the session is recorded under; empty
	// when the opener sent none.
	ProjectRoot string `json:"project_root,omitempty"`
}

func (s *Server) handleAgentOpen(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, ok := agentKey(w, req); !ok {
		return
	}
	if req.ProjectRoot != "" && !filepath.IsAbs(req.ProjectRoot) {
		writeError(w, http.StatusBadRequest, "bad_request", "project_root must be an absolute path")
		return
	}
	res, err := s.hub.OpenFor(req.File, req.Reopen, cleanRoot(req.ProjectRoot))
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
		ProjectRoot:      res.ProjectRoot,
	})
}

// PollResponse is the body of POST /api/agent/poll.
type PollResponse struct {
	Session string `json:"session"`
	File    string `json:"file"`
	Status  string `json:"status"`
	EndedBy string `json:"ended_by,omitempty"`
	// ProjectRoot is the project root recorded for the session, so the listener
	// knows whose inbox the prompts belong to.
	ProjectRoot string   `json:"project_root,omitempty"`
	Prompts     []Prompt `json:"prompts"`
	// All is set when the poll covered every open session; File then says which
	// one the result is about (empty for a status that is about none of them).
	All bool `json:"all,omitempty"`
	// OtherPending counts the other sessions a multiplexed poll left with
	// feedback still waiting.
	OtherPending int `json:"other_pending,omitempty"`
	// Delivery is the lease to confirm with POST /api/agent/ack once the prompts
	// were read; until then they are delivered again by the next poll.
	Delivery string `json:"delivery,omitempty"`
}

func (s *Server) handleAgentPoll(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	var res PollResult
	var err error
	if req.All {
		res, err = s.hub.PollAllWith(r.Context(), timeout, PollAllOptions{Relay: req.Relay})
	} else {
		key, ok := agentKey(w, req)
		if !ok {
			return
		}
		res, err = s.hub.Poll(r.Context(), key, timeout)
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeHubError(w, err)
		return
	}
	// QueueKey is browser-only bookkeeping; the agent never sees it.
	delivered := make([]Prompt, len(res.Prompts))
	for i, p := range res.Prompts {
		p.QueueKey = ""
		p.LayoutIDs = nil
		// The agent reads attachments from disk: hand it their local paths.
		p.Attachments = append([]Attachment(nil), p.Attachments...)
		for j := range p.Attachments {
			p.Attachments[j].Path = s.hub.AttachmentPath(res.Key, p.Attachments[j].ID)
		}
		delivered[i] = p
	}
	// Nothing to undo if the client vanished before reading this: the prompts
	// are leased, not consumed, and the next poll delivers them again.
	writeJSON(w, http.StatusOK, PollResponse{
		Session:      res.Key,
		File:         res.File,
		Status:       res.Status,
		EndedBy:      res.EndedBy,
		ProjectRoot:  res.ProjectRoot,
		Prompts:      delivered,
		All:          req.All,
		OtherPending: res.OtherPending,
		Delivery:     res.Delivery,
	})
}

func (s *Server) handleAgentAck(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key, ok := agentKey(w, req)
	if !ok {
		return
	}
	if err := s.hub.Ack(key, req.Delivery); err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
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

// RelayResponse is the body of POST /api/agent/relay.
type RelayResponse struct {
	Active bool `json:"active"`
	Posted bool `json:"posted"`
}

func (s *Server) handleAgentRelay(w http.ResponseWriter, r *http.Request) {
	var req agentFileRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key, ok := agentKey(w, req)
	if !ok {
		return
	}
	res, err := s.hub.Relay(key, req.Commander, req.Text)
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, RelayResponse{Active: res.Active, Posted: res.Posted})
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

// stopRequest is the body of POST /api/agent/stop. OnlyIfIdle makes the stop
// conditional on nothing being open or connected, so a client replacing a
// stale server cannot kill one that gained a tab since it looked.
type stopRequest struct {
	OnlyIfIdle bool `json:"only_if_idle"`
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	var req stopRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.OnlyIfIdle {
		if a := s.hub.Activity(); a.Busy() {
			writeError(w, http.StatusConflict, "busy", "the forum server is in use ("+a.String()+")")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
	if s.shutdown != nil {
		go s.shutdown()
	}
}

func (s *Server) handleAgentStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ServerStatus{PID: os.Getpid(), Build: s.build, Protocol: s.protocol, Activity: s.hub.Activity()})
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

// cleanRoot cleans a non-empty path and leaves an empty one empty (filepath.Clean
// would turn it into ".").
func cleanRoot(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}
