// Package opencodeapi talks to the OpenCode V2 background service through its
// own CLI front end, "opencode api". It exists so the sentinel can put a wake
// into an OpenCode commander's session from outside the TUI, so doctor can
// tell whether that path works, and so dispatch can give an OpenCode soldier
// its own session on the model the task names (see Sessions).
//
// The wire is `opencode api <METHOD> <path> -d <json>` (V2 only: OpenCode 1.x
// has no such command) against the service the TUI talks to. The service is
// found through the caller's HOME and XDG_* directories (its password lives in
// $XDG_CONFIG_HOME/opencode/service.json), so a process with another HOME or
// XDG layout than the commander's reaches another service, or none. Calls here
// never start a service: `opencode api` would, so every call first checks
// `opencode service status`. The one exception is Sessions.EnsureService, which
// dispatch calls to start the service itself, from a clean environment, before
// it creates a soldier's session.
//
// Every call runs with OPENCODE_CONFIG, OPENCODE_CONFIG_DIR and
// OPENCODE_CONFIG_CONTENT removed from its environment: the service reads
// those once, when it starts, and a service started by one of these calls
// would carry the caller's config layer to every TUI that connects to it.
//
// The routes used are GET /api/info, POST /api/session/<id>/prompt, GET
// /api/model, POST /api/session and GET /api/session/<id> from the generated
// client of the OpenCode v2.0.32 sources. They were exercised live on 2.0.32
// only; a later 2.x may move them.
package opencodeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
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

// Sessions is what dispatch needs from OpenCode V2 to start a soldier on a
// session of its own: the service, the models it can run, and the session
// with the task's model. It is a separate interface from Client so the
// commander's wake path keeps its rule of never starting a service.
type Sessions interface {
	// EnsureService starts the background service, from a clean environment,
	// when none runs. It is the only call of this package that starts one.
	EnsureService() error
	// Models is GET /api/model for the project at directory: every model the
	// service can run there, with the variants each declares.
	Models(directory string) ([]Model, error)
	// CreateSession is POST /api/session: a session located at directory that
	// runs on model, with no agent (the project's default agent is kept) and no
	// permissions (the project's own rules apply). It returns the session id.
	CreateSession(directory, title string, model SessionModel) (string, error)
	// Session is GET /api/session/<id>. A session the service does not know is
	// an error for which IsSessionNotFound is true.
	Session(id string) (Session, error)
}

// Model is one entry of GET /api/model.
type Model struct {
	ProviderID string
	ID         string
	Variants   []string
}

// SessionModel is the model a session runs on. Variant is empty for the
// model's default.
type SessionModel struct {
	ProviderID string `json:"providerID"`
	ID         string `json:"id"`
	Variant    string `json:"variant,omitempty"`
}

// Session is what GET /api/session/<id> says about a session.
type Session struct {
	ID        string
	Directory string
	Model     SessionModel
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
	cmd.Env = cleanEnv(os.Environ())
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
	return c.call(method, path, body, nil)
}

// call is request with extra "name: value" request headers.
func (c CLI) call(method, path, body string, headers []string) (string, error) {
	running, err := c.ServiceRunning()
	if err != nil {
		return "", err
	}
	if !running {
		return "", ErrServiceStopped
	}
	args := []string{"api", method, path}
	for _, h := range headers {
		args = append(args, "-H", h)
	}
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

// strippedEnv are the variables through which OpenCode takes a config layer
// from its environment. The service reads them once, when it starts.
var strippedEnv = []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT"}

// cleanEnv is env without the strippedEnv variables.
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(strippedEnv, name) {
			out = append(out, kv)
		}
	}
	return out
}

func (c CLI) EnsureService() error {
	running, err := c.ServiceRunning()
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	if _, errOut, err := c.run("service", "start"); err != nil {
		// Another dispatch may have started it between the check and the start.
		if running, statusErr := c.ServiceRunning(); statusErr == nil && running {
			return nil
		}
		return fmt.Errorf("opencode service start: %w%s", err, tail(errOut))
	}
	running, err = c.ServiceRunning()
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("opencode service start returned, but the service is not running")
	}
	return nil
}

