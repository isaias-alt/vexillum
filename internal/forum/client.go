package forum

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// Client talks to the running forum server on behalf of the agent-facing
// commands. It carries the agent token read from the discovery file.
type Client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(st ServerState) *Client { return NewClient(st.Addr, st.AgentToken) }

// NewClient returns a Client for the server at addr (host:port) using the agent
// token. Production code gets one from EnsureServer or Discover; this exists
// for callers that already know both (tests running a server in-process).
func NewClient(addr, agentToken string) *Client {
	return &Client{
		base:  "http://" + addr,
		token: agentToken,
		// No overall timeout: poll legitimately blocks for as long as the
		// reviewer takes. Callers bound requests with their context.
		http: &http.Client{},
	}
}

// APIError is a non-2xx answer from the server.
type APIError struct {
	Status  int
	Code    string
	Message string
	EndedBy string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("forum server answered %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("reading forum server response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var ae apiError
		_ = json.Unmarshal(data, &ae)
		return &APIError{Status: resp.StatusCode, Code: ae.Code, Message: ae.Error, EndedBy: ae.EndedBy}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("parsing forum server response: %w", err)
		}
	}
	return nil
}

func (c *Client) health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.App != "vexillum-forum" {
		return errors.New("not a " + cmdname.Name + " forum server")
	}
	return nil
}

// Open creates or resumes the session for file (absolute path).
func (c *Client) Open(ctx context.Context, file string, reopen bool) (OpenResponse, error) {
	var out OpenResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/open", agentFileRequest{File: file, Reopen: reopen}, &out)
	return out, err
}

// OpenFor is Open that also records projectRoot (absolute; empty for none) as
// the project whose commander owns the session.
func (c *Client) OpenFor(ctx context.Context, file string, reopen bool, projectRoot string) (OpenResponse, error) {
	var out OpenResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/open", agentFileRequest{File: file, Reopen: reopen, ProjectRoot: projectRoot}, &out)
	return out, err
}

// Poll blocks for the next feedback, session end, browser disconnect, or
// timeout (0 = none).
func (c *Client) Poll(ctx context.Context, file string, timeout time.Duration) (PollResponse, error) {
	var out PollResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/poll", agentFileRequest{File: file, TimeoutMS: timeout.Milliseconds()}, &out)
	return out, err
}

// PollAll is Poll over every open session at once; the result names the session
// it is about.
func (c *Client) PollAll(ctx context.Context, timeout time.Duration) (PollResponse, error) {
	var out PollResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/poll", agentFileRequest{All: true, TimeoutMS: timeout.Milliseconds()}, &out)
	return out, err
}

// PollRelay is PollAll as the listener: it covers only sessions that have a
// project root, and it does not count as the agent listening or end the wait for
// the commander's answer.
func (c *Client) PollRelay(ctx context.Context, timeout time.Duration) (PollResponse, error) {
	var out PollResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/poll", agentFileRequest{All: true, Relay: true, TimeoutMS: timeout.Milliseconds()}, &out)
	return out, err
}

// Relay tells the server the latest round of file was forwarded to the
// commander (commander is CommanderConnected or CommanderNone) and posts notice
// once per round without answering it.
func (c *Client) Relay(ctx context.Context, file, commander, notice string) (RelayResponse, error) {
	var out RelayResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/relay", agentFileRequest{File: file, Commander: commander, Text: notice}, &out)
	return out, err
}

// Ack confirms that the prompts of a poll's delivery were read, so the server
// stops holding them for redelivery.
func (c *Client) Ack(ctx context.Context, file, delivery string) error {
	return c.do(ctx, http.MethodPost, "/api/agent/ack", agentFileRequest{File: file, Delivery: delivery}, nil)
}

// Reply shows text (markdown) in the browser's conversation panel.
func (c *Client) Reply(ctx context.Context, file, text string) error {
	return c.do(ctx, http.MethodPost, "/api/agent/reply", agentFileRequest{File: file, Text: text}, nil)
}

// End ends the session as the agent.
func (c *Client) End(ctx context.Context, file string) error {
	return c.do(ctx, http.MethodPost, "/api/agent/end", agentFileRequest{File: file}, nil)
}

// Stop asks the server process to shut down.
func (c *Client) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/agent/stop", struct{}{}, nil)
}

// StopIfIdle asks the server to shut down only if nothing is open or
// connected. A busy server answers 409 with code "busy" (an *APIError).
func (c *Client) StopIfIdle(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/agent/stop", stopRequest{OnlyIfIdle: true}, nil)
}

// Status reports the server's build, protocol and current activity. A server
// older than the status route answers 404 (an *APIError).
func (c *Client) Status(ctx context.Context) (ServerStatus, error) {
	var out ServerStatus
	err := c.do(ctx, http.MethodGet, "/api/agent/status", nil, &out)
	return out, err
}
