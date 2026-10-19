// Package opencodeapi talks to the OpenCode V2 background service through its
// own CLI front end, "opencode api". It exists so the sentinel can put a wake
// into an OpenCode commander's session from outside the TUI, and so doctor can
// tell whether that path works.
//
// The wire is `opencode api <METHOD> <path> -d <json>` (V2 only: OpenCode 1.x
// has no such command) against the service the TUI talks to. The service is
// found through the caller's HOME and XDG_* directories (its password lives in
// $XDG_CONFIG_HOME/opencode/service.json), so a process with another HOME or
// XDG layout than the commander's reaches another service, or none. Calls here
// never start a service: `opencode api` would, so every call first checks
// `opencode service status`.
//
// The routes used are GET /api/info and POST /api/session/<id>/prompt from the
// generated client of the OpenCode v2.0.32 sources. They were exercised live
// on 2.0.32 only; a later 2.x may move them.
package opencodeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client is what the sentinel and doctor need from a running OpenCode V2.
type Client interface {
	// ServiceRunning reports whether the background service answers in the
	// caller's environment, without starting one.
	ServiceRunning() (bool, error)
	// Info is GET /api/info.
	Info() (Info, error)
	// Prompt delivers text as a user turn of the session, queued behind the
	// turn that is running if there is one. It never touches the TUI's editor,
	// so a draft the person is typing stays where it is.
	Prompt(sessionID, text string) error
}

// Info is what GET /api/info says about the service.
type Info struct {
	Version string   `json:"version"`
	PID     int      `json:"pid"`
	URLs    []string `json:"urls"`
}

// ErrServiceStopped means no OpenCode background service runs in the caller's
// environment.
var ErrServiceStopped = errors.New("the OpenCode background service is not running in this environment")

// HTTPError is a non-success answer from the service.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("the OpenCode service answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("the OpenCode service answered HTTP %d: %s", e.Status, e.Body)
}

// IsSessionNotFound reports whether err says the service does not know the
// session: the commander's session id belongs to another service (another HOME
// or XDG layout), or the session was deleted.
func IsSessionNotFound(err error) bool {
	var h *HTTPError
	return errors.As(err, &h) && h.Status == 404
}

// CLI is the real Client, shelling out to the opencode binary.
type CLI struct {
	// Binary is the opencode executable; empty means "opencode" from PATH.
	Binary string
	// Timeout bounds each call; zero means 30 seconds.
	Timeout time.Duration
}

func (c CLI) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	return "opencode"
}

func (c CLI) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

// run executes opencode with args and returns what it wrote on each stream. An
// exit status other than 0 is the returned error, with stderr in its text.
func (c CLI) run(args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary(), args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	// A child that outlives the killed opencode must not keep the call waiting
	// on the pipes.
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return out.String(), errOut.String(), fmt.Errorf("opencode %s timed out after %s", args[0], c.timeout())
	}
	return out.String(), errOut.String(), err
}

func (c CLI) ServiceRunning() (bool, error) {
	out, errOut, err := c.run("service", "status")
	if err != nil {
		return false, fmt.Errorf("opencode service status: %w%s", err, tail(errOut))
	}
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	switch {
	case first == "stopped":
		return false, nil
	case strings.HasPrefix(first, "http://"), strings.HasPrefix(first, "https://"):
		return true, nil
	}
	return false, fmt.Errorf("unexpected answer from opencode service status: %q", first)
}

func (c CLI) Info() (Info, error) {
	out, err := c.request("GET", "/api/info", "")
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return Info{}, fmt.Errorf("parsing the answer of GET /api/info: %w", err)
	}
	return info, nil
}

func (c CLI) Prompt(sessionID, text string) error {
	if !validSessionID(sessionID) {
		return fmt.Errorf("not an OpenCode session id: %q", sessionID)
	}
	body, err := json.Marshal(map[string]any{
		"text":     text,
		"delivery": "queue",
	})
	if err != nil {
		return err
	}
	_, err = c.request("POST", "/api/session/"+url.PathEscape(sessionID)+"/prompt", string(body))
	return err
}

// request is one `opencode api` call, refused without starting a service.
func (c CLI) request(method, path, body string) (string, error) {
	running, err := c.ServiceRunning()
	if err != nil {
		return "", err
	}
	if !running {
		return "", ErrServiceStopped
	}
	args := []string{"api", method, path}
	if body != "" {
		args = append(args, "-d", body)
	}
	out, errOut, err := c.run(args...)
	if err == nil {
		return out, nil
	}
	if m := httpStatus.FindStringSubmatch(errOut); m != nil {
		status, _ := strconv.Atoi(m[1])
		return "", &HTTPError{Status: status, Body: strings.TrimSpace(out)}
	}
	return "", fmt.Errorf("opencode api %s %s: %w%s", method, path, err, tail(errOut))
}

// httpStatus matches the status line `opencode api` prints on stderr when the
// service answers with an error ("HTTP 404 Not Found").
var httpStatus = regexp.MustCompile(`(?m)^HTTP (\d{3})\b`)

// sessionIDPattern is what OpenCode names a session: "ses_" and an alphanumeric
// tail. Checking it keeps a hostile id from reshaping the request path.
var sessionIDPattern = regexp.MustCompile(`^ses_[A-Za-z0-9]+$`)

func validSessionID(id string) bool { return sessionIDPattern.MatchString(id) }

// tail is stderr as a suffix for an error message: ": <last line>" or nothing.
func tail(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	lines := strings.Split(stderr, "\n")
	return ": " + strings.TrimSpace(lines[len(lines)-1])
}