func (c CLI) Models(directory string) ([]Model, error) {
	out, err := c.call("GET", "/api/model", "", []string{"x-opencode-directory: " + directory})
	if err != nil {
		return nil, err
	}
	var answer struct {
		Data []struct {
			ID         string `json:"id"`
			ModelID    string `json:"modelID"`
			ProviderID string `json:"providerID"`
			Enabled    *bool  `json:"enabled"`
			Variants   []struct {
				ID string `json:"id"`
			} `json:"variants"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		return nil, fmt.Errorf("parsing the answer of GET /api/model: %w", err)
	}
	var models []Model
	for _, m := range answer.Data {
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		id := m.ID
		if id == "" {
			id = m.ModelID
		}
		if m.ProviderID == "" || id == "" {
			continue
		}
		model := Model{ProviderID: m.ProviderID, ID: id}
		for _, v := range m.Variants {
			model.Variants = append(model.Variants, v.ID)
		}
		models = append(models, model)
	}
	return models, nil
}

func (c CLI) CreateSession(directory, title string, model SessionModel) (string, error) {
	body, err := json.Marshal(map[string]any{
		"title":    title,
		"model":    model,
		"location": map[string]string{"directory": directory},
	})
	if err != nil {
		return "", err
	}
	out, err := c.request("POST", "/api/session", string(body))
	if err != nil {
		return "", err
	}
	var answer struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		return "", fmt.Errorf("parsing the answer of POST /api/session: %w", err)
	}
	if !validSessionID(answer.Data.ID) {
		return "", fmt.Errorf("POST /api/session answered with no usable session id: %q", strings.TrimSpace(out))
	}
	return answer.Data.ID, nil
}

func (c CLI) Session(id string) (Session, error) {
	if !validSessionID(id) {
		return Session{}, fmt.Errorf("not an OpenCode session id: %q", id)
	}
	out, err := c.request("GET", "/api/session/"+url.PathEscape(id), "")
	if err != nil {
		return Session{}, err
	}
	var answer struct {
		Data struct {
			ID       string       `json:"id"`
			Model    SessionModel `json:"model"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		return Session{}, fmt.Errorf("parsing the answer of GET /api/session/%s: %w", id, err)
	}
	return Session{ID: answer.Data.ID, Directory: answer.Data.Location.Directory, Model: answer.Data.Model}, nil
}

// maxListed bounds how many names an error lists.
const maxListed = 25

// CheckModel reports whether models can run providerID/modelID on variant
// (empty means the model's default), and when not, says what exists instead:
// the providers, the provider's models, or the model's variants.
func CheckModel(models []Model, providerID, modelID, variant string) error {
	var providers, ofProvider []string
	var found *Model
	for i, m := range models {
		if !slices.Contains(providers, m.ProviderID) {
			providers = append(providers, m.ProviderID)
		}
		if m.ProviderID != providerID {
			continue
		}
		ofProvider = append(ofProvider, m.ID)
		if m.ID == modelID {
			found = &models[i]
		}
	}
	switch {
	case len(models) == 0:
		return fmt.Errorf("OpenCode lists no models here: connect a provider first (run 'opencode' and use /connect)")
	case len(ofProvider) == 0:
		return fmt.Errorf("OpenCode has no provider %q here; providers with models: %s", providerID, listed(providers))
	case found == nil:
		return fmt.Errorf("OpenCode's provider %q has no model %q; its models: %s", providerID, modelID, listed(ofProvider))
	case variant == "" || slices.Contains(found.Variants, variant):
		return nil
	case len(found.Variants) == 0:
		return fmt.Errorf("model %s/%s has no variant %q: it declares none", providerID, modelID, variant)
	}
	return fmt.Errorf("model %s/%s has no variant %q; its variants: %s", providerID, modelID, variant, listed(found.Variants))
}

// listed is names sorted and joined, cut to maxListed with a count of the rest.
func listed(names []string) string {
	names = slices.Clone(names)
	sort.Strings(names)
	more := 0
	if len(names) > maxListed {
		more = len(names) - maxListed
		names = names[:maxListed]
	}
	out := strings.Join(names, ", ")
	if more > 0 {
		out += fmt.Sprintf(", and %d more", more)
	}
	return out
}
